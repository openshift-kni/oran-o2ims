/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils/spokeclient"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
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
		nodesInfo := extractNodeDetails(existingCIUnstructured)
		assignNodeDetails(renderedCIUnstructured, nodesInfo)
	}

	// Handle cluster upgrade transformations before validation
	if ciExists && ctlrutils.IsClusterProvisionCompleted(t.object) {
		err = t.handleClusterInstanceUpgrade(existingCIUnstructured, renderedCIUnstructured)
		if err != nil {
			return nil, fmt.Errorf("failed to handle cluster instance upgrade: %w", err)
		}
	}

	// Validate that node scaling only affects worker nodes
	if ciExists && ctlrutils.IsClusterProvisionCompleted(t.object) {
		if err := validateScaleWorkerOnly(existingCIUnstructured, renderedCIUnstructured); err != nil {
			return nil, err
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
		return nil, typederrors.NewInputError("failed to convert to siteconfig.ClusterInstance type: %w", err)
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
	// from the hardware manager if they have not been set through the ProvisioningRequest.
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

	// Remove interface labels from the ClusterInstance spec as they are not part of the ClusterInstance CRD schema
	// but are needed during hardware provisioning for MAC address assignment
	if err := ctlrutils.RemoveLabelFromInterfaces(renderedClusterInstanceUnstructured.Object["spec"]); err != nil {
		return nil, fmt.Errorf("failed to remove interface labels from ClusterInstance spec: %w", err)
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

		// Update the ProvisioningRequest status with the clusterInstance details
		if updateErr := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
			return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
		}
	}

	// Detect scale-out: if the rendered CI has more nodes than the last fulfilled
	// count, proactively transition the PR to Progressing. Without this, the PR
	// stays at Fulfilled because siteconfig doesn't reset the CI's Provisioned
	// condition when nodes are added to an already-provisioned cluster.
	fulfilledCount := 0
	if t.object.Status.Extensions.ClusterDetails != nil {
		fulfilledCount = t.object.Status.Extensions.ClusterDetails.FulfilledNodeCount
	}
	renderedNodeCount := len(getNodeRolesByHostname(clusterInstance))
	if fulfilledCount > 0 && renderedNodeCount > fulfilledCount {
		t.logger.InfoContext(ctx, "Scale-out in progress",
			slog.Int("fulfilledNodeCount", fulfilledCount),
			slog.Int("renderedNodeCount", renderedNodeCount))

		// Only set InProgress on the first detection (when ClusterProvisioned is
		// still Completed).
		if ctlrutils.IsClusterProvisionCompleted(t.object) {
			message := fmt.Sprintf("Scale-out: waiting for new node to join the cluster (%d → %d nodes)",
				fulfilledCount, renderedNodeCount)
			t.logger.InfoContext(ctx, message)
			ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
				provisioningv1alpha1.PRconditionTypes.ClusterProvisioned,
				provisioningv1alpha1.CRconditionReasons.InProgress,
				metav1.ConditionFalse,
				message,
			)
			ctlrutils.SetProvisioningStateInProgress(t.object, message)
			currentTime := metav1.Now()
			t.object.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt = &currentTime
			if updateErr := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
				return fmt.Errorf("failed to update scale-out status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
			}
		}

		// WORKAROUND: Approve pending CSRs for scale-out worker nodes.
		// The Assisted Service Agent controller should handle day-2 CSR approval
		// via tryApproveDay2CSRs(), but it is not firing for scale-out workers
		// (the Agent stays in installing-in-progress:Rebooting with empty
		// csrStatus). This workaround can be removed once the Assisted Service
		// bug is fixed. See: project_scaleout_csr_approval.md
		if err := t.approveScaleOutCSRs(ctx, clusterInstance); err != nil {
			t.logger.WarnContext(ctx, "Scale-out CSR approval attempt failed, will retry",
				slog.Any("error", err))
		}

		// Check if all rendered nodes have joined the spoke cluster. If so,
		// update fulfilledNodeCount and let the normal finalization flow run.
		allJoined, checkErr := t.checkAllNodesJoined(ctx, clusterInstance)
		if checkErr != nil {
			t.logger.WarnContext(ctx, "Failed to check spoke node status, will retry",
				slog.Any("error", checkErr))
		}
		t.logger.InfoContext(ctx, "Scale-out node join check",
			slog.Bool("allJoined", allJoined))

		if allJoined {
			t.logger.InfoContext(ctx, "All scale-out nodes have joined the cluster")
			t.object.Status.Extensions.ClusterDetails.FulfilledNodeCount = renderedNodeCount
			ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
				provisioningv1alpha1.PRconditionTypes.ClusterProvisioned,
				provisioningv1alpha1.CRconditionReasons.Completed,
				metav1.ConditionTrue,
				"Provisioning completed",
			)
			if updateErr := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
				return fmt.Errorf("failed to update scale-out completion status: %w", updateErr)
			}
			// Fall through to normal finalization
		} else {
			// Don't fall through to checkClusterProvisionStatus while scale-out
			// is in progress. Siteconfig doesn't reset the CI's Provisioned
			// condition for scale-out, so checkClusterProvisionStatus would
			// overwrite our InProgress status back to Completed, causing
			// premature finalization.
			return nil
		}
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

