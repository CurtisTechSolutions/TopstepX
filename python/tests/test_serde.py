"""Decimals, timestamps, enums, models and small building blocks."""

from __future__ import annotations

import copy
import pickle
from datetime import date, datetime, timedelta, timezone
from decimal import Decimal

import pytest

from topstepx import (
    ApiError,
    Backoff,
    DecodeError,
    HTTPStatusError,
    OrderStatus,
    OrderType,
    RateLimit,
    RateLimitError,
    SpecEnum,
    ValidationError,
)
from topstepx._ratelimit import SlidingWindow
from topstepx._retry import parse_retry_after
from topstepx._serde import dumps, format_timestamp, loads, parse_timestamp
from topstepx.enums import LoginErrorCode, OrderSide, SearchOrderErrorCode
from topstepx.models import (
    AggregateBar,
    LoginApiKeyRequest,
    LoginAppRequest,
    LoginResponse,
    Order,
    ProblemDetails,
    SearchAccountResponse,
    TradingAccount,
    ValidateResponse,
)

UTC = timezone.utc

# ---------------------------------------------------------------------------
# Decimals
# ---------------------------------------------------------------------------


def test_json_numbers_decode_exactly() -> None:
    data = loads('{"a": 6000.25, "b": 0.1, "c": 12345678901234567890.123456789, "d": 7}')
    assert data["a"] == Decimal("6000.25")
    assert data["b"] == Decimal("0.1")
    assert data["c"] == Decimal("12345678901234567890.123456789")
    assert data["d"] == 7
    assert type(data["d"]) is int


@pytest.mark.parametrize(
    ("value", "text"),
    [
        (Decimal("6000.25"), "6000.25"),
        (Decimal("6000.250"), "6000.250"),
        (Decimal("1E+2"), "100"),
        (Decimal("1.5E-7"), "0.00000015"),
        (Decimal("-0.5"), "-0.5"),
        (Decimal("12345678901234567890.123456789"), "12345678901234567890.123456789"),
    ],
)
def test_decimals_encode_as_plain_json_numbers(value: Decimal, text: str) -> None:
    assert dumps({"p": value}) == '{"p":' + text + "}"
    assert loads(dumps(value)) == value


def test_non_finite_numbers_are_rejected() -> None:
    with pytest.raises(ValueError, match="finite"):
        dumps(Decimal("NaN"))
    with pytest.raises(ValueError, match="finite"):
        dumps(float("inf"))
    with pytest.raises(ValueError, match="NaN"):
        loads('{"a": NaN}')


def test_dumps_handles_enums_strings_and_nesting() -> None:
    text = dumps({"t": OrderType.Limit, "s": 'q"é', "l": [True, None, 1.5], "n": {}})
    assert text == '{"t":1,"s":"q\\"é","l":[true,null,1.5],"n":{}}'
    with pytest.raises(TypeError):
        dumps({"x": object()})
    with pytest.raises(TypeError):
        dumps({1: "non-string key"})


def test_model_decimal_fields_accept_numbers_and_numeric_strings() -> None:
    account = TradingAccount.from_dict(
        {
            "id": 1,
            "name": "A",
            "balance": "50000.10",
            "canTrade": True,
            "isVisible": True,
            "simulated": True,
        }
    )
    assert account.balance == Decimal("50000.10")
    with pytest.raises(DecodeError):
        TradingAccount.from_dict(
            {
                "id": 1,
                "name": "A",
                "balance": True,
                "canTrade": True,
                "isVisible": True,
                "simulated": True,
            }
        )


# ---------------------------------------------------------------------------
# Timestamps
# ---------------------------------------------------------------------------


