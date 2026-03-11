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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	metal3pluginscontrollers "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/metal3/controller"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	testutils "github.com/openshift-kni/oran-o2ims/test/utils"
	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"k8s.io/client-go/kubernetes"
)

var _ = Describe("MNO Day2 Hardware Configuration test", Ordered, Label("mno-day2-hw-updates"), func() {
	const (
		timeout     = time.Minute * 2
		interval    = time.Second * 3
		masterCount = 3
		workerCount = 8

		annotationTrue = "true"
	)

	var (
		testCtx      context.Context
		clusterName  = "std-test"
		ctNamespace  = "std-4-20-15"
		prName       string // populated from PR YAML metadata.name
		spokeRestore func()

		pr  *provisioningv1alpha1.ProvisioningRequest
		nar *pluginsv1alpha1.NodeAllocationRequest
	)

	testCtx = context.Background()

	// R740 BIOS schema entries (for master nodes)
	r740SchemaEntries := map[string]metal3v1alpha1.SettingSchema{
		"MemTest": {
			AttributeType:   "Enumeration",
			AllowableValues: []string{"Disabled", "Enabled"},
		},
		"AcPwrRcvryUserDelay": {
			AttributeType: "Integer",
			LowerBound:    intPtr(0),
			UpperBound:    intPtr(600),
		},
	}

	// XR8620t BIOS schema entries (for worker nodes)
	xr8620tSchemaEntries := map[string]metal3v1alpha1.SettingSchema{
		"SysProfile": {
			AttributeType:   "Enumeration",
			AllowableValues: []string{"Custom", "Performance", "PerfPerWattOptimizedDapc"},
		},
		"WorkloadProfile": {
			AttributeType:   "Enumeration",
			AllowableValues: []string{"TelcoOptimizedProfile", "NotAvailable", "HpcProfile"},
		},
		"SriovGlobalEnable": {
			AttributeType:   "Enumeration",
			AllowableValues: []string{"Enabled", "Disabled"},
		},
		"AcPwrRcvryUserDelay": {
			AttributeType: "Integer",
			LowerBound:    intPtr(0),
			UpperBound:    intPtr(600),
		},
	}

	// R740 v1 BIOS status settings from mno/hw-profile-dell-r740-v1.yaml
	r740V1StatusSettings := map[string]string{
		"MemTest":             "Disabled",
		"AcPwrRcvryUserDelay": "60",
	}

	// XR8620t v1 BIOS status settings from mno/hw-profile-dell-xr860t-v1.yaml
	xr8620tV1StatusSettings := map[string]string{
		"SysProfile":          "Custom",
		"WorkloadProfile":     "TelcoOptimizedProfile",
		"SriovGlobalEnable":   "Enabled",
		"AcPwrRcvryUserDelay": "120",
	}

	cmYamls := []string{
		"../resources/mno_hw_configuration/clusterinstance-defaults-v1.yaml",
		"../resources/mno_hw_configuration/policytemplate-defaults-v1.yaml",
	}

	hwProfileYamls := []string{
		"../resources/mno_hw_configuration/hw-profile-dell-r740-v1.yaml",
		"../resources/mno_hw_configuration/hw-profile-dell-xr860t-v1.yaml",
		"../resources/mno_hw_configuration/hw-profile-dell-r740-v2.yaml",
		"../resources/mno_hw_configuration/hw-profile-dell-xr860t-v2.yaml",
		"../resources/mno_hw_configuration/hw-profile-dell-xr860t-v3.yaml",
	}

	hwTemplateYaml := "../resources/mno_hw_configuration/hw-std-dell-r740-green-xr8620t-blue.yaml"
	ctYaml := "../resources/mno_hw_configuration/ct-std-dell-r740-green-xr8620t-blue.yaml"

	BeforeAll(func() {
		// Setup

		pr = &provisioningv1alpha1.ProvisioningRequest{}
		nar = &pluginsv1alpha1.NodeAllocationRequest{}

		By("Creating namespaces")
		for _, ns := range []string{ctNamespace, "ztp-" + ctNamespace, "dell-r740-pool", "dell-xr8620t-pool"} {
			err := K8SClient.Create(testCtx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: ns},
			})
			if err != nil && !errors.IsAlreadyExists(err) {
				Expect(err).ToNot(HaveOccurred())
			}
		}

		By("Creating ClusterTemplate, HardwareTemplate, HardwareProfiles, and supporting resources")
		for _, yaml := range cmYamls {
			cm, err := testutils.LoadYAML[corev1.ConfigMap](yaml)
			Expect(err).ToNot(HaveOccurred())
			Expect(K8SClient.Create(testCtx, cm)).To(Succeed())
		}
		for _, yaml := range hwProfileYamls {
			hwProfile, err := testutils.LoadYAML[hwmgmtv1alpha1.HardwareProfile](yaml)
			Expect(err).ToNot(HaveOccurred())
			Expect(K8SClient.Create(testCtx, hwProfile)).To(Succeed())
		}

		hwTemplate, err := testutils.LoadYAML[hwmgmtv1alpha1.HardwareTemplate](hwTemplateYaml)
		Expect(err).ToNot(HaveOccurred())
		Expect(K8SClient.Create(testCtx, hwTemplate)).To(Succeed())
		ct, err := testutils.LoadYAML[provisioningv1alpha1.ClusterTemplate](ctYaml)
		Expect(err).ToNot(HaveOccurred())
		Expect(K8SClient.Create(testCtx, ct)).To(Succeed())

		// Other resources
		pullSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "pull-secret", Namespace: ctNamespace},
			Data:       map[string][]byte{".dockerconfigjson": []byte(testutils.TestSecretDataStr)},
			Type:       corev1.SecretTypeDockerConfigJson,
		}
		clusterImageSet := &hivev1.ClusterImageSet{
			ObjectMeta: metav1.ObjectMeta{Name: "4.20.15"},
			Spec: hivev1.ClusterImageSetSpec{
				ReleaseImage: "quay.io/openshift-release-dev/ocp-release:4.20.15-x86_64",
			},
		}
		// Extra manifests ConfigMap referenced by mno/clusterinstance-defaults-v1.yaml
		extraManifests := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "clustertemplate-sample.v1.0.0-extramanifests",
				Namespace: ctNamespace,
			},
			Data: map[string]string{},
		}
		resources := []client.Object{
			pullSecret, extraManifests, clusterImageSet,
		}
		for _, r := range resources {
			Expect(K8SClient.Create(testCtx, r)).To(Succeed())
		}

		By("Creating 11 BMHs with BMC secrets, HardwareData, HFS, and HFC")
		bmhList := testutils.MnoBMHs(masterCount, workerCount)
		for _, bmhData := range bmhList {
			bmh := testutils.CreateBareMetalHost(bmhData)
			bmcSecret := testutils.CreateBMCSecret(bmhData.Name)
			Expect(K8SClient.Create(testCtx, bmh)).To(Succeed())
			Expect(K8SClient.Create(testCtx, bmcSecret)).To(Succeed())

			// Set the BMH to Available state
			bmh.Status = metal3v1alpha1.BareMetalHostStatus{
				Provisioning: metal3v1alpha1.ProvisionStatus{State: metal3v1alpha1.StateAvailable},
				HardwareDetails: &metal3v1alpha1.HardwareDetails{
					CPU: metal3v1alpha1.CPU{Arch: "x86_64"},
					NIC: []metal3v1alpha1.NIC{
						{Name: "eno1", MAC: bmhData.MacAddress},
					},
				},
			}
			Expect(K8SClient.Status().Update(testCtx, bmh)).To(Succeed())

			hwData := &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bmhData.Name,
					Namespace: bmhData.Namespace,
				},
				Spec: metal3v1alpha1.HardwareDataSpec{
					HardwareDetails: &metal3v1alpha1.HardwareDetails{
						CPU: metal3v1alpha1.CPU{Arch: "x86_64"},
						NIC: []metal3v1alpha1.NIC{
							{Name: "eno1", MAC: bmhData.MacAddress},
						},
					},
				},
			}
			Expect(K8SClient.Create(testCtx, hwData)).To(Succeed())

			// Create FirmwareSchemas
			schemaName := fmt.Sprintf("schema-%s", bmhData.Name)
			vendor := "Dell Inc."
			model := fmt.Sprintf("PowerEdge %s", bmhData.ServerType)
			schemaEntries := r740SchemaEntries
			statusSettings := r740V1StatusSettings
			if bmhData.ServerType == "XR8620t" {
				statusSettings = xr8620tV1StatusSettings
				schemaEntries = xr8620tSchemaEntries
			}
			firmwareSchema := testutils.CreateFirmwareSchema(schemaName, bmhData.Namespace, vendor, model, schemaEntries)
			Expect(K8SClient.Create(testCtx, firmwareSchema)).To(Succeed())

			// HFS status matching v1 profile BIOS attributes
			hfs := testutils.CreateHostFirmwareSettings(bmhData.Name, bmhData.Namespace)
			Expect(K8SClient.Create(testCtx, hfs)).To(Succeed())
			hfs.Status = testutils.UpdateHostFirmwareSettingsStatus(schemaName,
				bmhData.Namespace, statusSettings, metav1.ConditionTrue, metav1.ConditionFalse, hfs.Generation)
			Expect(K8SClient.Status().Update(testCtx, hfs)).To(Succeed())

			// HFC with existing component versions (required for day2 validateHFCHasRequiredComponents)
			var components []metal3v1alpha1.FirmwareComponentStatus
			if bmhData.ServerType == "R740" {
				components = []metal3v1alpha1.FirmwareComponentStatus{
					{Component: "bios", CurrentVersion: "2.20.0"},
					{Component: "bmc", CurrentVersion: "6.10.00.00"},
					{Component: "nic:0", CurrentVersion: "15.0.0"},
				}
			} else {
				components = []metal3v1alpha1.FirmwareComponentStatus{
					{Component: "bios", CurrentVersion: "2.1.0"},
					{Component: "bmc", CurrentVersion: "7.0.0"},
				}
			}
			hfc := testutils.CreateHostFirmwareComponents(bmhData.Name, bmhData.Namespace)
			Expect(K8SClient.Create(testCtx, hfc)).To(Succeed())
			hfc.Status = testutils.UpdateHostFirmwareComponentsStatus(
				bmhData.Name, bmhData.Namespace, components, metav1.ConditionTrue, metav1.ConditionFalse, hfc.Generation)
			Expect(K8SClient.Status().Update(testCtx, hfc)).To(Succeed())
		}

		By("Waiting for all 11 BMHs to be visible via List")
		Eventually(func() int {
			bmhListResult := &metal3v1alpha1.BareMetalHostList{}
			Expect(K8SClient.List(testCtx, bmhListResult,
				client.MatchingLabels{"resources.clcm.openshift.io/siteId": "local-123"})).To(Succeed())
			available := 0
			for _, b := range bmhListResult.Items {
				if b.Status.Provisioning.State == metal3v1alpha1.StateAvailable {
					available++
				}
			}
			return available
		}, timeout, interval).Should(Equal(masterCount+workerCount),
			"All BMHs should be visible and in Available state via List")

		By("Waiting for ClusterTemplate reconciliation")
		Eventually(func() bool {
			newct := &provisioningv1alpha1.ClusterTemplate{}
			Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(ct), newct)).To(Succeed())
			return newct.Status.Conditions != nil
		}, timeout, interval).Should(BeTrue())

		By("Creating ProvisioningRequest with v1 hwProfiles (basic BIOS settings)")
		prFromYAML, err := testutils.LoadYAML[provisioningv1alpha1.ProvisioningRequest]("../resources/mno_hw_configuration/pr-std.yaml")
		Expect(err).ToNot(HaveOccurred())
		prName = prFromYAML.Name
		Expect(K8SClient.Create(testCtx, prFromYAML)).To(Succeed())

		By("Waiting for NAR creation")
		Eventually(func() error {
			return K8SClient.Get(testCtx, types.NamespacedName{
				Name: clusterName, Namespace: constants.DefaultNamespace}, nar)
		}, timeout, interval).Should(Succeed())

		By("Waiting for all 11 AllocatedNodes to be created")
		Eventually(func() int {
			return len(listAllocatedNodesForNAR(testCtx, clusterName).Items)
		}, timeout, interval).Should(Equal(masterCount + workerCount))

		By("Waiting for day0 to complete (NAR Provisioned=True)")
		Eventually(func() bool {
			Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(nar), nar)).To(Succeed())
			cond := meta.FindStatusCondition(nar.Status.Conditions, string(hwmgmtv1alpha1.Provisioned))
			return cond != nil && cond.Status == metav1.ConditionTrue
		}, timeout, interval).Should(BeTrue(), "NAR should reach Provisioned=True")

		By("Triggering callback to complete hardware provisioning on the PR")
		Expect(simulateCallback(testCtx, prName, string(hwmgmtv1alpha1.Completed))).To(Succeed())

		By("Waiting for PR HardwareProvisioned=True")
		Eventually(func() bool {
			Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
			cond := meta.FindStatusCondition(pr.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned))
			return cond != nil && cond.Status == metav1.ConditionTrue
		}, timeout, interval).Should(BeTrue(), "PR should reach HardwareProvisioned=True")

		By("Simulating AllocatedNodeHostMap on PR")
		Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
		nodeList := listAllocatedNodesForNAR(testCtx, clusterName)
		hostMap := make(map[string]string)
		masterIdx, workerIdx := 1, 1
		hostnames := []string{}
		for _, node := range nodeList.Items {
			hostname := ""
			switch node.Spec.GroupName {
			case "master":
				Expect(masterIdx).To(BeNumerically("<=", masterCount))
				hostname = fmt.Sprintf("master-%d.%s.example.com", masterIdx, clusterName)
				masterIdx++
			case "worker":
				Expect(workerIdx).To(BeNumerically("<=", workerCount))
				hostname = fmt.Sprintf("worker-%d.%s.example.com", workerIdx, clusterName)
				workerIdx++
			}
			hostMap[node.Name] = hostname
			hostnames = append(hostnames, hostname)
		}
		pr.Status.Extensions.AllocatedNodeHostMap = hostMap
		Expect(K8SClient.Status().Update(testCtx, pr)).To(Succeed())
		Eventually(func() bool {
			Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
			return len(pr.Status.Extensions.AllocatedNodeHostMap) == masterCount+workerCount
		}, timeout, interval).Should(BeTrue(), "AllocatedNodeHostMap should be populated")

		// Set up spoke client mock for day2 operations
		spokeRestore = setupSpokeClientMock(testCtx, hostnames)
	})

	AfterAll(func() {
		if spokeRestore != nil {
			spokeRestore()
		}

		By("Deleting created resources")
		// Delete BMH-related resources first (BMH, HardwareData, HFS, HFC, FirmwareSchema, BMC secrets)
		bmhList := testutils.MnoBMHs(masterCount, workerCount)
		for _, bmhData := range bmhList {
			for _, obj := range []client.Object{
				&metal3v1alpha1.BareMetalHost{ObjectMeta: metav1.ObjectMeta{Name: bmhData.Name, Namespace: bmhData.Namespace}},
				&metal3v1alpha1.HardwareData{ObjectMeta: metav1.ObjectMeta{Name: bmhData.Name, Namespace: bmhData.Namespace}},
				&metal3v1alpha1.HostFirmwareSettings{ObjectMeta: metav1.ObjectMeta{Name: bmhData.Name, Namespace: bmhData.Namespace}},
				&metal3v1alpha1.HostFirmwareComponents{ObjectMeta: metav1.ObjectMeta{Name: bmhData.Name, Namespace: bmhData.Namespace}},
				&metal3v1alpha1.FirmwareSchema{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("schema-%s", bmhData.Name), Namespace: bmhData.Namespace}},
				&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-bmc-secret", bmhData.Name), Namespace: constants.DefaultNamespace}},
			} {
				existing := obj.DeepCopyObject().(client.Object)
				if err := K8SClient.Get(testCtx, client.ObjectKeyFromObject(obj), existing); err == nil {
					_ = K8SClient.Delete(testCtx, existing)
				}
			}
		}

		// Delete ProvisioningRequest.
		// Strip finalizer first: the PR controller's finalizer waits for cluster namespace
		// deletion, which never completes in envtest (no namespace controller running).
		prObj := &provisioningv1alpha1.ProvisioningRequest{}
		if err := K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, prObj); err == nil {
			prObj.Finalizers = nil
			_ = K8SClient.Update(testCtx, prObj)
			_ = K8SClient.Delete(testCtx, prObj)
		}

		// Clean up NARs and AllocatedNodes (not cascade-deleted since PR finalizer was stripped)
		narObj := &pluginsv1alpha1.NodeAllocationRequest{}
		if err := K8SClient.Get(testCtx, types.NamespacedName{
			Name: clusterName, Namespace: constants.DefaultNamespace,
		}, narObj); err == nil {
			narObj.Finalizers = nil
			_ = K8SClient.Update(testCtx, narObj)
			_ = K8SClient.Delete(testCtx, narObj)
		}
		anList := listAllocatedNodesForNAR(testCtx, clusterName)
		for i := range anList.Items {
			_ = K8SClient.Delete(testCtx, &anList.Items[i])
		}

		ct, err := testutils.LoadYAML[provisioningv1alpha1.ClusterTemplate](ctYaml)
		Expect(err).ToNot(HaveOccurred())
		if err := K8SClient.Get(testCtx, types.NamespacedName{Name: ct.Name, Namespace: ct.Namespace}, ct); err == nil {
			_ = K8SClient.Delete(testCtx, ct)
		}

		hwTemplate, err := testutils.LoadYAML[hwmgmtv1alpha1.HardwareTemplate](hwTemplateYaml)
		Expect(err).ToNot(HaveOccurred())
		if err := K8SClient.Get(testCtx, types.NamespacedName{Name: hwTemplate.Name, Namespace: hwTemplate.Namespace}, hwTemplate); err == nil {
			_ = K8SClient.Delete(testCtx, hwTemplate)
		}

		for _, yaml := range cmYamls {
			cm, err := testutils.LoadYAML[corev1.ConfigMap](yaml)
			Expect(err).ToNot(HaveOccurred())
			if err := K8SClient.Get(testCtx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, cm); err == nil {
				_ = K8SClient.Delete(testCtx, cm)
			}
		}
		for _, yaml := range hwProfileYamls {
			hwProfile, err := testutils.LoadYAML[hwmgmtv1alpha1.HardwareProfile](yaml)
			Expect(err).ToNot(HaveOccurred())
			if err := K8SClient.Get(testCtx, types.NamespacedName{Name: hwProfile.Name, Namespace: hwProfile.Namespace}, hwProfile); err == nil {
				_ = K8SClient.Delete(testCtx, hwProfile)
			}
		}

		cis := &hivev1.ClusterImageSet{}
		if err := K8SClient.Get(testCtx, types.NamespacedName{Name: "4.20.15"}, cis); err == nil {
			_ = K8SClient.Delete(testCtx, cis)
		}

		// Delete remaining resources in test namespaces.
		for _, obj := range []client.Object{
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pull-secret", Namespace: ctNamespace}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "clustertemplate-sample.v1.0.0-extramanifests", Namespace: ctNamespace}},
		} {
			existing := obj.DeepCopyObject().(client.Object)
			if err := K8SClient.Get(testCtx, client.ObjectKeyFromObject(obj), existing); err == nil {
				_ = K8SClient.Delete(testCtx, existing)
			}
		}
	})

	// Test day2 hardware configuration update with master serial and worker parallel updates.
	Describe("Performs day2 hardware configuration update successfully", func() {
		It("Should update the PR with new hwProfiles and trigger configuration update", func() {
			Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
			newPrFromYAML, err := testutils.LoadYAML[provisioningv1alpha1.ProvisioningRequest]("../resources/mno_hw_configuration/pr-std-succeed.yaml")
			Expect(err).ToNot(HaveOccurred())
			pr.Spec.TemplateParameters = newPrFromYAML.Spec.TemplateParameters
			Expect(K8SClient.Update(testCtx, pr)).To(Succeed())
		})

		It("Should detect HW configuration changes and begin update process (NAR InProgress=True)", func() {
			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(nar), nar)).To(Succeed())
				cond := meta.FindStatusCondition(nar.Status.Conditions, string(hwmgmtv1alpha1.Configured))
				return cond != nil && cond.Reason == string(hwmgmtv1alpha1.InProgress)
			}, timeout, interval).Should(BeTrue(), "NAR should be in InProgress")
		})

		It("Should PR reach InProgress", func() {
			// Simulate hardware plugin sending in progress callback.
			Expect(simulateCallback(testCtx, prName, string(hwmgmtv1alpha1.InProgress))).To(Succeed())
			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
				cond := meta.FindStatusCondition(pr.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured))
				return cond != nil && cond.Reason == string(provisioningv1alpha1.CRconditionReasons.InProgress) &&
					cond.Status == metav1.ConditionFalse
			}, timeout, interval).Should(BeTrue(), "PR should reach InProgress")
		})

		It("Should complete rolling update: masters first (maxUnavailable=1), then workers (maxUnavailable=3)", func() {
			By("Polling and advancing BMH state transitions for each node")
			Eventually(func() bool {
				allComplete := true
				nodeList := listAllocatedNodesForNAR(testCtx, clusterName)

				// Track rolling-update invariants
				mastersAllDone := true
				mastersInProgress := 0
				workersInProgress := 0

				for i := range nodeList.Items {
					node := &nodeList.Items[i]

					cond := meta.FindStatusCondition(node.Status.Conditions, string(hwmgmtv1alpha1.Configured))
					isComplete := cond != nil && cond.Reason == string(hwmgmtv1alpha1.ConfigApplied)
					isInProgress := cond != nil && cond.Status == metav1.ConditionFalse &&
						cond.Reason == string(hwmgmtv1alpha1.ConfigUpdate)

					if node.Spec.GroupName == master {
						if !isComplete {
							mastersAllDone = false
						}
						if isInProgress {
							mastersInProgress++
						}
					} else if node.Spec.GroupName == worker {
						if isInProgress {
							workersInProgress++
						}
					}

					// Skip nodes that are already completed
					if isComplete {
						continue
					}

					allComplete = false
					bmhKey := types.NamespacedName{
						Name:      node.Spec.HwMgrNodeId,
						Namespace: node.Spec.HwMgrNodeNs,
					}
					bmh := &metal3v1alpha1.BareMetalHost{}
					if err := K8SClient.Get(testCtx, bmhKey, bmh); err != nil {
						continue
					}

					hasBiosAnnotation := bmh.Annotations[metal3pluginscontrollers.BiosUpdateNeededAnnotation] == annotationTrue
					hasFirmwareAnnotation := bmh.Annotations[metal3pluginscontrollers.FirmwareUpdateNeededAnnotation] == annotationTrue
					// For nodes that require BIOS or firmware updates, simulate the BMO BIOS and firmware updates.
					if (hasBiosAnnotation || hasFirmwareAnnotation) &&
						bmh.Status.OperationalStatus != metal3v1alpha1.OperationalStatusServicing {
						completeBMHDay2(testCtx, node, bmh)
					}
				}

				// Verify rolling-update invariants:
				// 1. No workers should be in-progress before all masters are done
				if !mastersAllDone {
					Expect(workersInProgress).To(Equal(0),
						"workers should not start updates before all masters complete")
				}
				// 2. Masters in-progress should not exceed maxUnavailable (1)
				Expect(mastersInProgress).To(BeNumerically("<=", 1),
					"masters in-progress should not exceed maxUnavailable=1")
				// 3. Workers in-progress should not exceed maxUnavailable (3)
				Expect(workersInProgress).To(BeNumerically("<=", 3),
					"workers in-progress should not exceed maxUnavailable=3")

				return allComplete
			}, timeout*5, interval).Should(BeTrue(), "All nodes should reach ConfigApplied after rolling update")

			By("Waiting for NAR to reach ConfigApplied")
			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(nar), nar)).To(Succeed())
				cond := meta.FindStatusCondition(nar.Status.Conditions, string(hwmgmtv1alpha1.Configured))
				return cond != nil && cond.Status == metav1.ConditionTrue &&
					cond.Reason == string(hwmgmtv1alpha1.ConfigApplied)
			}, timeout, interval).Should(BeTrue(), "NAR should reach ConfigApplied")
		})

		It("Should PR reach HardwareConfigured=True", func() {
			// Simulate hardware plugin sending completion callback.
			Expect(simulateCallback(testCtx, prName, string(hwmgmtv1alpha1.ConfigApplied))).To(Succeed())
			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
				cond := meta.FindStatusCondition(pr.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured))
				return cond != nil && cond.Status == metav1.ConditionTrue
			}, timeout, interval).Should(BeTrue(), "PR should reach HardwareConfigured=True")
		})
	})

	// Test day2 hardware configuration update where one worker BMH enters error state.
	// Only workers are updated (master profiles unchanged). The controller should detect the
	// BMH error, mark the corresponding AllocatedNode as Failed, and propagate the failure to NAR and PR.
	Describe("Handles day2 hardware configuration update with BMH error", func() {
		It("Should update the PR with v3 worker profile (masters unchanged)", func() {
			Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
			newPrFromYAML, err := testutils.LoadYAML[provisioningv1alpha1.ProvisioningRequest]("../resources/mno_hw_configuration/pr-std-fail.yaml")
			Expect(err).ToNot(HaveOccurred())
			pr.Spec.TemplateParameters = newPrFromYAML.Spec.TemplateParameters
			Expect(K8SClient.Update(testCtx, pr)).To(Succeed())
		})

		It("Should detect worker configuration changes and begin update (NAR InProgress)", func() {
			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(nar), nar)).To(Succeed())
				cond := meta.FindStatusCondition(nar.Status.Conditions, string(hwmgmtv1alpha1.Configured))
				return cond != nil && cond.Reason == string(hwmgmtv1alpha1.InProgress)
			}, timeout, interval).Should(BeTrue(), "NAR should be in InProgress")

			Expect(simulateCallback(testCtx, prName, string(hwmgmtv1alpha1.InProgress))).To(Succeed())
		})

		It("Should PR reach InProgress", func() {
			// Simulate hardware plugin sending in progress callback.
			Expect(simulateCallback(testCtx, prName, string(hwmgmtv1alpha1.InProgress))).To(Succeed())
			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
				cond := meta.FindStatusCondition(pr.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured))
				return cond != nil && cond.Reason == string(provisioningv1alpha1.CRconditionReasons.InProgress) &&
					cond.Status == metav1.ConditionFalse
			}, timeout, interval).Should(BeTrue(), "PR should reach InProgress")
		})

		It("Should fail when one worker BMH enters error state during update", func() {
			By("Failing one worker BMH and waiting for NAR to reach Failed")
			Eventually(func() bool {
				nodeList := listAllocatedNodesForNAR(testCtx, clusterName)
				for i := range nodeList.Items {
					node := &nodeList.Items[i]

					if node.Spec.GroupName == "master" {
						continue
					}

					// Return true if one of the workers has reached the Failed state
					cond := meta.FindStatusCondition(node.Status.Conditions, string(hwmgmtv1alpha1.Configured))
					if cond != nil && (cond.Reason == string(hwmgmtv1alpha1.ConfigApplied) ||
						cond.Reason == string(hwmgmtv1alpha1.Failed)) {
						return true
					}

					bmhKey := types.NamespacedName{
						Name:      node.Spec.HwMgrNodeId,
						Namespace: node.Spec.HwMgrNodeNs,
					}
					bmh := &metal3v1alpha1.BareMetalHost{}
					if err := K8SClient.Get(testCtx, bmhKey, bmh); err != nil {
						continue
					}

					hasBiosAnnotation := bmh.Annotations[metal3pluginscontrollers.BiosUpdateNeededAnnotation] == annotationTrue
					hasFirmwareAnnotation := bmh.Annotations[metal3pluginscontrollers.FirmwareUpdateNeededAnnotation] == annotationTrue
					if !(hasBiosAnnotation || hasFirmwareAnnotation) ||
						bmh.Status.OperationalStatus == metal3v1alpha1.OperationalStatusServicing {
						continue
					}

					// Fail the first worker with update annotations ready to be failed
					failBMHDay2(testCtx, node, bmh)
					break
				}

				return false
			}, timeout*5, interval).Should(BeTrue(), "One worker BMH should have been failed")

			By("Waiting for NAR to reach Configured=False/Failed")
			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(nar), nar)).To(Succeed())
				cond := meta.FindStatusCondition(nar.Status.Conditions, string(hwmgmtv1alpha1.Configured))
				return cond != nil && cond.Status == metav1.ConditionFalse &&
					cond.Reason == string(hwmgmtv1alpha1.Failed)
			}, timeout, interval).Should(BeTrue(), "NAR should reach Configured=False/Failed")
		})

		It("Should PR reach HardwareConfigured=Failed after callback", func() {
			// Simulate hardware plugin sending failed callback.
			Expect(simulateCallback(testCtx, prName, string(hwmgmtv1alpha1.Failed))).To(Succeed())
			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
				cond := meta.FindStatusCondition(pr.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured))
				return cond != nil && cond.Status == metav1.ConditionFalse &&
					cond.Reason == string(provisioningv1alpha1.CRconditionReasons.Failed)
			}, timeout, interval).Should(BeTrue(), "PR should reach HardwareConfigured=Failed")
		})
	})
})

