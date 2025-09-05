/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Generated-By: Claude Code/claude-sonnet-4
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
)

var _ = Describe("Resource Selection", func() {
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

	createBMHForResourceSelection := func(name, namespace string, labels map[string]string, state metal3v1alpha1.ProvisioningState) *metal3v1alpha1.BareMetalHost {
		bmh := &metal3v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    labels,
			},
			Status: metal3v1alpha1.BareMetalHostStatus{
				Provisioning: metal3v1alpha1.ProvisionStatus{
					State: state,
				},
			},
		}
		return bmh
	}

	createHardwareData := func(name, namespace string, details *metal3v1alpha1.HardwareDetails) *metal3v1alpha1.HardwareData {
		return &metal3v1alpha1.HardwareData{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: metal3v1alpha1.HardwareDataSpec{
				HardwareDetails: details,
			},
		}
	}

	Describe("ResourceSelectionPrimaryFilter", func() {
		It("should create filters for site ID", func() {
			nodeGroupData := hwmgmtv1alpha1.NodeGroupData{
				ResourceSelector: map[string]string{},
			}

			opts, err := ResourceSelectionPrimaryFilter(ctx, nil, logger, "test-site", nodeGroupData)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(opts)).To(Equal(2)) // MatchingLabelsSelector + MatchingLabels

			// Check that the site ID filter is present
			matchingLabels, ok := opts[1].(client.MatchingLabels)
			Expect(ok).To(BeTrue())
			Expect(matchingLabels[LabelSiteID]).To(Equal("test-site"))
		})

		It("should create filters for resource pool ID", func() {
			nodeGroupData := hwmgmtv1alpha1.NodeGroupData{
				ResourcePoolId:   "test-pool",
				ResourceSelector: map[string]string{},
			}

			opts, err := ResourceSelectionPrimaryFilter(ctx, nil, logger, "", nodeGroupData)
			Expect(err).NotTo(HaveOccurred())

			matchingLabels, ok := opts[1].(client.MatchingLabels)
			Expect(ok).To(BeTrue())
			Expect(matchingLabels[LabelResourcePoolID]).To(Equal("test-pool"))
		})

		It("should create filters for resource selector labels", func() {
			nodeGroupData := hwmgmtv1alpha1.NodeGroupData{
				ResourceSelector: map[string]string{
					"zone":                              "east",
					LabelPrefixResourceSelector + "env": "prod",   // Use correct prefix
					HardwareDataPrefix + FieldCPUArch:   "x86_64", // Should be skipped
				},
			}

			opts, err := ResourceSelectionPrimaryFilter(ctx, nil, logger, "", nodeGroupData)
			Expect(err).NotTo(HaveOccurred())

			matchingLabels, ok := opts[1].(client.MatchingLabels)
			Expect(ok).To(BeTrue())
			Expect(matchingLabels[LabelPrefixResourceSelector+"zone"]).To(Equal("east"))
			Expect(matchingLabels[LabelPrefixResourceSelector+"env"]).To(Equal("prod"))
			_, hasHardwareData := matchingLabels[HardwareDataPrefix+FieldCPUArch]
			Expect(hasHardwareData).To(BeFalse()) // Should be excluded from primary filter
		})

		It("should exclude allocated BMHs", func() {
			nodeGroupData := hwmgmtv1alpha1.NodeGroupData{
				ResourceSelector: map[string]string{},
			}

			opts, err := ResourceSelectionPrimaryFilter(ctx, nil, logger, "", nodeGroupData)
			Expect(err).NotTo(HaveOccurred())

			// Check that the label selector excludes allocated BMHs
			labelSelector, ok := opts[0].(client.MatchingLabelsSelector)
			Expect(ok).To(BeTrue())

			requirements, _ := labelSelector.Selector.Requirements()
			found := false
			for _, req := range requirements {
				if req.Key() == BmhAllocatedLabel {
					Expect(string(req.Operator())).To(Equal("notin"))
					Expect(req.Values().List()).To(ContainElement(ValueTrue))
					found = true
					break
				}
			}
			Expect(found).To(BeTrue())
		})
	})

	Describe("ResourceSelectionSecondaryFilter", func() {
		var (
			fakeClient client.Client
		)

		BeforeEach(func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		})

		It("should filter BMHs by availability state", func() {
			bmhList := metal3v1alpha1.BareMetalHostList{
				Items: []metal3v1alpha1.BareMetalHost{
					*createBMHForResourceSelection("bmh1", "test-ns", nil, metal3v1alpha1.StateAvailable),
					*createBMHForResourceSelection("bmh2", "test-ns", nil, metal3v1alpha1.StateProvisioning),
				},
			}

			nodeGroupData := hwmgmtv1alpha1.NodeGroupData{
				ResourceSelector: map[string]string{},
			}

			result, err := ResourceSelectionSecondaryFilter(ctx, fakeClient, logger, nodeGroupData, bmhList)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(result.Items)).To(Equal(0)) // No hardware data available, so no BMHs pass
		})

		It("should filter BMHs with hardware data criteria", func() {
			// Create BMH and corresponding HardwareData
			bmh1 := createBMHForResourceSelection("bmh1", "test-ns", nil, metal3v1alpha1.StateAvailable)
			hwData1 := createHardwareData("bmh1", "test-ns", &metal3v1alpha1.HardwareDetails{
				CPU: metal3v1alpha1.CPU{
					Arch:  "x86_64",
					Model: "Intel Xeon",
					Count: 8,
				},
				RAMMebibytes: 16384,
			})

			bmh2 := createBMHForResourceSelection("bmh2", "test-ns", nil, metal3v1alpha1.StateAvailable)
			hwData2 := createHardwareData("bmh2", "test-ns", &metal3v1alpha1.HardwareDetails{
				CPU: metal3v1alpha1.CPU{
					Arch:  "arm64",
					Model: "ARM Cortex",
					Count: 4,
				},
				RAMMebibytes: 8192,
			})

			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh1, bmh2, hwData1, hwData2).Build()

			bmhList := metal3v1alpha1.BareMetalHostList{
				Items: []metal3v1alpha1.BareMetalHost{*bmh1, *bmh2},
			}

			nodeGroupData := hwmgmtv1alpha1.NodeGroupData{
				ResourceSelector: map[string]string{
					HardwareDataPrefix + FieldCPUArch: "x86_64",
				},
			}

			result, err := ResourceSelectionSecondaryFilter(ctx, fakeClient, logger, nodeGroupData, bmhList)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(result.Items)).To(Equal(1))
			Expect(result.Items[0].Name).To(Equal("bmh1"))
		})

		It("should handle BMHs without hardware data", func() {
			bmh1 := createBMHForResourceSelection("bmh1", "test-ns", nil, metal3v1alpha1.StateAvailable)
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh1).Build()

			bmhList := metal3v1alpha1.BareMetalHostList{
				Items: []metal3v1alpha1.BareMetalHost{*bmh1},
			}

			nodeGroupData := hwmgmtv1alpha1.NodeGroupData{
				ResourceSelector: map[string]string{
					HardwareDataPrefix + FieldCPUArch: "x86_64",
				},
			}

			result, err := ResourceSelectionSecondaryFilter(ctx, fakeClient, logger, nodeGroupData, bmhList)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(result.Items)).To(Equal(0))
		})
	})

	Describe("ResourceSelectionSecondaryFilterHardwareData", func() {
		It("should return true for non-hardwaredata criteria", func() {
			hwData := createHardwareData("test", "test-ns", &metal3v1alpha1.HardwareDetails{})

			result, err := ResourceSelectionSecondaryFilterHardwareData(ctx, nil, logger, "zone", "east", hwData)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})

		It("should filter by CPU architecture", func() {
			hwData := createHardwareData("test", "test-ns", &metal3v1alpha1.HardwareDetails{
				CPU: metal3v1alpha1.CPU{Arch: "x86_64"},
			})

			result, err := ResourceSelectionSecondaryFilterHardwareData(ctx, nil, logger, HardwareDataPrefix+FieldCPUArch, "x86_64", hwData)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())

			result, err = ResourceSelectionSecondaryFilterHardwareData(ctx, nil, logger, HardwareDataPrefix+FieldCPUArch, "arm64", hwData)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
		})

		It("should filter by CPU model", func() {
			hwData := createHardwareData("test", "test-ns", &metal3v1alpha1.HardwareDetails{
				CPU: metal3v1alpha1.CPU{Model: "Intel Xeon"},
			})

			result, err := ResourceSelectionSecondaryFilterHardwareData(ctx, nil, logger, HardwareDataPrefix+FieldCPUModel, "Intel Xeon", hwData)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())

			result, err = ResourceSelectionSecondaryFilterHardwareData(ctx, nil, logger, HardwareDataPrefix+FieldCPUModel, "AMD EPYC", hwData)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
		})

		It("should filter by number of threads", func() {
			hwData := createHardwareData("test", "test-ns", &metal3v1alpha1.HardwareDetails{
				CPU: metal3v1alpha1.CPU{Count: 8},
			})

			result, err := ResourceSelectionSecondaryFilterHardwareData(ctx, nil, logger, HardwareDataPrefix+FieldNumThreads, "8", hwData)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())

			result, err = ResourceSelectionSecondaryFilterHardwareData(ctx, nil, logger, HardwareDataPrefix+FieldNumThreads+";gt", "4", hwData)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())

			result, err = ResourceSelectionSecondaryFilterHardwareData(ctx, nil, logger, HardwareDataPrefix+FieldNumThreads+";lt", "4", hwData)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
		})

		It("should filter by RAM", func() {
			hwData := createHardwareData("test", "test-ns", &metal3v1alpha1.HardwareDetails{
				RAMMebibytes: 16384,
			})

			result, err := ResourceSelectionSecondaryFilterHardwareData(ctx, nil, logger, HardwareDataPrefix+FieldRAM, "16384", hwData)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())

			result, err = ResourceSelectionSecondaryFilterHardwareData(ctx, nil, logger, HardwareDataPrefix+FieldRAM+";gte", "8192", hwData)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})

		It("should filter by manufacturer", func() {
			hwData := createHardwareData("test", "test-ns", &metal3v1alpha1.HardwareDetails{
				SystemVendor: metal3v1alpha1.HardwareSystemVendor{
					Manufacturer: "Dell Inc.",
				},
			})

			result, err := ResourceSelectionSecondaryFilterHardwareData(ctx, nil, logger, HardwareDataPrefix+FieldManufacturer, "Dell Inc.", hwData)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())

			result, err = ResourceSelectionSecondaryFilterHardwareData(ctx, nil, logger, HardwareDataPrefix+FieldManufacturer, "HP", hwData)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
		})

		It("should filter by product name", func() {
			hwData := createHardwareData("test", "test-ns", &metal3v1alpha1.HardwareDetails{
				SystemVendor: metal3v1alpha1.HardwareSystemVendor{
					ProductName: "PowerEdge R640",
				},
			})

			result, err := ResourceSelectionSecondaryFilterHardwareData(ctx, nil, logger, HardwareDataPrefix+FieldProductName, "PowerEdge R640", hwData)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})
	})

	Describe("String matching with qualifiers", func() {
		Describe("checkStringWithQualifiers", func() {
			It("should match exact strings by default", func() {
				result := checkStringWithQualifiers([]string{}, "test", "test", true)
				Expect(result).To(BeTrue())

				result = checkStringWithQualifiers([]string{}, "test", "Test", true)
				Expect(result).To(BeFalse())
			})

			It("should match case insensitive with icase qualifier", func() {
				result := checkStringWithQualifiers([]string{"icase"}, "test", "Test", false)
				Expect(result).To(BeTrue())

				result = checkStringWithQualifiers([]string{"icase"}, "test", "TEST", false)
				Expect(result).To(BeTrue())
			})

			It("should match substrings with substring qualifier", func() {
				result := checkStringWithQualifiers([]string{"substring"}, "Dell", "Dell Inc.", true)
				Expect(result).To(BeTrue())

				result = checkStringWithQualifiers([]string{"substring"}, "HP", "Dell Inc.", true)
				Expect(result).To(BeFalse())
			})

			It("should combine qualifiers", func() {
				result := checkStringWithQualifiers([]string{"substring", "icase"}, "DELL", "Dell Inc.", false)
				Expect(result).To(BeTrue())

				result = checkStringWithQualifiers([]string{"icase", "substring"}, "inc", "Dell Inc.", false)
				Expect(result).To(BeTrue())
			})
		})
	})

	Describe("Integer matching with qualifiers", func() {
		Describe("checkIntWithQualifiers", func() {
			It("should match exact values by default", func() {
				result, err := checkIntWithQualifiers([]string{}, "8", 8)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkIntWithQualifiers([]string{}, "8", 4)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeFalse())
			})

			It("should handle greater than comparisons", func() {
				result, err := checkIntWithQualifiers([]string{"gt"}, "4", 8)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkIntWithQualifiers([]string{">"}, "4", 8)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkIntWithQualifiers([]string{"gt"}, "8", 8)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeFalse())
			})

			It("should handle greater than or equal comparisons", func() {
				result, err := checkIntWithQualifiers([]string{"gte"}, "8", 8)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkIntWithQualifiers([]string{">="}, "8", 16)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkIntWithQualifiers([]string{"gte"}, "16", 8)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeFalse())
			})

			It("should handle less than comparisons", func() {
				result, err := checkIntWithQualifiers([]string{"lt"}, "16", 8)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkIntWithQualifiers([]string{"<"}, "16", 8)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkIntWithQualifiers([]string{"lt"}, "8", 8)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeFalse())
			})

			It("should handle less than or equal comparisons", func() {
				result, err := checkIntWithQualifiers([]string{"lte"}, "8", 8)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkIntWithQualifiers([]string{"<="}, "16", 8)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkIntWithQualifiers([]string{"lte"}, "4", 8)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeFalse())
			})

			It("should handle equality and inequality", func() {
				result, err := checkIntWithQualifiers([]string{"eq"}, "8", 8)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkIntWithQualifiers([]string{"=="}, "8", 8)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkIntWithQualifiers([]string{"neq"}, "8", 4)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkIntWithQualifiers([]string{"!="}, "8", 4)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkIntWithQualifiers([]string{"neq"}, "8", 8)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeFalse())
			})

			It("should return error for invalid qualifiers", func() {
				_, err := checkIntWithQualifiers([]string{"invalid"}, "8", 8)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid qualifier"))
			})

			It("should return error for invalid values", func() {
				_, err := checkIntWithQualifiers([]string{}, "invalid", 8)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid value"))
			})

			It("should return error for multiple qualifiers", func() {
				_, err := checkIntWithQualifiers([]string{"gt", "lt"}, "8", 8)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("supports at most one qualifier"))
			})
		})
	})

	Describe("NIC filtering", func() {
		Describe("checkNicsWithQualifiers", func() {
			var nics []metal3v1alpha1.NIC

			BeforeEach(func() {
				nics = []metal3v1alpha1.NIC{
					{Model: "Intel E1000", SpeedGbps: 1},
					{Model: "Broadcom BCM", SpeedGbps: 10},
					{Model: "Intel X710", SpeedGbps: 10},
				}
			})

			It("should filter by model", func() {
				result, err := checkNicsWithQualifiers([]string{"model=Intel E1000"}, "present", nics, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkNicsWithQualifiers([]string{"model!=Intel E1000"}, "present", nics, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue()) // Should match other NICs

				result, err = checkNicsWithQualifiers([]string{"model=NonExistent"}, "present", nics, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeFalse())
			})

			It("should filter by speed", func() {
				result, err := checkNicsWithQualifiers([]string{"speedGbps==10"}, "present", nics, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkNicsWithQualifiers([]string{"speedGbps>5"}, "present", nics, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkNicsWithQualifiers([]string{"speedGbps>20"}, "present", nics, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeFalse())
			})

			It("should filter by count", func() {
				result, err := checkNicsWithQualifiers([]string{"count==3"}, "present", nics, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkNicsWithQualifiers([]string{"count>=2"}, "present", nics, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkNicsWithQualifiers([]string{"count>5"}, "present", nics, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeFalse())
			})

			It("should combine multiple criteria", func() {
				// Should match Intel NICs with speed >= 10
				result, err := checkNicsWithQualifiers([]string{"model~Intel", "speedGbps>=10"}, "present", nics, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				// Should count NICs with speed >= 10 and check if count >= 2
				result, err = checkNicsWithQualifiers([]string{"speedGbps>=10", "count>=2"}, "present", nics, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())
			})
		})
	})

	Describe("Storage filtering", func() {
		Describe("checkStorageWithQualifiers", func() {
			var storage []metal3v1alpha1.Storage

			BeforeEach(func() {
				storage = []metal3v1alpha1.Storage{
					{
						Name:      "sda",
						Type:      metal3v1alpha1.HDD,
						SizeBytes: 1000000000000, // 1TB
						Vendor:    "Seagate",
						Model:     "ST1000",
					},
					{
						Name:      "sdb",
						Type:      metal3v1alpha1.SSD,
						SizeBytes: 500000000000, // 500GB
						Vendor:    "Samsung",
						Model:     "860 EVO",
					},
				}
			})

			It("should filter by storage type", func() {
				result, err := checkStorageWithQualifiers([]string{"type=HDD"}, "present", storage, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkStorageWithQualifiers([]string{"type=SSD"}, "present", storage, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkStorageWithQualifiers([]string{"type=NVME"}, "present", storage, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeFalse())
			})

			It("should filter by size", func() {
				result, err := checkStorageWithQualifiers([]string{"sizeBytes>600000000000"}, "present", storage, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkStorageWithQualifiers([]string{"sizeBytes<400000000000"}, "present", storage, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeFalse())
			})

			It("should filter by vendor", func() {
				result, err := checkStorageWithQualifiers([]string{"vendor=Seagate"}, "present", storage, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkStorageWithQualifiers([]string{"vendor~Sam"}, "present", storage, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkStorageWithQualifiers([]string{"vendor=Intel"}, "present", storage, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeFalse())
			})

			It("should filter by model", func() {
				result, err := checkStorageWithQualifiers([]string{"model=ST1000"}, "present", storage, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkStorageWithQualifiers([]string{"model~EVO"}, "present", storage, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())
			})

			It("should filter by name including alternate names", func() {
				storage[0].AlternateNames = []string{"disk1", "primary-disk"}

				result, err := checkStorageWithQualifiers([]string{"name=sda"}, "present", storage, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkStorageWithQualifiers([]string{"name=disk1"}, "present", storage, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkStorageWithQualifiers([]string{"name=nonexistent"}, "present", storage, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeFalse())
			})

			It("should filter by count", func() {
				result, err := checkStorageWithQualifiers([]string{"count==2"}, "present", storage, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkStorageWithQualifiers([]string{"count>=1"}, "present", storage, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkStorageWithQualifiers([]string{"count>5"}, "present", storage, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeFalse())
			})

			It("should combine multiple criteria", func() {
				// Should count SSD devices and verify count >= 1
				result, err := checkStorageWithQualifiers([]string{"type=SSD", "count>=1"}, "present", storage, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				// Should match devices with size > 600GB and vendor Seagate
				result, err = checkStorageWithQualifiers([]string{"sizeBytes>600000000000", "vendor=Seagate"}, "present", storage, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())
			})
		})
	})

	Describe("Operator qualifier parsing", func() {
		Describe("qualifierSetFromQualifiers", func() {
			It("should parse simple qualifiers", func() {
				qualifiers := []string{"model=Intel", "speedGbps>10"}

				result, err := qualifierSetFromQualifiers(qualifiers)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(result)).To(Equal(2))
				Expect(result["model"].Op).To(Equal("="))
				Expect(result["model"].Value).To(Equal("Intel"))
				Expect(result["speedGbps"].Op).To(Equal(">"))
				Expect(result["speedGbps"].Value).To(Equal("10"))
			})

			It("should handle complex operators", func() {
				qualifiers := []string{"size>=1000", "name!=sda", "vendor~Samsung"}

				result, err := qualifierSetFromQualifiers(qualifiers)
				Expect(err).NotTo(HaveOccurred())
				Expect(result["size"].Op).To(Equal(">="))
				Expect(result["size"].Value).To(Equal("1000"))
				Expect(result["name"].Op).To(Equal("!="))
				Expect(result["name"].Value).To(Equal("sda"))
				Expect(result["vendor"].Op).To(Equal("~"))
				Expect(result["vendor"].Value).To(Equal("Samsung"))
			})

			It("should return error for invalid qualifier format", func() {
				qualifiers := []string{"invalidqualifier"}

				_, err := qualifierSetFromQualifiers(qualifiers)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid qualifier"))
			})
		})
	})

	Describe("String operator qualifiers", func() {
		Describe("checkStringWithOpQualifier", func() {
			It("should handle equality operators", func() {
				result, err := checkStringWithOpQualifier("=", "test", "test", true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkStringWithOpQualifier("!=", "test", "other", true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkStringWithOpQualifier("!=", "test", "test", true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeFalse())
			})

			It("should handle substring operators", func() {
				result, err := checkStringWithOpQualifier("~", "tel", "Intel", true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkStringWithOpQualifier("!~", "AMD", "Intel", true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkStringWithOpQualifier("!~", "tel", "Intel", true)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeFalse())
			})

			It("should return error for invalid operators", func() {
				_, err := checkStringWithOpQualifier("invalid", "test", "test", true)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid operator"))
			})
		})
	})

	Describe("Integer operator qualifiers", func() {
		Describe("checkIntWithOpQualifier", func() {
			It("should handle all comparison operators", func() {
				result, err := checkIntWithOpQualifier("==", "8", 8)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkIntWithOpQualifier("!=", "8", 4)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkIntWithOpQualifier(">", "4", 8)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkIntWithOpQualifier(">=", "8", 8)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkIntWithOpQualifier("<", "16", 8)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())

				result, err = checkIntWithOpQualifier("<=", "8", 8)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeTrue())
			})

			It("should return error for invalid values", func() {
				_, err := checkIntWithOpQualifier("==", "invalid", 8)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid value"))
			})

			It("should return error for invalid operators", func() {
				_, err := checkIntWithOpQualifier("invalid", "8", 8)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid operator"))
			})
		})
	})

	Describe("Constants and patterns", func() {
		It("should have correct hardwaredata prefix", func() {
			Expect(HardwareDataPrefix).To(Equal("hardwaredata/"))
		})

		It("should have correct regex patterns", func() {
			// Test hardwaredata pattern
			matches := REPatternHardwareData.FindStringSubmatch(HardwareDataPrefix + FieldCPUArch)
			Expect(matches).To(HaveLen(2))
			Expect(matches[1]).To(Equal(FieldCPUArch))

			matches = REPatternHardwareData.FindStringSubmatch("other/" + FieldCPUArch)
			Expect(matches).To(BeNil())

			// Test qualifier operation pattern
			matches = REPatternQualifierOp.FindStringSubmatch("model=Intel")
			Expect(matches).To(HaveLen(4)) // Full match + 3 groups (but 0-indexed: [0] is full match, [1], [2], [3] are groups)
			Expect(matches[1]).To(Equal("model"))
			Expect(matches[2]).To(Equal("="))
			Expect(matches[3]).To(Equal("Intel"))
		})
	})
})
