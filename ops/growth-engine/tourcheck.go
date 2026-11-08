// SPDX-License-Identifier: Apache-2.0
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// TourReport is the result of validating the static product tour.
type TourReport struct {
	SchemaVersion int      `json:"schema_version"`
	Dir           string   `json:"dir"`
	OK            bool     `json:"ok"`
	Checks        []string `json:"checks"`
	Issues        []string `json:"issues"`
}

var htmlAttribute = regexp.MustCompile(`(?i)(?:src|href)="([^"]+)"`)

// CheckTour validates the offline tour that lowers the entry barrier without a
// server. It must be honest (Fixture labelled), self-contained (no third-party
// scripts or analytics), responsive, and reference only files that exist.
func CheckTour(dir string) (TourReport, error) {
	report := TourReport{SchemaVersion: 1, Dir: dir, OK: true, Checks: []string{}, Issues: []string{}}
	fail := func(message string) {
		report.OK = false
		report.Issues = append(report.Issues, message)
	}
	pass := func(message string) {
		report.Checks = append(report.Checks, message)
	}
	indexPath := filepath.Join(dir, "index.html")
	index, err := os.ReadFile(indexPath)
	if err != nil {
		return report, err
	}
	text := string(index)
	if !strings.Contains(text[:min(512, len(text))], "SPDX-License-Identifier: Apache-2.0") {
		fail("index.html is missing the SPDX header")
	} else {
		pass("index.html carries the SPDX header")
	}
	if !strings.Contains(strings.ToLower(text), `name="viewport"`) {
		fail("index.html has no viewport meta tag")
	} else {
		pass("viewport meta present")
	}
	if !strings.Contains(strings.ToLower(text), `lang="en"`) {
		fail("index.html does not declare its language")
	}
	if !strings.Contains(text, "<h1") {
		fail("index.html has no h1")
	}
	for _, required := range []string{"Fixture", "not a live AI service", "Derive"} {
		if !strings.Contains(text, required) {
			fail("index.html is missing the required text: " + required)
		}
	}
	lower := strings.ToLower(text)
	for _, forbidden := range []string{"googletagmanager", "google-analytics", "plausible.io", "posthog", "<iframe"} {
		if strings.Contains(lower, forbidden) {
			fail("index.html contains forbidden third-party or embedded content: " + forbidden)
		}
	}
	for _, ref := range htmlAttribute.FindAllStringSubmatch(text, -1) {
		value := ref[1]
		switch {
		case strings.HasPrefix(value, "#"), strings.HasPrefix(value, "mailto:"):
			continue
		case strings.HasPrefix(value, "https://github.com/QiyanAI/derive-world"):
			continue
		case strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://"):
			fail("external resource or link is not allowlisted: " + value)
			continue
		case strings.HasPrefix(value, "data:"):
			continue
		}
		candidate := filepath.Clean(filepath.Join(dir, filepath.FromSlash(value)))
		if info, err := os.Stat(candidate); err != nil || info.IsDir() {
			fail("referenced file does not exist: " + value)
		}
	}
	for _, name := range []string{"tour.css", "tour.js"} {
		path := filepath.Join(dir, name)
		content, err := os.ReadFile(path)
		if err != nil {
			fail("missing " + name)
			continue
		}
		if !strings.Contains(string(content[:min(512, len(content))]), "SPDX-License-Identifier: Apache-2.0") {
			fail(name + " is missing the SPDX header")
		}
		if strings.Contains(strings.ToLower(string(content)), "http://") ||
			strings.Contains(strings.ToLower(string(content)), "https://") {
			fail(name + " references an absolute URL")
		}
		if name == "tour.css" && !strings.Contains(string(content), "@media") {
			fail("tour.css has no responsive rule")
		} else if name == "tour.css" {
			pass("tour.css has a responsive rule")
		}
	}
	pass("offline asset references resolve")
	return report, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func runTourCheck(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("tour-check", stderr)
	dir := flags.String("dir", "../../showcase/tour", "tour directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	report, err := CheckTour(*dir)
	if err != nil {
		return err
	}
	for _, check := range report.Checks {
		fmt.Fprintln(stdout, "OK   "+check)
	}
	for _, issue := range report.Issues {
		fmt.Fprintln(stdout, "FAIL "+issue)
	}
	if !report.OK {
		return errors.New("tour validation failed")
	}
	fmt.Fprintln(stdout, "tour is offline, labelled and responsive")
	return nil
}
