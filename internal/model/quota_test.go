// SPDX-License-Identifier: Apache-2.0
package model

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// quotaFakeDB returns independent user and global day totals. It lets the
// unit tests prove restart seeding without PostgreSQL.
type quotaFakeDB struct {
	userCalls, userChars     int
	globalCalls, globalChars int
	err                      error
}

func (f quotaFakeDB) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if strings.Contains(sql, "WHERE provider = $1") {
		return quotaFakeRow{calls: f.globalCalls, chars: f.globalChars, err: f.err}
	}
	return quotaFakeRow{calls: f.userCalls, chars: f.userChars, err: f.err}
}

type quotaFakeRow struct {
	calls, chars int
	err          error
}

func (r quotaFakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 2 {
		return errors.New("expected two scan destinations")
	}
	*(dest[0].(*int)) = r.calls
	*(dest[1].(*int)) = r.chars
	return nil
}

func testUser() uuid.UUID {
	return uuid.MustParse("11111111-1111-7111-8111-111111111111")
}

func TestQuotaPerUserDailyCalls(t *testing.T) {
	quota := NewQuota(quotaFakeDB{}, QuotaConfig{
		Mode: "fixture", DailyCalls: 2, DailyInputChars: 1_000_000, Concurrency: 4,
	})
	user := testUser()
	for index := 0; index < 2; index++ {
		release, err := quota.Reserve(context.Background(), user, 10)
		if err != nil {
			t.Fatalf("reserve %d: %v", index, err)
		}
		release(10)
	}
	if _, err := quota.Reserve(context.Background(), user, 10); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("third reserve error = %v, want ErrBudgetExceeded", err)
	}
}

func TestQuotaGlobalDailyCalls(t *testing.T) {
	quota := NewQuota(quotaFakeDB{}, QuotaConfig{
		Mode: "real", DailyCalls: 100, DailyInputChars: 1_000_000,
		Concurrency: 4, GlobalDailyCalls: 2,
	})
	first := uuid.MustParse("11111111-1111-7111-8111-111111111111")
	second := uuid.MustParse("22222222-2222-7222-8222-222222222222")
	release, err := quota.Reserve(context.Background(), first, 10)
	if err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	release(10)
	release, err = quota.Reserve(context.Background(), second, 10)
	if err != nil {
		t.Fatalf("second reserve: %v", err)
	}
	release(10)
	if _, err := quota.Reserve(context.Background(), first, 10); !errors.Is(err, ErrGlobalBudgetExceeded) {
		t.Fatalf("global reserve error = %v, want ErrGlobalBudgetExceeded", err)
	}
	calls, _ := quota.GlobalSnapshot()
	if calls != 2 {
		t.Fatalf("global calls = %d, want 2", calls)
	}
}

func TestQuotaSeedsFromDatabaseAfterRestart(t *testing.T) {
	db := quotaFakeDB{userCalls: 1, userChars: 40, globalCalls: 1, globalChars: 40}
	quota := NewQuota(db, QuotaConfig{
		Mode: "real", DailyCalls: 2, DailyInputChars: 1000,
		Concurrency: 2, GlobalDailyCalls: 2,
	})
	release, err := quota.Reserve(context.Background(), testUser(), 10)
	if err != nil {
		t.Fatalf("reserve after restart: %v", err)
	}
	release(10)
	if _, err := quota.Reserve(context.Background(), testUser(), 10); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("second reserve after restart = %v, want ErrBudgetExceeded", err)
	}
}

func TestQuotaGlobalSeedsFromDatabaseAfterRestart(t *testing.T) {
	db := quotaFakeDB{globalCalls: 2, globalChars: 20}
	quota := NewQuota(db, QuotaConfig{
		Mode: "real", DailyCalls: 100, DailyInputChars: 1000,
		Concurrency: 2, GlobalDailyCalls: 2,
	})
	if _, err := quota.Reserve(context.Background(), testUser(), 10); !errors.Is(err, ErrGlobalBudgetExceeded) {
		t.Fatalf("reserve = %v, want ErrGlobalBudgetExceeded", err)
	}
}

func TestQuotaConcurrencyAndRelease(t *testing.T) {
	quota := NewQuota(quotaFakeDB{}, QuotaConfig{
		Mode: "fixture", DailyCalls: 10, DailyInputChars: 1000, Concurrency: 1,
	})
	release, err := quota.Reserve(context.Background(), testUser(), 10)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, err := quota.Reserve(context.Background(), testUser(), 10); !errors.Is(err, ErrConcurrencyLimit) {
		t.Fatalf("concurrent reserve = %v, want ErrConcurrencyLimit", err)
	}
	release(10)
	if _, err := quota.Reserve(context.Background(), testUser(), 10); err != nil {
		t.Fatalf("reserve after release: %v", err)
	}
}

func TestQuotaInputTooLarge(t *testing.T) {
	quota := NewQuota(quotaFakeDB{}, QuotaConfig{
		Mode: "real", DailyCalls: 10, DailyInputChars: 1_000_000,
		Concurrency: 2, MaxInputChars: 100,
	})
	if _, err := quota.Reserve(context.Background(), testUser(), 101); !errors.Is(err, ErrInputTooLarge) {
		t.Fatalf("oversized reserve = %v, want ErrInputTooLarge", err)
	}
}

