/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package repo

import (
	"context"

	"github.com/google/uuid"

	commonmodels "github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
)

type RepositoryInterface interface {
	GetSubscriptions(context.Context) ([]commonmodels.Subscription, error)
	GetSubscription(context.Context, uuid.UUID) (*commonmodels.Subscription, error)
	DeleteSubscription(context.Context, uuid.UUID) (int64, error)
	CreateSubscription(context.Context, *commonmodels.Subscription) (*commonmodels.Subscription, error)
	UpdateSubscription(context.Context, *commonmodels.Subscription) (*commonmodels.Subscription, error)
	GetDataSourceByName(context.Context, string) (*commonmodels.DataSource, error)
	CreateDataSource(context.Context, *commonmodels.DataSource) (*commonmodels.DataSource, error)
	UpdateDataSource(context.Context, *commonmodels.DataSource) (*commonmodels.DataSource, error)
	CreateDataChangeEvent(context.Context, *commonmodels.DataChangeEvent) (*commonmodels.DataChangeEvent, error)
	DeleteDataChangeEvent(context.Context, uuid.UUID) (int64, error)
	GetDataChangeEvents(context.Context) ([]commonmodels.DataChangeEvent, error)
}
