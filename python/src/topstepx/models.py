"""Typed models of the ProjectX Gateway REST API.

Request, response and enum types keep their specification schema names. Object
models drop the ``Model`` suffix (``OrderModel`` is :class:`Order`). Attribute
names are the snake_case form of the JSON names, except the single-letter bar
fields, which get descriptive names (see :class:`AggregateBar`).

All models are frozen dataclasses with keyword-only constructors:

* numbers with the spec format ``decimal`` are :class:`decimal.Decimal` and are
  decoded and encoded without losing digits;
* timestamps are aware :class:`datetime.datetime` objects in UTC (naive values
  passed to a request are taken to be UTC);
* every field the spec marks nullable or not required is optional (``None``);
  missing arrays decode as empty tuples;
* unknown JSON members are ignored; ``None`` request fields are omitted.

Secrets (API keys, passwords, tokens) are redacted from ``repr``.
"""

from __future__ import annotations

import dataclasses
from collections.abc import Mapping, Sequence
from datetime import datetime
from decimal import Decimal
from typing import Any, ClassVar, Generic, TypeVar

from ._serde import (
    ANY,
    BOOL,
    DECIMAL,
    INT,
    STR,
    TIMESTAMP,
    Model,
    array,
    enum,
    jfield,
    model,
    nested,
    to_decimal,
)
from .enums import (
    AggregateBarUnit,
    CancelOrderErrorCode,
    ClosePositionErrorCode,
    ListAvailableContractErrorCode,
    LoginErrorCode,
    LogoutErrorCode,
    ModifyOrderErrorCode,
    OrderSide,
    OrderSortField,
    OrderStatus,
    OrderType,
    PartialClosePositionErrorCode,
    PlaceOrderErrorCode,
    PositionType,
    RetrieveBarErrorCode,
    SearchAccountErrorCode,
    SearchContractByIdErrorCode,
    SearchContractErrorCode,
    SearchOrderByIdErrorCode,
    SearchOrderErrorCode,
    SearchPositionErrorCode,
    SearchTradeErrorCode,
    SortDirection,
    SpecEnum,
    ValidateErrorCode,
)

__all__ = [
    "MAX_BARS",
    "Model",
    "RequestModel",
    "ResponseEnvelope",
    # objects
    "AggregateBar",
    "Contract",
    "GatewayOrderFilter",
    "HalfTrade",
    "Order",
    "PlaceOrderBracket",
    "Position",
    "ProblemDetails",
    "TradingAccount",
    # requests
    "CancelOrderRequest",
    "CloseContractPositionRequest",
    "ListAvailableContractRequest",
    "LoginApiKeyRequest",
    "LoginAppRequest",
    "ModifyOrderRequest",
    "PartialCloseContractPositionRequest",
    "PlaceOrderRequest",
    "RetrieveBarRequest",
    "SearchAccountRequest",
    "SearchContractByIdRequest",
    "SearchContractRequest",
    "SearchOpenOrderRequest",
    "SearchOrderByIdRequest",
    "SearchOrderRequest",
    "SearchOrdersQueryRequest",
    "SearchPositionRequest",
    "SearchTradeRequest",
    # responses
    "CancelOrderResponse",
    "ClosePositionResponse",
    "ListAvailableContractResponse",
    "LoginResponse",
    "LogoutResponse",
    "ModifyOrderResponse",
    "PartialClosePositionResponse",
    "PlaceOrderResponse",
    "RetrieveBarResponse",
    "SearchAccountResponse",
    "SearchContractByIdResponse",
    "SearchContractResponse",
    "SearchHalfTradeResponse",
    "SearchOrderByIdResponse",
    "SearchOrderResponse",
    "SearchOrdersQueryResponse",
    "SearchPositionResponse",
    "ValidateResponse",
    # results
    "OrderQueryResult",
    "SCHEMA_MODELS",
]

RespT = TypeVar("RespT", bound="ResponseEnvelope")

MAX_BARS = 20_000
"""The documented maximum ``RetrieveBarRequest.limit``."""

Number = Decimal | int | float | str
"""Values accepted by the convenience constructors for prices."""


def _dec(value: Number | None) -> Decimal | None:
    return None if value is None else to_decimal(value)


# ===========================================================================
# Shared envelope and request base
# ===========================================================================


@model
class ResponseEnvelope(Model):
    """Fields carried by every REST response except ``Status_Ping``.

    A response is a *business failure* when ``success`` is false or
    ``error_code`` is non-zero (see :attr:`is_business_failure`). High-level
    client methods raise :class:`~topstepx.errors.ApiError` for business failures;
    ``Client.execute`` returns the envelope unchanged.

    Attributes:
        success: Whether the operation succeeded.
        error_code: The operation's error code (``0`` is success). Each response
            type narrows this to its own enum.
        error_message: Optional error text; may be ``None`` even on failure.
        has_business_failure: The server's own flag; ``False`` when absent.
    """

    success: bool = jfield("success", BOOL)
    error_code: int = jfield("errorCode", INT)
    error_message: str | None = jfield("errorMessage", STR, default=None)
    has_business_failure: bool = jfield("hasBusinessFailure", BOOL, default=False)

    @property
    def is_business_failure(self) -> bool:
        """``True`` when ``success`` is false or ``error_code`` is non-zero."""
        return not self.success or int(self.error_code) != 0

    @property
    def error_code_name(self) -> str:
        """The enum name of :attr:`error_code`, or ``"Unrecognized(<n>)"``."""
        code = self.error_code
        if isinstance(code, SpecEnum):
            return code.name
        return f"Unrecognized({int(code)})"


