// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

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
		Variables: make(map[string]string, len(state.Variables)),
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
	for name, value := range state.Variables {
		switch value.(type) {
		case string:
			context.Variables[name] = "text"
		case float64:
			context.Variables[name] = "number"
		case bool:
			context.Variables[name] = "flag"
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

func (world openWorldCapabilityWorld) Variable(name string) (any, bool) {
	value, ok := world.state.Variables[name]
	return value, ok
}

func (world openWorldCapabilityWorld) SetVariable(name string, value any) error {
	if !ValidOpenWorldCreateRef(name) {
		return fmt.Errorf("%w: experience variable name", ErrInvalidRequest)
	}
	switch typed := value.(type) {
	case string:
		if runeLength(typed) > maxOpenWorldVariableTextRune {
			return fmt.Errorf("%w: experience variable value", ErrInvalidRequest)
		}
	case float64, bool:
	default:
		return fmt.Errorf("%w: experience variable value", ErrInvalidRequest)
	}
	existing, exists := world.state.Variables[name]
	if !exists && len(world.state.Variables) >= maxOpenWorldVariables {
		return fmt.Errorf("%w: experience variables", ErrInvalidRequest)
	}
	if exists {
		existingType, _ := variableScalarType(existing)
		nextType, _ := variableScalarType(value)
		if existingType != nextType {
			return fmt.Errorf("%w: experience variable type", ErrInvalidRequest)
		}
	}
	if world.state.Variables == nil {
		world.state.Variables = map[string]any{}
	}
	world.state.Variables[name] = value
	return nil
}

func (world openWorldCapabilityWorld) CreateEntity(kindID, name string, values map[string]any) (string, error) {
	kind := findOpenWorldKind(world.state, kindID)
	if kind == nil {
		return "", fmt.Errorf("%w: experience kind", ErrInvalidRequest)
	}
	name = strings.TrimSpace(name)
	if name == "" || runeLength(name) > maxOpenWorldNodeNameRunes {
		return "", fmt.Errorf("%w: experience entity name", ErrInvalidRequest)
	}
	if len(world.state.Nodes) >= maxOpenWorldNodes {
		return "", fmt.Errorf("%w: experience entity count", ErrInvalidRequest)
	}
	nodeValues := make(map[string]any, len(kind.Fields))
	for _, field := range kind.Fields {
		value, ok := values[field.Key]
		if !ok {
			switch field.Type {
			case OpenWorldFieldText:
				nodeValues[field.Key] = ""
			case OpenWorldFieldNumber:
				nodeValues[field.Key] = float64(0)
			case OpenWorldFieldFlag:
				nodeValues[field.Key] = false
			}
			continue
		}
		if err := validateOpenWorldValue(field, value, "experience.create.values"); err != nil {
			return "", err
		}
		nodeValues[field.Key] = value
	}
	for key := range values {
		if _, ok := nodeValues[key]; !ok {
			return "", fmt.Errorf("%w: experience.create.values", ErrInvalidRequest)
		}
	}
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	x, y := defaultOpenWorldPosition(len(world.state.Nodes), len(world.state.Nodes)+1)
	world.state.Nodes = append(world.state.Nodes, OpenWorldNode{
		ID: id.String(), KindID: kindID, Name: name, Values: nodeValues,
		Status: OpenWorldStatusOpen, X: x, Y: y,
	})
	return id.String(), nil
}

func variableScalarType(value any) (string, bool) {
	switch value.(type) {
	case string:
		return "text", true
	case float64:
		return "number", true
	case bool:
		return "flag", true
	default:
		return "", false
	}
}
