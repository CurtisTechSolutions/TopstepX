"""Realtime payload models, event types and lenient decoders.

The payload models follow the official realtime guide; the REST models are not
assumed identical. Every field except the ``id`` of user payloads is optional,
because payloads may be partial (quote updates in particular may carry only the
fields that changed).

Decoding is lenient and never raises into the connection:

* a user-hub payload may be bare (``{...}``), wrapped as
  ``{"action": n, "data": {...}}``, or an array of either; one event is emitted
  per element, with ``action`` exposed when present (its meaning is
  undocumented);
* a ``GatewayTrade`` or ``GatewayDepth`` payload may be one object or an array;
  it is normalized to a tuple delivered as one event per server message;
* volumes decode to ``int`` from an integer, an integral float (``3.0``) or a
  numeric string;
* unknown targets and payloads that fail to decode become :class:`UnknownEvent`
  carrying the target, the raw arguments and the decode error.
"""

from __future__ import annotations

from collections.abc import Callable, Mapping
from dataclasses import dataclass, field
from datetime import datetime
from decimal import Decimal
from typing import Any, TypeVar

from .._serde import (
    BOOL,
    DECIMAL,
    INT,
    LENIENT_INT,
    STR,
    TIMESTAMP,
    Model,
    enum,
    jfield,
    model,
)
from ..enums import DomType, OrderSide, OrderStatus, OrderType, PositionType, TradeLogType
from ..errors import DecodeError

__all__ = [
    # payloads
    "GatewayDepth",
    "GatewayQuote",
    "GatewayTrade",
    "GatewayUserAccount",
    "GatewayUserOrder",
    "GatewayUserPosition",
    "GatewayUserTrade",
    # events
    "HubEvent",
    "StateEvent",
    "Connected",
    "Reconnecting",
    "Reconnected",
    "Closed",
    "SubscriptionReplayFailed",
    "UserAccountEvent",
    "UserOrderEvent",
    "UserPositionEvent",
    "UserTradeEvent",
    "QuoteEvent",
    "MarketTradeEvent",
    "DepthEvent",
    "UnknownEvent",
    # decoders
    "decode_market_event",
    "decode_user_event",
]

P = TypeVar("P", bound=Model)

# ===========================================================================
# Payload models
# ===========================================================================


@model
class GatewayUserAccount(Model):
    """Payload of ``GatewayUserAccount``.

    Attributes:
        id: Account id.
        name: Account name.
        balance: Account balance.
        can_trade: Whether the account may trade.
        is_visible: Whether the account is visible.
        simulated: Whether the account is simulated.
    """

    id: int = jfield("id", LENIENT_INT)
    name: str | None = jfield("name", STR, default=None)
    balance: Decimal | None = jfield("balance", DECIMAL, default=None)
    can_trade: bool | None = jfield("canTrade", BOOL, default=None)
    is_visible: bool | None = jfield("isVisible", BOOL, default=None)
    simulated: bool | None = jfield("simulated", BOOL, default=None)


@model
class GatewayUserPosition(Model):
    """Payload of ``GatewayUserPosition``.

    Attributes:
        id: Position id.
        account_id: Account id.
        contract_id: Contract id.
        creation_timestamp: When the position was opened.
        type: Long or short.
        size: Position size.
        average_price: Average entry price.
    """

    id: int = jfield("id", LENIENT_INT)
    account_id: int | None = jfield("accountId", LENIENT_INT, default=None)
    contract_id: str | None = jfield("contractId", STR, default=None)
    creation_timestamp: datetime | None = jfield("creationTimestamp", TIMESTAMP, default=None)
    type: PositionType | None = jfield("type", enum(PositionType), default=None)
    size: int | None = jfield("size", LENIENT_INT, default=None)
    average_price: Decimal | None = jfield("averagePrice", DECIMAL, default=None)


