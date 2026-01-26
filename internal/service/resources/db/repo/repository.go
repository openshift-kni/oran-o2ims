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

	"github.com/openshift-kni/oran-o2ims/internal/service/common/repo"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

// ResourcesRepository defines the database repository for the resource server tables
type ResourcesRepository struct {
	repo.CommonRepository
}

// GetDeploymentManagers retrieves all DeploymentManager tuples or returns an empty array if no tuples are found
func (r *ResourcesRepository) GetDeploymentManagers(ctx context.Context) ([]models.DeploymentManager, error) {
	return svcutils.FindAll[models.DeploymentManager](ctx, r.Db)
}

// GetDeploymentManagersNotIn returns the list of DeploymentManager records not matching the list of keys provided, or
// an empty list if none exist; otherwise an error
func (r *ResourcesRepository) GetDeploymentManagersNotIn(ctx context.Context, keys []any) ([]models.DeploymentManager, error) {
	var e bob.Expression = nil
	if len(keys) > 0 {
		e = psql.Quote(models.DeploymentManager{}.PrimaryKey()).NotIn(psql.Arg(keys...))
	}
	return svcutils.Search[models.DeploymentManager](ctx, r.Db, e)
}

// GetDeploymentManager retrieves a specific DeploymentManager tuple or returns ErrNotFound if not found
func (r *ResourcesRepository) GetDeploymentManager(ctx context.Context, id uuid.UUID) (*models.DeploymentManager, error) {
	return svcutils.Find[models.DeploymentManager](ctx, r.Db, id)
}

// GetResourceTypes retrieves all ResourceType tuples or returns an empty array if no tuples are found
func (r *ResourcesRepository) GetResourceTypes(ctx context.Context) ([]models.ResourceType, error) {
	return svcutils.FindAll[models.ResourceType](ctx, r.Db)
}

// GetResourceType retrieves a specific ResourceType tuple or returns ErrNotFound if not found
func (r *ResourcesRepository) GetResourceType(ctx context.Context, id uuid.UUID) (*models.ResourceType, error) {
	return svcutils.Find[models.ResourceType](ctx, r.Db, id)
}

// GetResourcePools retrieves all ResourcePool tuples or returns an empty array if no tuples are found
func (r *ResourcesRepository) GetResourcePools(ctx context.Context) ([]models.ResourcePool, error) {
	return svcutils.FindAll[models.ResourcePool](ctx, r.Db)
}

// GetResourcePool retrieves a specific ResourcePool tuple or returns ErrNotFound if not found
func (r *ResourcesRepository) GetResourcePool(ctx context.Context, id uuid.UUID) (*models.ResourcePool, error) {
	return svcutils.Find[models.ResourcePool](ctx, r.Db, id)
}

// ResourcePoolExists determines whether a ResourcePool exists or not
func (r *ResourcesRepository) ResourcePoolExists(ctx context.Context, id uuid.UUID) (bool, error) {
	return svcutils.Exists[models.ResourcePool](ctx, r.Db, id)
}

// CreateResourcePool creates a new ResourcePool tuple
func (r *ResourcesRepository) CreateResourcePool(ctx context.Context, resourcePool *models.ResourcePool) (*models.ResourcePool, error) {
	return svcutils.Create[models.ResourcePool](ctx, r.Db, *resourcePool)
}

// UpdateResourcePool updates a specific ResourcePool tuple
func (r *ResourcesRepository) UpdateResourcePool(ctx context.Context, resourcePool *models.ResourcePool) (*models.ResourcePool, error) {
	return svcutils.Update[models.ResourcePool](ctx, r.Db, resourcePool.ResourcePoolID, *resourcePool)
}

// GetResourcePoolResources retrieves all Resource tuples for a specific ResourcePool returns an empty array if not found
func (r *ResourcesRepository) GetResourcePoolResources(ctx context.Context, id uuid.UUID) ([]models.Resource, error) {
	e := psql.Quote("resource_pool_id").EQ(psql.Arg(id))
	return svcutils.Search[models.Resource](ctx, r.Db, e)
}

// GetResource retrieves a specific Resource tuple or returns ErrNotFound if not found
func (r *ResourcesRepository) GetResource(ctx context.Context, id uuid.UUID) (*models.Resource, error) {
	return svcutils.Find[models.Resource](ctx, r.Db, id)
}

// CreateResource creates a new Resource tuple
func (r *ResourcesRepository) CreateResource(ctx context.Context, resource *models.Resource) (*models.Resource, error) {
	return svcutils.Create[models.Resource](ctx, r.Db, *resource)
}

