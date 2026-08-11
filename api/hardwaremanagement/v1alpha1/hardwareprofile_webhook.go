/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var hardwareprofilelog = logf.Log.WithName("hardwareprofile-webhook")

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *HardwareProfile) SetupWebhookWithManager(mgr ctrl.Manager) error {
	// nolint:wrapcheck
	return ctrl.NewWebhookManagedBy(mgr, &HardwareProfile{}).
		WithValidator(&hardwareProfileValidator{Client: mgr.GetClient()}).
		Complete()
}

//+kubebuilder:webhook:path=/validate-clcm-openshift-io-v1alpha1-hardwareprofile,mutating=false,failurePolicy=fail,sideEffects=None,groups=clcm.openshift.io,resources=hardwareprofiles,verbs=create;update,versions=v1alpha1,name=hardwareprofiles.clcm.openshift.io,admissionReviewVersions=v1

type hardwareProfileValidator struct {
	client.Client
}

var _ admission.Validator[*HardwareProfile] = &hardwareProfileValidator{}

// ValidateCreate implements admission.Validator
func (v *hardwareProfileValidator) ValidateCreate(ctx context.Context, hp *HardwareProfile) (admission.Warnings, error) {
	hardwareprofilelog.Info("validate create", "name", hp.Name)

	return nil, v.validateFirmwareReferences(ctx, hp)
}

// ValidateUpdate implements admission.Validator
func (v *hardwareProfileValidator) ValidateUpdate(ctx context.Context, _, newHP *HardwareProfile) (admission.Warnings, error) {
	hardwareprofilelog.Info("validate update", "name", newHP.Name)
	return nil, v.validateFirmwareReferences(ctx, newHP)
}

// ValidateDelete implements admission.Validator
func (v *hardwareProfileValidator) ValidateDelete(_ context.Context, _ *HardwareProfile) (admission.Warnings, error) {
	return nil, nil
}

// validateFirmwareReferences checks that all firmware references in the
// HardwareProfile exist in the singleton FirmwareCatalog and have the
// correct component type.
func (v *hardwareProfileValidator) validateFirmwareReferences(ctx context.Context, hp *HardwareProfile) error {
	if !hasFirmwareReferences(hp) {
		return nil
	}

	catalog := &FirmwareCatalog{}
	if err := v.Client.Get(ctx, types.NamespacedName{
		Name: FirmwareCatalogName, Namespace: hp.Namespace,
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
		} else if img.Component != ComponentBIOS {
			errs = append(errs, fmt.Sprintf("biosFirmware entry %q has component %q, expected %s", hp.Spec.BiosFirmware, img.Component, ComponentBIOS))
		}
	}

	if hp.Spec.BmcFirmware != "" {
		if img, ok := imageMap[hp.Spec.BmcFirmware]; !ok {
			errs = append(errs, fmt.Sprintf("bmcFirmware entry %q not found in FirmwareCatalog", hp.Spec.BmcFirmware))
		} else if img.Component != ComponentBMC {
			errs = append(errs, fmt.Sprintf("bmcFirmware entry %q has component %q, expected %s", hp.Spec.BmcFirmware, img.Component, ComponentBMC))
		}
	}

	for _, name := range hp.Spec.NicFirmware {
		if img, ok := imageMap[name]; !ok {
			errs = append(errs, fmt.Sprintf("nicFirmware entry %q not found in FirmwareCatalog", name))
		} else if img.Component != ComponentNIC {
			errs = append(errs, fmt.Sprintf("nicFirmware entry %q has component %q, expected %s", name, img.Component, ComponentNIC))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid firmware references: %s", strings.Join(errs, "; "))
	}

	return nil
}

func hasFirmwareReferences(hp *HardwareProfile) bool {
	return hp.Spec.BiosFirmware != "" || hp.Spec.BmcFirmware != "" || len(hp.Spec.NicFirmware) > 0
}
