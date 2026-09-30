"""The 21 REST operations of the ProjectX Gateway API, as one table.

Each :class:`Operation` records its HTTP method and path, whether it needs a
bearer token, its client-side rate-limit bucket and its retry class, plus its
request and response types. The high-level client methods and the low-level
``execute`` escape hatch are both driven by this table::

    response = client.execute(operations.AUTH_VALIDATE)  # body-less operation
    response = client.execute(PlaceOrderRequest.market(...))  # operation from the request type
"""

from __future__ import annotations

import enum
from dataclasses import dataclass
from typing import Any, Generic, Literal, TypeVar

from .models import (
    CancelOrderRequest,
    CancelOrderResponse,
    CloseContractPositionRequest,
    ClosePositionResponse,
    ListAvailableContractRequest,
    ListAvailableContractResponse,
    LoginApiKeyRequest,
    LoginAppRequest,
    LoginResponse,
    LogoutResponse,
    ModifyOrderRequest,
    ModifyOrderResponse,
    PartialCloseContractPositionRequest,
    PartialClosePositionResponse,
    PlaceOrderRequest,
    PlaceOrderResponse,
    RetrieveBarRequest,
    RetrieveBarResponse,
    SearchAccountRequest,
    SearchAccountResponse,
    SearchContractByIdRequest,
    SearchContractByIdResponse,
    SearchContractRequest,
    SearchContractResponse,
    SearchHalfTradeResponse,
    SearchOpenOrderRequest,
    SearchOrderByIdRequest,
    SearchOrderByIdResponse,
    SearchOrderRequest,
    SearchOrderResponse,
    SearchOrdersQueryRequest,
    SearchOrdersQueryResponse,
    SearchPositionRequest,
    SearchPositionResponse,
    SearchTradeRequest,
    ValidateResponse,
)

__all__ = [
    "AuthMode",
    "Operation",
    "RateBucket",
    "RetryClass",
    "OPERATIONS",
    "by_id",
    "ACCOUNT_SEARCH_ACCOUNTS",
    "AUTH_LOGIN_APP",
    "AUTH_LOGIN_KEY",
    "AUTH_LOGOUT",
    "AUTH_VALIDATE",
    "CONTRACT_AVAILABLE_CONTRACTS",
    "CONTRACT_SEARCH_CONTRACTS",
    "CONTRACT_SEARCH_CONTRACT_BY_ID",
    "HISTORY_GET_BARS",
    "ORDER_CANCEL_ORDER",
    "ORDER_MODIFY_ORDER",
    "ORDER_PLACE_ORDER",
    "ORDER_SEARCH_OPEN_ORDERS",
    "ORDER_SEARCH_ORDERS",
    "ORDER_SEARCH_ORDERS_QUERY",
    "ORDER_SEARCH_ORDER_BY_ID",
    "POSITION_CLOSE_CONTRACT_POSITION",
    "POSITION_PARTIAL_CLOSE_CONTRACT_POSITION",
    "POSITION_SEARCH_OPEN_POSITIONS",
    "STATUS_PING",
    "TRADE_SEARCH_HALF_TURN_TRADES",
]

ReqT = TypeVar("ReqT")
RespT = TypeVar("RespT")


class AuthMode(enum.Enum):
    """How an operation authenticates."""

    NONE = "none"
    """No bearer token (the login operations)."""
    BEARER = "bearer"
    """Bearer token required; obtained (or refreshed) before the request."""
    OPTIONAL = "optional"
    """Bearer token attached when held or obtainable, otherwise anonymous (ping)."""


class RateBucket(enum.Enum):
    """Client-side rate-limit bucket."""

    GENERAL = "general"
    """200 requests per 60 s by default."""
    HISTORY = "history"
    """50 requests per 30 s by default (``History_GetBars``)."""


class RetryClass(enum.Enum):
    """Which failures an operation may be retried on (HTTP 429 always is)."""

    READ = "read"
    """Also retried on HTTP 502/503/504 and transport errors."""
    WRITE = "write"
    """Never retried on 5xx or transport errors: it may already have executed."""


@dataclass(frozen=True, slots=True, eq=False)
class Operation(Generic[ReqT, RespT]):
    """One REST operation of the specification.

    Attributes:
        operation_id: The spec ``operationId``, for example ``"Order_PlaceOrder"``.
        method: ``"GET"`` or ``"POST"``.
        path: The request path, for example ``"/api/Order/place"``.
        auth: How the operation authenticates.
        bucket: The client-side rate-limit bucket.
        retry: The retry class.
        request_type: The request model, or ``None`` for body-less operations.
        response_type: The response model; ``str`` for ``Status_Ping``.
    """

    operation_id: str
    method: Literal["GET", "POST"]
    path: str
    auth: AuthMode
    bucket: RateBucket
    retry: RetryClass
    request_type: type[ReqT] | None
    response_type: type[RespT]

    def __repr__(self) -> str:
        return f"<Operation {self.operation_id}: {self.method} {self.path}>"


