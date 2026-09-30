//! Error types.

use std::fmt;
use std::time::Duration;

use reqwest::StatusCode;

use crate::enums::*;
use crate::models::ProblemDetails;
use crate::operation::Operation;

/// Result alias used throughout the crate.
pub type Result<T, E = Error> = std::result::Result<T, E>;

/// Everything that can go wrong when talking to the gateway.
#[derive(Debug, thiserror::Error)]
#[non_exhaustive]
pub enum Error {
    /// The gateway processed the request and reported a business failure: HTTP 200 with
    /// `success == false` or a non-zero `errorCode`. Invalid login credentials land here too.
    #[error(transparent)]
    Api(#[from] ApiError),

    /// HTTP 401. Automatic re-login (when credentials are configured) already failed or
    /// was not possible.
    #[error("{operation}: unauthorized (HTTP 401)")]
    Unauthorized {
        /// Operation that failed.
        operation: Operation,
        /// Response body, possibly empty.
        body: String,
    },

    /// HTTP 429 after the configured retries were exhausted.
    #[error("{operation}: rate limited (HTTP 429){}", .retry_after.map(|d| format!(", retry after {d:?}")).unwrap_or_default())]
    RateLimited {
        /// Operation that failed.
        operation: Operation,
        /// Parsed `Retry-After` header of the last response, if any.
        retry_after: Option<Duration>,
        /// Response body, possibly empty.
        body: String,
    },

    /// Any other non-success HTTP status.
    #[error(transparent)]
    Status(Box<StatusError>),

    /// The request could not be sent or the response could not be read (DNS, connect, TLS,
    /// timeout, reset).
    #[error("{operation}: transport error: {source}")]
    Transport {
        /// Operation that failed.
        operation: Operation,
        /// Underlying HTTP client error.
        #[source]
        source: reqwest::Error,
    },

    /// The response body was not the expected JSON.
    #[error("{operation}: failed to decode response: {source}")]
    Decode {
        /// Operation that failed.
        operation: Operation,
        /// Underlying JSON error.
        #[source]
        source: serde_json::Error,
        /// Raw response body.
        body: String,
    },

    /// Client-side validation rejected the request before it was sent (see
    /// `spec/SDK_BEHAVIOR.md` §9). Disable with
    /// [`ClientBuilder::validate_requests`](crate::ClientBuilder::validate_requests).
    #[error("{operation}: invalid request: {message}")]
    InvalidRequest {
        /// Operation whose request was rejected.
        operation: Operation,
        /// What is wrong.
        message: String,
    },

    /// An authenticated operation was attempted without a token and without credentials to
    /// obtain one.
    #[error("not authenticated: configure credentials or a token")]
    NotAuthenticated,

    /// The gateway reported success but omitted data the operation must return.
    #[error("{operation}: successful response is missing `{field}`")]
    MissingData {
        /// Operation that returned the incomplete response.
        operation: Operation,
        /// Missing JSON field.
        field: &'static str,
    },

    /// Invalid client configuration, such as a malformed URL.
    #[error("invalid configuration: {0}")]
    Config(String),

    /// Realtime (SignalR) failure.
    #[cfg(feature = "realtime")]
    #[cfg_attr(docsrs, doc(cfg(feature = "realtime")))]
    #[error(transparent)]
    Realtime(#[from] crate::realtime::RealtimeError),
}

impl Error {
    /// Returns the business error, if this is one.
    pub fn as_api(&self) -> Option<&ApiError> {
        match self {
            Error::Api(err) => Some(err),
            _ => None,
        }
    }

    /// Returns the HTTP status when the failure came from an HTTP response.
    pub fn status(&self) -> Option<StatusCode> {
        match self {
            Error::Unauthorized { .. } => Some(StatusCode::UNAUTHORIZED),
            Error::RateLimited { .. } => Some(StatusCode::TOO_MANY_REQUESTS),
            Error::Status(err) => Some(err.status),
            _ => None,
        }
    }

    /// Returns the operation the error belongs to, when known.
    pub fn operation(&self) -> Option<Operation> {
        match self {
            Error::Api(err) => Some(err.operation),
            Error::Unauthorized { operation, .. }
            | Error::RateLimited { operation, .. }
            | Error::Transport { operation, .. }
            | Error::Decode { operation, .. }
            | Error::InvalidRequest { operation, .. }
            | Error::MissingData { operation, .. } => Some(*operation),
            Error::Status(err) => Some(err.operation),
            _ => None,
        }
    }
}

impl From<StatusError> for Error {
    fn from(err: StatusError) -> Self {
        Error::Status(Box::new(err))
    }
}

/// A business failure reported by the gateway.
#[derive(Debug, Clone, PartialEq, Eq)]
#[non_exhaustive]
pub struct ApiError {
    /// Operation that failed.
    pub operation: Operation,
    /// Typed result code.
    pub code: ErrorCode,
    /// `errorMessage` from the response; may be `None` even for failures.
    pub message: Option<String>,
    /// For [`Operation::OrderPlace`]: the order id returned with the failure, if any. A
    /// returned identifier does not mean the order was accepted.
    pub order_id: Option<i64>,
}

impl fmt::Display for ApiError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{} failed with {}", self.operation, self.code)?;
        if let Some(message) = &self.message {
            write!(f, ": {message}")?;
        }
        if let Some(order_id) = self.order_id {
            write!(f, " (order id {order_id})")?;
        }
        Ok(())
    }
}

impl std::error::Error for ApiError {}

