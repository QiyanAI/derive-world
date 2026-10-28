// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"fmt"
	"strings"
)

// storyCapability reads a world as an interactive book. Chapters are real
// things of one kind, choices are real links, and reading progress is a shared
// world variable. It is a reading surface over the same content, not a
// separate story document or a per-world page.
type storyCapability struct{}

func (storyCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.story", Kind: "presentation", Version: 1,
		Title:       Localized{En: "Living book", Zh: "可读的世界"},
		Description: Localized{En: "Read chapters stored as real things; choices follow real relations and update a shared reading position.", Zh: "把真实存在的篇章当作书来读；选择沿真实关系推进，并改变共享的阅读位置。"},
		Params: &ParamSchema{
			Type: "object",
			Properties: map[string]*ParamProperty{
				"title":             {Type: "string", MaxLength: 120, Title: Localized{En: "Title", Zh: "书名"}},
				"chapter_kind_id":   {Type: "string", Format: "kind", Title: Localized{En: "Chapter kind", Zh: "篇章种类"}},
				"progress_variable": {Type: "string", MaxLength: 40, Title: Localized{En: "Progress variable", Zh: "阅读位置变量"}},
				"jump_behaviors": {
					Type: "array", Title: Localized{En: "Chapter jump behaviors", Zh: "章节跳转行为"},
					Items: &ParamProperty{
						Type: "object",
						Properties: map[string]*ParamProperty{
							"entity_id":   {Type: "string", Format: "entity"},
							"behavior_id": {Type: "string", Format: "behavior"},
						},
						Required: []string{"entity_id", "behavior_id"},
					},
				},
				"restart_behavior_id": {Type: "string", Format: "behavior", Title: Localized{En: "Restart behavior", Zh: "重新开始行为"}},
			},
			Required: []string{"chapter_kind_id"},
		},
	}
}

type storyJump struct {
	EntityID   string `json:"entity_id"`
	BehaviorID string `json:"behavior_id"`
}

type storyProps struct {
	Title             string      `json:"title,omitempty"`
	ChapterKindID     string      `json:"chapter_kind_id"`
	ProgressVariable  string      `json:"progress_variable"`
	JumpBehaviors     []storyJump `json:"jump_behaviors,omitempty"`
	RestartBehaviorID string      `json:"restart_behavior_id,omitempty"`
}

func (storyCapability) Validate(raw json.RawMessage, ctx ValidateContext) (json.RawMessage, error) {
	var props storyProps
	if err := decodeStrict(raw, &props); err != nil {
		return nil, err
	}
	if _, ok := ctx.World.Kinds[props.ChapterKindID]; !ok {
		return nil, fmt.Errorf("%w: story.chapter_kind_id", ErrInvalid)
	}
	props.Title = strings.TrimSpace(props.Title)
	if runeLength(props.Title) > 120 {
		return nil, fmt.Errorf("%w: story.title", ErrInvalid)
	}
	if props.ProgressVariable == "" {
		props.ProgressVariable = "story.chapter"
	}
	if !validVariableName(props.ProgressVariable) {
		return nil, fmt.Errorf("%w: story.progress_variable", ErrInvalid)
	}
	if len(props.JumpBehaviors) > 40 {
		return nil, fmt.Errorf("%w: story.jump_behaviors", ErrInvalid)
	}
	seen := map[string]bool{}
	for index := range props.JumpBehaviors {
		jump := &props.JumpBehaviors[index]
		entity, ok := ctx.World.Entities[jump.EntityID]
		if !ok || entity.KindID != props.ChapterKindID || seen[jump.EntityID] {
			return nil, fmt.Errorf("%w: story.jump_behaviors/%d/entity_id", ErrInvalid, index)
		}
		seen[jump.EntityID] = true
		if !validLocalID(jump.BehaviorID) || !ctx.World.HasBehavior(jump.BehaviorID) {
			return nil, fmt.Errorf("%w: story.jump_behaviors/%d/behavior_id", ErrInvalid, index)
		}
	}
	if props.RestartBehaviorID != "" {
		if !validLocalID(props.RestartBehaviorID) || !ctx.World.HasBehavior(props.RestartBehaviorID) {
			return nil, fmt.Errorf("%w: story.restart_behavior_id", ErrInvalid)
		}
	}
	return json.Marshal(props)
}
