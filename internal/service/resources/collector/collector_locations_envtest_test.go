/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

const testNamespace = "test-locations"

var (
	testEnv        *envtest.Environment
	k8sClient      client.Client
	k8sWatchClient client.WithWatch // For Watch-capable client
	ctx            context.Context
	cancel         context.CancelFunc
)

func TestCollectorLocationsEnvtest(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Collector Location/OCloudSite Envtest Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.Background())

	scheme := runtime.NewScheme()
	Expect(corev1.AddToScheme(scheme)).To(Succeed())
	Expect(inventoryv1alpha1.AddToScheme(scheme)).To(Succeed())

	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
		Scheme:                scheme,
	}

	cfg, err := testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).ToNot(HaveOccurred())

	// Create watch-capable client for LocationDataSource tests
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
var _ = Describe("Location CEL Validation", Label("envtest"), func() {
	It("rejects Location without any address field", func() {
		loc := &inventoryv1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-loc-no-address",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.LocationSpec{
				GlobalLocationID: "LOC-INVALID",
				Name:             "Invalid Location",
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
				Name:             "Valid Location with Coordinate",
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
		Expect(fetched.Spec.Name).To(Equal("Valid Location with Coordinate"))
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
				Name:             "Valid Location with Civic Address",
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
		Expect(fetched.Spec.Name).To(Equal("Valid Location with Civic Address"))
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
				Name:             "Valid Location with Address",
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
		Expect(fetched.Spec.Name).To(Equal("Valid Location with Address"))
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
				Name:             "Invalid Latitude",
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
				Name:             "Invalid Longitude",
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
				Name:             "Invalid Latitude Pattern",
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
				Name:             "Valid Boundary Coordinates",
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
		Expect(fetched.Spec.Name).To(Equal("Valid Boundary Coordinates"))
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
				Name:             "Valid Location with Altitude",
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
		Expect(fetched.Spec.Name).To(Equal("Valid Location with Altitude"))
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
				Name:             "Invalid Site",
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
				Name:             "Valid Site",
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
		Expect(fetched.Spec.Name).To(Equal("Valid Site"))
		Expect(fetched.Spec.Description).To(Equal("A valid site"))
		Expect(fetched.Spec.Extensions).To(BeEmpty())
	})
})

