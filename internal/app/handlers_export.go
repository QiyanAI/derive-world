// SPDX-License-Identifier: Apache-2.0
package app

import (
	"net/http"
	"time"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/httpx"
	"github.com/QiyanAI/derive-world/internal/identity"
	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/personalai"
	"github.com/QiyanAI/derive-world/internal/trust"
)

type exportDocument struct {
	ExportedAt       time.Time                       `json:"exported_at"`
	User             userResponse                    `json:"user"`
	PersonalAI       *personalai.Profile             `json:"personal_ai"`
	PrivateContext   *personalai.PrivateContext      `json:"private_context"`
	Conversations    []personalai.ConversationDetail `json:"conversations"`
	Delegations      []trust.Delegation              `json:"delegations"`
	Tokens           []identity.TokenMetadata        `json:"tokens"`
	Objects          []objects.ListItem              `json:"objects"`
	ObjectsTruncated bool                            `json:"objects_truncated"`
}

func (a *App) exportMe(w http.ResponseWriter, r *http.Request) {
	if a.personalAI == nil || a.trust == nil || a.identity == nil || a.objects == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
		return
	}
	principal := principalFrom(r.Context())
	if !a.allowExport(w, r, principal.User.ID) {
		return
	}
	profile, err := a.personalAI.GetProfile(r.Context(), principal.User.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	privateContext, err := a.personalAI.GetContext(r.Context(), principal.User.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	conversations, err := a.personalAI.ExportConversations(r.Context(), principal.User.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	delegations, err := a.trust.ListDelegations(r.Context(), principal.User.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	tokens, err := a.identity.ListTokens(r.Context(), principal.User.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	accessible := make([]objects.ListItem, 0)
	var cursor *objects.ListCursor
	truncated := false
	for page := 0; page < 100; page++ {
		items, next, err := a.objects.List(r.Context(), principal.User.ID, "all", cursor, 100)
		if err != nil {
			a.writeDomainError(w, r, err)
			return
		}
		accessible = append(accessible, items...)
		if next == nil {
			cursor = nil
			break
		}
		cursor = next
		if page == 99 {
			truncated = true
		}
	}
	document := exportDocument{
		ExportedAt: time.Now().UTC(), User: userView(principal.User),
		PersonalAI: profile, PrivateContext: privateContext,
		Conversations: conversations, Delegations: delegations,
		Tokens: tokens, Objects: accessible, ObjectsTruncated: truncated,
	}
	if err := a.audit.WriteStandalone(r.Context(), audit.Event{
		Actor:        audit.UserActor(principal.User.ID),
		Action:       audit.ActionMeExport,
		ResourceType: "user",
		ResourceID:   &principal.User.ID,
		Decision:     "allow",
		RequestID:    httpx.RequestIDFromContext(r.Context()),
		IPHash:       httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)),
		Metadata:     map[string]any{"format": "json"},
	}); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, document)
}
