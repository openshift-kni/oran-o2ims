/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
)

// InventoryConditionType defines conditions of an Inventory deployment.
type InventoryConditionType string

var InventoryConditionTypes = struct {
	Ready                    InventoryConditionType
	NotReady                 InventoryConditionType
	Error                    InventoryConditionType
	Available                InventoryConditionType
	SmoRegistrationCompleted InventoryConditionType

	AlarmServerError        InventoryConditionType
	ArtifactsServerError    InventoryConditionType
	ClusterServerError      InventoryConditionType
	DatabaseServerError     InventoryConditionType
	ResourceServerError     InventoryConditionType
	ProvisioningServerError InventoryConditionType

	AlarmServerAvailable        InventoryConditionType
	ArtifactsServerAvailable    InventoryConditionType
	ClusterServerAvailable      InventoryConditionType
	DatabaseServerAvailable     InventoryConditionType
	ResourceServerAvailable     InventoryConditionType
	ProvisioningServerAvailable InventoryConditionType
}{
	Ready:                    "InventoryReady",
	NotReady:                 "InventoryConditionType",
	Error:                    "Error",
	Available:                "Available",
	SmoRegistrationCompleted: "SmoRegistrationCompleted",

	AlarmServerError:        "AlarmServerError",
	ArtifactsServerError:    "ArtifactsServerError",
	ClusterServerError:      "ClusterServerError",
	DatabaseServerError:     "DatabaseServerError",
	ResourceServerError:     "ResourceServerError",
	ProvisioningServerError: "ProvisioningServerError",

	AlarmServerAvailable:        "AlarmServerAvailable",
	ArtifactsServerAvailable:    "ArtifactsServerAvailable",
	ClusterServerAvailable:      "ClusterServerAvailable",
	DatabaseServerAvailable:     "DatabaseServerAvailable",
	ResourceServerAvailable:     "ResourceServerAvailable",
	ProvisioningServerAvailable: "ProvisioningServerAvailable",
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
	OAuthClientIDNotConfigured        InventoryConditionReason
}{
	DatabaseDeploymentFailed:          "DatabaseDeploymentFailed",
	DeploymentsReady:                  "AllDeploymentsReady",
	ErrorGettingDeploymentInformation: "ErrorGettingDeploymentInformation",
	DeploymentNotFound:                "DeploymentNotFound",
	ServerArgumentsError:              "ServerArgumentsError",
	SmoRegistrationSuccessful:         "SmoRegistrationSuccessful",
	SmoRegistrationFailed:             "SmoRegistrationFailed",
	SmoNotConfigured:                  "SmoNotConfigured",
	OAuthClientIDNotConfigured:        "OAuthClientIDNotConfigured",
}

var MapAvailableDeploymentNameConditionType = map[string]InventoryConditionType{
	InventoryAlarmServerName:        InventoryConditionTypes.AlarmServerAvailable,
	InventoryArtifactsServerName:    InventoryConditionTypes.ArtifactsServerAvailable,
	InventoryClusterServerName:      InventoryConditionTypes.ClusterServerAvailable,
	InventoryDatabaseServerName:     InventoryConditionTypes.DatabaseServerAvailable,
	InventoryResourceServerName:     InventoryConditionTypes.ResourceServerAvailable,
	InventoryProvisioningServerName: InventoryConditionTypes.ProvisioningServerAvailable,
}

var MapErrorDeploymentNameConditionType = map[string]InventoryConditionType{
	InventoryAlarmServerName:        InventoryConditionTypes.AlarmServerError,
	InventoryArtifactsServerName:    InventoryConditionTypes.ArtifactsServerError,
	InventoryClusterServerName:      InventoryConditionTypes.ClusterServerError,
	InventoryDatabaseServerName:     InventoryConditionTypes.DatabaseServerError,
	InventoryResourceServerName:     InventoryConditionTypes.ResourceServerError,
	InventoryProvisioningServerName: InventoryConditionTypes.ProvisioningServerError,
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
	NodeID         string
	HwMgrNodeId    string
	HwMgrNodeNs    string
	Interfaces     []*hwv1alpha1.Interface
}
