/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	commonapi "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
	models2 "github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/repo"
)

// asyncEventBufferSize defines the number of buffered entries in the async event channel
const asyncEventBufferSize = 10

// DataSource represents the operations required to be supported by any objects implementing a
// data collection backend.
type DataSource interface {
	Name() string
	GetID() uuid.UUID
	Init(dataSourceID uuid.UUID, generationID int, asyncEventChannel chan<- *async.AsyncChangeEvent)
	SetGenerationID(value int)
	GetGenerationID() int
	IncrGenerationID() int
}

// WatchableDataSource defines an interface of a data source capable of watching for async events.
type WatchableDataSource interface {
	Watch(ctx context.Context) error
}

// ResourceWithType pairs a Resource with its corresponding ResourceType for
// batch persistence operations.
type ResourceWithType struct {
	Resource     models.Resource
	ResourceType models.ResourceType
}

// PoolChangeNotifier is implemented by data sources that need to re-evaluate
// their resources when a ResourcePool is created or updated.
type PoolChangeNotifier interface {
	BuildResourcesForPool(ctx context.Context, poolName string) ([]ResourceWithType, error)
}

// NotificationHandler defines an interface over which notifications are published.
type NotificationHandler interface {
	Notify(ctx context.Context, event *notifier.Notification)
}

// Collector defines the attributes required by the collector implementation.
type Collector struct {
	pool                *pgxpool.Pool // Direct pool injection for transaction operations
	notificationHandler NotificationHandler
	repository          *repo.ResourcesRepository
	dataSources         []DataSource
	AsyncChangeEvents   chan *async.AsyncChangeEvent
	alarmDictCache      map[string]*uuid.UUID
}

// NewCollector creates a new collector instance
func NewCollector(pool *pgxpool.Pool, repo *repo.ResourcesRepository, notificationHandler NotificationHandler, dataSources []DataSource) *Collector {
	return &Collector{
		pool:                pool,
		repository:          repo,
		notificationHandler: notificationHandler,
		dataSources:         dataSources,
		AsyncChangeEvents:   make(chan *async.AsyncChangeEvent, asyncEventBufferSize),
	}
}

// Run executes the collector main loop to process watch events from all data sources
func (c *Collector) Run(ctx context.Context) error {
	if err := c.init(ctx); err != nil {
		return err
	}

	// Start listening for async events from all data sources
	if err := c.watchForChanges(ctx); err != nil {
		return fmt.Errorf("failed to start listeners: %w", err)
	}

	for {
		select {
		case event := <-c.AsyncChangeEvents:
			if err := c.handleAsyncEvent(ctx, event); err != nil {
				slog.Error("failed to handle async change", "event", event, "error", err)
			}
		case <-ctx.Done():
			slog.Info("Context terminated; collector exiting")
			return nil
		}
	}
}

// init runs the onetime initialization steps for the collector
func (c *Collector) init(ctx context.Context) error {
	slog.Info("initializing data collector")

	for _, d := range c.dataSources {
		err := c.initDataSource(ctx, d)
		if err != nil {
			return err
		}
	}

	return nil
}

// initDataSource initializes a single data source from persistent storage.  This recovers its unique UUID and
// generation NotificationID so that it continues from its last save point.
func (c *Collector) initDataSource(ctx context.Context, dataSource DataSource) error {
	name := dataSource.Name()
	record, err := c.repository.GetDataSourceByName(ctx, name)
	switch {
	case errors.Is(err, svcutils.ErrNotFound):
		// Doesn't exist so create it now.
		result, err := c.repository.CreateDataSource(ctx, &models2.DataSource{
			Name:         name,
			GenerationID: dataSource.GetGenerationID(),
		})
		if err != nil {
			return fmt.Errorf("failed to create new data source %q: %w", name, err)
		}

		dataSource.Init(*result.DataSourceID, 0, c.AsyncChangeEvents)
		slog.Info("created new data source", "name", name, "uuid", *result.DataSourceID)
	case err != nil:
		return fmt.Errorf("failed to get data source %q: %w", name, err)
	default:
		dataSource.Init(*record.DataSourceID, record.GenerationID, c.AsyncChangeEvents)
		slog.Info("restored data source",
			"name", name, "uuid", record.DataSourceID, "generation", record.GenerationID)
	}
	return nil
}

