//! The REST client.

use std::fmt;
use std::sync::Arc;
use std::time::Duration;

use chrono::{DateTime, Utc};
use reqwest::header::{ACCEPT, CONTENT_TYPE};
use reqwest::{Method, StatusCode};
use serde::de::DeserializeOwned;
use url::Url;

use crate::api;
use crate::error::{Error, Result, StatusError};
use crate::models::{LoginApiKeyRequest, LoginResponse, ProblemDetails, ValidateResponse};
use crate::operation::{
    AuthRequirement, Endpoint, GatewayResponse, HttpMethod, Operation, RetryClass,
};
use crate::rate_limit::{RateLimiter, RateLimits};
use crate::retry::{self, RetryPolicy};
use crate::session::{Credentials, Freshness, Session, TokenState};

/// Default REST base URL (TopstepX). Other firms run the gateway on other hosts.
pub const DEFAULT_API_URL: &str = "https://api.topstepx.com";
/// Default SignalR user hub URL (TopstepX).
pub const DEFAULT_USER_HUB_URL: &str = "https://rtc.topstepx.com/hubs/user";
/// Default SignalR market hub URL (TopstepX).
pub const DEFAULT_MARKET_HUB_URL: &str = "https://rtc.topstepx.com/hubs/market";

const DEFAULT_TIMEOUT: Duration = Duration::from_secs(30);
const DEFAULT_CONNECT_TIMEOUT: Duration = Duration::from_secs(10);
const DEFAULT_REFRESH_MARGIN: Duration = Duration::from_secs(60 * 60);

/// Async client for the gateway's REST API.
///
/// Cheap to clone; clones share the HTTP connection pool, the session token and the rate
/// limiter. See the [crate documentation](crate) for an overview.
#[derive(Clone)]
pub struct Client {
    pub(crate) inner: Arc<Inner>,
}

pub(crate) struct Inner {
    http: reqwest::Client,
    base_url: Url,
    #[cfg_attr(not(feature = "realtime"), allow(dead_code))]
    pub(crate) user_hub_url: Url,
    #[cfg_attr(not(feature = "realtime"), allow(dead_code))]
    pub(crate) market_hub_url: Url,
    credentials: Option<Credentials>,
    pub(crate) session: Session,
    limiter: Option<RateLimiter>,
    retry: RetryPolicy,
    validate_requests: bool,
    refresh_margin: Duration,
}

impl fmt::Debug for Client {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("Client")
            .field("base_url", &self.inner.base_url.as_str())
            .field(
                "username",
                &self.inner.credentials.as_ref().map(|c| c.username.as_str()),
            )
            .field("has_token", &self.inner.session.current().is_some())
            .field("rate_limited", &self.inner.limiter.is_some())
            .field("retry", &self.inner.retry)
            .finish_non_exhaustive()
    }
}

/// How a request authenticates.
#[derive(Clone, Copy)]
pub(crate) enum AuthMode<'a> {
    /// No `Authorization` header.
    None,
    /// This exact token, without 401 recovery (used by validate and logout).
    Explicit(&'a str),
    /// The session token, logging in or refreshing as needed.
    Session,
    /// The session token if one is held or obtainable, anonymous otherwise.
    Optional,
}

/// Result of an HTTP exchange that did not fail outright.
enum Outcome {
    Success(String),
    Unauthorized(String),
}

/// Result of a single HTTP attempt.
enum Attempt {
    Finished(Result<Outcome>),
    /// A failure that may be retried after `delay`; `error` is returned when the retry budget
    /// is exhausted.
    Retryable {
        error: Error,
        delay: Duration,
    },
}

impl Client {
    /// Starts configuring a client.
    pub fn builder() -> ClientBuilder {
        ClientBuilder::new()
    }

    /// Creates a client for the default (TopstepX) endpoints with API-key credentials.
    pub fn new(username: impl Into<String>, api_key: impl Into<String>) -> Result<Client> {
        ClientBuilder::new().credentials(username, api_key).build()
    }

    /// Creates a client from environment variables; see [`ClientBuilder::from_env`].
    pub fn from_env() -> Result<Client> {
        ClientBuilder::from_env().build()
    }

