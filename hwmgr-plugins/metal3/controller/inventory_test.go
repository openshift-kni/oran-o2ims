/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"log/slog"
	"regexp"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/server/inventory"
)

var _ = Describe("Inventory", func() {

	// Helper function to create a basic BareMetalHost for testing
	createBasicBMH := func(name, namespace string) *metal3v1alpha1.BareMetalHost {
		return &metal3v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}
	}

	// Helper function to create BareMetalHost with labels
	createBMHWithLabels := func(name, namespace string, labels map[string]string) *metal3v1alpha1.BareMetalHost {
		bmh := createBasicBMH(name, namespace)
		bmh.Labels = labels
		return bmh
	}

	// Helper function to create BareMetalHost with annotations
	createBMHWithAnnotations := func(name, namespace string, annotations map[string]string) *metal3v1alpha1.BareMetalHost {
		bmh := createBasicBMH(name, namespace)
		bmh.Annotations = annotations
		return bmh
	}

	// Helper function to create BareMetalHost with hardware details
	createBMHWithHardwareDetails := func(name, namespace string) *metal3v1alpha1.BareMetalHost {
		bmh := createBasicBMH(name, namespace)
		bmh.Status.HardwareDetails = &metal3v1alpha1.HardwareDetails{
			RAMMebibytes: 8192,
			SystemVendor: metal3v1alpha1.HardwareSystemVendor{
				Manufacturer: "Dell Inc.",
				ProductName:  "PowerEdge R640",
				SerialNumber: "ABC123456",
			},
			CPU: metal3v1alpha1.CPU{
				Arch:  "x86_64",
				Model: "Intel Xeon Gold 6138",
				Count: 40,
			},
		}
		return bmh
	}

	// Helper function to create AllocatedNode
	createAllocatedNode := func(name, hwProfile string) *pluginsv1alpha1.AllocatedNode {
		return &pluginsv1alpha1.AllocatedNode{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Status: pluginsv1alpha1.AllocatedNodeStatus{
				HwProfile: hwProfile,
			},
		}
	}

	Describe("getResourceInfoAdminState", func() {
		It("should return UNKNOWN admin state", func() {
			result := getResourceInfoAdminState()
			Expect(result).To(Equal(inventory.ResourceInfoAdminStateUNKNOWN))
		})
	})

	Describe("getResourceInfoDescription", func() {
		It("should return description from annotations when present", func() {
			bmh := createBMHWithAnnotations("test-bmh", "test-ns", map[string]string{
				AnnotationResourceInfoDescription: "Test description",
			})
			result := getResourceInfoDescription(*bmh)
			Expect(result).To(Equal("Test description"))
		})

		It("should return empty string when annotations are nil", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			result := getResourceInfoDescription(*bmh)
			Expect(result).To(Equal(""))
		})

		It("should return empty string when description annotation is missing", func() {
			bmh := createBMHWithAnnotations("test-bmh", "test-ns", map[string]string{
				"other.annotation": "other value",
			})
			result := getResourceInfoDescription(*bmh)
			Expect(result).To(Equal(""))
		})
	})

	Describe("getResourceInfoGlobalAssetId", func() {
		It("should return global asset ID from annotations when present", func() {
			bmh := createBMHWithAnnotations("test-bmh", "test-ns", map[string]string{
				AnnotationResourceInfoGlobalAssetId: "GA123456",
			})
			result := getResourceInfoGlobalAssetId(*bmh)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal("GA123456"))
		})

		It("should return pointer to empty string when annotations are nil", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			result := getResourceInfoGlobalAssetId(*bmh)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal(""))
		})
	})

	Describe("getResourceInfoGroups", func() {
		It("should parse comma-separated groups from annotations", func() {
			bmh := createBMHWithAnnotations("test-bmh", "test-ns", map[string]string{
				AnnotationsResourceInfoGroups: "group1,group2,group3",
			})
			result := getResourceInfoGroups(*bmh)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal([]string{"group1", "group2", "group3"}))
		})

		It("should handle groups with spaces around commas", func() {
			bmh := createBMHWithAnnotations("test-bmh", "test-ns", map[string]string{
				AnnotationsResourceInfoGroups: "group1 , group2 , group3",
			})
			result := getResourceInfoGroups(*bmh)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal([]string{"group1", "group2", "group3"}))
		})

		It("should return nil when annotations are nil", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			result := getResourceInfoGroups(*bmh)
			Expect(result).To(BeNil())
		})

		It("should return nil when groups annotation is missing", func() {
			bmh := createBMHWithAnnotations("test-bmh", "test-ns", map[string]string{
				"other.annotation": "other value",
			})
			result := getResourceInfoGroups(*bmh)
			Expect(result).To(BeNil())
		})
	})

	Describe("getResourceInfoLabels", func() {
		It("should return all labels when present", func() {
			labels := map[string]string{
				"label1": "value1",
				"label2": "value2",
			}
			bmh := createBMHWithLabels("test-bmh", "test-ns", labels)
			result := getResourceInfoLabels(bmh)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal(labels))
		})

		It("should return nil when labels are nil", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			result := getResourceInfoLabels(bmh)
			Expect(result).To(BeNil())
		})
	})

	Describe("getResourceInfoMemory", func() {
		It("should return memory from hardware details when present", func() {
			bmh := createBMHWithHardwareDetails("test-bmh", "test-ns")
			result := getResourceInfoMemory(bmh)
			Expect(result).To(Equal(8192))
		})

		It("should return 0 when hardware details are nil", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			result := getResourceInfoMemory(bmh)
			Expect(result).To(Equal(0))
		})
	})

	Describe("getResourceInfoModel", func() {
		It("should return model from hardware details when present", func() {
			bmh := createBMHWithHardwareDetails("test-bmh", "test-ns")
			result := getResourceInfoModel(bmh)
			Expect(result).To(Equal("PowerEdge R640"))
		})

		It("should return empty string when hardware details are nil", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			result := getResourceInfoModel(bmh)
			Expect(result).To(Equal(""))
		})
	})

	Describe("getResourceInfoName", func() {
		It("should return BMH name", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			result := getResourceInfoName(bmh)
			Expect(result).To(Equal("test-bmh"))
		})
	})

	Describe("getResourceInfoOperationalState", func() {
		It("should return UNKNOWN operational state", func() {
			result := getResourceInfoOperationalState()
			Expect(result).To(Equal(inventory.ResourceInfoOperationalStateUNKNOWN))
		})
	})

	Describe("getResourceInfoPartNumber", func() {
		It("should return part number from annotations when present", func() {
			bmh := createBMHWithAnnotations("test-bmh", "test-ns", map[string]string{
				AnnotationResourceInfoPartNumber: "PN123456",
			})
			result := getResourceInfoPartNumber(*bmh)
			Expect(result).To(Equal("PN123456"))
		})

		It("should return empty string when annotations are nil", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			result := getResourceInfoPartNumber(*bmh)
			Expect(result).To(Equal(""))
		})
	})

	Describe("getResourceInfoPowerState", func() {
		It("should return ON when BMH is powered on", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.PoweredOn = true
			result := getResourceInfoPowerState(bmh)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal(inventory.ON))
		})

		It("should return OFF when BMH is powered off", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.PoweredOn = false
			result := getResourceInfoPowerState(bmh)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal(inventory.OFF))
		})
	})

	Describe("getProcessorInfoArchitecture", func() {
		It("should return CPU architecture from hardware details when present", func() {
			bmh := createBMHWithHardwareDetails("test-bmh", "test-ns")
			result := getProcessorInfoArchitecture(bmh)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal("x86_64"))
		})

		It("should return pointer to empty string when hardware details are nil", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			result := getProcessorInfoArchitecture(bmh)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal(""))
		})
	})

	Describe("getProcessorInfoCores", func() {
		It("should return CPU core count from hardware details when present", func() {
			bmh := createBMHWithHardwareDetails("test-bmh", "test-ns")
			result := getProcessorInfoCores(bmh)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal(40))
		})

		It("should return nil when hardware details are nil", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			result := getProcessorInfoCores(bmh)
			Expect(result).To(BeNil())
		})
	})

	Describe("getProcessorInfoManufacturer", func() {
		It("should return pointer to empty string", func() {
			result := getProcessorInfoManufacturer()
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal(""))
		})
	})

	Describe("getProcessorInfoModel", func() {
		It("should return CPU model from hardware details when present", func() {
			bmh := createBMHWithHardwareDetails("test-bmh", "test-ns")
			result := getProcessorInfoModel(bmh)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal("Intel Xeon Gold 6138"))
		})

		It("should return pointer to empty string when hardware details are nil", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			result := getProcessorInfoModel(bmh)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal(""))
		})
	})

	Describe("getResourceInfoProcessors", func() {
		It("should return processor info array with hardware details", func() {
			bmh := createBMHWithHardwareDetails("test-bmh", "test-ns")
			result := getResourceInfoProcessors(bmh)
			Expect(result).To(HaveLen(1))

			processor := result[0]
			Expect(*processor.Architecture).To(Equal("x86_64"))
			Expect(*processor.Cores).To(Equal(40))
			Expect(*processor.Manufacturer).To(Equal(""))
			Expect(*processor.Model).To(Equal("Intel Xeon Gold 6138"))
		})

		It("should return empty array when hardware details are nil", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			result := getResourceInfoProcessors(bmh)
			Expect(result).To(HaveLen(0))
		})
	})

	Describe("getResourceInfoResourceId", func() {
		It("should return formatted resource ID", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			result := getResourceInfoResourceId(*bmh)
			Expect(result).To(Equal("test-ns/test-bmh"))
		})
	})

	Describe("getResourceInfoResourcePoolId", func() {
		It("should return resource pool ID from labels", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				LabelResourcePoolID: "pool123",
			})
			result := getResourceInfoResourcePoolId(bmh)
			Expect(result).To(Equal("pool123"))
		})
	})

	Describe("getResourceInfoResourceProfileId", func() {
		It("should return HW profile from allocated node when present", func() {
			node := createAllocatedNode("test-node", "profile123")
			result := getResourceInfoResourceProfileId(node)
			Expect(result).To(Equal("profile123"))
		})

		It("should return empty string when node is nil", func() {
			result := getResourceInfoResourceProfileId(nil)
			Expect(result).To(Equal(""))
		})
	})

	Describe("getResourceInfoSerialNumber", func() {
		It("should return serial number from hardware details when present", func() {
			bmh := createBMHWithHardwareDetails("test-bmh", "test-ns")
			result := getResourceInfoSerialNumber(bmh)
			Expect(result).To(Equal("ABC123456"))
		})

		It("should return empty string when hardware details are nil", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			result := getResourceInfoSerialNumber(bmh)
			Expect(result).To(Equal(""))
		})
	})

	Describe("getResourceInfoTags", func() {
		It("should extract resource selector labels as tags", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				LabelPrefixResourceSelector + "zone":        "zone1",
				LabelPrefixResourceSelector + "environment": "prod",
				"other.label": "other-value",
			})
			result := getResourceInfoTags(bmh)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(ContainElements("zone: zone1", "environment: prod"))
			Expect(*result).ToNot(ContainElement("other.label: other-value"))
		})

		It("should return empty array when no resource selector labels present", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				"other.label": "other-value",
			})
			result := getResourceInfoTags(bmh)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(HaveLen(0))
		})
	})

	Describe("getResourceInfoUsageState", func() {
		It("should return UNKNOWN usage state", func() {
			result := getResourceInfoUsageState()
			Expect(result).To(Equal(inventory.UNKNOWN))
		})
	})

	Describe("getResourceInfoVendor", func() {
		It("should return vendor from hardware details when present", func() {
			bmh := createBMHWithHardwareDetails("test-bmh", "test-ns")
			result := getResourceInfoVendor(bmh)
			Expect(result).To(Equal("Dell Inc."))
		})

		It("should return empty string when hardware details are nil", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			result := getResourceInfoVendor(bmh)
			Expect(result).To(Equal(""))
		})
	})

	Describe("includeInInventory", func() {
		It("should return false when labels are nil", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			result := includeInInventory(bmh)
			Expect(result).To(BeFalse())
		})

		It("should return false when resource pool ID label is missing", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				LabelSiteID: "site123",
			})
			result := includeInInventory(bmh)
			Expect(result).To(BeFalse())
		})

		It("should return false when site ID label is missing", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				LabelResourcePoolID: "pool123",
			})
			result := includeInInventory(bmh)
			Expect(result).To(BeFalse())
		})

		Context("with required labels present", func() {
			var bmh *metal3v1alpha1.BareMetalHost

			BeforeEach(func() {
				bmh = createBMHWithLabels("test-bmh", "test-ns", map[string]string{
					LabelResourcePoolID: "pool123",
					LabelSiteID:         "site123",
				})
			})

			It("should return true for StateAvailable", func() {
				bmh.Status.Provisioning.State = metal3v1alpha1.StateAvailable
				result := includeInInventory(bmh)
				Expect(result).To(BeTrue())
			})

			It("should return true for StateProvisioning", func() {
				bmh.Status.Provisioning.State = metal3v1alpha1.StateProvisioning
				result := includeInInventory(bmh)
				Expect(result).To(BeTrue())
			})

			It("should return true for StateProvisioned", func() {
				bmh.Status.Provisioning.State = metal3v1alpha1.StateProvisioned
				result := includeInInventory(bmh)
				Expect(result).To(BeTrue())
			})

			It("should return true for StatePreparing", func() {
				bmh.Status.Provisioning.State = metal3v1alpha1.StatePreparing
				result := includeInInventory(bmh)
				Expect(result).To(BeTrue())
			})

			It("should return true for StateDeprovisioning", func() {
				bmh.Status.Provisioning.State = metal3v1alpha1.StateDeprovisioning
				result := includeInInventory(bmh)
				Expect(result).To(BeTrue())
			})

			It("should return false for other states", func() {
				bmh.Status.Provisioning.State = metal3v1alpha1.StateInspecting
				result := includeInInventory(bmh)
				Expect(result).To(BeFalse())
			})
		})
	})

	Describe("getResourceInfo", func() {
		It("should aggregate all resource information correctly", func() {
			bmh := createBMHWithHardwareDetails("test-bmh", "test-ns")
			bmh.Labels = map[string]string{
				LabelResourcePoolID:                  "pool123",
				LabelPrefixResourceSelector + "zone": "zone1",
			}
			bmh.Annotations = map[string]string{
				AnnotationResourceInfoDescription: "Test description",
				AnnotationResourceInfoPartNumber:  "PN123456",
			}
			bmh.Status.PoweredOn = true

			node := createAllocatedNode("test-node", "profile123")

			result := getResourceInfo(bmh, node)

			Expect(result.AdminState).To(Equal(inventory.ResourceInfoAdminStateUNKNOWN))
			Expect(result.Description).To(Equal("Test description"))
			Expect(result.HwProfile).To(Equal("profile123"))
			Expect(result.Memory).To(Equal(8192))
			Expect(result.Model).To(Equal("PowerEdge R640"))
			Expect(result.Name).To(Equal("test-bmh"))
			Expect(result.OperationalState).To(Equal(inventory.ResourceInfoOperationalStateUNKNOWN))
			Expect(result.PartNumber).To(Equal("PN123456"))
			Expect(*result.PowerState).To(Equal(inventory.ON))
			Expect(result.Processors).To(HaveLen(1))
			Expect(result.ResourceId).To(Equal("test-ns/test-bmh"))
			Expect(result.ResourcePoolId).To(Equal("pool123"))
			Expect(result.SerialNumber).To(Equal("ABC123456"))
			Expect(*result.Tags).To(ContainElement("zone: zone1"))
			Expect(result.UsageState).To(Equal(inventory.UNKNOWN))
			Expect(result.Vendor).To(Equal("Dell Inc."))
		})
	})

	Describe("GetResourcePools", func() {
		var (
			ctx    context.Context
			scheme *runtime.Scheme
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme = runtime.NewScheme()
			Expect(metal3v1alpha1.AddToScheme(scheme)).To(Succeed())
		})

		It("should return resource pools from BMHs included in inventory", func() {
			// Create BMHs with required labels and valid states
			bmh1 := createBMHWithLabels("bmh1", "ns1", map[string]string{
				LabelResourcePoolID: "pool1",
				LabelSiteID:         "site1",
			})
			bmh1.Status.Provisioning.State = metal3v1alpha1.StateAvailable

			bmh2 := createBMHWithLabels("bmh2", "ns2", map[string]string{
				LabelResourcePoolID: "pool2",
				LabelSiteID:         "site2",
			})
			bmh2.Status.Provisioning.State = metal3v1alpha1.StateProvisioned

			// Create BMH that should be excluded (missing labels)
			bmh3 := createBasicBMH("bmh3", "ns3")

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bmh1, bmh2, bmh3).
				Build()

			result, err := GetResourcePools(ctx, client)
			Expect(err).ToNot(HaveOccurred())

			response, ok := result.(inventory.GetResourcePools200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(response).To(HaveLen(2))

			// Check if both pools are present
			poolIds := make([]string, len(response))
			for i, pool := range response {
				poolIds[i] = pool.ResourcePoolId
			}
			Expect(poolIds).To(ContainElements("pool1", "pool2"))
		})

		It("should handle empty BMH list", func() {
			client := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			result, err := GetResourcePools(ctx, client)
			Expect(err).ToNot(HaveOccurred())

			response, ok := result.(inventory.GetResourcePools200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(response).To(HaveLen(0))
		})
	})

	Describe("GetResources", func() {
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
			Expect(pluginsv1alpha1.AddToScheme(scheme)).To(Succeed())
		})

		It("should return resources from BMHs included in inventory", func() {
			// Create BMH with required labels and valid state
			bmh := createBMHWithHardwareDetails("test-bmh", "test-ns")
			bmh.Labels = map[string]string{
				LabelResourcePoolID: "pool123",
				LabelSiteID:         "site123",
			}
			bmh.Status.Provisioning.State = metal3v1alpha1.StateAvailable

			// Create AllocatedNode that corresponds to this BMH
			node := createAllocatedNode("test-node", "profile123")
			node.Spec.HwMgrNodeId = "test-bmh"
			node.Spec.HwMgrNodeNs = "test-ns"

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bmh, node).
				Build()

			result, err := GetResources(ctx, logger, client)
			Expect(err).ToNot(HaveOccurred())

			response, ok := result.(inventory.GetResources200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(response).To(HaveLen(1))

			resource := response[0]
			Expect(resource.Name).To(Equal("test-bmh"))
			Expect(resource.ResourceId).To(Equal("test-ns/test-bmh"))
			Expect(resource.ResourcePoolId).To(Equal("pool123"))
			Expect(resource.HwProfile).To(Equal("profile123"))
		})

		It("should handle BMH without corresponding AllocatedNode", func() {
			// Create BMH with required labels and valid state
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				LabelResourcePoolID: "pool123",
				LabelSiteID:         "site123",
			})
			bmh.Status.Provisioning.State = metal3v1alpha1.StateAvailable

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bmh).
				Build()

			result, err := GetResources(ctx, logger, client)
			Expect(err).ToNot(HaveOccurred())

			response, ok := result.(inventory.GetResources200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(response).To(HaveLen(1))

			resource := response[0]
			Expect(resource.Name).To(Equal("test-bmh"))
			Expect(resource.HwProfile).To(Equal("")) // No corresponding node
		})
	})

	Describe("Regex patterns", func() {
		Describe("REPatternInterfaceLabel", func() {
			It("should match interface labels correctly", func() {
				matches := REPatternInterfaceLabel.FindStringSubmatch(LabelPrefixInterfaces + "eth0")
				Expect(matches).To(HaveLen(2))
				Expect(matches[1]).To(Equal("eth0"))
			})

			It("should not match non-interface labels", func() {
				matches := REPatternInterfaceLabel.FindStringSubmatch("other.label/eth0")
				Expect(matches).To(BeNil())
			})
		})

		Describe("REPatternResourceSelectorLabel", func() {
			It("should match resource selector labels", func() {
				pattern := regexp.MustCompile(`^` + LabelPrefixResourceSelector)
				Expect(pattern.MatchString(LabelPrefixResourceSelector + "zone")).To(BeTrue())
				Expect(pattern.MatchString("other.label")).To(BeFalse())
			})
		})

		Describe("REPatternResourceSelectorLabelMatch", func() {
			It("should extract resource selector label suffix", func() {
				matches := REPatternResourceSelectorLabelMatch.FindStringSubmatch(LabelPrefixResourceSelector + "zone")
				Expect(matches).To(HaveLen(2))
				Expect(matches[1]).To(Equal("zone"))
			})
		})
	})

	Describe("Constants", func() {
		It("should have correct label prefixes", func() {
			Expect(LabelPrefixResources).To(Equal("resources.clcm.openshift.io/"))
			Expect(LabelResourcePoolID).To(Equal("resources.clcm.openshift.io/resourcePoolId"))
			Expect(LabelSiteID).To(Equal("resources.clcm.openshift.io/siteId"))
			Expect(LabelPrefixResourceSelector).To(Equal("resourceselector.clcm.openshift.io/"))
			Expect(LabelPrefixInterfaces).To(Equal("interfacelabel.clcm.openshift.io/"))
		})

		It("should have correct annotation prefixes", func() {
			Expect(AnnotationPrefixResourceInfo).To(Equal("resourceinfo.clcm.openshift.io/"))
			Expect(AnnotationResourceInfoDescription).To(Equal("resourceinfo.clcm.openshift.io/description"))
			Expect(AnnotationResourceInfoPartNumber).To(Equal("resourceinfo.clcm.openshift.io/partNumber"))
			Expect(AnnotationResourceInfoGlobalAssetId).To(Equal("resourceinfo.clcm.openshift.io/globalAssetId"))
			Expect(AnnotationsResourceInfoGroups).To(Equal("resourceinfo.clcm.openshift.io/groups"))
		})
	})
})
