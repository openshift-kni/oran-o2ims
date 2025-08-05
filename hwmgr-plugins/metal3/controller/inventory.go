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

func getResourceInfoAdminState() inventory.ResourceInfoAdminState {
	return inventory.ResourceInfoAdminStateUNKNOWN
}

func getResourceInfoDescription(bmh metal3v1alpha1.BareMetalHost) string {
	if bmh.Annotations != nil {
		return bmh.Annotations[AnnotationResourceInfoDescription]
	}

	return emptyString
}

func getResourceInfoGlobalAssetId(bmh metal3v1alpha1.BareMetalHost) *string {
	if bmh.Annotations != nil {
		annotation := bmh.Annotations[AnnotationResourceInfoGlobalAssetId]
		return &annotation
	}

	return &emptyString
}

func getResourceInfoGroups(bmh metal3v1alpha1.BareMetalHost) *[]string {
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

func getResourceInfoPartNumber(bmh metal3v1alpha1.BareMetalHost) string {
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

func getResourceInfoResourceId(bmh metal3v1alpha1.BareMetalHost) string {
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

func getResourceInfoSerialNumber(bmh *metal3v1alpha1.BareMetalHost) string {
	if bmh.Status.HardwareDetails != nil {
		return bmh.Status.HardwareDetails.SystemVendor.SerialNumber
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
		metal3v1alpha1.StatePreparing,
		metal3v1alpha1.StateDeprovisioning:
		return true
	}
	return false
}

func getResourceInfo(bmh *metal3v1alpha1.BareMetalHost, node *pluginsv1alpha1.AllocatedNode) inventory.ResourceInfo {
	return inventory.ResourceInfo{
		AdminState:       getResourceInfoAdminState(),
		Description:      getResourceInfoDescription(*bmh),
		GlobalAssetId:    getResourceInfoGlobalAssetId(*bmh),
		Groups:           getResourceInfoGroups(*bmh),
		HwProfile:        getResourceInfoResourceProfileId(node),
		Labels:           getResourceInfoLabels(bmh),
		Memory:           getResourceInfoMemory(bmh),
		Model:            getResourceInfoModel(bmh),
		Name:             getResourceInfoName(bmh),
		OperationalState: getResourceInfoOperationalState(),
		PartNumber:       getResourceInfoPartNumber(*bmh),
		PowerState:       getResourceInfoPowerState(bmh),
		Processors:       getResourceInfoProcessors(bmh),
		ResourceId:       getResourceInfoResourceId(*bmh),
		ResourcePoolId:   getResourceInfoResourcePoolId(bmh),
		SerialNumber:     getResourceInfoSerialNumber(bmh),
		Tags:             getResourceInfoTags(bmh),
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

	for _, bmh := range bmhList.Items {
		if includeInInventory(&bmh) {
			resp = append(resp, getResourceInfo(&bmh, hwmgrutils.GetNodeForBMH(nodes, &bmh)))
		}
	}

	return inventory.GetResources200JSONResponse(resp), nil
}
