// SPDX-License-Identifier: Apache-2.0

// Package identity owns users, browser sessions, and personal access tokens.
// It never makes object authorization decisions and never reads object data.
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/QiyanAI/derive-world/internal/audit"
)

// Domain errors mapped by the composition root.
var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthenticated    = errors.New("unauthenticated")
	ErrConflict           = errors.New("identity conflict")
	ErrInvalidRequest     = errors.New("invalid identity request")
)

var handlePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,31}$`)

// User is the public identity record; password hashes never leave this package.
type User struct {
	ID          uuid.UUID
	Email       string
	Handle      string
	DisplayName string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Session is a freshly issued browser session.
type Session struct {
	ID        uuid.UUID
	Token     string
	CSRFToken string
	ExpiresAt time.Time
}

// Principal is the authenticated server-side identity for one request.
// AuthMethod is either "session" or "pat"; PAT principals never carry a CSRF
// hash or an expiration that belongs to a browser session.
type Principal struct {
	User           User
	AuthMethod     string
	SessionID      uuid.UUID
	CSRFTokenHash  []byte
	SessionExpires time.Time
	LastSeenAt     time.Time
	TokenID        *uuid.UUID
	Scopes         []string
}

// RegisterInput is the strict registration body.
type RegisterInput struct {
	Email       string `json:"email"`
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}

// PreparedRegistration carries IDs, hashes, and tokens assembled before the
// registration transaction. It never contains a plaintext password.
type PreparedRegistration struct {
	UserID           uuid.UUID
	ProfileID        uuid.UUID
	DelegationID     uuid.UUID
	SessionID        uuid.UUID
	Email            string
	Handle           string
	DisplayName      string
	PasswordHash     string
	SessionToken     string
	CSRFToken        string
	SessionTokenHash []byte
	CSRFTokenHash    []byte
	CreatedAt        time.Time
	ExpiresAt        time.Time
}

// LoginResult is returned by a successful sign-in.
type LoginResult struct {
	User    User
	Session Session
}

// Service is the identity domain service.
type Service struct {
	db          *pgxpool.Pool
	audit       *audit.Writer
	dummyHash   string
	absoluteTTL time.Duration
	idleTTL     time.Duration
	argonSlots  chan struct{}
}

// NewService assembles the identity service. The idle TTL controls session
// validity; the absolute TTL controls both the row and cookie lifespan.
func NewService(db *pgxpool.Pool, auditWriter *audit.Writer, absoluteTTL, idleTTL time.Duration) (*Service, error) {
	if absoluteTTL <= 0 || idleTTL <= 0 {
		return nil, errors.New("session lifetimes must be positive")
	}
	if idleTTL > absoluteTTL {
		return nil, errors.New("session idle lifetime must not exceed the absolute lifetime")
	}
	dummy, err := HashPassword("dummy-password-for-timing-equalization-0001")
	if err != nil {
		return nil, err
	}
	slots := runtime.GOMAXPROCS(0)
	if slots < 1 {
		slots = 1
	}
	return &Service{
		db:          db,
		audit:       auditWriter,
		dummyHash:   dummy,
		absoluteTTL: absoluteTTL,
		idleTTL:     idleTTL,
		argonSlots:  make(chan struct{}, slots),
	}, nil
}

// PrepareRegistration validates input and performs all expensive work before
// the caller opens the registration transaction.
func (s *Service) PrepareRegistration(input RegisterInput) (*PreparedRegistration, error) {
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if len(email) < 3 || len(email) > 254 {
		return nil, fmt.Errorf("%w: email", ErrInvalidRequest)
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return nil, fmt.Errorf("%w: email", ErrInvalidRequest)
	}
	handle := strings.ToLower(strings.TrimSpace(input.Handle))
	if !handlePattern.MatchString(handle) {
		return nil, fmt.Errorf("%w: handle", ErrInvalidRequest)
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if runes := utf8.RuneCountInString(displayName); runes < 1 || runes > 80 {
		return nil, fmt.Errorf("%w: display_name", ErrInvalidRequest)
	}
	passwordHash, err := s.hashPassword(input.Password)
	if err != nil {
		return nil, fmt.Errorf("%w: password", ErrInvalidRequest)
	}
	userID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	profileID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	delegationID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	sessionID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	csrf, err := randomToken()
	if err != nil {
		return nil, err
	}
	return &PreparedRegistration{
		UserID:           userID,
		ProfileID:        profileID,
		DelegationID:     delegationID,
		SessionID:        sessionID,
		Email:            email,
		Handle:           handle,
		DisplayName:      displayName,
		PasswordHash:     passwordHash,
		SessionToken:     token,
		CSRFToken:        csrf,
		SessionTokenHash: sha256Bytes(token),
		CSRFTokenHash:    sha256Bytes(csrf),
		CreatedAt:        time.Now().UTC(),
		ExpiresAt:        time.Now().UTC().Add(s.absoluteTTL),
	}, nil
}

func (s *Service) hashPassword(password string) (string, error) {
	s.argonSlots <- struct{}{}
	defer func() { <-s.argonSlots }()
	return HashPassword(password)
}

func (s *Service) verifyPassword(encoded, password string) (bool, error) {
	s.argonSlots <- struct{}{}
	defer func() { <-s.argonSlots }()
	return VerifyPassword(encoded, password)
}

// InsertUserTx writes the user row inside the caller's transaction.
func (s *Service) InsertUserTx(ctx context.Context, tx pgx.Tx, prepared *PreparedRegistration) error {
	_, err := tx.Exec(ctx, `
