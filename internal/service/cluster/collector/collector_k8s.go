package collector

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/openshift/assisted-service/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	v1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

// Interface compile enforcement
var _ ClusterDataSource = (*K8SDataSource)(nil)

// K8SDataSource defines an instance of a data source collector that interacts with the Kubernetes API
type K8SDataSource struct {
	dataSourceID      uuid.UUID
	cloudID           uuid.UUID
	hubClient         client.WithWatch
	generationID      int
	asyncChangeEvents chan<- *async.AsyncChangeEvent
}

const (
	NodeClusterTypeUUIDNamespace     = "b9b24e78-5764-461b-8a0b-55bbe189d3d9"
	ClusterResourceUUIDNamespace     = "75d74e65-f51f-4195-94ea-e4495b940a8f"
	ClusterResourceTypeUUIDNamespace = "be370c55-e9fc-4094-81c3-83d6be4a6a14"
)

const (
	clusterReflectorName = "cluster-reflector"
	agentReflectorName   = "agent-reflector"
)

// NewK8SDataSource creates a new instance of an K8S data source collector whose purpose is to collect data from the
// Kubernetes API to be included in the node cluster, node cluster type, cluster resource and cluster resource type
// tables.
func NewK8SDataSource(cloudID uuid.UUID, extensions []string) (DataSource, error) {
	// Create a k8s client for the hub to be able to retrieve managed cluster info
	hubClient, err := k8s.NewClientForHub()
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client for hub: %w", err)
	}

	return &K8SDataSource{
		generationID: 0,
		cloudID:      cloudID,
		hubClient:    hubClient,
	}, nil
}

// Name returns the name of this data source
func (d *K8SDataSource) Name() string {
	return "K8S"
}

// GetID returns the data source ID for this data source
func (d *K8SDataSource) GetID() uuid.UUID {
	return d.dataSourceID
}

// Init initializes the data source with its configuration data; including the ID, the GenerationID, and its extension
// values if provided.
func (d *K8SDataSource) Init(uuid uuid.UUID, generationID int, asyncEventChannel chan<- *async.AsyncChangeEvent) {
	d.dataSourceID = uuid
	d.generationID = generationID
	d.asyncChangeEvents = asyncEventChannel
}

// GetGenerationID retrieves the current generation id for this data source.
func (d *K8SDataSource) GetGenerationID() int {
	return d.generationID
}

// IncrGenerationID increments the current generation id for this data source.
func (d *K8SDataSource) IncrGenerationID() int {
	d.generationID++
	return d.generationID
}

// makeClusterResourceTypeName builds a descriptive string to represent the resource type
func (d *K8SDataSource) makeClusterResourceTypeName(architecture, cores string) string {
	return fmt.Sprintf("%s CPU with %s Cores", strings.ToUpper(architecture), cores)
}

// makeClusterResourceTypeID builds a UUID value for this resource type based on its name so that it has
// a consistent value each time it is created.
func (d *K8SDataSource) makeClusterResourceTypeID(architecture, cores string) uuid.UUID {
	return utils.MakeUUIDFromName(ClusterResourceTypeUUIDNamespace, d.cloudID, d.makeClusterResourceTypeName(architecture, cores))
}

// MakeClusterResourceType creates an instance of a ResourceType from a Resource object.
func (d *K8SDataSource) MakeClusterResourceType(resource *models.ClusterResource) (*models.ClusterResourceType, error) {
	extensions := resource.Extensions
	cpuExtension, ok := (*extensions)["cpu"]
	if !ok {
		// This should never happen since we inserted them when we create the ClusterResource
		return nil, fmt.Errorf("no cores extension")
	}
	cpu := cpuExtension.(map[string]string)
	architecture := cpu["architecture"]
	cores := cpu["cores"]

	resourceTypeName := d.makeClusterResourceTypeName(architecture, cores)
	resourceTypeID := utils.MakeUUIDFromName(ClusterResourceTypeUUIDNamespace, d.cloudID, resourceTypeName)

	result := models.ClusterResourceType{
		ClusterResourceTypeID: resourceTypeID,
		Name:                  resourceTypeName,
		Description:           resourceTypeName,
		Extensions:            nil,
		DataSourceID:          d.dataSourceID,
		GenerationID:          d.generationID,
	}

	return &result, nil
}