    /// Authentication operations (login, validate, logout).
    pub fn auth(&self) -> api::Auth<'_> {
        api::Auth::new(self)
    }

    /// Account operations.
    pub fn accounts(&self) -> api::Accounts<'_> {
        api::Accounts::new(self)
    }

    /// Contract discovery.
    pub fn contracts(&self) -> api::Contracts<'_> {
        api::Contracts::new(self)
    }

    /// Historical bars.
    pub fn history(&self) -> api::History<'_> {
        api::History::new(self)
    }

    /// Order operations.
    pub fn orders(&self) -> api::Orders<'_> {
        api::Orders::new(self)
    }

    /// Position operations.
    pub fn positions(&self) -> api::Positions<'_> {
        api::Positions::new(self)
    }

    /// Trade history.
    pub fn trades(&self) -> api::Trades<'_> {
        api::Trades::new(self)
    }

    /// Service status.
    pub fn status(&self) -> api::Status<'_> {
        api::Status::new(self)
    }

    /// REST base URL.
    pub fn base_url(&self) -> &Url {
        &self.inner.base_url
    }

    /// Logs in with the configured credentials, replacing any stored token.
    ///
    /// Calling this is optional: authenticated operations log in on first use.
    pub async fn login(&self) -> Result<()> {
        let credentials = self
            .inner
            .credentials
            .as_ref()
            .ok_or(Error::NotAuthenticated)?;
        let _guard = self.inner.session.refresh.lock().await;
        self.login_locked(credentials).await?;
        Ok(())
    }

    /// Returns a valid session token, logging in or renewing it first when needed.
    ///
    /// Use this to authenticate your own connections (the realtime hubs do).
    pub async fn token(&self) -> Result<String> {
        Ok(self.ensure_token().await?.token.to_string())
    }

    /// Replaces the stored token, for example with one obtained elsewhere.
    pub fn set_token(&self, token: impl Into<String>) {
        self.inner.session.store(token.into());
    }

    /// Forgets the stored token without calling the gateway.
    pub fn clear_token(&self) {
        self.inner.session.clear();
    }

    /// Whether a token is stored (it may be expired).
    pub fn has_token(&self) -> bool {
        self.inner.session.current().is_some()
    }

    /// Expiry of the stored token: its JWT `exp` claim, or 24 hours after it was stored when
    /// the claim is unreadable.
    pub fn token_expires_at(&self) -> Option<DateTime<Utc>> {
        self.inner.session.current().map(|state| state.expires_at)
    }

    /// Sends a request and returns its response envelope, turning a business failure into
    /// [`Error::Api`].
    ///
    /// The high-level methods (`client.orders().place(...)`, ...) are built on this. It never
    /// changes the stored session: executing a login request does not store its token.
    pub async fn execute<E: Endpoint>(&self, request: &E) -> Result<E::Response> {
        let response = self.execute_raw(request).await?;
        response.check(E::OPERATION)?;
        Ok(response)
    }

    /// Sends a request and returns its response envelope **without** checking `success`
    /// or `errorCode`. HTTP, transport, decode and validation errors are still returned.
    ///
    /// Use it to read fields the high-level methods drop, such as the `orderId` of a
    /// rejected order.
    pub async fn execute_raw<E: Endpoint>(&self, request: &E) -> Result<E::Response> {
        let auth = match E::OPERATION.auth() {
            AuthRequirement::None => AuthMode::None,
            AuthRequirement::Bearer => AuthMode::Session,
            AuthRequirement::Optional => AuthMode::Optional,
        };
        self.execute_with(request, auth).await
    }

    pub(crate) async fn execute_with<E: Endpoint>(
        &self,
        request: &E,
        auth: AuthMode<'_>,
    ) -> Result<E::Response> {
        let body = self.prepare(request)?;
        self.request_json(E::OPERATION, body.as_deref(), auth).await
    }

    /// Validates (when enabled) and serializes a request body.
    fn prepare<E: Endpoint>(&self, request: &E) -> Result<Option<Vec<u8>>> {
        if self.inner.validate_requests {
            request.validate()?;
        }
        if !E::HAS_BODY {
            return Ok(None);
        }
        serde_json::to_vec(request)
            .map(Some)
            .map_err(|err| Error::InvalidRequest {
                operation: E::OPERATION,
                message: format!("request could not be serialized: {err}"),
            })
    }

    pub(crate) async fn ping(&self) -> Result<String> {
        let text = self
            .request_text(Operation::StatusPing, None, AuthMode::Optional)
            .await?;
        let trimmed = text.trim();
        Ok(serde_json::from_str::<String>(trimmed).unwrap_or_else(|_| trimmed.to_owned()))
    }

    // ----- session management -------------------------------------------------------------

    /// Returns a usable token following `spec/SDK_BEHAVIOR.md` §5.
    pub(crate) async fn ensure_token(&self) -> Result<TokenState> {
        let margin = self.inner.refresh_margin;
        if let Some(state) = self.inner.session.current() {
            if state.freshness(Utc::now(), margin) == Freshness::Fresh {
                return Ok(state);
            }
        }

        let _guard = self.inner.session.refresh.lock().await;
        let Some(state) = self.inner.session.current() else {
            return match &self.inner.credentials {
                Some(credentials) => self.login_locked(credentials).await,
                None => Err(Error::NotAuthenticated),
            };
        };
        match state.freshness(Utc::now(), margin) {
            Freshness::Fresh => Ok(state),
            Freshness::Expired => match &self.inner.credentials {
                Some(credentials) => self.login_locked(credentials).await,
                // Without credentials, let the server judge the token (clock skew).
                None => Ok(state),
            },
            Freshness::NeedsRefresh => match self.validate_locked(&state.token).await {
                Ok(renewed) => Ok(renewed),
                Err(error) => match &self.inner.credentials {
                    Some(credentials) => {
                        tracing::debug!(%error, "token renewal failed; logging in again");
                        self.login_locked(credentials).await
                    }
                    None => {
                        tracing::warn!(%error, "token renewal failed; using the current token until it expires");
                        Ok(state)
                    }
                },
            },
        }
    }

    async fn optional_token(&self) -> Result<Option<TokenState>> {
        if self.inner.session.current().is_none() && self.inner.credentials.is_none() {
            return Ok(None);
        }
        self.ensure_token().await.map(Some)
    }

    /// Logs in and stores the token. The caller holds the refresh lock.
    async fn login_locked(&self, credentials: &Credentials) -> Result<TokenState> {
        tracing::debug!(username = %credentials.username, "logging in with API key");
        let request =
            LoginApiKeyRequest::new(credentials.username.clone(), credentials.api_key.clone());
        let token = self.login_request(&request).await?;
        Ok(self.inner.session.store(token))
    }

    /// Sends a login request and returns the token without storing it.
    pub(crate) async fn login_request<E>(&self, request: &E) -> Result<String>
    where
        E: Endpoint<Response = LoginResponse>,
    {
        let body = self.prepare(request)?;
        let response: LoginResponse = self
            .request_json_plain(E::OPERATION, body.as_deref(), None)
            .await?;
        response.check(E::OPERATION)?;
        response
            .token
            .filter(|token| !token.is_empty())
            .ok_or(Error::MissingData {
                operation: E::OPERATION,
                field: "token",
            })
    }

    /// Renews `token` and stores the new one. The caller holds the refresh lock.
    pub(crate) async fn validate_locked(&self, token: &str) -> Result<TokenState> {
        let response: ValidateResponse = self
            .request_json_plain(Operation::AuthValidate, None, Some(token))
            .await?;
        response.check(Operation::AuthValidate)?;
        let new_token =
            response
                .new_token
                .filter(|token| !token.is_empty())
                .ok_or(Error::MissingData {
                    operation: Operation::AuthValidate,
                    field: "newToken",
                })?;
        Ok(self.inner.session.store(new_token))
    }

    /// After a 401: logs in again unless another caller already replaced the failed token.
    async fn recover_from_unauthorized(&self, failed_generation: u64) -> Result<TokenState> {
        let _guard = self.inner.session.refresh.lock().await;
        if let Some(current) = self.inner.session.current() {
            if current.generation != failed_generation {
                return Ok(current);
            }
        }
        let credentials = self
            .inner
            .credentials
            .as_ref()
            .ok_or(Error::NotAuthenticated)?;
        self.login_locked(credentials).await
    }

    // ----- HTTP plumbing ------------------------------------------------------------------

    pub(crate) async fn request_json<T: DeserializeOwned>(
        &self,
        operation: Operation,
        body: Option<&[u8]>,
        auth: AuthMode<'_>,
    ) -> Result<T> {
        let text = self.request_text(operation, body, auth).await?;
        serde_json::from_str(&text).map_err(|source| Error::Decode {
            operation,
            source,
            body: text,
        })
    }

    /// Like [`request_json`](Self::request_json) but never touches the session; used by the
    /// login and renewal paths themselves.
    async fn request_json_plain<T: DeserializeOwned>(
        &self,
        operation: Operation,
        body: Option<&[u8]>,
        bearer: Option<&str>,
    ) -> Result<T> {
        let text = Self::authorized(operation, self.send(operation, body, bearer).await?)?;
        serde_json::from_str(&text).map_err(|source| Error::Decode {
            operation,
            source,
            body: text,
        })
    }

    async fn request_text(
        &self,
        operation: Operation,
        body: Option<&[u8]>,
        auth: AuthMode<'_>,
    ) -> Result<String> {
        let token = match auth {
            AuthMode::None => None,
            AuthMode::Explicit(token) => {
                return Self::authorized(operation, self.send(operation, body, Some(token)).await?);
            }
            AuthMode::Session => Some(self.ensure_token().await?),
            AuthMode::Optional => self.optional_token().await?,
        };
        let Some(token) = token else {
            return Self::authorized(operation, self.send(operation, body, None).await?);
        };
        match self.send(operation, body, Some(&token.token)).await? {
            Outcome::Success(text) => Ok(text),
            Outcome::Unauthorized(text) => {
                if self.inner.credentials.is_none() {
                    return Err(Error::Unauthorized {
                        operation,
                        body: text,
                    });
                }
                tracing::info!(%operation, "HTTP 401; logging in again and retrying once");
                let fresh = self.recover_from_unauthorized(token.generation).await?;
                Self::authorized(
                    operation,
                    self.send(operation, body, Some(&fresh.token)).await?,
                )
            }
        }
    }

    fn authorized(operation: Operation, outcome: Outcome) -> Result<String> {
        match outcome {
            Outcome::Success(text) => Ok(text),
            Outcome::Unauthorized(body) => Err(Error::Unauthorized { operation, body }),
        }
    }

    /// Sends one logical request, applying the rate limiter and the retry policy.
    async fn send(
        &self,
        operation: Operation,
        body: Option<&[u8]>,
        bearer: Option<&str>,
    ) -> Result<Outcome> {
        let url = self.url(operation)?;
        let mut retries = 0u32;
        loop {
            if let Some(limiter) = &self.inner.limiter {
                limiter.acquire(operation.rate_bucket()).await;
            }
            match self.attempt(operation, &url, body, bearer, retries).await {
                Attempt::Finished(result) => return result,
                Attempt::Retryable { error, delay } => {
                    if retries >= self.inner.retry.max_retries {
                        return Err(error);
                    }
                    tracing::debug!(%operation, %error, ?delay, retry = retries + 1, "retrying request");
                    tokio::time::sleep(delay).await;
                    retries += 1;
                }
            }
        }
    }

    async fn attempt(
        &self,
        operation: Operation,
        url: &Url,
        body: Option<&[u8]>,
        bearer: Option<&str>,
        retry_index: u32,
    ) -> Attempt {
        let transient_retry = operation.retry_class() == RetryClass::Read;
        let backoff = self.inner.retry.backoff(retry_index);
        let transport_failure = |source: reqwest::Error| {
            let error = Error::Transport { operation, source };
            if transient_retry {
                Attempt::Retryable {
                    error,
                    delay: backoff,
                }
            } else {
                Attempt::Finished(Err(error))
            }
        };

        let method = match operation.method() {
            HttpMethod::Get => Method::GET,
            HttpMethod::Post => Method::POST,
        };
        let mut request = self
            .inner
            .http
            .request(method, url.clone())
            .header(ACCEPT, "application/json");
        request = match (body, operation.method()) {
            (Some(body), _) => request
                .header(CONTENT_TYPE, "application/json")
                .body(body.to_vec()),
            // Validate and logout POST an empty body.
            (None, HttpMethod::Post) => request.body(Vec::new()),
            (None, HttpMethod::Get) => request,
        };
        if let Some(token) = bearer {
            request = request.bearer_auth(token);
        }

        let response = match request.send().await {
            Ok(response) => response,
            Err(source) => return transport_failure(source),
        };
        let status = response.status();
        let retry_after = retry::retry_after(response.headers());
        let text = match response.text().await {
            Ok(text) => text,
            Err(source) => return transport_failure(source),
        };

        if status.is_success() {
            return Attempt::Finished(Ok(Outcome::Success(text)));
        }
        match status {
            StatusCode::UNAUTHORIZED => Attempt::Finished(Ok(Outcome::Unauthorized(text))),
            StatusCode::TOO_MANY_REQUESTS => Attempt::Retryable {
                delay: retry_after.unwrap_or(backoff),
                error: Error::RateLimited {
                    operation,
                    retry_after,
                    body: text,
                },
            },
            StatusCode::BAD_GATEWAY
            | StatusCode::SERVICE_UNAVAILABLE
            | StatusCode::GATEWAY_TIMEOUT
                if transient_retry =>
            {
                Attempt::Retryable {
                    error: status_error(operation, status, text),
                    delay: backoff,
                }
            }
            _ => Attempt::Finished(Err(status_error(operation, status, text))),
        }
    }

    fn url(&self, operation: Operation) -> Result<Url> {
        let base = self.inner.base_url.as_str().trim_end_matches('/');
        Url::parse(&format!("{base}{}", operation.path()))
            .map_err(|err| Error::Config(format!("cannot build URL for {operation}: {err}")))
    }
}

