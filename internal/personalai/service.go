// SPDX-License-Identifier: Apache-2.0
package personalai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/handoff"
	"github.com/QiyanAI/derive-world/internal/model"
	"github.com/QiyanAI/derive-world/internal/store"
	"github.com/QiyanAI/derive-world/internal/trust"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidRequest  = errors.New("invalid personal AI request")
	ErrPayloadTooLarge = errors.New("personal AI payload too large")
	ErrForbidden       = errors.New("personal AI permission denied")
)

// ProposalWriter is implemented by objects.Service. Keeping the dependency as
// an interface prevents personalai from writing object_proposals directly.
type ProposalWriter interface {
	CreateProposalTx(
		ctx context.Context, tx pgx.Tx,
		objectID, actorUserID, aiProfileID, delegationID uuid.UUID,
		action string, input json.RawMessage, baseVersion int,
		requestID string, ipHash []byte,
	) (uuid.UUID, bool, error)
}

// CapabilityWriter routes Personal AI actions through the single Capability
// Broker. The composition root supplies the adapter; personalai never imports
// the capability package (A.12).
type CapabilityWriter interface {
	ProposeAITx(ctx context.Context, tx pgx.Tx, userID, profileID, objectID uuid.UUID,
		action string, input json.RawMessage, expectedVersion int,
		requestID string, ipHash []byte) (uuid.UUID, uuid.UUID, bool, error)
	DeriveConfirmationAITx(ctx context.Context, tx pgx.Tx, userID, profileID, objectID uuid.UUID,
		input json.RawMessage, expectedVersion int,
		requestID string, ipHash []byte) (uuid.UUID, uuid.UUID, bool, error)
	AllowAI(userID uuid.UUID) bool
}

// Options assembles the private AI runtime.
type Options struct {
	DB                    *pgxpool.Pool
	Provider              model.Provider
	ModelName             string
	Handoff               *handoff.Service
	Trust                 *trust.Service
	Proposals             ProposalWriter
	Capabilities          CapabilityWriter
	Audit                 *audit.Writer
	Idempotency           *store.Idempotency
	Recorder              *model.Recorder
	Quota                 *model.Quota
	IncludePrivateContext bool
	Logger                *slog.Logger
}

// Service owns Personal AI profiles, private contexts, private conversations,
// messages, and the model-call runtime. It never writes object state.
type Service struct {
	db                    *pgxpool.Pool
	provider              model.Provider
	modelName             string
	handoff               *handoff.Service
	trust                 *trust.Service
	proposals             ProposalWriter
	capabilities          CapabilityWriter
	audit                 *audit.Writer
	idem                  *store.Idempotency
	recorder              *model.Recorder
	quota                 *model.Quota
	includePrivateContext bool
	logger                *slog.Logger
}

// NewService assembles the runtime.
func NewService(options Options) (*Service, error) {
	if options.DB == nil || options.Provider == nil || options.Handoff == nil ||
		options.Trust == nil || options.Proposals == nil || options.Audit == nil ||
		options.Idempotency == nil || options.Recorder == nil || options.Quota == nil {
		return nil, errors.New("personalai: missing required dependency")
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		db: options.DB, provider: options.Provider, modelName: options.ModelName,
		handoff: options.Handoff, trust: options.Trust, proposals: options.Proposals,
		capabilities: options.Capabilities,
		audit:        options.Audit, idem: options.Idempotency,
		recorder: options.Recorder, quota: options.Quota,
		includePrivateContext: options.IncludePrivateContext, logger: logger,
	}, nil
}

// PrivateContext is the caller's private context. It is never returned to any
// other user and is never written to audit or Handoff records.
type PrivateContext struct {
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UpdateProfileInput is a strict PATCH body. At least one field is required.
type UpdateProfileInput struct {
	Name    *string `json:"name"`
	Persona *string `json:"persona"`
}

// GetProfile returns the caller's own profile.
func (s *Service) GetProfile(ctx context.Context, userID uuid.UUID) (*Profile, error) {
	return GetProfile(ctx, s.db, userID)
}

// UpdateProfile updates the caller's own profile and writes the audit row in
// the same transaction.
func (s *Service) UpdateProfile(ctx context.Context, userID uuid.UUID, input UpdateProfileInput, requestID string, ipHash []byte) (*Profile, error) {
	if input.Name == nil && input.Persona == nil {
		return nil, fmt.Errorf("%w: profile", ErrInvalidRequest)
	}
	var name, persona *string
	if input.Name != nil {
		value := strings.TrimSpace(*input.Name)
		if utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 80 {
			return nil, fmt.Errorf("%w: name", ErrInvalidRequest)
		}
		name = &value
	}
	if input.Persona != nil {
		if utf8.RuneCountInString(*input.Persona) > 2000 {
			return nil, fmt.Errorf("%w: persona", ErrInvalidRequest)
		}
		persona = input.Persona
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var (
		nameArg, personaArg any
	)
	if name != nil {
		nameArg = *name
	}
	if persona != nil {
		personaArg = *persona
	}
	var profile Profile
	err = tx.QueryRow(ctx, `
UPDATE personal_ai_profiles
   SET name = COALESCE($2, name),
       persona = COALESCE($3, persona),
       updated_at = GREATEST(now(), updated_at)
 WHERE user_id = $1
RETURNING id, name, persona, created_at, updated_at`,
		userID.String(), nameArg, personaArg).Scan(
		&profile.ID, &profile.Name, &profile.Persona, &profile.CreatedAt, &profile.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(userID),
		Action:       audit.ActionAIProfileUpdate,
		ResourceType: "personal_ai_profile",
		ResourceID:   &profile.ID,
		Decision:     "allow",
		RequestID:    safeRequestID(requestID),
		IPHash:       ipHash,
		Metadata:     map[string]any{},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &profile, nil
}

// GetContext returns the caller's private context.
func (s *Service) GetContext(ctx context.Context, userID uuid.UUID) (*PrivateContext, error) {
	var value PrivateContext
	err := s.db.QueryRow(ctx, `
SELECT content, updated_at FROM private_ai_contexts WHERE user_id = $1`,
		userID.String()).Scan(&value.Content, &value.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &value, nil
}

// UpdateContext replaces the private context. An empty string clears it.
func (s *Service) UpdateContext(ctx context.Context, userID uuid.UUID, content string, requestID string, ipHash []byte) (*PrivateContext, error) {
	if len([]byte(content)) > 8192 {
		return nil, ErrPayloadTooLarge
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var value PrivateContext
	err = tx.QueryRow(ctx, `
UPDATE private_ai_contexts
   SET content = $2, updated_at = GREATEST(now(), updated_at)
 WHERE user_id = $1
RETURNING content, updated_at`, userID.String(), content).Scan(&value.Content, &value.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(userID),
		Action:       audit.ActionAIContextUpdate,
		ResourceType: "private_ai_context",
		ResourceID:   &userID,
		Decision:     "allow",
		RequestID:    safeRequestID(requestID),
		IPHash:       ipHash,
		Metadata:     map[string]any{"bytes": len([]byte(content))},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &value, nil
}

func safeRequestID(value string) string {
	if len(value) >= 8 && len(value) <= 64 {
		return value
	}
	return "internal-ai"
}
