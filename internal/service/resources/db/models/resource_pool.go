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
var _ db.Model = (*ResourcePool)(nil)

// ResourcePool represents a record in the resource_pool table.
type ResourcePool struct {
	ResourcePoolID   uuid.UUID              `db:"resource_pool_id"`   // Non-nil because we always set this from named values
	GlobalLocationID uuid.UUID              `db:"global_location_id"` // Deprecated by O-RAN.WG6.TS.O2IMS-INTERFACE-R005-v11.00
	Name             string                 `db:"name"`
	Description      string                 `db:"description"`
	OCloudID         uuid.UUID              `db:"o_cloud_id"`      // Deprecated by O-RAN.WG6.TS.O2IMS-INTERFACE-R005-v11.00
	Location         *string                `db:"location"`        // Deprecated by O-RAN.WG6.TS.O2IMS-INTERFACE-R005-v11.00
	OCloudSiteID     *uuid.UUID             `db:"o_cloud_site_id"` // O-RAN.WG6.TS.O2IMS-INTERFACE-R005-v11.00
	Extensions       map[string]interface{} `db:"extensions"`
	DataSourceID     uuid.UUID              `db:"data_source_id"`
	GenerationID     int                    `db:"generation_id"`
	ExternalID       string                 `db:"external_id"`
	CreatedAt        *time.Time             `db:"created_at"`
}

// TableName returns the table name associated to this model
func (r ResourcePool) TableName() string {
	return "resource_pool"
}

// PrimaryKey returns the primary key column associated to this model
func (r ResourcePool) PrimaryKey() string { return "resource_pool_id" }

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r ResourcePool) OnConflict() string { return "" }
