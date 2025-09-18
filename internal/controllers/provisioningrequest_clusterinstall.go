/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	disableAutoImportAnnotation = "import.open-cluster-management.io/disable-auto-import"
)

// nodeInfo represents the node details extracted
// from the existing ClusterInstance
type nodeInfo struct {
	bmcAddress          *string
	bootMACAddress      *string
	bmcCredentialsName  *string
	HwMgrNodeId         string
	HwMgrNodeNs         string
	interfaceMACAddress map[string]string // keyed by interface name
}

func (t *provisioningRequestReconcilerTask) buildClusterInstance(
	ctx context.Context) (*siteconfig.ClusterInstance, error) {
	t.logger.InfoContext(
		ctx,
		"Rendering the ClusterInstance template for ProvisioningRequest",
		slog.String("name", t.object.Name),
	)

	// Build an initial unstructured ClusterInstance using the merged ClusterInstance data
	// and default values.
	renderedCIUnstructured, err := t.buildClusterInstanceUnstructured()
	if err != nil {
		return nil, fmt.Errorf("failed to build unstructured ClusterInstance %s: %w", t.clusterInput.clusterInstanceData["clusterName"].(string), err)
	}

	// Create the ClusterInstance namespace if it does not exist.
	ciName := renderedCIUnstructured.GetName()
	err = t.createClusterInstanceNamespace(ctx, ciName)
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster namespace %s: %w", ciName, err)
	}

	// We want to add the disable-auto-import annotation to the
	// rendered ClusterInstance until the cluster installation
	// is marked as completed.
	if !ctlrutils.IsClusterProvisionCompleted(t.object) {
		err = addDisableAutoImportAnnotation(renderedCIUnstructured)
		if err != nil {
			return nil, fmt.Errorf(
				"failed to add disable-auto-import annotation to the rendered ClusterInstance (%s): %w",
				renderedCIUnstructured.GetName(), err)
		}
	}

	existingCIUnstructured := &unstructured.Unstructured{}
	existingCIUnstructured.SetGroupVersionKind(renderedCIUnstructured.GroupVersionKind())
	ciExists, err := ctlrutils.DoesK8SResourceExist(ctx, t.client, ciName, ciName, existingCIUnstructured)
	if err != nil {
		return nil, fmt.Errorf("failed to get ClusterInstance (%s): %w", ciName, err)
	}
	if ciExists {
		// Extract node details from the existing ClusterInstance and assign them to the rendered ClusterInstance
		// when hardware provisioning is enabled.
		if !t.isHardwareProvisionSkipped() {
			nodesInfo := extractNodeDetails(existingCIUnstructured)
			assignNodeDetails(renderedCIUnstructured, nodesInfo)
		}
	}

	// Handle cluster upgrade transformations before validation
	if ciExists && ctlrutils.IsClusterProvisionCompleted(t.object) {
		err = t.handleClusterInstanceUpgrade(existingCIUnstructured, renderedCIUnstructured)
		if err != nil {
			return nil, fmt.Errorf("failed to handle cluster instance upgrade: %w", err)
		}
	}

	// Validate the rendered ClusterInstance with dry-run. The defaults defined in the
	// ClusterInstance CRD will be applied by the APIserver after the dry-run.
	// NOTE: ClusterInstance immutable field validation is handled by ACM 2.13+
	// admission webhook, so no additional validation is needed here.
	isDryRun := true
	err = t.applyClusterInstance(ctx, renderedCIUnstructured, isDryRun)
	if err != nil {
		return nil, fmt.Errorf("failed to validate the rendered ClusterInstance with dry-run: %w", err)
	}

	// Convert unstructured to siteconfig.ClusterInstance type
	renderedCI := &siteconfig.ClusterInstance{}
	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(
		renderedCIUnstructured.Object, renderedCI); err != nil {
		// Unlikely to happen since dry-run validation has passed
		return nil, ctlrutils.NewInputError("failed to convert to siteconfig.ClusterInstance type: %w", err)
	}

	return renderedCI, nil
}

