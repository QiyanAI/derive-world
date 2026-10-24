// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestValidateCreateCollaborativeNormalizes(t *testing.T) {
	stateRaw, sharedRaw, err := ValidateCreate(CreateInput{
		ObjectType: TypeCollaborative,
		Title:      "  Launch  ",
		Content: json.RawMessage(`{
			"summary":"s","body":"b",
			"checklist":[{"text":"one","done":true},{"text":"two"}],
			"tags":["zeta","alpha","zeta"]
		}`),
		SharedContext: json.RawMessage(`{"note":"shared"}`),
	})
	if err != nil {
		t.Fatalf("ValidateCreate: %v", err)
	}
	var state CollaborativeState
	if err := json.Unmarshal(stateRaw, &state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if state.Title != "Launch" {
		t.Fatalf("title = %q", state.Title)
	}
	if len(state.Checklist) != 2 || !state.Checklist[0].Done {
		t.Fatalf("checklist = %+v", state.Checklist)
	}
	for _, item := range state.Checklist {
		parsed, err := uuid.Parse(item.ID)
		if err != nil || parsed.Version() != 7 {
			t.Fatalf("checklist id %q is not UUIDv7", item.ID)
		}
	}
	if strings.Join(state.Tags, ",") != "alpha,zeta" {
		t.Fatalf("tags = %v", state.Tags)
	}
	if string(sharedRaw) != `{"note":"shared"}` {
		t.Fatalf("shared context = %s", sharedRaw)
	}
}

