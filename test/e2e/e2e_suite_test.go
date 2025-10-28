/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllersE2Etest

import (
	"context"
	"fmt"
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
	"github.com/openshift-kni/oran-o2ims/api/common"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	provisioningcontrollers "github.com/openshift-kni/oran-o2ims/internal/controllers"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	testutils "github.com/openshift-kni/oran-o2ims/test/utils"
	assistedservicev1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
)

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

	// Set the operator namespace for tests
	os.Setenv(constants.DefaultNamespaceEnvName, constants.DefaultNamespace)

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
	err = pluginsv1alpha1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = hivev1.AddToScheme(testScheme)
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
		Client:         K8SClient,
		Logger:         logger,
		CallbackConfig: ctlrutils.NewNarCallbackConfig(constants.DefaultNarCallbackServicePort),
	}
	err = ProvReqTestReconciler.SetupWithManager(K8SManager)
	Expect(err).ToNot(HaveOccurred())

	// Start mock hardware plugin server for e2e tests with Kubernetes client
	mockServer := provisioningcontrollers.NewMockHardwarePluginServerWithClient(K8SClient)

	suiteCrs := []client.Object{
		// oran-o2ims operator namespace
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: constants.DefaultNamespace,
			},
		},
		// Basic auth secret for hardware plugin
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-hwmgr-auth-secret",
				Namespace: constants.DefaultNamespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"username": []byte("test-user"),
				"password": []byte("test-password"),
			},
		},
		// HardwarePlugin CRs
		&hwmgmtv1alpha1.HardwarePlugin{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: constants.DefaultNamespace,
				Name:      testHardwarePluginRef,
			},
			Spec: hwmgmtv1alpha1.HardwarePluginSpec{
				ApiRoot: mockServer.GetURL(),
				AuthClientConfig: &common.AuthClientConfig{
					Type:            common.Basic,
					BasicAuthSecret: stringPtr("test-hwmgr-auth-secret"),
				},
			},
		},
	}

	for _, cr := range suiteCrs {
		err := K8SClient.Create(context.Background(), cr)
		Expect(err).ToNot(HaveOccurred())
	}

	// Update HardwarePlugin status to mark it as registered
	mockHwPlugin := &hwmgmtv1alpha1.HardwarePlugin{}
	err = K8SClient.Get(context.Background(), types.NamespacedName{
		Namespace: constants.DefaultNamespace,
		Name:      testHardwarePluginRef,
	}, mockHwPlugin)
	Expect(err).ToNot(HaveOccurred())

	mockHwPlugin.Status.Conditions = []metav1.Condition{
		{
			Type:               string(hwmgmtv1alpha1.ConditionTypes.Registration),
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             string(hwmgmtv1alpha1.ConditionReasons.Completed),
			Message:            "Mock HardwarePlugin registered successfully for e2e tests",
		},
	}
	err = K8SClient.Status().Update(context.Background(), mockHwPlugin)
	Expect(err).ToNot(HaveOccurred())

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
				ctlrutils.ClusterInstallationTimeoutConfigKey: "60s",
				ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey: `
clusterImageSetNameRef: "4.15.0"
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
				ctlrutils.ClusterInstallationTimeoutConfigKey: "60s",
				ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey: `
