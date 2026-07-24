/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllersE2Etest

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	provisioningcontrollers "github.com/openshift-kni/oran-o2ims/internal/controllers"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils/spokeclient"
	testutils "github.com/openshift-kni/oran-o2ims/test/utils"
	configv1 "github.com/openshift/api/config/v1"
	mcfgv1 "github.com/openshift/api/machineconfiguration/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
)

const (
	cvUpgradeTimeout  = time.Minute * 2
	cvUpgradeInterval = time.Second * 3
	prName            = "88744070-717a-4305-8461-796244098339"
	clusterName       = "std-du-cluster"
)

// --- Test suite ---

var _ = Describe("MNO Standard ClusterVersion Upgrade", Ordered, Label("mno-cv-upgrade"), func() {
	const (
		timeout  = cvUpgradeTimeout
		interval = cvUpgradeInterval

		ctName     = "std-du"
		ctVersion1 = "v4-19-3-v1"
		ctRelease1 = "4.19.3"
		ctVersion2 = "v4-20-5-v1"
		ctRelease2 = "4.20.5"
		ctVersion3 = "v4-21-2-v1"
		ctRelease3 = "4.21.2"
		ctVersion4 = "v4-22-4-v1"
		ctRelease4 = "4.22.4"

		eusIntermediateVersion = "4.21.7"

		ctNamespace = "std-ran-v4-19-3"

		resourceDir = "../resources/mno_cv_upgrade"
	)

	var (
		testCtx     context.Context
		spokeClient client.Client
		pr          *provisioningv1alpha1.ProvisioningRequest
	)

	testCtx = context.Background()

	cmYamls := []string{
		filepath.Join(resourceDir, "clusterinstance-defaults-v1.yaml"),
		filepath.Join(resourceDir, "policytemplate-defaults-v1.yaml"),
	}

	BeforeAll(func() {
		pr = &provisioningv1alpha1.ProvisioningRequest{}

		By("Creating namespaces")
		for _, ns := range []string{ctNamespace, "ztp-" + ctNamespace, "dell-r740-pool", "dell-xr8620t-pool"} {
			Expect(client.IgnoreAlreadyExists(K8SClient.Create(testCtx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: ns},
			}))).To(Succeed())
		}

		By("Creating ClusterTemplates and supporting resources")
		// Create default ConfigMaps
		for _, yaml := range cmYamls {
			cm, err := testutils.LoadYAML[corev1.ConfigMap](yaml)
			Expect(err).ToNot(HaveOccurred())
			Expect(K8SClient.Create(testCtx, cm)).To(Succeed())
		}

		// Create ClusterImageSets for clustertemplate validation
		for _, cis := range []string{ctRelease1, ctRelease2, ctRelease3, ctRelease4} {
			clusterImageSet := &hivev1.ClusterImageSet{
				ObjectMeta: metav1.ObjectMeta{Name: cis},
				Spec:       hivev1.ClusterImageSetSpec{ReleaseImage: "quay.io/openshift-release-dev/ocp-release:" + cis + "-x86_64"},
			}
			Expect(K8SClient.Create(testCtx, clusterImageSet)).To(Succeed())
		}

		otherResources := []client.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "pull-secret", Namespace: ctNamespace},
				Data:       map[string][]byte{".dockerconfigjson": []byte(testutils.TestSecretDataStr)},
				Type:       corev1.SecretTypeDockerConfigJson,
			},
			// Extra manifests ConfigMap referenced by mno/clusterinstance-defaults-v1.yaml
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "clustertemplate-sample.v1.0.0-extramanifests",
					Namespace: ctNamespace,
				},
				Data: map[string]string{},
			},
			&clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   clusterName,
					Labels: map[string]string{"openshiftVersion": ctRelease1},
				},
				Spec: clusterv1.ManagedClusterSpec{
					ManagedClusterClientConfigs: []clusterv1.ClientConfig{
						{URL: "https://api." + clusterName + ".example.com:6443"},
					},
				},
			},
		}
		for _, obj := range otherResources {
			err := K8SClient.Create(testCtx, obj)
			if err != nil && !errors.IsAlreadyExists(err) {
				Expect(err).ToNot(HaveOccurred())
			}
		}

		// Create base ClusterTemplate
		ctV1, err := testutils.LoadYAML[provisioningv1alpha1.ClusterTemplate](
			filepath.Join(resourceDir, "ct-std-du-v1.yaml"))
		Expect(err).ToNot(HaveOccurred())
		Expect(K8SClient.Create(testCtx, ctV1)).To(Succeed())

		// Create upgrade ClusterTemplates
		createCIDefaultsCM(testCtx, K8SClient, resourceDir, "clusterinstance-defaults-v2", ctRelease1, ctRelease2)
		createCIDefaultsCM(testCtx, K8SClient, resourceDir, "clusterinstance-defaults-v3", ctRelease1, ctRelease3)
		createCIDefaultsCM(testCtx, K8SClient, resourceDir, "clusterinstance-defaults-v4", ctRelease1, ctRelease4)
		createUpgradeCT(testCtx, K8SClient, resourceDir, ctName, ctVersion2, ctRelease2, "clusterinstance-defaults-v2")
		createUpgradeCT(testCtx, K8SClient, resourceDir, ctName, ctVersion3, ctRelease3, "clusterinstance-defaults-v3")
		createUpgradeCT(testCtx, K8SClient, resourceDir, ctName, ctVersion4, ctRelease4, "clusterinstance-defaults-v4")

		By("Creating BMHs resources")
		bmhList := testutils.MnoBMHs(3, 2)
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
		}

		By("Creating ProvisioningRequest referencing CT v4-19-3-v1")
		prObj, err := testutils.LoadYAML[provisioningv1alpha1.ProvisioningRequest](
			filepath.Join(resourceDir, "pr-std.yaml"))
		Expect(err).ToNot(HaveOccurred())
		Expect(K8SClient.Create(testCtx, prObj)).To(Succeed())

		By("Waiting for ClusterInstance creation and simulating provisioning completion")
		ci := &siteconfig.ClusterInstance{}
		Eventually(func() error {
			return K8SClient.Get(testCtx, types.NamespacedName{Name: clusterName, Namespace: clusterName}, ci)
		}, timeout, interval).Should(Succeed(), "ClusterInstance should be created")

		Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: clusterName, Namespace: clusterName}, ci)).To(Succeed())
		ci.Status.Conditions = []metav1.Condition{
			{Type: string(siteconfig.ClusterInstanceValidated), Status: metav1.ConditionTrue, Reason: "Completed", Message: "Validated", LastTransitionTime: metav1.Now()},
			{Type: string(siteconfig.RenderedTemplates), Status: metav1.ConditionTrue, Reason: "Completed", Message: "Rendered", LastTransitionTime: metav1.Now()},
			{Type: string(siteconfig.RenderedTemplatesValidated), Status: metav1.ConditionTrue, Reason: "Completed", Message: "Validated", LastTransitionTime: metav1.Now()},
			{Type: string(siteconfig.RenderedTemplatesApplied), Status: metav1.ConditionTrue, Reason: "Completed", Message: "Applied", LastTransitionTime: metav1.Now()},
			{Type: string(siteconfig.ClusterProvisioned), Status: metav1.ConditionTrue, Reason: "Completed", Message: "Provisioned", LastTransitionTime: metav1.Now()},
		}
		Expect(K8SClient.Status().Update(testCtx, ci)).To(Succeed())

		By("Waiting for PR to reach Fulfilled")
		Eventually(func() bool {
			Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
			return pr.Status.ProvisioningStatus.ProvisioningPhase == provisioningv1alpha1.StateFulfilled
		}, timeout, interval).Should(BeTrue(), "PR should reach Fulfilled")

		By("Creating ManagedClusterAddOn for the cluster")
		Expect(K8SClient.Create(testCtx, &addonv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{Name: "managed-serviceaccount", Namespace: clusterName},
		})).To(Succeed())

		By("Setting up spoke client mock at version 4.19.3")
		cv := &configv1.ClusterVersion{
			ObjectMeta: metav1.ObjectMeta{Name: ctlrutils.ClusterVersionName, Generation: 1},
			Status: configv1.ClusterVersionStatus{
				ObservedGeneration: 1,
				History: []configv1.UpdateHistory{
					{Version: ctRelease1, State: configv1.CompletedUpdate},
				},
				Conditions: []configv1.ClusterOperatorStatusCondition{
					{Type: configv1.OperatorUpgradeable, Status: configv1.ConditionFalse,
						Message: "Cluster should not be upgraded between minor versions: AdminAckRequired"},
				},
			},
		}
		spokeClient = setupUpgradeSpokeClient(cv)
	})

	AfterAll(func() {
		By("Deleting created resources")
		spokeclient.ClearCache()

		// Delete ProvisioningRequest (strip finalizer first — envtest has no namespace controller)
		prObj := &provisioningv1alpha1.ProvisioningRequest{}
		if err := K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, prObj); err == nil {
			prObj.Finalizers = nil
			Expect(K8SClient.Update(testCtx, prObj)).To(Succeed())
			Expect(K8SClient.Delete(testCtx, prObj)).To(Succeed())
		}

		// Delete NAR and AllocatedNodes
		narObj := &hwmgmtv1alpha1.NodeAllocationRequest{}
		if err := K8SClient.Get(testCtx, types.NamespacedName{
			Name: prName, Namespace: constants.DefaultNamespace,
		}, narObj); err == nil {
			narObj.Finalizers = nil
			Expect(K8SClient.Update(testCtx, narObj)).To(Succeed())
			Expect(K8SClient.Delete(testCtx, narObj)).To(Succeed())
		}
		anList := testNonCachingListAllocatedNodesForNAR(testCtx, prName)
		for i := range anList.Items {
			anList.Items[i].Finalizers = nil
			Expect(K8SClient.Update(testCtx, &anList.Items[i])).To(Succeed())
			Expect(client.IgnoreNotFound(K8SClient.Delete(testCtx, &anList.Items[i]))).To(Succeed())
		}

		// Delete BMH-related resources
		for _, bmhData := range testutils.MnoBMHs(3, 2) {
			Expect(client.IgnoreNotFound(K8SClient.Delete(testCtx, &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{Name: bmhData.Name, Namespace: bmhData.Namespace}}))).To(Succeed())
			Expect(client.IgnoreNotFound(K8SClient.Delete(testCtx, &metal3v1alpha1.HardwareData{
				ObjectMeta: metav1.ObjectMeta{Name: bmhData.Name, Namespace: bmhData.Namespace}}))).To(Succeed())
			Expect(client.IgnoreNotFound(K8SClient.Delete(testCtx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-bmc-secret", bmhData.Name),
					Namespace: constants.DefaultNamespace,
				}}))).To(Succeed())
		}

		// Delete ClusterTemplates
		for _, ver := range []string{ctVersion1, ctVersion2, ctVersion3, ctVersion4} {
			Expect(client.IgnoreNotFound(K8SClient.Delete(testCtx, &provisioningv1alpha1.ClusterTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      provisioningcontrollers.GetClusterTemplateRefName(ctName, ver),
					Namespace: ctNamespace,
				}}))).To(Succeed())
		}

		// Delete ConfigMaps
		for _, yaml := range cmYamls {
			cm, err := testutils.LoadYAML[corev1.ConfigMap](yaml)
			if err == nil {
				Expect(client.IgnoreNotFound(K8SClient.Delete(testCtx, cm))).To(Succeed())
			}
		}
		for _, name := range []string{
			"clusterinstance-defaults-v2",
			"clusterinstance-defaults-v3",
			"clusterinstance-defaults-v4",
			"clustertemplate-sample.v1.0.0-extramanifests",
		} {
			Expect(client.IgnoreNotFound(K8SClient.Delete(testCtx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ctNamespace}}))).To(Succeed())
		}

		// Delete ClusterImageSets
		for _, cis := range []string{ctRelease1, ctRelease2, ctRelease3, ctRelease4} {
			Expect(client.IgnoreNotFound(K8SClient.Delete(testCtx, &hivev1.ClusterImageSet{
				ObjectMeta: metav1.ObjectMeta{Name: cis}}))).To(Succeed())
		}

		// Delete pull secret, ManagedCluster
		Expect(client.IgnoreNotFound(K8SClient.Delete(testCtx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "pull-secret", Namespace: ctNamespace}}))).To(Succeed())
		Expect(client.IgnoreNotFound(K8SClient.Delete(testCtx, &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName}}))).To(Succeed())
	})

	// ================================================================
	// Test 1: y-stream upgrade with failure recovery and successful completion
	// ================================================================

	Describe("y-stream upgrade 4.19.3 to 4.20.5 with failure recovery and successful completion", func() {
		It("should fail with version mismatch when PR overrides desiredUpdate.version", func() {
			updatePR(testCtx, K8SClient, ctVersion2, map[string]any{
				constants.TemplateParamUpgrade: map[string]any{
					ctlrutils.UpgradeDefaultsClusterVersionKey: map[string]any{
						"desiredUpdate": map[string]any{"version": ctRelease3},
					},
				},
			})

			simulateSpokeAccessReady(testCtx, K8SClient)
			waitForPRUpgradeCondition(testCtx, K8SClient,
				string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed),
				"does not match the ClusterTemplate spec.release",
				provisioningv1alpha1.StateFailed,
			)
		})

		It("should report CV's Upgradeable=False after fixing version", func() {
			updatePR(testCtx, K8SClient, "", map[string]any{
				constants.TemplateParamUpgrade: map[string]any{
					ctlrutils.UpgradeDefaultsClusterVersionKey: map[string]any{
						"desiredUpdate": map[string]any{"version": ctRelease2},
					},
				},
			})

			waitForPRUpgradeCondition(testCtx, K8SClient,
				string(provisioningv1alpha1.CRconditionReasons.Pending),
				"should not be upgraded",
				provisioningv1alpha1.StateProgressing,
			)
		})

		It("should report CV's RetrievedUpdates=False after fixing Upgradeable", func() {
			updateSpokeCV(testCtx, spokeClient, func(cv *configv1.ClusterVersion) {
				cv.Status.Conditions = setCVCondition(cv.Status.Conditions,
					configv1.OperatorUpgradeable, configv1.ConditionTrue, "")
				cv.Status.Conditions = setCVCondition(cv.Status.Conditions,
					configv1.RetrievedUpdates, configv1.ConditionFalse,
					"Unable to retrieve available updates: connection refused")
			})

			waitForPRUpgradeCondition(testCtx, K8SClient,
				string(provisioningv1alpha1.CRconditionReasons.Pending),
				"Unable to retrieve available updates",
				provisioningv1alpha1.StateProgressing,
			)
		})

		It("should report target not available when graph has no target", func() {
			updateSpokeCV(testCtx, spokeClient, func(cv *configv1.ClusterVersion) {
				cv.Status.Conditions = setCVCondition(cv.Status.Conditions,
					configv1.RetrievedUpdates, configv1.ConditionTrue, "")
			})

			waitForPRUpgradeCondition(testCtx, K8SClient,
				string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed),
				"Target version "+ctRelease2+" is not available for upgrade",
				provisioningv1alpha1.StateFailed,
			)
		})

		It("should trigger upgrade after channel update and CV's target available", func() {
			updateSpokeCV(testCtx, spokeClient, func(cv *configv1.ClusterVersion) {
				cv.Status.AvailableUpdates = []configv1.Release{{Version: ctRelease2}}
			})

			updatePR(testCtx, K8SClient, "", map[string]any{
				constants.TemplateParamUpgrade: map[string]any{
					ctlrutils.UpgradeDefaultsClusterVersionKey: map[string]any{
						"desiredUpdate": map[string]any{"version": ctRelease2},
						"channel":       "stable-4.20",
					},
				},
			})

			waitForPRUpgradeCondition(testCtx, K8SClient,
				string(provisioningv1alpha1.CRconditionReasons.Pending),
				"triggered. Waiting for upgrade to start",
				provisioningv1alpha1.StateProgressing,
			)
		})

		It("should show InProgress with CV's Progressing message", func() {
			updateSpokeCV(testCtx, spokeClient, func(cv *configv1.ClusterVersion) {
				cv.Status.History = append([]configv1.UpdateHistory{
					{Version: ctRelease2, State: configv1.PartialUpdate, StartedTime: metav1.Now()},
				}, cv.Status.History...)
				cv.Status.Conditions = setCVCondition(cv.Status.Conditions,
					configv1.OperatorProgressing, configv1.ConditionTrue,
					"Working towards "+ctRelease2+": 100 of 904 done (11% complete)")
			})

			waitForPRUpgradeCondition(testCtx, K8SClient,
				string(provisioningv1alpha1.CRconditionReasons.InProgress),
				"Upgrading to desired version "+ctRelease2+": Working towards "+ctRelease2,
				provisioningv1alpha1.StateProgressing,
			)
		})

		It("should complete upgrade and return to fulfilled", func() {
			updateSpokeCV(testCtx, spokeClient, func(cv *configv1.ClusterVersion) {
				cv.Status.History[0].State = configv1.CompletedUpdate
				cv.Status.Conditions = setCVCondition(cv.Status.Conditions,
					configv1.OperatorProgressing, configv1.ConditionFalse,
					"Cluster version is "+ctRelease2)
			})

			waitForPRUpgradeCondition(testCtx, K8SClient,
				string(provisioningv1alpha1.CRconditionReasons.Completed),
				"Upgrade to version "+ctRelease2+" completed",
				provisioningv1alpha1.StateFulfilled,
			)

			// Verify spoke access resources are cleaned up
			assertSpokeAccessCleaned(testCtx, K8SClient)

			// Simulate the ACM controller behavior after upgrade - update the ManagedCluster label to the new version
			mc := &clusterv1.ManagedCluster{}
			Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: clusterName}, mc)).To(Succeed())
			patch := client.MergeFrom(mc.DeepCopy())
			mc.Labels["openshiftVersion"] = ctRelease2
			Expect(K8SClient.Patch(testCtx, mc, patch)).To(Succeed())
		})
	})

	// ================================
	// Test 2: Upgrade timeout
	// ================================

	Describe("y-stream upgrade timeout when upgrading from 4.20.5 to 4.21.2", func() {
		It("should start upgrade to 4.21.2 and reach InProgress", func() {
			updatePR(testCtx, K8SClient, ctVersion3, map[string]any{
				constants.TemplateParamUpgrade: map[string]any{
					ctlrutils.UpgradeDefaultsClusterVersionKey: map[string]any{
						"desiredUpdate": map[string]any{"version": ctRelease3},
					},
				},
			})

			simulateSpokeAccessReady(testCtx, K8SClient)

			updateSpokeCV(testCtx, spokeClient, func(cv *configv1.ClusterVersion) {
				cv.Status.History = []configv1.UpdateHistory{
					{Version: ctRelease3, State: configv1.PartialUpdate, StartedTime: metav1.Now()},
					{Version: ctRelease2, State: configv1.CompletedUpdate},
				}
				cv.Status.Conditions = setCVCondition(cv.Status.Conditions,
					configv1.OperatorProgressing, configv1.ConditionTrue,
					"Working towards "+ctRelease3+": 50 of 904 done")
				cv.Status.Conditions = setCVCondition(cv.Status.Conditions,
					configv1.RetrievedUpdates, configv1.ConditionTrue, "")
				cv.Status.Conditions = setCVCondition(cv.Status.Conditions,
					ctlrutils.CVConditionFailing, configv1.ConditionTrue,
					"Multiple unknown errors")
				cv.Status.AvailableUpdates = []configv1.Release{{Version: ctRelease3}}
			})

			waitForPRUpgradeCondition(testCtx, K8SClient,
				string(provisioningv1alpha1.CRconditionReasons.InProgress),
				"Upgrading to desired version "+ctRelease3+": Working towards "+ctRelease3,
				provisioningv1alpha1.StateProgressing,
			)
		})

		It("should time out when clusterUpgradeTimeout is set to 5s", func() {
			updatePR(testCtx, K8SClient, "", map[string]any{
				constants.TemplateParamUpgrade: map[string]any{
					ctlrutils.UpgradeDefaultsClusterVersionKey: map[string]any{
						"desiredUpdate": map[string]any{},
					},
					ctlrutils.ClusterUpgradeTimeoutConfigKey: "5s",
				},
			})

			waitForPRUpgradeCondition(testCtx, K8SClient,
				string(provisioningv1alpha1.CRconditionReasons.TimedOut),
				"Upgrade timed out: Multiple unknown errors",
				provisioningv1alpha1.StateFailed,
			)

			assertSpokeAccessCleaned(testCtx, K8SClient)
		})
	})

	// =================================
	// Test 3: Terminal failure recovery
	// =================================

	Describe("Terminal failure recovery from 4.21.2 to 4.20.5", func() {
		It("should recover to fulfilled when switching back to CT matching current ClusterVersion 4.20.5", func() {
			updatePR(testCtx, K8SClient, ctVersion2, map[string]any{
				constants.TemplateParamUpgrade: nil,
			})

			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
				cond := meta.FindStatusCondition(pr.Status.Conditions,
					string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted))
				return cond == nil &&
					pr.Status.ProvisioningStatus.ProvisioningPhase == provisioningv1alpha1.StateFulfilled
			}, timeout, interval).Should(BeTrue(),
				"UpgradeCompleted condition should be removed and PR should be Fulfilled")
		})
	})

	// ============================
	// Test 4: EUS upgrade timeout
	// ============================

	Describe("EUS upgrade timeout when upgrading from 4.20.5 to 4.22.4 via 4.21.6", func() {
		It("should start EUS intermediate upgrade and reach InProgress", func() {
			updateSpokeCV(testCtx, spokeClient, func(cv *configv1.ClusterVersion) {
				cv.Status.History = []configv1.UpdateHistory{
					{Version: ctRelease2, State: configv1.CompletedUpdate},
				}
				cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{
					{Type: configv1.OperatorUpgradeable, Status: configv1.ConditionTrue, LastTransitionTime: metav1.Now()},
					{Type: configv1.RetrievedUpdates, Status: configv1.ConditionTrue, LastTransitionTime: metav1.Now()},
				}
				cv.Status.AvailableUpdates = []configv1.Release{{Version: "4.21.6"}}
			})

			updateSpokeMCP(testCtx, spokeClient, false, corev1.ConditionTrue)

			updatePR(testCtx, K8SClient, ctVersion4, map[string]any{
				constants.TemplateParamUpgrade: map[string]any{
					ctlrutils.UpgradeDefaultsClusterVersionKey: map[string]any{
						"desiredUpdate": map[string]any{"version": ctRelease4},
					},
					ctlrutils.UpgradeIntermediateVersionConfigKey: "4.21.6",
				},
			})

			simulateSpokeAccessReady(testCtx, K8SClient)

			waitForPRUpgradeCondition(testCtx, K8SClient,
				string(provisioningv1alpha1.CRconditionReasons.Pending),
				"Upgrade to intermediate version 4.21.6 triggered. Waiting for upgrade to start",
				provisioningv1alpha1.StateProgressing,
			)

			updateSpokeCV(testCtx, spokeClient, func(cv *configv1.ClusterVersion) {
				cv.Status.History = append([]configv1.UpdateHistory{
					{Version: "4.21.6", State: configv1.PartialUpdate, StartedTime: metav1.Now()},
				}, cv.Status.History...)
				cv.Status.Conditions = setCVCondition(cv.Status.Conditions,
					configv1.OperatorProgressing, configv1.ConditionTrue,
					"Working towards 4.21.6: 50 of 904 done")
				cv.Status.Conditions = setCVCondition(cv.Status.Conditions,
					ctlrutils.CVConditionFailing, configv1.ConditionTrue,
					"Multiple unknown errors")
			})

			waitForPRUpgradeCondition(testCtx, K8SClient,
				string(provisioningv1alpha1.CRconditionReasons.InProgress),
				"Upgrading to intermediate version 4.21.6",
				provisioningv1alpha1.StateProgressing,
			)
		})

		It("should time out during EUS intermediate upgrade", func() {
			updatePR(testCtx, K8SClient, "", map[string]any{
				constants.TemplateParamUpgrade: map[string]any{
					ctlrutils.UpgradeDefaultsClusterVersionKey: map[string]any{
						"desiredUpdate": map[string]any{"version": ctRelease4},
					},
					ctlrutils.UpgradeIntermediateVersionConfigKey: "4.21.6",
					ctlrutils.ClusterUpgradeTimeoutConfigKey:      "5s",
				},
			})

			waitForPRUpgradeCondition(testCtx, K8SClient,
				string(provisioningv1alpha1.CRconditionReasons.TimedOut),
				"Upgrade timed out: Multiple unknown errors",
				provisioningv1alpha1.StateFailed,
			)

			mcp := &mcfgv1.MachineConfigPool{}
			Expect(spokeClient.Get(testCtx, types.NamespacedName{Name: "worker"}, mcp)).To(Succeed())
			Expect(mcp.Spec.Paused).To(BeTrue(), "Worker MCP should still be paused after timeout")

			assertSpokeAccessCleaned(testCtx, K8SClient)
		})
	})

	// ================================================================
	// Test 5: Terminal failure recovery after EUS timeout
	// ================================================================

	Describe("Terminal failure recovery after EUS timeout from 4.22.4 to 4.20.5", func() {
		It("should recover to fulfilled when switching back to CT matching current ClusterVersion 4.20.5", func() {
			updatePR(testCtx, K8SClient, ctVersion2, map[string]any{
				constants.TemplateParamUpgrade: nil,
			})

			Eventually(func() bool {
				Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
				cond := meta.FindStatusCondition(pr.Status.Conditions,
					string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted))
				return cond == nil &&
					pr.Status.ProvisioningStatus.ProvisioningPhase == provisioningv1alpha1.StateFulfilled
			}, timeout, interval).Should(BeTrue(),
				"UpgradeCompleted condition should be removed and PR should be Fulfilled")
		})
	})

	// ===================================================================
	// Test 6: EUS upgrade with failure recovery and successful completion
	// ===================================================================

	Describe("EUS upgrade 4.20.5 to 4.22.4 via 4.21.7 with failure recovery and successful completion", func() {
		It("should fail with MCPs not updated", func() {
			updateSpokeCV(testCtx, spokeClient, func(cv *configv1.ClusterVersion) {
				cv.Status.History = []configv1.UpdateHistory{
					{Version: ctRelease2, State: configv1.CompletedUpdate},
				}
				cv.Status.Conditions = []configv1.ClusterOperatorStatusCondition{
					{Type: configv1.OperatorUpgradeable, Status: configv1.ConditionTrue, LastTransitionTime: metav1.Now()},
					{Type: configv1.RetrievedUpdates, Status: configv1.ConditionTrue, LastTransitionTime: metav1.Now()},
				}
				cv.Status.AvailableUpdates = []configv1.Release{{Version: eusIntermediateVersion}}
			})

			updateSpokeMCP(testCtx, spokeClient, false, corev1.ConditionFalse)

			updatePR(testCtx, K8SClient, ctVersion4, map[string]any{
				constants.TemplateParamUpgrade: map[string]any{
					ctlrutils.UpgradeDefaultsClusterVersionKey: map[string]any{
						"desiredUpdate": map[string]any{"version": ctRelease4},
					},
					ctlrutils.UpgradeIntermediateVersionConfigKey: eusIntermediateVersion,
				},
			})

			simulateSpokeAccessReady(testCtx, K8SClient)

			waitForPRUpgradeCondition(testCtx, K8SClient,
				string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed),
				"MachineConfigPools not updated",
				provisioningv1alpha1.StateFailed,
			)
		})

		It("should fail with invalid intermediateVersion after fixing MCPs", func() {
			updateSpokeMCP(testCtx, spokeClient, false, corev1.ConditionTrue)

			updatePR(testCtx, K8SClient, "", map[string]any{
				constants.TemplateParamUpgrade: map[string]any{
					ctlrutils.UpgradeDefaultsClusterVersionKey: map[string]any{
						"desiredUpdate": map[string]any{"version": ctRelease4},
					},
					ctlrutils.UpgradeIntermediateVersionConfigKey: "4.20.3",
				},
			})

			waitForPRUpgradeCondition(testCtx, K8SClient,
				string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed),
				"intermediateVersion 4.20.3 must be exactly one minor version below targetVersion "+ctRelease4,
				provisioningv1alpha1.StateFailed,
			)
		})

		It("should start intermediate upgrade after fixing intermediateVersion", func() {
			updatePR(testCtx, K8SClient, "", map[string]any{
				constants.TemplateParamUpgrade: map[string]any{
					ctlrutils.UpgradeDefaultsClusterVersionKey: map[string]any{
						"desiredUpdate": map[string]any{"version": ctRelease4},
					},
					ctlrutils.UpgradeIntermediateVersionConfigKey: eusIntermediateVersion,
				},
			})

			waitForPRUpgradeCondition(testCtx, K8SClient,
				string(provisioningv1alpha1.CRconditionReasons.Pending),
				"Upgrade to intermediate version "+eusIntermediateVersion+" triggered. Waiting for upgrade to start",
				provisioningv1alpha1.StateProgressing,
			)
		})

		It("should show intermediate upgrade in progress", func() {
			updateSpokeCV(testCtx, spokeClient, func(cv *configv1.ClusterVersion) {
				cv.Status.History = append([]configv1.UpdateHistory{
					{Version: eusIntermediateVersion, State: configv1.PartialUpdate, StartedTime: metav1.Now()},
				}, cv.Status.History...)
				cv.Status.Conditions = setCVCondition(cv.Status.Conditions,
					configv1.OperatorProgressing, configv1.ConditionTrue,
					"Working towards "+eusIntermediateVersion+": 100 of 904 done (11% complete)")
			})

			waitForPRUpgradeCondition(testCtx, K8SClient,
				string(provisioningv1alpha1.CRconditionReasons.InProgress),
				"Upgrading to intermediate version "+eusIntermediateVersion,
				provisioningv1alpha1.StateProgressing,
			)
		})

		It("should start target upgrade after intermediate completes", func() {
			updateSpokeCV(testCtx, spokeClient, func(cv *configv1.ClusterVersion) {
				cv.Status.History[0].State = configv1.CompletedUpdate
				cv.Status.Conditions = setCVCondition(cv.Status.Conditions,
					configv1.OperatorProgressing, configv1.ConditionFalse,
					"Cluster version is "+eusIntermediateVersion)
				cv.Status.AvailableUpdates = []configv1.Release{{Version: ctRelease4}}
			})

			waitForPRUpgradeCondition(testCtx, K8SClient,
				string(provisioningv1alpha1.CRconditionReasons.Pending),
				"Upgrade to desired version "+ctRelease4+" triggered. Waiting for upgrade to start",
				provisioningv1alpha1.StateProgressing,
			)
		})

		It("should show target upgrade in progress", func() {
			updateSpokeCV(testCtx, spokeClient, func(cv *configv1.ClusterVersion) {
				cv.Status.History = append([]configv1.UpdateHistory{
					{Version: ctRelease4, State: configv1.PartialUpdate, StartedTime: metav1.Now()},
				}, cv.Status.History...)
				cv.Status.Conditions = setCVCondition(cv.Status.Conditions,
					configv1.OperatorProgressing, configv1.ConditionTrue,
					"Working towards "+ctRelease4+": 100 of 904 done (11% complete)")
			})

			waitForPRUpgradeCondition(testCtx, K8SClient,
				string(provisioningv1alpha1.CRconditionReasons.InProgress),
				"Upgrading to desired version "+ctRelease4,
				provisioningv1alpha1.StateProgressing,
			)
		})

		It("should wait for MCPs to update after target upgrade completes", func() {
			updateSpokeMCP(testCtx, spokeClient, true, corev1.ConditionFalse)

			updateSpokeCV(testCtx, spokeClient, func(cv *configv1.ClusterVersion) {
				cv.Status.History[0].State = configv1.CompletedUpdate
				cv.Status.Conditions = setCVCondition(cv.Status.Conditions,
					configv1.OperatorProgressing, configv1.ConditionFalse,
					"Cluster version is "+ctRelease4)
			})

			waitForPRUpgradeCondition(testCtx, K8SClient,
				string(provisioningv1alpha1.CRconditionReasons.InProgress),
				"Waiting for worker MachineConfigPools to finish updating",
				provisioningv1alpha1.StateProgressing,
			)

			mcp := &mcfgv1.MachineConfigPool{}
			Expect(spokeClient.Get(testCtx, types.NamespacedName{Name: "worker"}, mcp)).To(Succeed())
			Expect(mcp.Spec.Paused).To(BeFalse(), "Worker MCP should be unpaused after target upgrade completes")
		})

		It("should complete EUS upgrade when MCPs are updated", func() {
			updateSpokeMCP(testCtx, spokeClient, false, corev1.ConditionTrue)

			waitForPRUpgradeCondition(testCtx, K8SClient,
				string(provisioningv1alpha1.CRconditionReasons.Completed),
				"Upgrade to version "+ctRelease4+" completed",
				provisioningv1alpha1.StateFulfilled,
			)

			assertSpokeAccessCleaned(testCtx, K8SClient)

			mc := &clusterv1.ManagedCluster{}
			Expect(K8SClient.Get(testCtx, types.NamespacedName{Name: clusterName}, mc)).To(Succeed())
			patch := client.MergeFrom(mc.DeepCopy())
			mc.Labels["openshiftVersion"] = ctRelease4
			Expect(K8SClient.Patch(testCtx, mc, patch)).To(Succeed())
		})
	})
})

