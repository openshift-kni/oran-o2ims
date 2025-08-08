/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
)

// BMHAllocationStatus defines filtering options for FetchBMHList
type BMHAllocationStatus string

const ErrorRetryWindow = 5 * time.Minute

const (
	AllBMHs         BMHAllocationStatus = "all"
	UnallocatedBMHs BMHAllocationStatus = "unallocated"
	AllocatedBMHs   BMHAllocationStatus = "allocated"
)

const (
	BmhRebootAnnotation            = "reboot.metal3.io"
	BmhNetworkDataPrefx            = "network-data"
	BiosUpdateNeededAnnotation     = "clcm.openshift.io/bios-update-needed"
	FirmwareUpdateNeededAnnotation = "clcm.openshift.io/firmware-update-needed"
	BmhAllocatedLabel              = "clcm.openshift.io/allocated"
	NodeNameAnnotation             = "clcm.openshift.io/node-name"
	BmhDeallocationDoneAnnotation  = "clcm.openshift.io/deallocation-complete"
	BmhErrorTimestampAnnotation    = "clcm.openshift.io/bmh-error-timestamp"
	BmhHostMgmtAnnotation          = "bmac.agent-install.openshift.io/allow-provisioned-host-management"
	BmhInfraEnvLabel               = "infraenvs.agent-install.openshift.io"
	SiteConfigOwnedByLabel         = "siteconfig.open-cluster-management.io/owned-by"
	UpdateReasonBIOSSettings       = "bios-settings-update"
	UpdateReasonFirmware           = "firmware-update"
	ValueTrue                      = "true"
	MetaTypeLabel                  = "label"
	MetaTypeAnnotation             = "annotation"
	OpAdd                          = "add"
	OpRemove                       = "remove"
	BmhServicingErr                = "BMH Servicing Error"
)

// Struct definitions for the nodelist configmap
type bmhBmcInfo struct {
	Address         string `json:"address,omitempty"`
	CredentialsName string `json:"credentialsName,omitempty"`
}

type bmhNodeInfo struct {
	ResourcePoolID string                       `json:"poolID,omitempty"`
	BMC            *bmhBmcInfo                  `json:"bmc,omitempty"`
	Interfaces     []*pluginsv1alpha1.Interface `json:"interfaces,omitempty"`
}

func updateBMHMetaWithRetry(
	ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	name types.NamespacedName,
	metaType string, // "label" or "annotation"
	key, value, operation string,
) error {
	// nolint: wrapcheck
	return retry.OnError(retry.DefaultRetry, errors.IsConflict, func() error {
		// Fetch the latest version of the BMH
		var latestBMH metal3v1alpha1.BareMetalHost
		if err := c.Get(ctx, name, &latestBMH); err != nil {
			logger.ErrorContext(ctx, "Failed to fetch BMH for "+metaType+" update",
				slog.Any("bmh", name),
				slog.String("error", err.Error()))
			return err
		}

		var targetMap map[string]string
		switch metaType {
		case MetaTypeLabel:
			if latestBMH.Labels == nil && operation == OpAdd {
				latestBMH.Labels = make(map[string]string)
			}
			targetMap = latestBMH.Labels
		case MetaTypeAnnotation:
			if latestBMH.Annotations == nil && operation == OpAdd {
				latestBMH.Annotations = make(map[string]string)
			}
			targetMap = latestBMH.Annotations
		default:
			return fmt.Errorf("unsupported meta type: %s", metaType)
		}

		if operation == OpRemove {
			if targetMap == nil {
				logger.InfoContext(ctx, fmt.Sprintf("BMH has no %ss, skipping remove operation", metaType),
					slog.Any("bmh", name))
				return nil
			}
			if _, exists := targetMap[key]; !exists {
				logger.InfoContext(ctx, fmt.Sprintf("%s not present, skipping remove operation", metaType),
					slog.Any("bmh", name),
					slog.String(metaType, key))
				return nil
			}
		}

		// Create a patch base
		patch := client.MergeFrom(latestBMH.DeepCopy())

		switch operation {
		case OpAdd:
			targetMap[key] = value
		case OpRemove:
			delete(targetMap, key)
		default:
			return fmt.Errorf("unsupported operation: %s", operation)
		}

		// Apply the patch
		if err := c.Patch(ctx, &latestBMH, patch); err != nil {
			logger.ErrorContext(ctx, "Failed to update BMH "+metaType,
				slog.String("bmh", name.Name),
				slog.String("operation", operation),
				slog.String("error", err.Error()))
			return fmt.Errorf("failed to %s %s on BMH %s: %w", operation, metaType, name.Name, err)
		}

		logger.InfoContext(ctx, "Successfully updated BMH "+metaType,
			slog.String("bmh", name.Name),
			slog.String("operation", operation))
		return nil
	})
}

