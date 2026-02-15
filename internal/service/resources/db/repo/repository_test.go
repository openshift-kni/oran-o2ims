/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package repo_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pashagolub/pgxmock/v4"

	commonrepo "github.com/openshift-kni/oran-o2ims/internal/service/common/repo"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/repo"
)

func TestRepository(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Resources Repository Suite")
}

var _ = Describe("Location Repository", func() {
	var (
		ctx        context.Context
		mock       pgxmock.PgxPoolIface
		repository *repo.ResourcesRepository
	)

	BeforeEach(func() {
		var err error
		ctx = context.Background()
		mock, err = pgxmock.NewPool()
		Expect(err).ToNot(HaveOccurred())

		// Create repository with mock
		repository = &repo.ResourcesRepository{
			CommonRepository: commonrepo.CommonRepository{Db: mock},
		}
	})

	AfterEach(func() {
		mock.Close()
	})

	Describe("GetLocations", func() {
		It("should return all locations", func() {

			rows := pgxmock.NewRows([]string{
				"global_location_id", "name", "description", "coordinate", "civic_address", "address",
			}).AddRow(
				"loc-1", "Location 1", "First location",
				map[string]interface{}{"latitude": 45.0, "longitude": -75.0},
				[]map[string]interface{}{{"country": "CA"}},
				nil,
			).AddRow(
				"loc-2", "Location 2", "Second location",
				nil,
				nil,
				stringPtr("123 Main St"),
			)
			// Expected
			mock.ExpectQuery(`SELECT .* FROM location`).WillReturnRows(rows)

			// Actual
			results, err := repository.GetLocations(ctx)

			// Verify
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(2))
			Expect(results[0].GlobalLocationID).To(Equal("loc-1"))
			Expect(results[0].Name).To(Equal("Location 1"))
			Expect(results[1].GlobalLocationID).To(Equal("loc-2"))
			Expect(results[1].Address).ToNot(BeNil())
			Expect(*results[1].Address).To(Equal("123 Main St"))

			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should return empty slice when no locations exist", func() {

			rows := pgxmock.NewRows([]string{
				"global_location_id", "name", "description", "coordinate", "civic_address", "address",
			})
			// Expected
			mock.ExpectQuery(`SELECT .* FROM location`).WillReturnRows(rows)

			// Actual
			results, err := repository.GetLocations(ctx)

			// Verify
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(BeEmpty())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("GetLocation", func() {
		It("should return location by globalLocationId", func() {

			rows := pgxmock.NewRows([]string{
				"global_location_id", "name", "description", "coordinate", "civic_address", "address",
			}).AddRow(
				"loc-1", "Location 1", "First location",
				map[string]interface{}{"latitude": 45.0, "longitude": -75.0},
				nil,
				nil,
			)
			// Expected
			mock.ExpectQuery(`SELECT .* FROM location WHERE`).
				WithArgs("loc-1").
				WillReturnRows(rows)

			// Actual
			result, err := repository.GetLocation(ctx, "loc-1")

			// Verify
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.GlobalLocationID).To(Equal("loc-1"))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should return ErrNotFound when location not found", func() {
			rows := pgxmock.NewRows([]string{
				"global_location_id", "name", "description", "coordinate", "civic_address", "address",
			})

			mock.ExpectQuery(`SELECT .* FROM location WHERE`).
				WithArgs("non-existent").
				WillReturnRows(rows)

			// Actual
			result, err := repository.GetLocation(ctx, "non-existent")

			// Verify
			Expect(err).To(Equal(svcutils.ErrNotFound))
			Expect(result).To(BeNil())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("GetOCloudSiteIDsForLocation", func() {
		It("should return site IDs for a location", func() {
			siteID1 := uuid.New()
			siteID2 := uuid.New()

			rows := pgxmock.NewRows([]string{
				"o_cloud_site_id", "global_location_id",
			}).AddRow(
				siteID1, "loc-1",
			).AddRow(
				siteID2, "loc-1",
			)

			// Expected
			mock.ExpectQuery(`SELECT .* FROM o_cloud_site WHERE`).
				WithArgs("loc-1").
				WillReturnRows(rows)

			// Actual
			ids, err := repository.GetOCloudSiteIDsForLocation(ctx, "loc-1")

			// Verify
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(HaveLen(2))
			Expect(ids).To(ContainElements(siteID1, siteID2))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should return empty slice when no sites for location", func() {
			rows := pgxmock.NewRows([]string{
				"o_cloud_site_id", "global_location_id",
			})
			// Expected
			mock.ExpectQuery(`SELECT .* FROM o_cloud_site WHERE`).
				WithArgs("loc-1").
				WillReturnRows(rows)

			// Actual
			ids, err := repository.GetOCloudSiteIDsForLocation(ctx, "loc-1")

			// Verify
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(BeEmpty())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})
})

var _ = Describe("OCloudSite Repository", func() {
	var (
		ctx        context.Context
		mock       pgxmock.PgxPoolIface
		repository *repo.ResourcesRepository
	)

	BeforeEach(func() {
		var err error
		ctx = context.Background()
		mock, err = pgxmock.NewPool()
		Expect(err).ToNot(HaveOccurred())

		// Create repository with mock
		repository = &repo.ResourcesRepository{
			CommonRepository: commonrepo.CommonRepository{Db: mock},
		}
	})

	AfterEach(func() {
		mock.Close()
	})

	Describe("GetOCloudSites", func() {
		It("should return all O-Cloud sites", func() {
			siteID1 := uuid.New()
			siteID2 := uuid.New()

			rows := pgxmock.NewRows([]string{
				"o_cloud_site_id", "global_location_id",
			}).AddRow(
				siteID1, "loc-1",
			).AddRow(
				siteID2, "loc-2",
			)

			// Expected
			mock.ExpectQuery(`SELECT .* FROM o_cloud_site`).WillReturnRows(rows)

			// Actual
			results, err := repository.GetOCloudSites(ctx)

			// Verify
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(2))
			Expect(results[0].OCloudSiteID).To(Equal(siteID1))
			Expect(results[0].GlobalLocationID).To(Equal("loc-1"))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should return empty slice when no sites exist", func() {
			rows := pgxmock.NewRows([]string{
				"o_cloud_site_id", "global_location_id",
			})

			mock.ExpectQuery(`SELECT .* FROM o_cloud_site`).WillReturnRows(rows)

			// Actual
			results, err := repository.GetOCloudSites(ctx)

			// Verify
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(BeEmpty())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("GetOCloudSite", func() {
		It("should return O-Cloud site by ID", func() {
			siteID := uuid.New()

			rows := pgxmock.NewRows([]string{
				"o_cloud_site_id", "global_location_id",
			}).AddRow(
				siteID, "loc-1",
			)
			// Expected
			mock.ExpectQuery(`SELECT .* FROM o_cloud_site WHERE`).
				WithArgs(siteID).
				WillReturnRows(rows)

			// Actual
			result, err := repository.GetOCloudSite(ctx, siteID)

			// Verify
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.OCloudSiteID).To(Equal(siteID))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should return ErrNotFound when O-Cloud site not found", func() {
			siteID := uuid.New()
			rows := pgxmock.NewRows([]string{
				"o_cloud_site_id", "global_location_id",
			})

			// Expected
			mock.ExpectQuery(`SELECT .* FROM o_cloud_site WHERE`).
				WithArgs(siteID).
				WillReturnRows(rows)

			// Actual
			result, err := repository.GetOCloudSite(ctx, siteID)

			// Verify
			Expect(err).To(Equal(svcutils.ErrNotFound))
			Expect(result).To(BeNil())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("GetResourcePoolIDsForSite", func() {
		It("should return resource pool IDs for a site", func() {
			poolID1 := uuid.New()
			poolID2 := uuid.New()
			siteID := uuid.New()
			globalLocID := uuid.New()

			rows := pgxmock.NewRows([]string{
				"resource_pool_id", "global_location_id", "name", "description", "o_cloud_id",
				"location", "o_cloud_site_id", "extensions", "data_source_id", "generation_id",
				"external_id", "created_at",
			}).AddRow(
				poolID1, globalLocID, "pool1", "desc1", uuid.New(),
				nil, &siteID, nil, uuid.New(), 1, "ext-1", nil,
			).AddRow(
				poolID2, globalLocID, "pool2", "desc2", uuid.New(),
				nil, &siteID, nil, uuid.New(), 1, "ext-2", nil,
			)

			// Expected
			mock.ExpectQuery(`SELECT .* FROM resource_pool WHERE`).
				WithArgs(siteID).
				WillReturnRows(rows)

			// Actual
			ids, err := repository.GetResourcePoolIDsForSite(ctx, siteID)

			// Verify
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(HaveLen(2))
			Expect(ids).To(ContainElements(poolID1, poolID2))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should return empty slice when no pools for site", func() {
			siteID := uuid.New()
			rows := pgxmock.NewRows([]string{
				"resource_pool_id", "global_location_id", "name", "description", "o_cloud_id",
				"location", "o_cloud_site_id", "extensions", "data_source_id", "generation_id",
				"external_id", "created_at",
			})

			// Expected
			mock.ExpectQuery(`SELECT .* FROM resource_pool WHERE`).
				WithArgs(siteID).
				WillReturnRows(rows)

			// Actual
			ids, err := repository.GetResourcePoolIDsForSite(ctx, siteID)

			// Verify
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(BeEmpty())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})
})

var _ = Describe("ResourcePool Repository", func() {
	var (
		ctx        context.Context
		mock       pgxmock.PgxPoolIface
		repository *repo.ResourcesRepository
	)

	BeforeEach(func() {
		var err error
		ctx = context.Background()
		mock, err = pgxmock.NewPool()
		Expect(err).ToNot(HaveOccurred())

		// Create repository with mock
		repository = &repo.ResourcesRepository{
			CommonRepository: commonrepo.CommonRepository{Db: mock},
		}
	})

	AfterEach(func() {
		mock.Close()
	})

	Describe("GetResourcePools", func() {
		It("should return all resource pools", func() {
			poolID1 := uuid.New()
			poolID2 := uuid.New()
			siteID := uuid.New()
			globalLocID := uuid.New()

			rows := pgxmock.NewRows([]string{
				"resource_pool_id", "global_location_id", "name", "description", "o_cloud_id",
				"location", "o_cloud_site_id", "extensions", "data_source_id", "generation_id",
				"external_id", "created_at",
			}).AddRow(
				poolID1, globalLocID, "pool1", "First pool", uuid.New(),
				nil, &siteID, nil, uuid.New(), 1, "ext-1", nil,
			).AddRow(
				poolID2, globalLocID, "pool2", "Second pool", uuid.New(),
				nil, nil, nil, uuid.New(), 1, "ext-2", nil,
			)

			// Expected
			mock.ExpectQuery(`SELECT .* FROM resource_pool`).WillReturnRows(rows)

			// Actual
			results, err := repository.GetResourcePools(ctx)

			// Verify
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(2))
			Expect(results[0].ResourcePoolID).To(Equal(poolID1))
			Expect(results[0].Name).To(Equal("pool1"))
			Expect(results[0].OCloudSiteID).ToNot(BeNil())
			Expect(results[1].OCloudSiteID).To(BeNil())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("GetResourcePool", func() {
		It("should return resource pool by ID", func() {
			poolID := uuid.New()
			siteID := uuid.New()
			globalLocID := uuid.New()

			rows := pgxmock.NewRows([]string{
				"resource_pool_id", "global_location_id", "name", "description", "o_cloud_id",
				"location", "o_cloud_site_id", "extensions", "data_source_id", "generation_id",
				"external_id", "created_at",
			}).AddRow(
				poolID, globalLocID, "pool1", "Test pool", uuid.New(),
				nil, &siteID, nil, uuid.New(), 1, "ext-1", nil,
			)

			// Expected
			mock.ExpectQuery(`SELECT .* FROM resource_pool WHERE`).
				WithArgs(poolID).
				WillReturnRows(rows)

			// Actual
			result, err := repository.GetResourcePool(ctx, poolID)

			// Verify
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.ResourcePoolID).To(Equal(poolID))
			Expect(result.Name).To(Equal("pool1"))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should return ErrNotFound when pool not found", func() {
			poolID := uuid.New()
			rows := pgxmock.NewRows([]string{
				"resource_pool_id", "global_location_id", "name", "description", "o_cloud_id",
				"location", "o_cloud_site_id", "extensions", "data_source_id", "generation_id",
				"external_id", "created_at",
			})

			mock.ExpectQuery(`SELECT .* FROM resource_pool WHERE`).
				WithArgs(poolID).
				WillReturnRows(rows)

			// Actual - using repository method
			result, err := repository.GetResourcePool(ctx, poolID)

			// Verify
			Expect(err).To(Equal(svcutils.ErrNotFound))
			Expect(result).To(BeNil())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})
})

var _ = Describe("DeploymentManager Repository", func() {
	var (
		ctx        context.Context
		mock       pgxmock.PgxPoolIface
		repository *repo.ResourcesRepository
	)

	BeforeEach(func() {
		var err error
		ctx = context.Background()
		mock, err = pgxmock.NewPool()
		Expect(err).ToNot(HaveOccurred())

		// Create repository with mock
		repository = &repo.ResourcesRepository{
			CommonRepository: commonrepo.CommonRepository{Db: mock},
		}
	})

	AfterEach(func() {
		mock.Close()
	})

	Describe("GetDeploymentManagers", func() {
		It("should return all deployment managers", func() {
			dmID1 := uuid.New()
			dmID2 := uuid.New()

			rows := pgxmock.NewRows([]string{
				"deployment_manager_id", "name", "description", "o_cloud_id",
				"url", "locations", "capabilities", "capacity_info",
				"data_source_id", "generation_id", "extensions",
			}).AddRow(
				dmID1, "dm1", "First DM", uuid.New(),
				"http://dm1.example.com", nil, nil, nil,
				uuid.New(), 1, nil,
			).AddRow(
				dmID2, "dm2", "Second DM", uuid.New(),
				"http://dm2.example.com", nil, nil, nil,
				uuid.New(), 1, nil,
			)

			// Expected
			mock.ExpectQuery(`SELECT .* FROM deployment_manager`).WillReturnRows(rows)

			// Actual
			results, err := repository.GetDeploymentManagers(ctx)

			// Verify
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(2))
			Expect(results[0].DeploymentManagerID).To(Equal(dmID1))
			Expect(results[0].Name).To(Equal("dm1"))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("GetDeploymentManager", func() {
		It("should return deployment manager by ID", func() {
			dmID := uuid.New()

			rows := pgxmock.NewRows([]string{
				"deployment_manager_id", "name", "description", "o_cloud_id",
				"url", "locations", "capabilities", "capacity_info",
				"data_source_id", "generation_id", "extensions",
			}).AddRow(
				dmID, "dm1", "Test DM", uuid.New(),
				"http://dm.example.com", nil, nil, nil,
				uuid.New(), 1, nil,
			)

			// Expected
			mock.ExpectQuery(`SELECT .* FROM deployment_manager WHERE`).
				WithArgs(dmID).
				WillReturnRows(rows)

			// Actual
			result, err := repository.GetDeploymentManager(ctx, dmID)

			// Verify
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.DeploymentManagerID).To(Equal(dmID))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should return ErrNotFound when deployment manager not found", func() {
			dmID := uuid.New()
			rows := pgxmock.NewRows([]string{
				"deployment_manager_id", "name", "description", "o_cloud_id",
				"url", "locations", "capabilities", "capacity_info",
				"data_source_id", "generation_id", "extensions",
			})

			// Expected
			mock.ExpectQuery(`SELECT .* FROM deployment_manager WHERE`).
				WithArgs(dmID).
				WillReturnRows(rows)

			// Actual
			result, err := repository.GetDeploymentManager(ctx, dmID)

			// Verify
			Expect(err).To(Equal(svcutils.ErrNotFound))
			Expect(result).To(BeNil())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})
})

// Helper function
func stringPtr(s string) *string {
	return &s
}
