package utils

import "time"

// Default namespace
const (
	InventoryNamespace = "oran-o2ims"
)

// Base resource names
const (
	InventoryDatabase          = "postgres"
	InventoryMetadata          = "metadata"
	InventoryDeploymentManager = "deployment-manager"
	InventoryResource          = "resource"
	InventoryAlarms            = "alarms"
	InventoryCluster           = "cluster"
)

// Suffix for server names
const serverSuffix = "-server"

// Deployment names
const (
	InventoryDatabaseServerName          = InventoryDatabase + serverSuffix
	InventoryMetadataServerName          = InventoryMetadata + serverSuffix
	InventoryDeploymentManagerServerName = InventoryDeploymentManager + serverSuffix
	InventoryResourceServerName          = InventoryResource + serverSuffix
	InventoryAlarmServerName             = InventoryAlarms + serverSuffix
	InventoryClusterServerName           = InventoryCluster + serverSuffix
)

// InventoryIngressName the name of our Ingress controller instance
const InventoryIngressName = "api"

// Resource operations
const (
	UPDATE = "Update"
	PATCH  = "Patch"
)

// Container arguments
var (
	AlarmServerArgs = []string{
		"alarms-server",
		"serve",
		"--api-listener-address=127.0.0.1:8000",
	}
	MetadataServerArgs = []string{
		"start",
		"metadata-server",
		"--log-level=debug",
		"--log-file=stdout",
		"--api-listener-address=127.0.0.1:8000",
	}
	DeploymentManagerServerArgs = []string{
		"start",
		"deployment-manager-server",
		"--log-level=debug",
		"--log-file=stdout",
		"--api-listener-address=127.0.0.1:8000",
	}
	ResourceServerArgs = []string{
		"start",
		"resource-server",
		"--log-level=debug",
		"--log-file=stdout",
		"--api-listener-address=127.0.0.1:8000",
	}
	ClusterServerArgs = []string{
		"cluster-server",
		"serve",
		"--api-listener-address=127.0.0.1:8000",
	}
)

// DefaultOCloudID defines the default Global O-Cloud ID to be used until the end user configures this value.
const DefaultOCloudID = "undefined"

// DefaultAppName defines the name prepended to the ingress host to form our FQDN hostname.
const DefaultAppName = "o2ims"

// Defines information related to the operator instance in a namespace
const (
	DefaultInventoryCR      = "default"
	DefaultNamespace        = "oran-o2ims"
	DefaultNamespaceEnvName = "OCLOUD_MANAGER_NAMESPACE"
)

// Search API attributes
const (
	SearchApiLabelKey   = "search-monitor"
	SearchApiLabelValue = "search-api"
)

// Default values for backend URL and token:
const (
	defaultApiServerURL     = "https://kubernetes.default.svc"
	DefaultBackendTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"          // nolint: gosec // hardcoded path only
	defaultBackendCABundle  = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"         // nolint: gosec // hardcoded path only
	DefaultServiceCAFile    = "/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt" // nolint: gosec // hardcoded path only
)

// Default timeout values
const (
	DefaultHardwareProvisioningTimeout = 90 * time.Minute
	DefaultClusterInstallationTimeout  = 90 * time.Minute
	DefaultClusterConfigurationTimeout = 30 * time.Minute
)

// These are optional keys in the respective ConfigMaps defined in ClusterTemplate
// spec.templates, used to configure the timeout values for each operation.
// If not specified, the default timeout values will be applied.
const (
	HardwareProvisioningTimeoutConfigKey = "hardwareProvisioningTimeout"
	ClusterInstallationTimeoutConfigKey  = "clusterInstallationTimeout"
	ClusterConfigurationTimeoutConfigKey = "clusterConfigurationTimeout"
)

// Required template schema parameters
const (
	TemplateParamNodeClusterName = "nodeClusterName"
	TemplateParamOCloudSiteId    = "oCloudSiteId"
	TemplateParamClusterInstance = "clusterInstanceParameters"
	TemplateParamPolicyConfig    = "policyTemplateParameters"
)

// ClusterInstance template constants
const (
	ClusterInstanceTemplateName                 = "ClusterInstance"
	ClusterInstanceTemplatePath                 = "controllers/clusterinstance-template.yaml"
	ClusterInstanceTemplateDefaultsConfigmapKey = "clusterinstance-defaults"
)