// UpdateResource updates a specific Resource tuple
func (r *ResourcesRepository) UpdateResource(ctx context.Context, resource *models.Resource) (*models.Resource, error) {
	return svcutils.Update[models.Resource](ctx, r.Db, resource.ResourceID, *resource)
}

// FindStaleResources returns any Resource objects that have a generation less than the specific generation
func (r *ResourcesRepository) FindStaleResources(ctx context.Context, dataSourceID uuid.UUID, generationID int) ([]models.Resource, error) {
	e := psql.Quote("data_source_id").EQ(psql.Arg(dataSourceID)).And(psql.Quote("generation_id").LT(psql.Arg(generationID)))
	return svcutils.Search[models.Resource](ctx, r.Db, e)
}

// FindStaleResourcePools returns any ResourcePool objects that have a generation less than the specific generation
func (r *ResourcesRepository) FindStaleResourcePools(ctx context.Context, dataSourceID uuid.UUID, generationID int) ([]models.ResourcePool, error) {
	e := psql.Quote("data_source_id").EQ(psql.Arg(dataSourceID)).And(psql.Quote("generation_id").LT(psql.Arg(generationID)))
	return svcutils.Search[models.ResourcePool](ctx, r.Db, e)
}

// FindStaleResourceTypes returns any ResourceType objects that have a generation less than the specific generation
func (r *ResourcesRepository) FindStaleResourceTypes(ctx context.Context, dataSourceID uuid.UUID, generationID int) ([]models.ResourceType, error) {
	e := psql.Quote("data_source_id").EQ(psql.Arg(dataSourceID)).And(psql.Quote("generation_id").LT(psql.Arg(generationID)))
	return svcutils.Search[models.ResourceType](ctx, r.Db, e)
}

// GetAlarmDictionaries returns the list of AlarmDictionary records or an empty list if none exist; otherwise an error
func (r *ResourcesRepository) GetAlarmDictionaries(ctx context.Context) ([]models.AlarmDictionary, error) {
	return svcutils.FindAll[models.AlarmDictionary](ctx, r.Db)
}

// GetAlarmDictionary returns an AlarmDictionary record matching the specified UUID value or ErrNotFound if no record matched
func (r *ResourcesRepository) GetAlarmDictionary(ctx context.Context, id uuid.UUID) (*models.AlarmDictionary, error) {
	return svcutils.Find[models.AlarmDictionary](ctx, r.Db, id)
}

// GetAlarmDefinitionsByAlarmDictionaryID returns the list of AlarmDefinition records for a given AlarmDictionary ID
func (r *ResourcesRepository) GetAlarmDefinitionsByAlarmDictionaryID(ctx context.Context, alarmDictionaryID uuid.UUID) ([]models.AlarmDefinition, error) {
	e := psql.Quote("alarm_dictionary_id").EQ(psql.Arg(alarmDictionaryID))
	return svcutils.Search[models.AlarmDefinition](ctx, r.Db, e)
}

// GetResourceTypeAlarmDictionary returns the AlarmDictionary record for a given ResourceType ID
func (r *ResourcesRepository) GetResourceTypeAlarmDictionary(ctx context.Context, resourceTypeID uuid.UUID) ([]models.AlarmDictionary, error) {
	e := psql.Quote("resource_type_id").EQ(psql.Arg(resourceTypeID))
	return svcutils.Search[models.AlarmDictionary](ctx, r.Db, e)
}

// UpsertAlarmDefinitions inserts or updates alarm definition records
func (r *ResourcesRepository) UpsertAlarmDefinitions(ctx context.Context, db svcutils.DBQuery, records []models.AlarmDefinition) ([]models.AlarmDefinition, error) {
	if len(records) == 0 {
		return []models.AlarmDefinition{}, nil
	}

	dbModel := models.AlarmDefinition{}
	columns := svcutils.GetColumns(records[0], []string{
		"AlarmName", "AlarmLastChange", "AlarmChangeType", "AlarmDescription",
		"ProposedRepairActions", "ClearingType", "ManagementInterfaceID",
		"PKNotificationField", "AlarmAdditionalFields", "Severity", "AlarmDictionaryID"},
	)

	modInsert := []bob.Mod[*dialect.InsertQuery]{
		im.Into(dbModel.TableName(), columns...),
		im.OnConflictOnConstraint(dbModel.OnConflict()).DoUpdate(
			im.SetExcluded(columns...)),
		im.Returning(dbModel.PrimaryKey()),
	}

	for _, record := range records {
		modInsert = append(modInsert, im.Values(psql.Arg(
			record.AlarmName, record.AlarmLastChange, record.AlarmChangeType,
			record.AlarmDescription, record.ProposedRepairActions, record.ClearingType,
			record.ManagementInterfaceID, record.PKNotificationField,
			record.AlarmAdditionalFields, record.Severity, record.AlarmDictionaryID)))
	}

	query := psql.Insert(modInsert...)
	sql, args, err := query.Build(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build UpsertAlarmDefinitions query: %w", err)
	}

	return svcutils.ExecuteCollectRows[models.AlarmDefinition](ctx, db, sql, args)
}

