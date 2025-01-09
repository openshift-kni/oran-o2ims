package models

import (
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
)

// ClusterResourceToModel converts a DB tuple to an API model
func ClusterResourceToModel(record *ClusterResource) generated.ClusterResource {
	return generated.ClusterResource{
		ArtifactResourceIds:   record.ArtifactResourceIDs,
		ClusterResourceId:     record.ClusterResourceID,
		ClusterResourceTypeId: record.ClusterResourceTypeID,
		Description:           record.Description,
		Extensions:            record.Extensions,
		MemberOf:              nil, // TODO
		Name:                  record.Name,
		ResourceId:            record.ResourceID,
	}
}

// ClusterResourceTypeToModel converts a DB tuple to an API model
func ClusterResourceTypeToModel(record *ClusterResourceType) generated.ClusterResourceType {
	return generated.ClusterResourceType{
		ClusterResourceTypeId: record.ClusterResourceTypeID,
		Description:           record.Description,
		Extensions:            record.Extensions,
		Name:                  record.Name,
	}
}

// NodeClusterToModel converts a DB tuple to an API model
func NodeClusterToModel(record *NodeCluster, clusterResourceIDs []uuid.UUID) generated.NodeCluster {
	object := generated.NodeCluster{
		NodeClusterId:                  record.NodeClusterID,
		NodeClusterTypeId:              record.NodeClusterTypeID,
		ArtifactResourceId:             record.ArtifactResourceID,
		ClientNodeClusterId:            record.ClientNodeClusterID,
		ClusterDistributionDescription: record.ClusterDistributionDescription,
		ClusterResourceGroups:          record.ClusterResourceGroups,
		Description:                    record.Description,
		Extensions:                     record.Extensions,
		Name:                           record.Name,
	}

	object.ClusterResourceIds = clusterResourceIDs
	if clusterResourceIDs == nil {
		object.ClusterResourceIds = []uuid.UUID{}
	}

	return object
}

// NodeClusterTypeToModel converts a DB tuple to an API model
func NodeClusterTypeToModel(record *NodeClusterType) generated.NodeClusterType {
	return generated.NodeClusterType{
		Description:       record.Description,
		Extensions:        record.Extensions,
		Name:              record.Name,
		NodeClusterTypeId: record.NodeClusterTypeID,
	}
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
		SubscriptionID: *record.SubscriptionID,
		Callback:       record.Callback,
		Filter:         record.Filter,
		EventCursor:    record.EventCursor,
	}
}

// getEventType determines the event type based on the object transition
func getEventType(before, after *string) int {
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
		value = fmt.Sprintf("%s/clusterResourceTypes/%s", utils.BaseURL, objectID.String())
	case ClusterResource{}.TableName():
		value = fmt.Sprintf("%s/clusterResource/%s", utils.BaseURL, objectID.String())
	case NodeClusterType{}.TableName():
		value = fmt.Sprintf("%s/nodeClusterTypes/%s", utils.BaseURL, objectID.String())
	case NodeCluster{}.TableName():
		value = fmt.Sprintf("%s/nodeClusters/%s", utils.BaseURL, objectID.String())
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
		PostObjectState:       record.AfterState,
		PriorObjectState:      record.BeforeState,
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
