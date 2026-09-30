"""Python SDK for the ProjectX Gateway API as deployed by TopstepX.

Quickstart::

    from topstepx import Client

    with Client.from_env() as client:  # TOPSTEPX_USERNAME / TOPSTEPX_API_KEY
        for account in client.accounts.search():
            print(account.id, account.name, account.balance)

* :class:`Client` (blocking, thread-safe) and :class:`AsyncClient` (asyncio)
  expose the same endpoint groups: ``auth``, ``accounts``, ``contracts``,
  ``history``, ``orders``, ``positions``, ``trades`` and ``status``.
* :mod:`topstepx.realtime` provides the SignalR ``UserHub`` and ``MarketHub``.
* :mod:`topstepx.models`, :mod:`topstepx.enums` and :mod:`topstepx.errors`
  hold the typed models, enums and exceptions; :mod:`topstepx.operations` the
  table of the 21 REST operations.

The behavior follows the cross-SDK contract in ``spec/SDK_BEHAVIOR.md``.
**Orders are real**: develop against simulated accounts.
"""

from . import enums, errors, models, operations, realtime
from ._config import (
    DEFAULT_BASE_URL,
    DEFAULT_MARKET_HUB_URL,
    DEFAULT_USER_HUB_URL,
    USER_AGENT,
    Backoff,
    RateLimit,
    RateLimits,
)
from ._version import __version__
from .async_client import AsyncClient
from .client import Client
from .enums import (
    AggregateBarUnit,
    DomType,
    OrderSide,
    OrderSortField,
    OrderStatus,
    OrderType,
    PositionType,
    SortDirection,
    SpecEnum,
    TradeLogType,
)
from .errors import (
    ApiError,
    ConnectionLostError,
    DecodeError,
    HandshakeError,
    HTTPStatusError,
    HubConnectError,
    HubError,
    HubTimeoutError,
    InvocationError,
    NotAuthenticatedError,
    NotConnectedError,
    RateLimitError,
    ServerCloseError,
    TokenRejectedError,
    TopstepXError,
    TransportError,
    UnauthorizedError,
    ValidationError,
)
from .models import (
    AggregateBar,
    CancelOrderRequest,
    CloseContractPositionRequest,
    Contract,
    GatewayOrderFilter,
    HalfTrade,
    ListAvailableContractRequest,
    LoginApiKeyRequest,
    LoginAppRequest,
    ModifyOrderRequest,
    Order,
    OrderQueryResult,
    PartialCloseContractPositionRequest,
    PlaceOrderBracket,
    PlaceOrderRequest,
    Position,
    ProblemDetails,
    RetrieveBarRequest,
    SearchAccountRequest,
    SearchContractByIdRequest,
    SearchContractRequest,
    SearchOpenOrderRequest,
    SearchOrderByIdRequest,
    SearchOrderRequest,
    SearchOrdersQueryRequest,
    SearchPositionRequest,
    SearchTradeRequest,
    TradingAccount,
)
from .operations import Operation
from .realtime import HubOptions, MarketHub, ReconnectPolicy, UserHub

__all__ = [
    "__version__",
    # clients and configuration
    "Client",
    "AsyncClient",
    "Backoff",
    "RateLimit",
    "RateLimits",
    "Operation",
    "DEFAULT_BASE_URL",
    "DEFAULT_USER_HUB_URL",
    "DEFAULT_MARKET_HUB_URL",
    "USER_AGENT",
    # realtime
    "UserHub",
    "MarketHub",
    "HubOptions",
    "ReconnectPolicy",
    # modules
    "enums",
    "errors",
    "models",
    "operations",
    "realtime",
    # enums
    "SpecEnum",
    "AggregateBarUnit",
    "OrderSide",
    "OrderSortField",
    "OrderStatus",
    "OrderType",
    "PositionType",
    "SortDirection",
    "DomType",
    "TradeLogType",
    # errors
    "TopstepXError",
    "ApiError",
    "HTTPStatusError",
    "UnauthorizedError",
    "RateLimitError",
    "TransportError",
    "DecodeError",
    "ValidationError",
    "NotAuthenticatedError",
    "HubError",
    "HubConnectError",
    "HandshakeError",
    "NotConnectedError",
    "ConnectionLostError",
    "ServerCloseError",
    "TokenRejectedError",
    "InvocationError",
    "HubTimeoutError",
    # models
    "TradingAccount",
    "Contract",
    "AggregateBar",
    "Order",
    "Position",
    "HalfTrade",
    "ProblemDetails",
    "PlaceOrderBracket",
    "GatewayOrderFilter",
    "OrderQueryResult",
    "SearchAccountRequest",
    "LoginApiKeyRequest",
    "LoginAppRequest",
    "SearchContractRequest",
    "SearchContractByIdRequest",
    "ListAvailableContractRequest",
    "RetrieveBarRequest",
    "SearchOrderRequest",
    "SearchOrderByIdRequest",
    "SearchOpenOrderRequest",
    "SearchOrdersQueryRequest",
    "PlaceOrderRequest",
    "CancelOrderRequest",
    "ModifyOrderRequest",
    "SearchPositionRequest",
    "CloseContractPositionRequest",
    "PartialCloseContractPositionRequest",
    "SearchTradeRequest",
]
