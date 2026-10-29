// SPDX-License-Identifier: Apache-2.0
package personalai

import (
	"encoding/json"
	"strings"
	"testing"
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