class RequestModel(Model, Generic[RespT]):
    """Base class of request bodies; parameterized by the operation's response type.

    ``__operation_id__`` names the operation the request belongs to, which lets
    ``Client.execute(request)`` find it.
    """

    __slots__ = ()
    __operation_id__: ClassVar[str] = ""


# ===========================================================================
# Objects
# ===========================================================================


@model
class TradingAccount(Model):
    """A trading account (``TradingAccountModel``).

    Attributes:
        id: Account id.
        name: Account name.
        balance: Account balance.
        can_trade: Whether the account may trade.
        is_visible: Whether the account is visible.
        simulated: Whether the account is simulated.
    """

    __schema__ = "TradingAccountModel"

    id: int = jfield("id", INT)
    name: str = jfield("name", STR)
    balance: Decimal = jfield("balance", DECIMAL)
    can_trade: bool = jfield("canTrade", BOOL)
    is_visible: bool = jfield("isVisible", BOOL)
    simulated: bool = jfield("simulated", BOOL)


@model
class Contract(Model):
    """A tradable contract (``ContractModel``).

    Attributes:
        id: Contract id, for example ``"CON.F.US.EP.U25"``.
        name: Short name, for example ``"ESU5"``.
        description: Human-readable description.
        tick_size: Minimum price increment.
        tick_value: Value of one tick.
        active_contract: Whether this is the active contract.
        symbol_id: Symbol id, for example ``"F.US.EP"``.
    """

    __schema__ = "ContractModel"

    id: str = jfield("id", STR)
    name: str = jfield("name", STR)
    description: str = jfield("description", STR)
    tick_size: Decimal = jfield("tickSize", DECIMAL)
    tick_value: Decimal = jfield("tickValue", DECIMAL)
    active_contract: bool = jfield("activeContract", BOOL)
    symbol_id: str = jfield("symbolId", STR)


@model
class AggregateBar(Model):
    """One OHLCV bar (``AggregateBarModel``).

    The JSON uses single-letter names; the attributes are descriptive:
    ``t`` is :attr:`timestamp`, ``o`` :attr:`open`, ``h`` :attr:`high`,
    ``l`` :attr:`low`, ``c`` :attr:`close` and ``v`` :attr:`volume`.
    ``d`` and ``k`` are present in Swagger but undocumented; they are exposed raw
    and their meaning is not guessed.

    Attributes:
        timestamp: Bar timestamp (``t``).
        open: Opening price (``o``).
        high: Highest price (``h``).
        low: Lowest price (``l``).
        close: Closing price (``c``).
        volume: Traded volume (``v``).
        d: Undocumented date string (``format: date`` in Swagger), exposed raw.
        k: Undocumented integer, exposed raw.
    """

    __schema__ = "AggregateBarModel"

    timestamp: datetime = jfield("t", TIMESTAMP)
    open: Decimal = jfield("o", DECIMAL)
    high: Decimal = jfield("h", DECIMAL)
    low: Decimal = jfield("l", DECIMAL)
    close: Decimal = jfield("c", DECIMAL)
    volume: int = jfield("v", INT)
    d: str | None = jfield("d", STR, default=None)
    k: int | None = jfield("k", INT, default=None)


@model
class Order(Model):
    """An order (``OrderModel``).

    ``update_timestamp`` is required by the spec but modeled as optional on
    purpose, so a null update time never fails decoding.

    Attributes:
        id: Order id.
        account_id: Account id.
        contract_id: Contract id.
        symbol_id: Symbol id.
        creation_timestamp: When the order was created.
        update_timestamp: When the order was last updated, if known.
        status: Order status.
        type: Order type.
        side: Order side.
        size: Order size.
        limit_price: Limit price, if any.
        stop_price: Stop price, if any.
        fill_volume: Filled quantity.
        filled_price: Average fill price, if any.
        custom_tag: The caller's custom tag, if any.
        trail_distance: Trailing distance, if any.
        trail_price: For a returned trailing stop this is the price **distance**
            (ticks multiplied by tick size), not the absolute level that was
            submitted. Compare :attr:`PlaceOrderRequest.trail_price`.
        parent_order_id: Parent order id (brackets), if any.
        linked_order_id: Linked order id (brackets), if any.
    """

    __schema__ = "OrderModel"

    id: int = jfield("id", INT)
    account_id: int = jfield("accountId", INT)
    contract_id: str = jfield("contractId", STR)
    symbol_id: str = jfield("symbolId", STR)
    creation_timestamp: datetime = jfield("creationTimestamp", TIMESTAMP)
    update_timestamp: datetime | None = jfield("updateTimestamp", TIMESTAMP, default=None)
    status: OrderStatus = jfield("status", enum(OrderStatus))
    type: OrderType = jfield("type", enum(OrderType))
    side: OrderSide = jfield("side", enum(OrderSide))
    size: int = jfield("size", INT)
    limit_price: Decimal | None = jfield("limitPrice", DECIMAL, default=None)
    stop_price: Decimal | None = jfield("stopPrice", DECIMAL, default=None)
    fill_volume: int = jfield("fillVolume", INT)
    filled_price: Decimal | None = jfield("filledPrice", DECIMAL, default=None)
    custom_tag: str | None = jfield("customTag", STR, default=None)
    trail_distance: int | None = jfield("trailDistance", INT, default=None)
    trail_price: Decimal | None = jfield("trailPrice", DECIMAL, default=None)
    parent_order_id: int | None = jfield("parentOrderId", INT, default=None)
    linked_order_id: int | None = jfield("linkedOrderId", INT, default=None)


