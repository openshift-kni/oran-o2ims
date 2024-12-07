package repo

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

// ResourcesRepository defines the database repository for the resource server tables
type ResourcesRepository struct {
	Db *pgxpool.Pool
}

// GetDeploymentManagers retrieves all DeploymentManager tuples or returns an empty array if no tuples are found
func (r *ResourcesRepository) GetDeploymentManagers(ctx context.Context) ([]models.DeploymentManager, error) {
	dbModel := models.DeploymentManager{}

	tags := utils.GetAllDBTagsFromStruct(dbModel)

	sql, args, err := psql.Select(
		sm.Columns(tags.Columns()...),
		sm.From(dbModel.TableName()),
	).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	return utils.ExecuteCollectRows[models.DeploymentManager](ctx, r.Db, sql, args)
}

// GetDeploymentManager retrieves a specific DeploymentManager tuple or returns nil if not found
func (r *ResourcesRepository) GetDeploymentManager(ctx context.Context, id uuid.UUID) (*models.DeploymentManager, error) {
	dbModel := models.DeploymentManager{}

	tags := utils.GetAllDBTagsFromStruct(dbModel)

	sql, args, err := psql.Select(
		sm.Columns(tags.Columns()...),
		sm.From(dbModel.TableName()),
		sm.Where(psql.Quote(dbModel.PrimaryKey()).EQ(psql.Arg(id))),
	).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	return utils.ExecuteCollectExactlyOneRow[models.DeploymentManager](ctx, r.Db, sql, args)
}

// GetSubscriptions retrieves all Subscription tuples or returns an empty array if no tuples are found
func (r *ResourcesRepository) GetSubscriptions(ctx context.Context) ([]models.Subscription, error) {
	dbModel := models.Subscription{}

	tags := utils.GetAllDBTagsFromStruct(dbModel)

	sql, args, err := psql.Select(
		sm.Columns(tags.Columns()...),
		sm.From(dbModel.TableName()),
	).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	return utils.ExecuteCollectRows[models.Subscription](ctx, r.Db, sql, args)
}

// GetSubscription retrieves a specific Subscription tuple or returns nil if not found
func (r *ResourcesRepository) GetSubscription(ctx context.Context, id uuid.UUID) (*models.Subscription, error) {
	dbModel := models.Subscription{}

	tags := utils.GetAllDBTagsFromStruct(dbModel)

	sql, args, err := psql.Select(
		sm.Columns(tags.Columns()...),
		sm.From(dbModel.TableName()),
		sm.Where(psql.Quote(dbModel.PrimaryKey()).EQ(psql.Arg(id))),
	).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	return utils.ExecuteCollectExactlyOneRow[models.Subscription](ctx, r.Db, sql, args)
}

// DeleteSubscription deletes a Subscription tuple.  The caller should ensure that it exists prior to calling this.
func (r *ResourcesRepository) DeleteSubscription(ctx context.Context, id uuid.UUID) (int64, error) {
	dbModel := models.Subscription{}

	query := psql.Delete(
		dm.From(dbModel.TableName()),
		dm.Where(psql.Quote(models.Subscription{}.PrimaryKey()).EQ(psql.Arg(id))))

	sql, args, err := query.Build()
	if err != nil {
		return 0, fmt.Errorf("failed to build query: %w", err)
	}

	return utils.ExecuteExec[models.Subscription](ctx, r.Db, sql, args)
}

// CreateSubscription create a new Subscription tuple or returns nil if not found
func (r *ResourcesRepository) CreateSubscription(ctx context.Context, subscription *models.Subscription) (*models.Subscription, error) {
	nonNilTags := utils.GetNonNilDBTagsFromStruct(subscription)

	// Return all columns to get any defaulted values that the DB may set
	query := psql.Insert(im.Into(subscription.TableName()), im.Returning("*"))

	// Add columns to the expression.  Maintain the order here so that it coincides with the order of the values
	columns, values := utils.GetColumnsAndValues[models.Subscription](*subscription, nonNilTags)
	query.Expression.Columns = columns
	query.Apply(im.Values(psql.Arg(values...)))

	sql, args, err := query.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create insert expression: %w", err)
	}

	return utils.ExecuteCollectExactlyOneRow[models.Subscription](ctx, r.Db, sql, args)
}

