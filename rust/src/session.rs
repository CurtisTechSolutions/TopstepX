//! Credentials and token state.

use std::fmt;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, RwLock};

use base64::Engine;
use chrono::{DateTime, Duration as ChronoDuration, Utc};

/// Documented token lifetime, used when a token's expiry cannot be read.
const DEFAULT_TOKEN_LIFETIME_HOURS: i64 = 24;

/// Platform username and API key. `Debug` output redacts the key.
#[derive(Clone)]
pub(crate) struct Credentials {
    pub(crate) username: String,
    pub(crate) api_key: String,
}

impl fmt::Debug for Credentials {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("Credentials")
            .field("username", &self.username)
            .field("api_key", &"<redacted>")
            .finish()
    }
}

/// A stored session token.
#[derive(Clone)]
pub(crate) struct TokenState {
    pub(crate) token: Arc<str>,
    pub(crate) expires_at: DateTime<Utc>,
    /// Increases every time a token is stored, so concurrent 401 handlers can tell whether
    /// someone else already replaced the token that failed.
    pub(crate) generation: u64,
    /// After a failed renewal that kept this token, no renewal is attempted before this
    /// time. A newly stored token starts without a cooldown.
    pub(crate) renewal_blocked_until: Option<DateTime<Utc>>,
}

impl fmt::Debug for TokenState {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("TokenState")
            .field("token", &"<redacted>")
            .field("expires_at", &self.expires_at)
            .field("generation", &self.generation)
            .finish()
    }
}

/// Where a token stands relative to its expiry and the refresh margin.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum Freshness {
    Fresh,
    NeedsRefresh,
    Expired,
}

impl TokenState {
    pub(crate) fn freshness(&self, now: DateTime<Utc>, margin: std::time::Duration) -> Freshness {
        if now >= self.expires_at {
            return Freshness::Expired;
        }
        let margin = ChronoDuration::from_std(margin).unwrap_or(ChronoDuration::MAX);
        match self.expires_at.checked_sub_signed(margin) {
            Some(refresh_at) if now < refresh_at => Freshness::Fresh,
            // Inside the margin but cooling down after a failed renewal: use as-is.
            _ if self.renewal_blocked_until.is_some_and(|until| now < until) => Freshness::Fresh,
            _ => Freshness::NeedsRefresh,
        }
    }
}

/// Token storage plus the single-flight lock for logins and refreshes.
#[derive(Debug, Default)]
pub(crate) struct Session {
    state: RwLock<Option<TokenState>>,
    generation: AtomicU64,
    /// Held while logging in or refreshing, so concurrent callers wait for one attempt
    /// instead of stampeding.
    pub(crate) refresh: tokio::sync::Mutex<()>,
}

impl Session {
    pub(crate) fn current(&self) -> Option<TokenState> {
        self.state
            .read()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .clone()
    }

    pub(crate) fn store(&self, token: String) -> TokenState {
        let expires_at = jwt_expiry(&token)
            .unwrap_or_else(|| Utc::now() + ChronoDuration::hours(DEFAULT_TOKEN_LIFETIME_HOURS));
        let state = TokenState {
            token: Arc::from(token),
            expires_at,
            generation: self.generation.fetch_add(1, Ordering::SeqCst) + 1,
            renewal_blocked_until: None,
        };
        *self
            .state
            .write()
            .unwrap_or_else(|poisoned| poisoned.into_inner()) = Some(state.clone());
        state
    }

    pub(crate) fn clear(&self) {
        *self
            .state
            .write()
            .unwrap_or_else(|poisoned| poisoned.into_inner()) = None;
    }

    /// Blocks renewal of the token stored as `generation` until `until`. Does nothing when
    /// that token was already replaced.
    pub(crate) fn pause_renewal(&self, generation: u64, until: DateTime<Utc>) {
        let mut state = self
            .state
            .write()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if let Some(current) = state.as_mut().filter(|s| s.generation == generation) {
            current.renewal_blocked_until = Some(until);
        }
    }

    /// Clears the stored token only if it is still `token`; returns whether it did.
    pub(crate) fn clear_if(&self, token: &str) -> bool {
        let mut state = self
            .state
            .write()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if state
            .as_ref()
            .is_some_and(|current| &*current.token == token)
        {
            *state = None;
            true
        } else {
            false
        }
    }
}

