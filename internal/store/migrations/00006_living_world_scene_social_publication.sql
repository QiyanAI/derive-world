-- SPDX-License-Identifier: Apache-2.0
-- +goose Up
-- Living world schema: the third native object type, the Social domain, and the
-- Publication domain. This migration is append-only: it does not rewrite or
-- remove any earlier migration or object history.
--
-- It preserves the existing contract: one objects write path, membership-based
-- private reads, versioned derivation edges, and no anonymous access to the
-- private object endpoint.

-- =====================================================================
-- living_scene_v1 — third native object type
-- =====================================================================
ALTER TABLE living_objects DROP CONSTRAINT living_objects_type_domain;
ALTER TABLE living_objects ADD CONSTRAINT living_objects_type_domain
    CHECK (object_type IN ('collaborative', 'interactive', 'living_scene'));

ALTER TABLE living_objects DROP CONSTRAINT living_objects_template_domain;
ALTER TABLE living_objects ADD CONSTRAINT living_objects_template_domain
    CHECK (presentation_template IN (
        'collaborative_v1', 'interactive_v1', 'living_scene_v1'
    ));

ALTER TABLE living_objects DROP CONSTRAINT living_objects_template_matches_type;
ALTER TABLE living_objects ADD CONSTRAINT living_objects_template_matches_type CHECK (
    (object_type = 'collaborative' AND presentation_template = 'collaborative_v1')
 OR (object_type = 'interactive'   AND presentation_template = 'interactive_v1')
 OR (object_type = 'living_scene'  AND presentation_template = 'living_scene_v1')
);

-- =====================================================================
-- derivation_edges — scene inheritance fields
-- =====================================================================
ALTER TABLE derivation_edges DROP CONSTRAINT derivation_edges_fields_domain;
ALTER TABLE derivation_edges ADD CONSTRAINT derivation_edges_fields_domain CHECK (
    inherited_fields <@ ARRAY[
        'summary', 'body', 'checklist', 'tags', 'prompt', 'options',
        'premise', 'elements'
    ]::text[]
);