// UpsertAlarmDictionary inserts or updates an alarm dictionary record
func (r *ResourcesRepository) UpsertAlarmDictionary(ctx context.Context, db svcutils.DBQuery, record models.AlarmDictionary) (*models.AlarmDictionary, error) {
	columns := svcutils.GetColumns(record, []string{
		"AlarmDictionaryVersion", "AlarmDictionarySchemaVersion",
		"EntityType", "Vendor", "ManagementInterfaceID", "PKNotificationField", "ResourceTypeID"},
	)

	modInsert := []bob.Mod[*dialect.InsertQuery]{
		im.Into(record.TableName(), columns...),
		im.OnConflict(record.OnConflict()).DoUpdate(
			im.SetExcluded(columns...)),
		im.Returning(record.PrimaryKey()),
	}

	modInsert = append(modInsert, im.Values(psql.Arg(
		record.AlarmDictionaryVersion, record.AlarmDictionarySchemaVersion,
		record.EntityType, record.Vendor, record.ManagementInterfaceID,
		record.PKNotificationField, record.ResourceTypeID)))

	query := psql.Insert(modInsert...)
	sql, args, err := query.Build(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build UpsertAlarmDictionary query: %w", err)
	}

	results, err := svcutils.ExecuteCollectRows[models.AlarmDictionary](ctx, db, sql, args)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("upsertAlarmDictionary returned no rows")
	}

	return &results[0], nil
}

// GetLocations retrieves all Location tuples or returns an empty array if no tuples are found
func (r *ResourcesRepository) GetLocations(ctx context.Context) ([]models.Location, error) {
	return svcutils.FindAll[models.Location](ctx, r.Db)
}

// GetLocation retrieves a specific Location tuple by globalLocationId or returns ErrNotFound if not found
func (r *ResourcesRepository) GetLocation(ctx context.Context, globalLocationID string) (*models.Location, error) {
	e := psql.Quote("global_location_id").EQ(psql.Arg(globalLocationID))
	results, err := svcutils.Search[models.Location](ctx, r.Db, e)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, svcutils.ErrNotFound
	}
	return &results[0], nil
}

// GetOCloudSiteIDsForLocation retrieves the list of OCloudSite IDs for a given Location
func (r *ResourcesRepository) GetOCloudSiteIDsForLocation(ctx context.Context, globalLocationID string) ([]uuid.UUID, error) {
	e := psql.Quote("global_location_id").EQ(psql.Arg(globalLocationID))
	sites, err := svcutils.Search[models.OCloudSite](ctx, r.Db, e)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(sites))
	for i, site := range sites {
		ids[i] = site.OCloudSiteID
	}
	return ids, nil
}

// GetOCloudSites retrieves all OCloudSite tuples or returns an empty array if no tuples are found
func (r *ResourcesRepository) GetOCloudSites(ctx context.Context) ([]models.OCloudSite, error) {
	return svcutils.FindAll[models.OCloudSite](ctx, r.Db)
}

// GetOCloudSite retrieves a specific OCloudSite tuple or returns ErrNotFound if not found
func (r *ResourcesRepository) GetOCloudSite(ctx context.Context, id uuid.UUID) (*models.OCloudSite, error) {
	return svcutils.Find[models.OCloudSite](ctx, r.Db, id)
}

// GetResourcePoolIDsForSite retrieves the list of ResourcePool IDs for a given OCloudSite
func (r *ResourcesRepository) GetResourcePoolIDsForSite(ctx context.Context, oCloudSiteID uuid.UUID) ([]uuid.UUID, error) {
	e := psql.Quote("o_cloud_site_id").EQ(psql.Arg(oCloudSiteID))
	pools, err := svcutils.Search[models.ResourcePool](ctx, r.Db, e)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(pools))
	for i, pool := range pools {
		ids[i] = pool.ResourcePoolID
	}
	return ids, nil
}