// buildClusterInstanceUnstructured creates a ClusterInstance in an unstructured format using both placeholder
// values and configuration values from t.clusterInput.clusterInstanceData.
func (t *provisioningRequestReconcilerTask) buildClusterInstanceUnstructured() (*unstructured.Unstructured, error) {

	renderedClusterInstanceUnstructured := &unstructured.Unstructured{}
	// Set the GVK and metadata.
	renderedClusterInstanceUnstructured.SetAPIVersion(fmt.Sprintf("%s/%s", siteconfig.Group, siteconfig.Version))
	renderedClusterInstanceUnstructured.SetKind(siteconfig.ClusterInstanceKind)
	renderedClusterInstanceUnstructured.SetName(t.clusterInput.clusterInstanceData["clusterName"].(string))
	renderedClusterInstanceUnstructured.SetNamespace(t.clusterInput.clusterInstanceData["clusterName"].(string))

	// Set the spec to the value obtained from the merge of the ClusterInstance default ConfigMap and
	// the ProvisioningRequest ClusterInstance input.
	err := unstructured.SetNestedField(renderedClusterInstanceUnstructured.Object, t.clusterInput.clusterInstanceData, "spec")
	if err != nil {
		return nil, fmt.Errorf(
			"failed to set the unstructured spec for the %s(%s) CRD: %w",
			renderedClusterInstanceUnstructured.GetName(), renderedClusterInstanceUnstructured.GetNamespace(), err)
	}

	// Override the hardware properties with placeholders if they are empty, as they will come
	// from the HW Manager plugin if they have not been set through the ProvisioningRequest.
	// If hardware provisioning is disabled, then the values should have been set, so they will not be changed here.
	nodes := renderedClusterInstanceUnstructured.Object["spec"].(map[string]interface{})["nodes"].([]interface{})

	for _, node := range nodes {
		// Set placeholders for BMC details.
		nodeMap := node.(map[string]interface{})
		if value, ok := nodeMap["bmcAddress"]; !ok || value == "" {
			nodeMap["bmcAddress"] = "placeholder"
		}
		if value, ok := nodeMap["bootMACAddress"]; !ok || value == "" {
			nodeMap["bootMACAddress"] = "00:00:5E:00:53:AF"
		}
		if value, ok := nodeMap["bmcCredentialsName"]; !ok || value == "" {
			secretName, err := ctlrutils.GenerateSecretName(nodeMap, renderedClusterInstanceUnstructured.GetName())
			if err != nil {
				return nil, fmt.Errorf("failed to generate Secret name: %w", err)
			}
			nodeMap["bmcCredentialsName"] = map[string]interface{}{
				"name": secretName,
			}
		}
		if nodeNetwork, ok := nodeMap["nodeNetwork"]; ok {
			if interfaces, ok := nodeNetwork.(map[string]interface{})["interfaces"]; ok {
				if interfaceItems, ok := interfaces.([]interface{}); ok {
					for _, interfaceItem := range interfaceItems {
						interfaceMap := interfaceItem.(map[string]interface{})
						if value, ok := interfaceMap["macAddress"]; !ok || value == "" {
							interfaceMap["macAddress"] = "00:00:5E:00:53:AF"
						}
					}
				}
			}
		}
	}

	// Add ProvisioningRequest labels to the generated ClusterInstance.
	labels := make(map[string]string)
	labels[provisioningv1alpha1.ProvisioningRequestNameLabel] = t.object.Name
	renderedClusterInstanceUnstructured.SetLabels(labels)

	return renderedClusterInstanceUnstructured, nil
}

