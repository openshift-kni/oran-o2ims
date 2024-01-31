package utils

// ORANO2IMSConditionType defines conditions of an ORANO2IMS deployment.
type ORANO2IMSConditionType string

var ORANO2IMSConditionTypes = struct {
	Ready                     ORANO2IMSConditionType
	NotReady                  ORANO2IMSConditionType
	Error                     ORANO2IMSConditionType
	Available                 ORANO2IMSConditionType
	MetadataServerAvailable   ORANO2IMSConditionType
	DeploymentServerAvailable ORANO2IMSConditionType
}{
	Ready:                     "ORANO2IMSReady",
	NotReady:                  "ORANO2IMSConditionType",
	Error:                     "Error",
	Available:                 "Available",
	MetadataServerAvailable:   "MetadataServerAvailable",
	DeploymentServerAvailable: "DeploymentServerAvailable",
}

type ORANO2IMSConditionReason string

var ORANO2IMSConditionReasons = struct {
	DeploymentsReady                  ORANO2IMSConditionReason
	DeploymentsError                  ORANO2IMSConditionReason
	ErrorGettingDeploymentInformation ORANO2IMSConditionReason
	DeploymentNotFound                ORANO2IMSConditionReason
}{
	ErrorGettingDeploymentInformation: "ErrorGettingDeploymentInformation",
	DeploymentNotFound:                "DeploymentNotFound",
}

var MapDeploymentNameConditionType = map[string]ORANO2IMSConditionType{
	ORANO2IMSMetadataServerName:          ORANO2IMSConditionTypes.MetadataServerAvailable,
	ORANO2IMSDeploymentManagerServerName: ORANO2IMSConditionTypes.DeploymentServerAvailable,
}
