/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var firmwarecataloglog = logf.Log.WithName("firmwarecatalog-webhook")

// SetupFirmwareCatalogWebhookWithManager sets up the validating webhook for FirmwareCatalog
func SetupFirmwareCatalogWebhookWithManager(mgr ctrl.Manager) error {
	// nolint:wrapcheck
	return ctrl.NewWebhookManagedBy(mgr).
		For(&FirmwareCatalog{}).
		WithValidator(&firmwareCatalogValidator{Client: mgr.GetClient()}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-clcm-openshift-io-v1alpha1-firmwarecatalog,mutating=false,failurePolicy=fail,sideEffects=None,groups=clcm.openshift.io,resources=firmwarecatalogs,verbs=update,versions=v1alpha1,name=firmwarecatalogs.clcm.openshift.io,admissionReviewVersions=v1

type firmwareCatalogValidator struct {
	client.Client
}

var _ webhook.CustomValidator = &firmwareCatalogValidator{}

func (v *firmwareCatalogValidator) ValidateCreate(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (v *firmwareCatalogValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldCatalog, ok := oldObj.(*FirmwareCatalog)
	if !ok {
		return nil, fmt.Errorf("expected a FirmwareCatalog but got a %T", oldObj)
	}
	newCatalog, ok := newObj.(*FirmwareCatalog)
	if !ok {
		return nil, fmt.Errorf("expected a FirmwareCatalog but got a %T", newObj)
	}

	firmwarecataloglog.Info("validate update", "name", oldCatalog.Name)

	removedEntries := findRemovedEntries(oldCatalog.Spec.Images, newCatalog.Spec.Images)
	if len(removedEntries) == 0 {
		return nil, nil
	}

	profiles := &HardwareProfileList{}
	if err := v.Client.List(ctx, profiles, client.InNamespace(oldCatalog.Namespace)); err != nil {
		return nil, fmt.Errorf("failed to list HardwareProfiles: %w", err)
	}

	var referencedEntries []string
	for _, entryName := range removedEntries {
		for _, profile := range profiles.Items {
			if isEntryReferencedByProfile(entryName, &profile) {
				referencedEntries = append(referencedEntries, fmt.Sprintf(
					"%q (referenced by HardwareProfile %q)", entryName, profile.Name))
				break
			}
		}
	}

	if len(referencedEntries) > 0 {
		return nil, fmt.Errorf("cannot remove firmware catalog entries that are still referenced by HardwareProfiles: %s",
			strings.Join(referencedEntries, ", "))
	}

	return nil, nil
}

func (v *firmwareCatalogValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func findRemovedEntries(oldImages, newImages []FirmwareImage) []string {
	newSet := make(map[string]struct{}, len(newImages))
	for _, img := range newImages {
		newSet[img.Name] = struct{}{}
	}

	var removed []string
	for _, img := range oldImages {
		if _, exists := newSet[img.Name]; !exists {
			removed = append(removed, img.Name)
		}
	}
	return removed
}

// isEntryReferencedByProfile checks whether a firmware catalog entry is
// referenced by the given HardwareProfile. In Phase 1 of the firmware catalog
// rollout, HardwareProfiles still use inline firmware structs and do not
// reference catalog entries by name, so this always returns false. Phase 2
// will change the HardwareProfile fields to string references and update this
// function accordingly.
func isEntryReferencedByProfile(_ string, _ *HardwareProfile) bool {
	return false
}