// applyClusterInstance ensures the state of the ClusterInstance in the cluster matches the desired state.
// It uses Server-Side Apply (SSA), which is the modern, idempotent, and robust Kubernetes standard.
// This single function correctly handles create, update, and conflict scenarios without needing to
// manually check if the object already exists.
func (t *provisioningRequestReconcilerTask) applyClusterInstance(ctx context.Context, clusterInstance client.Object, isDryRun bool) error {

	if clusterInstance == nil {
		return typederrors.NewInputError("clusterInstance cannot be nil")
	}
	unstructuredObj, ok := clusterInstance.(*unstructured.Unstructured)
	if !ok {
		// This would indicate a programming error in the calling code.
		return fmt.Errorf("internal error: clusterInstance is not of type *unstructured.Unstructured, but %T", clusterInstance)
	}

	// Log function entry with structured context
	t.logger.InfoContext(
		ctx,
		"Applying ClusterInstance using Server-Side Apply",
		slog.String("name", unstructuredObj.GetName()),
		slog.String("namespace", unstructuredObj.GetNamespace()),
		slog.Bool("isDryRun", isDryRun),
	)

	// Use DeepCopy() to create a safe, mutable copy for the patch operation. This prevents
	// any modifications to the original object that might be used elsewhere.
	patchObj := unstructuredObj.DeepCopy()

	// Set controller reference to ensure proper ownership and enable watch functionality
	if err := ctrl.SetControllerReference(t.object, patchObj, t.client.Scheme()); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Build Server-Side Apply Options
	patchOpts := []client.PatchOption{
		client.ForceOwnership,
		client.FieldOwner(ctlrutils.ProvisioningRequestFieldManager),
	}
	if isDryRun {
		patchOpts = append(patchOpts, client.DryRunAll)
	}

	// Execute the Apply Operation
	// This single API call instructs the server to converge the object to our desired state.
	// The server handles the complex logic of creating or merging the changes.
	if err := t.client.Patch(ctx, patchObj, client.Apply, patchOpts...); err != nil {
		// Standard error handling for Kubernetes API calls.
		if errors.IsConflict(err) {
			t.logger.InfoContext(
				ctx,
				"Conflict detected during Server-Side Apply, requeueing for retry. This is a normal part of reconciliation.",
				slog.String("name", patchObj.GetName()),
				slog.String("namespace", patchObj.GetNamespace()),
			)
			// Returning a conflict error will cause the reconciler to retry the operation.
			return fmt.Errorf("conflict during server-side apply: %w", err)
		}
		if errors.IsInvalid(err) || errors.IsBadRequest(err) || errors.IsForbidden(err) {
			return typederrors.NewInputError("invalid ClusterInstance configuration: %w", err)
		}
		return fmt.Errorf("failed to apply ClusterInstance: %w", err)
	}

	// Determine the operation type for logging, conforming to the original function's style.
	// With SSA, the operation is best described as "Applied" or "Updated" since the server
	// converges the state regardless of whether it was a create or update.
	operationType := ctlrutils.OperationTypeUpdated
	if isDryRun {
		operationType = ctlrutils.OperationTypeDryRun
	}

	// This log message now reflects that the state has been successfully converged.
	t.logger.InfoContext(
		ctx,
		"Successfully applied ClusterInstance",
		slog.String("name", patchObj.GetName()),
		slog.String("namespace", patchObj.GetNamespace()),
		slog.String("operation", operationType),
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
			provisioningv1alpha1.CRconditionReasons.InProgress,
			metav1.ConditionFalse,
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
				provisioningv1alpha1.CRconditionReasons.InProgress,
				metav1.ConditionFalse,
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

// validateScaleWorkerOnly ensures that node scaling only adds worker nodes.
// It compares the existing and rendered ClusterInstance node lists and rejects
// any operation that removes nodes (scale-in is not yet supported), adds
// non-worker nodes, or scales a single-node cluster.
func validateScaleWorkerOnly(existingCI, renderedCI *unstructured.Unstructured) error {
	existingNodes := getNodeRolesByHostname(existingCI)
	renderedNodes := getNodeRolesByHostname(renderedCI)

	// No scaling detected
	if len(existingNodes) == len(renderedNodes) {
		same := true
		for hostname := range existingNodes {
			if _, exists := renderedNodes[hostname]; !exists {
				same = false
				break
			}
		}
		if same {
			return nil
		}
	}

	// Reject scaling on single-node clusters
	masterCount := 0
	for _, role := range existingNodes {
		if role == "master" || role == "control-plane" {
			masterCount++
		}
	}
	if len(existingNodes) == 1 || (len(existingNodes) == masterCount && masterCount <= 1) {
		return typederrors.NewInputError("node scaling is not supported on single-node clusters")
	}

	// Reject any node removals — scale-in is not yet supported
	for hostname, role := range existingNodes {
		if _, exists := renderedNodes[hostname]; !exists {
			return typederrors.NewInputError(
				"node removal is not yet supported: cannot remove %s node %q",
				role, hostname)
		}
	}

	// Check for added nodes with non-worker role
	for hostname, role := range renderedNodes {
		if _, exists := existingNodes[hostname]; !exists {
			if role != "worker" {
				return typederrors.NewInputError(
					"node scaling is restricted to worker nodes: cannot add %s node %q",
					role, hostname)
			}
		}
	}

	return nil
}

// scaleOutCSRRBACRules defines the RBAC permissions delivered to the spoke cluster
// for approving CSRs during scale-out operations.
var scaleOutCSRRBACRules = []rbacv1.PolicyRule{
	{
		APIGroups: []string{"certificates.k8s.io"},
		Resources: []string{"certificatesigningrequests"},
		Verbs:     []string{"get", "list", "watch"},
	},
	{
		APIGroups: []string{"certificates.k8s.io"},
		Resources: []string{"certificatesigningrequests/approval"},
		Verbs:     []string{"update"},
	},
	{
		APIGroups: []string{"certificates.k8s.io"},
		Resources: []string{"signers"},
		ResourceNames: []string{
			"kubernetes.io/kube-apiserver-client-kubelet",
			"kubernetes.io/kubelet-serving",
		},
		Verbs: []string{"approve"},
	},
	{
		APIGroups: []string{""},
		Resources: []string{"nodes"},
		Verbs:     []string{"get", "list"},
	},
}

// scaleOutSpokeScheme is the scheme used by the spoke client for CSR operations.
var scaleOutSpokeScheme = spokeclient.NewSpokeScheme(certificatesv1.AddToScheme, corev1.AddToScheme)

// approveScaleOutCSRs is a WORKAROUND for an Assisted Service bug where
// tryApproveDay2CSRs() does not fire for scale-out worker nodes. It connects
// to the spoke cluster and approves pending CSRs whose CN matches a node
// hostname in the rendered ClusterInstance.
//
// This workaround can be removed once the Assisted Service bug is fixed.
// See: project_scaleout_csr_approval.md
func (t *provisioningRequestReconcilerTask) approveScaleOutCSRs(
	ctx context.Context, clusterInstance *unstructured.Unstructured) error {

	clusterName := clusterInstance.GetName()
	msaName := t.object.Name + "-scaleout"
	mwName := t.object.Name + "-scaleout-rbac"

	spokeClient, ready, err := spokeclient.EnsureSpokeClient(
		ctx, t.client, t.logger, clusterName,
		msaName, mwName,
		scaleOutCSRRBACRules, scaleOutSpokeScheme)
	if err != nil {
		return fmt.Errorf("failed to setup spoke client for CSR approval: %w", err)
	}
	if !ready {
		t.logger.InfoContext(ctx, "Spoke client for CSR approval not ready yet, will retry")
		return nil
	}

	// Build set of valid hostnames from the rendered CI
	validHostnames := getNodeRolesByHostname(clusterInstance)

	// List all CSRs on the spoke
	csrList := &certificatesv1.CertificateSigningRequestList{}
	if err := spokeClient.List(ctx, csrList); err != nil {
		return fmt.Errorf("failed to list CSRs on spoke cluster: %w", err)
	}

	for i := range csrList.Items {
		csr := &csrList.Items[i]

		// Skip already approved/denied CSRs
		if isCSRApprovedOrDenied(csr) {
			continue
		}

		// Only handle node bootstrap and kubelet serving CSRs
		if csr.Spec.SignerName != "kubernetes.io/kube-apiserver-client-kubelet" &&
			csr.Spec.SignerName != "kubernetes.io/kubelet-serving" {
			continue
		}

		// Extract the node hostname from the CSR's CN
		hostname, err := extractNodeHostnameFromCSR(csr)
		if err != nil {
			t.logger.WarnContext(ctx, "Failed to parse CSR subject",
				slog.String("csr", csr.Name), slog.Any("error", err))
			continue
		}

		// Only approve if the hostname is in the rendered CI
		if _, ok := validHostnames[hostname]; !ok {
			continue
		}

		// Approve the CSR
		csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
			Type:           certificatesv1.CertificateApproved,
			Status:         corev1.ConditionTrue,
			Reason:         "O2IMSScaleOutApproval",
			Message:        "CSR approved by O-Cloud Manager for scale-out worker node",
			LastUpdateTime: metav1.Now(),
		})
		if err := spokeClient.SubResource("approval").Update(ctx, csr); err != nil {
			t.logger.WarnContext(ctx, "Failed to approve CSR",
				slog.String("csr", csr.Name), slog.String("hostname", hostname),
				slog.Any("error", err))
			continue
		}
		t.logger.InfoContext(ctx, "Approved scale-out CSR",
			slog.String("csr", csr.Name), slog.String("hostname", hostname),
			slog.String("signerName", csr.Spec.SignerName))
	}

	return nil
}

