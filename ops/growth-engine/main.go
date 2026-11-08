// SPDX-License-Identifier: Apache-2.0

// Command growth-engine is the Derive World Growth Engine (DGE) operator.
//
// It is a deterministic, offline analysis tool. It reads the read-only
// snapshots produced by ops/github-metrics, verified product facts, curated
// channel rules and experiment records, and writes reports and prepared drafts.
// It never calls a model provider, never posts to an external service, never
// reads credentials, and never modifies product code or data.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

const toolVersion = "1.0.0"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stderr)
		return errors.New("missing subcommand")
	}
	switch args[0] {
	case "facts":
		return runFacts(args[1:], stdout, stderr)
	case "analytics":
		return runAnalytics(args[1:], stdout, stderr)
	case "opportunities":
		return runOpportunities(args[1:], stdout, stderr)
	case "channels":
		return runChannels(args[1:], stdout, stderr)
	case "content":
		return runContent(args[1:], stdout, stderr)
	case "verify-content":
		return runVerifyContent(args[1:], stdout, stderr)
	case "experiments":
		return runExperiments(args[1:], stdout, stderr)
	case "research":
		return runResearch(args[1:], stdout, stderr)
	case "accounts":
		return runAccounts(args[1:], stdout, stderr)
	case "publish":
		return runPublish(args[1:], stdout, stderr)
	case "pipeline":
		return runPipeline(args[1:], stdout, stderr)
	case "weekly":
		return runWeekly(args[1:], stdout, stderr)
	case "tour-check":
		return runTourCheck(args[1:], stdout, stderr)
	case "version":
		fmt.Fprintln(stdout, "growth-engine "+toolVersion)
		return nil
	default:
		usage(stderr)
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, `usage: growth-engine <command> [flags]

commands:
  facts verify      verify every product fact against the repository
  analytics         growth analytics from github-metrics snapshots
  opportunities     sourced growth opportunities
  channels          channel registry and fit rules
  content           prepare an offline content draft
  verify-content    fact-check and safety-check a draft
  experiments       list or evaluate controlled growth experiments
  weekly            render the weekly growth report
  tour-check        validate the static product tour assets
  version           print the tool version

The engine never calls a model provider and never publishes anything.`)
}

// newFlagSet returns a flag set that reports to stderr and does not exit on
// parse errors, so every failure path returns an error and a non-zero exit.
func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	return flags
}
