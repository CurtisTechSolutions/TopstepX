"""Hub connections against a local WebSocket server that speaks SignalR JSON
(SDK_BEHAVIOR.md sections 11 and 12.4)."""

from __future__ import annotations

import asyncio
import dataclasses
import logging
import time
from collections.abc import AsyncIterator
from typing import Any

import httpx
import pytest

from conftest import MockAPI, envelope, make_jwt
from fake_signalr import RS, FakeSignalR, wait_until
from topstepx import AsyncClient, Client, __version__
from topstepx.errors import (
    ConnectionLostError,
    HandshakeError,
    HubConnectError,
    HubError,
    HubTimeoutError,
    InvocationError,
    NotConnectedError,
    ServerCloseError,
    TokenRejectedError,
)
from topstepx.realtime import (
    Closed,
    Connected,
    DepthEvent,
    HubEvent,
    HubOptions,
    HubState,
    MarketHub,
    MarketTradeEvent,
    QuoteEvent,
    Reconnected,
    Reconnecting,
    ReconnectPolicy,
    SubscriptionReplayFailed,
    UnknownEvent,
    UserAccountEvent,
    UserHub,
    UserOrderEvent,
)

FAST = HubOptions(
    reconnect=ReconnectPolicy(delays=(0.0, 0.02), repeat_delay=0.05),
    keepalive_interval=0.5,
    server_timeout=2.0,
    handshake_timeout=1.0,
    invocation_timeout=1.0,
    open_timeout=2.0,
    close_timeout=0.5,
    proxy=None,
)


class Tokens:
    """A token provider that returns token-1, token-2, ..."""

    def __init__(self) -> None:
        self.count = 0

    async def __call__(self) -> str:
        self.count += 1
        return f"token-{self.count}"


class Events:
    """Consumes a hub's event stream in the background."""

    def __init__(self, hub: UserHub | MarketHub) -> None:
        self.items: list[HubEvent] = []
        self._task = asyncio.create_task(self._consume(hub.events()))

    async def _consume(self, stream: AsyncIterator[HubEvent]) -> None:
        async for event in stream:
            self.items.append(event)

    def of(self, kind: type[Any]) -> list[Any]:
        return [e for e in self.items if isinstance(e, kind)]

    async def wait_for(self, kind: type[Any], count: int = 1, timeout: float = 3.0) -> list[Any]:
        await wait_until(lambda: len(self.of(kind)) >= count, timeout)
        return self.of(kind)

    async def finished(self, timeout: float = 3.0) -> None:
        await asyncio.wait_for(self._task, timeout)


@pytest.fixture
async def server() -> AsyncIterator[FakeSignalR]:
    async with FakeSignalR() as fake:
        yield fake


def user_hub(server: FakeSignalR, **options: Any) -> UserHub:
    return UserHub(Tokens(), url=server.url, options=dataclasses.replace(FAST, **options))


# ---------------------------------------------------------------------------
# Connect and handshake
# ---------------------------------------------------------------------------


async def test_handshake_and_connection_details(server: FakeSignalR) -> None:
    hub = user_hub(server)
    assert hub.state is HubState.DISCONNECTED
    async with hub:
        assert hub.state is HubState.CONNECTED
        conn = server.conns[0]
        assert conn.path == "/hubs/user"
        assert conn.token == "token-1"
        assert conn.headers["Authorization"] == "Bearer token-1"
        assert conn.headers["User-Agent"] == f"topstepx-python/{__version__}"
        assert conn.handshake == {"protocol": "json", "version": 1}
        stream = hub.events()
        first = await asyncio.wait_for(anext(stream), 1)
        assert first == Connected()
        await stream.aclose()
    assert hub.state is HubState.CLOSED


async def test_handshake_error_fails_start(server: FakeSignalR) -> None:
    server.handshake_reply = '{"error":"Requested protocol \'json\' is not available."}'
    hub = user_hub(server)
    with pytest.raises(HandshakeError, match="not available"):
        await hub.start()
    assert hub.state is HubState.DISCONNECTED
    await hub.close()


