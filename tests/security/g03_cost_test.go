// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/QiyanAI/derive-world/internal/config"
	"github.com/QiyanAI/derive-world/internal/model"
)

type blockingProvider struct {
	entered chan struct{}
	release chan struct{}
}

func (p *blockingProvider) Mode() string { return "fixture" }

func (p *blockingProvider) Complete(context.Context, model.Request) (model.Response, error) {
	p.entered <- struct{}{}
	<-p.release
	return model.Response{
		Text:  `{"reply":"released","proposed_actions":[]}`,
		Model: "blocking-test-double",
		Usage: model.Usage{InputTokens: 1, OutputTokens: 1},
	}, nil
}

func TestG03ModelDailyBudget(t *testing.T) {
	provider := &scriptedProvider{}
	env := newTestEnvWithProvider(t, func(cfg *config.Config) {
		cfg.RateLimitAIPerHour = 1_000_000
		// This test isolates the daily *call* budget. The prompt now always
		// carries the compact world-brief contract, so the default character
		// budget would stop the loop first and test a different guard.
		cfg.Model.DailyInputCharsPerUser = 1_000_000
	}, provider)
	a := env.register(t, "g03budget-a")
	conversation := a.createConversation(t, "Budget chat", nil)
	for index := 0; index < 100; index++ {
		status, body := a.sendMessage(t, conversation, "x")
		if status != http.StatusOK {
			t.Fatalf("message %d status = %d body=%s", index+1, status, body)
		}
	}
	status, body := a.sendMessage(t, conversation, "x")
	requireStatus(t, status, http.StatusTooManyRequests, body)
	requireCode(t, body, "budget_exceeded")
	if got := len(provider.requests); got != 100 {
		t.Fatalf("provider calls = %d, want 100", got)
	}
	var recorded int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM model_invocations WHERE user_id=$1`, a.id).Scan(&recorded); err != nil {
		t.Fatalf("count model invocations: %v", err)
	}
	if recorded != 100 {
		t.Fatalf("model_invocations = %d", recorded)
	}
}

func TestG03ModelConcurrencyLimit(t *testing.T) {
	provider := &blockingProvider{entered: make(chan struct{}, 3), release: make(chan struct{})}
	env := newTestEnvWithProvider(t, func(cfg *config.Config) {
		cfg.RateLimitAIPerHour = 1_000_000
	}, provider)
	a := env.register(t, "g03conc2-a")
	object := a.createObject(t, "collaborative", "Concurrency object", map[string]any{
		"body": "body",
	})
	conversation := a.createConversation(t, "Concurrency chat", &object.ID)

	type outcome struct {
		status int
		body   []byte
	}
	results := make(chan outcome, 3)
	send := func() {
		origin := env.origin
		csrf := a.csrf
		status, body, _ := env.call(t, a.client, http.MethodPost,
			"/api/v1/ai/conversations/"+conversation+"/messages", callOptions{
				Origin: &origin, CSRFToken: &csrf,
				IdempotencyKey: newIdempotencyKey(),
				Body:           map[string]any{"content": "block"},
			})
		results <- outcome{status: status, body: body}
	}
	var wait sync.WaitGroup
	wait.Add(2)
	go func() { defer wait.Done(); send() }()
	go func() { defer wait.Done(); send() }()
	<-provider.entered
	<-provider.entered
	wait.Add(1)
	go func() { defer wait.Done(); send() }()
	third := <-results
	if third.status != http.StatusTooManyRequests {
		t.Fatalf("third concurrent status = %d body=%s", third.status, third.body)
	}
	requireCode(t, third.body, "rate_limited")
	close(provider.release)
	wait.Wait()
	close(results)
	successes := 0
	for result := range results {
		if result.status == http.StatusOK {
			successes++
		}
	}
	if successes != 2 {
		t.Fatalf("successful concurrent calls = %d", successes)
	}
	var invocations int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM model_invocations WHERE user_id=$1`, a.id).Scan(&invocations); err != nil {
		t.Fatalf("count invocations: %v", err)
	}
	if invocations != 2 {
		t.Fatalf("model invocations = %d", invocations)
	}
}
