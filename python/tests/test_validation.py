"""Client-side validation (SDK_BEHAVIOR.md section 9, validation-report.json)."""

from __future__ import annotations

import json
from datetime import datetime, timezone
from decimal import Decimal
from pathlib import Path
from typing import Any

import pytest

from topstepx import (
    AggregateBarUnit,
    OrderSide,
    OrderType,
    PartialCloseContractPositionRequest,
    PlaceOrderBracket,
    PlaceOrderRequest,
    RetrieveBarRequest,
    ValidationError,
)
from topstepx._validation import validate_request

REPORT_PATH = Path(__file__).resolve().parent.parent.parent / "spec" / "validation-report.json"
T0 = datetime(2025, 7, 1, tzinfo=timezone.utc)


def place(**overrides: Any) -> PlaceOrderRequest:
    fields: dict[str, Any] = {
        "account_id": 123,
        "contract_id": "CON.F.US.EP.U25",
        "type": OrderType.Market,
        "side": OrderSide.Bid,
        "size": 1,
    }
    fields.update(overrides)
    return PlaceOrderRequest(**fields)


def bars(limit: int) -> RetrieveBarRequest:
    return RetrieveBarRequest(
        contract_id="C", start_time=T0, end_time=T0, unit=AggregateBarUnit.Minute, limit=limit
    )


NEGATIVE_CASES: dict[str, Any] = {
    "trailing-stop requires trailPrice": lambda: place(type=OrderType.TrailingStop),
    "trailing-stop rejects null trailPrice": lambda: PlaceOrderRequest.from_dict(
        {
            "accountId": 123,
            "contractId": "C",
            "type": 5,
            "side": 1,
            "size": 1,
            "trailPrice": None,
        }
    ),
    "placement rejects unsupported StopLimit": lambda: place(type=OrderType.StopLimit),
    "bar count cannot exceed 20000": lambda: bars(20_001),
    "partial close rejects zero": lambda: PartialCloseContractPositionRequest(
        account_id=1, contract_id="C", size=0
    ),
}


def test_every_negative_case_of_the_validation_report_is_covered() -> None:
    report = json.loads(REPORT_PATH.read_text(encoding="utf-8"))
    names = {case["name"] for case in report["negativeCases"]}
    assert names == set(NEGATIVE_CASES)


@pytest.mark.parametrize("name", sorted(NEGATIVE_CASES))
def test_negative_case_is_rejected(name: str) -> None:
    request = NEGATIVE_CASES[name]()
    with pytest.raises(ValidationError) as info:
        validate_request(request)
    assert info.value.request_type == type(request).__name__
    assert info.value.violations
    assert isinstance(info.value, ValueError)


@pytest.mark.parametrize("order_type", [0, 3, 8, -1, 99])
def test_unsupported_and_unrecognized_types_are_rejected(order_type: int) -> None:
    with pytest.raises(ValidationError):
        validate_request(place(type=OrderType(order_type)))


@pytest.mark.parametrize("bracket_type", [OrderType.Unknown, OrderType.StopLimit, OrderType(12)])
def test_bracket_types_are_restricted(bracket_type: OrderType) -> None:
    with pytest.raises(ValidationError, match="stop_loss_bracket"):
        validate_request(place(stop_loss_bracket=PlaceOrderBracket(ticks=8, type=bracket_type)))
    with pytest.raises(ValidationError, match="take_profit_bracket"):
        validate_request(place(take_profit_bracket=PlaceOrderBracket(ticks=8, type=bracket_type)))


def test_all_violations_are_reported_together() -> None:
    request = place(
        type=OrderType.StopLimit,
        stop_loss_bracket=PlaceOrderBracket(ticks=8, type=OrderType.Unknown),
    )
    with pytest.raises(ValidationError) as info:
        validate_request(request)
    assert len(info.value.violations) == 2


POSITIVE_CASES = [
    place(),
    PlaceOrderRequest.limit(1, "C", OrderSide.Bid, 1, Decimal("6000.25")),
    PlaceOrderRequest.stop(1, "C", OrderSide.Ask, 1, 5990),
    PlaceOrderRequest.trailing_stop(1, "C", OrderSide.Ask, 1, "5998.5"),
    PlaceOrderRequest.join_bid(1, "C", OrderSide.Bid, 1),
    PlaceOrderRequest.join_ask(1, "C", OrderSide.Ask, 1),
    PlaceOrderRequest.market(
        1,
        "C",
        OrderSide.Bid,
        1,
        stop_loss=PlaceOrderBracket(ticks=8, type=OrderType.Stop),
        take_profit=PlaceOrderBracket(ticks=16, type=OrderType.Limit),
    ),
    bars(20_000),
    bars(1),
    PartialCloseContractPositionRequest(account_id=1, contract_id="C", size=1),
]


@pytest.mark.parametrize("request_model", POSITIVE_CASES, ids=repr)
def test_valid_requests_pass(request_model: Any) -> None:
    validate_request(request_model)


def test_convenience_constructors_build_the_expected_requests() -> None:
    req = PlaceOrderRequest.trailing_stop(7, "C", 1, 2, 5998.5, custom_tag="tag-1")
    assert req.type is OrderType.TrailingStop
    assert req.side is OrderSide.Ask
    assert req.trail_price == Decimal("5998.5")
    assert req.custom_tag == "tag-1"
    limit = PlaceOrderRequest.limit(7, "C", OrderSide.Bid, 1, "6000.25")
    assert limit.to_dict() == {
        "accountId": 7,
        "contractId": "C",
        "type": 1,
        "side": 0,
        "size": 1,
        "limitPrice": Decimal("6000.25"),
    }
    stop_loss = PlaceOrderBracket(ticks=8, type=OrderType.Stop)
    take_profit = PlaceOrderBracket(ticks=16, type=OrderType.Limit)
    tagged = limit.with_custom_tag("abc").with_brackets(stop_loss=stop_loss)
    both = tagged.with_brackets(take_profit=take_profit)
    assert both.custom_tag == "abc"
    assert both.stop_loss_bracket == stop_loss
    assert both.take_profit_bracket == take_profit
    assert limit.custom_tag is None  # immutable: copies were returned
    assert both.to_json().endswith(
        '"customTag":"abc","stopLossBracket":{"ticks":8,"type":4},'
        '"takeProfitBracket":{"ticks":16,"type":1}}'
    )


def test_constructors_reject_non_finite_prices() -> None:
    with pytest.raises(ValueError, match="finite"):
        PlaceOrderRequest.limit(1, "C", OrderSide.Bid, 1, float("nan"))
    with pytest.raises(ValueError, match="decimal"):
        PlaceOrderRequest.limit(1, "C", OrderSide.Bid, 1, "abc")
