// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

const proposalChangeReply = `{
  "reply":"I propose a body change.",
  "proposed_actions":[
    {"capability_id":"object.propose_change","input":{"action":"set_body","input":{"body":"AI proposed body"},"expected_version":1}}
  ]}`

// createProposalForUser creates an object and one pending proposal through the
// user's Personal AI, returning the object and proposal IDs.
func createProposalForUser(t *testing.T, env *testEnv, user *testUser, title string) (objectResponse, string) {
	t.Helper()
	object := user.createObject(t, "collaborative", title, map[string]any{
		"body": "original body",
	})
	conversationID := user.createConversation(t, title+" chat", &object.ID)
	status, body := user.sendMessage(t, conversationID, "propose a change")
	requireStatus(t, status, http.StatusOK, body)
	turn := decodeTurn(t, body)
	if len(turn.ProposedActions) != 1 || turn.ProposedActions[0].Kind != "proposal" {
		t.Fatalf("expected one proposal: %s", body)
	}
	return object, turn.ProposedActions[0].ProposalID
}

func applyProposal(t *testing.T, user *testUser, objectID, proposalID string, expectedVersion int) (int, []byte) {
	t.Helper()
	status, body, _ := user.call(t, http.MethodPost,
		"/api/v1/objects/"+objectID+"/proposals/"+proposalID+"/apply", callOptions{
			Body: map[string]any{"expected_version": expectedVersion},
		})
	return status, body
}

