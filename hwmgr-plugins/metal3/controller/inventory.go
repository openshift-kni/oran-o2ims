/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"regexp"

	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/server/inventory"
)

const (
	LabelPrefixResources = "resources.oran.openshift.io/"
	LabelResourcePoolID  = LabelPrefixResources + "resourcePoolId"
	LabelSiteID          = LabelPrefixResources + "siteId"

	LabelPrefixResourceSelector = "resourceselector.oran.openshift.io/"

	LabelPrefixInterfaces = "interfacelabel.oran.openshift.io/"
)

// The following regex pattern is used to find interface labels
var REPatternInterfaceLabel = regexp.MustCompile(`^` + LabelPrefixInterfaces + `(.*)`)

// The following regex pattern is used to check resourceselector label pattern
var REPatternResourceSelectorLabel = regexp.MustCompile(`^` + LabelPrefixResourceSelector)

var emptyString = ""

func getResourceInfoAdminState() inventory.ResourceInfoAdminState {
	return inventory.ResourceInfoAdminStateUNKNOWN
}

func getResourceInfoDescription() string {
	return emptyString
}

func getResourceInfoGlobalAssetId() *string {
	return &emptyString
}

func getResourceInfoGroups() *[]string {
	return nil
}

func getResourceInfoLabels(bmh *metal3v1alpha1.BareMetalHost) *map[string]string { // nolint: gocritic
	if bmh.Labels != nil {
		labels := make(map[string]string)
		for label, value := range bmh.Labels {
			labels[label] = value
		}
		return &labels
	}

	return nil
}

func getResourceInfoMemory(bmh *metal3v1alpha1.BareMetalHost) int {
	if bmh.Status.HardwareDetails != nil {
		return bmh.Status.HardwareDetails.RAMMebibytes
	}
	return 0
}

func getResourceInfoModel(bmh *metal3v1alpha1.BareMetalHost) string {
	if bmh.Status.HardwareDetails != nil {
		return bmh.Status.HardwareDetails.SystemVendor.ProductName
	}
	return emptyString
}

func getResourceInfoName(bmh *metal3v1alpha1.BareMetalHost) string {
	return bmh.Name
}

func getResourceInfoOperationalState() inventory.ResourceInfoOperationalState {
	return inventory.ResourceInfoOperationalStateUNKNOWN
}

func getResourceInfoPartNumber() string {
	return emptyString
}

func getResourceInfoPowerState(bmh *metal3v1alpha1.BareMetalHost) *inventory.ResourceInfoPowerState {
	state := inventory.OFF
	if bmh.Status.PoweredOn {
		state = inventory.ON
	}

	return &state
}

func getProcessorInfoArchitecture(bmh *metal3v1alpha1.BareMetalHost) *string {
	if bmh.Status.HardwareDetails != nil {
		return &bmh.Status.HardwareDetails.CPU.Arch
	}
	return &emptyString
}

func getProcessorInfoCores(bmh *metal3v1alpha1.BareMetalHost) *int {
	if bmh.Status.HardwareDetails != nil {
		return &bmh.Status.HardwareDetails.CPU.Count
	}

	return nil
}

func getProcessorInfoManufacturer() *string {
	return &emptyString
}

func getProcessorInfoModel(bmh *metal3v1alpha1.BareMetalHost) *string {
	if bmh.Status.HardwareDetails != nil {
		return &bmh.Status.HardwareDetails.CPU.Model
	}
	return &emptyString
}

func getResourceInfoProcessors(bmh *metal3v1alpha1.BareMetalHost) []inventory.ProcessorInfo {
	processors := []inventory.ProcessorInfo{}

	if bmh.Status.HardwareDetails != nil {
		processors = append(processors, inventory.ProcessorInfo{
			Architecture: getProcessorInfoArchitecture(bmh),
			Cores:        getProcessorInfoCores(bmh),
			Manufacturer: getProcessorInfoManufacturer(),
			Model:        getProcessorInfoModel(bmh),
		})
	}
	return processors
}

func getResourceInfoResourceId() string {
	return emptyString
}

func getResourceInfoResourcePoolId(bmh *metal3v1alpha1.BareMetalHost) string {
	return bmh.Labels[LabelResourcePoolID]
}