// handleClusterInstallation creates/updates the ClusterInstance to handle the cluster provisioning.
func (t *provisioningRequestReconcilerTask) handleClusterInstallation(ctx context.Context, clusterInstance *unstructured.Unstructured) error {
	isDryRun := false
	err := t.applyClusterInstance(ctx, clusterInstance, isDryRun)
	if err != nil {
		return fmt.Errorf("failed to apply the rendered ClusterInstance (%s): %s", clusterInstance.GetName(), err.Error())

	} else {
		// Set ClusterDetails
		if t.object.Status.Extensions.ClusterDetails == nil {
			t.object.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{}
		}
		t.object.Status.Extensions.ClusterDetails.Name = clusterInstance.GetName()
	}

	// Continue checking the existing ClusterInstance provision status
	if err := t.checkClusterProvisionStatus(ctx, clusterInstance.GetName()); err != nil {
		return err
	}

	// Remove the disable-auto-import annotation for the managed cluster
	// if the cluster provisioning is completed.
	if ctlrutils.IsClusterProvisionCompleted(t.object) {
		return t.removeDisableAutoImportAnnotation(ctx, clusterInstance)
	}

	return nil
}

// removeDisableAutoImportAnnotation removes the disable-auto-import annotation
// from the ManagedCluster if it exists.
func (t *provisioningRequestReconcilerTask) removeDisableAutoImportAnnotation(
	ctx context.Context, ci *unstructured.Unstructured) error {

	managedCluster := &clusterv1.ManagedCluster{}
	exists, err := ctlrutils.DoesK8SResourceExist(
		ctx, t.client, ci.GetName(), "", managedCluster)
	if err != nil {
		return fmt.Errorf("failed to check if ManagedCluster exists: %w", err)
	}
	if exists {
		if _, ok := managedCluster.GetAnnotations()[disableAutoImportAnnotation]; ok {
			delete(managedCluster.GetAnnotations(), disableAutoImportAnnotation)
			err = t.client.Update(ctx, managedCluster)
			if err != nil {
				return fmt.Errorf("failed to update managed cluster: %w", err)
			}
			t.logger.InfoContext(ctx,
				fmt.Sprintf("disable-auto-import annotation is removed for ManagedCluster: %s", t.object.Name))
		}
	}
	return nil
}

// checkClusterProvisionStatus checks the status of cluster provisioning
func (t *provisioningRequestReconcilerTask) checkClusterProvisionStatus(
	ctx context.Context, clusterInstanceName string) error {

	clusterInstance := &siteconfig.ClusterInstance{}
	if err := ctlrutils.RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
		exists, err := ctlrutils.DoesK8SResourceExist(ctx, t.client, clusterInstanceName, clusterInstanceName, clusterInstance)
		if err != nil {
			return fmt.Errorf("failed to get ClusterInstance %s: %w", clusterInstanceName, err)
		}
		if !exists {
			return fmt.Errorf("clusterInstance %s does not exist", clusterInstanceName)
		}
		return nil
	}); err != nil {
		// nolint: wrapcheck
		return err
	}
	// Check ClusterInstance status and update the corresponding ProvisioningRequest status conditions.
	t.updateClusterInstanceProcessedStatus(clusterInstance)
	t.updateClusterProvisionStatus(clusterInstance)

	if updateErr := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
	}

	return nil
}

