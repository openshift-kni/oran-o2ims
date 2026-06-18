/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package db

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
)

// MigrationsTable table created by migration lib to track state of migration
const MigrationsTable = "schema_migrations"

// MigrationConfig PG config for migration
type MigrationConfig struct {
	Host            string
	Port            string
	User            string
	Password        string
	Database        string
	MigrationsTable string
	Source          source.Driver
}

// StartMigration starts migration for alarms server from a k8s job.
func StartMigration(pgc PgConfig, source source.Driver) error {
	// Retry the initial connection. The migration init container starts
	// simultaneously with postgres, so it may need to wait for the database
	// to become ready. Note: do not use Cap with Factor > 1.0 — the wait
	// library zeros Steps when Duration exceeds Cap, ending retries
	// prematurely.
	connectBackoff := wait.Backoff{
		Duration: 5 * time.Second,
		Factor:   1.0,
		Jitter:   0.1,
		Steps:    40, // ~3.3 minutes total
	}

	var h *MigrationHandler
	err := retry.OnError(connectBackoff, func(_ error) bool { return true }, func() error {
		var handlerErr error
		h, handlerErr = NewHandler(PGtoMigrateConfig(pgc, source))
		if handlerErr != nil {
			slog.Warn("Database not ready for migration, retrying", slog.Any("error", handlerErr))
			return fmt.Errorf("failed to create migrations handler: %w", handlerErr)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to connect to database for migration after retries: %w", err)
	}

	// Setup signal handling
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		slog.Info("Received shutdown signal, stopping migration gracefully")
		h.Migrate.GracefulStop <- true
	}()

	// Run migrations
	if err := h.runMigrationUp(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	slog.Info("Migrations completed successfully")
	return nil
}

// PGtoMigrateConfig convert postgres conn config to migration conn config
func PGtoMigrateConfig(pgc PgConfig, source source.Driver) MigrationConfig {
	return MigrationConfig{
		Host:            pgc.Host,
		Port:            pgc.Port,
		User:            pgc.User,
		Password:        pgc.Password,
		Database:        pgc.Database,
		MigrationsTable: MigrationsTable,
		Source:          source,
	}
}

type MigrationHandler struct {
	Migrate *migrate.Migrate
}

// Printf is the implementation of migrate lib's logger interface
func (h *MigrationHandler) Printf(format string, v ...interface{}) {
	slog.Debug(fmt.Sprintf(format, v...))
}

// Verbose is the implementation of migrate lib's logger interface
func (h *MigrationHandler) Verbose() bool {
	return true
}

// NewHandler configure the migration data
func NewHandler(cfg MigrationConfig) (*MigrationHandler, error) {
	// https://github.com/golang-migrate/migrate/tree/c378583d782e026f472dff657bfd088bf2510038/database/pgx/v5
	connStr := fmt.Sprintf("pgx5://%s:%s@%s:%s/%s?sslmode=verify-full&connect_timeout=10",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
	if cfg.MigrationsTable != "" {
		connStr += fmt.Sprintf("&x-migrations-table=%s", cfg.MigrationsTable)
	}

	if _, err := os.Stat(constants.DefaultServiceCAFile); err == nil {
		connStr += fmt.Sprintf("&sslrootcert=%s", constants.DefaultServiceCAFile)
	} else {
		slog.Warn("No service CA file found")
	}

	m, err := migrate.NewWithSourceInstance("iofs", cfg.Source, connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to create migrate instance: %w", err)
	}

	h := &MigrationHandler{
		Migrate: m,
	}
	m.Log = h

	return h, nil
}

func timer(name string) func() {
	start := time.Now()
	return func() {
		slog.Debug(fmt.Sprintf("%s took %s", name, time.Since(start)))
	}
}

func (h *MigrationHandler) runMigrationUp() error {
	defer timer("Up")()

	if err := h.Migrate.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed up: %w", err)
	}
	return nil
}
