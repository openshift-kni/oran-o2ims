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
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
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

	Describe("LocationExists", func() {
		It("should return true when location exists", func() {
			rows := pgxmock.NewRows([]string{"exists"}).AddRow(true)
			mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM "location" WHERE`).
				WithArgs("loc-1").
				WillReturnRows(rows)

			exists, err := repository.LocationExists(ctx, "loc-1")

			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should return false when location does not exist", func() {
			rows := pgxmock.NewRows([]string{"exists"}).AddRow(false)
			mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM "location" WHERE`).
				WithArgs("non-existent").
				WillReturnRows(rows)

			exists, err := repository.LocationExists(ctx, "non-existent")

			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeFalse())
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

			rows := pgxmock.NewRows([]string{
				"resource_pool_id", "name", "description", "o_cloud_site_id",
				"extensions", "data_source_id", "generation_id", "external_id", "created_at",
			}).AddRow(
				poolID1, "pool1", "desc1", siteID,
				nil, uuid.New(), 1, "ext-1", nil,
			).AddRow(
				poolID2, "pool2", "desc2", siteID,
				nil, uuid.New(), 1, "ext-2", nil,
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
				"resource_pool_id", "name", "description", "o_cloud_site_id",
				"extensions", "data_source_id", "generation_id", "external_id", "created_at",
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

	Describe("OCloudSiteExists", func() {
		It("should return true when site exists", func() {
			siteID := uuid.New()
			rows := pgxmock.NewRows([]string{"exists"}).AddRow(true)
			mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM "o_cloud_site" WHERE`).
				WithArgs(siteID).
				WillReturnRows(rows)

			exists, err := repository.OCloudSiteExists(ctx, siteID)

			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should return false when site does not exist", func() {
			siteID := uuid.New()
			rows := pgxmock.NewRows([]string{"exists"}).AddRow(false)
			mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM "o_cloud_site" WHERE`).
				WithArgs(siteID).
				WillReturnRows(rows)

			exists, err := repository.OCloudSiteExists(ctx, siteID)

			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeFalse())
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
			siteID2 := uuid.New()

			rows := pgxmock.NewRows([]string{
				"resource_pool_id", "name", "description", "o_cloud_site_id",
				"extensions", "data_source_id", "generation_id", "external_id", "created_at",
			}).AddRow(
				poolID1, "pool1", "First pool", siteID,
				nil, uuid.New(), 1, "ext-1", nil,
			).AddRow(
				poolID2, "pool2", "Second pool", siteID2,
				nil, uuid.New(), 1, "ext-2", nil,
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
			Expect(results[0].OCloudSiteID).To(Equal(siteID))
			Expect(results[1].OCloudSiteID).To(Equal(siteID2))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("GetResourcePool", func() {
		It("should return resource pool by ID", func() {
			poolID := uuid.New()
			siteID := uuid.New()

			rows := pgxmock.NewRows([]string{
				"resource_pool_id", "name", "description", "o_cloud_site_id",
				"extensions", "data_source_id", "generation_id", "external_id", "created_at",
			}).AddRow(
				poolID, "pool1", "Test pool", siteID,
				nil, uuid.New(), 1, "ext-1", nil,
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
				"resource_pool_id", "name", "description", "o_cloud_site_id",
				"extensions", "data_source_id", "generation_id", "external_id", "created_at",
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

	Describe("ResourcePoolExists", func() {
		It("should return true when resource pool exists", func() {
			poolID := uuid.New()
			rows := pgxmock.NewRows([]string{"exists"}).AddRow(true)
			mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM "resource_pool" WHERE`).
				WithArgs(poolID).
				WillReturnRows(rows)

			exists, err := repository.ResourcePoolExists(ctx, poolID)

			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should return false when resource pool does not exist", func() {
			poolID := uuid.New()
			rows := pgxmock.NewRows([]string{"exists"}).AddRow(false)
			mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM "resource_pool" WHERE`).
				WithArgs(poolID).
				WillReturnRows(rows)

			exists, err := repository.ResourcePoolExists(ctx, poolID)

			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeFalse())
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

	Describe("GetDeploymentManagersNotIn", func() {
		It("should return deployment managers not in the given list", func() {
			dmID1 := uuid.New()
			dmID2 := uuid.New()
			excluded := uuid.New()

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

			mock.ExpectQuery(`SELECT .* FROM deployment_manager WHERE .* NOT IN`).
				WithArgs(excluded).
				WillReturnRows(rows)

			result, err := repository.GetDeploymentManagersNotIn(ctx, []any{excluded})

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(2))
			Expect(result[0].DeploymentManagerID).To(Equal(dmID1))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should return all deployment managers when keys list is empty", func() {
			dmID := uuid.New()
			rows := pgxmock.NewRows([]string{
				"deployment_manager_id", "name", "description", "o_cloud_id",
				"url", "locations", "capabilities", "capacity_info",
				"data_source_id", "generation_id", "extensions",
			}).AddRow(
				dmID, "dm1", "DM", uuid.New(),
				"http://dm.example.com", nil, nil, nil,
				uuid.New(), 1, nil,
			)

			mock.ExpectQuery(`SELECT .* FROM deployment_manager`).WillReturnRows(rows)

			result, err := repository.GetDeploymentManagersNotIn(ctx, []any{})

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})
})

var _ = Describe("Batch Query Functions", func() {
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

		repository = &repo.ResourcesRepository{
			CommonRepository: commonrepo.CommonRepository{Db: mock},
		}
	})

	AfterEach(func() {
		mock.Close()
	})

	Describe("GetAllOCloudSiteIDsByLocation", func() {
		It("should group site IDs by location", func() {
			siteID1 := uuid.New()
			siteID2 := uuid.New()
			siteID3 := uuid.New()

			rows := pgxmock.NewRows([]string{
				"o_cloud_site_id", "global_location_id", "name", "description", "extensions",
			}).AddRow(
				siteID1, "loc-1", "Site 1", "desc", nil,
			).AddRow(
				siteID2, "loc-1", "Site 2", "desc", nil, // Same location as site 1
			).AddRow(
				siteID3, "loc-2", "Site 3", "desc", nil, // Different location
			)

			mock.ExpectQuery(`SELECT .* FROM o_cloud_site`).WillReturnRows(rows)

			result, err := repository.GetAllOCloudSiteIDsByLocation(ctx)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(2))
			Expect(result["loc-1"]).To(HaveLen(2))
			Expect(result["loc-1"]).To(ContainElements(siteID1, siteID2))
			Expect(result["loc-2"]).To(HaveLen(1))
			Expect(result["loc-2"]).To(ContainElement(siteID3))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should return empty map when no sites exist", func() {
			rows := pgxmock.NewRows([]string{
				"o_cloud_site_id", "global_location_id", "name", "description", "extensions",
			})

			mock.ExpectQuery(`SELECT .* FROM o_cloud_site`).WillReturnRows(rows)

			result, err := repository.GetAllOCloudSiteIDsByLocation(ctx)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeEmpty())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should handle single site per location", func() {
			siteID1 := uuid.New()
			siteID2 := uuid.New()

			rows := pgxmock.NewRows([]string{
				"o_cloud_site_id", "global_location_id", "name", "description", "extensions",
			}).AddRow(
				siteID1, "loc-1", "Site 1", "desc", nil,
			).AddRow(
				siteID2, "loc-2", "Site 2", "desc", nil,
			)

			mock.ExpectQuery(`SELECT .* FROM o_cloud_site`).WillReturnRows(rows)

			result, err := repository.GetAllOCloudSiteIDsByLocation(ctx)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(2))
			Expect(result["loc-1"]).To(HaveLen(1))
			Expect(result["loc-1"]).To(ContainElement(siteID1))
			Expect(result["loc-2"]).To(HaveLen(1))
			Expect(result["loc-2"]).To(ContainElement(siteID2))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("GetAllResourcePoolIDsBySite", func() {
		It("should group pool IDs by site", func() {
			poolID1 := uuid.New()
			poolID2 := uuid.New()
			poolID3 := uuid.New()
			siteID1 := uuid.New()
			siteID2 := uuid.New()

			rows := pgxmock.NewRows([]string{
				"resource_pool_id", "name", "description", "o_cloud_site_id",
				"extensions", "data_source_id", "generation_id", "external_id", "created_at",
			}).AddRow(
				poolID1, "pool1", "desc1", siteID1,
				nil, uuid.New(), 1, "ext-1", nil,
			).AddRow(
				poolID2, "pool2", "desc2", siteID1, // Same site as pool 1
				nil, uuid.New(), 1, "ext-2", nil,
			).AddRow(
				poolID3, "pool3", "desc3", siteID2, // Different site
				nil, uuid.New(), 1, "ext-3", nil,
			)

			mock.ExpectQuery(`SELECT .* FROM resource_pool`).WillReturnRows(rows)

			result, err := repository.GetAllResourcePoolIDsBySite(ctx)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(2))
			Expect(result[siteID1]).To(HaveLen(2))
			Expect(result[siteID1]).To(ContainElements(poolID1, poolID2))
			Expect(result[siteID2]).To(HaveLen(1))
			Expect(result[siteID2]).To(ContainElement(poolID3))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should return empty map when no pools exist", func() {
			rows := pgxmock.NewRows([]string{
				"resource_pool_id", "name", "description", "o_cloud_site_id",
				"extensions", "data_source_id", "generation_id", "external_id", "created_at",
			})

			mock.ExpectQuery(`SELECT .* FROM resource_pool`).WillReturnRows(rows)

			result, err := repository.GetAllResourcePoolIDsBySite(ctx)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeEmpty())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("GetResourcePoolsNotIn", func() {
		It("should return pools not in the given list", func() {
			poolID1 := uuid.New()
			poolID2 := uuid.New()
			excludedPoolID := uuid.New()
			siteID := uuid.New()

			rows := pgxmock.NewRows([]string{
				"resource_pool_id", "name", "description", "o_cloud_site_id",
				"extensions", "data_source_id", "generation_id", "external_id", "created_at",
			}).AddRow(
				poolID1, "pool1", "desc1", siteID,
				nil, uuid.New(), 1, "ext-1", nil,
			).AddRow(
				poolID2, "pool2", "desc2", siteID,
				nil, uuid.New(), 1, "ext-2", nil,
			)

			// Query should include NOT IN clause
			mock.ExpectQuery(`SELECT .* FROM resource_pool WHERE .* NOT IN`).
				WithArgs(excludedPoolID).
				WillReturnRows(rows)

			result, err := repository.GetResourcePoolsNotIn(ctx, []any{excludedPoolID})

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(2))
			Expect(result[0].ResourcePoolID).To(Equal(poolID1))
			Expect(result[1].ResourcePoolID).To(Equal(poolID2))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should return all pools when keys list is empty", func() {
			poolID1 := uuid.New()
			poolID2 := uuid.New()
			siteID := uuid.New()

			rows := pgxmock.NewRows([]string{
				"resource_pool_id", "name", "description", "o_cloud_site_id",
				"extensions", "data_source_id", "generation_id", "external_id", "created_at",
			}).AddRow(
				poolID1, "pool1", "desc1", siteID,
				nil, uuid.New(), 1, "ext-1", nil,
			).AddRow(
				poolID2, "pool2", "desc2", siteID,
				nil, uuid.New(), 1, "ext-2", nil,
			)

			// Query should NOT include NOT IN clause when keys is empty
			mock.ExpectQuery(`SELECT .* FROM resource_pool`).WillReturnRows(rows)

			result, err := repository.GetResourcePoolsNotIn(ctx, []any{})

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(2))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should return empty when all pools are excluded", func() {
			excludedPoolID1 := uuid.New()
			excludedPoolID2 := uuid.New()

			rows := pgxmock.NewRows([]string{
				"resource_pool_id", "name", "description", "o_cloud_site_id",
				"extensions", "data_source_id", "generation_id", "external_id", "created_at",
			})

			mock.ExpectQuery(`SELECT .* FROM resource_pool WHERE .* NOT IN`).
				WithArgs(excludedPoolID1, excludedPoolID2).
				WillReturnRows(rows)

			result, err := repository.GetResourcePoolsNotIn(ctx, []any{excludedPoolID1, excludedPoolID2})

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeEmpty())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("GetOCloudSitesNotIn", func() {
		It("should return sites not in the given list", func() {
			siteID1 := uuid.New()
			siteID2 := uuid.New()
			excludedSiteID := uuid.New()

			rows := pgxmock.NewRows([]string{
				"o_cloud_site_id", "global_location_id", "name", "description", "extensions",
			}).AddRow(
				siteID1, "loc-1", "Site 1", "desc", nil,
			).AddRow(
				siteID2, "loc-2", "Site 2", "desc", nil,
			)

			mock.ExpectQuery(`SELECT .* FROM o_cloud_site WHERE .* NOT IN`).
				WithArgs(excludedSiteID).
				WillReturnRows(rows)

			result, err := repository.GetOCloudSitesNotIn(ctx, []any{excludedSiteID})

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(2))
			Expect(result[0].OCloudSiteID).To(Equal(siteID1))
			Expect(result[1].OCloudSiteID).To(Equal(siteID2))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should return all sites when keys list is empty", func() {
			siteID1 := uuid.New()
			siteID2 := uuid.New()

			rows := pgxmock.NewRows([]string{
				"o_cloud_site_id", "global_location_id", "name", "description", "extensions",
			}).AddRow(
				siteID1, "loc-1", "Site 1", "desc", nil,
			).AddRow(
				siteID2, "loc-2", "Site 2", "desc", nil,
			)

			mock.ExpectQuery(`SELECT .* FROM o_cloud_site`).WillReturnRows(rows)

			result, err := repository.GetOCloudSitesNotIn(ctx, []any{})

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(2))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should return empty when all sites are excluded", func() {
			excludedSiteID1 := uuid.New()
			excludedSiteID2 := uuid.New()

			rows := pgxmock.NewRows([]string{
				"o_cloud_site_id", "global_location_id", "name", "description", "extensions",
			})

			mock.ExpectQuery(`SELECT .* FROM o_cloud_site WHERE .* NOT IN`).
				WithArgs(excludedSiteID1, excludedSiteID2).
				WillReturnRows(rows)

			result, err := repository.GetOCloudSitesNotIn(ctx, []any{excludedSiteID1, excludedSiteID2})

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeEmpty())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})
})

var _ = Describe("Extended ResourcesRepository coverage", func() {
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
		repository = &repo.ResourcesRepository{
			CommonRepository: commonrepo.CommonRepository{Db: mock},
		}
	})

	AfterEach(func() {
		mock.Close()
	})

	Describe("GetResourceTypes", func() {
		It("should return all resource types", func() {
			rtID := uuid.New()
			rows := pgxmock.NewRows([]string{
				"resource_type_id", "name", "description", "vendor", "model", "version",
				"resource_kind", "resource_class", "extensions", "data_source_id", "generation_id", "created_at",
			}).AddRow(
				rtID, "rt1", "desc", "ven", "mod", "1.0",
				string(models.ResourceKindPhysical), string(models.ResourceClassCompute),
				nil, uuid.New(), 1, nil,
			)

			mock.ExpectQuery(`SELECT .* FROM resource_type`).WillReturnRows(rows)

			result, err := repository.GetResourceTypes(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(result[0].ResourceTypeID).To(Equal(rtID))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("GetResourceType", func() {
		It("should return a resource type by ID", func() {
			rtID := uuid.New()
			rows := pgxmock.NewRows([]string{
				"resource_type_id", "name", "description", "vendor", "model", "version",
				"resource_kind", "resource_class", "extensions", "data_source_id", "generation_id", "created_at",
			}).AddRow(
				rtID, "rt1", "desc", "ven", "mod", "1.0",
				string(models.ResourceKindPhysical), string(models.ResourceClassCompute),
				nil, uuid.New(), 1, nil,
			)

			mock.ExpectQuery(`SELECT .* FROM resource_type WHERE`).
				WithArgs(rtID).
				WillReturnRows(rows)

			result, err := repository.GetResourceType(ctx, rtID)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.ResourceTypeID).To(Equal(rtID))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("GetResourcePoolResources", func() {
		It("should return resources for a pool", func() {
			poolID := uuid.New()
			resID := uuid.New()
			rtID := uuid.New()
			dsID := uuid.New()
			rows := pgxmock.NewRows([]string{
				"resource_id", "description", "resource_type_id", "global_asset_id", "resource_pool_id",
				"extensions", "groups", "tags", "data_source_id", "generation_id", "external_id", "created_at",
			}).AddRow(
				resID, "r1", rtID, nil, poolID,
				nil, nil, nil, dsID, 1, "ext", nil,
			)

			mock.ExpectQuery(`SELECT .* FROM resource WHERE`).
				WithArgs(poolID).
				WillReturnRows(rows)

			result, err := repository.GetResourcePoolResources(ctx, poolID)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(result[0].ResourceID).To(Equal(resID))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("GetResource", func() {
		It("should return a resource by ID", func() {
			resID := uuid.New()
			poolID := uuid.New()
			rtID := uuid.New()
			dsID := uuid.New()
			rows := pgxmock.NewRows([]string{
				"resource_id", "description", "resource_type_id", "global_asset_id", "resource_pool_id",
				"extensions", "groups", "tags", "data_source_id", "generation_id", "external_id", "created_at",
			}).AddRow(
				resID, "r1", rtID, nil, poolID,
				nil, nil, nil, dsID, 1, "ext", nil,
			)

			mock.ExpectQuery(`SELECT .* FROM resource WHERE`).
				WithArgs(resID).
				WillReturnRows(rows)

			result, err := repository.GetResource(ctx, resID)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.ResourceID).To(Equal(resID))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("GetResourcesNotIn", func() {
		It("should return resources not in the given list", func() {
			resID1 := uuid.New()
			resID2 := uuid.New()
			excludedResID := uuid.New()

			rows := pgxmock.NewRows([]string{
				"resource_id", "description", "resource_type_id", "global_asset_id", "resource_pool_id",
				"extensions", "groups", "tags", "data_source_id", "generation_id", "external_id", "created_at",
			}).AddRow(
				resID1, "r1", uuid.New(), nil, uuid.New(),
				nil, nil, nil, uuid.New(), 1, "ext-1", nil,
			).AddRow(
				resID2, "r2", uuid.New(), nil, uuid.New(),
				nil, nil, nil, uuid.New(), 1, "ext-2", nil,
			)

			mock.ExpectQuery(`SELECT .* FROM resource WHERE .*resource_id.* NOT IN`).
				WithArgs(excludedResID).
				WillReturnRows(rows)

			result, err := repository.GetResourcesNotIn(ctx, []any{excludedResID})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(2))
			Expect(result[0].ResourceID).To(Equal(resID1))
			Expect(result[1].ResourceID).To(Equal(resID2))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should return all resources when keys list is empty", func() {
			resID := uuid.New()

			rows := pgxmock.NewRows([]string{
				"resource_id", "description", "resource_type_id", "global_asset_id", "resource_pool_id",
				"extensions", "groups", "tags", "data_source_id", "generation_id", "external_id", "created_at",
			}).AddRow(
				resID, "r1", uuid.New(), nil, uuid.New(),
				nil, nil, nil, uuid.New(), 1, "ext-1", nil,
			)

			mock.ExpectQuery(`SELECT .* FROM resource`).WillReturnRows(rows)

			result, err := repository.GetResourcesNotIn(ctx, []any{})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("GetAlarmDictionaries", func() {
		It("should return all alarm dictionaries", func() {
			adID := uuid.New()
			rtID := uuid.New()
			rows := pgxmock.NewRows([]string{
				"alarm_dictionary_id", "alarm_dictionary_version", "alarm_dictionary_schema_version",
				"entity_type", "vendor", "management_interface_id", "pk_notification_field",
				"resource_type_id", "created_at",
			}).AddRow(
				adID, "v1", "s1", "e", "ven",
				[]string{"m"}, []string{"pk"}, rtID, nil,
			)

			mock.ExpectQuery(`SELECT .* FROM alarm_dictionary`).WillReturnRows(rows)

			result, err := repository.GetAlarmDictionaries(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(result[0].AlarmDictionaryID).To(Equal(adID))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("GetAlarmDictionary", func() {
		It("should return an alarm dictionary by ID", func() {
			adID := uuid.New()
			rtID := uuid.New()
			rows := pgxmock.NewRows([]string{
				"alarm_dictionary_id", "alarm_dictionary_version", "alarm_dictionary_schema_version",
				"entity_type", "vendor", "management_interface_id", "pk_notification_field",
				"resource_type_id", "created_at",
			}).AddRow(
				adID, "v1", "s1", "e", "ven",
				[]string{"m"}, []string{"pk"}, rtID, nil,
			)

			mock.ExpectQuery(`SELECT .* FROM alarm_dictionary WHERE`).
				WithArgs(adID).
				WillReturnRows(rows)

			result, err := repository.GetAlarmDictionary(ctx, adID)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.AlarmDictionaryID).To(Equal(adID))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("GetResourceTypeAlarmDictionary", func() {
		It("should return alarm dictionaries for a resource type", func() {
			adID := uuid.New()
			rtID := uuid.New()
			rows := pgxmock.NewRows([]string{
				"alarm_dictionary_id", "alarm_dictionary_version", "alarm_dictionary_schema_version",
				"entity_type", "vendor", "management_interface_id", "pk_notification_field",
				"resource_type_id", "created_at",
			}).AddRow(
				adID, "v1", "s1", "e", "ven",
				[]string{"m"}, []string{"pk"}, rtID, nil,
			)

			mock.ExpectQuery(`SELECT .* FROM alarm_dictionary WHERE`).
				WithArgs(rtID).
				WillReturnRows(rows)

			result, err := repository.GetResourceTypeAlarmDictionary(ctx, rtID)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(result[0].ResourceTypeID).To(Equal(rtID))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("UpsertAlarmDefinitions", func() {
		It("should return empty slice when no records", func() {
			out, err := repository.UpsertAlarmDefinitions(ctx, mock, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(BeEmpty())
			out, err = repository.UpsertAlarmDefinitions(ctx, mock, []models.AlarmDefinition{})
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(BeEmpty())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("should upsert alarm definitions", func() {
			dictID := uuid.New()
			def := models.AlarmDefinition{
				AlarmName: "a", AlarmLastChange: "lc", AlarmChangeType: "ct",
				AlarmDescription: "d", ProposedRepairActions: "p", ClearingType: "clr",
				ManagementInterfaceID: []string{"m"}, PKNotificationField: []string{"pk"},
				Severity: "s", AlarmDictionaryID: &dictID,
			}
			defID := uuid.New()
			ret := pgxmock.NewRows([]string{"alarm_definition_id"}).AddRow(defID)

			mock.ExpectQuery(`INSERT INTO alarm_definition\(`).
				WithArgs("a", "lc", "ct", "d", "p", "clr", []string{"m"}, []string{"pk"}, pgxmock.AnyArg(), "s", pgxmock.AnyArg()).
				WillReturnRows(ret)

			out, err := repository.UpsertAlarmDefinitions(ctx, mock, []models.AlarmDefinition{def})
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(HaveLen(1))
			Expect(out[0].AlarmDefinitionID).To(Equal(defID))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("UpsertAlarmDictionary", func() {
		It("should upsert an alarm dictionary", func() {
			rtID := uuid.New()
			rec := models.AlarmDictionary{
				AlarmDictionaryVersion: "v", AlarmDictionarySchemaVersion: "s",
				EntityType: "e", Vendor: "ven",
				ManagementInterfaceID: []string{"m"}, PKNotificationField: []string{"pk"},
				ResourceTypeID: rtID,
			}
			adID := uuid.New()
			ret := pgxmock.NewRows([]string{"alarm_dictionary_id"}).AddRow(adID)

			mock.ExpectQuery(`INSERT INTO alarm_dictionary\(`).
				WithArgs("v", "s", "e", "ven", []string{"m"}, []string{"pk"}, rtID).
				WillReturnRows(ret)

			out, err := repository.UpsertAlarmDictionary(ctx, mock, rec)
			Expect(err).ToNot(HaveOccurred())
			Expect(out.AlarmDictionaryID).To(Equal(adID))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("CreateResourcePool", func() {
		It("should insert and return a resource pool", func() {
			poolID := uuid.New()
			siteID := uuid.New()
			dsID := uuid.New()
			pool := &models.ResourcePool{
				ResourcePoolID: poolID,
				Name:           "n", Description: "d",
				OCloudSiteID: siteID, DataSourceID: dsID,
				GenerationID: 1, ExternalID: "ext",
			}
			ret := pgxmock.NewRows([]string{
				"resource_pool_id", "name", "description", "o_cloud_site_id",
				"extensions", "data_source_id", "generation_id", "external_id", "created_at",
			}).AddRow(poolID, "n", "d", siteID, nil, dsID, 1, "ext", nil)

			mock.ExpectQuery(`INSERT INTO resource_pool\(`).
				WithArgs(dsID, "d", pgxmock.AnyArg(), "ext", 1, "n", siteID, poolID).
				WillReturnRows(ret)

			out, err := repository.CreateResourcePool(ctx, pool)
			Expect(err).ToNot(HaveOccurred())
			Expect(out.ResourcePoolID).To(Equal(poolID))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("UpdateResourcePool", func() {
		It("should update and return a resource pool", func() {
			poolID := uuid.New()
			siteID := uuid.New()
			dsID := uuid.New()
			pool := &models.ResourcePool{
				ResourcePoolID: poolID,
				Name:           "n2", Description: "d2",
				OCloudSiteID: siteID, DataSourceID: dsID,
				GenerationID: 2, ExternalID: "ext2",
			}
			ret := pgxmock.NewRows([]string{
				"resource_pool_id", "name", "description", "o_cloud_site_id",
				"extensions", "data_source_id", "generation_id", "external_id", "created_at",
			}).AddRow(poolID, "n2", "d2", siteID, nil, dsID, 2, "ext2", nil)

			mock.ExpectQuery(`UPDATE resource_pool SET`).
				WithArgs(dsID, "d2", pgxmock.AnyArg(), "ext2", 2, "n2", siteID, poolID, poolID).
				WillReturnRows(ret)

			out, err := repository.UpdateResourcePool(ctx, pool)
			Expect(err).ToNot(HaveOccurred())
			Expect(out.Name).To(Equal("n2"))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("CreateResource", func() {
		It("should insert and return a resource", func() {
			resID := uuid.New()
			poolID := uuid.New()
			rtID := uuid.New()
			dsID := uuid.New()
			res := &models.Resource{
				ResourceID: resID, Description: "d",
				ResourceTypeID: rtID, ResourcePoolID: poolID,
				DataSourceID: dsID, GenerationID: 1, ExternalID: "e",
			}
			ret := pgxmock.NewRows([]string{
				"resource_id", "description", "resource_type_id", "global_asset_id", "resource_pool_id",
				"extensions", "groups", "tags", "data_source_id", "generation_id", "external_id", "created_at",
			}).AddRow(resID, "d", rtID, nil, poolID, nil, nil, nil, dsID, 1, "e", nil)

			mock.ExpectQuery(`INSERT INTO resource\(`).
				WithArgs(dsID, "d", pgxmock.AnyArg(), "e", 1, resID, poolID, rtID).
				WillReturnRows(ret)

			out, err := repository.CreateResource(ctx, res)
			Expect(err).ToNot(HaveOccurred())
			Expect(out.ResourceID).To(Equal(resID))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("UpdateResource", func() {
		It("should update and return a resource", func() {
			resID := uuid.New()
			poolID := uuid.New()
			rtID := uuid.New()
			dsID := uuid.New()
			res := &models.Resource{
				ResourceID: resID, Description: "d2",
				ResourceTypeID: rtID, ResourcePoolID: poolID,
				DataSourceID: dsID, GenerationID: 2, ExternalID: "e2",
			}
			ret := pgxmock.NewRows([]string{
				"resource_id", "description", "resource_type_id", "global_asset_id", "resource_pool_id",
				"extensions", "groups", "tags", "data_source_id", "generation_id", "external_id", "created_at",
			}).AddRow(resID, "d2", rtID, nil, poolID, nil, nil, nil, dsID, 2, "e2", nil)

			mock.ExpectQuery(`UPDATE resource SET`).
				WithArgs(dsID, "d2", pgxmock.AnyArg(), "e2", 2, resID, poolID, rtID, resID).
				WillReturnRows(ret)

			out, err := repository.UpdateResource(ctx, res)
			Expect(err).ToNot(HaveOccurred())
			Expect(out.Description).To(Equal("d2"))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})
})

// Helper function
func stringPtr(s string) *string {
	return &s
}
