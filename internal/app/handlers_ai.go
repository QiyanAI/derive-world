// SPDX-License-Identifier: Apache-2.0
package app

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/httpx"
	"github.com/QiyanAI/derive-world/internal/model"
	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/personalai"
	"github.com/QiyanAI/derive-world/internal/trust"
)

const (
	conversationCreateRoute = "/api/v1/ai/conversations"
	messageRoute            = "/api/v1/ai/conversations/{id}/messages"
)

func (a *App) requirePersonalAI(w http.ResponseWriter, r *http.Request) bool {
	if a.personalAI != nil && a.trust != nil {
		return true
	}
	httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
	return false
}

type profileUsageResponse struct {
	Calls                int    `json:"calls"`
	InputChars           int    `json:"input_chars"`
	DailyCallsLimit      int    `json:"daily_calls_limit"`
	DailyInputCharsLimit int    `json:"daily_input_chars_limit"`
	ModelMode            string `json:"model_mode"`
	ModelName            string `json:"model_name"`
}

func (a *App) getAIProfile(w http.ResponseWriter, r *http.Request) {
	if !a.requirePersonalAI(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	profile, err := a.personalAI.GetProfile(r.Context(), principal.User.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	calls, chars, err := model.NewRecorder().DayUsage(r.Context(), a.store.Pool(), principal.User.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	modelName := a.config.Model.Name
	if modelName == "" {
		modelName = "fixture-model"
	}
	dailyCallsLimit := a.config.Model.DailyCallsPerUser
	if dailyCallsLimit <= 0 {
		dailyCallsLimit = model.DefaultDailyCalls
	}
	dailyCharsLimit := a.config.Model.DailyInputCharsPerUser
	if dailyCharsLimit <= 0 {
		dailyCharsLimit = model.DefaultDailyInputChars
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		*personalai.Profile
		Usage profileUsageResponse `json:"usage"`
	}{
		Profile: profile,
		Usage: profileUsageResponse{
			Calls: calls, InputChars: chars,
			DailyCallsLimit:      dailyCallsLimit,
			DailyInputCharsLimit: dailyCharsLimit,
			ModelMode:            a.config.Model.Mode, ModelName: modelName,
		},
	})
}

func (a *App) patchAIProfile(w http.ResponseWriter, r *http.Request) {
	if !a.requirePersonalAI(w, r) {
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
	var input personalai.UpdateProfileInput
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	profile, err := a.personalAI.UpdateProfile(r.Context(), principal.User.ID, input,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, profile)
}

func (a *App) getAIContext(w http.ResponseWriter, r *http.Request) {
	if !a.requirePersonalAI(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	value, err := a.personalAI.GetContext(r.Context(), principal.User.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}

func (a *App) putAIContext(w http.ResponseWriter, r *http.Request) {
	if !a.requirePersonalAI(w, r) {
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
	var input struct {
		Content *string `json:"content"`
	}
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	if input.Content == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "content is required").
			WithDetails(map[string]any{"field": "/content"}))
		return
	}
	value, err := a.personalAI.UpdateContext(r.Context(), principal.User.ID, *input.Content,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}

func (a *App) listConversations(w http.ResponseWriter, r *http.Request) {
	if !a.requirePersonalAI(w, r) {
		return
	}
	limit, err := httpx.ParseLimit(r.URL.Query().Get("limit"), 20, 100)
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "limit is invalid"))
		return
	}
	var cursor *personalai.ConversationCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		decoded, err := httpx.DecodeCursor([]byte(a.config.CursorHMACSecret), raw)
		if err != nil {
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "cursor is invalid"))
			return
		}
		prefix, stamp, ok := strings.Cut(decoded.Key, "|")
		if !ok || prefix != "conversations" {
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "cursor is invalid"))
			return
		}
		updatedAt, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "cursor is invalid"))
			return
		}
		id, err := uuid.Parse(decoded.ID)
		if err != nil {
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "cursor is invalid"))
			return
		}
		cursor = &personalai.ConversationCursor{UpdatedAt: updatedAt, ID: id}
	}
	principal := principalFrom(r.Context())
	items, next, err := a.personalAI.ListConversations(r.Context(), principal.User.ID, cursor, limit)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	var nextCursor *string
	if next != nil {
		encoded, err := httpx.EncodeCursor([]byte(a.config.CursorHMACSecret), httpx.Cursor{
			Key: "conversations|" + next.UpdatedAt.UTC().Format(time.RFC3339Nano), ID: next.ID.String(),
		})
		if err != nil {
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "internal server error"))
			return
		}
		nextCursor = &encoded
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items      []personalai.Conversation `json:"items"`
		NextCursor *string                   `json:"next_cursor"`
	}{Items: items, NextCursor: nextCursor})
}

