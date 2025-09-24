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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	hwmgrpluginapi "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/client/provisioning"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"k8s.io/apimachinery/pkg/api/meta"
)

// createOrUpdateNodeAllocationRequest creates a new NodeAllocationRequest resource if it doesn't exist or updates it if the spec has changed.
func (t *provisioningRequestReconcilerTask) createOrUpdateNodeAllocationRequest(ctx context.Context,
	clusterNamespace string,
	nodeAllocationRequest *hwmgrpluginapi.NodeAllocationRequest) error {

	var (
		nodeAllocationRequestID       string
		existingNodeAllocationRequest *hwmgrpluginapi.NodeAllocationRequestResponse
		err                           error
	)

	if t.object.Status.Extensions.NodeAllocationRequestRef != nil {
		nodeAllocationRequestID = t.object.Status.Extensions.NodeAllocationRequestRef.NodeAllocationRequestID
	}

	if nodeAllocationRequestID == "" {
		return t.createNodeAllocationRequestResources(ctx, clusterNamespace, nodeAllocationRequest)
	} else {
		existingNodeAllocationRequest, _, err = t.hwpluginClient.GetNodeAllocationRequest(ctx, nodeAllocationRequestID)
		if err != nil {
			return fmt.Errorf("failed to get NodeAllocationRequest %s: %w", nodeAllocationRequestID, err)
		}
	}

	// The template validate is already completed; compare NodeGroup and update them if necessary
	if !equality.Semantic.DeepEqual(existingNodeAllocationRequest.NodeAllocationRequest.NodeGroup, nodeAllocationRequest.NodeGroup) {
		narID, err := t.hwpluginClient.UpdateNodeAllocationRequest(ctx, nodeAllocationRequestID, *nodeAllocationRequest)
		if err != nil {
			return fmt.Errorf("failed to update NodeAllocationRequest '%s': %w", nodeAllocationRequestID, err)
		}

		if narID != nodeAllocationRequestID {
			return fmt.Errorf("received nodeAllocationRequestID '%s' != expected nodeAllocationRequestID '%s'", narID, nodeAllocationRequestID)
		}

		// Set hardware configuration start time after the NodeAllocationRequest is updated
		currentTime := metav1.Now()
		t.object.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart = &currentTime

		err = ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object)
		if err != nil {
			return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, err)
		}

		t.logger.InfoContext(ctx,
			fmt.Sprintf("NodeAllocationRequest (%s) configuration changes have been detected", nodeAllocationRequestID))
	}
	return nil
}

func (t *provisioningRequestReconcilerTask) createNodeAllocationRequestResources(ctx context.Context,
	clusterNamespace string,
	nodeAllocationRequest *hwmgrpluginapi.NodeAllocationRequest) error {

	// Create/update the clusterInstance namespace, adding ProvisioningRequest labels to the namespace
	err := t.createClusterInstanceNamespace(ctx, clusterNamespace)
	if err != nil {
		return err
	}

	// Create the node allocation request resource
	nodeAllocationRequestID, err := t.hwpluginClient.CreateNodeAllocationRequest(ctx, *nodeAllocationRequest)
	if err != nil {
		t.logger.ErrorContext(ctx, "Failed to create the NodeAllocationRequest", slog.String("error", err.Error()))
		return fmt.Errorf("failed to create/update the NodeAllocationRequest: %w", err)
	}

	// Set NodeAllocationRequestRef
	if t.object.Status.Extensions.NodeAllocationRequestRef == nil {
		t.object.Status.Extensions.NodeAllocationRequestRef = &provisioningv1alpha1.NodeAllocationRequestRef{}
	}
	t.object.Status.Extensions.NodeAllocationRequestRef.NodeAllocationRequestID = nodeAllocationRequestID

	// Set hardware provisioning start time after the NodeAllocationRequest is created
	currentTime := metav1.Now()
	t.object.Status.Extensions.NodeAllocationRequestRef.HardwareProvisioningCheckStart = &currentTime

	err = ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object)
	if err != nil {
		return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, err)
	}

	t.logger.InfoContext(ctx, fmt.Sprintf("Created NodeAllocationRequest (%s) if not already exist", nodeAllocationRequestID))

	return nil
}

