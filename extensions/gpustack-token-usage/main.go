package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm"
	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/higress-group/wasm-go/pkg/tokenusage"
	"github.com/higress-group/wasm-go/pkg/wrapper"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Constants for log keys in Filter State
const (
	pluginName = "gpustack-token-usage"
)

// Constants for context keys
const (
	IsStreamingResponse        = "is_streaming_response"
	StatisticsRequestStartTime = "gpustack_request_start_time"
	StatisticsFirstTokenTime   = "gpustack_first_token_time"
	TimeToFirstTokenDuration   = "gpustack_llm_first_token_duration"

	IncompleteChunk     = "gpustack_incomplete_chunk"
	IncompleteChunkData = "gpustack_incomplete_chunk_data"
	UsageExtraKey       = "gpustack_usage_extra"
	ModifiedKey         = "gpustack_usage_modified"
)

func main() {}

func init() {
	wrapper.SetCtx(
		// Plugin name
		pluginName,
		// Set custom function for parsing plugin configuration
		wrapper.ParseConfig(parseConfig),
		// Set custom function for processing request headers
		wrapper.ProcessRequestHeaders(onHttpRequestHeaders),
		// Set custom function for processing response headers
		wrapper.ProcessResponseHeaders(onHttpResponseHeaders),
		// Set custom function for processing streaming response body
		wrapper.ProcessStreamingResponseBody(onStreamingResponseBody),
	)
}

// PluginConfig Custom plugin configuration
type PluginConfig struct {
	RealIPToHeader     string
	EnableOnPathSuffix []string
}

func (c *PluginConfig) shouldProcess(targetURI string) bool {
	// check target uri is vaild or not
	u, err := url.ParseRequestURI(targetURI)
	if err != nil {
		proxywasm.LogDebugf("shouldProcess: invalid targetURI: %s", targetURI)
		return false
	}
	// filterred by path suffix
	path := u.Path
	for _, suffix := range c.EnableOnPathSuffix {
		if len(suffix) > 0 && len(path) >= len(suffix) && path[len(path)-len(suffix):] == suffix {
			proxywasm.LogDebugf("shouldProcess: matched suffix %s for path %s", suffix, path)
			return true
		}
	}
	return false
}

// The YAML configuration filled in the console will be automatically converted to JSON,
// so we can directly parse the configuration from this JSON parameter
func parseConfig(json gjson.Result, config *PluginConfig) error {
	config.RealIPToHeader = json.Get("realIPToHeader").String()
	suffixes := json.Get("enableOnPathSuffix").Array()
	defaultSuffixes := map[string]bool{
		"/chat/completions": true,
		"/completions":      true,
	}
	for _, suffix := range suffixes {
		path := suffix.String()
		if _, err := url.ParseRequestURI(path); err != nil {
			proxywasm.LogDebugf("onParseConfig: %s is not a valid uri, skipping", path)
		}
		defaultSuffixes[path] = true
	}
	config.EnableOnPathSuffix = []string{}
	for path := range defaultSuffixes {
		config.EnableOnPathSuffix = append(config.EnableOnPathSuffix, path)
	}
	return nil
}

func realIpHandler(_ wrapper.HttpContext, headerName string) {
	var (
		realIpStr string
	)
	// Get all request headers
	if headerName == "" {
		return
	}
	headers, err := proxywasm.GetHttpRequestHeaders()
	if err != nil {
		proxywasm.LogDebugf("failed to get request headers, %s", err)
		return
	}
	data, err := proxywasm.GetProperty([]string{"source", "address"})
	if err != nil {
		proxywasm.LogDebugf("failed to get remote address, %s", err)
		return
	}
	// Only keeps the host without port
	host, _, err := net.SplitHostPort(string(data))
	if err == nil {
		realIpStr = host
	}
	headers = append(headers, [2]string{
		headerName, realIpStr,
	})
	_ = proxywasm.ReplaceHttpRequestHeaders(headers)
}

// onHttpRequestHeaders processes the request headers and logs them if enabled
func onHttpRequestHeaders(ctx wrapper.HttpContext, config PluginConfig) types.Action {
	realIpHandler(ctx, config.RealIPToHeader)

	if !config.shouldProcess(ctx.Path()) {
		return types.ActionContinue
	}

	ctx.SetContext(StatisticsRequestStartTime, time.Now().UnixMilli())

	return types.ActionContinue
}

