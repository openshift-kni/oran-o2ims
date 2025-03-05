/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package alertmanager

import (
	"context"
	"log/slog"
	"maps"
	"time"

	"github.com/google/uuid"
	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/infrastructure"
)

// ConvertAmToAlarmEventRecordModels get alarmEventRecords based on the alertmanager notification and AlarmDefinition
func ConvertAmToAlarmEventRecordModels(ctx context.Context, alerts *[]api.Alert, infrastructureClient infrastructure.Client) []models.AlarmEventRecord {
	records := make([]models.AlarmEventRecord, 0, len(*alerts))
	for _, alert := range *alerts {
		record := models.AlarmEventRecord{}

		// Validate startsAt is always there
		if alert.StartsAt != nil && !alert.StartsAt.IsZero() {
			record.AlarmRaisedTime = *alert.StartsAt
		} else {
			slog.Error("Alert StartsAt is required, skipping.", "alert", alert)
			continue
		}

		// Validate status is always there, also derive the PerceivedSeverity as needed
		if alert.Status != nil {
			record.AlarmStatus = string(*alert.Status)

			// Get labels safely
			labels := getLabels(alert)

			// Make sure the current payload has the right severity
			if *alert.Status == api.Resolved {
				record.PerceivedSeverity = severityToPerceivedSeverity("cleared")
			} else {
				ps, _ := getPerceivedSeverity(labels)
				record.PerceivedSeverity = ps
			}
		} else {
			slog.Error("Alert Status is required, skipping.", "alert", alert)
			continue
		}

		// Validate Fingerprint is always there
		if alert.Fingerprint != nil {
			record.Fingerprint = *alert.Fingerprint
		} else {
			slog.Error("Alert Fingerprint is required, skipping.", "alert", alert)
			continue
		}

		if alert.EndsAt != nil && !alert.EndsAt.IsZero() {
			record.AlarmClearedTime = alert.EndsAt
		}

		// Get labels
		labels := getLabels(alert)

		// Update Extensions with things we didn't really process
		record.Extensions = getExtensions(alert)

		// for caas alerts object is the cluster ID
		record.ObjectID = getClusterID(labels)

		// derive ObjectTypeID from ObjectID
		if record.ObjectID != nil {
			objectTypeID, err := infrastructureClient.GetObjectTypeID(ctx, *record.ObjectID)
			if err != nil {
				slog.Warn("Could not get object type ID", "objectID", record.ObjectID, "err", err.Error())
			} else {
				record.ObjectTypeID = &objectTypeID
			}
		}

		// See if possible to pick up additional info from its definition
		if record.ObjectTypeID != nil {
			// Severity from PromRule (shows up in alert's label)
			_, severity := getPerceivedSeverity(labels)
			alarmDefinitionID, err := infrastructureClient.GetAlarmDefinitionID(ctx, *record.ObjectTypeID, getAlertName(labels), severity)
			if err != nil {
				slog.Warn("Could not get alarm definition ID", "objectTypeID", *record.ObjectTypeID, "name", getAlertName(labels), "severity", severity, "err", err.Error())
			} else {
				record.AlarmDefinitionID = &alarmDefinitionID
			}
		}

		// Anything else that's not mentioned explicitly will be handled by DB such ID generation and default values as needed.
		records = append(records, record)
	}

	slog.Info("Converted alerts", "records", len(records))
	return records
}

func getClusterID(labels map[string]string) *uuid.UUID {
	val, ok := labels["managed_cluster"]
	if !ok {
		slog.Warn("Could not find managed_cluster", "labels", labels)
		return nil
	}

	id, err := uuid.Parse(val)
	if err != nil {
		slog.Warn("Could convert managed_cluster string to uuid", "labels", labels, "err", err.Error())
		return nil
	}

	return &id
}

// getAlertName extract name from alert label
func getAlertName(labels map[string]string) string {
	val, ok := labels["alertname"]
	if !ok {
		// this may never execute but keeping a check just in case
		slog.Warn("Could not find alertname", "labels", labels)
		return "Unknown"
	}

	return val
}

