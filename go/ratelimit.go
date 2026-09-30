package topstepx

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RateLimit is a sliding-window limit: at most Requests HTTP attempts in any
// Window.
type RateLimit struct {
	// Requests is the number of attempts allowed per window.
	Requests int
	// Window is the length of the sliding window.
	Window time.Duration
}

// RateLimits holds the limit of each rate bucket.
type RateLimits struct {
	// History applies to History_GetBars.
	History RateLimit
	// General applies to every other operation.
	General RateLimit
}

// DefaultRateLimits returns the documented gateway limits: 50 requests per
// 30 seconds for History_GetBars and 200 requests per 60 seconds for every
// other operation.
func DefaultRateLimits() RateLimits {
	return RateLimits{
		History: RateLimit{Requests: 50, Window: 30 * time.Second},
		General: RateLimit{Requests: 200, Window: 60 * time.Second},
	}
}

func (l RateLimit) validate(name string) error {
	if l.Requests <= 0 || l.Window <= 0 {
		return fmt.Errorf("topstepx: invalid %s rate limit: requests and window must be positive", name)
	}
	return nil
}

// slidingWindow admits at most limit acquisitions in any window. A caller
// that finds the window full waits until the oldest entry expires.
type slidingWindow struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu     sync.Mutex
	stamps []time.Time // acquisition times inside the window, oldest first
}

func newSlidingWindow(l RateLimit, now func() time.Time) *slidingWindow {
	return &slidingWindow{limit: l.Requests, window: l.Window, now: now, stamps: make([]time.Time, 0, l.Requests)}
}

// acquire takes a slot, waiting while the window is full. It returns the
// context's error if ctx ends first.
func (w *slidingWindow) acquire(ctx context.Context) error {
	for {
		wait := w.tryAcquire()
		if wait <= 0 {
			return nil
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// tryAcquire takes a slot and returns 0, or returns how long to wait until
// the oldest entry expires.
func (w *slidingWindow) tryAcquire() time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	cutoff := now.Add(-w.window)
	expired := 0
	for expired < len(w.stamps) && !w.stamps[expired].After(cutoff) {
		expired++
	}
	if expired > 0 {
		w.stamps = append(w.stamps[:0], w.stamps[expired:]...)
	}
	if len(w.stamps) < w.limit {
		w.stamps = append(w.stamps, now)
		return 0
	}
	wait := w.stamps[0].Add(w.window).Sub(now)
	if wait <= 0 {
		wait = time.Millisecond
	}
	return wait
}
