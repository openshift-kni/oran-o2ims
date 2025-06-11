/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package teste2eutils

import (
	"fmt"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
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
	MasterNodeName = "node1"
	BmcSecretName  = "bmc-secret"
)

var (
	TestFullTemplateSchema = fmt.Sprintf(TestFullClusterSchemaTemplate, utils.TemplateParamClusterInstance, TestClusterInstanceSchema,
		utils.TemplateParamPolicyConfig, TestPolicyTemplateSchema)

	TestFullTemplateParameters = fmt.Sprintf(`{
		"%s": "exampleCluster",
		"%s": "local-123",
		"%s": %s,
		"%s": %s
	}`, utils.TemplateParamNodeClusterName,
		utils.TemplateParamOCloudSiteId,
		utils.TemplateParamClusterInstance,
		TestClusterInstanceInput,
		utils.TemplateParamPolicyConfig,
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
			"repoName":    "cluster-group-upgrades-operator",
			"modName":     "github.com/openshift-kni",
			"crdPath":     "config/crd/bases",
			"owner":       "openshift-kni",
			"crdFileName": "lcm.openshift.io_imagebasedgroupupgrades.yaml",
		},
		{
			"repoName":    "oran-hwmgr-plugin",
			"modName":     "github.com/openshift-kni",
			"crdPath":     "config/crd/bases",
			"owner":       "openshift-kni",
			"crdFileName": "hwmgr-plugin.oran.openshift.io_hardwaremanagers.yaml",
		},
	}
)
