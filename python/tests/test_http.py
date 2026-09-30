"""HTTP behavior against a mock server (SDK_BEHAVIOR.md sections 4-8, 12.3).

Every test runs twice: once with ``Client`` and once with ``AsyncClient``.
"""

from __future__ import annotations

import json
import time
from datetime import datetime, timedelta, timezone
from decimal import Decimal
from email.utils import format_datetime
from typing import Any

import httpx
import pytest

from conftest import API_KEY, BASE_URL, USERNAME, MockAPI, bearer, body, envelope, failure, make_jwt
from topstepx import (
    AggregateBarUnit,
    ApiError,
    DecodeError,
    GatewayOrderFilter,
    HTTPStatusError,
    LoginAppRequest,
    ModifyOrderRequest,
    NotAuthenticatedError,
    OrderSide,
    OrderStatus,
    OrderType,
    PlaceOrderRequest,
    RateLimit,
    RateLimitError,
    RateLimits,
    RetrieveBarRequest,
    SearchOrderRequest,
    SearchOrdersQueryRequest,
    SearchTradeRequest,
    TransportError,
    UnauthorizedError,
    ValidationError,
    operations,
)
from topstepx.models import PlaceOrderResponse, ValidateResponse

ORDER = {
    "id": 456,
    "accountId": 123,
    "contractId": "CON.F.US.EP.U25",
    "symbolId": "F.US.EP",
    "creationTimestamp": "2025-07-01T14:00:00Z",
    "updateTimestamp": None,
    "status": 1,
    "type": 1,
    "side": 0,
    "size": 1,
    "limitPrice": 6000.25,
    "fillVolume": 0,
}
START = datetime(2025, 7, 1, tzinfo=timezone.utc)


def fresh_token(lifetime: float = 24 * 3600) -> str:
    return make_jwt(time.time() + lifetime)


# ---------------------------------------------------------------------------
# Authentication
# ---------------------------------------------------------------------------


def test_lazy_login_and_bearer_header(make: Any, api: MockAPI, creds: dict[str, str]) -> None:
    api.on("/api/Account/search", envelope(accounts=[]))
    h = make(**creds)
    assert api.requests == []  # nothing happens at construction
    h.run(lambda c: c.accounts.search())
    h.run(lambda c: c.accounts.search(only_active_accounts=False))

    assert api.paths() == ["/api/Auth/loginKey", "/api/Account/search", "/api/Account/search"]
    login = api.requests[0]
    assert login.method == "POST"
    assert body(login) == {"userName": USERNAME, "apiKey": API_KEY}
    assert "Authorization" not in login.headers
    token = api.issued[0]
    for request in api.calls("/api/Account/search"):
        assert request.headers["Authorization"] == f"Bearer {token}"
        assert request.headers["Content-Type"] == "application/json"
        assert request.headers["Accept"] == "application/json"
        assert request.headers["User-Agent"] == "topstepx-python/0.1.0"
    assert body(api.calls("/api/Account/search")[1]) == {"onlyActiveAccounts": False}
    assert h.client.token == token


def test_pre_existing_token_is_used_without_login(make: Any, api: MockAPI) -> None:
    token = fresh_token()
    h = make(token=token)
    h.run(lambda c: c.accounts.search())
    assert api.paths() == ["/api/Account/search"]
    assert bearer(api.requests[0]) == token


def test_no_token_and_no_credentials_fails_before_io(make: Any, api: MockAPI) -> None:
    h = make()
    with pytest.raises(NotAuthenticatedError):
        h.run(lambda c: c.accounts.search())
    with pytest.raises(NotAuthenticatedError):
        h.run(lambda c: c.get_token())
    assert api.requests == []


def test_invalid_credentials_raise_api_error_and_store_nothing(
    make: Any, api: MockAPI, creds: dict[str, str]
) -> None:
    api.on("/api/Auth/loginKey", failure(3, token=None))
    h = make(**creds)
    with pytest.raises(ApiError) as info:
        h.run(lambda c: c.accounts.search())
    error = info.value
    assert error.operation_id == "Auth_LoginKey"
    assert error.path == "/api/Auth/loginKey"
    assert error.code == 3
    assert error.code_name == "InvalidCredentials"
    assert error.error_message is None
    assert h.client.token is None
    assert api.paths() == ["/api/Auth/loginKey"]


def test_explicit_login_key_stores_token(make: Any, api: MockAPI) -> None:
    h = make()
    token = h.run(lambda c: c.auth.login_key("someone", "key-1"))
    assert token == api.issued[0] == h.client.token
    assert body(api.requests[0]) == {"userName": "someone", "apiKey": "key-1"}


def test_explicit_login_key_failure_raises(make: Any, api: MockAPI) -> None:
    api.on("/api/Auth/loginKey", failure(10))
    h = make()
    with pytest.raises(ApiError) as info:
        h.run(lambda c: c.auth.login_key("someone", "key-1"))
    assert info.value.code_name == "ApiKeyAuthenticationDisabled"
    assert h.client.token is None


