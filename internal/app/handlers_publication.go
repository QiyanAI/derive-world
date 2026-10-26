// SPDX-License-Identifier: Apache-2.0
package app

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/httpx"
	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/publication"
)

const (
	publicationsRoute = "/api/v1/objects/{id}/publications"
	publicDeriveRoute = "/api/v1/public/worlds/{id}/derive"
)

func (a *App) publicationServiceUnavailable(w http.ResponseWriter, r *http.Request) bool {
	if a.publication != nil {
		return false
	}
	httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
	return true
}

func (a *App) listPublications(w http.ResponseWriter, r *http.Request) {
	if a.publicationServiceUnavailable(w, r) {
		return
	}
	object, _, ok := a.loadAuthorized(w, r, objects.PermRead)
	if !ok {
		return
	}
	items, err := a.publication.ListForObject(r.Context(), principalFrom(r.Context()).User.ID, object.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items []publication.Publication `json:"items"`
	}{Items: items})
}

func (a *App) createPublication(w http.ResponseWriter, r *http.Request) {
	if a.publicationServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	if !a.allowUserLimit(w, r, principal.User.ID, a.publicationLimiter, 10,
		"publication_rate_limited", "too many publication requests") {
		return
	}
	object, _, ok := a.loadAuthorized(w, r, objects.PermManage)
	if !ok {
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
	var input publication.CreateInput
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	key, ok := a.requireIdempotencyKey(w, r)
	if !ok {
		return
	}
	claim, ok := a.claimIdempotency(w, r, principal.User.ID, publicationsRoute, key, body)
	if !ok {
		return
	}
	created, err := a.publication.Create(r.Context(), principal.User.ID, object.ID, input,
		publicationsRoute, key, claim.LeaseUntil,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		a.completeIdempotentFailure(r, principal.User.ID, publicationsRoute, key, claim.LeaseUntil, err)
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
}

func (a *App) revokePublication(w http.ResponseWriter, r *http.Request) {
	if a.publicationServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	if a.rejectActorOverrideInBody(w, r) {
		return
	}
	publicationID, ok := parseUUIDParam(r, "id")
	if !ok {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	if err := a.publication.Revoke(r.Context(), principal.User.ID, publicationID,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders))); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) listPublicationConsents(w http.ResponseWriter, r *http.Request) {
	if a.publicationServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	items, err := a.publication.PendingConsents(r.Context(), principal.User.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items []publication.Consent `json:"items"`
	}{Items: items})
}

func (a *App) approvePublicationConsent(w http.ResponseWriter, r *http.Request) {
	a.respondPublicationConsent(w, r, true)
}

func (a *App) declinePublicationConsent(w http.ResponseWriter, r *http.Request) {
	a.respondPublicationConsent(w, r, false)
}

func (a *App) respondPublicationConsent(w http.ResponseWriter, r *http.Request, approve bool) {
	if a.publicationServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	if a.rejectActorOverrideInBody(w, r) {
		return
	}
	consentID, ok := parseUUIDParam(r, "id")
	if !ok {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	consent, err := a.publication.RespondConsent(r.Context(), principal.User.ID, consentID, approve,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	a.notifyUser(principal.User.ID)
	httpx.WriteJSON(w, http.StatusOK, consent)
}

// publicWorlds is anonymous and only returns active publications.
func (a *App) publicWorlds(w http.ResponseWriter, r *http.Request) {
	if a.publicationServiceUnavailable(w, r) {
		return
	}
	limit, err := httpx.ParseLimit(r.URL.Query().Get("limit"), 20, 50)
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "limit is invalid"))
		return
	}
	var cursor *publication.PublicCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		decoded, err := httpx.DecodeCursor([]byte(a.config.CursorHMACSecret), raw)
		if err != nil {
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "cursor is invalid"))
			return
		}
		prefix, stamp, ok := strings.Cut(decoded.Key, "|")
		if !ok || prefix != "public" {
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "cursor is invalid"))
			return
		}
		createdAt, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "cursor is invalid"))
			return
		}
		id, err := uuid.Parse(decoded.ID)
		if err != nil {
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "cursor is invalid"))
			return
		}
		cursor = &publication.PublicCursor{CreatedAt: createdAt, ID: id}
	}
	items, next, err := a.publication.PublicList(r.Context(), cursor, limit)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	var nextCursor *string
	if next != nil {
		encoded, err := httpx.EncodeCursor([]byte(a.config.CursorHMACSecret), httpx.Cursor{
			Key: "public|" + next.CreatedAt.UTC().Format(time.RFC3339Nano), ID: next.ID.String(),
		})
		if err != nil {
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "internal server error"))
			return
		}
		nextCursor = &encoded
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items      []publication.PublicWorld `json:"items"`
		NextCursor *string                   `json:"next_cursor"`
	}{Items: items, NextCursor: nextCursor})
}

func (a *App) publicWorld(w http.ResponseWriter, r *http.Request) {
	if a.publicationServiceUnavailable(w, r) {
		return
	}
	publicationID, ok := parseUUIDParam(r, "id")
	if !ok {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	item, err := a.publication.PublicGet(r.Context(), publicationID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (a *App) publicWorldLineage(w http.ResponseWriter, r *http.Request) {
	if a.publicationServiceUnavailable(w, r) {
		return
	}
	publicationID, ok := parseUUIDParam(r, "id")
	if !ok {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	lineage, err := a.publication.PublicLineage(r.Context(), publicationID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, lineage)
}

func (a *App) reportPublicWorld(w http.ResponseWriter, r *http.Request) {
	if a.publicationServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	if !a.allowUserLimit(w, r, principal.User.ID, a.reportLimiter, 5,
		"report_rate_limited", "too many reports") {
		return
	}
	publicationID, ok := parseUUIDParam(r, "id")
	if !ok {
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
		Reason string `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	if err := a.publication.Report(r.Context(), principal.User.ID, publicationID, input.Reason,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders))); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) publicDerive(w http.ResponseWriter, r *http.Request) {
	if a.publicationServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	publicationID, ok := parseUUIDParam(r, "id")
	if !ok {
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
		Title             string   `json:"title"`
		InheritanceFields []string `json:"inheritance_fields"`
	}
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	key, ok := a.requireIdempotencyKey(w, r)
	if !ok {
		return
	}
	claim, ok := a.claimIdempotency(w, r, principal.User.ID, publicDeriveRoute, key, body)
	if !ok {
		return
	}
	view, err := a.publication.PublicDerive(r.Context(), principal.User.ID, publicationID,
		input.Title, input.InheritanceFields, publicDeriveRoute, key, claim.LeaseUntil,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		a.completeIdempotentFailure(r, principal.User.ID, publicDeriveRoute, key, claim.LeaseUntil, err)
		a.writeDomainError(w, r, err)
		return
	}
	a.notifyUser(principal.User.ID)
	httpx.WriteJSON(w, http.StatusCreated, view)
}
