// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type worldFixture struct {
	ObjectType string          `json:"object_type"`
	Title      string          `json:"title"`
	Content    json.RawMessage `json:"content"`
}

// TestExampleWorldsValidate keeps the committed example worlds honest. The
// Derive Relay base world must be a body the real create path accepts.
func TestExampleWorldsValidate(t *testing.T) {
	t.Helper()
	dir := filepath.Join("..", "..", "examples", "worlds")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read example worlds: %v", err)
	}
	checked := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var fixture worldFixture
		if err := json.Unmarshal(content, &fixture); err != nil {
			t.Fatalf("%s is not valid JSON: %v", path, err)
		}
		if fixture.ObjectType != TypeOpenWorld {
			t.Fatalf("%s must be an open_world fixture", path)
		}
		if fixture.Title == "" {
			t.Fatalf("%s has no title", path)
		}
		if _, _, err := ValidateCreate(CreateInput{
			ObjectType: fixture.ObjectType,
			Title:      fixture.Title,
			Content:    fixture.Content,
		}); err != nil {
			t.Fatalf("%s does not pass ValidateCreate: %v", path, err)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no example worlds were checked")
	}
}