// FetchBMHList retrieves BareMetalHosts filtered by site ID, allocation status, and optional namespace.
func fetchBMHList(
	ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	site string,
	nodeGroupData hwmgmtv1alpha1.NodeGroupData,
	allocationStatus BMHAllocationStatus,
	namespace string) (metal3v1alpha1.BareMetalHostList, error) {

	var bmhList metal3v1alpha1.BareMetalHostList
	opts := []client.ListOption{}
	matchingLabels := make(client.MatchingLabels)

	// Add site ID filter if provided
	if site != "" {
		matchingLabels[LabelSiteID] = site
	}

	// Add pool ID filter if provided
	if nodeGroupData.ResourcePoolId != "" {
		matchingLabels[LabelResourcePoolID] = nodeGroupData.ResourcePoolId
	}

	for key, value := range nodeGroupData.ResourceSelector {
		fullLabelName := key
		if !REPatternResourceSelectorLabel.MatchString(fullLabelName) {
			fullLabelName = LabelPrefixResourceSelector + key
		}

		matchingLabels[fullLabelName] = value
	}

	// Add namespace filter if provided
	if namespace != "" {
		opts = append(opts, client.InNamespace(namespace))
	}

	// Apply allocation filtering based on enum value
	switch allocationStatus {
	case AllocatedBMHs:
		// Fetch only allocated BMHs
		matchingLabels[BmhAllocatedLabel] = ValueTrue

	case UnallocatedBMHs:
		// Fetch only unallocated BMHs
		selector := metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      BmhAllocatedLabel,
					Operator: metav1.LabelSelectorOpNotIn,
					Values:   []string{ValueTrue}, // Exclude allocated=true
				},
			},
		}
		labelSelector, err := metav1.LabelSelectorAsSelector(&selector)
		if err != nil {
			return bmhList, fmt.Errorf("failed to create label selector: %w", err)
		}
		opts = append(opts, client.MatchingLabelsSelector{Selector: labelSelector})

	case AllBMHs:
		// fetch all BMHs
	}

	opts = append(opts, matchingLabels)

	// Fetch BMHs based on filters
	if err := c.List(ctx, &bmhList, opts...); err != nil {
		return bmhList, fmt.Errorf("failed to get BMH list: %w", err)
	}

	if len(bmhList.Items) == 0 {
		logger.WarnContext(ctx, "No BareMetalHosts found",
			slog.String(LabelSiteID, site),
			slog.String("Allocation Status", string(allocationStatus)))
		return bmhList, nil
	}

	// we only care about the ones in "available" state
	return filterAvailableBMHs(bmhList), nil
}

// filterAvailableBMHs filters out BareMetalHosts that are not in the "Available" provisioning state.
func filterAvailableBMHs(bmhList metal3v1alpha1.BareMetalHostList) metal3v1alpha1.BareMetalHostList {
	var filteredBMHs metal3v1alpha1.BareMetalHostList
	for _, bmh := range bmhList.Items {
		if bmh.Status.Provisioning.State == metal3v1alpha1.StateAvailable {
			filteredBMHs.Items = append(filteredBMHs.Items, bmh)
		}
	}
	return filteredBMHs
}

// GroupBMHsByResourcePool groups unallocated BMHs by resource pool ID.
func GroupBMHsByResourcePool(
	unallocatedBMHs metal3v1alpha1.BareMetalHostList,
) map[string][]metal3v1alpha1.BareMetalHost {
	grouped := make(map[string][]metal3v1alpha1.BareMetalHost)
	for _, bmh := range unallocatedBMHs.Items {
		if resourcePoolID, exists := bmh.Labels[LabelResourcePoolID]; exists {
			grouped[resourcePoolID] = append(grouped[resourcePoolID], bmh)
		}
	}
	return grouped
}

func buildInterfacesFromBMH(
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
	bmh *metal3v1alpha1.BareMetalHost) ([]*pluginsv1alpha1.Interface, error) {
	var interfaces []*pluginsv1alpha1.Interface

	if bmh.Status.HardwareDetails == nil {
		return nil, fmt.Errorf("bareMetalHost.status.hardwareDetails should not be nil")
	}

	for _, nic := range bmh.Status.HardwareDetails.NIC {
		label := ""

		if strings.EqualFold(nic.MAC, bmh.Spec.BootMACAddress) {
			label = nodeAllocationRequest.Spec.BootInterfaceLabel
		} else {
			// Interface labels with MACs use - instead of :
			hyphenatedMac := strings.ReplaceAll(nic.MAC, ":", "-")

			// Process interface labels
			for fullLabel, value := range bmh.Labels {
				match := REPatternInterfaceLabel.FindStringSubmatch(fullLabel)
				if len(match) != 2 {
					continue
				}

				if value == nic.Name || strings.EqualFold(hyphenatedMac, value) {
					// We found a matching label
					label = match[1]
					break
				}
			}
		}

		interfaces = append(interfaces, &pluginsv1alpha1.Interface{
			Name:       nic.Name,
			MACAddress: nic.MAC,
			Label:      label,
		})
	}

	return interfaces, nil
}

func countNodesInGroup(ctx context.Context,
	noncachedClient client.Reader,
	logger *slog.Logger,
	namespace string,
	nodeNames []string,
	groupName string) int {
	count := 0
	for _, nodeName := range nodeNames {
		node, err := hwmgrutils.GetNode(ctx, logger, noncachedClient, namespace, nodeName)
		if err == nil && node != nil {
			if node.Spec.GroupName == groupName {
				count++
			}
		}
	}
	return count
}

