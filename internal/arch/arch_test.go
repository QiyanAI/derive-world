// SPDX-License-Identifier: Apache-2.0
package arch

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

const modulePath = "github.com/QiyanAI/derive-world"

type listedPackage struct {
	ImportPath   string
	Name         string
	Dir          string
	Imports      []string
	TestImports  []string
	XTestImports []string
	GoFiles      []string
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate arch test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func listPackages(t *testing.T, dir string) map[string]listedPackage {
	t.Helper()
	command := exec.Command("go", "list", "-json", "./...")
	command.Dir = dir
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("go list in %s: %v\n%s", dir, err, stderr.String())
	}
	decoder := json.NewDecoder(&stdout)
	packages := map[string]listedPackage{}
	for decoder.More() {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		packages[pkg.ImportPath] = pkg
	}
	return packages
}

func internalPath(name string) string {
	return modulePath + "/internal/" + name
}

func importsInternal(imp string) bool {
	return imp == modulePath+"/internal" || strings.HasPrefix(imp, modulePath+"/internal/")
}

func assertForbidden(t *testing.T, packages map[string]listedPackage, from string, forbidden ...string) {
	t.Helper()
	pkg, ok := packages[internalPath(from)]
	if !ok {
		t.Fatalf("package %s is missing", internalPath(from))
	}
	for _, imported := range pkg.Imports {
		for _, banned := range forbidden {
			bannedPath := internalPath(banned)
			if imported == bannedPath || strings.HasPrefix(imported, bannedPath+"/") {
				t.Errorf("%s must not import %s", pkg.ImportPath, imported)
			}
		}
	}
}

func TestModuleBoundaries(t *testing.T) {
	root := repoRoot(t)
	packages := listPackages(t, root)

	// D08 §3.2 forbidden edges.
	assertForbidden(t, packages, "identity", "objects", "derive", "capability", "personalai", "handoff")
	assertForbidden(t, packages, "objects", "derive", "capability", "personalai", "handoff", "trust")
	assertForbidden(t, packages, "derive", "capability", "personalai", "handoff", "identity", "trust")
	assertForbidden(t, packages, "trust", "capability", "derive", "personalai", "handoff")
	assertForbidden(t, packages, "handoff", "capability", "derive", "personalai", "identity", "trust")
	assertForbidden(t, packages, "capability", "derive", "personalai", "identity")
	assertForbidden(t, packages, "model", "identity", "personalai", "objects", "derive", "trust", "handoff", "capability", "audit", "store", "httpx", "config")
	assertForbidden(t, packages, "httpx", "identity", "personalai", "objects", "derive", "trust", "handoff", "capability", "model", "audit", "store", "config")
	assertForbidden(t, packages, "config", "identity", "personalai", "objects", "derive", "trust", "handoff", "capability", "model", "audit", "store", "httpx")
	assertForbidden(t, packages, "store", "identity", "personalai", "objects", "derive", "trust", "handoff", "capability", "model", "audit", "httpx", "config")
	assertForbidden(t, packages, "audit", "identity", "personalai", "objects", "derive", "trust", "handoff", "capability", "model", "httpx", "config")

	// cmd/server may only import app, config, and store.
	if pkg, ok := packages[modulePath+"/cmd/server"]; ok {
		allowed := map[string]bool{
			internalPath("app"):    true,
			internalPath("config"): true,
			internalPath("store"):  true,
		}
		for _, imported := range pkg.Imports {
			if importsInternal(imported) && !allowed[imported] {
				t.Errorf("cmd/server must not import %s", imported)
			}
		}
	}

	assertNoCycles(t, packages)
	assertDirectDependencyCount(t, root, 6)
	assertSubmoduleIsolation(t, root, "examples/capability-provider", modulePath+"/examples/capability-provider")
	assertSubmoduleIsolation(t, root, "ops/github-metrics", modulePath+"/ops/github-metrics")
	assertTableOwnership(t, root, packages)
}

func assertNoCycles(t *testing.T, packages map[string]listedPackage) {
	t.Helper()
	state := map[string]int{}
	var visit func(string) bool
	visit = func(path string) bool {
		switch state[path] {
		case 1:
			return true
		case 2:
			return false
		}
		state[path] = 1
		pkg := packages[path]
		for _, imported := range pkg.Imports {
			if _, ok := packages[imported]; !ok {
				continue
			}
			if visit(imported) {
				t.Errorf("import cycle through %s -> %s", path, imported)
				return true
			}
		}
		state[path] = 2
		return false
	}
	for path := range packages {
		visit(path)
	}
}

func assertDirectDependencyCount(t *testing.T, root string, want int) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	count := 0
	inBlock := false
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "require ("):
			inBlock = true
			continue
		case inBlock && line == ")":
			inBlock = false
			continue
		case strings.HasPrefix(line, "require "):
			if !strings.Contains(line, "// indirect") {
				count++
			}
			continue
		}
		if inBlock && line != "" && !strings.HasPrefix(line, "//") && !strings.Contains(line, "// indirect") {
			count++
		}
	}
	if count != want {
		t.Fatalf("go.mod direct dependency count = %d, want %d", count, want)
	}
}

