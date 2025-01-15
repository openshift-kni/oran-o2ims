/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var provisioningrequestlog = logf.Log.WithName("provisioningrequest-webhook")

var webhookClient client.Client

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *ProvisioningRequest) SetupWebhookWithManager(mgr ctrl.Manager) error {
	if webhookClient == nil {
		webhookClient = mgr.GetClient()
	}

	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
//+kubebuilder:webhook:path=/validate-o2ims-provisioning-oran-org-v1alpha1-provisioningrequest,mutating=false,failurePolicy=fail,sideEffects=None,groups=o2ims.provisioning.oran.org,resources=provisioningrequests,verbs=create;update,versions=v1alpha1,name=provisioningrequests.o2ims.provisioning.oran.org,admissionReviewVersions=v1

var _ webhook.Validator = &ProvisioningRequest{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *ProvisioningRequest) ValidateCreate() (admission.Warnings, error) {
	provisioningrequestlog.Info("validate create", "name", r.Spec.Name)

	if err := r.validateCreateOrUpdate(nil); err != nil {
		provisioningrequestlog.Error(err, "failed to validate the ProvisioningRequest")
		return nil, err
	}

	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *ProvisioningRequest) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	provisioningrequestlog.Info("validate update", "name", r.Name)

	if !r.DeletionTimestamp.IsZero() {
		// ProvisioningRequest is being deleted, this update is triggered by finalizer removal
		return nil, nil
	}

	oldPr, casted := old.(*ProvisioningRequest)
	if !casted {
		provisioningrequestlog.Error(fmt.Errorf("old object conversion error for ProvisioningRequest"), "validate update error")
		return nil, nil
	}

	if err := r.validateCreateOrUpdate(oldPr); err != nil {
		provisioningrequestlog.Error(err, "failed to validate the ProvisioningRequest")
		return nil, err
	}

	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *ProvisioningRequest) ValidateDelete() (admission.Warnings, error) {
	provisioningrequestlog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil, nil
}

func (r *ProvisioningRequest) validateCreateOrUpdate(oldPr *ProvisioningRequest) error {
	clusterTemplate, err := r.GetClusterTemplateRef(context.TODO(), webhookClient)
	if err != nil {
		return err
	}

	if err = r.ValidateTemplateInputMatchesSchema(clusterTemplate); err != nil {
		return err
	}

	// We only validate the ClusterInstance input here, not the PolicyTemplate input since
	// its schema is not just for ProvisioningRequest.
	newPrClusterInstanceInput, err := r.ValidateClusterInstanceInputMatchesSchema(clusterTemplate)
	if err != nil {
		return err
	}

	if oldPr == nil {
		// ProvisioningRequest is being created, no immutable fields to check
		return nil
	}

	// Check for updates to immutable fields in the ClusterInstance input.
	// Once provisioning has started or reached a final state (Completed or Failed),
	// updates to immutable fields in the ClusterInstance input are disallowed,
	// with the exception of scaling up/down when Cluster provisioning is completed.
	crProvisionedCond := meta.FindStatusCondition(
		r.Status.Conditions, string(PRconditionTypes.ClusterProvisioned))
	if crProvisionedCond != nil && crProvisionedCond.Reason != string(CRconditionReasons.Unknown) {
		oldPrClusterInstanceInput, err := ExtractMatchingInput(
			oldPr.Spec.TemplateParameters.Raw, TemplateParamClusterInstance)
		if err != nil {
			return fmt.Errorf(
				"failed to extract matching input for subSchema %s: %w", TemplateParamClusterInstance, err)
		}

		updatedFields, scalingNodes, err := FindClusterInstanceImmutableFieldUpdates(
			oldPrClusterInstanceInput.(map[string]any), newPrClusterInstanceInput.(map[string]any), [][]string{})
		if err != nil {
			return fmt.Errorf("failed to find immutable field updates for ClusterInstance (%s): %w", r.Name, err)
		}

		if len(scalingNodes) != 0 && crProvisionedCond.Reason != "Completed" {
			updatedFields = append(updatedFields, scalingNodes...)
		}

		if len(updatedFields) != 0 {
			return fmt.Errorf("only \"extraAnnotations\" and/or \"extraLabels\" changes in spec.TemplateParameters.ClusterInstanceParameters "+
				"are allowed once cluster installation has started or reached to Completed/Failed state, detected changes in immutable fields: %s",
				strings.Join(updatedFields, ", "))
		}
	}

	return nil
}
