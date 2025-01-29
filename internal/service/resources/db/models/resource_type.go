package models

import (
	"time"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

// Interface compile enforcement
var _ db.Model = (*ResourceType)(nil)

type ResourceKind string
type ResourceClass string

const (
	ResourceClassCompute    ResourceClass = "COMPUTE"
	ResourceClassNetworking ResourceClass = "NETWORKING"
	ResourceClassStorage    ResourceClass = "STORAGE"
	ResourceClassUndefined  ResourceClass = "UNDEFINED"
)

const (
	ResourceKindPhysical  ResourceKind = "PHYSICAL"
	ResourceKindLogical   ResourceKind = "LOGICAL"
	ResourcekindUndefined ResourceKind = "UNDEFINED"
)

// ResourceType represents a record in the resource_type table.
type ResourceType struct {
	ResourceTypeID uuid.UUID              `db:"resource_type_id"` // Non-nil because we always set this from named values
	Name           string                 `db:"name"`
	Description    string                 `db:"description"`
	Vendor         string                 `db:"vendor"`
	Model          string                 `db:"model"`
	Version        string                 `db:"version"`
	ResourceKind   ResourceKind           `db:"resource_kind"`
	ResourceClass  ResourceClass          `db:"resource_class"`
	Extensions     map[string]interface{} `db:"extensions"`
	DataSourceID   uuid.UUID              `db:"data_source_id"`
	GenerationID   int                    `db:"generation_id"`
	CreatedAt      *time.Time             `db:"created_at"`
}

// TableName returns the table name associated to this model
func (r ResourceType) TableName() string {
	return "resource_type"
}

// PrimaryKey returns the primary key column associated to this model
func (r ResourceType) PrimaryKey() string { return "resource_type_id" }

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r ResourceType) OnConflict() string { return "" }
