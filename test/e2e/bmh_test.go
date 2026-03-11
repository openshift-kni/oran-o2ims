/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllersE2Etest

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	testutils "github.com/openshift-kni/oran-o2ims/test/utils"
)

// TODO: These tests do not test any actual functionality, leave them here for now for revisiting.
// Test empty bootMACAddress workflow where bootMAC is populated from interface labels
var _ = Describe("BareMetalHost with empty bootMACAddress", Ordered, func() {
	const timeout = time.Second * 90
	const interval = time.Second * 3

	var (
		testCtx           = context.Background()
		bmhName           = "bmh-empty-bootmac"
		bootInterfaceName = "ens3f0"
		bootInterfaceMAC  = "aa:bb:cc:dd:ee:ff"
	)

	It("Creates BMH without bootMACAddress with interface labels", func() {
		// Create a BMH similar to the ones in BeforeSuite, but WITHOUT bootMACAddress
		bmh := &metal3v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bmhName,
				Namespace: constants.DefaultNamespace,
				Labels: map[string]string{
					// Add interface label that maps to the boot interface
					constants.BootInterfaceLabelFullKey: bootInterfaceName,
					// Add same resource selector labels as existing BMHs for allocation
					"resourceselector.clcm.openshift.io/server-colour": "green",
					"resources.clcm.openshift.io/resourcePoolId":       testutils.TestPoolID,
					"resourceselector.clcm.openshift.io/server-type":   testutils.TestServerType,
				},
			},
			Spec: metal3v1alpha1.BareMetalHostSpec{
				Online: true,
				BMC: metal3v1alpha1.BMCDetails{
					Address:         "redfish://192.168.1.200/redfish/v1/Systems/1",
					CredentialsName: fmt.Sprintf("%s-bmc-secret", bmhName),
				},
				// Leave BootMACAddress EMPTY - this is the key test scenario
				BootMACAddress: "",
				// Set PreprovisioningNetworkDataName to allow empty bootMACAddress
				PreprovisioningNetworkDataName: "test-network-data",
			},
		}

		// Create BMC secret
		bmcSecret := testutils.CreateBMCSecret(bmhName)
		Expect(K8SClient.Create(testCtx, bmcSecret)).To(Succeed())

		// Create the BMH
		Expect(K8SClient.Create(testCtx, bmh)).To(Succeed())

		// Verify BMH was created with empty bootMACAddress
		createdBMH := &metal3v1alpha1.BareMetalHost{}
		Eventually(func() error {
			return K8SClient.Get(testCtx, types.NamespacedName{
				Name:      bmhName,
				Namespace: constants.DefaultNamespace,
			}, createdBMH)
		}, timeout, interval).Should(Succeed())

		Expect(createdBMH.Spec.BootMACAddress).To(Equal(""),
			"bootMACAddress should be empty when BMH is created")
		Expect(createdBMH.Spec.PreprovisioningNetworkDataName).To(Equal("test-network-data"))
	})

	It("Simulates hardware inspection populating NIC details", func() {
		// Update BMH status to simulate inspection completing (like BeforeSuite does)
		bmh := &metal3v1alpha1.BareMetalHost{}
		Expect(K8SClient.Get(testCtx, types.NamespacedName{
			Name:      bmhName,
			Namespace: constants.DefaultNamespace,
		}, bmh)).To(Succeed())

		// Set status similar to BeforeSuite setup
		bmh.Status = metal3v1alpha1.BareMetalHostStatus{
			Provisioning: metal3v1alpha1.ProvisionStatus{
				State: metal3v1alpha1.StateAvailable,
			},
			HardwareDetails: &metal3v1alpha1.HardwareDetails{
				Hostname: "test-empty-bootmac.example.com",
				CPU: metal3v1alpha1.CPU{
					Arch: "x86_64",
				},
				RAMMebibytes: 131072, // 128GB - matches selection criteria
				NIC: []metal3v1alpha1.NIC{
					{
						Name: bootInterfaceName, // This matches the interface label value
						MAC:  bootInterfaceMAC,
					},
					{
						Name: "eth0",
						MAC:  "aa:bb:cc:dd:ee:01",
					},
				},
				Storage: []metal3v1alpha1.Storage{
					{
						Name:       "sda",
						SizeBytes:  6000000000000, // 6TB - matches green BMH criteria
						Rotational: false,
					},
				},
			},
		}
		Expect(K8SClient.Status().Update(testCtx, bmh)).To(Succeed())

		// Verify hardware details were set
		Eventually(func() bool {
			updatedBMH := &metal3v1alpha1.BareMetalHost{}
			err := K8SClient.Get(testCtx, types.NamespacedName{
				Name:      bmhName,
				Namespace: constants.DefaultNamespace,
			}, updatedBMH)
			return err == nil && updatedBMH.Status.HardwareDetails != nil
		}, timeout, interval).Should(BeTrue())

		// Verify bootMACAddress is STILL empty (not populated yet)
		updatedBMH := &metal3v1alpha1.BareMetalHost{}
		Expect(K8SClient.Get(testCtx, types.NamespacedName{
			Name:      bmhName,
			Namespace: constants.DefaultNamespace,
		}, updatedBMH)).To(Succeed())
		Expect(updatedBMH.Spec.BootMACAddress).To(Equal(""),
			"bootMACAddress should still be empty before allocation")
	})

	It("Verifies bootMACAddress is populated during allocation", func() {
		// The bmh-empty-bootmac should be available for allocation now
		// When a NodeAllocationRequest with matching criteria is processed,
		// it should be allocated and bootMACAddress should be set

		// We can't easily trigger allocation in isolation, but we can verify
		// the BMH is in the right state and ready to be allocated
		bmh := &metal3v1alpha1.BareMetalHost{}
		Expect(K8SClient.Get(testCtx, types.NamespacedName{
			Name:      bmhName,
			Namespace: constants.DefaultNamespace,
		}, bmh)).To(Succeed())

		// Verify BMH has all required fields except bootMACAddress
		Expect(bmh.Spec.BMC.Address).ToNot(BeEmpty())
		Expect(bmh.Spec.BootMACAddress).To(Equal(""))
		Expect(bmh.Spec.PreprovisioningNetworkDataName).ToNot(BeEmpty())
		Expect(bmh.Status.Provisioning.State).To(Equal(metal3v1alpha1.StateAvailable))
		Expect(bmh.Status.HardwareDetails).ToNot(BeNil())

		// Verify interface label is present
		Expect(bmh.Labels).To(HaveKey(constants.BootInterfaceLabelFullKey))
		Expect(bmh.Labels[constants.BootInterfaceLabelFullKey]).To(Equal(bootInterfaceName))

		// Verify the boot interface exists in hardware details
		foundBootInterface := false
		for _, nic := range bmh.Status.HardwareDetails.NIC {
			if nic.Name == bootInterfaceName {
				Expect(nic.MAC).To(Equal(bootInterfaceMAC))
				foundBootInterface = true
				break
			}
		}
		Expect(foundBootInterface).To(BeTrue(),
			"Boot interface should exist in hardware details")
	})

	AfterAll(func() {
		// Clean up the test BMH
		bmh := &metal3v1alpha1.BareMetalHost{}
		err := K8SClient.Get(testCtx, types.NamespacedName{
			Name:      bmhName,
			Namespace: constants.DefaultNamespace,
		}, bmh)
		if err == nil {
			_ = K8SClient.Delete(testCtx, bmh)
		}

		// Clean up BMC secret
		secret := &corev1.Secret{}
		err = K8SClient.Get(testCtx, types.NamespacedName{
			Name:      fmt.Sprintf("%s-bmc-secret", bmhName),
			Namespace: constants.DefaultNamespace,
		}, secret)
		if err == nil {
			_ = K8SClient.Delete(testCtx, secret)
		}
	})
})

