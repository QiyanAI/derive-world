// SPDX-License-Identifier: Apache-2.0
package personalai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/worldseed"
)

const validWorldCreateBrief = `{
	"premise":"A quiet room with a single lamp.",
	"kinds":[{"name":"Room","hue":210},{"name":"Lamp","hue":48}],
	"things":[{"kind":0,"name":"Room"},{"kind":1,"name":"Lamp"}],
	"experience":{
		"surfaces":["scene3d"],
		"primary":"scene3d",
		"scene3d":{"title":"Room","camera":"follow","player":0,
			"views":[{"kind":1,"primitive":"sphere"}]}
	}
}`

func TestBuildWorldCreateContentStages(t *testing.T) {
	cases := []struct {
		name      string
		input     worldCreateActionInput
		wantStage string
	}{
		{
			name:  "valid scene3d brief",
			input: worldCreateActionInput{Title: "Room", Brief: json.RawMessage(validWorldCreateBrief)},
		},
		{
			name:      "missing title",
			input:     worldCreateActionInput{Brief: json.RawMessage(validWorldCreateBrief)},
			wantStage: "title_or_body",
		},
		{
			name:      "missing body",
			input:     worldCreateActionInput{Title: "Room"},
			wantStage: "title_or_body",
		},
		{
			// The brief's own content lives under "kinds"/"things". A misspelled
			// top-level key is not guessed back: dropping it leaves things
			// pointing at no kinds, which is rejected rather than invented.
			name: "misspelled kinds is not guessed",
			input: worldCreateActionInput{Title: "Room", Brief: json.RawMessage(`{
				"premise":"A quiet room.",
				"kindz":[{"name":"Room"}],
				"things":[{"kind":0,"name":"Room"}]
			}`)},
			wantStage: "seed_decode",
		},
		{
			name: "missing premise",
			input: worldCreateActionInput{Title: "Room", Brief: json.RawMessage(`{
				"kinds":[{"name":"Room"}],
				"things":[{"kind":0,"name":"Room"}]
			}`)},
			wantStage: "seed_compose",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content, rejection := buildWorldCreateContent(tc.input)
			gotStage := ""
			if rejection != nil {
				gotStage = rejection.Stage
			}
			if gotStage != tc.wantStage {
				t.Fatalf("stage = %q, want %q (detail=%q)", gotStage, tc.wantStage, rejectionDetail(rejection))
			}
			if tc.wantStage == "" && len(content) == 0 {
				t.Fatal("valid input produced empty content")
			}
		})
	}
}

// A model that adds one harmless descriptive field must not lose the whole
// creation. These are the exact shapes seen on site: "background" at the top
// level of the brief, and an extra colour on a thing. The plan is accepted with
// a bounded repair, and the user is told the plan was fitted, not changed.
func TestWorldCreateRepairsBenignBriefShapes(t *testing.T) {
	cases := []struct {
		name    string
		brief   string
		dropped string
	}{
		{
			name: "background beside real fields",
			brief: `{
				"premise":"A rainy street at night.",
				"background":"heavy rain",
				"kinds":[{"name":"Street"}],
				"things":[{"kind":0,"name":"Street"}]
			}`,
			dropped: "$.background",
		},
		{
			name: "extra field on a thing",
			brief: `{
				"premise":"A rainy street at night.",
				"kinds":[{"name":"Street"}],
				"things":[{"kind":0,"name":"Street","colour":"grey"}]
			}`,
			dropped: "$.things[0].colour",
		},
		{
			name: "title nested inside the brief",
			brief: `{
				"premise":"A rainy street at night.",
				"title":"inside the brief",
				"kinds":[{"name":"Street"}],
				"things":[{"kind":0,"name":"Street"}]
			}`,
			dropped: "$.title",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content, repair, rejection := buildWorldCreate(worldCreateActionInput{
				Title: "Rain City", Brief: json.RawMessage(tc.brief),
			})
			if rejection != nil {
				t.Fatalf("benign shape was rejected at %s: %s", rejection.Stage, rejection.Detail)
			}
			if len(content) == 0 {
				t.Fatal("accepted repair produced empty content")
			}
			if repair == nil {
				t.Fatal("no repair was recorded, so the user would not be told")
			}
			found := false
			for _, path := range repair.Dropped {
				if path == tc.dropped {
					found = true
				}
			}
			if !found {
				t.Fatalf("dropped paths %v lack %q", repair.Dropped, tc.dropped)
			}
			if notice := seedRepairNotice(repair, false); notice == "" {
				t.Fatal("a repair with no user-visible notice")
			}
		})
	}
}

