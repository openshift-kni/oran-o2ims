package repo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
)

// All DB interaction code goes here

type AlarmsRepository struct {
	Db *pgxpool.Pool
}

// GetAlarmEventRecordWithUuid grabs a row of alarm_event_record using uuid
func (ar *AlarmsRepository) GetAlarmEventRecordWithUuid(ctx context.Context, uuid uuid.UUID) ([]models.AlarmEventRecord, error) {
	// Build sql query
	query := `
SELECT alarm_event_record_id, alarm_definition_id, probable_cause_id, alarm_raised_time, 
       alarm_changed_time, alarm_cleared_time, alarm_acknowledged_time, 
       alarm_acknowledged, perceived_severity, extensions, resource_type_id
FROM alarm_event_record 
WHERE alarm_event_record_id = $1
`

	// Run query
	rows, _ := ar.Db.Query(ctx, query, uuid.String()) // note: err is passed on to Collect* func so we can ignore this
	record, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByNameLax[models.AlarmEventRecord])
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