fn status_error(operation: Operation, status: StatusCode, body: String) -> Error {
    let problem = serde_json::from_str::<ProblemDetails>(&body)
        .ok()
        .filter(|p| {
            p.title.is_some()
                || p.status.is_some()
                || p.detail.is_some()
                || p.problem_type.is_some()
        });
    StatusError {
        operation,
        status,
        body,
        problem,
    }
    .into()
}

/// Configures a [`Client`].
pub struct ClientBuilder {
    base_url: String,
    user_hub_url: String,
    market_hub_url: String,
    credentials: Option<Credentials>,
    token: Option<String>,
    timeout: Duration,
    connect_timeout: Duration,
    retry: RetryPolicy,
    rate_limits: Option<RateLimits>,
    validate_requests: bool,
    refresh_margin: Duration,
    user_agent: String,
    http_client: Option<reqwest::Client>,
}

impl fmt::Debug for ClientBuilder {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("ClientBuilder")
            .field("base_url", &self.base_url)
            .field("user_hub_url", &self.user_hub_url)
            .field("market_hub_url", &self.market_hub_url)
            .field("credentials", &self.credentials)
            .field("token", &self.token.as_ref().map(|_| "<redacted>"))
            .field("timeout", &self.timeout)
            .field("retry", &self.retry)
            .field("rate_limits", &self.rate_limits)
            .field("validate_requests", &self.validate_requests)
            .field("refresh_margin", &self.refresh_margin)
            .finish_non_exhaustive()
    }
}

