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
	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

const locationReflectorName = "location-reflector"

// LocationDataSource defines a data source that watches Location CRs from Kubernetes
// and publishes changes via async events.
type LocationDataSource struct {
	dataSourceID      uuid.UUID
	cloudID           uuid.UUID
	hubClient         client.WithWatch
	generationID      int
	AsyncChangeEvents chan<- *async.AsyncChangeEvent
}

// NewLocationDataSource creates a new instance of a LocationDataSource.
func NewLocationDataSource(cloudID uuid.UUID, hubClient client.WithWatch) (*LocationDataSource, error) {
	if hubClient == nil {
		return nil, fmt.Errorf("hubClient is required")
	}

	return &LocationDataSource{
		generationID: 0,
		cloudID:      cloudID,
		hubClient:    hubClient,
	}, nil
}

// Name returns the name of this data source
func (d *LocationDataSource) Name() string {
	return "Location"
}

// GetID returns the data source ID for this data source
func (d *LocationDataSource) GetID() uuid.UUID {
	return d.dataSourceID
}

// Init initializes the data source with its configuration data
func (d *LocationDataSource) Init(dataSourceID uuid.UUID, generationID int, asyncEventChannel chan<- *async.AsyncChangeEvent) {
	d.dataSourceID = dataSourceID
	d.generationID = generationID
	d.AsyncChangeEvents = asyncEventChannel
}

// SetGenerationID sets the current generation id for this data source
func (d *LocationDataSource) SetGenerationID(value int) {
	d.generationID = value
}

// GetGenerationID retrieves the current generation id for this data source
func (d *LocationDataSource) GetGenerationID() int {
	return d.generationID
}

// IncrGenerationID increments the current generation id for this data source
func (d *LocationDataSource) IncrGenerationID() int {
	d.generationID++
	return d.generationID
}

// Watch starts a watcher for Location CRs.
// The watch is dispatched to a go routine. If the context is canceled, the watcher is stopped.
func (d *LocationDataSource) Watch(ctx context.Context) error {
	// Create a channel to signal stop events to the Reflector
	stopCh := make(chan struct{})
	go func() {
		<-ctx.Done()
		slog.Info("context canceled; stopping location reflector")
		close(stopCh)
	}()

	// Create a Reflector to watch Location objects
	lister := cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			var locationList inventoryv1alpha1.LocationList
			err := d.hubClient.List(ctx, &locationList, &client.ListOptions{Raw: &options})
			if err != nil {
				return nil, fmt.Errorf("error listing locations: %w", err)
			}
			return &locationList, nil
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			var locationList inventoryv1alpha1.LocationList
			w, err := d.hubClient.Watch(ctx, &locationList, &client.ListOptions{Raw: &options})
			if err != nil {
				return nil, fmt.Errorf("error watching locations: %w", err)
			}
			return w, nil
		},
		DisableChunking: false,
	}

	// Start the Reflector
	store := async.NewReflectorStore(&inventoryv1alpha1.Location{})
	reflector := cache.NewNamedReflector(locationReflectorName, &lister, &inventoryv1alpha1.Location{}, store, time.Duration(0))
	slog.Info("starting location reflector")
	go reflector.Run(stopCh)

	// Start monitoring the store to process incoming events
	slog.Info("starting to receive from location reflector store")
	go store.Receive(ctx, d)

	return nil
}

// HandleAsyncEvent handles an add/update/delete event received from the Reflector.
func (d *LocationDataSource) HandleAsyncEvent(ctx context.Context, obj interface{}, eventType async.AsyncEventType) (uuid.UUID, error) {
	slog.Debug("handleAsyncEvent received for location", "type", eventType, "object", fmt.Sprintf("%T", obj))

	switch value := obj.(type) {
	case *inventoryv1alpha1.Location:
		return d.handleLocationWatchEvent(ctx, value, eventType)
	default:
		slog.Warn("Unknown object type in LocationDataSource", "type", fmt.Sprintf("%T", obj))
		return uuid.Nil, fmt.Errorf("unknown type: %T", obj)
	}
}

// HandleSyncComplete handles the end of a sync operation by sending an event to the Collector.
func (d *LocationDataSource) HandleSyncComplete(ctx context.Context, objectType runtime.Object, keys []uuid.UUID) error {
	var object db.Model
	switch objectType.(type) {
	case *inventoryv1alpha1.Location:
		object = models.Location{}
	default:
		slog.Warn("Unknown object type in HandleSyncComplete", "type", fmt.Sprintf("%T", objectType))
		return nil
	}

	select {
	case <-ctx.Done():
		slog.Info("context cancelled while writing location sync complete event; aborting")
		return fmt.Errorf("context cancelled; aborting")
	case d.AsyncChangeEvents <- &async.AsyncChangeEvent{
		DataSourceID: d.dataSourceID,
		EventType:    async.SyncComplete,
		Object:       object,
		Keys:         keys}:
		return nil
	}
}

// handleLocationWatchEvent handles an async event received for a Location CR
func (d *LocationDataSource) handleLocationWatchEvent(ctx context.Context, location *inventoryv1alpha1.Location, eventType async.AsyncEventType) (uuid.UUID, error) {
	slog.Debug("handleLocationWatchEvent received", "globalLocationId", location.Spec.GlobalLocationID, "type", eventType)

	record, err := d.convertLocationToModel(location)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to convert location CR to model: %w", err)
	}

	select {
	case <-ctx.Done():
		slog.Info("context cancelled while writing to async event channel; aborting")
		return uuid.Nil, fmt.Errorf("context cancelled; aborting")
	case d.AsyncChangeEvents <- &async.AsyncChangeEvent{
		DataSourceID: d.dataSourceID,
		EventType:    eventType,
		Object:       record}:
		// return a generated trackingUUID for tracking purposes
		trackingUUID := uuid.NewSHA1(uuid.NameSpaceDNS, []byte(record.GlobalLocationID))
		return trackingUUID, nil
	}
}

// convertLocationToModel converts a Location CR to a database model
func (d *LocationDataSource) convertLocationToModel(loc *inventoryv1alpha1.Location) (models.Location, error) {
	coordinate, err := convertCoordinateToGeoJSON(loc.Spec.Coordinate)
	if err != nil {
		return models.Location{}, fmt.Errorf("failed to convert coordinate for location %q: %w", loc.Spec.GlobalLocationID, err)
	}

	var extensions map[string]interface{}
	if loc.Spec.Extensions != nil {
		extensions = make(map[string]interface{})
		for k, v := range loc.Spec.Extensions {
			extensions[k] = v
		}
	}

	return models.Location{
		GlobalLocationID: loc.Spec.GlobalLocationID,
		Name:             loc.Spec.Name,
		Description:      loc.Spec.Description,
		Coordinate:       coordinate,
		CivicAddress:     convertCivicAddress(loc.Spec.CivicAddress),
		Address:          loc.Spec.Address,
		Extensions:       extensions,
		DataSourceID:     d.dataSourceID,
		GenerationID:     d.generationID,
	}, nil
}
