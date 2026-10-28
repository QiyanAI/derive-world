// SPDX-License-Identifier: Apache-2.0
package worldseed

import (
	"testing"

	"github.com/QiyanAI/derive-world/internal/worldcap"
)

func findNode(root *worldcap.ViewNode, capability string) *worldcap.ViewNode {
	if root == nil {
		return nil
	}
	if root.Capability == capability {
		return root
	}
	for index := range root.Children {
		if found := findNode(&root.Children[index], capability); found != nil {
			return found
		}
	}
	return nil
}

func TestComposeScene3DAudioAndMediaSlot(t *testing.T) {
	brief, err := Decode([]byte(`{
		"premise":"A planet where footsteps make echoes.",
		"kinds":[
			{"name":"Traveler","hue":180},
			{"name":"Beacon","hue":48},
			{"name":"Pillar","hue":120,"solid":true}
		],
		"things":[
			{"kind":0,"name":"Traveler","x":0.5,"y":0.5},
			{"kind":1,"name":"First beacon","x":0.66,"y":0.5},
			{"kind":2,"name":"Pillar","x":0.8,"y":0.7}
		],
		"experience":{
			"surfaces":["scene3d","media"],
			"primary":"scene3d",
			"scene3d":{"title":"Echo Planet","camera":"follow","player":0,"gravity":18,
				"views":[{"kind":2,"primitive":"cylinder","collider":"box"}]},
			"audio":{"title":"Echo","tone":{"frequency":196,"wave":"sine","volume":0.06,"pulse":0.3},
				"cues":[{"variable":"signals","mode":"increase","tone":{"frequency":520}}]},
			"media":{"title":"A photo","kind":"image","caption":"Upload one."}
		}
	}`))
	if err != nil {
		t.Fatalf("decode brief: %v", err)
	}
	composition, err := Compose(brief, "Echo Planet")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	experience := composition.Content.Experience
	if experience == nil {
		t.Fatal("no experience")
	}
	scene := findNode(experience.Root, "core.scene3d")
	if scene == nil {
		t.Fatal("scene3d node missing")
	}
	if findNode(experience.Root, "core.media") == nil {
		t.Fatal("media node missing")
	}
	if findNode(experience.Root, "core.audio") == nil {
		t.Fatal("audio node missing")
	}
	if len(experience.Behaviors) == 0 {
		t.Fatal("expected a sense behavior for the audio cue")
	}
	foundSignal := false
	for _, behavior := range experience.Behaviors {
		for _, action := range behavior.Actions {
			if action.Capability == "core.variable" {
				foundSignal = true
			}
		}
	}
	if !foundSignal {
		t.Fatal("expected a variable action behind the numeric cue")
	}
}

func TestComposeMediaOnlyWorld(t *testing.T) {
	brief, err := Decode([]byte(`{
		"premise":"A single photograph to look at.",
		"experience":{
			"surfaces":["media"],
			"primary":"media",
			"media":{"title":"One photograph","kind":"image","caption":"Upload it."}
		}
	}`))
	if err != nil {
		t.Fatalf("decode brief: %v", err)
	}
	composition, err := Compose(brief, "One photograph")
	if err != nil {
		t.Fatalf("compose media-only: %v", err)
	}
	if composition.Content.Experience == nil ||
		findNode(composition.Content.Experience.Root, "core.media") == nil {
		t.Fatal("media-only world did not compose")
	}
}
