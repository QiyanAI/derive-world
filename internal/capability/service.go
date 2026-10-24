// SPDX-License-Identifier: Apache-2.0
package capability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/httpx"
	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/store"
	"github.com/QiyanAI/derive-world/internal/trust"
)

// ContextSource is the already-authorized snapshot handed to the Handoff
// projector. The projector is injected by the composition root so this
// package never imports the handoff package.
type ContextSource struct {
	ObjectID      uuid.UUID
	ObjectType    string
	Version       int
	State         json.RawMessage
	SharedContext json.RawMessage
	Permissions   []string
}

// ContextProjector builds the frozen A.7.4 projection.
type ContextProjector func(ctx context.Context, source ContextSource, actor audit.Actor, requestID string, ipHash []byte) (json.RawMessage, error)

// DeriveRequest is the capability-shaped derive input.
type DeriveRequest struct {
	Title             string
	SourceVersion     *int
	InheritanceFields []string
}

// DeriveResult is returned by the trusted derive adapter. ViewJSON is the
// human-facing object view used by confirmation approval.
type DeriveResult struct {
	ChildID       uuid.UUID
	SourceVersion int
	ParentVersion int
	ViewJSON      json.RawMessage
}

// Deriver is implemented by an adapter over derive.Service in the composition
// root. Capability never imports derive (A.12).
type Deriver interface {
	CreateCapability(ctx context.Context, actorID, parentID uuid.UUID, request DeriveRequest,
		requestID string, ipHash []byte) (*DeriveResult, error)
	CreateConfirmedTx(ctx context.Context, tx pgx.Tx, actorID, parentID uuid.UUID, expectedVersion int,
		request DeriveRequest, aiProfileID, delegationID, confirmationID uuid.UUID,
		requestID string, ipHash []byte) (*DeriveResult, error)
}

// Options assembles the capability subsystem.
type Options struct {
	DB               *pgxpool.Pool
	Audit            *audit.Writer
	Idempotency      *store.Idempotency
	Objects          *objects.Service
	Trust            *trust.Service
	Projector        ContextProjector
	Deriver          Deriver
	ConfigPath       string
	HMACSecret       string
	HMACKeyID        string
	RateLimitPerHour int
	Logger           *slog.Logger
	ExternalOptions  ExternalClientOptions
}

// Service is the single Capability Broker.
type Service struct {
	db        *pgxpool.Pool
	audit     *audit.Writer
	idem      *store.Idempotency
	objects   *objects.Service
	trust     *trust.Service
	projector ContextProjector
	deriver   Deriver
	registry  *Registry
	external  *ExternalClient
	logger    *slog.Logger

	configSHA256 string
	limiter      *httpx.Limiter
	concurrency  *keyedSemaphore
	ready        atomic.Bool
}

