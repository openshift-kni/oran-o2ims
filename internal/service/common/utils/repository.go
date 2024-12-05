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
func Find[T db.Model](ctx context.Context, db *pgxpool.Pool, uuid uuid.UUID, fields ...string) (*T, error) {
	// Build sql query
	var record T
	tags := GetAllDBTagsFromStruct(record)
	if len(fields) > 0 {
		tags = GetDBTagsFromStructFields(record, fields...)
	}

	sql, args, err := psql.Select(
		sm.Columns(tags.Columns()...),
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
func FindAll[T db.Model](ctx context.Context, db *pgxpool.Pool, fields ...string) ([]T, error) {
	return Search[T](ctx, db, nil, fields...)
}

// Delete deletes a specific tuple from the database table specified given an expression for Where clause
func Delete[T db.Model](ctx context.Context, db *pgxpool.Pool, expr psql.Expression) (int64, error) {
	var record T
	query := psql.Delete(
		dm.From(record.TableName()),
		dm.Where(expr))

	sql, args, err := query.Build()
	if err != nil {
		return 0, fmt.Errorf("failed to build delete query for '%s': %w", record.TableName(), err)
	}

	slog.Debug("executing statement", "query", query, "args", args)

	result, err := db.Exec(ctx, sql, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to delete '%s': %w", record.TableName(), err)
	}

	return result.RowsAffected(), nil
}

// Search retrieves a tuple from the database using arbitrary column values.  If no record is found an empty array
// is returned.
func Search[T db.Model](ctx context.Context, db *pgxpool.Pool, expression bob.Expression, fields ...string) ([]T, error) {
	// Build sql query
	var record T
	tags := GetAllDBTagsFromStruct(record)
	if len(fields) > 0 {
		tags = GetDBTagsFromStructFields(record, fields...)
	}

	params := []bob.Mod[*dialect.SelectQuery]{
		sm.Columns(tags.Columns()...),
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
	tags := GetAllDBTagsFromStruct(record)

	// Return all columns to get any defaulted values that the DB may set
	query := psql.Insert(im.Into(record.TableName()), im.Returning(tags.Columns()...))

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
// an error is returned.  If the `fields` argument is set then only those columns are updated but the returned object
// will contain all columns.
func Update[T db.Model](ctx context.Context, db *pgxpool.Pool, record T, uuid uuid.UUID, fields ...string) (*T, error) {
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

	slog.Debug("executing statement", "sql", sql, "args", args)

	// Run the query
	result, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute insert expression '%s' with args '%s': %w", sql, args, err)
	}

	record, err = pgx.CollectExactlyOneRow(result, pgx.RowToStructByName[T])
	if err != nil {
		return nil, fmt.Errorf("failed to extract updated record: %w", err)
	}

	return &record, nil
}

// UpsertOnConflict inserts or updates a set of records in the database table specified. It uses the bob.Mod OnConflict to define the columns to check for conflicts.
// Args: ctx - context, db - pgxpool.Pool, columns - columns to check for conflicts, values - values to insert or update
// Returns: records - the records only contain the primary key, error - if any
func UpsertOnConflict[T db.Model](ctx context.Context, db *pgxpool.Pool, columns []string, values []bob.Expression) ([]T, error) {
	var record T

	modInsert := []bob.Mod[*dialect.InsertQuery]{
		im.Into(record.TableName(), columns...),
		im.OnConflict(record.OnConflict()).DoUpdate(
			im.SetExcluded(columns...)),
		im.Returning(record.PrimaryKey()),
	}

	for _, value := range values {
		modInsert = append(modInsert, im.Values(value))
	}

	query := psql.Insert(
		modInsert...,
	)

	return executeUpsert[T](ctx, db, query)
}

// UpsertOnConflictConstraint inserts or updates a set of records in the database table specified. It uses the bob.Mod OnConflictOnConstraint to define the constraint to check for conflicts.
// Args: ctx - context, db - pgxpool.Pool, columns - columns to check for conflicts, values - values to insert or update
// Returns: records - the records only contain the primary key, error - if any
func UpsertOnConflictConstraint[T db.Model](ctx context.Context, db *pgxpool.Pool, columns []string, values []bob.Expression) ([]T, error) {
	var record T

	modInsert := []bob.Mod[*dialect.InsertQuery]{
		im.Into(record.TableName(), columns...),
		im.OnConflictOnConstraint(record.OnConflict()).DoUpdate(
			im.SetExcluded(columns...)),
		im.Returning(record.PrimaryKey()),
	}

	for _, value := range values {
		modInsert = append(modInsert, im.Values(value))
	}

	query := psql.Insert(
		modInsert...,
	)

	return executeUpsert[T](ctx, db, query)
}

// executeUpsert executes the upsert query and returns the records
// TODO: this might be used by other functions
func executeUpsert[T db.Model](ctx context.Context, db *pgxpool.Pool, query bob.BaseQuery[*dialect.InsertQuery]) ([]T, error) {
	var record T

	sql, args, err := query.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create upsert expression: %w", err)
	}

	// Run query
	rows, _ := db.Query(ctx, sql, args...)
	records, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[T])
	if err != nil {
		return nil, fmt.Errorf("failed to call database: %w", err)
	}

	slog.Info("upsert successful", "affected rows", len(records), "table", record.TableName())
	return records, nil
}