impl Default for ClientBuilder {
    fn default() -> Self {
        ClientBuilder::new()
    }
}

impl ClientBuilder {
    /// Defaults: TopstepX URLs, 30 s timeout, 3 retries, documented rate limits, validation
    /// on, token refresh one hour before expiry.
    pub fn new() -> Self {
        ClientBuilder {
            base_url: DEFAULT_API_URL.to_owned(),
            user_hub_url: DEFAULT_USER_HUB_URL.to_owned(),
            market_hub_url: DEFAULT_MARKET_HUB_URL.to_owned(),
            credentials: None,
            token: None,
            timeout: DEFAULT_TIMEOUT,
            connect_timeout: DEFAULT_CONNECT_TIMEOUT,
            retry: RetryPolicy::default(),
            rate_limits: Some(RateLimits::default()),
            validate_requests: true,
            refresh_margin: DEFAULT_REFRESH_MARGIN,
            user_agent: concat!("topstepx-rust/", env!("CARGO_PKG_VERSION")).to_owned(),
            http_client: None,
        }
    }

    /// Starts from [`new`](Self::new) and applies these environment variables when set:
    /// `TOPSTEPX_USERNAME` and `TOPSTEPX_API_KEY` (credentials), `TOPSTEPX_API_URL`,
    /// `TOPSTEPX_USER_HUB_URL` and `TOPSTEPX_MARKET_HUB_URL`.
    pub fn from_env() -> Self {
        let var = |name: &str| {
            std::env::var(name)
                .ok()
                .filter(|value| !value.trim().is_empty())
        };
        let mut builder = ClientBuilder::new();
        if let (Some(username), Some(api_key)) = (var("TOPSTEPX_USERNAME"), var("TOPSTEPX_API_KEY"))
        {
            builder = builder.credentials(username, api_key);
        }
        if let Some(url) = var("TOPSTEPX_API_URL") {
            builder = builder.base_url(url);
        }
        if let Some(url) = var("TOPSTEPX_USER_HUB_URL") {
            builder = builder.user_hub_url(url);
        }
        if let Some(url) = var("TOPSTEPX_MARKET_HUB_URL") {
            builder = builder.market_hub_url(url);
        }
        builder
    }

