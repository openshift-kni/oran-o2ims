package repo

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/stephenafamo/bob/dialect/psql/dm"

	"github.com/stephenafamo/bob/dialect/psql/dialect"
	"github.com/stephenafamo/bob/dialect/psql/um"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/alertmanager"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

// All DB interaction code goes here

type AlarmsRepository struct {
	Db *pgxpool.Pool
}

// GetAlarmEventRecord grabs a row of alarm_event_record using a primary key
func (ar *AlarmsRepository) GetAlarmEventRecord(ctx context.Context, id uuid.UUID) (*models.AlarmEventRecord, error) {
	return utils.Find[models.AlarmEventRecord](ctx, ar.Db, id)
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
	return utils.Create[models.AlarmSubscription](ctx, ar.Db, record, "ConsumerSubscriptionID", "Filter", "Callback")
}

// GetAlarmSubscription grabs a row of alarm_subscription using a primary key
func (ar *AlarmsRepository) GetAlarmSubscription(ctx context.Context, id uuid.UUID) (*models.AlarmSubscription, error) {
	return utils.Find[models.AlarmSubscription](ctx, ar.Db, id)
}

// DeleteAlarmDictionariesNotIn deletes all alarm dictionaries that are not in the list of resource type IDs
func (ar *AlarmsRepository) DeleteAlarmDictionariesNotIn(ctx context.Context, ids []any) error {
	tags := utils.GetDBTagsFromStructFields(models.AlarmDictionary{}, "ResourceTypeID")

	expr := psql.Quote(tags["ResourceTypeID"]).NotIn(psql.Arg(ids...))
	_, err := utils.Delete[models.AlarmDictionary](ctx, ar.Db, expr)
	return err
}

// DeleteAlarmDefinitionsNotIn deletes all alarm definitions identified by the primary key that are not in the list of IDs.
// The Where expression also uses the column "resource_type_id" to filter the records
func (ar *AlarmsRepository) DeleteAlarmDefinitionsNotIn(ctx context.Context, ids []any, resourceTypeID uuid.UUID) error {
	tags := utils.GetDBTagsFromStructFields(models.AlarmDefinition{}, "ResourceTypeID")

	expr := psql.Quote(models.AlarmDefinition{}.PrimaryKey()).NotIn(psql.Arg(ids...)).And(psql.Quote(tags["ResourceTypeID"]).EQ(psql.Arg(resourceTypeID)))
	_, err := utils.Delete[models.AlarmDefinition](ctx, ar.Db, expr)
	return err
}

// UpsertAlarmDictionary inserts or updates an alarm dictionary record
func (ar *AlarmsRepository) UpsertAlarmDictionary(ctx context.Context, record models.AlarmDictionary) ([]models.AlarmDictionary, error) {
	dbModel := models.AlarmDictionary{}

	tags := utils.GetDBTagsFromStructFields(dbModel, "AlarmDictionaryVersion", "EntityType", "Vendor", "ResourceTypeID")

	columns := []string{tags["AlarmDictionaryVersion"], tags["EntityType"], tags["Vendor"], tags["ResourceTypeID"]}

	query := psql.Insert(
		im.Into(record.TableName(), columns...),
		im.Values(psql.Arg(record.AlarmDictionaryVersion, record.EntityType, record.Vendor, record.ResourceTypeID)),
		im.OnConflict(record.OnConflict()).DoUpdate(
			im.SetExcluded(columns...)),
		im.Returning(record.PrimaryKey()),
	)

	sql, args, err := query.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	return utils.ExecuteCollectRows[models.AlarmDictionary](ctx, ar.Db, sql, args)
}

