//! Async Rust SDK for the [ProjectX Gateway API] as deployed by TopstepX.
//!
//! The crate covers the 21 REST operations of the derived OpenAPI reference shipped in
//! `spec/` and, with the default `realtime` feature, the SignalR user and market hubs.
//! Behavior shared with the Python and Go SDKs in this repository is specified in
//! `spec/SDK_BEHAVIOR.md`.
//!
//! # Quick start
//!
//! ```no_run
//! use topstepx::Client;
//!
//! # async fn run() -> topstepx::Result<()> {
//! let client = Client::builder()
//!     .credentials("my-platform-username", "my-api-key")
//!     .build()?;
//!
//! // The first authenticated call logs in; tokens are refreshed automatically.
//! for account in client.accounts().search(true).await? {
//!     println!("{} {} balance={}", account.id, account.name, account.balance);
//! }
//! # Ok(())
//! # }
//! ```
//!
//! # Design notes
//!
//! * Every high-level method checks the gateway's business result (`success` and
//!   `errorCode`) and returns [`Error::Api`] on failure. [`Client::execute_raw`] returns the
//!   full response envelope instead.
//! * Prices and money are [`Decimal`]s: exact on the way in, JSON numbers on the way out.
//! * Enumerations tolerate values this SDK does not know about (`Unrecognized(n)`).
//! * HTTP 429 is retried (honoring `Retry-After`), read-only operations are retried on
//!   transient failures, and a client-side sliding-window limiter keeps requests within the
//!   documented limits.
//!
//! [ProjectX Gateway API]: https://gateway.docs.projectx.com/docs/intro/

#![cfg_attr(docsrs, feature(doc_cfg))]
#![forbid(unsafe_code)]
#![warn(
    missing_docs,
    missing_debug_implementations,
    rust_2018_idioms,
    unreachable_pub
)]

#[macro_use]
mod macros;

pub mod api;
mod client;
pub mod enums;
mod error;
pub mod models;
mod operation;
mod rate_limit;
#[cfg(feature = "realtime")]
#[cfg_attr(docsrs, doc(cfg(feature = "realtime")))]
pub mod realtime;
mod retry;
mod serde_util;
mod session;

pub use client::{
    Client, ClientBuilder, DEFAULT_API_URL, DEFAULT_MARKET_HUB_URL, DEFAULT_USER_HUB_URL,
};
pub use error::{ApiError, Error, ErrorCode, Result, StatusError};
pub use operation::{
    AuthRequirement, Endpoint, GatewayResponse, HttpMethod, LogoutRequest, Operation, RateBucket,
    RetryClass, ValidateRequest,
};
pub use rate_limit::{RateLimit, RateLimits};
pub use retry::RetryPolicy;

/// Re-export of the decimal type used for every price and amount.
pub use rust_decimal::Decimal;
/// Re-export of the URL type used for endpoint configuration.
pub use url::Url;

/// Re-export of [`chrono`], whose `DateTime<Utc>` is used for every timestamp.
pub use chrono;
/// Re-export of [`rust_decimal`], matching the version this crate was built with.
pub use rust_decimal;

// Compiles the README's examples as doctests without publishing the README in the docs.
#[cfg(doctest)]
#[doc = include_str!("../README.md")]
struct ReadmeDoctests;

/// The most commonly used types, for glob import.
pub mod prelude {
    pub use crate::enums::*;
    pub use crate::models::*;
    #[cfg(feature = "realtime")]
    pub use crate::realtime::{MarketEvent, MarketHub, UserEvent, UserHub};
    pub use crate::{Client, Decimal, Error, Result};
}
