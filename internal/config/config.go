// SPDX-License-Identifier: Apache-2.0

// Package config loads and validates environment configuration.
//
// Configuration comes only from environment variables (D08 §3.5.4). Validation
// happens once at startup. Error messages identify variable names, never values.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Model budget defaults mirror the frozen A.9.3 constants. The config package
// keeps its own copies so it continues to depend only on the standard library.
const (
	defaultModelDailyCalls      = 100
	defaultModelDailyInputChars = 200_000
	defaultModelConcurrency     = 2
	defaultModelMaxInputChars   = 64 * 1024
	defaultRealGlobalDailyCalls = 200
	maxModelOutputTokens        = 8192
)

// EnvKeys is the canonical environment variable set frozen in D08 §3.5.4.
func EnvKeys() []string {
	return []string{
		"APP_ENV",
		"PUBLIC_BASE_URL",
		"BIND_ADDR",
		"DATABASE_URL",
		"WEB_DIST_DIR",
		"CURSOR_HMAC_SECRET",
		"IP_HASH_PEPPER",
		"CAPABILITY_HMAC_SECRET",
		"CAPABILITY_HMAC_KEY_ID",
		"CAPABILITIES_CONFIG",
		"MODEL_MODE",
		"MODEL_BASE_URL",
		"MODEL_API_KEY",
		"MODEL_NAME",
		"MODEL_TIMEOUT",
		"MODEL_CHAT_PATH",
		"MODEL_RESPONSE_FORMAT",
		"MODEL_THINKING_MODE",
		"MODEL_MAX_OUTPUT_TOKENS",
		"MODEL_MAX_INPUT_CHARS",
		"MODEL_DAILY_CALLS_PER_USER",
		"MODEL_DAILY_INPUT_CHARS_PER_USER",
		"MODEL_CONCURRENCY_PER_USER",
		"MODEL_GLOBAL_DAILY_CALLS",
		"MODEL_GLOBAL_DAILY_INPUT_CHARS",
		"MODEL_KILL_SWITCH",
		"AI_INCLUDE_PRIVATE_CONTEXT",
		"CORS_ALLOWED_ORIGINS",
		"LOG_LEVEL",
		"LOG_FORMAT",
		"TRUST_PROXY_HEADERS",
		"RATE_LIMIT_AUTH_PER_MIN",
		"RATE_LIMIT_API_PER_MIN",
		"RATE_LIMIT_AI_PER_HOUR",
		"RATE_LIMIT_CAPABILITY_PER_HOUR",
		"SESSION_ABSOLUTE_TTL",
		"SESSION_IDLE_TTL",
		"MAX_REQUEST_BYTES",
		"AUDIT_RETENTION_DAYS",
		"CLEANUP_INTERVAL",
		"SHUTDOWN_TIMEOUT",
		"GITHUB_REPO",
		"METRICS_TOKEN",
	}
}

// ModelConfig is the model-adapter configuration.
type ModelConfig struct {
	Mode                   string
	BaseURL                string
	APIKey                 string
	Name                   string
	Timeout                time.Duration
	ChatPath               string
	ResponseFormat         string
	ThinkingMode           string
	MaxOutputTokens        int
	MaxInputChars          int
	DailyCallsPerUser      int
	DailyInputCharsPerUser int
	ConcurrencyPerUser     int
	GlobalDailyCalls       int
	GlobalDailyInputChars  int
	KillSwitch             bool
	IncludePrivateContext  bool
}

// Config is the validated application configuration.
type Config struct {
	AppEnv               string
	PublicBaseURL        string
	BindAddr             string
	DatabaseURL          string
	WebDistDir           string
	CursorHMACSecret     string
	IPHashPepper         string
	CapabilityHMACSecret string
	CapabilityHMACKeyID  string
	CapabilitiesConfig   string
	Model                ModelConfig
	CORSAllowedOrigins   []string
	LogLevel             string
	LogFormat            string
	TrustProxyHeaders    bool
	RateLimitAuthPerMin  int
	RateLimitAPIPerMin   int
	RateLimitAIPerHour   int
	RateLimitCapPerHour  int
	SessionAbsoluteTTL   time.Duration
	SessionIdleTTL       time.Duration
	MaxRequestBytes      int64
	AuditRetentionDays   int
	CleanupInterval      time.Duration
	ShutdownTimeout      time.Duration
	GitHubRepo           string
	MetricsToken         string
}

