// SPDX-License-Identifier: Apache-2.0
package model

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Frozen A.9.3 defaults.
const (
	DefaultDailyCalls      = 100
	DefaultDailyInputChars = 200_000
	DefaultUserConcurrency = 2
	// DefaultGlobalDailyCalls is the deployment-wide Real-mode hard stop. It
	// is deliberately small: an operator must choose to raise it, and the
	// approved real-AI plan must state the exact budget. Fixture mode has no
	// cost and uses 0 (unlimited) unless the operator configures a cap.
	DefaultGlobalDailyCalls = 200
	// MaxModelInputChars matches the adapter's 64 KiB input ceiling. The
	// quota works in Unicode code points; the adapter enforces bytes.
	MaxModelInputChars = 64 * 1024
)

var (
	// ErrBudgetExceeded maps to 429 budget_exceeded.
	ErrBudgetExceeded = errors.New("model daily budget exceeded")
	// ErrGlobalBudgetExceeded maps to 429 budget_exceeded and is kept
	// distinct so audit metadata can identify the deployment-wide stop.
	ErrGlobalBudgetExceeded = errors.New("model global daily budget exceeded")
	// ErrConcurrencyLimit maps to 429 rate_limited.
	ErrConcurrencyLimit = errors.New("model concurrency limit exceeded")
	// ErrInputTooLarge maps to 413 payload_too_large before any provider call.
	ErrInputTooLarge = errors.New("model input exceeds the per-call limit")
	// ErrModelDisabled is the operator kill switch. It maps to 502
	// model_unavailable and no provider call is made.
	ErrModelDisabled = errors.New("model calls are disabled by the operator")
)

// QuotaConfig is the validated model budget. Zero global values mean
// "unlimited" and must only be used in Fixture mode.
type QuotaConfig struct {
	Mode             string
	DailyCalls       int
	DailyInputChars  int
	Concurrency      int
	MaxInputChars    int
	GlobalDailyCalls int
	GlobalDailyChars int
	Disabled         bool
}

// DefaultQuotaConfig returns the frozen defaults for a mode.
func DefaultQuotaConfig(mode string) QuotaConfig {
	config := QuotaConfig{
		Mode:            mode,
		DailyCalls:      DefaultDailyCalls,
		DailyInputChars: DefaultDailyInputChars,
		Concurrency:     DefaultUserConcurrency,
		MaxInputChars:   MaxModelInputChars,
	}
	if mode == "real" {
		config.GlobalDailyCalls = DefaultGlobalDailyCalls
	}
	return config
}

type quotaState struct {
	day      string
	calls    int
	chars    int
	inFlight int
}

type quotaGlobal struct {
	day      string
	calls    int
	chars    int
	inFlight int
}

// Quota is a single-instance reservation gate for model calls. It is seeded
// from model_invocations on first use in a UTC day and then tracks
// reservations in memory. It is not a permission cache and never contains
// prompt content.
type Quota struct {
	mu     sync.Mutex
	db     Querier
	config QuotaConfig
	users  map[uuid.UUID]*quotaState
	global quotaGlobal
	now    func() time.Time
}

// NewQuota assembles the usage gate. Non-positive per-user bounds use the
// frozen defaults; a non-positive global bound means unlimited.
func NewQuota(db Querier, config QuotaConfig) *Quota {
	if config.DailyCalls <= 0 {
		config.DailyCalls = DefaultDailyCalls
	}
	if config.DailyInputChars <= 0 {
		config.DailyInputChars = DefaultDailyInputChars
	}
	if config.Concurrency <= 0 {
		config.Concurrency = DefaultUserConcurrency
	}
	if config.MaxInputChars <= 0 || config.MaxInputChars > MaxModelInputChars {
		config.MaxInputChars = MaxModelInputChars
	}
	if config.Mode == "" {
		config.Mode = "fixture"
	}
	return &Quota{
		db: db, config: config, users: map[uuid.UUID]*quotaState{}, now: time.Now,
	}
}

// Config returns a copy of the effective configuration for reporting.
func (q *Quota) Config() QuotaConfig { return q.config }

// Reserve atomically checks the kill switch, per-call input bound,
// concurrency, user daily calls/characters, and the deployment-wide daily
// calls/characters, then counts the attempt. The returned release function
// must be called exactly once with the final input character count (normally
// the reserved count). A failure still consumes one call, matching provider
// billing.
func (q *Quota) Reserve(ctx context.Context, userID uuid.UUID, inputChars int) (func(actualInputChars int), error) {
	if q.config.Disabled {
		return nil, ErrModelDisabled
	}
	if inputChars < 0 {
		inputChars = 0
	}
	if inputChars > q.config.MaxInputChars {
		return nil, ErrInputTooLarge
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	day := q.now().UTC().Format("2006-01-02")
	// A day rollover reseeds from the database, but never while a call is
	// in flight: that call is already counted in memory and not yet visible in
	// model_invocations. Deferring the reseed is conservative and prevents a
	// midnight reservation from escaping the new day's budget.
	if q.global.day != day && q.global.inFlight == 0 {
		calls, chars, err := NewRecorder().GlobalDayUsage(ctx, q.db, q.config.Mode)
		if err != nil {
			return nil, err
		}
		q.global = quotaGlobal{day: day, calls: calls, chars: chars}
	}
	state := q.users[userID]
	if state == nil || (state.day != day && state.inFlight == 0) {
		calls, chars, err := NewRecorder().DayUsage(ctx, q.db, userID)
		if err != nil {
			return nil, err
		}
		if state == nil {
			state = &quotaState{}
			q.users[userID] = state
		}
		state.day = day
		state.calls = calls
		state.chars = chars
	}
	if state.inFlight >= q.config.Concurrency {
		return nil, ErrConcurrencyLimit
	}
	if state.calls+1 > q.config.DailyCalls || state.chars+inputChars > q.config.DailyInputChars {
		return nil, ErrBudgetExceeded
	}
	if q.config.GlobalDailyCalls > 0 && q.global.calls+1 > q.config.GlobalDailyCalls {
		return nil, ErrGlobalBudgetExceeded
	}
	if q.config.GlobalDailyChars > 0 && q.global.chars+inputChars > q.config.GlobalDailyChars {
		return nil, ErrGlobalBudgetExceeded
	}
	state.calls++
	state.chars += inputChars
	state.inFlight++
	q.global.calls++
	q.global.chars += inputChars
	q.global.inFlight++
	released := false
	return func(actualInputChars int) {
		q.mu.Lock()
		defer q.mu.Unlock()
		if released {
			return
		}
		released = true
		if state.inFlight > 0 {
			state.inFlight--
		}
		if q.global.inFlight > 0 {
			q.global.inFlight--
		}
		if actualInputChars >= 0 && actualInputChars != inputChars {
			delta := actualInputChars - inputChars
			state.chars += delta
			q.global.chars += delta
		}
	}, nil
}

// Snapshot returns the tracked user day totals for display and tests.
func (q *Quota) Snapshot(userID uuid.UUID) (calls, chars, inFlight int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	state := q.users[userID]
	if state == nil {
		return 0, 0, 0
	}
	return state.calls, state.chars, state.inFlight
}

// GlobalSnapshot returns the deployment-wide tracked day totals.
func (q *Quota) GlobalSnapshot() (calls, chars int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.global.calls, q.global.chars
}
