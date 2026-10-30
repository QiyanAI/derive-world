// SPDX-License-Identifier: Apache-2.0

// Package handoff builds the bounded, permission-trimmed Handoff projection.
// It reads only living_objects and object_events; private AI context,
// conversations, messages, delegations, tokens, sessions, and member lists are
// structurally outside this package's data access.
package handoff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/QiyanAI/derive-world/internal/audit"
)

const (
	maxProjectionBytes        = 32 * 1024
	maxSharedBytes            = 4 * 1024
	maxStateBodyBytes         = 8 * 1024
	maxProjectedItemTextBytes = 360
	maxRecentEvents           = 20
	trimRecentEvents          = 5
	maxSummaryRunes           = 280
	projectionTTL             = 5 * time.Minute
)

// ErrPayloadTooLarge means even the maximum permitted trimming could not fit
// the frozen 32 KiB projection budget.
var ErrPayloadTooLarge = errors.New("handoff projection exceeds 32 KiB")

// Source is the already-authorized object snapshot passed by the caller.
// Permissions are the requester's current effective permissions.
type Source struct {
	ObjectID      uuid.UUID
	ObjectType    string
	Version       int
	State         json.RawMessage
	SharedContext json.RawMessage
	Permissions   []string
}

// RecentEvent is the fixed five-field event summary.
type RecentEvent struct {
	Seq       int64     `json:"seq"`
	EventType string    `json:"event_type"`
	ActorType string    `json:"actor_type"`
	CreatedAt time.Time `json:"created_at"`
	Summary   string    `json:"summary"`
}

// ObjectContext is the only permitted object context shape.
type ObjectContext struct {
	State         json.RawMessage `json:"state"`
	SharedContext json.RawMessage `json:"shared_context"`
	RecentEvents  []RecentEvent   `json:"recent_events"`
}

// Provenance records the source and version of a projected field.
type Provenance struct {
	Field         string `json:"field"`
	Source        string `json:"source"`
	ObjectVersion int    `json:"object_version"`
	Truncated     bool   `json:"truncated,omitempty"`
}

// Projection is the frozen A.7.4 response shape.
type Projection struct {
	HandoffID              uuid.UUID     `json:"handoff_id"`
	IssuedAt               time.Time     `json:"issued_at"`
	ExpiresAt              time.Time     `json:"expires_at"`
	ObjectID               uuid.UUID     `json:"object_id"`
	ObjectVersion          int           `json:"object_version"`
	ObjectType             string        `json:"object_type"`
	PermittedObjectContext ObjectContext `json:"permitted_object_context"`
	PermittedActions       []string      `json:"permitted_actions"`
	DataProvenance         []Provenance  `json:"data_provenance"`
	ContextSchemaVersion   int           `json:"context_schema_version"`
	Truncated              bool          `json:"truncated"`
	ProjectedBytes         int           `json:"-"`
}

// Service builds projections and writes the mandatory audit row.
type Service struct {
	db    *pgxpool.Pool
	audit *audit.Writer
}

// NewService assembles the read-only projection service.
func NewService(db *pgxpool.Pool, auditWriter *audit.Writer) *Service {
	return &Service{db: db, audit: auditWriter}
}

