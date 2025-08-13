/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"slices"

	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/server/inventory"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
)

const (
	LabelPrefixResources = "resources.clcm.openshift.io/"
	LabelResourcePoolID  = LabelPrefixResources + "resourcePoolId"
	LabelSiteID          = LabelPrefixResources + "siteId"

	LabelPrefixResourceSelector = "resourceselector.clcm.openshift.io/"

	LabelPrefixInterfaces = "interfacelabel.clcm.openshift.io/"

	AnnotationPrefixResourceInfo        = "resourceinfo.clcm.openshift.io/"
	AnnotationResourceInfoDescription   = AnnotationPrefixResourceInfo + "description"
	AnnotationResourceInfoPartNumber    = AnnotationPrefixResourceInfo + "partNumber"
	AnnotationResourceInfoGlobalAssetId = AnnotationPrefixResourceInfo + "globalAssetId"
	AnnotationsResourceInfoGroups       = AnnotationPrefixResourceInfo + "groups"
)

// The following regex pattern is used to find interface labels
var REPatternInterfaceLabel = regexp.MustCompile(`^` + LabelPrefixInterfaces + `(.*)`)

// The following regex pattern is used to check resourceselector label pattern
var REPatternResourceSelectorLabel = regexp.MustCompile(`^` + LabelPrefixResourceSelector)

var REPatternResourceSelectorLabelMatch = regexp.MustCompile(`^` + LabelPrefixResourceSelector + `(.*)`)
var emptyString = ""

func getResourceInfoAdminState(bmh *metal3v1alpha1.BareMetalHost) inventory.ResourceInfoAdminState {
	// TODO: This should also consider whether the node has been cordoned, at least for an MNO
	if bmh.Spec.Online {
		return inventory.ResourceInfoAdminStateUNLOCKED
	}

	return inventory.ResourceInfoAdminStateLOCKED
}

func getResourceInfoDescription(bmh *metal3v1alpha1.BareMetalHost) string {
	if bmh.Annotations != nil {
		return bmh.Annotations[AnnotationResourceInfoDescription]
	}

	return emptyString
}

func getResourceInfoGlobalAssetId(bmh *metal3v1alpha1.BareMetalHost) *string {
	if bmh.Annotations != nil {
		annotation := bmh.Annotations[AnnotationResourceInfoGlobalAssetId]
		return &annotation
	}

	return &emptyString
}

