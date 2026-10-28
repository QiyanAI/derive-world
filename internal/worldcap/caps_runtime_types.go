// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"fmt"
	"strings"
)

// entityAction binds one concrete thing to one behavior. This is what lets a
// scene, a game or a workspace act on the thing the user actually touched,
// instead of forcing one global click behavior for an entire world.
type entityAction struct {
	EntityID   string `json:"entity_id"`
	BehaviorID string `json:"behavior_id"`
	Label      string `json:"label,omitempty"`
}

func validateEntityActions(actions []entityAction, ctx ValidateContext) error {
	if len(actions) > 40 {
		return fmt.Errorf("%w: entity_actions", ErrInvalid)
	}
	seen := map[string]bool{}
	for index := range actions {
		action := &actions[index]
		if !ctx.World.HasEntity(action.EntityID) || seen[action.EntityID] {
			return fmt.Errorf("%w: entity_actions/%d/entity_id", ErrInvalid, index)
		}
		seen[action.EntityID] = true
		if !validLocalID(action.BehaviorID) || !ctx.World.HasBehavior(action.BehaviorID) {
			return fmt.Errorf("%w: entity_actions/%d/behavior_id", ErrInvalid, index)
		}
		action.Label = strings.TrimSpace(action.Label)
		if runeLength(action.Label) > 80 {
			return fmt.Errorf("%w: entity_actions/%d/label", ErrInvalid, index)
		}
	}
	return nil
}

func validSceneMotion(value string) bool {
	switch value {
	case "", "none", "pulse", "float", "sway", "flicker", "breathe", "orbit":
		return true
	default:
		return false
	}
}
