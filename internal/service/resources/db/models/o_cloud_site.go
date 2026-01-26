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
var _ db.Model = (*OCloudSite)(nil)

// OCloudSite represents a record in the o_cloud_site table.
// O-RAN.WG6.TS.O2IMS-INTERFACE-R005-v11.00
type OCloudSite struct {
	OCloudSiteID     uuid.UUID              `db:"o_cloud_site_id"`
	GlobalLocationID string                 `db:"global_location_id"`
	Name             string                 `db:"name"`
	Description      string                 `db:"description"`
	Extensions       map[string]interface{} `db:"extensions"`
	DataSourceID     uuid.UUID              `db:"data_source_id"`
	GenerationID     int                    `db:"generation_id"`
	CreatedAt        *time.Time             `db:"created_at"`
}

// TableName returns the table name associated to this model
func (r OCloudSite) TableName() string {
	return "o_cloud_site"
}

// PrimaryKey returns the primary key column associated to this model
func (r OCloudSite) PrimaryKey() string { return "o_cloud_site_id" }

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r OCloudSite) OnConflict() string { return "" }
