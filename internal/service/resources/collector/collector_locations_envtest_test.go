/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
)

const testNamespace = "test-locations"

var (
	testEnv   *envtest.Environment
	k8sClient client.Client
	ctx       context.Context
	cancel    context.CancelFunc
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

var _ = Describe("Location/OCloudSite List Functions", Label("envtest"), func() {
	var c *Collector

	BeforeEach(func() {
		// Create a Collector with only hubClient set for testing list functions
		c = &Collector{hubClient: k8sClient}
	})

	Describe("listLocations", func() {
		It("returns empty list when no Locations exist", func() {
			locations, err := c.listLocations(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(locations).To(BeEmpty())
		})

		It("lists deployed Location CRs", func() {
			loc := &inventoryv1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-loc-list",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.LocationSpec{
					GlobalLocationID: "LOC-001",
					Name:             "Test Location",
					Description:      "Test location description",
					Address:          ptrTo("123 Test Street"),
				},
			}
			Expect(k8sClient.Create(ctx, loc)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, loc) })

			locations, err := c.listLocations(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(locations).To(HaveLen(1))

			// Verify all fields from the listed Location
			listed := locations[0]
			Expect(listed.Name).To(Equal("test-loc-list"))
			Expect(listed.Namespace).To(Equal(testNamespace))
			Expect(listed.Spec.GlobalLocationID).To(Equal("LOC-001"))
			Expect(listed.Spec.Name).To(Equal("Test Location"))
			Expect(listed.Spec.Description).To(Equal("Test location description"))
			Expect(listed.Spec.Address).ToNot(BeNil())
			Expect(*listed.Spec.Address).To(Equal("123 Test Street"))
			Expect(listed.Spec.Coordinate).To(BeNil())
			Expect(listed.Spec.CivicAddress).To(BeEmpty())
			Expect(listed.Spec.Extensions).To(BeEmpty())
		})
	})

	Describe("listOCloudSites", func() {
		It("returns empty list when no OCloudSites exist", func() {
			sites, err := c.listOCloudSites(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(sites).To(BeEmpty())
		})

		It("lists deployed OCloudSite CRs", func() {
			site := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-site-list",
					Namespace: testNamespace,
				},
				Spec: inventoryv1alpha1.OCloudSiteSpec{
					SiteID:           "site-001",
					GlobalLocationID: "LOC-001",
					Name:             "Test Site",
					Description:      "Test site description",
				},
			}
			Expect(k8sClient.Create(ctx, site)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, site) })

			sites, err := c.listOCloudSites(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(sites).To(HaveLen(1))

			// Verify all fields from the listed OCloudSite
			listed := sites[0]
			Expect(listed.Name).To(Equal("test-site-list"))
			Expect(listed.Namespace).To(Equal(testNamespace))
			Expect(listed.Spec.SiteID).To(Equal("site-001"))
			Expect(listed.Spec.GlobalLocationID).To(Equal("LOC-001"))
			Expect(listed.Spec.Name).To(Equal("Test Site"))
			Expect(listed.Spec.Description).To(Equal("Test site description"))
			Expect(listed.Spec.Extensions).To(BeEmpty())
		})
	})
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

func ptrTo(s string) *string {
	return &s
}