// Test BMH with bootMACAddress set but no boot-interface label (Scenario 1)
var _ = Describe("BareMetalHost with bootMACAddress set but no interface label", Ordered, func() {
	const timeout = time.Second * 90
	const interval = time.Second * 3

	var (
		testCtx           = context.Background()
		bmhName           = "bmh-bootmac-no-label"
		bootInterfaceName = "ens3f1"
		bootInterfaceMAC  = "bb:cc:dd:ee:ff:00"
	)

	It("Creates BMH with bootMACAddress but without boot-interface label", func() {
		// Create a BMH with bootMACAddress set, but WITHOUT the boot-interface label
		// This tests the scenario where the hardware was pre-provisioned with a known boot MAC
		bmh := &metal3v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bmhName,
				Namespace: constants.DefaultNamespace,
				Labels: map[string]string{
					// NO boot-interface label - this is the key difference from scenario 2
					// Add other interface labels for non-boot interfaces
					constants.LabelPrefixInterfaces + "mgmt": "eth0",
					// Add same resource selector labels as existing BMHs for allocation
					"resourceselector.clcm.openshift.io/server-colour": "green",
					"resources.clcm.openshift.io/resourcePoolId":       testutils.TestPoolID,
					"resourceselector.clcm.openshift.io/server-type":   testutils.TestServerType,
				},
			},
			Spec: metal3v1alpha1.BareMetalHostSpec{
				Online: true,
				BMC: metal3v1alpha1.BMCDetails{
					Address:         "redfish://192.168.1.201/redfish/v1/Systems/1",
					CredentialsName: fmt.Sprintf("%s-bmc-secret", bmhName),
				},
				// bootMACAddress IS set - pre-provisioned with known boot MAC
				BootMACAddress: bootInterfaceMAC,
			},
		}

		// Create BMC secret
		bmcSecret := testutils.CreateBMCSecret(bmhName)
		Expect(K8SClient.Create(testCtx, bmcSecret)).To(Succeed())

		// Create the BMH
		Expect(K8SClient.Create(testCtx, bmh)).To(Succeed())

		// Verify BMH was created with bootMACAddress set
		createdBMH := &metal3v1alpha1.BareMetalHost{}
		Eventually(func() error {
			return K8SClient.Get(testCtx, types.NamespacedName{
				Name:      bmhName,
				Namespace: constants.DefaultNamespace,
			}, createdBMH)
		}, timeout, interval).Should(Succeed())

		Expect(createdBMH.Spec.BootMACAddress).To(Equal(bootInterfaceMAC),
			"bootMACAddress should be set to the pre-provisioned value")
		// Verify NO boot-interface label
		Expect(createdBMH.Labels).ToNot(HaveKey(constants.BootInterfaceLabelFullKey),
			"Should NOT have boot-interface label")
	})

	It("Simulates hardware inspection populating NIC details", func() {
		// Update BMH status to simulate inspection completing
		bmh := &metal3v1alpha1.BareMetalHost{}
		Expect(K8SClient.Get(testCtx, types.NamespacedName{
			Name:      bmhName,
			Namespace: constants.DefaultNamespace,
		}, bmh)).To(Succeed())

		// Set status with hardware details
		bmh.Status = metal3v1alpha1.BareMetalHostStatus{
			Provisioning: metal3v1alpha1.ProvisionStatus{
				State: metal3v1alpha1.StateAvailable,
			},
			HardwareDetails: &metal3v1alpha1.HardwareDetails{
				Hostname: "test-bootmac-no-label.example.com",
				CPU: metal3v1alpha1.CPU{
					Arch: "x86_64",
				},
				RAMMebibytes: 131072, // 128GB
				NIC: []metal3v1alpha1.NIC{
					{
						Name: bootInterfaceName,
						MAC:  bootInterfaceMAC,
					},
					{
						Name: "eth0",
						MAC:  "bb:cc:dd:ee:ff:01",
					},
				},
				Storage: []metal3v1alpha1.Storage{
					{
						Name:       "sda",
						SizeBytes:  6000000000000, // 6TB - matches green BMH criteria
						Rotational: false,
					},
				},
			},
		}
		Expect(K8SClient.Status().Update(testCtx, bmh)).To(Succeed())

		// Verify hardware details were set
		Eventually(func() bool {
			updatedBMH := &metal3v1alpha1.BareMetalHost{}
			err := K8SClient.Get(testCtx, types.NamespacedName{
				Name:      bmhName,
				Namespace: constants.DefaultNamespace,
			}, updatedBMH)
			return err == nil && updatedBMH.Status.HardwareDetails != nil
		}, timeout, interval).Should(BeTrue())

		// Verify bootMACAddress remains unchanged
		updatedBMH := &metal3v1alpha1.BareMetalHost{}
		Expect(K8SClient.Get(testCtx, types.NamespacedName{
			Name:      bmhName,
			Namespace: constants.DefaultNamespace,
		}, updatedBMH)).To(Succeed())
		Expect(updatedBMH.Spec.BootMACAddress).To(Equal(bootInterfaceMAC),
			"bootMACAddress should remain unchanged")
	})

	It("Verifies BMH is ready for allocation without requiring boot-interface label", func() {
		bmh := &metal3v1alpha1.BareMetalHost{}
		Expect(K8SClient.Get(testCtx, types.NamespacedName{
			Name:      bmhName,
			Namespace: constants.DefaultNamespace,
		}, bmh)).To(Succeed())

		// Verify BMH has bootMACAddress set and is ready for allocation
		Expect(bmh.Spec.BMC.Address).ToNot(BeEmpty())
		Expect(bmh.Spec.BootMACAddress).To(Equal(bootInterfaceMAC))
		Expect(bmh.Status.Provisioning.State).To(Equal(metal3v1alpha1.StateAvailable))
		Expect(bmh.Status.HardwareDetails).ToNot(BeNil())

		// Verify NO boot-interface label (allocation should still work with bootMACAddress already set)
		Expect(bmh.Labels).ToNot(HaveKey(constants.BootInterfaceLabelFullKey),
			"Should not have boot-interface label")

		// Verify the boot interface exists in hardware details and matches bootMACAddress
		foundBootInterface := false
		for _, nic := range bmh.Status.HardwareDetails.NIC {
			if nic.MAC == bootInterfaceMAC {
				Expect(nic.Name).To(Equal(bootInterfaceName))
				foundBootInterface = true
				break
			}
		}
		Expect(foundBootInterface).To(BeTrue(),
			"Boot interface with matching MAC should exist in hardware details")
	})

	AfterAll(func() {
		// Clean up the test BMH
		bmh := &metal3v1alpha1.BareMetalHost{}
		err := K8SClient.Get(testCtx, types.NamespacedName{
			Name:      bmhName,
			Namespace: constants.DefaultNamespace,
		}, bmh)
		if err == nil {
			_ = K8SClient.Delete(testCtx, bmh)
		}

		// Clean up BMC secret
		secret := &corev1.Secret{}
		err = K8SClient.Get(testCtx, types.NamespacedName{
			Name:      fmt.Sprintf("%s-bmc-secret", bmhName),
			Namespace: constants.DefaultNamespace,
		}, secret)
		if err == nil {
			_ = K8SClient.Delete(testCtx, secret)
		}
	})
})
