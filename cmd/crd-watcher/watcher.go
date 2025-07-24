/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Generated-By: Cursor/claude-4-sonnet
*/

// nolint: wrapcheck
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	metal3plugincontroller "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/metal3/controller"
	"k8s.io/apimachinery/pkg/api/meta"
)

type CRDWatcher struct {
	clientset        kubernetes.Interface
	dynamicClient    dynamic.Interface
	scheme           *runtime.Scheme
	config           *Config
	formatter        OutputFormatter
	includedBMHNames map[string]bool
	bmhMutex         sync.RWMutex
	inventoryClient  *InventoryClient
}

type WatchEvent struct {
	Type      watch.EventType
	Object    runtime.Object
	Timestamp time.Time
	CRDType   string
}

func NewCRDWatcher(clientset kubernetes.Interface, restConfig *rest.Config, scheme *runtime.Scheme, config *Config) *CRDWatcher {
	dynamicClient := dynamic.NewForConfigOrDie(restConfig)

	// Prepare CRD types list including inventory types if enabled
	crdTypes := make([]string, len(config.CRDTypes))
	copy(crdTypes, config.CRDTypes)

	// Add inventory types if inventory module is enabled
	if config.EnableInventory {
		inventoryTypes := []string{
			"inventory-resource-pools",
			"inventory-resources",
			"inventory-node-clusters",
		}
		crdTypes = append(crdTypes, inventoryTypes...)
	}

	// Create a verification function for stale resource cleanup
	verifyFunc := func(event WatchEvent) bool {
		// This will be set later when the watcher is fully initialized
		return true // Default to keeping resources if verification isn't available yet
	}

	formatter := NewOutputFormatter(config.OutputFormat, config.Watch, config.RefreshInterval, crdTypes, verifyFunc)

	watcher := &CRDWatcher{
		clientset:        clientset,
		dynamicClient:    dynamicClient,
		scheme:           scheme,
		config:           config,
		formatter:        formatter,
		includedBMHNames: make(map[string]bool),
	}

	// Initialize inventory client if enabled
	if config.EnableInventory {
		inventoryConfig := &InventoryConfig{
			ServerURL:      config.InventoryServerURL,
			TokenURL:       config.OAuthTokenURL,
			ClientID:       config.OAuthClientID,
			ClientSecret:   config.OAuthClientSecret,
			Scopes:         config.OAuthScopes,
			ClientCertFile: config.ClientCertFile,
			ClientKeyFile:  config.ClientKeyFile,
			CACertFile:     config.CACertFile,
			MaxRetries:     config.InventoryMaxRetries,
			RetryDelayMs:   config.InventoryRetryDelayMs,
		}

		inventoryClient, err := NewInventoryClient(inventoryConfig)
		if err != nil {
			klog.Errorf("Failed to create inventory client: %v", err)
			klog.V(1).Info("Continuing without inventory module")
		} else {
			watcher.inventoryClient = inventoryClient
			klog.V(1).Info("Inventory module enabled")
		}
	}

	// Set up the resource verification function for stale cleanup
	if tuiFormatter, ok := watcher.formatter.(*TUIFormatter); ok {
		tuiFormatter.verifyFunc = watcher.verifyResourceExists
	}

	return watcher
}

// GetFormatter returns the formatter used by this watcher
func (w *CRDWatcher) GetFormatter() OutputFormatter {
	return w.formatter
}

// verifyResourceExists checks if a resource still exists in its source (Kubernetes API or Inventory API)
func (w *CRDWatcher) verifyResourceExists(event WatchEvent) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch event.CRDType {
	case "inventory-resources":
		return w.verifyInventoryResourceExists(ctx, event)
	case "inventory-resource-pools":
		return w.verifyInventoryResourcePoolExists(ctx, event)
	case "inventory-node-clusters":
		return w.verifyInventoryNodeClusterExists(ctx, event)
	default:
		// For Kubernetes CRDs, use the dynamic client
		return w.verifyKubernetesResourceExists(ctx, event)
	}
}

