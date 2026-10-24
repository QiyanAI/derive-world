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

	"github.com/QiyanAI/derive-world/internal/config"
	"github.com/QiyanAI/derive-world/internal/model"
)

type scriptedProvider struct {
	mu       sync.Mutex
	replies  []string
	errs     []error
	requests []model.Request
}

func (p *scriptedProvider) Mode() string { return "fixture" }

func (p *scriptedProvider) requestCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

func (p *scriptedProvider) Complete(_ context.Context, request model.Request) (model.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, request)
	reply := `{"reply":"scripted fallback","proposed_actions":[]}`
	if len(p.replies) > 0 {
		reply = p.replies[0]
		p.replies = p.replies[1:]
	} else if len(p.errs) > 0 {
		err := p.errs[0]
		p.errs = p.errs[1:]
		return model.Response{}, err
	}
	return model.Response{
		Text: reply, Model: "scripted-test-double",
		Usage: model.Usage{InputTokens: 10, OutputTokens: 10},
	}, nil
}

func (p *scriptedProvider) lastPrompt() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.requests) == 0 {
		return ""
	}
	return renderPrompt(p.requests[len(p.requests)-1])
}

func (p *scriptedProvider) promptAt(index int) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index < 0 || index >= len(p.requests) {
		return ""
	}
	return renderPrompt(p.requests[index])
}

func renderPrompt(request model.Request) string {
	var builder strings.Builder
	builder.WriteString(request.System)
	for _, message := range request.Messages {
		builder.WriteString("\n")
		builder.WriteString(message.Content)
	}
	return builder.String()
}

type turnResponse struct {
	AssistantMessage struct {
		Content     string `json:"content"`
		ParseStatus string `json:"parse_status"`
	} `json:"assistant_message"`
	ParseStatus     string `json:"parse_status"`
	ContextStale    bool   `json:"context_stale"`
	ProposedActions []struct {
		Kind           string `json:"kind"`
		ProposalID     string `json:"proposal_id"`
		ConfirmationID string `json:"confirmation_id"`
		Action         string `json:"action"`
	} `json:"proposed_actions"`
}

func decodeTurn(t *testing.T, body []byte) turnResponse {
	t.Helper()
	var turn turnResponse
	if err := json.Unmarshal(body, &turn); err != nil {
		t.Fatalf("decode turn: %v body=%s", err, body)
	}
	return turn
}

