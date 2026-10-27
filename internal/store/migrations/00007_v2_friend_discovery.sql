-- SPDX-License-Identifier: Apache-2.0
-- +goose Up
-- Opt-in discoverability: no account is ever searchable by nickname by default.
-- No existing user, object, personal AI or message is made public.
CREATE TABLE social_discovery_settings (
    user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE RESTRICT,
    discoverable boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX social_discovery_enabled_idx ON social_discovery_settings (user_id)
    WHERE discoverable = true;
GRANT SELECT, INSERT, UPDATE ON social_discovery_settings TO derive_world_app;
INSERT INTO audit_actions (action, description) VALUES
    ('social.discovery_preference', 'Caller changed own opt-in discoverability')
ON CONFLICT (action) DO NOTHING;
UPDATE app_meta SET value = '7', updated_at = now() WHERE key = 'schema_version';

-- +goose Down
-- Forward-only user privacy preferences: rollback is intentionally prohibited.
SELECT 1;