def test_login_app_posts_its_body_and_stores_token(make: Any, api: MockAPI) -> None:
    api.on("/api/Auth/loginApp", envelope(token="APP-TOKEN"))
    h = make()
    request = LoginAppRequest(
        user_name="u", password="p", device_id="d", app_id="a", verify_key="v"
    )
    assert h.run(lambda c: c.auth.login_app(request)) == "APP-TOKEN"
    assert body(api.requests[0]) == {
        "userName": "u",
        "password": "p",
        "deviceId": "d",
        "appId": "a",
        "verifyKey": "v",
    }
    assert h.client.token == "APP-TOKEN"


def test_refresh_margin_calls_validate(make: Any, api: MockAPI) -> None:
    old = fresh_token(lifetime=1800)  # inside the default one-hour margin
    new = fresh_token()
    api.on("/api/Auth/validate", envelope(newToken=new))
    h = make(token=old)
    h.run(lambda c: c.accounts.search())

    assert api.paths() == ["/api/Auth/validate", "/api/Account/search"]
    validate = api.requests[0]
    assert validate.method == "POST"
    assert validate.content == b""
    assert "Content-Type" not in validate.headers
    assert bearer(validate) == old
    assert bearer(api.requests[1]) == new
    assert h.client.token == new


def test_refresh_failure_logs_in_when_credentials_exist(
    make: Any, api: MockAPI, creds: dict[str, str]
) -> None:
    api.on("/api/Auth/validate", failure(3))
    h = make(token=fresh_token(lifetime=60), **creds)
    h.run(lambda c: c.accounts.search())
    assert api.paths() == ["/api/Auth/validate", "/api/Auth/loginKey", "/api/Account/search"]
    assert bearer(api.requests[2]) == api.issued[0]


def test_refresh_http_failure_without_credentials_keeps_current_token(
    make: Any, api: MockAPI
) -> None:
    current = fresh_token(lifetime=60)
    api.on("/api/Auth/validate", httpx.Response(401))
    h = make(token=current)
    h.run(lambda c: c.accounts.search())
    assert api.paths() == ["/api/Auth/validate", "/api/Account/search"]
    assert bearer(api.requests[1]) == current
    assert h.client.token == current


def test_refresh_without_new_token_counts_as_failure(make: Any, api: MockAPI) -> None:
    current = fresh_token(lifetime=60)
    api.on("/api/Auth/validate", envelope(newToken=""))
    h = make(token=current)
    h.run(lambda c: c.accounts.search())
    assert h.client.token == current


def test_expired_token_without_credentials_is_sent_anyway(make: Any, api: MockAPI) -> None:
    expired = make_jwt(time.time() - 10)
    h = make(token=expired)
    h.run(lambda c: c.accounts.search())
    assert api.paths() == ["/api/Account/search"]
    assert bearer(api.requests[0]) == expired


def test_expired_token_with_credentials_logs_in(
    make: Any, api: MockAPI, creds: dict[str, str]
) -> None:
    h = make(token=make_jwt(time.time() - 10), **creds)
    h.run(lambda c: c.accounts.search())
    assert api.paths() == ["/api/Auth/loginKey", "/api/Account/search"]


def test_non_jwt_token_is_assumed_valid_for_24_hours(make: Any, api: MockAPI) -> None:
    h = make(token="opaque-token")
    h.run(lambda c: c.accounts.search())
    assert api.paths() == ["/api/Account/search"]
    expires = h.client.token_expires_at
    assert expires is not None
    assert 24 * 3600 - 60 < expires - time.time() <= 24 * 3600


def test_explicit_validate_stores_new_token(make: Any, api: MockAPI) -> None:
    api.on("/api/Auth/validate", envelope(newToken="RENEWED"))
    h = make(token=fresh_token())
    assert h.run(lambda c: c.auth.validate()) == "RENEWED"
    assert h.client.token == "RENEWED"
    assert api.paths() == ["/api/Auth/validate"]


def test_explicit_validate_failure_raises(make: Any, api: MockAPI) -> None:
    api.on("/api/Auth/validate", failure(3))
    token = fresh_token()
    h = make(token=token)
    with pytest.raises(ApiError) as info:
        h.run(lambda c: c.auth.validate())
    assert info.value.code_name == "ExpiredToken"
    assert h.client.token == token


def test_logout_clears_token_and_next_call_logs_in(
    make: Any, api: MockAPI, creds: dict[str, str]
) -> None:
    h = make(**creds)
    h.run(lambda c: c.accounts.search())
    first = h.client.token
    h.run(lambda c: c.auth.logout())
    assert h.client.token is None
    logout = api.calls("/api/Auth/logout")[0]
    assert bearer(logout) == first
    assert logout.content == b""
    h.run(lambda c: c.accounts.search())
    assert len(api.calls("/api/Auth/loginKey")) == 2
    assert h.client.token == api.issued[1]