// UpsertAlarmDefinitions inserts or updates alarm definition records
func (ar *AlarmsRepository) UpsertAlarmDefinitions(ctx context.Context, records []models.AlarmDefinition) ([]models.AlarmDefinition, error) {
	dbModel := models.AlarmDefinition{}

	if len(records) == 0 {
		return nil, nil
	}

	tags := utils.GetDBTagsFromStructFields(records[0], "AlarmName", "AlarmLastChange", "AlarmDescription", "ProposedRepairActions", "AlarmAdditionalFields", "AlarmDictionaryID", "Severity")
	columns := []string{tags["AlarmName"], tags["AlarmLastChange"], tags["AlarmDescription"], tags["ProposedRepairActions"], tags["AlarmAdditionalFields"], tags["AlarmDictionaryID"], tags["Severity"]}

	modInsert := []bob.Mod[*dialect.InsertQuery]{
		im.Into(dbModel.TableName(), columns...),
		im.OnConflictOnConstraint(dbModel.OnConflict()).DoUpdate(
			im.SetExcluded(columns...)),
		im.Returning(dbModel.PrimaryKey()),
	}

	for _, record := range records {
		modInsert = append(modInsert, im.Values(psql.Arg(record.AlarmName, record.AlarmLastChange, record.AlarmDescription, record.ProposedRepairActions, record.AlarmAdditionalFields, record.AlarmDictionaryID, record.Severity)))
	}

	query := psql.Insert(
		modInsert...,
	)

	sql, args, err := query.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	return utils.ExecuteCollectRows[models.AlarmDefinition](ctx, ar.Db, sql, args)
}

// UpsertAlarmEventRecord insert and updating an AlarmEventRecord.
func (ar *AlarmsRepository) UpsertAlarmEventRecord(ctx context.Context, records []models.AlarmEventRecord) error {
	// Build queries for each record
	sql, params, err := buildAlarmEventRecordUpsertQuery(records, models.AlarmEventRecordView.Name(ctx).String())
	if err != nil {
		return fmt.Errorf("failed to build query for event upsert: %w", err)
	}

	_, err = ar.Db.Exec(ctx, sql, params...)
	if err != nil {
		return fmt.Errorf("failed to execute upsert query: %w", err)
	}

	slog.Info("Successfully inserted alerts from alertmanager", "table", models.AlarmEventRecordView.Name(ctx).String(), "count", len(records))
	return nil
}

// buildAlarmEventRecordUpsertQuery builds the query for insert and updating an AlarmEventRecord
func buildAlarmEventRecordUpsertQuery(records []models.AlarmEventRecord, tableName string) (string, []any, error) {
	query := psql.Insert(im.Into(tableName))

	// Dynamically construct the column list
	columns := []string{
		"alarm_raised_time", "alarm_changed_time", "alarm_cleared_time",
		"alarm_acknowledged_time", "alarm_acknowledged", "perceived_severity",
		"extensions", "resource_id", "resource_type_id",
		"alarm_status", "fingerprint", "alarm_definition_id",
		"probable_cause_id",
	}

	// Check if alarm_event_record_id is needed
	hasAlarmEventRecordID := false
	for _, record := range records {
		if record.AlarmEventRecordID != nil {
			hasAlarmEventRecordID = true
			columns = append([]string{"alarm_event_record_id"}, columns...)
			break
		}
	}

	// Set the column list for the InsertQuery
	query.Expression.Columns = columns

	// Add the values for each record
	values := make([]bob.Mod[*dialect.InsertQuery], 0, len(records))
	for _, record := range records {
		// Build the values for the row
		args := []any{
			record.AlarmRaisedTime, record.AlarmChangedTime, record.AlarmClearedTime,
			record.AlarmAcknowledgedTime, record.AlarmAcknowledged, record.PerceivedSeverity,
			record.Extensions, record.ResourceID, record.ResourceTypeID,
			record.AlarmStatus, record.Fingerprint, record.AlarmDefinitionID,
			record.ProbableCauseID,
		}

		if hasAlarmEventRecordID {
			if record.AlarmEventRecordID != nil {
				args = append([]any{record.AlarmEventRecordID}, args...)
			} else {
				args = append([]any{nil}, args...)
			}
		}

		values = append(values, im.Values(psql.Arg(args...)))
	}

	// Apply the values to the query
	query.Apply(values...)

	// constraints only applicable for non-archive table
	if tableName == models.AlarmEventRecordView.Name(context.Background()).String() {
		query.Apply(im.OnConflictOnConstraint("unique_fingerprint_alarm_raised_time").DoUpdate(
			im.SetExcluded("alarm_changed_time"),
			im.SetExcluded("alarm_cleared_time"),
			im.SetExcluded("alarm_status"),
			im.SetExcluded("resource_id"),
			im.SetExcluded("alarm_definition_id"),
			im.SetExcluded("probable_cause_id"),
		))
	}

	// Compile
	sql, args, err := query.Build()
	if err != nil {
		return sql, args, fmt.Errorf("failed build upsert query for tabls %s: %w", tableName, err)
	}

	return sql, args, nil
}

