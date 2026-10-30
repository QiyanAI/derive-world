// SPDX-License-Identifier: Apache-2.0
package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/capability"
	"github.com/QiyanAI/derive-world/internal/handoff"
	"github.com/QiyanAI/derive-world/internal/httpx"
	"github.com/QiyanAI/derive-world/internal/identity"
	"github.com/QiyanAI/derive-world/internal/model"
	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/personalai"
	"github.com/QiyanAI/derive-world/internal/publication"
	"github.com/QiyanAI/derive-world/internal/social"
	"github.com/QiyanAI/derive-world/internal/store"
	"github.com/QiyanAI/derive-world/internal/trust"
)

const (
	sessionCookieName = "dw_session"
	csrfCookieName    = "dw_csrf"
	sessionCookieAge  = 7 * 24 * 60 * 60
)

type principalContextKey struct{}

func withPrincipal(ctx context.Context, principal *identity.Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func principalFrom(ctx context.Context) *identity.Principal {
	value, _ := ctx.Value(principalContextKey{}).(*identity.Principal)
	return value
}

// withAuthPolicy authenticates a browser session or an allowed PAT and places
// the server-derived actor in the request context. It is the only source of
// identity.
func (a *App) withAuthPolicy(route businessRoute) http.HandlerFunc {
	next := route.handler
	return func(w http.ResponseWriter, r *http.Request) {
		if a.identity == nil {
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
			return
		}
		bearer, hasBearer := bearerToken(r)
		if hasBearer {
			if !route.allowPAT {
				if !a.allowAnonymousAPI(w, r) {
					return
				}
				w.Header().Set("WWW-Authenticate", "Bearer")
				httpx.WriteError(w, r, httpx.NewError(httpx.CodeUnauthenticated, "bearer authentication is not allowed for this endpoint"))
				return
			}
			principal, err := a.identity.AuthenticatePAT(r.Context(), bearer)
			if err != nil {
				if errors.Is(err, identity.ErrUnauthenticated) {
					// Invalid bearer credentials are unauthenticated traffic and
					// must consume the IP bucket like an absent session.
					if !a.allowAnonymousAPI(w, r) {
						return
					}
					w.Header().Set("WWW-Authenticate", "Bearer")
					httpx.WriteError(w, r, httpx.NewError(httpx.CodeUnauthenticated, "authentication required"))
					return
				}
				a.logger.Error("authentication_internal_error",
					"request_id", httpx.RequestIDFromContext(r.Context()),
					"error_type", fmt.Sprintf("%T", err))
				httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "internal server error"))
				return
			}
			ctx := withPrincipal(r.Context(), principal)
			actor := httpx.Actor{UserID: principal.User.ID.String(), Type: "user"}
			httpx.SetActor(w, actor)
			ctx = httpx.WithActor(ctx, actor)
			if !a.allowAPI(w, r, principal) {
				return
			}
			if route.patScope != "" && !identity.HasScope(principal, route.patScope) {
				a.auditDeny(r, audit.UserActor(principal.User.ID), audit.ActionAuthzDenied,
					"route", nil, "missing_pat_scope",
					map[string]any{"required_scope": route.patScope})
				httpx.WriteError(w, r, httpx.NewError(httpx.CodeForbidden, "forbidden"))
				return
			}
			if field := detectActorOverride(r, nil); field != "" {
				a.rejectActorOverride(w, r, &principal.User.ID, field)
				return
			}
			next(w, r.WithContext(ctx))
			return
		}

		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			if !a.allowAnonymousAPI(w, r) {
				return
			}
			a.clearSessionCookies(w)
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeUnauthenticated, "authentication required"))
			return
		}
		principal, err := a.identity.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, identity.ErrUnauthenticated) {
				if !a.allowAnonymousAPI(w, r) {
					return
				}
				a.clearSessionCookies(w)
				httpx.WriteError(w, r, httpx.NewError(httpx.CodeUnauthenticated, "authentication required"))
				return
			}
			// A database or transport failure is not proof that the session is
			// invalid. Keep cookies and return an explicit internal error.
			a.logger.Error("authentication_internal_error",
				"request_id", httpx.RequestIDFromContext(r.Context()),
				"error_type", fmt.Sprintf("%T", err))
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "internal server error"))
			return
		}
		ctx := withPrincipal(r.Context(), principal)
		actor := httpx.Actor{UserID: principal.User.ID.String(), Type: "user"}
		httpx.SetActor(w, actor)
		ctx = httpx.WithActor(ctx, actor)
		if !a.allowAPI(w, r, principal) {
			return
		}
		if field := detectActorOverride(r, nil); field != "" {
			a.rejectActorOverride(w, r, &principal.User.ID, field)
			return
		}
		next(w, r.WithContext(ctx))
	}
}

