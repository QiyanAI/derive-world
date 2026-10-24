// SPDX-License-Identifier: Apache-2.0
package app

import (
	"net/http"
	"time"

	"github.com/QiyanAI/derive-world/internal/httpx"
	"github.com/QiyanAI/derive-world/internal/identity"
	"github.com/QiyanAI/derive-world/internal/personalai"
	"github.com/QiyanAI/derive-world/internal/trust"
)

type userResponse struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	Handle      string    `json:"handle"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

type profileResponse struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Persona string `json:"persona"`
}

type authResponse struct {
	User       userResponse    `json:"user"`
	PersonalAI profileResponse `json:"personal_ai"`
	AuthMethod string          `json:"auth_method"`
	CSRFToken  string          `json:"csrf_token,omitempty"`
	Scopes     []string        `json:"scopes,omitempty"`
}

func userView(user identity.User) userResponse {
	return userResponse{
		ID: user.ID.String(), Email: user.Email, Handle: user.Handle,
		DisplayName: user.DisplayName, CreatedAt: user.CreatedAt,
	}
}

func profileView(profile *personalai.Profile) profileResponse {
	if profile == nil {
		return profileResponse{}
	}
	return profileResponse{ID: profile.ID.String(), Name: profile.Name, Persona: profile.Persona}
}

func (a *App) register(w http.ResponseWriter, r *http.Request) {
	if a.identity == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
		return
	}
	if !a.allowAuthIP(w, r) || !a.checkOrigin(w, r) {
		return
	}
	body, err := a.readRequestBody(w, r)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	if field := detectActorOverride(r, body); field != "" {
		a.rejectActorOverride(w, r, nil, field)
		return
	}
	var input identity.RegisterInput
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	if !a.allowAuthAccount(w, r, input.Email) {
		return
	}
	prepared, err := a.identity.PrepareRegistration(input)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	requestID := httpx.RequestIDFromContext(r.Context())
	ipHash := httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders))
	if err := a.store.WithTx(r.Context(), func(tx storeTx) error {
		if err := a.identity.InsertUserTx(r.Context(), tx, prepared); err != nil {
			return err
		}
		if err := personalai.InsertRegistrationRowsTx(r.Context(), tx, prepared.UserID, prepared.ProfileID, prepared.DisplayName); err != nil {
			return err
		}
		if err := trust.InsertDefaultDelegationTx(r.Context(), tx, prepared.UserID, prepared.ProfileID, prepared.DelegationID); err != nil {
			return err
		}
		if err := a.identity.InsertSessionTx(r.Context(), tx, prepared); err != nil {
			return err
		}
		return a.identity.WriteRegisterAudit(r.Context(), tx, prepared, requestID, ipHash)
	}); err != nil {
		a.writeDomainError(w, r, err)
		return
	}

	a.setSessionCookies(w, identity.Session{
		ID: prepared.SessionID, Token: prepared.SessionToken,
		CSRFToken: prepared.CSRFToken, ExpiresAt: prepared.ExpiresAt,
	})
	httpx.WriteJSON(w, http.StatusCreated, authResponse{
		User: userView(identity.User{
			ID: prepared.UserID, Email: prepared.Email, Handle: prepared.Handle,
			DisplayName: prepared.DisplayName, Status: "active", CreatedAt: prepared.CreatedAt,
		}),
		PersonalAI: profileResponse{ID: prepared.ProfileID.String(), Name: prepared.DisplayName},
		AuthMethod: "session",
		CSRFToken:  prepared.CSRFToken,
	})
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if a.identity == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
		return
	}
	if !a.allowAuthIP(w, r) || !a.checkOrigin(w, r) {
		return
	}
	body, err := a.readRequestBody(w, r)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if field := detectActorOverride(r, body); field != "" {
		a.rejectActorOverride(w, r, nil, field)
		return
	}
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	if !a.allowAuthAccount(w, r, input.Email) {
		return
	}
	oldToken := ""
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		oldToken = cookie.Value
	}
	result, err := a.identity.Login(r.Context(), input.Email, input.Password, oldToken,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	profile, _ := personalai.GetProfile(r.Context(), a.store.Pool(), result.User.ID)
	a.setSessionCookies(w, result.Session)
	httpx.WriteJSON(w, http.StatusOK, authResponse{
		User:       userView(result.User),
		PersonalAI: profileView(profile),
		AuthMethod: "session",
		CSRFToken:  result.Session.CSRFToken,
	})
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r.Context())
	if principal == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeUnauthenticated, "authentication required"))
		return
	}
	if !a.checkCSRF(w, r, principal) {
		return
	}
	if a.rejectActorOverrideInBody(w, r) {
		return
	}

	err := a.identity.Logout(r.Context(), principal,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	a.clearSessionCookies(w)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) me(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r.Context())
	if principal == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeUnauthenticated, "authentication required"))
		return
	}
	profile, err := personalai.GetProfile(r.Context(), a.store.Pool(), principal.User.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	method := principal.AuthMethod
	if method == "" {
		method = "session"
	}
	httpx.WriteJSON(w, http.StatusOK, authResponse{
		User:       userView(principal.User),
		PersonalAI: profileView(profile),
		AuthMethod: method,
		Scopes:     principal.Scopes,
	})
}
