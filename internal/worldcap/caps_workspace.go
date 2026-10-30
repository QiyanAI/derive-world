// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"fmt"
	"strings"
)

// workspaceCapability is a real, operable table over one kind of thing. Edits
// go through the normal versioned field actions; row and global actions run
// behaviors; core.create can add a row. It is the non-entertainment surface of
// the same world model, not a separate product.
type workspaceCapability struct{}

func (workspaceCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.workspace", Kind: "presentation", Version: 1,
		Title:       Localized{En: "Workspace", Zh: "工作台"},
		Description: Localized{En: "Edit real things in a table, run row actions, add rows and read live summaries.", Zh: "在表格里编辑真实存在、运行行操作、添加行并查看实时汇总。"},
		Params: &ParamSchema{
			Type: "object",
			Properties: map[string]*ParamProperty{
				"title":   {Type: "string", MaxLength: 120, Title: Localized{En: "Title", Zh: "标题"}},
				"kind_id": {Type: "string", Format: "kind", Title: Localized{En: "Rows", Zh: "行种类"}},
				"columns": {
					Type: "array", Title: Localized{En: "Columns", Zh: "列"},
					Items: &ParamProperty{
						Type: "object",
						Properties: map[string]*ParamProperty{
							"field": {Type: "string", Format: "field"},
							"label": {Type: "string", MaxLength: 80},
							"type":  {Type: "string"},
						},
						Required: []string{"field", "label"},
					},
				},
				"actions": {
					Type: "array", Title: Localized{En: "Actions", Zh: "操作"},
					Items: &ParamProperty{
						Type: "object",
						Properties: map[string]*ParamProperty{
							"label":       {Type: "string", MaxLength: 80},
							"behavior_id": {Type: "string", Format: "behavior"},
							"scope":       {Type: "string"},
						},
						Required: []string{"label", "behavior_id"},
					},
				},
				"summary": {
					Type: "array", Title: Localized{En: "Summary", Zh: "汇总"},
					Items: &ParamProperty{
						Type: "object",
						Properties: map[string]*ParamProperty{
							"type":  {Type: "string"},
							"field": {Type: "string"},
							"label": {Type: "string", MaxLength: 80},
						},
					},
				},
			},
			Required: []string{"kind_id", "columns"},
		},
	}
}

type workspaceColumn struct {
	Field string `json:"field"`
	Label string `json:"label"`
	Type  string `json:"type,omitempty"`
}

type workspaceAction struct {
	Label      string `json:"label"`
	BehaviorID string `json:"behavior_id"`
	Scope      string `json:"scope,omitempty"`
}

type workspaceSummary struct {
	Type  string `json:"type"`
	Field string `json:"field,omitempty"`
	Label string `json:"label"`
}

type workspaceProps struct {
	Title   string             `json:"title,omitempty"`
	KindID  string             `json:"kind_id"`
	Columns []workspaceColumn  `json:"columns"`
	Actions []workspaceAction  `json:"actions,omitempty"`
	Summary []workspaceSummary `json:"summary,omitempty"`
}

func (workspaceCapability) Validate(raw json.RawMessage, ctx ValidateContext) (json.RawMessage, error) {
	var props workspaceProps
	if err := decodeStrict(raw, &props); err != nil {
		return nil, err
	}
	kind, ok := ctx.World.Kinds[props.KindID]
	if !ok {
		return nil, fmt.Errorf("%w: workspace.kind_id", ErrInvalid)
	}
	props.Title = strings.TrimSpace(props.Title)
	if runeLength(props.Title) > 120 {
		return nil, fmt.Errorf("%w: workspace.title", ErrInvalid)
	}
	if len(props.Columns) < 1 || len(props.Columns) > 8 {
		return nil, fmt.Errorf("%w: workspace.columns", ErrInvalid)
	}
	seenFields := map[string]bool{}
	for index := range props.Columns {
		column := &props.Columns[index]
		column.Label = strings.TrimSpace(column.Label)
		if column.Label == "" || runeLength(column.Label) > 80 {
			return nil, fmt.Errorf("%w: workspace.columns/%d/label", ErrInvalid, index)
		}
		if seenFields[column.Field] {
			return nil, fmt.Errorf("%w: workspace.columns/%d/field", ErrInvalid, index)
		}
		seenFields[column.Field] = true
		if column.Field == "status" {
			column.Type = "status"
			continue
		}
		fieldType, ok := kind.Fields[column.Field]
		if !ok {
			return nil, fmt.Errorf("%w: workspace.columns/%d/field", ErrInvalid, index)
		}
		if column.Type != "" && column.Type != fieldType {
			return nil, fmt.Errorf("%w: workspace.columns/%d/type", ErrInvalid, index)
		}
		column.Type = fieldType
	}
	if len(props.Actions) > 8 {
		return nil, fmt.Errorf("%w: workspace.actions", ErrInvalid)
	}
	for index := range props.Actions {
		action := &props.Actions[index]
		action.Label = strings.TrimSpace(action.Label)
		if action.Label == "" || runeLength(action.Label) > 80 {
			return nil, fmt.Errorf("%w: workspace.actions/%d/label", ErrInvalid, index)
		}
		if action.Scope == "" {
			action.Scope = "row"
		}
		if action.Scope != "row" && action.Scope != "global" {
			return nil, fmt.Errorf("%w: workspace.actions/%d/scope", ErrInvalid, index)
		}
		if !validLocalID(action.BehaviorID) || !ctx.World.HasBehavior(action.BehaviorID) {
			return nil, fmt.Errorf("%w: workspace.actions/%d/behavior_id", ErrInvalid, index)
		}
	}
	if len(props.Summary) > 4 {
		return nil, fmt.Errorf("%w: workspace.summary", ErrInvalid)
	}
	for index := range props.Summary {
		summary := &props.Summary[index]
		summary.Label = strings.TrimSpace(summary.Label)
		if summary.Label == "" || runeLength(summary.Label) > 80 {
			return nil, fmt.Errorf("%w: workspace.summary/%d/label", ErrInvalid, index)
		}
		switch summary.Type {
		case "count", "done":
		case "sum":
			fieldType, ok := kind.Fields[summary.Field]
			if !ok || fieldType != "number" {
				return nil, fmt.Errorf("%w: workspace.summary/%d/field", ErrInvalid, index)
			}
		default:
			return nil, fmt.Errorf("%w: workspace.summary/%d/type", ErrInvalid, index)
		}
	}
	return json.Marshal(props)
}
