/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Generated-By: Cursor/claude-4-sonnet
*/

/*
Test Case Descriptions for HostFirmwareSettings Manager

This file contains unit tests for the HostFirmwareSettings manager functionality,
which handles BIOS settings configuration for bare metal hosts through Metal3.

Test Groups:

1. convertBiosSettingsToHostFirmware:
   - Tests conversion of BIOS settings from hardware management API to Metal3 HostFirmwareSettings
   - Validates proper mapping of attributes and metadata
   - Handles empty attributes gracefully

2. createHostFirmwareSettings:
   - Tests creation of new HostFirmwareSettings resources in Kubernetes
   - Verifies successful resource creation and persistence

3. getHostFirmwareSettings:
   - Tests retrieval of existing HostFirmwareSettings resources
   - Handles cases where resources exist and don't exist
   - Returns appropriate errors for missing resources

4. getOrCreateHostFirmwareSettings:
   - Tests the get-or-create pattern for HostFirmwareSettings
   - Returns existing resources when found
   - Creates new resources when not found

5. validSettings:
   - Tests validation of BIOS settings against firmware schema
   - Validates settings exist in the host status
   - Handles mixed valid/invalid settings scenarios
   - Returns appropriate validation errors

6. isChangeDetected:
   - Tests change detection logic between spec and status settings
   - Detects value changes in existing settings
   - Detects new settings added to spec
   - Handles empty spec maps correctly

7. isFirmwareSettingsChangeDetectedAndValid:
   - Tests checking of Metal3 condition status for firmware settings
   - Validates both ChangeDetected and Valid conditions are true
   - Handles missing HostFirmwareSettings resources

8. updateHostFirmwareSettings:
   - Tests updating existing HostFirmwareSettings resources
   - Verifies settings are properly updated in Kubernetes
   - Handles errors for non-existent resources

9. validateBiosSettings:
   - Tests comprehensive BIOS settings validation against firmware schema
   - Validates settings against allowable values from schema
   - Handles missing or invalid firmware schema references
   - Returns validation errors for invalid configurations

10. checkAndUpdateFirmwareSettings:
    - Tests the complete check-and-update workflow
    - Determines if updates are needed based on change detection
    - Performs updates when changes are detected
    - Skips updates when no changes are needed

11. IsBiosUpdateRequired (Main Public API):
    - Tests the main public function to determine if BIOS updates are required
    - Handles creation of new HostFirmwareSettings when none exist
    - Validates settings against firmware schema
    - Returns true when settings changes are detected and valid
    - Returns false when no changes are needed
    - Handles error cases like missing schema or invalid settings

These tests ensure reliable BIOS configuration management for bare metal hosts
in the O-RAN O2IMS infrastructure management system.
*/

package controller

import (
	"context"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
)