async def test_handshake_timeout(server: FakeSignalR) -> None:
    server.handshake_reply = None
    hub = user_hub(server, handshake_timeout=0.2)
    started = time.monotonic()
    with pytest.raises(HandshakeError, match="no handshake response"):
        await hub.start()
    assert time.monotonic() - started < 2
    await hub.close()


async def test_upgrade_rejection_carries_the_status(server: FakeSignalR) -> None:
    server.reject_status = 401
    hub = user_hub(server)
    with pytest.raises(HubConnectError) as info:
        await hub.start()
    assert info.value.status_code == 401
    assert "token-1" not in str(info.value)
    await hub.close()


async def test_connection_refused(server: FakeSignalR) -> None:
    url = server.url
    await server.stop()
    hub = UserHub(Tokens(), url=url, options=FAST)
    with pytest.raises(HubConnectError, match="could not connect"):
        await hub.start()
    await hub.close()


async def test_messages_sharing_the_handshake_frame_are_processed(server: FakeSignalR) -> None:
    server.handshake_extra = (
        '{"type":1,"target":"GatewayUserAccount","arguments":[{"id":7,"name":"A"}]}' + RS
    )
    async with user_hub(server) as hub:
        events = Events(hub)
        (account,) = await events.wait_for(UserAccountEvent)
        assert account.account.id == 7
        assert isinstance(events.items[0], Connected)


# ---------------------------------------------------------------------------
# Invocations
# ---------------------------------------------------------------------------


async def test_invoke_success_and_error_completions(server: FakeSignalR) -> None:
    server.rules["Echo"] = lambda args: ({"echo": args}, None)
    server.rules["Fail"] = lambda args: (None, "Unauthorized subscription")
    async with user_hub(server) as hub:
        assert await hub.invoke("Echo", 1, "two") == {"echo": [1, "two"]}
        assert await hub.invoke("Void") is None
        with pytest.raises(InvocationError) as info:
            await hub.invoke("Fail", 5)
        assert info.value.method == "Fail"
        assert info.value.error == "Unauthorized subscription"
        ids = [m["invocationId"] for m in server.conns[0].invocations]
        assert ids == ["1", "2", "3"]


async def test_invocation_timeout(server: FakeSignalR) -> None:
    server.rules["Hang"] = lambda args: None
    async with user_hub(server, invocation_timeout=0.1) as hub:
        with pytest.raises(HubTimeoutError):
            await hub.invoke("Hang")
        assert isinstance(HubTimeoutError("x"), TimeoutError)
        assert await hub.invoke("Other") is None  # the connection is still usable


async def test_send_does_not_wait_for_a_completion(server: FakeSignalR) -> None:
    async with user_hub(server) as hub:
        await hub.send("Notify", 1)
        await wait_until(lambda: len(server.conns[0].invocations) == 1)
        assert "invocationId" not in server.conns[0].invocations[0]


async def test_invocations_fail_fast_when_not_connected(server: FakeSignalR) -> None:
    hub = user_hub(server)
    with pytest.raises(NotConnectedError):
        await hub.invoke("SubscribeAccounts")
    with pytest.raises(NotConnectedError):
        await hub.subscribe_accounts()
    await hub.close()
    with pytest.raises(NotConnectedError):
        await hub.send("X")


async def test_pending_invocations_fail_when_the_connection_drops(server: FakeSignalR) -> None:
    server.rules["Hang"] = lambda args: None
    async with user_hub(server) as hub:
        pending = asyncio.create_task(hub.invoke("Hang"))
        await wait_until(lambda: len(server.conns[0].invocations) == 1)
        server.drop()
        with pytest.raises(ConnectionLostError):
            await pending


# ---------------------------------------------------------------------------
# Events
# ---------------------------------------------------------------------------


