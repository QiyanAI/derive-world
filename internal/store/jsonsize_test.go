// SPDX-License-Identifier: Apache-2.0
package store

import (
	"strings"
	"testing"
)

func TestCompactJSONSizeDoesNotCountHTMLEscaping(t *testing.T) {
	value := map[string]any{"body": strings.Repeat("&", 20000)}
	got, err := CompactJSONSize(value)
	if err != nil {
		t.Fatalf("CompactJSONSize: %v", err)
	}
	if want := 20011; got != want {
		t.Fatalf("size = %d, want %d", got, want)
	}
}

func TestCompactJSONSizeAdjustsLineSeparators(t *testing.T) {
	value := map[string]any{"body": strings.Repeat(" ", 20000)}
	got, err := CompactJSONSize(value)
	if err != nil {
		t.Fatalf("CompactJSONSize: %v", err)
	}
	if want := 60011; got != want {
		t.Fatalf("size = %d, want %d", got, want)
	}
}

func TestCanonicalJSONSizeUsesCanonicalForm(t *testing.T) {
	got, err := CanonicalJSONSize([]byte(`{"b":"&","a":1}`))
	if err != nil {
		t.Fatalf("CanonicalJSONSize: %v", err)
	}
	if want := len(`{"a":1,"b":"&"}`); got != want {
		t.Fatalf("size = %d, want %d", got, want)
	}
	canonical, err := CanonicalJSON([]byte(`{"b":"&","a":1}`))
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	if len(canonical) <= got {
		t.Fatalf("escaped canonical length %d must exceed measured %d", len(canonical), got)
	}
}

func TestCanonicalJSONSizeEmpty(t *testing.T) {
	got, err := CanonicalJSONSize([]byte("  "))
	if err != nil {
		t.Fatalf("CanonicalJSONSize: %v", err)
	}
	if got != len("null") {
		t.Fatalf("size = %d", got)
	}
}