var _ = Describe("LocationDataSource Watch", Label("envtest"), func() {
	var (
		ds           *LocationDataSource
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
		ds, err = NewLocationDataSource(testCloudID, k8sWatchClient)
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

	It("receives Created event when Location CR is created", func() {
		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Give the reflector time to start
		time.Sleep(100 * time.Millisecond)

		// Create a Location CR
		loc := &inventoryv1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-create",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.LocationSpec{
				GlobalLocationID: "LOC-WATCH-CREATE",
				Name:             "Watch Test Location",
				Description:      "Testing watch create",
				Address:          ptrTo("123 Watch Street"),
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, loc) })

		// Wait for the event
		var event *async.AsyncChangeEvent
		Eventually(func() bool {
			select {
			case event = <-eventChannel:
				// Skip SyncComplete events
				if event.EventType == async.SyncComplete {
					return false
				}
				return true
			default:
				return false
			}
		}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

		// Verify the event
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Updated))
		Expect(event.DataSourceID).To(Equal(testDSID))

		// Verify the object is a Location model
		locModel, ok := event.Object.(models.Location)
		Expect(ok).To(BeTrue())
		Expect(locModel.GlobalLocationID).To(Equal("LOC-WATCH-CREATE"))
		Expect(locModel.Name).To(Equal("Watch Test Location"))
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
				Name:             "Original Name",
				Description:      "Original description",
				Address:          ptrTo("Original Address"),
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, loc) })

		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Give the reflector time to start and process initial list
		time.Sleep(200 * time.Millisecond)

		// Drain initial events (create + possible sync)
		drainEvents(eventChannel)

		// Update the Location CR
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(loc), loc)).To(Succeed())
		loc.Spec.Name = "Updated Name"
		loc.Spec.Description = "Updated description"
		Expect(k8sClient.Update(ctx, loc)).To(Succeed())

		// Wait for the update event
		var event *async.AsyncChangeEvent
		Eventually(func() bool {
			select {
			case event = <-eventChannel:
				if event.EventType == async.SyncComplete {
					return false
				}
				return true
			default:
				return false
			}
		}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

		// Verify the event
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Updated))

		locModel, ok := event.Object.(models.Location)
		Expect(ok).To(BeTrue())
		Expect(locModel.GlobalLocationID).To(Equal("LOC-WATCH-UPDATE"))
		Expect(locModel.Name).To(Equal("Updated Name"))
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
				Name:             "To Be Deleted",
				Description:      "Will be deleted",
				Address:          ptrTo("Delete Street"),
			},
		}
		Expect(k8sClient.Create(ctx, loc)).To(Succeed())

		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Give the reflector time to start and process initial list
		time.Sleep(200 * time.Millisecond)

		// Drain initial events
		drainEvents(eventChannel)

		// Delete the Location CR
		Expect(k8sClient.Delete(ctx, loc)).To(Succeed())

		// Wait for the delete event
		var event *async.AsyncChangeEvent
		Eventually(func() bool {
			select {
			case event = <-eventChannel:
				if event.EventType == async.SyncComplete {
					return false
				}
				return true
			default:
				return false
			}
		}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

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

		time.Sleep(100 * time.Millisecond)

		// Create a Location CR with coordinates
		altitude := "100.5"
		loc := &inventoryv1alpha1.Location{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-coord",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.LocationSpec{
				GlobalLocationID: "LOC-WATCH-COORD",
				Name:             "Coordinate Test",
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

		// Wait for the event
		var event *async.AsyncChangeEvent
		Eventually(func() bool {
			select {
			case event = <-eventChannel:
				if event.EventType == async.SyncComplete {
					return false
				}
				return true
			default:
				return false
			}
		}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

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
				Name:           "Invalid Pool",
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
				Name:           "Invalid Pool",
				Description:    "Empty oCloudSiteId",
			},
		}
		err := k8sClient.Create(ctx, rp)
		Expect(err).To(HaveOccurred())
		// MinLength validation
	})

	It("rejects ResourcePool with empty name", func() {
		rp := &inventoryv1alpha1.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-rp-empty-name",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.ResourcePoolSpec{
				ResourcePoolId: "pool-001",
				OCloudSiteId:   "site-001",
				Name:           "", // Invalid: empty
				Description:    "Empty name",
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
				Name:           "Valid Resource Pool",
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
		Expect(fetched.Spec.Name).To(Equal("Valid Resource Pool"))
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
				Name:           "Full Resource Pool",
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
		Expect(fetched.Spec.Name).To(Equal("Full Resource Pool"))
		Expect(fetched.Spec.Description).To(Equal("A resource pool with all fields"))
		Expect(fetched.Spec.Extensions).To(HaveLen(3))
		Expect(fetched.Spec.Extensions["vendor"]).To(Equal("acme"))
		Expect(fetched.Spec.Extensions["tier"]).To(Equal("premium"))
		Expect(fetched.Spec.Extensions["rack"]).To(Equal("R42"))
	})
})

