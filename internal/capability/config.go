// SPDX-License-Identifier: Apache-2.0
package capability

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// FileConfig is the validated capabilities.yaml document.
type FileConfig struct {
	ContractVersion string
	Providers       []ProviderConfig
	Enabled         map[string]bool
	SHA256          string
	Raw             []byte
}

// ProviderConfig is one external Provider entry. It is never returned by the
// public descriptor API.
type ProviderConfig struct {
	ProviderID        string
	Transport         string
	BaseURL           string
	Network           string
	AllowedHosts      []string
	TimeoutMS         int
	MaxInputBytes     int
	MaxOutputBytes    int
	TLSCAFile         string
	TLSServerName     string
	TLSClientCertFile string
	TLSClientKeyFile  string
	parsed            *url.URL
	tlsBaseDir        string
}

const maxConfigBytes = 1 << 20

var configTopKeys = map[string]bool{"contract_version": true, "providers": true, "capabilities": true}
var providerKeys = map[string]bool{
	"provider_id": true, "transport": true, "base_url": true, "network": true,
	"allowed_hosts": true, "timeout_ms": true, "max_input_bytes": true,
	"max_output_bytes": true,
	"tls_ca_file":      true, "tls_server_name": true,
	"tls_client_cert_file": true, "tls_client_key_file": true,
}

// LoadFileConfig reads, parses, and validates capabilities.yaml. Every
// disagreement with the frozen contract is a startup error (D06-19).
func LoadFileConfig(path string) (*FileConfig, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%w: CAPABILITIES_CONFIG is required", ErrConfigInvalid)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %v", ErrConfigInvalid, path, err)
	}
	if info.Size() > maxConfigBytes {
		return nil, fmt.Errorf("%w: %s exceeds %d bytes", ErrConfigInvalid, path, maxConfigBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %v", ErrConfigInvalid, path, err)
	}
	config, err := ParseFileConfig(raw)
	if err != nil {
		return nil, err
	}
	baseDir := filepath.Dir(path)
	for index := range config.Providers {
		config.Providers[index].tlsBaseDir = baseDir
	}
	return config, nil
}

// ParseFileConfig validates raw YAML bytes.
func ParseFileConfig(raw []byte) (*FileConfig, error) {
	parsed, err := parseYAML(raw)
	if err != nil {
		return nil, err
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: root must be a mapping", ErrConfigInvalid)
	}
	if err := requireExactKeys(root, configTopKeys, "root"); err != nil {
		return nil, err
	}
	version, err := requireString(root, "contract_version")
	if err != nil {
		return nil, err
	}
	if version != ContractVersion {
		return nil, fmt.Errorf("%w: contract_version must be %q", ErrConfigInvalid, ContractVersion)
	}
	providers, err := parseProviders(root["providers"])
	if err != nil {
		return nil, err
	}
	enabled, err := parseEnabled(root["capabilities"])
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	return &FileConfig{
		ContractVersion: version,
		Providers:       providers,
		Enabled:         enabled,
		SHA256:          hex.EncodeToString(digest[:]),
		Raw:             append([]byte(nil), raw...),
	}, nil
}

func parseProviders(value any) ([]ProviderConfig, error) {
	if value == nil {
		return nil, fmt.Errorf("%w: providers is required", ErrConfigInvalid)
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: providers must be a sequence", ErrConfigInvalid)
	}
	if len(items) != 1 {
		return nil, fmt.Errorf("%w: exactly one external provider is permitted", ErrConfigInvalid)
	}
	providers := make([]ProviderConfig, 0, 1)
	for index, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: providers[%d] must be a mapping", ErrConfigInvalid, index)
		}
		if err := requireAllowedKeys(object, providerKeys, fmt.Sprintf("providers[%d]", index)); err != nil {
			return nil, err
		}
		provider, err := parseProvider(object, index)
		if err != nil {
			return nil, err
		}
		providers = append(providers, *provider)
	}
	return providers, nil
}