// watchForChanges starts an event listener for each data source.  Data is reported by via the channel provided to
// each data source.
func (c *Collector) watchForChanges(ctx context.Context) error {
	for _, d := range c.dataSources {
		if _, ok := d.(WatchableDataSource); !ok {
			continue
		}

		if err := d.(WatchableDataSource).Watch(ctx); err != nil {
			return fmt.Errorf("failed to watch for changes from %s: %w", d.Name(), err)
		}
	}
	return nil
}

// handleAsyncResourceTypeEvent handles an async event for a ResourceType object.
func (c *Collector) handleAsyncResourceTypeEvent(ctx context.Context, resourceType models.ResourceType, deleted bool) error {
	var dataChangeEvent *models2.DataChangeEvent
	var err error

	if deleted {
		dataChangeEvent, err = svcutils.DeleteObjectWithChangeEvent(
			ctx, c.pool, resourceType, resourceType.ResourceTypeID, nil, func(object interface{}) any {
				record, _ := object.(models.ResourceType)
				return models.ResourceTypeToModel(&record, nil)
			})
		if err != nil {
			return fmt.Errorf("failed to delete resource type '%s': %w", resourceType.ResourceTypeID, err)
		}
	} else {
		alarmDictID := c.getAlarmDictionaryID(ctx, resourceType.ResourceTypeID)

		dataChangeEvent, err = svcutils.PersistObjectWithChangeEvent(
			ctx, c.pool, resourceType, resourceType.ResourceTypeID, nil, func(object interface{}) any {
				record, _ := object.(models.ResourceType)
				return models.ResourceTypeToModel(&record, alarmDictID)
			})
		if err != nil {
			return fmt.Errorf("failed to persist resource type '%s': %w", resourceType.ResourceTypeID, err)
		}
	}

	if dataChangeEvent != nil {
		c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
	}

	return nil
}

// handleAsyncResourceEvent handles an async event for a Resource object.
func (c *Collector) handleAsyncResourceEvent(ctx context.Context, resource models.Resource, deleted bool) error {
	var dataChangeEvent *models2.DataChangeEvent
	var err error

	if deleted {
		dataChangeEvent, err = svcutils.DeleteObjectWithChangeEvent(
			ctx, c.pool, resource, resource.ResourceID, &resource.ResourcePoolID, func(object interface{}) any {
				record, _ := object.(models.Resource)
				return models.ResourceToModel(&record, nil)
			})
		if err != nil {
			return fmt.Errorf("failed to delete resource '%s': %w", resource.ResourceID, err)
		}
	} else {
		dataChangeEvent, err = svcutils.PersistObjectWithChangeEvent(
			ctx, c.pool, resource, resource.ResourceID, &resource.ResourcePoolID, func(object interface{}) any {
				record, _ := object.(models.Resource)
				return models.ResourceToModel(&record, nil)
			})
		if err != nil {
			return fmt.Errorf("failed to persist resource '%s': %w", resource.ResourceID, err)
		}
	}

	if dataChangeEvent != nil {
		c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
	}

	return nil
}

// handleResourceSyncCompletion handles the sync completion for Resource objects
// by purging resources and resource types not in the key set.
func (c *Collector) handleResourceSyncCompletion(ctx context.Context, ids []any) error {
	slog.Debug("Handling end of sync for Resource instances", "count", len(ids))
	c.invalidateAlarmDictCache()

	// Purge stale resources
	resources, err := c.repository.GetResourcesNotIn(ctx, ids)
	if err != nil {
		return fmt.Errorf("failed to get stale resources: %w", err)
	}

	resourceCount := 0
	for _, resource := range resources {
		dataChangeEvent, err := svcutils.DeleteObjectWithChangeEvent(ctx, c.pool, resource, resource.ResourceID,
			&resource.ResourcePoolID, func(object interface{}) any {
				r, _ := object.(models.Resource)
				return models.ResourceToModel(&r, nil)
			})
		if err != nil {
			return fmt.Errorf("failed to delete stale resource: %w", err)
		}
		if dataChangeEvent != nil {
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
			resourceCount++
		}
	}

	if resourceCount > 0 {
		slog.Info("Deleted stale resources", "count", resourceCount)
	}

	return nil
}

