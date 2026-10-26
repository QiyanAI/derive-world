// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// TestDWPublicationRevokeDeriveRace holds the publication row lock, starts a
// public derive that must wait on FOR SHARE, revokes the publication in the
// same transaction, then commits. The derive's final recheck must see the
// revoked status and create no child. This deterministically covers the
// withdrawal-vs-derive boundary instead of trusting timing.
func TestDWPublicationRevokeDeriveRace(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "race-owner")
	visitor := env.register(t, "race-visitor")
	object := owner.createObject(t, "collaborative", "Race source", map[string]any{
		"summary": "A race-safe public snapshot.",
		"body":    "Body.",
	})
	status, body, _ := owner.call(t, http.MethodPost,
		"/api/v1/objects/"+object.ID+"/publications", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"derive_allowed": true, "license": "cc_by_4.0",
			},
		})
	requireStatus(t, status, http.StatusCreated, body)
	var publication struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &publication); err != nil {
		t.Fatalf("decode publication: %v", err)
	}

	ctx := t.Context()
	tx, err := env.store.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx,
		`SELECT id FROM publications WHERE id = $1 FOR UPDATE`, publication.ID); err != nil {
		t.Fatalf("lock publication: %v", err)
	}

	type outcome struct {
		status int
		body   []byte
	}
	done := make(chan outcome, 1)
	go func() {
		status, body, _ := visitor.call(t, http.MethodPost,
			"/api/v1/public/worlds/"+publication.ID+"/derive", callOptions{
				IdempotencyKey: newIdempotencyKey(),
				Body: map[string]any{
					"title": "Race branch", "inheritance_fields": []string{"summary"},
				},
			})
		done <- outcome{status: status, body: body}
	}()

	select {
	case result := <-done:
		t.Fatalf("derive finished before the publication lock was released: status=%d body=%s",
			result.status, result.body)
	case <-time.After(300 * time.Millisecond):
		// The derive is blocked on the row lock, as intended.
	}

	if _, err := tx.Exec(ctx,
		`UPDATE publications SET status = 'revoked', revoked_at = now() WHERE id = $1`,
		publication.ID); err != nil {
		t.Fatalf("revoke publication: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit revoke: %v", err)
	}

	select {
	case result := <-done:
		if result.status != http.StatusNotFound {
			t.Fatalf("derive after revoke status = %d body = %s, want 404",
				result.status, result.body)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("derive did not finish after the publication lock was released")
	}

	if rows := env.count(t, `
SELECT count(*) FROM publication_derivations WHERE publication_id = $1`,
		publication.ID); rows != 0 {
		t.Fatalf("publication_derivations rows = %d, want 0", rows)
	}
	if rows := env.count(t, `
SELECT count(*) FROM living_objects
 WHERE creator_id = $1 AND state->>'title' = $2`,
		visitor.id.String(), "Race branch"); rows != 0 {
		t.Fatalf("revoked derive created %d child objects, want 0", rows)
	}
}
