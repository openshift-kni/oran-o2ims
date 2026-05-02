/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"strings"

	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// getNAR retrieves the NodeAllocationRequest CR for this ProvisioningRequest.
// The NAR name matches the ProvisioningRequest name (1:1 relationship).
func (t *provisioningRequestReconcilerTask) getNAR(ctx context.Context) (*pluginsv1alpha1.NodeAllocationRequest, error) {
	narNS := ctlrutils.GetEnvOrDefault(constants.DefaultNamespaceEnvName, constants.DefaultNamespace)
	nar := &pluginsv1alpha1.NodeAllocationRequest{}
	if err := t.client.Get(ctx, types.NamespacedName{Name: t.object.Name, Namespace: narNS}, nar); err != nil {
		return nil, fmt.Errorf("failed to get NodeAllocationRequest %s/%s: %w", narNS, t.object.Name, err)
	}
	return nar, nil
}

// setNARClusterProvisioned sets the ClusterProvisioned field on the NodeAllocationRequest
// to signal to the hardware plugin that the cluster is fully provisioned and operational.
func (t *provisioningRequestReconcilerTask) setNARClusterProvisioned(ctx context.Context) error {
	nar, err := t.getNAR(ctx)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get NodeAllocationRequest %s: %w", t.object.Name, err)
	}

	// Skip if already set
	if nar.Spec.ClusterProvisioned {
		return nil
	}

	// Set ClusterProvisioned and update
	patch := client.MergeFrom(nar.DeepCopy())
	nar.Spec.ClusterProvisioned = true
	if err := t.client.Patch(ctx, nar, patch); err != nil {
		return fmt.Errorf("failed to set clusterProvisioned on NodeAllocationRequest %s: %w", t.object.Name, err)
	}

	t.logger.InfoContext(ctx, "Set clusterProvisioned on NodeAllocationRequest", slog.String("nar", t.object.Name))
	return nil
}

// syncNARSkipCleanup synchronizes the skip-cleanup annotation from the ProvisioningRequest
// to the SkipCleanup field on the NodeAllocationRequest. This is used for fulfilled PRs
// where createOrUpdateNodeAllocationRequest is no longer called.
func (t *provisioningRequestReconcilerTask) syncNARSkipCleanup(ctx context.Context) error {
	nar, err := t.getNAR(ctx)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get NodeAllocationRequest %s: %w", t.object.Name, err)
	}

	_, hasAnnotation := t.object.Annotations[ctlrutils.SkipCleanupAnnotation]

	if hasAnnotation == nar.Spec.SkipCleanup {
		return nil // Already in sync
	}

	patch := client.MergeFrom(nar.DeepCopy())
	nar.Spec.SkipCleanup = hasAnnotation
	if err := t.client.Patch(ctx, nar, patch); err != nil {
		return fmt.Errorf("failed to sync skipCleanup on NodeAllocationRequest %s: %w", t.object.Name, err)
	}

	t.logger.InfoContext(ctx, "Synced skipCleanup on NodeAllocationRequest",
		slog.String("nar", t.object.Name),
		slog.Bool("skipCleanup", hasAnnotation))
	return nil
}

// createOrUpdateNodeAllocationRequest creates a new NodeAllocationRequest resource if it doesn't exist or updates it if the spec has changed.
func (t *provisioningRequestReconcilerTask) createOrUpdateNodeAllocationRequest(ctx context.Context,
	clusterNamespace string,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) error {

	// Check if the NAR already exists
	existingNAR, err := t.getNAR(ctx)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to get NodeAllocationRequest %s: %w", t.object.Name, err)
		}
		// NAR doesn't exist — create it
		return t.createNodeAllocationRequestResources(ctx, clusterNamespace, nodeAllocationRequest)
	}

	// NAR exists — compare spec and update if changed.
	// Carry over fields managed by the plugin to avoid false-positive change detection.
	nodeAllocationRequest.Spec.ClusterProvisioned = existingNAR.Spec.ClusterProvisioned
	if !equality.Semantic.DeepEqual(existingNAR.Spec, nodeAllocationRequest.Spec) {
		patch := client.MergeFrom(existingNAR.DeepCopy())
		existingNAR.Spec = nodeAllocationRequest.Spec
		if err := t.client.Patch(ctx, existingNAR, patch); err != nil {
			return fmt.Errorf("failed to update NodeAllocationRequest %s: %w", t.object.Name, err)
		}

		t.logger.InfoContext(ctx,
			fmt.Sprintf("NodeAllocationRequest (%s) spec changes have been detected", t.object.Name))
	}
	return nil
}

