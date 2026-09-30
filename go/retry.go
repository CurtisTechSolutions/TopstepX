package topstepx

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// backoffDelay returns the delay before retry number n (0-based): the initial
// backoff doubled n times, capped at the maximum, without jitter.
func backoffDelay(initial, maxDelay time.Duration, n int) time.Duration {
	d := initial
	for i := 0; i < n; i++ {
		if d >= maxDelay || d > math.MaxInt64/2 {
			return maxDelay
		}
		d *= 2
	}
	if d > maxDelay {
		return maxDelay
	}
	return d
}

// parseRetryAfter reads a Retry-After header in delta-seconds or HTTP-date
// form. A date in the past yields zero.
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		if secs < 0 {
			return 0, false
		}
		if secs > int64(math.MaxInt64/time.Second) {
			return time.Duration(math.MaxInt64), true
		}
		return time.Duration(secs) * time.Second, true
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && !math.IsInf(f, 0) && !math.IsNaN(f) {
		if f > float64(math.MaxInt64/time.Second) {
			return time.Duration(math.MaxInt64), true
		}
		return time.Duration(f * float64(time.Second)), true
	}
	if t, err := http.ParseTime(v); err == nil {
		d := t.Sub(now)
		if d < 0 {
			d = 0
		}
		return d, true
	}
	return 0, false
}

// sleepContext waits for d or until ctx ends.
func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
