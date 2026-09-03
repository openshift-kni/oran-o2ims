/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllersE2Etest

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	hwmgrcontrollers "github.com/openshift-kni/oran-o2ims/internal/hardwaremanager/controller"
	"github.com/openshift-kni/oran-o2ims/internal/spokeclient"
	testutils "github.com/openshift-kni/oran-o2ims/test/utils"
	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"k8s.io/client-go/kubernetes"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	mnoTimeout     = time.Minute * 2
	mnoLongTimeout = time.Minute * 3
	mnoInterval    = time.Second * 3
)

var _ = Describe("MNO Day2 Hardware Configuration test", Ordered, Label("mno-day2-hw-updates"), func() {
	const (
		timeout       = mnoTimeout
		interval      = mnoInterval
		master        = "master"
		workerR740    = "worker-r740-blue"
		workerXR8620t = "worker-xr8620t-blue"
		masterCount   = 3
		// Two worker pools exercise concurrent per-MCP day2 updates.
		r740WorkerCount    = 4
		xr8620tWorkerCount = 4
		workerCount        = r740WorkerCount + xr8620tWorkerCount

		// Resource pool label values on MNO BMHs (see testutils.MnoBMHsTwoWorkerPools).
		mnoBMHResourcePoolDellR740   = "dell-r740-pool"
		mnoBMHResourcePoolDellXR8620 = "dell-xr8620t-pool"

		prName          = "88744070-717a-4305-8461-796244098338"
		hwConfigMSAName = prName + "-hwconfig"
		hwConfigMWName  = prName + "-hwconfig-rbac"
	)

	var (
		testCtx      context.Context
		clusterName  = "std-test"
		ctNamespace  = "std-4-20-15"
		spokeRestore func()

		pr  *provisioningv1alpha1.ProvisioningRequest
		nar *hwmgmtv1alpha1.NodeAllocationRequest
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

	ctYaml := "../resources/mno_hw_configuration/ct-std-dell-r740-green-xr8620t-blue.yaml"

	BeforeAll(func() {
		// Setup

		pr = &provisioningv1alpha1.ProvisioningRequest{}
		nar = &hwmgmtv1alpha1.NodeAllocationRequest{}

		By("Creating namespaces")
		for _, ns := range []string{ctNamespace, "ztp-" + ctNamespace, "dell-r740-pool", "dell-xr8620t-pool"} {
			err := K8SClient.Create(testCtx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: ns},
			})
			if err != nil && !errors.IsAlreadyExists(err) {
				Expect(err).ToNot(HaveOccurred())
			}
		}

		By("Creating FirmwareCatalog, ClusterTemplate, HardwareProfiles, and supporting resources")
		fwCatalog, err := testutils.LoadYAML[hwmgmtv1alpha1.FirmwareCatalog](
			"../resources/mno_hw_configuration/firmware-catalog.yaml")
		Expect(err).ToNot(HaveOccurred())
		existing := &hwmgmtv1alpha1.FirmwareCatalog{}
		if err := K8SClient.Get(testCtx, client.ObjectKeyFromObject(fwCatalog), existing); err == nil {
			Expect(K8SClient.Delete(testCtx, existing)).To(Succeed())
		}
		Expect(K8SClient.Create(testCtx, fwCatalog)).To(Succeed())

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
			&clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName},
				Spec: clusterv1.ManagedClusterSpec{
					ManagedClusterClientConfigs: []clusterv1.ClientConfig{
						{URL: "https://api." + clusterName + ".example.com:6443"},
					},
				},
			},
		}
		for _, r := range resources {
			Expect(K8SClient.Create(testCtx, r)).To(Succeed())
		}

		By("Creating 11 BMHs with BMC secrets, HardwareData, HFS, and HFC")
		bmhList := mnoBMHsTwoWorkerPools(masterCount, r740WorkerCount, xr8620tWorkerCount)
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

		mnoBMHPoolSel := labels.NewSelector()
		req, reqErr := labels.NewRequirement(constants.LabelResourcePoolName, selection.In,
			[]string{mnoBMHResourcePoolDellR740, mnoBMHResourcePoolDellXR8620})
		Expect(reqErr).ToNot(HaveOccurred())
		mnoBMHPoolSel = mnoBMHPoolSel.Add(*req)

		By("Waiting for all 11 BMHs to be visible via List")
		Eventually(func() int {
			bmhListResult := &metal3v1alpha1.BareMetalHostList{}
			Expect(K8SClient.List(testCtx, bmhListResult,
				client.MatchingLabelsSelector{Selector: mnoBMHPoolSel})).To(Succeed())
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
		Expect(prFromYAML.Name).To(Equal(prName))
		Expect(K8SClient.Create(testCtx, prFromYAML)).To(Succeed())

		By("Waiting for NAR creation")
		Eventually(func() error {
			return K8SClient.Get(testCtx, types.NamespacedName{
				Name: prName, Namespace: constants.DefaultNamespace}, nar)
		}, timeout, interval).Should(Succeed())

		By("Waiting for all 11 AllocatedNodes to be created")
		Eventually(func() int {
			return len(testNonCachingListAllocatedNodesForNAR(testCtx, prName).Items)
		}, timeout, interval).Should(Equal(masterCount + workerCount))

		By("Waiting for day0 to complete (NAR Provisioned=True)")
		Eventually(func() bool {
			Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(nar), nar)).To(Succeed())
			cond := meta.FindStatusCondition(nar.Status.Conditions, string(hwmgmtv1alpha1.Provisioned))
			return cond != nil && cond.Status == metav1.ConditionTrue
		}, timeout, interval).Should(BeTrue(), "NAR should reach Provisioned=True")

		By("Waiting for PR HardwareProvisioned=True")
		Eventually(func() bool {
			Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
			cond := meta.FindStatusCondition(pr.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned))
			return cond != nil && cond.Status == metav1.ConditionTrue
		}, timeout, interval).Should(BeTrue(), "PR should reach HardwareProvisioned=True")

		By("Simulating AllocatedNodeHostMap on PR")
		Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
		nodeList := testNonCachingListAllocatedNodesForNAR(testCtx, prName)
		hostMap := make(map[string]string)
		masterIdx, r740Idx, xr8620tIdx := 1, 1, 1
		hostnames := []string{}
		for _, node := range nodeList.Items {
			hostname := ""
			switch node.Spec.GroupName {
			case master:
				Expect(masterIdx).To(BeNumerically("<=", masterCount))
				hostname = fmt.Sprintf("master-%d.%s.example.com", masterIdx, clusterName)
				masterIdx++
			case workerR740:
				Expect(r740Idx).To(BeNumerically("<=", r740WorkerCount))
				hostname = fmt.Sprintf("worker-r740-%d.%s.example.com", r740Idx, clusterName)
				r740Idx++
			case workerXR8620t:
				Expect(xr8620tIdx).To(BeNumerically("<=", xr8620tWorkerCount))
				hostname = fmt.Sprintf("worker-xr8620t-%d.%s.example.com", xr8620tIdx, clusterName)
				xr8620tIdx++
			}
			hostMap[node.Name] = hostname
			hostnames = append(hostnames, hostname)
		}
		pr.Status.Extensions.AllocatedNodeHostMap = hostMap
		if pr.Status.Extensions.ClusterDetails == nil {
			pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{}
		}
		pr.Status.Extensions.ClusterDetails.Name = clusterName
		Expect(K8SClient.Status().Update(testCtx, pr)).To(Succeed())
		Eventually(func() bool {
			Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
			return len(pr.Status.Extensions.AllocatedNodeHostMap) == masterCount+workerCount
		}, timeout, interval).Should(BeTrue(), "AllocatedNodeHostMap should be populated")

		// Set up spoke client mock for day2 operations
		spokeRestore = setupSpokeClientMock(hostnames)

		By("Creating ManagedClusterAddOn for the cluster")
		Expect(client.IgnoreAlreadyExists(K8SClient.Create(testCtx, &addonv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{Name: "managed-serviceaccount", Namespace: clusterName},
		}))).To(Succeed())
	})

	AfterAll(func() {
		if spokeRestore != nil {
			spokeRestore()
		}

		By("Deleting created resources")
		// Delete BMH-related resources first (BMH, HardwareData, HFS, HFC, FirmwareSchema, BMC secrets)
		bmhList := mnoBMHsTwoWorkerPools(masterCount, r740WorkerCount, xr8620tWorkerCount)
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
		narObj := &hwmgmtv1alpha1.NodeAllocationRequest{}
		if err := K8SClient.Get(testCtx, types.NamespacedName{
			Name: prName, Namespace: constants.DefaultNamespace,
		}, narObj); err == nil {
			narObj.Finalizers = nil
			_ = K8SClient.Update(testCtx, narObj)
			_ = K8SClient.Delete(testCtx, narObj)
		}
		anList := testNonCachingListAllocatedNodesForNAR(testCtx, prName)
		for i := range anList.Items {
			anList.Items[i].Finalizers = nil
			_ = K8SClient.Update(testCtx, &anList.Items[i])
			_ = K8SClient.Delete(testCtx, &anList.Items[i])
		}

		ct, err := testutils.LoadYAML[provisioningv1alpha1.ClusterTemplate](ctYaml)
		Expect(err).ToNot(HaveOccurred())
		if err := K8SClient.Get(testCtx, types.NamespacedName{Name: ct.Name, Namespace: ct.Namespace}, ct); err == nil {
			_ = K8SClient.Delete(testCtx, ct)
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

		fwCatalogCleanup, err := testutils.LoadYAML[hwmgmtv1alpha1.FirmwareCatalog](
			"../resources/mno_hw_configuration/firmware-catalog.yaml")
		Expect(err).ToNot(HaveOccurred())
		if err := K8SClient.Get(testCtx, types.NamespacedName{Name: fwCatalogCleanup.Name, Namespace: fwCatalogCleanup.Namespace}, fwCatalogCleanup); err == nil {
			_ = K8SClient.Delete(testCtx, fwCatalogCleanup)
		}

		cis := &hivev1.ClusterImageSet{}
		if err := K8SClient.Get(testCtx, types.NamespacedName{Name: "4.20.15"}, cis); err == nil {
			_ = K8SClient.Delete(testCtx, cis)
		}

		Expect(client.IgnoreNotFound(K8SClient.Delete(testCtx, &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName},
		}))).To(Succeed())

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
			testutils.SimulateSpokeAccessReady(testCtx, K8SClient, clusterName, hwConfigMSAName, hwConfigMWName, timeout, interval)
		})

		It("Should detect HW configuration changes and begin update process (NAR InProgress=True)", func() {
			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(nar), nar)).To(Succeed())
				cond := meta.FindStatusCondition(nar.Status.Conditions, string(hwmgmtv1alpha1.Configured))
				return cond != nil && cond.Reason == string(hwmgmtv1alpha1.InProgress)
			}, timeout, interval).Should(BeTrue(), "NAR should be in InProgress")
		})

		It("Should PR reach InProgress", func() {
			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
				cond := meta.FindStatusCondition(pr.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured))
				return cond != nil && cond.Reason == string(provisioningv1alpha1.CRconditionReasons.InProgress) &&
					cond.Status == metav1.ConditionFalse
			}, timeout, interval).Should(BeTrue(), "PR should reach InProgress")
		})

		It("Should complete rolling update: masters first (maxUnavailable=1), then both worker pools concurrently (worker-r740-blue maxUnavailable=2, worker-xr8620t-blue maxUnavailable=3)", func() {
			By("Polling and advancing BMH state transitions for each node")
			// Tracks whether both worker pools were ever observed updating at the
			// same time (proves cross-pool concurrency).
			bothWorkerPoolsConcurrent := false
			Eventually(func() bool {
				allComplete := true
				nodeList := testNonCachingListAllocatedNodesForNAR(testCtx, prName)

				// Track rolling-update invariants
				mastersAllDone := true
				mastersInProgress := 0
				inProgressByWorkerPool := map[string]int{} // keyed by worker pool GroupName

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
					} else if isInProgress {
						// Any non-master group is a worker pool; count per pool.
						inProgressByWorkerPool[node.Spec.GroupName]++
					}

					// Skip nodes that are already completed
					if isComplete {
						continue
					}

					allComplete = false

					if !isInProgress {
						continue
					}
					bmhKey := types.NamespacedName{
						Name:      node.Spec.HwMgrNodeId,
						Namespace: node.Spec.HwMgrNodeNs,
					}
					bmh := &metal3v1alpha1.BareMetalHost{}
					if err := K8SClient.Get(testCtx, bmhKey, bmh); err != nil {
						continue
					}

					// State machine: simulate the BMO day2 BIOS/firmware update lifecycle
					// for a single BMH. Each iteration advances one step per node. The
					// outer Eventually loop re-polls until all nodes reach ConfigApplied.
					//
					// Simulate the BMO BIOS/firmware update behavior:
					//   1. Detect that HFS/HFC spec changes by setting ChangeDetected=True on the status conditions
					//   2. After BMH is annotated with reboot annotation, BMH transitions to Servicing and removes the annotation
					//   3. Update the HFS/HFC status to match the new profile values
					//   4. Transition BMH back to OK
					//
					// The checks below are ordered from latest to earliest state so that
					// a node already past an earlier step is not re-processed.
					hasConfigAnnotation := node.Annotations[hwmgrcontrollers.ConfigAnnotation] != ""
					_, hasReboot := bmh.Annotations[hwmgrcontrollers.BmhRebootAnnotation]
					isServicing := bmh.Status.OperationalStatus == metal3v1alpha1.OperationalStatusServicing

					// Complete the update by updating HFS/HFC status to match the new profile
					// values and transition BMH back to OK.
					if hasConfigAnnotation && isServicing {
						completeBMHServicing(testCtx, node, bmh)
						continue
					}

					// Waiting for controller:
					// - isServicing && !hasConfigAnnotation: BMH entered Servicing,
					//   waiting for controller to add config-in-progress annotation.
					// - !isServicing && hasConfigAnnotation: BMH transitioned back to OK,
					//   waiting for controller to validate completion and remove config-in-progress.
					if isServicing || hasConfigAnnotation {
						continue
					}

					// The controller has added the reboot annotation after validating
					// HFS/HFC changes. Simulate BMO processing the reboot by transitioning BMH
					// to Servicing and removing the reboot annotation.
					if !isServicing && !hasConfigAnnotation && hasReboot {
						patchBMHStatus(testCtx, bmhKey, func(bmh *metal3v1alpha1.BareMetalHost) {
							bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusServicing
						})
						removeBMHRebootAnnotation(testCtx, bmhKey)
						continue
					}

					// Simulate metal3 detecting that HFS/HFC specs have changed by
					// setting ChangeDetected=True on their status conditions. This triggers
					// the controller to validate and add the reboot annotation.
					if !isServicing && !hasConfigAnnotation && !isHFSChangeDetected(testCtx, bmhKey) {
						simulateHFSAndHFCChangeDetected(testCtx, bmhKey)
					}
				}

				// Verify rolling-update invariants, split by rollout phase.
				if !mastersAllDone {
					// Masters roll first, serially: no worker pool may start yet,
					// and masters honor the control-plane maxUnavailable=1.
					inProgressWorkers := inProgressByWorkerPool[workerR740] + inProgressByWorkerPool[workerXR8620t]
					Expect(inProgressWorkers).To(Equal(0),
						"workers should not start updates before all masters complete")
					Expect(mastersInProgress).To(BeNumerically("<=", 1),
						"masters in-progress should not exceed maxUnavailable=1")
				} else {
					// Masters done: worker pools roll concurrently, each bounded by
					// its own MCP maxUnavailable.
					Expect(inProgressByWorkerPool[workerR740]).To(BeNumerically("<=", 2),
						"worker-r740-blue in-progress should not exceed its maxUnavailable=2")
					Expect(inProgressByWorkerPool[workerXR8620t]).To(BeNumerically("<=", 3),
						"worker-xr8620t-blue in-progress should not exceed its maxUnavailable=3")
					// Record cross-pool concurrency: both worker pools updating at once.
					if inProgressByWorkerPool[workerR740] > 0 && inProgressByWorkerPool[workerXR8620t] > 0 {
						bothWorkerPoolsConcurrent = true
					}
				}

				return allComplete
			}, timeout*5, interval).Should(BeTrue(), "All nodes should reach ConfigApplied after rolling update")

			By("Verifying the two worker pools were updated concurrently")
			Expect(bothWorkerPoolsConcurrent).To(BeTrue(),
				"worker-r740-blue and worker-xr8620t-blue should be in-progress simultaneously after masters complete")

			By("Waiting for NAR to reach ConfigApplied")
			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(nar), nar)).To(Succeed())
				cond := meta.FindStatusCondition(nar.Status.Conditions, string(hwmgmtv1alpha1.Configured))
				return cond != nil && cond.Status == metav1.ConditionTrue &&
					cond.Reason == string(hwmgmtv1alpha1.ConfigApplied)
			}, timeout, interval).Should(BeTrue(), "NAR should reach ConfigApplied")
		})

		It("Should PR reach HardwareConfigured=True", func() {
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
			testutils.SimulateSpokeAccessReady(testCtx, K8SClient, clusterName, hwConfigMSAName, hwConfigMWName, timeout, interval)
		})

		It("Should detect worker configuration changes and begin update (NAR InProgress)", func() {
			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(nar), nar)).To(Succeed())
				cond := meta.FindStatusCondition(nar.Status.Conditions, string(hwmgmtv1alpha1.Configured))
				return cond != nil && cond.Reason == string(hwmgmtv1alpha1.InProgress)
			}, timeout, interval).Should(BeTrue(), "NAR should be in InProgress")

		})

		It("Should PR reach InProgress", func() {
			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
				cond := meta.FindStatusCondition(pr.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured))
				return cond != nil && cond.Reason == string(provisioningv1alpha1.CRconditionReasons.InProgress) &&
					cond.Status == metav1.ConditionFalse
			}, timeout, interval).Should(BeTrue(), "PR should reach InProgress")
		})

		It("Should fail when one worker BMH enters error state during update", func() {
			By("Finding the first in-progress worker")
			var failNode *hwmgmtv1alpha1.AllocatedNode
			Eventually(func() bool {
				nodeList := testNonCachingListAllocatedNodesForNAR(testCtx, prName)
				for i := range nodeList.Items {
					node := &nodeList.Items[i]
					if node.Spec.GroupName == master {
						continue
					}
					cond := meta.FindStatusCondition(node.Status.Conditions, string(hwmgmtv1alpha1.Configured))
					isInProgress := cond != nil && cond.Status == metav1.ConditionFalse &&
						cond.Reason == string(hwmgmtv1alpha1.ConfigUpdate)
					if isInProgress {
						failNode = node
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "Should find an in-progress worker to fail")

			By("Failing the worker BMH")
			bmhKey := types.NamespacedName{
				Name:      failNode.Spec.HwMgrNodeId,
				Namespace: failNode.Spec.HwMgrNodeNs,
			}
			bmh := &metal3v1alpha1.BareMetalHost{}
			Expect(K8SClient.Get(testCtx, bmhKey, bmh)).To(Succeed())
			failBMHDay2(testCtx, failNode, bmh)

			By("Waiting for the worker node to reach Failed")
			Eventually(func() bool {
				n := &hwmgmtv1alpha1.AllocatedNode{}
				Expect(K8SClient.Get(testCtx, types.NamespacedName{
					Name: failNode.Name, Namespace: failNode.Namespace}, n)).To(Succeed())
				cond := meta.FindStatusCondition(n.Status.Conditions, string(hwmgmtv1alpha1.Configured))
				return cond != nil && cond.Reason == string(hwmgmtv1alpha1.Failed)
			}, timeout, interval).Should(BeTrue(), "Worker node should reach Failed")

			By("Waiting for NAR to reach Configured=False/Failed")
			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(nar), nar)).To(Succeed())
				cond := meta.FindStatusCondition(nar.Status.Conditions, string(hwmgmtv1alpha1.Configured))
				return cond != nil && cond.Status == metav1.ConditionFalse &&
					cond.Reason == string(hwmgmtv1alpha1.Failed)
			}, timeout, interval).Should(BeTrue(), "NAR should reach Configured=False/Failed")
		})

		It("Should PR reach HardwareConfigured=Failed", func() {
			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
				cond := meta.FindStatusCondition(pr.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured))
				return cond != nil && cond.Status == metav1.ConditionFalse &&
					cond.Reason == string(provisioningv1alpha1.CRconditionReasons.Failed)
			}, timeout, interval).Should(BeTrue(), "PR should reach HardwareConfigured=Failed")
		})
	})

	// Test mid-flight profile change with mixed BMH states (scoped to the worker-xr8620t-blue pool):
	// 1. Trigger a real v1 update (BIOS + firmware changes) so its workers enter ConfigUpdate
	// 2. Advance ONE worker's BMH to Servicing with config-in-progress annotation
	// 3. Change profile to v2 mid-flight
	// 4. Workers with BMH=OK are abandoned immediately
	// 5. The 1 worker with BMH=Servicing defers abandon (controller requeues)
	// 6. Transition the Servicing BMH to OK -> deferred abandon proceeds
	// 7. All worker-xr8620t-blue nodes reconverge to v2
	Describe("Handles mid-flight profile change by abandoning stale updates", func() {
		var (
			servicingNodeKey types.NamespacedName
			servicingBMHKey  types.NamespacedName
		)

		v1Profile := "dell-xr8620t-bios-basic"
		v2WorkerProfile := "dell-xr8620t-bios-2.3.5-bmc-7.10.70.10"

		It("Should update PR with v1 worker profile requiring BIOS and firmware changes", func() {
			// Start from pr-std-succeed, then override only the worker-xr8620t-blue
			// hwProfile to v1 (dell-xr8620t-bios-basic). Master and worker-r740-blue
			// stay on their succeed profiles, so only the xr8620t pool rolls. This
			// forces real BIOS changes (AcPwrRcvryUserDelay 130→120) and firmware
			// downgrades (bios 2.3.5→2.1.0, bmc 7.10.70.10→7.0.0).
			Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
			v2PR, err := testutils.LoadYAML[provisioningv1alpha1.ProvisioningRequest](
				"../resources/mno_hw_configuration/pr-std-succeed.yaml")
			Expect(err).ToNot(HaveOccurred())
			pr.Spec.TemplateParameters = v2PR.Spec.TemplateParameters

			var params map[string]interface{}
			Expect(json.Unmarshal(pr.Spec.TemplateParameters.Raw, &params)).To(Succeed())
			hwParams := params["hwMgmtParameters"].(map[string]interface{})
			nodeGroupData := hwParams["nodeGroupData"].([]interface{})
			for _, ng := range nodeGroupData {
				ngMap := ng.(map[string]interface{})
				if ngMap["name"] == workerXR8620t {
					ngMap["hwProfile"] = v1Profile
				}
			}
			raw, err := json.Marshal(params)
			Expect(err).ToNot(HaveOccurred())
			pr.Spec.TemplateParameters = runtime.RawExtension{Raw: raw}
			Expect(K8SClient.Update(testCtx, pr)).To(Succeed())
			testutils.SimulateSpokeAccessReady(testCtx, K8SClient, clusterName, hwConfigMSAName, hwConfigMWName, timeout, interval)
		})

		It("Should have 3 workers in ConfigUpdate and advance one BMH to Servicing", func() {
			By("Detect worker configuration changes and begin update (NAR InProgress)")
			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(nar), nar)).To(Succeed())
				cond := meta.FindStatusCondition(nar.Status.Conditions, string(hwmgmtv1alpha1.Configured))
				return cond != nil && cond.Reason == string(hwmgmtv1alpha1.InProgress)
			}, timeout, interval).Should(BeTrue(), "NAR should be in InProgress")

			By("Waiting for 3 nodes in the worker-xr8620t-blue pool to be in ConfigUpdate with v1 profile")
			Eventually(func() int {
				count := 0
				nodeList := testNonCachingListAllocatedNodesForNAR(testCtx, prName)
				for i := range nodeList.Items {
					node := &nodeList.Items[i]
					if node.Spec.GroupName != workerXR8620t || node.Spec.HwProfile != v1Profile {
						continue
					}
					cond := meta.FindStatusCondition(node.Status.Conditions, string(hwmgmtv1alpha1.Configured))
					if cond != nil && cond.Reason == string(hwmgmtv1alpha1.ConfigUpdate) {
						count++
					}
				}
				return count
			}, timeout, interval).Should(BeNumerically(">=", 3),
				"3 nodes in the worker-xr8620t-blue pool should be in ConfigUpdate with the v1 profile")

			By("Picking one worker and advancing its BMH to Servicing with config-in-progress")
			nodeList := testNonCachingListAllocatedNodesForNAR(testCtx, prName)
			for i := range nodeList.Items {
				node := &nodeList.Items[i]
				if node.Spec.GroupName != workerXR8620t || node.Spec.HwProfile != v1Profile {
					continue
				}
				cond := meta.FindStatusCondition(node.Status.Conditions, string(hwmgmtv1alpha1.Configured))
				if cond != nil && cond.Reason == string(hwmgmtv1alpha1.ConfigUpdate) {
					servicingNodeKey = types.NamespacedName{Name: node.Name, Namespace: node.Namespace}
					servicingBMHKey = types.NamespacedName{Name: node.Spec.HwMgrNodeId, Namespace: node.Spec.HwMgrNodeNs}
					break
				}
			}
			Expect(servicingNodeKey.Name).ToNot(BeEmpty(), "Should find a worker in ConfigUpdate")

			// Simulate metal3 detecting HFS/HFC spec changes
			simulateHFSAndHFCChangeDetected(testCtx, servicingBMHKey)

			// Wait for the controller to add the reboot annotation on the BMH
			waitForBMHRebootAnnotation(testCtx, servicingBMHKey)

			// Transition BMH to Servicing and remove the reboot annotation (simulates metal3 processing the reboot annotation)
			patchBMHStatus(testCtx, servicingBMHKey, func(bmh *metal3v1alpha1.BareMetalHost) {
				bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusServicing
			})
			removeBMHRebootAnnotation(testCtx, servicingBMHKey)

			// Wait for controller to set config-in-progress annotation on the AllocatedNode
			Eventually(func() bool {
				n := &hwmgmtv1alpha1.AllocatedNode{}
				Expect(K8SClient.Get(testCtx, servicingNodeKey, n)).To(Succeed())
				return n.Annotations[hwmgrcontrollers.ConfigAnnotation] != ""
			}, timeout, interval).Should(BeTrue(),
				"Worker should have config-in-progress annotation")
		})

		It("Should change to v2 worker profile mid-flight, defer abandon for Servicing worker, then converge all", func() {
			By("Changing worker profile to v2 while v1 updates are in-flight")
			Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
			v2PR, err := testutils.LoadYAML[provisioningv1alpha1.ProvisioningRequest](
				"../resources/mno_hw_configuration/pr-std-succeed.yaml")
			Expect(err).ToNot(HaveOccurred())
			pr.Spec.TemplateParameters = v2PR.Spec.TemplateParameters
			Expect(K8SClient.Update(testCtx, pr)).To(Succeed())

			By("Waiting for the non-servicing worker-xr8620t-blue nodes to converge to v2 while the Servicing worker defers abandon")
			// Within the worker-xr8620t-blue pool: workers with BMH=OK are abandoned
			// immediately -> re-initiated with v2 -> ConfigApplied quickly (HFS/HFC status
			// already matches v2 from the success test, so no actual HW changes are needed).
			// The 1 Servicing worker can't be abandoned (BMH Servicing) -> stays in ConfigUpdate.
			Eventually(func() int {
				count := 0
				nodeList := testNonCachingListAllocatedNodesForNAR(testCtx, prName)
				for _, node := range nodeList.Items {
					if node.Spec.GroupName != workerXR8620t {
						continue
					}
					cond := meta.FindStatusCondition(node.Status.Conditions, string(hwmgmtv1alpha1.Configured))
					if cond != nil && cond.Status == metav1.ConditionTrue &&
						cond.Reason == string(hwmgmtv1alpha1.ConfigApplied) &&
						node.Spec.HwProfile == v2WorkerProfile {
						count++
					}
				}
				return count
			}, mnoLongTimeout, interval).Should(Equal(xr8620tWorkerCount-1),
				"all worker-xr8620t-blue nodes except the deferred Servicing one should converge to v2")

			By("Verifying the Servicing worker is still in ConfigUpdate with v1 profile (deferred abandon)")
			n := &hwmgmtv1alpha1.AllocatedNode{}
			Expect(K8SClient.Get(testCtx, servicingNodeKey, n)).To(Succeed())
			Expect(n.Spec.HwProfile).To(Equal(v1Profile),
				"Servicing worker should still be in v1 profile")
			cond := meta.FindStatusCondition(n.Status.Conditions, string(hwmgmtv1alpha1.Configured))
			Expect(cond).ToNot(BeNil())
			Expect(cond.Reason).To(Equal(string(hwmgmtv1alpha1.ConfigUpdate)), "Servicing worker should still be in ConfigUpdate")

			By("Transitioning the Servicing BMH to OK to allow deferred abandon")
			patchBMHStatus(testCtx, servicingBMHKey, func(bmh *metal3v1alpha1.BareMetalHost) {
				bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusOK
				bmh.Status.ErrorMessage = ""
				bmh.Status.ErrorType = ""
			})

			By("Waiting for NAR to reach ConfigApplied after all workers converge")
			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(nar), nar)).To(Succeed())
				cond := meta.FindStatusCondition(nar.Status.Conditions, string(hwmgmtv1alpha1.Configured))
				return cond != nil && cond.Status == metav1.ConditionTrue &&
					cond.Reason == string(hwmgmtv1alpha1.ConfigApplied)
			}, timeout, interval).Should(BeTrue(), "NAR should reach ConfigApplied")

			By("Verifying all worker-xr8620t-blue nodes converged to v2 profile")
			nodeList := testNonCachingListAllocatedNodesForNAR(testCtx, prName)
			for _, node := range nodeList.Items {
				if node.Spec.GroupName != workerXR8620t {
					continue
				}
				Expect(node.Spec.HwProfile).To(Equal(v2WorkerProfile),
					"Worker %s should have v2 profile", node.Name)
			}
		})

		It("Should PR reach HardwareConfigured=True", func() {
			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
				cond := meta.FindStatusCondition(pr.Status.Conditions,
					string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured))
				return cond != nil && cond.Status == metav1.ConditionTrue
			}, timeout, interval).Should(BeTrue(), "PR should reach HardwareConfigured=True")
		})
	})
})

