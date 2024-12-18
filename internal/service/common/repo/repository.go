package repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"

	models2 "github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

// CommonRepository defines the database repository for the resource server tables
type CommonRepository struct {
	Db *pgxpool.Pool
}

// GetSubscriptions retrieves all Subscription tuples or returns an empty array if no tuples are found
func (r *CommonRepository) GetSubscriptions(ctx context.Context) ([]models2.Subscription, error) {
	return utils.FindAll[models2.Subscription](ctx, r.Db)
}

// GetSubscription retrieves a specific Subscription tuple or returns ErrNotFound if not found
func (r *CommonRepository) GetSubscription(ctx context.Context, id uuid.UUID) (*models2.Subscription, error) {
	return utils.Find[models2.Subscription](ctx, r.Db, id)
}

// DeleteSubscription deletes a Subscription tuple.  The caller should ensure that it exists prior to calling this.
func (r *CommonRepository) DeleteSubscription(ctx context.Context, id uuid.UUID) (int64, error) {
	expr := psql.Quote(models2.Subscription{}.PrimaryKey()).EQ(psql.Arg(id))
	return utils.Delete[models2.Subscription](ctx, r.Db, expr)
}

// CreateSubscription create a new Subscription tuple
func (r *CommonRepository) CreateSubscription(ctx context.Context, subscription *models2.Subscription) (*models2.Subscription, error) {
	return utils.Create[models2.Subscription](ctx, r.Db, *subscription)
}

// UpdateSubscription updates a specific Subscription tuple
func (r *CommonRepository) UpdateSubscription(ctx context.Context, subscription *models2.Subscription) (*models2.Subscription, error) {
	return utils.Update[models2.Subscription](ctx, r.Db, *subscription.SubscriptionID, *subscription)
}

// GetDataSourceByName retrieves a specific DataSource tuple by name or returns ErrNotFound if not found
func (r *CommonRepository) GetDataSourceByName(ctx context.Context, name string) (*models2.DataSource, error) {
	e := psql.Quote("name").EQ(psql.Arg(name))
	records, err := utils.Search[models2.DataSource](ctx, r.Db, e)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, utils.ErrNotFound
	}
	if len(records) != 1 {
		return nil, fmt.Errorf("expected 1 record, got %d", len(records))
	}
	return &records[0], nil
}

// CreateDataSource creates a new DataSource tuple
func (r *CommonRepository) CreateDataSource(ctx context.Context, dataSource *models2.DataSource) (*models2.DataSource, error) {
	return utils.Create[models2.DataSource](ctx, r.Db, *dataSource)
}

// UpdateDataSource updates a specific DataSource tuple
func (r *CommonRepository) UpdateDataSource(ctx context.Context, dataSource *models2.DataSource) (*models2.DataSource, error) {
	return utils.Update[models2.DataSource](ctx, r.Db, *dataSource.DataSourceID, *dataSource)
}

// CreateDataChangeEvent creates a new DataSource tuple
func (r *CommonRepository) CreateDataChangeEvent(ctx context.Context, dataChangeEvent *models2.DataChangeEvent) (*models2.DataChangeEvent, error) {
	return utils.Create[models2.DataChangeEvent](ctx, r.Db, *dataChangeEvent)
}

// DeleteDataChangeEvent deletes a DataChangeEvent tuple.  The caller should ensure that it exists prior to calling this.
func (r *CommonRepository) DeleteDataChangeEvent(ctx context.Context, id uuid.UUID) (int64, error) {
	expr := psql.Quote(models2.DataChangeEvent{}.PrimaryKey()).EQ(psql.Arg(id))
	return utils.Delete[models2.DataChangeEvent](ctx, r.Db, expr)
}

// GetDataChangeEvents retrieves all DataChangeEvent tuples or returns an empty array if no tuples are found
func (r *CommonRepository) GetDataChangeEvents(ctx context.Context) ([]models2.DataChangeEvent, error) {
	return utils.FindAll[models2.DataChangeEvent](ctx, r.Db)
}
