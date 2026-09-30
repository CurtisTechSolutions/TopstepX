//! SignalR realtime hubs: account, order, position and trade updates on the [`UserHub`];
//! quotes, trades and depth on the [`MarketHub`].
//!
//! The client speaks the ASP.NET Core SignalR JSON protocol directly over WebSockets. It
//! skips negotiation, as the official guide does, and authenticates with the same token as
//! the REST API. It sends keep-alive pings and reconnects with backoff, fetching a fresh token
//! for each attempt. After a reconnect it replays every active subscription before emitting
//! [`ConnectionEvent::Reconnected`].
//!
//! ```no_run
//! use topstepx::realtime::UserEvent;
//!
//! # async fn run(client: topstepx::Client, account_id: i32) -> topstepx::Result<()> {
//! let hub = client.connect_user_hub().await?;
//! let mut events = hub.events(); // subscribe to events before subscribing to data
//! hub.subscribe_orders(account_id).await?;
//! hub.subscribe_positions(account_id).await?;
//! while let Ok(event) = events.recv().await {
//!     match event {
//!         UserEvent::Order { order, .. } => println!("order {} is {:?}", order.id, order.status),
//!         UserEvent::Position { position, .. } => println!("position {:?}", position.size),
//!         UserEvent::Connection(state) => println!("connection: {state:?}"),
//!         _ => {}
//!     }
//! }
//! # Ok(())
//! # }
//! ```
//!
//! # Payloads
//!
//! Payload models follow the event tables of the official realtime guide, which the REST
//! specification does not cover. Every field that is not an identifier is optional, because
//! updates can be partial; quotes in particular often carry only what changed (see
//! [`GatewayQuote::merge`]). User-hub payloads are accepted bare, wrapped as
//! `{"action": n, "data": {...}}`, or as arrays of either. Anything that fails to decode is
//! delivered as an `Unknown` event instead of being dropped.

use std::fmt;
use std::future::Future;
use std::pin::Pin;
use std::sync::Arc;
use std::time::Duration;

use serde_json::Value;

use crate::client::Client;
use crate::error::Result;

mod connection;
mod market;
pub(crate) mod protocol;
mod user;

pub use market::{GatewayDepth, GatewayQuote, GatewayTrade, MarketEvent, MarketHub};
pub use user::{
    GatewayUserAccount, GatewayUserOrder, GatewayUserPosition, GatewayUserTrade, UserEvent, UserHub,
};

/// A boxed, sendable future.
pub type BoxFuture<'a, T> = Pin<Box<dyn Future<Output = T> + Send + 'a>>;

/// Current state of a hub connection.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum ConnectionState {
    /// Connected and processing messages.
    Connected,
    /// Connection lost; reconnect attempts are in progress. Invocations fail with
    /// [`RealtimeError::NotConnected`].
    Reconnecting,
    /// Closed for good, by [`UserHub::close`] / [`MarketHub::close`], by the server, or after
    /// reconnect attempts ran out.
    Closed,
}

/// Connection lifecycle notification, delivered in the same stream as data events.
///
/// There is no event for the initial connection: `connect` returning `Ok` means the hub is
/// connected. Receivers see every change after that.
#[derive(Debug, Clone, PartialEq, Eq)]
#[non_exhaustive]
pub enum ConnectionEvent {
    /// The connection was lost; reconnect attempt `attempt` is starting.
    Reconnecting {
        /// One-based attempt number.
        attempt: u32,
        /// Why the connection was lost, or why the previous attempt failed.
        cause: String,
    },
    /// Reconnected, and every active subscription was replayed.
    Reconnected,
    /// Replaying a subscription after a reconnect failed.
    ResubscribeFailed {
        /// Hub method that failed, such as `SubscribeOrders`.
        method: String,
        /// Error reported by the hub.
        error: String,
    },
    /// The connection is closed for good; the event stream ends after this.
    Closed {
        /// Why, when the close was not requested by the client.
        cause: Option<String>,
    },
}

/// A server message this SDK does not recognize or could not decode.
#[derive(Debug, Clone, PartialEq)]
#[non_exhaustive]
pub struct UnknownEvent {
    /// Hub method the server invoked, such as `GatewayUserOrder`.
    pub target: String,
    /// Raw arguments.
    pub arguments: Vec<Value>,
    /// Decode error, or `None` when the target itself is unknown.
    pub error: Option<String>,
}

