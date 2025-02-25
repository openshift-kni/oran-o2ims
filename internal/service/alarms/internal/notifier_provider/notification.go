package notifier_provider

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"

	"github.com/google/uuid"

	a "github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
)

// Compile time check for interface compliance
var _ notifier.NotificationProvider = (*NotificationStorageProvider)(nil)

// NotificationStorageProvider implements the NotificationProvider interface as a means to abstract the concrete
// notification type out of the Notifier
type NotificationStorageProvider struct {
	repository    a.AlarmRepositoryInterface
	globalCloudID uuid.UUID
}

// NewNotificationStorageProvider creates a new NotificationProvider
func NewNotificationStorageProvider(repository a.AlarmRepositoryInterface, globalCloudID uuid.UUID) notifier.NotificationProvider {
	return &NotificationStorageProvider{
		repository:    repository,
		globalCloudID: globalCloudID,
	}
}

// GetNotifications return all notations from outbox
func (n *NotificationStorageProvider) GetNotifications(ctx context.Context) ([]notifier.Notification, error) {
	var notifications []notifier.Notification
	records, err := n.repository.GetAllAlarmsDataChange(ctx)
	if err != nil {
		return notifications, fmt.Errorf("failed to get alarms for subscription: %w", err)
	}

	for _, record := range records {
		notification, err := models.DataChangeEventToNotification(&record, n.globalCloudID)
		if err != nil {
			slog.Warn("failed to convert alarm event to notification", "err", err)
			continue
		}
		if notification != nil {
			notifications = append(notifications, *notification)
		}
	}

	if len(notifications) == 0 {
		slog.Info("No notifications")
	}

	return notifications, nil
}

func (n *NotificationStorageProvider) DeleteNotification(ctx context.Context, dataChangeId uuid.UUID) error {
	if err := n.repository.DeleteAlarmsDataChange(ctx, dataChangeId); err != nil {
		return fmt.Errorf("failed to delete alarms notifications from outbox : %w", err)
	}

	return nil
}