    /// Platform username and API key, used to log in on demand and again when a token
    /// expires or is rejected.
    #[must_use]
    pub fn credentials(mut self, username: impl Into<String>, api_key: impl Into<String>) -> Self {
        self.credentials = Some(Credentials {
            username: username.into(),
            api_key: api_key.into(),
        });
        self
    }

    /// An existing session token. Without credentials the client cannot log in again once
    /// it expires, but it still renews it through `validate` while it is valid.
    #[must_use]
    pub fn token(mut self, token: impl Into<String>) -> Self {
        self.token = Some(token.into());
        self
    }

    /// REST base URL (scheme and host, optionally a path prefix; without `/api`).
    #[must_use]
    pub fn base_url(mut self, url: impl Into<String>) -> Self {
        self.base_url = url.into();
        self
    }

    /// SignalR user hub URL.
    #[must_use]
    pub fn user_hub_url(mut self, url: impl Into<String>) -> Self {
        self.user_hub_url = url.into();
        self
    }

    /// SignalR market hub URL.
    #[must_use]
    pub fn market_hub_url(mut self, url: impl Into<String>) -> Self {
        self.market_hub_url = url.into();
        self
    }

    /// Whole-request timeout (default 30 s). Ignored with [`http_client`](Self::http_client).
    #[must_use]
    pub fn timeout(mut self, timeout: Duration) -> Self {
        self.timeout = timeout;
        self
    }

