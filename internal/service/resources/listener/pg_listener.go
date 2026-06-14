/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package listener

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/listener"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/repo"
)

// ResourceTypeChangeNotification represents the payload from the resource_type_changed notification
type ResourceTypeChangeNotification struct {
	ResourceTypeID uuid.UUID `json:"resource_type_id"`
	ChangeType     string    `json:"change_type"` // "created", "updated", or "deleted"
}

// ListenForResourcePgChannels registers the channels with their handlers and starts listening.
// The onResourceTypeChanged callback is invoked whenever a resource_type_changed notification
// is received or the periodic catch-up sync runs, allowing callers to invalidate caches.
func ListenForResourcePgChannels(ctx context.Context, pool *pgxpool.Pool, repository *repo.ResourcesRepository, onResourceTypeChanged func()) {
	slog.InfoContext(ctx, "Starting PostgreSQL listener for resource server")

	// Sync existing ResourceTypes on startup to handle any that were created before listener started
	// This prevents race condition where collector creates ResourceTypes before listener is ready
	if err := syncExistingResourceTypes(ctx, repository); err != nil {
		slog.ErrorContext(ctx, "Failed to sync existing resource types on startup", slog.Any("error", err))
	}

	// Initialize the generic listener manager
	lm := listener.NewListenerManager(pool)

	// Register the resource_type_changed listener
	lm.RegisterListener(
		// Channel name specified in pg_notify call
		"resource_type_changed",
		// Function called after a notification is received
		func(ctx context.Context, pgNotification *pgconn.Notification) error {
			err := processResourceTypeChangeNotification(ctx, repository, pgNotification)
			if err == nil && onResourceTypeChanged != nil {
				onResourceTypeChanged()
			}
			return err
		},
		// Catch-up function runs periodically to handle missed notifications or failures
		func(ctx context.Context) error {
			err := syncExistingResourceTypes(ctx, repository)
			if err == nil && onResourceTypeChanged != nil {
				onResourceTypeChanged()
			}
			return err
		},
		// Catch-up interval - sync every 15 minutes as backup
		15*time.Minute,
	)

	// Start all registered listeners
	lm.StartListeners(ctx)

	// Block until the context is canceled
	<-ctx.Done()
	slog.InfoContext(ctx, "PostgreSQL listener for resource server shutting down")
	lm.Wait()
}

// processResourceTypeChangeNotification handles resource_type_changed notifications
func processResourceTypeChangeNotification(ctx context.Context, repository *repo.ResourcesRepository, pgNotification *pgconn.Notification) error {
	slog.DebugContext(ctx, "Received resource_type_changed notification")

	// Parse the notification payload
	var notification ResourceTypeChangeNotification
	if err := json.Unmarshal([]byte(pgNotification.Payload), &notification); err != nil {
		return fmt.Errorf("failed to unmarshal resource_type_changed notification: %w", err)
	}

	slog.InfoContext(ctx, "Processing resource type change",
		slog.Any("resource_type_id", notification.ResourceTypeID),
		slog.String("change_type", notification.ChangeType))

	// Handle different change types
	switch notification.ChangeType {
	case "deleted":
		// CASCADE delete in the database schema handles alarm dictionary cleanup
		slog.InfoContext(ctx, "Resource type deleted, alarm dictionary will be cascade deleted",
			slog.Any("resource_type_id", notification.ResourceTypeID))
		return nil

	case "created", "updated":
		// Sync alarm dictionary for this resource type
		if err := syncAlarmDictionaryForResourceType(ctx, repository, notification.ResourceTypeID); err != nil {
			slog.ErrorContext(ctx, "Failed to sync alarm dictionary for resource type",
				slog.Any("resource_type_id", notification.ResourceTypeID),
				slog.Any("error", err))
			return fmt.Errorf("failed to sync alarm dictionary: %w", err)
		}
		return nil

	default:
		slog.WarnContext(ctx, "Unknown resource type change type", slog.String("change_type", notification.ChangeType))
		return nil
	}
}

// syncExistingResourceTypes syncs alarm dictionaries for all existing resource types
// Called on startup and periodically (every 15min) to handle missed notifications or failures
func syncExistingResourceTypes(ctx context.Context, repository *repo.ResourcesRepository) error {
	slog.DebugContext(ctx, "Syncing alarm dictionaries for existing resource types")

	resourceTypes, err := repository.GetResourceTypes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get resource types: %w", err)
	}

	slog.DebugContext(ctx, "Found existing resource types", slog.Int("count", len(resourceTypes)))

	for _, rt := range resourceTypes {
		if err := syncAlarmDictionaryForResourceType(ctx, repository, rt.ResourceTypeID); err != nil {
			slog.ErrorContext(ctx, "Failed to sync alarm dictionary for existing resource type",
				slog.Any("resource_type_id", rt.ResourceTypeID),
				slog.Any("error", err))
			// Continue with other resource types even if one fails
		}
	}

	slog.DebugContext(ctx, "Completed sync of existing resource types", slog.Int("count", len(resourceTypes)))
	return nil
}
