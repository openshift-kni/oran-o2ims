/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	hwmgrcontroller "github.com/openshift-kni/oran-o2ims/internal/hardwaremanager/controller"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

// Interface compile enforcement
var _ WatchableDataSource = (*HardwareDataSource)(nil)
var _ async.AsyncEventHandler = (*HardwareDataSource)(nil)
var _ PoolChangeNotifier = (*HardwareDataSource)(nil)

const (
	bmhReflectorName           = "bmh-reflector"
	hwdataReflectorName        = "hwdata-reflector"
	allocatednodeReflectorName = "allocatednode-reflector"
)

// HardwareDataSource collects hardware inventory data (BMHs, HardwareData,
// AllocatedNodes) via K8s watches using three separate Reflectors.
type HardwareDataSource struct {
	dataSourceID      uuid.UUID
	generationID      atomic.Int32
	cloudID           uuid.UUID
	globalCloudID     uuid.UUID
	hubClient         client.WithWatch
	AsyncChangeEvents chan<- *async.AsyncChangeEvent
}

// Defines the UUID namespace values used to generate name based UUID values for inventory objects.
// These values are selected arbitrarily.
// TODO: move to somewhere more generic
const (
	ResourceTypeUUIDNamespace = "255c4b4c-84a8-4c95-95ba-217e1688a03d"
)

const (
	vendorExtension           = "vendor"
	modelExtension            = "model"
	memoryExtension           = "memory"
	processorsExtension       = "processors"
	adminStateExtension       = "adminState"
	operationalStateExtension = "operationalState"
	usageStateExtension       = "usageState"
	powerStateExtension       = "powerState"
	hwProfileExtension        = "hwProfile"
	labelsExtension           = "labels"
	allocatedExtension        = "allocated"
	nicsExtension             = "nics"
	storageExtension          = "storage"
)

// NewHardwareDataSource creates a hardware inventory data source that watches
// BMH, HardwareData, and AllocatedNode CRs for changes.
func NewHardwareDataSource(hubClient client.WithWatch, cloudID, globalCloudID uuid.UUID) (DataSource, error) {
	if hubClient == nil {
		return nil, fmt.Errorf("hubClient is required")
	}

	return &HardwareDataSource{
		cloudID:       cloudID,
		globalCloudID: globalCloudID,
		hubClient:     hubClient,
	}, nil
}

// Name returns the name of this data source
func (d *HardwareDataSource) Name() string {
	return "HardwareDataSource"
}

// GetID returns the data source ID for this data source
func (d *HardwareDataSource) GetID() uuid.UUID {
	return d.dataSourceID
}

// Init initializes the data source with its configuration data; including the ID, the GenerationID, and the
// async event channel for sending events to the collector.
func (d *HardwareDataSource) Init(uuid uuid.UUID, generationID int, asyncEventChannel chan<- *async.AsyncChangeEvent) {
	d.dataSourceID = uuid
	d.generationID.Store(int32(generationID)) //nolint:gosec // generationID is a small counter, overflow impossible
	d.AsyncChangeEvents = asyncEventChannel
}

// SetGenerationID sets the current generation id for this data source.
func (d *HardwareDataSource) SetGenerationID(value int) {
	d.generationID.Store(int32(value)) //nolint:gosec // generationID is a small counter, overflow impossible
}

// GetGenerationID retrieves the current generation id for this data source.
func (d *HardwareDataSource) GetGenerationID() int {
	return int(d.generationID.Load())
}

// IncrGenerationID increments the current generation id for this data source.
func (d *HardwareDataSource) IncrGenerationID() int {
	return int(d.generationID.Add(1))
}

// Watch starts watchers for BMH, HardwareData, and AllocatedNode CRs.
func (d *HardwareDataSource) Watch(ctx context.Context) error {
	stopCh := make(chan struct{})
	go func() {
		<-ctx.Done()
		slog.Info("context canceled; stopping hardware data source reflectors")
		close(stopCh)
	}()

	d.watchBMH(ctx, stopCh)
	d.watchHardwareData(ctx, stopCh)
	d.watchAllocatedNodes(ctx, stopCh)

	return nil
}

