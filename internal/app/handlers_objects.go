// SPDX-License-Identifier: Apache-2.0
package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/derive"
	"github.com/QiyanAI/derive-world/internal/httpx"
	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/store"
)

const (
	createRoute = "/api/v1/objects"
	actionRoute = "/api/v1/objects/{id}/actions"
	deriveRoute = "/api/v1/objects/{id}/derive"
)

func (a *App) objectServiceUnavailable(w http.ResponseWriter, r *http.Request) bool {
	if a.objects != nil {
		return false
	}
	httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
	return true
}

func parseObjectID(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// loadAuthorized performs LoadResource and the required permission check. It
// returns false after writing the response.
func (a *App) loadAuthorized(w http.ResponseWriter, r *http.Request, required string) (*objects.Object, *uuid.UUID, bool) {
	principal := principalFrom(r.Context())
	if principal == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeUnauthenticated, "authentication required"))
		return nil, nil, false
	}
	objectID, ok := parseObjectID(r)
	if !ok {
		a.auditNotFound(r, audit.UserActor(principal.User.ID), uuid.Nil)
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return nil, nil, false
	}
	object, err := a.objects.LoadForActor(r.Context(), principal.User.ID, objectID)
	if err != nil {
		if errors.Is(err, objects.ErrNotFound) {
			a.auditNotFound(r, audit.UserActor(principal.User.ID), objectID)
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
			return nil, nil, false
		}
		a.writeDomainError(w, r, err)
		return nil, nil, false
	}
	if required != "" && !objects.HasPermission(object.Permissions, required) {
		a.auditDeny(r, audit.UserActor(principal.User.ID), audit.ActionAuthzDenied,
			"living_object", &objectID, "missing_permission",
			map[string]any{"required_permission": required})
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeForbidden, "forbidden"))
		return nil, nil, false
	}
	actorID := principal.User.ID
	return object, &actorID, true
}

func (a *App) requireIdempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "Idempotency-Key is required"))
		return "", false
	}
	if !store.ValidIdempotencyKey(key) {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "Idempotency-Key has an invalid format"))
		return "", false
	}
	return key, true
}

func (a *App) claimIdempotency(w http.ResponseWriter, r *http.Request, actorID uuid.UUID, route, key string, body []byte, replayChecks ...func() error) (*store.ClaimResult, bool) {
	hash, err := store.RequestHash(r.Method, r.URL.Path, body)
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "request body is not valid JSON"))
		return nil, false
	}
	claim, err := a.idem.Claim(r.Context(), actorID, route, key, hash)
	if err != nil {
		a.writeDomainError(w, r, err)
		return nil, false
	}
	if claim.Replay {
		for _, check := range replayChecks {
			if check == nil {
				continue
			}
			if err := check(); err != nil {
				a.writeDomainError(w, r, err)
				return nil, false
			}
		}
		a.writeReplay(w, claim)
		return nil, false
	}
	return claim, true
}

func (a *App) writeReplay(w http.ResponseWriter, claim *store.ClaimResult) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Idempotency-Replayed", "true")
	if claim.Truncated {
		w.Header().Set("Idempotency-Truncated", "true")
	}
	w.WriteHeader(claim.StatusCode)
	_, _ = w.Write(claim.Body)
}

func (a *App) listObjects(w http.ResponseWriter, r *http.Request) {
	if a.objectServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "all"
	}
	if scope != "owned" && scope != "shared" && scope != "all" {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "scope must be owned, shared, or all"))
		return
	}
	limit, err := httpx.ParseLimit(r.URL.Query().Get("limit"), 20, 100)
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "limit is invalid"))
		return
	}
	var cursor *objects.ListCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		decoded, err := httpx.DecodeCursor([]byte(a.config.CursorHMACSecret), raw)
		if err != nil {
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "cursor is invalid"))
			return
		}
		prefix, stamp, ok := strings.Cut(decoded.Key, "|")
		if !ok || prefix != scope {
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
		cursor = &objects.ListCursor{CreatedAt: createdAt, ID: id}
	}
	items, next, err := a.objects.List(r.Context(), principal.User.ID, scope, cursor, limit)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	var nextCursor *string
	if next != nil {
		encoded, err := httpx.EncodeCursor([]byte(a.config.CursorHMACSecret), httpx.Cursor{
			Key: scope + "|" + next.CreatedAt.UTC().Format(time.RFC3339Nano), ID: next.ID.String(),
		})
		if err != nil {
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "internal server error"))
			return
		}
		nextCursor = &encoded
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items      []objects.ListItem `json:"items"`
		NextCursor *string            `json:"next_cursor"`
	}{Items: items, NextCursor: nextCursor})
}

