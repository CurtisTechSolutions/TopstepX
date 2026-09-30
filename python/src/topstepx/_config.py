"""Client configuration: defaults, retry backoff, rate limits and environment.

Internal module; the public names are re-exported from :mod:`topstepx`.
"""

from __future__ import annotations

import math
import os
from collections.abc import Mapping
from dataclasses import dataclass
from datetime import timedelta
from urllib.parse import urlsplit

import httpx

from ._version import __version__

DEFAULT_BASE_URL = "https://api.topstepx.com"
"""Default REST base URL (the TopstepX deployment)."""
DEFAULT_USER_HUB_URL = "https://rtc.topstepx.com/hubs/user"
"""Default SignalR user hub URL."""
DEFAULT_MARKET_HUB_URL = "https://rtc.topstepx.com/hubs/market"
"""Default SignalR market hub URL."""
DEFAULT_TIMEOUT = 30.0
"""Default request timeout in seconds."""
DEFAULT_CONNECT_TIMEOUT = 10.0
"""Default connect timeout in seconds."""
DEFAULT_MAX_RETRIES = 3
"""Default retry budget per request (see ``SDK_BEHAVIOR.md`` section 7)."""
DEFAULT_REFRESH_MARGIN = 3600.0
"""Default token refresh margin in seconds (one hour before expiry)."""
USER_AGENT = f"topstepx-python/{__version__}"
"""The ``User-Agent`` sent on every request."""

ENV_USERNAME = "TOPSTEPX_USERNAME"
ENV_API_KEY = "TOPSTEPX_API_KEY"
ENV_API_URL = "TOPSTEPX_API_URL"
ENV_USER_HUB_URL = "TOPSTEPX_USER_HUB_URL"
ENV_MARKET_HUB_URL = "TOPSTEPX_MARKET_HUB_URL"


@dataclass(frozen=True, slots=True)
class Backoff:
    """Exponential retry backoff without jitter.

    The n-th retry (1-based) waits ``min(maximum, initial * multiplier ** (n - 1))``
    seconds: 1 s, 2 s, 4 s, ... capped at 30 s by default.

    Attributes:
        initial: Delay before the first retry, in seconds.
        multiplier: Growth factor per retry.
        maximum: Upper bound of any delay, in seconds.
    """

    initial: float = 1.0
    multiplier: float = 2.0
    maximum: float = 30.0

    def __post_init__(self) -> None:
        if self.initial < 0 or self.maximum < 0 or self.multiplier < 1:
            raise ValueError("backoff needs initial >= 0, maximum >= 0 and multiplier >= 1")

    def delay(self, retry: int) -> float:
        """Return the delay before retry number ``retry`` (1-based)."""
        exponent = max(retry - 1, 0)
        try:
            value = self.initial * self.multiplier**exponent
        except OverflowError:
            return self.maximum
        return min(self.maximum, value) if math.isfinite(value) else self.maximum


@dataclass(frozen=True, slots=True)
class RateLimit:
    """A sliding-window limit: at most ``requests`` per ``window`` seconds.

    Attributes:
        requests: Maximum number of requests inside the window.
        window: Window length in seconds.
    """

    requests: int
    window: float

    def __post_init__(self) -> None:
        if self.requests < 1 or self.window <= 0:
            raise ValueError("a rate limit needs requests >= 1 and window > 0")


@dataclass(frozen=True, slots=True)
class RateLimits:
    """Client-side rate limits per bucket; ``None`` disables a bucket.

    The defaults are the documented limits: ``History_GetBars`` 50 requests per
    30 s, every other operation 200 requests per 60 s. Limits are enforced per
    client instance; separate processes (or clients) do not share them.

    Attributes:
        general: Limit shared by every operation except ``History_GetBars``.
        history: Limit of ``History_GetBars``.
    """

    general: RateLimit | None = RateLimit(200, 60.0)
    history: RateLimit | None = RateLimit(50, 30.0)


@dataclass(frozen=True, slots=True)
class ClientSettings:
    """Resolved, validated settings of one client instance."""

    base_url: str
    user_hub_url: str
    market_hub_url: str
    timeout: httpx.Timeout
    max_retries: int
    backoff: Backoff
    rate_limits: RateLimits | None
    validate_requests: bool
    refresh_margin: float


def _check_url(name: str, url: str, schemes: tuple[str, ...]) -> str:
    parts = urlsplit(url)
    if parts.scheme.lower() not in schemes or not parts.netloc:
        raise ValueError(f"{name} must be an absolute {'/'.join(schemes)} URL, got {url!r}")
    return url


def build_settings(
    *,
    base_url: str,
    user_hub_url: str,
    market_hub_url: str,
    timeout: float | httpx.Timeout | None,
    max_retries: int,
    backoff: Backoff,
    rate_limit: bool | RateLimits | None,
    validate_requests: bool,
    refresh_margin: float | timedelta,
) -> ClientSettings:
    """Validate constructor arguments and resolve them into :class:`ClientSettings`."""
    hub_schemes = ("https", "http", "wss", "ws")
    base_url = _check_url("base_url", base_url, ("https", "http")).rstrip("/")
    _check_url("user_hub_url", user_hub_url, hub_schemes)
    _check_url("market_hub_url", market_hub_url, hub_schemes)
    if isinstance(timeout, httpx.Timeout):
        resolved_timeout = timeout
    elif timeout is None:
        resolved_timeout = httpx.Timeout(None)
    else:
        if timeout <= 0:
            raise ValueError("timeout must be positive")
        resolved_timeout = httpx.Timeout(timeout, connect=min(DEFAULT_CONNECT_TIMEOUT, timeout))
    if max_retries < 0:
        raise ValueError("max_retries must be >= 0")
    if rate_limit is True:
        limits: RateLimits | None = RateLimits()
    elif rate_limit is False or rate_limit is None:
        limits = None
    else:
        limits = rate_limit
    margin = (
        refresh_margin.total_seconds()
        if isinstance(refresh_margin, timedelta)
        else float(refresh_margin)
    )
    if margin < 0:
        raise ValueError("refresh_margin must be >= 0")
    return ClientSettings(
        base_url=base_url,
        user_hub_url=user_hub_url,
        market_hub_url=market_hub_url,
        timeout=resolved_timeout,
        max_retries=max_retries,
        backoff=backoff,
        rate_limits=limits,
        validate_requests=validate_requests,
        refresh_margin=margin,
    )


def env_overrides(environ: Mapping[str, str] | None = None) -> dict[str, str]:
    """Read the documented ``TOPSTEPX_*`` variables into constructor keyword arguments.

    Empty variables are ignored.
    """
    env = os.environ if environ is None else environ
    mapping = {
        ENV_USERNAME: "username",
        ENV_API_KEY: "api_key",
        ENV_API_URL: "base_url",
        ENV_USER_HUB_URL: "user_hub_url",
        ENV_MARKET_HUB_URL: "market_hub_url",
    }
    result: dict[str, str] = {}
    for variable, argument in mapping.items():
        value = env.get(variable, "").strip()
        if value:
            result[argument] = value
    return result
