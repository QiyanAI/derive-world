// SPDX-License-Identifier: Apache-2.0
package personalai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func proposalRawAction(t *testing.T, action string, input map[string]any,
	extra map[string]any) rawAction {
	t.Helper()
	payload := map[string]any{"action": action, "input": input}
	for key, value := range extra {
		payload[key] = value
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return rawAction{CapabilityID: "object.propose_change", Input: encoded}
}

func TestGroupTurnActionsCombinesCompatibleChanges(t *testing.T) {
	objectID := uuid.MustParse("01a00000-0000-7000-8000-000000000001")
	actions := []rawAction{
		proposalRawAction(t, "add_node", map[string]any{
			"kind_id": "01a00000-0000-7000-8000-000000000010", "name": "井边灯",
		}, nil),
		proposalRawAction(t, "add_node", map[string]any{
			"kind_id": "01a00000-0000-7000-8000-000000000010", "name": "院角灯",
		}, nil),
		proposalRawAction(t, "set_premise", map[string]any{
			"premise": "小院在夜里慢慢亮起来。",
		}, nil),
	}
	grouped, away := groupTurnActions(actions, objectID, 3, true)
	if len(grouped) != 1 || away != 2 {
		t.Fatalf("grouped=%d away=%d", len(grouped), away)
	}
	var proposal proposalActionInput
	if err := decodeSingleJSON(string(grouped[0].Input), &proposal); err != nil {
		t.Fatalf("decode grouped action: %v", err)
	}
	if proposal.Action != "batch_change" {
		t.Fatalf("action = %q", proposal.Action)
	}
	var payload struct {
		Note    string            `json:"note"`
		Actions []json.RawMessage `json:"actions"`
	}
	if err := json.Unmarshal(proposal.Input, &payload); err != nil {
		t.Fatalf("decode batch input: %v", err)
	}
	if len(payload.Actions) != 3 {
		t.Fatalf("batch actions = %d, want 3", len(payload.Actions))
	}
	if !strings.Contains(payload.Note, "井边灯") || !strings.Contains(payload.Note, "院角灯") {
		t.Fatalf("note lost human names: %q", payload.Note)
	}
	if strings.Contains(payload.Note, "01a00000") {
		t.Fatalf("note leaked an id: %q", payload.Note)
	}
}

func TestGroupTurnActionsKeepsMismatchedVersionSeparate(t *testing.T) {
	objectID := uuid.MustParse("01a00000-0000-7000-8000-000000000001")
	actions := []rawAction{
		proposalRawAction(t, "add_node", map[string]any{
			"kind_id": "01a00000-0000-7000-8000-000000000010", "name": "One",
		}, nil),
		proposalRawAction(t, "add_node", map[string]any{
			"kind_id": "01a00000-0000-7000-8000-000000000010", "name": "Two",
		}, nil),
		proposalRawAction(t, "set_premise", map[string]any{"premise": "stale"},
			map[string]any{"expected_version": 2}),
	}
	grouped, away := groupTurnActions(actions, objectID, 3, true)
	if len(grouped) != 2 || away != 1 {
		t.Fatalf("grouped=%d away=%d; the stale action must stay separate", len(grouped), away)
	}
	var stale proposalActionInput
	last := grouped[len(grouped)-1]
	if err := decodeSingleJSON(string(last.Input), &stale); err != nil {
		t.Fatal(err)
	}
	if stale.ExpectedVersion == nil || *stale.ExpectedVersion != 2 {
		t.Fatalf("stale action lost its expected_version: %#v", stale)
	}
}

func TestGroupTurnActionsLeavesOneChangeAlone(t *testing.T) {
	objectID := uuid.MustParse("01a00000-0000-7000-8000-000000000001")
	actions := []rawAction{
		proposalRawAction(t, "set_premise", map[string]any{"premise": "one"}, nil),
	}
	grouped, away := groupTurnActions(actions, objectID, 1, false)
	if len(grouped) != 1 || away != 0 {
		t.Fatalf("grouped=%d away=%d", len(grouped), away)
	}
	if string(grouped[0].Input) != string(actions[0].Input) {
		t.Fatal("a single change was rewritten")
	}
}
