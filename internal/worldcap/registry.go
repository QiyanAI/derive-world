// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Presentation is one implementation of a visual/content capability. It
// validates its own params and is paired with one client renderer by ID. The
// world core does not know what the capability draws.
type Presentation interface {
	Descriptor() Descriptor
	// Validate returns normalized props. It must reject unknown fields and
	// every out-of-range value; the normalized bytes are what get persisted.
	Validate(props json.RawMessage, ctx ValidateContext) (json.RawMessage, error)
}

// WorldAccess is the bounded state surface a behavior action may read and
// write. It cannot enumerate other objects, open files, or reach the network.
type WorldAccess interface {
	HasEntity(entityID string) bool
	FieldType(entityID, field string) (string, bool)
	Value(entityID, field string) (any, bool)
	SetValue(entityID, field string, value any) error
	Status(entityID string) (string, bool)
	SetStatus(entityID, status string) error
}

// BehaviorCapability is one implementation of a state-changing capability.
// It validates its own action params and executes only against WorldAccess.
type BehaviorCapability interface {
	Descriptor() Descriptor
	ValidateAction(action string, params json.RawMessage, ctx ValidateContext) (json.RawMessage, error)
	Execute(action string, params json.RawMessage, event Event, world WorldAccess) ([]AppliedAction, error)
}

// Registry is an immutable-after-startup set of capability modules. Adding a
// module does not change Experience, ViewNode, Behavior, Action, Condition, or
// any other core world type. Every world can use every registered module.
type Registry struct {
	presentations map[string]Presentation
	behaviors     map[string]BehaviorCapability
	frozen        bool
}

// NewRegistry builds the full default capability set. Tests may build their
// own registry and add a test capability without touching the world core.
func NewRegistry() *Registry {
	registry := &Registry{
		presentations: map[string]Presentation{},
		behaviors:     map[string]BehaviorCapability{},
	}
	for _, capability := range []Presentation{
		stackCapability{}, textCapability{}, spaceCapability{}, collectionCapability{},
		tableCapability{}, fieldCapability{}, buttonCapability{}, choiceCapability{},
		meterCapability{},
	} {
		if err := registry.RegisterPresentation(capability); err != nil {
			panic(fmt.Sprintf("worldcap: register presentation: %v", err))
		}
	}
	for _, capability := range []BehaviorCapability{stateCapability{}, transferCapability{}} {
		if err := registry.RegisterBehavior(capability); err != nil {
			panic(fmt.Sprintf("worldcap: register behavior: %v", err))
		}
	}
	return registry
}

var defaultRegistry = func() *Registry {
	registry := NewRegistry()
	registry.frozen = true
	return registry
}()

// Default returns the process-wide registry. It is complete before serving
// and is never mutated at runtime.
func Default() *Registry {
	return defaultRegistry
}

// RegisterPresentation adds one presentation module. Duplicate IDs, a
// mismatched descriptor kind, or an empty module are rejected.
func (r *Registry) RegisterPresentation(capability Presentation) error {
	if r.frozen {
		return fmt.Errorf("%w: registry is frozen", ErrInvalid)
	}
	if capability == nil {
		return fmt.Errorf("%w: nil presentation", ErrInvalid)
	}
	descriptor := capability.Descriptor()
	if descriptor.Kind != "presentation" {
		return fmt.Errorf("%w: presentation kind %q", ErrInvalid, descriptor.Kind)
	}
	if err := validateDescriptor(descriptor); err != nil {
		return err
	}
	if _, exists := r.presentations[descriptor.ID]; exists {
		return fmt.Errorf("%w: duplicate presentation %q", ErrInvalid, descriptor.ID)
	}
	if _, exists := r.behaviors[descriptor.ID]; exists {
		return fmt.Errorf("%w: ID collides with behavior %q", ErrInvalid, descriptor.ID)
	}
	r.presentations[descriptor.ID] = capability
	return nil
}

// RegisterBehavior adds one behavior module.
func (r *Registry) RegisterBehavior(capability BehaviorCapability) error {
	if r.frozen {
		return fmt.Errorf("%w: registry is frozen", ErrInvalid)
	}
	if capability == nil {
		return fmt.Errorf("%w: nil behavior", ErrInvalid)
	}
	descriptor := capability.Descriptor()
	if descriptor.Kind != "behavior" {
		return fmt.Errorf("%w: behavior kind %q", ErrInvalid, descriptor.Kind)
	}
	if err := validateDescriptor(descriptor); err != nil {
		return err
	}
	if _, exists := r.behaviors[descriptor.ID]; exists {
		return fmt.Errorf("%w: duplicate behavior %q", ErrInvalid, descriptor.ID)
	}
	if _, exists := r.presentations[descriptor.ID]; exists {
		return fmt.Errorf("%w: ID collides with presentation %q", ErrInvalid, descriptor.ID)
	}
	r.behaviors[descriptor.ID] = capability
	return nil
}

