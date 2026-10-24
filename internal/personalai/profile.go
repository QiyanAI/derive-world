// SPDX-License-Identifier: Apache-2.0

// Package personalai owns the Personal AI profile, private context, and private
// conversations. G02 only creates the registration-owned profile and empty
// private context; conversational AI features are delivered in G03.
package personalai

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when the caller's profile does not exist.
var ErrNotFound = errors.New("personal AI profile not found")

// Profile is the public Personal AI profile projection.
type Profile struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Persona   string    `json:"persona"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// InsertRegistrationRowsTx creates the profile and the empty private context
// that every Digital Self owns. It runs inside the registration transaction.
func InsertRegistrationRowsTx(ctx context.Context, tx pgx.Tx, userID, profileID uuid.UUID, displayName string) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO personal_ai_profiles (id, user_id, name, persona)
VALUES ($1,$2,$3,'')`, profileID.String(), userID.String(), displayName); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO private_ai_contexts (user_id, content)
VALUES ($1,'')`, userID.String()); err != nil {
		return err
	}
	return nil
}

// GetProfile returns the caller's own profile.
func GetProfile(ctx context.Context, db *pgxpool.Pool, userID uuid.UUID) (*Profile, error) {
	var profile Profile
	err := db.QueryRow(ctx, `
SELECT id, name, persona, created_at, updated_at
  FROM personal_ai_profiles WHERE user_id = $1`, userID.String()).Scan(
		&profile.ID, &profile.Name, &profile.Persona, &profile.CreatedAt, &profile.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &profile, nil
}
