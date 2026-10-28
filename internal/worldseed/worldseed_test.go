// SPDX-License-Identifier: Apache-2.0
package worldseed

import (
	"encoding/json"
	"testing"

	"github.com/QiyanAI/derive-world/internal/objects"
)

func intPtr(value int) *int           { return &value }
func floatPtr(value float64) *float64 { return &value }

func decodeBrief(t *testing.T, raw string) *Brief {
	t.Helper()
	brief, err := Decode(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("decode brief: %v", err)
	}
	return brief
}

func TestComposeCollectGame(t *testing.T) {
	brief := decodeBrief(t, `{
		"premise":"A traveler gathers fallen light in an old forest.",
		"atmosphere":"quiet, green, drifting motes",
		"kinds":[
			{"name":"Traveler","form":"M 0 -24 A 24 24 0 1 1 0 24 A 24 24 0 1 1 0 -24 Z","hue":180},
			{"name":"Light","form":"M 0 -18 L 12 0 L 0 18 L -12 0 Z","hue":48,"motion":"pulse"},
			{"name":"Old tree","form":"M 0 -30 L 10 20 L -10 20 Z","hue":120,"solid":true}
		],
		"things":[
			{"kind":0,"name":"The traveler","text":"They carry an empty lantern."},
			{"kind":1,"name":"First light","text":"A warm seed of light."},
			{"kind":1,"name":"Second light","text":"It hums when touched."},
			{"kind":1,"name":"Third light","text":"It remembers the sun."},
			{"kind":2,"name":"The oldest tree","text":"Its roots hold the path."}
		],
		"relations":[{"from":0,"to":1,"label":"sees"}],
		"experience":{"primary":"live","live":{"avatar":0,"goal":{"type":"collect","kind":1,"count":3,"label":"Gather the light"},"particles":{"kind":"motes","count":30}}}
	}`)
	composition, err := Compose(brief, "Forest of Light")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if composition.Primary != "live" {
		t.Fatalf("primary = %q", composition.Primary)
	}
	raw, err := json.Marshal(composition.Content)
	if err != nil {
		t.Fatal(err)
	}
	state, _, err := objects.ValidateCreate(objects.CreateInput{
		ObjectType: objects.TypeOpenWorld, Title: "Forest of Light", Content: raw,
	})
	if err != nil {
		t.Fatalf("validate create: %v", err)
	}
	var decoded objects.OpenWorldState
	if err := json.Unmarshal(state, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Experience == nil || decoded.Experience.Root.Capability != "core.live" {
		t.Fatalf("expected live root, got %#v", decoded.Experience)
	}
}

func TestComposeAnimation(t *testing.T) {
	brief := decodeBrief(t, `{
		"premise":"A slow nebula breathes in the dark.",
		"atmosphere":"stars and drifting dust",
		"kinds":[
			{"name":"Nebula","form":"M 0 -30 Q 30 -10 0 30 Q -30 -10 0 -30 Z","hue":280,"motion":"breathe"},
			{"name":"Dust","form":"M 0 -10 L 10 0 L 0 10 L -10 0 Z","hue":40,"motion":"flicker"}
		],
		"things":[
			{"kind":0,"name":"The sleeping nebula","text":"It turns in its sleep."},
			{"kind":1,"name":"A grain of dust","text":"It catches a distant star."},
			{"kind":1,"name":"Another grain","text":"It drifts sideways."}
		],
		"experience":{"primary":"live","live":{"title":"Breathing Nebula"}}
	}`)
	composition, err := Compose(brief, "Breathing Nebula")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if composition.Primary != "live" {
		t.Fatalf("primary = %q", composition.Primary)
	}
}

func TestComposeStory(t *testing.T) {
	brief := decodeBrief(t, `{
		"premise":"A library where the tide decides which book opens.",
		"experience":{"primary":"story","story":{"title":"The Tide Library","chapters":[
			{"title":"The first door","text":"The tide comes in. A green book floats to your hands.","choices":[{"label":"Open it","to":1},{"label":"Put it back","to":2}]},
			{"title":"What the book remembers","text":"The pages are wet with a sea that no longer exists.","choices":[{"label":"Read on","to":0}]},
			{"title":"The librarian","text":"She says every book is a tide that chose to stay.","choices":[]}
		]}}
	}`)
	composition, err := Compose(brief, "The Tide Library")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if composition.Primary != "story" {
		t.Fatalf("primary = %q", composition.Primary)
	}
	raw, _ := json.Marshal(composition.Content)
	state, _, err := objects.ValidateCreate(objects.CreateInput{
		ObjectType: objects.TypeOpenWorld, Title: "The Tide Library", Content: raw,
	})
	if err != nil {
		t.Fatalf("validate create: %v", err)
	}
	var decoded objects.OpenWorldState
	if err := json.Unmarshal(state, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Experience == nil || decoded.Experience.Root.Capability != "core.story" {
		t.Fatalf("expected story root, got %#v", decoded.Experience)
	}
	if decoded.Variables["story.chapter"] != float64(0) {
		t.Fatalf("expected reading variable, got %#v", decoded.Variables)
	}
}

func TestComposeWorkspace(t *testing.T) {
	brief := decodeBrief(t, `{
		"premise":"A quiet workbench for a writing plan.",
		"kinds":[
			{"name":"Draft","hue":210,"fields":[
				{"label":"Title","type":"text","initial":"Untitled"},
				{"label":"Words","type":"number","initial":0},
				{"label":"Notes","type":"text"}
			]}
		],
		"things":[
			{"kind":0,"name":"Opening chapter","text":"A first draft.","values":{"Title":"Opening chapter","Words":1200}},
			{"kind":0,"name":"Second chapter","text":"Still growing.","values":{"Title":"Second chapter","Words":400}}
		],
		"experience":{"primary":"workspace","workspace":{"title":"Writing plan","kind":0,"columns":["Title","Words"]}}
	}`)
	composition, err := Compose(brief, "Writing plan")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if composition.Primary != "workspace" {
		t.Fatalf("primary = %q", composition.Primary)
	}
	raw, _ := json.Marshal(composition.Content)
	if _, _, err := objects.ValidateCreate(objects.CreateInput{
		ObjectType: objects.TypeOpenWorld, Title: "Writing plan", Content: raw,
	}); err != nil {
		t.Fatalf("validate create: %v", err)
	}
}

func TestComposeCombinedSurfaces(t *testing.T) {
	brief := decodeBrief(t, `{
		"premise":"A city where every choice leaves a light.",
		"kinds":[
			{"name":"Resident","hue":168,"fields":[{"label":"Mood","type":"text","initial":"curious"}]},
			{"name":"Square","hue":214,"fields":[{"label":"Light","type":"number","initial":1}]}
		],
		"things":[
			{"kind":0,"name":"Mira","text":"She listens to the city."},
			{"kind":1,"name":"The listening square","text":"It answers with light."}
		],
		"interactions":[{"from":0,"verb":"Listen","to":1,"field":"Light","op":"add","value":1}],
		"experience":{"surfaces":["live","story","workspace"],"primary":"live","live":{"avatar":0},
		"story":{"chapters":[{"title":"The first night","text":"Mira hears the square breathe.","choices":[]}]},
		"workspace":{"kind":1,"columns":["Light"]}}
	}`)
	composition, err := Compose(brief, "City of Listening")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if len(composition.Surfaces) != 3 {
		t.Fatalf("surfaces = %#v", composition.Surfaces)
	}
	raw, _ := json.Marshal(composition.Content)
	if _, _, err := objects.ValidateCreate(objects.CreateInput{
		ObjectType: objects.TypeOpenWorld, Title: "City of Listening", Content: raw,
	}); err != nil {
		t.Fatalf("validate create: %v", err)
	}
}

func TestReshapeMakesWorldWalkable(t *testing.T) {
	initial := decodeBrief(t, `{
		"premise":"A garden of glass and rain.",
		"kinds":[{"name":"Glass flower","hue":190,"fields":[{"label":"Glow","type":"number","initial":2}]}],
		"things":[{"kind":0,"name":"The first flower","text":"It rings in the rain."}]
	}`)
	composition, err := Compose(initial, "Glass Garden")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	raw, _ := json.Marshal(composition.Content)
	state, _, err := objects.ValidateCreate(objects.CreateInput{
		ObjectType: objects.TypeOpenWorld, Title: "Glass Garden", Content: raw,
	})
	if err != nil {
		t.Fatalf("validate create: %v", err)
	}
	reshaped := decodeBrief(t, `{
		"premise":"A garden of glass and rain.",
		"experience":{"primary":"live","live":{"avatar":0,"title":"Walk the garden"}}
	}`)
	result, err := Reshape(state, reshaped)
	if err != nil {
		t.Fatalf("reshape: %v", err)
	}
	if result.Primary != "live" {
		t.Fatalf("primary = %q", result.Primary)
	}
	encoded, _ := json.Marshal(result.State)
	if err := objects.ValidateStateBytes(objects.TypeOpenWorld, encoded); err != nil {
		t.Fatalf("validate reshaped state: %v", err)
	}
}

func TestComposeRejectsUnsupportedShape(t *testing.T) {
	raw := `{"premise":"A world","kinds":[{"name":"Thing","form":"<script>alert(1)</script>"}],"things":[{"kind":0,"name":"One"}]}`
	if _, err := Decode([]byte(raw)); err == nil {
		t.Fatal("expected invalid form to be rejected")
	}
}

func TestReshapeAppendsStoryChapters(t *testing.T) {
	initial := decodeBrief(t, `{"premise":"A story that grows.","experience":{"primary":"story","story":{"chapters":[{"title":"One","text":"First.","choices":[]}]}}}`)
	composition, err := Compose(initial, "Growing Story")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	raw, _ := json.Marshal(composition.Content)
	state, _, err := objects.ValidateCreate(objects.CreateInput{
		ObjectType: objects.TypeOpenWorld, Title: "Growing Story", Content: raw,
	})
	if err != nil {
		t.Fatalf("validate create: %v", err)
	}
	next := decodeBrief(t, `{"premise":"A story that grows.","experience":{"primary":"story","story":{"chapters":[{"title":"Two","text":"Second.","choices":[{"label":"Back","to":0}]}]}}}`)
	result, err := Reshape(state, next)
	if err != nil {
		t.Fatalf("reshape: %v", err)
	}
	if result.Primary != "story" {
		t.Fatalf("primary = %q", result.Primary)
	}
	encoded, _ := json.Marshal(result.State)
	if err := objects.ValidateStateBytes(objects.TypeOpenWorld, encoded); err != nil {
		t.Fatalf("validate reshaped state: %v", err)
	}
}

func TestComposeCustomInstrument(t *testing.T) {
	brief := decodeBrief(t, `{
		"premise":"A star map you can play with your hands.",
		"experience":{"primary":"custom","custom":{
			"title":"Star instrument","width":1200,"height":800,
			"background":{"mode":"gradient","hue":240,"hue2":265,"lightness":8,"accent_hue":42},
			"vars":{"pad_a":false,"pad_b":false,"note":0},
			"nodes":[
				{"id":"pad_a","type":"dot","x":420,"y":420,"r":56,"hue":42,
				 "bindings":[{"prop":"opacity","var":"pad_a","in_min":0,"in_max":1,"out_min":0.35,"out_max":1}],
				 "on_click":[
					{"type":"toggle_var","name":"pad_a"},
					{"type":"sound","freq":440,"duration":0.3,"wave":"sine"},
					{"type":"if","cond":{"var":"pad_a","op":"truthy"},
					 "then":[{"type":"add_var","name":"note","value":1}]}
				 ]},
				{"id":"pad_b","type":"dot","x":620,"y":420,"r":56,"hue":200,
				 "on_click":[{"type":"toggle_var","name":"pad_b"},
					{"type":"sound","freq":660,"duration":0.3,"wave":"triangle"}]},
				{"id":"readout","type":"text","x":420,"y":600,"text":"notes","size":28,
				 "bind_text":{"var":"note"}}
			],
			"timeline":{"duration":8,"loop":true,"tracks":[
				{"node_id":"pad_a","property":"r","keys":[{"t":0,"v":56},{"t":4,"v":64},{"t":8,"v":56}]}
			]}
		}}
	}`)
	composition, err := Compose(brief, "Star instrument")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if composition.Primary != "custom" {
		t.Fatalf("primary = %q", composition.Primary)
	}
	raw, _ := json.Marshal(composition.Content)
	state, _, err := objects.ValidateCreate(objects.CreateInput{
		ObjectType: objects.TypeOpenWorld, Title: "Star instrument", Content: raw,
	})
	if err != nil {
		t.Fatalf("validate create: %v", err)
	}
	var decoded objects.OpenWorldState
	_ = json.Unmarshal(state, &decoded)
	if decoded.Experience == nil || decoded.Experience.Root.Capability != "core.custom" {
		t.Fatalf("expected custom root, got %#v", decoded.Experience)
	}
	if decoded.Variables["pad_a"] != false || decoded.Variables["note"] != float64(0) {
		t.Fatalf("custom variables not initialized: %#v", decoded.Variables)
	}
}

func TestReshapeAppendsCustomNodes(t *testing.T) {
	initial := decodeBrief(t, `{"premise":"A growing instrument.","experience":{"primary":"custom","custom":{
		"title":"Instrument","background":{"mode":"solid","hue":240,"hue2":260,"lightness":8,"accent_hue":40},
		"vars":{"notes":0},"nodes":[{"id":"pad_a","type":"dot","x":300,"y":300,"r":40,"hue":40,
		"on_click":[{"type":"add_var","name":"notes","value":1}]}]}}}`)
	composition, err := Compose(initial, "Growing instrument")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	raw, _ := json.Marshal(composition.Content)
	state, _, err := objects.ValidateCreate(objects.CreateInput{
		ObjectType: objects.TypeOpenWorld, Title: "Growing instrument", Content: raw,
	})
	if err != nil {
		t.Fatalf("validate create: %v", err)
	}
	next := decodeBrief(t, `{"premise":"A growing instrument.","experience":{"primary":"custom","custom":{
		"title":"Instrument","nodes":[{"id":"pad_b","type":"dot","x":500,"y":300,"r":40,"hue":200,
		"on_click":[{"type":"add_var","name":"notes","value":1}]}],
		"vars":{"extra":true}}}}`)
	result, err := Reshape(state, next)
	if err != nil {
		t.Fatalf("reshape: %v", err)
	}
	if result.State.Experience == nil {
		t.Fatal("missing experience")
	}
	var program struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
		Vars map[string]any `json:"vars"`
	}
	if err := json.Unmarshal(result.State.Experience.Root.Props, &program); err != nil {
		t.Fatalf("decode custom program: %v", err)
	}
	if len(program.Nodes) != 2 || program.Nodes[0].ID != "pad_a" || program.Nodes[1].ID != "pad_b" {
		t.Fatalf("nodes = %#v", program.Nodes)
	}
	if program.Vars["notes"] != float64(0) || program.Vars["extra"] != true {
		t.Fatalf("vars = %#v", program.Vars)
	}
	encoded, _ := json.Marshal(result.State)
	if err := objects.ValidateStateBytes(objects.TypeOpenWorld, encoded); err != nil {
		t.Fatalf("validate reshaped state: %v", err)
	}
}