@pytest.mark.parametrize(
    ("text", "expected"),
    [
        ("2025-07-01T14:00:00Z", datetime(2025, 7, 1, 14, tzinfo=UTC)),
        ("2025-07-01T14:00:00z", datetime(2025, 7, 1, 14, tzinfo=UTC)),
        ("2025-07-01T14:00:00", datetime(2025, 7, 1, 14, tzinfo=UTC)),  # missing offset = UTC
        ("2025-07-01T14:00:00+02:00", datetime(2025, 7, 1, 12, tzinfo=UTC)),
        ("2025-07-01T14:00:00-0530", datetime(2025, 7, 1, 19, 30, tzinfo=UTC)),
        ("2025-07-01T14:00:00+05", datetime(2025, 7, 1, 9, tzinfo=UTC)),
        ("2025-07-01T14:00:00.5Z", datetime(2025, 7, 1, 14, 0, 0, 500000, tzinfo=UTC)),
        ("2025-07-01T14:00:00.1234567Z", datetime(2025, 7, 1, 14, 0, 0, 123456, tzinfo=UTC)),
        ("2025-07-01T14:00:00.123456789+00:00", datetime(2025, 7, 1, 14, 0, 0, 123456, tzinfo=UTC)),
        ("2025-07-01T14:00:00,5Z", datetime(2025, 7, 1, 14, 0, 0, 500000, tzinfo=UTC)),
        ("2025-07-01 14:00:00Z", datetime(2025, 7, 1, 14, tzinfo=UTC)),
        ("2025-07-01", datetime(2025, 7, 1, tzinfo=UTC)),
        ("2016-12-31T23:59:60Z", datetime(2017, 1, 1, tzinfo=UTC)),  # leap second
    ],
)
def test_rfc3339_parsing(text: str, expected: datetime) -> None:
    parsed = parse_timestamp(text)
    assert parsed == expected
    assert parsed.tzinfo is UTC


@pytest.mark.parametrize(
    "text",
    ["", "yesterday", "2025-13-01T00:00:00Z", "2025-07-01T25:00:00Z", "2025-07-01T14:00:00+24:00"],
)
def test_invalid_timestamps_are_rejected(text: str) -> None:
    with pytest.raises(ValueError, match="timestamp"):
        parse_timestamp(text)


def test_timestamps_serialize_as_utc_with_z() -> None:
    assert format_timestamp(datetime(2025, 7, 1, 14, tzinfo=UTC)) == "2025-07-01T14:00:00Z"
    offset = timezone(timedelta(hours=-5))
    assert format_timestamp(datetime(2025, 7, 1, 9, 0, 0, 250000, tzinfo=offset)) == (
        "2025-07-01T14:00:00.250000Z"
    )
    assert format_timestamp(datetime(2025, 7, 1, 14)) == "2025-07-01T14:00:00Z"  # naive = UTC
    assert format_timestamp(date(2025, 7, 1)) == "2025-07-01T00:00:00Z"
    assert format_timestamp(datetime(5, 1, 2, tzinfo=UTC)) == "0005-01-02T00:00:00Z"


def test_timestamp_fields_decode_to_aware_utc() -> None:
    bar = AggregateBar.from_dict(
        {"t": "2025-07-01T14:00:00.1234567-04:00", "o": 1, "h": 2, "l": 0.5, "c": 1.5, "v": 3}
    )
    assert bar.timestamp == datetime(2025, 7, 1, 18, 0, 0, 123456, tzinfo=UTC)
    assert bar.to_dict()["t"] == "2025-07-01T18:00:00.123456Z"
    with pytest.raises(DecodeError) as info:
        AggregateBar.from_dict({"t": "nope", "o": 1, "h": 2, "l": 0.5, "c": 1.5, "v": 3})
    assert info.value.path == "t"


# ---------------------------------------------------------------------------
# Enums
# ---------------------------------------------------------------------------


def test_enum_names_match_the_spec() -> None:
    assert OrderType.Limit.name == "Limit"
    assert OrderType(3).name == "StopLimit"
    assert OrderStatus(0).name == "None"
    assert OrderStatus.NONE is OrderStatus["None"] is OrderStatus(0)
    assert [m.name for m in OrderStatus][:2] == ["None", "Open"]
    assert len(OrderStatus) == 9  # the NONE alias is not a separate member


def test_unrecognized_values_become_pseudo_members() -> None:
    unknown = OrderType(42)
    assert isinstance(unknown, OrderType)
    assert isinstance(unknown, SpecEnum)
    assert unknown == 42
    assert int(unknown) == 42
    assert unknown.value == 42
    assert unknown.name == "Unrecognized(42)"
    assert not unknown.is_recognized
    assert OrderType.Limit.is_recognized
    assert OrderType(42) is unknown  # cached
    assert unknown not in list(OrderType)
    assert len(OrderType) == 8
    assert hash(unknown) == hash(42)
    assert repr(unknown) == "<OrderType.Unrecognized(42): 42>"