// reservedNestedKeys mirrors the HTTP actor-override body guard. A capability
// property with one of these names could never be submitted through the
// product or API, so the registry rejects it at startup instead of shipping an
// unusable module.
var reservedNestedKeys = map[string]bool{
	"version": true, "permissions": true, "user_id": true, "actor_id": true,
	"actor_user_id": true, "actor_type": true, "delegation_id": true,
	"owner_id": true, "creator_id": true,
}

func validateParamSchema(schema *ParamSchema) error {
	if schema == nil {
		return nil
	}
	for name, property := range schema.Properties {
		if reservedNestedKeys[strings.ToLower(name)] {
			return fmt.Errorf("%w: reserved capability property %q", ErrInvalid, name)
		}
		if property == nil {
			return fmt.Errorf("%w: nil capability property %q", ErrInvalid, name)
		}
		if property.Items != nil {
			if err := validateParamProperty(property.Items); err != nil {
				return err
			}
		}
		if len(property.Properties) > 0 {
			if err := validateParamSchema(&ParamSchema{Properties: property.Properties}); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateParamProperty(property *ParamProperty) error {
	if property == nil {
		return fmt.Errorf("%w: nil capability property", ErrInvalid)
	}
	if property.Items != nil {
		if err := validateParamProperty(property.Items); err != nil {
			return err
		}
	}
	return validateParamSchema(&ParamSchema{Properties: property.Properties})
}

func validateDescriptor(descriptor Descriptor) error {
	if !validSlug(descriptor.ID) {
		return fmt.Errorf("%w: capability id %q", ErrInvalid, descriptor.ID)
	}
	if descriptor.Version < 1 {
		return fmt.Errorf("%w: %s version", ErrInvalid, descriptor.ID)
	}
	if strings.TrimSpace(descriptor.Title.En) == "" || strings.TrimSpace(descriptor.Title.Zh) == "" {
		return fmt.Errorf("%w: %s title", ErrInvalid, descriptor.ID)
	}
	if descriptor.Kind == "behavior" {
		if len(descriptor.Actions) == 0 {
			return fmt.Errorf("%w: %s actions", ErrInvalid, descriptor.ID)
		}
		seen := map[string]bool{}
		for _, action := range descriptor.Actions {
			if !validSlug(action.ID) || seen[action.ID] {
				return fmt.Errorf("%w: %s action id %q", ErrInvalid, descriptor.ID, action.ID)
			}
			seen[action.ID] = true
			if strings.TrimSpace(action.Title.En) == "" || strings.TrimSpace(action.Title.Zh) == "" {
				return fmt.Errorf("%w: %s action title", ErrInvalid, descriptor.ID)
			}
			if err := validateParamSchema(action.Params); err != nil {
				return fmt.Errorf("%w: %s action params", ErrInvalid, descriptor.ID)
			}
		}
	}
	if err := validateParamSchema(descriptor.Params); err != nil {
		return err
	}
	if descriptor.ChildScope != "" && descriptor.ChildScope != "item" {
		return fmt.Errorf("%w: %s child scope", ErrInvalid, descriptor.ID)
	}
	return nil
}

// Presentation returns one registered presentation module.
func (r *Registry) Presentation(id string) (Presentation, bool) {
	capability, ok := r.presentations[id]
	return capability, ok
}

// Behavior returns one registered behavior module.
func (r *Registry) Behavior(id string) (BehaviorCapability, bool) {
	capability, ok := r.behaviors[id]
	return capability, ok
}

// Catalog returns descriptors in a deterministic order. This is the same data
// the product uses to build its capability palette and generated forms.
func (r *Registry) Catalog() []Descriptor {
	items := make([]Descriptor, 0, len(r.presentations)+len(r.behaviors))
	for _, capability := range r.presentations {
		items = append(items, capability.Descriptor())
	}
	for _, capability := range r.behaviors {
		items = append(items, capability.Descriptor())
	}
	sort.Slice(items, func(left, right int) bool {
		if items[left].Kind != items[right].Kind {
			return items[left].Kind < items[right].Kind
		}
		return items[left].ID < items[right].ID
	})
	return items
}

func validSlug(value string) bool {
	if len(value) < 1 || len(value) > 64 {
		return false
	}
	for index, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && index > 0:
		case (r == '_' || r == '-' || r == '.') && index > 0:
		default:
			return false
		}
	}
	return true
}
