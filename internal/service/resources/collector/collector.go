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
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	commonapi "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
	models2 "github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/repo"
)

const pollingDelay = 1 * time.Minute

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

// ResourceDataSource defines an interface of a data source capable of getting handling Inventory resources.
type ResourceDataSource interface {
	DataSource
	GetResourcePools(ctx context.Context) ([]models.ResourcePool, error)
	GetResources(ctx context.Context, pools []models.ResourcePool) ([]models.Resource, error)
	MakeResourceType(resource *models.Resource) (*models.ResourceType, error)
}

// WatchableDataSource defines an interface of a data source capable of watching for async events.
type WatchableDataSource interface {
	Watch(ctx context.Context) error
}

// NotificationHandler defines an interface over which notifications are published.
type NotificationHandler interface {
	Notify(ctx context.Context, event *notifier.Notification)
}

// DataSourceLoader defines an interface to be used to load dynamic data sources
type DataSourceLoader interface {
	Load(ctx context.Context) ([]DataSource, error)
}

// Collector defines the attributes required by the collector implementation.
type Collector struct {
	pool                *pgxpool.Pool // Direct pool injection for transaction operations
	notificationHandler NotificationHandler
	repository          *repo.ResourcesRepository
	dataSources         []DataSource
	AsyncChangeEvents   chan *async.AsyncChangeEvent
	loader              DataSourceLoader
}

// NewCollector creates a new collector instance
func NewCollector(pool *pgxpool.Pool, repo *repo.ResourcesRepository, notificationHandler NotificationHandler, loader DataSourceLoader, dataSources []DataSource) *Collector {
	return &Collector{
		pool:                pool,
		repository:          repo,
		notificationHandler: notificationHandler,
		dataSources:         dataSources,
		loader:              loader,
		AsyncChangeEvents:   make(chan *async.AsyncChangeEvent, asyncEventBufferSize),
	}
}

// Run executes the collector main loop to gather data from external sources and writing to the database
func (c *Collector) Run(ctx context.Context) error {
	if err := c.init(ctx); err != nil {
		return err
	}

	if err := c.loadDynamicDataSources(ctx); err != nil {
		slog.Warn("failed to load dynamic data sources", "error", err)
		// this will get retried later
	}

	// Start listening for async events
	if err := c.watchForChanges(ctx); err != nil {
		return fmt.Errorf("failed to start listeners: %w", err)
	}

	// Run the initial data collection
	c.execute(ctx)

	for {
		select {
		// TODO: Add hook for new data sources from watch events
		case event := <-c.AsyncChangeEvents:
			if err := c.handleAsyncEvent(ctx, event); err != nil {
				slog.Error("failed to handle async change", "event", event, "error", err)
			}
		case <-time.After(pollingDelay):
			c.execute(ctx)
		case <-ctx.Done():
			slog.Info("Context terminated; collector exiting")
			return nil
		}
	}
}

// findDataSource looks up the data source by name and returns it; otherwise nil is returned
func (c *Collector) findDataSource(name string) DataSource {
	for _, dataSource := range c.dataSources {
		if dataSource.Name() == name {
			return dataSource
		}
	}
	return nil
}

// loadDynamicDataSources attempts to load any dynamic data sources that aren't necessarily known at init time.
func (c *Collector) loadDynamicDataSources(ctx context.Context) error {
	slog.Info("Loading dynamic data sources")
	result, err := c.loader.Load(ctx)
	if err != nil {
		return fmt.Errorf("failed to load data sources: %w", err)
	}

	count := 0
	for _, dataSource := range result {
		if found := c.findDataSource(dataSource.Name()); found != nil {
			continue
		}

		err = c.initDataSource(ctx, dataSource)
		if err != nil {
			return fmt.Errorf("failed to initialize dynamic data source %s: %w", dataSource.Name(), err)
		}

		c.dataSources = append(c.dataSources, dataSource)
		slog.Info("Data source dynamically loaded", "name", dataSource.Name())
		count++
	}

	slog.Info("Loaded dynamic data sources", "count", count)
	return nil
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
			slog.Error("failed to watch for changes", "source", d.Name(), "error", err)
			return fmt.Errorf("failed to watch for changes: %w", err)
		}
	}
	return nil
}

// execute runs a single iteration of the main loop.  It does not return an error because all errors should be handled
// gracefully.  If a truly unrecoverable error happens then a panic should be used to restart the process.
func (c *Collector) execute(ctx context.Context) {
	slog.Debug("collector loop running", "sources", len(c.dataSources))

	if err := c.loadDynamicDataSources(ctx); err != nil {
		slog.Warn("failed to load dynamic data sources", "error", err)
		// this will get retried later
	}

	for _, d := range c.dataSources {
		rd, ok := d.(ResourceDataSource)
		if !ok {
			continue
		}

		d.IncrGenerationID()
		slog.Debug("collecting data from data source", "source", d.Name(), "generationID", d.GetGenerationID())
		if err := c.executeOneDataSource(ctx, rd); err != nil {
			slog.Warn("failed to collect data from data source", "source", d.Name(), "error", err)
		} else {
			slog.Debug("collected data from data source", "source", d.Name())
		}
	}
	slog.Debug("collector loop complete", "sources", len(c.dataSources))
}

