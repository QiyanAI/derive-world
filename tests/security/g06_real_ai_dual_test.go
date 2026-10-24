// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QiyanAI/derive-world/internal/config"
	"github.com/QiyanAI/derive-world/internal/model"
)

// g06RealCalls is process-wide so repeated runs cannot multiply paid calls.
var g06RealCalls int64

type g06RelayProvider struct {
	inner         model.Provider
	maxCalls      int64
	maxInputChars int

	mu      sync.Mutex
	prompts []string
	ledger  *approvedRunLedger
}

func (p *g06RelayProvider) setLedger(ledger *approvedRunLedger) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ledger = ledger
}

func (p *g06RelayProvider) Mode() string {
	if modeProvider, ok := p.inner.(model.ModeProvider); ok {
		return modeProvider.Mode()
	}
	return "real"
}

func (p *g06RelayProvider) Complete(ctx context.Context, request model.Request) (model.Response, error) {
	inputChars := len([]rune(request.System))
	for _, message := range request.Messages {
		inputChars += len([]rune(message.Content))
	}
	if inputChars > p.maxInputChars {
		return model.Response{}, fmt.Errorf("G06 real AI input budget exceeded: %d > %d",
			inputChars, p.maxInputChars)
	}
	if atomic.AddInt64(&g06RealCalls, 1) > p.maxCalls {
		return model.Response{}, fmt.Errorf("G06 real AI call budget exceeded: %d", p.maxCalls)
	}
	p.mu.Lock()
	ledger := p.ledger
	p.prompts = append(p.prompts, request.System)
	p.mu.Unlock()
	if ledger == nil {
		return model.Response{}, errors.New("paid-run ledger is not attached; refusing a paid call")
	}
	// Reserve the paid slot before the network request. If the process dies
	// after this point, the slot is consumed and a restart cannot repeat it.
	if err := ledger.ReserveCall(); err != nil {
		return model.Response{}, err
	}
	return p.inner.Complete(ctx, request)
}

func (p *g06RelayProvider) promptAt(index int) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index < 0 || index >= len(p.prompts) {
		return ""
	}
	return p.prompts[index]
}

func (p *g06RelayProvider) requestCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.prompts)
}

