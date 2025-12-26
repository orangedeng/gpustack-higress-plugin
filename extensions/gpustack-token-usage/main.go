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
	proxywasm.LogDebugf("shouldProcess: no match for path %s", path)
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

	usageExtra := getUsageExtra(ctx, data)
	if usageExtra == nil {
		return data
	}

	isStreamingResponse := ctx.GetBoolContext(IsStreamingResponse, false)
	proxywasm.LogDebugf("origin datas: %s", string(data))
	if isStreamingResponse {
		chunks := bytes.SplitSeq(wrapper.UnifySSEChunk(data), []byte("\n\n"))
		var rtn = [][]byte{}
		for chunk := range chunks {
			proxywasm.LogDebugf("chunk data: %s", string(chunk))
			data := bytes.TrimPrefix(chunk, []byte("data: "))
			/***
			In case that the chunk doesn't contained the complete json response,
			we need to save the incomplete json and merge them togather.
			For example:
			1. The first frame contains data: {"xxx":111,"usage":{},"xxx":xx,"x
			2. The second frame contains  xx":1234,"xxxx":"abcd"}
			In the first frame, mergeLargeUsageChunks should return nil and save the data in context.
			In the second frame, mergeLargeUsageChunks should return the complete json and remove the incomplete content in context.
			***/
			data = mergeLargeUsageChunks(ctx, data)
			if data != nil && json.Valid(data) {
				modified := process_data_with_token(data, usageExtra)
				proxywasm.LogDebugf("modified: %s", string(modified))
				rtn = append(rtn, append([]byte("data: "), modified...))
				ctx.SetContext(UsageExtraKey, nil)
			} else if data != nil {
				rtn = append(rtn, chunk)
			}
		}
		return bytes.Join(rtn, []byte("\n\n"))
	} else {
		new_data := process_data_with_token(data, usageExtra)
		_ = proxywasm.ReplaceHttpResponseHeader("content-length", strconv.Itoa(len(new_data)))
		return new_data
	}

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
	// the data: prefix is removed from chunk already.
	incompleteChunk := !json.Valid(chunk)
	hasUsage := gjson.GetBytes(chunk, "usage")
	isStoredIncomplete := ctx.GetBoolContext(IncompleteChunk, false)
	if incompleteChunk && !hasUsage.Exists() && !isStoredIncomplete {
		return chunk
	}
	// the chunk must contain usage block
	ctx.SetContext(IncompleteChunk, true)

	// end of streaming
	if len(bytes.TrimSpace(chunk)) == 0 {
		ctx.SetContext(IncompleteChunk, false)
		chunk = ctx.GetByteSliceContext(IncompleteChunkData, chunk)
		ctx.SetContext(IncompleteChunkData, nil)
		return chunk
	}
	deltaMessage := ctx.GetByteSliceContext(IncompleteChunkData, []byte{})
	chunk = append(deltaMessage, chunk...)

	if !json.Valid(chunk) {
		ctx.SetContext(IncompleteChunkData, chunk)
		chunk = nil
	}

	return chunk
}