async def test_user_event_forms_are_dispatched(server: FakeSignalR) -> None:
    order = {"id": 1, "accountId": 2, "status": 1, "limitPrice": 6000.25}
    async with user_hub(server) as hub:
        events = Events(hub)
        await server.push("GatewayUserOrder", order)
        await server.push("GatewayUserOrder", {"action": 1, "data": {**order, "id": 2}})
        await server.push(
            "GatewayUserOrder", [{**order, "id": 3}, {"action": 0, "data": {**order, "id": 4}}]
        )
        received = await events.wait_for(UserOrderEvent, 4)
        assert [(e.order.id, e.action) for e in received] == [(1, None), (2, 1), (3, None), (4, 0)]
        assert str(received[0].order.limit_price) == "6000.25"


async def test_market_events_are_dispatched(server: FakeSignalR) -> None:
    async with MarketHub(Tokens(), url=server.url, options=FAST) as hub:
        events = Events(hub)
        await server.push("GatewayQuote", "CON.F.US.EP.U25", {"lastPrice": 6000.25})
        await server.push("GatewayTrade", "CON.F.US.EP.U25", {"price": 6000, "volume": 1})
        await server.push("GatewayTrade", "CON.F.US.EP.U25", [{"price": 6000}, {"price": 6000.25}])
        await server.push("GatewayDepth", "CON.F.US.EP.U25", [{"price": 5999.75, "volume": "4"}])
        (quote,) = await events.wait_for(QuoteEvent)
        trades = await events.wait_for(MarketTradeEvent, 2)
        (depth,) = await events.wait_for(DepthEvent)
        assert quote.contract_id == "CON.F.US.EP.U25"
        assert [len(t.trades) for t in trades] == [1, 2]
        assert depth.entries[0].volume == 4


async def test_unknown_targets_and_bad_payloads_do_not_break_the_connection(
    server: FakeSignalR,
) -> None:
    async with user_hub(server) as hub:
        events = Events(hub)
        await server.push("GatewaySomethingNew", {"x": 1})
        await server.push("GatewayUserOrder", {"id": "not-a-number"})
        await server.send_raw("this is not json" + RS)
        await server.push("GatewayUserAccount", {"id": 9})
        unknown = await events.wait_for(UnknownEvent, 2)
        assert unknown[0].target == "GatewaySomethingNew"
        assert unknown[0].error is None
        assert unknown[0].arguments == ({"x": 1},)
        assert unknown[1].target == "GatewayUserOrder"
        assert unknown[1].error is not None
        (account,) = await events.wait_for(UserAccountEvent)
        assert account.account.id == 9
        assert hub.state is HubState.CONNECTED


async def test_split_and_binary_frames(server: FakeSignalR) -> None:
    async with user_hub(server) as hub:
        events = Events(hub)
        message = (
            '{"type":1,"target":"GatewayUserAccount","arguments":[{"id":1,"name":"café"}]}' + RS
        )
        await server.send_raw(message[:20])
        await server.send_raw(message[20:])
        both = message.replace('"id":1', '"id":2') + message.replace('"id":1', '"id":3')
        await server.send_raw(both.encode())
        received = await events.wait_for(UserAccountEvent, 3)
        assert [e.account.id for e in received] == [1, 2, 3]
        assert received[0].account.name == "café"


async def test_callbacks(server: FakeSignalR, caplog: pytest.LogCaptureFixture) -> None:
    seen: list[str] = []

    def on_order(event: UserOrderEvent) -> None:
        seen.append(f"order:{event.order.id}")

    async def on_any(event: HubEvent) -> None:
        await asyncio.sleep(0)
        seen.append(type(event).__name__)

    def broken(event: HubEvent) -> None:
        raise RuntimeError("callback bug")

    hub = user_hub(server)
    hub.on(UserOrderEvent, on_order)
    hub.on(HubEvent, on_any)
    remove = hub.on(UserOrderEvent, broken)
    async with hub:
        await server.push("GatewayUserOrder", {"id": 5})
        await wait_until(lambda: "order:5" in seen and "UserOrderEvent" in seen)
        remove()
        await server.push("GatewayUserOrder", {"id": 6})
        await wait_until(lambda: "order:6" in seen)
    await wait_until(lambda: "Closed" in seen)
    assert seen[0] == "Connected"
    assert (
        sum("callback bug" in (r.exc_text or "") or "callback" in r.message for r in caplog.records)
        == 1
    )


