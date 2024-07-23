package controllers

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// The ClusterTemplate and ClusterRequests are still under development, so all the tests
// will need to be rewritten.

/*
const (

	clusterTemplateInputOk = `{
		"additionalNTPSources": [
		  "NTP.server1"
		],
		"baseDomain": "example.com",
		"clusterImageSetNameRef": "openshift-v4.15",
		"caBundleRef": {
		  "name": "my-bundle-ref"
		},
		"clusterLabels": {
		  "common": "true",
		  "group-du-sno": "test",
		  "sites": "site-sno-du-1"
		},
		"clusterName": "site-sno-du-1",
		"clusterNetwork": [
		  {
			"cidr": "10.128.0.0/14"
		  }
		],
		"clusterType": "SNO",
		"diskEncryption": {
		  "tang": [
			{
			  "thumbprint": "1234567890",
			  "url": "http://10.0.0.1:7500"
			}
		  ],
		  "type": "nbde"
		},
		"extraManifestsRefs": [
		  {
			"name": "foobar1"
		  }
		],
		"machineNetwork": [
		  {
			"cidr": "10.16.231.0/24"
		  }
		],
		"networkType": "OVNKubernetes",
		"nodes": [
		  {
			"bmcAddress": "idrac-virtualmedia+https://10.16.231.87/redfish/v1/Systems/System.Embedded.1",
			"bmcCredentialsName": {
			  "name": "site-sno-du-1-bmc-secret"
			},
			"bmcCredentialsDetails": {
			  "username": "YWFh",
			  "password": "YmJi"
			},
			"bootMACAddress": "00:00:00:01:20:30",
			"bootMode": "UEFI",
			"cpuset": "2-19,22-39",
			"hostName": "node1",
			"installerArgs": "[\"--append-karg\", \"nameserver=8.8.8.8\", \"-n\"]",
			"ironicInspect": "",
			"templateRefs": [
			  {
				"name": "ai-cluster-templates-v1",
				"namespace": "siteconfig-operator"
			  }
			],
			"nodeNetwork": {
			  "config": {
				"dns-resolver": {
				  "config": {
					"server": [
					  "10.19.42.41"
					]
				  }
				},
				"interfaces": [
				  {
					"ipv4": {
					  "address": [
						{
						  "ip": "10.16.231.3",
						  "prefix-length": 24
						},
						{
						  "ip": "10.16.231.28",
						  "prefix-length": 24
						},
						{
						  "ip": "10.16.231.31",
						  "prefix-length": 24
						}
					  ],
					  "dhcp": false,
					  "enabled": true
					},
					"ipv6": {
					  "address": [
						{
						  "ip": "2620:52:0:10e7:e42:a1ff:fe8a:601",
						  "prefix-length": 64
						},
						{
						  "ip": "2620:52:0:10e7:e42:a1ff:fe8a:602",
						  "prefix-length": 64
						},
						{
						  "ip": "2620:52:0:10e7:e42:a1ff:fe8a:603",
						  "prefix-length": 64
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
						  "ip": "2620:52:0:1302::100"
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
					  "next-hop-address": "10.16.231.254",
					  "next-hop-interface": "eno1",
					  "table-id": 254
					}
				  ]
				}
			  },
			  "interfaces": [
				{
				  "macAddress": "00:00:00:01:20:30",
				  "name": "eno1"
				},
				{
				  "macAddress": "02:00:00:80:12:14",
				  "name": "eth0"
				},
				{
				  "macAddress": "02:00:00:80:12:15",
				  "name": "eth1"
				}
			  ]
			},
			"role": "master",
			"rootDeviceHints": {
			  "hctl": "1:2:0:0"
			}
		  }
		],
		"proxy": {
		  "noProxy": "foobar"
		},
		"pullSecretRef": {
		  "name": "site-sno-du-1-pull-secret"
		},
		"serviceNetwork": [
		  {
			"cidr": "172.30.0.0/16"
		  }
		],
		"sshPublicKey": "ssh-rsa ",
		"templateRefs": [
		  {
			"name": "ai-cluster-templates-v1",
			"namespace": "siteconfig-operator"
		  }
		]
	 }`
	// NTP servers should be a list, but it's a string and baseDomain is required, but missing.
	clusterTemplateInputMismatch = `{
		"additionalNTPSources":   "NTP.server1",
		"clusterImageSetNameRef": "openshift-v4.15",
		"caBundleRef": {
		  "name": "my-bundle-ref"
		},
		"clusterLabels": {
		  "common": "true",
		  "group-du-sno": "test",
		  "sites": "site-sno-du-1"
		},
		"clusterName": "site-sno-du-1",
		"clusterNetwork": [
		  {
			"cidr": "10.128.0.0/14"
		  }
		],
		"clusterType": "SNO",
		"diskEncryption": {
		  "tang": [
			{
			  "thumbprint": "1234567890",
			  "url": "http://10.0.0.1:7500"
			}
		  ],
		  "type": "nbde"
		},
		"extraManifestsRefs": [
		  {
			"name": "foobar1"
		  }
		],
		"machineNetwork": [
		  {
			"cidr": "10.16.231.0/24"
		  }
		],
		"networkType": "OVNKubernetes",
		"nodes": [
		  {
			"bmcAddress": "idrac-virtualmedia+https://10.16.231.87/redfish/v1/Systems/System.Embedded.1",
			"bmcCredentialsName": {
			  "name": "site-sno-du-1-bmc-secret"
			},
			"bmcCredentialsDetails": {
			  "username": "YWFh",
			  "password": "YmJi"
			},
			"bootMACAddress": "00:00:00:01:20:30",
			"bootMode": "UEFI",
			"cpuset": "2-19,22-39",
			"hostName": "node1",
			"installerArgs": "[\"--append-karg\", \"nameserver=8.8.8.8\", \"-n\"]",
			"ironicInspect": "",
			"templateRefs": [
			  {
				"name": "ai-cluster-templates-v1",
				"namespace": "siteconfig-operator"
			  }
			],
			"nodeNetwork": {
			  "config": {
				"dns-resolver": {
				  "config": {
					"server": [
					  "10.19.42.41"
					]
				  }
				},
				"interfaces": [
				  {
					"ipv4": {
					  "address": [
						{
						  "ip": "10.16.231.3",
						  "prefix-length": 24
						},
						{
						  "ip": "10.16.231.28",
						  "prefix-length": 24
						},
						{
						  "ip": "10.16.231.31",
						  "prefix-length": 24
						}
					  ],
					  "dhcp": false,
					  "enabled": true
					},
					"ipv6": {
					  "address": [
						{
						  "ip": "2620:52:0:10e7:e42:a1ff:fe8a:601",
						  "prefix-length": 64
						},
						{
						  "ip": "2620:52:0:10e7:e42:a1ff:fe8a:602",
						  "prefix-length": 64
						},
						{
						  "ip": "2620:52:0:10e7:e42:a1ff:fe8a:603",
						  "prefix-length": 64
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
						  "ip": "2620:52:0:1302::100"
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
					  "next-hop-address": "10.16.231.254",
					  "next-hop-interface": "eno1",
					  "table-id": 254
					}
				  ]
				}
			  },
			  "interfaces": [
				{
				  "macAddress": "00:00:00:01:20:30",
				  "name": "eno1"
				},
				{
				  "macAddress": "02:00:00:80:12:14",
				  "name": "eth0"
				},
				{
				  "macAddress": "02:00:00:80:12:15",
				  "name": "eth1"
				}
			  ]
			},
			"role": "master",
			"rootDeviceHints": {
			  "hctl": "1:2:0:0"
			}
		  }
		],
		"proxy": {
		  "noProxy": "foobar"
		},
		"pullSecretRef": {
		  "name": "site-sno-du-1-pull-secret"
		},
		"serviceNetwork": [
		  {
			"cidr": "172.30.0.0/16"
		  }
		],
		"sshPublicKey": "ssh-rsa ",
		"templateRefs": [
		  {
			"name": "ai-cluster-templates-v1",
			"namespace": "siteconfig-operator"
		  }
		]
	}`
	ClusterTemplateInputDataSchema = `
	{
		"description": "SiteConfigSpec defines the desired state of SiteConfig.",
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
		  "caBundleRef": {
			"description": "CABundle is a reference to a config map containing the new bundle of trusted certificates for the host. The tls-ca-bundle.pem entry in the config map will be written to /etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem",
			"properties": {
			  "name": {
				"description": "Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names TODO: Add other useful fields. apiVersion, kind, uid?",
				"type": "string"
			  }
			},
			"type": "object",
			"x-kubernetes-map-type": "atomic"
		  },
		  "clusterImageSetNameRef": {
			"description": "ClusterImageSetNameRef is the name of the ClusterImageSet resource indicating which OpenShift version to deploy.",
			"type": "string"
		  },
		  "clusterLabels": {
			"additionalProperties": {
			  "type": "string"
			},
			"description": "ClusterLabels is used to assign labels to the cluster to assist with policy binding.",
			"type": "object"
		  },
		  "clusterName": {
			"description": "ClusterName is the name of the cluster.",
			"type": "string"
		  },
		  "clusterNetwork": {
			"description": "ClusterNetwork is the list of IP address pools for pods.",
			"items": {
			  "description": "ClusterNetworkEntry is a single IP address block for pod IP blocks. IP blocks are allocated with size 2^HostSubnetLength.",
			  "properties": {
				"cidr": {
				  "description": "CIDR is the IP block address pool.",
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
		  "clusterType": {
			"description": "ClusterType is a string representing the cluster type",
			"enum": [
			  "SNO",
			  "HighlyAvailable"
			],
			"type": "string"
		  },
		  "cpuPartitioningMode": {
			"default": "None",
			"description": "CPUPartitioning determines if a cluster should be setup for CPU workload partitioning at install time. When this field is set the cluster will be flagged for CPU Partitioning allowing users to segregate workloads to specific CPU Sets. This does not make any decisions on workloads it only configures the nodes to allow CPU Partitioning. The \"AllNodes\" value will setup all nodes for CPU Partitioning, the default is \"None\".",
			"enum": [
			  "None",
			  "AllNodes"
			],
			"type": "string"
		  },
		  "diskEncryption": {
			"description": "DiskEncryption is the configuration to enable/disable disk encryption for cluster nodes.",
			"properties": {
			  "tang": {
				"items": {
				  "properties": {
					"thumbprint": {
					  "type": "string"
					},
					"url": {
					  "type": "string"
					}
				  },
				  "type": "object"
				},
				"type": "array"
			  },
			  "type": {
				"default": "none",
				"type": "string"
			  }
			},
			"type": "object"
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
		  "extraManifestsRef": {
			"description": "ExtraManifestsRefs is list of config map references containing additional manifests to be applied to the cluster.",
			"items": {
			  "description": "LocalObjectReference contains enough information to let you locate the referenced object inside the same namespace.",
			  "properties": {
				"name": {
				  "description": "Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names TODO: Add other useful fields. apiVersion, kind, uid?",
				  "type": "string"
				}
			  },
			  "type": "object",
			  "x-kubernetes-map-type": "atomic"
			},
			"type": "array"
		  },
		  "holdInstallation": {
			"default": false,
			"description": "HoldInstallation will prevent installation from happening when true. Inspection and validation will proceed as usual, but once the RequirementsMet condition is true, installation will not begin until this field is set to false.",
			"type": "boolean"
		  },
		  "ignitionConfigOverride": {
			"description": "Json formatted string containing the user overrides for the initial ignition config",
			"type": "string"
		  },
		  "ingressVIPs": {
			"description": "IngressVIPs are the virtual IPs used for cluster ingress traffic. Enter one IP address for single-stack clusters, or up to two for dual-stack clusters (at most one IP address per IP stack used). The order of stacks should be the same as order of subnets in Cluster Networks, Service Networks, and Machine Networks.",
			"items": {
			  "type": "string"
			},
			"maxItems": 2,
			"type": "array"
		  },
		  "installConfigOverrides": {
			"description": "InstallConfigOverrides is a Json formatted string that provides a generic way of passing install-config parameters.",
			"type": "string"
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
		  "networkType": {
			"default": "OVNKubernetes",
			"description": "NetworkType is the Container Network Interface (CNI) plug-in to install The default value is OpenShiftSDN for IPv4, and OVNKubernetes for IPv6 or SNO",
			"enum": [
			  "OpenShiftSDN",
			  "OVNKubernetes"
			],
			"type": "string"
		  },
		  "nodes": {
			"items": {
			  "description": "NodeSpec",
			  "properties": {
				"automatedCleaningMode": {
				  "default": "disabled",
				  "description": "When set to disabled, automated cleaning will be avoided during provisioning and deprovisioning. Set the value to metadata to enable the removal of the disk’s partitioning table only, without fully wiping the disk. The default value is disabled.",
				  "enum": [
					"metadata",
					"disabled"
				  ],
				  "type": "string"
				},
				"bmcAddress": {
				  "description": "BmcAddress holds the URL for accessing the controller on the network.",
				  "type": "string"
				},
				"bmcCredentialsName": {
				  "description": "BmcCredentialsName is the name of the secret containing the BMC credentials (requires keys \"username\" and \"password\").",
				  "properties": {
					"name": {
					  "type": "string"
					}
				  },
				  "type": "object"
				},
				"bmcCredentialsDetails": {
				  "description": "BmcCredentialsName requires keys \"username\" and \"password\".",
				  "properties": {
					"username": {
					  "type": "string"
					},
					"password": {
					  "type": "string"
					}
				  },
				  "type": "object"
				},
				"bootMACAddress": {
				  "description": "Which MAC address will PXE boot? This is optional for some types, but required for libvirt VMs driven by vbmc.",
				  "pattern": "[0-9a-fA-F]{2}(:[0-9a-fA-F]{2}){5}",
				  "type": "string"
				},
				"bootMode": {
				  "default": "UEFI",
				  "description": "Provide guidance about how to choose the device for the image being provisioned.",
				  "enum": [
					"UEFI",
					"UEFISecureBoot",
					"legacy"
				  ],
				  "type": "string"
				},
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
				"hostName": {
				  "description": "Hostname is the desired hostname for the host",
				  "type": "string"
				},
				"ignitionConfigOverride": {
				  "description": "Json formatted string containing the user overrides for the host's ignition config IgnitionConfigOverride enables the assignment of partitions for persistent storage. Adjust disk ID and size to the specific hardware.",
				  "type": "string"
				},
				"installerArgs": {
				  "description": "Json formatted string containing the user overrides for the host's coreos installer args",
				  "type": "string"
				},
				"ironicInspect": {
				  "default": "",
				  "description": "IronicInspect is used to specify if automatic introspection carried out during registration of BMH is enabled or disabled",
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
					},
					"interfaces": {
					  "description": "Interfaces is an array of interface objects containing the name and MAC address for interfaces that are referenced in the raw nmstate config YAML. Interfaces listed here will be automatically renamed in the nmstate config YAML to match the real device name that is observed to have the corresponding MAC address. At least one interface must be listed so that it can be used to identify the correct host, which is done by matching any MAC address in this list to any MAC address observed on the host.",
					  "items": {
						"properties": {
						  "macAddress": {
							"description": "mac address present on the host.",
							"pattern": "^([0-9A-Fa-f]{2}[:]){5}([0-9A-Fa-f]{2})$",
							"type": "string"
						  },
						  "name": {
							"description": "nic name used in the yaml, which relates 1:1 to the mac address. Name in REST API: logicalNICName",
							"type": "string"
						  }
						},
						"required": [
						  "macAddress",
						  "name"
						],
						"type": "object"
					  },
					  "minItems": 1,
					  "type": "array"
					}
				  },
				  "type": "object"
				},
				"role": {
				  "default": "master",
				  "enum": [
					"master",
					"worker"
				  ],
				  "type": "string"
				},
				"rootDeviceHints": {
				  "description": "RootDeviceHints specifies the device for deployment. Identifiers that are stable across reboots are recommended, for example, wwn: <disk_wwn> or deviceName: /dev/disk/by-path/<device_path>",
				  "properties": {
					"deviceName": {
					  "description": "A Linux device name like \"/dev/vda\", or a by-path link to it like \"/dev/disk/by-path/pci-0000:01:00.0-scsi-0:2:0:0\". The hint must match the actual value exactly.",
					  "type": "string"
					},
					"hctl": {
					  "description": "A SCSI bus address like 0:0:0:0. The hint must match the actual value exactly.",
					  "type": "string"
					},
					"model": {
					  "description": "A vendor-specific device identifier. The hint can be a substring of the actual value.",
					  "type": "string"
					},
					"rotational": {
					  "description": "True if the device should use spinning media, false otherwise.",
					  "type": "boolean"
					},
					"serialNumber": {
					  "description": "Device serial number. The hint must match the actual value exactly.",
					  "type": "string"
					},
					"vendor": {
					  "description": "The name of the vendor or manufacturer of the device. The hint can be a substring of the actual value.",
					  "type": "string"
					},
					"wwn": {
					  "description": "Unique storage identifier. The hint must match the actual value exactly.",
					  "type": "string"
					},
					"wwnVendorExtension": {
					  "description": "Unique vendor storage identifier. The hint must match the actual value exactly.",
					  "type": "string"
					},
					"wwnWithExtension": {
					  "description": "Unique storage identifier with the vendor extension appended. The hint must match the actual value exactly.",
					  "type": "string"
					}
				  },
				  "type": "object"
				},
				"suppressedManifests": {
				  "description": "SuppressedManifests is a list of node-level manifest names to be excluded from the template rendering process",
				  "items": {
					"type": "string"
				  },
				  "type": "array"
				},
				"templateRefs": {
				  "description": "TemplateRefs is a list of references to node-level templates. A node-level template consists of a ConfigMap in which the keys of the data field represent the kind of the installation manifest(s). Node-level templates are instantiated once for each node in the SiteConfig CR.",
				  "items": {
					"description": "TemplateRef is used to specify the installation CR templates",
					"properties": {
					  "name": {
						"type": "string"
					  },
					  "namespace": {
						"type": "string"
					  }
					},
					"type": "object"
				  },
				  "type": "array"
				}
			  },
			  "required": [
				"bmcAddress",
				"bmcCredentialsName",
				"bmcCredentialsDetails",
				"bootMACAddress",
				"hostName"
			  ],
			  "type": "object"
			},
			"type": "array"
		  },
		  "proxy": {
			"description": "Proxy defines the proxy settings used for the install config",
			"properties": {
			  "httpProxy": {
				"description": "HTTPProxy is the URL of the proxy for HTTP requests.",
				"type": "string"
			  },
			  "httpsProxy": {
				"description": "HTTPSProxy is the URL of the proxy for HTTPS requests.",
				"type": "string"
			  },
			  "noProxy": {
				"description": "NoProxy is a comma-separated list of domains and CIDRs for which the proxy should not be used.",
				"type": "string"
			  }
			},
			"type": "object"
		  },
		  "pullSecretRef": {
			"description": "PullSecretRef is the reference to the secret to use when pulling images.",
			"properties": {
			  "name": {
				"description": "Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names TODO: Add other useful fields. apiVersion, kind, uid?",
				"type": "string"
			  }
			},
			"type": "object",
			"x-kubernetes-map-type": "atomic"
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
		  },
		  "suppressedManifests": {
			"description": "SuppressedManifests is a list of manifest names to be excluded from the template rendering process",
			"items": {
			  "type": "string"
			},
			"type": "array"
		  },
		  "templateRefs": {
			"description": "TemplateRefs is a list of references to cluster-level templates. A cluster-level template consists of a ConfigMap in which the keys of the data field represent the kind of the installation manifest(s). Cluster-level templates are instantiated once per cluster (SiteConfig CR).",
			"items": {
			  "description": "TemplateRef is used to specify the installation CR templates",
			  "properties": {
				"name": {
				  "type": "string"
				},
				"namespace": {
				  "type": "string"
				}
			  },
			  "type": "object"
			},
			"type": "array"
		  }
		},
		"required": [
		  "baseDomain",
		  "clusterImageSetNameRef",
		  "clusterName",
		  "clusterType",
		  "nodes",
		  "pullSecretRef",
		  "templateRefs"
		],
		"type": "object"
	  }
	`

)
*/
var _ = DescribeTable(
	"Reconciler",
	func(objs []client.Object, request reconcile.Request,
		validate func(result ctrl.Result, reconciler ClusterRequestReconciler)) {

		// Declare the Namespace for the ClusterRequest resource.
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster-template",
			},
		}
		// Declare the Namespace for the managed cluster resource.
		nsSite := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "site-sno-du-1",
			},
		}

		// Update the testcase objects to include the Namespace.
		objs = append(objs, ns, nsSite)

		// Get the fake client.
		fakeClient, err := getFakeClientFromObjects(objs...)
		Expect(err).ToNot(HaveOccurred())

		// Initialize the O-RAN O2IMS reconciler.
		r := &ClusterRequestReconciler{
			Client: fakeClient,
			Logger: logger,
		}

		// Reconcile.
		result, err := r.Reconcile(context.TODO(), request)
		Expect(err).ToNot(HaveOccurred())

		validate(result, *r)
	},

	/*
		Entry(
			"ClusterRequest matches ClusterTemplate and the ClusterInstance is created",
			[]client.Object{
				// Cluster Template
				&oranv1alpha1.ClusterTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-template",
						Namespace: "cluster-template",
					},
					Spec: oranv1alpha1.ClusterTemplateSpec{
						InputDataSchema: ClusterTemplateInputDataSchema,
					},
				},
				// Cluster Request
				&oranv1alpha1.ClusterRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-request",
						Namespace: "cluster-template",
						Finalizers: []string{clusterRequestFinalizer},
					},
					Spec: oranv1alpha1.ClusterRequestSpec{
						ClusterTemplateRef: "cluster-template",
						ClusterTemplateInput: clusterTemplateInputOk,
					},
				},
				// Pull secret
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "site-sno-du-1-pull-secret",
						Namespace: "cluster-template",
					},
					Data: map[string][]byte{".dockerconfigjson:": []byte("value1")},
					Type: corev1.SecretTypeDockerConfigJson,
				},
				// BMC secret
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "site-sno-du-1-bmc-secret",
						Namespace: "site-sno-du-1",
					},
					Data: map[string][]byte{
						"username": []byte("username"),
						"password": []byte("password"),
					},
				},
				// Extra-manifests ConfigMap
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-template",
						Namespace: "cluster-template",
					},
					Data: map[string]string{"key1": "value1"},
				},
			},
			reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "cluster-request",
					Namespace: "cluster-template",
				},
			},
			func(result ctrl.Result, reconciler ClusterRequestReconciler) {
				// Get the ClusterRequest and check that everything is valid.
				clusterRequest := &oranv1alpha1.ClusterRequest{}
				err := reconciler.Client.Get(
					context.TODO(),
					types.NamespacedName{
						Name:      "cluster-request",
						Namespace: "cluster-template",
					},
					clusterRequest)
				Expect(err).ToNot(HaveOccurred())
				Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputIsValid).
					To(Equal(true))

				// Get the ClusterInstance and check that everything is valid.
				clusterInstance := &siteconfig.ClusterInstance{}
				err = reconciler.Client.Get(
					context.TODO(),
					types.NamespacedName{
						Name:      "site-sno-du-1",
						Namespace: "site-sno-du-1",
					},
					clusterInstance)
					Expect(err).ToNot(HaveOccurred())
			},
		),

		Entry(
			"ClusterRequest input does not match ClusterTemplate",
			[]client.Object{
				&oranv1alpha1.ClusterTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-template",
						Namespace: "cluster-template",
					},
					Spec: oranv1alpha1.ClusterTemplateSpec{
						InputDataSchema: `{
										"type": "object",
										"properties": {
											"name": {
												"type": "string"
											},
											"age": {
												"type": "integer"
											},
											"email": {
												"type": "string",
												"format": "email"
											},
											"address": {
												"type": "object",
												"properties": {
													"street": {
														"type": "string"
													},
													"city": {
														"type": "string"
													},
													"zipcode": {
														"type": "string"
													},
													"capital": {
													  "type": "boolean"
													}
												},
												"required": ["street", "city"]
											},
											"phoneNumbers": {
												"type": "array",
												"items": {
													"type": "string"
												}
											}
										},
										"required": ["name", "age", "address"]
									  }`,
					},
				},
				&oranv1alpha1.ClusterRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-request",
						Namespace: "cluster-template",
					},
					Spec: oranv1alpha1.ClusterRequestSpec{
						ClusterTemplateRef: "cluster-template",
						ClusterTemplateInput: `{
									"name": "Bob",
									"age": 35,
									"email": "bob@example.com",
									"phoneNumbers": ["123-456-7890", "987-654-3210"]
								  }`,
					},
				},
			},
			reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "cluster-request",
					Namespace: "cluster-template",
				},
			},
			func(result ctrl.Result, reconciler ClusterRequestReconciler) {
				Expect(result).To(Equal(ctrl.Result{}))

				// Get the ClusterRequest and check that everything is valid.
				clusterRequest := &oranv1alpha1.ClusterRequest{}
				err := reconciler.Client.Get(
					context.TODO(),
					types.NamespacedName{
						Name:      "cluster-request",
						Namespace: "cluster-template",
					},
					clusterRequest)
				Expect(err).ToNot(HaveOccurred())
				Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputIsValid).
					To(Equal(true))
				Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputMatchesTemplate).
					To(Equal(false))
				Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputMatchesTemplateError).
					To(ContainSubstring("The JSON input does not match the JSON schema:  (root): address is required"))
			},
		),
	*/
	Entry(
		"ClusterTemplate specified by ClusterTemplateRef is missing and input is invalid",
		[]client.Object{
			&oranv1alpha1.ClusterRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "cluster-request",
					Namespace:  "cluster-template",
					Finalizers: []string{clusterRequestFinalizer},
				},
				Spec: oranv1alpha1.ClusterRequestSpec{
					ClusterTemplateRef: "cluster-template",
					ClusterTemplateInput: oranv1alpha1.ClusterTemplateInput{
						ClusterInstanceInput: `
								"name": "Bob",
								"age": 35,
								"email": "bob@example.com",
								"phoneNumbers": ["123-456-7890", "987-654-3210"]
							}`,
					},
				},
			},
		},
		reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "cluster-request",
				Namespace: "cluster-template",
			},
		},
		func(result ctrl.Result, reconciler ClusterRequestReconciler) {
			// Get the ClusterRequest and check that everything is valid.
			clusterRequest := &oranv1alpha1.ClusterRequest{}
			err := reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      "cluster-request",
					Namespace: "cluster-template",
				},
				clusterRequest)
			Expect(err).ToNot(HaveOccurred())
			Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputIsValid).
				To(Equal(false))
			Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputError).
				To(ContainSubstring("invalid character ':' after top-level value"))
		},
	),

	/*
		Entry(
			"ClusterTemplate change triggers automatic reconciliation",
			[]client.Object{
				&oranv1alpha1.ClusterTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-template",
						Namespace: "cluster-template",
					},
					Spec: oranv1alpha1.ClusterTemplateSpec{
						InputDataSchema: `{
								"type": "object",
								"properties": {
									"name": {
										"type": "string"
									},
									"age": {
										"type": "integer"
									},
									"email": {
										"type": "string",
										"format": "email"
									},
									"address": {
										"type": "object",
										"properties": {
											"street": {
												"type": "string"
											},
											"city": {
												"type": "string"
											},
											"zipcode": {
												"type": "string"
											},
											"capital": {
											  "type": "boolean"
											}
										},
										"required": ["street", "city"]
									},
									"phoneNumbers": {
										"type": "array",
										"items": {
											"type": "string"
										}
									}
								},
								"required": ["name", "age"]
							  }`,
					},
				},
				&oranv1alpha1.ClusterRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-request",
						Namespace: "cluster-template",
					},
					Spec: oranv1alpha1.ClusterRequestSpec{
						ClusterTemplateRef: "cluster-template",
						ClusterTemplateInput: `{
							"name": "Bob",
							"age": 35,
							"email": "bob@example.com",
							"phoneNumbers": ["123-456-7890", "987-654-3210"]
						  }`,
					},
				},
			},
			reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "cluster-request",
					Namespace: "cluster-template",
				},
			},
			func(result ctrl.Result, reconciler ClusterRequestReconciler) {
				// Get the ClusterRequest and check that everything is valid.
				clusterRequest := &oranv1alpha1.ClusterRequest{}
				err := reconciler.Client.Get(
					context.TODO(),
					types.NamespacedName{
						Name:      "cluster-request",
						Namespace: "cluster-template",
					},
					clusterRequest)
				Expect(err).ToNot(HaveOccurred())
				Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputIsValid).
					To(Equal(true))
				Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputMatchesTemplate).
					To(Equal(true))
				Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputMatchesTemplateError).
					To(Equal(""))

				// Update the ClusterTemplate to have the address required.
				// Get the ClusterRequest again.
				clusterTemplate := &oranv1alpha1.ClusterTemplate{}
				err = reconciler.Client.Get(
					context.TODO(),
					types.NamespacedName{
						Name:      "cluster-template",
						Namespace: "cluster-template",
					},
					clusterTemplate)
				Expect(err).ToNot(HaveOccurred())

				clusterTemplate.Spec.InputDataSchema =
					`{
					"type": "object",
					"properties": {
						"name": {
							"type": "string"
						},
						"age": {
							"type": "integer"
						},
						"email": {
							"type": "string",
							"format": "email"
						},
						"address": {
							"type": "object",
							"properties": {
								"street": {
									"type": "string"
								},
								"city": {
									"type": "string"
								},
								"zipcode": {
									"type": "string"
								},
								"capital": {
								  "type": "boolean"
								}
							},
							"required": ["street", "city"]
						},
						"phoneNumbers": {
							"type": "array",
							"items": {
								"type": "string"
							}
						}
					},
					"required": ["name", "age", "address"]
				}`

				err = reconciler.Client.Update(context.TODO(), clusterTemplate)
				Expect(err).ToNot(HaveOccurred())

				// The reconciliation doesn't run automatically here, but we can obtain it
				// from the findClusterRequestsForClusterTemplate function and run it.
				req := reconciler.findClusterRequestsForClusterTemplate(context.TODO(), clusterTemplate)
				Expect(req).To(HaveLen(1))
				_, err = reconciler.Reconcile(context.TODO(), req[0])
				Expect(err).ToNot(HaveOccurred())

				// Get the ClusterRequest again.
				err = reconciler.Client.Get(
					context.TODO(),
					types.NamespacedName{
						Name:      "cluster-request",
						Namespace: "cluster-template",
					},
					clusterRequest)
				Expect(err).ToNot(HaveOccurred())

				// Expect for the ClusterRequest to not match the ClusterTemplate and to
				// report that the required field is missing.
				Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputIsValid).
					To(Equal(true))
				Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputMatchesTemplate).
					To(Equal(false))
				Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputMatchesTemplateError).
					To(ContainSubstring("The JSON input does not match the JSON schema:  (root): address is required"))
			},
		),
	*/
)
