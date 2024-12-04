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
	"github.com/stephenafamo/bob/dialect/psql/dialect"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

// ErrNotFound represents the error returned by any repository when not record matches the requested criteria
var ErrNotFound = errors.New("record not found")

// Find retrieves a specific tuple from the database table specified.  If no record is found ErrNotFound is returned
// as an error; otherwise a pointer to the stored record is returned or a generic error on failure.
func Find[T db.Model](ctx context.Context, db *pgxpool.Pool, uuid uuid.UUID, columns []any) (*T, error) {
	// Build sql query
	var record T
	if columns == nil {
		tags := GetAllDBTagsFromStruct(record)
		columns = tags.Columns()
	}

	sql, args, err := psql.Select(
		sm.Columns(columns...),
		sm.From(record.TableName()),
		sm.Where(psql.Quote(record.PrimaryKey()).EQ(psql.Arg(uuid))),
	).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	slog.Debug("executing statement", "sql", sql, "args", args)

	// Run query
	rows, _ := db.Query(ctx, sql, args...) // note: err is passed on to Collect* func so we can ignore this
	record, err = pgx.CollectExactlyOneRow(rows, pgx.RowToStructByNameLax[T])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Debug("no record found", "uuid", uuid, "table", record.TableName())
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	slog.Debug("record found", "table", record.TableName(), "uuid", uuid)
	return &record, nil
}

// FindAll retrieves all tuples from the database table specified.  If no records are found then an empty array is
// returned.
func FindAll[T db.Model](ctx context.Context, db *pgxpool.Pool, columns []any) ([]T, error) {
	return Search[T](ctx, db, nil, columns)
}

// Delete deletes a specific tuple from the database table specified.  If no matching tuples are found an error will be
// returned therefore the caller is responsible for checking for existing records.
func Delete[T db.Model](ctx context.Context, db *pgxpool.Pool, uuid uuid.UUID) (int64, error) {
	var record T
	query := psql.Delete(
		dm.From(record.TableName()),
		dm.Where(psql.Quote(record.PrimaryKey()).EQ(psql.Arg(uuid))))

	sql, args, err := query.Build()
	if err != nil {
		return 0, fmt.Errorf("failed to build delete query for '%s/%s': %w", record.TableName(), uuid, err)
	}

	slog.Debug("executing statement", "query", query, "args", args)

	result, err := db.Exec(ctx, sql, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to delete '%s/%s': %w", record.TableName(), uuid, err)
	}

	return result.RowsAffected(), nil
}

// Search retrieves a tuple from the database using arbitrary column values.  If no record is found an empty array
// is returned.
func Search[T db.Model](ctx context.Context, db *pgxpool.Pool, expression bob.Expression, columns []any) ([]T, error) {
	// Build sql query
	var record T
	if columns == nil {
		tags := GetAllDBTagsFromStruct(record)
		columns = tags.Columns()
	}

	params := []bob.Mod[*dialect.SelectQuery]{
		sm.Columns(columns...),
		sm.From(record.TableName()),
	}

	if expression != nil {
		params = append(params, sm.Where(expression))
	}

	sql, args, err := psql.Select(params...).Build()
	if err != nil {
		return []T{}, fmt.Errorf("failed to build query: %w", err)
	}

	slog.Debug("executing statement", "sql", sql, "args", args)

	// Run query
	rows, _ := db.Query(ctx, sql, args...) // note: err is passed on to Collect* func so we can ignore this
	records, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[T])
	if err != nil {
		return []T{}, fmt.Errorf("failed to execute query: %w", err)
	}

	slog.Debug("records found", "table", record.TableName(), "expression", expression, "count", len(records))
	return records, nil
}

// Exists checks whether a record exists in the table with a primary key that matches uuid.
func Exists[T db.Model](ctx context.Context, db *pgxpool.Pool, uuid uuid.UUID) (bool, error) {
	var record T
	query := psql.RawQuery(fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE %s=?)",
		psql.Quote(record.TableName()), psql.Quote(record.PrimaryKey())), uuid)

	sql, args, err := query.Build()
	if err != nil {
		return false, fmt.Errorf("failed to build query: %w", err)
	}

	slog.Error("executing query", "sql", sql, "args", args)

	var result bool
	err = db.QueryRow(ctx, sql, args...).Scan(&result)
	if err != nil {
		return false, fmt.Errorf("failed to execute query: %w", err)
	}

	return result, nil
}

// Create creates a record of the requested model type.  The stored record is returned on success; otherwise an error
// is returned.
func Create[T db.Model](ctx context.Context, db *pgxpool.Pool, record T) (*T, error) {
	nonNilTags := GetNonNilDBTagsFromStruct(record)

	// Return all columns to get any defaulted values that the DB may set
	query := psql.Insert(im.Into(record.TableName()), im.Returning("*"))

	// Add columns to the expression.  Maintain the order here so that it coincides with the order of the values
	columns, values := GetColumnsAndValues(record, nonNilTags)
	query.Expression.Columns = columns
	query.Apply(im.Values(psql.Arg(values...)))

	sql, args, err := query.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create insert expression: %w", err)
	}

	slog.Debug("executing statement", "query", sql, "args", args)

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

// Update attempts to update a record with a matching primary key.  The stored record is returned on success; otherwise
// an error is returned.
func Update[T db.Model](ctx context.Context, db *pgxpool.Pool, record T, uuid uuid.UUID) (*T, error) {
	tags := GetAllDBTagsFromStruct(record)

	// Set up the arguments to the call to psql.Update(...) by using an array because there's no obvious way to add
	// multiple Set(..) operation without having to add them one at a time separately.
	mods := []bob.Mod[*dialect.UpdateQuery]{
		um.Table(record.TableName()),
		um.Where(psql.Quote(record.PrimaryKey()).EQ(psql.Arg(uuid))),
		um.Returning(tags.Columns()...)}

	// Add the individual column sets
	columns, values := GetColumnsAndValues(record, tags)
	for i, column := range columns {
		mods = append(mods, um.SetCol(column).ToArg(values[i]))
	}

	// Build the query
	query := psql.Update(mods...)
	sql, args, err := query.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create update expression: %w", err)
	}

	slog.Debug("executing statement", "sql", sql, "args", args)

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
