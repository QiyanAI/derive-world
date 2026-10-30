// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/assets"
	"github.com/QiyanAI/derive-world/internal/worldcap"
)

// openWorldAssetRefs returns the asset references declared by one persisted
// open-world state. It is the single collector shared by save-time checks,
// derivation and publication.
func openWorldAssetRefs(raw []byte) ([]worldcap.AssetRef, error) {
	var state struct {
		Experience *worldcap.Experience `json:"experience"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("%w: state", ErrInvalidRequest)
	}
	return worldcap.CollectAssetRefs(state.Experience), nil
}

// verifyOpenWorldAssetRefs proves that every asset the new state references is
// an active reference of this object and has the media kind the capability
// requires. It runs inside the same transaction that persists the state, after
// the object row lock.
func verifyOpenWorldAssetRefs(ctx context.Context, tx pgx.Tx, objectID uuid.UUID, raw []byte) error {
	refs, err := openWorldAssetRefs(raw)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		var kind string
		err := tx.QueryRow(ctx, `
SELECT a.media_kind
  FROM object_asset_refs r
  JOIN assets a ON a.id = r.asset_id
  JOIN living_objects o ON o.id = r.object_id
 WHERE r.object_id = $1 AND r.asset_id = $2 AND r.revoked_at IS NULL
   AND o.status = 'active'`, objectID.String(), ref.ID).Scan(&kind)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: asset reference", ErrInvalidRequest)
		}
		if err != nil {
			return err
		}
		if ref.ExpectedKind != "" && ref.ExpectedKind != kind {
			return fmt.Errorf("%w: asset kind", ErrInvalidRequest)
		}
	}
	return nil
}

// copyOpenWorldAssetRefs inserts child references only for the assets that the
// assembled child state actually contains. A derive that drops the experience
// (or a sub-surface) cannot retain access to media it did not inherit.
func copyOpenWorldAssetRefs(ctx context.Context, tx pgx.Tx,
	parentID, childID, actorID uuid.UUID, raw []byte) error {
	refs, err := openWorldAssetRefs(raw)
	if err != nil {
		return err
	}
	if err := assets.CopyObjectRefs(ctx, tx, parentID, childID, actorID, refs); err != nil {
		return fmt.Errorf("%w: asset reference", ErrInvalidRequest)
	}
	return nil
}

// ensureNoAssetRefs is used at creation: an object cannot reference an asset
// before it exists. The human uploads files into the world after creation.
func ensureNoAssetRefs(raw []byte) error {
	refs, err := openWorldAssetRefs(raw)
	if err != nil {
		return err
	}
	if len(refs) > 0 {
		return fmt.Errorf("%w: assets are uploaded after creation", ErrInvalidRequest)
	}
	return nil
}
