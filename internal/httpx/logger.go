// SPDX-License-Identifier: Apache-2.0
package httpx

import (
	"io"
	"log/slog"
	"strings"
)

var redactedKeys = []string{
	"password",
	"token",
	"secret",
	"authorization",
	"cookie",
	"api_key",
	"content",
	"state",
	"prompt",
}

// NewLogger creates a structured logger. JSON logs use `ts` for the timestamp
// and redact any attribute whose key looks sensitive.
func NewLogger(level, format string, output io.Writer) *slog.Logger {
	var slogLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	options := &slog.HandlerOptions{
		Level:       slogLevel,
		ReplaceAttr: redactAttr,
	}
	var handler slog.Handler
	if strings.EqualFold(format, "text") {
		handler = slog.NewTextHandler(output, options)
	} else {
		handler = slog.NewJSONHandler(output, options)
	}
	return slog.New(handler)
}

func redactAttr(_ []string, attr slog.Attr) slog.Attr {
	if attr.Key == slog.TimeKey {
		attr.Key = "ts"
		return attr
	}
	lower := strings.ToLower(attr.Key)
	for _, banned := range redactedKeys {
		if strings.Contains(lower, banned) {
			return slog.String(attr.Key, "[REDACTED]")
		}
	}
	return attr
}