-- =====================================================================
-- friendships — one row per unordered user pair
-- =====================================================================
CREATE TABLE friendships (
    id           uuid        PRIMARY KEY,
    requester_id uuid        NOT NULL,
    addressee_id uuid        NOT NULL,
    status       text        NOT NULL DEFAULT 'pending',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    responded_at timestamptz,
    CONSTRAINT friendships_requester_fk
        FOREIGN KEY (requester_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT friendships_addressee_fk
        FOREIGN KEY (addressee_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT friendships_no_self CHECK (requester_id <> addressee_id),
    CONSTRAINT friendships_status_domain
        CHECK (status IN ('pending', 'accepted', 'declined', 'cancelled', 'removed')),
    CONSTRAINT friendships_response_shape CHECK (
        (status = 'pending' AND responded_at IS NULL)
     OR (status <> 'pending' AND responded_at IS NOT NULL)
    ),
    CONSTRAINT friendships_time_order CHECK (
        updated_at >= created_at
        AND (responded_at IS NULL OR responded_at >= created_at)
    )
);

CREATE UNIQUE INDEX friendships_pair_key
    ON friendships (LEAST(requester_id, addressee_id), GREATEST(requester_id, addressee_id));
CREATE INDEX friendships_requester_idx ON friendships (requester_id, updated_at DESC);
CREATE INDEX friendships_addressee_idx ON friendships (addressee_id, updated_at DESC);

-- =====================================================================
-- user_blocks — independent of the friendship state machine
-- =====================================================================
CREATE TABLE user_blocks (
    blocker_id uuid        NOT NULL,
    blocked_id uuid        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    CONSTRAINT user_blocks_pkey PRIMARY KEY (blocker_id, blocked_id),
    CONSTRAINT user_blocks_blocker_fk
        FOREIGN KEY (blocker_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT user_blocks_blocked_fk
        FOREIGN KEY (blocked_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT user_blocks_no_self CHECK (blocker_id <> blocked_id),
    CONSTRAINT user_blocks_revoked_order CHECK (
        revoked_at IS NULL OR revoked_at >= created_at
    )
);

CREATE INDEX user_blocks_blocked_active_idx
    ON user_blocks (blocked_id) WHERE revoked_at IS NULL;

-- =====================================================================
-- direct_messages — human-to-human, never Personal AI private data
-- =====================================================================
CREATE TABLE direct_messages (
    id                uuid        PRIMARY KEY,
    seq               bigserial   NOT NULL,
    client_message_id uuid        NOT NULL,
    sender_id         uuid        NOT NULL,
    recipient_id      uuid        NOT NULL,
    body              text        NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    read_at           timestamptz,
    CONSTRAINT direct_messages_sender_fk
        FOREIGN KEY (sender_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT direct_messages_recipient_fk
        FOREIGN KEY (recipient_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT direct_messages_no_self CHECK (sender_id <> recipient_id),
    CONSTRAINT direct_messages_client_unique UNIQUE (sender_id, client_message_id),
    CONSTRAINT direct_messages_body_len CHECK (char_length(body) BETWEEN 1 AND 4000),
    CONSTRAINT direct_messages_body_size CHECK (octet_length(body) <= 16384),
    CONSTRAINT direct_messages_read_order CHECK (
        read_at IS NULL OR read_at >= created_at
    )
);

CREATE UNIQUE INDEX direct_messages_seq_key ON direct_messages (seq);
CREATE INDEX direct_messages_pair_idx
    ON direct_messages (LEAST(sender_id, recipient_id), GREATEST(sender_id, recipient_id), seq DESC);
CREATE INDEX direct_messages_unread_idx
    ON direct_messages (recipient_id, seq DESC) WHERE read_at IS NULL;

-- =====================================================================
-- object_discussions — human discussion around an authorized object
-- =====================================================================
CREATE TABLE object_discussions (
    id                uuid        PRIMARY KEY,
    seq               bigserial   NOT NULL,
    client_message_id uuid        NOT NULL,
    object_id         uuid        NOT NULL,
    author_id         uuid        NOT NULL,
    body              text        NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT object_discussions_object_fk
        FOREIGN KEY (object_id) REFERENCES living_objects (id) ON DELETE RESTRICT,
    CONSTRAINT object_discussions_author_fk
        FOREIGN KEY (author_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT object_discussions_client_unique UNIQUE (author_id, client_message_id),
    CONSTRAINT object_discussions_body_len CHECK (char_length(body) BETWEEN 1 AND 4000),
    CONSTRAINT object_discussions_body_size CHECK (octet_length(body) <= 16384)
);

CREATE UNIQUE INDEX object_discussions_seq_key ON object_discussions (seq);
CREATE INDEX object_discussions_object_idx ON object_discussions (object_id, seq ASC);

-- =====================================================================
-- publications — controlled public projection of one object version
-- =====================================================================
CREATE TABLE publications (
    id                    uuid        PRIMARY KEY,
    object_id             uuid        NOT NULL,
    source_version        integer     NOT NULL,
    publisher_id          uuid        NOT NULL,
    title                 text        NOT NULL,
    summary               text        NOT NULL DEFAULT '',
    object_type           text        NOT NULL,
    presentation_template text        NOT NULL,
    snapshot              jsonb       NOT NULL,
    status                text        NOT NULL DEFAULT 'pending_consent',
    derive_allowed        boolean     NOT NULL DEFAULT false,
    license               text        NOT NULL DEFAULT 'all_rights_reserved',
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    revoked_at            timestamptz,
    CONSTRAINT publications_object_fk
        FOREIGN KEY (object_id) REFERENCES living_objects (id) ON DELETE RESTRICT,
    CONSTRAINT publications_publisher_fk
        FOREIGN KEY (publisher_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT publications_source_version CHECK (source_version >= 1),
    CONSTRAINT publications_title_len CHECK (char_length(title) BETWEEN 1 AND 200),
    CONSTRAINT publications_summary_len CHECK (char_length(summary) <= 2000),
    CONSTRAINT publications_type_domain
        CHECK (object_type IN ('collaborative', 'interactive', 'living_scene')),
    CONSTRAINT publications_template_matches_type CHECK (
        (object_type = 'collaborative' AND presentation_template = 'collaborative_v1')
     OR (object_type = 'interactive'   AND presentation_template = 'interactive_v1')
     OR (object_type = 'living_scene'  AND presentation_template = 'living_scene_v1')
    ),
    CONSTRAINT publications_snapshot_object CHECK (jsonb_typeof(snapshot) = 'object'),
    -- 64 KiB object state plus the small public envelope (title, type,
    -- template); the object state CHECK remains the binding content limit.
    CONSTRAINT publications_snapshot_size CHECK (octet_length(snapshot::text) <= 66560),
    CONSTRAINT publications_status_domain
        CHECK (status IN ('pending_consent', 'active', 'declined', 'revoked')),
    CONSTRAINT publications_license_domain
        CHECK (license IN ('all_rights_reserved', 'cc_by_4.0', 'cc_by_sa_4.0')),
    CONSTRAINT publications_revoked_shape CHECK (
        (status = 'revoked' AND revoked_at IS NOT NULL)
     OR (status <> 'revoked' AND revoked_at IS NULL)
    ),
    CONSTRAINT publications_time_order CHECK (
        updated_at >= created_at
        AND (revoked_at IS NULL OR revoked_at >= created_at)
    )
);

CREATE UNIQUE INDEX publications_live_version_key
    ON publications (object_id, source_version)
    WHERE status IN ('pending_consent', 'active');
CREATE INDEX publications_active_idx
    ON publications (created_at DESC, id DESC) WHERE status = 'active';
CREATE INDEX publications_object_idx
    ON publications (object_id, created_at DESC);

-- =====================================================================
-- publication_consents — contributor approval before public visibility
-- =====================================================================
CREATE TABLE publication_consents (
    id             uuid        PRIMARY KEY,
    publication_id uuid        NOT NULL,
    contributor_id uuid        NOT NULL,
    status         text        NOT NULL DEFAULT 'pending',
    created_at     timestamptz NOT NULL DEFAULT now(),
    responded_at   timestamptz,
    CONSTRAINT publication_consents_publication_fk
        FOREIGN KEY (publication_id) REFERENCES publications (id) ON DELETE RESTRICT,
    CONSTRAINT publication_consents_contributor_fk
        FOREIGN KEY (contributor_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT publication_consents_unique UNIQUE (publication_id, contributor_id),
    CONSTRAINT publication_consents_status_domain
        CHECK (status IN ('pending', 'approved', 'declined')),
    CONSTRAINT publication_consents_response_shape CHECK (
        (status = 'pending' AND responded_at IS NULL)
     OR (status <> 'pending' AND responded_at IS NOT NULL)
    ),
    CONSTRAINT publication_consents_time_order CHECK (
        responded_at IS NULL OR responded_at >= created_at
    )
);

CREATE INDEX publication_consents_pending_idx
    ON publication_consents (contributor_id, created_at DESC) WHERE status = 'pending';

-- =====================================================================
-- publication_derivations — public lineage relation for derived children
-- =====================================================================
CREATE TABLE publication_derivations (
    child_object_id uuid        PRIMARY KEY,
    publication_id  uuid        NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT publication_derivations_child_fk
        FOREIGN KEY (child_object_id) REFERENCES living_objects (id) ON DELETE RESTRICT,
    CONSTRAINT publication_derivations_publication_fk
        FOREIGN KEY (publication_id) REFERENCES publications (id) ON DELETE RESTRICT
);

CREATE INDEX publication_derivations_publication_idx
    ON publication_derivations (publication_id);

-- =====================================================================
-- publication_reports — basic abuse intake for public content
-- =====================================================================
CREATE TABLE publication_reports (
    id             uuid        PRIMARY KEY,
    publication_id uuid        NOT NULL,
    reporter_id    uuid        NOT NULL,
    reason         text        NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT publication_reports_publication_fk
        FOREIGN KEY (publication_id) REFERENCES publications (id) ON DELETE RESTRICT,
    CONSTRAINT publication_reports_reporter_fk
        FOREIGN KEY (reporter_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT publication_reports_unique UNIQUE (publication_id, reporter_id),
    CONSTRAINT publication_reports_reason_len CHECK (char_length(reason) BETWEEN 1 AND 1000)
);

CREATE INDEX publication_reports_publication_idx
    ON publication_reports (publication_id, created_at DESC);

-- =====================================================================
-- audit action registry additions
-- =====================================================================
INSERT INTO audit_actions (action, description) VALUES
    ('social.friend_request',   'A friend request was sent'),
    ('social.friend_accept',    'A friend request was accepted'),
    ('social.friend_decline',   'A friend request was declined'),
    ('social.friend_remove',    'A friendship was removed'),
    ('social.block',            'A user blocked another user'),
    ('social.unblock',          'A user unblocked another user'),
    ('social.message_send',     'A direct human message was sent'),
    ('social.discussion_post',  'A human posted on an object discussion'),
    ('publication.create',      'An object version was submitted for publication'),
    ('publication.activate',    'A publication became publicly visible'),
    ('publication.revoke',      'A publication was revoked'),
    ('publication.consent',     'A contributor responded to a publication consent request'),
    ('publication.derive',      'A public snapshot was derived into a private object'),
    ('publication.report',      'A public snapshot was reported for abuse')
ON CONFLICT (action) DO NOTHING;

-- =====================================================================
-- runtime role grants (least privilege, no DELETE)
-- =====================================================================
GRANT SELECT, INSERT, UPDATE ON
    friendships,
    user_blocks,
    direct_messages,
    object_discussions,
    publications,
    publication_consents
TO derive_world_app;

GRANT SELECT, INSERT ON publication_derivations TO derive_world_app;
GRANT SELECT, INSERT ON publication_reports TO derive_world_app;

GRANT USAGE, SELECT ON SEQUENCE direct_messages_seq_seq TO derive_world_app;
GRANT USAGE, SELECT ON SEQUENCE object_discussions_seq_seq TO derive_world_app;

UPDATE app_meta
   SET value = '6', updated_at = now()
 WHERE key = 'schema_version';