func (d *HardwareDataSource) watchBMH(ctx context.Context, stopCh <-chan struct{}) {
	lister := cache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			var list metal3v1alpha1.BareMetalHostList
			if err := d.hubClient.List(ctx, &list, &client.ListOptions{Raw: &options}); err != nil {
				return nil, fmt.Errorf("error listing BareMetalHosts: %w", err)
			}
			return &list, nil
		},
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			var list metal3v1alpha1.BareMetalHostList
			w, err := d.hubClient.Watch(ctx, &list, &client.ListOptions{Raw: &options})
			if err != nil {
				return nil, fmt.Errorf("error watching BareMetalHosts: %w", err)
			}
			return w, nil
		},
		DisableChunking: false,
	}

	store := async.NewReflectorStore(&metal3v1alpha1.BareMetalHost{})
	reflector := cache.NewNamedReflector(bmhReflectorName, &lister, &metal3v1alpha1.BareMetalHost{}, store, time.Duration(0))
	slog.Info("starting BMH reflector")
	go reflector.Run(stopCh)
	go store.Receive(ctx, d)
}

func (d *HardwareDataSource) watchHardwareData(ctx context.Context, stopCh <-chan struct{}) {
	lister := cache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			var list metal3v1alpha1.HardwareDataList
			if err := d.hubClient.List(ctx, &list, &client.ListOptions{Raw: &options}); err != nil {
				return nil, fmt.Errorf("error listing HardwareData: %w", err)
			}
			return &list, nil
		},
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			var list metal3v1alpha1.HardwareDataList
			w, err := d.hubClient.Watch(ctx, &list, &client.ListOptions{Raw: &options})
			if err != nil {
				return nil, fmt.Errorf("error watching HardwareData: %w", err)
			}
			return w, nil
		},
		DisableChunking: false,
	}

	store := async.NewReflectorStore(&metal3v1alpha1.HardwareData{})
	reflector := cache.NewNamedReflector(hwdataReflectorName, &lister, &metal3v1alpha1.HardwareData{}, store, time.Duration(0))
	slog.Info("starting HardwareData reflector")
	go reflector.Run(stopCh)
	go store.Receive(ctx, d)
}

func (d *HardwareDataSource) watchAllocatedNodes(ctx context.Context, stopCh <-chan struct{}) {
	lister := cache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			var list hwmgmtv1alpha1.AllocatedNodeList
			if err := d.hubClient.List(ctx, &list, &client.ListOptions{Raw: &options}); err != nil {
				return nil, fmt.Errorf("error listing AllocatedNodes: %w", err)
			}
			return &list, nil
		},
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			var list hwmgmtv1alpha1.AllocatedNodeList
			w, err := d.hubClient.Watch(ctx, &list, &client.ListOptions{Raw: &options})
			if err != nil {
				return nil, fmt.Errorf("error watching AllocatedNodes: %w", err)
			}
			return w, nil
		},
		DisableChunking: false,
	}

	store := async.NewReflectorStore(&hwmgmtv1alpha1.AllocatedNode{})
	reflector := cache.NewNamedReflector(allocatednodeReflectorName, &lister, &hwmgmtv1alpha1.AllocatedNode{}, store, time.Duration(0))
	slog.Info("starting AllocatedNode reflector")
	go reflector.Run(stopCh)
	go store.Receive(ctx, d)
}

// HandleAsyncEvent handles an add/update/delete event from any of the three Reflectors.
func (d *HardwareDataSource) HandleAsyncEvent(ctx context.Context, obj interface{}, eventType async.AsyncEventType) (uuid.UUID, error) {
	switch value := obj.(type) {
	case *metal3v1alpha1.BareMetalHost:
		return d.handleBMHEvent(ctx, value, eventType)
	case *metal3v1alpha1.HardwareData:
		return d.handleHardwareDataEvent(ctx, value, eventType)
	case *hwmgmtv1alpha1.AllocatedNode:
		return d.handleAllocatedNodeEvent(ctx, value, eventType)
	default:
		slog.Warn("Unknown object type in HardwareDataSource", "type", fmt.Sprintf("%T", obj))
		return uuid.Nil, fmt.Errorf("unknown type: %T", obj)
	}
}

// HandleSyncComplete handles the end of a sync operation.
// Only BMH SyncComplete triggers resource purging; HardwareData and
// AllocatedNode SyncComplete are no-ops since BMH is the authoritative
// source for resource existence.
func (d *HardwareDataSource) HandleSyncComplete(ctx context.Context, objectType runtime.Object, keys []uuid.UUID) error {
	switch objectType.(type) {
	case *metal3v1alpha1.BareMetalHost:
		slog.Info("BMH sync complete", "resourceCount", len(keys))
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled; aborting")
		case d.AsyncChangeEvents <- &async.AsyncChangeEvent{
			DataSourceID: d.dataSourceID,
			EventType:    async.SyncComplete,
			Object:       models.Resource{},
			Keys:         keys}:
			return nil
		}
	case *metal3v1alpha1.HardwareData:
		slog.Info("HardwareData sync complete")
		return nil
	case *hwmgmtv1alpha1.AllocatedNode:
		slog.Info("AllocatedNode sync complete")
		return nil
	default:
		slog.Warn("Unknown object type in HandleSyncComplete", "type", fmt.Sprintf("%T", objectType))
		return nil
	}
}