_G = RateBucket.GENERAL
_R = RetryClass.READ
_W = RetryClass.WRITE
_BEARER = AuthMode.BEARER

ACCOUNT_SEARCH_ACCOUNTS: Operation[SearchAccountRequest, SearchAccountResponse] = Operation(
    "Account_SearchAccounts", "POST", "/api/Account/search", _BEARER, _G, _R,
    SearchAccountRequest, SearchAccountResponse,
)  # fmt: skip
AUTH_LOGIN_APP: Operation[LoginAppRequest, LoginResponse] = Operation(
    "Auth_LoginApp", "POST", "/api/Auth/loginApp", AuthMode.NONE, _G, _W,
    LoginAppRequest, LoginResponse,
)  # fmt: skip
AUTH_LOGIN_KEY: Operation[LoginApiKeyRequest, LoginResponse] = Operation(
    "Auth_LoginKey", "POST", "/api/Auth/loginKey", AuthMode.NONE, _G, _W,
    LoginApiKeyRequest, LoginResponse,
)  # fmt: skip
AUTH_LOGOUT: Operation[None, LogoutResponse] = Operation(
    "Auth_Logout", "POST", "/api/Auth/logout", _BEARER, _G, _W,
    None, LogoutResponse,
)  # fmt: skip
AUTH_VALIDATE: Operation[None, ValidateResponse] = Operation(
    "Auth_Validate", "POST", "/api/Auth/validate", _BEARER, _G, _W,
    None, ValidateResponse,
)  # fmt: skip
CONTRACT_SEARCH_CONTRACTS: Operation[SearchContractRequest, SearchContractResponse] = Operation(
    "Contract_SearchContracts", "POST", "/api/Contract/search", _BEARER, _G, _R,
    SearchContractRequest, SearchContractResponse,
)  # fmt: skip
CONTRACT_SEARCH_CONTRACT_BY_ID: Operation[
    SearchContractByIdRequest, SearchContractByIdResponse
] = Operation(
    "Contract_SearchContractById", "POST", "/api/Contract/searchById", _BEARER, _G, _R,
    SearchContractByIdRequest, SearchContractByIdResponse,
)  # fmt: skip
CONTRACT_AVAILABLE_CONTRACTS: Operation[
    ListAvailableContractRequest, ListAvailableContractResponse
] = Operation(
    "Contract_AvailableContracts", "POST", "/api/Contract/available", _BEARER, _G, _R,
    ListAvailableContractRequest, ListAvailableContractResponse,
)  # fmt: skip
HISTORY_GET_BARS: Operation[RetrieveBarRequest, RetrieveBarResponse] = Operation(
    "History_GetBars", "POST", "/api/History/retrieveBars", _BEARER, RateBucket.HISTORY, _R,
    RetrieveBarRequest, RetrieveBarResponse,
)  # fmt: skip
ORDER_SEARCH_ORDERS: Operation[SearchOrderRequest, SearchOrderResponse] = Operation(
    "Order_SearchOrders", "POST", "/api/Order/search", _BEARER, _G, _R,
    SearchOrderRequest, SearchOrderResponse,
)  # fmt: skip
ORDER_SEARCH_ORDER_BY_ID: Operation[SearchOrderByIdRequest, SearchOrderByIdResponse] = Operation(
    "Order_SearchOrderById", "POST", "/api/Order/searchById", _BEARER, _G, _R,
    SearchOrderByIdRequest, SearchOrderByIdResponse,
)  # fmt: skip
ORDER_SEARCH_OPEN_ORDERS: Operation[SearchOpenOrderRequest, SearchOrderResponse] = Operation(
    "Order_SearchOpenOrders", "POST", "/api/Order/searchOpen", _BEARER, _G, _R,
    SearchOpenOrderRequest, SearchOrderResponse,
)  # fmt: skip
ORDER_SEARCH_ORDERS_QUERY: Operation[SearchOrdersQueryRequest, SearchOrdersQueryResponse] = (
    Operation(
        "Order_SearchOrdersQuery", "POST", "/api/Order/v2/query", _BEARER, _G, _R,
        SearchOrdersQueryRequest, SearchOrdersQueryResponse,
    )
)  # fmt: skip
ORDER_PLACE_ORDER: Operation[PlaceOrderRequest, PlaceOrderResponse] = Operation(
    "Order_PlaceOrder", "POST", "/api/Order/place", _BEARER, _G, _W,
    PlaceOrderRequest, PlaceOrderResponse,
)  # fmt: skip
ORDER_CANCEL_ORDER: Operation[CancelOrderRequest, CancelOrderResponse] = Operation(
    "Order_CancelOrder", "POST", "/api/Order/cancel", _BEARER, _G, _W,
    CancelOrderRequest, CancelOrderResponse,
)  # fmt: skip
ORDER_MODIFY_ORDER: Operation[ModifyOrderRequest, ModifyOrderResponse] = Operation(
    "Order_ModifyOrder", "POST", "/api/Order/modify", _BEARER, _G, _W,
    ModifyOrderRequest, ModifyOrderResponse,
)  # fmt: skip
POSITION_SEARCH_OPEN_POSITIONS: Operation[SearchPositionRequest, SearchPositionResponse] = (
    Operation(
        "Position_SearchOpenPositions", "POST", "/api/Position/searchOpen", _BEARER, _G, _R,
        SearchPositionRequest, SearchPositionResponse,
    )
)  # fmt: skip
POSITION_CLOSE_CONTRACT_POSITION: Operation[
    CloseContractPositionRequest, ClosePositionResponse
] = Operation(
    "Position_CloseContractPosition", "POST", "/api/Position/closeContract", _BEARER, _G, _W,
    CloseContractPositionRequest, ClosePositionResponse,
)  # fmt: skip
POSITION_PARTIAL_CLOSE_CONTRACT_POSITION: Operation[
    PartialCloseContractPositionRequest, PartialClosePositionResponse
] = Operation(
    "Position_PartialCloseContractPosition", "POST", "/api/Position/partialCloseContract",
    _BEARER, _G, _W,
    PartialCloseContractPositionRequest, PartialClosePositionResponse,
)  # fmt: skip
STATUS_PING: Operation[None, str] = Operation(
    "Status_Ping", "GET", "/api/Status/ping", AuthMode.OPTIONAL, _G, _R,
    None, str,
)  # fmt: skip
TRADE_SEARCH_HALF_TURN_TRADES: Operation[SearchTradeRequest, SearchHalfTradeResponse] = Operation(
    "Trade_SearchHalfTurnTrades", "POST", "/api/Trade/search", _BEARER, _G, _R,
    SearchTradeRequest, SearchHalfTradeResponse,
)  # fmt: skip

