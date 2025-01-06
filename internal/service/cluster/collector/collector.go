package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/db/repo"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
	models2 "github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

const pollingDelay = 10 * time.Minute

// clusterNameExtension represents a mandatory extension that data sources must add to ClusterResource objects to
// identify their parent NodeCluster.  The term "mandatory" here doesn't refer to the definition of the spec; only that
// internally we rely on it being present to find the node cluster ID value using its name value. We actually delete
// this extension and do not expose it to the API.
const clusterNameExtension = "clusterName"

// asyncEventBufferSize defines the number of buffered entries in the async handleWatchEvent channel
const asyncEventBufferSize = 10

// DataSource represents the operations required to be supported by any objects implementing a
// data collection backend.
type DataSource interface {
	Name() string
	GetID() uuid.UUID
	Init(dataSourceID uuid.UUID, generationID int, asyncEventChannel chan<- *async.AsyncChangeEvent)
	GetGenerationID() int
	IncrGenerationID() int
}

// ClusterDataSource defines an interface of a data source capable of getting handling Cluster resources.
type ClusterDataSource interface {
	DataSource
	MakeNodeClusterType(resource *models.NodeCluster) (*models.NodeClusterType, error)
	MakeClusterResourceType(resource *models.ClusterResource) (*models.ClusterResourceType, error)
}

// WatchableDataSource defines an interface of a data source capable of watching for async events.
type WatchableDataSource interface {
	Watch(ctx context.Context) error
}

// NotificationHandler defines an interface over which notifications are published.
type NotificationHandler interface {
	Notify(ctx context.Context, event *notifier.Notification)
}

// Collector defines the attributes required by the collector implementation.
type Collector struct {
	notificationHandler NotificationHandler
	repository          *repo.ClusterRepository
	dataSources         []DataSource
	asyncChangeEvents   chan *async.AsyncChangeEvent
}

// NewCollector creates a new collector instance
func NewCollector(repo *repo.ClusterRepository, notificationHandler NotificationHandler, dataSources []DataSource) *Collector {
	return &Collector{
		repository:          repo,
		notificationHandler: notificationHandler,
		dataSources:         dataSources,
		asyncChangeEvents:   make(chan *async.AsyncChangeEvent, asyncEventBufferSize),
	}
}

// Run executes the collector main loop to gather data from external sources and writing to the database
func (c *Collector) Run(ctx context.Context) error {
	if err := c.init(ctx); err != nil {
		return err
	}

	// Run the initial data collection
	c.execute(ctx)

	// Start listening for async events
	if err := c.watchForChanges(ctx); err != nil {
		return fmt.Errorf("failed to start watchers: %w", err)
	}

	for {
		select {
		case event := <-c.asyncChangeEvents:
			if err := c.handleAsyncEvent(ctx, event); err != nil {
				slog.Error("failed to handle async change", "handleWatchEvent", event, "error", err)
			}
		case <-time.After(pollingDelay):
			c.execute(ctx)
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
	case errors.Is(err, utils.ErrNotFound):
		// Doesn't exist so create it now.
		result, err := c.repository.CreateDataSource(ctx, &models2.DataSource{
			Name:         name,
			GenerationID: dataSource.GetGenerationID(),
		})
		if err != nil {
			return fmt.Errorf("failed to create new data source %q: %w", name, err)
		}

		dataSource.Init(*result.DataSourceID, 0, c.asyncChangeEvents)
		slog.Info("created new data source", "name", name, "uuid", *result.DataSourceID)
	case err != nil:
		return fmt.Errorf("failed to get data source %q: %w", name, err)
	default:
		dataSource.Init(*record.DataSourceID, record.GenerationID, c.asyncChangeEvents)
		slog.Info("restored data source",
			"name", name, "uuid", record.DataSourceID, "generation", record.GenerationID)
	}

	return nil
}

// watchForChanges starts a watch listener for each data source.  Data is reported by via the channel provided to
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
	for _, d := range c.dataSources {
		// TODO: only run this loop for alarm data sources

		d.IncrGenerationID()
		slog.Debug("collecting data from data source", "source", d.Name(), "generationID", d.GetGenerationID())
		if err := c.executeOneDataSource(ctx, d); err != nil {
			slog.Warn("failed to collect data from data source", "source", d.Name(), "error", err)
		} else {
			slog.Debug("collected data from data source", "source", d.Name())
		}
	}
	slog.Debug("collector loop complete", "sources", len(c.dataSources))
}