func intPtr(v int) *int { return &v }

func simulateCallback(ctx context.Context, prName, status string) error {
	pr := &provisioningv1alpha1.ProvisioningRequest{}
	if err := K8SClient.Get(ctx, types.NamespacedName{Name: prName}, pr); err != nil {
		return fmt.Errorf("failed to get ProvisioningRequest %s: %w", prName, err)
	}
	if pr.Annotations == nil {
		pr.Annotations = make(map[string]string)
	}
	pr.Annotations[ctlrutils.CallbackReceivedAnnotation] = fmt.Sprintf("%d", time.Now().Unix())
	pr.Annotations[ctlrutils.CallbackStatusAnnotation] = status
	if err := K8SClient.Update(ctx, pr); err != nil {
		return fmt.Errorf("failed to update ProvisioningRequest %s: %w", prName, err)
	}
	return nil
}

func listAllocatedNodesForNAR(ctx context.Context, narName string) *pluginsv1alpha1.AllocatedNodeList {
	all := &pluginsv1alpha1.AllocatedNodeList{}
	Expect(K8SClient.List(ctx, all, client.InNamespace(constants.DefaultNamespace))).To(Succeed())
	filtered := &pluginsv1alpha1.AllocatedNodeList{}
	for _, n := range all.Items {
		if n.Spec.NodeAllocationRequest == narName {
			filtered.Items = append(filtered.Items, n)
		}
	}
	return filtered
}

