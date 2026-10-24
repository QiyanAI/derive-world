// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QiyanAI/derive-world/internal/model"
)

// g07Conversation creates a conversation owned by u and returns its id.
func g07Conversation(t *testing.T, u *testUser, title string, objectID *string) string {
	t.Helper()
	return u.createConversation(t, title, objectID)
}

// g07Invocation reads the single model invocation belonging to userID.
func g07Invocation(t *testing.T, env *testEnv, userID string) (status, errorCode string, inputChars, outputChars int) {
	t.Helper()
	err := env.store.Pool().QueryRow(t.Context(), `
SELECT status, COALESCE(error_code, ''), input_chars, output_chars
  FROM model_invocations
 WHERE user_id = $1
 ORDER BY created_at DESC, id DESC
 LIMIT 1`, userID).Scan(&status, &errorCode, &inputChars, &outputChars)
	if err != nil {
		t.Fatalf("read model_invocations: %v", err)
	}
	return status, errorCode, inputChars, outputChars
}

func g07DropTrigger(t *testing.T, env *testEnv, trigger, function string) {
	t.Helper()
	_, _ = env.store.Pool().Exec(context.Background(),
		"DROP TRIGGER IF EXISTS "+trigger+" ON model_invocations")
	_, _ = env.store.Pool().Exec(context.Background(),
		"DROP FUNCTION IF EXISTS "+function+"()")
}

// TestG07QuotaReservationIsDurableBeforeAndAfterCall proves the runtime writes
// a durable pre-call reservation, completes it on success, and that a fresh
// quota instance (a restart) counts it.
func TestG07QuotaReservationIsDurableBeforeAndAfterCall(t *testing.T) {
	env := newTestEnvWithProvider(t, nil, &scriptedProvider{})
	a := env.register(t, "g07durable")
	object := a.createObject(t, "collaborative", "G07 durable quota", map[string]any{
		"body": "durable quota body",
	})
	conversation := g07Conversation(t, a, "G07 durable", &object.ID)

	status, body := a.sendMessage(t, conversation, "Return a short fixture reply.")
	requireStatus(t, status, http.StatusOK, body)
	statusText, errorCode, inputChars, outputChars := g07Invocation(t, env, a.id.String())
	if statusText != "succeeded" || errorCode != "" || inputChars <= 0 || outputChars <= 0 {
		t.Fatalf("invocation = status %q error %q input %d output %d", statusText, errorCode, inputChars, outputChars)
	}

	// A fresh Quota sees the persisted invocation; the in-memory state is gone.
	restarted := model.NewQuota(env.store.Pool(), model.QuotaConfig{
		Mode: "fixture", DailyCalls: 1, DailyInputChars: 1_000_000,
		Concurrency: 1, MaxInputChars: model.MaxModelInputChars,
	})
	if _, err := restarted.Reserve(t.Context(), a.id, 1); !errors.Is(err, model.ErrBudgetExceeded) {
		t.Fatalf("restarted quota reserve error = %v, want ErrBudgetExceeded", err)
	}
}

