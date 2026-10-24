// SPDX-License-Identifier: Apache-2.0
package capability

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/audit"
)

const reconciliationGrace = 70 * time.Second

// reconcile implements the D06 §3.7.4 startup scan. It never turns a pending
// invocation into success; confirmation states are honored and every decision
// is audited.
func (s *Service) reconcile(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
SELECT ci.id, ci.created_at, c.status, c.expires_at
  FROM capability_invocations ci
  LEFT JOIN LATERAL (
      SELECT status, expires_at
        FROM confirmations
       WHERE resource_type = 'capability_invocation' AND resource_id = ci.id
       ORDER BY created_at DESC
       LIMIT 1
  ) c ON true
 WHERE ci.status = 'pending'
 ORDER BY ci.created_at`)
	if err != nil {
		return err
	}
	type pending struct {
		id         uuid.UUID
		createdAt  time.Time
		confStatus *string
		expiresAt  *time.Time
	}
	items := make([]pending, 0)
	for rows.Next() {
		var (
			idText string
			item   pending
		)
		if err := rows.Scan(&idText, &item.createdAt, &item.confStatus, &item.expiresAt); err != nil {
			rows.Close()
			return err
		}
		item.id = uuid.MustParse(idText)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	now := time.Now().UTC()
	for _, item := range items {
		var (
			toStatus  string
			errorCode string
			message   string
		)
		switch {
		case item.confStatus != nil && *item.confStatus == "pending" &&
			item.expiresAt != nil && item.expiresAt.After(now):
			continue
		case item.confStatus != nil && *item.confStatus == "pending":
			toStatus, errorCode = "denied", "confirmation_expired"
			message = "confirmation expired before execution"
		case item.confStatus != nil && *item.confStatus == "denied":
			toStatus, errorCode = "denied", "capability_denied"
			message = "confirmation was denied before execution"
		case item.confStatus != nil && *item.confStatus == "expired":
			toStatus, errorCode = "denied", "confirmation_expired"
			message = "confirmation expired before execution"
		case item.confStatus != nil && *item.confStatus == "approved":
			toStatus, errorCode = "unknown", "capability_unknown"
			message = "approved confirmation has no recorded execution"
		default:
			if now.Sub(item.createdAt) <= reconciliationGrace {
				continue
			}
			toStatus, errorCode = "unknown", "capability_unknown"
			message = "invocation was abandoned after dispatch"
		}
		tag, err := tx.Exec(ctx, `
UPDATE capability_invocations
   SET status = $2, error_code = $3, error_message = $4,
       duration_ms = 0, completed_at = now()
 WHERE id = $1 AND status = 'pending'`,
			item.id.String(), toStatus, errorCode, message)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			continue
		}
		if err := s.audit.Write(ctx, tx, audit.Event{
			Actor:        audit.SystemActor(),
			Action:       audit.ActionCapabilityReconciled,
			ResourceType: "capability_invocation",
			ResourceID:   &item.id,
			Decision:     "allow",
			RequestID:    "system-capability-reconcile",
			Metadata: map[string]any{
				"invocation_id": item.id.String(),
				"from_status":   "pending",
				"to_status":     toStatus,
			},
		}); err != nil {
			return err
		}
		if toStatus == "unknown" {
			if err := s.audit.Write(ctx, tx, audit.Event{
				Actor:        audit.SystemActor(),
				Action:       audit.ActionCapabilityUnknown,
				ResourceType: "capability_invocation",
				ResourceID:   &item.id,
				Decision:     "deny",
				ReasonCode:   errorCode,
				RequestID:    "system-capability-reconcile",
				Metadata: map[string]any{
					"invocation_id":   item.id.String(),
					"transport_phase": "startup_reconciliation",
				},
			}); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}
