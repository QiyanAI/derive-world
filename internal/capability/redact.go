// SPDX-License-Identifier: Apache-2.0
package capability

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/QiyanAI/derive-world/internal/store"
)

const (
	maxRedactedBytes = 2048
	redactLongString = 64
)

var alwaysRedactedKeys = map[string]bool{
	"text": true, "body": true, "summary": true, "prompt": true, "title": true,
}

// canonicalInputHash computes SHA-256 over the canonical
// {capability_id, contract_version, input} object required by D06 §3.9.1.
func canonicalInputHash(capabilityID string, input json.RawMessage) ([]byte, error) {
	decoded, err := decodeJSON(input)
	if err != nil {
		return nil, fmt.Errorf("%w: input is not valid JSON", ErrInvalidRequest)
	}
	envelope := map[string]any{
		"capability_id":    capabilityID,
		"contract_version": ContractVersion,
		"input":            decoded,
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	canonical, err := store.CanonicalJSON(encoded)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(canonical)
	return digest[:], nil
}

// redactedInput builds the ≤2 KiB structural summary stored in
// capability_invocations.input_redacted. It never contains object text.
func redactedInput(capabilityID string, input json.RawMessage) ([]byte, error) {
	decoded, err := decodeJSON(input)
	if err != nil {
		return nil, fmt.Errorf("%w: input is not valid JSON", ErrInvalidRequest)
	}
	object, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: input must be an object", ErrInvalidRequest)
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fields := make(map[string]any, len(keys))
	for _, key := range keys {
		fields[key] = redactValue(key, object[key])
	}
	summary := map[string]any{
		"capability_id": capabilityID,
		"input_keys":    keys,
		"fields":        fields,
	}
	if value, ok := object["object_id"].(string); ok {
		summary["object_id"] = value
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		return nil, err
	}
	if len(encoded) <= maxRedactedBytes {
		return encoded, nil
	}
	// The schema bounds input to a handful of keys; a larger summary can only
	// come from a malformed request. Keep the shape, redact every field.
	for key := range fields {
		fields[key] = "[REDACTED]"
	}
	encoded, err = json.Marshal(summary)
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxRedactedBytes {
		return nil, fmt.Errorf("%w: redacted input exceeds 2 KiB", ErrInvalidRequest)
	}
	return encoded, nil
}

func redactValue(key string, value any) any {
	switch typed := value.(type) {
	case string:
		if alwaysRedactedKeys[strings.ToLower(key)] || utf8.RuneCountInString(typed) > redactLongString {
			digest := sha256.Sum256([]byte(typed))
			return map[string]any{
				"chars":  utf8.RuneCountInString(typed),
				"sha256": hex.EncodeToString(digest[:]),
			}
		}
		return typed
	case json.Number:
		return typed.String()
	case bool:
		return typed
	case nil:
		return nil
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for nested := range typed {
			keys = append(keys, nested)
		}
		sort.Strings(keys)
		return map[string]any{"type": "object", "keys": keys}
	case []any:
		return map[string]any{"type": "array", "length": len(typed)}
	default:
		return "[REDACTED]"
	}
}
