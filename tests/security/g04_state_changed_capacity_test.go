// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// C-G04-04 resolves R-G04-17: object.state_changed alone is widened from
// 96 KiB to 132,352 bytes. These tests exercise the real PostgreSQL CHECK,
// the business maximum that can fit the 64 KiB object state, the rejection
// boundary above it, and the unchanged 96 KiB bound on every other event.

func proposeSetBody(t *testing.T, user *testUser, objectID, body string) (int, []byte) {
	t.Helper()
	status, response, _ := user.invokeCapability(t, "object.propose_change", map[string]any{
		"object_id":        objectID,
		"action":           "set_body",
		"input":            map[string]any{"body": body},
		"expected_version": 1,
	}, newIdempotencyKey())
	return status, response
}

// proposeSetBodyRaw sends the same capability body with raw UTF-8 characters.
// The ordinary helper marshals with Go's json.Marshal, which HTML-escapes
// &, < and >; a real client is not required to do that, and the frozen
// capability cap is measured on the sent bytes.
func proposeSetBodyRaw(t *testing.T, user *testUser, objectID, body string) (int, []byte) {
	t.Helper()
	raw := []byte(`{"object_id":"` + objectID + `","action":"set_body","input":{"body":"` +
		body + `"},"expected_version":1}`)
	status, response, _ := user.call(t, http.MethodPost,
		"/api/v1/capabilities/object.propose_change/invoke",
		callOptions{RawBody: raw, IdempotencyKey: newIdempotencyKey()})
	return status, response
}

func proposalIDFromResponse(t *testing.T, response []byte) string {
	t.Helper()
	invocation := decodeInvocation(t, response)
	var output struct {
		ProposalID string `json:"proposal_id"`
	}
	if err := json.Unmarshal(invocation.Output, &output); err != nil {
		t.Fatalf("decode proposal output: %v", err)
	}
	if output.ProposalID == "" {
		t.Fatalf("proposal id is empty: %s", invocation.Output)
	}
	return output.ProposalID
}

func applyProposalResponse(t *testing.T, body []byte) (string, objectResponse) {
	t.Helper()
	var applied struct {
		Proposal struct {
			Status string `json:"status"`
		} `json:"proposal"`
		Object objectResponse `json:"object"`
	}
	if err := json.Unmarshal(body, &applied); err != nil {
		t.Fatalf("decode apply response: %v", err)
	}
	return applied.Proposal.Status, applied.Object
}