    /// Connection timeout (default 10 s). Ignored with [`http_client`](Self::http_client).
    #[must_use]
    pub fn connect_timeout(mut self, timeout: Duration) -> Self {
        self.connect_timeout = timeout;
        self
    }

    /// Retry policy (default: 3 retries, 1 s initial backoff doubling up to 30 s).
    #[must_use]
    pub fn retry_policy(mut self, policy: RetryPolicy) -> Self {
        self.retry = policy;
        self
    }

    /// Shorthand for changing only the number of retries.
    #[must_use]
    pub fn max_retries(mut self, max_retries: u32) -> Self {
        self.retry.max_retries = max_retries;
        self
    }

    /// Client-side rate limits (default: the documented gateway limits).
    #[must_use]
    pub fn rate_limits(mut self, limits: RateLimits) -> Self {
        self.rate_limits = Some(limits);
        self
    }

    /// Disables the client-side rate limiter.
    #[must_use]
    pub fn disable_rate_limiting(mut self) -> Self {
        self.rate_limits = None;
        self
    }

    /// Enables or disables client-side request validation (default: enabled).
    #[must_use]
    pub fn validate_requests(mut self, enabled: bool) -> Self {
        self.validate_requests = enabled;
        self
    }

    /// How long before expiry the token is renewed (default: one hour). Zero renews only
    /// after expiry, by logging in again.
    #[must_use]
    pub fn refresh_margin(mut self, margin: Duration) -> Self {
        self.refresh_margin = margin;
        self
    }

    /// `User-Agent` header. Ignored with [`http_client`](Self::http_client).
    #[must_use]
    pub fn user_agent(mut self, user_agent: impl Into<String>) -> Self {
        self.user_agent = user_agent.into();
        self
    }

    /// Uses a preconfigured `reqwest` client, for proxies, custom roots or tracing
    /// middleware. Timeout and user-agent settings of this builder are then ignored.
    #[must_use]
    pub fn http_client(mut self, client: reqwest::Client) -> Self {
        self.http_client = Some(client);
        self
    }

