/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package serviceconfig

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"
)

// CleanupInterval is how often the in-process cleanup of resolved alarm events
// runs. It matches the hourly cadence of the CronJob this replaced.
const CleanupInterval = 1 * time.Hour

// Config drives the periodic in-process cleanup of resolved alarm events.
//
// The cleanup was previously implemented as a Kubernetes CronJob created by the
// alarms-server. That required the alarms-server ServiceAccount to hold
// batch/cronjobs "create" permission, which allowed a compromised pod to create
// a CronJob whose pods run as the near-cluster-admin controller-manager
// ServiceAccount (ORAN-O2IMS-2026-008). Running the cleanup in-process against
// the connection pool the server already holds removes that permission — and
// the escalation vector — entirely.
type Config struct {
	// Repository reads the current retention period and deletes resolved alarm
	// events.
	Repository repo.AlarmRepositoryInterface
}

// Start runs the resolved-alarm-event cleanup once immediately and then on every
// tick of interval until ctx is cancelled. It is intended to be launched in its
// own goroutine.
func (c *Config) Start(ctx context.Context, interval time.Duration) {
	slog.InfoContext(ctx, "Starting in-process alarms events cleanup", slog.Duration("interval", interval))

	// Run an initial cleanup at startup so resolved events aren't retained until
	// the first tick.
	if err := c.RunCleanup(ctx); err != nil {
		slog.ErrorContext(ctx, "Failed to run alarms events cleanup", slog.Any("error", err))
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "Stopping in-process alarms events cleanup")
			return
		case <-ticker.C:
			if err := c.RunCleanup(ctx); err != nil {
				slog.ErrorContext(ctx, "Failed to run alarms events cleanup", slog.Any("error", err))
			}
		}
	}
}

// RunCleanup deletes resolved alarm events older than the retention period
// currently stored in the Alarm Service Configuration. The retention period is
// read fresh on every run so that changes made through the
// AlarmServiceConfiguration API take effect on the next cleanup without any
// additional coordination.
func (c *Config) RunCleanup(ctx context.Context) error {
	records, err := c.Repository.GetServiceConfigurations(ctx)
	if err != nil {
		return fmt.Errorf("failed to get alarm service configuration: %w", err)
	}

	// There must always be a single record.
	if len(records) != 1 {
		return fmt.Errorf("expected a single alarm service configuration record, but got %d", len(records))
	}

	retentionPeriod := records[0].RetentionPeriod
	deleted, ran, err := c.Repository.DeleteResolvedAlarmEventsBefore(ctx, retentionPeriod)
	if err != nil {
		return fmt.Errorf("failed to delete resolved alarm events: %w", err)
	}
	if !ran {
		slog.DebugContext(ctx, "Skipped alarms events cleanup; another replica holds the cleanup lock")
		return nil
	}

	slog.InfoContext(ctx, "Completed alarms events cleanup",
		slog.Int("retentionPeriod", retentionPeriod), slog.Int64("deleted", deleted))
	return nil
}