func assertSubmoduleIsolation(t *testing.T, root, relative, submodulePath string) {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(relative))

	command := exec.Command("go", "list", "-m", "all")
	command.Dir = dir
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go list -m all in %s: %v", relative, err)
	}
	lines := strings.Fields(strings.TrimSpace(string(output)))
	if len(lines) != 1 {
		t.Fatalf("%s must have zero requirements, got modules: %v", relative, lines)
	}

	packages := listPackages(t, dir)
	for importPath, pkg := range packages {
		for _, imported := range pkg.Imports {
			if imported == modulePath {
				t.Errorf("%s imports the root module: %s", importPath, imported)
				continue
			}
			if strings.HasPrefix(imported, modulePath+"/") && !strings.HasPrefix(imported, submodulePath) {
				t.Errorf("%s imports a root-module package: %s", importPath, imported)
			}
		}
	}
}

var writeTablePattern = regexp.MustCompile(`(?i)\b(?:insert\s+into|update|delete\s+from|merge\s+into|truncate(?:\s+table)?)\s+(?:(?:"[^"]+"|[a-z_][a-z0-9_]*)\.)?"?([a-z_][a-z0-9_]*)"?`)
var copyFromPattern = regexp.MustCompile(`(?i)\bcopy\s+(?:(?:"[^"]+"|[a-z_][a-z0-9_]*)\.)?"?([a-z_][a-z0-9_]*)"?\s+from`)

func assertTableOwnership(t *testing.T, root string, packages map[string]listedPackage) {
	t.Helper()
	owners := map[string]string{
		"users": "identity", "user_sessions": "identity", "api_tokens": "identity",
		"personal_ai_profiles": "personalai", "private_ai_contexts": "personalai",
		"ai_conversations": "personalai", "ai_messages": "personalai",
		"living_objects": "objects", "object_memberships": "objects", "object_events": "objects",
		"object_action_receipts": "objects", "object_proposals": "objects", "invitations": "objects",
		"derivation_edges":  "objects",
		"delegation_grants": "trust", "confirmations": "trust",
		"capability_definitions": "capability", "capability_invocations": "capability",
		"model_invocations": "model",
		"audit_events":      "audit", "audit_actions": "audit",
		"idempotency_keys": "store", "schema_migrations": "store",
		"social_discovery_settings": "social",
		// G01 bootstrap infrastructure; not part of the 22 business tables.
		"app_meta": "store",
	}

	for importPath, pkg := range packages {
		if !strings.HasPrefix(importPath, modulePath+"/internal/") {
			continue
		}
		module := strings.TrimPrefix(importPath, modulePath+"/internal/")
		if index := strings.IndexByte(module, '/'); index >= 0 {
			module = module[:index]
		}
		for _, goFile := range pkg.GoFiles {
			path := filepath.Join(pkg.Dir, goFile)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			for _, pattern := range []*regexp.Regexp{writeTablePattern, copyFromPattern} {
				for _, match := range pattern.FindAllStringSubmatch(string(content), -1) {
					table := strings.ToLower(match[1])
					owner, known := owners[table]
					if known && owner != module {
						t.Errorf("%s writes table %s owned by %s", path, table, owner)
					}
				}
			}
		}
	}

}

func TestWriteTablePatternCoversWriteForms(t *testing.T) {
	cases := map[string]string{
		`INSERT INTO users (id) VALUES (1)`:        "users",
		`insert into public.users (id) values (1)`: "users",
		`INSERT INTO "users" (id) VALUES (1)`:      "users",
		`UPDATE living_objects SET version = 2`:    "living_objects",
		`DELETE FROM object_events WHERE id = 1`:   "object_events",
		`MERGE INTO audit_events USING x ON true`:  "audit_events",
		`TRUNCATE TABLE idempotency_keys`:          "idempotency_keys",
		`COPY audit_events FROM STDIN`:             "audit_events",
	}
	for statement, want := range cases {
		matches := writeTablePattern.FindStringSubmatch(statement)
		if len(matches) < 2 {
			matches = copyFromPattern.FindStringSubmatch(statement)
		}
		if len(matches) < 2 {
			t.Errorf("no table matched in %q", statement)
			continue
		}
		if strings.ToLower(matches[1]) != want {
			t.Errorf("match for %q = %q, want %q", statement, matches[1], want)
		}
	}
}

// TestHandoffSQLWhitelist enforces D05-21: the Handoff projection may read
// only object data. Private context, conversations, messages, delegations,
// tokens, and sessions must not appear even as SQL identifiers.
func TestHandoffSQLWhitelist(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, "internal", "handoff")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read handoff dir: %v", err)
	}
	forbidden := []string{
		"private_ai_contexts", "ai_conversations", "ai_messages",
		"delegation_grants", "api_tokens", "user_sessions",
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		var code strings.Builder
		for _, line := range strings.Split(string(content), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			code.WriteString(line)
			code.WriteString("\n")
		}
		lower := strings.ToLower(code.String())
		for _, table := range forbidden {
			if strings.Contains(lower, table) {
				t.Errorf("handoff source %s references private table %s", entry.Name(), table)
			}
		}
	}
}