// waitForHardwareData waits for the NodeAllocationRequest to be provisioned and update BMC details
// and bootMacAddress in ClusterInstance.
func (t *provisioningRequestReconcilerTask) waitForHardwareData(
	ctx context.Context,
	clusterInstance *unstructured.Unstructured,
	nodeAllocationRequestResponse *hwmgrpluginapi.NodeAllocationRequestResponse) (bool, *bool, bool, error) {

	var configured *bool
	provisioned, timedOutOrFailed, err := t.checkNodeAllocationRequestProvisionStatus(ctx, clusterInstance, nodeAllocationRequestResponse)
	if err != nil {
		return provisioned, nil, timedOutOrFailed, err
	}
	if provisioned {
		configured, timedOutOrFailed, err = t.checkNodeAllocationRequestConfigStatus(ctx, nodeAllocationRequestResponse)
	}

	// Clear callback annotations after hardware processing to prevent false callback detection
	// This ensures future reconciliations aren't incorrectly treated as callback-triggered
	// If no annotations exist, this does nothing
	annotations := t.object.GetAnnotations()
	hasCallbackAnnotations := annotations != nil && annotations[ctlrutils.CallbackReceivedAnnotation] != ""

	if hasCallbackAnnotations {
		t.logger.InfoContext(ctx, "Clearing PR callback annotations after hardware processing",
			"callbackReceived", annotations[ctlrutils.CallbackReceivedAnnotation],
			"callbackStatus", annotations[ctlrutils.CallbackStatusAnnotation],
			"narId", annotations[ctlrutils.CallbackNodeAllocationRequestIdAnnotation],
			"hadError", err != nil)

		// Clear annotations and persist to cluster to prevent false callback detection on next reconciliation
		if clearErr := ctlrutils.ClearPRCallbackAnnotationsWithPatch(ctx, t.client, t.object); clearErr != nil {
			t.logger.WarnContext(ctx, "Failed to clear PR callback annotations", "error", clearErr.Error())
		} else {
			t.logger.InfoContext(ctx, "Successfully cleared PR callback annotations")
		}
	}

	return provisioned, configured, timedOutOrFailed, err
}

// updateClusterInstance updates the given ClusterInstance object based on the provisioned nodeAllocationRequest.
func (t *provisioningRequestReconcilerTask) updateClusterInstance(ctx context.Context,
	clusterInstance *unstructured.Unstructured, nodeAllocationRequest *hwmgrpluginapi.NodeAllocationRequestResponse) error {

	nodeAllocationRequestID := t.getNodeAllocationRequestID()
	if nodeAllocationRequestID == "" {
		return fmt.Errorf("missing nodeAllocationRequest identifier")
	}

	nodes, err := t.hwpluginClient.GetAllocatedNodesFromNodeAllocationRequest(ctx, nodeAllocationRequestID)
	if err != nil {
		return fmt.Errorf("failed to get AllocatedNodes for NodeAllocationRequest '%s': %w", nodeAllocationRequestID, err)
	}

	hwNodes, err := collectNodeDetails(ctx, t.client, nodes)
	if err != nil {
		return fmt.Errorf("failed to collect hardware node %s details for node allocation request: %w", nodeAllocationRequestID, err)
	}

	hwpluginRef, err := ctlrutils.GetHardwarePluginRefFromProvisioningRequest(ctx, t.client, t.object)
	if err != nil {
		return fmt.Errorf("failed to get HardwarePluginRef: %w", err)
	}

	if hwpluginRef != hwmgrutils.Metal3HardwarePluginID {
		if err := ctlrutils.CopyBMCSecrets(ctx, t.client, hwNodes, clusterInstance.GetNamespace()); err != nil {
			return fmt.Errorf("failed to copy BMC secret: %w", err)
		}
	} else {
		// The pull secret must be in the same namespace as the BMH.
		pullSecretName, err := ctlrutils.GetPullSecretName(clusterInstance)
		if err != nil {
			return fmt.Errorf("failed to get pull secret name from cluster instance: %w", err)
		}
		if err := ctlrutils.CopyPullSecret(ctx, t.client, t.object, t.ctDetails.namespace, pullSecretName, hwNodes); err != nil {
			return fmt.Errorf("failed to copy pull secret: %w", err)
		}
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
		return fmt.Errorf("failed to apply node configuration for NodeAllocationRequest '%s': %w", nodeAllocationRequestID, configErr)
	}
	return nil
}

