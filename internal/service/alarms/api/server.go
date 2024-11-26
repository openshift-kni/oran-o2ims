package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/alertmanager"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/clusterserver"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"
	api2 "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	apiresources "github.com/openshift-kni/oran-o2ims/internal/service/resources/api/generated"
)

const (
	DefaultRetentionPeriod = 10
	minRetentionPeriod     = 1
)

// AlarmsServerConfig defines the configuration attributes for the alarms server
type AlarmsServerConfig struct {
	Address       string
	GlobalCloudID string
}

type AlarmsServer struct {
	// GlobalCloudID is the global O-Cloud identifier. Create subscription requests are blocked if the global O-Cloud identifier is not set
	GlobalCloudID uuid.UUID
	// AlarmsRepository is the repository for the alarms
	AlarmsRepository *repo.AlarmsRepository
	// ClusterServer contains the cluster server client and fetched objects
	ClusterServer *clusterserver.ClusterServer
}

// AlarmsServer implements StrictServerInterface. This ensures that we've conformed to the `StrictServerInterface` with a compile-time check
var _ api.StrictServerInterface = (*AlarmsServer)(nil)

// baseURL is the prefix for all of our supported API endpoints
var baseURL = "/o2ims-infrastructureMonitoring/v1"
var currentVersion = "1.0.0"

// GetAllVersions receives the API request to this endpoint, executes the request, and responds appropriately
func (r *AlarmsServer) GetAllVersions(ctx context.Context, request api.GetAllVersionsRequestObject) (api.GetAllVersionsResponseObject, error) {
	// We currently only support a single version
	versions := []common.APIVersion{
		{
			Version: &currentVersion,
		},
	}

	return api.GetAllVersions200JSONResponse(common.APIVersions{
		ApiVersions: &versions,
		UriPrefix:   &baseURL,
	}), nil
}

// GetMinorVersions receives the API request to this endpoint, executes the request, and responds appropriately
func (r *AlarmsServer) GetMinorVersions(ctx context.Context, request api.GetMinorVersionsRequestObject) (api.GetMinorVersionsResponseObject, error) {
	// We currently only support a single version
	versions := []common.APIVersion{
		{
			Version: &currentVersion,
		},
	}

	return api.GetMinorVersions200JSONResponse(common.APIVersions{
		ApiVersions: &versions,
		UriPrefix:   &baseURL,
	}), nil
}

// GetSubscriptions handles an API request to fetch Alarm Subscriptions
func (a *AlarmsServer) GetSubscriptions(ctx context.Context, _ api.GetSubscriptionsRequestObject) (api.GetSubscriptionsResponseObject, error) {
	records, err := a.AlarmsRepository.GetAlarmSubscriptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Alarm Subscriptions: %w", err)
	}

	objects := make([]api.AlarmSubscriptionInfo, 0, len(records))
	for _, record := range records {
		objects = append(objects, models.ConvertSubscriptionModelToApi(record))
	}

	return api.GetSubscriptions200JSONResponse(objects), nil
}

// CreateSubscription handles an API request to create an Alarm Subscription
func (a *AlarmsServer) CreateSubscription(ctx context.Context, request api.CreateSubscriptionRequestObject) (api.CreateSubscriptionResponseObject, error) {
	// Block API if GlobalCloudID is not set
	if a.GlobalCloudID == uuid.Nil {
		return api.CreateSubscription409ApplicationProblemPlusJSONResponse(common.ProblemDetails{
			Detail: "provisioning of Alarm Subscriptions is blocked until the SMO attributes are configured",
			Status: http.StatusConflict,
		}), nil
	}

	// Validate the subscription
	if err := api2.ValidateCallbackURL(request.Body.Callback); err != nil {
		return api.CreateSubscription400ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"callback": request.Body.Callback,
			},
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}, nil
	}

	r := models.ConvertSubscriptionAPIToModel(request.Body)

	// TODO: perform a Reachability check as suggested in O-RAN.WG6.ORCH-USE-CASES-R003-v11.00

	record, err := a.AlarmsRepository.CreateAlarmSubscription(ctx, r)
	if err != nil {
		if strings.Contains(err.Error(), "unique_callback") {
			// 409 is a more common choice for a duplicate entry, but the conformance tests expect a 400
			return api.CreateSubscription400ApplicationProblemPlusJSONResponse(common.ProblemDetails{
				AdditionalAttributes: &map[string]string{
					"callback": request.Body.Callback,
				},
				Detail: "callback value must be unique",
				Status: http.StatusBadRequest,
			}), nil
		}
		return api.CreateSubscription500ApplicationProblemPlusJSONResponse(common.ProblemDetails{
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}), nil
	}

	return api.CreateSubscription201JSONResponse(models.ConvertSubscriptionModelToApi(*record)), nil
}