func assertProposalState(t *testing.T, env *testEnv, objectID, proposalID, wantProposalStatus string, wantVersion int) {
	t.Helper()
	var (
		version int
		status  string
	)
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT o.version, p.status
  FROM living_objects o
  JOIN object_proposals p ON p.object_id = o.id
 WHERE o.id = $1 AND p.id = $2`,
		mustUUID(t, objectID), mustUUID(t, proposalID)).Scan(&version, &status); err != nil {
		t.Fatalf("query proposal state: %v", err)
	}
	if version != wantVersion || status != wantProposalStatus {
		t.Fatalf("object version=%d proposal status=%s, want version=%d status=%s",
			version, status, wantVersion, wantProposalStatus)
	}
}

func countAppliedEvents(t *testing.T, env *testEnv, objectID string) int {
	t.Helper()
	return env.count(t, `
SELECT count(*) FROM object_events
 WHERE object_id = $1
   AND event_type IN ('object.state_changed','ai.proposal_applied')`,
		mustUUID(t, objectID))
}

func assertNoApplyAudit(t *testing.T, env *testEnv, objectID string) {
	t.Helper()
	if got := env.count(t, `
SELECT count(*) FROM audit_events
 WHERE action = 'object.proposal_apply' AND resource_id = $1`,
		mustUUID(t, objectID)); got != 0 {
		t.Fatalf("failed apply wrote %d object.proposal_apply audit rows", got)
	}
}

func assertDelegationDeniedAudit(t *testing.T, env *testEnv, objectID, proposalID string) {
	t.Helper()
	if got := env.count(t, `
SELECT count(*) FROM audit_events
 WHERE action = 'authz.denied' AND decision = 'deny'
   AND reason_code = 'delegation_not_active'
   AND resource_id = $1 AND metadata->>'proposal_id' = $2`,
		mustUUID(t, objectID), proposalID); got < 1 {
		t.Fatal("delegation denial was not audited as authz.denied")
	}
}

// TestG03ProposalApplyRequiresOriginalDelegation is the negative case from
// independent review: a proposal created under an AI delegation must not
// create a new object version after that exact delegation is revoked, even
// though the human user still has contribute and even if a different valid
// delegation exists for the same object and profile.
func TestG03ProposalApplyRequiresOriginalDelegation(t *testing.T) {
	provider := &scriptedProvider{replies: []string{proposalChangeReply}}
	env := newTestEnvWithProvider(t, nil, provider)
	a := env.register(t, "g03prop-a")
	object, proposalID := createProposalForUser(t, env, a, "Proposal revocation")

	for _, item := range a.listDelegations(t) {
		if item.Status == "active" {
			a.revokeDelegation(t, item.ID)
		}
	}

	// No active delegation remains. The user still owns the object and keeps
	// the contribute permission.
	status, body := applyProposal(t, a, object.ID, proposalID, 1)
	requireStatus(t, status, http.StatusForbidden, body)
	requireCode(t, body, "forbidden")
	assertProposalState(t, env, object.ID, proposalID, "pending", 1)
	if countAppliedEvents(t, env, object.ID) != 0 {
		t.Fatal("revoked delegation produced an applied event")
	}
	assertNoApplyAudit(t, env, object.ID)
	assertDelegationDeniedAudit(t, env, object.ID, proposalID)

	// Authorization failure precedes the version check: a stale expected
	// version still returns 403, not 409.
	status, body = applyProposal(t, a, object.ID, proposalID, 99)
	requireStatus(t, status, http.StatusForbidden, body)
	requireCode(t, body, "forbidden")

	// A different valid delegation must not make the old proposal executable:
	// the proposal is bound to the exact delegation that authorized it.
	status, body = a.createDelegation(t, map[string]any{
		"object_id":   object.ID,
		"permissions": []string{"read", "contribute"},
	})
	requireStatus(t, status, http.StatusCreated, body)

	status, body = applyProposal(t, a, object.ID, proposalID, 1)
	requireStatus(t, status, http.StatusForbidden, body)
	requireCode(t, body, "forbidden")
	assertProposalState(t, env, object.ID, proposalID, "pending", 1)
	if countAppliedEvents(t, env, object.ID) != 0 {
		t.Fatal("a different delegation resurrected a revoked proposal")
	}
	assertNoApplyAudit(t, env, object.ID)
	assertDelegationDeniedAudit(t, env, object.ID, proposalID)

	// The human session is unaffected by delegation revocation: a direct
	// action under the still-valid membership must succeed.
	actionStatus, actionBody, acted := a.postAction(t, object.ID, "set_body",
		map[string]any{"body": "human edit"}, 1)
	requireStatus(t, actionStatus, http.StatusOK, actionBody)
	if acted.Version != 2 || acted.State["body"] != "human edit" {
		t.Fatalf("human action = %+v", acted)
	}
}

// TestG03ProposalApplyWithActiveOriginalDelegation is the positive control:
// the exact delegation recorded on the proposal is still active, so the
// proposal applies normally.
func TestG03ProposalApplyWithActiveOriginalDelegation(t *testing.T) {
	provider := &scriptedProvider{replies: []string{proposalChangeReply}}
	env := newTestEnvWithProvider(t, nil, provider)
	a := env.register(t, "g03propok-a")

	// Replace the default global delegation with an object-scoped one so the
	// stored proposal.delegation_id is the scoped grant.
	for _, item := range a.listDelegations(t) {
		if item.Status == "active" {
			a.revokeDelegation(t, item.ID)
		}
	}
	object := a.createObject(t, "collaborative", "Active delegation object", map[string]any{
		"body": "original body",
	})
	status, body := a.createDelegation(t, map[string]any{
		"object_id":   object.ID,
		"permissions": []string{"read", "contribute"},
	})
	requireStatus(t, status, http.StatusCreated, body)
	conversationID := a.createConversation(t, "Active delegation chat", &object.ID)
	messageStatus, messageBody := a.sendMessage(t, conversationID, "propose a change")
	requireStatus(t, messageStatus, http.StatusOK, messageBody)
	turn := decodeTurn(t, messageBody)
	if len(turn.ProposedActions) != 1 || turn.ProposedActions[0].Kind != "proposal" {
		t.Fatalf("expected one proposal: %s", messageBody)
	}
	proposalID := turn.ProposedActions[0].ProposalID

	applyStatus, applyBody := applyProposal(t, a, object.ID, proposalID, 1)
	requireStatus(t, applyStatus, http.StatusOK, applyBody)
	var applied struct {
		Proposal struct {
			Status string `json:"status"`
		} `json:"proposal"`
		Object objectResponse `json:"object"`
	}
	if err := json.Unmarshal(applyBody, &applied); err != nil {
		t.Fatalf("decode apply: %v", err)
	}
	if applied.Proposal.Status != "applied" || applied.Object.Version != 2 ||
		applied.Object.State["body"] != "AI proposed body" {
		t.Fatalf("applied = %+v", applied)
	}
}

// TestG03ProposalApplyRejectsExpiredOriginalDelegation proves the same
// execution-time check covers expiry, not only explicit revocation.
func TestG03ProposalApplyRejectsExpiredOriginalDelegation(t *testing.T) {
	provider := &scriptedProvider{replies: []string{proposalChangeReply}}
	env := newTestEnvWithProvider(t, nil, provider)
	a := env.register(t, "g03propexp-a")
	object, proposalID := createProposalForUser(t, env, a, "Expired delegation")

	env.exec(t, `
UPDATE delegation_grants
   SET created_at = now() - interval '2 days',
       expires_at = now() - interval '1 day'
 WHERE id = (SELECT delegation_id FROM object_proposals WHERE id = $1)`,
		mustUUID(t, proposalID))

	status, body := applyProposal(t, a, object.ID, proposalID, 1)
	requireStatus(t, status, http.StatusForbidden, body)
	requireCode(t, body, "forbidden")
	assertProposalState(t, env, object.ID, proposalID, "pending", 1)
	if countAppliedEvents(t, env, object.ID) != 0 {
		t.Fatal("expired delegation produced an applied event")
	}
	assertNoApplyAudit(t, env, object.ID)
	assertDelegationDeniedAudit(t, env, object.ID, proposalID)
}

// TestG03ProposalApplyRejectsRevokedContributingDelegation covers the merged
// delegation case: the proposal's recorded delegation is a newer read-only
// grant, while contribute came from the older default delegation. Revoking the
// contributing grant must still block the pending proposal even though the
// recorded grant remains active.
func TestG03ProposalApplyRejectsRevokedContributingDelegation(t *testing.T) {
	provider := &scriptedProvider{replies: []string{proposalChangeReply}}
	env := newTestEnvWithProvider(t, nil, provider)
	a := env.register(t, "g03propmulti-a")

	// A newer read-only global delegation becomes Effective.DelegationID.
	status, body := a.createDelegation(t, map[string]any{
		"permissions": []string{"read"},
	})
	requireStatus(t, status, http.StatusCreated, body)
	var readOnly delegationResponse
	if err := json.Unmarshal(body, &readOnly); err != nil {
		t.Fatalf("decode read-only delegation: %v", err)
	}

	object, proposalID := createProposalForUser(t, env, a, "Merged delegation")
	var recorded string
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT delegation_id::text FROM object_proposals WHERE id = $1`,
		mustUUID(t, proposalID)).Scan(&recorded); err != nil {
		t.Fatalf("read recorded delegation: %v", err)
	}
	if recorded != readOnly.ID {
		t.Fatalf("recorded delegation = %s, want newer read-only %s", recorded, readOnly.ID)
	}

	// Revoke only the older default delegation that supplied contribute.
	revoked := false
	for _, item := range a.listDelegations(t) {
		if item.Status != "active" {
			continue
		}
		for _, permission := range item.Permissions {
			if permission == "contribute" {
				a.revokeDelegation(t, item.ID)
				revoked = true
			}
		}
	}
	if !revoked {
		t.Fatal("did not find the contributing delegation")
	}

	status, body = applyProposal(t, a, object.ID, proposalID, 1)
	requireStatus(t, status, http.StatusForbidden, body)
	requireCode(t, body, "forbidden")
	assertProposalState(t, env, object.ID, proposalID, "pending", 1)
	if countAppliedEvents(t, env, object.ID) != 0 {
		t.Fatal("revoked contributing delegation produced an applied event")
	}
	assertNoApplyAudit(t, env, object.ID)
	assertDelegationDeniedAudit(t, env, object.ID, proposalID)
}

