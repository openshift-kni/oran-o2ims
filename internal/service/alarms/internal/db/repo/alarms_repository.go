/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package repo

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	commonmodels "github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
	"github.com/stephenafamo/bob/dialect/psql/dm"

	"github.com/google/uuid"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dialect"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/um"

	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

type AlarmsRepository struct {
	Db svcutils.DBQuery
}

// Compile time check for interface implementation
var _ AlarmRepositoryInterface = (*AlarmsRepository)(nil)

// WithTransaction a helper function do transaction without exposing anything internal to repo
func (ar *AlarmsRepository) WithTransaction(ctx context.Context, fn func(tx pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, ar.Db, fn) //nolint:wrapcheck
}

// GetAlarmEventRecords grabs all rows of alarm_event_record
func (ar *AlarmsRepository) GetAlarmEventRecords(ctx context.Context) ([]models.AlarmEventRecord, error) {
	return svcutils.FindAll[models.AlarmEventRecord](ctx, ar.Db)
}

func (ar *AlarmsRepository) PatchAlarmEventRecordACK(ctx context.Context, id uuid.UUID, record *models.AlarmEventRecord) (*models.AlarmEventRecord, error) {
	return svcutils.Update[models.AlarmEventRecord](ctx, ar.Db, id, *record, "AlarmAcknowledged", "AlarmAcknowledgedTime", "PerceivedSeverity", "AlarmClearedTime", "AlarmChangedTime")
}

// GetAlarmEventRecord grabs a row of alarm_event_record using a primary key
func (ar *AlarmsRepository) GetAlarmEventRecord(ctx context.Context, id uuid.UUID) (*models.AlarmEventRecord, error) {
	return svcutils.Find[models.AlarmEventRecord](ctx, ar.Db, id)
}

// CreateServiceConfiguration inserts a new row of alarm_service_configuration or returns the existing one
func (ar *AlarmsRepository) CreateServiceConfiguration(ctx context.Context, defaultRetentionPeriod int) (*models.ServiceConfiguration, error) {
	records, err := svcutils.FindAll[models.ServiceConfiguration](ctx, ar.Db)
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

		_, err = svcutils.Delete[models.ServiceConfiguration](ctx, ar.Db, psql.Quote(models.ServiceConfiguration{}.PrimaryKey()).In(psql.Arg(ids...)))
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
	return svcutils.Create[models.ServiceConfiguration](ctx, ar.Db, record, "RetentionPeriod")
}

// GetServiceConfigurations grabs all rows of alarm_service_configuration
func (ar *AlarmsRepository) GetServiceConfigurations(ctx context.Context) ([]models.ServiceConfiguration, error) {
	return svcutils.FindAll[models.ServiceConfiguration](ctx, ar.Db)
}

// UpdateServiceConfiguration updates a row of alarm_service_configuration using a primary key
func (ar *AlarmsRepository) UpdateServiceConfiguration(ctx context.Context, id uuid.UUID, record *models.ServiceConfiguration) (*models.ServiceConfiguration, error) {
	return svcutils.Update[models.ServiceConfiguration](ctx, ar.Db, id, *record, "RetentionPeriod", "Extensions")
}

// GetAlarmSubscriptions grabs all rows of alarm_subscription
func (ar *AlarmsRepository) GetAlarmSubscriptions(ctx context.Context) ([]models.AlarmSubscription, error) {
	return svcutils.FindAll[models.AlarmSubscription](ctx, ar.Db)
}

// DeleteAlarmSubscription deletes a row of alarm_subscription using a primary key
func (ar *AlarmsRepository) DeleteAlarmSubscription(ctx context.Context, id uuid.UUID) (int64, error) {
	expr := psql.Quote(models.AlarmSubscription{}.PrimaryKey()).EQ(psql.Arg(id))
	return svcutils.Delete[models.AlarmSubscription](ctx, ar.Db, expr)
}

