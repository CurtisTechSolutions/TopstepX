"""SignalR framing, messages and lenient event decoding (no network)."""

from __future__ import annotations

from datetime import datetime, timezone
from decimal import Decimal
from typing import Any
from urllib.parse import parse_qsl, urlsplit

import pytest

from topstepx import HandshakeError
from topstepx.enums import DomType, OrderSide, OrderStatus, PositionType, TradeLogType
from topstepx.errors import DecodeError
from topstepx.realtime import (
    DepthEvent,
    MarketTradeEvent,
    QuoteEvent,
    ReconnectPolicy,
    UnknownEvent,
    UserAccountEvent,
    UserOrderEvent,
    UserPositionEvent,
    UserTradeEvent,
    build_hub_url,
    decode_market_event,
    decode_user_event,
)
from topstepx.realtime.protocol import (
    HANDSHAKE_REQUEST,
    PING_MESSAGE,
    CloseMessage,
    CompletionMessage,
    FrameParser,
    InvocationMessage,
    OtherMessage,
    PingMessage,
    ProtocolError,
    encode_invocation,
    parse_handshake_response,
    parse_message,
)

RS = "\x1e"

# ---------------------------------------------------------------------------
# Framing and messages
# ---------------------------------------------------------------------------


def test_handshake_and_ping_encoding() -> None:
    assert HANDSHAKE_REQUEST == '{"protocol":"json","version":1}' + RS
    assert PING_MESSAGE == '{"type":6}' + RS
    assert encode_invocation("SubscribeOrders", [123], "7") == (
        '{"type":1,"invocationId":"7","target":"SubscribeOrders","arguments":[123]}' + RS
    )
    assert encode_invocation("Send", [], None) == '{"type":1,"target":"Send","arguments":[]}' + RS


def test_frame_parser_splits_and_buffers() -> None:
    parser = FrameParser()
    assert parser.feed('{"type":6}' + RS + '{"type":1,') == ['{"type":6}']
    assert parser.pending == '{"type":1,'
    assert parser.feed('"target":"x"}' + RS + RS) == ['{"type":1,"target":"x"}']
    assert parser.feed("") == []


def test_frame_parser_decodes_binary_frames_across_split_characters() -> None:
    parser = FrameParser()
    payload = ('{"type":1,"target":"café"}' + RS).encode()
    split = payload.index("é".encode()) + 1  # in the middle of the two-byte character
    assert parser.feed(payload[:split]) == []
    assert parser.feed(payload[split:]) == ['{"type":1,"target":"café"}']


def test_frame_parser_limits_unterminated_messages() -> None:
    parser = FrameParser(max_buffer=10)
    with pytest.raises(ProtocolError):
        parser.feed("x" * 11)
    with pytest.raises(ProtocolError):
        FrameParser().feed(b"\xff\xfe")


def test_parse_messages() -> None:
    invocation = parse_message(
        '{"type":1,"target":"GatewayQuote","arguments":["C",{"lastPrice":1.25}]}'
    )
    assert invocation == InvocationMessage("GatewayQuote", ("C", {"lastPrice": Decimal("1.25")}))
    assert parse_message('{"type":3,"invocationId":"1","result":42}') == CompletionMessage("1", 42)
    assert parse_message('{"type":3,"invocationId":"2","error":"bad"}') == CompletionMessage(
        "2", None, "bad"
    )
    assert parse_message('{"type":6}') == PingMessage()
    assert parse_message('{"type":7,"error":"bye","allowReconnect":true}') == CloseMessage(
        "bye", True
    )
    assert parse_message('{"type":7}') == CloseMessage(None, False)
    other = parse_message('{"type":2,"invocationId":"1","item":{}}')
    assert isinstance(other, OtherMessage)
    assert other.type == 2
    assert parse_message('{"type":1,"target":"t"}') == InvocationMessage("t", ())


@pytest.mark.parametrize(
    "text",
    [
        "not json",
        "[]",
        '{"no":"type"}',
        '{"type":"1"}',
        '{"type":1}',
        '{"type":1,"target":"x","arguments":{}}',
        '{"type":3}',
        '{"type":3,"invocationId":5}',
    ],
)
def test_malformed_messages_raise_protocol_errors(text: str) -> None:
    with pytest.raises(ProtocolError):
        parse_message(text)


def test_handshake_responses() -> None:
    parse_handshake_response("{}")
    parse_handshake_response('{"minorVersion":1}')
    with pytest.raises(HandshakeError, match="not supported"):
        parse_handshake_response('{"error":"Protocol not supported"}')
    with pytest.raises(HandshakeError):
        parse_handshake_response("nope")


