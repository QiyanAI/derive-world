// SPDX-License-Identifier: Apache-2.0

// Package audit owns append-only audit events and the frozen audit action
// registry. It never updates or deletes rows and never stores secrets, private
// context, object content, or raw IP addresses.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Execer is satisfied by pgx.Tx and pgxpool.Pool.
type Execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// DB is satisfied by pgxpool.Pool.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Actor is the server-derived actor quadruple. Identity never comes from the
// request body or query string.
type Actor struct {
	Type         string
	UserID       *uuid.UUID
	AIProfileID  *uuid.UUID
	DelegationID *uuid.UUID
}

// UserActor builds a human actor.
func UserActor(userID uuid.UUID) Actor {
	return Actor{Type: "user", UserID: &userID}
}

// SystemActor builds a system actor for migrations and lifecycle events.
func SystemActor() Actor {
	return Actor{Type: "system"}
}

// Event is one append-only audit record.
type Event struct {
	Actor        Actor
	Action       Action
	ResourceType string
	ResourceID   *uuid.UUID
	Decision     string
	ReasonCode   string
	RequestID    string
	IPHash       []byte
	Metadata     map[string]any
}

// Writer writes audit rows inside a caller-owned transaction.
type Writer struct {
	db DB
}

// NewWriter returns a writer backed by the pool.
func NewWriter(db DB) *Writer {
	return &Writer{db: db}
}

// Write appends an audit row using the provided transaction. Audit failure
// must fail the enclosing business transaction.
func (w *Writer) Write(ctx context.Context, tx Execer, event Event) error {
	if err := validate(event); err != nil {
		return err
	}
	metadata, err := normalizeMetadata(event.Metadata)
	if err != nil {
		return err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO audit_events (
    id, actor_type, actor_user_id, actor_ai_profile_id, delegation_id,
    action, resource_type, resource_id, decision, reason_code, request_id,
    ip_hash, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb)`,
		id.String(), event.Actor.Type, nullableUUID(event.Actor.UserID),
		nullableUUID(event.Actor.AIProfileID), nullableUUID(event.Actor.DelegationID),
		string(event.Action), event.ResourceType, nullableUUID(event.ResourceID),
		event.Decision, nullString(event.ReasonCode), event.RequestID,
		event.IPHash, string(metadata),
	)
	return err
}

// WriteStandalone writes a denial audit in its own short transaction. It is
// used only for rejected requests whose business transaction has rolled back
// (D04 §3.10.5). Success-path audit must use Write inside the business tx.
func (w *Writer) WriteStandalone(ctx context.Context, event Event) error {
	if w.db == nil {
		return errors.New("audit writer has no database")
	}
	// Denial audits must survive a client disconnect. The business transaction
	// is already gone, so a short detached context is the only way to keep the
	// mandatory audit row (D04 §3.16.2).
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	tx, err := w.db.Begin(auditCtx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(auditCtx) }()
	if err := w.Write(auditCtx, tx, event); err != nil {
		return err
	}
	return tx.Commit(auditCtx)
}

func validate(event Event) error {
	if !IsRegistered(event.Action) {
		return fmt.Errorf("audit action %q is not registered", event.Action)
	}
	switch event.Actor.Type {
	case "system":
		if event.Actor.UserID != nil || event.Actor.AIProfileID != nil || event.Actor.DelegationID != nil {
			return errors.New("system actor must not carry a user, AI profile, or delegation")
		}
	case "user":
		if event.Actor.UserID == nil || event.Actor.AIProfileID != nil {
			return errors.New("user actor must carry exactly a user id")
		}
	case "personal_ai":
		if event.Actor.UserID == nil || event.Actor.AIProfileID == nil || event.Actor.DelegationID == nil {
			return errors.New("personal_ai actor must carry user, AI profile, and delegation ids")
		}
	default:
		return fmt.Errorf("unknown actor type %q", event.Actor.Type)
	}
	if event.Decision != "allow" && event.Decision != "deny" {
		return fmt.Errorf("unknown audit decision %q", event.Decision)
	}
	if event.ResourceType == "" {
		return errors.New("audit resource type is required")
	}
	if length := len(event.RequestID); length < 8 || length > 64 {
		return fmt.Errorf("audit request id length %d is out of range", length)
	}
	if event.IPHash != nil && len(event.IPHash) != 32 {
		return fmt.Errorf("audit ip hash must be 32 bytes, got %d", len(event.IPHash))
	}
	return nil
}

var sensitiveKeyFragments = []string{
	"password", "token", "secret", "authorization", "cookie", "api_key",
	"private_context", "context", "message", "content", "state", "prompt",
}

func normalizeMetadata(input map[string]any) ([]byte, error) {
	if input == nil {
		return []byte("{}"), nil
	}
	clean := make(map[string]any, len(input))
	for key, value := range input {
		if isSensitiveKey(key) {
			clean[key] = "[REDACTED]"
			continue
		}
		switch value.(type) {
		case nil, string, bool, int, int64, float64:
			clean[key] = value
		default:
			return nil, fmt.Errorf("audit metadata key %q must be a scalar value", key)
		}
	}
	encoded, err := json.Marshal(clean)
	if err != nil {
		return nil, err
	}
	if len(encoded) > 4096 {
		return nil, errors.New("audit metadata exceeds 4 KiB")
	}
	return encoded, nil
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, fragment := range sensitiveKeyFragments {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

func nullableUUID(value *uuid.UUID) any {
	if value == nil {
		return nil
	}
	return value.String()
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