// withPublicPolicy guards anonymous read endpoints. It applies the anonymous
// IP budget and the authoritative-field ban but never creates a principal.
func (a *App) withPublicPolicy(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.allowAnonymousAPI(w, r) {
			return
		}
		if field := detectActorOverride(r, nil); field != "" {
			a.rejectActorOverride(w, r, nil, field)
			return
		}
		next(w, r)
	}
}

func bearerToken(r *http.Request) (string, bool) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return "", false
	}
	scheme, value, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}

func (a *App) allowAPI(w http.ResponseWriter, r *http.Request, principal *identity.Principal) bool {
	remaining, reset, ok := a.apiLimiter.Allow(principal.User.ID.String())
	a.setRateLimitHeaders(w, a.rateAPILimit, remaining, reset)
	if ok {
		return true
	}
	a.auditDeny(r, audit.UserActor(principal.User.ID), audit.ActionRateLimitHit, "route", nil, "rate_limited", nil)
	httpx.WriteError(w, r, httpx.NewError(httpx.CodeRateLimited, "too many requests"))
	return false
}

// allowAI applies the frozen 20/hour, burst 5 Personal AI bucket. It is
// checked after authentication and before the model call.
func (a *App) allowAI(w http.ResponseWriter, r *http.Request, userID uuid.UUID) bool {
	remaining, reset, ok := a.aiLimiter.Allow(userID.String())
	a.setRateLimitHeaders(w, a.rateAILimit, remaining, reset)
	if ok {
		return true
	}
	a.auditDeny(r, audit.UserActor(userID), audit.ActionRateLimitHit, "route", nil, "ai_rate_limited", nil)
	httpx.WriteError(w, r, httpx.NewError(httpx.CodeRateLimited, "too many AI requests"))
	return false
}

// allowExport applies the 1/hour personal export bucket.
func (a *App) allowExport(w http.ResponseWriter, r *http.Request, userID uuid.UUID) bool {
	remaining, reset, ok := a.exportLimiter.Allow(userID.String())
	a.setRateLimitHeaders(w, 1, remaining, reset)
	if ok {
		return true
	}
	a.auditDeny(r, audit.UserActor(userID), audit.ActionRateLimitHit, "route", nil, "export_rate_limited", nil)
	httpx.WriteError(w, r, httpx.NewError(httpx.CodeRateLimited, "export limit reached"))
	return false
}

// allowUserLimit applies one named per-user bucket and writes the standard
// rate-limit headers. It is used for living-world social and publication abuse
// controls in addition to the global API bucket.
func (a *App) allowUserLimit(w http.ResponseWriter, r *http.Request, userID uuid.UUID,
	limiter *httpx.Limiter, limit int, reason, message string) bool {
	if limiter == nil {
		return true
	}
	remaining, reset, ok := limiter.Allow(userID.String())
	a.setRateLimitHeaders(w, limit, remaining, reset)
	if ok {
		return true
	}
	a.auditDeny(r, audit.UserActor(userID), audit.ActionRateLimitHit, "route", nil, reason, nil)
	httpx.WriteError(w, r, httpx.NewError(httpx.CodeRateLimited, message))
	return false
}

func (a *App) allowAnonymousAPI(w http.ResponseWriter, r *http.Request) bool {
	ip := httpx.ClientIP(r, a.config.TrustProxyHeaders)
	remaining, reset, ok := a.anonAPILimiter.Allow("ip:" + ip)
	a.setRateLimitHeaders(w, a.rateAPILimit, remaining, reset)
	if ok {
		return true
	}
	a.auditDeny(r, audit.SystemActor(), audit.ActionRateLimitHit, "route", nil, "rate_limited", nil)
	httpx.WriteError(w, r, httpx.NewError(httpx.CodeRateLimited, "too many requests"))
	return false
}

