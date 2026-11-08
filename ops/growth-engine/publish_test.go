// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func publishFixture(t *testing.T) (draftPath, accountsPath, ledgerPath, auditPath, hash string) {
	t.Helper()
	facts, err := loadFacts("state/facts.json")
	if err != nil {
		t.Fatal(err)
	}
	channels, err := loadChannels("state/channels.json")
	if err != nil {
		t.Fatal(err)
	}
	github, _ := findChannel(channels, "github")
	draft, err := GenerateDraft("technical-intro", facts, github)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	draftPath = filepath.Join(dir, "draft.md")
	if err := os.WriteFile(draftPath, []byte(draft), 0o644); err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareForPublication(draft)
	if err != nil {
		t.Fatal(err)
	}
	title, body := splitTitleAndBody(prepared)
	hash = contentHash(title, body)
	accounts, err := loadAccounts("state/accounts.json")
	if err != nil {
		t.Fatal(err)
	}
	devto, _ := findAccount(accounts, "devto")
	devto.Status = "active"
	devto.Security = Security{TwoFactor: true, RecoveryEmail: true}
	devto.Authorization = Authorization{Scope: []string{"publish"}, GrantedBy: "owner", GrantedAt: "2026-11-08", ExpiresAt: "2099-12-31"}
	githubAccount, _ := findAccount(accounts, "github")
	githubAccount.Status = "active"
	githubAccount.Security = Security{TwoFactor: true, RecoveryEmail: true}
	githubAccount.Authorization = Authorization{Scope: []string{"publish"}, GrantedBy: "owner", GrantedAt: "2026-11-08", ExpiresAt: "2099-12-31"}
	accountsPath = filepath.Join(dir, "accounts.json")
	if err := writeFile(accountsPath, mustJSON(t, accounts), false); err != nil {
		t.Fatal(err)
	}
	return draftPath, accountsPath, filepath.Join(dir, "published.json"), filepath.Join(dir, "audit.jsonl"), hash
}

func approvalFor(t *testing.T, dir, channel, hash string) string {
	t.Helper()
	return writeJSONTemp(t, dir, "approval-"+channel+".json", Approval{
		SchemaVersion: 1,
		Channel:       channel,
		Action:        "publish",
		ContentSHA256: hash,
		ApprovedBy:    "owner",
		ApprovedAt:    "2026-11-08",
		ExpiresAt:     "2099-12-31",
	})
}

