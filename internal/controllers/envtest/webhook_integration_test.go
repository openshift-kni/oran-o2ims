/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package envtest

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8senvtest "sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
)

// This test suite tests the webhook admission logic with a real webhook server.
var _ = Describe("Webhook Integration Tests", Label("envtest"), Ordered, func() {
	var (
		webhookTestEnv    *k8senvtest.Environment
		webhookClient     client.Client
		webhookCtx        context.Context
		webhookCancel     context.CancelFunc
		webhookNamespace  string
	)

	BeforeAll(func() {
		webhookCtx, webhookCancel = context.WithCancel(context.Background())
		webhookNamespace = fmt.Sprintf("webhook-test-%s", randString(5))

		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(inventoryv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(admissionv1.AddToScheme(scheme)).To(Succeed())

		// Set up envtest with webhook support
		webhookTestEnv = &k8senvtest.Environment{
			CRDDirectoryPaths: []string{
				filepath.Join("..", "..", "..", "config", "crd", "bases"),
			},
			ErrorIfCRDPathMissing: true,
			Scheme:                scheme,
			WebhookInstallOptions: k8senvtest.WebhookInstallOptions{
				Paths: []string{filepath.Join("..", "..", "..", "config", "webhook")},
			},
		}

		cfg, err := webhookTestEnv.Start()
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg).ToNot(BeNil())

		// Get webhook server options from envtest
		webhookInstallOptions := &webhookTestEnv.WebhookInstallOptions
		
		// Create manager with webhook server
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme: scheme,
			Metrics: metricsserver.Options{
				BindAddress: "0",
			},
			WebhookServer: webhook.NewServer(webhook.Options{
				Host:    webhookInstallOptions.LocalServingHost,
				Port:    webhookInstallOptions.LocalServingPort,
				CertDir: webhookInstallOptions.LocalServingCertDir,
			}),
		})
		Expect(err).ToNot(HaveOccurred())

		// Register webhooks
		err = (&inventoryv1alpha1.Location{}).SetupWebhookWithManager(mgr)
		Expect(err).ToNot(HaveOccurred())

		err = (&inventoryv1alpha1.OCloudSite{}).SetupWebhookWithManager(mgr)
		Expect(err).ToNot(HaveOccurred())

		err = (&inventoryv1alpha1.ResourcePool{}).SetupWebhookWithManager(mgr)
		Expect(err).ToNot(HaveOccurred())

		// Start manager
		go func() {
			defer GinkgoRecover()
			err := mgr.Start(webhookCtx)
			Expect(err).ToNot(HaveOccurred())
		}()

		// Wait for webhook server to be ready
		dialer := &net.Dialer{Timeout: time.Second}
		addrPort := fmt.Sprintf("%s:%d", webhookInstallOptions.LocalServingHost, webhookInstallOptions.LocalServingPort)
		Eventually(func() error {
			conn, err := tls.DialWithDialer(dialer, "tcp", addrPort, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec
			if err != nil {
				return err
			}
			return conn.Close()
		}, 10*time.Second, 250*time.Millisecond).Should(Succeed())

		// Create client
		webhookClient, err = client.New(cfg, client.Options{Scheme: scheme})
		Expect(err).ToNot(HaveOccurred())

		// Create test namespace
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: webhookNamespace}}
		Expect(webhookClient.Create(webhookCtx, ns)).To(Succeed())
	})

	AfterAll(func() {
		webhookCancel()
		if webhookTestEnv != nil {
			Expect(webhookTestEnv.Stop()).To(Succeed())
		}
	})

	Context("Location webhook", func() {
		It("should reject duplicate globalLocationId on create", func() {
			// Create first Location
			loc1 := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webhook-loc-original",
					Namespace: webhookNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "DUPLICATE-LOC-ID",
					Address:          ptrTo("Original Address"),
				},
			}
			Expect(webhookClient.Create(webhookCtx, loc1)).To(Succeed())
			defer func() { _ = webhookClient.Delete(webhookCtx, loc1) }()

			// Try to create second Location with same ID, should be rejected by webhook
			loc2 := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webhook-loc-duplicate",
					Namespace: webhookNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "DUPLICATE-LOC-ID", // Same ID!
					Address:          ptrTo("Duplicate Address"),
				},
			}
			err := webhookClient.Create(webhookCtx, loc2)

			// Webhook should reject with error
			Expect(err).To(HaveOccurred())
			Expect(errors.IsForbidden(err) || errors.IsInvalid(err)).To(BeTrue(),
				"Expected Forbidden or Invalid error, got: %v", err)
			Expect(err.Error()).To(ContainSubstring("DUPLICATE-LOC-ID"))
		})

		It("should allow unique globalLocationId on create", func() {
			loc := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("webhook-loc-unique-%s", randString(5)),
					Namespace: webhookNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: fmt.Sprintf("UNIQUE-LOC-%s", randString(8)),
					Address:          ptrTo("Unique Address"),
				},
			}
			Expect(webhookClient.Create(webhookCtx, loc)).To(Succeed())
			defer func() { _ = webhookClient.Delete(webhookCtx, loc) }()
		})
	})

	Context("OCloudSite webhook", func() {
		var parentLocation *inventoryv1alpha1.Location

		BeforeEach(func() {
			parentLocation = &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("webhook-site-parent-%s", randString(5)),
					Namespace: webhookNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: fmt.Sprintf("SITE-PARENT-%s", randString(5)),
					Address:          ptrTo("Parent Location"),
				},
			}
			Expect(webhookClient.Create(webhookCtx, parentLocation)).To(Succeed())
		})

		AfterEach(func() {
			if parentLocation != nil {
				_ = webhookClient.Delete(webhookCtx, parentLocation)
			}
		})

		It("should reject duplicate siteId on create", func() {
			// Create first OCloudSite
			site1 := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webhook-site-original",
					Namespace: webhookNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "DUPLICATE-SITE-ID",
					GlobalLocationID: parentLocation.Spec.GlobalLocationID,
				},
			}
			Expect(webhookClient.Create(webhookCtx, site1)).To(Succeed())
			defer func() { _ = webhookClient.Delete(webhookCtx, site1) }()

			// Try to create second OCloudSite with same ID
			site2 := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webhook-site-duplicate",
					Namespace: webhookNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "DUPLICATE-SITE-ID", // Same ID!
					GlobalLocationID: parentLocation.Spec.GlobalLocationID,
				},
			}
			err := webhookClient.Create(webhookCtx, site2)

			// Webhook should reject with error
			Expect(err).To(HaveOccurred())
			Expect(errors.IsForbidden(err) || errors.IsInvalid(err)).To(BeTrue(),
				"Expected Forbidden or Invalid error, got: %v", err)
			Expect(err.Error()).To(ContainSubstring("DUPLICATE-SITE-ID"))
		})
	})

	Context("ResourcePool webhook", func() {
		var parentLocation *inventoryv1alpha1.Location
		var parentSite *inventoryv1alpha1.OCloudSite

		BeforeEach(func() {
			parentLocation = &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("webhook-pool-loc-%s", randString(5)),
					Namespace: webhookNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: fmt.Sprintf("POOL-LOC-%s", randString(5)),
					Address:          ptrTo("Pool Parent Location"),
				},
			}
			Expect(webhookClient.Create(webhookCtx, parentLocation)).To(Succeed())

			parentSite = &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("webhook-pool-site-%s", randString(5)),
					Namespace: webhookNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           fmt.Sprintf("POOL-SITE-%s", randString(5)),
					GlobalLocationID: parentLocation.Spec.GlobalLocationID,
				},
			}
			Expect(webhookClient.Create(webhookCtx, parentSite)).To(Succeed())
		})

		AfterEach(func() {
			if parentSite != nil {
				_ = webhookClient.Delete(webhookCtx, parentSite)
			}
			if parentLocation != nil {
				_ = webhookClient.Delete(webhookCtx, parentLocation)
			}
		})

		It("should reject duplicate resourcePoolId on create", func() {
			// Create first ResourcePool
			pool1 := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webhook-pool-original",
					Namespace: webhookNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					ResourcePoolId: "DUPLICATE-POOL-ID",
					OCloudSiteId:   parentSite.Spec.SiteID,
				},
			}
			Expect(webhookClient.Create(webhookCtx, pool1)).To(Succeed())
			defer func() { _ = webhookClient.Delete(webhookCtx, pool1) }()

			// Try to create second ResourcePool with same ID
			pool2 := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webhook-pool-duplicate",
					Namespace: webhookNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					ResourcePoolId: "DUPLICATE-POOL-ID", // Same ID!
					OCloudSiteId:   parentSite.Spec.SiteID,
				},
			}
			err := webhookClient.Create(webhookCtx, pool2)

			// Webhook should reject with error
			Expect(err).To(HaveOccurred())
			Expect(errors.IsForbidden(err) || errors.IsInvalid(err)).To(BeTrue(),
				"Expected Forbidden or Invalid error, got: %v", err)
			Expect(err.Error()).To(ContainSubstring("DUPLICATE-POOL-ID"))
		})
	})
})
