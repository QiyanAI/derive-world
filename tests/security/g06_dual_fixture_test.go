// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/QiyanAI/derive-world/internal/config"
)

type g06TurnResponse struct {
	ModelMode    string `json:"model_mode"`
	ParseStatus  string `json:"parse_status"`
	ContextStale bool   `json:"context_stale"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	AssistantMessage struct {
		ModelMode   string `json:"model_mode"`
		ParseStatus string `json:"parse_status"`
	} `json:"assistant_message"`
	ProposedActions []struct {
		Kind           string `json:"kind"`
		ProposalID     string `json:"proposal_id"`
		ConfirmationID string `json:"confirmation_id"`
		Action         string `json:"action"`
	} `json:"proposed_actions"`
}

// TestG06DualPersonalAIRelayFixture proves the complete two-user,
// two-Personal-AI relay with deterministic Fixture output. It is a regression
// for the G06 relay shape only; it is not Real AI acceptance and is labelled
// as Fixture throughout.
func TestG06DualPersonalAIRelayFixture(t *testing.T) {
	provider := &scriptedProvider{replies: []string{
		`{"reply":"A fixture turn","proposed_actions":[{"capability_id":"object.propose_change","input":{"action":"set_body","input":{"body":"Fixture proposed body"},"expected_version":1}}]}`,
		`{"reply":"B fixture turn","proposed_actions":[{"capability_id":"object.derive","input":{"title":"Fixture derived child","source_version":2,"inheritance_fields":["body"]}}]}`,
	}}
	env := newTestEnvWithProvider(t, func(cfg *config.Config) {
		cfg.Model.IncludePrivateContext = true
	}, provider)
	a := env.register(t, "g06dualfa")
	b := env.register(t, "g06dualB")
	if a.profileID(t) == b.profileID(t) {
		t.Fatal("both users received the same Personal AI profile")
	}
	a.setPrivateContext(t, "G06-FIXTURE-PRIVATE-A")
	b.setPrivateContext(t, "G06-FIXTURE-PRIVATE-B")

	object := a.createObject(t, "collaborative", "G06 fixture parent", map[string]any{
		"body":    "G06-FIXTURE-BASE-BODY",
		"summary": "Two independent Personal AI turns.",
	})

	// A's Personal AI proposes one human-reviewable change.
	aConversation := a.createConversation(t, "G06 A Personal AI", &object.ID)
	status, body := a.sendMessage(t, aConversation, "propose_change")
	requireStatus(t, status, http.StatusOK, body)
	var aTurn g06TurnResponse
	if err := json.Unmarshal(body, &aTurn); err != nil {
		t.Fatalf("decode A turn: %v", err)
	}
	if aTurn.ModelMode != "fixture" || aTurn.ParseStatus != "structured" ||
		aTurn.AssistantMessage.ModelMode != "fixture" {
		t.Fatalf("A turn = %+v, want structured Fixture output", aTurn)
	}
	if len(aTurn.ProposedActions) != 1 || aTurn.ProposedActions[0].Kind != "proposal" ||
		aTurn.ProposedActions[0].ProposalID == "" {
		t.Fatalf("A proposed actions = %+v", aTurn.ProposedActions)
	}
	proposalID := aTurn.ProposedActions[0].ProposalID

	// The model turn alone changes nothing.
	preStatus, preBody, _ := a.call(t, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, preStatus, http.StatusOK, preBody)
	if before := decodeObject(t, preBody); before.Version != 1 {
		t.Fatalf("AI turn changed the object before human review: version %d", before.Version)
	}

	applyStatus, applyBody := applyProposal(t, a, object.ID, proposalID, 1)
	requireStatus(t, applyStatus, http.StatusOK, applyBody)
	var applied struct {
		Object objectResponse `json:"object"`
	}
	if err := json.Unmarshal(applyBody, &applied); err != nil {
		t.Fatalf("decode A apply: %v", err)
	}
	if applied.Object.Version != 2 || applied.Object.State["body"] != "Fixture proposed body" {
		t.Fatalf("A applied object = %+v", applied.Object)
	}

	// A authorizes B. B's AI sees only B's clipped view.
	invitation := a.createInvitation(t, object.ID, []string{"read", "contribute", "derive"}, 1)
	joinStatus, joinBody := b.acceptInvitation(t, *invitation.Token)
	requireStatus(t, joinStatus, http.StatusOK, joinBody)

	bConversation := b.createConversation(t, "G06 B Personal AI", &object.ID)
	status, body = b.sendMessage(t, bConversation, "propose_derive")
	requireStatus(t, status, http.StatusOK, body)
	var bTurn g06TurnResponse
	if err := json.Unmarshal(body, &bTurn); err != nil {
		t.Fatalf("decode B turn: %v", err)
	}
	if bTurn.ModelMode != "fixture" || bTurn.ParseStatus != "structured" {
		t.Fatalf("B turn = %+v, want structured Fixture output", bTurn)
	}
	if len(bTurn.ProposedActions) != 1 || bTurn.ProposedActions[0].Kind != "confirmation" ||
		bTurn.ProposedActions[0].ConfirmationID == "" {
		t.Fatalf("B proposed actions = %+v", bTurn.ProposedActions)
	}
	confirmationID := bTurn.ProposedActions[0].ConfirmationID

	// Each Personal AI saw only its own private context and the shared,
	// permission-clipped object view.
	if aPrompt := provider.promptAt(0); aPrompt == "" ||
		!strings.Contains(aPrompt, "G06-FIXTURE-PRIVATE-A") ||
		!strings.Contains(aPrompt, "G06-FIXTURE-BASE-BODY") ||
		strings.Contains(aPrompt, "G06-FIXTURE-PRIVATE-B") {
		t.Fatal("A's Fixture prompt did not contain exactly A's authorized context")
	}
	if bPrompt := provider.promptAt(1); bPrompt == "" ||
		!strings.Contains(bPrompt, "G06-FIXTURE-PRIVATE-B") ||
		!strings.Contains(bPrompt, "Fixture proposed body") ||
		strings.Contains(bPrompt, "G06-FIXTURE-PRIVATE-A") {
		t.Fatal("B's Fixture prompt did not contain exactly B's authorized context")
	}
	if provider.requestCount() != 2 {
		t.Fatalf("fixture provider requests = %d, want 2", provider.requestCount())
	}

	// Human B approves the AI-proposed derive; the child is independent.
	approveStatus, approveBody, _ := b.call(t, http.MethodPost,
		"/api/v1/confirmations/"+confirmationID+"/approve",
		callOptions{IdempotencyKey: newIdempotencyKey()})
	requireStatus(t, approveStatus, http.StatusOK, approveBody)
	var child objectResponse
	if err := json.Unmarshal(approveBody, &child); err != nil {
		t.Fatalf("decode B child: %v", err)
	}
	if child.OwnerID != b.id.String() || child.Version != 1 {
		t.Fatalf("derived child = %+v", child)
	}
	if child.Lineage == nil || child.Lineage.Parent.ObjectID != object.ID ||
		child.Lineage.Parent.Version != 2 {
		t.Fatalf("child lineage = %+v, want parent version 2", child.Lineage)
	}

	// A cannot read B's private derived object.
	aChildStatus, aChildBody, _ := a.call(t, http.MethodGet, "/api/v1/objects/"+child.ID, callOptions{})
	requireStatus(t, aChildStatus, http.StatusNotFound, aChildBody)

	// Exactly one model invocation per user, attributed to that user, with the
	// Fixture provider and no cross-attribution.
	var aInvocations, bInvocations int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM model_invocations
 WHERE user_id = $1 AND provider = 'fixture' AND status = 'succeeded'`,
		a.id).Scan(&aInvocations); err != nil {
		t.Fatalf("count A invocations: %v", err)
	}
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM model_invocations
 WHERE user_id = $1 AND provider = 'fixture' AND status = 'succeeded'`,
		b.id).Scan(&bInvocations); err != nil {
		t.Fatalf("count B invocations: %v", err)
	}
	if aInvocations != 1 || bInvocations != 1 {
		t.Fatalf("invocations A=%d B=%d, want one each", aInvocations, bInvocations)
	}

	// Both human decisions are recorded in the audit chain with the AI
	// proposal/confirmation provenance.
	for _, action := range []string{
		"ai.message", "object.proposal_create", "object.proposal_apply",
		"ai.confirmation_approve", "object.derive",
	} {
		if got := env.count(t, `SELECT count(*) FROM audit_events WHERE action=$1`, action); got == 0 {
			t.Fatalf("missing audit action %s", action)
		}
	}
}