// isCSRApprovedOrDenied checks if a CSR has already been approved or denied.
func isCSRApprovedOrDenied(csr *certificatesv1.CertificateSigningRequest) bool {
	for _, c := range csr.Status.Conditions {
		if c.Type == certificatesv1.CertificateApproved || c.Type == certificatesv1.CertificateDenied {
			return true
		}
	}
	return false
}

// extractNodeHostnameFromCSR parses the PEM-encoded CSR request and returns
// the node hostname from the CN (expected format: "system:node:<hostname>").
func extractNodeHostnameFromCSR(csr *certificatesv1.CertificateSigningRequest) (string, error) {
	block, _ := pem.Decode(csr.Spec.Request)
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block from CSR %s", csr.Name)
	}
	req, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse certificate request from CSR %s: %w", csr.Name, err)
	}
	cn := req.Subject.CommonName
	const nodePrefix = "system:node:"
	if !strings.HasPrefix(cn, nodePrefix) {
		return "", fmt.Errorf("csr %s CN %q does not have expected prefix %q", csr.Name, cn, nodePrefix)
	}
	return cn[len(nodePrefix):], nil
}

// checkAllNodesJoined verifies that all hostnames in the rendered ClusterInstance
// exist as nodes on the spoke cluster. Returns true only if every rendered
// hostname has a corresponding node.
func (t *provisioningRequestReconcilerTask) checkAllNodesJoined(
	ctx context.Context, clusterInstance *unstructured.Unstructured) (bool, error) {

	clusterName := clusterInstance.GetName()
	msaName := t.object.Name + "-scaleout"
	mwName := t.object.Name + "-scaleout-rbac"

	spokeClient, ready, err := spokeclient.EnsureSpokeClient(
		ctx, t.client, t.logger, clusterName,
		msaName, mwName,
		scaleOutCSRRBACRules, scaleOutSpokeScheme)
	if err != nil {
		return false, fmt.Errorf("failed to setup spoke client for node check: %w", err)
	}
	if !ready {
		return false, nil
	}

	// List nodes on the spoke
	nodeList := &corev1.NodeList{}
	if err := spokeClient.List(ctx, nodeList); err != nil {
		return false, fmt.Errorf("failed to list nodes on spoke cluster: %w", err)
	}

	spokeNodes := make(map[string]bool, len(nodeList.Items))
	for _, node := range nodeList.Items {
		spokeNodes[node.Name] = true
	}

	// Check that every rendered hostname exists as a spoke node
	renderedHostnames := getNodeRolesByHostname(clusterInstance)
	for hostname := range renderedHostnames {
		if !spokeNodes[hostname] {
			return false, nil
		}
	}

	return true, nil
}

