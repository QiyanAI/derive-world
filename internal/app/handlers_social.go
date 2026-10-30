// SPDX-License-Identifier: Apache-2.0
package app

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/httpx"
	"github.com/QiyanAI/derive-world/internal/social"
)

func (a *App) socialServiceUnavailable(w http.ResponseWriter, r *http.Request) bool {
	if a.social != nil {
		return false
	}
	httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
	return true
}

func parseUUIDParam(r *http.Request, name string) (uuid.UUID, bool) {
	parsed, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		return uuid.Nil, false
	}
	return parsed, true
}

func (a *App) listFriends(w http.ResponseWriter, r *http.Request) {
	if a.socialServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	items, err := a.social.ListFriends(r.Context(), principal.User.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items []social.Friend `json:"items"`
	}{Items: items})
}

func (a *App) listFriendRequests(w http.ResponseWriter, r *http.Request) {
	if a.socialServiceUnavailable(w, r) {
		return
	}
	direction := r.URL.Query().Get("direction")
	if direction == "" {
		direction = "incoming"
	}
	if direction != "incoming" && direction != "outgoing" {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "direction is invalid"))
		return
	}
	principal := principalFrom(r.Context())
	items, err := a.social.ListRequests(r.Context(), principal.User.ID, direction)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items []social.FriendRequest `json:"items"`
	}{Items: items})
}

// searchUsers is query-scoped, authenticated account discovery. It is not a
// directory endpoint: an empty query is rejected and results are capped.
func (a *App) searchUsers(w http.ResponseWriter, r *http.Request) {
	if a.socialServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.allowUserLimit(w, r, principal.User.ID, a.userSearchLimiter, 60,
		"user_search_rate_limited", "too many account searches") {
		return
	}
	items, err := a.social.SearchUsers(r.Context(), principal.User.ID, r.URL.Query().Get("q"))
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items []social.UserSearchResult `json:"items"`
	}{Items: items})
}

func (a *App) getDiscoverability(w http.ResponseWriter, r *http.Request) {
	if a.identity == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
		return
	}
	principal := principalFrom(r.Context())
	discoverable, err := a.identity.Discoverability(r.Context(), principal.User.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Discoverable bool `json:"discoverable"`
	}{Discoverable: discoverable})
}

func (a *App) setDiscoverability(w http.ResponseWriter, r *http.Request) {
	if a.identity == nil {
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
		Discoverable *bool `json:"discoverable"`
	}
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	if input.Discoverable == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "discoverable is required"))
		return
	}
	if err := a.identity.SetDiscoverability(r.Context(), principal.User.ID,
		*input.Discoverable, httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders))); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Discoverable bool `json:"discoverable"`
	}{Discoverable: *input.Discoverable})
}