@model
class Position(Model):
    """An open position (``PositionModel``).

    Attributes:
        id: Position id.
        account_id: Account id.
        contract_id: Contract id.
        contract_display_name: Display name of the contract, if provided.
        creation_timestamp: When the position was opened.
        type: Long or short.
        size: Position size.
        average_price: Average entry price.
    """

    __schema__ = "PositionModel"

    id: int = jfield("id", INT)
    account_id: int = jfield("accountId", INT)
    contract_id: str = jfield("contractId", STR)
    contract_display_name: str | None = jfield("contractDisplayName", STR, default=None)
    creation_timestamp: datetime = jfield("creationTimestamp", TIMESTAMP)
    type: PositionType = jfield("type", enum(PositionType))
    size: int = jfield("size", INT)
    average_price: Decimal = jfield("averagePrice", DECIMAL)


@model
class HalfTrade(Model):
    """A trade execution (``HalfTradeModel``).

    Attributes:
        id: Trade id.
        account_id: Account id.
        contract_id: Contract id.
        creation_timestamp: Execution time.
        price: Execution price.
        profit_and_loss: Realized P&L; ``None`` marks a half-turn trade.
        fees: Fees charged.
        commissions: Commissions, if provided.
        side: Side of the execution.
        size: Executed quantity.
        voided: Whether the trade was voided.
        order_id: The order that produced the trade.
    """

    __schema__ = "HalfTradeModel"

    id: int = jfield("id", INT)
    account_id: int = jfield("accountId", INT)
    contract_id: str = jfield("contractId", STR)
    creation_timestamp: datetime = jfield("creationTimestamp", TIMESTAMP)
    price: Decimal = jfield("price", DECIMAL)
    profit_and_loss: Decimal | None = jfield("profitAndLoss", DECIMAL, default=None)
    fees: Decimal = jfield("fees", DECIMAL)
    commissions: Decimal | None = jfield("commissions", DECIMAL, default=None)
    side: OrderSide = jfield("side", enum(OrderSide))
    size: int = jfield("size", INT)
    voided: bool = jfield("voided", BOOL)
    order_id: int = jfield("orderId", INT)


@model
class ProblemDetails(Model):
    """An RFC 7807 problem document, as returned with HTTP 400 by some operations.

    Members the spec does not name are kept in :attr:`extensions`.

    Attributes:
        type: Problem type URI.
        title: Short summary.
        status: HTTP status.
        detail: Explanation.
        instance: URI of this occurrence.
        extensions: Every other member, with raw JSON values.
    """

    __schema__ = "ProblemDetails"

    type: str | None = jfield("type", STR, default=None)
    title: str | None = jfield("title", STR, default=None)
    status: int | None = jfield("status", INT, default=None)
    detail: str | None = jfield("detail", STR, default=None)
    instance: str | None = jfield("instance", STR, default=None)
    extensions: Mapping[str, Any] = jfield("(extensions)", ANY, default_factory=dict, extras=True)


@model
class PlaceOrderBracket(Model):
    """A stop-loss or take-profit bracket attached to an order placement.

    Brackets require the account's *Auto OCO Brackets* mode; accounts in
    *Position Brackets* mode reject them with error code 2.

    Attributes:
        ticks: Bracket distance in ticks.
        type: Bracket order type; one of Limit, Market, Stop, TrailingStop,
            JoinBid or JoinAsk.
    """

    __schema__ = "PlaceOrderBracket"

    ticks: int = jfield("ticks", INT)
    type: OrderType = jfield("type", enum(OrderType))


@model
class GatewayOrderFilter(Model):
    """Filter of an ``Order_SearchOrdersQuery`` request.

    Attributes:
        account_id: Account to search (required).
        statuses: Only orders with these statuses.
        contract_id: Only orders for this contract.
        created_after: Only orders created after this time.
        created_before: Only orders created before this time.
    """

    __schema__ = "GatewayOrderFilter"

    account_id: int = jfield("accountId", INT)
    statuses: Sequence[OrderStatus] | None = jfield(
        "statuses", array(enum(OrderStatus)), default=None
    )
    contract_id: str | None = jfield("contractId", STR, default=None)
    created_after: datetime | None = jfield("createdAfter", TIMESTAMP, default=None)
    created_before: datetime | None = jfield("createdBefore", TIMESTAMP, default=None)


# ===========================================================================
# Responses
# ===========================================================================


@model
class SearchAccountResponse(ResponseEnvelope):
    """Response of ``Account_SearchAccounts``."""

    __schema__ = "SearchAccountResponse"

    error_code: SearchAccountErrorCode = jfield("errorCode", enum(SearchAccountErrorCode))
    accounts: tuple[TradingAccount, ...] = jfield(
        "accounts", array(nested(TradingAccount)), default=()
    )