func getResourceInfoGroups(bmh *metal3v1alpha1.BareMetalHost) *[]string {
	if bmh.Annotations != nil {
		annotation, exists := bmh.Annotations[AnnotationsResourceInfoGroups]
		if exists {
			// Split by comma, removing leading or trailing whitespace around the comma
			re := regexp.MustCompile(` *, *`)
			groups := re.Split(annotation, -1)
			slices.Sort(groups)
			return &groups
		}
	}
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

func getResourceInfoMemory(hwdata *metal3v1alpha1.HardwareData) int {
	if hwdata.Spec.HardwareDetails != nil {
		return hwdata.Spec.HardwareDetails.RAMMebibytes
	}
	return 0
}

func getResourceInfoModel(hwdata *metal3v1alpha1.HardwareData) string {
	if hwdata.Spec.HardwareDetails != nil {
		return hwdata.Spec.HardwareDetails.SystemVendor.ProductName
	}
	return emptyString
}

func getResourceInfoName(bmh *metal3v1alpha1.BareMetalHost) string {
	return bmh.Name
}

func getResourceInfoOperationalState(bmh *metal3v1alpha1.BareMetalHost) inventory.ResourceInfoOperationalState {
	if bmh.Status.OperationalStatus == metal3v1alpha1.OperationalStatusOK &&
		bmh.Spec.Online &&
		bmh.Status.PoweredOn &&
		bmh.Status.Provisioning.State == metal3v1alpha1.StateProvisioned {
		return inventory.ResourceInfoOperationalStateENABLED
	}

	return inventory.ResourceInfoOperationalStateDISABLED
}

func getResourceInfoPartNumber(bmh *metal3v1alpha1.BareMetalHost) string {
	if bmh.Annotations != nil {
		return bmh.Annotations[AnnotationResourceInfoPartNumber]
	}

	return emptyString
}

func getResourceInfoPowerState(bmh *metal3v1alpha1.BareMetalHost) *inventory.ResourceInfoPowerState {
	state := inventory.OFF
	if bmh.Status.PoweredOn {
		state = inventory.ON
	}

	return &state
}

func getProcessorInfoArchitecture(hwdata *metal3v1alpha1.HardwareData) *string {
	if hwdata.Spec.HardwareDetails != nil {
		return &hwdata.Spec.HardwareDetails.CPU.Arch
	}
	return &emptyString
}

func getProcessorInfoCores(hwdata *metal3v1alpha1.HardwareData) *int {
	if hwdata.Spec.HardwareDetails != nil {
		return &hwdata.Spec.HardwareDetails.CPU.Count
	}

	return nil
}

func getProcessorInfoManufacturer() *string {
	return &emptyString
}

func getProcessorInfoModel(hwdata *metal3v1alpha1.HardwareData) *string {
	if hwdata.Spec.HardwareDetails != nil {
		return &hwdata.Spec.HardwareDetails.CPU.Model
	}
	return &emptyString
}

func getResourceInfoProcessors(hwdata *metal3v1alpha1.HardwareData) []inventory.ProcessorInfo {
	processors := []inventory.ProcessorInfo{}

	if hwdata.Spec.HardwareDetails != nil {
		processors = append(processors, inventory.ProcessorInfo{
			Architecture: getProcessorInfoArchitecture(hwdata),
			Cores:        getProcessorInfoCores(hwdata),
			Manufacturer: getProcessorInfoManufacturer(),
			Model:        getProcessorInfoModel(hwdata),
		})
	}
	return processors
}

func getResourceInfoResourceId(bmh *metal3v1alpha1.BareMetalHost) string {
	return fmt.Sprintf("%s/%s", bmh.Namespace, bmh.Name)
}

func getResourceInfoResourcePoolId(bmh *metal3v1alpha1.BareMetalHost) string {
	return bmh.Labels[LabelResourcePoolID]
}

func getResourceInfoResourceProfileId(node *pluginsv1alpha1.AllocatedNode) string {
	if node != nil {
		return node.Status.HwProfile
	}
	return emptyString
}

func getResourceInfoSerialNumber(hwdata *metal3v1alpha1.HardwareData) string {
	if hwdata.Spec.HardwareDetails != nil {
		return hwdata.Spec.HardwareDetails.SystemVendor.SerialNumber
	}
	return emptyString
}

func getResourceInfoTags(bmh *metal3v1alpha1.BareMetalHost) *[]string {
	var tags []string

	for fullLabel, value := range bmh.Labels {
		match := REPatternResourceSelectorLabelMatch.FindStringSubmatch(fullLabel)
		if len(match) != 2 {
			continue
		}

		tags = append(tags, fmt.Sprintf("%s: %s", match[1], value))
	}

	slices.Sort(tags)
	return &tags
}

func getResourceInfoUsageState(bmh *metal3v1alpha1.BareMetalHost) inventory.ResourceInfoUsageState {
	// The following switch statement determines the ResourceInfoUsageState of a BareMetalHost
	// based on its current provisioning state and operational status. It maps the internal
	// Metal3 states to the external API usage states (ACTIVE, BUSY, IDLE, UNKNOWN) as defined
	// in the inventory API, considering whether the host is provisioned, available, or in
	// transition states such as provisioning or deprovisioning.

	switch bmh.Status.Provisioning.State {
	case metal3v1alpha1.StateProvisioned:
		if bmh.Status.OperationalStatus == metal3v1alpha1.OperationalStatusOK && bmh.Spec.Online && bmh.Status.PoweredOn {
			return inventory.ACTIVE
		}

		return inventory.BUSY
	case metal3v1alpha1.StateAvailable:
		if bmh.Status.OperationalStatus == metal3v1alpha1.OperationalStatusOK {
			return inventory.IDLE
		}

		return inventory.BUSY
	case metal3v1alpha1.StateProvisioning,
		metal3v1alpha1.StatePreparing,
		metal3v1alpha1.StateDeprovisioning,
		metal3v1alpha1.StateInspecting,
		metal3v1alpha1.StatePoweringOffBeforeDelete,
		metal3v1alpha1.StateDeleting:
		return inventory.BUSY
	default:
		return inventory.UNKNOWN
	}
}

func getResourceInfoVendor(hwdata *metal3v1alpha1.HardwareData) string {
	if hwdata.Spec.HardwareDetails != nil {
		return hwdata.Spec.HardwareDetails.SystemVendor.Manufacturer
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
		metal3v1alpha1.StatePreparing,
		metal3v1alpha1.StateDeprovisioning:
		return true
	}
	return false
}

func getResourceInfo(bmh *metal3v1alpha1.BareMetalHost, node *pluginsv1alpha1.AllocatedNode, hwdata *metal3v1alpha1.HardwareData) inventory.ResourceInfo {
	return inventory.ResourceInfo{
		AdminState:       getResourceInfoAdminState(bmh),
		Description:      getResourceInfoDescription(bmh),
		GlobalAssetId:    getResourceInfoGlobalAssetId(bmh),
		Groups:           getResourceInfoGroups(bmh),
		HwProfile:        getResourceInfoResourceProfileId(node),
		Labels:           getResourceInfoLabels(bmh),
		Memory:           getResourceInfoMemory(hwdata),
		Model:            getResourceInfoModel(hwdata),
		Name:             getResourceInfoName(bmh),
		OperationalState: getResourceInfoOperationalState(bmh),
		PartNumber:       getResourceInfoPartNumber(bmh),
		PowerState:       getResourceInfoPowerState(bmh),
		Processors:       getResourceInfoProcessors(hwdata),
		ResourceId:       getResourceInfoResourceId(bmh),
		ResourcePoolId:   getResourceInfoResourcePoolId(bmh),
		SerialNumber:     getResourceInfoSerialNumber(hwdata),
		Tags:             getResourceInfoTags(bmh),
		UsageState:       getResourceInfoUsageState(bmh),
		Vendor:           getResourceInfoVendor(hwdata),
	}
}

func GetResourcePools(ctx context.Context, c client.Client) (inventory.GetResourcePoolsResponseObject, error) {

	var resp []inventory.ResourcePoolInfo

	var bmhList metal3v1alpha1.BareMetalHostList
	var opts []client.ListOption

	if err := c.List(ctx, &bmhList, opts...); err != nil {
		return nil, fmt.Errorf("failed to list BareMetalHosts: %w", err)
	}

	for _, bmh := range bmhList.Items {
		if includeInInventory(&bmh) {
			siteID := bmh.Labels[LabelSiteID]
			poolID := bmh.Labels[LabelResourcePoolID]
			resp = append(resp, inventory.ResourcePoolInfo{
				ResourcePoolId: poolID,
				Description:    poolID,
				Name:           poolID,
				SiteId:         &siteID,
			})
		}
	}

	return inventory.GetResourcePools200JSONResponse(resp), nil
}

func GetResources(ctx context.Context,
	logger *slog.Logger,
	c client.Client) (inventory.GetResourcesResponseObject, error) {
	var resp []inventory.ResourceInfo

	nodes, err := hwmgrutils.GetBMHToNodeMap(ctx, logger, c)
	if err != nil {
		return nil, fmt.Errorf("failed to query current nodes: %w", err)
	}

	var bmhList metal3v1alpha1.BareMetalHostList
	if err := c.List(ctx, &bmhList); err != nil {
		return nil, fmt.Errorf("failed to list BareMetalHosts: %w", err)
	}

	var hwdataList metal3v1alpha1.HardwareDataList
	if err := c.List(ctx, &hwdataList); err != nil {
		return nil, fmt.Errorf("failed to list HardwareData: %w", err)
	}

	bmhToHardwareData := make(map[string]metal3v1alpha1.HardwareData)
	for _, hwdata := range hwdataList.Items {
		bmhToHardwareData[hwdata.Namespace+"/"+hwdata.Name] = hwdata
	}

	for _, bmh := range bmhList.Items {
		if includeInInventory(&bmh) {
			hwdata := bmhToHardwareData[bmh.Namespace+"/"+bmh.Name]
			resp = append(resp, getResourceInfo(&bmh, hwmgrutils.GetNodeForBMH(nodes, &bmh), &hwdata))
		}
	}

	return inventory.GetResources200JSONResponse(resp), nil
}
