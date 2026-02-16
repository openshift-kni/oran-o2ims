/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package repo

import (
	"context"

	"github.com/google/uuid"

	commonrepo "github.com/openshift-kni/oran-o2ims/internal/service/common/repo"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

//go:generate mockgen -source=repository_interface.go -destination=generated/mock_repo.generated.go -package=generated

// ResourcesRepositoryInterface defines the interface for the resources repository
type ResourcesRepositoryInterface interface {
	commonrepo.RepositoryInterface

	// DeploymentManager methods
	GetDeploymentManagers(ctx context.Context) ([]models.DeploymentManager, error)
	GetDeploymentManager(ctx context.Context, id uuid.UUID) (*models.DeploymentManager, error)

	// ResourceType methods
	GetResourceTypes(ctx context.Context) ([]models.ResourceType, error)
	GetResourceType(ctx context.Context, id uuid.UUID) (*models.ResourceType, error)

	// ResourcePool methods
	GetResourcePools(ctx context.Context) ([]models.ResourcePool, error)
	GetResourcePool(ctx context.Context, id uuid.UUID) (*models.ResourcePool, error)
	ResourcePoolExists(ctx context.Context, id uuid.UUID) (bool, error)

	// Resource methods
	GetResourcePoolResources(ctx context.Context, id uuid.UUID) ([]models.Resource, error)
	GetResource(ctx context.Context, id uuid.UUID) (*models.Resource, error)

	// AlarmDictionary methods
	GetAlarmDictionaries(ctx context.Context) ([]models.AlarmDictionary, error)
	GetAlarmDictionary(ctx context.Context, id uuid.UUID) (*models.AlarmDictionary, error)
	GetAlarmDefinitionsByAlarmDictionaryID(ctx context.Context, alarmDictionaryID uuid.UUID) ([]models.AlarmDefinition, error)
	GetResourceTypeAlarmDictionary(ctx context.Context, resourceTypeID uuid.UUID) ([]models.AlarmDictionary, error)

	// Location methods
	GetLocations(ctx context.Context) ([]models.Location, error)
	GetLocation(ctx context.Context, globalLocationID string) (*models.Location, error)
	GetOCloudSiteIDsForLocation(ctx context.Context, globalLocationID string) ([]uuid.UUID, error)
	GetAllOCloudSiteIDsByLocation(ctx context.Context) (map[string][]uuid.UUID, error)
	CreateOrUpdateLocation(ctx context.Context, location models.Location) (*models.Location, error)
	FindStaleLocations(ctx context.Context, dataSourceID uuid.UUID, generationID int) ([]models.Location, error)

	// OCloudSite methods
	GetOCloudSites(ctx context.Context) ([]models.OCloudSite, error)
	GetOCloudSite(ctx context.Context, id uuid.UUID) (*models.OCloudSite, error)
	GetResourcePoolIDsForSite(ctx context.Context, oCloudSiteID uuid.UUID) ([]uuid.UUID, error)
	GetAllResourcePoolIDsBySite(ctx context.Context) (map[uuid.UUID][]uuid.UUID, error)
	CreateOrUpdateOCloudSite(ctx context.Context, site models.OCloudSite) (*models.OCloudSite, error)
	FindStaleOCloudSites(ctx context.Context, dataSourceID uuid.UUID, generationID int) ([]models.OCloudSite, error)
	GetOCloudSitesNotIn(ctx context.Context, ids []any) ([]models.OCloudSite, error)
}

// Compile-time check that ResourcesRepository implements ResourcesRepositoryInterface
var _ ResourcesRepositoryInterface = (*ResourcesRepository)(nil)
