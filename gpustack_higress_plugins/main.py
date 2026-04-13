"""Command-line interface and server for GPUStack Higress Plugins."""

import argparse
import sys
from pathlib import Path
from typing import Optional

import uvicorn
from fastapi import FastAPI

from gpustack_higress_plugins.server import router


def _get_version() -> str:
    """Get package version from pyproject.toml."""
    pyproject = Path(__file__).parent.parent / "pyproject.toml"
    if pyproject.exists():
        for line in pyproject.read_text().split("\n"):
            if line.startswith("version ="):
                return line.split("=")[1].strip().strip('"')
    return "0.0.0"


def create_app(
    title: str = "GPUStack Higress Plugins",
    description: str = "HTTP server for Higress Wasm plugins",
    version: Optional[str] = None,
) -> FastAPI:
    """Create a FastAPI app with the plugin router.

    Args:
        title: App title
        description: App description
        version: App version (defaults to package version)

    Returns:
        Configured FastAPI app
    """
    if version is None:
        version = _get_version()

    app = FastAPI(
        title=title,
        description=description,
        version=version,
    )
    app.include_router(router)
    return app


def main(argv: Optional[list] = None) -> int:
    """Main CLI entry point.

    Args:
        argv: Command line arguments (defaults to sys.argv[1:])

    Returns:
        Exit code
    """
    parser = argparse.ArgumentParser(
        description="GPUStack Higress Plugins Server",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  Start server:  gpustack-plugins start --port 8080 --host 0.0.0.0
  Show version:  gpustack-plugins --version
        """,
    )

    parser.add_argument("--version", action="version", version=f"%(prog)s {_get_version()}")

    subparsers = parser.add_subparsers(dest="command", help="Available commands")

    # Start server command
    start_parser = subparsers.add_parser("start", help="Start the plugin HTTP server")
    start_parser.add_argument(
        "--port", type=int, default=8080, help="Port to listen on (default: 8080)"
    )
    start_parser.add_argument(
        "--host", default="0.0.0.0", help="Host to bind to (default: 0.0.0.0)"
    )
    start_parser.add_argument(
        "--log-level",
        default="info",
        choices=["critical", "error", "warning", "info", "debug"],
        help="Log level (default: info)",
    )

    args = parser.parse_args(argv)

    if args.command == "start":
        app = create_app()
        uvicorn.run(
            app,
            host=args.host,
            port=args.port,
            log_level=args.log_level,
        )
        return 0
    else:
        parser.print_help()
        return 0


if __name__ == "__main__":
    sys.exit(main())
