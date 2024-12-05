package repo

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

// ResourcesRepository defines the database repository for the resource server tables
type ResourcesRepository struct {
	Db *pgxpool.Pool
}

// GetDeploymentManagers retrieves all DeploymentManager tuples or returns an empty array if no tuples are found
func (r *ResourcesRepository) GetDeploymentManagers(ctx context.Context) ([]models.DeploymentManager, error) {
	return utils.FindAll[models.DeploymentManager](ctx, r.Db, nil)
}

// GetDeploymentManager retrieves a specific DeploymentManager tuple or returns nil if not found
func (r *ResourcesRepository) GetDeploymentManager(ctx context.Context, id uuid.UUID) (*models.DeploymentManager, error) {
	return utils.Find[models.DeploymentManager](ctx, r.Db, id, nil)
}

// GetSubscriptions retrieves all Subscription tuples or returns an empty array if no tuples are found
func (r *ResourcesRepository) GetSubscriptions(ctx context.Context) ([]models.Subscription, error) {
	return utils.FindAll[models.Subscription](ctx, r.Db, nil)
}

// GetSubscription retrieves a specific Subscription tuple or returns nil if not found
func (r *ResourcesRepository) GetSubscription(ctx context.Context, id uuid.UUID) (*models.Subscription, error) {
	return utils.Find[models.Subscription](ctx, r.Db, id, nil)
}

// DeleteSubscription deletes a Subscription tuple.  The caller should ensure that it exists prior to calling this.
func (r *ResourcesRepository) DeleteSubscription(ctx context.Context, id uuid.UUID) (int64, error) {
	expr := psql.Quote(models.Subscription{}.PrimaryKey()).EQ(psql.Arg(id))
	return utils.Delete[models.Subscription](ctx, r.Db, expr)
}

// CreateSubscription create a new Subscription tuple or returns nil if not found
func (r *ResourcesRepository) CreateSubscription(ctx context.Context, subscription *models.Subscription) (*models.Subscription, error) {
	return utils.Create[models.Subscription](ctx, r.Db, *subscription)
}

// GetResourceTypes retrieves all ResourceType tuples or returns an empty array if no tuples are found
func (r *ResourcesRepository) GetResourceTypes(ctx context.Context) ([]models.ResourceType, error) {
	return utils.FindAll[models.ResourceType](ctx, r.Db, nil)
}

// GetResourceType retrieves a specific ResourceType tuple or returns nil if not found
func (r *ResourcesRepository) GetResourceType(ctx context.Context, id uuid.UUID) (*models.ResourceType, error) {
	return utils.Find[models.ResourceType](ctx, r.Db, id, nil)
}

// GetResourcePools retrieves all ResourcePool tuples or returns an empty array if no tuples are found
func (r *ResourcesRepository) GetResourcePools(ctx context.Context) ([]models.ResourcePool, error) {
	return utils.FindAll[models.ResourcePool](ctx, r.Db, nil)
}

// GetResourcePool retrieves a specific ResourcePool tuple or returns nil if not found
func (r *ResourcesRepository) GetResourcePool(ctx context.Context, id uuid.UUID) (*models.ResourcePool, error) {
	return utils.Find[models.ResourcePool](ctx, r.Db, id, nil)
}

// ResourcePoolExists determines whether a ResourcePool exists or not
func (r *ResourcesRepository) ResourcePoolExists(ctx context.Context, id uuid.UUID) (bool, error) {
	return utils.Exists[models.ResourcePool](ctx, r.Db, id)
}

// GetResourcePoolResources retrieves all Resource tuples for a specific ResourcePool returns an empty array if not found
func (r *ResourcesRepository) GetResourcePoolResources(ctx context.Context, id uuid.UUID) ([]models.Resource, error) {
	e := psql.Quote("resource_pool_id").EQ(psql.Arg(id))
	return utils.Search[models.Resource](ctx, r.Db, e, nil)
}

// GetResource retrieves a specific ResourceType tuple or returns nil if not found
func (r *ResourcesRepository) GetResource(ctx context.Context, id uuid.UUID) (*models.Resource, error) {
	return utils.Find[models.Resource](ctx, r.Db, id, nil)
}
