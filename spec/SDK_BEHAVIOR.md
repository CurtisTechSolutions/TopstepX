# Cross-SDK behavior contract

This repository ships three SDKs for the ProjectX Gateway API as deployed by
TopstepX: Rust (`rust/`), Python (`python/`) and Go (`go/`). They are
independent implementations of the **same behavior**. This document is that
behavior. Where an SDK deviates, its README must say so.

Sources, in priority order:

1. `projectx-openapi.json` / `projectx-openapi.yaml`: the REST contract (21 operations, 69 schemas).
2. `README.md` in this directory: the modeling decisions and known source conflicts.
3. `realtime.md`: the SignalR hub index.
4. `validation-report.json`: the negative cases every SDK must reproduce as tests.

The derived spec is not a vendor release. Anything the sources leave unspecified is left
unspecified here too, and the SDKs decode leniently where the sources are uncertain.

---

## 1. Packages

| SDK | Directory | Package | Import |
| --- | --- | --- | --- |
| Rust | `rust/` | crate `topstepx` | `use topstepx::...` |
| Python | `python/` | distribution `topstepx`, `src/` layout | `import topstepx` |
| Go | `go/` | module `github.com/CurtisTechSolutions/TopstepX/go` | `import topstepx "github.com/CurtisTechSolutions/TopstepX/go"` |

All three start at version `0.1.0` and are licensed `GPL-3.0-only` (repository `LICENSE`).

## 2. Configuration defaults

| Setting | Default |
| --- | --- |
| REST base URL | `https://api.topstepx.com` |
| User hub URL | `https://rtc.topstepx.com/hubs/user` |
| Market hub URL | `https://rtc.topstepx.com/hubs/market` |
| Request timeout | 30 s (connect timeout 10 s where the HTTP stack allows it) |
| Retries (see §7) | 3 |
| Retry backoff | 1 s initial, ×2 per attempt, capped at 30 s, no jitter |
| Client-side rate limiting (see §8) | enabled |
| Client-side request validation (see §9) | enabled |
| Token refresh margin (see §5) | 1 hour before expiry |
| User-Agent | `topstepx-rust/<ver>`, `topstepx-python/<ver>`, `topstepx-go/<ver>` |

Every URL is configurable because other firms run the same gateway on different hosts.
Examples read `TOPSTEPX_USERNAME` and `TOPSTEPX_API_KEY`. They also accept the optional
overrides `TOPSTEPX_API_URL`, `TOPSTEPX_USER_HUB_URL` and `TOPSTEPX_MARKET_HUB_URL`.

## 3. Operations

Method names use the same verb stems in every SDK: snake_case in Rust and Python,
PascalCase in Go. Rust exposes endpoint groups as `client.orders()`, Python as `client.orders`
and Go as `client.Orders`. Each high-level method checks the business result (see §4) and
returns the payload.

