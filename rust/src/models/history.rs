use chrono::{DateTime, Utc};
use rust_decimal::Decimal;
use serde::{Deserialize, Serialize};

use crate::enums::AggregateBarUnit;
use crate::error::{Error, Result};
use crate::operation::{Endpoint, Operation};

/// Maximum number of bars a single request may ask for.
pub const MAX_BARS_PER_REQUEST: i32 = 20_000;

/// An OHLCV bar (`AggregateBarModel`). JSON fields are `t`, `o`, `h`, `l`, `c`, `v`, `d` and
/// `k`.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[non_exhaustive]
pub struct AggregateBar {
    /// Bar timestamp (`t`).
    #[serde(rename = "t", with = "crate::serde_util::timestamp")]
    pub timestamp: DateTime<Utc>,
    /// Opening price (`o`).
    #[serde(rename = "o", with = "rust_decimal::serde::float")]
    pub open: Decimal,
    /// Highest price (`h`).
    #[serde(rename = "h", with = "rust_decimal::serde::float")]
    pub high: Decimal,
    /// Lowest price (`l`).
    #[serde(rename = "l", with = "rust_decimal::serde::float")]
    pub low: Decimal,
    /// Closing price (`c`).
    #[serde(rename = "c", with = "rust_decimal::serde::float")]
    pub close: Decimal,
    /// Traded volume (`v`).
    #[serde(rename = "v")]
    pub volume: i64,
    /// Undocumented date field (`d`, format `date` in Swagger), exposed raw.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub d: Option<String>,
    /// Undocumented integer field (`k`), exposed raw.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub k: Option<i64>,
}

/// Request of `POST /api/History/retrieveBars` (`RetrieveBarRequest`).
///
/// Bar retrieval has its own rate limit: 50 requests per 30 seconds.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RetrieveBarRequest {
    /// Contract identifier (a string, despite the guide's `integer` typo).
    pub contract_id: String,
    /// Selects the live (`true`) or simulated (`false`) market-data subscription.
    pub live: bool,
    /// Range start.
    #[serde(with = "crate::serde_util::timestamp")]
    pub start_time: DateTime<Utc>,
    /// Range end.
    #[serde(with = "crate::serde_util::timestamp")]
    pub end_time: DateTime<Utc>,
    /// Aggregation unit.
    pub unit: AggregateBarUnit,
    /// Number of units per bar, such as 5 for 5-minute bars.
    pub unit_number: i32,
    /// Maximum number of bars to return; at most [`MAX_BARS_PER_REQUEST`].
    pub limit: i32,
    /// Whether to include the still-forming bar.
    pub include_partial_bar: bool,
}

impl RetrieveBarRequest {
    /// Creates a request for simulated data (`live: false`) without the partial bar.
    pub fn new(
        contract_id: impl Into<String>,
        unit: AggregateBarUnit,
        unit_number: i32,
        start_time: DateTime<Utc>,
        end_time: DateTime<Utc>,
        limit: i32,
    ) -> Self {
        RetrieveBarRequest {
            contract_id: contract_id.into(),
            live: false,
            start_time,
            end_time,
            unit,
            unit_number,
            limit,
            include_partial_bar: false,
        }
    }

    /// Sets the market-data subscription flag.
    #[must_use]
    pub fn live(mut self, live: bool) -> Self {
        self.live = live;
        self
    }

    /// Sets whether the still-forming bar is included.
    #[must_use]
    pub fn include_partial_bar(mut self, include: bool) -> Self {
        self.include_partial_bar = include;
        self
    }
}

gateway_response! {
    /// Response of `POST /api/History/retrieveBars` (`RetrieveBarResponse`).
    pub struct RetrieveBarResponse: RetrieveBarErrorCode {
        /// Bars; empty when absent.
        #[serde(default)]
        pub bars: Vec<AggregateBar>,
    }
}

impl Endpoint for RetrieveBarRequest {
    const OPERATION: Operation = Operation::HistoryRetrieveBars;
    type Response = RetrieveBarResponse;

    fn validate(&self) -> Result<()> {
        if self.limit > MAX_BARS_PER_REQUEST {
            return Err(Error::InvalidRequest {
                operation: Self::OPERATION,
                message: format!(
                    "limit {} exceeds the maximum of {MAX_BARS_PER_REQUEST} bars per request",
                    self.limit
                ),
            });
        }
        Ok(())
    }
}