// A change to an existing world goes through the same bounded repairs as a
// creation. Nothing here is guessed: the unknown key is removed, the number is
// moved to its documented bound, and the change still has to decode strictly.
func TestReimagineRepairsBenignBriefShapes(t *testing.T) {
	state := json.RawMessage(`{"title":"Rain City","premise":"rain","kinds":[{"id":"k1","name":"Street","hue":210}],"nodes":[],"links":[],"rules":[],"views":["scene"],"tags":[]}`)
	brief := json.RawMessage(`{
		"premise":"A rainy street at night.",
		"background":"heavy rain",
		"things":[{"kind":0,"name":"Wet street"}],
		"experience":{
			"surfaces":["live"],
			"live":{"particles":{"kind":"rain","count":900}}
		}
	}`)
	seed, repair, rejection := decodeReimagineBrief(brief, state)
	if rejection != nil {
		t.Fatalf("a change with benign shapes was rejected at %s: %s", rejection.Stage, rejection.Detail)
	}
	if seed == nil || len(seed.Things) != 1 {
		t.Fatal("the change lost its content")
	}
	if len(repair.Dropped) != 1 || repair.Dropped[0] != "$.background" {
		t.Fatalf("dropped paths = %v", repair.Dropped)
	}
	if !repair.Clamped {
		t.Fatal("an out-of-range particle count was not clamped")
	}
	if notice := seedRepairNotice(repair, true); notice == "" {
		t.Fatal("the repair is invisible to the user")
	}

	ambiguous := json.RawMessage(`{"premise":"A rainy street.","things":[{"kind":4,"name":"Nowhere"}]}`)
	if _, _, rejection := decodeReimagineBrief(ambiguous, state); rejection == nil {
		t.Fatal("a thing pointing at a missing kind was accepted")
	} else if rejection.Stage != "reimagine_decode" {
		t.Fatalf("stage = %q", rejection.Stage)
	}
}

// Personal AI must be able to keep modifying a world it already created. A
// person says "add another lamp": the brief names one new thing and no kinds,
// and the change still appends to the same world, keeping every existing id.
func TestConsecutiveModificationAppendsToTheSameWorld(t *testing.T) {
	state := composedWorldState(t, `{
		"premise":"A rainy street at night.",
		"kinds":[{"name":"Street"}],
		"things":[{"kind":0,"name":"Wet street"}]
	}`, "Rain City")
	var before objects.OpenWorldState
	if err := json.Unmarshal(state, &before); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if len(before.Nodes) != 1 {
		t.Fatalf("fixture has %d nodes", len(before.Nodes))
	}
	existingID := before.Nodes[0].ID

	steps := []struct {
		name  string
		brief string
	}{
		{"thing by kind index", `{"things":[{"kind":0,"name":"A second lamp"}]}`},
		{"thing by exact kind name", `{"things":[{"kind":"Street","name":"A third lamp"}]}`},
		{"look change only", `{"experience":{"surfaces":["live"],"primary":"live",
			"live":{"particles":{"kind":"rain","count":30}}}}`},
	}
	wantNodes := len(before.Nodes)
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			seed, _, rejection := decodeReimagineBrief(json.RawMessage(step.brief), state)
			if rejection != nil {
				t.Fatalf("change rejected at %s: %s", rejection.Stage, rejection.Detail)
			}
			result, err := worldseed.Reshape(state, seed)
			if err != nil {
				t.Fatalf("reshape: %v", err)
			}
			if strings.Contains(step.brief, "things") {
				wantNodes++
			}
			if len(result.State.Nodes) != wantNodes {
				t.Fatalf("nodes = %d, want %d", len(result.State.Nodes), wantNodes)
			}
			if result.State.Nodes[0].ID != existingID {
				t.Fatalf("existing node id changed: %q", result.State.Nodes[0].ID)
			}
			if result.State.Premise != "A rainy street at night." {
				t.Fatalf("premise was overwritten: %q", result.State.Premise)
			}
			state = mustMarshalState(t, result.State)
		})
	}
}

