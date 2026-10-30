// SPDX-License-Identifier: Apache-2.0
package personalai

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// groupTurnActions combines the plain changes of one assistant reply into one
// batch_change proposal. They were prepared against the same object version and
// are meant to be approved as one complete plan; creating one proposal per
// action made every proposal after the first stale as soon as the first one was
// applied. Actions that replace the whole state or execute a program are never
// nested inside a batch.
func groupTurnActions(actions []rawAction, objectID uuid.UUID, version int,
	zh bool) ([]rawAction, int) {
	batchable := make(map[int]proposalActionInput, len(actions))
	indexes := make([]int, 0, len(actions))
	for index, action := range actions {
		if action.CapabilityID != "object.propose_change" {
			continue
		}
		var proposal proposalActionInput
		if err := decodeSingleJSON(string(action.Input), &proposal); err != nil {
			continue
		}
		if !batchableChangeAction(proposal.Action) || len(proposal.Input) == 0 {
			continue
		}
		// A mismatched object or expected version must still reach the normal
		// rejection path; it is never folded into a batch that would skip it.
		if proposal.ObjectID != nil && *proposal.ObjectID != objectID.String() {
			continue
		}
		if proposal.ExpectedVersion != nil && *proposal.ExpectedVersion != version {
			continue
		}
		batchable[index] = proposal
		indexes = append(indexes, index)
	}
	if len(indexes) < 2 {
		return actions, 0
	}
	changes := make([]proposalActionInput, 0, len(indexes))
	for _, index := range indexes {
		changes = append(changes, batchable[index])
	}
	note := batchChangeNote(changes, zh)
	subActions := make([]map[string]any, 0, len(changes))
	for _, change := range changes {
		subActions = append(subActions, map[string]any{
			"action": change.Action, "input": change.Input,
		})
	}
	encoded, err := json.Marshal(map[string]any{
		"action": "batch_change",
		"input":  map[string]any{"note": note, "actions": subActions},
	})
	if err != nil {
		return actions, 0
	}
	synthetic := rawAction{CapabilityID: "object.propose_change", Input: encoded}
	result := make([]rawAction, 0, len(actions)-len(indexes)+1)
	inserted := false
	for index, action := range actions {
		if _, ok := batchable[index]; ok {
			if !inserted {
				result = append(result, synthetic)
				inserted = true
			}
			continue
		}
		result = append(result, action)
	}
	return result, len(indexes) - 1
}

// batchableChangeAction lists the plain open-world edits that compose into one
// state. Whole-state reshapes, custom programs and behavior runs are excluded
// by the same rule in the objects package.
func batchableChangeAction(action string) bool {
	switch action {
	case "set_title", "set_premise", "add_tag", "remove_tag",
		"add_kind", "update_kind", "remove_kind",
		"add_node", "update_node", "remove_node", "move_node",
		"set_node_field", "set_node_status", "touch_node",
		"add_link", "remove_link", "add_rule", "remove_rule",
		"set_views", "set_presentation", "set_experience":
		return true
	default:
		return false
	}
}

// batchChangeNote writes the one-line human preview of the combined plan. It
// uses only names and prose the model supplied; ids and field keys never reach
// the approval card.
func batchChangeNote(changes []proposalActionInput, zh bool) string {
	parts := make([]string, 0, len(changes))
	for _, change := range changes {
		parts = append(parts, changeSummary(change, zh))
	}
	joiner := ", "
	prefix := fmt.Sprintf("Apply %d changes: ", len(changes))
	if zh {
		joiner = "、"
		prefix = fmt.Sprintf("一次应用 %d 项修改：", len(changes))
	}
	return truncateRunes(prefix+strings.Join(parts, joiner), 480)
}

func changeSummary(change proposalActionInput, zh bool) string {
	var input map[string]any
	_ = json.Unmarshal(change.Input, &input)
	name := ""
	for _, key := range []string{"name", "title", "label", "text", "premise", "tag"} {
		if value, ok := input[key].(string); ok && strings.TrimSpace(value) != "" {
			name = strings.TrimSpace(value)
			break
		}
	}
	if utf8.RuneCountInString(name) > 40 {
		name = truncateRunes(name, 40) + "…"
	}
	quoted := func(fallback string) string {
		if name == "" {
			return fallback
		}
		if zh {
			return "「" + name + "」"
		}
		return "“" + name + "”"
	}
	switch change.Action {
	case "set_title":
		if zh {
			return "重命名为" + quoted("新标题")
		}
		return "rename to " + quoted("a new title")
	case "set_premise":
		if zh {
			return "改变世界前提"
		}
		return "change the premise"
	case "add_tag":
		if zh {
			return "添加标签" + quoted("一个标签")
		}
		return "add a tag"
	case "remove_tag":
		if zh {
			return "移除标签" + quoted("一个标签")
		}
		return "remove a tag"
	case "add_kind":
		if zh {
			return "添加一种内容" + quoted("")
		}
		return "add a kind" + quoted("")
	case "update_kind":
		if zh {
			return "更新一种内容" + quoted("")
		}
		return "update a kind"
	case "remove_kind":
		if zh {
			return "移除一种内容" + quoted("")
		}
		return "remove a kind"
	case "add_node":
		if zh {
			return "添加" + quoted("一个事物")
		}
		return "add " + quoted("a thing")
	case "update_node":
		if zh {
			return "更新" + quoted("一个事物")
		}
		return "update " + quoted("a thing")
	case "remove_node":
		if zh {
			return "移除" + quoted("一个事物")
		}
		return "remove " + quoted("a thing")
	case "move_node":
		if zh {
			return "移动" + quoted("一个事物")
		}
		return "move " + quoted("a thing")
	case "set_node_field", "set_node_status", "touch_node":
		if zh {
			return "改变" + quoted("一个事物") + "的状态"
		}
		return "change the state of " + quoted("a thing")
	case "add_link":
		if zh {
			return "添加一条连接"
		}
		return "add a connection"
	case "remove_link":
		if zh {
			return "移除一条连接"
		}
		return "remove a connection"
	case "add_rule":
		if zh {
			return "添加一条规则"
		}
		return "add a rule"
	case "remove_rule":
		if zh {
			return "移除一条规则"
		}
		return "remove a rule"
	case "set_views":
		if zh {
			return "改变可见视图"
		}
		return "change the visible views"
	case "set_presentation":
		if zh {
			return "改变外观"
		}
		return "change the appearance"
	case "set_experience":
		if zh {
			return "改变体验方式"
		}
		return "change how the world is experienced"
	default:
		if zh {
			return "做一项修改"
		}
		return "make a change"
	}
}
