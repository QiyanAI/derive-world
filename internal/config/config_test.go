// SPDX-License-Identifier: Apache-2.0
package config

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

func validEnv() map[string]string {
	return map[string]string{
		"APP_ENV":                        "test",
		"PUBLIC_BASE_URL":                "http://127.0.0.1:8080",
		"BIND_ADDR":                      "127.0.0.1:8080",
		"DATABASE_URL":                   "postgres://derive:dw_local_only@127.0.0.1:5432/derive_world?sslmode=disable",
		"WEB_DIST_DIR":                   "./web/dist",
		"CURSOR_HMAC_SECRET":             "0123456789abcdef0123456789abcdef",
		"IP_HASH_PEPPER":                 "abcdef0123456789abcdef0123456789",
		"CAPABILITY_HMAC_SECRET":         "fedcba9876543210fedcba9876543210",
		"CAPABILITIES_CONFIG":            "./capabilities.yaml",
		"MODEL_MODE":                     "fixture",
		"MODEL_BASE_URL":                 "",
		"MODEL_API_KEY":                  "",
		"MODEL_NAME":                     "",
		"MODEL_TIMEOUT":                  "60s",
		"MODEL_MAX_OUTPUT_TOKENS":        "2048",
		"AI_INCLUDE_PRIVATE_CONTEXT":     "true",
		"CORS_ALLOWED_ORIGINS":           "",
		"LOG_LEVEL":                      "info",
		"LOG_FORMAT":                     "json",
		"TRUST_PROXY_HEADERS":            "false",
		"RATE_LIMIT_AUTH_PER_MIN":        "10",
		"RATE_LIMIT_API_PER_MIN":         "120",
		"RATE_LIMIT_AI_PER_HOUR":         "20",
		"RATE_LIMIT_CAPABILITY_PER_HOUR": "60",
		"SESSION_ABSOLUTE_TTL":           "168h",
		"SESSION_IDLE_TTL":               "24h",
		"MAX_REQUEST_BYTES":              "1048576",
		"AUDIT_RETENTION_DAYS":           "90",
		"CLEANUP_INTERVAL":               "1h",
		"SHUTDOWN_TIMEOUT":               "15s",
		"GITHUB_REPO":                    "QiyanAI/derive-world",
		"METRICS_TOKEN":                  "",
	}
}

func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	}
}

func TestLoadValidFixture(t *testing.T) {
	cfg, err := LoadFrom(lookupFrom(validEnv()))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.AppEnv != "test" || cfg.Model.Mode != "fixture" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.Model.Timeout.String() != "1m0s" {
		t.Fatalf("model timeout = %s, want 1m0s", cfg.Model.Timeout)
	}
}

func TestRequiresAppEnv(t *testing.T) {
	env := validEnv()
	delete(env, "APP_ENV")
	if _, err := LoadFrom(lookupFrom(env)); err == nil || !strings.Contains(err.Error(), "APP_ENV") {
		t.Fatalf("expected APP_ENV error, got %v", err)
	}
}

func TestRequiresModelMode(t *testing.T) {
	env := validEnv()
	delete(env, "MODEL_MODE")
	if _, err := LoadFrom(lookupFrom(env)); err == nil || !strings.Contains(err.Error(), "MODEL_MODE") {
		t.Fatalf("expected MODEL_MODE error, got %v", err)
	}
}

func TestProductionRequiresHTTPSPublicBaseURL(t *testing.T) {
	env := validEnv()
	env["APP_ENV"] = "production"
	env["PUBLIC_BASE_URL"] = "http://example.com"
	env["DATABASE_URL"] = "postgres://user:realpass@db:5432/derive?sslmode=require"
	if _, err := LoadFrom(lookupFrom(env)); err == nil || !strings.Contains(err.Error(), "PUBLIC_BASE_URL") {
		t.Fatalf("expected PUBLIC_BASE_URL error, got %v", err)
	}
}

