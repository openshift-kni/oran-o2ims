package models

import (
	"time"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

// Interface compile enforcement
var _ db.Model = (*Subscription)(nil)

// Subscription represents a record in the subscription table.
type Subscription struct {
	SubscriptionID         *uuid.UUID `db:"subscription_id"`
	ConsumerSubscriptionID *string    `db:"consumer_subscription_id"`
	Filter                 *string    `db:"filter"`
	Callback               string     `db:"callback"`
	EventCursor            int        `db:"event_cursor"`
	CreatedAt              time.Time  `db:"created_at"`
}

// TableName returns the table name associated to this model
func (r Subscription) TableName() string {
	return "subscription"
}

// PrimaryKey returns the primary key column associated to this model
func (r Subscription) PrimaryKey() string { return "subscription_id" }
