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
	ResourceID     *uuid.UUID        `db:"resource_id"`
	Description    string            `db:"description"`
	ResourceTypeID uuid.UUID         `db:"resource_type_id"`
	GlobalAssetID  *string           `db:"global_asset_id"`
	ResourcePoolID uuid.UUID         `db:"resource_pool_id"`
	Extensions     map[string]string `db:"extensions"`
	Groups         *[]string         `db:"groups"`
	Tags           *[]string         `db:"tags"`
	DataSourceID   int               `db:"data_source_id"`
	GenerationID   int               `db:"generation_id"`
	ExternalID     string            `db:"external_id"`
	CreatedAt      time.Time         `db:"created_at"`
}

// TableName returns the table name associated to this model
func (r Resource) TableName() string {
	return "resource"
}

// PrimaryKey returns the primary key column associated to this model
func (r Resource) PrimaryKey() string { return "resource_id" }
