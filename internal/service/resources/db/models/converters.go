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
	commonapi "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
	models2 "github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/api/generated"
)

// DeploymentManagerToModel converts a DB tuple to an API Model
func DeploymentManagerToModel(record *DeploymentManager, options *commonapi.FieldOptions) generated.DeploymentManager {
	object := generated.DeploymentManager{
		Capabilities:        map[string]string{},
		Capacity:            map[string]string{},
		DeploymentManagerId: record.DeploymentManagerID,
		Description:         record.Description,
		Name:                record.Name,
		OCloudId:            record.OCloudID,
		ServiceUri:          record.URL,
		SupportedLocations:  record.Locations,
	}

	if options.IsIncluded(commonapi.ExtensionsAttribute) {
		if record.Extensions == nil {
			extensions := make(map[string]interface{})
			object.Extensions = &extensions
		} else {
			object.Extensions = &record.Extensions
		}
	}

	if options.IsIncluded(commonapi.CapacityAttribute) {
		if record.CapacityInfo != nil {
			object.Capacity = record.CapacityInfo
		} else {
			object.Capacity = map[string]string{}
		}
	}

	if options.IsIncluded(commonapi.CapabilitiesAttribute) {
		if record.Capabilities != nil {
			object.Capabilities = record.Capabilities
		} else {
			object.Capabilities = map[string]string{}
		}
	}

	return object
}

// ResourceTypeToModel converts a DB tuple to an API Model.
// If alarmDictionary is provided, it will be included in the response; otherwise the field will be nil.
func ResourceTypeToModel(record *ResourceType, alarmDictionary *common.AlarmDictionary) generated.ResourceType {
	object := generated.ResourceType{
		AlarmDictionary: alarmDictionary, // Deprecated by O-RAN.WG6.TS.O2IMS-INTERFACE-R005-v11.00
		Description:     record.Description,
		Model:           record.Model,
		Name:            record.Name,
		ResourceClass:   generated.ResourceTypeResourceClass(record.ResourceClass),
		ResourceKind:    generated.ResourceTypeResourceKind(record.ResourceKind),
		ResourceTypeId:  record.ResourceTypeID,
		Vendor:          record.Vendor,
		Version:         record.Version,
	}

	// Handle optional Extensions field
	if record.Extensions != nil {
		object.Extensions = &record.Extensions
	}

	// Populate alarmDictionaryId from the alarm dictionary if present
	// O-RAN.WG6.TS.O2IMS-INTERFACE-R005-v11.00
	if alarmDictionary != nil {
		object.AlarmDictionaryId = &alarmDictionary.AlarmDictionaryId
	}

	return object
}

// SubscriptionToModel converts a DB tuple to an API Model
func SubscriptionToModel(record *models2.Subscription) generated.Subscription {
	object := generated.Subscription{
		Callback:               record.Callback,
		ConsumerSubscriptionId: record.ConsumerSubscriptionID,
		Filter:                 record.Filter,
		SubscriptionId:         record.SubscriptionID,
	}

	return object
}

// SubscriptionFromModel converts an API model to a DB tuple
func SubscriptionFromModel(object *generated.Subscription) *models2.Subscription {
	id := uuid.Must(uuid.NewRandom())

	record := models2.Subscription{
		SubscriptionID:         &id,
		ConsumerSubscriptionID: object.ConsumerSubscriptionId,
		Filter:                 object.Filter,
		Callback:               object.Callback,
		EventCursor:            0,
	}

	return &record
}

// ResourcePoolToModel converts a DB tuple to an API model
func ResourcePoolToModel(record *ResourcePool, options *commonapi.FieldOptions) generated.ResourcePool {
	object := generated.ResourcePool{
		Description:      record.Description,
		GlobalLocationId: record.GlobalLocationID, // Deprecated by O-RAN.WG6.TS.O2IMS-INTERFACE-R005-v11.00
		Location:         record.Location,         // Deprecated by O-RAN.WG6.TS.O2IMS-INTERFACE-R005-v11.00
		Name:             record.Name,
		OCloudId:         record.OCloudID, // Deprecated by O-RAN.WG6.TS.O2IMS-INTERFACE-R005-v11.00
		ResourcePoolId:   record.ResourcePoolID,
	}

	if record.OCloudSiteID != nil {
		object.OCloudSiteId = *record.OCloudSiteID
	}

	if options.IsIncluded(commonapi.ExtensionsAttribute) {
		if record.Extensions == nil {
			extensions := make(map[string]interface{})
			object.Extensions = &extensions
		} else {
			object.Extensions = &record.Extensions
		}
	}

	return object
}

