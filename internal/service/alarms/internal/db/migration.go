package db

import (
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrations embed.FS

// MigrationConfig PG config for migration
type MigrationConfig struct {
	Host            string
	Port            string
	User            string
	Password        string
	Database        string
	MigrationsTable string
}

// StartMigration starts migration for alarms server.
func StartMigration() error {
	// Init
	pgc := GetPgConfig()
	cfg := MigrationConfig{
		Host:            pgc.Host,
		Port:            pgc.Port,
		User:            pgc.User,
		Password:        pgc.Password,
		Database:        pgc.Database,
		MigrationsTable: "schema_migrations",
	}

	h, err := newHandler(cfg)
	if err != nil {
		return fmt.Errorf("failed to create migrations handler: %w", err)
	}

	// Setup signal handling
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		slog.Info("Received shutdown signal, stopping migration gracefully")
		h.migrate.GracefulStop <- true
	}()

	// Run migrations
	if err := h.runMigrationUp(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	slog.Info("Migrations completed successfully")
	return nil
}

type MigrationHandler struct {
	migrate *migrate.Migrate
}

// Printf is the implementation of migrate lib's logger interface
func (h *MigrationHandler) Printf(format string, v ...interface{}) {
	slog.Info(fmt.Sprintf(format, v...))
}

// Verbose is the implementation of migrate lib's logger interface
func (h *MigrationHandler) Verbose() bool {
	return true
}

// newHandler configure the migration data
func newHandler(cfg MigrationConfig) (*MigrationHandler, error) {
	d, err := iofs.New(migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to create migrations source: %w", err)
	}

	// https://github.com/golang-migrate/migrate/tree/c378583d782e026f472dff657bfd088bf2510038/database/pgx/v5
	connStr := fmt.Sprintf("pgx5://%s:%s@%s:%s/%s?sslmode=disable&connect_timeout=10",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
	if cfg.MigrationsTable != "" {
		connStr += fmt.Sprintf("&x-migrations-table=%s", cfg.MigrationsTable)
	}

	m, err := migrate.NewWithSourceInstance("iofs", d, connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to create migrate instance: %w", err)
	}

	h := &MigrationHandler{
		migrate: m,
	}
	m.Log = h

	return h, nil
}

func timer(name string) func() {
	start := time.Now()
	return func() {
		slog.Info(fmt.Sprintf("%s took %s", name, time.Since(start)))
	}
}

func (h *MigrationHandler) runMigrationUp() error {
	defer timer("Up")()

	if err := h.migrate.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed up: %w", err)
	}
	return nil
}
