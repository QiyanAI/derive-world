// SPDX-License-Identifier: Apache-2.0
package assets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/worldcap"
)

// Upload validates, stores and records one asset. The caller must have
// authenticated the actor; authorization is re-checked inside the write
// transaction so a concurrent membership revocation cannot be bypassed.
func (s *Service) Upload(ctx context.Context, actorID, objectID uuid.UUID, kind Kind,
	name string, body io.Reader, requestID string, ipHash []byte) (*Asset, error) {
	limit, ok := LimitFor(kind)
	if !ok {
		return nil, ErrInvalidKind
	}
	if body == nil {
		return nil, ErrInvalidContent
	}
	// Authorize before touching the request body. The transaction repeats this
	// check under a row lock, but an unauthorized caller must not be able to
	// force disk work.
	if err := s.authorizeUploader(ctx, actorID, objectID); err != nil {
		return nil, err
	}

	temp, err := os.CreateTemp(s.tempRoot, "upload-*")
	if err != nil {
		return nil, ErrUnavailable
	}
	tempPath := temp.Name()
	keepTemp := false
	defer func() {
		_ = temp.Close()
		if !keepTemp {
			_ = os.Remove(tempPath)
		}
	}()

	// One extra byte is read so an oversized body is detected instead of
	// silently truncated.
	written, err := io.Copy(temp, io.LimitReader(body, limit+1))
	if err != nil {
		return nil, ErrInvalidContent
	}
	if written == 0 {
		return nil, ErrInvalidContent
	}
	if written > limit {
		return nil, ErrTooLarge
	}
	if err := temp.Sync(); err != nil {
		return nil, ErrUnavailable
	}
	if err := temp.Close(); err != nil {
		return nil, ErrUnavailable
	}

	inspection, normalizedPath, err := inspectFile(ctx, tempPath, kind, limit, s.imageGate)
	if err != nil {
		return nil, err
	}
	if normalizedPath != "" {
		_ = os.Remove(tempPath)
		tempPath = normalizedPath
		inspection.Size, err = fileSize(tempPath)
		if err != nil {
			return nil, err
		}
	}
	checksum, err := checksumFile(tempPath)
	if err != nil {
		return nil, err
	}
	blobPath := s.blobPath(checksum)
	asset := &Asset{
		Kind:      kind,
		MIME:      inspection.MIME,
		ByteSize:  inspection.Size,
		Width:     inspection.Width,
		Height:    inspection.Height,
		Name:      sanitizeName(name),
		Checksum:  checksum,
		CreatedAt: time.Now().UTC(),
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	permissions, err := assetPermissions(ctx, tx, actorID, objectID, true)
	if err != nil {
		return nil, err
	}
	if !hasPermission(permissions, "contribute") {
		return nil, ErrForbidden
	}
	if err := lockAssetWrite(ctx, tx, actorID, objectID); err != nil {
		return nil, err
	}

	// A repeated upload of identical bytes into the same world reuses the
	// existing asset (and revives a removed reference) instead of charging the
	// quota twice.
	var (
		existingID      string
		existingName    string
		existingCreated time.Time
		revokedAt       *time.Time
	)
	err = tx.QueryRow(ctx, `
SELECT a.id, a.original_name, a.created_at, r.revoked_at
  FROM assets a
  JOIN object_asset_refs r ON r.asset_id = a.id
 WHERE r.object_id = $1 AND a.checksum = $2 AND a.media_kind = $3
 ORDER BY a.created_at ASC
 LIMIT 1`, objectID.String(), checksum, string(kind)).Scan(
		&existingID, &existingName, &existingCreated, &revokedAt)
	if err == nil {
		if revokedAt != nil {
			if err := s.checkQuota(ctx, tx, actorID, objectID, inspection.Size); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, `
UPDATE object_asset_refs
   SET revoked_at = NULL
 WHERE object_id = $1 AND asset_id = $2`, objectID.String(), existingID); err != nil {
				return nil, err
			}
		}
		if err := s.storeBlob(tempPath, blobPath); err != nil {
			return nil, err
		}
		keepTemp = true
		if err := s.writeUploadAudit(ctx, tx, actorID, objectID, existingID, kind,
			inspection.Size, requestID, ipHash); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		asset.ID = existingID
		asset.Name = existingName
		asset.CreatedAt = existingCreated
		return asset, nil
	}
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}

	if err := s.checkQuota(ctx, tx, actorID, objectID, inspection.Size); err != nil {
		return nil, err
	}
	assetID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	asset.ID = assetID.String()
	if err := tx.QueryRow(ctx, `
INSERT INTO assets (
    id, checksum, media_kind, mime, byte_size, width, height,
    original_name, created_by, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,now())
RETURNING created_at`,
		asset.ID, checksum, string(kind), inspection.MIME, inspection.Size,
		nullInt(inspection.Width), nullInt(inspection.Height),
		asset.Name, actorID.String()).Scan(&asset.CreatedAt); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO object_asset_refs (object_id, asset_id, created_by)
