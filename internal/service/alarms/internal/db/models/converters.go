package models

import (
	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
)

// ConvertAlarmEventRecordModelToApi converts an AlarmEventRecord DB model to an API model
func ConvertAlarmEventRecordModelToApi(aerModel AlarmEventRecord) api.AlarmEventRecord {
	return api.AlarmEventRecord{
		AlarmAcknowledged:     aerModel.AlarmAcknowledged,
		AlarmAcknowledgedTime: aerModel.AlarmAcknowledgedTime,
		AlarmChangedTime:      aerModel.AlarmChangedTime,
		AlarmClearedTime:      aerModel.AlarmClearedTime,
		AlarmDefinitionId:     *aerModel.AlarmDefinitionID,
		AlarmEventRecordId:    aerModel.AlarmEventRecordID,
		AlarmRaisedTime:       aerModel.AlarmRaisedTime,
		PerceivedSeverity:     aerModel.PerceivedSeverity,
		ProbableCauseId:       *aerModel.ProbableCauseID,
		ResourceTypeID:        *aerModel.ObjectTypeID,
		Extensions:            aerModel.Extensions,
	}
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
		*apiModel.Filter = api.AlarmSubscriptionInfoFilter(*subscriptionModel.Filter)
	}

	return apiModel
}

// ConvertSubscriptionAPIToModel converts an AlarmSubscription API model to a DB model
func ConvertSubscriptionAPIToModel(subscriptionAPI *api.AlarmSubscriptionInfo) AlarmSubscription {
	return AlarmSubscription{
		Callback:               subscriptionAPI.Callback,
		ConsumerSubscriptionID: subscriptionAPI.ConsumerSubscriptionId,
		Filter:                 (*string)(subscriptionAPI.Filter),
	}
}
