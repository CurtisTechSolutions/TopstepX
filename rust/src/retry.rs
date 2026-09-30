//! Retry policy and `Retry-After` parsing.

use std::time::Duration;

use chrono::{DateTime, Utc};
use reqwest::header::{HeaderMap, RETRY_AFTER};

/// How failed requests are retried.
///
/// HTTP 429 is retried for every operation, because it means the request was not processed.
/// HTTP 502, 503 and 504 and transport errors are retried only for read-only operations.
/// Delays follow `Retry-After` when the server sends it, and otherwise grow exponentially
/// from `initial_backoff`, capped at `max_backoff`.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct RetryPolicy {
    /// Retries after the first attempt; `0` disables retries.
    pub max_retries: u32,
    /// Delay before the first retry.
    pub initial_backoff: Duration,
    /// Upper bound for the exponential delay (not applied to `Retry-After`).
    pub max_backoff: Duration,
}

impl Default for RetryPolicy {
    fn default() -> Self {
        RetryPolicy {
            max_retries: 3,
            initial_backoff: Duration::from_secs(1),
            max_backoff: Duration::from_secs(30),
        }
    }
}

impl RetryPolicy {
    /// A policy that never retries.
    pub const fn none() -> Self {
        RetryPolicy {
            max_retries: 0,
            initial_backoff: Duration::ZERO,
            max_backoff: Duration::ZERO,
        }
    }

    /// Exponential delay before retry number `retry` (zero-based).
    pub fn backoff(&self, retry: u32) -> Duration {
        let factor = 1u32.checked_shl(retry).unwrap_or(u32::MAX);
        self.initial_backoff
            .saturating_mul(factor)
            .min(self.max_backoff)
    }
}

/// Parses `Retry-After` as delta-seconds or an HTTP date.
pub(crate) fn retry_after(headers: &HeaderMap) -> Option<Duration> {
    let value = headers.get(RETRY_AFTER)?.to_str().ok()?.trim();
    if let Ok(seconds) = value.parse::<u64>() {
        return Some(Duration::from_secs(seconds));
    }
    if let Ok(seconds) = value.parse::<f64>() {
        if seconds.is_finite() && seconds >= 0.0 {
            return Some(Duration::from_secs_f64(seconds));
        }
    }
    let date = DateTime::parse_from_rfc2822(value)
        .ok()?
        .with_timezone(&Utc);
    Some((date - Utc::now()).to_std().unwrap_or(Duration::ZERO))
}

#[cfg(test)]
mod tests {
    use super::*;
    use reqwest::header::HeaderValue;

    #[test]
    fn exponential_backoff_is_capped() {
        let policy = RetryPolicy::default();
        assert_eq!(policy.backoff(0), Duration::from_secs(1));
        assert_eq!(policy.backoff(1), Duration::from_secs(2));
        assert_eq!(policy.backoff(2), Duration::from_secs(4));
        assert_eq!(policy.backoff(10), Duration::from_secs(30));
        assert_eq!(policy.backoff(64), Duration::from_secs(30));
    }

    #[test]
    fn parses_retry_after() {
        let mut headers = HeaderMap::new();
        assert_eq!(retry_after(&headers), None);
        headers.insert(RETRY_AFTER, HeaderValue::from_static("7"));
        assert_eq!(retry_after(&headers), Some(Duration::from_secs(7)));
        headers.insert(
            RETRY_AFTER,
            HeaderValue::from_static("Wed, 21 Oct 2015 07:28:00 GMT"),
        );
        assert_eq!(retry_after(&headers), Some(Duration::ZERO));
        let future = (Utc::now() + chrono::Duration::seconds(120)).to_rfc2822();
        headers.insert(RETRY_AFTER, HeaderValue::from_str(&future).unwrap());
        let parsed = retry_after(&headers).unwrap();
        assert!(parsed > Duration::from_secs(100) && parsed <= Duration::from_secs(120));
        headers.insert(RETRY_AFTER, HeaderValue::from_static("soon"));
        assert_eq!(retry_after(&headers), None);
    }
}