// A kind the world does not have is not guessed, even though its name would be
// a plausible thing to add.
func TestReimagineRejectsAnUnknownKind(t *testing.T) {
	state := composedWorldState(t, `{
		"premise":"A rainy street at night.",
		"kinds":[{"name":"Street"}],
		"things":[{"kind":0,"name":"Wet street"}]
	}`, "Rain City")
	_, _, rejection := decodeReimagineBrief(
		json.RawMessage(`{"things":[{"kind":"Lighthouse","name":"A lighthouse"}]}`), state)
	if rejection == nil {
		t.Fatal("a thing of an unknown kind was accepted")
	}
	if rejection.Stage != "reimagine_decode" {
		t.Fatalf("stage = %q", rejection.Stage)
	}
}

func composedWorldState(t *testing.T, briefJSON, title string) json.RawMessage {
	t.Helper()
	brief, _, err := worldseed.DecodeRepairing(json.RawMessage(briefJSON))
	if err != nil {
		t.Fatalf("decode fixture brief: %v", err)
	}
	composition, err := worldseed.Compose(brief, title)
	if err != nil {
		t.Fatalf("compose fixture world: %v", err)
	}
	encoded, err := json.Marshal(composition.Content)
	if err != nil {
		t.Fatal(err)
	}
	state, _, err := objects.ValidateCreate(objects.CreateInput{
		ObjectType: objects.TypeOpenWorld, Title: title, Content: encoded,
	})
	if err != nil {
		t.Fatalf("validate fixture world: %v", err)
	}
	return state
}

func mustMarshalState(t *testing.T, state objects.OpenWorldState) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	return encoded
}

// Out-of-range presentation numbers are bounded, never re-guessed, and the
// strict compiler still sees a valid brief.
func TestWorldCreateClampsOutOfRangePresentation(t *testing.T) {
	brief := `{
		"premise":"A rainy street at night.",
		"kinds":[{"name":"Street","hue":900}],
		"things":[{"kind":0,"name":"Street","x":4,"y":-2}],
		"experience":{
			"surfaces":["live"],
			"primary":"live",
			"live":{"particles":{"kind":"rain","count":400,"hue":500,"size":20,"speed":9}}
		}
	}`
	content, repair, rejection := buildWorldCreate(worldCreateActionInput{
		Title: "Rain City", Brief: json.RawMessage(brief),
	})
	if rejection != nil {
		t.Fatalf("out-of-range presentation was rejected at %s: %s", rejection.Stage, rejection.Detail)
	}
	if repair == nil || !repair.Clamped {
		t.Fatal("clamping was not recorded")
	}
	var composed map[string]any
	if err := json.Unmarshal(content, &composed); err != nil {
		t.Fatalf("decode composed content: %v", err)
	}
	text := string(content)
	for _, over := range []string{`"count":400`, `"hue":500`, `"size":20`, `"speed":9`} {
		if strings.Contains(text, over) {
			t.Fatalf("composed content still carries %s", over)
		}
	}
	if !strings.Contains(text, `"count":120`) {
		t.Fatalf("particle count was not clamped to its documented bound: %s", truncateActionDetail(text))
	}
}

func rejectionDetail(rejection *actionRejection) string {
	if rejection == nil {
		return ""
	}
	return rejection.Detail
}

func TestWorldSeedGuideEnumeratesMotionAndFieldTypes(t *testing.T) {
	system, _, _, err := buildPrompt(
		&Profile{Name: "P2E Real Check"},
		nil,
		nil,
		nil,
		"Create a 3D room with a lamp.",
		"create",
	)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	for _, phrase := range []string{
		"none|pulse|float|sway|flicker|breathe|orbit",
		"flag uses true/false",
		"Unknown enum values reject the seed",
	} {
		if !strings.Contains(system, phrase) {
			t.Fatalf("world seed guide lacks %q", phrase)
		}
	}
}

func TestWorldCreationPromptRequiresStrictActionShape(t *testing.T) {
	system, _, _, err := buildPrompt(
		&Profile{Name: "P2E Real Check"},
		nil,
		nil,
		nil,
		"Create a 3D room with a lamp.",
		"create",
	)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	for _, phrase := range []string{
		"top level",
		"unknown fields are rejected",
		"views[].kind and player",
	} {
		if !strings.Contains(system, phrase) {
			t.Fatalf("world-creation prompt lacks %q", phrase)
		}
	}
}
