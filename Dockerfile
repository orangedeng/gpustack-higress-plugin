# Multi-stage Dockerfile for GPUStack Higress Plugins Server
#
# Usage:
#   Build image:        docker build -t gpustack-higress-plugins .
#   Build & extract whl: docker build --target=whl-output --output type=local,dest=./dist
#   Build & extract whl (specific file): docker build --target=whl-output --output type=local,dest=./dist --output=type=local,dest=./dist/gpustack_higress_plugins-1.0.0-py3-none-any.whl

# Stage 1: Build Go plugins
FROM golang:1.24-alpine AS go-builder
ARG GOPROXY
ENV GOPROXY=${GOPROXY:-https://proxy.golang.org,direct}
RUN apk add --no-cache git make

WORKDIR /project

# Copy Go plugins source
COPY extensions/ extensions/
COPY gpustack_higress_plugins/ gpustack_higress_plugins/

# Build all local Go plugins
RUN cd extensions && make build-all && \
    rm -rf */*.go */go.mod */go.sum

# Stage 2: Build Python wheel package
FROM python:3.11-slim AS whl-builder

WORKDIR /app

# Install system dependencies
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked,target=/var/lib/apt \
    apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl python3-venv && \
    rm -rf /var/lib/apt/lists/*

# Copy Python package files and config from host
COPY pyproject.toml README.md uv.lock ./
COPY gpustack_higress_plugins/ ./gpustack_higress_plugins/
COPY scripts/ scripts/
COPY extensions/remote_plugins.yaml ./extensions/

# Copy built Go plugins from builder stage
COPY --from=go-builder /project/gpustack_higress_plugins/plugins ./gpustack_higress_plugins/plugins

# Build wheel package
# Fetch remote OCI plugins using oras (mounted from official image)
RUN --mount=type=bind,from=ghcr.io/oras-project/oras:v1.3.1,source=/bin/oras,target=/bin/oras <<EOF

set -e

# Create venv for build
python3 -m venv .venv
.venv/bin/pip install --no-cache-dir uv

# Install project dependencies
.venv/bin/uv sync --no-dev

# Fetch remote OCI plugins if configured
if [ -f extensions/remote_plugins.yaml ]; then
    .venv/bin/python scripts/fetch_remote_plugins.py --config extensions/remote_plugins.yaml --oras oras
fi

# Build wheel package using uv
.venv/bin/uv build --out-dir /dist

# Show what was built
echo "Built wheel files:"
ls -lh /dist/

# Clean up build venv
rm -rf .venv
EOF

# Stage 3: Output stage (scratch for extracting whl files)
FROM scratch AS whl-output
COPY --from=whl-builder /dist/ /

# Stage 4: Create final runtime image
FROM python:3.11-slim AS runtime

WORKDIR /app

# Copy built wheel from whl-builder stage
COPY --from=whl-builder /dist /tmp/dist/

# Install the wheel (contains all plugins and code)
RUN pip install --no-cache-dir --break-system-packages /tmp/dist/*.whl && \
    rm -rf /tmp/dist

# Create a non-root user
RUN useradd -m -u 1000 plugins && \
    chown -R plugins:plugins /app

USER plugins

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD python -c "from urllib.request import urlopen; urlopen('http://localhost:8080/', timeout=2)" || exit 1

# Set default command to start the plugin server
CMD ["gpustack-plugins", "start", "--port", "8080", "--host", "0.0.0.0"]
