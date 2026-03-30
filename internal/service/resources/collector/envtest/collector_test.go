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

var _ = Describe("Location CEL Validation", Label("envtest"), func() {
	It("rejects Location without any address field", func() {
		loc := &inventoryv1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-loc-no-address",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.LocationSpec{
				Description: "Missing address fields",
				// Missing: coordinate, civicAddress, AND address
			},
		}
		err := k8sClient.Create(ctx, loc)
		Expect(err).To(MatchError(ContainSubstring("at least one of coordinate, non-empty civicAddress, or non-empty address must be specified")))
	})

	It("accepts Location with coordinate field", func() {
		loc := &inventoryv1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-loc-coord",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.LocationSpec{
				Description: "Has coordinate",
				Coordinate: &inventoryv1alpha1.GeoLocation{
					Latitude:  "40.7128",
					Longitude: "-74.0060",
				},
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { deleteAndWait(loc) })

		// verify all fields
		fetched := &inventoryv1alpha1.Location{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), fetched)).To(Succeed())
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
				Description: "Has civic address",
				CivicAddress: []inventoryv1alpha1.CivicAddressElement{
					{CaType: 0, CaValue: "US"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { deleteAndWait(loc) })

		// verify all fields
		fetched := &inventoryv1alpha1.Location{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), fetched)).To(Succeed())
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
				Description: "Has address string",
				Address:     ptrTo("123 Main St, City, Country"),
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { deleteAndWait(loc) })

		// verify all fields
		fetched := &inventoryv1alpha1.Location{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), fetched)).To(Succeed())
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
				Description: "Latitude out of range",
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
				Description: "Longitude out of range",
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
				Description: "Latitude is not a number",
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
				Description: "At boundary values",
				Coordinate: &inventoryv1alpha1.GeoLocation{
					Latitude:  "90.0",   // Max valid latitude
					Longitude: "-180.0", // Min valid longitude
				},
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { deleteAndWait(loc) })

		// verify all fields
		fetched := &inventoryv1alpha1.Location{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), fetched)).To(Succeed())
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
				Description: "Has altitude",
				Coordinate: &inventoryv1alpha1.GeoLocation{
					Latitude:  "40.7128",
					Longitude: "-74.0060",
					Altitude:  &alt,
				},
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { deleteAndWait(loc) })

		// verify all fields
		fetched := &inventoryv1alpha1.Location{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), fetched)).To(Succeed())
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
	It("rejects OCloudSite with empty globalLocationName", func() {
		site := &inventoryv1alpha1.OCloudSite{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-site-empty-locname",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.OCloudSiteSpec{
				GlobalLocationName: "", // Invalid: empty
				Description:        "Empty globalLocationName",
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
				GlobalLocationName: "test-location",
				Description:        "A valid site",
			},
		}
		Expect(k8sClient.Create(ctx, site)).To(Succeed())
		DeferCleanup(func() { deleteAndWait(site) })

		// verify all fields
		fetched := &inventoryv1alpha1.OCloudSite{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetched)).To(Succeed())
		Expect(fetched.Spec.GlobalLocationName).To(Equal("test-location"))
		Expect(fetched.Name).To(Equal("valid-site"))
		Expect(fetched.Spec.Description).To(Equal("A valid site"))
		Expect(fetched.Spec.Extensions).To(BeEmpty())
	})
})