def test_enums_pickle_and_copy() -> None:
    for member in (OrderType.Limit, OrderStatus(0), OrderType(42), LoginErrorCode(77)):
        restored = pickle.loads(pickle.dumps(member))
        assert restored == member
        assert restored is member
        assert type(restored) is type(member)
        assert copy.deepcopy(member) is member


def test_enums_reject_non_integers() -> None:
    with pytest.raises(ValueError, match="not a valid"):
        OrderType("Limit")
    with pytest.raises(ValueError, match="not a valid"):
        OrderType(1.5)


def test_enum_fields_decode_leniently() -> None:
    order = {
        "id": 1,
        "accountId": 2,
        "contractId": "C",
        "symbolId": "S",
        "creationTimestamp": "2025-07-01T14:00:00Z",
        "status": 42,
        "type": "1",
        "side": 1.0,
        "size": 1,
        "fillVolume": 0,
    }
    decoded = Order.from_dict(order)
    assert decoded.status == 42
    assert decoded.status.name == "Unrecognized(42)"
    assert decoded.type is OrderType.Limit
    assert decoded.side is OrderSide.Ask
    assert decoded.to_dict()["status"] == 42
    with pytest.raises(DecodeError):
        Order.from_dict({**order, "status": True})


def test_shared_error_code_enum() -> None:
    assert SearchOrderErrorCode(1).name == "AccountNotFound"


# ---------------------------------------------------------------------------
# Models
# ---------------------------------------------------------------------------


def test_unknown_members_are_ignored_and_missing_arrays_are_empty() -> None:
    response = SearchAccountResponse.from_dict({"success": True, "errorCode": 0, "brandNew": 1})
    assert response.accounts == ()
    assert response.has_business_failure is False
    assert response.error_message is None
    assert not response.is_business_failure


def test_required_fields_are_enforced() -> None:
    with pytest.raises(DecodeError) as info:
        SearchAccountResponse.from_dict({"success": True})
    assert info.value.path == "errorCode"
    with pytest.raises(DecodeError, match="must not be null"):
        SearchAccountResponse.from_dict({"success": None, "errorCode": 0})
    with pytest.raises(DecodeError):
        SearchAccountResponse.from_dict([])  # type: ignore[arg-type]


def test_member_names_fall_back_to_case_insensitive_matching() -> None:
    response = SearchAccountResponse.from_dict({"Success": True, "ErrorCode": 0})
    assert response.success is True


def test_int64_values_keep_full_precision() -> None:
    order_id = 2**63 - 1
    order = Order.from_json(
        f'{{"id": {order_id}, "accountId": 1, "contractId": "C", "symbolId": "S", '
        '"creationTimestamp": "2025-07-01T14:00:00Z", "status": 1, "type": 1, '
        '"side": 0, "size": 1, "fillVolume": 0}'
    )
    assert order.id == order_id
    assert f'"id":{order_id}' in order.to_json()


def test_models_are_immutable_hashable_and_picklable() -> None:
    account = TradingAccount(
        id=1, name="A", balance=Decimal("1.5"), can_trade=True, is_visible=True, simulated=False
    )
    with pytest.raises(AttributeError):
        account.name = "B"  # type: ignore[misc]
    assert hash(account) == hash(copy.copy(account))
    assert pickle.loads(pickle.dumps(account)) == account
    assert not hasattr(account, "__dict__")


def test_secrets_are_redacted_in_model_reprs() -> None:
    texts = [
        repr(LoginApiKeyRequest(user_name="me", api_key="KEY-SECRET")),
        repr(
            LoginAppRequest(
                user_name="me",
                password="PW-SECRET",
                device_id="d",
                app_id="a",
                verify_key="VK-SECRET",
            )
        ),
        repr(LoginResponse(success=True, error_code=LoginErrorCode.Success, token="TOKEN-SECRET")),
        repr(
            ValidateResponse.from_dict({"success": True, "errorCode": 0, "newToken": "NEW-SECRET"})
        ),
    ]
    for text in texts:
        assert "SECRET" not in text
        assert "<redacted>" in text
    assert "user_name='me'" in texts[0]


