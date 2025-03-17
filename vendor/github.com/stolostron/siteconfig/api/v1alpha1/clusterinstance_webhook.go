/*
Copyright 2025.

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
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"

	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// logger for this package
var clusterInstanceLogger = logf.Log.WithName("clusterinstance-webhook")

// clusterInstanceValidator handles validation for ClusterInstance resources.
type clusterInstanceValidator struct{}

// Ensure clusterInstanceValidator implements the webhook.CustomValidator interface.
var _ webhook.CustomValidator = &clusterInstanceValidator{}

//nolint:lll
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-siteconfig-open-cluster-management-io-v1alpha1-clusterinstance,mutating=false,failurePolicy=fail,sideEffects=None,groups=siteconfig.open-cluster-management.io,resources=clusterinstances,verbs=create;update;delete,versions=v1alpha1,name=clusterinstances.siteconfig.open-cluster-management.io,admissionReviewVersions=v1

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *ClusterInstance) SetupWebhookWithManager(mgr ctrl.Manager) error {
	err := ctrl.NewWebhookManagedBy(mgr).
		For(&ClusterInstance{}).
		WithValidator(&clusterInstanceValidator{}).
		Complete()
	if err != nil {
		return fmt.Errorf("encountered an error creating a new webhook builder for ClusterInstance: %w", err)
	}
	return nil
}

// ValidateCreate checks if the ClusterInstance object is valid during creation.
func (v *clusterInstanceValidator) ValidateCreate(ctx context.Context, obj runtime.Object,
) (admission.Warnings, error) {
	clusterInstance, ok := obj.(*ClusterInstance)
	if !ok {
		return nil, fmt.Errorf("expected ClusterInstance but received %T", obj)
	}

	log := clusterInstanceLogger.WithValues(
		"name", clusterInstance.Name,
		"namespace", clusterInstance.Namespace,
		"resourceVersion", clusterInstance.ResourceVersion)

	log.Info("Validating create request")

	// Reinstall field must not be set during initial installation.
	if clusterInstance.Spec.Reinstall != nil {
		msg := "reinstall spec cannot be set during initial installation"
		log.Error(nil, msg)
		return nil, errors.New(msg)
	}

	if err := ValidateClusterInstance(clusterInstance); err != nil {
		log.Error(err, "Validation failed")
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	log.Info("Validations passed for create request")
	return nil, nil
}

// ValidateUpdate validates updates to a ClusterInstance object.
func (v *clusterInstanceValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object,
) (admission.Warnings, error) {
	oldClusterInstance, ok := oldObj.(*ClusterInstance)
	if !ok {
		return nil, fmt.Errorf("expected a ClusterInstance but received %T", oldObj)
	}

	newClusterInstance, ok := newObj.(*ClusterInstance)
	if !ok {
		return nil, fmt.Errorf("expected a ClusterInstance but received %T", newObj)
	}

	log := clusterInstanceLogger.WithValues(
		"name", newClusterInstance.Name,
		"namespace", newClusterInstance.Namespace,
		"resourceVersion", newClusterInstance.ResourceVersion)

	log.Info("validating update request")

	// Allow updates if the object is being deleted (finalizer removal case).
	if !newClusterInstance.DeletionTimestamp.IsZero() {
		return nil, nil
	}

	// Prevent spec changes during provisioning or reinstall processes.
	if isProvisioningInProgress(newClusterInstance) || isReinstallInProgress(newClusterInstance) {
		if hasSpecChanged(oldClusterInstance, newClusterInstance) {
			log.Error(nil, "Spec update not allowed during provisioning or cluster reinstalls")
			return nil, errors.New("spec update not allowed during provisioning or cluster reinstalls")
		}
		log.Info("Provisioning or Cluster Reinstall is in progress")
		return nil, nil
	}

	// Validate permissible changes after provisioning is completed.
	if isProvisioningCompleted(newClusterInstance) {
		reinstallRequested := isReinstallRequested(newClusterInstance)
		if reinstallRequested {
			if isReinstallInProgress(newClusterInstance) &&
				newClusterInstance.Spec.Reinstall.Generation != oldClusterInstance.Spec.Reinstall.Generation {
				log.Error(nil, "Reinstall Generation update not allowed during reinstall")
				return nil, errors.New("reinstall Generation update not allowed during reinstall")
			}

			if err := validateReinstallRequest(newClusterInstance); err != nil {
				log.Error(err, "Invalid reinstall fields")
				return nil, fmt.Errorf("invalid reinstall fields: %w", err)
			}
		}

		// Validate allowed day-N changes.
		err := validatePostProvisioningChanges(log, oldClusterInstance, newClusterInstance, reinstallRequested)
		if err != nil {
			log.Error(err, "Invalid spec changes detected")
			return nil, fmt.Errorf("invalid spec changes detected: %w", err)
		}

	}

	// Perform general validation on the updated ClusterInstance.
	if err := ValidateClusterInstance(newClusterInstance); err != nil {
		log.Error(err, "Validation failed")
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	log.Info("Validations passed for update request")

	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (v *clusterInstanceValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