@model
class LoginResponse(ResponseEnvelope):
    """Response of ``Auth_LoginKey`` and ``Auth_LoginApp``. ``token`` is redacted in repr."""

    __schema__ = "LoginResponse"

    error_code: LoginErrorCode = jfield("errorCode", enum(LoginErrorCode))
    token: str | None = jfield("token", STR, default=None, secret=True)


@model
class LogoutResponse(ResponseEnvelope):
    """Response of ``Auth_Logout``."""

    __schema__ = "LogoutResponse"

    error_code: LogoutErrorCode = jfield("errorCode", enum(LogoutErrorCode))


@model
class ValidateResponse(ResponseEnvelope):
    """Response of ``Auth_Validate``. ``new_token`` is redacted in repr."""

    __schema__ = "ValidateResponse"

    error_code: ValidateErrorCode = jfield("errorCode", enum(ValidateErrorCode))
    new_token: str | None = jfield("newToken", STR, default=None, secret=True)


@model
class SearchContractResponse(ResponseEnvelope):
    """Response of ``Contract_SearchContracts``."""

    __schema__ = "SearchContractResponse"

    error_code: SearchContractErrorCode = jfield("errorCode", enum(SearchContractErrorCode))
    contracts: tuple[Contract, ...] = jfield("contracts", array(nested(Contract)), default=())


@model
class SearchContractByIdResponse(ResponseEnvelope):
    """Response of ``Contract_SearchContractById``."""

    __schema__ = "SearchContractByIdResponse"

    error_code: SearchContractByIdErrorCode = jfield("errorCode", enum(SearchContractByIdErrorCode))
    contract: Contract | None = jfield("contract", nested(Contract), default=None)


@model
class ListAvailableContractResponse(ResponseEnvelope):
    """Response of ``Contract_AvailableContracts``."""

    __schema__ = "ListAvailableContractResponse"

    error_code: ListAvailableContractErrorCode = jfield(
        "errorCode", enum(ListAvailableContractErrorCode)
    )
    contracts: tuple[Contract, ...] = jfield("contracts", array(nested(Contract)), default=())


@model
class RetrieveBarResponse(ResponseEnvelope):
    """Response of ``History_GetBars``."""

    __schema__ = "RetrieveBarResponse"

    error_code: RetrieveBarErrorCode = jfield("errorCode", enum(RetrieveBarErrorCode))
    bars: tuple[AggregateBar, ...] = jfield("bars", array(nested(AggregateBar)), default=())


@model
class SearchOrderResponse(ResponseEnvelope):
    """Response of ``Order_SearchOrders`` and ``Order_SearchOpenOrders``."""

    __schema__ = "SearchOrderResponse"

    error_code: SearchOrderErrorCode = jfield("errorCode", enum(SearchOrderErrorCode))
    orders: tuple[Order, ...] = jfield("orders", array(nested(Order)), default=())


@model
class SearchOrderByIdResponse(ResponseEnvelope):
    """Response of ``Order_SearchOrderById``."""

    __schema__ = "SearchOrderByIdResponse"

    error_code: SearchOrderByIdErrorCode = jfield("errorCode", enum(SearchOrderByIdErrorCode))
    order: Order | None = jfield("order", nested(Order), default=None)


@model
class SearchOrdersQueryResponse(ResponseEnvelope):
    """Response of ``Order_SearchOrdersQuery``."""

    __schema__ = "SearchOrdersQueryResponse"

    error_code: SearchOrderErrorCode = jfield("errorCode", enum(SearchOrderErrorCode))
    orders: tuple[Order, ...] = jfield("orders", array(nested(Order)), default=())
    total_count: int | None = jfield("totalCount", INT, default=None)


@model
class PlaceOrderResponse(ResponseEnvelope):
    """Response of ``Order_PlaceOrder``.

    ``order_id`` can be present even when placement failed; it does not establish
    acceptance. Always check :attr:`is_business_failure`.
    """

    __schema__ = "PlaceOrderResponse"

    error_code: PlaceOrderErrorCode = jfield("errorCode", enum(PlaceOrderErrorCode))
    order_id: int | None = jfield("orderId", INT, default=None)


@model
class CancelOrderResponse(ResponseEnvelope):
    """Response of ``Order_CancelOrder``."""

    __schema__ = "CancelOrderResponse"

    error_code: CancelOrderErrorCode = jfield("errorCode", enum(CancelOrderErrorCode))


@model
class ModifyOrderResponse(ResponseEnvelope):
    """Response of ``Order_ModifyOrder``."""

    __schema__ = "ModifyOrderResponse"

    error_code: ModifyOrderErrorCode = jfield("errorCode", enum(ModifyOrderErrorCode))


@model
class SearchPositionResponse(ResponseEnvelope):
    """Response of ``Position_SearchOpenPositions``."""

    __schema__ = "SearchPositionResponse"

    error_code: SearchPositionErrorCode = jfield("errorCode", enum(SearchPositionErrorCode))
    positions: tuple[Position, ...] = jfield("positions", array(nested(Position)), default=())


@model
class ClosePositionResponse(ResponseEnvelope):
    """Response of ``Position_CloseContractPosition``."""

    __schema__ = "ClosePositionResponse"

    error_code: ClosePositionErrorCode = jfield("errorCode", enum(ClosePositionErrorCode))


@model
class PartialClosePositionResponse(ResponseEnvelope):
    """Response of ``Position_PartialCloseContractPosition``."""

    __schema__ = "PartialClosePositionResponse"

    error_code: PartialClosePositionErrorCode = jfield(
        "errorCode", enum(PartialClosePositionErrorCode)
    )


