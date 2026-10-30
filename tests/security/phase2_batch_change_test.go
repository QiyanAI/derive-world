// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/QiyanAI/derive-world/internal/model"
)

type sequenceStep struct {
	text string
	err  error
}

// sequenceProvider returns one scripted step per call and records the exact
// request, including the clean-context retry prompt.
type sequenceProvider struct {
	mu       sync.Mutex
	steps    []sequenceStep
	requests []model.Request
}

func (p *sequenceProvider) Mode() string { return "fixture" }

func (p *sequenceProvider) Complete(_ context.Context, request model.Request) (model.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, request)
	if len(p.steps) == 0 {
		return model.Response{Text: `{"reply":"done","proposed_actions":[]}`, Model: "sequence-test-double"}, nil
	}
	step := p.steps[0]
	p.steps = p.steps[1:]
	if step.err != nil {
		return model.Response{}, step.err
	}
	return model.Response{Text: step.text, Model: "sequence-test-double"}, nil
}

func (p *sequenceProvider) promptAt(index int) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index < 0 || index >= len(p.requests) {
		return ""
	}
	return renderPrompt(p.requests[index])
}

func batchWorld(t *testing.T, user *testUser, title string) (objectResponse, string, string) {
	t.Helper()
	object := user.createObject(t, "open_world", title, map[string]any{
		"premise": "A small garden.",
		"kinds": []any{
			map[string]any{"name": "Lamp", "hue": 42, "fields": []any{}},
		},
		"nodes": []any{
			map[string]any{"kind_index": 0, "name": "North lamp", "text": "It glows."},
		},
		"links": []any{},
		"rules": []any{},
	})
	kinds, _ := object.State["kinds"].([]any)
	if len(kinds) == 0 {
		t.Fatal("created world has no kinds")
	}
	kind, _ := kinds[0].(map[string]any)
	kindID, _ := kind["id"].(string)
	nodes, _ := object.State["nodes"].([]any)
	nodeID := ""
	if len(nodes) > 0 {
		node, _ := nodes[0].(map[string]any)
		nodeID, _ = node["id"].(string)
	}
	if kindID == "" || nodeID == "" {
		t.Fatalf("world ids missing: kind=%q node=%q", kindID, nodeID)
	}
	return object, kindID, nodeID
}

