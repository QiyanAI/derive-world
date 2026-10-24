// SPDX-License-Identifier: Apache-2.0
package store

import (
	"io/fs"
	"strings"
	"testing"
)

// C-G04-04 must remain a forward-only, event-type-scoped migration: the
// object.state_changed bound is 132,352 and every other event stays at 98,304.
func TestEmbeddedStateChangedPayloadMigration(t *testing.T) {
	content, err := fs.ReadFile(migrationsFS, "migrations/00005_g04_state_changed_payload_limit.sql")
	if err != nil {
		t.Fatalf("00005 migration is not embedded: %v", err)
	}
	text := string(content)
	for _, want := range []string{
		"object_events_payload_size",
		"WHEN 'object.state_changed' THEN 132352",
		"ELSE 98304",
		"SET value = '5'",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("00005 migration is missing %q", want)
		}
	}
}

func TestEmbeddedBootstrapMigration(t *testing.T) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no embedded migrations")
	}
	found := false
	for _, entry := range entries {
		if entry.Name() == "00001_bootstrap.sql" {
			found = true
		}
	}
	if !found {
		t.Fatal("00001_bootstrap.sql is not embedded")
	}
	content, err := fs.ReadFile(migrationsFS, "migrations/00001_bootstrap.sql")
	if err != nil {
		t.Fatalf("read bootstrap migration: %v", err)
	}
	if !strings.Contains(string(content), "CREATE TABLE app_meta") {
		t.Fatal("bootstrap migration does not create app_meta")
	}
	if strings.Contains(string(content), "CREATE TABLE users") {
		t.Fatal("bootstrap migration must not create business tables")
	}
}
