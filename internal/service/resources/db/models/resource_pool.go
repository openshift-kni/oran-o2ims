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
	ResourcePoolID   uuid.UUID          `db:"resource_pool_id"` // Non-nil because we always set this from named values
	GlobalLocationID string             `db:"global_location_id"`
	Name             string             `db:"name"`
	Description      string             `db:"description"`
	OCloudID         uuid.UUID          `db:"o_cloud_id"`
	Location         *string            `db:"location"`
	Extensions       *map[string]string `db:"extensions"`
	DataSourceID     uuid.UUID          `db:"data_source_id"`
	GenerationID     int                `db:"generation_id"`
	ExternalID       string             `db:"external_id"`
	CreatedAt        *time.Time         `db:"created_at"`
}

// TableName returns the table name associated to this model
func (r ResourcePool) TableName() string {
	return "resource_pool"
}

// PrimaryKey returns the primary key column associated to this model
func (r ResourcePool) PrimaryKey() string { return "resource_pool_id" }

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r ResourcePool) OnConflict() string { return "" }
