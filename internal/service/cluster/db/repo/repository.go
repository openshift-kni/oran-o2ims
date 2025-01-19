package repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"

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

// GetNodeClustersNotIn returns the list of NodeCluster records not matching the list of keys provided, or an empty list
// if none exist; otherwise an error
func (r *ClusterRepository) GetNodeClustersNotIn(ctx context.Context, keys []any) ([]models.NodeCluster, error) {
	var e bob.Expression = nil
	if len(keys) > 0 {
		e = psql.Quote(models.NodeCluster{}.PrimaryKey()).NotIn(psql.Arg(keys...))
	}
	return utils.Search[models.NodeCluster](ctx, r.Db, e)
}

// GetNodeCluster returns a NodeCluster record matching the specified UUID value or ErrNotFound if no record matched;
// otherwise an error
func (r *ClusterRepository) GetNodeCluster(ctx context.Context, id uuid.UUID) (*models.NodeCluster, error) {
	return utils.Find[models.NodeCluster](ctx, r.Db, id)
}

// GetNodeClusterByName returns a NodeCluster record matching the specified name or ErrNotFound if no record matched;
// otherwise an error
func (r *ClusterRepository) GetNodeClusterByName(ctx context.Context, name string) (*models.NodeCluster, error) {
	e := psql.Quote("name").EQ(psql.Arg(name))
	results, err := utils.Search[models.NodeCluster](ctx, r.Db, e)
	if err != nil {
		return nil, fmt.Errorf("failed to search for node cluster by name: %w", err)
	}
	if len(results) == 0 {
		return nil, utils.ErrNotFound
	}
	if len(results) > 1 {
		return nil, fmt.Errorf("more than one node cluster with name %s found", name)
	}
	return &results[0], nil
}

// GetNodeClusterResources returns the list of ClusterResource records that have a matching "cluster_name" attribute or
// an empty list if none exist; otherwise an error
func (r *ClusterRepository) GetNodeClusterResources(ctx context.Context, nodeClusterID uuid.UUID) ([]models.ClusterResource, error) {
	e := psql.Quote("node_cluster_id").EQ(psql.Arg(nodeClusterID))
	return utils.Search[models.ClusterResource](ctx, r.Db, e)
}

// GetNodeClusterResourceIDs returns an array of ClusterResourceIDs which contains the list of ClusterResourceID values
// for each NodeCluster in the database.  If the `clusters` parameter is set, then we scope the response to only those
// clusters.
func (r *ClusterRepository) GetNodeClusterResourceIDs(ctx context.Context, clusters ...any) ([]models.ClusterResourceIDs, error) {
	// Couldn't find an obvious way to supply an alias (e.g., AS) to psql.F("array_agg", "cluster_resource_id") so opted
	// to write this query out directly
	var err error
	var sql string
	var args []any
	query := "SELECT node_cluster_id, array_agg(cluster_resource_id) as cluster_resource_ids FROM cluster_resource"
	if len(clusters) == 0 {
		sql, args, err = psql.RawQuery(fmt.Sprintf("%s GROUP BY node_cluster_id", query)).Build()
	} else {
		sql, args, err = psql.RawQuery(fmt.Sprintf("%s WHERE node_cluster_id IN (?) GROUP BY node_cluster_id", query), psql.Arg(clusters...)).Build()
	}
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}
	return utils.ExecuteCollectRows[models.ClusterResourceIDs](ctx, r.Db, sql, args)
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

// GetClusterResourcesNotIn returns the list of ClusterResource records not matching the list of keys provided, or an
// empty list if none exist; otherwise an error
func (r *ClusterRepository) GetClusterResourcesNotIn(ctx context.Context, keys []any) ([]models.ClusterResource, error) {
	var e bob.Expression = nil
	if len(keys) > 0 {
		e = psql.Quote(models.ClusterResource{}.PrimaryKey()).NotIn(psql.Arg(keys...))
	}
	return utils.Search[models.ClusterResource](ctx, r.Db, e)
}

// GetClusterResource returns a ClusterResource record matching the specified UUID value or ErrNotFound if no record matched;
// otherwise an error
func (r *ClusterRepository) GetClusterResource(ctx context.Context, id uuid.UUID) (*models.ClusterResource, error) {
	return utils.Find[models.ClusterResource](ctx, r.Db, id)
}
