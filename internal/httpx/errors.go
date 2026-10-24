// SPDX-License-Identifier: Apache-2.0

// Package httpx provides HTTP cross-cutting concerns: request IDs, structured
// access logs, security headers, JSON helpers, error envelopes, rate limiting,
// and cursor signing. It does not contain domain logic or database access.
package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
)

// ErrorCode is the closed error-code set from G00 contract §A.4.5.
type ErrorCode string

const (
	CodeInvalidRequest       ErrorCode = "invalid_request"
	CodeUnauthenticated      ErrorCode = "unauthenticated"
	CodeForbidden            ErrorCode = "forbidden"
	CodeNotFound             ErrorCode = "not_found"
	CodeConflict             ErrorCode = "conflict"
	CodeVersionConflict      ErrorCode = "version_conflict"
	CodeIdempotencyConflict  ErrorCode = "idempotency_conflict"
	CodeRequestInProgress    ErrorCode = "request_in_progress"
	CodeConfirmationRequired ErrorCode = "confirmation_required"
	CodeConfirmationExpired  ErrorCode = "confirmation_expired"
	CodeInvitationInvalid    ErrorCode = "invitation_invalid"
	CodeRateLimited          ErrorCode = "rate_limited"
	CodeBudgetExceeded       ErrorCode = "budget_exceeded"
	CodeModelUnavailable     ErrorCode = "model_unavailable"
	CodeModelTimeout         ErrorCode = "model_timeout"
	CodeModelInvalidOutput   ErrorCode = "model_invalid_output"
	CodeCapabilityDenied     ErrorCode = "capability_denied"
	CodeCapabilityFailed     ErrorCode = "capability_failed"
	CodeCapabilityTimeout    ErrorCode = "capability_timeout"
	CodeCapabilityUnknown    ErrorCode = "capability_unknown"
	CodePayloadTooLarge      ErrorCode = "payload_too_large"
	CodeUnsupportedMediaType ErrorCode = "unsupported_media_type"
	CodeInternal             ErrorCode = "internal"
)

// StatusFor maps an error code to its HTTP status per G00 contract §A.4.5.
func StatusFor(code ErrorCode) int {
	switch code {
	case CodeInvalidRequest:
		return http.StatusBadRequest
	case CodeUnauthenticated:
		return http.StatusUnauthorized
	case CodeForbidden, CodeCapabilityDenied:
		return http.StatusForbidden
	case CodeNotFound, CodeInvitationInvalid:
		return http.StatusNotFound
	case CodeConflict, CodeVersionConflict, CodeIdempotencyConflict, CodeRequestInProgress:
		return http.StatusConflict
	case CodeConfirmationRequired:
		return http.StatusAccepted
	case CodeConfirmationExpired:
		return http.StatusGone
	case CodePayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	case CodeUnsupportedMediaType:
		return http.StatusUnsupportedMediaType
	case CodeRateLimited, CodeBudgetExceeded:
		return http.StatusTooManyRequests
	case CodeModelUnavailable, CodeModelInvalidOutput, CodeCapabilityFailed, CodeCapabilityUnknown:
		return http.StatusBadGateway
	case CodeModelTimeout, CodeCapabilityTimeout:
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

// APIError is an error with a closed code, a safe message, and structured details.
type APIError struct {
	Code    ErrorCode
	Message string
	Details map[string]any
}

func (e *APIError) Error() string {
	return string(e.Code) + ": " + e.Message
}

// NewError constructs an APIError.
func NewError(code ErrorCode, message string) *APIError {
	return &APIError{Code: code, Message: message}
}

// WithDetails returns a copy with structured details.
func (e *APIError) WithDetails(details map[string]any) *APIError {
	return &APIError{Code: e.Code, Message: e.Message, Details: details}
}

// ErrorBody is the public error payload.
type ErrorBody struct {
	Code      ErrorCode      `json:"code"`
	Message   string         `json:"message"`
	RequestID string         `json:"request_id"`
	Details   map[string]any `json:"details"`
}

type errorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// WriteError writes the unified error envelope. Unknown errors become
// internal and never expose the underlying message.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	apiErr := &APIError{Code: CodeInternal, Message: "internal server error"}
	var typed *APIError
	if errors.As(err, &typed) {
		apiErr = typed
	}

	status := StatusFor(apiErr.Code)
	body := errorEnvelope{
		Error: ErrorBody{
			Code:      apiErr.Code,
			Message:   apiErr.Message,
			RequestID: RequestIDFromContext(r.Context()),
			Details:   apiErr.Details,
		},
	}
	if body.Error.Details == nil {
		body.Error.Details = map[string]any{}
	}
	if recorder, ok := w.(interface{ SetErrorCode(string) }); ok {
		recorder.SetErrorCode(string(apiErr.Code))
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// WriteConfirmationRequired writes the 202 Accepted confirmation response.
// `confirmation_required` is a normal outcome, not an error envelope.
func WriteConfirmationRequired(w http.ResponseWriter, r *http.Request, confirmationID string) {
	if recorder, ok := w.(interface{ SetErrorCode(string) }); ok {
		recorder.SetErrorCode(string(CodeConfirmationRequired))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":          "confirmation_required",
		"confirmation_id": confirmationID,
	})
	_ = r
}