// MakeNodeClusterType creates an instance of a NodeClusterType from a NodeCluster object.
func (d *K8SDataSource) MakeNodeClusterType(resource *models.NodeCluster) (*models.NodeClusterType, error) {
	extensions := resource.Extensions
	if extensions == nil {
		return nil, fmt.Errorf("no extensions found")
	}

	// We know these extensions exist because we checked when we processed the NodeCluster
	vendor := (*extensions)[utils.ClusterVendorExtension].(string)
	version := (*extensions)[utils.OpenshiftVersionLabelName].(string)
	clusterType := (*extensions)[utils.ClusterModelExtension].(string)

	resourceTypeName := d.makeNodeClusterTypeName(clusterType, vendor, version)
	resourceTypeID := utils.MakeUUIDFromName(NodeClusterTypeUUIDNamespace, d.cloudID, resourceTypeName)
	alarmDictionaryID := utils.MakeUUIDFromName(NodeClusterTypeUUIDNamespace, d.cloudID, fmt.Sprintf("%s-%s", resourceTypeName, "alarms"))

	// We expect that the standard will eventually evolve to contain more attributes to align more
	// closely with how ResourceType was defined, but for now we'll add some additional info as
	// extensions so that they are known to clients.  These will be used by the alarm code to build
	// the dictionary.
	typeExtensions := map[string]interface{}{
		utils.ClusterVendorExtension:            vendor,
		utils.ClusterVersionExtension:           version,
		utils.ClusterModelExtension:             clusterType,
		utils.ClusterAlarmDictionaryIDExtension: alarmDictionaryID,
	}

	result := models.NodeClusterType{
		NodeClusterTypeID: resourceTypeID,
		Name:              resourceTypeName,
		Description:       resourceTypeName,
		Extensions:        &typeExtensions,
		DataSourceID:      d.dataSourceID,
		GenerationID:      d.generationID,
	}

	return &result, nil
}

// convertAgentToClusterResource converts an Agent CR to a ClusterResource object
func (d *K8SDataSource) convertAgentToClusterResource(agent *v1beta1.Agent) models.ClusterResource {
	// Build a unique UUID value using the namespace and name.  Choosing not to tie ourselves to the agent UUID since
	// we don't know if/when it can change and how we want to behave if the node gets deleted and re-installed.
	name := fmt.Sprintf("%s/%s", agent.Namespace, agent.Name)
	resourceID := utils.MakeUUIDFromName(ClusterResourceUUIDNamespace, d.cloudID, name)

	architecture := agent.Status.Inventory.Cpu.Architecture
	cores := agent.Status.Inventory.Cpu.Count
	resourceTypeID := d.makeClusterResourceTypeID(architecture, strconv.FormatInt(cores, 10))

	extensions := map[string]interface{}{
		clusterNameExtension: agent.Spec.ClusterDeploymentName.Name,
		"cpu": map[string]string{
			"cores":        strconv.FormatInt(cores, 10),
			"architecture": architecture,
			"model":        agent.Status.Inventory.Cpu.ModelName,
		},
		"memory": map[string]string{
			"GiB": strconv.FormatInt(agent.Status.Inventory.Memory.PhysicalBytes/(1024*1024*1024), 10),
		},
		"role": string(agent.Status.Role),
		"system": map[string]string{
			"manufacturer": agent.Status.Inventory.SystemVendor.Manufacturer,
			"product":      agent.Status.Inventory.SystemVendor.ProductName,
			"serial":       agent.Status.Inventory.SystemVendor.SerialNumber,
		},
		// TODO: add more info for disks, nics, etc...
	}

	// NodeClusterID is filled in by the caller since it has to convert the cluster name to an id.
	return models.ClusterResource{
		ClusterResourceID:     resourceID,
		ClusterResourceTypeID: resourceTypeID,
		Name:                  agent.Spec.Hostname,
		Description:           agent.Spec.Hostname,
		Extensions:            &extensions,
		ArtifactResourceIDs:   nil,         // TODO: need to link this to template?
		ResourceID:            uuid.UUID{}, // TODO: need to link this to h/w resource
		ExternalID:            name,
		DataSourceID:          d.dataSourceID,
		GenerationID:          d.generationID,
	}
}

// makeNodeClusterTypeName builds a descriptive string to represent the node cluster type
func (d *K8SDataSource) makeNodeClusterTypeName(clusterType, vendor, version string) string {
	return fmt.Sprintf("%s-%s-%s", clusterType, vendor, version)
}

// makeClusterResourceTypeID builds a UUID value for this resource type based on its name so that it has
// a consistent value each time it is created.
func (d *K8SDataSource) makeNodeClusterTypeID(clusterType, vendor, version string) uuid.UUID {
	return utils.MakeUUIDFromName(NodeClusterTypeUUIDNamespace, d.cloudID, d.makeNodeClusterTypeName(clusterType, vendor, version))
}

// getExtensionsFromLabels converts a label map to an extensions map
func (d *K8SDataSource) getExtensionsFromLabels(labels map[string]string) map[string]interface{} {
	result := map[string]interface{}{}
	for key, value := range labels {
		result[key] = value
	}
	return result
}

