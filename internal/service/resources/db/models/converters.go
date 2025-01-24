package models

import (
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
	models2 "github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/utils"
)

// managementInterfaceID defines the unique identifier for the IMS O2 interface
const managementInterfaceID = "O2IMS"

// dummyDefinitionID is a temporary value used to render a placeholder alarm definition.  To be replaced when we support
// retrieving alarm dictionaries from the hardware manager
const dummyDefinitionID = "46a600ca-bb4d-470d-b8ca-0f95989518e4"

// dummyVersion is a temporary value to used to render a placeholder alarm dictionary/definition.
const dummyVersion = "0.0.0"

// DeploymentManagerToModel converts a DB tuple to an API Model
func DeploymentManagerToModel(record *DeploymentManager) generated.DeploymentManager {
	object := generated.DeploymentManager{
		Capabilities:        map[string]string{},
		Capacity:            map[string]string{},
		DeploymentManagerId: record.DeploymentManagerID,
		Description:         record.Description,
		Extensions:          &record.Extensions,
		Name:                record.Name,
		OCloudId:            record.OCloudID,
		ServiceUri:          record.URL,
		SupportedLocations:  record.Locations,
	}

	if record.CapacityInfo != nil {
		object.Capacity = record.CapacityInfo
	}

	if record.Capabilities != nil {
		object.Capabilities = record.Capabilities
	}

	return object
}

// ResourceTypeToModel converts a DB tuple to an API Model
func ResourceTypeToModel(record *ResourceType) generated.ResourceType {
	object := generated.ResourceType{
		// TODO: fill-in a proper alarm dictionary when we can get it from the hardware manager
		AlarmDictionary: &common.AlarmDictionary{
			AlarmDefinition: []common.AlarmDefinition{
				{
					AlarmAdditionalFields: nil,
					AlarmChangeType:       common.ADDED,
					AlarmDefinitionId:     uuid.MustParse(dummyDefinitionID),
					AlarmDescription:      "Sample alarm definition",
					AlarmLastChange:       dummyVersion,
					AlarmName:             "Sample alarm name",
					ClearingType:          common.MANUAL,
					ManagementInterfaceId: []common.AlarmDefinitionManagementInterfaceId{managementInterfaceID},
					PkNotificationField:   []string{"alarmDefinitionID"},
					ProposedRepairActions: "Please consult the documentation",
				},
			},
			AlarmDictionarySchemaVersion: dummyVersion,
			AlarmDictionaryVersion:       dummyVersion,
			EntityType:                   fmt.Sprintf("%s/%s", record.Model, record.Version),
			ManagementInterfaceId:        []common.AlarmDictionaryManagementInterfaceId{"O2IMS"},
			PkNotificationField:          []string{"alarmDictionaryID"},
			Vendor:                       record.Vendor,
		},
		Description:    record.Description,
		Extensions:     &record.Extensions,
		Model:          record.Model,
		Name:           record.Name,
		ResourceClass:  generated.ResourceTypeResourceClass(record.ResourceClass),
		ResourceKind:   generated.ResourceTypeResourceKind(record.ResourceKind),
		ResourceTypeId: record.ResourceTypeID,
		Vendor:         record.Vendor,
		Version:        record.Version,
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
func ResourcePoolToModel(record *ResourcePool) generated.ResourcePool {
	object := generated.ResourcePool{
		Description:      record.Description,
		Extensions:       &record.Extensions,
		GlobalLocationId: record.GlobalLocationID,
		Location:         record.Location,
		Name:             record.Name,
		OCloudId:         record.OCloudID,
		ResourcePoolId:   record.ResourcePoolID,
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
		value = fmt.Sprintf("%s/resourceTypes/%s", utils.BaseInventoryURL, objectID.String())
	case Resource{}.TableName():
		value = fmt.Sprintf("%s/resourcePools/%s/resources/%s", utils.BaseInventoryURL, parentID.String(), objectID.String())
	case ResourcePool{}.TableName():
		value = fmt.Sprintf("%s/resourcePools/%s", utils.BaseInventoryURL, objectID.String())
	case DeploymentManager{}.TableName():
		value = fmt.Sprintf("%s/deploymentManagers/%s", utils.BaseInventoryURL, objectID.String())
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
