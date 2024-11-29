package repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

// Compile time check for interface compliance
var _ notifier.NotificationProvider = (*NotificationStorageProvider)(nil)

// NotificationStorageProvider implements the NotificationProvider interface as a means to abstract the concrete
// notification type out of the Notifier
type NotificationStorageProvider struct {
	repository *ResourcesRepository
}

// NewNotificationStorageProvider creates a new NotificationProvider
func NewNotificationStorageProvider(repository *ResourcesRepository) notifier.NotificationProvider {
	return &NotificationStorageProvider{
		repository: repository,
	}
}

// GetNotifications returns the list of notifications persisted to the database
func (p *NotificationStorageProvider) GetNotifications(ctx context.Context) ([]notifier.Notification, error) {
	records, err := p.repository.GetDataChangeEvents(ctx)
	if err != nil {
		return []notifier.Notification{}, fmt.Errorf("failed to get data change events: %w", err)
	}

	var notifications []notifier.Notification
	for _, record := range records {
		notifications = append(notifications, *models.DataChangeEventToNotification(&record))
	}

	return notifications, nil
}

// DeleteNotification deletes a notification.  This should be invoked when the notifier has ensured
// that the notification has been seen by all subscriptions.
func (p *NotificationStorageProvider) DeleteNotification(ctx context.Context, notificationID uuid.UUID) error {
	_, err := p.repository.DeleteDataChangeEvent(ctx, notificationID)
	if err != nil {
		return fmt.Errorf("failed to delete notification %s: %w", notificationID, err)
	}
	return nil
}