func (t *provisioningRequestReconcilerTask) createNodeAllocationRequestResources(ctx context.Context,
	clusterNamespace string,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) error {

	// Create/update the clusterInstance namespace, adding ProvisioningRequest labels to the namespace
	err := t.createClusterInstanceNamespace(ctx, clusterNamespace)
	if err != nil {
		return err
	}

	// Create the NodeAllocationRequest CR
	if err := t.client.Create(ctx, nodeAllocationRequest); err != nil {
		t.logger.ErrorContext(ctx, "Failed to create the NodeAllocationRequest", slog.String("error", err.Error()))
		return fmt.Errorf("failed to create NodeAllocationRequest %s: %w", t.object.Name, err)
	}

	t.logger.InfoContext(ctx, fmt.Sprintf("Created NodeAllocationRequest (%s)", t.object.Name))

	return nil
}

// waitForHardwareData waits for the NodeAllocationRequest to be provisioned and update BMC details
// and bootMacAddress in ClusterInstance.
func (t *provisioningRequestReconcilerTask) waitForHardwareData(
	ctx context.Context,
	clusterInstance *unstructured.Unstructured,
	nodeAllocationRequestResponse *pluginsv1alpha1.NodeAllocationRequest) (bool, *bool, bool, error) {

	var configured *bool
	provisioned, timedOutOrFailed, err := t.checkNodeAllocationRequestProvisionStatus(ctx, clusterInstance, nodeAllocationRequestResponse)
	if err != nil {
		return provisioned, nil, timedOutOrFailed, err
	}
	if provisioned {
		configured, timedOutOrFailed, err = t.checkNodeAllocationRequestConfigStatus(ctx, nodeAllocationRequestResponse)
	}

	return provisioned, configured, timedOutOrFailed, err
}

// updateClusterInstance updates the given ClusterInstance object based on the provisioned nodeAllocationRequest.
func (t *provisioningRequestReconcilerTask) updateClusterInstance(ctx context.Context,
	clusterInstance *unstructured.Unstructured, nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest) error {

	narNS := ctlrutils.GetEnvOrDefault(constants.DefaultNamespaceEnvName, constants.DefaultNamespace)
	allocatedNodeList, err := listAllocatedNodesForNAR(ctx, t.client, t.object.Name, narNS)
	if err != nil {
		return fmt.Errorf("failed to list AllocatedNodes for NodeAllocationRequest '%s': %w", t.object.Name, err)
	}

	hwNodes, err := collectNodeDetails(allocatedNodeList)
	if err != nil {
		return fmt.Errorf("failed to collect hardware node %s details for node allocation request: %w", t.object.Name, err)
	}

	// The pull secret must be in the same namespace as the BMH.
	pullSecretName, err := ctlrutils.GetPullSecretName(clusterInstance)
	if err != nil {
		return fmt.Errorf("failed to get pull secret name from cluster instance: %w", err)
	}
	if err := ctlrutils.CopyPullSecret(ctx, t.client, t.object, t.ctDetails.namespace, pullSecretName, hwNodes); err != nil {
		return fmt.Errorf("failed to copy pull secret: %w", err)
	}

	configErr := t.applyNodeConfiguration(ctx, hwNodes, nodeAllocationRequest, clusterInstance)
	if configErr != nil {
		msg := "Failed to apply node configuration to the rendered ClusterInstance: " + configErr.Error()
		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.HardwareNodeConfigApplied,
			provisioningv1alpha1.CRconditionReasons.NotApplied,
			metav1.ConditionFalse,
			msg)
		ctlrutils.SetProvisioningStateFailed(t.object, msg)
	} else {
		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.HardwareNodeConfigApplied,
			provisioningv1alpha1.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"Node configuration has been applied to the rendered ClusterInstance")
	}

	if updateErr := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
	}

	if configErr != nil {
		return fmt.Errorf("failed to apply node configuration for NodeAllocationRequest '%s': %w", t.object.Name, configErr)
	}
	return nil
}

