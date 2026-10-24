// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStructureDeterministicMarkdown(t *testing.T) {
	text := "# Title\n\nfirst paragraph\n\n- one\n- two\n\n> quoted\n\n```go\nfmt.Println(\"x\")\n\nsecond line\n```"
	first, err := structureText(text, "markdown")
	if err != nil {
		t.Fatalf("structure: %v", err)
	}
	second, err := structureText(text, "markdown")
	if err != nil {
		t.Fatalf("structure again: %v", err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("output is not deterministic")
	}
	if first.ItemCount != len(first.Items) {
		t.Fatalf("item_count=%d items=%d", first.ItemCount, len(first.Items))
	}
	kinds := make([]string, 0, len(first.Items))
	for index, item := range first.Items {
		if item.Index != index {
			t.Fatalf("index %d = %d", index, item.Index)
		}
		kinds = append(kinds, item.Kind)
	}
	want := []string{"heading", "paragraph", "list_item", "quote", "code_block"}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	if first.Items[0].Level == nil || *first.Items[0].Level != 1 {
		t.Fatalf("heading level = %v", first.Items[0].Level)
	}
	if !strings.Contains(first.Items[4].Text, "second line") {
		t.Fatalf("fenced blank line was not preserved: %q", first.Items[4].Text)
	}
}

func TestStructurePlainText(t *testing.T) {
	output, err := structureText("one\nline\n\nsecond", "plain_text")
	if err != nil {
		t.Fatalf("structure: %v", err)
	}
	if output.ItemCount != 2 || output.Items[0].Kind != "paragraph" {
		t.Fatalf("output = %+v", output)
	}
	if output.Items[0].Text != "one\nline" {
		t.Fatalf("text = %q", output.Items[0].Text)
	}
}

func TestStructureLongBlockSplitsAt8000(t *testing.T) {
	text := strings.Repeat("a", 8001)
	output, err := structureText(text, "plain_text")
	if err != nil {
		t.Fatalf("structure: %v", err)
	}
	if len(output.Items) != 2 {
		t.Fatalf("items = %d", len(output.Items))
	}
	if len([]rune(output.Items[0].Text)) != 8000 || len([]rune(output.Items[1].Text)) != 1 {
		t.Fatalf("chunk sizes = %d, %d", len([]rune(output.Items[0].Text)), len([]rune(output.Items[1].Text)))
	}
	if output.Items[1].Level != nil {
		t.Fatal("continuation chunk must not carry a level")
	}
}

func TestStructureTruncatesAbove500(t *testing.T) {
	var builder strings.Builder
	for index := 0; index < 501; index++ {
		if index > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString("paragraph")
	}
	output, err := structureText(builder.String(), "plain_text")
	if err != nil {
		t.Fatalf("structure: %v", err)
	}
	if len(output.Items) != 500 || !output.Truncated {
		t.Fatalf("items=%d truncated=%v", len(output.Items), output.Truncated)
	}
}

func TestStructureWhitespaceOnlyIsEmpty(t *testing.T) {
	output, err := structureText("   \n\t", "markdown")
	if err != nil {
		t.Fatalf("structure: %v", err)
	}
	if output.ItemCount != 0 || len(output.Items) != 0 || output.Truncated {
		t.Fatalf("output = %+v", output)
	}
}

func TestStructureRejectsUnsupportedFormat(t *testing.T) {
	if _, err := structureText("x", "sentences"); err == nil {
		t.Fatal("expected an error")
	}
}
