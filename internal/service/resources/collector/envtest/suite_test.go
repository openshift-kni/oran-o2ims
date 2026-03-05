/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package envtest

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8senvtest "sigs.k8s.io/controller-runtime/pkg/envtest"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
)

const testNamespace = "test-collector"

var (
	testEnv        *k8senvtest.Environment
	k8sClient      client.Client
	k8sWatchClient client.WithWatch // For Watch-capable client
	ctx            context.Context
	cancel         context.CancelFunc
)

func TestCollectorEnvtest(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Collector Envtest Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.Background())

	scheme := runtime.NewScheme()
	Expect(corev1.AddToScheme(scheme)).To(Succeed())
	Expect(inventoryv1alpha1.AddToScheme(scheme)).To(Succeed())

	testEnv = &k8senvtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "..", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
		Scheme:                scheme,
	}

	cfg, err := testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).ToNot(HaveOccurred())

	// Create watch-capable client for DataSource tests
	k8sWatchClient, err = client.NewWithWatch(cfg, client.Options{Scheme: scheme})
	Expect(err).ToNot(HaveOccurred())

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	Expect(k8sClient.Create(ctx, ns)).To(Succeed())
})

var _ = AfterSuite(func() {
	cancel()
	if testEnv != nil {
		Expect(testEnv.Stop()).To(Succeed())
	}
})
