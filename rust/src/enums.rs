//! Integer enumerations from the specification.
//!
//! Every enum decodes values it does not know into an `Unrecognized(n)` variant instead of
//! failing, compares by numeric value, and serializes as the raw integer. Decoding also accepts
//! the specification name (case-insensitive) or a numeric string.

use std::fmt;
use std::marker::PhantomData;

use serde::de::{self, Unexpected, Visitor};

/// Behavior shared by every integer enumeration in this crate.
pub(crate) trait IntEnum: Sized {
    /// Rust type name, used in error messages.
    const TYPE_NAME: &'static str;
    /// Converts a wire value, mapping unknown values to `Unrecognized`.
    fn from_value(value: i32) -> Self;
    /// Looks up a variant by its specification name, ignoring ASCII case.
    fn from_name(name: &str) -> Option<Self>;
}

/// Serde visitor accepting integers, integral floats, numeric strings and variant names.
pub(crate) struct IntEnumVisitor<T>(PhantomData<T>);

impl<T> IntEnumVisitor<T> {
    pub(crate) const fn new() -> Self {
        IntEnumVisitor(PhantomData)
    }
}

impl<'de, T: IntEnum> Visitor<'de> for IntEnumVisitor<T> {
    type Value = T;

    fn expecting(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "an integer {} value", T::TYPE_NAME)
    }

    fn visit_i64<E: de::Error>(self, v: i64) -> Result<T, E> {
        i32::try_from(v)
            .map(T::from_value)
            .map_err(|_| E::invalid_value(Unexpected::Signed(v), &self))
    }

    fn visit_u64<E: de::Error>(self, v: u64) -> Result<T, E> {
        i32::try_from(v)
            .map(T::from_value)
            .map_err(|_| E::invalid_value(Unexpected::Unsigned(v), &self))
    }

    fn visit_f64<E: de::Error>(self, v: f64) -> Result<T, E> {
        if v.fract() == 0.0 && v >= f64::from(i32::MIN) && v <= f64::from(i32::MAX) {
            Ok(T::from_value(v as i32))
        } else {
            Err(E::invalid_value(Unexpected::Float(v), &self))
        }
    }

    fn visit_str<E: de::Error>(self, v: &str) -> Result<T, E> {
        let trimmed = v.trim();
        if let Ok(number) = trimmed.parse::<i32>() {
            return Ok(T::from_value(number));
        }
        T::from_name(trimmed).ok_or_else(|| E::invalid_value(Unexpected::Str(v), &self))
    }
}

int_enum! {
    /// Bar aggregation unit (`AggregateBarUnit`).
    ///
    /// The guide documents `Second` through `Month`; `Unspecified` and `Tick` appear only in
    /// Swagger and their acceptance by the server is unverified.
    pub enum AggregateBarUnit {
        /// Swagger-only; acceptance unverified.
        Unspecified = 0,
        /// Seconds.
        Second = 1,
        /// Minutes.
        Minute = 2,
        /// Hours.
        Hour = 3,
        /// Days.
        Day = 4,
        /// Weeks.
        Week = 5,
        /// Months.
        Month = 6,
        /// Swagger-only; acceptance unverified.
        Tick = 7,
    }
}

int_enum! {
    /// Order lifecycle status (`OrderStatus`).
    pub enum OrderStatus {
        /// No status.
        None = 0,
        /// Working at the exchange.
        Open = 1,
        /// Completely filled.
        Filled = 2,
        /// Cancelled.
        Cancelled = 3,
        /// Expired.
        Expired = 4,
        /// Rejected.
        Rejected = 5,
        /// Accepted but not yet working.
        Pending = 6,
        /// Cancellation requested.
        PendingCancellation = 7,
        /// Suspended.
        Suspended = 8,
    }
}

impl OrderStatus {
    /// Returns `true` for statuses after which the order can no longer change
    /// (`Filled`, `Cancelled`, `Expired`, `Rejected`).
    pub fn is_terminal(self) -> bool {
        matches!(
            self,
            OrderStatus::Filled
                | OrderStatus::Cancelled
                | OrderStatus::Expired
                | OrderStatus::Rejected
        )
    }
}