@model
class GatewayUserOrder(Model):
    """Payload of ``GatewayUserOrder``.

    Attributes:
        id: Order id.
        account_id: Account id.
        contract_id: Contract id.
        symbol_id: Symbol id.
        creation_timestamp: Creation time.
        update_timestamp: Last update time.
        status: Order status.
        type: Order type.
        side: Order side.
        size: Order size.
        limit_price: Limit price.
        stop_price: Stop price.
        fill_volume: Filled quantity.
        filled_price: Average fill price.
        custom_tag: Custom tag.
    """

    id: int = jfield("id", LENIENT_INT)
    account_id: int | None = jfield("accountId", LENIENT_INT, default=None)
    contract_id: str | None = jfield("contractId", STR, default=None)
    symbol_id: str | None = jfield("symbolId", STR, default=None)
    creation_timestamp: datetime | None = jfield("creationTimestamp", TIMESTAMP, default=None)
    update_timestamp: datetime | None = jfield("updateTimestamp", TIMESTAMP, default=None)
    status: OrderStatus | None = jfield("status", enum(OrderStatus), default=None)
    type: OrderType | None = jfield("type", enum(OrderType), default=None)
    side: OrderSide | None = jfield("side", enum(OrderSide), default=None)
    size: int | None = jfield("size", LENIENT_INT, default=None)
    limit_price: Decimal | None = jfield("limitPrice", DECIMAL, default=None)
    stop_price: Decimal | None = jfield("stopPrice", DECIMAL, default=None)
    fill_volume: int | None = jfield("fillVolume", LENIENT_INT, default=None)
    filled_price: Decimal | None = jfield("filledPrice", DECIMAL, default=None)
    custom_tag: str | None = jfield("customTag", STR, default=None)


@model
class GatewayUserTrade(Model):
    """Payload of ``GatewayUserTrade``.

    Attributes:
        id: Trade id.
        account_id: Account id.
        contract_id: Contract id.
        creation_timestamp: Execution time.
        price: Execution price.
        profit_and_loss: Realized P&L; ``None`` for a half-turn trade.
        fees: Fees charged.
        side: Side of the execution.
        size: Executed quantity.
        voided: Whether the trade was voided.
        order_id: The order that produced the trade.
    """

    id: int = jfield("id", LENIENT_INT)
    account_id: int | None = jfield("accountId", LENIENT_INT, default=None)
    contract_id: str | None = jfield("contractId", STR, default=None)
    creation_timestamp: datetime | None = jfield("creationTimestamp", TIMESTAMP, default=None)
    price: Decimal | None = jfield("price", DECIMAL, default=None)
    profit_and_loss: Decimal | None = jfield("profitAndLoss", DECIMAL, default=None)
    fees: Decimal | None = jfield("fees", DECIMAL, default=None)
    side: OrderSide | None = jfield("side", enum(OrderSide), default=None)
    size: int | None = jfield("size", LENIENT_INT, default=None)
    voided: bool | None = jfield("voided", BOOL, default=None)
    order_id: int | None = jfield("orderId", LENIENT_INT, default=None)


@model
class GatewayQuote(Model):
    """Payload of ``GatewayQuote``; updates may carry only the changed fields.

    Attributes:
        symbol: Symbol.
        symbol_name: Symbol display name.
        last_price: Last traded price.
        best_bid: Best bid.
        best_ask: Best ask.
        change: Price change.
        change_percent: Price change in percent.
        open: Session open.
        high: Session high.
        low: Session low.
        volume: Session volume.
        last_updated: Last update time.
        timestamp: Quote time.
    """

    symbol: str | None = jfield("symbol", STR, default=None)
    symbol_name: str | None = jfield("symbolName", STR, default=None)
    last_price: Decimal | None = jfield("lastPrice", DECIMAL, default=None)
    best_bid: Decimal | None = jfield("bestBid", DECIMAL, default=None)
    best_ask: Decimal | None = jfield("bestAsk", DECIMAL, default=None)
    change: Decimal | None = jfield("change", DECIMAL, default=None)
    change_percent: Decimal | None = jfield("changePercent", DECIMAL, default=None)
    open: Decimal | None = jfield("open", DECIMAL, default=None)
    high: Decimal | None = jfield("high", DECIMAL, default=None)
    low: Decimal | None = jfield("low", DECIMAL, default=None)
    volume: int | None = jfield("volume", LENIENT_INT, default=None)
    last_updated: datetime | None = jfield("lastUpdated", TIMESTAMP, default=None)
    timestamp: datetime | None = jfield("timestamp", TIMESTAMP, default=None)