// CreateAlarmSubscription inserts a new row of alarm_subscription
func (ar *AlarmsRepository) CreateAlarmSubscription(ctx context.Context, record models.AlarmSubscription) (*models.AlarmSubscription, error) {
	return svcutils.Create[models.AlarmSubscription](ctx, ar.Db, record, "ConsumerSubscriptionID", "Filter", "Callback", "EventCursor")
}

// GetAlarmSubscription grabs a row of alarm_subscription using a primary key
func (ar *AlarmsRepository) GetAlarmSubscription(ctx context.Context, id uuid.UUID) (*models.AlarmSubscription, error) {
	return svcutils.Find[models.AlarmSubscription](ctx, ar.Db, id)
}

// UpsertAlarmEventCaaSRecord insert and updating an AlarmEventRecord.
func (ar *AlarmsRepository) UpsertAlarmEventCaaSRecord(ctx context.Context, tx pgx.Tx, records []models.AlarmEventRecord, generationID int64) error {
	if len(records) == 0 {
		slog.Warn("No records for events upsert")
		return nil
	}

	m := models.AlarmEventRecord{}
	query := psql.Insert(im.Into(m.TableName()))

	// Set cols
	query.Expression.Columns = svcutils.GetColumns(records[0], []string{
		"AlarmRaisedTime", "AlarmClearedTime", "AlarmAcknowledgedTime",
		"AlarmAcknowledged", "PerceivedSeverity", "Extensions",
		"ObjectID", "ObjectTypeID", "AlarmStatus",
		"Fingerprint", "AlarmDefinitionID", "ProbableCauseID",
		"GenerationID", "AlarmSource",
	})

	// Set values
	values := make([]bob.Mod[*dialect.InsertQuery], 0, len(records))
	for _, record := range records {
		values = append(values, im.Values(psql.Arg(
			record.AlarmRaisedTime, record.AlarmClearedTime, record.AlarmAcknowledgedTime,
			record.AlarmAcknowledged, record.PerceivedSeverity, record.Extensions,
			record.ObjectID, record.ObjectTypeID, record.AlarmStatus,
			record.Fingerprint, record.AlarmDefinitionID, record.ProbableCauseID,
			generationID, record.AlarmSource,
		)))
	}
	query.Apply(values...)

	// Set upsert constraints
	// Cols here should match 'manage_alarm_event trigger' function as needed to trigger a notification using 'should_create_data_change_event'
	dbTags := svcutils.GetAllDBTagsFromStruct(m)
	query.Apply(im.OnConflictOnConstraint(m.OnConflict()).DoUpdate(
		im.SetExcluded(dbTags["AlarmStatus"]),
		im.SetExcluded(dbTags["AlarmClearedTime"]),
		im.SetExcluded(dbTags["PerceivedSeverity"]),
		im.SetExcluded(dbTags["ObjectID"]),
		im.SetExcluded(dbTags["ObjectTypeID"]),
		im.SetExcluded(dbTags["AlarmDefinitionID"]),
		im.SetExcluded(dbTags["ProbableCauseID"]),
		im.SetExcluded(dbTags["GenerationID"]),
		im.SetExcluded(dbTags["AlarmSource"]),
	))

	sql, params, err := query.Build(ctx)
	if err != nil {
		return fmt.Errorf("failed to build query for event upsert: %w", err)
	}

	_, err = tx.Exec(ctx, sql, params...)
	if err != nil {
		return fmt.Errorf("failed to execute upsert query: %w", err)
	}

	return nil
}

// TimeNow allows test to override time.Now
var TimeNow = time.Now