func (a *App) createObject(w http.ResponseWriter, r *http.Request) {
	if a.objectServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
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
	var input objects.CreateInput
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	if _, _, err := objects.ValidateCreate(input); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	key, ok := a.requireIdempotencyKey(w, r)
	if !ok {
		return
	}
	claim, ok := a.claimIdempotency(w, r, principal.User.ID, createRoute, key, body)
	if !ok {
		return
	}
	view, err := a.objects.Create(r.Context(), principal.User.ID, input, createRoute, key, claim.LeaseUntil,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		a.completeIdempotentFailure(r, principal.User.ID, createRoute, key, claim.LeaseUntil, err)
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, view)
}

func (a *App) getObject(w http.ResponseWriter, r *http.Request) {
	if a.objectServiceUnavailable(w, r) {
		return
	}
	object, _, ok := a.loadAuthorized(w, r, objects.PermRead)
	if !ok {
		return
	}
	view := object.View()
	lineage, err := a.objects.LineageForObject(r.Context(), principalFrom(r.Context()).User.ID, object.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	view.Lineage = lineage
	httpx.WriteJSON(w, http.StatusOK, view)
}

func (a *App) deleteObject(w http.ResponseWriter, r *http.Request) {
	if a.objectServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	if a.rejectActorOverrideInBody(w, r) {
		return
	}

	object, actorID, ok := a.loadAuthorized(w, r, objects.PermManage)
	if !ok {
		return
	}
	if err := a.objects.Delete(r.Context(), *actorID, object.ID,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders))); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) listEvents(w http.ResponseWriter, r *http.Request) {
	if a.objectServiceUnavailable(w, r) {
		return
	}
	object, _, ok := a.loadAuthorized(w, r, objects.PermRead)
	if !ok {
		return
	}
	limit, err := httpx.ParseLimit(r.URL.Query().Get("limit"), 20, 100)
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "limit is invalid"))
		return
	}
	var cursor int64
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		cursor, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || cursor < 0 {
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "cursor is invalid"))
			return
		}
	}
	// Events uses the viewer for proposal visibility.
	principal := principalFrom(r.Context())
	items, next, err := a.objects.Events(r.Context(), principal.User.ID, object.ID, object.Permissions, cursor, limit)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	var nextCursor *string
	if next != nil {
		value := strconv.FormatInt(*next, 10)
		nextCursor = &value
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items      []objects.Event `json:"items"`
		NextCursor *string         `json:"next_cursor"`
	}{Items: items, NextCursor: nextCursor})
}