| operationId | HTTP | Group / method | Returns | Auth | Rate bucket | Retry class |
| --- | --- | --- | --- | --- | --- | --- |
| `Account_SearchAccounts` | POST `/api/Account/search` | accounts / `search(only_active_accounts)` | list of `TradingAccount` | bearer | general | read |
| `Auth_LoginApp` | POST `/api/Auth/loginApp` | auth / `login_app(request)` | token (stored) | none | general | write |
| `Auth_LoginKey` | POST `/api/Auth/loginKey` | auth / `login_key(user_name, api_key)` | token (stored) | none | general | write |
| `Auth_Logout` | POST `/api/Auth/logout` | auth / `logout()` | nothing; clears stored token | bearer | general | write |
| `Auth_Validate` | POST `/api/Auth/validate` | auth / `validate()` | new token (stored) | bearer | general | write |
| `Contract_SearchContracts` | POST `/api/Contract/search` | contracts / `search(search_text, live)` | list of `Contract` | bearer | general | read |
| `Contract_SearchContractById` | POST `/api/Contract/searchById` | contracts / `search_by_id(contract_id)` | `Contract` | bearer | general | read |
| `Contract_AvailableContracts` | POST `/api/Contract/available` | contracts / `available(live)` | list of `Contract` | bearer | general | read |
| `History_GetBars` | POST `/api/History/retrieveBars` | history / `retrieve_bars(request)` | list of `AggregateBar` | bearer | **history** | read |
| `Order_SearchOrders` | POST `/api/Order/search` | orders / `search(request)` | list of `Order` | bearer | general | read |
| `Order_SearchOrderById` | POST `/api/Order/searchById` | orders / `search_by_id(account_id, order_id)` | `Order` | bearer | general | read |
| `Order_SearchOpenOrders` | POST `/api/Order/searchOpen` | orders / `search_open(account_id)` | list of `Order` | bearer | general | read |
| `Order_SearchOrdersQuery` | POST `/api/Order/v2/query` | orders / `query(request)` | orders + optional `totalCount` | bearer | general | read |
| `Order_PlaceOrder` | POST `/api/Order/place` | orders / `place(request)` | order id (int64) | bearer | general | write |
| `Order_CancelOrder` | POST `/api/Order/cancel` | orders / `cancel(account_id, order_id)` | nothing | bearer | general | write |
| `Order_ModifyOrder` | POST `/api/Order/modify` | orders / `modify(request)` | nothing | bearer | general | write |
| `Position_SearchOpenPositions` | POST `/api/Position/searchOpen` | positions / `search_open(account_id)` | list of `Position` | bearer | general | read |
| `Position_CloseContractPosition` | POST `/api/Position/closeContract` | positions / `close(account_id, contract_id)` | nothing | bearer | general | write |
| `Position_PartialCloseContractPosition` | POST `/api/Position/partialCloseContract` | positions / `partial_close(account_id, contract_id, size)` | nothing | bearer | general | write |
| `Status_Ping` | GET `/api/Status/ping` | status / `ping()` | string | bearer if available (see §6) | general | read |
| `Trade_SearchHalfTurnTrades` | POST `/api/Trade/search` | trades / `search(request)` | list of `HalfTrade` | bearer | general | read |

Each SDK also provides a **low-level escape hatch**. It performs any of the operations
above and returns the complete typed response envelope **without** turning a business
failure into an error. HTTP, transport and decode errors are still raised. Callers use it
to read fields the high-level methods discard, such as the `orderId` of a rejected order.

`login_app` targets an operation the current authentication guide does not support.
Its documentation must say so and point to `login_key`.

## 4. Response envelope and business failures

Every JSON response except `Status_Ping` carries:

| Field | Parsing rule |
| --- | --- |
| `success` | required boolean |
| `errorCode` | required integer, typed with the operation's error-code enum (§10) |
| `errorMessage` | optional, nullable string |
| `hasBusinessFailure` | boolean, **defaults to false when absent** (the guides omit it) |

A response is a **business failure** when `success == false` **or** `errorCode != 0`.
High-level methods raise or return an `ApiError` carrying:

- the operation (path and/or operationId);
- the raw integer code **and** its enum name, or `Unrecognized(<n>)` for values the enum
  lacks;
- `errorMessage`, which may be null;
- for `Order_PlaceOrder`, the `orderId` when one was returned. A returned identifier does
  not establish acceptance.

Business failures arrive with HTTP 200, including invalid login credentials
(`errorCode` 3).

Response parsing is tolerant: unknown JSON fields are ignored. Absent optional fields and
absent arrays decode as "none" or empty. The body is parsed as JSON whatever the
`Content-Type` (`text/plain`, `application/json` or `text/json`).

## 5. Authentication and session lifecycle

- **Credentials**: platform username and API key. The API key and every token are
  secrets. They must never appear in `Debug`, `repr`, `%v` or log output.
- **Login** (`login_key`) POSTs `{"userName": ..., "apiKey": ...}`. On success it stores the
  returned `token`. On a business failure it raises `ApiError` and stores nothing.
- **Pre-existing token**: a client may be built with a token instead of, or in addition to,
  credentials.
- **Expiry** comes from the JWT `exp` claim. Decode the base64url payload segment; never
  verify the signature. If the token is not a decodable JWT or has no `exp`, assume it
  expires 24 hours after it was obtained.
