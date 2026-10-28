// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"sort"
)

// AssetRef is one media reference declared by an experience document.
// ExpectedKind is the media kind the capability requires; an empty kind means
// the reference is accepted from any media category (no current module does
// this, but it keeps the collector forward-compatible).
type AssetRef struct {
	ID           string
	ExpectedKind string
}

// CollectAssetRefs walks an experience and returns the deduplicated asset ids
// its built-in presentation modules declare. The caller uses the same list for
// save-time authorization and publication freezing, so a reference cannot be
// valid in one path and invisible in another.
//
// Unknown capabilities are ignored: their validators own their own reference
// rules and no unknown capability can be persisted through the product.
func CollectAssetRefs(experience *Experience) []AssetRef {
	if experience == nil || experience.Root == nil {
		return nil
	}
	refs := make([]AssetRef, 0)
	collectNodeAssetRefs(experience.Root, &refs)
	sort.Slice(refs, func(left, right int) bool {
		if refs[left].ID != refs[right].ID {
			return refs[left].ID < refs[right].ID
		}
		return refs[left].ExpectedKind < refs[right].ExpectedKind
	})
	unique := refs[:0]
	for _, ref := range refs {
		if ref.ID == "" {
			continue
		}
		if len(unique) > 0 && unique[len(unique)-1] == ref {
			continue
		}
		unique = append(unique, ref)
	}
	return unique
}

func collectNodeAssetRefs(node *ViewNode, refs *[]AssetRef) {
	if node == nil {
		return
	}
	switch node.Capability {
	case "core.media":
		var props struct {
			Kind    string `json:"kind"`
			AssetID string `json:"asset_id"`
		}
		if json.Unmarshal(node.Props, &props) == nil && props.AssetID != "" {
			*refs = append(*refs, AssetRef{ID: props.AssetID, ExpectedKind: props.Kind})
		}
	case "core.audio":
		var props struct {
			AmbientAssetID string `json:"ambient_asset_id"`
			Cues           []struct {
				AssetID string `json:"asset_id"`
			} `json:"cues"`
		}
		if json.Unmarshal(node.Props, &props) == nil {
			if props.AmbientAssetID != "" {
				*refs = append(*refs, AssetRef{ID: props.AmbientAssetID, ExpectedKind: "audio"})
			}
			for _, cue := range props.Cues {
				if cue.AssetID != "" {
					*refs = append(*refs, AssetRef{ID: cue.AssetID, ExpectedKind: "audio"})
				}
			}
		}
	case "core.scene3d":
		var props struct {
			Views []struct {
				ModelAssetID string `json:"model_asset_id"`
			} `json:"views"`
		}
		if json.Unmarshal(node.Props, &props) == nil {
			for _, view := range props.Views {
				if view.ModelAssetID != "" {
					*refs = append(*refs, AssetRef{ID: view.ModelAssetID, ExpectedKind: "model"})
				}
			}
		}
	}
	for index := range node.Children {
		collectNodeAssetRefs(&node.Children[index], refs)
	}
}
