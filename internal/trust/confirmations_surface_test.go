// SPDX-License-Identifier: Apache-2.0
package trust

import (
	"reflect"
	"testing"
)

func TestCollectExperienceSurfacesWalksStackAndTabs(t *testing.T) {
	root := experienceViewNode{
		Capability: "core.stack",
		Children: []experienceViewNode{
			{Capability: "core.audio"},
			{
				Capability: "core.tabs",
				Children: []experienceViewNode{
					{Capability: "core.scene3d"},
					{Capability: "core.media"},
					{Capability: "core.live"},
					{Capability: "core.scene3d"},
				},
			},
		},
	}
	got := []string{}
	collectExperienceSurfaces(root, &got, map[string]bool{})
	want := []string{"audio", "scene3d", "media", "live"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("surfaces = %v, want %v", got, want)
	}
	primary := ""
	for _, surface := range got {
		if surface != "audio" {
			primary = surface
			break
		}
	}
	if primary != "scene3d" {
		t.Fatalf("primary = %q, want scene3d", primary)
	}
}