@model
class GatewayDepth(Model):
    """One entry of a ``GatewayDepth`` payload.

    Attributes:
        timestamp: Entry time.
        type: Depth entry type.
        price: Price level.
        volume: Volume at the level.
        current_volume: Current volume at the level.
    """

    timestamp: datetime | None = jfield("timestamp", TIMESTAMP, default=None)
    type: DomType | None = jfield("type", enum(DomType), default=None)
    price: Decimal | None = jfield("price", DECIMAL, default=None)
    volume: int | None = jfield("volume", LENIENT_INT, default=None)
    current_volume: int | None = jfield("currentVolume", LENIENT_INT, default=None)


@model
class GatewayTrade(Model):
    """One entry of a ``GatewayTrade`` (market trade) payload.

    Attributes:
        symbol_id: Symbol id.
        price: Trade price.
        timestamp: Trade time.
        type: Aggressor side.
        volume: Trade volume.
    """

    symbol_id: str | None = jfield("symbolId", STR, default=None)
    price: Decimal | None = jfield("price", DECIMAL, default=None)
    timestamp: datetime | None = jfield("timestamp", TIMESTAMP, default=None)
    type: TradeLogType | None = jfield("type", enum(TradeLogType), default=None)
    volume: int | None = jfield("volume", LENIENT_INT, default=None)


# ===========================================================================
# Events
# ===========================================================================


@dataclass(frozen=True, slots=True)
class HubEvent:
    """Base class of everything a hub delivers through ``events()`` and callbacks."""


@dataclass(frozen=True, slots=True)
class StateEvent(HubEvent):
    """Base class of connection state events."""


@dataclass(frozen=True, slots=True)
class Connected(StateEvent):
    """The hub connected (after ``start()``)."""


@dataclass(frozen=True, slots=True)
class Reconnecting(StateEvent):
    """The connection was lost and a reconnect attempt is about to start.

    Attributes:
        attempt: Attempt number.
        cause: Why the connection was lost, or why the previous attempt failed.
    """

    attempt: int
    cause: BaseException | None = None


@dataclass(frozen=True, slots=True)
class Reconnected(StateEvent):
    """The hub reconnected and replayed its subscriptions.

    Attributes:
        attempt: The attempt that succeeded.
    """

    attempt: int


@dataclass(frozen=True, slots=True)
class Closed(StateEvent):
    """The hub is closed for good; the event stream ends after this event.

    Attributes:
        cause: ``None`` after ``close()``; otherwise the error that ended the
            connection (a close without ``allowReconnect``, or exhausted
            reconnect attempts).
    """

    cause: BaseException | None = None


@dataclass(frozen=True, slots=True)
class SubscriptionReplayFailed(StateEvent):
    """Re-subscribing after a reconnect failed.

    The subscription stays registered and is replayed after the next reconnect.

    Attributes:
        method: The ``Subscribe*`` method.
        arguments: Its arguments.
        error: The failure.
    """

    method: str
    arguments: tuple[Any, ...]
    error: BaseException


@dataclass(frozen=True, slots=True)
class UserAccountEvent(HubEvent):
    """``GatewayUserAccount``: an account update.

    Attributes:
        account: The (possibly partial) account.
        action: The envelope's ``action`` when present (meaning undocumented).
        raw: The raw JSON object.
    """

    account: GatewayUserAccount
    action: int | None = None
    raw: Mapping[str, Any] = field(default_factory=dict, repr=False, compare=False)