func isBMHAllocated(bmh *metal3v1alpha1.BareMetalHost) bool {
	if currentValue, exists := bmh.Labels[BmhAllocatedLabel]; exists && currentValue == ValueTrue {
		return true
	}
	return false
}

func clearBMHNetworkData(ctx context.Context, c client.Client, name types.NamespacedName) error {
	// nolint:wrapcheck
	return retry.OnError(retry.DefaultRetry, errors.IsConflict, func() error {
		updatedBmh := &metal3v1alpha1.BareMetalHost{}

		if err := c.Get(ctx, name, updatedBmh); err != nil {
			return fmt.Errorf("failed to fetch BMH %s/%s: %w", name.Namespace, name.Name, err)
		}
		if updatedBmh.Spec.PreprovisioningNetworkDataName != "" {
			updatedBmh.Spec.PreprovisioningNetworkDataName = ""
			return c.Update(ctx, updatedBmh)
		}
		return nil
	})
}

func processHwProfileWithHandledError(
	ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	pluginNamespace string,
	bmh *metal3v1alpha1.BareMetalHost,
	nodeName, nodeNamepace, profileName string,
	postInstall bool,
) (bool, error) {

	updateRequired, err := processHwProfile(ctx, c, logger, pluginNamespace, bmh, profileName, postInstall)
	contType := string(hwmgmtv1alpha1.Provisioned)
	if postInstall {
		contType = string(hwmgmtv1alpha1.Configured)
	}
	if err != nil {
		reason := hwmgmtv1alpha1.Failed
		if typederrors.IsInputError(err) {
			reason = hwmgmtv1alpha1.InvalidInput
		}
		if err := hwmgrutils.SetNodeConditionStatus(ctx, c, noncachedClient, nodeName, nodeNamepace,
			contType, metav1.ConditionFalse, string(reason), err.Error()); err != nil {
			logger.ErrorContext(ctx, "failed to update node status", slog.String("node", nodeName), slog.String("error", err.Error()))
		}
		return updateRequired, err
	}
	if !updateRequired && postInstall {
		if err := hwmgrutils.SetNodeConditionStatus(ctx, c, noncachedClient, nodeName, nodeNamepace,
			contType, metav1.ConditionTrue, string(hwmgmtv1alpha1.ConfigApplied),
			string(hwmgmtv1alpha1.ConfigSuccess)); err != nil {
			logger.ErrorContext(ctx, "failed to update node status", slog.String("node", nodeName), slog.String("error", err.Error()))
		}
	}
	return updateRequired, nil
}

func processHwProfile(ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	pluginNamespace string,
	bmh *metal3v1alpha1.BareMetalHost, profileName string, postInstall bool) (bool, error) {

	var err error
	name := types.NamespacedName{
		Name:      profileName,
		Namespace: pluginNamespace,
	}

	hwProfile := &hwmgmtv1alpha1.HardwareProfile{}
	if err := c.Get(ctx, name, hwProfile); err != nil {
		return false, fmt.Errorf("unable to find HardwareProfile CR (%s): %w", profileName, err)
	}

	// Check if BIOS update is required
	biosUpdateRequired := false
	if hwProfile.Spec.Bios.Attributes != nil {
		biosUpdateRequired, err = IsBiosUpdateRequired(ctx, c, logger, bmh, hwProfile.Spec.Bios)
		if err != nil {
			return false, err
		}
	}

	// Check if firmware update is required
	firmwareUpdateRequired, err := IsFirmwareUpdateRequired(ctx, c, logger, bmh, hwProfile.Spec)
	if err != nil {
		return false, err
	}

	// If nothing is required, return early
	if !biosUpdateRequired && !firmwareUpdateRequired {
		return false, nil
	}

	if postInstall {
		if err = createOrUpdateHostUpdatePolicy(ctx, c, logger, bmh, firmwareUpdateRequired, biosUpdateRequired); err != nil {
			return true, fmt.Errorf("failed create or update  HostUpdatePolicy%s/%s: %w", bmh.Namespace, bmh.Name, err)
		}
	}

	bmhName := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
	// If bios update is required, annotate BMH
	if biosUpdateRequired {
		if err := updateBMHMetaWithRetry(ctx, c, logger, bmhName, MetaTypeAnnotation, BiosUpdateNeededAnnotation, ValueTrue, OpAdd); err != nil {
			return true, fmt.Errorf("failed to annotate BMH %s/%s: %w", bmh.Namespace, bmh.Name, err)
		}
	}

	// if firmware update is required, annotate BMH
	if firmwareUpdateRequired {
		if err := updateBMHMetaWithRetry(ctx, c, logger, bmhName, MetaTypeAnnotation, FirmwareUpdateNeededAnnotation, ValueTrue, OpAdd); err != nil {
			return true, fmt.Errorf("failed to annotate BMH %s/%s: %w", bmh.Namespace, bmh.Name, err)
		}
	}

	return true, nil
}

func checkBMHStatus(ctx context.Context, logger *slog.Logger, bmh *metal3v1alpha1.BareMetalHost, state metal3v1alpha1.ProvisioningState) bool {
	// Check if the BMH is in  desired state
	if bmh.Status.Provisioning.State == state {
		logger.InfoContext(ctx, "BMH is now in desired state", slog.String("BMH", bmh.Name), slog.Any("State", state))
		return true
	}
	return false
}

