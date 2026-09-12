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

	return nil, v.validateFirmware(ctx, hp)
}

// ValidateUpdate implements admission.Validator
func (v *hardwareProfileValidator) ValidateUpdate(ctx context.Context, _, newHP *HardwareProfile) (admission.Warnings, error) {
	hardwareprofilelog.Info("validate update", "name", newHP.Name)
	return nil, v.validateFirmware(ctx, newHP)
}

// ValidateDelete implements admission.Validator
func (v *hardwareProfileValidator) ValidateDelete(_ context.Context, _ *HardwareProfile) (admission.Warnings, error) {
	return nil, nil
}

// validateFirmware validates the HardwareProfile firmware configuration.
//
// A HardwareProfile may specify firmware using one of two mutually exclusive
// approaches:
//   - the recommended FirmwareImages list, which references FirmwareCatalog
//     entries by name; or
//   - the deprecated inline BiosFirmware/BmcFirmware/NicFirmware fields.
//
// Setting both approaches at once is rejected. When FirmwareImages is used,
// every entry must exist in the singleton FirmwareCatalog, and at most one
// entry may resolve to component "bios" and at most one to component "bmc".
// The deprecated inline fields carry their own URL/version and require no
// catalog validation.
func (v *hardwareProfileValidator) validateFirmware(ctx context.Context, hp *HardwareProfile) error {
	hasInline := hasInlineFirmware(hp)
	hasImages := len(hp.Spec.FirmwareImages) > 0

	if hasInline && hasImages {
		return fmt.Errorf("firmwareImages is mutually exclusive with the deprecated " +
			"biosFirmware, bmcFirmware, and nicFirmware fields; set only one approach")
	}

	if !hasImages {
		// Nothing to resolve: either no firmware is configured, or only the
		// deprecated inline fields (which carry their own URL/version) are set.
		return nil
	}

	return v.validateFirmwareImages(ctx, hp)
}

// validateFirmwareImages checks that every entry in spec.firmwareImages exists
// in the singleton FirmwareCatalog and that the resolved component types are
// consistent (at most one bios, at most one bmc; multiple nic allowed).
func (v *hardwareProfileValidator) validateFirmwareImages(ctx context.Context, hp *HardwareProfile) error {
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
	var biosCount, bmcCount int
	seen := make(map[string]struct{}, len(hp.Spec.FirmwareImages))

	for _, name := range hp.Spec.FirmwareImages {
		if _, dup := seen[name]; dup {
			errs = append(errs, fmt.Sprintf("firmwareImages contains duplicate entry %q", name))
			continue
		}
		seen[name] = struct{}{}

		img, ok := imageMap[name]
		if !ok {
			errs = append(errs, fmt.Sprintf("firmwareImages entry %q not found in FirmwareCatalog", name))
			continue
		}
		switch img.Component {
		case ComponentBIOS:
			biosCount++
		case ComponentBMC:
			bmcCount++
		case ComponentNIC:
			// Multiple NIC entries are allowed.
		default:
			errs = append(errs, fmt.Sprintf("firmwareImages entry %q has unsupported component %q", name, img.Component))
		}
	}

	if biosCount > 1 {
		errs = append(errs, fmt.Sprintf("at most one firmwareImages entry with component %q is allowed, found %d", ComponentBIOS, biosCount))
	}
	if bmcCount > 1 {
		errs = append(errs, fmt.Sprintf("at most one firmwareImages entry with component %q is allowed, found %d", ComponentBMC, bmcCount))
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid firmware references: %s", strings.Join(errs, "; "))
	}

	return nil
}

// hasInlineFirmware reports whether any of the deprecated inline firmware
// fields are populated.
func hasInlineFirmware(hp *HardwareProfile) bool {
	return !hp.Spec.BiosFirmware.IsEmpty() || !hp.Spec.BmcFirmware.IsEmpty() || len(hp.Spec.NicFirmware) > 0
}
