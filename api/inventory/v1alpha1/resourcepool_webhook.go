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
var resourcepoollog = logf.Log.WithName("resourcepool-webhook")

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *ResourcePool) SetupWebhookWithManager(mgr ctrl.Manager) error {
	// nolint:wrapcheck
	return ctrl.NewWebhookManagedBy(mgr).
		For(&ResourcePool{}).
		WithValidator(&resourcePoolValidator{Client: mgr.GetClient()}).
		Complete()
}

// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-ocloud-openshift-io-v1alpha1-resourcepool,mutating=false,failurePolicy=ignore,sideEffects=None,groups=ocloud.openshift.io,resources=resourcepools,verbs=create;update,versions=v1alpha1,name=resourcepools.ocloud.openshift.io,admissionReviewVersions=v1

// resourcePoolValidator is a webhook validator for ResourcePool
type resourcePoolValidator struct {
	client.Client
}

var _ webhook.CustomValidator = &resourcePoolValidator{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (v *resourcePoolValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	pool, ok := obj.(*ResourcePool)
	if !ok {
		return nil, fmt.Errorf("expected a ResourcePool but got a %T", obj)
	}
	resourcepoollog.Info("validate create", "name", pool.Name, "resourcePoolId", pool.Spec.ResourcePoolId)

	if err := v.validateNoDuplicate(ctx, pool, ""); err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (v *resourcePoolValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldPool, ok := oldObj.(*ResourcePool)
	if !ok {
		return nil, fmt.Errorf("expected a ResourcePool but got a %T", oldObj)
	}
	newPool, ok := newObj.(*ResourcePool)
	if !ok {
		return nil, fmt.Errorf("expected a ResourcePool but got a %T", newObj)
	}
	resourcepoollog.Info("validate update", "name", newPool.Name, "resourcePoolId", newPool.Spec.ResourcePoolId)

	// Only validate if resourcePoolId changed
	if oldPool.Spec.ResourcePoolId != newPool.Spec.ResourcePoolId {
		if err := v.validateNoDuplicate(ctx, newPool, oldPool.Spec.ResourcePoolId); err != nil {
			return nil, err
		}
	}

	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (v *resourcePoolValidator) ValidateDelete(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	// No validation on delete
	return nil, nil
}

// validateNoDuplicate checks if another ResourcePool with the same resourcePoolId exists.
// excludeOldID is used during updates to exclude the old ID from duplicate check.
func (v *resourcePoolValidator) validateNoDuplicate(ctx context.Context, pool *ResourcePool, excludeOldID string) error {
	var poolList ResourcePoolList
	if err := v.List(ctx, &poolList); err != nil {
		resourcepoollog.Error(err, "failed to list ResourcePools")
		// On error, allow the request (fail-open) - controller will catch it
		return nil
	}

	for _, existing := range poolList.Items {
		// Skip self
		if existing.Name == pool.Name && existing.Namespace == pool.Namespace {
			continue
		}
		// Skip if this is the old ID being changed from
		if excludeOldID != "" && existing.Spec.ResourcePoolId == excludeOldID {
			continue
		}
		// Check for duplicate
		if existing.Spec.ResourcePoolId == pool.Spec.ResourcePoolId {
			return fmt.Errorf("resourcePoolId '%s' is already used by ResourcePool '%s'",
				pool.Spec.ResourcePoolId, existing.Name)
		}
	}

	return nil
}
