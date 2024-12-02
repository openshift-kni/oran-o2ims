package utils

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

// Find retrieves a specific tuple from the database table specified.  If no record is found an empty array is returned.
func Find[T db.Model](ctx context.Context, db *pgxpool.Pool, uuid uuid.UUID, columns []any) ([]T, error) {
	// Build sql query
	var record T
	if columns == nil {
		tags := GetAllDBTagsFromStruct(record, IncludeNilValues)
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
		tags := GetAllDBTagsFromStruct(record, IncludeNilValues)
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

// Create creates a record of the request model type
func Create[T db.Model](ctx context.Context, db *pgxpool.Pool, record T) (*T, error) {
	nonNilTags := GetAllDBTagsFromStruct(record, ExcludeNilValues)
	tags := GetAllDBTagsFromStruct(record, IncludeNilValues)

	// Return all columns to get any defaulted values that the DB may set
	query := psql.Insert(im.Into(record.TableName()), im.Returning(tags.Columns()...))

	// Add columns to the expression.  Maintain the order here so that it coincides with the order of the values
	columns := make([]string, len(nonNilTags))
	for i, c := range nonNilTags.Columns() {
		columns[i] = c.(string)
	}
	query.Expression.Columns = columns

	query.Apply(im.Values(psql.Arg(GetFieldValues(record, nonNilTags)...)))

	sql, args, err := query.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create insert expression: %w", err)
	}

	// Run the query
	result, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute insert expression '%s' with args '%s': %w", sql, args, err)
	}

	record, err = pgx.CollectExactlyOneRow(result, pgx.RowToStructByName[T])
	if err != nil {
		return nil, fmt.Errorf("failed to extract inserted record: %w", err)
	}

	return &record, nil
}

// Update attempts to update a record with a matching primary key.
func Update[T db.Model](ctx context.Context, db *pgxpool.Pool, record T, uuid uuid.UUID) (*T, error) {
	// TODO:	nonNilTags := GetAllDBTagsFromStruct(record, ExcludeNilValues)
	tags := GetAllDBTagsFromStruct(record, IncludeNilValues)

	// Return all columns to get any defaulted values that the DB may set
	query := psql.Update(
		um.Table(record.TableName()),
		// TODO:		um.Set(...)
		um.Where(psql.Quote(record.PrimaryKey()).EQ(psql.Arg(uuid))),
		um.Returning(tags.Columns()...))

	sql, args, err := query.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create insert expression: %w", err)
	}

	// Run the query
	result, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute insert expression '%s' with args '%s': %w", sql, args, err)
	}

	record, err = pgx.CollectExactlyOneRow(result, pgx.RowToStructByName[T])
	if err != nil {
		return nil, fmt.Errorf("failed to extract inserted record: %w", err)
	}

	return &record, nil
}

// Upsert attempts to update a record with a matching primary key if it exists; otherwise it inserts it.
func Upsert[T db.Model](ctx context.Context, db *pgxpool.Pool, record T, uuid uuid.UUID) (*T, error) {
	result, err := Update(ctx, db, record, uuid)
	if err != nil {
		return nil, fmt.Errorf("failed to update record '%s/%s': %w", record.TableName(), uuid, err)
	}

	if result == nil {
		// No records exist with a matching primary key create it
		result, err = Create(ctx, db, record)
		if err != nil {
			return nil, fmt.Errorf("failed to create record '%s/%s': %w", record.TableName(), uuid, err)
		}
	}

	return result, nil
}
