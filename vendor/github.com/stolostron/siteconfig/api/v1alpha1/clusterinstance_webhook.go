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

	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// logger for this package
var clusterInstanceLogger = logf.Log.WithName("clusterinstance-webhook")

// Validation error message
const validationFailedMsg = "validation failed"

// clusterInstanceValidator handles validation for ClusterInstance resources.
type clusterInstanceValidator struct{}

// Ensure clusterInstanceValidator implements the admission.Validator interface.
var _ admission.Validator[*ClusterInstance] = &clusterInstanceValidator{}

//nolint:lll
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-siteconfig-open-cluster-management-io-v1alpha1-clusterinstance,mutating=false,failurePolicy=fail,sideEffects=None,groups=siteconfig.open-cluster-management.io,resources=clusterinstances,verbs=create;update;delete,versions=v1alpha1,name=clusterinstances.siteconfig.open-cluster-management.io,admissionReviewVersions=v1

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *ClusterInstance) SetupWebhookWithManager(mgr ctrl.Manager) error {
	err := ctrl.NewWebhookManagedBy(mgr, &ClusterInstance{}).
		WithValidator(&clusterInstanceValidator{}).
		Complete()
	if err != nil {
		return fmt.Errorf("encountered an error creating a new webhook builder for ClusterInstance: %w", err)
	}
	return nil
}

// ValidateCreate checks if the ClusterInstance object is valid during creation.
func (v *clusterInstanceValidator) ValidateCreate(ctx context.Context, clusterInstance *ClusterInstance,
) (admission.Warnings, error) {
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
		log.Error(err, validationFailedMsg)
		return nil, fmt.Errorf("%s: %w", validationFailedMsg, err)
	}

	log.Info("Validations passed for create request")
	return nil, nil
}

// ValidateUpdate validates updates to a ClusterInstance object.
func (v *clusterInstanceValidator) ValidateUpdate(
	ctx context.Context, oldClusterInstance, newClusterInstance *ClusterInstance,
) (admission.Warnings, error) {
	log := clusterInstanceLogger.WithValues(
		"name", newClusterInstance.Name,
		"namespace", newClusterInstance.Namespace,
		"resourceVersion", newClusterInstance.ResourceVersion)

	log.Info("validating update request")

	// Allow updates if the object is being deleted (finalizer removal case).
	if !newClusterInstance.DeletionTimestamp.IsZero() {
		return nil, nil
	}

	// HoldInstallation can ONLY be set to true during CREATE, not UPDATE
	// Block any attempt to change HoldInstallation from false to true
	if !oldClusterInstance.Spec.HoldInstallation && newClusterInstance.Spec.HoldInstallation {
		log.Error(nil, "HoldInstallation can only be set to true during creation")
		return nil, errors.New("holdInstallation can only be set to true during creation, not during updates")
	}

	// Determine what type of spec changes are allowed based on current state
	permission := determineSpecChangePermission(oldClusterInstance, newClusterInstance)
	log.Info(fmt.Sprintf("Spec change permission level: %s", permission.String()))

	// If changes are blocked, reject any spec modifications
	if permission.IsBlocked() {
		if hasSpecChanged(oldClusterInstance, newClusterInstance) {
			log.Error(nil, "Spec changes not allowed during provisioning or reinstall")
			return nil, errors.New("spec update not allowed during provisioning or cluster reinstalls")
		}
		log.Info("Provisioning or Cluster Reinstall is in progress - no spec changes allowed")
		return nil, nil
	}

	// If all changes are allowed (HoldInstallation enabled), skip field-level validation
	if permission.AllowsAllChanges() {
		log.Info("HoldInstallation enabled - allowing all spec changes")
		// Still need to run general ClusterInstance validation
		if err := ValidateClusterInstance(newClusterInstance); err != nil {
			log.Error(err, validationFailedMsg)
			return nil, fmt.Errorf("%s: %w", validationFailedMsg, err)
		}
		log.Info("Validations passed for update request")
		return nil, nil
	}

	// Partial changes allowed - validate specific fields based on permission level
	if isProvisioningCompleted(newClusterInstance) {
		reinstallRequested := (permission == SpecChangeReinstall)

		if reinstallRequested {
			// Additional validation for reinstall requests
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

		// Validate allowed day-N changes based on permission level
		err := validatePostProvisioningChanges(log, oldClusterInstance, newClusterInstance, reinstallRequested)
		if err != nil {
			log.Error(err, "Invalid spec changes detected")
			return nil, fmt.Errorf("invalid spec changes detected: %w", err)
		}
	}

	// Perform general validation on the updated ClusterInstance
	if err := ValidateClusterInstance(newClusterInstance); err != nil {
		log.Error(err, validationFailedMsg)
		return nil, fmt.Errorf("%s: %w", validationFailedMsg, err)
	}

	log.Info("Validations passed for update request")
	return nil, nil
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type
func (v *clusterInstanceValidator) ValidateDelete(
	ctx context.Context, obj *ClusterInstance,
) (admission.Warnings, error) {
	return nil, nil
}
