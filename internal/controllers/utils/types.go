package utils

import (
	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
)

// InventoryConditionType defines conditions of an Inventory deployment.
type InventoryConditionType string

var InventoryConditionTypes = struct {
	Ready                     InventoryConditionType
	NotReady                  InventoryConditionType
	Error                     InventoryConditionType
	Available                 InventoryConditionType
	MetadataServerAvailable   InventoryConditionType
	DeploymentServerAvailable InventoryConditionType
	ResourceServerAvailable   InventoryConditionType
	AlarmServerError          InventoryConditionType
	DatabaseServerError       InventoryConditionType
	MetadataServerError       InventoryConditionType
	DeploymentServerError     InventoryConditionType
	ResourceServerError       InventoryConditionType
	SmoRegistrationCompleted  InventoryConditionType
	DatabaseServerAvailable   InventoryConditionType
	AlarmServerAvailable      InventoryConditionType
}{
	Ready:                     "InventoryReady",
	NotReady:                  "InventoryConditionType",
	Error:                     "Error",
	Available:                 "Available",
	MetadataServerAvailable:   "MetadataServerAvailable",
	DeploymentServerAvailable: "DeploymentServerAvailable",
	ResourceServerAvailable:   "ResourceServerAvailable",
	AlarmServerError:          "AlarmServerError",
	DatabaseServerError:       "DatabaseServerError",
	MetadataServerError:       "MetadataServerError",
	DeploymentServerError:     "DeploymentServerError",
	ResourceServerError:       "ResourceServerError",
	SmoRegistrationCompleted:  "SmoRegistrationCompleted",
	DatabaseServerAvailable:   "DatabaseServerAvailable",
	AlarmServerAvailable:      "AlarmServerAvailable",
}

type InventoryConditionReason string

var InventoryConditionReasons = struct {
	DeploymentsReady                  InventoryConditionReason
	ErrorGettingDeploymentInformation InventoryConditionReason
	DatabaseDeploymentFailed          InventoryConditionReason
	DeploymentNotFound                InventoryConditionReason
	ServerArgumentsError              InventoryConditionReason
	SmoRegistrationSuccessful         InventoryConditionReason
	SmoRegistrationFailed             InventoryConditionReason
	SmoNotConfigured                  InventoryConditionReason
}{
	DatabaseDeploymentFailed:          "DatabaseDeploymentFailed",
	DeploymentsReady:                  "AllDeploymentsReady",
	ErrorGettingDeploymentInformation: "ErrorGettingDeploymentInformation",
	DeploymentNotFound:                "DeploymentNotFound",
	ServerArgumentsError:              "ServerArgumentsError",
	SmoRegistrationSuccessful:         "SmoRegistrationSuccessful",
	SmoRegistrationFailed:             "SmoRegistrationFailed",
	SmoNotConfigured:                  "SmoNotConfigured",
}

var MapAvailableDeploymentNameConditionType = map[string]InventoryConditionType{
	InventoryAlarmServerName:             InventoryConditionTypes.AlarmServerAvailable,
	InventoryDatabaseServerName:          InventoryConditionTypes.DatabaseServerAvailable,
	InventoryMetadataServerName:          InventoryConditionTypes.MetadataServerAvailable,
	InventoryDeploymentManagerServerName: InventoryConditionTypes.DeploymentServerAvailable,
	InventoryResourceServerName:          InventoryConditionTypes.ResourceServerAvailable,
}

var MapErrorDeploymentNameConditionType = map[string]InventoryConditionType{
	InventoryAlarmServerName:             InventoryConditionTypes.AlarmServerError,
	InventoryMetadataServerName:          InventoryConditionTypes.MetadataServerError,
	InventoryDeploymentManagerServerName: InventoryConditionTypes.DeploymentServerError,
	InventoryResourceServerName:          InventoryConditionTypes.ResourceServerError,
	InventoryDatabaseServerName:          InventoryConditionTypes.DatabaseServerError,
}

// AvailableNotification represents the data sent to the SMO once the O2IMS is ready to accept API calls.   This is
// from table 3.6.5.1.2-1 in the O-RAN.WG6.O2IMS-INTERFACE-R003-v06.00 document, and presumably will be formally defined
// in an OpenAPI that we can just import at some point.
type AvailableNotification struct {
	GlobalCloudId string `json:"globalCloudId"`
	OCloudId      string `json:"oCloudId"`
	ImsEndpoint   string `json:"IMS_EP"`
}

type NodeInfo struct {
	BmcAddress     string
	BmcCredentials string
	NodeName       string
	Interfaces     []*hwv1alpha1.Interface
}