// getHardwareStatusFromConditions reads hardware status from existing ProvisioningRequest conditions
func (t *provisioningRequestReconcilerTask) getHardwareStatusFromConditions(condition hwmgmtv1alpha1.ConditionType) (bool, bool) {
	var conditionType string
	switch condition {
	case hwmgmtv1alpha1.Provisioned:
		conditionType = string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned)
	case hwmgmtv1alpha1.Configured:
		conditionType = string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured)
	default:
		return false, false
	}

	hwCondition := meta.FindStatusCondition(t.object.Status.Conditions, conditionType)
	if hwCondition == nil {
		return false, false
	}

	status := hwCondition.Status == metav1.ConditionTrue
	timedOutOrFailed := hwCondition.Reason == string(provisioningv1alpha1.CRconditionReasons.TimedOut) ||
		hwCondition.Reason == string(provisioningv1alpha1.CRconditionReasons.Failed)

	return status, timedOutOrFailed
}

// checkNodeAllocationRequestStatus checks the NodeAllocationRequest status of a given condition type
// and updates the provisioning request status accordingly.
func (t *provisioningRequestReconcilerTask) checkNodeAllocationRequestStatus(
	ctx context.Context,
	nodeAllocationRequestResponse *hwmgrpluginapi.NodeAllocationRequestResponse,
	condition hwmgmtv1alpha1.ConditionType) (bool, bool, error) {

	var status bool
	var timedOutOrFailed bool
	var err error

	// Only update hardware status on callback notifications or initial setup
	// This eliminates the read-after-write race condition
	if t.shouldUpdateHardwareStatus(condition) {
		t.logger.InfoContext(ctx, "Updating hardware status",
			slog.String("condition", string(condition)))

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
	} else {
		// For non-callback reconciliation, read current status from PR conditions instead of querying plugin
		status, timedOutOrFailed = t.getHardwareStatusFromConditions(condition)
		t.logger.InfoContext(ctx, "Skipping hardware status update - not callback triggered",
			slog.String("condition", string(condition)),
			slog.Bool("currentStatus", status),
			slog.Bool("timedOut", timedOutOrFailed))
	}

	return status, timedOutOrFailed, err
}