var _ = Describe("HostFirmwareSettings Manager", func() {
	var (
		ctx    context.Context
		scheme *runtime.Scheme
		logger *slog.Logger
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(metal3v1alpha1.AddToScheme(scheme)).To(Succeed())
		logger = slog.Default()
	})

	Describe("convertBiosSettingsToHostFirmware", func() {
		It("should convert basic BIOS settings to HostFirmwareSettings", func() {
			bmh := metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-host",
					Namespace: "test-namespace",
				},
			}
			biosSettings := hwmgmtv1alpha1.Bios{
				Attributes: map[string]intstr.IntOrString{
					"ProcTurboMode": intstr.FromString("Enabled"),
					"BootMode":      intstr.FromString("UEFI"),
				},
			}

			result := convertBiosSettingsToHostFirmware(bmh, biosSettings)

			Expect(result.Name).To(Equal("test-host"))
			Expect(result.Namespace).To(Equal("test-namespace"))
			Expect(result.Spec.Settings).To(Equal(metal3v1alpha1.DesiredSettingsMap(biosSettings.Attributes)))
		})

		It("should handle empty attributes", func() {
			bmh := metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-host-2",
					Namespace: "test-namespace-2",
				},
			}
			biosSettings := hwmgmtv1alpha1.Bios{
				Attributes: map[string]intstr.IntOrString{},
			}

			result := convertBiosSettingsToHostFirmware(bmh, biosSettings)

			Expect(result.Name).To(Equal("test-host-2"))
			Expect(result.Namespace).To(Equal("test-namespace-2"))
			Expect(result.Spec.Settings).To(BeEmpty())
		})
	})

	Describe("createHostFirmwareSettings", func() {
		It("should successfully create HostFirmwareSettings", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			hfs := &metal3v1alpha1.HostFirmwareSettings{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hfs",
					Namespace: "test-namespace",
				},
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: map[string]intstr.IntOrString{
						"ProcTurboMode": intstr.FromString("Enabled"),
					},
				},
			}

			err := createHostFirmwareSettings(ctx, fakeClient, logger, hfs)

			Expect(err).ToNot(HaveOccurred())

			// Verify the object was created
			created := &metal3v1alpha1.HostFirmwareSettings{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      hfs.Name,
				Namespace: hfs.Namespace,
			}, created)
			Expect(err).ToNot(HaveOccurred())
			Expect(created.Spec.Settings).To(Equal(hfs.Spec.Settings))
		})
	})

	Describe("getHostFirmwareSettings", func() {
		It("should return existing HostFirmwareSettings", func() {
			existingHFS := &metal3v1alpha1.HostFirmwareSettings{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "existing-hfs",
					Namespace: "test-namespace",
				},
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: map[string]intstr.IntOrString{
						"ProcTurboMode": intstr.FromString("Enabled"),
					},
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingHFS).Build()

			result, err := getHostFirmwareSettings(ctx, fakeClient, "existing-hfs", "test-namespace")

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.Name).To(Equal("existing-hfs"))
			Expect(result.Namespace).To(Equal("test-namespace"))
			Expect(result.Spec.Settings).To(Equal(existingHFS.Spec.Settings))
		})

		It("should return error when HostFirmwareSettings not found", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			result, err := getHostFirmwareSettings(ctx, fakeClient, "non-existent-hfs", "test-namespace")

			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
		})
	})

	Describe("getOrCreateHostFirmwareSettings", func() {
		It("should return existing HostFirmwareSettings when found", func() {
			existingHFS := &metal3v1alpha1.HostFirmwareSettings{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "existing-hfs",
					Namespace: "test-namespace",
				},
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: map[string]intstr.IntOrString{
						"ProcTurboMode": intstr.FromString("Enabled"),
					},
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingHFS).Build()

			hfs := &metal3v1alpha1.HostFirmwareSettings{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "existing-hfs",
					Namespace: "test-namespace",
				},
			}

			result, err := getOrCreateHostFirmwareSettings(ctx, fakeClient, logger, hfs)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.Name).To(Equal("existing-hfs"))
			Expect(result.Namespace).To(Equal("test-namespace"))
		})

		It("should create new HostFirmwareSettings when not found", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			hfs := &metal3v1alpha1.HostFirmwareSettings{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-hfs",
					Namespace: "test-namespace",
				},
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: map[string]intstr.IntOrString{
						"BootMode": intstr.FromString("UEFI"),
					},
				},
			}

			result, err := getOrCreateHostFirmwareSettings(ctx, fakeClient, logger, hfs)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.Name).To(Equal("new-hfs"))
			Expect(result.Namespace).To(Equal("test-namespace"))
		})
	})

	Describe("validSettings", func() {
		var hfs *metal3v1alpha1.HostFirmwareSettings
		var schema *metal3v1alpha1.FirmwareSchema

		BeforeEach(func() {
			hfs = &metal3v1alpha1.HostFirmwareSettings{
				Status: metal3v1alpha1.HostFirmwareSettingsStatus{
					Settings: map[string]string{
						"ProcTurboMode": "Enabled",
						"BootMode":      "UEFI",
						"ValidSetting":  "ValidValue",
					},
				},
			}

			schema = &metal3v1alpha1.FirmwareSchema{
				Spec: metal3v1alpha1.FirmwareSchemaSpec{
					Schema: map[string]metal3v1alpha1.SettingSchema{
						"ProcTurboMode": {
							AllowableValues: []string{"Enabled", "Disabled"},
						},
						"BootMode": {
							AllowableValues: []string{"UEFI", "Legacy"},
						},
					},
				},
			}
		})

		It("should return no errors for valid settings", func() {
			newSettings := map[string]intstr.IntOrString{
				"ProcTurboMode": intstr.FromString("Disabled"),
				"BootMode":      intstr.FromString("Legacy"),
			}

			errors := validSettings(hfs, schema, newSettings)

			Expect(errors).To(BeEmpty())
		})

		It("should return error for setting not in status", func() {
			newSettings := map[string]intstr.IntOrString{
				"NonExistentSetting": intstr.FromString("Value"),
			}

			errors := validSettings(hfs, schema, newSettings)

			Expect(errors).To(HaveLen(1))
			Expect(errors[0].Error()).To(ContainSubstring("not in the Status field"))
		})

		It("should handle mixed valid and invalid settings", func() {
			newSettings := map[string]intstr.IntOrString{
				"ProcTurboMode":      intstr.FromString("Enabled"),
				"NonExistentSetting": intstr.FromString("Value"),
			}

			errors := validSettings(hfs, schema, newSettings)

			Expect(errors).To(HaveLen(1))
			Expect(errors[0].Error()).To(ContainSubstring("not in the Status field"))
		})
	})

	Describe("isChangeDetected", func() {
		It("should return false when no changes detected", func() {
			specMap := map[string]intstr.IntOrString{
				"ProcTurboMode": intstr.FromString("Enabled"),
				"BootMode":      intstr.FromString("UEFI"),
			}
			statusMap := map[string]string{
				"ProcTurboMode": "Enabled",
				"BootMode":      "UEFI",
			}

			result := isChangeDetected(ctx, logger, specMap, statusMap)

			Expect(result).To(BeFalse())
		})

		It("should return true when change detected - different value", func() {
			specMap := map[string]intstr.IntOrString{
				"ProcTurboMode": intstr.FromString("Disabled"),
				"BootMode":      intstr.FromString("UEFI"),
			}
			statusMap := map[string]string{
				"ProcTurboMode": "Enabled",
				"BootMode":      "UEFI",
			}

			result := isChangeDetected(ctx, logger, specMap, statusMap)

			Expect(result).To(BeTrue())
		})

		It("should return true when change detected - missing in status", func() {
			specMap := map[string]intstr.IntOrString{
				"ProcTurboMode": intstr.FromString("Enabled"),
				"NewSetting":    intstr.FromString("Value"),
			}
			statusMap := map[string]string{
				"ProcTurboMode": "Enabled",
			}

			result := isChangeDetected(ctx, logger, specMap, statusMap)

			Expect(result).To(BeTrue())
		})

		It("should return false for empty spec", func() {
			specMap := map[string]intstr.IntOrString{}
			statusMap := map[string]string{"ProcTurboMode": "Enabled"}

			result := isChangeDetected(ctx, logger, specMap, statusMap)

			Expect(result).To(BeFalse())
		})
	})

	Describe("isFirmwareSettingsChangeDetectedAndValid", func() {
		It("should return true when change detected and valid", func() {
			bmh := &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-host",
					Namespace: "test-namespace",
				},
			}
			existingHFS := &metal3v1alpha1.HostFirmwareSettings{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-host",
					Namespace:  "test-namespace",
					Generation: 1,
				},
				Status: metal3v1alpha1.HostFirmwareSettingsStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(metal3v1alpha1.FirmwareSettingsChangeDetected),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:   string(metal3v1alpha1.FirmwareSettingsValid),
							Status: metav1.ConditionTrue,
						},
					},
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingHFS).Build()

			result, err := isFirmwareSettingsChangeDetectedAndValid(ctx, fakeClient, bmh)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeTrue())
		})

		It("should return false when change not detected", func() {
			bmh := &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-host",
					Namespace: "test-namespace",
				},
			}
			existingHFS := &metal3v1alpha1.HostFirmwareSettings{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-host",
					Namespace:  "test-namespace",
					Generation: 1,
				},
				Status: metal3v1alpha1.HostFirmwareSettingsStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(metal3v1alpha1.FirmwareSettingsChangeDetected),
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:   string(metal3v1alpha1.FirmwareSettingsValid),
							Status: metav1.ConditionTrue,
						},
					},
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingHFS).Build()

			result, err := isFirmwareSettingsChangeDetectedAndValid(ctx, fakeClient, bmh)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeFalse())
		})

		It("should return error when HFS not found", func() {
			bmh := &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-existent-host",
					Namespace: "test-namespace",
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			result, err := isFirmwareSettingsChangeDetectedAndValid(ctx, fakeClient, bmh)

			Expect(err).To(HaveOccurred())
			Expect(result).To(BeFalse())
		})
	})

	Describe("updateHostFirmwareSettings", func() {
		It("should successfully update HostFirmwareSettings", func() {
			existingHFS := &metal3v1alpha1.HostFirmwareSettings{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hfs",
					Namespace: "test-namespace",
				},
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: map[string]intstr.IntOrString{
						"ProcTurboMode": intstr.FromString("Enabled"),
					},
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingHFS).Build()

			namespacedName := types.NamespacedName{
				Name:      "test-hfs",
				Namespace: "test-namespace",
			}
			newSettings := metal3v1alpha1.HostFirmwareSettings{
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: map[string]intstr.IntOrString{
						"ProcTurboMode": intstr.FromString("Disabled"),
						"BootMode":      intstr.FromString("UEFI"),
					},
				},
			}

			err := updateHostFirmwareSettings(ctx, fakeClient, namespacedName, newSettings)

			Expect(err).ToNot(HaveOccurred())

			// Verify the update
			updated := &metal3v1alpha1.HostFirmwareSettings{}
			err = fakeClient.Get(ctx, namespacedName, updated)
			Expect(err).ToNot(HaveOccurred())
			Expect(updated.Spec.Settings).To(Equal(newSettings.Spec.Settings))
		})

		It("should return error when HFS not found", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			namespacedName := types.NamespacedName{
				Name:      "non-existent-hfs",
				Namespace: "test-namespace",
			}
			newSettings := metal3v1alpha1.HostFirmwareSettings{
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: map[string]intstr.IntOrString{
						"BootMode": intstr.FromString("UEFI"),
					},
				},
			}

			err := updateHostFirmwareSettings(ctx, fakeClient, namespacedName, newSettings)

			Expect(err).To(HaveOccurred())
		})
	})

	Describe("validateBiosSettings", func() {
		It("should validate settings successfully with valid schema", func() {
			firmwareSchema := &metal3v1alpha1.FirmwareSchema{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-schema",
					Namespace: "test-namespace",
				},
				Spec: metal3v1alpha1.FirmwareSchemaSpec{
					Schema: map[string]metal3v1alpha1.SettingSchema{
						"ProcTurboMode": {
							AllowableValues: []string{"Enabled", "Disabled"},
						},
					},
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(firmwareSchema).Build()

			existingHFS := &metal3v1alpha1.HostFirmwareSettings{
				Status: metal3v1alpha1.HostFirmwareSettingsStatus{
					FirmwareSchema: &metal3v1alpha1.SchemaReference{
						Name:      "test-schema",
						Namespace: "test-namespace",
					},
					Settings: map[string]string{
						"ProcTurboMode": "Enabled",
					},
				},
			}
			newSettings := map[string]intstr.IntOrString{
				"ProcTurboMode": intstr.FromString("Disabled"),
			}

			err := validateBiosSettings(ctx, fakeClient, existingHFS, newSettings)

			Expect(err).ToNot(HaveOccurred())
		})

		It("should return error when firmware schema is missing", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			existingHFS := &metal3v1alpha1.HostFirmwareSettings{
				Status: metal3v1alpha1.HostFirmwareSettingsStatus{
					FirmwareSchema: nil,
				},
			}
			newSettings := map[string]intstr.IntOrString{
				"ProcTurboMode": intstr.FromString("Disabled"),
			}

			err := validateBiosSettings(ctx, fakeClient, existingHFS, newSettings)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get FirmwareSchema from HFS"))
		})

		It("should return error when schema name is empty", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			existingHFS := &metal3v1alpha1.HostFirmwareSettings{
				Status: metal3v1alpha1.HostFirmwareSettingsStatus{
					FirmwareSchema: &metal3v1alpha1.SchemaReference{
						Name:      "",
						Namespace: "test-namespace",
					},
				},
			}
			newSettings := map[string]intstr.IntOrString{
				"ProcTurboMode": intstr.FromString("Disabled"),
			}

			err := validateBiosSettings(ctx, fakeClient, existingHFS, newSettings)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("firmwareSchema name or namespace is nil"))
		})
	})

	Describe("checkAndUpdateFirmwareSettings", func() {
		It("should return false when no changes detected", func() {
			existingHFS := &metal3v1alpha1.HostFirmwareSettings{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hfs",
					Namespace: "test-namespace",
				},
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: map[string]intstr.IntOrString{
						"ProcTurboMode": intstr.FromString("Enabled"),
					},
				},
				Status: metal3v1alpha1.HostFirmwareSettingsStatus{
					Settings: map[string]string{
						"ProcTurboMode": "Enabled",
					},
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingHFS).Build()

			newHFS := &metal3v1alpha1.HostFirmwareSettings{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hfs",
					Namespace: "test-namespace",
				},
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: map[string]intstr.IntOrString{
						"ProcTurboMode": intstr.FromString("Enabled"),
					},
				},
			}

			updated, err := checkAndUpdateFirmwareSettings(ctx, fakeClient, logger, existingHFS, newHFS)

			Expect(err).ToNot(HaveOccurred())
			Expect(updated).To(BeFalse())
		})

		It("should return true and update when changes detected", func() {
			existingHFS := &metal3v1alpha1.HostFirmwareSettings{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hfs",
					Namespace: "test-namespace",
				},
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: map[string]intstr.IntOrString{
						"ProcTurboMode": intstr.FromString("Enabled"),
					},
				},
				Status: metal3v1alpha1.HostFirmwareSettingsStatus{
					Settings: map[string]string{
						"ProcTurboMode": "Enabled",
					},
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingHFS).Build()

			newHFS := &metal3v1alpha1.HostFirmwareSettings{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hfs",
					Namespace: "test-namespace",
				},
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: map[string]intstr.IntOrString{
						"ProcTurboMode": intstr.FromString("Disabled"),
					},
				},
			}

			updated, err := checkAndUpdateFirmwareSettings(ctx, fakeClient, logger, existingHFS, newHFS)

			Expect(err).ToNot(HaveOccurred())
			Expect(updated).To(BeTrue())

			// Verify the update was applied
			result := &metal3v1alpha1.HostFirmwareSettings{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      newHFS.Name,
				Namespace: newHFS.Namespace,
			}, result)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Spec.Settings).To(Equal(newHFS.Spec.Settings))
		})
	})

	Describe("IsBiosUpdateRequired", func() {
		var firmwareSchema *metal3v1alpha1.FirmwareSchema
		var bmh *metal3v1alpha1.BareMetalHost

		BeforeEach(func() {
			firmwareSchema = &metal3v1alpha1.FirmwareSchema{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-schema",
					Namespace: "test-namespace",
				},
				Spec: metal3v1alpha1.FirmwareSchemaSpec{
					Schema: map[string]metal3v1alpha1.SettingSchema{
						"ProcTurboMode": {
							AllowableValues: []string{"Enabled", "Disabled"},
						},
					},
				},
			}

			bmh = &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-host",
					Namespace: "test-namespace",
				},
			}
		})

		It("should return error when update required - new HFS created without schema", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(firmwareSchema).Build()

			biosSettings := hwmgmtv1alpha1.Bios{
				Attributes: map[string]intstr.IntOrString{
					"ProcTurboMode": intstr.FromString("Enabled"),
				},
			}

			updateRequired, err := IsBiosUpdateRequired(ctx, fakeClient, logger, bmh, biosSettings)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get FirmwareSchema from HFS"))
			Expect(updateRequired).To(BeFalse())
		})

		It("should return true when update required - settings changed", func() {
			existingHFS := &metal3v1alpha1.HostFirmwareSettings{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-host",
					Namespace: "test-namespace",
				},
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: map[string]intstr.IntOrString{
						"ProcTurboMode": intstr.FromString("Enabled"),
					},
				},
				Status: metal3v1alpha1.HostFirmwareSettingsStatus{
					FirmwareSchema: &metal3v1alpha1.SchemaReference{
						Name:      "test-schema",
						Namespace: "test-namespace",
					},
					Settings: map[string]string{
						"ProcTurboMode": "Enabled",
					},
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(firmwareSchema, existingHFS).Build()

			biosSettings := hwmgmtv1alpha1.Bios{
				Attributes: map[string]intstr.IntOrString{
					"ProcTurboMode": intstr.FromString("Disabled"),
				},
			}

			updateRequired, err := IsBiosUpdateRequired(ctx, fakeClient, logger, bmh, biosSettings)

			Expect(err).ToNot(HaveOccurred())
			Expect(updateRequired).To(BeTrue())
		})

		It("should return false when no update required - settings same", func() {
			existingHFS := &metal3v1alpha1.HostFirmwareSettings{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-host",
					Namespace: "test-namespace",
				},
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: map[string]intstr.IntOrString{
						"ProcTurboMode": intstr.FromString("Enabled"),
					},
				},
				Status: metal3v1alpha1.HostFirmwareSettingsStatus{
					FirmwareSchema: &metal3v1alpha1.SchemaReference{
						Name:      "test-schema",
						Namespace: "test-namespace",
					},
					Settings: map[string]string{
						"ProcTurboMode": "Enabled",
					},
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(firmwareSchema, existingHFS).Build()

			biosSettings := hwmgmtv1alpha1.Bios{
				Attributes: map[string]intstr.IntOrString{
					"ProcTurboMode": intstr.FromString("Enabled"),
				},
			}

			updateRequired, err := IsBiosUpdateRequired(ctx, fakeClient, logger, bmh, biosSettings)

			Expect(err).ToNot(HaveOccurred())
			Expect(updateRequired).To(BeFalse())
		})
	})
})
