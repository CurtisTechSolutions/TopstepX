"""Integer enums of the ProjectX Gateway API.

Every enum is an :class:`~enum.IntEnum` whose member names are exactly the
specification's ``x-enumNames``, so ``OrderType.Limit.name == "Limit"`` and
``OrderType(1) is OrderType.Limit``.

Decoding never fails on a value the enum does not know: ``OrderType(42)`` returns
a pseudo-member that compares equal to ``42``, whose ``name`` is
``"Unrecognized(42)"`` and whose :attr:`SpecEnum.is_recognized` is ``False``.
Pseudo-members are cached, pickle, and are not part of iteration or ``len()``.

``OrderStatus`` value ``0`` is named ``"None"``, which is not a valid Python
attribute name; use ``OrderStatus.NONE`` (an alias) or ``OrderStatus["None"]``.
"""

from __future__ import annotations

import threading
from enum import IntEnum
from typing import Any

__all__ = [
    "SpecEnum",
    # domain enums
    "AggregateBarUnit",
    "OrderSide",
    "OrderSortField",
    "OrderStatus",
    "OrderType",
    "PositionType",
    "SortDirection",
    # realtime-only enums
    "DomType",
    "TradeLogType",
    # error-code enums
    "CancelOrderErrorCode",
    "ClosePositionErrorCode",
    "ListAvailableContractErrorCode",
    "LoginErrorCode",
    "LogoutErrorCode",
    "ModifyOrderErrorCode",
    "PartialClosePositionErrorCode",
    "PlaceOrderErrorCode",
    "RetrieveBarErrorCode",
    "SearchAccountErrorCode",
    "SearchContractByIdErrorCode",
    "SearchContractErrorCode",
    "SearchOrderByIdErrorCode",
    "SearchOrderErrorCode",
    "SearchPositionErrorCode",
    "SearchTradeErrorCode",
    "ValidateErrorCode",
    "SCHEMA_ENUMS",
]

_PSEUDO_CACHE_LIMIT = 4096
_pseudo_cache: dict[tuple[type[SpecEnum], int], SpecEnum] = {}
_pseudo_lock = threading.Lock()


class SpecEnum(IntEnum):
    """Base class of every SDK enum: an ``IntEnum`` that tolerates unknown values."""

    @classmethod
    def _missing_(cls, value: object) -> Any:
        if not isinstance(value, int) or isinstance(value, bool):
            return None
        number = int(value)
        key = (cls, number)
        member = _pseudo_cache.get(key)
        if member is not None:
            return member
        member = int.__new__(cls, number)
        member._name_ = f"Unrecognized({number})"
        member._value_ = number
        with _pseudo_lock:
            if len(_pseudo_cache) < _PSEUDO_CACHE_LIMIT:
                member = _pseudo_cache.setdefault(key, member)
        return member

    @property
    def is_recognized(self) -> bool:
        """``False`` for a pseudo-member created for a value the enum does not define."""
        return self._name_ in type(self).__members__


# ---------------------------------------------------------------------------
# Domain enums
# ---------------------------------------------------------------------------


class AggregateBarUnit(SpecEnum):
    """Bar aggregation unit (``AggregateBarUnit``).

    The guide documents 1-6; ``Unspecified`` (0) and ``Tick`` (7) appear only in
    Swagger and their acceptance is unverified.
    """

    Unspecified = 0
    Second = 1
    Minute = 2
    Hour = 3
    Day = 4
    Week = 5
    Month = 6
    Tick = 7


class OrderStatus(SpecEnum):
    """Order status (``OrderStatus``). Value 0 is named ``"None"``; use ``NONE``."""

    locals()["None"] = 0  # "None" is a keyword; defined through the class namespace.
    Open = 1
    Filled = 2
    Cancelled = 3
    Expired = 4
    Rejected = 5
    Pending = 6
    PendingCancellation = 7
    Suspended = 8
    NONE = 0  # alias of the member named "None"


class OrderType(SpecEnum):
    """Order type (``OrderType``).

    Placement accepts only Limit, Market, Stop, TrailingStop, JoinBid and JoinAsk;
    ``Unknown`` and ``StopLimit`` appear in responses only.
    """

    Unknown = 0
    Limit = 1
    Market = 2
    StopLimit = 3
    Stop = 4
    TrailingStop = 5
    JoinBid = 6
    JoinAsk = 7


class OrderSide(SpecEnum):
    """Order side (``OrderSide``): ``Bid`` buys, ``Ask`` sells."""

    Bid = 0
    Ask = 1


class PositionType(SpecEnum):
    """Position direction (``PositionType``)."""

    Undefined = 0
    Long = 1
    Short = 2


class OrderSortField(SpecEnum):
    """Sort field for ``Order_SearchOrdersQuery`` (``OrderSortField``)."""

    CreatedAt = 0
    Id = 1


class SortDirection(SpecEnum):
    """Sort direction for ``Order_SearchOrdersQuery`` (``SortDirection``)."""

    Asc = 0
    Desc = 1


# ---------------------------------------------------------------------------
# Realtime-only enums (from the official realtime guide, not the OpenAPI spec)
# ---------------------------------------------------------------------------


class DomType(SpecEnum):
    """Market-depth entry type (``DomType``) used by ``GatewayDepth`` events."""

    Unknown = 0
    Ask = 1
    Bid = 2
    BestAsk = 3
    BestBid = 4
    Trade = 5
    Reset = 6
    Low = 7
    High = 8
    NewBestBid = 9
    NewBestAsk = 10
    Fill = 11