def test_logout_clears_token_even_on_business_failure(make: Any, api: MockAPI) -> None:
    api.on("/api/Auth/logout", failure(1))
    h = make(token=fresh_token())
    with pytest.raises(ApiError) as info:
        h.run(lambda c: c.auth.logout())
    assert info.value.code_name == "InvalidSession"
    assert h.client.token is None


def test_logout_without_token_is_a_no_op(make: Any, api: MockAPI) -> None:
    h = make()
    h.run(lambda c: c.auth.logout())
    assert api.requests == []


def test_get_token_runs_ensure_token(make: Any, api: MockAPI, creds: dict[str, str]) -> None:
    h = make(**creds)
    assert h.run(lambda c: c.get_token()) == api.issued[0]
    assert h.run(lambda c: c.get_token()) == api.issued[0]
    assert api.paths() == ["/api/Auth/loginKey"]


# ---------------------------------------------------------------------------
# 401 handling
# ---------------------------------------------------------------------------


def test_401_leads_to_one_relogin_and_retry(make: Any, api: MockAPI, creds: dict[str, str]) -> None:
    stale = fresh_token()
    api.on("/api/Account/search", httpx.Response(401), envelope(accounts=[]))
    h = make(token=stale, **creds)
    assert h.run(lambda c: c.accounts.search()) == []
    assert api.paths() == ["/api/Account/search", "/api/Auth/loginKey", "/api/Account/search"]
    assert bearer(api.requests[0]) == stale
    assert bearer(api.requests[2]) == api.issued[0]


def test_second_401_raises_unauthorized(make: Any, api: MockAPI, creds: dict[str, str]) -> None:
    api.on("/api/Account/search", httpx.Response(401, text="nope"))
    h = make(token=fresh_token(), **creds)
    with pytest.raises(UnauthorizedError) as info:
        h.run(lambda c: c.accounts.search())
    assert info.value.status_code == 401
    assert info.value.body == "nope"
    assert api.paths() == ["/api/Account/search", "/api/Auth/loginKey", "/api/Account/search"]


def test_401_without_credentials_raises_immediately(make: Any, api: MockAPI) -> None:
    api.on("/api/Order/place", httpx.Response(401))
    h = make(token=fresh_token())
    with pytest.raises(UnauthorizedError):
        h.run(lambda c: c.orders.place(PlaceOrderRequest.market(1, "C", OrderSide.Bid, 1)))
    assert api.paths() == ["/api/Order/place"]


def test_401_is_retried_even_for_writes(make: Any, api: MockAPI, creds: dict[str, str]) -> None:
    api.on("/api/Order/place", httpx.Response(401), envelope(orderId=9))
    h = make(token=fresh_token(), **creds)
    order_id = h.run(lambda c: c.orders.place(PlaceOrderRequest.market(1, "C", OrderSide.Bid, 1)))
    assert order_id == 9
    assert h.sleeps == []  # 401 does not use the retry budget or backoff


def test_401_retries_with_token_replaced_by_another_caller(
    make: Any, api: MockAPI, creds: dict[str, str]
) -> None:
    replacement = fresh_token()
    holder: dict[str, Any] = {}

    def first(request: httpx.Request) -> httpx.Response:
        holder["h"].client._core.store_token(replacement)  # another caller refreshed
        return httpx.Response(401)

    api.on("/api/Account/search", first, envelope(accounts=[]))
    h = make(token=fresh_token(), **creds)
    holder["h"] = h
    h.run(lambda c: c.accounts.search())
    assert api.paths() == ["/api/Account/search", "/api/Account/search"]
    assert bearer(api.requests[1]) == replacement


# ---------------------------------------------------------------------------
# Business failures and the escape hatch
# ---------------------------------------------------------------------------


REJECTED = failure(2, "Trail Distance not set.", orderId=457)


def test_rejected_order_raises_api_error_with_order_id(make: Any, api: MockAPI) -> None:
    api.on("/api/Order/place", REJECTED)
    h = make(token=fresh_token())
    with pytest.raises(ApiError) as info:
        h.run(lambda c: c.orders.place(PlaceOrderRequest.market(123, "C", OrderSide.Ask, 1)))
    error = info.value
    assert (error.operation_id, error.code, error.code_name) == (
        "Order_PlaceOrder",
        2,
        "OrderRejected",
    )
    assert error.order_id == 457
    assert error.error_message == "Trail Distance not set."
    assert isinstance(error.response, PlaceOrderResponse)
    assert "457" in str(error)


def test_execute_returns_the_envelope_without_raising(make: Any, api: MockAPI) -> None:
    api.on("/api/Order/place", REJECTED)
    h = make(token=fresh_token())
    request = PlaceOrderRequest.market(123, "C", OrderSide.Ask, 1)
    response = h.run(lambda c: c.execute(request))
    assert isinstance(response, PlaceOrderResponse)
    assert response.is_business_failure
    assert response.order_id == 457
    assert response.error_code_name == "OrderRejected"
    with pytest.raises(ApiError):
        h.run(lambda c: c.execute(request, check=True))


