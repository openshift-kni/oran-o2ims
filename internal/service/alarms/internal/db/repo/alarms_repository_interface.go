package repo

import (
	"context"

	"github.com/google/uuid"
	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
)

//go:generate mockgen -source=alarms_repository_interface.go -destination=generated/mock_repo.generated.go -package=generated

type AlarmRepositoryInterface interface {
	GetAlarmEventRecords(ctx context.Context) ([]models.AlarmEventRecord, error)
	PatchAlarmEventRecordACK(ctx context.Context, id uuid.UUID, record *models.AlarmEventRecord) (*models.AlarmEventRecord, error)
	GetAlarmEventRecord(ctx context.Context, id uuid.UUID) (*models.AlarmEventRecord, error)
	CreateServiceConfiguration(ctx context.Context, defaultRetentionPeriod int) (*models.ServiceConfiguration, error)
	GetServiceConfigurations(ctx context.Context) ([]models.ServiceConfiguration, error)
	UpdateServiceConfiguration(ctx context.Context, id uuid.UUID, record *models.ServiceConfiguration) (*models.ServiceConfiguration, error)
	GetAlarmSubscriptions(ctx context.Context) ([]models.AlarmSubscription, error)
	DeleteAlarmSubscription(ctx context.Context, id uuid.UUID) (int64, error)
	CreateAlarmSubscription(ctx context.Context, record models.AlarmSubscription) (*models.AlarmSubscription, error)
	GetAlarmSubscription(ctx context.Context, id uuid.UUID) (*models.AlarmSubscription, error)
	UpsertAlarmEventRecord(ctx context.Context, records []models.AlarmEventRecord) error
	ResolveNotificationIfNotInCurrent(ctx context.Context, am *api.AlertmanagerNotification) error
	GetAlarmsForSubscription(ctx context.Context, subscription models.AlarmSubscription) ([]models.AlarmEventRecord, error)
	UpdateSubscriptionEventCursor(ctx context.Context, subscription models.AlarmSubscription) error
	GetMaxAlarmSeq(ctx context.Context) (int64, error)
}