def test_build_hub_url() -> None:
    assert build_hub_url("https://rtc.topstepx.com/hubs/user", "a.b-c_d") == (
        "wss://rtc.topstepx.com/hubs/user?access_token=a.b-c_d"
    )
    assert build_hub_url("http://localhost:5000/hubs/market?x=1&access_token=old", "t+/=") == (
        "ws://localhost:5000/hubs/market?x=1&access_token=t%2B%2F%3D"
    )


@pytest.mark.parametrize(
    ("configured", "expected_query"),
    [
        ("https://host/hubs/user?access_token=old&x=1", [("access_token", "new"), ("x", "1")]),
        ("https://host/hubs/user?access_token=", [("access_token", "new")]),  # guide style
        (
            "https://host/hubs/user?a=1&access_token=old&access_token=older&b=2",
            [("a", "1"), ("access_token", "new"), ("b", "2")],
        ),
        ("https://host/hubs/user?x=1", [("x", "1"), ("access_token", "new")]),
    ],
)
def test_build_hub_url_sets_exactly_one_access_token(
    configured: str, expected_query: list[tuple[str, str]]
) -> None:
    built = urlsplit(build_hub_url(configured, "new"))
    assert (built.scheme, built.netloc, built.path) == ("wss", "host", "/hubs/user")
    query = parse_qsl(built.query, keep_blank_values=True)
    assert query == expected_query
    assert [value for key, value in query if key == "access_token"] == ["new"]
    assert build_hub_url("wss://h/p", "t") == "wss://h/p?access_token=t"
    with pytest.raises(ValueError, match="absolute"):
        build_hub_url("ftp://h/p", "t")


def test_reconnect_policy_delays() -> None:
    policy = ReconnectPolicy()
    assert [policy.delay(n) for n in range(1, 9)] == [0, 1, 2, 5, 10, 30, 30, 30]
    limited = ReconnectPolicy(delays=(0.5,), repeat_delay=2, max_attempts=3)
    assert [limited.delay(n) for n in range(1, 5)] == [0.5, 2, 2, None]
    assert ReconnectPolicy.disabled().delay(1) is None
    with pytest.raises(ValueError, match=">= 0"):
        ReconnectPolicy(delays=(-1,))


# ---------------------------------------------------------------------------
# User hub payloads
# ---------------------------------------------------------------------------

ORDER = {
    "id": 456,
    "accountId": 123,
    "contractId": "CON.F.US.EP.U25",
    "symbolId": "F.US.EP",
    "creationTimestamp": "2025-07-01T14:00:00.1234567+00:00",
    "status": 2,
    "type": 1,
    "side": 1,
    "size": 1,
    "limitPrice": Decimal("6000.25"),
    "fillVolume": 1,
    "filledPrice": Decimal("6000.25"),
    "customTag": None,
}


def test_user_payload_bare_envelope_and_array() -> None:
    bare = decode_user_event("GatewayUserOrder", (ORDER,))
    wrapped = decode_user_event("GatewayUserOrder", ({"action": 1, "data": ORDER},))
    array = decode_user_event(
        "GatewayUserOrder", ([ORDER, {"action": 2, "data": {**ORDER, "id": 457}}],)
    )
    assert len(bare) == len(wrapped) == 1
    assert len(array) == 2
    for event in (*bare, *wrapped, *array):
        assert isinstance(event, UserOrderEvent)
    first = bare[0]
    assert isinstance(first, UserOrderEvent)
    assert first.action is None
    assert first.order.id == 456
    assert first.order.status is OrderStatus.Filled
    assert first.order.side is OrderSide.Ask
    assert first.order.limit_price == Decimal("6000.25")
    assert first.order.creation_timestamp == datetime(
        2025, 7, 1, 14, 0, 0, 123456, tzinfo=timezone.utc
    )
    assert first.raw == ORDER
    assert [e.action for e in wrapped] == [1]  # type: ignore[attr-defined]
    assert [(e.order.id, e.action) for e in array] == [(456, None), (457, 2)]  # type: ignore[attr-defined]


def test_user_envelope_with_array_data() -> None:
    events = decode_user_event(
        "GatewayUserPosition", ({"action": 0, "data": [{"id": 1}, {"id": 2}]},)
    )
    assert [e.position.id for e in events] == [1, 2]  # type: ignore[attr-defined]
    assert all(isinstance(e, UserPositionEvent) and e.action == 0 for e in events)


