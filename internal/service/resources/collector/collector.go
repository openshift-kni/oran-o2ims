package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	models2 "github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/repo"
)

const pollingDelay = 10 * time.Minute

// DataSource represents the operations required to be supported by any objects implementing a
// data collection backend.
type DataSource interface {
	Name() string
	SetID(uuid.UUID)
	SetGenerationID(value int)
	GetGenerationID() int
	IncrGenerationID() int
	GetResourcePools(ctx context.Context) ([]models.ResourcePool, error)
	GetResources(ctx context.Context, pools []models.ResourcePool) ([]models.Resource, error)
	GetDeploymentManagers(ctx context.Context) ([]models.DeploymentManager, error)
	MakeResourceType(resource *models.Resource) (*models.ResourceType, error)
}

type NotificationHandler interface {
	Notify(ctx context.Context, event *notifier.Notification)
}

// Collector defines the attributes required by the collector implementation.
type Collector struct {
	notificationHandler NotificationHandler
	repository          *repo.ResourcesRepository
	dataSources         []DataSource
}

// NewCollector creates a new collector instance
func NewCollector(repo *repo.ResourcesRepository, notificationHandler NotificationHandler, dataSources []DataSource) *Collector {
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

	// Get the list of deployment managers for this data source
	_, err = c.collectDeploymentManagers(ctx, dataSource)
	if err != nil {
		return fmt.Errorf("failed to collect deployment managers: %w", err)
	}

	// TODO: purge stale record

	// TODO: persist data source info

	return nil
}

// collectResources collects Resource objects from the data source, persists them to the database,
// and signals any change events to the notification processor.
func (c *Collector) collectResources(ctx context.Context, dataSource DataSource,
	pools []models.ResourcePool) ([]models.Resource, error) {
	slog.Debug("collecting resource and types", "source", dataSource.Name())

	resources, err := dataSource.GetResources(ctx, pools)
	if err != nil {
		return nil, fmt.Errorf("failed to get resources: %w", err)
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

		dataChangeEvent, err := utils.PersistObjectWithChangeEvent(
			ctx, c.repository.Db, *resourceType, resourceType.ResourceTypeID, nil, func(object interface{}) any {
				record, _ := object.(models.ResourceType)
				return models.ResourceTypeToModel(&record)
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
		dataChangeEvent, err := utils.PersistObjectWithChangeEvent(
			ctx, c.repository.Db, resource, resource.ResourceID, &resource.ResourcePoolID, func(object interface{}) any {
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

// collectResourcePools collects ResourcePool objects from the data source, persists them to the database,
// and signals any change events to the notification processor.
func (c *Collector) collectResourcePools(ctx context.Context, dataSource DataSource) ([]models.ResourcePool, error) {
	slog.Debug("collecting resource pools", "source", dataSource.Name())

	pools, err := dataSource.GetResourcePools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource pools: %w", err)
	}

	// Loop over the set of resource pools and insert (or update) as needed
	for _, pool := range pools {
		dataChangeEvent, err := utils.PersistObjectWithChangeEvent(
			ctx, c.repository.Db, pool, pool.ResourcePoolID, nil, func(object interface{}) any {
				record, _ := object.(models.ResourcePool)
				return models.ResourcePoolToModel(&record)
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

// collectDeploymentManagers collects DeploymentManager objects from the data source, persists them to the database,
// and signals any change events to the notification processor.
func (c *Collector) collectDeploymentManagers(ctx context.Context, dataSource DataSource) ([]models.DeploymentManager, error) {
	slog.Debug("collecting deployment managers", "source", dataSource.Name())

	dms, err := dataSource.GetDeploymentManagers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment managers: %w", err)
	}

	// Loop over the set of deployment managers and insert (or update) as needed
	for _, dm := range dms {
		dataChangeEvent, err := utils.PersistObjectWithChangeEvent(
			ctx, c.repository.Db, dm, dm.DeploymentManagerID, nil, func(object interface{}) any {
				record, _ := object.(models.DeploymentManager)
				return models.DeploymentManagerToModel(&record)
			})
		if err != nil {
			return nil, fmt.Errorf("failed to persist deployment manager: %w", err)
		}

		if dataChangeEvent != nil {
			c.notificationHandler.Notify(ctx, models.DataChangeEventToNotification(dataChangeEvent))
		}
	}

	return dms, nil
}