func (t *provisioningRequestReconcilerTask) applyClusterInstance(ctx context.Context, clusterInstance client.Object, isDryRun bool) error {
	var operationType string

	existingClusterInstance := &unstructured.Unstructured{}

	existingClusterInstance.SetGroupVersionKind(clusterInstance.GetObjectKind().GroupVersionKind())

	err := t.client.Get(
		ctx,
		types.NamespacedName{
			Name:      clusterInstance.GetName(),
			Namespace: clusterInstance.GetNamespace(),
		},
		existingClusterInstance,
	)

	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get ClusterInstance: %w", err)
		}

		operationType = ctlrutils.OperationTypeCreated
		opts := []client.CreateOption{}
		if isDryRun {
			opts = append(opts, client.DryRunAll)
			operationType = ctlrutils.OperationTypeDryRun
		}

		err = ctrl.SetControllerReference(t.object, clusterInstance, t.client.Scheme())
		if err != nil {
			return fmt.Errorf("failed to set controller reference: %w", err)
		}

		if err = t.client.Create(ctx, clusterInstance, opts...); err != nil {
			if !errors.IsInvalid(err) && !errors.IsBadRequest(err) {
				return fmt.Errorf("failed to create ClusterInstance: %w", err)
			}
			return ctlrutils.NewInputError("%s", err.Error())
		}
	} else {
		// Compare spec fields of both unstructured objects
		newSpec, _, err := unstructured.NestedMap(clusterInstance.(*unstructured.Unstructured).Object, "spec")
		if err != nil {
			return fmt.Errorf("failed to extract spec from new object: %w", err)
		}

		existingSpec, _, err := unstructured.NestedMap(existingClusterInstance.Object, "spec")
		if err != nil {
			return fmt.Errorf("failed to extract spec from existing object: %w", err)
		}

		if equality.Semantic.DeepEqual(existingSpec, newSpec) {
			return nil
		}

		// Preserve metadata
		clusterInstance.SetResourceVersion(existingClusterInstance.GetResourceVersion())
		clusterInstance.SetFinalizers(existingClusterInstance.GetFinalizers())
		clusterInstance.SetLabels(existingClusterInstance.GetLabels())
		clusterInstance.SetAnnotations(existingClusterInstance.GetAnnotations())

		operationType = ctlrutils.OperationTypeUpdated
		opts := []client.PatchOption{}
		if isDryRun {
			opts = append(opts, client.DryRunAll)
			operationType = ctlrutils.OperationTypeDryRun
		}

		patch := client.MergeFrom(existingClusterInstance.DeepCopy())
		if err := t.client.Patch(ctx, clusterInstance, patch, opts...); err != nil {
			if !errors.IsInvalid(err) && !errors.IsBadRequest(err) {
				return fmt.Errorf("failed to patch ClusterInstance: %w", err)
			}
			return ctlrutils.NewInputError("%s", err.Error())
		}
	}

	t.logger.InfoContext(
		ctx,
		fmt.Sprintf(
			"Rendered ClusterInstance %s in the namespace %s %s",
			clusterInstance.GetName(),
			clusterInstance.GetNamespace(),
			operationType,
		),
	)
	return nil
}

func (t *provisioningRequestReconcilerTask) updateClusterInstanceProcessedStatus(ci *siteconfig.ClusterInstance) {
	if ci == nil {
		return
	}

	clusterInstanceConditionTypes := []siteconfig.ClusterInstanceConditionType{
		siteconfig.ClusterInstanceValidated,
		siteconfig.RenderedTemplates,
		siteconfig.RenderedTemplatesValidated,
		siteconfig.RenderedTemplatesApplied,
	}

	if len(ci.Status.Conditions) == 0 {
		message := fmt.Sprintf("Waiting for ClusterInstance (%s) to be processed", ci.Name)
		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed,
			provisioningv1alpha1.CRconditionReasons.Unknown,
			metav1.ConditionUnknown,
			message,
		)
		ctlrutils.SetProvisioningStateInProgress(t.object, message)
		return
	}

	for _, condType := range clusterInstanceConditionTypes {
		ciCondition := meta.FindStatusCondition(ci.Status.Conditions, string(condType))
		if ciCondition != nil && ciCondition.Status != metav1.ConditionTrue {
			ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
				provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed,
				provisioningv1alpha1.ConditionReason(ciCondition.Reason),
				ciCondition.Status,
				ciCondition.Message,
			)
			ctlrutils.SetProvisioningStateFailed(t.object, ciCondition.Message)
			return
		}
	}

	ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
		provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed,
		provisioningv1alpha1.CRconditionReasons.Completed,
		metav1.ConditionTrue,
		fmt.Sprintf("Applied and processed ClusterInstance (%s) successfully", ci.Name),
	)
}