// ResourceToModel converts a DB tuple to an API model
func ResourceToModel(record *Resource, elements []Resource) generated.Resource {
	object := generated.Resource{
		Description:    record.Description,
		Extensions:     record.Extensions,
		ResourceId:     record.ResourceID,
		ResourcePoolId: record.ResourcePoolID,
		ResourceTypeId: record.ResourceTypeID,
	}

	if record.Groups != nil {
		object.Groups = *record.Groups
	}

	if record.Tags != nil {
		object.Tags = *record.Tags
	}

	if record.GlobalAssetID != nil {
		object.GlobalAssetId = *record.GlobalAssetID
	}

	if elements != nil {
		object.Elements = make([]generated.Resource, len(elements))
		for i, element := range elements {
			object.Elements[i] = ResourceToModel(&element, nil)
		}
	}
	return object
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
func getObjectReference(objectType string, objectID uuid.UUID, parentID *uuid.UUID) *string {
	var value string
	switch objectType {
	case ResourceType{}.TableName():
		value = fmt.Sprintf("%s%s/%s", constants.O2IMSInventoryBaseURL, constants.ResourceTypesPath, objectID.String())
	case Resource{}.TableName():
		value = fmt.Sprintf("%s%s/%s%s/%s", constants.O2IMSInventoryBaseURL, constants.ResourcePoolsPath, parentID.String(), constants.ResourcesPath, objectID.String())
	case ResourcePool{}.TableName():
		value = fmt.Sprintf("%s%s/%s", constants.O2IMSInventoryBaseURL, constants.ResourcePoolsPath, objectID.String())
	case DeploymentManager{}.TableName():
		value = fmt.Sprintf("%s%s/%s", constants.O2IMSInventoryBaseURL, constants.DeploymentManagersPath, objectID.String())
	default:
		return nil
	}

	return &value
}

// DataChangeEventToModel converts a DB tuple to an API model
func DataChangeEventToModel(record *models2.DataChangeEvent) generated.InventoryChangeNotification {
	eventType := getEventType(record.BeforeState, record.AfterState)
	object := generated.InventoryChangeNotification{
		NotificationEventType: generated.InventoryChangeNotificationNotificationEventType(eventType),
		NotificationId:        *record.DataChangeID,
		ObjectRef:             getObjectReference(record.ObjectType, record.ObjectID, record.ParentID),
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
func DataChangeEventToNotification(record *models2.DataChangeEvent) *notifier.Notification {
	return &notifier.Notification{
		NotificationID: *record.DataChangeID,
		SequenceID:     *record.SequenceID,
		Payload:        DataChangeEventToModel(record),
	}
}

// SubscriptionToInfo converts a Subscription to a generic SubscriptionInfo
func SubscriptionToInfo(record *models2.Subscription) *notifier.SubscriptionInfo {
	return &notifier.SubscriptionInfo{
		SubscriptionID:         *record.SubscriptionID,
		ConsumerSubscriptionID: record.ConsumerSubscriptionID,
		Callback:               record.Callback,
		Filter:                 record.Filter,
		EventCursor:            record.EventCursor,
	}
}

// LocationToModel converts a Location DB record to an API model
func LocationToModel(record *Location, oCloudSiteIDs []uuid.UUID) generated.LocationInfo {
	object := generated.LocationInfo{
		GlobalLocationId: record.GlobalLocationID,
		Name:             record.Name,
		Description:      record.Description,
		Address:          record.Address,
	}

	// Handle optional Extensions field
	if record.Extensions != nil {
		object.Extensions = &record.Extensions
	}

	// Handle Coordinate (GeoJSON Point)
	if record.Coordinate != nil {
		coordType := generated.Point
		coord := struct {
			Coordinates *[]float32                            `json:"coordinates,omitempty"`
			Type        *generated.LocationInfoCoordinateType `json:"type,omitempty"`
		}{
			Type: &coordType,
		}
		// Extract coordinates from the map if present
		if coords, ok := record.Coordinate["coordinates"].([]interface{}); ok {
			floatCoords := make([]float32, len(coords))
			for i, c := range coords {
				if f, ok := c.(float64); ok {
					floatCoords[i] = float32(f)
				}
			}
			coord.Coordinates = &floatCoords
		}
		object.Coordinate = &coord
	}

	// Handle CivicAddress (array of {caType, caValue})
	if record.CivicAddress != nil && len(record.CivicAddress) > 0 {
		civicAddr := make([]struct {
			CaType  int    `json:"caType"`
			CaValue string `json:"caValue"`
		}, len(record.CivicAddress))
		for i, ca := range record.CivicAddress {
			if caType, ok := ca["caType"].(float64); ok {
				civicAddr[i].CaType = int(caType)
			}
			if caValue, ok := ca["caValue"].(string); ok {
				civicAddr[i].CaValue = caValue
			}
		}
		object.CivicAddress = &civicAddr
	}

	// Handle OCloudSiteIds
	if oCloudSiteIDs != nil && len(oCloudSiteIDs) > 0 {
		object.OCloudSiteIds = &oCloudSiteIDs
	}

	return object
}

// OCloudSiteToModel converts an OCloudSite DB record to an API model
func OCloudSiteToModel(record *OCloudSite, resourcePoolIDs []uuid.UUID) generated.OCloudSiteInfo {
	object := generated.OCloudSiteInfo{
		OCloudSiteId:     record.OCloudSiteID,
		GlobalLocationId: record.GlobalLocationID,
		Name:             record.Name,
		Description:      record.Description,
		ResourcePools:    resourcePoolIDs,
	}

	// Handle optional Extensions field
	if record.Extensions != nil {
		object.Extensions = &record.Extensions
	}

	// Ensure ResourcePools is not nil (API requires it)
	if object.ResourcePools == nil {
		object.ResourcePools = []uuid.UUID{}
	}

	return object
}

// AlarmDictionaryToModel converts an AlarmDictionary DB record to an API model
func AlarmDictionaryToModel(record *AlarmDictionary, alarmDefinitionRecords []AlarmDefinition) common.AlarmDictionary {
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