def test_execute_accepts_operations_and_operation_ids(make: Any, api: MockAPI) -> None:
    api.on("/api/Auth/validate", envelope(newToken="N"))
    api.on("/api/Status/ping", httpx.Response(200, text="pong"))
    token = fresh_token()
    h = make(token=token)
    response = h.run(lambda c: c.execute(operations.AUTH_VALIDATE))
    assert isinstance(response, ValidateResponse)
    assert response.new_token == "N"
    assert h.client.token == token  # execute never changes the stored session
    assert h.run(lambda c: c.execute("Status_Ping")) == "pong"
    with pytest.raises(TypeError):
        h.run(lambda c: c.execute(operations.ORDER_PLACE_ORDER))  # needs a body
    with pytest.raises(TypeError):
        h.run(lambda c: c.execute(42))


def test_unrecognized_error_code_is_named(make: Any, api: MockAPI) -> None:
    api.on("/api/Account/search", failure(99, "new failure"))
    h = make(token=fresh_token())
    with pytest.raises(ApiError) as info:
        h.run(lambda c: c.accounts.search())
    assert info.value.code == 99
    assert info.value.code_name == "Unrecognized(99)"


def test_error_code_without_success_false_is_a_failure(make: Any, api: MockAPI) -> None:
    api.on("/api/Order/cancel", {"success": True, "errorCode": 2})
    h = make(token=fresh_token())
    with pytest.raises(ApiError) as info:
        h.run(lambda c: c.orders.cancel(1, 2))
    assert info.value.code_name == "OrderNotFound"


def test_missing_has_business_failure_defaults_to_false(make: Any, api: MockAPI) -> None:
    api.on("/api/Account/search", {"success": True, "errorCode": 0})
    h = make(token=fresh_token())
    assert h.run(lambda c: c.accounts.search()) == []


# ---------------------------------------------------------------------------
# Retries
# ---------------------------------------------------------------------------


def test_429_is_retried_and_honors_retry_after(make: Any, api: MockAPI) -> None:
    api.on(
        "/api/Order/place",
        httpx.Response(429, headers={"Retry-After": "7"}),
        envelope(orderId=5),
    )
    h = make(token=fresh_token())
    assert h.run(lambda c: c.orders.place(PlaceOrderRequest.market(1, "C", 0, 1))) == 5
    assert h.sleeps == [7.0]
    assert len(api.calls("/api/Order/place")) == 2


def test_429_honors_http_date_retry_after(make: Any, api: MockAPI) -> None:
    when = format_datetime(datetime.now(timezone.utc) + timedelta(seconds=30), usegmt=True)
    api.on("/api/Account/search", httpx.Response(429, headers={"Retry-After": when}), envelope())
    h = make(token=fresh_token())
    h.run(lambda c: c.accounts.search())
    assert len(h.sleeps) == 1
    assert 25 <= h.sleeps[0] <= 30


def test_429_uses_backoff_without_retry_after_and_gives_up(make: Any, api: MockAPI) -> None:
    api.on("/api/Account/search", httpx.Response(429))
    h = make(token=fresh_token())
    with pytest.raises(RateLimitError) as info:
        h.run(lambda c: c.accounts.search())
    assert info.value.retry_after is None
    assert h.sleeps == [1.0, 2.0, 4.0]
    assert len(api.calls("/api/Account/search")) == 4


def test_rate_limit_error_exposes_retry_after(make: Any, api: MockAPI) -> None:
    api.on("/api/Account/search", httpx.Response(429, headers={"Retry-After": "3"}))
    h = make(token=fresh_token(), max_retries=1)
    with pytest.raises(RateLimitError) as info:
        h.run(lambda c: c.accounts.search())
    assert info.value.retry_after == 3.0
    assert info.value.status_code == 429


def test_write_operations_are_not_retried_on_503(make: Any, api: MockAPI) -> None:
    api.on("/api/Order/place", httpx.Response(503, text="unavailable"))
    h = make(token=fresh_token())
    with pytest.raises(HTTPStatusError) as info:
        h.run(lambda c: c.orders.place(PlaceOrderRequest.market(1, "C", 0, 1)))
    assert info.value.status_code == 503
    assert info.value.body == "unavailable"
    assert len(api.calls("/api/Order/place")) == 1
    assert h.sleeps == []


@pytest.mark.parametrize("status", [502, 503, 504])
def test_read_operations_are_retried_on_gateway_errors(
    make: Any, api: MockAPI, status: int
) -> None:
    api.on("/api/Contract/search", httpx.Response(status), httpx.Response(status), envelope())
    h = make(token=fresh_token())
    assert h.run(lambda c: c.contracts.search("ES")) == []
    assert h.sleeps == [1.0, 2.0]


def test_other_statuses_are_not_retried(make: Any, api: MockAPI) -> None:
    api.on("/api/Contract/search", httpx.Response(500, text="boom"))
    h = make(token=fresh_token())
    with pytest.raises(HTTPStatusError) as info:
        h.run(lambda c: c.contracts.search("ES"))
    assert info.value.status_code == 500
    assert len(api.requests) == 1


