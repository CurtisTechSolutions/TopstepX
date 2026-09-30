//! Client-side sliding-window rate limiting.

use std::collections::VecDeque;
use std::sync::Mutex;
use std::time::Duration;

use tokio::time::Instant;

use crate::operation::RateBucket;

/// A request budget: at most `max_requests` within any `window`.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct RateLimit {
    /// Requests allowed per window. Must be positive.
    pub max_requests: u32,
    /// Window length.
    pub window: Duration,
}

impl RateLimit {
    /// Creates a limit.
    pub const fn new(max_requests: u32, window: Duration) -> Self {
        RateLimit {
            max_requests,
            window,
        }
    }
}

/// Limits for both buckets. The default is the documented gateway limits.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct RateLimits {
    /// Every operation except bar retrieval (documented: 200 requests per 60 s).
    pub general: RateLimit,
    /// `POST /api/History/retrieveBars` (documented: 50 requests per 30 s).
    pub history: RateLimit,
}

impl RateLimits {
    /// The limits published in the gateway documentation.
    pub const DOCUMENTED: RateLimits = RateLimits {
        general: RateLimit::new(200, Duration::from_secs(60)),
        history: RateLimit::new(50, Duration::from_secs(30)),
    };
}

impl Default for RateLimits {
    fn default() -> Self {
        RateLimits::DOCUMENTED
    }
}

#[derive(Debug)]
struct SlidingWindow {
    limit: RateLimit,
    sent: Mutex<VecDeque<Instant>>,
}

impl SlidingWindow {
    fn new(limit: RateLimit) -> Self {
        SlidingWindow {
            limit,
            sent: Mutex::new(VecDeque::with_capacity(limit.max_requests as usize)),
        }
    }

    /// Waits until a request fits in the window, then records it.
    async fn acquire(&self) {
        loop {
            let wait = {
                let mut sent = self
                    .sent
                    .lock()
                    .unwrap_or_else(|poisoned| poisoned.into_inner());
                let now = Instant::now();
                while let Some(&oldest) = sent.front() {
                    if now.duration_since(oldest) >= self.limit.window {
                        sent.pop_front();
                    } else {
                        break;
                    }
                }
                if sent.len() < self.limit.max_requests as usize {
                    sent.push_back(now);
                    return;
                }
                let oldest = *sent.front().expect("window is full, so it is not empty");
                self.limit.window.saturating_sub(now.duration_since(oldest))
            };
            tracing::debug!(?wait, "client-side rate limit reached; waiting");
            tokio::time::sleep(wait).await;
        }
    }
}

/// One sliding window per bucket, shared by every request of a client.
#[derive(Debug)]
pub(crate) struct RateLimiter {
    general: SlidingWindow,
    history: SlidingWindow,
}

impl RateLimiter {
    pub(crate) fn new(limits: RateLimits) -> Self {
        RateLimiter {
            general: SlidingWindow::new(limits.general),
            history: SlidingWindow::new(limits.history),
        }
    }

    pub(crate) async fn acquire(&self, bucket: RateBucket) {
        match bucket {
            RateBucket::General => self.general.acquire().await,
            RateBucket::History => self.history.acquire().await,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test(start_paused = true)]
    async fn waits_for_the_oldest_request_to_leave_the_window() {
        let limiter = RateLimiter::new(RateLimits {
            general: RateLimit::new(3, Duration::from_secs(10)),
            history: RateLimit::new(1, Duration::from_secs(30)),
        });
        let start = Instant::now();
        for _ in 0..3 {
            limiter.acquire(RateBucket::General).await;
        }
        assert_eq!(start.elapsed(), Duration::ZERO);
        limiter.acquire(RateBucket::General).await;
        assert_eq!(start.elapsed(), Duration::from_secs(10));

        // Buckets are independent.
        let before = Instant::now();
        limiter.acquire(RateBucket::History).await;
        assert_eq!(before.elapsed(), Duration::ZERO);
        limiter.acquire(RateBucket::History).await;
        assert_eq!(before.elapsed(), Duration::from_secs(30));
    }

    #[test]
    fn documented_defaults() {
        let limits = RateLimits::default();
        assert_eq!(limits.general, RateLimit::new(200, Duration::from_secs(60)));
        assert_eq!(limits.history, RateLimit::new(50, Duration::from_secs(30)));
    }
}
