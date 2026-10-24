-- SPDX-License-Identifier: Apache-2.0
-- +goose Up
-- G04 contract correction C-G04-04 (resolves R-G04-17).
--
-- D04-09 and D04 section 3.9 require object.state_changed to store the full
-- normalized input and the full state_after snapshot. D04-21 froze every event
-- payload at 96 KiB. A legal 20,000-character multibyte body can fit the
-- 64 KiB object state but its action input plus state_after duplicate the
-- body, so a legal apply could not be stored.
--
-- Only object.state_changed is widened, to 132,352 bytes. Every other event
-- keeps the frozen 96 KiB bound. 132,352 is an upper bound derived from the
-- frozen component maxima:
--   65,536 bytes  object state (A.6.3 / D04-21)
--   65,792 bytes  proposal input storage (C-G04-03: 65,536 + 256 jsonb text)
--    1,024 bytes  fixed action/input/state_after structural allowance
-- The largest legal state_changed payload reachable through a whitelisted
-- action is 131,064 jsonb-text bytes (set_body with a 20,000-character body
-- whose input and state_after both fit); see C-G04-04 for the proof.
--
-- The Go layer applies the same event-type-scoped limit in
-- appendEventReturningID before this CHECK runs.
ALTER TABLE object_events DROP CONSTRAINT object_events_payload_size;
ALTER TABLE object_events ADD CONSTRAINT object_events_payload_size
    CHECK (octet_length(payload::text) <= CASE event_type
        WHEN 'object.state_changed' THEN 132352
        ELSE 98304
    END);

UPDATE app_meta
   SET value = '5', updated_at = now()
 WHERE key = 'schema_version';
