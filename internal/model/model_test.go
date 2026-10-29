// SPDX-License-Identifier: Apache-2.0
package model

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAICompatibleRequestPath(t *testing.T) {
	var seenPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q", got)
		}
		var body chatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body.Model != "test-model" || body.MaxTokens != 2048 {
			t.Errorf("unexpected body: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"test-model","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`)
	}))
	defer server.Close()

	bases := []string{
		server.URL,
		server.URL + "/",
		server.URL + "/v1",
		server.URL + "/v1/",
	}
	for _, base := range bases {
		seenPath = ""
		provider, err := NewOpenAICompatible(OpenAIOptions{
			BaseURL:      base,
			APIKey:       "test-key",
			DefaultModel: "test-model",
		})
		if err != nil {
			t.Fatalf("NewOpenAICompatible(%q): %v", base, err)
		}
		response, err := provider.Complete(context.Background(), Request{
			Messages: []Message{{Role: "user", Content: "hello"}},
		})
		if err != nil {
			t.Fatalf("Complete(%q): %v", base, err)
		}
		if seenPath != "/v1/chat/completions" {
			t.Fatalf("base %q produced path %q, want /v1/chat/completions", base, seenPath)
		}
		if strings.Contains(seenPath, "/v1/v1") {
			t.Fatalf("duplicate /v1 in path %q", seenPath)
		}
		if response.Text != "ok" || response.Usage.InputTokens != 3 {
			t.Fatalf("unexpected response: %+v", response)
		}
	}
}

// A provider that stops on its output limit returns a partial document. The
// adapter must surface that fact so the Personal AI runtime can substitute a
// safe explanation instead of displaying the fragment.
func TestOpenAICompatibleReportsFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"test-model","choices":[{"message":{"content":"{\"reply\":\"part"},"finish_reason":"length"}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`)
	}))
	defer server.Close()
	provider := mustProvider(t, server.URL, time.Second)
	response, err := provider.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if response.FinishReason != "length" {
		t.Fatalf("finish reason = %q, want length", response.FinishReason)
	}
}