// TestG04StateChangedPayloadMaxLegal proves the exact business maxima.
//
// A 20,000-character body of 65,466 UTF-8 bytes reaches the 65,536-byte state
// limit; its object.state_changed payload is exactly 131,064 jsonb-text bytes,
// above the former 96 KiB bound and inside C-G04-04's 132,352-byte bound.
//
// The capability wrapper (object_id, action, expected_version, input) consumes
// 113 bytes of the frozen 64 KiB capability input. The largest proposal body
// is therefore 65,423 bytes, producing a 65,493-byte state and a
// 130,978-byte payload. Both paths are exercised.
func TestG04StateChangedPayloadMaxLegal(t *testing.T) {
	env, _ := newG04Env(t, g04Success)
	user := env.register(t, "g04cap-max")

	t.Run("human action reaches the exact state maximum", func(t *testing.T) {
		object := user.createObject(t, "collaborative", "x", map[string]any{"body": ""})
		body := strings.Repeat("汉", 14534) + strings.Repeat("😀", 5466)
		if runes := len([]rune(body)); runes != 20000 {
			t.Fatalf("body runes = %d, want 20000", runes)
		}
		if bytes := len([]byte(body)); bytes != 65466 {
			t.Fatalf("body bytes = %d, want 65466", bytes)
		}

		status, actionBody, acted := user.postAction(t, object.ID, "set_body",
			map[string]any{"body": body}, 1)
		requireStatus(t, status, http.StatusOK, actionBody)
		if acted.Version != 2 || acted.State["body"] != body {
			t.Fatalf("action version=%d bodyBytes=%d",
				acted.Version, len([]byte(acted.State["body"].(string))))
		}

		var (
			stateBytes   int
			payloadBytes int
			eventState   int
			action       string
			inputBody    string
			equal        bool
		)
		if err := env.store.Pool().QueryRow(t.Context(), `
SELECT octet_length(o.state::text),
       octet_length(e.payload::text),
       octet_length((e.payload->'state_after')::text),
       e.payload->>'action',
       e.payload->'input'->>'body',
       (e.payload->'state_after') = o.state
  FROM living_objects o
  JOIN object_events e ON e.object_id = o.id
 WHERE o.id = $1 AND e.event_type = 'object.state_changed' AND e.object_version = 2`,
			mustUUID(t, object.ID)).Scan(&stateBytes, &payloadBytes, &eventState, &action, &inputBody, &equal); err != nil {
			t.Fatalf("read max-capacity event: %v", err)
		}
		if stateBytes != 65536 || eventState != 65536 {
			t.Fatalf("state bytes = %d, event state_after bytes = %d, want 65536/65536", stateBytes, eventState)
		}
		if payloadBytes != 131064 {
			t.Fatalf("event payload bytes = %d, want 131064", payloadBytes)
		}
		if payloadBytes <= 98304 || payloadBytes >= 132352 {
			t.Fatalf("payload bytes = %d is not strictly inside (98304,132352)", payloadBytes)
		}
		if action != "set_body" || inputBody != body || !equal {
			t.Fatalf("event mismatch action=%q inputBytes=%d stateEqual=%v", action, len([]byte(inputBody)), equal)
		}
		if got := env.count(t, `
SELECT count(*) FROM object_events
 WHERE object_id = $1 AND object_version = 2 AND event_type = 'object.state_changed'`,
			mustUUID(t, object.ID)); got != 1 {
			t.Fatalf("version 2 state_changed events = %d, want 1", got)
		}
		if got := env.count(t, `
SELECT count(*) FROM audit_events
 WHERE actor_user_id = $1 AND action = 'object.action' AND resource_id = $2`,
			user.id, mustUUID(t, object.ID)); got != 1 {
			t.Fatalf("object.action audit rows = %d, want 1", got)
		}
	})

	t.Run("proposal apply near capability and state maxima", func(t *testing.T) {
		object := user.createObject(t, "collaborative", "x", map[string]any{"body": ""})
		body := strings.Repeat("汉", 14577) + strings.Repeat("😀", 5423)
		if runes := len([]rune(body)); runes != 20000 {
			t.Fatalf("body runes = %d, want 20000", runes)
		}
		if bytes := len([]byte(body)); bytes != 65423 {
			t.Fatalf("body bytes = %d, want 65423", bytes)
		}

		status, response := proposeSetBody(t, user, object.ID, body)
		requireStatus(t, status, http.StatusOK, response)
		proposalID := proposalIDFromResponse(t, response)

		applyStatus, applyBody := applyProposal(t, user, object.ID, proposalID, 1)
		requireStatus(t, applyStatus, http.StatusOK, applyBody)
		proposalStatus, applied := applyProposalResponse(t, applyBody)
		if proposalStatus != "applied" || applied.Version != 2 || applied.State["body"] != body {
			t.Fatalf("apply proposal=%s version=%d", proposalStatus, applied.Version)
		}

		var (
			stateBytes   int
			payloadBytes int
			action       string
			inputBody    string
		)
		if err := env.store.Pool().QueryRow(t.Context(), `
SELECT octet_length(o.state::text),
       octet_length(e.payload::text),
       e.payload->>'action',
       e.payload->'input'->>'body'
  FROM living_objects o
  JOIN object_events e ON e.object_id = o.id
 WHERE o.id = $1 AND e.event_type = 'object.state_changed' AND e.object_version = 2`,
			mustUUID(t, object.ID)).Scan(&stateBytes, &payloadBytes, &action, &inputBody); err != nil {
			t.Fatalf("read proposal capacity event: %v", err)
		}
		if stateBytes != 65493 || payloadBytes != 130978 {
			t.Fatalf("state bytes = %d, payload bytes = %d, want 65493/130978", stateBytes, payloadBytes)
		}
		if action != "set_body" || inputBody != body {
			t.Fatalf("event mismatch action=%q inputBytes=%d", action, len([]byte(inputBody)))
		}
		var proposalStatusDB string
		var version int
		if err := env.store.Pool().QueryRow(t.Context(), `
SELECT o.version, p.status
  FROM living_objects o
  JOIN object_proposals p ON p.object_id = o.id
 WHERE o.id = $1 AND p.id = $2`,
			mustUUID(t, object.ID), mustUUID(t, proposalID)).Scan(&version, &proposalStatusDB); err != nil {
			t.Fatalf("read proposal state: %v", err)
		}
		if version != 2 || proposalStatusDB != "applied" {
			t.Fatalf("db version=%d proposal=%s", version, proposalStatusDB)
		}
		countVersionTwoEvents := func() int {
			return env.count(t, `
SELECT count(*) FROM object_events
 WHERE object_id = $1 AND object_version = 2
   AND event_type IN ('object.state_changed','ai.proposal_applied')`,
				mustUUID(t, object.ID))
		}
		if got := countVersionTwoEvents(); got != 2 {
			t.Fatalf("version 2 events = %d, want 2", got)
		}
		if got := env.count(t, `
SELECT count(*) FROM audit_events
 WHERE actor_user_id = $1 AND action = 'object.proposal_apply' AND resource_id = $2`,
			user.id, mustUUID(t, object.ID)); got != 1 {
			t.Fatalf("apply audit rows = %d, want 1", got)
		}

		// Idempotent retry: no second version, event pair, or audit row.
		replayStatus, replayBody := applyProposal(t, user, object.ID, proposalID, 1)
		requireStatus(t, replayStatus, http.StatusOK, replayBody)
		_, replayed := applyProposalResponse(t, replayBody)
		if replayed.Version != 2 {
			t.Fatalf("replay version = %d", replayed.Version)
		}
		if got := countVersionTwoEvents(); got != 2 {
			t.Fatalf("replay wrote events: version 2 events = %d", got)
		}
		if got := env.count(t, `
SELECT count(*) FROM audit_events
 WHERE actor_user_id = $1 AND action = 'object.proposal_apply' AND resource_id = $2`,
			user.id, mustUUID(t, object.ID)); got != 1 {
			t.Fatalf("replay wrote apply audit: %d", got)
		}
	})
}