// handleBMHEvent handles a BMH watch event by building the full resource
// from BMH + HardwareData + AllocatedNode and sending events to the collector.
func (d *HardwareDataSource) handleBMHEvent(ctx context.Context, bmh *metal3v1alpha1.BareMetalHost, eventType async.AsyncEventType) (uuid.UUID, error) {
	slog.Debug("handleBMHEvent", "bmh", bmh.Namespace+"/"+bmh.Name, "type", eventType)

	resourceID := uuid.MustParse(string(bmh.UID))

	if eventType == async.Deleted {
		return d.sendDeleteEvent(ctx, resourceID)
	}

	if !hwmgrcontroller.IncludeInInventory(bmh) {
		slog.Debug("BMH not included in inventory, treating as deletion",
			"bmh", bmh.Namespace+"/"+bmh.Name)
		return d.sendDeleteEvent(ctx, resourceID)
	}

	return d.buildAndSendResource(ctx, bmh)
}

// handleHardwareDataEvent handles a HardwareData watch event by looking up
// the corresponding BMH and rebuilding the resource.
func (d *HardwareDataSource) handleHardwareDataEvent(ctx context.Context, hwdata *metal3v1alpha1.HardwareData, eventType async.AsyncEventType) (uuid.UUID, error) {
	slog.Debug("handleHardwareDataEvent", "hwdata", hwdata.Namespace+"/"+hwdata.Name, "type", eventType)

	var bmh metal3v1alpha1.BareMetalHost
	if err := d.hubClient.Get(ctx, types.NamespacedName{Name: hwdata.Name, Namespace: hwdata.Namespace}, &bmh); err != nil {
		if errors.IsNotFound(err) {
			slog.Debug("BMH not found for HardwareData, skipping", "hwdata", hwdata.Namespace+"/"+hwdata.Name)
			return uuid.Nil, nil
		}
		return uuid.Nil, fmt.Errorf("failed to get BMH for HardwareData %s/%s: %w", hwdata.Namespace, hwdata.Name, err)
	}

	if !hwmgrcontroller.IncludeInInventory(&bmh) {
		return uuid.Nil, nil
	}

	return d.buildAndSendResource(ctx, &bmh)
}

// handleAllocatedNodeEvent handles an AllocatedNode watch event by looking up
// the corresponding BMH and rebuilding the resource.
func (d *HardwareDataSource) handleAllocatedNodeEvent(ctx context.Context, node *hwmgmtv1alpha1.AllocatedNode, eventType async.AsyncEventType) (uuid.UUID, error) {
	slog.Debug("handleAllocatedNodeEvent", "node", node.Namespace+"/"+node.Name, "type", eventType)

	bmhName := node.Spec.HwMgrNodeId
	bmhNamespace := node.Spec.HwMgrNodeNs
	if bmhName == "" || bmhNamespace == "" {
		slog.Debug("AllocatedNode missing BMH reference, skipping", "node", node.Namespace+"/"+node.Name)
		return uuid.Nil, nil
	}

	var bmh metal3v1alpha1.BareMetalHost
	if err := d.hubClient.Get(ctx, types.NamespacedName{Name: bmhName, Namespace: bmhNamespace}, &bmh); err != nil {
		if errors.IsNotFound(err) {
			slog.Debug("BMH not found for AllocatedNode, skipping", "bmh", bmhNamespace+"/"+bmhName)
			return uuid.Nil, nil
		}
		return uuid.Nil, fmt.Errorf("failed to get BMH for AllocatedNode %s/%s: %w", node.Namespace, node.Name, err)
	}

	if !hwmgrcontroller.IncludeInInventory(&bmh) {
		return uuid.Nil, nil
	}

	return d.buildAndSendResource(ctx, &bmh)
}