VALUES ($1,$2,$3)`, objectID.String(), asset.ID, actorID.String()); err != nil {
		return nil, err
	}
	if err := s.storeBlob(tempPath, blobPath); err != nil {
		return nil, err
	}
	keepTemp = true
	if err := s.writeUploadAudit(ctx, tx, actorID, objectID, asset.ID, kind,
		inspection.Size, requestID, ipHash); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return asset, nil
}

// authorizeUploader is the pre-I/O membership check. It uses the configured
// pool rather than a transaction; the upload transaction re-checks it.
func (s *Service) authorizeUploader(ctx context.Context, actorID, objectID uuid.UUID) error {
	var permissions []string
	err := s.db.QueryRow(ctx, `
SELECT m.permissions
  FROM object_memberships m
  JOIN living_objects o ON o.id = m.object_id
 WHERE m.object_id = $1 AND m.user_id = $2 AND m.revoked_at IS NULL
   AND o.status = 'active'`, objectID.String(), actorID.String()).Scan(&permissions)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !hasPermission(permissions, "contribute") {
		return ErrForbidden
	}
	return nil
}

// lockAssetWrite serializes uploads per user and world across processes, so
// concurrent requests cannot both pass the quota and dedupe checks.
func lockAssetWrite(ctx context.Context, tx pgx.Tx, actorID, objectID uuid.UUID) error {
	for _, key := range []string{"asset-user:" + actorID.String(), "asset-object:" + objectID.String()} {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) checkQuota(ctx context.Context, tx pgx.Tx, actorID, objectID uuid.UUID, size int64) error {
	var worldBytes int64
	if err := tx.QueryRow(ctx, `
SELECT COALESCE(SUM(a.byte_size), 0)
  FROM object_asset_refs r
  JOIN assets a ON a.id = r.asset_id
 WHERE r.object_id = $1 AND r.revoked_at IS NULL`, objectID.String()).Scan(&worldBytes); err != nil {
		return err
	}
	if worldBytes+size > MaxWorldBytes {
		return ErrQuota
	}
	var count int
	if err := tx.QueryRow(ctx, `
SELECT count(*) FROM object_asset_refs
 WHERE object_id = $1 AND revoked_at IS NULL`, objectID.String()).Scan(&count); err != nil {
		return err
	}
	if count >= MaxAssetsPerWorld {
		return ErrQuota
	}
	var dailyBytes int64
	if err := tx.QueryRow(ctx, `
SELECT COALESCE(SUM(byte_size), 0)
  FROM assets
 WHERE created_by = $1 AND created_at >= date_trunc('day', now())`,
		actorID.String()).Scan(&dailyBytes); err != nil {
		return err
	}
	if dailyBytes+size > MaxUserDailyBytes {
		return ErrQuota
	}
	return nil
}

func (s *Service) writeUploadAudit(ctx context.Context, tx pgx.Tx, actorID, objectID uuid.UUID,
	assetID string, kind Kind, size int64, requestID string, ipHash []byte) error {
	if s.audit == nil {
		return nil
	}
	resourceID := objectID
	return s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(actorID),
		Action:       audit.ActionAssetUpload,
		ResourceType: "living_object",
		ResourceID:   &resourceID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata: map[string]any{
			"asset_id": assetID, "kind": string(kind), "byte_size": size,
		},
	})
}

// List returns active assets of one world in reverse creation order. The
// caller's active membership is part of the query, so a future caller cannot
// enumerate another world by passing its id.
func (s *Service) List(ctx context.Context, actorID, objectID uuid.UUID) ([]Asset, error) {
	rows, err := s.db.Query(ctx, `
SELECT a.id, a.media_kind, a.mime, a.byte_size, a.width, a.height,
       a.original_name, a.created_at
  FROM object_asset_refs r
  JOIN assets a ON a.id = r.asset_id
  JOIN object_memberships m ON m.object_id = r.object_id
                           AND m.user_id = $2 AND m.revoked_at IS NULL
  JOIN living_objects o ON o.id = r.object_id AND o.status = 'active'
 WHERE r.object_id = $1 AND r.revoked_at IS NULL
 ORDER BY r.created_at DESC`, objectID.String(), actorID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Asset, 0)
	for rows.Next() {
		var (
			asset  Asset
			kind   string
			width  *int
			height *int
		)
		if err := rows.Scan(&asset.ID, &kind, &asset.MIME, &asset.ByteSize,
			&width, &height, &asset.Name, &asset.CreatedAt); err != nil {
			return nil, err
		}
		asset.Kind = Kind(kind)
		if width != nil {
			asset.Width = *width
		}
		if height != nil {
			asset.Height = *height
		}
		items = append(items, asset)
	}
	return items, rows.Err()
}

// Open returns one asset for a world reader. The active membership, active
// object and active reference are all part of the same query.
func (s *Service) Open(ctx context.Context, actorID, objectID, assetID uuid.UUID) (*Asset, *os.File, error) {
	var (
		asset  Asset
		kind   string
		width  *int
		height *int
	)
	err := s.db.QueryRow(ctx, `