// TestG04StateChangedRejectsOverStateCapacity proves the independent upper
// contract bound. A 20,000-character body one byte above the 64 KiB state
// capacity is rejected with the frozen 413 semantics and no state or event
// side effect. The AI proposal path cannot even transmit it because the
// 64 KiB capability input is exhausted first; that rejection is also tested.
// Raising the event-payload limit does not and must not bypass this bound.
func TestG04StateChangedRejectsOverStateCapacity(t *testing.T) {
	env, _ := newG04Env(t, g04Success)
	user := env.register(t, "g04cap-over")
	object := user.createObject(t, "collaborative", "x", map[string]any{"body": ""})

	body := strings.Repeat("汉", 14533) + strings.Repeat("😀", 5467)
	if runes := len([]rune(body)); runes != 20000 {
		t.Fatalf("body runes = %d", runes)
	}
	if bytes := len([]byte(body)); bytes != 65467 {
		t.Fatalf("body bytes = %d, want 65467", bytes)
	}

	// Human direct action: the Go state marshaler accepts 65,528 compact bytes,
	// then the PostgreSQL state CHECK rejects the 65,537-byte jsonb text.
	actionStatus, actionBody, _ := user.postAction(t, object.ID, "set_body",
		map[string]any{"body": body}, 1)
	requireStatus(t, actionStatus, http.StatusRequestEntityTooLarge, actionBody)
	requireCode(t, actionBody, "payload_too_large")

	var version int
	var stateBody string
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT version, state->>'body' FROM living_objects WHERE id = $1`,
		mustUUID(t, object.ID)).Scan(&version, &stateBody); err != nil {
		t.Fatalf("read object: %v", err)
	}
	if version != 1 || stateBody != "" {
		t.Fatalf("failed action polluted object: version=%d bodyBytes=%d", version, len(stateBody))
	}
	if got := env.count(t, `
SELECT count(*) FROM object_events
 WHERE object_id = $1 AND event_type = 'object.state_changed'`, mustUUID(t, object.ID)); got != 0 {
		t.Fatalf("failed action wrote %d state_changed events", got)
	}
	if got := env.count(t, `
SELECT count(*) FROM audit_events
 WHERE actor_user_id = $1 AND action = 'object.action' AND decision = 'allow' AND resource_id = $2`,
		user.id, mustUUID(t, object.ID)); got != 0 {
		t.Fatalf("failed action wrote %d allow audit rows", got)
	}

	// AI proposal path: the whole capability input (body + 113-byte wrapper)
	// is 65,580 bytes, so the frozen 64 KiB capability input rejects it before
	// a proposal row or event exists.
	proposalStatus, proposalBody := proposeSetBody(t, user, object.ID, body)
	requireStatus(t, proposalStatus, http.StatusRequestEntityTooLarge, proposalBody)
	requireCode(t, proposalBody, "payload_too_large")
	if got := env.count(t, `
SELECT count(*) FROM object_proposals WHERE object_id = $1`, mustUUID(t, object.ID)); got != 0 {
		t.Fatalf("oversized proposal input wrote %d proposal rows", got)
	}
	if got := env.count(t, `
SELECT count(*) FROM object_events
 WHERE object_id = $1 AND event_type = 'ai.proposal_created'`, mustUUID(t, object.ID)); got != 0 {
		t.Fatalf("oversized proposal input wrote %d proposal events", got)
	}
	if got := env.count(t, `
SELECT count(*) FROM audit_events
 WHERE actor_user_id = $1 AND action = 'object.proposal_create' AND resource_id = $2`,
		user.id, mustUUID(t, object.ID)); got != 0 {
		t.Fatalf("oversized proposal input wrote %d create audit rows", got)
	}

	// Emoji-only 20,000 characters (~80,000 bytes) exceeds the state capacity
	// on the human path and the capability input on the proposal path.
	emojiStatus, emojiBody, _ := user.postAction(t, object.ID, "set_body",
		map[string]any{"body": strings.Repeat("😀", 20000)}, 1)
	requireStatus(t, emojiStatus, http.StatusRequestEntityTooLarge, emojiBody)
	requireCode(t, emojiBody, "payload_too_large")
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT version FROM living_objects WHERE id = $1`,
		mustUUID(t, object.ID)).Scan(&version); err != nil {
		t.Fatalf("read version after oversized action: %v", err)
	}
	if version != 1 {
		t.Fatalf("emoji action changed version to %d", version)
	}
	if got := env.count(t, `
SELECT count(*) FROM object_events
 WHERE object_id = $1 AND event_type = 'object.state_changed'`, mustUUID(t, object.ID)); got != 0 {
		t.Fatalf("emoji action wrote %d state_changed events", got)
	}
	if got := env.count(t, `
SELECT count(*) FROM audit_events
 WHERE actor_user_id = $1 AND action = 'object.action' AND decision = 'allow' AND resource_id = $2`,
		user.id, mustUUID(t, object.ID)); got != 0 {
		t.Fatalf("emoji action wrote %d allow audit rows", got)
	}
}

