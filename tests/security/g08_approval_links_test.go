// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const linkedWorldReply = `{"reply":"I prepared a linked world plan.","proposed_actions":[{"capability_id":"world.create","input":{"title":"Linked World","brief":{"premise":"A small world that proves where its approval belongs.","kinds":[{"name":"Thing"}],"things":[{"kind":0,"name":"One thing"}],"experience":{"surfaces":["live"],"primary":"live","live":{"avatar":0,"speed":1.8}}}}}]}`

const linkedProposalReply = `{"reply":"I propose one body edit.","proposed_actions":[{"capability_id":"object.propose_change","input":{"action":"set_body","input":{"body":"linked body"}}}]}`

const linked3DReply = `{"reply":"I prepared a 3D street with sound.","proposed_actions":[{"capability_id":"world.create","input":{"title":"Linked 3D Street","brief":{"premise":"A short 3D street at dusk with a lamp and evening sound.","kinds":[{"name":"Lamp"}],"things":[{"kind":0,"name":"One lamp"}],"experience":{"surfaces":["scene3d","media"],"primary":"scene3d","scene3d":{"player":0,"views":[{"kind":0,"primitive":"sphere"}]},"media":{"title":"Street photo","kind":"image"},"audio":{"title":"Evening","volume":0.3}}}}}]}`

type linkedConfirmation struct {
	ID               string `json:"id"`
	Action           string `json:"action"`
	Status           string `json:"status"`
	ConversationID   string `json:"conversation_id"`
	MessageID        string `json:"message_id"`
	ResultObjectID   string `json:"result_object_id"`
	ResultObjectType string `json:"result_object_type"`
	Effect           *struct {
		Surfaces []string `json:"surfaces"`
		Primary  string   `json:"primary_surface"`
	} `json:"effect"`
}

type linkedProposal struct {
	ID              string `json:"id"`
	ConversationID  string `json:"conversation_id"`
	SourceMessageID string `json:"source_message_id"`
	Action          string `json:"action"`
	Status          string `json:"status"`
}

