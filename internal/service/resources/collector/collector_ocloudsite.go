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

const oCloudSiteReflectorName = "ocloudsite-reflector"

// OCloudSiteDataSource defines a data source that watches OCloudSite CRs from Kubernetes
// and publishes changes via async events.
type OCloudSiteDataSource struct {
	dataSourceID      uuid.UUID
	cloudID           uuid.UUID
	hubClient         client.WithWatch
	generationID      int
	AsyncChangeEvents chan<- *async.AsyncChangeEvent
}

// NewOCloudSiteDataSource creates a new instance of an OCloudSiteDataSource.
func NewOCloudSiteDataSource(cloudID uuid.UUID, hubClient client.WithWatch) (*OCloudSiteDataSource, error) {
	if hubClient == nil {
		return nil, fmt.Errorf("hubClient is required")
	}

	return &OCloudSiteDataSource{
		generationID: 0,
		cloudID:      cloudID,
		hubClient:    hubClient,
	}, nil
}

// Name returns the name of this data source
func (d *OCloudSiteDataSource) Name() string {
	return "OCloudSite"
}

// GetID returns the data source ID for this data source
func (d *OCloudSiteDataSource) GetID() uuid.UUID {
	return d.dataSourceID
}

// Init initializes the data source with its configuration data
func (d *OCloudSiteDataSource) Init(dataSourceID uuid.UUID, generationID int, asyncEventChannel chan<- *async.AsyncChangeEvent) {
	d.dataSourceID = dataSourceID
	d.generationID = generationID
	d.AsyncChangeEvents = asyncEventChannel
}

// SetGenerationID sets the current generation id for this data source
func (d *OCloudSiteDataSource) SetGenerationID(value int) {
	d.generationID = value
}

// GetGenerationID retrieves the current generation id for this data source
func (d *OCloudSiteDataSource) GetGenerationID() int {
	return d.generationID
}

// IncrGenerationID increments the current generation id for this data source
func (d *OCloudSiteDataSource) IncrGenerationID() int {
	d.generationID++
	return d.generationID
}

// Watch starts a watcher for OCloudSite CRs.
// The watch is dispatched to a go routine. If the context is canceled, the watcher is stopped.
func (d *OCloudSiteDataSource) Watch(ctx context.Context) error {
	// Create a channel to signal stop events to the Reflector
	stopCh := make(chan struct{})
	go func() {
		<-ctx.Done()
		slog.Info("context canceled; stopping ocloudsite reflector")
		close(stopCh)
	}()

	// Create a Reflector to watch OCloudSite objects
	lister := cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			var oCloudSiteList inventoryv1alpha1.OCloudSiteList
			err := d.hubClient.List(ctx, &oCloudSiteList, &client.ListOptions{Raw: &options})
			if err != nil {
				return nil, fmt.Errorf("error listing ocloudsites: %w", err)
			}
			return &oCloudSiteList, nil
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			var oCloudSiteList inventoryv1alpha1.OCloudSiteList
			w, err := d.hubClient.Watch(ctx, &oCloudSiteList, &client.ListOptions{Raw: &options})
			if err != nil {
				return nil, fmt.Errorf("error watching ocloudsites: %w", err)
			}
			return w, nil
		},
		DisableChunking: false,
	}

	// Start the Reflector
	store := async.NewReflectorStore(&inventoryv1alpha1.OCloudSite{})
	reflector := cache.NewNamedReflector(oCloudSiteReflectorName, &lister, &inventoryv1alpha1.OCloudSite{}, store, time.Duration(0))
	slog.Info("starting ocloudsite reflector")
	go reflector.Run(stopCh)

	// Start monitoring the store to process incoming events
	slog.Info("starting to receive from ocloudsite reflector store")
	go store.Receive(ctx, d)

	return nil
}

// HandleAsyncEvent handles an add/update/delete event received from the Reflector.
func (d *OCloudSiteDataSource) HandleAsyncEvent(ctx context.Context, obj interface{}, eventType async.AsyncEventType) (uuid.UUID, error) {
	slog.Debug("handleAsyncEvent received for ocloudsite", "type", eventType, "object", fmt.Sprintf("%T", obj))

	switch value := obj.(type) {
	case *inventoryv1alpha1.OCloudSite:
		return d.handleOCloudSiteWatchEvent(ctx, value, eventType)
	default:
		slog.Warn("Unknown object type in OCloudSiteDataSource", "type", fmt.Sprintf("%T", obj))
		return uuid.Nil, fmt.Errorf("unknown type: %T", obj)
	}
}

// HandleSyncComplete handles the end of a sync operation by sending an event to the Collector.
func (d *OCloudSiteDataSource) HandleSyncComplete(ctx context.Context, objectType runtime.Object, keys []uuid.UUID) error {
	var object db.Model
	switch objectType.(type) {
	case *inventoryv1alpha1.OCloudSite:
		object = models.OCloudSite{}
	default:
		slog.Warn("Unknown object type in HandleSyncComplete", "type", fmt.Sprintf("%T", objectType))
		return nil
	}

	select {
	case <-ctx.Done():
		slog.Info("context cancelled while writing ocloudsite sync complete event; aborting")
		return fmt.Errorf("context cancelled; aborting")
	case d.AsyncChangeEvents <- &async.AsyncChangeEvent{
		DataSourceID: d.dataSourceID,
		EventType:    async.SyncComplete,
		Object:       object,
		Keys:         keys}:
		return nil
	}
}

// handleOCloudSiteWatchEvent handles an async event received for an OCloudSite CR
func (d *OCloudSiteDataSource) handleOCloudSiteWatchEvent(ctx context.Context, site *inventoryv1alpha1.OCloudSite, eventType async.AsyncEventType) (uuid.UUID, error) {
	slog.Debug("handleOCloudSiteWatchEvent received", "siteId", site.Spec.SiteID, "type", eventType)

	record := d.convertOCloudSiteToModel(site)

	select {
	case <-ctx.Done():
		slog.Info("context cancelled while writing to async event channel; aborting")
		return uuid.Nil, fmt.Errorf("context cancelled; aborting")
	case d.AsyncChangeEvents <- &async.AsyncChangeEvent{
		DataSourceID: d.dataSourceID,
		EventType:    eventType,
		Object:       record}:
		// return the generated oCloudSiteID (UUID) for tracking purposes
		return record.OCloudSiteID, nil
	}
}

// convertOCloudSiteToModel converts an OCloudSite CR to a database model
func (d *OCloudSiteDataSource) convertOCloudSiteToModel(site *inventoryv1alpha1.OCloudSite) models.OCloudSite {
	// Generate deterministic UUID from cloudID and siteId
	oCloudSiteID := ctlrutils.MakeUUIDFromNames(OCloudSiteUUIDNamespace, d.cloudID, site.Spec.SiteID)

	var extensions map[string]interface{}
	if site.Spec.Extensions != nil {
		extensions = make(map[string]interface{})
		for k, v := range site.Spec.Extensions {
			extensions[k] = v
		}
	}

	return models.OCloudSite{
		OCloudSiteID:     oCloudSiteID,
		GlobalLocationID: site.Spec.GlobalLocationID,
		Name:             site.Spec.Name,
		Description:      site.Spec.Description,
		Extensions:       extensions,
		DataSourceID:     d.dataSourceID,
		GenerationID:     d.generationID,
	}
}
