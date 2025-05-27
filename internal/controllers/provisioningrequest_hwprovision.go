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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// createOrUpdateNodeAllocationRequest creates a new NodeAllocationRequest resource if it doesn't exist or updates it if the spec has changed.
func (t *provisioningRequestReconcilerTask) createOrUpdateNodeAllocationRequest(ctx context.Context, nodeAllocationRequest *hwv1alpha1.NodeAllocationRequest) error {

	existingNodeAllocationRequest := &hwv1alpha1.NodeAllocationRequest{}

	exist, err := utils.DoesK8SResourceExist(ctx, t.client, nodeAllocationRequest.Name, nodeAllocationRequest.Namespace, existingNodeAllocationRequest)
	if err != nil {
		return fmt.Errorf("failed to get NodeAllocationRequest %s in namespace %s: %w", nodeAllocationRequest.GetName(), nodeAllocationRequest.GetNamespace(), err)
	}

	if !exist {
		return t.createNodeAllocationRequestResources(ctx, nodeAllocationRequest)
	}

	// The template validate is already completed; compare NodeGroup and update them if necessary
	if !equality.Semantic.DeepEqual(existingNodeAllocationRequest.Spec.NodeGroup, nodeAllocationRequest.Spec.NodeGroup) {
		// Only process the configuration changes
		patch := client.MergeFrom(existingNodeAllocationRequest.DeepCopy())
		// Update the spec field with the new data
		existingNodeAllocationRequest.Spec = nodeAllocationRequest.Spec
		// Apply the patch to update the NodeAllocationRequest with the new spec
		if err = t.client.Patch(ctx, existingNodeAllocationRequest, patch); err != nil {
			return fmt.Errorf("failed to patch NodeAllocationRequest %s in namespace %s: %w", nodeAllocationRequest.GetName(), nodeAllocationRequest.GetNamespace(), err)
		}

		// Set hardware configuration start time after the NodeAllocationRequest is updated
		if t.object.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart.IsZero() {
			currentTime := metav1.Now()
			t.object.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart = &currentTime
		}
		err = utils.UpdateK8sCRStatus(ctx, t.client, t.object)
		if err != nil {
			return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, err)
		}

		t.logger.InfoContext(
			ctx,
			fmt.Sprintf(
				"NodeAllocationRequest (%s) in the namespace %s configuration changes have been detected",
				nodeAllocationRequest.GetName(),
				nodeAllocationRequest.GetNamespace(),
			),
		)
	}
	return nil
}

func (t *provisioningRequestReconcilerTask) createNodeAllocationRequestResources(ctx context.Context, nodeAllocationRequest *hwv1alpha1.NodeAllocationRequest) error {
	// Create the hardware plugin namespace.
	pluginNameSpace := nodeAllocationRequest.ObjectMeta.Namespace
	if exists, err := utils.HwMgrPluginNamespaceExists(ctx, t.client, pluginNameSpace); err != nil {
		return fmt.Errorf("failed check if hardware manager plugin namespace exists %s, err: %w", pluginNameSpace, err)
	} else if !exists {
		return fmt.Errorf("specified hardware manager plugin namespace does not exist: %s", pluginNameSpace)
	}

	// Create/update the clusterInstance namespace, adding ProvisioningRequest labels to the namespace
	err := t.createClusterInstanceNamespace(ctx, nodeAllocationRequest.GetName())
	if err != nil {
		return err
	}

	// Create the node allocation request resource
	createErr := utils.CreateK8sCR(ctx, t.client, nodeAllocationRequest, t.object, "")
	if createErr != nil {
		t.logger.ErrorContext(
			ctx,
			fmt.Sprintf(
				"Failed to create the NodeAllocationRequest %s in the namespace %s",
				nodeAllocationRequest.GetName(),
				nodeAllocationRequest.GetNamespace(),
			),
			slog.String("error", createErr.Error()),
		)
		return fmt.Errorf("failed to create/update the NodeAllocationRequest: %s", createErr.Error())
	}

	// Set NodeAllocationRequestRef
	if t.object.Status.Extensions.NodeAllocationRequestRef == nil {
		t.object.Status.Extensions.NodeAllocationRequestRef = &provisioningv1alpha1.NodeAllocationRequestRef{}
	}
	t.object.Status.Extensions.NodeAllocationRequestRef.Name = nodeAllocationRequest.GetName()
	t.object.Status.Extensions.NodeAllocationRequestRef.Namespace = nodeAllocationRequest.GetNamespace()
	// Set hardware provisioning start time after the NodeAllocationRequest is created
	currentTime := metav1.Now()
	t.object.Status.Extensions.NodeAllocationRequestRef.HardwareProvisioningCheckStart = &currentTime

	err = utils.UpdateK8sCRStatus(ctx, t.client, t.object)
	if err != nil {
		return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, err)
	}

	t.logger.InfoContext(
		ctx,
		fmt.Sprintf(
			"Created NodeAllocationRequest (%s) in the namespace %s, if not already exist",
			nodeAllocationRequest.GetName(),
			nodeAllocationRequest.GetNamespace(),
		),
	)

	return nil
}

