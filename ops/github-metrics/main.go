// SPDX-License-Identifier: Apache-2.0

// Command github-metrics collects read-only GitHub repository metrics,
// compares snapshots, and renders Markdown reports for AI-OPS.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: github-metrics <snapshot|diff|report> [flags]")
		return errors.New("missing subcommand")
	}
	switch args[0] {
	case "snapshot":
		return runSnapshot(args[1:], stdout, stderr)
	case "diff":
		return runDiff(args[1:], stdout, stderr)
	case "report":
		return runReport(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func runSnapshot(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", os.Getenv("GITHUB_REPO"), "repository owner/name")
	output := flags.String("out", "", "output JSON path (default stdout)")
	token := flags.String("token", firstNonEmpty(os.Getenv("METRICS_TOKEN"), os.Getenv("GITHUB_TOKEN")), "read-only GitHub token")
	baseURL := flags.String("base-url", "https://api.github.com", "GitHub API base URL")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *repo == "" {
		return errors.New("--repo or GITHUB_REPO is required")
	}
	client := NewClient(*baseURL, *token)
	snapshot, err := TakeSnapshot(context.Background(), client, *repo, time.Now())
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	return writeOutput(*output, append(data, '\n'), stdout)
}

func runDiff(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("diff", flag.ContinueOnError)
	flags.SetOutput(stderr)
	beforePath := flags.String("before", "", "baseline snapshot JSON")
	afterPath := flags.String("after", "", "new snapshot JSON")
	output := flags.String("out", "", "output path (default stdout)")
	text := flags.Bool("text", false, "write a compact text summary instead of JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *beforePath == "" || *afterPath == "" {
		return errors.New("--before and --after are required")
	}
	var before, after Snapshot
	if err := readJSON(*beforePath, &before); err != nil {
		return err
	}
	if err := readJSON(*afterPath, &after); err != nil {
		return err
	}
	diff := ComputeDiff(&before, &after)
	if *text {
		return writeOutput(*output, []byte(DiffText(diff)), stdout)
	}
	data, err := json.MarshalIndent(diff, "", "  ")
	if err != nil {
		return err
	}
	return writeOutput(*output, append(data, '\n'), stdout)
}

func runReport(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("report", flag.ContinueOnError)
	flags.SetOutput(stderr)
	snapshotPath := flags.String("snapshot", "", "snapshot JSON")
	beforePath := flags.String("before", "", "optional baseline snapshot JSON")
	output := flags.String("out", "", "output path (default stdout)")
	title := flags.String("title", "", "report title")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *snapshotPath == "" {
		return errors.New("--snapshot is required")
	}
	var snapshot Snapshot
	if err := readJSON(*snapshotPath, &snapshot); err != nil {
		return err
	}
	var diff *SnapshotDiff
	if *beforePath != "" {
		var before Snapshot
		if err := readJSON(*beforePath, &before); err != nil {
			return err
		}
		computed := ComputeDiff(&before, &snapshot)
		diff = &computed
	}
	report := RenderReport(&snapshot, diff, *title)
	return writeOutput(*output, []byte(report), stdout)
}

func readJSON(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewDecoder(file).Decode(target)
}

func writeOutput(path string, data []byte, stdout io.Writer) error {
	if path == "" {
		_, err := stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
