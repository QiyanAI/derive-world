// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QiyanAI/derive-world/internal/config"
	"github.com/QiyanAI/derive-world/internal/model"
)

// realRelayCalls is process-wide so `go test -count=N` cannot multiply the
// number of paid provider calls even if the test is invoked repeatedly.
var realRelayCalls int64

// relayProvider forwards to a real provider but reserves the paid call before
// delegating and captures the request for authorization assertions. It never
// prints the API key or provider headers.
type relayProvider struct {
	inner         model.Provider
	maxCalls      int64
	maxInputChars int

	mu       sync.Mutex
	requests []model.Request
}

func (p *relayProvider) Mode() string {
	if modeProvider, ok := p.inner.(model.ModeProvider); ok {
		return modeProvider.Mode()
	}
	return "real"
}

func (p *relayProvider) Complete(ctx context.Context, request model.Request) (model.Response, error) {
	inputChars := len(request.System)
	for _, message := range request.Messages {
		inputChars += len(message.Content)
	}
	if inputChars > p.maxInputChars {
		return model.Response{}, fmt.Errorf("real AI test input budget exceeded: %d > %d",
			inputChars, p.maxInputChars)
	}
	if atomic.AddInt64(&realRelayCalls, 1) > p.maxCalls {
		return model.Response{}, fmt.Errorf("real AI test call budget exceeded: %d", p.maxCalls)
	}
	p.mu.Lock()
	p.requests = append(p.requests, request)
	p.mu.Unlock()
	return p.inner.Complete(ctx, request)
}

func (p *relayProvider) promptAt(index int) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index < 0 || index >= len(p.requests) {
		return ""
	}
	return renderPrompt(p.requests[index])
}

func (p *relayProvider) requestCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