// waitForHardwareData waits for the NodeAllocationRequest to be provisioned and update BMC details
// and bootMacAddress in ClusterInstance.
func (t *provisioningRequestReconcilerTask) waitForHardwareData(ctx context.Context,
	clusterInstance *unstructured.Unstructured, nodeAllocationRequest *hwv1alpha1.NodeAllocationRequest) (bool, *bool, bool, error) {

	var configured *bool
	provisioned, timedOutOrFailed, err := t.checkNodeAllocationRequestProvisionStatus(ctx, clusterInstance, nodeAllocationRequest)
	if err != nil {
		return provisioned, nil, timedOutOrFailed, err
	}
	if provisioned {
		configured, timedOutOrFailed, err = t.checkNodeAllocationRequestConfigStatus(ctx, nodeAllocationRequest)
	}
	return provisioned, configured, timedOutOrFailed, err
}

// updateClusterInstance updates the given ClusterInstance object based on the provisioned nodeAllocationRequest.
func (t *provisioningRequestReconcilerTask) updateClusterInstance(ctx context.Context,
	clusterInstance *unstructured.Unstructured, nodeAllocationRequest *hwv1alpha1.NodeAllocationRequest) error {

	hwNodes, err := utils.CollectNodeDetails(ctx, t.client, nodeAllocationRequest)
	if err != nil {
		return fmt.Errorf("failed to collect hardware node %s details for node allocation request: %w", nodeAllocationRequest.GetName(), err)
	}
	if nodeAllocationRequest.Spec.HwMgrId != utils.Metal3PluginName {
		if err := utils.CopyBMCSecrets(ctx, t.client, hwNodes, nodeAllocationRequest); err != nil {
			return fmt.Errorf("failed to copy BMC secret: %w", err)
		}
	} else {
		// The pull secret must be in the same namespace as the BMH.
		pullSecretName, err := utils.GetPullSecretName(clusterInstance)
		if err != nil {
			return fmt.Errorf("failed to get pull secret name from cluster instance: %w", err)
		}
		if err := utils.CopyPullSecret(ctx, t.client, t.object, t.ctDetails.namespace, pullSecretName, hwNodes); err != nil {
			return fmt.Errorf("failed to copy pull secret: %w", err)
		}
	}

	configErr := t.applyNodeConfiguration(ctx, hwNodes, nodeAllocationRequest, clusterInstance)
	if configErr != nil {
		msg := "Failed to apply node configuration to the rendered ClusterInstance: " + configErr.Error()
		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.HardwareNodeConfigApplied,
			provisioningv1alpha1.CRconditionReasons.NotApplied,
			metav1.ConditionFalse,
			msg)
		utils.SetProvisioningStateFailed(t.object, msg)
	} else {
		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.HardwareNodeConfigApplied,
			provisioningv1alpha1.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"Node configuration has been applied to the rendered ClusterInstance")
	}

	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
	}

	if configErr != nil {
		return fmt.Errorf("failed to apply node configuration for NodeAllocationRequest %s: %w", nodeAllocationRequest.GetName(), configErr)
	}
	return nil
}

