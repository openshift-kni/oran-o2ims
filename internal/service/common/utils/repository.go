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

// Following functions are meant to fulfill basic CRUD operations on the database. More complex queries or bulk operations
// for Insert or Update should be built in the repository files of the specific service and called one of the Execute helper functions.

// Find retrieves a specific tuple from the database table specified.
// The `uuid` argument is the primary key of the record to retrieve.
// If no record is found ErrNotFound is returned as an error.
func Find[T db.Model](ctx context.Context, db *pgxpool.Pool, uuid uuid.UUID) (*T, error) {
	var record T
	tags := GetAllDBTagsFromStruct(record)

	sql, args, err := psql.Select(
		sm.Columns(tags.Columns()...),
		sm.From(record.TableName()),
		sm.Where(psql.Quote(record.PrimaryKey()).EQ(psql.Arg(uuid))),
	).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	return ExecuteCollectExactlyOneRow[T](ctx, db, sql, args)
}

// FindAll retrieves all tuples from the database table specified.
// The `fields` argument is a list of columns to retrieve. If no fields are specified then all columns are fetched.
// If no records are found then an empty array is returned.
func FindAll[T db.Model](ctx context.Context, db *pgxpool.Pool, fields ...string) ([]T, error) {
	return Search[T](ctx, db, nil, fields...)
}

// Search retrieves tuples from the database table specified using a custom expression.
// The `fields` argument is a list of columns to retrieve. If no fields are specified then all columns are fetched.
// The `whereExpr` argument is a custom expression to filter the records.
// If no records are found then an empty array is returned.
func Search[T db.Model](ctx context.Context, db *pgxpool.Pool, whereExpr bob.Expression, fields ...string) ([]T, error) {
	// Build sql query
	var record T
	tags := GetAllDBTagsFromStruct(record)
	if len(fields) > 0 {
		tags = GetDBTagsFromStructFields(record, fields...)
	}

	if whereExpr == nil {
		whereExpr = psql.RawQuery("1=1")
	}

	sql, args, err := psql.Select(
		sm.Columns(tags.Columns()...),
		sm.From(record.TableName()),
		sm.Where(whereExpr),
	).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	return ExecuteCollectRows[T](ctx, db, sql, args)
}

// Delete deletes a specific tuple from the database table specified using a custom expression.
// The `whereExpr` argument is a custom expression to filter the records.
// The number of rows affected is returned on success; otherwise an error is returned.
func Delete[T db.Model](ctx context.Context, db *pgxpool.Pool, whereExpr psql.Expression) (int64, error) {
	var record T
	query := psql.Delete(
		dm.From(record.TableName()),
		dm.Where(whereExpr))

	sql, args, err := query.Build()
	if err != nil {
		return 0, fmt.Errorf("failed to build delete query for '%s': %w", record.TableName(), err)
	}

	slog.Debug("executing statement", "sql", sql, "args", args)

	result, err := db.Exec(ctx, sql, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to delete '%s': %w", record.TableName(), err)
	}

	return result.RowsAffected(), nil
}

// Create creates a record of the requested model type.
// The "record" argument is the record to store in the database.
// The "fields" argument is a list of columns to store. If no fields are specified only non-nil fields are stored.
// The stored record is returned on success; otherwise an error is returned.
func Create[T db.Model](ctx context.Context, db *pgxpool.Pool, record T, fields ...string) (*T, error) {
	all := GetAllDBTagsFromStruct(record)
	tags := GetNonNilDBTagsFromStruct(record)
	if len(fields) > 0 {
		tags = GetDBTagsFromStructFields(record, fields...)
	}

	columns, values := GetColumnsAndValues(record, tags)

	query := psql.Insert(
		im.Into(record.TableName(), columns...),
		im.Values(psql.Arg(values...)),
		im.Returning(all.Columns()...))

	sql, args, err := query.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create insert expression: %w", err)
	}

	return ExecuteCollectExactlyOneRow[T](ctx, db, sql, args)
}

// Update attempts to update a record of the requested model type.
// The `uuid` argument is the primary key of the record to update.
// The `record` argument is the record to update in the database.
// The `fields` argument is a list of columns to update. If no fields are specified only non-nil fields are updated.
// The updated record is returned on success; otherwise an error is returned.
func Update[T db.Model](ctx context.Context, db *pgxpool.Pool, uuid uuid.UUID, record T, fields ...string) (*T, error) {
	all := GetAllDBTagsFromStruct(record)
	tags := all
	if len(fields) > 0 {
		tags = GetDBTagsFromStructFields(record, fields...)
	}

	// Set up the arguments to the call to psql.Update(...) by using an array because there's no obvious way to add
	// multiple Set(..) operation without having to add them one at a time separately.
	mods := []bob.Mod[*dialect.UpdateQuery]{
		um.Table(record.TableName()),
		um.Where(psql.Quote(record.PrimaryKey()).EQ(psql.Arg(uuid))),
		um.Returning(all.Columns()...)}

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

	return ExecuteCollectExactlyOneRow[T](ctx, db, sql, args)
}

// Exists checks whether a record exists in the database table specified.
// The `uuid` argument is the primary key of the record to check.
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

// Helper Execute Query functions

// ExecuteCollectExactlyOneRow executes a query and collects result using pgx.CollectExactlyOneRow.
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

// ExecuteCollectRows executes a query and collects result using pgx.CollectRows.
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
