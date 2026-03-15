/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Hierarchy Index Functions", func() {

	Describe("registerIndex", func() {
		var (
			ctx     context.Context
			scheme  *runtime.Scheme
			indexer *fakeFieldIndexer
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme = runtime.NewScheme()
			Expect(inventoryv1alpha1.AddToScheme(scheme)).To(Succeed())
			indexer = &fakeFieldIndexer{
				registeredIndexes: make(map[string]client.IndexerFunc),
			}
		})

		Context("with OCloudSite type", func() {
			It("should register an index that extracts spec.globalLocationName", func() {
				err := registerIndex(ctx, indexer, OCloudSiteGlobalLocationNameIndex,
					func(s *inventoryv1alpha1.OCloudSite) string { return s.Spec.GlobalLocationName },
				)
				Expect(err).ToNot(HaveOccurred())
				Expect(indexer.registeredIndexes).To(HaveKey(OCloudSiteGlobalLocationNameIndex))

				indexerFunc := indexer.registeredIndexes[OCloudSiteGlobalLocationNameIndex]

				site := &inventoryv1alpha1.OCloudSite{
					ObjectMeta: metav1.ObjectMeta{Name: "test-site"},
					Spec: inventoryv1alpha1.OCloudSiteSpec{
						GlobalLocationName: "loc-1",
					},
				}
				result := indexerFunc(site)
				Expect(result).To(Equal([]string{"loc-1"}))
			})

			It("should return nil for empty globalLocationName", func() {
				err := registerIndex(ctx, indexer, OCloudSiteGlobalLocationNameIndex,
					func(s *inventoryv1alpha1.OCloudSite) string { return s.Spec.GlobalLocationName },
				)
				Expect(err).ToNot(HaveOccurred())

				indexerFunc := indexer.registeredIndexes[OCloudSiteGlobalLocationNameIndex]

				site := &inventoryv1alpha1.OCloudSite{
					ObjectMeta: metav1.ObjectMeta{Name: "test-site"},
					Spec: inventoryv1alpha1.OCloudSiteSpec{
						GlobalLocationName: "",
					},
				}
				result := indexerFunc(site)
				Expect(result).To(BeNil())
			})
		})

		Context("with ResourcePool type", func() {
			It("should register an index that extracts spec.oCloudSiteName", func() {
				err := registerIndex(ctx, indexer, ResourcePoolOCloudSiteNameIndex,
					func(p *inventoryv1alpha1.ResourcePool) string { return p.Spec.OCloudSiteName },
				)
				Expect(err).ToNot(HaveOccurred())
				Expect(indexer.registeredIndexes).To(HaveKey(ResourcePoolOCloudSiteNameIndex))

				indexerFunc := indexer.registeredIndexes[ResourcePoolOCloudSiteNameIndex]

				pool := &inventoryv1alpha1.ResourcePool{
					ObjectMeta: metav1.ObjectMeta{Name: "test-pool"},
					Spec: inventoryv1alpha1.ResourcePoolSpec{
						OCloudSiteName: "site-1",
					},
				}
				result := indexerFunc(pool)
				Expect(result).To(Equal([]string{"site-1"}))
			})

			It("should return nil for empty oCloudSiteName", func() {
				err := registerIndex(ctx, indexer, ResourcePoolOCloudSiteNameIndex,
					func(p *inventoryv1alpha1.ResourcePool) string { return p.Spec.OCloudSiteName },
				)
				Expect(err).ToNot(HaveOccurred())

				indexerFunc := indexer.registeredIndexes[ResourcePoolOCloudSiteNameIndex]

				pool := &inventoryv1alpha1.ResourcePool{
					ObjectMeta: metav1.ObjectMeta{Name: "test-pool"},
					Spec: inventoryv1alpha1.ResourcePoolSpec{
						OCloudSiteName: "",
					},
				}
				result := indexerFunc(pool)
				Expect(result).To(BeNil())
			})
		})
	})

	Describe("Integration with fake client", func() {
		var (
			ctx        context.Context
			scheme     *runtime.Scheme
			fakeClient client.Client
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme = runtime.NewScheme()
			Expect(inventoryv1alpha1.AddToScheme(scheme)).To(Succeed())
		})

		It("should support indexed queries via fake client", func() {
			// Create test objects
			site1 := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{Name: "site-1", Namespace: "default"},
				Spec:       inventoryv1alpha1.OCloudSiteSpec{GlobalLocationName: "loc-a"},
			}
			site2 := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{Name: "site-2", Namespace: "default"},
				Spec:       inventoryv1alpha1.OCloudSiteSpec{GlobalLocationName: "loc-a"},
			}
			site3 := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{Name: "site-3", Namespace: "default"},
				Spec:       inventoryv1alpha1.OCloudSiteSpec{GlobalLocationName: "loc-b"},
			}

			// Build fake client with index
			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(site1, site2, site3).
				WithIndex(&inventoryv1alpha1.OCloudSite{}, OCloudSiteGlobalLocationNameIndex,
					func(obj client.Object) []string {
						site := obj.(*inventoryv1alpha1.OCloudSite)
						if site.Spec.GlobalLocationName != "" {
							return []string{site.Spec.GlobalLocationName}
						}
						return nil
					}).
				Build()

			// Query by index
			var siteList inventoryv1alpha1.OCloudSiteList
			err := fakeClient.List(ctx, &siteList, client.MatchingFields{
				OCloudSiteGlobalLocationNameIndex: "loc-a",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(siteList.Items).To(HaveLen(2))

			// Verify the correct sites are returned
			names := []string{siteList.Items[0].Name, siteList.Items[1].Name}
			Expect(names).To(ContainElements("site-1", "site-2"))
		})

		It("should return empty list when no matches found", func() {
			site1 := &inventoryv1alpha1.OCloudSite{
				ObjectMeta: metav1.ObjectMeta{Name: "site-1", Namespace: "default"},
				Spec:       inventoryv1alpha1.OCloudSiteSpec{GlobalLocationName: "loc-a"},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(site1).
				WithIndex(&inventoryv1alpha1.OCloudSite{}, OCloudSiteGlobalLocationNameIndex,
					func(obj client.Object) []string {
						site := obj.(*inventoryv1alpha1.OCloudSite)
						if site.Spec.GlobalLocationName != "" {
							return []string{site.Spec.GlobalLocationName}
						}
						return nil
					}).
				Build()

			var siteList inventoryv1alpha1.OCloudSiteList
			err := fakeClient.List(ctx, &siteList, client.MatchingFields{
				OCloudSiteGlobalLocationNameIndex: "non-existent",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(siteList.Items).To(BeEmpty())
		})
	})
})

// fakeFieldIndexer is a test double for client.FieldIndexer that captures registered indexes
type fakeFieldIndexer struct {
	registeredIndexes map[string]client.IndexerFunc
}

func (f *fakeFieldIndexer) IndexField(ctx context.Context, obj client.Object, field string, extractValue client.IndexerFunc) error {
	f.registeredIndexes[field] = extractValue
	return nil
}
