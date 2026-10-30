// SPDX-License-Identifier: Apache-2.0
package personalai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/handoff"
)

// The authoring contract must never depend on a keyword guess. The on-site
// failure this locks was "Start this world from nothing": a perfectly clear
// creation request that carried no creation keyword, so the model received the
// world state but not the brief shape and answered with a question.
func TestPromptBudgetShape(t *testing.T) {
	system, _, chars, err := buildPrompt(nil, nil, (*handoff.Projection)(nil), nil, "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(system, "A world brief has this shape") {
		t.Fatal("an unbound turn, which is the creation entry point, lost the world brief guide")
	}
	if !strings.Contains(system, "WORLD BRIEF CONTRACT") {
		t.Fatal("the compact world brief contract is missing")
	}
	if !strings.Contains(system, "particles count 0..120") {
		t.Fatal("the always-on contract lost the hard bounds")
	}
	// The always-on part of the prompt stays small; only the guide and the
	// projection vary with the turn.
	contractSystem, _, _, err := buildPrompt(nil, nil, (*handoff.Projection)(nil), nil, "x", "")
	if err != nil {
		t.Fatal(err)
	}
	guideStart := strings.Index(contractSystem, "A world brief has this shape")
	if guideStart < 0 {
		t.Fatal("guide missing")
	}
	if guideStart > 4200 {
		t.Fatalf("the instructions before the guide are %d bytes", guideStart)
	}
	if chars <= 0 {
		t.Fatal("prompt character accounting is empty")
	}

	createSystem, _, _, err := buildPrompt(nil, nil, (*handoff.Projection)(nil), nil, "x", "create")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(createSystem, "A world brief has this shape") {
		t.Fatal("creation intent did not include the world brief guide")
	}
	// Any follow-up in a global conversation keeps the full schema. The real
	// 12:04 failure was exactly this case: the approval turn had no guide and
	// the model invented a non-existent brief shape.
	history := []Message{{Role: "user", Content: "伦敦贝克街的街景"}}
	followSystem, _, _, err := buildPrompt(nil, nil, (*handoff.Projection)(nil), history, "批准通过", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(followSystem, "A world brief has this shape") {
		t.Fatal("a follow-up turn lost the full world brief guide")
	}
}

func TestPromptKeepsContractForANaturalChangeRequest(t *testing.T) {
	// The exact on-site wording that lost the guide under keyword gating.
	projection := openWorldProjection(t, `{"premise":"rain","kinds":[{"id":"k1","name":"Building"}],"nodes":[]}`)
	system, _, _, err := buildPrompt(nil, nil, projection, nil, "把这里改成下雨的夜晚", "")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	for _, phrase := range []string{
		"A world brief has this shape",
		"particles count 0..120",
		"send only what should change",
		"you may leave kinds out",
	} {
		if !strings.Contains(system, phrase) {
			t.Fatalf("a bound open world turn lacks %q", phrase)
		}
	}
}

func TestPromptTellsTheModelHowToStartAnEmptyWorld(t *testing.T) {
	projection := openWorldProjection(t, `{"premise":"","kinds":[],"nodes":[],"links":[],"rules":[],"views":["scene"],"tags":[]}`)
	system, _, _, err := buildPrompt(nil, nil, projection, nil, "Start this world from nothing", "")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	for _, phrase := range []string{
		"<world_bootstrap>",
		"This world is empty",
		"Do not ask the user to choose kinds, fields, parameters",
		"world.reimagine",
	} {
		if !strings.Contains(system, phrase) {
			t.Fatalf("empty-world prompt lacks %q", phrase)
		}
	}
}

func TestPromptOmitsBootstrapWhenTheWorldHasContent(t *testing.T) {
	projection := openWorldProjection(t, `{"premise":"rain","kinds":[{"id":"k1","name":"Building"}],"nodes":[]}`)
	system, _, _, err := buildPrompt(nil, nil, projection, nil, "add a lamp", "")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if strings.Contains(system, "<world_bootstrap>") {
		t.Fatal("bootstrap guidance leaked into a world that already has content")
	}
}

// A rich world used to project to more than the whole 16 KiB call budget, so
// the turn failed with 413 before the model saw anything. The projection is now
// compacted to fit while ids and names stay exact.
func TestPromptCompactsARichWorldToFitTheBudget(t *testing.T) {
	projection := openWorldProjection(t, richOpenWorldState(t, 40))
	const budget = 16384 // the deployment value that failed on site
	system, _, chars, err := buildPromptWithin(nil, nil, projection, nil, "把这里改成下雨的夜晚", "", budget)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if chars > budget {
		t.Fatalf("prompt is %d chars, budget %d", chars, budget)
	}
	for _, phrase := range []string{
		"<untrusted_object_content>",
		"kind-000",
		"thing-039",
		"A world brief has this shape",
		"Long prose in this context was shortened",
	} {
		if !strings.Contains(system, phrase) {
			t.Fatalf("compacted prompt lost %q", phrase)
		}
	}
}

func TestPromptCarriesThePreviousRejectionOnce(t *testing.T) {
	notice := rejectionExplanation("seed_decode", true)
	history := []Message{
		{Role: "user", Content: "把这里改成下雨的夜晚"},
		{Role: "assistant", Content: "我建议把雨声和雨丝打开。", Notice: &notice},
	}
	system, _, _, err := buildPrompt(nil, nil, nil, history, "再试一次", "create")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if !strings.Contains(system, "<previous_turn>") || !strings.Contains(system, notice) {
		t.Fatal("the previous rejection is not available to the model")
	}
	clean := []Message{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "hi"}}
	system, _, _, err = buildPrompt(nil, nil, nil, clean, "next", "")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if strings.Contains(system, "<previous_turn>") {
		t.Fatal("a turn without a rejection claims the previous plan was rejected")
	}
	// A plan the server repaired and accepted was not refused. Telling the
	// model it was would make it rewrite a world that already changed.
	repair := seedRepairNotice(&seedRepair{Dropped: []string{"$.background"}}, true)
	repaired := []Message{
		{Role: "user", Content: "把这里改成下雨的夜晚"},
		{Role: "assistant", Content: "雨夜已经准备好，请确认。", Notice: &repair},
	}
	system, _, _, err = buildPrompt(nil, nil, nil, repaired, "再调整一下", "create")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if strings.Contains(system, "<previous_turn>") {
		t.Fatal("an accepted, repaired plan is reported to the model as a rejection")
	}
}