- **`ensure_token()`** runs before every authenticated request:
  1. No token: log in if credentials are configured. Otherwise fail with a
     *not authenticated* error before any network I/O.
  2. Token already expired (`now >= exp`): log in if credentials are configured. Otherwise
     send the request with the current token and let the server decide, which tolerates
     clock skew.
  3. Token inside the refresh margin (`now >= exp - margin`): call `validate`. If it
     succeeds with a non-empty `newToken`, store the new token. If it fails in any way,
     log in when credentials are configured, and otherwise keep using the current,
     still-valid token.
  4. Otherwise use the stored token.

  Refreshes are **single-flight**. Concurrent callers wait for one refresh or login
  instead of stampeding.
- **HTTP 401 on an authenticated request**: if credentials are configured and this
  request has not already been retried for a 401, log in again and retry the request once.
  If another caller replaced the token in the meantime, retry with that token instead of
  logging in again. Otherwise raise an *unauthorized* error. A 401 means the request was
  never processed, so retrying is safe even for writes.
- **`validate()`** called explicitly sends the current token and stores `newToken` on
  success.
- **`logout()`** calls the endpoint, then clears the stored token whatever the result.
  A business failure is still raised. A later authenticated call logs in again if
  credentials are configured.
- The client exposes the current token through an accessor that runs `ensure_token()`.
  The realtime hubs use this accessor as their token provider.

## 6. HTTP details

- POST bodies are JSON (`Content-Type: application/json`) with camelCase names exactly as
  in the spec. Absent or null optional request fields are **omitted**.
- Requests send `Accept: application/json`.
- `Auth_Validate` and `Auth_Logout` are POSTs with an **empty body**. No request body is
  documented for them.
- `Status_Ping` is a GET. The response may be `text/plain` (`pong`) or a JSON string
  (`"pong"`). Return the plain string in both cases. Attach the bearer token when one is
  held or obtainable; otherwise ping anonymously rather than failing, because anonymous
  access is unverified either way.
- Error mapping for non-2xx responses:
  - **400**: HTTP status error with the body text. When the body is RFC 7807
    `ProblemDetails` JSON (as `Order_SearchOrdersQuery` documents), expose it parsed,
    including extension members.
  - **401**: *unauthorized* error, after the re-login rule in §5.
  - **429**: *rate limited* error, after the retries in §7. Expose `Retry-After` when present.
  - **Anything else**: HTTP status error with status and body text.
- Transport failures such as DNS, connection, TLS or timeout errors surface as a
  transport error.
- A body that cannot be decoded surfaces as a decode error carrying the body text.

## 7. Retries

- **429**: retry any operation up to `max_retries` times. A 429 means the request was
  throttled before processing. Wait `Retry-After` when present, in delta-seconds or
  HTTP-date form. Otherwise use the backoff (1 s, 2 s, 4 s, … capped at 30 s).
- **502, 503, 504 and transport errors**: retry only operations in the **read** retry
  class (§3), with the same budget and backoff. Never retry a write for these conditions,
  because it may already have executed.
- **401** follows §5 and does not consume the retry budget.
- Nothing else is retried.
- Setting `max_retries = 0` disables retries.

## 8. Client-side rate limiting

The documented limits are: `History_GetBars`, 50 requests per 30 s; every other
operation, 200 requests per 60 s.

Each client instance enforces them with a **sliding window** per bucket. Before each
HTTP attempt, including retries and logins, it acquires a slot. When the window is full
it waits until the oldest entry expires. The limiter is enabled by default. Callers can
disable it or replace the limits. It is per client instance, so separate processes do not
share it, and the README must say so.

## 9. Client-side validation

Validation runs before any network I/O, before login too. It is enabled by default and
can be disabled per client, because the server is the final authority. These rules come
from the spec, and each reproduces a case in `validation-report.json`:

1. `PlaceOrderRequest.type` must be Limit (1), Market (2), Stop (4), TrailingStop (5),
   JoinBid (6) or JoinAsk (7). StopLimit (3), Unknown (0) and unrecognized values are
   rejected. *(validation-report: "placement rejects unsupported StopLimit")*
