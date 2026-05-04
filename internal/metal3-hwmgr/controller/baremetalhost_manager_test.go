/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Generated-By: Cursor/claude-4-sonnet
*/

/*
Package controller provides unit tests for BareMetalHost (BMH) management functionality
in the Metal3 hardware plugin controller.

This test file contains comprehensive test coverage for the following areas:

BMH Status and Allocation Management:
- Testing BMH allocation status constants and helper functions
- Validating BMH allocation state checking and filtering
- Testing BMH allocation marking and deallocation workflows

BMH Grouping and Organization:
- Testing BMH grouping by resource pools
- Validating BMH filtering by availability status
- Testing BMH list fetching with various filter criteria

BMH Network and Interface Management:
- Testing interface building from BMH hardware details
- Validating network data clearing and configuration
- Testing boot interface identification and labeling

BMH Metadata and Annotation Management:
- Testing label and annotation operations (add/remove)
- Validating BMH metadata updates with retry logic
- Testing infrastructure environment label management

BMH Lifecycle Operations:
- Testing BMH host management permission settings
- Validating BMH finalization and cleanup procedures
- Testing BMH reboot annotation management

Node and Hardware Integration:
- Testing AllocatedNode to BMH relationships
- Validating node configuration progress tracking
- Testing node grouping and counting operations

Supporting Infrastructure:
- Testing PreprovisioningImage label management
- Validating BMC information handling
- Testing error handling and edge cases

The tests use Ginkgo/Gomega testing framework with fake Kubernetes clients
to simulate controller operations without requiring actual cluster resources.
*/

package controller

import (
	"context"
	"log/slog"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
)

const (
	nonexistentBMHID        = "nonexistent-bmh"
	nonexistentBMHNamespace = "nonexistent-namespace"
	testBootMAC             = "00:11:22:33:44:55"
)

// Helper functions
// nolint:unparam
func createBMH(name, namespace string, labels, annotations map[string]string, state metal3v1alpha1.ProvisioningState) *metal3v1alpha1.BareMetalHost {
	bmh := &metal3v1alpha1.BareMetalHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Status: metal3v1alpha1.BareMetalHostStatus{
			Provisioning: metal3v1alpha1.ProvisionStatus{
				State: state,
			},
			HardwareDetails: &metal3v1alpha1.HardwareDetails{
				NIC: []metal3v1alpha1.NIC{
					{
						Name: "eth0",
						MAC:  testBootMAC,
					},
					{
						Name: "eth1",
						MAC:  "00:11:22:33:44:56",
					},
				},
			},
		},
	}
	return bmh
}

// nolint:unparam
func createNodeAllocationRequest(name, namespace string) *pluginsv1alpha1.NodeAllocationRequest {
	return &pluginsv1alpha1.NodeAllocationRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: pluginsv1alpha1.NodeAllocationRequestSpec{},
	}
}

func createAllocatedNode(name, namespace, hwMgrNodeId, hwMgrNodeNs string) *pluginsv1alpha1.AllocatedNode {
	return &pluginsv1alpha1.AllocatedNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: pluginsv1alpha1.AllocatedNodeSpec{
			HwMgrNodeId: hwMgrNodeId,
			HwMgrNodeNs: hwMgrNodeNs,
		},
		Status: pluginsv1alpha1.AllocatedNodeStatus{
			Conditions: []metav1.Condition{},
		},
	}
}

// nolint:unparam
func createAllocatedNodeWithGroup(name, namespace, hwMgrNodeId, hwMgrNodeNs, groupName, hwProfile string) *pluginsv1alpha1.AllocatedNode {
	return &pluginsv1alpha1.AllocatedNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: pluginsv1alpha1.AllocatedNodeSpec{
			HwMgrNodeId: hwMgrNodeId,
			HwMgrNodeNs: hwMgrNodeNs,
			GroupName:   groupName,
			HwProfile:   hwProfile,
		},
		Status: pluginsv1alpha1.AllocatedNodeStatus{
			Conditions: []metav1.Condition{},
		},
	}
}