func setupSpokeClientMock(ctx context.Context, hostnames []string) func() {
	_ = ctx
	spokeScheme := runtime.NewScheme()
	Expect(corev1.AddToScheme(spokeScheme)).To(Succeed())
	Expect(machineconfigv1.Install(spokeScheme)).To(Succeed())

	// Explicitly set master MCP maxUnavailable to 1.
	masterMCP := &machineconfigv1.MachineConfigPool{
		ObjectMeta: metav1.ObjectMeta{Name: "master"},
		Spec: machineconfigv1.MachineConfigPoolSpec{
			MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
		},
	}
	// Set worker MCP maxUnavailable to 3.
	workerMCP := &machineconfigv1.MachineConfigPool{
		ObjectMeta: metav1.ObjectMeta{Name: "worker"},
		Spec: machineconfigv1.MachineConfigPoolSpec{
			MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 3},
		},
	}
	var spokeObjects []client.Object
	spokeObjects = append(spokeObjects, masterMCP, workerMCP)
	var k8sNodes []runtime.Object
	for _, hostname := range hostnames {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: hostname},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		}
		spokeObjects = append(spokeObjects, node)
		k8sNodes = append(k8sNodes, node.DeepCopy())
	}
	spokeClient := fakeclient.NewClientBuilder().
		WithScheme(spokeScheme).
		WithObjects(spokeObjects...).
		WithStatusSubresource(&corev1.Node{}).
		Build()
	spokeClientset := kubefake.NewSimpleClientset(k8sNodes...)

	return metal3pluginscontrollers.SetTestSpokeClientCreators(
		func(_ context.Context, _ client.Client, _ string) (client.Client, error) {
			return spokeClient, nil
		},
		func(_ context.Context, _ client.Client, _ string) (kubernetes.Interface, error) {
			return spokeClientset, nil
		},
	)
}