// getAlarmDictionaryID returns the alarm dictionary ID for a given resource
// type, using a cached map to avoid repeated database queries.
func (c *Collector) getAlarmDictionaryID(ctx context.Context, resourceTypeID uuid.UUID) *uuid.UUID {
	if c.alarmDictCache == nil {
		alarmDictionaries, err := c.repository.GetAlarmDictionaries(ctx)
		if err != nil {
			slog.Warn("failed to fetch alarm dictionaries", "error", err)
			return nil
		}
		c.alarmDictCache = make(map[string]*uuid.UUID)
		for _, dict := range alarmDictionaries {
			dictID := dict.AlarmDictionaryID
			c.alarmDictCache[dict.ResourceTypeID.String()] = &dictID
		}
	}
	return c.alarmDictCache[resourceTypeID.String()]
}

// invalidateAlarmDictCache clears the cached alarm dictionary map so it will
// be refreshed on the next lookup.
func (c *Collector) invalidateAlarmDictCache() {
	c.alarmDictCache = nil
}

// collectResourcePools collects ResourcePool objects from the data source, persists them to the database,
// and signals any change events to the notification processor.
// handleDeploymentManagerSyncCompletion handles the end of sync for DeploymentManager objects.  It deletes any
// DeploymentManager objects not included in the set of keys received during the sync operation.
func (c *Collector) handleDeploymentManagerSyncCompletion(ctx context.Context, ids []any) error {
	slog.Debug("Handling end of sync for DeploymentManager instances", "count", len(ids))
	records, err := c.repository.GetDeploymentManagersNotIn(ctx, ids)
	if err != nil {
		return fmt.Errorf("failed to get stale deployment managers: %w", err)
	}

	count := 0
	for _, record := range records {
		dataChangeEvent, err := svcutils.DeleteObjectWithChangeEvent(ctx, c.pool, record, record.DeploymentManagerID, nil, func(object interface{}) any {
			r, _ := object.(models.DeploymentManager)
			return models.DeploymentManagerToModel(&r, commonapi.NewDefaultFieldOptions())
		})

		if err != nil {
			return fmt.Errorf("failed to delete stale deployment manager: %w", err)
		}

		if dataChangeEvent != nil {
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
			count++
		}
	}

	if count > 0 {
		slog.Info("Deleted stale deployment manager records", "count", count)
	}

	return nil
}

// handleSyncCompletion handles the case of a data source watcher that has discovered that it is
// out of date with the data on the API server.  In response to that, it has re-listed all objects to
// get back to a synchronized state before processing new change events.  This function is being
// invoked to inform us that all data has been received and that any objects in the database that
// are not part of the set of keys received can be deleted.
func (c *Collector) handleSyncCompletion(ctx context.Context, objectType db.Model, keys []uuid.UUID) error {
	ids := make([]any, len(keys))
	for i, key := range keys {
		ids[i] = key
	}

	switch obj := objectType.(type) {
	case models.DeploymentManager:
		return c.handleDeploymentManagerSyncCompletion(ctx, ids)
	case models.Location:
		return c.handleLocationSyncCompletion(ctx, keys)
	case models.OCloudSite:
		return c.handleOCloudSiteSyncCompletion(ctx, ids)
	case models.ResourcePool:
		return c.handleResourcePoolSyncCompletion(ctx, ids)
	case models.Resource:
		return c.handleResourceSyncCompletion(ctx, ids)
	default:
		return fmt.Errorf("unsupported sync completion type for '%T'", obj)
	}
}

// handleAsyncEvent receives and processes an async event received from a data source.
func (c *Collector) handleAsyncEvent(ctx context.Context, event *async.AsyncChangeEvent) error {
	if event.EventType == async.SyncComplete {
		// The watch is expired (e.g., the ResourceVersion is too old).  We need to re-sync and start again.
		return c.handleSyncCompletion(ctx, event.Object, event.Keys)
	}

	switch value := event.Object.(type) {
	case models.DeploymentManager:
		return c.handleAsyncDeploymentManagerEvent(ctx, value, event.EventType == async.Deleted)
	case models.Location:
		return c.handleAsyncLocationEvent(ctx, value, event.EventType == async.Deleted)
	case models.OCloudSite:
		return c.handleAsyncOCloudSiteEvent(ctx, value, event.EventType == async.Deleted)
	case models.ResourcePool:
		return c.handleAsyncResourcePoolEvent(ctx, value, event.EventType == async.Deleted)
	case models.ResourceType:
		return c.handleAsyncResourceTypeEvent(ctx, value, event.EventType == async.Deleted)
	case models.Resource:
		return c.handleAsyncResourceEvent(ctx, value, event.EventType == async.Deleted)
	default:
		return fmt.Errorf("unknown object type '%T'", event.Object)
	}
}

