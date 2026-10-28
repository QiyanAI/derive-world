// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/QiyanAI/derive-world/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Fatal("DATABASE_URL is required for integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(database.Close)
	return database
}

func TestMigrationIdempotent(t *testing.T) {
	database := openTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("second migrate must be idempotent: %v", err)
	}

	var schemaVersion string
	if err := database.Pool().QueryRow(ctx,
		"SELECT value FROM app_meta WHERE key = 'schema_version'").Scan(&schemaVersion); err != nil {
		t.Fatalf("read app_meta: %v", err)
	}
	if schemaVersion != "9" {
		t.Fatalf("schema_version = %q, want 9", schemaVersion)
	}

	var migrationTableExists bool
	if err := database.Pool().QueryRow(ctx,
		"SELECT to_regclass('public.schema_migrations') IS NOT NULL").Scan(&migrationTableExists); err != nil {
		t.Fatalf("check schema_migrations: %v", err)
	}
	if !migrationTableExists {
		t.Fatal("schema_migrations table is missing")
	}
}

func TestBootstrapMigrationStillCreatesNoBusinessTables(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "internal", "store", "migrations", "00001_bootstrap.sql")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read bootstrap migration: %v", err)
	}
	businessTables := []string{
		"users", "user_sessions", "api_tokens", "personal_ai_profiles",
		"private_ai_contexts", "ai_conversations", "ai_messages",
		"living_objects", "object_memberships", "object_events",
		"object_action_receipts", "invitations", "delegation_grants",
		"derivation_edges", "capability_definitions", "capability_invocations",
		"model_invocations", "audit_events", "idempotency_keys", "confirmations",
		"object_proposals", "audit_actions",
	}
	for _, table := range businessTables {
		if strings.Contains(string(content), "CREATE TABLE "+table) {
			t.Errorf("00001_bootstrap.sql must not create business table %s", table)
		}
	}
}

