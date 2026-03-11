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

var _ = Describe("ResourcePoolDataSource Watch", Label("envtest"), func() {
	var (
		ds           *collector.ResourcePoolDataSource
		eventChannel chan *async.AsyncChangeEvent
		watchCtx     context.Context
		watchCancel  context.CancelFunc
		testCloudID  uuid.UUID
		testDSID     uuid.UUID
		// A fake parent site UID for testing
		fakeSiteUID string
	)

	BeforeEach(func() {
		testCloudID = uuid.New()
		testDSID = uuid.New()
		fakeSiteUID = uuid.New().String()
		eventChannel = make(chan *async.AsyncChangeEvent, 10)

		var err error
		ds, err = collector.NewResourcePoolDataSource(testCloudID, newWatchClient())
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

	It("receives Created event when ResourcePool CR is created with Ready=True", func() {
		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Wait for the watch to be ready (SyncComplete event)
		waitForWatchReady(eventChannel)

		// Create a ResourcePool CR
		rp := &inventoryv1alpha1.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-rp-create",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.ResourcePoolSpec{
				OCloudSiteName: "test-site",
				Description:    "Testing watch create",
			},
		}
		Expect(k8sClient.Create(ctx, rp)).To(Succeed())
		DeferCleanup(func() { deleteAndWait(rp) })

		// Set Ready=True status with ResolvedOCloudSiteUID (simulating controller behavior)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), rp)).To(Succeed())
		rp.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
		rp.Status.ResolvedOCloudSiteUID = fakeSiteUID
		Expect(k8sClient.Status().Update(ctx, rp)).To(Succeed())

		// Wait for the event
		event := waitForEvent(eventChannel)

		// Verify the event
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Updated))
		Expect(event.DataSourceID).To(Equal(testDSID))

		// Verify the object is a ResourcePool model
		rpModel, ok := event.Object.(models.ResourcePool)
		Expect(ok).To(BeTrue())
		Expect(rpModel.Name).To(Equal("watch-test-rp-create"))
		Expect(rpModel.Description).To(Equal("Testing watch create"))
		Expect(rpModel.DataSourceID).To(Equal(testDSID))
		// ResourcePoolID should be from metadata.uid
		Expect(rpModel.ResourcePoolID).ToNot(Equal(uuid.Nil))
		// OCloudSiteID should be from status.resolvedOCloudSiteUID
		Expect(rpModel.OCloudSiteID).To(Equal(uuid.MustParse(fakeSiteUID)))
	})

	It("receives Updated event when ResourcePool CR is modified", func() {
		// Create a ResourcePool CR first
		rp := &inventoryv1alpha1.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-rp-update",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.ResourcePoolSpec{
				OCloudSiteName: "test-site",
				Description:    "Original pool description",
			},
		}
		Expect(k8sClient.Create(ctx, rp)).To(Succeed())
		DeferCleanup(func() { deleteAndWait(rp) })

		// Set Ready=True status with ResolvedOCloudSiteUID
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), rp)).To(Succeed())
		rp.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
		rp.Status.ResolvedOCloudSiteUID = fakeSiteUID
		Expect(k8sClient.Status().Update(ctx, rp)).To(Succeed())

		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Wait for the watch to be ready (SyncComplete event)
		waitForWatchReady(eventChannel)

		// Drain initial events (create + possible sync)
		drainEvents(eventChannel)

		// Update the ResourcePool CR (update description since Name is now in metadata)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), rp)).To(Succeed())
		rp.Spec.Description = "Updated pool description"
		Expect(k8sClient.Update(ctx, rp)).To(Succeed())

		// Wait for the update event
		event := waitForEvent(eventChannel)

		// Verify the event
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Updated))

		rpModel, ok := event.Object.(models.ResourcePool)
		Expect(ok).To(BeTrue())
		Expect(rpModel.Name).To(Equal("watch-test-rp-update"))
		Expect(rpModel.Description).To(Equal("Updated pool description"))
	})

	It("receives Deleted event when ResourcePool CR is deleted", func() {
		// Create a ResourcePool CR first
		rp := &inventoryv1alpha1.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-rp-delete",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.ResourcePoolSpec{
				OCloudSiteName: "test-site",
				Description:    "Will be deleted",
			},
		}
		Expect(k8sClient.Create(ctx, rp)).To(Succeed())

		// Set Ready=True status with ResolvedOCloudSiteUID
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), rp)).To(Succeed())
		rp.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
		rp.Status.ResolvedOCloudSiteUID = fakeSiteUID
		Expect(k8sClient.Status().Update(ctx, rp)).To(Succeed())

		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Wait for the watch to be ready (SyncComplete event)
		waitForWatchReady(eventChannel)

		// Drain initial events
		drainEvents(eventChannel)

		// Delete the ResourcePool CR
		Expect(k8sClient.Delete(ctx, rp)).To(Succeed())

		// Wait for the delete event
		event := waitForEvent(eventChannel)

		// Verify the event
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Deleted))

		rpModel, ok := event.Object.(models.ResourcePool)
		Expect(ok).To(BeTrue())
		Expect(rpModel.Name).To(Equal("watch-test-rp-delete"))
	})

	It("uses metadata.uid for ResourcePoolID", func() {
		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Wait for the watch to be ready (SyncComplete event)
		waitForWatchReady(eventChannel)

		// Create a ResourcePool CR
		rp := &inventoryv1alpha1.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-rp-uuid",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.ResourcePoolSpec{
				OCloudSiteName: "test-site",
				Description:    "Testing UUID from metadata.uid",
			},
		}
		Expect(k8sClient.Create(ctx, rp)).To(Succeed())
		DeferCleanup(func() { deleteAndWait(rp) })

		// Get the created pool to capture its metadata.uid
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), rp)).To(Succeed())
		expectedPoolUID := uuid.MustParse(string(rp.UID))

		// Set Ready=True status with ResolvedOCloudSiteUID
		rp.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
		rp.Status.ResolvedOCloudSiteUID = fakeSiteUID
		Expect(k8sClient.Status().Update(ctx, rp)).To(Succeed())

		// Wait for the event
		event := waitForEvent(eventChannel)

		// Get the ResourcePoolID from the event
		rpModel, ok := event.Object.(models.ResourcePool)
		Expect(ok).To(BeTrue())

		// The ResourcePoolID should match the metadata.uid from Kubernetes
		Expect(rpModel.ResourcePoolID).To(Equal(expectedPoolUID))
		// The OCloudSiteID should match the status.resolvedOCloudSiteUID
		Expect(rpModel.OCloudSiteID).To(Equal(uuid.MustParse(fakeSiteUID)))

	})

	It("includes optional fields when provided", func() {
		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Wait for the watch to be ready (SyncComplete event)
		waitForWatchReady(eventChannel)

		// Create a ResourcePool CR with all optional fields
		rp := &inventoryv1alpha1.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-rp-full",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.ResourcePoolSpec{
				OCloudSiteName: "test-site",
				Description:    "Pool with all fields",
				Extensions: map[string]string{
					"vendor": "acme",
					"tier":   "premium",
				},
			},
		}
		Expect(k8sClient.Create(ctx, rp)).To(Succeed())
		DeferCleanup(func() { deleteAndWait(rp) })

		// Set Ready=True status with ResolvedOCloudSiteUID
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), rp)).To(Succeed())
		rp.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
		rp.Status.ResolvedOCloudSiteUID = fakeSiteUID
		Expect(k8sClient.Status().Update(ctx, rp)).To(Succeed())

		// Wait for the event
		event := waitForEvent(eventChannel)

		// Verify all fields including optional ones
		rpModel, ok := event.Object.(models.ResourcePool)
		Expect(ok).To(BeTrue())
		Expect(rpModel.Name).To(Equal("watch-test-rp-full"))
		Expect(rpModel.Description).To(Equal("Pool with all fields"))
		Expect(rpModel.Extensions).To(HaveLen(2))
		Expect(rpModel.Extensions["vendor"]).To(Equal("acme"))
		Expect(rpModel.Extensions["tier"]).To(Equal("premium"))
	})
})
