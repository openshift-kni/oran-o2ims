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

var _ = Describe("Location CEL Validation", Label("envtest"), func() {
	It("rejects Location without any address field", func() {
		loc := &inventoryv1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-loc-no-address",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.LocationSpec{
				GlobalLocationID: "LOC-INVALID",
				Description:      "Missing address fields",
				// Missing: coordinate, civicAddress, AND address
			},
		}
		err := k8sClient.Create(ctx, loc)
		Expect(err).To(MatchError(ContainSubstring("at least one of coordinate, civicAddress, or address")))
	})

	It("accepts Location with coordinate field", func() {
		loc := &inventoryv1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-loc-coord",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.LocationSpec{
				GlobalLocationID: "LOC-COORD",
				Description:      "Has coordinate",
				Coordinate: &inventoryv1alpha1.GeoLocation{
					Latitude:  "40.7128",
					Longitude: "-74.0060",
				},
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, loc) })

		// verify all fields
		fetched := &inventoryv1alpha1.Location{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), fetched)).To(Succeed())
		Expect(fetched.Spec.GlobalLocationID).To(Equal("LOC-COORD"))
		Expect(fetched.Name).To(Equal("valid-loc-coord"))
		Expect(fetched.Spec.Description).To(Equal("Has coordinate"))
		Expect(fetched.Spec.Coordinate).ToNot(BeNil())
		Expect(fetched.Spec.Coordinate.Latitude).To(Equal("40.7128"))
		Expect(fetched.Spec.Coordinate.Longitude).To(Equal("-74.0060"))
		Expect(fetched.Spec.Coordinate.Altitude).To(BeNil())
		Expect(fetched.Spec.CivicAddress).To(BeEmpty())
		Expect(fetched.Spec.Address).To(BeNil())
		Expect(fetched.Spec.Extensions).To(BeEmpty())
	})

	It("accepts Location with civicAddress field", func() {
		loc := &inventoryv1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-loc-civic",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.LocationSpec{
				GlobalLocationID: "LOC-CIVIC",
				Description:      "Has civic address",
				CivicAddress: []inventoryv1alpha1.CivicAddressElement{
					{CaType: 0, CaValue: "US"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, loc) })

		// verify all fields
		fetched := &inventoryv1alpha1.Location{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), fetched)).To(Succeed())
		Expect(fetched.Spec.GlobalLocationID).To(Equal("LOC-CIVIC"))
		Expect(fetched.Name).To(Equal("valid-loc-civic"))
		Expect(fetched.Spec.Description).To(Equal("Has civic address"))
		Expect(fetched.Spec.Coordinate).To(BeNil())
		Expect(fetched.Spec.CivicAddress).To(HaveLen(1))
		Expect(fetched.Spec.CivicAddress[0].CaType).To(Equal(0))
		Expect(fetched.Spec.CivicAddress[0].CaValue).To(Equal("US"))
		Expect(fetched.Spec.Address).To(BeNil())
		Expect(fetched.Spec.Extensions).To(BeEmpty())
	})

	It("accepts Location with address field", func() {
		loc := &inventoryv1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-loc-addr",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.LocationSpec{
				GlobalLocationID: "LOC-ADDR",
				Description:      "Has address string",
				Address:          ptrTo("123 Main St, City, Country"),
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, loc) })

		// verify all fields
		fetched := &inventoryv1alpha1.Location{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), fetched)).To(Succeed())
		Expect(fetched.Spec.GlobalLocationID).To(Equal("LOC-ADDR"))
		Expect(fetched.Name).To(Equal("valid-loc-addr"))
		Expect(fetched.Spec.Description).To(Equal("Has address string"))
		Expect(fetched.Spec.Coordinate).To(BeNil())
		Expect(fetched.Spec.CivicAddress).To(BeEmpty())
		Expect(fetched.Spec.Address).ToNot(BeNil())
		Expect(*fetched.Spec.Address).To(Equal("123 Main St, City, Country"))
		Expect(fetched.Spec.Extensions).To(BeEmpty())
	})

	It("validates latitude range - rejects invalid latitude", func() {
		loc := &inventoryv1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-lat",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.LocationSpec{
				GlobalLocationID: "LOC-LAT-INVALID",
				Description:      "Latitude out of range",
				Coordinate: &inventoryv1alpha1.GeoLocation{
					Latitude:  "100.0", // Invalid: > 90
					Longitude: "0.0",
				},
			},
		}
		err := k8sClient.Create(ctx, loc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("latitude must be between -90.0 and 90.0"))
	})

	It("validates longitude range - rejects invalid longitude", func() {
		loc := &inventoryv1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-lon",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.LocationSpec{
				GlobalLocationID: "LOC-LON-INVALID",
				Description:      "Longitude out of range",
				Coordinate: &inventoryv1alpha1.GeoLocation{
					Latitude:  "0.0",
					Longitude: "200.0", // Invalid: > 180
				},
			},
		}
		err := k8sClient.Create(ctx, loc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("longitude must be between -180.0 and 180.0"))
	})

	It("validates latitude pattern - rejects non-numeric string", func() {
		loc := &inventoryv1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-lat-pattern",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.LocationSpec{
				GlobalLocationID: "LOC-LAT-PATTERN",
				Description:      "Latitude is not a number",
				Coordinate: &inventoryv1alpha1.GeoLocation{
					Latitude:  "not-a-number",
					Longitude: "0.0",
				},
			},
		}
		err := k8sClient.Create(ctx, loc)
		Expect(err).To(HaveOccurred())
		// Pattern validation error
	})

	It("accepts valid latitude and longitude at boundary values", func() {
		loc := &inventoryv1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-boundary-coords",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.LocationSpec{
				GlobalLocationID: "LOC-BOUNDARY",
				Description:      "At boundary values",
				Coordinate: &inventoryv1alpha1.GeoLocation{
					Latitude:  "90.0",   // Max valid latitude
					Longitude: "-180.0", // Min valid longitude
				},
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, loc) })

		// verify all fields
		fetched := &inventoryv1alpha1.Location{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), fetched)).To(Succeed())
		Expect(fetched.Spec.GlobalLocationID).To(Equal("LOC-BOUNDARY"))
		Expect(fetched.Name).To(Equal("valid-boundary-coords"))
		Expect(fetched.Spec.Description).To(Equal("At boundary values"))
		Expect(fetched.Spec.Coordinate).ToNot(BeNil())
		Expect(fetched.Spec.Coordinate.Latitude).To(Equal("90.0"))
		Expect(fetched.Spec.Coordinate.Longitude).To(Equal("-180.0"))
		Expect(fetched.Spec.Coordinate.Altitude).To(BeNil())
		Expect(fetched.Spec.CivicAddress).To(BeEmpty())
		Expect(fetched.Spec.Address).To(BeNil())
		Expect(fetched.Spec.Extensions).To(BeEmpty())
	})

	It("accepts Location with optional altitude", func() {
		alt := "100.5"
		loc := &inventoryv1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-with-altitude",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.LocationSpec{
				GlobalLocationID: "LOC-ALT",
				Description:      "Has altitude",
				Coordinate: &inventoryv1alpha1.GeoLocation{
					Latitude:  "40.7128",
					Longitude: "-74.0060",
					Altitude:  &alt,
				},
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, loc) })

		// verify all fields
		fetched := &inventoryv1alpha1.Location{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), fetched)).To(Succeed())
		Expect(fetched.Spec.GlobalLocationID).To(Equal("LOC-ALT"))
		Expect(fetched.Name).To(Equal("valid-with-altitude"))
		Expect(fetched.Spec.Description).To(Equal("Has altitude"))
		Expect(fetched.Spec.Coordinate).ToNot(BeNil())
		Expect(fetched.Spec.Coordinate.Latitude).To(Equal("40.7128"))
		Expect(fetched.Spec.Coordinate.Longitude).To(Equal("-74.0060"))
		Expect(fetched.Spec.Coordinate.Altitude).ToNot(BeNil())
		Expect(*fetched.Spec.Coordinate.Altitude).To(Equal("100.5"))
		Expect(fetched.Spec.CivicAddress).To(BeEmpty())
		Expect(fetched.Spec.Address).To(BeNil())
		Expect(fetched.Spec.Extensions).To(BeEmpty())
	})
})

