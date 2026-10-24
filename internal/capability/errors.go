// SPDX-License-Identifier: Apache-2.0
package capability

import (
	"errors"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/httpx"
)

// Domain errors. The composition root maps these to the frozen error
// envelope; invocation-bound failures also carry the invocation id.
var (
	ErrNotFound        = errors.New("capability not found")
	ErrDenied          = errors.New("capability denied")
	ErrForbidden       = errors.New("forbidden")
	ErrInvalidRequest  = errors.New("invalid capability request")
	ErrPayloadTooLarge = errors.New("capability payload too large")
	ErrVersionConflict = errors.New("version conflict")
	ErrConflict        = errors.New("conflict")
	ErrFailed          = errors.New("capability failed")
	ErrTimeout         = errors.New("capability timeout")
	ErrUnknown         = errors.New("capability result unknown")
	ErrUnavailable     = errors.New("capability service unavailable")
)

// Failure is an invocation-bound failure with a public, closed error code.
type Failure struct {
	Code         httpx.ErrorCode
	Message      string
	InvocationID *uuid.UUID
	Confirmation *uuid.UUID
	Field        string
	Cause        error
}

func (f *Failure) Error() string {
	if f == nil {
		return "<nil>"
	}
	if f.Cause != nil {
		return string(f.Code) + ": " + f.Message + ": " + f.Cause.Error()
	}
	return string(f.Code) + ": " + f.Message
}

// Unwrap allows errors.Is against the underlying domain sentinel.
func (f *Failure) Unwrap() error {
	if f == nil {
		return nil
	}
	return f.Cause
}

// AsAPIError converts a failure into the public error envelope. Details are
// restricted to the four keys permitted by D06 §3.11.
func (f *Failure) AsAPIError() *httpx.APIError {
	details := map[string]any{}
	if f.InvocationID != nil {
		details["invocation_id"] = f.InvocationID.String()
	}
	if f.Confirmation != nil {
		details["confirmation_id"] = f.Confirmation.String()
	}
	if f.Field != "" {
		details["field"] = f.Field
	}
	return httpx.NewError(f.Code, f.Message).WithDetails(details)
}

func newFailure(code httpx.ErrorCode, message string, cause error) *Failure {
	return &Failure{Code: code, Message: message, Cause: cause}
}

func deniedFailure(code httpx.ErrorCode, message string, invocationID uuid.UUID, cause error) *Failure {
	if invocationID != uuid.Nil {
		return failureWithInvocation(code, message, invocationID, cause)
	}
	return newFailure(code, message, cause)
}

func failureWithInvocation(code httpx.ErrorCode, message string, invocationID uuid.UUID, cause error) *Failure {
	return &Failure{Code: code, Message: message, InvocationID: &invocationID, Cause: cause}
}
