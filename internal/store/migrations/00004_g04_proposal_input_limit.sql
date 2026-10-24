-- SPDX-License-Identifier: Apache-2.0
-- +goose Up
-- G04 contract closeout correction C-G04-03.
--
-- A.11 fixed the AI proposal input at 16 KiB, while A.6.4 permits a
-- 20,000-character set_body input. A 20,000-character body that can exist in
-- the 64 KiB object state (A.6.3) could therefore never be proposed. The
-- business cap is raised to 64 KiB to match the object-state ceiling:
--   * the Broker/objects service enforce <= 65,536 bytes of canonical JSON;
--   * this storage CHECK allows 256 bytes for PostgreSQL jsonb text formatting
--     (the logical cap remains 65,536 bytes, enforced in Go).
-- No object-state limit is changed.
ALTER TABLE object_proposals DROP CONSTRAINT object_proposals_input_size;
ALTER TABLE object_proposals ADD CONSTRAINT object_proposals_input_size
    CHECK (octet_length(input::text) <= 65792);

UPDATE app_meta
   SET value = '4', updated_at = now()
 WHERE key = 'schema_version';
