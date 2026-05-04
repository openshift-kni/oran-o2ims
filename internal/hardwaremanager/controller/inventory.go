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

	"github.com/google/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/internal/hardwaremanager/utils"
)

const (
	// LabelPrefixResourceSelector is the prefix for resource selector labels on BMHs
	LabelPrefixResourceSelector = "resourceselector.clcm.openshift.io/"

	// ValidationUnavailableLabelKey is the label key used to mark BMHs with missing firmware data
	ValidationUnavailableLabelKey = "validation.clcm.openshift.io/unavailable"

	// Label values for different missing firmware scenarios
	LabelValueMissingFirmwareData = "hfc-missing-firmware-data"
	LabelValueMissingNICData      = "hfc-missing-nic-data"
	LabelValueMissingBMCData      = "hfc-missing-bmc-data"
	LabelValueMissingBIOSData     = "hfc-missing-bios-data"

	AnnotationPrefixResourceInfo      = "resourceinfo.clcm.openshift.io/"
	AnnotationResourceInfoDescription = AnnotationPrefixResourceInfo + "description"
	AnnotationResourceInfoPartNumber  = AnnotationPrefixResourceInfo + "partNumber"
	AnnotationsResourceInfoGroups     = AnnotationPrefixResourceInfo + "groups"
)

// The following regex pattern is used to find interface labels
var REPatternInterfaceLabel = regexp.MustCompile(`^` + constants.LabelPrefixInterfaces + `(.*)`)

// The following regex pattern is used to check resourceselector label pattern
var REPatternResourceSelectorLabel = regexp.MustCompile(`^` + LabelPrefixResourceSelector)

var REPatternResourceSelectorLabelMatch = regexp.MustCompile(`^` + LabelPrefixResourceSelector + `(.*)`)
var emptyString = ""

func getResourceInfoAdminState(bmh *metal3v1alpha1.BareMetalHost) ResourceInfoAdminState {
	// TODO: This should also consider whether the node has been cordoned, at least for an MNO
	if bmh.Spec.Online {
		return ResourceInfoAdminStateUNLOCKED
	}

	return ResourceInfoAdminStateLOCKED
}

func getResourceInfoDescription(bmh *metal3v1alpha1.BareMetalHost) string {
	if bmh.Annotations != nil {
		return bmh.Annotations[AnnotationResourceInfoDescription]
	}

	return emptyString
}

