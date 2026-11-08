// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// UsageStore persists the cumulative call/cost ledger. A GitHub Actions
// workspace is ephemeral, so the workflow uses a dedicated git state branch;
// local runs use an atomic file store. Both stores serialize updates with a
// lease lock and fail closed when the lock or push cannot be acquired.
type UsageStore interface {
	Load() (*Usage, error)
	Update(func(*Usage) error) error
}

type FileUsageStore struct {
	Path string
	Now  func() time.Time
}

func (store *FileUsageStore) Load() (*Usage, error) {
	return loadUsage(store.Path)
}

func (store *FileUsageStore) Update(update func(*Usage) error) error {
	now := store.Now()
	unlock, err := acquireLock(store.Path+".lock", now)
	if err != nil {
		return err
	}
	defer unlock()
	usage, err := loadUsage(store.Path)
	if err != nil {
		return err
	}
	if err := update(usage); err != nil {
		return err
	}
	return usage.save(store.Path, now)
}

// GitUsageStore keeps usage.json in a dedicated git work tree. A reservation
// is only accepted after the commit is pushed, so a crashed or fresh runner
// cannot start from a reset counter. A push conflict is retried against the
// latest remote state, which re-runs the validation against the new totals.
type GitUsageStore struct {
	Dir     string
	File    string
	Branch  string
	Now     func() time.Time
	Commits int
}

func (store *GitUsageStore) usagePath() string {
	return filepath.Join(store.Dir, filepath.FromSlash(store.File))
}

func (store *GitUsageStore) lockPath() string {
	return filepath.Join(store.Dir, ".dge-usage.lock")
}

func (store *GitUsageStore) git(args ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", store.Dir}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func (store *GitUsageStore) Load() (*Usage, error) {
	return loadUsage(store.usagePath())
}

func (store *GitUsageStore) Update(update func(*Usage) error) error {
	if store.Dir == "" || store.File == "" || store.Branch == "" {
		return errors.New("git usage store needs dir, file and branch")
	}
	if _, err := os.Stat(filepath.Join(store.Dir, ".git")); err != nil {
		return fmt.Errorf("git usage store: %s is not a git work tree: %w", store.Dir, err)
	}
	now := store.Now()
	unlock, err := acquireLock(store.lockPath(), now)
	if err != nil {
		return err
	}
	defer unlock()

	attempts := 3
	if store.Commits > 0 {
		attempts = store.Commits
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if _, err := store.git("fetch", "origin", store.Branch); err != nil {
			return err
		}
		if _, err := store.git("reset", "--hard", "FETCH_HEAD"); err != nil {
			return err
		}
		usage, err := loadUsage(store.usagePath())
		if err != nil {
			return err
		}
		if err := update(usage); err != nil {
			return err
		}
		if err := usage.save(store.usagePath(), now); err != nil {
			return err
		}
		if _, err := store.git("add", "--", store.File); err != nil {
			return err
		}
		message := fmt.Sprintf("dge: usage update (auth=%s calls=%d cost=%.6f)",
			usage.AuthorizationID, usage.Calls, usage.CostUSD)
		if _, err := store.git("commit", "-m", message); err != nil {
			// No change is not an error for settlement; keep the counters.
			if !strings.Contains(err.Error(), "nothing to commit") {
				return err
			}
		}
		if _, err := store.git("push", "origin", "HEAD:"+store.Branch); err == nil {
			return nil
		} else {
			lastErr = err
		}
		// Discard the rejected local commit and retry against the new remote.
		_, _ = store.git("reset", "--hard", "origin/"+store.Branch)
	}
	return fmt.Errorf("usage state push failed after %d attempts: %w", attempts, lastErr)
}

func acquireLock(path string, now time.Time) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 25; attempt++ {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprintf(file, "%d %s\n", os.Getpid(), now.UTC().Format(time.RFC3339))
			file.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, statErr := os.Stat(path); statErr == nil && now.Sub(info.ModTime()) > 5*time.Minute {
			_ = os.Remove(path)
			continue
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, fmt.Errorf("usage store is locked: %s", path)
}

// usageSummary is the non-secret view used in reports.
type UsageSummary struct {
	AuthorizationID  string  `json:"authorization_id"`
	Calls            int     `json:"calls"`
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	CostUSD          float64 `json:"estimated_cost_usd"`
	CostCapEnforced  bool    `json:"cost_cap_enforced"`
	PricingKnown     bool    `json:"pricing_known"`
	RemainingCalls   int     `json:"remaining_calls"`
	RemainingCostUSD float64 `json:"remaining_cost_usd"`
	Note             string  `json:"note,omitempty"`
}

func summarizeUsage(usage *Usage, budget *Budget, provider Provider) UsageSummary {
	summary := UsageSummary{
		AuthorizationID:  usage.AuthorizationID,
		Calls:            usage.Calls,
		InputTokens:      usage.InputTokens,
		OutputTokens:     usage.OutputTokens,
		CostUSD:          usage.CostUSD,
		CostCapEnforced:  usage.CostCapEnforced,
		PricingKnown:     provider.PricingKnown(),
		RemainingCalls:   budget.MaxCalls - usage.Calls,
		RemainingCostUSD: budget.MaxCostUSD - usage.CostUSD,
	}
	if summary.RemainingCalls < 0 {
		summary.RemainingCalls = 0
	}
	if !summary.CostCapEnforced {
		summary.Note = "cost cap NOT enforced: provider pricing is missing or zero, or the owner allowed a run without a cost cap"
		summary.RemainingCostUSD = 0
	}
	return summary
}

var _ = json.MarshalIndent
