# topstepx: Rust SDK for the ProjectX Gateway API

Async Rust client for the [ProjectX Gateway API](https://gateway.docs.projectx.com/docs/intro/)
as deployed by TopstepX:

* all **21 REST operations** of the derived OpenAPI reference in [`../spec`](../spec);
* the **SignalR user and market hubs**, with automatic reconnection and subscription replay.

The Python (`../python`) and Go (`../go`) SDKs in this repository implement the same behavior.
That behavior is specified in [`../spec/SDK_BEHAVIOR.md`](../spec/SDK_BEHAVIOR.md).

> **Safety:** order placement, modification, cancellation and position closing act on real
> accounts. Develop against a simulated account. No test or read-only example in this crate
> sends orders, and the one example that does refuses to run unless you set
> `TOPSTEPX_ALLOW_TRADING=1`.

## Installation

The crate is not on crates.io. Depend on it from git, where cargo finds the package in the
`rust/` directory:

```toml
[dependencies]
topstepx = { git = "https://github.com/CurtisTechSolutions/TopstepX" }
tokio = { version = "1", features = ["macros", "rt-multi-thread"] }
```

| Cargo feature | Default | Purpose |
| --- | --- | --- |
| `rustls` | yes | TLS through rustls: reqwest's default provider and platform verifier, native roots for WebSockets |
| `native-tls` | no | TLS through the platform library (use with `default-features = false`) |
| `realtime` | yes | SignalR hubs (`topstepx::realtime`) |

One TLS feature is required to reach the real `https://` endpoints. The minimum supported Rust
version is 1.88; the crate is developed with 1.94.

## Quick start

```rust,no_run
use topstepx::prelude::*;

#[tokio::main]
async fn main() -> topstepx::Result<()> {
    // Or Client::from_env(), which reads TOPSTEPX_USERNAME and TOPSTEPX_API_KEY.
    let client = Client::new("my-platform-username", "my-api-key")?;

    let account = client.accounts().search(true).await?.remove(0);
    let es = client.contracts().search("ES", false).await?.remove(0);
    println!("{} trades {} (tick {})", account.name, es.name, es.tick_size);

    let order_id = client
        .orders()
        .place(&PlaceOrderRequest::limit(account.id, &es.id, OrderSide::Bid, 1, es.round_to_tick(Decimal::from(5000))))
        .await?;
    client.orders().cancel(account.id, order_id).await?;
    Ok(())
}
```

## Authentication and token lifecycle

* **Credentials** are the platform username and an API key, set with
  `ClientBuilder::credentials`, `Client::new` or the `TOPSTEPX_USERNAME` and
  `TOPSTEPX_API_KEY` environment variables. The first authenticated call logs in
  (`POST /api/Auth/loginKey`). Calling `client.login()` up front is optional.
* **Token lifetime** comes from the JWT `exp` claim. The signature is not verified. When the
  claim is unreadable, the token is assumed to expire 24 hours after it was obtained.
* **Renewal:** within `refresh_margin` of expiry (default 1 hour), the client calls
  `POST /api/Auth/validate` and stores `newToken`. If renewal fails, it logs in again when
  credentials are configured. Otherwise it keeps the still-valid token and waits 60 s
  before trying `validate` again, so a failing renewal cannot double every request. An expired token
  is replaced by a fresh login, never renewed, as the current guide recommends. Concurrent
  callers share a single login or renewal.
* **HTTP 401:** with credentials, the client logs in again and retries the request once.
* **Explicit calls:** `client.auth().login_key(user, key)`, `.validate()`, `.logout()`, and
  `.login_app(..)`. The current authentication guide does not support `login_app`, so prefer
  API keys. `logout()` always forgets the stored token and is a no-op when none is stored.
* **Your own connections:** `client.token()` returns a valid token. If another service
  rejects that token, `client.invalidate_token(&token)` discards it so the next call logs in
  again. You can also start from an existing token with `ClientBuilder::token`.
* Debug output redacts the API key and every token.

## REST operations

| Group | Method | Operation |
| --- | --- | --- |
| `client.accounts()` | `search(only_active)` | `POST /api/Account/search` |
| `client.auth()` | `login_key`, `login_app`, `validate`, `logout` | `POST /api/Auth/*` |
| `client.contracts()` | `search(text, live)`, `search_by_id(id)`, `available(live)` | `POST /api/Contract/*` |
| `client.history()` | `retrieve_bars(&RetrieveBarRequest)` | `POST /api/History/retrieveBars` |
| `client.orders()` | `search`, `search_range`, `search_by_id`, `search_open`, `query`, `place`, `cancel`, `modify` | `POST /api/Order/*` |
| `client.positions()` | `search_open`, `close`, `partial_close` | `POST /api/Position/*` |
| `client.trades()` | `search`, `search_range` | `POST /api/Trade/search` |
| `client.status()` | `ping()` | `GET /api/Status/ping` |

Request types have constructors and builder methods, for example
`PlaceOrderRequest::{market, limit, stop, trailing_stop, join_bid, join_ask}` with
`.with_custom_tag(..)`, `.with_stop_loss_bracket(ticks, type)` and
`.with_take_profit_bracket(..)`, or `RetrieveBarRequest::new(..).live(true)`.

Every request type also implements `Endpoint`, so you can send it directly:

```rust,no_run
# async fn run(client: topstepx::Client) -> topstepx::Result<()> {
use topstepx::models::*;
use topstepx::enums::*;

let request = PlaceOrderRequest::market(123, "CON.F.US.EP.U25", OrderSide::Bid, 1);
let checked = client.execute(&request).await?;   // a business failure becomes Error::Api
let raw = client.execute_raw(&request).await?;   // the envelope as returned, success or not
println!("success={} code={} order={:?}", raw.success, raw.error_code, raw.order_id);
# let _ = checked; Ok(()) }
```

`Operation::ALL` lists the 21 operations with their HTTP method, path, authentication
requirement, rate-limit bucket and retry class.

## Realtime

```rust,no_run
use topstepx::realtime::{GatewayQuote, MarketEvent, UserEvent};

# async fn run(client: topstepx::Client, account_id: i32, contract_id: &str) -> topstepx::Result<()> {
let user = client.connect_user_hub().await?;
let mut updates = user.events();          // call before subscribing so nothing is missed
user.subscribe_orders(account_id).await?;
user.subscribe_positions(account_id).await?;

let market = client.connect_market_hub().await?;
let mut ticks = market.events();
market.subscribe_contract_quotes(contract_id).await?;

let mut quote = GatewayQuote::default();
loop {
    tokio::select! {
        event = updates.recv() => match event {
            Ok(UserEvent::Order { order, .. }) => println!("order {} -> {:?}", order.id, order.status),
            Ok(_) => {}
            Err(err) => { eprintln!("user stream: {err}"); break; }   // Lagged(n) or Closed
        },
        event = ticks.recv() => match event {
            Ok(MarketEvent::Quote { quote: update, .. }) => {
                quote.merge(&update);   // quote updates can be partial
                println!("{:?} / {:?}", quote.best_bid, quote.best_ask);
            }
            Ok(_) => {}
            Err(err) => { eprintln!("market stream: {err}"); break; }
        },
    }
}
# Ok(())
# }
```

* **Hub methods:**
  * `UserHub`: `subscribe_accounts`, `subscribe_orders`, `subscribe_positions`,
    `subscribe_trades`.
  * `MarketHub`: `subscribe_contract_quotes`, `subscribe_contract_trades`,
    `subscribe_contract_market_depth`.
  * Each has an `unsubscribe_*` counterpart. `invoke` calls any other hub method.
* **Events:** `hub.events()` returns a `tokio::sync::broadcast::Receiver`. A receiver that
  falls more than `HubOptions::event_capacity` events behind gets `RecvError::Lagged(n)`.
  The connection itself never blocks on slow consumers.
* **Connection lifecycle:**
  * `connect_*` returning `Ok` means the hub is connected.
  * Later changes arrive as `UserEvent::Connection(..)` or `MarketEvent::Connection(..)`:
    `Reconnecting { attempt, cause }`, `Reconnected`, `ResubscribeFailed` and
    `Closed { cause }`.
  * `hub.state()` and `hub.wait_for_state(..)` expose the current state.
* **Protocol:** the SignalR JSON protocol is implemented directly over WebSockets, skipping
  negotiation as the official guide does. The token is sent both as `access_token` and in
  the `Authorization` header.
* **Keep-alive:** a ping goes out every 15 s, and a server silent for 30 s counts as a lost
  connection.
* **Reconnection:** the client reconnects immediately, then after 1, 2, 5 and 10 s, then
  every 30 s, indefinitely. Each attempt gets a fresh token. Active subscriptions are
  replayed before `Reconnected` is emitted. Everything is configurable through
  `HubOptions` and `ReconnectPolicy`.
* **Rejected tokens:** if the hub refuses the WebSocket upgrade with HTTP 401, the token is
  reported to the `TokenProvider`. The client-backed provider discards it, so the next
  attempt logs in again instead of reusing it until expiry. Custom providers can react
  through `TokenProvider::on_rejected`.
* **Closing:** `hub.close()` closes cleanly, as does dropping every clone of the hub.
* **Payloads:** the models follow the official realtime guide's event tables, which the REST
  specification does not cover. Every non-identifier field is `Option`. User-hub payloads
  are accepted bare, wrapped as `{"action": n, "data": {...}}`, or as arrays. An unknown
  target or an undecodable payload arrives as an `Unknown` event with the raw arguments; it
  is never silently dropped.

## Errors

`topstepx::Error` distinguishes:

| Variant | Meaning |
| --- | --- |
| `Api(ApiError)` | Business failure: HTTP 200 with `success: false` or a non-zero `errorCode`. `code` is a typed `ErrorCode` such as `ErrorCode::PlaceOrder(PlaceOrderErrorCode::InsufficientFunds)`, and `message` may be `None`. For placements, `order_id` keeps any id the gateway returned; that id does **not** mean the order was accepted. |
| `Unauthorized`, `RateLimited` | HTTP 401 after re-login, or HTTP 429 after retries (with the last `Retry-After`) |
| `Status(StatusError)` | Other HTTP statuses, with RFC 7807 `problem` details when present (for example HTTP 400 from `/api/Order/v2/query`) |
| `Transport`, `Decode`, `MissingData` | Network failure; a body that is not the expected JSON; a successful response missing required data |
| `InvalidRequest` | Rejected by client-side validation before any I/O |
| `NotAuthenticated`, `Config` | No token and no credentials; invalid builder settings |
| `Realtime(RealtimeError)` | Hub failures: `Connect`, `UpgradeRejected { status }` (HTTP refusal of the WebSocket upgrade), `Handshake`, `Invocation`, `NotConnected`, `ConnectionLost`, `Closed`, `Timeout` |

## Retries and rate limits

* **HTTP 429** is retried for every operation, because the request was not processed. The
  client waits `Retry-After` when present, and otherwise backs off exponentially: 1 s, 2 s,
  4 s, … capped at 30 s.
* **HTTP 502, 503, 504 and transport errors** are retried only for read-only operations.
  Placement, cancellation, modification, closing and the auth calls are never retried after
  they might have executed.
* The retry budget defaults to 3 retries (`ClientBuilder::retry_policy` or `max_retries`).
* **Client-side sliding-window limiter:** 50 requests per 30 s for bar retrieval and 200 per
  60 s for everything else. These are the documented gateway limits. The limiter is per
  `Client` instance (clones share it) and is not coordinated across processes. Change it
  with `rate_limits(..)` or turn it off with `disable_rate_limiting()`.

## Client-side validation

These checks come from the specification. They run before any network I/O and can be
disabled with `validate_requests(false)`:

1. The placement type must be Limit, Market, Stop, TrailingStop, JoinBid or JoinAsk.
   StopLimit is rejected.
2. A trailing stop requires `trail_price`, an **absolute** price aligned to the tick size.
   Use `Contract::round_to_tick`.
3. Bracket types must come from the same six.
4. A bar request may ask for at most 20,000 bars.
5. A partial close size must be at least 1.

## Types

* **Prices and amounts** are `rust_decimal::Decimal`, re-exported as `topstepx::Decimal`.
  They are decoded without float artifacts (`6000.25` stays `6000.25`) and sent as JSON
  numbers.
* **Timestamps** are `chrono::DateTime<Utc>`. Decoding accepts `Z`, numeric offsets, up to 9
  fractional digits, and a missing offset, which is read as UTC. Encoding produces RFC 3339
  with `Z`.
* **Enums** carry the specification's values and names. A value this SDK does not know
  decodes to `Unrecognized(n)` instead of failing.
* **Type names** follow the specification; object models drop the `Model` suffix, so
  `OrderModel` is `Order` and `HalfTradeModel` is `HalfTrade`.

## Specification notes

* `loginApp`, `logout`, `Order/searchById`, `Order/v2/query` and `Status/ping` appear only in
  Swagger, and their runtime availability is unverified. `ping` sends a token when one is
  held or obtainable. When there is none, or the login fails, it goes anonymous.
* `Order.update_timestamp` is optional here, although the specification marks it required,
  so a `null` cannot break decoding.
* Bar fields `d` and `k` are undocumented and exposed raw.
* `Order.trail_price` in responses is a **distance** in price units. In requests,
  `trail_price` is an **absolute** price.
* `live` selects the market-data subscription. It says nothing about whether the account
  is simulated.
* Realtime payload models come from the realtime guide. `realtime.md` only indexes hubs and
  events, so decoding is deliberately lenient.

## Development

```sh
cargo test --all-features          # unit, spec-conformance, HTTP and realtime tests; no network
cargo clippy --all-targets --all-features -- -D warnings
cargo fmt --check
RUSTDOCFLAGS="-D warnings" cargo doc --no-deps --all-features
```

* `tests/spec_conformance.rs` checks the SDK against `../spec/projectx-openapi.json`:
  * all 21 operations;
  * every request and response example;
  * a synthesized full-object round trip for all 45 object schemas;
  * the values and names of all 24 enums.
* `tests/http.rs` runs the client against a local mock server.
* `tests/realtime.rs` runs the hubs against a local SignalR server.

The examples in `examples/` read `TOPSTEPX_USERNAME` and `TOPSTEPX_API_KEY`, plus the
optional `TOPSTEPX_API_URL`, `TOPSTEPX_USER_HUB_URL` and `TOPSTEPX_MARKET_HUB_URL`:

```sh
cargo run --example accounts
cargo run --example market_data -- NQ
cargo run --example realtime_user
cargo run --example realtime_market -- ES
TOPSTEPX_ALLOW_TRADING=1 cargo run --example place_order -- <ACCOUNT_ID> ES   # real order!
```

## License

GPL-3.0-only, see [`../LICENSE`](../LICENSE).
