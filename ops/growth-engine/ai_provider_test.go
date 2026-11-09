// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeJSONTemp(t *testing.T, dir, name string, value any) string {
	t.Helper()
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func futureBudget() *Budget {
	return &Budget{
		SchemaVersion:       1,
		AuthorizationID:     "test-auth",
		AuthorizedBy:        "test owner",
		AuthorizedAt:        "2026-11-08",
		ExpiresAt:           "2099-12-31",
		MaxCalls:            3,
		MaxInputChars:       20000,
		MaxOutputTokens:     2000,
		MaxCostUSD:          0,
		AllowWithoutCostCap: true,
		Purposes:            []string{"research", "strategy", "content"},
	}
}

func testClient(provider Provider, budget *Budget, usagePath, apiKey string) *ModelClient {
	return NewModelClient(provider, budget, &FileUsageStore{Path: usagePath, Now: time.Now}, apiKey, time.Now)
}

func testProvider(baseURL, protocol string) Provider {
	return Provider{
		ID:                    "test-provider",
		Protocol:              protocol,
		BaseURL:               baseURL,
		Model:                 "test-model",
		APIKeyEnv:             "DGE_TEST_KEY",
		MaxOutputTokens:       200,
		TimeoutSeconds:        5,
		InputPricePerMillion:  0,
		OutputPricePerMillion: 0,
	}
}

func openAIServer(t *testing.T, reply func(body map[string]any) string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		text := reply(body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": text}}},
			"usage":   map[string]int{"prompt_tokens": 10, "completion_tokens": 20},
		})
	}))
}

func TestOpenAIProviderCallAndUsage(t *testing.T) {
	server := openAIServer(t, func(map[string]any) string { return "hello" })
	defer server.Close()
	dir := t.TempDir()
	usagePath := filepath.Join(dir, "usage.json")
	client := testClient(testProvider(server.URL, "openai"), futureBudget(),
		usagePath, "test-key")
	response, err := client.Complete(context.Background(), ModelRequest{
		Purpose: "research", System: "system", User: "user", MaxOutputTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "hello" || response.InputTokens != 10 || response.OutputTokens != 20 {
		t.Fatalf("response = %+v", response)
	}
	usage, err := loadUsage(usagePath)
	if err != nil {
		t.Fatal(err)
	}
	if usage.Calls != 1 || usage.InputTokens != 10 || usage.OutputTokens != 20 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestAnthropicProviderCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "test-key" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, `{"content":[{"type":"text","text":"from anthropic"}],"usage":{"input_tokens":7,"output_tokens":9}}`)
	}))
	defer server.Close()
	dir := t.TempDir()
	client := testClient(testProvider(server.URL, "anthropic"), futureBudget(),
		filepath.Join(dir, "usage.json"), "test-key")
	response, err := client.Complete(context.Background(), ModelRequest{
		Purpose: "strategy", System: "system", User: "user", MaxOutputTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "from anthropic" || response.InputTokens != 7 {
		t.Fatalf("response = %+v", response)
	}
}

func TestBudgetRefusesUnauthorizedPurposeBeforeAnyRequest(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	budget := futureBudget()
	budget.Purposes = []string{"content"}
	dir := t.TempDir()
	client := testClient(testProvider(server.URL, "openai"), budget,
		filepath.Join(dir, "usage.json"), "test-key")
	if _, err := client.Complete(context.Background(), ModelRequest{Purpose: "research", System: "s", User: "u"}); err == nil {
		t.Fatal("unauthorized purpose accepted")
	}
	if hits != 0 {
		t.Fatalf("model endpoint was called %d times", hits)
	}
}

func TestBudgetRefusesEstimatedCostOverCap(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		fmt.Fprint(w, `{"choices":[{"message":{"content":"x"}}]}`)
	}))
	defer server.Close()
	provider := testProvider(server.URL, "openai")
	provider.InputPricePerMillion = 1_000_000
	provider.OutputPricePerMillion = 1_000_000
	budget := futureBudget()
	budget.MaxCostUSD = 0.0000001
	client := testClient(provider, budget, filepath.Join(t.TempDir(), "usage.json"), "test-key")
	if _, err := client.Complete(context.Background(), ModelRequest{Purpose: "research", System: "s", User: "u", MaxOutputTokens: 100}); err == nil {
		t.Fatal("cost cap not enforced")
	}
	if hits != 0 {
		t.Fatalf("model endpoint was called %d times", hits)
	}
}

