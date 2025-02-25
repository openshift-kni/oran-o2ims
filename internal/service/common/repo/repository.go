package repo

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"

	commonmodels "github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

// CommonRepository defines the database repository for the resource server tables
type CommonRepository struct {
	Db *pgxpool.Pool
}

// GetSubscriptions retrieves all Subscription tuples or returns an empty array if no tuples are found
func (r *CommonRepository) GetSubscriptions(ctx context.Context) ([]commonmodels.Subscription, error) {
	return utils.FindAll[commonmodels.Subscription](ctx, r.Db)
}

// GetSubscription retrieves a specific Subscription tuple or returns ErrNotFound if not found
func (r *CommonRepository) GetSubscription(ctx context.Context, id uuid.UUID) (*commonmodels.Subscription, error) {
	return utils.Find[commonmodels.Subscription](ctx, r.Db, id)
}

// DeleteSubscription deletes a Subscription tuple.  The caller should ensure that it exists prior to calling this.
func (r *CommonRepository) DeleteSubscription(ctx context.Context, id uuid.UUID) (int64, error) {
	expr := psql.Quote(commonmodels.Subscription{}.PrimaryKey()).EQ(psql.Arg(id))
	return utils.Delete[commonmodels.Subscription](ctx, r.Db, expr)
}

// CreateSubscription create a new Subscription tuple
func (r *CommonRepository) CreateSubscription(ctx context.Context, subscription *commonmodels.Subscription) (*commonmodels.Subscription, error) {
	return utils.Create[commonmodels.Subscription](ctx, r.Db, *subscription)
}

// UpdateSubscription updates a specific Subscription tuple
func (r *CommonRepository) UpdateSubscription(ctx context.Context, subscription *commonmodels.Subscription) (*commonmodels.Subscription, error) {
	return utils.Update[commonmodels.Subscription](ctx, r.Db, *subscription.SubscriptionID, *subscription)
}

// GetDataSourceByName retrieves a specific DataSource tuple by name or returns ErrNotFound if not found
func (r *CommonRepository) GetDataSourceByName(ctx context.Context, name string) (*commonmodels.DataSource, error) {
	e := psql.Quote("name").EQ(psql.Arg(name))
	records, err := utils.Search[commonmodels.DataSource](ctx, r.Db, e)
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
func (r *CommonRepository) CreateDataSource(ctx context.Context, dataSource *commonmodels.DataSource) (*commonmodels.DataSource, error) {
	return utils.Create[commonmodels.DataSource](ctx, r.Db, *dataSource)
}

// UpdateDataSource updates a specific DataSource tuple
func (r *CommonRepository) UpdateDataSource(ctx context.Context, dataSource *commonmodels.DataSource) (*commonmodels.DataSource, error) {
	return utils.Update[commonmodels.DataSource](ctx, r.Db, *dataSource.DataSourceID, *dataSource)
}

// CreateDataChangeEvent creates a new DataSource tuple
func (r *CommonRepository) CreateDataChangeEvent(ctx context.Context, dataChangeEvent *commonmodels.DataChangeEvent) (*commonmodels.DataChangeEvent, error) {
	return utils.Create[commonmodels.DataChangeEvent](ctx, r.Db, *dataChangeEvent)
}

// DeleteDataChangeEvent deletes a DataChangeEvent tuple.  The caller should ensure that it exists prior to calling this.
func (r *CommonRepository) DeleteDataChangeEvent(ctx context.Context, id uuid.UUID) (int64, error) {
	expr := psql.Quote(commonmodels.DataChangeEvent{}.PrimaryKey()).EQ(psql.Arg(id))
	return utils.Delete[commonmodels.DataChangeEvent](ctx, r.Db, expr)
}

// GetDataChangeEvents retrieves all DataChangeEvent tuples or returns an empty array if no tuples are found
func (r *CommonRepository) GetDataChangeEvents(ctx context.Context) ([]commonmodels.DataChangeEvent, error) {
	return utils.FindAll[commonmodels.DataChangeEvent](ctx, r.Db)
}

// ClaimDataChangeEvent claims a batch of DataChangeEvent and updates the high-water mark.
func ClaimDataChangeEvent(pool *pgxpool.Pool, ctx context.Context) ([]commonmodels.DataChangeEvent, error) {
	dataChangeEvents := make([]commonmodels.DataChangeEvent, 0)
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		// Retrieve the current high watermark.
		highWatermark, err := getHighWatermark(ctx, tx)
		if err != nil {
			return fmt.Errorf("failed to get high watermark: %w", err)
		}

		// Claim new outbox events (use FOR UPDATE SKIP LOCKED to avoid duplicate claims).
		dataChangeEvents, err = getLatestDataChangeEvent(ctx, tx, highWatermark)
		if err != nil {
			return fmt.Errorf("failed to get latest data change events: %w", err)
		}

		// Update the high watermark if there are new events
		if err := updateHighWatermark(ctx, tx, dataChangeEvents, highWatermark); err != nil {
			return fmt.Errorf("failed to update high watermark: %w", err)
		}

		return nil
	}); err != nil {
		return dataChangeEvents, fmt.Errorf("failed transaction to claim notifications: %w", err)
	}

	return dataChangeEvents, nil
}

