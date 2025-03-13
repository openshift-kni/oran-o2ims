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
	"strings"

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

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	disableAutoImportAnnotation = "import.open-cluster-management.io/disable-auto-import"
)

func (t *provisioningRequestReconcilerTask) renderClusterInstanceTemplate(
	ctx context.Context) (*siteconfig.ClusterInstance, error) {
	t.logger.InfoContext(
		ctx,
		"Rendering the ClusterInstance template for ProvisioningRequest",
		slog.String("name", t.object.Name),
	)

	// Wrap the merged ClusterInstance data in a map with key "Cluster"
	// This data object will be consumed by the clusterInstance template
	mergedClusterInstanceData := map[string]any{
		"Cluster": t.clusterInput.clusterInstanceData,
	}

	disableAutoImport := true
	suppressedManifests := []string{}

	renderedClusterInstance := &siteconfig.ClusterInstance{}
	renderedClusterInstanceUnstructure, err := utils.RenderTemplateForK8sCR(
		"ClusterInstance", utils.ClusterInstanceTemplatePath, mergedClusterInstanceData)
	if err != nil {
		return nil, utils.NewInputError("failed to render the ClusterInstance template for ProvisioningRequest: %w", err)
	} else {
		// Add ProvisioningRequest labels to the generated ClusterInstance
		labels := make(map[string]string)
		labels[provisioningv1alpha1.ProvisioningRequestNameLabel] = t.object.Name
		renderedClusterInstanceUnstructure.SetLabels(labels)

		// Create the ClusterInstance namespace if not exist.
		ciName := renderedClusterInstanceUnstructure.GetName()
		err = t.createClusterInstanceNamespace(ctx, ciName)
		if err != nil {
			return nil, fmt.Errorf("failed to create cluster namespace %s: %w", ciName, err)
		}

		// Check for updates to immutable fields in the ClusterInstance, if it exists.
		// Once provisioning has started or reached a final state (Completed or Failed),
		// updates to immutable fields in the ClusterInstance spec are disallowed,
		// with the exception of scaling up/down when Cluster provisioning is completed.
		crProvisionedCond := meta.FindStatusCondition(t.object.Status.Conditions,
			string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned))
		if crProvisionedCond != nil && crProvisionedCond.Reason != string(provisioningv1alpha1.CRconditionReasons.Unknown) {
			disableAutoImport = false

			existingClusterInstance := &unstructured.Unstructured{}
			existingClusterInstance.SetGroupVersionKind(
				renderedClusterInstanceUnstructure.GroupVersionKind())
			ciExists, err := utils.DoesK8SResourceExist(
				ctx, t.client, ciName, ciName, existingClusterInstance,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to get ClusterInstance (%s): %w",
					ciName, err)
			}
			if ciExists {
				updatedFields, scalingNodes, err := provisioningv1alpha1.FindClusterInstanceImmutableFieldUpdates(
					existingClusterInstance.Object["spec"].(map[string]any),
					renderedClusterInstanceUnstructure.Object["spec"].(map[string]any),
					utils.IgnoredClusterInstanceFields)
				if err != nil {
					return nil, fmt.Errorf(
						"failed to find immutable field updates for ClusterInstance (%s): %w", ciName, err)
				}

				// copy the existing suppressedManifests
				existingCI := &siteconfig.ClusterInstance{}
				err = runtime.DefaultUnstructuredConverter.FromUnstructured(existingClusterInstance.Object, existingCI)
				if err != nil {
					return nil, fmt.Errorf("failed to get current suppressedManifests values: %w", err)
				}
				suppressedManifests = existingCI.Spec.SuppressedManifests

				var disallowedChanges []string
				for _, updatedField := range updatedFields {
					// Suppress install manifests to prevent unnecessary updates
					if updatedField == "clusterImageSetNameRef" &&
						crProvisionedCond.Reason == string(provisioningv1alpha1.CRconditionReasons.Completed) {
						for _, crd := range utils.CRDsToBeSuppressedForUpgrade {
							if !slices.Contains(suppressedManifests, crd) {
								suppressedManifests = append(suppressedManifests, crd)
							}
						}
					} else {
						disallowedChanges = append(disallowedChanges, updatedField)
					}
				}
				if len(scalingNodes) != 0 &&
					crProvisionedCond.Reason != string(provisioningv1alpha1.CRconditionReasons.Completed) {
					// In-progress || Failed
					disallowedChanges = append(disallowedChanges, scalingNodes...)
				}

				if len(disallowedChanges) != 0 {
					return nil, utils.NewInputError(
						"detected changes in immutable fields: %s", strings.Join(disallowedChanges, ", "))
				}
			}
		}

		// Validate the rendered ClusterInstance with dry-run
		isDryRun := true
		err = t.applyClusterInstance(ctx, renderedClusterInstanceUnstructure, isDryRun)
		if err != nil {
			return nil, fmt.Errorf("failed to validate the rendered ClusterInstance with dry-run: %w", err)
		}

		// Convert unstructured to siteconfig.ClusterInstance type
		if err = runtime.DefaultUnstructuredConverter.FromUnstructured(
			renderedClusterInstanceUnstructure.Object, renderedClusterInstance); err != nil {
			// Unlikely to happen since dry-run validation has passed
			return nil, utils.NewInputError("failed to convert to siteconfig.ClusterInstance type: %w", err)
		}
		renderedClusterInstance.Spec.SuppressedManifests = append(renderedClusterInstance.Spec.SuppressedManifests, suppressedManifests...)

		if disableAutoImport {
			// Disable ManagedCluster auto-import by adding annotation import.open-cluster-management.io/disable-auto-import
			// through the ClusterInstance.
			// This workaround addresses a race condition during server reboot caused by hardware provisioning.
			// During this period, stale clusters may be re-imported because timing issues (e.g., delayed leader election
			// or incomplete container restarts) cause ACM to mistakenly identify an old cluster as ready.
			// This annotation will be removed from ManagedCluster once the cluster installation starts.
			if renderedClusterInstance.Spec.ExtraAnnotations == nil {
				renderedClusterInstance.Spec.ExtraAnnotations = make(map[string]map[string]string)
			}
			if _, exists := renderedClusterInstance.Spec.ExtraAnnotations["ManagedCluster"]; !exists {
				renderedClusterInstance.Spec.ExtraAnnotations["ManagedCluster"] = make(map[string]string)
			}
			renderedClusterInstance.Spec.ExtraAnnotations["ManagedCluster"][disableAutoImportAnnotation] = "true"
		}
	}

	return renderedClusterInstance, nil
}