// verifyInventoryResourceExists checks if an inventory resource still exists
func (w *CRDWatcher) verifyInventoryResourceExists(ctx context.Context, event WatchEvent) bool {
	if w.inventoryClient == nil {
		return true // If no inventory client, assume it exists
	}

	iro, ok := event.Object.(*InventoryResourceObject)
	if !ok {
		return false
	}

	// Try to get all resources and check if this one exists
	// Note: The inventory API doesn't have a direct "get resource by ID" endpoint,
	// so we need to get all resources and search for it
	pools, err := w.inventoryClient.GetAllResourcePools(ctx)
	if err != nil {
		klog.V(3).Infof("Error getting resource pools for verification: %v", err)
		return true // On error, assume it exists to avoid false deletions
	}

	for _, pool := range pools {
		resources, err := w.inventoryClient.GetResources(ctx, pool.ResourcePoolID)
		if err != nil {
			continue // Skip this pool on error
		}

		for _, resource := range resources {
			if resource.ResourceID == iro.Resource.ResourceID {
				return true // Found the resource
			}
		}
	}

	return false // Resource not found in any pool
}

// verifyInventoryResourcePoolExists checks if an inventory resource pool still exists
func (w *CRDWatcher) verifyInventoryResourcePoolExists(ctx context.Context, event WatchEvent) bool {
	if w.inventoryClient == nil {
		return true // If no inventory client, assume it exists
	}

	rpo, ok := event.Object.(*ResourcePoolObject)
	if !ok {
		return false
	}

	pools, err := w.inventoryClient.GetAllResourcePools(ctx)
	if err != nil {
		klog.V(3).Infof("Error getting resource pools for verification: %v", err)
		return true // On error, assume it exists to avoid false deletions
	}

	for _, pool := range pools {
		if pool.ResourcePoolID == rpo.ResourcePool.ResourcePoolID {
			return true // Found the resource pool
		}
	}

	return false // Resource pool not found
}

// verifyInventoryNodeClusterExists checks if an inventory node cluster still exists
func (w *CRDWatcher) verifyInventoryNodeClusterExists(ctx context.Context, event WatchEvent) bool {
	if w.inventoryClient == nil {
		return true // If no inventory client, assume it exists
	}

	nco, ok := event.Object.(*NodeClusterObject)
	if !ok {
		return false
	}

	clusters, err := w.inventoryClient.GetAllNodeClusters(ctx)
	if err != nil {
		klog.V(3).Infof("Error getting node clusters for verification: %v", err)
		return true // On error, assume it exists to avoid false deletions
	}

	for _, cluster := range clusters {
		if cluster.Name == nco.NodeCluster.Name {
			return true // Found the node cluster
		}
	}

	return false // Node cluster not found
}

// verifyKubernetesResourceExists checks if a Kubernetes resource still exists
func (w *CRDWatcher) verifyKubernetesResourceExists(ctx context.Context, event WatchEvent) bool {
	accessor, err := meta.Accessor(event.Object)
	if err != nil {
		klog.V(3).Infof("Cannot get accessor for verification: %v", err)
		return true // On error, assume it exists
	}

	// Get the GVR for this CRD type
	gvr, err := w.getGVRForCRDType(event.CRDType)
	if err != nil {
		klog.V(3).Infof("Cannot get GVR for %s: %v", event.CRDType, err)
		return true // On error, assume it exists
	}

	namespace := accessor.GetNamespace()
	name := accessor.GetName()

	var resourceInterface dynamic.ResourceInterface
	if namespace != "" {
		resourceInterface = w.dynamicClient.Resource(gvr).Namespace(namespace)
	} else {
		resourceInterface = w.dynamicClient.Resource(gvr)
	}

	_, err = resourceInterface.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false // Resource confirmed not found
		}
		klog.V(3).Infof("Error verifying resource existence: %v", err)
		return true // On other errors, assume it exists to avoid false deletions
	}

	return true // Resource exists
}