func (a *App) allowAuthIP(w http.ResponseWriter, r *http.Request) bool {
	ip := httpx.ClientIP(r, a.config.TrustProxyHeaders)
	remaining, reset, ok := a.authIPLimiter.Allow(ip)
	a.setRateLimitHeaders(w, a.rateAuthLimit, remaining, reset)
	if ok {
		return true
	}
	a.auditDeny(r, audit.SystemActor(), audit.ActionRateLimitHit, "route", nil, "rate_limited", nil)
	httpx.WriteError(w, r, httpx.NewError(httpx.CodeRateLimited, "too many authentication attempts"))
	return false
}

func (a *App) allowAuthAccount(w http.ResponseWriter, r *http.Request, account string) bool {
	normalized := strings.ToLower(strings.TrimSpace(account))
	sum := sha256.Sum256([]byte(normalized))
	remaining, reset, ok := a.authAccountLimiter.Allow(hex.EncodeToString(sum[:]))
	a.setRateLimitHeaders(w, a.rateAuthAccountLimit, remaining, reset)
	if ok {
		return true
	}
	a.auditDeny(r, audit.SystemActor(), audit.ActionRateLimitHit, "route", nil, "rate_limited", nil)
	httpx.WriteError(w, r, httpx.NewError(httpx.CodeRateLimited, "too many authentication attempts"))
	return false
}

func (a *App) setRateLimitHeaders(w http.ResponseWriter, limit, remaining int, reset time.Time) {
	if limit < 1 {
		return
	}
	if remaining < 0 {
		remaining = 0
	}
	w.Header().Set("X-RateLimit-Limit", itoa(limit))
	w.Header().Set("X-RateLimit-Remaining", itoa(remaining))
	w.Header().Set("X-RateLimit-Reset", reset.UTC().Format(time.RFC3339))
}

func (a *App) sessionCookieSecure() bool {
	return strings.HasPrefix(strings.ToLower(a.config.PublicBaseURL), "https://")
}

func (a *App) setSessionCookies(w http.ResponseWriter, session identity.Session) {
	maxAge := int(time.Until(session.ExpiresAt).Seconds())
	if maxAge <= 0 {
		maxAge = sessionCookieAge
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    session.Token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   a.sessionCookieSecure(),
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    session.CSRFToken,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: false,
		Secure:   a.sessionCookieSecure(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *App) clearSessionCookies(w http.ResponseWriter) {
	epoch := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1, Expires: epoch,
		HttpOnly: true, Secure: a.sessionCookieSecure(), SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookieName, Value: "", Path: "/", MaxAge: -1, Expires: epoch,
		HttpOnly: false, Secure: a.sessionCookieSecure(), SameSite: http.SameSiteLaxMode,
	})
}

func (a *App) expectedOrigin() string {
	origin, ok := normalizedOrigin(a.config.PublicBaseURL)
	if !ok {
		return ""
	}
	return origin
}

// normalizedOrigin serializes an origin the way browsers do: lowercase scheme
// and host, no default port, and brackets around IPv6 literals.
func normalizedOrigin(raw string) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	switch {
	case port != "":
		host = net.JoinHostPort(host, port)
	case strings.Contains(host, ":"):
		host = "[" + host + "]"
	}
	return scheme + "://" + host, true
}

// checkOrigin validates login/register same-origin expectations. It writes the
// denial response and returns false on failure.
func (a *App) checkOrigin(w http.ResponseWriter, r *http.Request) bool {
	expected := a.expectedOrigin()
	if expected == "" {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "public base URL is not configured"))
		return false
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
		a.auditOriginRejected(r)
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeForbidden, "cross-site request rejected"))
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		normalized, ok := normalizedOrigin(origin)
		if !ok || normalized != expected {
			a.auditOriginRejected(r)
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeForbidden, "origin is not allowed"))
			return false
		}
		return true
	}
	if referer := r.Header.Get("Referer"); referer != "" {
		normalized, ok := normalizedOrigin(referer)
		if !ok || normalized != expected {
			a.auditOriginRejected(r)
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeForbidden, "origin is not allowed"))
			return false
		}
	}
	return true
}

