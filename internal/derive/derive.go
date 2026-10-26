// SPDX-License-Identifier: Apache-2.0

// Package derive implements derivation rules and whitelist inheritance. It
// creates child objects only through the objects package's controlled
// interface and never writes the parent object.
package derive

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/store"
)

var (
	collaborativeFields = []string{"summary", "body", "checklist", "tags"}
	interactiveFields   = []string{"prompt", "options"}
	livingSceneFields   = []string{"premise", "elements", "tags"}
)

// AvailableFields returns the derive inheritance whitelist for an object type.
// The returned slice is a copy; callers cannot mutate the frozen lists.
func AvailableFields(objectType string) []string {
	fields := whitelistFor(objectType)
	if fields == nil {
		return nil
	}
	return append([]string(nil), fields...)
}

// Request is the strict derive body. SourceVersion is a pointer so an omitted
// value (use current) is distinguishable from an explicit invalid zero.
type Request struct {
	Title             string   `json:"title"`
	SourceVersion     *int     `json:"source_version"`
	InheritanceFields []string `json:"inheritance_fields"`
}

// Service is the derivation domain service.
type Service struct {
	db      *pgxpool.Pool
	objects *objects.Service
	idem    *store.Idempotency
}

// NewService assembles the derive service.
func NewService(db *pgxpool.Pool, objectService *objects.Service, idem *store.Idempotency) *Service {
	return &Service{db: db, objects: objectService, idem: idem}
}

// Create derives a child object from a parent snapshot. The caller supplies
// the idempotency key, route template, and request hash computed from the raw
// body. On replay, the first response is returned unchanged.
func (s *Service) Create(
	ctx context.Context,
	actorID, parentID uuid.UUID,
	request Request,
	route, idemKey string,
	requestHash []byte,
	requestID string,
	ipHash []byte,
) (*objects.ObjectView, *store.ClaimResult, error) {
	if err := validateDeriveRequest(request); err != nil {
		return nil, nil, err
	}
	parent, err := s.objects.LoadForActor(ctx, actorID, parentID)
	if err != nil {
		return nil, nil, err
	}
	if !objects.HasPermission(parent.Permissions, objects.PermRead) {
		return nil, nil, objects.ErrNotFound
	}
	if !objects.HasPermission(parent.Permissions, objects.PermDerive) {
		return nil, nil, objects.ErrForbidden
	}

	if err := validateDeriveAgainstParent(parent, request); err != nil {
		return nil, nil, err
	}

	claim, err := s.idem.Claim(ctx, actorID, route, idemKey, requestHash)
	if err != nil {
		return nil, nil, err
	}
	if claim.Replay {
		return nil, claim, nil
	}

	loadedVersion := parent.Version
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, claim, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	prepared, err := s.prepareDeriveTx(ctx, tx, actorID, parentID, request, &loadedVersion)
	if err != nil {
		return nil, claim, err
	}
	childID, err := uuid.NewV7()
	if err != nil {
		return nil, claim, err
	}
	view, err := s.objects.CreateDerived(ctx, tx, objects.DerivedInput{
		ChildID:         childID,
		ParentID:        parentID,
		ParentType:      prepared.Parent.ObjectType,
		Template:        prepared.Parent.PresentationTemplate,
		ActorID:         actorID,
		State:           prepared.ChildState,
		SourceVersion:   prepared.SourceVersion,
		InheritedFields: request.InheritanceFields,
		Lineage:         prepared.Lineage,
		Route:           route,
		IdemKey:         idemKey,
		LeaseUntil:      claim.LeaseUntil,
		RequestID:       requestID,
		IPHash:          ipHash,
	})
	if err != nil {
		return nil, claim, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, claim, err
	}
	return view, claim, nil
}

// Result is the capability-facing derivation result. It carries the verified
// snapshot versions used by the Broker's output check.
type Result struct {
	View          *objects.ObjectView
	ChildID       uuid.UUID
	SourceVersion int
	ParentVersion int
}

