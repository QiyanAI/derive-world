// SPDX-License-Identifier: Apache-2.0
package publication

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QiyanAI/derive-world/internal/objects"
)

func TestBuildSnapshotIsWhitelisted(t *testing.T) {
	state := json.RawMessage(`{
		"title":"Public title",
		"summary":"Public summary",
		"body":"Public body",
		"checklist":[{"id":"x","text":"visible item","done":false}],
		"tags":["tag"]
	}`)
	encoded, title, summary, err := buildSnapshot(objects.TypeCollaborative, objects.TemplateCollaborative, state)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if title != "Public title" || summary != "Public summary" {
		t.Fatalf("title/summary = %q/%q", title, summary)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if len(decoded) != 4 {
		t.Fatalf("snapshot keys = %v, want exactly title/object_type/presentation_template/state", decoded)
	}
	if decoded["object_type"] != objects.TypeCollaborative ||
		decoded["presentation_template"] != objects.TemplateCollaborative {
		t.Fatalf("snapshot header = %v", decoded)
	}
	text := string(encoded)
	for _, forbidden := range []string{"shared_context", "memberships", "private_ai", "api_key"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("snapshot leaked %q: %s", forbidden, text)
		}
	}
}

func TestBuildSnapshotRejectsUnknownType(t *testing.T) {
	if _, _, _, err := buildSnapshot("unknown", "unknown_v1", json.RawMessage(`{"title":"x"}`)); err == nil {
		t.Fatal("unknown type was accepted")
	}
}
