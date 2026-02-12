/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector_test

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
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/collector"
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

var _ = Describe("Location/OCloudSite List Functions", func() {
	var c *collector.Collector

	BeforeEach(func() {
		c = collector.NewCollectorForTest(k8sClient)
	})

	Describe("ListLocations", func() {
		It("returns empty list when no Locations exist", func() {
			locations, err := c.ListLocations(ctx)
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

			locations, err := c.ListLocations(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(locations).To(HaveLen(1))
			Expect(locations[0].Spec.GlobalLocationID).To(Equal("LOC-001"))
		})
	})

	Describe("ListOCloudSites", func() {
		It("returns empty list when no OCloudSites exist", func() {
			sites, err := c.ListOCloudSites(ctx)
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

			sites, err := c.ListOCloudSites(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(sites).To(HaveLen(1))
			Expect(sites[0].Spec.SiteID).To(Equal("site-001"))
		})
	})
})

var _ = Describe("Location CEL Validation", func() {
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
	})
})

var _ = Describe("OCloudSite Validation", func() {
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
	})
})

func ptrTo(s string) *string {
	return &s
}
