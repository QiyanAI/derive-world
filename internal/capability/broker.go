// SPDX-License-Identifier: Apache-2.0
package capability

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/httpx"
	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/store"
	"github.com/QiyanAI/derive-world/internal/trust"
)

// Caller is the server-derived HTTP actor. It is never populated from a
// request body.
type Caller struct {
	UserID     uuid.UUID
	AuthMethod string
	Scopes     []string
}

func (c Caller) auditActor() audit.Actor {
	return audit.UserActor(c.UserID)
}

func (c Caller) hasScope(scope string) bool {
	if c.AuthMethod != "pat" {
		return true
	}
	for _, value := range c.Scopes {
		if value == scope {
			return true
		}
	}
	return false
}

// InvokeRequest is one HTTP capability invocation. The request hash is
// computed inside the Broker only after authorization and schema validation.
type InvokeRequest struct {
	Actor          Caller
	CapabilityID   string
	Input          json.RawMessage
	ContentType    string
	Route          string
	ActualPath     string
	IdempotencyKey string
	RequestID      string
	IPHash         []byte
}

// InvokeResult is a completed or confirmation-pending response.
type executionPlan struct {
	proposalProfileID    uuid.UUID
	proposalDelegationID uuid.UUID
}

type InvokeResult struct {
	StatusCode     int
	Body           []byte
	InvocationID   uuid.UUID
	ConfirmationID *uuid.UUID
	Replayed       bool
}

type executionStatus int

const (
	executionSucceeded executionStatus = iota
	executionFailed
	executionTimedOut
	executionUnknown
)

type executionResult struct {
	status                   executionStatus
	output                   json.RawMessage
	errorCode                string
	message                  string
	providerAuthFailure      bool
	providerIntegrityFailure bool
}

