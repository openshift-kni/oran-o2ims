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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	observabilityv1beta1 "github.com/stolostron/multicluster-observability-operator/operators/multiclusterobservability/api/v1beta1"
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
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	ibgu "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/api/common"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	metal3pluginscontrollers "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/metal3/controller"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	provisioningcontrollers "github.com/openshift-kni/oran-o2ims/internal/controllers"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	testutils "github.com/openshift-kni/oran-o2ims/test/utils"
	assistedservicev1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
)

var (
	K8SClient                           client.Client
	ProvisioningManager                 ctrl.Manager
	Metal3Manager                       ctrl.Manager
	ProvisioningRequestTestReconciler   *provisioningcontrollers.ProvisioningRequestReconciler
	ClusterTemplateTestReconciler       *provisioningcontrollers.ClusterTemplateReconciler
	NodeAllocationRequestTestReconciler *metal3pluginscontrollers.NodeAllocationRequestReconciler
	AllocatedNodeTestReconciler         *metal3pluginscontrollers.AllocatedNodeReconciler
	testEnv                             *envtest.Environment
	ctx                                 context.Context
	cancel                              context.CancelFunc
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
	err = metal3v1alpha1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = observabilityv1beta1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())

	// Get the needed external CRDs. Their details are under test/utils/vars.go - ExternalCrdsData.
	// Update that with any other CRDs that the provisioning controller depends on.
	err = testutils.GetExternalCrdFiles(tmpDir)
	Expect(err).ToNot(HaveOccurred())
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

	// Get the managers for O2IMS controllers.
	ProvisioningManager, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: testScheme,
	})
	Expect(err).ToNot(HaveOccurred())
	Expect(ProvisioningManager).NotTo(BeNil())

	// Get a separate manager for Metal3 controllers (simulates separate pod deployment).
	Metal3Manager, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: testScheme,
		Metrics: metricsserver.Options{
			BindAddress: ":8081", // Use different port to avoid conflict
		},
	})
	Expect(err).ToNot(HaveOccurred())
	Expect(Metal3Manager).NotTo(BeNil())

	// Get the client.
	K8SClient, err = client.New(cfg, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(K8SClient).NotTo(BeNil())

	// Setup the ClusterTemplate Reconciler.
	ClusterTemplateTestReconciler = &provisioningcontrollers.ClusterTemplateReconciler{
		Client: K8SClient,
		Logger: logger,
	}
	err = ClusterTemplateTestReconciler.SetupWithManager(ProvisioningManager)
	Expect(err).ToNot(HaveOccurred())

	// Initialize NodeAllocationRequest utils for Metal3 controllers
	err = hwmgrutils.InitNodeAllocationRequestUtils(testScheme)
	Expect(err).ToNot(HaveOccurred())

	// Setup Metal3 controllers on separate manager (simulates separate pod deployment)
	metal3controllers, err := metal3pluginscontrollers.SetupMetal3Controllers(Metal3Manager, constants.DefaultNamespace, logger)
	Expect(err).ToNot(HaveOccurred())
	NodeAllocationRequestTestReconciler = metal3controllers.NodeAllocationReconciler
	AllocatedNodeTestReconciler = metal3controllers.AllocatedNodeReconciler

	// Setup the ProvisioningRequest Reconciler on main manager.
	ProvReqTestReconciler := &provisioningcontrollers.ProvisioningRequestReconciler{
		Client:         K8SClient,
		Logger:         logger,
		CallbackConfig: ctlrutils.NewNarCallbackConfig(constants.DefaultNarCallbackServicePort),
	}
	err = ProvReqTestReconciler.SetupWithManager(ProvisioningManager)
	Expect(err).ToNot(HaveOccurred())

	// Start mock hardware plugin server for e2e tests with Kubernetes client
	// Use queryKubernetes=true to make the mock server query real Kubernetes resources
	// instead of returning default mock data, enabling integration with Metal3 controllers
	mockServer := provisioningcontrollers.NewMockHardwarePluginServerWithK8SQuery(K8SClient, true)

	suiteCrs := []client.Object{
		// oran-o2ims operator namespace
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: constants.DefaultNamespace,
			},
		},
		// BMH Pool namespace
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testutils.BmhPoolName,
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
		// HardwarePlugin CRs - must be in HWMGR_PLUGIN_NAMESPACE where controller looks for it
		&hwmgmtv1alpha1.HardwarePlugin{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: constants.DefaultNamespace,
				Name:      testutils.TestHwPluginRef,
			},
			Spec: hwmgmtv1alpha1.HardwarePluginSpec{
				ApiRoot: mockServer.GetURL(),
				AuthClientConfig: &common.AuthClientConfig{
					Type:            common.Basic,
					BasicAuthSecret: stringPtr("test-hwmgr-auth-secret"),
				},
			},
		},
		// HardwareProfile
		&hwmgmtv1alpha1.HardwareProfile{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testutils.TestHwProfileName,
				Namespace: constants.DefaultNamespace,
			},
			Spec: hwmgmtv1alpha1.HardwareProfileSpec{
				// Basic hardware profile spec - minimal firmware config to satisfy CRD validation
				BiosFirmware: hwmgmtv1alpha1.Firmware{
					Version: "test-bios-v1.0",
					URL:     "https://example.com/bios-firmware.bin",
				},
			},
		},
		// HardwareTemplate Blue
		&hwmgmtv1alpha1.HardwareTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testutils.TestHwTemplateBlue,
				Namespace: ctlrutils.InventoryNamespace,
			},
			Spec: hwmgmtv1alpha1.HardwareTemplateSpec{
				HardwarePluginRef:           testutils.TestHwPluginRef,
				BootInterfaceLabel:          "bootable-interface",
				HardwareProvisioningTimeout: "10m",
				NodeGroupData: []hwmgmtv1alpha1.NodeGroupData{
					{
						Name:           "single-node",
						Role:           "master",
						ResourcePoolId: testutils.TestPoolID,
						HwProfile:      testutils.TestHwProfileName,
						ResourceSelector: map[string]string{
							"resourceselector.clcm.openshift.io/server-colour": "blue",
							"resourceselector.clcm.openshift.io/server-type":   testutils.TestServerType,
							"hardwaredata/cpu_arch":                            "x86_64",
							"hardwaredata/storage;sizeBytes>500000000000":      "present",
							"hardwaredata/ramMebibytes;gt":                     "65536",
						},
					},
				},
			},
		},
		// HardwareTemplate Green
		&hwmgmtv1alpha1.HardwareTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testutils.TestHwTemplateGreen,
				Namespace: ctlrutils.InventoryNamespace,
			},
			Spec: hwmgmtv1alpha1.HardwareTemplateSpec{
				HardwarePluginRef:           testutils.TestHwPluginRef,
				BootInterfaceLabel:          "bootable-interface",
				HardwareProvisioningTimeout: "10m",
				NodeGroupData: []hwmgmtv1alpha1.NodeGroupData{
					{
						Name:           "single-node",
						Role:           "master",
						ResourcePoolId: testutils.TestPoolID,
						HwProfile:      testutils.TestHwProfileName,
						ResourceSelector: map[string]string{
							"resourceselector.clcm.openshift.io/server-colour": "green",
						},
					},
				},
			},
		},
	}

	// Create BareMetalHosts and associated resources in BeforeSuite
	for _, bmhData := range testutils.TestBMHs {
		bmh := testutils.CreateBareMetalHost(bmhData)
		hwData := testutils.CreateHardwareData(bmhData.Name, bmhData)
		bmcSecret := testutils.CreateBMCSecret(bmhData.Name)

		suiteCrs = append(suiteCrs, bmh, hwData, bmcSecret)
	}

	for _, cr := range suiteCrs {
		err := K8SClient.Create(context.Background(), cr)
		Expect(err).ToNot(HaveOccurred())
	}

	// Update HardwarePlugin status to mark it as registered
	mockHwPlugin := &hwmgmtv1alpha1.HardwarePlugin{}
	err = K8SClient.Get(context.Background(), types.NamespacedName{
		Namespace: constants.DefaultNamespace,
		Name:      testutils.TestHwPluginRef,
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

	// Update BMH status to make them Available for controller selection
	for _, bmhData := range testutils.TestBMHs {
		bmh := &metal3v1alpha1.BareMetalHost{}
		Expect(K8SClient.Get(context.Background(), types.NamespacedName{
			Name:      bmhData.Name,
			Namespace: constants.DefaultNamespace,
		}, bmh)).To(Succeed())

		// Add interface labels to BMH so the controller can map interface names to labels
		// Label KEY contains the interface label name, VALUE contains the interface name that should match
		if bmh.Labels == nil {
			bmh.Labels = make(map[string]string)
		}
		bmh.Labels["interfacelabel.clcm.openshift.io/bootable-interface"] = "eno1"
		bmh.Labels["interfacelabel.clcm.openshift.io/base-interface"] = "eth0"
		bmh.Labels["interfacelabel.clcm.openshift.io/data-interface"] = "eth1"
		Expect(K8SClient.Update(context.Background(), bmh)).To(Succeed())

		// Set the status with hardware details and Available state
		hostname := bmhData.Hostname
		if bmhData.Name == "bmh-2" {
			// bmh-2 will be selected, so use hostname that matches ClusterInstance template
			hostname = "node1"
		}
		bmh.Status = metal3v1alpha1.BareMetalHostStatus{
			Provisioning: metal3v1alpha1.ProvisionStatus{
				State: metal3v1alpha1.StateAvailable,
			},
			HardwareDetails: &metal3v1alpha1.HardwareDetails{
				Hostname: hostname,
				CPU: metal3v1alpha1.CPU{
					Arch: "x86_64",
				},
				RAMMebibytes: int(bmhData.RamMB),
				NIC: []metal3v1alpha1.NIC{
					{
						Name: "eno1",
						MAC:  bmhData.MacAddress,
					},
					{
						Name: "eth0",
						MAC:  fmt.Sprintf("%s10", bmhData.MacAddress[:15]),
					},
					{
						Name: "eth1",
						MAC:  fmt.Sprintf("%s20", bmhData.MacAddress[:15]),
					},
				},
				Storage: []metal3v1alpha1.Storage{
					{
						Name:         "sda",
						SizeBytes:    1000000000000, // 1TB
						Rotational:   false,         // SSD
						Type:         "SSD",
						Model:        "Samsung SSD 980 PRO 1TB",
						SerialNumber: fmt.Sprintf("SN-%s", bmhData.Name),
					},
				},
			},
		}
		Expect(K8SClient.Status().Update(context.Background(), bmh)).To(Succeed())
	}

	NodeAllocationRequestTestReconciler.InitializeCallbackContext(ctx)

	// Start the main O2IMS manager
	go func() {
		defer GinkgoRecover()
		err = ProvisioningManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run main manager")
	}()

	// Start the Metal3 manager
	go func() {
		defer GinkgoRecover()
		err = Metal3Manager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run Metal3 manager")
	}()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

// stringPtr is a helper function to get a pointer to a string
func stringPtr(s string) *string {
	return &s
}

var _ = Describe("End-to-end ProvisioningRequestReconcile with metal3 plugin", Ordered, func() {
	// This test suite runs with both O2IMS and Metal3 controllers active
	// Metal3 manager runs alongside Provisioning manager for all tests
	const timeout = time.Second * 60
	const interval = time.Second * 3

	var (
		testCtx                context.Context
		ProvRequestCR          *provisioningv1alpha1.ProvisioningRequest
		reconciledPR           *provisioningv1alpha1.ProvisioningRequest
		ctIncomplete           *provisioningv1alpha1.ClusterTemplate
		ctComplete             *provisioningv1alpha1.ClusterTemplate
		tName                  = "clustertemplate-a"
		tVersion1              = "v1.0.0"
		tVersion2              = "v2.0.0"
		ctNamespace            = "clustertemplate-a-v4-16"
		ciDefaultsCmIncomplete = "clusterinstance-defaults-v1"
		ciDefaultsCmComplete   = "clusterinstance-defaults-v2"
		crName                 = ""
		ptDefaultsCm           = "policytemplate-defaults-v1"
		allocatedNode          *pluginsv1alpha1.AllocatedNode
		nar                    *pluginsv1alpha1.NodeAllocationRequest
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
				HwTemplate:              testutils.TestHwTemplateGreen,
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
				HwTemplate:              testutils.TestHwTemplateBlue,
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

	BeforeAll(func() {
		reconciledPR = &provisioningv1alpha1.ProvisioningRequest{}
		allocatedNode = &pluginsv1alpha1.AllocatedNode{}
		nar = &pluginsv1alpha1.NodeAllocationRequest{}

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

	It("Verify status conditions if ClusterInstance rendering fails", func() {
		crName = "cluster-1"
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

		Eventually(func() bool {
			err := K8SClient.Get(testCtx, client.ObjectKeyFromObject(ProvRequestCR), reconciledPR)
			Expect(err).ToNot(HaveOccurred())
			return len(reconciledPR.Status.Conditions) == 2
		}, time.Minute*3, time.Second*3).Should(BeTrue())

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

	It("Starts hardware provisioning", func() {
		crName = "cluster-2"
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

		// Wait for hardware provisioning to start.
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
		}, time.Minute*3, time.Second*3).Should(BeTrue())

		// Verify initial state - hardware provisioning in progress.
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
	})

	It("Creates NodeAllocationRequest and AllocatedNode", func() {
		// Wait for NodeAllocationRequest to be created.
		Eventually(func() bool {
			// Check if the NodeAllocationRequest has been created
			err := K8SClient.Get(testCtx, types.NamespacedName{
				Name:      crName,
				Namespace: constants.DefaultNamespace,
			}, nar)
			Expect(err).ToNot(HaveOccurred())
			return true
		}, time.Minute*3, time.Second*3).Should(BeTrue())

		// Verify NodeAllocationRequest contains expected node groups (only 1 single-node group).
		Expect(len(nar.Spec.NodeGroup)).To(Equal(1))

		// Verify the single node group.
		singleNodeGroup := nar.Spec.NodeGroup[0]
		Expect(singleNodeGroup.NodeGroupData.Name).To(Equal("single-node"))
		Expect(singleNodeGroup.Size).To(Equal(1))
		Expect(singleNodeGroup.NodeGroupData.HwProfile).To(Equal(testutils.TestHwProfileName))

		// Wait for Metal3 controllers to automatically create AllocatedNode resources.
		allocatedNodes := &pluginsv1alpha1.AllocatedNodeList{}
		Eventually(func() bool {
			err := K8SClient.List(testCtx, allocatedNodes, client.InNamespace(constants.DefaultNamespace))
			Expect(err).ToNot(HaveOccurred())
			return len(allocatedNodes.Items) == 1 && allocatedNodes.Items[0].Spec.NodeAllocationRequest == crName
		}, time.Minute*3, time.Second*3).Should(BeTrue())

		// Save the single allocated node for later use
		allocatedNode = &allocatedNodes.Items[0]
	})

	It("Updates the BMHs with the right labels", func() {
		// Test that the allocatedNode is bmh-2, even though bmh-4 also matches the selection criteria.
		Expect(allocatedNode.Spec.HwMgrNodeId).To(Equal("bmh-2"))

		// Get the BMH.
		bmh := &metal3v1alpha1.BareMetalHost{}
		err := K8SClient.Get(testCtx, types.NamespacedName{
			Name: allocatedNode.Spec.HwMgrNodeId, Namespace: allocatedNode.Spec.HwMgrNodeNs}, bmh)
		Expect(err).ToNot(HaveOccurred())

		// Check that the allocation labels are present.
		Expect(bmh.Labels).To(HaveKey("clcm.openshift.io/allocated"))
		Expect(bmh.Labels["clcm.openshift.io/allocated"]).To(Equal("true"))

		// Verify the AllocatedNode references the correct NodeAllocationRequest
		Expect(allocatedNode.Spec.NodeAllocationRequest).To(Equal(crName))

		// Verify that bmh-1, bmh-3 and bmh-4 (non-selected BMHs) do NOT have allocation labels, even though
		// bmh-4 also matches the selection criteria (identical to bmh-2).
		nonSelectedBMHs := []string{"bmh-1", "bmh-3", "bmh-4"}
		for _, bmhName := range nonSelectedBMHs {
			nonSelectedBMH := &metal3v1alpha1.BareMetalHost{}
			Expect(K8SClient.Get(testCtx, types.NamespacedName{
				Name:      bmhName,
				Namespace: constants.DefaultNamespace,
			}, nonSelectedBMH)).To(Succeed())

			// Check that allocation labels are NOT present.
			Expect(nonSelectedBMH.Labels).ToNot(HaveKey("clcm.openshift.io/allocated"),
				fmt.Sprintf("BMH %s should not have allocated label since it was not selected", bmhName))
			Expect(nonSelectedBMH.Labels).ToNot(HaveKey("clcm.openshift.io/allocated-node"),
				fmt.Sprintf("BMH %s should not have allocated-node label since it was not selected", bmhName))
		}
	})

	It("NodeAllocationRequest and AllocatedNode complete", func() {
		// Get the BMH.
		bmh := &metal3v1alpha1.BareMetalHost{}
		err := K8SClient.Get(testCtx, types.NamespacedName{
			Name: allocatedNode.Spec.HwMgrNodeId, Namespace: allocatedNode.Spec.HwMgrNodeNs}, bmh)
		Expect(err).ToNot(HaveOccurred())

		// Check that the BMH has the FirmwareUpdateNeededAnnotation set.
		Expect(bmh.Annotations[metal3pluginscontrollers.FirmwareUpdateNeededAnnotation]).To(Equal("true"))

		// Update the BMH status to Preparing.
		bmh.Status.Provisioning.State = metal3v1alpha1.StatePreparing
		bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusOK
		Expect(K8SClient.Status().Update(testCtx, bmh)).To(Succeed())

		// Check that the AllocatedNode has the config-in-progress annotation.
		Eventually(func() bool {
			err := K8SClient.Get(testCtx, types.NamespacedName{Name: allocatedNode.Name, Namespace: constants.DefaultNamespace}, allocatedNode)
			Expect(err).ToNot(HaveOccurred())
			return allocatedNode.Annotations[metal3pluginscontrollers.ConfigAnnotation] != ""
		}, time.Minute*3, time.Second*3).Should(BeTrue())

		// Check if the HostFirmwareComponents already exists.
		hfc := &metal3v1alpha1.HostFirmwareComponents{}
		err = K8SClient.Get(testCtx, types.NamespacedName{
			Name: allocatedNode.Spec.HwMgrNodeId, Namespace: allocatedNode.Spec.HwMgrNodeNs}, hfc)
		Expect(err).ToNot(HaveOccurred())

		hfc.Status = metal3v1alpha1.HostFirmwareComponentsStatus{
			Components: []metal3v1alpha1.FirmwareComponentStatus{
				{
					Component:      "bios",
					CurrentVersion: "test-bios-v1.0", // Match the HardwareProfile requirement
				},
			},
			Conditions: []metav1.Condition{
				{
					Type:               string(metal3v1alpha1.HostFirmwareComponentsValid),
					Status:             metav1.ConditionTrue,
					Reason:             "Valid",
					Message:            "All firmware components are valid",
					LastTransitionTime: metav1.Now(),
					ObservedGeneration: 1,
				},
				{
					Type:               string(metal3v1alpha1.HostFirmwareComponentsChangeDetected),
					Status:             metav1.ConditionFalse,
					Reason:             "NoChangesNeeded",
					Message:            "No firmware changes required",
					LastTransitionTime: metav1.Now(),
					ObservedGeneration: 1,
				},
			},
		}
		// Update the status of the HostFirmwareComponents
		Expect(K8SClient.Status().Update(testCtx, hfc)).To(Succeed())

		// Create PreprovisioningImage to satisfy the network data clearing logic.
		ppi := &metal3v1alpha1.PreprovisioningImage{
			ObjectMeta: metav1.ObjectMeta{
				Name:      allocatedNode.Spec.HwMgrNodeId,
				Namespace: allocatedNode.Spec.HwMgrNodeNs,
			},
			Status: metal3v1alpha1.PreprovisioningImageStatus{
				NetworkData: metal3v1alpha1.SecretStatus{
					Name:    "", // Empty to indicate network data is cleared
					Version: "", // Empty to indicate network data is cleared
				},
			},
		}
		Expect(K8SClient.Create(testCtx, ppi)).To(Succeed())
		Expect(K8SClient.Status().Update(testCtx, ppi)).To(Succeed())
		// Update the BMH status to Available.
		// Get the updated BMH.
		err = K8SClient.Get(testCtx, types.NamespacedName{
			Name: allocatedNode.Spec.HwMgrNodeId, Namespace: allocatedNode.Spec.HwMgrNodeNs}, bmh)
		Expect(err).ToNot(HaveOccurred())
		bmh.Status.Provisioning.State = metal3v1alpha1.StateAvailable
		bmh.Status.OperationalStatus = metal3v1alpha1.OperationalStatusOK
		Expect(K8SClient.Status().Update(testCtx, bmh)).To(Succeed())
		// Remove the FirmwareUpdateNeededAnnotation.
		delete(bmh.Annotations, metal3pluginscontrollers.FirmwareUpdateNeededAnnotation)
		Expect(K8SClient.Update(testCtx, bmh)).To(Succeed())

		// Make sure the NodeAllocationRequest and AllocatedNode are completed.
		Eventually(func() bool {
			err := K8SClient.Get(testCtx, client.ObjectKeyFromObject(nar), nar)
			Expect(err).ToNot(HaveOccurred())
			err = K8SClient.Get(testCtx, types.NamespacedName{Name: allocatedNode.Name, Namespace: constants.DefaultNamespace}, allocatedNode)
			Expect(err).ToNot(HaveOccurred())

			return nar.Status.Conditions[0].Status == metav1.ConditionTrue && allocatedNode.Status.Conditions[0].Status == metav1.ConditionTrue
		}, time.Minute*3, time.Second*5).Should(BeTrue())
		conditions := nar.Status.Conditions
		testutils.VerifyStatusCondition(conditions[0], metav1.Condition{
			Type:   string(hwmgmtv1alpha1.Provisioned),
			Status: metav1.ConditionTrue,
			Reason: string(hwmgmtv1alpha1.Completed),
		})
	})

	It("Completes hardware provisioning", func() {
		// Trigger callback-based reconciliation to notify ProvisioningRequest that hardware provisioning is complete.
		// This simulates the hardware plugin sending a completion callback.
		Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(ProvRequestCR), ProvRequestCR)).To(Succeed())
		if ProvRequestCR.Annotations == nil {
			ProvRequestCR.Annotations = make(map[string]string)
		}
		ProvRequestCR.Annotations[ctlrutils.CallbackReceivedAnnotation] = fmt.Sprintf("%d", time.Now().Unix())
		Expect(K8SClient.Update(testCtx, ProvRequestCR)).To(Succeed())

		// The ProvisioningRequest should complete hardware provisioning.
		Eventually(func() bool {
			err := K8SClient.Get(testCtx, client.ObjectKeyFromObject(ProvRequestCR), reconciledPR)
			Expect(err).ToNot(HaveOccurred())
			// Look for HardwareProvisioned condition with status True (completed).
			for _, cond := range reconciledPR.Status.Conditions {
				if cond.Type == string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned) &&
					cond.Status == metav1.ConditionTrue {
					return true
				}
			}
			return false
		}, time.Minute*3, time.Second*5).Should(BeTrue())

		conditions := reconciledPR.Status.Conditions
		// Find and verify HardwareProvisioned condition is now completed.
		var hwProvCondition *metav1.Condition
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

	})

	It("Skips re-rendering the ClusterInstance when configuration changes occur during active provisioning", func() {
		// Wait for ProvisioningRequest to move to cluster installation phase.
		// This should update the ProvisioningDetails from hardware provisioning to cluster installation.
		Eventually(func() bool {
			err := K8SClient.Get(testCtx, client.ObjectKeyFromObject(ProvRequestCR), reconciledPR)
			Expect(err).ToNot(HaveOccurred())
			// Check if ProvisioningDetails moved from hardware provisioning message.
			return !strings.Contains(reconciledPR.Status.ProvisioningStatus.ProvisioningDetails, "Hardware provisioning")
		}, time.Minute*3, time.Second*5).Should(BeTrue())

		// Update the ProvisioningRequest to use a ClusterTemplate pointing to a ConfigMap that would attempt to
		// trigger re-rendering the ClusterInstance.
		Expect(K8SClient.Get(testCtx, client.ObjectKeyFromObject(ProvRequestCR), reconciledPR)).To(Succeed())
		reconciledPR.Spec.TemplateVersion = tVersion1
		Expect(K8SClient.Update(testCtx, reconciledPR)).To(Succeed())

		// With the HardwareProvisioned condition set to True, the ClusterInstance should be created.
		// We now expect to skip re-rendering the ClusterInstance and mitigate a possible dry-run failure.
		err := K8SClient.Get(testCtx, client.ObjectKeyFromObject(ProvRequestCR), reconciledPR)
		Expect(err).ToNot(HaveOccurred())
		conditions := reconciledPR.Status.Conditions
		testutils.VerifyStatusCondition(conditions[1], metav1.Condition{
			Type:    string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered),
			Status:  metav1.ConditionTrue,
			Reason:  string(provisioningv1alpha1.CRconditionReasons.Completed),
			Message: "ClusterInstance rendered and passed dry-run validation",
		})

		// Find and verify HardwareProvisioned condition after template version change.
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