/// Realtime failures.
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
#[non_exhaustive]
pub enum RealtimeError {
    /// The WebSocket connection could not be established.
    #[error("cannot connect to {url}: {message}")]
    Connect {
        /// Hub URL (without the access token).
        url: String,
        /// Underlying error.
        message: String,
    },
    /// The SignalR handshake failed.
    #[error("SignalR handshake failed: {0}")]
    Handshake(String),
    /// The hub method returned an error.
    #[error("hub method `{target}` failed: {message}")]
    Invocation {
        /// Hub method.
        target: String,
        /// Error reported by the hub.
        message: String,
    },
    /// The connection is reconnecting; retry after [`ConnectionEvent::Reconnected`].
    #[error("hub is not connected")]
    NotConnected,
    /// The connection dropped before the invocation completed.
    #[error("connection lost before `{target}` completed")]
    ConnectionLost {
        /// Hub method.
        target: String,
    },
    /// The hub connection is closed.
    #[error("hub connection is closed")]
    Closed,
    /// A timeout elapsed.
    #[error("timed out waiting for {what}")]
    Timeout {
        /// What was awaited.
        what: String,
    },
    /// The token provider failed.
    #[error("cannot obtain an access token: {0}")]
    Token(String),
    /// The hub URL is unusable.
    #[error("invalid hub URL: {0}")]
    Url(String),
}

/// Reconnection schedule. The default retries forever: immediately, then after 1, 2, 5 and 10
/// seconds, then every 30 seconds.
#[derive(Debug, Clone, PartialEq, Eq)]
#[non_exhaustive]
pub struct ReconnectPolicy {
    /// Delays before the first attempts, in order.
    pub delays: Vec<Duration>,
    /// Delay before every attempt after `delays` is exhausted.
    pub then_every: Duration,
    /// Maximum attempts per outage; `None` retries forever, `Some(0)` disables reconnection.
    pub max_attempts: Option<u32>,
}

impl Default for ReconnectPolicy {
    fn default() -> Self {
        ReconnectPolicy {
            delays: [0, 1, 2, 5, 10]
                .into_iter()
                .map(Duration::from_secs)
                .collect(),
            then_every: Duration::from_secs(30),
            max_attempts: None,
        }
    }
}

impl ReconnectPolicy {
    /// Never reconnect.
    pub fn disabled() -> Self {
        ReconnectPolicy {
            max_attempts: Some(0),
            ..ReconnectPolicy::default()
        }
    }

    /// Custom schedule.
    pub fn new(delays: Vec<Duration>, then_every: Duration, max_attempts: Option<u32>) -> Self {
        ReconnectPolicy {
            delays,
            then_every,
            max_attempts,
        }
    }

    /// Delay before one-based attempt `attempt`.
    pub fn delay(&self, attempt: u32) -> Duration {
        let index = attempt.saturating_sub(1) as usize;
        self.delays.get(index).copied().unwrap_or(self.then_every)
    }
}

/// Hub connection settings.
#[derive(Debug, Clone)]
#[non_exhaustive]
pub struct HubOptions {
    /// Reconnection schedule.
    pub reconnect: ReconnectPolicy,
    /// A ping is sent when nothing was sent for this long (default 15 s).
    pub keep_alive_interval: Duration,
    /// The connection counts as lost when nothing arrives for this long (default 30 s).
    pub server_timeout: Duration,
    /// Limit for the WebSocket connect plus SignalR handshake (default 15 s).
    pub handshake_timeout: Duration,
    /// Limit for an invocation's completion (default 30 s).
    pub invoke_timeout: Duration,
    /// Event buffer per receiver (default 1024). Receivers that fall further behind get
    /// `RecvError::Lagged` with the number of skipped events.
    pub event_capacity: usize,
}

impl Default for HubOptions {
    fn default() -> Self {
        HubOptions {
            reconnect: ReconnectPolicy::default(),
            keep_alive_interval: Duration::from_secs(15),
            server_timeout: Duration::from_secs(30),
            handshake_timeout: Duration::from_secs(15),
            invoke_timeout: Duration::from_secs(30),
            event_capacity: 1024,
        }
    }
}

impl HubOptions {
    /// Sets the reconnection schedule.
    #[must_use]
    pub fn reconnect(mut self, policy: ReconnectPolicy) -> Self {
        self.reconnect = policy;
        self
    }

    /// Sets the keep-alive interval.
    #[must_use]
    pub fn keep_alive_interval(mut self, interval: Duration) -> Self {
        self.keep_alive_interval = interval;
        self
    }

    /// Sets the server timeout.
    #[must_use]
    pub fn server_timeout(mut self, timeout: Duration) -> Self {
        self.server_timeout = timeout;
        self
    }

    /// Sets the handshake timeout.
    #[must_use]
    pub fn handshake_timeout(mut self, timeout: Duration) -> Self {
        self.handshake_timeout = timeout;
        self
    }

    /// Sets the invocation timeout.
    #[must_use]
    pub fn invoke_timeout(mut self, timeout: Duration) -> Self {
        self.invoke_timeout = timeout;
        self
    }

