/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package models

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"

	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
)

// ConvertAlarmEventRecordModelToApi converts an AlarmEventRecord DB model to an API model
func ConvertAlarmEventRecordModelToApi(aerModel AlarmEventRecord) api.AlarmEventRecord {
	record := api.AlarmEventRecord{
		AlarmAcknowledged:     aerModel.AlarmAcknowledged,
		AlarmAcknowledgedTime: aerModel.AlarmAcknowledgedTime,
		AlarmChangedTime:      aerModel.AlarmChangedTime,
		AlarmClearedTime:      aerModel.AlarmClearedTime,
		AlarmEventRecordId:    aerModel.AlarmEventRecordID,
		AlarmRaisedTime:       aerModel.AlarmRaisedTime,
		PerceivedSeverity:     aerModel.PerceivedSeverity,
		Extensions:            aerModel.Extensions,
	}

	if aerModel.AlarmDefinitionID != nil {
		record.AlarmDefinitionID = *aerModel.AlarmDefinitionID
	}
	if aerModel.ProbableCauseID != nil {
		record.ProbableCauseID = *aerModel.ProbableCauseID
	}
	if aerModel.ObjectTypeID != nil {
		record.ResourceTypeID = *aerModel.ObjectTypeID
	}
	if aerModel.ObjectID != nil {
		record.ResourceID = *aerModel.ObjectID
	}
	if aerModel.ObjectClass != nil {
		record.ObjectClass = aerModel.ObjectClass
	}

	return record
}

// timePtrToNullable converts an optional time to a Nullable that serializes as a value or
// explicit null (never omitted), matching the M/0..1 notification time fields (C4).
func timePtrToNullable(t *time.Time) nullable.Nullable[time.Time] {
	if t == nil {
		return nullable.NewNullNullable[time.Time]()
	}
	return nullable.NewNullableWithValue(*t)
}

// ConvertAlarmEventRecordModelToAlarmEventNotification converts an AlarmEventRecord to api AlarmEventNotification
func ConvertAlarmEventRecordModelToAlarmEventNotification(aerModel AlarmEventRecord, globalCloudID uuid.UUID) api.AlarmEventNotification {
	// objectRef is built root-relative: O2IMSMonitoringBaseURL is a path prefix (no host), so
	// consumers that require an absolute URL must prepend the O-Cloud base URL. The absence of a
	// configurable public base URL is a known interoperability limitation (C5(d)).
	or := fmt.Sprintf("%s%s/%v", constants.O2IMSMonitoringBaseURL, constants.AlarmsPath, aerModel.AlarmEventRecordID.String())
	notification := api.AlarmEventNotification{
		AlarmAcknowledged:     aerModel.AlarmAcknowledged,
		AlarmAcknowledgeTime:  timePtrToNullable(aerModel.AlarmAcknowledgedTime),
		AlarmChangedTime:      timePtrToNullable(aerModel.AlarmChangedTime),
		AlarmClearedTime:      timePtrToNullable(aerModel.AlarmClearedTime),
		AlarmEventRecordId:    aerModel.AlarmEventRecordID,
		AlarmRaisedTime:       aerModel.AlarmRaisedTime,
		Extensions:            aerModel.Extensions,
		GlobalCloudID:         globalCloudID,
		NotificationEventType: AlarmFilterToEventType(aerModel.NotificationEventType),
		ObjectRef:             nullable.NewNullableWithValue(or),
		// consumerSubscriptionId is per-subscriber: the base notification serializes it as an
		// explicit null and Transform overrides it for subscribers that provided one (C4).
		ConsumerSubscriptionId: nullable.NewNullNullable[string](),
		PerceivedSeverity:      aerModel.PerceivedSeverity,
	}

	// Handle all pointer fields together
	if aerModel.AlarmDefinitionID != nil {
		notification.AlarmDefinitionID = *aerModel.AlarmDefinitionID
	}
	if aerModel.ProbableCauseID != nil {
		notification.ProbableCauseID = *aerModel.ProbableCauseID
	}
	if aerModel.ObjectID != nil {
		notification.ResourceID = *aerModel.ObjectID
	}
	if aerModel.ObjectTypeID != nil {
		notification.ResourceTypeID = *aerModel.ObjectTypeID
	}
	if aerModel.ObjectClass != nil {
		notification.ObjectClass = *aerModel.ObjectClass
	}

	return notification
}

// ConvertServiceConfigurationToAPI converts an ServiceConfiguration DB model to an API model
func ConvertServiceConfigurationToAPI(config ServiceConfiguration) api.AlarmServiceConfiguration {
	apiModel := api.AlarmServiceConfiguration{
		RetentionPeriod: config.RetentionPeriod,
	}

	if config.Extensions != nil {
		apiModel.Extensions = config.Extensions
	}

	return apiModel
}

