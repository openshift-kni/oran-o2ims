package models

import (
	"time"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

// Interface compile enforcement
var _ db.Model = (*NodeClusterType)(nil)

// NodeClusterType represents a record in the node_cluster_type table.
type NodeClusterType struct {
	NodeClusterTypeID uuid.UUID               `db:"node_cluster_type_id"` // Non-nil because we always set this from named values
	Name              string                  `db:"name"`
	Description       string                  `db:"description"`
	Extensions        *map[string]interface{} `db:"extensions"`
	DataSourceID      uuid.UUID               `db:"data_source_id"`
	GenerationID      int                     `db:"generation_id"`
	CreatedAt         *time.Time              `db:"created_at"`
}

// TableName returns the table name associated to this model
func (r NodeClusterType) TableName() string {
	return "node_cluster_type"
}

// PrimaryKey returns the primary key column associated to this model
func (r NodeClusterType) PrimaryKey() string { return "node_cluster_type_id" }

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r NodeClusterType) OnConflict() string { return "" }