// aannotateNodeConfigInProgress sets an annotation on the corresponding Node object to indicate configuration is in progress.
func annotateNodeConfigInProgress(ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	namespace, nodeName, reason string) error {
	// Fetch the Node object
	node := &pluginsv1alpha1.AllocatedNode{}
	if err := c.Get(ctx, types.NamespacedName{Name: nodeName, Namespace: namespace}, node); err != nil {
		return fmt.Errorf("unable to get Node object (%s): %w", nodeName, err)
	}

	// Set annotation to indicate configuration is in progress
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}

	setConfigAnnotation(node, reason)

	// Update the Node object
	if err := c.Update(ctx, node); err != nil {
		logger.InfoContext(ctx, "Failed to annotate node for BIOS configuration", slog.String("node", nodeName))
		return fmt.Errorf("failed to update node %s: %w", nodeName, err)
	}
	logger.InfoContext(ctx, "Annotated node with BIOS config", slog.String("node", nodeName))
	return nil
}

func handleTransitionNodes(ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	pluginNamespace string,
	nodelist *pluginsv1alpha1.AllocatedNodeList, postInstall bool) (bool, error) {

	for _, node := range nodelist.Items {
		bmh, err := getBMHForNode(ctx, c, &node)
		if err != nil {
			return false, fmt.Errorf("failed to get BMH for node %s: %w", node.Name, err)
		}

		if bmh.Annotations == nil {
			bmh.Annotations = make(map[string]string)
		}

		if postInstall {
			if err := evaluateCRForReboot(ctx, c, logger, bmh); err != nil {
				return true, err
			}
		}
		updateCases := []struct {
			AnnotationKey string
			Reason        string
			LogLabel      string
		}{
			{BiosUpdateNeededAnnotation, UpdateReasonBIOSSettings, "BIOS settings"},
			{FirmwareUpdateNeededAnnotation, UpdateReasonFirmware, "firmware"},
		}

		// Process each update case for the current BMH.
		for _, uc := range updateCases {
			if _, exists := bmh.Annotations[uc.AnnotationKey]; !exists {
				continue
			}
			// Only handle one update case per reconciliation cycle
			return true, processBMHUpdateCase(ctx, c, logger, pluginNamespace, &node, bmh, uc, postInstall)
		}
	}

	return false, nil
}

func addRebootAnnotation(ctx context.Context, c client.Client, logger *slog.Logger, bmh *metal3v1alpha1.BareMetalHost) error {
	bmhName := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
	if err := updateBMHMetaWithRetry(ctx, c, logger, bmhName, "annotation", BmhRebootAnnotation, "", OpAdd); err != nil {
		return fmt.Errorf("failed to add %s to BMH %+v: %w", BmhRebootAnnotation, bmhName, err)
	}
	return nil
}

func evaluateCRForReboot(ctx context.Context, c client.Client, logger *slog.Logger, bmh *metal3v1alpha1.BareMetalHost) error {
	// Check if both annotations are present
	hasBiosAnnotation := bmh.Annotations[BiosUpdateNeededAnnotation] != ""
	hasFirmwareAnnotation := bmh.Annotations[FirmwareUpdateNeededAnnotation] != ""

	var biosChange, firmwareChange bool
	var err error

	// If both annotations are present, require both checks to pass
	if hasBiosAnnotation && hasFirmwareAnnotation {
		biosChange, err = isFirmwareSettingsChangeDetectedAndValid(ctx, c, bmh)
		if err != nil {
			return fmt.Errorf("failed to evaluate FirmwareSettings status: %w", err)
		}

		firmwareChange, err = isHostFirmwareComponentsChangeDetectedAndValid(ctx, c, bmh)
		if err != nil {
			return fmt.Errorf("failed to evaluate HostFirmwareComponents status: %w", err)
		}

		if biosChange && firmwareChange {
			return addRebootAnnotation(ctx, c, logger, bmh)
		}
		return nil
	}

	// If only BIOS annotation is present
	if hasBiosAnnotation {
		biosChange, err = isFirmwareSettingsChangeDetectedAndValid(ctx, c, bmh)
		if err != nil {
			return fmt.Errorf("failed to evaluate FirmwareSettings status: %w", err)
		}
		if biosChange {
			return addRebootAnnotation(ctx, c, logger, bmh)
		}
	}

	// If only firmware annotation is present
	if hasFirmwareAnnotation {
		firmwareChange, err = isHostFirmwareComponentsChangeDetectedAndValid(ctx, c, bmh)
		if err != nil {
			return fmt.Errorf("failed to evaluate HostFirmwareComponents status: %w", err)
		}
		if firmwareChange {
			return addRebootAnnotation(ctx, c, logger, bmh)
		}
	}

	return nil
}