var _ = Describe("OCloudSite Validation", Label("envtest"), func() {
	It("rejects OCloudSite with empty siteId", func() {
		site := &inventoryv1alpha1.OCloudSite{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-site-empty-siteid",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.OCloudSiteSpec{
				SiteID:           "", // Invalid: empty
				GlobalLocationID: "LOC-001",
				Description:      "Empty siteId",
			},
		}
		err := k8sClient.Create(ctx, site)
		Expect(err).To(HaveOccurred())
		// MinLength validation
	})

	It("accepts valid OCloudSite", func() {
		site := &inventoryv1alpha1.OCloudSite{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-site",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.OCloudSiteSpec{
				SiteID:           "site-valid",
				GlobalLocationID: "LOC-001",
				Description:      "A valid site",
			},
		}
		Expect(k8sClient.Create(ctx, site)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, site) })

		// verify all fields
		fetched := &inventoryv1alpha1.OCloudSite{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetched)).To(Succeed())
		Expect(fetched.Spec.SiteID).To(Equal("site-valid"))
		Expect(fetched.Spec.GlobalLocationID).To(Equal("LOC-001"))
		Expect(fetched.Name).To(Equal("valid-site"))
		Expect(fetched.Spec.Description).To(Equal("A valid site"))
		Expect(fetched.Spec.Extensions).To(BeEmpty())
	})
})

