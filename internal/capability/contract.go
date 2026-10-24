// SPDX-License-Identifier: Apache-2.0
package capability

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ContractVersion is frozen by D06-04. v0.1.0 accepts only "1.0".
const ContractVersion = "1.0"

// Capability IDs and Provider IDs are frozen by G00 contract §A.4.4 and §A.8.2.
const (
	IDObjectRead          = "object.read"
	IDObjectContext       = "object.context"
	IDObjectProposeChange = "object.propose_change"
	IDObjectDerive        = "object.derive"
	IDContentStructure    = "content.structure"

	ProviderTrustedCore      = "trusted_core"
	ProviderContentStructure = "reference.content-structure"
	TransportInProcess       = "in_process"
	// TransportHTTPHMAC is the frozen D06 1.0 request-signed transport. Its
	// response is not signed; it remains available only for internal
	// compatibility.
	TransportHTTPHMAC = "http_hmac"
	// TransportHTTPHMACV2 is the G06 versioned transport profile. It uses the
	// identical D06 1.0 request envelope and adds a signed response bound to
	// the request timestamp, invocation id, status, and body hash. Public
	// providers must use v2.
	TransportHTTPHMACV2        = "http_hmac_v2"
	maxTimeoutMS               = 10000
	maxCapabilityInputBytes    = 65536
	maxCapabilityOutputBytes   = 262144
	minHMACSecretBytes         = 32
	maxProviderResponseBytes   = 262144
	defaultCapabilityRateLimit = 60
)

//go:embed schemas/*.json
var schemaFS embed.FS

// Constraints are the frozen execution limits. Configuration may lower them
// but never raise them.
type Constraints struct {
	TimeoutMS      int `json:"timeout_ms"`
	MaxInputBytes  int `json:"max_input_bytes"`
	MaxOutputBytes int `json:"max_output_bytes"`
}

// FrozenConstraints is the D06 limit set for every first-release capability.
func FrozenConstraints() Constraints {
	return Constraints{
		TimeoutMS:      maxTimeoutMS,
		MaxInputBytes:  maxCapabilityInputBytes,
		MaxOutputBytes: maxCapabilityOutputBytes,
	}
}

// Definition is the A.8.1 descriptor plus its enabled state. Provider network
// details are deliberately absent from this type.
type Definition struct {
	CapabilityID         string          `json:"capability_id"`
	ContractVersion      string          `json:"contract_version"`
	ProviderID           string          `json:"provider_id"`
	Description          string          `json:"description"`
	InputSchema          json.RawMessage `json:"input_schema"`
	OutputSchema         json.RawMessage `json:"output_schema"`
	RequiredScopes       []string        `json:"required_scopes"`
	SupportedOperations  []string        `json:"supported_operations"`
	ExecutionConstraints Constraints     `json:"execution_constraints"`
	// Enabled is internal state; A.8.1 descriptors expose only enabled
	// capabilities, so the field is never serialized.
	Enabled bool `json:"-"`
}

// CompiledDefinition carries the runtime validators beside the descriptor.
type CompiledDefinition struct {
	Definition
	Input  *Schema
	Output *Schema
}

type frozenDefinition struct {
	id          string
	provider    string
	description string
	scopes      []string
	operations  []string
}

var frozenDefinitions = []frozenDefinition{
	{
		id: IDObjectRead, provider: ProviderTrustedCore,
		description: "Read an authorized Living Object snapshot.",
		scopes:      []string{"read"}, operations: []string{"read"},
	},
	{
		id: IDObjectContext, provider: ProviderTrustedCore,
		description: "Return the bounded Handoff projection for an authorized Living Object.",
		scopes:      []string{"read"}, operations: []string{"read"},
	},
	{
		id: IDObjectProposeChange, provider: ProviderTrustedCore,
		description: "Create a human-reviewable proposal for one whitelisted object action.",
		scopes:      []string{"contribute"}, operations: []string{"propose"},
	},
	{
		id: IDObjectDerive, provider: ProviderTrustedCore,
		description: "Create an independent derived Living Object from an authorized snapshot.",
		scopes:      []string{"derive"}, operations: []string{"derive"},
	},
	{
		id: IDContentStructure, provider: ProviderContentStructure,
		description: "Structure caller-provided text deterministically through the reference Provider.",
		scopes:      []string{"read"}, operations: []string{"structure"},
	},
}

// FrozenIDs returns the five capability IDs in contract order.
func FrozenIDs() []string {
	ids := make([]string, 0, len(frozenDefinitions))
	for _, item := range frozenDefinitions {
		ids = append(ids, item.id)
	}
	return ids
}

// ProviderFor returns the frozen provider binding for a capability.
func ProviderFor(capabilityID string) (string, bool) {
	for _, item := range frozenDefinitions {
		if item.id == capabilityID {
			return item.provider, true
		}
	}
	return "", false
}