// processBMHUpdateCase handles the update for a given BMH and update case.
func processBMHUpdateCase(ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	namespace string,
	node *pluginsv1alpha1.AllocatedNode, bmh *metal3v1alpha1.BareMetalHost,
	uc struct {
		AnnotationKey string
		Reason        string
		LogLabel      string
	}, postInstall bool) error {

	if bmh.Status.OperationalStatus == metal3v1alpha1.OperationalStatusError {
		tolerate, err := tolerateAndAnnotateTransientBMHError(ctx, c, logger, bmh)
		if err != nil || tolerate {
			return nil
		}

		message := fmt.Sprintf("BMH in error state: %s", bmh.Status.ErrorType)
		logger.WarnContext(ctx, message, slog.String("BMH", bmh.Name))
		condType := hwmgmtv1alpha1.Provisioned
		if postInstall {
			condType = hwmgmtv1alpha1.Configured
		}
		if err := hwmgrutils.SetNodeFailedStatus(ctx, c, logger, node, string(condType), message); err != nil {
			logger.ErrorContext(ctx, "failed to set node failed status", slog.String("node", node.Name), slog.String("error", err.Error()))
		}
		return fmt.Errorf("unable to initiate update for BMH %s/%s", bmh.Namespace, bmh.Name)
	}

	// clear transient error annotation if BMH recovered
	if _, hasAnnotation := bmh.Annotations[BmhErrorTimestampAnnotation]; hasAnnotation {
		if err := clearTransientBMHErrorAnnotation(ctx, c, logger, bmh); err != nil {
			logger.WarnContext(ctx, "failed to clean up transient error annotation", slog.String("BMH", bmh.Name), slog.String("error", err.Error()))
			// Don't fail the entire operation for annotation cleanup failure
		}
	}

	// Check whether the current state of the BMH meets the transition condition.
	if postInstall {
		if bmh.Status.OperationalStatus != metal3v1alpha1.OperationalStatusServicing {
			logger.InfoContext(ctx,
				"BMH not in 'Servicing' state yet, requeuing",
				slog.String("BMH", bmh.Name),
				slog.String("expected", string(metal3v1alpha1.OperationalStatusServicing)),
				slog.String("current", string(bmh.Status.OperationalStatus)))
			return nil
		}
		logger.InfoContext(ctx,
			fmt.Sprintf("BMH transitioned to 'Servicing' state for %s update", uc.LogLabel),
			slog.String("BMH", bmh.Name))
	} else {
		if bmh.Status.Provisioning.State != metal3v1alpha1.StatePreparing {
			logger.InfoContext(ctx,
				"BMH not in 'Preparing' state yet, requeuing",
				slog.String("BMH", bmh.Name),
				slog.String("expected", string(metal3v1alpha1.StatePreparing)),
				slog.String("current", string(bmh.Status.Provisioning.State)))
			return nil
		}
		logger.InfoContext(ctx,
			fmt.Sprintf("BMH transitioned to 'Preparing' state for %s update", uc.LogLabel),
			slog.String("BMH", bmh.Name))
	}

	// Remove the update-needed annotation from the BMH.
	bmhName := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
	if err := updateBMHMetaWithRetry(ctx, c, logger, bmhName, MetaTypeAnnotation, uc.AnnotationKey, "", OpRemove); err != nil {
		return fmt.Errorf("failed to remove annotation %s from BMH %s: %w", uc.AnnotationKey, bmh.Name, err)
	}

	// Only add the in-progress annotation if the node is not already annotated.
	if getConfigAnnotation(node) == "" {
		if err := annotateNodeConfigInProgress(ctx, c, logger, namespace, node.Name, uc.Reason); err != nil {
			logger.ErrorContext(ctx,
				fmt.Sprintf("Failed to annotate %s update in progress", uc.LogLabel),
				slog.String("error", err.Error()))
			return err
		}
		logger.InfoContext(ctx,
			fmt.Sprintf("BMH %s update initiated", uc.LogLabel),
			slog.String("BMH", bmh.Name))
	} else {
		logger.InfoContext(ctx,
			"Skipping annotation; another config already in progress",
			slog.String("BMH", bmh.Name),
			slog.String("skipped", uc.Reason))
	}

	return nil
}

