// SPDX-License-Identifier: Apache-2.0
package model

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Execer is satisfied by pgx.Tx.
type Execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// Querier is satisfied by pgxpool.Pool.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Recorder owns model_invocations writes. It contains no business state and
// never stores prompts, messages, or keys.
type Recorder struct{}

// NewRecorder returns the model-owned invocation recorder.
func NewRecorder() *Recorder { return &Recorder{} }

// Invocation is one metered model call. OutputChars is zero for failures.
type Invocation struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	ConversationID *uuid.UUID
	Provider       string
	Model          string
	InputChars     int
	OutputChars    int
	DurationMS     int
	Status         string
	ErrorCode      string
}

// RecordTx writes the invocation row inside the caller's transaction.
func (r *Recorder) RecordTx(ctx context.Context, tx Execer, invocation Invocation) error {
	if invocation.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		invocation.ID = id
	}
	var errorCode any
	if invocation.ErrorCode != "" {
		errorCode = invocation.ErrorCode
	}
	var conversationID any
	if invocation.ConversationID != nil {
		conversationID = invocation.ConversationID.String()
	}
	_, err := tx.Exec(ctx, `
INSERT INTO model_invocations (
    id, user_id, conversation_id, provider, model, input_chars, output_chars,
    duration_ms, status, error_code
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		invocation.ID.String(), invocation.UserID.String(), conversationID,
		invocation.Provider, invocation.Model, invocation.InputChars,
		invocation.OutputChars, invocation.DurationMS, invocation.Status, errorCode)
	return err
}

// Reserve inserts a durable pre-call reservation. The frozen
// model_invocations status domain has no "reserved" value, so the row is
// written as failed with error_code "in_flight". DayUsage and GlobalDayUsage
// count every row regardless of status, so this row consumes the budget even
// if the process crashes before the call is recorded, the business
// transaction rolls back, or the final update fails. CompleteTx replaces it
// with the real outcome.
func (r *Recorder) Reserve(ctx context.Context, db Execer, invocation Invocation) error {
	if invocation.Status == "" {
		invocation.Status = "failed"
	}
	if invocation.Status == "succeeded" {
		invocation.ErrorCode = ""
	} else if invocation.ErrorCode == "" {
		invocation.ErrorCode = "in_flight"
	}
	return r.RecordTx(ctx, db, invocation)
}

// CompleteTx updates a pre-call reservation with its final outcome. The
// boolean is false when no reservation row matched; the caller may then
// insert a completed row. A failed update leaves the in-flight reservation
// counted, which is the fail-closed behaviour required for billable calls.
func (r *Recorder) CompleteTx(ctx context.Context, tx Execer, invocation Invocation) (bool, error) {
	if invocation.Status == "" {
		invocation.Status = "failed"
	}
	var errorCode any
	if invocation.Status == "succeeded" {
		invocation.ErrorCode = ""
	} else if invocation.ErrorCode == "" {
		invocation.ErrorCode = "unknown"
	}
	if invocation.ErrorCode != "" {
		errorCode = invocation.ErrorCode
	}
	tag, err := tx.Exec(ctx, `
UPDATE model_invocations
   SET status = $3,
       output_chars = $4,
       duration_ms = $5,
       model = CASE WHEN $6 = '' THEN model ELSE $6 END,
       error_code = $7
 WHERE id = $1 AND user_id = $2`,
		invocation.ID.String(), invocation.UserID.String(), invocation.Status,
		invocation.OutputChars, invocation.DurationMS, invocation.Model, errorCode)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// DayUsage returns UTC-day call and input-character totals.
func (r *Recorder) DayUsage(ctx context.Context, db Querier, userID uuid.UUID) (int, int, error) {
	var calls, chars int
	err := db.QueryRow(ctx, `
SELECT count(*), COALESCE(sum(input_chars), 0)
  FROM model_invocations
 WHERE user_id = $1
   AND created_at >= date_trunc('day', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'`,
		userID.String()).Scan(&calls, &chars)
	return calls, chars, err
}

// GlobalDayUsage returns UTC-day deployment-wide call and input-character
// totals for one provider mode. It seeds the hard global budget after a
// restart so the in-memory counter cannot reset a spent allowance.
func (r *Recorder) GlobalDayUsage(ctx context.Context, db Querier, provider string) (int, int, error) {
	var calls, chars int
	err := db.QueryRow(ctx, `
SELECT count(*), COALESCE(sum(input_chars), 0)
  FROM model_invocations
 WHERE provider = $1
   AND created_at >= date_trunc('day', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'`,
		provider).Scan(&calls, &chars)
	return calls, chars, err
}
