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

// NOTE: Hierarchy integration tests with controllers are in internal/controllers/envtest/.
// This suite tests data source filtering behavior with manually controlled CR status (
// sets the readiness status checked by the previous test suite manually).

var _ = Describe("Ready Status Filtering", Label("envtest"), func() {
	Describe("Location Ready Filter", func() {
		var (
			ds           *collector.LocationDataSource
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
			ds, err = collector.NewLocationDataSource(testCloudID, newWatchClient())
			Expect(err).ToNot(HaveOccurred())

			ds.Init(testDSID, 0, eventChannel)
			watchCtx, watchCancel = context.WithCancel(ctx)
		})

		AfterEach(func() {
			watchCancel()
			// Note: We intentionally don't close eventChannel here.
			// Closing immediately after watchCancel() risks a panic if a
			// goroutine is still sending. The channel will be GC'd anyway.
		})

		It("should emit DELETE event for Location without Ready status", func() {
			err := ds.Watch(watchCtx)
			Expect(err).ToNot(HaveOccurred())
			waitForWatchReady(eventChannel)

			// Create a Location CR without setting Ready status
			loc := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "loc-no-ready",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					Description: "Should emit DELETE event (not ready = remove from API)",
					Address:     ptrTo("123 No Ready St"),
				},
			}
			Expect(k8sClient.Create(ctx, loc)).To(Succeed())
			DeferCleanup(func() { deleteAndWait(loc) })

			// CRs without Ready status are treated as deletions from API perspective
			// This ensures stale data is removed when CRs are not valid
			event := waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Deleted),
				"Location without Ready status should emit DELETE event")
		})

		It("should emit DELETE event for Location with Ready=False", func() {
			err := ds.Watch(watchCtx)
			Expect(err).ToNot(HaveOccurred())
			waitForWatchReady(eventChannel)

			// Create a Location CR
			loc := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "loc-not-ready",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					Description: "Should emit DELETE event (Ready=False = remove from API)",
					Address:     ptrTo("456 Not Ready Ave"),
				},
			}
			Expect(k8sClient.Create(ctx, loc)).To(Succeed())
			DeferCleanup(func() { deleteAndWait(loc) })

			// First event: DELETE (CR created without Ready status)
			event := waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Deleted))

			// Set Ready=False status explicitly
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), loc)).To(Succeed())
			loc.Status.Conditions = []metav1.Condition{
				{
					Type:               inventoryv1alpha1.ConditionTypeReady,
					Status:             metav1.ConditionFalse,
					Reason:             inventoryv1alpha1.ReasonParentNotFound,
					Message:            "Parent not found",
					LastTransitionTime: metav1.Now(),
				},
			}
			Expect(k8sClient.Status().Update(ctx, loc)).To(Succeed())

			// Second event: DELETE (Ready=False also triggers deletion from API)
			event = waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Deleted),
				"Location with Ready=False should emit DELETE event")
		})

		It("should emit UPDATE event when Location transitions to Ready=True", func() {
			err := ds.Watch(watchCtx)
			Expect(err).ToNot(HaveOccurred())
			waitForWatchReady(eventChannel)

			// Create a Location CR without Ready status
			loc := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "loc-transition-ready",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					Description: "Will transition to Ready=True",
					Address:     ptrTo("789 Transition Blvd"),
				},
			}
			Expect(k8sClient.Create(ctx, loc)).To(Succeed())
			DeferCleanup(func() { deleteAndWait(loc) })

			// First event: DELETE (CR created without Ready status)
			event := waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Deleted),
				"Location created without Ready should emit DELETE")

			// Now set Ready=True status
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), loc)).To(Succeed())
			loc.Status.Conditions = []metav1.Condition{
				{
					Type:               inventoryv1alpha1.ConditionTypeReady,
					Status:             metav1.ConditionTrue,
					Reason:             inventoryv1alpha1.ReasonReady,
					Message:            "Resource is ready",
					LastTransitionTime: metav1.Now(),
				},
			}
			Expect(k8sClient.Status().Update(ctx, loc)).To(Succeed())

			// Second event: UPDATE (now Ready=True, added to API)
			event = waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Updated),
				"Location transitioning to Ready=True should emit UPDATE")

			locModel, ok := event.Object.(models.Location)
			Expect(ok).To(BeTrue())
			Expect(locModel.GlobalLocationID).To(Equal("loc-transition-ready"))
		})

		It("should always emit Delete event regardless of Ready status", func() {
			err := ds.Watch(watchCtx)
			Expect(err).ToNot(HaveOccurred())
			waitForWatchReady(eventChannel)

			// Create a Location CR with Ready=False
			loc := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "loc-delete-not-ready",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					Description: "Delete with Ready=False",
					Address:     ptrTo("999 Delete St"),
				},
			}
			Expect(k8sClient.Create(ctx, loc)).To(Succeed())

			// Set Ready=False status
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), loc)).To(Succeed())
			loc.Status.Conditions = []metav1.Condition{
				{
					Type:               inventoryv1alpha1.ConditionTypeReady,
					Status:             metav1.ConditionFalse,
					Reason:             inventoryv1alpha1.ReasonParentNotFound,
					Message:            "Parent not found",
					LastTransitionTime: metav1.Now(),
				},
			}
			Expect(k8sClient.Status().Update(ctx, loc)).To(Succeed())

			// Drain any events
			drainEvents(eventChannel)

			// Delete the Location CR
			Expect(k8sClient.Delete(ctx, loc)).To(Succeed())

			// Delete event SHOULD be emitted even though Ready=False
			event := waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Deleted))

			locModel, ok := event.Object.(models.Location)
			Expect(ok).To(BeTrue())
			Expect(locModel.GlobalLocationID).To(Equal("loc-delete-not-ready"))
		})

		It("should emit DELETE when Location transitions from Ready=True to Ready=False", func() {
			// A CR that was Ready and in the API becomes not Ready (e.g., parent deleted) and
			// must be removed from the API.

			err := ds.Watch(watchCtx)
			Expect(err).ToNot(HaveOccurred())
			waitForWatchReady(eventChannel)

			// Create a Location CR
			loc := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "loc-ready-to-not-ready",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					Description: "Will transition Ready=True → Ready=False",
					Address:     ptrTo("111 Transition St"),
				},
			}
			Expect(k8sClient.Create(ctx, loc)).To(Succeed())
			DeferCleanup(func() { deleteAndWait(loc) })

			// First event: DELETE (CR created without Ready status)
			event := waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Deleted))

			// Set Ready=True status (simulates CR becoming valid)
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), loc)).To(Succeed())
			loc.Status.Conditions = []metav1.Condition{
				{
					Type:               inventoryv1alpha1.ConditionTypeReady,
					Status:             metav1.ConditionTrue,
					Reason:             inventoryv1alpha1.ReasonReady,
					Message:            "Resource is ready",
					LastTransitionTime: metav1.Now(),
				},
			}
			Expect(k8sClient.Status().Update(ctx, loc)).To(Succeed())

			// Second event: UPDATE (Ready=True, added to API)
			event = waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Updated),
				"Location becoming Ready=True should emit UPDATE")

			// Now transition to Ready=False (simulates parent being deleted)
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), loc)).To(Succeed())
			loc.Status.Conditions = []metav1.Condition{
				{
					Type:               inventoryv1alpha1.ConditionTypeReady,
					Status:             metav1.ConditionFalse,
					Reason:             inventoryv1alpha1.ReasonParentNotFound,
					Message:            "Parent location no longer exists",
					LastTransitionTime: metav1.Now(),
				},
			}
			Expect(k8sClient.Status().Update(ctx, loc)).To(Succeed())

			// Third event: DELETE (Ready=False means remove from API)
			event = waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Deleted),
				"Location transitioning to Ready=False should emit DELETE to remove from API")

			locModel, ok := event.Object.(models.Location)
			Expect(ok).To(BeTrue())
			Expect(locModel.GlobalLocationID).To(Equal("loc-ready-to-not-ready"))
		})
	})

	Describe("OCloudSite Ready Filter", func() {
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

			ds.Init(testDSID, 0, eventChannel)
			watchCtx, watchCancel = context.WithCancel(ctx)
		})

		AfterEach(func() {
			watchCancel()
			// Note: We intentionally don't close eventChannel here.
			// Closing immediately after watchCancel() risks a panic if a
			// goroutine is still sending. The channel will be GC'd anyway.
		})

		It("should emit DELETE event for OCloudSite without Ready status", func() {
			err := ds.Watch(watchCtx)
			Expect(err).ToNot(HaveOccurred())
			waitForWatchReady(eventChannel)

			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "site-no-ready",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					GlobalLocationName: "test-location",
					Description:        "Should emit DELETE event (not ready = remove from API)",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() { deleteAndWait(site) })

			// CRs without Ready status are treated as deletions from API perspective
			event := waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Deleted),
				"OCloudSite without Ready status should emit DELETE event")
		})

		It("should emit UPDATE event when OCloudSite transitions to Ready=True", func() {
			err := ds.Watch(watchCtx)
			Expect(err).ToNot(HaveOccurred())
			waitForWatchReady(eventChannel)

			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "site-transition-ready",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					GlobalLocationName: "test-location",
					Description:        "Will transition to Ready=True",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() { deleteAndWait(site) })

			// First event: DELETE (CR created without Ready status)
			event := waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Deleted),
				"OCloudSite created without Ready should emit DELETE")

			// Set Ready=True status
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(site), site)).To(Succeed())
			site.Status.Conditions = []metav1.Condition{
				{
					Type:               inventoryv1alpha1.ConditionTypeReady,
					Status:             metav1.ConditionTrue,
					Reason:             inventoryv1alpha1.ReasonReady,
					Message:            "Resource is ready",
					LastTransitionTime: metav1.Now(),
				},
			}
			Expect(k8sClient.Status().Update(ctx, site)).To(Succeed())

			// Second event: UPDATE (now Ready=True, added to API)
			event = waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Updated),
				"OCloudSite transitioning to Ready=True should emit UPDATE")

			siteModel, ok := event.Object.(models.OCloudSite)
			Expect(ok).To(BeTrue())
			Expect(siteModel.Name).To(Equal("site-transition-ready"))
		})

		It("should always emit Delete event regardless of Ready status", func() {
			err := ds.Watch(watchCtx)
			Expect(err).ToNot(HaveOccurred())
			waitForWatchReady(eventChannel)

			// Create an OCloudSite CR with Ready=False
			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "site-delete-not-ready",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					GlobalLocationName: "test-location",
					Description:        "Delete with Ready=False",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())

			// Set Ready=False status
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(site), site)).To(Succeed())
			site.Status.Conditions = []metav1.Condition{
				{
					Type:               inventoryv1alpha1.ConditionTypeReady,
					Status:             metav1.ConditionFalse,
					Reason:             inventoryv1alpha1.ReasonParentNotFound,
					Message:            "Parent not found",
					LastTransitionTime: metav1.Now(),
				},
			}
			Expect(k8sClient.Status().Update(ctx, site)).To(Succeed())

			// Drain any events
			drainEvents(eventChannel)

			// Delete the OCloudSite CR
			Expect(k8sClient.Delete(ctx, site)).To(Succeed())

			// Delete event SHOULD be emitted even though Ready=False
			event := waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Deleted))

			siteModel, ok := event.Object.(models.OCloudSite)
			Expect(ok).To(BeTrue())
			Expect(siteModel.Name).To(Equal("site-delete-not-ready"))
		})

		It("should emit DELETE when OCloudSite transitions from Ready=True to Ready=False", func() {
			// A CR that was Ready and in the API becomes not Ready (e.g., parent deleted) and
			// must be removed from the API.
			err := ds.Watch(watchCtx)
			Expect(err).ToNot(HaveOccurred())
			waitForWatchReady(eventChannel)

			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "site-ready-to-not-ready",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					GlobalLocationName: "test-location",
					Description:        "Will transition Ready=True → Ready=False",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() { deleteAndWait(site) })

			// First event: DELETE (CR created without Ready status)
			event := waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Deleted))

			// Set Ready=True status (simulates CR becoming valid)
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(site), site)).To(Succeed())
			site.Status.Conditions = []metav1.Condition{
				{
					Type:               inventoryv1alpha1.ConditionTypeReady,
					Status:             metav1.ConditionTrue,
					Reason:             inventoryv1alpha1.ReasonReady,
					Message:            "Resource is ready",
					LastTransitionTime: metav1.Now(),
				},
			}
			Expect(k8sClient.Status().Update(ctx, site)).To(Succeed())

			// Second event: UPDATE (Ready=True, added to API)
			event = waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Updated),
				"OCloudSite becoming Ready=True should emit UPDATE")

			// Now transition to Ready=False (simulates parent being deleted)
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(site), site)).To(Succeed())
			site.Status.Conditions = []metav1.Condition{
				{
					Type:               inventoryv1alpha1.ConditionTypeReady,
					Status:             metav1.ConditionFalse,
					Reason:             inventoryv1alpha1.ReasonParentNotFound,
					Message:            "Parent location no longer exists",
					LastTransitionTime: metav1.Now(),
				},
			}
			Expect(k8sClient.Status().Update(ctx, site)).To(Succeed())

			// Third event: DELETE (Ready=False means remove from API)
			event = waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Deleted),
				"OCloudSite transitioning to Ready=False should emit DELETE to remove from API")

			siteModel, ok := event.Object.(models.OCloudSite)
			Expect(ok).To(BeTrue())
			Expect(siteModel.Name).To(Equal("site-ready-to-not-ready"))
		})
	})

	Describe("ResourcePool Ready Filter", func() {
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
			ds, err = collector.NewResourcePoolDataSource(testCloudID, newWatchClient())
			Expect(err).ToNot(HaveOccurred())

			ds.Init(testDSID, 0, eventChannel)
			watchCtx, watchCancel = context.WithCancel(ctx)
		})

		AfterEach(func() {
			watchCancel()
			// Note: We intentionally don't close eventChannel here.
			// Closing immediately after watchCancel() risks a panic if a
			// goroutine is still sending. The channel will be GC'd anyway.
		})

		It("should emit DELETE event for ResourcePool without Ready status", func() {
			err := ds.Watch(watchCtx)
			Expect(err).ToNot(HaveOccurred())
			waitForWatchReady(eventChannel)

			rp := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pool-no-ready",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					OCloudSiteName: "test-site",
					Description:    "Should emit DELETE event (not ready = remove from API)",
				},
			}
			Expect(k8sClient.Create(ctx, rp)).To(Succeed())
			DeferCleanup(func() { deleteAndWait(rp) })

			// CRs without Ready status are treated as deletions from API perspective
			event := waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Deleted),
				"ResourcePool without Ready status should emit DELETE event")
		})

		It("should emit UPDATE event when ResourcePool transitions to Ready=True", func() {
			err := ds.Watch(watchCtx)
			Expect(err).ToNot(HaveOccurred())
			waitForWatchReady(eventChannel)

			// Create a fake site UID for the status
			fakeSiteUID := uuid.New().String()

			rp := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pool-transition-ready",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					OCloudSiteName: "test-site",
					Description:    "Will transition to Ready=True",
				},
			}
			Expect(k8sClient.Create(ctx, rp)).To(Succeed())
			DeferCleanup(func() { deleteAndWait(rp) })

			// First event: DELETE (CR created without Ready status)
			event := waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Deleted),
				"ResourcePool created without Ready should emit DELETE")

			// Set Ready=True status with ResolvedOCloudSiteUID
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), rp)).To(Succeed())
			rp.Status.Conditions = []metav1.Condition{
				{
					Type:               inventoryv1alpha1.ConditionTypeReady,
					Status:             metav1.ConditionTrue,
					Reason:             inventoryv1alpha1.ReasonReady,
					Message:            "Resource is ready",
					LastTransitionTime: metav1.Now(),
				},
			}
			rp.Status.ResolvedOCloudSiteUID = fakeSiteUID
			Expect(k8sClient.Status().Update(ctx, rp)).To(Succeed())

			// Second event: UPDATE (now Ready=True, added to API)
			event = waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Updated),
				"ResourcePool transitioning to Ready=True should emit UPDATE")

			rpModel, ok := event.Object.(models.ResourcePool)
			Expect(ok).To(BeTrue())
			Expect(rpModel.Name).To(Equal("pool-transition-ready"))
		})

		It("should always emit Delete event regardless of Ready status", func() {
			err := ds.Watch(watchCtx)
			Expect(err).ToNot(HaveOccurred())
			waitForWatchReady(eventChannel)

			// Create a ResourcePool CR with Ready=False
			rp := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pool-delete-not-ready",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					OCloudSiteName: "test-site",
					Description:    "Delete with Ready=False",
				},
			}
			Expect(k8sClient.Create(ctx, rp)).To(Succeed())

			// Set Ready=False status
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), rp)).To(Succeed())
			rp.Status.Conditions = []metav1.Condition{
				{
					Type:               inventoryv1alpha1.ConditionTypeReady,
					Status:             metav1.ConditionFalse,
					Reason:             inventoryv1alpha1.ReasonParentNotReady,
					Message:            "Parent not ready",
					LastTransitionTime: metav1.Now(),
				},
			}
			Expect(k8sClient.Status().Update(ctx, rp)).To(Succeed())

			// Drain any events
			drainEvents(eventChannel)

			// Delete the ResourcePool CR
			Expect(k8sClient.Delete(ctx, rp)).To(Succeed())

			// Delete event SHOULD be emitted even though Ready=False
			event := waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Deleted))

			rpModel, ok := event.Object.(models.ResourcePool)
			Expect(ok).To(BeTrue())
			Expect(rpModel.Name).To(Equal("pool-delete-not-ready"))
		})

		It("should emit DELETE when ResourcePool transitions from Ready=True to Ready=False", func() {
			// A CR that was Ready and in the API becomes not Ready (e.g., parent deleted) and must
			// be removed from the API.

			err := ds.Watch(watchCtx)
			Expect(err).ToNot(HaveOccurred())
			waitForWatchReady(eventChannel)

			// Create a fake site UID for the status
			fakeSiteUID := uuid.New().String()

			rp := &inventoryv1alpha1.ResourcePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pool-ready-to-not-ready",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.ResourcePoolSpec{
					OCloudSiteName: "test-site",
					Description:    "Will transition Ready=True → Ready=False",
				},
			}
			Expect(k8sClient.Create(ctx, rp)).To(Succeed())
			DeferCleanup(func() { deleteAndWait(rp) })

			// First event: DELETE (CR created without Ready status)
			event := waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Deleted))

			// Set Ready=True status with ResolvedOCloudSiteUID (simulates CR becoming valid)
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), rp)).To(Succeed())
			rp.Status.Conditions = []metav1.Condition{
				{
					Type:               inventoryv1alpha1.ConditionTypeReady,
					Status:             metav1.ConditionTrue,
					Reason:             inventoryv1alpha1.ReasonReady,
					Message:            "Resource is ready",
					LastTransitionTime: metav1.Now(),
				},
			}
			rp.Status.ResolvedOCloudSiteUID = fakeSiteUID
			Expect(k8sClient.Status().Update(ctx, rp)).To(Succeed())

			// Second event: UPDATE (Ready=True, added to API)
			event = waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Updated),
				"ResourcePool becoming Ready=True should emit UPDATE")

			// Now transition to Ready=False (simulates parent being deleted)
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), rp)).To(Succeed())
			rp.Status.Conditions = []metav1.Condition{
				{
					Type:               inventoryv1alpha1.ConditionTypeReady,
					Status:             metav1.ConditionFalse,
					Reason:             inventoryv1alpha1.ReasonParentNotFound,
					Message:            "Parent site no longer exists",
					LastTransitionTime: metav1.Now(),
				},
			}
			// Note: ResolvedOCloudSiteUID may still be set, but Ready=False takes precedence
			Expect(k8sClient.Status().Update(ctx, rp)).To(Succeed())

			// Third event: DELETE (Ready=False means remove from API)
			event = waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Deleted),
				"ResourcePool transitioning to Ready=False should emit DELETE to remove from API")

			rpModel, ok := event.Object.(models.ResourcePool)
			Expect(ok).To(BeTrue())
			Expect(rpModel.Name).To(Equal("pool-ready-to-not-ready"))
		})
	})
})
