package repo

import (
	"context"
	"fmt"

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

// GetAlarmEventRecordWithUuid grabs a row of alarm_event_record using uuid
func (ar *AlarmsRepository) GetAlarmEventRecordWithUuid(ctx context.Context, uuid uuid.UUID) (*models.AlarmEventRecord, error) {
	return utils.Find[models.AlarmEventRecord](ctx, ar.Db, uuid)
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