// checkNodeAllocationRequestStatus checks the NodeAllocationRequest status of a given condition type
// and updates the provisioning request status accordingly.
func (t *provisioningRequestReconcilerTask) checkNodeAllocationRequestStatus(ctx context.Context,
	nodeAllocationRequest *hwv1alpha1.NodeAllocationRequest, condition hwv1alpha1.ConditionType) (bool, bool, error) {

	// Get the generated NodeAllocationRequest and its status.
	if err := utils.RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
		exists, err := utils.DoesK8SResourceExist(ctx, t.client, nodeAllocationRequest.GetName(),
			nodeAllocationRequest.GetNamespace(), nodeAllocationRequest)
		if err != nil {
			return fmt.Errorf("failed to get node allocation request; %w", err)
		}
		if !exists {
			return fmt.Errorf("node allocation request does not exist")
		}
		return nil
	}); err != nil {
		// nolint: wrapcheck
		return false, false, err
	}

	// Update the provisioning request Status with status from the NodeAllocationRequest object.
	status, timedOutOrFailed, err := t.updateHardwareStatus(ctx, nodeAllocationRequest, condition)
	if err != nil && !utils.IsConditionDoesNotExistsErr(err) {
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
func (t *provisioningRequestReconcilerTask) checkNodeAllocationRequestProvisionStatus(ctx context.Context,
	clusterInstance *unstructured.Unstructured, nodeAllocationRequest *hwv1alpha1.NodeAllocationRequest) (bool, bool, error) {

	provisioned, timedOutOrFailed, err := t.checkNodeAllocationRequestStatus(ctx, nodeAllocationRequest, hwv1alpha1.Provisioned)
	if provisioned && err == nil {
		t.logger.InfoContext(
			ctx,
			fmt.Sprintf(
				"NodeAllocationRequest (%s) in the namespace %s is provisioned",
				nodeAllocationRequest.GetName(),
				nodeAllocationRequest.GetNamespace(),
			),
		)
		if err = t.updateClusterInstance(ctx, clusterInstance, nodeAllocationRequest); err != nil {
			return provisioned, timedOutOrFailed, fmt.Errorf("failed to update the rendered cluster instance: %w", err)
		}
	}

	return provisioned, timedOutOrFailed, err
}

// checkNodeAllocationRequestConfigStatus checks the configured status of the node allocation request.
func (t *provisioningRequestReconcilerTask) checkNodeAllocationRequestConfigStatus(ctx context.Context, nodeAllocationRequest *hwv1alpha1.NodeAllocationRequest) (*bool, bool, error) {

	status, timedOutOrFailed, err := t.checkNodeAllocationRequestStatus(ctx, nodeAllocationRequest, hwv1alpha1.Configured)
	if err != nil {
		if utils.IsConditionDoesNotExistsErr(err) {
			// Condition does not exist, return nil (acceptable case)
			return nil, timedOutOrFailed, nil
		}
		return nil, timedOutOrFailed, fmt.Errorf("failed to check NodeAllocationRequest configured status: %w", err)
	}
	return &status, timedOutOrFailed, nil
}

// applyNodeConfiguration updates the clusterInstance with BMC details, interface MACAddress and bootMACAddress
func (t *provisioningRequestReconcilerTask) applyNodeConfiguration(ctx context.Context, hwNodes map[string][]utils.NodeInfo,
	nodeAllocationRequest *hwv1alpha1.NodeAllocationRequest, clusterInstance *unstructured.Unstructured) error {

	// Create a map to track unmatched nodes
	unmatchedNodes := make(map[int]string)

	roleToNodeGroupName := utils.GetRoleToGroupNameMap(nodeAllocationRequest)

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
		bootMAC, err := utils.GetBootMacAddress(nodeInfos[0].Interfaces, nodeAllocationRequest)
		if err != nil {
			return fmt.Errorf("failed to get boot MAC for node '%s': %w", hostName, err)
		}
		updatedNode["bootMACAddress"] = bootMAC

		// Assign MACs to interfaces
		if err := utils.AssignMacAddress(t.clusterInput.clusterInstanceData, nodeInfos[0].Interfaces, updatedNode); err != nil {
			return fmt.Errorf("failed to assign MACs for node '%s': %w", hostName, err)
		}

		// Update node status
		if err := utils.UpdateNodeStatusWithHostname(ctx, t.client, nodeInfos[0].NodeName, hostName, nodeAllocationRequest.Namespace); err != nil {
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

// updateHardwareStatus updates the hardware status for the ProvisioningRequest
func (t *provisioningRequestReconcilerTask) updateHardwareStatus(
	ctx context.Context, nodeAllocationRequest *hwv1alpha1.NodeAllocationRequest, condition hwv1alpha1.ConditionType) (bool, bool, error) {
	if t.object.Status.Extensions.NodeAllocationRequestRef == nil {
		return false, false, fmt.Errorf("status.nodeAllocationRequestRef is empty")
	}

	var (
		status  metav1.ConditionStatus
		reason  string
		message string
		err     error
	)
	timedOutOrFailed := false // Default to false unless explicitly needed

	// Retrieve the given hardware condition(Provisioned or Configured) from the nodeAllocationRequest status.
	hwCondition := meta.FindStatusCondition(nodeAllocationRequest.Status.Conditions, string(condition))
	if hwCondition == nil {
		// Condition does not exist
		status = metav1.ConditionUnknown
		reason = string(provisioningv1alpha1.CRconditionReasons.Unknown)
		message = fmt.Sprintf("Waiting for NodeAllocationRequest (%s) to be processed", nodeAllocationRequest.GetName())

		if condition == hwv1alpha1.Configured {
			// If there was no hardware configuration update initiated, return a custom error to
			// indicate that the configured condition does not exist.
			if t.object.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart.IsZero() {
				return false, false, &utils.ConditionDoesNotExistsErr{ConditionName: string(condition)}
			}
		}
		utils.SetProvisioningStateInProgress(t.object, message)
	} else {
		// A hardware condition was found; use its details.
		status = hwCondition.Status
		reason = hwCondition.Reason
		message = hwCondition.Message

		// If the condition is Configured and it's completed, reset the configuring check start time.
		if hwCondition.Type == string(hwv1alpha1.Configured) && status == metav1.ConditionTrue {
			t.object.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart = nil
		} else if hwCondition.Type == string(hwv1alpha1.Configured) && t.object.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart == nil {
			// HardwareConfiguringCheckStart is nil, so reset it to current time
			currentTime := metav1.Now()
			t.object.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart = &currentTime
		}

		// Ensure a consistent message for the provisioning request, regardless of which plugin is used.
		if status == metav1.ConditionFalse {
			message = fmt.Sprintf("Hardware %s is in progress", utils.GetStatusMessage(condition))
			utils.SetProvisioningStateInProgress(t.object, message)

			if reason == string(hwv1alpha1.Failed) {
				timedOutOrFailed = true
				message = fmt.Sprintf("Hardware %s failed", utils.GetStatusMessage(condition))
				utils.SetProvisioningStateFailed(t.object, message)
			}
		}
	}

	// Unknown or in progress hardware status, check if it timed out
	if status != metav1.ConditionTrue && reason != string(hwv1alpha1.Failed) {
		// Handle timeout logic
		timedOutOrFailed, reason, message = utils.HandleHardwareTimeout(
			condition,
			t.object.Status.Extensions.NodeAllocationRequestRef.HardwareProvisioningCheckStart,
			t.object.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart,
			t.timeouts.hardwareProvisioning,
			reason,
			message,
		)
		if timedOutOrFailed {
			utils.SetProvisioningStateFailed(t.object, message)
		}
	}

	conditionType := provisioningv1alpha1.PRconditionTypes.HardwareProvisioned
	if condition == hwv1alpha1.Configured {
		conditionType = provisioningv1alpha1.PRconditionTypes.HardwareConfigured
	}

	// Set the status condition for hardware status.
	utils.SetStatusCondition(&t.object.Status.Conditions,
		conditionType,
		provisioningv1alpha1.ConditionReason(reason),
		status,
		message)
	t.logger.InfoContext(ctx, fmt.Sprintf("NodeAllocationRequest (%s) %s status: %s",
		nodeAllocationRequest.GetName(), utils.GetStatusMessage(condition), message))

	// Update the CR status for the ProvisioningRequest.
	if err = utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		err = fmt.Errorf("failed to update Hardware %s status: %w", utils.GetStatusMessage(condition), err)
	}
	return status == metav1.ConditionTrue, timedOutOrFailed, err
}

// checkExistingNodeAllocationRequest checks for an existing NodeAllocationRequest and verifies changes if necessary
func (t *provisioningRequestReconcilerTask) checkExistingNodeAllocationRequest(ctx context.Context, clusterInstance *unstructured.Unstructured,
	hwTemplate *hwv1alpha1.HardwareTemplate, nodeAllocationRequest *hwv1alpha1.NodeAllocationRequest) error {

	ns := utils.GetHwMgrPluginNS()
	exist, err := utils.DoesK8SResourceExist(ctx, t.client, clusterInstance.GetName(), ns, nodeAllocationRequest)
	if err != nil {
		return fmt.Errorf("failed to get NodeAllocationRequest %s in namespace %s: %w", clusterInstance.GetName(), ns, err)
	}

	if exist {
		_, err := utils.CompareHardwareTemplateWithNodeAllocationRequest(hwTemplate, nodeAllocationRequest)
		if err != nil {
			return utils.NewInputError("%w", err)
		}
	}

	return nil
}

// buildNodeAllocationRequestSpec builds the NodeAllocationRequest spec based on the templates and cluster instance
func (t *provisioningRequestReconcilerTask) buildNodeAllocationRequestSpec(clusterInstance *unstructured.Unstructured,
	hwTemplate *hwv1alpha1.HardwareTemplate, nodeAllocationRequest *hwv1alpha1.NodeAllocationRequest) error {

	roleCounts := make(map[string]int)
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
		roleCounts[role]++
	}

	nodeGroups := []hwv1alpha1.NodeGroup{}
	for _, group := range hwTemplate.Spec.NodeGroupData {
		nodeGroup := utils.NewNodeGroup(group, roleCounts)
		nodeGroups = append(nodeGroups, nodeGroup)
	}

	siteID, err := provisioningv1alpha1.ExtractMatchingInput(
		t.object.Spec.TemplateParameters.Raw, utils.TemplateParamOCloudSiteId)
	if err != nil {
		return fmt.Errorf("failed to get %s from templateParameters: %w", utils.TemplateParamOCloudSiteId, err)
	}

	nodeAllocationRequest.Spec.CloudID = clusterInstance.GetName()
	nodeAllocationRequest.Spec.Site = siteID.(string)
	nodeAllocationRequest.Spec.HwMgrId = hwTemplate.Spec.HwMgrId
	nodeAllocationRequest.Spec.Extensions = hwTemplate.Spec.Extensions
	nodeAllocationRequest.Spec.NodeGroup = nodeGroups
	nodeAllocationRequest.ObjectMeta.Name = clusterInstance.GetName()
	nodeAllocationRequest.ObjectMeta.Namespace = utils.GetHwMgrPluginNS()

	// Add boot interface label annotation to the generated nodeAllocationRequest
	utils.SetNodeAllocationRequestAnnotations(nodeAllocationRequest, hwv1alpha1.BootInterfaceLabelAnnotation, hwTemplate.Spec.BootInterfaceLabel)
	// Add ProvisioningRequest labels to the generated nodeAllocationRequest
	utils.SetNodeAllocationRequestLabels(nodeAllocationRequest, provisioningv1alpha1.ProvisioningRequestNameLabel, t.object.Name)

	return nil
}

func (t *provisioningRequestReconcilerTask) handleRenderHardwareTemplate(ctx context.Context,
	clusterInstance *unstructured.Unstructured) (*hwv1alpha1.NodeAllocationRequest, error) {

	nodeAllocationRequest := &hwv1alpha1.NodeAllocationRequest{}

	clusterTemplate, err := t.object.GetClusterTemplateRef(ctx, t.client)
	if err != nil {
		return nil, fmt.Errorf("failed to get the ClusterTemplate for ProvisioningRequest %s: %w ", t.object.Name, err)
	}

	hwTemplateName := clusterTemplate.Spec.Templates.HwTemplate
	hwTemplate, err := utils.GetHardwareTemplate(ctx, t.client, hwTemplateName)
	if err != nil {
		return nil, fmt.Errorf("failed to get the HardwareTemplate %s resource: %w ", hwTemplateName, err)
	}

	if err := t.checkExistingNodeAllocationRequest(ctx, clusterInstance, hwTemplate, nodeAllocationRequest); err != nil {
		if utils.IsInputError(err) {
			updateErr := utils.UpdateHardwareTemplateStatusCondition(ctx, t.client, hwTemplate, provisioningv1alpha1.ConditionType(hwv1alpha1.Validation),
				provisioningv1alpha1.ConditionReason(hwv1alpha1.Failed), metav1.ConditionFalse, err.Error())
			if updateErr != nil {
				// nolint: wrapcheck
				return nil, updateErr
			}
		}
		return nil, err
	}

	hwmgr := &pluginv1alpha1.HardwareManager{}
	if err := t.client.Get(ctx, types.NamespacedName{Namespace: utils.GetHwMgrPluginNS(), Name: hwTemplate.Spec.HwMgrId}, hwmgr); err != nil {
		updateErr := utils.UpdateHardwareTemplateStatusCondition(ctx, t.client, hwTemplate, provisioningv1alpha1.ConditionType(hwv1alpha1.Validation),
			provisioningv1alpha1.ConditionReason(hwv1alpha1.Failed), metav1.ConditionFalse,
			"Unable to find specified HardwareManager: "+hwTemplate.Spec.HwMgrId)
		if updateErr != nil {
			return nil, fmt.Errorf("failed to update hwtemplate %s status: %w", hwTemplateName, updateErr)
		}
		return nil, fmt.Errorf("could not find specified HardwareManager: %s/%s, err=%w", utils.GetHwMgrPluginNS(), hwTemplate.Spec.HwMgrId, err)
	}

	// The HardwareTemplate is validated by the CRD schema and no additional validation is needed
	updateErr := utils.UpdateHardwareTemplateStatusCondition(ctx, t.client, hwTemplate, provisioningv1alpha1.ConditionType(hwv1alpha1.Validation),
		provisioningv1alpha1.ConditionReason(hwv1alpha1.Completed), metav1.ConditionTrue, "Validated")
	if updateErr != nil {
		// nolint: wrapcheck
		return nil, updateErr
	}

	if err := t.buildNodeAllocationRequestSpec(clusterInstance, hwTemplate, nodeAllocationRequest); err != nil {
		return nil, err
	}

	return nodeAllocationRequest, nil
}