// TestG04BodyCharacterLimit proves the first of the two independent V1 limits
// on its own: 20,001 Unicode code points are rejected even though 20,001 ASCII
// bytes are far below the 64 KiB state capacity. The second limit is covered
// by TestG04StateChangedPayloadMaxLegal and
// TestG04StateChangedRejectsOverStateCapacity at 65,536/65,537 bytes.
func TestG04BodyCharacterLimit(t *testing.T) {
	env, _ := newG04Env(t, g04Success)
	user := env.register(t, "g04cap-charlimit")
	object := user.createObject(t, "collaborative", "x", map[string]any{"body": ""})
	body := strings.Repeat("a", 20001)
	if runes := len([]rune(body)); runes != 20001 {
		t.Fatalf("body runes = %d", runes)
	}

	status, actionBody, _ := user.postAction(t, object.ID, "set_body",
		map[string]any{"body": body}, 1)
	requireStatus(t, status, http.StatusBadRequest, actionBody)
	requireCode(t, actionBody, "invalid_request")
	var version int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT version FROM living_objects WHERE id = $1`,
		mustUUID(t, object.ID)).Scan(&version); err != nil {
		t.Fatalf("read object: %v", err)
	}
	if version != 1 {
		t.Fatalf("20,001-character action changed version to %d", version)
	}
	if got := env.count(t, `
SELECT count(*) FROM object_events
 WHERE object_id = $1 AND event_type = 'object.state_changed'`, mustUUID(t, object.ID)); got != 0 {
		t.Fatalf("20,001-character action wrote %d state_changed events", got)
	}
	if got := env.count(t, `
SELECT count(*) FROM audit_events
 WHERE actor_user_id = $1 AND action = 'object.action' AND decision = 'allow' AND resource_id = $2`,
		user.id, mustUUID(t, object.ID)); got != 0 {
		t.Fatalf("20,001-character action wrote %d allow audit rows", got)
	}

	proposalStatus, proposalBody := proposeSetBody(t, user, object.ID, body)
	requireStatus(t, proposalStatus, http.StatusBadRequest, proposalBody)
	requireCode(t, proposalBody, "invalid_request")
	if got := env.count(t, `
SELECT count(*) FROM object_proposals WHERE object_id = $1`, mustUUID(t, object.ID)); got != 0 {
		t.Fatalf("20,001-character proposal wrote %d proposal rows", got)
	}
	if got := env.count(t, `
SELECT count(*) FROM object_events
 WHERE object_id = $1 AND event_type = 'ai.proposal_created'`, mustUUID(t, object.ID)); got != 0 {
		t.Fatalf("20,001-character proposal wrote %d proposal events", got)
	}
}

// insertSyntheticEvent writes a payload whose jsonb text length is exactly
// target bytes. {"pad": "<target-11 x>"} renders as target bytes. Synthetic
// rows exist only to exercise the storage CHECK boundary; they are not
// claimed to be business-valid events.
func insertSyntheticEvent(t *testing.T, env *testEnv, objectID, eventType string, version, target int) error {
	t.Helper()
	if target < 11 {
		t.Fatalf("target %d too small", target)
	}
	payload := fmt.Sprintf(`jsonb_build_object('pad', repeat('x', %d))`, target-11)
	_, err := env.store.Pool().Exec(t.Context(), `
INSERT INTO object_events (id, object_id, object_version, event_type, actor_type, payload)
VALUES ($1,$2,$3,$4,'system', `+payload+`)`,
		uuid.NewString(), objectID, version, eventType)
	return err
}

func assertPayloadConstraintError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("oversized event payload was accepted")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" ||
		pgErr.ConstraintName != "object_events_payload_size" {
		t.Fatalf("error = %v, want CHECK violation on object_events_payload_size", err)
	}
}

func TestG04ObjectEventPayloadCheckBoundaries(t *testing.T) {
	env, _ := newG04Env(t, g04Success)
	user := env.register(t, "g04cap-check")
	object := user.createObject(t, "collaborative", "x", map[string]any{"body": ""})

	// object.state_changed: limit-1 and exactly limit are accepted.
	for _, target := range []int{132351, 132352} {
		if err := insertSyntheticEvent(t, env, object.ID, "object.state_changed", 2, target); err != nil {
			t.Fatalf("state_changed payload %d rejected: %v", target, err)
		}
		var stored int
		if err := env.store.Pool().QueryRow(t.Context(), `
SELECT octet_length(payload::text) FROM object_events
 WHERE object_id = $1 AND event_type = 'object.state_changed'
 ORDER BY seq DESC LIMIT 1`, mustUUID(t, object.ID)).Scan(&stored); err != nil {
			t.Fatalf("read stored payload: %v", err)
		}
		if stored != target {
			t.Fatalf("stored payload = %d, want %d", stored, target)
		}
	}
	// limit+1 is rejected by the database CHECK.
	assertPayloadConstraintError(t,
		insertSyntheticEvent(t, env, object.ID, "object.state_changed", 2, 132353))

	// Every other event keeps the frozen 96 KiB bound.
	if err := insertSyntheticEvent(t, env, object.ID, "object.deleted", 1, 98304); err != nil {
		t.Fatalf("object.deleted payload 98304 rejected: %v", err)
	}
	assertPayloadConstraintError(t,
		insertSyntheticEvent(t, env, object.ID, "object.deleted", 1, 98305))

	// A transaction that updates the object and then fails the event CHECK
	// rolls back both writes.
	rollbackObject := user.createObject(t, "collaborative", "rollback", map[string]any{"body": "original"})
	tx, err := env.store.Pool().Begin(t.Context())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(t.Context(), `
UPDATE living_objects SET state = jsonb_set(state, '{body}', '"polluted"') WHERE id = $1`,
		mustUUID(t, rollbackObject.ID)); err != nil {
		_ = tx.Rollback(t.Context())
		t.Fatalf("update object: %v", err)
	}
	payload := fmt.Sprintf(`jsonb_build_object('pad', repeat('x', %d))`, 132353-11)
	_, insertErr := tx.Exec(t.Context(), `
INSERT INTO object_events (id, object_id, object_version, event_type, actor_type, payload)
VALUES ($1,$2,2,'object.state_changed','system', `+payload+`)`,
		uuid.NewString(), rollbackObject.ID)
	assertPayloadConstraintError(t, insertErr)
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	var body string
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT state->>'body' FROM living_objects WHERE id = $1`,
		mustUUID(t, rollbackObject.ID)).Scan(&body); err != nil {
		t.Fatalf("read rollback object: %v", err)
	}
	if body != "original" {
		t.Fatalf("rollback left state %q", body)
	}
	if got := env.count(t, `
SELECT count(*) FROM object_events
 WHERE object_id = $1 AND payload->>'pad' IS NOT NULL`, mustUUID(t, rollbackObject.ID)); got != 0 {
		t.Fatalf("rollback left %d synthetic events", got)
	}
}

