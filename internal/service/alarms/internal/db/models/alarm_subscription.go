package models

import (
	"time"

	"github.com/google/uuid"
)

// AlarmSubscription represents the alarm_subscription_info table in the database
type AlarmSubscription struct {
	SubscriptionID         uuid.UUID  `db:"subscription_id"`
	ConsumerSubscriptionID *uuid.UUID `db:"consumer_subscription_id"`
	Filter                 *string    `db:"filter"`
	Callback               string     `db:"callback"`

	EventCursor int64     `db:"event_cursor"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

// TableName returns the name of the table in the database
func (r AlarmSubscription) TableName() string {
	return "alarm_subscription_info"
}

// PrimaryKey returns the primary key of the table
func (r AlarmSubscription) PrimaryKey() string {
	return "subscription_id"
}

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r AlarmSubscription) OnConflict() string {
	return ""
}
