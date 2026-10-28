// SPDX-License-Identifier: Apache-2.0
package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/derive"
	"github.com/QiyanAI/derive-world/internal/httpx"
	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/trust"
)

const confirmationApproveRoute = "/api/v1/confirmations/{id}/approve"

func (a *App) listConfirmations(w http.ResponseWriter, r *http.Request) {
	if a.trust == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
		return
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}
	principal := principalFrom(r.Context())
	items, err := a.trust.ListConfirmations(r.Context(), principal.User.ID, status)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items []trust.ConfirmationView `json:"items"`
	}{Items: items})
}

func (a *App) getConfirmation(w http.ResponseWriter, r *http.Request) {
	if a.trust == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
		return
	}
	confirmationID, ok := parsePathUUID(w, r, "id")
	if !ok {
		return
	}
	principal := principalFrom(r.Context())
	view, err := a.trust.GetConfirmation(r.Context(), principal.User.ID, confirmationID)
	if err != nil {
		if errors.Is(err, trust.ErrConfirmationExpired) {
			a.expireCapabilityConfirmation(r.Context(), principal.User.ID, confirmationID)
		}
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, view)
}

func (a *App) approveConfirmation(w http.ResponseWriter, r *http.Request) {
	if a.trust == nil || a.derive == nil || a.objects == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	if a.rejectActorOverrideInBody(w, r) {
		return
	}

	confirmationID, ok := parsePathUUID(w, r, "id")
	if !ok {
		return
	}
	// Idempotent replay is authorized against the current target resource.
	// A confirmation for an object the user can no longer read must not replay
	// an old 200 (D05-20).
	view, err := a.trust.GetConfirmation(r.Context(), principal.User.ID, confirmationID)
	if err != nil {
		if errors.Is(err, trust.ErrConfirmationExpired) {
			a.expireCapabilityConfirmation(r.Context(), principal.User.ID, confirmationID)
		}
		a.writeDomainError(w, r, err)
		return
	}
	if view.ResourceType == "living_object" {
		if _, err := a.objects.LoadForActor(r.Context(), principal.User.ID, view.ResourceID); err != nil {
			if errors.Is(err, objects.ErrNotFound) {
				a.auditNotFound(r, audit.UserActor(principal.User.ID), view.ResourceID)
				httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
				return
			}
			a.writeDomainError(w, r, err)
			return
		}
	}
	if view.ResourceType == "capability_invocation" {
		if a.capability == nil {
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
			return
		}
		objectID, hasObject, err := a.capability.ObjectIDForInvocation(r.Context(), principal.User.ID, view.ResourceID)
		if err != nil {
			a.writeDomainError(w, r, err)
			return
		}
		if hasObject {
			if _, err := a.objects.LoadForActor(r.Context(), principal.User.ID, objectID); err != nil {
				if errors.Is(err, objects.ErrNotFound) {
					a.auditNotFound(r, audit.UserActor(principal.User.ID), objectID)
					httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
					return
				}
				a.writeDomainError(w, r, err)
				return
			}
		}
	}
	key, ok := a.requireIdempotencyKey(w, r)
	if !ok {
		return
	}
	claim, ok := a.claimIdempotency(w, r, principal.User.ID, confirmationApproveRoute, key, nil, func() error {
		if view.ResourceType == "living_object" {
			if err := a.trust.ConfirmationReplayAllowed(r.Context(), principal.User.ID, confirmationID); err != nil {
				a.auditNotFound(r, audit.UserActor(principal.User.ID), view.ResourceID)
				return err
			}
			return nil
		}
		if view.ResourceType == "personal_ai" {
			return a.trust.WorldCreateReplayAllowed(r.Context(), principal.User.ID, confirmationID)
		}
		confirmation, err := a.trust.ConfirmationForReplay(r.Context(), principal.User.ID, confirmationID)
		if err != nil {
			return err
		}
		payload, err := trust.DecodeCapabilityConfirmationPayload(confirmation.Payload)
		if err != nil {
			return trust.ErrConflict
		}
		return a.capability.AIDeriveReplayAllowed(r.Context(), principal.User.ID, payload)
	})
	if !ok {
		return
	}
	body, err := a.trust.Approve(r.Context(), principal.User.ID, confirmationID,
		confirmationApproveRoute, key, claim.LeaseUntil,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)),
		func(ctx context.Context, tx pgx.Tx, confirmation trust.Confirmation) ([]byte, error) {
			return a.executeConfirmation(ctx, tx, confirmation)
		})
	if err != nil {
		if errors.Is(err, trust.ErrConfirmationExpired) && view.ResourceType == "capability_invocation" &&
			a.capability != nil {
			_ = a.capability.DenyAIInvocation(r.Context(), view.ResourceID,
				principal.User.ID, "confirmation expired", "confirmation_expired")
		}
		_ = a.idem.Cancel(r.Context(), principal.User.ID, confirmationApproveRoute, key, claim.LeaseUntil)
		a.writeDomainError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (a *App) expireCapabilityConfirmation(ctx context.Context, userID, confirmationID uuid.UUID) {
	if a.capability == nil {
		return
	}
	confirmation, err := a.trust.ConfirmationForReplay(ctx, userID, confirmationID)
	if err != nil || confirmation.ResourceType != "capability_invocation" {
		return
	}
	_ = a.capability.DenyAIInvocation(ctx, confirmation.ResourceID,
		userID, "confirmation expired", "confirmation_expired")
}

func (a *App) executeConfirmation(ctx context.Context, tx pgx.Tx, confirmation trust.Confirmation) ([]byte, error) {
	if confirmation.ResourceType == "personal_ai" {
		payload, err := trust.DecodeWorldCreatePayload(confirmation.Payload)
		if err != nil {
			return nil, err
		}
		effective, err := a.trust.ResolveGlobalAITx(ctx, tx, confirmation.UserID, payload.AIProfileID)
		if err != nil {
			return nil, err
		}
		if !effective.Can(objects.PermContribute) {
			return nil, trust.ErrForbidden
		}
		active, err := a.trust.ActiveDelegationTx(ctx, tx, payload.DelegationID,
			confirmation.UserID, payload.AIProfileID, nil)
		if err != nil {
			return nil, err
		}
		if !active {
			return nil, trust.ErrDelegationNotActive
		}
		confirmedBy := confirmation.UserID
		view, err := a.objects.CreateWithinTx(ctx, tx, confirmation.UserID, objects.CreateInput{
			ObjectType: objects.TypeOpenWorld,
			Title:      payload.Title,
			Content:    payload.Content,
		}, httpx.RequestIDFromContext(ctx), nil, objects.CreateOrigin{
			Actor:          effective.Actor(),
			ConfirmationID: &confirmation.ID,
			ConfirmedBy:    &confirmedBy,
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(view)
	}
	if confirmation.ResourceType == "capability_invocation" {
		if a.capability == nil {
			return nil, trust.ErrUnsupportedAction
		}
		payload, err := trust.DecodeCapabilityConfirmationPayload(confirmation.Payload)
		if err != nil {
			return nil, trust.ErrConflict
		}
		return a.capability.ApproveAIDeriveTx(ctx, tx, confirmation.UserID, confirmation.ID, payload)
	}
	payload, err := trust.DecodeDerivePayload(confirmation.Payload)
	if err != nil {
		return nil, err
	}
	effective, err := a.trust.ResolveAITx(ctx, tx, confirmation.UserID,
		payload.AIProfileID, payload.ObjectID)
	if err != nil {
		return nil, err
	}
	active, err := a.trust.ActiveDelegationTx(ctx, tx, payload.DelegationID,
		confirmation.UserID, payload.AIProfileID, &payload.ObjectID)
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, trust.ErrDelegationNotActive
	}
	if effective.ObjectVersion != payload.ExpectedVersion {
		return nil, trust.ErrVersionConflict
	}
	if !effective.Can("derive") {
		return nil, trust.ErrForbidden
	}
	view, err := a.derive.CreateConfirmedTx(ctx, tx, confirmation.UserID, payload.ObjectID,
		payload.ExpectedVersion, derive.Request{
			Title: payload.Title, SourceVersion: payload.SourceVersion,
			InheritanceFields: payload.InheritanceFields,
		}, payload.AIProfileID, payload.DelegationID, confirmation.ID,
		httpx.RequestIDFromContext(ctx),
		nil)
	if err != nil {
		return nil, err
	}
	return json.Marshal(view)
}

func (a *App) denyConfirmation(w http.ResponseWriter, r *http.Request) {
	if a.trust == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	if a.rejectActorOverrideInBody(w, r) {
		return
	}

	confirmationID, ok := parsePathUUID(w, r, "id")
	if !ok {
		return
	}
	view, err := a.trust.Deny(r.Context(), principal.User.ID, confirmationID,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		if errors.Is(err, trust.ErrConfirmationExpired) {
			a.expireCapabilityConfirmation(r.Context(), principal.User.ID, confirmationID)
		}
		a.writeDomainError(w, r, err)
		return
	}
	if view.ResourceType == "capability_invocation" && a.capability != nil {
		_ = a.capability.DenyAIInvocation(r.Context(), view.ResourceID,
			principal.User.ID, "confirmation denied", "capability_denied")
	}
	httpx.WriteJSON(w, http.StatusOK, view)
}