func (w *CRDWatcher) Start(ctx context.Context) error {
	// Initialize the BareMetalHost tracking map with existing resources
	if err := w.initializeBMHTracking(ctx); err != nil {
		klog.V(1).Infof("Failed to initialize BareMetalHost tracking: %v", err)
		// Don't fail, continue with empty map
	}

	// If inventory is enabled, fetch initial inventory data
	if w.inventoryClient != nil {
		// Fetch and display initial inventory resource pools
		if err := w.fetchAndDisplayInventoryResourcePools(ctx); err != nil {
			klog.Errorf("Failed to fetch initial inventory resource pools: %v", err)
		}

		// Fetch and display initial inventory resources
		if err := w.fetchAndDisplayInventoryResources(ctx); err != nil {
			klog.Errorf("Failed to fetch initial inventory resources: %v", err)
		}

		// Fetch and display initial inventory node clusters
		if err := w.fetchAndDisplayInventoryNodeClusters(ctx); err != nil {
			klog.Errorf("Failed to fetch initial inventory node clusters: %v", err)
		}
	}

	g, gCtx := errgroup.WithContext(ctx)

	// Start watchers for each specified CRD type
	for _, crdType := range w.config.CRDTypes {
		crdType := crdType // capture loop variable
		g.Go(func() error {
			return w.watchCRD(gCtx, crdType)
		})
	}

	return g.Wait()
}

func (w *CRDWatcher) ListAndDisplay(ctx context.Context) error {
	// Initialize the BareMetalHost tracking map with existing resources
	if err := w.initializeBMHTracking(ctx); err != nil {
		klog.V(1).Infof("Failed to initialize BareMetalHost tracking: %v", err)
		// Don't fail, continue with empty map
	}

	// Collect all events from listing resources
	var allEvents []WatchEvent

	// List Kubernetes CRD resources for each specified CRD type
	for _, crdType := range w.config.CRDTypes {
		events, err := w.listCRDResources(ctx, crdType)
		if err != nil {
			klog.Errorf("Failed to list %s: %v", crdType, err)
			continue
		}
		allEvents = append(allEvents, events...)
	}

	// Fetch inventory resources if enabled
	if w.inventoryClient != nil {
		inventoryEvents, err := w.listInventoryResources(ctx)
		if err != nil {
			klog.Errorf("Failed to list inventory resources: %v", err)
		} else {
			allEvents = append(allEvents, inventoryEvents...)
		}

		// Fetch inventory resource pools
		resourcePoolEvents, err := w.listInventoryResourcePools(ctx)
		if err != nil {
			klog.Errorf("Failed to list inventory resource pools: %v", err)
		} else {
			allEvents = append(allEvents, resourcePoolEvents...)
		}

		// Fetch inventory node clusters
		nodeClusterEvents, err := w.listInventoryNodeClusters(ctx)
		if err != nil {
			klog.Errorf("Failed to list inventory node clusters: %v", err)
		} else {
			allEvents = append(allEvents, nodeClusterEvents...)
		}
	}

	// Process events through formatter
	for _, event := range allEvents {
		if err := w.formatter.FormatEvent(event); err != nil {
			klog.Errorf("Error formatting event: %v", err)
		}
	}

	// Flush events for table formatter
	if tableFormatter, ok := w.formatter.(*TableFormatter); ok {
		if err := tableFormatter.FlushEvents(); err != nil {
			return fmt.Errorf("failed to flush events: %w", err)
		}
	}

	return nil
}

func (w *CRDWatcher) listCRDResources(ctx context.Context, crdType string) ([]WatchEvent, error) {
	gvr, err := w.getGVRForCRDType(crdType)
	if err != nil {
		return nil, fmt.Errorf("failed to get GVR for CRD type %s: %w", crdType, err)
	}

	namespace := w.getNamespace()
	var listOptions metav1.ListOptions

	var list *unstructured.UnstructuredList
	if w.config.AllNamespaces || namespace == "" {
		list, err = w.dynamicClient.Resource(gvr).List(ctx, listOptions)
	} else {
		list, err = w.dynamicClient.Resource(gvr).Namespace(namespace).List(ctx, listOptions)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to list %s: %w", crdType, err)
	}

	var events []WatchEvent
	for _, item := range list.Items {
		typedObj, err := w.convertToTypedObject(&item, crdType)
		if err != nil {
			klog.V(1).Infof("Failed to convert object: %v", err)
			continue
		}

		// Apply filtering logic
		shouldInclude := true

		// Filter BareMetalHosts based on resource selector labels
		if crdType == CRDTypeBareMetalHosts {
			if bmh, ok := typedObj.(*metal3v1alpha1.BareMetalHost); ok {
				shouldInclude = w.shouldIncludeBareMetalHost(bmh)
			}
		}

		// Filter firmware CRDs based on matching BareMetalHost names
		if crdType == CRDTypeHostFirmwareComponents || crdType == CRDTypeHostFirmwareSettings {
			accessor, _ := meta.Accessor(typedObj)
			resourceName := accessor.GetName()

			w.bmhMutex.RLock()
			_, bmhExists := w.includedBMHNames[resourceName]
			w.bmhMutex.RUnlock()

			shouldInclude = bmhExists
		}

		if shouldInclude {
			event := WatchEvent{
				Type:      watch.Added, // For listing, we treat all as "Added"
				Object:    typedObj,
				Timestamp: time.Now(),
				CRDType:   crdType,
			}
			events = append(events, event)
		}
	}

	return events, nil
}

