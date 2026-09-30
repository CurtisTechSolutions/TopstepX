"""Shared test helpers: JWTs, a programmable mock REST API, and a harness that
runs one test body against both the sync and the async client."""

from __future__ import annotations

import asyncio
import base64
import inspect
import itertools
import json
import time
from collections.abc import Callable, Iterator
from typing import Any

import httpx
import pytest

from topstepx import AsyncClient, Client

BASE_URL = "https://gateway.test"
USERNAME = "test-user"
API_KEY = "test-api-key-SECRET"


def _b64(data: dict[str, Any]) -> str:
    raw = json.dumps(data, separators=(",", ":")).encode()
    return base64.urlsafe_b64encode(raw).rstrip(b"=").decode()


def make_jwt(exp: float | None, **claims: Any) -> str:
    """An unsigned JWT-shaped token with the given ``exp`` (epoch seconds)."""
    payload: dict[str, Any] = {"sub": "tester", **claims}
    if exp is not None:
        payload["exp"] = int(exp)
    return f"{_b64({'alg': 'HS256', 'typ': 'JWT'})}.{_b64(payload)}.signature"


def envelope(**fields: Any) -> dict[str, Any]:
    """A successful response envelope with extra fields."""
    return {
        "success": True,
        "errorCode": 0,
        "errorMessage": None,
        "hasBusinessFailure": False,
        **fields,
    }


def failure(code: int, message: str | None = None, **fields: Any) -> dict[str, Any]:
    """A business-failure envelope."""
    return {
        "success": False,
        "errorCode": code,
        "errorMessage": message,
        "hasBusinessFailure": True,
        **fields,
    }


Responder = Callable[[httpx.Request], httpx.Response] | httpx.Response | dict[str, Any]


class MockAPI:
    """A programmable fake of the REST API for ``httpx.MockTransport``.

    ``/api/Auth/loginKey`` issues a fresh 24 h JWT per call by default. Other
    paths answer from queues registered with :meth:`on`; the last queued answer
    repeats. Unregistered paths answer ``envelope()``.
    """

    def __init__(self) -> None:
        self.requests: list[httpx.Request] = []
        self.routes: dict[str, list[Responder]] = {}
        self.issued: list[str] = []
        self._serial = itertools.count(1)

    def issue_token(self, lifetime: float = 24 * 3600) -> str:
        token = make_jwt(time.time() + lifetime, jti=f"t{next(self._serial)}")
        self.issued.append(token)
        return token

    def on(self, path: str, *responders: Responder) -> None:
        self.routes[path] = list(responders)

    def calls(self, path: str) -> list[httpx.Request]:
        return [r for r in self.requests if r.url.path == path]

    def paths(self) -> list[str]:
        return [r.url.path for r in self.requests]

    def handler(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(request)
        path = request.url.path
        queue = self.routes.get(path)
        if queue:
            responder = queue.pop(0) if len(queue) > 1 else queue[0]
        elif path == "/api/Auth/loginKey":
            responder = envelope(token=self.issue_token())
        else:
            responder = envelope()
        if callable(responder):
            return responder(request)
        if isinstance(responder, httpx.Response):
            return responder
        return httpx.Response(200, json=responder)


def bearer(request: httpx.Request) -> str | None:
    value = request.headers.get("Authorization")
    return value.removeprefix("Bearer ") if value else None


def body(request: httpx.Request) -> Any:
    return json.loads(request.content) if request.content else None


class Harness:
    """Runs a test body against a sync or async client with a mock transport.

    Sleeps are recorded instead of performed and advance a fake monotonic clock
    used by the rate limiter.
    """

    def __init__(self, mode: str, api: MockAPI, **kwargs: Any) -> None:
        self.mode = mode
        self.api = api
        self.sleeps: list[float] = []
        self.now = 1000.0
        transport = httpx.MockTransport(api.handler)
        kwargs.setdefault("base_url", BASE_URL)
        self.loop: asyncio.AbstractEventLoop | None = None
        if mode == "sync":
            sync_client = Client(transport=transport, **kwargs)
            sync_client._sleep = self._record_sleep
            self.client: Any = sync_client
        else:
            self.loop = asyncio.new_event_loop()
            async_client = AsyncClient(transport=transport, **kwargs)

            async def fake_sleep(seconds: float) -> None:
                self._record_sleep(seconds)

            async_client._sleep = fake_sleep
            self.client = async_client
        self.client._core.limiter.clock = lambda: self.now

    def _record_sleep(self, seconds: float) -> None:
        self.sleeps.append(seconds)
        self.now += seconds

    def run(self, fn: Callable[[Any], Any]) -> Any:
        result = fn(self.client)
        if inspect.isawaitable(result):
            assert self.loop is not None
            return self.loop.run_until_complete(result)
        return result

    def close(self) -> None:
        if self.loop is not None:
            self.loop.run_until_complete(self.client.aclose())
            self.loop.close()
        else:
            self.client.close()


@pytest.fixture
def api() -> MockAPI:
    return MockAPI()


@pytest.fixture(params=["sync", "async"])
def mode(request: pytest.FixtureRequest) -> str:
    return str(request.param)


@pytest.fixture
def make(mode: str, api: MockAPI) -> Iterator[Callable[..., Harness]]:
    """Factory of harnesses for the parametrized client mode."""
    created: list[Harness] = []

    def factory(**kwargs: Any) -> Harness:
        harness = Harness(mode, api, **kwargs)
        created.append(harness)
        return harness

    yield factory
    for harness in created:
        harness.close()


@pytest.fixture
def creds() -> dict[str, str]:
    return {"username": USERNAME, "api_key": API_KEY}
