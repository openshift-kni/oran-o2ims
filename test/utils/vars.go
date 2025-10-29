/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package teste2eutils

import (
	"fmt"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	GithubCommitsAPI          = "https://api.github.com/repos/%s/%s/commits/%s"
	GithubUserContentLink     = "https://raw.githubusercontent.com/%s/%s/%s/%s/%s"
	TestClusterInstanceSchema = `{
		"description": "ClusterInstanceSpec defines the params that are allowed in the ProvisioningRequest spec.clusterInstanceInput",
		"properties": {
		  "additionalNTPSources": {
			"description": "AdditionalNTPSources is a list of NTP sources (hostname or IP) to be added to all cluster hosts. They are added to any NTP sources that were configured through other means.",
			"items": {
			  "type": "string"
			},
			"type": "array"
		  },
		  "apiVIPs": {
			"description": "APIVIPs are the virtual IPs used to reach the OpenShift cluster's API. Enter one IP address for single-stack clusters, or up to two for dual-stack clusters (at most one IP address per IP stack used). The order of stacks should be the same as order of subnets in Cluster Networks, Service Networks, and Machine Networks.",
			"items": {
			  "type": "string"
			},
			"maxItems": 2,
			"type": "array"
		  },
		  "baseDomain": {
			"description": "BaseDomain is the base domain to use for the deployed cluster.",
			"type": "string"
		  },
		  "clusterName": {
			"description": "ClusterName is the name of the cluster.",
			"type": "string"
		  },
		  "extraAnnotations": {
			"additionalProperties": {
			  "additionalProperties": {
				"type": "string"
			  },
			  "type": "object"
			},
			"description": "Additional cluster-wide annotations to be applied to the rendered templates",
			"type": "object"
		  },
		  "extraLabels": {
			"additionalProperties": {
			  "additionalProperties": {
				"type": "string"
			  },
			  "type": "object"
			},
			"description": "Additional cluster-wide labels to be applied to the rendered templates",
			"type": "object"
		  },
		  "ingressVIPs": {
			"description": "IngressVIPs are the virtual IPs used for cluster ingress traffic. Enter one IP address for single-stack clusters, or up to two for dual-stack clusters (at most one IP address per IP stack used). The order of stacks should be the same as order of subnets in Cluster Networks, Service Networks, and Machine Networks.",
			"items": {
			  "type": "string"
			},
			"maxItems": 2,
			"type": "array"
		  },
		  "machineNetwork": {
			"description": "MachineNetwork is the list of IP address pools for machines.",
			"items": {
			  "description": "MachineNetworkEntry is a single IP address block for node IP blocks.",
			  "properties": {
				"cidr": {
				  "description": "CIDR is the IP block address pool for machines within the cluster.",
				  "type": "string"
				}
			  },
			  "required": [
				"cidr"
			  ],
			  "type": "object"
			},
			"type": "array"
		  },
		  "nodes": {
			"items": {
			  "description": "NodeSpec",
			  "properties": {
				"extraAnnotations": {
				  "additionalProperties": {
					"additionalProperties": {
					  "type": "string"
					},
					"type": "object"
				  },
				  "description": "Additional node-level annotations to be applied to the rendered templates",
				  "type": "object"
				},
				"extraLabels": {
				  "additionalProperties": {
					"additionalProperties": {
					  "type": "string"
					},
					"type": "object"
				  },
				  "description": "Additional node-level labels to be applied to the rendered templates",
				  "type": "object"
				},
				"hostName": {
				  "description": "Hostname is the desired hostname for the host",
				  "type": "string"
				},
				"nodeLabels": {
				  "additionalProperties": {
					"type": "string"
				  },
				  "description": "NodeLabels allows the specification of custom roles for your nodes in your managed clusters. These are additional roles are not used by any OpenShift Container Platform components, only by the user. When you add a custom role, it can be associated with a custom machine config pool that references a specific configuration for that role. Adding custom labels or roles during installation makes the deployment process more effective and prevents the need for additional reboots after the installation is complete.",
				  "type": "object"
				},
				"nodeNetwork": {
				  "description": "NodeNetwork is a set of configurations pertaining to the network settings for the node.",
				  "properties": {
					"config": {
					  "description": "yaml that can be processed by nmstate, using custom marshaling/unmarshaling that will allow to populate nmstate config as plain yaml.",
					  "type": "object",
					  "x-kubernetes-preserve-unknown-fields": true
					}
				  },
				  "type": "object"
				}
			  },
			  "required": [
				"hostName"
			  ],
			  "type": "object"
			},
			"type": "array"
		  },
		  "serviceNetwork": {
			"description": "ServiceNetwork is the list of IP address pools for services.",
			"items": {
			  "description": "ServiceNetworkEntry is a single IP address block for node IP blocks.",
			  "properties": {
				"cidr": {
				  "description": "CIDR is the IP block address pool for machines within the cluster.",
				  "type": "string"
				}
			  },
			  "required": [
				"cidr"
			  ],
			  "type": "object"
			},
			"type": "array"
		  },
		  "sshPublicKey": {
			"description": "SSHPublicKey is the public Secure Shell (SSH) key to provide access to instances. This key will be added to the host to allow ssh access",
			"type": "string"
		  }
		},
		"required": [
		  "clusterName",
		  "nodes"
		],
		"type": "object"
	  }`

	TestClusterInstanceInput = `{
		"additionalNTPSources": [
		  "NTP.server1"
		],
		"apiVIPs": [
		  "192.0.2.2"
		],
		"baseDomain": "example.com",
		"clusterName": "cluster-1",
		"machineNetwork": [
		  {
			"cidr": "192.0.2.0/24"
		  }
		],
		"extraAnnotations": {
          "AgentClusterInstall": {
		    "extra-annotation-key": "extra-annotation-value"
		  }
		},
		"extraLabels": {
          "AgentClusterInstall": {
		    "extra-label-key": "extra-label-value"
		  },
		  "ManagedCluster": {
			"cluster-version": "v4.17"
		  }
		},
		"ingressVIPs": [
		  "192.0.2.4"
		],
		"nodes": [
		  {
			"hostName": "node1",
			"nodeLabels": {
			  "node-role.kubernetes.io/infra": "",
			  "node-role.kubernetes.io/master": ""
			},
			"nodeNetwork": {
			  "config": {
				"dns-resolver": {
				  "config": {
					"server": [
					  "192.0.2.22"
					]
				  }
				},
				"interfaces": [
				  {
					"ipv4": {
					  "address": [
						{
						  "ip": "192.0.2.10",
						  "prefix-length": 24
						},
						{
						  "ip": "192.0.2.11",
						  "prefix-length": 24
						},
						{
						  "ip": "192.0.2.12",
						  "prefix-length": 24
						}
					  ],
					  "dhcp": false,
					  "enabled": true
					},
					"ipv6": {
					  "address": [
						{
						  "ip": "2001:db8:0:1::42",
						  "prefix-length": 32
						},
						{
						  "ip": "2001:db8:0:1::43",
						  "prefix-length": 32
						},
						{
						  "ip": "2001:db8:0:1::44",
						  "prefix-length": 32
						}
					  ],
					  "dhcp": false,
					  "enabled": true
					},
					"name": "eno1",
					"type": "ethernet"
				  },
				  {
					"ipv6": {
					  "address": [
						{
						  "ip": "2001:db8:abcd:1234::1"
						}
					  ],
					  "enabled": true,
					  "link-aggregation": {
						"mode": "balance-rr",
						"options": {
						  "miimon": "140"
						},
						"slaves": [
						  "eth0",
						  "eth1"
						]
					  },
					  "prefix-length": 64
					},
					"name": "bond99",
					"state": "up",
					"type": "bond"
				  }
				],
				"routes": {
				  "config": [
					{
					  "destination": "0.0.0.0/0",
					  "next-hop-address": "192.0.2.254",
					  "next-hop-interface": "eno1",
					  "table-id": 254
					}
				  ]
				}
			  }
			}
		  }
		],
		"serviceNetwork": [
		  {
			"cidr": "233.252.0.0/24"
		  }
		],
		"sshPublicKey": "ssh-rsa "
	 }`

	TestPolicyTemplateSchema = `{
	"type": "object",
	"properties": {
	  "cpu-isolated": {
		"type": "string"
	  }
	}
}`
	TestPolicyTemplateInput = `{
	"cpu-isolated": "1-2"
}`
	TestFullClusterSchemaTemplate = `{
	"properties": {
		"nodeClusterName": {"type": "string"},
		"oCloudSiteId": {"type": "string"},
		"%s": %s,
		"%s": %s
	},
	"type": "object",
	"required": [
"nodeClusterName",
"oCloudSiteId",
"policyTemplateParameters",
"clusterInstanceParameters"
]
}`
	TestSecretDataStr = `{"auths":{"example.com":{"username":"dummyuser","password": "dummypass","realm":"example.io"}}}` // #nosec G101
)

