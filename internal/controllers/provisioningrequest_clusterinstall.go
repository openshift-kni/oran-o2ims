package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

	suppressedManifests := []string{}

	renderedClusterInstance := &siteconfig.ClusterInstance{}
	renderedClusterInstanceUnstructure, err := utils.RenderTemplateForK8sCR(
		"ClusterInstance", utils.ClusterInstanceTemplatePath, mergedClusterInstanceData)
	if err != nil {
		return nil, utils.NewInputError("failed to render the ClusterInstance template for ProvisioningRequest: %w", err)
	} else {
		// Add ProvisioningRequest labels to the generated ClusterInstance
		labels := make(map[string]string)
		labels[provisioningRequestNameLabel] = t.object.Name
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
			string(utils.PRconditionTypes.ClusterProvisioned))
		if crProvisionedCond != nil && crProvisionedCond.Reason != string(utils.CRconditionReasons.Unknown) {
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
				updatedFields, scalingNodes, err := utils.FindClusterInstanceImmutableFieldUpdates(
					existingClusterInstance, renderedClusterInstanceUnstructure)
				if err != nil {
					return nil, fmt.Errorf(
						"failed to find immutable field updates for ClusterInstance (%s): %w", ciName, err)
				}

				var disallowedChanges []string

				for _, updatedField := range updatedFields {
					// Add "AgentClusterInstall" to ClusterInstance.SuppressedManifests in order to
					// prevent unnecessary updates to ACI.
					if updatedField == "clusterImageSetNameRef" &&
						crProvisionedCond.Reason == string(utils.CRconditionReasons.Completed) {
						suppressedManifests = append(suppressedManifests, "AgentClusterInstall")
					} else {
						disallowedChanges = append(disallowedChanges, updatedField)
					}
				}
				if len(scalingNodes) != 0 &&
					crProvisionedCond.Reason != string(utils.CRconditionReasons.Completed) {
					// In-progress || Failed
					disallowedChanges = append(disallowedChanges, scalingNodes...)
				}

				if len(disallowedChanges) != 0 {
					return nil, utils.NewInputError(fmt.Sprintf(
						"detected changes in immutable fields: %s", strings.Join(disallowedChanges, ", ")))
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
		if t.object.Status.ClusterDetails == nil {
			t.object.Status.ClusterDetails = &provisioningv1alpha1.ClusterDetails{}
		}
		t.object.Status.ClusterDetails.Name = clusterInstance.GetName()
	}

	// Continue checking the existing ClusterInstance provision status
	if err := t.checkClusterProvisionStatus(ctx, clusterInstance.Name); err != nil {
		return err
	}
	return nil
}

// checkClusterProvisionStatus checks the status of cluster provisioning
func (t *provisioningRequestReconcilerTask) checkClusterProvisionStatus(
	ctx context.Context, clusterInstanceName string) error {

	clusterInstance := &siteconfig.ClusterInstance{}
	exists, err := utils.DoesK8SResourceExist(ctx, t.client, clusterInstanceName, clusterInstanceName, clusterInstance)
	if err != nil {
		return fmt.Errorf("failed to get ClusterInstance %s: %w", clusterInstanceName, err)
	}
	if !exists {
		return nil
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

		// Create the ClusterInstance
		err = t.client.Create(ctx, clusterInstance, opts...)
		if err != nil {
			if !errors.IsInvalid(err) && !errors.IsBadRequest(err) {
				return fmt.Errorf("failed to create ClusterInstance: %w", err)
			}
			// Invalid or webhook error
			return utils.NewInputError(err.Error())
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
			return utils.NewInputError(err.Error())
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
			utils.PRconditionTypes.ClusterInstanceProcessed,
			utils.CRconditionReasons.Unknown,
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
				utils.PRconditionTypes.ClusterInstanceProcessed,
				utils.ConditionReason(ciCondition.Reason),
				ciCondition.Status,
				ciCondition.Message,
			)
			utils.SetProvisioningStateFailed(t.object, ciCondition.Message)
			return
		}
	}

	utils.SetStatusCondition(&t.object.Status.Conditions,
		utils.PRconditionTypes.ClusterInstanceProcessed,
		utils.CRconditionReasons.Completed,
		metav1.ConditionTrue,
		fmt.Sprintf("Applied and processed ClusterInstance (%s) successfully", ci.Name),
	)
}

func (t *provisioningRequestReconcilerTask) updateClusterProvisionStatus(ci *siteconfig.ClusterInstance) {
	if ci == nil {
		return
	}

	// Search for ClusterInstance Provisioned condition
	ciProvisionedCondition := meta.FindStatusCondition(
		ci.Status.Conditions, string(hwv1alpha1.Provisioned))

	if ciProvisionedCondition == nil {
		crClusterInstanceProcessedCond := meta.FindStatusCondition(
			t.object.Status.Conditions, string(utils.PRconditionTypes.ClusterInstanceProcessed))
		if crClusterInstanceProcessedCond != nil && crClusterInstanceProcessedCond.Status == metav1.ConditionTrue {
			message := "Waiting for cluster installation to start"
			utils.SetStatusCondition(&t.object.Status.Conditions,
				utils.PRconditionTypes.ClusterProvisioned,
				utils.CRconditionReasons.Unknown,
				metav1.ConditionUnknown,
				message,
			)
			utils.SetProvisioningStateInProgress(t.object, message)
		}
	} else {
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ClusterProvisioned,
			utils.ConditionReason(ciProvisionedCondition.Reason),
			ciProvisionedCondition.Status,
			ciProvisionedCondition.Message,
		)
	}

	if utils.IsClusterProvisionPresent(t.object) {
		// Set the start timestamp if it's not already set
		if t.object.Status.ClusterDetails.ClusterProvisionStartedAt.IsZero() {
			t.object.Status.ClusterDetails.ClusterProvisionStartedAt = metav1.Now()
		}

		if utils.IsClusterProvisionFailed(t.object) {
			utils.SetProvisioningStateFailed(t.object, "Cluster installation failed")
		} else if !utils.IsClusterProvisionCompleted(t.object) {
			// If it's not failed or completed, check if it has timed out
			if utils.TimeoutExceeded(
				t.object.Status.ClusterDetails.ClusterProvisionStartedAt.Time,
				t.timeouts.clusterProvisioning) {
				// timed out
				message := "Cluster installation timed out"
				utils.SetStatusCondition(&t.object.Status.Conditions,
					utils.PRconditionTypes.ClusterProvisioned,
					utils.CRconditionReasons.TimedOut,
					metav1.ConditionFalse,
					message,
				)
				utils.SetProvisioningStateFailed(t.object, message)
			} else {
				utils.SetProvisioningStateInProgress(t.object, "Cluster installation is in progress")
			}
		}
	}
}
