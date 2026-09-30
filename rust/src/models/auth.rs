use std::fmt;

use serde::{Deserialize, Serialize};

use crate::operation::{Endpoint, Operation};

const REDACTED: &str = "<redacted>";

fn redact(value: &Option<String>) -> Option<&'static str> {
    value.as_ref().map(|_| REDACTED)
}

/// Request of `POST /api/Auth/loginKey` (`LoginApiKeyRequest`).
///
/// `Debug` output redacts the API key.
#[derive(Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct LoginApiKeyRequest {
    /// Platform sign-in username (case-insensitive); not an email address or account name.
    pub user_name: String,
    /// API key issued in the firm's trading platform.
    pub api_key: String,
}

impl LoginApiKeyRequest {
    /// Creates the request.
    pub fn new(user_name: impl Into<String>, api_key: impl Into<String>) -> Self {
        LoginApiKeyRequest {
            user_name: user_name.into(),
            api_key: api_key.into(),
        }
    }
}

impl fmt::Debug for LoginApiKeyRequest {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("LoginApiKeyRequest")
            .field("user_name", &self.user_name)
            .field("api_key", &REDACTED)
            .finish()
    }
}

impl Endpoint for LoginApiKeyRequest {
    const OPERATION: Operation = Operation::AuthLoginKey;
    type Response = LoginResponse;
}

/// Request of `POST /api/Auth/loginApp` (`LoginAppRequest`).
///
/// This operation is in the Swagger schema, but the current authentication guide supports
/// only API-key login. Prefer [`LoginApiKeyRequest`]. `Debug` output redacts the password and
/// verification key.
#[derive(Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct LoginAppRequest {
    /// Username.
    pub user_name: String,
    /// Password.
    pub password: String,
    /// Device identifier.
    pub device_id: String,
    /// Application identifier.
    pub app_id: String,
    /// Application verification key.
    pub verify_key: String,
}

impl fmt::Debug for LoginAppRequest {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("LoginAppRequest")
            .field("user_name", &self.user_name)
            .field("password", &REDACTED)
            .field("device_id", &self.device_id)
            .field("app_id", &self.app_id)
            .field("verify_key", &REDACTED)
            .finish()
    }
}

impl Endpoint for LoginAppRequest {
    const OPERATION: Operation = Operation::AuthLoginApp;
    type Response = LoginResponse;
}

gateway_response! {
    manual_debug
    /// Response of the login operations (`LoginResponse`). `Debug` output redacts the token.
    pub struct LoginResponse: LoginErrorCode {
        /// Session JWT; `null` on failure. Valid for 24 hours.
        #[serde(default)]
        pub token: Option<String>,
    }
}

impl fmt::Debug for LoginResponse {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("LoginResponse")
            .field("success", &self.success)
            .field("error_code", &self.error_code)
            .field("error_message", &self.error_message)
            .field("has_business_failure", &self.has_business_failure)
            .field("token", &redact(&self.token))
            .finish()
    }
}

gateway_response! {
    /// Response of `POST /api/Auth/logout` (`LogoutResponse`).
    pub struct LogoutResponse: LogoutErrorCode {}
}

gateway_response! {
    manual_debug
    /// Response of `POST /api/Auth/validate` (`ValidateResponse`). `Debug` output redacts the
    /// token.
    pub struct ValidateResponse: ValidateErrorCode {
        /// Renewed session token.
        #[serde(default)]
        pub new_token: Option<String>,
    }
}

impl fmt::Debug for ValidateResponse {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("ValidateResponse")
            .field("success", &self.success)
            .field("error_code", &self.error_code)
            .field("error_message", &self.error_message)
            .field("has_business_failure", &self.has_business_failure)
            .field("new_token", &redact(&self.new_token))
            .finish()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn debug_output_redacts_secrets() {
        let req = LoginApiKeyRequest::new("trader", "super-secret-key");
        let text = format!("{req:?}");
        assert!(
            text.contains("trader") && !text.contains("super-secret-key"),
            "{text}"
        );

        let resp: LoginResponse = serde_json::from_str(
            r#"{"success":true,"errorCode":0,"errorMessage":null,"token":"jwt-secret"}"#,
        )
        .unwrap();
        assert!(!format!("{resp:?}").contains("jwt-secret"));
        assert!(
            !resp.has_business_failure,
            "absent hasBusinessFailure defaults to false"
        );

        let app = LoginAppRequest {
            user_name: "u".into(),
            password: "pw-secret".into(),
            device_id: "d".into(),
            app_id: "a".into(),
            verify_key: "vk-secret".into(),
        };
        let text = format!("{app:?}");
        assert!(!text.contains("pw-secret") && !text.contains("vk-secret"));
    }
}
