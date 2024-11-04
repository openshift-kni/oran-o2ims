package controllers

import (
	"context"
	"fmt"
	"log/slog"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
)

func (t *provisioningRequestReconcilerTask) createOrUpdateNodePool(ctx context.Context, nodePool *hwv1alpha1.NodePool) error {

	existingNodePool := &hwv1alpha1.NodePool{}

	exist, err := utils.DoesK8SResourceExist(ctx, t.client, nodePool.Name, nodePool.Namespace, existingNodePool)
	if err != nil {
		return fmt.Errorf("failed to get NodePool %s in namespace %s: %w", nodePool.GetName(), nodePool.GetNamespace(), err)
	}

	if !exist {
		return t.createNodePoolResources(ctx, nodePool)
	}

	// The template validate is already completed; compare NodeGroup and update them if necessary
	if !equality.Semantic.DeepEqual(existingNodePool.Spec.NodeGroup, nodePool.Spec.NodeGroup) {
		// Only process the configuration changes
		patch := client.MergeFrom(existingNodePool.DeepCopy())
		// Update the spec field with the new data
		existingNodePool.Spec = nodePool.Spec
		// Apply the patch to update the NodePool with the new spec
		if err = t.client.Patch(ctx, existingNodePool, patch); err != nil {
			return fmt.Errorf("failed to patch NodePool %s in namespace %s: %w", nodePool.GetName(), nodePool.GetNamespace(), err)
		}

		// After successful patch, update the status
		existingNodePool.Status.CloudManager.ObservedGeneration = existingNodePool.ObjectMeta.Generation
		err := utils.UpdateNodePoolStatus(ctx, t.client, existingNodePool, hwv1alpha1.Configured, metav1.ConditionFalse,
			hwv1alpha1.ConfigUpdate, hwv1alpha1.AwaitConfig)
		if err != nil {
			return fmt.Errorf("failed to update status of NodePool %s in namespace %s: %w", nodePool.GetName(), nodePool.GetNamespace(), err)
		}
		t.logger.InfoContext(
			ctx,
			fmt.Sprintf(
				"NodePool %s in the namespace %s configuration changes have been detected",
				nodePool.GetName(),
				nodePool.GetNamespace(),
			),
		)
	}
	return nil
}

func (t *provisioningRequestReconcilerTask) createNodePoolResources(ctx context.Context, nodePool *hwv1alpha1.NodePool) error {
	// Create the hardware plugin namespace.
	pluginNameSpace := nodePool.ObjectMeta.Namespace
	if exists, err := utils.HwMgrPluginNamespaceExists(ctx, t.client, pluginNameSpace); err != nil {
		return fmt.Errorf("failed check if hardware manager plugin namespace exists %s, err: %w", pluginNameSpace, err)
	} else if !exists {
		return fmt.Errorf("specified hardware manager plugin namespace does not exist: %s", pluginNameSpace)
	}

	// Create/update the clusterInstance namespace, adding ProvisioningRequest labels to the namespace
	err := t.createClusterInstanceNamespace(ctx, nodePool.GetName())
	if err != nil {
		return err
	}

	// Create the node pool resource
	createErr := utils.CreateK8sCR(ctx, t.client, nodePool, t.object, "")
	if createErr != nil {
		t.logger.ErrorContext(
			ctx,
			fmt.Sprintf(
				"Failed to create the NodePool %s in the namespace %s",
				nodePool.GetName(),
				nodePool.GetNamespace(),
			),
			slog.String("error", createErr.Error()),
		)
		return fmt.Errorf("failed to create/update the NodePool: %s", createErr.Error())
	}

	// Set NodePoolRef
	if t.object.Status.NodePoolRef == nil {
		t.object.Status.NodePoolRef = &provisioningv1alpha1.NodePoolRef{}
	}
	t.object.Status.NodePoolRef.Name = nodePool.GetName()
	t.object.Status.NodePoolRef.Namespace = nodePool.GetNamespace()

	t.logger.InfoContext(
		ctx,
		fmt.Sprintf(
			"Created NodePool %s in the namespace %s, if not already exist",
			nodePool.GetName(),
			nodePool.GetNamespace(),
		),
	)
	// Set the CloudManager's ObservedGeneration on the node pool resource status field
	err = utils.SetCloudManagerInitialObservedGeneration(ctx, t.client, nodePool)
	if err != nil {
		return fmt.Errorf("failed to set CloudManager's ObservedGeneration: %w", err)
	}
	return nil
}