// getPerceivedSeverity am's `severity` to oran's PerceivedSeverity
func getPerceivedSeverity(labels map[string]string) (api.PerceivedSeverity, string) {
	severity, ok := labels["severity"]
	if !ok {
		slog.Warn("Could not find severity label", "labels", labels)
		return api.INDETERMINATE, ""
	}

	return severityToPerceivedSeverity(severity), severity
}

func severityToPerceivedSeverity(input string) api.PerceivedSeverity {
	switch input {
	case "cleared":
		return api.CLEARED
	case "critical":
		return api.CRITICAL
	case "major":
		return api.MAJOR
	case "minor", "low":
		return api.MINOR
	case "warning", "info":
		return api.WARNING
	default:
		return api.INDETERMINATE
	}
}

// getExtensions extract oran extension from alert
func getExtensions(a api.Alert) map[string]string {
	result := make(map[string]string)

	// Bring in labels and annoations
	maps.Copy(result, getLabels(a))
	maps.Copy(result, getAnnotations(a))

	// Add gen url
	if a.GeneratorURL != nil {
		result["generatorURL"] = *a.GeneratorURL
	}

	return result
}

// Safely get labels
func getLabels(alert api.Alert) map[string]string {
	if alert.Labels == nil {
		return make(map[string]string)
	}
	return *alert.Labels
}

// Safely get annotations
func getAnnotations(alert api.Alert) map[string]string {
	if alert.Annotations == nil {
		return make(map[string]string)
	}
	return *alert.Annotations
}

// ConvertAPIAlertsToWebhook converts a slice of API into a slice of Webhook.
func ConvertAPIAlertsToWebhook(apiAlerts *[]APIAlert) []api.Alert {
	// Handle nil input
	if apiAlerts == nil {
		return []api.Alert{}
	}

	// Handle empty input array
	if len(*apiAlerts) == 0 {
		return []api.Alert{}
	}

	webhookAlerts := make([]api.Alert, 0, len(*apiAlerts))
	now := time.Now().UTC()

	for _, a := range *apiAlerts {
		// Determine the alert status based on endsAt compared to current time.
		//
		// This is strange but API call will always have an `endsAt` regardless if an alert actually "ended"/resolved.
		// The value of `endsAt` is either future or time in the past.
		// For alerts coming from Prometheus it will always have an "endsAt" from source but in other cases AM will add do 'endsAt = now() + resolved_time'...but this is not applicable for us.
		// More info: https://prometheus.io/docs/alerting/latest/configuration/#file-layout-and-global-settings
		// A time in past indicates an alert is resolved.
		//
		// A resolved alerts (i.e. endsAt from past) is set to be removed from the '/alerts' but depending on when we call the API
		// we may still see "resolved" alerts, which is why this need to be handled correctly.
		var (
			status      api.AlertmanagerNotificationStatus
			finalEndsAt *time.Time
		)

		// Assume alert is firing as this is likely case
		status = api.Firing
		finalEndsAt = nil

		// If endsAt in the past it's resolved
		if a.EndsAt != nil && now.After(*a.EndsAt) {
			status = api.Resolved
			finalEndsAt = a.EndsAt
		}

		// Other than endsAt and status (which needs to be computed) everything else is a pass-through
		// to make API payload to a webhook payload
		webhookAlert := api.Alert{
			Annotations:  a.Annotations,
			Labels:       a.Labels,
			StartsAt:     a.StartsAt,
			EndsAt:       finalEndsAt,
			Fingerprint:  a.Fingerprint,
			GeneratorURL: a.GeneratorURL,
			Status:       &status,
		}

		webhookAlerts = append(webhookAlerts, webhookAlert)
	}

	slog.Info("Converted from API to Webhook", "alerts", len(webhookAlerts))
	return webhookAlerts
}