@dataclass(frozen=True, slots=True)
class UserOrderEvent(HubEvent):
    """``GatewayUserOrder``: an order update.

    Attributes:
        order: The (possibly partial) order.
        action: The envelope's ``action`` when present (meaning undocumented).
        raw: The raw JSON object.
    """

    order: GatewayUserOrder
    action: int | None = None
    raw: Mapping[str, Any] = field(default_factory=dict, repr=False, compare=False)


@dataclass(frozen=True, slots=True)
class UserPositionEvent(HubEvent):
    """``GatewayUserPosition``: a position update.

    Attributes:
        position: The (possibly partial) position.
        action: The envelope's ``action`` when present (meaning undocumented).
        raw: The raw JSON object.
    """

    position: GatewayUserPosition
    action: int | None = None
    raw: Mapping[str, Any] = field(default_factory=dict, repr=False, compare=False)


@dataclass(frozen=True, slots=True)
class UserTradeEvent(HubEvent):
    """``GatewayUserTrade``: an execution on one of the user's accounts.

    Attributes:
        trade: The (possibly partial) trade.
        action: The envelope's ``action`` when present (meaning undocumented).
        raw: The raw JSON object.
    """

    trade: GatewayUserTrade
    action: int | None = None
    raw: Mapping[str, Any] = field(default_factory=dict, repr=False, compare=False)


@dataclass(frozen=True, slots=True)
class QuoteEvent(HubEvent):
    """``GatewayQuote``: a quote update for ``contract_id``.

    Attributes:
        contract_id: The contract id from the event arguments.
        quote: The (possibly partial) quote.
        raw: The raw JSON object.
    """

    contract_id: str
    quote: GatewayQuote
    raw: Mapping[str, Any] = field(default_factory=dict, repr=False, compare=False)


@dataclass(frozen=True, slots=True)
class MarketTradeEvent(HubEvent):
    """``GatewayTrade``: market trades for ``contract_id`` (one event per message).

    Attributes:
        contract_id: The contract id from the event arguments.
        trades: The trades of the message (a single object is normalized to one entry).
        raw: The raw payload.
    """

    contract_id: str
    trades: tuple[GatewayTrade, ...]
    raw: Any = field(default=None, repr=False, compare=False)


@dataclass(frozen=True, slots=True)
class DepthEvent(HubEvent):
    """``GatewayDepth``: market-depth entries for ``contract_id``.

    One event is delivered per server message.

    Attributes:
        contract_id: The contract id from the event arguments.
        entries: The depth entries (a single object is normalized to one entry).
        raw: The raw payload.
    """

    contract_id: str
    entries: tuple[GatewayDepth, ...]
    raw: Any = field(default=None, repr=False, compare=False)


@dataclass(frozen=True, slots=True)
class UnknownEvent(HubEvent):
    """A server event with an unknown target, or whose payload failed to decode.

    Attributes:
        target: The server method name.
        arguments: The raw arguments.
        error: The decode error, or ``None`` for an unknown target.
    """

    target: str
    arguments: tuple[Any, ...]
    error: Exception | None = None


# ===========================================================================
# Decoders
# ===========================================================================


def _action(value: Any) -> int | None:
    if value is None:
        return None
    try:
        return INT.decode(value, "action")
    except DecodeError:
        return None


def _unwrap_user_payload(payload: Any) -> list[tuple[Any, int | None]]:
    """Flatten bare / enveloped / array payloads into ``(data, action)`` pairs."""
    items = payload if isinstance(payload, list) else [payload]
    result: list[tuple[Any, int | None]] = []
    for item in items:
        if isinstance(item, dict) and "data" in item and isinstance(item["data"], (dict, list)):
            action = _action(item.get("action"))
            inner = item["data"]
            elements = inner if isinstance(inner, list) else [inner]
            result.extend((element, action) for element in elements)
        else:
            result.append((item, None))
    return result


