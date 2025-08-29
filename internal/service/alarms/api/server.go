/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/alertmanager"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/infrastructure"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/serviceconfig"
	commonapi "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

const (
	DefaultRetentionPeriod = 1 // Default retention of resolved alarms in days
	minRetentionPeriod     = 1 // Minimum retention of resolved alarms in days
)

// AlarmsServerConfig defines the configuration attributes for the alarms server
type AlarmsServerConfig struct {
	svcutils.CommonServerConfig
	Address       string
	GlobalCloudID string
}

type AlarmsServer struct {
	// GlobalCloudID is the global O-Cloud identifier. Create subscription requests are blocked if the global O-Cloud identifier is not set
	GlobalCloudID uuid.UUID
	// AlarmsRepository is the repository for the alarms
	AlarmsRepository repo.AlarmRepositoryInterface
	// Infrastructure clients
	Infrastructure *infrastructure.Infrastructure
	// Wg to allow alarm server level background tasks to finish before graceful exit
	Wg sync.WaitGroup
	// SubscriptionEventHandler to notify subscribers with new events
	SubscriptionEventHandler notifier.SubscriptionEventHandler
	// ServiceConfig config needed to manage ServiceConfig
	ServiceConfig serviceconfig.Config
}

// AlarmsServer implements StrictServerInterface. This ensures that we've conformed to the `StrictServerInterface` with a compile-time check
var _ api.StrictServerInterface = (*AlarmsServer)(nil)

// baseURL is the prefix for all of our supported API endpoints
var baseURL = constants.O2IMSMonitoringBaseURL
var currentVersion = "1.0.0"