func TestOpenAICompatibleErrorMapping(t *testing.T) {
	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "nope", http.StatusInternalServerError)
		}))
		defer server.Close()
		provider := mustProvider(t, server.URL, time.Second)
		_, err := provider.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "x"}}})
		assertModelCode(t, err, CodeUnavailable)
	})

	t.Run("invalid json", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, "not-json")
		}))
		defer server.Close()
		provider := mustProvider(t, server.URL, time.Second)
		_, err := provider.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "x"}}})
		assertModelCode(t, err, CodeInvalidOutput)
	})

	t.Run("empty choices", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"choices":[]}`)
		}))
		defer server.Close()
		provider := mustProvider(t, server.URL, time.Second)
		_, err := provider.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "x"}}})
		assertModelCode(t, err, CodeInvalidOutput)
	})

	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)
			fmt.Fprint(w, `{"choices":[{"message":{"content":"late"}}]}`)
		}))
		defer server.Close()
		provider := mustProvider(t, server.URL, 10*time.Millisecond)
		_, err := provider.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "x"}}})
		assertModelCode(t, err, CodeTimeout)
	})

	t.Run("input too large", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("server must not be called for oversized input")
		}))
		defer server.Close()
		provider := mustProvider(t, server.URL, time.Second)
		_, err := provider.Complete(context.Background(), Request{
			Messages: []Message{{Role: "user", Content: strings.Repeat("x", maxInputBytes+1)}},
		})
		assertModelCode(t, err, CodePayloadTooLarge)
	})
}

func TestFixtureDeterministic(t *testing.T) {
	fixture := NewFixture("../../tests/fixtures/model")
	if fixture.Mode() != "fixture" {
		t.Fatalf("mode = %q", fixture.Mode())
	}
	request := Request{Messages: []Message{{Role: "user", Content: "hello"}}}
	first, err := fixture.Complete(context.Background(), request)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	second, err := fixture.Complete(context.Background(), request)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if first.Text != second.Text {
		t.Fatalf("fixture is not deterministic: %q vs %q", first.Text, second.Text)
	}
	if !strings.Contains(first.Text, "hello") || !strings.Contains(first.Text, "not real AI") {
		t.Fatalf("unexpected fixture reply: %q", first.Text)
	}

	unmatched, err := fixture.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "unknown prompt"}}})
	if err != nil {
		t.Fatalf("Complete unmatched: %v", err)
	}
	if !strings.Contains(unmatched.Text, "deterministic") {
		t.Fatalf("unmatched fixture reply: %q", unmatched.Text)
	}
}

func TestFixtureInputLimit(t *testing.T) {
	fixture := NewFixture("")
	_, err := fixture.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: strings.Repeat("x", maxInputBytes+1)}},
	})
	assertModelCode(t, err, CodePayloadTooLarge)
}

func TestNewFactory(t *testing.T) {
	fixture, err := New("fixture", "", "", "", time.Second, 100, "")
	if err != nil {
		t.Fatalf("New fixture: %v", err)
	}
	if _, ok := fixture.(*Fixture); !ok {
		t.Fatalf("fixture type = %T", fixture)
	}

	realProvider, err := New("real", "https://example.com", "key", "model", time.Second, 100, "")
	if err != nil {
		t.Fatalf("New real: %v", err)
	}
	if _, ok := realProvider.(*OpenAICompatible); !ok {
		t.Fatalf("real type = %T", realProvider)
	}

	if _, err := New("unknown", "", "", "", time.Second, 100, ""); err == nil {
		t.Fatal("unknown mode must fail")
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	cases := map[string]string{
		"https://api.example.com":     "https://api.example.com",
		"https://api.example.com/":    "https://api.example.com",
		"https://api.example.com/v1":  "https://api.example.com",
		"https://api.example.com/v1/": "https://api.example.com",
	}
	for input, want := range cases {
		if got := normalizeBaseURL(input); got != want {
			t.Fatalf("normalizeBaseURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func mustProvider(t *testing.T, baseURL string, timeout time.Duration) *OpenAICompatible {
	t.Helper()
	provider, err := NewOpenAICompatible(OpenAIOptions{
		BaseURL:      baseURL,
		APIKey:       "test-key",
		DefaultModel: "test-model",
		Timeout:      timeout,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatible: %v", err)
	}
	return provider
}

func assertModelCode(t *testing.T, err error, want ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error code %s, got nil", want)
	}
	var modelErr *Error
	if !errors.As(err, &modelErr) {
		t.Fatalf("error type = %T, want *model.Error: %v", err, err)
	}
	if modelErr.Code != want {
		t.Fatalf("error code = %s, want %s (%v)", modelErr.Code, want, err)
	}
}

func TestOpenAICompatibleRateLimitMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer server.Close()
	provider := mustProvider(t, server.URL, time.Second)
	_, err := provider.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "x"}}})
	assertModelCode(t, err, CodeRateLimited)
}

func TestOpenAICompatibleResponseSizeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", maxModelResponseBytes+1))
	}))
	defer server.Close()
	provider := mustProvider(t, server.URL, 5*time.Second)
	_, err := provider.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "x"}}})
	assertModelCode(t, err, CodeInvalidOutput)
}

func TestOpenAICompatibleOutputTokenCeiling(t *testing.T) {
	var seen int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body chatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		seen = body.MaxTokens
		fmt.Fprint(w, `{"model":"test-model","choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer server.Close()
	provider, err := NewOpenAICompatible(OpenAIOptions{
		BaseURL: server.URL, APIKey: "test-key", DefaultModel: "test-model",
		MaxOutputTokens: 128, Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if _, err := provider.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "x"}}, MaxOutputTokens: 9999,
	}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if seen != 128 {
		t.Fatalf("max_tokens = %d, want the 128-token ceiling", seen)
	}
}

func TestOpenAICompatibleTransportIsHardened(t *testing.T) {
	provider := mustProvider(t, "https://example.com", time.Second)
	transport, ok := provider.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T", provider.client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("model transport must not use environment or system proxies")
	}
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("TLS config = %#v, want TLS 1.2 minimum", transport.TLSClientConfig)
	}
	if transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("model transport disabled certificate verification")
	}
}

