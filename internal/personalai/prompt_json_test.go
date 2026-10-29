// SPDX-License-Identifier: Apache-2.0
package personalai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QiyanAI/derive-world/internal/model"
)

// DeepSeek JSON Output requires the prompt to contain an explicit json
// instruction when response_format=json_object is sent. Every Personal AI
// turn is either parsed as one json object or falls back; the world-creation
// branch must carry that instruction explicitly.
func TestWorldCreationPromptCarriesJSONModeInstruction(t *testing.T) {
	system, messages, chars, err := buildPrompt(
		&Profile{Name: "P2E Real Check"},
		nil,
		nil,
		nil,
		"Create a one-sentence world: a room with a single lamp.",
		"create",
	)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if len(messages) != 1 || messages[0].Role != "user" {
		t.Fatalf("messages = %+v", messages)
	}
	if !strings.Contains(strings.ToLower(system), "json") {
		t.Fatalf("world-creation prompt lacks an explicit json instruction (input_chars=%d)", chars)
	}
}

// This exercises the same wire profile as the real DeepSeek deployment against
// a local server: exact path, model, response_format, thinking, max_tokens,
// prompt instruction, and parsing of the provider-shaped reply. It never
// contacts a real provider.
func TestParseModelOutputRepairsMissingClosers(t *testing.T) {
	valid := `{"reply":"hello","proposed_actions":[{"capability_id":"world.create","input":{"title":"Room","brief":` +
		validWorldCreateBrief + `}}]}`
	truncated := strings.TrimSuffix(valid, "]}")
	if truncated == valid {
		t.Fatal("test fixture is not truncated")
	}
	reply, actions, status, err := parseModelOutput(truncated)
	if err != nil || status != "structured" || reply != "hello" || len(actions) != 1 {
		t.Fatalf("repaired parse: reply=%q status=%q actions=%d err=%v", reply, status, len(actions), err)
	}
	var create worldCreateActionInput
	if err := json.Unmarshal(actions[0].Input, &create); err != nil {
		t.Fatalf("action input: %v", err)
	}
	if _, rejection := buildWorldCreateContent(create); rejection != nil {
		t.Fatalf("repaired action rejected at %s: %s", rejection.Stage, rejection.Detail)
	}
}

func TestParseModelOutputAcceptsBenignTypeEcho(t *testing.T) {
	payload := `{"reply":"hello","proposed_actions":[{"capability_id":"world.create","input":{"title":"Room","brief":` +
		validWorldCreateBrief + `}}],"type":"json_object"}`
	reply, actions, status, err := parseModelOutput(payload)
	if err != nil || status != "structured" || reply != "hello" || len(actions) != 1 {
		t.Fatalf("echo parse: reply=%q status=%q actions=%d err=%v", reply, status, len(actions), err)
	}
	var create worldCreateActionInput
	if err := json.Unmarshal(actions[0].Input, &create); err != nil {
		t.Fatalf("action input: %v", err)
	}
	if _, rejection := buildWorldCreateContent(create); rejection != nil {
		t.Fatalf("echo action rejected at %s: %s", rejection.Stage, rejection.Detail)
	}
}

func TestParseModelOutputDoesNotRepairInsideString(t *testing.T) {
	reply, actions, status, err := parseModelOutput(`{"reply":"hel`)
	if err != nil || status != "fallback" || len(actions) != 0 || reply == "" {
		t.Fatalf("mid-string parse: reply=%q status=%q actions=%d err=%v", reply, status, len(actions), err)
	}
}

func TestCompleteTruncatedJSONRejectsMismatchedClosers(t *testing.T) {
	if _, ok := completeTruncatedJSON(`{"a":1]`); ok {
		t.Fatal("mismatched closer must not be repaired")
	}
}

func TestDeepSeekJSONWireProfileAndResponseParsing(t *testing.T) {
	system, messages, _, err := buildPrompt(
		&Profile{Name: "P2E Real Check"},
		nil,
		nil,
		nil,
		"Create a one-sentence world: a room with a single lamp.",
		"create",
	)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}

	var path string
	var wire struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		MaxTokens      int     `json:"max_tokens"`
		Temperature    float64 `json:"temperature"`
		ResponseFormat *struct {
			Type string `json:"type"`
		} `json:"response_format"`
		Thinking *struct {
			Type string `json:"type"`
		} `json:"thinking"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		content, err := json.Marshal(`{"reply":"A room with one lamp.","proposed_actions":[]}`)
		if err != nil {
			t.Errorf("marshal reply: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"model":"deepseek-flash","choices":[{"message":{"content":%s}}],"usage":{"prompt_tokens":10,"completion_tokens":8}}`, content)
	}))
	defer server.Close()

	provider, err := model.NewOpenAICompatible(model.OpenAIOptions{
		BaseURL:         server.URL,
		APIKey:          "test-key",
		DefaultModel:    "deepseek-flash",
		MaxOutputTokens: 2048,
		Timeout:         2 * time.Second,
		ChatPath:        "/chat/completions",
		ResponseFormat:  "json_object",
		ThinkingMode:    "disabled",
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	response, err := provider.Complete(context.Background(), model.Request{
		Model:       "deepseek-flash",
		System:      system,
		Messages:    messages,
		Temperature: 0.2,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	if path != "/chat/completions" {
		t.Fatalf("path = %q", path)
	}
	if wire.Model != "deepseek-flash" {
		t.Fatalf("model = %q", wire.Model)
	}
	if wire.ResponseFormat == nil || wire.ResponseFormat.Type != "json_object" {
		t.Fatalf("response_format = %+v", wire.ResponseFormat)
	}
	if wire.Thinking == nil || wire.Thinking.Type != "disabled" {
		t.Fatalf("thinking = %+v", wire.Thinking)
	}
	if wire.MaxTokens != 2048 || wire.Temperature != 0.2 {
		t.Fatalf("budget = max_tokens %d temperature %v", wire.MaxTokens, wire.Temperature)
	}
	if len(wire.Messages) != 2 || wire.Messages[0].Role != "system" || wire.Messages[1].Role != "user" {
		t.Fatalf("messages = %+v", wire.Messages)
	}
	if !strings.Contains(strings.ToLower(wire.Messages[0].Content), "json") {
		t.Fatalf("system message lacks an explicit json instruction")
	}

	reply, actions, status, err := parseModelOutput(response.Text)
	if err != nil {
		t.Fatalf("parse model output: %v", err)
	}
	if status != "structured" || reply == "" || len(actions) != 0 {
		t.Fatalf("parsed = reply %q status %q actions %d", reply, status, len(actions))
	}
}