func intPtr(v int) *int { return &v }

// testNonCachingListAllocatedNodesForNAR is a test-only helper that lists AllocatedNodes
// for a NAR using the non-caching K8SClient with in-memory filtering. This is
// intentionally different from the production listAllocatedNodesForNAR in
// internal/controllers/helpers.go which uses MatchingFields on a cached client
// — the non-caching client is needed here so test assertions see the latest
// API server state without cache delay.
func testNonCachingListAllocatedNodesForNAR(ctx context.Context, narName string) *hwmgmtv1alpha1.AllocatedNodeList {
	all := &hwmgmtv1alpha1.AllocatedNodeList{}
	Expect(K8SClient.List(ctx, all, client.InNamespace(constants.DefaultNamespace))).To(Succeed())
	filtered := &hwmgmtv1alpha1.AllocatedNodeList{}
	for i := range all.Items {
		if all.Items[i].Spec.NodeAllocationRequest == narName {
			filtered.Items = append(filtered.Items, all.Items[i])
		}
	}
	return filtered
}

func setupSpokeClientMock(hostnames []string) func() {
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
	// Two worker MCPs with different maxUnavailable, keyed by group/MCP name, so
	// GetMaxUnavailable resolves each worker pool's own rolling ceiling.
	workerR740MCP := &machineconfigv1.MachineConfigPool{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-r740-blue"},
		Spec: machineconfigv1.MachineConfigPoolSpec{
			MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 2},
		},
	}
	workerXR8620tMCP := &machineconfigv1.MachineConfigPool{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-xr8620t-blue"},
		Spec: machineconfigv1.MachineConfigPoolSpec{
			MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 3},
		},
	}
	var spokeObjects []client.Object
	spokeObjects = append(spokeObjects, masterMCP, workerR740MCP, workerXR8620tMCP)
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

	restoreClient := spokeclient.SetTestSpokeClientCreator(
		func(_ string, _ string, _ []byte, _ *runtime.Scheme) (client.Client, error) {
			return spokeClient, nil
		},
	)
	restoreClientset := spokeclient.SetTestSpokeClientsetCreator(
		func(_ string, _ string, _ []byte) (kubernetes.Interface, error) {
			return spokeClientset, nil
		},
	)
	return func() {
		restoreClient()
		restoreClientset()
		spokeclient.ClearCache()
	}
}

