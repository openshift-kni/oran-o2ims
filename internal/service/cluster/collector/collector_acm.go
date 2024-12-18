package collector

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/itchyny/gojq"
	v1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/graphql"
	"github.com/openshift-kni/oran-o2ims/internal/jq"
	"github.com/openshift-kni/oran-o2ims/internal/service"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
)

const vendorLabelName = "vendor"
const openshiftVersionLabelName = "openshiftVersion-major-minor"

// Interface compile enforcement
var _ DataSource = (*ACMDataSource)(nil)

// ACMDataSource defines an instance of a data source collector that interacts with the ACM search-api
type ACMDataSource struct {
	dataSourceID        uuid.UUID
	cloudID             uuid.UUID
	extensions          []string
	jqTool              *jq.Tool
	hubClient           client.Client
	resourceFetcher     *service.ResourceFetcher
	resourcePoolFetcher *service.ResourcePoolFetcher
	generationID        int
}

// Defines the UUID namespace values used to generated name based UUID values for cluster objects.
// These values are selected arbitrarily.
// TODO: move to somewhere more generic
const (
	NodeClusterUUIDNamespace         = "1993e743-ad11-447a-ae00-816e22a37f63"
	NodeClusterTypeUUIDNamespace     = "b9b24e78-5764-461b-8a0b-55bbe189d3d9"
	ClusterResourceUUIDNamespace     = "75d74e65-f51f-4195-94ea-e4495b940a8f"
	ClusterResourceTypeUUIDNamespace = "be370c55-e9fc-4094-81c3-83d6be4a6a14"
)

// graphqlQuery defines the query expression supported by the search api
const graphqlQuery = `query ($input: [SearchInput]) {
				searchResult: search(input: $input) {
						items,    
					}
			}`

// NewACMDataSource creates a new instance of an ACM data source collector whose purpose is to collect data from the
// ACM search API to be included in the node cluster, node cluster type, cluster resource and cluster resource type
// tables.
func NewACMDataSource(cloudID uuid.UUID, backendURL string, extensions []string) (DataSource, error) {
	// TODO: this needs to be refactored so that the token is re-read if a 401 error is returned so that we can
	//   refresh it automatically.
	backendTokenData, err := os.ReadFile(utils.DefaultBackendTokenFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read backend token file: %w", err)
	}
	backendToken := strings.TrimSpace(string(backendTokenData))

	searchAPI, err := utils.GenerateSearchApiUrl(backendURL)
	if err != nil {
		return nil, fmt.Errorf("failed to generate search API URL: %w", err)
	}

	// Log handling for fetchers
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug, // TODO: set log level from server args
	}))

	resourceFetcher, err := service.NewResourceFetcher().
		SetLogger(logger).
		SetGraphqlQuery(graphqlQuery).
		SetGraphqlVars(getNodeGraphqlVars()).
		SetBackendURL(searchAPI).
		SetBackendToken(backendToken).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build ACM resource fetcher: %w", err)
	}

	resourcePoolFetcher, err := service.NewResourcePoolFetcher().
		SetLogger(logger).
		SetCloudID(cloudID.String()).
		SetGraphqlQuery(graphqlQuery).
		SetGraphqlVars(getClusterGraphqlVars()).
		SetBackendURL(searchAPI).
		SetBackendToken(backendToken).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build resource pool fetcher: %w", err)
	}

	// Create a jq compiler function for parsing labels
	compilerFunc := gojq.WithFunction("parse_labels", 0, 1, func(x any, _ []any) any {
		if labels, ok := x.(string); ok {
			return data.GetLabelsMap(labels)
		}
		return nil
	})

	// Create the jq tool:
	jqTool, err := jq.NewTool().
		SetLogger(logger).
		SetCompilerOption(&compilerFunc).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build jq tool: %w", err)
	}

	// Check that extensions are at least syntactically valid:
	for _, extension := range extensions {
		_, err = jqTool.Compile(extension)
		if err != nil {
			return nil, fmt.Errorf("failed to compile extension %q: %w", extension, err)
		}
	}

	// Create a k8s client for the hub to be able to retrieve managed cluster info
	hubClient, err := k8s.NewClientForHub()
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client for hub: %w", err)
	}

	return &ACMDataSource{
		generationID:        0,
		cloudID:             cloudID,
		extensions:          extensions,
		jqTool:              jqTool,
		hubClient:           hubClient,
		resourceFetcher:     resourceFetcher,
		resourcePoolFetcher: resourcePoolFetcher,
	}, nil
}

