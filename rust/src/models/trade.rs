use chrono::{DateTime, Utc};
use rust_decimal::Decimal;
use serde::{Deserialize, Serialize};

use crate::enums::OrderSide;
use crate::operation::{Endpoint, Operation};

/// An executed trade (`HalfTradeModel`).
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
#[non_exhaustive]
pub struct HalfTrade {
    /// Trade identifier.
    pub id: i64,
    /// Owning account.
    pub account_id: i32,
    /// Contract identifier.
    pub contract_id: String,
    /// Execution time.
    #[serde(with = "crate::serde_util::timestamp")]
    pub creation_timestamp: DateTime<Utc>,
    /// Execution price.
    #[serde(with = "rust_decimal::serde::float")]
    pub price: Decimal,
    /// Realized profit and loss; `None` (JSON `null`) marks a half-turn trade.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub profit_and_loss: Option<Decimal>,
    /// Fees.
    #[serde(with = "rust_decimal::serde::float")]
    pub fees: Decimal,
    /// Commissions, when reported.
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        with = "rust_decimal::serde::float_option"
    )]
    pub commissions: Option<Decimal>,
    /// Side.
    pub side: OrderSide,
    /// Quantity.
    pub size: i32,
    /// Whether the trade was voided.
    pub voided: bool,
    /// Order that produced the trade.
    pub order_id: i64,
}

impl HalfTrade {
    /// `true` for a half-turn (opening) trade, which has no profit and loss.
    pub fn is_half_turn(&self) -> bool {
        self.profit_and_loss.is_none()
    }
}

/// Request of `POST /api/Trade/search` (`SearchTradeRequest`).
///
/// The guide requires `startTimestamp` (Swagger makes it optional); this SDK follows the
/// guide.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SearchTradeRequest {
    /// Account to search.
    pub account_id: i32,
    /// Range start.
    #[serde(with = "crate::serde_util::timestamp")]
    pub start_timestamp: DateTime<Utc>,
    /// Optional range end.
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        with = "crate::serde_util::timestamp_option"
    )]
    pub end_timestamp: Option<DateTime<Utc>>,
}

impl SearchTradeRequest {
    /// Creates an open-ended search starting at `start_timestamp`.
    pub fn new(account_id: i32, start_timestamp: DateTime<Utc>) -> Self {
        SearchTradeRequest {
            account_id,
            start_timestamp,
            end_timestamp: None,
        }
    }

    /// Sets the range end.
    #[must_use]
    pub fn end(mut self, end_timestamp: DateTime<Utc>) -> Self {
        self.end_timestamp = Some(end_timestamp);
        self
    }
}

gateway_response! {
    /// Response of `POST /api/Trade/search` (`SearchHalfTradeResponse`).
    pub struct SearchHalfTradeResponse: SearchTradeErrorCode {
        /// Trades; empty when absent.
        #[serde(default)]
        pub trades: Vec<HalfTrade>,
    }
}

impl Endpoint for SearchTradeRequest {
    const OPERATION: Operation = Operation::TradeSearch;
    type Response = SearchHalfTradeResponse;
}