async def test_backpressure_drops_the_oldest_events(server: FakeSignalR) -> None:
    async with user_hub(server, event_buffer_size=5) as hub:
        for number in range(1, 21):
            await server.push("GatewayUserAccount", {"id": number})
        # 21 events (Connected + 20) went through a buffer of 5
        await wait_until(lambda: hub.dropped_events == 16)
        assert await hub.invoke("StillResponsive") is None  # the read loop never blocked
        stream = hub.events()
        remaining = [await asyncio.wait_for(anext(stream), 1) for _ in range(5)]
        assert [e.account.id for e in remaining] == [16, 17, 18, 19, 20]  # type: ignore[attr-defined]
        await stream.aclose()


# ---------------------------------------------------------------------------
# Keepalive, reconnect and replay
# ---------------------------------------------------------------------------


async def test_keepalive_pings(server: FakeSignalR) -> None:
    async with user_hub(server, keepalive_interval=0.05):
        await wait_until(lambda: server.conns[0].pings >= 3, timeout=2)


async def test_keepalive_counts_from_the_last_message_sent(server: FakeSignalR) -> None:
    async with user_hub(server, keepalive_interval=0.3) as hub:
        for _ in range(5):
            await hub.invoke("Busy")
            await asyncio.sleep(0.05)
        assert server.conns[0].pings == 0


async def test_reconnect_uses_a_fresh_token_and_replays_subscriptions(server: FakeSignalR) -> None:
    async with user_hub(server) as hub:
        events = Events(hub)
        await hub.subscribe_accounts()
        await hub.subscribe_orders(1)
        await hub.subscribe_positions(1)
        await hub.subscribe_trades(2)
        await hub.subscribe_orders(1)  # duplicate: recorded once
        await hub.unsubscribe_positions(1)
        assert hub.subscriptions == [
            ("SubscribeAccounts", ()),
            ("SubscribeOrders", (1,)),
            ("SubscribeTrades", (2,)),
        ]

        server.drop()
        (reconnected,) = await events.wait_for(Reconnected)
        assert reconnected.attempt == 1
        (reconnecting,) = events.of(Reconnecting)
        assert reconnecting.attempt == 1
        assert isinstance(reconnecting.cause, ConnectionLostError)

        assert len(server.conns) == 2
        second = server.conns[1]
        assert second.token == "token-2"
        assert second.headers["Authorization"] == "Bearer token-2"
        assert second.targets() == [
            ("SubscribeAccounts", []),
            ("SubscribeOrders", [1]),
            ("SubscribeTrades", [2]),
        ]
        # replay happens before Reconnected is emitted, which precedes new data
        state_events = [e for e in events.items if not isinstance(e, UserOrderEvent)]
        assert [type(e) for e in state_events] == [Connected, Reconnecting, Reconnected]
        assert hub.state is HubState.CONNECTED
        await server.push("GatewayUserOrder", {"id": 11})
        (order,) = await events.wait_for(UserOrderEvent)
        assert order.order.id == 11


