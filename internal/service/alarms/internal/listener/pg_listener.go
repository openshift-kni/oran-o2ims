/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package listener

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/repo"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/listener"
)

// ListenForAlarmsPgChannels registers the channels with their handlers, and starts the catch-up routine.
func ListenForAlarmsPgChannels(ctx context.Context, pool *pgxpool.Pool, a *api.AlarmsServer) error {
	// Initialize the generic listener manager.
	lm := listener.NewListenerManager(pool)

	// Register the outbox notification listener.
	lm.RegisterListener(
		// Channel name specified in pg_notify call
		"alarm_event_record_outbox_queued",
		// Function called after a notification is received,
		func(ctx context.Context, pgNotification *pgconn.Notification) error {
			return processOutboxNotification(ctx, pool, a, "primary")
		},
		// Catch-up function
		func(ctx context.Context) error {
			return processOutboxNotification(ctx, pool, a, "backup")
		},
		// Catch-up interval
		15*time.Minute,
	)

	// Register more listener function with optional catchup + interval as needed

	// Start all registered listeners.
	if err := lm.StartListeners(ctx); err != nil {
		return fmt.Errorf("failed to start pg listener: %w", err)
	}

	// Block until the context is canceled.
	<-ctx.Done()
	lm.Wait()
	return nil
}

// processOutboxNotification retrieve and send notification
func processOutboxNotification(ctx context.Context, pool *pgxpool.Pool, a *api.AlarmsServer, caller string) error {
	dataChangeEvents, err := repo.ClaimDataChangeEvent(pool, ctx)
	if err != nil {
		return fmt.Errorf("failed to claim notifications: %w", err)
	}

	if len(dataChangeEvents) > 0 && caller == "backup" {
		slog.Warn("outbox is expected to empty but found at least one data change event when running a backup loop")
	}

	// Finally construct notifications from data change events
	notifications := make([]notifier.Notification, 0)
	for _, dataChangeEvent := range dataChangeEvents {
		notification, err := models.DataChangeEventToNotification(&dataChangeEvent, a.GlobalCloudID)
		if err != nil {
			return fmt.Errorf("failed to convert dataChangeEvent to Notification: %w", err)
		}
		if notification != nil {
			notifications = append(notifications, *notification)
		}
	}

	// Dispatch notification
	for _, notification := range notifications {
		a.Notifier.Notify(ctx, &notification)
	}

	slog.Info("Successfully processed outbox notifications", "caller", caller)
	return nil
}
