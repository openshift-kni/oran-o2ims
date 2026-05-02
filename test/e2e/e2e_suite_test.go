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

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	observabilityv1beta1 "github.com/stolostron/multicluster-observability-operator/operators/multiclusterobservability/api/v1beta1"
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
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	ibgu "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	metal3pluginscontrollers "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/metal3/controller"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	provisioningcontrollers "github.com/openshift-kni/oran-o2ims/internal/controllers"
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

	// Override Metal3 NoncachedClient to use the same direct K8SClient used by
	// provisioning controllers and test assertions, avoiding envtest watchcache
	// timing discrepancies between different API reader instances.
	// Client (cached) is kept as mgr.GetClient() because it has field indexers
	// (e.g., spec.nodeAllocationRequest) required by field selector queries.
	NodeAllocationRequestTestReconciler.NoncachedClient = K8SClient
	AllocatedNodeTestReconciler.NoncachedClient = K8SClient
	metal3controllers.HostFirmwareComponentsReconciler.NoncachedClient = K8SClient

	// Setup the ProvisioningRequest Reconciler on main manager.
	ProvReqTestReconciler := &provisioningcontrollers.ProvisioningRequestReconciler{
		Client: K8SClient,
		Logger: logger,
	}
	err = ProvReqTestReconciler.SetupWithManager(ProvisioningManager)
	Expect(err).ToNot(HaveOccurred())

	suiteCrs := []client.Object{
		// oran-o2ims operator namespace
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: constants.DefaultNamespace,
			},
		},
	}

	for _, cr := range suiteCrs {
		err := K8SClient.Create(context.Background(), cr)
		Expect(err).ToNot(HaveOccurred())
	}

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