func TestFrozenSchemaShape(t *testing.T) {
	database := openTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	businessTables := []string{
		"users", "user_sessions", "api_tokens", "personal_ai_profiles",
		"private_ai_contexts", "ai_conversations", "ai_messages",
		"living_objects", "object_memberships", "object_events",
		"object_action_receipts", "invitations", "delegation_grants",
		"derivation_edges", "capability_definitions", "capability_invocations",
		"model_invocations", "audit_events", "idempotency_keys", "confirmations",
		"object_proposals", "audit_actions",
		"friendships", "user_blocks", "direct_messages", "object_discussions",
		"publications", "publication_consents", "publication_derivations",
		"publication_reports", "assets", "object_asset_refs",
		"publication_asset_refs",
	}
	for _, table := range businessTables {
		var exists bool
		if err := database.Pool().QueryRow(ctx,
			"SELECT to_regclass('public."+table+"') IS NOT NULL").Scan(&exists); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("business table %s is missing", table)
		}
	}

	var auditActions int
	if err := database.Pool().QueryRow(ctx, "SELECT count(*) FROM audit_actions").Scan(&auditActions); err != nil {
		t.Fatalf("count audit actions: %v", err)
	}
	if auditActions != 66 {
		t.Fatalf("audit_actions rows = %d, want 66", auditActions)
	}

	var sourceColumns int
	if err := database.Pool().QueryRow(ctx, `
SELECT count(*) FROM information_schema.columns
 WHERE table_schema='public' AND table_name='living_objects'
   AND column_name IN ('lineage','parent_id','parent_version')
`).Scan(&sourceColumns); err != nil {
		t.Fatalf("check lineage columns: %v", err)
	}
	if sourceColumns != 0 {
		t.Fatal("living_objects must not persist lineage/parent columns")
	}

	var triggerCount int
	if err := database.Pool().QueryRow(ctx, `
SELECT count(*) FROM information_schema.triggers WHERE trigger_schema='public'
`).Scan(&triggerCount); err != nil {
		t.Fatalf("count triggers: %v", err)
	}
	if triggerCount != 0 {
		t.Fatalf("database must not use triggers, found %d", triggerCount)
	}

	var nonTimestamptz int
	if err := database.Pool().QueryRow(ctx, `
SELECT count(*) FROM information_schema.columns
 WHERE table_schema='public'
   AND table_name <> 'schema_migrations'
   AND data_type = 'timestamp without time zone'
`).Scan(&nonTimestamptz); err != nil {
		t.Fatalf("check timestamps: %v", err)
	}
	if nonTimestamptz != 0 {
		t.Fatalf("all business time columns must be timestamptz, found %d without time zone", nonTimestamptz)
	}

	var roleCanLogin bool
	if err := database.Pool().QueryRow(ctx,
		"SELECT rolcanlogin FROM pg_roles WHERE rolname = 'derive_world_app'").Scan(&roleCanLogin); err != nil {
		t.Fatalf("read runtime role: %v", err)
	}
	if roleCanLogin {
		t.Fatal("derive_world_app must not be able to log in")
	}
	for table, privilege := range map[string]string{
		"object_events":    "UPDATE",
		"audit_events":     "DELETE",
		"idempotency_keys": "DELETE",
	} {
		var allowed bool
		query := "SELECT has_table_privilege('derive_world_app', $1, $2)"
		if err := database.Pool().QueryRow(ctx, query, table, privilege).Scan(&allowed); err != nil {
			t.Fatalf("check privilege %s on %s: %v", privilege, table, err)
		}
		if allowed {
			t.Fatalf("derive_world_app must not have %s on %s", privilege, table)
		}
	}
	for table, privilege := range map[string]string{
		"object_events":      "INSERT",
		"audit_events":       "INSERT",
		"living_objects":     "UPDATE",
		"user_world_layouts": "UPDATE",
		"idempotency_keys":   "UPDATE",
	} {
		var allowed bool
		query := "SELECT has_table_privilege('derive_world_app', $1, $2)"
		if err := database.Pool().QueryRow(ctx, query, table, privilege).Scan(&allowed); err != nil {
			t.Fatalf("check privilege %s on %s: %v", privilege, table, err)
		}
		if !allowed {
			t.Fatalf("derive_world_app requires %s on %s", privilege, table)
		}
	}

	var templateCheck string
	if err := database.Pool().QueryRow(ctx, `
SELECT pg_get_constraintdef(oid)
  FROM pg_constraint
 WHERE conname = 'living_objects_template_matches_type'
`).Scan(&templateCheck); err != nil {
		t.Fatalf("read template constraint: %v", err)
	}
	if !strings.Contains(templateCheck, "collaborative_v1") ||
		!strings.Contains(templateCheck, "interactive_v1") ||
		!strings.Contains(templateCheck, "living_scene_v1") ||
		!strings.Contains(templateCheck, "open_world_v1") {
		t.Fatalf("template pairing constraint is incomplete: %s", templateCheck)
	}

	// access_policy may never carry a public visibility value.
	var policyCheck string
	if err := database.Pool().QueryRow(ctx, `
SELECT pg_get_constraintdef(oid)
  FROM pg_constraint
 WHERE conname = 'living_objects_access_policy_shape'
`).Scan(&policyCheck); err != nil {
		t.Fatalf("read access_policy constraint: %v", err)
	}
	if !strings.Contains(policyCheck, "private") {
		t.Fatalf("access_policy constraint must pin visibility to private: %s", policyCheck)
	}
}

func TestRuntimeRoleEnforcesLeastPrivilege(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Fatal("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open owner store: %v", err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	runtimeStore, err := store.OpenWithRole(ctx, dsn, store.AppRole)
	if err != nil {
		t.Fatalf("open runtime role store: %v", err)
	}
	defer runtimeStore.Close()

	var currentUser string
	if err := runtimeStore.Pool().QueryRow(ctx, "SELECT current_user").Scan(&currentUser); err != nil {
		t.Fatalf("read current_user: %v", err)
	}
	if currentUser != store.AppRole {
		t.Fatalf("current_user = %q, want %q", currentUser, store.AppRole)
	}
	if _, err := runtimeStore.Pool().Exec(ctx, "DELETE FROM audit_events"); err == nil {
		t.Fatal("runtime role was able to DELETE audit_events")
	}
	if _, err := runtimeStore.Pool().Exec(ctx, "UPDATE object_events SET payload = '{}'::jsonb"); err == nil {
		t.Fatal("runtime role was able to UPDATE object_events")
	}
}