// --- Helpers for CV upgrade tests ---
func setupUpgradeSpokeClient(cv *configv1.ClusterVersion) client.Client {
	spokeScheme := runtime.NewScheme()
	Expect(configv1.Install(spokeScheme)).To(Succeed())
	Expect(mcfgv1.Install(spokeScheme)).To(Succeed())

	spokeClient := fakeclient.NewClientBuilder().
		WithScheme(spokeScheme).
		WithObjects(cv).
		WithStatusSubresource(cv).
		Build()

	spokeclient.SetTestSpokeClientCreator(
		func(_ string, _ string, _ []byte, _ *runtime.Scheme) (client.Client, error) {
			return spokeClient, nil
		},
	)
	return spokeClient
}

func updateSpokeCV(ctx context.Context, spokeClient client.Client, mutateFn func(*configv1.ClusterVersion)) {
	cv := &configv1.ClusterVersion{}
	Expect(spokeClient.Get(ctx, types.NamespacedName{Name: ctlrutils.ClusterVersionName}, cv)).To(Succeed())
	patch := client.MergeFrom(cv.DeepCopy())
	mutateFn(cv)
	Expect(spokeClient.Status().Patch(ctx, cv, patch)).To(Succeed())
}

func setCVCondition(
	conditions []configv1.ClusterOperatorStatusCondition,
	condType configv1.ClusterStatusConditionType,
	status configv1.ConditionStatus,
	message string,
) []configv1.ClusterOperatorStatusCondition {
	for i := range conditions {
		if conditions[i].Type == condType {
			conditions[i].Status = status
			conditions[i].Message = message
			conditions[i].LastTransitionTime = metav1.Now()
			return conditions
		}
	}
	return append(conditions, configv1.ClusterOperatorStatusCondition{
		Type: condType, Status: status, Message: message,
		LastTransitionTime: metav1.Now(),
	})
}