// handleAsyncDeploymentManagerEvent handles an async event received for a ClusterResource object.
func (c *Collector) handleAsyncDeploymentManagerEvent(ctx context.Context, deploymentManager models.DeploymentManager, deleted bool) error {
	var dataChangeEvent *models2.DataChangeEvent
	var err error
	if deleted {
		dataChangeEvent, err = svcutils.DeleteObjectWithChangeEvent(
			ctx, c.pool, deploymentManager, deploymentManager.DeploymentManagerID, nil, func(object interface{}) any {
				record, _ := object.(models.DeploymentManager)
				return models.DeploymentManagerToModel(&record, commonapi.NewDefaultFieldOptions())
			})

		if err != nil {
			return fmt.Errorf("failed to delete deployment manager '%s'': %w", deploymentManager.DeploymentManagerID, err)
		}
	} else {
		dataChangeEvent, err = svcutils.PersistObjectWithChangeEvent(
			ctx, c.pool, deploymentManager, deploymentManager.DeploymentManagerID, nil, func(object interface{}) any {
				record, _ := object.(models.DeploymentManager)
				return models.DeploymentManagerToModel(&record, commonapi.NewDefaultFieldOptions())
			})

		if err != nil {
			return fmt.Errorf("failed to update deployment manager '%s'': %w", deploymentManager.DeploymentManagerID, err)
		}
	}

	if dataChangeEvent != nil {
		c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
	}

	return nil
}

// locationAPIForChangeEvent builds LocationInfo for data-change notifications with oCloudSiteIds
// loaded from the repository, matching GET /locations/{globalLocationId}.
func (c *Collector) locationAPIForChangeEvent(ctx context.Context, record *models.Location) any {
	siteIDs, err := c.repository.GetOCloudSiteIDsForLocation(ctx, record.GlobalLocationID)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load O-Cloud site IDs for location change event",
			"globalLocationId", record.GlobalLocationID,
			"error", err)
		return models.LocationToModel(record, nil)
	}
	return models.LocationToModel(record, siteIDs)
}

// oCloudSiteAPIForChangeEvent builds OCloudSiteInfo for data-change notifications with resourcePools
// loaded from the repository, matching GET /oCloudSites/{oCloudSiteId}.
func (c *Collector) oCloudSiteAPIForChangeEvent(ctx context.Context, record *models.OCloudSite) any {
	poolIDs, err := c.repository.GetResourcePoolIDsForSite(ctx, record.OCloudSiteID)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load resource pool IDs for O-Cloud site change event",
			"oCloudSiteId", record.OCloudSiteID,
			"error", err)
		return models.OCloudSiteToModel(record, nil)
	}
	return models.OCloudSiteToModel(record, poolIDs)
}

// handleAsyncLocationEvent handles an async event received for a Location object.
func (c *Collector) handleAsyncLocationEvent(ctx context.Context, location models.Location, deleted bool) error {
	var dataChangeEvent *models2.DataChangeEvent
	var err error
	converter := func(object interface{}) any {
		record, _ := object.(models.Location)
		return c.locationAPIForChangeEvent(ctx, &record)
	}

	if deleted {
		dataChangeEvent, err = svcutils.DeleteObjectWithChangeEvent(
			ctx, c.pool, location, location.GlobalLocationID, nil, converter)
		if err != nil {
			return fmt.Errorf("failed to delete location '%s': %w", location.GlobalLocationID, err)
		}
	} else {
		dataChangeEvent, err = svcutils.PersistObjectWithChangeEvent(
			ctx, c.pool, location, location.GlobalLocationID, nil, converter)
		if err != nil {
			return fmt.Errorf("failed to persist location '%s': %w", location.GlobalLocationID, err)
		}
	}

	if dataChangeEvent != nil {
		c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
	}

	return nil
}

