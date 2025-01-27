package repo

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/alertmanager"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dialect"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"

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

		_, err = utils.Delete[models.AlarmDefinition](ctx, ar.Db, psql.Quote(models.AlarmDefinition{}.PrimaryKey()).In(psql.Arg(ids...)))
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

// DeleteAlarmDictionariesNotIn deletes all alarm dictionaries that are not in the list of object type IDs
func (ar *AlarmsRepository) DeleteAlarmDictionariesNotIn(ctx context.Context, ids []any) error {
	tags := utils.GetDBTagsFromStructFields(models.AlarmDictionary{}, "ObjectTypeID")

	expr := psql.Quote(tags["ObjectTypeID"]).NotIn(psql.Arg(ids...))
	_, err := utils.Delete[models.AlarmDictionary](ctx, ar.Db, expr)
	return err
}

// GetAlarmDefinition grabs a row of alarm_definition using a primary key
func (ar *AlarmsRepository) GetAlarmDefinition(ctx context.Context, id uuid.UUID) (*models.AlarmDefinition, error) {
	return utils.Find[models.AlarmDefinition](ctx, ar.Db, id)
}

// DeleteAlarmDefinitionsNotIn deletes all alarm definitions identified by the primary key that are not in the list of IDs.
// The Where expression also uses the column "object_type_id" to filter the records
func (ar *AlarmsRepository) DeleteAlarmDefinitionsNotIn(ctx context.Context, ids []any, objectTypeID uuid.UUID) (int64, error) {
	tags := utils.GetDBTagsFromStructFields(models.AlarmDefinition{}, "ObjectTypeID")

	expr := psql.Quote(models.AlarmDefinition{}.PrimaryKey()).NotIn(psql.Arg(ids...)).And(psql.Quote(tags["ObjectTypeID"]).EQ(psql.Arg(objectTypeID)))
	count, err := utils.Delete[models.AlarmDefinition](ctx, ar.Db, expr)
	return count, err
}

