package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
)

// AlarmEventRecord represents a record in the alarm_event_record table.
type AlarmEventRecord struct {
	AlarmEventRecordID    uuid.UUID                             `db:"alarm_event_record_id" json:"alarm_event_record_id"`
	AlarmDefinitionID     *uuid.UUID                            `db:"alarm_definition_id" json:"alarm_definition_id,omitempty"` // nullable since ACM may not provide the cluster ID. please manually track them and let ACM know about this.
	ProbableCauseID       *uuid.UUID                            `db:"probable_cause_id" json:"probable_cause_id,omitempty"`     // nullable since ACM may not provide the cluster ID. please manually track them and let ACM know about this.
	AlarmRaisedTime       time.Time                             `db:"alarm_raised_time" json:"alarm_raised_time"`
	AlarmChangedTime      *time.Time                            `db:"alarm_changed_time" json:"alarm_changed_time,omitempty"`
	AlarmClearedTime      *time.Time                            `db:"alarm_cleared_time" json:"alarm_cleared_time,omitempty"`
	AlarmAcknowledgedTime *time.Time                            `db:"alarm_acknowledged_time" json:"alarm_acknowledged_time,omitempty"`
	AlarmAcknowledged     bool                                  `db:"alarm_acknowledged" json:"alarm_acknowledged"`
	PerceivedSeverity     generated.PerceivedSeverity           `db:"perceived_severity" json:"perceived_severity"`
	Extensions            map[string]string                     `db:"extensions" json:"extensions"`
	ObjectID              *uuid.UUID                            `db:"object_id" json:"object_id,omitempty"`           // nullable since ACM may not provide the cluster ID. please manually track them and let ACM know about this.
	ObjectTypeID          *uuid.UUID                            `db:"object_type_id" json:"object_type_id,omitempty"` // nullable since ACM may not provide the cluster ID. please manually track them and let ACM know about this.
	NotificationEventType generated.AlarmSubscriptionInfoFilter `db:"notification_event_type" json:"notification_event_type"`
	AlarmStatus           string                                `db:"alarm_status" json:"alarm_status"`
	Fingerprint           string                                `db:"fingerprint" json:"fingerprint"`
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
	return "unique_fingerprint_alarm_raised_time"
}
