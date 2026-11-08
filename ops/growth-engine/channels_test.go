// SPDX-License-Identifier: Apache-2.0
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChannelFitAndAuthorization(t *testing.T) {
	file, err := loadChannels("state/channels.json")
	if err != nil {
		t.Fatal(err)
	}
	github, ok := findChannel(file, "github")
	if !ok {
		t.Fatal("github channel missing")
	}
	if err := CheckChannel(github, "technical-intro"); err != nil {
		t.Fatalf("github rejected a technical intro: %v", err)
	}
	hackerNews, ok := findChannel(file, "hacker-news")
	if !ok {
		t.Fatal("hacker-news channel missing")
	}
	if err := CheckChannel(hackerNews, "technical-intro"); err == nil {
		t.Fatal("an owner-account channel was treated as authorized")
	}
	if err := CheckChannel(github, "paid-promotion"); err == nil {
		t.Fatal("an unknown content kind was accepted")
	}
}

func TestChannelRegistryRejectsUnknownAuthorization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "channels.json")
	body := `{"schema_version":1,"updated_at":"2026-11-08","channels":[
		{"id":"x","name":"x","audience":"a","rules":["r"],"fits":["technical-intro"],"cadence":"c","authorization":"anything-goes","observed":[]}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadChannels(path); err == nil {
		t.Fatal("an unknown authorization value was accepted")
	}
}

func TestSafeJoinRejectsEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := safeJoin(root, "../outside"); err == nil {
		t.Fatal("parent traversal accepted")
	}
	if _, err := safeJoin(root, filepath.Join(root, "absolute")); err == nil {
		t.Fatal("absolute path accepted")
	}
	if _, err := safeJoin(root, "inside/path"); err != nil {
		t.Fatalf("valid relative path rejected: %v", err)
	}
}
