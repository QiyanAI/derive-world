// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QiyanAI/derive-world/internal/config"
	"github.com/QiyanAI/derive-world/internal/model"
)

func g06FixtureBaseline(t *testing.T) int {
	t.Helper()
	database := openTestStore(t)
	calls, _, err := model.NewRecorder().GlobalDayUsage(t.Context(), database.Pool(), "fixture")
	if err != nil {
		t.Fatalf("read fixture baseline: %v", err)
	}
	return calls
}

func g06ValidTurn() string {
	return `{"reply":"scripted turn","proposed_actions":[{"capability_id":"object.propose_change","input":{"action":"set_body","input":{"body":"G06 budget body"},"expected_version":1}}]}`
}

func TestG06GlobalBudgetStopsBeforeProviderCall(t *testing.T) {
	baseline := g06FixtureBaseline(t)
	provider := &scriptedProvider{replies: []string{g06ValidTurn(), g06ValidTurn()}}
	env := newTestEnvWithProvider(t, func(cfg *config.Config) {
		cfg.Model.GlobalDailyCalls = baseline + 1
		cfg.Model.DailyCallsPerUser = 10
	}, provider)
	a := env.register(t, "g06budgeta")
	b := env.register(t, "g06budgetb")
	object := a.createObject(t, "collaborative", "G06 budget parent", map[string]any{"body": "base"})
	invitation := a.createInvitation(t, object.ID, []string{"read", "contribute", "derive"}, 1)
	joinStatus, joinBody := b.acceptInvitation(t, *invitation.Token)
	requireStatus(t, joinStatus, http.StatusOK, joinBody)

	aConversation := a.createConversation(t, "G06 budget A", &object.ID)
	status, body := a.sendMessage(t, aConversation, "first")
	requireStatus(t, status, http.StatusOK, body)

	bConversation := b.createConversation(t, "G06 budget B", &object.ID)
	status, body = b.sendMessage(t, bConversation, "second")
	requireStatus(t, status, http.StatusTooManyRequests, body)
	requireCode(t, body, "budget_exceeded")

	if provider.requestCount() != 1 {
		t.Fatalf("provider requests = %d, want exactly 1 (B must be refused before the call)",
			provider.requestCount())
	}
	var bInvocations int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM model_invocations WHERE user_id = $1`, b.id).Scan(&bInvocations); err != nil {
		t.Fatalf("count B invocations: %v", err)
	}
	if bInvocations != 0 {
		t.Fatalf("B recorded %d model invocations despite the global stop", bInvocations)
	}
	var deniedAudit int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM audit_events
 WHERE action = 'rate.limit_hit' AND decision = 'deny'
   AND actor_user_id = $1 AND metadata ->> 'reason' = 'global_budget_exceeded'`,
		b.id).Scan(&deniedAudit); err != nil {
		t.Fatalf("read budget audit: %v", err)
	}
	if deniedAudit == 0 {
		t.Fatal("the global budget refusal was not audited")
	}
}

func TestG06GlobalBudgetSurvivesRestart(t *testing.T) {
	baseline := g06FixtureBaseline(t)
	provider := &scriptedProvider{replies: []string{g06ValidTurn()}}
	env := newTestEnvWithProvider(t, func(cfg *config.Config) {
		cfg.Model.GlobalDailyCalls = baseline + 1
		cfg.Model.DailyCallsPerUser = 10
	}, provider)
	a := env.register(t, "g06restarta")
	object := a.createObject(t, "collaborative", "G06 restart parent", map[string]any{"body": "base"})
	conversation := a.createConversation(t, "G06 restart A", &object.ID)
	status, body := a.sendMessage(t, conversation, "spend the budget")
	requireStatus(t, status, http.StatusOK, body)

	// A second app instance with a fresh pool represents a process restart.
	// The deployment-wide counter must be reseeded from model_invocations.
	secondProvider := &scriptedProvider{replies: []string{g06ValidTurn()}}
	restarted := newTestEnvWithProvider(t, func(cfg *config.Config) {
		cfg.Model.GlobalDailyCalls = baseline + 1
		cfg.Model.DailyCallsPerUser = 10
	}, secondProvider)
	c := restarted.register(t, "g06restartc")
	secondObject := c.createObject(t, "collaborative", "G06 restart C", map[string]any{"body": "base"})
	secondConversation := c.createConversation(t, "G06 restart C", &secondObject.ID)
	status, body = c.sendMessage(t, secondConversation, "after restart")
	requireStatus(t, status, http.StatusTooManyRequests, body)
	requireCode(t, body, "budget_exceeded")
	if secondProvider.requestCount() != 0 {
		t.Fatalf("restarted process made %d provider calls despite the spent budget",
			secondProvider.requestCount())
	}
}