// TestG04ProposalApplyConcurrencyAndVersionConflict covers two simultaneous
// applies of the same proposal (exactly one execution) and a stale expected
// version (no execution).
func TestG04ProposalApplyConcurrencyAndVersionConflict(t *testing.T) {
	env, _ := newG04Env(t, g04Success)
	user := env.register(t, "g04cap-concurrent")
	object := user.createObject(t, "collaborative", "concurrent", map[string]any{"body": "original"})
	body := strings.Repeat("汉", 20000)
	status, response := proposeSetBody(t, user, object.ID, body)
	requireStatus(t, status, http.StatusOK, response)
	proposalID := proposalIDFromResponse(t, response)

	type outcome struct {
		status int
		body   []byte
	}
	results := make(chan outcome, 2)
	var wait sync.WaitGroup
	for index := 0; index < 2; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			applyStatus, applyBody, _ := user.call(t, http.MethodPost,
				"/api/v1/objects/"+object.ID+"/proposals/"+proposalID+"/apply",
				callOptions{Body: map[string]any{"expected_version": 1}})
			results <- outcome{status: applyStatus, body: applyBody}
		}()
	}
	wait.Wait()
	close(results)
	for item := range results {
		requireStatus(t, item.status, http.StatusOK, item.body)
		proposalStatus, applied := applyProposalResponse(t, item.body)
		if proposalStatus != "applied" || applied.Version != 2 {
			t.Fatalf("concurrent apply proposal=%s version=%d", proposalStatus, applied.Version)
		}
	}
	if got := env.count(t, `
SELECT count(*) FROM object_events
 WHERE object_id = $1 AND object_version = 2
   AND event_type IN ('object.state_changed','ai.proposal_applied')`,
		mustUUID(t, object.ID)); got != 2 {
		t.Fatalf("concurrent apply wrote %d version-2 events, want 2", got)
	}
	if got := env.count(t, `
SELECT count(*) FROM audit_events
 WHERE actor_user_id = $1 AND action = 'object.proposal_apply'`, user.id); got != 1 {
		t.Fatalf("concurrent apply wrote %d audit rows, want 1", got)
	}

	// Stale expected version on a fresh proposal: 409 and no writes.
	conflictObject := user.createObject(t, "collaborative", "conflict", map[string]any{"body": "original"})
	conflictStatus, conflictResponse := proposeSetBody(t, user, conflictObject.ID, "changed")
	requireStatus(t, conflictStatus, http.StatusOK, conflictResponse)
	conflictProposal := proposalIDFromResponse(t, conflictResponse)
	applyStatus, applyBody := applyProposal(t, user, conflictObject.ID, conflictProposal, 99)
	requireStatus(t, applyStatus, http.StatusConflict, applyBody)
	requireCode(t, applyBody, "version_conflict")
	if got := env.count(t, `
SELECT count(*) FROM object_events
 WHERE object_id = $1 AND event_type = 'object.state_changed'`,
		mustUUID(t, conflictObject.ID)); got != 0 {
		t.Fatalf("version conflict wrote %d events", got)
	}
	if got := env.count(t, `
SELECT count(*) FROM audit_events
 WHERE actor_user_id = $1 AND resource_id = $2 AND action = 'object.proposal_apply'`,
		user.id, mustUUID(t, conflictObject.ID)); got != 0 {
		t.Fatalf("version conflict wrote %d audit rows", got)
	}
	var proposalStatus string
	var version int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT o.version, p.status
  FROM living_objects o JOIN object_proposals p ON p.object_id = o.id
 WHERE o.id = $1 AND p.id = $2`,
		mustUUID(t, conflictObject.ID), mustUUID(t, conflictProposal)).Scan(&version, &proposalStatus); err != nil {
		t.Fatalf("read conflict state: %v", err)
	}
	if version != 1 || proposalStatus != "pending" {
		t.Fatalf("version conflict version=%d proposal=%s", version, proposalStatus)
	}

	// Drift-only branch: the proposal and the caller both expect version 1,
	// but a human action already advanced the object to version 2.
	driftObject := user.createObject(t, "collaborative", "drift", map[string]any{"body": "original"})
	driftProposalStatus, driftResponse := proposeSetBody(t, user, driftObject.ID, "proposal body")
	requireStatus(t, driftProposalStatus, http.StatusOK, driftResponse)
	driftProposal := proposalIDFromResponse(t, driftResponse)
	humanStatus, humanBody, humanObject := user.postAction(t, driftObject.ID, "set_summary",
		map[string]any{"summary": "human advanced the object"}, 1)
	requireStatus(t, humanStatus, http.StatusOK, humanBody)
	if humanObject.Version != 2 {
		t.Fatalf("human action version = %d", humanObject.Version)
	}
	applyStatus, applyBody = applyProposal(t, user, driftObject.ID, driftProposal, 1)
	requireStatus(t, applyStatus, http.StatusConflict, applyBody)
	requireCode(t, applyBody, "version_conflict")
	if got := env.count(t, `
