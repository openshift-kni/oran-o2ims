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
var ocloudsitelog = logf.Log.WithName("ocloudsite-webhook")

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *OCloudSite) SetupWebhookWithManager(mgr ctrl.Manager) error {
	// nolint:wrapcheck
	return ctrl.NewWebhookManagedBy(mgr).
		For(&OCloudSite{}).
		WithValidator(&ocloudSiteValidator{Client: mgr.GetClient()}).
		Complete()
}

// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-ocloud-openshift-io-v1alpha1-ocloudsite,mutating=false,failurePolicy=ignore,sideEffects=None,groups=ocloud.openshift.io,resources=ocloudsites,verbs=create;update,versions=v1alpha1,name=ocloudsites.ocloud.openshift.io,admissionReviewVersions=v1

// ocloudSiteValidator is a webhook validator for OCloudSite
type ocloudSiteValidator struct {
	client.Client
}

var _ webhook.CustomValidator = &ocloudSiteValidator{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (v *ocloudSiteValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	site, ok := obj.(*OCloudSite)
	if !ok {
		return nil, fmt.Errorf("expected an OCloudSite but got a %T", obj)
	}
	ocloudsitelog.Info("validate create", "name", site.Name, "siteId", site.Spec.SiteID)

	if err := v.validateNoDuplicate(ctx, site, ""); err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (v *ocloudSiteValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldSite, ok := oldObj.(*OCloudSite)
	if !ok {
		return nil, fmt.Errorf("expected an OCloudSite but got a %T", oldObj)
	}
	newSite, ok := newObj.(*OCloudSite)
	if !ok {
		return nil, fmt.Errorf("expected an OCloudSite but got a %T", newObj)
	}
	ocloudsitelog.Info("validate update", "name", newSite.Name, "siteId", newSite.Spec.SiteID)

	// Only validate if siteId changed
	if oldSite.Spec.SiteID != newSite.Spec.SiteID {
		if err := v.validateNoDuplicate(ctx, newSite, oldSite.Spec.SiteID); err != nil {
			return nil, err
		}
	}

	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (v *ocloudSiteValidator) ValidateDelete(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	// No validation on delete
	return nil, nil
}

// validateNoDuplicate checks if another OCloudSite with the same siteId exists.
// excludeOldID is used during updates to exclude the old ID from duplicate check.
func (v *ocloudSiteValidator) validateNoDuplicate(ctx context.Context, site *OCloudSite, excludeOldID string) error {
	var siteList OCloudSiteList
	if err := v.List(ctx, &siteList); err != nil {
		ocloudsitelog.Error(err, "failed to list OCloudSites")
		// On error, allow the request (fail-open) - controller will catch it
		return nil
	}

	for _, existing := range siteList.Items {
		// Skip self
		if existing.Name == site.Name && existing.Namespace == site.Namespace {
			continue
		}
		// Skip if this is the old ID being changed from
		if excludeOldID != "" && existing.Spec.SiteID == excludeOldID {
			continue
		}
		// Check for duplicate
		if existing.Spec.SiteID == site.Spec.SiteID {
			return fmt.Errorf("siteId '%s' is already used by OCloudSite '%s'",
				site.Spec.SiteID, existing.Name)
		}
	}

	return nil
}
