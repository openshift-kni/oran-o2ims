package models

import (
	"time"

	"github.com/google/uuid"
)

// AlarmEventRecord represents a record in the alarm_event_record table.
type AlarmEventRecord struct {
	AlarmEventRecordID    uuid.UUID              `db:"alarm_event_record_id"`
	AlarmDefinitionID     uuid.UUID              `db:"alarm_definition_id"`
	ProbableCauseID       uuid.UUID              `db:"probable_cause_id"`
	AlarmRaisedTime       time.Time              `db:"alarm_raised_time"`
	AlarmChangedTime      *time.Time             `db:"alarm_changed_time"`
	AlarmClearedTime      *time.Time             `db:"alarm_cleared_time"`
	AlarmAcknowledgedTime *time.Time             `db:"alarm_acknowledged_time"`
	AlarmAcknowledged     bool                   `db:"alarm_acknowledged"`
	PerceivedSeverity     int                    `db:"perceived_severity"`
	Extensions            map[string]interface{} `db:"extensions"`
	ResourceID            uuid.UUID              `db:"resource_id"`
	ResourceTypeID        uuid.UUID              `db:"resource_type_id"`
	NotificationEventType int                    `db:"notification_event_type"`
	AlarmStatus           string                 `db:"alarm_status"`
	Fingerprint           string                 `db:"fingerprint"`
	AlarmSequenceNumber   int64                  `db:"alarm_sequence_number"`
	CreatedAt             time.Time              `db:"created_at"`
}

// TableName returns the name of the table in the database
func (r AlarmEventRecord) TableName() string {
	return "alarm_event_record"
}

// PrimaryKey returns the primary key of the table
func (r AlarmEventRecord) PrimaryKey() string {
	return "alarm_event_record_id"
}

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r AlarmEventRecord) OnConflict() string {
	return ""
}