// checkCSRF enforces the header hash plus Origin/Referer rule for every unsafe
// Cookie-authenticated request (D05-06).
func (a *App) checkCSRF(w http.ResponseWriter, r *http.Request, principal *identity.Principal) bool {
	expected := a.expectedOrigin()
	if principal.AuthMethod == "pat" {
		// Bearer tokens are not automatically attached by browsers, so no CSRF
		// token is required. A supplied Origin/Referer must still be same-origin.
		originOK := true
		if origin := r.Header.Get("Origin"); origin != "" {
			normalized, ok := normalizedOrigin(origin)
			originOK = ok && normalized == expected
		} else if referer := r.Header.Get("Referer"); referer != "" {
			normalized, ok := normalizedOrigin(referer)
			originOK = ok && normalized == expected
		}
		if originOK {
			return true
		}
		a.auditDeny(r, audit.UserActor(principal.User.ID), audit.ActionAuthCSRFFailed,
			"route", nil, "origin_failed", nil)
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeForbidden, "origin is not allowed"))
		return false
	}
	header := r.Header.Get("X-CSRF-Token")
	csrfOK := identity.ValidateCSRF(principal, header)
	originOK := false
	if origin := r.Header.Get("Origin"); origin != "" {
		normalized, ok := normalizedOrigin(origin)
		originOK = ok && normalized == expected
	} else if referer := r.Header.Get("Referer"); referer != "" {
		normalized, ok := normalizedOrigin(referer)
		originOK = ok && normalized == expected
	}
	if csrfOK && originOK {
		return true
	}
	a.auditDeny(r, audit.UserActor(principal.User.ID), audit.ActionAuthCSRFFailed,
		"route", nil, "csrf_or_origin_failed", nil)
	httpx.WriteError(w, r, httpx.NewError(httpx.CodeForbidden, "CSRF validation failed"))
	return false
}

func (a *App) auditOriginRejected(r *http.Request) {
	a.auditDeny(r, audit.SystemActor(), audit.ActionAuthOriginRejected,
		"route", nil, "origin_rejected", nil)
}

// auditDeny writes a denial row in an independent transaction. Rejection paths
// have no business transaction to join (D04 §3.10.5).
func (a *App) auditDeny(r *http.Request, actor audit.Actor, action audit.Action, resourceType string, resourceID *uuid.UUID, reason string, metadata map[string]any) {
	if a.audit == nil {
		return
	}
	requestID := httpx.RequestIDFromContext(r.Context())
	if len(requestID) < 8 {
		requestID = "unknown-request"
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["route"] = httpx.RouteFromContext(r.Context())
	event := audit.Event{
		Actor:        actor,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Decision:     "deny",
		ReasonCode:   reason,
		RequestID:    requestID,
		IPHash:       httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)),
		Metadata:     metadata,
	}
	_ = a.audit.WriteStandalone(r.Context(), event)
}

func (a *App) auditNotFound(r *http.Request, actor audit.Actor, objectID uuid.UUID) {
	digest := sha256.Sum256([]byte(objectID.String()))
	a.auditDeny(r, actor, audit.ActionAuthzNotFound, "living_object", nil,
		"object_not_visible", map[string]any{"resource_hash": hex.EncodeToString(digest[:8])})
}

// forbiddenFields are authoritative fields that may never come from a client.
var forbiddenFields = map[string]bool{
	"user_id": true, "actor_id": true, "actor_user_id": true, "actor_type": true,
	"delegation_id": true, "owner_id": true, "creator_id": true,
	"permissions": true, "version": true,
}

// detectActorOverride returns the first forbidden field found in the query,
// headers, or JSON body. It is intentionally conservative: nested JSON keys are
// also rejected.
func detectActorOverride(r *http.Request, body []byte) string {
	return detectActorOverrideExcept(r, body, nil)
}

// detectActorOverrideExcept permits explicitly documented top-level fields
// (currently only invitation/delegation permissions). Nested occurrences are
// never permitted.
func detectActorOverrideExcept(r *http.Request, body []byte, allowedTopLevel map[string]bool) string {
	for key := range r.URL.Query() {
		if forbiddenFields[strings.ToLower(key)] {
			return strings.ToLower(key)
		}
	}
	for name := range r.Header {
		normalized := strings.ToLower(strings.ReplaceAll(name, "-", "_"))
		normalized = strings.TrimPrefix(normalized, "x_")
		if forbiddenFields[normalized] {
			return "header:" + normalized
		}
	}
	if len(bytes.TrimSpace(body)) > 0 {
		var value any
		if err := json.Unmarshal(body, &value); err == nil {
			if field := findForbiddenKey(value, allowedTopLevel, true); field != "" {
				return field
			}
		}
	}
	return ""
}

func findForbiddenKey(value any, allowedTopLevel map[string]bool, topLevel bool) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			lower := strings.ToLower(key)
			if forbiddenFields[lower] && !(topLevel && allowedTopLevel[lower]) {
				return key
			}
			if field := findForbiddenKey(nested, allowedTopLevel, false); field != "" {
				return field
			}
		}
	case []any:
		for _, nested := range typed {
			if field := findForbiddenKey(nested, allowedTopLevel, false); field != "" {
				return field
			}
		}
	}
	return ""
}

