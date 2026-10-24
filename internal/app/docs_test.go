// SPDX-License-Identifier: Apache-2.0
package app

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var documentedCommands = map[string]bool{
	"git clone https://github.com/QiyanAI/derive-world.git": true,
	"cp .env.example .env":                      true,
	"docker compose up --build -d":              true,
	"docker compose ps":                         true,
	"docker compose down":                       true,
	"docker compose down -v":                    true,
	"docker compose up -d --force-recreate app": true,
	`docker compose exec db sh -c 'pg_dump -Fc -U "$POSTGRES_USER" -d "$POSTGRES_DB"' > backup.dump`:                       true,
	`docker compose exec -T db sh -c 'pg_restore --clean --if-exists -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < backup.dump`: true,
	"go run ./cmd/server migrate":     true,
	"go run ./cmd/server serve":       true,
	"npm ci":                          true,
	"npm run -w web build":            true,
	"npm run -w sdk/typescript build": true,
	"npm run -w web dev":              true,
	"npm run test:e2e":                true,
}

func TestDocsCommands(t *testing.T) {
	files := []string{
		"README.md",
		"README.zh-CN.md",
		"docs/en/quickstart.md",
		"docs/zh/quickstart.md",
		"docs/en/self-hosting.md",
		"docs/zh/self-hosting.md",
	}
	prefixes := []string{
		"git clone ",
		"cp .env",
		"docker compose ",
		"go run ./cmd/server",
		"npm run ",
		"npm ci",
	}
	found := 0
	for _, relative := range files {
		path := filepath.Join(repoRoot(t), filepath.FromSlash(relative))
		file, err := os.Open(path)
		if err != nil {
			t.Fatalf("open %s: %v", relative, err)
		}
		inFence := false
		lineNumber := 0
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			lineNumber++
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "```") {
				inFence = !inFence
				continue
			}
			if !inFence || line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.TrimPrefix(line, "$ ")
			matched := false
			for _, prefix := range prefixes {
				if strings.HasPrefix(line, prefix) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			found++
			if !documentedCommands[line] {
				t.Errorf("%s:%d documents an unapproved command: %q", relative, lineNumber, line)
			}
		}
		if err := scanner.Err(); err != nil {
			t.Fatalf("read %s: %v", relative, err)
		}
		file.Close()
	}
	if found == 0 {
		t.Fatal("no documented commands were found")
	}
}
