/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"fmt"
	"time"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
)

// Default namespace
const (
	InventoryNamespace = constants.DefaultNamespace
)

// Base resource names
const (
	InventoryDatabase     = "postgres"
	InventoryResource     = "resource"
	InventoryAlarms       = "alarms"
	InventoryCluster      = "cluster"
	InventoryArtifacts    = "artifacts"
	InventoryProvisioning = "provisioning"
)

// Suffix for server names
const serverSuffix = "-server"

// Deployment names
const (
	InventoryDatabaseServerName     = InventoryDatabase + serverSuffix
	InventoryResourceServerName     = InventoryResource + serverSuffix
	InventoryAlarmServerName        = InventoryAlarms + serverSuffix
	InventoryClusterServerName      = InventoryCluster + serverSuffix
	InventoryArtifactsServerName    = InventoryArtifacts + serverSuffix
	InventoryProvisioningServerName = InventoryProvisioning + serverSuffix
)

const (
	HardwarePluginManager           = "hardwareplugin-manager"
	HardwarePluginManagerServerName = HardwarePluginManager + serverSuffix
)

// HardwarePlugins
const (
	Metal3Plugin           = "metal3-hardwareplugin"
	Metal3PluginServerName = Metal3Plugin + serverSuffix
)

// IngressName defines the name of our ingress controller
const IngressName = "oran-o2ims-ingress"

// IngressClassName defines the ingress controller class to be used
const IngressClassName = "openshift-default"

// IngressPortName defines the name of service port to which our ingress controller directs traffic to
const IngressPortName = "api"

// Resource operations
const (
	UPDATE = "Update"
	PATCH  = "Patch"
)

// Container arguments
var (
	AlarmServerArgs = []string{
		constants.AlarmsServerCmd,
		constants.ServeSubcommand,
		fmt.Sprintf("--api-listener-address=0.0.0.0:%d", constants.DefaultContainerPort),
		fmt.Sprintf("--tls-server-cert=%s/%s", constants.TLSServerMountPath, constants.TLSCertField),
		fmt.Sprintf("--tls-server-key=%s/%s", constants.TLSServerMountPath, constants.TLSKeyField),
	}

	ArtifactsServerArgs = []string{
		constants.ArtifactsServerCmd,
		constants.ServeSubcommand,
		fmt.Sprintf("--api-listener-address=0.0.0.0:%d", constants.DefaultContainerPort),
		fmt.Sprintf("--tls-server-cert=%s/%s", constants.TLSServerMountPath, constants.TLSCertField),
		fmt.Sprintf("--tls-server-key=%s/%s", constants.TLSServerMountPath, constants.TLSKeyField),
	}

	ResourceServerArgs = []string{
		constants.ResourceServerCmd,
		constants.ServeSubcommand,
		fmt.Sprintf("--api-listener-address=0.0.0.0:%d", constants.DefaultContainerPort),
		fmt.Sprintf("--tls-server-cert=%s/%s", constants.TLSServerMountPath, constants.TLSCertField),
		fmt.Sprintf("--tls-server-key=%s/%s", constants.TLSServerMountPath, constants.TLSKeyField),
	}

	ClusterServerArgs = []string{
		constants.ClusterServerCmd,
		constants.ServeSubcommand,
		fmt.Sprintf("--api-listener-address=0.0.0.0:%d", constants.DefaultContainerPort),
		fmt.Sprintf("--tls-server-cert=%s/%s", constants.TLSServerMountPath, constants.TLSCertField),
		fmt.Sprintf("--tls-server-key=%s/%s", constants.TLSServerMountPath, constants.TLSKeyField),
	}

	ProvisioningServerArgs = []string{
		constants.ProvisioningServerCmd,
		constants.ServeSubcommand,
		fmt.Sprintf("--api-listener-address=0.0.0.0:%d", constants.DefaultContainerPort),
		fmt.Sprintf("--tls-server-cert=%s/%s", constants.TLSServerMountPath, constants.TLSCertField),
		fmt.Sprintf("--tls-server-key=%s/%s", constants.TLSServerMountPath, constants.TLSKeyField),
	}

	HardwarePluginManagerArgs = []string{
		constants.HardwarePluginManagerCmd,
		constants.StartSubcommand,
		constants.HealthProbeFlag + "=" + constants.HealthProbePort,
		constants.MetricsFlag + "=" + constants.MetricsPort,
		fmt.Sprintf("--metrics-tls-cert-dir=%s", constants.TLSServerMountPath),
		constants.LeaderElectFlag,
	}

	Metal3PluginServerArgs = []string{
		constants.Metal3HardwarePluginManagerCmd,
		constants.StartSubcommand,
		constants.HealthProbeFlag + "=" + constants.HealthProbePort,
		constants.MetricsFlag + "=" + constants.MetricsPort,
		fmt.Sprintf("--metrics-tls-cert-dir=%s", constants.TLSServerMountPath),
		constants.LeaderElectFlag,
		fmt.Sprintf("--api-listener-address=0.0.0.0:%d", constants.DefaultContainerPort),
		fmt.Sprintf("--tls-server-cert=%s/%s", constants.TLSServerMountPath, constants.TLSCertField),
		fmt.Sprintf("--tls-server-key=%s/%s", constants.TLSServerMountPath, constants.TLSKeyField),
	}
)