// TestG07QuotaReservationSurvivesPersistFailure injects a PostgreSQL failure
// on the final success update. The provider call succeeds, but the business
// transaction rolls back; the pre-call reservation must remain counted.
func TestG07QuotaReservationSurvivesPersistFailure(t *testing.T) {
	env := newTestEnvWithProvider(t, nil, &scriptedProvider{})
	a := env.register(t, "g07persistfail")
	object := a.createObject(t, "collaborative", "G07 persist failure", map[string]any{
		"body": "persist failure body",
	})
	conversation := g07Conversation(t, a, "G07 persist failure", &object.ID)

	const trigger = "g07_fail_invocation_update"
	const function = "g07_fail_invocation_update"
	env.exec(t, `CREATE OR REPLACE FUNCTION `+function+`() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'g07 injected model_invocations update failure';
END;
$$ LANGUAGE plpgsql`)
	env.exec(t, `CREATE TRIGGER `+trigger+` BEFORE UPDATE ON model_invocations
FOR EACH ROW EXECUTE FUNCTION `+function+`()`)
	t.Cleanup(func() { g07DropTrigger(t, env, trigger, function) })

	status, body := a.sendMessage(t, conversation, "Return a short fixture reply.")
	if status == http.StatusOK {
		t.Fatalf("persist failure unexpectedly returned 200: %s", body)
	}
	statusText, errorCode, inputChars, _ := g07Invocation(t, env, a.id.String())
	if statusText != "failed" || errorCode != "in_flight" || inputChars <= 0 {
		t.Fatalf("reservation after persist failure = status %q error %q input %d", statusText, errorCode, inputChars)
	}
	if got := env.count(t, `SELECT count(*) FROM ai_messages WHERE conversation_id = $1`, conversation); got != 0 {
		t.Fatalf("rolled-back turn left %d ai_messages", got)
	}

	// Restart semantics: a fresh quota sees the reservation and refuses another call.
	restarted := model.NewQuota(env.store.Pool(), model.QuotaConfig{
		Mode: "fixture", DailyCalls: 1, DailyInputChars: 1_000_000,
		Concurrency: 1, MaxInputChars: model.MaxModelInputChars,
	})
	if _, err := restarted.Reserve(t.Context(), a.id, 1); !errors.Is(err, model.ErrBudgetExceeded) {
		t.Fatalf("restarted quota after rollback = %v, want ErrBudgetExceeded", err)
	}
}

// TestG07QuotaReservationCountsProviderFailure proves a provider failure is
// recorded against the pre-call reservation and remains billable-counted.
func TestG07QuotaReservationCountsProviderFailure(t *testing.T) {
	provider := &scriptedProvider{errs: []error{
		&model.Error{Code: model.CodeTimeout, Message: "g07 injected timeout"},
	}}
	env := newTestEnvWithProvider(t, nil, provider)
	a := env.register(t, "g07providerfail")
	object := a.createObject(t, "collaborative", "G07 provider failure", map[string]any{
		"body": "provider failure body",
	})
	conversation := g07Conversation(t, a, "G07 provider failure", &object.ID)

	status, body := a.sendMessage(t, conversation, "Return a short fixture reply.")
	if status == http.StatusOK {
		t.Fatalf("provider failure unexpectedly returned 200: %s", body)
	}
	statusText, errorCode, inputChars, _ := g07Invocation(t, env, a.id.String())
	if statusText != "timeout" || errorCode != string(model.CodeTimeout) || inputChars <= 0 {
		t.Fatalf("failed invocation = status %q error %q input %d", statusText, errorCode, inputChars)
	}
	if got := provider.requestCount(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
}

// TestG07QuotaReservationInsertFailureBlocksProvider injects a PostgreSQL
// failure on the pre-call reservation insert. The provider must not be called.
func TestG07QuotaReservationInsertFailureBlocksProvider(t *testing.T) {
	provider := &scriptedProvider{}
	env := newTestEnvWithProvider(t, nil, provider)
	a := env.register(t, "g07insertfail")
	object := a.createObject(t, "collaborative", "G07 insert failure", map[string]any{
		"body": "insert failure body",
	})
	conversation := g07Conversation(t, a, "G07 insert failure", &object.ID)

	const trigger = "g07_fail_invocation_insert"
	const function = "g07_fail_invocation_insert"
	env.exec(t, `CREATE OR REPLACE FUNCTION `+function+`() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'g07 injected model_invocations insert failure';
END;
$$ LANGUAGE plpgsql`)
	env.exec(t, `CREATE TRIGGER `+trigger+` BEFORE INSERT ON model_invocations
FOR EACH ROW EXECUTE FUNCTION `+function+`()`)
	t.Cleanup(func() { g07DropTrigger(t, env, trigger, function) })

	status, body := a.sendMessage(t, conversation, "Return a short fixture reply.")
	if status == http.StatusOK {
		t.Fatalf("insert failure unexpectedly returned 200: %s", body)
	}
	if got := provider.requestCount(); got != 0 {
		t.Fatalf("provider was called %d times despite reservation insert failure", got)
	}
	if got := env.count(t, `SELECT count(*) FROM model_invocations WHERE user_id = $1`, a.id.String()); got != 0 {
		t.Fatalf("insert failure left %d model_invocations rows", got)
	}
}