func (a *App) sendFriendRequest(w http.ResponseWriter, r *http.Request) {
	if a.socialServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	if !a.allowUserLimit(w, r, principal.User.ID, a.friendRequestLimiter, 20,
		"friend_request_rate_limited", "too many friend requests") {
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
		Handle string `json:"handle"`
	}
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	request, err := a.social.SendRequest(r.Context(), principal.User.ID, input.Handle,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	if target, err := uuid.Parse(request.UserID); err == nil {
		a.notifyUser(target)
	}
	httpx.WriteJSON(w, http.StatusCreated, request)
}

func (a *App) acceptFriendRequest(w http.ResponseWriter, r *http.Request) {
	a.respondFriendRequest(w, r, true)
}

func (a *App) declineFriendRequest(w http.ResponseWriter, r *http.Request) {
	a.respondFriendRequest(w, r, false)
}

func (a *App) respondFriendRequest(w http.ResponseWriter, r *http.Request, accept bool) {
	if a.socialServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	if a.rejectActorOverrideInBody(w, r) {
		return
	}
	requestID, ok := parseUUIDParam(r, "id")
	if !ok {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	request, err := a.social.RespondRequest(r.Context(), principal.User.ID, requestID, accept,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	if other, err := uuid.Parse(request.UserID); err == nil {
		a.notifyUser(other)
	}
	httpx.WriteJSON(w, http.StatusOK, request)
}

func (a *App) removeFriend(w http.ResponseWriter, r *http.Request) {
	if a.socialServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	if a.rejectActorOverrideInBody(w, r) {
		return
	}
	otherID, ok := parseUUIDParam(r, "user_id")
	if !ok {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	if err := a.social.RemoveFriend(r.Context(), principal.User.ID, otherID,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders))); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	a.notifyUser(otherID)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) listBlocks(w http.ResponseWriter, r *http.Request) {
	if a.socialServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	items, err := a.social.ListBlocks(r.Context(), principal.User.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items []social.Block `json:"items"`
	}{Items: items})
}

func (a *App) blockUser(w http.ResponseWriter, r *http.Request) {
	if a.socialServiceUnavailable(w, r) {
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
		BlockedUserID string `json:"blocked_user_id"`
	}
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	targetID, err := uuid.Parse(input.BlockedUserID)
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "blocked_user_id is invalid"))
		return
	}
	if err := a.social.Block(r.Context(), principal.User.ID, targetID,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders))); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	a.notifyUser(targetID)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) unblockUser(w http.ResponseWriter, r *http.Request) {
	if a.socialServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	if a.rejectActorOverrideInBody(w, r) {
		return
	}
	targetID, ok := parseUUIDParam(r, "user_id")
	if !ok {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	if err := a.social.Unblock(r.Context(), principal.User.ID, targetID,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders))); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	a.notifyUser(targetID)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) listMessages(w http.ResponseWriter, r *http.Request) {
	if a.socialServiceUnavailable(w, r) {
		return
	}
	withID, err := uuid.Parse(r.URL.Query().Get("with"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "with is invalid"))
		return
	}
	limit, err := httpx.ParseLimit(r.URL.Query().Get("limit"), 50, 100)
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
	principal := principalFrom(r.Context())
	if r.URL.Query().Get("latest") == "1" {
		items, err := a.social.ListRecentMessages(r.Context(), principal.User.ID, withID, limit)
		if err != nil {
			a.writeDomainError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, struct {
			Items      []social.Message `json:"items"`
			NextCursor *string          `json:"next_cursor"`
		}{Items: items, NextCursor: nil})
		return
	}
	items, next, err := a.social.ListMessages(r.Context(), principal.User.ID, withID, cursor, limit)
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
		Items      []social.Message `json:"items"`
		NextCursor *string          `json:"next_cursor"`
	}{Items: items, NextCursor: nextCursor})
}

func (a *App) sendDirectMessage(w http.ResponseWriter, r *http.Request) {
	if a.socialServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	if !a.allowUserLimit(w, r, principal.User.ID, a.socialWriteLimiter, 60,
		"social_rate_limited", "too many social writes") {
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
		ToUserID        string `json:"to_user_id"`
		Body            string `json:"body"`
		ClientMessageID string `json:"client_message_id"`
	}
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	recipientID, err := uuid.Parse(input.ToUserID)
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "to_user_id is invalid"))
		return
	}
	message, err := a.social.SendMessage(r.Context(), principal.User.ID, recipientID,
		input.Body, input.ClientMessageID, httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	a.notifyUser(recipientID)
	httpx.WriteJSON(w, http.StatusCreated, message)
}

func (a *App) markMessagesRead(w http.ResponseWriter, r *http.Request) {
	if a.socialServiceUnavailable(w, r) {
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
		WithUserID string `json:"with_user_id"`
	}
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	withID, err := uuid.Parse(input.WithUserID)
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "with_user_id is invalid"))
		return
	}
	marked, err := a.social.MarkRead(r.Context(), principal.User.ID, withID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Marked int64 `json:"marked"`
	}{Marked: marked})
}
