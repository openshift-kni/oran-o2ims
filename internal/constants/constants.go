/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package constants

// O2 IMS API path prefixes
const (
	O2IMSInventoryAPIPath    = "/o2ims-infrastructureInventory"
	O2IMSClusterAPIPath      = "/o2ims-infrastructureCluster"
	O2IMSMonitoringAPIPath   = "/o2ims-infrastructureMonitoring"
	O2IMSArtifactsAPIPath    = "/o2ims-infrastructureArtifacts"
	O2IMSProvisioningAPIPath = "/o2ims-infrastructureProvisioning"
)

// Hardware Manager API path prefixes
const (
	HardwareManagerProvisioningAPIPath = "/hardware-manager/provisioning"
	HardwareManagerInventoryAPIPath    = "/hardware-manager/inventory"
)

// API version suffix
const APIVersionV1 = "/v1"

// Full API base URLs (computed constants)
var (
	O2IMSInventoryBaseURL              = O2IMSInventoryAPIPath + APIVersionV1
	O2IMSClusterBaseURL                = O2IMSClusterAPIPath + APIVersionV1
	O2IMSMonitoringBaseURL             = O2IMSMonitoringAPIPath + APIVersionV1
	O2IMSArtifactsBaseURL              = O2IMSArtifactsAPIPath + APIVersionV1
	O2IMSProvisioningBaseURL           = O2IMSProvisioningAPIPath + APIVersionV1
	HardwareManagerProvisioningBaseURL = HardwareManagerProvisioningAPIPath + APIVersionV1
	HardwareManagerInventoryBaseURL    = HardwareManagerInventoryAPIPath + APIVersionV1
)

// API endpoint path segments
const (
	// Inventory/Resources API paths
	ResourceTypesPath      = "/resourceTypes"
	ResourcePoolsPath      = "/resourcePools"
	ResourcesPath          = "/resources"
	DeploymentManagersPath = "/deploymentManagers"

	// Cluster API paths
	ClusterResourceTypesPath = "/clusterResourceTypes"
	ClusterResourcePath      = "/clusterResource"
	NodeClusterTypesPath     = "/nodeClusterTypes"
	NodeClustersPath         = "/nodeClusters"

	// Monitoring/Alarms API paths
	AlarmsPath = "/alarms"
)

// Command line argument constants
const (
	HealthProbeFlag = "--health-probe-bind-address"
	MetricsFlag     = "--metrics-bind-address"
	LeaderElectFlag = "--leader-elect"
)

// Port constants
const (
	MetricsPort     = ":8080"
	HealthProbePort = ":8081"
)

// Server command names
const (
	AlarmsServerCmd       = "alarms-server"
	ArtifactsServerCmd    = "artifacts-server"
	ClusterServerCmd      = "cluster-server"
	ProvisioningServerCmd = "provisioning-server"
	ResourceServerCmd     = "resource-server"
)

// Hardware plugin command names
const (
	HardwarePluginManagerCmd       = "hardwareplugin-manager"
	Metal3HardwarePluginManagerCmd = "metal3-hardwareplugin-manager"
)

// TLS/Certificate field names
const (
	TLSCertField  = "tls.crt"
	TLSKeyField   = "tls.key"
	CABundleField = "ca-bundle"
)

// Kubernetes service domains
const (
	ClusterLocalDomain   = "svc.cluster.local"
	KubernetesAPIService = "kubernetes.default.svc"
)

// Network addresses
const (
	Localhost = "127.0.0.1"
)

// Common server arguments
const (
	ServeSubcommand = "serve"
	StartSubcommand = "start"
)

// Executable paths
const (
	ManagerExec = "/usr/bin/oran-o2ims"
	PsqlExec    = "psql"
)

// Default namespace and environment variables
const (
	DefaultNamespace        = "oran-o2ims"
	DefaultNamespaceEnvName = "OCLOUD_MANAGER_NAMESPACE"
	ImagePullPolicyEnvName  = "IMAGE_PULL_POLICY"
)

// Application and resource names
const (
	DefaultAppName     = "o2ims"
	DefaultInventoryCR = "default"
	DefaultOCloudID    = "undefined"
)

// Port constants
const (
	DefaultServicePort   = 8443
	DefaultContainerPort = 8443
	DatabaseServicePort  = 5432
)

// Container names
const (
	MigrationContainerName = "migration"
	ServerContainerName    = "server"
)

// TLS mount paths
const (
	TLSServerMountPath = "/secrets/tls"
	TLSClientMountPath = "/secrets/smo/tls"
	CABundleMountPath  = "/secrets/smo/certs"
	CABundleFilename   = "ca-bundle.crt"
)

// Standard Kubernetes service account paths
const (
	DefaultBackendTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token" // nolint: gosec // hardcoded path only
	DefaultServiceCAFile    = "/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt"
)

// Environment variable names
const (
	ServerImageName         = "IMAGE"
	PostgresImageName       = "POSTGRES_IMAGE"
	InternalServicePortName = "INTERNAL_SERVICE_PORT"
)

// NodeAllocationRequest Callback Service configuration
const (
	NarCallbackServiceNameEnv      = "NAR_CALLBACK_SERVICE_NAME"
	NarCallbackServiceNamespaceEnv = "NAR_CALLBACK_SERVICE_NAMESPACE"
	DefaultNarCallbackServiceName  = "oran-o2ims-nar-callback-service"
	DefaultNarCallbackServicePort  = 8090
)

// NodeAllocationRequest Callback API Paths and URLs
const (
	NarCallbackAPIPath                  = "/nar-callback"
	NarCallbackProvisioningRequestsPath = "/provisioning-requests"
	NarCallbackBaseURL                  = NarCallbackAPIPath + APIVersionV1
	NarCallbackServicePath              = NarCallbackBaseURL + NarCallbackProvisioningRequestsPath
)

// Label prefixes
const (
	LabelPrefixInterfaces = "interfacelabel.clcm.openshift.io/"
)

// Boot interface configuration
const (
	BootInterfaceLabel        = "boot-interface"
	BootInterfaceLabelFullKey = LabelPrefixInterfaces + BootInterfaceLabel
)
