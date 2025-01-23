package collector

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/google/uuid"
	inventoryclient "github.com/openshift-kni/oran-hwmgr-plugin/pkg/inventory-client/generated"
	"k8s.io/client-go/transport"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

// Interface compile enforcement
var _ ResourceDataSource = (*HwMgrDataSource)(nil)

// HwMgrDataSource defines an instance of a data source collector that interacts with the ACM search-api
type HwMgrDataSource struct {
	dataSourceID  uuid.UUID
	generationID  int
	name          string
	cloudID       uuid.UUID
	globalCloudID uuid.UUID
	client        *inventoryclient.ClientWithResponses
}

// Defines the UUID namespace values used to generated name based UUID values for inventory objects.
// These values are selected arbitrarily.
// TODO: move to somewhere more generic
const (
	ResourcePoolUUIDNamespace = "daee6434-767a-485d-816b-bc04c21f1acf"
	ResourceUUIDNamespace     = "8ef67482-1215-470d-9a43-eb02af4a7c05"
	ResourceTypeUUIDNamespace = "255c4b4c-84a8-4c95-95ba-217e1688a03d"
)

const (
	vendorExtension           = "vendor"
	modelExtension            = "model"
	partNumberExtension       = "partNumber"
	serialNumberExtension     = "serialNumber"
	memoryExtension           = "memory"
	processorsExtension       = "processors"
	adminStateExtension       = "adminState"
	operationalStateExtension = "operationalState"
	usageStateExtension       = "usageState"
)

// NewHwMgrDataSource creates a new instance of an ACM data source collector whose purpose is to collect data from the
// ACM search API to be included in the resource, resource pool, and resource type tables.
func NewHwMgrDataSource(name string, cloudID, globalCloudID uuid.UUID) (DataSource, error) {
	inventoryClient, err := setupInventoryClient(name)
	if err != nil {
		return nil, err
	}

	return &HwMgrDataSource{
		generationID:  0,
		name:          name,
		cloudID:       cloudID,
		globalCloudID: globalCloudID,
		client:        inventoryClient,
	}, nil
}

func setupInventoryClient(name string) (*inventoryclient.ClientWithResponses, error) {
	slog.Info("Creating inventory API client", "name", name)

	url := fmt.Sprintf("https://oran-hwmgr-plugin-controller-manager.%s.svc.cluster.local:6443", os.Getenv(utils.HwMgrPluginNameSpace))

	// Set up transport
	tr, err := utils.GetDefaultBackendTransport()
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP transport: %w", err)
	}

	hc := http.Client{Transport: tr}

	// Create a request editor that uses a cached token source capable of re-reading from file to pickup changes
	// as our token is renewed.
	editor := clients.AuthorizationEditor{
		Source: transport.NewCachedFileTokenSource(utils.DefaultBackendTokenFile),
	}

	c, err := inventoryclient.NewClientWithResponses(url, inventoryclient.WithHTTPClient(&hc), inventoryclient.WithRequestEditorFn(editor.Editor))
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	return c, nil
}

// Name returns the name of this data source
func (d *HwMgrDataSource) Name() string {
	return fmt.Sprintf("HwMgr(plugin=%s)", d.name)
}

// GetID returns the data source ID for this data source
func (d *HwMgrDataSource) GetID() uuid.UUID {
	return d.dataSourceID
}

// Init initializes the data source with its configuration data; including the ID, the GenerationID, and its extension
// values if provided.
func (d *HwMgrDataSource) Init(uuid uuid.UUID, generationID int, asyncEventChannel chan<- *async.AsyncChangeEvent) {
	d.dataSourceID = uuid
	d.generationID = generationID
}

// SetGenerationID sets the current generation id for this data source.  This value is expected to
// be restored from persistent storage at initialization time.
func (d *HwMgrDataSource) SetGenerationID(value int) {
	d.generationID = value
}

// GetGenerationID retrieves the current generation id for this data source.
func (d *HwMgrDataSource) GetGenerationID() int {
	return d.generationID
}

// IncrGenerationID increments the current generation id for this data source.
func (d *HwMgrDataSource) IncrGenerationID() int {
	d.generationID++
	return d.generationID
}

