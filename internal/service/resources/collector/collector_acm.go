package collector

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/itchyny/gojq"
	"github.com/thoas/go-funk"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/graphql"
	"github.com/openshift-kni/oran-o2ims/internal/jq"
	"github.com/openshift-kni/oran-o2ims/internal/service"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

// Interface compile enforcement
var _ ResourceDataSource = (*ACMDataSource)(nil)

// ACMDataSource defines an instance of a data source collector that interacts with the ACM search-api
type ACMDataSource struct {
	dataSourceID        uuid.UUID
	cloudID             uuid.UUID
	globalCloudID       uuid.UUID
	extensions          []string
	jqTool              *jq.Tool
	hubClient           client.WithWatch
	resourceFetcher     *service.ResourceFetcher
	resourcePoolFetcher *service.ResourcePoolFetcher
	generationID        int
}

// Defines the UUID namespace values used to generated name based UUID values for inventory objects.
// These values are selected arbitrarily.
// TODO: move to somewhere more generic
const (
	ResourcePoolUUIDNamespace = "1993e743-ad11-447a-ae00-816e22a37f63"
	ResourceUUIDNamespace     = "e6501bad-6f4e-46b1-b4eb-f6952bf532e1"
	ResourceTypeUUIDNamespace = "2e300cf4-3c4c-4c9a-a34c-1985bf4b7c41"
)

// graphqlQuery defines the query expression supported by the search api
const graphqlQuery = `query ($input: [SearchInput]) {
				searchResult: search(input: $input) {
						items,    
					}
			}`

