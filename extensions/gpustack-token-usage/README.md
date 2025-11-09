# gpustack-token-usage Plugin

## Overview

`gpustack-token-usage` is a Higress Proxy-Wasm plugin for collecting and injecting token usage statistics into AI API streaming responses. It tracks metrics such as total tokens, time to first token, average token latency, and tokens per second. The plugin supports SSE (Server-Sent Events) protocol and can automatically recognize and process various AI API paths.

## Key Features

- Automatic recognition of mainstream AI API paths (OpenAI, Alibaba Cloud, etc.)
- Customizable URI path suffixes for processing
- Automatic statistics for time to first token, average token latency, tokens per second
- Supports multi-chunk SSE response processing
- Can inject the real client IP into a specified header

## Configuration

The plugin supports the following configuration options (YAML/JSON):

```yaml
realIPToHeader: "X-GPUStack-Real-IP" # Optional, specify which header to inject the real IP
enableOnPathSuffix: # Optional, list of URI path suffixes to process
  - "/chat/completions"
  - "/completions"
  - "/embeddings"
  - "/audio/transcriptions"
  - "/audio/speech"
  - "/images/generations"
  - "/images/edits"
  - "/rerank"
```

If `enableOnPathSuffix` is not configured, the plugin will use the above default AI paths.

Recommended priority: 910
Recommanded phase: `UNSPECIFIED_PHASE`

## Usage Effects

1. **Token Statistics Injection**:

   - Automatically injects the following statistics into the `usage` field of SSE responses:
     - `time_to_first_token_ms`: Time to first token (ms)
     - `time_per_output_token_ms`: Average output token latency (ms)
     - `tokens_per_second`: Token rate (tokens/sec)

2. **Real IP Injection**:

   - If `realIPToHeader` is configured, the plugin will inject the real client IP into the specified header.

3. **Multi-chunk SSE Support**:
   - For streaming responses, each chunk is correctly recognized and processed; non-usage chunks remain unchanged.

## Example

Assume the original SSE response chunk:

```json
data: {"usage": {"total_tokens": 100}}
```

After processing, statistics are automatically injected:

```json
data: {"usage": {"total_tokens": 100, "time_to_first_token_ms": 123, "time_per_output_token_ms": 45, "tokens_per_second": 6.7}}
```

## Deployment & Usage

1. Build the wasm file
2. Upload the plugin and configure parameters in the Higress console
3. Apply to the target route to take effect

## Notes

- Only configured path suffixes are processed; other paths are ignored
- Only chunks containing the `usage` field will have statistics injected
- The plugin must run in a Proxy-Wasm compatible gateway environment
