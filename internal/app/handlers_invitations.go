// SPDX-License-Identifier: Apache-2.0
package app

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/httpx"
	"github.com/QiyanAI/derive-world/internal/objects"
)

const invitationCreateRoute = "/api/v1/objects/{id}/invitations"

func (a *App) createInvitation(w http.ResponseWriter, r *http.Request) {
	if a.objects == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	object, _, ok := a.loadAuthorized(w, r, objects.PermRead)
	if !ok {
		return
	}
	if !objects.HasPermission(object.Permissions, objects.PermInvite) {
		a.auditDeny(r, audit.UserActor(principal.User.ID), audit.ActionAuthzDenied,
			"living_object", &object.ID, "missing_permission",
			map[string]any{"required_permission": objects.PermInvite})
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeForbidden, "forbidden"))
		return
	}
	body, err := a.readRequestBody(w, r)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	if field := detectActorOverrideExcept(r, body, map[string]bool{"permissions": true}); field != "" {
		a.rejectActorOverride(w, r, &principal.User.ID, field)
		return
	}
	var input objects.CreateInvitationInput
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	key, ok := a.requireIdempotencyKey(w, r)
	if !ok {
		return
	}
	claim, ok := a.claimIdempotency(w, r, principal.User.ID, invitationCreateRoute, key, body)
	if !ok {
		return
	}
	view, err := a.objects.CreateInvitation(r.Context(), principal.User.ID, object.ID, input,
		invitationCreateRoute, key, claim.LeaseUntil,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		a.completeIdempotentFailure(r, principal.User.ID, invitationCreateRoute, key, claim.LeaseUntil, err)
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, view)
}

func (a *App) listInvitations(w http.ResponseWriter, r *http.Request) {
	if a.objects == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
		return
	}
	object, _, ok := a.loadAuthorized(w, r, objects.PermRead)
	if !ok {
		return
	}
	if !objects.HasPermission(object.Permissions, objects.PermInvite) &&
		!objects.HasPermission(object.Permissions, objects.PermManage) {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeForbidden, "forbidden"))
		return
	}
	items, err := a.objects.ListInvitations(r.Context(), object.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items []objects.InvitationView `json:"items"`
	}{Items: items})
}

func (a *App) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	if a.objects == nil {
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

	object, _, ok := a.loadAuthorized(w, r, objects.PermRead)
	if !ok {
		return
	}
	if !objects.HasPermission(object.Permissions, objects.PermInvite) &&
		!objects.HasPermission(object.Permissions, objects.PermManage) {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeForbidden, "forbidden"))
		return
	}
	invitationID, err := uuid.Parse(r.PathValue("invitation_id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	if err := a.objects.RevokeInvitation(r.Context(), principal.User.ID, object.ID, invitationID,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders))); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) previewInvitation(w http.ResponseWriter, r *http.Request) {
	if a.objects == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
		return
	}
	principal := principalFrom(r.Context())
	preview, err := a.objects.PreviewInvitation(r.Context(), principal.User.ID, r.PathValue("token"))
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, preview)
}

func (a *App) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	if a.objects == nil {
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

	result, err := a.objects.AcceptInvitation(r.Context(), principal.User.ID, r.PathValue("token"),
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}