func TestOpenAICompatibleNegativeUsageIsSanitized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"model":"m","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":-5,"completion_tokens":-9}}`)
	}))
	defer server.Close()
	provider := mustProvider(t, server.URL, time.Second)
	response, err := provider.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if response.Usage.InputTokens != 0 || response.Usage.OutputTokens != 0 {
		t.Fatalf("usage = %+v, want zeroed negative values", response.Usage)
	}
}

func TestOpenAICompatibleDefaultProfileHasNoOptionalFields(t *testing.T) {
	var path string
	var body map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		fmt.Fprint(w, `{"model":"generic-model","choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer server.Close()
	provider := mustProvider(t, server.URL, time.Second)
	if _, err := provider.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hello"}},
	}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if path != "/v1/chat/completions" {
		t.Fatalf("default path = %q", path)
	}
	if _, ok := body["thinking"]; ok {
		t.Fatal("generic profile sent a thinking parameter")
	}
	if _, ok := body["response_format"]; ok {
		t.Fatal("generic profile sent a response_format parameter")
	}
	if string(body["model"]) != `"test-model"` {
		t.Fatalf("model = %s", body["model"])
	}
}

func TestOpenAICompatibleOptionalProviderProfile(t *testing.T) {
	var path string
	var body map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		fmt.Fprint(w, `{"model":"deepseek-flash","choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer server.Close()
	provider, err := NewOpenAICompatible(OpenAIOptions{
		BaseURL: server.URL, APIKey: "test-key", DefaultModel: "deepseek-flash",
		MaxOutputTokens: 256, Timeout: time.Second,
		ChatPath: "/chat/completions", ResponseFormat: "json_object",
		ThinkingMode: "disabled",
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if _, err := provider.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "return json"}},
	}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if path != "/chat/completions" {
		t.Fatalf("configured path = %q", path)
	}
	if string(body["thinking"]) != `{"type":"disabled"}` {
		t.Fatalf("thinking = %s", body["thinking"])
	}
	if string(body["response_format"]) != `{"type":"json_object"}` {
		t.Fatalf("response_format = %s", body["response_format"])
	}
	if string(body["max_tokens"]) != "256" {
		t.Fatalf("max_tokens = %s", body["max_tokens"])
	}
	if string(body["model"]) != `"deepseek-flash"` {
		t.Fatalf("model = %s", body["model"])
	}
}

func TestOpenAICompatibleRejectsInvalidAdapterOptions(t *testing.T) {
	cases := []OpenAIOptions{
		{BaseURL: "https://example.com", APIKey: "k", DefaultModel: "m", ChatPath: "//evil.example/path"},
		{BaseURL: "https://example.com", APIKey: "k", DefaultModel: "m", ChatPath: "https://evil.example/path"},
		{BaseURL: "https://example.com", APIKey: "k", DefaultModel: "m", ChatPath: "/path?x=1"},
		{BaseURL: "https://example.com", APIKey: "k", DefaultModel: "m", ChatPath: "/../path"},
		{BaseURL: "https://example.com", APIKey: "k", DefaultModel: "m", ResponseFormat: "json_schema"},
		{BaseURL: "https://example.com", APIKey: "k", DefaultModel: "m", ThinkingMode: "enabled"},
	}
	for _, options := range cases {
		if _, err := NewOpenAICompatible(options); err == nil {
			t.Fatalf("invalid adapter options accepted: %+v", options)
		}
	}
}

func TestNewWithOptionsFixtureIgnoresAdapterCapabilities(t *testing.T) {
	provider, err := NewWithOptions(AdapterOptions{
		Mode: "fixture", ChatPath: "/chat/completions",
		ResponseFormat: "json_object", ThinkingMode: "disabled",
	})
	if err != nil {
		t.Fatalf("fixture with optional fields: %v", err)
	}
	modeProvider, ok := provider.(ModeProvider)
	if !ok || modeProvider.Mode() != "fixture" {
		t.Fatalf("mode provider = %T", provider)
	}
}

func TestOpenAICompatibleBaseURLAndModelSubstitution(t *testing.T) {
	var path string
	var body map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		fmt.Fprint(w, `{"model":"alternate-model","choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer server.Close()
	provider, err := NewOpenAICompatible(OpenAIOptions{
		BaseURL: server.URL, APIKey: "test-key", DefaultModel: "generic-model",
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	response, err := provider.Complete(context.Background(), Request{
		Model:    "alternate-model",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if path != "/v1/chat/completions" {
		t.Fatalf("path = %q", path)
	}
	if string(body["model"]) != `"alternate-model"` {
		t.Fatalf("model = %s", body["model"])
	}
	if _, ok := body["thinking"]; ok {
		t.Fatal("substituted provider received a thinking parameter")
	}
	if _, ok := body["response_format"]; ok {
		t.Fatal("substituted provider received a response_format parameter")
	}
	if response.Model != "alternate-model" {
		t.Fatalf("response model = %q", response.Model)
	}
}
