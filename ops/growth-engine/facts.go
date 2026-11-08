// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// FactFile is the curated, machine-checked set of product statements the
// content generator is allowed to use. A fact is only usable when every
// declared source file exists in the tree and contains the quoted text.
type FactFile struct {
	SchemaVersion int    `json:"schema_version"`
	Facts         []Fact `json:"facts"`
}

// Fact is one verified product statement with its evidence.
type Fact struct {
	ID        string       `json:"id"`
	Statement string       `json:"statement"`
	Audience  string       `json:"audience"`
	Sources   []FactSource `json:"sources"`
}

// FactSource is a repository file plus the exact text that must appear in it.
type FactSource struct {
	Path     string `json:"path"`
	Contains string `json:"contains"`
}

// FactStatus is the verification outcome of one fact.
type FactStatus struct {
	ID     string   `json:"id"`
	OK     bool     `json:"ok"`
	Errors []string `json:"errors"`
}

// FactReport is the complete verification result.
type FactReport struct {
	SchemaVersion int          `json:"schema_version"`
	Root          string       `json:"root"`
	Verified      bool         `json:"verified"`
	Checked       int          `json:"checked"`
	Facts         []FactStatus `json:"facts"`
}

func loadFacts(path string) (*FactFile, error) {
	var file FactFile
	if err := readJSON(path, &file); err != nil {
		return nil, err
	}
	if file.SchemaVersion != 1 {
		return nil, fmt.Errorf("%s: unsupported facts schema_version %d", path, file.SchemaVersion)
	}
	if len(file.Facts) == 0 {
		return nil, fmt.Errorf("%s: no facts", path)
	}
	seen := map[string]bool{}
	for index, fact := range file.Facts {
		if strings.TrimSpace(fact.ID) == "" || strings.TrimSpace(fact.Statement) == "" {
			return nil, fmt.Errorf("%s: fact %d has an empty id or statement", path, index)
		}
		if seen[fact.ID] {
			return nil, fmt.Errorf("%s: duplicate fact id %q", path, fact.ID)
		}
		seen[fact.ID] = true
		if len(fact.Sources) == 0 {
			return nil, fmt.Errorf("%s: fact %q has no sources", path, fact.ID)
		}
	}
	return &file, nil
}

func factMap(file *FactFile) map[string]Fact {
	facts := make(map[string]Fact, len(file.Facts))
	for _, fact := range file.Facts {
		facts[fact.ID] = fact
	}
	return facts
}

// verifyFacts checks every source file and quoted text. It never treats a
// missing file as a pass, and it never fetches remote evidence.
func verifyFacts(file *FactFile, root string) FactReport {
	report := FactReport{SchemaVersion: 1, Root: root, Checked: len(file.Facts), Facts: make([]FactStatus, 0, len(file.Facts))}
	verified := true
	for _, fact := range file.Facts {
		status := FactStatus{ID: fact.ID, OK: true, Errors: []string{}}
		for _, source := range fact.Sources {
			path, err := safeJoin(root, source.Path)
			if err != nil {
				status.OK = false
				status.Errors = append(status.Errors, err.Error())
				continue
			}
			content, err := os.ReadFile(path)
			if err != nil {
				status.OK = false
				status.Errors = append(status.Errors, fmt.Sprintf("%s: %v", source.Path, err))
				continue
			}
			if source.Contains == "" {
				status.OK = false
				status.Errors = append(status.Errors, source.Path+": empty quoted text")
				continue
			}
			if !bytes.Contains(content, []byte(source.Contains)) {
				status.OK = false
				status.Errors = append(status.Errors, fmt.Sprintf("%s: missing %q", source.Path, source.Contains))
			}
		}
		if !status.OK {
			verified = false
		}
		report.Facts = append(report.Facts, status)
	}
	report.Verified = verified
	return report
}

func runFacts(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "verify" {
		return errors.New("usage: growth-engine facts verify [--facts PATH] [--root PATH] [--json]")
	}
	flags := newFlagSet("facts verify", stderr)
	factsPath := flags.String("facts", "state/facts.json", "verified product facts")
	root := flags.String("root", "../..", "repository root for source checks")
	asJSON := flags.Bool("json", false, "write JSON instead of text")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	file, err := loadFacts(*factsPath)
	if err != nil {
		return err
	}
	report := verifyFacts(file, *root)
	if *asJSON {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		_, err = stdout.Write(append(encoded, '\n'))
		if !report.Verified {
			return errors.New("one or more facts are not verified")
		}
		return err
	}
	for _, status := range report.Facts {
		if status.OK {
			fmt.Fprintf(stdout, "OK   %s\n", status.ID)
			continue
		}
		fmt.Fprintf(stdout, "FAIL %s\n", status.ID)
		for _, detail := range status.Errors {
			fmt.Fprintf(stdout, "     %s\n", redact(detail))
		}
	}
	fmt.Fprintf(stdout, "checked %d facts: verified=%t\n", report.Checked, report.Verified)
	if !report.Verified {
		return errors.New("one or more facts are not verified")
	}
	return nil
}
