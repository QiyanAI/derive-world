-- SPDX-License-Identifier: Apache-2.0
-- +goose Up
-- A bounded, server-authored notice attached to an assistant message.
--
-- Personal AI turns can parse successfully yet contain an action the server
-- rejects (invalid seed, stale version, missing permission, exhausted budget),
-- or the provider output can be unreadable. The user-visible reason for that
-- outcome must survive a refresh, so it is stored beside the message rather
-- than only returned in the one HTTP response or written to an operator log.
--
-- The column is never provider text: the server derives it from a closed set
-- of reason codes. Notice is assistant-only and bounded so it can never carry
-- a raw model document or private context.
ALTER TABLE ai_messages
    ADD COLUMN notice text;

ALTER TABLE ai_messages
    ADD CONSTRAINT ai_messages_notice_shape
    CHECK (
        notice IS NULL
        OR (role = 'assistant' AND char_length(notice) >= 1 AND octet_length(notice) <= 2048)
    );

UPDATE app_meta
   SET value = '10', updated_at = now()
 WHERE key = 'schema_version';
