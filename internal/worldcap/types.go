// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"errors"
)

// A world experience is a bounded tree of capability instances plus reusable
// behaviors. The core open-world protocol stores this document as data; it
// never interprets a capability by name outside the registry. A capability
// module owns the validation and execution of its own props.
//
// Nothing in this package executes user code, loads external resources, or
// performs network access. Capabilities interpret validated JSON only.

// ErrInvalid marks any experience document that may not be persisted or run.
var ErrInvalid = errors.New("invalid world experience")

// Experience is the optional experience document of one open world. The wire
// key is "protocol", not "version": the action body security gate bans any
// nested "version" key because it could be mistaken for an actor override.
type Experience struct {
	Version   int        `json:"protocol"`
	Root      *ViewNode  `json:"root"`
	Behaviors []Behavior `json:"behaviors,omitempty"`
}

// ViewNode is one capability instance. IDs are local, readable slugs rather
// than global object identities; entity and kind references use UUIDs.
type ViewNode struct {
	ID          string          `json:"id"`
	Capability  string          `json:"capability"`
	Props       json.RawMessage `json:"props"`
	Children    []ViewNode      `json:"children,omitempty"`
	VisibleWhen *Condition      `json:"visible_when,omitempty"`
}

// Behavior is a reusable bundle of actions. A control or entity interaction
// references it; the server executes it once, without chaining.
type Behavior struct {
	ID        string     `json:"id"`
	Name      string     `json:"name,omitempty"`
	Condition *Condition `json:"condition,omitempty"`
	Actions   []Action   `json:"actions"`
}

// Action is one behavior-capability invocation. Params are validated by the
// named capability, not by the world core.
type Action struct {
	Capability string          `json:"capability"`
	Action     string          `json:"action"`
	Params     json.RawMessage `json:"params"`
}

// Condition is a bounded expression tree over current entity fields. A leaf
// compares one field; All/Any/Not compose leaves. It cannot call, loop, or
// allocate; the server limits depth and leaf count.
type Condition struct {
	EntityID string      `json:"entity_id,omitempty"`
	Field    string      `json:"field,omitempty"`
	Op       string      `json:"op,omitempty"`
	Value    any         `json:"value,omitempty"`
	All      []Condition `json:"all,omitempty"`
	Any      []Condition `json:"any,omitempty"`
	Not      *Condition  `json:"not,omitempty"`
}

// Event is the bounded scope supplied when a behavior is invoked.
type Event struct {
	EntityID string
	Field    string
	Value    any
}

// AppliedAction is one action that actually changed state, recorded in the
// object event so the world explains itself.
type AppliedAction struct {
	Capability string `json:"capability"`
	Action     string `json:"action"`
	EntityID   string `json:"entity_id"`
	Field      string `json:"field"`
	Value      any    `json:"value,omitempty"`
}

// Localized is a two-language product label. The server catalog is the source
// of truth for the client palette and its generated forms.
type Localized struct {
	En string `json:"en"`
	Zh string `json:"zh"`
}

// Descriptor describes one registered capability to the product and to AI.
type Descriptor struct {
	ID             string             `json:"id"`
	Kind           string             `json:"kind"` // "presentation" or "behavior"
	Version        int                `json:"version"`
	Title          Localized          `json:"title"`
	Description    Localized          `json:"description"`
	AllowsChildren bool               `json:"allows_children,omitempty"`
	ChildScope     string             `json:"child_scope,omitempty"`     // "item" when children repeat per entity
	ChildKindProp  string             `json:"child_kind_prop,omitempty"` // prop that selects the child kind
	Params         *ParamSchema       `json:"params,omitempty"`
	Actions        []ActionDescriptor `json:"actions,omitempty"`
}

// ActionDescriptor describes one action of a behavior capability.
type ActionDescriptor struct {
	ID          string       `json:"id"`
	Title       Localized    `json:"title"`
	Description Localized    `json:"description"`
	Params      *ParamSchema `json:"params,omitempty"`
}

// ParamSchema is the small, deliberately bounded JSON-schema subset the
// product form generator understands. Capability validators remain the
// authority; this is a UI/description contract, never an execution language.
type ParamSchema struct {
	Type       string                    `json:"type"` // object
	Properties map[string]*ParamProperty `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

// ParamProperty is one field of a capability's props.
type ParamProperty struct {
	Type        string                    `json:"type"` // string, number, integer, boolean, array, object
	Title       Localized                 `json:"title"`
	Description Localized                 `json:"description,omitempty"`
	Format      string                    `json:"format,omitempty"` // text, textarea, template, path, kind, entity, behavior, field, item_entity, color_hue
	Enum        []string                  `json:"enum,omitempty"`
	Minimum     *float64                  `json:"minimum,omitempty"`
	Maximum     *float64                  `json:"maximum,omitempty"`
	MaxLength   int                       `json:"max_length,omitempty"`
	Items       *ParamProperty            `json:"items,omitempty"`
	Properties  map[string]*ParamProperty `json:"properties,omitempty"`
	Required    []string                  `json:"required,omitempty"`
}

// KindContext is the validation-facing view of one world kind.
type KindContext struct {
	ID     string
	Name   string
	Fields map[string]string // field key -> text | number | flag
}

// EntityContext is the validation-facing view of one world entity.
type EntityContext struct {
	ID     string
	KindID string
}

// Context is everything a capability needs to validate references. It is
// derived from the same state that is about to be persisted.
type Context struct {
	Kinds     map[string]KindContext
	Entities  map[string]EntityContext
	Behaviors map[string]bool
	Variables map[string]string // variable name -> text | number | flag
}

// KindOfEntity returns the kind context of an entity.
func (c *Context) KindOfEntity(entityID string) (KindContext, bool) {
	entity, ok := c.Entities[entityID]
	if !ok {
		return KindContext{}, false
	}
	kind, ok := c.Kinds[entity.KindID]
	return kind, ok
}

// FieldType returns one entity field's declared type, including status.
func (c *Context) FieldType(entityID, field string) (string, bool) {
	if field == "status" {
		if _, ok := c.Entities[entityID]; ok {
			return "status", true
		}
		return "", false
	}
	kind, ok := c.KindOfEntity(entityID)
	if !ok {
		return "", false
	}
	value, ok := kind.Fields[field]
	return value, ok
}

// HasEntity reports whether an explicit entity id exists.
func (c *Context) HasEntity(entityID string) bool {
	_, ok := c.Entities[entityID]
	return ok
}

// HasKind reports whether an explicit kind id exists.
func (c *Context) HasKind(kindID string) bool {
	_, ok := c.Kinds[kindID]
	return ok
}

// HasBehavior reports whether a behavior id exists.
func (c *Context) HasBehavior(behaviorID string) bool {
	return c.Behaviors[behaviorID]
}

// ValidateContext adds the local scope in which a node or action is evaluated.
type ValidateContext struct {
	World      *Context
	ItemKindID string // non-empty inside core.collection
	AllowEvent bool   // behavior/action/condition may use $event.*
}
