use rust_decimal::{Decimal, RoundingStrategy};
use serde::{Deserialize, Serialize};

use crate::operation::{Endpoint, Operation};

/// A tradable contract (`ContractModel`).
///
/// Contract identifiers change as contracts roll. Retrieve current ones with
/// [`Contracts::search`](crate::api::Contracts::search) or
/// [`Contracts::available`](crate::api::Contracts::available) rather than hard-coding them.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
#[non_exhaustive]
pub struct Contract {
    /// Contract identifier, such as `CON.F.US.EP.U25`.
    pub id: String,
    /// Short name, such as `ESU5`.
    pub name: String,
    /// Description.
    pub description: String,
    /// Minimum price increment.
    #[serde(with = "rust_decimal::serde::float")]
    pub tick_size: Decimal,
    /// Currency value of one tick.
    #[serde(with = "rust_decimal::serde::float")]
    pub tick_value: Decimal,
    /// Whether this is the active (front) contract.
    pub active_contract: bool,
    /// Symbol identifier, such as `F.US.EP`.
    pub symbol_id: String,
}

impl Contract {
    /// Rounds `price` to the nearest multiple of [`tick_size`](Self::tick_size), with
    /// midpoints rounded away from zero. Prices are returned unchanged when the tick size is
    /// not positive.
    ///
    /// Trailing-stop and modification prices must be tick-aligned or the gateway rejects
    /// them.
    pub fn round_to_tick(&self, price: Decimal) -> Decimal {
        if self.tick_size <= Decimal::ZERO {
            return price;
        }
        let ticks = (price / self.tick_size)
            .round_dp_with_strategy(0, RoundingStrategy::MidpointAwayFromZero);
        (ticks * self.tick_size).normalize()
    }

    /// Returns `true` when `price` is an exact multiple of the tick size.
    pub fn is_tick_aligned(&self, price: Decimal) -> bool {
        self.tick_size > Decimal::ZERO && (price % self.tick_size).is_zero()
    }

    /// Number of ticks between two prices (`to - from`), rounded toward zero.
    pub fn ticks_between(&self, from: Decimal, to: Decimal) -> Option<i64> {
        use rust_decimal::prelude::ToPrimitive;
        if self.tick_size <= Decimal::ZERO {
            return None;
        }
        ((to - from) / self.tick_size).trunc().to_i64()
    }
}

/// Request of `POST /api/Contract/search` (`SearchContractRequest`).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SearchContractRequest {
    /// Text to search for, such as `ES`. The gateway returns at most 20 contracts.
    pub search_text: String,
    /// Selects the live (`true`) or simulated (`false`) market-data subscription.
    pub live: bool,
}

impl SearchContractRequest {
    /// Creates the request.
    pub fn new(search_text: impl Into<String>, live: bool) -> Self {
        SearchContractRequest {
            search_text: search_text.into(),
            live,
        }
    }
}

gateway_response! {
    /// Response of `POST /api/Contract/search` (`SearchContractResponse`).
    pub struct SearchContractResponse: SearchContractErrorCode {
        /// Matching contracts (at most 20); empty when absent.
        #[serde(default)]
        pub contracts: Vec<Contract>,
    }
}

impl Endpoint for SearchContractRequest {
    const OPERATION: Operation = Operation::ContractSearch;
    type Response = SearchContractResponse;
}

/// Request of `POST /api/Contract/searchById` (`SearchContractByIdRequest`).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SearchContractByIdRequest {
    /// Contract identifier.
    pub contract_id: String,
}

impl SearchContractByIdRequest {
    /// Creates the request.
    pub fn new(contract_id: impl Into<String>) -> Self {
        SearchContractByIdRequest {
            contract_id: contract_id.into(),
        }
    }
}

gateway_response! {
    /// Response of `POST /api/Contract/searchById` (`SearchContractByIdResponse`).
    pub struct SearchContractByIdResponse: SearchContractByIdErrorCode {
        /// The contract, when found.
        #[serde(default)]
        pub contract: Option<Contract>,
    }
}

impl Endpoint for SearchContractByIdRequest {
    const OPERATION: Operation = Operation::ContractSearchById;
    type Response = SearchContractByIdResponse;
}

/// Request of `POST /api/Contract/available` (`ListAvailableContractRequest`).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ListAvailableContractRequest {
    /// Selects the live (`true`) or simulated (`false`) market-data subscription.
    pub live: bool,
}

impl ListAvailableContractRequest {
    /// Creates the request.
    pub const fn new(live: bool) -> Self {
        ListAvailableContractRequest { live }
    }
}

gateway_response! {
    /// Response of `POST /api/Contract/available` (`ListAvailableContractResponse`).
    pub struct ListAvailableContractResponse: ListAvailableContractErrorCode {
        /// Available contracts; empty when absent.
        #[serde(default)]
        pub contracts: Vec<Contract>,
    }
}

impl Endpoint for ListAvailableContractRequest {
    const OPERATION: Operation = Operation::ContractAvailable;
    type Response = ListAvailableContractResponse;
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::str::FromStr;

    fn es() -> Contract {
        Contract {
            id: "CON.F.US.EP.U25".into(),
            name: "ESU5".into(),
            description: "E-mini".into(),
            tick_size: Decimal::from_str("0.25").unwrap(),
            tick_value: Decimal::from_str("12.5").unwrap(),
            active_contract: true,
            symbol_id: "F.US.EP".into(),
        }
    }

    #[test]
    fn tick_helpers() {
        let c = es();
        let d = |s: &str| Decimal::from_str(s).unwrap();
        assert_eq!(c.round_to_tick(d("5998.37")), d("5998.25"));
        assert_eq!(c.round_to_tick(d("5998.375")), d("5998.5"));
        assert!(c.is_tick_aligned(d("5998.5")));
        assert!(!c.is_tick_aligned(d("5998.1")));
        assert_eq!(c.ticks_between(d("6000"), d("6002")), Some(8));
        assert_eq!(c.ticks_between(d("6000"), d("5999.9")), Some(0));

        let mut fx = es();
        fx.tick_size = d("0.00005");
        assert_eq!(fx.round_to_tick(d("1.123456")), d("1.12345"));
    }
}