// NewACMDataSource creates a new instance of an ACM data source collector whose purpose is to collect data from the
// ACM search API to be included in the resource, resource pool, and resource type tables.
func NewACMDataSource(cloudID, globalCloudID uuid.UUID, backendURL string, extensions []string) (DataSource, error) {
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
		globalCloudID:       globalCloudID,
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

// GetID returns the data source ID for this data source
func (d *ACMDataSource) GetID() uuid.UUID {
	return d.dataSourceID
}

// Init initializes the data source with its configuration data; including the ID, the GenerationID, and its extension
// values if provided.
func (d *ACMDataSource) Init(uuid uuid.UUID, generationID int, asyncEventChannel chan<- *async.AsyncChangeEvent) {
	d.dataSourceID = uuid
	d.generationID = generationID
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

// makeResourceTypeName builds a descriptive string to represent the resource type
func (d *ACMDataSource) makeResourceTypeName(cpu, architecture string) string {
	return fmt.Sprintf("%s CPU with %s Cores", architecture, cpu)
}

// makeResourceTypeID builds a UUID value for this resource type based on its name so that it has
// a consistent value each time it is created.
func (d *ACMDataSource) makeResourceTypeID(cpu, architecture string) uuid.UUID {
	return utils.MakeUUIDFromName(ResourceTypeUUIDNamespace, d.cloudID, d.makeResourceTypeName(cpu, architecture))
}

// MakeResourceType creates an instance of a ResourceType from a Resource object.
func (d *ACMDataSource) MakeResourceType(resource *models.Resource) (*models.ResourceType, error) {
	extensions := resource.Extensions
	cpu, ok := extensions["cpu"]
	if !ok {
		return nil, fmt.Errorf("no cpu extension found")
	}
	architecture, ok := extensions["architecture"]
	if !ok {
		return nil, fmt.Errorf("no architecture extension found")
	}

	resourceTypeName := d.makeResourceTypeName(cpu, architecture)
	resourceTypeID := utils.MakeUUIDFromName(ResourceTypeUUIDNamespace, d.cloudID, d.makeResourceTypeName(cpu, architecture))

	// TODO: finish filling this in with data
	result := models.ResourceType{
		ResourceTypeID: resourceTypeID,
		Name:           resourceTypeName,
		Description:    resourceTypeName,
		Vendor:         "",
		Model:          "",
		Version:        "",
		ResourceKind:   models.ResourceKind(service.ResourceKindLogical),
		ResourceClass:  models.ResourceClass(service.ResourceClassCompute),
		Extensions:     nil,
		DataSourceID:   d.dataSourceID,
		GenerationID:   d.generationID,
	}

	return &result, nil
}

// GetResources returns the list of resources available for this data source.  The resources to be
// retrieved can be scoped to a set of pools (currently not used by this data source)
func (d *ACMDataSource) GetResources(ctx context.Context, _ []models.ResourcePool) ([]models.Resource, error) {
	items, err := d.resourceFetcher.FetchItems(ctx)
	if err != nil {
		return []models.Resource{}, fmt.Errorf("failed to fetch items: %w", err)
	}

	// Transform items from a stream to a slice
	objects, err := data.Collect(ctx, items)
	if err != nil {
		return []models.Resource{}, fmt.Errorf("failed to collect objects: %w", err)
	}

	// Convert them to the DB model object type
	var results []models.Resource
	for _, object := range objects {
		if result, err := d.convertNodeToResource(object); err != nil {
			slog.Warn("failed to convert node object to resource", "object", object, "error", err)
			// Continue on conversion failures so that we don't exclude valid data just because of
			// a single bad object.
		} else {
			results = append(results, result)
		}
	}

	return results, nil
}

// GetResourcePools returns the list of resource pools available for this data source.
func (d *ACMDataSource) GetResourcePools(ctx context.Context) ([]models.ResourcePool, error) {
	items, err := d.resourcePoolFetcher.FetchItems(ctx)
	if err != nil {
		return []models.ResourcePool{}, fmt.Errorf("failed to fetch items: %w", err)
	}

	// Transform items from a stream to a slice
	objects, err := data.Collect(ctx, items)
	if err != nil {
		return []models.ResourcePool{}, fmt.Errorf("failed to collect objects: %w", err)
	}

	// Convert them to the DB model object type
	var results []models.ResourcePool
	for _, object := range objects {
		if result, err := d.convertClusterToResourcePool(object); err != nil {
			slog.Warn("failed to convert cluster object to resource", "object", object, "error", err)
			// Continue on conversion failures so that we don't exclude valid data just because of
			// a single bad object.
		} else {
			results = append(results, result)
		}
	}

	return results, nil
}

// convertNodeToResource converts a Node object received from ACM to a Resource object.
func (d *ACMDataSource) convertNodeToResource(from data.Object) (to models.Resource, err error) {
	description, err := data.GetString(from,
		graphql.PropertyNode("description").MapProperty())
	if err != nil {
		return
	}

	resourcePoolIdName, err := data.GetString(from,
		graphql.PropertyNode("resourcePoolId").MapProperty())
	if err != nil {
		return
	}

	globalAssetId, err := data.GetString(from,
		graphql.PropertyNode("globalAssetId").MapProperty())
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

	labels, err := data.GetString(from, "label")
	if err != nil {
		return
	}
	labelsMap := data.GetLabelsMap(labels)

	// Add the extensions:
	extensionsMap, err := data.GetExtensions(from, d.extensions, d.jqTool)
	if err != nil {
		return
	}
	if len(extensionsMap) == 0 {
		// Fallback to all labels
		extensionsMap = labelsMap
	}

	// Add the cpu/architecture values
	extensionsMap["cpu"] = cpu
	extensionsMap["architecture"] = architecture

	// Convert the extensions to strings since that's how our API side models are defined.  We know the data is coming
	// from the ACM search API, and it is all represented as strings so there shouldn't be an issue doing the conversion.
	extensions := utils.ConvertMapAnyToString(extensionsMap)

	// For now continue to generate UUID values based on the string values assigned
	resourceID := utils.MakeUUIDFromName(ResourceUUIDNamespace, d.cloudID, name)
	resourcePoolID := utils.MakeUUIDFromName(ResourcePoolUUIDNamespace, d.cloudID, resourcePoolIdName)

	resourceTypeID := d.makeResourceTypeID(cpu, architecture)
	to = models.Resource{
		ResourceID:     resourceID,
		Description:    description,
		ResourceTypeID: resourceTypeID,
		GlobalAssetID:  &globalAssetId,
		ResourcePoolID: resourcePoolID,
		Extensions:     extensions,
		Groups:         nil,
		Tags:           nil,
		DataSourceID:   d.dataSourceID,
		GenerationID:   d.generationID,
		ExternalID:     globalAssetId,
	}

	return
}

// convertClusterToResourcePool converts a Cluster object received from ACM to a ResourcePool object.
func (d *ACMDataSource) convertClusterToResourcePool(from data.Object) (to models.ResourcePool, err error) {
	resourcePoolIdName, err := data.GetString(from,
		graphql.PropertyCluster("resourcePoolId").MapProperty())
	if err != nil {
		return
	}

	name, err := data.GetString(from,
		graphql.PropertyCluster("name").MapProperty())
	if err != nil {
		return
	}

	labels, err := data.GetString(from, "label")
	if err != nil {
		return
	}
	labelsMap := data.GetLabelsMap(labels)
	labelsKeys := funk.Keys(labelsMap)

	// Set 'location' according to the 'region' label
	regionKey := funk.Find(labelsKeys, func(key string) bool {
		return strings.Contains(key, "region")
	})
	var location string
	if regionKey != nil {
		location = labelsMap[regionKey.(string)].(string)
	}

	// Set 'description' according to the 'clusterID' label
	clusterIDKey := funk.Find(labelsKeys, func(key string) bool {
		return strings.Contains(key, "clusterID")
	})
	var description string
	if clusterIDKey != nil {
		description = labelsMap[clusterIDKey.(string)].(string)
	}

	// Add the extensions:
	extensionsMap, err := data.GetExtensions(from, d.extensions, d.jqTool)
	if err != nil {
		return
	}
	if len(extensionsMap) == 0 {
		// Fallback to all labels
		extensionsMap = labelsMap
	}

	// Convert the extensions to strings since that's how our API side models are defined.  We know the data is coming
	// from the ACM search API, and it is all represented as strings so there shouldn't be an issue doing the conversion.
	extensions := utils.ConvertMapAnyToString(extensionsMap)

	// For now continue to generate UUID values based on the string values assigned
	resourcePoolID := utils.MakeUUIDFromName(ResourcePoolUUIDNamespace, d.cloudID, resourcePoolIdName)

	to = models.ResourcePool{
		ResourcePoolID:   resourcePoolID,
		GlobalLocationID: d.globalCloudID,
		Name:             name,
		Description:      description,
		OCloudID:         d.cloudID,
		Location:         &location,
		Extensions:       &extensions,
		DataSourceID:     d.dataSourceID,
		GenerationID:     d.generationID,
		ExternalID:       resourcePoolIdName,
	}

	return
}
