// SPDX-License-Identifier: Apache-2.0
package httpx

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// Limiter is a single-instance, in-process token bucket keyed by an opaque
// string. It is not shared across processes (D03-14, D03-31).
type Limiter struct {
	mu          sync.Mutex
	entries     map[string]*limiterEntry
	limit       rate.Limit
	burst       int
	window      time.Duration
	lastCleanup time.Time
}

// NewLimiter creates a limiter with the given number of events per window.
func NewLimiter(events int, window time.Duration) *Limiter {
	if events < 1 {
		events = 1
	}
	if window <= 0 {
		window = time.Minute
	}
	return &Limiter{
		entries:     make(map[string]*limiterEntry),
		limit:       rate.Every(window / time.Duration(events)),
		burst:       events,
		window:      window,
		lastCleanup: time.Now(),
	}
}

// NewLimiterWithBurst creates a limiter with an independent burst capacity.
// D05-19 freezes the API bucket at 120/minute with burst 60.
func NewLimiterWithBurst(events int, window time.Duration, burst int) *Limiter {
	limiter := NewLimiter(events, window)
	if burst > 0 {
		limiter.burst = burst
	}
	return limiter
}

// Allow consumes one token. It returns the remaining tokens, a reset hint, and
// whether the request is allowed.
func (l *Limiter) Allow(key string) (int, time.Time, bool) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	l.cleanupLocked(now)
	entry, ok := l.entries[key]
	if !ok {
		entry = &limiterEntry{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.entries[key] = entry
	}
	entry.lastSeen = now

	reservation := entry.limiter.ReserveN(now, 1)
	if !reservation.OK() {
		return 0, now.Add(l.window), false
	}
	if delay := reservation.DelayFrom(now); delay > 0 {
		reservation.CancelAt(now)
		return 0, now.Add(delay), false
	}
	return int(entry.limiter.TokensAt(now)), now.Add(l.window), true
}

func (l *Limiter) cleanupLocked(now time.Time) {
	if now.Sub(l.lastCleanup) < 10*time.Minute {
		return
	}
	maxIdle := 3 * l.window
	for key, entry := range l.entries {
		if now.Sub(entry.lastSeen) > maxIdle {
			delete(l.entries, key)
		}
	}
	l.lastCleanup = now
}

// Len reports the number of tracked keys (used by tests).
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}