func parseProvider(object map[string]any, index int) (*ProviderConfig, error) {
	providerID, err := requireString(object, "provider_id")
	if err != nil {
		return nil, err
	}
	if providerID != ProviderContentStructure {
		return nil, fmt.Errorf("%w: providers[%d].provider_id must be %q", ErrConfigInvalid, index, ProviderContentStructure)
	}
	transport, err := requireString(object, "transport")
	if err != nil {
		return nil, err
	}
	if transport != TransportHTTPHMAC && transport != TransportHTTPHMACV2 {
		return nil, fmt.Errorf("%w: providers[%d].transport must be %q or %q",
			ErrConfigInvalid, index, TransportHTTPHMAC, TransportHTTPHMACV2)
	}
	baseURL, err := requireString(object, "base_url")
	if err != nil {
		return nil, err
	}
	network, err := requireString(object, "network")
	if err != nil {
		return nil, err
	}
	if network != "internal" && network != "public" {
		return nil, fmt.Errorf("%w: providers[%d].network must be internal or public", ErrConfigInvalid, index)
	}
	allowedHosts, err := requireStringSlice(object, "allowed_hosts")
	if err != nil {
		return nil, err
	}
	if len(allowedHosts) == 0 {
		return nil, fmt.Errorf("%w: providers[%d].allowed_hosts must not be empty", ErrConfigInvalid, index)
	}
	seenHosts := map[string]bool{}
	for _, host := range allowedHosts {
		normalized := strings.ToLower(host)
		if normalized == "" || normalized != host {
			return nil, fmt.Errorf("%w: providers[%d].allowed_hosts must be lowercase without surrounding spaces", ErrConfigInvalid, index)
		}
		if strings.ContainsAny(normalized, "*/") || strings.Contains(normalized, "\\") {
			return nil, fmt.Errorf("%w: providers[%d].allowed_hosts does not support wildcards or paths", ErrConfigInvalid, index)
		}
		if ip := net.ParseIP(normalized); ip != nil {
			if err := validateLiteralAddress(ip, network); err != nil {
				return nil, fmt.Errorf("%w: providers[%d].allowed_hosts: %v", ErrConfigInvalid, index, err)
			}
		} else if !validDNSName(normalized) {
			return nil, fmt.Errorf("%w: providers[%d].allowed_hosts has an invalid DNS name", ErrConfigInvalid, index)
		}
		if seenHosts[normalized] {
			return nil, fmt.Errorf("%w: providers[%d].allowed_hosts has a duplicate", ErrConfigInvalid, index)
		}
		seenHosts[normalized] = true
	}
	parsed, err := parseProviderURL(baseURL, network, allowedHosts)
	if err != nil {
		return nil, fmt.Errorf("%w: providers[%d].base_url: %v", ErrConfigInvalid, index, err)
	}
	if network == "public" && transport != TransportHTTPHMACV2 {
		return nil, fmt.Errorf("%w: providers[%d]: public providers require transport %q",
			ErrConfigInvalid, index, TransportHTTPHMACV2)
	}
	tlsCAFile, err := optionalString(object, "tls_ca_file")
	if err != nil {
		return nil, fmt.Errorf("%w: providers[%d].%v", ErrConfigInvalid, index, err)
	}
	tlsServerName, err := optionalString(object, "tls_server_name")
	if err != nil {
		return nil, fmt.Errorf("%w: providers[%d].%v", ErrConfigInvalid, index, err)
	}
	tlsClientCert, err := optionalString(object, "tls_client_cert_file")
	if err != nil {
		return nil, fmt.Errorf("%w: providers[%d].%v", ErrConfigInvalid, index, err)
	}
	tlsClientKey, err := optionalString(object, "tls_client_key_file")
	if err != nil {
		return nil, fmt.Errorf("%w: providers[%d].%v", ErrConfigInvalid, index, err)
	}
	if (tlsClientCert == "") != (tlsClientKey == "") {
		return nil, fmt.Errorf("%w: providers[%d]: tls_client_cert_file and tls_client_key_file must be set together", ErrConfigInvalid, index)
	}
	if parsed.Scheme == "http" && (tlsCAFile != "" || tlsServerName != "" || tlsClientCert != "" || tlsClientKey != "") {
		return nil, fmt.Errorf("%w: providers[%d]: TLS settings require an https base_url", ErrConfigInvalid, index)
	}
	if tlsServerName != "" {
		if !validDNSName(tlsServerName) {
			return nil, fmt.Errorf("%w: providers[%d].tls_server_name is not a valid DNS name", ErrConfigInvalid, index)
		}
		if network == "public" && !strings.EqualFold(tlsServerName, parsed.Hostname()) {
			return nil, fmt.Errorf("%w: providers[%d].tls_server_name must match the public base_url host", ErrConfigInvalid, index)
		}
	}
	timeoutMS, err := requireInt(object, "timeout_ms")
	if err != nil {
		return nil, err
	}
	maxInput, err := requireInt(object, "max_input_bytes")
	if err != nil {
		return nil, err
	}
	maxOutput, err := requireInt(object, "max_output_bytes")
	if err != nil {
		return nil, err
	}
	constraints := Constraints{TimeoutMS: timeoutMS, MaxInputBytes: maxInput, MaxOutputBytes: maxOutput}
	if err := validateConstraints(constraints, FrozenConstraints()); err != nil {
		return nil, fmt.Errorf("%w: providers[%d]: %v", ErrConfigInvalid, index, err)
	}
	return &ProviderConfig{
		ProviderID: providerID, Transport: transport, BaseURL: baseURL,
		Network: network, AllowedHosts: allowedHosts,
		TimeoutMS: timeoutMS, MaxInputBytes: maxInput, MaxOutputBytes: maxOutput,
		TLSCAFile: tlsCAFile, TLSServerName: tlsServerName,
		TLSClientCertFile: tlsClientCert, TLSClientKeyFile: tlsClientKey,
		parsed: parsed,
	}, nil
}

// requireAllowedKeys rejects unknown keys while allowing optional ones.
func requireAllowedKeys(object map[string]any, allowed map[string]bool, where string) error {
	for key := range object {
		if !allowed[key] {
			return fmt.Errorf("%w: %s has unknown key %q", ErrConfigInvalid, where, key)
		}
	}
	return nil
}