func TestG06KillSwitchRefusesBeforeProviderCall(t *testing.T) {
	provider := &scriptedProvider{replies: []string{g06ValidTurn()}}
	env := newTestEnvWithProvider(t, func(cfg *config.Config) {
		cfg.Model.KillSwitch = true
	}, provider)
	a := env.register(t, "g06kills")
	object := a.createObject(t, "collaborative", "G06 kill parent", map[string]any{"body": "base"})
	conversation := a.createConversation(t, "G06 kill", &object.ID)
	status, body := a.sendMessage(t, conversation, "blocked")
	requireStatus(t, status, http.StatusBadGateway, body)
	requireCode(t, body, "model_unavailable")
	if provider.requestCount() != 0 {
		t.Fatalf("kill switch made %d provider calls", provider.requestCount())
	}
	var audited int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM audit_events
 WHERE action = 'rate.limit_hit' AND decision = 'deny'
   AND actor_user_id = $1 AND metadata ->> 'reason' = 'kill_switch'`,
		a.id).Scan(&audited); err != nil {
		t.Fatalf("read kill-switch audit: %v", err)
	}
	if audited == 0 {
		t.Fatal("the kill-switch refusal was not audited")
	}
}

func TestG06PerCallInputLimitRefusesBeforeProviderCall(t *testing.T) {
	provider := &scriptedProvider{replies: []string{g06ValidTurn()}}
	env := newTestEnvWithProvider(t, func(cfg *config.Config) {
		cfg.Model.MaxInputChars = 1024
		cfg.Model.IncludePrivateContext = true
	}, provider)
	a := env.register(t, "g06input")
	a.setPrivateContext(t, strings.Repeat("x", 2000))
	object := a.createObject(t, "collaborative", "G06 input parent", map[string]any{"body": "base"})
	conversation := a.createConversation(t, "G06 input", &object.ID)
	status, body := a.sendMessage(t, conversation, "too large")
	requireStatus(t, status, http.StatusRequestEntityTooLarge, body)
	requireCode(t, body, "payload_too_large")
	if provider.requestCount() != 0 {
		t.Fatalf("oversized input made %d provider calls", provider.requestCount())
	}
	if got := env.count(t, `SELECT count(*) FROM ai_messages WHERE conversation_id=$1`,
		mustUUID(t, conversation)); got != 0 {
		t.Fatalf("oversized input persisted %d messages", got)
	}
}

func TestG06UpstreamRateLimitAndServerError(t *testing.T) {
	cases := []struct {
		name       string
		code       model.ErrorCode
		wantStatus int
		wantCode   string
		wantRow    string
	}{
		{"upstream 429", model.CodeRateLimited, http.StatusTooManyRequests, "rate_limited", "failed"},
		{"upstream 500", model.CodeUnavailable, http.StatusBadGateway, "model_unavailable", "failed"},
		{"upstream timeout", model.CodeTimeout, http.StatusGatewayTimeout, "model_timeout", "timeout"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			provider := &scriptedProvider{errs: []error{
				&model.Error{Code: item.code, Message: "scripted upstream failure"},
			}}
			env := newTestEnvWithProvider(t, nil, provider)
			a := env.register(t, "g06upstream"+strings.ReplaceAll(item.name, " ", ""))
			object := a.createObject(t, "collaborative", "G06 upstream", map[string]any{"body": "base"})
			conversation := a.createConversation(t, "G06 upstream", &object.ID)
			status, body := a.sendMessage(t, conversation, "fail")
			requireStatus(t, status, item.wantStatus, body)
			requireCode(t, body, item.wantCode)
			if provider.requestCount() != 1 {
				t.Fatalf("provider requests = %d, want 1 (no automatic retry)", provider.requestCount())
			}
			var rowStatus, errorCode string
			if err := env.store.Pool().QueryRow(t.Context(), `
SELECT status, COALESCE(error_code, '') FROM model_invocations
 WHERE user_id = $1`, a.id).Scan(&rowStatus, &errorCode); err != nil {
				t.Fatalf("read invocation: %v", err)
			}
			if rowStatus != item.wantRow || errorCode != item.wantCode {
				t.Fatalf("invocation status=%s error=%s, want %s/%s",
					rowStatus, errorCode, item.wantRow, item.wantCode)
			}
			if got := env.count(t, `SELECT count(*) FROM ai_messages WHERE conversation_id=$1`,
				mustUUID(t, conversation)); got != 0 {
				t.Fatalf("failed model turn persisted %d messages", got)
			}
			getStatus, getBody, _ := a.call(t, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{})
			requireStatus(t, getStatus, http.StatusOK, getBody)
			if got := decodeObject(t, getBody); got.Version != 1 {
				t.Fatalf("failed model turn changed the object to version %d", got.Version)
			}
		})
	}
}

func TestG06ProviderIntegrityFailuresAreUnknown(t *testing.T) {
	cases := []struct {
		name string
		mode g04ProviderMode
	}{
		{"unsigned response", g04UnsignedResponse},
		{"tampered response", g04TamperedResponse},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			mode := item.mode
			env, _ := newG04Env(t, mode)
			a := env.register(t, "g06integrity")
			object := a.createObject(t, "collaborative", "G06 integrity", map[string]any{"body": "b"})
			status, body, _ := a.invokeCapability(t, "content.structure",
				map[string]any{"object_id": object.ID, "text": "text", "format": "plain_text"},
				newIdempotencyKey())
			requireStatus(t, status, http.StatusBadGateway, body)
			code, invocationID := decodeCapabilityError(t, body)
			if code != "capability_unknown" || invocationID == "" {
				t.Fatalf("code=%s invocation=%s body=%s", code, invocationID, body)
			}
			rowStatus, output, errorCode, _ := env.invocationRow(t, invocationID)
			if rowStatus != "unknown" || len(output) != 0 ||
				errorCode == nil || *errorCode != "capability_unknown" {
				t.Fatalf("row = %s output=%s error=%v", rowStatus, output, errorCode)
			}
			var audited int
			if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM audit_events
 WHERE action = 'capability.unknown' AND reason_code = 'provider_integrity_failed'
   AND resource_id = $1`, mustUUID(t, invocationID)).Scan(&audited); err != nil {
				t.Fatalf("read integrity audit: %v", err)
			}
			if audited == 0 {
				t.Fatal("provider integrity failure was not audited")
			}
		})
	}
}

