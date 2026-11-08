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
	for _, channel := range []string{"devto", "github"} {
		account, _ := findAccount(accounts, channel)
		account.Status = "active"
		account.Security = Security{TwoFactor: true, RecoveryEmail: true}
		account.Authorization = Authorization{Scope: []string{"publish"}, GrantedBy: "owner", GrantedAt: "2026-11-08", ExpiresAt: "2099-12-31"}
	}
	accountsPath = filepath.Join(dir, "accounts.json")
	if err := writeFile(accountsPath, mustJSON(t, accounts), false); err != nil {
		t.Fatal(err)
	}
	return draftPath, accountsPath, filepath.Join(dir, "published.json"), filepath.Join(dir, "audit.jsonl"), hash
}

func makeOwnerKey(t *testing.T, dir string) (privatePath, keysPath string) {
	t.Helper()
	privatePath = filepath.Join(dir, "owner.key")
	keysPath = filepath.Join(dir, "approval-keys.json")
	var stdout, stderr bytes.Buffer
	if err := runApprovals([]string{
		"keygen", "--id", "test-owner", "--private", privatePath,
		"--keys", keysPath, "--date", "2026-11-08", "--force",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("keygen: %v\n%s", err, stderr.String())
	}
	return privatePath, keysPath
}

func signApproval(t *testing.T, dir, privatePath, approvalID, channel, hash string) string {
	t.Helper()
	payload := ApprovalPayload{
		SchemaVersion: 2,
		ApprovalID:    approvalID,
		Channel:       channel,
		Action:        "publish",
		ContentSHA256: hash,
		ApprovedBy:    "owner",
		ApprovedAt:    "2026-11-08",
		ExpiresAt:     "2099-12-31",
		KeyID:         "test-owner",
	}
	payloadPath := writeJSONTemp(t, dir, "payload-"+approvalID+".json", payload)
	output := filepath.Join(dir, "approval-"+approvalID+".json")
	var stdout, stderr bytes.Buffer
	if err := runApprovals([]string{
		"sign", "--file", payloadPath, "--key", privatePath, "--out", output, "--force",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("sign: %v\n%s", err, stderr.String())
	}
	return output
}

func approvalArgs(dir, keysPath, approvalPath string) []string {
	return []string{
		"--keys", keysPath,
		"--approval-uses", filepath.Join(dir, "approvals.json"),
		"--approval", approvalPath,
	}
}

func TestPublishDryRunSendsNothing(t *testing.T) {
	draft, accounts, ledger, audit, _ := publishFixture(t)
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	err := runPublish([]string{
		"--channel", "devto", "--file", draft, "--accounts", accounts,
		"--ledger", ledger, "--audit", audit, "--date", "2026-11-08",
		"--approval-uses", filepath.Join(dir, "approvals.json"),
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

func TestPublishRequiresSignedApprovalAndMatchingHash(t *testing.T) {
	draft, accounts, ledger, audit, hash := publishFixture(t)
	dir := t.TempDir()
	privatePath, keysPath := makeOwnerKey(t, dir)
	base := []string{
		"--channel", "devto", "--file", draft, "--accounts", accounts,
		"--ledger", ledger, "--audit", audit, "--date", "2026-11-08", "--confirm",
		"--keys", keysPath, "--approval-uses", filepath.Join(dir, "approvals.json"),
	}
	var stdout, stderr bytes.Buffer
	if err := runPublish(base, &stdout, &stderr); err == nil {
		t.Fatal("live publish without approval was accepted")
	}

	unsigned := writeJSONTemp(t, dir, "unsigned.json", ApprovalPayload{
		SchemaVersion: 2, ApprovalID: "unsigned", Channel: "devto", Action: "publish",
		ContentSHA256: hash, ApprovedBy: "owner", ApprovedAt: "2026-11-08",
		ExpiresAt: "2099-12-31", KeyID: "test-owner",
	})
	if err := runPublish(append(base, "--approval", unsigned), &stdout, &stderr); err == nil {
		t.Fatal("an unsigned approval was accepted")
	}

	forged := signApproval(t, dir, privatePath, "forged", "devto", hash)
	raw, err := os.ReadFile(forged)
	if err != nil {
		t.Fatal(err)
	}
	var signed SignedApproval
	if err := json.Unmarshal(raw, &signed); err != nil {
		t.Fatal(err)
	}
	signed.ApprovedBy = "someone-else"
	tampered, _ := json.MarshalIndent(signed, "", "  ")
	if err := os.WriteFile(forged, tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runPublish(append(base, "--approval", forged), &stdout, &stderr); err == nil ||
		!strings.Contains(strings.ToLower(err.Error()), "signature") {
		t.Fatalf("tampered approval error = %v", err)
	}

	wrong := signApproval(t, dir, privatePath, "wrong-hash", "devto", "deadbeef")
	if err := runPublish(append(base, "--approval", wrong), &stdout, &stderr); err == nil ||
		!strings.Contains(err.Error(), "hash") {
		t.Fatalf("wrong hash error = %v", err)
	}
}

func TestPublishToDevToWithApproval(t *testing.T) {
	draft, accounts, ledger, audit, hash := publishFixture(t)
	dir := t.TempDir()
	privatePath, keysPath := makeOwnerKey(t, dir)
	approval := signApproval(t, dir, privatePath, "devto-ok", "devto", hash)
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
	args := []string{
		"--channel", "devto", "--file", draft, "--accounts", accounts,
		"--ledger", ledger, "--audit", audit, "--date", "2026-11-08",
		"--confirm", "--tags", "opensource,ai",
	}
	args = append(args, approvalArgs(dir, keysPath, approval)...)
	var stdout, stderr bytes.Buffer
	if err := runPublish(args, &stdout, &stderr); err != nil {
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
	if len(published.Entries) != 1 || published.Entries[0].Status != "published" || published.Entries[0].ContentSHA256 != hash {
		t.Fatalf("ledger = %+v", published)
	}
	uses, err := loadApprovalUses(filepath.Join(dir, "approvals.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !uses.contains("devto-ok") {
		t.Fatal("approval use was not recorded")
	}
	if _, err := os.Stat(audit); err != nil {
		t.Fatalf("audit log missing: %v", err)
	}
	// A second identical publish is refused before any network call.
	if err := runPublish(args, &stdout, &stderr); err == nil ||
		!strings.Contains(err.Error(), "pending, unknown or published") {
		t.Fatalf("duplicate publish error = %v", err)
	}
}

func TestApprovalReplayIsRefused(t *testing.T) {
	draft, accounts, ledger, audit, _ := publishFixture(t)
	dir := t.TempDir()
	privatePath, keysPath := makeOwnerKey(t, dir)
	// Build a second draft with a different title and hash.
	modified := filepath.Join(dir, "modified.md")
	raw, err := os.ReadFile(draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modified, []byte(strings.Replace(string(raw), "# Derive World:", "# Derive World (2):", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareForPublication(string(mustRead(t, modified)))
	if err != nil {
		t.Fatal(err)
	}
	title, body := splitTitleAndBody(prepared)
	newHash := contentHash(title, body)
	if newHash == contentHash(strings.Split(prepared, "\n")[0], "") {
		t.Fatal("test fixture did not produce a different hash")
	}
	approval := signApproval(t, dir, privatePath, "replay-id", "devto", newHash)
	// Consume the approval id without publishing.
	usesPath := filepath.Join(dir, "approvals.json")
	uses := &ApprovalUseFile{SchemaVersion: 1, Uses: []ApprovalUse{{
		ApprovalID: "replay-id", Channel: "devto", ContentSHA256: newHash, UsedAt: "2026-11-08",
	}}}
	if err := uses.save(usesPath); err != nil {
		t.Fatal(err)
	}
	args := []string{
		"--channel", "devto", "--file", modified, "--accounts", accounts,
		"--ledger", ledger, "--audit", audit, "--date", "2026-11-08", "--confirm",
		"--keys", keysPath, "--approval-uses", usesPath, "--approval", approval,
	}
	var stdout, stderr bytes.Buffer
	if err := runPublish(args, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("replayed approval error = %v", err)
	}
}

func TestManualPlatformRefusesAutomatedPublish(t *testing.T) {
	draft, accounts, ledger, audit, _ := publishFixture(t)
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	err := runPublish([]string{
		"--channel", "hacker-news", "--file", draft, "--accounts", accounts,
		"--ledger", ledger, "--audit", audit, "--date", "2026-11-08",
		"--approval-uses", filepath.Join(dir, "approvals.json"), "--confirm",
	}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "manual") {
		t.Fatalf("manual platform error = %v", err)
	}
}

func TestPublishToGitHubDiscussion(t *testing.T) {
	draft, accounts, ledger, audit, hash := publishFixture(t)
	dir := t.TempDir()
	privatePath, keysPath := makeOwnerKey(t, dir)
	approval := signApproval(t, dir, privatePath, "github-ok", "github", hash)
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
	args := []string{
		"--channel", "github", "--file", draft, "--accounts", accounts,
		"--ledger", ledger, "--audit", audit, "--date", "2026-11-08", "--confirm",
	}
	args = append(args, approvalArgs(dir, keysPath, approval)...)
	var stdout, stderr bytes.Buffer
	if err := runPublish(args, &stdout, &stderr); err != nil {
		t.Fatalf("GitHub publish: %v\nstderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "discussions/1") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestAmbiguousPublishRequiresReconcile(t *testing.T) {
	draft, accounts, ledger, audit, hash := publishFixture(t)
	dir := t.TempDir()
	privatePath, keysPath := makeOwnerKey(t, dir)
	failing := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing {
			http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"id":9,"url":"https://dev.to/derive-world/recovered"}`)
	}))
	defer server.Close()
	t.Setenv("DEVTO_API_KEY", "test-key")
	t.Setenv("DGE_DEVTO_API_BASE", server.URL)
	firstApproval := signApproval(t, dir, privatePath, "attempt-1", "devto", hash)
	args := []string{
		"--channel", "devto", "--file", draft, "--accounts", accounts,
		"--ledger", ledger, "--audit", audit, "--date", "2026-11-08", "--confirm",
		"--keys", keysPath, "--approval-uses", filepath.Join(dir, "approvals.json"),
		"--approval", firstApproval,
	}
	var stdout, stderr bytes.Buffer
	if err := runPublish(args, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "UNKNOWN") {
		t.Fatalf("ambiguous error = %v", err)
	}
	ledgerAfter, err := loadPublishLedger(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if len(ledgerAfter.Entries) != 1 || ledgerAfter.Entries[0].Status != "unknown" {
		t.Fatalf("ledger after failure = %+v", ledgerAfter)
	}
	// The same hash cannot be retried until the owner reconciles it.
	secondApproval := signApproval(t, dir, privatePath, "attempt-2", "devto", hash)
	args = append(args[:len(args)-1], secondApproval)
	if err := runPublish(args, &stdout, &stderr); err == nil ||
		!strings.Contains(err.Error(), "reconcile") {
		t.Fatalf("unreconciled retry error = %v", err)
	}
	// Reconcile as not published, then a fresh approval can publish.
	if err := runPublish([]string{
		"reconcile", "--channel", "devto", "--hash", hash, "--ledger", ledger,
		"--audit", audit, "--date", "2026-11-09", "--not-published",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	failing = false
	finalApproval := signApproval(t, dir, privatePath, "attempt-3", "devto", hash)
	args = append(args[:len(args)-1], finalApproval)
	if err := runPublish(args, &stdout, &stderr); err != nil {
		t.Fatalf("publish after reconcile: %v", err)
	}
	ledgerAfter, err = loadPublishLedger(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if len(ledgerAfter.Entries) != 2 || ledgerAfter.Entries[1].Status != "published" {
		t.Fatalf("ledger after recovery = %+v", ledgerAfter)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