// failBMHDay2 simulates a BMH entering an error state during a day2 hardware configuration update.
// It transitions the BMH through Servicing and then to Error with an expired transient error timestamp,
// so the controller immediately treats it as a non-transient failure and marks the AllocatedNode as Failed.
func failBMHDay2(ctx context.Context, node *pluginsv1alpha1.AllocatedNode, bmh *metal3v1alpha1.BareMetalHost) {
	bmhKey := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
	nodeKey := types.NamespacedName{Name: node.Name, Namespace: node.Namespace}

	// Step 1: Simulate metal3 detecting HFS/HFC spec changes
	hfs := &metal3v1alpha1.HostFirmwareSettings{}
	Expect(K8SClient.Get(ctx, bmhKey, hfs)).To(Succeed())
	schemaName := hfs.Status.FirmwareSchema.Name
	hfs.Status = testutils.UpdateHostFirmwareSettingsStatus(
		schemaName, hfs.Namespace, hfs.Status.Settings, metav1.ConditionTrue, metav1.ConditionTrue, hfs.Generation)
	Expect(K8SClient.Status().Update(ctx, hfs)).To(Succeed())

	hfc := &metal3v1alpha1.HostFirmwareComponents{}
	Expect(K8SClient.Get(ctx, bmhKey, hfc)).To(Succeed())
	hfc.Status = testutils.UpdateHostFirmwareComponentsStatus(
		hfc.Name, hfc.Namespace, hfc.Status.Components, metav1.ConditionTrue, metav1.ConditionTrue, hfc.Generation)
	Expect(K8SClient.Status().Update(ctx, hfc)).To(Succeed())

	// Step 2: Transition BMH to Servicing (simulates metal3 processing the reboot annotation)
	Expect(K8SClient.Get(ctx, bmhKey, bmh)).To(Succeed())
	bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusServicing
	Expect(K8SClient.Status().Update(ctx, bmh)).To(Succeed())

	// Step 3: Wait for hwplugin to set config-in-progress annotation on AllocatedNode
	Eventually(func() bool {
		n := &pluginsv1alpha1.AllocatedNode{}
		Expect(K8SClient.Get(ctx, nodeKey, n)).To(Succeed())
		return n.Annotations[metal3pluginscontrollers.ConfigAnnotation] != ""
	}, time.Minute*3, time.Second*2).Should(BeTrue(),
		"AllocatedNode %s should have config annotation", node.Name)

	// Step 4: Transition BMH to Error with an expired transient error timestamp.
	// Pre-setting a timestamp older than ErrorRetryWindow (5min) ensures the controller
	// treats this as a non-transient failure and immediately marks the node as Failed.
	Expect(K8SClient.Get(ctx, bmhKey, bmh)).To(Succeed())
	if bmh.Annotations == nil {
		bmh.Annotations = make(map[string]string)
	}
	bmh.Annotations[metal3pluginscontrollers.BmhErrorTimestampAnnotation] = time.Now().Add(-10 * time.Minute).Format(time.RFC3339)
	Expect(K8SClient.Update(ctx, bmh)).To(Succeed())

	Expect(K8SClient.Get(ctx, bmhKey, bmh)).To(Succeed())
	bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusError
	bmh.Status.ErrorMessage = "Firmware update failed"
	bmh.Status.ErrorType = metal3v1alpha1.ServicingError
	Expect(K8SClient.Status().Update(ctx, bmh)).To(Succeed())
}

