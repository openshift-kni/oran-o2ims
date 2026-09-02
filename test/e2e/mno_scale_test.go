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
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	testutils "github.com/openshift-kni/oran-o2ims/test/utils"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
)

var _ = Describe("MNO Scale-Out test", Ordered, Label("mno-scale-out"), func() {
	const (
		timeout            = time.Minute * 2
		interval           = time.Second * 3
		master             = "master"
		workerR740         = "worker-dell-r740"
		workerXR8620t      = "worker-dell-xr8620t"
		masterCount        = 3
		r740WorkerCount    = 2
		xr8620tWorkerCount = 1

		scalePoolR740   = "scale-r740-pool"
		scalePoolXR8620 = "scale-xr8620t-pool"
	)

	var (
		testCtx     context.Context
		clusterName = "scale-cluster"
		ctNamespace = "scale-4-20-16"
		prName      string

		pr                        *provisioningv1alpha1.ProvisioningRequest
		nar                       *hwmgmtv1alpha1.NodeAllocationRequest
		initialAllocatedNodeNames map[string]bool
		createdClusterImageSet    bool
	)

	testCtx = context.Background()

	resourceDir := "../resources/mno_scale"

	cmYamls := []string{
		resourceDir + "/clusterinstance-defaults-v1.yaml",
		resourceDir + "/policytemplate-defaults-v1.yaml",
	}

	BeforeAll(func() {
		pr = &provisioningv1alpha1.ProvisioningRequest{}
		nar = &hwmgmtv1alpha1.NodeAllocationRequest{}

		By("Creating namespaces")
		for _, ns := range []string{ctNamespace, "ztp-" + ctNamespace, scalePoolR740, scalePoolXR8620} {
			err := K8SClient.Create(testCtx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: ns},
			})
			if err != nil && !errors.IsAlreadyExists(err) {
				Expect(err).ToNot(HaveOccurred())
			}
		}

		By("Creating ConfigMaps")
		for _, yaml := range cmYamls {
			cm, err := testutils.LoadYAML[corev1.ConfigMap](yaml)
			Expect(err).ToNot(HaveOccurred())
			Expect(K8SClient.Create(testCtx, cm)).To(Succeed())
		}

		By("Creating ClusterTemplate and supporting resources")
		ct, err := testutils.LoadYAML[provisioningv1alpha1.ClusterTemplate](resourceDir + "/ct-scale.yaml")
		Expect(err).ToNot(HaveOccurred())
		Expect(K8SClient.Create(testCtx, ct)).To(Succeed())

		pullSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "pull-secret", Namespace: ctNamespace},
			Data:       map[string][]byte{".dockerconfigjson": []byte(testutils.TestSecretDataStr)},
			Type:       corev1.SecretTypeDockerConfigJson,
		}
		clusterImageSet := &hivev1.ClusterImageSet{
			ObjectMeta: metav1.ObjectMeta{Name: "4.20.16"},
			Spec: hivev1.ClusterImageSetSpec{
				ReleaseImage: "quay.io/openshift-release-dev/ocp-release:4.20.16-x86_64",
			},
		}
		extraManifests := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "clustertemplate-sample.v1.0.0-extramanifests",
				Namespace: ctNamespace,
			},
			Data: map[string]string{},
		}
		for _, r := range []client.Object{pullSecret, extraManifests} {
			if err := K8SClient.Create(testCtx, r); err != nil && !errors.IsAlreadyExists(err) {
				Expect(err).ToNot(HaveOccurred())
			}
		}
		if err := K8SClient.Create(testCtx, clusterImageSet); err != nil {
			if !errors.IsAlreadyExists(err) {
				Expect(err).ToNot(HaveOccurred())
			}
		} else {
			createdClusterImageSet = true
		}

		By("Creating 8 BMHs (3 masters + 2 r740 workers + 1 xr8620t worker + 1 extra xr8620t and 1 extra r740 for scale-out)")
		bmhList := scaleBMHs(masterCount, r740WorkerCount+1, xr8620tWorkerCount+1)
		for _, bmhData := range bmhList {
			bmh := testutils.CreateBareMetalHost(bmhData)
			bmcSecret := testutils.CreateBMCSecret(bmhData.Name)
			Expect(K8SClient.Create(testCtx, bmh)).To(Succeed())
			Expect(K8SClient.Create(testCtx, bmcSecret)).To(Succeed())

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

			hfs := testutils.CreateHostFirmwareSettings(bmhData.Name, bmhData.Namespace)
			Expect(K8SClient.Create(testCtx, hfs)).To(Succeed())
			hfs.Status = testutils.UpdateHostFirmwareSettingsStatus(
				fmt.Sprintf("schema-%s", bmhData.Name), bmhData.Namespace,
				map[string]string{}, metav1.ConditionTrue, metav1.ConditionFalse, hfs.Generation)
			Expect(K8SClient.Status().Update(testCtx, hfs)).To(Succeed())

			hfc := testutils.CreateHostFirmwareComponents(bmhData.Name, bmhData.Namespace)
			Expect(K8SClient.Create(testCtx, hfc)).To(Succeed())
			hfc.Status = testutils.UpdateHostFirmwareComponentsStatus(
				bmhData.Name, bmhData.Namespace,
				[]metal3v1alpha1.FirmwareComponentStatus{
					{Component: "bios", CurrentVersion: "2.0.0"},
					{Component: "bmc", CurrentVersion: "6.0.0"},
				},
				metav1.ConditionTrue, metav1.ConditionFalse, hfc.Generation)
			Expect(K8SClient.Status().Update(testCtx, hfc)).To(Succeed())
		}

		poolSel := labels.NewSelector()
		req, reqErr := labels.NewRequirement(constants.LabelResourcePoolName, selection.In,
			[]string{scalePoolR740, scalePoolXR8620})
		Expect(reqErr).ToNot(HaveOccurred())
		poolSel = poolSel.Add(*req)

		By("Waiting for all 8 BMHs to be visible")
		Eventually(func() int {
			bmhListResult := &metal3v1alpha1.BareMetalHostList{}
			Expect(K8SClient.List(testCtx, bmhListResult,
				client.MatchingLabelsSelector{Selector: poolSel})).To(Succeed())
			available := 0
			for _, b := range bmhListResult.Items {
				if b.Status.Provisioning.State == metal3v1alpha1.StateAvailable {
					available++
				}
			}
			return available
		}, timeout, interval).Should(Equal(masterCount + r740WorkerCount + 1 + xr8620tWorkerCount + 1))

		By("Waiting for ClusterTemplate reconciliation")
		Eventually(func() bool {
			newct := &provisioningv1alpha1.ClusterTemplate{}
			Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(ct), newct)).To(Succeed())
			return newct.Status.Conditions != nil
		}, timeout, interval).Should(BeTrue())

		By("Creating initial ProvisioningRequest with 3 masters + 2 r740 workers + 1 xr8620t worker")
		prFromYAML, err := testutils.LoadYAML[provisioningv1alpha1.ProvisioningRequest](resourceDir + "/pr-scale-initial.yaml")
		Expect(err).ToNot(HaveOccurred())
		prName = prFromYAML.Name
		Expect(K8SClient.Create(testCtx, prFromYAML)).To(Succeed())

		By("Waiting for NAR creation")
		Eventually(func() error {
			return K8SClient.Get(testCtx, types.NamespacedName{
				Name: prName, Namespace: constants.DefaultNamespace}, nar)
		}, timeout, interval).Should(Succeed())

		By("Verifying initial NAR NodeGroup sizes")
		Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(nar), nar)).To(Succeed())
		verifyNodeGroupSizes(nar, map[string]int{
			master:        masterCount,
			workerR740:    r740WorkerCount,
			workerXR8620t: xr8620tWorkerCount,
		})

		By("Waiting for all 6 AllocatedNodes to be created")
		Eventually(func() int {
			return len(testNonCachingListAllocatedNodesForNAR(testCtx, prName).Items)
		}, timeout, interval).Should(Equal(masterCount + r740WorkerCount + xr8620tWorkerCount))

		By("Waiting for NAR Provisioned=True")
		Eventually(func() bool {
			Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(nar), nar)).To(Succeed())
			cond := meta.FindStatusCondition(nar.Status.Conditions, string(hwmgmtv1alpha1.Provisioned))
			return cond != nil && cond.Status == metav1.ConditionTrue
		}, timeout, interval).Should(BeTrue())

		By("Waiting for PR HardwareProvisioned=True")
		Eventually(func() bool {
			Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
			cond := meta.FindStatusCondition(pr.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned))
			return cond != nil && cond.Status == metav1.ConditionTrue
		}, timeout, interval).Should(BeTrue())

		By("Capturing initial AllocatedNode names before scale-out")
		initialNodes := testNonCachingListAllocatedNodesForNAR(testCtx, prName)
		initialAllocatedNodeNames = make(map[string]bool, len(initialNodes.Items))
		for _, n := range initialNodes.Items {
			initialAllocatedNodeNames[n.Name] = true
		}

		By("Simulating AllocatedNodeHostMap on PR")
		Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
		nodeList := testNonCachingListAllocatedNodesForNAR(testCtx, prName)
		hostMap := make(map[string]string)
		masterIdx, r740Idx, xr8620tIdx := 1, 1, 1
		for _, node := range nodeList.Items {
			hostname := ""
			switch node.Spec.GroupName {
			case master:
				hostname = fmt.Sprintf("master-%d.%s.example.com", masterIdx, clusterName)
				masterIdx++
			case workerR740:
				hostname = fmt.Sprintf("worker-r740-%d.%s.example.com", r740Idx, clusterName)
				r740Idx++
			case workerXR8620t:
				hostname = fmt.Sprintf("worker-xr8620-%d.%s.example.com", xr8620tIdx, clusterName)
				xr8620tIdx++
			default:
				Fail(fmt.Sprintf("unexpected AllocatedNode group %q", node.Spec.GroupName))
			}
			hostMap[node.Name] = hostname
		}
		pr.Status.Extensions.AllocatedNodeHostMap = hostMap
		Expect(K8SClient.Status().Update(testCtx, pr)).To(Succeed())

		By("Simulating ClusterProvisioned=Completed on PR")
		Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
		meta.SetStatusCondition(&pr.Status.Conditions, metav1.Condition{
			Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
			Status: metav1.ConditionTrue,
			Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
		})
		pr.Status.ProvisioningStatus.ProvisioningPhase = provisioningv1alpha1.StateFulfilled
		Expect(K8SClient.Status().Update(testCtx, pr)).To(Succeed())
	})

	AfterAll(func() {
		By("Cleaning up scale test resources")
		// Delete PR (strip finalizer first)
		prObj := &provisioningv1alpha1.ProvisioningRequest{}
		if err := K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, prObj); err == nil {
			prObj.Finalizers = nil
			_ = K8SClient.Update(testCtx, prObj)
			_ = K8SClient.Delete(testCtx, prObj)
		}

		// Delete NAR (strip finalizer)
		narObj := &hwmgmtv1alpha1.NodeAllocationRequest{}
		if err := K8SClient.Get(testCtx, types.NamespacedName{
			Name: prName, Namespace: constants.DefaultNamespace,
		}, narObj); err == nil {
			narObj.Finalizers = nil
			_ = K8SClient.Update(testCtx, narObj)
			_ = K8SClient.Delete(testCtx, narObj)
		}

		// Delete AllocatedNodes (strip finalizers first to avoid leftovers)
		anList := testNonCachingListAllocatedNodesForNAR(testCtx, prName)
		for i := range anList.Items {
			anList.Items[i].Finalizers = nil
			_ = K8SClient.Update(testCtx, &anList.Items[i])
			_ = K8SClient.Delete(testCtx, &anList.Items[i])
		}

		// Delete BMHs and associated resources
		bmhList := scaleBMHs(masterCount, r740WorkerCount+1, xr8620tWorkerCount+1)
		for _, bmhData := range bmhList {
			for _, obj := range []client.Object{
				&metal3v1alpha1.BareMetalHost{ObjectMeta: metav1.ObjectMeta{Name: bmhData.Name, Namespace: bmhData.Namespace}},
				&metal3v1alpha1.HardwareData{ObjectMeta: metav1.ObjectMeta{Name: bmhData.Name, Namespace: bmhData.Namespace}},
				&metal3v1alpha1.HostFirmwareSettings{ObjectMeta: metav1.ObjectMeta{Name: bmhData.Name, Namespace: bmhData.Namespace}},
				&metal3v1alpha1.HostFirmwareComponents{ObjectMeta: metav1.ObjectMeta{Name: bmhData.Name, Namespace: bmhData.Namespace}},
				&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-bmc-secret", bmhData.Name), Namespace: constants.DefaultNamespace}},
			} {
				existing := obj.DeepCopyObject().(client.Object)
				if err := K8SClient.Get(testCtx, client.ObjectKeyFromObject(obj), existing); err == nil {
					_ = K8SClient.Delete(testCtx, existing)
				}
			}
		}

		// Delete ClusterTemplate
		ct := &provisioningv1alpha1.ClusterTemplate{}
		if err := K8SClient.Get(testCtx, types.NamespacedName{
			Name: "scale-test.v4-20-16-v1", Namespace: ctNamespace,
		}, ct); err == nil {
			_ = K8SClient.Delete(testCtx, ct)
		}

		// Delete ConfigMaps
		for _, cmName := range []string{"clusterinstance-defaults-v1", "policytemplate-defaults-v1", "clustertemplate-sample.v1.0.0-extramanifests"} {
			cm := &corev1.ConfigMap{}
			if err := K8SClient.Get(testCtx, types.NamespacedName{Name: cmName, Namespace: ctNamespace}, cm); err == nil {
				_ = K8SClient.Delete(testCtx, cm)
			}
		}

		// Delete ClusterImageSet only if this test created it
		if createdClusterImageSet {
			cis := &hivev1.ClusterImageSet{}
			if err := K8SClient.Get(testCtx, types.NamespacedName{Name: "4.20.16"}, cis); err == nil {
				_ = K8SClient.Delete(testCtx, cis)
			}
		}
	})

	Describe("Scale-out: add a worker to each custom pool", func() {
		It("should increase NAR Size for both worker groups", func() {
			By("Updating PR to append a host under worker-dell-r740 and worker-dell-xr8620t")
			Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())

			var templateParams map[string]any
			Expect(json.Unmarshal(pr.Spec.TemplateParameters.Raw, &templateParams)).To(Succeed())
			ciParams := templateParams["clusterInstanceParameters"].(map[string]any)
			nodeGroups := ciParams["nodeGroups"].([]any)

			appendWorkerHost(nodeGroups, workerR740,
				fmt.Sprintf("worker-r740-3.%s.example.com", clusterName), "192.0.2.52/24")
			appendWorkerHost(nodeGroups, workerXR8620t,
				fmt.Sprintf("worker-xr8620-2.%s.example.com", clusterName), "192.0.2.57/24")

			ciParams["nodeGroups"] = nodeGroups
			templateParams["clusterInstanceParameters"] = ciParams

			updatedParams, err := json.Marshal(templateParams)
			Expect(err).ToNot(HaveOccurred())
			pr.Spec.TemplateParameters.Raw = updatedParams
			Expect(K8SClient.Update(testCtx, pr)).To(Succeed())

			By("Verifying NAR NodeGroup sizes after scale-out")
			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(nar), nar)).To(Succeed())
				sizes := nodeGroupSizeMap(nar)
				return sizes[workerR740] == r740WorkerCount+1 &&
					sizes[workerXR8620t] == xr8620tWorkerCount+1 &&
					sizes[master] == masterCount
			}, timeout, interval).Should(BeTrue(),
				"both worker groups should grow by 1; master Size should remain %d", masterCount)
		})

		It("should create a new AllocatedNode in each scaled worker group", func() {
			By("Waiting for 8 AllocatedNodes (3 masters + 3 r740 workers + 2 xr8620t workers)")
			Eventually(func() int {
				return len(testNonCachingListAllocatedNodesForNAR(testCtx, prName).Items)
			}, timeout, interval).Should(Equal(masterCount + r740WorkerCount + 1 + xr8620tWorkerCount + 1))

			By("Verifying original nodes are preserved and each worker group gained one node")
			allNodes := testNonCachingListAllocatedNodesForNAR(testCtx, prName)
			newByGroup := map[string]int{}
			for _, n := range allNodes.Items {
				if !initialAllocatedNodeNames[n.Name] {
					newByGroup[n.Spec.GroupName]++
				}
			}
			Expect(newByGroup).To(Equal(map[string]int{
				workerR740:    1,
				workerXR8620t: 1,
			}))
		})
	})

	Describe("Scale-in: remove a worker from each custom pool", func() {
		It("should decrease NAR Size for both worker groups", func() {
			By("Updating PR to remove the added host from worker-dell-r740 and worker-dell-xr8620t")
			Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())

			var templateParams map[string]any
			Expect(json.Unmarshal(pr.Spec.TemplateParameters.Raw, &templateParams)).To(Succeed())
			ciParams := templateParams["clusterInstanceParameters"].(map[string]any)
			nodeGroups := ciParams["nodeGroups"].([]any)

			truncateWorkerHosts(nodeGroups, workerR740, r740WorkerCount)
			truncateWorkerHosts(nodeGroups, workerXR8620t, xr8620tWorkerCount)

			ciParams["nodeGroups"] = nodeGroups
			templateParams["clusterInstanceParameters"] = ciParams

			updatedParams, err := json.Marshal(templateParams)
			Expect(err).ToNot(HaveOccurred())
			pr.Spec.TemplateParameters.Raw = updatedParams
			Expect(K8SClient.Update(testCtx, pr)).To(Succeed())

			By("Verifying NAR NodeGroup sizes after scale-in")
			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(nar), nar)).To(Succeed())
				sizes := nodeGroupSizeMap(nar)
				return sizes[workerR740] == r740WorkerCount &&
					sizes[workerXR8620t] == xr8620tWorkerCount &&
					sizes[master] == masterCount
			}, timeout, interval).Should(BeTrue(),
				"both worker groups should return to the initial size; master Size should remain %d", masterCount)
		})

		// NOTE: AllocatedNode deletion is gated on spoke node removal
		// (checkRemovedNodesGone), which requires a real spoke cluster.
		// This cannot be verified in the e2e test environment. Lab testing
		// validates the full flow including AllocatedNode cleanup.
	})
})

