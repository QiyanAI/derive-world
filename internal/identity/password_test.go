// SPDX-License-Identifier: Apache-2.0
package identity

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	password := "correct horse battery staple"
	encoded, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("unexpected hash format: %s", encoded)
	}
	ok, err := VerifyPassword(encoded, password)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Fatal("correct password rejected")
	}
	ok, err = VerifyPassword(encoded, "wrong password value")
	if err != nil {
		t.Fatalf("VerifyPassword wrong: %v", err)
	}
	if ok {
		t.Fatal("wrong password accepted")
	}
}

func TestPasswordValidation(t *testing.T) {
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("short password accepted")
	}
	if _, err := HashPassword(strings.Repeat(" ", 12)); err == nil {
		t.Fatal("whitespace-only password accepted")
	}
	if _, err := HashPassword(strings.Repeat("x", 129)); err == nil {
		t.Fatal("overlong password accepted")
	}
}

func TestMalformedHash(t *testing.T) {
	for _, value := range []string{"", "not-a-hash", "$argon2id$v=19$m=65536,t=3,p=2$bad"} {
		if _, err := VerifyPassword(value, "correct horse battery staple"); err == nil {
			t.Fatalf("malformed hash accepted: %q", value)
		}
	}
}