// readRequestBody captures the raw body for actor-override detection and
// idempotency hashing, then restores it for the JSON decoder.
func (a *App) readRequestBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	limit := a.config.MaxRequestBytes
	if limit <= 0 {
		limit = 1 << 20
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, httpx.NewError(httpx.CodePayloadTooLarge, "request body exceeds the limit")
		}
		return nil, httpx.NewError(httpx.CodeInvalidRequest, "request body could not be read")
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// rejectActorOverrideInBody reads and restores an otherwise unused request body
// solely to enforce D05-01's authoritative-field ban on bodyless endpoints.
func (a *App) rejectActorOverrideInBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil {
		return false
	}
	body, err := a.readRequestBody(w, r)
	if err != nil {
		a.writeDomainError(w, r, err)
		return true
	}
	field := detectActorOverride(r, body)
	if field == "" {
		return false
	}
	var actor *uuid.UUID
	if principal := principalFrom(r.Context()); principal != nil {
		actor = &principal.User.ID
	}
	a.rejectActorOverride(w, r, actor, field)
	return true
}

func (a *App) rejectActorOverride(w http.ResponseWriter, r *http.Request, actor *uuid.UUID, field string) {
	if actor != nil {
		a.auditDeny(r, audit.UserActor(*actor), audit.ActionAuthzActorOverride,
			"route", nil, "actor_override_attempt", map[string]any{"field": field})
	} else {
		a.auditDeny(r, audit.SystemActor(), audit.ActionAuthzActorOverride,
			"route", nil, "actor_override_attempt", map[string]any{"field": field})
	}
	httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "authoritative fields are server-derived"))
}

// writeDomainError maps domain errors to the closed HTTP error set. Unknown
// failures are logged by request id and never expose their detail.
func (a *App) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	var capFailure *capability.Failure
	if errors.As(err, &capFailure) {
		httpx.WriteError(w, r, capFailure.AsAPIError())
		return
	}
	var apiErr *httpx.APIError
	if !errors.As(err, &apiErr) &&
		!errors.Is(err, objects.ErrNotFound) &&
		!errors.Is(err, objects.ErrForbidden) &&
		!errors.Is(err, objects.ErrDelegationNotActive) &&
		!errors.Is(err, objects.ErrVersionConflict) &&
		!errors.Is(err, objects.ErrConflict) &&
		!errors.Is(err, objects.ErrPayloadTooLarge) &&
		!errors.Is(err, objects.ErrInvalidRequest) &&
		!errors.Is(err, objects.ErrSnapshotMissing) &&
		!errors.Is(err, objects.ErrInvitationInvalid) &&
		!errors.Is(err, objects.ErrCannotRemoveOwner) &&
		!errors.Is(err, objects.ErrOwnerCannotLeave) &&
		!errors.Is(err, store.ErrIdempotencyConflict) &&
		!errors.Is(err, store.ErrRequestInProgress) &&
		!errors.Is(err, identity.ErrInvalidCredentials) &&
		!errors.Is(err, identity.ErrConflict) &&
		!errors.Is(err, identity.ErrInvalidRequest) &&
		!errors.Is(err, identity.ErrNotFound) &&
		!errors.Is(err, trust.ErrNotFound) &&
		!errors.Is(err, trust.ErrForbidden) &&
		!errors.Is(err, trust.ErrInvalidRequest) &&
		!errors.Is(err, trust.ErrConflict) &&
		!errors.Is(err, trust.ErrVersionConflict) &&
		!errors.Is(err, trust.ErrConfirmationExpired) &&
		!errors.Is(err, trust.ErrNoActiveDelegation) &&
		!errors.Is(err, trust.ErrPayloadTooLarge) &&
		!errors.Is(err, trust.ErrDelegationNotActive) &&
		!errors.Is(err, trust.ErrUnsupportedAction) &&
		!errors.Is(err, trust.ErrProfileNotOwned) &&
		!errors.Is(err, personalai.ErrNotFound) &&
		!errors.Is(err, personalai.ErrInvalidRequest) &&
		!errors.Is(err, personalai.ErrPayloadTooLarge) &&
		!errors.Is(err, personalai.ErrForbidden) &&
		!errors.Is(err, social.ErrNotFound) &&
		!errors.Is(err, social.ErrForbidden) &&
		!errors.Is(err, social.ErrBlocked) &&
		!errors.Is(err, social.ErrConflict) &&
		!errors.Is(err, social.ErrInvalidRequest) &&
		!errors.Is(err, publication.ErrNotFound) &&
		!errors.Is(err, publication.ErrForbidden) &&
		!errors.Is(err, publication.ErrConflict) &&
		!errors.Is(err, publication.ErrInvalidRequest) &&
		!errors.Is(err, model.ErrBudgetExceeded) &&
		!errors.Is(err, model.ErrGlobalBudgetExceeded) &&
		!errors.Is(err, model.ErrInputTooLarge) &&
		!errors.Is(err, model.ErrModelDisabled) &&
		!errors.Is(err, model.ErrConcurrencyLimit) &&
		!errors.Is(err, handoff.ErrPayloadTooLarge) &&
		!errors.Is(err, capability.ErrNotFound) &&
		!errors.Is(err, capability.ErrForbidden) &&
		!errors.Is(err, capability.ErrDenied) &&
		!errors.Is(err, capability.ErrInvalidRequest) &&
		!errors.Is(err, capability.ErrConflict) &&
		!errors.Is(err, capability.ErrPayloadTooLarge) &&
		modelErrorCode(err) == "" {
		fields := []any{
			"request_id", httpx.RequestIDFromContext(r.Context()),
			"error_type", fmt.Sprintf("%T", err),
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			fields = append(fields, "sqlstate", pgErr.Code)
			if pgErr.ConstraintName != "" {
				fields = append(fields, "constraint", pgErr.ConstraintName)
			}
		}
		a.logger.Error("internal_error", fields...)
	}
	writeDomainError(w, r, err)
}

func writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, objects.ErrNotFound):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
	case errors.Is(err, objects.ErrForbidden), errors.Is(err, objects.ErrDelegationNotActive):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeForbidden, "forbidden"))
	case errors.Is(err, objects.ErrVersionConflict):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeVersionConflict, "version conflict"))
	case errors.Is(err, objects.ErrConflict):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeConflict, "conflict"))
	case errors.Is(err, objects.ErrPayloadTooLarge):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodePayloadTooLarge, "payload too large"))
	case errors.Is(err, objects.ErrInvalidRequest):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "invalid request").WithDetails(validationFieldDetails(err)))
	case errors.Is(err, objects.ErrSnapshotMissing):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "internal server error"))
	case errors.Is(err, store.ErrIdempotencyConflict):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeIdempotencyConflict, "idempotency key was used for a different request"))
	case errors.Is(err, store.ErrRequestInProgress):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeRequestInProgress, "an identical request is already in progress"))
	case errors.Is(err, identity.ErrInvalidCredentials):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeUnauthenticated, "Invalid credentials"))
	case errors.Is(err, identity.ErrUnauthenticated):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeUnauthenticated, "authentication required"))
	case errors.Is(err, identity.ErrConflict):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeConflict, "conflict"))
	case errors.Is(err, identity.ErrInvalidRequest):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "invalid request").WithDetails(validationFieldDetails(err)))
	case errors.Is(err, identity.ErrNotFound), errors.Is(err, trust.ErrNotFound),
		errors.Is(err, trust.ErrProfileNotOwned), errors.Is(err, trust.ErrConfirmationNotOwned),
		errors.Is(err, personalai.ErrNotFound), errors.Is(err, social.ErrNotFound),
		errors.Is(err, publication.ErrNotFound):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
	case errors.Is(err, objects.ErrInvitationInvalid):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvitationInvalid, "invitation is not valid"))
	case errors.Is(err, objects.ErrCannotRemoveOwner):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeConflict, "conflict").
			WithDetails(map[string]any{"reason": "cannot_remove_owner"}))
	case errors.Is(err, objects.ErrOwnerCannotLeave):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeConflict, "conflict").
			WithDetails(map[string]any{"reason": "owner_cannot_leave"}))
	case errors.Is(err, trust.ErrForbidden), errors.Is(err, trust.ErrNoActiveDelegation),
		errors.Is(err, trust.ErrDelegationNotActive), errors.Is(err, personalai.ErrForbidden),
		errors.Is(err, social.ErrForbidden), errors.Is(err, social.ErrBlocked),
		errors.Is(err, publication.ErrForbidden):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeForbidden, "forbidden"))
	case errors.Is(err, trust.ErrInvalidRequest), errors.Is(err, personalai.ErrInvalidRequest),
		errors.Is(err, trust.ErrUnsupportedAction), errors.Is(err, social.ErrInvalidRequest),
		errors.Is(err, publication.ErrInvalidRequest):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "invalid request").WithDetails(validationFieldDetails(err)))
	case errors.Is(err, trust.ErrVersionConflict):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeVersionConflict, "version conflict"))
	case errors.Is(err, trust.ErrConflict), errors.Is(err, social.ErrConflict),
		errors.Is(err, publication.ErrConflict):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeConflict, "conflict"))
	case errors.Is(err, trust.ErrConfirmationExpired):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeConfirmationExpired, "confirmation expired"))
	case errors.Is(err, trust.ErrPayloadTooLarge), errors.Is(err, personalai.ErrPayloadTooLarge),
		errors.Is(err, handoff.ErrPayloadTooLarge):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodePayloadTooLarge, "payload too large"))
	case errors.Is(err, model.ErrBudgetExceeded), errors.Is(err, model.ErrGlobalBudgetExceeded):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeBudgetExceeded, "daily model budget exceeded"))
	case errors.Is(err, model.ErrInputTooLarge):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodePayloadTooLarge, "payload too large"))
	case errors.Is(err, model.ErrModelDisabled):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeModelUnavailable, "model calls are disabled"))
	case errors.Is(err, model.ErrConcurrencyLimit):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeRateLimited, "model concurrency limit reached"))
	case errors.Is(err, capability.ErrNotFound):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
	case errors.Is(err, capability.ErrForbidden), errors.Is(err, capability.ErrDenied):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeForbidden, "forbidden"))
	case errors.Is(err, capability.ErrInvalidRequest):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "invalid request"))
	case errors.Is(err, capability.ErrPayloadTooLarge):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodePayloadTooLarge, "payload too large"))
	case errors.Is(err, capability.ErrConflict):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeConflict, "conflict"))
	case modelErrorCode(err) != "":
		httpx.WriteError(w, r, httpx.NewError(modelErrorCode(err), "model call failed"))
	default:
		var apiErr *httpx.APIError
		if errors.As(err, &apiErr) {
			httpx.WriteError(w, r, apiErr)
			return
		}
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "internal server error"))
	}
}