INSERT INTO users (id, email, handle, display_name, password_hash)
VALUES ($1,$2,$3,$4,$5)`,
		prepared.UserID.String(), prepared.Email, prepared.Handle,
		prepared.DisplayName, prepared.PasswordHash)
	return mapUniqueViolation(err)
}

// InsertSessionTx writes the registration session inside the same transaction.
func (s *Service) InsertSessionTx(ctx context.Context, tx pgx.Tx, prepared *PreparedRegistration) error {
	_, err := tx.Exec(ctx, `
INSERT INTO user_sessions (id, user_id, token_hash, csrf_hash, expires_at)
VALUES ($1,$2,$3,$4,$5)`,
		prepared.SessionID.String(), prepared.UserID.String(),
		prepared.SessionTokenHash, prepared.CSRFTokenHash, prepared.ExpiresAt)
	return err
}

// WriteRegisterAudit writes the registration audit row in the caller's tx.
func (s *Service) WriteRegisterAudit(ctx context.Context, tx pgx.Tx, prepared *PreparedRegistration, requestID string, ipHash []byte) error {
	return s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(prepared.UserID),
		Action:       audit.ActionAuthRegister,
		ResourceType: "user",
		ResourceID:   &prepared.UserID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     map[string]any{"user_id": prepared.UserID.String()},
	})
}

// Login verifies credentials, rotates any presented session, and issues a new
// session. Failures are uniform and audited.
func (s *Service) Login(ctx context.Context, email, password, oldToken, requestID string, ipHash []byte) (*LoginResult, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	var (
		user         User
		passwordHash string
	)
	err := s.db.QueryRow(ctx, `
SELECT id, email, handle, display_name, status, created_at, updated_at, password_hash
  FROM users WHERE email = $1`, normalized).Scan(
		&user.ID, &user.Email, &user.Handle, &user.DisplayName, &user.Status,
		&user.CreatedAt, &user.UpdatedAt, &passwordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		_, _ = s.verifyPassword(s.dummyHash, password)
		s.writeLoginFailure(ctx, nil, requestID, ipHash)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	ok, err := s.verifyPassword(passwordHash, password)
	if err != nil || !ok || user.Status != "active" {
		s.writeLoginFailure(ctx, &user.ID, requestID, ipHash)
		return nil, ErrInvalidCredentials
	}

	session, err := s.newSession()
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if oldToken != "" {
		if _, err := tx.Exec(ctx, `
UPDATE user_sessions SET revoked_at = GREATEST(now(), created_at)
 WHERE token_hash = $1 AND user_id = $2 AND revoked_at IS NULL`,
			sha256Bytes(oldToken), user.ID.String()); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO user_sessions (id, user_id, token_hash, csrf_hash, expires_at)
VALUES ($1,$2,$3,$4,$5)`,
		session.ID.String(), user.ID.String(), sha256Bytes(session.Token),
		sha256Bytes(session.CSRFToken), session.ExpiresAt); err != nil {
		return nil, err
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(user.ID),
		Action:       audit.ActionAuthLogin,
		ResourceType: "user",
		ResourceID:   &user.ID,
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
	return &LoginResult{User: user, Session: *session}, nil
}

func (s *Service) writeLoginFailure(ctx context.Context, userID *uuid.UUID, requestID string, ipHash []byte) {
	event := audit.Event{
		Actor:        audit.SystemActor(),
		Action:       audit.ActionAuthLoginFailed,
		ResourceType: "user",
		Decision:     "deny",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     map[string]any{"reason": "invalid_credentials"},
	}
	if userID != nil {
		event.Actor = audit.UserActor(*userID)
		event.ResourceID = userID
	}
	_ = s.audit.WriteStandalone(ctx, event)
}