int_enum! {
    /// Order type (`OrderType`).
    ///
    /// Placement supports `Limit`, `Market`, `Stop`, `TrailingStop`, `JoinBid` and `JoinAsk`;
    /// `Unknown` and `StopLimit` appear only in responses.
    pub enum OrderType {
        /// Unknown type (responses only).
        Unknown = 0,
        /// Limit order.
        Limit = 1,
        /// Market order.
        Market = 2,
        /// Stop-limit order (responses only; placement is not supported).
        StopLimit = 3,
        /// Stop (stop-market) order.
        Stop = 4,
        /// Trailing stop order.
        TrailingStop = 5,
        /// Limit order joining the best bid.
        JoinBid = 6,
        /// Limit order joining the best ask.
        JoinAsk = 7,
    }
}

impl OrderType {
    /// The six types the placement guide supports.
    pub const PLACEABLE: [OrderType; 6] = [
        OrderType::Limit,
        OrderType::Market,
        OrderType::Stop,
        OrderType::TrailingStop,
        OrderType::JoinBid,
        OrderType::JoinAsk,
    ];

    /// Returns `true` when the type may be used in a placement request or bracket.
    pub fn is_placeable(self) -> bool {
        Self::PLACEABLE.contains(&self)
    }
}

int_enum! {
    /// Order side (`OrderSide`).
    pub enum OrderSide {
        /// Buy.
        Bid = 0,
        /// Sell.
        Ask = 1,
    }
}

impl OrderSide {
    /// Alias for [`OrderSide::Bid`].
    pub const BUY: OrderSide = OrderSide::Bid;
    /// Alias for [`OrderSide::Ask`].
    pub const SELL: OrderSide = OrderSide::Ask;

    /// Returns the opposite side; unrecognized values are returned unchanged.
    pub fn opposite(self) -> OrderSide {
        match self {
            OrderSide::Bid => OrderSide::Ask,
            OrderSide::Ask => OrderSide::Bid,
            other => other,
        }
    }
}

int_enum! {
    /// Position direction (`PositionType`).
    pub enum PositionType {
        /// Undefined.
        Undefined = 0,
        /// Long position.
        Long = 1,
        /// Short position.
        Short = 2,
    }
}

int_enum! {
    /// Sort field for `/api/Order/v2/query` (`OrderSortField`).
    pub enum OrderSortField {
        /// Order creation time.
        CreatedAt = 0,
        /// Order identifier.
        Id = 1,
    }
}

int_enum! {
    /// Sort direction for `/api/Order/v2/query` (`SortDirection`).
    pub enum SortDirection {
        /// Ascending.
        Asc = 0,
        /// Descending.
        Desc = 1,
    }
}

int_enum! {
    /// Market-depth entry type used by the realtime `GatewayDepth` event (`DomType`).
    ///
    /// Taken from the realtime guide; not part of the REST specification.
    pub enum DomType {
        /// Unknown entry.
        Unknown = 0,
        /// Ask level.
        Ask = 1,
        /// Bid level.
        Bid = 2,
        /// Best ask.
        BestAsk = 3,
        /// Best bid.
        BestBid = 4,
        /// Trade.
        Trade = 5,
        /// Book reset; discard the current book.
        Reset = 6,
        /// Session low.
        Low = 7,
        /// Session high.
        High = 8,
        /// New best bid.
        NewBestBid = 9,
        /// New best ask.
        NewBestAsk = 10,
        /// Fill.
        Fill = 11,
    }
}

int_enum! {
    /// Aggressor side of a realtime market trade (`TradeLogType`).
    ///
    /// Taken from the realtime guide; not part of the REST specification.
    pub enum TradeLogType {
        /// Buyer-initiated trade.
        Buy = 0,
        /// Seller-initiated trade.
        Sell = 1,
    }
}

int_enum! {
    /// Result code of `POST /api/Account/search`.
    pub enum SearchAccountErrorCode {
        /// Success.
        Success = 0,
    }
}

int_enum! {
    /// Result code of the login operations.
    pub enum LoginErrorCode {
        /// Success.
        Success = 0,
        /// The user does not exist.
        UserNotFound = 1,
        /// Password verification failed.
        PasswordVerificationFailed = 2,
        /// Username or API key is wrong.
        InvalidCredentials = 3,
        /// Application not found.
        AppNotFound = 4,
        /// Application verification failed.
        AppVerificationFailed = 5,
        /// Invalid device.
        InvalidDevice = 6,
        /// Required agreements are not signed.
        AgreementsNotSigned = 7,
        /// Unknown error.
        UnknownError = 8,
        /// No API subscription.
        ApiSubscriptionNotFound = 9,
        /// API-key authentication is disabled.
        ApiKeyAuthenticationDisabled = 10,
    }
}

