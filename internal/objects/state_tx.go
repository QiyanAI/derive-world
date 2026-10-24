// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// StateMutation is the shared result of applying one whitelisted state plan
// inside an object-row transaction. Both the human action endpoint and the
// human proposal-apply endpoint use this single implementation; neither
// duplicates G02 state mutation logic.
type StateMutation struct {
	NewState           []byte
	NewShared          []byte
	NewVersion         int
	VoteOption         string
	PreviousVoteOption string
}

// applyStateMutationTx assumes the caller has already locked the object row,
// authorized the actor, and checked the expected version. It updates the
// object row conditionally and maintains the vote receipt.
func applyStateMutationTx(
	ctx context.Context,
	tx pgx.Tx,
	actorID, objectID uuid.UUID,
	objectType string,
	currentVersion int,
	stateRaw, sharedRaw []byte,
	plan *ActionPlan,
) (*StateMutation, error) {
	previousVote := ""
	if plan.Action == "cast_vote" {
		err := tx.QueryRow(ctx, `
SELECT payload->>'option_id' FROM object_action_receipts
 WHERE object_id = $1 AND user_id = $2 AND action_key = 'cast_vote'`,
			objectID.String(), actorID.String()).Scan(&previousVote)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	newState, newShared, voteOption, err := ApplyStateAction(objectType, stateRaw, sharedRaw, plan, previousVote)
	if err != nil {
		return nil, err
	}
	newVersion := currentVersion + 1
	tag, err := tx.Exec(ctx, `
UPDATE living_objects
   SET state = $2::jsonb, shared_context = $3::jsonb, version = version + 1, updated_at = GREATEST(now(), updated_at, created_at)
 WHERE id = $1 AND version = $4 AND status = 'active'`,
		objectID.String(), string(newState), string(newShared), currentVersion)
	if err != nil {
		return nil, mapConstraintError(err)
	}
	if tag.RowsAffected() != 1 {
		return nil, ErrVersionConflict
	}
	if plan.Action == "cast_vote" {
		if _, err := tx.Exec(ctx, `
INSERT INTO object_action_receipts (object_id, user_id, action_key, payload)
VALUES ($1,$2,'cast_vote',$3::jsonb)
ON CONFLICT (object_id, user_id, action_key)
DO UPDATE SET payload = EXCLUDED.payload, updated_at = GREATEST(now(), object_action_receipts.updated_at)`,
			objectID.String(), actorID.String(),
			fmt.Sprintf(`{"option_id":%q}`, voteOption)); err != nil {
			return nil, err
		}
	}
	return &StateMutation{
		NewState: newState, NewShared: newShared, NewVersion: newVersion,
		VoteOption: voteOption, PreviousVoteOption: previousVote,
	}, nil
}