func (s *Service) newSession() (*Session, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	csrf, err := randomToken()
	if err != nil {
		return nil, err
	}
	return &Session{
		ID:        id,
		Token:     token,
		CSRFToken: csrf,
		ExpiresAt: time.Now().UTC().Add(s.absoluteTTL),
	}, nil
}

// Authenticate resolves a session cookie token. It refreshes last_seen_at at
// most once per minute and never caches permissions.
func (s *Service) Authenticate(ctx context.Context, token string) (*Principal, error) {
	if token == "" {
		return nil, ErrUnauthenticated
	}
	var (
		principal Principal
		revokedAt *time.Time
	)
	err := s.db.QueryRow(ctx, `
SELECT s.id, s.csrf_hash, s.expires_at, s.last_seen_at, s.revoked_at,
       u.id, u.email, u.handle, u.display_name, u.status, u.created_at, u.updated_at
  FROM user_sessions s
  JOIN users u ON u.id = s.user_id
 WHERE s.token_hash = $1
   AND s.revoked_at IS NULL
   AND s.expires_at > now()
   AND s.last_seen_at > now() - make_interval(secs => $2)
   AND u.status = 'active'`, sha256Bytes(token), s.idleTTL.Seconds()).Scan(
		&principal.SessionID, &principal.CSRFTokenHash, &principal.SessionExpires,
		&principal.LastSeenAt, &revokedAt,
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
	if revokedAt != nil || !principal.SessionExpires.After(now) ||
		principal.LastSeenAt.Add(s.idleTTL).Before(now) ||
		principal.User.Status != "active" {
		return nil, ErrUnauthenticated
	}
	refreshAfter := 60 * time.Second
	if s.idleTTL < 2*time.Minute {
		refreshAfter = s.idleTTL / 2
	}
	if refreshAfter <= 0 {
		refreshAfter = time.Second
	}
	_, _ = s.db.Exec(ctx, `
UPDATE user_sessions
   SET last_seen_at = LEAST(GREATEST(now(), last_seen_at), expires_at)
 WHERE id = $1 AND last_seen_at < now() - make_interval(secs => $2)`,
		principal.SessionID.String(), refreshAfter.Seconds())
	principal.AuthMethod = "session"
	return &principal, nil
}

// Logout revokes exactly the current session and writes the audit row.
func (s *Service) Logout(ctx context.Context, principal *Principal, requestID string, ipHash []byte) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
UPDATE user_sessions SET revoked_at = GREATEST(now(), created_at)
 WHERE id = $1 AND revoked_at IS NULL`, principal.SessionID.String())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUnauthenticated
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(principal.User.ID),
		Action:       audit.ActionAuthLogout,
		ResourceType: "user_session",
		ResourceID:   &principal.SessionID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     map[string]any{},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ValidateCSRF compares the header value to the session-bound hash.
func ValidateCSRF(principal *Principal, header string) bool {
	if header == "" || len(principal.CSRFTokenHash) != 32 {
		return false
	}
	sum := sha256Bytes(header)
	return subtle.ConstantTimeCompare(sum, principal.CSRFTokenHash) == 1
}

// GetUser loads one user by id.
func (s *Service) GetUser(ctx context.Context, userID uuid.UUID) (*User, error) {
	var user User
	err := s.db.QueryRow(ctx, `
SELECT id, email, handle, display_name, status, created_at, updated_at
  FROM users WHERE id = $1`, userID.String()).Scan(
		&user.ID, &user.Email, &user.Handle, &user.DisplayName, &user.Status,
		&user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUnauthenticated
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// Discoverability returns whether this account may appear in authenticated,
// query-scoped account discovery.
func (s *Service) Discoverability(ctx context.Context, userID uuid.UUID) (bool, error) {
	var discoverable bool
	err := s.db.QueryRow(ctx, `
SELECT discoverable FROM users WHERE id = $1 AND status = 'active'`,
		userID.String()).Scan(&discoverable)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrUnauthenticated
	}
	return discoverable, err
}

// SetDiscoverability changes the account-discovery preference. Identity owns
// the users row; the privacy-relevant change is audited in the same
// transaction.
func (s *Service) SetDiscoverability(ctx context.Context, userID uuid.UUID, discoverable bool,
	requestID string, ipHash []byte) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	command, err := tx.Exec(ctx, `
UPDATE users SET discoverable = $2, updated_at = now()
 WHERE id = $1 AND status = 'active'`, userID.String(), discoverable)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrUnauthenticated
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(userID),
		Action:       audit.ActionSocialDiscoverabilityUpdate,
		ResourceType: "user",
		ResourceID:   &userID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     map[string]any{"discoverable": discoverable},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func randomToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func sha256Bytes(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

func mapUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return err
}
