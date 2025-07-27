/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"log/slog"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
)

var _ = Describe("HostFirmwareComponents Manager", func() {
	var (
		ctx    context.Context
		logger *slog.Logger
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		logger = slog.Default()
		scheme = runtime.NewScheme()
		Expect(metal3v1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())
	})

	createBMH := func(name, namespace string) *metal3v1alpha1.BareMetalHost {
		return &metal3v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: metal3v1alpha1.BareMetalHostSpec{},
		}
	}

	createHFC := func(name, namespace string, updates []metal3v1alpha1.FirmwareUpdate) *metal3v1alpha1.HostFirmwareComponents {
		return &metal3v1alpha1.HostFirmwareComponents{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: metal3v1alpha1.HostFirmwareComponentsSpec{
				Updates: updates,
			},
		}
	}

	createHFCWithStatus := func(name, namespace string, components []metal3v1alpha1.FirmwareComponentStatus, conditions []metav1.Condition, generation int64) *metal3v1alpha1.HostFirmwareComponents {
		return &metal3v1alpha1.HostFirmwareComponents{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name,
				Namespace:  namespace,
				Generation: generation,
			},
			Status: metal3v1alpha1.HostFirmwareComponentsStatus{
				Components: components,
				Conditions: conditions,
			},
		}
	}

	Describe("validateFirmwareUpdateSpec", func() {
		It("should return nil for empty firmware specs", func() {
			spec := hwmgmtv1alpha1.HardwareProfileSpec{}
			err := validateFirmwareUpdateSpec(spec)
			Expect(err).To(BeNil())
		})

		It("should return error when BIOS version is set but URL is empty", func() {
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: hwmgmtv1alpha1.Firmware{
					Version: "1.0.0",
					URL:     "",
				},
			}
			err := validateFirmwareUpdateSpec(spec)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("missing BIOS firmware URL"))
		})

		It("should return error when BIOS URL is invalid", func() {
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: hwmgmtv1alpha1.Firmware{
					Version: "1.0.0",
					URL:     "invalid-url",
				},
			}
			err := validateFirmwareUpdateSpec(spec)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid BIOS firmware URL"))
		})

		It("should return error when BMC version is set but URL is empty", func() {
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BmcFirmware: hwmgmtv1alpha1.Firmware{
					Version: "2.0.0",
					URL:     "",
				},
			}
			err := validateFirmwareUpdateSpec(spec)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("missing BMC firmware URL"))
		})

		It("should return error when BMC URL is invalid", func() {
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BmcFirmware: hwmgmtv1alpha1.Firmware{
					Version: "2.0.0",
					URL:     "invalid-url",
				},
			}
			err := validateFirmwareUpdateSpec(spec)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid BMC firmware URL"))
		})

		It("should return nil for valid firmware specs", func() {
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: hwmgmtv1alpha1.Firmware{
					Version: "1.0.0",
					URL:     "https://example.com/bios.bin",
				},
				BmcFirmware: hwmgmtv1alpha1.Firmware{
					Version: "2.0.0",
					URL:     "https://example.com/bmc.bin",
				},
			}
			err := validateFirmwareUpdateSpec(spec)
			Expect(err).To(BeNil())
		})
	})

	Describe("convertToFirmwareUpdates", func() {
		It("should return empty slice when no firmware is specified", func() {
			spec := hwmgmtv1alpha1.HardwareProfileSpec{}
			updates := convertToFirmwareUpdates(spec)
			Expect(updates).To(BeEmpty())
		})

		It("should convert BIOS firmware to update", func() {
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: hwmgmtv1alpha1.Firmware{
					Version: "1.0.0",
					URL:     "https://example.com/bios.bin",
				},
			}
			updates := convertToFirmwareUpdates(spec)
			Expect(updates).To(HaveLen(1))
			Expect(updates[0].Component).To(Equal("bios"))
			Expect(updates[0].URL).To(Equal("https://example.com/bios.bin"))
		})

		It("should convert BMC firmware to update", func() {
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BmcFirmware: hwmgmtv1alpha1.Firmware{
					Version: "2.0.0",
					URL:     "https://example.com/bmc.bin",
				},
			}
			updates := convertToFirmwareUpdates(spec)
			Expect(updates).To(HaveLen(1))
			Expect(updates[0].Component).To(Equal("bmc"))
			Expect(updates[0].URL).To(Equal("https://example.com/bmc.bin"))
		})

		It("should convert both BIOS and BMC firmware to updates", func() {
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: hwmgmtv1alpha1.Firmware{
					Version: "1.0.0",
					URL:     "https://example.com/bios.bin",
				},
				BmcFirmware: hwmgmtv1alpha1.Firmware{
					Version: "2.0.0",
					URL:     "https://example.com/bmc.bin",
				},
			}
			updates := convertToFirmwareUpdates(spec)
			Expect(updates).To(HaveLen(2))

			var biosUpdate, bmcUpdate metal3v1alpha1.FirmwareUpdate
			for _, update := range updates {
				if update.Component == "bios" {
					biosUpdate = update
				} else if update.Component == "bmc" {
					bmcUpdate = update
				}
			}

			Expect(biosUpdate.Component).To(Equal("bios"))
			Expect(biosUpdate.URL).To(Equal("https://example.com/bios.bin"))
			Expect(bmcUpdate.Component).To(Equal("bmc"))
			Expect(bmcUpdate.URL).To(Equal("https://example.com/bmc.bin"))
		})

		It("should not include firmware with empty URL", func() {
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: hwmgmtv1alpha1.Firmware{
					Version: "1.0.0",
					URL:     "",
				},
				BmcFirmware: hwmgmtv1alpha1.Firmware{
					Version: "2.0.0",
					URL:     "https://example.com/bmc.bin",
				},
			}
			updates := convertToFirmwareUpdates(spec)
			Expect(updates).To(HaveLen(1))
			Expect(updates[0].Component).To(Equal("bmc"))
			Expect(updates[0].URL).To(Equal("https://example.com/bmc.bin"))
		})
	})

	Describe("isHostFirmwareComponentsChangeDetectedAndValid", func() {
		var fakeClient client.Client

		BeforeEach(func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		})

		It("should return error when HFC does not exist", func() {
			bmh := createBMH("test-bmh", "test-namespace")

			changeDetected, err := isHostFirmwareComponentsChangeDetectedAndValid(ctx, fakeClient, bmh)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get HostFirmwareComponents"))
			Expect(changeDetected).To(BeFalse())
		})

		It("should return error when change detected condition is missing", func() {
			bmh := createBMH("test-bmh", "test-namespace")
			hfc := createHFCWithStatus("test-bmh", "test-namespace", []metal3v1alpha1.FirmwareComponentStatus{}, []metav1.Condition{}, 1)

			Expect(fakeClient.Create(ctx, hfc)).To(Succeed())

			changeDetected, err := isHostFirmwareComponentsChangeDetectedAndValid(ctx, fakeClient, bmh)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get HostFirmwareComponents"))
			Expect(changeDetected).To(BeFalse())
		})

		It("should return false when change is not detected", func() {
			bmh := createBMH("test-bmh", "test-namespace")
			conditions := []metav1.Condition{
				{
					Type:               string(metal3v1alpha1.HostFirmwareComponentsChangeDetected),
					Status:             metav1.ConditionFalse,
					ObservedGeneration: 1,
				},
				{
					Type:   string(metal3v1alpha1.HostFirmwareComponentsValid),
					Status: metav1.ConditionTrue,
				},
			}
			hfc := createHFCWithStatus("test-bmh", "test-namespace", []metal3v1alpha1.FirmwareComponentStatus{}, conditions, 1)

			Expect(fakeClient.Create(ctx, hfc)).To(Succeed())

			changeDetected, err := isHostFirmwareComponentsChangeDetectedAndValid(ctx, fakeClient, bmh)
			Expect(err).To(BeNil())
			Expect(changeDetected).To(BeFalse())
		})

		It("should return false when HFC is not valid", func() {
			bmh := createBMH("test-bmh", "test-namespace")
			conditions := []metav1.Condition{
				{
					Type:               string(metal3v1alpha1.HostFirmwareComponentsChangeDetected),
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 1,
				},
				{
					Type:   string(metal3v1alpha1.HostFirmwareComponentsValid),
					Status: metav1.ConditionFalse,
				},
			}
			hfc := createHFCWithStatus("test-bmh", "test-namespace", []metal3v1alpha1.FirmwareComponentStatus{}, conditions, 1)

			Expect(fakeClient.Create(ctx, hfc)).To(Succeed())

			changeDetected, err := isHostFirmwareComponentsChangeDetectedAndValid(ctx, fakeClient, bmh)
			Expect(err).To(BeNil())
			Expect(changeDetected).To(BeFalse())
		})

		It("should return false when observed generation doesn't match", func() {
			bmh := createBMH("test-bmh", "test-namespace")
			conditions := []metav1.Condition{
				{
					Type:               string(metal3v1alpha1.HostFirmwareComponentsChangeDetected),
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 1,
				},
				{
					Type:   string(metal3v1alpha1.HostFirmwareComponentsValid),
					Status: metav1.ConditionTrue,
				},
			}
			hfc := createHFCWithStatus("test-bmh", "test-namespace", []metal3v1alpha1.FirmwareComponentStatus{}, conditions, 2)

			Expect(fakeClient.Create(ctx, hfc)).To(Succeed())

			changeDetected, err := isHostFirmwareComponentsChangeDetectedAndValid(ctx, fakeClient, bmh)
			Expect(err).To(BeNil())
			Expect(changeDetected).To(BeFalse())
		})

		It("should return true when change is detected, valid, and generation matches", func() {
			bmh := createBMH("test-bmh", "test-namespace")
			conditions := []metav1.Condition{
				{
					Type:               string(metal3v1alpha1.HostFirmwareComponentsChangeDetected),
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 1,
				},
				{
					Type:   string(metal3v1alpha1.HostFirmwareComponentsValid),
					Status: metav1.ConditionTrue,
				},
			}
			hfc := createHFCWithStatus("test-bmh", "test-namespace", []metal3v1alpha1.FirmwareComponentStatus{}, conditions, 1)

			Expect(fakeClient.Create(ctx, hfc)).To(Succeed())

			changeDetected, err := isHostFirmwareComponentsChangeDetectedAndValid(ctx, fakeClient, bmh)
			Expect(err).To(BeNil())
			Expect(changeDetected).To(BeTrue())
		})
	})

	Describe("isVersionChangeDetected", func() {
		It("should return no updates when no firmware components exist", func() {
			status := &metal3v1alpha1.HostFirmwareComponentsStatus{
				Components: []metal3v1alpha1.FirmwareComponentStatus{},
			}
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: hwmgmtv1alpha1.Firmware{
					Version: "1.0.0",
					URL:     "https://example.com/bios.bin",
				},
			}

			updates, updateRequired := isVersionChangeDetected(ctx, logger, status, spec)
			Expect(updates).To(BeEmpty())
			Expect(updateRequired).To(BeFalse())
		})

		It("should skip empty firmware specs", func() {
			status := &metal3v1alpha1.HostFirmwareComponentsStatus{
				Components: []metal3v1alpha1.FirmwareComponentStatus{
					{
						Component:      "bios",
						CurrentVersion: "0.9.0",
					},
				},
			}
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: hwmgmtv1alpha1.Firmware{}, // Empty firmware spec
			}

			updates, updateRequired := isVersionChangeDetected(ctx, logger, status, spec)
			Expect(updates).To(BeEmpty())
			Expect(updateRequired).To(BeFalse())
		})

		It("should detect version change when current version differs from desired", func() {
			status := &metal3v1alpha1.HostFirmwareComponentsStatus{
				Components: []metal3v1alpha1.FirmwareComponentStatus{
					{
						Component:      "bios",
						CurrentVersion: "0.9.0",
					},
				},
			}
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: hwmgmtv1alpha1.Firmware{
					Version: "1.0.0",
					URL:     "https://example.com/bios.bin",
				},
			}

			updates, updateRequired := isVersionChangeDetected(ctx, logger, status, spec)
			Expect(updates).To(HaveLen(1))
			Expect(updates[0].Component).To(Equal("bios"))
			Expect(updates[0].URL).To(Equal("https://example.com/bios.bin"))
			Expect(updateRequired).To(BeTrue())
		})

		It("should not detect change when versions match", func() {
			status := &metal3v1alpha1.HostFirmwareComponentsStatus{
				Components: []metal3v1alpha1.FirmwareComponentStatus{
					{
						Component:      "bios",
						CurrentVersion: "1.0.0",
					},
				},
			}
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: hwmgmtv1alpha1.Firmware{
					Version: "1.0.0",
					URL:     "https://example.com/bios.bin",
				},
			}

			updates, updateRequired := isVersionChangeDetected(ctx, logger, status, spec)
			Expect(updates).To(BeEmpty())
			Expect(updateRequired).To(BeFalse())
		})

		It("should detect changes for both BIOS and BMC", func() {
			status := &metal3v1alpha1.HostFirmwareComponentsStatus{
				Components: []metal3v1alpha1.FirmwareComponentStatus{
					{
						Component:      "bios",
						CurrentVersion: "0.9.0",
					},
					{
						Component:      "bmc",
						CurrentVersion: "1.9.0",
					},
				},
			}
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: hwmgmtv1alpha1.Firmware{
					Version: "1.0.0",
					URL:     "https://example.com/bios.bin",
				},
				BmcFirmware: hwmgmtv1alpha1.Firmware{
					Version: "2.0.0",
					URL:     "https://example.com/bmc.bin",
				},
			}

			updates, updateRequired := isVersionChangeDetected(ctx, logger, status, spec)
			Expect(updates).To(HaveLen(2))
			Expect(updateRequired).To(BeTrue())

			var biosUpdate, bmcUpdate *metal3v1alpha1.FirmwareUpdate
			for i, update := range updates {
				if update.Component == "bios" {
					biosUpdate = &updates[i]
				} else if update.Component == "bmc" {
					bmcUpdate = &updates[i]
				}
			}

			Expect(biosUpdate).NotTo(BeNil())
			Expect(biosUpdate.Component).To(Equal("bios"))
			Expect(biosUpdate.URL).To(Equal("https://example.com/bios.bin"))

			Expect(bmcUpdate).NotTo(BeNil())
			Expect(bmcUpdate.Component).To(Equal("bmc"))
			Expect(bmcUpdate.URL).To(Equal("https://example.com/bmc.bin"))
		})
	})

	Describe("createHostFirmwareComponents", func() {
		var fakeClient client.Client

		BeforeEach(func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		})

		It("should create HFC with firmware updates", func() {
			bmh := createBMH("test-bmh", "test-namespace")
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: hwmgmtv1alpha1.Firmware{
					Version: "1.0.0",
					URL:     "https://example.com/bios.bin",
				},
			}

			hfc, err := createHostFirmwareComponents(ctx, fakeClient, bmh, spec)
			Expect(err).To(BeNil())
			Expect(hfc).NotTo(BeNil())
			Expect(hfc.Name).To(Equal("test-bmh"))
			Expect(hfc.Namespace).To(Equal("test-namespace"))
			Expect(hfc.Spec.Updates).To(HaveLen(1))
			Expect(hfc.Spec.Updates[0].Component).To(Equal("bios"))
			Expect(hfc.Spec.Updates[0].URL).To(Equal("https://example.com/bios.bin"))

			// Verify it was created in the cluster
			createdHFC := &metal3v1alpha1.HostFirmwareComponents{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-bmh", Namespace: "test-namespace"}, createdHFC)
			Expect(err).To(BeNil())
			Expect(createdHFC.Spec.Updates).To(HaveLen(1))
		})

		It("should create HFC with empty updates when no firmware specified", func() {
			bmh := createBMH("test-bmh", "test-namespace")
			spec := hwmgmtv1alpha1.HardwareProfileSpec{}

			hfc, err := createHostFirmwareComponents(ctx, fakeClient, bmh, spec)
			Expect(err).To(BeNil())
			Expect(hfc).NotTo(BeNil())
			Expect(hfc.Spec.Updates).To(BeEmpty())
		})
	})

	Describe("updateHostFirmwareComponents", func() {
		var fakeClient client.Client

		BeforeEach(func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		})

		It("should update existing HFC with new firmware updates", func() {
			// Create initial HFC
			hfc := createHFC("test-bmh", "test-namespace", []metal3v1alpha1.FirmwareUpdate{
				{Component: "bios", URL: "https://example.com/old-bios.bin"},
			})
			Expect(fakeClient.Create(ctx, hfc)).To(Succeed())

			// Update with new firmware
			newUpdates := []metal3v1alpha1.FirmwareUpdate{
				{Component: "bios", URL: "https://example.com/new-bios.bin"},
				{Component: "bmc", URL: "https://example.com/bmc.bin"},
			}

			err := updateHostFirmwareComponents(ctx, fakeClient, types.NamespacedName{
				Name:      "test-bmh",
				Namespace: "test-namespace",
			}, newUpdates)
			Expect(err).To(BeNil())

			// Verify the update
			updatedHFC := &metal3v1alpha1.HostFirmwareComponents{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-bmh", Namespace: "test-namespace"}, updatedHFC)
			Expect(err).To(BeNil())
			Expect(updatedHFC.Spec.Updates).To(HaveLen(2))
		})

		It("should return error when HFC does not exist", func() {
			newUpdates := []metal3v1alpha1.FirmwareUpdate{
				{Component: "bios", URL: "https://example.com/bios.bin"},
			}

			err := updateHostFirmwareComponents(ctx, fakeClient, types.NamespacedName{
				Name:      "non-existent",
				Namespace: "test-namespace",
			}, newUpdates)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to fetch HostFirmwareComponents"))
		})
	})

	Describe("getHostFirmwareComponents", func() {
		var fakeClient client.Client

		BeforeEach(func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		})

		It("should return HFC when it exists", func() {
			hfc := createHFC("test-bmh", "test-namespace", []metal3v1alpha1.FirmwareUpdate{})
			Expect(fakeClient.Create(ctx, hfc)).To(Succeed())

			retrievedHFC, err := getHostFirmwareComponents(ctx, fakeClient, "test-bmh", "test-namespace")
			Expect(err).To(BeNil())
			Expect(retrievedHFC).NotTo(BeNil())
			Expect(retrievedHFC.Name).To(Equal("test-bmh"))
			Expect(retrievedHFC.Namespace).To(Equal("test-namespace"))
		})

		It("should return error when HFC does not exist", func() {
			_, err := getHostFirmwareComponents(ctx, fakeClient, "non-existent", "test-namespace")
			Expect(err).To(HaveOccurred())
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})

	Describe("getOrCreateHostFirmwareComponents", func() {
		var fakeClient client.Client

		BeforeEach(func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		})

		It("should return existing HFC without creating new one", func() {
			bmh := createBMH("test-bmh", "test-namespace")
			spec := hwmgmtv1alpha1.HardwareProfileSpec{}

			// Create HFC first
			existingHFC := createHFC("test-bmh", "test-namespace", []metal3v1alpha1.FirmwareUpdate{})
			Expect(fakeClient.Create(ctx, existingHFC)).To(Succeed())

			hfc, created, err := getOrCreateHostFirmwareComponents(ctx, fakeClient, logger, bmh, spec)
			Expect(err).To(BeNil())
			Expect(created).To(BeFalse())
			Expect(hfc).NotTo(BeNil())
			Expect(hfc.Name).To(Equal("test-bmh"))
		})

		It("should create new HFC when it does not exist", func() {
			bmh := createBMH("test-bmh", "test-namespace")
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: hwmgmtv1alpha1.Firmware{
					Version: "1.0.0",
					URL:     "https://example.com/bios.bin",
				},
			}

			hfc, created, err := getOrCreateHostFirmwareComponents(ctx, fakeClient, logger, bmh, spec)
			Expect(err).To(BeNil())
			Expect(created).To(BeTrue())
			Expect(hfc).NotTo(BeNil())
			Expect(hfc.Name).To(Equal("test-bmh"))
			Expect(hfc.Spec.Updates).To(HaveLen(1))
		})
	})

	Describe("IsFirmwareUpdateRequired", func() {
		var fakeClient client.Client

		BeforeEach(func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		})

		It("should return error for invalid firmware spec", func() {
			bmh := createBMH("test-bmh", "test-namespace")
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: hwmgmtv1alpha1.Firmware{
					Version: "1.0.0",
					URL:     "", // Invalid: version set but URL empty
				},
			}

			required, err := IsFirmwareUpdateRequired(ctx, fakeClient, logger, bmh, spec)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("missing BIOS firmware URL"))
			Expect(required).To(BeFalse())
		})

		It("should return true when HFC is created for the first time", func() {
			bmh := createBMH("test-bmh", "test-namespace")
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: hwmgmtv1alpha1.Firmware{
					Version: "1.0.0",
					URL:     "https://example.com/bios.bin",
				},
			}

			required, err := IsFirmwareUpdateRequired(ctx, fakeClient, logger, bmh, spec)
			Expect(err).To(BeNil())
			Expect(required).To(BeTrue())

			// Verify HFC was created
			hfc := &metal3v1alpha1.HostFirmwareComponents{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-bmh", Namespace: "test-namespace"}, hfc)
			Expect(err).To(BeNil())
		})

		It("should return false when no update is needed", func() {
			bmh := createBMH("test-bmh", "test-namespace")
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: hwmgmtv1alpha1.Firmware{
					Version: "1.0.0",
					URL:     "https://example.com/bios.bin",
				},
			}

			// Create HFC with status showing current version matches desired
			components := []metal3v1alpha1.FirmwareComponentStatus{
				{
					Component:      "bios",
					CurrentVersion: "1.0.0", // Same as desired version
				},
			}
			hfc := createHFCWithStatus("test-bmh", "test-namespace", components, []metav1.Condition{}, 1)
			Expect(fakeClient.Create(ctx, hfc)).To(Succeed())

			required, err := IsFirmwareUpdateRequired(ctx, fakeClient, logger, bmh, spec)
			Expect(err).To(BeNil())
			Expect(required).To(BeFalse())
		})

		It("should return true and update HFC when version change is detected", func() {
			bmh := createBMH("test-bmh", "test-namespace")
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: hwmgmtv1alpha1.Firmware{
					Version: "2.0.0",
					URL:     "https://example.com/new-bios.bin",
				},
			}

			// Create HFC with status showing different current version
			components := []metal3v1alpha1.FirmwareComponentStatus{
				{
					Component:      "bios",
					CurrentVersion: "1.0.0", // Different from desired version
				},
			}
			hfc := createHFCWithStatus("test-bmh", "test-namespace", components, []metav1.Condition{}, 1)
			Expect(fakeClient.Create(ctx, hfc)).To(Succeed())

			required, err := IsFirmwareUpdateRequired(ctx, fakeClient, logger, bmh, spec)
			Expect(err).To(BeNil())
			Expect(required).To(BeTrue())

			// Verify HFC was updated with new firmware URL
			updatedHFC := &metal3v1alpha1.HostFirmwareComponents{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-bmh", Namespace: "test-namespace"}, updatedHFC)
			Expect(err).To(BeNil())
			Expect(updatedHFC.Spec.Updates).To(HaveLen(1))
			Expect(updatedHFC.Spec.Updates[0].Component).To(Equal("bios"))
			Expect(updatedHFC.Spec.Updates[0].URL).To(Equal("https://example.com/new-bios.bin"))
		})

		It("should handle multiple firmware components correctly", func() {
			bmh := createBMH("test-bmh", "test-namespace")
			spec := hwmgmtv1alpha1.HardwareProfileSpec{
				BiosFirmware: hwmgmtv1alpha1.Firmware{
					Version: "2.0.0",
					URL:     "https://example.com/new-bios.bin",
				},
				BmcFirmware: hwmgmtv1alpha1.Firmware{
					Version: "3.0.0",
					URL:     "https://example.com/new-bmc.bin",
				},
			}

			// Create HFC with status showing current versions differ
			components := []metal3v1alpha1.FirmwareComponentStatus{
				{
					Component:      "bios",
					CurrentVersion: "1.0.0",
				},
				{
					Component:      "bmc",
					CurrentVersion: "2.5.0",
				},
			}
			hfc := createHFCWithStatus("test-bmh", "test-namespace", components, []metav1.Condition{}, 1)
			Expect(fakeClient.Create(ctx, hfc)).To(Succeed())

			required, err := IsFirmwareUpdateRequired(ctx, fakeClient, logger, bmh, spec)
			Expect(err).To(BeNil())
			Expect(required).To(BeTrue())

			// Verify HFC was updated with both firmware URLs
			updatedHFC := &metal3v1alpha1.HostFirmwareComponents{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-bmh", Namespace: "test-namespace"}, updatedHFC)
			Expect(err).To(BeNil())
			Expect(updatedHFC.Spec.Updates).To(HaveLen(2))

			// Check that both components are included
			updateMap := make(map[string]string)
			for _, update := range updatedHFC.Spec.Updates {
				updateMap[update.Component] = update.URL
			}
			Expect(updateMap["bios"]).To(Equal("https://example.com/new-bios.bin"))
			Expect(updateMap["bmc"]).To(Equal("https://example.com/new-bmc.bin"))
		})
	})
})
