package internal

import (
	"context"

	"log/slog"

	"fmt"
	"net/http"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"

	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"

	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
)

type AlarmsServer struct {
	AlarmsRepository *repo.AlarmsRepository
}

// AlarmsServer implements StrictServerInterface. This ensures that we've conformed to the `StrictServerInterface` with a compile-time check
var _ api.StrictServerInterface = (*AlarmsServer)(nil)

func (a *AlarmsServer) GetSubscriptions(ctx context.Context, request api.GetSubscriptionsRequestObject) (api.GetSubscriptionsResponseObject, error) {
	// TODO implement me
	panic("implement me")
}

func (a *AlarmsServer) CreateSubscription(ctx context.Context, request api.CreateSubscriptionRequestObject) (api.CreateSubscriptionResponseObject, error) {
	// TODO implement me
	panic("implement me")
}

func (a *AlarmsServer) DeleteSubscription(ctx context.Context, request api.DeleteSubscriptionRequestObject) (api.DeleteSubscriptionResponseObject, error) {
	// TODO implement me
	panic("implement me")
}

func (a *AlarmsServer) GetSubscription(ctx context.Context, request api.GetSubscriptionRequestObject) (api.GetSubscriptionResponseObject, error) {
	// TODO implement me
	panic("implement me")
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

// GetAlarm returns an AlarmEventRecord with a given ID
func (a *AlarmsServer) GetAlarm(ctx context.Context, request api.GetAlarmRequestObject) (api.GetAlarmResponseObject, error) {
	aerModel, err := a.AlarmsRepository.GetAlarmEventRecordWithUuid(ctx, request.AlarmEventRecordId)
	if err != nil {
		return nil, fmt.Errorf("failed to get AlarmEventRecord due to issues with DB conn: %w", err)
	}

	// Nothing found
	if len(aerModel) == 0 {
		return api.GetAlarm404ApplicationProblemPlusJSONResponse(common.ProblemDetails{
			AdditionalAttributes: &map[string]string{
				"UUID": request.AlarmEventRecordId.String(),
			},
			Detail: "Could not find AlarmEventRecord for given UUID",
			Status: http.StatusNotFound,
		}), nil
	}

	return api.GetAlarm200JSONResponse(convertAerModelToApi(aerModel[0])), nil
}

func (a *AlarmsServer) AckAlarm(ctx context.Context, request api.AckAlarmRequestObject) (api.AckAlarmResponseObject, error) {
	// TODO implement me
	panic("implement me")
}

func (a *AlarmsServer) GetProbableCauses(ctx context.Context, request api.GetProbableCausesRequestObject) (api.GetProbableCausesResponseObject, error) {
	// TODO implement me
	panic("implement me")
}

func (a *AlarmsServer) GetProbableCause(ctx context.Context, request api.GetProbableCauseRequestObject) (api.GetProbableCauseResponseObject, error) {
	// TODO implement me
	panic("implement me")
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
	panic("implement me")
}

func convertAerModelToApi(aerModel models.AlarmEventRecord) api.AlarmEventRecord {
	return api.AlarmEventRecord{
		AlarmAcknowledged:     aerModel.AlarmAcknowledged,
		AlarmAcknowledgedTime: aerModel.AlarmAcknowledgedTime,
		AlarmChangedTime:      aerModel.AlarmChangedTime,
		AlarmClearedTime:      aerModel.AlarmClearedTime,
		AlarmDefinitionId:     aerModel.AlarmDefinitionID,
		AlarmEventRecordId:    aerModel.AlarmEventRecordID,
		AlarmRaisedTime:       aerModel.AlarmRaisedTime,
		PerceivedSeverity:     api.PerceivedSeverity(aerModel.PerceivedSeverity),
		ProbableCauseId:       aerModel.ProbableCauseID,
		ResourceTypeID:        aerModel.ResourceTypeID,
	}
}
