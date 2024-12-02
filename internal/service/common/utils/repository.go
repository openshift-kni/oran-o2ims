package utils

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

// Find retrieves a specific tuple from the database table specified.  If no record is found an empty array is returned.
func Find[T db.Model](ctx context.Context, db *pgxpool.Pool, uuid uuid.UUID, columns []any) ([]T, error) {
	// Build sql query
	var record T
	if columns == nil {
		tags := GetAllDBTagsFromStruct(record)
		columns = tags.Columns()
	}

	query, args, err := psql.Select(
		sm.Columns(columns...),
		sm.From(record.TableName()),
		sm.Where(psql.Quote(record.PrimaryKey()).EQ(psql.Arg(uuid))),
	).Build()
	if err != nil {
		return []T{}, fmt.Errorf("failed to build query: %w", err)
	}

	// Run query
	rows, _ := db.Query(ctx, query, args...) // note: err is passed on to Collect* func so we can ignore this
	record, err = pgx.CollectExactlyOneRow(rows, pgx.RowToStructByNameLax[T])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Info("No Entity found", "uuid", uuid, "table", record.TableName())
			return []T{}, nil
		}
		return []T{}, fmt.Errorf("failed to call database: %w", err)
	}

	slog.Info("record found", "table", record.TableName(), "uuid", uuid)
	return []T{record}, nil
}

// FindAll retrieves all tuples from the database table specified.  If no records are found then an empty array is
// returned.
func FindAll[T db.Model](ctx context.Context, db *pgxpool.Pool, columns []any) ([]T, error) {
	// Build sql query
	var record T
	var records []T
	if columns == nil {
		tags := GetAllDBTagsFromStruct(record)
		columns = tags.Columns()
	}

	query, args, err := psql.Select(
		sm.Columns(columns...),
		sm.From(record.TableName()),
	).Build()
	if err != nil {
		return []T{}, fmt.Errorf("failed to build query: %w", err)
	}

	// Run query
	rows, _ := db.Query(ctx, query, args...) // note: err is passed on to Collect* func so we can ignore this
	records, err = pgx.CollectRows(rows, pgx.RowToStructByNameLax[T])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return []T{}, nil
		}
		return []T{}, fmt.Errorf("failed to call database: %w", err)
	}

	slog.Info("records found", "count", len(records), "table", record.TableName())
	return records, nil
}

// Delete deletes a specific tuple from the database table specified.  If no matching tuples are found an error will be
// returned therefore the caller is responsible for checking for existing records.
func Delete[T db.Model](ctx context.Context, db *pgxpool.Pool, uuid uuid.UUID) (int64, error) {
	var record T
	query := psql.Delete(
		dm.From(record.TableName()),
		dm.Where(psql.Quote(record.PrimaryKey()).EQ(psql.Arg(uuid))))

	sql, params, err := query.Build()
	if err != nil {
		return 0, fmt.Errorf("failed to build delete query for '%s/%s': %w", record.TableName(), uuid, err)
	}

	result, err := db.Exec(ctx, sql, params...)
	if err != nil {
		return 0, fmt.Errorf("failed to delete '%s/%s': %w", record.TableName(), uuid, err)
	}

	return result.RowsAffected(), nil
}

// Search retrieves a tuple from the database using arbitrary column values.  If no record is found an empty array is returned.
func Search[T db.Model](ctx context.Context, db *pgxpool.Pool, expression bob.Expression) ([]T, error) {
	// Build sql query
	var record T
	tags := GetAllDBTagsFromStruct(record)

	query, args, err := psql.Select(
		sm.Columns(tags.Columns()...),
		sm.From(record.TableName()),
		sm.Where(expression),
	).Build()
	if err != nil {
		return []T{}, fmt.Errorf("failed to build query: %w", err)
	}

	// Run query
	rows, _ := db.Query(ctx, query, args...) // note: err is passed on to Collect* func so we can ignore this
	record, err = pgx.CollectExactlyOneRow(rows, pgx.RowToStructByNameLax[T])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Info("No Entity found", "expression", expression, "table", record.TableName())
			return []T{}, nil
		}
		return []T{}, fmt.Errorf("failed to call database: %w", err)
	}

	slog.Info("records found", "table", record.TableName(), "expression", expression)
	return []T{record}, nil
}
