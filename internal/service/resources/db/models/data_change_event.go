package models

import (
	"time"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

// Interface compile enforcement
var _ db.Model = (*DataChangeEvent)(nil)

// DataChangeEvent represents a record in the data_change_event table.
type DataChangeEvent struct {
	DataChangeID *uuid.UUID `db:"data_change_id"`
	ObjectType   string     `db:"object_type"`
	ObjectID     uuid.UUID  `db:"object_id"`
	ParentID     *uuid.UUID `db:"parent_id"`
	BeforeState  *string    `db:"before_state"`
	AfterState   *string    `db:"after_state"`
	SequenceID   *int       `db:"sequence_id"`
	CreatedAt    *time.Time `db:"created_at"`
}

// TableName returns the table name associated to this model
func (r DataChangeEvent) TableName() string {
	return "data_change_event"
}

// PrimaryKey returns the primary key column associated to this model
func (r DataChangeEvent) PrimaryKey() string { return "data_change_id" }

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r DataChangeEvent) OnConflict() string {
	return ""
}