func modelErrorCode(err error) httpx.ErrorCode {
	var modelErr *model.Error
	if !errors.As(err, &modelErr) {
		return ""
	}
	switch modelErr.Code {
	case model.CodeUnavailable:
		return httpx.CodeModelUnavailable
	case model.CodeTimeout:
		return httpx.CodeModelTimeout
	case model.CodeInvalidOutput, model.CodeEmptyOutput:
		return httpx.CodeModelInvalidOutput
	case model.CodeRateLimited:
		return httpx.CodeRateLimited
	case model.CodePayloadTooLarge:
		return httpx.CodePayloadTooLarge
	case model.CodeInvalidRequest:
		return httpx.CodeInvalidRequest
	default:
		return httpx.CodeInternal
	}
}

// validationFieldDetails exposes the JSON Pointer carried by validation
// errors, as required by D04 section 3.6.1 item 5.
func validationFieldDetails(err error) map[string]any {
	text := err.Error()
	for _, base := range []string{
		objects.ErrInvalidRequest.Error(), identity.ErrInvalidRequest.Error(),
		trust.ErrInvalidRequest.Error(), personalai.ErrInvalidRequest.Error(),
	} {
		prefix := base + ": "
		if !strings.HasPrefix(text, prefix) {
			continue
		}
		field := strings.TrimSpace(strings.TrimPrefix(text, prefix))
		if field == "" {
			return nil
		}
		if !strings.HasPrefix(field, "/") {
			field = "/" + strings.ReplaceAll(field, ".", "/")
		}
		return map[string]any{"field": field}
	}
	return nil
}

func errorEnvelope(r *http.Request, code httpx.ErrorCode, message string) (int, []byte) {
	body := map[string]any{
		"error": map[string]any{
			"code":       code,
			"message":    message,
			"request_id": httpx.RequestIDFromContext(r.Context()),
			"details":    map[string]any{},
		},
	}
	encoded, _ := json.Marshal(body)
	return httpx.StatusFor(code), encoded
}

