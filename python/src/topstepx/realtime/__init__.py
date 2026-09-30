"""Realtime (SignalR) hubs of the ProjectX Gateway API.

The SDK speaks the ASP.NET Core SignalR JSON hub protocol directly over
WebSockets (no SignalR library). Two hubs are available:

* :class:`UserHub` - accounts, orders, positions and trades of the user;
* :class:`MarketHub` - quotes, market trades and depth per contract.

Both deliver typed events through ``async for event in hub.events()`` and/or
callbacks registered with ``hub.on(EventType, callback)``; reconnect with a
fresh token and subscription replay are automatic.
"""

from .connection import (
    HubConnection,
    HubOptions,
    HubState,
    ReconnectPolicy,
    TokenProvider,
    TokenSource,
    build_hub_url,
)
from .events import (
    Closed,
    Connected,
    DepthEvent,
    GatewayDepth,
    GatewayQuote,
    GatewayTrade,
    GatewayUserAccount,
    GatewayUserOrder,
    GatewayUserPosition,
    GatewayUserTrade,
    HubEvent,
    MarketTradeEvent,
    QuoteEvent,
    Reconnected,
    Reconnecting,
    StateEvent,
    SubscriptionReplayFailed,
    UnknownEvent,
    UserAccountEvent,
    UserOrderEvent,
    UserPositionEvent,
    UserTradeEvent,
    decode_market_event,
    decode_user_event,
)
from .market import MarketHub
from .user import UserHub

__all__ = [
    "UserHub",
    "MarketHub",
    "HubConnection",
    "HubOptions",
    "HubState",
    "ReconnectPolicy",
    "TokenProvider",
    "TokenSource",
    "build_hub_url",
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
    # payloads
    "GatewayUserAccount",
    "GatewayUserOrder",
    "GatewayUserPosition",
    "GatewayUserTrade",
    "GatewayQuote",
    "GatewayTrade",
    "GatewayDepth",
    "decode_user_event",
    "decode_market_event",
]
