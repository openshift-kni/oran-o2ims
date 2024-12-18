package models

import (
	"time"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

// Interface compile enforcement
var _ db.Model = (*NodeCluster)(nil)

// NodeCluster represents a record in the node_cluster table.
type NodeCluster struct {
	NodeClusterID                  uuid.UUID               `db:"node_cluster_id"` // Non-nil because we always set this from named values
	NodeClusterTypeID              uuid.UUID               `db:"node_cluster_type_id"`
	ClientNodeClusterID            uuid.UUID               `db:"client_node_cluster_id"`
	Name                           string                  `db:"name"`
	Description                    string                  `db:"description"`
	Extensions                     *map[string]interface{} `db:"extensions"`
	ClusterDistributionDescription string                  `db:"cluster_distribution_description"`
	ArtifactResourceID             uuid.UUID               `db:"artifact_resource_id"`
	ClusterResourceGroups          *[]uuid.UUID            `db:"cluster_resource_groups"`
	ExternalID                     string                  `db:"external_id"`
	DataSourceID                   uuid.UUID               `db:"data_source_id"`
	GenerationID                   int                     `db:"generation_id"`
	CreatedAt                      *time.Time              `db:"created_at"`
}

// TableName returns the table name associated to this model
func (r NodeCluster) TableName() string {
	return "node_cluster"
}

// PrimaryKey returns the primary key column associated to this model
func (r NodeCluster) PrimaryKey() string { return "node_cluster_id" }

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r NodeCluster) OnConflict() string { return "" }
