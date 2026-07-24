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

var hardwareprofilelog = logf.Log.WithName("hardwareprofile-webhook")

// SetupHardwareProfileWebhookWithManager sets up the validating webhook for HardwareProfile
func SetupHardwareProfileWebhookWithManager(mgr ctrl.Manager) error {
	// nolint:wrapcheck
	return ctrl.NewWebhookManagedBy(mgr).
		For(&HardwareProfile{}).
		WithValidator(&hardwareProfileValidator{Client: mgr.GetClient()}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-clcm-openshift-io-v1alpha1-hardwareprofile,mutating=false,failurePolicy=fail,sideEffects=None,groups=clcm.openshift.io,resources=hardwareprofiles,verbs=create,versions=v1alpha1,name=hardwareprofiles.clcm.openshift.io,admissionReviewVersions=v1

type hardwareProfileValidator struct {
	client.Client
}

var _ webhook.CustomValidator = &hardwareProfileValidator{}

func (v *hardwareProfileValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	hp, ok := obj.(*HardwareProfile)
	if !ok {
		return nil, fmt.Errorf("expected a HardwareProfile but got a %T", obj)
	}
	hardwareprofilelog.Info("validate create", "name", hp.Name)

	if !hasFirmwareReferences(hp) {
		return nil, nil
	}

	catalog := &FirmwareCatalog{}
	if err := v.Client.Get(ctx, types.NamespacedName{
		Name:      FirmwareCatalogName,
		Namespace: hp.Namespace,
	}, catalog); err != nil {
		return nil, fmt.Errorf("failed to get FirmwareCatalog: %w", err)
	}

	imageMap := make(map[string]FirmwareImage, len(catalog.Spec.Images))
	for _, img := range catalog.Spec.Images {
		imageMap[img.Name] = img
	}

	var errs []string

	if hp.Spec.BiosFirmware != "" {
		if err := validateFirmwareRef(imageMap, hp.Spec.BiosFirmware, "bios", "biosFirmware"); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if hp.Spec.BmcFirmware != "" {
		if err := validateFirmwareRef(imageMap, hp.Spec.BmcFirmware, "bmc", "bmcFirmware"); err != nil {
			errs = append(errs, err.Error())
		}
	}

	for i, name := range hp.Spec.NicFirmware {
		if err := validateFirmwareRef(imageMap, name, "nic", fmt.Sprintf("nicFirmware[%d]", i)); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid firmware references: %s", strings.Join(errs, "; "))
	}

	return nil, nil
}

func (v *hardwareProfileValidator) ValidateUpdate(_ context.Context, _, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (v *hardwareProfileValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func hasFirmwareReferences(hp *HardwareProfile) bool {
	return hp.Spec.BiosFirmware != "" || hp.Spec.BmcFirmware != "" || len(hp.Spec.NicFirmware) > 0
}

func validateFirmwareRef(imageMap map[string]FirmwareImage, entryName, expectedComponent, fieldName string) error {
	img, ok := imageMap[entryName]
	if !ok {
		return fmt.Errorf("%s: entry %q not found in FirmwareCatalog", fieldName, entryName)
	}
	if img.Component != expectedComponent {
		return fmt.Errorf("%s: entry %q has component %q, expected %q", fieldName, entryName, img.Component, expectedComponent)
	}
	return nil
}