// DeleteSubscription handles an API request to delete an Alarm Subscription
func (a *AlarmsServer) DeleteSubscription(ctx context.Context, request api.DeleteSubscriptionRequestObject) (api.DeleteSubscriptionResponseObject, error) {
	count, err := a.AlarmsRepository.DeleteAlarmSubscription(ctx, request.AlarmSubscriptionId)
	if err != nil {
		return api.DeleteSubscription500ApplicationProblemPlusJSONResponse(common.ProblemDetails{
			AdditionalAttributes: &map[string]string{
				"alarmSubscriptionId": request.AlarmSubscriptionId.String(),
			},
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		}), nil
	}

	if count == 0 {
		return api.DeleteSubscription404ApplicationProblemPlusJSONResponse(common.ProblemDetails{
			AdditionalAttributes: &map[string]string{
				"alarmSubscriptionId": request.AlarmSubscriptionId.String(),
			},
			Detail: "requested Alarm Subscription not found",
			Status: http.StatusNotFound,
		}), nil
	}

	return api.DeleteSubscription200Response{}, nil
}

// GetSubscription handles an API request to retrieve an Alarm Subscription
func (a *AlarmsServer) GetSubscription(ctx context.Context, request api.GetSubscriptionRequestObject) (api.GetSubscriptionResponseObject, error) {
	record, err := a.AlarmsRepository.GetAlarmSubscription(ctx, request.AlarmSubscriptionId)
	if errors.Is(err, utils.ErrNotFound) {
		return api.GetSubscription404ApplicationProblemPlusJSONResponse(common.ProblemDetails{
			AdditionalAttributes: &map[string]string{
				"alarmSubscriptionId": request.AlarmSubscriptionId.String(),
			},
			Detail: "requested Alarm Subscription not found",
			Status: http.StatusNotFound,
		}), nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get Alarm Subscription: %w", err)
	}

	return api.GetSubscription200JSONResponse(models.ConvertSubscriptionModelToApi(*record)), nil
}

// GetAlarms handles an API request to fetch Alarm Event Records
func (a *AlarmsServer) GetAlarms(ctx context.Context, _ api.GetAlarmsRequestObject) (api.GetAlarmsResponseObject, error) {
	records, err := a.AlarmsRepository.GetAlarmEventRecords(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Alarm Event Records: %w", err)
	}

	objects := make([]api.AlarmEventRecord, 0, len(records))
	for _, record := range records {
		objects = append(objects, models.ConvertAlarmEventRecordModelToApi(record))
	}

	return api.GetAlarms200JSONResponse(objects), nil
}

// GetAlarm handles an API request to retrieve an Alarm Event Record
func (a *AlarmsServer) GetAlarm(ctx context.Context, request api.GetAlarmRequestObject) (api.GetAlarmResponseObject, error) {
	record, err := a.AlarmsRepository.GetAlarmEventRecord(ctx, request.AlarmEventRecordId)
	if errors.Is(err, utils.ErrNotFound) {
		// Nothing found
		return api.GetAlarm404ApplicationProblemPlusJSONResponse(common.ProblemDetails{
			AdditionalAttributes: &map[string]string{
				"alarmEventRecordId": request.AlarmEventRecordId.String(),
			},
			Detail: "requested Alarm Event Record not found",
			Status: http.StatusNotFound,
		}), nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get Alarm Event Record: %w", err)
	}

	return api.GetAlarm200JSONResponse(models.ConvertAlarmEventRecordModelToApi(*record)), nil
}