// Build generates one projection, trims it to the frozen budget, and audits
// the issue. The projection is never persisted and handoff_id is not a
// credential.
func (s *Service) Build(ctx context.Context, source Source, actor audit.Actor, requestID string, ipHash []byte) (*Projection, error) {
	if source.Version < 1 || source.ObjectID == uuid.Nil {
		return nil, errors.New("invalid handoff source")
	}
	state := source.State
	if len(state) == 0 {
		state = json.RawMessage(`{}`)
	}
	shared := source.SharedContext
	if len(shared) == 0 {
		shared = json.RawMessage(`{}`)
	}
	events, err := s.recentEvents(ctx, source.ObjectID, source.Version, maxRecentEvents)
	if err != nil {
		return nil, err
	}
	provenance := buildProvenance(state, shared, source.Version)
	now := time.Now().UTC()
	handoffID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	projection := &Projection{
		HandoffID: handoffID, IssuedAt: now, ExpiresAt: now.Add(projectionTTL),
		ObjectID: source.ObjectID, ObjectVersion: source.Version,
		ObjectType: source.ObjectType,
		PermittedObjectContext: ObjectContext{
			State: state, SharedContext: shared, RecentEvents: events,
		},
		PermittedActions:     permittedActions(source.Permissions),
		DataProvenance:       provenance,
		ContextSchemaVersion: 1,
	}

	encoded, err := marshalProjection(projection)
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxProjectionBytes {
		projection.Truncated = true
		trimmedEvents := tailEvents(events, trimRecentEvents)
		if len(trimmedEvents) < len(events) {
			projection.PermittedObjectContext.RecentEvents = trimmedEvents
			markTruncated(projection.DataProvenance, "permitted_object_context.recent_events")
		}
		encoded, err = marshalProjection(projection)
		if err != nil {
			return nil, err
		}
	}
	if len(encoded) > maxProjectionBytes && len(shared) > maxSharedBytes {
		trimmed, err := trimSharedContext(shared)
		if err != nil {
			return nil, err
		}
		projection.PermittedObjectContext.SharedContext = trimmed
		markTruncated(projection.DataProvenance, "shared_context")
		encoded, err = marshalProjection(projection)
		if err != nil {
			return nil, err
		}
	}
	if len(encoded) > maxProjectionBytes {
		trimmedState, field, changed, err := trimStateForProjection(projection.PermittedObjectContext.State)
		if err != nil {
			return nil, err
		}
		if changed {
			projection.PermittedObjectContext.State = trimmedState
			projection.Truncated = true
			markTruncated(projection.DataProvenance, field)
			encoded, err = marshalProjection(projection)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(encoded) > maxProjectionBytes {
		return nil, ErrPayloadTooLarge
	}
	projection.ProjectedBytes = len(encoded)
	if s.audit != nil {
		if err := s.audit.WriteStandalone(ctx, audit.Event{
			Actor:        actor,
			Action:       audit.ActionAIHandoffIssue,
			ResourceType: "living_object",
			ResourceID:   &source.ObjectID,
			Decision:     "allow",
			RequestID:    safeRequestID(requestID),
			IPHash:       ipHash,
			Metadata: map[string]any{
				"object_version": source.Version,
				"truncated":      projection.Truncated,
			},
		}); err != nil {
			return nil, err
		}
	}
	return projection, nil
}

func (s *Service) recentEvents(ctx context.Context, objectID uuid.UUID, version, limit int) ([]RecentEvent, error) {
	rows, err := s.db.Query(ctx, `
SELECT seq, event_type, actor_type, created_at, payload
  FROM object_events
 WHERE object_id = $1
   AND object_version <= $2
 ORDER BY seq DESC
 LIMIT $3`, objectID.String(), version, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]RecentEvent, 0, limit)
	for rows.Next() {
		var (
			item    RecentEvent
			payload []byte
		)
		if err := rows.Scan(&item.Seq, &item.EventType, &item.ActorType,
			&item.CreatedAt, &payload); err != nil {
			return nil, err
		}
		if !handoffEventAllowed(item.EventType) {
			continue
		}
		item.Summary = eventSummary(item.EventType, payload)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
	return items, nil
}

func handoffEventAllowed(eventType string) bool {
	switch eventType {
	case "object.created", "object.state_changed", "object.derived":
		return true
	default:
		return false
	}
}

func eventSummary(eventType string, payload []byte) string {
	summary := eventType
	switch eventType {
	case "object.created":
		summary = "created"
	case "object.derived":
		summary = "derived"
	case "object.state_changed":
		var value map[string]any
		if json.Unmarshal(payload, &value) == nil {
			if action, ok := value["action"].(string); ok && action != "" {
				summary = action
			}
		}
	}
	summary = strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' && r != '\t' {
			return ' '
		}
		return r
	}, summary)
	if utf8.RuneCountInString(summary) > maxSummaryRunes {
		runes := []rune(summary)
		summary = string(runes[:maxSummaryRunes])
	}
	return summary
}

func permittedActions(permissions []string) []string {
	present := make(map[string]bool, len(permissions))
	for _, permission := range permissions {
		present[permission] = true
	}
	out := make([]string, 0, 3)
	for _, permission := range []string{"read", "contribute", "derive"} {
		if present[permission] {
			out = append(out, permission)
		}
	}
	return out
}

