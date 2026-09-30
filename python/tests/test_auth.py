"""Token lifecycle: JWT expiry, the ensure_token table and single-flight refresh."""

from __future__ import annotations

import asyncio
import inspect
import pickle
import threading
import time
from typing import Any

import httpx
import pytest

from conftest import API_KEY, USERNAME, MockAPI, bearer, envelope, make_jwt
from topstepx import AsyncClient, Client
from topstepx._auth import (
    Credentials,
    Session,
    TokenAction,
    make_credentials,
    new_session,
    plan_token,
    token_expiry,
)

BASE = "https://gateway.test"


def test_token_expiry_reads_the_exp_claim() -> None:
    assert token_expiry(make_jwt(1_900_000_000)) == 1_900_000_000.0


@pytest.mark.parametrize(
    "token",
    [
        "not-a-jwt",
        "a.b.c",  # payload is not base64 JSON
        make_jwt(None),  # no exp
        "x." + "eyJleHAiOiJzb29uIn0" + ".y",  # exp is a string
        "x." + "WzEsMl0" + ".y",  # payload is an array
        "",
    ],
)
def test_token_expiry_is_none_for_undecodable_tokens(token: str) -> None:
    assert token_expiry(token) is None


def test_new_session_falls_back_to_24_hours() -> None:
    session = new_session("opaque", now=1000.0, generation=1)
    assert session.expires_at == 1000.0 + 24 * 3600
    jwt_session = new_session(make_jwt(5000), now=1000.0, generation=2)
    assert jwt_session.expires_at == 5000.0


def test_plan_token_table() -> None:
    now, margin = 10_000.0, 3600.0
    valid = Session("t", 0, now + 7200, 1)
    in_margin = Session("t", 0, now + 1800, 1)
    expired = Session("t", 0, now, 1)  # now >= exp counts as expired
    assert plan_token(None, now, margin, True) is TokenAction.LOGIN
    assert plan_token(None, now, margin, False) is TokenAction.FAIL
    assert plan_token(expired, now, margin, True) is TokenAction.LOGIN
    assert plan_token(expired, now, margin, False) is TokenAction.USE
    assert plan_token(in_margin, now, margin, True) is TokenAction.VALIDATE
    assert plan_token(in_margin, now, margin, False) is TokenAction.VALIDATE
    assert plan_token(valid, now, margin, True) is TokenAction.USE


def test_plan_token_respects_the_renewal_cooldown() -> None:
    now, margin = 10_000.0, 3600.0
    blocked = Session("t", 0, now + 600, 1, renewal_blocked_until=now + 60)
    assert plan_token(blocked, now, margin, False) is TokenAction.USE
    assert plan_token(blocked, now + 59.9, margin, False) is TokenAction.USE
    assert plan_token(blocked, now + 60, margin, False) is TokenAction.VALIDATE
    assert plan_token(blocked, now + 600, margin, False) is TokenAction.USE  # expired: step 2
    assert plan_token(blocked, now + 600, margin, True) is TokenAction.LOGIN
    assert new_session("fresh", now, 2).renewal_blocked_until is None


def test_secrets_are_redacted_from_reprs() -> None:
    credentials = Credentials("user", "super-secret-key")
    session = Session("secret-token-value", 1.0, 2.0, 3)
    assert "super-secret-key" not in repr(credentials)
    assert "secret-token-value" not in repr(session)
    assert "user" in repr(credentials)


def test_make_credentials_requires_both_parts() -> None:
    assert make_credentials(None, None) is None
    with pytest.raises(ValueError, match="together"):
        make_credentials("user", None)
    with pytest.raises(ValueError, match="together"):
        make_credentials(None, "key")


def test_credentials_survive_pickling_without_leaking() -> None:
    credentials = pickle.loads(pickle.dumps(Credentials("u", "k")))
    assert credentials == Credentials("u", "k")


# ---------------------------------------------------------------------------
# Single-flight
# ---------------------------------------------------------------------------


async def test_concurrent_async_callers_share_one_login() -> None:
    api = MockAPI()
    logins = 0

    async def handler(request: httpx.Request) -> httpx.Response:
        nonlocal logins
        api.requests.append(request)
        if request.url.path == "/api/Auth/loginKey":
            logins += 1
            await asyncio.sleep(0.05)
            return httpx.Response(200, json=envelope(token=api.issue_token()))
        return httpx.Response(200, json=envelope(accounts=[]))

    async with AsyncClient(
        username=USERNAME,
        api_key=API_KEY,
        base_url=BASE,
        transport=httpx.MockTransport(handler),
    ) as client:
        await asyncio.gather(*(client.accounts.search() for _ in range(10)))
    assert logins == 1
    searches = [r for r in api.requests if r.url.path == "/api/Account/search"]
    assert len(searches) == 10
    assert {bearer(r) for r in searches} == {api.issued[0]}


