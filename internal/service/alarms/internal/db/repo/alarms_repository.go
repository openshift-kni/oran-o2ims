package repo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

// All DB interaction code goes here

type AlarmsRepository struct {
	Db *pgxpool.Pool
}

// GetAlarmEventRecordWithUuid grabs a row of alarm_event_record using uuid
func (ar *AlarmsRepository) GetAlarmEventRecordWithUuid(ctx context.Context, uuid uuid.UUID) ([]models.AlarmEventRecord, error) {
	// Build sql query
	record := models.AlarmEventRecord{}
	tags := utils.GetAllDBTagsFromStruct(&record)

	query, args, err := psql.Select(
		sm.Columns(tags.Columns()...),
		sm.From(record.TableName()),
		sm.Where(psql.Quote(tags["AlarmEventRecordID"]).EQ(psql.Arg(uuid))),
	).Build()
	if err != nil {
		return []models.AlarmEventRecord{}, fmt.Errorf("failed to build query: %w", err)
	}

	// Run query
	rows, _ := ar.Db.Query(ctx, query, args...) // note: err is passed on to Collect* func so we can ignore this
	record, err = pgx.CollectExactlyOneRow(rows, pgx.RowToStructByNameLax[models.AlarmEventRecord])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Info("No AlarmEventRecord found", "uuid", uuid)
			return []models.AlarmEventRecord{}, nil
		}
		return []models.AlarmEventRecord{}, fmt.Errorf("failed to call database: %w", err)
	}

	slog.Info("AlarmEventRecord found", "uuid", uuid)
	return []models.AlarmEventRecord{record}, nil
}
