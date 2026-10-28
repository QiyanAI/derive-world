// SPDX-License-Identifier: Apache-2.0
package worldcap

import "fmt"

// ExecuteBehavior runs one validated behavior once. Conditions and actions
// never chain, so a world cannot loop through its own interaction graph.
func ExecuteBehavior(registry *Registry, behavior *Behavior, event Event, world WorldAccess) ([]AppliedAction, error) {
	if registry == nil || behavior == nil || world == nil {
		return nil, fmt.Errorf("%w: behavior", ErrInvalid)
	}
	if len(behavior.Actions) == 0 || len(behavior.Actions) > maxActionsPerAction {
		return nil, fmt.Errorf("%w: behavior actions", ErrInvalid)
	}
	if behavior.Condition != nil &&
		!EvaluateCondition(behavior.Condition, event, "", world) {
		return nil, nil
	}
	applied := make([]AppliedAction, 0, len(behavior.Actions))
	for index := range behavior.Actions {
		action := &behavior.Actions[index]
		capability, ok := registry.Behavior(action.Capability)
		if !ok {
			return nil, fmt.Errorf("%w: unknown behavior %q", ErrInvalid, action.Capability)
		}
		if !descriptorHasAction(capability.Descriptor(), action.Action) {
			return nil, fmt.Errorf("%w: unknown action %q", ErrInvalid, action.Action)
		}
		changes, err := capability.Execute(action.Action, action.Params, event, world)
		if err != nil {
			return nil, err
		}
		applied = append(applied, changes...)
	}
	return applied, nil
}

// FindBehavior returns one behavior by local id.
func FindBehavior(experience *Experience, behaviorID string) *Behavior {
	if experience == nil {
		return nil
	}
	for index := range experience.Behaviors {
		if experience.Behaviors[index].ID == behaviorID {
			return &experience.Behaviors[index]
		}
	}
	return nil
}
