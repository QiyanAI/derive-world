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
	case strings.Contains(req.System, `"object_type":"open_world"`) && reimagineIntent(last):
		reply = fixtureReimagineReply(containsCJK(last), last)
	case strings.Contains(req.System, `"object_type":"open_world"`) && objectChangeIntent(last):
		reply = fixtureOpenWorldObjectProposal()
	case strings.Contains(req.System, `"purpose":`) && strings.Contains(req.System, `"elements":`) &&
		objectChangeIntent(last):
		reply = fixtureLivingSceneObjectProposal()
	default:
		reply = f.responses[last]
	}
	if reply == "" && strings.Contains(req.System, "<world_creation_mode>") && worldCreationIntent(last) {
		reply = fixtureWorldCreateReply(containsCJK(last), last)
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

func reimagineIntent(message string) bool {
	lower := strings.ToLower(message)
	for _, marker := range []string{
		"walk", "move around", "play", "game", "animation", "instrument",
		"music", "story", "novel", "workbench", "form", "table",
		"走动", "走", "玩", "游戏", "动画", "乐器", "弹", "音乐",
		"小说", "故事", "工作台", "表格", "另一种", "形式",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// fixtureReimagineReply is a deterministic demonstration of the same
// world.reimagine path the real model uses. It never claims to be real AI.
func fixtureReimagineReply(chinese bool, message string) string {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "乐器") || strings.Contains(lower, "弹") ||
		strings.Contains(lower, "instrument") || strings.Contains(lower, "music") {
		if chinese {
			return `{"reply":"我把这个世界改成一架可以弹奏的星图乐器：点击星点会亮起并发出声音，音符数量会真实保存。这是 Fixture 演示。","proposed_actions":[{"capability_id":"world.reimagine","input":{"summary":"把世界变成可弹奏的星图乐器","brief":{"experience":{"surfaces":["custom"],"primary":"custom","custom":{"title":"星图乐器","width":1200,"height":800,"background":{"mode":"gradient","hue":240,"hue2":265,"lightness":8,"accent_hue":42},"vars":{"notes":0,"pad_a":false,"pad_b":false},"nodes":[{"id":"pad_a","type":"dot","x":420,"y":420,"r":56,"hue":42,"bindings":[{"prop":"opacity","var":"pad_a","in_min":0,"in_max":1,"out_min":0.35,"out_max":1}],"on_click":[{"type":"toggle_var","name":"pad_a"},{"type":"sound","freq":440,"duration":0.3,"wave":"sine"},{"type":"add_var","name":"notes","value":1}]},{"id":"pad_b","type":"dot","x":720,"y":420,"r":56,"hue":200,"on_click":[{"type":"toggle_var","name":"pad_b"},{"type":"sound","freq":660,"duration":0.3,"wave":"triangle"}]},{"id":"readout","type":"text","x":420,"y":620,"text":"notes","size":28,"bind_text":{"var":"notes"}}],"timeline":{"duration":8,"loop":true,"tracks":[{"node_id":"pad_a","property":"r","keys":[{"t":0,"v":56},{"t":4,"v":64},{"t":8,"v":56}]}]}}}}}}]}`
		}
		return `{"reply":"I turned this world into a playable star instrument: clicking a star lights it and plays a tone, and the note count is really saved. This is fixture demo output.","proposed_actions":[{"capability_id":"world.reimagine","input":{"summary":"Turn the world into a playable star instrument","brief":{"experience":{"surfaces":["custom"],"primary":"custom","custom":{"title":"Star instrument","width":1200,"height":800,"background":{"mode":"gradient","hue":240,"hue2":265,"lightness":8,"accent_hue":42},"vars":{"notes":0,"pad_a":false,"pad_b":false},"nodes":[{"id":"pad_a","type":"dot","x":420,"y":420,"r":56,"hue":42,"bindings":[{"prop":"opacity","var":"pad_a","in_min":0,"in_max":1,"out_min":0.35,"out_max":1}],"on_click":[{"type":"toggle_var","name":"pad_a"},{"type":"sound","freq":440,"duration":0.3,"wave":"sine"},{"type":"add_var","name":"notes","value":1}]},{"id":"pad_b","type":"dot","x":720,"y":420,"r":56,"hue":200,"on_click":[{"type":"toggle_var","name":"pad_b"},{"type":"sound","freq":660,"duration":0.3,"wave":"triangle"}]},{"id":"readout","type":"text","x":420,"y":620,"text":"notes","size":28,"bind_text":{"var":"notes"}}],"timeline":{"duration":8,"loop":true,"tracks":[{"node_id":"pad_a","property":"r","keys":[{"t":0,"v":56},{"t":4,"v":64},{"t":8,"v":56}]}]}}}}}}]}`
	}
	if chinese {
		return `{"reply":"我给这个世界加上可以走动和互动的表面；靠近一个存在后再点击它，世界会真实回应。这是 Fixture 演示。","proposed_actions":[{"capability_id":"world.reimagine","input":{"summary":"让这个世界可以走动","brief":{"experience":{"surfaces":["live"],"primary":"live","live":{"avatar":0,"speed":1.8,"particles":{"kind":"motes","count":24}}}}}}]}`
	}
	return `{"reply":"I added a walkable, interactive surface; approach a thing and click it to make the world really respond. This is fixture demo output.","proposed_actions":[{"capability_id":"world.reimagine","input":{"summary":"Make this world walkable","brief":{"experience":{"surfaces":["live"],"primary":"live","live":{"avatar":0,"speed":1.8,"particles":{"kind":"motes","count":24}}}}}}]}`
}

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
func fixtureWorldCreateReply(chinese bool, message string) string {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "adventure") || strings.Contains(lower, "legend") ||
		strings.Contains(lower, "冒险") || strings.Contains(lower, "传奇"):
		if chinese {
			return `{"reply":"这是一个可以游玩、也可以阅读的森林冒险：先拾起第一束光，再翻开它的故事。这是 Fixture 演示。","proposed_actions":[{"capability_id":"world.create","input":{"title":"森林冒险","brief":{"premise":"一场可以游玩、也可以阅读的森林冒险。","atmosphere":"安静的绿色森林，漂浮的微光","kinds":[{"name":"旅人","form":"M 0 -22 A 22 22 0 1 1 0 22 A 22 22 0 1 1 0 -22 Z","hue":180},{"name":"光点","form":"M 0 -16 L 11 0 L 0 16 L -11 0 Z","hue":48,"motion":"pulse"},{"name":"古树","form":"M 0 -30 L 12 22 L -12 22 Z","hue":120,"solid":true}],"things":[{"kind":0,"name":"旅人","text":"她提着一盏空灯笼。","x":0.5,"y":0.5},{"kind":1,"name":"第一束光","text":"它记得黎明。","x":0.56,"y":0.5},{"kind":2,"name":"最老的树","text":"它站在路的中央。"}],"experience":{"surfaces":["live","story"],"primary":"live","live":{"avatar":0,"goal":{"type":"collect","kind":1,"count":1,"label":"拾起第一束光"},"particles":{"kind":"motes","count":18}},"story":{"title":"森林的第一章","chapters":[{"title":"灯笼","text":"旅人走进森林。灯笼里还没有光。","choices":[{"label":"继续走","to":1}]},{"title":"第一束光","text":"一束光落在她脚边，像一句没说完的话。","choices":[{"label":"回到开头","to":0}]}]}}}}}]}`
		}
		return `{"reply":"Here is a forest adventure you can play and also read: gather the first light, then open its story. This is fixture demo output.","proposed_actions":[{"capability_id":"world.create","input":{"title":"Forest adventure","brief":{"premise":"A forest adventure you can play and also read.","atmosphere":"quiet green woods with drifting motes","kinds":[{"name":"Traveler","form":"M 0 -22 A 22 22 0 1 1 0 22 A 22 22 0 1 1 0 -22 Z","hue":180},{"name":"Light","form":"M 0 -16 L 11 0 L 0 16 L -11 0 Z","hue":48,"motion":"pulse"},{"name":"Old tree","form":"M 0 -30 L 12 22 L -12 22 Z","hue":120,"solid":true}],"things":[{"kind":0,"name":"Traveler","text":"She carries an empty lantern.","x":0.5,"y":0.5},{"kind":1,"name":"First light","text":"It remembers dawn.","x":0.56,"y":0.5},{"kind":2,"name":"The oldest tree","text":"It stands in the middle of the path."}],"experience":{"surfaces":["live","story"],"primary":"live","live":{"avatar":0,"goal":{"type":"collect","kind":1,"count":1,"label":"Gather the first light"},"particles":{"kind":"motes","count":18}},"story":{"title":"The first chapter of the forest","chapters":[{"title":"The lantern","text":"The traveler enters the forest. The lantern holds no light yet.","choices":[{"label":"Walk on","to":1}]},{"title":"The first light","text":"A light falls at her feet, like a sentence left unfinished.","choices":[{"label":"Return to the beginning","to":0}]}]}}}}}]}`
	case strings.Contains(lower, "story") || strings.Contains(lower, "novel") ||
		strings.Contains(lower, "library") || strings.Contains(lower, "小说") ||
		strings.Contains(lower, "故事") || strings.Contains(lower, "图书馆"):
		if chinese {
			return `{"reply":"这是一个会真正让你读下去的互动故事：翻章、做选择，阅读位置会保存。这是 Fixture 演示。","proposed_actions":[{"capability_id":"world.create","input":{"title":"潮汐图书馆","brief":{"premise":"一座由潮汐决定哪本书会打开的图书馆。","experience":{"primary":"story","story":{"title":"潮汐图书馆","chapters":[{"title":"第一道门","text":"潮水漫进来。一本绿色的书自己漂到你的手里。书脊上有一行字：请把我读成一场雨。","choices":[{"label":"打开它","to":1},{"label":"把它放回去","to":2}]},{"title":"书记得的海","text":"书页是湿的，带着一片已经不存在的大海。每翻一页，远处的钟就响一次。","choices":[{"label":"继续读下去","to":2}]},{"title":"图书管理员","text":"她说，每一本书都是一次选择留下来的潮汐。你合上书，潮水退去，但图书馆记住了你。","choices":[]}]}}}}}]}`
		}
		return `{"reply":"Here is an interactive story you can really read: turn chapters, make choices, and your place is saved. This is fixture demo output.","proposed_actions":[{"capability_id":"world.create","input":{"title":"The Tide Library","brief":{"premise":"A library where the tide decides which book opens.","experience":{"primary":"story","story":{"title":"The Tide Library","chapters":[{"title":"The first door","text":"The tide comes in. A green book floats into your hands. On its spine: read me as a rainstorm.","choices":[{"label":"Open it","to":1},{"label":"Put it back","to":2}]},{"title":"The sea the book remembers","text":"The pages are wet with a sea that no longer exists. Every page turn rings a distant bell.","choices":[{"label":"Read on","to":2}]},{"title":"The librarian","text":"She says every book is a tide that chose to stay. You close it; the water withdraws, but the library remembers you.","choices":[]}]}}}}}]}`
	case strings.Contains(lower, "workbench") || strings.Contains(lower, "workspace") ||
		strings.Contains(lower, "table") || strings.Contains(lower, "work plan") ||
		strings.Contains(lower, "工作台") || strings.Contains(lower, "表格"):
		if chinese {
			return `{"reply":"这是一个真正可用的工作台：你可以编辑条目、添加新行、查看汇总，刷新后仍会保存。这是 Fixture 演示。","proposed_actions":[{"capability_id":"world.create","input":{"title":"写作计划","brief":{"premise":"一个安静地管理写作计划的工作台。","kinds":[{"name":"稿件","hue":210,"fields":[{"label":"标题","type":"text","initial":"未命名"},{"label":"字数","type":"number","initial":0},{"label":"备注","type":"text"}]}],"things":[{"kind":0,"name":"开篇","text":"第一稿。","values":{"标题":"开篇","字数":1200}},{"kind":0,"name":"第二章","text":"还在生长。","values":{"标题":"第二章","字数":400}}],"experience":{"primary":"workspace","workspace":{"title":"写作计划","kind":0,"columns":["标题","字数"]}}}}}]}`
		}
		return `{"reply":"Here is a workbench you can really use: edit rows, add new ones, read live summaries, and everything survives refresh. This is fixture demo output.","proposed_actions":[{"capability_id":"world.create","input":{"title":"Writing plan","brief":{"premise":"A quiet workbench for a writing plan.","kinds":[{"name":"Draft","hue":210,"fields":[{"label":"Title","type":"text","initial":"Untitled"},{"label":"Words","type":"number","initial":0},{"label":"Notes","type":"text"}]}],"things":[{"kind":0,"name":"Opening chapter","text":"A first draft.","values":{"Title":"Opening chapter","Words":1200}},{"kind":0,"name":"Second chapter","text":"Still growing.","values":{"Title":"Second chapter","Words":400}}],"experience":{"primary":"workspace","workspace":{"title":"Writing plan","kind":0,"columns":["Title","Words"]}}}}}]}`
	case strings.Contains(lower, "animation") || strings.Contains(lower, "animated") ||
		strings.Contains(lower, "nebula") || strings.Contains(lower, "动画") ||
		strings.Contains(lower, "星云"):
		if chinese {
			return `{"reply":"这是一幅会真正播放的动态画面：星云呼吸、尘埃漂移，你可以播放、暂停和拖动时间。这是 Fixture 演示。","proposed_actions":[{"capability_id":"world.create","input":{"title":"呼吸的星云","brief":{"premise":"一片会呼吸、会漂移的星云。","atmosphere":"星尘与缓慢的呼吸","kinds":[{"name":"星云","form":"M 0 -30 Q 30 -10 0 30 Q -30 -10 0 -30 Z","hue":280,"motion":"breathe"},{"name":"尘埃","form":"M 0 -10 L 10 0 L 0 10 L -10 0 Z","hue":40,"motion":"flicker"}],"things":[{"kind":0,"name":"沉睡的星云","text":"它在睡梦中转身。"},{"kind":1,"name":"一粒尘埃","text":"它接住了一颗远处的星。"},{"kind":1,"name":"另一粒尘埃","text":"它向侧面漂去。"}],"experience":{"primary":"live","live":{"title":"呼吸的星云","particles":{"kind":"stars","count":50}}}}}}]}`
		}
		return `{"reply":"Here is a genuinely playing animated world: the nebula breathes, dust drifts, and you can play, pause and scrub time. This is fixture demo output.","proposed_actions":[{"capability_id":"world.create","input":{"title":"Breathing Nebula","brief":{"premise":"A slow nebula breathing in the dark.","atmosphere":"stars and drifting dust","kinds":[{"name":"Nebula","form":"M 0 -30 Q 30 -10 0 30 Q -30 -10 0 -30 Z","hue":280,"motion":"breathe"},{"name":"Dust","form":"M 0 -10 L 10 0 L 0 10 L -10 0 Z","hue":40,"motion":"flicker"}],"things":[{"kind":0,"name":"The sleeping nebula","text":"It turns in its sleep."},{"kind":1,"name":"A grain of dust","text":"It catches a distant star."},{"kind":1,"name":"Another grain","text":"It drifts sideways."}],"experience":{"primary":"live","live":{"title":"Breathing Nebula","particles":{"kind":"stars","count":50}}}}}}]}`
	case strings.Contains(lower, "instrument") || strings.Contains(lower, "music") ||
		strings.Contains(lower, "乐器") || strings.Contains(lower, "音乐") ||
		strings.Contains(lower, "弹"):
		if chinese {
			return `{"reply":"这是一架可以真正弹奏的星图乐器：点击星点会亮起并发出声音，音符会真实保存。这是 Fixture 演示。","proposed_actions":[{"capability_id":"world.create","input":{"title":"星图乐器","brief":{"premise":"一架可以弹奏的星图。","experience":{"primary":"custom","custom":{"title":"星图乐器","width":1200,"height":800,"background":{"mode":"gradient","hue":240,"hue2":265,"lightness":8,"accent_hue":42},"vars":{"notes":0,"pad_a":false,"pad_b":false},"nodes":[{"id":"pad_a","type":"dot","x":420,"y":420,"r":56,"hue":42,"bindings":[{"prop":"opacity","var":"pad_a","in_min":0,"in_max":1,"out_min":0.35,"out_max":1}],"on_click":[{"type":"toggle_var","name":"pad_a"},{"type":"sound","freq":440,"duration":0.3,"wave":"sine"},{"type":"add_var","name":"notes","value":1}]},{"id":"pad_b","type":"dot","x":720,"y":420,"r":56,"hue":200,"on_click":[{"type":"toggle_var","name":"pad_b"},{"type":"sound","freq":660,"duration":0.3,"wave":"triangle"}]},{"id":"readout","type":"text","x":420,"y":620,"text":"notes","size":28,"bind_text":{"var":"notes"}}],"timeline":{"duration":8,"loop":true,"tracks":[{"node_id":"pad_a","property":"r","keys":[{"t":0,"v":56},{"t":4,"v":64},{"t":8,"v":56}]}]}}}}}}]}`
		}
		return `{"reply":"Here is a playable star instrument: clicking a star lights it and plays a tone, and notes are really saved. This is fixture demo output.","proposed_actions":[{"capability_id":"world.create","input":{"title":"Star instrument","brief":{"premise":"A star map you can play with your hands.","experience":{"primary":"custom","custom":{"title":"Star instrument","width":1200,"height":800,"background":{"mode":"gradient","hue":240,"hue2":265,"lightness":8,"accent_hue":42},"vars":{"notes":0,"pad_a":false,"pad_b":false},"nodes":[{"id":"pad_a","type":"dot","x":420,"y":420,"r":56,"hue":42,"bindings":[{"prop":"opacity","var":"pad_a","in_min":0,"in_max":1,"out_min":0.35,"out_max":1}],"on_click":[{"type":"toggle_var","name":"pad_a"},{"type":"sound","freq":440,"duration":0.3,"wave":"sine"},{"type":"add_var","name":"notes","value":1}]},{"id":"pad_b","type":"dot","x":720,"y":420,"r":56,"hue":200,"on_click":[{"type":"toggle_var","name":"pad_b"},{"type":"sound","freq":660,"duration":0.3,"wave":"triangle"}]},{"id":"readout","type":"text","x":420,"y":620,"text":"notes","size":28,"bind_text":{"var":"notes"}}],"timeline":{"duration":8,"loop":true,"tracks":[{"node_id":"pad_a","property":"r","keys":[{"t":0,"v":56},{"t":4,"v":64},{"t":8,"v":56}]}]}}}}}}]}`
	case strings.Contains(lower, "forest") || strings.Contains(lower, "gather") ||
		strings.Contains(lower, "star-dust") || strings.Contains(lower, "星尘") ||
		strings.Contains(lower, "森林"):
		if chinese {
			return `{"reply":"这是一个可以真正游玩的星尘森林：你控制一位旅人，靠近并点击三束散落的光，把它们收进灯笼。这是 Fixture 演示。","proposed_actions":[{"capability_id":"world.create","input":{"title":"星尘森林","brief":{"premise":"一座会记住每一束被拾起的光的星尘森林。","atmosphere":"安静的绿色森林，漂浮的微光","kinds":[{"name":"旅人","form":"M 0 -22 A 22 22 0 1 1 0 22 A 22 22 0 1 1 0 -22 Z","hue":180},{"name":"光点","form":"M 0 -16 L 11 0 L 0 16 L -11 0 Z","hue":48,"motion":"pulse"},{"name":"古树","form":"M 0 -30 L 12 22 L -12 22 Z","hue":120,"solid":true}],"things":[{"kind":0,"name":"旅人","text":"她提着一盏空灯笼。","x":0.5,"y":0.5},{"kind":1,"name":"第一束光","text":"它记得黎明。","x":0.56,"y":0.5},{"kind":1,"name":"第二束光","text":"它记得一场雨。"},{"kind":1,"name":"第三束光","text":"它记得一个名字。"},{"kind":2,"name":"最老的树","text":"它站在路的中央。"}],"relations":[{"from":0,"to":1,"label":"看见"}],"experience":{"primary":"live","live":{"avatar":0,"goal":{"type":"collect","kind":1,"count":3,"label":"收集三束光"},"particles":{"kind":"motes","count":24}}}}}}]}`
		}
		return `{"reply":"Here is a star-dust forest you can actually play: you control a traveler, approach three fallen lights and click them into the lantern. This is fixture demo output.","proposed_actions":[{"capability_id":"world.create","input":{"title":"Star-dust forest","brief":{"premise":"A forest that remembers every light you pick up.","atmosphere":"quiet green woods with drifting motes","kinds":[{"name":"Traveler","form":"M 0 -22 A 22 22 0 1 1 0 22 A 22 22 0 1 1 0 -22 Z","hue":180},{"name":"Light","form":"M 0 -16 L 11 0 L 0 16 L -11 0 Z","hue":48,"motion":"pulse"},{"name":"Old tree","form":"M 0 -30 L 12 22 L -12 22 Z","hue":120,"solid":true}],"things":[{"kind":0,"name":"Traveler","text":"She carries an empty lantern.","x":0.5,"y":0.5},{"kind":1,"name":"First light","text":"It remembers dawn.","x":0.56,"y":0.5},{"kind":1,"name":"Second light","text":"It remembers rain."},{"kind":1,"name":"Third light","text":"It remembers a name."},{"kind":2,"name":"The oldest tree","text":"It stands in the middle of the path."}],"relations":[{"from":0,"to":1,"label":"sees"}],"experience":{"primary":"live","live":{"avatar":0,"goal":{"type":"collect","kind":1,"count":3,"label":"Gather the three lights"},"particles":{"kind":"motes","count":24}}}}}}]}`
	default:
		if chinese {
			return `{"reply":"这是一个可以开始生长的城市方案：居民的心情会改变广场的光，广场的回应又会改变居民。它还只是提案，你批准之前不会创建任何世界。","proposed_actions":[{"capability_id":"world.create","input":{"title":"一座随选择改变的城市","content":{"premise":"城市会记住每一个人的选择：居民改变广场，广场再回应居民。","kinds":[{"name":"居民","hue":168,"fields":[{"key":"mood","label":"心情","type":"text"}]},{"name":"地点","hue":214,"fields":[{"key":"light","label":"光","type":"number"}]}],"nodes":[{"kind_index":0,"name":"第一位居民","text":"他先听见了城市的声音。","x":460,"y":330},{"kind_index":1,"name":"会倾听的广场","text":"每一次选择都会在这里留下光。","x":760,"y":470},{"kind_index":1,"name":"回声长廊","text":"广场的回应沿着长廊继续传播。","x":1000,"y":300}],"links":[{"from_index":0,"to_index":1,"label":"常去"},{"from_index":1,"to_index":2,"label":"回应"}],"rules":[{"trigger_index":0,"effects":[{"node_index":1,"field":"light","op":"add","value":1}]},{"trigger_index":1,"effects":[{"node_index":0,"field":"mood","op":"set","value":"明亮"}]}],"views":["stage","cards","timeline"],"tags":["fixture"]}}}]}`
		}
		return `{"reply":"Here is a city that can begin growing: a resident's mood changes the square's light, and the square answers the resident. This is only a proposal. Nothing is created until you approve it.","proposed_actions":[{"capability_id":"world.create","input":{"title":"A city that changes with every choice","content":{"premise":"The city remembers every choice: residents change the square, and the square answers them.","kinds":[{"name":"Resident","hue":168,"fields":[{"key":"mood","label":"Mood","type":"text"}]},{"name":"Place","hue":214,"fields":[{"key":"light","label":"Light","type":"number"}]}],"nodes":[{"kind_index":0,"name":"The first resident","text":"They heard the city first.","x":460,"y":330},{"kind_index":1,"name":"A square that listens","text":"Every choice leaves light here.","x":760,"y":470},{"kind_index":1,"name":"Echo corridor","text":"The square's answer travels on.","x":1000,"y":300}],"links":[{"from_index":0,"to_index":1,"label":"visits"},{"from_index":1,"to_index":2,"label":"answers"}],"rules":[{"trigger_index":0,"effects":[{"node_index":1,"field":"light","op":"add","value":1}]},{"trigger_index":1,"effects":[{"node_index":0,"field":"mood","op":"set","value":"bright"}]}],"views":["stage","cards","timeline"],"tags":["fixture"]}}}]}`
	}
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
