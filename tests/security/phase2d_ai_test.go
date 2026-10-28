// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/QiyanAI/derive-world/internal/config"
)

func TestPhase2DAICreateBriefReimagineAndCustomAction(t *testing.T) {
	createReply := `{"reply":"A playable forest. Nothing exists until you approve it.","proposed_actions":[{"capability_id":"world.create","input":{"title":"Phase 2D forest","brief":{"premise":"A forest that remembers every light.","kinds":[{"name":"Traveler","form":"M 0 -22 A 22 22 0 1 1 0 22 A 22 22 0 1 1 0 -22 Z","hue":180},{"name":"Light","form":"M 0 -16 L 11 0 L 0 16 L -11 0 Z","hue":48,"motion":"pulse"}],"things":[{"kind":0,"name":"Traveler","text":"She carries a lantern.","x":0.5,"y":0.5},{"kind":1,"name":"First light","text":"It remembers dawn.","x":0.56,"y":0.5}],"experience":{"primary":"live","live":{"avatar":0,"goal":{"type":"collect","kind":1,"count":1,"label":"Gather the light"},"particles":{"kind":"motes","count":12}}}}}}]}`
	reimagineReply := `{"reply":"I turned it into a playable star instrument. Nothing changes until you apply the proposal.","proposed_actions":[{"capability_id":"world.reimagine","input":{"summary":"Turn the world into a star instrument","brief":{"experience":{"surfaces":["custom"],"primary":"custom","custom":{"title":"Star instrument","width":1200,"height":800,"background":{"mode":"gradient","hue":240,"hue2":265,"lightness":8,"accent_hue":42},"vars":{"notes":0,"pad_a":false},"nodes":[{"id":"pad_a","type":"dot","x":420,"y":420,"r":56,"hue":42,"on_click":[{"type":"toggle_var","name":"pad_a"},{"type":"sound","freq":440,"duration":0.3,"wave":"sine"},{"type":"if","cond":{"var":"pad_a","op":"truthy"},"then":[{"type":"add_var","name":"notes","value":1}]}]},{"id":"readout","type":"text","x":420,"y":620,"text":"notes","size":28,"bind_text":{"var":"notes"}}]}}}}}]}`
	provider := &scriptedProvider{replies: []string{createReply, reimagineReply}}
	env := newTestEnvWithProvider(t, func(cfg *config.Config) {}, provider)
	owner := env.register(t, "p2dai")

	conversationID := owner.createConversation(t, "Phase 2D creation", nil)
	status, body := owner.sendMessage(t, conversationID, "Create a world that is a playable forest")
	requireStatus(t, status, http.StatusOK, body)
	turn := decodeTurn(t, body)
	if turn.ParseStatus != "structured" || len(turn.ProposedActions) != 1 {
		t.Fatalf("create turn: %s", body)
	}
	if turn.ProposedActions[0].Action != "world.create" || turn.ProposedActions[0].ConfirmationID == "" {
		t.Fatalf("expected a world.create confirmation: %s", body)
	}
	approveStatus, approveBody, _ := owner.call(t, http.MethodPost,
		"/api/v1/confirmations/"+turn.ProposedActions[0].ConfirmationID+"/approve",
		callOptions{IdempotencyKey: newIdempotencyKey()})
	requireStatus(t, approveStatus, http.StatusOK, approveBody)
	var created objectResponse
	if err := json.Unmarshal(approveBody, &created); err != nil {
		t.Fatalf("decode created world: %v", err)
	}
	var createdState struct {
		Experience struct {
			Root struct {
				Capability string `json:"capability"`
			} `json:"root"`
		} `json:"experience"`
	}
	createdRaw, _ := json.Marshal(created.State)
	if err := json.Unmarshal(createdRaw, &createdState); err != nil {
		t.Fatalf("decode created state: %v", err)
	}
	if createdState.Experience.Root.Capability != "core.live" {
		t.Fatalf("created root = %q", createdState.Experience.Root.Capability)
	}

	boundID := owner.createConversation(t, "Phase 2D reshape", &created.ID)
	status, body = owner.sendMessage(t, boundID, "Turn this world into a playable star instrument")
	requireStatus(t, status, http.StatusOK, body)
	turn = decodeTurn(t, body)
	if turn.ParseStatus != "structured" || len(turn.ProposedActions) != 1 {
		t.Fatalf("reshape turn: %s", body)
	}
	if turn.ProposedActions[0].Kind != "proposal" || turn.ProposedActions[0].Action != "reshape_world" {
		t.Fatalf("expected reshape proposal: %s", body)
	}
	applyStatus, applyBody := applyProposal(t, owner, created.ID,
		turn.ProposedActions[0].ProposalID, created.Version)
	requireStatus(t, applyStatus, http.StatusOK, applyBody)
	var applied struct {
		Object objectResponse `json:"object"`
	}
	if err := json.Unmarshal(applyBody, &applied); err != nil {
		t.Fatalf("decode reshaped: %v", err)
	}
	reshaped := applied.Object
	var reshapedState struct {
		Variables  map[string]any `json:"variables"`
		Experience struct {
			Root struct {
				Capability string `json:"capability"`
			} `json:"root"`
		} `json:"experience"`
	}
	reshapedRaw, _ := json.Marshal(reshaped.State)
	if err := json.Unmarshal(reshapedRaw, &reshapedState); err != nil {
		t.Fatalf("decode reshaped state: %v", err)
	}
	if reshapedState.Experience.Root.Capability != "core.custom" {
		t.Fatalf("reshaped root = %q", reshapedState.Experience.Root.Capability)
	}
	if reshapedState.Variables["notes"] != float64(0) {
		t.Fatalf("custom vars = %#v", reshapedState.Variables)
	}

	status, body, _ = owner.call(t, http.MethodPost,
		"/api/v1/objects/"+created.ID+"/actions", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"action": "custom_action",
				"input": map[string]any{
					"surface_id": "root", "event": "click", "node_id": "pad_a",
				},
				"expected_version": reshaped.Version,
			},
		})
	requireStatus(t, status, http.StatusOK, body)
	var after objectResponse
	if err := json.Unmarshal(body, &after); err != nil {
		t.Fatalf("decode custom action: %v", err)
	}
	var afterState struct {
		Variables map[string]any `json:"variables"`
	}
	afterRaw, _ := json.Marshal(after.State)
	if err := json.Unmarshal(afterRaw, &afterState); err != nil {
		t.Fatalf("decode after state: %v", err)
	}
	if afterState.Variables["notes"] != float64(1) || afterState.Variables["pad_a"] != true {
		t.Fatalf("custom action variables = %#v", afterState.Variables)
	}
}