// PatchAlarm handles an API request to patch an Alarm Event Record
func (a *AlarmsServer) PatchAlarm(ctx context.Context, request api.PatchAlarmRequestObject) (api.PatchAlarmResponseObject, error) {
	// Fetch the Alarm Event Record to be patched
	record, err := a.AlarmsRepository.GetAlarmEventRecord(ctx, request.AlarmEventRecordId)
	if errors.Is(err, utils.ErrNotFound) {
		// Nothing found
		return api.PatchAlarm404ApplicationProblemPlusJSONResponse(common.ProblemDetails{
			AdditionalAttributes: &map[string]string{
				"alarmEventRecordId": request.AlarmEventRecordId.String(),
			},
			Detail: "requested Alarm Event Record not found",
			Status: http.StatusNotFound,
		}), nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get Alarm Event Record: %w", err)
	}

	// Check that request has at least one field to patch
	if request.Body.AlarmAcknowledged == nil && request.Body.PerceivedSeverity == nil {
		// Bad request - at least one field is required to patch
		return api.PatchAlarm400ApplicationProblemPlusJSONResponse(common.ProblemDetails{
			AdditionalAttributes: &map[string]string{
				"alarmEventRecordId": request.AlarmEventRecordId.String(),
			},
			Detail: "at least one field is required to patch",
			Status: http.StatusBadRequest,
		}), nil
	}

	// Check if both fields are included in the request
	if request.Body.AlarmAcknowledged != nil && request.Body.PerceivedSeverity != nil {
		// Bad request - only one field is allowed to be patched
		return api.PatchAlarm400ApplicationProblemPlusJSONResponse(common.ProblemDetails{
			AdditionalAttributes: &map[string]string{
				"alarmEventRecordId": request.AlarmEventRecordId.String(),
			},
			Detail: "either alarmAcknowledged or perceivedSeverity shall be included in a request message content, but not both",
			Status: http.StatusBadRequest,
		}), nil

	}

	// Patch perceivedSeverity
	if request.Body.PerceivedSeverity != nil {
		perceivedSeverity := *request.Body.PerceivedSeverity
		// Only the value "5" for "CLEARED" is permitted in a request message content
		if perceivedSeverity != api.CLEARED {
			return api.PatchAlarm400ApplicationProblemPlusJSONResponse(common.ProblemDetails{
				AdditionalAttributes: &map[string]string{
					"alarmEventRecordId": request.AlarmEventRecordId.String(),
				},
				Detail: fmt.Sprintf("only the value %d for CLEARED is permitted in the perceivedSeverity field", api.CLEARED),
				Status: http.StatusBadRequest,
			}), nil
		}

		// Check if associated alarm definition has clearing type "manual". If not, return 409.
		alarmDefinition, err := a.AlarmsRepository.GetAlarmDefinition(ctx, *record.AlarmDefinitionID)
		if errors.Is(err, utils.ErrNotFound) {
			return api.PatchAlarm404ApplicationProblemPlusJSONResponse(common.ProblemDetails{
				AdditionalAttributes: &map[string]string{
					"alarmEventRecordId": request.AlarmEventRecordId.String(),
				},
				Detail: "associated Alarm Definition not found",
				Status: http.StatusNotFound,
			}), nil
		}

		if alarmDefinition.ClearingType != string(apiresources.MANUAL) {
			return api.PatchAlarm409ApplicationProblemPlusJSONResponse(common.ProblemDetails{
				AdditionalAttributes: &map[string]string{
					"alarmEventRecordId": request.AlarmEventRecordId.String(),
				},
				Detail: "cannot clear an alarm with clearing type other than MANUAL",
				Status: http.StatusConflict,
			}), nil
		}

		// Check if the Alarm Event Record has already been cleared
		if record.PerceivedSeverity == perceivedSeverity {
			// Nothing to patch
			return api.PatchAlarm409ApplicationProblemPlusJSONResponse(common.ProblemDetails{
				AdditionalAttributes: &map[string]string{
					"alarmEventRecordId": request.AlarmEventRecordId.String(),
				},
				Detail: "Alarm record is already cleared",
				Status: http.StatusConflict,
			}), nil
		}

		// Patch the Alarm Event Record
		record.PerceivedSeverity = perceivedSeverity
		currentTime := time.Now()
		record.AlarmClearedTime = &currentTime
		record.AlarmChangedTime = &currentTime
	}

	// Patch alarmAcknowledged
	if request.Body.AlarmAcknowledged != nil {
		alarmAcknowledged := *request.Body.AlarmAcknowledged

		// Check if requested Acknowledged status is true
		if !alarmAcknowledged {
			// Bad request - Acknowledged status is expected to be true
			return api.PatchAlarm400ApplicationProblemPlusJSONResponse(common.ProblemDetails{
				AdditionalAttributes: &map[string]string{
					"alarmEventRecordId": request.AlarmEventRecordId.String(),
				},
				Detail: "alarmAcknowledged field is expected to be true",
				Status: http.StatusBadRequest,
			}), nil
		}

		// Check if the Alarm Event Record has already been acknowledged
		if record.AlarmAcknowledged == alarmAcknowledged {
			// Nothing to patch
			return api.PatchAlarm400ApplicationProblemPlusJSONResponse(common.ProblemDetails{
				AdditionalAttributes: &map[string]string{
					"alarmEventRecordId": request.AlarmEventRecordId.String(),
				},
				Detail: "Alarm record is already acknowledged",
				Status: http.StatusConflict,
			}), nil
		}

		// Patch the Alarm Event Record
		record.AlarmAcknowledged = alarmAcknowledged
		currentTime := time.Now()
		record.AlarmAcknowledgedTime = &currentTime
		record.AlarmChangedTime = &currentTime
	}

	// Update the Alarm Event Record
	updated, err := a.AlarmsRepository.PatchAlarmEventRecordACK(ctx, request.AlarmEventRecordId, record)
	if err != nil {
		return nil, fmt.Errorf("failed to patch Alarm Event Record: %w", err)
	}

	slog.Debug("Alarm acknowledged/cleared", "alarmEventRecordId", updated.AlarmEventRecordID, "alarmAcknowledged", updated.AlarmAcknowledged, "alarmAcknowledgedTime", updated.AlarmAcknowledgedTime,
		"alarmClearedTime", updated.AlarmClearedTime, "perceivedSeverity", updated.PerceivedSeverity, "alarmChangedTime", updated.AlarmChangedTime)

	return api.PatchAlarm200JSONResponse{AlarmAcknowledged: request.Body.AlarmAcknowledged, PerceivedSeverity: request.Body.PerceivedSeverity}, nil
}

