// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"strings"
	"testing"
)

func customContext() ValidateContext {
	return ValidateContext{World: &Context{
		Kinds: map[string]KindContext{}, Entities: map[string]EntityContext{},
		Behaviors: map[string]bool{}, Variables: map[string]string{},
	}}
}

func TestCustomProgramRejectsUnsafeValues(t *testing.T) {
	base := `{"background":{"mode":"solid","hue":220,"hue2":220,"lightness":10,"accent_hue":40},"nodes":[%s]}`
	node := func(body string) json.RawMessage {
		return json.RawMessage(strings.Replace(base, "%s", body, 1))
	}
	cases := []struct {
		name string
		raw  json.RawMessage
	}{
		{"script path", node(`{"id":"n1","type":"path","x":10,"y":10,"path":"M 0 0 url(#x)"}`)},
		{"invalid expression", node(`{"id":"n1","type":"dot","x":10,"y":10,"r":10,"on_click":[{"type":"set_var","name":"a","value":{"expr":"1 +"}}]}`)},
		{"unknown field", node(`{"id":"n1","type":"dot","x":10,"y":10,"r":10,"evil":true}`)},
		{"unsafe sound", node(`{"id":"n1","type":"dot","x":10,"y":10,"r":10,"on_click":[{"type":"sound","freq":99999,"duration":0.3}]}`)},
		{"bad condition", node(`{"id":"n1","type":"dot","x":10,"y":10,"r":10,"visible_when":{"var":"a","op":"exec"}}`)},
		{"unknown variable name", node(`{"id":"n1","type":"dot","x":10,"y":10,"r":10,"on_click":[{"type":"set_var","name":"A BAD","value":1}]}`)},
	}
	for _, tc := range cases {
		if _, err := ValidateCustom(tc.raw, customContext()); err == nil {
			t.Errorf("%s: expected rejection", tc.name)
		}
	}
}

func TestCustomProgramAcceptsSafeInstrument(t *testing.T) {
	raw := json.RawMessage(`{
		"title":"instrument","background":{"mode":"gradient","hue":240,"hue2":265,"lightness":8,"accent_hue":42},
		"vars":{"a":false,"notes":0},
		"nodes":[{"id":"n1","type":"dot","x":400,"y":400,"r":40,"hue":40,
			"on_click":[{"type":"toggle_var","name":"a"},
				{"type":"add_var","name":"notes","value":1},
				{"type":"sound","freq":440,"duration":0.2,"wave":"sine"}]}],
		"timeline":{"duration":8,"loop":true,"tracks":[{"node_id":"n1","property":"r","keys":[{"t":0,"v":40},{"t":4,"v":50}]}]}
	}`)
	normalized, err := ValidateCustom(raw, customContext())
	if err != nil {
		t.Fatalf("safe program rejected: %v", err)
	}
	var program CustomProgram
	if err := json.Unmarshal(normalized, &program); err != nil {
		t.Fatalf("normalized program: %v", err)
	}
	if len(program.Nodes) != 1 || program.Nodes[0].Scale != 1 {
		t.Fatalf("normalized program = %#v", program)
	}
	programFor, ok := CustomProgramFor(&Experience{Version: 1, Root: &ViewNode{
		ID: "root", Capability: "core.custom", Props: normalized,
	}}, "root")
	if !ok || programFor.Title != "instrument" {
		t.Fatalf("program lookup failed: %#v", programFor)
	}
}