// ResolveStaleAlarmEventCaaSRecord resolve all alerts with older generation ID
func (ar *AlarmsRepository) ResolveStaleAlarmEventCaaSRecord(ctx context.Context, tx pgx.Tx, generationID int64) error {
	m := models.AlarmEventRecord{}
	dbTags := svcutils.GetAllDBTagsFromStruct(m)
	var (
		tableName          = m.TableName()
		generationIDCol    = dbTags["GenerationID"]
		clearedTime        = dbTags["AlarmClearedTime"]
		alarmStatus        = dbTags["AlarmStatus"]
		perceivedSeverity  = dbTags["PerceivedSeverity"]
		alarmEventRecordID = dbTags["AlarmEventRecordID"]
		alarmSource        = dbTags["AlarmSource"]
	)

	updateClearedTimeCase := fmt.Sprintf(
		"%s = CASE WHEN %s IS NULL THEN ? ELSE %s END",
		clearedTime, clearedTime, clearedTime,
	)

	query := psql.Update(
		um.Table(tableName),
		um.SetCol(alarmStatus).ToArg(api.Resolved),                                                         // Set to resolved
		um.SetCol(perceivedSeverity).ToArg(api.CLEARED),                                                    // Set corresponding perceivedSeverity
		um.Set(psql.Raw(updateClearedTimeCase, TimeNow())),                                                 // Set a resolved time if not there already
		um.Where(psql.Quote(generationIDCol).LT(psql.Arg(generationID))),                                   // An alert is stale if its GenID is less than current
		um.Where(psql.Quote(alarmSource).In(psql.Arg(models.AlarmSourceCaaS, models.AlarmSourceHardware))), // Support both CaaS and hardware alerts
		um.Where(psql.Quote(alarmStatus).NE(psql.Arg(api.Resolved))),                                       // If already resolved no need to process that row
		um.Returning(psql.Quote(alarmEventRecordID)),
	)

	sql, params, err := query.Build(ctx)
	if err != nil {
		return fmt.Errorf("failed to build AlarmEventRecord update query when processing AM notification: %w", err)
	}
	records, err := svcutils.ExecuteCollectRows[models.AlarmEventRecord](ctx, tx, sql, params)
	if err != nil {
		return err
	}

	if len(records) > 0 {
		slog.Info("Successfully resolved stale CaaS (alertmanager) alarmeventrecords", "records", len(records))
	}
	return nil
}

// UpdateSubscriptionEventCursor update a given subscription event cursor with a alarm sequence value
func (ar *AlarmsRepository) UpdateSubscriptionEventCursor(ctx context.Context, subscription models.AlarmSubscription) error {
	_, err := svcutils.Update[models.AlarmSubscription](ctx, ar.Db, subscription.SubscriptionID, subscription, "EventCursor")
	if err != nil {
		return fmt.Errorf("failed to execute UpdateSubscriptionEventCursor query: %w", err)
	}

	return nil
}

// GetAllAlarmsDataChange get all outbox entries
func (ar *AlarmsRepository) GetAllAlarmsDataChange(ctx context.Context) ([]commonmodels.DataChangeEvent, error) {
	return svcutils.FindAll[commonmodels.DataChangeEvent](ctx, ar.Db)
}

// DeleteAlarmsDataChange delete outbox entry with given dataChangeID
func (ar *AlarmsRepository) DeleteAlarmsDataChange(ctx context.Context, dataChangeId uuid.UUID) error {
	dataChangeModel := commonmodels.DataChangeEvent{}
	dbTags := svcutils.GetAllDBTagsFromStruct(dataChangeModel)

	q := psql.Delete(
		dm.From(dataChangeModel.TableName()),
		dm.Where(psql.Quote(dbTags["DataChangeID"]).EQ(psql.Arg(dataChangeId))),
	)
	sql, params, err := q.Build(ctx)
	if err != nil {
		return fmt.Errorf("failed to build AlarmsDataChangeEvent delete query: %w", err)
	}

	_, err = ar.Db.Exec(ctx, sql, params...)
	if err != nil {
		return fmt.Errorf("failed to execute DeleteAlarmsDataChange: %w", err)
	}

	return nil
}
