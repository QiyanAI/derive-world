// SPDX-License-Identifier: Apache-2.0
package app

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/httpx"
	"github.com/QiyanAI/derive-world/internal/objects"
)

func (a *App) listProposals(w http.ResponseWriter, r *http.Request) {
	if a.objects == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
		return
	}
	object, _, ok := a.loadAuthorized(w, r, objects.PermRead)
	if !ok {
		return
	}
	if !objects.HasPermission(object.Permissions, objects.PermContribute) {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeForbidden, "forbidden"))
		return
	}
	items, err := a.objects.ListProposals(r.Context(), principalFrom(r.Context()).User.ID, object.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items []objects.Proposal `json:"items"`
	}{Items: items})
}

func (a *App) applyProposal(w http.ResponseWriter, r *http.Request) {
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
	if !objects.HasPermission(object.Permissions, objects.PermContribute) {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeForbidden, "forbidden"))
		return
	}
	proposalID, err := uuid.Parse(r.PathValue("proposal_id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	body, err := a.readRequestBody(w, r)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	if field := detectActorOverride(r, body); field != "" {
		a.rejectActorOverride(w, r, &principal.User.ID, field)
		return
	}
	var input struct {
		ExpectedVersion int `json:"expected_version"`
	}
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	if input.ExpectedVersion < 1 {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "expected_version is required").
			WithDetails(map[string]any{"field": "/expected_version"}))
		return
	}
	view, proposal, err := a.objects.ApplyProposal(r.Context(), principal.User.ID, object.ID, proposalID,
		input.ExpectedVersion, httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		if errors.Is(err, objects.ErrDelegationNotActive) {
			a.auditDeny(r, audit.UserActor(principal.User.ID), audit.ActionAuthzDenied,
				"living_object", &object.ID, "delegation_not_active",
				map[string]any{"proposal_id": proposalID.String()})
		}
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Proposal *objects.Proposal   `json:"proposal"`
		Object   *objects.ObjectView `json:"object"`
	}{Proposal: proposal, Object: view})
}

func (a *App) denyProposal(w http.ResponseWriter, r *http.Request) {
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
	if !objects.HasPermission(object.Permissions, objects.PermContribute) {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeForbidden, "forbidden"))
		return
	}
	proposalID, err := uuid.Parse(r.PathValue("proposal_id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	proposal, err := a.objects.DenyProposal(r.Context(), principal.User.ID, object.ID, proposalID,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, proposal)
}
