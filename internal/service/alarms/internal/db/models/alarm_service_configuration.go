/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package models

import (
	"time"

	"github.com/google/uuid"
)

// ServiceConfiguration represents the alarm_service_configuration table in the database
type ServiceConfiguration struct {
	ID              uuid.UUID         `db:"id"`
	RetentionPeriod int               `db:"retention_period"`
	Extensions      map[string]string `db:"extensions"`

	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// TableName returns the name of the table in the database
func (r ServiceConfiguration) TableName() string {
	return "alarm_service_configuration"
}

// PrimaryKey returns the primary key of the table
func (r ServiceConfiguration) PrimaryKey() string {
	return "id"
}

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r ServiceConfiguration) OnConflict() string {
	return ""
}