func TestProductionRejectsDevSecrets(t *testing.T) {
	env := validEnv()
	env["APP_ENV"] = "production"
	env["PUBLIC_BASE_URL"] = "https://example.com"
	env["DATABASE_URL"] = "postgres://user:realpass@db:5432/derive?sslmode=require"
	env["CURSOR_HMAC_SECRET"] = "dw_dev_only_cursor_secret_000000000000"
	if _, err := LoadFrom(lookupFrom(env)); err == nil || !strings.Contains(err.Error(), "CURSOR_HMAC_SECRET") {
		t.Fatalf("expected CURSOR_HMAC_SECRET error, got %v", err)
	}
}

func TestRealModeRequiresModelSettings(t *testing.T) {
	env := validEnv()
	env["MODEL_MODE"] = "real"
	_, err := LoadFrom(lookupFrom(env))
	if err == nil {
		t.Fatal("expected real-mode validation error")
	}
	for _, name := range []string{"MODEL_BASE_URL", "MODEL_API_KEY", "MODEL_NAME"} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("error %q does not mention %s", err, name)
		}
	}
}

func TestModelBaseURLNormalization(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"https://api.example.com", "https://api.example.com"},
		{"https://api.example.com/", "https://api.example.com"},
		{"https://api.example.com/v1", "https://api.example.com"},
		{"https://api.example.com/v1/", "https://api.example.com"},
		{"http://host.docker.internal:11434/", "http://host.docker.internal:11434"},
	}
	for _, tc := range cases {
		got, err := normalizeModelBaseURL(tc.raw)
		if err != nil {
			t.Fatalf("normalizeModelBaseURL(%q) error = %v", tc.raw, err)
		}
		if got != tc.want {
			t.Fatalf("normalizeModelBaseURL(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestCORSRejectsWildcard(t *testing.T) {
	env := validEnv()
	env["CORS_ALLOWED_ORIGINS"] = "*"
	if _, err := LoadFrom(lookupFrom(env)); err == nil || !strings.Contains(err.Error(), "CORS_ALLOWED_ORIGINS") {
		t.Fatalf("expected CORS error, got %v", err)
	}
}

func TestValidationErrorDoesNotLeakSecret(t *testing.T) {
	env := validEnv()
	secret := "short-secret-value"
	env["CURSOR_HMAC_SECRET"] = secret
	_, err := LoadFrom(lookupFrom(env))
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("validation error leaked secret value: %v", err)
	}
}

func TestEnvExampleKeys(t *testing.T) {
	file, err := os.Open("../../.env.example")
	if err != nil {
		t.Fatalf("open .env.example: %v", err)
	}
	defer file.Close()

	example := map[string]bool{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		example[strings.TrimSpace(key)] = true
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read .env.example: %v", err)
	}

	canonical := map[string]bool{}
	for _, key := range EnvKeys() {
		canonical[key] = true
		if !example[key] {
			t.Errorf(".env.example is missing %s", key)
		}
	}
	for key := range example {
		if !canonical[key] {
			t.Errorf(".env.example has unknown key %s", key)
		}
	}
}

func TestModelBudgetDefaults(t *testing.T) {
	cfg, err := LoadFrom(lookupFrom(validEnv()))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Model.MaxInputChars != 65536 ||
		cfg.Model.DailyCallsPerUser != 100 ||
		cfg.Model.DailyInputCharsPerUser != 200000 ||
		cfg.Model.ConcurrencyPerUser != 2 {
		t.Fatalf("unexpected budget defaults: %+v", cfg.Model)
	}
	if cfg.Model.GlobalDailyCalls != 0 {
		t.Fatalf("fixture global cap = %d, want 0 (unlimited)", cfg.Model.GlobalDailyCalls)
	}
	if cfg.CapabilityHMACKeyID != "k1" {
		t.Fatalf("key id = %q, want k1", cfg.CapabilityHMACKeyID)
	}
}

func TestRealModeForcesGlobalCallCap(t *testing.T) {
	env := validEnv()
	env["MODEL_MODE"] = "real"
	env["MODEL_BASE_URL"] = "https://api.example.com"
	env["MODEL_API_KEY"] = "test-key"
	env["MODEL_NAME"] = "test-model"
	env["MODEL_GLOBAL_DAILY_CALLS"] = "0"
	cfg, err := LoadFrom(lookupFrom(env))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Model.GlobalDailyCalls != 200 {
		t.Fatalf("real global cap = %d, want 200", cfg.Model.GlobalDailyCalls)
	}
}

func TestModelBudgetBounds(t *testing.T) {
	cases := map[string]string{
		"MODEL_MAX_INPUT_CHARS":            "65537",
		"MODEL_MAX_OUTPUT_TOKENS":          "8193",
		"MODEL_CONCURRENCY_PER_USER":       "17",
		"MODEL_DAILY_CALLS_PER_USER":       "0",
		"MODEL_DAILY_INPUT_CHARS_PER_USER": "10",
	}
	for key, value := range cases {
		t.Run(key, func(t *testing.T) {
			env := validEnv()
			env[key] = value
			if _, err := LoadFrom(lookupFrom(env)); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("%s=%s error = %v", key, value, err)
			}
		})
	}
}

