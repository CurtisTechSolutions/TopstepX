"""Client-side sliding-window rate limiting.

Internal module. :class:`RateLimiter` never sleeps itself: :meth:`reserve`
either takes a slot (returning ``0``) or returns how long the caller must wait
before trying again, which lets the sync and async clients share it.
"""

from __future__ import annotations

import threading
import time
from collections import deque
from collections.abc import Callable

from ._config import RateLimit, RateLimits
from .operations import RateBucket


class SlidingWindow:
    """At most ``limit.requests`` acquisitions in any ``limit.window`` seconds."""

    def __init__(self, limit: RateLimit) -> None:
        self.limit = limit
        self._stamps: deque[float] = deque()
        self._lock = threading.Lock()

    def reserve(self, now: float) -> float:
        """Take a slot at time ``now`` (returning ``0``) or return the wait in seconds.

        The wait lasts until the oldest entry leaves the window.
        """
        with self._lock:
            horizon = now - self.limit.window
            stamps = self._stamps
            while stamps and stamps[0] <= horizon:
                stamps.popleft()
            if len(stamps) < self.limit.requests:
                stamps.append(now)
                return 0.0
            return max(stamps[0] - horizon, 1e-3)


class RateLimiter:
    """One sliding window per :class:`~topstepx.operations.RateBucket`."""

    def __init__(
        self, limits: RateLimits | None, clock: Callable[[], float] = time.monotonic
    ) -> None:
        self.clock = clock
        self._windows: dict[RateBucket, SlidingWindow] = {}
        if limits is not None:
            if limits.general is not None:
                self._windows[RateBucket.GENERAL] = SlidingWindow(limits.general)
            if limits.history is not None:
                self._windows[RateBucket.HISTORY] = SlidingWindow(limits.history)

    def reserve(self, bucket: RateBucket) -> float:
        """Take a slot in ``bucket`` (returning ``0``) or return the wait in seconds."""
        window = self._windows.get(bucket)
        if window is None:
            return 0.0
        return window.reserve(self.clock())
