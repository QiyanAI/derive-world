// SPDX-License-Identifier: Apache-2.0
package derive

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/objects"
)

func TestAssembleCollaborativeWhitelist(t *testing.T) {
	parent := objects.CollaborativeState{
		Title:   "Parent",
		Summary: "summary",
		Body:    "body",
		Checklist: []objects.ChecklistItem{
			{ID: mustV7(t), Text: "item", Done: true},
		},
		Tags: []string{"one", "two"},
	}
	raw, _ := json.Marshal(parent)
	childRaw, err := assemble(objects.TypeCollaborative, "Child", raw, []string{"summary"})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	var child objects.CollaborativeState
	if err := json.Unmarshal(childRaw, &child); err != nil {
		t.Fatal(err)
	}
	if child.Title != "Child" || child.Summary != "summary" {
		t.Fatalf("child = %+v", child)
	}
	if child.Body != "" || len(child.Checklist) != 0 || len(child.Tags) != 0 {
		t.Fatalf("unselected fields were inherited: %+v", child)
	}

	childRaw, err = assemble(objects.TypeCollaborative, "Child", raw, []string{"checklist", "tags"})
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(childRaw, &child)
	if len(child.Checklist) != 1 || child.Checklist[0].Text != "item" || !child.Checklist[0].Done {
		t.Fatalf("checklist = %+v", child.Checklist)
	}
	if child.Checklist[0].ID == parent.Checklist[0].ID {
		t.Fatal("checklist item id was not regenerated")
	}
	if len(child.Tags) != 2 {
		t.Fatalf("tags = %v", child.Tags)
	}
}

func TestAssembleInteractiveRegeneratesIDsAndVotes(t *testing.T) {
	optionA, optionB := mustV7(t), mustV7(t)
	parent := objects.InteractiveState{
		Title: "Poll", Prompt: "prompt",
		Options: []objects.InteractiveOption{
			{ID: optionA, Label: "A"}, {ID: optionB, Label: "B"},
		},
		Votes:  map[string]int{optionA: 3, optionB: 1},
		Status: "closed",
	}
	raw, _ := json.Marshal(parent)
	childRaw, err := assemble(objects.TypeInteractive, "Child poll", raw, []string{"options"})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	var child objects.InteractiveState
	if err := json.Unmarshal(childRaw, &child); err != nil {
		t.Fatal(err)
	}
	if child.Prompt != "" || child.Status != "open" || len(child.Options) != 2 {
		t.Fatalf("child = %+v", child)
	}
	if child.Options[0].ID == optionA || child.Options[1].ID == optionB {
		t.Fatal("option ids were not regenerated")
	}
	for _, option := range child.Options {
		if child.Votes[option.ID] != 0 {
			t.Fatalf("vote for %s = %d", option.ID, child.Votes[option.ID])
		}
	}
}

func TestAssembleRejectsInvalidSource(t *testing.T) {
	if _, err := assemble(objects.TypeCollaborative, "x", []byte(`not-json`), []string{"summary"}); err == nil {
		t.Fatal("invalid source JSON was accepted")
	}
	if _, err := assemble("unknown", "x", []byte(`{}`), []string{"summary"}); err == nil {
		t.Fatal("unknown object type was accepted")
	}
}

func mustV7(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}
