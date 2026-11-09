// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ProviderFile is the model-neutral provider registry. A provider is a wire
// protocol plus an endpoint, model name and environment variable that holds
// the credential. No model vendor is hard-coded anywhere in the engine.
type ProviderFile struct {
	SchemaVersion int        `json:"schema_version"`
	Providers     []Provider `json:"providers"`
}

type Provider struct {
	ID                    string  `json:"id"`
	Protocol              string  `json:"protocol"` // openai | anthropic
	BaseURL               string  `json:"base_url"`
	Model                 string  `json:"model"`
	APIKeyEnv             string  `json:"api_key_env"`
	MaxOutputTokens       int     `json:"max_output_tokens"`
	TimeoutSeconds        int     `json:"timeout_seconds"`
	JSONMode              bool    `json:"json_mode"`
	ThinkingMode          string  `json:"thinking_mode,omitempty"`
	InputPricePerMillion  float64 `json:"input_price_per_million_usd"`
	OutputPricePerMillion float64 `json:"output_price_per_million_usd"`
}

// Budget is an owner-authored spend authorization. It is a hard pre-call gate,
// not a security boundary: the owner controls the file and the credential.
type Budget struct {
	SchemaVersion       int      `json:"schema_version"`
	AuthorizationID     string   `json:"authorization_id"`
	AuthorizedBy        string   `json:"authorized_by"`
	AuthorizedAt        string   `json:"authorized_at"`
	ExpiresAt           string   `json:"expires_at"`
	MaxCalls            int      `json:"max_calls"`
	MaxInputChars       int      `json:"max_input_chars"`
	MaxOutputTokens     int      `json:"max_output_tokens"`
	MaxCostUSD          float64  `json:"max_cost_usd"`
	AllowWithoutCostCap bool     `json:"allow_without_cost_cap"`
	Purposes            []string `json:"purposes"`
}

// PricingKnown reports whether a hard cost cap can be truthfully enforced.
// A zero or missing price means the cost is unknown, not free.
func (provider Provider) PricingKnown() bool {
	return provider.InputPricePerMillion > 0 && provider.OutputPricePerMillion > 0
}

