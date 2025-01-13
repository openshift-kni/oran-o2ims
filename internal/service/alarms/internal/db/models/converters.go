package models

import (
	"fmt"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"

	"github.com/google/uuid"
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
		record.AlarmDefinitionId = *aerModel.AlarmDefinitionID
	}
	if aerModel.ProbableCauseID != nil {
		record.ProbableCauseId = *aerModel.ProbableCauseID
	}
	if aerModel.ObjectTypeID != nil {
		record.ResourceTypeID = *aerModel.ObjectTypeID
	}

	return record
}

// ConvertAlarmEventRecordModelToAlarmEventNotification converts an AlarmEventRecord to api AlarmEventNotification
func ConvertAlarmEventRecordModelToAlarmEventNotification(aerModel AlarmEventRecord, subModel AlarmSubscription, globalCloudID uuid.UUID) api.AlarmEventNotification {
	or := fmt.Sprintf("%s/alarms/%v", "/o2ims-infrastructureMonitoring/v1", aerModel.AlarmEventRecordID.String())
	notification := api.AlarmEventNotification{
		AlarmAcknowledgeTime:   aerModel.AlarmAcknowledgedTime,
		AlarmAcknowledged:      aerModel.AlarmAcknowledged,
		AlarmEventRecordId:     aerModel.AlarmEventRecordID,
		AlarmRaisedTime:        aerModel.AlarmRaisedTime,
		ConsumerSubscriptionId: subModel.ConsumerSubscriptionID,
		Extensions:             aerModel.Extensions,
		GlobalCloudID:          globalCloudID,
		NotificationEventType:  AlarmFilterToEventType(aerModel.NotificationEventType),
		ObjectRef:              &or,
		PerceivedSeverity:      aerModel.PerceivedSeverity,
	}

	// Handle all pointer fields together
	if aerModel.AlarmChangedTime != nil {
		notification.AlarmChangedTime = *aerModel.AlarmChangedTime
	}
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

	return notification
}

// ConvertServiceConfigurationToAPI converts an ServiceConfiguration DB model to an API model
func ConvertServiceConfigurationToAPI(config ServiceConfiguration) api.AlarmServiceConfiguration {
	apiModel := api.AlarmServiceConfiguration{
		RetentionPeriod: config.RetentionPeriod,
	}

	if config.Extensions != nil {
		apiModel.Extensions = &config.Extensions
	}

	return apiModel
}

// ConvertSubscriptionModelToApi converts an AlarmSubscription DB model to an API model
func ConvertSubscriptionModelToApi(subscriptionModel AlarmSubscription) api.AlarmSubscriptionInfo {
	apiModel := api.AlarmSubscriptionInfo{
		SubscriptionID:         &subscriptionModel.SubscriptionID,
		Callback:               subscriptionModel.Callback,
		ConsumerSubscriptionId: subscriptionModel.ConsumerSubscriptionID,
	}

	if subscriptionModel.Filter != nil {
		apiModel.Filter = subscriptionModel.Filter
	}

	return apiModel
}

// ConvertSubscriptionAPIToModel converts an AlarmSubscription API model to a DB model
func ConvertSubscriptionAPIToModel(subscriptionAPI *api.AlarmSubscriptionInfo) AlarmSubscription {
	return AlarmSubscription{
		Callback:               subscriptionAPI.Callback,
		ConsumerSubscriptionID: subscriptionAPI.ConsumerSubscriptionId,
		Filter:                 subscriptionAPI.Filter,
	}
}

// AlarmFilterToEventType map text to int e.g NEW -> 0
func AlarmFilterToEventType(filter api.AlarmSubscriptionInfoFilter) api.AlarmEventNotificationNotificationEventType {
	switch filter {
	case api.AlarmSubscriptionInfoFilterNEW:
		return api.AlarmEventNotificationNotificationEventTypeNEW
	case api.AlarmSubscriptionInfoFilterCHANGE:
		return api.AlarmEventNotificationNotificationEventTypeCHANGE
	case api.AlarmSubscriptionInfoFilterCLEAR:
		return api.AlarmEventNotificationNotificationEventTypeCLEAR
	case api.AlarmSubscriptionInfoFilterACKNOWLEDGE:
		return api.AlarmEventNotificationNotificationEventTypeACKNOWLEDGE
	default:
		return api.AlarmEventNotificationNotificationEventTypeNEW
	}
}

// ConvertAlertSubToNotificationSub alarms subscription to generic subscription
func ConvertAlertSubToNotificationSub(as *AlarmSubscription) *notifier.SubscriptionInfo {
	info := notifier.SubscriptionInfo{
		SubscriptionID: as.SubscriptionID,
		Callback:       as.Callback,
		EventCursor:    int(as.EventCursor),
	}

	if as.Filter != nil {
		info.Filter = (*string)(as.Filter)
	}
	return &info
}

// ConvertAlarmDefinitionToProbableCause probableCause is subset of alarm definition
func ConvertAlarmDefinitionToProbableCause(def AlarmDefinition) api.ProbableCause {
	return api.ProbableCause{
		Description:     def.AlarmDescription,
		Name:            def.AlarmName,
		ProbableCauseId: def.ProbableCauseID,
	}
}
