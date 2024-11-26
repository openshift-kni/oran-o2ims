package collector

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/openshift/assisted-service/api/v1beta1"
	v1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
)

// Interface compile enforcement
var _ DataSource = (*K8SDataSource)(nil)

// K8SDataSource defines an instance of a data source collector that interacts with the Kubernetes API
type K8SDataSource struct {
	dataSourceID uuid.UUID
	cloudID      uuid.UUID
	extensions   []string
	hubClient    client.Client
	generationID int
}

const (
	NodeClusterTypeUUIDNamespace     = "b9b24e78-5764-461b-8a0b-55bbe189d3d9"
	ClusterResourceUUIDNamespace     = "75d74e65-f51f-4195-94ea-e4495b940a8f"
	ClusterResourceTypeUUIDNamespace = "be370c55-e9fc-4094-81c3-83d6be4a6a14"
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
		extensions:   extensions,
		hubClient:    hubClient,
	}, nil
}

// Name returns the name of this data source
func (d *K8SDataSource) Name() string {
	return "K8S"
}

// SetID sets the unique identifier for this data source
func (d *K8SDataSource) SetID(uuid uuid.UUID) {
	d.dataSourceID = uuid
}

// SetGenerationID sets the current generation id for this data source.  This value is expected to
// be restored from persistent storage at initialization time.
func (d *K8SDataSource) SetGenerationID(value int) {
	d.generationID = value
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

	// We expect that the standard will eventually evolve to contain more attributes to align more
	// closely with how ResourceType was defined, but for now we'll add some additional info as
	// extensions so that they are known to clients.  These will be used by the alarm code to build
	// the dictionary.
	typeExtensions := map[string]interface{}{
		utils.ClusterVendorExtension:  vendor,
		utils.ClusterVersionExtension: version,
		utils.ClusterModelExtension:   clusterType,
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

// GetClusterResources returns the list of cluster resources available for this data source.  The cluster resources to
// be retrieved can be scoped to a set of node clusters (currently not used by this data source)
func (d *K8SDataSource) GetClusterResources(ctx context.Context, _ []models.NodeCluster) ([]models.ClusterResource, error) {
	var agents v1beta1.AgentList
	err := d.hubClient.List(ctx, &agents)
	if err != nil {
		return []models.ClusterResource{}, fmt.Errorf("failed to list install agents: %w", err)
	}

	var results []models.ClusterResource
	for _, agent := range agents.Items {
		results = append(results, d.convertAgentToClusterResource(&agent))
	}

	return results, nil
}

// GetNodeClusters returns the list of node clusters for this data source
func (d *K8SDataSource) GetNodeClusters(ctx context.Context) ([]models.NodeCluster, error) {
	var clusters v1.ManagedClusterList
	err := d.hubClient.List(ctx, &clusters)
	if err != nil {
		return []models.NodeCluster{}, fmt.Errorf("failed to list clusters: %w", err)
	}

	var results []models.NodeCluster
	for _, cluster := range clusters.Items {
		if result, err := d.convertManagedClusterToNodeCluster(cluster); err != nil {
			slog.Warn("failed to convert managed cluster to node cluster", "cluster", cluster, "error", err)
			// Continue on conversion failures so that we don't exclude valid data just because of
			// a single bad object.
		} else {
			results = append(results, result)
		}
	}

	return results, nil
}

func (d *K8SDataSource) convertAgentToClusterResource(agent *v1beta1.Agent) models.ClusterResource {
	// Build a unique UUID value using the namespace and name.  Choosing not to tie ourselves to the agent UUID since
	// we don't know if/when it can change and how we want to behave if the node gets deleted and re-installed.
	name := fmt.Sprintf("%s/%s", agent.Namespace, agent.Name)
	resourceID := utils.MakeUUIDFromName(ClusterResourceUUIDNamespace, d.cloudID, name)

	architecture := agent.Status.Inventory.Cpu.Architecture
	cores := agent.Status.Inventory.Cpu.Count
	resourceTypeID := d.makeClusterResourceTypeID(architecture, strconv.FormatInt(cores, 10))

	extensions := map[string]interface{}{
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
func (d *K8SDataSource) convertManagedClusterToNodeCluster(cluster v1.ManagedCluster) (models.NodeCluster, error) {
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
