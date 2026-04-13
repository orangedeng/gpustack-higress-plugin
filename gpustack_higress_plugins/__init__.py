"""GPUStack Higress Plugins - Higress Wasm plugins for GPUStack."""

from pathlib import Path

from gpustack_higress_plugins.server import router

# Read version from pyproject.toml (works in both development and installed modes)
PYPROJECT = Path(__file__).parent.parent / "pyproject.toml"
for line in PYPROJECT.read_text().split("\n"):
    if line.startswith("version ="):
        __version__ = line.split("=")[1].strip().strip('"')
        break
else:
    __version__ = "1.0.0"

__all__ = [
    "__version__",
    "router",
]
