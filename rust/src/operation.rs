//! The operation catalog and the traits tying request types to operations.

use std::fmt;

use serde::de::DeserializeOwned;
use serde::Serialize;

use crate::error::{ApiError, ErrorCode, Result};

/// HTTP method of an operation.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum HttpMethod {
    /// `GET`.
    Get,
    /// `POST`.
    Post,
}

impl HttpMethod {
    /// Upper-case method name.
    pub const fn as_str(self) -> &'static str {
        match self {
            HttpMethod::Get => "GET",
            HttpMethod::Post => "POST",
        }
    }
}

/// How an operation authenticates.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum AuthRequirement {
    /// No `Authorization` header (the login operations).
    None,
    /// Bearer token required.
    Bearer,
    /// Bearer token attached when one is held or obtainable, anonymous otherwise
    /// (`/api/Status/ping`, whose anonymous behavior is unverified).
    Optional,
}

/// Client-side rate-limit bucket (documented limits: history 50 per 30 s, general 200 per 60 s).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum RateBucket {
    /// Every operation except bar retrieval.
    General,
    /// `POST /api/History/retrieveBars`.
    History,
}

/// Whether an operation may be retried after transient failures.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum RetryClass {
    /// Read-only: retried on 429, 502, 503, 504 and transport errors.
    Read,
    /// State-changing: retried only on 429, which means the request was not processed.
    Write,
}

/// The 21 operations of the specification.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, PartialOrd, Ord)]
#[non_exhaustive]
pub enum Operation {
    /// `Account_SearchAccounts`: `POST /api/Account/search`.
    AccountSearch,
    /// `Auth_LoginApp`: `POST /api/Auth/loginApp` (not supported by the current guide).
    AuthLoginApp,
    /// `Auth_LoginKey`: `POST /api/Auth/loginKey`.
    AuthLoginKey,
    /// `Auth_Logout`: `POST /api/Auth/logout`.
    AuthLogout,
    /// `Auth_Validate`: `POST /api/Auth/validate`.
    AuthValidate,
    /// `Contract_SearchContracts`: `POST /api/Contract/search`.
    ContractSearch,
    /// `Contract_SearchContractById`: `POST /api/Contract/searchById`.
    ContractSearchById,
    /// `Contract_AvailableContracts`: `POST /api/Contract/available`.
    ContractAvailable,
    /// `History_GetBars`: `POST /api/History/retrieveBars`.
    HistoryRetrieveBars,
    /// `Order_SearchOrders`: `POST /api/Order/search`.
    OrderSearch,
    /// `Order_SearchOrderById`: `POST /api/Order/searchById`.
    OrderSearchById,
    /// `Order_SearchOpenOrders`: `POST /api/Order/searchOpen`.
    OrderSearchOpen,
    /// `Order_SearchOrdersQuery`: `POST /api/Order/v2/query`.
    OrderQuery,
    /// `Order_PlaceOrder`: `POST /api/Order/place`.
    OrderPlace,
    /// `Order_CancelOrder`: `POST /api/Order/cancel`.
    OrderCancel,
    /// `Order_ModifyOrder`: `POST /api/Order/modify`.
    OrderModify,
    /// `Position_SearchOpenPositions`: `POST /api/Position/searchOpen`.
    PositionSearchOpen,
    /// `Position_CloseContractPosition`: `POST /api/Position/closeContract`.
    PositionClose,
    /// `Position_PartialCloseContractPosition`: `POST /api/Position/partialCloseContract`.
    PositionPartialClose,
    /// `Status_Ping`: `GET /api/Status/ping`.
    StatusPing,
    /// `Trade_SearchHalfTurnTrades`: `POST /api/Trade/search`.
    TradeSearch,
}

struct Spec {
    id: &'static str,
    method: HttpMethod,
    path: &'static str,
    auth: AuthRequirement,
    bucket: RateBucket,
    retry: RetryClass,
}

const fn spec(
    id: &'static str,
    method: HttpMethod,
    path: &'static str,
    auth: AuthRequirement,
    bucket: RateBucket,
    retry: RetryClass,
) -> Spec {
    Spec {
        id,
        method,
        path,
        auth,
        bucket,
        retry,
    }
}

