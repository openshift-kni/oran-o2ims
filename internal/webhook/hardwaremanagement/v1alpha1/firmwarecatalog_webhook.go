/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"context"
	"fmt"
	"strings"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var firmwarecataloglog = logf.Log.WithName("firmwarecatalog-webhook")

// SetupFirmwareCatalogWebhookWithManager registers the FirmwareCatalog validation webhook.
func SetupFirmwareCatalogWebhookWithManager(mgr ctrl.Manager) error {
	// nolint:wrapcheck
	return ctrl.NewWebhookManagedBy(mgr, &hwmgmtv1alpha1.FirmwareCatalog{}).
		WithValidator(&firmwareCatalogValidator{
			Client: mgr.GetClient(),
			Reader: mgr.GetAPIReader(),
		}).
		Complete()
}

//+kubebuilder:webhook:path=/validate-clcm-openshift-io-v1alpha1-firmwarecatalog,mutating=false,failurePolicy=fail,sideEffects=None,groups=clcm.openshift.io,resources=firmwarecatalogs,verbs=update;delete,versions=v1alpha1,name=firmwarecatalogs.clcm.openshift.io,admissionReviewVersions=v1

type firmwareCatalogValidator struct {
	client.Client
	// Reader is an uncached API reader used to list HardwareProfiles when
	// validating entry removal and catalog deletion. The cached client can
	// lag behind the API server, and here staleness fails open: a
	// newly created HardwareProfile that is not yet in the cache would let a
	// referenced entry be removed. Reading directly from the API server
	// avoids that window.
	Reader client.Reader
}

var _ admission.Validator[*hwmgmtv1alpha1.FirmwareCatalog] = &firmwareCatalogValidator{}

// ValidateCreate implements admission.Validator
func (v *firmwareCatalogValidator) ValidateCreate(_ context.Context, _ *hwmgmtv1alpha1.FirmwareCatalog) (admission.Warnings, error) {
	return nil, nil
}

// ValidateUpdate implements admission.Validator
func (v *firmwareCatalogValidator) ValidateUpdate(ctx context.Context, oldCatalog, newCatalog *hwmgmtv1alpha1.FirmwareCatalog) (admission.Warnings, error) {
	firmwarecataloglog.Info("validate update", "name", oldCatalog.Name)

	if modified := findModifiedImmutableFields(oldCatalog.Spec.Images, newCatalog.Spec.Images); len(modified) > 0 {
		return nil, fmt.Errorf("firmware catalog entries are immutable: %s", strings.Join(modified, "; "))
	}

	removed := findRemovedEntries(oldCatalog.Spec.Images, newCatalog.Spec.Images)
	if len(removed) == 0 {
		return nil, nil
	}

	hwProfiles := &hwmgmtv1alpha1.HardwareProfileList{}
	if err := v.Reader.List(ctx, hwProfiles, client.InNamespace(oldCatalog.Namespace)); err != nil {
		return nil, fmt.Errorf("failed to list HardwareProfiles: %w", err)
	}

	referencedNames := buildReferencedEntryNames(hwProfiles.Items)
	var referenced []string
	for _, name := range removed {
		if _, ok := referencedNames[name]; ok {
			referenced = append(referenced, name)
		}
	}

	if len(referenced) > 0 {
		return nil, fmt.Errorf("cannot remove firmware catalog entries still referenced by HardwareProfiles: %s",
			strings.Join(referenced, ", "))
	}

	return nil, nil
}

// ValidateDelete implements admission.Validator
func (v *firmwareCatalogValidator) ValidateDelete(ctx context.Context, catalog *hwmgmtv1alpha1.FirmwareCatalog) (admission.Warnings, error) {
	firmwarecataloglog.Info("validate delete", "name", catalog.Name)

	hwProfiles := &hwmgmtv1alpha1.HardwareProfileList{}
	if err := v.Reader.List(ctx, hwProfiles, client.InNamespace(catalog.Namespace)); err != nil {
		return nil, fmt.Errorf("failed to list HardwareProfiles: %w", err)
	}

	referencedNames := buildReferencedEntryNames(hwProfiles.Items)
	var referenced []string
	for _, img := range catalog.Spec.Images {
		if _, ok := referencedNames[img.Name]; ok {
			referenced = append(referenced, img.Name)
		}
	}

	if len(referenced) > 0 {
		return nil, fmt.Errorf("cannot delete FirmwareCatalog: entries still referenced by HardwareProfiles: %s",
			strings.Join(referenced, ", "))
	}

	return nil, nil
}

// findRemovedEntries returns the names of entries present in old but absent from updated.
func findRemovedEntries(old, updated []hwmgmtv1alpha1.FirmwareImage) []string {
	newNames := make(map[string]struct{}, len(updated))
	for _, img := range updated {
		newNames[img.Name] = struct{}{}
	}

	var removed []string
	for _, img := range old {
		if _, exists := newNames[img.Name]; !exists {
			removed = append(removed, img.Name)
		}
	}
	return removed
}

// findModifiedImmutableFields compares entries that exist in both old and updated lists
// and returns descriptions of any immutable field changes (component, url, version, vendor).
func findModifiedImmutableFields(old, updated []hwmgmtv1alpha1.FirmwareImage) []string {
	oldByName := make(map[string]hwmgmtv1alpha1.FirmwareImage, len(old))
	for _, img := range old {
		oldByName[img.Name] = img
	}

	var violations []string
	for _, cur := range updated {
		prev, exists := oldByName[cur.Name]
		if !exists {
			continue
		}
		if cur.Component != prev.Component {
			violations = append(violations, fmt.Sprintf("%q: component is immutable", cur.Name))
		}
		if cur.URL != prev.URL {
			violations = append(violations, fmt.Sprintf("%q: url is immutable", cur.Name))
		}
		if cur.Version != prev.Version {
			violations = append(violations, fmt.Sprintf("%q: version is immutable", cur.Name))
		}
		if cur.Vendor != prev.Vendor {
			violations = append(violations, fmt.Sprintf("%q: vendor is immutable", cur.Name))
		}
	}
	return violations
}

// buildReferencedEntryNames returns the set of FirmwareCatalog entry names
// referenced by any HardwareProfile's firmwareImages list, computed in a single
// pass over all profiles. Callers can then test membership in O(1) instead of
// rescanning every profile per candidate entry. Only the firmwareImages approach
// references catalog entries by name; the deprecated inline
// BiosFirmware/BmcFirmware/NicFirmware fields carry their own URL and version and
// therefore create no dependency on the catalog.
func buildReferencedEntryNames(profiles []hwmgmtv1alpha1.HardwareProfile) map[string]struct{} {
	referenced := make(map[string]struct{})
	for i := range profiles {
		for _, ref := range profiles[i].Spec.FirmwareImages {
			referenced[ref] = struct{}{}
		}
	}
	return referenced
}