// checkNodeAllocationRequestProvisionStatus checks the provisioned status of the node allocation request.
func (t *provisioningRequestReconcilerTask) checkNodeAllocationRequestProvisionStatus(
	ctx context.Context,
	clusterInstance *unstructured.Unstructured,
	nodeAllocationRequestResponse *hwmgrpluginapi.NodeAllocationRequestResponse,
) (bool, bool, error) {

	nodeAllocationRequestID := t.getNodeAllocationRequestID()
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
	nodeAllocationRequestResponse *hwmgrpluginapi.NodeAllocationRequestResponse,
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
	nar *hwmgrpluginapi.NodeAllocationRequestResponse,
	clusterInstance *unstructured.Unstructured,
) error {

	// Create a map to track unmatched nodes
	unmatchedNodes := make(map[int]string)

	roleToNodeGroupName := getRoleToGroupNameMap(nar.NodeAllocationRequest)

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
		bootMAC := ""
		if !t.isHardwareProvisionSkipped() {
			clusterTemplate, err := t.object.GetClusterTemplateRef(ctx, t.client)
			if err != nil {
				return fmt.Errorf("failed to get the ClusterTemplate for ProvisioningRequest %s: %w ", t.object.Name, err)
			}

			hwTemplateName := clusterTemplate.Spec.Templates.HwTemplate
			hwTemplate, err := ctlrutils.GetHardwareTemplate(ctx, t.client, hwTemplateName)
			if err != nil {
				return fmt.Errorf("failed to get the HardwareTemplate %s resource: %w ", hwTemplateName, err)
			}
			bootInterfaceLabel := hwTemplate.Spec.BootInterfaceLabel
			bootMAC, err = ctlrutils.GetBootMacAddress(nodeInfos[0].Interfaces, bootInterfaceLabel)
			if err != nil {
				return fmt.Errorf("failed to get boot MAC for node '%s': %w", hostName, err)
			}
		} else {
			return fmt.Errorf("failed to get boot MAC for node '%s'", hostName)
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
	hwCondition *hwmgrpluginapi.Condition,
	condition hwmgmtv1alpha1.ConditionType,
) (metav1.ConditionStatus, string, string, bool) {
	status := metav1.ConditionStatus(hwCondition.Status)
	reason := hwCondition.Reason
	message := hwCondition.Message
	timedOutOrFailed := false

	// If the condition is Configured and it's completed, reset the configuring check start time.
	if hwCondition.Type == string(hwmgmtv1alpha1.Configured) && status == metav1.ConditionTrue {
		t.object.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart = nil
	} else if hwCondition.Type == string(hwmgmtv1alpha1.Configured) && t.object.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart == nil {
		// HardwareConfiguringCheckStart is nil, so reset it to current time
		currentTime := metav1.Now()
		t.object.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart = &currentTime
	}

	// Unknown or in progress hardware status, check if it timed out
	if status != metav1.ConditionTrue && reason != string(hwmgmtv1alpha1.Failed) {
		// Handle timeout logic
		timedOutOrFailed, reason, message = ctlrutils.HandleHardwareTimeout(
			condition,
			t.object.Status.Extensions.NodeAllocationRequestRef.HardwareProvisioningCheckStart,
			t.object.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart,
			t.timeouts.hardwareProvisioning,
			reason,
			message,
		)
		if timedOutOrFailed {
			ctlrutils.SetProvisioningStateFailed(t.object, message)
		}
	}

	// Ensure a consistent message for the provisioning request, regardless of which plugin is used.
	if status == metav1.ConditionFalse {
		message = fmt.Sprintf("Hardware %s is in progress", ctlrutils.GetStatusMessage(condition))
		ctlrutils.SetProvisioningStateInProgress(t.object, message)

		if reason == string(hwmgmtv1alpha1.Failed) || reason == string(hwmgmtv1alpha1.TimedOut) {
			timedOutOrFailed = true
			if reason == string(hwmgmtv1alpha1.TimedOut) {
				message = fmt.Sprintf("Hardware %s timed out", ctlrutils.GetStatusMessage(condition))
			} else {
				message = fmt.Sprintf("Hardware %s failed", ctlrutils.GetStatusMessage(condition))
			}
			ctlrutils.SetProvisioningStateFailed(t.object, message)
		}
	}

	return status, reason, message, timedOutOrFailed
}

// updateHardwareStatus updates the hardware status for the ProvisioningRequest
func (t *provisioningRequestReconcilerTask) updateHardwareStatus(
	ctx context.Context,
	nodeAllocationRequest *hwmgrpluginapi.NodeAllocationRequestResponse,
	condition hwmgmtv1alpha1.ConditionType,
) (bool, bool, error) {

	nodeAllocationRequestID := t.getNodeAllocationRequestID()
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
	var hwCondition *hwmgrpluginapi.Condition
	if nodeAllocationRequest.Status.Conditions != nil {
		for _, cond := range *nodeAllocationRequest.Status.Conditions {
			if cond.Type == string(condition) {
				hwCondition = &cond
			}
		}
	}

	waitingForConfigStart := condition == hwmgmtv1alpha1.Configured &&
		(nodeAllocationRequest.Status.ObservedConfigTransactionId == nil ||
			*nodeAllocationRequest.Status.ObservedConfigTransactionId != t.object.Generation)

	if hwCondition == nil || waitingForConfigStart {
		// Condition does not exist
		status = metav1.ConditionFalse
		reason = string(provisioningv1alpha1.CRconditionReasons.InProgress)
		message = fmt.Sprintf("Hardware %s is in progress", ctlrutils.GetStatusMessage(condition))

		if condition == hwmgmtv1alpha1.Configured {
			// If there was no hardware configuration update initiated, return a custom error to
			// indicate that the configured condition does not exist.
			if t.object.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart.IsZero() {
				return false, false, &ctlrutils.ConditionDoesNotExistsErr{ConditionName: string(condition)}
			}
		}
		ctlrutils.SetProvisioningStateInProgress(t.object, message)
	} else {
		// A hardware condition was found and should be processed; use its details.
		status, reason, message, timedOutOrFailed = t.processExistingHardwareCondition(hwCondition, condition)
	}

	conditionType := provisioningv1alpha1.PRconditionTypes.HardwareProvisioned
	if condition == hwmgmtv1alpha1.Configured {
		conditionType = provisioningv1alpha1.PRconditionTypes.HardwareConfigured
	}

	// Set the status condition for hardware status.
	ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
		conditionType,
		provisioningv1alpha1.ConditionReason(reason),
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

// isCallbackTriggeredReconciliation checks if this reconciliation was triggered by a hardware plugin callback
func (t *provisioningRequestReconcilerTask) isCallbackTriggeredReconciliation() bool {
	ann := t.object.GetAnnotations()
	if ann == nil {
		return false
	}

	// Must have a non-empty "callback received" marker
	if ann[ctlrutils.CallbackReceivedAnnotation] == "" {
		return false
	}

	// If the callback specified a NAR ID, ensure it matches
	if narFromCallback := ann[ctlrutils.CallbackNodeAllocationRequestIdAnnotation]; narFromCallback != "" {
		var narFromStatus string
		if t.object.Status.Extensions.NodeAllocationRequestRef != nil {
			narFromStatus = t.object.Status.Extensions.NodeAllocationRequestRef.NodeAllocationRequestID
		}
		if narFromStatus != "" && narFromCallback != narFromStatus {
			return false
		}
	}

	return true
}

// shouldUpdateHardwareStatus determines if we should update hardware status based on callback or timeout
func (t *provisioningRequestReconcilerTask) shouldUpdateHardwareStatus(condition hwmgmtv1alpha1.ConditionType) bool {
	// Always update on callback-triggered reconciliation
	if t.isCallbackTriggeredReconciliation() {
		return true
	}

	// For non-callback reconciliation, check different conditions
	switch condition {
	case hwmgmtv1alpha1.Provisioned:
		hwProvisionedCond := meta.FindStatusCondition(t.object.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned))
		return hwProvisionedCond == nil // Only update if no status set yet
	case hwmgmtv1alpha1.Configured:
		hwConfiguredCond := meta.FindStatusCondition(t.object.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured))

		// If no status exists yet, allow update
		if hwConfiguredCond == nil {
			return true
		}

		// If configuration has been started, continue checking until complete
		// Allow updates for in-progress states AND completion states
		configStarted := t.object.Status.Extensions.NodeAllocationRequestRef != nil &&
			!t.object.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart.IsZero()
		configNotCompleted := hwConfiguredCond.Status != metav1.ConditionTrue

		return configStarted && configNotCompleted
	}

	return false
}