def test_every_user_target_decodes_partial_payloads() -> None:
    account = decode_user_event("GatewayUserAccount", ({"id": 1, "balance": 50000.5},))[0]
    assert isinstance(account, UserAccountEvent)
    assert account.account.balance == Decimal("50000.5")
    assert account.account.name is None
    position = decode_user_event("GatewayUserPosition", ({"id": 2, "type": 2, "size": "3"},))[0]
    assert isinstance(position, UserPositionEvent)
    assert position.position.type is PositionType.Short
    assert position.position.size == 3
    trade = decode_user_event(
        "GatewayUserTrade", ({"id": 3, "price": 6000, "profitAndLoss": None, "voided": False},)
    )[0]
    assert isinstance(trade, UserTradeEvent)
    assert trade.trade.profit_and_loss is None
    assert trade.trade.price == Decimal(6000)


def test_user_decode_failures_become_unknown_events() -> None:
    (missing_id,) = decode_user_event("GatewayUserOrder", ({"status": 1},))
    assert isinstance(missing_id, UnknownEvent)
    assert isinstance(missing_id.error, DecodeError)
    assert missing_id.target == "GatewayUserOrder"
    (not_object,) = decode_user_event("GatewayUserOrder", ("text",))
    assert isinstance(not_object, UnknownEvent)
    (no_args,) = decode_user_event("GatewayUserOrder", ())
    assert isinstance(no_args, UnknownEvent)
    mixed = decode_user_event("GatewayUserOrder", ([ORDER, {"id": "x"}],))
    assert isinstance(mixed[0], UserOrderEvent)
    assert isinstance(mixed[1], UnknownEvent)


def test_unknown_user_target() -> None:
    (event,) = decode_user_event("GatewayLogout", ({"reason": 1},))
    assert event == UnknownEvent("GatewayLogout", ({"reason": 1},), None)


# ---------------------------------------------------------------------------
# Market hub payloads
# ---------------------------------------------------------------------------

QUOTE = {
    "symbol": "F.US.EP",
    "symbolName": "/ES",
    "lastPrice": Decimal("6000.25"),
    "bestBid": Decimal("6000"),
    "bestAsk": Decimal("6000.25"),
    "change": Decimal("-5.5"),
    "changePercent": Decimal("-0.09"),
    "open": 6005,
    "high": 6010,
    "low": 5990,
    "volume": Decimal("125000.0"),
    "lastUpdated": "2025-07-01T14:00:00Z",
    "timestamp": "2025-07-01T14:00:00Z",
}


def test_quote_event() -> None:
    (event,) = decode_market_event("GatewayQuote", ("CON.F.US.EP.U25", QUOTE))
    assert isinstance(event, QuoteEvent)
    assert event.contract_id == "CON.F.US.EP.U25"
    assert event.quote.last_price == Decimal("6000.25")
    assert event.quote.volume == 125000
    (partial,) = decode_market_event("GatewayQuote", ("C", {"bestBid": 1}))
    assert isinstance(partial, QuoteEvent)
    assert partial.quote.best_bid == Decimal(1)
    assert partial.quote.last_price is None


def test_trade_and_depth_payloads_normalize_to_one_event() -> None:
    trade = {
        "symbolId": "F.US.EP",
        "price": 6000.25,
        "timestamp": "2025-07-01T14:00:00Z",
        "type": 1,
        "volume": "2",
    }
    (single,) = decode_market_event("GatewayTrade", ("C", trade))
    (many,) = decode_market_event("GatewayTrade", ("C", [trade, {**trade, "volume": 3.0}]))
    assert isinstance(single, MarketTradeEvent)
    assert isinstance(many, MarketTradeEvent)
    assert len(single.trades) == 1
    assert [t.volume for t in many.trades] == [2, 3]
    assert many.trades[0].type is TradeLogType.Sell

    depth = {
        "timestamp": "2025-07-01T14:00:00Z",
        "type": 9,
        "price": 6000,
        "volume": 10,
        "currentVolume": 12,
    }
    (depth_event,) = decode_market_event("GatewayDepth", ("C", [depth, {**depth, "type": 6}]))
    assert isinstance(depth_event, DepthEvent)
    assert [e.type for e in depth_event.entries] == [DomType.NewBestBid, DomType.Reset]
    assert depth_event.entries[0].current_volume == 12


@pytest.mark.parametrize(
    "arguments",
    [
        ("C",),  # data missing
        (42, {}),  # contract id is not a string
        ("C", "not an object"),
        ("C", [{"volume": 1.5}]),  # non-integral volume
    ],
)
def test_market_decode_failures_become_unknown_events(arguments: tuple[Any, ...]) -> None:
    (event,) = decode_market_event("GatewayTrade", arguments)
    assert isinstance(event, UnknownEvent)
    assert event.error is not None


def test_unknown_market_target() -> None:
    (event,) = decode_market_event("GatewayNews", ("C", {}))
    assert isinstance(event, UnknownEvent)
    assert event.error is None