var _ = Describe("OCloudSiteDataSource Watch", Label("envtest"), func() {
	var (
		ds           *OCloudSiteDataSource
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
		ds, err = NewOCloudSiteDataSource(testCloudID, k8sWatchClient)
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

	It("receives Created event when OCloudSite CR is created", func() {
		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Give the reflector time to start
		time.Sleep(100 * time.Millisecond)

		// Create an OCloudSite CR
		site := &inventoryv1alpha1.OCloudSite{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-site-create",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.OCloudSiteSpec{
				SiteID:           "site-watch-create",
				GlobalLocationID: "LOC-001",
				Name:             "Watch Test Site",
				Description:      "Testing watch create for site",
			},
		}
		Expect(k8sClient.Create(ctx, site)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, site) })

		// Wait for the event
		var event *async.AsyncChangeEvent
		Eventually(func() bool {
			select {
			case event = <-eventChannel:
				// Skip SyncComplete events
				if event.EventType == async.SyncComplete {
					return false
				}
				return true
			default:
				return false
			}
		}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

		// Verify the event
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Updated))
		Expect(event.DataSourceID).To(Equal(testDSID))

		// Verify the object is an OCloudSite model
		siteModel, ok := event.Object.(models.OCloudSite)
		Expect(ok).To(BeTrue())
		Expect(siteModel.GlobalLocationID).To(Equal("LOC-001"))
		Expect(siteModel.Name).To(Equal("Watch Test Site"))
		Expect(siteModel.Description).To(Equal("Testing watch create for site"))
		Expect(siteModel.DataSourceID).To(Equal(testDSID))
		// OCloudSiteID should be a deterministically generated UUID
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
				SiteID:           "site-watch-update",
				GlobalLocationID: "LOC-001",
				Name:             "Original Site Name",
				Description:      "Original site description",
			},
		}
		Expect(k8sClient.Create(ctx, site)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, site) })

		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Give the reflector time to start and process initial list
		time.Sleep(200 * time.Millisecond)

		// Drain initial events (create + possible sync)
		drainEvents(eventChannel)

		// Update the OCloudSite CR
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(site), site)).To(Succeed())
		site.Spec.Name = "Updated Site Name"
		site.Spec.Description = "Updated site description"
		Expect(k8sClient.Update(ctx, site)).To(Succeed())

		// Wait for the update event
		var event *async.AsyncChangeEvent
		Eventually(func() bool {
			select {
			case event = <-eventChannel:
				if event.EventType == async.SyncComplete {
					return false
				}
				return true
			default:
				return false
			}
		}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

		// Verify the event
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Updated))

		siteModel, ok := event.Object.(models.OCloudSite)
		Expect(ok).To(BeTrue())
		Expect(siteModel.Name).To(Equal("Updated Site Name"))
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
				SiteID:           "site-watch-delete",
				GlobalLocationID: "LOC-001",
				Name:             "To Be Deleted Site",
				Description:      "Will be deleted",
			},
		}
		Expect(k8sClient.Create(ctx, site)).To(Succeed())

		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Give the reflector time to start and process initial list
		time.Sleep(200 * time.Millisecond)

		// Drain initial events
		drainEvents(eventChannel)

		// Delete the OCloudSite CR
		Expect(k8sClient.Delete(ctx, site)).To(Succeed())

		// Wait for the delete event
		var event *async.AsyncChangeEvent
		Eventually(func() bool {
			select {
			case event = <-eventChannel:
				if event.EventType == async.SyncComplete {
					return false
				}
				return true
			default:
				return false
			}
		}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

		// Verify the event
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Deleted))

		siteModel, ok := event.Object.(models.OCloudSite)
		Expect(ok).To(BeTrue())
		Expect(siteModel.GlobalLocationID).To(Equal("LOC-001"))
	})

	It("generates deterministic UUID for OCloudSiteID", func() {
		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		time.Sleep(100 * time.Millisecond)

		// Create an OCloudSite CR
		site := &inventoryv1alpha1.OCloudSite{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-site-uuid",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.OCloudSiteSpec{
				SiteID:           "site-uuid-test",
				GlobalLocationID: "LOC-001",
				Name:             "UUID Test Site",
				Description:      "Testing UUID generation",
			},
		}
		Expect(k8sClient.Create(ctx, site)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, site) })

		// Wait for the event
		var event *async.AsyncChangeEvent
		Eventually(func() bool {
			select {
			case event = <-eventChannel:
				if event.EventType == async.SyncComplete {
					return false
				}
				return true
			default:
				return false
			}
		}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

		// Get the OCloudSiteID from the event
		siteModel, ok := event.Object.(models.OCloudSite)
		Expect(ok).To(BeTrue())
		firstUUID := siteModel.OCloudSiteID

		// The UUID should be deterministic - creating a new datasource with the same cloudID
		// and processing the same siteId should produce the same UUID
		ds2, err := NewOCloudSiteDataSource(testCloudID, k8sWatchClient)
		Expect(err).ToNot(HaveOccurred())
		ds2.Init(testDSID, 0, nil) // nil channel ok for this test

		// Fetch the site and convert it manually
		fetchedSite := &inventoryv1alpha1.OCloudSite{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(site), fetchedSite)).To(Succeed())
		convertedModel := ds2.convertOCloudSiteToModel(fetchedSite)

		// The UUIDs should match
		Expect(convertedModel.OCloudSiteID).To(Equal(firstUUID))
	})
})

