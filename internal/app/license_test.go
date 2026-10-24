// SPDX-License-Identifier: Apache-2.0
package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLicenseHeaders(t *testing.T) {
	root := repoRoot(t)
	checked := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "dist", "bin", "test-results", "playwright-report", "coverage":
				return filepath.SkipDir
			}
			return nil
		}
		extension := filepath.Ext(path)
		switch extension {
		case ".go", ".ts", ".tsx", ".css", ".mjs", ".sql", ".html":
		default:
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		header := string(content)
		if len(header) > 512 {
			header = header[:512]
		}
		if !strings.Contains(header, "SPDX-License-Identifier: Apache-2.0") {
			t.Errorf("%s is missing the SPDX license header", path)
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatalf("walk repository: %v", err)
	}
	if checked == 0 {
		t.Fatal("no source files were checked")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
}
