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
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const firmwareCatalogName = "firmware-catalog"

var hardwareprofilelog = logf.Log.WithName("hardwareprofile-webhook")

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *HardwareProfile) SetupWebhookWithManager(mgr ctrl.Manager) error {
	// nolint:wrapcheck
	return ctrl.NewWebhookManagedBy(mgr).
		For(&HardwareProfile{}).
		WithValidator(&hardwareProfileValidator{Client: mgr.GetClient()}).
		Complete()
}

//+kubebuilder:webhook:path=/validate-clcm-openshift-io-v1alpha1-hardwareprofile,mutating=false,failurePolicy=fail,sideEffects=None,groups=clcm.openshift.io,resources=hardwareprofiles,verbs=create,versions=v1alpha1,name=hardwareprofiles.clcm.openshift.io,admissionReviewVersions=v1

type hardwareProfileValidator struct {
	client.Client
}

var _ webhook.CustomValidator = &hardwareProfileValidator{}

// ValidateCreate implements webhook.CustomValidator
func (v *hardwareProfileValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	hp, ok := obj.(*HardwareProfile)
	if !ok {
		return nil, fmt.Errorf("expected a HardwareProfile but got a %T", obj)
	}
	hardwareprofilelog.Info("validate create", "name", hp.Name)

	return nil, v.validateFirmwareReferences(ctx, hp)
}

// ValidateUpdate implements webhook.CustomValidator
func (v *hardwareProfileValidator) ValidateUpdate(_ context.Context, _, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator
func (v *hardwareProfileValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// validateFirmwareReferences checks that all firmware references in the
// HardwareProfile exist in the singleton FirmwareCatalog and have the
// correct component type.
func (v *hardwareProfileValidator) validateFirmwareReferences(ctx context.Context, hp *HardwareProfile) error {
	refs := collectFirmwareReferences(hp)
	if len(refs) == 0 {
		return nil
	}

	catalog := &FirmwareCatalog{}
	if err := v.Client.Get(ctx, types.NamespacedName{
		Name: firmwareCatalogName, Namespace: hp.Namespace,
	}, catalog); err != nil {
		return fmt.Errorf("failed to get FirmwareCatalog: %w", err)
	}

	imageMap := make(map[string]FirmwareImage, len(catalog.Spec.Images))
	for _, img := range catalog.Spec.Images {
		imageMap[img.Name] = img
	}

	var errs []string

	if hp.Spec.BiosFirmware != "" {
		if img, ok := imageMap[hp.Spec.BiosFirmware]; !ok {
			errs = append(errs, fmt.Sprintf("biosFirmware entry %q not found in FirmwareCatalog", hp.Spec.BiosFirmware))
		} else if img.Component != "bios" {
			errs = append(errs, fmt.Sprintf("biosFirmware entry %q has component %q, expected bios", hp.Spec.BiosFirmware, img.Component))
		}
	}

	if hp.Spec.BmcFirmware != "" {
		if img, ok := imageMap[hp.Spec.BmcFirmware]; !ok {
			errs = append(errs, fmt.Sprintf("bmcFirmware entry %q not found in FirmwareCatalog", hp.Spec.BmcFirmware))
		} else if img.Component != "bmc" {
			errs = append(errs, fmt.Sprintf("bmcFirmware entry %q has component %q, expected bmc", hp.Spec.BmcFirmware, img.Component))
		}
	}

	for _, name := range hp.Spec.NicFirmware {
		if img, ok := imageMap[name]; !ok {
			errs = append(errs, fmt.Sprintf("nicFirmware entry %q not found in FirmwareCatalog", name))
		} else if img.Component != "nic" {
			errs = append(errs, fmt.Sprintf("nicFirmware entry %q has component %q, expected nic", name, img.Component))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid firmware references: %s", strings.Join(errs, "; "))
	}

	return nil
}

// collectFirmwareReferences returns all non-empty firmware reference names
// from a HardwareProfile.
func collectFirmwareReferences(hp *HardwareProfile) []string {
	var refs []string
	if hp.Spec.BiosFirmware != "" {
		refs = append(refs, hp.Spec.BiosFirmware)
	}
	if hp.Spec.BmcFirmware != "" {
		refs = append(refs, hp.Spec.BmcFirmware)
	}
	refs = append(refs, hp.Spec.NicFirmware...)
	return refs
}
