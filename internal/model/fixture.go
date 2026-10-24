// SPDX-License-Identifier: Apache-2.0
package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxInputBytes  = 64 * 1024
	maxOutputBytes = 8 * 1024
)

// Fixture is a deterministic adapter. It never calls a model provider.
type Fixture struct {
	responses map[string]string
}

type fixtureFile struct {
	Responses []fixtureEntry `json:"responses"`
}

type fixtureEntry struct {
	Match string `json:"match"`
	Reply string `json:"reply"`
}

// NewFixture loads optional JSON fixtures from dir. Missing files are not an
// error: the adapter falls back to deterministic built-in replies.
func NewFixture(dir string) *Fixture {
	fixture := &Fixture{responses: map[string]string{
		"hello":  "Fixture reply: hello. This is deterministic demo output, not real AI.",
		"derive": "Fixture reply: derive is a demo response produced in fixture mode. This is deterministic output, not real AI.",
	}}
	if dir == "" {
		return fixture
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fixture
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var parsed fixtureFile
		if err := json.Unmarshal(content, &parsed); err != nil {
			continue
		}
		for _, item := range parsed.Responses {
			if item.Match != "" && item.Reply != "" {
				fixture.responses[item.Match] = item.Reply
			}
		}
	}
	return fixture
}

// Mode reports the deterministic adapter mode.
func (f *Fixture) Mode() string {
	return "fixture"
}

// Complete returns a deterministic reply. It never reports a real model call.
func (f *Fixture) Complete(_ context.Context, req Request) (Response, error) {
	inputSize := len(req.System)
	for _, message := range req.Messages {
		inputSize += len(message.Content)
	}
	if inputSize > maxInputBytes {
		return Response{}, &Error{Code: CodePayloadTooLarge, Message: "model input exceeds 64 KiB"}
	}

	modelName := req.Model
	if modelName == "" {
		modelName = "fixture-model"
	}
	reply := f.responses[lastUserMessage(req.Messages)]
	if reply == "" {
		reply = "Fixture reply: no scripted response. This is deterministic demo output, not real AI."
	}
	if len(reply) > maxOutputBytes {
		reply = reply[:maxOutputBytes]
	}
	return Response{
		Text:  reply,
		Model: modelName,
		Usage: Usage{
			InputTokens:  approxTokens(inputSize),
			OutputTokens: approxTokens(len(reply)),
		},
	}, nil
}

func lastUserMessage(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

func approxTokens(bytes int) int {
	if bytes <= 0 {
		return 0
	}
	return (bytes + 3) / 4
}

// fingerprint is retained for deterministic fixtures that need an input key.
func fingerprint(req Request) string {
	hash := sha256.New()
	hash.Write([]byte(req.System))
	for _, message := range req.Messages {
		hash.Write([]byte("\n" + message.Role + ":" + message.Content))
	}
	return hex.EncodeToString(hash.Sum(nil))[:16]
}