SELECT a.id, a.media_kind, a.mime, a.byte_size, a.width, a.height,
       a.original_name, a.created_at, a.checksum
  FROM object_asset_refs r
  JOIN assets a ON a.id = r.asset_id
  JOIN object_memberships m ON m.object_id = r.object_id
                           AND m.user_id = $3 AND m.revoked_at IS NULL
  JOIN living_objects o ON o.id = r.object_id
 WHERE r.object_id = $1 AND r.asset_id = $2 AND r.revoked_at IS NULL
   AND o.status = 'active'`, objectID.String(), assetID.String(), actorID.String()).Scan(
		&asset.ID, &kind, &asset.MIME, &asset.ByteSize, &width, &height,
		&asset.Name, &asset.CreatedAt, &asset.Checksum)
	if err == pgx.ErrNoRows {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	asset.Kind = Kind(kind)
	if width != nil {
		asset.Width = *width
	}
	if height != nil {
		asset.Height = *height
	}
	file, err := os.Open(s.blobPath(asset.Checksum))
	if err != nil {
		return nil, nil, ErrUnavailable
	}
	return &asset, file, nil
}

// PublicOpen returns an asset frozen into an active publication. It never
// consults the private object reference table.
func (s *Service) PublicOpen(ctx context.Context, publicationID, assetID uuid.UUID) (*Asset, *os.File, error) {
	var (
		asset  Asset
		kind   string
		width  *int
		height *int
	)
	err := s.db.QueryRow(ctx, `
SELECT a.id, a.media_kind, a.mime, a.byte_size, a.width, a.height,
       a.original_name, a.created_at, a.checksum
  FROM publication_asset_refs r
  JOIN publications p ON p.id = r.publication_id
  JOIN assets a ON a.id = r.asset_id
 WHERE r.publication_id = $1 AND r.asset_id = $2 AND p.status = 'active'`,
		publicationID.String(), assetID.String()).Scan(
		&asset.ID, &kind, &asset.MIME, &asset.ByteSize, &width, &height,
		&asset.Name, &asset.CreatedAt, &asset.Checksum)
	if err == pgx.ErrNoRows {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	asset.Kind = Kind(kind)
	if width != nil {
		asset.Width = *width
	}
	if height != nil {
		asset.Height = *height
	}
	file, err := os.Open(s.blobPath(asset.Checksum))
	if err != nil {
		return nil, nil, ErrUnavailable
	}
	return &asset, file, nil
}

// Remove revokes one asset reference from one world. The bytes and the asset
// metadata remain so other derived worlds and old versions are unaffected.
func (s *Service) Remove(ctx context.Context, actorID, objectID, assetID uuid.UUID,
	requestID string, ipHash []byte) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	permissions, err := assetPermissions(ctx, tx, actorID, objectID, true)
	if err != nil {
		return err
	}
	if !hasPermission(permissions, "manage") {
		return ErrForbidden
	}
	// A removal that would break the current world is rejected. The creator
	// must first remove the reference through a versioned state action.
	var (
		objectType string
		state      []byte
	)
	err = tx.QueryRow(ctx, `
SELECT object_type, state FROM living_objects
 WHERE id = $1 AND status = 'active' FOR SHARE`, objectID.String()).Scan(&objectType, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if objectType == "open_world" {
		refs, err := stateAssetRefs(state)
		if err != nil {
			return err
		}
		for _, ref := range refs {
			if ref.ID == assetID.String() {
				return ErrInUse
			}
		}
	}
	tag, err := tx.Exec(ctx, `
UPDATE object_asset_refs
   SET revoked_at = now()
 WHERE object_id = $1 AND asset_id = $2 AND revoked_at IS NULL`,
		objectID.String(), assetID.String())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		var revokedAt *time.Time
		err := tx.QueryRow(ctx, `