func TestBudgetExpiryIsEnforced(t *testing.T) {
	budget := futureBudget()
	budget.ExpiresAt = "2020-01-01"
	client := testClient(testProvider("http://127.0.0.1:1", "openai"), budget,
		filepath.Join(t.TempDir(), "usage.json"), "key")
	if _, err := client.Complete(context.Background(), ModelRequest{Purpose: "research", System: "s", User: "u"}); err == nil {
		t.Fatal("expired budget accepted")
	}
}

func TestCallBudgetIsEnforced(t *testing.T) {
	server := openAIServer(t, func(map[string]any) string { return "ok" })
	defer server.Close()
	budget := futureBudget()
	budget.MaxCalls = 1
	dir := t.TempDir()
	client := testClient(testProvider(server.URL, "openai"), budget,
		filepath.Join(dir, "usage.json"), "test-key")
	if _, err := client.Complete(context.Background(), ModelRequest{Purpose: "research", System: "s", User: "u"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Complete(context.Background(), ModelRequest{Purpose: "research", System: "s", User: "u"}); err == nil {
		t.Fatal("second call exceeded the budget but was accepted")
	}
}

func TestRedirectsAreRefused(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"leaked"}}]}`)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client := testClient(testProvider(server.URL, "openai"), futureBudget(),
		filepath.Join(t.TempDir(), "usage.json"), "test-key")
	if _, err := client.Complete(context.Background(), ModelRequest{Purpose: "research", System: "s", User: "u"}); err == nil {
		t.Fatal("redirect was followed")
	}
}

func TestProviderConfigRefusesPlaintextRemoteEndpoint(t *testing.T) {
	provider := testProvider("http://example.com/v1", "openai")
	if err := validateProvider(provider); err == nil {
		t.Fatal("plaintext remote provider accepted")
	}
	if err := validateProvider(testProvider("http://127.0.0.1:8080", "openai")); err != nil {
		t.Fatalf("loopback provider rejected: %v", err)
	}
}

func TestModelErrorDoesNotEchoCredential(t *testing.T) {
	secret := "super-secret-key-value"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream rejected the request", http.StatusBadGateway)
	}))
	defer server.Close()
	client := testClient(testProvider(server.URL, "openai"), futureBudget(),
		filepath.Join(t.TempDir(), "usage.json"), secret)
	_, err := client.Complete(context.Background(), ModelRequest{Purpose: "research", System: "s", User: "u"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked the credential: %v", err)
	}
}

func TestCrossRunBudgetPersistenceUsesSharedLedger(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":5,"completion_tokens":5}}`)
	}))
	defer server.Close()
	usagePath := filepath.Join(t.TempDir(), "usage.json")
	budget := futureBudget()
	budget.MaxCalls = 1
	first := testClient(testProvider(server.URL, "openai"), budget, usagePath, "key")
	if _, err := first.Complete(context.Background(), ModelRequest{Purpose: "research", System: "s", User: "u"}); err != nil {
		t.Fatal(err)
	}
	// A new client represents a new process or a fresh GitHub Actions runner.
	second := testClient(testProvider(server.URL, "openai"), budget, usagePath, "key")
	if _, err := second.Complete(context.Background(), ModelRequest{Purpose: "research", System: "s", User: "u"}); err == nil {
		t.Fatal("a fresh client reset the cumulative call budget")
	}
	if hits != 1 {
		t.Fatalf("model endpoint hits = %d, want 1", hits)
	}
}

func TestCostCapIsRefusedWhenPricingIsUnknown(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer server.Close()
	budget := futureBudget()
	budget.MaxCostUSD = 1.0
	budget.AllowWithoutCostCap = false
	client := testClient(testProvider(server.URL, "openai"), budget, filepath.Join(t.TempDir(), "usage.json"), "key")
	if _, err := client.Complete(context.Background(), ModelRequest{Purpose: "research", System: "s", User: "u"}); err == nil {
		t.Fatal("a zero-price provider was allowed to claim a hard cost cap")
	}
	if hits != 0 {
		t.Fatalf("model endpoint was called %d times", hits)
	}
}

