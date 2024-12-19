package repo

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dialect"
	"github.com/stephenafamo/bob/dialect/psql/im"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

// All DB interaction code goes here

type AlarmsRepository struct {
	Db *pgxpool.Pool
}

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

// GetAlarmDefinition grabs a row of alarm_definition using a primary key
func (ar *AlarmsRepository) GetAlarmDefinition(ctx context.Context, id uuid.UUID) (*models.AlarmDefinition, error) {
	return utils.Find[models.AlarmDefinition](ctx, ar.Db, id)
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
