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

var _ db.Model = (*ClusterResourceGroup)(nil)

// ClusterResourceGroup is an inventory group within one node cluster.
type ClusterResourceGroup struct {
	ClusterResourceGroupID uuid.UUID               `db:"cluster_resource_group_id"`
	NodeClusterID          uuid.UUID               `db:"node_cluster_id"`
	Name                   string                  `db:"name"`
	Description            string                  `db:"description"`
	Extensions             *map[string]interface{} `db:"extensions"`
	ClusterResources       []uuid.UUID             `db:"cluster_resources"`
	DataSourceID           uuid.UUID               `db:"data_source_id"`
	GenerationID           int                     `db:"generation_id"`
	CreatedAt              *time.Time              `db:"created_at"`
}

func (ClusterResourceGroup) TableName() string  { return "cluster_resource_group" }
func (ClusterResourceGroup) PrimaryKey() string { return "cluster_resource_group_id" }
func (ClusterResourceGroup) OnConflict() string { return "" }