async def test_concurrent_async_callers_share_one_refresh() -> None:
    validations = 0
    new_token = make_jwt(time.time() + 86400)

    async def handler(request: httpx.Request) -> httpx.Response:
        nonlocal validations
        if request.url.path == "/api/Auth/validate":
            validations += 1
            await asyncio.sleep(0.05)
            return httpx.Response(200, json=envelope(newToken=new_token))
        return httpx.Response(200, json=envelope(accounts=[]))

    old = make_jwt(time.time() + 600)
    async with AsyncClient(
        token=old, base_url=BASE, transport=httpx.MockTransport(handler)
    ) as client:
        await asyncio.gather(*(client.accounts.search() for _ in range(8)))
        assert client.token == new_token
    assert validations == 1


async def test_concurrent_callers_do_not_stampede_a_failing_refresh() -> None:
    validations = 0
    old = make_jwt(time.time() + 600)

    async def handler(request: httpx.Request) -> httpx.Response:
        nonlocal validations
        if request.url.path == "/api/Auth/validate":
            validations += 1
            await asyncio.sleep(0.05)
            return httpx.Response(503)
        return httpx.Response(200, json=envelope(accounts=[]))

    async with AsyncClient(
        token=old, base_url=BASE, transport=httpx.MockTransport(handler)
    ) as client:
        await asyncio.gather(*(client.accounts.search() for _ in range(5)))
        assert client.token == old
    assert validations == 1  # waiters reuse the outcome instead of refreshing again


def test_concurrent_threads_share_one_login() -> None:
    api = MockAPI()
    lock = threading.Lock()
    logins = 0

    def handler(request: httpx.Request) -> httpx.Response:
        nonlocal logins
        with lock:
            api.requests.append(request)
        if request.url.path == "/api/Auth/loginKey":
            with lock:
                logins += 1
                token = api.issue_token()
            time.sleep(0.1)
            return httpx.Response(200, json=envelope(token=token))
        return httpx.Response(200, json=envelope(accounts=[]))

    with Client(
        username=USERNAME,
        api_key=API_KEY,
        base_url=BASE,
        transport=httpx.MockTransport(handler),
    ) as client:
        barrier = threading.Barrier(8)
        errors: list[BaseException] = []

        def worker() -> None:
            barrier.wait()
            try:
                client.accounts.search()
            except BaseException as exc:  # pragma: no cover - reported below
                errors.append(exc)

        threads = [threading.Thread(target=worker) for _ in range(8)]
        for thread in threads:
            thread.start()
        for thread in threads:
            thread.join(timeout=10)
    assert errors == []
    assert logins == 1
    searches = [r for r in api.requests if r.url.path == "/api/Account/search"]
    assert {bearer(r) for r in searches} == {api.issued[0]}


async def test_cancellation_releases_the_auth_lock() -> None:
    started = asyncio.Event()

    async def handler(request: httpx.Request) -> httpx.Response:
        if request.url.path == "/api/Auth/loginKey":
            started.set()
            await asyncio.sleep(10)
        return httpx.Response(200, json=envelope(token=make_jwt(time.time() + 86400)))

    async with AsyncClient(
        username=USERNAME,
        api_key=API_KEY,
        base_url=BASE,
        transport=httpx.MockTransport(handler),
    ) as client:
        task = asyncio.create_task(client.get_token())
        await started.wait()
        task.cancel()
        with pytest.raises(asyncio.CancelledError):
            await task
        assert not client._auth_lock.locked()


GROUPS = ("auth", "accounts", "contracts", "history", "orders", "positions", "trades", "status")


def test_sync_and_async_clients_expose_identical_documented_groups() -> None:
    client = Client(token="t", base_url=BASE)
    async_client = AsyncClient(token="t", base_url=BASE)
    try:
        for group in GROUPS:
            sync_group: Any = getattr(client, group)
            async_group: Any = getattr(async_client, group)
            sync_names = {n for n in dir(sync_group) if not n.startswith("_")}
            async_names = {n for n in dir(async_group) if not n.startswith("_")}
            assert sync_names == async_names, group
            for name in sync_names:
                assert getattr(sync_group, name).__doc__, (group, name)
                assert inspect.iscoroutinefunction(getattr(async_group, name)), (group, name)
    finally:
        client.close()
        asyncio.run(async_client.aclose())
