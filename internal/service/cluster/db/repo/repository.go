package repo

import (
	"context"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/repo"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

// ClusterRepository defines the database repository for the cluster server tables
type ClusterRepository struct {
	repo.CommonRepository
}

// GetNodeClusterTypes returns the list of NodeClusterType records or an empty list if none exist; otherwise an error
func (r *ClusterRepository) GetNodeClusterTypes(ctx context.Context) ([]models.NodeClusterType, error) {
	return utils.FindAll[models.NodeClusterType](ctx, r.Db)
}

// GetNodeClusterType returns a NodeClusterType record matching the specified UUID value or ErrNotFound if no record
// matched; otherwise an error
func (r *ClusterRepository) GetNodeClusterType(ctx context.Context, id uuid.UUID) (*models.NodeClusterType, error) {
	return utils.Find[models.NodeClusterType](ctx, r.Db, id)
}

// GetNodeClusters returns the list of NodeCluster records or an empty list if none exist; otherwise an error
func (r *ClusterRepository) GetNodeClusters(ctx context.Context) ([]models.NodeCluster, error) {
	return utils.FindAll[models.NodeCluster](ctx, r.Db)
}

// GetNodeCluster returns a NodeCluster record matching the specified UUID value or ErrNotFound if no record matched;
// otherwise an error
func (r *ClusterRepository) GetNodeCluster(ctx context.Context, id uuid.UUID) (*models.NodeCluster, error) {
	return utils.Find[models.NodeCluster](ctx, r.Db, id)
}

// GetClusterResourceTypes returns the list of ClusterResourceType records or an empty list if none exist; otherwise an error
func (r *ClusterRepository) GetClusterResourceTypes(ctx context.Context) ([]models.ClusterResourceType, error) {
	return utils.FindAll[models.ClusterResourceType](ctx, r.Db)
}

// GetClusterResourceType returns a ClusterResourceType record matching the specified UUID value or ErrNotFound if no record
// matched; otherwise an error
func (r *ClusterRepository) GetClusterResourceType(ctx context.Context, id uuid.UUID) (*models.ClusterResourceType, error) {
	return utils.Find[models.ClusterResourceType](ctx, r.Db, id)
}

// GetClusterResources returns the list of ClusterResource records or an empty list if none exist; otherwise an error
func (r *ClusterRepository) GetClusterResources(ctx context.Context) ([]models.ClusterResource, error) {
	return utils.FindAll[models.ClusterResource](ctx, r.Db)
}

// GetClusterResource returns a ClusterResource record matching the specified UUID value or ErrNotFound if no record matched;
// otherwise an error
func (r *ClusterRepository) GetClusterResource(ctx context.Context, id uuid.UUID) (*models.ClusterResource, error) {
	return utils.Find[models.ClusterResource](ctx, r.Db, id)
}
