package main

import (
	"fmt"
	"log/slog"
	"os"

	"aiops-platform/backend/internal/config"
	"aiops-platform/backend/migrations"
)

const usage = "usage: go run ./cmd/migrate <up|down|status>"

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	command := os.Args[1]
	if command != "up" && command != "down" && command != "status" {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration error", "error", err)
		os.Exit(1)
	}
	if command == "down" && !migrations.RollbackAllowed(cfg.Environment) {
		slog.Error("migration rollback is disabled in production")
		os.Exit(1)
	}

	runner, err := migrations.New(cfg.Database.DSN())
	if err != nil {
		slog.Error("migration initialization failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := runner.Close(); err != nil {
			slog.Warn("migration resources did not close cleanly", "error", err)
		}
	}()

	switch command {
	case "up":
		err = runner.Up()
	case "down":
		err = runner.DownOne()
	case "status":
		var version uint
		var dirty bool
		version, dirty, err = runner.Version()
		if err == nil {
			slog.Info("migration status", "version", version, "dirty", dirty)
		}
	}

	if err != nil {
		slog.Error("migration command failed", "command", command, "error", err)
		os.Exit(1)
	}
	slog.Info("migration command completed", "command", command)
}