func (c *Collector) purgeStaleResources(ctx context.Context, dataSource DataSource) (int, error) {
	resources, err := c.repository.FindStaleResources(ctx, dataSource.GetID(), dataSource.GetGenerationID())
	if err != nil {
		return 0, fmt.Errorf("failed to find stale resources: %w", err)
	}

	count := 0
	for _, resource := range resources {
		dataChangeEvent, err := svcutils.DeleteObjectWithChangeEvent(ctx, c.pool, resource, resource.ResourceID,
			&resource.ResourcePoolID, func(object interface{}) any {
				r, _ := object.(models.Resource)
				return models.ResourceToModel(&r, nil)
			})
		if err != nil {
			return count, fmt.Errorf("failed to delete stale resource: %w", err)
		}
		if dataChangeEvent != nil {
			count++
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
		}
	}

	if count > 0 {
		slog.Info("Purged stale resources", "count", count)
	}

	return count, nil
}

func (c *Collector) purgeStaleResourcePools(ctx context.Context, dataSource DataSource) (int, error) {
	pools, err := c.repository.FindStaleResourcePools(ctx, dataSource.GetID(), dataSource.GetGenerationID())
	if err != nil {
		return 0, fmt.Errorf("failed to find stale resources: %w", err)
	}

	count := 0
	for _, pool := range pools {
		dataChangeEvent, err := svcutils.DeleteObjectWithChangeEvent(ctx, c.pool, pool, pool.ResourcePoolID,
			nil, func(object interface{}) any {
				r, _ := object.(models.ResourcePool)
				return models.ResourcePoolToModel(&r, commonapi.NewDefaultFieldOptions())
			})
		if err == nil {
			return count, fmt.Errorf("failed to delete stale resource pool: %w", err)
		}
		if dataChangeEvent != nil {
			count++
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
		}
	}

	if count > 0 {
		slog.Info("Purged stale resource pool", "count", count)
	}

	return count, nil
}

func (c *Collector) purgeStaleResourceTypes(ctx context.Context, dataSource DataSource) (int, error) {
	pools, err := c.repository.FindStaleResourcePools(ctx, dataSource.GetID(), dataSource.GetGenerationID())
	if err != nil {
		return 0, fmt.Errorf("failed to find stale resource types: %w", err)
	}

	count := 0
	for _, pool := range pools {
		dataChangeEvent, err := svcutils.DeleteObjectWithChangeEvent(ctx, c.pool, pool, pool.ResourcePoolID,
			nil, func(object interface{}) any {
				r, _ := object.(models.ResourcePool)
				return models.ResourcePoolToModel(&r, commonapi.NewDefaultFieldOptions())
			})
		if err == nil {
			return count, fmt.Errorf("failed to delete stale resource type: %w", err)
		}
		if dataChangeEvent != nil {
			count++
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
		}
	}

	if count > 0 {
		slog.Info("Purged stale resource types", "count", count)
	}

	return count, nil
}

// purgeStaleData removes any records that have a generation id older than the generation id of the data source which
// created it.
func (c *Collector) purgeStaleData(ctx context.Context, dataSource DataSource) error {
	slog.Info("Purging stale data", "source", dataSource.Name())

	total := 0
	count := 0

	count, err := c.purgeStaleResources(ctx, dataSource)
	if err != nil {
		return fmt.Errorf("failed to purge stale resources': %w", err)
	}
	total += count

	count, err = c.purgeStaleResourcePools(ctx, dataSource)
	if err != nil {
		return fmt.Errorf("failed to purge stale resource pools: %w", err)
	}
	total += count

	count, err = c.purgeStaleResourceTypes(ctx, dataSource)
	if err != nil {
		return fmt.Errorf("failed to purge stale resource types: %w", err)
	}
	total += count

	slog.Info("Purged stale data", "source", dataSource.Name(), "count", total)

	return nil
}

// executeOneDataSource runs a single iteration of the main loop for a specific data source instance.
func (c *Collector) executeOneDataSource(ctx context.Context, dataSource ResourceDataSource) (err error) {
	// TODO: Add code to retrieve alarm dictionaries

	// Get the list of resource pools for this data source
	pools, err := c.collectResourcePools(ctx, dataSource)
	if err != nil {
		return fmt.Errorf("failed to collect resource pools: %w", err)
	}

	// Get the list of resources for this data source
	_, err = c.collectResources(ctx, dataSource, pools)
	if err != nil {
		return fmt.Errorf("failed to collect resources: %w", err)
	}

	// Persist data source info
	id := dataSource.GetID()
	_, err = c.repository.UpdateDataSource(ctx, &models2.DataSource{
		DataSourceID: &id,
		Name:         dataSource.Name(),
		GenerationID: dataSource.GetGenerationID(),
	})
	if err != nil {
		return fmt.Errorf("failed to update data source %q: %w", dataSource.Name(), err)
	}

	err = c.purgeStaleData(ctx, dataSource)
	if err != nil {
		return fmt.Errorf("failed to purge stale data from '%s': %w", dataSource.Name(), err)
	}

	return nil
}

