package notifier_provider

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	a "github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
)

// Compile time check for interface compliance
var _ notifier.NotificationProvider = (*NotificationStorageProvider)(nil)

// NotificationStorageProvider implements the NotificationProvider interface as a means to abstract the concrete
// notification type out of the Notifier
type NotificationStorageProvider struct {
	repository    *a.AlarmsRepository
	globalCloudID uuid.UUID
}

// NewNotificationStorageProvider creates a new NotificationProvider
func NewNotificationStorageProvider(repository *a.AlarmsRepository) notifier.NotificationProvider {
	return &NotificationStorageProvider{
		repository: repository,
	}
}

func (n *NotificationStorageProvider) GetNotifications(ctx context.Context) ([]notifier.Notification, error) {
	var notifications []notifier.Notification
	// Get all subscriptions
	subscriptions, err := n.repository.GetAlarmSubscriptions(ctx)
	if err != nil {
		return notifications, fmt.Errorf("failed to get all subscriptions for alarms: %w", err)
	}
	if len(subscriptions) == 0 {
		slog.Info("No subscriptions to notify")
		return notifications, nil
	}
	slog.Info("Found subscriptions to notify", "subscription count", len(subscriptions))
	// Find the min event cursor, this will decide the total events that needs to be there ignoring the filter
	alarms, err := n.repository.GetAlarmsForSubscription(ctx, *getMinSubscription(subscriptions))
	if err != nil {
		return notifications, fmt.Errorf("failed to get alarms for subscription: %w", err)
	}

	// TODO: Refactor to use a callback interface for subscription-specific payload modifications
	// rather than cloning notifications for each subscription. This would improve efficiency
	// and make the notification system more generic.
	// As workaround for now using notificationID as subscriberID to link them with Match interface.
	for _, sub := range subscriptions {
		var subNotification []notifier.Notification
		for _, alarm := range alarms {
			if alarm.AlarmSequenceNumber > sub.EventCursor {
				if sub.Filter != nil && alarm.NotificationEventType == *sub.Filter {
					continue
				}
				subNotification = append(subNotification, notifier.Notification{
					NotificationID: sub.SubscriptionID,
					SequenceID:     int(alarm.AlarmSequenceNumber),
					Payload:        models.ConvertAlarmEventRecordModelToAlarmEventNotification(alarm, sub, n.globalCloudID),
				})
			}
		}
		if len(subNotification) > 0 {
			slog.Info("Subscription will receive notifications", "subscriptionID", sub.SubscriptionID, "notification count", len(subNotification))
		}

		notifications = append(notifications, subNotification...)
	}

	if len(notifications) == 0 {
		slog.Info("No notifications for any of the subscriptions")
	}

	return notifications, nil
}

// getMinSubscription get the min cursor and set filer to nil - this will retrieve the alarms that are greater and ignoring the filter
func getMinSubscription(subscriptions []models.AlarmSubscription) *models.AlarmSubscription {
	minSub := subscriptions[0]
	for i := 1; i < len(subscriptions); i++ {
		if subscriptions[i].EventCursor < minSub.EventCursor {
			minSub = subscriptions[i]
		}
	}
	minSub.Filter = nil
	return &minSub // return a copy
}

func (n *NotificationStorageProvider) DeleteNotification(_ context.Context, _ uuid.UUID) error {
	return nil
}