// convertManagedClusterToNodeCluster converts a ManagedCluster to a ManagedCluster object
func (d *K8SDataSource) convertManagedClusterToNodeCluster(cluster *v1.ManagedCluster) (models.NodeCluster, error) {
	vendor, found := cluster.Labels[utils.ClusterVendorExtension]
	if !found {
		return models.NodeCluster{}, fmt.Errorf("no vendor label found on cluster %s", cluster.Name)
	}

	var version string
	if vendor == "OpenShift" {
		version, found = cluster.Labels[utils.OpenshiftVersionLabelName]
		if !found {
			return models.NodeCluster{}, fmt.Errorf("no version label found on cluster %s", cluster.Name)
		}
	}

	var clusterID string
	clusterID, found = cluster.Labels[utils.ClusterIDLabelName]
	if !found {
		return models.NodeCluster{}, fmt.Errorf("no '%s' found on cluster %s", utils.ClusterIDLabelName, cluster.Name)
	}

	// Determine the cluster type so we can differentiate between a hub and a managed cluster
	clusterType := utils.ClusterModelManagedCluster
	if value, found := cluster.Labels[utils.LocalClusterLabelName]; found {
		localCluster, err := strconv.ParseBool(value)
		if err != nil {
			return models.NodeCluster{}, fmt.Errorf("failed to parse '%s' value as boolean: %w", utils.LocalClusterLabelName, err)
		}
		if localCluster {
			clusterType = utils.ClusterModelHubCluster
		}
	}

	// Use the cluster ID label to facilitate mapping to alarms and possibly other entities
	resourceID, err := uuid.Parse(clusterID)
	if err != nil {
		return models.NodeCluster{}, fmt.Errorf("failed to parse '%s' label value '%s' into UUID", utils.ClusterIDLabelName, clusterID)
	}

	// For now continue to generate UUID values based on the string values assigned
	resourceTypeID := d.makeNodeClusterTypeID(clusterType, vendor, version)

	// Until we know which extensions we want to expose we'll just publish all labels
	extensions := d.getExtensionsFromLabels(cluster.Labels)

	// Add the cluster type so that clients don't have to depend on the local-cluster bool
	extensions[utils.ClusterModelExtension] = clusterType

	to := models.NodeCluster{
		NodeClusterID:                  resourceID,
		NodeClusterTypeID:              resourceTypeID,
		ClientNodeClusterID:            uuid.UUID{}, // TODO: supposed to get this from provisioning request
		Name:                           cluster.Name,
		Description:                    cluster.Name,
		Extensions:                     &extensions,
		ClusterDistributionDescription: "",          // TODO: not sure where to get this from
		ArtifactResourceID:             uuid.UUID{}, // TODO: need to get this from template?
		ClusterResourceGroups:          nil,         // TODO: not sure where to get this from
		ExternalID:                     cluster.Name,
		DataSourceID:                   d.dataSourceID,
		GenerationID:                   d.generationID,
	}

	return to, nil
}

// handleClusterWatchEvent handles an async event received from the managed cluster watcher
func (d *K8SDataSource) handleClusterWatchEvent(ctx context.Context, cluster *v1.ManagedCluster, eventType async.AsyncEventType) (uuid.UUID, error) {
	slog.Debug("handleWatchEvent received for managed cluster", "agent", cluster.Name, "type", eventType)

	record, err := d.convertManagedClusterToNodeCluster(cluster)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to convert managed cluster to node cluster: %w", err)
	}

	select {
	case <-ctx.Done():
		slog.Info("context cancelled while writing to async event channel; aborting")
		return uuid.Nil, fmt.Errorf("context cancelled; aborting")
	case d.asyncChangeEvents <- &async.AsyncChangeEvent{
		DataSourceID: d.dataSourceID,
		EventType:    eventType,
		Object:       record}:
		return record.NodeClusterID, nil
	}
}

// handleAgentWatchEvent handles an async event received from the agent watcher
func (d *K8SDataSource) handleAgentWatchEvent(ctx context.Context, agent *v1beta1.Agent, eventType async.AsyncEventType) (uuid.UUID, error) {
	slog.Debug("handleWatchEvent received for agent", "agent", agent.Name, "type", eventType)

	record := d.convertAgentToClusterResource(agent)

	select {
	case <-ctx.Done():
		slog.Info("context cancelled while writing to async event channel; aborting")
		return uuid.Nil, fmt.Errorf("context cancelled; aborting")
	case d.asyncChangeEvents <- &async.AsyncChangeEvent{
		DataSourceID: d.dataSourceID,
		EventType:    eventType,
		Object:       record}:
		return record.ClusterResourceID, nil
	}
}