func TestG03AITurnCreatesProposalAndConfirmedDerive(t *testing.T) {
	provider := &scriptedProvider{replies: []string{`{
      "reply":"I can update the body and derive a child.",
      "proposed_actions":[
        {"capability_id":"object.propose_change","input":{"action":"set_body","input":{"body":"AI proposed body"},"expected_version":1}},
        {"capability_id":"object.derive","input":{"title":"B derived child","inheritance_fields":["body"]}}
      ]}`}}
	env := newTestEnvWithProvider(t, func(cfg *config.Config) {
		cfg.Model.IncludePrivateContext = true
	}, provider)
	a := env.register(t, "g03ai-a")
	b := env.register(t, "g03ai-b")
	a.setPrivateContext(t, "MARK-A private only")
	b.setPrivateContext(t, "MARK-B private only")

	object := a.createObject(t, "collaborative", "AI proposal object", map[string]any{
		"body": "original body",
	})
	invitation := a.createInvitation(t, object.ID, []string{"read", "contribute", "derive"}, 1)
	status, body := b.acceptInvitation(t, *invitation.Token)
	requireStatus(t, status, http.StatusOK, body)

	conversationID := b.createConversation(t, "AI proposal chat", &object.ID)
	messageStatus, messageBody := b.sendMessage(t, conversationID, "please update and derive")
	requireStatus(t, messageStatus, http.StatusOK, messageBody)
	turn := decodeTurn(t, messageBody)
	if turn.ParseStatus != "structured" {
		t.Fatalf("parse_status = %q body=%s", turn.ParseStatus, messageBody)
	}
	if len(turn.ProposedActions) != 2 {
		t.Fatalf("accepted actions = %d body=%s", len(turn.ProposedActions), messageBody)
	}
	var proposalID, confirmationID string
	for _, action := range turn.ProposedActions {
		switch action.Kind {
		case "proposal":
			proposalID = action.ProposalID
		case "confirmation":
			confirmationID = action.ConfirmationID
		}
	}
	if proposalID == "" || confirmationID == "" {
		t.Fatalf("missing proposal/confirmation ids: %s", messageBody)
	}
	prompt := provider.lastPrompt()
	if strings.Contains(prompt, "MARK-A") {
		t.Fatal("B's AI prompt included A's private context")
	}
	if !strings.Contains(prompt, "MARK-B") {
		t.Fatal("B's AI prompt did not include B's own private context")
	}
	if strings.Contains(prompt, "ignore previous instructions") {
		t.Fatal("unexpected test prompt marker")
	}

	var (
		proposalStatus string
		proposalInput  string
		eventType      string
		eventActor     string
	)
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT p.status, p.input::text, e.event_type, e.actor_type
  FROM object_proposals p
  JOIN object_events e ON e.event_type = 'ai.proposal_created'
 WHERE p.id = $1`, mustUUID(t, proposalID)).Scan(
		&proposalStatus, &proposalInput, &eventType, &eventActor); err != nil {
		t.Fatalf("query proposal: %v", err)
	}
	if proposalStatus != "pending" || eventType != "ai.proposal_created" || eventActor != "personal_ai" {
		t.Fatalf("proposal state=%s event=%s actor=%s", proposalStatus, eventType, eventActor)
	}
	if strings.Contains(proposalInput, "AI proposed body") == false {
		t.Fatal("proposal did not retain the AI input for its actor")
	}

	// A can list only A's own proposals; B's input must not be visible.
	aProposalsStatus, aProposalsBody, _ := a.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID+"/proposals", callOptions{})
	requireStatus(t, aProposalsStatus, http.StatusOK, aProposalsBody)
	if strings.Contains(string(aProposalsBody), proposalID) {
		t.Fatal("another user's proposal id leaked through list")
	}

	// Approve the derive while the parent is still at version 1.
	approveStatus, approveBody, _ := b.call(t, http.MethodPost,
		"/api/v1/confirmations/"+confirmationID+"/approve", callOptions{
			IdempotencyKey: newIdempotencyKey(),
		})
	requireStatus(t, approveStatus, http.StatusOK, approveBody)
	var child objectResponse
	if err := json.Unmarshal(approveBody, &child); err != nil {
		t.Fatalf("decode child: %v", err)
	}
	if child.CreatorID != b.id.String() || child.OwnerID != b.id.String() || child.Version != 1 {
		t.Fatalf("unexpected child: %+v", child)
	}
	if child.Lineage == nil || child.Lineage.Parent.ObjectID != object.ID ||
		child.Lineage.Parent.Version != 1 {
		t.Fatalf("child lineage = %+v", child.Lineage)
	}
	var deriveActor string
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT actor_type FROM object_events
 WHERE object_id = $1 AND event_type = 'object.derived'`,
		mustUUID(t, child.ID)).Scan(&deriveActor); err != nil {
		t.Fatalf("query derive actor: %v", err)
	}
	if deriveActor != "personal_ai" {
		t.Fatalf("derive event actor = %q", deriveActor)
	}

	// Apply the proposal as the human actor.
	applyStatus, applyBody, _ := b.call(t, http.MethodPost,
		"/api/v1/objects/"+object.ID+"/proposals/"+proposalID+"/apply", callOptions{
			Body: map[string]any{"expected_version": 1},
		})
	requireStatus(t, applyStatus, http.StatusOK, applyBody)
	var applied struct {
		Proposal struct {
			Status string `json:"status"`
		} `json:"proposal"`
		Object objectResponse `json:"object"`
	}
	if err := json.Unmarshal(applyBody, &applied); err != nil {
		t.Fatalf("decode apply: %v", err)
	}
	if applied.Proposal.Status != "applied" || applied.Object.Version != 2 {
		t.Fatalf("applied response = %+v", applied)
	}
	if applied.Object.State["body"] != "AI proposed body" {
		t.Fatalf("body not applied: %v", applied.Object.State["body"])
	}
	// Duplicate apply is idempotent and does not create version 3.
	duplicateStatus, duplicateBody, _ := b.call(t, http.MethodPost,
		"/api/v1/objects/"+object.ID+"/proposals/"+proposalID+"/apply", callOptions{
			Body: map[string]any{"expected_version": 1},
		})
	requireStatus(t, duplicateStatus, http.StatusOK, duplicateBody)
	var duplicated struct {
		Object objectResponse `json:"object"`
	}
	if err := json.Unmarshal(duplicateBody, &duplicated); err != nil {
		t.Fatalf("decode duplicate apply: %v", err)
	}
	if duplicated.Object.Version != 2 {
		t.Fatalf("duplicate apply changed version to %d", duplicated.Object.Version)
	}

	// The parent remains at version 2 even though the derive occurred while it
	// was version 1; derive never writes the parent.
	parentStatus, parentBody, _ := a.call(t, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, parentStatus, http.StatusOK, parentBody)
	var parent objectResponse
	if err := json.Unmarshal(parentBody, &parent); err != nil {
		t.Fatalf("decode parent: %v", err)
	}
	if parent.Version != 2 {
		t.Fatalf("parent version = %d", parent.Version)
	}
}
