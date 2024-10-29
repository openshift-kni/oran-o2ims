package controllers

import (
	"context"
	"fmt"
	"log/slog"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
)

func (t *provisioningRequestReconcilerTask) createNodePoolResources(ctx context.Context, nodePool *hwv1alpha1.NodePool) error {
	// Create the hardware plugin namespace.
	pluginNameSpace := nodePool.ObjectMeta.Namespace
	if exists, err := utils.HwMgrPluginNamespaceExists(ctx, t.client, pluginNameSpace); err != nil {
		return fmt.Errorf("failed check if hardware manager plugin namespace exists %s, err: %w", pluginNameSpace, err)
	} else if !exists && pluginNameSpace == utils.UnitTestHwmgrNamespace {
		// TODO: For test purposes only. Code to be removed once hwmgr plugin(s) are fully utilized
		createErr := utils.CreateHwMgrPluginNamespace(ctx, t.client, pluginNameSpace)
		if createErr != nil {
			return fmt.Errorf(
				"failed to create hardware manager plugin namespace %s, err: %w", pluginNameSpace, createErr)
		}
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
	err = utils.SetCloudManagerGenerationStatus(ctx, t.client, nodePool)
	if err != nil {
		return fmt.Errorf("failed to set CloudManager's ObservedGeneration: %w", err)
	}
	return nil
}

// waitForHardwareData waits for the NodePool to be provisioned and update BMC details
// and bootMacAddress in ClusterInstance.
func (t *provisioningRequestReconcilerTask) waitForHardwareData(ctx context.Context,
	clusterInstance *siteconfig.ClusterInstance, nodePool *hwv1alpha1.NodePool) (bool, bool, error) {

	provisioned, timedOutOrFailed, err := t.checkNodePoolProvisionStatus(ctx, nodePool)
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
			err = fmt.Errorf("failed to update the rendered cluster instance: %w", err)
		}
	}
	return provisioned, timedOutOrFailed, err
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

	if err := t.applyNodeConfiguration(ctx, hwNodes, nodePool, clusterInstance); err != nil {
		return fmt.Errorf("failed to apply node config to the cluster instance: %w", err)
	}

	return nil
}

// checkNodePoolProvisionStatus checks for the NodePool status to be in the provisioned state.
func (t *provisioningRequestReconcilerTask) checkNodePoolProvisionStatus(ctx context.Context,
	nodePool *hwv1alpha1.NodePool) (bool, bool, error) {

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
	provisioned, timedOutOrFailed, err := t.updateHardwareProvisioningStatus(ctx, nodePool)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to update the NodePool status for ProvisioningRequest",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)
	}

	return provisioned, timedOutOrFailed, err
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

// updateHardwareProvisioningStatus updates the status for the ProvisioningRequest
func (t *provisioningRequestReconcilerTask) updateHardwareProvisioningStatus(
	ctx context.Context, nodePool *hwv1alpha1.NodePool) (bool, bool, error) {
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
	if t.object.Status.NodePoolRef.HardwareProvisioningCheckStart.IsZero() {
		t.object.Status.NodePoolRef.HardwareProvisioningCheckStart = metav1.Now()
	}

	provisionedCondition := meta.FindStatusCondition(
		nodePool.Status.Conditions, string(hwv1alpha1.Provisioned))
	if provisionedCondition != nil {
		status = provisionedCondition.Status
		reason = provisionedCondition.Reason
		message = provisionedCondition.Message

		if provisionedCondition.Status == metav1.ConditionFalse && reason == string(hwv1alpha1.Failed) {
			t.logger.InfoContext(
				ctx,
				fmt.Sprintf(
					"NodePool %s in the namespace %s provisioning failed",
					nodePool.GetName(),
					nodePool.GetNamespace(),
				),
			)
			// Ensure a consistent message for the provisioning request, regardless of which plugin is used.
			message = "Hardware provisioning failed"
			timedOutOrFailed = true
			utils.SetProvisioningStateFailed(t.object, message)
		}
	} else {
		// No provisioning condition found, set the status to unknown.
		status = metav1.ConditionUnknown
		reason = string(utils.CRconditionReasons.Unknown)
		message = "Unknown state of hardware provisioning"
		utils.SetProvisioningStateInProgress(t.object, message)
	}

	// Check for timeout if not already failed or provisioned
	if status != metav1.ConditionTrue && reason != string(hwv1alpha1.Failed) {
		if utils.TimeoutExceeded(
			t.object.Status.NodePoolRef.HardwareProvisioningCheckStart.Time,
			t.timeouts.hardwareProvisioning) {
			t.logger.InfoContext(
				ctx,
				fmt.Sprintf(
					"NodePool %s in the namespace %s provisioning timed out",
					nodePool.GetName(),
					nodePool.GetNamespace(),
				),
			)
			reason = string(hwv1alpha1.TimedOut)
			message = "Hardware provisioning timed out"
			status = metav1.ConditionFalse
			timedOutOrFailed = true
			utils.SetProvisioningStateFailed(t.object, message)
		} else {
			utils.SetProvisioningStateInProgress(t.object, "Hardware provisioning is in progress")
		}
	}

	// Set the status condition for hardware provisioning.
	utils.SetStatusCondition(&t.object.Status.Conditions,
		utils.PRconditionTypes.HardwareProvisioned,
		utils.ConditionReason(reason),
		status,
		message)

	// Update the CR status for the ProvisioningRequest.
	if err = utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		err = fmt.Errorf("failed to update HardwareProvisioning status: %w", err)
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

	siteId, err := utils.ExtractMatchingInput(
		t.object.Spec.TemplateParameters.Raw, utils.TemplateParamOCloudSiteId)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s from templateParameters: %w", utils.TemplateParamOCloudSiteId, err)
	}

	nodePool.Spec.CloudID = clusterInstance.GetName()
	nodePool.Spec.Site = siteId.(string)
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