// Search API attributes
const (
	SearchApiLabelKey   = "search-monitor"
	SearchApiLabelValue = "search-api"
)

// Default values for backend URL and token:
const (
	defaultApiServerURL    = "https://" + constants.KubernetesAPIService
	defaultBackendCABundle = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt" // nolint: gosec // hardcoded path only
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
	ClusterInstanceTemplateDefaultsConfigmapKey = "clusterinstance-defaults"
	ClusterInstanceCrdName                      = "clusterinstances"
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
		{"nodes", "*", "hostRef"},
		{"nodes", "*", "nodeNetwork", "interfaces", "*", "macAddress"},
		// The interface labels are not part of the ClusterInstance.
		{"nodes", "*", "nodeNetwork", "interfaces", "*", "label"},
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
	HwTemplatePluginMgr             = "hardwarePluginRef"
	HwTemplateNodeAllocationRequest = "node-group-data"
	HwTemplateBootIfaceLabel        = "bootInterfaceLabel"
	HwTemplateExtensions            = "extensions"
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
	UnitTestHwPluginRef    = "hwmgr"
	UnitTestHwmgrNamespace = "hwmgr"
	DefaultPluginNamespace = constants.DefaultNamespace
)

// POD Port Values
const (
	DefaultServiceTargetPort = "https"
	DatabaseTargetPort       = "database"
)

// Environment values
const (
	HwMgrPluginNameSpace = "HWMGR_PLUGIN_NAMESPACE"
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
	ClusterModelExtension             = "model"
	ClusterVersionExtension           = "version"
	ClusterVendorExtension            = "vendor"
	ClusterAlarmDictionaryIDExtension = "alarmDictionaryID"

	ClusterModelHubCluster     = "hub-cluster"
	ClusterModelManagedCluster = "managed-cluster"

	OpenshiftVersionLabelName = "openshiftVersion"
	ClusterIDLabelName        = "clusterID"
	LocalClusterLabelName     = "local-cluster"

	ClusterTemplateArtifactsLabel = "clustertemplates.clcm.openshift.io/templateId"
	HardwarePluginRefLabel        = "clcm.openshift.io/hardwarePluginRef"
	HardwareManagerNodeIdLabel    = "clcm.openshift.io/hwMgrNodeId"
)

// AlarmDefinitionSeverityField severity field within additional fields of alarm definition
const AlarmDefinitionSeverityField = "severity"

// Alertmanager values
const (
	AlertmanagerObjectName                      = "alertmanager"
	OpenClusterManagementObservabilityNamespace = "open-cluster-management-observability"
	AlertmanagerSA                              = "alertmanager"
)

// SMO OAuth specific environment variables.  These values are stored in environment variables to
// avoid them being visible in the command line arguments.
const (
	OAuthClientIDEnvName     = "SMO_OAUTH_CLIENT_ID"
	OAuthClientSecretEnvName = "SMO_OAUTH_CLIENT_SECRET" // nolint: gosec
)

// OAuth Secret fields
const (
	OAuthClientIDField     = "client-id"
	OAuthClientSecretField = "client-secret"
)

// HardwarePluginValidationEndpoint is the endpoint that the HardwarePlugin manager will try to reach for the plugin being
// registered.
const HardwarePluginValidationEndpoint = "/hardware-manager/provisioning/api_versions"

const AllocatedNodeLabel = "clcm.openshift.io/allocated-node"