def test_transport_errors_are_retried_for_reads_only(make: Any, api: MockAPI) -> None:
    def refuse(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("connection refused", request=request)

    api.on("/api/Contract/available", refuse, envelope())
    api.on("/api/Order/cancel", refuse)
    h = make(token=fresh_token())
    assert h.run(lambda c: c.contracts.available()) == []
    assert h.sleeps == [1.0]
    with pytest.raises(TransportError) as info:
        h.run(lambda c: c.orders.cancel(1, 2))
    assert isinstance(info.value.__cause__, httpx.ConnectError)
    assert len(api.calls("/api/Order/cancel")) == 1


def test_timeouts_surface_as_transport_errors(make: Any, api: MockAPI) -> None:
    def slow(request: httpx.Request) -> httpx.Response:
        raise httpx.ReadTimeout("timed out", request=request)

    api.on("/api/Account/search", slow)
    h = make(token=fresh_token(), max_retries=0)
    with pytest.raises(TransportError) as info:
        h.run(lambda c: c.accounts.search())
    assert info.value.timeout is True


def test_max_retries_zero_disables_retries(make: Any, api: MockAPI) -> None:
    api.on("/api/Account/search", httpx.Response(429))
    h = make(token=fresh_token(), max_retries=0)
    with pytest.raises(RateLimitError):
        h.run(lambda c: c.accounts.search())
    assert len(api.requests) == 1


# ---------------------------------------------------------------------------
# Error bodies, decoding, content types
# ---------------------------------------------------------------------------


def test_400_problem_details_is_parsed(make: Any, api: MockAPI) -> None:
    problem = {
        "type": "https://tools.ietf.org/html/rfc9110#section-15.5.1",
        "title": "One or more validation errors occurred.",
        "status": 400,
        "traceId": "00-abc-01",
        "errors": {"filter.accountId": ["The accountId field is required."]},
    }
    api.on(
        "/api/Order/v2/query",
        httpx.Response(
            400,
            content=json.dumps(problem).encode(),
            headers={"Content-Type": "application/problem+json"},
        ),
    )
    h = make(token=fresh_token())
    request = SearchOrdersQueryRequest(filter=GatewayOrderFilter(account_id=1))
    with pytest.raises(HTTPStatusError) as info:
        h.run(lambda c: c.orders.query(request))
    error = info.value
    assert error.status_code == 400
    assert error.problem is not None
    assert error.problem.title == problem["title"]
    assert error.problem.status == 400
    assert error.problem.extensions["traceId"] == "00-abc-01"
    assert error.problem.extensions["errors"] == problem["errors"]
    assert json.loads(error.body) == problem


def test_400_plain_body_has_no_problem(make: Any, api: MockAPI) -> None:
    api.on("/api/Auth/loginKey", httpx.Response(400, text="userName is required"))
    h = make()
    with pytest.raises(HTTPStatusError) as info:
        h.run(lambda c: c.auth.login_key("", ""))
    assert info.value.problem is None
    assert info.value.body == "userName is required"


@pytest.mark.parametrize("content_type", ["text/plain", "application/json", "text/json"])
def test_json_is_parsed_whatever_the_content_type(
    make: Any, api: MockAPI, content_type: str
) -> None:
    payload = json.dumps(envelope(orders=[ORDER])).encode()
    api.on(
        "/api/Order/searchOpen",
        httpx.Response(200, content=payload, headers={"Content-Type": content_type}),
    )
    h = make(token=fresh_token())
    orders = h.run(lambda c: c.orders.search_open(123))
    assert orders[0].limit_price == Decimal("6000.25")
    assert orders[0].status is OrderStatus.Open
    assert orders[0].update_timestamp is None


def test_invalid_json_raises_decode_error_with_body(make: Any, api: MockAPI) -> None:
    api.on("/api/Account/search", httpx.Response(200, text="<html>oops</html>"))
    h = make(token=fresh_token())
    with pytest.raises(DecodeError) as info:
        h.run(lambda c: c.accounts.search())
    assert info.value.body == "<html>oops</html>"
    assert info.value.operation_id == "Account_SearchAccounts"


def test_decode_errors_never_expose_tokens(make: Any, api: MockAPI) -> None:
    api.on("/api/Auth/loginKey", {"success": "yes", "errorCode": 0, "token": "SECRET-TOKEN"})
    h = make()
    with pytest.raises(DecodeError) as info:
        h.run(lambda c: c.auth.login_key("u", "k"))
    assert info.value.path == "success"
    assert info.value.body is not None
    assert "SECRET-TOKEN" not in info.value.body
    assert '"token":"<redacted>"' in info.value.body
    assert "SECRET-TOKEN" not in str(info.value) + repr(info.value)


def test_wrong_shape_raises_decode_error_with_path(make: Any, api: MockAPI) -> None:
    api.on("/api/Account/search", envelope(accounts=[{"id": "x"}]))
    h = make(token=fresh_token())
    with pytest.raises(DecodeError) as info:
        h.run(lambda c: c.accounts.search())
    assert info.value.path == "accounts[0].id"


def test_missing_single_payload_raises_decode_error(make: Any, api: MockAPI) -> None:
    api.on("/api/Contract/searchById", envelope())
    h = make(token=fresh_token())
    with pytest.raises(DecodeError):
        h.run(lambda c: c.contracts.search_by_id("C"))


def test_ping_as_text_and_as_json(make: Any, api: MockAPI) -> None:
    api.on(
        "/api/Status/ping",
        httpx.Response(200, text="pong", headers={"Content-Type": "text/plain"}),
        httpx.Response(200, json="pong"),
    )
    h = make()
    assert h.run(lambda c: c.status.ping()) == "pong"
    assert h.run(lambda c: c.status.ping()) == "pong"
    ping = api.calls("/api/Status/ping")[0]
    assert ping.method == "GET"
    assert "Authorization" not in ping.headers  # anonymous: no token, no credentials


def test_ping_sends_token_when_available(make: Any, api: MockAPI, creds: dict[str, str]) -> None:
    api.on("/api/Status/ping", httpx.Response(200, text="pong"))
    h = make(**creds)
    assert h.run(lambda c: c.status.ping()) == "pong"
    assert api.paths() == ["/api/Auth/loginKey", "/api/Status/ping"]
    assert bearer(api.requests[1]) == api.issued[0]


def _refuse(request: httpx.Request) -> httpx.Response:
    raise httpx.ConnectError("refused", request=request)


@pytest.mark.parametrize(
    "login_reply",
    [failure(3), httpx.Response(500), httpx.Response(401), _refuse, httpx.Response(200, text="?")],
    ids=["business-failure", "http-500", "http-401", "transport-error", "undecodable"],
)
def test_ping_falls_back_to_anonymous_when_login_fails(
    make: Any, api: MockAPI, creds: dict[str, str], login_reply: Any
) -> None:
    api.on("/api/Auth/loginKey", login_reply)
    api.on("/api/Status/ping", httpx.Response(200, text="pong"))
    h = make(**creds)
    assert h.run(lambda c: c.status.ping()) == "pong"
    assert "Authorization" not in api.calls("/api/Status/ping")[0].headers


def test_invalidate_token_discards_only_the_matching_token(
    make: Any, api: MockAPI, creds: dict[str, str]
) -> None:
    h = make(**creds)
    token = h.run(lambda c: c.get_token())
    assert h.client.invalidate_token("some-other-token") is False
    assert h.client.token == token
    assert h.client.invalidate_token(token) is True
    assert h.client.token is None
    assert h.client.invalidate_token(token) is False
    assert h.run(lambda c: c.get_token()) == api.issued[1]  # logs in again


# ---------------------------------------------------------------------------
# Validation, encoding and every endpoint
# ---------------------------------------------------------------------------


def test_validation_runs_before_login_and_any_io(
    make: Any, api: MockAPI, creds: dict[str, str]
) -> None:
    h = make(**creds)
    request = PlaceOrderRequest(
        account_id=1, contract_id="C", type=OrderType.StopLimit, side=OrderSide.Bid, size=1
    )
    with pytest.raises(ValidationError):
        h.run(lambda c: c.orders.place(request))
    with pytest.raises(ValidationError):
        h.run(lambda c: c.positions.partial_close(1, "C", 0))
    assert api.requests == []


def test_validation_can_be_disabled(make: Any, api: MockAPI) -> None:
    api.on("/api/Position/partialCloseContract", failure(5))
    h = make(token=fresh_token(), validate_requests=False)
    with pytest.raises(ApiError) as info:
        h.run(lambda c: c.positions.partial_close(1, "C", 0))
    assert info.value.code_name == "InvalidCloseSize"
    assert body(api.requests[0]) == {"accountId": 1, "contractId": "C", "size": 0}


def test_decimals_are_exact_on_the_wire(make: Any, api: MockAPI) -> None:
    price = Decimal("6000.123456789012345678901")
    api.on("/api/Order/place", envelope(orderId=1))
    api.on(
        "/api/Order/searchOpen",
        httpx.Response(
            200,
            content=b'{"success":true,"errorCode":0,"orders":[{"id":1,"accountId":1,'
            b'"contractId":"C","symbolId":"S","creationTimestamp":"2025-07-01T14:00:00Z",'
            b'"status":1,"type":1,"side":0,"size":1,"fillVolume":0,'
            b'"limitPrice":6000.123456789012345678901}]}',
        ),
    )
    h = make(token=fresh_token())
    h.run(lambda c: c.orders.place(PlaceOrderRequest.limit(1, "C", OrderSide.Bid, 1, price)))
    raw = api.calls("/api/Order/place")[0].content
    assert b'"limitPrice":6000.123456789012345678901' in raw
    orders = h.run(lambda c: c.orders.search_open(1))
    assert orders[0].limit_price == price


def test_null_optional_request_fields_are_omitted(make: Any, api: MockAPI) -> None:
    h = make(token=fresh_token())
    h.run(lambda c: c.orders.modify(ModifyOrderRequest(account_id=1, order_id=2, size=3)))
    assert body(api.requests[0]) == {"accountId": 1, "orderId": 2, "size": 3}


def _every_endpoint() -> list[tuple[str, str, Any, dict[str, Any], Any]]:
    bars_request = RetrieveBarRequest(
        contract_id="C",
        start_time=START,
        end_time=START + timedelta(days=1),
        unit=AggregateBarUnit.Minute,
        unit_number=5,
        limit=10,
    )
    return [
        (
            "POST",
            "/api/Account/search",
            lambda c: c.accounts.search(),
            envelope(accounts=[]),
            {"onlyActiveAccounts": True},
        ),
        (
            "POST",
            "/api/Contract/search",
            lambda c: c.contracts.search("ES", live=True),
            envelope(contracts=[]),
            {"searchText": "ES", "live": True},
        ),
        (
            "POST",
            "/api/Contract/searchById",
            lambda c: c.contracts.search_by_id("C"),
            envelope(
                contract={
                    "id": "C",
                    "name": "N",
                    "description": "D",
                    "tickSize": 0.25,
                    "tickValue": 12.5,
                    "activeContract": True,
                    "symbolId": "S",
                }
            ),
            {"contractId": "C"},
        ),
        (
            "POST",
            "/api/Contract/available",
            lambda c: c.contracts.available(),
            envelope(contracts=[]),
            {"live": False},
        ),
        (
            "POST",
            "/api/History/retrieveBars",
            lambda c: c.history.retrieve_bars(bars_request),
            envelope(bars=[]),
            {
                "contractId": "C",
                "live": False,
                "startTime": "2025-07-01T00:00:00Z",
                "endTime": "2025-07-02T00:00:00Z",
                "unit": 2,
                "unitNumber": 5,
                "limit": 10,
                "includePartialBar": False,
            },
        ),
        (
            "POST",
            "/api/Order/search",
            lambda c: c.orders.search(SearchOrderRequest(account_id=1, start_timestamp=START)),
            envelope(orders=[]),
            {"accountId": 1, "startTimestamp": "2025-07-01T00:00:00Z"},
        ),
        (
            "POST",
            "/api/Order/searchById",
            lambda c: c.orders.search_by_id(1, 456),
            envelope(order=ORDER),
            {"accountId": 1, "orderId": 456},
        ),
        (
            "POST",
            "/api/Order/searchOpen",
            lambda c: c.orders.search_open(1),
            envelope(orders=[]),
            {"accountId": 1},
        ),
        (
            "POST",
            "/api/Order/v2/query",
            lambda c: c.orders.query(
                SearchOrdersQueryRequest(
                    filter=GatewayOrderFilter(account_id=1, statuses=[OrderStatus.Open]),
                    include_total_count=True,
                )
            ),
            envelope(orders=[ORDER], totalCount=1),
            {"filter": {"accountId": 1, "statuses": [1]}, "includeTotalCount": True},
        ),
        (
            "POST",
            "/api/Order/place",
            lambda c: c.orders.place(
                PlaceOrderRequest.trailing_stop(1, "C", OrderSide.Ask, 1, "5998.5")
            ),
            envelope(orderId=7),
            {
                "accountId": 1,
                "contractId": "C",
                "type": 5,
                "side": 1,
                "size": 1,
                "trailPrice": 5998.5,
            },
        ),
        (
            "POST",
            "/api/Order/cancel",
            lambda c: c.orders.cancel(1, 2),
            envelope(),
            {"accountId": 1, "orderId": 2},
        ),
        (
            "POST",
            "/api/Order/modify",
            lambda c: c.orders.modify(
                ModifyOrderRequest(account_id=1, order_id=2, limit_price=Decimal("6000"))
            ),
            envelope(),
            {"accountId": 1, "orderId": 2, "limitPrice": 6000},
        ),
        (
            "POST",
            "/api/Position/searchOpen",
            lambda c: c.positions.search_open(1),
            envelope(positions=[]),
            {"accountId": 1},
        ),
        (
            "POST",
            "/api/Position/closeContract",
            lambda c: c.positions.close(1, "C"),
            envelope(),
            {"accountId": 1, "contractId": "C"},
        ),
        (
            "POST",
            "/api/Position/partialCloseContract",
            lambda c: c.positions.partial_close(1, "C", 2),
            envelope(),
            {"accountId": 1, "contractId": "C", "size": 2},
        ),
        (
            "POST",
            "/api/Trade/search",
            lambda c: c.trades.search(SearchTradeRequest(account_id=1, start_timestamp=START)),
            envelope(trades=[]),
            {"accountId": 1, "startTimestamp": "2025-07-01T00:00:00Z"},
        ),
        ("GET", "/api/Status/ping", lambda c: c.status.ping(), None, None),
    ]


@pytest.mark.parametrize(
    ("method", "path", "call", "reply", "expected_body"),
    _every_endpoint(),
    ids=[entry[1] for entry in _every_endpoint()],
)
def test_every_endpoint_method_hits_its_path(
    make: Any,
    api: MockAPI,
    method: str,
    path: str,
    call: Any,
    reply: Any,
    expected_body: Any,
) -> None:
    if reply is not None:
        api.on(path, reply)
    token = fresh_token()
    h = make(token=token)
    h.run(call)
    request = api.requests[-1]
    assert (request.method, request.url.path) == (method, path)
    assert str(request.url) == BASE_URL + path
    assert bearer(request) == token
    assert body(request) == expected_body


def test_query_returns_orders_and_total_count(make: Any, api: MockAPI) -> None:
    api.on("/api/Order/v2/query", envelope(orders=[ORDER], totalCount=41))
    h = make(token=fresh_token())
    result = h.run(
        lambda c: c.orders.query(SearchOrdersQueryRequest(filter=GatewayOrderFilter(account_id=1)))
    )
    assert result.total_count == 41
    assert result.orders[0].id == 456


# ---------------------------------------------------------------------------
# Rate limiting
# ---------------------------------------------------------------------------


def test_client_side_rate_limit_waits_for_the_window(make: Any, api: MockAPI) -> None:
    h = make(token=fresh_token(), rate_limit=RateLimits(general=RateLimit(2, 10.0)))
    for _ in range(3):
        h.run(lambda c: c.accounts.search())
    assert len(api.requests) == 3
    assert h.sleeps == [pytest.approx(10.0)]


def test_history_bucket_is_separate(make: Any, api: MockAPI) -> None:
    limits = RateLimits(general=RateLimit(1, 60.0), history=RateLimit(1, 30.0))
    h = make(token=fresh_token(), rate_limit=limits)
    request = RetrieveBarRequest(
        contract_id="C", start_time=START, end_time=START, unit=AggregateBarUnit.Day
    )
    h.run(lambda c: c.accounts.search())
    h.run(lambda c: c.history.retrieve_bars(request))
    assert h.sleeps == []
    h.run(lambda c: c.history.retrieve_bars(request))
    assert h.sleeps == [pytest.approx(30.0)]


def test_rate_limit_can_be_disabled(make: Any, api: MockAPI) -> None:
    h = make(token=fresh_token(), rate_limit=False)
    for _ in range(250):
        h.run(lambda c: c.status.ping())
    assert h.sleeps == []


def test_logins_and_retries_consume_rate_limit_slots(
    make: Any, api: MockAPI, creds: dict[str, str]
) -> None:
    api.on("/api/Account/search", httpx.Response(503), envelope())
    h = make(rate_limit=RateLimits(general=RateLimit(2, 100.0)), **creds)
    h.run(lambda c: c.accounts.search())
    # login + first attempt fill the window; the retry waits for a slot
    assert api.paths() == ["/api/Auth/loginKey", "/api/Account/search", "/api/Account/search"]
    assert h.sleeps[0] == 1.0  # backoff
    assert sum(h.sleeps) == pytest.approx(100.0)


# ---------------------------------------------------------------------------
# Configuration and secrets
# ---------------------------------------------------------------------------


def test_repr_never_contains_secrets(make: Any, api: MockAPI, creds: dict[str, str]) -> None:
    h = make(**creds)
    h.run(lambda c: c.accounts.search())
    text = repr(h.client)
    assert API_KEY not in text
    assert api.issued[0] not in text
    assert "has_token=True" in text


def test_invalid_configuration_is_rejected(mode: str) -> None:
    from topstepx import AsyncClient, Client

    cls = Client if mode == "sync" else AsyncClient
    with pytest.raises(ValueError, match="together"):
        cls(username="only-user")
    with pytest.raises(ValueError, match="base_url"):
        cls(base_url="ftp://example.com")
    with pytest.raises(ValueError, match="max_retries"):
        cls(max_retries=-1)


def test_from_env_reads_documented_variables(mode: str) -> None:
    from topstepx import AsyncClient, Client

    cls = Client if mode == "sync" else AsyncClient
    environ = {
        "TOPSTEPX_USERNAME": "env-user",
        "TOPSTEPX_API_KEY": "env-key",
        "TOPSTEPX_API_URL": "https://gateway.example",
        "TOPSTEPX_USER_HUB_URL": "https://rtc.example/hubs/user",
        "TOPSTEPX_MARKET_HUB_URL": "https://rtc.example/hubs/market",
    }
    client = cls.from_env(environ=environ, max_retries=1)
    try:
        assert client.base_url == "https://gateway.example"
        assert client.user_hub_url == "https://rtc.example/hubs/user"
        assert client.market_hub_url == "https://rtc.example/hubs/market"
        assert client._core.credentials is not None
        assert client._core.credentials.username == "env-user"
        assert client._core.settings.max_retries == 1
    finally:
        if isinstance(client, Client):
            client.close()
        else:
            import asyncio

            asyncio.run(client.aclose())
