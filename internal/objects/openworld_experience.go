// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"fmt"

	"github.com/QiyanAI/derive-world/internal/worldcap"
)

// openWorldCapabilityContext turns the current world into the read-only
// reference context capability validators use. It is derived from the same
// state that is about to be persisted, never from a separate database.
func openWorldCapabilityContext(state *OpenWorldState) *worldcap.Context {
	context := &worldcap.Context{
		Kinds:     make(map[string]worldcap.KindContext, len(state.Kinds)),
		Entities:  make(map[string]worldcap.EntityContext, len(state.Nodes)),
		Behaviors: map[string]bool{},
	}
	for _, kind := range state.Kinds {
		fields := make(map[string]string, len(kind.Fields))
		for _, field := range kind.Fields {
			fields[field.Key] = field.Type
		}
		context.Kinds[kind.ID] = worldcap.KindContext{
			ID: kind.ID, Name: kind.Name, Fields: fields,
		}
	}
	for _, node := range state.Nodes {
		context.Entities[node.ID] = worldcap.EntityContext{
			ID: node.ID, KindID: node.KindID,
		}
	}
	if state.Experience != nil {
		for _, behavior := range state.Experience.Behaviors {
			context.Behaviors[behavior.ID] = true
		}
	}
	return context
}

func validateOpenWorldExperience(state *OpenWorldState) error {
	if state.Experience == nil {
		return nil
	}
	if err := worldcap.ValidateExperience(state.Experience, openWorldCapabilityContext(state)); err != nil {
		return fmt.Errorf("%w: /experience: %v", ErrInvalidRequest, err)
	}
	return nil
}

// openWorldCapabilityWorld is the bounded mutation surface a behavior sees.
// It can only read and write fields of things in this one world.
type openWorldCapabilityWorld struct {
	state *OpenWorldState
}

func (world openWorldCapabilityWorld) HasEntity(entityID string) bool {
	return findOpenWorldNode(world.state, entityID) != nil
}

func (world openWorldCapabilityWorld) FieldType(entityID, field string) (string, bool) {
	node := findOpenWorldNode(world.state, entityID)
	if node == nil {
		return "", false
	}
	if field == OpenWorldStatusField {
		return "status", true
	}
	definition := openWorldFieldFor(world.state, node, field)
	if definition == nil {
		return "", false
	}
	return definition.Type, true
}

func (world openWorldCapabilityWorld) Value(entityID, field string) (any, bool) {
	node := findOpenWorldNode(world.state, entityID)
	if node == nil {
		return nil, false
	}
	if field == OpenWorldStatusField {
		return node.Status, true
	}
	value, ok := node.Values[field]
	return value, ok
}

func (world openWorldCapabilityWorld) SetValue(entityID, field string, value any) error {
	node := findOpenWorldNode(world.state, entityID)
	if node == nil {
		return fmt.Errorf("%w: experience entity", ErrInvalidRequest)
	}
	definition := openWorldFieldFor(world.state, node, field)
	if definition == nil {
		return fmt.Errorf("%w: experience field", ErrInvalidRequest)
	}
	if err := validateOpenWorldValue(*definition, value, "experience.value"); err != nil {
		return err
	}
	if node.Values == nil {
		node.Values = map[string]any{}
	}
	node.Values[field] = value
	return nil
}

func (world openWorldCapabilityWorld) Status(entityID string) (string, bool) {
	node := findOpenWorldNode(world.state, entityID)
	if node == nil {
		return "", false
	}
	return node.Status, true
}

func (world openWorldCapabilityWorld) SetStatus(entityID, status string) error {
	if status != OpenWorldStatusOpen && status != OpenWorldStatusDone {
		return fmt.Errorf("%w: experience status", ErrInvalidRequest)
	}
	node := findOpenWorldNode(world.state, entityID)
	if node == nil {
		return fmt.Errorf("%w: experience entity", ErrInvalidRequest)
	}
	node.Status = status
	return nil
}