var (
	// AllowedClusterInstanceFields contains path patterns for fields that are allowed to be updated.
	// The wildcard "*" is used to match any index in a list.
	AllowedClusterInstanceFields = [][]string{
		// Cluster-level non-immutable fields
		{"extraAnnotations"},
		{"extraLabels"},
		// Node-level non-immutable fields
		{"nodes", "*", "extraAnnotations"},
		{"nodes", "*", "extraLabels"},
	}

	// IgnoredClusterInstanceFields contains path patterns for fields that should be ignored.
	// The wildcard "*" is used to match any index in a list.
	IgnoredClusterInstanceFields = [][]string{
		// Node-level ignored fields
		{"nodes", "*", "bmcAddress"},
		{"nodes", "*", "bmcCredentialsName"},
		{"nodes", "*", "bootMACAddress"},
		{"nodes", "*", "nodeNetwork", "interfaces", "*", "macAddress"},
		// modified for upgrade
		{"suppressedManifests"},
	}
)

// PolicyTemplate constants
const (
	PolicyTemplateDefaultsConfigmapKey = "policytemplate-defaults"
	ClusterVersionLabelKey             = "cluster-version"
)

// Cluster status
const (
	ClusterZtpDone    = "ZTP Done"
	ClusterZtpNotDone = "ZTP Not Done"
)

// Hardeware template constants
const (
	HwTemplatePluginMgr      = "hwMgrId"
	HwTemplateNodePool       = "node-pools-data"
	HwTemplateBootIfaceLabel = "bootInterfaceLabel"
	HwTemplateExtensions     = "extensions"
)

const (
	OperationTypeCreated = "created"
	OperationTypeUpdated = "updated"
	OperationTypeDryRun  = "validated with dry-run"
)

// Environment variable names
const (
	TLSSkipVerifyEnvName      = "INSECURE_SKIP_VERIFY"
	TLSSkipVerifyDefaultValue = false
)

// Label specific to ACM child policies.
const (
	ChildPolicyRootPolicyLabel       = "policy.open-cluster-management.io/root-policy"
	ChildPolicyClusterNameLabel      = "policy.open-cluster-management.io/cluster-name"
	ChildPolicyClusterNamespaceLabel = "policy.open-cluster-management.io/cluster-namespace"
)

// Hardware Manager plugin constants
const (
	UnitTestHwmgrID        = "hwmgr"
	UnitTestHwmgrNamespace = "hwmgr"
	DefaultPluginNamespace = "oran-hwmgr-plugin"
)

// POD Container Names
const (
	MigrationContainerName = "migration"
	RbacContainerName      = "rbac"
	ServerContainerName    = "server"
)

// POD Port Values
const (
	DefaultServicePort   = 8000
	DefaultTargetPort    = "https"
	DefaultContainerPort = 8000

	DatabaseServicePort = 5432
	DatabaseTargetPort  = "database"
)

// Environment values
const (
	ServerImageName        = "IMAGE"
	KubeRbacProxyImageName = "KUBE_RBAC_PROXY_IMAGE"
	PostgresImageName      = "POSTGRES_IMAGE"
	HwMgrPluginNameSpace   = "HWMGR_PLUGIN_NAMESPACE"
)

// ClusterVersionName is the name given to the default ClusterVersion object
const ClusterVersionName = "version"

// Upgrade constants
const (
	UpgradeDefaultsConfigmapKey = "ibgu"
)

// CRDs needed to be suppressed in ClusterInstance for upgrade
var (
	CRDsToBeSuppressedForUpgrade = []string{
		"AgentClusterInstall",
	}
)

// Postgres values
const (
	AdminPasswordEnvName     = "POSTGRESQL_ADMIN_PASSWORD"     // nolint: gosec
	AlarmsPasswordEnvName    = "ORAN_O2IMS_ALARMS_PASSWORD"    // nolint: gosec
	ResourcesPasswordEnvName = "ORAN_O2IMS_RESOURCES_PASSWORD" // nolint: gosec
	ClustersPasswordEnvName  = "ORAN_O2IMS_CLUSTERS_PASSWORD"  // nolint: gosec

	DatabaseHostnameEnvVar = "POSTGRES_HOSTNAME"
)

// NodeCluster/ClusterResource extensions
const (
	ClusterModelExtension   = "model"
	ClusterVersionExtension = "version"
	ClusterVendorExtension  = "vendor"

	ClusterModelHubCluster     = "hub-cluster"
	ClusterModelManagedCluster = "managed-cluster"

	OpenshiftVersionLabelName = "openshiftVersion"
	ClusterIDLabelName        = "clusterID"
	LocalClusterLabelName     = "local-cluster"
)

// Alertmanager values
const (
	AlertmanagerObjectName = "alertmanager"
	AlertmanagerNamespace  = "open-cluster-management-observability"
	AlertmanagerSA         = "alertmanager"
)
