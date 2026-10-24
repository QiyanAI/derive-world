// SPDX-License-Identifier: Apache-2.0
package arch

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoProviderVendorHardcodingOutsideModelAdapter enforces the G06 vendor-
// neutrality invariant: business, web, and command code must not depend on a
// specific model vendor, host, or model id. Provider-specific behavior belongs
// in the model adapter and its deployment configuration.
func TestNoProviderVendorHardcodingOutsideModelAdapter(t *testing.T) {
	root := repoRoot(t)
	modelAdapter := filepath.Join(root, "internal", "model") + string(os.PathSeparator)
	forbidden := []string{
		"deepseek",
		"api.openai.com",
		"api.anthropic.com",
		"generativelanguage.googleapis.com",
	}
	directories := []string{
		filepath.Join(root, "internal"),
		filepath.Join(root, "cmd"),
		filepath.Join(root, "web", "src"),
	}
	var findings []string
	for _, directory := range directories {
		err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				name := entry.Name()
				if name == "node_modules" || name == "dist" || name == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			lowerPath := strings.ToLower(path)
			if strings.HasPrefix(path, modelAdapter) {
				return nil
			}
			if strings.HasSuffix(lowerPath, "_test.go") ||
				strings.HasSuffix(lowerPath, ".spec.ts") ||
				strings.HasSuffix(lowerPath, ".test.ts") ||
				strings.HasSuffix(lowerPath, ".spec.tsx") ||
				strings.HasSuffix(lowerPath, ".test.tsx") {
				return nil
			}
			switch strings.ToLower(filepath.Ext(path)) {
			case ".go", ".ts", ".tsx", ".js", ".jsx":
			default:
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			lower := strings.ToLower(string(content))
			for _, term := range forbidden {
				if strings.Contains(lower, term) {
					relative, _ := filepath.Rel(root, path)
					findings = append(findings, relative+" contains "+term)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", directory, err)
		}
	}
	if len(findings) > 0 {
		t.Fatalf("vendor hardcoding outside internal/model: %s", strings.Join(findings, "; "))
	}
}