func getResourceInfoResourceProfileId(bmh *metal3v1alpha1.BareMetalHost) string {
	return bmh.Status.HardwareProfile
}

func getResourceInfoSerialNumber(bmh *metal3v1alpha1.BareMetalHost) string {
	if bmh.Status.HardwareDetails != nil {
		return bmh.Status.HardwareDetails.SystemVendor.SerialNumber
	}
	return emptyString
}

func getResourceInfoTags() *[]string {
	return nil
}

func getResourceInfoUsageState() inventory.ResourceInfoUsageState {
	return inventory.UNKNOWN
}

func getResourceInfoVendor(bmh *metal3v1alpha1.BareMetalHost) string {
	if bmh.Status.HardwareDetails != nil {
		return bmh.Status.HardwareDetails.SystemVendor.Manufacturer
	}
	return emptyString
}

func includeInInventory(bmh *metal3v1alpha1.BareMetalHost) bool {
	if bmh.Labels == nil || bmh.Labels[LabelResourcePoolID] == "" || bmh.Labels[LabelSiteID] == "" {
		// Ignore BMH CRs without the required labels
		return false
	}

	switch bmh.Status.Provisioning.State {
	case metal3v1alpha1.StateAvailable,
		metal3v1alpha1.StateProvisioning,
		metal3v1alpha1.StateProvisioned,
		metal3v1alpha1.StatePreparing:
		return true
	}
	return false
}

func getResourceInfo(bmh *metal3v1alpha1.BareMetalHost) inventory.ResourceInfo {
	return inventory.ResourceInfo{
		AdminState:       getResourceInfoAdminState(),
		Description:      getResourceInfoDescription(),
		GlobalAssetId:    getResourceInfoGlobalAssetId(),
		Groups:           getResourceInfoGroups(),
		HwProfile:        getResourceInfoResourceProfileId(bmh),
		Labels:           getResourceInfoLabels(bmh),
		Memory:           getResourceInfoMemory(bmh),
		Model:            getResourceInfoModel(bmh),
		Name:             getResourceInfoName(bmh),
		OperationalState: getResourceInfoOperationalState(),
		PartNumber:       getResourceInfoPartNumber(),
		PowerState:       getResourceInfoPowerState(bmh),
		Processors:       getResourceInfoProcessors(bmh),
		ResourceId:       getResourceInfoResourceId(),
		ResourcePoolId:   getResourceInfoResourcePoolId(bmh),
		SerialNumber:     getResourceInfoSerialNumber(bmh),
		Tags:             getResourceInfoTags(),
		UsageState:       getResourceInfoUsageState(),
		Vendor:           getResourceInfoVendor(bmh),
	}
}

func GetResourcePools(ctx context.Context, c client.Client) (inventory.GetResourcePoolsResponseObject, error) {

	var resp []inventory.ResourcePoolInfo

	var bmhList metal3v1alpha1.BareMetalHostList
	var opts []client.ListOption

	if err := c.List(ctx, &bmhList, opts...); err != nil {
		return nil, fmt.Errorf("failed to list BareMetalHosts: %w", err)
	}

	pools := make(map[string]string)

	for _, bmh := range bmhList.Items {
		if includeInInventory(&bmh) {
			pools[bmh.Labels[LabelSiteID]] = bmh.Labels[LabelResourcePoolID]
		}
	}

	for siteId, poolID := range pools {
		resp = append(resp, inventory.ResourcePoolInfo{
			ResourcePoolId: poolID,
			Description:    poolID,
			Name:           poolID,
			SiteId:         &siteId,
		})
	}

	return inventory.GetResourcePools200JSONResponse(resp), nil
}

func GetResources(ctx context.Context, c client.Client) (inventory.GetResourcesResponseObject, error) {
	var resp []inventory.ResourceInfo

	var bmhList metal3v1alpha1.BareMetalHostList
	var opts []client.ListOption

	if err := c.List(ctx, &bmhList, opts...); err != nil {
		return nil, fmt.Errorf("failed to list BareMetalHosts: %w", err)
	}

	for _, bmh := range bmhList.Items {
		if includeInInventory(&bmh) {
			resp = append(resp, getResourceInfo(&bmh))
		}
	}

	return inventory.GetResources200JSONResponse(resp), nil
}