clusterImageSetNameRef: "4.15.0"
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
				ctlrutils.ClusterConfigurationTimeoutConfigKey: "1m",
				ctlrutils.PolicyTemplateDefaultsConfigmapKey: `
cpu-isolated: "2-31"
cpu-reserved: "0-1"
defaultHugepagesSize: "1G"`,
			},
		},
		// hardware template
		&hwmgmtv1alpha1.HardwareTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hwTemplate,
				Namespace: ctlrutils.InventoryNamespace,
			},
			Spec: hwmgmtv1alpha1.HardwareTemplateSpec{
				HardwarePluginRef:           ctlrutils.UnitTestHwPluginRef,
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
		// ClusterImageSet for e2e tests
		&hivev1.ClusterImageSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "4.15.0",
			},
			Spec: hivev1.ClusterImageSetSpec{
				ReleaseImage: "quay.io/openshift-release-dev/ocp-release:4.15.0-x86_64",
			},
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
			Release:    "4.15.0",
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
			Release:    "4.15.0",
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
			if err != nil && !errors.IsAlreadyExists(err) {
				Expect(err).ToNot(HaveOccurred())
			}
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

		It("Skips re-rendering the ClusterInstance when configuration changes occur during active provisioning", func() {
			crName := "cluster-2"
			// Make sure the needed ClusterTemplate exists.
			oranCT := &provisioningv1alpha1.ClusterTemplate{}
			err := K8SClient.Get(testCtx, client.ObjectKeyFromObject(ctComplete), oranCT)
			Expect(err).ToNot(HaveOccurred())

			// Create the ProvisioningRequest.
			ProvRequestCR.Name = crName
			templateParms := strings.Replace(
				testutils.TestFullTemplateParameters, "\"clusterName\": \"cluster-1\"", "\"clusterName\": \"cluster-2\"", 1)
			templateParms = strings.Replace(
				templateParms, "\"nodeClusterName\": \"exampleCluster\"", "\"nodeClusterName\": \"cluster-2\"", 1)
			ProvRequestCR.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(templateParms)}
			copyProvRequestCR := ProvRequestCR.DeepCopy()
			copyProvRequestCR.Spec.TemplateVersion = tVersion2
			err = K8SClient.Create(testCtx, copyProvRequestCR)
			Expect(err).ToNot(HaveOccurred())

			// Wait for NodeAllocationRequest to be created and initially have hardware provisioning in progress
			reconciledPR := &provisioningv1alpha1.ProvisioningRequest{}
			Eventually(func() bool {
				err := K8SClient.Get(testCtx, client.ObjectKeyFromObject(ProvRequestCR), reconciledPR)
				Expect(err).ToNot(HaveOccurred())
				// Check that we have at least the basic conditions and hardware provisioning has started
				if len(reconciledPR.Status.Conditions) < 4 {
					return false
				}
				// Look for HardwareProvisioned condition with status False (in progress)
				for _, cond := range reconciledPR.Status.Conditions {
					if cond.Type == string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned) &&
						cond.Status == metav1.ConditionFalse {
						return true
					}
				}
				return false
			}, time.Minute*1, time.Second*3).Should(BeTrue())

			// Verify initial state - hardware provisioning in progress
			conditions := reconciledPR.Status.Conditions
			testutils.VerifyStatusCondition(conditions[0], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.Validated),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			testutils.VerifyStatusCondition(conditions[1], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			// Find and verify HardwareProvisioned condition
			var hwProvCondition *metav1.Condition
			for i := range conditions {
				if conditions[i].Type == string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned) {
					hwProvCondition = &conditions[i]
					break
				}
			}
			Expect(hwProvCondition).ToNot(BeNil())
			testutils.VerifyStatusCondition(*hwProvCondition, metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.InProgress),
				Message: "Hardware provisioning is in progress",
			})
			// Verify the provisioningState moves to progressing.
			testutils.VerifyProvisioningStatus(reconciledPR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Hardware provisioning is in progress", nil)

			// Now provision the NodeAllocationRequest to complete hardware provisioning
			currentNp := &pluginsv1alpha1.NodeAllocationRequest{}
			Expect(K8SClient.Get(ctx, types.NamespacedName{Name: crName, Namespace: constants.DefaultNamespace}, currentNp)).To(Succeed())
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

			// Simulate callback-triggered reconciliation by adding callback annotation to ProvisioningRequest
			// This is needed because hardware status updates now only occur during callback-triggered reconciliations
			// First, get the latest version to ensure we have the correct resourceVersion
			Expect(K8SClient.Get(ctx, client.ObjectKeyFromObject(ProvRequestCR), ProvRequestCR)).To(Succeed())
			if ProvRequestCR.Annotations == nil {
				ProvRequestCR.Annotations = make(map[string]string)
			}
			ProvRequestCR.Annotations[ctlrutils.CallbackReceivedAnnotation] = fmt.Sprintf("%d", time.Now().Unix())
			Expect(K8SClient.Update(ctx, ProvRequestCR)).To(Succeed())

			// Give the reconciler a moment to process the NodeAllocationRequest status update
			time.Sleep(2 * time.Second)

			// The ProvisioningRequest should complete hardware provisioning.
			Eventually(func() bool {
				err := K8SClient.Get(testCtx, client.ObjectKeyFromObject(ProvRequestCR), reconciledPR)
				Expect(err).ToNot(HaveOccurred())
				// Look for HardwareProvisioned condition with status True (completed)
				for _, cond := range reconciledPR.Status.Conditions {
					if cond.Type == string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned) &&
						cond.Status == metav1.ConditionTrue {
						return true
					}
				}
				return false
			}, time.Minute*1, time.Second*3).Should(BeTrue())
			conditions = reconciledPR.Status.Conditions
			// Find and verify HardwareProvisioned condition is now completed
			hwProvCondition = nil
			for i := range conditions {
				if conditions[i].Type == string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned) {
					hwProvCondition = &conditions[i]
					break
				}
			}
			Expect(hwProvCondition).ToNot(BeNil())
			testutils.VerifyStatusCondition(*hwProvCondition, metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})

			// Update the ProvisioningRequest to use a ClusterTemplate pointing to a ConfigMap that would attempt to
			// trigger re-rendering the ClusterInstance.
			Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(ProvRequestCR), reconciledPR)).To(Succeed())
			reconciledPR.Spec.TemplateVersion = tVersion1
			Expect(K8SClient.Update(testCtx, reconciledPR)).To(Succeed())

			// With the HardwareProvisioned condition set to True, the ClusterInstance should be created.
			// We now expect to skip re-rendering the ClusterInstance and mitigate a possible dry-run failure.
			err = K8SClient.Get(testCtx, client.ObjectKeyFromObject(ProvRequestCR), reconciledPR)
			Expect(err).ToNot(HaveOccurred())
			conditions = reconciledPR.Status.Conditions
			testutils.VerifyStatusCondition(conditions[1], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered),
				Status:  metav1.ConditionTrue,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Completed),
				Message: "ClusterInstance rendered and passed dry-run validation",
			})

			// Find and verify HardwareProvisioned condition after template version change
			hwProvConditionAfterChange := &metav1.Condition{}
			found := false
			for i := range conditions {
				if conditions[i].Type == string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned) {
					hwProvConditionAfterChange = &conditions[i]
					found = true
					break
				}
			}
			Expect(found).To(BeTrue(), "HardwareProvisioned condition should exist")
			testutils.VerifyStatusCondition(*hwProvConditionAfterChange, metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})

			// Verify the provisioningState is progressing.
			testutils.VerifyProvisioningStatus(reconciledPR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, fmt.Sprintf("Waiting for ClusterInstance (%s) to be processed", crName), nil)
		})
	})
})

// stringPtr is a helper function to get a pointer to a string
func stringPtr(s string) *string {
	return &s
}