// waitForHardwareData waits for the NodePool to be provisioned and update BMC details
// and bootMacAddress in ClusterInstance.
func (t *provisioningRequestReconcilerTask) waitForHardwareData(ctx context.Context,
	clusterInstance *siteconfig.ClusterInstance, nodePool *hwv1alpha1.NodePool) (bool, *bool, bool, error) {

	var configured *bool
	provisioned, timedOutOrFailed, err := t.checkNodePoolProvisionStatus(ctx, clusterInstance, nodePool)
	if err != nil {
		return provisioned, nil, timedOutOrFailed, err
	}
	if provisioned {
		configured, timedOutOrFailed, err = t.checkNodePoolConfigStatus(ctx, nodePool)
	}
	return provisioned, configured, timedOutOrFailed, err
}

// updateClusterInstance updates the given ClusterInstance object based on the provisioned nodePool.
func (t *provisioningRequestReconcilerTask) updateClusterInstance(ctx context.Context,
	clusterInstance *siteconfig.ClusterInstance, nodePool *hwv1alpha1.NodePool) error {

	hwNodes, err := utils.CollectNodeDetails(ctx, t.client, nodePool)
	if err != nil {
		return fmt.Errorf("failed to collect hardware node %s details for node pool: %w", nodePool.GetName(), err)
	}

	if err := utils.CopyBMCSecrets(ctx, t.client, hwNodes, nodePool); err != nil {
		return fmt.Errorf("failed to copy BMC secret: %w", err)
	}

	configErr := t.applyNodeConfiguration(ctx, hwNodes, nodePool, clusterInstance)
	if configErr != nil {
		msg := "Failed to apply node configuration to the rendered ClusterInstance: " + configErr.Error()
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.HardwareNodeConfigApplied,
			utils.CRconditionReasons.NotApplied,
			metav1.ConditionFalse,
			msg)
		utils.SetProvisioningStateFailed(t.object, msg)
	} else {
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.HardwareNodeConfigApplied,
			utils.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"Node configuration has been applied to the rendered ClusterInstance")
	}

	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
	}

	if configErr != nil {
		return fmt.Errorf("failed to apply node configuration for NodePool %s: %w", nodePool.GetName(), err)
	}
	return nil
}

// checkNodePoolStatus checks the NodePool status of a given condition type
// and updates the provisioning request status accordingly.
func (t *provisioningRequestReconcilerTask) checkNodePoolStatus(ctx context.Context,
	nodePool *hwv1alpha1.NodePool, condition hwv1alpha1.ConditionType) (bool, bool, error) {

	// Get the generated NodePool and its status.
	exists, err := utils.DoesK8SResourceExist(ctx, t.client, nodePool.GetName(),
		nodePool.GetNamespace(), nodePool)

	if err != nil {
		return false, false, fmt.Errorf("failed to get node pool; %w", err)
	}
	if !exists {
		return false, false, fmt.Errorf("node pool does not exist")
	}

	// Update the provisioning request Status with status from the NodePool object.
	status, timedOutOrFailed, err := t.updateHardwareStatus(ctx, nodePool, condition)
	if err != nil && !utils.IsConditionDoesNotExistsErr(err) {
		t.logger.ErrorContext(
			ctx,
			"Failed to update the NodePool status for ProvisioningRequest",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)
	}

	return status, timedOutOrFailed, err
}

// checkNodePoolProvisionStatus checks the provisioned status of the node pool.
func (t *provisioningRequestReconcilerTask) checkNodePoolProvisionStatus(ctx context.Context,
	clusterInstance *siteconfig.ClusterInstance, nodePool *hwv1alpha1.NodePool) (bool, bool, error) {

	provisioned, timedOutOrFailed, err := t.checkNodePoolStatus(ctx, nodePool, hwv1alpha1.Provisioned)
	if provisioned && err == nil {
		t.logger.InfoContext(
			ctx,
			fmt.Sprintf(
				"NodePool %s in the namespace %s is provisioned",
				nodePool.GetName(),
				nodePool.GetNamespace(),
			),
		)
		if err = t.updateClusterInstance(ctx, clusterInstance, nodePool); err != nil {
			return provisioned, timedOutOrFailed, fmt.Errorf("failed to update the rendered cluster instance: %w", err)
		}
	}

	return provisioned, timedOutOrFailed, err
}

// checkNodePoolConfigStatus checks the configured status of the node pool.
func (t *provisioningRequestReconcilerTask) checkNodePoolConfigStatus(ctx context.Context, nodePool *hwv1alpha1.NodePool) (*bool, bool, error) {

	status, timedOutOrFailed, err := t.checkNodePoolStatus(ctx, nodePool, hwv1alpha1.Configured)
	if err != nil {
		if utils.IsConditionDoesNotExistsErr(err) {
			// Condition does not exist, return nil (acceptable case)
			return nil, timedOutOrFailed, nil
		}
		return nil, timedOutOrFailed, fmt.Errorf("failed to check NodePool configured status: %w", err)
	}
	return &status, timedOutOrFailed, nil
}