func isStreamingResponse(headers map[string][]string) bool {
	// Transfer-Encoding: chunked
	if tes, ok := headers["transfer-encoding"]; ok {
		for _, te := range tes {
			if strings.ToLower(te) == "chunked" {
				return true
			}
		}
	}

	// Check for Content-Type
	if cts, ok := headers["content-type"]; ok {
		for _, contentType := range cts {
			ct := strings.ToLower(contentType)
			if strings.Contains(ct, "text/event-stream") ||
				strings.Contains(ct, "application/stream+json") ||
				(strings.Contains(ct, "text/plain") && hasHeaderValue(headers, "x-stream", "true")) {
				return true
			}
			if strings.Contains(ct, "application/json") {
				return false
			}
		}
	}

	// If there is no Content-Length and status code is 2xx (except 204/304)
	if _, hasContentLength := headers["content-length"]; !hasContentLength {
		statusCodes := headers[":status"]
		for _, codeStr := range statusCodes {
			statusCode, err := strconv.Atoi(codeStr)
			if err == nil && statusCode != 204 && statusCode != 304 && statusCode >= 200 && statusCode < 300 {
				return true
			}
		}
	}

	return false
}

// Check if header key contains a specific value
func hasHeaderValue(headers map[string][]string, key, value string) bool {
	if vs, ok := headers[strings.ToLower(key)]; ok {
		for _, v := range vs {
			if strings.EqualFold(v, value) {
				return true
			}
		}
	}
	return false
}

func onHttpResponseHeaders(ctx wrapper.HttpContext, config PluginConfig) types.Action {
	_, ok := ctx.GetContext(StatisticsRequestStartTime).(int64)
	if !ok {
		return types.ActionContinue
	}
	responseHeaders, err := proxywasm.GetHttpResponseHeaders()
	if err != nil {
		proxywasm.LogDebugf("failed to get response headers, %v", err)
		return types.ActionContinue
	}
	headerMap := convertHeaders(responseHeaders)
	isStreaming := isStreamingResponse(headerMap)
	ctx.SetContext(IsStreamingResponse, isStreaming)
	if !isStreaming {
		return types.HeaderStopIteration
	}
	return types.ActionContinue
}

// Requires to calculate time_to_first_token_ms, time_per_output_token_ms and tokens_per_second.
func onStreamingResponseBody(ctx wrapper.HttpContext, config PluginConfig, data []byte, endOfStream bool) []byte {
	// Get requestStartTime from http context
	requestStartTime, ok := ctx.GetContext(StatisticsRequestStartTime).(int64)
	if !ok {
		return data
	}
	// If this is the first chunk, record first token duration metric and span attribute
	if ctx.GetContext(StatisticsFirstTokenTime) == nil {
		firstTokenTime := time.Now().UnixMilli()
		ctx.SetContext(StatisticsFirstTokenTime, firstTokenTime)
		ctx.SetContext(TimeToFirstTokenDuration, firstTokenTime-requestStartTime)
		proxywasm.LogDebugf("onStreamingResponseBody: firstTokenTime=%d, timeToFirstTokenDuration=%d", firstTokenTime, firstTokenTime-requestStartTime)
	}
	isStreamingResponse := ctx.GetBoolContext(IsStreamingResponse, false)
	if !isStreamingResponse {
		// for non-stream sresponse
		usageExtra := getUsageExtra(ctx, data)
		if usageExtra == nil {
			return data
		}
		new_data := process_data_with_token(data, usageExtra)
		_ = proxywasm.ReplaceHttpResponseHeader("content-length", strconv.Itoa(len(new_data)))
		return new_data
	}

	chunks := bytes.SplitSeq(wrapper.UnifySSEChunk(data), []byte("\n\n"))
	var rtn = [][]byte{}
	for chunk := range chunks {
		if ctx.GetBoolContext(ModifiedKey, false) {
			rtn = append(rtn, chunk)
			continue
		}
		chunk = mergeLargeUsageChunks(ctx, chunk)
		if chunk == nil {
			// to avoid the two chunk didn't join with \n\n
			rtn = append(rtn, []byte(""))
			continue
		}
		trimed_data := bytes.TrimPrefix(chunk, []byte("data: "))
		if !json.Valid(trimed_data) {
			// if is part of the json
			rtn = append(rtn, chunk)
			continue
		}
		result := gjson.GetBytes(trimed_data, "usage")
		// no usage found
		if !result.Exists() {
			rtn = append(rtn, chunk)
			continue
		}
		proxywasm.LogDebugf("valid chunk: %s", string(trimed_data))
		usageExtra := getUsageExtra(ctx, trimed_data)
		if usageExtra == nil {
			proxywasm.LogWarnf("no usage is found in a chunk with usage bytes, chunk data is %s", string(chunk))
			rtn = append(rtn, chunk)
			continue
		}
		modified := process_data_with_token(trimed_data, usageExtra)
		proxywasm.LogDebugf("modified: %s", string(modified))
		rtn = append(rtn, append([]byte("data: "), modified...))
		ctx.SetContext(ModifiedKey, true)
	}
	return bytes.Join(rtn, []byte("\n\n"))
}

