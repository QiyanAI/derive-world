// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const (
	testKindID   = "01a00000-0000-7000-8000-000000000001"
	testEntityID = "01a00000-0000-7000-8000-000000000002"
)

func testContext() *Context {
	return &Context{
		Kinds: map[string]KindContext{
			testKindID: {
				ID: testKindID, Name: "Thing",
				Fields: map[string]string{"name": "text", "count": "number", "lit": "flag"},
			},
		},
		Entities: map[string]EntityContext{
			testEntityID: {ID: testEntityID, KindID: testKindID},
		},
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return encoded
}

func TestCatalogIsDeterministicAndComplete(t *testing.T) {
	catalog := Default().Catalog()
	if len(catalog) < 10 {
		t.Fatalf("catalog has %d entries", len(catalog))
	}
	seen := map[string]bool{}
	for _, descriptor := range catalog {
		if seen[descriptor.ID] {
			t.Fatalf("duplicate capability %q", descriptor.ID)
		}
		seen[descriptor.ID] = true
		if descriptor.Kind != "presentation" && descriptor.Kind != "behavior" {
			t.Fatalf("capability %q kind = %q", descriptor.ID, descriptor.Kind)
		}
	}
	for _, id := range []string{
		"core.stack", "core.text", "core.space", "core.collection", "core.table",
		"core.field", "core.button", "core.choice", "core.meter", "core.state", "core.transfer",
	} {
		if !seen[id] {
			t.Fatalf("catalog is missing %q", id)
		}
	}
}

func TestValidateExperience(t *testing.T) {
	experience := &Experience{
		Version: 1,
		Root: &ViewNode{
			ID: "root", Capability: "core.stack",
			Props: mustJSON(t, map[string]any{"direction": "vertical", "gap": 12}),
			Children: []ViewNode{
				{
					ID: "title", Capability: "core.text",
					Props: mustJSON(t, map[string]any{"text": "{{world.title}} · {{entity." + testEntityID + ".field.count}}"}),
				},
				{
					ID: "toggle", Capability: "core.button",
					Props: mustJSON(t, map[string]any{"label": "Light it", "behavior_id": "light"}),
				},
			},
		},
		Behaviors: []Behavior{{
			ID: "light", Name: "Light it",
			Actions: []Action{{
				Capability: "core.state", Action: "add",
				Params: mustJSON(t, map[string]any{"entity_id": testEntityID, "field": "count", "value": 1}),
			}},
		}},
	}
	if err := ValidateExperience(experience, testContext()); err != nil {
		t.Fatalf("valid experience rejected: %v", err)
	}
}

func TestValidateExperienceRejectsClosedOrUnsafeShapes(t *testing.T) {
	base := func() *Experience {
		return &Experience{
			Version: 1,
			Root: &ViewNode{
				ID: "root", Capability: "core.text",
				Props: mustJSON(t, map[string]any{"text": "hello"}),
			},
		}
	}
	cases := map[string]func(*Experience){
		"unknown capability": func(experience *Experience) {
			experience.Root.Capability = "world.fixed_page"
		},
		"unknown prop": func(experience *Experience) {
			experience.Root.Props = mustJSON(t, map[string]any{"text": "x", "script": "alert(1)"})
		},
		"bad template": func(experience *Experience) {
			experience.Root.Props = mustJSON(t, map[string]any{"text": "{{run.code}})"})
		},
		"duplicate node": func(experience *Experience) {
			experience.Root.Children = []ViewNode{*experience.Root}
		},
		"children on text": func(experience *Experience) {
			experience.Root.Children = []ViewNode{{ID: "child", Capability: "core.text", Props: mustJSON(t, map[string]any{"text": "x"})}}
		},
		"missing behavior": func(experience *Experience) {
			experience.Root = &ViewNode{
				ID: "button", Capability: "core.button",
				Props: mustJSON(t, map[string]any{"label": "Go", "behavior_id": "missing"}),
			}
		},
		"event scope outside behavior": func(experience *Experience) {
			experience.Root = &ViewNode{
				ID: "field", Capability: "core.field",
				Props: mustJSON(t, map[string]any{"entity_id": "$event.entity", "field": "count"}),
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			experience := base()
			mutate(experience)
			if err := ValidateExperience(experience, testContext()); err == nil {
				t.Fatalf("expected rejection")
			}
		})
	}
}

func TestSpaceRejectsScriptAndHTMLPaths(t *testing.T) {
	base := &Experience{
		Version: 1,
		Root: &ViewNode{
			ID: "space", Capability: "core.space",
			Props: mustJSON(t, map[string]any{
				"views": []map[string]any{{
					"kind_id": testKindID, "path": "M 0 0 L 10 10 Z",
				}},
			}),
		},
	}
	if err := ValidateExperience(base, testContext()); err != nil {
		t.Fatalf("valid path rejected: %v", err)
	}
	for _, path := range []string{
		`<script>alert(1)</script>`,
		`M 0 0 url(#evil)`,
		`M 0 0 onclick="x"`,
		`javascript:alert(1)`,
	} {
		base.Root.Props = mustJSON(t, map[string]any{
			"views": []map[string]any{{"kind_id": testKindID, "path": path}},
		})
		if err := ValidateExperience(base, testContext()); err == nil {
			t.Fatalf("unsafe path accepted: %q", path)
		}
	}
	base.Root.Props = mustJSON(t, map[string]any{
		"views": []map[string]any{{
			"kind_id": testKindID, "path": "M 0 0 L 10 10 Z",
			"bindings": []map[string]any{{
				"field": "count", "target": "size",
				"in_min": -1e308, "in_max": 1e308, "out_min": 0.8, "out_max": 1.6,
			}},
		}},
	})
	if err := ValidateExperience(base, testContext()); err == nil {
		t.Fatalf("extreme binding input range accepted")
	}
}

func TestCollectionScopesItemReferences(t *testing.T) {
	experience := &Experience{
		Version: 1,
		Root: &ViewNode{
			ID: "list", Capability: "core.collection",
			Props: mustJSON(t, map[string]any{"kind_id": testKindID}),
			Children: []ViewNode{{
				ID: "row", Capability: "core.text",
				Props: mustJSON(t, map[string]any{"text": "{{item.name}} · {{item.field.count}}"}),
			}},
		},
	}
	if err := ValidateExperience(experience, testContext()); err != nil {
		t.Fatalf("item template rejected: %v", err)
	}
	experience.Root.Children[0].Props = mustJSON(t, map[string]any{"text": "{{item.field.missing}}"})
	if err := ValidateExperience(experience, testContext()); err == nil {
		t.Fatalf("missing item field accepted")
	}
}

type fakeEntity struct {
	kindID string
	status string
	values map[string]any
}

type fakeWorld struct {
	entities map[string]*fakeEntity
	fields   map[string]map[string]string
}

func newFakeWorld() *fakeWorld {
	return &fakeWorld{
		entities: map[string]*fakeEntity{
			testEntityID: {kindID: testKindID, status: "open", values: map[string]any{"count": float64(2), "lit": false, "name": "North"}},
		},
		fields: map[string]map[string]string{
			testKindID: {"name": "text", "count": "number", "lit": "flag"},
		},
	}
}

func (world *fakeWorld) HasEntity(entityID string) bool {
	_, ok := world.entities[entityID]
	return ok
}

func (world *fakeWorld) FieldType(entityID, field string) (string, bool) {
	entity, ok := world.entities[entityID]
	if !ok {
		return "", false
	}
	if field == "status" {
		return "status", true
	}
	value, ok := world.fields[entity.kindID][field]
	return value, ok
}

func (world *fakeWorld) Value(entityID, field string) (any, bool) {
	entity, ok := world.entities[entityID]
	if !ok {
		return nil, false
	}
	value, ok := entity.values[field]
	return value, ok
}

func (world *fakeWorld) SetValue(entityID, field string, value any) error {
	entity, ok := world.entities[entityID]
	if !ok {
		return ErrInvalid
	}
	entity.values[field] = value
	return nil
}

func (world *fakeWorld) Status(entityID string) (string, bool) {
	entity, ok := world.entities[entityID]
	if !ok {
		return "", false
	}
	return entity.status, true
}

func (world *fakeWorld) SetStatus(entityID, status string) error {
	entity, ok := world.entities[entityID]
	if !ok {
		return ErrInvalid
	}
	entity.status = status
	return nil
}

func TestExecuteBehaviorAndCondition(t *testing.T) {
	world := newFakeWorld()
	event := Event{EntityID: testEntityID, Value: float64(3)}
	behavior := &Behavior{
		ID: "act",
		Condition: &Condition{
			EntityID: "$event.entity", Field: "count", Op: "gte", Value: float64(2),
		},
		Actions: []Action{
			{Capability: "core.state", Action: "add", Params: mustJSON(t, map[string]any{"entity_id": "$event.entity", "field": "count", "value": 3})},
			{Capability: "core.state", Action: "toggle", Params: mustJSON(t, map[string]any{"entity_id": testEntityID, "field": "lit"})},
			{Capability: "core.state", Action: "set", Params: mustJSON(t, map[string]any{"entity_id": testEntityID, "field": "status", "value": "done"})},
		},
	}
	applied, err := ExecuteBehavior(Default(), behavior, event, world)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(applied) != 3 {
		t.Fatalf("applied = %d", len(applied))
	}
	if world.entities[testEntityID].values["count"] != float64(5) {
		t.Fatalf("count = %v", world.entities[testEntityID].values["count"])
	}
	if world.entities[testEntityID].values["lit"] != true {
		t.Fatalf("lit = %v", world.entities[testEntityID].values["lit"])
	}
	if world.entities[testEntityID].status != "done" {
		t.Fatalf("status = %q", world.entities[testEntityID].status)
	}

	// The same behavior is skipped when its condition is false.
	world.entities[testEntityID].values["count"] = float64(0)
	behavior.Condition.Value = float64(10)
	applied, err = ExecuteBehavior(Default(), behavior, event, world)
	if err != nil {
		t.Fatalf("execute skipped: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("condition false still applied %d actions", len(applied))
	}
}

func TestExperienceBounds(t *testing.T) {
	textProps := func() json.RawMessage {
		return mustJSON(t, map[string]any{"text": "x", "size": 18, "weight": "regular", "tone": "default", "align": "left"})
	}
	// 1 root + 24 branches + 48 leaves = 73 static nodes.
	wide := &Experience{Version: 1, Root: &ViewNode{
		ID: "root", Capability: "core.stack", Props: mustJSON(t, map[string]any{"direction": "vertical"}),
	}}
	for index := 0; index < 24; index += 1 {
		wide.Root.Children = append(wide.Root.Children, ViewNode{
			ID: fmt.Sprintf("branch-%d", index), Capability: "core.stack",
			Props: mustJSON(t, map[string]any{"direction": "vertical"}),
			Children: []ViewNode{
				{ID: fmt.Sprintf("a-%d", index), Capability: "core.text", Props: textProps()},
				{ID: fmt.Sprintf("b-%d", index), Capability: "core.text", Props: textProps()},
			},
		})
	}
	if err := ValidateExperience(wide, testContext()); err == nil {
		t.Fatalf("static node count above 64 was accepted")
	}

	// A stack chain deeper than 8.
	deep := &Experience{Version: 1, Root: &ViewNode{ID: "depth-0", Capability: "core.stack", Props: mustJSON(t, map[string]any{"direction": "vertical"})}}
	cursor := deep.Root
	for index := 1; index <= 9; index += 1 {
		child := ViewNode{ID: fmt.Sprintf("depth-%d", index), Capability: "core.stack", Props: mustJSON(t, map[string]any{"direction": "vertical"})}
		cursor.Children = []ViewNode{child}
		cursor = &cursor.Children[0]
	}
	if err := ValidateExperience(deep, testContext()); err == nil {
		t.Fatalf("view depth above 8 was accepted")
	}

	// 25 behaviors and 9 actions in one behavior.
	behavior := Behavior{ID: "act", Actions: []Action{{
		Capability: "core.state", Action: "toggle",
		Params: mustJSON(t, map[string]any{"entity_id": testEntityID, "field": "lit"}),
	}}}
	manyBehaviors := &Experience{Version: 1, Root: &ViewNode{ID: "root", Capability: "core.text", Props: textProps()}}
	for index := 0; index < 25; index += 1 {
		entry := behavior
		entry.ID = fmt.Sprintf("act-%d", index)
		manyBehaviors.Behaviors = append(manyBehaviors.Behaviors, entry)
	}
	if err := ValidateExperience(manyBehaviors, testContext()); err == nil {
		t.Fatalf("more than 24 behaviors was accepted")
	}
	manyActions := &Experience{Version: 1, Root: &ViewNode{ID: "root", Capability: "core.text", Props: textProps()}}
	manyActions.Behaviors = []Behavior{{ID: "act", Actions: make([]Action, 9)}}
	for index := range manyActions.Behaviors[0].Actions {
		manyActions.Behaviors[0].Actions[index] = behavior.Actions[0]
	}
	if err := ValidateExperience(manyActions, testContext()); err == nil {
		t.Fatalf("more than 8 actions was accepted")
	}

	// Oversized props and an oversized whole document.
	oversizedProps := &Experience{Version: 1, Root: &ViewNode{
		ID: "root", Capability: "core.text",
		Props: mustJSON(t, map[string]any{"text": strings.Repeat("x", 5000)}),
	}}
	if err := ValidateExperience(oversizedProps, testContext()); err == nil {
		t.Fatalf("props above 4 KiB was accepted")
	}
	large := &Experience{Version: 1, Root: &ViewNode{
		ID: "root", Capability: "core.stack", Props: mustJSON(t, map[string]any{"direction": "vertical"}),
	}}
	for index := 0; index < 40; index += 1 {
		large.Root.Children = append(large.Root.Children, ViewNode{
			ID: fmt.Sprintf("large-%d", index), Capability: "core.text",
			Props: mustJSON(t, map[string]any{"text": strings.Repeat("x", 1000)}),
		})
	}
	if err := ValidateExperience(large, testContext()); err == nil {
		t.Fatalf("experience above 32 KiB was accepted")
	}
}

func TestFieldAndMeterValidation(t *testing.T) {
	field := &Experience{Version: 1, Root: &ViewNode{
		ID: "field", Capability: "core.field",
		Props: mustJSON(t, map[string]any{"entity_id": testEntityID, "field": "count", "control": "auto"}),
	}}
	if err := ValidateExperience(field, testContext()); err != nil {
		t.Fatalf("valid field rejected: %v", err)
	}
	field.Root.Props = mustJSON(t, map[string]any{"entity_id": testEntityID, "field": "ghost", "control": "auto"})
	if err := ValidateExperience(field, testContext()); err == nil {
		t.Fatalf("field with unknown field accepted")
	}
	field.Root.Props = mustJSON(t, map[string]any{"entity_id": testEntityID, "field": "status", "control": "auto"})
	if err := ValidateExperience(field, testContext()); err == nil {
		t.Fatalf("field editing status was accepted without a renderer")
	}
	meter := &Experience{Version: 1, Root: &ViewNode{
		ID: "meter", Capability: "core.meter",
		Props: mustJSON(t, map[string]any{"entity_id": testEntityID, "field": "name", "min": 0, "max": 10}),
	}}
	if err := ValidateExperience(meter, testContext()); err == nil {
		t.Fatalf("meter over a text field accepted")
	}
}

func TestChoiceValidation(t *testing.T) {
	experience := &Experience{Version: 1, Root: &ViewNode{
		ID: "choice", Capability: "core.choice",
		Props: mustJSON(t, map[string]any{"prompt": "", "options": []any{
			map[string]any{"label": "One", "behavior_id": "act"},
		}}),
	}, Behaviors: []Behavior{{ID: "act", Actions: []Action{{
		Capability: "core.state", Action: "toggle",
		Params: mustJSON(t, map[string]any{"entity_id": testEntityID, "field": "lit"}),
	}}}}}
	if err := ValidateExperience(experience, testContext()); err == nil {
		t.Fatalf("choice with one option accepted")
	}
	experience.Root.Props = mustJSON(t, map[string]any{"prompt": "", "options": []any{
		map[string]any{"label": "One", "behavior_id": "act"},
		map[string]any{"label": "Two", "behavior_id": "act"},
	}})
	if err := ValidateExperience(experience, testContext()); err != nil {
		t.Fatalf("two-option choice rejected: %v", err)
	}
}

func TestExpandedViewBudget(t *testing.T) {
	context := testContext()
	for index := 0; index < 40; index += 1 {
		id := fmt.Sprintf("01a00000-0000-7000-8000-%012d", index+10)
		context.Entities[id] = EntityContext{ID: id, KindID: testKindID}
	}
	nested := &Experience{
		Version: 1,
		Root: &ViewNode{
			ID: "outer", Capability: "core.collection",
			Props: mustJSON(t, map[string]any{"kind_id": testKindID}),
			Children: []ViewNode{{
				ID: "inner", Capability: "core.collection",
				Props: mustJSON(t, map[string]any{"kind_id": testKindID}),
				Children: []ViewNode{{
					ID: "row", Capability: "core.text",
					Props: mustJSON(t, map[string]any{"text": "{{item.name}}"}),
				}},
			}},
		},
	}
	if err := ValidateExperience(nested, context); err == nil {
		t.Fatalf("nested collection expansion was accepted")
	}

	// A collection of tables/spaces multiplies the table rows/space entities
	// for every collection item; the budget must charge that inner work too.
	repeated := &Experience{
		Version: 1,
		Root: &ViewNode{
			ID: "outer", Capability: "core.collection",
			Props: mustJSON(t, map[string]any{"kind_id": testKindID}),
		},
	}
	for index := 0; index < 4; index += 1 {
		repeated.Root.Children = append(repeated.Root.Children, ViewNode{
			ID: fmt.Sprintf("table-%d", index), Capability: "core.table",
			Props: mustJSON(t, map[string]any{
				"kind_id": testKindID,
				"columns": []any{map[string]any{"source": "name", "label": ""}},
			}),
		})
	}
	if err := ValidateExperience(repeated, context); err == nil {
		t.Fatalf("repeated table inner work was not charged to the budget")
	}
}

func TestDefaultRegistryIsFrozen(t *testing.T) {
	if err := Default().RegisterPresentation(testBadge{}); err == nil {
		t.Fatalf("default registry accepted a runtime registration")
	}
}

type reservedBadge struct{}

func (reservedBadge) Descriptor() Descriptor {
	return Descriptor{
		ID: "test.reserved", Kind: "presentation", Version: 1,
		Title:       Localized{En: "Reserved", Zh: "保留"},
		Description: Localized{En: "Bad", Zh: "坏"},
		Params: &ParamSchema{Type: "object", Properties: map[string]*ParamProperty{
			"version": {Type: "number", Title: Localized{En: "Version", Zh: "版本"}},
		}},
	}
}

func (reservedBadge) Validate(raw json.RawMessage, _ ValidateContext) (json.RawMessage, error) {
	return raw, nil
}

func TestDescriptorRejectsActorOverridePropNames(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterPresentation(reservedBadge{}); err == nil {
		t.Fatalf("registry accepted a prop that the HTTP actor guard would always reject")
	}
}

func TestTransferCapabilityReuse(t *testing.T) {
	secondEntity := "01a00000-0000-7000-8000-000000000003"
	world := newFakeWorld()
	world.entities[secondEntity] = &fakeEntity{
		kindID: testKindID, status: "open", values: map[string]any{"count": float64(4)},
	}
	params := mustJSON(t, map[string]any{
		"from_entity": testEntityID, "to_entity": secondEntity, "field": "count", "amount": float64(3),
	})
	context := testContext()
	context.Entities[secondEntity] = EntityContext{ID: secondEntity, KindID: testKindID}
	if _, err := (transferCapability{}).ValidateAction("move", params, ValidateContext{
		World: context, AllowEvent: true,
	}); err != nil {
		t.Fatalf("transfer rejected: %v", err)
	}
	behavior := &Behavior{
		ID: "transfer",
		Actions: []Action{{
			Capability: "core.transfer", Action: "move", Params: params,
		}},
	}
	applied, err := ExecuteBehavior(Default(), behavior, Event{}, world)
	if err != nil {
		t.Fatalf("transfer execute: %v", err)
	}
	if len(applied) != 2 {
		t.Fatalf("applied = %d", len(applied))
	}
	if world.entities[testEntityID].values["count"] != float64(0) {
		t.Fatalf("source = %v", world.entities[testEntityID].values["count"])
	}
	if world.entities[secondEntity].values["count"] != float64(6) {
		t.Fatalf("target = %v", world.entities[secondEntity].values["count"])
	}
}

func TestRemapKeepsLocalIdentifiers(t *testing.T) {
	newEntity := "01a00000-0000-7000-8000-000000000098"
	localBehavior := "a" + testEntityID
	localField := "f" + testEntityID
	experience := &Experience{
		Version: 1,
		Root: &ViewNode{
			ID: "root", Capability: "core.text",
			Props: mustJSON(t, map[string]any{
				"text": "literal " + testEntityID + " and {{entity." + testEntityID + ".name}}",
				"size": 18, "weight": "regular", "tone": "default", "align": "left",
			}),
		},
		Behaviors: []Behavior{{
			ID: localBehavior,
			Actions: []Action{{
				Capability: "core.state", Action: "set",
				Params: mustJSON(t, map[string]any{"entity_id": testEntityID, "field": localField, "value": "x"}),
			}},
		}},
	}
	remapped, err := RemapExperience(experience, nil, map[string]string{testEntityID: newEntity})
	if err != nil {
		t.Fatalf("remap: %v", err)
	}
	if remapped.Behaviors[0].ID != localBehavior {
		t.Fatalf("local behavior id changed: %q", remapped.Behaviors[0].ID)
	}
	params := string(remapped.Behaviors[0].Actions[0].Params)
	if !strings.Contains(params, localField) {
		t.Fatalf("local field key changed: %s", params)
	}
	if !strings.Contains(params, newEntity) {
		t.Fatalf("reference id not remapped: %s", params)
	}
	text := string(remapped.Root.Props)
	if !strings.Contains(text, "literal "+testEntityID) {
		t.Fatalf("literal UUID text was rewritten: %s", text)
	}
	if !strings.Contains(text, "{{entity."+newEntity+".name}}") {
		t.Fatalf("template reference was not remapped: %s", text)
	}
}

func TestRemapExperienceRewritesReferences(t *testing.T) {
	newKind := "01a00000-0000-7000-8000-000000000099"
	newEntity := "01a00000-0000-7000-8000-000000000098"
	experience := &Experience{
		Version: 1,
		Root: &ViewNode{
			ID: "list", Capability: "core.collection",
			Props: mustJSON(t, map[string]any{"kind_id": testKindID}),
			Children: []ViewNode{{
				ID: "row", Capability: "core.text",
				Props: mustJSON(t, map[string]any{"text": "{{item.name}} at {{entity." + testEntityID + ".name}}"}),
			}},
		},
		Behaviors: []Behavior{{
			ID: "act",
			Actions: []Action{{
				Capability: "core.state", Action: "set",
				Params: mustJSON(t, map[string]any{"entity_id": testEntityID, "field": "name", "value": "x"}),
			}},
		}},
	}
	remapped, err := RemapExperience(experience, map[string]string{testKindID: newKind}, map[string]string{testEntityID: newEntity})
	if err != nil {
		t.Fatalf("remap: %v", err)
	}
	encoded, _ := json.Marshal(remapped)
	text := string(encoded)
	if strings.Contains(text, testKindID) || strings.Contains(text, testEntityID) {
		t.Fatalf("old ids survived: %s", text)
	}
	if !strings.Contains(text, newKind) || !strings.Contains(text, newEntity) {
		t.Fatalf("new ids missing: %s", text)
	}
	if remapped.Behaviors[0].ID != "act" || remapped.Root.ID != "list" {
		t.Fatalf("local ids changed")
	}
}

// testBadge is a capability that exists only in this test. It proves that a
// new presentation module can be validated for multiple worlds without any
// change to Experience, ViewNode, Behavior, Action, Condition or the registry
// core.
type testBadge struct{}

func (testBadge) Descriptor() Descriptor {
	return Descriptor{
		ID: "test.badge", Kind: "presentation", Version: 1,
		Title:       Localized{En: "Test badge", Zh: "测试徽章"},
		Description: Localized{En: "Test-only capability.", Zh: "仅测试能力。"},
		Params: &ParamSchema{Type: "object", Properties: map[string]*ParamProperty{
			"label": {Type: "string", Title: Localized{En: "Label", Zh: "文字"}},
		}},
	}
}

func (testBadge) Validate(raw json.RawMessage, _ ValidateContext) (json.RawMessage, error) {
	var props struct {
		Label string `json:"label"`
	}
	if err := decodeStrict(raw, &props); err != nil {
		return nil, err
	}
	if props.Label == "" || runeLength(props.Label) > 80 {
		return nil, ErrInvalid
	}
	return json.Marshal(props)
}

func TestCapabilityExtensionDoesNotChangeCore(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterPresentation(testBadge{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	for index, context := range []*Context{
		testContext(),
		func() *Context {
			other := testContext()
			other.Kinds["kind-two"] = KindContext{ID: "kind-two", Name: "Other", Fields: map[string]string{"value": "number"}}
			return other
		}(),
	} {
		experience := &Experience{
			Version: 1,
			Root: &ViewNode{
				ID: "root", Capability: "test.badge",
				Props: mustJSON(t, map[string]any{"label": "world " + string(rune('A'+index))}),
			},
		}
		if err := ValidateExperienceWith(registry, experience, context); err != nil {
			t.Fatalf("world %d rejected: %v", index, err)
		}
	}
	// The default registry still rejects the test-only module.
	experience := &Experience{
		Version: 1,
		Root:    &ViewNode{ID: "root", Capability: "test.badge", Props: mustJSON(t, map[string]any{"label": "x"})},
	}
	if err := ValidateExperience(experience, testContext()); err == nil {
		t.Fatalf("default registry accepted test-only capability")
	}
}