// checkExistingNodeAllocationRequest checks for an existing NodeAllocationRequest and verifies changes if necessary
func (t *provisioningRequestReconcilerTask) checkExistingNodeAllocationRequest(
	ctx context.Context,
	hwTemplate *hwmgmtv1alpha1.HardwareTemplate,
	nodeAllocationRequestId string) (*hwmgrpluginapi.NodeAllocationRequestResponse, error) {

	if t.hwpluginClient == nil {
		return nil, fmt.Errorf("hwpluginClient is nil")
	}

	nodeAllocationRequestResponse, exist, err := t.hwpluginClient.GetNodeAllocationRequest(ctx, nodeAllocationRequestId)
	if err != nil {
		return nil, fmt.Errorf("failed to get NodeAllocationRequest '%s': %w", nodeAllocationRequestId, err)
	}
	if exist {
		_, err := compareHardwareTemplateWithNodeAllocationRequest(hwTemplate, nodeAllocationRequestResponse.NodeAllocationRequest)
		if err != nil {
			return nil, ctlrutils.NewInputError("%w", err)
		}
	}

	return nodeAllocationRequestResponse, nil
}

// buildNodeAllocationRequest builds the NodeAllocationRequest based on the templates and cluster instance
func (t *provisioningRequestReconcilerTask) buildNodeAllocationRequest(clusterInstance *unstructured.Unstructured,
	hwTemplate *hwmgmtv1alpha1.HardwareTemplate) (*hwmgrpluginapi.NodeAllocationRequest, error) {

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

	nodeGroups := []hwmgrpluginapi.NodeGroup{}
	for _, group := range hwTemplate.Spec.NodeGroupData {
		ngd := hwmgrpluginapi.NodeGroupData{
			HwProfile:        group.HwProfile,
			Name:             group.Name,
			ResourceGroupId:  group.ResourcePoolId,
			ResourceSelector: group.ResourceSelector,
			Role:             group.Role,
		}
		nodeGroup := newNodeGroup(ngd, roleCounts)
		nodeGroups = append(nodeGroups, nodeGroup)
	}

	siteID, err := provisioningv1alpha1.ExtractMatchingInput(
		t.object.Spec.TemplateParameters.Raw, ctlrutils.TemplateParamOCloudSiteId)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s from templateParameters: %w", ctlrutils.TemplateParamOCloudSiteId, err)
	}

	clusterId, err := provisioningv1alpha1.ExtractMatchingInput(
		t.object.Spec.TemplateParameters.Raw, ctlrutils.TemplateParamNodeClusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s from templateParameters: %w", ctlrutils.TemplateParamNodeClusterName, err)
	}

	callbackURL := t.callbackConfig.BuildCallbackURL(t.object.Name)

	nodeAllocationRequest := &hwmgrpluginapi.NodeAllocationRequest{}
	nodeAllocationRequest.Site = siteID.(string)
	nodeAllocationRequest.ClusterId = clusterId.(string)
	nodeAllocationRequest.NodeGroup = nodeGroups
	nodeAllocationRequest.BootInterfaceLabel = hwTemplate.Spec.BootInterfaceLabel
	nodeAllocationRequest.ConfigTransactionId = t.object.Generation

	// Create callback configuration with the callback URL
	nodeAllocationRequest.Callback = &hwmgrpluginapi.Callback{
		CallbackURL: callbackURL,
		// Note: CaBundleName and AuthClientConfig are optional and can be added later if needed
		// CaBundleName: nil,
		// AuthClientConfig: nil,
	}

	return nodeAllocationRequest, nil
}