// simulateSpokeAccessReady waits for the controller to create the MSA and MW,
// then simulates the addon controller by creating the token secret and setting
// the MSA tokenSecretRef and MW Available status.
func simulateSpokeAccessReady(ctx context.Context, k8sClient client.Client) {
	msaName := prName + "-upgrade"
	mwName := prName + "-upgrade-rbac"
	tokenSecretName := msaName + "-token"

	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{Name: msaName, Namespace: clusterName},
			&msav1beta1.ManagedServiceAccount{})
	}, cvUpgradeTimeout, cvUpgradeInterval).Should(Succeed(), "MSA should be created by the controller")

	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: tokenSecretName, Namespace: clusterName},
		Data:       map[string][]byte{"token": []byte("test-token"), "ca.crt": []byte("test-ca")},
	}
	err := k8sClient.Create(ctx, tokenSecret)
	if err != nil && !errors.IsAlreadyExists(err) {
		Expect(err).ToNot(HaveOccurred())
	}

	msa := &msav1beta1.ManagedServiceAccount{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: msaName, Namespace: clusterName}, msa)).To(Succeed())
	msa.Status.TokenSecretRef = &msav1beta1.SecretRef{
		Name:                 tokenSecretName,
		LastRefreshTimestamp: metav1.Now(),
	}
	Expect(k8sClient.Status().Update(ctx, msa)).To(Succeed())

	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{Name: mwName, Namespace: clusterName},
			&workv1.ManifestWork{})
	}, cvUpgradeTimeout, cvUpgradeInterval).Should(Succeed(), "ManifestWork should be created by the controller")

	mw := &workv1.ManifestWork{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: mwName, Namespace: clusterName}, mw)).To(Succeed())
	mw.Status.Conditions = []metav1.Condition{
		{Type: workv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "Applied", LastTransitionTime: metav1.Now()},
	}
	Expect(k8sClient.Status().Update(ctx, mw)).To(Succeed())
}

