// SPDX-License-Identifier: Apache-2.0
package httpx

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
)

// Cursor is an opaque, signed pagination cursor.
type Cursor struct {
	Key string `json:"k"`
	ID  string `json:"id"`
}

var errInvalidCursor = errors.New("invalid cursor")

// EncodeCursor signs a cursor with the dedicated cursor secret.
func EncodeCursor(secret []byte, cursor Cursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	signature := signCursor(secret, encoded)
	return encoded + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// DecodeCursor verifies the signature and returns the cursor.
func DecodeCursor(secret []byte, raw string) (Cursor, error) {
	var cursor Cursor
	encoded, signaturePart, ok := cutLast(raw, '.')
	if !ok {
		return cursor, errInvalidCursor
	}
	signature, err := base64.RawURLEncoding.DecodeString(signaturePart)
	if err != nil {
		return cursor, errInvalidCursor
	}
	expected := signCursor(secret, encoded)
	if subtle.ConstantTimeCompare(signature, expected) != 1 {
		return cursor, errInvalidCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return cursor, errInvalidCursor
	}
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return cursor, errInvalidCursor
	}
	return cursor, nil
}

func signCursor(secret []byte, encoded string) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(encoded))
	return mac.Sum(nil)
}

func cutLast(value string, separator byte) (string, string, bool) {
	for i := len(value) - 1; i >= 0; i-- {
		if value[i] == separator {
			return value[:i], value[i+1:], true
		}
	}
	return "", "", false
}

// ParseLimit parses ?limit= with a default and maximum.
func ParseLimit(raw string, defaultValue, maxValue int) (int, error) {
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errInvalidCursor
	}
	if value < 1 || value > maxValue {
		return 0, errInvalidCursor
	}
	return value, nil
}
