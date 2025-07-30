/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package models

import (
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/api/generated"
	commonapi "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
)

// ClusterResourceToModel converts a DB tuple to an API model
func ClusterResourceToModel(record *ClusterResource, options *commonapi.FieldOptions) generated.ClusterResource {
	result := generated.ClusterResource{
		ArtifactResourceIds:   record.ArtifactResourceIDs,
		ClusterResourceId:     record.ClusterResourceID,
		ClusterResourceTypeId: record.ClusterResourceTypeID,
		Description:           record.Description,
		Name:                  record.Name,
		ResourceId:            record.ResourceID,
	}

	if options.IsIncluded(commonapi.ExtensionsAttribute) {
		if record.Extensions == nil {
			extensions := make(map[string]interface{})
			result.Extensions = &extensions
		} else {
			result.Extensions = record.Extensions
		}
	}

	if options.IsIncluded(commonapi.MemberOfAttribute) {
		// TODO
		value := make([]string, 0)
		result.MemberOf = &value
	}

	return result
}

// ClusterResourceTypeToModel converts a DB tuple to an API model
func ClusterResourceTypeToModel(record *ClusterResourceType, options *commonapi.FieldOptions) generated.ClusterResourceType {
	result := generated.ClusterResourceType{
		ClusterResourceTypeId: record.ClusterResourceTypeID,
		Description:           record.Description,
		Name:                  record.Name,
	}

	if options.IsIncluded(commonapi.ExtensionsAttribute) {
		if record.Extensions == nil {
			extensions := make(map[string]interface{})
			result.Extensions = &extensions
		} else {
			result.Extensions = record.Extensions
		}
	}

	return result
}

// NodeClusterToModel converts a DB tuple to an API model
func NodeClusterToModel(record *NodeCluster, clusterResourceIDs []uuid.UUID, options *commonapi.FieldOptions) generated.NodeCluster {
	object := generated.NodeCluster{
		NodeClusterId:                  record.NodeClusterID,
		NodeClusterTypeId:              record.NodeClusterTypeID,
		ArtifactResourceId:             record.ArtifactResourceID,
		ClientNodeClusterId:            record.ClientNodeClusterID,
		ClusterDistributionDescription: record.ClusterDistributionDescription,
		ClusterResourceGroups:          record.ClusterResourceGroups,
		Description:                    record.Description,
		Name:                           record.Name,
	}

	object.ClusterResourceIds = clusterResourceIDs
	if clusterResourceIDs == nil {
		object.ClusterResourceIds = []uuid.UUID{}
	}

	if options.IsIncluded(commonapi.ExtensionsAttribute) {
		if record.Extensions == nil {
			extensions := make(map[string]interface{})
			object.Extensions = &extensions
		} else {
			object.Extensions = record.Extensions
		}
	}

	return object
}

// NodeClusterTypeToModel converts a DB tuple to an API model
func NodeClusterTypeToModel(record *NodeClusterType, options *commonapi.FieldOptions) generated.NodeClusterType {
	result := generated.NodeClusterType{
		Description:       record.Description,
		Name:              record.Name,
		NodeClusterTypeId: record.NodeClusterTypeID,
	}

	if options.IsIncluded(commonapi.ExtensionsAttribute) {
		if record.Extensions == nil {
			extensions := make(map[string]interface{})
			result.Extensions = &extensions
		} else {
			result.Extensions = record.Extensions
		}
	}

	return result
}

// SubscriptionToModel converts a DB tuple to an API Model
func SubscriptionToModel(record *models.Subscription) generated.Subscription {
	object := generated.Subscription{
		Callback:               record.Callback,
		ConsumerSubscriptionId: record.ConsumerSubscriptionID,
		Filter:                 record.Filter,
		SubscriptionId:         record.SubscriptionID,
	}

	return object
}

// SubscriptionFromModel converts an API model to a DB tuple
func SubscriptionFromModel(object *generated.Subscription) *models.Subscription {
	id := uuid.Must(uuid.NewRandom())

	record := models.Subscription{
		SubscriptionID:         &id,
		ConsumerSubscriptionID: object.ConsumerSubscriptionId,
		Filter:                 object.Filter,
		Callback:               object.Callback,
		EventCursor:            0,
	}

	return &record
}

// SubscriptionToInfo converts a Subscription to a generic SubscriptionInfo
func SubscriptionToInfo(record *models.Subscription) *notifier.SubscriptionInfo {
	return &notifier.SubscriptionInfo{
		SubscriptionID:         *record.SubscriptionID,
		ConsumerSubscriptionID: record.ConsumerSubscriptionID,
		Callback:               record.Callback,
		Filter:                 record.Filter,
		EventCursor:            record.EventCursor,
	}
}

