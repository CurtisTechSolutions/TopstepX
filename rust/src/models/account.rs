use rust_decimal::Decimal;
use serde::{Deserialize, Serialize};

use crate::operation::{Endpoint, Operation};

/// A trading account (`TradingAccountModel`).
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
#[non_exhaustive]
pub struct TradingAccount {
    /// Account identifier, used by every account-scoped operation.
    pub id: i32,
    /// Account name.
    pub name: String,
    /// Current balance.
    #[serde(with = "rust_decimal::serde::float")]
    pub balance: Decimal,
    /// Whether the account may trade.
    pub can_trade: bool,
    /// Whether the account is visible.
    pub is_visible: bool,
    /// Whether the account is simulated. Do not infer this from the market-data `live` flag.
    pub simulated: bool,
}

/// Request of `POST /api/Account/search` (`SearchAccountRequest`).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SearchAccountRequest {
    /// Return only active accounts.
    pub only_active_accounts: bool,
}

impl SearchAccountRequest {
    /// Creates the request.
    pub const fn new(only_active_accounts: bool) -> Self {
        SearchAccountRequest {
            only_active_accounts,
        }
    }
}

gateway_response! {
    /// Response of `POST /api/Account/search` (`SearchAccountResponse`).
    pub struct SearchAccountResponse: SearchAccountErrorCode {
        /// Matching accounts; empty when absent.
        #[serde(default)]
        pub accounts: Vec<TradingAccount>,
    }
}

impl Endpoint for SearchAccountRequest {
    const OPERATION: Operation = Operation::AccountSearch;
    type Response = SearchAccountResponse;
}