func TestCapabilityKeyIDValidation(t *testing.T) {
	for _, value := range []string{"K1", "k 1", "-k1", "k1-", "abcdefghijklmnopq"} {
		env := validEnv()
		env["CAPABILITY_HMAC_KEY_ID"] = value
		if _, err := LoadFrom(lookupFrom(env)); err == nil || !strings.Contains(err.Error(), "CAPABILITY_HMAC_KEY_ID") {
			t.Fatalf("key id %q accepted: %v", value, err)
		}
	}
	env := validEnv()
	env["CAPABILITY_HMAC_KEY_ID"] = "k2-rotate"
	if _, err := LoadFrom(lookupFrom(env)); err != nil {
		t.Fatalf("valid key id rejected: %v", err)
	}
}

func TestModelKillSwitch(t *testing.T) {
	env := validEnv()
	env["MODEL_KILL_SWITCH"] = "true"
	cfg, err := LoadFrom(lookupFrom(env))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if !cfg.Model.KillSwitch {
		t.Fatal("kill switch was not parsed")
	}
}

func TestModelAdapterProfileDefaultsAndOverrides(t *testing.T) {
	cfg, err := LoadFrom(lookupFrom(validEnv()))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Model.ChatPath != "/v1/chat/completions" ||
		cfg.Model.ResponseFormat != "" || cfg.Model.ThinkingMode != "" {
		t.Fatalf("adapter defaults = %+v", cfg.Model)
	}

	env := validEnv()
	env["MODEL_CHAT_PATH"] = "/chat/completions"
	env["MODEL_RESPONSE_FORMAT"] = "json_object"
	env["MODEL_THINKING_MODE"] = "disabled"
	cfg, err = LoadFrom(lookupFrom(env))
	if err != nil {
		t.Fatalf("LoadFrom overrides: %v", err)
	}
	if cfg.Model.ChatPath != "/chat/completions" ||
		cfg.Model.ResponseFormat != "json_object" ||
		cfg.Model.ThinkingMode != "disabled" {
		t.Fatalf("adapter overrides = %+v", cfg.Model)
	}
}

func TestModelAdapterProfileRejectsInvalidValues(t *testing.T) {
	cases := map[string]string{
		"MODEL_CHAT_PATH":       "//evil.example/path",
		"MODEL_RESPONSE_FORMAT": "json_schema",
		"MODEL_THINKING_MODE":   "enabled",
	}
	for key, value := range cases {
		t.Run(key, func(t *testing.T) {
			env := validEnv()
			env[key] = value
			if _, err := LoadFrom(lookupFrom(env)); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("%s=%s error = %v", key, value, err)
			}
		})
	}
}
