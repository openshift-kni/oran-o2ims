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

// CheckReferencesFunc reports an error when the named HardwareProfile is still
// referenced by a ClusterTemplate or ProvisioningRequest, which blocks its
// deletion. It is injected at webhook setup time: the reference check must
// import the provisioning API group, which itself imports this package, so
// implementing it here would create an import cycle. The implementation lives
// in internal/controllers/utils, which can import both API groups.
//
// +kubebuilder:object:generate=false
type CheckReferencesFunc func(ctx context.Context, reader client.Reader, hpName, hpNamespace string) error

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *HardwareProfile) SetupWebhookWithManager(mgr ctrl.Manager, checkReferences CheckReferencesFunc) error {
	// nolint:wrapcheck
	return ctrl.NewWebhookManagedBy(mgr, &HardwareProfile{}).
		WithValidator(&hardwareProfileValidator{
			Client:          mgr.GetClient(),
			Reader:          mgr.GetAPIReader(),
			CheckReferences: checkReferences,
		}).
		Complete()
}

//+kubebuilder:webhook:path=/validate-clcm-openshift-io-v1alpha1-hardwareprofile,mutating=false,failurePolicy=fail,sideEffects=None,groups=clcm.openshift.io,resources=hardwareprofiles,verbs=create;delete,versions=v1alpha1,name=hardwareprofiles.clcm.openshift.io,admissionReviewVersions=v1

type hardwareProfileValidator struct {
	client.Client
	// Reader is an uncached API reader used when checking whether a
	// HardwareProfile is still referenced before allowing its deletion. The
	// cached client can lag the API server, and staleness here fails open: a
	// newly created ClusterTemplate or ProvisioningRequest that is not yet in
	// the cache would let a referenced HardwareProfile be deleted. Reading
	// directly from the API server avoids that window.
	Reader client.Reader
	// CheckReferences blocks deletion of a still-referenced HardwareProfile.
	// See CheckReferencesFunc for why it is injected rather than implemented
	// in this package.
	CheckReferences CheckReferencesFunc
}

var _ admission.Validator[*HardwareProfile] = &hardwareProfileValidator{}

// ValidateCreate implements admission.Validator
func (v *hardwareProfileValidator) ValidateCreate(ctx context.Context, hp *HardwareProfile) (admission.Warnings, error) {
	hardwareprofilelog.Info("validate create", "name", hp.Name)

	return nil, v.validateFirmware(ctx, hp)
}

// ValidateUpdate implements admission.Validator.
//
// The HardwareProfile spec is immutable (enforced by the CEL rule on the type),
// so there is nothing to validate on update. This method is retained only to
// satisfy the admission.Validator interface.
func (v *hardwareProfileValidator) ValidateUpdate(_ context.Context, _, newHP *HardwareProfile) (admission.Warnings, error) {
	hardwareprofilelog.Info("validate update (no-op, spec is immutable)", "name", newHP.Name)
	return nil, nil
}

// ValidateDelete implements admission.Validator. It blocks deletion of a
// HardwareProfile that is still referenced by a ClusterTemplate or
// ProvisioningRequest.
func (v *hardwareProfileValidator) ValidateDelete(ctx context.Context, hp *HardwareProfile) (admission.Warnings, error) {
	hardwareprofilelog.Info("validate delete", "name", hp.Name)

	if v.CheckReferences == nil {
		return nil, nil
	}
	return nil, v.CheckReferences(ctx, v.Reader, hp.Name, hp.Namespace)
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
