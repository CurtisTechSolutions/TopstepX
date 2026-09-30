use chrono::{DateTime, Utc};
use rust_decimal::Decimal;
use serde::{Deserialize, Serialize};

use crate::enums::{OrderSide, OrderSortField, OrderStatus, OrderType, SortDirection};
use crate::error::{Error, Result};
use crate::operation::{Endpoint, Operation};

/// An order (`OrderModel`).
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
#[non_exhaustive]
pub struct Order {
    /// Order identifier.
    pub id: i64,
    /// Owning account.
    pub account_id: i32,
    /// Contract identifier.
    pub contract_id: String,
    /// Symbol identifier.
    pub symbol_id: String,
    /// Creation time.
    #[serde(with = "crate::serde_util::timestamp")]
    pub creation_timestamp: DateTime<Utc>,
    /// Last update time. The specification requires it, but it is modeled as optional so
    /// a `null` update time cannot break decoding.
    #[serde(default, with = "crate::serde_util::timestamp_option")]
    pub update_timestamp: Option<DateTime<Utc>>,
    /// Status.
    pub status: OrderStatus,
    /// Order type (`type`).
    #[serde(rename = "type")]
    pub order_type: OrderType,
    /// Side.
    pub side: OrderSide,
    /// Quantity.
    pub size: i32,
    /// Limit price, if applicable.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub limit_price: Option<Decimal>,
    /// Stop price, if applicable.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub stop_price: Option<Decimal>,
    /// Filled quantity.
    pub fill_volume: i32,
    /// Average fill price, if filled.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub filled_price: Option<Decimal>,
    /// Custom tag supplied at placement.
    #[serde(default)]
    pub custom_tag: Option<String>,
    /// Trailing distance in ticks, for trailing stops.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub trail_distance: Option<i32>,
    /// For a trailing stop this is the trail **distance in price units** (ticks × tick size),
    /// not the absolute level that was submitted.
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        with = "rust_decimal::serde::float_option"
    )]
    pub trail_price: Option<Decimal>,
    /// Parent order (for bracket legs).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub parent_order_id: Option<i64>,
    /// Linked order (for OCO legs).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub linked_order_id: Option<i64>,
}

/// Request of `POST /api/Order/search` (`SearchOrderRequest`): orders by creation time.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SearchOrderRequest {
    /// Account to search.
    pub account_id: i32,
    /// Range start (creation time).
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

impl SearchOrderRequest {
    /// Creates an open-ended search starting at `start_timestamp`.
    pub fn new(account_id: i32, start_timestamp: DateTime<Utc>) -> Self {
        SearchOrderRequest {
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
    /// Response of `POST /api/Order/search` and `POST /api/Order/searchOpen`
    /// (`SearchOrderResponse`).
    pub struct SearchOrderResponse: SearchOrderErrorCode {
        /// Orders; empty when absent.
        #[serde(default)]
        pub orders: Vec<Order>,
    }
}

impl Endpoint for SearchOrderRequest {
    const OPERATION: Operation = Operation::OrderSearch;
    type Response = SearchOrderResponse;
}

/// Request of `POST /api/Order/searchById` (`SearchOrderByIdRequest`). Swagger-only operation.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SearchOrderByIdRequest {
    /// Account owning the order; it must belong to the caller.
    pub account_id: i32,
    /// Order identifier.
    pub order_id: i64,
}

gateway_response! {
    /// Response of `POST /api/Order/searchById` (`SearchOrderByIdResponse`).
    pub struct SearchOrderByIdResponse: SearchOrderByIdErrorCode {
        /// The order, when found.
        #[serde(default)]
        pub order: Option<Order>,
    }
}

impl Endpoint for SearchOrderByIdRequest {
    const OPERATION: Operation = Operation::OrderSearchById;
    type Response = SearchOrderByIdResponse;
}

/// Request of `POST /api/Order/searchOpen` (`SearchOpenOrderRequest`).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SearchOpenOrderRequest {
    /// Account to search.
    pub account_id: i32,
}

impl Endpoint for SearchOpenOrderRequest {
    const OPERATION: Operation = Operation::OrderSearchOpen;
    type Response = SearchOrderResponse;
}

/// Filter of `POST /api/Order/v2/query` (`GatewayOrderFilter`).
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct GatewayOrderFilter {
    /// Account to search.
    pub account_id: i32,
    /// Only these statuses.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub statuses: Option<Vec<OrderStatus>>,
    /// Only this contract.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub contract_id: Option<String>,
    /// Created at or after.
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        with = "crate::serde_util::timestamp_option"
    )]
    pub created_after: Option<DateTime<Utc>>,
    /// Created at or before.
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        with = "crate::serde_util::timestamp_option"
    )]
    pub created_before: Option<DateTime<Utc>>,
}