var _ = Describe("ResourcePool Validation", Label("envtest"), func() {
	It("rejects ResourcePool with empty resourcePoolId", func() {
		rp := &inventoryv1alpha1.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-rp-empty-poolid",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.ResourcePoolSpec{
				ResourcePoolId: "", // Invalid: empty
				OCloudSiteId:   "site-001",
				Description:    "Empty resourcePoolId",
			},
		}
		err := k8sClient.Create(ctx, rp)
		Expect(err).To(HaveOccurred())
		// MinLength validation
	})

	It("rejects ResourcePool with empty oCloudSiteId", func() {
		rp := &inventoryv1alpha1.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-rp-empty-siteid",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.ResourcePoolSpec{
				ResourcePoolId: "pool-001",
				OCloudSiteId:   "", // Invalid: empty
				Description:    "Empty oCloudSiteId",
			},
		}
		err := k8sClient.Create(ctx, rp)
		Expect(err).To(HaveOccurred())
		// MinLength validation
	})

	It("accepts valid ResourcePool with required fields", func() {
		rp := &inventoryv1alpha1.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-rp-basic",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.ResourcePoolSpec{
				ResourcePoolId: "pool-valid-001",
				OCloudSiteId:   "site-valid-001",
				Description:    "A valid resource pool",
			},
		}
		Expect(k8sClient.Create(ctx, rp)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, rp) })

		// Verify all fields
		fetched := &inventoryv1alpha1.ResourcePool{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), fetched)).To(Succeed())
		Expect(fetched.Spec.ResourcePoolId).To(Equal("pool-valid-001"))
		Expect(fetched.Spec.OCloudSiteId).To(Equal("site-valid-001"))
		Expect(fetched.Name).To(Equal("valid-rp-basic"))
		Expect(fetched.Spec.Description).To(Equal("A valid resource pool"))
		Expect(fetched.Spec.Extensions).To(BeEmpty())
	})

	It("accepts valid ResourcePool with all optional fields", func() {
		rp := &inventoryv1alpha1.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-rp-full",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.ResourcePoolSpec{
				ResourcePoolId: "pool-full-001",
				OCloudSiteId:   "site-full-001",
				Description:    "A resource pool with all fields",
				Extensions: map[string]string{
					"vendor": "acme",
					"tier":   "premium",
					"rack":   "R42",
				},
			},
		}
		Expect(k8sClient.Create(ctx, rp)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, rp) })

		// Verify all fields
		fetched := &inventoryv1alpha1.ResourcePool{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), fetched)).To(Succeed())
		Expect(fetched.Spec.ResourcePoolId).To(Equal("pool-full-001"))
		Expect(fetched.Spec.OCloudSiteId).To(Equal("site-full-001"))
		Expect(fetched.Name).To(Equal("valid-rp-full"))
		Expect(fetched.Spec.Description).To(Equal("A resource pool with all fields"))
		Expect(fetched.Spec.Extensions).To(HaveLen(3))
		Expect(fetched.Spec.Extensions["vendor"]).To(Equal("acme"))
		Expect(fetched.Spec.Extensions["tier"]).To(Equal("premium"))
		Expect(fetched.Spec.Extensions["rack"]).To(Equal("R42"))
	})
})