func (a *App) createConversation(w http.ResponseWriter, r *http.Request) {
	if !a.requirePersonalAI(w, r) {
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
	var input personalai.CreateConversationInput
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	if input.ObjectID != nil {
		if _, err := a.objects.LoadForActor(r.Context(), principal.User.ID, *input.ObjectID); err != nil {
			if errors.Is(err, objects.ErrNotFound) {
				a.auditNotFound(r, audit.UserActor(principal.User.ID), *input.ObjectID)
				httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
				return
			}
			a.writeDomainError(w, r, err)
			return
		}
	}
	key, ok := a.requireIdempotencyKey(w, r)
	if !ok {
		return
	}
	claim, ok := a.claimIdempotency(w, r, principal.User.ID, conversationCreateRoute, key, body)
	if !ok {
		return
	}
	conversation, err := a.personalAI.CreateConversation(r.Context(), principal.User.ID, input,
		conversationCreateRoute, key, claim.LeaseUntil,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		a.completeIdempotentFailure(r, principal.User.ID, conversationCreateRoute, key, claim.LeaseUntil, err)
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, conversation)
}

func (a *App) getConversation(w http.ResponseWriter, r *http.Request) {
	if !a.requirePersonalAI(w, r) {
		return
	}
	conversationID, ok := parsePathUUID(w, r, "id")
	if !ok {
		return
	}
	principal := principalFrom(r.Context())
	detail, err := a.personalAI.GetConversation(r.Context(), principal.User.ID, conversationID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, detail)
}

func (a *App) deleteConversation(w http.ResponseWriter, r *http.Request) {
	if !a.requirePersonalAI(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	if a.rejectActorOverrideInBody(w, r) {
		return
	}

	conversationID, ok := parsePathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := a.personalAI.DeleteConversation(r.Context(), principal.User.ID, conversationID,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders))); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) sendMessage(w http.ResponseWriter, r *http.Request) {
	if !a.requirePersonalAI(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	if !a.allowAI(w, r, principal.User.ID) {
		return
	}
	conversationID, ok := parsePathUUID(w, r, "id")
	if !ok {
		return
	}
	conversation, err := a.personalAI.ConversationMeta(r.Context(), principal.User.ID, conversationID)
	if err != nil {
		a.writeDomainError(w, r, err)
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
		Content string `json:"content"`
		Intent  string `json:"intent"`
	}
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	switch input.Intent {
	case "", "create", "change":
	default:
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "intent must be create or change").WithDetails(map[string]any{"field": "/intent"}))
		return
	}
	var access *personalai.ObjectAccess
	if conversation.ObjectID != nil {
		object, err := a.objects.LoadForActor(r.Context(), principal.User.ID, *conversation.ObjectID)
		if err != nil {
			if errors.Is(err, objects.ErrNotFound) {
				a.auditNotFound(r, audit.UserActor(principal.User.ID), *conversation.ObjectID)
				httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
				return
			}
			a.writeDomainError(w, r, err)
			return
		}
		access = &personalai.ObjectAccess{
			ObjectID: object.ID, ObjectType: object.ObjectType, Version: object.Version,
			State: object.State, SharedContext: object.SharedContext,
			MemberPermissions: object.Permissions,
		}
	}
	key, ok := a.requireIdempotencyKey(w, r)
	if !ok {
		return
	}
	claim, ok := a.claimIdempotency(w, r, principal.User.ID, messageRoute, key, body, func() error {
		// A cached AI turn may only replay while the caller's current
		// membership and an active AI delegation still permit the read.
		profile, err := a.personalAI.GetProfile(r.Context(), principal.User.ID)
		if err != nil {
			return err
		}
		var objectID *uuid.UUID
		if access != nil {
			objectID = &access.ObjectID
		}
		if err := a.trust.ReplayAIAllowed(r.Context(), principal.User.ID, profile.ID, objectID, nil); err != nil {
			if access != nil {
				a.auditNotFound(r, audit.UserActor(principal.User.ID), access.ObjectID)
			}
			return err
		}
		return nil
	})
	if !ok {
		return
	}
	result, err := a.personalAI.SendMessage(r.Context(), personalai.SendMessageInput{
		UserID: principal.User.ID, ConversationID: conversationID, Content: input.Content,
		Intent: input.Intent,
		Object: access, Route: messageRoute, IdemKey: key, LeaseUntil: claim.LeaseUntil,
		RequestID: httpx.RequestIDFromContext(r.Context()),
		IPHash:    httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)),
	})
	if err != nil {
		_ = a.idem.Cancel(r.Context(), principal.User.ID, messageRoute, key, claim.LeaseUntil)
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (a *App) listDelegations(w http.ResponseWriter, r *http.Request) {
	if !a.requirePersonalAI(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	items, err := a.trust.ListDelegations(r.Context(), principal.User.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items []trust.Delegation `json:"items"`
	}{Items: items})
}

func (a *App) createDelegation(w http.ResponseWriter, r *http.Request) {
	if !a.requirePersonalAI(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	profile, err := a.personalAI.GetProfile(r.Context(), principal.User.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
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
	var input trust.CreateDelegationInput
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	if input.AIProfileID != nil && *input.AIProfileID != profile.ID {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	if input.ObjectID != nil {
		if _, err := a.objects.LoadForActor(r.Context(), principal.User.ID, *input.ObjectID); err != nil {
			if errors.Is(err, objects.ErrNotFound) {
				a.auditNotFound(r, audit.UserActor(principal.User.ID), *input.ObjectID)
				httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
				return
			}
			a.writeDomainError(w, r, err)
			return
		}
	}
	delegation, err := a.trust.CreateDelegation(r.Context(), principal.User.ID, profile.ID, input,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, delegation)
}

func (a *App) revokeDelegation(w http.ResponseWriter, r *http.Request) {
	if !a.requirePersonalAI(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	if a.rejectActorOverrideInBody(w, r) {
		return
	}

	delegationID, ok := parsePathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := a.trust.RevokeDelegation(r.Context(), principal.User.ID, delegationID,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders))); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parsePathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return uuid.Nil, false
	}
	return id, true
}
