/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/google/uuid"

	rtclient "sigs.k8s.io/controller-runtime/pkg/client"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	inventoryclient "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/client/inventory"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

// Interface compile enforcement
var _ ResourceDataSource = (*HwPluginDataSource)(nil)

// HwPluginDataSource defines an instance of a data source collector that interacts with the ACM search-api
type HwPluginDataSource struct {
	dataSourceID  uuid.UUID
	generationID  atomic.Int32
	hwplugin      *hwmgmtv1alpha1.HardwarePlugin
	cloudID       uuid.UUID
	globalCloudID uuid.UUID
	client        *inventoryclient.InventoryClient
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

// NewHwPluginDataSource creates a new instance of an ACM data source collector whose purpose is to collect data from the
// ACM search API to be included in the resource, resource pool, and resource type tables.
func NewHwPluginDataSource(ctx context.Context, hubClient rtclient.Client, hwplugin *hwmgmtv1alpha1.HardwarePlugin, cloudID, globalCloudID uuid.UUID) (DataSource, error) {
	slog.Info("Creating inventory API client", "name", hwplugin.Name)
	inventoryClient, err := inventoryclient.NewInventoryClient(ctx, hubClient, hwplugin)
	if err != nil {
		return nil, fmt.Errorf("failed to get inventory client for HardwarePlugin '%s': %w", hwplugin.Name, err)
	}

	return &HwPluginDataSource{
		hwplugin:      hwplugin,
		cloudID:       cloudID,
		globalCloudID: globalCloudID,
		client:        inventoryClient,
	}, nil
}

// Name returns the name of this data source
func (d *HwPluginDataSource) Name() string {
	return fmt.Sprintf("HardwarePlugin(name=%s)", d.hwplugin.Name)
}

// GetID returns the data source ID for this data source
func (d *HwPluginDataSource) GetID() uuid.UUID {
	return d.dataSourceID
}

// Init initializes the data source with its configuration data; including the ID, the GenerationID, and its extension
// values if provided.
func (d *HwPluginDataSource) Init(uuid uuid.UUID, generationID int, asyncEventChannel chan<- *async.AsyncChangeEvent) {
	d.dataSourceID = uuid
	d.generationID.Store(int32(generationID)) //nolint:gosec // generationID is a small counter, overflow impossible
}

// SetGenerationID sets the current generation id for this data source.  This value is expected to
// be restored from persistent storage at initialization time.
func (d *HwPluginDataSource) SetGenerationID(value int) {
	d.generationID.Store(int32(value)) //nolint:gosec // generationID is a small counter, overflow impossible
}

// GetGenerationID retrieves the current generation id for this data source.
func (d *HwPluginDataSource) GetGenerationID() int {
	return int(d.generationID.Load())
}

// IncrGenerationID increments the current generation id for this data source.
func (d *HwPluginDataSource) IncrGenerationID() int {
	return int(d.generationID.Add(1))
}

// MakeResourceType creates an instance of a ResourceType from a Resource object.
func (d *HwPluginDataSource) MakeResourceType(resource *models.Resource) (*models.ResourceType, error) {
	vendor := resource.Extensions[vendorExtension].(string)
	model := resource.Extensions[modelExtension].(string)
	name := fmt.Sprintf("%s/%s", vendor, model)
	resourceTypeID := ctlrutils.MakeUUIDFromNames(ResourceTypeUUIDNamespace, d.cloudID, d.hwplugin.Name, name)

	// TODO: finish filling this in with data
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

// GetResources returns the list of resources available for this data source.
func (d *HwPluginDataSource) GetResources(ctx context.Context) ([]models.Resource, error) {
	result, err := d.client.GetResourcesWithResponse(ctx)
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
		converted := d.convertResource(&resource)
		resources = append(resources, *converted)
	}

	return resources, nil
}

func (d *HwPluginDataSource) convertResource(resource *inventoryclient.ResourceInfo) *models.Resource {
	resourceID := resource.ResourceId

	// ResourcePoolId is the Kubernetes UID of the ResourcePool CR
	resourcePoolID := resource.ResourcePoolId

	name := fmt.Sprintf("%s/%s", resource.Vendor, resource.Model)
	resourceTypeID := ctlrutils.MakeUUIDFromNames(ResourceTypeUUIDNamespace, d.cloudID, d.hwplugin.Name, name)

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
		ExternalID:   fmt.Sprintf("%s/%s", d.hwplugin.Name, resource.ResourceId),
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
