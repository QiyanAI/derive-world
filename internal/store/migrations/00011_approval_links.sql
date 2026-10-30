-- SPDX-License-Identifier: Apache-2.0
-- +goose Up
-- Link every approval to the conversation turn that produced it.
--
-- The product rule is "confirmation appears where the action was proposed".
-- A pending confirmation or proposal therefore records the ai_conversations row
-- and the exact assistant ai_messages row it belongs to. The UI renders the
-- approval directly under that message and can find it again after a refresh.
--
-- All columns are nullable: confirmations and proposals created outside a
-- Personal AI turn (capability invocations, legacy rows) have no message. The
-- links are display-only and never participate in authorization; the approval
-- path still revalidates permission, delegation, version and payload hash.
--
-- result_object_id records the object an approval actually created (a world or
-- a derived branch). It lets a refreshed conversation show the real entry point
-- instead of asking for approval again. Capability invocations leave it null.

-- A message is identified inside its conversation. The composite unique key
-- lets the approval tables enforce that a linked message and conversation
-- agree, so an approval can never appear under another conversation's reply.
ALTER TABLE ai_messages
    ADD CONSTRAINT ai_messages_id_conversation_unique UNIQUE (id, conversation_id);

ALTER TABLE confirmations
    ADD COLUMN conversation_id uuid,
    ADD COLUMN message_id uuid,
    ADD COLUMN result_object_id uuid;

ALTER TABLE confirmations
    ADD CONSTRAINT confirmations_approval_link_pair
        CHECK ((conversation_id IS NULL) = (message_id IS NULL)),
    -- RESTRICT is deliberate: these are durable provenance links, and the
    -- product only soft-deletes conversations, messages and objects. A hard
    -- delete must fail loudly rather than silently sever a pending approval.
    ADD CONSTRAINT confirmations_message_conversation_fk
        FOREIGN KEY (message_id, conversation_id)
        REFERENCES ai_messages (id, conversation_id) ON DELETE RESTRICT,
    ADD CONSTRAINT confirmations_result_object_fk
        FOREIGN KEY (result_object_id) REFERENCES living_objects (id) ON DELETE RESTRICT;

-- A pending approval is looked up by its conversation; the message link is
-- rendered inline. Partial indexes keep the common pending read small.
CREATE INDEX confirmations_conversation_idx
    ON confirmations (conversation_id, created_at DESC, id DESC)
    WHERE conversation_id IS NOT NULL;
CREATE INDEX confirmations_message_idx
    ON confirmations (message_id) WHERE message_id IS NOT NULL;

ALTER TABLE object_proposals
    ADD COLUMN conversation_id uuid,
    ADD COLUMN source_message_id uuid;

ALTER TABLE object_proposals
    ADD CONSTRAINT object_proposals_approval_link_pair
        CHECK ((conversation_id IS NULL) = (source_message_id IS NULL)),
    ADD CONSTRAINT object_proposals_source_message_conversation_fk
        FOREIGN KEY (source_message_id, conversation_id)
        REFERENCES ai_messages (id, conversation_id) ON DELETE RESTRICT;

CREATE INDEX object_proposals_conversation_idx
    ON object_proposals (conversation_id, created_at DESC, id DESC)
    WHERE conversation_id IS NOT NULL;
CREATE INDEX object_proposals_source_message_idx
    ON object_proposals (source_message_id) WHERE source_message_id IS NOT NULL;

-- Existing approved creations already recorded their confirmation id in the
-- object.created event. Backfill that real result so a refreshed saved
-- conversation offers the world's entry instead of asking again. Rows without
-- a matching event stay null and are shown as created without a link.
UPDATE confirmations c
   SET result_object_id = e.object_id
  FROM object_events e
 WHERE c.result_object_id IS NULL
   AND c.status = 'approved'
   AND e.event_type = 'object.created'
   AND e.payload->>'confirmation_id' = c.id::text;

UPDATE app_meta
   SET value = '11', updated_at = now()
 WHERE key = 'schema_version';
