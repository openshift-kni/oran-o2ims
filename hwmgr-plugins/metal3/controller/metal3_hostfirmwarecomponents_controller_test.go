/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Generated-By: Claude Sonnet 4.5
*/

/*
Test Cases for HostFirmwareComponents Controller:

Reconcile tests:
- Skips reconciliation when HFC does not exist
- Skips reconciliation when corresponding BMH does not exist
- Skips reconciliation when BMH is not O-Cloud managed
- Removes label when BMH is not O-Cloud managed but has label
- Skips reconciliation when system is not HPE or Dell
- Removes label when system is not HPE/Dell but has label
- Adds label when BIOS component is missing
- Adds label when BMC component is missing
- Adds label when NIC component is missing
- Adds label when multiple components are missing (no components at all)
- Removes label when all components are present
- Does not modify label when already set correctly

isHPEOrDell tests:
- Returns true for HPE manufacturer
- Returns true for Dell Inc. manufacturer
- Returns false for other manufacturers
- Returns false when HardwareData does not exist
- Returns false when HardwareData has nil hardware details

checkMissingComponents tests:
- Returns missing-firmware-data when no components exist
- Returns missing-bios-data when only BIOS is missing
- Returns missing-bmc-data when only BMC is missing
- Returns missing-nic-data when only NIC is missing
- Returns missing-firmware-data when multiple components missing
- Returns empty string when all components present
- Handles case-insensitive component names
*/

package controller