// executeOneDataSource runs a single iteration of the main loop for a specific data source instance.
func (c *Collector) executeOneDataSource(ctx context.Context, dataSource DataSource) (err error) {
	// TODO: Add code to retrieve alarm dictionaries

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

	// TODO: purge stale record

	return nil
}

// persistClusterResource persists a ClusterResource to the database.  This may be an add or an update.
func (c *Collector) persistClusterResource(ctx context.Context, dataSource ClusterDataSource, resource models.ClusterResource, seen map[uuid.UUID]bool) error {
	resourceType, err := dataSource.MakeClusterResourceType(&resource)
	if err != nil {
		return fmt.Errorf("failed to make cluster resource type from '%v': %w", resource, err)
	}

	if !seen[resourceType.ClusterResourceTypeID] {
		seen[resourceType.ClusterResourceTypeID] = true

		dataChangeEvent, err := utils.PersistObjectWithChangeEvent(
			ctx, c.repository.Db, *resourceType, resourceType.ClusterResourceTypeID, nil, func(object interface{}) any {
				record, _ := object.(models.ClusterResourceType)
				return models.ClusterResourceTypeToModel(&record)
			})
		if err != nil {
			return fmt.Errorf("failed to persist cluster resource type': %w", err)
		}

		if dataChangeEvent != nil {
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
		}
	}

	dataChangeEvent, err := utils.PersistObjectWithChangeEvent(
		ctx, c.repository.Db, resource, resource.ClusterResourceID, nil, func(object interface{}) any {
			record, _ := object.(models.ClusterResource)
			return models.ClusterResourceToModel(&record)
		})
	if err != nil {
		return fmt.Errorf("failed to persist cluster resource: %w", err)
	}

	if dataChangeEvent != nil {
		c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
	}

	return nil
}

// persistNodeCluster persists a NodeCluster to the database.  This may be an add or an update.
func (c *Collector) persistNodeCluster(ctx context.Context, dataSource ClusterDataSource, cluster models.NodeCluster, seen map[uuid.UUID]bool) error {
	resourceType, err := dataSource.MakeNodeClusterType(&cluster)
	if err != nil {
		return fmt.Errorf("failed to make node cluster type from '%s': %w", cluster.Name, err)
	}

	if !seen[resourceType.NodeClusterTypeID] {
		seen[resourceType.NodeClusterTypeID] = true
		dataChangeEvent, err := utils.PersistObjectWithChangeEvent(
			ctx, c.repository.Db, *resourceType, resourceType.NodeClusterTypeID, nil, func(object interface{}) any {
				record, _ := object.(models.NodeClusterType)
				return models.NodeClusterTypeToModel(&record)
			})
		if err != nil {
			return fmt.Errorf("failed to persist node cluster type': %w", err)
		}

		if dataChangeEvent != nil {
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
		}
	}

	dataChangeEvent, err := utils.PersistObjectWithChangeEvent(
		ctx, c.repository.Db, cluster, cluster.NodeClusterID, nil, func(object interface{}) any {
			record, _ := object.(models.NodeCluster)
			return models.NodeClusterToModel(&record, nil)
		})
	if err != nil {
		return fmt.Errorf("failed to persist node cluster: %w", err)
	}

	if dataChangeEvent != nil {
		c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
	}

	return nil
}

// findDataSource looks up the data source by ID value.  It returns nil if no matching data source was found.
func (c *Collector) findDataSource(dataSourceID uuid.UUID) DataSource {
	for _, dataSource := range c.dataSources {
		if dataSource.GetID() == dataSourceID {
			return dataSource
		}
	}
	return nil
}

