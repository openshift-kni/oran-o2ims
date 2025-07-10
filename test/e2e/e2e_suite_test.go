/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllersE2Etest

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	ibgu "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	provisioningcontrollers "github.com/openshift-kni/oran-o2ims/internal/controllers"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	testutils "github.com/openshift-kni/oran-o2ims/test/utils"
	assistedservicev1beta1 "github.com/openshift/assisted-service/api/v1beta1"
)

const testHwMgrPluginNameSpace = "hwmgr"
const testHardwarePluginRef = "hwmgr"

var (
	K8SClient                     client.Client
	K8SManager                    ctrl.Manager
	ProvReqTestReconciler         *provisioningcontrollers.ProvisioningRequestReconciler
	ClusterTemplateTestReconciler *provisioningcontrollers.ClusterTemplateReconciler
	testEnv                       *envtest.Environment
	ctx                           context.Context
	cancel                        context.CancelFunc
	// store external CRDs
	tmpDir string
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	tmpDir = t.TempDir()
	RunSpecs(t, "Controllers end to end")
}

// Logger used for tests:
var logger *slog.Logger

var _ = BeforeSuite(func() {
	// Create a logger that writes to the Ginkgo writer, so that the log messages will be
	// attached to the output of the right test:
	options := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	handler := slog.NewJSONHandler(GinkgoWriter, options)
	logger = slog.New(handler)

	// Configure the controller runtime library to use our logger:
	adapter := logr.FromSlogHandler(logger.Handler())
	ctrl.SetLogger(adapter)
	klog.SetLogger(adapter)

	// Set the hardware manager plugin details.
	os.Setenv(utils.HwMgrPluginNameSpace, testHwMgrPluginNameSpace)

	// Set the scheme.
	testScheme := runtime.NewScheme()
	err := provisioningv1alpha1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = hwmgmtv1alpha1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = corev1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = siteconfig.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = policiesv1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = clusterv1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = assistedservicev1beta1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = apiextensionsv1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = admissionregistrationv1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = ibgu.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = policiesv1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = clusterv1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())

	// Get the needed external CRDs. Their details are under test/utils/vars.go - ExternalCrdsData.
	// Update that with any other CRDs that the provisioning controller depends on.
	Expect(testutils.GetExternalCrdFiles(tmpDir)).To(Succeed())
	// Start testEnv - include the directories holding the external CRDs.
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
			filepath.Join("..", "..", "vendor", "open-cluster-management.io", "api", "cluster", "v1"),
			tmpDir,
		},
		ErrorIfCRDPathMissing: true,
		Scheme:                testScheme,
	}
	// Start testEnv.
	cfg, err := testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())
	ctx, cancel = context.WithCancel(context.TODO())

	// Get the manager.
	K8SManager, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: testScheme,
	})
	Expect(err).ToNot(HaveOccurred())
	Expect(K8SManager).NotTo(BeNil())

	// Get the client.
	K8SClient, err = client.New(cfg, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(K8SClient).NotTo(BeNil())

	// Setup the ClusterTemplate Reconciler.
	ClusterTemplateTestReconciler = &provisioningcontrollers.ClusterTemplateReconciler{
		Client: K8SClient,
		Logger: logger,
	}
	err = ClusterTemplateTestReconciler.SetupWithManager(K8SManager)
	Expect(err).ToNot(HaveOccurred())

	// Setup the ProvisioningRequest Reconciler.
	ProvReqTestReconciler = &provisioningcontrollers.ProvisioningRequestReconciler{
		Client: K8SClient,
		Logger: logger,
	}
	err = ProvReqTestReconciler.SetupWithManager(K8SManager)
	Expect(err).ToNot(HaveOccurred())

	suiteCrs := []client.Object{
		// HW plugin test namespace
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: utils.UnitTestHwmgrNamespace,
			},
		},
		// oran-o2ims
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "oran-o2ims",
			},
		},
		// HardwarePlugin CRs
		&hwmgmtv1alpha1.HardwarePlugin{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testHwMgrPluginNameSpace,
				Name:      testHardwarePluginRef,
			},
			Spec: hwmgmtv1alpha1.HardwarePluginSpec{
				ApiRoot: "todo",
			},
		},
	}

	for _, cr := range suiteCrs {
		err := K8SClient.Create(context.Background(), cr)
		Expect(err).ToNot(HaveOccurred())
	}

	go func() {
		defer GinkgoRecover()
		err = K8SManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

var _ = Describe("Dry-run-ProvisioningRequestReconcile", func() {
	const timeout = time.Second * 60
	const interval = time.Second * 3

	var (
		testCtx                context.Context
		ProvRequestCR          *provisioningv1alpha1.ProvisioningRequest
		ctIncomplete           *provisioningv1alpha1.ClusterTemplate
		ctComplete             *provisioningv1alpha1.ClusterTemplate
		tName                  = "clustertemplate-a"
		tVersion1              = "v1.0.0"
		tVersion2              = "v2.0.0"
		ctNamespace            = "clustertemplate-a-v4-16"
		ciDefaultsCmIncomplete = "clusterinstance-defaults-v1"
		ciDefaultsCmComplete   = "clusterinstance-defaults-v2"
		ptDefaultsCm           = "policytemplate-defaults-v1"
		hwTemplate             = "hwtemplate-v1"
	)

	testCtx = context.Background()

	mainCRs := []client.Object{
		// Cluster Template Namespace
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: ctNamespace,
			},
		},
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ztp-" + ctNamespace,
			},
		},
		// Configmap for ClusterInstance defaults v1 - missing required values.
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ciDefaultsCmIncomplete,
				Namespace: ctNamespace,
			},
			Data: map[string]string{
				utils.ClusterInstallationTimeoutConfigKey: "60s",
				utils.ClusterInstanceTemplateDefaultsConfigmapKey: `
clusterImageSetNameRef: "4.15"
holdInstallation: false
cpuPartitioningMode: AllNodes
networkType: OVNKubernetes
pullSecretRef:
  name: "pull-secret"
templateRefs:
- name: "ai-cluster-templates-v1"
  namespace: "siteconfig-operator"
nodes:
- role: master
  automatedCleaningMode: disabled
  ironicInspect: ""
  bootMode: UEFI
  nodeNetwork:
    interfaces:
    - name: eno1
      label: bootable-interface
    - name: eth0
      label: base-interface
    - name: eth1
      label: data-interface
`,
			},
		},
		// Configmap for ClusterInstance defaults - complete.
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ciDefaultsCmComplete,
				Namespace: ctNamespace,
			},
			Data: map[string]string{
				utils.ClusterInstallationTimeoutConfigKey: "60s",
				utils.ClusterInstanceTemplateDefaultsConfigmapKey: `
clusterImageSetNameRef: "4.15"
holdInstallation: false
cpuPartitioningMode: AllNodes
networkType: OVNKubernetes
pullSecretRef:
  name: "pull-secret"
templateRefs:
- name: "ai-cluster-templates-v1"
  namespace: "siteconfig-operator"
nodes:
- role: master
  automatedCleaningMode: disabled
  ironicInspect: ""
  bootMode: UEFI
  hostName: node1
  nodeNetwork:
    interfaces:
    - name: eno1
      label: bootable-interface
    - name: eth0
      label: base-interface
    - name: eth1
      label: data-interface
  templateRefs:
  - name: test
    namespace: test
`,
			},
		},
		// Configmap for policy template defaults
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ptDefaultsCm,
				Namespace: ctNamespace,
			},
			Data: map[string]string{
				utils.ClusterConfigurationTimeoutConfigKey: "1m",
				utils.PolicyTemplateDefaultsConfigmapKey: `
cpu-isolated: "2-31"
cpu-reserved: "0-1"
defaultHugepagesSize: "1G"`,
			},
		},
		// hardware template
		&hwmgmtv1alpha1.HardwareTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hwTemplate,
				Namespace: utils.InventoryNamespace,
			},
			Spec: hwmgmtv1alpha1.HardwareTemplateSpec{
				HardwarePluginRef:           utils.UnitTestHwPluginRef,
				BootInterfaceLabel:          "bootable-interface",
				HardwareProvisioningTimeout: "1m",
				NodeGroupData: []hwmgmtv1alpha1.NodeGroupData{
					{
						Name:           "controller",
						Role:           "master",
						ResourcePoolId: "xyz",
						HwProfile:      "profile-spr-single-processor-64G",
					},
					{
						Name:           "worker",
						Role:           "worker",
						ResourcePoolId: "xyz",
						HwProfile:      "profile-spr-dual-processor-128G",
					},
				},
				Extensions: map[string]string{
					"resourceTypeId": "ResourceGroup~2.1.1",
				},
			},
		},
		// Pull secret for ClusterInstance
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pull-secret",
				Namespace: ctNamespace,
			},
			Data: map[string][]byte{
				".dockerconfigjson": []byte(testutils.TestSecretDataStr),
			},
			Type: corev1.SecretTypeDockerConfigJson,
		},
	}
	// Define the cluster templates.
	ctIncomplete = &provisioningv1alpha1.ClusterTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      provisioningcontrollers.GetClusterTemplateRefName(tName, tVersion1),
			Namespace: ctNamespace,
		},
		Spec: provisioningv1alpha1.ClusterTemplateSpec{
			Name:       tName,
			Version:    tVersion1,
			TemplateID: "aab39bda-ac56-4143-9b10-d1a71517d04f",
			Templates: provisioningv1alpha1.Templates{
				ClusterInstanceDefaults: ciDefaultsCmIncomplete,
				PolicyTemplateDefaults:  ptDefaultsCm,
				HwTemplate:              hwTemplate,
			},
			TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testutils.TestFullTemplateSchema)},
		},
	}
	ctComplete = &provisioningv1alpha1.ClusterTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      provisioningcontrollers.GetClusterTemplateRefName(tName, tVersion2),
			Namespace: ctNamespace,
		},
		Spec: provisioningv1alpha1.ClusterTemplateSpec{
			Name:       tName,
			Version:    tVersion2,
			TemplateID: "bbb39bda-ac56-4143-9b10-d1a71517d04f",
			Templates: provisioningv1alpha1.Templates{
				ClusterInstanceDefaults: ciDefaultsCmComplete,
				PolicyTemplateDefaults:  ptDefaultsCm,
				HwTemplate:              hwTemplate,
			},
			TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testutils.TestFullTemplateSchema)},
		},
	}
	ctCRs := []client.Object{ctComplete, ctIncomplete}

	// Define the provisioning request.
	ProvRequestCR = &provisioningv1alpha1.ProvisioningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{provisioningv1alpha1.ProvisioningRequestFinalizer},
			// Name to be set up in each test.
		},
		Spec: provisioningv1alpha1.ProvisioningRequestSpec{
			Name:            "test",
			Description:     "description",
			TemplateName:    tName,
			TemplateVersion: tVersion1,
		},
	}

	AfterEach(func() {
		for _, cr := range mainCRs {
			// Deleting namespaces is not supported.
			if _, ok := cr.(*corev1.Namespace); !ok {
				err := K8SClient.Delete(testCtx, cr)
				Expect(err).ToNot(HaveOccurred())
			}
		}

		for _, cr := range ctCRs {
			Expect(K8SClient.Delete(testCtx, cr)).To(Succeed())
		}
	})

	BeforeEach(func() {
		for _, cr := range mainCRs {
			crCopy := cr.DeepCopyObject().(client.Object)
			err := K8SClient.Create(testCtx, crCopy)
			if err != nil && !errors.IsAlreadyExists(err) {
				Panic()
			}
		}

		for _, cr := range ctCRs {
			crCopy := cr.DeepCopyObject().(client.Object)
			err := K8SClient.Create(testCtx, crCopy)
			Expect(err).ToNot(HaveOccurred())
		}

		Eventually(func() bool {
			newct := &provisioningv1alpha1.ClusterTemplate{}
			Expect(K8SClient.Get(context.Background(), client.ObjectKeyFromObject(ctComplete), newct)).To(Succeed())
			return newct.Status.Conditions != nil
		}, timeout, interval).Should(BeTrue())

		Eventually(func() bool {
			newct := &provisioningv1alpha1.ClusterTemplate{}
			Expect(K8SClient.Get(context.Background(), client.ObjectKeyFromObject(ctIncomplete), newct)).To(Succeed())
			return newct.Status.Conditions != nil
		}, timeout, interval).Should(BeTrue())
	})

	Context("Provisioning Request is created", func() {
		It("Verify status conditions if ClusterInstance rendering fails", func() {
			crName := "cluster-1"
			// Make sure the needed ClusterTemplate exists.
			oranCT := &provisioningv1alpha1.ClusterTemplate{}
			err := K8SClient.Get(testCtx, client.ObjectKeyFromObject(ctIncomplete), oranCT)
			Expect(err).ToNot(HaveOccurred())

			// Create the ProvisioningRequest.
			ProvRequestCR.Name = crName
			ProvRequestCR.Spec.TemplateParameters = runtime.RawExtension{
				Raw: []byte(testutils.TestFullTemplateParameters),
			}
			copyProvRequestCR := ProvRequestCR.DeepCopy()
			err = K8SClient.Create(testCtx, copyProvRequestCR)
			Expect(err).ToNot(HaveOccurred())

			reconciledPR := &provisioningv1alpha1.ProvisioningRequest{}
			Eventually(func() bool {
				err := K8SClient.Get(testCtx, client.ObjectKeyFromObject(ProvRequestCR), reconciledPR)
				Expect(err).ToNot(HaveOccurred())
				return len(reconciledPR.Status.Conditions) == 2
			}, time.Minute*1, time.Second*3).Should(BeTrue())

			conditions := reconciledPR.Status.Conditions
			// Verify the ProvisioningRequest's status conditions.
			Expect(len(conditions)).To(Equal(2))
			testutils.VerifyStatusCondition(conditions[0], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.Validated),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			testutils.VerifyStatusCondition(conditions[1], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Failed),
				Message: "ClusterInstance.siteconfig.open-cluster-management.io \"cluster-1\" is invalid: spec.nodes[0].templateRefs: Required value",
			})

			// Verify provisioningState is failed when the clusterInstance rendering fails.
			testutils.VerifyProvisioningStatus(reconciledPR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Failed to render and validate ClusterInstance", nil)
		})
	})

	Context("When NodeAllocationRequest has been created", func() {

		It("Verify status when configuration change causes ClusterInstance rendering to fail but NodeAllocationRequest becomes provisioned", func() {
			crName := "cluster-2"
			// Make sure the needed ClusterTemplate exists.
			oranCT := &provisioningv1alpha1.ClusterTemplate{}
			err := K8SClient.Get(testCtx, client.ObjectKeyFromObject(ctComplete), oranCT)
			Expect(err).ToNot(HaveOccurred())

			// Create the ProvisioningRequest.
			ProvRequestCR.Name = crName
			templateParms := strings.Replace(
				testutils.TestFullTemplateParameters, "\"clusterName\": \"cluster-1\"", "\"clusterName\": \"cluster-2\"", 1)
			ProvRequestCR.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(templateParms)}
			copyProvRequestCR := ProvRequestCR.DeepCopy()
			copyProvRequestCR.Spec.TemplateVersion = tVersion2
			err = K8SClient.Create(testCtx, copyProvRequestCR)
			Expect(err).ToNot(HaveOccurred())

			// Eventually, the PR should have 5 conditions.
			reconciledPR := &provisioningv1alpha1.ProvisioningRequest{}
			Eventually(func() bool {
				err := K8SClient.Get(testCtx, client.ObjectKeyFromObject(ProvRequestCR), reconciledPR)
				Expect(err).ToNot(HaveOccurred())
				return len(reconciledPR.Status.Conditions) == 5
			}, time.Minute*1, time.Second*3).Should(BeTrue())

			conditions := reconciledPR.Status.Conditions
			// Verify the ProvisioningRequest's status conditions - the last should be showing that
			// we're waiting for the NodeAllocationRequest.
			Expect(len(conditions)).To(Equal(5))

			testutils.VerifyStatusCondition(conditions[1], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			testutils.VerifyStatusCondition(conditions[4], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
				Status:  metav1.ConditionUnknown,
				Reason:  string(metav1.ConditionUnknown),
				Message: "Waiting for NodeAllocationRequest (cluster-2) to be processed",
			})
			// Verify the provisioningState moves to progressing.
			testutils.VerifyProvisioningStatus(reconciledPR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Waiting for NodeAllocationRequest (cluster-2) to be processed", nil)

			// Patch NodeAllocationRequest provision status to Completed.
			currentNp := &pluginsv1alpha1.NodeAllocationRequest{}
			Expect(K8SClient.Get(ctx, types.NamespacedName{Name: crName, Namespace: utils.UnitTestHwmgrNamespace}, currentNp)).To(Succeed())
			Expect(currentNp.Status.Conditions).To(BeEmpty())
			currentNp.Status.Conditions = []metav1.Condition{
				{
					Status:             metav1.ConditionTrue,
					Reason:             string(hwmgmtv1alpha1.Completed),
					Type:               string(hwmgmtv1alpha1.Provisioned),
					LastTransitionTime: metav1.Now(),
				},
			}
			Expect(K8SClient.Status().Update(ctx, currentNp)).To(Succeed())
			// Create the expected nodes.
			testutils.CreateNodeResources(ctx, K8SClient, currentNp.Name)

			// The ProvisioningRequest should complete hardware provisioning.
			Eventually(func() bool {
				err := K8SClient.Get(testCtx, client.ObjectKeyFromObject(ProvRequestCR), reconciledPR)
				Expect(err).ToNot(HaveOccurred())
				return reconciledPR.Status.Conditions[4].Status == metav1.ConditionTrue
			}, time.Minute*1, time.Second*3).Should(BeTrue())
			conditions = reconciledPR.Status.Conditions
			testutils.VerifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})

			// Update the ProvisioningRequest to use a ClusterTemplate pointing to a ConfigMap that would
			// trigger ClusterInstance dry-run failure.
			Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(ProvRequestCR), reconciledPR)).To(Succeed())
			reconciledPR.Spec.TemplateVersion = tVersion1
			Expect(K8SClient.Update(testCtx, reconciledPR)).To(Succeed())

			Eventually(func() bool {
				err := K8SClient.Get(testCtx, client.ObjectKeyFromObject(ProvRequestCR), reconciledPR)
				Expect(err).ToNot(HaveOccurred())
				return reconciledPR.Status.Conditions[1].Status == metav1.ConditionFalse
			}, time.Minute*1, time.Second*3).Should(BeTrue())

			conditions = reconciledPR.Status.Conditions
			testutils.VerifyStatusCondition(conditions[1], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Failed),
				Message: "ClusterInstance.siteconfig.open-cluster-management.io \"cluster-2\" is invalid: spec.nodes[0].templateRefs: Required value",
			})

			testutils.VerifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			// Verify the provisioningState moves to failed.
			testutils.VerifyProvisioningStatus(reconciledPR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Failed to render and validate ClusterInstance", nil)
		})
	})
})
