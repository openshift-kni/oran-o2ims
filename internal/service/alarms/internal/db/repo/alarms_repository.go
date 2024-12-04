package repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

// All DB interaction code goes here

type AlarmsRepository struct {
	Db *pgxpool.Pool
}

// GetAlarmEventRecordWithUuid grabs a row of alarm_event_record using uuid
func (ar *AlarmsRepository) GetAlarmEventRecordWithUuid(ctx context.Context, uuid uuid.UUID) (*models.AlarmEventRecord, error) {
	return utils.Find[models.AlarmEventRecord](ctx, ar.Db, uuid, nil)
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
	tags := utils.GetDBTagsFromStructFields(record, "AlarmDictionaryVersion", "EntityType", "Vendor", "ResourceTypeID")

	// Important to keep the order. The order of tags.Columns().. is not deterministic
	values := []bob.Expression{psql.Arg(record.AlarmDictionaryVersion, record.EntityType, record.Vendor, record.ResourceTypeID)}
	records, err := utils.UpsertOnConflict[models.AlarmDictionary](ctx, ar.Db, []string{tags["AlarmDictionaryVersion"], tags["EntityType"], tags["Vendor"], tags["ResourceTypeID"]}, values)
	if err != nil {
		return nil, fmt.Errorf("failed to upsert alarm dictionary: %w", err)
	}

	return records, nil
}

// UpsertAlarmDefinitions inserts or updates alarm definition records
func (ar *AlarmsRepository) UpsertAlarmDefinitions(ctx context.Context, records []models.AlarmDefinition) ([]models.AlarmDefinition, error) {
	if len(records) == 0 {
		return nil, nil
	}

	tags := utils.GetDBTagsFromStructFields(records[0], "AlarmName", "AlarmLastChange", "AlarmDescription", "ProposedRepairActions", "AlarmAdditionalFields", "AlarmDictionaryID", "Severity")
	var values []bob.Expression
	for _, record := range records {
		// Important to keep the order. The order of tags.Columns().. is not deterministic
		values = append(values, psql.Arg(record.AlarmName, record.AlarmLastChange, record.AlarmDescription, record.ProposedRepairActions, record.AlarmAdditionalFields, record.AlarmDictionaryID, record.Severity))
	}

	records, err := utils.UpsertOnConflictConstraint[models.AlarmDefinition](ctx, ar.Db, []string{tags["AlarmName"], tags["AlarmLastChange"], tags["AlarmDescription"], tags["ProposedRepairActions"], tags["AlarmAdditionalFields"], tags["AlarmDictionaryID"], tags["Severity"]}, values)
	if err != nil {
		return nil, fmt.Errorf("failed to upsert alarm dictionary: %w", err)
	}

	return records, nil
}
