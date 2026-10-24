// SPDX-License-Identifier: Apache-2.0
package app

import (
	"errors"
	"net/http"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/handoff"
	"github.com/QiyanAI/derive-world/internal/httpx"
)

func (a *App) getHandoff(w http.ResponseWriter, r *http.Request) {
	if a.objects == nil || a.handoff == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
		return
	}
	object, _, ok := a.loadAuthorized(w, r, "read")
	if !ok {
		return
	}
	principal := principalFrom(r.Context())
	projection, err := a.handoff.Build(r.Context(), handoff.Source{
		ObjectID: object.ID, ObjectType: object.ObjectType, Version: object.Version,
		State: object.State, SharedContext: object.SharedContext,
		Permissions: object.Permissions,
	}, audit.UserActor(principal.User.ID),
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		if errors.Is(err, handoff.ErrPayloadTooLarge) {
			httpx.WriteError(w, r, httpx.NewError(httpx.CodePayloadTooLarge, "handoff projection exceeds the size limit"))
			return
		}
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projection)
}
