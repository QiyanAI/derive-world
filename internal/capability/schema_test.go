// SPDX-License-Identifier: Apache-2.0
package capability

import (
	"errors"
	"strings"
	"testing"
)

func TestFrozenSchemasCompile(t *testing.T) {
	definitions, err := loadCompiledDefinitions()
	if err != nil {
		t.Fatalf("compile frozen schemas: %v", err)
	}
	if len(definitions) != 5 {
		t.Fatalf("definitions = %d", len(definitions))
	}
	if err := validateFrozenSet(definitions); err != nil {
		t.Fatalf("validate frozen set: %v", err)
	}
	for id, definition := range definitions {
		if err := definition.Input.ValidateJSON([]byte(`{}`)); err == nil {
			t.Errorf("%s input accepted an empty object", id)
		}
	}
}

func TestObjectReadSchema(t *testing.T) {
	definitions, _ := loadCompiledDefinitions()
	schema := definitions[IDObjectRead]
	valid := `{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc"}`
	if err := schema.Input.ValidateJSON([]byte(valid)); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	for _, invalid := range []string{
		`{"object_id":"not-a-uuid"}`,
		`{"object_id":"0192f0c4-5a6b-4c8d-9e0f-123456789abc"}`,
		`{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","extra":true}`,
		`{}`,
	} {
		if err := schema.Input.ValidateJSON([]byte(invalid)); err == nil {
			t.Errorf("invalid input accepted: %s", invalid)
		}
	}
}

func TestContentStructureSchema(t *testing.T) {
	definitions, _ := loadCompiledDefinitions()
	schema := definitions[IDContentStructure]
	valid := `{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","text":"hello","format":"markdown"}`
	if err := schema.Input.ValidateJSON([]byte(valid)); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	if err := schema.Input.ValidateJSON([]byte(`{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","text":"","format":"markdown"}`)); err == nil {
		t.Fatal("empty text accepted")
	}
	if err := schema.Input.ValidateJSON([]byte(`{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","text":"x","format":"sentences"}`)); err == nil {
		t.Fatal("unsupported format accepted")
	}
	output := `{"items":[{"index":0,"kind":"heading","text":"T","level":1}],"item_count":1,"truncated":false,"format":"markdown"}`
	if err := schema.Output.ValidateJSON([]byte(output)); err != nil {
		t.Fatalf("valid output rejected: %v", err)
	}
	for _, invalid := range []string{
		`{"items":[{"index":0,"kind":"heading","text":"T","level":7}],"item_count":1,"truncated":false,"format":"markdown"}`,
		`{"items":[{"index":0,"kind":"heading","text":"T","extra":1}],"item_count":1,"truncated":false,"format":"markdown"}`,
		`{"items":[],"item_count":0,"truncated":false,"format":"markdown","extra":1}`,
	} {
		if err := schema.Output.ValidateJSON([]byte(invalid)); err == nil {
			t.Errorf("invalid output accepted: %s", invalid)
		}
	}
}

func TestDeriveSchemaConstraints(t *testing.T) {
	definitions, _ := loadCompiledDefinitions()
	schema := definitions[IDObjectDerive]
	valid := []string{
		`{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","title":"Child","inheritance_fields":["summary"]}`,
		`{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","title":"Child","inheritance_fields":["summary","body"],"source_version":2}`,
	}
	for _, input := range valid {
		if err := schema.Input.ValidateJSON([]byte(input)); err != nil {
			t.Errorf("valid input rejected: %v", err)
		}
	}
	invalid := []string{
		`{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","title":"Child","inheritance_fields":[]}`,
		`{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","title":"Child","inheritance_fields":["summary","summary"]}`,
		`{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","title":"Child","inheritance_fields":["unknown"]}`,
		`{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","title":"","inheritance_fields":["summary"]}`,
	}
	for _, input := range invalid {
		if err := schema.Input.ValidateJSON([]byte(input)); err == nil {
			t.Errorf("invalid input accepted: %s", input)
		}
	}
}

func TestCompileRejectsUnsupportedKeywords(t *testing.T) {
	raw := `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"oneOf":[]}`
	if _, err := CompileSchema([]byte(raw)); err == nil {
		t.Fatal("oneOf was accepted")
	}
	open := `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":true}`
	if _, err := CompileSchema([]byte(open)); err == nil {
		t.Fatal("open root was accepted")
	}
}

func TestIntegerRequiresIntegralNumber(t *testing.T) {
	raw := `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"properties":{"n":{"type":"integer","minimum":1}},"required":["n"]}`
	schema, err := CompileSchema([]byte(raw))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if err := schema.ValidateJSON([]byte(`{"n":1.0}`)); err != nil {
		t.Fatalf("integral float was rejected: %v", err)
	}
	if err := schema.ValidateJSON([]byte(`{"n":1.5}`)); err == nil {
		t.Fatal("fractional integer was accepted")
	}
}

func TestSchemaValidationErrorPointer(t *testing.T) {
	definitions, _ := loadCompiledDefinitions()
	err := definitions[IDObjectRead].Input.ValidateJSON([]byte(`{"object_id":"bad"}`))
	if err == nil {
		t.Fatal("expected error")
	}
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error is not a ValidationError: %v", err)
	}
	if validation.Pointer != "/object_id" {
		t.Fatalf("pointer = %q", validation.Pointer)
	}
	if !strings.Contains(err.Error(), "/object_id") {
		t.Fatalf("error = %v", err)
	}
}
