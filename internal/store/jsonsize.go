// SPDX-License-Identifier: Apache-2.0
package store

import (
	"bytes"
	"encoding/json"
)

// CompactJSONSize returns the byte length of a compact JSON encoding with
// HTML escaping disabled. The frozen byte limits in D04-21 are defined on
// octet_length(value::text) for jsonb, where &, < and > occupy one byte and
// U+2028/U+2029 occupy three UTF-8 bytes. Go's json.Marshal escapes those to
// six bytes, which would reject a body that is legal under the frozen limit.
//
// This measurement is a lower bound on the jsonb text length (it omits the
// spaces jsonb adds after colons and commas). It is used only by application
// pre-checks; the database CHECK remains the authoritative limit and maps a
// rejection to the same 413 semantics.
func CompactJSONSize(value any) (int, error) {
	encoded, err := MarshalCompactJSON(value)
	if err != nil {
		return 0, err
	}
	size := len(encoded)
	// encoding/json always escapes U+2028 and U+2029 even with HTML escaping
	// disabled; jsonb text stores them as three bytes each. Adjust the
	// measurement only. Literal backslash-u sequences are underestimated by
	// three bytes, which is safe for a pre-check.
	size -= 3 * (bytes.Count(encoded, []byte("\\u2028")) +
		bytes.Count(encoded, []byte("\\u2029")))
	return size, nil
}

// MarshalCompactJSON encodes value as compact JSON with HTML escaping
// disabled. Storing this form changes no JSON value: PostgreSQL parses it to
// the same jsonb. It keeps the bytes used for state snapshots and events
// aligned with the representation the frozen byte limits are defined on.
func MarshalCompactJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}
