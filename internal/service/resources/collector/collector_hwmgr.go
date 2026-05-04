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

	"github.com/google/uuid"

	rtclient "sigs.k8s.io/controller-runtime/pkg/client"

	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	hwmgrcontroller "github.com/openshift-kni/oran-o2ims/internal/hardwaremanager/controller"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

// Interface compile enforcement
var _ ResourceDataSource = (*HardwareDataSource)(nil)

// HardwareDataSource collects hardware inventory data (BMHs, AllocatedNodes)
// directly via the K8s client.
type HardwareDataSource struct {
	dataSourceID  uuid.UUID
	generationID  atomic.Int32
	cloudID       uuid.UUID
	globalCloudID uuid.UUID
	hubClient     rtclient.Client
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

// NewHardwareDataSource creates a hardware inventory data source that collects
// BMH resource data directly via the K8s client.
func NewHardwareDataSource(hubClient rtclient.Client, cloudID, globalCloudID uuid.UUID) DataSource {
	return &HardwareDataSource{
		cloudID:       cloudID,
		globalCloudID: globalCloudID,
		hubClient:     hubClient,
	}
}

// Name returns the name of this data source
func (d *HardwareDataSource) Name() string {
	return "HardwareDataSource"
}

// GetID returns the data source ID for this data source
func (d *HardwareDataSource) GetID() uuid.UUID {
	return d.dataSourceID
}

// Init initializes the data source with its configuration data; including the ID, the GenerationID, and its extension
// values if provided.
func (d *HardwareDataSource) Init(uuid uuid.UUID, generationID int, asyncEventChannel chan<- *async.AsyncChangeEvent) {
	d.dataSourceID = uuid
	d.generationID.Store(int32(generationID)) //nolint:gosec // generationID is a small counter, overflow impossible
}

// SetGenerationID sets the current generation id for this data source.  This value is expected to
// be restored from persistent storage at initialization time.
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

// MakeResourceType creates an instance of a ResourceType from a Resource object.
func (d *HardwareDataSource) MakeResourceType(resource *models.Resource) (*models.ResourceType, error) {
	vendor := resource.Extensions[vendorExtension].(string)
	model := resource.Extensions[modelExtension].(string)
	name := fmt.Sprintf("%s/%s", vendor, model)
	resourceTypeID := ctlrutils.MakeUUIDFromNames(ResourceTypeUUIDNamespace, d.cloudID, name)

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
func (d *HardwareDataSource) GetResources(ctx context.Context) ([]models.Resource, error) {
	resourceInfos, err := hwmgrcontroller.GetResources(ctx, slog.Default(), d.hubClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get resources: %w", err)
	}

	resources := make([]models.Resource, 0, len(resourceInfos))
	for i := range resourceInfos {
		converted := d.convertResource(&resourceInfos[i])
		resources = append(resources, *converted)
	}

	return resources, nil
}

func (d *HardwareDataSource) convertResource(resource *hwmgrcontroller.ResourceInfo) *models.Resource {
	resourceID := resource.ResourceId

	// ResourcePoolId is the Kubernetes UID of the ResourcePool CR
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
