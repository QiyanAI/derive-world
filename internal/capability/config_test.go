// SPDX-License-Identifier: Apache-2.0
package capability

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func validConfigText(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "capabilities.yaml"))
	if err != nil {
		t.Fatalf("read capabilities.yaml: %v", err)
	}
	return string(raw)
}

func TestParseRepositoryCapabilities(t *testing.T) {
	config, err := ParseFileConfig([]byte(validConfigText(t)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if config.ContractVersion != ContractVersion || config.SHA256 == "" {
		t.Fatalf("config = %+v", config)
	}
	if len(config.Providers) != 1 || config.Providers[0].ProviderID != ProviderContentStructure {
		t.Fatalf("providers = %+v", config.Providers)
	}
	if config.Providers[0].BaseURL != "http://capability-provider:8081" {
		t.Fatalf("base_url = %q", config.Providers[0].BaseURL)
	}
	if config.Providers[0].Transport != TransportHTTPHMACV2 {
		t.Fatalf("transport = %q, want %q", config.Providers[0].Transport, TransportHTTPHMACV2)
	}
	for _, id := range FrozenIDs() {
		if !config.Enabled[id] {
			t.Fatalf("capability %s is not enabled", id)
		}
	}
	if _, err := NewRegistry(config); err != nil {
		t.Fatalf("registry: %v", err)
	}
}

func TestParseInlineCapabilities(t *testing.T) {
	text := strings.Replace(validConfigText(t), `  object.read:
    enabled: true`, `  object.read: { enabled: true }`, 1)
	config, err := ParseFileConfig([]byte(text))
	if err != nil {
		t.Fatalf("parse inline map: %v", err)
	}
	if !config.Enabled[IDObjectRead] {
		t.Fatal("inline map was not parsed")
	}
}

func TestConfigRejectsInvalidVariants(t *testing.T) {
	base := validConfigText(t)
	cases := map[string]string{
		"disabled capability": strings.Replace(base, "  object.derive:\n    enabled: true", "  object.derive:\n    enabled: false", 1),
		"missing capability":  strings.Replace(base, "  object.derive:\n    enabled: true\n", "", 1),
		"unknown capability":  strings.Replace(base, "  object.read:", "  object.unknown:", 1),
		"unknown root key":    base + "\nextra: true\n",
		"loopback base_url":   strings.Replace(base, "http://capability-provider:8081", "http://127.0.0.1:8081", 1),
		"metadata base_url":   strings.Replace(base, "http://capability-provider:8081", "http://169.254.169.254:8081", 1),
		"public http":         strings.Replace(base, "network: internal", "network: public", 1),
		"host not allowed":    strings.Replace(base, "http://capability-provider:8081", "http://other-provider:8081", 1),
		"uppercase host":      strings.Replace(base, "      - capability-provider", "      - Capability-Provider", 1),
		"timeout too high":    strings.Replace(base, "timeout_ms: 10000", "timeout_ms: 10001", 1),
		"input too high":      strings.Replace(base, "max_input_bytes: 65536", "max_input_bytes: 65537", 1),
		"wrong transport":     strings.Replace(base, "transport: http_hmac", "transport: in_process", 1),
		"query string":        strings.Replace(base, "http://capability-provider:8081", "http://capability-provider:8081?x=1", 1),
		"userinfo":            strings.Replace(base, "http://capability-provider:8081", "http://user:pass@capability-provider:8081", 1),
		"duplicate key":       base + "\ncontract_version: \"1.0\"\n",
		"yaml anchor":         strings.Replace(base, "network: internal", "network: &net internal", 1),
		"tab indentation":     strings.Replace(base, "  object.read:", "\tobject.read:", 1),
		"wrong contract":      strings.Replace(base, `contract_version: "1.0"`, `contract_version: "2.0"`, 1),
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseFileConfig([]byte(text)); err == nil {
				t.Fatalf("invalid configuration was accepted")
			}
		})
	}
}

func TestConfigAllowsLowerLimits(t *testing.T) {
	text := strings.Replace(validConfigText(t), "timeout_ms: 10000", "timeout_ms: 5000", 1)
	config, err := ParseFileConfig([]byte(text))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	registry, err := NewRegistry(config)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	if registry.definitions[IDContentStructure].ExecutionConstraints.TimeoutMS != 5000 {
		t.Fatal("lower timeout was not applied")
	}
}

func TestConfigMissingFileFails(t *testing.T) {
	if _, err := LoadFileConfig(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("missing file was accepted")
	}
	if _, err := LoadFileConfig(""); err == nil {
		t.Fatal("empty path was accepted")
	}
}

func TestProviderTransportV2AndTLSValidation(t *testing.T) {
	base := validConfigText(t)

	t.Run("public requires v2", func(t *testing.T) {
		text := strings.Replace(base, "transport: http_hmac_v2", "transport: http_hmac", 1)
		text = strings.Replace(text, "http://capability-provider:8081", "https://capability-provider:8081", 1)
		text = strings.Replace(text, "network: internal", "network: public", 1)
		if _, err := ParseFileConfig([]byte(text)); err == nil {
			t.Fatal("public provider without response signing was accepted")
		}
	})

	t.Run("tls settings require https", func(t *testing.T) {
		text := strings.Replace(base, "    max_output_bytes: 262144",
			"    max_output_bytes: 262144\n    tls_ca_file: ca.pem", 1)
		if _, err := ParseFileConfig([]byte(text)); err == nil || !strings.Contains(err.Error(), "https") {
			t.Fatalf("http provider with TLS settings error = %v", err)
		}
	})

	t.Run("client certificate requires key", func(t *testing.T) {
		text := strings.Replace(base, "http://capability-provider:8081", "https://capability-provider:8081", 1)
		text = strings.Replace(text, "    max_output_bytes: 262144",
			"    max_output_bytes: 262144\n    tls_client_cert_file: client.pem", 1)
		if _, err := ParseFileConfig([]byte(text)); err == nil || !strings.Contains(err.Error(), "tls_client_key_file") {
			t.Fatalf("missing client key error = %v", err)
		}
	})

	t.Run("public server name must match", func(t *testing.T) {
		text := strings.Replace(base, "http://capability-provider:8081", "https://provider.example.com:8081", 1)
		text = strings.Replace(text, "      - capability-provider", "      - provider.example.com", 1)
		text = strings.Replace(text, "network: internal", "network: public", 1)
		text = strings.Replace(text, "    max_output_bytes: 262144",
			"    max_output_bytes: 262144\n    tls_server_name: other.example.com", 1)
		if _, err := ParseFileConfig([]byte(text)); err == nil || !strings.Contains(err.Error(), "tls_server_name") {
			t.Fatalf("public mismatch error = %v", err)
		}
	})

	t.Run("internal https with private CA and client cert", func(t *testing.T) {
		text := strings.Replace(base, "http://capability-provider:8081", "https://capability-provider:8081", 1)
		text = strings.Replace(text, "    max_output_bytes: 262144",
			"    max_output_bytes: 262144\n    tls_ca_file: ca.pem\n    tls_server_name: provider.test\n    tls_client_cert_file: client.pem\n    tls_client_key_file: client-key.pem", 1)
		config, err := ParseFileConfig([]byte(text))
		if err != nil {
			t.Fatalf("valid TLS config rejected: %v", err)
		}
		provider := config.Providers[0]
		if provider.TLSCAFile != "ca.pem" || provider.TLSServerName != "provider.test" ||
			provider.TLSClientCertFile != "client.pem" || provider.TLSClientKeyFile != "client-key.pem" {
			t.Fatalf("TLS fields = %+v", provider)
		}
	})
}