func batchReply(t *testing.T, actions ...map[string]any) string {
	t.Helper()
	proposed := make([]any, 0, len(actions))
	for _, action := range actions {
		proposed = append(proposed, map[string]any{
			"capability_id": "object.propose_change", "input": action,
		})
	}
	encoded, err := json.Marshal(map[string]any{
		"reply": "I prepared one complete plan.", "proposed_actions": proposed,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func addNode(name, kindID string) map[string]any {
	return map[string]any{
		"action": "add_node",
		"input":  map[string]any{"kind_id": kindID, "name": name},
	}
}

func applyBatchProposal(t *testing.T, user *testUser, objectID, proposalID string,
	expectedVersion int) objectResponse {
	t.Helper()
	status, body, _ := user.call(t, http.MethodPost,
		"/api/v1/objects/"+objectID+"/proposals/"+proposalID+"/apply", callOptions{
			Body: map[string]any{"expected_version": expectedVersion},
		})
	requireStatus(t, status, http.StatusOK, body)
	var applied struct {
		Object objectResponse `json:"object"`
	}
	if err := json.Unmarshal(body, &applied); err != nil {
		t.Fatalf("decode applied object: %v", err)
	}
	return applied.Object
}

// The real model can return several propose_change actions for one intent.
// They must become one previewable, human-approved batch that advances the
// world version exactly once. Three consecutive turns must each keep working.
func TestAIBatchChangeKeepsThreeConsecutiveEditsRunnable(t *testing.T) {
	provider := &sequenceProvider{}
	env := newTestEnvWithProvider(t, nil, provider)
	user := env.register(t, "batchchange")
	object, kindID, originalNodeID := batchWorld(t, user, "Batch garden")
	conversation := user.createConversation(t, "Batch chat", &object.ID)

	turns := []struct {
		actions     []map[string]any
		wantVersion int
		wantNodes   int
	}{
		{
			actions: []map[string]any{
				addNode("Lamp A", kindID), addNode("Lamp B", kindID), addNode("Lamp C", kindID),
				{"action": "set_premise", "input": map[string]any{"premise": "A lit garden."}},
			},
			wantVersion: 2, wantNodes: 4,
		},
		{
			actions: []map[string]any{
				addNode("Lamp D", kindID), addNode("Lamp E", kindID),
				{"action": "set_title", "input": map[string]any{"title": "Evening garden"}},
			},
			wantVersion: 3, wantNodes: 6,
		},
		{
			actions: []map[string]any{
				addNode("Lamp F", kindID),
				{"action": "add_tag", "input": map[string]any{"tag": "night"}},
			},
			wantVersion: 4, wantNodes: 7,
		},
	}
	proposalIDs := make([]string, 0, len(turns))
	for index, turn := range turns {
		provider.mu.Lock()
		provider.steps = []sequenceStep{{text: batchReply(t, turn.actions...)}}
		provider.mu.Unlock()
		status, body := user.sendMessage(t, conversation, "update the garden")
		requireStatus(t, status, http.StatusOK, body)
		decoded := decodeTurn(t, body)
		if decoded.ParseStatus != "structured" {
			t.Fatalf("turn %d parse_status = %q body=%s", index+1, decoded.ParseStatus, body)
		}
		if len(decoded.ProposedActions) != 1 || decoded.ProposedActions[0].Action != "batch_change" {
			t.Fatalf("turn %d proposed = %+v", index+1, decoded.ProposedActions)
		}
		proposalID := decoded.ProposedActions[0].ProposalID
		if proposalID == "" {
			t.Fatalf("turn %d has no proposal id", index+1)
		}
		proposalIDs = append(proposalIDs, proposalID)
		applied := applyBatchProposal(t, user, object.ID, proposalID, turn.wantVersion-1)
		if applied.Version != turn.wantVersion {
			t.Fatalf("turn %d version = %d, want %d", index+1, applied.Version, turn.wantVersion)
		}
		nodes, _ := applied.State["nodes"].([]any)
		if len(nodes) != turn.wantNodes {
			t.Fatalf("turn %d nodes = %d, want %d", index+1, len(nodes), turn.wantNodes)
		}
		if index == 0 {
			first, _ := nodes[0].(map[string]any)
			if first["id"] != originalNodeID {
				t.Fatal("the original node id was not retained")
			}
		}
	}

	var proposalCount, appliedCount int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*), count(*) FILTER (WHERE status = 'applied')
  FROM object_proposals WHERE object_id = $1`, object.ID).Scan(
		&proposalCount, &appliedCount); err != nil {
		t.Fatal(err)
	}
	if proposalCount != 3 || appliedCount != 3 {
		t.Fatalf("proposals=%d applied=%d, want 3/3", proposalCount, appliedCount)
	}
	var batchAction, batchInput string
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT action, input::text FROM object_proposals WHERE id = $1`, proposalIDs[0]).Scan(
		&batchAction, &batchInput); err != nil {
		t.Fatal(err)
	}
	if batchAction != "batch_change" {
		t.Fatalf("proposal action = %q, want batch_change", batchAction)
	}
	for _, name := range []string{"Lamp A", "Lamp B", "Lamp C"} {
		if !strings.Contains(batchInput, name) {
			t.Fatalf("batch input lost %q: %s", name, batchInput)
		}
	}

	// Two plans prepared against the same version: applying one must expire
	// the other, with no card left for the user to discover and close.
	provider.mu.Lock()
	provider.steps = []sequenceStep{{text: batchReply(t, addNode("Lamp G", kindID))}}
	provider.mu.Unlock()
	status, body := user.sendMessage(t, conversation, "add one more lamp")
	requireStatus(t, status, http.StatusOK, body)
	first := decodeTurn(t, body)
	if len(first.ProposedActions) != 1 {
		t.Fatalf("first pending proposal = %+v", first.ProposedActions)
	}
	provider.mu.Lock()
	provider.steps = []sequenceStep{{text: batchReply(t, addNode("Lamp H", kindID))}}
	provider.mu.Unlock()
	status, body = user.sendMessage(t, conversation, "add another lamp")
	requireStatus(t, status, http.StatusOK, body)
	second := decodeTurn(t, body)
	if len(second.ProposedActions) != 1 {
		t.Fatalf("second pending proposal = %+v", second.ProposedActions)
	}
	applied := applyBatchProposal(t, user, object.ID, second.ProposedActions[0].ProposalID, 4)
	if applied.Version != 5 {
		t.Fatalf("version = %d, want 5", applied.Version)
	}
	var staleStatus string
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT status FROM object_proposals WHERE id = $1`, first.ProposedActions[0].ProposalID).Scan(&staleStatus); err != nil {
		t.Fatal(err)
	}
	if staleStatus != "expired" {
		t.Fatalf("older pending proposal status = %q, want expired", staleStatus)
	}
}

// An empty provider response must be recovered once with a clean context. The
// user never has to clear the conversation, and both calls are recorded.
func TestAIEmptyOutputRetriesWithCleanContext(t *testing.T) {
	provider := &sequenceProvider{steps: []sequenceStep{
		{text: `{"reply":"history marker ALPHA","proposed_actions":[]}`},
		{err: &model.Error{Code: model.CodeEmptyOutput, Message: "model response is empty"}},
		{text: `{"reply":"recovered from a clean context","proposed_actions":[]}`},
	}}
	env := newTestEnvWithProvider(t, nil, provider)
	user := env.register(t, "emptyretry")
	object, _, _ := batchWorld(t, user, "Retry garden")
	conversation := user.createConversation(t, "Retry chat", &object.ID)

	status, body := user.sendMessage(t, conversation, "first successful turn")
	requireStatus(t, status, http.StatusOK, body)

	status, body = user.sendMessage(t, conversation, "recover this turn")
	requireStatus(t, status, http.StatusOK, body)
	decoded := decodeTurn(t, body)
	if decoded.ParseStatus != "structured" {
		t.Fatalf("parse_status = %q body=%s", decoded.ParseStatus, body)
	}
	if !strings.Contains(decoded.AssistantMessage.Content, "recovered") {
		t.Fatalf("assistant content = %q", decoded.AssistantMessage.Content)
	}
	provider.mu.Lock()
	requestCount := len(provider.requests)
	provider.mu.Unlock()
	if requestCount != 3 {
		t.Fatalf("provider calls = %d, want 3 (one retry)", requestCount)
	}
	if !strings.Contains(provider.promptAt(1), "history marker ALPHA") {
		t.Fatal("the first attempt did not carry the conversation history")
	}
	if strings.Contains(provider.promptAt(2), "history marker ALPHA") {
		t.Fatal("the clean retry still carried the history")
	}
	var failed, succeeded int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FILTER (WHERE status = 'failed' AND error_code = 'model_empty_output'),
       count(*) FILTER (WHERE status = 'succeeded')
  FROM model_invocations WHERE conversation_id = $1`, conversation).Scan(&failed, &succeeded); err != nil {
		t.Fatal(err)
	}
	if failed != 1 || succeeded != 2 {
		t.Fatalf("invocations failed=%d succeeded=%d, want 1/2", failed, succeeded)
	}
}