// ValidationError reports startup configuration problems by variable name.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "invalid configuration: " + strings.Join(e.Problems, "; ")
}

// Load reads configuration from the process environment.
func Load() (*Config, error) {
	return LoadFrom(os.LookupEnv)
}

// LoadFrom reads configuration from an injectable lookup function.
func LoadFrom(lookup func(string) (string, bool)) (*Config, error) {
	get := func(key string) string {
		value, ok := lookup(key)
		if !ok {
			return ""
		}
		return strings.TrimSpace(value)
	}

	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	appEnv := get("APP_ENV")
	if appEnv == "" {
		add("APP_ENV is required")
	} else if appEnv != "development" && appEnv != "test" && appEnv != "production" {
		add("APP_ENV must be development, test, or production")
	}
	production := appEnv == "production"

	publicBaseURL := get("PUBLIC_BASE_URL")
	if publicBaseURL == "" {
		if production {
			add("PUBLIC_BASE_URL is required in production")
		} else {
			publicBaseURL = "http://127.0.0.1:8080"
		}
	}
	if publicBaseURL != "" {
		if err := validateHTTPURL("PUBLIC_BASE_URL", publicBaseURL, production); err != nil {
			add("%s", err)
		}
	}

	bindAddr := get("BIND_ADDR")
	if bindAddr == "" {
		bindAddr = "127.0.0.1:8080"
	}

	databaseURL := get("DATABASE_URL")
	if databaseURL == "" {
		add("DATABASE_URL is required")
	} else if production {
		if strings.Contains(databaseURL, "sslmode=disable") {
			add("DATABASE_URL must not use sslmode=disable in production")
		}
		if strings.Contains(databaseURL, "dw_local_only") {
			add("DATABASE_URL must not use the local demo password in production")
		}
	}

	webDistDir := get("WEB_DIST_DIR")
	if webDistDir == "" {
		webDistDir = "./web/dist"
	}

	cursorSecret := get("CURSOR_HMAC_SECRET")
	ipPepper := get("IP_HASH_PEPPER")
	capabilitySecret := get("CAPABILITY_HMAC_SECRET")
	requireSecret := func(name, value string) {
		if value == "" {
			add("%s is required", name)
			return
		}
		if len([]byte(value)) < 32 {
			add("%s must be at least 32 bytes", name)
		}
		if production && strings.HasPrefix(value, "dw_dev_only_") {
			add("%s must not use a dw_dev_only_ value in production", name)
		}
	}
	requireSecret("CURSOR_HMAC_SECRET", cursorSecret)
	requireSecret("IP_HASH_PEPPER", ipPepper)
	requireSecret("CAPABILITY_HMAC_SECRET", capabilitySecret)
	capabilityKeyID := get("CAPABILITY_HMAC_KEY_ID")
	if capabilityKeyID == "" {
		capabilityKeyID = "k1"
	} else if !validKeyID(capabilityKeyID) {
		add("CAPABILITY_HMAC_KEY_ID must be 1..16 lowercase letters, digits, or hyphens and start with a letter or digit")
	}

	capabilitiesConfig := get("CAPABILITIES_CONFIG")
	if capabilitiesConfig == "" {
		capabilitiesConfig = "./capabilities.yaml"
	}

	modelMode := get("MODEL_MODE")
	if modelMode == "" {
		add("MODEL_MODE is required")
	} else if modelMode != "fixture" && modelMode != "real" {
		add("MODEL_MODE must be fixture or real")
	}

	modelBaseURL := get("MODEL_BASE_URL")
	normalizedBaseURL := ""
	if modelBaseURL != "" {
		normalized, err := normalizeModelBaseURL(modelBaseURL)
		if err != nil {
			add("MODEL_BASE_URL %s", err)
		} else {
			normalizedBaseURL = normalized
			if production {
				if err := validateHTTPURL("MODEL_BASE_URL", normalized, true); err != nil {
					add("%s", err)
				}
			}
		}
	}
	modelAPIKey := get("MODEL_API_KEY")
	modelName := get("MODEL_NAME")
	if modelMode == "real" {
		if normalizedBaseURL == "" {
			add("MODEL_BASE_URL is required when MODEL_MODE=real")
		}
		if modelAPIKey == "" {
			add("MODEL_API_KEY is required when MODEL_MODE=real")
		}
		if modelName == "" {
			add("MODEL_NAME is required when MODEL_MODE=real")
		}
	}

	modelTimeout, err := durationValue(get("MODEL_TIMEOUT"), 60*time.Second)
	if err != nil {
		add("MODEL_TIMEOUT %s", err)
	}
	modelChatPath := get("MODEL_CHAT_PATH")
	if modelChatPath == "" {
		modelChatPath = "/v1/chat/completions"
	} else if !validModelChatPath(modelChatPath) {
		add("MODEL_CHAT_PATH must be an absolute path without host, query, fragment, traversal, or whitespace")
	}
	modelResponseFormat := get("MODEL_RESPONSE_FORMAT")
	if modelResponseFormat != "" && modelResponseFormat != "json_object" {
		add("MODEL_RESPONSE_FORMAT must be empty or json_object")
	}
	modelThinkingMode := get("MODEL_THINKING_MODE")
	switch modelThinkingMode {
	case "", "default", "disabled":
	default:
		add("MODEL_THINKING_MODE must be empty, default, or disabled")
	}
	maxOutputTokens, err := intValue(get("MODEL_MAX_OUTPUT_TOKENS"), 2048, 1)
	if err != nil {
		add("MODEL_MAX_OUTPUT_TOKENS %s", err)
	} else if maxOutputTokens > maxModelOutputTokens {
		add("MODEL_MAX_OUTPUT_TOKENS must be at most %d", maxModelOutputTokens)
	}
	maxInputChars, err := intValue(get("MODEL_MAX_INPUT_CHARS"), defaultModelMaxInputChars, 1024)
	if err != nil {
		add("MODEL_MAX_INPUT_CHARS %s", err)
	} else if maxInputChars > defaultModelMaxInputChars {
		add("MODEL_MAX_INPUT_CHARS must be at most %d", defaultModelMaxInputChars)
	}
	dailyCalls, err := intValue(get("MODEL_DAILY_CALLS_PER_USER"), defaultModelDailyCalls, 1)
	if err != nil {
		add("MODEL_DAILY_CALLS_PER_USER %s", err)
	} else if dailyCalls > 1_000_000 {
		add("MODEL_DAILY_CALLS_PER_USER must be at most 1000000")
	}
	dailyInputChars, err := intValue(get("MODEL_DAILY_INPUT_CHARS_PER_USER"), defaultModelDailyInputChars, 1024)
	if err != nil {
		add("MODEL_DAILY_INPUT_CHARS_PER_USER %s", err)
	}
	concurrency, err := intValue(get("MODEL_CONCURRENCY_PER_USER"), defaultModelConcurrency, 1)
	if err != nil {
		add("MODEL_CONCURRENCY_PER_USER %s", err)
	} else if concurrency > 16 {
		add("MODEL_CONCURRENCY_PER_USER must be at most 16")
	}
	globalDailyCalls, err := intValue(get("MODEL_GLOBAL_DAILY_CALLS"), 0, 0)
	if err != nil {
		add("MODEL_GLOBAL_DAILY_CALLS %s", err)
	} else if globalDailyCalls > 1_000_000 {
		add("MODEL_GLOBAL_DAILY_CALLS must be at most 1000000")
	}
	if modelMode == "real" && globalDailyCalls <= 0 {
		// A Real deployment always has a deployment-wide hard stop. Zero
		// cannot mean unlimited in Real mode; an operator must set a positive
		// value to change the default.
		globalDailyCalls = defaultRealGlobalDailyCalls
	}
	globalDailyInputChars, err := intValue(get("MODEL_GLOBAL_DAILY_INPUT_CHARS"), 0, 0)
	if err != nil {
		add("MODEL_GLOBAL_DAILY_INPUT_CHARS %s", err)
	}
	killSwitch, err := boolValue(get("MODEL_KILL_SWITCH"), false)
	if err != nil {
		add("MODEL_KILL_SWITCH %s", err)
	}
	includePrivateContext, err := boolValue(get("AI_INCLUDE_PRIVATE_CONTEXT"), true)
	if err != nil {
		add("AI_INCLUDE_PRIVATE_CONTEXT %s", err)
	}

	corsOrigins, err := parseOrigins(get("CORS_ALLOWED_ORIGINS"))
	if err != nil {
		add("CORS_ALLOWED_ORIGINS %s", err)
	}

	logLevel := get("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}
	switch logLevel {
	case "debug", "info", "warn", "error":
		if production && logLevel == "debug" {
			add("LOG_LEVEL must not be debug in production")
		}
	default:
		add("LOG_LEVEL must be debug, info, warn, or error")
	}

	logFormat := get("LOG_FORMAT")
	if logFormat == "" {
		logFormat = "json"
	}
	if logFormat != "json" && logFormat != "text" {
		add("LOG_FORMAT must be json or text")
	}

	trustProxyHeaders, err := boolValue(get("TRUST_PROXY_HEADERS"), false)
	if err != nil {
		add("TRUST_PROXY_HEADERS %s", err)
	}

	rateAuth, err := intValue(get("RATE_LIMIT_AUTH_PER_MIN"), 10, 1)
	if err != nil {
		add("RATE_LIMIT_AUTH_PER_MIN %s", err)
	}
	rateAPI, err := intValue(get("RATE_LIMIT_API_PER_MIN"), 120, 1)
	if err != nil {
		add("RATE_LIMIT_API_PER_MIN %s", err)
	}
	rateAI, err := intValue(get("RATE_LIMIT_AI_PER_HOUR"), 20, 1)
	if err != nil {
		add("RATE_LIMIT_AI_PER_HOUR %s", err)
	}
	rateCapability, err := intValue(get("RATE_LIMIT_CAPABILITY_PER_HOUR"), 60, 1)
	if err != nil {
		add("RATE_LIMIT_CAPABILITY_PER_HOUR %s", err)
	} else if rateCapability > 60 {
		add("RATE_LIMIT_CAPABILITY_PER_HOUR must be 1..60")
	}

	sessionAbsoluteTTL, err := durationValue(get("SESSION_ABSOLUTE_TTL"), 168*time.Hour)
	if err != nil {
		add("SESSION_ABSOLUTE_TTL %s", err)
	}
	sessionIdleTTL, err := durationValue(get("SESSION_IDLE_TTL"), 24*time.Hour)
	if err != nil {
		add("SESSION_IDLE_TTL %s", err)
	}

	maxRequestBytes, err := int64Value(get("MAX_REQUEST_BYTES"), 1048576, 1)
	if err != nil {
		add("MAX_REQUEST_BYTES %s", err)
	}
	auditRetentionDays, err := intValue(get("AUDIT_RETENTION_DAYS"), 90, 1)
	if err != nil {
		add("AUDIT_RETENTION_DAYS %s", err)
	}
	cleanupInterval, err := durationValue(get("CLEANUP_INTERVAL"), time.Hour)
	if err != nil {
		add("CLEANUP_INTERVAL %s", err)
	}
	shutdownTimeout, err := durationValue(get("SHUTDOWN_TIMEOUT"), 15*time.Second)
	if err != nil {
		add("SHUTDOWN_TIMEOUT %s", err)
	}

	cfg := &Config{
		AppEnv:               appEnv,
		PublicBaseURL:        publicBaseURL,
		BindAddr:             bindAddr,
		DatabaseURL:          databaseURL,
		WebDistDir:           webDistDir,
		CursorHMACSecret:     cursorSecret,
		IPHashPepper:         ipPepper,
		CapabilityHMACSecret: capabilitySecret,
		CapabilityHMACKeyID:  capabilityKeyID,
		CapabilitiesConfig:   capabilitiesConfig,
		Model: ModelConfig{
			Mode:                   modelMode,
			BaseURL:                normalizedBaseURL,
			APIKey:                 modelAPIKey,
			Name:                   modelName,
			Timeout:                modelTimeout,
			ChatPath:               modelChatPath,
			ResponseFormat:         modelResponseFormat,
			ThinkingMode:           modelThinkingMode,
			MaxOutputTokens:        maxOutputTokens,
			MaxInputChars:          maxInputChars,
			DailyCallsPerUser:      dailyCalls,
			DailyInputCharsPerUser: dailyInputChars,
			ConcurrencyPerUser:     concurrency,
			GlobalDailyCalls:       globalDailyCalls,
			GlobalDailyInputChars:  globalDailyInputChars,
			KillSwitch:             killSwitch,
			IncludePrivateContext:  includePrivateContext,
		},
		CORSAllowedOrigins:  corsOrigins,
		LogLevel:            logLevel,
		LogFormat:           logFormat,
		TrustProxyHeaders:   trustProxyHeaders,
		RateLimitAuthPerMin: rateAuth,
		RateLimitAPIPerMin:  rateAPI,
		RateLimitAIPerHour:  rateAI,
		RateLimitCapPerHour: rateCapability,
		SessionAbsoluteTTL:  sessionAbsoluteTTL,
		SessionIdleTTL:      sessionIdleTTL,
		MaxRequestBytes:     maxRequestBytes,
		AuditRetentionDays:  auditRetentionDays,
		CleanupInterval:     cleanupInterval,
		ShutdownTimeout:     shutdownTimeout,
		GitHubRepo:          get("GITHUB_REPO"),
		MetricsToken:        get("METRICS_TOKEN"),
	}

	if len(problems) > 0 {
		return nil, &ValidationError{Problems: problems}
	}
	return cfg, nil
}

func validateHTTPURL(name, raw string, requireHTTPS bool) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must be an absolute http(s) URL", name)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use http or https", name)
	}
	if requireHTTPS && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use https in production", name)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s must not contain credentials", name)
	}
	return nil
}