func getLatestDataChangeEvent(ctx context.Context, tx pgx.Tx, highWatermark int) ([]commonmodels.DataChangeEvent, error) {
	m := commonmodels.DataChangeEvent{}
	all := utils.GetAllDBTagsFromStruct(m)

	queryGetLatestEvents := psql.Select(
		sm.Columns(all.Columns()...),
		sm.From(m.TableName()),
		sm.Where(psql.Quote(all["SequenceID"]).GT(psql.Arg(highWatermark))),
		sm.OrderBy(all["SequenceID"]).Asc(),
		sm.ForUpdate(m.TableName()).SkipLocked(),
	)

	sql, params, err := queryGetLatestEvents.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build queryGetLatestEvents query: %w", err)
	}

	dataChangeEvents, err := utils.ExecuteCollectRows[commonmodels.DataChangeEvent](ctx, tx, sql, params)
	if err != nil {
		return nil, fmt.Errorf("failed to execute queryGetLatestEvents: %w", err)
	}
	return dataChangeEvents, nil
}

// getHighWatermark from notification_cursor get last_event_id to get the highwatermark
func getHighWatermark(ctx context.Context, tx pgx.Tx) (int, error) {
	m := commonmodels.NotificationCursor{}
	all := utils.GetAllDBTagsFromStruct(m)

	queryGetHighWater := psql.Select(
		sm.Columns(all["LastEventID"]),
		sm.From(m.TableName()),
		sm.Where(psql.Quote(all["ID"]).EQ(psql.Arg(1))),
	)

	sql, params, err := queryGetHighWater.Build()
	if err != nil {
		return 0, fmt.Errorf("failed to build queryGetHighWater query: %w", err)
	}

	highWatermark, err := utils.ExecuteCollectExactlyOneRow[commonmodels.NotificationCursor](ctx, tx, sql, params)
	if err != nil {
		return 0, fmt.Errorf("failed to execute queryGetHighWater: %w", err)
	}

	return highWatermark.LastEventID, nil
}

// updateHighWatermark update notification cursor with the highest datachange event
func updateHighWatermark(ctx context.Context, tx pgx.Tx, dataChangeEvents []commonmodels.DataChangeEvent, highWatermark int) error {
	if len(dataChangeEvents) == 0 {
		return nil
	}

	m := commonmodels.NotificationCursor{}
	all := utils.GetAllDBTagsFromStruct(m)

	lastSeqProcessed := dataChangeEvents[len(dataChangeEvents)-1].SequenceID // The DataChangeEvent should come in sorted in ascending order
	queryUpdateHighWater := psql.Update(
		um.Table(m.TableName()),
		um.SetCol(all["LastEventID"]).ToArg(lastSeqProcessed),
		um.Where(psql.Quote(all["ID"]).EQ(psql.Arg(1))),                         // This is always be same since we only expect one row to be present
		um.Where(psql.Quote(all["LastEventID"]).LT(psql.Arg(lastSeqProcessed))), // Make sure high is always the highest
		um.Returning(all["LastEventID"]),
	)

	sql, params, err := queryUpdateHighWater.Build()
	if err != nil {
		return fmt.Errorf("failed to build queryUpdateHighWater query: %w", err)
	}

	updateHighWatermark, err := utils.ExecuteCollectExactlyOneRow[commonmodels.NotificationCursor](ctx, tx, sql, params)
	if err != nil {
		return fmt.Errorf("failed to execute queryUpdateHighWater: %w", err)
	}

	slog.Debug("high-watermark", "from", highWatermark, "to", updateHighWatermark.LastEventID)

	return nil
}