int_enum! {
    /// Result code of `POST /api/Auth/logout`.
    pub enum LogoutErrorCode {
        /// Success.
        Success = 0,
        /// Invalid session.
        InvalidSession = 1,
        /// Unknown error.
        UnknownError = 2,
    }
}

int_enum! {
    /// Result code of `POST /api/Auth/validate`.
    pub enum ValidateErrorCode {
        /// Success.
        Success = 0,
        /// Invalid session.
        InvalidSession = 1,
        /// Session not found.
        SessionNotFound = 2,
        /// Token expired.
        ExpiredToken = 3,
        /// Unknown error.
        UnknownError = 4,
    }
}

int_enum! {
    /// Result code of `POST /api/Contract/search`.
    pub enum SearchContractErrorCode {
        /// Success.
        Success = 0,
    }
}

int_enum! {
    /// Result code of `POST /api/Contract/searchById`.
    pub enum SearchContractByIdErrorCode {
        /// Success.
        Success = 0,
        /// No contract has that identifier.
        ContractNotFound = 1,
    }
}

int_enum! {
    /// Result code of `POST /api/Contract/available`.
    pub enum ListAvailableContractErrorCode {
        /// Success.
        Success = 0,
    }
}

int_enum! {
    /// Result code of `POST /api/History/retrieveBars`.
    pub enum RetrieveBarErrorCode {
        /// Success.
        Success = 0,
        /// Contract not found.
        ContractNotFound = 1,
        /// Invalid unit.
        UnitInvalid = 2,
        /// Invalid unit number.
        UnitNumberInvalid = 3,
        /// Invalid limit.
        LimitInvalid = 4,
    }
}

int_enum! {
    /// Result code of the order search operations (`search`, `searchOpen`, `v2/query`).
    pub enum SearchOrderErrorCode {
        /// Success.
        Success = 0,
        /// Account not found.
        AccountNotFound = 1,
    }
}

int_enum! {
    /// Result code of `POST /api/Order/searchById`.
    pub enum SearchOrderByIdErrorCode {
        /// Success.
        Success = 0,
        /// Order not found.
        OrderNotFound = 1,
    }
}

int_enum! {
    /// Result code of `POST /api/Order/place`.
    pub enum PlaceOrderErrorCode {
        /// Success.
        Success = 0,
        /// Account not found.
        AccountNotFound = 1,
        /// Order rejected; also returned for brackets in Position Brackets mode.
        OrderRejected = 2,
        /// Insufficient funds.
        InsufficientFunds = 3,
        /// Account violation.
        AccountViolation = 4,
        /// Outside trading hours.
        OutsideTradingHours = 5,
        /// Order pending.
        OrderPending = 6,
        /// Unknown error.
        UnknownError = 7,
        /// Contract not found.
        ContractNotFound = 8,
        /// Contract not active.
        ContractNotActive = 9,
        /// Account rejected.
        AccountRejected = 10,
    }
}

int_enum! {
    /// Result code of `POST /api/Order/cancel`.
    pub enum CancelOrderErrorCode {
        /// Success.
        Success = 0,
        /// Account not found.
        AccountNotFound = 1,
        /// Order not found.
        OrderNotFound = 2,
        /// Rejected.
        Rejected = 3,
        /// Pending.
        Pending = 4,
        /// Unknown error.
        UnknownError = 5,
        /// Account rejected; covers unsupported live accounts and copy-trading followers.
        AccountRejected = 6,
    }
}

int_enum! {
    /// Result code of `POST /api/Order/modify`.
    pub enum ModifyOrderErrorCode {
        /// Success.
        Success = 0,
        /// Account not found.
        AccountNotFound = 1,
        /// Order not found.
        OrderNotFound = 2,
        /// Rejected; also returned for misaligned prices or a missing last price.
        Rejected = 3,
        /// Pending.
        Pending = 4,
        /// Unknown error.
        UnknownError = 5,
        /// Account rejected.
        AccountRejected = 6,
        /// Contract not found.
        ContractNotFound = 7,
    }
}

int_enum! {
    /// Result code of `POST /api/Position/searchOpen`.
    pub enum SearchPositionErrorCode {
        /// Success.
        Success = 0,
        /// Account not found.
        AccountNotFound = 1,
    }
}