// UpsertAlarmDictionary inserts or updates an alarm dictionary record
func (ar *AlarmsRepository) UpsertAlarmDictionary(ctx context.Context, record models.AlarmDictionary) ([]models.AlarmDictionary, error) {
	dbModel := models.AlarmDictionary{}
	columns := utils.GetColumns(dbModel, []string{"AlarmDictionaryVersion", "EntityType", "Vendor", "ObjectTypeID"})

	query := psql.Insert(
		im.Into(record.TableName(), columns...),
		im.Values(psql.Arg(record.AlarmDictionaryVersion, record.EntityType, record.Vendor, record.ObjectTypeID)),
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
		return []models.AlarmDefinition{}, nil
	}

	columns := utils.GetColumns(records[0], []string{
		"AlarmName", "AlarmLastChange", "AlarmDescription",
		"ProposedRepairActions", "AlarmAdditionalFields", "AlarmDictionaryID",
		"Severity"},
	)

	modInsert := []bob.Mod[*dialect.InsertQuery]{
		im.Into(dbModel.TableName(), columns...),
		im.OnConflictOnConstraint(dbModel.OnConflict()).DoUpdate(
			im.SetExcluded(columns...)),
		im.Returning(dbModel.PrimaryKey()),
	}

	for _, record := range records {
		modInsert = append(modInsert, im.Values(psql.Arg(record.AlarmName, record.AlarmLastChange, record.AlarmDescription,
			record.ProposedRepairActions, record.AlarmAdditionalFields, record.AlarmDictionaryID,
			record.Severity)))
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
	sql, params, err := buildAlarmEventRecordUpsertQuery(records)
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
func buildAlarmEventRecordUpsertQuery(records []models.AlarmEventRecord) (string, []any, error) {
	m := models.AlarmEventRecord{}
	query := psql.Insert(im.Into(m.TableName()))

	// Set cols
	query.Expression.Columns = utils.GetColumns(records[0], []string{
		"AlarmRaisedTime", "AlarmClearedTime", "AlarmAcknowledgedTime",
		"AlarmAcknowledged", "PerceivedSeverity", "Extensions",
		"ObjectID", "ObjectTypeID", "AlarmStatus",
		"Fingerprint", "AlarmDefinitionID", "ProbableCauseID",
	})

	// Set values
	values := make([]bob.Mod[*dialect.InsertQuery], 0, len(records))
	for _, record := range records {
		values = append(values, im.Values(psql.Arg(
			record.AlarmRaisedTime, record.AlarmClearedTime, record.AlarmAcknowledgedTime,
			record.AlarmAcknowledged, record.PerceivedSeverity, record.Extensions,
			record.ObjectID, record.ObjectTypeID, record.AlarmStatus,
			record.Fingerprint, record.AlarmDefinitionID, record.ProbableCauseID,
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
	))

	return query.Build() //nolint:wrapcheck
}

// GetAlarmDefinitions needed to build out aer
func (ar *AlarmsRepository) GetAlarmDefinitions(ctx context.Context, am *api.AlertmanagerNotification, clusterIDToObjectTypeID map[uuid.UUID]uuid.UUID) ([]models.AlarmDefinition, error) {
	m := models.AlarmDefinition{}
	dbTags := utils.GetAllDBTagsFromStruct(m)
	query := psql.Select(
		sm.Columns(
			utils.GetColumnsAsAny(utils.GetColumns(m, []string{
				"AlarmName", "AlarmDefinitionID", "ProbableCauseID",
				"ObjectTypeID", "Severity",
			}))...),
		sm.From(m.TableName()),
		sm.Where(
			psql.Group(
				psql.Quote(dbTags["AlarmName"]),
				psql.Quote(dbTags["ObjectTypeID"]),
				psql.Quote(dbTags["Severity"]),
			).
				In(getGetAlertNameObjectTypeIDAndSeverity(am, clusterIDToObjectTypeID)...), // Dynamically pass the pairs
		),
	)

	sql, params, err := query.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build alarm definitions query when processing AM notification: %w", err)
	}

	return utils.ExecuteCollectRows[models.AlarmDefinition](ctx, ar.Db, sql, params)
}

func getGetAlertNameObjectTypeIDAndSeverity(am *api.AlertmanagerNotification, clusterIDToObjectTypeID map[uuid.UUID]uuid.UUID) []bob.Expression {
	var b []bob.Expression
	for _, alert := range am.Alerts {
		labels := *alert.Labels
		if id := alertmanager.GetClusterID(labels); id != nil {
			if objectTypeId, ok := clusterIDToObjectTypeID[*id]; ok {
				_, severity := alertmanager.GetPerceivedSeverity(labels)
				b = append(b, psql.ArgGroup(
					alertmanager.GetAlertName(labels),
					objectTypeId,
					severity,
				))
			}
		}
	}
	return b
}

// ResolveNotificationIfNotInCurrent find and only keep the alerts that are available in the current payload
func (ar *AlarmsRepository) ResolveNotificationIfNotInCurrent(ctx context.Context, am *api.AlertmanagerNotification) error {
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
		um.Set(psql.Raw(updateClearedTimeCase, time.Now())),
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

// GetAlarmsForSubscription for a given subscription get all alarms based on the sequence number and filter
func (ar *AlarmsRepository) GetAlarmsForSubscription(ctx context.Context, subscription models.AlarmSubscription) ([]models.AlarmEventRecord, error) {
	m := models.AlarmEventRecord{}
	dbTags := utils.GetAllDBTagsFromStruct(m)
	queryMods := []bob.Mod[*dialect.SelectQuery]{
		sm.Columns(utils.GetColumnsAsAny(utils.GetColumns(m, []string{
			"AlarmEventRecordID", "AlarmDefinitionID", "ProbableCauseID",
			"AlarmRaisedTime", "AlarmChangedTime", "AlarmClearedTime",
			"AlarmAcknowledgedTime", "AlarmAcknowledged", "PerceivedSeverity",
			"Extensions", "ObjectID", "ObjectTypeID",
			"NotificationEventType", "AlarmSequenceNumber",
		}))...),
		sm.From(m.TableName()),
	}

	// Start with the base condition
	// Collect all events that haven't been sent to the subscriber yet
	whereClause := psql.Quote(dbTags["AlarmSequenceNumber"]).GT(psql.Arg(subscription.EventCursor))
	// If we have a filter, add it to the WHERE clause to reduce further
	if subscription.Filter != nil {
		whereClause = psql.And(
			whereClause,
			psql.Quote(dbTags["NotificationEventType"]).NE(psql.Arg(subscription.Filter)),
		)
	}
	// Add WHERE and ORDER BY clauses
	queryMods = append(queryMods,
		sm.Where(whereClause),
		sm.OrderBy(dbTags["AlarmSequenceNumber"]).Asc(),
	)

	// Build final query
	query := psql.Select(queryMods...)

	sql, params, err := query.Build()
	if err != nil {
		return []models.AlarmEventRecord{}, fmt.Errorf("failed to build GetAlarmsForSubscription query: %w", err)
	}

	records, err := utils.ExecuteCollectRows[models.AlarmEventRecord](ctx, ar.Db, sql, params)
	if err != nil {
		return []models.AlarmEventRecord{}, fmt.Errorf("failed to execute GetAlarmsForSubscription query: %w", err)
	}

	if len(records) > 0 {
		slog.Info("Successfully got alarms for subscription", "alarm count", len(records), "Subscription", subscription.SubscriptionID)
	}
	return records, nil
}

// UpdateSubscriptionEventCursor update a given subscription event cursor with a alarm sequence value
func (ar *AlarmsRepository) UpdateSubscriptionEventCursor(ctx context.Context, subscription models.AlarmSubscription) error {
	_, err := utils.Update[models.AlarmSubscription](ctx, ar.Db, subscription.SubscriptionID, subscription, "EventCursor")
	if err != nil {
		return fmt.Errorf("failed to execute UpdateSubscriptionEventCursor query: %w", err)
	}

	return nil
}

// GetMaxAlarmSeq get the max seq value from alarms, if no alarms return 0
func (ar *AlarmsRepository) GetMaxAlarmSeq(ctx context.Context) (int64, error) {
	m := models.AlarmEventRecord{}
	dbTags := utils.GetAllDBTagsFromStruct(m)

	// Create the MAX function with COALESCE to handle NULL (defaults to 0)
	maxFunc := psql.F("COALESCE", psql.F("MAX", psql.Raw(dbTags["AlarmSequenceNumber"])), 0)
	query := psql.Select(
		sm.Columns(maxFunc),
		sm.From(psql.Quote(m.TableName())),
	)

	sql, args, err := query.Build()
	if err != nil {
		return 0, fmt.Errorf("failed to build query to get max sequence: %w", err)
	}

	var maxSeq int64
	if err := ar.Db.QueryRow(ctx, sql, args...).Scan(&maxSeq); err != nil {
		return 0, fmt.Errorf("failed to get max alarm seq: %w", err)
	}

	return maxSeq, nil
}