func decodeLinkedConfirmations(t *testing.T, body []byte) []linkedConfirmation {
	t.Helper()
	var list struct {
		Items []linkedConfirmation `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode confirmations: %v body=%s", err, body)
	}
	return list.Items
}

func findConfirmation(items []linkedConfirmation, id string) *linkedConfirmation {
	for index := range items {
		if items[index].ID == id {
			return &items[index]
		}
	}
	return nil
}

// TestApprovalLinksToTurnAndResult proves the end-to-end contract behind the
// inline approval: every AI-originated confirmation or proposal records the
// exact conversation and assistant message that produced it, an approved
// creation records its real result object, one conversation cannot hold two
// pending world plans, and a legacy row without links is still listable.
func TestApprovalLinksToTurnAndResult(t *testing.T) {
	provider := &scriptedProvider{replies: []string{
		linkedWorldReply,
		linkedWorldReply,
		deriveReply("Linked Child"),
		linkedProposalReply,
		linked3DReply,
		linkedWorldReply,
		linkedWorldReply,
	}}
	env := newTestEnvWithProvider(t, nil, provider)
	a := env.register(t, "g08links")

	// --- World creation: link, duplicate guard, result object -------------
	conversation := a.createConversation(t, "Link conversation", nil)
	status, body := a.sendMessage(t, conversation, "Prepare a linked world")
	requireStatus(t, status, http.StatusOK, body)
	turn := decodeTurn(t, body)
	if len(turn.ProposedActions) != 1 || turn.ProposedActions[0].Kind != "confirmation" {
		t.Fatalf("expected one confirmation: %s", body)
	}
	confirmationID := turn.ProposedActions[0].ConfirmationID
	assistantID := turn.AssistantMessage.ID
	if assistantID == "" {
		t.Fatalf("assistant message id missing: %s", body)
	}

	// A second world plan in the same conversation is refused before any row
	// exists, so the user never sees two confirm buttons with one meaning.
	status, body = a.sendMessage(t, conversation, "Prepare another linked world")
	requireStatus(t, status, http.StatusOK, body)
	duplicate := decodeTurn(t, body)
	if len(duplicate.ProposedActions) != 0 {
		t.Fatalf("second world plan was accepted: %s", body)
	}
	if len(duplicate.Rejections) == 0 || duplicate.Rejections[0].Stage != "world_pending" {
		t.Fatalf("expected world_pending rejection: %s", body)
	}

	status, body, _ = a.call(t, http.MethodGet,
		"/api/v1/confirmations?status=all&conversation_id="+conversation, callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	linked := findConfirmation(decodeLinkedConfirmations(t, body), confirmationID)
	if linked == nil {
		t.Fatalf("confirmation missing from its conversation: %s", body)
	}
	if linked.ConversationID != conversation {
		t.Fatalf("conversation_id = %q, want %q", linked.ConversationID, conversation)
	}
	if linked.MessageID != assistantID {
		t.Fatalf("message_id = %q, want %q", linked.MessageID, assistantID)
	}

	status, body, _ = a.call(t, http.MethodPost,
		"/api/v1/confirmations/"+confirmationID+"/approve", callOptions{
			IdempotencyKey: newIdempotencyKey(),
		})
	requireStatus(t, status, http.StatusOK, body)
	var created objectResponse
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode created world: %v", err)
	}
	status, body, _ = a.call(t, http.MethodGet,
		"/api/v1/confirmations/"+confirmationID, callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	var approved linkedConfirmation
	if err := json.Unmarshal(body, &approved); err != nil {
		t.Fatalf("decode approved confirmation: %v", err)
	}
	if approved.Status != "approved" {
		t.Fatalf("status = %q, want approved", approved.Status)
	}
	if approved.ResultObjectID != created.ID {
		t.Fatalf("result_object_id = %q, want %q", approved.ResultObjectID, created.ID)
	}

	// --- Derivation: the capability-backed child is the result object -----
	object := a.createObject(t, "collaborative", "Link parent", map[string]any{
		"body": "parent body",
	})
	bound := a.createConversation(t, "Bound link conversation", &object.ID)
	status, body = a.sendMessage(t, bound, "Fork this object")
	requireStatus(t, status, http.StatusOK, body)
	turn = decodeTurn(t, body)
	if len(turn.ProposedActions) != 1 || turn.ProposedActions[0].Kind != "confirmation" {
		t.Fatalf("expected one derive confirmation: %s", body)
	}
	deriveID := turn.ProposedActions[0].ConfirmationID
	deriveAssistantID := turn.AssistantMessage.ID

	status, body, _ = a.call(t, http.MethodGet,
		"/api/v1/confirmations?status=all&conversation_id="+bound, callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	linkedDerive := findConfirmation(decodeLinkedConfirmations(t, body), deriveID)
	if linkedDerive == nil || linkedDerive.MessageID != deriveAssistantID {
		t.Fatalf("derive confirmation not linked to its reply: %s", body)
	}

	status, body, _ = a.call(t, http.MethodPost,
		"/api/v1/confirmations/"+deriveID+"/approve", callOptions{
			IdempotencyKey: newIdempotencyKey(),
		})
	requireStatus(t, status, http.StatusOK, body)
	var child objectResponse
	if err := json.Unmarshal(body, &child); err != nil {
		t.Fatalf("decode derived child: %v", err)
	}
	status, body, _ = a.call(t, http.MethodGet,
		"/api/v1/confirmations/"+deriveID, callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	var approvedDerive linkedConfirmation
	if err := json.Unmarshal(body, &approvedDerive); err != nil {
		t.Fatalf("decode approved derive: %v", err)
	}
	if approvedDerive.ResultObjectID != child.ID {
		t.Fatalf("derive result_object_id = %q, want %q", approvedDerive.ResultObjectID, child.ID)
	}

	// --- Proposal: linked to the same turn --------------------------------
	status, body = a.sendMessage(t, bound, "Edit the body")
	requireStatus(t, status, http.StatusOK, body)
	turn = decodeTurn(t, body)
	if len(turn.ProposedActions) != 1 || turn.ProposedActions[0].Kind != "proposal" {
		t.Fatalf("expected one proposal: %s", body)
	}
	proposalID := turn.ProposedActions[0].ProposalID
	status, body, _ = a.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID+"/proposals", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	var proposals struct {
		Items []linkedProposal `json:"items"`
	}
	if err := json.Unmarshal(body, &proposals); err != nil {
		t.Fatalf("decode proposals: %v", err)
	}
	var found *linkedProposal
	for index := range proposals.Items {
		if proposals.Items[index].ID == proposalID {
			found = &proposals.Items[index]
			break
		}
	}
	if found == nil {
		t.Fatalf("proposal missing: %s", body)
	}
	if found.ConversationID != bound || found.SourceMessageID != turn.AssistantMessage.ID {
		t.Fatalf("proposal links = %q/%q, want %q/%q",
			found.ConversationID, found.SourceMessageID, bound, turn.AssistantMessage.ID)
	}

	// --- A 3D + media + sound world keeps its real surfaces ---------------
	streetConversation := a.createConversation(t, "Street conversation", nil)
	status, body = a.sendMessage(t, streetConversation, "Make a 3D street with evening sound")
	requireStatus(t, status, http.StatusOK, body)
	streetTurn := decodeTurn(t, body)
	if len(streetTurn.ProposedActions) != 1 || streetTurn.ProposedActions[0].Kind != "confirmation" {
		t.Fatalf("expected one 3D confirmation: %s", body)
	}
	streetID := streetTurn.ProposedActions[0].ConfirmationID
	status, body, _ = a.call(t, http.MethodGet,
		"/api/v1/confirmations?status=all&conversation_id="+streetConversation, callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	street := findConfirmation(decodeLinkedConfirmations(t, body), streetID)
	if street == nil || street.Effect == nil {
		t.Fatalf("3D confirmation preview missing: %s", body)
	}
	joined := strings.Join(street.Effect.Surfaces, ",")
	for _, surface := range []string{"scene3d", "media", "audio"} {
		if !strings.Contains(joined, surface) {
			t.Fatalf("surface %q missing from %v", surface, street.Effect.Surfaces)
		}
	}
	if street.Effect.Primary != "scene3d" {
		t.Fatalf("primary surface = %q, want scene3d", street.Effect.Primary)
	}

	// --- Legacy rows stay listable without the new fields -----------------
	if _, err := env.store.Pool().Exec(t.Context(), `
UPDATE confirmations SET conversation_id = NULL, message_id = NULL WHERE id = $1`,
		mustUUID(t, deriveID)); err != nil {
		t.Fatalf("clear links: %v", err)
	}
	status, body, _ = a.call(t, http.MethodGet,
		"/api/v1/confirmations?status=all&conversation_id="+bound, callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if findConfirmation(decodeLinkedConfirmations(t, body), deriveID) != nil {
		t.Fatalf("legacy row appeared inside the conversation scope: %s", body)
	}
	status, body, _ = a.call(t, http.MethodGet, "/api/v1/confirmations?status=all", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	legacy := findConfirmation(decodeLinkedConfirmations(t, body), deriveID)
	if legacy == nil {
		t.Fatalf("legacy row disappeared from the global list: %s", body)
	}
	if legacy.ConversationID != "" || legacy.MessageID != "" {
		t.Fatalf("legacy row exposed links: %+v", legacy)
	}
	if legacy.ResultObjectID != child.ID {
		t.Fatalf("legacy row lost its real result: %+v", legacy)
	}

	// --- The schema itself rejects half-links and cross-conversation links --
	if _, err := env.store.Pool().Exec(t.Context(), `
UPDATE confirmations SET conversation_id = $1, message_id = NULL WHERE id = $2`,
		mustUUID(t, conversation), mustUUID(t, deriveID)); err == nil {
		t.Fatal("half-linked confirmation was accepted")
	}
	otherConversation := a.createConversation(t, "Other conversation", nil)
	if _, err := env.store.Pool().Exec(t.Context(), `
UPDATE confirmations SET conversation_id = $1, message_id = $2 WHERE id = $3`,
		mustUUID(t, otherConversation), mustUUID(t, assistantID), mustUUID(t, deriveID)); err == nil {
		t.Fatal("cross-conversation message link was accepted")
	}

	// --- A past-window plan no longer blocks a fresh plan -----------------
	// Use a second user so the AI burst limiter sees a fresh bucket; the
	// default integration configuration allows five immediate AI requests.
	expiryUser := env.register(t, "g08expiry")
	expiryConversation := expiryUser.createConversation(t, "Expiry conversation", nil)
	status, body = expiryUser.sendMessage(t, expiryConversation, "Prepare a linked world")
	requireStatus(t, status, http.StatusOK, body)
	firstPlan := decodeTurn(t, body)
	if len(firstPlan.ProposedActions) != 1 || firstPlan.ProposedActions[0].Kind != "confirmation" {
		t.Fatalf("expected one pending plan: %s", body)
	}
	firstPlanID := firstPlan.ProposedActions[0].ConfirmationID
	// Move the whole window into the past; the schema keeps expires_at within
	// five minutes of created_at, so both columns move together.
	if _, err := env.store.Pool().Exec(t.Context(), `
UPDATE confirmations
   SET created_at = now() - interval '10 minutes',
       expires_at = now() - interval '5 minutes'
 WHERE id = $1`, mustUUID(t, firstPlanID)); err != nil {
		t.Fatalf("expire the first plan: %v", err)
	}
	status, body = expiryUser.sendMessage(t, expiryConversation, "Prepare a linked world")
	requireStatus(t, status, http.StatusOK, body)
	freshPlan := decodeTurn(t, body)
	if len(freshPlan.ProposedActions) != 1 || freshPlan.ProposedActions[0].Kind != "confirmation" {
		t.Fatalf("expired plan still blocked a fresh plan: %s", body)
	}
}