var _ = Describe("ResourcePool Validation", Label("envtest"), func() {
	It("rejects ResourcePool with empty oCloudSiteName", func() {
		rp := &inventoryv1alpha1.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-rp-empty-sitename",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.ResourcePoolSpec{
				OCloudSiteName: "", // Invalid: empty
				Description:    "Empty oCloudSiteName",
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
				OCloudSiteName: "test-site",
				Description:    "A valid resource pool",
			},
		}
		Expect(k8sClient.Create(ctx, rp)).To(Succeed())
		DeferCleanup(func() { deleteAndWait(rp) })

		// Verify all fields
		fetched := &inventoryv1alpha1.ResourcePool{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), fetched)).To(Succeed())
		Expect(fetched.Spec.OCloudSiteName).To(Equal("test-site"))
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
				OCloudSiteName: "test-site-full",
				Description:    "A resource pool with all fields",
				Extensions: map[string]string{
					"vendor": "acme",
					"tier":   "premium",
					"rack":   "R42",
				},
			},
		}
		Expect(k8sClient.Create(ctx, rp)).To(Succeed())
		DeferCleanup(func() { deleteAndWait(rp) })

		// Verify all fields
		fetched := &inventoryv1alpha1.ResourcePool{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), fetched)).To(Succeed())
		Expect(fetched.Spec.OCloudSiteName).To(Equal("test-site-full"))
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
		ds, err = collector.NewLocationDataSource(testCloudID, newWatchClient())
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

	It("receives Updated event when Location CR is created with Ready=True", func() {
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
				Description: "Testing watch create",
				Address:     ptrTo("123 Watch Street"),
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { deleteAndWait(loc) })

		// First event: DELETE (CR created without Ready status)
		event := waitForEvent(eventChannel)
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Deleted),
			"Location created without Ready should emit DELETE")

		// Set Ready=True status (required for Updated event to be emitted)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), loc)).To(Succeed())
		loc.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
		Expect(k8sClient.Status().Update(ctx, loc)).To(Succeed())

		// Second event: Updated (CR now Ready=True)
		event = waitForEvent(eventChannel)

		// Verify the event
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Updated))
		Expect(event.DataSourceID).To(Equal(testDSID))

		// Verify the object is a Location model
		locModel, ok := event.Object.(models.Location)
		Expect(ok).To(BeTrue())
		Expect(locModel.GlobalLocationID).To(Equal("watch-test-create")) // metadata.name is globalLocationId
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
				Description: "Original description",
				Address:     ptrTo("Original Address"),
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { deleteAndWait(loc) })

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

		// Wait for the update event (use name filter to avoid stale events from previous tests)
		event := waitForEvent(eventChannel)

		// Verify the event
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Updated))

		locModel, ok := event.Object.(models.Location)
		Expect(ok).To(BeTrue())
		Expect(locModel.GlobalLocationID).To(Equal("watch-test-update"))
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
				Description: "Will be deleted",
				Address:     ptrTo("Delete Street"),
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

		// Wait for the delete event (use name filter to avoid stale events from previous tests)
		event := waitForEvent(eventChannel)

		// Verify the event
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Deleted))

		locModel, ok := event.Object.(models.Location)
		Expect(ok).To(BeTrue())
		Expect(locModel.GlobalLocationID).To(Equal("watch-test-delete"))
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
				Description: "Testing coordinate conversion",
				Coordinate: &inventoryv1alpha1.GeoLocation{
					Latitude:  "40.7128",
					Longitude: "-74.0060",
					Altitude:  &altitude,
				},
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { deleteAndWait(loc) })

		// Set Ready=True status (required for event to be emitted)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), loc)).To(Succeed())
		loc.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
		Expect(k8sClient.Status().Update(ctx, loc)).To(Succeed())

		// Wait for the event (use name filter to avoid stale events from previous tests)
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
				Description: "Testing civicAddress conversion",
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
		DeferCleanup(func() { deleteAndWait(loc) })

		// Set Ready=True status (required for event to be emitted)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), loc)).To(Succeed())
		loc.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
		Expect(k8sClient.Status().Update(ctx, loc)).To(Succeed())

		// Wait for the event (use name filter to avoid stale events from previous tests)
		event := waitForEvent(eventChannel)

		// Verify the civicAddress was converted to database format
		locModel, ok := event.Object.(models.Location)
		Expect(ok).To(BeTrue())
		Expect(locModel.GlobalLocationID).To(Equal("watch-test-civic"))
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
				Description: "Testing extensions propagation",
				Address:     ptrTo("123 Extension St"),
				Extensions: map[string]string{
					"region":      "us-east",
					"tier":        "primary",
					"datacenter":  "dc-01",
					"environment": "production",
				},
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { deleteAndWait(loc) })

		// Set Ready=True status (required for event to be emitted)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), loc)).To(Succeed())
		loc.Status.Conditions = []metav1.Condition{
			{Type: inventoryv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: inventoryv1alpha1.ReasonReady, LastTransitionTime: metav1.Now()},
		}
		Expect(k8sClient.Status().Update(ctx, loc)).To(Succeed())

		// Wait for the event (use name filter to avoid stale events from previous tests)
		event := waitForEvent(eventChannel)

		// Verify extensions were propagated correctly
		locModel, ok := event.Object.(models.Location)
		Expect(ok).To(BeTrue())
		Expect(locModel.GlobalLocationID).To(Equal("watch-test-ext"))
		Expect(locModel.Extensions).ToNot(BeNil())
		Expect(locModel.Extensions).To(HaveLen(4))
		Expect(locModel.Extensions["region"]).To(Equal("us-east"))
		Expect(locModel.Extensions["tier"]).To(Equal("primary"))
		Expect(locModel.Extensions["datacenter"]).To(Equal("dc-01"))
		Expect(locModel.Extensions["environment"]).To(Equal("production"))
	})
})
