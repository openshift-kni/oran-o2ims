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

// DataSource represents the operations required to be supported by any objects implementing a
// data collection backend.
type DataSource interface {
	Name() string
	SetID(uuid.UUID)
	SetGenerationID(value int)
	GetGenerationID() int
	IncrGenerationID() int
	GetNodeClusters(ctx context.Context) ([]models.NodeCluster, error)
	GetClusterResources(ctx context.Context, clusters []models.NodeCluster) ([]models.ClusterResource, error)
	MakeNodeClusterType(resource *models.NodeCluster) (*models.NodeClusterType, error)
	MakeClusterResourceType(resource *models.ClusterResource) (*models.ClusterResourceType, error)
}

type NotificationHandler interface {
	Notify(ctx context.Context, event *notifier.Notification)
}

// Collector defines the attributes required by the collector implementation.
type Collector struct {
	notificationHandler NotificationHandler
	repository          *repo.ClusterRepository
	dataSources         []DataSource
}

// NewCollector creates a new collector instance
func NewCollector(repo *repo.ClusterRepository, notificationHandler NotificationHandler, dataSources []DataSource) *Collector {
	return &Collector{
		repository:          repo,
		notificationHandler: notificationHandler,
		dataSources:         dataSources,
	}
}

// Run executes the collector main loop to gather data from external sources and writing to the database
func (c *Collector) Run(ctx context.Context) error {
	if err := c.init(ctx); err != nil {
		return err
	}

	// Run the initial data collection
	c.execute(ctx)

	for {
		select {
		// TODO: Add hook for new data sources from watch events
		// TODO: Add hook for kick to run collection based on watch events on individual data sources
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

		dataSource.SetID(*result.DataSourceID)
		slog.Info("created new data source", "name", name, "uuid", *result.DataSourceID)
	case err != nil:
		return fmt.Errorf("failed to get data source %q: %w", name, err)
	default:
		dataSource.SetID(*record.DataSourceID)
		dataSource.SetGenerationID(record.GenerationID)
		slog.Info("restored data source",
			"name", name, "uuid", record.DataSourceID, "generation", record.GenerationID)
	}
	return nil
}

// execute runs a single iteration of the main loop.  It does not return an error because all errors should be handled
// gracefully.  If a truly unrecoverable error happens then a panic should be used to restart the process.
func (c *Collector) execute(ctx context.Context) {
	slog.Debug("collector loop running", "sources", len(c.dataSources))
	for _, d := range c.dataSources {
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
	// Get the list of node cluster for this data source
	pools, err := c.collectNodeClusters(ctx, dataSource)
	if err != nil {
		return fmt.Errorf("failed to collect node clusters: %w", err)
	}

	// Get the list of cluster resources for this data source
	_, err = c.collectClusterResources(ctx, dataSource, pools)
	if err != nil {
		return fmt.Errorf("failed to collect cluster resources: %w", err)
	}

	// TODO: purge stale record

	// TODO: persist data source info

	return nil
}

// collectClusterResources collects ClusterResource objects from the data source, persists them to
// the database, and signals any change events to the notification processor.
func (c *Collector) collectClusterResources(ctx context.Context, dataSource DataSource,
	clusters []models.NodeCluster) ([]models.ClusterResource, error) {
	slog.Debug("collecting cluster resources and types", "source", dataSource.Name())

	resources, err := dataSource.GetClusterResources(ctx, clusters)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster resources: %w", err)
	}

	// Build a mapping from name to id for clusters so that we can more easily access id values
	clusterNameToID := make(map[string]uuid.UUID)
	for _, cluster := range clusters {
		clusterNameToID[cluster.Name] = cluster.NodeClusterID
	}

	// Loop over the set of cluster resources and create the associated cluster resource types.
	seen := make(map[uuid.UUID]bool)
	for _, resource := range resources {
		resourceType, err := dataSource.MakeClusterResourceType(&resource)
		if err != nil {
			return nil, fmt.Errorf("failed to make cluster resource type from '%v': %w", resource, err)
		}

		if seen[resourceType.ClusterResourceTypeID] {
			// We have already seen this one so skip
			continue
		}
		seen[resourceType.ClusterResourceTypeID] = true

		dataChangeEvent, err := utils.PersistObjectWithChangeEvent(
			ctx, c.repository.Db, *resourceType, resourceType.ClusterResourceTypeID, nil, func(object interface{}) any {
				record, _ := object.(models.ClusterResourceType)
				return models.ClusterResourceTypeToModel(&record)
			})
		if err != nil {
			return nil, fmt.Errorf("failed to persist cluster resource type': %w", err)
		}

		if dataChangeEvent != nil {
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
		}
	}

	// Loop over the set of cluster resources and insert (or update) as needed
	for _, resource := range resources {
		// Set the cluster ID since the data source doesn't have access to this info
		if resource.Extensions != nil {
			clusterName := (*resource.Extensions)[clusterNameExtension].(string)
			resource.NodeClusterID = clusterNameToID[clusterName]
			// The name extension was added for the sole purpose of allowing us to find the matching cluster ID value
			// since it is not possible for the data source to do this directly.  Removing it since it is not required
			// by the spec and seems redundant since the full NodeCluster can be retrieved with the ID value.
			delete(*resource.Extensions, clusterNameExtension)
		}

		dataChangeEvent, err := utils.PersistObjectWithChangeEvent(
			ctx, c.repository.Db, resource, resource.ClusterResourceID, nil, func(object interface{}) any {
				record, _ := object.(models.ClusterResource)
				return models.ClusterResourceToModel(&record)
			})
		if err != nil {
			return nil, fmt.Errorf("failed to persist cluster resource: %w", err)
		}

		if dataChangeEvent != nil {
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
		}
	}

	return resources, nil
}

// collectNodeClusters collects NodeCluster objects from the data source, persists them to
// the database, and signals any change events to the notification processor.
func (c *Collector) collectNodeClusters(ctx context.Context, dataSource DataSource) ([]models.NodeCluster, error) {
	slog.Debug("collecting node clusters and types", "source", dataSource.Name())

	clusters, err := dataSource.GetNodeClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get node clsuters: %w", err)
	}

	// Loop over the set of resources and create the associated resource types.
	seen := make(map[uuid.UUID]bool)
	for _, cluster := range clusters {
		resourceType, err := dataSource.MakeNodeClusterType(&cluster)
		if err != nil {
			return nil, fmt.Errorf("failed to make node cluster type from '%v': %w", cluster, err)
		}

		if seen[resourceType.NodeClusterTypeID] {
			// We have already seen this one so skip
			continue
		}
		seen[resourceType.NodeClusterTypeID] = true

		dataChangeEvent, err := utils.PersistObjectWithChangeEvent(
			ctx, c.repository.Db, *resourceType, resourceType.NodeClusterTypeID, nil, func(object interface{}) any {
				record, _ := object.(models.NodeClusterType)
				return models.NodeClusterTypeToModel(&record)
			})
		if err != nil {
			return nil, fmt.Errorf("failed to persist node cluster type': %w", err)
		}

		if dataChangeEvent != nil {
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
		}
	}

	// Loop over the set of node cluster and insert (or update) as needed
	for _, cluster := range clusters {
		dataChangeEvent, err := utils.PersistObjectWithChangeEvent(
			ctx, c.repository.Db, cluster, cluster.NodeClusterID, nil, func(object interface{}) any {
				record, _ := object.(models.NodeCluster)
				return models.NodeClusterToModel(&record, nil)
			})
		if err != nil {
			return nil, fmt.Errorf("failed to persist node cluster: %w", err)
		}

		if dataChangeEvent != nil {
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
		}
	}

	return clusters, nil
}
