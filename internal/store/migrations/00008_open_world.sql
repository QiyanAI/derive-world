-- SPDX-License-Identifier: Apache-2.0
-- +goose Up
-- open_world — a fourth native object type whose state is a declarative,
-- user-defined world. It adds no new tables and changes no existing data: only
-- the closed type/template domains and the derive inheritance vocabulary grow.
--
-- The state remains bounded by the existing 64 KiB CHECK. Behaviour is data,
-- never executable code; the server interprets rules inside the same object
-- write path.

ALTER TABLE living_objects DROP CONSTRAINT living_objects_type_domain;
ALTER TABLE living_objects ADD CONSTRAINT living_objects_type_domain
    CHECK (object_type IN ('collaborative', 'interactive', 'living_scene', 'open_world'));

ALTER TABLE living_objects DROP CONSTRAINT living_objects_template_domain;
ALTER TABLE living_objects ADD CONSTRAINT living_objects_template_domain
    CHECK (presentation_template IN (
        'collaborative_v1', 'interactive_v1', 'living_scene_v1', 'open_world_v1'
    ));

ALTER TABLE living_objects DROP CONSTRAINT living_objects_template_matches_type;
ALTER TABLE living_objects ADD CONSTRAINT living_objects_template_matches_type CHECK (
    (object_type = 'collaborative' AND presentation_template = 'collaborative_v1')
 OR (object_type = 'interactive'   AND presentation_template = 'interactive_v1')
 OR (object_type = 'living_scene'  AND presentation_template = 'living_scene_v1')
 OR (object_type = 'open_world'    AND presentation_template = 'open_world_v1')
);

ALTER TABLE derivation_edges DROP CONSTRAINT derivation_edges_fields_domain;
ALTER TABLE derivation_edges ADD CONSTRAINT derivation_edges_fields_domain CHECK (
    inherited_fields <@ ARRAY[
        'summary', 'body', 'checklist', 'tags', 'prompt', 'options',
        'premise', 'elements', 'structure', 'views'
    ]::text[]
);

ALTER TABLE publications DROP CONSTRAINT publications_type_domain;
ALTER TABLE publications ADD CONSTRAINT publications_type_domain
    CHECK (object_type IN ('collaborative', 'interactive', 'living_scene', 'open_world'));

ALTER TABLE publications DROP CONSTRAINT publications_template_matches_type;
ALTER TABLE publications ADD CONSTRAINT publications_template_matches_type CHECK (
    (object_type = 'collaborative' AND presentation_template = 'collaborative_v1')
 OR (object_type = 'interactive'   AND presentation_template = 'interactive_v1')
 OR (object_type = 'living_scene'  AND presentation_template = 'living_scene_v1')
 OR (object_type = 'open_world'    AND presentation_template = 'open_world_v1')
);

-- The unbound Personal AI asks a human to approve one world creation. The
-- confirmation keeps the existing single-use, hashed-payload, five-minute
-- semantics; only the closed action/resource domains grow.
ALTER TABLE confirmations DROP CONSTRAINT confirmations_action_domain;
ALTER TABLE confirmations ADD CONSTRAINT confirmations_action_domain CHECK (
    action IN ('derive.create', 'invitation.create', 'capability.invoke', 'world.create')
);

ALTER TABLE confirmations DROP CONSTRAINT confirmations_resource_type_domain;
ALTER TABLE confirmations ADD CONSTRAINT confirmations_resource_type_domain CHECK (
    resource_type IN ('living_object', 'capability_invocation', 'personal_ai')
);

UPDATE app_meta
   SET value = '8', updated_at = now()
 WHERE key = 'schema_version';
