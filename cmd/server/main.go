// SPDX-License-Identifier: Apache-2.0

// Command server is the single Derive World entrypoint.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/QiyanAI/derive-world/internal/app"
	"github.com/QiyanAI/derive-world/internal/config"
	"github.com/QiyanAI/derive-world/internal/store"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	command := "serve"
	if len(args) > 0 {
		command = args[0]
	}
	switch command {
	case "serve":
		return runServe(stdout, stderr)
	case "migrate":
		return runMigrate(stdout, stderr)
	case "check-config":
		return runCheckConfig(stdout, stderr)
	case "check-model":
		return runCheckModel(stdout, stderr)
	case "version":
		fmt.Fprintf(stdout, "derive-world %s (commit %s, built %s)\n", version, commit, buildTime)
		return 0
	default:
		fmt.Fprintf(stderr, "usage: server [serve|migrate|check-config|check-model|version]\n")
		return 2
	}
}

// runCheckConfig validates exactly the environment the server will load. It
// does not connect to the database and does not print secret values.
func runCheckConfig(stdout, stderr io.Writer) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "configuration ok (model_mode=%s)\n", cfg.Model.Mode)
	return 0
}

// runCheckModel performs the same non-completion provider check as the
// authenticated settings page. It never sends a prompt and never prints the
// API key. A successful models listing is reported as unverified, not as proof
// that the configured model name is callable.
func runCheckModel(stdout, stderr io.Writer) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 2
	}
	result := app.CheckModelConnection(context.Background(), cfg)
	fmt.Fprintf(stdout, "model check: %s - %s\n", result.Status, result.Detail)
	switch result.Status {
	case "fixture", "unverified_model":
		return 0
	case "unauthorized":
		return 3
	case "rate_limited":
		return 4
	case "unavailable", "error":
		return 5
	case "tls_failure":
		return 6
	case "invalid_url":
		return 7
	case "unreachable":
		return 8
	default:
		return 5
	}
}

func runServe(stdout, stderr io.Writer) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "configuration error: %v\n", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := store.OpenWithRole(ctx, cfg.DatabaseURL, store.AppRole)
	if err != nil {
		fmt.Fprintf(stderr, "database open failed: %v\n", err)
		return 1
	}
	defer database.Close()

	fixtureDir := ""
	if _, err := os.Stat("tests/fixtures/model"); err == nil {
		fixtureDir = "tests/fixtures/model"
	}

	application, err := app.New(app.Options{
		Config:     cfg,
		LogOutput:  stdout,
		Store:      database,
		FixtureDir: fixtureDir,
		Version:    version,
		Commit:     commit,
		BuildTime:  buildTime,
	})
	if err != nil {
		fmt.Fprintf(stderr, "application init failed: %v\n", err)
		return 1
	}
	logger := application.Logger()

	server := &http.Server{
		Addr:              cfg.BindAddr,
		Handler:           application.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      75 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	listener, err := net.Listen("tcp", cfg.BindAddr)
	if err != nil {
		logger.Error("listen_failed", "error", err.Error())
		return 1
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	logger.Info("server_started",
		"bind", cfg.BindAddr,
		"model_mode", cfg.Model.Mode,
		"version", version,
		"commit", commit,
	)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown_failed", "error", err.Error())
		}
		if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server_error", "error", err.Error())
			return 1
		}
		logger.Info("server_stopped")
		return 0
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server_error", "error", err.Error())
			return 1
		}
		logger.Info("server_stopped")
		return 0
	}
}

func runMigrate(stdout, stderr io.Writer) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "configuration error: %v\n", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(stderr, "database open failed: %v\n", err)
		return 1
	}
	defer database.Close()

	if err := database.Migrate(ctx); err != nil {
		fmt.Fprintf(stderr, "migration failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "migration completed")
	return 0
}