func (a *App) postAction(w http.ResponseWriter, r *http.Request) {
	if a.objectServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	object, _, ok := a.loadAuthorized(w, r, "")
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
	var envelope struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Action == "" {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "action is required").WithDetails(map[string]any{"field": "/action"}))
		return
	}
	required, knownAction := objects.RequiredPermission(object.ObjectType, envelope.Action)
	if !knownAction {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "action is not allowed for this template").WithDetails(map[string]any{"field": "/action"}))
		return
	}
	if !objects.HasPermission(object.Permissions, required) {
		a.auditDeny(r, audit.UserActor(principal.User.ID), audit.ActionAuthzDenied,
			"living_object", &object.ID, "missing_permission",
			map[string]any{"action": envelope.Action, "required_permission": required})
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeForbidden, "forbidden"))
		return
	}
	var input objects.ActionInput
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	if len(bytes.TrimSpace(input.Input)) == 0 {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "input is required").WithDetails(map[string]any{"field": "/input"}))
		return
	}
	if input.ExpectedVersion < 1 {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "expected_version is required").WithDetails(map[string]any{"field": "/expected_version"}))
		return
	}
	plan, err := objects.PlanAction(object.ObjectType, input.Action, input.Input)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	key, ok := a.requireIdempotencyKey(w, r)
	if !ok {
		return
	}
	claim, ok := a.claimIdempotency(w, r, principal.User.ID, actionRoute, key, body)
	if !ok {
		return
	}
	if object.Version != input.ExpectedVersion {
		err := objects.ErrVersionConflict
		a.auditDeny(r, audit.UserActor(principal.User.ID), audit.ActionAuthzDenied,
			"living_object", &object.ID, "version_conflict",
			map[string]any{"action": input.Action, "expected_version": input.ExpectedVersion, "current_version": object.Version})
		a.completeIdempotentFailure(r, principal.User.ID, actionRoute, key, claim.LeaseUntil, err)
		a.writeDomainError(w, r, err)
		return
	}
	view, err := a.objects.ApplyAction(r.Context(), principal.User.ID, object.ID,
		input.ExpectedVersion, plan, actionRoute, key, claim.LeaseUntil,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		if errors.Is(err, objects.ErrVersionConflict) || errors.Is(err, objects.ErrConflict) {
			reason := "conflict"
			if errors.Is(err, objects.ErrVersionConflict) {
				reason = "version_conflict"
			}
			a.auditDeny(r, audit.UserActor(principal.User.ID), audit.ActionAuthzDenied,
				"living_object", &object.ID, reason,
				map[string]any{"action": input.Action, "expected_version": input.ExpectedVersion})
		}
		a.completeIdempotentFailure(r, principal.User.ID, actionRoute, key, claim.LeaseUntil, err)
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, view)
}

func (a *App) listMembers(w http.ResponseWriter, r *http.Request) {
	if a.objectServiceUnavailable(w, r) {
		return
	}
	object, _, ok := a.loadAuthorized(w, r, objects.PermRead)
	if !ok {
		return
	}
	members, err := a.objects.Members(r.Context(), object.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items []objects.Member `json:"items"`
	}{Items: members})
}

func (a *App) deriveObject(w http.ResponseWriter, r *http.Request) {
	if a.objectServiceUnavailable(w, r) {
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
	if !objects.HasPermission(object.Permissions, objects.PermDerive) {
		a.auditDeny(r, audit.UserActor(principal.User.ID), audit.ActionAuthzDenied,
			"living_object", &object.ID, "missing_permission",
			map[string]any{"required_permission": objects.PermDerive})
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeForbidden, "forbidden"))
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
	var input derive.Request
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	key, ok := a.requireIdempotencyKey(w, r)
	if !ok {
		return
	}
	hash, err := store.RequestHash(r.Method, r.URL.Path, body)
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "request body is not valid JSON"))
		return
	}
	view, claim, err := a.derive.Create(r.Context(), principal.User.ID, object.ID, input, deriveRoute, key, hash,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		if claim != nil && claim.Reserved {
			a.completeIdempotentFailure(r, principal.User.ID, deriveRoute, key, claim.LeaseUntil, err)
		}
		a.writeDomainError(w, r, err)
		return
	}
	if claim != nil && claim.Replay {
		a.writeReplay(w, claim)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, view)
}

func (a *App) getLineage(w http.ResponseWriter, r *http.Request) {
	if a.objectServiceUnavailable(w, r) {
		return
	}
	depth := objects.MaxLineageDepth
	if raw := r.URL.Query().Get("depth"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > objects.MaxLineageDepth {
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "depth must be 1..10"))
			return
		}
		depth = parsed
	}
	object, _, ok := a.loadAuthorized(w, r, objects.PermRead)
	if !ok {
		return
	}
	lineage, err := a.objects.Lineage(r.Context(), principalFrom(r.Context()).User.ID, object.ID, object.Version, depth)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, lineage)
}