var _ = Describe("LocationDataSource Watch", Label("envtest"), func() {
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
		ds, err = collector.NewLocationDataSource(testCloudID, k8sWatchClient)
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

	It("receives Created event when Location CR is created with Ready=True", func() {
		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Wait for the watch to be ready (SyncComplete event)
		waitForWatchReady(eventChannel)

		// Create a Location CR
		loc := &inventoryv1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-create",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.LocationSpec{
				GlobalLocationID: "LOC-WATCH-CREATE",
				Description:      "Testing watch create",
				Address:          ptrTo("123 Watch Street"),
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, loc) })

		// Set Ready=True status (required for event to be emitted)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), loc)).To(Succeed())
		loc.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
		Expect(k8sClient.Status().Update(ctx, loc)).To(Succeed())

		// Wait for the event
		event := waitForEvent(eventChannel)

		// Verify the event
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Updated))
		Expect(event.DataSourceID).To(Equal(testDSID))

		// Verify the object is a Location model
		locModel, ok := event.Object.(models.Location)
		Expect(ok).To(BeTrue())
		Expect(locModel.GlobalLocationID).To(Equal("LOC-WATCH-CREATE"))
		Expect(locModel.Name).To(Equal("watch-test-create"))
		Expect(locModel.Description).To(Equal("Testing watch create"))
		Expect(locModel.Address).ToNot(BeNil())
		Expect(*locModel.Address).To(Equal("123 Watch Street"))
		Expect(locModel.DataSourceID).To(Equal(testDSID))
	})

	It("receives Updated event when Location CR is modified", func() {
		// Create a Location CR first
		loc := &inventoryv1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-update",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.LocationSpec{
				GlobalLocationID: "LOC-WATCH-UPDATE",
				Description:      "Original description",
				Address:          ptrTo("Original Address"),
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, loc) })

		// Set Ready=True status (required for events to be emitted)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), loc)).To(Succeed())
		loc.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
		Expect(k8sClient.Status().Update(ctx, loc)).To(Succeed())

		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Wait for the watch to be ready (SyncComplete event)
		waitForWatchReady(eventChannel)

		// Drain initial events (create + possible sync)
		drainEvents(eventChannel)

		// Update the Location CR (update description since Name is now in metadata)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), loc)).To(Succeed())
		loc.Spec.Description = "Updated description"
		Expect(k8sClient.Update(ctx, loc)).To(Succeed())

		// Wait for the update event
		event := waitForEvent(eventChannel)

		// Verify the event
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Updated))

		locModel, ok := event.Object.(models.Location)
		Expect(ok).To(BeTrue())
		Expect(locModel.GlobalLocationID).To(Equal("LOC-WATCH-UPDATE"))
		Expect(locModel.Name).To(Equal("watch-test-update"))
		Expect(locModel.Description).To(Equal("Updated description"))
	})

	It("receives Deleted event when Location CR is deleted", func() {
		// Create a Location CR first
		loc := &inventoryv1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-delete",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.LocationSpec{
				GlobalLocationID: "LOC-WATCH-DELETE",
				Description:      "Will be deleted",
				Address:          ptrTo("Delete Street"),
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())

		// Set Ready=True status
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), loc)).To(Succeed())
		loc.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
		Expect(k8sClient.Status().Update(ctx, loc)).To(Succeed())

		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Wait for the watch to be ready (SyncComplete event)
		waitForWatchReady(eventChannel)

		// Drain initial events
		drainEvents(eventChannel)

		// Delete the Location CR
		Expect(k8sClient.Delete(ctx, loc)).To(Succeed())

		// Wait for the delete event
		event := waitForEvent(eventChannel)

		// Verify the event
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Deleted))

		locModel, ok := event.Object.(models.Location)
		Expect(ok).To(BeTrue())
		Expect(locModel.GlobalLocationID).To(Equal("LOC-WATCH-DELETE"))
	})

	It("converts coordinate to GeoJSON format", func() {
		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Wait for the watch to be ready (SyncComplete event)
		waitForWatchReady(eventChannel)

		// Create a Location CR with coordinates
		altitude := "100.5"
		loc := &inventoryv1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-coord",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.LocationSpec{
				GlobalLocationID: "LOC-WATCH-COORD",
				Description:      "Testing coordinate conversion",
				Coordinate: &inventoryv1alpha1.GeoLocation{
					Latitude:  "40.7128",
					Longitude: "-74.0060",
					Altitude:  &altitude,
				},
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, loc) })

		// Set Ready=True status (required for event to be emitted)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), loc)).To(Succeed())
		loc.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
		Expect(k8sClient.Status().Update(ctx, loc)).To(Succeed())

		// Wait for the event
		event := waitForEvent(eventChannel)

		// Verify the coordinate was converted to GeoJSON
		locModel, ok := event.Object.(models.Location)
		Expect(ok).To(BeTrue())
		Expect(locModel.Coordinate).ToNot(BeNil())
		Expect(locModel.Coordinate["type"]).To(Equal("Point"))

		coords, ok := locModel.Coordinate["coordinates"].([]float64)
		Expect(ok).To(BeTrue())
		Expect(coords).To(HaveLen(3))                              // [longitude, latitude, altitude]
		Expect(coords[0]).To(BeNumerically("~", -74.0060, 0.0001)) // longitude
		Expect(coords[1]).To(BeNumerically("~", 40.7128, 0.0001))  // latitude
		Expect(coords[2]).To(BeNumerically("~", 100.5, 0.0001))    // altitude
	})

	It("converts civicAddress to database format through watch events", func() {
		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Wait for the watch to be ready (SyncComplete event)
		waitForWatchReady(eventChannel)

		// Create a Location CR with civic address (RFC 4776 format)
		loc := &inventoryv1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-civic",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.LocationSpec{
				GlobalLocationID: "LOC-WATCH-CIVIC",
				Description:      "Testing civicAddress conversion",
				CivicAddress: []inventoryv1alpha1.CivicAddressElement{
					{CaType: 0, CaValue: "US"},        // Country (ISO 3166-1)
					{CaType: 1, CaValue: "Virginia"},  // State/Province
					{CaType: 3, CaValue: "Ashburn"},   // City
					{CaType: 6, CaValue: "20147"},     // Postal code
					{CaType: 22, CaValue: "Tech Way"}, // Street name
					{CaType: 26, CaValue: "123"},      // Building number
				},
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, loc) })

		// Set Ready=True status (required for event to be emitted)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), loc)).To(Succeed())
		loc.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
		Expect(k8sClient.Status().Update(ctx, loc)).To(Succeed())

		// Wait for the event
		event := waitForEvent(eventChannel)

		// Verify the civicAddress was converted to database format
		locModel, ok := event.Object.(models.Location)
		Expect(ok).To(BeTrue())
		Expect(locModel.GlobalLocationID).To(Equal("LOC-WATCH-CIVIC"))
		Expect(locModel.CivicAddress).ToNot(BeNil())
		Expect(locModel.CivicAddress).To(HaveLen(6))

		// Verify each civic address element was converted to map format
		Expect(locModel.CivicAddress[0]["caType"]).To(Equal(0))
		Expect(locModel.CivicAddress[0]["caValue"]).To(Equal("US"))
		Expect(locModel.CivicAddress[1]["caType"]).To(Equal(1))
		Expect(locModel.CivicAddress[1]["caValue"]).To(Equal("Virginia"))
		Expect(locModel.CivicAddress[2]["caType"]).To(Equal(3))
		Expect(locModel.CivicAddress[2]["caValue"]).To(Equal("Ashburn"))
		Expect(locModel.CivicAddress[3]["caType"]).To(Equal(6))
		Expect(locModel.CivicAddress[3]["caValue"]).To(Equal("20147"))
		Expect(locModel.CivicAddress[4]["caType"]).To(Equal(22))
		Expect(locModel.CivicAddress[4]["caValue"]).To(Equal("Tech Way"))
		Expect(locModel.CivicAddress[5]["caType"]).To(Equal(26))
		Expect(locModel.CivicAddress[5]["caValue"]).To(Equal("123"))
	})

	It("propagates extensions through watch events", func() {
		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Wait for the watch to be ready (SyncComplete event)
		waitForWatchReady(eventChannel)

		// Create a Location CR with extensions
		loc := &inventoryv1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-ext",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.LocationSpec{
				GlobalLocationID: "LOC-WATCH-EXT",
				Description:      "Testing extensions propagation",
				Address:          ptrTo("123 Extension St"),
				Extensions: map[string]string{
					"region":      "us-east",
					"tier":        "primary",
					"datacenter":  "dc-01",
					"environment": "production",
				},
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, loc) })

		// Set Ready=True status (required for event to be emitted)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), loc)).To(Succeed())
		loc.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
		Expect(k8sClient.Status().Update(ctx, loc)).To(Succeed())

		// Wait for the event
		event := waitForEvent(eventChannel)

		// Verify extensions were propagated correctly
		locModel, ok := event.Object.(models.Location)
		Expect(ok).To(BeTrue())
		Expect(locModel.GlobalLocationID).To(Equal("LOC-WATCH-EXT"))
		Expect(locModel.Extensions).ToNot(BeNil())
		Expect(locModel.Extensions).To(HaveLen(4))
		Expect(locModel.Extensions["region"]).To(Equal("us-east"))
		Expect(locModel.Extensions["tier"]).To(Equal("primary"))
		Expect(locModel.Extensions["datacenter"]).To(Equal("dc-01"))
		Expect(locModel.Extensions["environment"]).To(Equal("production"))
	})
})

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

func ptrTo(s string) *string {
	return &s
}
