# topstepx — Go SDK for the ProjectX Gateway API (TopstepX)

A Go client for the [ProjectX Gateway API](https://gateway.docs.projectx.com/docs/intro/)
as deployed by TopstepX: all 21 REST operations, the SignalR user and market hubs,
token lifecycle management, retries, client-side rate limiting and request validation.

This SDK implements the cross-SDK behavior contract in
[`../spec/SDK_BEHAVIOR.md`](../spec/SDK_BEHAVIOR.md); the Rust (`../rust`) and
Python (`../python`) SDKs implement the same behavior. Where this SDK deviates or
decides something the contract leaves open, [Deviations and Go-specific
decisions](#deviations-and-go-specific-decisions) says so.

- Module: `github.com/CurtisTechSolutions/TopstepX/go` (package `topstepx`), realtime
  hubs in `github.com/CurtisTechSolutions/TopstepX/go/realtime`
- Version 0.1.0, license GPL-3.0-only (see the repository `LICENSE`)
- Go 1.22 or later. Runtime dependencies: `github.com/shopspring/decimal` and
  `github.com/coder/websocket` (v1.8.13, the last release that supports Go 1.22)

> **Orders are real.** `Orders.Place`, `Cancel`, `Modify` and `Positions.Close` /
> `PartialClose` act on real accounts. Use a simulated account while developing.

## Installation

```sh
go get github.com/CurtisTechSolutions/TopstepX/go
```

```go
import (
	topstepx "github.com/CurtisTechSolutions/TopstepX/go"
	"github.com/CurtisTechSolutions/TopstepX/go/realtime"
)
```

## Quick start

```go
client, err := topstepx.NewClient(
	topstepx.WithCredentials(os.Getenv("TOPSTEPX_USERNAME"), os.Getenv("TOPSTEPX_API_KEY")),
)
if err != nil {
	log.Fatal(err)
}
ctx := context.Background()

accounts, err := client.Accounts.Search(ctx, true) // logs in lazily on first use
if err != nil {
	log.Fatal(err)
}
for _, a := range accounts {
	fmt.Println(a.ID, a.Name, a.Balance, a.Simulated)
}

contracts, _ := client.Contracts.Search(ctx, "ES", false)
bars, _ := client.History.RetrieveBars(ctx, topstepx.RetrieveBarRequest{
	ContractID: contracts[0].ID,
	StartTime:  time.Now().Add(-24 * time.Hour),
	EndTime:    time.Now(),
	Unit:       topstepx.AggregateBarUnitMinute,
	UnitNumber: 5,
	Limit:      100,
})
```

Every network method takes a `context.Context` first. A `*Client` is safe for
concurrent use; create one per process (or per account) and share it.

### Configuration

| Option | Default |
| --- | --- |
| `WithBaseURL(url)` | `https://api.topstepx.com` |
| `WithUserHubURL(url)` / `WithMarketHubURL(url)` | `https://rtc.topstepx.com/hubs/user` / `.../hubs/market` |
| `WithCredentials(userName, apiKey)` / `WithToken(token)` | none |
| `WithTimeout(d)` | 30 s per HTTP attempt (the default HTTP client also has a 10 s connect / TLS timeout) |
| `WithMaxRetries(n)` | 3 (`0` disables retries) |
| `WithRetryBackoff(initial, max)` | 1 s, doubling, capped at 30 s, no jitter |
| `WithRateLimits(limits)` / `WithoutRateLimiting()` | enabled: history 50 / 30 s, everything else 200 / 60 s |
| `WithoutValidation()` | validation enabled |
| `WithRefreshMargin(d)` | 1 h before expiry |
| `WithUserAgent(ua)` | `topstepx-go/0.1.0` |
| `WithHTTPClient(c)` | a dedicated client with connect/TLS timeouts |
| `WithLogger(*slog.Logger)` | discard |

Every URL is configurable because other firms run the same gateway on other hosts.
The examples read `TOPSTEPX_USERNAME` and `TOPSTEPX_API_KEY`, plus the optional
`TOPSTEPX_API_URL`, `TOPSTEPX_USER_HUB_URL` and `TOPSTEPX_MARKET_HUB_URL`.

## Authentication and token lifecycle

Log in with your platform username and an API key (`Auth_LoginKey`). With
`WithCredentials`, the client does this lazily before the first authenticated call.
You can also start from an existing token with `WithToken`, alone or together with
credentials.

Before every authenticated request the client runs *ensure-token*:

1. No token: log in when credentials are configured, otherwise fail with
   `ErrNotAuthenticated` before any network I/O.
2. Token expired (`now >= exp`): log in when credentials are configured; otherwise send
   the request with the current token and let the server decide (clock skew).
3. Token inside the refresh margin (`now >= exp - 1h`): call `Auth_Validate` and store
   its `newToken`. If that fails, log in when credentials are configured, and otherwise
   keep using the current, still-valid token.
4. Otherwise use the stored token.

Expiry comes from the JWT `exp` claim (decoded, never verified); a token that is not a
decodable JWT is assumed to last 24 hours. Refreshes are **single-flight**: concurrent
callers wait for one login or validate instead of stampeding.

An HTTP 401 on an authenticated request triggers **one** re-login and retry when
credentials are configured (or a retry with a token another caller stored meanwhile);
a second 401 returns an error matching `ErrUnauthorized`. A 401 never consumes the
retry budget.

```go
token, err := client.Token(ctx)           // ensure-token; also the realtime token provider
token, err = client.Auth.LoginKey(ctx, user, apiKey)
token, err = client.Auth.Validate(ctx)    // sends the current token, stores newToken
err = client.Auth.Logout(ctx)             // calls the endpoint, then clears the token (no-op without one)
client.InvalidateToken(token)             // discard a token the gateway rejected, if still stored
```

`client.InvalidateToken(token)` discards `token` only if it is still the stored token (a
stale report never discards a newer one). The next call then logs in again when
credentials are configured and fails with `ErrNotAuthenticated` otherwise. The realtime
hubs call it when the gateway rejects a token (see [Rejected tokens](#realtime-signalr)).

`Auth.LoginApp` targets `Auth_LoginApp`, which appears only in Swagger: **the current
authentication guide does not support application login**; use `LoginKey`.

The API key and tokens are secrets. `Client`, `LoginAPIKeyRequest`, `LoginAppRequest`,
`LoginResponse` and `ValidateResponse` redact them for every `fmt` verb (`%v`, `%+v`,
`%#v`, ...) and in `slog` output, error messages never contain them, and the SDK never
logs them.

## Endpoint groups

Each high-level method checks the business result (see [Error handling](#error-handling))
and returns the payload.

| Group | Method | Operation |
| --- | --- | --- |
| `client.Accounts` | `Search(ctx, onlyActiveAccounts) ([]TradingAccount, error)` | `POST /api/Account/search` |
| `client.Auth` | `LoginKey(ctx, userName, apiKey) (string, error)` | `POST /api/Auth/loginKey` |
| | `LoginApp(ctx, LoginAppRequest) (string, error)` *(Swagger only, unsupported by the guide)* | `POST /api/Auth/loginApp` |
| | `Validate(ctx) (string, error)` | `POST /api/Auth/validate` (empty body) |
| | `Logout(ctx) error` *(Swagger only)* | `POST /api/Auth/logout` (empty body) |
| `client.Contracts` | `Search(ctx, searchText, live) ([]Contract, error)` | `POST /api/Contract/search` |
| | `SearchByID(ctx, contractID) (*Contract, error)` | `POST /api/Contract/searchById` |
| | `Available(ctx, live) ([]Contract, error)` | `POST /api/Contract/available` |
| `client.History` | `RetrieveBars(ctx, RetrieveBarRequest) ([]AggregateBar, error)` | `POST /api/History/retrieveBars` |
| `client.Orders` | `Search(ctx, SearchOrderRequest) ([]Order, error)` | `POST /api/Order/search` |
| | `SearchByID(ctx, accountID, orderID) (*Order, error)` *(Swagger only)* | `POST /api/Order/searchById` |
| | `SearchOpen(ctx, accountID) ([]Order, error)` | `POST /api/Order/searchOpen` |
| | `Query(ctx, SearchOrdersQueryRequest) (*OrdersQueryResult, error)` *(Swagger only)* | `POST /api/Order/v2/query` |
| | `Place(ctx, PlaceOrderRequest) (int64, error)` | `POST /api/Order/place` |
| | `Cancel(ctx, accountID, orderID) error` | `POST /api/Order/cancel` |
| | `Modify(ctx, ModifyOrderRequest) error` | `POST /api/Order/modify` |
| `client.Positions` | `SearchOpen(ctx, accountID) ([]Position, error)` | `POST /api/Position/searchOpen` |
| | `Close(ctx, accountID, contractID) error` | `POST /api/Position/closeContract` |
| | `PartialClose(ctx, accountID, contractID, size) error` | `POST /api/Position/partialCloseContract` |
| `client.Status` | `Ping(ctx) (string, error)` *(Swagger only)* | `GET /api/Status/ping` |
| `client.Trades` | `Search(ctx, SearchTradeRequest) ([]HalfTrade, error)` | `POST /api/Trade/search` |

The `live` flag of the contract and bar methods selects the live or simulated
market-data subscription; it says nothing about whether an account is simulated.

`Status.Ping` returns `"pong"` whether the gateway answers `text/plain` or a JSON
string. It attaches a bearer token when one is held or obtainable. If obtaining one fails
for any reason other than cancellation, including a failed login, it pings anonymously
(anonymous access is unverified either way).

### Placing orders

```go
req := topstepx.NewLimitOrder(accountID, contractID, topstepx.OrderSideBid, 1,
	decimal.RequireFromString("6000.25")).
	WithCustomTag("my-unique-tag").
	WithStopLossBracket(8, topstepx.OrderTypeStop).
	WithTakeProfitBracket(16, topstepx.OrderTypeLimit)
orderID, err := client.Orders.Place(ctx, req)
```

Constructors exist for every supported type: `NewMarketOrder`, `NewLimitOrder`,
`NewStopOrder`, `NewTrailingStopOrder`, `NewJoinBidOrder` and `NewJoinAskOrder`.
Brackets require the account's Auto OCO Brackets mode. `customTag` must be unique within
the account.

**Trailing stops:** in requests (`PlaceOrderRequest.TrailPrice`,
`ModifyOrderRequest.TrailPrice`) `trailPrice` is an **absolute** stop price aligned to
the tick size. In responses (`Order.TrailPrice`) it is a price **distance**.

Optional fields are pointers; `topstepx.Ptr` fills them:
`ModifyOrderRequest{AccountID: 1, OrderID: 2, Size: topstepx.Ptr(int32(3))}`. Nil
fields are omitted from the request.

### Low-level escape hatch

`client.Do(ctx, op, req, resp)` performs any operation and decodes the **complete
envelope**, without turning a business failure into an error. HTTP, transport, decode
and validation errors are still returned, and authentication, rate limiting and retries
behave as for the high-level methods. Use it to read fields the high-level methods
discard, such as the `orderId` of a rejected order:

```go
var resp topstepx.PlaceOrderResponse
if err := client.Do(ctx, topstepx.OpOrderPlaceOrder, req, &resp); err != nil {
	return err // HTTP/transport/decode/validation error
}
if resp.IsBusinessFailure() {
	fmt.Println("rejected", resp.ErrorCode, resp.OrderID)
}
```

`topstepx.Operations()` describes all 21 operations (method, path, auth mode, rate
bucket, retry class). `Do` does not change the stored session.

## Realtime (SignalR)

The `realtime` package implements the ASP.NET Core SignalR JSON hub protocol v1 over
WebSockets itself (no SignalR library): it skips negotiation, sends the token both as
the URL-encoded `access_token` query parameter and as a bearer header, pings after 15 s
of client silence, treats 30 s of server silence as a lost connection and times
invocations out after 30 s.

```go
hub := realtime.NewUserHubForClient(client) // token provider = client.Token
if err := hub.Connect(ctx); err != nil {
	return err
}
defer hub.Close()
if err := hub.SubscribeOrders(ctx, accountID); err != nil {
	return err
}
for ev := range hub.Events() {
	switch ev := ev.(type) {
	case *realtime.OrderEvent:
		fmt.Println(ev.Order.ID, ev.Order.Status)
	case *realtime.StateEvent:
		fmt.Println("state:", ev.State, ev.Attempt, ev.Err)
	case *realtime.UnknownEvent:
		fmt.Println("undecodable", ev.Target, ev.Err)
	}
}
```

| Hub | Methods (each with an `Unsubscribe*` counterpart) | Events |
| --- | --- | --- |
| `UserHub` | `SubscribeAccounts(ctx)`, `SubscribeOrders(ctx, accountID)`, `SubscribePositions(ctx, accountID)`, `SubscribeTrades(ctx, accountID)` | `*AccountEvent`, `*OrderEvent`, `*PositionEvent`, `*UserTradeEvent` |
| `MarketHub` | `SubscribeContractQuotes(ctx, contractID)`, `SubscribeContractTrades(ctx, contractID)`, `SubscribeContractMarketDepth(ctx, contractID)` | `*QuoteEvent`, `*MarketTradesEvent`, `*DepthEvent` |

Both hubs also deliver `*StateEvent` (connected, reconnecting with attempt and cause,
reconnected, closed with cause), `*SubscriptionErrorEvent` (a failed replay after a
reconnect) and `*UnknownEvent` (unknown targets and undecodable payloads) on the same
channel. `hub.Invoke(ctx, method, args...)` calls any hub method.

- **Reconnect**: after a lost connection or a server close with `allowReconnect: true`,
  the hub reconnects with delays of 0, 1, 2, 5 and 10 s and then every 30 s, without an
  attempt limit, fetching a **fresh token** for every attempt. Configure it with
  `WithReconnectPolicy(realtime.ReconnectPolicy{...})` (`MaxAttempts: 0` disables it) or
  `WithoutReconnect()`. A close without `allowReconnect` ends the connection.
- **Rejected tokens**: when the WebSocket upgrade is rejected with HTTP 401, on the
  initial `Connect` or on any reconnect attempt, the hub reports the rejected token
  through `WithTokenRejected(func(token string))` before the next attempt.
  `NewUserHubForClient` and `NewMarketHubForClient` wire this to
  `client.InvalidateToken`, so the next attempt logs in again instead of reusing the
  token until it expires. The attempt fails with a `*realtime.DialError` whose
  `StatusCode` is 401 and which matches `realtime.ErrUnauthorized` (the same value as
  `topstepx.ErrUnauthorized`); a failed `Connect` returns it, a reconnect attempt reports
  it as the cause of the next `StateReconnecting` event. With a plain `TokenProvider` and
  no hook the hub works as before and still reports the 401 distinctly.
- **Subscriptions** that succeed are recorded (deduplicated by method and arguments;
  a successful unsubscribe removes them) and replayed in insertion order after every
  reconnect, before the `StateReconnected` event. Invocations while disconnected or
  reconnecting fail fast with `ErrNotConnected`; invocations pending when the connection
  drops fail with `ErrConnectionLost`.
- **Backpressure**: delivery never blocks the connection. When the buffered channel
  (`WithEventBuffer`, default 1024) is full, the oldest event is dropped
  (`WithOverflowPolicy(realtime.DropNewest)` drops the incoming one instead); `Dropped()`
  counts losses and the logger reports them.
- **`Close()`** stops reconnecting, sends a SignalR close message, closes the socket,
  fails pending invocations with `ErrClosed`, waits for the hub's goroutines and closes
  the event channel after a final `StateClosed` event. It is idempotent.
- Timings are configurable (`WithKeepAliveInterval`, `WithServerTimeout`,
  `WithHandshakeTimeout`, `WithInvocationTimeout`), as are `WithURL`, `WithHTTPClient`,
  `WithReadLimit` and `WithLogger`.

**Payload provenance and lenient decoding.** The payload models (`GatewayUserAccount`,
`GatewayUserOrder`, `GatewayUserPosition`, `GatewayUserTrade`, `GatewayQuote`,
`GatewayTrade`, `GatewayDepth`, plus `DomType` and `TradeLogType`) follow the official
realtime guide; they are not assumed to equal the REST models. Every field except
identifiers is a pointer because updates may be partial (quotes often carry only the
changed fields). User-hub payloads may arrive bare, wrapped as
`{"action": n, "data": {...}}` (the undocumented `action` is exposed as `Action`), or as
arrays of either: each element becomes one event. Market trades and depth arrive as one
object or an array and are normalized to a list in **one** event per message. Volumes
accept integers, integral floats such as `3.0` and numeric strings. A payload that
cannot be decoded arrives as `*UnknownEvent` with the raw arguments and the error; the
connection is unaffected. Every data event also carries the `Raw` JSON.

## Error handling

| Condition | Error | Match with |
| --- | --- | --- |
| `success == false` or `errorCode != 0` (arrives with HTTP 200, including invalid credentials, errorCode 3) | `*APIError`: `Operation`, `Code`, `CodeName` (or `Unrecognized(<n>)`), `Message` (may be nil), `OrderID` (place only), `Response` (full envelope) | `errors.As` |
| HTTP 401 after the single re-login | `*HTTPError` (`StatusCode` 401, `Err` = re-login failure if any) | `errors.Is(err, topstepx.ErrUnauthorized)` |
| HTTP 429 after the retries | `*RateLimitError` (`RetryAfter` when sent) | `errors.Is(err, topstepx.ErrRateLimited)` |
| HTTP 400 and other statuses | `*HTTPError` with `Body`, and `Problem` (RFC 7807 `ProblemDetails`, extensions included) when the body is one | `errors.As` |
| No token and no credentials | `ErrNotAuthenticated` (before any I/O) | `errors.Is` |
| Client-side validation | `*ValidationError` (`Field`, `Reason`) | `errors.As` |
| Undecodable body, or a successful response without its payload | `*DecodeError` with `Body` | `errors.As` |
| DNS, connection, TLS, timeout | `*TransportError` wrapping the cause | `errors.As`; `errors.Is(err, context.DeadlineExceeded)` works |
| Realtime | `realtime.ErrNotConnected`, `ErrConnectionLost`, `ErrClosed`, `ErrInvocationTimeout`, `*HubError` (invocation error), `*HandshakeError`, `*DialError` (token-free message, `StatusCode`; an upgrade rejected with 401 matches `realtime.ErrUnauthorized` / `topstepx.ErrUnauthorized`), `*ServerCloseError` | `errors.Is` / `errors.As` |

A returned `orderId` does not establish acceptance: a rejected order is an `*APIError`
whose `OrderID` may be set.

```go
id, err := client.Orders.Place(ctx, req)
var apiErr *topstepx.APIError
switch {
case errors.As(err, &apiErr):
	log.Printf("rejected: %s (code %d) orderId=%v", apiErr.CodeName, apiErr.Code, apiErr.OrderID)
case errors.Is(err, topstepx.ErrRateLimited):
	// back off
case err != nil:
	log.Print(err)
}
```

Responses are parsed as JSON whatever the `Content-Type` (`text/plain`,
`application/json`, `text/json`); unknown members are ignored, `hasBusinessFailure`
defaults to false and enums tolerate unknown values (`OrderStatus(42).String()` is
`"Unrecognized(42)"`).

## Retries and rate limits

- **HTTP 429**: every operation is retried up to `WithMaxRetries` times (default 3),
  waiting for `Retry-After` (delta-seconds or HTTP-date) when present and otherwise for
  the backoff: 1 s, 2 s, 4 s, ... capped at 30 s, without jitter.
- **HTTP 502, 503, 504 and transport errors**: only *read* operations are retried, with
  the same budget and backoff. Writes (login, validate, logout, place, cancel, modify,
  close, partial close) are never retried for these, because they may already have run.
- HTTP 401 follows the re-login rule and does not consume the budget. Nothing else is
  retried.

Client-side rate limiting uses a **sliding window** per bucket: `History_GetBars` 50
requests per 30 s, every other operation 200 per 60 s. A slot is taken before every HTTP
attempt, including retries and logins; when the window is full the call waits (honoring
its context) until the oldest entry expires. The limiter is **per `Client` instance**:
separate clients or processes do not share it, so share one client per account.
Replace the limits with `WithRateLimits` or disable them with `WithoutRateLimiting`.

## Validation

Validation runs before any network I/O, login included, and returns a
`*ValidationError`. It reproduces the spec's rules and nothing more:

1. `PlaceOrderRequest.Type` must be Limit, Market, Stop, TrailingStop, JoinBid or JoinAsk
   (StopLimit, Unknown and unrecognized values are rejected).
2. A TrailingStop order needs a non-nil `TrailPrice`.
3. Bracket types must come from the same six values.
4. `RetrieveBarRequest.Limit` must not exceed 20,000 (`topstepx.MaxBars`).
5. `PartialCloseContractPositionRequest.Size` must be at least 1.

The request types expose the rules as `Validate() error`. The server remains the final
authority: `WithoutValidation()` disables client-side checks.

## Decimals and timestamps

Prices and balances are `github.com/shopspring/decimal` values (`*decimal.Decimal` when
optional). They are always **sent as JSON numbers** (`"limitPrice":6000.25`, never
`"6000.25"`) and decoded from numbers without losing printed digits: `6000.25` is exactly
`6000.25`. The SDK never touches the global `decimal.MarshalJSONWithoutQuotes`; the
models have their own JSON methods.

Timestamps are `time.Time` values in UTC. They are sent as RFC 3339 with a `Z` suffix and
decoded leniently: `Z` or any numeric offset, 0 to 9 fractional digits (.NET emits 7),
and a missing offset, which is read as UTC.

Absent optional fields decode as nil pointers. Absent arrays decode as nil slices, which
Go treats as empty (length 0, safe to range over).

## Spec unknowns and deviations

- **Swagger-only operations**: `Auth_LoginApp` (unsupported by the current guide; use
  `LoginKey`), `Auth_Logout`, `Order_SearchOrderById`, `Order_SearchOrdersQuery` and
  `Status_Ping` are listed only in the gateway's Swagger schema. They are implemented,
  but their runtime availability is unverified. The pagination defaults and bounds of
  `Order_SearchOrdersQuery` are not published and none are invented.
- **Realtime payloads** come from the official realtime guide, not from the REST models,
  and are decoded leniently as described above.
- **Undocumented bar fields**: `AggregateBar.D` (JSON `d`, a date in Swagger) and
  `AggregateBar.K` (JSON `k`, an integer) are exposed raw and optional; their semantics
  are not guessed.
- `Order.UpdateTimestamp` is required by the spec but optional here, so a null update
  time does not fail decoding. `HalfTrade.ProfitAndLoss` is nil for a half-turn trade.

### Deviations and Go-specific decisions

These are either Go idioms or decisions on points the contract leaves open:

- **Field names.** Go initialisms apply to identifiers (`AccountID`,
  `SearchContractByIDRequest`, `LoginErrorCodeAPIKeyAuthenticationDisabled`); JSON names
  and `String()` names match the spec exactly. `AggregateBar` uses descriptive field names
  (`Time`, `Open`, `High`, `Low`, `Close`, `Volume`) for the JSON members `t`, `o`, `h`,
  `l`, `c`, `v`.
- **Missing arrays** decode as nil slices rather than allocated empty slices.
- **`Auth.Logout`** sends the stored token as-is (no renewal or login first, no re-login on
  401). **`Auth.LoginKey`** stores the token but does not configure the credentials for
  automatic re-login.
- **Failed renewal.** When validate fails inside the refresh margin and the follow-up
  login also fails while the current token has not expired, the client logs a warning and
  keeps using the current token instead of failing the request.
- **HTTP 401 without credentials**: if another caller replaced the token meanwhile, the
  request is still retried once with the new token.
- **Supersets**: `ProblemDetails` is parsed for every non-2xx status, not only 400; decimal
  fields also accept numeric strings; the timestamp parser also accepts a `t` or space
  separator and `+hhmm` / `+hh` offsets; a realtime envelope whose `data` is an array and a
  quote payload that is an array each yield one event per element.
- **Missing payloads**: a successful `Place`, `Orders.SearchByID` or
  `Contracts.SearchByID` response without its `orderId` / `order` / `contract` is a
  `*DecodeError`.
- **`Do`** never changes the stored session (a login or validate through `Do` does not
  store the token; a logout does not clear it).
- **Realtime**: the first `Connect` is not retried automatically (reconnection applies
  after a connection has been established); `Unsubscribe*` while disconnected fails with
  `ErrNotConnected` and keeps the registry entry; payload identifiers (`id`, `accountId`,
  `contractId`, `symbolId`, `orderId`, `symbol`) are plain values while every other field
  is a pointer; a state event is always enqueued but, like any buffered event, can be
  evicted under `DropOldest` when the consumer falls far behind (the channel closing still
  signals the end).

## Examples

| Example | What it does |
| --- | --- |
| [`examples/accounts`](examples/accounts) | lists active accounts (read-only) |
| [`examples/contracts`](examples/contracts) | searches contracts and prints recent bars (read-only) |
| [`examples/realtime-user`](examples/realtime-user) | streams account, order, position and trade updates (read-only) |
| [`examples/realtime-market`](examples/realtime-market) | streams quotes, trades and optionally depth (read-only) |
| [`examples/place-order`](examples/place-order) | places a limit order and cancels it; **refuses to run unless `TOPSTEPX_ALLOW_TRADING=1`** and refuses non-simulated accounts without `-allow-live` |

```sh
TOPSTEPX_USERNAME=... TOPSTEPX_API_KEY=... go run ./examples/accounts
```

## Running the tests

```sh
cd go
go test -race ./...
go vet ./...
```

The tests need no network access and no credentials: HTTP behavior runs against
`net/http/httptest` servers and the realtime tests against a local WebSocket server
that speaks the SignalR protocol. They cover spec conformance (reading
`../spec/projectx-openapi.json`: all 21 operations, every request and response example,
a synthesized full-object round trip of all 45 object schemas and the exact (value,
name) pairs of all 24 enums), the five negative cases of `../spec/validation-report.json`,
the HTTP rules (lazy login, business failures, 401 re-login, 429 and `Retry-After`,
no retries of writes on 503, `ProblemDetails`, ping, refresh through validate, logout,
single-flight refresh, rate limiting, secret redaction) and the realtime rules
(handshake, invocations, event forms, unknown targets, keepalive, server timeout,
reconnect with a fresh token and replay, rejected tokens on connect and reconnect
against a local REST login, close, backpressure and goroutine cleanup).