class TradeLogType(SpecEnum):
    """Market trade aggressor side (``TradeLogType``) used by ``GatewayTrade`` events."""

    Buy = 0
    Sell = 1


# ---------------------------------------------------------------------------
# Error-code enums (one per operation; SearchOrderErrorCode is shared)
# ---------------------------------------------------------------------------


class SearchAccountErrorCode(SpecEnum):
    """Error codes of ``Account_SearchAccounts``."""

    Success = 0


class LoginErrorCode(SpecEnum):
    """Error codes of ``Auth_LoginKey`` and ``Auth_LoginApp``."""

    Success = 0
    UserNotFound = 1
    PasswordVerificationFailed = 2
    InvalidCredentials = 3
    AppNotFound = 4
    AppVerificationFailed = 5
    InvalidDevice = 6
    AgreementsNotSigned = 7
    UnknownError = 8
    ApiSubscriptionNotFound = 9
    ApiKeyAuthenticationDisabled = 10


class LogoutErrorCode(SpecEnum):
    """Error codes of ``Auth_Logout``."""

    Success = 0
    InvalidSession = 1
    UnknownError = 2


class ValidateErrorCode(SpecEnum):
    """Error codes of ``Auth_Validate``."""

    Success = 0
    InvalidSession = 1
    SessionNotFound = 2
    ExpiredToken = 3
    UnknownError = 4


class SearchContractErrorCode(SpecEnum):
    """Error codes of ``Contract_SearchContracts``."""

    Success = 0


class SearchContractByIdErrorCode(SpecEnum):
    """Error codes of ``Contract_SearchContractById``."""

    Success = 0
    ContractNotFound = 1


class ListAvailableContractErrorCode(SpecEnum):
    """Error codes of ``Contract_AvailableContracts``."""

    Success = 0


class RetrieveBarErrorCode(SpecEnum):
    """Error codes of ``History_GetBars``."""

    Success = 0
    ContractNotFound = 1
    UnitInvalid = 2
    UnitNumberInvalid = 3
    LimitInvalid = 4


class SearchOrderErrorCode(SpecEnum):
    """Error codes shared by the three order-search operations.

    Used by ``Order_SearchOrders``, ``Order_SearchOpenOrders`` and
    ``Order_SearchOrdersQuery``.
    """

    Success = 0
    AccountNotFound = 1


class SearchOrderByIdErrorCode(SpecEnum):
    """Error codes of ``Order_SearchOrderById``."""

    Success = 0
    OrderNotFound = 1


class PlaceOrderErrorCode(SpecEnum):
    """Error codes of ``Order_PlaceOrder``."""

    Success = 0
    AccountNotFound = 1
    OrderRejected = 2
    InsufficientFunds = 3
    AccountViolation = 4
    OutsideTradingHours = 5
    OrderPending = 6
    UnknownError = 7
    ContractNotFound = 8
    ContractNotActive = 9
    AccountRejected = 10


class CancelOrderErrorCode(SpecEnum):
    """Error codes of ``Order_CancelOrder``."""

    Success = 0
    AccountNotFound = 1
    OrderNotFound = 2
    Rejected = 3
    Pending = 4
    UnknownError = 5
    AccountRejected = 6


class ModifyOrderErrorCode(SpecEnum):
    """Error codes of ``Order_ModifyOrder``."""

    Success = 0
    AccountNotFound = 1
    OrderNotFound = 2
    Rejected = 3
    Pending = 4
    UnknownError = 5
    AccountRejected = 6
    ContractNotFound = 7


class SearchPositionErrorCode(SpecEnum):
    """Error codes of ``Position_SearchOpenPositions``."""

    Success = 0
    AccountNotFound = 1


class ClosePositionErrorCode(SpecEnum):
    """Error codes of ``Position_CloseContractPosition``."""

    Success = 0
    AccountNotFound = 1
    PositionNotFound = 2
    ContractNotFound = 3
    ContractNotActive = 4
    OrderRejected = 5
    OrderPending = 6
    UnknownError = 7
    AccountRejected = 8


class PartialClosePositionErrorCode(SpecEnum):
    """Error codes of ``Position_PartialCloseContractPosition``."""

    Success = 0
    AccountNotFound = 1
    PositionNotFound = 2
    ContractNotFound = 3
    ContractNotActive = 4
    InvalidCloseSize = 5
    OrderRejected = 6
    OrderPending = 7
    UnknownError = 8
    AccountRejected = 9


class SearchTradeErrorCode(SpecEnum):
    """Error codes of ``Trade_SearchHalfTurnTrades``."""

    Success = 0
    AccountNotFound = 1


SCHEMA_ENUMS: dict[str, type[SpecEnum]] = {
    cls.__name__: cls
    for cls in (
        SearchAccountErrorCode,
        LoginErrorCode,
        LogoutErrorCode,
        ValidateErrorCode,
        SearchContractErrorCode,
        SearchContractByIdErrorCode,
        ListAvailableContractErrorCode,
        RetrieveBarErrorCode,
        AggregateBarUnit,
        SearchOrderErrorCode,
        OrderStatus,
        OrderType,
        OrderSide,
        SearchOrderByIdErrorCode,
        OrderSortField,
        SortDirection,
        PlaceOrderErrorCode,
        CancelOrderErrorCode,
        ModifyOrderErrorCode,
        SearchPositionErrorCode,
        PositionType,
        ClosePositionErrorCode,
        PartialClosePositionErrorCode,
        SearchTradeErrorCode,
    )
}
"""Every enum of the OpenAPI specification, keyed by its schema name."""