// cleanupScaleOutSpokeAccess removes the spoke client resources created for
// scale-out CSR approval.
func (t *provisioningRequestReconcilerTask) cleanupScaleOutSpokeAccess(ctx context.Context, clusterName string) {
	msaName := t.object.Name + "-scaleout"
	mwName := t.object.Name + "-scaleout-rbac"
	if err := spokeclient.CleanupSpokeAccess(ctx, t.client, clusterName, msaName, mwName); err != nil {
		t.logger.WarnContext(ctx, "Failed to cleanup scale-out spoke access",
			slog.Any("error", err))
	}
}

// getNodeRolesByHostname extracts a map of hostname → role from a ClusterInstance.
func getNodeRolesByHostname(ci *unstructured.Unstructured) map[string]string {
	result := make(map[string]string)
	spec, ok := ci.Object["spec"].(map[string]any)
	if !ok {
		return result
	}
	nodes, ok := spec["nodes"].([]any)
	if !ok {
		return result
	}
	for _, node := range nodes {
		nodeMap, ok := node.(map[string]any)
		if !ok {
			continue
		}
		hostname, _ := nodeMap["hostName"].(string)
		role, _ := nodeMap["role"].(string)
		if hostname != "" && role != "" {
			result[hostname] = role
		}
	}
	return result
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

		hostRef, ok := nodeMap["hostRef"].(map[string]any)
		if ok {
			hwMgrNodeId, okId := hostRef["name"].(string)
			hwMgrNodeNs, okNs := hostRef["namespace"].(string)
			if okId && okNs {
				extractedNodeInfo.HwMgrNodeId = hwMgrNodeId
				extractedNodeInfo.HwMgrNodeNs = hwMgrNodeNs
			}
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
				nodeMap["hostRef"] = map[string]any{
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