// failBMHDay2 simulates a BMH entering an error state during a day2 hardware configuration update.
// It simulates metal3 detecting HFS/HFC changes, waits for the controller to add the reboot
// annotation, transitions the BMH through Servicing and then to Error with an expired transient
// error timestamp, so the controller immediately treats it as a non-transient failure and marks
// the AllocatedNode as Failed.
func failBMHDay2(ctx context.Context, node *hwmgmtv1alpha1.AllocatedNode, bmh *metal3v1alpha1.BareMetalHost) {
	bmhKey := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
	nodeKey := types.NamespacedName{Name: node.Name, Namespace: node.Namespace}

	// Step 1: Simulate metal3 detecting HFS/HFC spec changes
	simulateHFSAndHFCChangeDetected(ctx, bmhKey)

	// Step 2: Wait for the controller to validate the changes and add the reboot annotation on the BMH
	waitForBMHRebootAnnotation(ctx, bmhKey)

	// Step 3: Transition BMH to Servicing and remove the reboot annotation (simulates metal3 processing the reboot annotation)
	patchBMHStatus(ctx, bmhKey, func(bmh *metal3v1alpha1.BareMetalHost) {
		bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusServicing
	})
	removeBMHRebootAnnotation(ctx, bmhKey)

	// Step 4: Wait for controller to set config-in-progress annotation on AllocatedNode
	Eventually(func() bool {
		n := &hwmgmtv1alpha1.AllocatedNode{}
		Expect(K8SClient.Get(ctx, nodeKey, n)).To(Succeed())
		return n.Annotations[hwmgrcontrollers.ConfigAnnotation] != ""
	}, mnoLongTimeout, mnoInterval).Should(BeTrue(),
		"AllocatedNode %s should have config annotation", node.Name)

	// Step 5: Transition BMH to Error with an expired transient error timestamp.
	// Pre-setting a timestamp older than ErrorRetryWindow (5min) ensures the controller
	// treats this as a non-transient failure and immediately marks the node as Failed.
	Expect(K8SClient.Get(ctx, bmhKey, bmh)).To(Succeed())
	patch := client.MergeFrom(bmh.DeepCopy())
	if bmh.Annotations == nil {
		bmh.Annotations = make(map[string]string)
	}
	bmh.Annotations[hwmgrcontrollers.BmhErrorTimestampAnnotation] = time.Now().Add(-10 * time.Minute).Format(time.RFC3339)
	Expect(K8SClient.Patch(ctx, bmh, patch)).To(Succeed())

	patchBMHStatus(ctx, bmhKey, func(bmh *metal3v1alpha1.BareMetalHost) {
		bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusError
		bmh.Status.ErrorMessage = "Firmware update failed"
		bmh.Status.ErrorType = metal3v1alpha1.ServicingError
	})
}

