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

var _ = Describe("BareMetalHost bootMACAddress workflows", Ordered, Label("bmh-bootmac"), func() {
	const timeout = time.Second * 90
	const interval = time.Second * 3

	var testCtx = context.Background()

	Context("With empty bootMACAddress", func() {
		var (
			bmhName           = "bmh-empty-bootmac"
			bootInterfaceName = "ens3f0"
			bootInterfaceMAC  = "aa:bb:cc:dd:ee:ff"
		)

		It("Creates BMH without bootMACAddress with interface labels", func() {
			By("Creating BMC secret and BMH with empty bootMACAddress and boot-interface label")
			bmh := &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bmhName,
					Namespace: constants.DefaultNamespace,
					Labels: map[string]string{
						constants.BootInterfaceLabelFullKey:                bootInterfaceName,
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
					BootMACAddress:                 "",
					PreprovisioningNetworkDataName: "test-network-data",
				},
			}

			bmcSecret := testutils.CreateBMCSecret(bmhName)
			Expect(K8SClient.Create(testCtx, bmcSecret)).To(Succeed())
			Expect(K8SClient.Create(testCtx, bmh)).To(Succeed())

			By("Verifying BMH was created with empty bootMACAddress")
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
			By("Setting BMH status with hardware details and Available state")
			bmh := &metal3v1alpha1.BareMetalHost{}
			Expect(K8SClient.Get(testCtx, types.NamespacedName{
				Name:      bmhName,
				Namespace: constants.DefaultNamespace,
			}, bmh)).To(Succeed())

			bmh.Status = metal3v1alpha1.BareMetalHostStatus{
				Provisioning: metal3v1alpha1.ProvisionStatus{
					State: metal3v1alpha1.StateAvailable,
				},
				HardwareDetails: &metal3v1alpha1.HardwareDetails{
					Hostname:     "test-empty-bootmac.example.com",
					CPU:          metal3v1alpha1.CPU{Arch: "x86_64"},
					RAMMebibytes: 131072,
					NIC: []metal3v1alpha1.NIC{
						{Name: bootInterfaceName, MAC: bootInterfaceMAC},
						{Name: "eth0", MAC: "aa:bb:cc:dd:ee:01"},
					},
					Storage: []metal3v1alpha1.Storage{
						{Name: "sda", SizeBytes: 6000000000000, Rotational: false},
					},
				},
			}
			Expect(K8SClient.Status().Update(testCtx, bmh)).To(Succeed())

			By("Verifying hardware details were set")
			Eventually(func() bool {
				updatedBMH := &metal3v1alpha1.BareMetalHost{}
				err := K8SClient.Get(testCtx, types.NamespacedName{
					Name:      bmhName,
					Namespace: constants.DefaultNamespace,
				}, updatedBMH)
				return err == nil && updatedBMH.Status.HardwareDetails != nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying bootMACAddress is still empty before allocation")
			updatedBMH := &metal3v1alpha1.BareMetalHost{}
			Expect(K8SClient.Get(testCtx, types.NamespacedName{
				Name:      bmhName,
				Namespace: constants.DefaultNamespace,
			}, updatedBMH)).To(Succeed())
			Expect(updatedBMH.Spec.BootMACAddress).To(Equal(""),
				"bootMACAddress should still be empty before allocation")
		})

		It("Verifies BMH is ready for allocation", func() {
			By("Verifying BMH has all required fields and is ready for allocation")
			bmh := &metal3v1alpha1.BareMetalHost{}
			Expect(K8SClient.Get(testCtx, types.NamespacedName{
				Name:      bmhName,
				Namespace: constants.DefaultNamespace,
			}, bmh)).To(Succeed())

			Expect(bmh.Spec.BMC.Address).ToNot(BeEmpty())
			Expect(bmh.Spec.BootMACAddress).To(Equal(""))
			Expect(bmh.Spec.PreprovisioningNetworkDataName).ToNot(BeEmpty())
			Expect(bmh.Status.Provisioning.State).To(Equal(metal3v1alpha1.StateAvailable))
			Expect(bmh.Status.HardwareDetails).ToNot(BeNil())

			By("Verifying boot-interface label maps to correct NIC in hardware details")
			Expect(bmh.Labels).To(HaveKey(constants.BootInterfaceLabelFullKey))
			Expect(bmh.Labels[constants.BootInterfaceLabelFullKey]).To(Equal(bootInterfaceName))

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
			bmh := &metal3v1alpha1.BareMetalHost{}
			if err := K8SClient.Get(testCtx, types.NamespacedName{
				Name: bmhName, Namespace: constants.DefaultNamespace,
			}, bmh); err == nil {
				_ = K8SClient.Delete(testCtx, bmh)
			}

			secret := &corev1.Secret{}
			if err := K8SClient.Get(testCtx, types.NamespacedName{
				Name: fmt.Sprintf("%s-bmc-secret", bmhName), Namespace: constants.DefaultNamespace,
			}, secret); err == nil {
				_ = K8SClient.Delete(testCtx, secret)
			}
		})
	})

	Context("With bootMACAddress set but no boot-interface label", func() {
		var (
			bmhName           = "bmh-bootmac-no-label"
			bootInterfaceName = "ens3f1"
			bootInterfaceMAC  = "bb:cc:dd:ee:ff:00"
		)

		It("Creates BMH with bootMACAddress but without boot-interface label", func() {
			By("Creating BMC secret and BMH with bootMACAddress set but no boot-interface label")
			bmh := &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bmhName,
					Namespace: constants.DefaultNamespace,
					Labels: map[string]string{
						constants.LabelPrefixInterfaces + "mgmt":           "eth0",
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
					BootMACAddress: bootInterfaceMAC,
				},
			}

			bmcSecret := testutils.CreateBMCSecret(bmhName)
			Expect(K8SClient.Create(testCtx, bmcSecret)).To(Succeed())
			Expect(K8SClient.Create(testCtx, bmh)).To(Succeed())

			By("Verifying BMH was created with bootMACAddress set")
			createdBMH := &metal3v1alpha1.BareMetalHost{}
			Eventually(func() error {
				return K8SClient.Get(testCtx, types.NamespacedName{
					Name:      bmhName,
					Namespace: constants.DefaultNamespace,
				}, createdBMH)
			}, timeout, interval).Should(Succeed())

			Expect(createdBMH.Spec.BootMACAddress).To(Equal(bootInterfaceMAC),
				"bootMACAddress should be set to the pre-provisioned value")
			Expect(createdBMH.Labels).ToNot(HaveKey(constants.BootInterfaceLabelFullKey),
				"Should NOT have boot-interface label")
		})

		It("Simulates hardware inspection populating NIC details", func() {
			By("Setting BMH status with hardware details and Available state")
			bmh := &metal3v1alpha1.BareMetalHost{}
			Expect(K8SClient.Get(testCtx, types.NamespacedName{
				Name:      bmhName,
				Namespace: constants.DefaultNamespace,
			}, bmh)).To(Succeed())

			bmh.Status = metal3v1alpha1.BareMetalHostStatus{
				Provisioning: metal3v1alpha1.ProvisionStatus{
					State: metal3v1alpha1.StateAvailable,
				},
				HardwareDetails: &metal3v1alpha1.HardwareDetails{
					Hostname:     "test-bootmac-no-label.example.com",
					CPU:          metal3v1alpha1.CPU{Arch: "x86_64"},
					RAMMebibytes: 131072,
					NIC: []metal3v1alpha1.NIC{
						{Name: bootInterfaceName, MAC: bootInterfaceMAC},
						{Name: "eth0", MAC: "bb:cc:dd:ee:ff:01"},
					},
					Storage: []metal3v1alpha1.Storage{
						{Name: "sda", SizeBytes: 6000000000000, Rotational: false},
					},
				},
			}
			Expect(K8SClient.Status().Update(testCtx, bmh)).To(Succeed())

			By("Verifying hardware details were set")
			Eventually(func() bool {
				updatedBMH := &metal3v1alpha1.BareMetalHost{}
				err := K8SClient.Get(testCtx, types.NamespacedName{
					Name:      bmhName,
					Namespace: constants.DefaultNamespace,
				}, updatedBMH)
				return err == nil && updatedBMH.Status.HardwareDetails != nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying bootMACAddress remains unchanged after inspection")
			updatedBMH := &metal3v1alpha1.BareMetalHost{}
			Expect(K8SClient.Get(testCtx, types.NamespacedName{
				Name:      bmhName,
				Namespace: constants.DefaultNamespace,
			}, updatedBMH)).To(Succeed())
			Expect(updatedBMH.Spec.BootMACAddress).To(Equal(bootInterfaceMAC),
				"bootMACAddress should remain unchanged")
		})

		It("Verifies BMH is ready for allocation without boot-interface label", func() {
			By("Verifying BMH has bootMACAddress set and is ready for allocation")
			bmh := &metal3v1alpha1.BareMetalHost{}
			Expect(K8SClient.Get(testCtx, types.NamespacedName{
				Name:      bmhName,
				Namespace: constants.DefaultNamespace,
			}, bmh)).To(Succeed())

			Expect(bmh.Spec.BMC.Address).ToNot(BeEmpty())
			Expect(bmh.Spec.BootMACAddress).To(Equal(bootInterfaceMAC))
			Expect(bmh.Status.Provisioning.State).To(Equal(metal3v1alpha1.StateAvailable))
			Expect(bmh.Status.HardwareDetails).ToNot(BeNil())

			Expect(bmh.Labels).ToNot(HaveKey(constants.BootInterfaceLabelFullKey),
				"Should not have boot-interface label")

			By("Verifying boot interface in hardware details matches bootMACAddress")
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
			bmh := &metal3v1alpha1.BareMetalHost{}
			if err := K8SClient.Get(testCtx, types.NamespacedName{
				Name: bmhName, Namespace: constants.DefaultNamespace,
			}, bmh); err == nil {
				_ = K8SClient.Delete(testCtx, bmh)
			}

			secret := &corev1.Secret{}
			if err := K8SClient.Get(testCtx, types.NamespacedName{
				Name: fmt.Sprintf("%s-bmc-secret", bmhName), Namespace: constants.DefaultNamespace,
			}, secret); err == nil {
				_ = K8SClient.Delete(testCtx, secret)
			}
		})
	})
})
