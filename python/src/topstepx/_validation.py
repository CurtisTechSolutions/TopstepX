"""Client-side request validation (``SDK_BEHAVIOR.md`` section 9).

Internal module. Exactly five rules, each reproducing a specification
constraint and a negative case of ``validation-report.json``; no others are
invented. Validation runs before any network I/O, including login.
"""

from __future__ import annotations

from .errors import ValidationError
from .models import (
    MAX_BARS,
    PartialCloseContractPositionRequest,
    PlaceOrderBracket,
    PlaceOrderRequest,
    RetrieveBarRequest,
)

PLACEABLE_ORDER_TYPES = frozenset({1, 2, 4, 5, 6, 7})
"""Limit, Market, Stop, TrailingStop, JoinBid and JoinAsk."""
_TRAILING_STOP = 5


def _as_int(value: object) -> int | None:
    if isinstance(value, bool) or not isinstance(value, int):
        return None
    return int(value)


def _check_type(value: object, what: str, violations: list[str]) -> None:
    number = _as_int(value)
    if number not in PLACEABLE_ORDER_TYPES:
        violations.append(
            f"{what} must be Limit (1), Market (2), Stop (4), TrailingStop (5), "
            f"JoinBid (6) or JoinAsk (7), got {value!r}"
        )


def _check_bracket(bracket: PlaceOrderBracket | None, what: str, violations: list[str]) -> None:
    if bracket is not None:
        _check_type(bracket.type, f"{what}.type", violations)


def validate_request(request: object) -> None:
    """Validate ``request`` against the specification's client-checkable rules.

    1. ``PlaceOrderRequest.type`` must be 1, 2, 4, 5, 6 or 7 (StopLimit, Unknown
       and unrecognized values are rejected).
    2. ``PlaceOrderRequest.type == 5`` requires a non-null ``trail_price``.
    3. Bracket types must come from the same six values.
    4. ``RetrieveBarRequest.limit <= 20000``.
    5. ``PartialCloseContractPositionRequest.size >= 1``.

    Raises:
        ValidationError: Listing every violated rule.
    """
    violations: list[str] = []
    if isinstance(request, PlaceOrderRequest):
        _check_type(request.type, "type", violations)
        if _as_int(request.type) == _TRAILING_STOP and request.trail_price is None:
            violations.append("a trailing-stop order (type 5) requires trail_price")
        _check_bracket(request.stop_loss_bracket, "stop_loss_bracket", violations)
        _check_bracket(request.take_profit_bracket, "take_profit_bracket", violations)
    elif isinstance(request, RetrieveBarRequest):
        limit = _as_int(request.limit)
        if limit is not None and limit > MAX_BARS:
            violations.append(f"limit cannot exceed {MAX_BARS} bars, got {limit}")
    elif isinstance(request, PartialCloseContractPositionRequest):
        size = _as_int(request.size)
        if size is not None and size < 1:
            violations.append(f"size must be at least 1, got {size}")
    if violations:
        raise ValidationError(type(request).__name__, violations)