    /// Validates the configuration and builds the client. No network I/O happens here.
    pub fn build(self) -> Result<Client> {
        let base_url = parse_url("base URL", &self.base_url, &["http", "https"])?;
        let user_hub_url = parse_url(
            "user hub URL",
            &self.user_hub_url,
            &["http", "https", "ws", "wss"],
        )?;
        let market_hub_url = parse_url(
            "market hub URL",
            &self.market_hub_url,
            &["http", "https", "ws", "wss"],
        )?;
        if let Some(limits) = &self.rate_limits {
            for limit in [limits.general, limits.history] {
                if limit.max_requests == 0 || limit.window.is_zero() {
                    return Err(Error::Config(
                        "rate limits need a positive request count and window; use disable_rate_limiting() instead"
                            .into(),
                    ));
                }
            }
        }
        if let Some(credentials) = &self.credentials {
            if credentials.username.trim().is_empty() || credentials.api_key.trim().is_empty() {
                return Err(Error::Config(
                    "username and API key must not be empty".into(),
                ));
            }
        }

        let http = match self.http_client {
            Some(client) => client,
            None => reqwest::Client::builder()
                .timeout(self.timeout)
                .connect_timeout(self.connect_timeout)
                .user_agent(self.user_agent)
                .build()
                .map_err(|err| Error::Config(format!("cannot build HTTP client: {err}")))?,
        };

        let session = Session::default();
        if let Some(token) = self.token {
            session.store(token);
        }
        Ok(Client {
            inner: Arc::new(Inner {
                http,
                base_url,
                user_hub_url,
                market_hub_url,
                credentials: self.credentials,
                session,
                limiter: self.rate_limits.map(RateLimiter::new),
                retry: self.retry,
                validate_requests: self.validate_requests,
                refresh_margin: self.refresh_margin,
            }),
        })
    }
}

fn parse_url(what: &str, value: &str, schemes: &[&str]) -> Result<Url> {
    let url = Url::parse(value.trim())
        .map_err(|err| Error::Config(format!("invalid {what} `{value}`: {err}")))?;
    if !schemes.contains(&url.scheme()) {
        return Err(Error::Config(format!(
            "{what} must use one of {schemes:?}, got `{}`",
            url.scheme()
        )));
    }
    Ok(url)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn builder_validates_configuration() {
        assert!(matches!(
            Client::builder().base_url("not a url").build(),
            Err(Error::Config(_))
        ));
        assert!(matches!(
            Client::builder().base_url("ftp://example.com").build(),
            Err(Error::Config(_))
        ));
        assert!(matches!(
            Client::builder().credentials("", "key").build(),
            Err(Error::Config(_))
        ));
        let zero = RateLimits {
            general: crate::RateLimit::new(0, Duration::from_secs(1)),
            ..RateLimits::default()
        };
        assert!(matches!(
            Client::builder().rate_limits(zero).build(),
            Err(Error::Config(_))
        ));
        Client::builder().disable_rate_limiting().build().unwrap();
    }

    #[test]
    fn urls_keep_path_prefixes() {
        let client = Client::builder()
            .base_url("https://gateway.example.com/prefix/")
            .build()
            .unwrap();
        assert_eq!(
            client.url(Operation::OrderPlace).unwrap().as_str(),
            "https://gateway.example.com/prefix/api/Order/place"
        );
        let client = Client::builder().build().unwrap();
        assert_eq!(
            client.url(Operation::StatusPing).unwrap().as_str(),
            "https://api.topstepx.com/api/Status/ping"
        );
    }

    #[test]
    fn debug_output_redacts_secrets() {
        let builder = Client::builder()
            .credentials("trader", "api-key-secret")
            .token("jwt-secret");
        let text = format!("{builder:?}");
        assert!(
            !text.contains("api-key-secret") && !text.contains("jwt-secret"),
            "{text}"
        );
        let client = builder.build().unwrap();
        let text = format!("{client:?}");
        assert!(
            !text.contains("api-key-secret") && !text.contains("jwt-secret"),
            "{text}"
        );
        assert!(text.contains("trader"));
    }

    #[tokio::test]
    async fn authenticated_calls_without_credentials_fail_before_io() {
        let client = Client::builder()
            .base_url("http://127.0.0.1:9")
            .build()
            .unwrap();
        let err = client.accounts().search(true).await.unwrap_err();
        assert!(matches!(err, Error::NotAuthenticated), "{err:?}");
    }
}