func (w *CRDWatcher) listInventoryResources(ctx context.Context) ([]WatchEvent, error) {
	klog.V(1).Info("Fetching inventory resources")

	resources, err := w.inventoryClient.GetAllResources(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get inventory resources: %w", err)
	}

	var events []WatchEvent
	for _, resource := range resources {
		event := WatchEvent{
			Type:      watch.Added, // For listing, we treat all as "Added"
			Object:    resource.ToRuntimeObject(),
			Timestamp: time.Now(),
			CRDType:   "inventory-resources",
		}
		events = append(events, event)
	}

	klog.V(1).Infof("Collected %d inventory resource events", len(events))
	return events, nil
}

func (w *CRDWatcher) listInventoryResourcePools(ctx context.Context) ([]WatchEvent, error) {
	klog.V(1).Info("Fetching inventory resource pools")

	resourcePools, err := w.inventoryClient.GetAllResourcePools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get inventory resource pools: %w", err)
	}

	var events []WatchEvent
	for _, resourcePool := range resourcePools {
		event := WatchEvent{
			Type:      watch.Added, // For listing, we treat all as "Added"
			Object:    resourcePool.ToRuntimeObject(),
			Timestamp: time.Now(),
			CRDType:   "inventory-resource-pools",
		}
		events = append(events, event)
	}

	klog.V(1).Infof("Collected %d inventory resource pool events", len(events))
	return events, nil
}

func (w *CRDWatcher) listInventoryNodeClusters(ctx context.Context) ([]WatchEvent, error) {
	klog.V(1).Info("Fetching inventory node clusters")

	nodeClusters, err := w.inventoryClient.GetAllNodeClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get inventory node clusters: %w", err)
	}

	var events []WatchEvent
	for _, nodeCluster := range nodeClusters {
		// Filter out the local-cluster
		if nodeCluster.Name == "local-cluster" {
			klog.V(2).Infof("Filtering out local-cluster from node clusters listing")
			continue
		}

		event := WatchEvent{
			Type:      watch.Added, // For listing, we treat all as "Added"
			Object:    nodeCluster.ToRuntimeObject(),
			Timestamp: time.Now(),
			CRDType:   "inventory-node-clusters",
		}
		events = append(events, event)
	}

	klog.V(1).Infof("Collected %d inventory node cluster events (filtered from %d total)", len(events), len(nodeClusters))
	return events, nil
}

func (w *CRDWatcher) fetchAndDisplayInventoryResourcePools(ctx context.Context) error {
	resourcePools, err := w.inventoryClient.GetAllResourcePools(ctx)
	if err != nil {
		return fmt.Errorf("failed to get inventory resource pools: %w", err)
	}

	for _, resourcePool := range resourcePools {
		event := WatchEvent{
			Type:      watch.Added,
			Object:    resourcePool.ToRuntimeObject(),
			Timestamp: time.Now(),
			CRDType:   "inventory-resource-pools",
		}
		if err := w.formatter.FormatEvent(event); err != nil {
			klog.Errorf("Error formatting resource pool event: %v", err)
		}
	}

	klog.V(1).Infof("Displayed %d inventory resource pools", len(resourcePools))
	return nil
}