// collectResources collects Resource objects from the data source, persists them to the database,
// and signals any change events to the notification processor.
func (c *Collector) collectResources(ctx context.Context, dataSource ResourceDataSource,
	pools []models.ResourcePool) ([]models.Resource, error) {
	slog.Debug("collecting resource and types", "source", dataSource.Name())

	resources, err := dataSource.GetResources(ctx, pools)
	if err != nil {
		return nil, fmt.Errorf("failed to get resources: %w", err)
	}

	// Fetch all alarm dictionaries and build a map for use in notifications
	alarmDictMap, err := c.buildAlarmDictionaryMap(ctx)
	if err != nil {
		slog.Warn("failed to fetch alarm dictionaries for notifications", "error", err)
		// Continue without alarm dictionaries - notifications will have nil alarmDictionary
		alarmDictMap = make(map[string]*common.AlarmDictionary)
	}

	// Loop over the set of resources and create the associated resource types.
	seen := make(map[uuid.UUID]bool)
	for _, resource := range resources {
		resourceType, err := dataSource.MakeResourceType(&resource)
		if err != nil {
			return nil, fmt.Errorf("failed to make resource type from '%v': %w", resource, err)
		}

		if seen[resourceType.ResourceTypeID] {
			// We have already seen this one so skip
			continue
		}
		seen[resourceType.ResourceTypeID] = true

		// Capture alarm dictionary for this resource type (may be nil if not found)
		alarmDict := alarmDictMap[resourceType.ResourceTypeID.String()]

		dataChangeEvent, err := svcutils.PersistObjectWithChangeEvent(
			ctx, c.pool, *resourceType, resourceType.ResourceTypeID, nil, func(object interface{}) any {
				record, _ := object.(models.ResourceType)
				return models.ResourceTypeToModel(&record, alarmDict)
			})
		if err != nil {
			return nil, fmt.Errorf("failed to persist resource type': %w", err)
		}

		if dataChangeEvent != nil {
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
		}
	}

	// Loop over the set of resources and insert (or update) as needed
	for _, resource := range resources {
		dataChangeEvent, err := svcutils.PersistObjectWithChangeEvent(
			ctx, c.pool, resource, resource.ResourceID, &resource.ResourcePoolID, func(object interface{}) any {
				record, _ := object.(models.Resource)
				return models.ResourceToModel(&record, nil)
			})
		if err != nil {
			return nil, fmt.Errorf("failed to persist resource: %w", err)
		}

		if dataChangeEvent != nil {
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
		}
	}

	return resources, nil
}

// buildAlarmDictionaryMap fetches all alarm dictionaries and their definitions,
// and returns a map keyed by resource type ID for efficient lookup.
func (c *Collector) buildAlarmDictionaryMap(ctx context.Context) (map[string]*common.AlarmDictionary, error) {
	alarmDictionaries, err := c.repository.GetAlarmDictionaries(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get alarm dictionaries: %w", err)
	}

	result := make(map[string]*common.AlarmDictionary)
	for _, dict := range alarmDictionaries {
		definitions, err := c.repository.GetAlarmDefinitionsByAlarmDictionaryID(ctx, dict.AlarmDictionaryID)
		if err != nil {
			slog.Warn("failed to get alarm definitions", "alarmDictionaryId", dict.AlarmDictionaryID, "error", err)
			continue
		}
		converted := models.AlarmDictionaryToModel(&dict, definitions)
		result[dict.ResourceTypeID.String()] = &converted
	}

	return result, nil
}

// collectResourcePools collects ResourcePool objects from the data source, persists them to the database,
// and signals any change events to the notification processor.
func (c *Collector) collectResourcePools(ctx context.Context, dataSource ResourceDataSource) ([]models.ResourcePool, error) {
	slog.Debug("collecting resource pools", "source", dataSource.Name())

	pools, err := dataSource.GetResourcePools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource pools: %w", err)
	}

	// Loop over the set of resource pools and insert (or update) as needed
	for _, pool := range pools {
		dataChangeEvent, err := svcutils.PersistObjectWithChangeEvent(
			ctx, c.pool, pool, pool.ResourcePoolID, nil, func(object interface{}) any {
				record, _ := object.(models.ResourcePool)
				return models.ResourcePoolToModel(&record, commonapi.NewDefaultFieldOptions())
			})
		if err != nil {
			return nil, fmt.Errorf("failed to persist resource pool: %w", err)
		}

		if dataChangeEvent != nil {
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
		}
	}

	return pools, nil
}

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
