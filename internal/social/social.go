// SPDX-License-Identifier: Apache-2.0

// Package social owns friendships, blocks, direct human messages, and object
// discussions. It never reads or writes Personal AI private data and never
// grants object permissions; object access remains owned by package objects.
package social

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/QiyanAI/derive-world/internal/audit"
)

var (
	ErrNotFound       = errors.New("social resource not found")
	ErrForbidden      = errors.New("social action forbidden")
	ErrConflict       = errors.New("social conflict")
	ErrInvalidRequest = errors.New("invalid social request")
	ErrBlocked        = errors.New("social contact blocked")
)

const (
	maxMessageRunes = 4000
)

// Friend is an accepted friendship projection with an unread counter.
type Friend struct {
	UserID      string    `json:"user_id"`
	Handle      string    `json:"handle"`
	DisplayName string    `json:"display_name"`
	Since       time.Time `json:"since"`
	Unread      int       `json:"unread"`
}

// FriendRequest is one pending request in either direction.
type FriendRequest struct {
	ID          string    `json:"id"`
	Direction   string    `json:"direction"`
	UserID      string    `json:"user_id"`
	Handle      string    `json:"handle"`
	DisplayName string    `json:"display_name"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

// Message is a direct human message.
type Message struct {
	ID              string     `json:"id"`
	Seq             int64      `json:"seq"`
	SenderID        string     `json:"sender_id"`
	RecipientID     string     `json:"recipient_id"`
	Body            string     `json:"body"`
	ClientMessageID string     `json:"client_message_id"`
	CreatedAt       time.Time  `json:"created_at"`
	ReadAt          *time.Time `json:"read_at"`
}

// Block is an active block projection.
type Block struct {
	UserID      string    `json:"user_id"`
	Handle      string    `json:"handle"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

// Discussion is one object discussion post.
type Discussion struct {
	ID          string    `json:"id"`
	Seq         int64     `json:"seq"`
	ObjectID    string    `json:"object_id"`
	AuthorID    string    `json:"author_id"`
	AuthorName  string    `json:"author_name"`
	Body        string    `json:"body"`
	CreatedAt   time.Time `json:"created_at"`
	ClientMsgID string    `json:"client_message_id"`
}

// Service is the controlled Social write path.
type Service struct {
	db    *pgxpool.Pool
	audit *audit.Writer
}

// NewService assembles the Social service.
func NewService(db *pgxpool.Pool, auditWriter *audit.Writer) *Service {
	return &Service{db: db, audit: auditWriter}
}

// DiscoveryCandidate is deliberately limited to opt-in account information.
// It never includes email, private AI context, memberships, or object data.
type DiscoveryCandidate struct {
	UserID      string `json:"user_id"`
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name"`
}

// DiscoveryEnabled reads only the requesting account's opt-in setting.
func (s *Service) DiscoveryEnabled(ctx context.Context, userID uuid.UUID) (bool, error) {
	var enabled bool
	err := s.db.QueryRow(ctx, `
SELECT COALESCE((SELECT discoverable FROM social_discovery_settings WHERE user_id = $1), false)`,
		userID.String()).Scan(&enabled)
	return enabled, err
}

// SetDiscoveryEnabled changes only the caller's own opt-in setting.
func (s *Service) SetDiscoveryEnabled(ctx context.Context, userID uuid.UUID, enabled bool, requestID string, ipHash []byte) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
INSERT INTO social_discovery_settings (user_id, discoverable) VALUES ($1,$2)
ON CONFLICT (user_id) DO UPDATE SET discoverable = EXCLUDED.discoverable, updated_at = now()`,
		userID.String(), enabled)
	if err != nil {
		return err
	}
	if err := s.writeAudit(ctx, tx, userID, audit.ActionSocialDiscoveryPreference,
		"social_discovery_settings", &userID, requestID, ipHash,
		map[string]any{"discoverable": enabled}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SearchDiscovery only returns actively opted-in users and excludes blocked pairs.
// Fixed size and escaped prefix/substring matching avoid unrestricted enumeration.
func (s *Service) SearchDiscovery(ctx context.Context, userID uuid.UUID, query string) ([]DiscoveryCandidate, error) {
	term := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(query), "@"))
	if utf8.RuneCountInString(term) < 2 || utf8.RuneCountInString(term) > 40 {
		return nil, fmt.Errorf("%w: search length", ErrInvalidRequest)
	}
	escaped := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(term)
	rows, err := s.db.Query(ctx, `
SELECT u.id, u.handle, u.display_name
  FROM users u
  JOIN social_discovery_settings opt ON opt.user_id = u.id AND opt.discoverable = true
 WHERE u.status = 'active' AND u.id <> $1
   AND (u.handle ILIKE $2 OR u.display_name ILIKE $3)
   AND NOT EXISTS (SELECT 1 FROM user_blocks b
                   WHERE b.revoked_at IS NULL
                     AND ((b.blocker_id = $1 AND b.blocked_id = u.id)
                       OR (b.blocker_id = u.id AND b.blocked_id = $1)))
 ORDER BY CASE WHEN lower(u.handle) = lower($4) THEN 0 ELSE 1 END,
          lower(u.display_name), u.handle
 LIMIT 10`, userID.String(), escaped+"%", "%"+escaped+"%", term)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	matches := make([]DiscoveryCandidate, 0, 10)
	for rows.Next() {
		var candidate DiscoveryCandidate
		if err := rows.Scan(&candidate.UserID, &candidate.Handle, &candidate.DisplayName); err != nil {
			return nil, err
		}
		matches = append(matches, candidate)
	}
	return matches, rows.Err()
}

// ListFriends returns accepted friends with unread message counts.
func (s *Service) ListFriends(ctx context.Context, userID uuid.UUID) ([]Friend, error) {
	rows, err := s.db.Query(ctx, `
SELECT other.id, other.handle, other.display_name, f.responded_at,
       (SELECT count(*)
          FROM direct_messages dm
         WHERE dm.sender_id = other.id
           AND dm.recipient_id = $1
           AND dm.read_at IS NULL)
  FROM friendships f
  JOIN users other
    ON other.id = CASE WHEN f.requester_id = $1 THEN f.addressee_id ELSE f.requester_id END
 WHERE f.status = 'accepted'
   AND ($1 IN (f.requester_id, f.addressee_id))
 ORDER BY lower(other.display_name), other.id`, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	friends := make([]Friend, 0)
	for rows.Next() {
		var (
			friend Friend
			id     string
			since  time.Time
		)
		if err := rows.Scan(&id, &friend.Handle, &friend.DisplayName, &since, &friend.Unread); err != nil {
			return nil, err
		}
		friend.UserID = id
		friend.Since = since
		friends = append(friends, friend)
	}
	return friends, rows.Err()
}

// ListRequests returns pending requests. direction is incoming or outgoing.
func (s *Service) ListRequests(ctx context.Context, userID uuid.UUID, direction string) ([]FriendRequest, error) {
	var query string
	switch direction {
	case "incoming":
		query = `
SELECT f.id, 'incoming', other.id, other.handle, other.display_name, f.status, f.created_at
  FROM friendships f
  JOIN users other ON other.id = f.requester_id
 WHERE f.addressee_id = $1 AND f.status = 'pending'
 ORDER BY f.created_at DESC, f.id DESC`
	case "outgoing":
		query = `
SELECT f.id, 'outgoing', other.id, other.handle, other.display_name, f.status, f.created_at
  FROM friendships f
  JOIN users other ON other.id = f.addressee_id
 WHERE f.requester_id = $1 AND f.status = 'pending'
 ORDER BY f.created_at DESC, f.id DESC`
	default:
		return nil, fmt.Errorf("%w: direction", ErrInvalidRequest)
	}
	rows, err := s.db.Query(ctx, query, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	requests := make([]FriendRequest, 0)
	for rows.Next() {
		var (
			request FriendRequest
			id      string
		)
		if err := rows.Scan(&id, &request.Direction, &request.UserID, &request.Handle,
			&request.DisplayName, &request.Status, &request.CreatedAt); err != nil {
			return nil, err
		}
		request.ID = id
		requests = append(requests, request)
	}
	return requests, rows.Err()
}

// SendRequest creates or revives a pending request for a target handle.
func (s *Service) SendRequest(ctx context.Context, userID uuid.UUID, handle, requestID string, ipHash []byte) (*FriendRequest, error) {
	normalized := strings.ToLower(strings.TrimSpace(handle))
	if len(normalized) < 3 || len(normalized) > 32 {
		return nil, fmt.Errorf("%w: handle", ErrInvalidRequest)
	}
	var (
		targetID   string
		targetName string
		targetHdl  string
	)
	err := s.db.QueryRow(ctx, `
SELECT id, handle, display_name FROM users WHERE handle = $1 AND status = 'active'`,
		normalized).Scan(&targetID, &targetHdl, &targetName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if targetID == userID.String() {
		return nil, fmt.Errorf("%w: cannot add yourself", ErrInvalidRequest)
	}
	targetUUID := uuid.MustParse(targetID)

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := ensureNotBlockedTx(ctx, tx, userID, targetUUID); err != nil {
		return nil, err
	}
	existing, err := friendshipTx(ctx, tx, userID, targetUUID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var friendshipID uuid.UUID
	if existing != nil {
		if existing.Status == "accepted" {
			return nil, fmt.Errorf("%w: already friends", ErrConflict)
		}
		if existing.Status == "pending" {
			return nil, fmt.Errorf("%w: request already pending", ErrConflict)
		}
		friendshipID = existing.ID
		_, err = tx.Exec(ctx, `
UPDATE friendships
   SET requester_id = $2, addressee_id = $3, status = 'pending',
       created_at = now(), updated_at = now(), responded_at = NULL
 WHERE id = $1`, friendshipID.String(), userID.String(), targetID)
		if err != nil {
			return nil, err
		}
	} else {
		friendshipID, err = uuid.NewV7()
		if err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `
INSERT INTO friendships (id, requester_id, addressee_id, status)
VALUES ($1,$2,$3,'pending')`, friendshipID.String(), userID.String(), targetID)
		if err != nil {
			return nil, err
		}
	}
	if err := s.writeAudit(ctx, tx, userID, audit.ActionSocialFriendRequest,
		"friendship", &friendshipID, requestID, ipHash, map[string]any{"target_user_id": targetID}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &FriendRequest{
		ID: friendshipID.String(), Direction: "outgoing", UserID: targetID,
		Handle: targetHdl, DisplayName: targetName, Status: "pending", CreatedAt: now,
	}, nil
}

// RespondRequest accepts or declines an incoming pending request.
func (s *Service) RespondRequest(ctx context.Context, userID, friendshipID uuid.UUID, accept bool, requestID string, ipHash []byte) (*FriendRequest, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		requesterID string
		addresseeID string
		status      string
	)
	err = tx.QueryRow(ctx, `
SELECT requester_id, addressee_id, status FROM friendships WHERE id = $1 FOR UPDATE`,
		friendshipID.String()).Scan(&requesterID, &addresseeID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if addresseeID != userID.String() || status != "pending" {
		return nil, ErrNotFound
	}
	requesterUUID := uuid.MustParse(requesterID)
	if err := ensureNotBlockedTx(ctx, tx, userID, requesterUUID); err != nil {
		return nil, err
	}
	newStatus := "declined"
	action := audit.ActionSocialFriendDecline
	if accept {
		newStatus = "accepted"
		action = audit.ActionSocialFriendAccept
	}
	_, err = tx.Exec(ctx, `
UPDATE friendships
   SET status = $2, responded_at = now(), updated_at = now()
 WHERE id = $1`, friendshipID.String(), newStatus)
	if err != nil {
		return nil, err
	}
	if err := s.writeAudit(ctx, tx, userID, action, "friendship", &friendshipID,
		requestID, ipHash, map[string]any{"other_user_id": requesterID}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &FriendRequest{
		ID: friendshipID.String(), Direction: "incoming", UserID: requesterID,
		Status: newStatus, CreatedAt: time.Now().UTC(),
	}, nil
}

// RemoveFriend ends an accepted friendship. Object memberships are unchanged.
func (s *Service) RemoveFriend(ctx context.Context, userID, otherID uuid.UUID, requestID string, ipHash []byte) error {
	if userID == otherID {
		return fmt.Errorf("%w: user_id", ErrInvalidRequest)
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	existing, err := friendshipTx(ctx, tx, userID, otherID)
	if err != nil {
		return err
	}
	if existing == nil || existing.Status != "accepted" {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
UPDATE friendships
   SET status = 'removed', responded_at = now(), updated_at = now()
 WHERE id = $1`, existing.ID.String()); err != nil {
		return err
	}
	if err := s.writeAudit(ctx, tx, userID, audit.ActionSocialFriendRemove,
		"friendship", &existing.ID, requestID, ipHash,
		map[string]any{"other_user_id": otherID.String()}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Block stops new friend contact and direct messages. It does not revoke
// object memberships; the UI must explain that separately.
func (s *Service) Block(ctx context.Context, userID, targetID uuid.UUID, requestID string, ipHash []byte) error {
	if userID == targetID {
		return fmt.Errorf("%w: user_id", ErrInvalidRequest)
	}
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND status = 'active')`,
		targetID.String()).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
INSERT INTO user_blocks (blocker_id, blocked_id)
VALUES ($1,$2)
ON CONFLICT (blocker_id, blocked_id) DO UPDATE SET revoked_at = NULL`,
		userID.String(), targetID.String()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE friendships
   SET status = 'removed', responded_at = COALESCE(responded_at, now()), updated_at = now()
 WHERE ((requester_id = $1 AND addressee_id = $2)
     OR (requester_id = $2 AND addressee_id = $1))
   AND status IN ('pending','accepted')`, userID.String(), targetID.String()); err != nil {
		return err
	}
	if err := s.writeAudit(ctx, tx, userID, audit.ActionSocialBlock,
		"user", &targetID, requestID, ipHash, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Unblock revokes an active block.
func (s *Service) Unblock(ctx context.Context, userID, targetID uuid.UUID, requestID string, ipHash []byte) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	command, err := tx.Exec(ctx, `
UPDATE user_blocks SET revoked_at = now()
 WHERE blocker_id = $1 AND blocked_id = $2 AND revoked_at IS NULL`,
		userID.String(), targetID.String())
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := s.writeAudit(ctx, tx, userID, audit.ActionSocialUnblock,
		"user", &targetID, requestID, ipHash, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListBlocks returns active blocks created by the user.
func (s *Service) ListBlocks(ctx context.Context, userID uuid.UUID) ([]Block, error) {
	rows, err := s.db.Query(ctx, `
SELECT u.id, u.handle, u.display_name, b.created_at
  FROM user_blocks b
  JOIN users u ON u.id = b.blocked_id
 WHERE b.blocker_id = $1 AND b.revoked_at IS NULL
 ORDER BY b.created_at DESC`, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	blocks := make([]Block, 0)
	for rows.Next() {
		var (
			block Block
			id    string
		)
		if err := rows.Scan(&id, &block.Handle, &block.DisplayName, &block.CreatedAt); err != nil {
			return nil, err
		}
		block.UserID = id
		blocks = append(blocks, block)
	}
	return blocks, rows.Err()
}

// SendMessage inserts one direct message. clientMessageID makes retries
// idempotent for the sender.
func (s *Service) SendMessage(ctx context.Context, userID, recipientID uuid.UUID, body, clientMessageID string, requestID string, ipHash []byte) (*Message, error) {
	if userID == recipientID {
		return nil, fmt.Errorf("%w: recipient_id", ErrInvalidRequest)
	}
	if err := validateBody(body); err != nil {
		return nil, err
	}
	clientID, err := uuid.Parse(clientMessageID)
	if err != nil {
		return nil, fmt.Errorf("%w: client_message_id", ErrInvalidRequest)
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	allowed, blocked, err := contactStateTx(ctx, tx, userID, recipientID)
	if err != nil {
		return nil, err
	}
	if blocked {
		return nil, ErrBlocked
	}
	if !allowed {
		return nil, ErrForbidden
	}

	messageID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	var (
		scannedID string
		message   Message
	)
	err = tx.QueryRow(ctx, `
INSERT INTO direct_messages (id, client_message_id, sender_id, recipient_id, body)
VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (sender_id, client_message_id) DO NOTHING
RETURNING id, seq, sender_id, recipient_id, body, client_message_id, created_at`,
		messageID.String(), clientID.String(), userID.String(), recipientID.String(), body,
	).Scan(&scannedID, &message.Seq, &message.SenderID, &message.RecipientID,
		&message.Body, &message.ClientMessageID, &message.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// A replay of the same sender/client id. It must describe the same
		// recipient and body, otherwise the key was reused for a new intent.
		err = tx.QueryRow(ctx, `
SELECT id, seq, sender_id, recipient_id, body, client_message_id, created_at, read_at
  FROM direct_messages
 WHERE sender_id = $1 AND client_message_id = $2`,
			userID.String(), clientID.String()).Scan(
			&scannedID, &message.Seq, &message.SenderID, &message.RecipientID,
			&message.Body, &message.ClientMessageID, &message.CreatedAt, &message.ReadAt)
		if err != nil {
			return nil, err
		}
		if message.RecipientID != recipientID.String() || message.Body != body {
			return nil, fmt.Errorf("%w: client_message_id reused", ErrConflict)
		}
		message.ID = scannedID
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &message, nil
	}
	if err != nil {
		return nil, err
	}
	message.ID = scannedID
	if err := s.writeAudit(ctx, tx, userID, audit.ActionSocialMessageSend,
		"direct_message", &messageID, requestID, ipHash,
		map[string]any{"recipient_id": recipientID.String()}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &message, nil
}

// ListMessages returns the pairwise thread ordered by server sequence.
func (s *Service) ListMessages(ctx context.Context, userID, otherID uuid.UUID, afterSeq int64, limit int) ([]Message, *int64, error) {
	if userID == otherID {
		return nil, nil, fmt.Errorf("%w: with", ErrInvalidRequest)
	}
	rows, err := s.db.Query(ctx, `
SELECT id, seq, sender_id, recipient_id, body, client_message_id, created_at, read_at
  FROM direct_messages
 WHERE ((sender_id = $1 AND recipient_id = $2)
     OR (sender_id = $2 AND recipient_id = $1))
   AND seq > $3
 ORDER BY seq ASC
 LIMIT $4`, userID.String(), otherID.String(), afterSeq, limit+1)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	messages := make([]Message, 0, limit)
	var (
		lastSeq int64
		count   int
	)
	for rows.Next() {
		var message Message
		if err := rows.Scan(&message.ID, &message.Seq, &message.SenderID,
			&message.RecipientID, &message.Body, &message.ClientMessageID,
			&message.CreatedAt, &message.ReadAt); err != nil {
			return nil, nil, err
		}
		count++
		if count > limit {
			continue
		}
		lastSeq = message.Seq
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if count > limit {
		return messages, &lastSeq, nil
	}
	return messages, nil, nil
}

// MarkRead marks messages from one sender as read.
func (s *Service) MarkRead(ctx context.Context, userID, otherID uuid.UUID) (int64, error) {
	if userID == otherID {
		return 0, fmt.Errorf("%w: with", ErrInvalidRequest)
	}
	command, err := s.db.Exec(ctx, `
UPDATE direct_messages SET read_at = now()
 WHERE recipient_id = $1 AND sender_id = $2 AND read_at IS NULL`,
		userID.String(), otherID.String())
	if err != nil {
		return 0, err
	}
	return command.RowsAffected(), nil
}

// PostDiscussion appends one human discussion post. The caller must already
// have authorized object read access through package objects.
func (s *Service) PostDiscussion(ctx context.Context, objectID, authorID uuid.UUID, body, clientMessageID, requestID string, ipHash []byte) (*Discussion, error) {
	if err := validateBody(body); err != nil {
		return nil, err
	}
	clientID, err := uuid.Parse(clientMessageID)
	if err != nil {
		return nil, fmt.Errorf("%w: client_message_id", ErrInvalidRequest)
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	discussionID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	var (
		scannedID  string
		discussion Discussion
	)
	err = tx.QueryRow(ctx, `
INSERT INTO object_discussions (id, client_message_id, object_id, author_id, body)
VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (author_id, client_message_id) DO NOTHING
RETURNING id, seq, object_id, author_id, body, client_message_id, created_at`,
		discussionID.String(), clientID.String(), objectID.String(), authorID.String(), body,
	).Scan(&scannedID, &discussion.Seq, &discussion.ObjectID, &discussion.AuthorID,
		&discussion.Body, &discussion.ClientMsgID, &discussion.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
SELECT id, seq, object_id, author_id, body, client_message_id, created_at
  FROM object_discussions
 WHERE author_id = $1 AND client_message_id = $2`,
			authorID.String(), clientID.String()).Scan(
			&scannedID, &discussion.Seq, &discussion.ObjectID, &discussion.AuthorID,
			&discussion.Body, &discussion.ClientMsgID, &discussion.CreatedAt)
		if err != nil {
			return nil, err
		}
		if discussion.ObjectID != objectID.String() || discussion.Body != body {
			return nil, fmt.Errorf("%w: client_message_id reused", ErrConflict)
		}
		discussion.ID = scannedID
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &discussion, nil
	}
	if err != nil {
		return nil, err
	}
	discussion.ID = scannedID
	if err := s.writeAudit(ctx, tx, authorID, audit.ActionSocialDiscussionPost,
		"living_object", &objectID, requestID, ipHash, nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &discussion, nil
}

