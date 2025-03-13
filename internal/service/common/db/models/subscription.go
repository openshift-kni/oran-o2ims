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
var _ db.Model = (*Subscription)(nil)

// Subscription represents a record in the subscription table.
type Subscription struct {
	SubscriptionID         *uuid.UUID `db:"subscription_id"`
	ConsumerSubscriptionID *uuid.UUID `db:"consumer_subscription_id"`
	Filter                 *string    `db:"filter"`
	Callback               string     `db:"callback"`
	// EventCursor holds the SequenceID of the last processed event.  Sequences start at 1 so we initialize this to 0.
	EventCursor int        `db:"event_cursor"`
	CreatedAt   *time.Time `db:"created_at"`
}

// TableName returns the table name associated to this model
func (r Subscription) TableName() string {
	return "subscription"
}

// PrimaryKey returns the primary key column associated to this model
func (r Subscription) PrimaryKey() string { return "subscription_id" }

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r Subscription) OnConflict() string { return "" }
