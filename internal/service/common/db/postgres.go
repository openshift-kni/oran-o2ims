/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/tracelog"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
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
	poolConfig, err := pgxpool.ParseConfig(fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database))
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Create the tracer with our custom logger
	poolConfig.ConnConfig.Tracer = &tracelog.TraceLog{
		Logger:   customLogger,
		LogLevel: tracelog.LogLevelDebug,
	}

	// Connection pool settings
	poolConfig.MaxConns = 10                                 // Maximum connections in the pool
	poolConfig.MinConns = 2                                  // Minimum idle connections to maintain
	poolConfig.MaxConnLifetime = time.Hour                   // Maximum lifetime of a connection
	poolConfig.MaxConnIdleTime = 30 * time.Minute            // How long a connection can be idle
	poolConfig.HealthCheckPeriod = time.Minute               // How often to check connection health
	poolConfig.MaxConnLifetimeJitter = 10 * time.Millisecond // Add jitter to prevent all connections from being closed at same time

	// Connection settings
	poolConfig.ConnConfig.ConnectTimeout = 2 * time.Minute // Connection timeout

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
	hostname := ctlrutils.GetDatabaseHostname()
	port := fmt.Sprintf("%d", constants.DatabaseServicePort)

	return PgConfig{
		Host:     hostname,
		Port:     port,
		User:     username,
		Password: password,
		Database: database,
	}
}

var (
	warnQueryThreshold  = 500 * time.Millisecond // Queries slower than this trigger warnings
	errorQueryThreshold = 2 * time.Second        // Queries slower than this trigger errors
	maxLogSQLLength     = 500                    // Maximum number of characters of SQL to log
)

// customLogger implements a pgx query logger that tracks query performance, truncates long SQL statements, and includes relevant metadata for debugging.
var customLogger = tracelog.LoggerFunc(func(ctx context.Context, level tracelog.LogLevel, msg string, data map[string]interface{}) {
	var attrs []slog.Attr
	attrs = append(attrs, slog.String("event", msg))

	// Handle duration and query performance
	var logLevel = convertLogLevel(level)
	if duration, ok := data["time"].(time.Duration); ok {
		attrs = append(attrs, slog.String("duration", duration.String()))

		switch {
		case duration >= errorQueryThreshold:
			logLevel = slog.LevelError
			attrs = append(attrs, slog.String("performance", "critical"))
		case duration >= warnQueryThreshold:
			logLevel = slog.LevelWarn
			attrs = append(attrs, slog.String("performance", "slow"))
		}
	}

	// Handle SQL truncation
	if sql, ok := data["sql"].(string); ok {
		if len(sql) > maxLogSQLLength {
			attrs = append(attrs,
				slog.String("sql", sql[:maxLogSQLLength]+"..."),
				slog.Int("sql_truncated_length", len(sql)-maxLogSQLLength),
			)
		} else {
			attrs = append(attrs, slog.String("sql", sql))
		}
	}

	// Add common pgx attributes
	if commandTag, ok := data["commandTag"]; ok {
		attrs = append(attrs, slog.Any("command_tag", commandTag))
	}
	if pid, ok := data["pid"]; ok {
		attrs = append(attrs, slog.Any("pid", pid))
	}
	if name, ok := data["name"].(string); ok {
		attrs = append(attrs, slog.String("statement_name", name))
	}
	if prepared, ok := data["alreadyPrepared"].(bool); ok {
		attrs = append(attrs, slog.Bool("already_prepared", prepared))
	}
	if host, ok := data["host"].(string); ok {
		attrs = append(attrs, slog.String("host", host))
	}
	if port, ok := data["port"].(uint16); ok {
		attrs = append(attrs, slog.Uint64("port", uint64(port)))
	}
	if db, ok := data["database"].(string); ok {
		attrs = append(attrs, slog.String("database", db))
	}
	if table, ok := data["tableName"]; ok {
		attrs = append(attrs, slog.Any("table_name", table))
	}
	if cols, ok := data["columnNames"]; ok {
		attrs = append(attrs, slog.Any("columns", cols))
	}
	if rows, ok := data["rowCount"]; ok {
		attrs = append(attrs, slog.Any("rows_affected", rows))
	}

	// Handle errors (this takes precedence over performance warnings)
	if err, ok := data["err"].(error); ok && err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
		slog.LogAttrs(ctx, slog.LevelError, fmt.Sprintf("Database %s failed", msg), attrs...)
		return
	}

	slog.LogAttrs(ctx, logLevel, fmt.Sprintf("Database %s", msg), attrs...)
})

func convertLogLevel(level tracelog.LogLevel) slog.Level {
	switch level {
	case tracelog.LogLevelTrace, tracelog.LogLevelDebug:
		return slog.LevelDebug
	case tracelog.LogLevelInfo:
		return slog.LevelInfo
	case tracelog.LogLevelWarn:
		return slog.LevelWarn
	case tracelog.LogLevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
