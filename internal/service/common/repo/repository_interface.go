package repo

import (
	"context"

	"github.com/google/uuid"

	models2 "github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
)

type RepositoryInterface interface {
	GetSubscriptions(context.Context) ([]models2.Subscription, error)
	GetSubscription(context.Context, uuid.UUID) (*models2.Subscription, error)
	DeleteSubscription(context.Context, uuid.UUID) (int64, error)
	CreateSubscription(context.Context, *models2.Subscription) (*models2.Subscription, error)
	UpdateSubscription(context.Context, *models2.Subscription) (*models2.Subscription, error)
	GetDataSourceByName(context.Context, string) (*models2.DataSource, error)
	CreateDataSource(context.Context, *models2.DataSource) (*models2.DataSource, error)
	UpdateDataSource(context.Context, *models2.DataSource) (*models2.DataSource, error)
	CreateDataChangeEvent(context.Context, *models2.DataChangeEvent) (*models2.DataChangeEvent, error)
	DeleteDataChangeEvent(context.Context, uuid.UUID) (int64, error)
	GetDataChangeEvents(context.Context) ([]models2.DataChangeEvent, error)
}