// GetAllVersions receives the API request to this endpoint, executes the request, and responds appropriately
func (a *AlarmsServer) GetAllVersions(ctx context.Context, request api.GetAllVersionsRequestObject) (api.GetAllVersionsResponseObject, error) {
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
func (a *AlarmsServer) GetMinorVersions(ctx context.Context, request api.GetMinorVersionsRequestObject) (api.GetMinorVersionsResponseObject, error) {
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
func (a *AlarmsServer) GetSubscriptions(ctx context.Context, request api.GetSubscriptionsRequestObject) (api.GetSubscriptionsResponseObject, error) {
	options := commonapi.NewFieldOptions(request.Params.AllFields, request.Params.Fields, request.Params.ExcludeFields)
	if err := options.Validate(api.AlarmSubscriptionInfo{}); err != nil {
		return api.GetSubscriptions400ApplicationProblemPlusJSONResponse{
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}, nil
	}

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
	if err := commonapi.ValidateCallbackURL(ctx, a.SubscriptionEventHandler.GetClientFactory(), request.Body.Callback); err != nil {
		return api.CreateSubscription400ApplicationProblemPlusJSONResponse{
			AdditionalAttributes: &map[string]string{
				"callback": request.Body.Callback,
			},
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}, nil
	}

	record, err := a.AlarmsRepository.CreateAlarmSubscription(ctx, models.ConvertSubscriptionAPIToModel(request.Body))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation && pgErr.ConstraintName == "unique_callback" {
			// 409 is a more common choice for a duplicate entry, but the conformance tests expect a 400
			return api.CreateSubscription400ApplicationProblemPlusJSONResponse(common.ProblemDetails{
				AdditionalAttributes: &map[string]string{
					"callback": request.Body.Callback,
				},
				Detail: "callback value must be unique",
				Status: http.StatusBadRequest,
			}), nil
		}
		return nil, fmt.Errorf("failed to create alarm subscription: %w", err)
	}

	// TODO make it event-driven with PG listen/notify
	// Signal the notifier to handle this new subscription
	a.SubscriptionEventHandler.SubscriptionEvent(ctx, &notifier.SubscriptionEvent{
		Removed:      false,
		Subscription: models.ConvertAlertSubToNotificationSub(record),
	})
	slog.Info("Successfully created Alarm Subscription", "record", record)
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

	// TODO make it event-driven with PG listen/notify
	// Signal the notifier to handle this subscription change
	a.SubscriptionEventHandler.SubscriptionEvent(ctx, &notifier.SubscriptionEvent{
		Removed:      true,
		Subscription: models.ConvertAlertSubToNotificationSub(&models.AlarmSubscription{SubscriptionID: request.AlarmSubscriptionId}),
	})
	slog.Info("Successfully deleted Alarm Subscription", "alarmSubscriptionId", request.AlarmSubscriptionId.String())
	return api.DeleteSubscription200Response{}, nil
}

// GetSubscription handles an API request to retrieve an Alarm Subscription
func (a *AlarmsServer) GetSubscription(ctx context.Context, request api.GetSubscriptionRequestObject) (api.GetSubscriptionResponseObject, error) {
	record, err := a.AlarmsRepository.GetAlarmSubscription(ctx, request.AlarmSubscriptionId)
	if errors.Is(err, svcutils.ErrNotFound) {
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
func (a *AlarmsServer) GetAlarms(ctx context.Context, request api.GetAlarmsRequestObject) (api.GetAlarmsResponseObject, error) {
	options := commonapi.NewFieldOptions(request.Params.AllFields, request.Params.Fields, request.Params.ExcludeFields)
	if err := options.Validate(api.AlarmEventRecord{}); err != nil {
		return api.GetAlarms400ApplicationProblemPlusJSONResponse{
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}, nil
	}

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
	if errors.Is(err, svcutils.ErrNotFound) {
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
	if errors.Is(err, svcutils.ErrNotFound) {
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

		// All our alarms have AUTOMATIC clearing type
		// TODO: support clearing type MANUAL alarms

		return api.PatchAlarm409ApplicationProblemPlusJSONResponse(common.ProblemDetails{
			AdditionalAttributes: &map[string]string{
				"alarmEventRecordId": request.AlarmEventRecordId.String(),
			},
			Detail: "cannot clear an alarm with clearing type other than MANUAL",
			Status: http.StatusConflict,
		}), nil
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
	}

	// Update the Alarm Event Record
	// Subscriber notification sent async
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

	// TODO make it event-driven with PG listen/notify
	// Update Cronjob
	if err := a.ServiceConfig.EnsureCleanupCronJob(ctx, patched); err != nil {
		return nil, fmt.Errorf("failed to start cleanup cronjob during AlarmServiceConfiguration patch: %w", err)
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

	// TODO make it event-driven with PG listen/notify
	// Update the Alarm Service Configuration
	updated, err := a.AlarmsRepository.UpdateServiceConfiguration(ctx, serviceConfigRecord.ID, &serviceConfigRecord)
	if err != nil {
		return nil, fmt.Errorf("failed to update Alarm Service Configuration: %w", err)
	}

	// Update Cronjob
	if err := a.ServiceConfig.EnsureCleanupCronJob(ctx, updated); err != nil {
		return nil, fmt.Errorf("failed to start cleanup cronjob during AlarmServiceConfiguration update: %w", err)
	}

	slog.Debug("Alarm Service Configuration updated", "retentionPeriod", updated.RetentionPeriod, "extensions", updated.Extensions)
	return api.UpdateAlarmServiceConfiguration200JSONResponse(models.ConvertServiceConfigurationToAPI(*updated)), nil

}

// AmNotification handles an API request coming from Alertmanager with CaaS alerts. This api is used internally.
// Note: the errors returned can also be view under alertmanager pod logs but also logging here for convenience
func (a *AlarmsServer) AmNotification(ctx context.Context, request api.AmNotificationRequestObject) (api.AmNotificationResponseObject, error) {
	// TODO: AM auto retries if it receives 5xx error code. That means any error, even if permanent (e.g postgres syntax), will be processed the same way. Once we have a better retry mechanism for pg, update all 5xx to 4xx as needed.

	if err := alertmanager.HandleAlerts(ctx, a.Infrastructure.Clients, a.AlarmsRepository, &request.Body.Alerts, alertmanager.Webhook); err != nil {
		msg := "failed to handle alerts"
		slog.Error(msg, "error", err)
		return nil, fmt.Errorf("%s: %w", msg, err)
	}

	// Subscriber notification sent async
	slog.Info("Successfully handled all alertmanager alerts")
	return api.AmNotification200Response{}, nil
}

func (a *AlarmsServer) HwNotification(ctx context.Context, request api.HwNotificationRequestObject) (api.HwNotificationResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}