func handleBMHCompletion(ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger, nodelist *pluginsv1alpha1.AllocatedNodeList) (bool, error) {

	logger.InfoContext(ctx, "Checking for node with config in progress")
	node := findNodeInProgress(nodelist)
	if node == nil {
		return false, nil // No node is in config progress
	}

	// Get BMH associated with the node
	bmh, err := getBMHForNode(ctx, c, node)
	if err != nil {
		return false, fmt.Errorf("failed to get BMH for node %s: %w", node.Name, err)
	}

	// Check if BMH has transitioned to "Available"
	// If BMH is not available yet, update is still ongoing
	if !checkBMHStatus(ctx, logger, bmh, metal3v1alpha1.StateAvailable) {
		// BMH entered an error state
		if bmh.Status.OperationalStatus == metal3v1alpha1.OperationalStatusError {
			tolerate, err := tolerateAndAnnotateTransientBMHError(ctx, c, logger, bmh)
			if err != nil || tolerate {
				return true, err
			}
			errMessage := fmt.Errorf("bmh %s/%s in an error state %s", bmh.Namespace, bmh.Name, bmh.Status.Provisioning.State)
			if err := hwmgrutils.SetNodeConditionStatus(ctx, c, noncachedClient, node.Name, node.Namespace,
				string(hwmgmtv1alpha1.Provisioned), metav1.ConditionFalse,
				string(hwmgmtv1alpha1.Failed), errMessage.Error()); err != nil {
				logger.ErrorContext(ctx, "failed to set node condition status",
					slog.String("Node", node.Name), slog.String("error", err.Error()))
			}
			return false, errMessage
		}
		// if BMH is not in error state, clean up transient annotation if it exists
		if _, hasAnnotation := bmh.Annotations[BmhErrorTimestampAnnotation]; hasAnnotation {
			if err := clearTransientBMHErrorAnnotation(ctx, c, logger, bmh); err != nil {
				logger.WarnContext(ctx, "failed to clean up transient error annotation", slog.String("BMH", bmh.Name), slog.String("error", err.Error()))
			}
		}
		return true, nil // still waiting for available
	}

	// BMH is now available - clear any stale transient error annotation
	if _, hasAnnotation := bmh.Annotations[BmhErrorTimestampAnnotation]; hasAnnotation {
		if err := clearTransientBMHErrorAnnotation(ctx, c, logger, bmh); err != nil {
			logger.WarnContext(ctx, "failed to clean up transient error annotation on BMH available transition",
				slog.String("BMH", bmh.Name), slog.String("error", err.Error()))
		}
	}

	// Apply post-config updates and finalize the process
	if err := applyPostConfigUpdates(ctx, c, noncachedClient, types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}, node); err != nil {
		return false, fmt.Errorf("failed to apply post config update on node %s: %w", node.Name, err)
	}

	return false, nil // update is now complete
}

func checkForPendingUpdate(ctx context.Context,
	c client.Client,
	noncachedClient client.Reader,
	logger *slog.Logger,
	namespace string,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) (bool, error) {
	// check if there are any pending work
	nodelist, err := hwmgrutils.GetChildNodes(ctx, logger, c, nodeAllocationRequest)
	if err != nil {
		return false, fmt.Errorf("failed to get child nodes for Node Pool %s: %w", nodeAllocationRequest.Name, err)
	}

	// Process BMHs transitioning to "Preparing"
	updating, err := handleTransitionNodes(ctx, c, logger, namespace, nodelist, false)
	if err != nil {
		return updating, err
	}

	if updating {
		logger.InfoContext(ctx, "Skipping handleBMHCompletion as update is in progress")
		return true, nil
	}

	// Check if configuration is completed
	updating, err = handleBMHCompletion(ctx, c, noncachedClient, logger, nodelist)
	if err != nil {
		return updating, err
	}

	return updating, nil
}

func getBMHForNode(ctx context.Context, c client.Client, node *pluginsv1alpha1.AllocatedNode) (*metal3v1alpha1.BareMetalHost, error) {
	bmhName := node.Spec.HwMgrNodeId
	bmhNamespace := node.Spec.HwMgrNodeNs
	name := types.NamespacedName{Name: bmhName, Namespace: bmhNamespace}

	var bmh metal3v1alpha1.BareMetalHost
	if err := c.Get(ctx, name, &bmh); err != nil {
		return nil, fmt.Errorf("unable to find BMH (%v): %w", name, err)
	}

	return &bmh, nil
}

func removeInfraEnvLabelFromPreprovisioningImage(ctx context.Context, c client.Client, logger *slog.Logger, name types.NamespacedName) error {
	// nolint: wrapcheck
	return retry.OnError(retry.DefaultRetry, errors.IsConflict, func() error {
		image := &metal3v1alpha1.PreprovisioningImage{}
		if err := c.Get(ctx, name, image); err != nil {
			logger.ErrorContext(ctx, "Failed to get PreprovisioningImage",
				slog.String("bmh", name.String()),
				slog.String("error", err.Error()))
			return err
		}

		patched := image.DeepCopy()
		delete(patched.Labels, BmhInfraEnvLabel)

		// Patch changes
		patch := client.MergeFrom(image)
		if err := c.Patch(ctx, patched, patch); err != nil {
			logger.ErrorContext(ctx, "Failed to patch PreprovisioningImage",
				slog.String("bmh", name.String()),
				slog.String("error", err.Error()))
			return fmt.Errorf("failed to patch PreprovisioningImage %s: %w", name.String(), err)
		}

		logger.InfoContext(ctx, "Successfully removed InfraEnv label from PreprovisioningImage",
			slog.String("bmh", name.String()))
		return nil
	})
}

// removeInfraEnvLabel removes InfraEnvLabel from BMH and the corresponding PreprovisioningImage resource.
func removeInfraEnvLabel(ctx context.Context, c client.Client, logger *slog.Logger, name types.NamespacedName) error {
	// Remove BmhInfraEnvLabel from BMH
	err := updateBMHMetaWithRetry(ctx, c, logger, name, MetaTypeLabel, BmhInfraEnvLabel, "", OpRemove)
	if err != nil {
		return fmt.Errorf("failed to remove %s label from BMH %v: %w", BmhInfraEnvLabel, name, err)
	}

	// Remove BmhInfraEnvLabel from preprovisioningImage
	err = removeInfraEnvLabelFromPreprovisioningImage(ctx, c, logger, name)
	if err != nil {
		return fmt.Errorf("failed to remove %s label from PreprovisioningImage %v: %w", BmhInfraEnvLabel, name, err)
	}
	return nil
}

