# GPUStack Higress Plugins

Higress Proxy-Wasm plugins for GPUStack, providing AI API traffic processing, observability, and enhanced gateway features.

## Overview

This repository contains custom Higress Proxy-Wasm plugins designed for GPUStack, distributed as a Python package that includes pre-compiled Wasm plugins and a built-in HTTP file server for serving them.

## Installation

```bash
# Install the package
pip install gpustack-higress-plugins

# Or with GPUStack
pip install "gpustack[higress]"
```

## Available Plugins

- **gpustack-token-usage** - Collects and injects token usage statistics into AI API streaming responses (SSE), including time to first token, average token latency, and tokens per second. Supports real client IP injection and path-based filtering.

- **gpustack-set-header-pre-route** - Automatically injects the route name and model name into HTTP request headers before routing, based on configurable path suffixes or prefixes.

## Usage

### Start Plugin Server

```bash
# Start the built-in HTTP file server
gpustack-plugins start --port 8080

# Or with custom host
gpustack-plugins start --port 8080 --host 0.0.0.0

# Or use Python
python -m gpustack_higress_plugins.cli start
```

The server will be available at:
- **Plugins API**: `http://localhost:8080/plugins`
- **Manifest**: `http://localhost:8080/manifest.json`
- **Plugin files**: `http://localhost:8080/plugins/{name}/{version}/plugin.wasm`

### API Endpoints

```bash
# List all plugins
curl http://localhost:8080/plugins

# Get plugin info
curl http://localhost:8080/plugins/gpustack-token-usage

# Download a plugin
curl http://localhost:8080/plugins/gpustack-token-usage/1.0.0/plugin.wasm -o plugin.wasm

# Get metadata
curl http://localhost:8080/plugins/gpustack-token-usage/1.0.0/metadata.txt

# Get manifest
curl http://localhost:8080/manifest.json
```

### Python API

```python
import json
from pathlib import Path

# Read plugin manifest directly
manifest_file = Path("gpustack_higress_plugins/manifest.json")
with open(manifest_file) as f:
    manifest = json.load(f)

# Construct plugin URL manually
# Format: http://{host}:{port}/plugins/{name}/{version}/plugin.wasm
url = "http://localhost:8080/plugins/gpustack-token-usage/1.0.0/plugin.wasm"
```

### Configure Higress WasmPlugin

```yaml
apiVersion: extensions.higress.io/v1alpha1
kind: WasmPlugin
metadata:
  name: gpustack-token-usage
  namespace: higress-system
spec:
  url: http://plugin-server:8080/gpustack-token-usage/1.0.0/plugin.wasm
  defaultConfig:
    realIPToHeader: x-gpustack-real-ip
```

## Development

### Build Plugins

```bash
# Build all plugins
make build

# Or build specific plugin
make -C extensions build PLUGIN_NAME=gpustack-token-usage
```

### Run Tests

```bash
# Test Go plugins
make -C extensions test PLUGIN_NAME=gpustack-token-usage

# Test Python package
make test
```

### Development Installation

```bash
# Install in editable mode
make dev

# Or with pip
pip install -e ".[dev]"
```

## Deployment

### All-in-One Mode (Docker Compose)

The plugin server runs inside the GPUStack container:

```yaml
# docker-compose.yml
services:
  gpustack:
    image: gpustack/gpustack:latest
    environment:
      - HIGRESS_PLUGINS_ENABLED=true
    # Plugin server automatically starts
```

### Kubernetes Mode (Helm)

Deploy the plugin server as a separate service:

```yaml
# values.yaml
plugins:
  enabled: true
  image: gpustack/higress-plugins:1.0.0
  service:
    type: ClusterIP
    port: 8080
```

```bash
helm install gpustack-plugins ./deploy/helm/gpustack-plugins
```

## Docker Image

```bash
# Build Docker image
make image

# Or use docker directly
docker build -t gpustack/higress-plugins:1.0.0 .

# Run standalone
docker run -p 8080:8080 gpustack/higress-plugins:1.0.0
```

## Project Structure

```
gpustack-higress-plugins/
├── extensions/                    # Go plugin source code
│   ├── gpustack-token-usage/
│   │   ├── main.go
│   │   ├── go.mod
│   │   └── VERSION
│   └── gpustack-set-header-pre-route/
│       └── ...
├── gpustack_higress_plugins/      # Python package
│   ├── __init__.py
│   ├── main.py                    # CLI entry point
│   ├── server.py                  # HTTP file server
│   ├── plugins/                   # Compiled .wasm files (built)
│   │   ├── gpustack-token-usage/1.0.0/plugin.wasm
│   │   └── ...
│   └── manifest.json              # Plugin manifest
├── scripts/                       # Build scripts
│   └── generate_manifest.py
├── pyproject.toml                 # Python package config
├── Makefile                       # Root Makefile (Python package)
└── extensions/Makefile            # Plugin Makefile (Go plugins)
```

## Versioning

- Package version follows Semantic Versioning (MAJOR.MINOR.PATCH)
- Each plugin shares the same version as the package
- See `gpustack_higress_plugins/_version.py` for current version

## License

Apache License 2.0

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## Support

- GitHub Issues: [https://github.com/gpustack/gpustack-higress-plugins/issues](https://github.com/gpustack/gpustack-higress-plugins/issues)
- GPUStack Documentation: [https://docs.gpustack.io](https://docs.gpustack.io)