func (t *provisioningRequestReconcilerTask) updateClusterProvisionStatus(ci *siteconfig.ClusterInstance) {
	if ci == nil {
		return
	}

	var message string

	// Search for ClusterInstance Provisioned condition
	ciProvisionedCondition := meta.FindStatusCondition(
		ci.Status.Conditions, string(hwmgmtv1alpha1.Provisioned))

	if ciProvisionedCondition == nil {
		crClusterInstanceProcessedCond := meta.FindStatusCondition(
			t.object.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed))
		if crClusterInstanceProcessedCond != nil && crClusterInstanceProcessedCond.Status == metav1.ConditionTrue {
			message = "Waiting for cluster installation to start"
			ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
				provisioningv1alpha1.PRconditionTypes.ClusterProvisioned,
				provisioningv1alpha1.CRconditionReasons.Unknown,
				metav1.ConditionUnknown,
				message,
			)
			ctlrutils.SetProvisioningStateInProgress(t.object, message)
		}
	} else {
		message = ciProvisionedCondition.Message
		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ClusterProvisioned,
			provisioningv1alpha1.ConditionReason(ciProvisionedCondition.Reason),
			ciProvisionedCondition.Status,
			message,
		)
	}

	if ctlrutils.IsClusterProvisionPresent(t.object) {
		// Set the start timestamp if it's not already set
		if t.object.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt.IsZero() {
			currentTime := metav1.Now()
			t.object.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt = &currentTime
		}

		if ctlrutils.IsClusterProvisionFailed(t.object) {
			message = "Cluster installation failed"
			ctlrutils.SetProvisioningStateFailed(t.object, message)
		} else if !ctlrutils.IsClusterProvisionCompleted(t.object) {
			// If it's not failed or completed, check if it has timed out
			if ctlrutils.TimeoutExceeded(
				t.object.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt.Time,
				t.timeouts.clusterProvisioning) {
				// timed out
				message = "Cluster installation timed out"
				ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
					provisioningv1alpha1.PRconditionTypes.ClusterProvisioned,
					provisioningv1alpha1.CRconditionReasons.TimedOut,
					metav1.ConditionFalse,
					message,
				)
				ctlrutils.SetProvisioningStateFailed(t.object, message)
			} else {
				message = "Cluster installation is in progress"
				ctlrutils.SetProvisioningStateInProgress(t.object, message)
			}
		}
	}

	t.logger.Info(
		fmt.Sprintf("ClusterInstance (%s) installation status: %s", ci.Name, message),
	)
}

// addDisableAutoImportAnnotation adds the disable-auto-import annotation to the ManagedCluster
// through the ClusterInstance.
// This workaround addresses a race condition during server reboot caused by hardware provisioning.
// During this period, stale clusters may be re-imported because timing issues (e.g., delayed leader election
// or incomplete container restarts) cause ACM to mistakenly identify an old cluster as ready.
// This annotation will be removed from ManagedCluster after the cluster installation is completed.
func addDisableAutoImportAnnotation(renderedCI *unstructured.Unstructured) error {
	extraAnnotations, found, err := unstructured.NestedMap(renderedCI.Object, "spec", "extraAnnotations")
	if err != nil {
		return fmt.Errorf("failed to get spec.extraAnnotations: %w", err)
	}
	if !found {
		extraAnnotations = make(map[string]any)
	}

	mcAnnotations, found, err := unstructured.NestedMap(extraAnnotations, "ManagedCluster")
	if err != nil {
		return fmt.Errorf("failed to get spec.extraAnnotations.ManagedCluster: %w", err)
	}
	if !found {
		mcAnnotations = make(map[string]any)
	}
	mcAnnotations[disableAutoImportAnnotation] = "true"
	extraAnnotations["ManagedCluster"] = mcAnnotations

	if err := unstructured.SetNestedMap(renderedCI.Object, extraAnnotations, "spec", "extraAnnotations"); err != nil {
		return fmt.Errorf("failed to set spec.extraAnnotations: %w", err)
	}
	return nil
}