// New validates the configuration and builds the registry. Initialize must be
// called before serving traffic.
func New(options Options) (*Service, error) {
	if options.DB == nil || options.Audit == nil || options.Idempotency == nil ||
		options.Objects == nil || options.Trust == nil || options.Projector == nil ||
		options.Deriver == nil {
		return nil, errors.New("capability: missing required dependency")
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	fileConfig, err := LoadFileConfig(options.ConfigPath)
	if err != nil {
		return nil, err
	}
	registry, err := NewRegistry(fileConfig)
	if err != nil {
		return nil, err
	}
	var external *ExternalClient
	for index := range fileConfig.Providers {
		provider := &fileConfig.Providers[index]
		keyID := options.HMACKeyID
		if keyID == "" {
			keyID = "k1"
		}
		client, err := NewExternalClient(provider, SigningKey{
			ID: keyID, Secret: []byte(options.HMACSecret),
		}, options.ExternalOptions)
		if err != nil {
			return nil, err
		}
		external = client
	}
	if external == nil {
		return nil, fmt.Errorf("%w: the external content.structure provider is required", ErrConfigInvalid)
	}
	perHour := options.RateLimitPerHour
	if perHour < 1 {
		perHour = defaultCapabilityRateLimit
	}
	if perHour > defaultCapabilityRateLimit {
		return nil, fmt.Errorf("%w: capability rate limit cannot exceed %d/hour", ErrConfigInvalid, defaultCapabilityRateLimit)
	}
	return &Service{
		db: options.DB, audit: options.Audit, idem: options.Idempotency,
		objects: options.Objects, trust: options.Trust,
		projector: options.Projector, deriver: options.Deriver,
		registry: registry, external: external, logger: logger,
		configSHA256: fileConfig.SHA256,
		limiter:      httpx.NewLimiterWithBurst(perHour, time.Hour, minInt(perHour, 10)),
		concurrency:  newKeyedSemaphore(2),
	}, nil
}

// Initialize syncs capability_definitions and reconciles abandoned pending
// invocations. A failure is a startup failure (D06-19).
func (s *Service) Initialize(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := syncRegistry(ctx, tx, s.audit, s.registry, s.configSHA256); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if err := s.reconcile(ctx); err != nil {
		return err
	}
	s.ready.Store(true)
	return nil
}

// Ready reports whether definitions are synced and startup reconciliation is
// complete.
func (s *Service) Ready() bool {
	return s.ready.Load()
}

// Registry exposes the immutable descriptors for contract tests.
func (s *Service) Registry() *Registry {
	return s.registry
}

// AttenuatedRateLimit is the frozen per-user capability budget.
func (s *Service) AttenuatedRateLimit() int {
	return defaultCapabilityRateLimit
}

// Allow consumes one capability budget token for the user.
func (s *Service) Allow(userID uuid.UUID) (int, time.Time, bool) {
	return s.limiter.Allow(userID.String())
}

// TryAcquire reserves one of the two per-user execution slots.
func (s *Service) TryAcquire(userID uuid.UUID) bool {
	return s.concurrency.TryAcquire(userID.String())
}

// Release returns a per-user execution slot.
func (s *Service) Release(userID uuid.UUID) {
	s.concurrency.Release(userID.String())
}

// ListDefinitions returns the synced descriptors. Only enabled capabilities
// are returned when enabledOnly is true.
func (s *Service) ListDefinitions(ctx context.Context, enabledOnly bool) ([]Definition, error) {
	items, err := definitionsFromDB(ctx, s.db, enabledOnly)
	if err != nil {
		return nil, err
	}
	return items, nil
}

// Definition returns one synced descriptor.
func (s *Service) Definition(ctx context.Context, capabilityID string) (*Definition, error) {
	var (
		item        Definition
		input       []byte
		output      []byte
		constraints []byte
	)
	err := s.db.QueryRow(ctx, `
SELECT id, contract_version, provider_id, description, input_schema, output_schema,
       required_scopes, supported_operations, execution_constraints, enabled
  FROM capability_definitions WHERE id = $1`, capabilityID).Scan(
		&item.CapabilityID, &item.ContractVersion, &item.ProviderID, &item.Description,
		&input, &output, &item.RequiredScopes, &item.SupportedOperations,
		&constraints, &item.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	item.InputSchema = append(json.RawMessage(nil), input...)
	item.OutputSchema = append(json.RawMessage(nil), output...)
	if err := json.Unmarshal(constraints, &item.ExecutionConstraints); err != nil {
		return nil, err
	}
	return &item, nil
}

// InvocationView is the public D06 §3.10.5 projection. It never contains the
// input hash or redacted input.
type InvocationView struct {
	ID              uuid.UUID       `json:"id"`
	CapabilityID    string          `json:"capability_id"`
	ContractVersion string          `json:"contract_version"`
	ProviderID      string          `json:"provider_id"`
	Status          string          `json:"status"`
	Output          json.RawMessage `json:"output"`
	ErrorCode       *string         `json:"error_code"`
	DurationMS      *int            `json:"duration_ms"`
	CreatedAt       time.Time       `json:"created_at"`
	CompletedAt     *time.Time      `json:"completed_at"`
}

// GetInvocation returns an invocation to its actor. Membership is rechecked
// against the current object state; a revoked caller receives 404.
func (s *Service) GetInvocation(ctx context.Context, userID, invocationID uuid.UUID,
	requestID string, ipHash []byte) (*InvocationView, error) {
	var (
		view        InvocationView
		output      []byte
		objectID    *string
		errorCode   *string
		duration    *int
		completedAt *time.Time
	)
	err := s.db.QueryRow(ctx, `
SELECT id, capability_id, contract_version, provider_id, status, output,
       error_code, duration_ms, created_at, completed_at, object_id
  FROM capability_invocations
 WHERE id = $1 AND actor_user_id = $2`, invocationID.String(), userID.String()).Scan(
		&view.ID, &view.CapabilityID, &view.ContractVersion, &view.ProviderID,
		&view.Status, &output, &errorCode, &duration, &view.CreatedAt,
		&completedAt, &objectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if view.Status == "pending" {
		expired, err := s.trust.ExpireCapabilityConfirmation(ctx, invocationID)
		if err != nil {
			return nil, err
		}
		if expired {
			if err := s.DenyAIInvocation(ctx, invocationID, userID,
				"confirmation expired", "confirmation_expired"); err != nil {
				return nil, err
			}
			view.Status = "denied"
			code := "confirmation_expired"
			errorCode = &code
			zero := 0
			duration = &zero
			now := time.Now().UTC()
			completedAt = &now
			output = nil
		}
	}
	if objectID != nil {
		parsed, err := uuid.Parse(*objectID)
		if err != nil {
			return nil, ErrNotFound
		}
		if _, err := s.objects.LoadForActor(ctx, userID, parsed); err != nil {
			if errors.Is(err, objects.ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
	}
	if len(output) > 0 {
		view.Output = append(json.RawMessage(nil), output...)
	} else {
		view.Output = json.RawMessage("null")
	}
	view.ErrorCode = errorCode
	view.DurationMS = duration
	view.CompletedAt = completedAt
	// A.4.6 / D05-17 require reads of stored capability invocations to be
	// audited. The invocation record is not modified by this read.
	if err := s.audit.WriteStandalone(ctx, audit.Event{
		Actor:        audit.UserActor(userID),
		Action:       audit.ActionCapabilityInvoke,
		ResourceType: "capability_invocation",
		ResourceID:   &invocationID,
		Decision:     "allow",
		RequestID:    safeRequestID(requestID),
		IPHash:       ipHash,
		Metadata: map[string]any{
			"capability_id": view.CapabilityID,
			"operation":     "invocation_read",
		},
	}); err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *Service) writeDenied(ctx context.Context, request InvokeRequest, capabilityID string,
	objectID *uuid.UUID, reason string, errorCode string, requestID string, ipHash []byte) uuid.UUID {
	inputHash, err := canonicalInputHash(capabilityID, request.Input)
	if err != nil {
		inputHash = make([]byte, 32)
	}
	redacted, err := redactedInput(capabilityID, request.Input)
	if err != nil {
		redacted = []byte(`{"capability_id":"` + capabilityID + `","fields":{}}`)
	}
	requestedObjectHash := ""
	if objectID == nil {
		if requested, ok := inputObjectID(request.Input); ok {
			digest := sha256.Sum256([]byte(requested))
			requestedObjectHash = hex.EncodeToString(digest[:])
			redacted = redactObjectIDHash(redacted, requestedObjectHash)
		}
	}
	invocationID, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		s.logger.Error("capability_denied_insert_failed", "error_type", fmt.Sprintf("%T", err), "error", err.Error())
		return uuid.Nil
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
INSERT INTO capability_invocations (
    id, capability_id, contract_version, provider_id, actor_user_id,
    actor_ai_profile_id, delegation_id, object_id, input_hash, input_redacted,
    status, output, error_code, error_message, duration_ms, completed_at
) VALUES ($1,$2,$3,$4,$5,NULL,NULL,$6,$7,$8::jsonb,'denied',NULL,$9,$10,0,now())`,
		invocationID.String(), capabilityID, ContractVersion,
		providerIDFor(capabilityID), request.Actor.UserID.String(),
		nullableUUID(objectID), inputHash, string(redacted), errorCode,
		truncateMessage(reason, 500))
	if err != nil {
		s.logger.Error("capability_denied_insert_failed", "error_type", fmt.Sprintf("%T", err), "error", err.Error())
		return uuid.Nil
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        request.Actor.auditActor(),
		Action:       audit.ActionCapabilityDenied,
		ResourceType: "capability_invocation",
		ResourceID:   &invocationID,
		Decision:     "deny",
		ReasonCode:   errorCode,
		RequestID:    safeRequestID(requestID),
		IPHash:       ipHash,
		Metadata:     deniedMetadata(capabilityID, errorCode, requestedObjectHash),
	}); err != nil {
		s.logger.Error("capability_denied_audit_failed", "error_type", fmt.Sprintf("%T", err), "error", err.Error())
		return uuid.Nil
	}
	if err := tx.Commit(ctx); err != nil {
		s.logger.Error("capability_denied_commit_failed", "error_type", fmt.Sprintf("%T", err), "error", err.Error())
		return uuid.Nil
	}
	return invocationID
}

func deniedMetadata(capabilityID, errorCode, requestedObjectHash string) map[string]any {
	metadata := map[string]any{
		"capability_id": capabilityID,
		"reason_code":   errorCode,
	}
	if requestedObjectHash != "" {
		metadata["requested_object_id_sha256"] = requestedObjectHash
	}
	return metadata
}

func inputObjectID(input json.RawMessage) (string, bool) {
	var value map[string]any
	if err := json.Unmarshal(input, &value); err != nil {
		return "", false
	}
	text, ok := value["object_id"].(string)
	return text, ok && text != ""
}

func redactObjectIDHash(redacted []byte, hash string) []byte {
	var value map[string]any
	if err := json.Unmarshal(redacted, &value); err != nil {
		return redacted
	}
	summary := map[string]any{"sha256": hash}
	value["object_id"] = summary
	if fields, ok := value["fields"].(map[string]any); ok {
		fields["object_id"] = summary
	}
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > maxRedactedBytes {
		return redacted
	}
	return encoded
}

func providerIDFor(capabilityID string) string {
	if provider, ok := ProviderFor(capabilityID); ok {
		return provider
	}
	return ProviderTrustedCore
}

func truncateMessage(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func safeRequestID(value string) string {
	if len(value) >= 8 && len(value) <= 64 {
		return value
	}
	return "capability-internal"
}

func nullableUUID(value *uuid.UUID) any {
	if value == nil {
		return nil
	}
	return value.String()
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

// keyedSemaphore bounds concurrent executions per user (frozen at 2).
type keyedSemaphore struct {
	mu      sync.Mutex
	limit   int
	entries map[string]*semaphoreEntry
}

type semaphoreEntry struct {
	inUse int
	seen  time.Time
}

func newKeyedSemaphore(limit int) *keyedSemaphore {
	return &keyedSemaphore{limit: limit, entries: map[string]*semaphoreEntry{}}
}

func (s *keyedSemaphore) TryAcquire(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if len(s.entries) > 1024 {
		for candidate, entry := range s.entries {
			if entry.inUse == 0 && now.Sub(entry.seen) > 10*time.Minute {
				delete(s.entries, candidate)
			}
		}
	}
	entry, ok := s.entries[key]
	if !ok {
		entry = &semaphoreEntry{}
		s.entries[key] = entry
	}
	entry.seen = now
	if entry.inUse >= s.limit {
		return false
	}
	entry.inUse++
	return true
}

func (s *keyedSemaphore) Release(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	if !ok {
		return
	}
	if entry.inUse > 0 {
		entry.inUse--
	}
	entry.seen = time.Now()
}
