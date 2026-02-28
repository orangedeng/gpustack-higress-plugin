package main

import (
	"errors"
	"strings"

	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm"
	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/higress-group/wasm-go/pkg/wrapper"
	"github.com/tidwall/gjson"
)

func main() {}

func init() {
	wrapper.SetCtx(
		"gpustack-set-header-pre-route",
		wrapper.ParseConfig(parseConfig),
		wrapper.ProcessRequestHeaders(onHttpRequestHeaders),
	)
}

type SetHeaderPreRouteConfig struct {
	routeNameHeader    string
	clusterNameHeader  string
	enableOnPathSuffix []string
	enableOnPathPrefix []string
}

func parseConfig(json gjson.Result, config *SetHeaderPreRouteConfig) error {
	config.routeNameHeader = json.Get("routeNameHeader").String()
	config.clusterNameHeader = json.Get("clusterNameHeader").String()
	if config.routeNameHeader == "" && config.clusterNameHeader == "" {
		return errors.New("one of the routeNameHeader or clusterNameHeader should be configured")
	}

	enableOnPathSuffix := json.Get("enableOnPathSuffix")
	if enableOnPathSuffix.Exists() && enableOnPathSuffix.IsArray() {
		for _, item := range enableOnPathSuffix.Array() {
			config.enableOnPathSuffix = append(config.enableOnPathSuffix, item.String())
		}
	} else {
		// Default suffixes if not provided
		config.enableOnPathSuffix = []string{
			"/completions",
			"/embeddings",
			"/images/generations",
			"/audio/speech",
			"/fine_tuning/jobs",
			"/moderations",
			"/image-synthesis",
			"/video-synthesis",
			"/rerank",
			"/messages",
			"/responses",
		}
	}
	enableOnPathPrefix := json.Get("enableOnPathPrefix")
	if enableOnPathPrefix.Exists() && enableOnPathPrefix.IsArray() {
		for _, item := range enableOnPathPrefix.Array() {
			config.enableOnPathPrefix = append(config.enableOnPathPrefix, item.String())
		}
	} else {
		config.enableOnPathPrefix = []string{
			"/model/proxy",
		}
	}

	return nil
}

func onHttpRequestHeaders(ctx wrapper.HttpContext, config SetHeaderPreRouteConfig) types.Action {
	path, err := proxywasm.GetHttpRequestHeader(":path")
	if err != nil {
		return types.ActionContinue
	}

	// Remove query parameters for suffix check
	if idx := strings.Index(path, "?"); idx != -1 {
		path = path[:idx]
	}

	enable := false
	for _, suffix := range config.enableOnPathSuffix {
		if suffix == "*" || strings.HasSuffix(path, suffix) {
			enable = true
			break
		}
	}
	if !enable {
		for _, prefix := range config.enableOnPathPrefix {
			if prefix == "" || strings.HasPrefix(path, prefix) {
				enable = true
				break
			}
		}
	}

	if !enable {
		return types.ActionContinue
	}

	if config.routeNameHeader != "" {
		routeName, err := GetRouteName()
		if err != nil {
			proxywasm.LogWarnf("failed to get route_name from property, %v", err)
		}
		if err := proxywasm.AddHttpRequestHeader(config.routeNameHeader, routeName); err != nil {
			proxywasm.LogWarnf("failed to set http header %s, %v", config.routeNameHeader, err)
		}
	}

	if config.clusterNameHeader != "" {
		clusterName, err := GetClusterName()
		if err != nil {
			proxywasm.LogWarnf("failed to get cluster_name from property, %v", err)
		}
		if err := proxywasm.AddHttpRequestHeader(config.clusterNameHeader, clusterName); err != nil {
			proxywasm.LogWarnf("failed to set http header %s, %v", config.clusterNameHeader, err)
		}
	}

	return types.ActionContinue
}

func GetRouteName() (string, error) {
	if raw, err := proxywasm.GetProperty([]string{"route_name"}); err != nil {
		return "", err
	} else {
		return string(raw), nil
	}
}

func GetClusterName() (string, error) {
	cluster := ""
	if raw, err := proxywasm.GetProperty([]string{"cluster_name"}); err != nil {
		return "", err
	} else {
		cluster = string(raw)
	}
	split := strings.SplitN(cluster, "|", 4)
	if len(split) != 4 {
		return "", errors.New("the cluster_name is not in the right format, expected format xxxx|xx||xxx")
	}
	return split[3], nil
}
