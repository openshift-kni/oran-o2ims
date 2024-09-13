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

// CR default names
const (
	InventoryIngressName   = "api"
	InventoryConfigMapName = "authz"
	InventoryClientSAName  = "client"
)

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
		"--api-listener-address=0.0.0.0:8000",
		"--api-listener-tls-crt=/secrets/tls/tls.crt",
		"--api-listener-tls-key=/secrets/tls/tls.key",
	}
	DeploymentManagerServerArgs = []string{
		"start",
		"deployment-manager-server",
		"--log-level=debug",
		"--log-file=stdout",
		"--api-listener-address=0.0.0.0:8000",
		"--api-listener-tls-crt=/secrets/tls/tls.crt",
		"--api-listener-tls-key=/secrets/tls/tls.key",
		"--authn-jwks-url=https://kubernetes.default.svc/openid/v1/jwks",
		"--authn-jwks-token-file=/run/secrets/kubernetes.io/serviceaccount/token",
		"--authn-jwks-ca-file=/run/secrets/kubernetes.io/serviceaccount/ca.crt",
		"--authz-acl-file=/configmaps/authz/acl.yaml",
	}
	ResourceServerArgs = []string{
		"start",
		"resource-server",
		"--log-level=debug",
		"--log-file=stdout",
		"--api-listener-address=0.0.0.0:8000",
		"--api-listener-tls-crt=/secrets/tls/tls.crt",
		"--api-listener-tls-key=/secrets/tls/tls.key",
	}
)

// Default values for backend URL and token:
const (
	defaultBackendURL       = "https://kubernetes.default.svc"
	defaultBackendTokenFile = "/run/secrets/kubernetes.io/serviceaccount/token"          // nolint: gosec // hardcoded path only
	defaultBackendCABundle  = "/run/secrets/kubernetes.io/serviceaccount/ca.crt"         // nolint: gosec // hardcoded path only
	defaultServiceCAFile    = "/run/secrets/kubernetes.io/serviceaccount/service-ca.crt" // nolint: gosec // hardcoded path only
)

// ClusterInstance template constants
const (
	ClusterInstanceTemplateName                 = "ClusterInstance"
	ClusterInstanceTemplatePath                 = "controllers/clusterinstance-template.yaml"
	ClusterInstanceTemplateConfigmapName        = "sc-clusterinstance-template"
	ClusterInstanceTemplateConfigmapNamespace   = InventoryNamespace
	ClusterInstanceTemplateDefaultsConfigmapKey = "clusterinstance-defaults"
	ClusterInstanceSchema                       = "clusterInstanceSchema"
)

// PolicyTemplate constants
const (
	PolicyTemplateDefaultsConfigmapKey = "policytemplate-defaults"
	PolicyTemplateSchema               = "policyTemplateSchema"
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
)