// GetAlarmDefinitions needed to build out aer
func (ar *AlarmsRepository) GetAlarmDefinitions(ctx context.Context, am *api.AlertmanagerNotification) ([]models.AlarmDefinition, error) {
	query := psql.Select(
		sm.Columns("alarm_name", "alarm_definition_id", "probable_cause_id", "resource_type_id"),
		sm.From("alarm_definition"),
		sm.Where(
			psql.Group(psql.Quote("alarm_name"), psql.Quote("resource_type_id")).
				In(getGetAlertNameAndResourceTypeID(am)...), // Dynamically pass the pairs
		),
	)

	sql, params, err := query.Build()
	if err != nil {
		return []models.AlarmDefinition{}, fmt.Errorf("failed to build alarm definitions query when processing AM notification: %w", err)
	}

	// Run query
	rows, _ := ar.Db.Query(ctx, sql, params...)
	records, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[models.AlarmDefinition])
	if err != nil {
		return []models.AlarmDefinition{}, fmt.Errorf("failed to collect alarm definitions: %w", err)
	}

	return records, nil
}

func getGetAlertNameAndResourceTypeID(am *api.AlertmanagerNotification) []bob.Expression {
	b := make([]bob.Expression, 0, len(am.Alerts))
	for _, alert := range am.Alerts {
		name := alertmanager.GetAlertName(*alert.Labels)
		resourceTypeId := alertmanager.GetResourceTypeID(*alert.Labels)
		b = append(b, psql.ArgGroup(name, resourceTypeId))
	}

	return b
}

// ArchiveResolvedAlarmEventRecords Delete resolved and move to archive
func (ar *AlarmsRepository) ArchiveResolvedAlarmEventRecords(ctx context.Context) error {
	return pgx.BeginFunc(ctx, ar.Db, func(tx pgx.Tx) error { //nolint:wrapcheck
		query := psql.Delete(
			dm.From(models.AlarmEventRecordView.NameAs(ctx)),
			dm.Where(psql.Quote("alarm_status").EQ(psql.Arg(api.Resolved))),
			dm.Returning(models.AlarmEventRecordView.Columns()),
		)

		sql, params, err := query.Build()
		if err != nil {
			return fmt.Errorf("failed to build delete alarm record query: %w", err)
		}

		// Run query to delete
		rows, _ := tx.Query(ctx, sql, params...)
		records, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[models.AlarmEventRecord])
		if err != nil {
			return fmt.Errorf("failed to delete alarm records: %w", err)
		}

		// Move deleted data to archived table which is identical to the original. Archived data will later be removed with cronjob.
		if len(records) > 0 {
			// Build queries for each record
			sql, params, err := buildAlarmEventRecordUpsertQuery(records, "alarm_event_record_archive")
			if err != nil {
				return fmt.Errorf("failed to build query for event upsert: %w", err)
			}

			_, err = tx.Exec(ctx, sql, params...)
			if err != nil {
				return fmt.Errorf("failed to execute upsert query: %w", err)
			}
			slog.Info("Successfully archived alarm event record records", "records", len(records))
		}

		return nil
	})
}

// ResolveNotificationIfNotInCurrent find and only keep the alerts that are available in the current payload
func (ar *AlarmsRepository) ResolveNotificationIfNotInCurrent(ctx context.Context, am *api.AlertmanagerNotification) error {
	query := psql.Update(
		um.Table(models.AlarmEventRecordView.Name(ctx)),
		um.SetCol("alarm_status").ToArg(api.Resolved),
		um.Where(
			psql.Group(psql.Quote("fingerprint"), psql.Quote("alarm_raised_time")).
				NotIn(getGetAlertFingerPrintAndStartAt(am)...),
		),
		um.Returning(psql.Quote("alarm_event_record_id")),
	)

	sql, params, err := query.Build()
	if err != nil {
		return fmt.Errorf("failed to build AlarmEventRecord update query when processing AM notification: %w", err)
	}

	rows, _ := ar.Db.Query(ctx, sql, params...)
	records, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[models.AlarmEventRecord])
	if err != nil {
		return fmt.Errorf("failed to call database w: %w", err)
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
