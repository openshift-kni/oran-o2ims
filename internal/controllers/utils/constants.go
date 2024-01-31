package utils

// Default image
const (
	ORANImage = "quay.io/openshift-kni/oran-o2ims:latest"
)

// Default namespace
const (
	ORANO2IMSNamespace = "oran-o2ims"
)

// Deployment names
const (
	ORANO2IMSMetadataServerName          = "metadata-server"
	ORANO2IMSDeploymentManagerServerName = "deployment-manager-server"
)

// CR default names
const (
	ORANO2IMSIngressName   = "api"
	ORANO2IMSConfigMapName = "authz"
	ORANO2IMSClientSAName  = "client"
)

// Resource operations
const (
	UPDATE = "Update"
	PATCH  = "Patch"
)