impl Operation {
    /// Every operation, in specification order.
    pub const ALL: [Operation; 21] = [
        Operation::AccountSearch,
        Operation::AuthLoginApp,
        Operation::AuthLoginKey,
        Operation::AuthLogout,
        Operation::AuthValidate,
        Operation::ContractSearch,
        Operation::ContractSearchById,
        Operation::ContractAvailable,
        Operation::HistoryRetrieveBars,
        Operation::OrderSearch,
        Operation::OrderSearchById,
        Operation::OrderSearchOpen,
        Operation::OrderQuery,
        Operation::OrderPlace,
        Operation::OrderCancel,
        Operation::OrderModify,
        Operation::PositionSearchOpen,
        Operation::PositionClose,
        Operation::PositionPartialClose,
        Operation::StatusPing,
        Operation::TradeSearch,
    ];

    const fn spec(self) -> Spec {
        use AuthRequirement::{Bearer, None as NoAuth, Optional};
        use HttpMethod::{Get, Post};
        use RateBucket::{General, History};
        use RetryClass::{Read, Write};
        match self {
            Operation::AccountSearch => spec(
                "Account_SearchAccounts",
                Post,
                "/api/Account/search",
                Bearer,
                General,
                Read,
            ),
            Operation::AuthLoginApp => spec(
                "Auth_LoginApp",
                Post,
                "/api/Auth/loginApp",
                NoAuth,
                General,
                Write,
            ),
            Operation::AuthLoginKey => spec(
                "Auth_LoginKey",
                Post,
                "/api/Auth/loginKey",
                NoAuth,
                General,
                Write,
            ),
            Operation::AuthLogout => spec(
                "Auth_Logout",
                Post,
                "/api/Auth/logout",
                Bearer,
                General,
                Write,
            ),
            Operation::AuthValidate => spec(
                "Auth_Validate",
                Post,
                "/api/Auth/validate",
                Bearer,
                General,
                Write,
            ),
            Operation::ContractSearch => spec(
                "Contract_SearchContracts",
                Post,
                "/api/Contract/search",
                Bearer,
                General,
                Read,
            ),
            Operation::ContractSearchById => spec(
                "Contract_SearchContractById",
                Post,
                "/api/Contract/searchById",
                Bearer,
                General,
                Read,
            ),
            Operation::ContractAvailable => spec(
                "Contract_AvailableContracts",
                Post,
                "/api/Contract/available",
                Bearer,
                General,
                Read,
            ),
            Operation::HistoryRetrieveBars => spec(
                "History_GetBars",
                Post,
                "/api/History/retrieveBars",
                Bearer,
                History,
                Read,
            ),
            Operation::OrderSearch => spec(
                "Order_SearchOrders",
                Post,
                "/api/Order/search",
                Bearer,
                General,
                Read,
            ),
            Operation::OrderSearchById => spec(
                "Order_SearchOrderById",
                Post,
                "/api/Order/searchById",
                Bearer,
                General,
                Read,
            ),
            Operation::OrderSearchOpen => spec(
                "Order_SearchOpenOrders",
                Post,
                "/api/Order/searchOpen",
                Bearer,
                General,
                Read,
            ),
            Operation::OrderQuery => spec(
                "Order_SearchOrdersQuery",
                Post,
                "/api/Order/v2/query",
                Bearer,
                General,
                Read,
            ),
            Operation::OrderPlace => spec(
                "Order_PlaceOrder",
                Post,
                "/api/Order/place",
                Bearer,
                General,
                Write,
            ),
            Operation::OrderCancel => spec(
                "Order_CancelOrder",
                Post,
                "/api/Order/cancel",
                Bearer,
                General,
                Write,
            ),
            Operation::OrderModify => spec(
                "Order_ModifyOrder",
                Post,
                "/api/Order/modify",
                Bearer,
                General,
                Write,
            ),
            Operation::PositionSearchOpen => spec(
                "Position_SearchOpenPositions",
                Post,
                "/api/Position/searchOpen",
                Bearer,
                General,
                Read,
            ),
            Operation::PositionClose => spec(
                "Position_CloseContractPosition",
                Post,
                "/api/Position/closeContract",
                Bearer,
                General,
                Write,
            ),
            Operation::PositionPartialClose => spec(
                "Position_PartialCloseContractPosition",
                Post,
                "/api/Position/partialCloseContract",
                Bearer,
                General,
                Write,
            ),
            Operation::StatusPing => spec(
                "Status_Ping",
                Get,
                "/api/Status/ping",
                Optional,
                General,
                Read,
            ),
            Operation::TradeSearch => spec(
                "Trade_SearchHalfTurnTrades",
                Post,
                "/api/Trade/search",
                Bearer,
                General,
                Read,
            ),
        }
    }

