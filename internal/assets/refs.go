// SPDX-License-Identifier: Apache-2.0
package assets

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/worldcap"
)

// CopyObjectRefs inserts child references only for the assets that the
// assembled child state actually contains. It owns the object_asset_refs write
// so derivation cannot acquire media that was not inherited.
func CopyObjectRefs(ctx context.Context, tx pgx.Tx,
	parentID, childID, actorID uuid.UUID, refs []worldcap.AssetRef) error {
	for _, ref := range refs {
		query := `
INSERT INTO object_asset_refs (object_id, asset_id, created_by)
SELECT $1, r.asset_id, $2
  FROM object_asset_refs r
  JOIN assets a ON a.id = r.asset_id
 WHERE r.object_id = $3 AND r.asset_id = $4 AND r.revoked_at IS NULL`
		arguments := []any{childID.String(), actorID.String(), parentID.String(), ref.ID}
		if ref.ExpectedKind != "" {
			query += ` AND a.media_kind = $5`
			arguments = append(arguments, ref.ExpectedKind)
		}
		query += ` ON CONFLICT DO NOTHING`
		tag, err := tx.Exec(ctx, query, arguments...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrInvalidContent
		}
	}
	return nil
}

// CopyPublicationRefs copies only the asset ids the derived child state
// references from the publication's frozen set.
func CopyPublicationRefs(ctx context.Context, tx pgx.Tx,
	publicationID, childID, actorID uuid.UUID, refs []worldcap.AssetRef) error {
	for _, ref := range refs {
		query := `
INSERT INTO object_asset_refs (object_id, asset_id, created_by)
SELECT $1, r.asset_id, $2
  FROM publication_asset_refs r
  JOIN assets a ON a.id = r.asset_id
 WHERE r.publication_id = $3 AND r.asset_id = $4`
		arguments := []any{childID.String(), actorID.String(), publicationID.String(), ref.ID}
		if ref.ExpectedKind != "" {
			query += ` AND a.media_kind = $5`
			arguments = append(arguments, ref.ExpectedKind)
		}
		query += ` ON CONFLICT DO NOTHING`
		tag, err := tx.Exec(ctx, query, arguments...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("%w: snapshot asset", ErrInvalidContent)
		}
	}
	return nil
}

// ObjectRefsFromState extracts asset references from one persisted open-world
// state. It is exported so the object layer can validate before it writes.
func ObjectRefsFromState(state []byte) ([]worldcap.AssetRef, error) {
	return stateAssetRefs(state)
}
