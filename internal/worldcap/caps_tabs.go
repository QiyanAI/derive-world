// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"fmt"
	"strings"
)

// tabsCapability combines several universal surfaces inside one world. Labels
// are world content, not product categories; the same world can be played,
// read and worked in without becoming another object type.
type tabsCapability struct{}

func (tabsCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.tabs", Kind: "presentation", Version: 1,
		Title:          Localized{En: "World surfaces", Zh: "世界的呈现方式"},
		Description:    Localized{En: "Switch between several surfaces of the same world; all share one state.", Zh: "在同一个世界的多个呈现方式之间切换；它们共享同一份状态。"},
		AllowsChildren: true,
		Params: &ParamSchema{
			Type: "object",
			Properties: map[string]*ParamProperty{
				"tabs": {
					Type: "array", Title: Localized{En: "Tabs", Zh: "标签"},
					Items: &ParamProperty{
						Type: "object",
						Properties: map[string]*ParamProperty{
							"label":    {Type: "string", MaxLength: 40, Title: Localized{En: "Label", Zh: "标签文字"}},
							"child_id": {Type: "string", Format: "text", Title: Localized{En: "Child", Zh: "子表面"}},
						},
						Required: []string{"label", "child_id"},
					},
				},
				"default": {Type: "string", Format: "text", Title: Localized{En: "Default tab", Zh: "默认标签"}},
			},
			Required: []string{"tabs"},
		},
	}
}

type tabSpec struct {
	Label   string `json:"label"`
	ChildID string `json:"child_id"`
}

type tabsProps struct {
	Tabs    []tabSpec `json:"tabs"`
	Default string    `json:"default,omitempty"`
}

func (tabsCapability) Validate(raw json.RawMessage, _ ValidateContext) (json.RawMessage, error) {
	var props tabsProps
	if err := decodeStrict(raw, &props); err != nil {
		return nil, err
	}
	if len(props.Tabs) < 1 || len(props.Tabs) > 8 {
		return nil, fmt.Errorf("%w: tabs.tabs", ErrInvalid)
	}
	seen := map[string]bool{}
	for index := range props.Tabs {
		tab := &props.Tabs[index]
		tab.Label = strings.TrimSpace(tab.Label)
		if tab.Label == "" || runeLength(tab.Label) > 40 {
			return nil, fmt.Errorf("%w: tabs.tabs/%d/label", ErrInvalid, index)
		}
		if !validLocalID(tab.ChildID) || seen[tab.ChildID] {
			return nil, fmt.Errorf("%w: tabs.tabs/%d/child_id", ErrInvalid, index)
		}
		seen[tab.ChildID] = true
	}
	if props.Default != "" && !seen[props.Default] {
		return nil, fmt.Errorf("%w: tabs.default", ErrInvalid)
	}
	if props.Default == "" {
		props.Default = props.Tabs[0].ChildID
	}
	return json.Marshal(props)
}
