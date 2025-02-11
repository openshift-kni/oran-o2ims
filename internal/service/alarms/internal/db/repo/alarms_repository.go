package repo

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	commonmodels "github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
	"github.com/stephenafamo/bob/dialect/psql/dm"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dialect"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"

	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

type AlarmsRepository struct {
	Db utils.DBQuery
}

// Compile time check for interface implementation
var _ AlarmRepositoryInterface = (*AlarmsRepository)(nil)

// GetAlarmEventRecords grabs all rows of alarm_event_record
func (ar *AlarmsRepository) GetAlarmEventRecords(ctx context.Context) ([]models.AlarmEventRecord, error) {
	return utils.FindAll[models.AlarmEventRecord](ctx, ar.Db)
}

func (ar *AlarmsRepository) PatchAlarmEventRecordACK(ctx context.Context, id uuid.UUID, record *models.AlarmEventRecord) (*models.AlarmEventRecord, error) {
	return utils.Update[models.AlarmEventRecord](ctx, ar.Db, id, *record, "AlarmAcknowledged", "AlarmAcknowledgedTime", "PerceivedSeverity", "AlarmClearedTime", "AlarmChangedTime")
}

// GetAlarmEventRecord grabs a row of alarm_event_record using a primary key
func (ar *AlarmsRepository) GetAlarmEventRecord(ctx context.Context, id uuid.UUID) (*models.AlarmEventRecord, error) {
	return utils.Find[models.AlarmEventRecord](ctx, ar.Db, id)
}

// CreateServiceConfiguration inserts a new row of alarm_service_configuration or returns the existing one
func (ar *AlarmsRepository) CreateServiceConfiguration(ctx context.Context, defaultRetentionPeriod int) (*models.ServiceConfiguration, error) {
	records, err := utils.FindAll[models.ServiceConfiguration](ctx, ar.Db)
	if err != nil {
		return nil, err
	}

	// Return record if it already exists
	if len(records) == 1 {
		slog.Debug("Service configuration already exists")
		return &records[0], nil
	}

	// If there are more than one record, pick the first one and delete the rest
	if len(records) > 1 {
		slog.Debug("Multiple service configurations found, deleting all but the first")

		ids := make([]any, 0, len(records)-1)
		for i := 1; i < len(records); i++ {
			ids = append(ids, records[i].ID)
		}

		_, err = utils.Delete[models.ServiceConfiguration](ctx, ar.Db, psql.Quote(models.ServiceConfiguration{}.PrimaryKey()).In(psql.Arg(ids...)))
		if err != nil {
			return nil, fmt.Errorf("failed to delete additional service configurations: %w", err)
		}

		return &records[0], nil
	}

	slog.Debug("Creating new service configuration")

	// Create a new record
	record := models.ServiceConfiguration{
		RetentionPeriod: defaultRetentionPeriod,
	}
	return utils.Create[models.ServiceConfiguration](ctx, ar.Db, record, "RetentionPeriod")
}

// GetServiceConfigurations grabs all rows of alarm_service_configuration
func (ar *AlarmsRepository) GetServiceConfigurations(ctx context.Context) ([]models.ServiceConfiguration, error) {
	return utils.FindAll[models.ServiceConfiguration](ctx, ar.Db)
}

// UpdateServiceConfiguration updates a row of alarm_service_configuration using a primary key
func (ar *AlarmsRepository) UpdateServiceConfiguration(ctx context.Context, id uuid.UUID, record *models.ServiceConfiguration) (*models.ServiceConfiguration, error) {
	return utils.Update[models.ServiceConfiguration](ctx, ar.Db, id, *record, "RetentionPeriod", "Extensions")
}

// GetAlarmSubscriptions grabs all rows of alarm_subscription
func (ar *AlarmsRepository) GetAlarmSubscriptions(ctx context.Context) ([]models.AlarmSubscription, error) {
	return utils.FindAll[models.AlarmSubscription](ctx, ar.Db)
}

// DeleteAlarmSubscription deletes a row of alarm_subscription using a primary key
func (ar *AlarmsRepository) DeleteAlarmSubscription(ctx context.Context, id uuid.UUID) (int64, error) {
	expr := psql.Quote(models.AlarmSubscription{}.PrimaryKey()).EQ(psql.Arg(id))
	return utils.Delete[models.AlarmSubscription](ctx, ar.Db, expr)
}

// CreateAlarmSubscription inserts a new row of alarm_subscription
func (ar *AlarmsRepository) CreateAlarmSubscription(ctx context.Context, record models.AlarmSubscription) (*models.AlarmSubscription, error) {
	return utils.Create[models.AlarmSubscription](ctx, ar.Db, record, "ConsumerSubscriptionID", "Filter", "Callback", "EventCursor")
}