// Invoke runs the frozen 16-step broker flow. Authorization, schema, limits,
// idempotency, execution, output validation, and audit all happen here; the
// HTTP layer only authenticates and translates errors.
func (s *Service) Invoke(ctx context.Context, request InvokeRequest) (*InvokeResult, error) {
	definition, ok := s.registry.Definition(request.CapabilityID)
	if !ok {
		return nil, newFailure(httpx.CodeNotFound, "not found", ErrNotFound)
	}
	var enabled bool
	err := s.db.QueryRow(ctx, `SELECT enabled FROM capability_definitions WHERE id = $1`,
		request.CapabilityID).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, newFailure(httpx.CodeNotFound, "not found", ErrNotFound)
	}
	if err != nil {
		return nil, newFailure(httpx.CodeInternal, "internal server error", err)
	}
	if !enabled {
		deniedID := s.writeDenied(ctx, request, request.CapabilityID, nil,
			"capability_disabled", string(httpx.CodeCapabilityDenied), request.RequestID, request.IPHash)
		return nil, deniedFailure(httpx.CodeCapabilityDenied, "capability is not available to this actor", deniedID, ErrDenied)
	}

	if request.Actor.AuthMethod == "pat" {
		if !request.Actor.hasScope("capabilities:invoke") {
			deniedID := s.writeDenied(ctx, request, request.CapabilityID, nil,
				"missing_capabilities_scope", string(httpx.CodeForbidden), request.RequestID, request.IPHash)
			return nil, deniedFailure(httpx.CodeForbidden, "forbidden", deniedID, ErrForbidden)
		}
		required := operationScope(request.CapabilityID)
		if required != "" && !request.Actor.hasScope(required) {
			deniedID := s.writeDenied(ctx, request, request.CapabilityID, nil,
				"missing_operation_scope", string(httpx.CodeForbidden), request.RequestID, request.IPHash)
			return nil, deniedFailure(httpx.CodeForbidden, "forbidden", deniedID, ErrForbidden)
		}
	}

	decoded, decodeErr := decodeJSON(request.Input)
	if decodeErr != nil {
		return nil, newFailure(httpx.CodeInvalidRequest, "invalid request", ErrInvalidRequest)
	}
	inputObject, ok := decoded.(map[string]any)
	if !ok {
		return nil, newFailure(httpx.CodeInvalidRequest, "invalid request", ErrInvalidRequest)
	}

	rawObjectID, ok := inputObject["object_id"].(string)
	if !ok {
		return nil, newFailure(httpx.CodeInvalidRequest, "invalid request",
			fmt.Errorf("%w: object_id", ErrInvalidRequest))
	}
	objectID, err := uuid.Parse(rawObjectID)
	if err != nil {
		return nil, newFailure(httpx.CodeInvalidRequest, "invalid request",
			fmt.Errorf("%w: object_id", ErrInvalidRequest))
	}

	object, err := s.objects.LoadForActor(ctx, request.Actor.UserID, objectID)
	if err != nil {
		if errors.Is(err, objects.ErrNotFound) {
			deniedID := s.writeDenied(ctx, request, request.CapabilityID, nil,
				"object_not_readable", string(httpx.CodeNotFound), request.RequestID, request.IPHash)
			return nil, deniedFailure(httpx.CodeNotFound, "not found", deniedID, ErrNotFound)
		}
		return nil, newFailure(httpx.CodeInternal, "internal server error", err)
	}
	if !objects.HasPermission(object.Permissions, objects.PermRead) {
		deniedID := s.writeDenied(ctx, request, request.CapabilityID, nil,
			"object_not_readable", string(httpx.CodeNotFound), request.RequestID, request.IPHash)
		return nil, deniedFailure(httpx.CodeNotFound, "not found", deniedID, ErrNotFound)
	}
	if permission := requiredObjectPermission(request.CapabilityID); permission != objects.PermRead &&
		!objects.HasPermission(object.Permissions, permission) {
		deniedID := s.writeDenied(ctx, request, request.CapabilityID, &objectID,
			"object_permission_insufficient", string(httpx.CodeForbidden), request.RequestID, request.IPHash)
		return nil, deniedFailure(httpx.CodeForbidden, "forbidden", deniedID, ErrForbidden)
	}

	plan := executionPlan{}
	if request.CapabilityID == IDObjectProposeChange {
		profileID, delegationID, err := s.trust.DelegationForUser(ctx, request.Actor.UserID,
			object.ID, objects.PermContribute)
		if err != nil {
			if errors.Is(err, trust.ErrNoActiveDelegation) || errors.Is(err, trust.ErrForbidden) {
				deniedID := s.writeDenied(ctx, request, request.CapabilityID, &objectID,
					"no_active_contribute_delegation", string(httpx.CodeCapabilityDenied),
					request.RequestID, request.IPHash)
				return nil, deniedFailure(httpx.CodeCapabilityDenied,
					"capability is not available to this actor", deniedID, ErrDenied)
			}
			return nil, newFailure(httpx.CodeInternal, "internal server error", err)
		}
		plan.proposalProfileID = profileID
		plan.proposalDelegationID = delegationID
	}
	if mediaType, _, mediaErr := mime.ParseMediaType(request.ContentType); mediaErr != nil ||
		!strings.EqualFold(mediaType, "application/json") {
		return nil, newFailure(httpx.CodeUnsupportedMediaType, "Content-Type must be application/json", ErrInvalidRequest)
	}
	if len(request.Input) > definition.ExecutionConstraints.MaxInputBytes {
		return nil, newFailure(httpx.CodePayloadTooLarge, "payload too large", ErrPayloadTooLarge)
	}
	if err := definition.Input.ValidateJSON(request.Input); err != nil {
		return nil, invalidInputFailure(err)
	}

	requestHash, err := store.RequestHash(http.MethodPost, request.ActualPath, request.Input)
	if err != nil {
		return nil, newFailure(httpx.CodeInvalidRequest, "invalid request", err)
	}
	claim, err := s.idem.Claim(ctx, request.Actor.UserID, request.Route,
		request.IdempotencyKey, requestHash)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrIdempotencyConflict):
			return nil, newFailure(httpx.CodeIdempotencyConflict,
				"idempotency key was used for a different request", err)
		case errors.Is(err, store.ErrRequestInProgress):
			return nil, newFailure(httpx.CodeRequestInProgress,
				"an identical request is already in progress", err)
		default:
			return nil, newFailure(httpx.CodeInvalidRequest, "invalid request", err)
		}
	}
	if claim.Replay {
		return &InvokeResult{
			StatusCode: claim.StatusCode, Body: claim.Body,
			InvocationID: uuid.Nil, Replayed: true,
		}, nil
	}
	cancelClaim := func() {
		_ = s.idem.Cancel(ctx, request.Actor.UserID, request.Route,
			request.IdempotencyKey, claim.LeaseUntil)
	}

	if failure := semaValidate(definition, object, inputObject); failure != nil {
		cancelClaim()
		return nil, failure
	}

	inputHash, err := canonicalInputHash(request.CapabilityID, request.Input)
	if err != nil {
		cancelClaim()
		return nil, newFailure(httpx.CodeInvalidRequest, "invalid request", err)
	}
	redacted, err := redactedInput(request.CapabilityID, request.Input)
	if err != nil {
		cancelClaim()
		return nil, newFailure(httpx.CodeInvalidRequest, "invalid request", err)
	}
	invocationID, err := uuid.NewV7()
	if err != nil {
		cancelClaim()
		return nil, newFailure(httpx.CodeInternal, "internal server error", err)
	}
	createdAt, failure := s.beginInvocation(ctx, invocationID, request, definition,
		&objectID, inputHash, redacted)
	if failure != nil {
		cancelClaim()
		return nil, failure
	}

	started := time.Now()
	executionCtx, cancelExecution := context.WithTimeout(ctx,
		time.Duration(definition.ExecutionConstraints.TimeoutMS)*time.Millisecond)
	execution := s.execute(executionCtx, invocationID, definition, object, request, inputObject, plan)
	deadlineExpired := errors.Is(executionCtx.Err(), context.DeadlineExceeded)
	cancelExecution()
	if deadlineExpired && execution.status != executionTimedOut {
		execution = executionResult{
			status: executionTimedOut, errorCode: string(httpx.CodeCapabilityTimeout),
			message: "capability timed out",
		}
	}
	duration := int(time.Since(started).Milliseconds())
	if duration < 0 {
		duration = 0
	}

	if execution.status == executionSucceeded {
		if err := validateOutput(ctx, definition, request.CapabilityID, object, request, inputObject,
			execution.output, s); err != nil {
			execution = executionResult{
				status: executionFailed, errorCode: string(httpx.CodeCapabilityFailed),
				message: "provider output failed validation: " + err.Error(),
			}
		}
	}

	finishCtx, cancelFinish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelFinish()
	return s.finishInvocation(finishCtx, invocationID, createdAt, request, definition,
		execution, duration, claim.LeaseUntil)
}