// handleAsyncOCloudSiteEvent handles an async event received for an OCloudSite object.
func (c *Collector) handleAsyncOCloudSiteEvent(ctx context.Context, site models.OCloudSite, deleted bool) error {
	var dataChangeEvent *models2.DataChangeEvent
	var err error

	if deleted {
		dataChangeEvent, err = svcutils.DeleteObjectWithChangeEvent(
			ctx, c.pool, site, site.OCloudSiteID, nil, func(object interface{}) any {
				record, _ := object.(models.OCloudSite)
				return c.oCloudSiteAPIForChangeEvent(ctx, &record)
			})

		if err != nil {
			return fmt.Errorf("failed to delete OCloudSite '%s': %w", site.OCloudSiteID, err)
		}
	} else {
		dataChangeEvent, err = svcutils.PersistObjectWithChangeEvent(
			ctx, c.pool, site, site.OCloudSiteID, nil, func(object interface{}) any {
				record, _ := object.(models.OCloudSite)
				return c.oCloudSiteAPIForChangeEvent(ctx, &record)
			})

		if err != nil {
			return fmt.Errorf("failed to persist OCloudSite '%s': %w", site.OCloudSiteID, err)
		}
	}

	if dataChangeEvent != nil {
		c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
	}

	return nil
}

// handleLocationSyncCompletion handles the sync completion for Location objects.
func (c *Collector) handleLocationSyncCompletion(ctx context.Context, keys []uuid.UUID) error {
	slog.Debug("Handling end of sync for Location instances", "count", len(keys))

	// Create a set of tracking UUIDs for fast lookup
	keySet := make(map[uuid.UUID]struct{}, len(keys))
	for _, key := range keys {
		keySet[key] = struct{}{}
	}

	// Get all locations and check if their tracking UUID is in the set
	locations, err := c.repository.GetLocations(ctx)
	if err != nil {
		return fmt.Errorf("failed to get locations: %w", err)
	}

	converter := func(object interface{}) any {
		record, _ := object.(models.Location)
		return c.locationAPIForChangeEvent(ctx, &record)
	}

	count := 0
	for _, location := range locations {
		trackingUUID := svcutils.GetTrackingUUID(location.GlobalLocationID)

		if _, exists := keySet[trackingUUID]; !exists {
			// This location is stale, delete it
			dataChangeEvent, deleteErr := svcutils.DeleteObjectWithChangeEvent(
				ctx, c.pool, location, location.GlobalLocationID, nil, converter)
			if deleteErr != nil {
				return fmt.Errorf("failed to delete stale location '%s': %w", location.GlobalLocationID, deleteErr)
			}

			if dataChangeEvent != nil {
				c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
			}
			count++
		}
	}

	if count > 0 {
		slog.Info("Deleted stale location records", "count", count)
	}

	return nil
}

// handleOCloudSiteSyncCompletion handles the sync completion for OCloudSite objects.
func (c *Collector) handleOCloudSiteSyncCompletion(ctx context.Context, ids []any) error {
	slog.Debug("Handling end of sync for OCloudSite instances", "count", len(ids))

	records, err := c.repository.GetOCloudSitesNotIn(ctx, ids)
	if err != nil {
		return fmt.Errorf("failed to get stale OCloudSites: %w", err)
	}

	count := 0
	for _, record := range records {
		dataChangeEvent, err := svcutils.DeleteObjectWithChangeEvent(ctx, c.pool, record, record.OCloudSiteID, nil, func(object interface{}) any {
			r, _ := object.(models.OCloudSite)
			return c.oCloudSiteAPIForChangeEvent(ctx, &r)
		})

		if err != nil {
			return fmt.Errorf("failed to delete stale OCloudSite: %w", err)
		}

		if dataChangeEvent != nil {
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
			count++
		}
	}

	if count > 0 {
		slog.Info("Deleted stale OCloudSite records", "count", count)
	}

	return nil
}

// handleAsyncResourcePoolEvent handles an async event received for a ResourcePool object.
func (c *Collector) handleAsyncResourcePoolEvent(ctx context.Context, pool models.ResourcePool, deleted bool) error {
	var dataChangeEvent *models2.DataChangeEvent
	var err error
	converter := func(object interface{}) any {
		record, _ := object.(models.ResourcePool)
		return models.ResourcePoolToModel(&record, nil)
	}

	if deleted {
		dataChangeEvent, err = svcutils.DeleteObjectWithChangeEvent(
			ctx, c.pool, pool, pool.ResourcePoolID, nil, converter)
		if err != nil {
			return fmt.Errorf("failed to delete ResourcePool '%s': %w", pool.ResourcePoolID, err)
		}
	} else {
		dataChangeEvent, err = svcutils.PersistObjectWithChangeEvent(
			ctx, c.pool, pool, pool.ResourcePoolID, nil, converter)
		if err != nil {
			return fmt.Errorf("failed to persist ResourcePool '%s': %w", pool.ResourcePoolID, err)
		}

		c.rebuildResourcesForPool(ctx, pool.Name)
	}

	if dataChangeEvent != nil {
		c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
	}

	return nil
}

