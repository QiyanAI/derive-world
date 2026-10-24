// SPDX-License-Identifier: Apache-2.0

// Package trust owns delegation grants and confirmations. G02 only creates the
// default registration delegation; the full authorization kernel, invitations,
// and confirmation flows arrive in G03.
package trust

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DefaultDelegationConstraints is frozen by D05-02.
const DefaultDelegationConstraints = `{"require_confirmation":["derive.create","capability.invoke"],"max_ai_actions_per_hour":20}`

// InsertDefaultDelegationTx creates the global delegation installed at
// registration. It never contains invite or manage.
func InsertDefaultDelegationTx(ctx context.Context, tx pgx.Tx, userID, profileID, delegationID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
INSERT INTO delegation_grants (
    id, user_id, ai_profile_id, object_id, permissions, constraints
) VALUES ($1,$2,$3,NULL,$4,$5::jsonb)`,
		delegationID.String(), userID.String(), profileID.String(),
		[]string{"read", "contribute", "derive"}, DefaultDelegationConstraints)
	return err
}