// checkNodeAllocationRequestStatus checks the NodeAllocationRequest status of a given condition type
// and updates the provisioning request status accordingly.
func (t *provisioningRequestReconcilerTask) checkNodeAllocationRequestStatus(
	ctx context.Context,
	nodeAllocationRequestResponse *pluginsv1alpha1.NodeAllocationRequest,
	condition hwmgmtv1alpha1.ConditionType) (bool, bool, error) {

	var status bool
	var timedOutOrFailed bool
	var err error

	// Guard against consuming stale Configured status during day-2 retries.
	// After a PR spec change, the NAR may still carry a Configured condition
	// from the previous attempt (Failed, TimedOut, or True from a prior success)
	// until the plugin processes the new ConfigTransactionId. Skip the update
	// and requeue until the plugin has observed the new transaction.
	// This only applies to Configured — the Provisioned condition transitions
	// once during initial provisioning and is not affected by spec changes.
	if condition == hwmgmtv1alpha1.Configured &&
		nodeAllocationRequestResponse.Spec.ConfigTransactionId != 0 &&
		nodeAllocationRequestResponse.Status.ObservedConfigTransactionId != nodeAllocationRequestResponse.Spec.ConfigTransactionId {
		for _, c := range nodeAllocationRequestResponse.Status.Conditions {
			if c.Type == string(condition) {
				t.logger.InfoContext(ctx, "Skipping stale NAR status — plugin has not observed new transaction",
					slog.String("condition", string(condition)),
					slog.String("reason", c.Reason),
					slog.Int64("specTransaction", nodeAllocationRequestResponse.Spec.ConfigTransactionId),
					slog.Int64("observedTransaction", nodeAllocationRequestResponse.Status.ObservedConfigTransactionId))
				return false, false, nil
			}
		}
	}

	// Update the provisioning request Status with status from the NodeAllocationRequest object.
	status, timedOutOrFailed, err = t.updateHardwareStatus(ctx, nodeAllocationRequestResponse, condition)
	if err != nil && !ctlrutils.IsConditionDoesNotExistsErr(err) {
		t.logger.ErrorContext(
			ctx,
			"Failed to update the NodeAllocationRequest status for ProvisioningRequest",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)
	}

	return status, timedOutOrFailed, err
}

// checkNodeAllocationRequestProvisionStatus checks the provisioned status of the node allocation request.
func (t *provisioningRequestReconcilerTask) checkNodeAllocationRequestProvisionStatus(
	ctx context.Context,
	clusterInstance *unstructured.Unstructured,
	nodeAllocationRequestResponse *pluginsv1alpha1.NodeAllocationRequest,
) (bool, bool, error) {

	nodeAllocationRequestID := t.object.Name
	if nodeAllocationRequestID == "" {
		return false, false, fmt.Errorf("missing NodeAllocationRequest identifier")
	}

	provisioned, timedOutOrFailed, err := t.checkNodeAllocationRequestStatus(ctx, nodeAllocationRequestResponse, hwmgmtv1alpha1.Provisioned)
	if provisioned && err == nil {
		t.logger.InfoContext(ctx, fmt.Sprintf("NodeAllocationRequest (%s) is provisioned", nodeAllocationRequestID))
		if err = t.updateClusterInstance(ctx, clusterInstance, nodeAllocationRequestResponse); err != nil {
			return provisioned, timedOutOrFailed, fmt.Errorf("failed to update the rendered cluster instance: %w", err)
		}
	}

	return provisioned, timedOutOrFailed, err
}