func (w *CRDWatcher) fetchAndDisplayInventoryResources(ctx context.Context) error {
	resources, err := w.inventoryClient.GetAllResources(ctx)
	if err != nil {
		return fmt.Errorf("failed to get inventory resources: %w", err)
	}

	for _, resource := range resources {
		event := WatchEvent{
			Type:      watch.Added,
			Object:    resource.ToRuntimeObject(),
			Timestamp: time.Now(),
			CRDType:   "inventory-resources",
		}
		if err := w.formatter.FormatEvent(event); err != nil {
			klog.Errorf("Error formatting resource event: %v", err)
		}
	}

	klog.V(1).Infof("Displayed %d inventory resources", len(resources))
	return nil
}

func (w *CRDWatcher) fetchAndDisplayInventoryNodeClusters(ctx context.Context) error {
	nodeClusters, err := w.inventoryClient.GetAllNodeClusters(ctx)
	if err != nil {
		return fmt.Errorf("failed to get inventory node clusters: %w", err)
	}

	filteredCount := 0
	for _, nodeCluster := range nodeClusters {
		// Filter out the local-cluster
		if nodeCluster.Name == "local-cluster" {
			klog.V(2).Infof("Filtering out local-cluster from node clusters display")
			continue
		}

		event := WatchEvent{
			Type:      watch.Added,
			Object:    nodeCluster.ToRuntimeObject(),
			Timestamp: time.Now(),
			CRDType:   "inventory-node-clusters",
		}
		if err := w.formatter.FormatEvent(event); err != nil {
			klog.Errorf("Error formatting node cluster event: %v", err)
		}
		filteredCount++
	}

	klog.V(1).Infof("Displayed %d inventory node clusters (filtered from %d total)", filteredCount, len(nodeClusters))
	return nil
}

// initializeBMHTracking pre-populates the BareMetalHost tracking map with existing resources
func (w *CRDWatcher) initializeBMHTracking(ctx context.Context) error {
	// Initialize if baremetalhosts or any firmware CRDs are being watched
	needsBMHTracking := false
	for _, crdType := range w.config.CRDTypes {
		if crdType == CRDTypeBareMetalHosts || crdType == CRDTypeHostFirmwareComponents || crdType == CRDTypeHostFirmwareSettings {
			needsBMHTracking = true
			break
		}
	}
	if !needsBMHTracking {
		klog.V(2).Info("No BareMetalHost or firmware CRDs being watched, skipping BMH tracking initialization")
		return nil
	}

	gvr, err := w.getGVRForCRDType(CRDTypeBareMetalHosts)
	if err != nil {
		return fmt.Errorf("failed to get GVR for baremetalhosts: %w", err)
	}

	namespace := w.getNamespace()
	var listOptions metav1.ListOptions

	var list *unstructured.UnstructuredList
	if w.config.AllNamespaces || namespace == "" {
		list, err = w.dynamicClient.Resource(gvr).List(ctx, listOptions)
		klog.V(2).Info("Listing BareMetalHosts from all namespaces")
	} else {
		list, err = w.dynamicClient.Resource(gvr).Namespace(namespace).List(ctx, listOptions)
		klog.V(2).Infof("Listing BareMetalHosts from namespace: %s", namespace)
	}

	if err != nil {
		return fmt.Errorf("failed to list BareMetalHosts: %w", err)
	}

	w.bmhMutex.Lock()
	defer w.bmhMutex.Unlock()

	initialCount := 0
	filteredCount := 0
	for _, item := range list.Items {
		typedObj, err := w.convertToTypedObject(&item, CRDTypeBareMetalHosts)
		if err != nil {
			klog.V(1).Infof("Failed to convert BareMetalHost: %v", err)
			continue
		}

		if bmh, ok := typedObj.(*metal3v1alpha1.BareMetalHost); ok {
			initialCount++
			if w.shouldIncludeBareMetalHost(bmh) {
				w.includedBMHNames[bmh.Name] = true
				filteredCount++
				klog.V(3).Infof("Initialized tracking for BareMetalHost: %s (namespace: %s)", bmh.Name, bmh.Namespace)
			} else {
				klog.V(3).Infof("Filtered out BareMetalHost: %s (namespace: %s) - missing resource selector labels", bmh.Name, bmh.Namespace)
			}
		}
	}

	klog.V(1).Infof("Initialized BareMetalHost tracking: %d/%d hosts included after filtering", filteredCount, initialCount)
	return nil
}