    /// The specification's `operationId`, such as `Order_PlaceOrder`.
    pub const fn operation_id(self) -> &'static str {
        self.spec().id
    }

    /// HTTP method.
    pub const fn method(self) -> HttpMethod {
        self.spec().method
    }

    /// Path relative to the API base URL, such as `/api/Order/place`.
    pub const fn path(self) -> &'static str {
        self.spec().path
    }

    /// Authentication requirement.
    pub const fn auth(self) -> AuthRequirement {
        self.spec().auth
    }

    /// Client-side rate-limit bucket.
    pub const fn rate_bucket(self) -> RateBucket {
        self.spec().bucket
    }

    /// Retry class.
    pub const fn retry_class(self) -> RetryClass {
        self.spec().retry
    }

    /// Looks up an operation by its `operationId`.
    pub fn from_operation_id(id: &str) -> Option<Operation> {
        Operation::ALL
            .into_iter()
            .find(|op| op.operation_id() == id)
    }
}

impl fmt::Display for Operation {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.operation_id())
    }
}

/// Common view of every gateway response envelope.
pub trait GatewayResponse {
    /// The `success` flag.
    fn success(&self) -> bool;

    /// The typed `errorCode`.
    fn error_code(&self) -> ErrorCode;

    /// The `errorMessage`, which may be absent even for failures.
    fn error_message(&self) -> Option<&str>;

    /// Order id returned alongside a failure; only `PlaceOrderResponse` reports one.
    fn failure_order_id(&self) -> Option<i64> {
        None
    }

    /// `true` when `success` is false or `errorCode` is non-zero.
    fn is_business_failure(&self) -> bool {
        !self.success() || self.error_code().value() != 0
    }

    /// Converts a business failure into an [`ApiError`]; `Ok(())` when the response
    /// succeeded.
    fn check(&self, operation: Operation) -> Result<(), ApiError> {
        if self.is_business_failure() {
            Err(ApiError {
                operation,
                code: self.error_code(),
                message: self.error_message().map(str::to_owned),
                order_id: self.failure_order_id(),
            })
        } else {
            Ok(())
        }
    }
}

/// A request type bound to its operation, usable with [`Client::execute`] and
/// [`Client::execute_raw`].
///
/// [`Client::execute`]: crate::Client::execute
/// [`Client::execute_raw`]: crate::Client::execute_raw
pub trait Endpoint: Serialize + Sync {
    /// The operation this request belongs to.
    const OPERATION: Operation;

    /// Whether the request is sent as a JSON body (`false` for logout and validate, which
    /// POST an empty body).
    const HAS_BODY: bool = true;

    /// The response envelope.
    type Response: GatewayResponse + DeserializeOwned + Send + 'static;

    /// Client-side validation from the specification; runs before any network I/O.
    fn validate(&self) -> Result<()> {
        Ok(())
    }
}

/// Empty request for `POST /api/Auth/logout`.
///
/// Executing it through [`Client::execute`](crate::Client::execute) does not clear the
/// stored token; use [`Auth::logout`](crate::api::Auth::logout) for that.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize)]
pub struct LogoutRequest;

/// Empty request for `POST /api/Auth/validate`.
///
/// Executing it through [`Client::execute`](crate::Client::execute) does not store the
/// renewed token; use [`Auth::validate`](crate::api::Auth::validate) for that.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize)]
pub struct ValidateRequest;

impl Endpoint for LogoutRequest {
    const OPERATION: Operation = Operation::AuthLogout;
    const HAS_BODY: bool = false;
    type Response = crate::models::LogoutResponse;
}

impl Endpoint for ValidateRequest {
    const OPERATION: Operation = Operation::AuthValidate;
    const HAS_BODY: bool = false;
    type Response = crate::models::ValidateResponse;
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn catalog_is_consistent() {
        let mut ids: Vec<_> = Operation::ALL.iter().map(|op| op.operation_id()).collect();
        ids.sort_unstable();
        ids.dedup();
        assert_eq!(ids.len(), 21);
        for op in Operation::ALL {
            assert_eq!(Operation::from_operation_id(op.operation_id()), Some(op));
            assert!(op.path().starts_with("/api/"));
        }
        assert_eq!(
            Operation::HistoryRetrieveBars.rate_bucket(),
            RateBucket::History
        );
        assert_eq!(Operation::OrderPlace.retry_class(), RetryClass::Write);
        assert_eq!(Operation::StatusPing.method(), HttpMethod::Get);
        assert_eq!(Operation::AuthLoginKey.auth(), AuthRequirement::None);
    }
}
