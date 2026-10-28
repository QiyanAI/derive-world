-- SPDX-License-Identifier: Apache-2.0
-- +goose Up
-- World assets: bounded, permission-checked media that a world can display.
--
-- The 64 KiB object state never stores file bytes. It stores short asset ids;
-- the bytes live in the configured asset directory and this table owns only
-- metadata. Every read is authorized against an active object reference (or an
-- active publication reference), so an asset id alone grants nothing.
--
-- This migration is append-only. It adds three tables, two audit actions, and
-- least-privilege grants; no existing row, table, or action is rewritten.

CREATE TABLE assets (
    id            uuid        PRIMARY KEY,
    checksum      bytea       NOT NULL,
    media_kind    text        NOT NULL,
    mime          text        NOT NULL,
    byte_size     bigint      NOT NULL,
    width         integer,
    height        integer,
    original_name text        NOT NULL DEFAULT '',
    created_by    uuid        NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT assets_creator_fk
        FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT assets_checksum_len CHECK (octet_length(checksum) = 32),
    CONSTRAINT assets_kind_domain
        CHECK (media_kind IN ('image', 'video', 'audio', 'model')),
    CONSTRAINT assets_mime_len CHECK (char_length(mime) BETWEEN 1 AND 80),
    -- 40 MiB is the hard per-file ceiling. The application applies smaller
    -- per-kind limits and a world/user quota before this row is written.
    CONSTRAINT assets_size_range CHECK (byte_size > 0 AND byte_size <= 41943040),
    CONSTRAINT assets_dimensions CHECK (
        (media_kind = 'image'
            AND width IS NOT NULL AND height IS NOT NULL
            AND width > 0 AND height > 0
            AND width <= 8192 AND height <= 8192)
     OR (media_kind <> 'image' AND width IS NULL AND height IS NULL)
    ),
    CONSTRAINT assets_name_len CHECK (char_length(original_name) <= 120)
);

CREATE INDEX assets_checksum_idx ON assets (checksum);
CREATE INDEX assets_creator_day_idx ON assets (created_by, created_at DESC);

-- One world references one asset at most once. Revocation removes an asset
-- from a world without touching other worlds that derived from it.
CREATE TABLE object_asset_refs (
    object_id  uuid        NOT NULL,
    asset_id   uuid        NOT NULL,
    created_by uuid        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    CONSTRAINT object_asset_refs_pkey PRIMARY KEY (object_id, asset_id),
    CONSTRAINT object_asset_refs_object_fk
        FOREIGN KEY (object_id) REFERENCES living_objects (id) ON DELETE RESTRICT,
    CONSTRAINT object_asset_refs_asset_fk
        FOREIGN KEY (asset_id) REFERENCES assets (id) ON DELETE RESTRICT,
    CONSTRAINT object_asset_refs_creator_fk
        FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT object_asset_refs_revoke_order CHECK (
        revoked_at IS NULL OR revoked_at >= created_at
    )
);

CREATE INDEX object_asset_refs_asset_idx ON object_asset_refs (asset_id);
CREATE INDEX object_asset_refs_active_idx
    ON object_asset_refs (object_id, created_at DESC) WHERE revoked_at IS NULL;

-- A publication freezes exactly the assets referenced by its snapshot. Public
-- readers use these rows; a later private change or revocation cannot widen or
-- silently narrow the public asset set.
CREATE TABLE publication_asset_refs (
    publication_id uuid        NOT NULL,
    asset_id       uuid        NOT NULL,
    CONSTRAINT publication_asset_refs_pkey PRIMARY KEY (publication_id, asset_id),
    CONSTRAINT publication_asset_refs_publication_fk
        FOREIGN KEY (publication_id) REFERENCES publications (id) ON DELETE RESTRICT,
    CONSTRAINT publication_asset_refs_asset_fk
        FOREIGN KEY (asset_id) REFERENCES assets (id) ON DELETE RESTRICT
);

CREATE INDEX publication_asset_refs_asset_idx
    ON publication_asset_refs (asset_id);

-- Asset changes are security- and privacy-relevant. Both actions are
-- append-only audit records; file bytes and original names never enter audit
-- metadata.
INSERT INTO audit_actions (action, description) VALUES
    ('asset.upload', 'A media asset was uploaded to a world'),
    ('asset.remove', 'A media asset was removed from a world')
ON CONFLICT (action) DO NOTHING;

-- Runtime role grants: append-only references, soft revocation only.
GRANT SELECT, INSERT, UPDATE ON assets TO derive_world_app;
GRANT SELECT, INSERT, UPDATE ON object_asset_refs TO derive_world_app;
GRANT SELECT, INSERT ON publication_asset_refs TO derive_world_app;

UPDATE app_meta
   SET value = '9', updated_at = now()
 WHERE key = 'schema_version';
