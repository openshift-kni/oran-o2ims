package utils

// Default namespace
const (
	InventoryNamespace = "oran-o2ims"
)

// Base resource names
const (
	InventoryMetadata          = "metadata"
	InventoryDeploymentManager = "deployment-manager"
	InventoryResource          = "resource"
	InventoryAlarmSubscription = "alarm-subscription"
	InventoryAlarmNotification = "alarm-notification"
)

// Suffix for server names
const serverSuffix = "-server"

// Deployment names
const (
	InventoryMetadataServerName          = InventoryMetadata + serverSuffix
	InventoryDeploymentManagerServerName = InventoryDeploymentManager + serverSuffix
	InventoryResourceServerName          = InventoryResource + serverSuffix
	InventoryAlarmSubscriptionServerName = InventoryAlarmSubscription + serverSuffix
	InventoryAlarmNotificationServerName = InventoryAlarmNotification + serverSuffix
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
)

// Default values for backend URL and token:
const (
	defaultBackendURL       = "https://kubernetes.default.svc"
	defaultBackendTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"          // nolint: gosec // hardcoded path only
	defaultBackendCABundle  = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"         // nolint: gosec // hardcoded path only
	defaultServiceCAFile    = "/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt" // nolint: gosec // hardcoded path only
)

// ClusterInstance template constants
const (
	ClusterInstanceTemplateName                 = "ClusterInstance"
	ClusterInstanceTemplatePath                 = "controllers/clusterinstance-template.yaml"
	ClusterInstanceTemplateConfigmapName        = "sc-clusterinstance-template"
	ClusterInstanceTemplateDefaultsConfigmapKey = "clusterinstance-defaults"
	clusterInstanceParameters                   = "clusterInstanceParameters"
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
	}
)

// PolicyTemplate constants
const (
	PolicyTemplateDefaultsConfigmapKey = "policytemplate-defaults"
	PolicyTemplateParameters           = "policyTemplateParameters"
	ClusterVersionLabelKey             = "cluster-version"
)

// ClusterInstance status
const (
	ClusterInstalling = "In progress"
	ClusterCompleted  = "Completed"
	ClusterFailed     = "Failed"
	ClusterZtpDone    = "ZTP Done"
	ClusterZtpNotDone = "ZTP Not Done"
)

// Hardware Provisioning status
const (
	HardwareProvisioningInProgress = "InProgress"
	HardwareProvisioningCompleted  = "Completed"
	HardwareProvisioningFailed     = "Failed"
	HardwareProvisioningUnknown    = "Unknown"
)

// Hardeware template constants
const (
	HwTemplatePluginMgr      = "hwMgrId"
	HwTemplateNodePool       = "node-pools-data"
	HwTemplateBootIfaceLabel = "bootInterfaceLabel"
)

const (
	ClusterInstanceDataType = "ClusterInstance"
	PolicyTemplateDataType  = "PolicyTemplate"
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
	UnitTestHwmgrID         = "hwmgr"
	UnitTestHwmgrNamespace  = "hwmgr"
	TempDellPluginNamespace = "dell-hwmgr"
	DefaultPluginNamespace  = "oran-hwmgr-plugin"
)

// POD Container Names
const (
	ServerContainerName = "server"
	RbacContainerName   = "rbac"
)

// Environment values
const (
	ServerImageName        = "IMAGE"
	KubeRbacProxyImageName = "KUBE_RBAC_PROXY_IMAGE"
	HwMgrPluginNameSpace   = "HWMGR_PLUGIN_NAMESPACE"
)