SELECT revoked_at FROM object_asset_refs
 WHERE object_id = $1 AND asset_id = $2`, objectID.String(), assetID.String()).Scan(&revokedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if revokedAt != nil {
			return nil
		}
		return ErrNotFound
	}
	if s.audit != nil {
		resourceID := objectID
		if err := s.audit.Write(ctx, tx, audit.Event{
			Actor:        audit.UserActor(actorID),
			Action:       audit.ActionAssetRemove,
			ResourceType: "living_object",
			ResourceID:   &resourceID,
			Decision:     "allow",
			RequestID:    requestID,
			IPHash:       ipHash,
			Metadata:     map[string]any{"asset_id": assetID.String()},
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// stateAssetRefs extracts the asset references declared by one open-world
// state. It is the shared interpretation used by save-time validation,
// publication freezing and derivation.
func stateAssetRefs(state []byte) ([]worldcap.AssetRef, error) {
	var envelope struct {
		Experience *worldcap.Experience `json:"experience"`
	}
	if err := json.Unmarshal(state, &envelope); err != nil {
		return nil, ErrInvalidContent
	}
	return worldcap.CollectAssetRefs(envelope.Experience), nil
}

// FreezePublicationAssets copies exactly the active assets that the published
// snapshot state references into the publication's immutable asset set. It
// verifies both existence and media kind in the same transaction.
func (s *Service) FreezePublicationAssets(ctx context.Context, tx pgx.Tx,
	objectID, publicationID uuid.UUID, state []byte) error {
	refs, err := stateAssetRefs(state)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		var kind string
		err := tx.QueryRow(ctx, `
SELECT a.media_kind
  FROM object_asset_refs r
  JOIN assets a ON a.id = r.asset_id
 WHERE r.object_id = $1 AND r.asset_id = $2 AND r.revoked_at IS NULL`,
			objectID.String(), ref.ID).Scan(&kind)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidContent
		}
		if err != nil {
			return err
		}
		if ref.ExpectedKind != "" && ref.ExpectedKind != kind {
			return ErrInvalidContent
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO publication_asset_refs (publication_id, asset_id)
VALUES ($1,$2) ON CONFLICT DO NOTHING`, publicationID.String(), ref.ID); err != nil {
			return err
		}
	}
	return nil
}

// AssetCreatorsForState returns the distinct users who uploaded assets that
// the published state references. They are unioned into publication consent
// contributors so media cannot be published without its uploader's consent.
func (s *Service) AssetCreatorsForState(ctx context.Context, tx pgx.Tx,
	objectID uuid.UUID, state []byte) ([]uuid.UUID, error) {
	refs, err := stateAssetRefs(state)
	if err != nil {
		return nil, err
	}
	creators := make([]uuid.UUID, 0)
	seen := map[uuid.UUID]bool{}
	for _, ref := range refs {
		var creator string
		query := `
SELECT a.created_by
  FROM object_asset_refs r
  JOIN assets a ON a.id = r.asset_id
 WHERE r.object_id = $1 AND r.asset_id = $2 AND r.revoked_at IS NULL`
		arguments := []any{objectID.String(), ref.ID}
		if ref.ExpectedKind != "" {
			query += ` AND a.media_kind = $3`
			arguments = append(arguments, ref.ExpectedKind)
		}
		if err := tx.QueryRow(ctx, query, arguments...).Scan(&creator); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrInvalidContent
			}
			return nil, err
		}
		parsed, err := uuid.Parse(creator)
		if err != nil {
			return nil, ErrInvalidContent
		}
		if !seen[parsed] {
			seen[parsed] = true
			creators = append(creators, parsed)
		}
	}
	return creators, nil
}

func assetPermissions(ctx context.Context, tx pgx.Tx, actorID, objectID uuid.UUID,
	lock bool) ([]string, error) {
	query := `
SELECT m.permissions
  FROM object_memberships m
  JOIN living_objects o ON o.id = m.object_id
 WHERE m.object_id = $1 AND m.user_id = $2 AND m.revoked_at IS NULL
   AND o.status = 'active'`
	if lock {
		query += ` FOR SHARE OF m`
	}
	var permissions []string
	err := tx.QueryRow(ctx, query, objectID.String(), actorID.String()).Scan(&permissions)
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	return permissions, err
}

func hasPermission(permissions []string, required string) bool {
	for _, permission := range permissions {
		if permission == required {
			return true
		}
	}
	return false
}

func (s *Service) blobPath(checksum []byte) string {
	hexChecksum := hex.EncodeToString(checksum)
	return filepath.Join(s.blobRoot, hexChecksum[:2], hexChecksum)
}

func (s *Service) storeBlob(source, destination string) error {
	if _, err := os.Stat(destination); err == nil {
		_ = os.Remove(source)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return ErrUnavailable
	}
	if err := os.Rename(source, destination); err != nil {
		if _, statErr := os.Stat(destination); statErr == nil {
			_ = os.Remove(source)
			return nil
		}
		return ErrUnavailable
	}
	return nil
}

func checksumFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { _ = file.Close() }()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return nil, ErrUnavailable
	}
	return hasher.Sum(nil), nil
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, ErrUnavailable
	}
	return info.Size(), nil
}

func nullInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}
