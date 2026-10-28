// SPDX-License-Identifier: Apache-2.0
package personalai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/QiyanAI/derive-world/internal/handoff"
	"github.com/QiyanAI/derive-world/internal/model"
	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/worldseed"
)

func TestGlobalPromptOffersWorldCreation(t *testing.T) {
	profile := &Profile{Name: "Test AI"}
	system, messages, _, err := buildPrompt(profile, nil, nil, nil, "Create my first world", "")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if !contains(system, "<world_creation_mode>") {
		t.Fatalf("system prompt has no world creation mode")
	}
	if !contains(system, "world.create") {
		t.Fatalf("system prompt has no world.create action")
	}
	if len(messages) != 1 || messages[0].Role != "user" {
		t.Fatalf("unexpected messages: %+v", messages)
	}
}

func TestObjectPromptDoesNotOfferWorldCreation(t *testing.T) {
	profile := &Profile{Name: "Test AI"}
	projection := &handoff.Projection{
		ObjectVersion:    1,
		ObjectType:       objects.TypeOpenWorld,
		PermittedActions: []string{"read", "contribute", "derive"},
		PermittedObjectContext: handoff.ObjectContext{
			State:         json.RawMessage(`{"title":"Test","premise":"","kinds":[],"nodes":[],"links":[],"rules":[],"views":[],"tags":[]}`),
			SharedContext: json.RawMessage(`{}`),
		},
	}
	system, _, _, err := buildPrompt(profile, nil, projection, nil, "Change this world", "")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if contains(system, "<world_creation_mode>") {
		t.Fatalf("object-bound prompt must not offer global creation")
	}
}

func TestFixtureWorldCreateProposalPassesServerValidation(t *testing.T) {
	fixture := model.NewFixture("")
	response, err := fixture.Complete(context.Background(), model.Request{
		System:   "You are the user's Personal AI. <world_creation_mode>",
		Messages: []model.Message{{Role: "user", Content: "Create my first world"}},
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	reply, actions, status, err := parseModelOutput(response.Text)
	if err != nil {
		t.Fatalf("parse fixture output: %v", err)
	}
	if status != "structured" {
		t.Fatalf("parse status = %q", status)
	}
	if len(reply) == 0 || len(actions) != 1 {
		t.Fatalf("unexpected fixture output: %q / %d actions", reply, len(actions))
	}
	if actions[0].CapabilityID != "world.create" {
		t.Fatalf("capability = %q", actions[0].CapabilityID)
	}
	var input worldCreateActionInput
	if err := decodeSingleJSON(string(actions[0].Input), &input); err != nil {
		t.Fatalf("decode action input: %v", err)
	}
	content := input.Content
	if len(input.Brief) > 0 {
		seed, err := worldseed.Decode(input.Brief)
		if err != nil {
			t.Fatalf("decode fixture brief: %v", err)
		}
		composition, err := worldseed.Compose(seed, input.Title)
		if err != nil {
			t.Fatalf("compose fixture brief: %v", err)
		}
		content, _ = json.Marshal(composition.Content)
	}
	if _, _, err := objects.ValidateCreate(objects.CreateInput{
		ObjectType: objects.TypeOpenWorld,
		Title:      input.Title,
		Content:    content,
	}); err != nil {
		t.Fatalf("fixture proposal is not a valid open world: %v", err)
	}
	encoded, _ := json.Marshal(content)
	if len(encoded) == 0 {
		t.Fatalf("empty content")
	}
	t.Logf("composed fixture content bytes=%d", len(encoded))
}

func contains(value, needle string) bool {
	for index := 0; index+len(needle) <= len(value); index++ {
		if value[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestParseModelOutputStripsMarkdownFence(t *testing.T) {
	reply, actions, status, err := parseModelOutput("```json\n{\"reply\":\"fenced\",\"proposed_actions\":[]}\n```")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if status != "structured" || reply != "fenced" || len(actions) != 0 {
		t.Fatalf("status=%s reply=%q actions=%d", status, reply, len(actions))
	}
}
