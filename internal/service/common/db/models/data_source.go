package models

import (
	"time"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

// Interface compile enforcement
var _ db.Model = (*DataSource)(nil)

// DataSource represents a record in the data_source table.
type DataSource struct {
	DataSourceID *uuid.UUID `db:"data_source_id"`
	Name         string     `db:"name"`
	GenerationID int        `db:"generation_id"`
	LastSnapshot *time.Time `db:"last_snapshot"`
	CreatedAt    *time.Time `db:"created_at"`
}

// TableName returns the table name associated to this model
func (r DataSource) TableName() string {
	return "data_source"
}

// PrimaryKey returns the primary key column associated to this model
func (r DataSource) PrimaryKey() string { return "id" }

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r DataSource) OnConflict() string {
	return ""
}
