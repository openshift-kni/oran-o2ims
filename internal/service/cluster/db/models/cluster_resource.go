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
var _ db.Model = (*ClusterResource)(nil)

// ClusterResource represents a record in the cluster_resource table.
type ClusterResource struct {
	ClusterResourceID     uuid.UUID               `db:"cluster_resource_id"` // Non-nil because we always set this from named values
	ClusterResourceTypeID uuid.UUID               `db:"cluster_resource_type_id"`
	Name                  string                  `db:"name"`
	NodeClusterID         *uuid.UUID              `db:"node_cluster_id"`
	NodeClusterName       string                  `db:"node_cluster_name"`
	Description           string                  `db:"description"`
	Extensions            *map[string]interface{} `db:"extensions"`
	ArtifactResourceIDs   *[]uuid.UUID            `db:"artifact_resource_ids"`
	ResourceID            uuid.UUID               `db:"resource_id"`
	ExternalID            string                  `db:"external_id"`
	DataSourceID          uuid.UUID               `db:"data_source_id"`
	GenerationID          int                     `db:"generation_id"`
	CreatedAt             *time.Time              `db:"created_at"`
}

// TableName returns the table name associated to this model
func (r ClusterResource) TableName() string {
	return "cluster_resource"
}

// PrimaryKey returns the primary key column associated to this model
func (r ClusterResource) PrimaryKey() string { return "cluster_resource_id" }

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r ClusterResource) OnConflict() string { return "" }

// ClusterResourceIDs represents the data returned in a customized query to return the list of ClusterResource ID values
// associated to each NodeCluster
type ClusterResourceIDs struct {
	NodeClusterID      uuid.UUID   `db:"node_cluster_id"`
	ClusterResourceIDs []uuid.UUID `db:"cluster_resource_ids"`
}

// TableName returns the table name associated to this model
func (r ClusterResourceIDs) TableName() string {
	return "cluster_resource"
}

// PrimaryKey returns the primary key column associated to this model
func (r ClusterResourceIDs) PrimaryKey() string { return "cluster_resource_id" }

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r ClusterResourceIDs) OnConflict() string { return "" }
