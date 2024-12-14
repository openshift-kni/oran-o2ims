package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/resourceserver"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

type AlarmsServer struct {
	// GlobalCloudID is the global O-Cloud identifier. Create subscription requests are blocked if the global O-Cloud identifier is not set
	GlobalCloudID uuid.UUID
	// AlarmsRepository is the repository for the alarms
	AlarmsRepository *repo.AlarmsRepository
	// ResourceServer contains the resources server client and fetched resources
	ResourceServer *resourceserver.ResourceServer
}

// AlarmsServer implements StrictServerInterface. This ensures that we've conformed to the `StrictServerInterface` with a compile-time check
var _ api.StrictServerInterface = (*AlarmsServer)(nil)

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

func (a *AlarmsServer) GetAlarms(ctx context.Context, request api.GetAlarmsRequestObject) (api.GetAlarmsResponseObject, error) {
	// TODO implement me

	// Fill out more details
	p := common.ProblemDetails{
		Detail: "invalid `filter` parameter syntax",
		Status: http.StatusBadRequest,
	}
	return api.GetAlarms400ApplicationProblemPlusJSONResponse(p), nil
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

func (a *AlarmsServer) AckAlarm(ctx context.Context, request api.AckAlarmRequestObject) (api.AckAlarmResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

func (a *AlarmsServer) GetProbableCauses(ctx context.Context, request api.GetProbableCausesRequestObject) (api.GetProbableCausesResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

func (a *AlarmsServer) GetProbableCause(ctx context.Context, request api.GetProbableCauseRequestObject) (api.GetProbableCauseResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

func (a *AlarmsServer) AmNotification(ctx context.Context, request api.AmNotificationRequestObject) (api.AmNotificationResponseObject, error) {
	// TODO: Implement the logic to handle the AM notification
	slog.Debug("Received AM notification", "groupLabels", request.Body.GroupLabels)
	for _, alert := range request.Body.Alerts {
		slog.Debug("Alert", "fingerprint", alert.Fingerprint, "startsAt", alert.StartsAt, "status", alert.Status)
	}

	return api.AmNotification200Response{}, nil
}

func (a *AlarmsServer) HwNotification(ctx context.Context, request api.HwNotificationRequestObject) (api.HwNotificationResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}