// Usage is the durable ledger. A reservation is written and pushed before the
// HTTP request, so a crash or a fresh runner cannot hide a billable call.
type Usage struct {
	SchemaVersion   int     `json:"schema_version"`
	AuthorizationID string  `json:"authorization_id"`
	Calls           int     `json:"calls"`
	InputTokens     int     `json:"input_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	CostUSD         float64 `json:"cost_usd"`
	CostCapEnforced bool    `json:"cost_cap_enforced"`
	UpdatedAt       string  `json:"updated_at"`
}

type ModelRequest struct {
	Purpose         string
	System          string
	User            string
	MaxOutputTokens int
	JSON            bool
}

type ModelResponse struct {
	Text            string
	Provider        string
	Model           string
	InputTokens     int
	OutputTokens    int
	CostUSD         float64
	CostCapEnforced bool
	PricingKnown    bool
}

type ModelClient struct {
	provider Provider
	budget   *Budget
	store    UsageStore
	apiKey   string
	now      func() time.Time
	http     *http.Client
}

func loadProviders(path string) (*ProviderFile, error) {
	var file ProviderFile
	if err := readJSON(path, &file); err != nil {
		return nil, err
	}
	if file.SchemaVersion != 1 {
		return nil, fmt.Errorf("%s: unsupported provider schema_version %d", path, file.SchemaVersion)
	}
	seen := map[string]bool{}
	for _, provider := range file.Providers {
		if err := validateProvider(provider); err != nil {
			return nil, fmt.Errorf("%s: provider %q: %w", path, provider.ID, err)
		}
		if seen[provider.ID] {
			return nil, fmt.Errorf("%s: duplicate provider %q", path, provider.ID)
		}
		seen[provider.ID] = true
	}
	return &file, nil
}

func validateProvider(provider Provider) error {
	if provider.ID == "" {
		return errors.New("missing id")
	}
	switch provider.Protocol {
	case "openai", "anthropic":
	default:
		return fmt.Errorf("unsupported protocol %q (use openai or anthropic)", provider.Protocol)
	}
	parsed, err := url.Parse(provider.BaseURL)
	if err != nil || parsed.Host == "" {
		return errors.New("base_url must be an absolute URL")
	}
	if parsed.Scheme != "https" && !loopbackHost(parsed.Hostname()) {
		return errors.New("base_url must use https unless it is a loopback test endpoint")
	}
	if provider.InputPricePerMillion < 0 || provider.OutputPricePerMillion < 0 {
		return errors.New("prices must not be negative")
	}
	if (provider.InputPricePerMillion > 0) != (provider.OutputPricePerMillion > 0) {
		return errors.New("input and output prices must both be positive or both zero")
	}
	if provider.Model == "" || provider.APIKeyEnv == "" {
		return errors.New("model and api_key_env are required")
	}
	if provider.ThinkingMode != "" && provider.ThinkingMode != "enabled" && provider.ThinkingMode != "disabled" {
		return errors.New("thinking_mode must be enabled or disabled")
	}
	if provider.MaxOutputTokens <= 0 {
		return errors.New("max_output_tokens must be positive")
	}
	if provider.TimeoutSeconds <= 0 {
		return errors.New("timeout_seconds must be positive")
	}
	return nil
}

func loopbackHost(host string) bool {
	switch host {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

func findProvider(file *ProviderFile, id string) (Provider, error) {
	for _, provider := range file.Providers {
		if strings.EqualFold(provider.ID, id) {
			return provider, nil
		}
	}
	return Provider{}, fmt.Errorf("unknown provider %q", id)
}

func loadBudget(path string) (*Budget, error) {
	var budget Budget
	if err := readJSON(path, &budget); err != nil {
		return nil, err
	}
	if budget.SchemaVersion != 1 {
		return nil, fmt.Errorf("%s: unsupported budget schema_version %d", path, budget.SchemaVersion)
	}
	if budget.AuthorizedBy == "" || budget.AuthorizedAt == "" || budget.ExpiresAt == "" {
		return nil, fmt.Errorf("%s: authorized_by, authorized_at and expires_at are required", path)
	}
	if budget.AuthorizationID == "" {
		return nil, fmt.Errorf("%s: authorization_id is required (a stable id for this cumulative budget)", path)
	}
	if budget.MaxCalls <= 0 || budget.MaxInputChars <= 0 || budget.MaxOutputTokens <= 0 {
		return nil, fmt.Errorf("%s: call, input and output limits must be positive", path)
	}
	if budget.MaxCostUSD < 0 {
		return nil, fmt.Errorf("%s: max_cost_usd must not be negative", path)
	}
	if budget.MaxCostUSD == 0 && !budget.AllowWithoutCostCap {
		return nil, fmt.Errorf("%s: a zero cost cap requires allow_without_cost_cap=true; the engine will not claim a cost limit it cannot enforce", path)
	}
	if _, err := time.Parse("2006-01-02", budget.AuthorizedAt); err != nil {
		return nil, fmt.Errorf("%s: authorized_at must be YYYY-MM-DD", path)
	}
	if _, err := time.Parse("2006-01-02", budget.ExpiresAt); err != nil {
		return nil, fmt.Errorf("%s: expires_at must be YYYY-MM-DD", path)
	}
	if len(budget.Purposes) == 0 {
		return nil, fmt.Errorf("%s: purposes must not be empty", path)
	}
	return &budget, nil
}

func (budget *Budget) validate(purpose string, now time.Time) error {
	expiry, err := time.Parse("2006-01-02", budget.ExpiresAt)
	if err != nil {
		return err
	}
	if now.UTC().After(expiry.Add(24*time.Hour - time.Second)) {
		return fmt.Errorf("budget expired on %s", budget.ExpiresAt)
	}
	for _, allowed := range budget.Purposes {
		if allowed == "all" || strings.EqualFold(allowed, purpose) {
			return nil
		}
	}
	return fmt.Errorf("purpose %q is not authorized by this budget", purpose)
}

func loadUsage(path string) (*Usage, error) {
	var usage Usage
	if err := readJSON(path, &usage); err != nil {
		if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file") {
			return &Usage{SchemaVersion: 2}, nil
		}
		return nil, err
	}
	if usage.SchemaVersion == 1 {
		usage.SchemaVersion = 2
	}
	if usage.SchemaVersion != 2 {
		return nil, fmt.Errorf("%s: unsupported usage schema_version %d", path, usage.SchemaVersion)
	}
	return &usage, nil
}

func (usage *Usage) save(path string, now time.Time) error {
	usage.UpdatedAt = now.UTC().Format(time.RFC3339)
	encoded, err := json.MarshalIndent(usage, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(encoded, '\n'), true)
}

func NewModelClient(provider Provider, budget *Budget, store UsageStore, apiKey string, now func() time.Time) *ModelClient {
	timeout := time.Duration(provider.TimeoutSeconds) * time.Second
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	return &ModelClient{
		provider: provider,
		budget:   budget,
		store:    store,
		apiKey:   apiKey,
		now:      now,
		http: &http.Client{
			Timeout:   timeout,
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("model endpoint redirects are not allowed")
			},
		},
	}
}

func estimateTokens(chars int) int {
	if chars <= 0 {
		return 0
	}
	return chars/4 + 1
}

func (client *ModelClient) estimatedCost(inputTokens, outputTokens int) float64 {
	return float64(inputTokens)*client.provider.InputPricePerMillion/1_000_000 +
		float64(outputTokens)*client.provider.OutputPricePerMillion/1_000_000
}

func (client *ModelClient) reserve(request ModelRequest) (int, int, float64, error) {
	now := client.now()
	if err := client.budget.validate(request.Purpose, now); err != nil {
		return 0, 0, 0, err
	}
	if client.budget.MaxCostUSD > 0 && !client.provider.PricingKnown() {
		return 0, 0, 0, errors.New("a hard cost cap was requested but provider pricing is missing or zero; supply both prices or allow_without_cost_cap")
	}
	inputChars := len(request.System) + len(request.User)
	if inputChars > client.budget.MaxInputChars {
		return 0, 0, 0, fmt.Errorf("input %d characters exceeds the authorized %d", inputChars, client.budget.MaxInputChars)
	}
	maxOutput := request.MaxOutputTokens
	if maxOutput <= 0 || maxOutput > client.provider.MaxOutputTokens {
		maxOutput = client.provider.MaxOutputTokens
	}
	if maxOutput > client.budget.MaxOutputTokens {
		return 0, 0, 0, fmt.Errorf("max output %d exceeds the authorized %d", maxOutput, client.budget.MaxOutputTokens)
	}
	inputTokens := estimateTokens(inputChars)
	estimate := client.estimatedCost(inputTokens, maxOutput)
	capEnforced := client.provider.PricingKnown() && client.budget.MaxCostUSD > 0
	err := client.store.Update(func(usage *Usage) error {
		if usage.AuthorizationID == "" {
			usage.AuthorizationID = client.budget.AuthorizationID
		}
		if usage.AuthorizationID != client.budget.AuthorizationID {
			return fmt.Errorf("usage ledger belongs to authorization %q, not %q; archive it before starting a new authorization",
				usage.AuthorizationID, client.budget.AuthorizationID)
		}
		if usage.Calls+1 > client.budget.MaxCalls {
			return fmt.Errorf("call budget exhausted: %d/%d (cumulative across runs)", usage.Calls, client.budget.MaxCalls)
		}
		if capEnforced && usage.CostUSD+estimate > client.budget.MaxCostUSD+1e-9 {
			return fmt.Errorf("estimated cost %.6f USD would exceed the authorized %.6f USD (cumulative)",
				usage.CostUSD+estimate, client.budget.MaxCostUSD)
		}
		if usage.Calls == 0 {
			usage.CostCapEnforced = capEnforced
		} else if !capEnforced {
			usage.CostCapEnforced = false
		}
		usage.Calls++
		usage.CostUSD += estimate
		usage.InputTokens += inputTokens
		usage.OutputTokens += maxOutput
		return nil
	})
	if err != nil {
		return 0, 0, 0, fmt.Errorf("record reservation: %w", err)
	}
	return inputTokens, maxOutput, estimate, nil
}

func (client *ModelClient) settle(reservationInput, reservationOutput int, reservationCost float64,
	inputTokens, outputTokens int, now time.Time) error {
	if inputTokens <= 0 {
		inputTokens = reservationInput
	}
	if outputTokens <= 0 {
		outputTokens = reservationOutput
	}
	actual := client.estimatedCost(inputTokens, outputTokens)
	return client.store.Update(func(usage *Usage) error {
		if usage.AuthorizationID != client.budget.AuthorizationID {
			return fmt.Errorf("usage ledger belongs to authorization %q, not %q", usage.AuthorizationID, client.budget.AuthorizationID)
		}
		usage.CostUSD += actual - reservationCost
		usage.InputTokens += inputTokens - reservationInput
		usage.OutputTokens += outputTokens - reservationOutput
		if usage.CostUSD < 0 {
			usage.CostUSD = 0
		}
		return nil
	})
}

// Complete performs one bounded model call. It refuses before the request when
// the purpose, call count, input size, output size or cost would exceed the
// owner's authorization.
func (client *ModelClient) Complete(ctx context.Context, request ModelRequest) (*ModelResponse, error) {
	reservationInput, reservationOutput, reservationCost, err := client.reserve(request)
	if err != nil {
		return nil, err
	}
	now := client.now()
	text, inputTokens, outputTokens, err := client.do(ctx, request)
	if err != nil {
		_ = client.settle(reservationInput, reservationOutput, reservationCost, reservationInput, reservationOutput, now)
		return nil, err
	}
	if inputTokens <= 0 {
		inputTokens = reservationInput
	}
	if outputTokens <= 0 {
		outputTokens = estimateTokens(len(text))
	}
	if err := client.settle(reservationInput, reservationOutput, reservationCost, inputTokens, outputTokens, now); err != nil {
		return nil, err
	}
	return &ModelResponse{
		Text:            text,
		Provider:        client.provider.ID,
		Model:           client.provider.Model,
		InputTokens:     inputTokens,
		OutputTokens:    outputTokens,
		CostUSD:         client.estimatedCost(inputTokens, outputTokens),
		CostCapEnforced: client.provider.PricingKnown() && client.budget.MaxCostUSD > 0,
		PricingKnown:    client.provider.PricingKnown(),
	}, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (client *ModelClient) do(ctx context.Context, request ModelRequest) (string, int, int, error) {
	switch client.provider.Protocol {
	case "openai":
		return client.doOpenAI(ctx, request)
	case "anthropic":
		return client.doAnthropic(ctx, request)
	default:
		return "", 0, 0, fmt.Errorf("unsupported protocol %q", client.provider.Protocol)
	}
}

func (client *ModelClient) endpoint(path string) string {
	return strings.TrimRight(client.provider.BaseURL, "/") + path
}

func (client *ModelClient) doOpenAI(ctx context.Context, request ModelRequest) (string, int, int, error) {
	payload := map[string]any{
		"model":       client.provider.Model,
		"temperature": 0.2,
		"max_tokens":  client.provider.MaxOutputTokens,
		"messages": []map[string]string{
			{"role": "system", "content": request.System},
			{"role": "user", "content": request.User},
		},
	}
	if request.JSON && client.provider.JSONMode {
		payload["response_format"] = map[string]string{"type": "json_object"}
	}
	if client.provider.ThinkingMode != "" {
		payload["thinking"] = map[string]string{"type": client.provider.ThinkingMode}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", 0, 0, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint("/chat/completions"), bytes.NewReader(body))
	if err != nil {
		return "", 0, 0, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+client.apiKey)
	httpRequest.Header.Set("User-Agent", "derive-world-growth-engine/1.0")
	response, err := client.http.Do(httpRequest)
	if err != nil {
		return "", 0, 0, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil {
		return "", 0, 0, err
	}
	if len(raw) > 1<<20 {
		return "", 0, 0, errors.New("model response exceeds 1 MiB")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", 0, 0, fmt.Errorf("model endpoint returned %s: %s", response.Status, redact(truncateText(string(raw), 300)))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", 0, 0, fmt.Errorf("decode model response: %w", err)
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return "", 0, 0, errors.New("model returned no text")
	}
	return parsed.Choices[0].Message.Content, parsed.Usage.PromptTokens, parsed.Usage.CompletionTokens, nil
}

func (client *ModelClient) doAnthropic(ctx context.Context, request ModelRequest) (string, int, int, error) {
	payload := map[string]any{
		"model":       client.provider.Model,
		"max_tokens":  client.provider.MaxOutputTokens,
		"temperature": 0.2,
		"system":      request.System,
		"messages": []map[string]string{
			{"role": "user", "content": request.User},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", 0, 0, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint("/v1/messages"), bytes.NewReader(body))
	if err != nil {
		return "", 0, 0, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("x-api-key", client.apiKey)
	httpRequest.Header.Set("anthropic-version", "2023-06-01")
	httpRequest.Header.Set("User-Agent", "derive-world-growth-engine/1.0")
	response, err := client.http.Do(httpRequest)
	if err != nil {
		return "", 0, 0, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil {
		return "", 0, 0, err
	}
	if len(raw) > 1<<20 {
		return "", 0, 0, errors.New("model response exceeds 1 MiB")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", 0, 0, fmt.Errorf("model endpoint returned %s: %s", response.Status, redact(truncateText(string(raw), 300)))
	}
	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", 0, 0, fmt.Errorf("decode model response: %w", err)
	}
	var text strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", 0, 0, errors.New("model returned no text")
	}
	return text.String(), parsed.Usage.InputTokens, parsed.Usage.OutputTokens, nil
}

func truncateText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

// loadModelClientFromFlags resolves the provider, key, budget and usage ledger
// shared by every AI command.
const defaultUsagePath = "var/growth-engine/usage.json"

func loadModelClient(providersPath, providerID, budgetPath, usagePath, usageGitDir, usageGitBranch, purpose string) (*ModelClient, error) {
	if providersPath == "" || providerID == "" || budgetPath == "" {
		return nil, errors.New("--providers, --provider and --budget are required for a model call")
	}
	providers, err := loadProviders(providersPath)
	if err != nil {
		return nil, err
	}
	provider, err := findProvider(providers, providerID)
	if err != nil {
		return nil, err
	}
	budget, err := loadBudget(budgetPath)
	if err != nil {
		return nil, err
	}
	if budget.MaxCostUSD > 0 && !provider.PricingKnown() {
		return nil, errors.New("the budget asks for a hard cost cap but provider pricing is missing or zero; supply input and output prices, or set max_cost_usd=0 with allow_without_cost_cap=true")
	}
	if budget.MaxCostUSD == 0 && !budget.AllowWithoutCostCap {
		return nil, errors.New("the budget has no cost cap; set allow_without_cost_cap=true to run with call/token limits only")
	}
	apiKey := os.Getenv(provider.APIKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("credential environment variable %s is not set; no model call was made", provider.APIKeyEnv)
	}
	now := time.Now
	var store UsageStore
	if usageGitDir != "" {
		file := usagePath
		if file == "" || file == defaultUsagePath {
			file = "usage.json"
		}
		file = filepath.ToSlash(file)
		if filepath.IsAbs(file) {
			file = filepath.Base(file)
		}
		branch := usageGitBranch
		if branch == "" {
			branch = "dge-state"
		}
		store = &GitUsageStore{Dir: usageGitDir, File: file, Branch: branch, Now: now}
	} else {
		if usagePath == "" {
			usagePath = defaultUsagePath
		}
		store = &FileUsageStore{Path: usagePath, Now: now}
	}
	return NewModelClient(provider, budget, store, apiKey, now), nil
}
