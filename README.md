# Higress plugins for GPUStack

## Project Overview

This repository contains custom Higress Proxy-Wasm plugins (extensions) designed for GPUStack, focusing on AI API traffic processing, observability, and enhanced gateway features. Each extension is implemented as a standalone module and can be deployed independently in a Higress-compatible environment.

## Available Extensions

- **gpustack-token-usage**
  - Collects and injects token usage statistics into AI API streaming responses (SSE), including time to first token, average token latency, and tokens per second. Supports real client IP injection and path-based filtering. See [`extensions/gpustack-token-usage/README.md`](./extensions/gpustack-token-usage/README.md) for details.

## Usage

1. Build the desired extension(s) to generate wasm files.
2. Upload the wasm file(s) to the Higress console and configure parameters as needed.
3. Apply the extension(s) to target routes for immediate effect.

## Notes

- All extensions are designed for Proxy-Wasm compatible gateways.
- See each extension's README for specific configuration and usage instructions.
