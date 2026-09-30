# topstepx — Python SDK for the ProjectX Gateway API (TopstepX)

A typed Python client for the [ProjectX Gateway API](https://gateway.docs.projectx.com/docs/intro/)
as deployed by TopstepX: all 21 REST operations (sync **and** asyncio clients) and the
SignalR user and market hubs.

It is one of three SDKs in this repository (Rust, Python, Go) that implement the same
behavior, defined in [`../spec/SDK_BEHAVIOR.md`](../spec/SDK_BEHAVIOR.md). The REST contract
is [`../spec/projectx-openapi.json`](../spec/projectx-openapi.json); the modeling decisions
and known source conflicts are in [`../spec/README.md`](../spec/README.md).

> **Safety: orders are real.** Placing, modifying, cancelling and closing trade real
> accounts. Develop and test against **simulated accounts** (`TradingAccount.simulated`).
> The order example refuses to run unless `TOPSTEPX_ALLOW_TRADING=1`.

- Python 3.10+; runtime dependencies: `httpx` and `websockets` only.
- Fully typed (`py.typed`, `mypy --strict` clean). License: GPL-3.0-only.

## Contents

- [Installation](#installation)
- [Quickstart](#quickstart)
- [Configuration](#configuration)
- [Authentication and the token lifecycle](#authentication-and-the-token-lifecycle)
- [Endpoint groups](#endpoint-groups)
- [The low-level escape hatch](#the-low-level-escape-hatch)
- [Realtime (SignalR)](#realtime-signalr)
- [Error handling](#error-handling)
- [Retries and rate limits](#retries-and-rate-limits)
- [Client-side validation](#client-side-validation)
- [Models, decimals, timestamps and enums](#models-decimals-timestamps-and-enums)
- [Spec unknowns and deviations](#spec-unknowns-and-deviations)
- [Logging and secrets](#logging-and-secrets)
- [Examples](#examples)
- [Development and tests](#development-and-tests)

## Installation

From this directory (the distribution is `topstepx`, imported as `import topstepx`):

```bash
pip install .            # or: pip install -e '.[dev]' for development
```

## Quickstart

```python
from topstepx import Client

# Reads TOPSTEPX_USERNAME and TOPSTEPX_API_KEY (and optional URL overrides).
with Client.from_env() as client:
    for account in client.accounts.search():
        print(account.id, account.name, account.balance, account.simulated)

    contract = client.contracts.search("ES")[0]
    print(contract.id, contract.tick_size)
```

The same with asyncio:

```python
import asyncio
from topstepx import AsyncClient


async def main() -> None:
    async with AsyncClient(username="me", api_key="...") as client:
        accounts = await client.accounts.search()


asyncio.run(main())
```

`Client` and `AsyncClient` have identical constructors, endpoint groups and method names;
every `AsyncClient` method is a coroutine. `Client` is thread-safe.

## Configuration

| Argument | Default | Meaning |
| --- | --- | --- |
| `username`, `api_key` | — | Platform username and API key (both or neither). |
| `token` | — | A pre-existing token, instead of or in addition to credentials. |
| `base_url` | `https://api.topstepx.com` | REST base URL; other firms run the gateway elsewhere. |
| `user_hub_url` | `https://rtc.topstepx.com/hubs/user` | Used by `UserHub(client)`. |
| `market_hub_url` | `https://rtc.topstepx.com/hubs/market` | Used by `MarketHub(client)`. |
| `timeout` | `30.0` (connect `10.0`) | Seconds, an `httpx.Timeout`, or `None`. |
| `max_retries` | `3` | Retry budget per request; `0` disables retries. |
| `backoff` | `Backoff(1.0, 2.0, 30.0)` | 1 s, ×2 per retry, capped at 30 s, no jitter. |
| `rate_limit` | `True` | `True` (documented limits), `False`, or a `RateLimits`. |
| `validate_requests` | `True` | Client-side validation before any I/O. |
| `refresh_margin` | `3600` | Renew the token this long before expiry (seconds or `timedelta`). |
| `transport` / `http_client` | — | Inject an `httpx` transport (e.g. `httpx.MockTransport`) or client. |

`Client.from_env()` / `AsyncClient.from_env()` read `TOPSTEPX_USERNAME`, `TOPSTEPX_API_KEY`,
`TOPSTEPX_API_URL`, `TOPSTEPX_USER_HUB_URL` and `TOPSTEPX_MARKET_HUB_URL`; keyword
arguments override them. Every request sends `User-Agent: topstepx-python/0.1.0` and
`Accept: application/json`.

## Authentication and the token lifecycle

API keys and tokens are secrets: they never appear in `repr`, exception messages or logs.

- **Lazy login.** With `username`/`api_key`, the first authenticated call logs in
  (`Auth_LoginKey`) and stores the token. Invalid credentials arrive as HTTP 200 with
  `errorCode` 3 and raise `ApiError` (`code_name == "InvalidCredentials"`); nothing is stored.
- **Expiry** is read from the JWT `exp` claim (the signature is never verified); a token that
  is not a decodable JWT is assumed to live 24 hours.
- **Before every authenticated request** (`ensure_token`):
  1. no token → log in (credentials) or raise `NotAuthenticatedError` before any I/O;
  2. expired → log in (credentials), otherwise send it anyway and let the server decide;
  3. within `refresh_margin` of expiry → `Auth_Validate`; on success store `newToken`; on any
     failure log in (credentials) or keep using the still-valid token;
  4. otherwise use the stored token.

  **Renewal cooldown:** when renewal fails and the client keeps the current token (no
  credentials), it does not call `Auth_Validate` again for 60 seconds; the token is used
  as-is until the cooldown ends or the token expires. Storing any new token clears the
  cooldown. (Without it, every request in the last hour would first make a failing
  `validate` call.)

  Refreshes and logins are **single-flight**: concurrent callers (threads or tasks) wait for
  one refresh instead of stampeding.
- **HTTP 401** on an authenticated request: with credentials, log in again and retry the
  request once (even writes — a 401 means it was never processed); if another caller already
  replaced the token, retry with that token instead. Otherwise raise `UnauthorizedError`.
- `client.get_token()` runs `ensure_token` and returns the token (the hubs use it);
  `client.token` returns the stored token without any refresh; `client.invalidate_token(t)`
  discards the stored token if it is `t` (used when a hub rejects a token).
- `auth.login_key(user_name, api_key)` logs in explicitly and stores the token. Those
  credentials are used for that call only; pass credentials to the constructor to enable
  automatic re-login.
- `auth.validate()` renews the current token and stores `newToken`.
- `auth.logout()` calls `Auth_Logout`, then clears the stored token whatever the result (a
  business failure is still raised). With no stored token it does nothing. A later
  authenticated call logs in again if credentials are configured.
- `auth.login_app(request)` targets `Auth_LoginApp`, which exists only in Swagger: **the
  current authentication guide does not support application login — use `login_key`.**

## Endpoint groups

Each high-level method checks the business result (raising `ApiError`) and returns the payload.

| Group | Method | Operation | Returns |
| --- | --- | --- | --- |
| `auth` | `login_key(user_name, api_key)` | `Auth_LoginKey` | token (stored) |
| | `login_app(request)` | `Auth_LoginApp` (unsupported by the guide) | token (stored) |
| | `logout()` | `Auth_Logout` | `None`; clears the token |
| | `validate()` | `Auth_Validate` | new token (stored) |
| `accounts` | `search(only_active_accounts=True)` | `Account_SearchAccounts` | `list[TradingAccount]` |
| `contracts` | `search(search_text, live=False)` | `Contract_SearchContracts` | `list[Contract]` (≤ 20) |
| | `search_by_id(contract_id)` | `Contract_SearchContractById` | `Contract` |
| | `available(live=False)` | `Contract_AvailableContracts` | `list[Contract]` |
| `history` | `retrieve_bars(request)` | `History_GetBars` | `list[AggregateBar]` |
| `orders` | `search(request)` | `Order_SearchOrders` | `list[Order]` |
| | `search_by_id(account_id, order_id)` | `Order_SearchOrderById` | `Order` |
| | `search_open(account_id)` | `Order_SearchOpenOrders` | `list[Order]` |
| | `query(request)` | `Order_SearchOrdersQuery` | `OrderQueryResult(orders, total_count)` |
| | `place(request)` | `Order_PlaceOrder` | order id (`int`) |
| | `cancel(account_id, order_id)` | `Order_CancelOrder` | `None` |
| | `modify(request)` | `Order_ModifyOrder` | `None` |
| `positions` | `search_open(account_id)` | `Position_SearchOpenPositions` | `list[Position]` |
| | `close(account_id, contract_id)` | `Position_CloseContractPosition` | `None` |
| | `partial_close(account_id, contract_id, size)` | `Position_PartialCloseContractPosition` | `None` |
| `trades` | `search(request)` | `Trade_SearchHalfTurnTrades` | `list[HalfTrade]` |
| `status` | `ping()` | `Status_Ping` | `"pong"` |

```python
from datetime import datetime, timedelta, timezone
from topstepx import (
    AggregateBarUnit,
    OrderSide,
    OrderType,
    PlaceOrderBracket,
    PlaceOrderRequest,
    RetrieveBarRequest,
    SearchTradeRequest,
)

now = datetime.now(timezone.utc)
bars = client.history.retrieve_bars(
    RetrieveBarRequest(
        contract_id="CON.F.US.EP.U25",
        start_time=now - timedelta(days=1),
        end_time=now,
        unit=AggregateBarUnit.Minute,
        unit_number=5,
        limit=500,
    )
)
trades = client.trades.search(
    SearchTradeRequest(account_id=123, start_timestamp=now - timedelta(days=7))
)

# Orders: typed constructors, then optional tag and brackets (Auto OCO Brackets mode).
order = PlaceOrderRequest.limit(123, "CON.F.US.EP.U25", OrderSide.Bid, 1, "6000.25")
order = order.with_custom_tag("my-unique-tag").with_brackets(
    stop_loss=PlaceOrderBracket(ticks=8, type=OrderType.Stop),
    take_profit=PlaceOrderBracket(ticks=16, type=OrderType.Limit),
)
order_id = client.orders.place(order)  # a SIMULATED account, please
```

Constructors: `PlaceOrderRequest.market / limit / stop / trailing_stop / join_bid / join_ask`.
For a trailing stop, `trail_price` is the **absolute** initial stop price (aligned to the
tick size); in responses `Order.trail_price` is a price **distance**.

`Status_Ping` sends the bearer token when one is held or obtainable; if obtaining one fails
for any reason other than cancellation (including a failed login), it pings anonymously.
It returns the plain string whether the server answers `text/plain` or a JSON string.

## The low-level escape hatch

`execute()` performs any operation and returns the **complete typed response envelope**
without turning a business failure into an exception (HTTP, transport and decode errors are
still raised). Use it to read fields the high-level methods discard, such as the `orderId`
of a rejected order:

```python
from topstepx import operations

response = client.execute(PlaceOrderRequest.market(123, "CON.F.US.EP.U25", OrderSide.Bid, 1))
if response.is_business_failure:
    print(response.error_code, response.error_code_name, response.error_message, response.order_id)

renewed = client.execute(operations.AUTH_VALIDATE)  # body-less operations
pong = client.execute("Status_Ping")  # or by operationId
checked = client.execute(request, check=True)  # raise ApiError instead
```

Authentication, retries, rate limiting and validation apply as usual, but `execute()`
never changes the stored session (executing a login or logout does not store or clear a
token). `topstepx.operations.OPERATIONS` is the table of all 21 operations (method, path,
auth, rate bucket, retry class, request and response types).

## Realtime (SignalR)

The SDK implements the ASP.NET Core SignalR JSON hub protocol directly over `websockets`
(no SignalR library, negotiation skipped). Hubs are asyncio-only; they take their token from
a `Client`/`AsyncClient` (calling `get_token()` — `ensure_token` — on **every** connection
attempt), a sync or async callable, or a fixed token string.

```python
import asyncio
from topstepx import AsyncClient, UserHub, MarketHub
from topstepx.realtime import UserOrderEvent, QuoteEvent, Reconnecting, Closed


async def main() -> None:
    async with AsyncClient.from_env() as client:
        async with UserHub(client) as hub:
            await hub.subscribe_accounts()
            await hub.subscribe_orders(123)
            async for event in hub.events():
                if isinstance(event, UserOrderEvent):
                    print(event.order.id, event.order.status, event.action)
                elif isinstance(event, Reconnecting):
                    print("reconnecting", event.attempt, event.cause)


asyncio.run(main())
```

| Hub | Methods | Events |
| --- | --- | --- |
| `UserHub` | `subscribe_accounts()`, `subscribe_orders(account_id)`, `subscribe_positions(account_id)`, `subscribe_trades(account_id)` and `unsubscribe_*` | `UserAccountEvent`, `UserOrderEvent`, `UserPositionEvent`, `UserTradeEvent` |
| `MarketHub` | `subscribe_contract_quotes(contract_id)`, `subscribe_contract_trades(contract_id)`, `subscribe_contract_market_depth(contract_id)` and `unsubscribe_*` | `QuoteEvent`, `MarketTradeEvent`, `DepthEvent` (each with `contract_id`) |

- **Events** arrive through `async for event in hub.events()` and/or callbacks registered
  with `hub.on(EventType, callback)` (sync or async; `hub.on(HubEvent, cb)` receives
  everything; callbacks run in their own task and exceptions are logged). State events
  (`Connected`, `Reconnecting(attempt, cause)`, `Reconnected(attempt)`, `Closed(cause)`,
  `SubscriptionReplayFailed`) share the stream with data events. Events are buffered from
  construction, so `Connected` is seen even when iteration starts after `start()`. Use one
  `events()` consumer per hub; use callbacks for fan-out.
- **Backpressure**: the read loop never blocks. The buffer (`HubOptions.event_buffer_size`,
  default 10,000) drops the oldest event when full; drops are counted in
  `hub.dropped_events` and logged.
- **Invocations**: `await hub.invoke(method, *args)` (completion result or `InvocationError`,
  `HubTimeoutError` after 30 s) and `await hub.send(method, *args)` (no completion).
  While not connected they fail fast with `NotConnectedError`.
- **Keepalive**: a ping after 15 s without sending; the connection counts as lost after 30 s
  without receiving anything.
- **Reconnect** after a connection loss or a server close with `allowReconnect`: delays
  0, 1, 2, 5, 10 s, then every 30 s, unlimited by default (`ReconnectPolicy`; `max_attempts=0`
  disables it). Each attempt fetches a fresh token; afterwards the subscription registry
  (successful `subscribe_*` calls, de-duplicated, minus `unsubscribe_*`) is replayed in
  insertion order, then `Reconnected` is emitted. A failed replay is surfaced as
  `SubscriptionReplayFailed` and stays registered. A server close without `allowReconnect`
  ends the hub. The initial `start()` is not retried.
- **Rejected tokens**: when the WebSocket upgrade is rejected with HTTP 401 (initial connect
  or reconnect), the token is reported to the provider — a client discards it if it is still
  the stored token, so the next attempt logs in again — and the attempt fails with
  `TokenRejectedError` (the message never contains the token). A custom callable provider
  receives the report through an optional `invalidate_token(token)` attribute.
- **`close()`** stops reconnecting, sends a close message, closes the socket, fails pending
  invocations with `ConnectionLostError`, emits `Closed(None)` and ends the stream.
- Tuning (all durations configurable, e.g. for tests): `HubOptions(reconnect=..., keepalive_interval=15,
  server_timeout=30, handshake_timeout=15, invocation_timeout=30, open_timeout=10,
  close_timeout=5, event_buffer_size=10_000, max_message_size=16 MiB, proxy=True)`.

**Lenient decoding.** A malformed payload never breaks the connection. User-hub payloads
may be bare (`{...}`), wrapped (`{"action": n, "data": {...}}`, `action` exposed — its
meaning is undocumented) or arrays of either: one event per element. `GatewayTrade` and
`GatewayDepth` payloads may be one object or an array: one event per message with a tuple.
Volumes accept integers, integral floats (`3.0`) and numeric strings. Unknown targets and
undecodable payloads become `UnknownEvent(target, arguments, error)`. Data events also keep
the raw JSON (`event.raw`).

## Error handling

All exceptions derive from `topstepx.TopstepXError`:

| Exception | When |
| --- | --- |
| `ApiError` | Business failure (`success == false` or `errorCode != 0`), usually with HTTP 200. Carries `operation_id`, `path`, `code`, `code_name` (enum name or `Unrecognized(<n>)`), `error_message` (may be `None`), `order_id` (placement only; not proof of acceptance) and the typed `response`. |
| `HTTPStatusError` | Any other non-2xx status: `status_code`, `body`, and `problem` (RFC 7807 `ProblemDetails` with extension members, when the body is a JSON object). |
| ├ `UnauthorizedError` | HTTP 401 after the re-login rule. |
| └ `RateLimitError` | HTTP 429 after the retries; `retry_after` in seconds when sent. |
| `TransportError` | DNS, connection, TLS or timeout failure (`timeout` flag; the `httpx` error is `__cause__`). |
| `DecodeError` | A body or payload that cannot be decoded (`body`, `path` such as `orders[0].status`). |
| `ValidationError` | Client-side validation (also a `ValueError`); `violations` lists every broken rule. |
| `NotAuthenticatedError` | No token and no credentials; raised before any I/O. |
| `HubError` | Realtime: `HubConnectError` (`TokenRejectedError`, `HandshakeError`), `NotConnectedError`, `ConnectionLostError`, `ServerCloseError`, `InvocationError`, `HubTimeoutError`. |

```python
from topstepx import ApiError, HTTPStatusError, RateLimitError

try:
    client.orders.place(order)
except ApiError as e:
    print(e.code_name, e.error_message, e.order_id)
except RateLimitError as e:
    print("throttled; retry after", e.retry_after)
except HTTPStatusError as e:
    print(e.status_code, e.problem or e.body)
```

## Retries and rate limits

**Retries** (budget `max_retries`, default 3; backoff 1 s, 2 s, 4 s … capped at 30 s):

- **HTTP 429** is retried for every operation, waiting `Retry-After` (delta-seconds or
  HTTP-date) when present, otherwise the backoff.
- **HTTP 502/503/504 and transport errors** are retried **only for read operations**
  (searches, bars, ping). Writes — logins, validate, logout, place, cancel, modify, close,
  partial close — are never retried on these, because they may already have executed.
- **HTTP 401** follows the re-login rule and does not use the budget. Nothing else is retried.

**Client-side rate limiting** (enabled by default) enforces the documented limits with a
sliding window per bucket: `History_GetBars` 50 requests per 30 s, every other operation
200 per 60 s. A slot is taken before every HTTP attempt, including retries and logins; when
the window is full the call waits until the oldest entry expires. Replace or disable it with
`rate_limit=RateLimits(general=RateLimit(200, 60), history=RateLimit(50, 30))` or
`rate_limit=False`. **The limiter is per client instance: separate clients and separate
processes do not share it**, so share one client per process to stay under the limits.

## Client-side validation

Before any network I/O (even before login), requests are checked against exactly the rules
the spec encodes (each reproducing a case of `validation-report.json`):

1. `PlaceOrderRequest.type` must be Limit (1), Market (2), Stop (4), TrailingStop (5),
   JoinBid (6) or JoinAsk (7) — StopLimit (3), Unknown (0) and unrecognized values are rejected;
2. a trailing stop (type 5) requires a non-null `trail_price`;
3. bracket types must come from the same six values;
4. `RetrieveBarRequest.limit <= 20000`;
5. `PartialCloseContractPositionRequest.size >= 1`.

Violations raise `ValidationError`. Disable validation with `validate_requests=False`; the
server remains the final authority.

## Models, decimals, timestamps and enums

- **Models** are frozen, slotted dataclasses with keyword-only constructors,
  `from_dict()`/`to_dict()` and `from_json()`/`to_json()`. Request, response and enum types
  keep their spec names; object models drop the `Model` suffix (`OrderModel` → `Order`).
  Attributes are snake_case (`account_id` ↔ `accountId`). Unknown JSON members are ignored,
  missing optional fields are `None`, missing arrays are empty; decoded arrays are tuples and
  high-level methods return lists. `None` request fields are omitted from the JSON.
  `topstepx.models.SCHEMA_MODELS` maps every spec schema name to its class.
- **Decimals.** Every `format: decimal` number is a `decimal.Decimal`, parsed from JSON
  without loss (`6000.25` is exactly `Decimal("6000.25")`) and sent as a JSON **number**
  with all its digits (never a quoted string, never through `float`). Convenience
  constructors also accept `int`, `float` (via its shortest repr) and numeric strings.
- **Timestamps** are aware `datetime` objects in UTC. Decoding accepts `Z` or any offset,
  0–9 fractional digits (.NET emits 7; digits beyond microseconds are truncated) and a
  missing offset (read as UTC). Requests send `YYYY-MM-DDTHH:MM:SS[.ffffff]Z`; a naive
  `datetime` is taken to be UTC.
- **Enums** are `IntEnum`s whose member names are the spec's `x-enumNames`
  (`OrderType.Limit.name == "Limit"`). Unknown values never fail decoding:
  `OrderType(42)` is a cached pseudo-member equal to `42`, named `"Unrecognized(42)"`, with
  `is_recognized == False`; pseudo-members pickle and compare like ints. `OrderStatus` value
  0 is named `"None"`, which is a Python keyword: use `OrderStatus.NONE` or `OrderStatus["None"]`.
  Every operation has its own error-code enum (`LoginErrorCode`, `PlaceOrderErrorCode`, …;
  `SearchOrderErrorCode` is shared by the three order searches).
- `AggregateBar` uses descriptive attributes for the single-letter JSON fields:
  `t`→`timestamp`, `o`→`open`, `h`→`high`, `l`→`low`, `c`→`close`, `v`→`volume`.

## Spec unknowns and deviations

- **Swagger-only operations**: `Auth_LoginApp` (unsupported by the current guide — use
  `login_key`), `Auth_Logout`, `Order_SearchOrderById`, `Order_SearchOrdersQuery` and
  `Status_Ping` exist only in Swagger; their runtime availability is unverified. Anonymous
  ping is unverified too.
- **Realtime payloads** come from the official realtime guide, not from the OpenAPI spec,
  and REST models are not assumed identical. Every non-identifier field is optional (quote
  updates may carry only changed fields), decoding is lenient (see above), and `DomType` /
  `TradeLogType` come from the guide.
- **Undocumented bar fields**: `AggregateBar.d` (a date string) and `AggregateBar.k` (an
  integer) are exposed raw; their meaning is not guessed.
- `hasBusinessFailure` defaults to `false` when absent, and `Order.updateTimestamp` is
  optional although the spec requires it. Other fields the spec marks required are required:
  a response lacking one raises `DecodeError` (for example `TradingAccount.simulated`, which
  some guide examples omit — no value is invented for it).
- Pagination defaults of `Order_SearchOrdersQuery` and error-body shapes beyond
  `ProblemDetails` are unspecified; none are invented.

Python-specific choices (the contract leaves them open):

- `execute()` never stores or clears the session token.
- Explicit `auth.logout()` does not re-login on HTTP 401 (it would only log in to log out);
  it clears the token and raises `UnauthorizedError`.
- `RetrieveBarRequest` defaults `unit_number=1` and `limit=20000`, and required boolean
  request flags default to `False` (`only_active_accounts` to `True`); these values are
  always sent.
- Decoding is additionally lenient where harmless: numeric strings for decimals, integral
  numbers for integers, member names matched case-insensitively as a fallback, and
  `Retry-After` with a fractional number of seconds.
- `ProblemDetails` is parsed from any non-2xx JSON-object body, not only HTTP 400.

## Logging and secrets

The SDK logs to `logging.getLogger("topstepx")` (realtime under `topstepx.realtime.*`):
logins, token refreshes, retries, reconnects and dropped events. Tokens, API keys and
request bodies are never logged; hub URLs are logged without their query string, and the
`websockets` library is prevented from logging handshake headers.

## Examples

Read-only examples (set `TOPSTEPX_USERNAME` and `TOPSTEPX_API_KEY`):

```bash
python examples/list_accounts.py
python examples/contracts_and_bars.py ES
python examples/realtime_user.py 60
python examples/realtime_market.py CON.F.US.EP.U25 60
```

`examples/place_order.py` places and immediately cancels a limit order on a **simulated**
account, and refuses to run unless `TOPSTEPX_ALLOW_TRADING=1`.

## Development and tests

```bash
python3 -m venv .venv
.venv/bin/pip install -e '.[dev]'
.venv/bin/pytest -q                  # the whole suite runs in a few seconds
.venv/bin/mypy                       # strict, configured in pyproject.toml
.venv/bin/ruff check . && .venv/bin/ruff format --check .
.venv/bin/python -m build            # sdist and wheel (with py.typed)
```

The tests never contact the real API and need no credentials. HTTP behavior is tested with
`httpx.MockTransport` against both clients; realtime behavior against a local
`websockets` server speaking the SignalR JSON protocol (`tests/fake_signalr.py`); spec
conformance reads [`../spec/projectx-openapi.json`](../spec/projectx-openapi.json) (all 21
operations, every request and response example, a synthesized full-object round trip for
every object schema, and every enum's values and names).
