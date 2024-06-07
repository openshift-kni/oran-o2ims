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
	ResourceServerAvailable   ORANO2IMSConditionType
	MetadataServerError       ORANO2IMSConditionType
	DeploymentServerError     ORANO2IMSConditionType
	ResourceServerError       ORANO2IMSConditionType
}{
	Ready:                     "ORANO2IMSReady",
	NotReady:                  "ORANO2IMSConditionType",
	Error:                     "Error",
	Available:                 "Available",
	MetadataServerAvailable:   "MetadataServerAvailable",
	DeploymentServerAvailable: "DeploymentServerAvailable",
	ResourceServerAvailable:   "ResourceServerAvailable",
	MetadataServerError:       "MetadataServerError",
	DeploymentServerError:     "DeploymentServerError",
	ResourceServerError:       "ResourceServerError",
}

type ORANO2IMSConditionReason string

var ORANO2IMSConditionReasons = struct {
	DeploymentsReady                  ORANO2IMSConditionReason
	ErrorGettingDeploymentInformation ORANO2IMSConditionReason
	DeploymentNotFound                ORANO2IMSConditionReason
	ServerArgumentsError              ORANO2IMSConditionReason
}{
	DeploymentsReady:                  "AllDeploymentsReady",
	ErrorGettingDeploymentInformation: "ErrorGettingDeploymentInformation",
	DeploymentNotFound:                "DeploymentNotFound",
	ServerArgumentsError:              "ServerArgumentsError",
}

var MapAvailableDeploymentNameConditionType = map[string]ORANO2IMSConditionType{
	ORANO2IMSMetadataServerName:          ORANO2IMSConditionTypes.MetadataServerAvailable,
	ORANO2IMSDeploymentManagerServerName: ORANO2IMSConditionTypes.DeploymentServerAvailable,
	ORANO2IMSResourceServerName:          ORANO2IMSConditionTypes.ResourceServerAvailable,
}

var MapErrorDeploymentNameConditionType = map[string]ORANO2IMSConditionType{
	ORANO2IMSMetadataServerName:          ORANO2IMSConditionTypes.MetadataServerError,
	ORANO2IMSDeploymentManagerServerName: ORANO2IMSConditionTypes.DeploymentServerError,
	ORANO2IMSResourceServerName:          ORANO2IMSConditionTypes.ResourceServerError,
}