int_enum! {
    /// Result code of `POST /api/Position/closeContract`.
    pub enum ClosePositionErrorCode {
        /// Success.
        Success = 0,
        /// Account not found.
        AccountNotFound = 1,
        /// Position not found.
        PositionNotFound = 2,
        /// Contract not found.
        ContractNotFound = 3,
        /// Contract not active.
        ContractNotActive = 4,
        /// Order rejected.
        OrderRejected = 5,
        /// Order pending.
        OrderPending = 6,
        /// Unknown error.
        UnknownError = 7,
        /// Account rejected.
        AccountRejected = 8,
    }
}

int_enum! {
    /// Result code of `POST /api/Position/partialCloseContract`.
    pub enum PartialClosePositionErrorCode {
        /// Success.
        Success = 0,
        /// Account not found.
        AccountNotFound = 1,
        /// Position not found.
        PositionNotFound = 2,
        /// Contract not found.
        ContractNotFound = 3,
        /// Contract not active.
        ContractNotActive = 4,
        /// Close size is invalid.
        InvalidCloseSize = 5,
        /// Order rejected; can mean the instrument is not tradable or has no execution price.
        OrderRejected = 6,
        /// Order pending.
        OrderPending = 7,
        /// Unknown error.
        UnknownError = 8,
        /// Account rejected; includes unsupported live accounts.
        AccountRejected = 9,
    }
}

int_enum! {
    /// Result code of `POST /api/Trade/search`.
    pub enum SearchTradeErrorCode {
        /// Success.
        Success = 0,
        /// Account not found.
        AccountNotFound = 1,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn known_values_round_trip() {
        for status in OrderStatus::KNOWN {
            assert_eq!(OrderStatus::from_value(status.value()), *status);
            assert!(status.is_known());
        }
        assert_eq!(OrderStatus::Open.value(), 1);
        assert_eq!(OrderStatus::Open.name(), Some("Open"));
        assert_eq!(OrderStatus::None.to_string(), "None");
    }

    #[test]
    fn unknown_values_are_preserved() {
        let status: OrderStatus = serde_json::from_str("42").unwrap();
        assert_eq!(status, OrderStatus::Unrecognized(42));
        assert_eq!(status.value(), 42);
        assert_eq!(status.name(), None);
        assert!(!status.is_known());
        assert_eq!(status.to_string(), "Unrecognized(42)");
        assert_eq!(serde_json::to_string(&status).unwrap(), "42");
    }

    #[test]
    fn unrecognized_with_known_value_is_canonical() {
        assert_eq!(OrderStatus::Unrecognized(1), OrderStatus::Open);
        assert_eq!(OrderStatus::Unrecognized(1).name(), Some("Open"));
        let mut set = std::collections::HashSet::new();
        set.insert(OrderStatus::Open);
        assert!(set.contains(&OrderStatus::Unrecognized(1)));
    }

    #[test]
    fn decodes_names_numeric_strings_and_integral_floats() {
        let t: OrderType = serde_json::from_str("\"trailingstop\"").unwrap();
        assert_eq!(t, OrderType::TrailingStop);
        let t: OrderType = serde_json::from_str("\"2\"").unwrap();
        assert_eq!(t, OrderType::Market);
        let t: OrderType = serde_json::from_str("4.0").unwrap();
        assert_eq!(t, OrderType::Stop);
        assert!(serde_json::from_str::<OrderType>("4.5").is_err());
        assert!(serde_json::from_str::<OrderType>("\"bogus\"").is_err());
        assert!(serde_json::from_str::<OrderType>("99999999999").is_err());
    }

    #[test]
    fn placeable_types_follow_the_guide() {
        assert!(OrderType::Limit.is_placeable());
        assert!(OrderType::JoinAsk.is_placeable());
        assert!(!OrderType::StopLimit.is_placeable());
        assert!(!OrderType::Unknown.is_placeable());
        assert!(!OrderType::Unrecognized(9).is_placeable());
    }

    #[test]
    fn side_helpers() {
        assert_eq!(OrderSide::BUY, OrderSide::Bid);
        assert_eq!(OrderSide::SELL.opposite(), OrderSide::Bid);
        assert!(OrderStatus::Filled.is_terminal());
        assert!(!OrderStatus::Open.is_terminal());
    }
}