func (t *provisioningRequestReconcilerTask) handleRenderHardwareTemplate(ctx context.Context,
	clusterInstance *unstructured.Unstructured) (*hwmgrpluginapi.NodeAllocationRequest, error) {

	clusterTemplate, err := t.object.GetClusterTemplateRef(ctx, t.client)
	if err != nil {
		return nil, fmt.Errorf("failed to get the ClusterTemplate for ProvisioningRequest %s: %w ", t.object.Name, err)
	}

	hwTemplateName := clusterTemplate.Spec.Templates.HwTemplate
	hwTemplate, err := ctlrutils.GetHardwareTemplate(ctx, t.client, hwTemplateName)
	if err != nil {
		return nil, fmt.Errorf("failed to get the HardwareTemplate %s resource: %w ", hwTemplateName, err)
	}

	if t.object.Status.Extensions.NodeAllocationRequestRef != nil {
		nodeAllocationRequestID := t.object.Status.Extensions.NodeAllocationRequestRef.NodeAllocationRequestID
		if _, err := t.checkExistingNodeAllocationRequest(ctx, hwTemplate, nodeAllocationRequestID); err != nil {
			if ctlrutils.IsInputError(err) {
				updateErr := ctlrutils.UpdateHardwareTemplateStatusCondition(ctx, t.client, hwTemplate, provisioningv1alpha1.ConditionType(hwmgmtv1alpha1.Validation),
					provisioningv1alpha1.ConditionReason(hwmgmtv1alpha1.Failed), metav1.ConditionFalse, err.Error())
				if updateErr != nil {
					// nolint: wrapcheck
					return nil, updateErr
				}
			}
			return nil, err
		}
	}

	hwplugin := &hwmgmtv1alpha1.HardwarePlugin{}
	if err := t.client.Get(ctx, types.NamespacedName{Namespace: ctlrutils.GetHwMgrPluginNS(), Name: hwTemplate.Spec.HardwarePluginRef}, hwplugin); err != nil {
		updateErr := ctlrutils.UpdateHardwareTemplateStatusCondition(ctx, t.client, hwTemplate, provisioningv1alpha1.ConditionType(hwmgmtv1alpha1.Validation),
			provisioningv1alpha1.ConditionReason(hwmgmtv1alpha1.Failed), metav1.ConditionFalse,
			"Unable to find specified HardwarePlugin: "+hwTemplate.Spec.HardwarePluginRef)
		if updateErr != nil {
			return nil, fmt.Errorf("failed to update hwtemplate %s status: %w", hwTemplateName, updateErr)
		}
		return nil, fmt.Errorf("could not find specified HardwarePlugin: %s/%s, err=%w", ctlrutils.GetHwMgrPluginNS(), hwTemplate.Spec.HardwarePluginRef, err)
	}

	// The HardwareTemplate is validated by the CRD schema and no additional validation is needed
	updateErr := ctlrutils.UpdateHardwareTemplateStatusCondition(ctx, t.client, hwTemplate, provisioningv1alpha1.ConditionType(hwmgmtv1alpha1.Validation),
		provisioningv1alpha1.ConditionReason(hwmgmtv1alpha1.Completed), metav1.ConditionTrue, "Validated")
	if updateErr != nil {
		// nolint: wrapcheck
		return nil, updateErr
	}

	nodeAllocationRequest, err := t.buildNodeAllocationRequest(clusterInstance, hwTemplate)
	if err != nil {
		return nil, err
	}

	return nodeAllocationRequest, nil
}