func buildProvenance(state, shared json.RawMessage, version int) []Provenance {
	provenance := make([]Provenance, 0)
	var stateMap map[string]json.RawMessage
	if json.Unmarshal(state, &stateMap) == nil {
		keys := make([]string, 0, len(stateMap))
		for key := range stateMap {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			provenance = append(provenance, Provenance{
				Field: "state." + key, Source: "object_state", ObjectVersion: version,
			})
		}
	}
	provenance = append(provenance, Provenance{
		Field: "shared_context", Source: "shared_context", ObjectVersion: version,
	})
	provenance = append(provenance, Provenance{
		Field:  "permitted_object_context.recent_events",
		Source: "event", ObjectVersion: version,
	})
	return provenance
}

func markTruncated(provenance []Provenance, field string) {
	for index := range provenance {
		if provenance[index].Field == field {
			provenance[index].Truncated = true
			return
		}
	}
}

func trimSharedContext(raw json.RawMessage) (json.RawMessage, error) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	trimmed := make(map[string]json.RawMessage)
	for _, key := range []string{"title", "summary"} {
		if item, ok := value[key]; ok {
			trimmed[key] = item
		}
	}
	return json.Marshal(trimmed)
}

// trimStateForProjection bounds prose-heavy state without destroying the
// identity or graph of a living scene or free world. It returns the state
// path that changed so data_provenance stays honest.
func trimStateForProjection(raw json.RawMessage) (json.RawMessage, string, bool, error) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return raw, "", false, err
	}
	if bodyRaw, ok := value["body"]; ok {
		var body string
		if err := json.Unmarshal(bodyRaw, &body); err == nil && len(body) > maxStateBodyBytes {
			body = truncateUTF8(body, maxStateBodyBytes) + "\n[truncated]"
			encoded, err := json.Marshal(body)
			if err != nil {
				return raw, "", false, err
			}
			value["body"] = encoded
			trimmed, err := json.Marshal(value)
			if err != nil {
				return raw, "", false, err
			}
			return trimmed, "state.body", true, nil
		}
	}
	for _, collection := range []string{"nodes", "elements"} {
		collectionRaw, ok := value[collection]
		if !ok {
			continue
		}
		var items []map[string]any
		if err := json.Unmarshal(collectionRaw, &items); err != nil {
			continue
		}
		changed := false
		for _, item := range items {
			if text, ok := item["text"].(string); ok && len(text) > maxProjectedItemTextBytes {
				item["text"] = truncateUTF8(text, maxProjectedItemTextBytes) + "…"
				changed = true
			}
		}
		if !changed {
			continue
		}
		encoded, err := json.Marshal(items)
		if err != nil {
			return raw, "", false, err
		}
		value[collection] = encoded
		trimmed, err := json.Marshal(value)
		if err != nil {
			return raw, "", false, err
		}
		return trimmed, "state." + collection, true, nil
	}
	return raw, "", false, nil
}

func trimStateBody(raw json.RawMessage) (json.RawMessage, bool, error) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return raw, false, err
	}
	bodyRaw, ok := value["body"]
	if !ok {
		return raw, false, nil
	}
	var body string
	if err := json.Unmarshal(bodyRaw, &body); err != nil || len(body) <= maxStateBodyBytes {
		return raw, false, nil
	}
	body = truncateUTF8(body, maxStateBodyBytes) + "\n[truncated]"
	encoded, err := json.Marshal(body)
	if err != nil {
		return raw, false, err
	}
	value["body"] = encoded
	trimmed, err := json.Marshal(value)
	if err != nil {
		return raw, false, err
	}
	return trimmed, true, nil
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}

func tailEvents(events []RecentEvent, count int) []RecentEvent {
	if len(events) <= count {
		return events
	}
	out := make([]RecentEvent, count)
	copy(out, events[len(events)-count:])
	return out
}

func marshalProjection(projection *Projection) ([]byte, error) {
	encoded, err := json.Marshal(projection)
	if err != nil {
		return nil, fmt.Errorf("marshal handoff: %w", err)
	}
	return encoded, nil
}

func safeRequestID(value string) string {
	if len(value) >= 8 && len(value) <= 64 {
		return value
	}
	return "internal-ai"
}