2. `PlaceOrderRequest.type == 5` requires a present, non-null `trailPrice`.
   *(validation-report: "trailing-stop requires trailPrice" and "trailing-stop rejects
   null trailPrice")*
3. `stopLossBracket.type` and `takeProfitBracket.type` must come from the same six values.
4. `RetrieveBarRequest.limit <= 20000`. *(validation-report: "bar count cannot exceed
   20000")*
5. `PartialCloseContractPositionRequest.size >= 1`. *(validation-report: "partial close
   rejects zero")*

No other rules are invented. Typed convenience constructors, such as a limit-order
constructor that takes a limit price, are encouraged; that is ergonomics, not validation.

## 10. Models and types

### Naming

Request, response and enum types keep their spec schema names. Object models drop the
`Model` suffix:

| Spec schema | SDK type |
| --- | --- |
| `TradingAccountModel` | `TradingAccount` |
| `ContractModel` | `Contract` |
| `AggregateBarModel` | `AggregateBar` |
| `OrderModel` | `Order` |
| `PositionModel` | `Position` |
| `HalfTradeModel` | `HalfTrade` |

Go applies its initialism convention to identifiers (`SearchContractByIDRequest`,
`AccountID`), but JSON tags stay exactly as in the spec.

### Type mapping

| Spec | Rust | Python | Go |
| --- | --- | --- | --- |
| integer/int32 | `i32` | `int` | `int32` |
| integer/int64 | `i64` | `int` | `int64` |
| number/decimal | `rust_decimal::Decimal` | `decimal.Decimal` | `shopspring/decimal.Decimal` |
| string/date-time | `chrono::DateTime<Utc>` | aware `datetime` in UTC | `time.Time` (UTC) |
| nullable or optional | `Option<T>` | `T \| None` | pointer (`*T`) |

- **Decimals** are sent as JSON **numbers**, never quoted strings. They are decoded from
  JSON numbers without losing the printed digits: `6000.25` decodes to exactly `6000.25`.
- **Timestamps** are sent as RFC 3339 UTC with a `Z` suffix. Decoding accepts `Z` or any
  numeric offset, 0 to 9 fractional digits (.NET emits up to 7), and a missing offset,
  which is read as UTC.
- **Optional fields**: every field the spec marks nullable or not-required is optional.
  One deliberate leniency applies: `Order.updateTimestamp` is required by the spec but
  modeled as optional, because the SDK must not fail on a null update time. Missing
  arrays decode as empty.
- `AggregateBar.d` (string) and `AggregateBar.k` (int64) are undocumented. They are
  exposed raw and optional, and their semantics are not guessed.
- `ProblemDetails` keeps unknown members as extensions.

### Enums

Enums are integer-valued and must **tolerate unrecognized values** on decode. Rust uses
`Unrecognized(i32)`. Python uses an `IntEnum` with a pseudo-member. Go uses a named
integer type. Each enum has a name accessor, and the (value, name) pairs must equal the
spec's `enum` and `x-enumNames` exactly:

| Enum | Values |
| --- | --- |
| `AggregateBarUnit` | 0 Unspecified, 1 Second, 2 Minute, 3 Hour, 4 Day, 5 Week, 6 Month, 7 Tick |
| `OrderStatus` | 0 None, 1 Open, 2 Filled, 3 Cancelled, 4 Expired, 5 Rejected, 6 Pending, 7 PendingCancellation, 8 Suspended |
| `OrderType` | 0 Unknown, 1 Limit, 2 Market, 3 StopLimit, 4 Stop, 5 TrailingStop, 6 JoinBid, 7 JoinAsk |
| `OrderSide` | 0 Bid (buy), 1 Ask (sell) |
| `PositionType` | 0 Undefined, 1 Long, 2 Short |
| `OrderSortField` | 0 CreatedAt, 1 Id |
| `SortDirection` | 0 Asc, 1 Desc |

Every operation's error-code enum follows the spec (`LoginErrorCode`,
`PlaceOrderErrorCode`, and so on, 17 in total). `SearchOrderErrorCode` is shared by
`Order_SearchOrders`, `Order_SearchOpenOrders` and `Order_SearchOrdersQuery`.

In `Order.trailPrice` the response value is a price **distance**. In requests,
`trailPrice` is an **absolute** price. Document this on both.

## 11. Realtime (SignalR)

### Hubs and methods

| Hub | Subscribe method (argument) | Server event | Event arguments |
| --- | --- | --- | --- |
| user | `SubscribeAccounts` () | `GatewayUserAccount` | `(data)` |
| user | `SubscribeOrders` (accountId: int) | `GatewayUserOrder` | `(data)` |
| user | `SubscribePositions` (accountId: int) | `GatewayUserPosition` | `(data)` |
| user | `SubscribeTrades` (accountId: int) | `GatewayUserTrade` | `(data)` |
| market | `SubscribeContractQuotes` (contractId: string) | `GatewayQuote` | `(contractId, data)` |
| market | `SubscribeContractTrades` (contractId: string) | `GatewayTrade` | `(contractId, data)` |
| market | `SubscribeContractMarketDepth` (contractId: string) | `GatewayDepth` | `(contractId, data)` |

Every `Subscribe*` method has an `Unsubscribe*` counterpart with the same argument.

### Payload models

The payload models come from the official realtime guide. `realtime.md` does not repeat
their fields, and REST models are **not** assumed identical. Every field that is not an
identifier is optional, because payloads may be partial. Quote updates in particular can
carry only the fields that changed.

- `GatewayUserAccount`: id (int32), name, balance (decimal), canTrade, isVisible, simulated.
- `GatewayUserPosition`: id (int32), accountId, contractId, creationTimestamp, type
  (`PositionType`), size (int32), averagePrice (decimal).
- `GatewayUserOrder`: id (int64), accountId, contractId, symbolId, creationTimestamp,
  updateTimestamp, status, type, side, size, limitPrice, stopPrice, fillVolume, filledPrice,
  customTag.
- `GatewayUserTrade`: id (int64), accountId, contractId, creationTimestamp, price,
  profitAndLoss (null for a half-turn), fees, side, size, voided, orderId.
- `GatewayQuote`: symbol, symbolName, lastPrice, bestBid, bestAsk, change, changePercent,
  open, high, low, volume, lastUpdated, timestamp.
- `GatewayDepth` entry: timestamp, type (`DomType`), price, volume, currentVolume.
- `GatewayTrade` entry: symbolId, price, timestamp, type (`TradeLogType`), volume.
- `DomType`: 0 Unknown, 1 Ask, 2 Bid, 3 BestAsk, 4 BestBid, 5 Trade, 6 Reset, 7 Low,
  8 High, 9 NewBestBid, 10 NewBestAsk, 11 Fill.
- `TradeLogType`: 0 Buy, 1 Sell.
- Volumes decode leniently to an integer, whether the JSON is an integer, an integral
  float such as `3.0`, or a numeric string.

### Lenient decoding

A malformed or unexpected payload must never break the connection:

- A user-hub payload may arrive bare (`{...}`), wrapped as `{"action": n, "data": {...}}`,
  or as an array of either form. Unwrap the envelope, expose `action` when present
  (meaning undocumented), and emit one event per element.
- A `GatewayTrade` or `GatewayDepth` payload may be a single object or an array. Normalize
  it to a list delivered as **one** event per server message.
- Unknown targets, and payloads that fail to decode, are delivered as an *unknown* event
  carrying the target, the raw arguments and the decode error.

### Protocol

The client implements the ASP.NET Core SignalR **JSON hub protocol v1** over WebSockets
directly, without depending on a SignalR library:

- **Connect**: convert the hub URL's `https`/`http` scheme to `wss`/`ws` and append
  `access_token=<token>` to the query string, URL-encoded. Skip negotiation, as the
  official guide does with `skipNegotiation: true`. Also send `Authorization: Bearer
  <token>` on the upgrade request.
- **Handshake**: send `{"protocol":"json","version":1}` followed by the record separator
  `0x1E`. Wait for the handshake response, with a 15 s timeout. `{}` means success; an
  `error` member fails the connect. Messages may share a frame with the handshake
  response and must still be processed.
- **Framing**: every message is a JSON text followed by `0x1E`. A frame can carry several
  messages. Buffer any incomplete trailing text until the next frame. Accept both text and
  binary frames.
- **Messages**:
  - Invocation (type 1). Client-to-server calls carry an increasing string `invocationId`.
    Server-to-client invocations carry none and become events.
  - Completion (type 3) resolves the pending invocation, with a `result` or an `error`.
  - Ping (type 6) is sent every **15 s** since the last message the client sent.
  - Close (type 7) carries an optional `error` and `allowReconnect`.
  - Stream types 2, 4 and 5, and anything unknown, are ignored.
- **Timeouts**: invocations time out after 30 s. If nothing arrives from the server for
  **30 s**, the connection counts as lost.
- **Reconnect**: on connection loss, or a close with `allowReconnect: true`, reconnect with
  delays of 0, 1, 2, 5 and 10 s, then every 30 s, with unlimited attempts by default. The
  policy is configurable, and zero attempts disables reconnect. Each attempt fetches a
  **fresh token** from the token provider. A close without `allowReconnect` ends the
  connection.
- **Pending invocations** fail with a *connection lost* error when the connection drops.
- **Subscription registry**: a successful `Subscribe*` is recorded, de-duplicated by
  (method, arguments). A successful `Unsubscribe*` removes the entry. After every
  reconnect the client replays the registry in insertion order, then emits *reconnected*.
  A failed replay is surfaced as an event, not swallowed. Invocations while disconnected
  fail fast with a *not connected* error.
- **State events**: *reconnecting* (attempt number and cause), *reconnected* and *closed*
  (cause) are delivered in the same stream as data events.
  - Callback-style APIs registered before connecting also report *connected*.
  - A pull-style stream is created after the connect call returns, so it may treat that
    successful return as the *connected* signal instead of emitting an event.
- **Backpressure**: event delivery never blocks the read loop or keepalive. A consumer
  that falls behind loses events, and the SDK makes the loss observable through a lag
  error, a dropped counter or a log line.
- **`close()`** stops reconnecting, sends a close message (type 7), closes the socket,
  fails pending invocations and ends the event stream.

## 12. Testing requirements

Every SDK must pass the following without network access and without credentials:

1. **Spec conformance** against `spec/projectx-openapi.json`:
   - all 21 operationIds are covered with the correct HTTP method and path;
   - every request example decodes into its request model and re-encodes to equivalent
     JSON; equivalence treats null and absent as equal, numbers by value and timestamps
     by instant;
   - every HTTP 200 response example decodes into its response model and re-encodes to
     equivalent JSON;
   - every object schema survives a **synthesized full-object round trip**. Generate a
     JSON object holding every property, with type-appropriate values that pass §9, then
     decode, re-encode and compare. This catches fields the model is missing or has in
     excess;
   - every enum's (value, name) pairs equal the spec's `enum` and `x-enumNames`.
2. **Validation**: the five `validation-report.json` negative cases, plus positive cases.
3. **HTTP behavior** against a local mock server:
   - lazy login and the bearer header;
   - a business failure from HTTP 200 becomes `ApiError`, including invalid credentials
     and a rejected order that still returns an `orderId`;
   - 401 leads to one re-login and retry;
   - 429 is retried and honors `Retry-After`;
   - write operations are not retried on 503;
   - a 400 `ProblemDetails` body is parsed;
   - ping works as text/plain and as JSON;
   - the refresh-margin path calls `validate`;
   - `logout` clears the token.
4. **Realtime** against a local WebSocket server that speaks the protocol:
   - handshake;
   - invoke and completion, success and error;
   - event dispatch, including the envelope, array and bare forms;
   - an unknown target;
   - keepalive ping;
   - reconnect with a fresh token and subscription replay;
   - `close()`.

No test or example may contact the real API. Examples that place orders must refuse to
run unless the environment variable `TOPSTEPX_ALLOW_TRADING=1` is set.

## 13. Documentation

Each SDK README covers:

- installation and a quickstart;
- authentication and token lifecycle;
- every endpoint group;
- realtime usage;
- error handling;
- retries and rate limits;
- validation;
- decimals and timestamps;
- the spec's unknowns and deviations: swagger-only operations, the realtime payload
  provenance and lenient decoding, and undocumented bar fields;
- a safety note: orders are real, so use simulated accounts while developing;
- how to run the tests.

Public API items carry doc comments.