// GetServiceConfiguration handles an API request to fetch the Alarm Service Configuration
func (a *AlarmsServer) GetServiceConfiguration(ctx context.Context, _ api.GetServiceConfigurationRequestObject) (api.GetServiceConfigurationResponseObject, error) {
	records, err := a.AlarmsRepository.GetServiceConfigurations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Alarm Service Configuration: %w", err)
	}

	// There must always be a single record
	if len(records) != 1 {
		return nil, fmt.Errorf("expected a single Alarm Service Configuration record, but got %d", len(records))
	}

	object := models.ConvertServiceConfigurationToAPI(records[0])

	return api.GetServiceConfiguration200JSONResponse(object), nil
}

// PatchAlarmServiceConfiguration handles an API request to patch the Alarm Service Configuration
func (a *AlarmsServer) PatchAlarmServiceConfiguration(ctx context.Context, request api.PatchAlarmServiceConfigurationRequestObject) (api.PatchAlarmServiceConfigurationResponseObject, error) {
	// Fetch the Alarm Service Configuration to be patched
	records, err := a.AlarmsRepository.GetServiceConfigurations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Alarm Service Configuration: %w", err)
	}

	// There must always be a single record
	if len(records) != 1 {
		return nil, fmt.Errorf("expected a single Alarm Service Configuration record, but got %d", len(records))
	}

	serviceConfigRecord := records[0]

	// Patch the Alarm Service Configuration
	if request.Body.RetentionPeriod != 0 {
		// Check if the retention period is valid
		if serviceConfigRecord.RetentionPeriod < minRetentionPeriod {
			return api.PatchAlarmServiceConfiguration400ApplicationProblemPlusJSONResponse(common.ProblemDetails{
				Detail: fmt.Sprintf("retentionPeriod must be greater than or equal to %d", minRetentionPeriod),
				Status: http.StatusBadRequest,
			}), nil
		}

		serviceConfigRecord.RetentionPeriod = request.Body.RetentionPeriod
	}

	if request.Body.Extensions != nil {
		serviceConfigRecord.Extensions = *request.Body.Extensions
	}

	// Patch the Alarm Service Configuration
	patched, err := a.AlarmsRepository.UpdateServiceConfiguration(ctx, serviceConfigRecord.ID, &serviceConfigRecord)
	if err != nil {
		return nil, fmt.Errorf("failed to patch Alarm Service Configuration: %w", err)
	}

	slog.Debug("Alarm Service Configuration patched", "retentionPeriod", patched.RetentionPeriod, "extensions", patched.Extensions)

	return api.PatchAlarmServiceConfiguration200JSONResponse(models.ConvertServiceConfigurationToAPI(*patched)), nil
}

