// SPDX-License-Identifier: Apache-2.0
package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/QiyanAI/derive-world/api/openapi"
	"github.com/QiyanAI/derive-world/internal/capability"
)

func TestCapabilityOpenAPIContract(t *testing.T) {
	loader := openapi3.NewLoader()
	document, err := loader.LoadFromData(openapi.Spec)
	if err != nil {
		t.Fatalf("load OpenAPI: %v", err)
	}
	if err := document.Validate(context.Background()); err != nil {
		t.Fatalf("validate OpenAPI: %v", err)
	}
	descriptorSchema := document.Components.Schemas["CapabilityDescriptor"]
	if descriptorSchema == nil || descriptorSchema.Value == nil {
		t.Fatal("CapabilityDescriptor schema is missing")
	}
	invokeSchema := document.Components.Schemas["CapabilityInvokeRequest"]
	if invokeSchema == nil || invokeSchema.Value == nil {
		t.Fatal("CapabilityInvokeRequest schema is missing")
	}

	definitions, err := capability.FrozenDefinitions()
	if err != nil {
		t.Fatalf("frozen definitions: %v", err)
	}
	if len(definitions) != 5 {
		t.Fatalf("definitions = %d", len(definitions))
	}
	for _, definition := range definitions {
		encoded, err := json.Marshal(definition)
		if err != nil {
			t.Fatalf("marshal descriptor: %v", err)
		}
		var value any
		if err := json.Unmarshal(encoded, &value); err != nil {
			t.Fatalf("decode descriptor: %v", err)
		}
		if err := descriptorSchema.Value.VisitJSON(value); err != nil {
			t.Errorf("%s descriptor does not match OpenAPI: %v", definition.CapabilityID, err)
		}
		if _, ok := value.(map[string]any)["enabled"]; ok {
			t.Errorf("%s descriptor exposes internal enabled state", definition.CapabilityID)
		}
	}

	samples := map[string]string{
		capability.IDObjectRead:          `{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc"}`,
		capability.IDObjectContext:       `{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc"}`,
		capability.IDObjectProposeChange: `{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","action":"set_title","input":{"title":"T"},"expected_version":1}`,
		capability.IDObjectDerive:        `{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","inheritance_fields":["summary"],"title":"Child"}`,
		capability.IDContentStructure:    `{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","text":"hello","format":"markdown"}`,
	}
	for id, sample := range samples {
		var value any
		if err := json.Unmarshal([]byte(sample), &value); err != nil {
			t.Fatalf("%s sample: %v", id, err)
		}
		if err := invokeSchema.Value.VisitJSON(value); err != nil {
			t.Errorf("%s sample input does not match OpenAPI: %v", id, err)
		}
	}

	responseSamples := map[string]string{
		"CapabilityInvocationSuccess": `{"id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","capability_id":"object.read","status":"succeeded","output":{"object":{"id":"x"}},"duration_ms":1,"created_at":"2026-10-23T00:00:00Z","completed_at":"2026-10-23T00:00:00.001Z"}`,
		"CapabilityInvocation":        `{"id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","capability_id":"object.read","contract_version":"1.0","provider_id":"trusted_core","status":"succeeded","output":{"object":{"id":"x"}},"error_code":null,"duration_ms":1,"created_at":"2026-10-23T00:00:00Z","completed_at":"2026-10-23T00:00:00.001Z"}`,
		"CapabilityInvocationPending": `{"id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","capability_id":"object.derive","status":"pending","confirmation_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abd","created_at":"2026-10-23T00:00:00Z"}`,
	}
	for name, sample := range responseSamples {
		schema := document.Components.Schemas[name]
		if schema == nil || schema.Value == nil {
			t.Errorf("%s schema is missing", name)
			continue
		}
		var value any
		if err := json.Unmarshal([]byte(sample), &value); err != nil {
			t.Fatalf("%s sample: %v", name, err)
		}
		if err := schema.Value.VisitJSON(value); err != nil {
			t.Errorf("%s sample does not match its schema: %v", name, err)
		}
	}

	for _, path := range []string{
		"/api/v1/capabilities",
		"/api/v1/capabilities/{capability_id}",
		"/api/v1/capabilities/{capability_id}/invoke",
		"/api/v1/capability-invocations/{id}",
	} {
		item := document.Paths.Find(path)
		if item == nil {
			t.Errorf("OpenAPI path %s is missing", path)
			continue
		}
		for method, operation := range item.Operations() {
			if operation.Security == nil || !hasSecurityScheme(*operation.Security, "bearerAuth") {
				t.Errorf("%s %s does not allow bearerAuth", method, path)
			}
		}
	}
}