func (s *Service) beginInvocation(ctx context.Context, invocationID uuid.UUID, request InvokeRequest,
	definition *CompiledDefinition, objectID *uuid.UUID, inputHash, redacted []byte) (time.Time, *Failure) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return time.Time{}, newFailure(httpx.CodeInternal, "internal server error", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var createdAt time.Time
	err = tx.QueryRow(ctx, `
INSERT INTO capability_invocations (
    id, capability_id, contract_version, provider_id, actor_user_id,
    actor_ai_profile_id, delegation_id, object_id, input_hash, input_redacted,
    status, output, error_code, error_message, duration_ms, completed_at
) VALUES ($1,$2,$3,$4,$5,NULL,NULL,$6,$7,$8::jsonb,'pending',NULL,NULL,NULL,NULL,NULL)
RETURNING created_at`,
		invocationID.String(), definition.CapabilityID, ContractVersion,
		definition.ProviderID, request.Actor.UserID.String(), nullableUUID(objectID),
		inputHash, string(redacted)).Scan(&createdAt)
	if err != nil {
		return time.Time{}, newFailure(httpx.CodeInternal, "internal server error", err)
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        request.Actor.auditActor(),
		Action:       audit.ActionCapabilityInvoke,
		ResourceType: "capability_invocation",
		ResourceID:   &invocationID,
		Decision:     "allow",
		RequestID:    safeRequestID(request.RequestID),
		IPHash:       request.IPHash,
		Metadata: map[string]any{
			"capability_id":    definition.CapabilityID,
			"provider_id":      definition.ProviderID,
			"contract_version": ContractVersion,
			"input_hash":       hex.EncodeToString(inputHash),
		},
	}); err != nil {
		return time.Time{}, newFailure(httpx.CodeInternal, "internal server error", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, newFailure(httpx.CodeInternal, "internal server error", err)
	}
	return createdAt, nil
}