func process_data_with_token(data []byte, usageExtra map[string]any) []byte {
	var err error
	// trim data: prefix
	var rtn = string(bytes.TrimPrefix(data, []byte("data: ")))
	for path, value := range usageExtra {
		var new_data string
		new_data, err = sjson.Set(rtn, fmt.Sprintf("usage.%s", path), value)
		if err != nil {
			continue
		}
		rtn = new_data
	}
	return []byte(rtn)
}

// headers: [][2]string -> map[string][]string
func convertHeaders(hs [][2]string) map[string][]string {
	ret := make(map[string][]string)
	for _, h := range hs {
		k, v := strings.ToLower(h[0]), h[1]
		ret[k] = append(ret[k], v)
	}
	return ret
}

func getUsageExtra(ctx wrapper.HttpContext, data []byte) map[string]any {
	// alread has extra info stored
	var usageExtraInfo map[string]any
	extra := ctx.GetContext(UsageExtraKey)
	if extra != nil {
		return extra.(map[string]any)
	}
	usage := tokenusage.GetTokenUsage(ctx, data)
	if usage.TotalToken == 0 {
		return nil
	}
	proxywasm.LogDebugf("onStreamingResponseBody: token usage: total=%d, output=%d", usage.TotalToken, usage.OutputToken)
	firstTokenTime := ctx.GetContext(StatisticsFirstTokenTime).(int64)
	if firstTokenTime == 0 {
		return nil
	}

	responseEndTime := time.Now().UnixMilli()
	outputTokenDuration := responseEndTime - firstTokenTime
	timeToFirstTokenDuration := ctx.GetContext(TimeToFirstTokenDuration).(int64)
	proxywasm.LogDebugf("onStreamingResponseBody: responseEndTime=%d, outputTokenDuration=%d, timeToFirstTokenDuration=%d", responseEndTime, outputTokenDuration, timeToFirstTokenDuration)
	var timePerOutputToken float64 = 0
	if usage.OutputToken > 1 {
		timePerOutputToken = float64(outputTokenDuration) / float64(usage.OutputToken-1)
	}
	var tokensPerSecond float64 = 0
	if outputTokenDuration > 0 {
		tokensPerSecond = float64(usage.OutputToken-1) / (float64(outputTokenDuration) / 1000)
	}

	usageExtraInfo = map[string]any{
		"time_to_first_token_ms":   timeToFirstTokenDuration,
		"time_per_output_token_ms": math.Round(timePerOutputToken*100) / 100,
		"tokens_per_second":        math.Round(tokensPerSecond*100) / 100,
	}
	ctx.SetContext(UsageExtraKey, usageExtraInfo)
	return usageExtraInfo
}

func mergeLargeUsageChunks(ctx wrapper.HttpContext, chunk []byte) []byte {
	trimed_data := bytes.TrimPrefix(chunk, []byte("data: "))
	if json.Valid(trimed_data) {
		ctx.SetContext(IncompleteChunk, false)
		return chunk
	}
	// end of streaming
	if len(bytes.TrimSpace(trimed_data)) == 0 {
		return chunk
	}
	ctx.SetContext(IncompleteChunk, true)
	deltaMessage := ctx.GetByteSliceContext(IncompleteChunkData, []byte{})
	trimed_data = append(deltaMessage, trimed_data...)
	proxywasm.LogDebugf("the delta is stored: %s", string(trimed_data))

	if !json.Valid(trimed_data) {
		ctx.SetContext(IncompleteChunkData, trimed_data)
		return nil
	}
	ctx.SetContext(IncompleteChunk, false)
	ctx.SetContext(IncompleteChunkData, nil)
	return append([]byte("data: "), trimed_data...)
}