async def test_failed_replay_is_surfaced_and_kept(server: FakeSignalR) -> None:
    async with user_hub(server) as hub:
        events = Events(hub)
        await hub.subscribe_orders(1)
        await hub.subscribe_accounts()
        server.rules["SubscribeOrders"] = lambda args: (None, "account not found")
        server.drop()
        (failed,) = await events.wait_for(SubscriptionReplayFailed)
        await events.wait_for(Reconnected)
        assert failed.method == "SubscribeOrders"
        assert failed.arguments == (1,)
        assert isinstance(failed.error, InvocationError)
        assert server.conns[1].targets() == [("SubscribeOrders", [1]), ("SubscribeAccounts", [])]
        assert ("SubscribeOrders", (1,)) in hub.subscriptions


async def test_server_timeout_counts_as_connection_loss(server: FakeSignalR) -> None:
    server.ping_interval = None  # the server goes silent after the handshake
    async with user_hub(server, server_timeout=0.2, keepalive_interval=0.05) as hub:
        events = Events(hub)
        (reconnecting,) = (await events.wait_for(Reconnecting))[:1]
        assert "no message from the server" in str(reconnecting.cause)
        await events.wait_for(Reconnected)
        assert len(server.conns) >= 2


async def test_close_with_allow_reconnect_reconnects(server: FakeSignalR) -> None:
    async with user_hub(server) as hub:
        events = Events(hub)
        await server.close_from_server("server restarting", allow_reconnect=True)
        await events.wait_for(Reconnected)
        (reconnecting,) = events.of(Reconnecting)
        assert isinstance(reconnecting.cause, ServerCloseError)
        assert reconnecting.cause.allow_reconnect is True


async def test_close_without_allow_reconnect_ends_the_hub(server: FakeSignalR) -> None:
    hub = user_hub(server)
    await hub.start()
    events = Events(hub)
    await server.close_from_server("session revoked")
    await events.finished()
    closed = events.items[-1]
    assert isinstance(closed, Closed)
    assert isinstance(closed.cause, ServerCloseError)
    assert closed.cause.error == "session revoked"
    assert events.of(Reconnecting) == []
    assert hub.state is HubState.CLOSED
    assert len(server.conns) == 1
    await hub.close()


async def test_reconnect_disabled(server: FakeSignalR) -> None:
    hub = user_hub(server, reconnect=ReconnectPolicy.disabled())
    await hub.start()
    events = Events(hub)
    server.drop()
    await events.finished()
    assert isinstance(events.items[-1], Closed)
    assert isinstance(events.items[-1].cause, ConnectionLostError)
    assert events.of(Reconnecting) == []
    await hub.close()


async def test_reconnect_gives_up_after_max_attempts(server: FakeSignalR) -> None:
    policy = ReconnectPolicy(delays=(0.0,), repeat_delay=0.01, max_attempts=3)
    hub = user_hub(server, reconnect=policy)
    await hub.start()
    events = Events(hub)
    await server.stop()  # nothing accepts new connections any more
    await events.finished(timeout=5)
    assert [e.attempt for e in events.of(Reconnecting)] == [1, 2, 3]
    closed = events.items[-1]
    assert isinstance(closed, Closed)
    assert isinstance(closed.cause, HubConnectError)
    await hub.close()


async def test_invocations_fail_fast_while_reconnecting(server: FakeSignalR) -> None:
    slow = ReconnectPolicy(delays=(0.3,), repeat_delay=0.3)
    async with user_hub(server, reconnect=slow) as hub:
        events = Events(hub)
        server.drop()
        await events.wait_for(Reconnecting)
        assert hub.state is HubState.RECONNECTING
        with pytest.raises(NotConnectedError):
            await hub.subscribe_orders(1)
        await events.wait_for(Reconnected)
        await hub.subscribe_orders(1)


# ---------------------------------------------------------------------------
# close()
# ---------------------------------------------------------------------------