func (a *App) completeIdempotentFailure(r *http.Request, actorID uuid.UUID, route, key string, leaseUntil time.Time, err error) {
	if key == "" {
		return
	}
	code := httpx.CodeInternal
	message := "internal server error"
	switch {
	case errors.Is(err, objects.ErrNotFound):
		code, message = httpx.CodeNotFound, "not found"
	case errors.Is(err, objects.ErrForbidden):
		code, message = httpx.CodeForbidden, "forbidden"
	case errors.Is(err, objects.ErrVersionConflict):
		code, message = httpx.CodeVersionConflict, "version conflict"
	case errors.Is(err, objects.ErrConflict):
		code, message = httpx.CodeConflict, "conflict"
	case errors.Is(err, objects.ErrPayloadTooLarge):
		code, message = httpx.CodePayloadTooLarge, "payload too large"
	case errors.Is(err, objects.ErrInvalidRequest):
		code, message = httpx.CodeInvalidRequest, "invalid request"
	case errors.Is(err, store.ErrIdempotencyConflict):
		code, message = httpx.CodeIdempotencyConflict, "idempotency key was used for a different request"
	case errors.Is(err, store.ErrRequestInProgress):
		code, message = httpx.CodeRequestInProgress, "an identical request is already in progress"
	case errors.Is(err, objects.ErrInvitationInvalid):
		code, message = httpx.CodeInvitationInvalid, "invitation is not valid"
	case errors.Is(err, personalai.ErrNotFound):
		code, message = httpx.CodeNotFound, "not found"
	case errors.Is(err, personalai.ErrForbidden):
		code, message = httpx.CodeForbidden, "forbidden"
	case errors.Is(err, personalai.ErrPayloadTooLarge):
		code, message = httpx.CodePayloadTooLarge, "payload too large"
	case errors.Is(err, personalai.ErrInvalidRequest):
		code, message = httpx.CodeInvalidRequest, "invalid request"
	case errors.Is(err, social.ErrNotFound), errors.Is(err, publication.ErrNotFound):
		code, message = httpx.CodeNotFound, "not found"
	case errors.Is(err, social.ErrForbidden), errors.Is(err, social.ErrBlocked),
		errors.Is(err, publication.ErrForbidden):
		code, message = httpx.CodeForbidden, "forbidden"
	case errors.Is(err, social.ErrConflict), errors.Is(err, publication.ErrConflict):
		code, message = httpx.CodeConflict, "conflict"
	case errors.Is(err, social.ErrInvalidRequest), errors.Is(err, publication.ErrInvalidRequest):
		code, message = httpx.CodeInvalidRequest, "invalid request"
	}
	deterministic := true
	switch {
	case errors.Is(err, objects.ErrNotFound), errors.Is(err, objects.ErrForbidden),
		errors.Is(err, objects.ErrVersionConflict), errors.Is(err, objects.ErrConflict),
		errors.Is(err, objects.ErrPayloadTooLarge), errors.Is(err, objects.ErrInvalidRequest),
		errors.Is(err, objects.ErrInvitationInvalid),
		errors.Is(err, store.ErrIdempotencyConflict), errors.Is(err, store.ErrRequestInProgress),
		errors.Is(err, identity.ErrInvalidCredentials), errors.Is(err, identity.ErrConflict),
		errors.Is(err, identity.ErrInvalidRequest), errors.Is(err, objects.ErrSnapshotMissing),
		errors.Is(err, personalai.ErrNotFound), errors.Is(err, personalai.ErrForbidden),
		errors.Is(err, personalai.ErrInvalidRequest), errors.Is(err, personalai.ErrPayloadTooLarge),
		errors.Is(err, social.ErrNotFound), errors.Is(err, social.ErrForbidden),
		errors.Is(err, social.ErrBlocked), errors.Is(err, social.ErrConflict),
		errors.Is(err, social.ErrInvalidRequest), errors.Is(err, publication.ErrNotFound),
		errors.Is(err, publication.ErrForbidden), errors.Is(err, publication.ErrConflict),
		errors.Is(err, publication.ErrInvalidRequest):
	default:
		deterministic = false
	}
	if !deterministic {
		return
	}
	status, body := errorEnvelope(r, code, message)
	_ = a.idem.CompleteStandalone(r.Context(), actorID, route, key, leaseUntil, status, body)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	buffer := make([]byte, 0, 12)
	for value > 0 {
		buffer = append([]byte{byte('0' + value%10)}, buffer...)
		value /= 10
	}
	if negative {
		buffer = append([]byte{'-'}, buffer...)
	}
	return string(buffer)
}