impl GatewayOrderFilter {
    /// Creates a filter matching every order of the account.
    pub fn new(account_id: i32) -> Self {
        GatewayOrderFilter {
            account_id,
            statuses: None,
            contract_id: None,
            created_after: None,
            created_before: None,
        }
    }

    /// Restricts to the given statuses.
    #[must_use]
    pub fn statuses(mut self, statuses: impl IntoIterator<Item = OrderStatus>) -> Self {
        self.statuses = Some(statuses.into_iter().collect());
        self
    }

    /// Restricts to one contract.
    #[must_use]
    pub fn contract_id(mut self, contract_id: impl Into<String>) -> Self {
        self.contract_id = Some(contract_id.into());
        self
    }

    /// Restricts to orders created at or after `time`.
    #[must_use]
    pub fn created_after(mut self, time: DateTime<Utc>) -> Self {
        self.created_after = Some(time);
        self
    }

    /// Restricts to orders created at or before `time`.
    #[must_use]
    pub fn created_before(mut self, time: DateTime<Utc>) -> Self {
        self.created_before = Some(time);
        self
    }
}

/// Request of `POST /api/Order/v2/query` (`SearchOrdersQueryRequest`). Swagger-only operation;
/// pagination defaults and bounds are not published.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SearchOrdersQueryRequest {
    /// Filter.
    pub filter: GatewayOrderFilter,
    /// Page size.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub page_size: Option<i32>,
    /// Zero-based page offset.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub page_offset: Option<i32>,
    /// Sort field.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub sort_by: Option<OrderSortField>,
    /// Sort direction.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub sort_direction: Option<SortDirection>,
    /// Whether to return `totalCount`.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub include_total_count: Option<bool>,
}

impl SearchOrdersQueryRequest {
    /// Creates a query with server-side defaults for paging and sorting.
    pub fn new(filter: GatewayOrderFilter) -> Self {
        SearchOrdersQueryRequest {
            filter,
            page_size: None,
            page_offset: None,
            sort_by: None,
            sort_direction: None,
            include_total_count: None,
        }
    }

    /// Sets the page size and offset.
    #[must_use]
    pub fn page(mut self, page_size: i32, page_offset: i32) -> Self {
        self.page_size = Some(page_size);
        self.page_offset = Some(page_offset);
        self
    }

    /// Sets the sort order.
    #[must_use]
    pub fn sort(mut self, sort_by: OrderSortField, direction: SortDirection) -> Self {
        self.sort_by = Some(sort_by);
        self.sort_direction = Some(direction);
        self
    }

    /// Requests `totalCount` in the response.
    #[must_use]
    pub fn include_total_count(mut self, include: bool) -> Self {
        self.include_total_count = Some(include);
        self
    }
}

gateway_response! {
    /// Response of `POST /api/Order/v2/query` (`SearchOrdersQueryResponse`).
    pub struct SearchOrdersQueryResponse: SearchOrderErrorCode {
        /// Orders of the requested page; empty when absent.
        #[serde(default)]
        pub orders: Vec<Order>,
        /// Total matches, when requested.
        #[serde(default)]
        pub total_count: Option<i32>,
    }
}

impl Endpoint for SearchOrdersQueryRequest {
    const OPERATION: Operation = Operation::OrderQuery;
    type Response = SearchOrdersQueryResponse;
}

/// Bracket attached to a placement (`PlaceOrderBracket`). Brackets require the account's
/// Auto OCO Brackets mode; Position Brackets mode rejects them with code 2.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PlaceOrderBracket {
    /// Distance from the entry, in ticks.
    pub ticks: i32,
    /// Bracket order type; one of [`OrderType::PLACEABLE`].
    #[serde(rename = "type")]
    pub order_type: OrderType,
}

impl PlaceOrderBracket {
    /// Creates a bracket.
    pub const fn new(ticks: i32, order_type: OrderType) -> Self {
        PlaceOrderBracket { ticks, order_type }
    }
}

