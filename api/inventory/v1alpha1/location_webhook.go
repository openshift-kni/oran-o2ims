/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var locationlog = logf.Log.WithName("location-webhook")

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *Location) SetupWebhookWithManager(mgr ctrl.Manager) error {
	// nolint:wrapcheck
	return ctrl.NewWebhookManagedBy(mgr).
		For(&Location{}).
		WithValidator(&locationValidator{Client: mgr.GetClient()}).
		Complete()
}

// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-ocloud-openshift-io-v1alpha1-location,mutating=false,failurePolicy=ignore,sideEffects=None,groups=ocloud.openshift.io,resources=locations,verbs=create;update,versions=v1alpha1,name=locations.ocloud.openshift.io,admissionReviewVersions=v1

// locationValidator is a webhook validator for Location
type locationValidator struct {
	client.Client
}

var _ webhook.CustomValidator = &locationValidator{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (v *locationValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	location, ok := obj.(*Location)
	if !ok {
		return nil, fmt.Errorf("expected a Location but got a %T", obj)
	}
	locationlog.Info("validate create", "name", location.Name, "globalLocationId", location.Spec.GlobalLocationID)

	if err := v.validateNoDuplicate(ctx, location, ""); err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (v *locationValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldLocation, ok := oldObj.(*Location)
	if !ok {
		return nil, fmt.Errorf("expected a Location but got a %T", oldObj)
	}
	newLocation, ok := newObj.(*Location)
	if !ok {
		return nil, fmt.Errorf("expected a Location but got a %T", newObj)
	}
	locationlog.Info("validate update", "name", newLocation.Name, "globalLocationId", newLocation.Spec.GlobalLocationID)

	// Only validate if globalLocationId changed
	if oldLocation.Spec.GlobalLocationID != newLocation.Spec.GlobalLocationID {
		if err := v.validateNoDuplicate(ctx, newLocation, oldLocation.Spec.GlobalLocationID); err != nil {
			return nil, err
		}
	}

	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (v *locationValidator) ValidateDelete(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	// No validation on delete
	return nil, nil
}

// validateNoDuplicate checks if another Location with the same globalLocationId exists.
// excludeOldID is used during updates to exclude the old ID from duplicate check.
func (v *locationValidator) validateNoDuplicate(ctx context.Context, location *Location, excludeOldID string) error {
	var locationList LocationList
	if err := v.List(ctx, &locationList); err != nil {
		locationlog.Error(err, "failed to list Locations")
		// On error, allow the request (fail-open) - controller will catch it
		return nil
	}

	for _, existing := range locationList.Items {
		// Skip self
		if existing.Name == location.Name && existing.Namespace == location.Namespace {
			continue
		}
		// Skip if this is the old ID being changed from
		if excludeOldID != "" && existing.Spec.GlobalLocationID == excludeOldID {
			continue
		}
		// Check for duplicate
		if existing.Spec.GlobalLocationID == location.Spec.GlobalLocationID {
			return fmt.Errorf("globalLocationId '%s' is already used by Location '%s'",
				location.Spec.GlobalLocationID, existing.Name)
		}
	}

	return nil
}
