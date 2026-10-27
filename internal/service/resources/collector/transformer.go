/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/api/generated"
)

// NotificationTransformer is responsible for transforming notification to add subscription-specific details before
// being published to the subscriber.
type NotificationTransformer struct {
}

// NewNotificationTransformer creates a new NotificationTransformer
func NewNotificationTransformer() *NotificationTransformer {
	return &NotificationTransformer{}
}

// Transform provides a mechanism to augment a notification with subscription-specific information.  If no
// transformation is possible or necessary, then the original notification is returned.
func (t *NotificationTransformer) Transform(subscription *notifier.SubscriptionInfo, notification *notifier.Notification) (*notifier.Notification, error) {
	if subscription.ConsumerSubscriptionID == nil {
		return notification, nil
	}

	payload, ok := notification.Payload.(generated.InventoryChangeNotification)
	if !ok {
		return nil, fmt.Errorf("notification payload is not of type InventoryChangeNotification")
	}

	// Shallow copy to ensure each subscriber gets a copy of the payload with its own id
	clone := payload
	// The notifier carries consumerSubscriptionId as a free-form string (M-10), but
	// this service's public API uses a UUID. Parse back to a UUID; consumerSubscriptionId
	// is optional, so on a parse failure deliver without it (set nil) rather than skipping
	// the subscriber, which could cause the notification to be dropped entirely.
	if parsed, err := uuid.Parse(*subscription.ConsumerSubscriptionID); err != nil {
		slog.Warn("consumerSubscriptionId is not a valid UUID; delivering notification without it",
			slog.String("subscription", subscription.SubscriptionID.String()),
			slog.String("consumerSubscriptionId", *subscription.ConsumerSubscriptionID), slog.Any("error", err))
		clone.ConsumerSubscriptionId = nil
	} else {
		clone.ConsumerSubscriptionId = &parsed
	}
	result := *notification
	result.Payload = clone
	return &result, nil
}