// finalizeBMHDeallocation deallocates a BareMetalHost that is no longer associated with a cluster deployment.
func finalizeBMHDeallocation(ctx context.Context, c client.Client, logger *slog.Logger, bmh *metal3v1alpha1.BareMetalHost) error {
	name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
	// nolint: wrapcheck
	return retry.OnError(retry.DefaultRetry, errors.IsConflict, func() error {
		// Fetch the latest version of the BMH
		var current metal3v1alpha1.BareMetalHost
		if err := c.Get(ctx, name, &current); err != nil {
			logger.ErrorContext(ctx, "Failed to get BMH",
				slog.String("bmh", name.String()),
				slog.String("error", err.Error()))
			return err
		}

		patched := current.DeepCopy()

		// Remove allocation-related labels
		for _, key := range []string{SiteConfigOwnedByLabel, BmhAllocatedLabel, ctlrutils.AllocatedNodeLabel} {
			delete(patched.Labels, key)
		}

		// Remove configuration-related annotations
		for _, key := range []string{BiosUpdateNeededAnnotation, FirmwareUpdateNeededAnnotation} {
			delete(patched.Annotations, key)
		}

		// Initialize annotations map if it's nil
		if patched.Annotations == nil {
			patched.Annotations = make(map[string]string)
		}
		patched.Annotations[BmhDeallocationDoneAnnotation] = "true"

		// Clear CustomDeploy entirely
		patched.Spec.CustomDeploy = nil

		if bmh.Status.Provisioning.State == metal3v1alpha1.StateProvisioned {
			// Wipe partition tables using automated cleaning
			patched.Spec.AutomatedCleaningMode = metal3v1alpha1.CleaningModeMetadata
			// Power off the host
			patched.Spec.Online = false
		}

		// Reset pre-provisioning data
		patched.Spec.PreprovisioningNetworkDataName = BmhNetworkDataPrefx + "-" + bmh.Name

		// Clear image reference
		patched.Spec.Image = nil

		// Patch changes
		patch := client.MergeFrom(&current)
		if err := c.Patch(ctx, patched, patch); err != nil {
			logger.ErrorContext(ctx, "Failed to patch BMH",
				slog.String("bmh", name.String()),
				slog.String("error", err.Error()))
			return fmt.Errorf("failed to patch BMH %s: %w", name.String(), err)
		}

		logger.InfoContext(ctx, "Successfully deallocated BMH",
			slog.String("bmh", name.String()))
		return nil
	})
}

// deallocateBMH deallocates a BareMetalHost that is no longer associated with a cluster deployment.
func deallocateBMH(ctx context.Context, c client.Client, logger *slog.Logger, bmh *metal3v1alpha1.BareMetalHost) error {
	name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}

	// Remove InfraEnvLabel: ensure the assisted-service is no longer managing PreprovisioningImage
	if err := removeInfraEnvLabel(ctx, c, logger, name); err != nil {
		return fmt.Errorf("unable to removeInfraEnvLabel: %w", err)
	}

	// Clean up BMH
	if err := finalizeBMHDeallocation(ctx, c, logger, bmh); err != nil {
		return fmt.Errorf("unable to finalizeBMHDeallocation: %w", err)
	}

	return nil
}

// markBMHAllocated sets the "allocated" label to "true" on a BareMetalHost.
func markBMHAllocated(ctx context.Context, c client.Client, logger *slog.Logger, bmh *metal3v1alpha1.BareMetalHost) error {
	// Check if the BMH is already allocated to avoid unnecessary patching
	if isBMHAllocated(bmh) {
		logger.InfoContext(ctx, "BMH is already allocated, skipping update", slog.String("bmh", bmh.Name))
		return nil // No change needed
	}
	name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
	return updateBMHMetaWithRetry(ctx, c, logger, name, MetaTypeLabel, BmhAllocatedLabel, ValueTrue, OpAdd)
}

// allowHostManagement sets bmac.agent-install.openshift.io/allow-provisioned-host-management annotation on a BareMetalHost.
func allowHostManagement(ctx context.Context, c client.Client, logger *slog.Logger, bmh *metal3v1alpha1.BareMetalHost) error {
	if val, exists := bmh.Annotations[BmhHostMgmtAnnotation]; exists && val == "" {
		return nil
	}
	name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
	return updateBMHMetaWithRetry(ctx, c, logger, name, MetaTypeAnnotation, BmhHostMgmtAnnotation, "", OpAdd)
}

func isBMHDeallocated(bmh *metal3v1alpha1.BareMetalHost) bool {
	return bmh.Annotations != nil && bmh.Annotations[BmhDeallocationDoneAnnotation] == "true"
}