// Name returns the name of this data source
func (d *ACMDataSource) Name() string {
	return "ACM"
}

// SetID sets the unique identifier for this data source
func (d *ACMDataSource) SetID(uuid uuid.UUID) {
	d.dataSourceID = uuid
}

// SetGenerationID sets the current generation id for this data source.  This value is expected to
// be restored from persistent storage at initialization time.
func (d *ACMDataSource) SetGenerationID(value int) {
	d.generationID = value
}

// GetGenerationID retrieves the current generation id for this data source.
func (d *ACMDataSource) GetGenerationID() int {
	return d.generationID
}

// IncrGenerationID increments the current generation id for this data source.
func (d *ACMDataSource) IncrGenerationID() int {
	d.generationID++
	return d.generationID
}

// makeClusterResourceTypeName builds a descriptive string to represent the resource type
func (d *ACMDataSource) makeClusterResourceTypeName(cpu, architecture string) string {
	return fmt.Sprintf("%s CPU with %s Cores", architecture, cpu)
}

// makeClusterResourceTypeID builds a UUID value for this resource type based on its name so that it has
// a consistent value each time it is created.
func (d *ACMDataSource) makeClusterResourceTypeID(cpu, architecture string) uuid.UUID {
	return utils.MakeUUIDFromName(ClusterResourceTypeUUIDNamespace, d.cloudID, d.makeClusterResourceTypeName(cpu, architecture))
}

