/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package models

import (
	"time"

	"github.com/google/uuid"
)

// AlarmDictionary represents the alarm_dictionary table in the database
type AlarmDictionary struct {
	AlarmDictionaryID            uuid.UUID `db:"alarm_dictionary_id"`
	AlarmDictionaryVersion       string    `db:"alarm_dictionary_version"`
	AlarmDictionarySchemaVersion string    `db:"alarm_dictionary_schema_version"`
	EntityType                   string    `db:"entity_type"`
	Vendor                       string    `db:"vendor"`
	ManagementInterfaceID        []string  `db:"management_interface_id"`
	PKNotificationField          []string  `db:"pk_notification_field"`

	NodeClusterTypeID uuid.UUID  `db:"node_cluster_type_id"`
	DataSourceID      uuid.UUID  `db:"data_source_id"`
	GenerationID      int        `db:"generation_id"`
	CreatedAt         *time.Time `db:"created_at"`
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
