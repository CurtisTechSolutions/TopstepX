"""Retry classification and ``Retry-After`` parsing.

Internal module (``SDK_BEHAVIOR.md`` section 7):

* HTTP 429 is retried for every operation, waiting ``Retry-After`` when present;
* HTTP 502/503/504 and transport errors are retried only for read operations;
* nothing else is retried, and HTTP 401 follows the re-login rule instead.
"""

from __future__ import annotations

import math
from datetime import datetime, timezone
from email.utils import parsedate_to_datetime

import httpx

RETRYABLE_READ_STATUSES = frozenset({502, 503, 504})
"""Statuses retried for read operations only."""


def parse_retry_after(value: str | None, now: datetime | None = None) -> float | None:
    """Parse a ``Retry-After`` header (delta-seconds or HTTP-date) into seconds.

    Returns ``None`` when the header is absent or unparseable. A date in the
    past yields ``0``.
    """
    if value is None:
        return None
    text = value.strip()
    if not text:
        return None
    try:
        seconds = float(text)
    except ValueError:
        pass
    else:
        return max(seconds, 0.0) if math.isfinite(seconds) else None
    try:
        when = parsedate_to_datetime(text)
    except (TypeError, ValueError, IndexError):
        return None
    if when.tzinfo is None:
        when = when.replace(tzinfo=timezone.utc)
    current = now or datetime.now(timezone.utc)
    return max((when - current).total_seconds(), 0.0)


def is_retryable_transport_error(exc: httpx.TransportError) -> bool:
    """Whether a transport failure is worth retrying for a read operation.

    Configuration problems (unsupported scheme, a malformed request) are not.
    """
    return not isinstance(exc, (httpx.UnsupportedProtocol, httpx.LocalProtocolError))