@model
class SearchHalfTradeResponse(ResponseEnvelope):
    """Response of ``Trade_SearchHalfTurnTrades``."""

    __schema__ = "SearchHalfTradeResponse"

    error_code: SearchTradeErrorCode = jfield("errorCode", enum(SearchTradeErrorCode))
    trades: tuple[HalfTrade, ...] = jfield("trades", array(nested(HalfTrade)), default=())


# ===========================================================================
# Requests
# ===========================================================================


@model
class SearchAccountRequest(RequestModel[SearchAccountResponse]):
    """Body of ``Account_SearchAccounts``.

    Attributes:
        only_active_accounts: Return only active accounts (default ``True``).
    """

    __schema__ = "SearchAccountRequest"
    __operation_id__ = "Account_SearchAccounts"

    only_active_accounts: bool = jfield("onlyActiveAccounts", BOOL, default=True)


@model
class LoginApiKeyRequest(RequestModel[LoginResponse]):
    """Body of ``Auth_LoginKey``. ``api_key`` is redacted in repr.

    Attributes:
        user_name: Platform sign-in username (case-insensitive; not an e-mail
            address or account label).
        api_key: API key issued in the firm's trading platform.
    """

    __schema__ = "LoginApiKeyRequest"
    __operation_id__ = "Auth_LoginKey"

    user_name: str = jfield("userName", STR)
    api_key: str = jfield("apiKey", STR, secret=True)


@model
class LoginAppRequest(RequestModel[LoginResponse]):
    """Body of ``Auth_LoginApp``.

    Application login is present in Swagger only; **the current authentication
    guide does not support it**. Use API-key login (``Auth_LoginKey``) instead.
    ``password`` and ``verify_key`` are redacted in repr.

    Attributes:
        user_name: Username.
        password: Password.
        device_id: Device id.
        app_id: Application id.
        verify_key: Application verification key.
    """

    __schema__ = "LoginAppRequest"
    __operation_id__ = "Auth_LoginApp"

    user_name: str = jfield("userName", STR)
    password: str = jfield("password", STR, secret=True)
    device_id: str = jfield("deviceId", STR)
    app_id: str = jfield("appId", STR)
    verify_key: str = jfield("verifyKey", STR, secret=True)


@model
class SearchContractRequest(RequestModel[SearchContractResponse]):
    """Body of ``Contract_SearchContracts`` (returns at most 20 contracts).

    Attributes:
        search_text: Contract name to search for.
        live: Selects the live data subscription (default ``False``). It does not
            indicate whether an account is simulated.
    """

    __schema__ = "SearchContractRequest"
    __operation_id__ = "Contract_SearchContracts"

    search_text: str = jfield("searchText", STR)
    live: bool = jfield("live", BOOL, default=False)


@model
class SearchContractByIdRequest(RequestModel[SearchContractByIdResponse]):
    """Body of ``Contract_SearchContractById``.

    Attributes:
        contract_id: The contract id.
    """

    __schema__ = "SearchContractByIdRequest"
    __operation_id__ = "Contract_SearchContractById"

    contract_id: str = jfield("contractId", STR)


@model
class ListAvailableContractRequest(RequestModel[ListAvailableContractResponse]):
    """Body of ``Contract_AvailableContracts``.

    Attributes:
        live: Selects the live data subscription (default ``False``).
    """

    __schema__ = "ListAvailableContractRequest"
    __operation_id__ = "Contract_AvailableContracts"

    live: bool = jfield("live", BOOL, default=False)


@model
class RetrieveBarRequest(RequestModel[RetrieveBarResponse]):
    """Body of ``History_GetBars``.

    Example::

        RetrieveBarRequest(
            contract_id="CON.F.US.EP.U25",
            start_time=datetime(2025, 7, 1, tzinfo=timezone.utc),
            end_time=datetime(2025, 7, 2, tzinfo=timezone.utc),
            unit=AggregateBarUnit.Minute,
            unit_number=5,
        )

    Attributes:
        contract_id: Contract id (a string, despite the guide's integer typo).
        live: Selects the live data subscription (default ``False``).
        start_time: Start of the range.
        end_time: End of the range.
        unit: Aggregation unit. The guide documents 1-6; 0 and 7 are Swagger-only.
        unit_number: Number of units per bar (default 1).
        limit: Maximum number of bars; at most 20,000 (default 20,000).
        include_partial_bar: Include the current, incomplete bar (default ``False``).
    """

    __schema__ = "RetrieveBarRequest"
    __operation_id__ = "History_GetBars"

    contract_id: str = jfield("contractId", STR)
    live: bool = jfield("live", BOOL, default=False)
    start_time: datetime = jfield("startTime", TIMESTAMP)
    end_time: datetime = jfield("endTime", TIMESTAMP)
    unit: AggregateBarUnit = jfield("unit", enum(AggregateBarUnit))
    unit_number: int = jfield("unitNumber", INT, default=1)
    limit: int = jfield("limit", INT, default=MAX_BARS)
    include_partial_bar: bool = jfield("includePartialBar", BOOL, default=False)


