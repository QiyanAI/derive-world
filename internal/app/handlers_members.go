// SPDX-License-Identifier: Apache-2.0
package app

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/httpx"
	"github.com/QiyanAI/derive-world/internal/objects"
)

func (a *App) removeMember(w http.ResponseWriter, r *http.Request) {
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
	if !objects.HasPermission(object.Permissions, objects.PermManage) {
		a.auditDeny(r, audit.UserActor(principal.User.ID), audit.ActionAuthzDenied,
			"living_object", &object.ID, "missing_permission",
			map[string]any{"required_permission": objects.PermManage})
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeForbidden, "forbidden"))
		return
	}
	targetID, err := uuid.Parse(r.PathValue("user_id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	if err := a.objects.RemoveMember(r.Context(), principal.User.ID, object.ID, targetID,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders))); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