// addSuppressedInstallManifests adds the suppressed manifests to the rendered ClusterInstance for upgrade
// case.
func addSuppressedInstallManifests(renderedCI *unstructured.Unstructured) error {
	suppressedManifests, found, err := unstructured.NestedSlice(renderedCI.Object, "spec", "suppressedManifests")
	if err != nil {
		return fmt.Errorf("failed to get spec.suppressedManifests: %w", err)
	}
	if !found {
		suppressedManifests = []any{}
	}
	for _, crd := range ctlrutils.CRDsToBeSuppressedForUpgrade {
		// Suppress install manifests to prevent unnecessary updates
		if !slices.ContainsFunc(suppressedManifests, func(item any) bool {
			return item.(string) == crd
		}) {
			suppressedManifests = append(suppressedManifests, crd)
		}
	}
	err = unstructured.SetNestedSlice(renderedCI.Object, suppressedManifests, "spec", "suppressedManifests")
	if err != nil {
		return fmt.Errorf("failed to set spec.suppressedManifests: %w", err)
	}

	return nil
}

// handleClusterInstanceUpgrade handles cluster upgrade transformations for the ClusterInstance.
// This includes adding suppressed manifests and preserving existing suppressed manifests.
func (t *provisioningRequestReconcilerTask) handleClusterInstanceUpgrade(
	existingCI *unstructured.Unstructured, renderedCI *unstructured.Unstructured) error {

	// Check if there is a clusterImageSetNameRef change (indicating an upgrade)
	changedFields, _, err := provisioningv1alpha1.FindClusterInstanceImmutableFieldUpdates(
		existingCI.Object["spec"].(map[string]any),
		renderedCI.Object["spec"].(map[string]any),
		ctlrutils.IgnoredClusterInstanceFields,
		provisioningv1alpha1.AllowedClusterInstanceFields)
	if err != nil {
		return fmt.Errorf(
			"failed to find field updates for ClusterInstance (%s): %w", existingCI.GetName(), err)
	}

	// Add suppressed manifests when clusterImageSetNameRef changes (indicating an upgrade)
	if slices.Contains(changedFields, "clusterImageSetNameRef") {
		err = addSuppressedInstallManifests(renderedCI)
		if err != nil {
			return fmt.Errorf(
				"failed to add suppressed install manifests to the rendered ClusterInstance (%s): %w",
				renderedCI.GetName(), err)
		}
	}

	// Preserve existing suppressedManifests from the current ClusterInstance
	if existingCISuppressed, ok := existingCI.Object["spec"].(map[string]any)["suppressedManifests"].([]any); ok {
		renderedCISuppressed, ok := renderedCI.Object["spec"].(map[string]any)["suppressedManifests"].([]any)
		if !ok {
			renderedCISuppressed = []any{}
		}
		for _, suppressedManifest := range existingCISuppressed {
			if !slices.ContainsFunc(renderedCISuppressed, func(item any) bool {
				return item.(string) == suppressedManifest.(string)
			}) {
				renderedCISuppressed = append(renderedCISuppressed, suppressedManifest)
			}
		}
		renderedCI.Object["spec"].(map[string]any)["suppressedManifests"] = renderedCISuppressed
	}

	return nil
}

// extractNodeDetails extracts necessary node details from the existing ClusterInstance
func extractNodeDetails(existingCI *unstructured.Unstructured) map[string]nodeInfo {
	nodes := existingCI.Object["spec"].(map[string]any)["nodes"].([]any)
	extractedNodesInfo := make(map[string]nodeInfo) // keyed by hostname

	for _, node := range nodes {
		nodeMap := node.(map[string]any)
		hostname, ok := nodeMap["hostName"].(string)
		if !ok {
			continue
		}

		extractedNodeInfo := nodeInfo{}
		bmcAddress, ok := nodeMap["bmcAddress"].(string)
		if ok {
			extractedNodeInfo.bmcAddress = &bmcAddress
		}

		bootMACAddress, ok := nodeMap["bootMACAddress"].(string)
		if ok {
			extractedNodeInfo.bootMACAddress = &bootMACAddress
		}

		bmcCreds, ok := nodeMap["bmcCredentialsName"].(map[string]any)
		if ok {
			bmcCredsName, ok := bmcCreds["name"].(string)
			if ok {
				extractedNodeInfo.bmcCredentialsName = &bmcCredsName
			}
		}

		hostRef, ok := nodeMap["hostRef"].(map[string]string)
		if ok {
			extractedNodeInfo.HwMgrNodeId = hostRef["name"]
			extractedNodeInfo.HwMgrNodeNs = hostRef["namespace"]
		}

		// Extract interface macAddress by interface name
		if nodeNetwork, ok := nodeMap["nodeNetwork"].(map[string]any); ok {
			if ifaces, ok := nodeNetwork["interfaces"].([]any); ok {
				macByIface := make(map[string]string)
				for _, iface := range ifaces {
					ifaceMap := iface.(map[string]any)
					name, ok := ifaceMap["name"].(string)
					if !ok {
						continue
					}
					mac, ok := ifaceMap["macAddress"].(string)
					if !ok {
						continue
					}
					macByIface[name] = mac
				}
				extractedNodeInfo.interfaceMACAddress = macByIface
			}
		}

		extractedNodesInfo[hostname] = extractedNodeInfo
	}

	return extractedNodesInfo
}