// GetAlarmSubscription grabs a row of alarm_subscription using a primary key
func (ar *AlarmsRepository) GetAlarmSubscription(ctx context.Context, id uuid.UUID) (*models.AlarmSubscription, error) {
	return utils.Find[models.AlarmSubscription](ctx, ar.Db, id)
}

// UpsertAlarmEventRecord insert and updating an AlarmEventRecord.
func (ar *AlarmsRepository) UpsertAlarmEventRecord(ctx context.Context, records []models.AlarmEventRecord, globalCloudId uuid.UUID) error {
	if len(records) == 0 {
		slog.Warn("No records for events upsert")
		return nil // this should never happen but handling it gracefully for tests
	}

	// Build queries for each record
	sql, params, err := buildAlarmEventRecordUpsertQuery(records, globalCloudId)
	if err != nil {
		return fmt.Errorf("failed to build query for event upsert: %w", err)
	}

	r, err := ar.Db.Exec(ctx, sql, params...)
	if err != nil {
		return fmt.Errorf("failed to execute upsert query: %w", err)
	}

	slog.Info("Successfully inserted and updated alerts from alertmanager", "count", r.RowsAffected())
	return nil
}

// buildAlarmEventRecordUpsertQuery builds the query for insert and updating an AlarmEventRecord
func buildAlarmEventRecordUpsertQuery(records []models.AlarmEventRecord, globalCloudId uuid.UUID) (string, []any, error) {
	m := models.AlarmEventRecord{}
	query := psql.Insert(im.Into(m.TableName()))

	// Set cols
	query.Expression.Columns = utils.GetColumns(records[0], []string{
		"AlarmRaisedTime", "AlarmClearedTime", "AlarmAcknowledgedTime",
		"AlarmAcknowledged", "PerceivedSeverity", "Extensions",
		"ObjectID", "ObjectTypeID", "AlarmStatus",
		"Fingerprint", "AlarmDefinitionID", "ProbableCauseID", "GlobalCloudID",
	})

	// Set values
	values := make([]bob.Mod[*dialect.InsertQuery], 0, len(records))
	for _, record := range records {
		values = append(values, im.Values(psql.Arg(
			record.AlarmRaisedTime, record.AlarmClearedTime, record.AlarmAcknowledgedTime,
			record.AlarmAcknowledged, record.PerceivedSeverity, record.Extensions,
			record.ObjectID, record.ObjectTypeID, record.AlarmStatus,
			record.Fingerprint, record.AlarmDefinitionID, record.ProbableCauseID, globalCloudId,
		)))
	}
	query.Apply(values...)

	// Set upsert constraints
	// Cols here should match manage_alarm_event trigger function.
	// This will ensure alarm_changed_time, alarm_changed_time, alarm_sequence_number are always updated as long as it has not been previously acked.
	dbTags := utils.GetAllDBTagsFromStruct(m)
	query.Apply(im.OnConflictOnConstraint(m.OnConflict()).DoUpdate(
		im.SetExcluded(dbTags["AlarmStatus"]),
		im.SetExcluded(dbTags["AlarmClearedTime"]),
		im.SetExcluded(dbTags["PerceivedSeverity"]),
		im.SetExcluded(dbTags["ObjectID"]),
		im.SetExcluded(dbTags["ObjectTypeID"]),
		im.SetExcluded(dbTags["AlarmDefinitionID"]),
		im.SetExcluded(dbTags["ProbableCauseID"]),
		im.SetExcluded(dbTags["GlobalCloudID"]),
	))

	return query.Build() //nolint:wrapcheck
}

// TimeNow allows test to override time.Now
var TimeNow = time.Now