// handleNodeClusterSyncCompletion handles the end of sync for NodeCluster objects.  It deletes any NodeCluster objects
// not included in the set of keys received during the sync operation.
func (c *Collector) handleNodeClusterSyncCompletion(ctx context.Context, ids []any) error {
	records, err := c.repository.GetNodeClustersNotIn(ctx, ids)
	if err != nil {
		return fmt.Errorf("failed to get stale node clusters: %w", err)
	}

	count := 0
	for _, record := range records {
		dataChangeEvent, err := utils.DeleteObjectWithChangeEvent(ctx, c.repository.Db, record, record.NodeClusterID, nil, func(object interface{}) any {
			r, _ := object.(models.NodeCluster)
			return models.NodeClusterToModel(&r, nil)
		})

		if err != nil {
			return fmt.Errorf("failed to delete stale node cluster: %w", err)
		}

		if dataChangeEvent != nil {
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
			count++
		}
	}

	if count > 0 {
		slog.Info("Deleted stale node cluster records", "count", count)
	}

	return nil
}

// handleClusterResourceSyncCompletion handles the end of sync for ClusterResource objects.  It deletes any
// ClusterResource objects not included in the set of keys received during the sync operation.
func (c *Collector) handleClusterResourceSyncCompletion(ctx context.Context, ids []any) error {
	records, err := c.repository.GetClusterResourcesNotIn(ctx, ids)
	if err != nil {
		return fmt.Errorf("failed to get stale cluster resources: %w", err)
	}

	count := 0
	for _, record := range records {
		dataChangeEvent, err := utils.DeleteObjectWithChangeEvent(ctx, c.repository.Db, record, record.NodeClusterID, nil, func(object interface{}) any {
			r, _ := object.(models.ClusterResource)
			return models.ClusterResourceToModel(&r)
		})

		if err != nil {
			return fmt.Errorf("failed to delete stale cluster resource: %w", err)
		}

		if dataChangeEvent != nil {
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
			count++
		}
	}

	if count > 0 {
		slog.Info("Deleted stale cluster resource records", "count", count)
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
	case models.NodeCluster:
		return c.handleNodeClusterSyncCompletion(ctx, ids)
	case models.ClusterResource:
		return c.handleClusterResourceSyncCompletion(ctx, ids)
	default:
		return fmt.Errorf("unsupported sync completion type for '%T'", obj)
	}
}

// handleAsyncEvent receives and processes an async event received from a data source.
func (c *Collector) handleAsyncEvent(ctx context.Context, event *async.AsyncChangeEvent) error {
	s := c.findDataSource(event.DataSourceID)
	if s == nil {
		return fmt.Errorf("data source '%s' not found", event.DataSourceID)
	}

	dataSource, ok := s.(ClusterDataSource)
	if !ok {
		return fmt.Errorf("data source '%s' is not a ClusterDataSource", event.DataSourceID)
	}

	if event.EventType == async.SyncComplete {
		// The watch is expired (e.g., the ResourceVersion is too old).  We need to re-sync and start again.
		return c.handleSyncCompletion(ctx, event.Object, event.Keys)
	}

	switch value := event.Object.(type) {
	case models.NodeCluster:
		return c.handleAsyncNodeClusterEvent(ctx, dataSource, value, event.EventType == async.Deleted)
	case models.ClusterResource:
		return c.handleAsyncClusterResourceEvent(ctx, dataSource, value, event.EventType == async.Deleted)
	default:
		return fmt.Errorf("unknown object type '%T'", event.Object)
	}
}

