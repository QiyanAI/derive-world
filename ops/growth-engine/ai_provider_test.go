// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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
		SchemaVersion:   1,
		AuthorizedBy:    "test owner",
		AuthorizedAt:    "2026-11-08",
		ExpiresAt:       "2099-12-31",
		MaxCalls:        3,
		MaxInputChars:   20000,
		MaxOutputTokens: 2000,
		MaxCostUSD:      1.0,
		Purposes:        []string{"research", "strategy", "content"},
	}
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
	client := NewModelClient(testProvider(server.URL, "openai"), futureBudget(),
		&Usage{SchemaVersion: 1}, usagePath, "test-key", time.Now)
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
	client := NewModelClient(testProvider(server.URL, "anthropic"), futureBudget(),
		&Usage{SchemaVersion: 1}, filepath.Join(dir, "usage.json"), "test-key", time.Now)
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
	client := NewModelClient(testProvider(server.URL, "openai"), budget,
		&Usage{SchemaVersion: 1}, filepath.Join(dir, "usage.json"), "test-key", time.Now)
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
	client := NewModelClient(provider, budget, &Usage{SchemaVersion: 1},
		filepath.Join(t.TempDir(), "usage.json"), "test-key", time.Now)
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
	client := NewModelClient(testProvider("http://127.0.0.1:1", "openai"), budget,
		&Usage{SchemaVersion: 1}, filepath.Join(t.TempDir(), "usage.json"), "key", time.Now)
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
	client := NewModelClient(testProvider(server.URL, "openai"), budget,
		&Usage{SchemaVersion: 1}, filepath.Join(dir, "usage.json"), "test-key", time.Now)
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
	client := NewModelClient(testProvider(server.URL, "openai"), futureBudget(),
		&Usage{SchemaVersion: 1}, filepath.Join(t.TempDir(), "usage.json"), "test-key", time.Now)
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
	client := NewModelClient(testProvider(server.URL, "openai"), futureBudget(),
		&Usage{SchemaVersion: 1}, filepath.Join(t.TempDir(), "usage.json"), secret, time.Now)
	_, err := client.Complete(context.Background(), ModelRequest{Purpose: "research", System: "s", User: "u"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked the credential: %v", err)
	}
}
