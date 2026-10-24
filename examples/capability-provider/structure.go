// SPDX-License-Identifier: Apache-2.0
package main

import (
	"errors"
	"regexp"
	"strings"
)

const (
	maxItems    = 500
	maxItemRune = 8000
)

type structureItem struct {
	Index int    `json:"index"`
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Level *int   `json:"level,omitempty"`
}

type structureOutput struct {
	Items     []structureItem `json:"items"`
	ItemCount int             `json:"item_count"`
	Truncated bool            `json:"truncated"`
	Format    string          `json:"format"`
}

var (
	headingPattern = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	listPattern    = regexp.MustCompile(`^([-*+]|[0-9]+\.)\s+(.*)$`)
	quotePattern   = regexp.MustCompile(`^>\s?(.*)$`)
)

// structureText implements the frozen D06 deterministic algorithm. It never
// reads the clock, never generates randomness, and never iterates a map.
func structureText(text, format string) (structureOutput, error) {
	if format != "plain_text" && format != "markdown" {
		return structureOutput{}, errors.New("unsupported format")
	}
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	normalized = strings.TrimSpace(normalized)
	blocks := splitBlocks(normalized, format == "markdown")
	items := make([]structureItem, 0)
	truncated := false
	for _, block := range blocks {
		kind, content, level, ok := classifyBlock(block, format)
		if !ok {
			continue
		}
		chunks := splitRunes(content, maxItemRune)
		for index, chunk := range chunks {
			if len(items) >= maxItems {
				truncated = true
				break
			}
			item := structureItem{Index: len(items), Kind: kind, Text: chunk}
			if index == 0 && level != nil {
				value := *level
				item.Level = &value
			}
			items = append(items, item)
		}
		if truncated {
			break
		}
	}
	return structureOutput{
		Items: items, ItemCount: len(items), Truncated: truncated, Format: format,
	}, nil
}

// splitBlocks separates on blank lines. In markdown, blank lines inside a
// fenced code block do not split the block.
func splitBlocks(text string, markdown bool) []string {
	lines := strings.Split(text, "\n")
	blocks := make([]string, 0)
	current := make([]string, 0)
	inFence := false
	flush := func() {
		if len(current) == 0 {
			return
		}
		block := strings.TrimSpace(strings.Join(current, "\n"))
		if block != "" {
			blocks = append(blocks, block)
		}
		current = current[:0]
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if markdown && strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			current = append(current, line)
			continue
		}
		if !inFence && trimmed == "" {
			flush()
			continue
		}
		current = append(current, line)
	}
	flush()
	return blocks
}

func classifyBlock(block, format string) (string, string, *int, bool) {
	trimmed := strings.TrimSpace(block)
	if trimmed == "" {
		return "", "", nil, false
	}
	if format == "plain_text" {
		return "paragraph", trimmed, nil, true
	}
	lines := strings.Split(trimmed, "\n")
	first := strings.TrimSpace(lines[0])
	if strings.HasPrefix(first, "```") {
		inner := lines[1:]
		if len(inner) > 0 && strings.HasPrefix(strings.TrimSpace(inner[len(inner)-1]), "```") {
			inner = inner[:len(inner)-1]
		}
		content := strings.TrimSpace(strings.Join(inner, "\n"))
		if content == "" {
			return "", "", nil, false
		}
		return "code_block", content, nil, true
	}
	if match := headingPattern.FindStringSubmatch(first); match != nil {
		level := len(match[1])
		rest := append([]string{match[2]}, lines[1:]...)
		content := strings.TrimSpace(strings.Join(rest, "\n"))
		if content == "" {
			return "", "", nil, false
		}
		return "heading", content, &level, true
	}
	if match := listPattern.FindStringSubmatch(first); match != nil {
		rest := append([]string{match[2]}, lines[1:]...)
		content := strings.TrimSpace(strings.Join(rest, "\n"))
		if content == "" {
			return "", "", nil, false
		}
		return "list_item", content, nil, true
	}
	if match := quotePattern.FindStringSubmatch(first); match != nil {
		rest := append([]string{match[1]}, lines[1:]...)
		content := strings.TrimSpace(strings.Join(rest, "\n"))
		if content == "" {
			return "", "", nil, false
		}
		return "quote", content, nil, true
	}
	return "paragraph", trimmed, nil, true
}

func splitRunes(text string, limit int) []string {
	runes := []rune(text)
	if len(runes) <= limit {
		return []string{text}
	}
	chunks := make([]string, 0, (len(runes)+limit-1)/limit)
	for start := 0; start < len(runes); start += limit {
		end := start + limit
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
	}
	return chunks
}
