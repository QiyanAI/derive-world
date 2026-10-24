// SPDX-License-Identifier: Apache-2.0
package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/capability"
	"github.com/QiyanAI/derive-world/internal/httpx"
)

func (a *App) capabilityUnavailable(w http.ResponseWriter, r *http.Request) bool {
	if a.capability != nil {
		return false
	}
	httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
	return true
}

func (a *App) listCapabilities(w http.ResponseWriter, r *http.Request) {
	if a.capabilityUnavailable(w, r) {
		return
	}
	items, err := a.capability.ListDefinitions(r.Context(), true)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items      []capability.Definition `json:"items"`
		NextCursor *string                 `json:"next_cursor"`
	}{Items: items, NextCursor: nil})
}

func (a *App) getCapability(w http.ResponseWriter, r *http.Request) {
	if a.capabilityUnavailable(w, r) {
		return
	}
	capabilityID := strings.TrimSpace(r.PathValue("capability_id"))
	definition, err := a.capability.Definition(r.Context(), capabilityID)
	if err != nil {
		if errors.Is(err, capability.ErrNotFound) {
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
			return
		}
		a.writeDomainError(w, r, err)
		return
	}
	if !definition.Enabled {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, definition)
}

func (a *App) invokeCapability(w http.ResponseWriter, r *http.Request) {
	if a.capabilityUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	remaining, reset, allowed := a.capability.Allow(principal.User.ID)
	a.setRateLimitHeaders(w, a.rateCapabilityLimit, remaining, reset)
	if !allowed {
		a.auditDeny(r, audit.UserActor(principal.User.ID), audit.ActionRateLimitHit,
			"capability_invocation", nil, "rate_limited", nil)
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeRateLimited, "too many capability invocations"))
		return
	}
	if !a.capability.TryAcquire(principal.User.ID) {
		a.auditDeny(r, audit.UserActor(principal.User.ID), audit.ActionRateLimitHit,
			"capability_invocation", nil, "concurrency_limited", nil)
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeRateLimited, "capability concurrency limit reached"))
		return
	}
	defer a.capability.Release(principal.User.ID)

	capabilityID := strings.TrimSpace(r.PathValue("capability_id"))
	if _, ok := a.capability.Registry().Definition(capabilityID); !ok {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	key := r.Header.Get("Idempotency-Key")
	body, err := a.readRequestBody(w, r)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	if field := detectActorOverride(r, body); field != "" {
		a.rejectActorOverride(w, r, &principal.User.ID, field)
		return
	}
	result, err := a.capability.Invoke(r.Context(), capability.InvokeRequest{
		Actor: capability.Caller{
			UserID: principal.User.ID, AuthMethod: principal.AuthMethod,
			Scopes: principal.Scopes,
		},
		CapabilityID:   capabilityID,
		Input:          body,
		ContentType:    r.Header.Get("Content-Type"),
		Route:          matchedRouteTemplate(r),
		ActualPath:     r.URL.Path,
		IdempotencyKey: key,
		RequestID:      httpx.RequestIDFromContext(r.Context()),
		IPHash:         httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)),
	})
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if result.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	w.WriteHeader(result.StatusCode)
	_, _ = w.Write(result.Body)
}

func matchedRouteTemplate(r *http.Request) string {
	pattern := r.Pattern
	if space := strings.IndexByte(pattern, ' '); space >= 0 {
		pattern = pattern[space+1:]
	}
	if pattern == "" {
		return "/api/v1/capabilities/{capability_id}/invoke"
	}
	return pattern
}

func (a *App) getCapabilityInvocation(w http.ResponseWriter, r *http.Request) {
	if a.capabilityUnavailable(w, r) {
		return
	}
	invocationID, ok := parsePathUUID(w, r, "id")
	if !ok {
		return
	}
	principal := principalFrom(r.Context())
	view, err := a.capability.GetInvocation(r.Context(), principal.User.ID, invocationID,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		if errors.Is(err, capability.ErrNotFound) {
			digest := sha256.Sum256([]byte(invocationID.String()))
			a.auditDeny(r, audit.UserActor(principal.User.ID), audit.ActionAuthzNotFound,
				"capability_invocation", nil, "invocation_not_visible",
				map[string]any{"resource_hash": hex.EncodeToString(digest[:8])})
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
			return
		}
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, view)
}
