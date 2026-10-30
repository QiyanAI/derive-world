// SPDX-License-Identifier: Apache-2.0
package personalai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/audit"
)

// Conversation is a private AI conversation. object_id is optional.
type Conversation struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	ObjectID  *uuid.UUID `json:"object_id"`
	Title     string     `json:"title"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at"`
}

// Message is one private message. Assistant messages carry parse_status and
// an optional server-authored notice explaining a rejected or unreadable turn.
type Message struct {
	ID             uuid.UUID `json:"id"`
	ConversationID uuid.UUID `json:"conversation_id"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	ModelMode      string    `json:"model_mode"`
	ParseStatus    *string   `json:"parse_status"`
	Notice         *string   `json:"notice,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// ConversationDetail is the owner-only conversation with messages.
type ConversationDetail struct {
	Conversation Conversation `json:"conversation"`
	Messages     []Message    `json:"messages"`
}

// ConversationCursor is the keyset cursor for private conversation lists.
type ConversationCursor struct {
	UpdatedAt time.Time
	ID        uuid.UUID
}

// CreateConversationInput is the strict conversation creation body.
type CreateConversationInput struct {
	Title    string     `json:"title"`
	ObjectID *uuid.UUID `json:"object_id"`
}

// CreateConversation writes the conversation, audit, and idempotency response
// in one transaction.
func (s *Service) CreateConversation(
	ctx context.Context,
	userID uuid.UUID,
	input CreateConversationInput,
	route, idemKey string,
	leaseUntil time.Time,
	requestID string,
	ipHash []byte,
) (*Conversation, error) {
	title := strings.TrimSpace(input.Title)
	if count := utf8.RuneCountInString(title); count < 1 || count > 120 {
		return nil, fmt.Errorf("%w: title", ErrInvalidRequest)
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	conversationID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	var objectArg any
	if input.ObjectID != nil {
		objectArg = input.ObjectID.String()
	}
	var conversation Conversation
	err = tx.QueryRow(ctx, `
INSERT INTO ai_conversations (id, user_id, object_id, title)
VALUES ($1,$2,$3,$4)
RETURNING id, user_id, object_id, title, created_at, updated_at, deleted_at`,
		conversationID.String(), userID.String(), objectArg, title).Scan(
		&conversation.ID, &conversation.UserID, &conversation.ObjectID,
		&conversation.Title, &conversation.CreatedAt, &conversation.UpdatedAt,
		&conversation.DeletedAt)
	if err != nil {
		return nil, err
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(userID),
		Action:       audit.ActionAIConversationCreate,
		ResourceType: "ai_conversation",
		ResourceID:   &conversationID,
		Decision:     "allow",
		RequestID:    safeRequestID(requestID),
		IPHash:       ipHash,
		Metadata:     map[string]any{"object_bound": input.ObjectID != nil},
	}); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(conversation)
	if err != nil {
		return nil, err
	}
	if err := s.idem.Complete(ctx, tx, userID, route, idemKey, leaseUntil, 201, encoded); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &conversation, nil
}

// ListConversations returns only the caller's non-deleted conversations.
func (s *Service) ListConversations(ctx context.Context, userID uuid.UUID, cursor *ConversationCursor, limit int) ([]Conversation, *ConversationCursor, error) {
	query := `
SELECT id, user_id, object_id, title, created_at, updated_at, deleted_at
  FROM ai_conversations
 WHERE user_id = $1 AND deleted_at IS NULL`
	args := []any{userID.String()}
	if cursor != nil {
		query += fmt.Sprintf(" AND (updated_at, id) < ($%d::timestamptz, $%d::uuid)", len(args)+1, len(args)+2)
		args = append(args, cursor.UpdatedAt, cursor.ID.String())
	}
	query += fmt.Sprintf(" ORDER BY updated_at DESC, id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit+1)
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	items := make([]Conversation, 0, limit)
	var last Conversation
	count := 0
	for rows.Next() {
		var item Conversation
		if err := rows.Scan(&item.ID, &item.UserID, &item.ObjectID, &item.Title,
			&item.CreatedAt, &item.UpdatedAt, &item.DeletedAt); err != nil {
			return nil, nil, err
		}
		count++
		if count <= limit {
			last = item
			items = append(items, item)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if count > limit {
		return items, &ConversationCursor{UpdatedAt: last.UpdatedAt, ID: last.ID}, nil
	}
	return items, nil, nil
}

// ConversationMeta returns only the owner's conversation row.
func (s *Service) ConversationMeta(ctx context.Context, userID, conversationID uuid.UUID) (*Conversation, error) {
	return loadConversation(ctx, s.db, userID, conversationID)
}

// GetConversation returns the owner's conversation and messages. A foreign or
// deleted conversation is indistinguishable from a missing one.
func (s *Service) GetConversation(ctx context.Context, userID, conversationID uuid.UUID) (*ConversationDetail, error) {
	conversation, err := loadConversation(ctx, s.db, userID, conversationID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
SELECT id, conversation_id, role, content, model_mode, parse_status, notice, created_at
  FROM ai_messages
 WHERE conversation_id = $1
 ORDER BY created_at ASC, id ASC`, conversationID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := make([]Message, 0)
	for rows.Next() {
		var item Message
		if err := rows.Scan(&item.ID, &item.ConversationID, &item.Role,
			&item.Content, &item.ModelMode, &item.ParseStatus, &item.Notice,
			&item.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &ConversationDetail{Conversation: *conversation, Messages: messages}, nil
}

// ExportConversations returns all non-deleted private conversations with
// messages for the caller's data export. It never crosses user boundaries.
func (s *Service) ExportConversations(ctx context.Context, userID uuid.UUID) ([]ConversationDetail, error) {
	rows, err := s.db.Query(ctx, `
SELECT id, user_id, object_id, title, created_at, updated_at, deleted_at
  FROM ai_conversations
 WHERE user_id = $1 AND deleted_at IS NULL
 ORDER BY created_at ASC, id ASC`, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	conversations := make([]Conversation, 0)
	for rows.Next() {
		var item Conversation
		if err := rows.Scan(&item.ID, &item.UserID, &item.ObjectID, &item.Title,
			&item.CreatedAt, &item.UpdatedAt, &item.DeletedAt); err != nil {
			return nil, err
		}
		conversations = append(conversations, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ConversationDetail, 0, len(conversations))
	for _, conversation := range conversations {
		detail, err := s.GetConversation(ctx, userID, conversation.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, *detail)
	}
	return out, nil
}

// DeleteConversation soft deletes the conversation and audits the write.
func (s *Service) DeleteConversation(ctx context.Context, userID, conversationID uuid.UUID, requestID string, ipHash []byte) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
UPDATE ai_conversations SET deleted_at = GREATEST(now(), created_at), updated_at = GREATEST(now(), updated_at)
 WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`,
		conversationID.String(), userID.String())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(userID),
		Action:       audit.ActionAIConversationDelete,
		ResourceType: "ai_conversation",
		ResourceID:   &conversationID,
		Decision:     "allow",
		RequestID:    safeRequestID(requestID),
		IPHash:       ipHash,
		Metadata:     map[string]any{},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type conversationQueryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func loadConversation(ctx context.Context, q conversationQueryer, userID, conversationID uuid.UUID) (*Conversation, error) {
	var conversation Conversation
	err := q.QueryRow(ctx, `
SELECT id, user_id, object_id, title, created_at, updated_at, deleted_at
  FROM ai_conversations
 WHERE id = $1 AND user_id = $2`, conversationID.String(), userID.String()).Scan(
		&conversation.ID, &conversation.UserID, &conversation.ObjectID,
		&conversation.Title, &conversation.CreatedAt, &conversation.UpdatedAt,
		&conversation.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && conversation.DeletedAt != nil) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &conversation, nil
}