func (w *CRDWatcher) watchCRD(ctx context.Context, crdType string) error {
	gvr, err := w.getGVRForCRDType(crdType)
	if err != nil {
		return fmt.Errorf("failed to get GVR for CRD type %s: %w", crdType, err)
	}

	klog.V(2).Infof("Starting watcher for CRD type: %s", crdType)

	namespace := w.getNamespace()

	var watcher watch.Interface
	if w.config.AllNamespaces || namespace == "" {
		watcher, err = w.dynamicClient.Resource(gvr).Watch(ctx, metav1.ListOptions{
			FieldSelector: fields.Everything().String(),
		})
	} else {
		watcher, err = w.dynamicClient.Resource(gvr).Namespace(namespace).Watch(ctx, metav1.ListOptions{
			FieldSelector: fields.Everything().String(),
		})
	}

	if err != nil {
		return fmt.Errorf("failed to create watcher for %s: %w", crdType, err)
	}
	defer watcher.Stop()

	klog.V(1).Infof("Successfully started watching %s", crdType)

	for {
		select {
		case <-ctx.Done():
			klog.V(1).Infof("Stopping watcher for %s", crdType)
			return nil
		case event, ok := <-watcher.ResultChan():
			if !ok {
				klog.V(1).Infof("Watch channel closed for %s, restarting...", crdType)
				return w.watchCRD(ctx, crdType) // Restart the watch
			}

			if err := w.handleWatchEvent(event, crdType); err != nil {
				klog.Errorf("Error handling watch event for %s: %v", crdType, err)
			}
		}
	}
}

func (w *CRDWatcher) handleWatchEvent(event watch.Event, crdType string) error {
	typedObj, err := w.convertToTypedObject(event.Object, crdType)
	if err != nil {
		return fmt.Errorf("failed to convert object: %w", err)
	}

	// Handle BareMetalHost filtering and tracking
	if crdType == CRDTypeBareMetalHosts {
		if bmh, ok := typedObj.(*metal3v1alpha1.BareMetalHost); ok {
			shouldInclude := w.shouldIncludeBareMetalHost(bmh)

			w.bmhMutex.Lock()
			if shouldInclude {
				// Add or keep this BMH name in our tracking
				if event.Type != watch.Deleted {
					w.includedBMHNames[bmh.Name] = true
				}
			} else {
				// Remove this BMH name from tracking and don't process the event
				delete(w.includedBMHNames, bmh.Name)
				w.bmhMutex.Unlock()
				return nil // Skip this BareMetalHost as it doesn't have the required labels
			}

			// Handle deletion events
			if event.Type == watch.Deleted {
				delete(w.includedBMHNames, bmh.Name)
			}
			w.bmhMutex.Unlock()
		}
	}

	// Filter firmware CRDs based on matching BareMetalHost names
	if crdType == CRDTypeHostFirmwareComponents || crdType == CRDTypeHostFirmwareSettings {
		accessor, _ := meta.Accessor(typedObj)
		resourceName := accessor.GetName()

		w.bmhMutex.RLock()
		_, bmhExists := w.includedBMHNames[resourceName]
		includedNames := make([]string, 0, len(w.includedBMHNames))
		for name := range w.includedBMHNames {
			includedNames = append(includedNames, name)
		}
		w.bmhMutex.RUnlock()

		if !bmhExists {
			klog.V(2).Infof("Filtering out %s '%s' - no matching BareMetalHost found (included BMHs: %v)", crdType, resourceName, includedNames)
			return nil
		} else {
			klog.V(3).Infof("Including %s '%s' - matches BareMetalHost", crdType, resourceName)
		}
	}

	watchEvent := WatchEvent{
		Type:      event.Type,
		Object:    typedObj,
		Timestamp: time.Now(),
		CRDType:   crdType,
	}

	return w.formatter.FormatEvent(watchEvent)
}

