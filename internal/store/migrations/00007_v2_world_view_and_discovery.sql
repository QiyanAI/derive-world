-- SPDX-License-Identifier: Apache-2.0
-- +goose Up
-- V2 experience additions. Both are additive and backward compatible:
--
-- 1. user_world_layouts stores only one user's private view of the spatial
--    World Stage. It is keyed by (user, object) and never writes object state,
--    versions, ownership, membership, or derivation lineage.
--
-- 2. users.discoverable lets an account opt out of authenticated account
--    discovery. Search remains query-only, authentication-only, rate-limited,
--    and never exposes email or an unfiltered directory.
--
-- The runtime role receives only the privileges this feature needs.

ALTER TABLE users
    ADD COLUMN discoverable boolean NOT NULL DEFAULT true;

CREATE TABLE user_world_layouts (
    user_id    uuid             NOT NULL,
    object_id  uuid             NOT NULL,
    x          double precision NOT NULL,
    y          double precision NOT NULL,
    updated_at timestamptz      NOT NULL DEFAULT now(),
    CONSTRAINT user_world_layouts_pkey PRIMARY KEY (user_id, object_id),
    CONSTRAINT user_world_layouts_user_fk
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT user_world_layouts_object_fk
        FOREIGN KEY (object_id) REFERENCES living_objects (id) ON DELETE RESTRICT,
    CONSTRAINT user_world_layouts_bounds CHECK (
        x >= -10000 AND x <= 10000
        AND y >= -10000 AND y <= 10000
    )
);

CREATE INDEX user_world_layouts_user_idx
    ON user_world_layouts (user_id, updated_at DESC);

-- A discoverability change is a privacy-relevant account action and belongs in
-- the append-only audit log. This expands the frozen G00 registry by one
-- explicit, documented action; no existing action is altered.
INSERT INTO audit_actions (action, description)
VALUES ('social.discoverability_update', 'A user changed account discoverability');

GRANT SELECT, INSERT, UPDATE ON user_world_layouts TO derive_world_app;

UPDATE app_meta
   SET value = '7', updated_at = now()
 WHERE key = 'schema_version';