// GetResourceTypes retrieves all ResourceType tuples or returns an empty array if no tuples are found
func (r *ResourcesRepository) GetResourceTypes(ctx context.Context) ([]models.ResourceType, error) {
	dbModel := models.ResourceType{}

	tags := utils.GetAllDBTagsFromStruct(dbModel)

	sql, args, err := psql.Select(
		sm.Columns(tags.Columns()...),
		sm.From(dbModel.TableName()),
	).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	return utils.ExecuteCollectRows[models.ResourceType](ctx, r.Db, sql, args)
}

// GetResourceType retrieves a specific ResourceType tuple or returns nil if not found
func (r *ResourcesRepository) GetResourceType(ctx context.Context, id uuid.UUID) (*models.ResourceType, error) {
	dbModel := models.ResourceType{}

	tags := utils.GetAllDBTagsFromStruct(dbModel)

	sql, args, err := psql.Select(
		sm.Columns(tags.Columns()...),
		sm.From(dbModel.TableName()),
		sm.Where(psql.Quote(dbModel.PrimaryKey()).EQ(psql.Arg(id))),
	).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	return utils.ExecuteCollectExactlyOneRow[models.ResourceType](ctx, r.Db, sql, args)
}

// GetResourcePools retrieves all ResourcePool tuples or returns an empty array if no tuples are found
func (r *ResourcesRepository) GetResourcePools(ctx context.Context) ([]models.ResourcePool, error) {
	dbModel := models.ResourcePool{}

	tags := utils.GetAllDBTagsFromStruct(dbModel)

	sql, args, err := psql.Select(
		sm.Columns(tags.Columns()...),
		sm.From(dbModel.TableName()),
	).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	return utils.ExecuteCollectRows[models.ResourcePool](ctx, r.Db, sql, args)
}

// GetResourcePool retrieves a specific ResourcePool tuple or returns nil if not found
func (r *ResourcesRepository) GetResourcePool(ctx context.Context, id uuid.UUID) (*models.ResourcePool, error) {
	dbModel := models.ResourcePool{}

	tags := utils.GetAllDBTagsFromStruct(dbModel)

	sql, args, err := psql.Select(
		sm.Columns(tags.Columns()...),
		sm.From(dbModel.TableName()),
		sm.Where(psql.Quote(dbModel.PrimaryKey()).EQ(psql.Arg(id))),
	).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	return utils.ExecuteCollectExactlyOneRow[models.ResourcePool](ctx, r.Db, sql, args)
}

// ResourcePoolExists determines whether a ResourcePool exists or not
func (r *ResourcesRepository) ResourcePoolExists(ctx context.Context, id uuid.UUID) (bool, error) {
	dbModel := models.ResourcePool{}

	query := psql.RawQuery(fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE %s=?)",
		psql.Quote(dbModel.TableName()), psql.Quote(dbModel.PrimaryKey())), id)

	sql, args, err := query.Build()
	if err != nil {
		return false, fmt.Errorf("failed to build query: %w", err)
	}

	slog.Error("executing query", "sql", sql, "args", args)

	var result bool
	err = r.Db.QueryRow(ctx, sql, args...).Scan(&result)
	if err != nil {
		return false, fmt.Errorf("failed to execute query: %w", err)
	}

	return result, nil
}

// GetResourcePoolResources retrieves all Resource tuples for a specific ResourcePool returns an empty array if not found
func (r *ResourcesRepository) GetResourcePoolResources(ctx context.Context, id uuid.UUID) ([]models.Resource, error) {
	resourceDBModel := models.Resource{}
	resourcePoolDBModel := models.ResourcePool{}

	tags := utils.GetAllDBTagsFromStruct(resourceDBModel)

	sql, args, err := psql.Select(
		sm.Columns(tags.Columns()...),
		sm.From(resourceDBModel.TableName()),
		sm.Where(psql.Quote(resourcePoolDBModel.PrimaryKey()).EQ(psql.Arg(id))),
	).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	return utils.ExecuteCollectRows[models.Resource](ctx, r.Db, sql, args)
}

// GetResource retrieves a specific ResourceType tuple or returns nil if not found
func (r *ResourcesRepository) GetResource(ctx context.Context, id uuid.UUID) (*models.Resource, error) {
	dbModel := models.Resource{}

	tags := utils.GetAllDBTagsFromStruct(dbModel)

	sql, args, err := psql.Select(
		sm.Columns(tags.Columns()...),
		sm.From(dbModel.TableName()),
		sm.Where(psql.Quote(dbModel.PrimaryKey()).EQ(psql.Arg(id))),
	).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	return utils.ExecuteCollectExactlyOneRow[models.Resource](ctx, r.Db, sql, args)
}