var _ = Describe("BareMetalHost Manager", func() {
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
		Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
	})

	Describe("isBMHAllocated", func() {
		It("should return true when BMH has allocated label set to true", func() {
			bmh := createBMH("test-bmh", "test-ns", map[string]string{
				BmhAllocatedLabel: ValueTrue,
			}, nil, metal3v1alpha1.StateAvailable)

			result := isBMHAllocated(bmh)
			Expect(result).To(BeTrue())
		})

		It("should return false when BMH has allocated label set to false", func() {
			bmh := createBMH("test-bmh", "test-ns", map[string]string{
				BmhAllocatedLabel: "false",
			}, nil, metal3v1alpha1.StateAvailable)

			result := isBMHAllocated(bmh)
			Expect(result).To(BeFalse())
		})

		It("should return false when BMH has no allocated label", func() {
			bmh := createBMH("test-bmh", "test-ns", nil, nil, metal3v1alpha1.StateAvailable)

			result := isBMHAllocated(bmh)
			Expect(result).To(BeFalse())
		})
	})

	Describe("GroupBMHsByResourcePool", func() {
		It("should group BMHs by resource pool ID", func() {
			bmhList := metal3v1alpha1.BareMetalHostList{
				Items: []metal3v1alpha1.BareMetalHost{
					*createBMH("bmh1", "test-ns", map[string]string{
						constants.LabelResourcePoolName: "pool1",
					}, nil, metal3v1alpha1.StateAvailable),
					*createBMH("bmh2", "test-ns", map[string]string{
						constants.LabelResourcePoolName: "pool2",
					}, nil, metal3v1alpha1.StateAvailable),
					*createBMH("bmh3", "test-ns", map[string]string{
						constants.LabelResourcePoolName: "pool1",
					}, nil, metal3v1alpha1.StateAvailable),
				},
			}

			grouped := GroupBMHsByResourcePool(bmhList)
			Expect(len(grouped)).To(Equal(2))
			Expect(len(grouped["pool1"])).To(Equal(2))
			Expect(len(grouped["pool2"])).To(Equal(1))
			Expect(grouped["pool1"][0].Name).To(Equal("bmh1"))
			Expect(grouped["pool1"][1].Name).To(Equal("bmh3"))
			Expect(grouped["pool2"][0].Name).To(Equal("bmh2"))
		})

		It("should handle BMHs without resource pool label", func() {
			bmhList := metal3v1alpha1.BareMetalHostList{
				Items: []metal3v1alpha1.BareMetalHost{
					*createBMH("bmh1", "test-ns", map[string]string{
						constants.LabelResourcePoolName: "pool1",
					}, nil, metal3v1alpha1.StateAvailable),
					*createBMH("bmh2", "test-ns", nil, nil, metal3v1alpha1.StateAvailable),
				},
			}

			grouped := GroupBMHsByResourcePool(bmhList)
			Expect(len(grouped)).To(Equal(1))
			Expect(len(grouped["pool1"])).To(Equal(1))
		})
	})

	Describe("setBootMACAddressFromLabel", func() {
		var (
			fakeClient client.Client
		)

		It("should set bootMACAddress when found by NIC name", func() {
			bmh := createBMH("test-bmh", "test-ns", map[string]string{
				constants.BootInterfaceLabelFullKey: "eth0",
			}, nil, metal3v1alpha1.StateAvailable)
			// bootMACAddress is not set
			bmh.Spec.BootMACAddress = ""

			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			err := setBootMACAddressFromLabel(ctx, fakeClient, logger, bmh)
			Expect(err).NotTo(HaveOccurred())

			// Verify bootMACAddress was set
			var updatedBMH metal3v1alpha1.BareMetalHost
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBMH.Spec.BootMACAddress).To(Equal(testBootMAC))
		})

		It("should set bootMACAddress when found by hyphenated MAC", func() {
			bmh := createBMH("test-bmh", "test-ns", map[string]string{
				constants.BootInterfaceLabelFullKey: "00-11-22-33-44-56",
			}, nil, metal3v1alpha1.StateAvailable)
			bmh.Spec.BootMACAddress = ""

			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			err := setBootMACAddressFromLabel(ctx, fakeClient, logger, bmh)
			Expect(err).NotTo(HaveOccurred())

			// Verify bootMACAddress was set to eth1's MAC
			var updatedBMH metal3v1alpha1.BareMetalHost
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBMH.Spec.BootMACAddress).To(Equal("00:11:22:33:44:56"))
		})

		It("should set bootMACAddress with case-insensitive hyphenated MAC matching", func() {
			// Label has uppercase MAC, but NIC.MAC in hardware details is lowercase with colons
			bmh := createBMH("test-bmh", "test-ns", map[string]string{
				constants.BootInterfaceLabelFullKey: "00-11-22-33-44-AA",
			}, nil, metal3v1alpha1.StateAvailable)
			bmh.Spec.BootMACAddress = ""

			// Add a NIC with lowercase MAC to test case-insensitive matching
			bmh.Status.HardwareDetails.NIC = append(bmh.Status.HardwareDetails.NIC, metal3v1alpha1.NIC{
				Name: "eth2",
				MAC:  "00:11:22:33:44:aa", // lowercase version of label's MAC
			})

			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			err := setBootMACAddressFromLabel(ctx, fakeClient, logger, bmh)
			Expect(err).NotTo(HaveOccurred())

			// Verify bootMACAddress was set using case-insensitive matching
			var updatedBMH metal3v1alpha1.BareMetalHost
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBMH.Spec.BootMACAddress).To(Equal("00:11:22:33:44:aa"))
		})

		It("should succeed when bootMACAddress is set but boot interface label is not present", func() {
			// BMH has bootMACAddress but no boot interface label
			bmh := createBMH("test-bmh", "test-ns", map[string]string{
				constants.LabelPrefixInterfaces + "mgmt": "eth1",
			}, nil, metal3v1alpha1.StateAvailable)
			bmh.Spec.BootMACAddress = testBootMAC

			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			err := setBootMACAddressFromLabel(ctx, fakeClient, logger, bmh)
			Expect(err).NotTo(HaveOccurred())

			// Verify bootMACAddress was not changed
			var updatedBMH metal3v1alpha1.BareMetalHost
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBMH.Spec.BootMACAddress).To(Equal(testBootMAC))
		})

		It("should validate bootMACAddress matches boot interface label when both are set", func() {
			bmh := createBMH("test-bmh", "test-ns", map[string]string{
				constants.BootInterfaceLabelFullKey: "eth0",
			}, nil, metal3v1alpha1.StateAvailable)
			bmh.Spec.BootMACAddress = testBootMAC

			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			err := setBootMACAddressFromLabel(ctx, fakeClient, logger, bmh)
			Expect(err).NotTo(HaveOccurred())

			// Verify bootMACAddress was not changed
			var updatedBMH metal3v1alpha1.BareMetalHost
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBMH.Spec.BootMACAddress).To(Equal(testBootMAC))
		})

		It("should return error when bootMACAddress doesn't match boot interface label", func() {
			bmh := createBMH("test-bmh", "test-ns", map[string]string{
				constants.BootInterfaceLabelFullKey: "eth0",
			}, nil, metal3v1alpha1.StateAvailable)
			// Set bootMACAddress to eth1's MAC, but label points to eth0
			bmh.Spec.BootMACAddress = "00:11:22:33:44:56"

			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			err := setBootMACAddressFromLabel(ctx, fakeClient, logger, bmh)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("bootMACAddress"))
			Expect(err.Error()).To(ContainSubstring("does not match"))
			Expect(err.Error()).To(ContainSubstring("00:11:22:33:44:56"))
			Expect(err.Error()).To(ContainSubstring(testBootMAC))
		})

		It("should return error when hardware details are nil", func() {
			bmh := createBMH("test-bmh", "test-ns", map[string]string{
				constants.BootInterfaceLabelFullKey: "eth0",
			}, nil, metal3v1alpha1.StateAvailable)
			bmh.Spec.BootMACAddress = ""
			bmh.Status.HardwareDetails = nil

			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			err := setBootMACAddressFromLabel(ctx, fakeClient, logger, bmh)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("bareMetalHost.status.hardwareDetails should not be nil"))
		})

		It("should return error when boot label not found on BMH", func() {
			bmh := createBMH("test-bmh", "test-ns", map[string]string{
				constants.LabelPrefixInterfaces + "mgmt": "eth0",
			}, nil, metal3v1alpha1.StateAvailable)
			bmh.Spec.BootMACAddress = ""

			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			err := setBootMACAddressFromLabel(ctx, fakeClient, logger, bmh)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("boot interface label"))
			Expect(err.Error()).To(ContainSubstring("not found"))
		})

		It("should return error when no NIC matches label value", func() {
			bmh := createBMH("test-bmh", "test-ns", map[string]string{
				constants.BootInterfaceLabelFullKey: "eth99",
			}, nil, metal3v1alpha1.StateAvailable)
			bmh.Spec.BootMACAddress = ""

			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			err := setBootMACAddressFromLabel(ctx, fakeClient, logger, bmh)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no NIC found matching"))
		})
	})

	Describe("buildInterfacesFromBMH", func() {
		It("should build interfaces correctly with boot interface", func() {
			bmh := createBMH("test-bmh", "test-ns", nil, nil, metal3v1alpha1.StateAvailable)
			bmh.Spec.BootMACAddress = testBootMAC

			interfaces, err := buildInterfacesFromBMH(bmh)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(interfaces)).To(Equal(2))

			// Find boot interface
			var bootInterface *pluginsv1alpha1.Interface
			for _, iface := range interfaces {
				if iface.MACAddress == testBootMAC {
					bootInterface = iface
					break
				}
			}
			Expect(bootInterface).NotTo(BeNil())
			Expect(bootInterface.Label).To(Equal(constants.BootInterfaceLabel))
			Expect(bootInterface.Name).To(Equal("eth0"))
		})

		It("should handle interface labels with MAC addresses", func() {
			bmh := createBMH("test-bmh", "test-ns", map[string]string{
				constants.LabelPrefixInterfaces + "mgmt": "00-11-22-33-44-56",
			}, nil, metal3v1alpha1.StateAvailable)

			interfaces, err := buildInterfacesFromBMH(bmh)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(interfaces)).To(Equal(2))

			// Find labeled interface
			var labeledInterface *pluginsv1alpha1.Interface
			for _, iface := range interfaces {
				if iface.MACAddress == "00:11:22:33:44:56" {
					labeledInterface = iface
					break
				}
			}
			Expect(labeledInterface).NotTo(BeNil())
			Expect(labeledInterface.Label).To(Equal("mgmt"))
		})

		It("should return error when hardware details are nil", func() {
			bmh := createBMH("test-bmh", "test-ns", nil, nil, metal3v1alpha1.StateAvailable)
			bmh.Status.HardwareDetails = nil

			_, err := buildInterfacesFromBMH(bmh)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("bareMetalHost.status.hardwareDetails should not be nil"))
		})
	})

	Describe("checkBMHStatus", func() {
		It("should return true when BMH is in desired state", func() {
			bmh := createBMH("test-bmh", "test-ns", nil, nil, metal3v1alpha1.StateAvailable)

			result := checkBMHStatus(ctx, logger, bmh, metal3v1alpha1.StateAvailable)
			Expect(result).To(BeTrue())
		})

		It("should return false when BMH is not in desired state", func() {
			bmh := createBMH("test-bmh", "test-ns", nil, nil, metal3v1alpha1.StateProvisioning)

			result := checkBMHStatus(ctx, logger, bmh, metal3v1alpha1.StateAvailable)
			Expect(result).To(BeFalse())
		})
	})

	Describe("updateBMHMetaWithRetry", func() {
		var (
			fakeClient client.Client
			bmh        *metal3v1alpha1.BareMetalHost
		)

		BeforeEach(func() {
			bmh = createBMH("test-bmh", "test-ns", nil, nil, metal3v1alpha1.StateAvailable)
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()
		})

		It("should add label successfully", func() {
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err := updateBMHMetaWithRetry(ctx, fakeClient, logger, name, MetaTypeLabel, "test-key", "test-value", OpAdd)
			Expect(err).NotTo(HaveOccurred())

			// Verify label was added
			var updatedBMH metal3v1alpha1.BareMetalHost
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBMH.Labels["test-key"]).To(Equal("test-value"))
		})

		It("should add annotation successfully", func() {
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err := updateBMHMetaWithRetry(ctx, fakeClient, logger, name, MetaTypeAnnotation, "test-key", "test-value", OpAdd)
			Expect(err).NotTo(HaveOccurred())

			// Verify annotation was added
			var updatedBMH metal3v1alpha1.BareMetalHost
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBMH.Annotations["test-key"]).To(Equal("test-value"))
		})

		It("should remove label successfully", func() {
			// First add a label
			bmh.Labels = map[string]string{"test-key": "test-value"}
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err := updateBMHMetaWithRetry(ctx, fakeClient, logger, name, MetaTypeLabel, "test-key", "", OpRemove)
			Expect(err).NotTo(HaveOccurred())

			// Verify label was removed
			var updatedBMH metal3v1alpha1.BareMetalHost
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			_, exists := updatedBMH.Labels["test-key"]
			Expect(exists).To(BeFalse())
		})

		It("should handle unsupported meta type", func() {
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err := updateBMHMetaWithRetry(ctx, fakeClient, logger, name, "invalid", "test-key", "test-value", OpAdd)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported meta type"))
		})

		It("should handle unsupported operation", func() {
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err := updateBMHMetaWithRetry(ctx, fakeClient, logger, name, MetaTypeLabel, "test-key", "test-value", "invalid")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported operation"))
		})

		It("should skip remove operation when map is nil", func() {
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err := updateBMHMetaWithRetry(ctx, fakeClient, logger, name, MetaTypeLabel, "test-key", "", OpRemove)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should skip remove operation when key doesn't exist", func() {
			bmh.Labels = map[string]string{"other-key": "other-value"}
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err := updateBMHMetaWithRetry(ctx, fakeClient, logger, name, MetaTypeLabel, "test-key", "", OpRemove)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("markBMHAllocated", func() {
		var (
			fakeClient client.Client
			bmh        *metal3v1alpha1.BareMetalHost
		)

		BeforeEach(func() {
			bmh = createBMH("test-bmh", "test-ns", nil, nil, metal3v1alpha1.StateAvailable)
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()
		})

		It("should mark BMH as allocated", func() {
			err := markBMHAllocated(ctx, fakeClient, logger, bmh)
			Expect(err).NotTo(HaveOccurred())

			// Verify BMH was marked as allocated
			var updatedBMH metal3v1alpha1.BareMetalHost
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBMH.Labels[BmhAllocatedLabel]).To(Equal(ValueTrue))
		})

		It("should skip update when BMH is already allocated", func() {
			bmh.Labels = map[string]string{BmhAllocatedLabel: ValueTrue}
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			err := markBMHAllocated(ctx, fakeClient, logger, bmh)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("allowHostManagement", func() {
		var (
			fakeClient client.Client
			bmh        *metal3v1alpha1.BareMetalHost
		)

		BeforeEach(func() {
			bmh = createBMH("test-bmh", "test-ns", nil, nil, metal3v1alpha1.StateAvailable)
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()
		})

		It("should add host management annotation", func() {
			err := allowHostManagement(ctx, fakeClient, logger, bmh)
			Expect(err).NotTo(HaveOccurred())

			// Verify annotation was added
			var updatedBMH metal3v1alpha1.BareMetalHost
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			_, exists := updatedBMH.Annotations[BmhHostMgmtAnnotation]
			Expect(exists).To(BeTrue())
		})

		It("should skip when annotation already exists with empty value", func() {
			bmh.Annotations = map[string]string{BmhHostMgmtAnnotation: ""}
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			err := allowHostManagement(ctx, fakeClient, logger, bmh)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("getBMHForNode", func() {
		var (
			fakeClient client.Client
			bmh        *metal3v1alpha1.BareMetalHost
			node       *pluginsv1alpha1.AllocatedNode
		)

		BeforeEach(func() {
			bmh = createBMH("test-bmh", "test-ns", nil, nil, metal3v1alpha1.StateAvailable)
			node = createAllocatedNode("test-node", "test-ns", "test-bmh", "test-ns")
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh, node).Build()
		})

		It("should return BMH for node successfully", func() {
			retrievedBMH, err := getBMHForNode(ctx, fakeClient, node)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrievedBMH.Name).To(Equal("test-bmh"))
			Expect(retrievedBMH.Namespace).To(Equal("test-ns"))
		})

		It("should return error when BMH not found", func() {
			node.Spec.HwMgrNodeId = nonexistentBMHID
			_, err := getBMHForNode(ctx, fakeClient, node)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unable to find BMH"))
		})
	})

	Describe("fetchBMHList", func() {
		var (
			fakeClient client.Client
			bmh1, bmh2 *metal3v1alpha1.BareMetalHost
			nodeGroup  hwmgmtv1alpha1.NodeGroupData
		)

		BeforeEach(func() {
			bmh1 = createBMH("bmh1", "test-ns", map[string]string{
				constants.LabelResourcePoolName: "pool1",
				BmhAllocatedLabel:               ValueTrue,
			}, nil, metal3v1alpha1.StateAvailable)

			bmh2 = createBMH("bmh2", "test-ns", map[string]string{
				constants.LabelResourcePoolName: "pool1",
			}, nil, metal3v1alpha1.StateAvailable)

			// Create corresponding HardwareData for each BMH
			hwData1 := &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bmh1",
					Namespace: "test-ns",
				},
				Spec: metal3v1alpha1.HardwareDataSpec{
					HardwareDetails: &metal3v1alpha1.HardwareDetails{
						CPU: metal3v1alpha1.CPU{Arch: "x86_64"},
					},
				},
			}
			hwData2 := &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bmh2",
					Namespace: "test-ns",
				},
				Spec: metal3v1alpha1.HardwareDataSpec{
					HardwareDetails: &metal3v1alpha1.HardwareDetails{
						CPU: metal3v1alpha1.CPU{Arch: "x86_64"},
					},
				},
			}

			nodeGroup = hwmgmtv1alpha1.NodeGroupData{
				ResourcePoolId:   "pool1",
				ResourceSelector: map[string]string{},
			}

			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh1, bmh2, hwData1, hwData2).Build()
		})

		It("should fetch only unallocated BMHs", func() {
			bmhList, err := fetchBMHList(ctx, fakeClient, logger, nodeGroup)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(bmhList.Items)).To(Equal(1))
			Expect(bmhList.Items[0].Name).To(Equal("bmh2"))
		})

		It("should filter by resource pool name", func() {
			bmh3 := createBMH("bmh3", "test-ns", map[string]string{
				constants.LabelResourcePoolName: "pool2",
			}, nil, metal3v1alpha1.StateAvailable)
			hwData3 := &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bmh3",
					Namespace: "test-ns",
				},
				Spec: metal3v1alpha1.HardwareDataSpec{
					HardwareDetails: &metal3v1alpha1.HardwareDetails{
						CPU: metal3v1alpha1.CPU{Arch: "x86_64"},
					},
				},
			}
			// Need to also include the original hardware data objects
			hwData1 := &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bmh1",
					Namespace: "test-ns",
				},
				Spec: metal3v1alpha1.HardwareDataSpec{
					HardwareDetails: &metal3v1alpha1.HardwareDetails{
						CPU: metal3v1alpha1.CPU{Arch: "x86_64"},
					},
				},
			}
			hwData2 := &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bmh2",
					Namespace: "test-ns",
				},
				Spec: metal3v1alpha1.HardwareDataSpec{
					HardwareDetails: &metal3v1alpha1.HardwareDetails{
						CPU: metal3v1alpha1.CPU{Arch: "x86_64"},
					},
				},
			}
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh1, bmh2, bmh3, hwData1, hwData2, hwData3).Build()

			bmhList, err := fetchBMHList(ctx, fakeClient, logger, nodeGroup)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(bmhList.Items)).To(Equal(1)) // Only bmh2 should match (bmh1 is allocated, bmh3 is pool2)
		})
	})

	Describe("finalizeBMHDeallocation", func() {
		var (
			fakeClient client.Client
			bmh        *metal3v1alpha1.BareMetalHost
		)

		BeforeEach(func() {
			bmh = createBMH("test-bmh", "test-ns", map[string]string{
				SiteConfigOwnedByLabel:     "test-cluster",
				BmhAllocatedLabel:          ValueTrue,
				"utils.AllocatedNodeLabel": "test-node",
			}, map[string]string{
				BiosUpdateNeededAnnotation:     ValueTrue,
				FirmwareUpdateNeededAnnotation: ValueTrue,
			}, metal3v1alpha1.StateProvisioned)

			// Set up CustomDeploy and Image
			bmh.Spec.CustomDeploy = &metal3v1alpha1.CustomDeploy{
				Method: "test-method",
			}
			bmh.Spec.Image = &metal3v1alpha1.Image{
				URL: "test-image-url",
			}
			bmh.Spec.Online = true
			bmh.Spec.PreprovisioningNetworkDataName = "old-network-data"

			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()
		})

		It("should deallocate BMH successfully", func() {
			err := finalizeBMHDeallocation(ctx, fakeClient, logger, bmh, false)
			Expect(err).NotTo(HaveOccurred())

			// Verify BMH was deallocated correctly
			var updatedBMH metal3v1alpha1.BareMetalHost
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())

			// Check that allocation labels were removed
			_, hasOwnedBy := updatedBMH.Labels[SiteConfigOwnedByLabel]
			Expect(hasOwnedBy).To(BeFalse())
			_, hasAllocated := updatedBMH.Labels[BmhAllocatedLabel]
			Expect(hasAllocated).To(BeFalse())

			// Check that configuration annotations were removed
			_, hasBiosAnnotation := updatedBMH.Annotations[BiosUpdateNeededAnnotation]
			Expect(hasBiosAnnotation).To(BeFalse())
			_, hasFirmwareAnnotation := updatedBMH.Annotations[FirmwareUpdateNeededAnnotation]
			Expect(hasFirmwareAnnotation).To(BeFalse())

			// Check that spec fields were updated
			Expect(updatedBMH.Spec.Online).To(BeFalse())
			Expect(updatedBMH.Spec.CustomDeploy).To(BeNil())
			Expect(updatedBMH.Spec.Image).To(BeNil())
			// PreprovisioningNetworkDataName is not empty, so it should be preserved
			Expect(updatedBMH.Spec.PreprovisioningNetworkDataName).To(Equal("old-network-data"))
		})

		It("should restore PreprovisioningNetworkDataName from annotation when overwritten", func() {
			// Simulate IBI overwriting the field while the annotation holds the original
			bmh.Spec.PreprovisioningNetworkDataName = "ibi-generated-secret"
			bmh.Annotations[OrigNetworkDataAnnotation] = "original-network-data"
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			err := finalizeBMHDeallocation(ctx, fakeClient, logger, bmh, false)
			Expect(err).NotTo(HaveOccurred())

			var updatedBMH metal3v1alpha1.BareMetalHost
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBMH.Spec.PreprovisioningNetworkDataName).To(Equal("original-network-data"))
			_, hasAnnotation := updatedBMH.Annotations[OrigNetworkDataAnnotation]
			Expect(hasAnnotation).To(BeFalse())
		})

		It("should restore empty PreprovisioningNetworkDataName from annotation", func() {
			// Simulate IBI overwriting a field that was originally empty
			bmh.Spec.PreprovisioningNetworkDataName = "ibi-generated-secret"
			bmh.Annotations[OrigNetworkDataAnnotation] = ""
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			err := finalizeBMHDeallocation(ctx, fakeClient, logger, bmh, false)
			Expect(err).NotTo(HaveOccurred())

			var updatedBMH metal3v1alpha1.BareMetalHost
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBMH.Spec.PreprovisioningNetworkDataName).To(BeEmpty())
		})

		It("should not modify PreprovisioningNetworkDataName when no annotation exists", func() {
			bmh.Spec.PreprovisioningNetworkDataName = "some-network-data"
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			err := finalizeBMHDeallocation(ctx, fakeClient, logger, bmh, false)
			Expect(err).NotTo(HaveOccurred())

			var updatedBMH metal3v1alpha1.BareMetalHost
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBMH.Spec.PreprovisioningNetworkDataName).To(Equal("some-network-data"))
		})

		It("should restore PreprovisioningNetworkDataName from legacy secret when no annotation and field is empty", func() {
			// Upgrade scenario: allocated by older operator that cleared the field
			bmh.Spec.PreprovisioningNetworkDataName = ""
			delete(bmh.Annotations, OrigNetworkDataAnnotation)
			expectedSecretName := BmhNetworkDataPrefx + "-" + bmh.Name
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      expectedSecretName,
					Namespace: bmh.Namespace,
				},
			}
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh, secret).Build()

			err := finalizeBMHDeallocation(ctx, fakeClient, logger, bmh, false)
			Expect(err).NotTo(HaveOccurred())

			var updatedBMH metal3v1alpha1.BareMetalHost
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBMH.Spec.PreprovisioningNetworkDataName).To(Equal(expectedSecretName))
		})

		It("should leave PreprovisioningNetworkDataName empty when no annotation and no legacy secret", func() {
			bmh.Spec.PreprovisioningNetworkDataName = ""
			delete(bmh.Annotations, OrigNetworkDataAnnotation)
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			err := finalizeBMHDeallocation(ctx, fakeClient, logger, bmh, false)
			Expect(err).NotTo(HaveOccurred())

			var updatedBMH metal3v1alpha1.BareMetalHost
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBMH.Spec.PreprovisioningNetworkDataName).To(BeEmpty())
		})

		It("should set automated cleaning mode for provisioned BMH", func() {
			bmh.Status.Provisioning.State = metal3v1alpha1.StateProvisioned
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			err := finalizeBMHDeallocation(ctx, fakeClient, logger, bmh, false)
			Expect(err).NotTo(HaveOccurred())

			var updatedBMH metal3v1alpha1.BareMetalHost
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBMH.Spec.AutomatedCleaningMode).To(Equal(metal3v1alpha1.CleaningModeMetadata))
		})

		It("should set automated cleaning mode for externally provisioned BMH", func() {
			bmh.Status.Provisioning.State = metal3v1alpha1.StateExternallyProvisioned
			bmh.Spec.ExternallyProvisioned = true
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			err := finalizeBMHDeallocation(ctx, fakeClient, logger, bmh, false)
			Expect(err).NotTo(HaveOccurred())

			var updatedBMH metal3v1alpha1.BareMetalHost
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBMH.Spec.AutomatedCleaningMode).To(Equal(metal3v1alpha1.CleaningModeMetadata))
			Expect(updatedBMH.Spec.Online).To(BeFalse())
			Expect(updatedBMH.Annotations[IBIWarningAnnotation]).To(Equal(IBIWarningMessage))
		})

		It("should not set IBI warning annotation for non-IBI BMH", func() {
			bmh.Status.Provisioning.State = metal3v1alpha1.StateProvisioned
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			err := finalizeBMHDeallocation(ctx, fakeClient, logger, bmh, false)
			Expect(err).NotTo(HaveOccurred())

			var updatedBMH metal3v1alpha1.BareMetalHost
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			_, hasIBIWarning := updatedBMH.Annotations[IBIWarningAnnotation]
			Expect(hasIBIWarning).To(BeFalse())
		})

		It("should not set cleaning mode or power off when skipCleanup is true", func() {
			bmh.Status.Provisioning.State = metal3v1alpha1.StateProvisioned
			bmh.Spec.Online = true
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			err := finalizeBMHDeallocation(ctx, fakeClient, logger, bmh, true)
			Expect(err).NotTo(HaveOccurred())

			var updatedBMH metal3v1alpha1.BareMetalHost
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBMH.Spec.AutomatedCleaningMode).To(BeEmpty())
			Expect(updatedBMH.Spec.Online).To(BeTrue())
			// Ensure deploy/image/networkdata were NOT cleared/reset when skipCleanup is true
			Expect(updatedBMH.Spec.CustomDeploy).NotTo(BeNil())
			Expect(updatedBMH.Spec.Image).NotTo(BeNil())
			Expect(updatedBMH.Spec.PreprovisioningNetworkDataName).To(Equal("old-network-data"))
		})

		It("should not set IBI warning when skipCleanup is true for externally provisioned BMH", func() {
			bmh.Status.Provisioning.State = metal3v1alpha1.StateExternallyProvisioned
			bmh.Spec.ExternallyProvisioned = true
			bmh.Spec.Online = true
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()

			err := finalizeBMHDeallocation(ctx, fakeClient, logger, bmh, true)
			Expect(err).NotTo(HaveOccurred())

			var updatedBMH metal3v1alpha1.BareMetalHost
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			// skipCleanup=true should skip cleaning, power-off, and IBI warning
			Expect(updatedBMH.Spec.AutomatedCleaningMode).To(BeEmpty())
			Expect(updatedBMH.Spec.Online).To(BeTrue())
			_, hasIBIWarning := updatedBMH.Annotations[IBIWarningAnnotation]
			Expect(hasIBIWarning).To(BeFalse())
		})
	})

	Describe("removeInfraEnvLabelFromPreprovisioningImage", func() {
		var (
			fakeClient client.Client
			image      *metal3v1alpha1.PreprovisioningImage
		)

		BeforeEach(func() {
			image = &metal3v1alpha1.PreprovisioningImage{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-image",
					Namespace: "test-ns",
					Labels: map[string]string{
						BmhInfraEnvLabel: "test-infraenv",
						"other-label":    "other-value",
					},
				},
			}
			// Add PreprovisioningImage to scheme
			Expect(metal3v1alpha1.AddToScheme(scheme)).To(Succeed())
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(image).Build()
		})

		It("should remove InfraEnv label from PreprovisioningImage", func() {
			name := types.NamespacedName{Name: image.Name, Namespace: image.Namespace}
			err := removeInfraEnvLabelFromPreprovisioningImage(ctx, fakeClient, logger, name)
			Expect(err).NotTo(HaveOccurred())

			// Verify label was removed
			var updatedImage metal3v1alpha1.PreprovisioningImage
			err = fakeClient.Get(ctx, name, &updatedImage)
			Expect(err).NotTo(HaveOccurred())
			_, exists := updatedImage.Labels[BmhInfraEnvLabel]
			Expect(exists).To(BeFalse())
			// Other labels should remain
			Expect(updatedImage.Labels["other-label"]).To(Equal("other-value"))
		})

		It("should remove the AI deprovision finalizer from PreprovisioningImage", func() {
			image.Finalizers = []string{
				PreprovisioningImageDeprovisionFinalizer,
				"other-finalizer",
			}
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(image).Build()

			name := types.NamespacedName{Name: image.Name, Namespace: image.Namespace}
			err := removeInfraEnvLabelFromPreprovisioningImage(ctx, fakeClient, logger, name)
			Expect(err).NotTo(HaveOccurred())

			// Verify finalizer was removed
			var updatedImage metal3v1alpha1.PreprovisioningImage
			err = fakeClient.Get(ctx, name, &updatedImage)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedImage.Finalizers).NotTo(ContainElement(PreprovisioningImageDeprovisionFinalizer))
			// Other finalizers should remain
			Expect(updatedImage.Finalizers).To(ContainElement("other-finalizer"))
		})

		It("should succeed when the AI deprovision finalizer is not present", func() {
			image.Finalizers = []string{"other-finalizer"}
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(image).Build()

			name := types.NamespacedName{Name: image.Name, Namespace: image.Namespace}
			err := removeInfraEnvLabelFromPreprovisioningImage(ctx, fakeClient, logger, name)
			Expect(err).NotTo(HaveOccurred())

			// Verify other finalizers are untouched
			var updatedImage metal3v1alpha1.PreprovisioningImage
			err = fakeClient.Get(ctx, name, &updatedImage)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedImage.Finalizers).To(ContainElement("other-finalizer"))
		})
	})

	Describe("annotateNodeConfigInProgress", func() {
		var (
			fakeClient client.Client
			node       *pluginsv1alpha1.AllocatedNode
		)

		BeforeEach(func() {
			node = createAllocatedNode("test-node", "test-ns", "test-bmh", "test-ns")
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()
		})

		It("should annotate node with config in progress", func() {
			err := annotateNodeConfigInProgress(ctx, fakeClient, logger, "test-ns", "test-node", UpdateReasonBIOSSettings)
			Expect(err).NotTo(HaveOccurred())

			// Verify annotation was added
			var updatedNode pluginsv1alpha1.AllocatedNode
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-node", Namespace: "test-ns"}, &updatedNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedNode.Annotations[ConfigAnnotation]).To(Equal(UpdateReasonBIOSSettings))
		})

		It("should handle node with existing annotations", func() {
			node.Annotations = map[string]string{"existing": "value"}
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()

			err := annotateNodeConfigInProgress(ctx, fakeClient, logger, "test-ns", "test-node", UpdateReasonFirmware)
			Expect(err).NotTo(HaveOccurred())

			// Verify both annotations exist
			var updatedNode pluginsv1alpha1.AllocatedNode
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-node", Namespace: "test-ns"}, &updatedNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedNode.Annotations[ConfigAnnotation]).To(Equal(UpdateReasonFirmware))
			Expect(updatedNode.Annotations["existing"]).To(Equal("value"))
		})
	})

	Describe("addRebootAnnotation", func() {
		var (
			fakeClient client.Client
			bmh        *metal3v1alpha1.BareMetalHost
		)

		BeforeEach(func() {
			bmh = createBMH("test-bmh", "test-ns", nil, nil, metal3v1alpha1.StateAvailable)
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh).Build()
		})

		It("should add reboot annotation to BMH", func() {
			err := addRebootAnnotation(ctx, fakeClient, logger, bmh)
			Expect(err).NotTo(HaveOccurred())

			// Verify annotation was added
			var updatedBMH metal3v1alpha1.BareMetalHost
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			_, exists := updatedBMH.Annotations[BmhRebootAnnotation]
			Expect(exists).To(BeTrue())
		})
	})

	Describe("removeInfraEnvLabel", func() {
		var (
			fakeClient client.Client
			bmh        *metal3v1alpha1.BareMetalHost
			image      *metal3v1alpha1.PreprovisioningImage
		)

		BeforeEach(func() {
			bmh = createBMH("test-bmh", "test-ns", map[string]string{
				BmhInfraEnvLabel: "test-infraenv",
			}, nil, metal3v1alpha1.StateAvailable)

			image = &metal3v1alpha1.PreprovisioningImage{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-bmh",
					Namespace: "test-ns",
					Labels: map[string]string{
						BmhInfraEnvLabel: "test-infraenv",
					},
				},
			}

			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh, image).Build()
		})

		It("should remove InfraEnv label from both BMH and PreprovisioningImage", func() {
			name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
			err := removeInfraEnvLabel(ctx, fakeClient, logger, name)
			Expect(err).NotTo(HaveOccurred())

			// Verify label was removed from BMH
			var updatedBMH metal3v1alpha1.BareMetalHost
			err = fakeClient.Get(ctx, name, &updatedBMH)
			Expect(err).NotTo(HaveOccurred())
			_, exists := updatedBMH.Labels[BmhInfraEnvLabel]
			Expect(exists).To(BeFalse())

			// Verify label was removed from PreprovisioningImage
			var updatedImage metal3v1alpha1.PreprovisioningImage
			err = fakeClient.Get(ctx, name, &updatedImage)
			Expect(err).NotTo(HaveOccurred())
			_, exists = updatedImage.Labels[BmhInfraEnvLabel]
			Expect(exists).To(BeFalse())
		})
	})

	Describe("Constants and types", func() {
		It("should have correct constant values", func() {
			Expect(BmhRebootAnnotation).To(Equal("reboot.metal3.io"))
			Expect(BmhNetworkDataPrefx).To(Equal("network-data"))
			Expect(BiosUpdateNeededAnnotation).To(Equal("clcm.openshift.io/bios-update-needed"))
			Expect(FirmwareUpdateNeededAnnotation).To(Equal("clcm.openshift.io/firmware-update-needed"))
			Expect(BmhAllocatedLabel).To(Equal("clcm.openshift.io/allocated"))
			Expect(BmhHostMgmtAnnotation).To(Equal("bmac.agent-install.openshift.io/allow-provisioned-host-management"))
			Expect(BmhInfraEnvLabel).To(Equal("infraenvs.agent-install.openshift.io"))
			Expect(PreprovisioningImageDeprovisionFinalizer).To(Equal("preprovisioningimage.agent-install.openshift.io/ai-deprovision"))
			Expect(SiteConfigOwnedByLabel).To(Equal("siteconfig.open-cluster-management.io/owned-by"))
			Expect(UpdateReasonBIOSSettings).To(Equal("bios-settings-update"))
			Expect(UpdateReasonFirmware).To(Equal("firmware-update"))
			Expect(ValueTrue).To(Equal("true"))
			Expect(MetaTypeLabel).To(Equal("label"))
			Expect(MetaTypeAnnotation).To(Equal("annotation"))
			Expect(OpAdd).To(Equal("add"))
			Expect(OpRemove).To(Equal("remove"))
			Expect(BmhServicingErr).To(Equal("BMH Servicing Error"))
		})
	})

	Describe("isNodeProvisioningInProgress", func() {
		It("should return true when node has provisioned condition with InProgress reason", func() {
			node := createNodeWithCondition("test-node", "test-ns", string(hwmgmtv1alpha1.Provisioned), string(hwmgmtv1alpha1.InProgress), metav1.ConditionFalse)

			result := isNodeProvisioningInProgress(node)
			Expect(result).To(BeTrue())
		})

		It("should return false when node has provisioned condition with Completed reason", func() {
			node := createNodeWithCondition("test-node", "test-ns", string(hwmgmtv1alpha1.Provisioned), string(hwmgmtv1alpha1.Completed), metav1.ConditionTrue)

			result := isNodeProvisioningInProgress(node)
			Expect(result).To(BeFalse())
		})

		It("should return false when node has provisioned condition with Failed reason", func() {
			node := createNodeWithCondition("test-node", "test-ns", string(hwmgmtv1alpha1.Provisioned), string(hwmgmtv1alpha1.Failed), metav1.ConditionFalse)

			result := isNodeProvisioningInProgress(node)
			Expect(result).To(BeFalse())
		})

		It("should return false when node has no provisioned condition", func() {
			node := createAllocatedNode("test-node", "test-ns", "test-bmh", "test-ns")

			result := isNodeProvisioningInProgress(node)
			Expect(result).To(BeFalse())
		})

		It("should return false when provisioned condition status is true with InProgress reason", func() {
			node := createNodeWithCondition("test-node", "test-ns", string(hwmgmtv1alpha1.Provisioned), string(hwmgmtv1alpha1.InProgress), metav1.ConditionTrue)

			result := isNodeProvisioningInProgress(node)
			Expect(result).To(BeFalse())
		})

		It("should return false when node has different condition type", func() {
			node := createNodeWithCondition("test-node", "test-ns", string(hwmgmtv1alpha1.Configured), string(hwmgmtv1alpha1.InProgress), metav1.ConditionFalse)

			result := isNodeProvisioningInProgress(node)
			Expect(result).To(BeFalse())
		})
	})

	Describe("config annotation helper functions", func() {
		var node *pluginsv1alpha1.AllocatedNode

		BeforeEach(func() {
			node = createAllocatedNode("test-node", "test-ns", "test-bmh", "test-ns")
		})

		Describe("setConfigAnnotation", func() {
			It("should set config annotation when annotations map is nil", func() {
				node.Annotations = nil
				setConfigAnnotation(node, "test-reason")

				Expect(node.Annotations).NotTo(BeNil())
				Expect(node.Annotations[ConfigAnnotation]).To(Equal("test-reason"))
			})

			It("should set config annotation when annotations map exists", func() {
				node.Annotations = map[string]string{"existing": "value"}
				setConfigAnnotation(node, "test-reason")

				Expect(node.Annotations[ConfigAnnotation]).To(Equal("test-reason"))
				Expect(node.Annotations["existing"]).To(Equal("value"))
			})

			It("should overwrite existing config annotation", func() {
				node.Annotations = map[string]string{ConfigAnnotation: "old-reason"}
				setConfigAnnotation(node, "new-reason")

				Expect(node.Annotations[ConfigAnnotation]).To(Equal("new-reason"))
			})
		})

		Describe("getConfigAnnotation", func() {
			It("should return empty string when annotations map is nil", func() {
				node.Annotations = nil
				result := getConfigAnnotation(node)

				Expect(result).To(Equal(""))
			})

			It("should return empty string when config annotation doesn't exist", func() {
				node.Annotations = map[string]string{"other": "value"}
				result := getConfigAnnotation(node)

				Expect(result).To(Equal(""))
			})

			It("should return config annotation value when it exists", func() {
				node.Annotations = map[string]string{ConfigAnnotation: "test-reason"}
				result := getConfigAnnotation(node)

				Expect(result).To(Equal("test-reason"))
			})
		})

		Describe("removeConfigAnnotation", func() {
			It("should handle nil annotations map gracefully", func() {
				node.Annotations = nil
				Expect(func() { removeConfigAnnotation(node) }).NotTo(Panic())
			})

			It("should remove config annotation when it exists", func() {
				node.Annotations = map[string]string{
					ConfigAnnotation: "test-reason",
					"other":          "value",
				}
				removeConfigAnnotation(node)

				_, exists := node.Annotations[ConfigAnnotation]
				Expect(exists).To(BeFalse())
				Expect(node.Annotations["other"]).To(Equal("value"))
			})

			It("should handle non-existent config annotation gracefully", func() {
				node.Annotations = map[string]string{"other": "value"}
				Expect(func() { removeConfigAnnotation(node) }).NotTo(Panic())
				Expect(node.Annotations["other"]).To(Equal("value"))
			})
		})
	})

	Describe("bmhBmcInfo and bmhNodeInfo structs", func() {
		It("should create bmhBmcInfo correctly", func() {
			bmcInfo := bmhBmcInfo{
				Address:         "192.168.1.100",
				CredentialsName: "test-credentials",
			}
			Expect(bmcInfo.Address).To(Equal("192.168.1.100"))
			Expect(bmcInfo.CredentialsName).To(Equal("test-credentials"))
		})

		It("should create bmhNodeInfo correctly", func() {
			nodeInfo := bmhNodeInfo{
				ResourcePoolID: "pool1",
				BMC: &bmhBmcInfo{
					Address:         "192.168.1.100",
					CredentialsName: "test-credentials",
				},
				Interfaces: []*pluginsv1alpha1.Interface{
					{
						Name:       "eth0",
						MACAddress: testBootMAC,
						Label:      "mgmt",
					},
				},
			}
			Expect(nodeInfo.ResourcePoolID).To(Equal("pool1"))
			Expect(nodeInfo.BMC.Address).To(Equal("192.168.1.100"))
			Expect(len(nodeInfo.Interfaces)).To(Equal(1))
			Expect(nodeInfo.Interfaces[0].Name).To(Equal("eth0"))
		})
	})

	Describe("handleBMHCompletion", func() {
		var (
			fakeClient client.Client
			pluginNs   = "test-plugin-ns"
		)

		BeforeEach(func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		})

		It("should return false when no nodes are updating", func() {
			// All nodes have Provisioned=True
			nodelist := &pluginsv1alpha1.AllocatedNodeList{
				Items: []pluginsv1alpha1.AllocatedNode{
					*createNodeWithCondition("node1", pluginNs, string(hwmgmtv1alpha1.Provisioned), string(hwmgmtv1alpha1.Completed), metav1.ConditionTrue),
					*createNodeWithCondition("node2", pluginNs, string(hwmgmtv1alpha1.Provisioned), string(hwmgmtv1alpha1.Completed), metav1.ConditionTrue),
					*createNodeWithCondition("node3", pluginNs, string(hwmgmtv1alpha1.Provisioned), string(hwmgmtv1alpha1.Completed), metav1.ConditionTrue),
				},
			}

			updating, err := handleBMHCompletion(ctx, fakeClient, fakeClient, logger, pluginNs, nodelist)
			Expect(err).ToNot(HaveOccurred())
			Expect(updating).To(BeFalse())
		})

		It("should aggregate errors from multiple node failures", func() {
			// Mix of nodes: some completed, some in progress, some with no condition
			nodelist := &pluginsv1alpha1.AllocatedNodeList{
				Items: []pluginsv1alpha1.AllocatedNode{
					*createNodeWithCondition("node1", pluginNs, string(hwmgmtv1alpha1.Provisioned), string(hwmgmtv1alpha1.Completed), metav1.ConditionTrue),
					*createNodeWithCondition("fail-node2", pluginNs, string(hwmgmtv1alpha1.Provisioned), string(hwmgmtv1alpha1.InProgress), metav1.ConditionFalse),
					*createAllocatedNode("fail-node3", pluginNs, "bmh-node3", pluginNs), // no condition
					*createNodeWithCondition("fail-node4", pluginNs, string(hwmgmtv1alpha1.Provisioned), string(hwmgmtv1alpha1.InProgress), metav1.ConditionFalse),
				},
			}

			// The function will return error because BMHs don't exist, but it should
			// process all 3 nodes that need completion (node2, node3, node4)
			updating, err := handleBMHCompletion(ctx, fakeClient, fakeClient, logger, pluginNs, nodelist)

			// Expect errors because BMHs don't exist for the nodes
			Expect(err).To(HaveOccurred())
			// The error should be aggregated from multiple node failures
			Expect(err.Error()).To(ContainSubstring("failed to handle BMH completion"))
			Expect(err.Error()).To(ContainSubstring("node fail-node2"))
			Expect(err.Error()).To(ContainSubstring("node fail-node3"))
			Expect(err.Error()).To(ContainSubstring("node fail-node4"))
			// Should return true because unexpected errors happened during processing, so we need to retry
			Expect(updating).To(BeTrue())
		})

		It("should return anyUpdating=true when nodes are still in progress", func() {
			// Create nodes with corresponding BMHs that are NOT in Available state
			bmh1 := createBMH("bmh-progress-node1", pluginNs, nil, nil, metal3v1alpha1.StatePreparing)
			bmh2 := createBMH("bmh-progress-node2", pluginNs, nil, nil, metal3v1alpha1.StatePreparing)

			err := fakeClient.Create(ctx, bmh1)
			Expect(err).ToNot(HaveOccurred())
			err = fakeClient.Create(ctx, bmh2)
			Expect(err).ToNot(HaveOccurred())

			nodelist := &pluginsv1alpha1.AllocatedNodeList{
				Items: []pluginsv1alpha1.AllocatedNode{
					*createNodeWithCondition("progress-node1", pluginNs, string(hwmgmtv1alpha1.Provisioned), string(hwmgmtv1alpha1.InProgress), metav1.ConditionFalse),
					*createNodeWithCondition("progress-node2", pluginNs, string(hwmgmtv1alpha1.Provisioned), string(hwmgmtv1alpha1.InProgress), metav1.ConditionFalse),
				},
			}

			updating, err := handleBMHCompletion(ctx, fakeClient, fakeClient, logger, pluginNs, nodelist)
			Expect(err).ToNot(HaveOccurred())
			Expect(updating).To(BeTrue()) // BMHs are not Available yet
		})
	})

	Describe("saveOrigNetworkData", func() {
		const testNs = "test-save-networkdata-ns"
		var fakeClient client.Client

		BeforeEach(func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		})

		It("should save the current PreprovisioningNetworkDataName as an annotation", func() {
			bmh := createBMH("test-bmh", testNs, nil, map[string]string{}, metal3v1alpha1.StateAvailable)
			bmh.Spec.PreprovisioningNetworkDataName = "my-network-data"
			Expect(fakeClient.Create(ctx, bmh)).To(Succeed())

			err := saveOrigNetworkData(ctx, fakeClient, logger, bmh)
			Expect(err).ToNot(HaveOccurred())

			var updated metal3v1alpha1.BareMetalHost
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-bmh", Namespace: testNs}, &updated)).To(Succeed())
			Expect(updated.Annotations[OrigNetworkDataAnnotation]).To(Equal("my-network-data"))
		})

		It("should save empty string when PreprovisioningNetworkDataName is not set", func() {
			bmh := createBMH("test-bmh", testNs, nil, map[string]string{}, metal3v1alpha1.StateAvailable)
			Expect(fakeClient.Create(ctx, bmh)).To(Succeed())

			err := saveOrigNetworkData(ctx, fakeClient, logger, bmh)
			Expect(err).ToNot(HaveOccurred())

			var updated metal3v1alpha1.BareMetalHost
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-bmh", Namespace: testNs}, &updated)).To(Succeed())
			val, exists := updated.Annotations[OrigNetworkDataAnnotation]
			Expect(exists).To(BeTrue())
			Expect(val).To(BeEmpty())
		})

		It("should not overwrite annotation if already set", func() {
			bmh := createBMH("test-bmh", testNs, nil, map[string]string{
				OrigNetworkDataAnnotation: "original-value",
			}, metal3v1alpha1.StateAvailable)
			bmh.Spec.PreprovisioningNetworkDataName = "new-value"
			Expect(fakeClient.Create(ctx, bmh)).To(Succeed())

			err := saveOrigNetworkData(ctx, fakeClient, logger, bmh)
			Expect(err).ToNot(HaveOccurred())

			var updated metal3v1alpha1.BareMetalHost
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-bmh", Namespace: testNs}, &updated)).To(Succeed())
			Expect(updated.Annotations[OrigNetworkDataAnnotation]).To(Equal("original-value"))
		})
	})

	Describe("deleteDataImageIfExists", func() {
		const testNs = "test-dataimage-ns"
		var fakeClient client.Client

		BeforeEach(func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		})

		It("should delete an existing DataImage", func() {
			dataImage := &metal3v1alpha1.DataImage{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-bmh",
					Namespace: testNs,
				},
				Spec: metal3v1alpha1.DataImageSpec{
					URL: "https://example.com/image.iso",
				},
			}
			Expect(fakeClient.Create(ctx, dataImage)).To(Succeed())

			err := deleteDataImageIfExists(ctx, fakeClient, logger, "test-bmh", testNs)
			Expect(err).ToNot(HaveOccurred())

			// Verify it was deleted
			result := &metal3v1alpha1.DataImage{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-bmh", Namespace: testNs}, result)
			Expect(err).To(HaveOccurred())
			Expect(client.IgnoreNotFound(err)).To(Succeed())
		})

		It("should succeed when no DataImage exists", func() {
			err := deleteDataImageIfExists(ctx, fakeClient, logger, "nonexistent-bmh", testNs)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
