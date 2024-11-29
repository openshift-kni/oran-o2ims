package models

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/service/resources/api/generated"
)

// DeploymentManagerToModel converts a DB tuple to an API Model
func DeploymentManagerToModel(record *DeploymentManager) (generated.DeploymentManager, error) {
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

	return object, nil
}

// ResourceTypeToModel converts a DB tuple to an API Model
func ResourceTypeToModel(record *ResourceType) (generated.ResourceType, error) {
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

	return object, nil
}

// SubscriptionToModel converts a DB tuple to an API Model
func SubscriptionToModel(record *Subscription) (generated.Subscription, error) {
	object := generated.Subscription{
		Callback:               record.Callback,
		ConsumerSubscriptionId: record.ConsumerSubscriptionID,
		Filter:                 record.Filter,
		SubscriptionId:         record.SubscriptionID,
	}

	return object, nil
}

// SubscriptionFromModel converts a API model to a DB tuple
func SubscriptionFromModel(object *generated.Subscription) (*Subscription, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("failed to generate subscription ID: %w", err)
	}

	record := Subscription{
		SubscriptionID:         &id,
		ConsumerSubscriptionID: object.ConsumerSubscriptionId,
		Filter:                 object.Filter,
		Callback:               object.Callback,
		EventCursor:            0,
	}

	return &record, nil
}