// applyNodeConfiguration updates the clusterInstance with BMC details, interface MACAddress and bootMACAddress
func (t *provisioningRequestReconcilerTask) applyNodeConfiguration(ctx context.Context, hwNodes map[string][]utils.NodeInfo,
	nodePool *hwv1alpha1.NodePool, clusterInstance *siteconfig.ClusterInstance) error {

	for i, node := range clusterInstance.Spec.Nodes {
		// Check if the node's role matches any key in hwNodes
		nodeInfos, exists := hwNodes[node.Role]
		if !exists || len(nodeInfos) == 0 {
			continue
		}

		clusterInstance.Spec.Nodes[i].BmcAddress = nodeInfos[0].BmcAddress
		clusterInstance.Spec.Nodes[i].BmcCredentialsName = siteconfig.BmcCredentialsName{Name: nodeInfos[0].BmcCredentials}
		// Get the boot MAC address based on the interface label
		bootMAC, err := utils.GetBootMacAddress(nodeInfos[0].Interfaces, nodePool)
		if err != nil {
			return fmt.Errorf("failed to get the node boot MAC address: %w", err)
		}
		clusterInstance.Spec.Nodes[i].BootMACAddress = bootMAC

		// Populate the MAC address for each interface
		if err := utils.AssignMacAddress(t.clusterInput.clusterInstanceData, nodeInfos[0].Interfaces, &clusterInstance.Spec.Nodes[i]); err != nil {
			return fmt.Errorf("failed to assign mac address:  %w", err)
		}

		// Indicates which host has been assigned to the node
		if err := utils.UpdateNodeStatusWithHostname(ctx, t.client, nodeInfos[0].NodeName, node.HostName,
			nodePool.Namespace); err != nil {
			return fmt.Errorf("failed to update the node status: %w", err)
		}
		hwNodes[node.Role] = nodeInfos[1:]
	}
	return nil
}

// updateHardwareStatus updates the hardware status for the ProvisioningRequest
func (t *provisioningRequestReconcilerTask) updateHardwareStatus(
	ctx context.Context, nodePool *hwv1alpha1.NodePool, condition hwv1alpha1.ConditionType) (bool, bool, error) {
	var status metav1.ConditionStatus
	var reason string
	var message string
	var err error
	timedOutOrFailed := false // Default to false unless explicitly needed

	if t.object.Status.NodePoolRef == nil {
		t.object.Status.NodePoolRef = &provisioningv1alpha1.NodePoolRef{}
	}

	t.object.Status.NodePoolRef.Name = nodePool.GetName()
	t.object.Status.NodePoolRef.Namespace = nodePool.GetNamespace()

	if condition == hwv1alpha1.Provisioned {
		if t.object.Status.NodePoolRef.HardwareProvisioningCheckStart.IsZero() {
			t.object.Status.NodePoolRef.HardwareProvisioningCheckStart = metav1.Now()
		}
	}

	hwCondition := meta.FindStatusCondition(
		nodePool.Status.Conditions, string(condition))
	switch {
	case hwCondition != nil:
		status = hwCondition.Status
		reason = hwCondition.Reason
		message = hwCondition.Message

		if condition == hwv1alpha1.Configured {
			if t.object.Status.NodePoolRef.HardwareConfiguringCheckStart.IsZero() {
				t.object.Status.NodePoolRef.HardwareConfiguringCheckStart = metav1.Now()
			}
		}

		// Reset the status check start time for the next configuration changes
		if hwCondition.Type == string(hwv1alpha1.Configured) && hwCondition.Status == metav1.ConditionTrue {
			t.object.Status.NodePoolRef.HardwareConfiguringCheckStart = metav1.Time{}
		}

		if hwCondition.Status == metav1.ConditionFalse && reason == string(hwv1alpha1.Failed) {
			t.logger.InfoContext(
				ctx,
				fmt.Sprintf(
					"NodePool %s in the namespace %s %s failed",
					nodePool.GetName(),
					nodePool.GetNamespace(),
					hwCondition.Type,
				),
			)
			// Ensure a consistent message for the provisioning request, regardless of which plugin is used.
			message = fmt.Sprintf("Hardware %s failed", utils.GetStatusMessage(condition))
			timedOutOrFailed = true
			utils.SetProvisioningStateFailed(t.object, message)
		}
	case condition == hwv1alpha1.Configured && hwCondition == nil:
		// Return a custom error if the configured condition does not exist, as it is optional
		return false, false, &utils.ConditionDoesNotExistsErr{ConditionName: string(condition)}
	default:
		// Condition not found, set the status to unknown.
		status = metav1.ConditionUnknown
		reason = string(utils.CRconditionReasons.Unknown)
		message = "Unknown state of hardware provisioning"
	}

	if status != metav1.ConditionTrue && reason != string(hwv1alpha1.Failed) {
		// Handle timeout logic
		timedOutOrFailed, reason, message = utils.HandleHardwareTimeout(
			condition,
			t.object.Status.NodePoolRef.HardwareProvisioningCheckStart,
			t.object.Status.NodePoolRef.HardwareConfiguringCheckStart,
			t.timeouts.hardwareProvisioning,
			reason,
			message,
		)
		if timedOutOrFailed {
			utils.SetProvisioningStateFailed(t.object, message)
		} else {
			message = fmt.Sprintf("Hardware %s is in progress", utils.GetStatusMessage(condition))
			utils.SetProvisioningStateInProgress(t.object, message)
		}
	}

	conditionType := utils.PRconditionTypes.HardwareProvisioned
	if condition == hwv1alpha1.Configured {
		conditionType = utils.PRconditionTypes.HardwareConfigured
	}

	// Set the status condition for hardware status.
	utils.SetStatusCondition(&t.object.Status.Conditions,
		conditionType,
		utils.ConditionReason(reason),
		status,
		message)

	// Update the CR status for the ProvisioningRequest.
	if err = utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		err = fmt.Errorf("failed to update Hardware %s status: %w", utils.GetStatusMessage(condition), err)
	}
	return status == metav1.ConditionTrue, timedOutOrFailed, err
}

