package db

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

type PgConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
}

// NewPgxPool get a concurrency safe pool of connection
func NewPgxPool(ctx context.Context, cfg PgConfig) (*pgxpool.Pool, error) {
	// TODO: update config with trace, timeouts etc.
	poolConfig, err := pgxpool.ParseConfig(fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database))
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	slog.Info("Database connection pool established")
	return pool, nil
}

// GetPgConfig common postgres config for alarms server
func GetPgConfig(username, password, database string) PgConfig {
	hostname := utils.GetDatabaseHostname()
	port := fmt.Sprintf("%d", utils.DatabaseServicePort)

	return PgConfig{
		Host:     hostname,
		Port:     port,
		User:     username,
		Password: password,
		Database: database,
	}
}