def test_problem_details_keeps_extensions() -> None:
    problem = ProblemDetails.from_dict(
        {"title": "Bad", "status": 400, "traceId": "x", "errors": {"a": ["b"]}}
    )
    assert problem.title == "Bad"
    assert problem.extensions == {"traceId": "x", "errors": {"a": ["b"]}}
    assert problem.to_dict() == {
        "title": "Bad",
        "status": 400,
        "traceId": "x",
        "errors": {"a": ["b"]},
    }


def test_from_json_rejects_invalid_text() -> None:
    with pytest.raises(DecodeError, match="invalid JSON"):
        TradingAccount.from_json("{")


# ---------------------------------------------------------------------------
# Errors
# ---------------------------------------------------------------------------


def test_errors_pickle_with_their_attributes() -> None:
    errors = [
        ApiError(
            operation_id="Op", path="/p", code=2, code_name="X", error_message="m", order_id=5
        ),
        HTTPStatusError(operation_id="Op", path="/p", status_code=500, body="b"),
        RateLimitError(operation_id="Op", path="/p", status_code=429, body="", retry_after=3.0),
        ValidationError("PlaceOrderRequest", ["bad"]),
        DecodeError("oops", path="a.b", body="{}", operation_id="Op"),
    ]
    for error in errors:
        restored = pickle.loads(pickle.dumps(error))
        assert type(restored) is type(error)
        assert str(restored) == str(error)
        assert restored.__dict__ == error.__dict__


def test_error_messages() -> None:
    error = ApiError(
        operation_id="Order_PlaceOrder",
        path="/api/Order/place",
        code=2,
        code_name="OrderRejected",
        error_message=None,
        order_id=457,
    )
    assert str(error) == "Order_PlaceOrder failed with errorCode 2 (OrderRejected) [orderId=457]"
    long_body = "x" * 2000
    http = HTTPStatusError(operation_id="Op", path="/p", status_code=500, body=long_body)
    assert len(str(http)) < 700
    assert http.body == long_body


# ---------------------------------------------------------------------------
# Retry-After, backoff and the sliding window
# ---------------------------------------------------------------------------


def test_retry_after_parsing() -> None:
    now = datetime(2025, 7, 1, 12, 0, 0, tzinfo=UTC)
    assert parse_retry_after("5", now) == 5.0
    assert parse_retry_after(" 2.5 ", now) == 2.5
    assert parse_retry_after("-3", now) == 0.0
    assert parse_retry_after("Tue, 01 Jul 2025 12:00:30 GMT", now) == 30.0
    assert parse_retry_after("Tue, 01 Jul 2025 11:00:00 GMT", now) == 0.0
    assert parse_retry_after(None, now) is None
    assert parse_retry_after("", now) is None
    assert parse_retry_after("soon", now) is None
    assert parse_retry_after("inf", now) is None


def test_backoff_doubles_and_caps() -> None:
    backoff = Backoff()
    assert [backoff.delay(n) for n in range(1, 8)] == [1, 2, 4, 8, 16, 30, 30]
    assert Backoff(initial=0.5, multiplier=3, maximum=5).delay(3) == 4.5
    assert Backoff().delay(10_000) == 30
    with pytest.raises(ValueError, match="backoff"):
        Backoff(multiplier=0.5)


def test_sliding_window() -> None:
    window = SlidingWindow(RateLimit(3, 10.0))
    assert [window.reserve(t) for t in (0.0, 1.0, 2.0)] == [0.0, 0.0, 0.0]
    assert window.reserve(3.0) == pytest.approx(7.0)  # until the entry at t=0 expires
    assert window.reserve(10.0) == 0.0
    assert window.reserve(10.5) == pytest.approx(0.5)
    assert window.reserve(11.0) == 0.0
    with pytest.raises(ValueError, match="rate limit"):
        RateLimit(0, 1.0)
