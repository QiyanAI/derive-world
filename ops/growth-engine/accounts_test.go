// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAccountRegistryAndKits(t *testing.T) {
	platforms, err := loadPlatforms("state/platforms.json")
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := loadAccounts("state/accounts.json")
	if err != nil {
		t.Fatal(err)
	}
	facts, err := loadFacts("state/facts.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(platforms.Platforms) != 5 || len(accounts.Accounts) != 5 {
		t.Fatalf("platforms=%d accounts=%d", len(platforms.Platforms), len(accounts.Accounts))
	}
	for _, platform := range platforms.Platforms {
		kit, err := buildAccountKit(platform, facts)
		if err != nil {
			t.Fatalf("%s: %v", platform.ID, err)
		}
		for _, want := range []string{"Prepared, not registered", platform.Name, "Owner-only steps", "Facts used"} {
			if !strings.Contains(kit, want) {
				t.Fatalf("%s kit missing %q", platform.ID, want)
			}
		}
		if strings.Contains(kit, "dw_pat_") || strings.Contains(kit, "sk-") {
			t.Fatalf("%s kit contains secret-shaped text", platform.ID)
		}
	}
}

func TestAccountPreflightRequiresRecordedOwnerFacts(t *testing.T) {
	file, err := loadAccounts("state/accounts.json")
	if err != nil {
		t.Fatal(err)
	}
	account, err := findAccount(file, "github")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	if err := AccountPreflight(account, "publish", now); err == nil {
		t.Fatal("an owner-action-required account passed preflight")
	}
	account.Status = "active"
	account.Security.TwoFactor = true
	account.Security.RecoveryEmail = true
	account.Authorization = Authorization{
		Scope: []string{"publish"}, GrantedBy: "owner", GrantedAt: "2026-11-08", ExpiresAt: "2099-12-31",
	}
	if err := AccountPreflight(account, "publish", now); err != nil {
		t.Fatalf("active account failed preflight: %v", err)
	}
	if err := AccountPreflight(account, "publish", time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("expired authorization passed preflight")
	}
}

func TestAccountRecordWritesAuditWithoutSecrets(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "accounts.json")
	file, err := loadAccounts("state/accounts.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFile(statePath, mustJSON(t, file), false); err != nil {
		t.Fatal(err)
	}
	auditPath := filepath.Join(dir, "audit.jsonl")
	var stdout, stderr bytes.Buffer
	err = runAccounts([]string{
		"record", "--accounts", statePath, "--audit", auditPath,
		"--channel", "github", "--status", "active", "--scope", "publish",
		"--authorized-by", "owner", "--authorized-at", "2026-11-08",
		"--two-factor", "--recovery-email", "--token-scope", "fine-grained: contents read",
		"--date", "2026-11-08", "--write",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	updated, err := loadAccounts(statePath)
	if err != nil {
		t.Fatal(err)
	}
	account, _ := findAccount(updated, "github")
	if account.Status != "active" || !account.Security.TwoFactor || len(account.Authorization.Scope) != 1 {
		t.Fatalf("account = %+v", account)
	}
	rawAudit, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawAudit), "password") || strings.Contains(string(rawAudit), "token=") {
		t.Fatalf("audit contains secret-shaped text: %s", rawAudit)
	}
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(rawAudit), &entry); err != nil {
		t.Fatalf("audit is not JSON: %v", err)
	}
	if entry["action"] != "account.record" || entry["channel"] != "github" {
		t.Fatalf("audit entry = %+v", entry)
	}
}

func TestOwnerTodoListsOnlyOwnerActions(t *testing.T) {
	accounts, err := loadAccounts("state/accounts.json")
	if err != nil {
		t.Fatal(err)
	}
	platforms, err := loadPlatforms("state/platforms.json")
	if err != nil {
		t.Fatal(err)
	}
	todo := renderOwnerTodo(accounts, platforms)
	for _, want := range []string{"enable two-factor authentication", "Hacker News", "never asks for a password"} {
		if !strings.Contains(todo, want) {
			t.Fatalf("owner todo missing %q", want)
		}
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(encoded, '\n')
}