SELECT count(*) FROM object_events
 WHERE object_id = $1 AND object_version = 3`, mustUUID(t, driftObject.ID)); got != 0 {
		t.Fatalf("drift conflict wrote %d version-3 events", got)
	}
	if got := env.count(t, `
SELECT count(*) FROM audit_events
 WHERE actor_user_id = $1 AND resource_id = $2 AND action = 'object.proposal_apply'`,
		user.id, mustUUID(t, driftObject.ID)); got != 0 {
		t.Fatalf("drift conflict wrote %d apply audit rows", got)
	}
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT o.version, p.status
  FROM living_objects o JOIN object_proposals p ON p.object_id = o.id
 WHERE o.id = $1 AND p.id = $2`,
		mustUUID(t, driftObject.ID), mustUUID(t, driftProposal)).Scan(&version, &proposalStatus); err != nil {
		t.Fatalf("read drift state: %v", err)
	}
	if version != 2 || proposalStatus != "pending" {
		t.Fatalf("drift conflict version=%d proposal=%s", version, proposalStatus)
	}
}

// TestG04NULInputRejectedAsInvalidRequest covers a pre-existing error-mapping
// gap exposed by adversarial verification: PostgreSQL jsonb cannot store
// U+0000 and rejects it with SQLSTATE 22P05. The input is not storable object
// state, so it must be a 400 invalid_request with no side effects, not a 500.
func TestG04NULInputRejectedAsInvalidRequest(t *testing.T) {
	env, _ := newG04Env(t, g04Success)
	user := env.register(t, "g04cap-nul")
	object := user.createObject(t, "collaborative", "x", map[string]any{"body": ""})
	body := "a\x00b"

	status, actionBody, _ := user.postAction(t, object.ID, "set_body",
		map[string]any{"body": body}, 1)
	requireStatus(t, status, http.StatusBadRequest, actionBody)
	requireCode(t, actionBody, "invalid_request")
	var version int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT version FROM living_objects WHERE id = $1`,
		mustUUID(t, object.ID)).Scan(&version); err != nil {
		t.Fatalf("read object: %v", err)
	}
	if version != 1 {
		t.Fatalf("NUL action changed version to %d", version)
	}
	if got := env.count(t, `
SELECT count(*) FROM object_events
 WHERE object_id = $1 AND event_type = 'object.state_changed'`, mustUUID(t, object.ID)); got != 0 {
		t.Fatalf("NUL action wrote %d state_changed events", got)
	}
	if got := env.count(t, `
SELECT count(*) FROM audit_events
 WHERE actor_user_id = $1 AND action = 'object.action' AND decision = 'allow' AND resource_id = $2`,
		user.id, mustUUID(t, object.ID)); got != 0 {
		t.Fatalf("NUL action wrote %d allow audit rows", got)
	}

	proposalStatus, proposalBody := proposeSetBody(t, user, object.ID, body)
	requireStatus(t, proposalStatus, http.StatusBadRequest, proposalBody)
	requireCode(t, proposalBody, "invalid_request")
	if got := env.count(t, `
SELECT count(*) FROM object_proposals WHERE object_id = $1`, mustUUID(t, object.ID)); got != 0 {
		t.Fatalf("NUL proposal wrote %d proposal rows", got)
	}
	if got := env.count(t, `
SELECT count(*) FROM object_events
 WHERE object_id = $1 AND event_type = 'ai.proposal_created'`, mustUUID(t, object.ID)); got != 0 {
		t.Fatalf("NUL proposal wrote %d proposal events", got)
	}
}

// TestG04HTMLSensitiveBodiesWithinCapacity covers a pre-existing measurement
// defect exposed by the A-02 boundary work: Go's json.Marshal HTML-escapes
// &, < and > to six bytes, while the frozen limits are defined on PostgreSQL
// jsonb text, which stores them as one byte. A body that is legal under the
// frozen state/event bounds must not be rejected because of a Go-side
// measurement artifact. The application pre-checks now measure the compact
// non-HTML-escaped JSON (a lower bound on jsonb text); the database CHECK
// remains the final authority.
func TestG04HTMLSensitiveBodiesWithinCapacity(t *testing.T) {
	env, _ := newG04Env(t, g04Success)
	user := env.register(t, "g04cap-html")

	assertPersisted := func(t *testing.T, objectID, body string, stateBytes, payloadBytes int) {
		t.Helper()
		var gotState, gotPayload int
		var inputBody string
		var equal bool
		if err := env.store.Pool().QueryRow(t.Context(), `
SELECT octet_length(o.state::text), octet_length(e.payload::text),
       e.payload->'input'->>'body', (e.payload->'state_after') = o.state
  FROM living_objects o
  JOIN object_events e ON e.object_id = o.id
 WHERE o.id = $1 AND e.event_type = 'object.state_changed' AND e.object_version = 2`,
			mustUUID(t, objectID)).Scan(&gotState, &gotPayload, &inputBody, &equal); err != nil {
			t.Fatalf("read html event: %v", err)
		}
		if gotState != stateBytes || gotPayload != payloadBytes {
			t.Fatalf("state=%d payload=%d, want %d/%d", gotState, gotPayload, stateBytes, payloadBytes)
		}
		if inputBody != body || !equal {
			t.Fatalf("event input mismatch bytes=%d equal=%v", len([]byte(inputBody)), equal)
		}
	}

	t.Run("20,000 ampersands by human action", func(t *testing.T) {
		object := user.createObject(t, "collaborative", "x", map[string]any{"body": ""})
		body := strings.Repeat("&", 20000)
		status, actionBody, acted := user.postAction(t, object.ID, "set_body",
			map[string]any{"body": body}, 1)
		requireStatus(t, status, http.StatusOK, actionBody)
		if acted.Version != 2 || acted.State["body"] != body {
			t.Fatalf("action version=%d", acted.Version)
		}
		assertPersisted(t, object.ID, body, 20070, 40132)
	})

	t.Run("20,000 ampersands by proposal", func(t *testing.T) {
		object := user.createObject(t, "collaborative", "x", map[string]any{"body": ""})
		body := strings.Repeat("&", 20000)
		status, response := proposeSetBodyRaw(t, user, object.ID, body)
		requireStatus(t, status, http.StatusOK, response)
		proposalID := proposalIDFromResponse(t, response)
		applyStatus, applyBody := applyProposal(t, user, object.ID, proposalID, 1)
		requireStatus(t, applyStatus, http.StatusOK, applyBody)
		proposalStatus, applied := applyProposalResponse(t, applyBody)
		if proposalStatus != "applied" || applied.Version != 2 || applied.State["body"] != body {
			t.Fatalf("apply proposal=%s version=%d", proposalStatus, applied.Version)
		}
		assertPersisted(t, object.ID, body, 20070, 40132)
	})

	t.Run("mixed ampersands and Chinese by proposal", func(t *testing.T) {
		object := user.createObject(t, "collaborative", "x", map[string]any{"body": ""})
		body := strings.Repeat("&", 5000) + strings.Repeat("汉", 15000)
		if runes := len([]rune(body)); runes != 20000 {
			t.Fatalf("body runes = %d", runes)
		}
		status, response := proposeSetBodyRaw(t, user, object.ID, body)
		requireStatus(t, status, http.StatusOK, response)
		proposalID := proposalIDFromResponse(t, response)
		applyStatus, applyBody := applyProposal(t, user, object.ID, proposalID, 1)
		requireStatus(t, applyStatus, http.StatusOK, applyBody)
		proposalStatus, applied := applyProposalResponse(t, applyBody)
		if proposalStatus != "applied" || applied.Version != 2 || applied.State["body"] != body {
			t.Fatalf("apply proposal=%s version=%d", proposalStatus, applied.Version)
		}
		assertPersisted(t, object.ID, body, 50070, 100132)
	})

	t.Run("20,000 line separators by human action", func(t *testing.T) {
		object := user.createObject(t, "collaborative", "x", map[string]any{"body": ""})
		body := strings.Repeat(" ", 20000)
		status, actionBody, acted := user.postAction(t, object.ID, "set_body",
			map[string]any{"body": body}, 1)
		requireStatus(t, status, http.StatusOK, actionBody)
		if acted.Version != 2 || acted.State["body"] != body {
			t.Fatalf("action version=%d", acted.Version)
		}
		assertPersisted(t, object.ID, body, 60070, 120132)
	})
}

// TestG04ProposalApplyPersistence verifies the full persisted result for the
// two 20,000-character classes that were previously weak on event, proposal,
// audit, and duplicate-execution assertions.
func TestG04ProposalApplyPersistence(t *testing.T) {
	env, _ := newG04Env(t, g04Success)
	user := env.register(t, "g04cap-persist")
	cases := []struct {
		name         string
		body         string
		stateBytes   int
		payloadBytes int
	}{
		{
			name:         "20,000 ASCII characters",
			body:         strings.Repeat("a", 20000),
			stateBytes:   20070,
			payloadBytes: 40132,
		},
		{
			name: "20,000 mixed characters",
			body: strings.Repeat("a", 10000) + strings.Repeat("汉", 5000) +
				strings.Repeat("😀", 5000),
			stateBytes:   45070,
			payloadBytes: 90132,
		},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			object := user.createObject(t, "collaborative", "x", map[string]any{"body": ""})
			status, response := proposeSetBody(t, user, object.ID, item.body)
			requireStatus(t, status, http.StatusOK, response)
			proposalID := proposalIDFromResponse(t, response)
			applyStatus, applyBody := applyProposal(t, user, object.ID, proposalID, 1)
			requireStatus(t, applyStatus, http.StatusOK, applyBody)
			proposalStatus, applied := applyProposalResponse(t, applyBody)
			if proposalStatus != "applied" || applied.Version != 2 || applied.State["body"] != item.body {
				t.Fatalf("apply proposal=%s version=%d", proposalStatus, applied.Version)
			}
			var stateBytes, payloadBytes int
			var action, inputBody string
			var equal bool
			if err := env.store.Pool().QueryRow(t.Context(), `
SELECT octet_length(o.state::text), octet_length(e.payload::text),
       e.payload->>'action', e.payload->'input'->>'body',
       (e.payload->'state_after') = o.state
  FROM living_objects o
  JOIN object_events e ON e.object_id = o.id
 WHERE o.id = $1 AND e.event_type = 'object.state_changed' AND e.object_version = 2`,
				mustUUID(t, object.ID)).Scan(&stateBytes, &payloadBytes, &action, &inputBody, &equal); err != nil {
				t.Fatalf("read persisted event: %v", err)
			}
			if stateBytes != item.stateBytes || payloadBytes != item.payloadBytes {
				t.Fatalf("state=%d payload=%d, want %d/%d",
					stateBytes, payloadBytes, item.stateBytes, item.payloadBytes)
			}
			if action != "set_body" || inputBody != item.body || !equal {
				t.Fatalf("event mismatch action=%q inputBytes=%d equal=%v",
					action, len([]byte(inputBody)), equal)
			}
			var dbStatus string
			if err := env.store.Pool().QueryRow(t.Context(), `
SELECT status FROM object_proposals WHERE id = $1`,
				mustUUID(t, proposalID)).Scan(&dbStatus); err != nil {
				t.Fatalf("read proposal status: %v", err)
			}
			if dbStatus != "applied" {
				t.Fatalf("db proposal status = %s", dbStatus)
			}
			eventCount := func() int {
				return env.count(t, `
SELECT count(*) FROM object_events
 WHERE object_id = $1 AND object_version = 2
   AND event_type IN ('object.state_changed','ai.proposal_applied')`,
					mustUUID(t, object.ID))
			}
			auditCount := func() int {
				return env.count(t, `
SELECT count(*) FROM audit_events
 WHERE actor_user_id = $1 AND action IN ('object.proposal_create','object.proposal_apply')
   AND resource_id = $2`, user.id, mustUUID(t, object.ID))
			}
			if got := eventCount(); got != 2 {
				t.Fatalf("version 2 events = %d, want 2", got)
			}
			if got := auditCount(); got != 2 {
				t.Fatalf("proposal audit rows = %d, want 2", got)
			}
			replayStatus, replayBody := applyProposal(t, user, object.ID, proposalID, 1)
			requireStatus(t, replayStatus, http.StatusOK, replayBody)
			_, replayed := applyProposalResponse(t, replayBody)
			if replayed.Version != 2 {
				t.Fatalf("replay version = %d", replayed.Version)
			}
			if got := eventCount(); got != 2 {
				t.Fatalf("replay wrote events: %d", got)
			}
			if got := auditCount(); got != 2 {
				t.Fatalf("replay wrote audit: %d", got)
			}
		})
	}
}

// blockEventType installs a test-only CHECK that rejects new rows of one event
// type, mirroring the existing blockAuditAction helper. It is always removed.
func blockEventType(t *testing.T, env *testEnv, eventType string) string {
	t.Helper()
	constraint := "test_block_event_" + strings.NewReplacer(".", "_", "-", "_").Replace(eventType)
	env.exec(t, `ALTER TABLE object_events DROP CONSTRAINT IF EXISTS `+constraint)
	env.exec(t, `ALTER TABLE object_events ADD CONSTRAINT `+constraint+
		` CHECK (event_type <> '`+eventType+`') NOT VALID`)
	t.Cleanup(func() {
		_, _ = env.store.Pool().Exec(context.Background(),
			`ALTER TABLE object_events DROP CONSTRAINT IF EXISTS `+constraint)
	})
	return constraint
}

// TestG04ProposalApplyRollbackOnEventInsertFailure injects a failure at the
// event INSERT after the object row was updated, and proves the whole apply
// transaction rolls back: no version bump, no events, no audit, proposal still
// pending. Removing the injected failure lets the same proposal apply once.
func TestG04ProposalApplyRollbackOnEventInsertFailure(t *testing.T) {
	env, _ := newG04Env(t, g04Success)
	user := env.register(t, "g04cap-rollback")
	object := user.createObject(t, "collaborative", "x", map[string]any{"body": ""})
	body := strings.Repeat("汉", 20000)
	status, response := proposeSetBody(t, user, object.ID, body)
	requireStatus(t, status, http.StatusOK, response)
	proposalID := proposalIDFromResponse(t, response)

	constraint := blockEventType(t, env, "object.state_changed")
	applyStatus, applyBody := applyProposal(t, user, object.ID, proposalID, 1)
	if applyStatus < 500 {
		t.Fatalf("injected event failure status = %d body=%s", applyStatus, applyBody)
	}
	var version int
	var proposalStatus string
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT o.version, p.status FROM living_objects o
  JOIN object_proposals p ON p.object_id = o.id
 WHERE o.id = $1 AND p.id = $2`,
		mustUUID(t, object.ID), mustUUID(t, proposalID)).Scan(&version, &proposalStatus); err != nil {
		t.Fatalf("read rolled-back state: %v", err)
	}
	if version != 1 || proposalStatus != "pending" {
		t.Fatalf("failed event insert version=%d proposal=%s", version, proposalStatus)
	}
	if got := env.count(t, `
SELECT count(*) FROM object_events
 WHERE object_id = $1 AND object_version = 2`, mustUUID(t, object.ID)); got != 0 {
		t.Fatalf("failed event insert left %d version-2 events", got)
	}
	if got := env.count(t, `
SELECT count(*) FROM audit_events
 WHERE actor_user_id = $1 AND action = 'object.proposal_apply' AND resource_id = $2`,
		user.id, mustUUID(t, object.ID)); got != 0 {
		t.Fatalf("failed event insert left %d apply audit rows", got)
	}

	env.exec(t, `ALTER TABLE object_events DROP CONSTRAINT `+constraint)
	retryStatus, retryBody := applyProposal(t, user, object.ID, proposalID, 1)
	requireStatus(t, retryStatus, http.StatusOK, retryBody)
	proposalStatus, applied := applyProposalResponse(t, retryBody)
	if proposalStatus != "applied" || applied.Version != 2 || applied.State["body"] != body {
		t.Fatalf("retry proposal=%s version=%d", proposalStatus, applied.Version)
	}
	if got := env.count(t, `
SELECT count(*) FROM object_events
 WHERE object_id = $1 AND object_version = 2
   AND event_type IN ('object.state_changed','ai.proposal_applied')`,
		mustUUID(t, object.ID)); got != 2 {
		t.Fatalf("retry wrote %d version-2 events, want 2", got)
	}
}
