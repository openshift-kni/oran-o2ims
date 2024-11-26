package alertmanager

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"maps"
	"text/template"

	"github.com/google/uuid"
	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

const (
	namespace  = "open-cluster-management-observability"
	secretName = "alertmanager-config"
	secretKey  = "alertmanager.yaml"
)

const templateName = "alertmanager.yaml"

//go:embed alertmanager.yaml.template
var alertManagerConfig []byte

// Setup updates the alertmanager config secret with the new configuration
func Setup(ctx context.Context, cl client.Client) error {
	// ACM recreates the secret when it is deleted, so we can safely assume it exists
	var secret corev1.Secret
	err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, &secret)
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
		"url": utils.GetServiceURL(utils.InventoryAlarmServerName),
	})
	if err != nil {
		return fmt.Errorf("failed to render alertmanager.yaml: %w", err)
	}

	secret.Data[secretKey] = rendered.Bytes()
	err = cl.Update(ctx, &secret)
	if err != nil {
		return fmt.Errorf("failed to update secret %s/%s: %w", namespace, secretName, err)
	}

	slog.Info("Successfully configured alertmanager")
	return nil
}

// ConvertAmToAlarmEventRecordModels get alarmEventRecords based on the alertmanager notification and AlarmDefinition
func ConvertAmToAlarmEventRecordModels(am *api.AlertmanagerNotification, aDefinitionRecords []models.AlarmDefinition) []models.AlarmEventRecord {
	records := make([]models.AlarmEventRecord, 0, len(am.Alerts))
	for _, alert := range am.Alerts {
		slog.Info("Converting Alertmanager alert", "alert name", GetAlertName(*alert.Labels))
		record := models.AlarmEventRecord{
			AlarmRaisedTime:   *alert.StartsAt,
			AlarmClearedTime:  alert.EndsAt,
			AlarmChangedTime:  alert.StartsAt, //Note: this value is not available as part of the notification (only in v2 api as updatedAt)
			PerceivedSeverity: getPerceivedSeverity(*alert.Labels),
			AlarmStatus:       string(*alert.Status),
			Fingerprint:       *alert.Fingerprint,
			ResourceID:        GetResourceID(*alert.Labels),
			ResourceTypeID:    GetResourceTypeID(*alert.Labels),
		}

		// Update Extensions with things we didn't really process
		record.Extensions = getExtensions(*alert.Labels, *alert.Annotations)

		// See if possible to pick up additional info from its definition
		for _, def := range aDefinitionRecords {
			if def.AlarmName == GetAlertName(*alert.Labels) { // TODO: match with the resourceTypeID too once init data is ready
				record.AlarmDefinitionID = def.AlarmDefinitionID
				record.ProbableCauseID = def.ProbableCauseID
			}
		}

		// Anything else that's not mentioned explicitly will be handled by DB such ID generation and default values as needed.
		records = append(records, record)
	}

	return records
}

// GetResourceID for caas alerts it's the cluster ID
func GetResourceID(labels map[string]string) uuid.UUID {
	val, ok := labels["managed_cluster"]
	if !ok {
		slog.Warn("Could not find managed_cluster", "labels", labels)
		return uuid.UUID{}
	}

	id, err := uuid.Parse(val)
	if err != nil {
		slog.Warn("Could convert managed_cluster string to uuid", "labels", labels, "err", err.Error())
		return uuid.UUID{}
	}

	return id
}

// GetResourceTypeID get resource Type ID.
// TODO Update this so that resourceTypeID correctly mapped to resourceID
func GetResourceTypeID(labels map[string]string) uuid.UUID {
	return GetResourceID(labels)
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

// getPerceivedSeverity am's `severity` to oran's PerceivedSeverity
func getPerceivedSeverity(labels map[string]string) api.PerceivedSeverity {
	input, ok := labels["severity"]
	if !ok {
		slog.Warn("Could not find severity label. This may impact the value of PerceivedSeverity", "labels", labels)
		return api.INDETERMINATE
	}

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
		slog.Warn("Could not map", "severity", input, "labels", labels)
		return api.INDETERMINATE
	}
}

// getExtensions extract oran extension from alert. For caas it's basically the labels and annotations from payload.
func getExtensions(labels, annotations map[string]string) map[string]string {
	maps.Copy(labels, annotations)
	return labels
}
