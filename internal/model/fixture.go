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
	last := lastUserMessage(req.Messages)
	var reply string
	switch {
	case strings.Contains(req.System, `"object_type":"open_world"`) && objectChangeIntent(last):
		reply = fixtureOpenWorldObjectProposal()
	case strings.Contains(req.System, `"purpose":`) && strings.Contains(req.System, `"elements":`) &&
		objectChangeIntent(last):
		reply = fixtureLivingSceneObjectProposal()
	default:
		reply = f.responses[last]
	}
	if reply == "" && strings.Contains(req.System, "<world_creation_mode>") && worldCreationIntent(last) {
		reply = fixtureWorldCreateReply(containsCJK(last))
	}
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

// worldCreationIntent recognizes the small, explicit vocabulary the fixture
// adapter can answer deterministically. It never guesses from arbitrary text.
func worldCreationIntent(message string) bool {
	lower := strings.ToLower(message)
	for _, marker := range []string{
		"创造", "创建", "建一个世", "做一个世", "第一个世", "世界",
		"create a world", "create my first world", "build a world", "make a world",
		"design a world", "my first world", "world that",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func objectChangeIntent(message string) bool {
	lower := strings.ToLower(message)
	for _, marker := range []string{
		"propose", "change", "develop", "improve", "add a", "add an",
		"提议", "改变", "改进", "添加", "发展", "继续",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// fixtureOpenWorldObjectProposal is a valid object.propose_change payload for
// an attached open_world. set_premise needs no invented identifiers, so the
// fixture never fabricates a kind or node that does not exist.
func fixtureOpenWorldObjectProposal() string {
	return `{"reply":"Fixture proposal: develop this world by sharpening its premise. Nothing changes until you apply the proposal.","proposed_actions":[{"capability_id":"object.propose_change","input":{"action":"set_premise","input":{"premise":"This world grows from one real change at a time. Each touch leaves a visible trace."}}}]}`
}

// fixtureLivingSceneObjectProposal is a valid object.propose_change payload for
// an attached living_scene. The existing generic fixture used set_body, which
// is not part of the living_scene whitelist.
func fixtureLivingSceneObjectProposal() string {
	return `{"reply":"Fixture proposal: add one new element to this living scene. Nothing changes until you apply the proposal.","proposed_actions":[{"capability_id":"object.propose_change","input":{"action":"add_element","input":{"kind":"idea","text":"A new direction that came from the real conversation."}}}]}`
}

func containsCJK(value string) bool {
	for _, r := range value {
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}

// fixtureWorldCreateReply is a complete, valid open_world proposal. It exists
// so the zero-world Personal AI journey can be verified end-to-end without a
// real provider. It is still ordinary model output: the server validates it
// and the human must confirm it before any object is written.
func fixtureWorldCreateReply(chinese bool) string {
	if chinese {
		return `{"reply":"这是一个可以开始生长的城市方案：居民的心情会改变广场的光，广场的回应又会改变居民。它还只是提案，你批准之前不会创建任何世界。","proposed_actions":[{"capability_id":"world.create","input":{"title":"一座随选择改变的城市","content":{"premise":"城市会记住每一个人的选择：居民改变广场，广场再回应居民。","kinds":[{"name":"居民","hue":168,"fields":[{"key":"mood","label":"心情","type":"text"}]},{"name":"地点","hue":214,"fields":[{"key":"light","label":"光","type":"number"}]}],"nodes":[{"kind_index":0,"name":"第一位居民","text":"他先听见了城市的声音。","x":460,"y":330},{"kind_index":1,"name":"会倾听的广场","text":"每一次选择都会在这里留下光。","x":760,"y":470},{"kind_index":1,"name":"回声长廊","text":"广场的回应沿着长廊继续传播。","x":1000,"y":300}],"links":[{"from_index":0,"to_index":1,"label":"常去"},{"from_index":1,"to_index":2,"label":"回应"}],"rules":[{"trigger_index":0,"effects":[{"node_index":1,"field":"light","op":"add","value":1}]},{"trigger_index":1,"effects":[{"node_index":0,"field":"mood","op":"set","value":"明亮"}]}],"views":["stage","cards","timeline"],"tags":["fixture"]}}}]}`
	}
	return `{"reply":"Here is a city that can begin growing: a resident's mood changes the square's light, and the square answers the resident. This is only a proposal. Nothing is created until you approve it.","proposed_actions":[{"capability_id":"world.create","input":{"title":"A city that changes with every choice","content":{"premise":"The city remembers every choice: residents change the square, and the square answers them.","kinds":[{"name":"Resident","hue":168,"fields":[{"key":"mood","label":"Mood","type":"text"}]},{"name":"Place","hue":214,"fields":[{"key":"light","label":"Light","type":"number"}]}],"nodes":[{"kind_index":0,"name":"The first resident","text":"They heard the city first.","x":460,"y":330},{"kind_index":1,"name":"A square that listens","text":"Every choice leaves light here.","x":760,"y":470},{"kind_index":1,"name":"Echo corridor","text":"The square's answer travels on.","x":1000,"y":300}],"links":[{"from_index":0,"to_index":1,"label":"visits"},{"from_index":1,"to_index":2,"label":"answers"}],"rules":[{"trigger_index":0,"effects":[{"node_index":1,"field":"light","op":"add","value":1}]},{"trigger_index":1,"effects":[{"node_index":0,"field":"mood","op":"set","value":"bright"}]}],"views":["stage","cards","timeline"],"tags":["fixture"]}}}]}`
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