// ListDiscussions returns object discussion posts after a sequence cursor.
func (s *Service) ListDiscussions(ctx context.Context, objectID uuid.UUID, afterSeq int64, limit int) ([]Discussion, *int64, error) {
	rows, err := s.db.Query(ctx, `
SELECT d.id, d.seq, d.object_id, d.author_id, u.display_name, d.body,
       d.client_message_id, d.created_at
  FROM object_discussions d
  JOIN users u ON u.id = d.author_id
 WHERE d.object_id = $1 AND d.seq > $2
 ORDER BY d.seq ASC
 LIMIT $3`, objectID.String(), afterSeq, limit+1)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	posts := make([]Discussion, 0, limit)
	var (
		lastSeq int64
		count   int
	)
	for rows.Next() {
		var post Discussion
		if err := rows.Scan(&post.ID, &post.Seq, &post.ObjectID, &post.AuthorID,
			&post.AuthorName, &post.Body, &post.ClientMsgID, &post.CreatedAt); err != nil {
			return nil, nil, err
		}
		count++
		if count > limit {
			continue
		}
		lastSeq = post.Seq
		posts = append(posts, post)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if count > limit {
		return posts, &lastSeq, nil
	}
	return posts, nil, nil
}

// Activity is the Social slice of Living Pulse: only real pending work.
type Activity struct {
	IncomingRequests []FriendRequest `json:"incoming_requests"`
	UnreadMessages   int             `json:"unread_messages"`
	RecentMessages   []Message       `json:"recent_messages"`
}

// RecentActivity returns real social signals for the caller.
func (s *Service) RecentActivity(ctx context.Context, userID uuid.UUID) (*Activity, error) {
	requests, err := s.ListRequests(ctx, userID, "incoming")
	if err != nil {
		return nil, err
	}
	var unread int
	if err := s.db.QueryRow(ctx, `
SELECT count(*) FROM direct_messages WHERE recipient_id = $1 AND read_at IS NULL`,
		userID.String()).Scan(&unread); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
SELECT id, seq, sender_id, recipient_id, body, client_message_id, created_at, read_at
  FROM direct_messages
 WHERE recipient_id = $1
 ORDER BY seq DESC
 LIMIT 5`, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	recent := make([]Message, 0, 5)
	for rows.Next() {
		var message Message
		if err := rows.Scan(&message.ID, &message.Seq, &message.SenderID,
			&message.RecipientID, &message.Body, &message.ClientMessageID,
			&message.CreatedAt, &message.ReadAt); err != nil {
			return nil, err
		}
		recent = append(recent, message)
	}
	return &Activity{IncomingRequests: requests, UnreadMessages: unread, RecentMessages: recent}, rows.Err()
}

type friendshipState struct {
	ID     uuid.UUID
	Status string
}

func friendshipTx(ctx context.Context, tx pgx.Tx, left, right uuid.UUID) (*friendshipState, error) {
	var (
		id     string
		status string
	)
	err := tx.QueryRow(ctx, `
SELECT id, status FROM friendships
 WHERE (requester_id = $1 AND addressee_id = $2)
    OR (requester_id = $2 AND addressee_id = $1)
 FOR UPDATE`, left.String(), right.String()).Scan(&id, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &friendshipState{ID: uuid.MustParse(id), Status: status}, nil
}

func contactStateTx(ctx context.Context, tx pgx.Tx, left, right uuid.UUID) (bool, bool, error) {
	var accepted, blocked bool
	err := tx.QueryRow(ctx, `
SELECT EXISTS (
         SELECT 1 FROM friendships
          WHERE status = 'accepted'
            AND ((requester_id = $1 AND addressee_id = $2)
              OR (requester_id = $2 AND addressee_id = $1))
       ),
       EXISTS (
         SELECT 1 FROM user_blocks
          WHERE revoked_at IS NULL
            AND ((blocker_id = $1 AND blocked_id = $2)
              OR (blocker_id = $2 AND blocked_id = $1))
       )`, left.String(), right.String()).Scan(&accepted, &blocked)
	return accepted, blocked, err
}

func ensureNotBlockedTx(ctx context.Context, tx pgx.Tx, left, right uuid.UUID) error {
	var blocked bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM user_blocks
   WHERE revoked_at IS NULL
     AND ((blocker_id = $1 AND blocked_id = $2)
       OR (blocker_id = $2 AND blocked_id = $1))
)`, left.String(), right.String()).Scan(&blocked); err != nil {
		return err
	}
	if blocked {
		return ErrBlocked
	}
	return nil
}

func validateBody(body string) error {
	trimmed := strings.TrimSpace(body)
	if runeLength(trimmed) < 1 || runeLength(body) > maxMessageRunes {
		return fmt.Errorf("%w: body", ErrInvalidRequest)
	}
	if strings.ContainsRune(body, 0) {
		return fmt.Errorf("%w: body", ErrInvalidRequest)
	}
	return nil
}

func runeLength(value string) int {
	return utf8.RuneCountInString(value)
}

func (s *Service) writeAudit(ctx context.Context, tx pgx.Tx, actorID uuid.UUID, action audit.Action,
	resourceType string, resourceID *uuid.UUID, requestID string, ipHash []byte, metadata map[string]any) error {
	if s.audit == nil {
		return nil
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	return s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(actorID),
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     metadata,
	})
}