// checkNodeAllocationRequestConfigStatus checks the Configured status of the node allocation request.
func (t *provisioningRequestReconcilerTask) checkNodeAllocationRequestConfigStatus(
	ctx context.Context,
	nodeAllocationRequestResponse *pluginsv1alpha1.NodeAllocationRequest,
) (*bool, bool, error) {

	status, timedOutOrFailed, err := t.checkNodeAllocationRequestStatus(ctx, nodeAllocationRequestResponse, hwmgmtv1alpha1.Configured)
	if err != nil {
		if ctlrutils.IsConditionDoesNotExistsErr(err) {
			// Condition does not exist, return nil (acceptable case)
			return nil, timedOutOrFailed, nil
		}
		return nil, timedOutOrFailed, fmt.Errorf("failed to check NodeAllocationRequest Configured status: %w", err)
	}
	return &status, timedOutOrFailed, nil
}

// applyNodeConfiguration updates the clusterInstance with BMC details, interface MACAddress and bootMACAddress
func (t *provisioningRequestReconcilerTask) applyNodeConfiguration(
	ctx context.Context,
	hwNodes map[string][]ctlrutils.NodeInfo,
	nar *pluginsv1alpha1.NodeAllocationRequest,
	clusterInstance *unstructured.Unstructured,
) error {

	// Create a map to track unmatched nodes
	unmatchedNodes := make(map[int]string)

	roleToNodeGroupName := getRoleToGroupNameMap(&nar.Spec)

	// Extract the nodes slice
	nodes, found, err := unstructured.NestedSlice(clusterInstance.Object, "spec", "nodes")
	if err != nil {
		return fmt.Errorf("failed to extract nodes from cluster instance: %w", err)
	}
	if !found {
		return fmt.Errorf("spec.nodes not found in cluster instance")
	}

	for i, n := range nodes {
		nodeMap, ok := n.(map[string]interface{})
		if !ok {
			return fmt.Errorf("node at index %d is not a valid map", i)
		}

		role, _, _ := unstructured.NestedString(nodeMap, "role")
		hostName, _, _ := unstructured.NestedString(nodeMap, "hostName")
		groupName := roleToNodeGroupName[role]

		nodeInfos, exists := hwNodes[groupName]
		if !exists || len(nodeInfos) == 0 {
			unmatchedNodes[i] = hostName
			continue
		}

		// Make a copy of the nodeMap before mutating
		updatedNode := maps.Clone(nodeMap)

		// Set BMC info
		updatedNode["bmcAddress"] = nodeInfos[0].BmcAddress
		updatedNode["bmcCredentialsName"] = map[string]interface{}{
			"name": nodeInfos[0].BmcCredentials,
		}

		if nodeInfos[0].HwMgrNodeId != "" && nodeInfos[0].HwMgrNodeNs != "" {
			hostRef, ok := updatedNode["hostRef"].(map[string]interface{})
			if !ok {
				hostRef = make(map[string]interface{})
			}
			hostRef["name"] = nodeInfos[0].HwMgrNodeId
			hostRef["namespace"] = nodeInfos[0].HwMgrNodeNs
			updatedNode["hostRef"] = hostRef
		}
		// Boot MAC
		bootMAC, err := ctlrutils.GetBootMacAddress(nodeInfos[0].Interfaces, constants.BootInterfaceLabel)
		if err != nil {
			return fmt.Errorf("failed to get boot MAC for node '%s': %w", hostName, err)
		}
		updatedNode["bootMACAddress"] = bootMAC

		// Assign MACs to interfaces
		if err := ctlrutils.AssignMacAddress(t.clusterInput.clusterInstanceData, nodeInfos[0].Interfaces, updatedNode); err != nil {
			return fmt.Errorf("failed to assign MACs for node '%s': %w", hostName, err)
		}

		// Update AllocatedNodeHostMap
		if err := t.updateAllocatedNodeHostMap(ctx, nodeInfos[0].NodeID, hostName); err != nil {
			return fmt.Errorf("failed to update status for node '%s': %w", hostName, err)
		}

		// Update the node only after all mutations succeed
		nodes[i] = updatedNode

		// Consume the nodeInfo
		hwNodes[groupName] = nodeInfos[1:]
	}

	// Final write back to clusterInstance
	if err := unstructured.SetNestedSlice(clusterInstance.Object, nodes, "spec", "nodes"); err != nil {
		return fmt.Errorf("failed to update nodes in cluster instance: %w", err)
	}
	// Check if there are unmatched nodes
	if len(unmatchedNodes) > 0 {
		var unmatchedDetails []string
		for idx, name := range unmatchedNodes {
			unmatchedDetails = append(unmatchedDetails, fmt.Sprintf("Index: %d, Host Name: %s", idx, name))
		}
		return fmt.Errorf("failed to find matches for the following nodes: %s", strings.Join(unmatchedDetails, "; "))
	}

	return nil
}