// buildAndSendResource builds a complete Resource from a BMH by looking up
// the related HardwareData and AllocatedNode, then sends both ResourceType
// and Resource events to the collector.
func (d *HardwareDataSource) buildAndSendResource(ctx context.Context, bmh *metal3v1alpha1.BareMetalHost) (uuid.UUID, error) {
	// Look up HardwareData by same name/namespace
	var hwdata metal3v1alpha1.HardwareData
	if err := d.hubClient.Get(ctx, types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}, &hwdata); err != nil {
		if !errors.IsNotFound(err) {
			return uuid.Nil, fmt.Errorf("failed to get HardwareData for BMH %s/%s: %w", bmh.Namespace, bmh.Name, err)
		}
		// HardwareData not found — use empty (BMH may not have been inspected yet)
		hwdata = metal3v1alpha1.HardwareData{}
	}

	// Look up AllocatedNode for this BMH
	node := d.findAllocatedNodeForBMH(ctx, bmh)

	// Build ResourcePool name→UID map
	poolNameToUID, err := d.getPoolNameToUID(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to get ResourcePool UIDs: %w", err)
	}

	// Check that the BMH has a valid pool reference
	poolUID := hwmgrcontroller.GetResourceInfoResourcePoolUID(bmh, poolNameToUID)
	if poolUID == "" {
		poolName := bmh.Labels[constants.LabelResourcePoolName]
		slog.Warn("skipping BMH: unresolved resourcePoolId (will retry on pool creation)",
			"bmh", bmh.Namespace+"/"+bmh.Name, "poolName", poolName)
		return uuid.Nil, nil
	}

	// Build ResourceInfo using existing inventory logic
	resourceInfo := hwmgrcontroller.GetResourceInfo(bmh, node, &hwdata, poolNameToUID)

	// Convert to models
	resource := d.convertResource(&resourceInfo)
	resourceType, err := d.MakeResourceType(resource)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to make resource type: %w", err)
	}

	// Send ResourceType event first (FK ordering)
	select {
	case <-ctx.Done():
		return uuid.Nil, fmt.Errorf("context cancelled; aborting")
	case d.AsyncChangeEvents <- &async.AsyncChangeEvent{
		DataSourceID: d.dataSourceID,
		EventType:    async.Updated,
		Object:       *resourceType}:
	}

	// Send Resource event
	select {
	case <-ctx.Done():
		return uuid.Nil, fmt.Errorf("context cancelled; aborting")
	case d.AsyncChangeEvents <- &async.AsyncChangeEvent{
		DataSourceID: d.dataSourceID,
		EventType:    async.Updated,
		Object:       *resource}:
	}

	return resource.ResourceID, nil
}

// sendDeleteEvent sends a delete event for a resource with the given ID.
func (d *HardwareDataSource) sendDeleteEvent(ctx context.Context, resourceID uuid.UUID) (uuid.UUID, error) {
	select {
	case <-ctx.Done():
		return uuid.Nil, fmt.Errorf("context cancelled; aborting")
	case d.AsyncChangeEvents <- &async.AsyncChangeEvent{
		DataSourceID: d.dataSourceID,
		EventType:    async.Deleted,
		Object:       models.Resource{ResourceID: resourceID}}:
		return resourceID, nil
	}
}

// findAllocatedNodeForBMH looks up the AllocatedNode for a BMH using the
// allocation label on the BMH.
func (d *HardwareDataSource) findAllocatedNodeForBMH(ctx context.Context, bmh *metal3v1alpha1.BareMetalHost) *hwmgmtv1alpha1.AllocatedNode {
	if bmh.Labels == nil {
		return nil
	}
	nodeName, exists := bmh.Labels[ctlrutils.AllocatedNodeLabel]
	if !exists || nodeName == "" {
		return nil
	}

	var node hwmgmtv1alpha1.AllocatedNode
	if err := d.hubClient.Get(ctx, types.NamespacedName{Name: nodeName, Namespace: bmh.Namespace}, &node); err != nil {
		if !errors.IsNotFound(err) {
			slog.Warn("failed to get AllocatedNode for BMH",
				"bmh", bmh.Namespace+"/"+bmh.Name, "node", nodeName, "error", err)
		}
		return nil
	}
	return &node
}

// getPoolNameToUID builds a map of ResourcePool name → UID.
func (d *HardwareDataSource) getPoolNameToUID(ctx context.Context) (map[string]string, error) {
	var poolList inventoryv1alpha1.ResourcePoolList
	if err := d.hubClient.List(ctx, &poolList); err != nil {
		return nil, fmt.Errorf("failed to list ResourcePools: %w", err)
	}
	result := make(map[string]string, len(poolList.Items))
	for _, pool := range poolList.Items {
		if !inventoryv1alpha1.IsResourceReady(pool.Status.Conditions) {
			continue
		}
		result[pool.Name] = string(pool.UID)
	}
	return result, nil
}

