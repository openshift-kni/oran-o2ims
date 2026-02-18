/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

const resourcePoolReflectorName = "resourcepool-reflector"

// ResourcePoolDataSource defines a data source that watches ResourcePool CRs from Kubernetes
// and publishes changes via async events.
type ResourcePoolDataSource struct {
	dataSourceID      uuid.UUID
	cloudID           uuid.UUID
	hubClient         client.WithWatch
	generationID      int
	AsyncChangeEvents chan<- *async.AsyncChangeEvent
}

// NewResourcePoolDataSource creates a new instance of a ResourcePoolDataSource.
func NewResourcePoolDataSource(cloudID uuid.UUID, hubClient client.WithWatch) (*ResourcePoolDataSource, error) {
	if hubClient == nil {
		return nil, fmt.Errorf("hubClient is required")
	}

	return &ResourcePoolDataSource{
		generationID: 0,
		cloudID:      cloudID,
		hubClient:    hubClient,
	}, nil
}

// Name returns the name of this data source
func (d *ResourcePoolDataSource) Name() string {
	return "ResourcePool"
}

// GetID returns the data source ID for this data source
func (d *ResourcePoolDataSource) GetID() uuid.UUID {
	return d.dataSourceID
}

// Init initializes the data source with its configuration data
func (d *ResourcePoolDataSource) Init(dataSourceID uuid.UUID, generationID int, asyncEventChannel chan<- *async.AsyncChangeEvent) {
	d.dataSourceID = dataSourceID
	d.generationID = generationID
	d.AsyncChangeEvents = asyncEventChannel
}

// SetGenerationID sets the current generation id for this data source
func (d *ResourcePoolDataSource) SetGenerationID(value int) {
	d.generationID = value
}

// GetGenerationID retrieves the current generation id for this data source
func (d *ResourcePoolDataSource) GetGenerationID() int {
	return d.generationID
}

// IncrGenerationID increments the current generation id for this data source
func (d *ResourcePoolDataSource) IncrGenerationID() int {
	d.generationID++
	return d.generationID
}

// Watch starts a watcher for ResourcePool CRs.
// The watch is dispatched to a go routine. If the context is canceled, the watcher is stopped.
func (d *ResourcePoolDataSource) Watch(ctx context.Context) error {
	// Create a channel to signal stop events to the Reflector
	stopCh := make(chan struct{})
	go func() {
		<-ctx.Done()
		slog.Info("context canceled; stopping resourcepool reflector")
		close(stopCh)
	}()

	// Create a Reflector to watch ResourcePool objects
	lister := cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			var resourcePoolList inventoryv1alpha1.ResourcePoolList
			err := d.hubClient.List(ctx, &resourcePoolList, &client.ListOptions{Raw: &options})
			if err != nil {
				return nil, fmt.Errorf("error listing resourcepools: %w", err)
			}
			return &resourcePoolList, nil
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			var resourcePoolList inventoryv1alpha1.ResourcePoolList
			w, err := d.hubClient.Watch(ctx, &resourcePoolList, &client.ListOptions{Raw: &options})
			if err != nil {
				return nil, fmt.Errorf("error watching resourcepools: %w", err)
			}
			return w, nil
		},
		DisableChunking: false,
	}

	// Start the Reflector
	store := async.NewReflectorStore(&inventoryv1alpha1.ResourcePool{})
	reflector := cache.NewNamedReflector(resourcePoolReflectorName, &lister, &inventoryv1alpha1.ResourcePool{}, store, time.Duration(0))
	slog.Info("starting resourcepool reflector")
	go reflector.Run(stopCh)

	// Start monitoring the store to process incoming events
	slog.Info("starting to receive from resourcepool reflector store")
	go store.Receive(ctx, d)

	return nil
}