@model
class SearchOrderRequest(RequestModel[SearchOrderResponse]):
    """Body of ``Order_SearchOrders``: orders created within a time range.

    Attributes:
        account_id: Account id.
        start_timestamp: Start of the creation-time range.
        end_timestamp: End of the range; open-ended when ``None``.
    """

    __schema__ = "SearchOrderRequest"
    __operation_id__ = "Order_SearchOrders"

    account_id: int = jfield("accountId", INT)
    start_timestamp: datetime = jfield("startTimestamp", TIMESTAMP)
    end_timestamp: datetime | None = jfield("endTimestamp", TIMESTAMP, default=None)


@model
class SearchOrderByIdRequest(RequestModel[SearchOrderByIdResponse]):
    """Body of ``Order_SearchOrderById`` (Swagger-only operation).

    Attributes:
        account_id: Account id; it must belong to the caller.
        order_id: Order id.
    """

    __schema__ = "SearchOrderByIdRequest"
    __operation_id__ = "Order_SearchOrderById"

    account_id: int = jfield("accountId", INT)
    order_id: int = jfield("orderId", INT)


@model
class SearchOpenOrderRequest(RequestModel[SearchOrderResponse]):
    """Body of ``Order_SearchOpenOrders``.

    Attributes:
        account_id: Account id.
    """

    __schema__ = "SearchOpenOrderRequest"
    __operation_id__ = "Order_SearchOpenOrders"

    account_id: int = jfield("accountId", INT)


@model
class SearchOrdersQueryRequest(RequestModel[SearchOrdersQueryResponse]):
    """Body of ``Order_SearchOrdersQuery`` (Swagger-only operation).

    Pagination defaults and bounds are not published; none are invented here.

    Attributes:
        filter: The order filter (required).
        page_size: Page size.
        page_offset: Page offset.
        sort_by: Sort field.
        sort_direction: Sort direction.
        include_total_count: Ask the server to return ``totalCount``.
    """

    __schema__ = "SearchOrdersQueryRequest"
    __operation_id__ = "Order_SearchOrdersQuery"

    filter: GatewayOrderFilter = jfield("filter", nested(GatewayOrderFilter))
    page_size: int | None = jfield("pageSize", INT, default=None)
    page_offset: int | None = jfield("pageOffset", INT, default=None)
    sort_by: OrderSortField | None = jfield("sortBy", enum(OrderSortField), default=None)
    sort_direction: SortDirection | None = jfield(
        "sortDirection", enum(SortDirection), default=None
    )
    include_total_count: bool | None = jfield("includeTotalCount", BOOL, default=None)