import (
	"context"
	"log/slog"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("HostFirmwareComponents Controller", func() {
	var (
		ctx        context.Context
		logger     *slog.Logger
		scheme     *runtime.Scheme
		reconciler *HostFirmwareComponentsReconciler
	)

	BeforeEach(func() {
		ctx = context.Background()
		logger = slog.Default()
		scheme = runtime.NewScheme()
		Expect(metal3v1alpha1.AddToScheme(scheme)).To(Succeed())
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

	createBMHWithLabels := func(name, namespace string, labels map[string]string) *metal3v1alpha1.BareMetalHost {
		bmh := createBMH(name, namespace)
		bmh.Labels = labels
		return bmh
	}

	createHFC := func(name, namespace string, components []metal3v1alpha1.FirmwareComponentStatus) *metal3v1alpha1.HostFirmwareComponents {
		return &metal3v1alpha1.HostFirmwareComponents{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Status: metal3v1alpha1.HostFirmwareComponentsStatus{
				Components: components,
			},
		}
	}

	createHardwareData := func(name, namespace, manufacturer string) *metal3v1alpha1.HardwareData {
		return &metal3v1alpha1.HardwareData{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: metal3v1alpha1.HardwareDataSpec{
				HardwareDetails: &metal3v1alpha1.HardwareDetails{
					SystemVendor: metal3v1alpha1.HardwareSystemVendor{
						Manufacturer: manufacturer,
					},
				},
			},
		}
	}

	Describe("Reconcile", func() {
		It("should skip reconciliation when HFC does not exist", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-hfc",
					Namespace: "test-ns",
				},
			}

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
		})

		It("should skip reconciliation when corresponding BMH does not exist", func() {
			hfc := createHFC("test-hfc", "test-ns", []metal3v1alpha1.FirmwareComponentStatus{})
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(hfc).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-hfc",
					Namespace: "test-ns",
				},
			}

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
		})

		It("should skip reconciliation when BMH is not O-Cloud managed", func() {
			bmh := createBMH("test-bmh", "test-ns")
			hfc := createHFC("test-bmh", "test-ns", []metal3v1alpha1.FirmwareComponentStatus{})

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bmh, hfc).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-bmh",
					Namespace: "test-ns",
				},
			}

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
		})

		It("should remove label when BMH is not O-Cloud managed but has label", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				ValidationUnavailableLabelKey: LabelValueMissingFirmwareData,
			})
			hfc := createHFC("test-bmh", "test-ns", []metal3v1alpha1.FirmwareComponentStatus{})

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bmh, hfc).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-bmh",
					Namespace: "test-ns",
				},
			}

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			// Verify label was removed
			updatedBMH := &metal3v1alpha1.BareMetalHost{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-bmh", Namespace: "test-ns"}, updatedBMH)
			Expect(err).ToNot(HaveOccurred())
			_, exists := updatedBMH.Labels[ValidationUnavailableLabelKey]
			Expect(exists).To(BeFalse())
		})

		It("should skip reconciliation when system is not HPE or Dell", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				LabelResourcePoolID: "pool123",
				LabelSiteID:         "site123",
			})
			hfc := createHFC("test-bmh", "test-ns", []metal3v1alpha1.FirmwareComponentStatus{})
			hwdata := createHardwareData("test-bmh", "test-ns", "Supermicro")

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bmh, hfc, hwdata).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-bmh",
					Namespace: "test-ns",
				},
			}

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
		})

		It("should remove label when system is not HPE/Dell but has label", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				LabelResourcePoolID:           "pool123",
				LabelSiteID:                   "site123",
				ValidationUnavailableLabelKey: LabelValueMissingFirmwareData,
			})
			hfc := createHFC("test-bmh", "test-ns", []metal3v1alpha1.FirmwareComponentStatus{})
			hwdata := createHardwareData("test-bmh", "test-ns", "Supermicro")

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bmh, hfc, hwdata).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-bmh",
					Namespace: "test-ns",
				},
			}

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			// Verify label was removed
			updatedBMH := &metal3v1alpha1.BareMetalHost{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-bmh", Namespace: "test-ns"}, updatedBMH)
			Expect(err).ToNot(HaveOccurred())
			_, exists := updatedBMH.Labels[ValidationUnavailableLabelKey]
			Expect(exists).To(BeFalse())
		})

		It("should add label when BIOS component is missing", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				LabelResourcePoolID: "pool123",
				LabelSiteID:         "site123",
			})
			hfc := createHFC("test-bmh", "test-ns", []metal3v1alpha1.FirmwareComponentStatus{
				{Component: "bmc", CurrentVersion: "1.0"},
				{Component: "nic:0", CurrentVersion: "2.0"},
			})
			hwdata := createHardwareData("test-bmh", "test-ns", "HPE")

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bmh, hfc, hwdata).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-bmh",
					Namespace: "test-ns",
				},
			}

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			// Verify label was added
			updatedBMH := &metal3v1alpha1.BareMetalHost{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-bmh", Namespace: "test-ns"}, updatedBMH)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedBMH.Labels[ValidationUnavailableLabelKey]).To(Equal(LabelValueMissingBIOSData))
		})

		It("should add label when BMC component is missing", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				LabelResourcePoolID: "pool123",
				LabelSiteID:         "site123",
			})
			hfc := createHFC("test-bmh", "test-ns", []metal3v1alpha1.FirmwareComponentStatus{
				{Component: "bios", CurrentVersion: "1.0"},
				{Component: "nic:0", CurrentVersion: "2.0"},
			})
			hwdata := createHardwareData("test-bmh", "test-ns", "Dell Inc.")

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bmh, hfc, hwdata).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-bmh",
					Namespace: "test-ns",
				},
			}

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			// Verify label was added
			updatedBMH := &metal3v1alpha1.BareMetalHost{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-bmh", Namespace: "test-ns"}, updatedBMH)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedBMH.Labels[ValidationUnavailableLabelKey]).To(Equal(LabelValueMissingBMCData))
		})

		It("should add label when NIC component is missing", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				LabelResourcePoolID: "pool123",
				LabelSiteID:         "site123",
			})
			hfc := createHFC("test-bmh", "test-ns", []metal3v1alpha1.FirmwareComponentStatus{
				{Component: "bios", CurrentVersion: "1.0"},
				{Component: "bmc", CurrentVersion: "2.0"},
			})
			hwdata := createHardwareData("test-bmh", "test-ns", "HPE")

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bmh, hfc, hwdata).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-bmh",
					Namespace: "test-ns",
				},
			}

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			// Verify label was added
			updatedBMH := &metal3v1alpha1.BareMetalHost{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-bmh", Namespace: "test-ns"}, updatedBMH)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedBMH.Labels[ValidationUnavailableLabelKey]).To(Equal(LabelValueMissingNICData))
		})

		It("should add label when multiple components are missing", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				LabelResourcePoolID: "pool123",
				LabelSiteID:         "site123",
			})
			hfc := createHFC("test-bmh", "test-ns", []metal3v1alpha1.FirmwareComponentStatus{})
			hwdata := createHardwareData("test-bmh", "test-ns", "Dell Inc.")

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bmh, hfc, hwdata).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-bmh",
					Namespace: "test-ns",
				},
			}

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			// Verify label was added
			updatedBMH := &metal3v1alpha1.BareMetalHost{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-bmh", Namespace: "test-ns"}, updatedBMH)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedBMH.Labels[ValidationUnavailableLabelKey]).To(Equal(LabelValueMissingFirmwareData))
		})

		It("should remove label when all components are present", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				LabelResourcePoolID:           "pool123",
				LabelSiteID:                   "site123",
				ValidationUnavailableLabelKey: LabelValueMissingNICData,
			})
			hfc := createHFC("test-bmh", "test-ns", []metal3v1alpha1.FirmwareComponentStatus{
				{Component: "bios", CurrentVersion: "1.0"},
				{Component: "bmc", CurrentVersion: "2.0"},
				{Component: "nic:0", CurrentVersion: "3.0"},
			})
			hwdata := createHardwareData("test-bmh", "test-ns", "HPE")

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bmh, hfc, hwdata).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-bmh",
					Namespace: "test-ns",
				},
			}

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			// Verify label was removed
			updatedBMH := &metal3v1alpha1.BareMetalHost{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-bmh", Namespace: "test-ns"}, updatedBMH)
			Expect(err).ToNot(HaveOccurred())
			_, exists := updatedBMH.Labels[ValidationUnavailableLabelKey]
			Expect(exists).To(BeFalse())
		})

		It("should not modify label when already set correctly", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				LabelResourcePoolID:           "pool123",
				LabelSiteID:                   "site123",
				ValidationUnavailableLabelKey: LabelValueMissingBIOSData,
			})
			hfc := createHFC("test-bmh", "test-ns", []metal3v1alpha1.FirmwareComponentStatus{
				{Component: "bmc", CurrentVersion: "2.0"},
				{Component: "nic:0", CurrentVersion: "3.0"},
			})
			hwdata := createHardwareData("test-bmh", "test-ns", "Dell Inc.")

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bmh, hfc, hwdata).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-bmh",
					Namespace: "test-ns",
				},
			}

			// Get initial resource version
			initialBMH := &metal3v1alpha1.BareMetalHost{}
			err := fakeClient.Get(ctx, types.NamespacedName{Name: "test-bmh", Namespace: "test-ns"}, initialBMH)
			Expect(err).ToNot(HaveOccurred())
			initialVersion := initialBMH.ResourceVersion

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			// Verify label is still set correctly and resource version unchanged
			updatedBMH := &metal3v1alpha1.BareMetalHost{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-bmh", Namespace: "test-ns"}, updatedBMH)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedBMH.Labels[ValidationUnavailableLabelKey]).To(Equal(LabelValueMissingBIOSData))
			Expect(updatedBMH.ResourceVersion).To(Equal(initialVersion))
		})
	})

	Describe("isHPEOrDell", func() {
		It("should return true for HPE manufacturer", func() {
			hwdata := createHardwareData("test-hwdata", "test-ns", "HPE")
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(hwdata).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			result := reconciler.isHPEOrDell(ctx, "test-ns", "test-hwdata")
			Expect(result).To(BeTrue())
		})

		It("should return true for Dell Inc. manufacturer", func() {
			hwdata := createHardwareData("test-hwdata", "test-ns", "Dell Inc.")
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(hwdata).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			result := reconciler.isHPEOrDell(ctx, "test-ns", "test-hwdata")
			Expect(result).To(BeTrue())
		})

		It("should return false for other manufacturers", func() {
			hwdata := createHardwareData("test-hwdata", "test-ns", "Supermicro")
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(hwdata).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			result := reconciler.isHPEOrDell(ctx, "test-ns", "test-hwdata")
			Expect(result).To(BeFalse())
		})

		It("should return false when HardwareData does not exist", func() {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			result := reconciler.isHPEOrDell(ctx, "test-ns", "nonexistent")
			Expect(result).To(BeFalse())
		})

		It("should return false when HardwareData has nil hardware details", func() {
			hwdata := &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hwdata",
					Namespace: "test-ns",
				},
				Spec: metal3v1alpha1.HardwareDataSpec{
					HardwareDetails: nil,
				},
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(hwdata).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			result := reconciler.isHPEOrDell(ctx, "test-ns", "test-hwdata")
			Expect(result).To(BeFalse())
		})
	})

	Describe("checkMissingComponents", func() {
		It("should return missing-firmware-data when no components exist", func() {
			hfc := createHFC("test-hfc", "test-ns", []metal3v1alpha1.FirmwareComponentStatus{})
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			result := reconciler.checkMissingComponents(ctx, hfc)
			Expect(result).To(Equal(LabelValueMissingFirmwareData))
		})

		It("should return missing-bios-data when only BIOS is missing", func() {
			hfc := createHFC("test-hfc", "test-ns", []metal3v1alpha1.FirmwareComponentStatus{
				{Component: "bmc", CurrentVersion: "1.0"},
				{Component: "nic:0", CurrentVersion: "2.0"},
			})
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			result := reconciler.checkMissingComponents(ctx, hfc)
			Expect(result).To(Equal(LabelValueMissingBIOSData))
		})

		It("should return missing-bmc-data when only BMC is missing", func() {
			hfc := createHFC("test-hfc", "test-ns", []metal3v1alpha1.FirmwareComponentStatus{
				{Component: "bios", CurrentVersion: "1.0"},
				{Component: "nic:0", CurrentVersion: "2.0"},
			})
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			result := reconciler.checkMissingComponents(ctx, hfc)
			Expect(result).To(Equal(LabelValueMissingBMCData))
		})

		It("should return missing-nic-data when only NIC is missing", func() {
			hfc := createHFC("test-hfc", "test-ns", []metal3v1alpha1.FirmwareComponentStatus{
				{Component: "bios", CurrentVersion: "1.0"},
				{Component: "bmc", CurrentVersion: "2.0"},
			})
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			result := reconciler.checkMissingComponents(ctx, hfc)
			Expect(result).To(Equal(LabelValueMissingNICData))
		})

		It("should return missing-firmware-data when multiple components missing", func() {
			hfc := createHFC("test-hfc", "test-ns", []metal3v1alpha1.FirmwareComponentStatus{
				{Component: "bios", CurrentVersion: "1.0"},
			})
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			result := reconciler.checkMissingComponents(ctx, hfc)
			Expect(result).To(Equal(LabelValueMissingFirmwareData))
		})

		It("should return empty string when all components present", func() {
			hfc := createHFC("test-hfc", "test-ns", []metal3v1alpha1.FirmwareComponentStatus{
				{Component: "bios", CurrentVersion: "1.0"},
				{Component: "bmc", CurrentVersion: "2.0"},
				{Component: "nic:0", CurrentVersion: "3.0"},
			})
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			result := reconciler.checkMissingComponents(ctx, hfc)
			Expect(result).To(Equal(""))
		})

		It("should handle case-insensitive component names", func() {
			hfc := createHFC("test-hfc", "test-ns", []metal3v1alpha1.FirmwareComponentStatus{
				{Component: "BIOS", CurrentVersion: "1.0"},
				{Component: "BMC", CurrentVersion: "2.0"},
				{Component: "NIC:0", CurrentVersion: "3.0"},
			})
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler = &HostFirmwareComponentsReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          logger,
			}

			result := reconciler.checkMissingComponents(ctx, hfc)
			Expect(result).To(Equal(""))
		})
	})
})