// HandleAsyncEvent handles an add/update/delete event received from the Reflector.
func (d *ResourcePoolDataSource) HandleAsyncEvent(ctx context.Context, obj interface{}, eventType async.AsyncEventType) (uuid.UUID, error) {
	slog.Debug("handleAsyncEvent received for resourcepool", "type", eventType, "object", fmt.Sprintf("%T", obj))

	switch value := obj.(type) {
	case *inventoryv1alpha1.ResourcePool:
		return d.handleResourcePoolWatchEvent(ctx, value, eventType)
	default:
		slog.Warn("Unknown object type in ResourcePoolDataSource", "type", fmt.Sprintf("%T", obj))
		return uuid.Nil, fmt.Errorf("unknown type: %T", obj)
	}
}

// HandleSyncComplete handles the end of a sync operation by sending an event to the Collector.
func (d *ResourcePoolDataSource) HandleSyncComplete(ctx context.Context, objectType runtime.Object, keys []uuid.UUID) error {
	var object db.Model
	switch objectType.(type) {
	case *inventoryv1alpha1.ResourcePool:
		object = models.ResourcePool{}
	default:
		slog.Warn("Unknown object type in HandleSyncComplete", "type", fmt.Sprintf("%T", objectType))
		return nil
	}

	select {
	case <-ctx.Done():
		slog.Info("context cancelled while writing resourcepool sync complete event; aborting")
		return fmt.Errorf("context cancelled; aborting")
	case d.AsyncChangeEvents <- &async.AsyncChangeEvent{
		DataSourceID: d.dataSourceID,
		EventType:    async.SyncComplete,
		Object:       object,
		Keys:         keys}:
		return nil
	}
}

// handleResourcePoolWatchEvent handles an async event received for a ResourcePool CR
func (d *ResourcePoolDataSource) handleResourcePoolWatchEvent(ctx context.Context, pool *inventoryv1alpha1.ResourcePool, eventType async.AsyncEventType) (uuid.UUID, error) {
	slog.Debug("handleResourcePoolWatchEvent received", "resourcePoolId", pool.Spec.ResourcePoolId, "type", eventType)

	record := d.convertResourcePoolToModel(pool)

	select {
	case <-ctx.Done():
		slog.Info("context cancelled while writing to async event channel; aborting")
		return uuid.Nil, fmt.Errorf("context cancelled; aborting")
	case d.AsyncChangeEvents <- &async.AsyncChangeEvent{
		DataSourceID: d.dataSourceID,
		EventType:    eventType,
		Object:       record}:
		// return the generated ResourcePoolID (UUID) for tracking purposes
		return record.ResourcePoolID, nil
	}
}

// convertResourcePoolToModel converts a ResourcePool CR to a database model
func (d *ResourcePoolDataSource) convertResourcePoolToModel(pool *inventoryv1alpha1.ResourcePool) models.ResourcePool {
	// Generate deterministic UUID from cloudID and resourcePoolId
	resourcePoolID := ctlrutils.MakeUUIDFromNames(ResourcePoolUUIDNamespace, d.cloudID, pool.Spec.ResourcePoolId)

	// Generate deterministic UUID for OCloudSiteID from cloudID and oCloudSiteId
	oCloudSiteID := ctlrutils.MakeUUIDFromNames(OCloudSiteUUIDNamespace, d.cloudID, pool.Spec.OCloudSiteId)

	var extensions map[string]interface{}
	if pool.Spec.Extensions != nil {
		extensions = make(map[string]interface{})
		for k, v := range pool.Spec.Extensions {
			extensions[k] = v
		}
	}

	var location *string
	if pool.Spec.Location != nil {
		location = pool.Spec.Location
	}

	return models.ResourcePool{
		ResourcePoolID: resourcePoolID,
		Name:           pool.Spec.Name,
		Description:    pool.Spec.Description,
		OCloudSiteID:   &oCloudSiteID,
		Location:       location,
		Extensions:     extensions,
		DataSourceID:   d.dataSourceID,
		GenerationID:   d.generationID,
		ExternalID:     fmt.Sprintf("%s/%s", pool.Namespace, pool.Name),
	}
}