// findNodeGroupByName returns the nodeGroup map with the given name from a
// clusterInstanceParameters.nodeGroups slice.
func findNodeGroupByName(nodeGroups []any, name string) map[string]any {
	for _, ng := range nodeGroups {
		group, ok := ng.(map[string]any)
		if !ok {
			continue
		}
		if group["name"] == name {
			return group
		}
	}
	return nil
}

func appendWorkerHost(nodeGroups []any, groupName, hostName, ipv4 string) {
	group := findNodeGroupByName(nodeGroups, groupName)
	Expect(group).ToNot(BeNil(), "nodeGroup %q should exist", groupName)
	nodes, ok := group["nodes"].([]any)
	Expect(ok).To(BeTrue(), "nodeGroup %q nodes should be a list", groupName)
	group["nodes"] = append(nodes, map[string]any{
		"hostName": hostName,
		"nodeNetwork": map[string]any{
			"interfaces": []any{
				map[string]any{
					"name": "eno1",
					"addresses": map[string]any{
						"ipv4": []any{ipv4},
					},
				},
			},
		},
	})
}

func truncateWorkerHosts(nodeGroups []any, groupName string, keep int) {
	group := findNodeGroupByName(nodeGroups, groupName)
	Expect(group).ToNot(BeNil(), "nodeGroup %q should exist", groupName)
	nodes, ok := group["nodes"].([]any)
	Expect(ok).To(BeTrue(), "nodeGroup %q nodes should be a list", groupName)
	Expect(len(nodes)).To(BeNumerically(">=", keep),
		"nodeGroup %q should have at least %d hosts", groupName, keep)
	group["nodes"] = nodes[:keep]
}