const (
	MasterNodeName      = "node1"
	BmcSecretName       = "bmc-secret"
	BmhPoolName         = "test-pool"
	TestHwTemplateBlue  = "hwtemplate-blue"
	TestHwTemplateGreen = "hwtemplate-green"
	TestHwProfileName   = "profile-spr-dual-processor-128g"
	TestHwPluginRef     = "hwmgr"
	TestPoolID          = "test-pool-001"
	TestServerType      = "test-server-type"
)

var (
	TestFullTemplateSchema = fmt.Sprintf(TestFullClusterSchemaTemplate, ctlrutils.TemplateParamClusterInstance, TestClusterInstanceSchema,
		ctlrutils.TemplateParamPolicyConfig, TestPolicyTemplateSchema)

	TestFullTemplateParameters = fmt.Sprintf(`{
		"%s": "exampleCluster",
		"%s": "local-123",
		"%s": %s,
		"%s": %s
	}`, ctlrutils.TemplateParamNodeClusterName,
		ctlrutils.TemplateParamOCloudSiteId,
		ctlrutils.TemplateParamClusterInstance,
		TestClusterInstanceInput,
		ctlrutils.TemplateParamPolicyConfig,
		TestPolicyTemplateInput,
	)
	ExternalCrdData = []map[string]string{
		{
			"repoName":    "governance-policy-propagator",
			"modName":     "open-cluster-management.io",
			"crdPath":     "deploy/crds",
			"owner":       "open-cluster-management-io",
			"crdFileName": "policy.open-cluster-management.io_policies.yaml",
		},
		{
			"repoName":    "siteconfig",
			"modName":     "github.com/stolostron",
			"crdPath":     "config/crd/bases",
			"owner":       "stolostron",
			"crdFileName": "siteconfig.open-cluster-management.io_clusterinstances.yaml",
		},
		{
			"repoName":    "multicluster-observability-operator",
			"modulePath":  "github.com/stolostron/multicluster-observability-operator",
			"crdPath":     "multicluster-observability-operator/operators/multiclusterobservability/manifests/endpoint-observability",
			"owner":       "stolostron",
			"crdFileName": "observability.open-cluster-management.io_observabilityaddon_crd.yaml",
		},
		{
			"repoName":    "cluster-group-upgrades-operator",
			"modName":     "github.com/openshift-kni",
			"crdPath":     "config/crd/bases",
			"owner":       "openshift-kni",
			"crdFileName": "lcm.openshift.io_imagebasedgroupupgrades.yaml",
		},
		{
			"repoName":    "hive",
			"modulePath":  "github.com/openshift/hive/apis",
			"crdPath":     "config/crds",
			"owner":       "openshift",
			"crdFileName": "hive.openshift.io_clusterimagesets.yaml",
		},
		{
			"repoName":    "baremetal-operator",
			"modulePath":  "github.com/metal3-io/baremetal-operator/apis",
			"crdPath":     "config/base/crds/bases",
			"owner":       "metal3-io",
			"crdFileName": "metal3.io_baremetalhosts.yaml",
		},
		{
			"repoName":    "baremetal-operator",
			"modulePath":  "github.com/metal3-io/baremetal-operator/apis",
			"crdPath":     "config/base/crds/bases",
			"owner":       "metal3-io",
			"crdFileName": "metal3.io_hardwaredata.yaml",
		},
		{
			"repoName":    "baremetal-operator",
			"modulePath":  "github.com/metal3-io/baremetal-operator/apis",
			"crdPath":     "config/base/crds/bases",
			"owner":       "metal3-io",
			"crdFileName": "metal3.io_hostfirmwarecomponents.yaml",
		},
		{
			"repoName":    "baremetal-operator",
			"modulePath":  "github.com/metal3-io/baremetal-operator/apis",
			"crdPath":     "config/base/crds/bases",
			"owner":       "metal3-io",
			"crdFileName": "metal3.io_hostfirmwaresettings.yaml",
		},
		{
			"repoName":    "baremetal-operator",
			"modulePath":  "github.com/metal3-io/baremetal-operator/apis",
			"crdPath":     "config/base/crds/bases",
			"owner":       "metal3-io",
			"crdFileName": "metal3.io_preprovisioningimages.yaml",
		},
	}

	HardwareProfile = &hwmgmtv1alpha1.HardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestHwProfileName,
			Namespace: constants.DefaultNamespace,
		},
		Spec: hwmgmtv1alpha1.HardwareProfileSpec{
			// Basic hardware profile spec - minimal firmware config to satisfy CRD validation
			BiosFirmware: hwmgmtv1alpha1.Firmware{
				Version: "test-bios-v1.0",
				URL:     "https://example.com/bios-firmware.bin",
			},
		},
	}

	TestBMHs = []struct {
		Name             string
		MacAddress       string
		BmcAddress       string
		Hostname         string
		RamMB            int32
		HwProfile        string
		Colour           string
		StorageSizeBytes metal3v1alpha1.Capacity
		IsPreferred      bool // Only one host will match the strict criteria
	}{
		{
			Name:             "bmh-1",
			MacAddress:       "aa:bb:cc:dd:ee:01",
			BmcAddress:       "redfish://192.168.1.101/redfish/v1/Systems/1",
			Hostname:         "server-node-1.example.com",
			RamMB:            65536, // 64GB - meets minimum but not preferred
			HwProfile:        TestHwProfileName,
			Colour:           "red",
			StorageSizeBytes: 500000000000,
			IsPreferred:      false,
		},
		{
			Name:             "bmh-2",
			MacAddress:       "aa:bb:cc:dd:ee:02",
			BmcAddress:       "redfish://192.168.1.102/redfish/v1/Systems/1",
			Hostname:         "server-node-2.example.com",
			RamMB:            131072, // 128GB - this one will be selected
			HwProfile:        TestHwProfileName,
			Colour:           "blue",
			StorageSizeBytes: 600000000000,
			IsPreferred:      true,
		},
		{
			Name:             "bmh-3",
			MacAddress:       "aa:bb:cc:dd:ee:03",
			BmcAddress:       "redfish://192.168.1.103/redfish/v1/Systems/1",
			Hostname:         "server-node-3.example.com",
			RamMB:            32768, // 32GB - below minimum requirements
			HwProfile:        TestHwProfileName,
			StorageSizeBytes: 6000000000000,
			Colour:           "green",
			IsPreferred:      false,
		},
		{
			Name:             "bmh-4",
			MacAddress:       "aa:bb:cc:dd:ee:04",
			BmcAddress:       "redfish://192.168.1.104/redfish/v1/Systems/1",
			Hostname:         "server-node-4.example.com",
			RamMB:            131072, // 128GB - identical to bmh-2
			HwProfile:        TestHwProfileName,
			Colour:           "blue",
			StorageSizeBytes: 600000000000,
			IsPreferred:      true,
		},
	}
)