// normalizeModelBaseURL trims trailing slashes and a terminal /v1 so the
// adapter can append /v1/chat/completions exactly once.
func normalizeModelBaseURL(raw string) (string, error) {
	value := strings.TrimRight(strings.TrimSpace(raw), "/")
	if strings.HasSuffix(value, "/v1") {
		value = strings.TrimSuffix(value, "/v1")
		value = strings.TrimRight(value, "/")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("must be an absolute http(s) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("must use http or https")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("must not contain credentials")
	}
	return value, nil
}

func parseOrigins(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		origin := strings.TrimSpace(part)
		if origin == "" {
			return nil, fmt.Errorf("contains an empty origin")
		}
		if origin == "*" {
			return nil, fmt.Errorf("must not contain *")
		}
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Path != "" {
			return nil, fmt.Errorf("contains an invalid origin")
		}
		origins = append(origins, origin)
	}
	return origins, nil
}

func durationValue(raw string, fallback time.Duration) (time.Duration, error) {
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("must be a duration such as 60s")
	}
	if value <= 0 {
		return 0, fmt.Errorf("must be positive")
	}
	return value, nil
}

func intValue(raw string, fallback, minimum int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("must be an integer")
	}
	if value < minimum {
		return 0, fmt.Errorf("must be at least %d", minimum)
	}
	return value, nil
}

func int64Value(raw string, fallback, minimum int64) (int64, error) {
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("must be an integer")
	}
	if value < minimum {
		return 0, fmt.Errorf("must be at least %d", minimum)
	}
	return value, nil
}

// validModelChatPath accepts only a bounded absolute path. It rejects host
// injection, query/fragment injection, whitespace, backslashes, and traversal.
func validModelChatPath(value string) bool {
	if len(value) < 2 || len(value) > 128 || value[0] != '/' || strings.HasPrefix(value, "//") {
		return false
	}
	if strings.ContainsAny(value, "?#\\ \t\r\n") || strings.Contains(value, "..") {
		return false
	}
	return true
}

// validKeyID accepts the bounded, lowercase key identifier used by the
// request/response HMAC headers during key rotation.
func validKeyID(value string) bool {
	if len(value) < 1 || len(value) > 16 {
		return false
	}
	for index, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			continue
		}
		if char == '-' && index > 0 && index < len(value)-1 {
			continue
		}
		return false
	}
	return true
}

func boolValue(raw string, fallback bool) (bool, error) {
	if raw == "" {
		return fallback, nil
	}
	switch strings.ToLower(raw) {
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("must be true or false")
	}
}
