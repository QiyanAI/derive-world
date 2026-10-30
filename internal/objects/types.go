// SPDX-License-Identifier: Apache-2.0

// Package objects owns Living Objects, memberships, events, action receipts,
// proposals, invitations, and derivation edges. It exposes the only write path
// for object state and never reads Personal AI private data.
package objects

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Domain errors mapped to the closed HTTP error set by the composition root.
var (
	ErrNotFound        = errors.New("object not found")
	ErrForbidden       = errors.New("permission denied")
	ErrConflict        = errors.New("conflict")
	ErrVersionConflict = errors.New("version conflict")
	ErrInvalidRequest  = errors.New("invalid request")
	ErrPayloadTooLarge = errors.New("payload too large")
	ErrSnapshotMissing = errors.New("state snapshot missing")
	// ErrDelegationNotActive reports that the delegation behind a pending AI
	// proposal was revoked/expired, or the actor's current AI effective
	// permission no longer covers the proposal. It maps to 403, never 404.
	ErrDelegationNotActive = errors.New("proposal delegation is not active")
)

// Permission values are frozen by G00 contract §A.4.2.
const (
	PermRead       = "read"
	PermContribute = "contribute"
	PermDerive     = "derive"
	PermInvite     = "invite"
	PermManage     = "manage"
)

var permissionOrder = []string{PermRead, PermContribute, PermDerive, PermInvite, PermManage}

// Object and template domains are frozen by G00 contract §A.4.3. living-world adds
// the third native object type without changing the first two.
const (
	TypeCollaborative = "collaborative"
	TypeInteractive   = "interactive"
	TypeLivingScene   = "living_scene"

	TemplateCollaborative = "collaborative_v1"
	TemplateInteractive   = "interactive_v1"
	TemplateLivingScene   = "living_scene_v1"

	StatusActive  = "active"
	StatusDeleted = "deleted"

	ScenePurposeProject = "project"
	ScenePurposeStory   = "story"

	SceneKindIdea    = "idea"
	SceneKindStep    = "step"
	SceneKindScene   = "scene"
	SceneKindChoice  = "choice"
	SceneKindOutcome = "outcome"

	SceneStatusOpen = "open"
	SceneStatusDone = "done"
)

// Object is the authoritative persisted object plus the caller's permissions.
type Object struct {
	ID                   uuid.UUID
	ObjectType           string
	SchemaVersion        int
	CreatorID            uuid.UUID
	OwnerID              uuid.UUID
	State                json.RawMessage
	Version              int
	SharedContext        json.RawMessage
	AccessPolicy         json.RawMessage
	PresentationTemplate string
	Status               string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	DeletedAt            *time.Time
	Permissions          []string
}

// HasPermission reports whether the permission set contains value.
func HasPermission(permissions []string, value string) bool {
	for _, permission := range permissions {
		if permission == value {
			return true
		}
	}
	return false
}

// NormalizePermissions deduplicates and orders a permission set using the
// frozen order read, contribute, derive, invite, manage.
func NormalizePermissions(input []string) []string {
	present := make(map[string]bool, len(input))
	for _, permission := range input {
		present[permission] = true
	}
	out := make([]string, 0, len(permissionOrder))
	for _, permission := range permissionOrder {
		if present[permission] {
			out = append(out, permission)
		}
	}
	return out
}

// OwnerPermissions is the only path that produces manage (A.6.2-3).
func OwnerPermissions() []string {
	return []string{PermRead, PermContribute, PermDerive, PermInvite, PermManage}
}

// RoleFromPermissions reverse-maps a permission set for display only.
func RoleFromPermissions(permissions []string) string {
	switch {
	case HasPermission(permissions, PermManage):
		return "owner"
	case HasPermission(permissions, PermContribute) && HasPermission(permissions, PermDerive):
		return "editor"
	case len(permissions) == 1 && HasPermission(permissions, PermRead):
		return "viewer"
	default:
		return "custom"
	}
}

// CreateInput is the validated create request. Content is decoded according to
// ObjectType; see ValidateCreate.
type CreateInput struct {
	ObjectType    string          `json:"object_type"`
	Title         string          `json:"title"`
	Content       json.RawMessage `json:"content"`
	SharedContext json.RawMessage `json:"shared_context"`
}

// ActionInput is the action envelope from D04 §3.7.1.
type ActionInput struct {
	Action          string          `json:"action"`
	Input           json.RawMessage `json:"input"`
	ExpectedVersion int             `json:"expected_version"`
}

// ChecklistItem is the frozen collaborative checklist item shape.
type ChecklistItem struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// CollaborativeState is the v1 collaborative template.
type CollaborativeState struct {
	Title     string          `json:"title"`
	Summary   string          `json:"summary"`
	Body      string          `json:"body"`
	Checklist []ChecklistItem `json:"checklist"`
	Tags      []string        `json:"tags"`
}

// InteractiveOption is the frozen interactive option shape.
type InteractiveOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// InteractiveState is the v1 interactive template.
type InteractiveState struct {
	Title   string              `json:"title"`
	Prompt  string              `json:"prompt"`
	Options []InteractiveOption `json:"options"`
	Votes   map[string]int      `json:"votes"`
	Status  string              `json:"status"`
}