func (s *Service) finishInvocation(ctx context.Context, invocationID uuid.UUID, createdAt time.Time,
	request InvokeRequest, definition *CompiledDefinition, execution executionResult,
	durationMS int, leaseUntil time.Time) (*InvokeResult, error) {
	status := "failed"
	switch execution.status {
	case executionSucceeded:
		status = "succeeded"
	case executionTimedOut:
		status = "timeout"
	case executionUnknown:
		status = "unknown"
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, newFailure(httpx.CodeInternal, "internal server error", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		output       []byte
		errorCode    any
		errorMessage any
	)
	if status == "succeeded" {
		output = execution.output
	} else {
		errorCode = execution.errorCode
		errorMessage = truncateMessage(execution.message, 500)
	}
	var completedAt time.Time
	err = tx.QueryRow(ctx, `
UPDATE capability_invocations
   SET status = $2,
       output = $3::jsonb,
       error_code = $4,
       error_message = $5,
       duration_ms = $6,
       completed_at = now()
 WHERE id = $1 AND status = 'pending'
RETURNING completed_at`,
		invocationID.String(), status, nullableBytes(output), errorCode, errorMessage, durationMS,
	).Scan(&completedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, failureWithInvocation(httpx.CodeRequestInProgress,
			"invocation is already terminal", invocationID, ErrConflict)
	}
	if err != nil {
		return nil, failureWithInvocation(httpx.CodeInternal, "internal server error", invocationID, err)
	}

	var body []byte
	var statusCode int
	if status == "succeeded" {
		body, err = json.Marshal(struct {
			ID           uuid.UUID       `json:"id"`
			CapabilityID string          `json:"capability_id"`
			Status       string          `json:"status"`
			Output       json.RawMessage `json:"output"`
			DurationMS   int             `json:"duration_ms"`
			CreatedAt    time.Time       `json:"created_at"`
			CompletedAt  time.Time       `json:"completed_at"`
		}{
			ID: invocationID, CapabilityID: definition.CapabilityID,
			Status: "succeeded", Output: execution.output, DurationMS: durationMS,
			CreatedAt: createdAt, CompletedAt: completedAt,
		})
		if err != nil {
			return nil, failureWithInvocation(httpx.CodeInternal, "internal server error", invocationID, err)
		}
		statusCode = 200
	} else {
		code := httpx.ErrorCode(execution.errorCode)
		if code == "" {
			code = httpx.CodeCapabilityFailed
		}
		statusCode = httpx.StatusFor(code)
		body = errorBody(code, execution.message, request.RequestID, invocationID)
	}

	if err := s.writeTerminalAudit(ctx, tx, invocationID, request, definition, status, execution, durationMS, len(execution.output)); err != nil {
		return nil, failureWithInvocation(httpx.CodeInternal, "internal server error", invocationID, err)
	}
	if err := s.idem.Complete(ctx, tx, request.Actor.UserID, request.Route,
		request.IdempotencyKey, leaseUntil, statusCode, body); err != nil {
		return nil, failureWithInvocation(httpx.CodeInternal, "internal server error", invocationID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, failureWithInvocation(httpx.CodeInternal, "internal server error", invocationID, err)
	}
	return &InvokeResult{StatusCode: statusCode, Body: body, InvocationID: invocationID}, nil
}

func (s *Service) writeTerminalAudit(ctx context.Context, tx pgx.Tx, invocationID uuid.UUID,
	request InvokeRequest, definition *CompiledDefinition, status string, execution executionResult, durationMS, outputBytes int) error {
	actor := request.Actor.auditActor()
	if execution.providerAuthFailure {
		if err := s.audit.Write(ctx, tx, audit.Event{
			Actor: actor, Action: audit.ActionCapabilityFailed,
			ResourceType: "capability_invocation", ResourceID: &invocationID,
			Decision: "deny", ReasonCode: "provider_auth_failed",
			RequestID: safeRequestID(request.RequestID), IPHash: request.IPHash,
			Metadata: map[string]any{
				"invocation_id": invocationID.String(),
				"error_code":    execution.errorCode,
				"duration_ms":   durationMS,
			},
		}); err != nil {
			return err
		}
	}
	if execution.providerIntegrityFailure {
		if err := s.audit.Write(ctx, tx, audit.Event{
			Actor: actor, Action: audit.ActionCapabilityUnknown,
			ResourceType: "capability_invocation", ResourceID: &invocationID,
			Decision: "deny", ReasonCode: "provider_integrity_failed",
			RequestID: safeRequestID(request.RequestID), IPHash: request.IPHash,
			Metadata: map[string]any{
				"invocation_id": invocationID.String(),
				"error_code":    execution.errorCode,
				"duration_ms":   durationMS,
			},
		}); err != nil {
			return err
		}
	}
	switch status {
	case "succeeded":
		return s.audit.Write(ctx, tx, audit.Event{
			Actor: actor, Action: audit.ActionCapabilityCompleted,
			ResourceType: "capability_invocation", ResourceID: &invocationID,
			Decision: "allow", RequestID: safeRequestID(request.RequestID), IPHash: request.IPHash,
			Metadata: map[string]any{
				"invocation_id": invocationID.String(), "duration_ms": durationMS,
				"output_bytes": outputBytes,
			},
		})
	case "unknown":
		if err := s.audit.Write(ctx, tx, audit.Event{
			Actor: actor, Action: audit.ActionCapabilityFailed,
			ResourceType: "capability_invocation", ResourceID: &invocationID,
			Decision: "deny", ReasonCode: execution.errorCode,
			RequestID: safeRequestID(request.RequestID), IPHash: request.IPHash,
			Metadata: map[string]any{
				"invocation_id": invocationID.String(),
				"error_code":    execution.errorCode,
				"duration_ms":   durationMS,
			},
		}); err != nil {
			return err
		}
		return s.audit.Write(ctx, tx, audit.Event{
			Actor: actor, Action: audit.ActionCapabilityUnknown,
			ResourceType: "capability_invocation", ResourceID: &invocationID,
			Decision: "deny", ReasonCode: execution.errorCode,
			RequestID: safeRequestID(request.RequestID), IPHash: request.IPHash,
			Metadata: map[string]any{
				"invocation_id":   invocationID.String(),
				"transport_phase": "response",
			},
		})
	default:
		return s.audit.Write(ctx, tx, audit.Event{
			Actor: actor, Action: audit.ActionCapabilityFailed,
			ResourceType: "capability_invocation", ResourceID: &invocationID,
			Decision: "deny", ReasonCode: execution.errorCode,
			RequestID: safeRequestID(request.RequestID), IPHash: request.IPHash,
			Metadata: map[string]any{
				"invocation_id": invocationID.String(),
				"error_code":    execution.errorCode,
				"duration_ms":   durationMS,
			},
		})
	}
}

func (s *Service) execute(ctx context.Context, invocationID uuid.UUID, definition *CompiledDefinition,
	object *objects.Object, request InvokeRequest, input map[string]any, plan executionPlan) executionResult {
	switch definition.CapabilityID {
	case IDObjectRead:
		output, err := buildReadOutput(object)
		if err != nil {
			return failedExecution(err)
		}
		return executionResult{status: executionSucceeded, output: output}
	case IDObjectContext:
		output, err := s.projector(ctx, ContextSource{
			ObjectID: object.ID, ObjectType: object.ObjectType, Version: object.Version,
			State: object.State, SharedContext: object.SharedContext,
			Permissions: object.Permissions,
		}, request.Actor.auditActor(), request.RequestID, request.IPHash)
		if err != nil {
			return failedExecution(err)
		}
		return executionResult{status: executionSucceeded, output: output}
	case IDObjectProposeChange:
		return s.executePropose(ctx, object, request, input, plan)
	case IDObjectDerive:
		return s.executeDerive(ctx, object, request, input)
	case IDContentStructure:
		return s.executeExternal(ctx, invocationID, definition, request)
	default:
		return executionResult{status: executionFailed, errorCode: string(httpx.CodeCapabilityFailed),
			message: "unknown capability"}
	}
}

func buildReadOutput(object *objects.Object) (json.RawMessage, error) {
	if err := objects.ValidateStateBytes(object.ObjectType, object.State); err != nil {
		return nil, fmt.Errorf("stored object state is invalid: %w", err)
	}
	if len(object.State) > 64*1024 {
		return nil, errors.New("object state exceeds 64 KiB")
	}
	if len(object.SharedContext) > 8*1024 {
		return nil, errors.New("shared context exceeds 8 KiB")
	}
	return json.Marshal(struct {
		Object struct {
			ID            uuid.UUID       `json:"id"`
			Type          string          `json:"type"`
			Version       int             `json:"version"`
			State         json.RawMessage `json:"state"`
			SharedContext json.RawMessage `json:"shared_context"`
		} `json:"object"`
	}{Object: struct {
		ID            uuid.UUID       `json:"id"`
		Type          string          `json:"type"`
		Version       int             `json:"version"`
		State         json.RawMessage `json:"state"`
		SharedContext json.RawMessage `json:"shared_context"`
	}{
		ID: object.ID, Type: object.ObjectType, Version: object.Version,
		State: object.State, SharedContext: object.SharedContext,
	}})
}

func (s *Service) executePropose(ctx context.Context, object *objects.Object,
	request InvokeRequest, input map[string]any, plan executionPlan) executionResult {
	action, _ := input["action"].(string)
	rawInput, _ := json.Marshal(input["input"])
	expectedVersion, _ := numberToInt(input["expected_version"])
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return failedExecution(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	proposalID, accepted, err := s.objects.CreateProposalTx(ctx, tx, object.ID,
		request.Actor.UserID, plan.proposalProfileID, plan.proposalDelegationID, action, rawInput,
		expectedVersion, request.RequestID, request.IPHash)
	if err != nil {
		if errors.Is(err, objects.ErrVersionConflict) || errors.Is(err, objects.ErrNotFound) ||
			errors.Is(err, objects.ErrForbidden) || errors.Is(err, objects.ErrInvalidRequest) {
			return failedExecution(err)
		}
		return failedExecution(err)
	}
	if !accepted {
		return s.proposalRejection(ctx, object, request, rawInput, expectedVersion)
	}
	if err := tx.Commit(ctx); err != nil {
		return failedExecution(err)
	}
	output, _ := json.Marshal(map[string]any{"proposal_id": proposalID, "status": "pending"})
	return executionResult{status: executionSucceeded, output: output}
}

func (s *Service) proposalRejection(ctx context.Context, object *objects.Object,
	request InvokeRequest, rawInput json.RawMessage, expectedVersion int) executionResult {
	current, err := s.objects.LoadForActor(ctx, request.Actor.UserID, object.ID)
	if err != nil {
		return failedExecution(err)
	}
	if current.Version != expectedVersion {
		return executionResult{status: executionFailed, errorCode: string(httpx.CodeVersionConflict),
			message: "version conflict"}
	}
	if !objects.HasPermission(current.Permissions, objects.PermContribute) {
		return executionResult{status: executionFailed, errorCode: string(httpx.CodeCapabilityDenied),
			message: "capability is not available to this actor"}
	}
	inputSize, err := store.CanonicalJSONSize(rawInput)
	if err != nil {
		return executionResult{status: executionFailed, errorCode: string(httpx.CodeInvalidRequest),
			message: "invalid request"}
	}
	if inputSize > 64*1024 {
		return executionResult{status: executionFailed, errorCode: string(httpx.CodePayloadTooLarge),
			message: "payload too large"}
	}
	return executionResult{status: executionFailed, errorCode: string(httpx.CodeInvalidRequest),
		message: "invalid request"}
}

func (s *Service) executeDerive(ctx context.Context, object *objects.Object,
	request InvokeRequest, input map[string]any) executionResult {
	title, _ := input["title"].(string)
	fields := stringSlice(input["inheritance_fields"])
	var sourceVersion *int
	if raw, ok := input["source_version"]; ok {
		value, ok := numberToInt(raw)
		if !ok {
			return failedExecution(fmt.Errorf("%w: source_version", ErrInvalidRequest))
		}
		sourceVersion = &value
	}
	timeout := s.registry.definitions[IDObjectDerive].ExecutionConstraints.TimeoutMS
	deriveCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()
	result, err := s.deriver.CreateCapability(deriveCtx, request.Actor.UserID, object.ID,
		DeriveRequest{Title: title, SourceVersion: sourceVersion, InheritanceFields: fields},
		request.RequestID, request.IPHash)
	if err != nil {
		return failedExecution(err)
	}
	if result.SourceVersion < 1 || result.ParentVersion < result.SourceVersion {
		return failedExecution(errors.New("derive adapter returned an inconsistent snapshot"))
	}
	if sourceVersion != nil && result.SourceVersion != *sourceVersion {
		return failedExecution(errors.New("derive adapter ignored the requested source_version"))
	}
	output, _ := json.Marshal(map[string]any{"child_object_id": result.ChildID})
	return executionResult{status: executionSucceeded, output: output}
}

func (s *Service) executeExternal(ctx context.Context, invocationID uuid.UUID,
	definition *CompiledDefinition, request InvokeRequest) executionResult {
	timeout := definition.ExecutionConstraints.TimeoutMS
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()
	result, err := s.external.Invoke(callCtx, invocationID.String(), definition.CapabilityID, request.Input)
	if err != nil {
		var providerFailure *ProviderFailure
		if errors.As(err, &providerFailure) {
			if providerFailure.AuthFailure {
				s.logger.Error("capability_provider_auth_failed",
					"provider_id", definition.ProviderID)
			}
			if providerFailure.IntegrityFailure {
				s.logger.Error("capability_provider_integrity_failed",
					"provider_id", definition.ProviderID)
			}
			switch providerFailure.Kind {
			case ProviderTimedOut:
				return executionResult{status: executionTimedOut,
					errorCode: string(httpx.CodeCapabilityTimeout), message: "capability timed out"}
			case ProviderUnknownResult:
				return executionResult{status: executionUnknown,
					errorCode: string(httpx.CodeCapabilityUnknown), message: "capability result is unknown",
					providerAuthFailure:      providerFailure.AuthFailure,
					providerIntegrityFailure: providerFailure.IntegrityFailure}
			default:
				return executionResult{status: executionFailed,
					errorCode: string(httpx.CodeCapabilityFailed), message: "capability failed",
					providerAuthFailure: providerFailure.AuthFailure}
			}
		}
		return failedExecution(err)
	}
	if len(result.Output) > definition.ExecutionConstraints.MaxOutputBytes {
		return executionResult{status: executionFailed,
			errorCode: string(httpx.CodeCapabilityFailed), message: "provider output exceeds max_output_bytes"}
	}
	return executionResult{status: executionSucceeded, output: result.Output}
}

func failedExecution(err error) executionResult {
	code := httpx.CodeInternal
	switch {
	case errors.Is(err, objects.ErrVersionConflict):
		code = httpx.CodeVersionConflict
	case errors.Is(err, objects.ErrForbidden), errors.Is(err, trust.ErrForbidden),
		errors.Is(err, trust.ErrNoActiveDelegation), errors.Is(err, trust.ErrDelegationNotActive):
		code = httpx.CodeCapabilityDenied
	case errors.Is(err, objects.ErrNotFound), errors.Is(err, trust.ErrNotFound):
		code = httpx.CodeNotFound
	case errors.Is(err, objects.ErrInvalidRequest), errors.Is(err, ErrInvalidRequest):
		code = httpx.CodeInvalidRequest
	case errors.Is(err, ErrPayloadTooLarge), errors.Is(err, objects.ErrPayloadTooLarge):
		code = httpx.CodePayloadTooLarge
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		code = httpx.CodeCapabilityTimeout
	}
	message := "capability failed"
	if code == httpx.CodeInternal {
		message = "internal server error"
	}
	return executionResult{status: executionFailed, errorCode: string(code), message: message}
}

func semaValidate(definition *CompiledDefinition, object *objects.Object, input map[string]any) *Failure {
	switch definition.CapabilityID {
	case IDObjectProposeChange:
		action, _ := input["action"].(string)
		rawInput, ok := input["input"]
		if !ok {
			return newFailure(httpx.CodeInvalidRequest, "invalid request",
				fmt.Errorf("%w: input", ErrInvalidRequest))
		}
		encoded, err := json.Marshal(rawInput)
		if err != nil {
			return newFailure(httpx.CodeInvalidRequest, "invalid request", err)
		}
		plan, err := objects.PlanAction(object.ObjectType, action, encoded)
		if err != nil {
			return newFailure(httpx.CodeInvalidRequest, "invalid request",
				fmt.Errorf("%w: action", ErrInvalidRequest))
		}
		if plan.Permission != objects.PermContribute {
			return newFailure(httpx.CodeInvalidRequest, "invalid request",
				fmt.Errorf("%w: action", ErrInvalidRequest))
		}
		if failure := validateActionReferences(object.ObjectType, object.State, plan); failure != nil {
			return failure
		}
		expected, ok := numberToInt(input["expected_version"])
		if !ok {
			return newFailure(httpx.CodeInvalidRequest, "invalid request",
				fmt.Errorf("%w: expected_version", ErrInvalidRequest))
		}
		if expected != object.Version {
			return newFailure(httpx.CodeVersionConflict, "version conflict", ErrVersionConflict)
		}
	case IDObjectDerive:
		title, _ := input["title"].(string)
		if strings.TrimSpace(title) == "" {
			return newFailure(httpx.CodeInvalidRequest, "invalid request",
				fmt.Errorf("%w: title", ErrInvalidRequest))
		}
		fields := stringSlice(input["inheritance_fields"])
		whitelist := deriveWhitelist(object.ObjectType)
		if whitelist == nil {
			return newFailure(httpx.CodeCapabilityFailed, "capability failed", ErrFailed)
		}
		for _, field := range fields {
			if !containsString(whitelist, field) {
				return newFailure(httpx.CodeInvalidRequest, "invalid request",
					fmt.Errorf("%w: inheritance_fields", ErrInvalidRequest))
			}
		}
		if object.ObjectType == objects.TypeInteractive && !containsString(fields, "options") {
			return newFailure(httpx.CodeInvalidRequest, "invalid request",
				fmt.Errorf("%w: inheritance_fields", ErrInvalidRequest))
		}
		if raw, ok := input["source_version"]; ok {
			value, ok := numberToInt(raw)
			if !ok || value < 1 || value > object.Version {
				return newFailure(httpx.CodeInvalidRequest, "invalid request",
					fmt.Errorf("%w: source_version", ErrInvalidRequest))
			}
		}
	}
	return nil
}

func validateOutput(ctx context.Context, definition *CompiledDefinition, capabilityID string, object *objects.Object,
	request InvokeRequest, input map[string]any, output json.RawMessage, service *Service) error {
	if len(output) == 0 || len(output) > definition.ExecutionConstraints.MaxOutputBytes {
		return errors.New("output size is out of range")
	}
	if err := definition.Output.ValidateJSON(output); err != nil {
		return err
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		return err
	}
	switch capabilityID {
	case IDObjectRead:
		inner, _ := decoded["object"].(map[string]any)
		if inner == nil {
			return errors.New("output.object is missing")
		}
		if inner["id"] != object.ID.String() || inner["type"] != object.ObjectType ||
			intValue(inner["version"]) != object.Version {
			return errors.New("output object snapshot does not match the authorized object")
		}
	case IDObjectContext:
		if decoded["object_id"] != object.ID.String() ||
			intValue(decoded["object_version"]) != object.Version {
			return errors.New("projection does not match the authorized object")
		}
		actions, _ := decoded["permitted_actions"].([]any)
		for _, action := range actions {
			value, _ := action.(string)
			if value != objects.PermRead && value != objects.PermContribute && value != objects.PermDerive {
				return errors.New("projection widened permitted_actions")
			}
			if !objects.HasPermission(object.Permissions, value) {
				return errors.New("projection contains an unavailable action")
			}
		}
	case IDObjectProposeChange:
		proposalID, err := uuid.Parse(stringValue(decoded["proposal_id"]))
		if err != nil {
			return err
		}
		proposal, err := service.objects.ProposalForValidation(ctx, proposalID)
		if err != nil {
			return err
		}
		action, _ := input["action"].(string)
		if proposal.ObjectID != object.ID || proposal.ActorUserID != request.Actor.UserID ||
			proposal.Action != action || proposal.Status != "pending" {
			return errors.New("proposal row does not match the invocation")
		}
	case IDObjectDerive:
		childID, err := uuid.Parse(stringValue(decoded["child_object_id"]))
		if err != nil {
			return err
		}
		child, err := service.objects.LoadForActor(ctx, request.Actor.UserID, childID)
		if err != nil {
			return err
		}
		if child.OwnerID != request.Actor.UserID || child.Version != 1 {
			return errors.New("derived child does not match the actor")
		}
		edge, err := service.objects.DerivationEdgeForValidation(ctx, childID)
		if err != nil {
			return err
		}
		if edge.ParentObjectID != object.ID {
			return errors.New("derivation edge points at a different parent")
		}
		fields := stringSlice(input["inheritance_fields"])
		if !sameStringSet(edge.InheritedFields, fields) {
			return errors.New("derivation edge inherited fields do not match the request")
		}
		if raw, ok := input["source_version"]; ok {
			expected, ok := numberToInt(raw)
			if !ok || edge.ParentVersion != expected {
				return errors.New("derivation edge version does not match the request")
			}
		} else if edge.ParentVersion < object.Version {
			return errors.New("derivation edge version precedes the authorized snapshot")
		}
	case IDContentStructure:
		if decoded["format"] != input["format"] {
			return errors.New("provider changed the requested format")
		}
		items, _ := decoded["items"].([]any)
		if intValue(decoded["item_count"]) != len(items) {
			return errors.New("item_count does not match items")
		}
		for index, raw := range items {
			item, _ := raw.(map[string]any)
			if item == nil || intValue(item["index"]) != index {
				return errors.New("item indexes are not contiguous")
			}
		}
	}
	return nil
}

func validateActionReferences(objectType string, state json.RawMessage, plan *objects.ActionPlan) *Failure {
	switch plan.Action {
	case "toggle_checklist_item", "remove_checklist_item":
		var collaborative objects.CollaborativeState
		if err := json.Unmarshal(state, &collaborative); err != nil {
			return newFailure(httpx.CodeInternal, "internal server error", err)
		}
		itemID, _ := plan.Input["item_id"].(string)
		for _, item := range collaborative.Checklist {
			if item.ID == itemID {
				return nil
			}
		}
		return newFailure(httpx.CodeInvalidRequest, "invalid request",
			fmt.Errorf("%w: input.item_id", ErrInvalidRequest))
	case "cast_vote":
		var interactive objects.InteractiveState
		if err := json.Unmarshal(state, &interactive); err != nil {
			return newFailure(httpx.CodeInternal, "internal server error", err)
		}
		optionID, _ := plan.Input["option_id"].(string)
		for _, option := range interactive.Options {
			if option.ID == optionID {
				return nil
			}
		}
		return newFailure(httpx.CodeInvalidRequest, "invalid request",
			fmt.Errorf("%w: input.option_id", ErrInvalidRequest))
	case "update_element", "remove_element", "move_element":
		var scene objects.LivingSceneState
		if err := json.Unmarshal(state, &scene); err != nil {
			return newFailure(httpx.CodeInternal, "internal server error", err)
		}
		elementID, _ := plan.Input["element_id"].(string)
		for _, element := range scene.Elements {
			if element.ID == elementID {
				if plan.Action == "move_element" {
					position, _ := plan.Input["position"].(int)
					if position < 0 || position >= len(scene.Elements) {
						return newFailure(httpx.CodeInvalidRequest, "invalid request",
							fmt.Errorf("%w: input.position", ErrInvalidRequest))
					}
				}
				return nil
			}
		}
		return newFailure(httpx.CodeInvalidRequest, "invalid request",
			fmt.Errorf("%w: input.element_id", ErrInvalidRequest))
	default:
		return nil
	}
}

func invalidInputFailure(err error) *Failure {
	failure := newFailure(httpx.CodeInvalidRequest, "invalid request", ErrInvalidRequest)
	var validation *ValidationError
	if errors.As(err, &validation) && validation.Pointer != "" {
		failure.Field = validation.Pointer
	}
	return failure
}

func errorBody(code httpx.ErrorCode, message, requestID string, invocationID uuid.UUID) []byte {
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"code":       code,
			"message":    message,
			"request_id": safeRequestID(requestID),
			"details":    map[string]any{"invocation_id": invocationID.String()},
		},
	})
	return body
}