/// A non-success HTTP status other than 401 and 429.
#[derive(Debug, Clone)]
#[non_exhaustive]
pub struct StatusError {
    /// Operation that failed.
    pub operation: Operation,
    /// HTTP status.
    pub status: StatusCode,
    /// Response body, possibly empty.
    pub body: String,
    /// RFC 7807 problem details, when the body contained them (for example HTTP 400 from
    /// `/api/Order/v2/query`).
    pub problem: Option<ProblemDetails>,
}

impl fmt::Display for StatusError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}: HTTP {}", self.operation, self.status)?;
        if let Some(problem) = &self.problem {
            if let Some(title) = &problem.title {
                write!(f, ": {title}")?;
            }
            if let Some(detail) = &problem.detail {
                write!(f, " ({detail})")?;
            }
        }
        Ok(())
    }
}

impl std::error::Error for StatusError {}

/// The typed `errorCode` of a failed operation. Each operation has its own code enumeration.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
#[non_exhaustive]
pub enum ErrorCode {
    /// `POST /api/Account/search`.
    SearchAccount(SearchAccountErrorCode),
    /// `POST /api/Auth/loginKey` and `POST /api/Auth/loginApp`.
    Login(LoginErrorCode),
    /// `POST /api/Auth/logout`.
    Logout(LogoutErrorCode),
    /// `POST /api/Auth/validate`.
    Validate(ValidateErrorCode),
    /// `POST /api/Contract/search`.
    SearchContract(SearchContractErrorCode),
    /// `POST /api/Contract/searchById`.
    SearchContractById(SearchContractByIdErrorCode),
    /// `POST /api/Contract/available`.
    ListAvailableContract(ListAvailableContractErrorCode),
    /// `POST /api/History/retrieveBars`.
    RetrieveBar(RetrieveBarErrorCode),
    /// `POST /api/Order/search`, `/api/Order/searchOpen` and `/api/Order/v2/query`.
    SearchOrder(SearchOrderErrorCode),
    /// `POST /api/Order/searchById`.
    SearchOrderById(SearchOrderByIdErrorCode),
    /// `POST /api/Order/place`.
    PlaceOrder(PlaceOrderErrorCode),
    /// `POST /api/Order/cancel`.
    CancelOrder(CancelOrderErrorCode),
    /// `POST /api/Order/modify`.
    ModifyOrder(ModifyOrderErrorCode),
    /// `POST /api/Position/searchOpen`.
    SearchPosition(SearchPositionErrorCode),
    /// `POST /api/Position/closeContract`.
    ClosePosition(ClosePositionErrorCode),
    /// `POST /api/Position/partialCloseContract`.
    PartialClosePosition(PartialClosePositionErrorCode),
    /// `POST /api/Trade/search`.
    SearchTrade(SearchTradeErrorCode),
}

macro_rules! error_code_dispatch {
    ($($variant:ident($ty:ident)),+ $(,)?) => {
        impl ErrorCode {
            /// Raw integer value; zero means success.
            pub fn value(self) -> i32 {
                match self {
                    $(ErrorCode::$variant(code) => code.value(),)+
                }
            }

            /// Specification name of the code, or `None` if this SDK does not know the value.
            pub fn name(self) -> Option<&'static str> {
                match self {
                    $(ErrorCode::$variant(code) => code.name(),)+
                }
            }
        }

        impl fmt::Display for ErrorCode {
            fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
                match self.name() {
                    Some(name) => write!(f, "{name} ({})", self.value()),
                    None => write!(f, "Unrecognized({})", self.value()),
                }
            }
        }

        $(
            impl From<$ty> for ErrorCode {
                fn from(code: $ty) -> Self {
                    ErrorCode::$variant(code)
                }
            }
        )+
    };
}

error_code_dispatch! {
    SearchAccount(SearchAccountErrorCode),
    Login(LoginErrorCode),
    Logout(LogoutErrorCode),
    Validate(ValidateErrorCode),
    SearchContract(SearchContractErrorCode),
    SearchContractById(SearchContractByIdErrorCode),
    ListAvailableContract(ListAvailableContractErrorCode),
    RetrieveBar(RetrieveBarErrorCode),
    SearchOrder(SearchOrderErrorCode),
    SearchOrderById(SearchOrderByIdErrorCode),
    PlaceOrder(PlaceOrderErrorCode),
    CancelOrder(CancelOrderErrorCode),
    ModifyOrder(ModifyOrderErrorCode),
    SearchPosition(SearchPositionErrorCode),
    ClosePosition(ClosePositionErrorCode),
    PartialClosePosition(PartialClosePositionErrorCode),
    SearchTrade(SearchTradeErrorCode),
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn api_error_display() {
        let err = ApiError {
            operation: Operation::OrderPlace,
            code: PlaceOrderErrorCode::OrderRejected.into(),
            message: Some("Trail Distance not set.".into()),
            order_id: Some(457),
        };
        assert_eq!(
            err.to_string(),
            "Order_PlaceOrder failed with OrderRejected (2): Trail Distance not set. (order id 457)"
        );
        let unknown = ErrorCode::from(LoginErrorCode::Unrecognized(77));
        assert_eq!(unknown.value(), 77);
        assert_eq!(unknown.name(), None);
        assert_eq!(unknown.to_string(), "Unrecognized(77)");
    }

    #[test]
    fn error_is_reasonably_small() {
        assert!(
            std::mem::size_of::<Error>() <= 96,
            "{}",
            std::mem::size_of::<Error>()
        );
    }
}