// TestG06RealAIDualPersonalAI is the owner-authorized G06 acceptance test: two
// independent users, two Personal AI profiles, two real model calls, a
// B-authorized Handoff, human apply and human derive approval, and auditable
// per-user usage. It is skipped unless an authorized operator supplies every
// gate below:
//
//	G06_REAL_AI_DUAL=1
//	G06_REAL_AI_AUTHORIZATION=APPROVED-2026-10-24
//	MODEL_BASE_URL (https root of G06_REAL_AI_ALLOWED_HOST), MODEL_API_KEY, MODEL_NAME
//	G06_REAL_AI_ALLOWED_HOST
//	G06_REAL_AI_LEDGER_DIR (persistent one-time ledger outside the repository)
//	G06_REAL_AI_MAX_CALLS=2
//	G06_REAL_AI_MAX_INPUT_CHARS=8192
//	G06_REAL_AI_MAX_OUTPUT_TOKENS=256
//	MODEL_CHAT_PATH=/chat/completions
//	MODEL_RESPONSE_FORMAT=json_object
//	MODEL_THINKING_MODE=disabled
//
// If MODEL_API_KEY is absent it skips with BLOCKED — API KEY REQUIRED and makes
// no paid call. The strict values above are the approved 2026-10-24 real-AI
// scope; a future authorization must amend the test and the plan together.
func TestG06RealAIDualPersonalAI(t *testing.T) {
	if os.Getenv("G06_REAL_AI_DUAL") != "1" {
		t.Skip("G06 real dual Personal AI is not configured in this job; the approved two-call run is recorded as VERIFIED_REAL")
	}
	if os.Getenv("CI") != "" && os.Getenv("G06_REAL_AI_ALLOW_CI") != "1" {
		t.Skip("G06 real dual Personal AI refused in CI without G06_REAL_AI_ALLOW_CI=1")
	}
	if os.Getenv("G06_REAL_AI_AUTHORIZATION") != "APPROVED-2026-10-24" {
		t.Fatal("G06_REAL_AI_AUTHORIZATION=APPROVED-2026-10-24 is required for a Real AI run")
	}
	baseURL := os.Getenv("MODEL_BASE_URL")
	apiKey := os.Getenv("MODEL_API_KEY")
	modelName := os.Getenv("MODEL_NAME")
	if apiKey == "" {
		t.Skip("BLOCKED — API KEY REQUIRED: MODEL_API_KEY is not injected; no paid call was made")
	}
	if baseURL == "" || modelName == "" {
		t.Fatal("MODEL_BASE_URL and MODEL_NAME are required for the G06 Real AI relay")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		t.Fatal("MODEL_BASE_URL must be an https root URL without credentials, query, fragment, or path")
	}
	allowedHost := strings.TrimSpace(os.Getenv("G06_REAL_AI_ALLOWED_HOST"))
	if allowedHost == "" {
		t.Fatal("G06_REAL_AI_ALLOWED_HOST is required for the approved real-AI run")
	}
	if !strings.EqualFold(parsed.Hostname(), allowedHost) {
		t.Fatalf("MODEL_BASE_URL host %q is not the authorized host %q", parsed.Hostname(), allowedHost)
	}
	if os.Getenv("MODEL_CHAT_PATH") != "/chat/completions" {
		t.Fatal("MODEL_CHAT_PATH must be /chat/completions for this approved real-AI run")
	}
	if os.Getenv("MODEL_RESPONSE_FORMAT") != "json_object" {
		t.Fatal("MODEL_RESPONSE_FORMAT must be json_object for this approved real-AI run")
	}
	if os.Getenv("MODEL_THINKING_MODE") != "disabled" {
		t.Fatal("MODEL_THINKING_MODE must be disabled for this approved real-AI run")
	}
	ledgerDir := os.Getenv("G06_REAL_AI_LEDGER_DIR")
	if ledgerDir == "" {
		t.Fatal("G06_REAL_AI_LEDGER_DIR is required for the one-time approved real-AI run")
	}
	maxCalls, err := strconv.Atoi(os.Getenv("G06_REAL_AI_MAX_CALLS"))
	if err != nil || maxCalls != 2 {
		t.Fatal("G06_REAL_AI_MAX_CALLS must be explicitly set to 2")
	}
	maxInputChars, err := strconv.Atoi(os.Getenv("G06_REAL_AI_MAX_INPUT_CHARS"))
	if err != nil || maxInputChars != 8192 {
		t.Fatal("G06_REAL_AI_MAX_INPUT_CHARS must be exactly 8192 for this approved real-AI run")
	}
	maxOutputTokens, err := strconv.Atoi(os.Getenv("G06_REAL_AI_MAX_OUTPUT_TOKENS"))
	if err != nil || maxOutputTokens != 256 {
		t.Fatal("G06_REAL_AI_MAX_OUTPUT_TOKENS must be exactly 256 for this approved real-AI run")
	}

	realProvider, err := model.NewWithOptions(model.AdapterOptions{
		Mode: "real", BaseURL: baseURL, APIKey: apiKey, ModelName: modelName,
		Timeout: 90 * time.Second, MaxOutputTokens: maxOutputTokens,
		ChatPath:       os.Getenv("MODEL_CHAT_PATH"),
		ResponseFormat: os.Getenv("MODEL_RESPONSE_FORMAT"),
		ThinkingMode:   os.Getenv("MODEL_THINKING_MODE"),
	})
	if err != nil {
		t.Fatalf("create Real provider: %v", err)
	}
	provider := &g06RelayProvider{
		inner: realProvider, maxCalls: int64(maxCalls), maxInputChars: maxInputChars,
	}
	env := newTestEnvWithProvider(t, func(cfg *config.Config) {
		cfg.Model = config.ModelConfig{
			Mode: "real", BaseURL: baseURL, Name: modelName,
			Timeout: 90 * time.Second, MaxOutputTokens: maxOutputTokens,
			ChatPath:               os.Getenv("MODEL_CHAT_PATH"),
			ResponseFormat:         os.Getenv("MODEL_RESPONSE_FORMAT"),
			ThinkingMode:           os.Getenv("MODEL_THINKING_MODE"),
			MaxInputChars:          maxInputChars,
			DailyCallsPerUser:      2,
			DailyInputCharsPerUser: maxInputChars * 3,
			ConcurrencyPerUser:     1,
			GlobalDailyCalls:       maxCalls,
			IncludePrivateContext:  true,
		}
	}, provider)

	a := env.register(t, "g06reala")
	b := env.register(t, "g06realb")
	if a.profileID(t) == b.profileID(t) {
		t.Fatal("two users received the same Personal AI profile")
	}
	a.setPrivateContext(t, "G06-REAL-A-PRIVATE")
	b.setPrivateContext(t, "G06-REAL-B-PRIVATE")

	object := a.createObject(t, "collaborative", "G06 real parent", map[string]any{
		"body":    "G06-REAL-BASE-BODY",
		"summary": "Two independent Real Personal AI turns.",
	})

	// ---- Human A -> Personal AI A -> Human A ----
	aConversation := a.createConversation(t, "G06 A Real Personal AI", &object.ID)
	aPrompt := `Return exactly one JSON object and no prose outside it:
{"reply":"proposal requested","proposed_actions":[
{"capability_id":"object.propose_change","input":{"action":"set_body","input":{"body":"G06-REAL-A-AI-BODY"},"expected_version":1}}
]}`

	// The one-time paid-run guard is acquired only after all non-paid setup, so
	// a setup failure does not consume the authorization. From this point, a
	// failure consumes the run and no retry is permitted.
	ledger, err := openApprovedRunLedger(ledgerDir, modelName, parsed.Hostname())
	if err != nil {
		t.Fatalf("paid-run ledger: %v", err)
	}
	provider.setLedger(ledger)
	completed := false
	defer func() {
		if !completed {
			ledger.MarkFailed("test did not complete")
		}
	}()

	status, body := a.sendMessage(t, aConversation, aPrompt)
	requireStatus(t, status, http.StatusOK, body)
	var aTurn g06TurnResponse
	if err := json.Unmarshal(body, &aTurn); err != nil {
		t.Fatalf("decode A Real turn: %v", err)
	}
	if aTurn.ModelMode != "real" || aTurn.ParseStatus != "structured" ||
		aTurn.AssistantMessage.ModelMode != "real" ||
		aTurn.AssistantMessage.ParseStatus != "structured" {
		t.Fatalf("A Real turn mode=%q parse_status=%q body=%s",
			aTurn.ModelMode, aTurn.ParseStatus, body)
	}
	if aTurn.Usage.InputTokens <= 0 || aTurn.Usage.OutputTokens <= 0 {
		t.Fatalf("A Real turn did not report provider token usage: %+v", aTurn.Usage)
	}
	if len(aTurn.ProposedActions) != 1 || aTurn.ProposedActions[0].Kind != "proposal" ||
		aTurn.ProposedActions[0].ProposalID == "" {
		t.Fatalf("A Real proposed actions = %+v: %s", aTurn.ProposedActions, body)
	}
	proposalID := aTurn.ProposedActions[0].ProposalID

	preStatus, preBody, _ := a.call(t, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, preStatus, http.StatusOK, preBody)
	if before := decodeObject(t, preBody); before.Version != 1 {
		t.Fatalf("AI turn mutated the object before human review: version %d", before.Version)
	}

	applyStatus, applyBody := applyProposal(t, a, object.ID, proposalID, 1)
	requireStatus(t, applyStatus, http.StatusOK, applyBody)
	var applied struct {
		Object objectResponse `json:"object"`
	}
	if err := json.Unmarshal(applyBody, &applied); err != nil {
		t.Fatalf("decode A Real apply: %v", err)
	}
	if applied.Object.Version != 2 || applied.Object.State["body"] != "G06-REAL-A-AI-BODY" {
		t.Fatalf("A Real applied object = %+v", applied.Object)
	}

	// A authorizes B through a one-time invitation.
	invitation := a.createInvitation(t, object.ID,
		[]string{"read", "contribute", "derive"}, 1)
	joinStatus, joinBody := b.acceptInvitation(t, *invitation.Token)
	requireStatus(t, joinStatus, http.StatusOK, joinBody)

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
	if handoff.ObjectVersion != 2 ||
		strings.Join(handoff.PermittedActions, ",") != "read,contribute,derive" {
		t.Fatalf("Handoff version=%d actions=%v", handoff.ObjectVersion, handoff.PermittedActions)
	}
	if strings.Contains(string(handoffBody), *invitation.Token) ||
		strings.Contains(string(handoffBody), "G06-REAL-A-PRIVATE") {
		t.Fatal("Handoff leaked the invitation token or A's private context")
	}

	// ---- Human B -> Personal AI B -> Human B -> derive ----
	bConversation := b.createConversation(t, "G06 B Real Personal AI", &object.ID)
	bPrompt := `Return exactly one JSON object and no prose outside it:
{"reply":"derive requested","proposed_actions":[
{"capability_id":"object.derive","input":{"title":"G06-REAL-B-AI-CHILD","source_version":2,"inheritance_fields":["body"]}}
]}`
	messageKey := newIdempotencyKey()
	origin := env.origin
	csrf := b.csrf
	messageStatus, messageBody, _ := env.call(t, b.client, http.MethodPost,
		"/api/v1/ai/conversations/"+bConversation+"/messages", callOptions{
			Origin: &origin, CSRFToken: &csrf, IdempotencyKey: messageKey,
			Body: map[string]any{"content": bPrompt},
		})
	requireStatus(t, messageStatus, http.StatusOK, messageBody)
	var bTurn g06TurnResponse
	if err := json.Unmarshal(messageBody, &bTurn); err != nil {
		t.Fatalf("decode B Real turn: %v", err)
	}
	if bTurn.ModelMode != "real" || bTurn.ParseStatus != "structured" ||
		bTurn.ContextStale {
		t.Fatalf("B Real turn = %+v: %s", bTurn, messageBody)
	}
	if bTurn.Usage.InputTokens <= 0 || bTurn.Usage.OutputTokens <= 0 {
		t.Fatalf("B Real turn did not report provider token usage: %+v", bTurn.Usage)
	}
	if len(bTurn.ProposedActions) != 1 || bTurn.ProposedActions[0].Kind != "confirmation" ||
		bTurn.ProposedActions[0].ConfirmationID == "" {
		t.Fatalf("B Real proposed actions = %+v: %s", bTurn.ProposedActions, messageBody)
	}
	confirmationID := bTurn.ProposedActions[0].ConfirmationID

	// Each prompt contains only that user's private context and the shared,
	// permission-clipped object view.
	if aPromptText := provider.promptAt(0); aPromptText == "" ||
		!strings.Contains(aPromptText, "G06-REAL-BASE-BODY") ||
		!strings.Contains(aPromptText, "G06-REAL-A-PRIVATE") ||
		strings.Contains(aPromptText, "G06-REAL-B-PRIVATE") {
		t.Fatal("A's Real prompt did not contain exactly A's authorized context")
	}
	if bPromptText := provider.promptAt(1); bPromptText == "" ||
		!strings.Contains(bPromptText, "G06-REAL-A-AI-BODY") ||
		!strings.Contains(bPromptText, "G06-REAL-B-PRIVATE") ||
		strings.Contains(bPromptText, "G06-REAL-A-PRIVATE") {
		t.Fatal("B's Real prompt did not contain exactly B's authorized context")
	}

	// No mutation happened before Human B approves.
	preApproveStatus, preApproveBody, _ := b.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, preApproveStatus, http.StatusOK, preApproveBody)
	if preApprove := decodeObject(t, preApproveBody); preApprove.Version != 2 {
		t.Fatalf("B's AI turn mutated the parent: version %d", preApprove.Version)
	}

	approveStatus, approveBody, _ := b.call(t, http.MethodPost,
		"/api/v1/confirmations/"+confirmationID+"/approve",
		callOptions{IdempotencyKey: newIdempotencyKey()})
	requireStatus(t, approveStatus, http.StatusOK, approveBody)
	var child objectResponse
	if err := json.Unmarshal(approveBody, &child); err != nil {
		t.Fatalf("decode B Real child: %v", err)
	}
	if child.OwnerID != b.id.String() || child.Version != 1 ||
		child.Lineage == nil || child.Lineage.Parent.ObjectID != object.ID ||
		child.Lineage.Parent.Version != 2 {
		t.Fatalf("B Real child = %+v", child)
	}

	// A cannot read B's private derived object.
	aChildStatus, aChildBody, _ := a.call(t, http.MethodGet, "/api/v1/objects/"+child.ID, callOptions{})
	requireStatus(t, aChildStatus, http.StatusNotFound, aChildBody)

	// Exactly two paid calls, exactly one successful invocation per user.
	var aInvocations, bInvocations int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM model_invocations
 WHERE user_id = $1 AND provider = 'real' AND status = 'succeeded'
   AND model <> '' AND input_chars > 0`,
		a.id).Scan(&aInvocations); err != nil {
		t.Fatalf("count A Real invocations: %v", err)
	}
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM model_invocations
 WHERE user_id = $1 AND provider = 'real' AND status = 'succeeded'
   AND model <> '' AND input_chars > 0`,
		b.id).Scan(&bInvocations); err != nil {
		t.Fatalf("count B Real invocations: %v", err)
	}
	if aInvocations != 1 || bInvocations != 1 {
		t.Fatalf("Real invocations A=%d B=%d, want one each", aInvocations, bInvocations)
	}
	if provider.requestCount() != 2 || atomic.LoadInt64(&g06RealCalls) != 2 {
		t.Fatalf("provider requests = %d, budget = %d",
			provider.requestCount(), maxCalls)
	}
	var storedModels string
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT COALESCE(string_agg(DISTINCT model, ',' ORDER BY model), '')
  FROM model_invocations
 WHERE user_id IN ($1,$2) AND provider = 'real' AND status = 'succeeded'`,
		a.id, b.id).Scan(&storedModels); err != nil {
		t.Fatalf("read stored model identity: %v", err)
	}
	if storedModels == "" {
		t.Fatal("no stored model identity was recorded")
	}
	t.Logf("G06 Real AI: 2 calls, requested model=%s, stored model(s)=%s", modelName, storedModels)
	t.Logf("G06 Real AI token usage: A input=%d output=%d; B input=%d output=%d",
		aTurn.Usage.InputTokens, aTurn.Usage.OutputTokens,
		bTurn.Usage.InputTokens, bTurn.Usage.OutputTokens)

	// Revocation still wins after the Real relay; replay makes no paid call.
	removeStatus, removeBody, _ := a.call(t, http.MethodDelete,
		"/api/v1/objects/"+object.ID+"/members/"+b.id.String(), callOptions{})
	requireStatus(t, removeStatus, http.StatusNoContent, removeBody)
	replayStatus, replayBody, _ := env.call(t, b.client, http.MethodPost,
		"/api/v1/ai/conversations/"+bConversation+"/messages", callOptions{
			Origin: &origin, CSRFToken: &csrf, IdempotencyKey: messageKey,
			Body: map[string]any{"content": bPrompt},
		})
	requireStatus(t, replayStatus, http.StatusNotFound, replayBody)
	if provider.requestCount() != 2 {
		t.Fatalf("replay made an additional paid call: %d", provider.requestCount())
	}
	if err := ledger.MarkCompleted(); err != nil {
		t.Fatalf("mark paid-run ledger completed: %v", err)
	}
	completed = true
}

// approvedRunLedgerState is the durable one-time record for the approved paid run.
// It contains no key, prompt, or private content.
type approvedRunLedgerState struct {
	Authorization string `json:"authorization"`
	Model         string `json:"model"`
	Host          string `json:"host"`
	CallsStarted  int    `json:"calls_started"`
	Status        string `json:"status"`
	Reason        string `json:"reason,omitempty"`
	UpdatedAt     string `json:"updated_at"`
}

type approvedRunLedger struct {
	mu        sync.Mutex
	statePath string
	model     string
	host      string
	calls     int
}

// openApprovedRunLedger acquires the one-time lock and creates the durable state
// file. The lock is intentionally never removed by the test; a rerun after a
// crash or failure is refused and requires an owner decision.
func openApprovedRunLedger(dir, model, host string) (*approvedRunLedger, error) {
	if dir == "" {
		return nil, errors.New("G06_REAL_AI_LEDGER_DIR is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// The historical one-time run used different ledger file names. Refuse a
	// new paid run if either legacy artifact is present, so sanitizing the
	// public label cannot bypass the consumed paid slot.
	for _, legacy := range []string{"auth09.lock", "auth09-state.json"} {
		path := filepath.Join(dir, legacy)
		if _, err := os.Stat(path); err == nil {
			return nil, fmt.Errorf("a consumed one-time paid-run ledger exists (%s); a new run requires a new owner authorization", legacy)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	lockPath := filepath.Join(dir, "approved-run.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("paid run already consumed or in use (lock exists): %w", err)
	}
	_, _ = fmt.Fprintf(lock, "pid=%d\ncreated_at=%s\n", os.Getpid(),
		time.Now().UTC().Format(time.RFC3339))
	_ = lock.Close()
	statePath := filepath.Join(dir, "approved-run-state.json")
	stateFile, err := os.OpenFile(statePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("paid-run state already exists: %w", err)
	}
	_ = stateFile.Close()
	ledger := &approvedRunLedger{statePath: statePath, model: model, host: host}
	if err := ledger.write("reserved", 0, ""); err != nil {
		return nil, err
	}
	return ledger, nil
}

func (l *approvedRunLedger) write(status string, calls int, reason string) error {
	state := approvedRunLedgerState{
		Authorization: "APPROVED-2026-10-24",
		Model:         l.model,
		Host:          l.host,
		CallsStarted:  calls,
		Status:        status,
		Reason:        reason,
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(l.statePath, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// ReserveCall durably consumes one paid slot before the network request.
func (l *approvedRunLedger) ReserveCall() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.calls >= 2 {
		return errors.New("paid-run call budget exhausted")
	}
	next := l.calls + 1
	if err := l.write("in_progress", next, ""); err != nil {
		return err
	}
	l.calls = next
	return nil
}

func (l *approvedRunLedger) MarkCompleted() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.write("completed", l.calls, "")
}

func (l *approvedRunLedger) MarkFailed(reason string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.write("failed", l.calls, reason)
}

// TestApprovedRunLedgerSingleUse proves the one-time guard without any paid
// call: two slots, a third refusal, and a restart refusal from the lock file.
func TestApprovedRunLedgerSingleUse(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ledger")
	ledger, err := openApprovedRunLedger(dir, "test-model", "example.test")
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	if err := ledger.ReserveCall(); err != nil {
		t.Fatalf("reserve call 1: %v", err)
	}
	if err := ledger.ReserveCall(); err != nil {
		t.Fatalf("reserve call 2: %v", err)
	}
	if err := ledger.ReserveCall(); err == nil {
		t.Fatal("third paid slot was reserved")
	}
	if _, err := openApprovedRunLedger(dir, "test-model", "example.test"); err == nil {
		t.Fatal("a second process acquired the same paid-run ledger")
	}
	if err := ledger.MarkCompleted(); err != nil {
		t.Fatalf("mark completed: %v", err)
	}
	encoded, err := os.ReadFile(filepath.Join(dir, "approved-run-state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var state approvedRunLedgerState
	if err := json.Unmarshal(encoded, &state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if state.Authorization != "APPROVED-2026-10-24" || state.CallsStarted != 2 || state.Status != "completed" {
		t.Fatalf("state = %+v", state)
	}
	if strings.Contains(string(encoded), "sk-") {
		t.Fatal("ledger contains a key-like value")
	}
}

func TestApprovedRunLedgerRefusesLegacyConsumedLock(t *testing.T) {
	for _, legacy := range []string{"auth09.lock", "auth09-state.json"} {
		t.Run(legacy, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, legacy), []byte("consumed"), 0o600); err != nil {
				t.Fatalf("write legacy ledger: %v", err)
			}
			if _, err := openApprovedRunLedger(dir, "test-model", "example.test"); err == nil {
				t.Fatalf("openApprovedRunLedger accepted a legacy consumed ledger %s", legacy)
			}
		})
	}
}

// TestApprovedRunLedgerRefusesRecordedConsumedLedger checks the actual
// historical ledger when G06_REAL_AI_LEDGER_DIR points at the external G06
// evidence directory. It is skipped in CI, where the evidence is not mounted.
func TestApprovedRunLedgerRefusesRecordedConsumedLedger(t *testing.T) {
	dir := os.Getenv("G06_REAL_AI_LEDGER_DIR")
	if dir == "" {
		t.Skip("G06_REAL_AI_LEDGER_DIR is not set; the recorded ledger lives outside this repository")
	}
	if _, err := openApprovedRunLedger(dir, "test-model", "example.test"); err == nil {
		t.Fatal("openApprovedRunLedger accepted the recorded consumed ledger")
	}
}