// completeBMHServicing simulates BMO completing a BIOS/firmware update on a BMH
// that is currently in Servicing state:
//  1. Update HFS/HFC status to match new profile values (simulates update completion)
//  2. Transition BMH to OK state (triggers handleNodeInProgressUpdate completion)
func completeBMHServicing(ctx context.Context, node *hwmgmtv1alpha1.AllocatedNode, bmh *metal3v1alpha1.BareMetalHost) {
	bmhKey := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
	nodeKey := types.NamespacedName{Name: node.Name, Namespace: node.Namespace}

	// Step 1.a: Update HFS status to match updated BIOS attributes (simulates BIOS update completion)
	hfs := &metal3v1alpha1.HostFirmwareSettings{}
	Expect(K8SClient.Get(ctx, bmhKey, hfs)).To(Succeed())
	schemaName := hfs.Status.FirmwareSchema.Name
	hfsPatch := client.MergeFrom(hfs.DeepCopy())
	updatedSettings := make(map[string]string)
	for k, v := range hfs.Spec.Settings {
		updatedSettings[k] = v.String()
	}
	hfs.Status.Settings = updatedSettings
	hfs.Status = testutils.UpdateHostFirmwareSettingsStatus(
		schemaName, hfs.Namespace, hfs.Status.Settings, metav1.ConditionTrue, metav1.ConditionFalse, hfs.Generation)
	Expect(K8SClient.Status().Patch(ctx, hfs, hfsPatch)).To(Succeed())

	// Step 1.b: Update HFC status components with new firmware versions (simulates firmware update completion)
	Expect(K8SClient.Get(ctx, nodeKey, node)).To(Succeed())
	hwProfile := &hwmgmtv1alpha1.HardwareProfile{}
	Expect(K8SClient.Get(ctx, types.NamespacedName{
		Name: node.Spec.HwProfile, Namespace: constants.DefaultNamespace,
	}, hwProfile)).To(Succeed())

	catalog := &hwmgmtv1alpha1.FirmwareCatalog{}
	Expect(K8SClient.Get(ctx, types.NamespacedName{
		Name: hwmgmtv1alpha1.FirmwareCatalogName, Namespace: constants.DefaultNamespace,
	}, catalog)).To(Succeed())
	imageMap := make(map[string]hwmgmtv1alpha1.FirmwareImage, len(catalog.Spec.Images))
	for _, img := range catalog.Spec.Images {
		imageMap[img.Name] = img
	}

	newComponents := []metal3v1alpha1.FirmwareComponentStatus{}
	if hwProfile.Spec.BiosFirmware != "" {
		img, ok := imageMap[hwProfile.Spec.BiosFirmware]
		Expect(ok).To(BeTrue(), "FirmwareCatalog missing entry %q referenced by HardwareProfile biosFirmware", hwProfile.Spec.BiosFirmware)
		newComponents = append(newComponents, metal3v1alpha1.FirmwareComponentStatus{
			Component: "bios", CurrentVersion: img.Version,
		})
	}
	if hwProfile.Spec.BmcFirmware != "" {
		img, ok := imageMap[hwProfile.Spec.BmcFirmware]
		Expect(ok).To(BeTrue(), "FirmwareCatalog missing entry %q referenced by HardwareProfile bmcFirmware", hwProfile.Spec.BmcFirmware)
		newComponents = append(newComponents, metal3v1alpha1.FirmwareComponentStatus{
			Component: "bmc", CurrentVersion: img.Version,
		})
	}
	for i, nicName := range hwProfile.Spec.NicFirmware {
		img, ok := imageMap[nicName]
		Expect(ok).To(BeTrue(), "FirmwareCatalog missing entry %q referenced by HardwareProfile nicFirmware[%d]", nicName, i)
		if img.Version != "" {
			newComponents = append(newComponents, metal3v1alpha1.FirmwareComponentStatus{
				Component: fmt.Sprintf("nic:%d", i), CurrentVersion: img.Version,
			})
		}
	}
	hfc := &metal3v1alpha1.HostFirmwareComponents{}
	Expect(K8SClient.Get(ctx, bmhKey, hfc)).To(Succeed())
	hfcPatch := client.MergeFrom(hfc.DeepCopy())
	hfc.Status.Components = newComponents
	hfc.Status = testutils.UpdateHostFirmwareComponentsStatus(
		hfc.Name, hfc.Namespace, hfc.Status.Components, metav1.ConditionTrue, metav1.ConditionFalse, hfc.Generation)
	Expect(K8SClient.Status().Patch(ctx, hfc, hfcPatch)).To(Succeed())

	// Step 2: Transition BMH back to OK state (triggers handleNodeInProgressUpdate completion)
	patchBMHStatus(ctx, bmhKey, func(bmh *metal3v1alpha1.BareMetalHost) {
		bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusOK
		bmh.Status.ErrorMessage = ""
		bmh.Status.ErrorType = ""
	})
}

