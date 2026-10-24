-- SPDX-License-Identifier: Apache-2.0
-- +goose Up
-- Runtime role with the least privilege required by D04 §3.2 item 10:
-- append-only tables receive SELECT/INSERT only; no table receives DELETE.
-- The role has no LOGIN; the application connects as the migration owner and
-- issues SET ROLE for every pooled connection.

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'derive_world_app') THEN
        CREATE ROLE derive_world_app NOLOGIN;
    END IF;
END
$$;
-- +goose StatementEnd

GRANT USAGE ON SCHEMA public TO derive_world_app;

GRANT SELECT, INSERT, UPDATE ON
    users,
    user_sessions,
    api_tokens,
    personal_ai_profiles,
    private_ai_contexts,
    ai_conversations,
    ai_messages,
    living_objects,
    object_memberships,
    object_action_receipts,
    invitations,
    delegation_grants,
    derivation_edges,
    capability_definitions,
    capability_invocations,
    model_invocations,
    confirmations,
    object_proposals,
    idempotency_keys
TO derive_world_app;

-- Append-only and registry tables.
GRANT SELECT, INSERT ON object_events TO derive_world_app;
GRANT SELECT, INSERT ON audit_events TO derive_world_app;
GRANT SELECT ON audit_actions TO derive_world_app;

-- object_events.seq is a bigserial.
GRANT USAGE, SELECT ON SEQUENCE object_events_seq_seq TO derive_world_app;

-- Only a superuser can grant the role to the connecting user. Self-hosters
-- running migrations as a non-superuser owner must grant it explicitly; the
-- self-hosting guide documents this requirement.
-- +goose StatementBegin
DO $$
BEGIN
    IF (SELECT rolsuper FROM pg_roles WHERE rolname = current_user) THEN
        EXECUTE format('GRANT derive_world_app TO %I', current_user);
    END IF;
END
$$;
-- +goose StatementEnd

UPDATE app_meta
   SET value = '3', updated_at = now()
 WHERE key = 'schema_version';
