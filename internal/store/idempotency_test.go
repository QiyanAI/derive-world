// SPDX-License-Identifier: Apache-2.0
package store

import (
	"bytes"
	"testing"
)

func TestCanonicalJSON(t *testing.T) {
	left, err := CanonicalJSON([]byte(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	right, err := CanonicalJSON([]byte("{\n  \"a\": 1,\n  \"b\": 2\n}"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(left, right) {
		t.Fatalf("canonical forms differ: %s vs %s", left, right)
	}
	if string(left) != `{"a":1,"b":2}` {
		t.Fatalf("canonical JSON = %s", left)
	}
	empty, err := CanonicalJSON(nil)
	if err != nil || string(empty) != "null" {
		t.Fatalf("empty canonical JSON = %s err=%v", empty, err)
	}
	if _, err := CanonicalJSON([]byte(`{"a":1} {"b":2}`)); err == nil {
		t.Fatal("multiple JSON values were accepted")
	}
	if _, err := CanonicalJSON([]byte(`{"a":1} trailing`)); err == nil {
		t.Fatal("trailing garbage was accepted")
	}

	for _, pair := range [][2]string{
		{`{"n":1}`, `{"n":1.0}`},
		{`{"n":1}`, `{"n":1e0}`},
		{`{"n":0.1}`, `{"n":0.10}`},
		{`{"n":1}`, `{"n":100e-2}`},
		{`{"n":-2.5}`, `{"n":-25e-1}`},
	} {
		left, err := CanonicalJSON([]byte(pair[0]))
		if err != nil {
			t.Fatalf("canonical %s: %v", pair[0], err)
		}
		right, err := CanonicalJSON([]byte(pair[1]))
		if err != nil {
			t.Fatalf("canonical %s: %v", pair[1], err)
		}
		if !bytes.Equal(left, right) {
			t.Fatalf("equivalent numbers differ: %s vs %s", left, right)
		}
	}
	if _, err := CanonicalJSON([]byte(`{"n":1e100000}`)); err != nil {
		t.Fatalf("extreme exponent should fall back, got %v", err)
	}
}

func TestRequestHashBindsMethodPathAndBody(t *testing.T) {
	base, err := RequestHash("POST", "/api/v1/objects/one/actions", []byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(base) != 32 {
		t.Fatalf("hash length = %d", len(base))
	}
	same, _ := RequestHash("POST", "/api/v1/objects/one/actions", []byte(`{"a":1}`))
	if !bytes.Equal(base, same) {
		t.Fatal("identical requests produced different hashes")
	}
	otherPath, _ := RequestHash("POST", "/api/v1/objects/two/actions", []byte(`{"a":1}`))
	if bytes.Equal(base, otherPath) {
		t.Fatal("object id was not bound into the request hash")
	}
	otherMethod, _ := RequestHash("PUT", "/api/v1/objects/one/actions", []byte(`{"a":1}`))
	if bytes.Equal(base, otherMethod) {
		t.Fatal("method was not bound into the request hash")
	}
	otherBody, _ := RequestHash("POST", "/api/v1/objects/one/actions", []byte(`{"a":2}`))
	if bytes.Equal(base, otherBody) {
		t.Fatal("body was not bound into the request hash")
	}
}

func TestIdempotencyKeyFormat(t *testing.T) {
	valid := []string{
		"0123456789abcdef",
		"ABCDEFGHIJKLMNOPQRSTUVWX",
		"a-b_c-0123456789",
	}
	for _, key := range valid {
		if !ValidIdempotencyKey(key) {
			t.Fatalf("valid key %q was rejected", key)
		}
	}
	invalid := []string{"", "short", "with space 0123456789", "with/slash/0123456789"}
	for _, key := range invalid {
		if ValidIdempotencyKey(key) {
			t.Fatalf("invalid key %q was accepted", key)
		}
	}
}
