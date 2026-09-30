use serde::{Deserialize, Serialize};

/// RFC 7807 problem details (`ProblemDetails`), returned with HTTP 400 by
/// `/api/Order/v2/query`.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
#[non_exhaustive]
pub struct ProblemDetails {
    /// URI identifying the problem type (`type`).
    #[serde(rename = "type", default, skip_serializing_if = "Option::is_none")]
    pub problem_type: Option<String>,
    /// Short summary.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub title: Option<String>,
    /// HTTP status code.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub status: Option<i32>,
    /// Occurrence-specific explanation.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub detail: Option<String>,
    /// URI identifying the occurrence.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub instance: Option<String>,
    /// Extension members, such as `traceId` or `errors`.
    #[serde(flatten)]
    pub extensions: serde_json::Map<String, serde_json::Value>,
}
