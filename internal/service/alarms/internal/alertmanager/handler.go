// SPDX-FileCopyrightText: Red Hat
//
// SPDX-License-Identifier: Apache-2.0
package alertmanager

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/infrastructure"
)

// SourceType of Alertmanager payload either through API or Webhook
type SourceType string

const (
	API     SourceType = "API"
	Webhook SourceType = "Webhook"
)

// HandleAlerts can be called when a payload from Webhook or API `/alerts` is received
// Webhook is our primary and API as our backup and sync mechanism
func HandleAlerts(ctx context.Context, clients []infrastructure.Client, repository repo.AlarmRepositoryInterface, alerts *[]api.Alert, source SourceType) error {
	// Handle nil alerts
	if alerts == nil {
		return nil
	}

	// Handle empty alerts
	if len(*alerts) == 0 {
		return nil
	}

	// Get cached cluster server data
	var (
		clusterServer infrastructure.Client
		found         bool
	)
	for i := range clients {
		if clients[i].Name() == infrastructure.Name {
			clusterServer = clients[i]
			found = true
		}
	}
	if !found {
		return fmt.Errorf("no cluster server found with name %q", infrastructure.Name)
	}

	// Combine possible definitions with events
	aerModels := ConvertAmToAlarmEventRecordModels(ctx, alerts, clusterServer)

	// Insert and update AlarmEventRecord and optionally resolve stale
	if err := repository.WithTransaction(ctx, func(tx pgx.Tx) error {
		// genID to determine if stale
		generationID := time.Now().UnixNano()

		// Insert or update with alerts
		if err := repository.UpsertAlarmEventCaaSRecord(ctx, tx, aerModels, generationID); err != nil {
			return fmt.Errorf("failed to upsert alarm event record model: %w", err)
		}

		// Resolve stale only if source is API `/alerts` as this step only works if we have full set of alerts
		if source == API {
			if err := repository.ResolveStaleAlarmEventCaaSRecord(ctx, tx, generationID); err != nil {
				return fmt.Errorf("could not resolve stale notification: %w", err)
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to handle alerts from %s: %w", string(source), err)
	}

	slog.Info("Successfully handled AlarmEventRecords", "source_type", string(source))
	return nil
}