// MakeResourceType creates an instance of a ResourceType from a Resource object.
func (d *HwMgrDataSource) MakeResourceType(resource *models.Resource) (*models.ResourceType, error) {
	vendor := resource.Extensions[vendorExtension].(string)
	model := resource.Extensions[modelExtension].(string)
	name := fmt.Sprintf("%s/%s", vendor, model)
	resourceTypeID := utils.MakeUUIDFromName(ResourceTypeUUIDNamespace, d.cloudID, name)

	// TODO: finish filling this in with data
	result := models.ResourceType{
		ResourceTypeID: resourceTypeID,
		Name:           name,
		Description:    name,
		Vendor:         vendor,
		Model:          model,
		Version:        "",
		ResourceKind:   models.ResourceKind(service.ResourceKindPhysical),
		ResourceClass:  models.ResourceClass(service.ResourceClassCompute),
		Extensions:     nil,
		DataSourceID:   d.dataSourceID,
		GenerationID:   d.generationID,
	}

	return &result, nil
}

// GetResources returns the list of resources available for this data source.  The resources to be
// retrieved can be scoped to a set of pools (currently not used by this data source)
func (d *HwMgrDataSource) GetResources(ctx context.Context, _ []models.ResourcePool) ([]models.Resource, error) {
	result, err := d.client.GetResourcesWithResponse(ctx, d.name)
	if err != nil {
		return nil, fmt.Errorf("failed to get resources: %w", err)
	}

	if result.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to get resources, status: %d", result.StatusCode())
	}

	if result.JSON200 == nil {
		return nil, fmt.Errorf("failed to get resources, empty response")
	}

	resources := make([]models.Resource, 0)
	for _, resource := range *result.JSON200 {
		resources = append(resources, *d.convertResource(&resource))
	}

	return resources, nil
}

// GetResourcePools returns the list of resource pools available for this data source.
func (d *HwMgrDataSource) GetResourcePools(ctx context.Context) ([]models.ResourcePool, error) {
	result, err := d.client.GetResourcePoolsWithResponse(ctx, d.name)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource pools: %w", err)
	}

	if result.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to get resource pools, status: %d", result.StatusCode())
	}

	if result.JSON200 == nil {
		return nil, fmt.Errorf("failed to get resource pools, empty response")
	}

	pools := make([]models.ResourcePool, 0)
	for _, pool := range *result.JSON200 {
		pools = append(pools, *d.convertResourcePool(&pool))
	}

	return pools, nil
}

func (d *HwMgrDataSource) convertResourcePool(pool *inventoryclient.ResourcePoolInfo) *models.ResourcePool {
	return &models.ResourcePool{
		ResourcePoolID:   utils.MakeUUIDFromName(ResourcePoolUUIDNamespace, d.cloudID, pool.ResourcePoolId),
		GlobalLocationID: d.globalCloudID, // TODO: spec wording is unclear about what this value should be.
		Name:             pool.Name,
		Description:      pool.Description,
		OCloudID:         d.cloudID,
		Location:         pool.SiteId,
		Extensions:       nil,
		DataSourceID:     d.dataSourceID,
		GenerationID:     d.generationID,
		ExternalID:       fmt.Sprintf("%s/%s", d.name, pool.Name),
	}
}

func (d *HwMgrDataSource) convertResource(resource *inventoryclient.ResourceInfo) *models.Resource {
	resourceID := utils.MakeUUIDFromName(ResourceUUIDNamespace, d.cloudID, resource.ResourceId)
	name := fmt.Sprintf("%s/%s", resource.Vendor, resource.Model)
	resourceTypeID := utils.MakeUUIDFromName(ResourceTypeUUIDNamespace, d.cloudID, name)

	result := &models.Resource{
		ResourceID:     resourceID,
		Description:    resource.Description,
		ResourceTypeID: resourceTypeID,
		GlobalAssetID:  resource.GlobalAssetId,
		ResourcePoolID: utils.MakeUUIDFromName(ResourcePoolUUIDNamespace, d.cloudID, resource.ResourcePoolId),
		Extensions: map[string]interface{}{
			modelExtension:            resource.Model,
			vendorExtension:           resource.Vendor,
			partNumberExtension:       resource.PartNumber,
			serialNumberExtension:     resource.SerialNumber,
			memoryExtension:           fmt.Sprintf("%d MiB", resource.Memory),
			processorsExtension:       resource.Processors,
			adminStateExtension:       string(resource.AdminState),
			operationalStateExtension: string(resource.OperationalState),
			usageStateExtension:       string(resource.UsageState),
		},
		Groups:       resource.Groups,
		Tags:         resource.Tags,
		DataSourceID: d.dataSourceID,
		GenerationID: d.generationID,
		ExternalID:   fmt.Sprintf("%s/%s", d.name, resource.Name),
	}

	if resource.PowerState != nil {
		result.Extensions["powerState"] = string(*resource.PowerState)
	}

	return result
}