func (t *provisioningRequestReconcilerTask) updateAllocatedNodeHostMap(ctx context.Context, allocatedNodeID, hostName string) error {

	if allocatedNodeID == "" || hostName == "" {
		t.logger.InfoContext(ctx, "Missing either allocatedNodeID or hostName for updating AllocatedNodeHostMap status")
		return nil
	}

	if t.object.Status.Extensions.AllocatedNodeHostMap == nil {
		t.object.Status.Extensions.AllocatedNodeHostMap = make(map[string]string)
	}

	if t.object.Status.Extensions.AllocatedNodeHostMap[allocatedNodeID] == hostName {
		// nothing to do
		return nil
	}

	t.logger.InfoContext(ctx, "Updating AllocatedNodeHostMap status",
		"allocatedNodeID", allocatedNodeID,
		"hostName", hostName)

	t.object.Status.Extensions.AllocatedNodeHostMap[allocatedNodeID] = hostName

	// Update the CR status for the ProvisioningRequest.
	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to update AllocatedNodeHostMap: %w", err)
	}

	return nil
}

// processExistingHardwareCondition processes an existing hardware condition and returns the appropriate status
func (t *provisioningRequestReconcilerTask) processExistingHardwareCondition(
	hwCondition *metav1.Condition,
	condition hwmgmtv1alpha1.ConditionType,
) (metav1.ConditionStatus, string, string, bool) {
	status := hwCondition.Status
	reason := hwCondition.Reason
	message := hwCondition.Message
	timedOutOrFailed := false

	// Check if the condition indicates failure or timeout
	if reason == string(hwmgmtv1alpha1.Failed) || reason == string(hwmgmtv1alpha1.TimedOut) {
		timedOutOrFailed = true
		ctlrutils.SetProvisioningStateFailed(t.object, message)
	}

	// Ensure a consistent message for the provisioning request, regardless of which plugin is used.
	// The message is augmented with additional NAR context for in-progress, failure, and success states.
	// - Success: update provisioningStatus to indicate hardware provisioning/configuration is complete
	// - Timeout/Failure: enrich the base failure message with detailed NAR error context
	// - In-progress: enrich the message with NAR context when available, otherwise provide a generic in-progress message
	if status == metav1.ConditionTrue {
		// Hardware provisioning/configuration completed successfully
		// Update provisioningStatus to reflect completion and allow progression to next phase
		if strings.TrimSpace(message) != "" {
			message = fmt.Sprintf("Hardware %s completed: %s", ctlrutils.GetStatusMessage(condition), message)
		} else {
			message = fmt.Sprintf("Hardware %s completed", ctlrutils.GetStatusMessage(condition))
		}
		ctlrutils.SetProvisioningStateInProgress(t.object, message)
	} else if status == metav1.ConditionFalse {
		if reason == string(hwmgmtv1alpha1.Failed) || reason == string(hwmgmtv1alpha1.TimedOut) {
			timedOutOrFailed = true
			if reason == string(hwmgmtv1alpha1.Failed) {
				// For failures, preserve the detailed error from NAR
				message = fmt.Sprintf("Hardware %s failed: %s", ctlrutils.GetStatusMessage(condition), message)
			} else if reason == string(hwmgmtv1alpha1.TimedOut) {
				// For timeouts, preserve the timeout message from NAR
				message = fmt.Sprintf("Hardware %s failed: %s", ctlrutils.GetStatusMessage(condition), message)
			}
			ctlrutils.SetProvisioningStateFailed(t.object, message)
		} else {
			// For in-progress states, preserve NAR context if it provides useful information
			if strings.TrimSpace(message) != "" {
				message = fmt.Sprintf("Hardware %s is in progress: %s", ctlrutils.GetStatusMessage(condition), message)
			} else {
				message = fmt.Sprintf("Hardware %s is in progress", ctlrutils.GetStatusMessage(condition))
			}
			ctlrutils.SetProvisioningStateInProgress(t.object, message)
		}
	}

	return status, reason, message, timedOutOrFailed
}