// handleClusterInstallation creates/updates the ClusterInstance to handle the cluster provisioning.
func (t *provisioningRequestReconcilerTask) handleClusterInstallation(ctx context.Context, clusterInstance *siteconfig.ClusterInstance) error {
	isDryRun := false
	err := t.applyClusterInstance(ctx, clusterInstance, isDryRun)
	if err != nil {
		return fmt.Errorf("failed to apply the rendered ClusterInstance (%s): %s", clusterInstance.Name, err.Error())

	} else {
		// Set ClusterDetails
		if t.object.Status.Extensions.ClusterDetails == nil {
			t.object.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{}
		}
		t.object.Status.Extensions.ClusterDetails.Name = clusterInstance.GetName()
	}

	// Continue checking the existing ClusterInstance provision status
	if err := t.checkClusterProvisionStatus(ctx, clusterInstance.Name); err != nil {
		return err
	}

	// Remove the disable-auto-import annotation for the managed cluster
	// if the cluster provisioning is completed.
	if utils.IsClusterProvisionCompleted(t.object) {
		return t.removeDisableAutoImportAnnotation(ctx, clusterInstance)
	}

	return nil
}

// removeDisableAutoImportAnnotation removes the disable-auto-import annotation
// from the ManagedCluster if it exists.
func (t *provisioningRequestReconcilerTask) removeDisableAutoImportAnnotation(
	ctx context.Context, ci *siteconfig.ClusterInstance) error {

	managedCluster := &clusterv1.ManagedCluster{}
	exists, err := utils.DoesK8SResourceExist(
		ctx, t.client, ci.Name, "", managedCluster)
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
	if err := utils.RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
		exists, err := utils.DoesK8SResourceExist(ctx, t.client, clusterInstanceName, clusterInstanceName, clusterInstance)
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

	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
	}

	return nil
}

