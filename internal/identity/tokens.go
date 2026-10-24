// SPDX-License-Identifier: Apache-2.0
package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/audit"
)

// Personal access token scopes are frozen by G00 contract §A.4.2. A scope is an
// endpoint capability ceiling; it never replaces object membership.
const (
	ScopeObjectsRead      = "objects:read"
	ScopeObjectsWrite     = "objects:write"
	ScopeDeriveCreate     = "derive:create"
	ScopeCapabilities     = "capabilities:invoke"
	patPrefix             = "dw_pat_"
	patPlaintextLength    = 50
	patPrefixLength       = 15
	patMaxExpiryDays      = 365
	patLastUsedRefreshGap = 60 * time.Second
)

var scopeOrder = []string{
	ScopeObjectsRead, ScopeObjectsWrite, ScopeDeriveCreate, ScopeCapabilities,
}

// ErrNotFound reports an identity-owned resource that does not belong to the
// caller. It maps to 404 so foreign resources are indistinguishable.
var ErrNotFound = errors.New("identity resource not found")

// ErrBudget is returned when a token request exceeds a frozen bound.
var ErrBudget = errors.New("identity budget exceeded")

// TokenMetadata is the non-secret projection of a PAT. It never contains the
// plaintext token or its hash.
type TokenMetadata struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	TokenPrefix string     `json:"token_prefix"`
	Scopes      []string   `json:"scopes"`
	ExpiresAt   *time.Time `json:"expires_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// CreatedToken is the one-time creation response. Plaintext is never stored.
type CreatedToken struct {
	TokenMetadata
	Plaintext string `json:"plaintext"`
}

// CreateTokenInput is the strict POST /tokens body.
type CreateTokenInput struct {
	Name          string   `json:"name"`
	Scopes        []string `json:"scopes"`
	ExpiresInDays *int     `json:"expires_in_days"`
}

// CreateToken issues a PAT and writes its audit row in the same transaction.
func (s *Service) CreateToken(ctx context.Context, userID uuid.UUID, input CreateTokenInput, requestID string, ipHash []byte) (*CreatedToken, error) {
	name := strings.TrimSpace(input.Name)
	if runes := len([]rune(name)); runes < 1 || runes > 64 {
		return nil, fmt.Errorf("%w: name", ErrInvalidRequest)
	}
	scopes, err := normalizeScopes(input.Scopes)
	if err != nil {
		return nil, err
	}
	var expiresAt *time.Time
	if input.ExpiresInDays != nil {
		days := *input.ExpiresInDays
		if days < 1 || days > patMaxExpiryDays {
			return nil, fmt.Errorf("%w: expires_in_days", ErrInvalidRequest)
		}
		value := time.Now().UTC().AddDate(0, 0, days)
		expiresAt = &value
	}
	raw, err := randomToken()
	if err != nil {
		return nil, err
	}
	plaintext := patPrefix + raw
	if len(plaintext) != patPlaintextLength {
		return nil, errors.New("personal access token length is invalid")
	}
	tokenID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	prefix := plaintext[:patPrefixLength]
	hash := sha256Bytes(plaintext)

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var createdAt time.Time
	if err := tx.QueryRow(ctx, `