func getResourceInfoGlobalAssetId(hwdata *metal3v1alpha1.HardwareData) *string {
	if hwdata.Spec.HardwareDetails != nil {
		return &hwdata.Spec.HardwareDetails.SystemVendor.SerialNumber
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

func getResourceInfoOperationalState(bmh *metal3v1alpha1.BareMetalHost) ResourceInfoOperationalState {
	if bmh.Status.OperationalStatus == metal3v1alpha1.OperationalStatusOK &&
		bmh.Spec.Online &&
		bmh.Status.PoweredOn &&
		(bmh.Status.Provisioning.State == metal3v1alpha1.StateProvisioned ||
			bmh.Status.Provisioning.State == metal3v1alpha1.StateExternallyProvisioned) {
		return ResourceInfoOperationalStateENABLED
	}

	return ResourceInfoOperationalStateDISABLED
}

func getResourceInfoPowerState(bmh *metal3v1alpha1.BareMetalHost) *ResourceInfoPowerState {
	state := OFF
	if bmh.Status.PoweredOn {
		state = ON
	}

	return &state
}

func getProcessorInfoArchitecture(hwdata *metal3v1alpha1.HardwareData) *string {
	if hwdata.Spec.HardwareDetails != nil {
		return &hwdata.Spec.HardwareDetails.CPU.Arch
	}
	return &emptyString
}

func getProcessorInfoCpus(hwdata *metal3v1alpha1.HardwareData) *int {
	if hwdata.Spec.HardwareDetails != nil {
		return &hwdata.Spec.HardwareDetails.CPU.Count
	}

	return nil
}

func getProcessorInfoFrequency(hwdata *metal3v1alpha1.HardwareData) *int {
	if hwdata.Spec.HardwareDetails != nil {
		freq := int(hwdata.Spec.HardwareDetails.CPU.ClockMegahertz)
		return &freq
	}

	return nil
}

func getProcessorInfoModel(hwdata *metal3v1alpha1.HardwareData) *string {
	if hwdata.Spec.HardwareDetails != nil {
		return &hwdata.Spec.HardwareDetails.CPU.Model
	}
	return &emptyString
}

func getResourceInfoProcessors(hwdata *metal3v1alpha1.HardwareData) []ProcessorInfo {
	processors := []ProcessorInfo{}

	if hwdata.Spec.HardwareDetails != nil {
		processors = append(processors, ProcessorInfo{
			Architecture: getProcessorInfoArchitecture(hwdata),
			Cpus:         getProcessorInfoCpus(hwdata),
			Frequency:    getProcessorInfoFrequency(hwdata),
			Model:        getProcessorInfoModel(hwdata),
		})
	}
	return processors
}

func getResourceInfoResourceId(bmh *metal3v1alpha1.BareMetalHost) uuid.UUID {
	return uuid.MustParse(string(bmh.UID))
}

// getResourceInfoResourcePoolUID returns the Kubernetes UID of the ResourcePool CR.
// It looks up the pool name from the BMH label and finds the corresponding UID from the map.
// Returns empty string if the pool name is not found or doesn't exist in the map.
func getResourceInfoResourcePoolUID(bmh *metal3v1alpha1.BareMetalHost, poolNameToUID map[string]string) string {
	poolName := bmh.Labels[constants.LabelResourcePoolName]
	if poolName == "" {
		return ""
	}
	if uid, ok := poolNameToUID[poolName]; ok {
		return uid
	}
	return ""
}

func getResourceInfoResourceProfileId(node *hwmgmtv1alpha1.AllocatedNode) string {
	if node != nil {
		return node.Status.HwProfile
	}
	return emptyString
}

func getResourceInfoNics(bmh *metal3v1alpha1.BareMetalHost, hwdata *metal3v1alpha1.HardwareData) map[string]NicInfo {
	if hwdata.Spec.HardwareDetails == nil || len(hwdata.Spec.HardwareDetails.NIC) == 0 {
		return nil
	}

	// Build a reverse map of interface name -> label suffix
	interfaceLabels := make(map[string]string)
	if bmh.Labels != nil {
		for fullLabel, interfaceName := range bmh.Labels {
			match := REPatternInterfaceLabel.FindStringSubmatch(fullLabel)
			if len(match) == 2 {
				// match[0] is the full label, match[1] is the suffix after the prefix
				labelSuffix := match[1]
				interfaceLabels[interfaceName] = labelSuffix
			}
		}
	}

	nics := make(map[string]NicInfo)
	for _, nic := range hwdata.Spec.HardwareDetails.NIC {
		nicInfo := NicInfo{
			Mac:       &nic.MAC,
			Model:     &nic.Model,
			SpeedGbps: &nic.SpeedGbps,
		}
		// Add label if this NIC has an interface label
		if labelSuffix, exists := interfaceLabels[nic.Name]; exists {
			nicInfo.Label = &labelSuffix
		}
		// Mark as boot interface if MAC matches BMH bootMACAddress
		if bmh.Spec.BootMACAddress != "" && nic.MAC == bmh.Spec.BootMACAddress {
			bootInterface := true
			nicInfo.BootInterface = &bootInterface
		}
		nics[nic.Name] = nicInfo
	}
	return nics
}

func getResourceInfoStorage(hwdata *metal3v1alpha1.HardwareData) map[string]StorageInfo {
	if hwdata.Spec.HardwareDetails == nil || len(hwdata.Spec.HardwareDetails.Storage) == 0 {
		return nil
	}

	storage := make(map[string]StorageInfo)
	for _, disk := range hwdata.Spec.HardwareDetails.Storage {
		storageType := StorageInfoType(disk.Type)
		storage[disk.Name] = StorageInfo{
			AlternateNames: &disk.AlternateNames,
			Model:          &disk.Model,
			SerialNumber:   &disk.SerialNumber,
			SizeBytes:      (*int64)(&disk.SizeBytes),
			Type:           &storageType,
			Wwn:            &disk.WWN,
		}
	}
	return storage
}

func getResourceInfoAllocated(bmh *metal3v1alpha1.BareMetalHost) *bool {
	if bmh.Labels != nil {
		if allocated, exists := bmh.Labels[BmhAllocatedLabel]; exists && allocated == ValueTrue {
			result := true
			return &result
		}
	}
	result := false
	return &result
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

func getResourceInfoUsageState(bmh *metal3v1alpha1.BareMetalHost) ResourceInfoUsageState {
	// The following switch statement determines the ResourceInfoUsageState of a BareMetalHost
	// based on its current provisioning state and operational status. It maps the internal
	// Metal3 states to the external API usage states (ACTIVE, BUSY, IDLE, UNKNOWN) as defined
	// in the inventory API, considering whether the host is provisioned, available, or in
	// transition states such as provisioning or deprovisioning.

	switch bmh.Status.Provisioning.State {
	case metal3v1alpha1.StateProvisioned, metal3v1alpha1.StateExternallyProvisioned:
		if bmh.Status.OperationalStatus == metal3v1alpha1.OperationalStatusOK && bmh.Spec.Online && bmh.Status.PoweredOn {
			return ACTIVE
		}

		return BUSY
	case metal3v1alpha1.StateAvailable:
		if bmh.Status.OperationalStatus == metal3v1alpha1.OperationalStatusOK {
			return IDLE
		}

		return BUSY
	case metal3v1alpha1.StateProvisioning,
		metal3v1alpha1.StatePreparing,
		metal3v1alpha1.StateDeprovisioning,
		metal3v1alpha1.StateInspecting,
		metal3v1alpha1.StatePoweringOffBeforeDelete,
		metal3v1alpha1.StateDeleting:
		return BUSY
	default:
		return UNKNOWN
	}
}

func getResourceInfoVendor(hwdata *metal3v1alpha1.HardwareData) string {
	if hwdata.Spec.HardwareDetails != nil {
		return hwdata.Spec.HardwareDetails.SystemVendor.Manufacturer
	}
	return emptyString
}

// IsOCloudManaged checks if a BareMetalHost is managed by O-Cloud Manager based on required labels.
// A BMH is considered O-Cloud managed if it has:
// 1. Required label: resourcePoolName (site is derived from ResourcePool -> OCloudSite chain)
// 2. OR at least one resource selector label (resourceselector.clcm.openshift.io/*)
func IsOCloudManaged(bmh *metal3v1alpha1.BareMetalHost) bool {
	if bmh.Labels == nil {
		return false
	}

	// Check for required label (resourcePoolName)
	hasRequiredLabel := bmh.Labels[constants.LabelResourcePoolName] != ""

	// Check for any resource selector labels
	hasResourceSelectorLabels := false
	for label := range bmh.Labels {
		if REPatternResourceSelectorLabel.MatchString(label) {
			hasResourceSelectorLabels = true
			break
		}
	}

	// BMH is O-Cloud managed if it has required label OR resource selector labels
	return hasRequiredLabel || hasResourceSelectorLabels
}

func includeInInventory(bmh *metal3v1alpha1.BareMetalHost) bool {
	if !IsOCloudManaged(bmh) {
		// Ignore BMH CRs without the required labels
		return false
	}

	switch bmh.Status.Provisioning.State {
	case metal3v1alpha1.StateAvailable,
		metal3v1alpha1.StateProvisioning,
		metal3v1alpha1.StateProvisioned,
		metal3v1alpha1.StateExternallyProvisioned,
		metal3v1alpha1.StatePreparing,
		metal3v1alpha1.StateDeprovisioning:
		return true
	}
	return false
}

func getResourceInfo(bmh *metal3v1alpha1.BareMetalHost, node *hwmgmtv1alpha1.AllocatedNode, hwdata *metal3v1alpha1.HardwareData, poolNameToUID map[string]string) ResourceInfo {
	nics := getResourceInfoNics(bmh, hwdata)
	storage := getResourceInfoStorage(hwdata)

	result := ResourceInfo{
		AdminState:       getResourceInfoAdminState(bmh),
		Allocated:        getResourceInfoAllocated(bmh),
		Description:      getResourceInfoDescription(bmh),
		GlobalAssetId:    getResourceInfoGlobalAssetId(hwdata),
		Groups:           getResourceInfoGroups(bmh),
		HwProfile:        getResourceInfoResourceProfileId(node),
		Labels:           getResourceInfoLabels(bmh),
		Memory:           getResourceInfoMemory(hwdata),
		Model:            getResourceInfoModel(hwdata),
		Name:             getResourceInfoName(bmh),
		OperationalState: getResourceInfoOperationalState(bmh),
		PowerState:       getResourceInfoPowerState(bmh),
		Processors:       getResourceInfoProcessors(hwdata),
		ResourceId:       getResourceInfoResourceId(bmh),
		ResourcePoolId:   uuid.MustParse(getResourceInfoResourcePoolUID(bmh, poolNameToUID)),
		Tags:             getResourceInfoTags(bmh),
		UsageState:       getResourceInfoUsageState(bmh),
		Vendor:           getResourceInfoVendor(hwdata),
	}

	if nics != nil {
		result.Nics = &nics
	}

	if storage != nil {
		result.Storage = &storage
	}

	return result
}

func GetResources(ctx context.Context,
	logger *slog.Logger,
	c client.Client) ([]ResourceInfo, error) {
	var resp []ResourceInfo

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

	// Build map of ResourcePool name to UID for lookup
	var poolList inventoryv1alpha1.ResourcePoolList
	if err := c.List(ctx, &poolList); err != nil {
		return nil, fmt.Errorf("failed to list ResourcePools: %w", err)
	}
	poolNameToUID := make(map[string]string, len(poolList.Items))
	for _, pool := range poolList.Items {
		if !inventoryv1alpha1.IsResourceReady(pool.Status.Conditions) {
			logger.DebugContext(ctx, "skipping ResourcePool: not Ready",
				slog.String("pool", pool.Name),
				slog.String("reason", inventoryv1alpha1.GetReadyReason(pool.Status.Conditions)))
			continue
		}
		poolNameToUID[pool.Name] = string(pool.UID)
	}

	bmhToHardwareData := make(map[string]metal3v1alpha1.HardwareData)
	for _, hwdata := range hwdataList.Items {
		bmhToHardwareData[hwdata.Namespace+"/"+hwdata.Name] = hwdata
	}

	for _, bmh := range bmhList.Items {
		if !includeInInventory(&bmh) {
			logger.DebugContext(ctx, "skipping BMH inventory resource: not included in inventory listing",
				slog.String("bmh", bmh.Namespace+"/"+bmh.Name),
				slog.Bool("oCloudManaged", IsOCloudManaged(&bmh)),
				slog.String("provisioningState", string(bmh.Status.Provisioning.State)))
			continue
		}
		poolUID := getResourceInfoResourcePoolUID(&bmh, poolNameToUID)
		if poolUID == "" {
			poolName := bmh.Labels[constants.LabelResourcePoolName]
			logger.Debug("skipping BMH inventory resource: unresolved resourcePoolId",
				slog.String("bmh", bmh.Namespace+"/"+bmh.Name),
				slog.String("poolName", poolName))
			continue
		}
		hwdata := bmhToHardwareData[bmh.Namespace+"/"+bmh.Name]
		resp = append(resp, getResourceInfo(&bmh, hwmgrutils.GetNodeForBMH(nodes, &bmh), &hwdata, poolNameToUID))
	}

	return resp, nil
}
