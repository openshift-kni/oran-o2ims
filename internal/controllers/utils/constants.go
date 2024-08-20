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

// Deployment names
const (
	ORANO2IMSMetadataServerName          = ORANO2IMSMetadata + "-server"
	ORANO2IMSDeploymentManagerServerName = ORANO2IMSDeploymentManager + "-server"
	ORANO2IMSResourceServerName          = ORANO2IMSResource + "-server"
	ORANO2IMSAlarmSubscriptionServerName = ORANO2IMSAlarmSubscription + "-server"
	ORANO2IMSAlarmNotificationServerName = ORANO2IMSAlarmNotification + "-server"
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
	defaultBackendTokenFile = "/run/secrets/kubernetes.io/serviceaccount/token"
)