/// Reads the `exp` claim of a JWT without verifying its signature.
pub(crate) fn jwt_expiry(token: &str) -> Option<DateTime<Utc>> {
    use base64::engine::general_purpose::{
        STANDARD_NO_PAD_INDIFFERENT, URL_SAFE_NO_PAD_INDIFFERENT,
    };
    let mut parts = token.split('.');
    let (_header, payload, _signature) = (parts.next()?, parts.next()?, parts.next()?);
    let bytes = URL_SAFE_NO_PAD_INDIFFERENT
        .decode(payload)
        .or_else(|_| STANDARD_NO_PAD_INDIFFERENT.decode(payload))
        .ok()?;
    let claims: serde_json::Value = serde_json::from_slice(&bytes).ok()?;
    let exp = claims.get("exp")?;
    let seconds = exp.as_i64().or_else(|| exp.as_f64().map(|f| f as i64))?;
    DateTime::from_timestamp(seconds, 0)
}

#[cfg(test)]
pub(crate) fn make_jwt(exp: i64) -> String {
    let encode = |value: serde_json::Value| {
        base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(serde_json::to_vec(&value).unwrap())
    };
    format!(
        "{}.{}.signature",
        encode(serde_json::json!({"alg": "HS256", "typ": "JWT"})),
        encode(serde_json::json!({"sub": "user", "exp": exp}))
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;

    #[test]
    fn reads_exp_claim() {
        let token = make_jwt(1_900_000_000);
        assert_eq!(
            jwt_expiry(&token),
            DateTime::from_timestamp(1_900_000_000, 0)
        );
        assert_eq!(jwt_expiry("opaque-token"), None);
        assert_eq!(jwt_expiry("a.b.c"), None);
    }

    #[test]
    fn falls_back_to_24_hours() {
        let session = Session::default();
        let state = session.store("opaque".into());
        let lifetime = state.expires_at - Utc::now();
        assert!(lifetime > ChronoDuration::hours(23) && lifetime <= ChronoDuration::hours(24));
    }

    #[test]
    fn renewal_cooldown_is_per_token() {
        let session = Session::default();
        let now = Utc::now();
        let margin = Duration::from_secs(3600);
        let soon = session.store(make_jwt((now + ChronoDuration::minutes(10)).timestamp()));
        session.pause_renewal(soon.generation + 1, now + ChronoDuration::seconds(60));
        assert_eq!(
            session.current().unwrap().freshness(now, margin),
            Freshness::NeedsRefresh,
            "stale generation is ignored"
        );

        session.pause_renewal(soon.generation, now + ChronoDuration::seconds(60));
        let paused = session.current().unwrap();
        assert_eq!(paused.freshness(now, margin), Freshness::Fresh);
        assert_eq!(
            paused.freshness(now + ChronoDuration::seconds(59), margin),
            Freshness::Fresh
        );
        assert_eq!(
            paused.freshness(now + ChronoDuration::seconds(60), margin),
            Freshness::NeedsRefresh
        );
        assert_eq!(
            paused.freshness(now + ChronoDuration::minutes(11), margin),
            Freshness::Expired
        );

        let replaced = session.store(make_jwt((now + ChronoDuration::minutes(10)).timestamp()));
        assert_eq!(
            replaced.renewal_blocked_until, None,
            "a new token starts without a cooldown"
        );
        assert_eq!(replaced.freshness(now, margin), Freshness::NeedsRefresh);
    }

    #[test]
    fn freshness_boundaries() {
        let session = Session::default();
        let now = Utc::now();
        let margin = Duration::from_secs(3600);
        let fresh = session.store(make_jwt((now + ChronoDuration::hours(5)).timestamp()));
        assert_eq!(fresh.freshness(now, margin), Freshness::Fresh);
        let soon = session.store(make_jwt((now + ChronoDuration::minutes(10)).timestamp()));
        assert_eq!(soon.freshness(now, margin), Freshness::NeedsRefresh);
        let past = session.store(make_jwt((now - ChronoDuration::minutes(1)).timestamp()));
        assert_eq!(past.freshness(now, margin), Freshness::Expired);
        assert!(past.generation > soon.generation && soon.generation > fresh.generation);
        assert!(!format!("{past:?}").contains(&*past.token));
    }
}
