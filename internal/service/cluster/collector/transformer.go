/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"fmt"

	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
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

	payload, ok := notification.Payload.(generated.ClusterChangeNotification)
	if !ok {
		return nil, fmt.Errorf("notification payload is not of type ClusterChangeNotification")
	}

	// Shallow copy to ensure each subscriber gets a copy of the payload with its own id
	clone := payload
	clone.ConsumerSubscriptionId = subscription.ConsumerSubscriptionID
	result := *notification
	result.Payload = clone
	return &result, nil
}
