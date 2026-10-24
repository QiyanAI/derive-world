// SPDX-License-Identifier: Apache-2.0
package capability

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCanonicalInputHashIsStable(t *testing.T) {
	first, err := canonicalInputHash(IDObjectRead,
		[]byte(`{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc"}`))
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	second, err := canonicalInputHash(IDObjectRead,
		[]byte(`{ "object_id" : "0192f0c4-5a6b-7c8d-9e0f-123456789abc" }`))
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("canonical hash depends on whitespace")
	}
	if len(first) != 32 {
		t.Fatalf("hash length = %d", len(first))
	}
}

func TestRedactedInputNeverContainsText(t *testing.T) {
	raw := []byte(`{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","text":"secret project text","format":"markdown"}`)
	encoded, err := redactedInput(IDContentStructure, raw)
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if bytes.Contains(encoded, []byte("secret project text")) {
		t.Fatal("redacted input contains the original text")
	}
	if len(encoded) > maxRedactedBytes {
		t.Fatalf("redacted input is %d bytes", len(encoded))
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	fields := decoded["fields"].(map[string]any)
	text := fields["text"].(map[string]any)
	if text["chars"] == nil || text["sha256"] == nil {
		t.Fatalf("text summary = %v", text)
	}
	if fields["format"] != "markdown" {
		t.Fatalf("short non-sensitive field = %v", fields["format"])
	}
}

func TestRedactedInputRedactsTitlesAndLongStrings(t *testing.T) {
	long := strings.Repeat("x", 100)
	raw := []byte(`{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","title":"` + long + `","summary":"short"}`)
	encoded, err := redactedInput(IDObjectProposeChange, raw)
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if bytes.Contains(encoded, []byte(long)) || bytes.Contains(encoded, []byte(`"short"`)) {
		t.Fatalf("sensitive value leaked: %s", encoded)
	}
}
