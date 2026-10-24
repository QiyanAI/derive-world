// SPDX-License-Identifier: Apache-2.0

// Package store owns database infrastructure: the pgx pool, transaction
// helper, embedded migrations, and idempotency storage. It contains no domain
// rules and no HTTP types.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AppRole is the least-privilege runtime role created by migration 00003.
const AppRole = "derive_world_app"

// Store wraps a pgx connection pool.
type Store struct {
	pool *pgxpool.Pool
}

// Open creates a validated pool that connects with the migration owner's
// privileges. It is used by migrate and by integration test setup.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	return open(ctx, databaseURL, "")
}

// OpenWithRole creates a pool whose every connection issues SET ROLE to the
// frozen runtime role. The server uses this path; migrations do not.
func OpenWithRole(ctx context.Context, databaseURL, role string) (*Store, error) {
	if role == "" {
		return nil, errors.New("runtime role is required")
	}
	return open(ctx, databaseURL, role)
}

func open(ctx context.Context, databaseURL, role string) (*Store, error) {
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	poolConfig.MaxConns = 10
	poolConfig.MinConns = 0
	poolConfig.MaxConnLifetime = 30 * time.Minute
	poolConfig.MaxConnIdleTime = 5 * time.Minute
	poolConfig.HealthCheckPeriod = time.Minute
	if role != "" {
		poolConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "SET ROLE "+pgx.Identifier{role}.Sanitize())
			return err
		}
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

// Ping checks database availability.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close releases all pooled connections.
func (s *Store) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// Pool exposes the pool to store-owned code and integration tests. Domain
// modules receive narrow interfaces instead of this type.
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

// WithTx runs fn in a transaction and commits only on success.
func (s *Store) WithTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