func TestValidateCreateInteractiveDefaults(t *testing.T) {
	stateRaw, _, err := ValidateCreate(CreateInput{
		ObjectType: TypeInteractive,
		Title:      "Poll",
		Content:    json.RawMessage(`{"prompt":"p","options":[{"label":"A"},{"label":"B"}]}`),
	})
	if err != nil {
		t.Fatalf("ValidateCreate: %v", err)
	}
	var state InteractiveState
	if err := json.Unmarshal(stateRaw, &state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if state.Status != "open" || len(state.Options) != 2 || len(state.Votes) != 2 {
		t.Fatalf("state = %+v", state)
	}
	for _, option := range state.Options {
		if state.Votes[option.ID] != 0 {
			t.Fatalf("vote for %s = %d", option.ID, state.Votes[option.ID])
		}
	}
}

func TestValidateCreateRejectsInvalidInput(t *testing.T) {
	cases := []CreateInput{
		{ObjectType: "other", Title: "x"},
		{ObjectType: TypeCollaborative, Title: ""},
		{ObjectType: TypeInteractive, Title: "x", Content: json.RawMessage(`{"options":[{"label":"only"}]}`)},
		{ObjectType: TypeCollaborative, Title: "x", SharedContext: json.RawMessage(`null`)},
		{ObjectType: TypeCollaborative, Title: "x", SharedContext: json.RawMessage(`[]`)},
		{ObjectType: TypeCollaborative, Title: "x", Content: json.RawMessage(`{"unknown":1}`)},
		{ObjectType: TypeCollaborative, Title: "x", Content: json.RawMessage(`null`)},
		{ObjectType: TypeCollaborative, Title: "x", Content: json.RawMessage(`{"summary":""} trailing`)},
	}
	for index, input := range cases {
		if _, _, err := ValidateCreate(input); err == nil {
			t.Fatalf("case %d was accepted", index)
		}
	}
}

func TestValidateStateBytesRejectsSchemaViolations(t *testing.T) {
	interactive := `{"title":"p","prompt":"","options":[{"id":"0192f0b2-1a2b-7c3d-8e4f-5a6b7c8d9e0f","label":"A"}],"votes":{},"status":"open"}`
	if err := ValidateStateBytes(TypeInteractive, []byte(interactive)); err == nil {
		t.Fatal("interactive state with one option was accepted")
	}
	extra := `{"title":"p","summary":"","body":"","checklist":[],"tags":[],"extra":true}`
	if err := ValidateStateBytes(TypeCollaborative, []byte(extra)); err == nil {
		t.Fatal("unknown state property was accepted")
	}
	mismatch := `{"title":"p","prompt":"","options":[{"id":"0192f0b2-1a2b-7c3d-8e4f-5a6b7c8d9e0f","label":"A"},{"id":"0192f0b2-1a2b-7c3d-8e4f-5a6b7c8d9e10","label":"B"}],"votes":{"0192f0b2-1a2b-7c3d-8e4f-5a6b7c8d9e0f":1},"status":"open"}`
	if err := ValidateStateBytes(TypeInteractive, []byte(mismatch)); err == nil {
		t.Fatal("votes mismatch was accepted")
	}
}

func TestPlanActionAndApplyState(t *testing.T) {
	state := CollaborativeState{
		Title: "t", Summary: "", Body: "",
		Checklist: []ChecklistItem{}, Tags: []string{},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanAction(TypeCollaborative, "add_checklist_item", json.RawMessage(`{"text":"first"}`))
	if err != nil {
		t.Fatalf("plan add: %v", err)
	}
	next, _, _, err := ApplyStateAction(TypeCollaborative, raw, json.RawMessage(`{}`), plan, "")
	if err != nil {
		t.Fatalf("apply add: %v", err)
	}
	var afterAdd CollaborativeState
	if err := json.Unmarshal(next, &afterAdd); err != nil {
		t.Fatal(err)
	}
	if len(afterAdd.Checklist) != 1 {
		t.Fatalf("checklist = %+v", afterAdd.Checklist)
	}
	itemID := afterAdd.Checklist[0].ID

	plan, _ = PlanAction(TypeCollaborative, "toggle_checklist_item", json.RawMessage(`{"item_id":"`+itemID+`","done":true}`))
	next, _, _, err = ApplyStateAction(TypeCollaborative, next, json.RawMessage(`{}`), plan, "")
	if err != nil {
		t.Fatalf("apply toggle: %v", err)
	}
	if err := json.Unmarshal(next, &afterAdd); err != nil {
		t.Fatal(err)
	}
	if !afterAdd.Checklist[0].Done {
		t.Fatal("toggle did not set done")
	}

	plan, _ = PlanAction(TypeCollaborative, "add_tag", json.RawMessage(`{"tag":"b"}`))
	next, _, _, err = ApplyStateAction(TypeCollaborative, next, json.RawMessage(`{}`), plan, "")
	if err != nil {
		t.Fatalf("apply add tag: %v", err)
	}
	plan, _ = PlanAction(TypeCollaborative, "add_tag", json.RawMessage(`{"tag":"b"}`))
	if _, _, _, err := ApplyStateAction(TypeCollaborative, next, json.RawMessage(`{}`), plan, ""); err == nil {
		t.Fatal("duplicate tag was accepted")
	}

	plan, _ = PlanAction(TypeCollaborative, "remove_checklist_item", json.RawMessage(`{"item_id":"`+itemID+`"}`))
	next, _, _, err = ApplyStateAction(TypeCollaborative, next, json.RawMessage(`{}`), plan, "")
	if err != nil {
		t.Fatalf("apply remove: %v", err)
	}
	plan, _ = PlanAction(TypeCollaborative, "remove_checklist_item", json.RawMessage(`{"item_id":"`+itemID+`"}`))
	if _, _, _, err := ApplyStateAction(TypeCollaborative, next, json.RawMessage(`{}`), plan, ""); err == nil {
		t.Fatal("removing a missing item was accepted")
	}
}

func TestVoteTransitions(t *testing.T) {
	optionA, _ := uuid.NewV7()
	optionB, _ := uuid.NewV7()
	state := InteractiveState{
		Title: "Poll", Prompt: "",
		Options: []InteractiveOption{
			{ID: optionA.String(), Label: "A"},
			{ID: optionB.String(), Label: "B"},
		},
		Votes:  map[string]int{optionA.String(): 0, optionB.String(): 0},
		Status: "open",
	}
	raw, _ := json.Marshal(state)
	shared := json.RawMessage(`{}`)

	plan, err := PlanAction(TypeInteractive, "cast_vote", json.RawMessage(`{"option_id":"`+optionA.String()+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	next, _, voted, err := ApplyStateAction(TypeInteractive, raw, shared, plan, "")
	if err != nil {
		t.Fatal(err)
	}
	if voted != optionA.String() {
		t.Fatalf("voted option = %s", voted)
	}
	var after InteractiveState
	_ = json.Unmarshal(next, &after)
	if after.Votes[optionA.String()] != 1 || after.Votes[optionB.String()] != 0 {
		t.Fatalf("votes = %v", after.Votes)
	}

	plan, _ = PlanAction(TypeInteractive, "cast_vote", json.RawMessage(`{"option_id":"`+optionB.String()+`"}`))
	next, _, voted, err = ApplyStateAction(TypeInteractive, next, shared, plan, optionA.String())
	if err != nil {
		t.Fatal(err)
	}
	if voted != optionB.String() {
		t.Fatalf("voted option = %s", voted)
	}
	_ = json.Unmarshal(next, &after)
	if after.Votes[optionA.String()] != 0 || after.Votes[optionB.String()] != 1 {
		t.Fatalf("changed votes = %v", after.Votes)
	}

	plan, _ = PlanAction(TypeInteractive, "close_poll", json.RawMessage(`{}`))
	closed, _, _, err := ApplyStateAction(TypeInteractive, next, shared, plan, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(closed, &after)
	if after.Status != "closed" {
		t.Fatal("poll did not close")
	}
	plan, _ = PlanAction(TypeInteractive, "cast_vote", json.RawMessage(`{"option_id":"`+optionA.String()+`"}`))
	if _, _, _, err := ApplyStateAction(TypeInteractive, closed, shared, plan, ""); err == nil {
		t.Fatal("vote on a closed poll was accepted")
	}
}

func TestSetSharedContextKeepsState(t *testing.T) {
	state := json.RawMessage(`{"title":"t","summary":"","body":"","checklist":[],"tags":[]}`)
	plan, err := PlanAction(TypeCollaborative, "set_shared_context", json.RawMessage(`{"shared_context":{"k":"v"}}`))
	if err != nil {
		t.Fatal(err)
	}
	newState, newShared, _, err := ApplyStateAction(TypeCollaborative, state, json.RawMessage(`{}`), plan, "")
	if err != nil {
		t.Fatal(err)
	}
	if string(newState) != string(state) {
		t.Fatal("set_shared_context changed state")
	}
	if string(newShared) != `{"k":"v"}` {
		t.Fatalf("shared context = %s", newShared)
	}
	if _, err := PlanAction(TypeCollaborative, "set_shared_context", json.RawMessage(`{"shared_context":null}`)); err == nil {
		t.Fatal("null shared context was accepted")
	}
}
