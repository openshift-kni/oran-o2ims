package repo

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

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
