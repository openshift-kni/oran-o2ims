/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package envtest

import (
	"context"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/collector"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

var _ = Describe("OCloudSiteDataSource Watch", Label("envtest"), func() {
	var (
		ds           *collector.OCloudSiteDataSource
		eventChannel chan *async.AsyncChangeEvent
		watchCtx     context.Context
		watchCancel  context.CancelFunc
		testCloudID  uuid.UUID
		testDSID     uuid.UUID
	)

	BeforeEach(func() {
		testCloudID = uuid.New()
		testDSID = uuid.New()
		eventChannel = make(chan *async.AsyncChangeEvent, 10)

		var err error
		ds, err = collector.NewOCloudSiteDataSource(testCloudID, newWatchClient())
		Expect(err).ToNot(HaveOccurred())

		// Initialize the data source
		ds.Init(testDSID, 0, eventChannel)

		// Create a separate context for the watch that we can cancel
		watchCtx, watchCancel = context.WithCancel(ctx)
	})

	AfterEach(func() {
		watchCancel()
		// Note: We intentionally don't close eventChannel here.
		// Closing immediately after watchCancel() risks a panic if a
		// goroutine is still sending. The channel will be GC'd anyway.
	})

	It("receives Created event when OCloudSite CR is created with Ready=True", func() {
		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Wait for the watch to be ready (SyncComplete event)
		waitForWatchReady(eventChannel)

		// Create an OCloudSite CR
		site := &inventoryv1alpha1.OCloudSite{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-site-create",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.OCloudSiteSpec{
				GlobalLocationName: "test-location",
				Description:        "Testing watch create for site",
			},
		}
		Expect(k8sClient.Create(ctx, site)).To(Succeed())
		DeferCleanup(func() { deleteAndWait(site) })

		// Set Ready=True status (required for event to be emitted)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(site), site)).To(Succeed())
		site.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
		Expect(k8sClient.Status().Update(ctx, site)).To(Succeed())

		// Wait for the event
		event := waitForEvent(eventChannel)

		// Verify the event
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Updated))
		Expect(event.DataSourceID).To(Equal(testDSID))

		// Verify the object is an OCloudSite model
		siteModel, ok := event.Object.(models.OCloudSite)
		Expect(ok).To(BeTrue())
		Expect(siteModel.GlobalLocationID).To(Equal("test-location"))
		Expect(siteModel.Name).To(Equal("watch-test-site-create"))
		Expect(siteModel.Description).To(Equal("Testing watch create for site"))
		Expect(siteModel.DataSourceID).To(Equal(testDSID))
		// OCloudSiteID should be from metadata.uid
		Expect(siteModel.OCloudSiteID).ToNot(Equal(uuid.Nil))
	})

	It("receives Updated event when OCloudSite CR is modified", func() {
		// Create an OCloudSite CR first
		site := &inventoryv1alpha1.OCloudSite{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-site-update",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.OCloudSiteSpec{
				GlobalLocationName: "test-location",
				Description:        "Original site description",
			},
		}
		Expect(k8sClient.Create(ctx, site)).To(Succeed())
		DeferCleanup(func() { deleteAndWait(site) })

		// Set Ready=True status (required for events to be emitted)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(site), site)).To(Succeed())
		site.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
		Expect(k8sClient.Status().Update(ctx, site)).To(Succeed())

		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Wait for the watch to be ready (SyncComplete event)
		waitForWatchReady(eventChannel)

		// Drain initial events (create + possible sync)
		drainEvents(eventChannel)

		// Update the OCloudSite CR (update description since Name is now in metadata)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(site), site)).To(Succeed())
		site.Spec.Description = "Updated site description"
		Expect(k8sClient.Update(ctx, site)).To(Succeed())

		// Wait for the update event
		event := waitForEvent(eventChannel)

		// Verify the event
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Updated))

		siteModel, ok := event.Object.(models.OCloudSite)
		Expect(ok).To(BeTrue())
		Expect(siteModel.Name).To(Equal("watch-test-site-update"))
		Expect(siteModel.Description).To(Equal("Updated site description"))
	})

	It("receives Deleted event when OCloudSite CR is deleted", func() {
		// Create an OCloudSite CR first
		site := &inventoryv1alpha1.OCloudSite{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-site-delete",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.OCloudSiteSpec{
				GlobalLocationName: "test-location",
				Description:        "Will be deleted",
			},
		}
		Expect(k8sClient.Create(ctx, site)).To(Succeed())

		// Set Ready=True status
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(site), site)).To(Succeed())
		site.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
		Expect(k8sClient.Status().Update(ctx, site)).To(Succeed())

		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Wait for the watch to be ready (SyncComplete event)
		waitForWatchReady(eventChannel)

		// Drain initial events
		drainEvents(eventChannel)

		// Delete the OCloudSite CR
		Expect(k8sClient.Delete(ctx, site)).To(Succeed())

		// Wait for the delete event
		event := waitForEvent(eventChannel)

		// Verify the event
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Deleted))

		siteModel, ok := event.Object.(models.OCloudSite)
		Expect(ok).To(BeTrue())
		Expect(siteModel.GlobalLocationID).To(Equal("test-location"))
	})

	It("uses metadata.uid for OCloudSiteID", func() {
		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Wait for the watch to be ready (SyncComplete event)
		waitForWatchReady(eventChannel)

		// Create an OCloudSite CR
		site := &inventoryv1alpha1.OCloudSite{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-site-uuid",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.OCloudSiteSpec{
				GlobalLocationName: "test-location",
				Description:        "Testing UUID from metadata.uid",
			},
		}
		Expect(k8sClient.Create(ctx, site)).To(Succeed())
		DeferCleanup(func() { deleteAndWait(site) })

		// Get the created site to capture its metadata.uid
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(site), site)).To(Succeed())
		expectedUID := uuid.MustParse(string(site.UID))

		// Set Ready=True status (required for event to be emitted)
		site.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
		Expect(k8sClient.Status().Update(ctx, site)).To(Succeed())

		// Wait for the event
		event := waitForEvent(eventChannel)

		// Get the OCloudSiteID from the event
		siteModel, ok := event.Object.(models.OCloudSite)
		Expect(ok).To(BeTrue())

		// The OCloudSiteID should match the metadata.uid from Kubernetes
		Expect(siteModel.OCloudSiteID).To(Equal(expectedUID))

	})
})
