"""Shared helpers for the examples (not part of the SDK)."""

from __future__ import annotations

import logging
import os
import sys


def require_credentials() -> None:
    """Exit with a helpful message unless TOPSTEPX_USERNAME and TOPSTEPX_API_KEY are set."""
    required = ("TOPSTEPX_USERNAME", "TOPSTEPX_API_KEY")
    missing = [name for name in required if not os.environ.get(name)]
    if missing:
        sys.exit(
            f"Set {' and '.join(missing)} first (optionally TOPSTEPX_API_URL, "
            "TOPSTEPX_USER_HUB_URL and TOPSTEPX_MARKET_HUB_URL for another firm)."
        )


def configure_logging() -> None:
    """Show the SDK's INFO logs (logins, reconnects); the SDK never logs secrets."""
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(name)s %(levelname)s %(message)s")