// TestG03ProposalApplyRaceWithDelegationRevocation proves the revalidation is
// inside the apply transaction and takes a row lock: when a revocation commits
// while the apply waits, the apply must observe the revoked row and fail
// instead of committing a new state version after the revocation.
func TestG03ProposalApplyRaceWithDelegationRevocation(t *testing.T) {
	provider := &scriptedProvider{replies: []string{proposalChangeReply}}
	env := newTestEnvWithProvider(t, nil, provider)
	a := env.register(t, "g03proprace-a")
	object, proposalID := createProposalForUser(t, env, a, "Race delegation")

	var delegationID string
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT delegation_id::text FROM object_proposals WHERE id = $1`,
		mustUUID(t, proposalID)).Scan(&delegationID); err != nil {
		t.Fatalf("read proposal delegation: %v", err)
	}
	parsedDelegation, err := uuid.Parse(delegationID)
	if err != nil {
		t.Fatalf("parse delegation id: %v", err)
	}

	tx, err := env.store.Pool().Begin(t.Context())
	if err != nil {
		t.Fatalf("begin lock transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var locked string
	if err := tx.QueryRow(t.Context(), `
SELECT id::text FROM delegation_grants WHERE id = $1 FOR UPDATE`,
		parsedDelegation).Scan(&locked); err != nil {
		t.Fatalf("lock delegation: %v", err)
	}

	type outcome struct {
		status int
		body   []byte
	}
	result := make(chan outcome, 1)
	go func() {
		origin := env.origin
		csrf := a.csrf
		status, body, _ := env.call(t, a.client, http.MethodPost,
			"/api/v1/objects/"+object.ID+"/proposals/"+proposalID+"/apply", callOptions{
				Origin: &origin, CSRFToken: &csrf,
				Body: map[string]any{"expected_version": 1},
			})
		result <- outcome{status: status, body: body}
	}()

	// Wait until the apply transaction is blocked on the delegation row lock.
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case early := <-result:
			t.Fatalf("apply was not blocked by the in-flight revocation: %d %s",
				early.status, early.body)
		default:
		}
		var blocked int
		if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM pg_stat_activity
 WHERE datname = current_database()
   AND wait_event_type = 'Lock'
   AND query ILIKE '%delegation_grants%'`).Scan(&blocked); err != nil {
			t.Fatalf("inspect waiting queries: %v", err)
		}
		if blocked > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("apply did not block on the delegation row lock")
		}
		time.Sleep(25 * time.Millisecond)
	}

	if _, err := tx.Exec(t.Context(), `
UPDATE delegation_grants
   SET revoked_at = GREATEST(now(), created_at)
 WHERE id = $1`, parsedDelegation); err != nil {
		t.Fatalf("revoke delegation: %v", err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatalf("commit revocation: %v", err)
	}

	select {
	case got := <-result:
		requireStatus(t, got.status, http.StatusForbidden, got.body)
		requireCode(t, got.body, "forbidden")
	case <-time.After(10 * time.Second):
		t.Fatal("apply did not finish after the revocation committed")
	}
	assertProposalState(t, env, object.ID, proposalID, "pending", 1)
	if countAppliedEvents(t, env, object.ID) != 0 {
		t.Fatal("race with revocation produced an applied event")
	}
	assertNoApplyAudit(t, env, object.ID)
	assertDelegationDeniedAudit(t, env, object.ID, proposalID)
}