// getEventType determines the event type based on the object transition
func getEventType(before, after map[string]interface{}) int {
	switch {
	case before == nil && after != nil:
		return 0
	case before != nil && after != nil:
		return 1
	case before != nil:
		return 2
	default:
		slog.Warn("unsupported event type", "before", before, "after", after)
		return -1
	}
}

// getObjectReference builds a partial URL referencing the API path location of the object
func getObjectReference(objectType string, objectID uuid.UUID) *string {
	var value string
	switch objectType {
	case ClusterResourceType{}.TableName():
		value = fmt.Sprintf("%s/clusterResourceTypes/%s", constants.O2IMSClusterBaseURL, objectID.String())
	case ClusterResource{}.TableName():
		value = fmt.Sprintf("%s/clusterResource/%s", constants.O2IMSClusterBaseURL, objectID.String())
	case NodeClusterType{}.TableName():
		value = fmt.Sprintf("%s/nodeClusterTypes/%s", constants.O2IMSClusterBaseURL, objectID.String())
	case NodeCluster{}.TableName():
		value = fmt.Sprintf("%s/nodeClusters/%s", constants.O2IMSClusterBaseURL, objectID.String())
	default:
		return nil
	}

	return &value
}

// DataChangeEventToModel converts a DB tuple to an API model
func DataChangeEventToModel(record *models.DataChangeEvent) generated.ClusterChangeNotification {
	eventType := getEventType(record.BeforeState, record.AfterState)
	object := generated.ClusterChangeNotification{
		NotificationEventType: generated.ClusterChangeNotificationNotificationEventType(eventType),
		NotificationId:        *record.DataChangeID,
		ObjectRef:             getObjectReference(record.ObjectType, record.ObjectID),
	}

	if record.AfterState != nil {
		object.PostObjectState = &record.AfterState
	}

	if record.BeforeState != nil {
		object.PriorObjectState = &record.BeforeState
	}

	return object
}

// DataChangeEventToNotification converts a DataChangeEvent to a generic Notification
func DataChangeEventToNotification(record *models.DataChangeEvent) *notifier.Notification {
	return &notifier.Notification{
		NotificationID: *record.DataChangeID,
		SequenceID:     *record.SequenceID,
		Payload:        DataChangeEventToModel(record),
	}
}

func AlarmDictionaryToModel(record *models.AlarmDictionary, alarmDefinitionRecords []models.AlarmDefinition) common.AlarmDictionary {
	alarmDictionary := common.AlarmDictionary{
		AlarmDictionaryId:            record.AlarmDictionaryID,
		AlarmDictionaryVersion:       record.AlarmDictionaryVersion,
		AlarmDictionarySchemaVersion: record.AlarmDictionarySchemaVersion,
		EntityType:                   record.EntityType,
		Vendor:                       record.Vendor,
		PkNotificationField:          record.PKNotificationField,
	}

	for _, interfaceID := range record.ManagementInterfaceID {
		alarmDictionary.ManagementInterfaceId = append(alarmDictionary.ManagementInterfaceId, common.AlarmDictionaryManagementInterfaceId(interfaceID))
	}

	// If there are no alarm definitions, return the dictionary with an empty slice
	if len(alarmDefinitionRecords) == 0 {
		alarmDictionary.AlarmDefinition = []common.AlarmDefinition{}
		return alarmDictionary
	}

	for _, alarmDefinitionRecord := range alarmDefinitionRecords {
		alarmDefinition := common.AlarmDefinition{
			AlarmDefinitionId:     alarmDefinitionRecord.AlarmDefinitionID,
			AlarmName:             alarmDefinitionRecord.AlarmName,
			AlarmLastChange:       alarmDefinitionRecord.AlarmLastChange,
			AlarmChangeType:       common.AlarmDefinitionAlarmChangeType(alarmDefinitionRecord.AlarmChangeType),
			AlarmDescription:      alarmDefinitionRecord.AlarmDescription,
			ProposedRepairActions: alarmDefinitionRecord.ProposedRepairActions,
			ClearingType:          common.AlarmDefinitionClearingType(alarmDefinitionRecord.ClearingType),
			PkNotificationField:   alarmDefinitionRecord.PKNotificationField,
			AlarmAdditionalFields: alarmDefinitionRecord.AlarmAdditionalFields,
		}

		for _, interfaceID := range alarmDefinitionRecord.ManagementInterfaceID {
			alarmDefinition.ManagementInterfaceId = append(alarmDefinition.ManagementInterfaceId, common.AlarmDefinitionManagementInterfaceId(interfaceID))
		}

		alarmDictionary.AlarmDefinition = append(alarmDictionary.AlarmDefinition, alarmDefinition)
	}

	return alarmDictionary
}