// simulateHFSAndHFCChangeDetected simulates metal3 detecting HFS/HFC spec changes by setting
// ChangeDetected=True and UpdatesRequired=True on the status conditions of the HFS and HFC resources.
func simulateHFSAndHFCChangeDetected(ctx context.Context, bmhKey types.NamespacedName) {
	hfs := &metal3v1alpha1.HostFirmwareSettings{}
	Expect(K8SClient.Get(ctx, bmhKey, hfs)).To(Succeed())
	schemaName := hfs.Status.FirmwareSchema.Name
	hfsPatch := client.MergeFrom(hfs.DeepCopy())
	hfs.Status = testutils.UpdateHostFirmwareSettingsStatus(
		schemaName, hfs.Namespace, hfs.Status.Settings, metav1.ConditionTrue, metav1.ConditionTrue, hfs.Generation)
	Expect(K8SClient.Status().Patch(ctx, hfs, hfsPatch)).To(Succeed())

	hfc := &metal3v1alpha1.HostFirmwareComponents{}
	Expect(K8SClient.Get(ctx, bmhKey, hfc)).To(Succeed())
	hfcPatch := client.MergeFrom(hfc.DeepCopy())
	hfc.Status = testutils.UpdateHostFirmwareComponentsStatus(
		hfc.Name, hfc.Namespace, hfc.Status.Components, metav1.ConditionTrue, metav1.ConditionTrue, hfc.Generation)
	Expect(K8SClient.Status().Patch(ctx, hfc, hfcPatch)).To(Succeed())
}