// ResolveNotificationIfNotInCurrent find and only keep the alerts that are available in the current payload
func (ar *AlarmsRepository) ResolveNotificationIfNotInCurrent(ctx context.Context, am *api.AlertmanagerNotification) error {
	if len(am.Alerts) == 0 {
		slog.Warn("No events to resolve")
		return nil // this should never happen
	}

	m := models.AlarmEventRecord{}
	dbTags := utils.GetAllDBTagsFromStruct(m)
	var (
		tableName          = m.TableName()
		fingerprint        = dbTags["Fingerprint"]
		raisedTime         = dbTags["AlarmRaisedTime"]
		clearedTime        = dbTags["AlarmClearedTime"]
		alarmStatus        = dbTags["AlarmStatus"]
		perceivedSeverity  = dbTags["PerceivedSeverity"]
		alarmEventRecordID = dbTags["AlarmEventRecordID"]
	)

	updateClearedTimeCase := fmt.Sprintf(
		"%s = CASE WHEN %s IS NULL THEN ? ELSE %s END",
		clearedTime, clearedTime, clearedTime,
	)

	query := psql.Update(
		um.Table(tableName),
		um.SetCol(alarmStatus).ToArg(api.Resolved),
		um.Set(psql.Raw(updateClearedTimeCase, TimeNow())),
		um.SetCol(perceivedSeverity).ToArg(api.CLEARED),
		um.Where(
			psql.Group(psql.Quote(fingerprint), psql.Quote(raisedTime)).
				NotIn(getGetAlertFingerPrintAndStartAt(am)...),
		),
		um.Returning(psql.Quote(alarmEventRecordID)),
	)

	sql, params, err := query.Build()
	if err != nil {
		return fmt.Errorf("failed to build AlarmEventRecord update query when processing AM notification: %w", err)
	}
	records, err := utils.ExecuteCollectRows[models.AlarmEventRecord](ctx, ar.Db, sql, params)
	if err != nil {
		return err
	}

	if len(records) > 0 {
		slog.Info("Successfully resolved alarms that no longer exist", "records", len(records))
	}
	return nil
}

func getGetAlertFingerPrintAndStartAt(am *api.AlertmanagerNotification) []bob.Expression {
	b := make([]bob.Expression, 0, len(am.Alerts))
	for _, alert := range am.Alerts {
		b = append(b, psql.ArgGroup(alert.Fingerprint, alert.StartsAt))
	}

	return b
}

// UpdateSubscriptionEventCursor update a given subscription event cursor with a alarm sequence value
func (ar *AlarmsRepository) UpdateSubscriptionEventCursor(ctx context.Context, subscription models.AlarmSubscription) error {
	_, err := utils.Update[models.AlarmSubscription](ctx, ar.Db, subscription.SubscriptionID, subscription, "EventCursor")
	if err != nil {
		return fmt.Errorf("failed to execute UpdateSubscriptionEventCursor query: %w", err)
	}

	return nil
}

// GetAllAlarmsDataChange get all outbox entries
func (ar *AlarmsRepository) GetAllAlarmsDataChange(ctx context.Context) ([]commonmodels.DataChangeEvent, error) {
	return utils.FindAll[commonmodels.DataChangeEvent](ctx, ar.Db)
}

// DeleteAlarmsDataChange delete outbox entry with given dataChangeID
func (ar *AlarmsRepository) DeleteAlarmsDataChange(ctx context.Context, dataChangeId uuid.UUID) error {
	dataChangeModel := commonmodels.DataChangeEvent{}
	dbTags := utils.GetAllDBTagsFromStruct(dataChangeModel)

	q := psql.Delete(
		dm.From(dataChangeModel.TableName()),
		dm.Where(psql.Quote(dbTags["DataChangeID"]).EQ(psql.Arg(dataChangeId))),
	)
	sql, params, err := q.Build()
	if err != nil {
		return fmt.Errorf("failed to build AlarmsDataChangeEvent delete query: %w", err)
	}

	_, err = ar.Db.Exec(ctx, sql, params...)
	if err != nil {
		return fmt.Errorf("failed to execute DeleteAlarmsDataChange: %w", err)
	}

	return nil
}

// ClaimDataChangeEvent claims a batch of DataChangeEvent and updates the high-water mark.
func (ar *AlarmsRepository) ClaimDataChangeEvent(ctx context.Context) ([]commonmodels.DataChangeEvent, error) {
	dataChangeEvents := make([]commonmodels.DataChangeEvent, 0)
	if err := pgx.BeginFunc(ctx, ar.Db, func(tx pgx.Tx) error {
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
	m := models.NotificationCursor{}
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

	highWatermark, err := utils.ExecuteCollectExactlyOneRow[models.NotificationCursor](ctx, tx, sql, params)
	if err != nil {
		return 0, fmt.Errorf("failed to execute queryGetHighWater: %w", err)
	}

	return highWatermark.LastEventID, nil
}

// updateHighWatermark update notification cursor with the highest datachange event
func updateHighWatermark(ctx context.Context, tx pgx.Tx, dataChangeEvents []commonmodels.DataChangeEvent, highWatermark int) error {
	if len(dataChangeEvents) > 0 {
		m := models.NotificationCursor{}
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

		updateHighWatermark, err := utils.ExecuteCollectExactlyOneRow[models.NotificationCursor](ctx, tx, sql, params)
		if err != nil {
			return fmt.Errorf("failed to execute queryUpdateHighWater: %w", err)
		}

		slog.Debug("high-watermark", "from", highWatermark, "to", updateHighWatermark.LastEventID)
	}

	return nil
}
