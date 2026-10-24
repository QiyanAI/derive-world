-- SPDX-License-Identifier: Apache-2.0
-- +goose Up
-- G01 bootstrap only. Business tables are added by later Gates.
CREATE TABLE app_meta (
    key        text        PRIMARY KEY,
    value      text        NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO app_meta (key, value)
VALUES
    ('schema_version', '1'),
    ('g01_bootstrap', 'true')
ON CONFLICT (key) DO NOTHING;
