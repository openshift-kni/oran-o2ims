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
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	ibgu "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	provisioningcontrollers "github.com/openshift-kni/oran-o2ims/internal/controllers"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	testutils "github.com/openshift-kni/oran-o2ims/test/utils"
	assistedservicev1beta1 "github.com/openshift/assisted-service/api/v1beta1"
)

const testHwMgrPluginNameSpace = "hwmgr"

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
	err = hwv1alpha1.AddToScheme(testScheme)
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

	namespaceCrs := []client.Object{
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
	}

	for _, cr := range namespaceCrs {
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
		ctx           context.Context
		ProvRequestCR *provisioningv1alpha1.ProvisioningRequest
		ct            *provisioningv1alpha1.ClusterTemplate
		tName         = "clustertemplate-a"
		tVersion      = "v1.0.0"
		ctNamespace   = "clustertemplate-a-v4-16"
		ciDefaultsCm  = "clusterinstance-defaults-v1"
		ptDefaultsCm  = "policytemplate-defaults-v1"
		hwTemplate    = "hwtemplate-v1"
		crName        = "aaaaaaaa-ac56-4143-9b10-d1a71517d04f"
	)

	BeforeEach(func() {
		ctx = context.Background()

		crs := []client.Object{
			// Cluster Template Namespace
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ctNamespace,
				},
			},
			// Configmap for ClusterInstance defaults
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ciDefaultsCm,
					Namespace: ctNamespace,
				},
				Data: map[string]string{
					utils.ClusterInstallationTimeoutConfigKey: "60s",
					utils.ClusterInstanceTemplateDefaultsConfigmapKey: `
    clusterImageSetNameRef: "4.15"
    pullSecretRef:
      name: "pull-secret"
    templateRefs:
    - name: "ai-cluster-templates-v1"
      namespace: "siteconfig-operator"
    nodes:
    - role: master
      bootMode: UEFI
      nodeNetwork:
        interfaces:
        - name: eno1
          label: bootable-interface
        - name: eth0
          label: base-interface
        - name: eth1
          label: data-interface
      templateRefs:
      - name: "ai-node-templates-v1"
        namespace: "siteconfig-operator"
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
			&hwv1alpha1.HardwareTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      hwTemplate,
					Namespace: utils.InventoryNamespace,
				},
				Spec: hwv1alpha1.HardwareTemplateSpec{
					HwMgrId:                     utils.UnitTestHwmgrID,
					BootInterfaceLabel:          "bootable-interface",
					HardwareProvisioningTimeout: "1m",
					NodePoolData: []hwv1alpha1.NodePoolData{
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
			},
		}
		// Define the cluster template.
		ct = &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      provisioningcontrollers.GetClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:       tName,
				Version:    tVersion,
				TemplateID: "57b39bda-ac56-4143-9b10-d1a71517d04f",
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
					HwTemplate:              hwTemplate,
				},
				TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testutils.TestFullTemplateSchema)},
			},
		}

		// Define the provisioning request.
		ProvRequestCR = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:       crName,
				Finalizers: []string{provisioningv1alpha1.ProvisioningRequestFinalizer},
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				Name:            "test",
				Description:     "description",
				TemplateName:    tName,
				TemplateVersion: tVersion,
				TemplateParameters: runtime.RawExtension{
					Raw: []byte(testutils.TestFullTemplateParameters),
				},
			},
		}

		crs = append(crs, ct)
		for _, cr := range crs {
			Expect(K8SClient.Create(ctx, cr)).To(Succeed())
		}

		Eventually(func() bool {
			newct := &provisioningv1alpha1.ClusterTemplate{}
			Expect(K8SClient.Get(context.Background(), client.ObjectKeyFromObject(ct), newct)).To(Succeed())
			return newct.Status.Conditions != nil
		}, timeout, interval).Should(BeTrue())
	})

	Context("Provisioning Request is created", func() {
		It("Simple test for validating functionality", func() {

			err := K8SClient.Get(ctx, client.ObjectKeyFromObject(ct), ct)
			Expect(err).ToNot(HaveOccurred())

			// Create the ProvisioningRequest.
			err = K8SClient.Create(ctx, ProvRequestCR)
			Expect(err).ToNot(HaveOccurred())

			reconciledPR := &provisioningv1alpha1.ProvisioningRequest{}
			Eventually(func() bool {
				err := K8SClient.Get(ctx, client.ObjectKeyFromObject(ProvRequestCR), reconciledPR)
				Expect(err).ToNot(HaveOccurred())
				return reconciledPR.Status.Conditions != nil
			}, timeout, interval).Should(BeTrue())

			conditions := reconciledPR.Status.Conditions
			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(3))
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
			testutils.VerifyStatusCondition(conditions[2], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterResourcesCreated),
				Status: metav1.ConditionFalse,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Failed),
			})

			// Verify provisioningState is failed when the clusterInstance rendering fails.
			testutils.VerifyProvisioningStatus(reconciledPR.Status.ProvisioningStatus,
				provisioningv1alpha1.StatePending, "Validating and preparing resources", nil)
		})
	})
})