    /// Sets the per-receiver event buffer.
    #[must_use]
    pub fn event_capacity(mut self, capacity: usize) -> Self {
        self.event_capacity = capacity;
        self
    }
}

type TokenFn = dyn Fn() -> BoxFuture<'static, Result<String>> + Send + Sync;

/// Supplies an access token for each connection attempt.
#[derive(Clone)]
pub struct TokenProvider(Arc<TokenFn>);

impl fmt::Debug for TokenProvider {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("TokenProvider(..)")
    }
}

impl TokenProvider {
    /// Wraps an async function returning a token.
    pub fn new<F, Fut>(provider: F) -> Self
    where
        F: Fn() -> Fut + Send + Sync + 'static,
        Fut: Future<Output = Result<String>> + Send + 'static,
    {
        TokenProvider(Arc::new(move || Box::pin(provider())))
    }

    /// Always returns `token`. Reconnects fail once it expires.
    pub fn fixed(token: impl Into<String>) -> Self {
        let token: Arc<str> = Arc::from(token.into());
        TokenProvider::new(move || {
            let token = token.clone();
            async move { Ok(token.to_string()) }
        })
    }

    /// Uses [`Client::token`], which logs in or renews the session as needed.
    pub fn from_client(client: &Client) -> Self {
        let client = client.clone();
        TokenProvider::new(move || {
            let client = client.clone();
            async move { client.token().await }
        })
    }

    pub(crate) fn token(&self) -> BoxFuture<'static, Result<String>> {
        (self.0)()
    }
}

/// Converts raw hub traffic into a typed event stream. Implemented by [`UserEvent`] and
/// [`MarketEvent`].
pub trait HubEvent: Clone + Send + 'static {
    /// Decodes a server invocation into zero or more events.
    fn from_invocation(target: String, arguments: Vec<Value>) -> Vec<Self>;
    /// Wraps a connection lifecycle notification.
    fn from_connection(event: ConnectionEvent) -> Self;
}

/// Splits `{"action": n, "data": {...}}` into its parts; other values pass through.
fn unwrap_envelope(item: Value) -> (Option<i32>, Value) {
    match item {
        Value::Object(mut map) if map.get("data").is_some_and(Value::is_object) => {
            let action = map
                .get("action")
                .and_then(Value::as_i64)
                .and_then(|a| i32::try_from(a).ok());
            let data = map.remove("data").unwrap_or(Value::Null);
            (action, data)
        }
        other => (None, other),
    }
}

impl Client {
    /// Connects to the user hub with default [`HubOptions`].
    pub async fn connect_user_hub(&self) -> Result<UserHub> {
        self.connect_user_hub_with(HubOptions::default()).await
    }

    /// Connects to the user hub.
    pub async fn connect_user_hub_with(&self, options: HubOptions) -> Result<UserHub> {
        UserHub::connect(
            self.inner.user_hub_url.clone(),
            TokenProvider::from_client(self),
            options,
        )
        .await
    }

    /// Connects to the market hub with default [`HubOptions`].
    pub async fn connect_market_hub(&self) -> Result<MarketHub> {
        self.connect_market_hub_with(HubOptions::default()).await
    }

    /// Connects to the market hub.
    pub async fn connect_market_hub_with(&self, options: HubOptions) -> Result<MarketHub> {
        MarketHub::connect(
            self.inner.market_hub_url.clone(),
            TokenProvider::from_client(self),
            options,
        )
        .await
    }

    /// User hub URL.
    pub fn user_hub_url(&self) -> &url::Url {
        &self.inner.user_hub_url
    }

    /// Market hub URL.
    pub fn market_hub_url(&self) -> &url::Url {
        &self.inner.market_hub_url
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn envelope_unwrapping() {
        assert_eq!(
            unwrap_envelope(json!({"action": 1, "data": {"id": 5}})),
            (Some(1), json!({"id": 5}))
        );
        assert_eq!(
            unwrap_envelope(json!({"data": {"id": 5}})),
            (None, json!({"id": 5}))
        );
        assert_eq!(unwrap_envelope(json!({"id": 5})), (None, json!({"id": 5})));
        assert_eq!(
            unwrap_envelope(json!({"data": 3})),
            (None, json!({"data": 3}))
        );
    }

    #[test]
    fn reconnect_schedule() {
        let policy = ReconnectPolicy::default();
        assert_eq!(policy.delay(1), Duration::ZERO);
        assert_eq!(policy.delay(5), Duration::from_secs(10));
        assert_eq!(policy.delay(6), Duration::from_secs(30));
        assert_eq!(policy.delay(600), Duration::from_secs(30));
        assert_eq!(ReconnectPolicy::disabled().max_attempts, Some(0));
    }
}
