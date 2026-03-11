/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package envtest

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8senvtest "sigs.k8s.io/controller-runtime/pkg/envtest"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
)

const testNamespace = "test-collector"

var (
	testEnv   *k8senvtest.Environment
	k8sClient client.Client
	testCfg   *rest.Config    // Stored for per-test watch client creation
	scheme    *runtime.Scheme // Stored for per-test watch client creation
	ctx       context.Context
	cancel    context.CancelFunc
)

func TestCollectorEnvtest(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Collector Envtest Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.Background())

	// Configure slog to use GinkgoWriter (only shows output on test failure)
	handler := slog.NewTextHandler(GinkgoWriter, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(handler))

	scheme = runtime.NewScheme()
	Expect(corev1.AddToScheme(scheme)).To(Succeed())
	Expect(inventoryv1alpha1.AddToScheme(scheme)).To(Succeed())

	testEnv = &k8senvtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "..", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
		Scheme:                scheme,
	}

	var err error
	testCfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(testCfg).ToNot(BeNil())

	k8sClient, err = client.New(testCfg, client.Options{Scheme: scheme})
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

//
// Shared Test Helpers
//

// newWatchClient creates a fresh watch-capable client for each test.
// This ensures complete test isolation, each test gets its own connection to the API server
func newWatchClient() client.WithWatch {
	watchClient, err := client.NewWithWatch(testCfg, client.Options{Scheme: scheme})
	Expect(err).ToNot(HaveOccurred())
	return watchClient
}

// waitForWatchReady waits for the SyncComplete event which signals the watch reflector is ready.
func waitForWatchReady(ch chan *async.AsyncChangeEvent) {
	Eventually(func() bool {
		select {
		case event := <-ch:
			return event.EventType == async.SyncComplete
		default:
			return false
		}
	}, 5*time.Second, 50*time.Millisecond).Should(BeTrue(), "watch should send SyncComplete when ready")
}

// waitForEvent waits for a non-SyncComplete event from the channel and returns it.
func waitForEvent(ch chan *async.AsyncChangeEvent) *async.AsyncChangeEvent {
	var event *async.AsyncChangeEvent
	Eventually(func() bool {
		select {
		case event = <-ch:
			// Skip SyncComplete events, we want actual change events
			if event.EventType == async.SyncComplete {
				return false
			}
			return true
		default:
			return false
		}
	}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(), "should receive a change event")
	return event
}

// drainEvents removes all pending events from the channel
func drainEvents(ch chan *async.AsyncChangeEvent) {
	for {
		select {
		case <-ch:
			// discard
		default:
			return
		}
	}
}

// ptrTo returns a pointer to the given string value.
func ptrTo(s string) *string {
	return &s
}

// deleteAndWait deletes a CR and waits until it's confirmed gone from the API server.
// This ensures the next test's watch won't see stale events from this CR.
func deleteAndWait(obj client.Object) {
	err := k8sClient.Delete(ctx, obj)
	if err != nil && !apierrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
	// Wait until the CR is actually gone from the API server
	Eventually(func() bool {
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)
		return apierrors.IsNotFound(err)
	}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(),
		"CR %s/%s should be deleted from API server", obj.GetNamespace(), obj.GetName())
}