func TestPublishDryRunSendsNothing(t *testing.T) {
	draft, accounts, ledger, audit, _ := publishFixture(t)
	var stdout, stderr bytes.Buffer
	err := runPublish([]string{
		"--channel", "devto", "--file", draft, "--accounts", accounts,
		"--ledger", ledger, "--audit", audit, "--date", "2026-11-08",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !strings.Contains(stdout.String(), `"would_publish": false`) {
		t.Fatalf("dry run output = %s", stdout.String())
	}
	if _, err := os.Stat(ledger); err == nil {
		t.Fatal("dry run wrote the publish ledger")
	}
}

func TestPublishRequiresApprovalAndMatchingHash(t *testing.T) {
	draft, accounts, ledger, audit, hash := publishFixture(t)
	base := []string{
		"--channel", "devto", "--file", draft, "--accounts", accounts,
		"--ledger", ledger, "--audit", audit, "--date", "2026-11-08", "--confirm",
	}
	var stdout, stderr bytes.Buffer
	if err := runPublish(base, &stdout, &stderr); err == nil {
		t.Fatal("live publish without approval was accepted")
	}
	dir := t.TempDir()
	wrong := approvalFor(t, dir, "devto", "deadbeef")
	args := append(append([]string{}, base...), "--approval", wrong)
	if err := runPublish(args, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "hash") {
		t.Fatalf("wrong hash error = %v", err)
	}
	good := approvalFor(t, dir, "devto", hash)
	_ = good
}

func TestPublishToDevToWithApproval(t *testing.T) {
	draft, accounts, ledger, audit, hash := publishFixture(t)
	var seenTag string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/articles" || r.Header.Get("api-key") != "test-key" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var payload struct {
			Article struct {
				Title string   `json:"title"`
				Tags  []string `json:"tags"`
			} `json:"article"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		seenTag = strings.Join(payload.Article.Tags, ",")
		if payload.Article.Title == "" {
			http.Error(w, "no title", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, `{"id":123,"url":"https://dev.to/derive-world/test"}`)
	}))
	defer server.Close()
	t.Setenv("DEVTO_API_KEY", "test-key")
	t.Setenv("DGE_DEVTO_API_BASE", server.URL)
	approval := approvalFor(t, t.TempDir(), "devto", hash)
	var stdout, stderr bytes.Buffer
	err := runPublish([]string{
		"--channel", "devto", "--file", draft, "--accounts", accounts,
		"--ledger", ledger, "--audit", audit, "--date", "2026-11-08",
		"--approval", approval, "--confirm", "--tags", "opensource,ai",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("publish: %v\nstderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "https://dev.to/derive-world/test") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if seenTag != "opensource,ai" {
		t.Fatalf("tags = %q", seenTag)
	}
	published, err := loadPublishLedger(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if len(published.Entries) != 1 || published.Entries[0].ContentSHA256 != hash {
		t.Fatalf("ledger = %+v", published)
	}
	if !published.contains("devto", hash) {
		t.Fatal("ledger does not contain the published hash")
	}
	if _, err := os.Stat(audit); err != nil {
		t.Fatalf("audit log missing: %v", err)
	}
	// A second identical publish is refused before any network call.
	if err := runPublish([]string{
		"--channel", "devto", "--file", draft, "--accounts", accounts,
		"--ledger", ledger, "--audit", audit, "--date", "2026-11-09",
		"--approval", approval, "--confirm",
	}, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "already published") {
		t.Fatalf("duplicate publish error = %v", err)
	}
}

func TestManualPlatformRefusesAutomatedPublish(t *testing.T) {
	draft, accounts, ledger, audit, hash := publishFixture(t)
	approval := approvalFor(t, t.TempDir(), "hacker-news", hash)
	var stdout, stderr bytes.Buffer
	err := runPublish([]string{
		"--channel", "hacker-news", "--file", draft, "--accounts", accounts,
		"--ledger", ledger, "--audit", audit, "--date", "2026-11-08",
		"--approval", approval, "--confirm",
	}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "manual") {
		t.Fatalf("manual platform error = %v", err)
	}
}

func TestPublishToGitHubDiscussion(t *testing.T) {
	draft, accounts, ledger, audit, hash := publishFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var payload struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if strings.Contains(payload.Query, "createDiscussion") {
			fmt.Fprint(w, `{"data":{"createDiscussion":{"discussion":{"id":"D1","url":"https://github.com/QiyanAI/derive-world/discussions/1"}}}}`)
			return
		}
		fmt.Fprint(w, `{"data":{"repository":{"id":"R1"}}}`)
	}))
	defer server.Close()
	t.Setenv("DGE_GITHUB_TOKEN", "test-token")
	t.Setenv("DGE_GITHUB_REPO", "QiyanAI/derive-world")
	t.Setenv("DGE_GITHUB_DISCUSSION_CATEGORY_ID", "CAT1")
	t.Setenv("DGE_GITHUB_API_BASE", server.URL)
	approval := approvalFor(t, t.TempDir(), "github", hash)
	var stdout, stderr bytes.Buffer
	err := runPublish([]string{
		"--channel", "github", "--file", draft, "--accounts", accounts,
		"--ledger", ledger, "--audit", audit, "--date", "2026-11-08",
		"--approval", approval, "--confirm",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("GitHub publish: %v\nstderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "discussions/1") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}