OPERATIONS: tuple[Operation[Any, Any], ...] = (
    ACCOUNT_SEARCH_ACCOUNTS,
    AUTH_LOGIN_APP,
    AUTH_LOGIN_KEY,
    AUTH_LOGOUT,
    AUTH_VALIDATE,
    CONTRACT_SEARCH_CONTRACTS,
    CONTRACT_SEARCH_CONTRACT_BY_ID,
    CONTRACT_AVAILABLE_CONTRACTS,
    HISTORY_GET_BARS,
    ORDER_SEARCH_ORDERS,
    ORDER_SEARCH_ORDER_BY_ID,
    ORDER_SEARCH_OPEN_ORDERS,
    ORDER_SEARCH_ORDERS_QUERY,
    ORDER_PLACE_ORDER,
    ORDER_CANCEL_ORDER,
    ORDER_MODIFY_ORDER,
    POSITION_SEARCH_OPEN_POSITIONS,
    POSITION_CLOSE_CONTRACT_POSITION,
    POSITION_PARTIAL_CLOSE_CONTRACT_POSITION,
    STATUS_PING,
    TRADE_SEARCH_HALF_TURN_TRADES,
)
"""All 21 operations, in specification order."""

_BY_ID: dict[str, Operation[Any, Any]] = {op.operation_id: op for op in OPERATIONS}
_BY_REQUEST: dict[type[Any], Operation[Any, Any]] = {
    op.request_type: op for op in OPERATIONS if op.request_type is not None
}


def by_id(operation_id: str) -> Operation[Any, Any]:
    """Return the operation with the given ``operationId``.

    Raises:
        KeyError: If there is no such operation.
    """
    try:
        return _BY_ID[operation_id]
    except KeyError:
        raise KeyError(f"unknown operationId {operation_id!r}") from None


def for_request(request: object) -> Operation[Any, Any]:
    """Return the operation whose request body type is ``type(request)``.

    Raises:
        TypeError: If ``request`` is not a request model of any operation.
    """
    for cls in type(request).__mro__:
        op = _BY_REQUEST.get(cls)
        if op is not None:
            return op
    raise TypeError(f"{type(request).__name__} is not a request model of any operation")