// completeBMHDay2 simulates the metal3 BMO BIOS and firmware updates for a day2.
// hardware configuration update on a single BMH:
//  1. Simulate metal3 detecting HFS/HFC spec changes (set conditions)
//  2. Transition BMH to Servicing state (simulates reboot)
//  3. Wait for controller to set config-in-progress annotation
//  4. Update HFS/HFC status to match v2 profile values (simulates BIOS and firmware update completion)
//  5. Transition BMH to OK state (triggers handleNodeInProgressUpdate completion)
func completeBMHDay2(ctx context.Context, node *pluginsv1alpha1.AllocatedNode, bmh *metal3v1alpha1.BareMetalHost) {
	bmhKey := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
	nodeKey := types.NamespacedName{Name: node.Name, Namespace: node.Namespace}

	// Step 1: Simulate metal3 detecting HFS and HFC spec changes
	hfs := &metal3v1alpha1.HostFirmwareSettings{}
	Expect(K8SClient.Get(ctx, bmhKey, hfs)).To(Succeed())
	schemaName := hfs.Status.FirmwareSchema.Name
	hfs.Status = testutils.UpdateHostFirmwareSettingsStatus(
		schemaName, hfs.Namespace, hfs.Status.Settings, metav1.ConditionTrue, metav1.ConditionTrue, hfs.Generation)
	Expect(K8SClient.Status().Update(ctx, hfs)).To(Succeed())
	hfc := &metal3v1alpha1.HostFirmwareComponents{}
	Expect(K8SClient.Get(ctx, bmhKey, hfc)).To(Succeed())
	hfc.Status = testutils.UpdateHostFirmwareComponentsStatus(
		hfc.Name, hfc.Namespace, hfc.Status.Components, metav1.ConditionTrue, metav1.ConditionTrue, hfc.Generation)
	Expect(K8SClient.Status().Update(ctx, hfc)).To(Succeed())

	// Step 2: Transition BMH to Servicing (simulates metal3 processing the reboot annotation)
	Expect(K8SClient.Get(ctx, bmhKey, bmh)).To(Succeed())
	bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusServicing
	Expect(K8SClient.Status().Update(ctx, bmh)).To(Succeed())

	// Step 3: Wait for hwplugin to set config-in-progress annotation on AllocatedNode
	Eventually(func() bool {
		n := &pluginsv1alpha1.AllocatedNode{}
		Expect(K8SClient.Get(ctx, nodeKey, n)).To(Succeed())
		return n.Annotations[metal3pluginscontrollers.ConfigAnnotation] != ""
	}, time.Minute*3, time.Second*2).Should(BeTrue(),
		"AllocatedNode %s should have config annotation", node.Name)

	// Step 4.a: Update HFS status to match updated BIOS attributes (simulates BIOS update completion)
	Expect(K8SClient.Get(ctx, bmhKey, hfs)).To(Succeed())
	updatedSettings := make(map[string]string)
	for k, v := range hfs.Spec.Settings {
		updatedSettings[k] = v.String()
	}
	hfs.Status.Settings = updatedSettings
	hfs.Status = testutils.UpdateHostFirmwareSettingsStatus(
		schemaName, hfs.Namespace, hfs.Status.Settings, metav1.ConditionTrue, metav1.ConditionFalse, hfs.Generation)
	Expect(K8SClient.Status().Update(ctx, hfs)).To(Succeed())

	// Step 4.b: Update HFC status components with v2 firmware versions (simulates firmware update completion)
	Expect(K8SClient.Get(ctx, nodeKey, node)).To(Succeed())
	hwProfile := &hwmgmtv1alpha1.HardwareProfile{}
	Expect(K8SClient.Get(ctx, types.NamespacedName{
		Name: node.Spec.HwProfile, Namespace: constants.DefaultNamespace,
	}, hwProfile)).To(Succeed())

	newComponents := []metal3v1alpha1.FirmwareComponentStatus{}
	if hwProfile.Spec.BiosFirmware.Version != "" {
		newComponents = append(newComponents, metal3v1alpha1.FirmwareComponentStatus{
			Component: "bios", CurrentVersion: hwProfile.Spec.BiosFirmware.Version,
		})
	}
	if hwProfile.Spec.BmcFirmware.Version != "" {
		newComponents = append(newComponents, metal3v1alpha1.FirmwareComponentStatus{
			Component: "bmc", CurrentVersion: hwProfile.Spec.BmcFirmware.Version,
		})
	}
	for i, nic := range hwProfile.Spec.NicFirmware {
		if nic.Version != "" {
			newComponents = append(newComponents, metal3v1alpha1.FirmwareComponentStatus{
				Component: fmt.Sprintf("nic:%d", i), CurrentVersion: nic.Version,
			})
		}
	}
	Expect(K8SClient.Get(ctx, bmhKey, hfc)).To(Succeed())
	hfc.Status.Components = newComponents
	hfc.Status = testutils.UpdateHostFirmwareComponentsStatus(
		hfc.Name, hfc.Namespace, hfc.Status.Components, metav1.ConditionTrue, metav1.ConditionFalse, hfc.Generation)
	Expect(K8SClient.Status().Update(ctx, hfc)).To(Succeed())

	// Step 5: Transition BMH back to OK state (triggers handleNodeInProgressUpdate completion)
	Expect(K8SClient.Get(ctx, bmhKey, bmh)).To(Succeed())
	bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusOK
	bmh.Status.ErrorMessage = ""
	bmh.Status.ErrorType = ""
	Expect(K8SClient.Status().Update(ctx, bmh)).To(Succeed())
}
