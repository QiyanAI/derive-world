// SPDX-License-Identifier: Apache-2.0
package capability

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/audit"
)

// Registry is the immutable, config-driven capability registry built at
// startup. It is the only source of descriptors in the running process.
type Registry struct {
	definitions map[string]*CompiledDefinition
	providers   map[string]*ProviderConfig
	order       []string
}

// NewRegistry validates the frozen set against the deployment configuration.
func NewRegistry(config *FileConfig) (*Registry, error) {
	if config == nil {
		return nil, fmt.Errorf("%w: configuration is required", ErrConfigInvalid)
	}
	if config.ContractVersion != ContractVersion {
		return nil, fmt.Errorf("%w: contract_version must be %q", ErrConfigInvalid, ContractVersion)
	}
	compiled, err := loadCompiledDefinitions()
	if err != nil {
		return nil, err
	}
	applyFileConfig(compiled, config)
	if err := validateFrozenSet(compiled); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConfigInvalid, err)
	}
	providers := make(map[string]*ProviderConfig, len(config.Providers))
	for index := range config.Providers {
		provider := &config.Providers[index]
		if _, exists := providers[provider.ProviderID]; exists {
			return nil, fmt.Errorf("%w: duplicate provider %q", ErrConfigInvalid, provider.ProviderID)
		}
		providers[provider.ProviderID] = provider
	}
	for id, definition := range compiled {
		if definition.ProviderID == ProviderTrustedCore {
			continue
		}
		if _, ok := providers[definition.ProviderID]; !ok {
			return nil, fmt.Errorf("%w: capability %s has no configured provider", ErrConfigInvalid, id)
		}
	}
	return &Registry{definitions: compiled, providers: providers, order: sortedIDs(compiled)}, nil
}

// Definition returns one compiled definition.
func (r *Registry) Definition(capabilityID string) (*CompiledDefinition, bool) {
	definition, ok := r.definitions[capabilityID]
	return definition, ok
}

// Provider returns the external provider configuration.
func (r *Registry) Provider(providerID string) (*ProviderConfig, bool) {
	provider, ok := r.providers[providerID]
	return provider, ok
}

// Definitions returns a stable-order copy of all descriptors.
func (r *Registry) Definitions() []Definition {
	out := make([]Definition, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.definitions[id].Definition)
	}
	return out
}

// EnabledDefinitions returns descriptors with enabled=true.
func (r *Registry) EnabledDefinitions() []Definition {
	out := make([]Definition, 0, len(r.order))
	for _, id := range r.order {
		if r.definitions[id].Enabled {
			out = append(out, r.definitions[id].Definition)
		}
	}
	return out
}

func (r *Registry) ids() []string {
	out := append([]string(nil), r.order...)
	sort.Strings(out)
	return out
}

// syncRegistry upserts the five descriptors and disables any historical
// definition not present in the frozen set. It runs in one transaction with
// its audit row (D06-19).
func syncRegistry(ctx context.Context, tx pgx.Tx, auditWriter *audit.Writer, registry *Registry, configSHA256 string) error {
	ids := registry.ids()
	for _, id := range ids {
		definition := registry.definitions[id]
		scopes := pgTextArray(definition.RequiredScopes)
		operations := pgTextArray(definition.SupportedOperations)
		constraints, err := json.Marshal(definition.ExecutionConstraints)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
INSERT INTO capability_definitions (
    id, contract_version, provider_id, description, input_schema, output_schema,
    required_scopes, supported_operations, execution_constraints, enabled, synced_at
) VALUES ($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9::jsonb,$10, now())
ON CONFLICT (id) DO UPDATE SET
    contract_version = EXCLUDED.contract_version,
    provider_id = EXCLUDED.provider_id,
    description = EXCLUDED.description,
    input_schema = EXCLUDED.input_schema,
    output_schema = EXCLUDED.output_schema,
    required_scopes = EXCLUDED.required_scopes,
    supported_operations = EXCLUDED.supported_operations,
    execution_constraints = EXCLUDED.execution_constraints,
    enabled = EXCLUDED.enabled,
    synced_at = now()`,
			id, definition.ContractVersion, definition.ProviderID, definition.Description,
			string(definition.InputSchema), string(definition.OutputSchema),
			scopes, operations, string(constraints), definition.Enabled)
		if err != nil {
			return fmt.Errorf("sync capability %s: %w", id, err)
		}
	}
	if _, err := tx.Exec(ctx, `
UPDATE capability_definitions
   SET enabled = false, synced_at = now()
 WHERE NOT (id = ANY($1)) AND enabled = true`, pgTextArray(ids)); err != nil {
		return err
	}
	enabledCount := 0
	for _, id := range ids {
		if registry.definitions[id].Enabled {
			enabledCount++
		}
	}
	if err := auditWriter.Write(ctx, tx, audit.Event{
		Actor:        audit.SystemActor(),
		Action:       audit.ActionCapabilityDefSynced,
		ResourceType: "capability_registry",
		Decision:     "allow",
		RequestID:    "system-capability-sync",
		Metadata: map[string]any{
			"contract_version": ContractVersion,
			"config_sha256":    configSHA256,
			"enabled_count":    enabledCount,
		},
	}); err != nil {
		return err
	}
	return nil
}

// pgTextArray renders a text[] literal without relying on driver-specific
// array encoding. Every element is validated by the frozen caller.
func pgTextArray(values []string) string {
	// Build the literal explicitly with quoted elements. Values are frozen
	// identifiers and contain no quotes.
	out := "{"
	for index, value := range values {
		if index > 0 {
			out += ","
		}
		out += `"` + value + `"`
	}
	out += "}"
	return out
}

// definitionsFromDB reads the authoritative synced rows for the list endpoint.
func definitionsFromDB(ctx context.Context, q interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}, enabledOnly bool) ([]Definition, error) {
	query := `
SELECT id, contract_version, provider_id, description, input_schema, output_schema,
       required_scopes, supported_operations, execution_constraints, enabled
  FROM capability_definitions`
	if enabledOnly {
		query += ` WHERE enabled = true`
	}
	query += ` ORDER BY id`
	rows, err := q.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Definition, 0, len(frozenDefinitions))
	for rows.Next() {
		var (
			item        Definition
			input       []byte
			output      []byte
			constraints []byte
		)
		if err := rows.Scan(&item.CapabilityID, &item.ContractVersion, &item.ProviderID,
			&item.Description, &input, &output, &item.RequiredScopes,
			&item.SupportedOperations, &constraints, &item.Enabled); err != nil {
			return nil, err
		}
		item.InputSchema = append(json.RawMessage(nil), input...)
		item.OutputSchema = append(json.RawMessage(nil), output...)
		if err := json.Unmarshal(constraints, &item.ExecutionConstraints); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