@model
class PlaceOrderRequest(RequestModel[PlaceOrderResponse]):
    """Body of ``Order_PlaceOrder``.

    **Orders are real**: develop against simulated accounts.

    Prefer the typed constructors :meth:`market`, :meth:`limit`, :meth:`stop`,
    :meth:`trailing_stop`, :meth:`join_bid` and :meth:`join_ask`, and
    :meth:`with_custom_tag` / :meth:`with_brackets`::

        req = PlaceOrderRequest.limit(123, "CON.F.US.EP.U25", OrderSide.Bid, 1, "6000.25")
        req = req.with_brackets(
            stop_loss=PlaceOrderBracket(ticks=8, type=OrderType.Stop),
            take_profit=PlaceOrderBracket(ticks=16, type=OrderType.Limit),
        )

    Client-side validation (when enabled) rejects types other than Limit (1),
    Market (2), Stop (4), TrailingStop (5), JoinBid (6) and JoinAsk (7), a
    trailing stop without ``trail_price``, and bracket types outside that set.

    Attributes:
        account_id: Account id.
        contract_id: Contract id.
        type: Order type.
        side: ``OrderSide.Bid`` buys, ``OrderSide.Ask`` sells.
        size: Quantity.
        limit_price: Limit price (limit orders).
        stop_price: Stop price (stop orders).
        trail_price: For a trailing stop (type 5): the **absolute** stop price,
            aligned to the tick size; never a distance. The server converts the
            gap from the last price into whole ticks and rejects initial trails
            above 1,000 ticks. Compare :attr:`Order.trail_price`, which is a
            distance.
        custom_tag: Optional tag; it must be unique within the account.
        stop_loss_bracket: Optional stop-loss bracket (Auto OCO Brackets mode).
        take_profit_bracket: Optional take-profit bracket (Auto OCO Brackets mode).
    """

    __schema__ = "PlaceOrderRequest"
    __operation_id__ = "Order_PlaceOrder"

    account_id: int = jfield("accountId", INT)
    contract_id: str = jfield("contractId", STR)
    type: OrderType = jfield("type", enum(OrderType))
    side: OrderSide = jfield("side", enum(OrderSide))
    size: int = jfield("size", INT)
    limit_price: Decimal | None = jfield("limitPrice", DECIMAL, default=None)
    stop_price: Decimal | None = jfield("stopPrice", DECIMAL, default=None)
    trail_price: Decimal | None = jfield("trailPrice", DECIMAL, default=None)
    custom_tag: str | None = jfield("customTag", STR, default=None)
    stop_loss_bracket: PlaceOrderBracket | None = jfield(
        "stopLossBracket", nested(PlaceOrderBracket), default=None
    )
    take_profit_bracket: PlaceOrderBracket | None = jfield(
        "takeProfitBracket", nested(PlaceOrderBracket), default=None
    )

    @classmethod
    def _build(
        cls,
        order_type: OrderType,
        account_id: int,
        contract_id: str,
        side: OrderSide | int,
        size: int,
        *,
        limit_price: Number | None = None,
        stop_price: Number | None = None,
        trail_price: Number | None = None,
        custom_tag: str | None = None,
        stop_loss: PlaceOrderBracket | None = None,
        take_profit: PlaceOrderBracket | None = None,
    ) -> PlaceOrderRequest:
        return cls(
            account_id=account_id,
            contract_id=contract_id,
            type=order_type,
            side=OrderSide(side),
            size=size,
            limit_price=_dec(limit_price),
            stop_price=_dec(stop_price),
            trail_price=_dec(trail_price),
            custom_tag=custom_tag,
            stop_loss_bracket=stop_loss,
            take_profit_bracket=take_profit,
        )

    @classmethod
    def market(
        cls,
        account_id: int,
        contract_id: str,
        side: OrderSide | int,
        size: int,
        *,
        custom_tag: str | None = None,
        stop_loss: PlaceOrderBracket | None = None,
        take_profit: PlaceOrderBracket | None = None,
    ) -> PlaceOrderRequest:
        """A market order."""
        return cls._build(
            OrderType.Market,
            account_id,
            contract_id,
            side,
            size,
            custom_tag=custom_tag,
            stop_loss=stop_loss,
            take_profit=take_profit,
        )

    @classmethod
    def limit(
        cls,
        account_id: int,
        contract_id: str,
        side: OrderSide | int,
        size: int,
        limit_price: Number,
        *,
        custom_tag: str | None = None,
        stop_loss: PlaceOrderBracket | None = None,
        take_profit: PlaceOrderBracket | None = None,
    ) -> PlaceOrderRequest:
        """A limit order at ``limit_price``."""
        return cls._build(
            OrderType.Limit,
            account_id,
            contract_id,
            side,
            size,
            limit_price=limit_price,
            custom_tag=custom_tag,
            stop_loss=stop_loss,
            take_profit=take_profit,
        )

    @classmethod
    def stop(
        cls,
        account_id: int,
        contract_id: str,
        side: OrderSide | int,
        size: int,
        stop_price: Number,
        *,
        custom_tag: str | None = None,
        stop_loss: PlaceOrderBracket | None = None,
        take_profit: PlaceOrderBracket | None = None,
    ) -> PlaceOrderRequest:
        """A stop order triggered at ``stop_price``."""
        return cls._build(
            OrderType.Stop,
            account_id,
            contract_id,
            side,
            size,
            stop_price=stop_price,
            custom_tag=custom_tag,
            stop_loss=stop_loss,
            take_profit=take_profit,
        )

    @classmethod
    def trailing_stop(
        cls,
        account_id: int,
        contract_id: str,
        side: OrderSide | int,
        size: int,
        trail_price: Number,
        *,
        custom_tag: str | None = None,
        stop_loss: PlaceOrderBracket | None = None,
        take_profit: PlaceOrderBracket | None = None,
    ) -> PlaceOrderRequest:
        """A trailing stop at the **absolute** initial stop price ``trail_price``.

        ``trail_price`` is aligned to the tick size; it is not a distance.
        """
        return cls._build(
            OrderType.TrailingStop,
            account_id,
            contract_id,
            side,
            size,
            trail_price=trail_price,
            custom_tag=custom_tag,
            stop_loss=stop_loss,
            take_profit=take_profit,
        )

    @classmethod
    def join_bid(
        cls,
        account_id: int,
        contract_id: str,
        side: OrderSide | int,
        size: int,
        *,
        custom_tag: str | None = None,
        stop_loss: PlaceOrderBracket | None = None,
        take_profit: PlaceOrderBracket | None = None,
    ) -> PlaceOrderRequest:
        """A JoinBid order (type 6)."""
        return cls._build(
            OrderType.JoinBid,
            account_id,
            contract_id,
            side,
            size,
            custom_tag=custom_tag,
            stop_loss=stop_loss,
            take_profit=take_profit,
        )

    @classmethod
    def join_ask(
        cls,
        account_id: int,
        contract_id: str,
        side: OrderSide | int,
        size: int,
        *,
        custom_tag: str | None = None,
        stop_loss: PlaceOrderBracket | None = None,
        take_profit: PlaceOrderBracket | None = None,
    ) -> PlaceOrderRequest:
        """A JoinAsk order (type 7)."""
        return cls._build(
            OrderType.JoinAsk,
            account_id,
            contract_id,
            side,
            size,
            custom_tag=custom_tag,
            stop_loss=stop_loss,
            take_profit=take_profit,
        )

    def with_custom_tag(self, custom_tag: str | None) -> PlaceOrderRequest:
        """Return a copy with ``custom_tag`` set (unique within the account)."""
        return dataclasses.replace(self, custom_tag=custom_tag)

    def with_brackets(
        self,
        *,
        stop_loss: PlaceOrderBracket | None = None,
        take_profit: PlaceOrderBracket | None = None,
    ) -> PlaceOrderRequest:
        """Return a copy with the given stop-loss and/or take-profit brackets."""
        return dataclasses.replace(
            self,
            stop_loss_bracket=stop_loss if stop_loss is not None else self.stop_loss_bracket,
            take_profit_bracket=(
                take_profit if take_profit is not None else self.take_profit_bracket
            ),
        )


