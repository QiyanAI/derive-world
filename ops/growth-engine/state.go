// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// readJSON decodes one JSON document with unknown fields rejected only by the
// caller's own decoder; this helper keeps the file open error explicit.
func readJSON(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// writeFile writes data atomically and refuses to overwrite an existing file
// unless force is set. Refusing by default prevents duplicate reports when a
// scheduled job runs twice for the same snapshot.
func writeFile(path string, data []byte, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; pass --force to replace it", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".growth-engine-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func writeOutput(path string, data []byte, stdout io.Writer, force bool) error {
	if path == "" {
		_, err := stdout.Write(data)
		return err
	}
	return writeFile(path, data, force)
}

// safeJoin resolves a repository-relative path and refuses absolute paths,
// parent traversal and symlink escapes. Facts, reports and tour assets are
// inputs from the working tree, never arbitrary filesystem paths.
func safeJoin(root, relative string) (string, error) {
	if relative == "" {
		return "", errors.New("empty path")
	}
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("%s: absolute paths are not allowed", relative)
	}
	cleaned := filepath.Clean(filepath.FromSlash(relative))
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s: path escapes the repository", relative)
	}
	joined := filepath.Join(root, cleaned)
	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	if resolved != resolvedRoot && !strings.HasPrefix(resolved, resolvedRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("%s: path escapes the repository", relative)
	}
	return resolved, nil
}

// stopRequested implements a local stop switch. A scheduled run checks it
// before writing any report, so an operator can halt automation without
// editing the workflow.
var defaultStopFile = "var/growth-engine/STOP"

func stopRequested(stopFile string) bool {
	if value := strings.TrimSpace(os.Getenv("DGE_STOP")); value == "1" || strings.EqualFold(value, "true") {
		return true
	}
	if stopFile == "" {
		stopFile = defaultStopFile
	}
	if stopFile == "" {
		return false
	}
	if _, err := os.Stat(stopFile); err == nil {
		return true
	}
	return false
}

// Secret redaction keeps accidental token material out of reports and logs.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`dw_pat_[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`dw_session=[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`(?i)(api[_-]?key|token|secret)\s*[:=]\s*[^\s"']{12,}`),
}

func redact(value string) string {
	for _, pattern := range secretPatterns {
		value = pattern.ReplaceAllString(value, "[REDACTED]")
	}
	return value
}