// HandleAsyncEvent handles an add/update/delete to an object received by from the Reflector.
func (d *K8SDataSource) HandleAsyncEvent(ctx context.Context, obj interface{}, eventType async.AsyncEventType) (uuid.UUID, error) {
	slog.Debug("handleWatchEvent received for store adapter", "type", eventType, "object", fmt.Sprintf("%T", obj))
	switch value := obj.(type) {
	case *v1.ManagedCluster:
		return d.handleClusterWatchEvent(ctx, value, eventType)
	case *v1beta1.Agent:
		return d.handleAgentWatchEvent(ctx, value, eventType)
	default:
		// We are only watching for specific event types so this should happen.
		slog.Warn("Unknown object type", "type", fmt.Sprintf("%T", obj))
		return uuid.Nil, fmt.Errorf("unknown type: %T", obj)
	}
}

// HandleSyncComplete handles the end of a sync operation by sending an event down to the Collector.
func (d *K8SDataSource) HandleSyncComplete(ctx context.Context, objectType runtime.Object, keys []uuid.UUID) error {
	var object db.Model
	switch objectType.(type) {
	case *v1.ManagedCluster:
		object = models.NodeCluster{}
	case *v1beta1.Agent:
		object = models.ClusterResource{}
	default:
		// This should never happen since we watch for specific types
		slog.Warn("Unknown object type", "type", fmt.Sprintf("%T", objectType))
		return nil
	}

	select {
	case <-ctx.Done():
		slog.Info("context cancelled while writing to async event channel; aborting")
		return fmt.Errorf("context cancelled; aborting")
	case d.asyncChangeEvents <- &async.AsyncChangeEvent{
		DataSourceID: d.dataSourceID,
		EventType:    async.SyncComplete,
		Object:       object,
		Keys:         keys}:
		return nil
	}
}

// Watch starts a watcher for each of the resources supported by this data source.
// The watch is dispatched to a go routine.  If the context is canceled, then the watchers are stopped.
func (d *K8SDataSource) Watch(ctx context.Context) error {

	// The Reflector package uses a channel to signal stop events rather than a context, so use this go routine to
	// bridge the two worlds.
	stopCh := make(chan struct{})
	go func() {
		<-ctx.Done()
		slog.Info("context canceled; stopping reflectors")
		close(stopCh)
	}()

	// We run this next block in a go routing because we need to call WaitForNamedCacheSync which
	// is a blocking operation that may not complete for an extended period of time if the API
	// server is not reachable.
	go func() {
		// Create a Reflector to watch ManagedCluster objects
		clusterLister := cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				var managedClusterList v1.ManagedClusterList
				err := d.hubClient.List(ctx, &managedClusterList, &client.ListOptions{Raw: &options})
				if err != nil {
					return nil, fmt.Errorf("error listing managed clusters: %w", err)
				}
				return &managedClusterList, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				var managedClusterList v1.ManagedClusterList
				w, err := d.hubClient.Watch(ctx, &managedClusterList, &client.ListOptions{Raw: &options})
				if err != nil {
					return nil, fmt.Errorf("error watching managed clusters: %w", err)
				}
				return w, nil
			},
			DisableChunking: false,
		}

		clusterStore := async.NewReflectorStore(&v1.ManagedCluster{})
		clusterReflector := cache.NewNamedReflector(clusterReflectorName, &clusterLister, &v1.ManagedCluster{}, clusterStore, time.Duration(0))
		slog.Info("starting cluster reflector")
		go clusterReflector.Run(stopCh)

		// Start monitoring the store to process incoming events
		slog.Info("starting to receive from cluster reflector store")
		go clusterStore.Receive(ctx, d)

		// We need the clusters to be retrieved before we handle any agents since they are dependent.
		cache.WaitForNamedCacheSync(clusterReflectorName, stopCh, clusterStore.HasSynced)

		// Create a Reflector to watch Agent objects
		agentLister := cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				var agentList v1beta1.AgentList
				err := d.hubClient.List(ctx, &agentList, &client.ListOptions{Raw: &options})
				if err != nil {
					return nil, fmt.Errorf("error listing agents: %w", err)
				}
				return &agentList, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				var agentList v1beta1.AgentList
				w, err := d.hubClient.Watch(ctx, &agentList, &client.ListOptions{Raw: &options})
				if err != nil {
					return nil, fmt.Errorf("error watching agents: %w", err)
				}
				return w, nil
			},
			DisableChunking: false,
		}

		agentStore := async.NewReflectorStore(&v1beta1.Agent{})
		agentReflector := cache.NewNamedReflector(agentReflectorName, &agentLister, &v1beta1.Agent{}, agentStore, time.Duration(0))
		slog.Info("starting agent reflector")
		go agentReflector.Run(stopCh)

		// Start monitoring the store to process incoming events
		slog.Info("starting to receive from agent reflector store")
		go agentStore.Receive(ctx, d)
	}()

	return nil
}
