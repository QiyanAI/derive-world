// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/QiyanAI/derive-world/internal/config"
)

// TestG03RealAIConnectivity is intentionally opt-in. It proves only that one
// user can reach a real model; it is not the full two-user relay and cannot
// support Real AI acceptance. It never runs in normal CI, never prints the key,
// and must be invoked only with an authorized, bounded Real AI configuration.
func TestG03RealAIConnectivity(t *testing.T) {
	if os.Getenv("G03_REAL_AI_TEST") != "1" {
		t.Skip("Real AI connectivity is not configured in this job; the approved two-call run is recorded as VERIFIED_REAL")
	}
	if os.Getenv("CI") != "" && os.Getenv("G03_REAL_AI_ALLOW_CI") != "1" {
		t.Skip("Real AI connectivity refused in CI without G03_REAL_AI_ALLOW_CI=1")
	}
	if os.Getenv("G03_REAL_AI_AUTHORIZATION") != "APPROVED-2026-10-24" {
		t.Fatal("G03_REAL_AI_AUTHORIZATION=APPROVED-2026-10-24 is required for a Real AI run")
	}
	baseURL := os.Getenv("MODEL_BASE_URL")
	apiKey := os.Getenv("MODEL_API_KEY")
	modelName := os.Getenv("MODEL_NAME")
	if baseURL == "" || apiKey == "" || modelName == "" {
		t.Fatal("MODEL_BASE_URL, MODEL_API_KEY, and MODEL_NAME are required for the Real AI test")
	}
	env := newTestEnvWith(t, func(cfg *config.Config) {
		cfg.Model = config.ModelConfig{
			Mode: "real", BaseURL: baseURL, APIKey: apiKey, Name: modelName,
			Timeout: 60 * time.Second, MaxOutputTokens: 128,
			IncludePrivateContext: false,
		}
	})
	a := env.register(t, "g03real-a")
	object := a.createObject(t, "collaborative", "Real AI object", map[string]any{
		"body": "A short real-model connectivity object.",
	})
	conversation := a.createConversation(t, "Real AI chat", &object.ID)
	status, body := a.sendMessage(t, conversation, "Reply with the single word: hello.")
	if status != http.StatusOK {
		t.Fatalf("Real AI turn failed: %d %s", status, body)
	}
	var turn struct {
		ModelMode string `json:"model_mode"`
	}
	if err := json.Unmarshal(body, &turn); err != nil {
		t.Fatalf("decode Real AI turn: %v", err)
	}
	if turn.ModelMode != "real" {
		t.Fatalf("model_mode = %q, want real", turn.ModelMode)
	}
	var invocations int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM model_invocations
 WHERE user_id=$1 AND provider='real' AND status='succeeded'`, a.id).Scan(&invocations); err != nil {
		t.Fatalf("count Real AI invocations: %v", err)
	}
	if invocations < 1 {
		t.Fatal("no successful Real AI invocation was recorded")
	}
}
