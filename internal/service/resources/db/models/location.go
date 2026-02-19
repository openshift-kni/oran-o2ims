/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package models

import (
	"time"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

// Interface compile enforcement
var _ db.Model = (*Location)(nil)

// Location represents a record in the location table.
// O-RAN.WG6.TS.O2IMS-INTERFACE-R005-v11.00
type Location struct {
	GlobalLocationID string                   `db:"global_location_id"`
	Name             string                   `db:"name"`
	Description      string                   `db:"description"`
	Coordinate       map[string]interface{}   `db:"coordinate"`
	CivicAddress     []map[string]interface{} `db:"civic_address"`
	Address          *string                  `db:"address"`
	Extensions       map[string]interface{}   `db:"extensions"`
	DataSourceID     uuid.UUID                `db:"data_source_id"`
	GenerationID     int                      `db:"generation_id"`
	CreatedAt        *time.Time               `db:"created_at"`
}

// TableName returns the table name associated to this model
func (r Location) TableName() string {
	return "location"
}

// PrimaryKey returns the primary key column associated to this model
func (r Location) PrimaryKey() string { return "global_location_id" }

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r Location) OnConflict() string { return "" }
