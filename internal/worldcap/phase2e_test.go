// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func phase2eContext() *Context {
	return &Context{
		Kinds: map[string]KindContext{
			"kind-1": {ID: "kind-1", Name: "Beacon", Fields: map[string]string{}},
		},
		Entities: map[string]EntityContext{
			"entity-1": {ID: "entity-1", KindID: "kind-1"},
		},
		Behaviors: map[string]bool{"act": true},
		Variables: map[string]string{"signals": "number", "ready": "flag"},
	}
}

func TestMediaCapabilityValidatesAssetsAndVariables(t *testing.T) {
	ctx := ValidateContext{World: phase2eContext()}
	asset := uuid.NewString()
	valid := json.RawMessage(`{"kind":"video","asset_id":"` + asset + `","caption":"A film","fit":"cover","controls":true,"loop":true,"muted":true,"autoplay":false,"play_variable":"ready"}`)
	if _, err := (mediaCapability{}).Validate(valid, ctx); err != nil {
		t.Fatalf("valid media rejected: %v", err)
	}
	cases := []string{
		`{"kind":"audio"}`,
		`{"kind":"image","asset_id":"not-a-uuid"}`,
		`{"kind":"image","play_variable":"ready"}`,
		`{"kind":"image","asset_id":"` + asset + `","play_variable":"missing"}`,
		`{"kind":"image","asset_id":"` + asset + `","unknown":true}`,
	}
	for _, raw := range cases {
		if _, err := (mediaCapability{}).Validate(json.RawMessage(raw), ctx); err == nil {
			t.Fatalf("media accepted invalid props: %s", raw)
		}
	}
}

func TestAudioCapabilityRequiresOneCueSourceAndTypedVariable(t *testing.T) {
	ctx := ValidateContext{World: phase2eContext()}
	valid := json.RawMessage(`{
		"title":"Echo",
		"tone":{"frequency":220,"wave":"sine","volume":0.08,"pulse":0.5},
		"volume":0.4,
		"cues":[{"variable":"signals","mode":"increase","tone":{"frequency":520,"wave":"triangle","volume":0.1}}]
	}`)
	if _, err := (audioCapability{}).Validate(valid, ctx); err != nil {
		t.Fatalf("valid audio rejected: %v", err)
	}
	asset := uuid.NewString()
	fileCue := json.RawMessage(`{"cues":[{"variable":"ready","mode":"truthy","asset_id":"` + asset + `"}]}`)
	if _, err := (audioCapability{}).Validate(fileCue, ctx); err != nil {
		t.Fatalf("file cue rejected: %v", err)
	}
	cases := []string{
		`{"cues":[{"variable":"signals","mode":"truthy","tone":{"frequency":440}}]}`,
		`{"cues":[{"variable":"missing","mode":"change","tone":{"frequency":440}}]}`,
		`{"cues":[{"variable":"signals","mode":"increase"}]}`,
		`{"cues":[{"variable":"signals","mode":"increase","asset_id":"` + asset + `","tone":{"frequency":440}}]}`,
		`{"ambient_asset_id":"` + asset + `","tone":{"frequency":440}}`,
	}
	for _, raw := range cases {
		if _, err := (audioCapability{}).Validate(json.RawMessage(raw), ctx); err == nil {
			t.Fatalf("audio accepted invalid props: %s", raw)
		}
	}
}

func TestScene3DCapabilityValidatesViewsAndPlayer(t *testing.T) {
	ctx := ValidateContext{World: phase2eContext()}
	model := uuid.NewString()
	valid := json.RawMessage(`{
		"title":"Echo planet",
		"sky_hue":220,"ground_hue":140,"fog":0.02,
		"camera_mode":"follow","camera_distance":14,"camera_height":8,"fov":55,
		"gravity":18,"ambient_light":0.8,"player_enabled":true,"player_node_id":"entity-1",
		"player_speed":4,"player_height":1,
		"views":[{"kind_id":"kind-1","primitive":"model","model_asset_id":"` + model + `","scale":1,"elevation":2,"hue":48,"opacity":0.9,"collider":"box","label":"always"}],
		"show_links":true,"activate_behavior_id":"act","bump_behavior_id":"act",
		"entity_actions":[{"entity_id":"entity-1","behavior_id":"act","label":"touch"}]
	}`)
	if _, err := (scene3DCapability{}).Validate(valid, ctx); err != nil {
		t.Fatalf("valid scene3d rejected: %v", err)
	}
	cases := []string{
		`{"views":[{"kind_id":"missing"}]}`,
		`{"views":[{"kind_id":"kind-1"},{"kind_id":"kind-1"}]}`,
		`{"player_node_id":"missing"}`,
		`{"views":[{"kind_id":"kind-1","primitive":"model","model_asset_id":"bad"}]}`,
		`{"activate_behavior_id":"missing"}`,
		`{"gravity":99}`,
	}
	for _, raw := range cases {
		if _, err := (scene3DCapability{}).Validate(json.RawMessage(raw), ctx); err == nil {
			t.Fatalf("scene3d accepted invalid props: %s", raw)
		}
	}
}

func TestCollectAssetRefsAndRemapKeepsAssetIDs(t *testing.T) {
	asset := uuid.NewString()
	model := uuid.NewString()
	experience := &Experience{
		Version: 1,
		Root: &ViewNode{
			ID: "root", Capability: "core.stack",
			Props: json.RawMessage(`{"direction":"vertical","gap":0,"padding":0,"align":"stretch","tone":"plain"}`),
			Children: []ViewNode{
				{ID: "media", Capability: "core.media", Props: json.RawMessage(`{"kind":"image","asset_id":"` + asset + `"}`)},
				{ID: "audio", Capability: "core.audio", Props: json.RawMessage(`{"cues":[{"variable":"signals","mode":"increase","asset_id":"` + asset + `"}]}`)},
				{ID: "scene", Capability: "core.scene3d", Props: json.RawMessage(`{"player_node_id":"entity-1","views":[{"kind_id":"kind-1","model_asset_id":"` + model + `"}]}`)},
			},
		},
	}
	refs := CollectAssetRefs(experience)
	if len(refs) != 3 {
		t.Fatalf("refs = %+v, want 3", refs)
	}
	remapped, err := RemapExperience(experience,
		map[string]string{"kind-1": "kind-2"},
		map[string]string{"entity-1": "entity-2", "player": "player-2"})
	if err != nil {
		t.Fatalf("remap: %v", err)
	}
	encoded, _ := json.Marshal(remapped)
	if !bytes.Contains(encoded, []byte(asset)) || !bytes.Contains(encoded, []byte(model)) {
		t.Fatal("remap rewrote an asset id")
	}
	sceneProps := remapped.Root.Children[2].Props
	if bytes.Contains(sceneProps, []byte("kind-1")) {
		t.Fatal("remap did not rewrite kind_id")
	}
	if bytes.Contains(sceneProps, []byte("entity-1")) {
		t.Fatal("remap did not rewrite player_node_id")
	}
}
