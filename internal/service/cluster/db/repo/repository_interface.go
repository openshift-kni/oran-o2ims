package repo

import (
	"context"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/db/models"
	commonmodels "github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/repo"
)

//go:generate mockgen -source=repository_interface.go -destination=generated/mock_repo.generated.go -package=generated

type RepositoryInterface interface {
	repo.RepositoryInterface
	GetNodeClusterTypes(context.Context) ([]models.NodeClusterType, error)
	GetNodeClusterType(context.Context, uuid.UUID) (*models.NodeClusterType, error)
	GetNodeClusters(context.Context) ([]models.NodeCluster, error)
	GetNodeClustersNotIn(context.Context, []any) ([]models.NodeCluster, error)
	GetNodeCluster(context.Context, uuid.UUID) (*models.NodeCluster, error)
	GetNodeClusterByName(context.Context, string) (*models.NodeCluster, error)
	GetNodeClusterResources(context.Context, uuid.UUID) ([]models.ClusterResource, error)
	GetNodeClusterResourceIDs(context.Context, ...any) ([]models.ClusterResourceIDs, error)
	GetClusterResourceTypes(context.Context) ([]models.ClusterResourceType, error)
	GetClusterResourceType(context.Context, uuid.UUID) (*models.ClusterResourceType, error)
	GetClusterResources(context.Context) ([]models.ClusterResource, error)
	GetClusterResourcesNotIn(context.Context, []any) ([]models.ClusterResource, error)
	GetClusterResource(context.Context, uuid.UUID) (*models.ClusterResource, error)
	UpsertAlarmDefinitions(context.Context, []commonmodels.AlarmDefinition) ([]commonmodels.AlarmDefinition, error)
	DeleteAlarmDefinitionsNotIn(context.Context, []any, uuid.UUID) (int64, error)
	GetAlarmDefinitionsByAlarmDictionaryID(context.Context, uuid.UUID) ([]commonmodels.AlarmDefinition, error)
	FindStaleAlarmDictionaries(context.Context, uuid.UUID, int) ([]commonmodels.AlarmDictionary, error)
	GetNodeClusterTypeAlarmDictionary(context.Context, uuid.UUID) ([]commonmodels.AlarmDictionary, error)
	GetAlarmDictionaries(context.Context) ([]commonmodels.AlarmDictionary, error)
	GetAlarmDictionary(context.Context, uuid.UUID) (*commonmodels.AlarmDictionary, error)
	SetNodeClusterID(context.Context, string, uuid.UUID) (int, error)
}
