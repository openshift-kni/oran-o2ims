package models

import (
	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
)

// ConvertAerModelToApi converts an AlarmEventRecord DB model to an API model
func ConvertAerModelToApi(aerModel AlarmEventRecord) api.AlarmEventRecord {
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

// ConvertSubsModelToApi converts an AlarmSubscription DB model to an API model
func ConvertSubsModelToApi(subsModel AlarmSubscription) api.AlarmSubscriptionInfo {
	apiModel := api.AlarmSubscriptionInfo{
		SubscriptionID:         &subsModel.SubscriptionID,
		Callback:               subsModel.Callback,
		ConsumerSubscriptionId: subsModel.ConsumerSubscriptionID,
	}

	if subsModel.Filter != nil {
		*apiModel.Filter = api.AlarmSubscriptionInfoFilter(*subsModel.Filter)
	}

	return apiModel
}