func scaleBMHs(masterCount, r740WorkerCount, xr8620tWorkerCount int) []testutils.BMHData {
	var bmhs []testutils.BMHData
	for i := 1; i <= masterCount; i++ {
		bmhs = append(bmhs, testutils.BMHData{
			Name:           fmt.Sprintf("scale-master%d", i),
			Namespace:      "scale-r740-pool",
			MacAddress:     fmt.Sprintf("aa:bb:cc:33:00:%02x", i),
			BmcAddress:     fmt.Sprintf("redfish://192.168.3.%d/redfish/v1/Systems/1", 100+i),
			ServerType:     "R740",
			Colour:         "green",
			ResourcePoolId: "scale-r740-pool",
		})
	}
	for i := 1; i <= r740WorkerCount; i++ {
		bmhs = append(bmhs, testutils.BMHData{
			Name:           fmt.Sprintf("scale-worker-r740-%d", i),
			Namespace:      "scale-r740-pool",
			MacAddress:     fmt.Sprintf("aa:bb:cc:44:00:%02x", i),
			BmcAddress:     fmt.Sprintf("redfish://192.168.4.%d/redfish/v1/Systems/1", 100+i),
			ServerType:     "R740",
			Colour:         "blue",
			ResourcePoolId: "scale-r740-pool",
		})
	}
	for i := 1; i <= xr8620tWorkerCount; i++ {
		bmhs = append(bmhs, testutils.BMHData{
			Name:           fmt.Sprintf("scale-worker-xr8620t-%d", i),
			Namespace:      "scale-xr8620t-pool",
			MacAddress:     fmt.Sprintf("aa:bb:cc:55:00:%02x", i),
			BmcAddress:     fmt.Sprintf("redfish://192.168.5.%d/redfish/v1/Systems/1", 100+i),
			ServerType:     "XR8620t",
			Colour:         "blue",
			ResourcePoolId: "scale-xr8620t-pool",
		})
	}
	return bmhs
}

func nodeGroupSizeMap(nar *hwmgmtv1alpha1.NodeAllocationRequest) map[string]int {
	sizes := make(map[string]int)
	for _, ng := range nar.Spec.NodeGroup {
		sizes[ng.NodeGroupData.Name] = ng.Size
	}
	return sizes
}

func verifyNodeGroupSizes(nar *hwmgmtv1alpha1.NodeAllocationRequest, expected map[string]int) {
	sizes := nodeGroupSizeMap(nar)
	for name, expectedSize := range expected {
		Expect(sizes).To(HaveKey(name), "NodeGroup %q should exist", name)
		Expect(sizes[name]).To(Equal(expectedSize),
			"NodeGroup.Size for %q should be %d", name, expectedSize)
	}
}
