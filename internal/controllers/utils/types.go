package utils

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
	MetadataServerError       InventoryConditionType
	DeploymentServerError     InventoryConditionType
	ResourceServerError       InventoryConditionType
}{
	Ready:                     "InventoryReady",
	NotReady:                  "InventoryConditionType",
	Error:                     "Error",
	Available:                 "Available",
	MetadataServerAvailable:   "MetadataServerAvailable",
	DeploymentServerAvailable: "DeploymentServerAvailable",
	ResourceServerAvailable:   "ResourceServerAvailable",
	MetadataServerError:       "MetadataServerError",
	DeploymentServerError:     "DeploymentServerError",
	ResourceServerError:       "ResourceServerError",
}

type InventoryConditionReason string

var InventoryConditionReasons = struct {
	DeploymentsReady                  InventoryConditionReason
	ErrorGettingDeploymentInformation InventoryConditionReason
	DeploymentNotFound                InventoryConditionReason
	ServerArgumentsError              InventoryConditionReason
}{
	DeploymentsReady:                  "AllDeploymentsReady",
	ErrorGettingDeploymentInformation: "ErrorGettingDeploymentInformation",
	DeploymentNotFound:                "DeploymentNotFound",
	ServerArgumentsError:              "ServerArgumentsError",
}

var MapAvailableDeploymentNameConditionType = map[string]InventoryConditionType{
	InventoryMetadataServerName:          InventoryConditionTypes.MetadataServerAvailable,
	InventoryDeploymentManagerServerName: InventoryConditionTypes.DeploymentServerAvailable,
	InventoryResourceServerName:          InventoryConditionTypes.ResourceServerAvailable,
}

var MapErrorDeploymentNameConditionType = map[string]InventoryConditionType{
	InventoryMetadataServerName:          InventoryConditionTypes.MetadataServerError,
	InventoryDeploymentManagerServerName: InventoryConditionTypes.DeploymentServerError,
	InventoryResourceServerName:          InventoryConditionTypes.ResourceServerError,
}
