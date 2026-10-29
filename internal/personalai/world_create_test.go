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
			name: "unknown brief field",
			input: worldCreateActionInput{Title: "Room", Brief: json.RawMessage(`{
				"premise":"A quiet room.",
				"title":"inside the brief",
				"kinds":[{"name":"Room"}],
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