// deleteRelatedClusterResources deletes all related ClusterResource objects prior to deleting a NodeCluster object.
// This is done explicitly rather than via a cascade action in the database so that we can send the inventory
// notifications for each of the deleted ClusterResource instances.
func (c *Collector) deleteRelatedClusterResources(ctx context.Context, nodeCluster models.NodeCluster) error {
	resources, err := c.repository.GetNodeClusterResources(ctx, nodeCluster.NodeClusterID)
	if err != nil {
		return fmt.Errorf("failed to get cluster resources for node_cluster_id %s: %w", nodeCluster.NodeClusterID, err)
	}

	slog.Debug("deleting related cluster resources", "node_cluster_id", nodeCluster.NodeClusterID, "count", len(resources))

	for _, resource := range resources {
		dataChangeEvent, err := utils.DeleteObjectWithChangeEvent(
			ctx, c.repository.Db, resource, resource.ClusterResourceID, &nodeCluster.NodeClusterID, func(object interface{}) any {
				record, _ := object.(models.ClusterResource)
				return models.ClusterResourceToModel(&record)
			})

		if err != nil {
			return fmt.Errorf("failed to delete cluster resource '%s': %w", resource.ClusterResourceID, err)
		}

		if dataChangeEvent != nil {
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
		}
	}

	return nil
}

// handleAsyncNodeClusterEvent handles an async handleWatchEvent received for a NodeCluster object.
func (c *Collector) handleAsyncNodeClusterEvent(ctx context.Context, dataSource ClusterDataSource, nodeCluster models.NodeCluster, deleted bool) error {
	if deleted {
		// Handle the NodeCluster deletion, but first delete all subtending ClusterResources
		if err := c.deleteRelatedClusterResources(ctx, nodeCluster); err != nil {
			return err
		}

		dataChangeEvent, err := utils.DeleteObjectWithChangeEvent(
			ctx, c.repository.Db, nodeCluster, nodeCluster.NodeClusterID, nil, func(object interface{}) any {
				record, _ := object.(models.NodeCluster)
				return models.NodeClusterToModel(&record, nil)
			})

		if err != nil {
			return fmt.Errorf("failed to delete node cluster '%s'': %w", nodeCluster.NodeClusterID, err)
		}

		if dataChangeEvent != nil {
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
		}

		return nil
	}

	err := c.persistNodeCluster(ctx, dataSource, nodeCluster, map[uuid.UUID]bool{})
	if err != nil {
		return fmt.Errorf("failed to update node cluster '%s'': %w", nodeCluster.NodeClusterID, err)
	}

	return err
}

// handleAsyncClusterResourceEvent handles an async handleWatchEvent received for a ClusterResource object.
func (c *Collector) handleAsyncClusterResourceEvent(ctx context.Context, dataSource ClusterDataSource, clusterResource models.ClusterResource, deleted bool) error {
	// The ClusterResource object has a link to its parent object via the NodeClusterID attribute, but unfortunately, that
	// piece of data does not appear in the backing data, so we need to augment it ourselves.
	if clusterResource.Extensions != nil {
		clusterName := (*clusterResource.Extensions)[clusterNameExtension].(string)
		nodeCluster, err := c.repository.GetNodeClusterByName(ctx, clusterName)
		if errors.Is(err, utils.ErrNotFound) {
			slog.Warn("no node cluster found", "name", clusterName)
			// This is likely a race condition in which we are processing a cluster resource object for which the cluster
			// has already been deleted or is not yet present.
			return nil
		}
		clusterResource.NodeClusterID = nodeCluster.NodeClusterID
		// The name extension was added for the sole purpose of allowing us to find the matching cluster ID value
		// since it is not possible for the data source to do this directly.  Removing it since it is not required
		// by the spec and seems redundant since the full NodeCluster can be retrieved with the ID value.
		delete(*clusterResource.Extensions, clusterNameExtension)
	}

	if deleted {
		dataChangeEvent, err := utils.DeleteObjectWithChangeEvent(
			ctx, c.repository.Db, clusterResource, clusterResource.ClusterResourceID, nil, func(object interface{}) any {
				record, _ := object.(models.NodeCluster)
				return models.NodeClusterToModel(&record, nil)
			})

		if err != nil {
			return fmt.Errorf("failed to delete cluster resource '%s'': %w", clusterResource.ResourceID, err)
		}

		if dataChangeEvent != nil {
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
		}

		return nil
	}

	err := c.persistClusterResource(ctx, dataSource, clusterResource, map[uuid.UUID]bool{})
	if err != nil {
		return fmt.Errorf("failed to update cluster resource '%s'': %w", clusterResource.ResourceID, err)
	}

	return nil
}
