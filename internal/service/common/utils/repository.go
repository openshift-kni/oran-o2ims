package utils

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

// ErrNotFound represents the error returned by any repository when not record matches the requested criteria
var ErrNotFound = errors.New("record not found")

// ExecuteCollectExactlyOneRow executes a query and returns exactly one row or error
func ExecuteCollectExactlyOneRow[T db.Model](ctx context.Context, db *pgxpool.Pool, sql string, args []any) (*T, error) {
	var record T
	var err error

	slog.Debug("executing statement", "sql", sql, "args", args)

	// Run query
	rows, _ := db.Query(ctx, sql, args...) // note: err is passed on to Collect* func so we can ignore this
	record, err = pgx.CollectExactlyOneRow(rows, pgx.RowToStructByNameLax[T])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Debug("no record found", "table", record.TableName())
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	slog.Debug("record found", "table", record.TableName())
	return &record, nil
}

// ExecuteCollectRows executes a query and returns all rows or empty array
func ExecuteCollectRows[T db.Model](ctx context.Context, db *pgxpool.Pool, sql string, args []any) ([]T, error) {
	var record T
	var err error

	slog.Debug("executing statement", "sql", sql, "args", args)

	// Run query
	rows, _ := db.Query(ctx, sql, args...) // note: err is passed on to Collect* func so we can ignore this
	records, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[T])
	if err != nil {
		return []T{}, fmt.Errorf("failed to execute query: %w", err)
	}

	slog.Debug("records found", "table", record.TableName(), "count", len(records))
	return records, nil
}

// ExecuteExec executes a query and returns the number of rows affected
func ExecuteExec[T db.Model](ctx context.Context, db *pgxpool.Pool, sql string, args []any) (int64, error) {
	var record T

	slog.Debug("executing statement", "sql", sql, "args", args)

	result, err := db.Exec(ctx, sql, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to delete '%s': %w", record.TableName(), err)
	}

	return result.RowsAffected(), nil
}
