-- SPDX-License-Identifier: Apache-2.0
-- +goose Up
-- G02 business schema. This migration materializes the 22 frozen tables from
-- G00 contract §A.6.1 and D04 §3.3–§3.4. Tables are created in foreign-key
-- dependency order; no triggers, no cascades, and no physical delete paths.
-- G02 only exercises users, sessions, objects, memberships, events, receipts,
-- derivation edges, audit, and idempotency. The remaining frozen tables are
-- created now so later gates do not renumber the schema.

-- =====================================================================
-- users
-- =====================================================================
CREATE TABLE users (
    id            uuid        PRIMARY KEY,
    email         text        NOT NULL,
    handle        text        NOT NULL,
    display_name  text        NOT NULL,
    password_hash text        NOT NULL,
    status        text        NOT NULL DEFAULT 'active',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_email_normalized CHECK (
        email = lower(email)
        AND char_length(email) BETWEEN 3 AND 254
        AND position('@' IN email) > 1
    ),
    CONSTRAINT users_handle_format CHECK (handle ~ '^[a-z0-9][a-z0-9_-]{2,31}$'),
    CONSTRAINT users_display_name_len CHECK (char_length(display_name) BETWEEN 1 AND 80),
    CONSTRAINT users_status_domain CHECK (status IN ('active', 'disabled')),
    CONSTRAINT users_password_hash_format CHECK (password_hash ~ '^\$argon2id\$'),
    CONSTRAINT users_time_order CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX users_email_key   ON users (email);
CREATE UNIQUE INDEX users_handle_key  ON users (handle);

-- =====================================================================
-- personal_ai_profiles
-- =====================================================================
CREATE TABLE personal_ai_profiles (
    id         uuid        PRIMARY KEY,
    user_id    uuid        NOT NULL,
    name       text        NOT NULL,
    persona    text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT personal_ai_profiles_user_fk
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT personal_ai_profiles_user_unique UNIQUE (user_id),
    CONSTRAINT personal_ai_profiles_name_len CHECK (char_length(name) BETWEEN 1 AND 80),
    CONSTRAINT personal_ai_profiles_persona_len CHECK (char_length(persona) <= 2000),
    CONSTRAINT personal_ai_profiles_time_order CHECK (updated_at >= created_at)
);

-- =====================================================================
-- private_ai_contexts
-- =====================================================================
CREATE TABLE private_ai_contexts (
    user_id    uuid        PRIMARY KEY,
    content    text        NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT private_ai_contexts_user_fk
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT private_ai_contexts_size CHECK (octet_length(content) <= 8192)
);

-- =====================================================================
-- living_objects
-- =====================================================================
CREATE TABLE living_objects (
    id                    uuid        PRIMARY KEY,
    object_type           text        NOT NULL,
    schema_version        integer     NOT NULL DEFAULT 1,
    creator_id            uuid        NOT NULL,
    owner_id              uuid        NOT NULL,
    state                 jsonb       NOT NULL,
    version               integer     NOT NULL DEFAULT 1,
    shared_context        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    access_policy         jsonb       NOT NULL DEFAULT
        '{"visibility":"private","invite_enabled":true,"derive_link_enabled":true}'::jsonb,
    presentation_template text        NOT NULL,
    status                text        NOT NULL DEFAULT 'active',
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    deleted_at            timestamptz,
    CONSTRAINT living_objects_creator_fk
        FOREIGN KEY (creator_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT living_objects_owner_fk
        FOREIGN KEY (owner_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT living_objects_type_domain
        CHECK (object_type IN ('collaborative', 'interactive')),
    CONSTRAINT living_objects_template_domain
        CHECK (presentation_template IN ('collaborative_v1', 'interactive_v1')),
    CONSTRAINT living_objects_template_matches_type CHECK (
        (object_type = 'collaborative' AND presentation_template = 'collaborative_v1')
     OR (object_type = 'interactive'   AND presentation_template = 'interactive_v1')
    ),
    CONSTRAINT living_objects_schema_version CHECK (schema_version = 1),
    CONSTRAINT living_objects_version_positive CHECK (version >= 1),
    CONSTRAINT living_objects_state_object CHECK (jsonb_typeof(state) = 'object'),
    CONSTRAINT living_objects_state_size CHECK (octet_length(state::text) <= 65536),
    CONSTRAINT living_objects_shared_context_object CHECK (jsonb_typeof(shared_context) = 'object'),
    CONSTRAINT living_objects_shared_context_size CHECK (octet_length(shared_context::text) <= 8192),
    CONSTRAINT living_objects_access_policy_shape CHECK (
        jsonb_typeof(access_policy) = 'object'
        AND access_policy ? 'visibility'
        AND access_policy ? 'invite_enabled'
        AND access_policy ? 'derive_link_enabled'
        AND access_policy - ARRAY['visibility','invite_enabled','derive_link_enabled']::text[] = '{}'::jsonb
        AND access_policy ->> 'visibility' = 'private'
        AND jsonb_typeof(access_policy -> 'invite_enabled') = 'boolean'
        AND jsonb_typeof(access_policy -> 'derive_link_enabled') = 'boolean'
    ),
    CONSTRAINT living_objects_status_domain CHECK (status IN ('active', 'deleted')),
    CONSTRAINT living_objects_soft_delete_consistent CHECK (
        (status = 'active'  AND deleted_at IS NULL)
     OR (status = 'deleted' AND deleted_at IS NOT NULL)
    ),
    CONSTRAINT living_objects_time_order CHECK (
        updated_at >= created_at
        AND (deleted_at IS NULL OR deleted_at >= created_at)
    )
);

CREATE INDEX living_objects_owner_active_idx
    ON living_objects (owner_id, updated_at DESC) WHERE status = 'active';
CREATE INDEX living_objects_creator_idx
    ON living_objects (creator_id);
CREATE INDEX living_objects_recent_idx
    ON living_objects (updated_at DESC) WHERE status = 'active';

-- =====================================================================
-- object_memberships   —— 授权唯一真源
-- =====================================================================
CREATE TABLE object_memberships (
    object_id   uuid        NOT NULL,
    user_id     uuid        NOT NULL,
    permissions text[]      NOT NULL,
    granted_by  uuid,
    created_at  timestamptz NOT NULL DEFAULT now(),
    revoked_at  timestamptz,
    CONSTRAINT object_memberships_pkey PRIMARY KEY (object_id, user_id),
    CONSTRAINT object_memberships_object_fk
        FOREIGN KEY (object_id) REFERENCES living_objects (id) ON DELETE RESTRICT,
    CONSTRAINT object_memberships_user_fk
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT object_memberships_granted_by_fk
        FOREIGN KEY (granted_by) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT object_memberships_permissions_domain CHECK (
        cardinality(permissions) BETWEEN 1 AND 5
        AND permissions <@ ARRAY['read','contribute','derive','invite','manage']::text[]
    ),
    CONSTRAINT object_memberships_read_required CHECK (permissions @> ARRAY['read']::text[]),
    CONSTRAINT object_memberships_revoked_order CHECK (
        revoked_at IS NULL OR revoked_at >= created_at
    )
);

CREATE INDEX object_memberships_user_idx
    ON object_memberships (user_id) WHERE revoked_at IS NULL;

-- =====================================================================
-- delegation_grants
-- =====================================================================
CREATE TABLE delegation_grants (
    id            uuid        PRIMARY KEY,
    user_id       uuid        NOT NULL,
    ai_profile_id uuid        NOT NULL,
    object_id     uuid,
    permissions   text[]      NOT NULL,
    constraints   jsonb       NOT NULL DEFAULT '{}'::jsonb,
    expires_at    timestamptz,
    revoked_at    timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT delegation_grants_user_fk
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT delegation_grants_ai_profile_fk
        FOREIGN KEY (ai_profile_id) REFERENCES personal_ai_profiles (id) ON DELETE RESTRICT,
    CONSTRAINT delegation_grants_object_fk
        FOREIGN KEY (object_id) REFERENCES living_objects (id) ON DELETE RESTRICT,
    CONSTRAINT delegation_grants_permissions_domain CHECK (
        cardinality(permissions) BETWEEN 1 AND 4
        AND permissions <@ ARRAY['read','contribute','derive','invite']::text[]
    ),
    CONSTRAINT delegation_grants_read_required CHECK (permissions @> ARRAY['read']::text[]),
    CONSTRAINT delegation_grants_constraints_object CHECK (jsonb_typeof(constraints) = 'object'),
    CONSTRAINT delegation_grants_constraints_size CHECK (octet_length(constraints::text) <= 4096),
    CONSTRAINT delegation_grants_expiry_order CHECK (
        expires_at IS NULL OR expires_at > created_at
    ),
    CONSTRAINT delegation_grants_revoked_order CHECK (
        revoked_at IS NULL OR revoked_at >= created_at
    )
);

CREATE INDEX delegation_grants_active_idx
    ON delegation_grants (user_id, ai_profile_id) WHERE revoked_at IS NULL;
CREATE INDEX delegation_grants_object_idx
    ON delegation_grants (object_id) WHERE revoked_at IS NULL;

-- =====================================================================
-- object_events   —— 追加写，不可改删
-- =====================================================================
CREATE TABLE object_events (
    id                  uuid        PRIMARY KEY,
    seq                 bigserial   NOT NULL,
    object_id           uuid        NOT NULL,
    object_version      integer     NOT NULL,
    event_type          text        NOT NULL,
    actor_type          text        NOT NULL,
    actor_user_id       uuid,
    actor_ai_profile_id uuid,
    delegation_id       uuid,
    payload             jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT object_events_object_fk
        FOREIGN KEY (object_id) REFERENCES living_objects (id) ON DELETE RESTRICT,
    CONSTRAINT object_events_actor_user_fk
        FOREIGN KEY (actor_user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT object_events_actor_ai_fk
        FOREIGN KEY (actor_ai_profile_id) REFERENCES personal_ai_profiles (id) ON DELETE RESTRICT,
    CONSTRAINT object_events_delegation_fk
        FOREIGN KEY (delegation_id) REFERENCES delegation_grants (id) ON DELETE RESTRICT,
    CONSTRAINT object_events_type_domain CHECK (event_type IN (
        'object.created','object.state_changed','object.member_added','object.member_removed',
        'object.invitation_created','object.invitation_revoked','object.derived','object.deleted',
        'ai.proposal_created','ai.proposal_applied'
    )),
    CONSTRAINT object_events_actor_type_domain
        CHECK (actor_type IN ('user', 'personal_ai', 'system')),
    CONSTRAINT object_events_actor_shape CHECK (
        (actor_type = 'system'      AND actor_user_id IS NULL
                                    AND actor_ai_profile_id IS NULL
                                    AND delegation_id IS NULL)
     OR (actor_type = 'user'        AND actor_user_id IS NOT NULL
                                    AND actor_ai_profile_id IS NULL
                                    AND delegation_id IS NULL)
     OR (actor_type = 'personal_ai' AND actor_user_id IS NOT NULL
                                    AND actor_ai_profile_id IS NOT NULL
                                    AND delegation_id IS NOT NULL)
    ),
    CONSTRAINT object_events_version_domain CHECK (
        (event_type IN ('object.created', 'object.derived') AND object_version = 1)
     OR (event_type = 'object.state_changed' AND object_version >= 2)
     OR (event_type IN ('object.member_added','object.member_removed',
                        'object.invitation_created','object.invitation_revoked',
                        'object.deleted','ai.proposal_created','ai.proposal_applied')
         AND object_version >= 1)
    ),
    CONSTRAINT object_events_payload_object CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT object_events_payload_size CHECK (octet_length(payload::text) <= 98304)
);

CREATE UNIQUE INDEX object_events_seq_key        ON object_events (seq);
CREATE INDEX        object_events_object_seq_idx ON object_events (object_id, seq);
CREATE INDEX        object_events_version_idx    ON object_events (object_id, object_version);

-- =====================================================================
-- object_action_receipts   —— 每人每动作唯一凭据
-- =====================================================================
CREATE TABLE object_action_receipts (
    object_id  uuid        NOT NULL,
    user_id    uuid        NOT NULL,
    action_key text        NOT NULL,
    payload    jsonb       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT object_action_receipts_pkey PRIMARY KEY (object_id, user_id, action_key),
    CONSTRAINT object_action_receipts_object_fk
        FOREIGN KEY (object_id) REFERENCES living_objects (id) ON DELETE RESTRICT,
    CONSTRAINT object_action_receipts_user_fk
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT object_action_receipts_action_key_domain
        CHECK (action_key IN ('cast_vote')),
    CONSTRAINT object_action_receipts_payload_object
        CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT object_action_receipts_payload_size
        CHECK (octet_length(payload::text) <= 1024),
    CONSTRAINT object_action_receipts_time_order CHECK (updated_at >= created_at)
);

CREATE INDEX object_action_receipts_vote_idx
    ON object_action_receipts (object_id, action_key);

-- =====================================================================
-- invitations   —— 含 Derive Link，同一张表
-- =====================================================================
CREATE TABLE invitations (
    id          uuid        PRIMARY KEY,
    object_id   uuid        NOT NULL,
    created_by  uuid        NOT NULL,
    token_hash  bytea       NOT NULL,
    permissions text[]      NOT NULL,
    max_uses    integer     NOT NULL DEFAULT 1,
    use_count   integer     NOT NULL DEFAULT 0,
    expires_at  timestamptz NOT NULL,
    revoked_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT invitations_object_fk
        FOREIGN KEY (object_id) REFERENCES living_objects (id) ON DELETE RESTRICT,
    CONSTRAINT invitations_created_by_fk
        FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT invitations_token_hash_len CHECK (octet_length(token_hash) = 32),
    CONSTRAINT invitations_permissions_domain CHECK (
        cardinality(permissions) BETWEEN 1 AND 4
        AND permissions <@ ARRAY['read','contribute','derive','invite']::text[]
    ),
    CONSTRAINT invitations_read_required CHECK (permissions @> ARRAY['read']::text[]),
    CONSTRAINT invitations_max_uses_range CHECK (max_uses BETWEEN 1 AND 100),
    CONSTRAINT invitations_use_count_range CHECK (use_count BETWEEN 0 AND max_uses),
    CONSTRAINT invitations_expiry_window CHECK (
        expires_at > created_at
        AND expires_at <= created_at + interval '30 days'
    ),
    CONSTRAINT invitations_revoked_order CHECK (
        revoked_at IS NULL OR revoked_at >= created_at
    )
);

CREATE UNIQUE INDEX invitations_token_hash_key ON invitations (token_hash);
CREATE INDEX        invitations_object_active_idx
    ON invitations (object_id, created_at DESC) WHERE revoked_at IS NULL;

-- =====================================================================
-- derivation_edges   —— 来源唯一真源
-- =====================================================================
CREATE TABLE derivation_edges (
    child_object_id  uuid        PRIMARY KEY,
    parent_object_id uuid        NOT NULL,
    parent_version   integer     NOT NULL,
    inherited_fields text[]      NOT NULL,
    created_by       uuid        NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT derivation_edges_child_fk
        FOREIGN KEY (child_object_id) REFERENCES living_objects (id) ON DELETE RESTRICT,
    CONSTRAINT derivation_edges_parent_fk
        FOREIGN KEY (parent_object_id) REFERENCES living_objects (id) ON DELETE RESTRICT,
    CONSTRAINT derivation_edges_created_by_fk
        FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT derivation_edges_no_self CHECK (child_object_id <> parent_object_id),
    CONSTRAINT derivation_edges_parent_version CHECK (parent_version >= 1),
    CONSTRAINT derivation_edges_fields_nonempty CHECK (cardinality(inherited_fields) >= 1),
    CONSTRAINT derivation_edges_fields_domain CHECK (
        inherited_fields <@ ARRAY['summary','body','checklist','tags','prompt','options']::text[]
    )
);

CREATE INDEX derivation_edges_parent_idx
    ON derivation_edges (parent_object_id, created_at DESC);

-- =====================================================================
-- user_sessions
-- =====================================================================
CREATE TABLE user_sessions (
    id           uuid        PRIMARY KEY,
    user_id      uuid        NOT NULL,
    token_hash   bytea       NOT NULL,
    csrf_hash    bytea       NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz,
    CONSTRAINT user_sessions_user_fk
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT user_sessions_token_hash_len CHECK (octet_length(token_hash) = 32),
    CONSTRAINT user_sessions_csrf_hash_len  CHECK (octet_length(csrf_hash) = 32),
    CONSTRAINT user_sessions_expiry_order CHECK (expires_at > created_at),
    CONSTRAINT user_sessions_seen_order CHECK (
        last_seen_at >= created_at AND last_seen_at <= expires_at
    ),
    CONSTRAINT user_sessions_revoked_order CHECK (
        revoked_at IS NULL OR revoked_at >= created_at
    )
);

CREATE UNIQUE INDEX user_sessions_token_hash_key ON user_sessions (token_hash);
CREATE INDEX        user_sessions_user_idx       ON user_sessions (user_id, created_at DESC);
CREATE INDEX        user_sessions_expiry_idx     ON user_sessions (expires_at);

-- =====================================================================
-- api_tokens
-- =====================================================================
CREATE TABLE api_tokens (
    id           uuid        PRIMARY KEY,
    user_id      uuid        NOT NULL,
    name         text        NOT NULL,
    token_prefix text        NOT NULL,
    token_hash   bytea       NOT NULL,
    scopes       text[]      NOT NULL,
    expires_at   timestamptz,
    last_used_at timestamptz,
    revoked_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT api_tokens_user_fk
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT api_tokens_name_len CHECK (char_length(name) BETWEEN 1 AND 64),
    CONSTRAINT api_tokens_prefix_format CHECK (token_prefix ~ '^dw_pat_[A-Za-z0-9_-]{8}$'),
    CONSTRAINT api_tokens_hash_len CHECK (octet_length(token_hash) = 32),
    CONSTRAINT api_tokens_scopes_domain CHECK (
        cardinality(scopes) BETWEEN 1 AND 4
        AND scopes <@ ARRAY['objects:read','objects:write','derive:create','capabilities:invoke']::text[]
    ),
    CONSTRAINT api_tokens_expiry_window CHECK (
        expires_at IS NULL
        OR (expires_at > created_at AND expires_at <= created_at + interval '365 days')
    ),
    CONSTRAINT api_tokens_revoked_order CHECK (
        revoked_at IS NULL OR revoked_at >= created_at
    )
);

CREATE UNIQUE INDEX api_tokens_hash_key ON api_tokens (token_hash);
CREATE INDEX        api_tokens_user_idx ON api_tokens (user_id, created_at DESC);

-- =====================================================================
-- ai_conversations
-- =====================================================================
CREATE TABLE ai_conversations (
    id         uuid        PRIMARY KEY,
    user_id    uuid        NOT NULL,
    object_id  uuid,
    title      text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CONSTRAINT ai_conversations_user_fk
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT ai_conversations_object_fk
        FOREIGN KEY (object_id) REFERENCES living_objects (id) ON DELETE RESTRICT,
    CONSTRAINT ai_conversations_title_len CHECK (char_length(title) BETWEEN 1 AND 120),
    CONSTRAINT ai_conversations_time_order CHECK (updated_at >= created_at),
    CONSTRAINT ai_conversations_deleted_order CHECK (
        deleted_at IS NULL OR deleted_at >= created_at
    )
);

CREATE INDEX ai_conversations_user_idx
    ON ai_conversations (user_id, updated_at DESC) WHERE deleted_at IS NULL;

-- =====================================================================
-- ai_messages
-- =====================================================================
CREATE TABLE ai_messages (
    id              uuid        PRIMARY KEY,
    conversation_id uuid        NOT NULL,
    role            text        NOT NULL,
    content         text        NOT NULL,
    model_mode      text        NOT NULL,
    parse_status    text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ai_messages_conversation_fk
        FOREIGN KEY (conversation_id) REFERENCES ai_conversations (id) ON DELETE RESTRICT,
    CONSTRAINT ai_messages_role_domain CHECK (role IN ('user', 'assistant', 'system')),
    CONSTRAINT ai_messages_model_mode_domain CHECK (model_mode IN ('fixture', 'real')),
    CONSTRAINT ai_messages_parse_status_domain CHECK (
        parse_status IS NULL OR parse_status IN ('structured', 'fallback')
    ),
    CONSTRAINT ai_messages_parse_status_role CHECK (
        (role = 'assistant') = (parse_status IS NOT NULL)
    ),
    CONSTRAINT ai_messages_content_size CHECK (octet_length(content) <= 16384),
    CONSTRAINT ai_messages_content_not_blank CHECK (char_length(content) >= 1)
);

CREATE INDEX ai_messages_conversation_idx
    ON ai_messages (conversation_id, created_at, id);

-- =====================================================================
-- confirmations
-- =====================================================================
CREATE TABLE confirmations (
    id            uuid        PRIMARY KEY,
    user_id       uuid        NOT NULL,
    action        text        NOT NULL,
    resource_type text        NOT NULL,
    resource_id   uuid        NOT NULL,
    payload       jsonb       NOT NULL,
    payload_hash  bytea       NOT NULL,
    status        text        NOT NULL DEFAULT 'pending',
    expires_at    timestamptz NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    decided_at    timestamptz,
    CONSTRAINT confirmations_user_fk
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT confirmations_action_domain
        CHECK (action IN ('derive.create', 'invitation.create', 'capability.invoke')),
    CONSTRAINT confirmations_resource_type_domain
        CHECK (resource_type IN ('living_object', 'capability_invocation')),
    CONSTRAINT confirmations_status_domain
        CHECK (status IN ('pending', 'approved', 'denied', 'expired')),
    CONSTRAINT confirmations_payload_hash_len CHECK (octet_length(payload_hash) = 32),
    CONSTRAINT confirmations_payload_size CHECK (octet_length(payload::text) <= 16384),
    CONSTRAINT confirmations_expiry_window CHECK (
        expires_at > created_at AND expires_at <= created_at + interval '5 minutes'
    ),
    CONSTRAINT confirmations_decided_shape CHECK (
        (status = 'pending'  AND decided_at IS NULL)
     OR (status IN ('approved','denied') AND decided_at IS NOT NULL)
     OR (status = 'expired'  AND decided_at IS NULL)
    ),
    CONSTRAINT confirmations_decided_order CHECK (
        decided_at IS NULL OR decided_at >= created_at
    )
);

CREATE INDEX confirmations_user_pending_idx
    ON confirmations (user_id, expires_at) WHERE status = 'pending';
CREATE INDEX confirmations_resource_idx
    ON confirmations (resource_type, resource_id);

-- =====================================================================
-- capability_definitions   —— 启动时由 capabilities.yaml 同步
-- =====================================================================
CREATE TABLE capability_definitions (
    id                    text        PRIMARY KEY,
    contract_version      text        NOT NULL,
    provider_id           text        NOT NULL,
    description           text        NOT NULL,
    input_schema          jsonb       NOT NULL,
    output_schema         jsonb       NOT NULL,
    required_scopes       text[]      NOT NULL,
    supported_operations  text[]      NOT NULL,
    execution_constraints jsonb       NOT NULL,
    enabled               boolean     NOT NULL DEFAULT true,
    synced_at             timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT capability_definitions_id_domain CHECK (id IN (
        'object.read','object.context','object.propose_change','object.derive','content.structure'
    )),
    CONSTRAINT capability_definitions_provider_domain CHECK (provider_id IN (
        'trusted_core','reference.content-structure'
    )),
    CONSTRAINT capability_definitions_contract_version CHECK (contract_version = '1.0'),
    CONSTRAINT capability_definitions_description_len CHECK (char_length(description) BETWEEN 1 AND 500),
    CONSTRAINT capability_definitions_scopes_domain CHECK (
        required_scopes <@ ARRAY['read','contribute','derive','invite','manage']::text[]
    ),
    CONSTRAINT capability_definitions_operations_nonempty CHECK (
        cardinality(supported_operations) BETWEEN 1 AND 8
    ),
    CONSTRAINT capability_definitions_constraints_shape CHECK (
        jsonb_typeof(execution_constraints) = 'object'
        AND execution_constraints ? 'timeout_ms'
        AND execution_constraints ? 'max_input_bytes'
        AND execution_constraints ? 'max_output_bytes'
    ),
    CONSTRAINT capability_definitions_schemas_object CHECK (
        jsonb_typeof(input_schema) = 'object' AND jsonb_typeof(output_schema) = 'object'
    )
);

CREATE INDEX capability_definitions_enabled_idx
    ON capability_definitions (enabled) WHERE enabled = true;

-- =====================================================================
-- capability_invocations
-- =====================================================================
CREATE TABLE capability_invocations (
    id                  uuid        PRIMARY KEY,
    capability_id       text        NOT NULL,
    contract_version    text        NOT NULL,
    provider_id         text        NOT NULL,
    actor_user_id       uuid        NOT NULL,
    actor_ai_profile_id uuid,
    delegation_id       uuid,
    object_id           uuid,
    input_hash          bytea       NOT NULL,
    input_redacted      jsonb       NOT NULL,
    status              text        NOT NULL DEFAULT 'pending',
    output              jsonb,
    error_code          text,
    error_message       text,
    duration_ms         integer,
    created_at          timestamptz NOT NULL DEFAULT now(),
    completed_at        timestamptz,
    CONSTRAINT capability_invocations_capability_fk
        FOREIGN KEY (capability_id) REFERENCES capability_definitions (id) ON DELETE RESTRICT,
    CONSTRAINT capability_invocations_actor_fk
        FOREIGN KEY (actor_user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT capability_invocations_ai_fk
        FOREIGN KEY (actor_ai_profile_id) REFERENCES personal_ai_profiles (id) ON DELETE RESTRICT,
    CONSTRAINT capability_invocations_delegation_fk
        FOREIGN KEY (delegation_id) REFERENCES delegation_grants (id) ON DELETE RESTRICT,
    CONSTRAINT capability_invocations_object_fk
        FOREIGN KEY (object_id) REFERENCES living_objects (id) ON DELETE RESTRICT,
    CONSTRAINT capability_invocations_status_domain CHECK (
        status IN ('pending','succeeded','failed','denied','timeout','unknown')
    ),
    CONSTRAINT capability_invocations_input_hash_len CHECK (octet_length(input_hash) = 32),
    CONSTRAINT capability_invocations_input_size CHECK (octet_length(input_redacted::text) <= 2048),
    CONSTRAINT capability_invocations_duration CHECK (duration_ms IS NULL OR duration_ms >= 0),
    CONSTRAINT capability_invocations_result_shape CHECK (
        (status = 'pending'
            AND output IS NULL AND error_code IS NULL AND completed_at IS NULL)
     OR (status = 'succeeded'
            AND output IS NOT NULL AND error_code IS NULL AND completed_at IS NOT NULL)
     OR (status IN ('failed','denied','timeout','unknown')
            AND error_code IS NOT NULL AND completed_at IS NOT NULL)
    ),
    CONSTRAINT capability_invocations_unknown_code CHECK (
        status <> 'unknown' OR error_code = 'capability_unknown'
    ),
    CONSTRAINT capability_invocations_output_size CHECK (
        output IS NULL OR octet_length(output::text) <= 262144
    )
);

CREATE INDEX capability_invocations_actor_idx
    ON capability_invocations (actor_user_id, created_at DESC);
CREATE INDEX capability_invocations_object_idx
    ON capability_invocations (object_id, created_at DESC) WHERE object_id IS NOT NULL;

-- =====================================================================
-- model_invocations
-- =====================================================================
CREATE TABLE model_invocations (
    id              uuid        PRIMARY KEY,
    user_id         uuid        NOT NULL,
    conversation_id uuid,
    provider        text        NOT NULL,
    model           text        NOT NULL,
    input_chars     integer     NOT NULL,
    output_chars    integer     NOT NULL,
    duration_ms     integer     NOT NULL,
    status          text        NOT NULL,
    error_code      text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT model_invocations_user_fk
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT model_invocations_conversation_fk
        FOREIGN KEY (conversation_id) REFERENCES ai_conversations (id) ON DELETE RESTRICT,
    CONSTRAINT model_invocations_provider_len CHECK (char_length(provider) BETWEEN 1 AND 40),
    CONSTRAINT model_invocations_model_len CHECK (char_length(model) BETWEEN 1 AND 120),
    CONSTRAINT model_invocations_status_domain
        CHECK (status IN ('succeeded', 'failed', 'timeout')),
    CONSTRAINT model_invocations_counts CHECK (
        input_chars >= 0 AND output_chars >= 0 AND duration_ms >= 0
    ),
    CONSTRAINT model_invocations_error_shape CHECK (
        (status = 'succeeded' AND error_code IS NULL)
     OR (status <> 'succeeded' AND error_code IS NOT NULL)
    )
);

CREATE INDEX model_invocations_user_day_idx
    ON model_invocations (user_id, created_at DESC);

-- =====================================================================
-- audit_actions   —— 唯一审计动作注册表（A.4.6 的 49 项）
-- =====================================================================
CREATE TABLE audit_actions (
    action      text PRIMARY KEY,
    description text NOT NULL
);

INSERT INTO audit_actions (action, description) VALUES
    ('auth.register', 'A user registered'),
    ('auth.login', 'A user signed in'),
    ('auth.login_failed', 'A sign-in attempt failed'),
    ('auth.logout', 'A user signed out'),
    ('auth.session_revoked', 'A session was revoked'),
    ('auth.csrf_failed', 'A CSRF check failed'),
    ('auth.origin_rejected', 'An origin check failed'),
    ('pat.create', 'A personal access token was created'),
    ('pat.revoke', 'A personal access token was revoked'),
    ('me.export', 'A user exported personal data'),
    ('authz.denied', 'An action was denied for lack of permission'),
    ('authz.actor_override_attempt', 'A request tried to supply an authoritative actor field'),
    ('authz.not_found', 'A resource was hidden from an unauthorized caller'),
    ('rate.limit_hit', 'A rate limit was hit'),
    ('object.create', 'A living object was created'),
    ('object.action', 'A living object action was accepted'),
    ('object.derive', 'A living object was derived'),
    ('object.delete', 'A living object was soft deleted'),
    ('object.member_add', 'An object member was added'),
    ('object.member_remove', 'An object member was removed'),
    ('object.invitation_create', 'An object invitation was created'),
    ('object.invitation_revoke', 'An object invitation was revoked'),
    ('object.invitation_accept', 'An object invitation was accepted'),
    ('object.proposal_create', 'An AI proposal was created'),
    ('object.proposal_apply', 'An AI proposal was applied'),
    ('object.proposal_deny', 'An AI proposal was denied'),
    ('ai.message', 'An AI message was sent'),
    ('ai.handoff_issue', 'A handoff projection was issued'),
    ('ai.delegation_create', 'An AI delegation was created'),
    ('ai.delegation_revoke', 'An AI delegation was revoked'),
    ('ai.confirmation_create', 'A confirmation was created'),
    ('ai.confirmation_approve', 'A confirmation was approved'),
    ('ai.confirmation_deny', 'A confirmation was denied'),
    ('ai.profile_update', 'A personal AI profile was updated'),
    ('ai.context_update', 'A private AI context was updated'),
    ('ai.conversation_create', 'A private AI conversation was created'),
    ('ai.conversation_delete', 'A private AI conversation was deleted'),
    ('capability.invoke', 'A capability invocation was accepted'),
    ('capability.completed', 'A capability invocation completed'),
    ('capability.failed', 'A capability invocation failed'),
    ('capability.denied', 'A capability invocation was denied'),
    ('capability.timeout', 'A capability invocation timed out'),
    ('capability.unknown', 'A capability invocation has an unknown result'),
    ('capability.definition_synced', 'Capability definitions were synchronized'),
    ('capability.reconciled', 'Capability definitions were reconciled'),
    ('system.startup', 'The service started'),
    ('system.shutdown', 'The service stopped'),
    ('system.migration', 'A database migration ran'),
    ('system.config_error', 'Configuration validation failed');

-- =====================================================================
-- audit_events   —— 追加写
-- =====================================================================
CREATE TABLE audit_events (
    id                  uuid        PRIMARY KEY,
    occurred_at         timestamptz NOT NULL DEFAULT now(),
    actor_type          text        NOT NULL,
    actor_user_id       uuid,
    actor_ai_profile_id uuid,
    delegation_id       uuid,
    action              text        NOT NULL,
    resource_type       text        NOT NULL,
    resource_id         uuid,
    decision            text        NOT NULL,
    reason_code         text,
    request_id          text        NOT NULL,
    ip_hash             bytea,
    metadata            jsonb       NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT audit_events_actor_user_fk
        FOREIGN KEY (actor_user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT audit_events_actor_ai_fk
        FOREIGN KEY (actor_ai_profile_id) REFERENCES personal_ai_profiles (id) ON DELETE RESTRICT,
    CONSTRAINT audit_events_delegation_fk
        FOREIGN KEY (delegation_id) REFERENCES delegation_grants (id) ON DELETE RESTRICT,
    CONSTRAINT audit_events_action_fk
        FOREIGN KEY (action) REFERENCES audit_actions (action) ON DELETE RESTRICT,
    CONSTRAINT audit_events_actor_type_domain
        CHECK (actor_type IN ('user', 'personal_ai', 'system')),
    CONSTRAINT audit_events_decision_domain CHECK (decision IN ('allow', 'deny')),
    CONSTRAINT audit_events_request_id_len CHECK (char_length(request_id) BETWEEN 8 AND 64),
    CONSTRAINT audit_events_ip_hash_len CHECK (ip_hash IS NULL OR octet_length(ip_hash) = 32),
    CONSTRAINT audit_events_metadata_object CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT audit_events_metadata_size CHECK (octet_length(metadata::text) <= 4096),
    CONSTRAINT audit_events_actor_shape CHECK (
        (actor_type = 'system'      AND actor_user_id IS NULL AND actor_ai_profile_id IS NULL)
     OR (actor_type = 'user'        AND actor_user_id IS NOT NULL AND actor_ai_profile_id IS NULL)
     OR (actor_type = 'personal_ai' AND actor_user_id IS NOT NULL AND actor_ai_profile_id IS NOT NULL)
    )
);

CREATE INDEX audit_events_occurred_idx  ON audit_events (occurred_at DESC);
CREATE INDEX audit_events_actor_idx     ON audit_events (actor_user_id, occurred_at DESC);
CREATE INDEX audit_events_resource_idx  ON audit_events (resource_type, resource_id, occurred_at DESC);
CREATE INDEX audit_events_action_idx    ON audit_events (action, occurred_at DESC);

-- =====================================================================
-- idempotency_keys   —— status_code IS NULL 表示处理中
-- =====================================================================
CREATE TABLE idempotency_keys (
    actor_user_id       uuid        NOT NULL,
    route               text        NOT NULL,
    key                 text        NOT NULL,
    request_hash        bytea       NOT NULL,
    status_code         integer,
    response_body       jsonb,
    response_truncated  boolean     NOT NULL DEFAULT false,
    lease_until         timestamptz NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    completed_at        timestamptz,
    expires_at          timestamptz NOT NULL,
    CONSTRAINT idempotency_keys_pkey PRIMARY KEY (actor_user_id, route, key),
    CONSTRAINT idempotency_keys_actor_fk
        FOREIGN KEY (actor_user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT idempotency_keys_key_format CHECK (key ~ '^[A-Za-z0-9_-]{16,128}$'),
    CONSTRAINT idempotency_keys_request_hash_len CHECK (octet_length(request_hash) = 32),
    CONSTRAINT idempotency_keys_status_range CHECK (
        status_code IS NULL OR status_code BETWEEN 200 AND 599
    ),
    CONSTRAINT idempotency_keys_response_shape CHECK (
        (status_code IS NULL AND completed_at IS NULL)
     OR (status_code IS NOT NULL AND completed_at IS NOT NULL)
    ),
    CONSTRAINT idempotency_keys_response_size CHECK (
        response_body IS NULL OR octet_length(response_body::text) <= 262144
    ),
    CONSTRAINT idempotency_keys_expires_after_created CHECK (expires_at > created_at)
);

CREATE INDEX idempotency_keys_lease_idx     ON idempotency_keys (lease_until);
CREATE INDEX idempotency_keys_completed_idx ON idempotency_keys (completed_at);
CREATE INDEX idempotency_keys_expires_idx   ON idempotency_keys (expires_at);

-- =====================================================================
-- object_proposals   —— AI 提议；input 仅 actor 可见
-- =====================================================================
CREATE TABLE object_proposals (
    id               uuid        PRIMARY KEY,
    object_id        uuid        NOT NULL,
    actor_user_id    uuid        NOT NULL,
    ai_profile_id    uuid        NOT NULL,
    delegation_id    uuid        NOT NULL,
    capability_id    text        NOT NULL,
    action           text        NOT NULL,
    input            jsonb       NOT NULL,
    input_hash       bytea       NOT NULL,
    base_version     integer     NOT NULL,
    status           text        NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    decided_at       timestamptz,
    applied_event_id uuid,
    CONSTRAINT object_proposals_object_fk
        FOREIGN KEY (object_id) REFERENCES living_objects (id) ON DELETE RESTRICT,
    CONSTRAINT object_proposals_actor_fk
        FOREIGN KEY (actor_user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT object_proposals_ai_fk
        FOREIGN KEY (ai_profile_id) REFERENCES personal_ai_profiles (id) ON DELETE RESTRICT,
    CONSTRAINT object_proposals_delegation_fk
        FOREIGN KEY (delegation_id) REFERENCES delegation_grants (id) ON DELETE RESTRICT,
    CONSTRAINT object_proposals_applied_event_fk
        FOREIGN KEY (applied_event_id) REFERENCES object_events (id) ON DELETE RESTRICT,
    CONSTRAINT object_proposals_input_hash_len CHECK (octet_length(input_hash) = 32),
    CONSTRAINT object_proposals_input_size CHECK (octet_length(input::text) <= 16384),
    CONSTRAINT object_proposals_base_version CHECK (base_version >= 1),
    CONSTRAINT object_proposals_status_domain CHECK (
        status IN ('pending','applied','denied','expired')
    )
);

CREATE INDEX object_proposals_actor_idx
    ON object_proposals (object_id, actor_user_id, status);

UPDATE app_meta
   SET value = '2', updated_at = now()
 WHERE key = 'schema_version';
