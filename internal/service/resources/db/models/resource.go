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
var _ db.Model = (*Resource)(nil)

// Resource represents a record in the resource table.
type Resource struct {
	ResourceID     uuid.UUID              `db:"resource_id"` // Non-nil because we always set this from named values
	Description    string                 `db:"description"`
	ResourceTypeID uuid.UUID              `db:"resource_type_id"`
	GlobalAssetID  *string                `db:"global_asset_id"`
	ResourcePoolID uuid.UUID              `db:"resource_pool_id"`
	Extensions     map[string]interface{} `db:"extensions"`
	Groups         *[]string              `db:"groups"`
	Tags           *[]string              `db:"tags"`
	DataSourceID   uuid.UUID              `db:"data_source_id"`
	GenerationID   int                    `db:"generation_id"`
	ExternalID     string                 `db:"external_id"`
	CreatedAt      *time.Time             `db:"created_at"`
}

// TableName returns the table name associated to this model
func (r Resource) TableName() string {
	return "resource"
}

// PrimaryKey returns the primary key column associated to this model
func (r Resource) PrimaryKey() string { return "resource_id" }

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r Resource) OnConflict() string { return "" }