// BuildResourcesForPool lists BMHs that reference the given pool and builds
// Resource + ResourceType pairs for each. This is called by the collector when
// a ResourcePool is created or updated, to handle the case where BMH events
// arrived before the pool existed.
func (d *HardwareDataSource) BuildResourcesForPool(ctx context.Context, poolName string) ([]ResourceWithType, error) {
	var bmhList metal3v1alpha1.BareMetalHostList
	if err := d.hubClient.List(ctx, &bmhList, client.MatchingLabels{
		constants.LabelResourcePoolName: poolName,
	}); err != nil {
		return nil, fmt.Errorf("failed to list BMHs for pool %s: %w", poolName, err)
	}

	poolNameToUID, err := d.getPoolNameToUID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get ResourcePool UIDs: %w", err)
	}

	var results []ResourceWithType
	for i := range bmhList.Items {
		bmh := &bmhList.Items[i]
		if !hwmgrcontroller.IncludeInInventory(bmh) {
			continue
		}

		poolUID := hwmgrcontroller.GetResourceInfoResourcePoolUID(bmh, poolNameToUID)
		if poolUID == "" {
			continue
		}

		var hwdata metal3v1alpha1.HardwareData
		if err := d.hubClient.Get(ctx, types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}, &hwdata); err != nil {
			if !errors.IsNotFound(err) {
				slog.Warn("failed to get HardwareData during pool rebuild",
					"bmh", bmh.Namespace+"/"+bmh.Name, "error", err)
			}
			hwdata = metal3v1alpha1.HardwareData{}
		}

		node := d.findAllocatedNodeForBMH(ctx, bmh)
		resourceInfo := hwmgrcontroller.GetResourceInfo(bmh, node, &hwdata, poolNameToUID)
		resource := d.convertResource(&resourceInfo)
		resourceType, err := d.MakeResourceType(resource)
		if err != nil {
			slog.Warn("failed to make resource type during pool rebuild",
				"bmh", bmh.Namespace+"/"+bmh.Name, "error", err)
			continue
		}

		results = append(results, ResourceWithType{
			Resource:     *resource,
			ResourceType: *resourceType,
		})
	}

	return results, nil
}

// MakeResourceType creates an instance of a ResourceType from a Resource object.
func (d *HardwareDataSource) MakeResourceType(resource *models.Resource) (*models.ResourceType, error) {
	vendor := resource.Extensions[vendorExtension].(string)
	model := resource.Extensions[modelExtension].(string)
	name := fmt.Sprintf("%s/%s", vendor, model)
	resourceTypeID := ctlrutils.MakeUUIDFromNames(ResourceTypeUUIDNamespace, d.cloudID, name)

	result := models.ResourceType{
		ResourceTypeID: resourceTypeID,
		Name:           name,
		Description:    name,
		Vendor:         vendor,
		Model:          model,
		Version:        "",
		ResourceKind:   models.ResourceKindPhysical,
		ResourceClass:  models.ResourceClassCompute,
		Extensions:     nil,
		DataSourceID:   d.dataSourceID,
		GenerationID:   int(d.generationID.Load()),
	}

	return &result, nil
}

func (d *HardwareDataSource) convertResource(resource *hwmgrcontroller.ResourceInfo) *models.Resource {
	resourceID := resource.ResourceId
	resourcePoolID := resource.ResourcePoolId

	name := fmt.Sprintf("%s/%s", resource.Vendor, resource.Model)
	resourceTypeID := ctlrutils.MakeUUIDFromNames(ResourceTypeUUIDNamespace, d.cloudID, name)

	result := &models.Resource{
		ResourceID:     resourceID,
		Description:    resource.Description,
		ResourceTypeID: resourceTypeID,
		GlobalAssetID:  resource.GlobalAssetId,
		ResourcePoolID: resourcePoolID,
		Extensions: map[string]interface{}{
			modelExtension:            resource.Model,
			vendorExtension:           resource.Vendor,
			memoryExtension:           fmt.Sprintf("%d MiB", resource.Memory),
			processorsExtension:       resource.Processors,
			adminStateExtension:       string(resource.AdminState),
			operationalStateExtension: string(resource.OperationalState),
			usageStateExtension:       string(resource.UsageState),
			hwProfileExtension:        resource.HwProfile,
		},
		Groups:       resource.Groups,
		Tags:         resource.Tags,
		DataSourceID: d.dataSourceID,
		GenerationID: int(d.generationID.Load()),
		ExternalID:   resource.ResourceId.String(),
	}

	if resource.PowerState != nil {
		result.Extensions[powerStateExtension] = string(*resource.PowerState)
	}

	if resource.Labels != nil {
		result.Extensions[labelsExtension] = *resource.Labels
	}

	if resource.Allocated != nil {
		result.Extensions[allocatedExtension] = *resource.Allocated
	}

	if resource.Nics != nil {
		result.Extensions[nicsExtension] = *resource.Nics
	}

	if resource.Storage != nil {
		result.Extensions[storageExtension] = *resource.Storage
	}

	return result
}
