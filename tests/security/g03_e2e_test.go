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

func TestG03TwoHumanTwoAIFixtureE2E(t *testing.T) {
	provider := &scriptedProvider{replies: []string{
		`{"reply":"A private summary","proposed_actions":[]}`,
		`{
		  "reply":"B proposes a body edit and a derive.",
		  "proposed_actions":[
		    {"capability_id":"object.propose_change","input":{"action":"set_body","input":{"body":"B via AI"},"expected_version":1}},
		    {"capability_id":"object.derive","input":{"title":"B child from handoff","inheritance_fields":["body"]}}
		  ]}`,
	}}
	env := newTestEnvWithProvider(t, func(cfg *config.Config) {
		cfg.Model.IncludePrivateContext = true
	}, provider)
	a := env.register(t, "g03e2e-a")
	b := env.register(t, "g03e2e-b")

	// Two independent Digital Selves, Personal AI profiles, and private data.
	if a.profileID(t) == b.profileID(t) {
		t.Fatal("two users received the same Personal AI profile")
	}
	a.setPrivateContext(t, "MARK-A E2E private")
	b.setPrivateContext(t, "MARK-B E2E private")
	aContextStatus, aContextBody, _ := a.call(t, http.MethodGet, "/api/v1/ai/context", callOptions{})
	requireStatus(t, aContextStatus, http.StatusOK, aContextBody)
	if !strings.Contains(string(aContextBody), "MARK-A") || strings.Contains(string(aContextBody), "MARK-B") {
		t.Fatalf("A context isolation failed: %s", aContextBody)
	}

	object := a.createObject(t, "collaborative", "E2E Living Object", map[string]any{
		"body": "A shared body",
	})
	aConversation := a.createConversation(t, "A private chat", &object.ID)
	aMessageStatus, aMessageBody := a.sendMessage(t, aConversation, "summarize my object")
	requireStatus(t, aMessageStatus, http.StatusOK, aMessageBody)
	if !strings.Contains(provider.promptAt(0), "MARK-A") ||
		strings.Contains(provider.promptAt(0), "MARK-B") {
		t.Fatal("A's AI prompt crossed private context boundaries")
	}

	invite := a.createInvitation(t, object.ID,
		[]string{"read", "contribute", "derive"}, 1)
	status, body := b.acceptInvitation(t, *invite.Token)
	requireStatus(t, status, http.StatusOK, body)

	// Handoff is available to B and excludes A's private context.
	handoffStatus, handoffBody, _ := b.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID+"/handoff", callOptions{})
	requireStatus(t, handoffStatus, http.StatusOK, handoffBody)
	if strings.Contains(string(handoffBody), "MARK-A") {
		t.Fatal("B handoff leaked A private context")
	}
	var handoff struct {
		PermittedActions []string `json:"permitted_actions"`
	}
	if err := json.Unmarshal(handoffBody, &handoff); err != nil {
		t.Fatalf("decode handoff: %v", err)
	}
	if len(handoff.PermittedActions) != 3 {
		t.Fatalf("B permitted actions = %v", handoff.PermittedActions)
	}

	// B's own AI runs against B's profile and the B-authorized projection.
	bConversation := b.createConversation(t, "B private chat", &object.ID)
	messageKey := newIdempotencyKey()
	origin := env.origin
	csrf := b.csrf
	messageStatus, messageBody, _ := b.call(t, http.MethodPost,
		"/api/v1/ai/conversations/"+bConversation+"/messages", callOptions{
			Origin: &origin, CSRFToken: &csrf, IdempotencyKey: messageKey,
			Body: map[string]any{"content": "propose a change"},
		})
	requireStatus(t, messageStatus, http.StatusOK, messageBody)
	prompt := provider.promptAt(1)
	if !strings.Contains(prompt, "MARK-B") || strings.Contains(prompt, "MARK-A") {
		t.Fatal("B's AI prompt crossed private context boundaries")
	}
	turn := decodeTurn(t, messageBody)
	if len(turn.ProposedActions) != 2 {
		t.Fatalf("B accepted actions = %d body=%s", len(turn.ProposedActions), messageBody)
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
		t.Fatalf("missing action ids: %s", messageBody)
	}
	// A must not see B's proposal input.
	aProposalStatus, aProposalBody, _ := a.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID+"/proposals", callOptions{})
	requireStatus(t, aProposalStatus, http.StatusOK, aProposalBody)
	if strings.Contains(string(aProposalBody), proposalID) ||
		strings.Contains(string(aProposalBody), "B via AI") {
		t.Fatal("A saw B's private AI proposal")
	}

	// Approved derive creates an independent child owned by B.
	approveKey := newIdempotencyKey()
	approveStatus, approveBody, _ := b.call(t, http.MethodPost,
		"/api/v1/confirmations/"+confirmationID+"/approve", callOptions{
			IdempotencyKey: approveKey,
		})
	requireStatus(t, approveStatus, http.StatusOK, approveBody)
	var child objectResponse
	if err := json.Unmarshal(approveBody, &child); err != nil {
		t.Fatalf("decode child: %v", err)
	}
	if child.OwnerID != b.id.String() || child.Version != 1 {
		t.Fatalf("child = %+v", child)
	}
	// Apply the proposal as B.
	applyStatus, applyBody, _ := b.call(t, http.MethodPost,
		"/api/v1/objects/"+object.ID+"/proposals/"+proposalID+"/apply", callOptions{
			Body: map[string]any{"expected_version": 1},
		})
	requireStatus(t, applyStatus, http.StatusOK, applyBody)
	var applied struct {
		Object objectResponse `json:"object"`
	}
	if err := json.Unmarshal(applyBody, &applied); err != nil {
		t.Fatalf("decode applied: %v", err)
	}
	if applied.Object.Version != 2 || applied.Object.State["body"] != "B via AI" {
		t.Fatalf("applied object = %+v", applied.Object)
	}

	// B sees the lineage of its child; A cannot see B's private child.
	bLineageStatus, bLineageBody, _ := b.call(t, http.MethodGet,
		"/api/v1/objects/"+child.ID+"/lineage", callOptions{})
	requireStatus(t, bLineageStatus, http.StatusOK, bLineageBody)
	if !strings.Contains(string(bLineageBody), object.ID) {
		t.Fatalf("child lineage missing parent: %s", bLineageBody)
	}
	aLineageStatus, aLineageBody, _ := a.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID+"/lineage", callOptions{})
	requireStatus(t, aLineageStatus, http.StatusOK, aLineageBody)
	if strings.Contains(string(aLineageBody), child.ID) {
		t.Fatal("parent owner saw B's private descendant")
	}

	// Private markers never enter object events or audit metadata.
	var privateLeak int
	for _, table := range []string{"object_events", "audit_events"} {
		if err := env.store.Pool().QueryRow(t.Context(),
			"SELECT count(*) FROM "+table+" WHERE "+table+"::text LIKE '%MARK-A%'").Scan(&privateLeak); err != nil {
			t.Fatalf("scan %s: %v", table, err)
		}
		if privateLeak != 0 {
			t.Fatalf("%s contains private context", table)
		}
	}

	// Revocation prevents all later object and AI access, and an old completed
	// idempotency key cannot replay a 200.
	removeStatus, removeBody, _ := a.call(t, http.MethodDelete,
		"/api/v1/objects/"+object.ID+"/members/"+b.id.String(), callOptions{})
	requireStatus(t, removeStatus, http.StatusNoContent, removeBody)
	revokedStatus, revokedBody, _ := b.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID+"/handoff", callOptions{})
	requireStatus(t, revokedStatus, http.StatusNotFound, revokedBody)
	replayStatus, replayBody, _ := b.call(t, http.MethodPost,
		"/api/v1/ai/conversations/"+bConversation+"/messages", callOptions{
			Origin: &origin, CSRFToken: &csrf, IdempotencyKey: messageKey,
			Body: map[string]any{"content": "propose a change"},
		})
	requireStatus(t, replayStatus, http.StatusNotFound, replayBody)
	approvalReplayStatus, approvalReplayBody, _ := b.call(t, http.MethodPost,
		"/api/v1/confirmations/"+confirmationID+"/approve", callOptions{
			Origin: &origin, CSRFToken: &csrf, IdempotencyKey: approveKey,
		})
	requireStatus(t, approvalReplayStatus, http.StatusNotFound, approvalReplayBody)

	// The full audit chain exists and is attributable.
	for _, action := range []string{
		"ai.message", "ai.handoff_issue", "object.proposal_create",
		"object.proposal_apply", "ai.confirmation_approve", "object.derive",
	} {
		if env.count(t, `SELECT count(*) FROM audit_events WHERE action=$1`, action) == 0 {
			t.Fatalf("missing audit action %s", action)
		}
	}
	var personalAIEvents int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM object_events
 WHERE event_type IN ('ai.proposal_created','object.derived')
   AND object_id IN ($1,$2)
   AND actor_type='personal_ai' AND actor_ai_profile_id IS NOT NULL
   AND delegation_id IS NOT NULL`, mustUUID(t, object.ID), mustUUID(t, child.ID)).Scan(&personalAIEvents); err != nil {
		t.Fatalf("query personal_ai events: %v", err)
	}
	if personalAIEvents != 2 {
		t.Fatalf("personal_ai events = %d, want 2", personalAIEvents)
	}
}
