// SPDX-License-Identifier: Apache-2.0
package store

import (
	"context"
	"embed"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate runs forward-only embedded migrations. It is safe to repeat.
func (s *Store) Migrate(ctx context.Context) error {
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	// G00 contract §A.6.1 names the migration table schema_migrations. This is
	// the normative source; D03-05's "goose default" wording is superseded on
	// the table name only. The migration mechanism remains goose.
	goose.SetTableName("schema_migrations")
	db := stdlib.OpenDBFromPool(s.pool)
	defer db.Close()
	return goose.UpContext(ctx, db, "migrations")
}
