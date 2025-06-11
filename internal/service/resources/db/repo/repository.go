/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package repo

import (
	"context"

	"github.com/google/uuid"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/repo"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

// ResourcesRepository defines the database repository for the resource server tables
type ResourcesRepository struct {
	repo.CommonRepository
}

// GetDeploymentManagers retrieves all DeploymentManager tuples or returns an empty array if no tuples are found
func (r *ResourcesRepository) GetDeploymentManagers(ctx context.Context) ([]models.DeploymentManager, error) {
	return utils.FindAll[models.DeploymentManager](ctx, r.Db)
}

// GetDeploymentManagersNotIn returns the list of DeploymentManager records not matching the list of keys provided, or
// an empty list if none exist; otherwise an error
func (r *ResourcesRepository) GetDeploymentManagersNotIn(ctx context.Context, keys []any) ([]models.DeploymentManager, error) {
	var e bob.Expression = nil
	if len(keys) > 0 {
		e = psql.Quote(models.DeploymentManager{}.PrimaryKey()).NotIn(psql.Arg(keys...))
	}
	return utils.Search[models.DeploymentManager](ctx, r.Db, e)
}

// GetDeploymentManager retrieves a specific DeploymentManager tuple or returns ErrNotFound if not found
func (r *ResourcesRepository) GetDeploymentManager(ctx context.Context, id uuid.UUID) (*models.DeploymentManager, error) {
	return utils.Find[models.DeploymentManager](ctx, r.Db, id)
}

// GetResourceTypes retrieves all ResourceType tuples or returns an empty array if no tuples are found
func (r *ResourcesRepository) GetResourceTypes(ctx context.Context) ([]models.ResourceType, error) {
	return utils.FindAll[models.ResourceType](ctx, r.Db)
}

// GetResourceType retrieves a specific ResourceType tuple or returns ErrNotFound if not found
func (r *ResourcesRepository) GetResourceType(ctx context.Context, id uuid.UUID) (*models.ResourceType, error) {
	return utils.Find[models.ResourceType](ctx, r.Db, id)
}

// GetResourcePools retrieves all ResourcePool tuples or returns an empty array if no tuples are found
func (r *ResourcesRepository) GetResourcePools(ctx context.Context) ([]models.ResourcePool, error) {
	return utils.FindAll[models.ResourcePool](ctx, r.Db)
}

// GetResourcePool retrieves a specific ResourcePool tuple or returns ErrNotFound if not found
func (r *ResourcesRepository) GetResourcePool(ctx context.Context, id uuid.UUID) (*models.ResourcePool, error) {
	return utils.Find[models.ResourcePool](ctx, r.Db, id)
}

// ResourcePoolExists determines whether a ResourcePool exists or not
func (r *ResourcesRepository) ResourcePoolExists(ctx context.Context, id uuid.UUID) (bool, error) {
	return utils.Exists[models.ResourcePool](ctx, r.Db, id)
}

// CreateResourcePool creates a new ResourcePool tuple
func (r *ResourcesRepository) CreateResourcePool(ctx context.Context, resourcePool *models.ResourcePool) (*models.ResourcePool, error) {
	return utils.Create[models.ResourcePool](ctx, r.Db, *resourcePool)
}

// UpdateResourcePool updates a specific ResourcePool tuple
func (r *ResourcesRepository) UpdateResourcePool(ctx context.Context, resourcePool *models.ResourcePool) (*models.ResourcePool, error) {
	return utils.Update[models.ResourcePool](ctx, r.Db, resourcePool.ResourcePoolID, *resourcePool)
}

// GetResourcePoolResources retrieves all Resource tuples for a specific ResourcePool returns an empty array if not found
func (r *ResourcesRepository) GetResourcePoolResources(ctx context.Context, id uuid.UUID) ([]models.Resource, error) {
	e := psql.Quote("resource_pool_id").EQ(psql.Arg(id))
	return utils.Search[models.Resource](ctx, r.Db, e)
}

// GetResource retrieves a specific Resource tuple or returns ErrNotFound if not found
func (r *ResourcesRepository) GetResource(ctx context.Context, id uuid.UUID) (*models.Resource, error) {
	return utils.Find[models.Resource](ctx, r.Db, id)
}

// CreateResource creates a new Resource tuple
func (r *ResourcesRepository) CreateResource(ctx context.Context, resource *models.Resource) (*models.Resource, error) {
	return utils.Create[models.Resource](ctx, r.Db, *resource)
}

// UpdateResource updates a specific Resource tuple
func (r *ResourcesRepository) UpdateResource(ctx context.Context, resource *models.Resource) (*models.Resource, error) {
	return utils.Update[models.Resource](ctx, r.Db, resource.ResourceID, *resource)
}

// FindStaleResources returns any Resource objects that have a generation less than the specific generation
func (r *ResourcesRepository) FindStaleResources(ctx context.Context, dataSourceID uuid.UUID, generationID int) ([]models.Resource, error) {
	e := psql.Quote("data_source_id").EQ(psql.Arg(dataSourceID)).And(psql.Quote("generation_id").LT(psql.Arg(generationID)))
	return utils.Search[models.Resource](ctx, r.Db, e)
}

// FindStaleResourcePools returns any ResourcePool objects that have a generation less than the specific generation
func (r *ResourcesRepository) FindStaleResourcePools(ctx context.Context, dataSourceID uuid.UUID, generationID int) ([]models.ResourcePool, error) {
	e := psql.Quote("data_source_id").EQ(psql.Arg(dataSourceID)).And(psql.Quote("generation_id").LT(psql.Arg(generationID)))
	return utils.Search[models.ResourcePool](ctx, r.Db, e)
}

// FindStaleResourceTypes returns any ResourceType objects that have a generation less than the specific generation
func (r *ResourcesRepository) FindStaleResourceTypes(ctx context.Context, dataSourceID uuid.UUID, generationID int) ([]models.ResourceType, error) {
	e := psql.Quote("data_source_id").EQ(psql.Arg(dataSourceID)).And(psql.Quote("generation_id").LT(psql.Arg(generationID)))
	return utils.Search[models.ResourceType](ctx, r.Db, e)
}
