package alertmanager

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"maps"
	"text/template"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/infrastructure"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
)

const (
	namespace  = utils.AlertmanagerNamespace
	secretName = "alertmanager-config"
	secretKey  = "alertmanager.yaml"
)

const templateName = "alertmanager.yaml"

//go:embed alertmanager.yaml.template
var alertManagerConfig []byte

var getHubClient = k8s.NewClientForHub

// Setup updates the alertmanager config secret with the new configuration
func Setup(ctx context.Context) error {
	hubClient, err := getHubClient()
	if err != nil {
		return fmt.Errorf("error creating client for hub: %w", err)
	}

	// ACM recreates the secret when it is deleted, so we can safely assume it exists
	var secret corev1.Secret
	err = hubClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, &secret)
	if err != nil {
		return fmt.Errorf("failed to get secret %s/%s: %w", namespace, secretName, err)
	}

	// Verify that Data has the key "alertmanager.yaml"
	if _, ok := secret.Data[secretKey]; !ok {
		return fmt.Errorf("%s not found in secret %s/%s", secretKey, namespace, secretName)
	}

	t, err := template.New(templateName).Parse(string(alertManagerConfig))
	if err != nil {
		return fmt.Errorf("failed to parse alertmanager.yaml: %w", err)
	}

	var rendered bytes.Buffer
	err = t.ExecuteTemplate(&rendered, templateName, map[string]string{
		"url":             utils.GetServiceURL(utils.InventoryAlarmServerName),
		"caFile":          utils.DefaultServiceCAFile,
		"bearerTokenFile": utils.DefaultBackendTokenFile,
	})
	if err != nil {
		return fmt.Errorf("failed to render alertmanager.yaml: %w", err)
	}

	secret.Data[secretKey] = rendered.Bytes()
	err = hubClient.Update(ctx, &secret)
	if err != nil {
		return fmt.Errorf("failed to update secret %s/%s: %w", namespace, secretName, err)
	}

	slog.Info("Successfully configured alertmanager")
	return nil
}

// ConvertAmToAlarmEventRecordModels get alarmEventRecords based on the alertmanager notification and AlarmDefinition
func ConvertAmToAlarmEventRecordModels(am *api.AlertmanagerNotification, infrastructureClient infrastructure.Client) []models.AlarmEventRecord {
	records := make([]models.AlarmEventRecord, 0, len(am.Alerts))
	for _, alert := range am.Alerts {
		record := models.AlarmEventRecord{
			AlarmRaisedTime:  *alert.StartsAt,
			AlarmClearedTime: setTime(*alert.EndsAt),
			AlarmStatus:      string(*alert.Status),
			Fingerprint:      *alert.Fingerprint,
		}

		// Make sure the current payload has the right severity
		if *alert.Status == api.Resolved {
			record.PerceivedSeverity = severityToPerceivedSeverity("cleared")
		} else {
			ps, _ := GetPerceivedSeverity(*alert.Labels)
			record.PerceivedSeverity = ps
		}

		// Update Extensions with things we didn't really process
		record.Extensions = getExtensions(*alert.Labels, *alert.Annotations)

		// for caas alerts object is the cluster ID
		record.ObjectID = GetClusterID(*alert.Labels)

		// derive ObjectTypeID from ObjectID
		if record.ObjectID != nil {
			objectTypeID, err := infrastructureClient.GetObjectTypeID(*record.ObjectID)
			if err != nil {
				slog.Warn("Could not get object type ID", "objectID", record.ObjectID, "err", err.Error())
			} else {
				record.ObjectTypeID = &objectTypeID
			}
		}

		// See if possible to pick up additional info from its definition
		if record.ObjectTypeID != nil {
			_, severity := GetPerceivedSeverity(*alert.Labels)
			alarmDefinitionID, err := infrastructureClient.GetAlarmDefinitionID(*record.ObjectTypeID, GetAlertName(*alert.Labels), severity)
			if err != nil {
				slog.Warn("Could not get alarm definition ID", "objectTypeID", *record.ObjectTypeID, "name", GetAlertName(*alert.Labels), "severity", severity, "err", err.Error())
			} else {
				record.AlarmDefinitionID = &alarmDefinitionID
			}
		}

		// Anything else that's not mentioned explicitly will be handled by DB such ID generation and default values as needed.
		records = append(records, record)
	}

	return records
}

// Function to set the time value
func setTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func GetClusterID(labels map[string]string) *uuid.UUID {
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

// GetAlertName extract name from alert label
func GetAlertName(labels map[string]string) string {
	val, ok := labels["alertname"]
	if !ok {
		// this may never execute but keeping a check just in case
		slog.Warn("Could not find alertname", "labels", labels)
		return "Unknown"
	}

	return val
}

// GetPerceivedSeverity am's `severity` to oran's PerceivedSeverity
func GetPerceivedSeverity(labels map[string]string) (api.PerceivedSeverity, string) {
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

// getExtensions extract oran extension from alert. For caas it's basically the labels and annotations from payload.
func getExtensions(labels, annotations map[string]string) map[string]string {
	if labels == nil {
		labels = make(map[string]string)
	}
	if annotations == nil {
		annotations = make(map[string]string)
	}

	result := make(map[string]string)
	maps.Copy(result, labels)
	maps.Copy(result, annotations)
	return labels
}
