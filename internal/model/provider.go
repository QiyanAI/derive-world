// SPDX-License-Identifier: Apache-2.0

// Package model defines the provider interface, deterministic fixture adapter,
// and OpenAI-compatible adapter. It does not parse business actions or write
// any table other than its own future model_invocations records.
package model

import (
	"context"
	"time"
)

// Message is one chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request is a single completion request.
type Request struct {
	Model           string
	System          string
	Messages        []Message
	MaxOutputTokens int
	Temperature     float64
}

// Usage reports token usage when the provider supplies it.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Response is a single completion response.
type Response struct {
	Text  string
	Usage Usage
	Model string
}

// Provider is the frozen model interface from G00 contract §A.9.1.
type Provider interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

// ModeProvider is implemented by adapters that can report their mode.
type ModeProvider interface {
	Mode() string
}

// ErrorCode is the model-layer error classification.
type ErrorCode string

const (
	CodeUnavailable     ErrorCode = "model_unavailable"
	CodeTimeout         ErrorCode = "model_timeout"
	CodeInvalidOutput   ErrorCode = "model_invalid_output"
	CodePayloadTooLarge ErrorCode = "payload_too_large"
	CodeInvalidRequest  ErrorCode = "invalid_request"
	// CodeRateLimited reports an upstream 429. It maps to the frozen 429
	// rate_limited response and is never retried automatically.
	CodeRateLimited ErrorCode = "rate_limited"
)

// Error is a model-layer error with a closed code.
type Error struct {
	Code    ErrorCode
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return string(e.Code) + ": " + e.Message + ": " + e.Err.Error()
	}
	return string(e.Code) + ": " + e.Message
}

func (e *Error) Unwrap() error {
	return e.Err
}

// AdapterOptions selects and configures one model adapter. ChatPath,
// ResponseFormat, and ThinkingMode are optional adapter capabilities, not
// business-domain concepts. Empty values preserve the generic
// OpenAI-compatible request used by every existing deployment.
type AdapterOptions struct {
	Mode            string
	BaseURL         string
	APIKey          string
	ModelName       string
	Timeout         time.Duration
	MaxOutputTokens int
	FixtureDir      string
	// ChatPath overrides the chat-completions path. The default is
	// /v1/chat/completions.
	ChatPath string
	// ResponseFormat optionally requests a provider-supported response
	// format. The only accepted value is json_object.
	ResponseFormat string
	// ThinkingMode optionally disables a provider's default reasoning mode.
	// The only accepted non-empty value is disabled.
	ThinkingMode string
}

// New creates the adapter selected by mode with the generic defaults.
func New(mode, baseURL, apiKey, modelName string, timeout time.Duration, maxOutputTokens int, fixtureDir string) (Provider, error) {
	return NewWithOptions(AdapterOptions{
		Mode: mode, BaseURL: baseURL, APIKey: apiKey, ModelName: modelName,
		Timeout: timeout, MaxOutputTokens: maxOutputTokens, FixtureDir: fixtureDir,
	})
}

// NewWithOptions creates the adapter selected by mode. Optional adapter
// capabilities are passed only to the OpenAI-compatible adapter and never to
// the business layer.
func NewWithOptions(options AdapterOptions) (Provider, error) {
	switch options.Mode {
	case "fixture":
		return NewFixture(options.FixtureDir), nil
	case "real":
		return NewOpenAICompatible(OpenAIOptions{
			BaseURL:         options.BaseURL,
			APIKey:          options.APIKey,
			DefaultModel:    options.ModelName,
			MaxOutputTokens: options.MaxOutputTokens,
			Timeout:         options.Timeout,
			ChatPath:        options.ChatPath,
			ResponseFormat:  options.ResponseFormat,
			ThinkingMode:    options.ThinkingMode,
		})
	default:
		return nil, &Error{Code: CodeInvalidRequest, Message: "unknown model mode"}
	}
}