async def test_close_sends_a_close_message_and_ends_the_stream(server: FakeSignalR) -> None:
    server.rules["Hang"] = lambda args: None
    hub = user_hub(server)
    await hub.start()
    events = Events(hub)
    pending = asyncio.create_task(hub.invoke("Hang"))
    await wait_until(lambda: len(server.conns[0].invocations) == 1)
    await hub.close()
    with pytest.raises(ConnectionLostError):
        await pending
    await events.finished()
    assert events.items[-1] == Closed(None)
    assert hub.state is HubState.CLOSED
    await wait_until(lambda: server.conns[0].closed.is_set())
    assert server.conns[0].client_close is True
    await hub.close()  # idempotent
    await hub.wait_closed()
    with pytest.raises(HubError):
        await hub.start()
    assert [e async for e in hub.events()] == []  # the stream is over


async def test_close_during_reconnect_stops_reconnecting(server: FakeSignalR) -> None:
    slow = ReconnectPolicy(delays=(5.0,))
    hub = user_hub(server, reconnect=slow)
    await hub.start()
    events = Events(hub)
    server.drop()
    await events.wait_for(Reconnecting)
    started = time.monotonic()
    await hub.close()
    assert time.monotonic() - started < 2
    await events.finished()
    assert events.items[-1] == Closed(None)
    assert len(server.conns) == 1


async def test_close_before_start(server: FakeSignalR) -> None:
    hub = user_hub(server)
    await hub.close()
    assert hub.state is HubState.CLOSED
    assert [e async for e in hub.events()] == [Closed(None)]


# ---------------------------------------------------------------------------
# Token sources and secrecy
# ---------------------------------------------------------------------------


def _login_transport() -> httpx.MockTransport:
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json=envelope(token=make_jwt(time.time() + 86400, jti="hub")))

    return httpx.MockTransport(handler)


async def test_async_client_as_token_source(server: FakeSignalR) -> None:
    async with (
        AsyncClient(
            username="u",
            api_key="k",
            base_url="https://gateway.test",
            user_hub_url=server.url,
            transport=_login_transport(),
        ) as client,
        UserHub(client, options=FAST) as hub,
    ):
        assert hub.url == server.url
        assert server.conns[0].token == client.token


async def test_sync_client_as_token_source(server: FakeSignalR) -> None:
    with Client(
        username="u",
        api_key="k",
        base_url="https://gateway.test",
        market_hub_url=server.url,
        transport=_login_transport(),
    ) as client:
        async with MarketHub(client, options=FAST):
            assert server.conns[0].token == client.token


@pytest.mark.parametrize("kind", ["string", "sync-callable", "async-callable"])
async def test_other_token_sources(server: FakeSignalR, kind: str) -> None:
    async def async_provider() -> str:
        return "async-token"

    source: Any = {
        "string": "fixed-token",
        "sync-callable": lambda: "sync-token",
        "async-callable": async_provider,
    }[kind]
    async with UserHub(source, url=server.url, options=FAST):
        assert server.conns[0].token in {"fixed-token", "sync-token", "async-token"}


async def test_bad_token_sources_are_rejected(server: FakeSignalR) -> None:
    with pytest.raises(TypeError):
        UserHub(42, url=server.url)  # type: ignore[arg-type]
    hub = UserHub(lambda: 42, url=server.url, options=FAST)  # type: ignore[arg-type,return-value]
    with pytest.raises(TypeError):
        await hub.start()
    await hub.close()
    with pytest.raises(ValueError, match="absolute"):
        UserHub("t", url="not a url")


# ---------------------------------------------------------------------------
# Rejected tokens (HTTP 401 on the WebSocket upgrade)
# ---------------------------------------------------------------------------


def _rest_client(kind: str, api: MockAPI, hub_url: str) -> Client | AsyncClient:
    cls = Client if kind == "sync" else AsyncClient
    return cls(
        username="u",
        api_key="k",
        base_url="https://gateway.test",
        user_hub_url=hub_url,
        transport=httpx.MockTransport(api.handler),
    )


async def _close_client(client: Client | AsyncClient) -> None:
    if isinstance(client, AsyncClient):
        await client.aclose()
    else:
        client.close()