func (w *CRDWatcher) convertToTypedObject(obj runtime.Object, crdType string) (runtime.Object, error) {
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return obj, nil // Return as-is if not unstructured
	}

	// Convert based on CRD type
	switch crdType {
	case CRDTypeProvisioningRequests:
		provReq := &provisioningv1alpha1.ProvisioningRequest{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(
			unstructuredObj.Object, provReq); err != nil {
			return nil, err
		}
		return provReq, nil
	case CRDTypeNodeAllocationRequests:
		nodeAllocReq := &pluginsv1alpha1.NodeAllocationRequest{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(
			unstructuredObj.Object, nodeAllocReq); err != nil {
			return nil, err
		}
		return nodeAllocReq, nil
	case CRDTypeAllocatedNodes:
		allocNode := &pluginsv1alpha1.AllocatedNode{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(
			unstructuredObj.Object, allocNode); err != nil {
			return nil, err
		}
		return allocNode, nil
	case CRDTypeBareMetalHosts:
		bmh := &metal3v1alpha1.BareMetalHost{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(
			unstructuredObj.Object, bmh); err != nil {
			return nil, err
		}
		return bmh, nil
	case CRDTypeHostFirmwareComponents:
		hfc := &metal3v1alpha1.HostFirmwareComponents{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(
			unstructuredObj.Object, hfc); err != nil {
			return nil, err
		}
		return hfc, nil
	case CRDTypeHostFirmwareSettings:
		hfs := &metal3v1alpha1.HostFirmwareSettings{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(
			unstructuredObj.Object, hfs); err != nil {
			return nil, err
		}
		return hfs, nil
	default:
		return obj, nil
	}
}

func (w *CRDWatcher) getGVRForCRDType(crdType string) (schema.GroupVersionResource, error) {
	switch crdType {
	case CRDTypeProvisioningRequests:
		return schema.GroupVersionResource{
			Group:    "clcm.openshift.io",
			Version:  "v1alpha1",
			Resource: CRDTypeProvisioningRequests,
		}, nil
	case CRDTypeNodeAllocationRequests:
		return schema.GroupVersionResource{
			Group:    "plugins.clcm.openshift.io",
			Version:  "v1alpha1",
			Resource: CRDTypeNodeAllocationRequests,
		}, nil
	case CRDTypeAllocatedNodes:
		return schema.GroupVersionResource{
			Group:    "plugins.clcm.openshift.io",
			Version:  "v1alpha1",
			Resource: CRDTypeAllocatedNodes,
		}, nil
	case CRDTypeBareMetalHosts:
		return schema.GroupVersionResource{
			Group:    "metal3.io",
			Version:  "v1alpha1",
			Resource: CRDTypeBareMetalHosts,
		}, nil
	case CRDTypeHostFirmwareComponents:
		return schema.GroupVersionResource{
			Group:    "metal3.io",
			Version:  "v1alpha1",
			Resource: CRDTypeHostFirmwareComponents,
		}, nil
	case CRDTypeHostFirmwareSettings:
		return schema.GroupVersionResource{
			Group:    "metal3.io",
			Version:  "v1alpha1",
			Resource: CRDTypeHostFirmwareSettings,
		}, nil
	default:
		return schema.GroupVersionResource{}, fmt.Errorf("unknown CRD type: %s", crdType)
	}
}

func (w *CRDWatcher) getNamespace() string {
	if w.config.AllNamespaces {
		return ""
	}

	if w.config.Namespace != "" {
		return w.config.Namespace
	}

	// Try to get current namespace from service account
	if data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		return string(data)
	}

	return "default"
}

// shouldIncludeBareMetalHost checks if a BareMetalHost should be included based on resource selector labels
func (w *CRDWatcher) shouldIncludeBareMetalHost(bmh *metal3v1alpha1.BareMetalHost) bool {
	if bmh.Labels == nil {
		return false
	}

	// Check if any label key starts with the resource selector prefix
	for labelKey := range bmh.Labels {
		if strings.HasPrefix(labelKey, metal3plugincontroller.LabelPrefixResourceSelector) {
			return true
		}
	}

	return false
}
