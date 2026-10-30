// SPDX-License-Identifier: Apache-2.0
package personalai

import (
	"encoding/json"
	"testing"
)

const referenceState = `{
  "title":"Garden","premise":"A small garden.",
  "kinds":[
    {"id":"01a00000-0000-7000-8000-000000000001","name":"灯","fields":[{"key":"f0","label":"亮着","type":"flag"}]},
    {"id":"01a00000-0000-7000-8000-000000000003","name":"石板","fields":[]}
  ],
  "nodes":[
    {"id":"01a00000-0000-7000-8000-000000000002","kind_id":"01a00000-0000-7000-8000-000000000001","name":"屋檐灯","text":"x","status":"open","values":{},"x":0.5,"y":0.5},
    {"id":"01a00000-0000-7000-8000-000000000004","kind_id":"01a00000-0000-7000-8000-000000000003","name":"石板小路","text":"y","status":"open","values":{},"x":0.5,"y":0.5}
  ],
  "links":[],"rules":[]
}`

func resolvedMap(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestResolveChangeReferencesMapsNamesAndIndexes(t *testing.T) {
	state := json.RawMessage(referenceState)
	kindID := "01a00000-0000-7000-8000-000000000001"
	nodeID := "01a00000-0000-7000-8000-000000000002"
	linkTo := "01a00000-0000-7000-8000-000000000004"
	cases := []struct {
		action string
		input  string
		check  func(map[string]any) bool
	}{
		{"add_node", `{"kind_id":"灯","name":"新灯"}`,
			func(m map[string]any) bool { return m["kind_id"] == kindID }},
		{"add_node", `{"kind_id":"0","name":"新灯"}`,
			func(m map[string]any) bool { return m["kind_id"] == kindID }},
		{"add_node", `{"kind_id":"屋檐灯","name":"新灯"}`,
			func(m map[string]any) bool { return m["kind_id"] == kindID }},
		{"set_node_field", `{"node_id":"屋檐灯","field":"亮着","value":true}`,
			func(m map[string]any) bool { return m["node_id"] == nodeID && m["field"] == "f0" }},
		{"add_link", `{"from":"屋檐灯","to":"石板小路"}`,
			func(m map[string]any) bool { return m["from"] == nodeID && m["to"] == linkTo }},
		{"batch_change", `{"note":"x","actions":[{"action":"add_node","input":{"kind_id":"灯","name":"新灯"}}]}`,
			func(m map[string]any) bool {
				actions, _ := m["actions"].([]any)
				if len(actions) != 1 {
					return false
				}
				sub, _ := actions[0].(map[string]any)
				input, _ := sub["input"].(map[string]any)
				return input["kind_id"] == kindID
			}},
	}
	for _, testCase := range cases {
		resolved, changed := resolveChangeReferences(state, json.RawMessage(testCase.input), testCase.action)
		if !changed {
			t.Fatalf("%s %s was not resolved", testCase.action, testCase.input)
		}
		if !testCase.check(resolvedMap(t, resolved)) {
			t.Fatalf("%s %s resolved incorrectly: %s", testCase.action, testCase.input, resolved)
		}
	}
}

func TestResolveChangeReferencesLeavesAmbiguousAndValidValuesAlone(t *testing.T) {
	state := json.RawMessage(`{
		"title":"Garden","premise":"x",
		"kinds":[
			{"id":"01a00000-0000-7000-8000-000000000001","name":"灯"},
			{"id":"01a00000-0000-7000-8000-000000000002","name":"灯"}
		],
		"nodes":[],"links":[],"rules":[]
	}`)
	if _, changed := resolveChangeReferences(state,
		json.RawMessage(`{"kind_id":"灯","name":"x"}`), "add_node"); changed {
		t.Fatal("a duplicate kind name was resolved")
	}
	valid := `{"kind_id":"01a00000-0000-7000-8000-000000000001"}`
	if _, changed := resolveChangeReferences(json.RawMessage(referenceState),
		json.RawMessage(valid), "add_node"); changed {
		t.Fatal("a valid id was rewritten")
	}
	if _, changed := resolveChangeReferences(json.RawMessage(referenceState),
		json.RawMessage(`{"kind_id":"不存在的类型"}`), "add_node"); changed {
		t.Fatal("an unknown kind name was resolved")
	}
}

func TestResolveChangeReferencesMovesShortAliases(t *testing.T) {
	state := json.RawMessage(referenceState)
	kindID := "01a00000-0000-7000-8000-000000000001"
	nodeID := "01a00000-0000-7000-8000-000000000002"
	resolved, changed := resolveChangeReferences(state,
		json.RawMessage(`{"kind":"灯","name":"新灯"}`), "add_node")
	if !changed {
		t.Fatal("kind alias was not resolved")
	}
	object := resolvedMap(t, resolved)
	if object["kind_id"] != kindID {
		t.Fatalf("kind_id = %#v", object["kind_id"])
	}
	if _, exists := object["kind"]; exists {
		t.Fatal("short alias was not removed")
	}
	resolved, changed = resolveChangeReferences(state,
		json.RawMessage(`{"node":"屋檐灯","field_label":"亮着","value":true}`), "set_node_field")
	if !changed {
		t.Fatal("node/field aliases were not resolved")
	}
	object = resolvedMap(t, resolved)
	if object["node_id"] != nodeID || object["field"] != "f0" {
		t.Fatalf("aliases resolved to %#v", object)
	}
}