@pytest.mark.parametrize("kind", ["sync", "async"])
async def test_rejected_token_on_initial_connect_then_retry_logs_in_again(
    server: FakeSignalR, kind: str
) -> None:
    api = MockAPI()
    client = _rest_client(kind, api, server.url)
    try:
        server.reject_queue = [401]
        hub = UserHub(client, options=FAST)
        with pytest.raises(TokenRejectedError) as info:
            await hub.start()
        first = api.issued[0]
        assert info.value.status_code == 401
        assert first not in str(info.value)
        assert server.rejected_tokens == [first]
        assert client.token is None  # the rejected token was discarded
        assert hub.state is HubState.DISCONNECTED

        await hub.start()  # the retry logs in again instead of reusing the token
        assert len(api.issued) == 2
        assert server.conns[0].token == api.issued[1] != first
        await hub.close()
    finally:
        await _close_client(client)


@pytest.mark.parametrize("kind", ["sync", "async"])
async def test_rejected_token_on_reconnect_logs_in_again(server: FakeSignalR, kind: str) -> None:
    api = MockAPI()
    client = _rest_client(kind, api, server.url)
    try:
        async with UserHub(client, options=FAST) as hub:
            events = Events(hub)
            await hub.subscribe_orders(1)
            first = api.issued[0]
            assert server.conns[0].token == first

            server.reject_queue = [401]
            server.drop()
            (reconnected,) = await events.wait_for(Reconnected)
            attempts = events.of(Reconnecting)
            assert [e.attempt for e in attempts] == [1, 2]
            assert isinstance(attempts[1].cause, TokenRejectedError)
            assert reconnected.attempt == 2
            assert server.rejected_tokens == [first]  # attempt 1 reused the stored token
            assert len(api.issued) == 2
            assert server.conns[1].token == api.issued[1] != first
            assert server.conns[1].targets() == [("SubscribeOrders", [1])]
    finally:
        await _close_client(client)


async def test_rejected_token_is_reported_to_a_custom_provider(server: FakeSignalR) -> None:
    class Provider:
        def __init__(self) -> None:
            self.rejected: list[str] = []
            self.serial = 0

        async def __call__(self) -> str:
            self.serial += 1
            return f"custom-{self.serial}"

        async def invalidate_token(self, token: str) -> None:
            self.rejected.append(token)

    provider = Provider()
    server.reject_queue = [401]
    hub = UserHub(provider, url=server.url, options=FAST)
    with pytest.raises(TokenRejectedError):
        await hub.start()
    assert provider.rejected == ["custom-1"]
    await hub.start()
    assert server.conns[0].token == "custom-2"
    await hub.close()


async def test_other_upgrade_failures_do_not_discard_the_token(server: FakeSignalR) -> None:
    api = MockAPI()
    client = _rest_client("async", api, server.url)
    try:
        server.reject_queue = [403]
        hub = UserHub(client, options=FAST)
        with pytest.raises(HubConnectError) as info:
            await hub.start()
        assert not isinstance(info.value, TokenRejectedError)
        assert info.value.status_code == 403
        assert client.token == api.issued[0]
        await hub.close()
    finally:
        await _close_client(client)


async def test_tokens_never_reach_logs_or_reprs(
    server: FakeSignalR, caplog: pytest.LogCaptureFixture
) -> None:
    caplog.set_level(logging.DEBUG)
    secret = "SECRET-TOKEN-VALUE"
    hub = UserHub(secret, url=server.url, options=FAST)
    async with hub:
        await hub.subscribe_accounts()
        assert secret not in repr(hub)
        server.drop()
        await wait_until(lambda: len(server.conns) == 2 and hub.state is HubState.CONNECTED)
    sdk_records = [
        r for r in caplog.records if r.name.startswith("topstepx") or r.name == "websockets.client"
    ]
    assert sdk_records  # the SDK did log (connect, reconnect, close)
    for record in sdk_records:
        assert secret not in record.getMessage()