// g06CancelProvider signals when the call starts and returns only after the
// request context is canceled, modelling a client disconnect or shutdown.
type g06CancelProvider struct {
	called chan struct{}
	done   chan struct{}
	once   sync.Once
}

func newG06CancelProvider() *g06CancelProvider {
	return &g06CancelProvider{called: make(chan struct{}), done: make(chan struct{})}
}

func (p *g06CancelProvider) Mode() string { return "fixture" }

func (p *g06CancelProvider) Complete(ctx context.Context, _ model.Request) (model.Response, error) {
	p.once.Do(func() { close(p.called) })
	<-ctx.Done()
	close(p.done)
	return model.Response{}, &model.Error{
		Code: model.CodeTimeout, Message: "model request was canceled", Err: ctx.Err(),
	}
}

func TestG06CanceledModelCallLeavesNoMessagesOrMutation(t *testing.T) {
	provider := newG06CancelProvider()
	env := newTestEnvWithProvider(t, nil, provider)
	a := env.register(t, "g06cancel")
	object := a.createObject(t, "collaborative", "G06 cancel parent", map[string]any{"body": "base"})
	conversation := a.createConversation(t, "G06 cancel", &object.ID)

	client := &http.Client{Jar: a.client.Jar, Timeout: 10 * time.Second}
	requestCtx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost,
		env.server.URL+"/api/v1/ai/conversations/"+conversation+"/messages",
		strings.NewReader(`{"content":"wait forever"}`))
	if err != nil {
		cancel()
		t.Fatalf("build canceled request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", env.origin)
	request.Header.Set("X-CSRF-Token", a.csrf)
	request.Header.Set("Idempotency-Key", newIdempotencyKey())
	type callOutcome struct {
		status int
		body   []byte
		err    error
	}
	callResult := make(chan callOutcome, 1)
	go func() {
		response, callErr := client.Do(request)
		outcome := callOutcome{err: callErr}
		if response != nil {
			outcome.status = response.StatusCode
			outcome.body, _ = io.ReadAll(response.Body)
			_ = response.Body.Close()
		}
		callResult <- outcome
	}()
	select {
	case <-provider.called:
	case outcome := <-callResult:
		cancel()
		t.Fatalf("request returned before the provider call: status=%d err=%v body=%s",
			outcome.status, outcome.err, outcome.body)
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("the provider was not called")
	}
	cancel()
	select {
	case outcome := <-callResult:
		if outcome.err == nil {
			t.Fatal("the canceled request unexpectedly completed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the canceled client request did not return")
	}
	select {
	case <-provider.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the provider did not observe cancellation")
	}

	// The failure record uses a detached context so it survives the canceled
	// request; no assistant message or object mutation exists.
	deadline := time.Now().Add(5 * time.Second)
	for {
		var status, errorCode string
		err := env.store.Pool().QueryRow(t.Context(), `
SELECT status, COALESCE(error_code, '') FROM model_invocations
 WHERE user_id = $1`, a.id).Scan(&status, &errorCode)
		if err == nil && status == "timeout" && errorCode == "model_timeout" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("canceled invocation not recorded: status=%q error=%q err=%v", status, errorCode, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := env.count(t, `SELECT count(*) FROM ai_messages WHERE conversation_id=$1`,
		mustUUID(t, conversation)); got != 0 {
		t.Fatalf("canceled turn persisted %d messages", got)
	}
	getStatus, getBody, _ := a.call(t, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, getStatus, http.StatusOK, getBody)
	if got := decodeObject(t, getBody); got.Version != 1 {
		t.Fatalf("canceled turn changed the object to version %d", got.Version)
	}
}
