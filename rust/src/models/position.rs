use chrono::{DateTime, Utc};
use rust_decimal::Decimal;
use serde::{Deserialize, Serialize};

use crate::enums::PositionType;
use crate::error::{Error, Result};
use crate::operation::{Endpoint, Operation};

/// An open position (`PositionModel`).
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
#[non_exhaustive]
pub struct Position {
    /// Position identifier.
    pub id: i32,
    /// Owning account.
    pub account_id: i32,
    /// Contract identifier.
    pub contract_id: String,
    /// Display name of the contract.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub contract_display_name: Option<String>,
    /// When the position was opened.
    #[serde(with = "crate::serde_util::timestamp")]
    pub creation_timestamp: DateTime<Utc>,
    /// Direction (`type`).
    #[serde(rename = "type")]
    pub position_type: PositionType,
    /// Quantity.
    pub size: i32,
    /// Average entry price.
    #[serde(with = "rust_decimal::serde::float")]
    pub average_price: Decimal,
}

/// Request of `POST /api/Position/searchOpen` (`SearchPositionRequest`).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SearchPositionRequest {
    /// Account to search.
    pub account_id: i32,
}

gateway_response! {
    /// Response of `POST /api/Position/searchOpen` (`SearchPositionResponse`).
    pub struct SearchPositionResponse: SearchPositionErrorCode {
        /// Open positions; empty when absent.
        #[serde(default)]
        pub positions: Vec<Position>,
    }
}

impl Endpoint for SearchPositionRequest {
    const OPERATION: Operation = Operation::PositionSearchOpen;
    type Response = SearchPositionResponse;
}

/// Request of `POST /api/Position/closeContract` (`CloseContractPositionRequest`).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct CloseContractPositionRequest {
    /// Account owning the position.
    pub account_id: i32,
    /// Contract whose position to close.
    pub contract_id: String,
}

gateway_response! {
    /// Response of `POST /api/Position/closeContract` (`ClosePositionResponse`).
    pub struct ClosePositionResponse: ClosePositionErrorCode {}
}

impl Endpoint for CloseContractPositionRequest {
    const OPERATION: Operation = Operation::PositionClose;
    type Response = ClosePositionResponse;
}

/// Request of `POST /api/Position/partialCloseContract`
/// (`PartialCloseContractPositionRequest`).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PartialCloseContractPositionRequest {
    /// Account owning the position.
    pub account_id: i32,
    /// Contract whose position to reduce.
    pub contract_id: String,
    /// Quantity to close: at least 1 and no more than the open size.
    pub size: i32,
}

gateway_response! {
    /// Response of `POST /api/Position/partialCloseContract` (`PartialClosePositionResponse`).
    ///
    /// Code 6 can mean the instrument is not tradable or has no execution price, and the
    /// message may be null.
    pub struct PartialClosePositionResponse: PartialClosePositionErrorCode {}
}

impl Endpoint for PartialCloseContractPositionRequest {
    const OPERATION: Operation = Operation::PositionPartialClose;
    type Response = PartialClosePositionResponse;

    fn validate(&self) -> Result<()> {
        if self.size < 1 {
            return Err(Error::InvalidRequest {
                operation: Self::OPERATION,
                message: format!("size must be at least 1 (got {})", self.size),
            });
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn partial_close_rejects_zero() {
        let req = PartialCloseContractPositionRequest {
            account_id: 1,
            contract_id: "C".into(),
            size: 0,
        };
        assert!(matches!(req.validate(), Err(Error::InvalidRequest { .. })));
        let req = PartialCloseContractPositionRequest { size: 1, ..req };
        req.validate().unwrap();
    }
}
