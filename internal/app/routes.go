// SPDX-License-Identifier: Apache-2.0
package app

import (
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/httpx"
	"github.com/QiyanAI/derive-world/internal/identity"
)

type storeTx = pgx.Tx

type businessRoute struct {
	method   string
	pattern  string
	auth     bool
	public   bool
	allowPAT bool
	patScope string
	handler  http.HandlerFunc
}

// registerBusinessRoutes attaches the G02 API surface. Every protected route
// runs authentication before the handler; handlers then perform the frozen
// LoadResource -> Authorize -> Validate -> Idempotency -> Version chain.
func (a *App) registerBusinessRoutes(mux *http.ServeMux) {
	routes := []businessRoute{
		{method: http.MethodPost, pattern: "/api/v1/auth/register", handler: a.register},
		{method: http.MethodPost, pattern: "/api/v1/auth/login", handler: a.login},
		{method: http.MethodPost, pattern: "/api/v1/auth/logout", auth: true, handler: a.logout},
		{method: http.MethodGet, pattern: "/api/v1/auth/me", auth: true, allowPAT: true, handler: a.me},
		{method: http.MethodGet, pattern: "/api/v1/me/export", auth: true, handler: a.exportMe},
		{method: http.MethodGet, pattern: "/api/v1/objects", auth: true, allowPAT: true, patScope: identity.ScopeObjectsRead, handler: a.listObjects},
		{method: http.MethodPost, pattern: "/api/v1/objects", auth: true, allowPAT: true, patScope: identity.ScopeObjectsWrite, handler: a.createObject},
		{method: http.MethodGet, pattern: "/api/v1/objects/{id}", auth: true, allowPAT: true, patScope: identity.ScopeObjectsRead, handler: a.getObject},
		{method: http.MethodDelete, pattern: "/api/v1/objects/{id}", auth: true, handler: a.deleteObject},
		{method: http.MethodGet, pattern: "/api/v1/objects/{id}/events", auth: true, allowPAT: true, patScope: identity.ScopeObjectsRead, handler: a.listEvents},
		{method: http.MethodPost, pattern: "/api/v1/objects/{id}/actions", auth: true, allowPAT: true, patScope: identity.ScopeObjectsWrite, handler: a.postAction},
		{method: http.MethodGet, pattern: "/api/v1/objects/{id}/members", auth: true, allowPAT: true, patScope: identity.ScopeObjectsRead, handler: a.listMembers},
		{method: http.MethodDelete, pattern: "/api/v1/objects/{id}/members/{user_id}", auth: true, handler: a.removeMember},
		{method: http.MethodPost, pattern: "/api/v1/objects/{id}/invitations", auth: true, handler: a.createInvitation},
		{method: http.MethodGet, pattern: "/api/v1/objects/{id}/invitations", auth: true, handler: a.listInvitations},
		{method: http.MethodDelete, pattern: "/api/v1/objects/{id}/invitations/{invitation_id}", auth: true, handler: a.revokeInvitation},
		{method: http.MethodGet, pattern: "/api/v1/invitations/{token}", auth: true, handler: a.previewInvitation},
		{method: http.MethodPost, pattern: "/api/v1/invitations/{token}/accept", auth: true, handler: a.acceptInvitation},
		{method: http.MethodPost, pattern: "/api/v1/objects/{id}/derive", auth: true, allowPAT: true, patScope: identity.ScopeDeriveCreate, handler: a.deriveObject},
		{method: http.MethodGet, pattern: "/api/v1/objects/{id}/lineage", auth: true, allowPAT: true, patScope: identity.ScopeObjectsRead, handler: a.getLineage},
		{method: http.MethodGet, pattern: "/api/v1/objects/{id}/handoff", auth: true, allowPAT: true, patScope: identity.ScopeObjectsRead, handler: a.getHandoff},
		{method: http.MethodGet, pattern: "/api/v1/objects/{id}/proposals", auth: true, handler: a.listProposals},
		{method: http.MethodPost, pattern: "/api/v1/objects/{id}/proposals/{proposal_id}/apply", auth: true, handler: a.applyProposal},
		{method: http.MethodPost, pattern: "/api/v1/objects/{id}/proposals/{proposal_id}/deny", auth: true, handler: a.denyProposal},
		{method: http.MethodGet, pattern: "/api/v1/ai/profile", auth: true, handler: a.getAIProfile},
		{method: http.MethodPatch, pattern: "/api/v1/ai/profile", auth: true, handler: a.patchAIProfile},
		{method: http.MethodGet, pattern: "/api/v1/ai/context", auth: true, handler: a.getAIContext},
		{method: http.MethodPut, pattern: "/api/v1/ai/context", auth: true, handler: a.putAIContext},
		{method: http.MethodGet, pattern: "/api/v1/ai/conversations", auth: true, handler: a.listConversations},
		{method: http.MethodPost, pattern: "/api/v1/ai/conversations", auth: true, handler: a.createConversation},
		{method: http.MethodGet, pattern: "/api/v1/ai/conversations/{id}", auth: true, handler: a.getConversation},
		{method: http.MethodDelete, pattern: "/api/v1/ai/conversations/{id}", auth: true, handler: a.deleteConversation},
		{method: http.MethodPost, pattern: "/api/v1/ai/conversations/{id}/messages", auth: true, handler: a.sendMessage},
		{method: http.MethodGet, pattern: "/api/v1/ai/delegations", auth: true, handler: a.listDelegations},
		{method: http.MethodPost, pattern: "/api/v1/ai/delegations", auth: true, handler: a.createDelegation},
		{method: http.MethodDelete, pattern: "/api/v1/ai/delegations/{id}", auth: true, handler: a.revokeDelegation},
		{method: http.MethodGet, pattern: "/api/v1/tokens", auth: true, handler: a.listTokens},
		{method: http.MethodPost, pattern: "/api/v1/tokens", auth: true, handler: a.createToken},
		{method: http.MethodDelete, pattern: "/api/v1/tokens/{id}", auth: true, handler: a.revokeToken},
		{method: http.MethodGet, pattern: "/api/v1/confirmations", auth: true, handler: a.listConfirmations},
		{method: http.MethodGet, pattern: "/api/v1/capabilities", auth: true, allowPAT: true, patScope: identity.ScopeCapabilities, handler: a.listCapabilities},
		{method: http.MethodGet, pattern: "/api/v1/capabilities/{capability_id}", auth: true, allowPAT: true, patScope: identity.ScopeCapabilities, handler: a.getCapability},
		{method: http.MethodPost, pattern: "/api/v1/capabilities/{capability_id}/invoke", auth: true, allowPAT: true, handler: a.invokeCapability},
		{method: http.MethodGet, pattern: "/api/v1/capability-invocations/{id}", auth: true, allowPAT: true, patScope: identity.ScopeCapabilities, handler: a.getCapabilityInvocation},
		{method: http.MethodGet, pattern: "/api/v1/confirmations/{id}", auth: true, handler: a.getConfirmation},
		{method: http.MethodPost, pattern: "/api/v1/confirmations/{id}/approve", auth: true, handler: a.approveConfirmation},
		{method: http.MethodPost, pattern: "/api/v1/confirmations/{id}/deny", auth: true, handler: a.denyConfirmation},

		// living-world World Stage, Living Pulse, Derive Lens, and diagnostics.
		{method: http.MethodGet, pattern: "/api/v1/world/stage", auth: true, handler: a.worldStage},
		{method: http.MethodGet, pattern: "/api/v1/world/layout", auth: true, handler: a.worldLayout},
		{method: http.MethodPut, pattern: "/api/v1/world/layout", auth: true, handler: a.saveWorldLayout},
		{method: http.MethodGet, pattern: "/api/v1/pulse", auth: true, handler: a.pulse},
		{method: http.MethodGet, pattern: "/api/v1/system/status", auth: true, handler: a.systemStatus},
		{method: http.MethodGet, pattern: "/api/v1/system/model-check", auth: true, handler: a.modelCheck},
		{method: http.MethodGet, pattern: "/api/v1/stream", auth: true, handler: a.streamEvents},
		{method: http.MethodGet, pattern: "/api/v1/objects/{id}/lens", auth: true, allowPAT: true, patScope: identity.ScopeObjectsRead, handler: a.getLens},
		{method: http.MethodGet, pattern: "/api/v1/objects/{id}/versions/{version}", auth: true, allowPAT: true, patScope: identity.ScopeObjectsRead, handler: a.getObjectVersion},
		{method: http.MethodGet, pattern: "/api/v1/objects/{id}/discussions", auth: true, allowPAT: true, patScope: identity.ScopeObjectsRead, handler: a.listDiscussions},
		{method: http.MethodPost, pattern: "/api/v1/objects/{id}/discussions", auth: true, handler: a.postDiscussion},

		// living-world Social: friendships, blocks, direct human messages.
		{method: http.MethodGet, pattern: "/api/v1/social/user-search", auth: true, handler: a.searchUsers},
		{method: http.MethodGet, pattern: "/api/v1/social/discoverability", auth: true, handler: a.getDiscoverability},
		{method: http.MethodPatch, pattern: "/api/v1/social/discoverability", auth: true, handler: a.setDiscoverability},
		{method: http.MethodGet, pattern: "/api/v1/social/friends", auth: true, handler: a.listFriends},
		{method: http.MethodGet, pattern: "/api/v1/social/friend-requests", auth: true, handler: a.listFriendRequests},
		{method: http.MethodPost, pattern: "/api/v1/social/friend-requests", auth: true, handler: a.sendFriendRequest},
		{method: http.MethodPost, pattern: "/api/v1/social/friend-requests/{id}/accept", auth: true, handler: a.acceptFriendRequest},
		{method: http.MethodPost, pattern: "/api/v1/social/friend-requests/{id}/decline", auth: true, handler: a.declineFriendRequest},
		{method: http.MethodDelete, pattern: "/api/v1/social/friends/{user_id}", auth: true, handler: a.removeFriend},
		{method: http.MethodGet, pattern: "/api/v1/social/messages", auth: true, handler: a.listMessages},
		{method: http.MethodPost, pattern: "/api/v1/social/messages", auth: true, handler: a.sendDirectMessage},
		{method: http.MethodPost, pattern: "/api/v1/social/messages/read", auth: true, handler: a.markMessagesRead},
		{method: http.MethodGet, pattern: "/api/v1/social/blocks", auth: true, handler: a.listBlocks},
		{method: http.MethodPost, pattern: "/api/v1/social/blocks", auth: true, handler: a.blockUser},
		{method: http.MethodDelete, pattern: "/api/v1/social/blocks/{user_id}", auth: true, handler: a.unblockUser},

		// living-world Publication: owner publication, consent, anonymous discovery.
		{method: http.MethodGet, pattern: "/api/v1/objects/{id}/publications", auth: true, allowPAT: true, patScope: identity.ScopeObjectsRead, handler: a.listPublications},
		{method: http.MethodPost, pattern: "/api/v1/objects/{id}/publications", auth: true, handler: a.createPublication},
		{method: http.MethodDelete, pattern: "/api/v1/publications/{id}", auth: true, handler: a.revokePublication},
		{method: http.MethodGet, pattern: "/api/v1/publication-consents", auth: true, handler: a.listPublicationConsents},
		{method: http.MethodPost, pattern: "/api/v1/publication-consents/{id}/approve", auth: true, handler: a.approvePublicationConsent},
		{method: http.MethodPost, pattern: "/api/v1/publication-consents/{id}/decline", auth: true, handler: a.declinePublicationConsent},
		{method: http.MethodGet, pattern: "/api/v1/public/worlds", public: true, handler: a.publicWorlds},
		{method: http.MethodGet, pattern: "/api/v1/public/worlds/{id}", public: true, handler: a.publicWorld},
		{method: http.MethodGet, pattern: "/api/v1/public/worlds/{id}/lineage", public: true, handler: a.publicWorldLineage},
		{method: http.MethodPost, pattern: "/api/v1/public/worlds/{id}/derive", auth: true, handler: a.publicDerive},
		{method: http.MethodPost, pattern: "/api/v1/public/worlds/{id}/report", auth: true, handler: a.reportPublicWorld},
	}
	methodsByPath := map[string][]string{}
	for _, route := range routes {
		handler := route.handler
		if route.auth {
			handler = a.withAuthPolicy(route)
		} else if route.public {
			handler = a.withPublicPolicy(handler)
		}
		mux.Handle(route.method+" "+route.pattern, httpx.TagRoute(route.pattern, handler))
		a.routes = append(a.routes, RouteSpec{Method: route.method, Pattern: route.pattern})
		methodsByPath[route.pattern] = append(methodsByPath[route.pattern], route.method)
	}
	// Method-only fallbacks keep 405 responses inside the JSON error envelope.
	for pattern, methods := range methodsByPath {
		allowed := strings.Join(methods, ", ")
		mux.Handle(pattern, httpx.TagRoute(pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", allowed)
			if recorder, ok := w.(interface{ SetErrorCode(string) }); ok {
				recorder.SetErrorCode(string(httpx.CodeInvalidRequest))
			}
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]any{
				"error": map[string]any{
					"code":       httpx.CodeInvalidRequest,
					"message":    "method not allowed",
					"request_id": httpx.RequestIDFromContext(r.Context()),
					"details":    map[string]any{},
				},
			})
		})))
	}
}