// ConvertSubscriptionModelToApi converts an AlarmSubscription DB model to an API model
func ConvertSubscriptionModelToApi(subscriptionModel AlarmSubscription) api.AlarmSubscriptionInfo {
	apiModel := api.AlarmSubscriptionInfo{
		AlarmSubscriptionId:    &subscriptionModel.SubscriptionID,
		Callback:               subscriptionModel.Callback,
		ConsumerSubscriptionId: subscriptionModel.ConsumerSubscriptionID,
	}

	// eventFilter is M/0..1: always serialize the key, as a value or explicit null.
	if subscriptionModel.EventFilter != nil {
		apiModel.EventFilter = nullable.NewNullableWithValue(*subscriptionModel.EventFilter)
	} else {
		apiModel.EventFilter = nullable.NewNullNullable[api.AlarmSubscriptionInfoEventFilter]()
	}

	return apiModel
}

// ConvertSubscriptionAPIToModel converts an AlarmSubscription API model to a DB model
func ConvertSubscriptionAPIToModel(subscriptionAPI *api.AlarmSubscriptionInfo) AlarmSubscription {
	model := AlarmSubscription{
		Callback:               subscriptionAPI.Callback,
		ConsumerSubscriptionID: subscriptionAPI.ConsumerSubscriptionId,
	}

	// Only persist event_filter when a concrete value is provided; an omitted or
	// explicit-null eventFilter leaves the column NULL (never call Get() unguarded).
	if subscriptionAPI.EventFilter.IsSpecified() && !subscriptionAPI.EventFilter.IsNull() {
		v, _ := subscriptionAPI.EventFilter.Get()
		model.EventFilter = &v
	}

	return model
}

// AlarmFilterToEventType map text to int e.g NEW -> 0
func AlarmFilterToEventType(filter api.AlarmSubscriptionInfoEventFilter) api.AlarmEventNotificationNotificationEventType {
	switch filter {
	case api.AlarmSubscriptionInfoEventFilterNEW:
		return api.AlarmEventNotificationNotificationEventTypeNEW
	case api.AlarmSubscriptionInfoEventFilterCHANGE:
		return api.AlarmEventNotificationNotificationEventTypeCHANGE
	case api.AlarmSubscriptionInfoEventFilterCLEAR:
		return api.AlarmEventNotificationNotificationEventTypeCLEAR
	case api.AlarmSubscriptionInfoEventFilterACKNOWLEDGE:
		return api.AlarmEventNotificationNotificationEventTypeACKNOWLEDGE
	default:
		return api.AlarmEventNotificationNotificationEventTypeNEW
	}
}

// ConvertAlertSubToNotificationSub alarms subscription to generic subscription
func ConvertAlertSubToNotificationSub(as *AlarmSubscription) *notifier.SubscriptionInfo {
	info := notifier.SubscriptionInfo{
		SubscriptionID:         as.SubscriptionID,
		ConsumerSubscriptionID: as.ConsumerSubscriptionID,
		Callback:               as.Callback,
		EventCursor:            int(as.EventCursor),
	}

	// Effective filter: a set event_filter excludes its event type; a NULL event_filter
	// matches all events (C3). The notifier SubscriptionInfo exposes a single generic Filter
	// field, so the effective eventFilter is mapped onto it here.
	if as.EventFilter != nil {
		info.Filter = (*string)(as.EventFilter)
	}
	return &info
}

// DataChangeEventToNotification converts a DataChangeEvent to a generic Notification.
// AlarmEventRecord is converted to AlarmEventNotification which becomes the final notification payload.
func DataChangeEventToNotification(record *models.DataChangeEvent, globalCloudID uuid.UUID) (*notifier.Notification, error) {
	if record == nil {
		return nil, fmt.Errorf("cannot convert nil record")
	}

	if record.AfterState == nil {
		return nil, fmt.Errorf("after_state is nil")
	}

	data, err := json.Marshal(record.AfterState)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal after_state: %w", err)
	}

	var alarm AlarmEventRecord
	if err := json.Unmarshal(data, &alarm); err != nil {
		return nil, fmt.Errorf("error unmarshalling alarm event: %w", err)
	}

	return &notifier.Notification{
		NotificationID: *record.DataChangeID,
		SequenceID:     *record.SequenceID,
		Payload:        ConvertAlarmEventRecordModelToAlarmEventNotification(alarm, globalCloudID),
	}, nil
}
