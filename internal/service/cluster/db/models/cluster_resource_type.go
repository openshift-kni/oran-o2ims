package models

import (
	"time"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

// Interface compile enforcement
var _ db.Model = (*ClusterResourceType)(nil)

// ClusterResourceType represents a record in the cluster_resource_type table.
type ClusterResourceType struct {
	ClusterResourceTypeID uuid.UUID               `db:"cluster_resource_type_id"` // Non-nil because we always set this from named values
	Name                  string                  `db:"name"`
	Description           string                  `db:"description"`
	Extensions            *map[string]interface{} `db:"extensions"`
	DataSourceID          uuid.UUID               `db:"data_source_id"`
	GenerationID          int                     `db:"generation_id"`
	CreatedAt             *time.Time              `db:"created_at"`
}

// TableName returns the table name associated to this model
func (r ClusterResourceType) TableName() string {
	return "cluster_resource_type"
}

// PrimaryKey returns the primary key column associated to this model
func (r ClusterResourceType) PrimaryKey() string { return "cluster_resource_type_id" }

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r ClusterResourceType) OnConflict() string { return "" }