func (a *AlarmsServer) UpdateAlarmServiceConfiguration(ctx context.Context, request api.UpdateAlarmServiceConfigurationRequestObject) (api.UpdateAlarmServiceConfigurationResponseObject, error) {
	// Fetch the Alarm Service Configuration to be updated
	records, err := a.AlarmsRepository.GetServiceConfigurations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Alarm Service Configuration: %w", err)
	}

	// There must always be a single record
	if len(records) != 1 {
		return nil, fmt.Errorf("expected a single Alarm Service Configuration record, but got %d", len(records))
	}

	serviceConfigRecord := records[0]

	// Check if the retention period is valid
	if request.Body.RetentionPeriod < minRetentionPeriod {
		return api.UpdateAlarmServiceConfiguration400ApplicationProblemPlusJSONResponse(common.ProblemDetails{
			Detail: fmt.Sprintf("retentionPeriod must be greater than or equal to %d (day)", minRetentionPeriod),
			Status: http.StatusBadRequest,
		}), nil
	}

	// Update the Alarm Service Configuration
	serviceConfigRecord.RetentionPeriod = request.Body.RetentionPeriod
	serviceConfigRecord.Extensions = nil
	if request.Body.Extensions != nil {
		serviceConfigRecord.Extensions = *request.Body.Extensions
	}

	// Update the Alarm Service Configuration
	updated, err := a.AlarmsRepository.UpdateServiceConfiguration(ctx, serviceConfigRecord.ID, &serviceConfigRecord)
	if err != nil {
		return nil, fmt.Errorf("failed to update Alarm Service Configuration: %w", err)
	}

	slog.Debug("Alarm Service Configuration updated", "retentionPeriod", updated.RetentionPeriod, "extensions", updated.Extensions)

	return api.UpdateAlarmServiceConfiguration200JSONResponse(models.ConvertServiceConfigurationToAPI(*updated)), nil

}

func (a *AlarmsServer) GetProbableCauses(ctx context.Context, request api.GetProbableCausesRequestObject) (api.GetProbableCausesResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

func (a *AlarmsServer) GetProbableCause(ctx context.Context, request api.GetProbableCauseRequestObject) (api.GetProbableCauseResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

// AmNotification handles an API request coming from AlertManager with CaaS alerts. This api is used internally.
// Note: the errors returned can also be view under alertmanager pod logs but also logging here for convenience
func (a *AlarmsServer) AmNotification(ctx context.Context, request api.AmNotificationRequestObject) (api.AmNotificationResponseObject, error) {
	// Audit the table with full list of alerts in the current payload. If missing set them to resolve
	if err := a.AlarmsRepository.ResolveNotificationIfNotInCurrent(ctx, request.Body); err != nil {
		msg := "failed to resolve notification that are not present"
		slog.Error(msg, "error", err)
		return nil, fmt.Errorf("%s: %w", msg, err)
	}

	// Get the definition data based on current set of Alert names and managed cluster ID
	alarmDefinitions, err := a.AlarmsRepository.GetAlarmDefinitions(ctx, request.Body, a.ClusterServer.ClusterIDToResourceTypeID)
	if err != nil {
		msg := "failed to get AlarmDefinitions"
		slog.Error(msg, "error", err)
		return nil, fmt.Errorf("%s: %w", msg, err)
	}

	// Combine possible definitions with events
	aerModels := alertmanager.ConvertAmToAlarmEventRecordModels(request.Body, alarmDefinitions, a.ClusterServer.ClusterIDToResourceTypeID)

	// Insert and update AlarmEventRecord
	if err := a.AlarmsRepository.UpsertAlarmEventRecord(ctx, aerModels); err != nil {
		msg := "failed to upsert AlarmEventRecord to db"
		slog.Error(msg, "error", err)
		return nil, fmt.Errorf("%s: %w", msg, err)
	}

	//TODO: Get subscriber

	//TODO: Notify subscriber
	return api.AmNotification200Response{}, nil
}

func (a *AlarmsServer) HwNotification(ctx context.Context, request api.HwNotificationRequestObject) (api.HwNotificationResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}