// TestG03RealAITwoUserRelay is the full Real AI acceptance test: two
// independent users, two Personal AI profiles, a B-authorized Handoff, one real
// model turn returning a structured proposal and derive confirmation, human
// approve/apply, and final member removal. It is separate from
// TestG03RealAIConnectivity, which proves one single-user model call only.
//
// It is skipped unless all of the following are set by an authorized operator:
//
//	G03_REAL_AI_RELAY=1
//	G03_REAL_AI_AUTHORIZATION=APPROVED-2026-10-24
//	MODEL_BASE_URL, MODEL_API_KEY, MODEL_NAME
//	G03_REAL_AI_MAX_CALLS (1 or 2)
//	G03_REAL_AI_MAX_INPUT_CHARS (1024..65536)
//
// It makes exactly one provider call and never runs in normal CI. Until such a
// run is recorded, this G03 relay test remains unconfigured by default; the
// approved G06 two-call run is recorded separately.
func TestG03RealAITwoUserRelay(t *testing.T) {
	if os.Getenv("G03_REAL_AI_RELAY") != "1" {
		t.Skip("Real AI two-user relay is not configured in this job; the approved two-call run is recorded as VERIFIED_REAL")
	}
	if os.Getenv("CI") != "" && os.Getenv("G03_REAL_AI_ALLOW_CI") != "1" {
		t.Skip("Real AI relay refused in CI without G03_REAL_AI_ALLOW_CI=1; this job does not run a paid call")
	}
	if os.Getenv("G03_REAL_AI_AUTHORIZATION") != "APPROVED-2026-10-24" {
		t.Fatal("G03_REAL_AI_AUTHORIZATION=APPROVED-2026-10-24 is required for a Real AI run")
	}
	baseURL := os.Getenv("MODEL_BASE_URL")
	apiKey := os.Getenv("MODEL_API_KEY")
	modelName := os.Getenv("MODEL_NAME")
	if baseURL == "" || apiKey == "" || modelName == "" {
		t.Fatal("MODEL_BASE_URL, MODEL_API_KEY, and MODEL_NAME are required for the Real AI relay")
	}
	maxCalls, err := strconv.Atoi(os.Getenv("G03_REAL_AI_MAX_CALLS"))
	if err != nil || maxCalls < 1 || maxCalls > 2 {
		t.Fatal("G03_REAL_AI_MAX_CALLS must be explicitly set to 1 or 2")
	}
	maxInputChars, err := strconv.Atoi(os.Getenv("G03_REAL_AI_MAX_INPUT_CHARS"))
	if err != nil || maxInputChars < 1024 || maxInputChars > 64*1024 {
		t.Fatal("G03_REAL_AI_MAX_INPUT_CHARS must be explicitly set between 1024 and 65536")
	}

	realProvider, err := model.New("real", baseURL, apiKey, modelName, 90*time.Second, 512, "")
	if err != nil {
		t.Fatalf("create Real provider: %v", err)
	}
	provider := &relayProvider{
		inner: realProvider, maxCalls: int64(maxCalls), maxInputChars: maxInputChars,
	}
	// The provider is injected already constructed, so the key is not copied
	// into the config struct a second time.
	env := newTestEnvWithProvider(t, func(cfg *config.Config) {
		cfg.Model = config.ModelConfig{
			Mode: "real", BaseURL: baseURL, Name: modelName,
			Timeout: 90 * time.Second, MaxOutputTokens: 512,
			IncludePrivateContext: true,
		}
	}, provider)

	a := env.register(t, "g03realrelay-a")
	b := env.register(t, "g03realrelay-b")
	if a.profileID(t) == b.profileID(t) {
		t.Fatal("two users received the same Personal AI profile")
	}
	a.setPrivateContext(t, "REAL-RELAY-PRIVATE-A")
	b.setPrivateContext(t, "REAL-RELAY-PRIVATE-B")

	object := a.createObject(t, "collaborative", "Real relay object", map[string]any{
		"body":    "REAL-RELAY-OBJECT-BODY",
		"summary": "B is authorized through a one-time Derive Link.",
	})
	invitation := a.createInvitation(t, object.ID,
		[]string{"read", "contribute", "derive"}, 1)
	status, body := b.acceptInvitation(t, *invitation.Token)
	requireStatus(t, status, http.StatusOK, body)

	// B receives only the B-authorized Handoff before any model call.
	handoffStatus, handoffBody, _ := b.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID+"/handoff", callOptions{})
	requireStatus(t, handoffStatus, http.StatusOK, handoffBody)
	var handoff struct {
		ObjectVersion    int      `json:"object_version"`
		PermittedActions []string `json:"permitted_actions"`
	}
	if err := json.Unmarshal(handoffBody, &handoff); err != nil {
		t.Fatalf("decode Handoff: %v", err)
	}
	if handoff.ObjectVersion != 1 ||
		strings.Join(handoff.PermittedActions, ",") != "read,contribute,derive" {
		t.Fatalf("Handoff version=%d actions=%v", handoff.ObjectVersion, handoff.PermittedActions)
	}
	if strings.Contains(string(handoffBody), *invitation.Token) ||
		strings.Contains(string(handoffBody), "REAL-RELAY-PRIVATE-A") {
		t.Fatal("Handoff leaked the invitation token or A's private context")
	}

	conversationID := b.createConversation(t, "Real relay chat", &object.ID)
	messageKey := newIdempotencyKey()
	prompt := `Return exactly one JSON object and no prose outside it:
{"reply":"proposal and derive requested","proposed_actions":[
{"capability_id":"object.propose_change","input":{"action":"set_body","input":{"body":"REAL-AI-RELAY-BODY"},"expected_version":1}},
{"capability_id":"object.derive","input":{"title":"REAL-AI-RELAY-CHILD","inheritance_fields":["body"]}}
]}`
	origin := env.origin
	csrf := b.csrf
	messageStatus, messageBody, _ := env.call(t, b.client, http.MethodPost,
		"/api/v1/ai/conversations/"+conversationID+"/messages", callOptions{
			Origin: &origin, CSRFToken: &csrf, IdempotencyKey: messageKey,
			Body: map[string]any{"content": prompt},
		})
	requireStatus(t, messageStatus, http.StatusOK, messageBody)
	var turn struct {
		ModelMode        string `json:"model_mode"`
		ParseStatus      string `json:"parse_status"`
		ContextStale     bool   `json:"context_stale"`
		AssistantMessage struct {
			ModelMode   string `json:"model_mode"`
			ParseStatus string `json:"parse_status"`
		} `json:"assistant_message"`
		ProposedActions []struct {
			Kind           string `json:"kind"`
			ProposalID     string `json:"proposal_id"`
			ConfirmationID string `json:"confirmation_id"`
		} `json:"proposed_actions"`
	}
	if err := json.Unmarshal(messageBody, &turn); err != nil {
		t.Fatalf("decode Real AI turn: %v", err)
	}
	if turn.ModelMode != "real" || turn.ParseStatus != "structured" ||
		turn.AssistantMessage.ModelMode != "real" ||
		turn.AssistantMessage.ParseStatus != "structured" {
		t.Fatalf("Real AI turn mode=%q parse_status=%q assistant=%+v body=%s",
			turn.ModelMode, turn.ParseStatus, turn.AssistantMessage, messageBody)
	}
	if turn.ContextStale {
		t.Fatal("Real AI turn reported context_stale on a fresh object")
	}
	if len(turn.ProposedActions) != 2 {
		t.Fatalf("Real model accepted actions = %d, want 2: %s",
			len(turn.ProposedActions), messageBody)
	}
	var proposalID, confirmationID string
	for _, action := range turn.ProposedActions {
		switch action.Kind {
		case "proposal":
			proposalID = action.ProposalID
		case "confirmation":
			confirmationID = action.ConfirmationID
		}
	}
	if proposalID == "" || confirmationID == "" {
		t.Fatalf("Real model did not produce both structured actions: %s", messageBody)
	}

	// The Real model saw only the B-authorized projection plus B's own private
	// context; A's private marker must never reach it.
	promptText := provider.promptAt(0)
	if promptText == "" || !strings.Contains(promptText, "REAL-RELAY-OBJECT-BODY") {
		t.Fatal("Real model prompt did not include the authorized Handoff")
	}
	if !strings.Contains(promptText, "REAL-RELAY-PRIVATE-B") {
		t.Fatal("Real model prompt did not include B's own private context")
	}
	if strings.Contains(promptText, "REAL-RELAY-PRIVATE-A") {
		t.Fatal("Real model prompt leaked A's private context")
	}

	// No mutation occurs before the human actions.
	preStatus, preBody, _ := b.call(t, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, preStatus, http.StatusOK, preBody)
	before := decodeObject(t, preBody)
	if before.Version != 1 || before.State["body"] != "REAL-RELAY-OBJECT-BODY" {
		t.Fatalf("AI turn mutated the object before human approval: %+v", before)
	}

	var (
		proposalStatus, proposalActor, proposalProfile, proposalDelegation string
		proposalBody                                                       string
	)
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT p.status, p.actor_user_id::text, p.ai_profile_id::text, p.delegation_id::text,
       p.input->'input'->>'body'
  FROM object_proposals p WHERE p.id = $1`, mustUUID(t, proposalID)).Scan(
		&proposalStatus, &proposalActor, &proposalProfile, &proposalDelegation,
		&proposalBody); err != nil {
		t.Fatalf("read Real AI proposal: %v", err)
	}
	if proposalStatus != "pending" || proposalActor != b.id.String() ||
		proposalProfile != b.profileID(t) || proposalDelegation == "" {
		t.Fatalf("proposal status=%s actor=%s profile=%s delegation=%s",
			proposalStatus, proposalActor, proposalProfile, proposalDelegation)
	}
	var confirmationStatus, confirmationUser, confirmationAction string
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT status, user_id::text, action FROM confirmations WHERE id = $1`,
		mustUUID(t, confirmationID)).Scan(
		&confirmationStatus, &confirmationUser, &confirmationAction); err != nil {
		t.Fatalf("read Real AI confirmation: %v", err)
	}
	if confirmationStatus != "pending" || confirmationUser != b.id.String() ||
		confirmationAction != "derive.create" {
		t.Fatalf("confirmation status=%s user=%s action=%s",
			confirmationStatus, confirmationUser, confirmationAction)
	}

	// Human executes the AI-proposed paths: approve the derive while the parent
	// is still version 1, then apply the proposal.
	approveStatus, approveBody, _ := b.call(t, http.MethodPost,
		"/api/v1/confirmations/"+confirmationID+"/approve", callOptions{
			IdempotencyKey: newIdempotencyKey(),
		})
	requireStatus(t, approveStatus, http.StatusOK, approveBody)
	var child objectResponse
	if err := json.Unmarshal(approveBody, &child); err != nil {
		t.Fatalf("decode Real AI child: %v", err)
	}
	if child.OwnerID != b.id.String() || child.Version != 1 ||
		child.Lineage == nil || child.Lineage.Parent.ObjectID != object.ID ||
		child.Lineage.Parent.Version != 1 {
		t.Fatalf("Real AI child = %+v", child)
	}
	var deriveActor, deriveDelegation *string
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT actor_type, delegation_id::text FROM object_events
 WHERE object_id = $1 AND event_type = 'object.derived'`,
		mustUUID(t, child.ID)).Scan(&deriveActor, &deriveDelegation); err != nil {
		t.Fatalf("read derive event: %v", err)
	}
	if deriveActor == nil || *deriveActor != "personal_ai" ||
		deriveDelegation == nil || *deriveDelegation == "" {
		t.Fatalf("derive actor=%v delegation=%v", deriveActor, deriveDelegation)
	}

	applyStatus, applyBody := applyProposal(t, b, object.ID, proposalID, 1)
	requireStatus(t, applyStatus, http.StatusOK, applyBody)
	var applied struct {
		Object objectResponse `json:"object"`
	}
	if err := json.Unmarshal(applyBody, &applied); err != nil {
		t.Fatalf("decode Real AI apply: %v", err)
	}
	if applied.Object.Version != 2 || applied.Object.State["body"] != proposalBody {
		t.Fatalf("Real AI applied object = %+v, proposed body = %q",
			applied.Object, proposalBody)
	}
	duplicateStatus, duplicateBody := applyProposal(t, b, object.ID, proposalID, 1)
	requireStatus(t, duplicateStatus, http.StatusOK, duplicateBody)
	var duplicated struct {
		Object objectResponse `json:"object"`
	}
	if err := json.Unmarshal(duplicateBody, &duplicated); err != nil {
		t.Fatalf("decode duplicate apply: %v", err)
	}
	if duplicated.Object.Version != 2 {
		t.Fatalf("duplicate apply changed version to %d", duplicated.Object.Version)
	}

	// A cannot see B's proposal or B's private child.
	aProposalsStatus, aProposalsBody, _ := a.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID+"/proposals", callOptions{})
	requireStatus(t, aProposalsStatus, http.StatusOK, aProposalsBody)
	if strings.Contains(string(aProposalsBody), proposalID) {
		t.Fatal("A saw B's private AI proposal")
	}
	aChildStatus, aChildBody, _ := a.call(t, http.MethodGet,
		"/api/v1/objects/"+child.ID, callOptions{})
	requireStatus(t, aChildStatus, http.StatusNotFound, aChildBody)

	// The full relay used one provider call, inside the authorized budget.
	var invocations int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM model_invocations
 WHERE user_id = $1 AND provider = 'real' AND status = 'succeeded'
   AND model = $2 AND input_chars > 0`,
		b.id, modelName).Scan(&invocations); err != nil {
		t.Fatalf("count Real AI invocations: %v", err)
	}
	if invocations != 1 || provider.requestCount() != 1 {
		t.Fatalf("Real AI invocations = %d, requests = %d, budget = %d",
			invocations, provider.requestCount(), maxCalls)
	}
	if got := env.count(t, `SELECT count(*) FROM model_invocations WHERE user_id = $1`,
		a.id); got != 0 {
		t.Fatalf("A made %d model invocations", got)
	}

	// Revocation still wins after the Real relay; no further provider call is
	// made by replay attempts.
	removeStatus, removeBody, _ := a.call(t, http.MethodDelete,
		"/api/v1/objects/"+object.ID+"/members/"+b.id.String(), callOptions{})
	requireStatus(t, removeStatus, http.StatusNoContent, removeBody)
	handoffStatus, handoffBody, _ = b.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID+"/handoff", callOptions{})
	requireStatus(t, handoffStatus, http.StatusNotFound, handoffBody)
	replayStatus, replayBody, _ := env.call(t, b.client, http.MethodPost,
		"/api/v1/ai/conversations/"+conversationID+"/messages", callOptions{
			Origin: &origin, CSRFToken: &csrf, IdempotencyKey: messageKey,
			Body: map[string]any{"content": prompt},
		})
	requireStatus(t, replayStatus, http.StatusNotFound, replayBody)
	replayApplyStatus, replayApplyBody := applyProposal(t, b, object.ID, proposalID, 1)
	requireStatus(t, replayApplyStatus, http.StatusNotFound, replayApplyBody)
	replayApproveStatus, replayApproveBody, _ := b.call(t, http.MethodPost,
		"/api/v1/confirmations/"+confirmationID+"/approve", callOptions{
			IdempotencyKey: newIdempotencyKey(),
		})
	requireStatus(t, replayApproveStatus, http.StatusNotFound, replayApproveBody)
	if provider.requestCount() != 1 {
		t.Fatalf("replay attempts made additional provider calls: %d", provider.requestCount())
	}
}
