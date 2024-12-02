package internal

import (
	"context"
	"log/slog"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/alertmanager"

	"fmt"
	"net/http"

	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/resourceserver"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
)

type AlarmsServer struct {
	AlarmsRepository *repo.AlarmsRepository
	ResourceServer   *resourceserver.ResourceServer
}

// AlarmsServer implements StrictServerInterface. This ensures that we've conformed to the `StrictServerInterface` with a compile-time check
var _ api.StrictServerInterface = (*AlarmsServer)(nil)

func (a *AlarmsServer) GetSubscriptions(ctx context.Context, request api.GetSubscriptionsRequestObject) (api.GetSubscriptionsResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

func (a *AlarmsServer) CreateSubscription(ctx context.Context, request api.CreateSubscriptionRequestObject) (api.CreateSubscriptionResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

func (a *AlarmsServer) DeleteSubscription(ctx context.Context, request api.DeleteSubscriptionRequestObject) (api.DeleteSubscriptionResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

func (a *AlarmsServer) GetSubscription(ctx context.Context, request api.GetSubscriptionRequestObject) (api.GetSubscriptionResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
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

// AmNotification Used internally, alertmanager from ACM calls here.
// Note: the errors returned can also be view under alertmanager pod logs but also logging here for convenience
func (a *AlarmsServer) AmNotification(ctx context.Context, request api.AmNotificationRequestObject) (api.AmNotificationResponseObject, error) {
	// Audit the table with full list of alerts in the current payload. If missing set them to resolve
	if err := a.AlarmsRepository.ResolveNotificationIfNotInCurrent(ctx, request.Body); err != nil {
		msg := "failed to resolve notification that are not present"
		slog.Error(msg, "err", err)
		return nil, fmt.Errorf("%s: %w", msg, err)
	}

	// Get the definition data based on current set of Alert names and managed cluster ID
	alarmDefinitions, err := a.AlarmsRepository.GetAlarmDefinitions(ctx, request.Body)
	if err != nil {
		msg := "failed to get AlarmDefinitions"
		slog.Error(msg, "err", err)
		return nil, fmt.Errorf("%s: %w", msg, err)
	}

	// Combine possible definitions with events
	aerModels := alertmanager.ConvertAmToAlarmEventRecordModels(request.Body, alarmDefinitions)

	// Insert and update AlarmEventRecord
	if err := a.AlarmsRepository.UpsertAlarmEventRecord(ctx, aerModels); err != nil {
		msg := "failed to upsert AlarmEventRecord to db"
		slog.Error(msg, "err", err)
		return nil, fmt.Errorf("%s: %w", msg, err)
	}

	//TODO: Get subscriber

	//TODO: Notify subscriber

	// Removed resolved events and move to archive
	if err := a.AlarmsRepository.ArchiveResolvedAlarmEventRecords(ctx); err != nil {
		msg := "failed to archive AlarmEventRecord to db"
		slog.Error(msg, "err", err)
		return nil, fmt.Errorf("%s: %w", msg, err)
	}

	return api.AmNotification200Response{}, nil
}

func (a *AlarmsServer) HwNotification(ctx context.Context, request api.HwNotificationRequestObject) (api.HwNotificationResponseObject, error) {
	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}

func convertAerModelToApi(aerModel models.AlarmEventRecord) api.AlarmEventRecord {
	return api.AlarmEventRecord{
		AlarmAcknowledged:     aerModel.AlarmAcknowledged,
		AlarmAcknowledgedTime: aerModel.AlarmAcknowledgedTime,
		AlarmChangedTime:      aerModel.AlarmChangedTime,
		AlarmClearedTime:      aerModel.AlarmClearedTime,
		AlarmDefinitionId:     aerModel.AlarmDefinitionID,
		AlarmEventRecordId:    *aerModel.AlarmEventRecordID,
		AlarmRaisedTime:       aerModel.AlarmRaisedTime,
		PerceivedSeverity:     aerModel.PerceivedSeverity,
		ProbableCauseId:       aerModel.ProbableCauseID,
		ResourceTypeID:        aerModel.ResourceTypeID,
		Extensions:            aerModel.Extensions,
	}
}
