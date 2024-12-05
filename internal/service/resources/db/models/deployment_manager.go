package models

import (
	"time"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

// Interface compile enforcement
var _ db.Model = (*DeploymentManager)(nil)

// DeploymentManager represents a record in the deployment_manager table.
type DeploymentManager struct {
	ClusterID    *uuid.UUID         `db:"cluster_id"`
	Name         string             `db:"name"`
	Description  string             `db:"description"`
	OCloudID     uuid.UUID          `db:"o_cloud_id"`
	URL          string             `db:"url"`
	Locations    []string           `db:"locations"`
	Capabilities map[string]string  `db:"capabilities"`
	CapacityInfo map[string]string  `db:"capacity_info"`
	Extensions   *map[string]string `db:"extensions"`
	DataSourceID int                `db:"data_source_id"`
	GenerationID int                `db:"generation_id"`
	ExternalID   string             `db:"external_id"`
	CreatedAt    time.Time          `db:"created_at"`
}

// TableName returns the table name associated to this model
func (r DeploymentManager) TableName() string {
	return "deployment_manager"
}

// PrimaryKey returns the primary key column associated to this model
func (r DeploymentManager) PrimaryKey() string { return "cluster_id" }

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r DeploymentManager) OnConflict() string { return "" }
