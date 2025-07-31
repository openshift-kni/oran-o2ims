/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dialect"
	"github.com/stephenafamo/bob/dialect/psql/im"

	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/db/models"
	commonmodels "github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
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
		sql, args, err = psql.RawQuery(fmt.Sprintf("%s GROUP BY node_cluster_id", query)).Build(ctx)
	} else {
		sql, args, err = psql.RawQuery(fmt.Sprintf("%s WHERE node_cluster_id IN (?) GROUP BY node_cluster_id", query), psql.Arg(clusters...)).Build(ctx)
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

// UpsertAlarmDefinitions inserts or updates alarm definition records
func (r *ClusterRepository) UpsertAlarmDefinitions(ctx context.Context, records []commonmodels.AlarmDefinition) ([]commonmodels.AlarmDefinition, error) {
	dbModel := commonmodels.AlarmDefinition{}

	if len(records) == 0 {
		return []commonmodels.AlarmDefinition{}, nil
	}

	columns := utils.GetColumns(records[0], []string{
		"AlarmName", "AlarmLastChange", "AlarmChangeType", "AlarmDescription",
		"ProposedRepairActions", "ClearingType", "AlarmAdditionalFields",
		"AlarmDictionaryID", "IsThanosRule", "Severity"},
	)

	modInsert := []bob.Mod[*dialect.InsertQuery]{
		im.Into(dbModel.TableName(), columns...),
		im.OnConflictOnConstraint(dbModel.OnConflict()).DoUpdate(
			im.SetExcluded(columns...)),
		im.Returning(dbModel.PrimaryKey()),
	}

	for _, record := range records {
		modInsert = append(modInsert, im.Values(psql.Arg(record.AlarmName, record.AlarmLastChange, record.AlarmChangeType,
			record.AlarmDescription, record.ProposedRepairActions, record.ClearingType, record.AlarmAdditionalFields,
			record.AlarmDictionaryID, record.IsThanosRule, record.Severity)))
	}

	query := psql.Insert(
		modInsert...,
	)

	sql, args, err := query.Build(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	return utils.ExecuteCollectRows[commonmodels.AlarmDefinition](ctx, r.Db, sql, args)
}

// DeleteAlarmDefinitionsNotIn deletes all alarm definitions identified by the primary key that are not in the list of IDs.
// The Where expression also uses the column "alarm_dictionary_id" to filter the records
func (r *ClusterRepository) DeleteAlarmDefinitionsNotIn(ctx context.Context, ids []any, alarmDictionaryID uuid.UUID) (int64, error) {
	tags := utils.GetDBTagsFromStructFields(commonmodels.AlarmDefinition{}, "AlarmDictionaryID")

	expr := psql.Quote(commonmodels.AlarmDefinition{}.PrimaryKey()).NotIn(psql.Arg(ids...)).And(psql.Quote(tags["AlarmDictionaryID"]).EQ(psql.Arg(alarmDictionaryID)))
	return utils.Delete[commonmodels.AlarmDefinition](ctx, r.Db, expr)
}

// DeleteThanosAlarmDefinitions deletes all thanos alarm definitions
func (r *ClusterRepository) DeleteThanosAlarmDefinitions(ctx context.Context) (int64, error) {
	tags := utils.GetDBTagsFromStructFields(commonmodels.AlarmDefinition{}, "IsThanosRule")

	expr := psql.Quote(tags["IsThanosRule"]).EQ(psql.Arg(true))
	return utils.Delete[commonmodels.AlarmDefinition](ctx, r.Db, expr)
}

// DeleteThanosAlarmDefinitionsNotIn deletes all thanos alarm definitions identified by the primary key that are not in the list of IDs
func (r *ClusterRepository) DeleteThanosAlarmDefinitionsNotIn(ctx context.Context, ids []any) (int64, error) {
	tags := utils.GetDBTagsFromStructFields(commonmodels.AlarmDefinition{}, "IsThanosRule")

	expr := psql.Quote(commonmodels.AlarmDefinition{}.PrimaryKey()).NotIn(psql.Arg(ids...)).And(psql.Quote(tags["IsThanosRule"]).EQ(psql.Arg(true)))
	return utils.Delete[commonmodels.AlarmDefinition](ctx, r.Db, expr)
}

// GetThanosAlarmDefinitions returns the list of Thanos alarm definitions or an empty list if none exist; otherwise an error
func (r *ClusterRepository) GetThanosAlarmDefinitions(ctx context.Context) ([]commonmodels.AlarmDefinition, error) {
	tags := utils.GetDBTagsFromStructFields(commonmodels.AlarmDefinition{}, "IsThanosRule")
	expr := psql.Quote(tags["IsThanosRule"]).EQ(psql.Arg(true))
	return utils.Search[commonmodels.AlarmDefinition](ctx, r.Db, expr)
}

// GetAlarmDefinitionsByAlarmDictionaryID returns the list of AlarmDefinition records that have a matching "alarm_dictionary_id"
func (r *ClusterRepository) GetAlarmDefinitionsByAlarmDictionaryID(ctx context.Context, alarmDictionaryID uuid.UUID) ([]commonmodels.AlarmDefinition, error) {
	e := psql.Quote("alarm_dictionary_id").EQ(psql.Arg(alarmDictionaryID))
	return utils.Search[commonmodels.AlarmDefinition](ctx, r.Db, e)
}

// FindStaleAlarmDictionaries returns the list of AlarmDictionary records that have a matching "data_source_id" and a "generation_id"
func (r *ClusterRepository) FindStaleAlarmDictionaries(ctx context.Context, dataSourceID uuid.UUID, generationID int) ([]commonmodels.AlarmDictionary, error) {
	e := psql.Quote("data_source_id").EQ(psql.Arg(dataSourceID)).And(psql.Quote("generation_id").LT(psql.Arg(generationID)))
	return utils.Search[commonmodels.AlarmDictionary](ctx, r.Db, e)
}

// GetNodeClusterTypeAlarmDictionary returns the list of AlarmDictionary records that have a matching "node_cluster_type_id"
func (r *ClusterRepository) GetNodeClusterTypeAlarmDictionary(ctx context.Context, nodeClusterTypeID uuid.UUID) ([]commonmodels.AlarmDictionary, error) {
	e := psql.Quote("node_cluster_type_id").EQ(psql.Arg(nodeClusterTypeID))
	return utils.Search[commonmodels.AlarmDictionary](ctx, r.Db, e)
}

// GetAlarmDictionaries returns the list of AlarmDictionary records or an empty list if none exist; otherwise an error
func (r *ClusterRepository) GetAlarmDictionaries(ctx context.Context) ([]commonmodels.AlarmDictionary, error) {
	return utils.FindAll[commonmodels.AlarmDictionary](ctx, r.Db)
}

// GetAlarmDictionary returns an AlarmDictionary record matching the specified UUID value or ErrNotFound if no record matched;
func (r *ClusterRepository) GetAlarmDictionary(ctx context.Context, id uuid.UUID) (*commonmodels.AlarmDictionary, error) {
	return utils.Find[commonmodels.AlarmDictionary](ctx, r.Db, id)
}

// SetNodeClusterID sets the nodeClusterID value on cluster resources that may have arrived out of order
func (r *ClusterRepository) SetNodeClusterID(ctx context.Context, nodeClusterName string, nodeClusterID uuid.UUID) (int, error) {
	updates := models.ClusterResource{NodeClusterID: &nodeClusterID}
	e := psql.Quote("node_cluster_name").EQ(psql.Arg(nodeClusterName)).And(psql.Quote("node_cluster_id").IsNull())
	results, err := utils.UpdateAll[models.ClusterResource](ctx, r.Db, e, updates, "NodeClusterID")
	if err != nil {
		return 0, fmt.Errorf("failed to update cluster resources: %w", err)
	}

	return len(results), nil
}
