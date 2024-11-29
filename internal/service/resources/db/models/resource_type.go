package models

import (
	"time"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

// Interface compile enforcement
var _ db.Model = (*ResourceType)(nil)

// ResourceType represents a record in the resource_type table.
type ResourceType struct {
	ResourceTypeID *uuid.UUID        `db:"resource_type_id"`
	Name           string            `db:"name"`
	Description    string            `db:"description"`
	Vendor         string            `db:"vendor"`
	Model          string            `db:"model"`
	Version        string            `db:"version"`
	ResourceKind   int               `db:"resource_kind"`
	ResourceClass  int               `db:"resource_class"`
	Extensions     map[string]string `db:"extensions"`
	DataSourceID   int               `db:"data_source_id"`
	GenerationID   int               `db:"generation_id"`
	CreatedAt      time.Time         `db:"created_at"`
}

// TableName returns the table name associated to this model
func (r ResourceType) TableName() string {
	return "resource_type"
}

// PrimaryKey returns the primary key column associated to this model
func (r ResourceType) PrimaryKey() string { return "resource_type_id" }