def _user_decoder(
    payload_type: type[P], make: Callable[[P, int | None, Mapping[str, Any]], HubEvent]
) -> Callable[[str, tuple[Any, ...]], list[HubEvent]]:
    def decode(target: str, arguments: tuple[Any, ...]) -> list[HubEvent]:
        if not arguments:
            return [UnknownEvent(target, arguments, DecodeError("event has no arguments"))]
        events: list[HubEvent] = []
        for data, action in _unwrap_user_payload(arguments[0]):
            if not isinstance(data, dict):
                error = DecodeError(f"expected an object, got {type(data).__name__}", path=target)
                events.append(UnknownEvent(target, arguments, error))
                continue
            try:
                events.append(make(payload_type.from_dict(data), action, data))
            except DecodeError as exc:
                events.append(UnknownEvent(target, arguments, exc))
        return events

    return decode


_USER_DECODERS: dict[str, Callable[[str, tuple[Any, ...]], list[HubEvent]]] = {
    "GatewayUserAccount": _user_decoder(
        GatewayUserAccount, lambda p, a, r: UserAccountEvent(p, a, r)
    ),
    "GatewayUserOrder": _user_decoder(GatewayUserOrder, lambda p, a, r: UserOrderEvent(p, a, r)),
    "GatewayUserPosition": _user_decoder(
        GatewayUserPosition, lambda p, a, r: UserPositionEvent(p, a, r)
    ),
    "GatewayUserTrade": _user_decoder(GatewayUserTrade, lambda p, a, r: UserTradeEvent(p, a, r)),
}


def decode_user_event(target: str, arguments: tuple[Any, ...]) -> list[HubEvent]:
    """Decode one user-hub server invocation into events (never raises)."""
    decoder = _USER_DECODERS.get(target)
    if decoder is None:
        return [UnknownEvent(target, arguments)]
    try:
        return decoder(target, arguments)
    except Exception as exc:  # defensive: decoding must never break the connection
        return [UnknownEvent(target, arguments, exc)]


def _market_entries(payload_type: type[P], payload: Any, target: str) -> tuple[P, ...]:
    items = payload if isinstance(payload, list) else [payload]
    entries: list[P] = []
    for index, item in enumerate(items):
        if not isinstance(item, dict):
            raise DecodeError(
                f"expected an object, got {type(item).__name__}", path=f"{target}[{index}]"
            )
        entries.append(payload_type.from_dict(item))
    return tuple(entries)


def _decode_market(target: str, arguments: tuple[Any, ...]) -> list[HubEvent]:
    if len(arguments) < 2:
        return [UnknownEvent(target, arguments, DecodeError("expected (contractId, data)"))]
    contract_id, payload = arguments[0], arguments[1]
    if not isinstance(contract_id, str):
        error = DecodeError(f"contractId must be a string, got {type(contract_id).__name__}")
        return [UnknownEvent(target, arguments, error)]
    try:
        if target == "GatewayQuote":
            return [
                QuoteEvent(contract_id, quote, raw)
                for quote, raw in zip(
                    _market_entries(GatewayQuote, payload, target),
                    payload if isinstance(payload, list) else [payload],
                    strict=True,
                )
            ]
        if target == "GatewayTrade":
            return [
                MarketTradeEvent(
                    contract_id, _market_entries(GatewayTrade, payload, target), payload
                )
            ]
        return [DepthEvent(contract_id, _market_entries(GatewayDepth, payload, target), payload)]
    except DecodeError as exc:
        return [UnknownEvent(target, arguments, exc)]


_MARKET_TARGETS = frozenset({"GatewayQuote", "GatewayTrade", "GatewayDepth"})


def decode_market_event(target: str, arguments: tuple[Any, ...]) -> list[HubEvent]:
    """Decode one market-hub server invocation into events (never raises)."""
    if target not in _MARKET_TARGETS:
        return [UnknownEvent(target, arguments)]
    try:
        return _decode_market(target, arguments)
    except Exception as exc:  # defensive: decoding must never break the connection
        return [UnknownEvent(target, arguments, exc)]