/// Request of `POST /api/Order/place` (`PlaceOrderRequest`).
///
/// Use the constructors ([`market`](Self::market), [`limit`](Self::limit),
/// [`stop`](Self::stop), [`trailing_stop`](Self::trailing_stop), [`join_bid`](Self::join_bid),
/// [`join_ask`](Self::join_ask)) and the `with_*` methods.
///
/// Placement rules enforced client-side (see `spec/SDK_BEHAVIOR.md` §9):
/// * the type must be one of [`OrderType::PLACEABLE`], so `StopLimit` is rejected;
/// * a trailing stop requires `trail_price`, an **absolute** price aligned to the tick size
///   (the server converts it into whole ticks and rejects trails above 1,000 ticks);
/// * bracket types must also be placeable.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PlaceOrderRequest {
    /// Account to trade.
    pub account_id: i32,
    /// Contract to trade.
    pub contract_id: String,
    /// Order type (`type`).
    #[serde(rename = "type")]
    pub order_type: OrderType,
    /// Side.
    pub side: OrderSide,
    /// Quantity.
    pub size: i32,
    /// Limit price.
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        with = "rust_decimal::serde::float_option"
    )]
    pub limit_price: Option<Decimal>,
    /// Stop price.
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        with = "rust_decimal::serde::float_option"
    )]
    pub stop_price: Option<Decimal>,
    /// Absolute trailing-stop price aligned to the tick size (not a distance). Required for
    /// trailing stops.
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        with = "rust_decimal::serde::float_option"
    )]
    pub trail_price: Option<Decimal>,
    /// Caller-defined tag; must be unique within the account.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub custom_tag: Option<String>,
    /// Stop-loss bracket.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub stop_loss_bracket: Option<PlaceOrderBracket>,
    /// Take-profit bracket.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub take_profit_bracket: Option<PlaceOrderBracket>,
}

impl PlaceOrderRequest {
    /// Creates a request of any type with no prices set.
    pub fn new(
        account_id: i32,
        contract_id: impl Into<String>,
        order_type: OrderType,
        side: OrderSide,
        size: i32,
    ) -> Self {
        PlaceOrderRequest {
            account_id,
            contract_id: contract_id.into(),
            order_type,
            side,
            size,
            limit_price: None,
            stop_price: None,
            trail_price: None,
            custom_tag: None,
            stop_loss_bracket: None,
            take_profit_bracket: None,
        }
    }

    /// Market order.
    pub fn market(
        account_id: i32,
        contract_id: impl Into<String>,
        side: OrderSide,
        size: i32,
    ) -> Self {
        Self::new(account_id, contract_id, OrderType::Market, side, size)
    }

    /// Limit order at `limit_price`.
    pub fn limit(
        account_id: i32,
        contract_id: impl Into<String>,
        side: OrderSide,
        size: i32,
        limit_price: Decimal,
    ) -> Self {
        Self::new(account_id, contract_id, OrderType::Limit, side, size)
            .with_limit_price(limit_price)
    }

    /// Stop (stop-market) order triggered at `stop_price`.
    pub fn stop(
        account_id: i32,
        contract_id: impl Into<String>,
        side: OrderSide,
        size: i32,
        stop_price: Decimal,
    ) -> Self {
        Self::new(account_id, contract_id, OrderType::Stop, side, size).with_stop_price(stop_price)
    }

    /// Trailing stop whose initial stop is the absolute price `trail_price`, which must be
    /// aligned to the tick size (see [`Contract::round_to_tick`](crate::models::Contract::round_to_tick)).
    pub fn trailing_stop(
        account_id: i32,
        contract_id: impl Into<String>,
        side: OrderSide,
        size: i32,
        trail_price: Decimal,
    ) -> Self {
        Self::new(account_id, contract_id, OrderType::TrailingStop, side, size)
            .with_trail_price(trail_price)
    }

    /// Limit order joining the best bid.
    pub fn join_bid(
        account_id: i32,
        contract_id: impl Into<String>,
        side: OrderSide,
        size: i32,
    ) -> Self {
        Self::new(account_id, contract_id, OrderType::JoinBid, side, size)
    }

    /// Limit order joining the best ask.
    pub fn join_ask(
        account_id: i32,
        contract_id: impl Into<String>,
        side: OrderSide,
        size: i32,
    ) -> Self {
        Self::new(account_id, contract_id, OrderType::JoinAsk, side, size)
    }

    /// Sets the limit price.
    #[must_use]
    pub fn with_limit_price(mut self, price: Decimal) -> Self {
        self.limit_price = Some(price);
        self
    }

    /// Sets the stop price.
    #[must_use]
    pub fn with_stop_price(mut self, price: Decimal) -> Self {
        self.stop_price = Some(price);
        self
    }

    /// Sets the absolute trailing-stop price.
    #[must_use]
    pub fn with_trail_price(mut self, price: Decimal) -> Self {
        self.trail_price = Some(price);
        self
    }