// CollaborativeContent is the create-time content input.
type CollaborativeContent struct {
	Summary   string                   `json:"summary"`
	Body      string                   `json:"body"`
	Checklist []CollaborativeItemInput `json:"checklist"`
	Tags      []string                 `json:"tags"`
}

// CollaborativeItemInput lets the client provide item text; the server
// generates the item id.
type CollaborativeItemInput struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// InteractiveContent is the create-time content input.
type InteractiveContent struct {
	Prompt  string                   `json:"prompt"`
	Options []InteractiveOptionInput `json:"options"`
}

// InteractiveOptionInput lets the client provide option labels; the server
// generates option ids and the zeroed vote map.
type InteractiveOptionInput struct {
	Label string `json:"label"`
}

// SceneElement is one structured creation element inside living_scene_v1.
type SceneElement struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Text   string `json:"text"`
	Status string `json:"status"`
}

// LivingSceneState is the v1 scene template. Ordering is array order.
type LivingSceneState struct {
	Title    string         `json:"title"`
	Purpose  string         `json:"purpose"`
	Premise  string         `json:"premise"`
	Elements []SceneElement `json:"elements"`
	Tags     []string       `json:"tags"`
}

// SceneContent is the create-time content input. The server generates element
// ids and normalizes status to open.
type SceneContent struct {
	Purpose  string              `json:"purpose"`
	Premise  string              `json:"premise"`
	Elements []SceneElementInput `json:"elements"`
	Tags     []string            `json:"tags"`
}

// SceneElementInput lets the client provide the initial element kind and text.
type SceneElementInput struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// SceneAvailableFields is the derive inheritance whitelist for scene objects.
// Purpose is structural and copied by the assembler, not selectable.
func SceneAvailableFields() []string {
	return []string{"premise", "elements", "tags"}
}

// ObjectView is the API representation of an object.
type ObjectView struct {
	ID                   string          `json:"id"`
	ObjectType           string          `json:"object_type"`
	PresentationTemplate string          `json:"presentation_template"`
	SchemaVersion        int             `json:"schema_version"`
	CreatorID            string          `json:"creator_id"`
	OwnerID              string          `json:"owner_id"`
	Version              int             `json:"version"`
	State                json.RawMessage `json:"state"`
	SharedContext        json.RawMessage `json:"shared_context"`
	Status               string          `json:"status"`
	Permissions          []string        `json:"permissions"`
	Role                 string          `json:"role"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
	Lineage              *DeriveLineage  `json:"lineage,omitempty"`
}

// DeriveLineage is the read projection assembled from derivation_edges.
// PublicationID/PublicationTitle are only populated when the parent is hidden
// as a private object but an active public publication authorizes the source
// relationship. Public is then true.
type DeriveLineage struct {
	Parent           DeriveParent `json:"parent"`
	InheritedFields  []string     `json:"inherited_fields"`
	PublicationID    *string      `json:"publication_id,omitempty"`
	PublicationTitle *string      `json:"publication_title,omitempty"`
	Public           bool         `json:"public,omitempty"`
}

// DeriveParent identifies the parent object and the version that was copied.
type DeriveParent struct {
	ObjectID string `json:"object_id"`
	Version  int    `json:"version"`
}

// View converts a persisted object into its API representation.
func (o Object) View() ObjectView {
	view := ObjectView{
		ID:                   o.ID.String(),
		ObjectType:           o.ObjectType,
		PresentationTemplate: o.PresentationTemplate,
		SchemaVersion:        o.SchemaVersion,
		CreatorID:            o.CreatorID.String(),
		OwnerID:              o.OwnerID.String(),
		Version:              o.Version,
		State:                o.State,
		SharedContext:        o.SharedContext,
		Status:               o.Status,
		Permissions:          NormalizePermissions(o.Permissions),
		Role:                 RoleFromPermissions(o.Permissions),
		CreatedAt:            o.CreatedAt,
		UpdatedAt:            o.UpdatedAt,
	}
	if view.SharedContext == nil {
		view.SharedContext = json.RawMessage(`{}`)
	}
	return view
}

// ListItem is a card projection for GET /objects.
type ListItem struct {
	ID          string    `json:"id"`
	ObjectType  string    `json:"object_type"`
	Title       string    `json:"title"`
	Version     int       `json:"version"`
	OwnerID     string    `json:"owner_id"`
	Role        string    `json:"role"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ListCursor is the decoded keyset cursor for object lists.
type ListCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// Member is a member projection; emails are never returned.
type Member struct {
	UserID      string    `json:"user_id"`
	DisplayName string    `json:"display_name"`
	Permissions []string  `json:"permissions"`
	Role        string    `json:"role"`
	GrantedBy   *string   `json:"granted_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// Event is the event timeline read projection.
type Event struct {
	ID            string         `json:"id"`
	Seq           int64          `json:"seq"`
	ObjectVersion int            `json:"object_version"`
	EventType     string         `json:"event_type"`
	Actor         EventActor     `json:"actor"`
	Payload       map[string]any `json:"payload"`
	CreatedAt     time.Time      `json:"created_at"`
}

// EventActor is the safe actor projection (no email, no token).
type EventActor struct {
	Type        string `json:"type"`
	UserID      string `json:"user_id,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}
