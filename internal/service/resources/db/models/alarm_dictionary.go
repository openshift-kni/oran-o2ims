/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package models

import (
	"time"

	"github.com/google/uuid"
)

// AlarmDictionary is the structure for alarm dictionaries in the resource server (has ResourceTypeID instead of NodeClusterTypeID)
type AlarmDictionary struct {
	AlarmDictionaryID            uuid.UUID  `db:"alarm_dictionary_id"`
	AlarmDictionaryVersion       string     `db:"alarm_dictionary_version"`
	AlarmDictionarySchemaVersion string     `db:"alarm_dictionary_schema_version"`
	EntityType                   string     `db:"entity_type"`
	Vendor                       string     `db:"vendor"`
	ManagementInterfaceID        []string   `db:"management_interface_id"`
	PKNotificationField          []string   `db:"pk_notification_field"`
	ResourceTypeID               uuid.UUID  `db:"resource_type_id"`
	CreatedAt                    *time.Time `db:"created_at"`
}

// TableName returns the name of the table in the database
func (r AlarmDictionary) TableName() string {
	return "alarm_dictionary"
}

// PrimaryKey returns the primary key of the table
func (r AlarmDictionary) PrimaryKey() string {
	return "alarm_dictionary_id"
}

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r AlarmDictionary) OnConflict() string {
	return ""
}
