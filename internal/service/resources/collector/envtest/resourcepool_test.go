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
	)

	BeforeEach(func() {
		testCloudID = uuid.New()
		testDSID = uuid.New()
		eventChannel = make(chan *async.AsyncChangeEvent, 10)

		var err error
		ds, err = collector.NewResourcePoolDataSource(testCloudID, k8sWatchClient)
		Expect(err).ToNot(HaveOccurred())

		// Initialize the data source
		ds.Init(testDSID, 0, eventChannel)

		// Create a separate context for the watch that we can cancel
		watchCtx, watchCancel = context.WithCancel(ctx)
	})

	AfterEach(func() {
		watchCancel()
		close(eventChannel)
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
				ResourcePoolId: "pool-watch-create",
				OCloudSiteId:   "site-watch-create",
				Description:    "Testing watch create",
			},
		}
		Expect(k8sClient.Create(ctx, rp)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, rp) })

		// Set Ready=True status (required for event to be emitted)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), rp)).To(Succeed())
		rp.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
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
		// ResourcePoolID should be a deterministically generated UUID
		Expect(rpModel.ResourcePoolID).ToNot(Equal(uuid.Nil))
		// OCloudSiteID should be set
		Expect(rpModel.OCloudSiteID).ToNot(Equal(uuid.Nil))
	})

	It("receives Updated event when ResourcePool CR is modified", func() {
		// Create a ResourcePool CR first
		rp := &inventoryv1alpha1.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-rp-update",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.ResourcePoolSpec{
				ResourcePoolId: "pool-watch-update",
				OCloudSiteId:   "site-watch-update",
				Description:    "Original pool description",
			},
		}
		Expect(k8sClient.Create(ctx, rp)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, rp) })

		// Set Ready=True status (required for events to be emitted)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), rp)).To(Succeed())
		rp.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
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
				ResourcePoolId: "pool-watch-delete",
				OCloudSiteId:   "site-watch-delete",
				Description:    "Will be deleted",
			},
		}
		Expect(k8sClient.Create(ctx, rp)).To(Succeed())

		// Set Ready=True status
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), rp)).To(Succeed())
		rp.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
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

	It("generates deterministic UUID for ResourcePoolID", func() {
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
				ResourcePoolId: "pool-uuid-test",
				OCloudSiteId:   "site-uuid-test",
				Description:    "Testing UUID generation",
			},
		}
		Expect(k8sClient.Create(ctx, rp)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, rp) })

		// Set Ready=True status (required for event to be emitted)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), rp)).To(Succeed())
		rp.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
		Expect(k8sClient.Status().Update(ctx, rp)).To(Succeed())

		// Wait for the event
		event := waitForEvent(eventChannel)

		// Get the ResourcePoolID from the event
		rpModel, ok := event.Object.(models.ResourcePool)
		Expect(ok).To(BeTrue())
		firstResourcePoolUUID := rpModel.ResourcePoolID
		firstOCloudSiteUUID := rpModel.OCloudSiteID

		// The UUID should be deterministic - creating a new datasource with the same cloudID
		// and processing the same resourcePoolId should produce the same UUID
		ds2, err := collector.NewResourcePoolDataSource(testCloudID, k8sWatchClient)
		Expect(err).ToNot(HaveOccurred())
		ds2.Init(testDSID, 0, nil) // nil channel ok for this test

		// Fetch the pool and convert it manually
		fetchedPool := &inventoryv1alpha1.ResourcePool{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), fetchedPool)).To(Succeed())
		convertedModel := ds2.ConvertResourcePoolToModel(fetchedPool)

		// The UUIDs should match
		Expect(convertedModel.ResourcePoolID).To(Equal(firstResourcePoolUUID))
		Expect(convertedModel.OCloudSiteID).To(Equal(firstOCloudSiteUUID))
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
				ResourcePoolId: "pool-full-test",
				OCloudSiteId:   "site-full-test",
				Description:    "Pool with all fields",
				Extensions: map[string]string{
					"vendor": "acme",
					"tier":   "premium",
				},
			},
		}
		Expect(k8sClient.Create(ctx, rp)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, rp) })

		// Set Ready=True status (required for event to be emitted)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), rp)).To(Succeed())
		rp.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
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