// MakeClusterResourceType creates an instance of a ResourceType from a Resource object.
func (d *ACMDataSource) MakeClusterResourceType(resource *models.ClusterResource) (*models.ClusterResourceType, error) {
	extensions := resource.Extensions
	cpu, ok := extensions["cpu"]
	if !ok {
		return nil, fmt.Errorf("no cpu extension found")
	}
	architecture, ok := extensions["architecture"]
	if !ok {
		return nil, fmt.Errorf("no architecture extension found")
	}

	resourceTypeName := d.makeClusterResourceTypeName(cpu, architecture)
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
func (d *ACMDataSource) MakeNodeClusterType(resource *models.NodeCluster) (*models.NodeClusterType, error) {
	extensions := resource.Extensions
	// We know these extensions exist because we checked when we processed the NodeCluster
	vendor := extensions[vendorLabelName]
	version := extensions[openshiftVersionLabelName]

	resourceTypeName := d.makeNodeClusterTypeName(vendor, version)
	resourceTypeID := utils.MakeUUIDFromName(NodeClusterTypeUUIDNamespace, d.cloudID, resourceTypeName)

	result := models.NodeClusterType{
		NodeClusterTypeID: resourceTypeID,
		Name:              resourceTypeName,
		Description:       resourceTypeName,
		Extensions:        nil,
		DataSourceID:      d.dataSourceID,
		GenerationID:      d.generationID,
	}

	return &result, nil
}

// GetClusterResources returns the list of cluster resources available for this data source.  The cluster resources to
// be retrieved can be scoped to a set of node clusters (currently not used by this data source)
func (d *ACMDataSource) GetClusterResources(ctx context.Context, _ []models.NodeCluster) ([]models.ClusterResource, error) {
	items, err := d.resourceFetcher.FetchItems(ctx)
	if err != nil {
		return []models.ClusterResource{}, fmt.Errorf("failed to fetch items: %w", err)
	}

	// Transform items from a stream to a slice
	objects, err := data.Collect(ctx, items)
	if err != nil {
		return []models.ClusterResource{}, fmt.Errorf("failed to collect objects: %w", err)
	}

	// Convert them to the DB model object type
	var results []models.ClusterResource
	for _, object := range objects {
		if result, err := d.convertNodeToClusterResource(object); err != nil {
			slog.Warn("failed to convert node object to cluster resource", "object", object, "error", err)
			// Continue on conversion failures so that we don't exclude valid data just because of
			// a single bad object.
		} else {
			results = append(results, result)
		}
	}

	return results, nil
}

// GetNodeClusters returns the list of node clusters for this data source
func (d *ACMDataSource) GetNodeClusters(ctx context.Context) ([]models.NodeCluster, error) {
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

// convertNodeToClusterResource converts a Node object received from ACM to a Resource object.
func (d *ACMDataSource) convertNodeToClusterResource(from data.Object) (to models.ClusterResource, err error) {
	description, err := data.GetString(from,
		graphql.PropertyNode("description").MapProperty())
	if err != nil {
		return
	}

	name, err := data.GetString(from, "name")
	if err != nil {
		return
	}

	cpu, err := data.GetString(from, "cpu")
	if err != nil {
		return
	}

	architecture, err := data.GetString(from, "architecture")
	if err != nil {
		return
	}

	// Add the extensions:
	extensionsMap, err := data.GetExtensions(from, d.extensions, d.jqTool)
	if err != nil {
		return
	}
	if len(extensionsMap) == 0 {
		// Fallback to all labels
		var labels string
		labels, err = data.GetString(from, "label")
		if err != nil {
			return
		}
		extensionsMap = data.GetLabelsMap(labels)
	}

	// Add the cpu/architecture values
	extensionsMap["cpu"] = cpu
	extensionsMap["architecture"] = architecture

	// Convert the extensions to strings since that's how our API side models are defined.  We know the data is coming
	// from the ACM search API, and it is all represented as strings so there shouldn't be an issue doing the conversion.
	extensions := utils.ConvertMapAnyToString(extensionsMap)

	// For now continue to generate UUID values based on the string values assigned
	resourceID := utils.MakeUUIDFromName(ClusterResourceUUIDNamespace, d.cloudID, name)
	resourceTypeID := d.makeClusterResourceTypeID(cpu, architecture)

	to = models.ClusterResource{
		ClusterResourceID:     resourceID,
		ClusterResourceTypeID: resourceTypeID,
		Name:                  name,
		Description:           description,
		Extensions:            extensions,
		ResourceID:            uuid.UUID{}, // TODO: need to link to Resource
		DataSourceID:          d.dataSourceID,
		GenerationID:          d.generationID,
	}

	return
}

// makeNodeClusterTypeName builds a descriptive string to represent the node cluster type
func (d *ACMDataSource) makeNodeClusterTypeName(vendor, version string) string {
	return fmt.Sprintf("%s-%s", vendor, version)
}

// makeClusterResourceTypeID builds a UUID value for this resource type based on its name so that it has
// a consistent value each time it is created.
func (d *ACMDataSource) makeNodeClusterTypeID(vendor, version string) uuid.UUID {
	return utils.MakeUUIDFromName(NodeClusterTypeUUIDNamespace, d.cloudID, d.makeNodeClusterTypeName(vendor, version))
}

// convertManagedClusterToNodeCluster converts a ManagedCluster to a ManagedCluster object
func (d *ACMDataSource) convertManagedClusterToNodeCluster(cluster v1.ManagedCluster) (models.NodeCluster, error) {
	vendor, found := cluster.Labels["vendor"]
	if !found {
		return models.NodeCluster{}, fmt.Errorf("no vendor label found on cluster %s", cluster.Name)
	}

	var version string
	if vendor == "OpenShift" {
		version, found = cluster.Labels[openshiftVersionLabelName]
		if !found {
			return models.NodeCluster{}, fmt.Errorf("no version label found on cluster %s", cluster.Name)
		}
	}

	// For now continue to generate UUID values based on the string values assigned
	resourceID := utils.MakeUUIDFromName(NodeClusterUUIDNamespace, d.cloudID, cluster.Name)
	resourceTypeID := d.makeNodeClusterTypeID(vendor, version)

	// Until we know which extensions we want to expose we'll just publish all labels
	extensions := maps.Clone(cluster.Labels)

	to := models.NodeCluster{
		NodeClusterID:      resourceID,
		NodeClusterTypeID:  resourceTypeID,
		Name:               cluster.Name,
		Description:        cluster.Name,
		Extensions:         extensions,
		ArtifactResourceID: uuid.UUID{}, // TODO: need to get this from template?
		DataSourceID:       d.dataSourceID,
		GenerationID:       d.generationID,
	}

	return to, nil
}