    /// Sets the custom tag (unique within the account).
    #[must_use]
    pub fn with_custom_tag(mut self, tag: impl Into<String>) -> Self {
        self.custom_tag = Some(tag.into());
        self
    }

    /// Attaches a stop-loss bracket `ticks` away from the entry.
    #[must_use]
    pub fn with_stop_loss_bracket(mut self, ticks: i32, order_type: OrderType) -> Self {
        self.stop_loss_bracket = Some(PlaceOrderBracket::new(ticks, order_type));
        self
    }

    /// Attaches a take-profit bracket `ticks` away from the entry.
    #[must_use]
    pub fn with_take_profit_bracket(mut self, ticks: i32, order_type: OrderType) -> Self {
        self.take_profit_bracket = Some(PlaceOrderBracket::new(ticks, order_type));
        self
    }
}

impl Endpoint for PlaceOrderRequest {
    const OPERATION: Operation = Operation::OrderPlace;
    type Response = PlaceOrderResponse;

    fn validate(&self) -> Result<()> {
        let invalid = |message: String| Error::InvalidRequest {
            operation: Self::OPERATION,
            message,
        };
        if !self.order_type.is_placeable() {
            return Err(invalid(format!(
                "order type {} ({}) cannot be placed; supported types are Limit, Market, Stop, TrailingStop, JoinBid and JoinAsk",
                self.order_type,
                self.order_type.value()
            )));
        }
        if self.order_type == OrderType::TrailingStop && self.trail_price.is_none() {
            return Err(invalid(
                "a trailing stop (type 5) requires trail_price: an absolute price aligned to the tick size".into(),
            ));
        }
        for (name, bracket) in [
            ("stop_loss_bracket", &self.stop_loss_bracket),
            ("take_profit_bracket", &self.take_profit_bracket),
        ] {
            if let Some(bracket) = bracket {
                if !bracket.order_type.is_placeable() {
                    return Err(invalid(format!(
                        "{name} type {} ({}) is not supported",
                        bracket.order_type,
                        bracket.order_type.value()
                    )));
                }
            }
        }
        Ok(())
    }
}

gateway_response! {
    /// Response of `POST /api/Order/place` (`PlaceOrderResponse`).
    ///
    /// An `order_id` can be present even when placement failed; always check the result.
    pub struct PlaceOrderResponse: PlaceOrderErrorCode {
        /// Order identifier.
        #[serde(default)]
        pub order_id: Option<i64>,
    }

    fn failure_order_id(&self) -> Option<i64> {
        self.order_id
    }
}

/// Request of `POST /api/Order/cancel` (`CancelOrderRequest`).
///
/// Cancellation is documented for simulated accounts that are not copy-trading followers;
/// code 6 covers unsupported live accounts and followers.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct CancelOrderRequest {
    /// Account owning the order.
    pub account_id: i32,
    /// Order to cancel.
    pub order_id: i64,
}

gateway_response! {
    /// Response of `POST /api/Order/cancel` (`CancelOrderResponse`).
    pub struct CancelOrderResponse: CancelOrderErrorCode {}
}

impl Endpoint for CancelOrderRequest {
    const OPERATION: Operation = Operation::OrderCancel;
    type Response = CancelOrderResponse;
}

/// Request of `POST /api/Order/modify` (`ModifyOrderRequest`). Only the fields you set are
/// sent.
///
/// For a trailing stop, `trail_price` is a new **absolute** level; the server recalculates
/// the tick distance from its last price. Misalignment or a missing last price yields code 3.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ModifyOrderRequest {
    /// Account owning the order.
    pub account_id: i32,
    /// Order to modify.
    pub order_id: i64,
    /// New quantity.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub size: Option<i32>,
    /// New limit price.
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        with = "rust_decimal::serde::float_option"
    )]
    pub limit_price: Option<Decimal>,
    /// New stop price.
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        with = "rust_decimal::serde::float_option"
    )]
    pub stop_price: Option<Decimal>,
    /// New absolute trailing-stop level.
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        with = "rust_decimal::serde::float_option"
    )]
    pub trail_price: Option<Decimal>,
}

impl ModifyOrderRequest {
    /// Creates a modification that changes nothing yet.
    pub fn new(account_id: i32, order_id: i64) -> Self {
        ModifyOrderRequest {
            account_id,
            order_id,
            size: None,
            limit_price: None,
            stop_price: None,
            trail_price: None,
        }
    }

    /// Sets the new quantity.
    #[must_use]
    pub fn size(mut self, size: i32) -> Self {
        self.size = Some(size);
        self
    }