// isHFSChangeDetected checks whether the HFS FirmwareSettingsChangeDetected
// condition is True, indicating that simulateHFSAndHFCChangeDetected has
// already been called for this BMH.
func isHFSChangeDetected(ctx context.Context, bmhKey types.NamespacedName) bool {
	hfs := &metal3v1alpha1.HostFirmwareSettings{}
	Expect(K8SClient.Get(ctx, bmhKey, hfs)).To(Succeed())
	cond := meta.FindStatusCondition(hfs.Status.Conditions, string(metal3v1alpha1.FirmwareSettingsChangeDetected))
	return cond != nil && cond.Status == metav1.ConditionTrue
}

// waitForBMHRebootAnnotation polls until the controller adds the reboot.metal3.io
// annotation on the BMH, indicating it has validated HFS/HFC changes and is ready
// for BMO to process the reboot.
func waitForBMHRebootAnnotation(ctx context.Context, bmhKey types.NamespacedName) {
	Eventually(func() bool {
		bmh := &metal3v1alpha1.BareMetalHost{}
		Expect(K8SClient.Get(ctx, bmhKey, bmh)).To(Succeed())
		_, exists := bmh.Annotations[hwmgrcontrollers.BmhRebootAnnotation]
		return exists
	}, mnoLongTimeout, mnoInterval).Should(BeTrue(),
		"BMH %s should have reboot annotation", bmhKey.Name)
}