func TestUnpricedRunDoesNotClaimCostCap(t *testing.T) {
	server := openAIServer(t, func(map[string]any) string { return "ok" })
	defer server.Close()
	budget := futureBudget()
	budget.MaxCostUSD = 0
	budget.AllowWithoutCostCap = true
	usagePath := filepath.Join(t.TempDir(), "usage.json")
	client := testClient(testProvider(server.URL, "openai"), budget, usagePath, "key")
	response, err := client.Complete(context.Background(), ModelRequest{Purpose: "research", System: "s", User: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if response.CostCapEnforced {
		t.Fatal("an unpriced provider claimed a hard cost cap")
	}
	usage, err := loadUsage(usagePath)
	if err != nil {
		t.Fatal(err)
	}
	if usage.CostCapEnforced {
		t.Fatal("usage ledger claimed a hard cost cap without pricing")
	}
	summary := summarizeUsage(usage, budget, testProvider(server.URL, "openai"))
	if !strings.Contains(summary.Note, "NOT enforced") || summary.RemainingCostUSD != 0 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestStaleUsageLockIsRecovered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	lock := path + ".lock"
	if err := os.WriteFile(lock, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	store := &FileUsageStore{Path: path, Now: time.Now}
	if err := store.Update(func(usage *Usage) error { usage.Calls++; return nil }); err != nil {
		t.Fatalf("stale lock was not recovered: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func TestGitUsageStorePersistsAcrossFreshClones(t *testing.T) {
	root := t.TempDir()
	bare := filepath.Join(root, "state.git")
	work := filepath.Join(root, "work")
	runGit(t, root, "init", "--bare", "--initial-branch=main", bare)
	runGit(t, root, "clone", bare, work)
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	initial := `{"schema_version":2,"authorization_id":"","calls":0,"input_tokens":0,"output_tokens":0,"cost_usd":0,"cost_cap_enforced":false}
`
	if err := os.WriteFile(filepath.Join(work, "usage.json"), []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "usage.json")
	runGit(t, work, "commit", "-m", "init usage")
	runGit(t, work, "push", "origin", "main")

	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":3}}`)
	}))
	defer server.Close()
	budget := futureBudget()
	budget.MaxCalls = 1
	provider := testProvider(server.URL, "openai")
	first := NewModelClient(provider, budget, &GitUsageStore{Dir: work, File: "usage.json", Branch: "main", Now: time.Now}, "key", time.Now)
	if _, err := first.Complete(context.Background(), ModelRequest{Purpose: "research", System: "s", User: "u"}); err != nil {
		t.Fatalf("first call: %v", err)
	}

	work2 := filepath.Join(root, "work2")
	runGit(t, root, "clone", "--branch", "main", bare, work2)
	second := NewModelClient(provider, budget, &GitUsageStore{Dir: work2, File: "usage.json", Branch: "main", Now: time.Now}, "key", time.Now)
	if _, err := second.Complete(context.Background(), ModelRequest{Purpose: "research", System: "s", User: "u"}); err == nil {
		t.Fatal("a fresh clone reset the cumulative budget")
	}
	if hits != 1 {
		t.Fatalf("model endpoint hits = %d, want 1", hits)
	}
	usage, err := second.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if usage.Calls != 1 || usage.AuthorizationID != "test-auth" {
		t.Fatalf("persisted usage = %+v", usage)
	}
}

func TestOpenAIThinkingModeParameter(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":3}}`)
	}))
	defer server.Close()
	provider := testProvider(server.URL, "openai")
	provider.ThinkingMode = "disabled"
	client := testClient(provider, futureBudget(), filepath.Join(t.TempDir(), "usage.json"), "key")
	if _, err := client.Complete(context.Background(), ModelRequest{Purpose: "content", System: "s", User: "u"}); err != nil {
		t.Fatal(err)
	}
	thinking, ok := got["thinking"].(map[string]any)
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("thinking parameter = %#v", got["thinking"])
	}
}