// clearBMHAnnotation clears both BmhDeallocationDoneAnnotation and BmhErrorTimestampAnnotation from a BareMetalHost in a single API call
func clearBMHAnnotation(ctx context.Context, c client.Client, logger *slog.Logger, bmh *metal3v1alpha1.BareMetalHost) error {
	name := types.NamespacedName{Namespace: bmh.Namespace, Name: bmh.Name}

	// nolint: wrapcheck
	return retry.OnError(retry.DefaultRetry, errors.IsConflict, func() error {
		// Fetch the latest version of the BMH
		var latestBMH metal3v1alpha1.BareMetalHost
		if err := c.Get(ctx, name, &latestBMH); err != nil {
			logger.ErrorContext(ctx, "Failed to fetch BMH for annotation cleanup",
				slog.Any("bmh", name),
				slog.String("error", err.Error()))
			return err
		}

		// Check if we need to do anything
		needsUpdate := false
		if latestBMH.Annotations != nil {
			if _, exists := latestBMH.Annotations[BmhDeallocationDoneAnnotation]; exists {
				needsUpdate = true
			}
			if _, exists := latestBMH.Annotations[BmhErrorTimestampAnnotation]; exists {
				needsUpdate = true
			}
		}

		// If no annotations to clear, skip the patch
		if !needsUpdate {
			logger.InfoContext(ctx, "No BMH annotations to clear, skipping update",
				slog.Any("bmh", name))
			return nil
		}

		// Create a patch base
		patch := client.MergeFrom(latestBMH.DeepCopy())

		// Remove both annotations in memory
		if latestBMH.Annotations != nil {
			delete(latestBMH.Annotations, BmhDeallocationDoneAnnotation)
			delete(latestBMH.Annotations, BmhErrorTimestampAnnotation)
		}

		// Apply the patch with both changes in a single API call
		if err := c.Patch(ctx, &latestBMH, patch); err != nil {
			logger.ErrorContext(ctx, "Failed to clear BMH annotations",
				slog.String("bmh", name.Name),
				slog.String("error", err.Error()))
			return fmt.Errorf("failed to clear BMH annotations on BMH %s: %w", name.Name, err)
		}

		logger.InfoContext(ctx, "Successfully cleared BMH annotations",
			slog.String("bmh", name.Name))
		return nil
	})
}

func patchOnlineFalse(ctx context.Context, c client.Client, bmh *metal3v1alpha1.BareMetalHost) error {
	name := types.NamespacedName{Namespace: bmh.Namespace, Name: bmh.Name}
	// nolint: wrapcheck
	return retry.OnError(retry.DefaultRetry, errors.IsConflict, func() error {
		var fresh metal3v1alpha1.BareMetalHost
		if err := c.Get(ctx, name, &fresh); err != nil {
			return err
		}
		patched := fresh.DeepCopy()
		patched.Spec.Online = false

		return c.Patch(ctx, patched, client.MergeFrom(&fresh))
	})
}

func markBMHTransitenError(ctx context.Context, c client.Client, logger *slog.Logger, bmh *metal3v1alpha1.BareMetalHost) error {
	if bmh.Annotations == nil {
		bmh.Annotations = make(map[string]string)
	}
	if _, exists := bmh.Annotations[BmhErrorTimestampAnnotation]; exists {
		return nil // Already marked
	}
	bmhName := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
	return updateBMHMetaWithRetry(ctx, c, logger, bmhName, MetaTypeAnnotation,
		BmhErrorTimestampAnnotation,
		time.Now().Format(time.RFC3339), OpAdd)
}

func clearTransientBMHErrorAnnotation(ctx context.Context, c client.Client, logger *slog.Logger, bmh *metal3v1alpha1.BareMetalHost) error {
	bmhName := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
	return updateBMHMetaWithRetry(ctx, c, logger, bmhName, MetaTypeAnnotation,
		BmhErrorTimestampAnnotation,
		"", OpRemove)
}

func isTransientBMHError(bmh *metal3v1alpha1.BareMetalHost) (bool, error) {
	if bmh.Status.OperationalStatus != metal3v1alpha1.OperationalStatusError {
		return false, nil
	}

	tsStr, ok := bmh.Annotations[BmhErrorTimestampAnnotation]
	if !ok {
		// First time seeing the error, should be treated as transient error
		return true, nil
	}

	ts, err := time.Parse(time.RFC3339, tsStr)
	if err != nil {
		return false, fmt.Errorf("invalid BMH error timestamp format: %w", err)
	}

	// Return true if still within retry window
	return time.Since(ts) < ErrorRetryWindow, nil
}

func tolerateAndAnnotateTransientBMHError(
	ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	bmh *metal3v1alpha1.BareMetalHost,
) (bool, error) {
	tolerate, err := isTransientBMHError(bmh)
	if err != nil {
		message := "error checking transient BMH error"
		logger.WarnContext(ctx, message, slog.String("BMH", bmh.Name), slog.String("error", err.Error()))
		return false, fmt.Errorf("%s: %w", message, err)
	}

	if tolerate {
		if err := markBMHTransitenError(ctx, c, logger, bmh); err != nil {
			message := "failed to annotate transient BMH error"
			logger.WarnContext(ctx, message, slog.String("BMH", bmh.Name), slog.String("error", err.Error()))
			return false, fmt.Errorf("%s: %w", message, err)
		}
		logger.InfoContext(ctx, "BMH in transient error — tolerating and skipping failure",
			slog.String("BMH", bmh.Name))
		return true, nil
	}

	return false, nil
}
