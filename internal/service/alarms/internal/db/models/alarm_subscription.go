/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package models

import (
	"time"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"

	"github.com/google/uuid"
)

// AlarmSubscription represents the alarm_subscription_info table in the database
type AlarmSubscription struct {
	SubscriptionID         uuid.UUID                              `db:"subscription_id"`
	ConsumerSubscriptionID *uuid.UUID                             `db:"consumer_subscription_id"`
	Filter                 *generated.AlarmSubscriptionInfoFilter `db:"filter"`
	Callback               string                                 `db:"callback"`

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
