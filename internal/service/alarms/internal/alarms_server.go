package internal

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
)

type AlarmsServer struct {
	AlarmsRepository *AlarmsRepository
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
	p := api.ProblemDetails{
		Detail: "invalid `filter` parameter syntax",
		Status: http.StatusBadRequest,
	}
	return api.GetAlarms400ApplicationProblemPlusJSONResponse(p), nil
}

func (a *AlarmsServer) GetAlarm(ctx context.Context, request api.GetAlarmRequestObject) (api.GetAlarmResponseObject, error) {
	// TODO implement me
	alarm := api.AlarmEventRecord{
		AlarmAcknowledged:     false,
		AlarmAcknowledgedTime: nil,
		AlarmChangedTime:      nil,
		AlarmClearedTime:      nil,
		AlarmDefinitionId:     uuid.New(),
		AlarmEventRecordId:    uuid.New(),
		AlarmRaisedTime:       time.Now(),
		PerceivedSeverity:     0,
		ProbableCauseId:       uuid.New(),
		ResourceTypeID:        uuid.New(),
	}

	return api.GetAlarm200JSONResponse(alarm), nil
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
	// TODO implement me
	panic("implement me")
}

func (a *AlarmsServer) HwNotification(ctx context.Context, request api.HwNotificationRequestObject) (api.HwNotificationResponseObject, error) {
	// TODO implement me
	panic("implement me")
}
