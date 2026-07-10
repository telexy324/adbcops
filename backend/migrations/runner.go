package migrations

import (
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed *.sql
var migrationFiles embed.FS

// Runner executes embedded, versioned SQL migrations.
type Runner struct {
	migrate *migrate.Migrate
}

// New creates a migration runner for the supplied PostgreSQL DSN.
func New(databaseDSN string) (*Runner, error) {
	source, err := iofs.New(migrationFiles, ".")
	if err != nil {
		return nil, fmt.Errorf("open embedded migrations: %w", err)
	}

	runner, err := migrate.NewWithSourceInstance("iofs", source, databaseDSN)
	if err != nil {
		return nil, fmt.Errorf("create migration runner: %w", err)
	}
	return &Runner{migrate: runner}, nil
}

// Up applies every pending forward migration.
func (r *Runner) Up() error {
	if err := r.migrate.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// DownOne rolls back exactly one migration.
func (r *Runner) DownOne() error {
	if err := r.migrate.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("rollback migration: %w", err)
	}
	return nil
}

// Version returns the current migration version and dirty state. An empty
// database is represented by version zero.
func (r *Runner) Version() (uint, bool, error) {
	version, dirty, err := r.migrate.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read migration version: %w", err)
	}
	return version, dirty, nil
}

// Close releases migration source and database resources.
func (r *Runner) Close() error {
	sourceErr, databaseErr := r.migrate.Close()
	return errors.Join(sourceErr, databaseErr)
}

// RollbackAllowed prevents destructive migration commands in production.
func RollbackAllowed(environment string) bool {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "prod", "production":
		return false
	default:
		return true
	}
}