// updateHardwareStatus updates the hardware status for the ProvisioningRequest.
// Returns:
//   - status (bool): true if the hardware condition is completed successfully (ConditionTrue)
//   - timedOutOrFailed (bool): true if the hardware has timed out or failed
//   - error: any error that occurred during status processing
func (t *provisioningRequestReconcilerTask) updateHardwareStatus(
	ctx context.Context,
	nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest,
	condition hwmgmtv1alpha1.ConditionType,
) (bool, bool, error) {

	nodeAllocationRequestID := t.object.Name
	if nodeAllocationRequestID == "" {
		return false, false, fmt.Errorf("missing NodeAllocationRequest identifier")
	}

	var (
		status  metav1.ConditionStatus
		reason  string
		message string
		err     error
	)
	timedOutOrFailed := false // Default to false unless explicitly needed

	// Retrieve the given hardware condition(Provisioned or Configured) from the nodeAllocationRequest status.
	var hwCondition *metav1.Condition
	for i := range nodeAllocationRequest.Status.Conditions {
		if nodeAllocationRequest.Status.Conditions[i].Type == string(condition) {
			hwCondition = &nodeAllocationRequest.Status.Conditions[i]
		}
	}

	// Check if we're waiting for a new configuration to start (only when condition doesn't exist)
	waitingForConfigStart := condition == hwmgmtv1alpha1.Configured &&
		hwCondition == nil &&
		!isConfigTransactionObserved(nodeAllocationRequest.Status.ObservedConfigTransactionId, t.object.Generation)

	if hwCondition == nil {
		// Condition does not exist in plugin response
		if waitingForConfigStart {
			// We're waiting for a new configuration to start - return ConditionDoesNotExistsErr
			// to indicate that configuration hasn't started yet for this transaction
			return false, false, &ctlrutils.ConditionDoesNotExistsErr{ConditionName: string(condition)}
		}
		// Condition doesn't exist and we're not waiting for config start
		status = metav1.ConditionFalse
		reason = string(provisioningv1alpha1.CRconditionReasons.InProgress)
		message = fmt.Sprintf("Hardware %s is in progress", ctlrutils.GetStatusMessage(condition))

		if condition == hwmgmtv1alpha1.Configured {
			// If there was no hardware configuration update initiated, return a custom error to
			// indicate that the configured condition does not exist.
			return false, false, &ctlrutils.ConditionDoesNotExistsErr{ConditionName: string(condition)}
		}
		ctlrutils.SetProvisioningStateInProgress(t.object, message)
	} else {
		// A hardware condition was found in plugin response - always process it
		// (even if we're waiting for config start, the plugin has provided valid state)
		status, reason, message, timedOutOrFailed = t.processExistingHardwareCondition(hwCondition, condition)
	}

	conditionType := provisioningv1alpha1.PRconditionTypes.HardwareProvisioned
	if condition == hwmgmtv1alpha1.Configured {
		conditionType = provisioningv1alpha1.PRconditionTypes.HardwareConfigured
	}

	// Map hardware-specific reasons to provisioning request reasons
	provisioningReason := ctlrutils.MapHardwareReasonToProvisioningReason(reason)

	// Handle unknown reasons with warning (only if Unknown was returned)
	if provisioningReason == provisioningv1alpha1.CRconditionReasons.Unknown {
		t.logger.WarnContext(ctx, "Unknown hardware condition reason encountered",
			slog.String("hardwareReason", reason),
			slog.String("conditionType", string(condition)),
			slog.String("status", string(status)))
	}

	// Set the status condition for hardware status.
	ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
		conditionType,
		provisioningReason,
		status,
		message)
	t.logger.InfoContext(ctx, fmt.Sprintf("NodeAllocationRequest (%s) %s status: %s",
		nodeAllocationRequestID, ctlrutils.GetStatusMessage(condition), message))

	// Update the CR status for the ProvisioningRequest.
	if err = ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		err = fmt.Errorf("failed to update Hardware %s status: %w", ctlrutils.GetStatusMessage(condition), err)
	}

	return status == metav1.ConditionTrue, timedOutOrFailed, err
}