// optionalString accepts an absent key but rejects an empty or non-string
// value when present.
func optionalString(object map[string]any, key string) (string, error) {
	value, ok := object[key]
	if !ok {
		return "", nil
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%s must be a non-empty string when set", key)
	}
	return text, nil
}

func parseEnabled(value any) (map[string]bool, error) {
	if value == nil {
		return nil, fmt.Errorf("%w: capabilities is required", ErrConfigInvalid)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: capabilities must be a mapping", ErrConfigInvalid)
	}
	frozen := map[string]bool{}
	for _, id := range FrozenIDs() {
		frozen[id] = true
	}
	for key := range object {
		if !frozen[key] {
			return nil, fmt.Errorf("%w: unknown capability %q", ErrConfigInvalid, key)
		}
	}
	enabled := map[string]bool{}
	for _, id := range FrozenIDs() {
		entry, ok := object[id]
		if !ok {
			return nil, fmt.Errorf("%w: capability %q is missing", ErrConfigInvalid, id)
		}
		entryObject, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: capability %q must be a mapping", ErrConfigInvalid, id)
		}
		if err := requireExactKeys(entryObject, map[string]bool{"enabled": true}, "capabilities."+id); err != nil {
			return nil, err
		}
		value, ok := entryObject["enabled"].(bool)
		if !ok {
			return nil, fmt.Errorf("%w: capability %q enabled must be boolean", ErrConfigInvalid, id)
		}
		if !value {
			return nil, fmt.Errorf("%w: capability %q must be enabled", ErrConfigInvalid, id)
		}
		enabled[id] = value
	}
	return enabled, nil
}

func requireExactKeys(object map[string]any, allowed map[string]bool, where string) error {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !allowed[key] {
			return fmt.Errorf("%w: %s has unknown key %q", ErrConfigInvalid, where, key)
		}
	}
	for key := range allowed {
		if _, ok := object[key]; !ok {
			return fmt.Errorf("%w: %s is missing %q", ErrConfigInvalid, where, key)
		}
	}
	return nil
}

func requireString(object map[string]any, key string) (string, error) {
	value, ok := object[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%w: %s must be a non-empty string", ErrConfigInvalid, key)
	}
	return value, nil
}

func requireStringSlice(object map[string]any, key string) ([]string, error) {
	values, ok := object[key].([]any)
	if !ok {
		return nil, fmt.Errorf("%w: %s must be a sequence", ErrConfigInvalid, key)
	}
	out := make([]string, 0, len(values))
	for _, item := range values {
		text, ok := item.(string)
		if !ok || text == "" {
			return nil, fmt.Errorf("%w: %s must contain strings", ErrConfigInvalid, key)
		}
		out = append(out, text)
	}
	return out, nil
}

func requireInt(object map[string]any, key string) (int, error) {
	switch value := object[key].(type) {
	case int64:
		if value < 0 || value > 1<<31-1 {
			return 0, fmt.Errorf("%w: %s is out of range", ErrConfigInvalid, key)
		}
		return int(value), nil
	case int:
		return value, nil
	default:
		return 0, fmt.Errorf("%w: %s must be an integer", ErrConfigInvalid, key)
	}
}

func parseProviderURL(raw, network string, allowedHosts []string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("scheme must be http or https")
	}
	if network == "public" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("public providers require https")
	}
	if network == "internal" && parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("scheme must be http or https")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("userinfo is not allowed")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("query strings and fragments are not allowed")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return nil, fmt.Errorf("only scheme://host[:port] is allowed")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return nil, fmt.Errorf("host is required")
	}
	matched := false
	for _, allowed := range allowedHosts {
		if strings.EqualFold(allowed, host) {
			matched = true
			break
		}
	}
	if !matched {
		return nil, fmt.Errorf("host is not in allowed_hosts")
	}
	if parsed.Port() != "" {
		port, err := strconv.Atoi(parsed.Port())
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("port is invalid")
		}
	}
	if ip := net.ParseIP(host); ip != nil {
		if err := validateLiteralAddress(ip, network); err != nil {
			return nil, err
		}
	}
	return parsed, nil
}

func validDNSName(host string) bool {
	if len(host) < 1 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) < 1 || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
}

// applyFileConfig creates the compiled definitions for this deployment. All
// five capabilities are present and enabled; content.structure inherits the
// configured (never raised) provider limits.
func applyFileConfig(compiled map[string]*CompiledDefinition, config *FileConfig) {
	for _, id := range FrozenIDs() {
		if enabled, ok := config.Enabled[id]; ok {
			compiled[id].Enabled = enabled
		}
	}
	for index := range config.Providers {
		provider := &config.Providers[index]
		if provider.ProviderID == ProviderContentStructure {
			compiled[IDContentStructure].ExecutionConstraints = Constraints{
				TimeoutMS: provider.TimeoutMS, MaxInputBytes: provider.MaxInputBytes,
				MaxOutputBytes: provider.MaxOutputBytes,
			}
		}
	}
}
