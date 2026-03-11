/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package envtest

import (
	"context"
	"time"

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

		It("should NOT emit event for Location without Ready status", func() {
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
					Description: "Should not emit event",
					Address:     ptrTo("123 No Ready St"),
				},
			}
			Expect(k8sClient.Create(ctx, loc)).To(Succeed())
			DeferCleanup(func() { deleteAndWait(loc) })

			// Verify NO event is emitted (wait a short time to be sure)
			Consistently(func() bool {
				select {
				case event := <-eventChannel:
					// SyncComplete events are OK, but no Updated/Created for this CR
					if event.EventType == async.SyncComplete {
						return true
					}
					return false // Got an unexpected event
				default:
					return true // No event, expected
				}
			}, 500*time.Millisecond, 50*time.Millisecond).Should(BeTrue(),
				"should not emit event for Location without Ready=True status")
		})

		It("should NOT emit event for Location with Ready=False", func() {
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
					Description: "Should not emit event",
					Address:     ptrTo("456 Not Ready Ave"),
				},
			}
			Expect(k8sClient.Create(ctx, loc)).To(Succeed())
			DeferCleanup(func() { deleteAndWait(loc) })

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

			// Verify NO event is emitted
			Consistently(func() bool {
				select {
				case event := <-eventChannel:
					if event.EventType == async.SyncComplete {
						return true
					}
					return false
				default:
					return true
				}
			}, 500*time.Millisecond, 50*time.Millisecond).Should(BeTrue(),
				"should not emit event for Location with Ready=False status")
		})

		It("should emit event when Location transitions to Ready=True", func() {
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

			// Wait to confirm no event is emitted initially
			Consistently(func() bool {
				select {
				case event := <-eventChannel:
					if event.EventType == async.SyncComplete {
						return true
					}
					return false
				default:
					return true
				}
			}, 300*time.Millisecond, 50*time.Millisecond).Should(BeTrue())

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

			// Now an event SHOULD be emitted
			event := waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Updated))

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

		It("should NOT emit event for OCloudSite without Ready status", func() {
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
					Description:        "Should not emit event",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() { deleteAndWait(site) })

			Consistently(func() bool {
				select {
				case event := <-eventChannel:
					if event.EventType == async.SyncComplete {
						return true
					}
					return false
				default:
					return true
				}
			}, 500*time.Millisecond, 50*time.Millisecond).Should(BeTrue(),
				"should not emit event for OCloudSite without Ready=True status")
		})

		It("should emit event when OCloudSite transitions to Ready=True", func() {
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

			// Wait to confirm no event initially
			Consistently(func() bool {
				select {
				case event := <-eventChannel:
					if event.EventType == async.SyncComplete {
						return true
					}
					return false
				default:
					return true
				}
			}, 300*time.Millisecond, 50*time.Millisecond).Should(BeTrue())

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

			event := waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Updated))

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

		It("should NOT emit event for ResourcePool without Ready status", func() {
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
					Description:    "Should not emit event",
				},
			}
			Expect(k8sClient.Create(ctx, rp)).To(Succeed())
			DeferCleanup(func() { deleteAndWait(rp) })

			Consistently(func() bool {
				select {
				case event := <-eventChannel:
					if event.EventType == async.SyncComplete {
						return true
					}
					return false
				default:
					return true
				}
			}, 500*time.Millisecond, 50*time.Millisecond).Should(BeTrue(),
				"should not emit event for ResourcePool without Ready=True status")
		})

		It("should emit event when ResourcePool transitions to Ready=True", func() {
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

			// Wait to confirm no event initially
			Consistently(func() bool {
				select {
				case event := <-eventChannel:
					if event.EventType == async.SyncComplete {
						return true
					}
					return false
				default:
					return true
				}
			}, 300*time.Millisecond, 50*time.Millisecond).Should(BeTrue())

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

			event := waitForEvent(eventChannel)
			Expect(event).ToNot(BeNil())
			Expect(event.EventType).To(Equal(async.Updated))

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
	})
})