// isConfigTransactionObserved checks if the observed config transaction ID matches the expected generation.
// Returns true if the transaction has been observed (not zero and matches generation).
// A value of 0 indicates the transaction has not been observed yet.
func isConfigTransactionObserved(observedID, expectedGeneration int64) bool {
	// Treat zero as unobserved
	if observedID == 0 {
		return false
	}
	// Check if the observed ID matches the expected generation
	return observedID == expectedGeneration
}

// checkExistingNodeAllocationRequest checks for an existing NodeAllocationRequest and verifies changes if necessary
func (t *provisioningRequestReconcilerTask) checkExistingNodeAllocationRequest(
	ctx context.Context,
	hwMgmtData map[string]any,
	nodeAllocationRequestId string) (*pluginsv1alpha1.NodeAllocationRequest, error) {

	nar, err := t.getNAR(ctx)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil //nolint:nilnil // NAR not found is not an error
		}
		return nil, fmt.Errorf("failed to get NodeAllocationRequest '%s': %w", nodeAllocationRequestId, err)
	}

	err = validateNodeGroupsMatchNAR(hwMgmtData, &nar.Spec)
	if err != nil {
		return nil, ctlrutils.NewInputError("%w", err)
	}

	return nar, nil
}

// buildNodeAllocationRequest builds the NodeAllocationRequest from the pre-merged hwMgmt data and cluster instance
func (t *provisioningRequestReconcilerTask) buildNodeAllocationRequest(
	clusterInstance *unstructured.Unstructured) (*pluginsv1alpha1.NodeAllocationRequest, error) {

	hwMgmtData := t.clusterInput.hwMgmtData

	roleCounts := make(map[string]int)
	nodes, found, err := unstructured.NestedSlice(clusterInstance.Object, "spec", "nodes")
	if err != nil {
		return nil, fmt.Errorf("failed to extract nodes from cluster instance: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("spec.nodes not found in cluster instance")
	}

	for i, n := range nodes {
		nodeMap, ok := n.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("node at index %d is not a valid map", i)
		}

		role, _, _ := unstructured.NestedString(nodeMap, "role")
		roleCounts[role]++
	}

	// Extract nodeGroupData from the pre-merged hwMgmt data
	nodeGroupDataRaw, ok := hwMgmtData["nodeGroupData"]
	if !ok {
		return nil, ctlrutils.NewInputError("nodeGroupData not found in merged hwMgmt data")
	}
	nodeGroupDataSlice, ok := nodeGroupDataRaw.([]any)
	if !ok {
		return nil, ctlrutils.NewInputError("nodeGroupData must be an array")
	}

	// Node group validation (name, role, duplicates) is handled by validateMergedNodeGroups
	// which runs before this function. Here we only extract values to build the NAR.
	nodeGroups := []pluginsv1alpha1.NodeGroup{}
	for _, ngRaw := range nodeGroupDataSlice {
		ngMap, ok := ngRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("nodeGroupData element is not a map")
		}

		name, _ := ngMap["name"].(string)
		role, _ := ngMap["role"].(string)
		hwProfile, _ := ngMap["hwProfile"].(string)
		resourcePoolId, _ := ngMap["resourcePoolId"].(string)

		resourceSelector := make(map[string]string)
		if rs, ok := ngMap["resourceSelector"].(map[string]any); ok {
			for k, v := range rs {
				if vStr, ok := v.(string); ok {
					resourceSelector[k] = vStr
				}
			}
		}

		ngd := hwmgmtv1alpha1.NodeGroupData{
			HwProfile:        hwProfile,
			Name:             name,
			ResourcePoolId:   resourcePoolId,
			ResourceSelector: resourceSelector,
			Role:             role,
		}
		nodeGroup := newNodeGroup(ngd, roleCounts)
		nodeGroups = append(nodeGroups, nodeGroup)
	}

	siteIDRaw, err := provisioningv1alpha1.ExtractMatchingInput(
		t.object.Spec.TemplateParameters.Raw, ctlrutils.TemplateParamOCloudSiteId)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s from templateParameters: %w", ctlrutils.TemplateParamOCloudSiteId, err)
	}
	siteID, ok := siteIDRaw.(string)
	if !ok {
		return nil, fmt.Errorf("%s is not a string", ctlrutils.TemplateParamOCloudSiteId)
	}

	clusterIdRaw, err := provisioningv1alpha1.ExtractMatchingInput(
		t.object.Spec.TemplateParameters.Raw, ctlrutils.TemplateParamNodeClusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s from templateParameters: %w", ctlrutils.TemplateParamNodeClusterName, err)
	}
	clusterId, ok := clusterIdRaw.(string)
	if !ok {
		return nil, fmt.Errorf("%s is not a string", ctlrutils.TemplateParamNodeClusterName)
	}

	narNS := ctlrutils.GetEnvOrDefault(constants.DefaultNamespaceEnvName, constants.DefaultNamespace)

	// Set HardwareProvisioningTimeout from merged data, or use default
	timeoutStr := ctlrutils.DefaultHardwareProvisioningTimeout.String()
	if ts, ok := hwMgmtData["hardwareProvisioningTimeout"].(string); ok && ts != "" {
		timeoutStr = ts
	}

	_, hasSkipCleanup := t.object.Annotations[ctlrutils.SkipCleanupAnnotation]

	nar := &pluginsv1alpha1.NodeAllocationRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      t.object.Name,
			Namespace: narNS,
		},
		Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
			ClusterId:                   clusterId,
			NodeGroup:                   nodeGroups,
			LocationSpec:                pluginsv1alpha1.LocationSpec{Site: siteID},
			ConfigTransactionId:         t.object.Generation,
			HardwareProvisioningTimeout: timeoutStr,
			SkipCleanup:                 hasSkipCleanup,
		},
	}

	return nar, nil
}

func (t *provisioningRequestReconcilerTask) handleRenderHardwareTemplate(ctx context.Context,
	clusterInstance *unstructured.Unstructured) (*pluginsv1alpha1.NodeAllocationRequest, error) {

	hwMgmtData := t.clusterInput.hwMgmtData

	// Check if an existing NAR needs validation against current hw config
	if _, err := t.checkExistingNodeAllocationRequest(ctx, hwMgmtData, t.object.Name); err != nil {
		return nil, err
	}

	nodeAllocationRequest, err := t.buildNodeAllocationRequest(clusterInstance)
	if err != nil {
		return nil, err
	}

	return nodeAllocationRequest, nil
}