@model
class CancelOrderRequest(RequestModel[CancelOrderResponse]):
    """Body of ``Order_CancelOrder``.

    Cancellation is documented for simulated accounts that are not copy-trading
    followers; error code 6 covers unsupported live accounts and followers.

    Attributes:
        account_id: Account id.
        order_id: Order id.
    """

    __schema__ = "CancelOrderRequest"
    __operation_id__ = "Order_CancelOrder"

    account_id: int = jfield("accountId", INT)
    order_id: int = jfield("orderId", INT)


@model
class ModifyOrderRequest(RequestModel[ModifyOrderResponse]):
    """Body of ``Order_ModifyOrder``. Fields left ``None`` are omitted.

    Attributes:
        account_id: Account id.
        order_id: Order id.
        size: New size.
        limit_price: New limit price.
        stop_price: New stop price.
        trail_price: New **absolute** trailing-stop level (not a distance); the
            server recalculates the tick distance from its current last price.
    """

    __schema__ = "ModifyOrderRequest"
    __operation_id__ = "Order_ModifyOrder"

    account_id: int = jfield("accountId", INT)
    order_id: int = jfield("orderId", INT)
    size: int | None = jfield("size", INT, default=None)
    limit_price: Decimal | None = jfield("limitPrice", DECIMAL, default=None)
    stop_price: Decimal | None = jfield("stopPrice", DECIMAL, default=None)
    trail_price: Decimal | None = jfield("trailPrice", DECIMAL, default=None)


@model
class SearchPositionRequest(RequestModel[SearchPositionResponse]):
    """Body of ``Position_SearchOpenPositions``.

    Attributes:
        account_id: Account id.
    """

    __schema__ = "SearchPositionRequest"
    __operation_id__ = "Position_SearchOpenPositions"

    account_id: int = jfield("accountId", INT)


@model
class CloseContractPositionRequest(RequestModel[ClosePositionResponse]):
    """Body of ``Position_CloseContractPosition``.

    Attributes:
        account_id: Account id.
        contract_id: Contract whose position is closed.
    """

    __schema__ = "CloseContractPositionRequest"
    __operation_id__ = "Position_CloseContractPosition"

    account_id: int = jfield("accountId", INT)
    contract_id: str = jfield("contractId", STR)


@model
class PartialCloseContractPositionRequest(RequestModel[PartialClosePositionResponse]):
    """Body of ``Position_PartialCloseContractPosition``.

    Attributes:
        account_id: Account id.
        contract_id: Contract whose position is reduced.
        size: Quantity to close: at least 1 and no larger than the position.
    """

    __schema__ = "PartialCloseContractPositionRequest"
    __operation_id__ = "Position_PartialCloseContractPosition"

    account_id: int = jfield("accountId", INT)
    contract_id: str = jfield("contractId", STR)
    size: int = jfield("size", INT)


@model
class SearchTradeRequest(RequestModel[SearchHalfTradeResponse]):
    """Body of ``Trade_SearchHalfTurnTrades``.

    ``start_timestamp`` is required, following the guide (Swagger requires only
    ``accountId``).

    Attributes:
        account_id: Account id.
        start_timestamp: Start of the range.
        end_timestamp: End of the range; open-ended when ``None``.
    """

    __schema__ = "SearchTradeRequest"
    __operation_id__ = "Trade_SearchHalfTurnTrades"

    account_id: int = jfield("accountId", INT)
    start_timestamp: datetime = jfield("startTimestamp", TIMESTAMP)
    end_timestamp: datetime | None = jfield("endTimestamp", TIMESTAMP, default=None)


# ===========================================================================
# High-level results
# ===========================================================================


@dataclasses.dataclass(frozen=True, slots=True)
class OrderQueryResult:
    """Result of ``orders.query()``.

    Attributes:
        orders: The page of orders.
        total_count: Total number of matching orders, when requested and returned.
    """

    orders: list[Order]
    total_count: int | None


SCHEMA_MODELS: dict[str, type[Model]] = {
    cls.__schema__: cls
    for cls in (
        # objects
        TradingAccount,
        Contract,
        AggregateBar,
        Order,
        Position,
        HalfTrade,
        ProblemDetails,
        PlaceOrderBracket,
        GatewayOrderFilter,
        # responses
        SearchAccountResponse,
        LoginResponse,
        LogoutResponse,
        ValidateResponse,
        SearchContractResponse,
        SearchContractByIdResponse,
        ListAvailableContractResponse,
        RetrieveBarResponse,
        SearchOrderResponse,
        SearchOrderByIdResponse,
        SearchOrdersQueryResponse,
        PlaceOrderResponse,
        CancelOrderResponse,
        ModifyOrderResponse,
        SearchPositionResponse,
        ClosePositionResponse,
        PartialClosePositionResponse,
        SearchHalfTradeResponse,
        # requests
        SearchAccountRequest,
        LoginApiKeyRequest,
        LoginAppRequest,
        SearchContractRequest,
        SearchContractByIdRequest,
        ListAvailableContractRequest,
        RetrieveBarRequest,
        SearchOrderRequest,
        SearchOrderByIdRequest,
        SearchOpenOrderRequest,
        SearchOrdersQueryRequest,
        PlaceOrderRequest,
        CancelOrderRequest,
        ModifyOrderRequest,
        SearchPositionRequest,
        CloseContractPositionRequest,
        PartialCloseContractPositionRequest,
        SearchTradeRequest,
    )
}
"""Every object schema of the OpenAPI specification, keyed by schema name."""
