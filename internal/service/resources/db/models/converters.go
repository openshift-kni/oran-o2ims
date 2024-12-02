package models

import (
	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/service/resources/api/generated"
)

// DeploymentManagerToModel converts a DB tuple to an API Model
func DeploymentManagerToModel(record *DeploymentManager) generated.DeploymentManager {
	object := generated.DeploymentManager{
		Capabilities:        map[string]string{},
		Capacity:            map[string]string{},
		DeploymentManagerId: *record.ClusterID,
		Description:         record.Description,
		Extensions:          record.Extensions,
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
		AlarmDictionary: nil,
		Description:     record.Description,
		Extensions:      record.Extensions,
		Model:           record.Model,
		Name:            record.Name,
		ResourceClass:   "",
		ResourceKind:    "",
		ResourceTypeId:  *record.ResourceTypeID,
		Vendor:          record.Vendor,
		Version:         record.Version,
	}

	return object
}

// SubscriptionToModel converts a DB tuple to an API Model
func SubscriptionToModel(record *Subscription) generated.Subscription {
	object := generated.Subscription{
		Callback:               record.Callback,
		ConsumerSubscriptionId: record.ConsumerSubscriptionID,
		Filter:                 record.Filter,
		SubscriptionId:         record.SubscriptionID,
	}

	return object
}

// SubscriptionFromModel converts an API model to a DB tuple
func SubscriptionFromModel(object *generated.Subscription) *Subscription {
	id := uuid.Must(uuid.NewRandom())

	record := Subscription{
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
		Extensions:       record.Extensions,
		GlobalLocationId: record.GlobalLocationID,
		Location:         record.Location,
		Name:             record.Name,
		OCloudId:         record.OCloudID,
		ResourcePoolId:   *record.ResourcePoolID,
	}

	return object
}

// ResourceToModel converts a DB tuple to an API model
func ResourceToModel(record *Resource, elements []Resource) generated.Resource {
	object := generated.Resource{
		Description:    record.Description,
		Extensions:     record.Extensions,
		ResourceId:     *record.ResourceID,
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