    /// Sets the new limit price.
    #[must_use]
    pub fn limit_price(mut self, price: Decimal) -> Self {
        self.limit_price = Some(price);
        self
    }

    /// Sets the new stop price.
    #[must_use]
    pub fn stop_price(mut self, price: Decimal) -> Self {
        self.stop_price = Some(price);
        self
    }

    /// Sets the new absolute trailing-stop level.
    #[must_use]
    pub fn trail_price(mut self, price: Decimal) -> Self {
        self.trail_price = Some(price);
        self
    }
}

gateway_response! {
    /// Response of `POST /api/Order/modify` (`ModifyOrderResponse`).
    pub struct ModifyOrderResponse: ModifyOrderErrorCode {}
}

impl Endpoint for ModifyOrderRequest {
    const OPERATION: Operation = Operation::OrderModify;
    type Response = ModifyOrderResponse;
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::str::FromStr;

    fn price(s: &str) -> Decimal {
        Decimal::from_str(s).unwrap()
    }

    #[test]
    fn validation_report_negative_cases() {
        // trailing-stop requires trailPrice (absent)
        let req: PlaceOrderRequest =
            serde_json::from_str(r#"{"accountId":1,"contractId":"C","type":5,"side":1,"size":1}"#)
                .unwrap();
        assert!(matches!(req.validate(), Err(Error::InvalidRequest { .. })));

        // trailing-stop rejects null trailPrice
        let req: PlaceOrderRequest = serde_json::from_str(
            r#"{"accountId":1,"contractId":"C","type":5,"side":1,"size":1,"trailPrice":null}"#,
        )
        .unwrap();
        assert!(matches!(req.validate(), Err(Error::InvalidRequest { .. })));

        // placement rejects unsupported StopLimit
        let req = PlaceOrderRequest::new(1, "C", OrderType::StopLimit, OrderSide::Bid, 1)
            .with_limit_price(price("1"))
            .with_stop_price(price("1"));
        assert!(matches!(req.validate(), Err(Error::InvalidRequest { .. })));
    }

    #[test]
    fn validation_accepts_supported_requests() {
        let ok = [
            PlaceOrderRequest::market(1, "C", OrderSide::Bid, 1),
            PlaceOrderRequest::limit(1, "C", OrderSide::Bid, 1, price("6000")),
            PlaceOrderRequest::stop(1, "C", OrderSide::Ask, 1, price("5990")),
            PlaceOrderRequest::trailing_stop(1, "C", OrderSide::Ask, 1, price("5998.5")),
            PlaceOrderRequest::join_bid(1, "C", OrderSide::Bid, 1),
            PlaceOrderRequest::join_ask(1, "C", OrderSide::Ask, 1),
            PlaceOrderRequest::market(1, "C", OrderSide::Bid, 1)
                .with_stop_loss_bracket(8, OrderType::Stop)
                .with_take_profit_bracket(16, OrderType::Limit),
        ];
        for req in ok {
            req.validate().unwrap();
        }
        let bad_bracket = PlaceOrderRequest::market(1, "C", OrderSide::Bid, 1)
            .with_stop_loss_bracket(8, OrderType::StopLimit);
        assert!(bad_bracket.validate().is_err());
        let unknown =
            PlaceOrderRequest::new(1, "C", OrderType::Unrecognized(42), OrderSide::Bid, 1);
        assert!(unknown.validate().is_err());
    }

    #[test]
    fn place_request_serializes_numbers_and_omits_unset_fields() {
        let req =
            PlaceOrderRequest::limit(123, "CON.F.US.EP.U25", OrderSide::Bid, 1, price("6000.25"))
                .with_custom_tag("t1");
        let json = serde_json::to_value(&req).unwrap();
        assert_eq!(
            json,
            serde_json::json!({
                "accountId": 123, "contractId": "CON.F.US.EP.U25", "type": 1, "side": 0,
                "size": 1, "limitPrice": 6000.25, "customTag": "t1"
            })
        );
    }

    #[test]
    fn decimals_decode_exactly() {
        let order: Order = serde_json::from_str(
            r#"{"id":1,"accountId":2,"contractId":"C","symbolId":"S","creationTimestamp":"2025-07-01T14:00:00Z",
                "updateTimestamp":null,"status":1,"type":1,"side":0,"size":1,"limitPrice":6000.25,
                "stopPrice":null,"fillVolume":0,"filledPrice":null,"customTag":null,"trailPrice":0.1}"#,
        )
        .unwrap();
        assert_eq!(order.limit_price, Some(price("6000.25")));
        assert_eq!(order.trail_price, Some(price("0.1")));
        assert_eq!(order.update_timestamp, None);
    }
}
