// SPDX-License-Identifier: Apache-2.0
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

// WriteJSON writes a JSON response with no-store caching.
func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// DecodeJSON decodes a single JSON object with a 1 MiB default limit and
// unknown-field rejection. Callers should map returned errors with WriteError.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	if r.Body == nil {
		return NewError(CodeInvalidRequest, "request body is required")
	}

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		return NewError(CodeUnsupportedMediaType, "Content-Type must be application/json")
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return NewError(CodeUnsupportedMediaType, "Content-Type must be application/json")
	}

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return NewError(CodePayloadTooLarge, "request body exceeds the limit")
		}
		if errors.Is(err, io.EOF) {
			return NewError(CodeInvalidRequest, "request body is empty")
		}
		return NewError(CodeInvalidRequest, "request body is not valid JSON for this endpoint")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return NewError(CodeInvalidRequest, "request body must contain a single JSON value")
	}
	return nil
}
