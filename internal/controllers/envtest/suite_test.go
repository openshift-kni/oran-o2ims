/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package envtest

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	bmhv1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8senvtest "sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers"
)

const testNamespace = "test-controllers"

var (
	testEnv   *k8senvtest.Environment
	k8sClient client.Client
	ctx       context.Context
	cancel    context.CancelFunc
	logger    *slog.Logger
)

const bmhCRDURLTemplate = "https://raw.githubusercontent.com/metal3-io/baremetal-operator/%s/config/base/crds/bases/metal3.io_baremetalhosts.yaml"

// getBMHVersionFromGoMod extracts the baremetal-operator version from go.mod
func getBMHVersionFromGoMod() (string, error) {
	goModPath := filepath.Join("..", "..", "..", "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return "", fmt.Errorf("failed to read go.mod: %w", err)
	}

	// Match: github.com/metal3-io/baremetal-operator/apis vX.Y.Z
	re := regexp.MustCompile(`github\.com/metal3-io/baremetal-operator/apis\s+(v[\d.]+)`)
	matches := re.FindSubmatch(data)
	if len(matches) < 2 {
		return "", fmt.Errorf("could not find baremetal-operator version in go.mod")
	}
	return string(matches[1]), nil
}

// validateCRDContent checks that the downloaded content is a valid Kubernetes CRD YAML
func validateCRDContent(data []byte) error {
	content := string(data)

	// Check it's not an HTML page (GitHub error pages start with <!DOCTYPE or <html)
	if strings.HasPrefix(strings.TrimSpace(content), "<") {
		return fmt.Errorf("received HTML instead of YAML")
	}

	// Check for expected CRD markers
	if !strings.Contains(content, "kind: CustomResourceDefinition") {
		return fmt.Errorf("missing 'kind: CustomResourceDefinition'")
	}

	return nil
}

// downloadBMHCRD downloads the BareMetalHost CRD from the upstream repository
func downloadBMHCRD() (string, error) {
	version, err := getBMHVersionFromGoMod()
	if err != nil {
		return "", fmt.Errorf("failed to get BMH version: %w", err)
	}

	url := fmt.Sprintf(bmhCRDURLTemplate, version)
	destPath := filepath.Join("testdata", "metal3.io_baremetalhosts.yaml")

	resp, err := http.Get(url) //nolint:gosec // URL is constructed from trusted template
	if err != nil {
		return "", fmt.Errorf("failed to download BMH CRD: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download BMH CRD: status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read BMH CRD response: %w", err)
	}

	// Validate the downloaded content is a valid CRD YAML, not an HTML error page
	if err := validateCRDContent(data); err != nil {
		return "", fmt.Errorf("downloaded content is not a valid CRD: %w", err)
	}

	if err := os.WriteFile(destPath, data, 0o600); err != nil {
		return "", fmt.Errorf("failed to write BMH CRD: %w", err)
	}

	return version, nil
}

func TestControllersEnvtest(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controllers Envtest Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.Background())

	// Create a logger for tests
	options := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	handler := slog.NewJSONHandler(GinkgoWriter, options)
	logger = slog.New(handler)

	// Configure controller runtime to use our logger
	adapter := logr.FromSlogHandler(logger.Handler())
	ctrl.SetLogger(adapter)
	klog.SetLogger(adapter)

	// Download BMH CRD from upstream (matching go.mod version)
	if version, err := downloadBMHCRD(); err != nil {
		logger.Warn("Failed to download BMH CRD from upstream", "error", err)
		Fail(fmt.Sprintf("Failed to download BMH CRD: %v", err))
	} else {
		logger.Info("Downloaded BMH CRD from upstream", "version", version)
	}

	scheme := runtime.NewScheme()
	Expect(corev1.AddToScheme(scheme)).To(Succeed())
	Expect(inventoryv1alpha1.AddToScheme(scheme)).To(Succeed())
	Expect(bmhv1alpha1.AddToScheme(scheme)).To(Succeed())

	testEnv = &k8senvtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "config", "crd", "bases"),
			"testdata", // BMH CRD downloaded at test time
		},
		ErrorIfCRDPathMissing: true,
		Scheme:                scheme,
	}

	cfg, err := testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	// Create a manager for the controllers
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0", // Disable metrics server for tests
		},
	})
	Expect(err).ToNot(HaveOccurred())

	// Register the Location controller
	err = (&controllers.LocationReconciler{
		Client: mgr.GetClient(),
		Logger: logger.With("controller", "Location"),
	}).SetupWithManager(mgr)
	Expect(err).ToNot(HaveOccurred())

	// Register the OCloudSite controller
	err = (&controllers.OCloudSiteReconciler{
		Client: mgr.GetClient(),
		Logger: logger.With("controller", "OCloudSite"),
	}).SetupWithManager(mgr)
	Expect(err).ToNot(HaveOccurred())

	// Register the ResourcePool controller
	err = (&controllers.ResourcePoolReconciler{
		Client: mgr.GetClient(),
		Logger: logger.With("controller", "ResourcePool"),
	}).SetupWithManager(mgr)
	Expect(err).ToNot(HaveOccurred())

	// Start the manager in a goroutine
	go func() {
		defer GinkgoRecover()
		err := mgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred())
	}()

	// Create a client for tests
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).ToNot(HaveOccurred())

	// Create test namespace
	ns := &corev1.Namespace{}
	ns.Name = testNamespace
	Expect(k8sClient.Create(ctx, ns)).To(Succeed())

	// Set environment variable for default namespace
	os.Setenv("ORAN_O2IMS_NAMESPACE", testNamespace)
})

var _ = AfterSuite(func() {
	cancel()
	if testEnv != nil {
		Expect(testEnv.Stop()).To(Succeed())
	}
})

// Common timeout and interval for Eventually/Consistently assertions
const (
	defaultTimeout  = 10 * time.Second
	defaultInterval = 250 * time.Millisecond
)

// waitForLocationReady waits for a Location to have Ready=True condition.
func waitForLocationReady(location *inventoryv1alpha1.Location) {
	Eventually(func() bool {
		fetched := &inventoryv1alpha1.Location{}
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(location), fetched)
		if err != nil {
			return false
		}
		for _, cond := range fetched.Status.Conditions {
			if cond.Type == inventoryv1alpha1.ConditionTypeReady &&
				cond.Status == metav1.ConditionTrue {
				return true
			}
		}
		return false
	}, defaultTimeout, defaultInterval).Should(BeTrue(), "Location should become Ready")
}

// waitForOCloudSiteReady waits for an OCloudSite to have Ready=True condition.
func waitForOCloudSiteReady(site *inventoryv1alpha1.OCloudSite) {
	Eventually(func() bool {
		fetched := &inventoryv1alpha1.OCloudSite{}
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetched)
		if err != nil {
			return false
		}
		for _, cond := range fetched.Status.Conditions {
			if cond.Type == inventoryv1alpha1.ConditionTypeReady &&
				cond.Status == metav1.ConditionTrue {
				return true
			}
		}
		return false
	}, defaultTimeout, defaultInterval).Should(BeTrue(), "OCloudSite should become Ready")
}