// assignNodeDetails assigns extracted node details to the rendered ClusterInstance
// Unexpected nodes structure in rendered ClusterInstance will be ignored here and
// the error will be caught by the dry-run validation.
func assignNodeDetails(renderedCI *unstructured.Unstructured, nodesInfo map[string]nodeInfo) {
	nodes, ok := renderedCI.Object["spec"].(map[string]any)["nodes"].([]any)
	if !ok {
		return
	}

	for _, node := range nodes {
		nodeMap, ok := node.(map[string]any)
		if !ok {
			continue
		}
		hostname, ok := nodeMap["hostName"].(string)
		if !ok {
			continue
		}
		// Assign the node info
		if extractedNode, exists := nodesInfo[hostname]; exists {
			if extractedNode.bmcAddress != nil {
				nodeMap["bmcAddress"] = *extractedNode.bmcAddress
			}
			if extractedNode.bootMACAddress != nil {
				nodeMap["bootMACAddress"] = *extractedNode.bootMACAddress
			}
			if extractedNode.bmcCredentialsName != nil {
				nodeMap["bmcCredentialsName"] = map[string]any{
					"name": *extractedNode.bmcCredentialsName,
				}
			}
			if extractedNode.HwMgrNodeId != "" && extractedNode.HwMgrNodeNs != "" {
				nodeMap["hostRef"] = map[string]string{
					"name":      extractedNode.HwMgrNodeId,
					"namespace": extractedNode.HwMgrNodeNs,
				}
			}

			// Only proceed with nodeNetwork.interfaces if interfaceMACAddress is present
			if len(extractedNode.interfaceMACAddress) > 0 {
				if _, exists := nodeMap["nodeNetwork"]; !exists {
					nodeMap["nodeNetwork"] = map[string]any{}
				}
				nodeNetworkMap, ok := nodeMap["nodeNetwork"].(map[string]any)
				if !ok {
					continue
				}
				if _, exists := nodeNetworkMap["interfaces"]; !exists {
					nodeNetworkMap["interfaces"] = []any{}
				}
				interfaces, ok := nodeNetworkMap["interfaces"].([]any)
				if !ok {
					continue
				}
				// Iterate through existing rendered interfaces and update in-place
				existingInterfaces := make(map[string]map[string]any)
				for _, iface := range interfaces {
					if ifaceMap, ok := iface.(map[string]any); ok {
						if name, ok := ifaceMap["name"].(string); ok {
							existingInterfaces[name] = ifaceMap
						}
					}
				}
				// Modify existing interfaces or append new ones
				for ifaceName, mac := range extractedNode.interfaceMACAddress {
					if existingIface, exists := existingInterfaces[ifaceName]; exists {
						existingIface["macAddress"] = mac
					} else {
						interfaces = append(interfaces, map[string]any{
							"name":       ifaceName,
							"macAddress": mac,
						})
					}
				}
				// Store updated interfaces back in nodeNetwork
				nodeNetworkMap["interfaces"] = interfaces
			}
		}
	}
}