func (t *provisioningRequestReconcilerTask) applyClusterInstance(ctx context.Context, clusterInstance client.Object, isDryRun bool) error {
	var operationType string

	// Query the ClusterInstance and its status.
	existingClusterInstance := &siteconfig.ClusterInstance{}
	err := t.client.Get(
		ctx,
		types.NamespacedName{
			Name:      clusterInstance.GetName(),
			Namespace: clusterInstance.GetNamespace()},
		existingClusterInstance)
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get ClusterInstance: %w", err)
		}

		operationType = utils.OperationTypeCreated
		opts := []client.CreateOption{}
		if isDryRun {
			opts = append(opts, client.DryRunAll)
			operationType = utils.OperationTypeDryRun
		}

		err = ctrl.SetControllerReference(t.object, clusterInstance, t.client.Scheme())
		if err != nil {
			return fmt.Errorf("failed to set controller reference: %w", err)
		}

		// Create the ClusterInstance
		err = t.client.Create(ctx, clusterInstance, opts...)
		if err != nil {
			if !errors.IsInvalid(err) && !errors.IsBadRequest(err) {
				return fmt.Errorf("failed to create ClusterInstance: %w", err)
			}
			// Invalid or webhook error
			return utils.NewInputError("%s", err.Error())
		}
	} else {
		if _, ok := clusterInstance.(*siteconfig.ClusterInstance); ok {
			// No update needed, return
			if equality.Semantic.DeepEqual(existingClusterInstance.Spec,
				clusterInstance.(*siteconfig.ClusterInstance).Spec) {
				return nil
			}
		}

		// Make sure these fields from existing object are copied
		clusterInstance.SetResourceVersion(existingClusterInstance.GetResourceVersion())
		clusterInstance.SetFinalizers(existingClusterInstance.GetFinalizers())
		clusterInstance.SetLabels(existingClusterInstance.GetLabels())
		clusterInstance.SetAnnotations(existingClusterInstance.GetAnnotations())

		operationType = utils.OperationTypeUpdated
		opts := []client.PatchOption{}
		if isDryRun {
			opts = append(opts, client.DryRunAll)
			operationType = utils.OperationTypeDryRun
		}
		patch := client.MergeFrom(existingClusterInstance.DeepCopy())
		if err := t.client.Patch(ctx, clusterInstance, patch, opts...); err != nil {
			if !errors.IsInvalid(err) && !errors.IsBadRequest(err) {
				return fmt.Errorf("failed to patch ClusterInstance: %w", err)
			}
			// Invalid or webhook error
			return utils.NewInputError("%s", err.Error())
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
		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed,
			provisioningv1alpha1.CRconditionReasons.Unknown,
			metav1.ConditionUnknown,
			message,
		)
		utils.SetProvisioningStateInProgress(t.object, message)
		return
	}

	for _, condType := range clusterInstanceConditionTypes {
		ciCondition := meta.FindStatusCondition(ci.Status.Conditions, string(condType))
		if ciCondition != nil && ciCondition.Status != metav1.ConditionTrue {
			utils.SetStatusCondition(&t.object.Status.Conditions,
				provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed,
				provisioningv1alpha1.ConditionReason(ciCondition.Reason),
				ciCondition.Status,
				ciCondition.Message,
			)
			utils.SetProvisioningStateFailed(t.object, ciCondition.Message)
			return
		}
	}

	utils.SetStatusCondition(&t.object.Status.Conditions,
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
		ci.Status.Conditions, string(hwv1alpha1.Provisioned))

	if ciProvisionedCondition == nil {
		crClusterInstanceProcessedCond := meta.FindStatusCondition(
			t.object.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed))
		if crClusterInstanceProcessedCond != nil && crClusterInstanceProcessedCond.Status == metav1.ConditionTrue {
			message = "Waiting for cluster installation to start"
			utils.SetStatusCondition(&t.object.Status.Conditions,
				provisioningv1alpha1.PRconditionTypes.ClusterProvisioned,
				provisioningv1alpha1.CRconditionReasons.Unknown,
				metav1.ConditionUnknown,
				message,
			)
			utils.SetProvisioningStateInProgress(t.object, message)
		}
	} else {
		message = ciProvisionedCondition.Message
		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ClusterProvisioned,
			provisioningv1alpha1.ConditionReason(ciProvisionedCondition.Reason),
			ciProvisionedCondition.Status,
			message,
		)
	}

	if utils.IsClusterProvisionPresent(t.object) {
		// Set the start timestamp if it's not already set
		if t.object.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt.IsZero() {
			currentTime := metav1.Now()
			t.object.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt = &currentTime
		}

		if utils.IsClusterProvisionFailed(t.object) {
			message = "Cluster installation failed"
			utils.SetProvisioningStateFailed(t.object, message)
		} else if !utils.IsClusterProvisionCompleted(t.object) {
			// If it's not failed or completed, check if it has timed out
			if utils.TimeoutExceeded(
				t.object.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt.Time,
				t.timeouts.clusterProvisioning) {
				// timed out
				message = "Cluster installation timed out"
				utils.SetStatusCondition(&t.object.Status.Conditions,
					provisioningv1alpha1.PRconditionTypes.ClusterProvisioned,
					provisioningv1alpha1.CRconditionReasons.TimedOut,
					metav1.ConditionFalse,
					message,
				)
				utils.SetProvisioningStateFailed(t.object, message)
			} else {
				message = "Cluster installation is in progress"
				utils.SetProvisioningStateInProgress(t.object, message)
			}
		}
	}

	t.logger.Info(
		fmt.Sprintf("ClusterInstance (%s) installation status: %s", ci.Name, message),
	)
}