func (t *provisioningRequestReconcilerTask) handleRenderHardwareTemplate(ctx context.Context,
	clusterInstance *siteconfig.ClusterInstance) (*hwv1alpha1.NodePool, error) {

	nodePool := &hwv1alpha1.NodePool{}

	clusterTemplate, err := t.getCrClusterTemplateRef(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get the ClusterTemplate for ProvisioningRequest %s: %w ", t.object.Name, err)
	}

	hwTemplateCmName := clusterTemplate.Spec.Templates.HwTemplate
	hwTemplateCm, err := utils.GetConfigmap(ctx, t.client, hwTemplateCmName, utils.InventoryNamespace)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get the %s configmap for Hardware Template, err: %w", hwTemplateCmName, err)
	}

	nodeGroup, err := utils.ExtractTemplateDataFromConfigMap[[]hwv1alpha1.NodeGroup](
		hwTemplateCm, utils.HwTemplateNodePool)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get the Hardware template from ConfigMap %s, err: %w", hwTemplateCmName, err)
	}

	// Check if the NodePool exists
	ns := utils.GetHwMgrPluginNS()
	exist, err := utils.DoesK8SResourceExist(ctx, t.client, clusterInstance.GetName(), ns, nodePool)
	if err != nil {
		return nodePool, fmt.Errorf("failed to get NodePool %s in namespace %s: %w", clusterInstance.GetName(), ns, err)
	}

	if exist {
		// Verify the changes
		changed, err := utils.CompareConfigMapWithNodePool(hwTemplateCm, nodePool, nodeGroup)
		if err != nil {
			return nodePool, utils.NewInputError("%w", err)
		}
		if changed && !utils.IsProvisioningStateFulfilled(t.object) {
			return nodePool, utils.NewInputError("hardware template changes are not allowed until the cluster provisioning is fulfilled")
		}
	}

	roleCounts := make(map[string]int)
	err = utils.ProcessClusterNodeGroups(clusterInstance, nodeGroup, roleCounts)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get the process node spec err: %w", err)
	}

	for i, group := range nodeGroup {
		if count, ok := roleCounts[group.Name]; ok {
			nodeGroup[i].Size = count
		}
	}

	siteID, err := utils.ExtractMatchingInput(
		t.object.Spec.TemplateParameters.Raw, utils.TemplateParamOCloudSiteId)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s from templateParameters: %w", utils.TemplateParamOCloudSiteId, err)
	}

	nodePool.Spec.CloudID = clusterInstance.GetName()
	nodePool.Spec.Site = siteID.(string)
	nodePool.Spec.HwMgrId = hwTemplateCm.Data[utils.HwTemplatePluginMgr]
	nodePool.Spec.NodeGroup = nodeGroup
	nodePool.ObjectMeta.Name = clusterInstance.GetName()
	nodePool.ObjectMeta.Namespace = utils.GetHwMgrPluginNS()

	// Add boot interface label to the generated nodePool
	annotation := make(map[string]string)
	annotation[utils.HwTemplateBootIfaceLabel] = hwTemplateCm.Data[utils.HwTemplateBootIfaceLabel]
	nodePool.SetAnnotations(annotation)

	// Add ProvisioningRequest labels to the generated nodePool
	labels := make(map[string]string)
	labels[provisioningRequestNameLabel] = t.object.Name
	nodePool.SetLabels(labels)
	return nodePool, nil
}