// removeBMHRebootAnnotation removes the reboot.metal3.io annotation from the BMH
// using a merge-patch, matching real BMO behavior after processing a reboot.
func removeBMHRebootAnnotation(ctx context.Context, bmhKey types.NamespacedName) {
	bmh := &metal3v1alpha1.BareMetalHost{}
	Expect(K8SClient.Get(ctx, bmhKey, bmh)).To(Succeed())
	patch := client.MergeFrom(bmh.DeepCopy())
	delete(bmh.Annotations, hwmgrcontrollers.BmhRebootAnnotation)
	Expect(K8SClient.Patch(ctx, bmh, patch)).To(Succeed())
}

// patchBMHStatus fetches the latest BMH, applies the given mutation to its status,
// and sends a merge-patch.
func patchBMHStatus(ctx context.Context, bmhKey types.NamespacedName, mutateFn func(*metal3v1alpha1.BareMetalHost)) {
	bmh := &metal3v1alpha1.BareMetalHost{}
	Expect(K8SClient.Get(ctx, bmhKey, bmh)).To(Succeed())
	patch := client.MergeFrom(bmh.DeepCopy())
	mutateFn(bmh)
	Expect(K8SClient.Status().Patch(ctx, bmh, patch)).To(Succeed())
}

// Masters and the R740 worker pool share the same server-type (R740) and are
// distinguished by colour (green vs blue); the two worker pools share the same
// colour (blue) and are distinguished by server-type (R740 vs XR8620t).
func mnoBMHsTwoWorkerPools(masterCount, r740WorkerCount, xr8620tWorkerCount int) []testutils.BMHData {
	var bmhs []testutils.BMHData
	for i := 1; i <= masterCount; i++ {
		bmhs = append(bmhs, testutils.BMHData{
			Name:           fmt.Sprintf("test-master%d", i),
			Namespace:      "dell-r740-pool",
			MacAddress:     fmt.Sprintf("aa:bb:cc:11:00:%02x", i),
			BmcAddress:     fmt.Sprintf("redfish://192.168.1.%d/redfish/v1/Systems/1", 100+i),
			ServerType:     "R740",
			Colour:         "green",
			ResourcePoolId: "dell-r740-pool",
		})
	}
	for i := 1; i <= r740WorkerCount; i++ {
		bmhs = append(bmhs, testutils.BMHData{
			Name:           fmt.Sprintf("test-worker-r740-%d", i),
			Namespace:      "dell-r740-pool",
			MacAddress:     fmt.Sprintf("aa:bb:cc:33:00:%02x", i),
			BmcAddress:     fmt.Sprintf("redfish://192.168.3.%d/redfish/v1/Systems/1", 100+i),
			ServerType:     "R740",
			Colour:         "blue",
			ResourcePoolId: "dell-r740-pool",
		})
	}
	for i := 1; i <= xr8620tWorkerCount; i++ {
		bmhs = append(bmhs, testutils.BMHData{
			Name:           fmt.Sprintf("test-worker-xr8620t-%d", i),
			Namespace:      "dell-xr8620t-pool",
			MacAddress:     fmt.Sprintf("aa:bb:cc:22:00:%02x", i),
			BmcAddress:     fmt.Sprintf("redfish://192.168.2.%d/redfish/v1/Systems/1", 100+i),
			ServerType:     "XR8620t",
			Colour:         "blue",
			ResourcePoolId: "dell-xr8620t-pool",
		})
	}
	return bmhs
}