func operationScope(capabilityID string) string {
	switch capabilityID {
	case IDObjectRead, IDObjectContext, IDContentStructure:
		return "objects:read"
	case IDObjectProposeChange:
		return "objects:write"
	case IDObjectDerive:
		return "derive:create"
	default:
		return ""
	}
}

func requiredObjectPermission(capabilityID string) string {
	switch capabilityID {
	case IDObjectProposeChange:
		return objects.PermContribute
	case IDObjectDerive:
		return objects.PermDerive
	default:
		return objects.PermRead
	}
}

func deriveWhitelist(objectType string) []string {
	switch objectType {
	case objects.TypeCollaborative:
		return []string{"summary", "body", "checklist", "tags"}
	case objects.TypeInteractive:
		return []string{"prompt", "options"}
	case objects.TypeLivingScene:
		return []string{"premise", "elements", "tags"}
	default:
		return nil
	}
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	present := make(map[string]bool, len(left))
	for _, value := range left {
		if present[value] {
			return false
		}
		present[value] = true
	}
	for _, value := range right {
		if !present[value] {
			return false
		}
	}
	return true
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func stringSlice(value any) []string {
	items, _ := value.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func numberToInt(value any) (int, bool) {
	switch typed := value.(type) {
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed), true
		}
		parsed, err := typed.Float64()
		if err != nil || parsed != math.Trunc(parsed) {
			return 0, false
		}
		return int(parsed), true
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), typed == math.Trunc(typed)
	default:
		return 0, false
	}
}

func intValue(value any) int {
	parsed, _ := numberToInt(value)
	return parsed
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return string(value)
}
