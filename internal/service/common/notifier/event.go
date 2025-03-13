/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"syscall"
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
	defer func(Body io.ReadCloser) {
		if err := Body.Close(); err != nil {
			slog.Error("failed to close response body", "error", err)
		}
	}(response.Body)

	if response.StatusCode > http.StatusNoContent {
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

	_ = callUrl(ctx, logger, client, url, event)

	completionChannel <- &SubscriptionJobComplete{
		subscriptionID: subscriptionID,
		notificationID: event.NotificationID,
		sequenceID:     event.SequenceID,
	}
}

// callUrl with retry
func callUrl(ctx context.Context, logger *slog.Logger, client *http.Client, url string, event Notification) error {
	var err error = nil
	delay := retryDelay
	for attempt := 0; attempt < maxRetries; attempt++ {
		err = sendNotification(ctx, client, url, event)
		if err == nil {
			break
		}

		if nonRetryErr := isRetryableError(err); nonRetryErr != nil {
			msg := "error sending notification; non retryable error"
			logger.Error(msg, "error", nonRetryErr,
				"notificationID", event.NotificationID, "sequenceID", event.SequenceID)
			return fmt.Errorf("%s: %w", msg, err)
		}

		logger.Warn("failed to send notification", "error", err,
			"notificationID", event.NotificationID, "sequenceID", event.SequenceID, "delay", delay.String(), "attemptsRemaining", maxRetries-attempt-1)

		if attempt < maxRetries-1 { // Skip delay for final attempt since no further retries will occur
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
	}

	if err != nil {
		logger.Error("error sending notification; retries exceeded", "error", err,
			"notificationID", event.NotificationID, "sequenceID", event.SequenceID)
		// TODO: If we were able to send this one then we are not likely to be able to send any
		//  of the others so perhaps we should purge our queue, or enter a longer backoff period.
		return fmt.Errorf("error sending notification; retries exceeded: %w", err)
	} else {
		logger.Info("notification sent",
			"notificationID", event.NotificationID, "sequenceID", event.SequenceID)
	}

	return nil
}

// isRetryableError returns nil if error is retryable, otherwise returns an error explaining why it can't be retried
func isRetryableError(err error) error {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return fmt.Errorf("host not found: %w", err)
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("connection refused: %w", err)
	}
	return nil
}