// TestG03ProposalTerminalStatusPrecedesDelegationCheck proves the new check is
// scoped to pending proposals: denied and expired proposals still return 409
// even after the originating delegation is revoked.
func TestG03ProposalTerminalStatusPrecedesDelegationCheck(t *testing.T) {
	provider := &scriptedProvider{replies: []string{proposalChangeReply, proposalChangeReply}}
	env := newTestEnvWithProvider(t, nil, provider)
	a := env.register(t, "g03propterm-a")
	deniedObject, deniedProposal := createProposalForUser(t, env, a, "Denied proposal")
	expiredObject, expiredProposal := createProposalForUser(t, env, a, "Expired proposal")

	denyStatus, denyBody, _ := a.call(t, http.MethodPost,
		"/api/v1/objects/"+deniedObject.ID+"/proposals/"+deniedProposal+"/deny", callOptions{})
	requireStatus(t, denyStatus, http.StatusOK, denyBody)
	env.exec(t, `
UPDATE object_proposals SET created_at = now() - interval '25 hours'
 WHERE id = $1`, mustUUID(t, expiredProposal))

	for _, item := range a.listDelegations(t) {
		if item.Status == "active" {
			a.revokeDelegation(t, item.ID)
		}
	}

	status, body := applyProposal(t, a, deniedObject.ID, deniedProposal, 1)
	requireStatus(t, status, http.StatusConflict, body)
	requireCode(t, body, "conflict")
	status, body = applyProposal(t, a, expiredObject.ID, expiredProposal, 1)
	requireStatus(t, status, http.StatusConflict, body)
	requireCode(t, body, "conflict")

	for _, objectID := range []string{deniedObject.ID, expiredObject.ID} {
		if countAppliedEvents(t, env, objectID) != 0 {
			t.Fatalf("terminal proposal %s produced an applied event", objectID)
		}
		assertNoApplyAudit(t, env, objectID)
	}
}

// TestG03AppliedProposalReplayAfterRevocation pins the frozen boundary:
// re-applying an already applied proposal returns the current object without
// writing a new version, even after the originating delegation is revoked.
// Only a new state modification is forbidden.
func TestG03AppliedProposalReplayAfterRevocation(t *testing.T) {
	provider := &scriptedProvider{replies: []string{proposalChangeReply}}
	env := newTestEnvWithProvider(t, nil, provider)
	a := env.register(t, "g03propreplay-a")
	object, proposalID := createProposalForUser(t, env, a, "Replay after revocation")

	status, body := applyProposal(t, a, object.ID, proposalID, 1)
	requireStatus(t, status, http.StatusOK, body)

	for _, item := range a.listDelegations(t) {
		if item.Status == "active" {
			a.revokeDelegation(t, item.ID)
		}
	}

	status, body = applyProposal(t, a, object.ID, proposalID, 1)
	requireStatus(t, status, http.StatusOK, body)
	var replay struct {
		Object objectResponse `json:"object"`
	}
	if err := json.Unmarshal(body, &replay); err != nil {
		t.Fatalf("decode replay: %v", err)
	}
	if replay.Object.Version != 2 {
		t.Fatalf("replay changed object version to %d", replay.Object.Version)
	}
	assertProposalState(t, env, object.ID, proposalID, "applied", 2)
}
