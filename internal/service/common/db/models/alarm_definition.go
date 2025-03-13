/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package models

import (
	"time"

	"github.com/google/uuid"
)

// AlarmDefinition represents the alarm_definition table in the database
type AlarmDefinition struct {
	AlarmDefinitionID     uuid.UUID               `db:"alarm_definition_id"`
	AlarmName             string                  `db:"alarm_name"`
	AlarmLastChange       string                  `db:"alarm_last_change"`
	AlarmChangeType       string                  `db:"alarm_change_type"`
	AlarmDescription      string                  `db:"alarm_description"`
	ProposedRepairActions string                  `db:"proposed_repair_actions"`
	ClearingType          string                  `db:"clearing_type"`
	ManagementInterfaceID []string                `db:"management_interface_id"`
	PKNotificationField   []string                `db:"pk_notification_field"`
	AlarmAdditionalFields *map[string]interface{} `db:"alarm_additional_fields"`

	Severity          string     `db:"severity"`
	AlarmDictionaryID uuid.UUID  `db:"alarm_dictionary_id"`
	CreatedAt         *time.Time `db:"created_at"`
}

// TableName returns the name of the table in the database
func (r AlarmDefinition) TableName() string {
	return "alarm_definition"
}

// PrimaryKey returns the primary key of the table
func (r AlarmDefinition) PrimaryKey() string {
	return "alarm_definition_id"
}

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r AlarmDefinition) OnConflict() string {
	// Used for the upsert operation
	return "unique_alarm"
}