func openWorldProjection(t *testing.T, state string) *handoff.Projection {
	t.Helper()
	return &handoff.Projection{
		ObjectID:      uuid.MustParse("018f0000-0000-7000-8000-000000000010"),
		ObjectVersion: 4,
		ObjectType:    "open_world",
		PermittedObjectContext: handoff.ObjectContext{
			State:         json.RawMessage(state),
			SharedContext: json.RawMessage(`{}`),
			RecentEvents: []handoff.RecentEvent{
				{Seq: 1, EventType: "object.created", Summary: "created"},
			},
		},
		PermittedActions:     []string{"read", "contribute", "derive"},
		ContextSchemaVersion: 1,
	}
}

// richOpenWorldState builds a world at the seed limits with long prose, the
// shape that overflowed the real 16 KiB call budget on site.
func richOpenWorldState(t *testing.T, nodes int) string {
	t.Helper()
	prose := strings.Repeat("雨落在湿亮的街道上，路灯把水洼照成一片片金色。", 40)
	things := make([]map[string]any, 0, nodes)
	for index := 0; index < nodes; index++ {
		things = append(things, map[string]any{
			"id":    "thing-" + pad3(index),
			"name":  "thing " + pad3(index),
			"kind":  "kind-000",
			"text":  prose,
			"x":     0.5,
			"y":     0.5,
			"state": map[string]any{"status": "open", "values": map[string]any{}},
		})
	}
	state := map[string]any{
		"title":   "雨城漫步",
		"premise": prose,
		"kinds": []map[string]any{{
			"id": "kind-000", "name": "楼宇", "hue": 210,
			"fields": []map[string]any{{"key": "f1", "label": "高度", "type": "number"}},
		}},
		"nodes": things,
		"links": []any{},
		"rules": []any{},
		"views": []string{"scene", "stage", "cards", "timeline"},
		"tags":  []string{},
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal rich state: %v", err)
	}
	return string(encoded)
}

func pad3(value int) string {
	digits := []byte{'0', '0', '0'}
	for index := 2; index >= 0 && value > 0; index-- {
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits)
}