func assertSpokeAccessCleaned(ctx context.Context, k8sClient client.Client) {
	msaName := prName + "-upgrade"
	mwName := prName + "-upgrade-rbac"

	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: msaName, Namespace: clusterName},
			&msav1beta1.ManagedServiceAccount{})
		return errors.IsNotFound(err)
	}, cvUpgradeTimeout, cvUpgradeInterval).Should(BeTrue(), "MSA should be deleted")

	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: mwName, Namespace: clusterName},
			&workv1.ManifestWork{})
		return errors.IsNotFound(err)
	}, cvUpgradeTimeout, cvUpgradeInterval).Should(BeTrue(), "ManifestWork should be deleted")
}

func waitForPRUpgradeCondition(ctx context.Context, k8SClient client.Client,
	reason, msgSubstring string, expectedPhase provisioningv1alpha1.ProvisioningPhase) {
	Eventually(func() bool {
		pr := &provisioningv1alpha1.ProvisioningRequest{}
		Expect(k8SClient.Get(ctx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
		cond := meta.FindStatusCondition(pr.Status.Conditions,
			string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted))
		return cond != nil &&
			cond.Reason == reason &&
			strings.Contains(cond.Message, msgSubstring) &&
			pr.Status.ProvisioningStatus.ProvisioningPhase == expectedPhase
	}, cvUpgradeTimeout, cvUpgradeInterval).Should(BeTrue(),
		fmt.Sprintf("Expected UpgradeCompleted reason=%s msg containing %q phase=%s",
			reason, msgSubstring, expectedPhase))
}

func updatePR(ctx context.Context, k8SClient client.Client, version string, params map[string]any) {
	pr := &provisioningv1alpha1.ProvisioningRequest{}
	Expect(k8SClient.Get(ctx, types.NamespacedName{Name: prName}, pr)).To(Succeed())
	if version != "" {
		pr.Spec.TemplateVersion = version
	}
	if params != nil {
		var existing map[string]any
		Expect(json.Unmarshal(pr.Spec.TemplateParameters.Raw, &existing)).To(Succeed())
		for k, v := range params {
			if v == nil {
				delete(existing, k)
			} else {
				existing[k] = v
			}
		}
		raw, err := json.Marshal(existing)
		Expect(err).ToNot(HaveOccurred())
		pr.Spec.TemplateParameters = runtime.RawExtension{Raw: raw}
	}
	Expect(k8SClient.Update(ctx, pr)).To(Succeed())
}

func createUpgradeCT(ctx context.Context, k8SClient client.Client,
	resourceDir, tName, version, release, ciDefaultsCMName string) {
	baseCT, err := testutils.LoadYAML[provisioningv1alpha1.ClusterTemplate](
		filepath.Join(resourceDir, "ct-std-du-v1.yaml"))
	Expect(err).ToNot(HaveOccurred())

	baseCT.Name = provisioningcontrollers.GetClusterTemplateRefName(tName, version)
	baseCT.Spec.Version = version
	baseCT.Spec.Release = release
	baseCT.Spec.TemplateDefaults.ClusterInstanceDefaults = ciDefaultsCMName
	Expect(k8SClient.Create(ctx, baseCT)).To(Succeed())
}

func updateSpokeMCP(ctx context.Context, spokeClient client.Client, paused bool, updated corev1.ConditionStatus) {
	mcp := &mcfgv1.MachineConfigPool{}
	err := spokeClient.Get(ctx, types.NamespacedName{Name: "worker"}, mcp)
	if errors.IsNotFound(err) {
		mcp = &mcfgv1.MachineConfigPool{
			ObjectMeta: metav1.ObjectMeta{Name: "worker"},
			Spec:       mcfgv1.MachineConfigPoolSpec{Paused: paused},
			Status: mcfgv1.MachineConfigPoolStatus{
				Conditions: []mcfgv1.MachineConfigPoolCondition{
					{Type: mcfgv1.MachineConfigPoolUpdated, Status: updated, LastTransitionTime: metav1.Now()},
				},
			},
		}
		Expect(spokeClient.Create(ctx, mcp)).To(Succeed())
		return
	}
	Expect(err).ToNot(HaveOccurred())
	mcp.Spec.Paused = paused
	for i, c := range mcp.Status.Conditions {
		if c.Type == mcfgv1.MachineConfigPoolUpdated {
			mcp.Status.Conditions[i].Status = updated
			mcp.Status.Conditions[i].LastTransitionTime = metav1.Now()
			Expect(spokeClient.Update(ctx, mcp)).To(Succeed())
			return
		}
	}
	mcp.Status.Conditions = append(mcp.Status.Conditions, mcfgv1.MachineConfigPoolCondition{
		Type: mcfgv1.MachineConfigPoolUpdated, Status: updated, LastTransitionTime: metav1.Now(),
	})
	Expect(spokeClient.Update(ctx, mcp)).To(Succeed())
}

func createCIDefaultsCM(ctx context.Context, k8SClient client.Client,
	resourceDir, name, baseRelease, newRelease string) {
	baseCM, err := testutils.LoadYAML[corev1.ConfigMap](
		filepath.Join(resourceDir, "clusterinstance-defaults-v1.yaml"))
	Expect(err).ToNot(HaveOccurred())

	baseCM.Name = name
	ciDefaults := strings.ReplaceAll(
		baseCM.Data[ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey],
		baseRelease, newRelease)
	baseCM.Data[ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey] = ciDefaults
	Expect(k8SClient.Create(ctx, baseCM)).To(Succeed())
}