// CreateCapability executes a human or PAT capability invocation without the
// HTTP endpoint's idempotency record; the Broker owns idempotency for this
// path. It reuses the single prepareDeriveTx validation path.
func (s *Service) CreateCapability(ctx context.Context, actorID, parentID uuid.UUID, request Request,
	requestID string, ipHash []byte) (*Result, error) {
	if err := validateDeriveRequest(request); err != nil {
		return nil, err
	}
	parent, err := s.objects.LoadForActor(ctx, actorID, parentID)
	if err != nil {
		return nil, err
	}
	if !objects.HasPermission(parent.Permissions, objects.PermRead) {
		return nil, objects.ErrNotFound
	}
	if !objects.HasPermission(parent.Permissions, objects.PermDerive) {
		return nil, objects.ErrForbidden
	}
	if err := validateDeriveAgainstParent(parent, request); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	loadedVersion := parent.Version
	prepared, err := s.prepareDeriveTx(ctx, tx, actorID, parentID, request, &loadedVersion)
	if err != nil {
		return nil, err
	}
	childID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	view, err := s.objects.CreateDerived(ctx, tx, objects.DerivedInput{
		ChildID: childID, ParentID: parentID, ParentType: prepared.Parent.ObjectType,
		Template: prepared.Parent.PresentationTemplate, ActorID: actorID,
		State: prepared.ChildState, SourceVersion: prepared.SourceVersion,
		InheritedFields: request.InheritanceFields, Lineage: prepared.Lineage,
		RequestID: requestID, IPHash: ipHash, SkipIdempotency: true,
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &Result{
		View: view, ChildID: childID, SourceVersion: prepared.SourceVersion,
		ParentVersion: prepared.Parent.Version,
	}, nil
}

type preparedDerive struct {
	Parent        *objects.Object
	SourceVersion int
	ChildState    json.RawMessage
	Lineage       *objects.DeriveLineage
}

// prepareDeriveTx is the single derivation preparation path. The normal
// endpoint and the confirmed AI execution both validate the same request,
// snapshot, whitelist, and version rules here.
func (s *Service) prepareDeriveTx(
	ctx context.Context,
	tx pgx.Tx,
	actorID, parentID uuid.UUID,
	request Request,
	expectedVersion *int,
) (*preparedDerive, error) {
	if err := validateDeriveRequest(request); err != nil {
		return nil, err
	}
	parent, permissions, err := s.objects.LoadForDerive(ctx, tx, actorID, parentID)
	if err != nil {
		return nil, err
	}
	if parent.Status != objects.StatusActive {
		return nil, objects.ErrNotFound
	}
	if expectedVersion != nil && parent.Version != *expectedVersion {
		return nil, objects.ErrVersionConflict
	}
	if !objects.HasPermission(permissions, objects.PermRead) {
		return nil, objects.ErrNotFound
	}
	if !objects.HasPermission(permissions, objects.PermDerive) {
		return nil, objects.ErrForbidden
	}
	if err := validateDeriveAgainstParent(parent, request); err != nil {
		return nil, err
	}
	if err := validateSourceVersion(parent, request); err != nil {
		return nil, err
	}
	sourceVersion := parent.Version
	if request.SourceVersion != nil {
		sourceVersion = *request.SourceVersion
	}
	var sourceState json.RawMessage
	if sourceVersion == parent.Version {
		sourceState = parent.State
	} else {
		sourceState, err = s.objects.StateAtVersion(ctx, tx, parentID, sourceVersion)
		if err != nil {
			return nil, err
		}
	}
	childState, err := assemble(parent.ObjectType, strings.TrimSpace(request.Title), sourceState, request.InheritanceFields)
	if err != nil {
		return nil, err
	}
	return &preparedDerive{
		Parent: parent, SourceVersion: sourceVersion, ChildState: childState,
		Lineage: &objects.DeriveLineage{
			Parent:          objects.DeriveParent{ObjectID: parentID.String(), Version: sourceVersion},
			InheritedFields: append([]string(nil), request.InheritanceFields...),
		},
	}, nil
}

func validateDeriveAgainstParent(parent *objects.Object, request Request) error {
	whitelist := whitelistFor(parent.ObjectType)
	if whitelist == nil {
		return fmt.Errorf("%w: object_type", objects.ErrInvalidRequest)
	}
	for _, field := range request.InheritanceFields {
		if !contains(whitelist, field) {
			return fmt.Errorf("%w: inheritance_fields", objects.ErrInvalidRequest)
		}
	}
	if parent.ObjectType == objects.TypeInteractive && !contains(request.InheritanceFields, "options") {
		return fmt.Errorf("%w: inheritance_fields.options", objects.ErrInvalidRequest)
	}
	return nil
}

// validateSourceVersion is the phase-5 CheckVersion work for Derive. It must
// run after the idempotency reservation so deterministic 400s are recorded.
func validateSourceVersion(parent *objects.Object, request Request) error {
	if request.SourceVersion != nil {
		if *request.SourceVersion < 1 || *request.SourceVersion > parent.Version {
			return fmt.Errorf("%w: source_version", objects.ErrInvalidRequest)
		}
	}
	return nil
}

func validateDeriveRequest(request Request) error {
	title := strings.TrimSpace(request.Title)
	if runes := utf8.RuneCountInString(title); runes < 1 || runes > 200 {
		return fmt.Errorf("%w: title", objects.ErrInvalidRequest)
	}
	if len(request.InheritanceFields) == 0 {
		return fmt.Errorf("%w: inheritance_fields", objects.ErrInvalidRequest)
	}
	seen := make(map[string]bool, len(request.InheritanceFields))
	for _, field := range request.InheritanceFields {
		if seen[field] {
			return fmt.Errorf("%w: inheritance_fields", objects.ErrInvalidRequest)
		}
		seen[field] = true
	}
	return nil
}

func whitelistFor(objectType string) []string {
	switch objectType {
	case objects.TypeCollaborative:
		return collaborativeFields
	case objects.TypeInteractive:
		return interactiveFields
	case objects.TypeLivingScene:
		return livingSceneFields
	default:
		return nil
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// AssembleFromState applies the frozen inheritance whitelist to an authorized
// source snapshot. It is shared by private derive and public publication
// derive so both paths produce identical child state.
func AssembleFromState(objectType, title string, source json.RawMessage, fields []string) (json.RawMessage, error) {
	return assemble(objectType, title, source, fields)
}

func assemble(objectType, title string, source json.RawMessage, fields []string) (json.RawMessage, error) {
	selected := make(map[string]bool, len(fields))
	for _, field := range fields {
		selected[field] = true
	}
	switch objectType {
	case objects.TypeCollaborative:
		var parent objects.CollaborativeState
		if err := json.Unmarshal(source, &parent); err != nil {
			return nil, fmt.Errorf("%w: state", objects.ErrInvalidRequest)
		}
		child := objects.CollaborativeState{
			Title:     title,
			Summary:   "",
			Body:      "",
			Checklist: make([]objects.ChecklistItem, 0),
			Tags:      make([]string, 0),
		}
		if selected["summary"] {
			child.Summary = parent.Summary
		}
		if selected["body"] {
			child.Body = parent.Body
		}
		if selected["checklist"] {
			for _, item := range parent.Checklist {
				id, err := uuid.NewV7()
				if err != nil {
					return nil, err
				}
				child.Checklist = append(child.Checklist, objects.ChecklistItem{
					ID: id.String(), Text: item.Text, Done: item.Done,
				})
			}
		}
		if selected["tags"] {
			child.Tags = append(child.Tags, parent.Tags...)
		}
		if err := objects.ValidateCollaborativeState(&child); err != nil {
			return nil, err
		}
		encoded, err := store.MarshalCompactJSON(child)
		if err != nil {
			return nil, err
		}
		size, err := store.CompactJSONSize(child)
		if err != nil {
			return nil, err
		}
		if size > 64*1024 {
			return nil, fmt.Errorf("%w: state", objects.ErrPayloadTooLarge)
		}
		return encoded, nil
	case objects.TypeInteractive:
		var parent objects.InteractiveState
		if err := json.Unmarshal(source, &parent); err != nil {
			return nil, fmt.Errorf("%w: state", objects.ErrInvalidRequest)
		}
		child := objects.InteractiveState{
			Title:   title,
			Prompt:  "",
			Options: make([]objects.InteractiveOption, 0, len(parent.Options)),
			Votes:   map[string]int{},
			Status:  "open",
		}
		if selected["prompt"] {
			child.Prompt = parent.Prompt
		}
		for _, option := range parent.Options {
			id, err := uuid.NewV7()
			if err != nil {
				return nil, err
			}
			optionID := id.String()
			child.Options = append(child.Options, objects.InteractiveOption{
				ID: optionID, Label: option.Label,
			})
			child.Votes[optionID] = 0
		}
		if err := objects.ValidateInteractiveState(&child); err != nil {
			return nil, err
		}
		encoded, err := store.MarshalCompactJSON(child)
		if err != nil {
			return nil, err
		}
		size, err := store.CompactJSONSize(child)
		if err != nil {
			return nil, err
		}
		if size > 64*1024 {
			return nil, fmt.Errorf("%w: state", objects.ErrPayloadTooLarge)
		}
		return encoded, nil
	case objects.TypeLivingScene:
		var parent objects.LivingSceneState
		if err := json.Unmarshal(source, &parent); err != nil {
			return nil, fmt.Errorf("%w: state", objects.ErrInvalidRequest)
		}
		child := objects.LivingSceneState{
			Title:    title,
			Purpose:  parent.Purpose,
			Premise:  "",
			Elements: make([]objects.SceneElement, 0),
			Tags:     make([]string, 0),
		}
		if selected["premise"] {
			child.Premise = parent.Premise
		}
		if selected["elements"] {
			for _, element := range parent.Elements {
				id, err := uuid.NewV7()
				if err != nil {
					return nil, err
				}
				child.Elements = append(child.Elements, objects.SceneElement{
					ID: id.String(), Kind: element.Kind, Text: element.Text,
					Status: element.Status,
				})
			}
		}
		if selected["tags"] {
			child.Tags = append(child.Tags, parent.Tags...)
		}
		if err := objects.ValidateLivingSceneState(&child); err != nil {
			return nil, err
		}
		encoded, err := store.MarshalCompactJSON(child)
		if err != nil {
			return nil, err
		}
		size, err := store.CompactJSONSize(child)
		if err != nil {
			return nil, err
		}
		if size > 64*1024 {
			return nil, fmt.Errorf("%w: state", objects.ErrPayloadTooLarge)
		}
		return encoded, nil
	default:
		return nil, fmt.Errorf("%w: object_type", objects.ErrInvalidRequest)
	}
}
