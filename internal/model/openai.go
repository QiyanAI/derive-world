// SPDX-License-Identifier: Apache-2.0
package model

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	// maxModelResponseBytes bounds a model HTTP response before decoding. It
	// is deliberately below the 1 MiB generic request limit so a hostile or
	// broken endpoint cannot force unbounded buffering.
	maxModelResponseBytes = 1 << 20
	modelConnectTimeout   = 5 * time.Second
)

// OpenAIOptions configures the OpenAI-compatible adapter. ChatPath,
// ResponseFormat, and ThinkingMode are optional capabilities; empty values
// preserve the generic request.
type OpenAIOptions struct {
	BaseURL         string
	APIKey          string
	DefaultModel    string
	MaxOutputTokens int
	Timeout         time.Duration
	ChatPath        string
	ResponseFormat  string
	ThinkingMode    string
}

// OpenAICompatible calls POST {BaseURL}{ChatPath} exactly once.
type OpenAICompatible struct {
	baseURL         string
	apiKey          string
	defaultModel    string
	maxOutputTokens int
	timeout         time.Duration
	chatPath        string
	responseFormat  string
	thinkingMode    string
	client          *http.Client
}

// NewOpenAICompatible validates adapter settings.
func NewOpenAICompatible(options OpenAIOptions) (*OpenAICompatible, error) {
	baseURL := normalizeBaseURL(options.BaseURL)
	if baseURL == "" {
		return nil, &Error{Code: CodeInvalidRequest, Message: "MODEL_BASE_URL is required"}
	}
	if options.APIKey == "" {
		return nil, &Error{Code: CodeInvalidRequest, Message: "MODEL_API_KEY is required"}
	}
	if options.DefaultModel == "" {
		return nil, &Error{Code: CodeInvalidRequest, Message: "MODEL_NAME is required"}
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	maxOutputTokens := options.MaxOutputTokens
	if maxOutputTokens <= 0 {
		maxOutputTokens = 2048
	}
	chatPath := options.ChatPath
	if chatPath == "" {
		chatPath = "/v1/chat/completions"
	}
	if !validChatPath(chatPath) {
		return nil, &Error{Code: CodeInvalidRequest, Message: "MODEL_CHAT_PATH is invalid"}
	}
	responseFormat := options.ResponseFormat
	if responseFormat != "" && responseFormat != "json_object" {
		return nil, &Error{Code: CodeInvalidRequest, Message: "MODEL_RESPONSE_FORMAT must be empty or json_object"}
	}
	thinkingMode := options.ThinkingMode
	switch thinkingMode {
	case "", "default", "disabled":
	default:
		return nil, &Error{Code: CodeInvalidRequest, Message: "MODEL_THINKING_MODE must be empty, default, or disabled"}
	}
	transport := &http.Transport{
		// No environment or system proxy: the API key must never be sent to
		// an unconfigured intermediary. The endpoint is operator-controlled.
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   modelConnectTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  timeout,
		ExpectContinueTimeout:  time.Second,
		MaxIdleConns:           4,
		MaxIdleConnsPerHost:    4,
		MaxConnsPerHost:        4,
		IdleConnTimeout:        90 * time.Second,
		ForceAttemptHTTP2:      true,
		DisableCompression:     true,
		MaxResponseHeaderBytes: 64 << 10,
	}
	return &OpenAICompatible{
		baseURL:         baseURL,
		apiKey:          options.APIKey,
		defaultModel:    options.DefaultModel,
		maxOutputTokens: maxOutputTokens,
		timeout:         timeout,
		chatPath:        chatPath,
		responseFormat:  responseFormat,
		thinkingMode:    thinkingMode,
		client: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// Mode reports real adapter mode.
func (o *OpenAICompatible) Mode() string {
	return "real"
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string              `json:"model"`
	Messages       []chatMessage       `json:"messages"`
	MaxTokens      int                 `json:"max_tokens"`
	Temperature    float64             `json:"temperature"`
	ResponseFormat *chatResponseFormat `json:"response_format,omitempty"`
	Thinking       *chatThinking       `json:"thinking,omitempty"`
}

type chatResponseFormat struct {
	Type string `json:"type"`
}

type chatThinking struct {
	Type string `json:"type"`
}

// validChatPath accepts only a bounded absolute path. It rejects host
// injection, query/fragment injection, whitespace, backslashes, and traversal.
func validChatPath(value string) bool {
	if len(value) < 2 || len(value) > 128 || value[0] != '/' || strings.HasPrefix(value, "//") {
		return false
	}
	if strings.ContainsAny(value, "?#\\ \t\r\n") || strings.Contains(value, "..") {
		return false
	}
	return true
}

type chatResponse struct {
	Model   string `json:"model"`
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

// Complete performs one non-streaming chat completion.
func (o *OpenAICompatible) Complete(ctx context.Context, req Request) (Response, error) {
	modelName := req.Model
	if modelName == "" {
		modelName = o.defaultModel
	}

	inputSize := len(req.System)
	for _, message := range req.Messages {
		inputSize += len(message.Content)
	}
	if inputSize > maxInputBytes {
		return Response{}, &Error{Code: CodePayloadTooLarge, Message: "model input exceeds 64 KiB"}
	}

	messages := make([]chatMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, chatMessage{Role: "system", Content: req.System})
	}
	for _, message := range req.Messages {
		messages = append(messages, chatMessage{Role: message.Role, Content: message.Content})
	}

	// A caller can lower the output-token bound but never raise it above the
	// deployment ceiling. This keeps the configured token budget authoritative.
	maxTokens := req.MaxOutputTokens
	if maxTokens <= 0 || maxTokens > o.maxOutputTokens {
		maxTokens = o.maxOutputTokens
	}

	request := chatRequest{
		Model:       modelName,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
	}
	if o.responseFormat != "" {
		request.ResponseFormat = &chatResponseFormat{Type: o.responseFormat}
	}
	if o.thinkingMode == "disabled" {
		request.Thinking = &chatThinking{Type: "disabled"}
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return Response{}, &Error{Code: CodeInvalidRequest, Message: "could not encode model request", Err: err}
	}

	requestCtx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	httpRequest, err := http.NewRequestWithContext(requestCtx, http.MethodPost, o.baseURL+o.chatPath, bytes.NewReader(payload))
	if err != nil {
		return Response{}, &Error{Code: CodeInvalidRequest, Message: "could not build model request", Err: err}
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+o.apiKey)

	httpResponse, err := o.client.Do(httpRequest)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return Response{}, &Error{Code: CodeTimeout, Message: "model request timed out", Err: err}
		}
		var netErr interface{ Timeout() bool }
		if errors.As(err, &netErr) && netErr.Timeout() {
			return Response{}, &Error{Code: CodeTimeout, Message: "model request timed out", Err: err}
		}
		return Response{}, &Error{Code: CodeUnavailable, Message: "model endpoint is unavailable", Err: err}
	}
	defer httpResponse.Body.Close()

	limited := io.LimitReader(httpResponse.Body, maxModelResponseBytes+1)
	raw, readErr := io.ReadAll(limited)
	if readErr != nil {
		// The status was known but the body could not be completed. The
		// provider may have generated (and billed) output, so this is never a
		// success and is never retried automatically.
		return Response{}, &Error{Code: CodeUnavailable, Message: "model response could not be read", Err: readErr}
	}
	if len(raw) > maxModelResponseBytes {
		return Response{}, &Error{Code: CodeInvalidOutput, Message: "model response exceeds the 1 MiB limit"}
	}
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		switch httpResponse.StatusCode {
		case http.StatusRequestTimeout, http.StatusGatewayTimeout:
			return Response{}, &Error{Code: CodeTimeout, Message: fmt.Sprintf("model endpoint returned status %d", httpResponse.StatusCode)}
		case http.StatusTooManyRequests:
			return Response{}, &Error{Code: CodeRateLimited, Message: "model endpoint rate limited the request"}
		default:
			return Response{}, &Error{Code: CodeUnavailable, Message: fmt.Sprintf("model endpoint returned status %d", httpResponse.StatusCode)}
		}
	}

	var decoded chatResponse
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&decoded); err != nil {
		return Response{}, &Error{Code: CodeInvalidOutput, Message: "model response is not valid JSON", Err: err}
	}
	if len(decoded.Choices) == 0 {
		return Response{}, &Error{Code: CodeInvalidOutput, Message: "model response has no choices"}
	}
	text := decoded.Choices[0].Message.Content
	if strings.TrimSpace(text) == "" {
		return Response{}, &Error{Code: CodeInvalidOutput, Message: "model response is empty"}
	}
	if len(text) > maxOutputBytes {
		return Response{}, &Error{Code: CodeInvalidOutput, Message: "model response exceeds 8 KiB"}
	}
	responseModel := decoded.Model
	if responseModel == "" {
		responseModel = modelName
	}
	inputTokens := decoded.Usage.PromptTokens
	if inputTokens < 0 {
		inputTokens = 0
	}
	outputTokens := decoded.Usage.CompletionTokens
	if outputTokens < 0 {
		outputTokens = 0
	}
	return Response{
		Text:  text,
		Model: responseModel,
		Usage: Usage{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
		},
	}, nil
}

func normalizeBaseURL(raw string) string {
	value := strings.TrimRight(strings.TrimSpace(raw), "/")
	if strings.HasSuffix(value, "/v1") {
		value = strings.TrimSuffix(value, "/v1")
		value = strings.TrimRight(value, "/")
	}
	return value
}