var _ = Describe("ResourcePoolDataSource Watch", Label("envtest"), func() {
	var (
		ds           *ResourcePoolDataSource
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
		ds, err = NewResourcePoolDataSource(testCloudID, k8sWatchClient)
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

	It("receives Created event when ResourcePool CR is created", func() {
		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Give the reflector time to start
		time.Sleep(100 * time.Millisecond)

		// Create a ResourcePool CR
		rp := &inventoryv1alpha1.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-rp-create",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.ResourcePoolSpec{
				ResourcePoolId: "pool-watch-create",
				OCloudSiteId:   "site-watch-create",
				Name:           "Watch Test Pool",
				Description:    "Testing watch create",
			},
		}
		Expect(k8sClient.Create(ctx, rp)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, rp) })

		// Wait for the event
		var event *async.AsyncChangeEvent
		Eventually(func() bool {
			select {
			case event = <-eventChannel:
				// Skip SyncComplete events
				if event.EventType == async.SyncComplete {
					return false
				}
				return true
			default:
				return false
			}
		}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

		// Verify the event
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Updated))
		Expect(event.DataSourceID).To(Equal(testDSID))

		// Verify the object is a ResourcePool model
		rpModel, ok := event.Object.(models.ResourcePool)
		Expect(ok).To(BeTrue())
		Expect(rpModel.Name).To(Equal("Watch Test Pool"))
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
				Name:           "Original Pool Name",
				Description:    "Original pool description",
			},
		}
		Expect(k8sClient.Create(ctx, rp)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, rp) })

		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Give the reflector time to start and process initial list
		time.Sleep(200 * time.Millisecond)

		// Drain initial events (create + possible sync)
		drainEvents(eventChannel)

		// Update the ResourcePool CR
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), rp)).To(Succeed())
		rp.Spec.Name = "Updated Pool Name"
		rp.Spec.Description = "Updated pool description"
		Expect(k8sClient.Update(ctx, rp)).To(Succeed())

		// Wait for the update event
		var event *async.AsyncChangeEvent
		Eventually(func() bool {
			select {
			case event = <-eventChannel:
				if event.EventType == async.SyncComplete {
					return false
				}
				return true
			default:
				return false
			}
		}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

		// Verify the event
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Updated))

		rpModel, ok := event.Object.(models.ResourcePool)
		Expect(ok).To(BeTrue())
		Expect(rpModel.Name).To(Equal("Updated Pool Name"))
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
				Name:           "To Be Deleted Pool",
				Description:    "Will be deleted",
			},
		}
		Expect(k8sClient.Create(ctx, rp)).To(Succeed())

		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		// Give the reflector time to start and process initial list
		time.Sleep(200 * time.Millisecond)

		// Drain initial events
		drainEvents(eventChannel)

		// Delete the ResourcePool CR
		Expect(k8sClient.Delete(ctx, rp)).To(Succeed())

		// Wait for the delete event
		var event *async.AsyncChangeEvent
		Eventually(func() bool {
			select {
			case event = <-eventChannel:
				if event.EventType == async.SyncComplete {
					return false
				}
				return true
			default:
				return false
			}
		}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

		// Verify the event
		Expect(event).ToNot(BeNil())
		Expect(event.EventType).To(Equal(async.Deleted))

		rpModel, ok := event.Object.(models.ResourcePool)
		Expect(ok).To(BeTrue())
		Expect(rpModel.Name).To(Equal("To Be Deleted Pool"))
	})

	It("generates deterministic UUID for ResourcePoolID", func() {
		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		time.Sleep(100 * time.Millisecond)

		// Create a ResourcePool CR
		rp := &inventoryv1alpha1.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-rp-uuid",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.ResourcePoolSpec{
				ResourcePoolId: "pool-uuid-test",
				OCloudSiteId:   "site-uuid-test",
				Name:           "UUID Test Pool",
				Description:    "Testing UUID generation",
			},
		}
		Expect(k8sClient.Create(ctx, rp)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, rp) })

		// Wait for the event
		var event *async.AsyncChangeEvent
		Eventually(func() bool {
			select {
			case event = <-eventChannel:
				if event.EventType == async.SyncComplete {
					return false
				}
				return true
			default:
				return false
			}
		}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

		// Get the ResourcePoolID from the event
		rpModel, ok := event.Object.(models.ResourcePool)
		Expect(ok).To(BeTrue())
		firstResourcePoolUUID := rpModel.ResourcePoolID
		firstOCloudSiteUUID := rpModel.OCloudSiteID

		// The UUID should be deterministic - creating a new datasource with the same cloudID
		// and processing the same resourcePoolId should produce the same UUID
		ds2, err := NewResourcePoolDataSource(testCloudID, k8sWatchClient)
		Expect(err).ToNot(HaveOccurred())
		ds2.Init(testDSID, 0, nil) // nil channel ok for this test

		// Fetch the pool and convert it manually
		fetchedPool := &inventoryv1alpha1.ResourcePool{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rp), fetchedPool)).To(Succeed())
		convertedModel := ds2.convertResourcePoolToModel(fetchedPool)

		// The UUIDs should match
		Expect(convertedModel.ResourcePoolID).To(Equal(firstResourcePoolUUID))
		Expect(convertedModel.OCloudSiteID).To(Equal(firstOCloudSiteUUID))
	})

	It("includes optional fields when provided", func() {
		// Start watching
		err := ds.Watch(watchCtx)
		Expect(err).ToNot(HaveOccurred())

		time.Sleep(100 * time.Millisecond)

		// Create a ResourcePool CR with all optional fields
		rp := &inventoryv1alpha1.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watch-test-rp-full",
				Namespace: testNamespace,
			},
			Spec: inventoryv1alpha1.ResourcePoolSpec{
				ResourcePoolId: "pool-full-test",
				OCloudSiteId:   "site-full-test",
				Name:           "Full Pool",
				Description:    "Pool with all fields",
				Extensions: map[string]string{
					"vendor": "acme",
					"tier":   "premium",
				},
			},
		}
		Expect(k8sClient.Create(ctx, rp)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, rp) })

		// Wait for the event
		var event *async.AsyncChangeEvent
		Eventually(func() bool {
			select {
			case event = <-eventChannel:
				if event.EventType == async.SyncComplete {
					return false
				}
				return true
			default:
				return false
			}
		}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

		// Verify all fields including optional ones
		rpModel, ok := event.Object.(models.ResourcePool)
		Expect(ok).To(BeTrue())
		Expect(rpModel.Name).To(Equal("Full Pool"))
		Expect(rpModel.Description).To(Equal("Pool with all fields"))
		Expect(rpModel.Extensions).To(HaveLen(2))
		Expect(rpModel.Extensions["vendor"]).To(Equal("acme"))
		Expect(rpModel.Extensions["tier"]).To(Equal("premium"))
	})
})

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