// loadCompiledDefinitions builds the five descriptors from the embedded
// schemas and validates every schema at startup. A schema error is fatal: no
// capability may start with a partially compiled validator.
func loadCompiledDefinitions() (map[string]*CompiledDefinition, error) {
	definitions := make(map[string]*CompiledDefinition, len(frozenDefinitions))
	for _, frozen := range frozenDefinitions {
		inputRaw, err := schemaFS.ReadFile("schemas/" + frozen.id + ".input.json")
		if err != nil {
			return nil, fmt.Errorf("read %s input schema: %w", frozen.id, err)
		}
		outputRaw, err := schemaFS.ReadFile("schemas/" + frozen.id + ".output.json")
		if err != nil {
			return nil, fmt.Errorf("read %s output schema: %w", frozen.id, err)
		}
		input, err := CompileSchema(inputRaw)
		if err != nil {
			return nil, fmt.Errorf("%s input schema: %w", frozen.id, err)
		}
		output, err := CompileSchema(outputRaw)
		if err != nil {
			return nil, fmt.Errorf("%s output schema: %w", frozen.id, err)
		}
		definitions[frozen.id] = &CompiledDefinition{
			Definition: Definition{
				CapabilityID:         frozen.id,
				ContractVersion:      ContractVersion,
				ProviderID:           frozen.provider,
				Description:          frozen.description,
				InputSchema:          append(json.RawMessage(nil), inputRaw...),
				OutputSchema:         append(json.RawMessage(nil), outputRaw...),
				RequiredScopes:       append([]string(nil), frozen.scopes...),
				SupportedOperations:  append([]string(nil), frozen.operations...),
				ExecutionConstraints: FrozenConstraints(),
				Enabled:              true,
			},
			Input:  input,
			Output: output,
		}
	}
	return definitions, nil
}

// FrozenDefinitions returns the five descriptors built from the embedded
// schemas with the frozen default limits. It is used by contract tests that
// compare runtime descriptors with the OpenAPI document.
func FrozenDefinitions() ([]Definition, error) {
	compiled, err := loadCompiledDefinitions()
	if err != nil {
		return nil, err
	}
	if err := validateFrozenSet(compiled); err != nil {
		return nil, err
	}
	out := make([]Definition, 0, len(compiled))
	for _, id := range sortedIDs(compiled) {
		out = append(out, compiled[id].Definition)
	}
	return out, nil
}

// validateFrozenSet enforces D06-02: exactly five IDs, frozen provider
// bindings, contract version, scopes, operations, and non-escalated limits.
func validateFrozenSet(definitions map[string]*CompiledDefinition) error {
	if len(definitions) != len(frozenDefinitions) {
		return fmt.Errorf("capability set has %d entries, want %d", len(definitions), len(frozenDefinitions))
	}
	for _, frozen := range frozenDefinitions {
		definition, ok := definitions[frozen.id]
		if !ok {
			return fmt.Errorf("capability %s is missing", frozen.id)
		}
		if definition.ContractVersion != ContractVersion {
			return fmt.Errorf("capability %s contract_version is %q", frozen.id, definition.ContractVersion)
		}
		if definition.ProviderID != frozen.provider {
			return fmt.Errorf("capability %s provider is %q", frozen.id, definition.ProviderID)
		}
		if strings.Join(definition.RequiredScopes, ",") != strings.Join(frozen.scopes, ",") {
			return fmt.Errorf("capability %s required_scopes drifted", frozen.id)
		}
		if strings.Join(definition.SupportedOperations, ",") != strings.Join(frozen.operations, ",") {
			return fmt.Errorf("capability %s supported_operations drifted", frozen.id)
		}
		if err := validateConstraints(definition.ExecutionConstraints, FrozenConstraints()); err != nil {
			return fmt.Errorf("capability %s: %w", frozen.id, err)
		}
	}
	return nil
}

func validateConstraints(actual, ceiling Constraints) error {
	if actual.TimeoutMS < 1 || actual.TimeoutMS > ceiling.TimeoutMS {
		return fmt.Errorf("timeout_ms must be 1..%d", ceiling.TimeoutMS)
	}
	if actual.MaxInputBytes < 1 || actual.MaxInputBytes > ceiling.MaxInputBytes {
		return fmt.Errorf("max_input_bytes must be 1..%d", ceiling.MaxInputBytes)
	}
	if actual.MaxOutputBytes < 1 || actual.MaxOutputBytes > ceiling.MaxOutputBytes {
		return fmt.Errorf("max_output_bytes must be 1..%d", ceiling.MaxOutputBytes)
	}
	return nil
}

func sortedIDs(definitions map[string]*CompiledDefinition) []string {
	ids := make([]string, 0, len(definitions))
	for id := range definitions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
