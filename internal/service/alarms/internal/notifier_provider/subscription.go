package notifier_provider

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	a "github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
)

// Compile time check for interface compliance
var _ notifier.SubscriptionProvider = (*SubscriptionStorageProvider)(nil)

// SubscriptionStorageProvider implements the SubscriptionProvider interface as a means to abstract the concrete
// subscription type out of the Notifier
type SubscriptionStorageProvider struct {
	repository *a.AlarmsRepository
}

// NewSubscriptionStorageProvider creates a new SubscriptionStorageProvider
func NewSubscriptionStorageProvider(repository *a.AlarmsRepository) notifier.SubscriptionProvider {
	return &SubscriptionStorageProvider{
		repository: repository,
	}
}

func (s *SubscriptionStorageProvider) GetSubscriptions(ctx context.Context) ([]notifier.SubscriptionInfo, error) {
	var subscriptions []notifier.SubscriptionInfo

	records, err := s.repository.GetAlarmSubscriptions(ctx)
	if err != nil {
		return []notifier.SubscriptionInfo{}, fmt.Errorf("failed to get all subscriptions: %w", err)
	}
	if len(records) == 0 {
		slog.Info("No subscriptions to notify")
		return subscriptions, nil
	}

	// Convert records to generic subs
	for _, record := range records {
		subscriptions = append(subscriptions, *models.ConvertAlertSubToNotificationSub(&record))
	}

	slog.Info("Found subscriptions to notify", "count", len(subscriptions))
	return subscriptions, nil
}

func (s *SubscriptionStorageProvider) Matches(subscription *notifier.SubscriptionInfo, notification *notifier.Notification) bool {
	// TODO we to need somehow group notifications per subscriber in slight nicer way, see GetNotifications for more
	return subscription.SubscriptionID == notification.NotificationID
}

func (s *SubscriptionStorageProvider) UpdateSubscription(ctx context.Context, subscription *notifier.SubscriptionInfo) error {
	if err := s.repository.UpdateSubscriptionEventCursor(ctx, models.AlarmSubscription{
		SubscriptionID: subscription.SubscriptionID,
		EventCursor:    int64(subscription.EventCursor),
	}); err != nil {
		return fmt.Errorf("update subscription failed for %s: %w", subscription.SubscriptionID, err)
	}

	slog.Info("Subscription cursor updated", "to", subscription.EventCursor)
	return nil
}
