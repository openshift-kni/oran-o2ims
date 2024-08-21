package utils

// Default namespace
const (
	ORANO2IMSNamespace = "oran-o2ims"
)

// Base resource names
const (
	ORANO2IMSMetadata          = "metadata"
	ORANO2IMSDeploymentManager = "deployment-manager"
	ORANO2IMSResource          = "resource"
	ORANO2IMSAlarmSubscription = "alarm-subscription"
	ORANO2IMSAlarmNotification = "alarm-notification"
)

// Suffix for server names
const serverSuffix = "-server"

// Deployment names
const (
	ORANO2IMSMetadataServerName          = ORANO2IMSMetadata + serverSuffix
	ORANO2IMSDeploymentManagerServerName = ORANO2IMSDeploymentManager + serverSuffix
	ORANO2IMSResourceServerName          = ORANO2IMSResource + serverSuffix
	ORANO2IMSAlarmSubscriptionServerName = ORANO2IMSAlarmSubscription + serverSuffix
	ORANO2IMSAlarmNotificationServerName = ORANO2IMSAlarmNotification + serverSuffix
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
	ClusterInstanceTemplateConfigmapNamespace   = ORANO2IMSNamespace
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
	HwTemplatePluginMgr = "hwMgrId"
	HwTemplateNodePool  = "node-pools-data"
)

const (
	ClusterInstanceDataType = "ClusterInstance"
	PolicyTemplateDataType  = "PolicyTemplate"
)

// Environment variable names
const (
	TLSSkipVerifyEnvName      = "INSECURE_SKIP_VERIFY"
	TLSSkipVerifyDefaultValue = false
)
