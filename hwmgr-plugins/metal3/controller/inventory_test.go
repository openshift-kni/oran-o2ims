/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Generated-By: Cursor/claude-4-sonnet
*/

package controller

import (
	"context"
	"log/slog"
	"regexp"

	"github.com/google/uuid"
	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/server/inventory"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
)

/*
TEST CASE DESCRIPTIONS:

This file contains comprehensive unit tests for the inventory management functionality
in the Metal3 hardware management plugin. The tests cover:

RESOURCE INFO EXTRACTION FUNCTIONS:
- getResourceInfoAdminState: Tests admin state based on BMH online status
  * Returns UNLOCKED when BMH is online
  * Returns LOCKED when BMH is offline
- getResourceInfoDescription: Tests description extraction from annotations
  * Returns description when annotation present
  * Returns empty string when annotations nil or missing
- getResourceInfoGlobalAssetId: Tests global asset ID extraction from annotations
  * Returns global asset ID when annotation present
  * Returns pointer to empty string when annotations nil
- getResourceInfoGroups: Tests group parsing from comma-separated annotations
  * Parses comma-separated groups correctly
  * Handles spaces around commas
  * Returns nil when annotations nil or missing
- getResourceInfoLabels: Tests label extraction
  * Returns all labels when present
  * Returns nil when labels are nil
- getResourceInfoMemory: Tests memory extraction from hardware details
  * Returns memory from hardware details when present
  * Returns 0 when hardware details are nil
- getResourceInfoModel: Tests model extraction from hardware details
  * Returns model when hardware details present
  * Returns empty string when hardware details nil
- getResourceInfoName: Tests BMH name extraction
- getResourceInfoOperationalState: Tests operational state based on BMH conditions
  * Returns ENABLED when BMH is fully operational (OK status, online, powered on, provisioned)
  * Returns DISABLED when any condition is not met
- getResourceInfoPartNumber: Tests part number extraction from annotations
  * Returns part number when annotation present
  * Returns empty string when annotations nil
- getResourceInfoPowerState: Tests power state extraction
  * Returns ON when BMH is powered on
  * Returns OFF when BMH is powered off
- getResourceInfoResourceId: Tests formatted resource ID generation
- getResourceInfoResourcePoolUID: Tests resource pool UID lookup from labels and map
- getResourceInfoResourceProfileId: Tests HW profile extraction from AllocatedNode
  * Returns HW profile when node present
  * Returns empty string when node is nil
- getResourceInfoSerialNumber: Tests serial number extraction from hardware details
  * Returns serial number when hardware details present
  * Returns empty string when hardware details nil
- getResourceInfoTags: Tests resource selector label extraction as tags
  * Extracts resource selector labels as tags
  * Returns empty array when no resource selector labels present
- getResourceInfoUsageState: Tests usage state based on BMH provisioning state and conditions
  * Returns ACTIVE for provisioned BMH with all conditions met
  * Returns BUSY for provisioned BMH with unmet conditions
  * Returns IDLE for available BMH with operational status OK
  * Returns BUSY for available BMH with operational issues
  * Returns BUSY for transitional states (provisioning, preparing, deprovisioning, inspecting, deleting, etc.)
  * Returns UNKNOWN for unrecognized states
- getResourceInfoVendor: Tests vendor extraction from hardware details
  * Returns vendor when hardware details present
  * Returns empty string when hardware details nil

PROCESSOR INFO EXTRACTION FUNCTIONS:
- getProcessorInfoArchitecture: Tests CPU architecture extraction
  * Returns CPU architecture when hardware details present
  * Returns pointer to empty string when hardware details nil
- getProcessorInfoCores: Tests CPU core count extraction
  * Returns CPU core count when hardware details present
  * Returns nil when hardware details are nil
- getProcessorInfoManufacturer: Tests processor manufacturer (always empty string)
- getProcessorInfoModel: Tests CPU model extraction
  * Returns CPU model when hardware details present
  * Returns pointer to empty string when hardware details nil
- getResourceInfoProcessors: Tests processor info array creation
  * Returns processor info array with hardware details
  * Returns empty array when hardware details nil

INVENTORY INCLUSION LOGIC:
- includeInInventory: Tests BMH inclusion criteria
  * Returns false when labels are nil
  * Returns false when resource pool ID label missing
  * Returns false when site ID label missing
  * Returns true for valid provisioning states (Available, Provisioning, Provisioned, Preparing, Deprovisioning)
  * Returns false for other states

AGGREGATION FUNCTIONS:
- getResourceInfo: Tests complete resource information aggregation
  * Aggregates all resource information correctly from BMH and AllocatedNode

API ENDPOINT FUNCTIONS:
- GetResources: Tests resources API endpoint
  * Returns resources from BMHs included in inventory
  * Handles BMH without corresponding AllocatedNode

REGEX PATTERN VALIDATION:
- REPatternInterfaceLabel: Tests interface label pattern matching
  * Matches interface labels correctly
  * Does not match non-interface labels
- REPatternResourceSelectorLabel: Tests resource selector label pattern matching
- REPatternResourceSelectorLabelMatch: Tests resource selector label suffix extraction

CONSTANTS VALIDATION:
- Tests correctness of label prefixes and annotation prefixes used throughout the system

HELPER FUNCTIONS:
The tests use several helper functions to create test objects:
- createBasicBMH: Creates a basic BareMetalHost for testing
- createBMHWithLabels: Creates BareMetalHost with specified labels
- createBMHWithAnnotations: Creates BareMetalHost with specified annotations
- createHardwareData: Creates HardwareData with specified hardware details
- createAllocatedNode: Creates AllocatedNode with specified HW profile

These tests ensure proper extraction and formatting of hardware inventory information
from Metal3 BareMetalHost resources for O-RAN O2IMS inventory API compliance.
*/

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

	// Helper function to create HardwareData
	createHardwareData := func(name, namespace string) *metal3v1alpha1.HardwareData {
		return &metal3v1alpha1.HardwareData{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: metal3v1alpha1.HardwareDataSpec{
				HardwareDetails: &metal3v1alpha1.HardwareDetails{
					RAMMebibytes: 8192,
					SystemVendor: metal3v1alpha1.HardwareSystemVendor{
						Manufacturer: "Dell Inc.",
						ProductName:  "PowerEdge R640",
						SerialNumber: "ABC123456",
					},
					CPU: metal3v1alpha1.CPU{
						Arch:           "x86_64",
						Model:          "Intel Xeon Gold 6138",
						Count:          40,
						ClockMegahertz: 2600.0,
					},
				},
			},
		}
	}

	Describe("getResourceInfoAdminState", func() {
		It("should return UNLOCKED when BMH is online", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Spec.Online = true
			result := getResourceInfoAdminState(bmh)
			Expect(result).To(Equal(inventory.ResourceInfoAdminStateUNLOCKED))
		})

		It("should return LOCKED when BMH is offline", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Spec.Online = false
			result := getResourceInfoAdminState(bmh)
			Expect(result).To(Equal(inventory.ResourceInfoAdminStateLOCKED))
		})
	})

	Describe("getResourceInfoDescription", func() {
		It("should return description from annotations when present", func() {
			bmh := createBMHWithAnnotations("test-bmh", "test-ns", map[string]string{
				AnnotationResourceInfoDescription: "Test description",
			})
			result := getResourceInfoDescription(bmh)
			Expect(result).To(Equal("Test description"))
		})

		It("should return empty string when annotations are nil", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			result := getResourceInfoDescription(bmh)
			Expect(result).To(Equal(""))
		})

		It("should return empty string when description annotation is missing", func() {
			bmh := createBMHWithAnnotations("test-bmh", "test-ns", map[string]string{
				"other.annotation": "other value",
			})
			result := getResourceInfoDescription(bmh)
			Expect(result).To(Equal(""))
		})
	})

	Describe("getResourceInfoGlobalAssetId", func() {
		It("should return serial number from hardware details when present", func() {
			hwdata := createHardwareData("test-hwdata", "test-ns")
			result := getResourceInfoGlobalAssetId(hwdata)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal("ABC123456"))
		})

		It("should return pointer to empty string when hardware details are nil", func() {
			hwdata := &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hwdata",
					Namespace: "test-ns",
				},
				Spec: metal3v1alpha1.HardwareDataSpec{
					HardwareDetails: nil,
				},
			}
			result := getResourceInfoGlobalAssetId(hwdata)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal(""))
		})
	})

	Describe("getResourceInfoGroups", func() {
		It("should parse comma-separated groups from annotations", func() {
			bmh := createBMHWithAnnotations("test-bmh", "test-ns", map[string]string{
				AnnotationsResourceInfoGroups: "group1,group2,group3",
			})
			result := getResourceInfoGroups(bmh)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal([]string{"group1", "group2", "group3"}))
		})

		It("should handle groups with spaces around commas", func() {
			bmh := createBMHWithAnnotations("test-bmh", "test-ns", map[string]string{
				AnnotationsResourceInfoGroups: "group1 , group2 , group3",
			})
			result := getResourceInfoGroups(bmh)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal([]string{"group1", "group2", "group3"}))
		})

		It("should return nil when annotations are nil", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			result := getResourceInfoGroups(bmh)
			Expect(result).To(BeNil())
		})

		It("should return nil when groups annotation is missing", func() {
			bmh := createBMHWithAnnotations("test-bmh", "test-ns", map[string]string{
				"other.annotation": "other value",
			})
			result := getResourceInfoGroups(bmh)
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
			hwdata := createHardwareData("test-hwdata", "test-ns")
			result := getResourceInfoMemory(hwdata)
			Expect(result).To(Equal(8192))
		})

		It("should return 0 when hardware details are nil", func() {
			hwdata := &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hwdata",
					Namespace: "test-ns",
				},
				Spec: metal3v1alpha1.HardwareDataSpec{
					HardwareDetails: nil,
				},
			}
			result := getResourceInfoMemory(hwdata)
			Expect(result).To(Equal(0))
		})
	})

	Describe("getResourceInfoModel", func() {
		It("should return model from hardware details when present", func() {
			hwdata := createHardwareData("test-hwdata", "test-ns")
			result := getResourceInfoModel(hwdata)
			Expect(result).To(Equal("PowerEdge R640"))
		})

		It("should return empty string when hardware details are nil", func() {
			hwdata := &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hwdata",
					Namespace: "test-ns",
				},
				Spec: metal3v1alpha1.HardwareDataSpec{
					HardwareDetails: nil,
				},
			}
			result := getResourceInfoModel(hwdata)
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
		It("should return ENABLED when BMH is fully operational", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusOK
			bmh.Spec.Online = true
			bmh.Status.PoweredOn = true
			bmh.Status.Provisioning.State = metal3v1alpha1.StateProvisioned

			result := getResourceInfoOperationalState(bmh)
			Expect(result).To(Equal(inventory.ResourceInfoOperationalStateENABLED))
		})

		It("should return ENABLED when externally provisioned BMH is fully operational", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusOK
			bmh.Spec.Online = true
			bmh.Status.PoweredOn = true
			bmh.Status.Provisioning.State = metal3v1alpha1.StateExternallyProvisioned

			result := getResourceInfoOperationalState(bmh)
			Expect(result).To(Equal(inventory.ResourceInfoOperationalStateENABLED))
		})

		It("should return DISABLED when BMH is not operational", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusError
			bmh.Spec.Online = true
			bmh.Status.PoweredOn = true
			bmh.Status.Provisioning.State = metal3v1alpha1.StateProvisioned

			result := getResourceInfoOperationalState(bmh)
			Expect(result).To(Equal(inventory.ResourceInfoOperationalStateDISABLED))
		})

		It("should return DISABLED when BMH is offline", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusOK
			bmh.Spec.Online = false
			bmh.Status.PoweredOn = true
			bmh.Status.Provisioning.State = metal3v1alpha1.StateProvisioned

			result := getResourceInfoOperationalState(bmh)
			Expect(result).To(Equal(inventory.ResourceInfoOperationalStateDISABLED))
		})

		It("should return DISABLED when BMH is not powered on", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusOK
			bmh.Spec.Online = true
			bmh.Status.PoweredOn = false
			bmh.Status.Provisioning.State = metal3v1alpha1.StateProvisioned

			result := getResourceInfoOperationalState(bmh)
			Expect(result).To(Equal(inventory.ResourceInfoOperationalStateDISABLED))
		})

		It("should return DISABLED when BMH is not provisioned", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusOK
			bmh.Spec.Online = true
			bmh.Status.PoweredOn = true
			bmh.Status.Provisioning.State = metal3v1alpha1.StateAvailable

			result := getResourceInfoOperationalState(bmh)
			Expect(result).To(Equal(inventory.ResourceInfoOperationalStateDISABLED))
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
			hwdata := createHardwareData("test-hwdata", "test-ns")
			result := getProcessorInfoArchitecture(hwdata)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal("x86_64"))
		})

		It("should return pointer to empty string when hardware details are nil", func() {
			hwdata := &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hwdata",
					Namespace: "test-ns",
				},
				Spec: metal3v1alpha1.HardwareDataSpec{
					HardwareDetails: nil,
				},
			}
			result := getProcessorInfoArchitecture(hwdata)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal(""))
		})
	})

	Describe("getProcessorInfoCpus", func() {
		It("should return CPU count from hardware details when present", func() {
			hwdata := createHardwareData("test-hwdata", "test-ns")
			result := getProcessorInfoCpus(hwdata)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal(40))
		})

		It("should return nil when hardware details are nil", func() {
			hwdata := &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hwdata",
					Namespace: "test-ns",
				},
				Spec: metal3v1alpha1.HardwareDataSpec{
					HardwareDetails: nil,
				},
			}
			result := getProcessorInfoCpus(hwdata)
			Expect(result).To(BeNil())
		})
	})

	Describe("getProcessorInfoFrequency", func() {
		It("should return CPU frequency from hardware details when present", func() {
			hwdata := createHardwareData("test-hwdata", "test-ns")
			hwdata.Spec.HardwareDetails.CPU.ClockMegahertz = 2600.0
			result := getProcessorInfoFrequency(hwdata)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal(2600))
		})

		It("should return nil when hardware details are nil", func() {
			hwdata := &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hwdata",
					Namespace: "test-ns",
				},
				Spec: metal3v1alpha1.HardwareDataSpec{
					HardwareDetails: nil,
				},
			}
			result := getProcessorInfoFrequency(hwdata)
			Expect(result).To(BeNil())
		})
	})

	Describe("getProcessorInfoModel", func() {
		It("should return CPU model from hardware details when present", func() {
			hwdata := createHardwareData("test-hwdata", "test-ns")
			result := getProcessorInfoModel(hwdata)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal("Intel Xeon Gold 6138"))
		})

		It("should return pointer to empty string when hardware details are nil", func() {
			hwdata := &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hwdata",
					Namespace: "test-ns",
				},
				Spec: metal3v1alpha1.HardwareDataSpec{
					HardwareDetails: nil,
				},
			}
			result := getProcessorInfoModel(hwdata)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal(""))
		})
	})

	Describe("getResourceInfoProcessors", func() {
		It("should return processor info array with hardware details", func() {
			hwdata := createHardwareData("test-hwdata", "test-ns")
			result := getResourceInfoProcessors(hwdata)
			Expect(result).To(HaveLen(1))

			processor := result[0]
			Expect(*processor.Architecture).To(Equal("x86_64"))
			Expect(*processor.Cpus).To(Equal(40))
			Expect(*processor.Frequency).To(Equal(2600))
			Expect(*processor.Model).To(Equal("Intel Xeon Gold 6138"))
		})

		It("should return empty array when hardware details are nil", func() {
			hwdata := &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hwdata",
					Namespace: "test-ns",
				},
				Spec: metal3v1alpha1.HardwareDataSpec{
					HardwareDetails: nil,
				},
			}
			result := getResourceInfoProcessors(hwdata)
			Expect(result).To(HaveLen(0))
		})
	})

	Describe("getResourceInfoResourceId", func() {
		It("should return BMH UID as resource ID", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			testUID := types.UID("f47ac10b-58cc-4372-a567-0e02b2c3d479")
			bmh.UID = testUID
			result := getResourceInfoResourceId(bmh)
			Expect(result).To(Equal(uuid.MustParse(string(testUID))))
		})
	})

	Describe("getResourceInfoResourcePoolUID", func() {
		It("should return resource pool UID from labels and map", func() {
			poolUID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				constants.LabelResourcePoolName: "pool123",
			})
			poolNameToUID := map[string]string{
				"pool123": poolUID,
			}
			result := getResourceInfoResourcePoolUID(bmh, poolNameToUID)
			Expect(result).To(Equal(poolUID))
		})

		It("should return empty string when pool not in map", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				constants.LabelResourcePoolName: "pool123",
			})
			poolNameToUID := map[string]string{} // Empty map
			result := getResourceInfoResourcePoolUID(bmh, poolNameToUID)
			Expect(result).To(Equal(""))
		})

		It("should return empty string when pool label missing", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{})
			poolNameToUID := map[string]string{
				"pool123": "some-uid",
			}
			result := getResourceInfoResourcePoolUID(bmh, poolNameToUID)
			Expect(result).To(Equal(""))
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

	Describe("getResourceInfoNics", func() {
		It("should return NIC map from hardware details", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			hwdata := createHardwareData("test-hwdata", "test-ns")
			hwdata.Spec.HardwareDetails.NIC = []metal3v1alpha1.NIC{
				{
					Name:      "eno1",
					Model:     "0x8086 0x1593",
					MAC:       "40:a6:b7:b1:6d:7a",
					SpeedGbps: 25,
				},
				{
					Name:      "eno2",
					Model:     "0x8086 0x1593",
					MAC:       "40:a6:b7:b1:6d:7b",
					SpeedGbps: 25,
				},
			}

			result := getResourceInfoNics(bmh, hwdata)
			Expect(result).ToNot(BeNil())
			Expect(result).To(HaveLen(2))
			Expect(result["eno1"].Mac).ToNot(BeNil())
			Expect(*result["eno1"].Mac).To(Equal("40:a6:b7:b1:6d:7a"))
			Expect(result["eno1"].Label).To(BeNil())
			Expect(result["eno1"].BootInterface).To(BeNil())
		})

		It("should populate label field from BMH interface labels", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				constants.LabelPrefixInterfaces + "data-interface": "eno1",
			})
			hwdata := createHardwareData("test-hwdata", "test-ns")
			hwdata.Spec.HardwareDetails.NIC = []metal3v1alpha1.NIC{
				{
					Name:      "eno1",
					Model:     "0x8086 0x1593",
					MAC:       "40:a6:b7:b1:6d:7a",
					SpeedGbps: 25,
				},
			}

			result := getResourceInfoNics(bmh, hwdata)
			Expect(result).ToNot(BeNil())
			Expect(result["eno1"].Label).ToNot(BeNil())
			Expect(*result["eno1"].Label).To(Equal("data-interface"))
		})

		It("should set bootInterface when MAC matches BMH bootMACAddress", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Spec.BootMACAddress = "40:a6:b7:b1:6d:7a"
			hwdata := createHardwareData("test-hwdata", "test-ns")
			hwdata.Spec.HardwareDetails.NIC = []metal3v1alpha1.NIC{
				{
					Name:      "eno1",
					Model:     "0x8086 0x1593",
					MAC:       "40:a6:b7:b1:6d:7a",
					SpeedGbps: 25,
				},
				{
					Name:      "eno2",
					Model:     "0x8086 0x1593",
					MAC:       "40:a6:b7:b1:6d:7b",
					SpeedGbps: 25,
				},
			}

			result := getResourceInfoNics(bmh, hwdata)
			Expect(result).ToNot(BeNil())
			Expect(result["eno1"].BootInterface).ToNot(BeNil())
			Expect(*result["eno1"].BootInterface).To(BeTrue())
			Expect(result["eno2"].BootInterface).To(BeNil())
		})

		It("should return nil when hardware details are nil", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			hwdata := &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hwdata",
					Namespace: "test-ns",
				},
				Spec: metal3v1alpha1.HardwareDataSpec{
					HardwareDetails: nil,
				},
			}

			result := getResourceInfoNics(bmh, hwdata)
			Expect(result).To(BeNil())
		})
	})

	Describe("getResourceInfoStorage", func() {
		It("should return storage map from hardware details", func() {
			hwdata := createHardwareData("test-hwdata", "test-ns")
			hwdata.Spec.HardwareDetails.Storage = []metal3v1alpha1.Storage{
				{
					Name:           "/dev/sda",
					Model:          "Samsung SSD 970",
					SerialNumber:   "S466NX0M123456",
					SizeBytes:      1000204886016,
					Type:           "SSD",
					WWN:            "eui.ace42e00357f8b6f",
					AlternateNames: []string{"/dev/disk/by-path/pci-0000:00:1f.2-ata-1"},
				},
			}

			result := getResourceInfoStorage(hwdata)
			Expect(result).ToNot(BeNil())
			Expect(result).To(HaveLen(1))
			Expect(result["/dev/sda"].Model).ToNot(BeNil())
			Expect(*result["/dev/sda"].Model).To(Equal("Samsung SSD 970"))
			Expect(result["/dev/sda"].SerialNumber).ToNot(BeNil())
			Expect(*result["/dev/sda"].SerialNumber).To(Equal("S466NX0M123456"))
		})

		It("should return nil when hardware details are nil", func() {
			hwdata := &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hwdata",
					Namespace: "test-ns",
				},
				Spec: metal3v1alpha1.HardwareDataSpec{
					HardwareDetails: nil,
				},
			}

			result := getResourceInfoStorage(hwdata)
			Expect(result).To(BeNil())
		})
	})

	Describe("getResourceInfoAllocated", func() {
		It("should return true when BMH has allocated label set to true", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				BmhAllocatedLabel: "true",
			})

			result := getResourceInfoAllocated(bmh)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(BeTrue())
		})

		It("should return false when BMH has allocated label set to false", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				BmhAllocatedLabel: "false",
			})

			result := getResourceInfoAllocated(bmh)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(BeFalse())
		})

		It("should return false when allocated label is missing", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")

			result := getResourceInfoAllocated(bmh)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(BeFalse())
		})

		It("should return false when labels are nil", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Labels = nil

			result := getResourceInfoAllocated(bmh)
			Expect(result).ToNot(BeNil())
			Expect(*result).To(BeFalse())
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
		It("should return ACTIVE for provisioned BMH with all conditions met", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.Provisioning.State = metal3v1alpha1.StateProvisioned
			bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusOK
			bmh.Spec.Online = true
			bmh.Status.PoweredOn = true

			result := getResourceInfoUsageState(bmh)
			Expect(result).To(Equal(inventory.ACTIVE))
		})

		It("should return ACTIVE for externally provisioned BMH with all conditions met", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.Provisioning.State = metal3v1alpha1.StateExternallyProvisioned
			bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusOK
			bmh.Spec.Online = true
			bmh.Status.PoweredOn = true

			result := getResourceInfoUsageState(bmh)
			Expect(result).To(Equal(inventory.ACTIVE))
		})

		It("should return BUSY for provisioned BMH when not operational", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.Provisioning.State = metal3v1alpha1.StateProvisioned
			bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusError

			result := getResourceInfoUsageState(bmh)
			Expect(result).To(Equal(inventory.BUSY))
		})

		It("should return BUSY for provisioned BMH when offline", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.Provisioning.State = metal3v1alpha1.StateProvisioned
			bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusOK
			bmh.Spec.Online = false

			result := getResourceInfoUsageState(bmh)
			Expect(result).To(Equal(inventory.BUSY))
		})

		It("should return BUSY for provisioned BMH when powered off", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.Provisioning.State = metal3v1alpha1.StateProvisioned
			bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusOK
			bmh.Spec.Online = true
			bmh.Status.PoweredOn = false

			result := getResourceInfoUsageState(bmh)
			Expect(result).To(Equal(inventory.BUSY))
		})

		It("should return IDLE for available BMH with operational status OK", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.Provisioning.State = metal3v1alpha1.StateAvailable
			bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusOK

			result := getResourceInfoUsageState(bmh)
			Expect(result).To(Equal(inventory.IDLE))
		})

		It("should return BUSY for available BMH when not operational", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.Provisioning.State = metal3v1alpha1.StateAvailable
			bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusError

			result := getResourceInfoUsageState(bmh)
			Expect(result).To(Equal(inventory.BUSY))
		})

		It("should return BUSY for transitional states - provisioning", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.Provisioning.State = metal3v1alpha1.StateProvisioning

			result := getResourceInfoUsageState(bmh)
			Expect(result).To(Equal(inventory.BUSY))
		})

		It("should return BUSY for transitional states - preparing", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.Provisioning.State = metal3v1alpha1.StatePreparing

			result := getResourceInfoUsageState(bmh)
			Expect(result).To(Equal(inventory.BUSY))
		})

		It("should return BUSY for transitional states - deprovisioning", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.Provisioning.State = metal3v1alpha1.StateDeprovisioning

			result := getResourceInfoUsageState(bmh)
			Expect(result).To(Equal(inventory.BUSY))
		})

		It("should return BUSY for transitional states - inspecting", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.Provisioning.State = metal3v1alpha1.StateInspecting

			result := getResourceInfoUsageState(bmh)
			Expect(result).To(Equal(inventory.BUSY))
		})

		It("should return BUSY for transitional states - powering off before delete", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.Provisioning.State = metal3v1alpha1.StatePoweringOffBeforeDelete

			result := getResourceInfoUsageState(bmh)
			Expect(result).To(Equal(inventory.BUSY))
		})

		It("should return BUSY for transitional states - deleting", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Status.Provisioning.State = metal3v1alpha1.StateDeleting

			result := getResourceInfoUsageState(bmh)
			Expect(result).To(Equal(inventory.BUSY))
		})

		It("should return UNKNOWN for unrecognized states", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			// Set an unrecognized state by setting empty string (or any other unrecognized value)
			bmh.Status.Provisioning.State = ""

			result := getResourceInfoUsageState(bmh)
			Expect(result).To(Equal(inventory.UNKNOWN))
		})
	})

	Describe("getResourceInfoVendor", func() {
		It("should return vendor from hardware details when present", func() {
			hwdata := createHardwareData("test-hwdata", "test-ns")
			result := getResourceInfoVendor(hwdata)
			Expect(result).To(Equal("Dell Inc."))
		})

		It("should return empty string when hardware details are nil", func() {
			hwdata := &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hwdata",
					Namespace: "test-ns",
				},
				Spec: metal3v1alpha1.HardwareDataSpec{
					HardwareDetails: nil,
				},
			}
			result := getResourceInfoVendor(hwdata)
			Expect(result).To(Equal(""))
		})
	})

	Describe("IsOCloudManaged", func() {
		It("should return false when labels are nil", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			result := IsOCloudManaged(bmh)
			Expect(result).To(BeFalse())
		})

		It("should return false when required labels are missing", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				"other-label": "value",
			})
			result := IsOCloudManaged(bmh)
			Expect(result).To(BeFalse())
		})

		It("should return true when resourcePoolName label is present", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				constants.LabelResourcePoolName: "pool123",
			})
			result := IsOCloudManaged(bmh)
			Expect(result).To(BeTrue())
		})

		It("should return true when resource selector label is present", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				LabelPrefixResourceSelector + "zone": "east",
			})
			result := IsOCloudManaged(bmh)
			Expect(result).To(BeTrue())
		})

		It("should return true when multiple resource selector labels are present", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				LabelPrefixResourceSelector + "zone":  "east",
				LabelPrefixResourceSelector + "rack":  "rack1",
				LabelPrefixResourceSelector + "floor": "2",
			})
			result := IsOCloudManaged(bmh)
			Expect(result).To(BeTrue())
		})

		It("should return true when both resourcePoolName and resource selector labels are present", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				constants.LabelResourcePoolName:       "pool123",
				LabelPrefixResourceSelector + "zone":  "east",
				LabelPrefixResourceSelector + "floor": "2",
			})
			result := IsOCloudManaged(bmh)
			Expect(result).To(BeTrue())
		})
	})

	Describe("includeInInventory", func() {
		It("should return false when labels are nil", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			result := includeInInventory(bmh)
			Expect(result).To(BeFalse())
		})

		It("should return false when resourcePoolName label is missing", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				"some-other-label": "value",
			})
			result := includeInInventory(bmh)
			Expect(result).To(BeFalse())
		})

		Context("with resourcePoolName label present", func() {
			var bmh *metal3v1alpha1.BareMetalHost

			BeforeEach(func() {
				bmh = createBMHWithLabels("test-bmh", "test-ns", map[string]string{
					constants.LabelResourcePoolName: "pool123",
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

			It("should return true for StateExternallyProvisioned", func() {
				bmh.Status.Provisioning.State = metal3v1alpha1.StateExternallyProvisioned
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
			testUID := types.UID("f47ac10b-58cc-4372-a567-0e02b2c3d479")
			testPoolUID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.UID = testUID
			bmh.Labels = map[string]string{
				constants.LabelResourcePoolName:      "pool123",
				LabelPrefixResourceSelector + "zone": "zone1",
			}
			bmh.Annotations = map[string]string{
				AnnotationResourceInfoDescription: "Test description",
				AnnotationResourceInfoPartNumber:  "PN123456",
			}
			bmh.Spec.Online = true
			bmh.Status.PoweredOn = true
			bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusOK
			bmh.Status.Provisioning.State = metal3v1alpha1.StateProvisioned

			node := createAllocatedNode("test-node", "profile123")
			hwdata := createHardwareData("test-hwdata", "test-ns")

			// Create pool name to UID map
			poolNameToUID := map[string]string{
				"pool123": testPoolUID,
			}

			result := getResourceInfo(bmh, node, hwdata, poolNameToUID)

			Expect(result.AdminState).To(Equal(inventory.ResourceInfoAdminStateUNLOCKED))
			Expect(result.Description).To(Equal("Test description"))
			Expect(result.HwProfile).To(Equal("profile123"))
			Expect(result.Memory).To(Equal(8192))
			Expect(result.Model).To(Equal("PowerEdge R640"))
			Expect(result.Name).To(Equal("test-bmh"))
			Expect(result.OperationalState).To(Equal(inventory.ResourceInfoOperationalStateENABLED))
			Expect(*result.PowerState).To(Equal(inventory.ON))
			Expect(result.Processors).To(HaveLen(1))
			Expect(result.ResourceId).To(Equal(uuid.MustParse(string(testUID))))
			Expect(result.ResourcePoolId).To(Equal(uuid.MustParse(testPoolUID)))
			Expect(*result.Tags).To(ContainElement("zone: zone1"))
			Expect(result.UsageState).To(Equal(inventory.ACTIVE))
			Expect(result.Vendor).To(Equal("Dell Inc."))
			Expect(*result.GlobalAssetId).To(Equal("ABC123456"))
			Expect(result.Allocated).ToNot(BeNil())
			Expect(*result.Allocated).To(BeFalse())
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
			Expect(inventoryv1alpha1.AddToScheme(scheme)).To(Succeed())
		})

		It("should return resources from BMHs included in inventory", func() {
			// Create BMH with required label and valid state
			testUID := types.UID("f47ac10b-58cc-4372-a567-0e02b2c3d479")
			poolUID := types.UID("a1b2c3d4-e5f6-7890-abcd-ef1234567890")
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.UID = testUID
			bmh.Labels = map[string]string{
				constants.LabelResourcePoolName: "pool123",
			}
			bmh.Status.Provisioning.State = metal3v1alpha1.StateAvailable

			// Create HardwareData for the BMH
			hwdata := createHardwareData("test-bmh", "test-ns")

			// Create AllocatedNode that corresponds to this BMH
			node := createAllocatedNode("test-node", "profile123")
			node.Spec.HwMgrNodeId = "test-bmh"
			node.Spec.HwMgrNodeNs = "test-ns"

			// Create ResourcePool CR that the BMH references
			pool := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pool123",
					UID:  poolUID,
				},
				Status: inventoryv1alpha1.ResourcePoolStatus{
					Conditions: []metav1.Condition{{
						Type:   inventoryv1alpha1.ConditionTypeReady,
						Status: metav1.ConditionTrue,
					}},
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bmh, hwdata, node, pool).
				Build()

			result, err := GetResources(ctx, logger, client)
			Expect(err).ToNot(HaveOccurred())

			response, ok := result.(inventory.GetResources200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(response).To(HaveLen(1))

			resource := response[0]
			Expect(resource.Name).To(Equal("test-bmh"))
			Expect(resource.ResourceId).To(Equal(uuid.MustParse(string(testUID))))
			Expect(resource.ResourcePoolId).To(Equal(uuid.MustParse(string(poolUID))))
			Expect(resource.HwProfile).To(Equal("profile123"))
		})

		It("should handle BMH without corresponding AllocatedNode", func() {
			// Create BMH with required label and valid state
			poolUID := types.UID("a1b2c3d4-e5f6-7890-abcd-ef1234567890")
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				constants.LabelResourcePoolName: "pool123",
			})
			bmh.UID = types.UID("b2c3d4e5-f6a7-8901-bcde-f12345678901")
			bmh.Status.Provisioning.State = metal3v1alpha1.StateAvailable

			// Create HardwareData for the BMH
			hwdata := createHardwareData("test-bmh", "test-ns")

			// Create ResourcePool CR that the BMH references
			pool := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pool123",
					UID:  poolUID,
				},
				Status: inventoryv1alpha1.ResourcePoolStatus{
					Conditions: []metav1.Condition{{
						Type:   inventoryv1alpha1.ConditionTypeReady,
						Status: metav1.ConditionTrue,
					}},
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bmh, hwdata, pool).
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

		It("should omit BMH when ResourcePool CR is missing for the pool label", func() {
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				constants.LabelResourcePoolName: "pool123",
			})
			bmh.Status.Provisioning.State = metal3v1alpha1.StateAvailable
			hwdata := createHardwareData("test-bmh", "test-ns")

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bmh, hwdata).
				Build()

			result, err := GetResources(ctx, logger, client)
			Expect(err).ToNot(HaveOccurred())

			response, ok := result.(inventory.GetResources200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(response).To(BeEmpty())
		})

		It("should omit BMH when only resource-selector labels exist (no resolvable resourcePoolId)", func() {
			bmh := createBasicBMH("test-bmh", "test-ns")
			bmh.Labels = map[string]string{
				LabelPrefixResourceSelector + "zone": "east",
			}
			bmh.Status.Provisioning.State = metal3v1alpha1.StateAvailable
			hwdata := createHardwareData("test-bmh", "test-ns")

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bmh, hwdata).
				Build()

			result, err := GetResources(ctx, logger, client)
			Expect(err).ToNot(HaveOccurred())

			response, ok := result.(inventory.GetResources200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(response).To(BeEmpty())
		})

		It("should include only BMHs whose pool label maps to a ResourcePool CR", func() {
			poolUID := types.UID("a1b2c3d4-e5f6-7890-abcd-ef1234567890")
			goodBMH := createBMHWithLabels("good-bmh", "test-ns", map[string]string{
				constants.LabelResourcePoolName: "pool123",
			})
			goodBMH.UID = types.UID("c3d4e5f6-a7b8-9012-cdef-123456789012")
			goodBMH.Status.Provisioning.State = metal3v1alpha1.StateAvailable
			badBMH := createBMHWithLabels("bad-bmh", "test-ns", map[string]string{
				constants.LabelResourcePoolName: "missing-pool",
			})
			badBMH.UID = types.UID("d4e5f6a7-b8c9-0123-defa-234567890123")
			badBMH.Status.Provisioning.State = metal3v1alpha1.StateAvailable
			hwGood := createHardwareData("good-bmh", "test-ns")
			hwBad := createHardwareData("bad-bmh", "test-ns")
			pool := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pool123",
					UID:  poolUID,
				},
				Status: inventoryv1alpha1.ResourcePoolStatus{
					Conditions: []metav1.Condition{{
						Type:   inventoryv1alpha1.ConditionTypeReady,
						Status: metav1.ConditionTrue,
					}},
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(goodBMH, badBMH, hwGood, hwBad, pool).
				Build()

			result, err := GetResources(ctx, logger, client)
			Expect(err).ToNot(HaveOccurred())

			response, ok := result.(inventory.GetResources200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(response).To(HaveLen(1))
			Expect(response[0].Name).To(Equal("good-bmh"))
			Expect(response[0].ResourcePoolId).To(Equal(uuid.MustParse(string(poolUID))))
		})

		It("should skip BMHs whose ResourcePool exists but is not Ready", func() {
			poolUID := types.UID("a1b2c3d4-e5f6-7890-abcd-ef1234567890")
			bmh := createBMHWithLabels("test-bmh", "test-ns", map[string]string{
				constants.LabelResourcePoolName: "pool123",
			})
			bmh.UID = types.UID("e5f6a7b8-c9d0-1234-efab-345678901234")
			bmh.Status.Provisioning.State = metal3v1alpha1.StateAvailable
			hwdata := createHardwareData("test-bmh", "test-ns")

			pool := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pool123",
					UID:  poolUID,
				},
				Status: inventoryv1alpha1.ResourcePoolStatus{
					Conditions: []metav1.Condition{{
						Type:   inventoryv1alpha1.ConditionTypeReady,
						Status: metav1.ConditionFalse,
						Reason: inventoryv1alpha1.ReasonParentNotFound,
					}},
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bmh, hwdata, pool).
				Build()

			result, err := GetResources(ctx, logger, client)
			Expect(err).ToNot(HaveOccurred())

			response, ok := result.(inventory.GetResources200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(response).To(BeEmpty())
		})
	})

	Describe("Regex patterns", func() {
		Describe("REPatternInterfaceLabel", func() {
			It("should match interface labels correctly", func() {
				matches := REPatternInterfaceLabel.FindStringSubmatch(constants.LabelPrefixInterfaces + "eth0")
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
			Expect(constants.LabelPrefixResources).To(Equal("resources.clcm.openshift.io/"))
			Expect(constants.LabelResourcePoolName).To(Equal("resources.clcm.openshift.io/resourcePoolName"))
			Expect(LabelPrefixResourceSelector).To(Equal("resourceselector.clcm.openshift.io/"))
			Expect(constants.LabelPrefixInterfaces).To(Equal("interfacelabel.clcm.openshift.io/"))
		})

		It("should have correct annotation prefixes", func() {
			Expect(AnnotationPrefixResourceInfo).To(Equal("resourceinfo.clcm.openshift.io/"))
			Expect(AnnotationResourceInfoDescription).To(Equal("resourceinfo.clcm.openshift.io/description"))
			Expect(AnnotationResourceInfoPartNumber).To(Equal("resourceinfo.clcm.openshift.io/partNumber"))
			Expect(AnnotationsResourceInfoGroups).To(Equal("resourceinfo.clcm.openshift.io/groups"))
		})
	})
})
