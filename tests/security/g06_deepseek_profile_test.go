// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QiyanAI/derive-world/internal/config"
	"github.com/QiyanAI/derive-world/internal/model"
)

// mockDeepSeekEndpoint exercises the exact optional adapter profile without a
// paid call: /chat/completions, thinking disabled, response_format
// json_object, max_tokens 256. It is a mock endpoint, not Real AI.
type mockDeepSeekEndpoint struct {
	mu     sync.Mutex
	calls  int
	paths  []string
	bodies []mockDeepSeekRequest
}

type mockDeepSeekRequest struct {
	Model     string `json:"model"`
	MaxTokens int    `json:"max_tokens"`
	Messages  []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Thinking struct {
		Type string `json:"type"`
	} `json:"thinking"`
	ResponseFormat struct {
		Type string `json:"type"`
	} `json:"response_format"`
}

func (m *mockDeepSeekEndpoint) handle(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.calls++
	call := m.calls
	m.paths = append(m.paths, r.URL.Path)
	var payload mockDeepSeekRequest
	decodeErr := json.NewDecoder(r.Body).Decode(&payload)
	m.bodies = append(m.bodies, payload)
	m.mu.Unlock()
	if decodeErr != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if r.URL.Path != "/chat/completions" {
		http.Error(w, "wrong path", http.StatusNotFound)
		return
	}
	if r.Header.Get("Authorization") != "Bearer test-only-deepseek-key" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if payload.Model != "deepseek-flash" || payload.MaxTokens != 256 ||
		payload.Thinking.Type != "disabled" ||
		payload.ResponseFormat.Type != "json_object" {
		http.Error(w, "adapter profile mismatch", http.StatusBadRequest)
		return
	}
	content := ""
	switch call {
	case 1:
		content = `{"reply":"mock A proposal","proposed_actions":[{"capability_id":"object.propose_change","input":{"action":"set_body","input":{"body":"G06-MOCK-A-BODY"},"expected_version":1}}]}`
	case 2:
		content = `{"reply":"mock B derive","proposed_actions":[{"capability_id":"object.derive","input":{"title":"G06-MOCK-B-CHILD","source_version":2,"inheritance_fields":["body"]}}]}`
	default:
		http.Error(w, "unexpected third call", http.StatusTooManyRequests)
		return
	}
	response := map[string]any{
		"model": "deepseek-flash",
		"choices": []map[string]any{{
			"message": map[string]any{"content": content},
		}},
		"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (m *mockDeepSeekEndpoint) snapshot() (int, []string, []mockDeepSeekRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	paths := append([]string(nil), m.paths...)
	bodies := append([]mockDeepSeekRequest(nil), m.bodies...)
	return m.calls, paths, bodies
}

// TestG06DeepSeekProfileMockRelay proves that the DeepSeek optional request
// profile reaches the existing HTTP adapter and that both structured replies
// pass the existing schema, permission, confirmation, and derive paths. It is
// VERIFIED_FIXTURE/PASS_SECURITY, not Real AI acceptance.
func TestG06DeepSeekProfileMockRelay(t *testing.T) {
	endpoint := &mockDeepSeekEndpoint{}
	server := httptest.NewServer(http.HandlerFunc(endpoint.handle))
	defer server.Close()

	provider, err := model.NewWithOptions(model.AdapterOptions{
		Mode: "real", BaseURL: server.URL, APIKey: "test-only-deepseek-key",
		ModelName: "deepseek-flash", Timeout: 5 * time.Second,
		MaxOutputTokens: 256, ChatPath: "/chat/completions",
		ResponseFormat: "json_object", ThinkingMode: "disabled",
	})
	if err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	env := newTestEnvWithProvider(t, func(cfg *config.Config) {
		cfg.Model = config.ModelConfig{
			Mode: "real", BaseURL: server.URL, Name: "deepseek-flash",
			Timeout: 5 * time.Second, MaxOutputTokens: 256,
			ChatPath: "/chat/completions", ResponseFormat: "json_object",
			ThinkingMode: "disabled", MaxInputChars: 8192,
			DailyCallsPerUser: 2, DailyInputCharsPerUser: 16384,
			ConcurrencyPerUser: 1, GlobalDailyCalls: 2,
			IncludePrivateContext: true,
		}
	}, provider)

	a := env.register(t, "g06mocka")
	b := env.register(t, "g06mockb")
	a.setPrivateContext(t, "G06-MOCK-PRIVATE-A")
	b.setPrivateContext(t, "G06-MOCK-PRIVATE-B")
	object := a.createObject(t, "collaborative", "G06 mock parent", map[string]any{
		"body": "G06-MOCK-BASE-BODY",
	})

	aConversation := a.createConversation(t, "G06 mock A", &object.ID)
	status, body := a.sendMessage(t, aConversation, "return the A proposal json")
	requireStatus(t, status, http.StatusOK, body)
	var aTurn g06TurnResponse
	if err := json.Unmarshal(body, &aTurn); err != nil {
		t.Fatalf("decode A mock turn: %v", err)
	}
	if aTurn.ModelMode != "real" || aTurn.ParseStatus != "structured" ||
		len(aTurn.ProposedActions) != 1 || aTurn.ProposedActions[0].Kind != "proposal" {
		t.Fatalf("A mock turn = %+v body=%s", aTurn, body)
	}
	proposalID := aTurn.ProposedActions[0].ProposalID
	applyStatus, applyBody := applyProposal(t, a, object.ID, proposalID, 1)
	requireStatus(t, applyStatus, http.StatusOK, applyBody)

	invitation := a.createInvitation(t, object.ID, []string{"read", "contribute", "derive"}, 1)
	joinStatus, joinBody := b.acceptInvitation(t, *invitation.Token)
	requireStatus(t, joinStatus, http.StatusOK, joinBody)
	bConversation := b.createConversation(t, "G06 mock B", &object.ID)
	status, body = b.sendMessage(t, bConversation, "return the B derive json")
	requireStatus(t, status, http.StatusOK, body)
	var bTurn g06TurnResponse
	if err := json.Unmarshal(body, &bTurn); err != nil {
		t.Fatalf("decode B mock turn: %v", err)
	}
	if bTurn.ModelMode != "real" || bTurn.ParseStatus != "structured" ||
		len(bTurn.ProposedActions) != 1 || bTurn.ProposedActions[0].Kind != "confirmation" {
		t.Fatalf("B mock turn = %+v body=%s", bTurn, body)
	}
	confirmationID := bTurn.ProposedActions[0].ConfirmationID
	approveStatus, approveBody, _ := b.call(t, http.MethodPost,
		"/api/v1/confirmations/"+confirmationID+"/approve",
		callOptions{IdempotencyKey: newIdempotencyKey()})
	requireStatus(t, approveStatus, http.StatusOK, approveBody)
	var child objectResponse
	if err := json.Unmarshal(approveBody, &child); err != nil {
		t.Fatalf("decode mock child: %v", err)
	}
	if child.Lineage == nil || child.Lineage.Parent.ObjectID != object.ID ||
		child.Lineage.Parent.Version != 2 {
		t.Fatalf("mock child lineage = %+v", child.Lineage)
	}

	calls, paths, bodies := endpoint.snapshot()
	if calls != 2 {
		t.Fatalf("mock endpoint calls = %d, want 2", calls)
	}
	for index, path := range paths {
		if path != "/chat/completions" {
			t.Fatalf("call %d path = %q", index+1, path)
		}
	}
	for index, payload := range bodies {
		joined := ""
		for _, message := range payload.Messages {
			joined += message.Content + "\n"
		}
		if index == 0 {
			if !strings.Contains(joined, "G06-MOCK-PRIVATE-A") ||
				!strings.Contains(joined, "G06-MOCK-BASE-BODY") ||
				strings.Contains(joined, "G06-MOCK-PRIVATE-B") {
				t.Fatal("A's mock prompt did not contain exactly A's authorized context")
			}
		} else {
			if !strings.Contains(joined, "G06-MOCK-PRIVATE-B") ||
				!strings.Contains(joined, "G06-MOCK-A-BODY") ||
				strings.Contains(joined, "G06-MOCK-PRIVATE-A") {
				t.Fatal("B's mock prompt did not contain exactly B's authorized context")
			}
		}
	}

	var aInvocations, bInvocations int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM model_invocations
 WHERE user_id = $1 AND provider = 'real' AND status = 'succeeded'`,
		a.id).Scan(&aInvocations); err != nil {
		t.Fatalf("count A mock invocations: %v", err)
	}
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM model_invocations
 WHERE user_id = $1 AND provider = 'real' AND status = 'succeeded'`,
		b.id).Scan(&bInvocations); err != nil {
		t.Fatalf("count B mock invocations: %v", err)
	}
	if aInvocations != 1 || bInvocations != 1 {
		t.Fatalf("mock invocations A=%d B=%d, want one each", aInvocations, bInvocations)
	}
}