func TestQuotaKillSwitch(t *testing.T) {
	quota := NewQuota(quotaFakeDB{}, QuotaConfig{
		Mode: "real", DailyCalls: 10, DailyInputChars: 1000,
		Concurrency: 2, Disabled: true,
	})
	if _, err := quota.Reserve(context.Background(), testUser(), 10); !errors.Is(err, ErrModelDisabled) {
		t.Fatalf("disabled reserve = %v, want ErrModelDisabled", err)
	}
	calls, chars := quota.GlobalSnapshot()
	if calls != 0 || chars != 0 {
		t.Fatalf("disabled quota recorded usage: calls=%d chars=%d", calls, chars)
	}
}

func TestQuotaReleaseReconcilesCharacters(t *testing.T) {
	quota := NewQuota(quotaFakeDB{}, QuotaConfig{
		Mode: "fixture", DailyCalls: 10, DailyInputChars: 1000, Concurrency: 2,
	})
	release, err := quota.Reserve(context.Background(), testUser(), 10)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	release(7)
	_, chars, _ := quota.Snapshot(testUser())
	if chars != 7 {
		t.Fatalf("user chars = %d, want 7", chars)
	}
	_, globalChars := quota.GlobalSnapshot()
	if globalChars != 7 {
		t.Fatalf("global chars = %d, want 7", globalChars)
	}
}

func TestQuotaSeedingFailureFailsClosed(t *testing.T) {
	quota := NewQuota(quotaFakeDB{err: errors.New("db unavailable")}, QuotaConfig{
		Mode: "real", DailyCalls: 10, DailyInputChars: 1000, Concurrency: 2,
	})
	if _, err := quota.Reserve(context.Background(), testUser(), 10); err == nil {
		t.Fatal("reserve succeeded despite a usage-seeding failure")
	}
}

func TestQuotaDayRolloverDefersReseedWhileInFlight(t *testing.T) {
	quota := NewQuota(quotaFakeDB{}, QuotaConfig{
		Mode: "fixture", DailyCalls: 1, DailyInputChars: 1000,
		Concurrency: 2, GlobalDailyCalls: 1,
	})
	current := time.Date(2026, 10, 24, 23, 59, 0, 0, time.UTC)
	quota.now = func() time.Time { return current }
	release, err := quota.Reserve(context.Background(), testUser(), 10)
	if err != nil {
		t.Fatalf("first reserve: %v", err)
	}

	// Midnight passes while the first call is still in flight. The old-day
	// reservation must remain counted; the new day is not reseeded yet.
	current = time.Date(2026, 10, 25, 0, 1, 0, 0, time.UTC)
	if _, err := quota.Reserve(context.Background(), testUser(), 10); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("reserve after midnight with in-flight call = %v, want ErrBudgetExceeded", err)
	}
	release(10)

	// Once the in-flight call finishes, the next reservation reseeds the new
	// day from the database (zero usage) and is allowed.
	release2, err := quota.Reserve(context.Background(), testUser(), 10)
	if err != nil {
		t.Fatalf("reserve after in-flight completion: %v", err)
	}
	release2(10)
}

func TestQuotaPerUserDailyCharacters(t *testing.T) {
	quota := NewQuota(quotaFakeDB{}, QuotaConfig{
		Mode: "fixture", DailyCalls: 100, DailyInputChars: 15, Concurrency: 4,
	})
	user := testUser()
	release, err := quota.Reserve(context.Background(), user, 10)
	if err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	release(10)
	if _, err := quota.Reserve(context.Background(), user, 10); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("second reserve error = %v, want ErrBudgetExceeded", err)
	}
}

func TestQuotaGlobalDailyCharacters(t *testing.T) {
	quota := NewQuota(quotaFakeDB{}, QuotaConfig{
		Mode: "real", DailyCalls: 100, DailyInputChars: 1000,
		Concurrency: 4, GlobalDailyCalls: 10, GlobalDailyChars: 15,
	})
	first := uuid.MustParse("11111111-1111-7111-8111-111111111111")
	second := uuid.MustParse("22222222-2222-7222-8222-222222222222")
	release, err := quota.Reserve(context.Background(), first, 10)
	if err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	release(10)
	if _, err := quota.Reserve(context.Background(), second, 10); !errors.Is(err, ErrGlobalBudgetExceeded) {
		t.Fatalf("global character reserve error = %v, want ErrGlobalBudgetExceeded", err)
	}
}

func TestQuotaCharacterBudgetsSeedFromDatabaseAfterRestart(t *testing.T) {
	db := quotaFakeDB{userChars: 10, globalChars: 10}
	quota := NewQuota(db, QuotaConfig{
		Mode: "real", DailyCalls: 100, DailyInputChars: 15,
		Concurrency: 2, GlobalDailyCalls: 10, GlobalDailyChars: 15,
	})
	if _, err := quota.Reserve(context.Background(), testUser(), 10); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("per-user character reserve error = %v, want ErrBudgetExceeded", err)
	}
}

func TestQuotaGlobalCharactersSeedFromDatabaseAfterRestart(t *testing.T) {
	db := quotaFakeDB{globalChars: 10}
	quota := NewQuota(db, QuotaConfig{
		Mode: "real", DailyCalls: 100, DailyInputChars: 1000,
		Concurrency: 2, GlobalDailyCalls: 10, GlobalDailyChars: 15,
	})
	if _, err := quota.Reserve(context.Background(), testUser(), 10); !errors.Is(err, ErrGlobalBudgetExceeded) {
		t.Fatalf("global character reserve error = %v, want ErrGlobalBudgetExceeded", err)
	}
}