// rebuildResourcesForPool asks data sources to re-evaluate resources that
// reference the given pool. This handles the case where BMH events arrive
// before the ResourcePool CR exists.
func (c *Collector) rebuildResourcesForPool(ctx context.Context, poolName string) {
	for _, ds := range c.dataSources {
		handler, ok := ds.(PoolChangeNotifier)
		if !ok {
			continue
		}

		results, err := handler.BuildResourcesForPool(ctx, poolName)
		if err != nil {
			slog.Error("failed to rebuild resources for pool", "pool", poolName, "error", err)
			continue
		}

		for i := range results {
			if err := c.handleAsyncResourceTypeEvent(ctx, results[i].ResourceType, false); err != nil {
				slog.Error("failed to persist resource type during pool rebuild",
					"pool", poolName, "error", err)
			}
			if err := c.handleAsyncResourceEvent(ctx, results[i].Resource, false); err != nil {
				slog.Error("failed to persist resource during pool rebuild",
					"pool", poolName, "error", err)
			}
		}

		if len(results) > 0 {
			slog.Info("Rebuilt resources for pool", "pool", poolName, "count", len(results))
		}
	}
}

// handleResourcePoolSyncCompletion handles the sync completion for ResourcePool objects.
func (c *Collector) handleResourcePoolSyncCompletion(ctx context.Context, ids []any) error {
	slog.Debug("Handling end of sync for ResourcePool instances", "count", len(ids))

	records, err := c.repository.GetResourcePoolsNotIn(ctx, ids)
	if err != nil {
		return fmt.Errorf("failed to get stale ResourcePools: %w", err)
	}

	count := 0
	for _, record := range records {
		dataChangeEvent, err := svcutils.DeleteObjectWithChangeEvent(ctx, c.pool, record, record.ResourcePoolID, nil, func(object interface{}) any {
			r, _ := object.(models.ResourcePool)
			return models.ResourcePoolToModel(&r, nil)
		})

		if err != nil {
			return fmt.Errorf("failed to delete stale ResourcePool: %w", err)
		}

		if dataChangeEvent != nil {
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
			count++
		}
	}

	if count > 0 {
		slog.Info("Deleted stale ResourcePool records", "count", count)
	}

	return nil
}

// convertCoordinateToGeoJSON converts CRD coordinate to GeoJSON Point format
// GeoJSON uses [longitude, latitude, altitude] order per RFC 7946
func convertCoordinateToGeoJSON(coord *inventoryv1alpha1.GeoLocation) (map[string]interface{}, error) {
	if coord == nil {
		// nil coordinate is valid (optional field), not an error condition
		return nil, nil //nolint:nilnil
	}

	lat, err := strconv.ParseFloat(coord.Latitude, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse latitude %q: %w", coord.Latitude, err)
	}

	lon, err := strconv.ParseFloat(coord.Longitude, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse longitude %q: %w", coord.Longitude, err)
	}

	coordinates := []float64{lon, lat} // GeoJSON: [longitude, latitude]

	if coord.Altitude != nil {
		alt, err := strconv.ParseFloat(*coord.Altitude, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse altitude %q: %w", *coord.Altitude, err)
		}
		coordinates = append(coordinates, alt)
	}

	return map[string]interface{}{
		"type":        "Point",
		"coordinates": coordinates,
	}, nil
}

// convertCivicAddress converts CRD civic address elements to database format
func convertCivicAddress(civic []inventoryv1alpha1.CivicAddressElement) []map[string]interface{} {
	if len(civic) == 0 {
		return nil
	}

	result := make([]map[string]interface{}, len(civic))
	for i, elem := range civic {
		result[i] = map[string]interface{}{
			"caType":  elem.CaType,
			"caValue": elem.CaValue,
		}
	}
	return result
}
