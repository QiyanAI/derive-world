// SPDX-License-Identifier: Apache-2.0
package worldseed

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeRepairingDropsUnknownFields(t *testing.T) {
	raw := json.RawMessage(`{
		"premise":"A rainy street at night.",
		"background":"heavy rain",
		"kinds":[{"name":"Street","hue":210,"style":"wet"}],
		"things":[{"kind":0,"name":"Street","colour":"grey"}]
	}`)
	brief, dropped, err := DecodeRepairing(raw)
	if err != nil {
		t.Fatalf("decode repairing: %v", err)
	}
	if brief.Premise != "A rainy street at night." {
		t.Fatalf("premise changed: %q", brief.Premise)
	}
	if len(brief.Kinds) != 1 || len(brief.Things) != 1 {
		t.Fatalf("content lost: kinds=%d things=%d", len(brief.Kinds), len(brief.Things))
	}
	want := map[string]bool{
		"$.background":       true,
		"$.kinds[0].style":   true,
		"$.things[0].colour": true,
	}
	for path := range want {
		found := false
		for _, got := range dropped {
			if got == path {
				found = true
			}
		}
		if !found {
			t.Errorf("dropped paths %v lack %q", dropped, path)
		}
	}
}

func TestDecodeRepairingNeverTouchesACleanSeed(t *testing.T) {
	raw := json.RawMessage(`{
		"premise":"A rainy street at night.",
		"kinds":[{"name":"Street"}],
		"things":[{"kind":0,"name":"Street"}]
	}`)
	if _, dropped, err := DecodeRepairing(raw); err != nil || len(dropped) != 0 {
		t.Fatalf("clean seed was repaired: dropped=%v err=%v", dropped, err)
	}
}

// Custom surfaces have their own declarative schema. A real brief repair must
// leave the custom block byte-for-byte intact, and a broken custom block must
// be rejected rather than having its content deleted.
func TestDecodeRepairingLeavesCustomSurfaceIntact(t *testing.T) {
	raw := json.RawMessage(`{
		"premise":"A star map you can play with your hands.",
		"mood":"quiet",
		"experience":{
			"surfaces":["custom"],
			"primary":"custom",
			"custom":{
				"title":"Star instrument","width":1200,"height":800,
				"vars":{"pad_a":false},
				"nodes":[{"id":"pad_a","type":"dot","x":420,"y":420,"r":56,
					"on_click":[{"type":"toggle_var","name":"pad_a"}]}]
			}
		}
	}`)
	brief, dropped, err := DecodeRepairing(raw)
	if err != nil {
		t.Fatalf("decode repairing: %v", err)
	}
	if len(dropped) != 1 || dropped[0] != "$.mood" {
		t.Fatalf("unexpected repair: %v", dropped)
	}
	if brief.Experience == nil || !strings.Contains(string(brief.Experience.Custom), `"pad_a"`) {
		t.Fatal("custom surface was lost or rewritten")
	}

	broken := json.RawMessage(`{
		"premise":"A star map.",
		"experience":{"primary":"custom","custom":{"title":"Star","width":1200,"height":800,
			"vars":{},"nodes":[{"id":"a","type":"dot","x":1,"y":1,"r":5,"mystery":true}]}}
	}`)
	if _, _, err := DecodeRepairing(broken); err == nil {
		t.Fatal("an unsupported custom key was deleted instead of rejected")
	}
}

// A seed whose required content disappears with the unknown fields is not
// repaired silently: the strict error reaches the caller unchanged.
func TestDecodeRepairingDoesNotInventMissingContent(t *testing.T) {
	raw := json.RawMessage(`{
		"premise":"A rainy street at night.",
		"kindz":[{"name":"Street"}],
		"things":[{"kind":0,"name":"Street"}]
	}`)
	if _, _, err := DecodeRepairing(raw); err == nil {
		t.Fatal("a world whose kinds vanished was accepted")
	} else if !strings.Contains(err.Error(), "kinds") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, _, err := DecodeRepairing(json.RawMessage(`{"premise":42}`)); err == nil {
		t.Fatal("a type error was treated as an unknown field")
	}
}

func TestClampBriefNumbersBoundsPresentation(t *testing.T) {
	raw := json.RawMessage(`{
		"premise":"A rainy street at night.",
		"kinds":[{"name":"Street","hue":900}],
		"things":[{"kind":0,"name":"Street","x":4,"y":-2}],
		"experience":{
			"surfaces":["live"],
			"primary":"live",
			"live":{
				"speed":40,
				"timer":99999,
				"particles":{"kind":"rain","count":400,"hue":500,"size":20,"speed":9}
			}
		}
	}`)
	clamped, changed := ClampBriefNumbers(raw)
	if !changed {
		t.Fatal("out-of-range numbers were not clamped")
	}
	var document map[string]any
	if err := json.Unmarshal(clamped, &document); err != nil {
		t.Fatalf("decode clamped: %v", err)
	}
	kinds := document["kinds"].([]any)
	kind := kinds[0].(map[string]any)
	if hue := kind["hue"].(float64); hue < 0 || hue > 359 {
		t.Fatalf("kind hue %v outside its bound", hue)
	}
	if _, err := Decode(clamped); err != nil {
		t.Fatalf("clamped seed still fails strict decode: %v", err)
	}
	encoded := string(clamped)
	for _, over := range []string{`"hue":500`, `"count":400`, `"size":20`, `"speed":9`, `"timer":99999`, `"speed":40`} {
		if strings.Contains(encoded, over) {
			t.Fatalf("clamped seed still carries %s", over)
		}
	}
	if !strings.Contains(encoded, `"count":120`) {
		t.Fatalf("particle count not clamped: %s", encoded)
	}
}

func TestClampBriefNumbersLeavesInRangeSeedsAlone(t *testing.T) {
	raw := json.RawMessage(`{
		"premise":"A rainy street at night.",
		"kinds":[{"name":"Street","hue":210}],
		"things":[{"kind":0,"name":"Street","x":0.5,"y":0.5}]
	}`)
	clamped, changed := ClampBriefNumbers(raw)
	if changed {
		t.Fatal("an in-range seed was reported as changed")
	}
	if string(clamped) != string(raw) {
		t.Fatalf("an in-range seed was rewritten: %s", clamped)
	}
}

// Absent numbers stay absent: clamping must never invent a value the user did
// not ask for.
func TestClampBriefNumbersKeepsAbsentNumbersAbsent(t *testing.T) {
	raw := json.RawMessage(`{"premise":"A street.","kinds":[{"name":"Street"}]}`)
	clamped, changed := ClampBriefNumbers(raw)
	if changed {
		t.Fatal("a seed without numbers was reported as changed")
	}
	if strings.Contains(string(clamped), "hue") {
		t.Fatalf("clamping invented values: %s", clamped)
	}
}