INSERT INTO api_tokens (id, user_id, name, token_prefix, token_hash, scopes, expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING created_at`,
		tokenID.String(), userID.String(), name, prefix, hash, scopes, expiresAt,
	).Scan(&createdAt); err != nil {
		return nil, err
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(userID),
		Action:       audit.ActionPATCreate,
		ResourceType: "api_token",
		ResourceID:   &tokenID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     map[string]any{},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &CreatedToken{
		TokenMetadata: TokenMetadata{
			ID: tokenID, Name: name, TokenPrefix: prefix, Scopes: scopes,
			ExpiresAt: expiresAt, CreatedAt: createdAt,
		},
		Plaintext: plaintext,
	}, nil
}

// ListTokens returns only the caller's token metadata.
func (s *Service) ListTokens(ctx context.Context, userID uuid.UUID) ([]TokenMetadata, error) {
	rows, err := s.db.Query(ctx, `
SELECT id, name, token_prefix, scopes, expires_at, last_used_at, revoked_at, created_at
  FROM api_tokens
 WHERE user_id = $1
 ORDER BY created_at DESC, id DESC`, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]TokenMetadata, 0)
	for rows.Next() {
		var (
			item TokenMetadata
			id   string
		)
		if err := rows.Scan(&id, &item.Name, &item.TokenPrefix, &item.Scopes,
			&item.ExpiresAt, &item.LastUsedAt, &item.RevokedAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.ID = uuid.MustParse(id)
		item.Scopes = NormalizeScopes(item.Scopes)
		items = append(items, item)
	}
	return items, rows.Err()
}

// RevokeToken revokes an owned token immediately.
func (s *Service) RevokeToken(ctx context.Context, userID, tokenID uuid.UUID, requestID string, ipHash []byte) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
UPDATE api_tokens SET revoked_at = GREATEST(now(), created_at)
 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		tokenID.String(), userID.String())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(userID),
		Action:       audit.ActionPATRevoke,
		ResourceType: "api_token",
		ResourceID:   &tokenID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     map[string]any{},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AuthenticatePAT resolves a bearer token. It refreshes last_used_at at most
// once per minute and never caches scopes or permissions.
func (s *Service) AuthenticatePAT(ctx context.Context, token string) (*Principal, error) {
	if len(token) != patPlaintextLength || !strings.HasPrefix(token, patPrefix) {
		return nil, ErrUnauthenticated
	}
	var (
		principal Principal
		tokenID   string
		scopes    []string
		lastUsed  *time.Time
		expiresAt *time.Time
		revokedAt *time.Time
	)
	err := s.db.QueryRow(ctx, `
SELECT t.id, t.scopes, t.last_used_at, t.expires_at, t.revoked_at,
       u.id, u.email, u.handle, u.display_name, u.status, u.created_at, u.updated_at
  FROM api_tokens t
  JOIN users u ON u.id = t.user_id
 WHERE t.token_hash = $1`, sha256Bytes(token)).Scan(
		&tokenID, &scopes, &lastUsed, &expiresAt, &revokedAt,
		&principal.User.ID, &principal.User.Email, &principal.User.Handle,
		&principal.User.DisplayName, &principal.User.Status,
		&principal.User.CreatedAt, &principal.User.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUnauthenticated
	}
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if revokedAt != nil || (expiresAt != nil && !expiresAt.After(now)) ||
		principal.User.Status != "active" {
		return nil, ErrUnauthenticated
	}
	id := uuid.MustParse(tokenID)
	if lastUsed == nil || now.Sub(*lastUsed) >= patLastUsedRefreshGap {
		_, _ = s.db.Exec(ctx, `
UPDATE api_tokens SET last_used_at = now()
 WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '60 seconds')`,
			tokenID)
	}
	principal.AuthMethod = "pat"
	principal.TokenID = &id
	principal.Scopes = NormalizeScopes(scopes)
	return &principal, nil
}

// HasScope reports whether a PAT carries the endpoint scope. Cookie sessions
// are never scope-limited by this helper.
func HasScope(principal *Principal, scope string) bool {
	if principal == nil {
		return false
	}
	if principal.AuthMethod != "pat" {
		return true
	}
	for _, value := range principal.Scopes {
		if value == scope {
			return true
		}
	}
	return false
}

// NormalizeScopes deduplicates and orders scopes using the frozen order.
func NormalizeScopes(input []string) []string {
	present := make(map[string]bool, len(input))
	for _, scope := range input {
		present[scope] = true
	}
	out := make([]string, 0, len(scopeOrder))
	for _, scope := range scopeOrder {
		if present[scope] {
			out = append(out, scope)
		}
	}
	return out
}

func normalizeScopes(input []string) ([]string, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("%w: scopes", ErrInvalidRequest)
	}
	seen := make(map[string]bool, len(input))
	for _, scope := range input {
		if !validScope(scope) || seen[scope] {
			return nil, fmt.Errorf("%w: scopes", ErrInvalidRequest)
		}
		seen[scope] = true
	}
	return NormalizeScopes(input), nil
}

func validScope(scope string) bool {
	for _, allowed := range scopeOrder {
		if scope == allowed {
			return true
		}
	}
	return false
}
