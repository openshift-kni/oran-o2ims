package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// sendNotification sends the inventory change notification to the subscriber
func sendNotification(ctx context.Context, client *http.Client, url string, event Notification) error {
	body, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("failed to marshal notification payload: %w", err)
	}

	buffer := bytes.NewBuffer(body)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, buffer)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("notification failed: %v", response.StatusCode)
	}

	return nil
}

// processEvent attempts to send a notification to the subscriber.  It does so until it succeeds or the maximum
// number of retries has been reached.  When it completes or timeout it sends a job completion event and signals back
// to the worker that this current event is complete
func processEvent(ctx context.Context, logger *slog.Logger, client *http.Client,
	completionChannel chan *SubscriptionJobComplete, event Notification, subscriptionID uuid.UUID, url string) {
	logger.Info("processing data change event",
		"notificationID", event.NotificationID, "sequenceID", event.SequenceID)

	var err error = nil
	delay := retryDelay
	for i := 0; i < maxRetries; i++ {
		err = sendNotification(ctx, client, url, event)
		if err == nil {
			break
		}

		logger.Warn("failed to send notification", "error", err,
			"notificationID", event.NotificationID, "sequenceID", event.SequenceID, "delay", delay)

		select {
		case <-ctx.Done():
			logger.Warn("context canceled while sending notification",
				"notificationID", event.NotificationID, "sequenceID", event.SequenceID)
			break
		case <-time.After(delay):
			delay *= 2
			logger.Debug("retrying notification",
				"notificationID", event.NotificationID, "sequenceID", event.SequenceID)
		}
	}

	if err != nil {
		logger.Error("error sending notification; retries exceeded", "error", err,
			"notificationID", event.NotificationID, "sequenceID", event.SequenceID)
		// TODO: If we were able to send this one then we are not likely to be able to send any
		//  of the others so perhaps we should purge our queue, or enter a longer backoff period.
	} else {
		logger.Info("notification sent",
			"notificationID", event.NotificationID, "sequenceID", event.SequenceID)
	}

	completionChannel <- &SubscriptionJobComplete{
		subscriptionID: subscriptionID,
		notificationID: event.NotificationID,
		sequenceID:     event.SequenceID,
	}
}
