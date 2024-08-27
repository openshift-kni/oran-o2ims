package controllers

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type expectedNodeDetails struct {
	BMCAddress         string
	BMCCredentialsName string
	BootMACAddress     string
}

const (
	testClusterTemplateSchema = `{
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
	testClusterTemplateInput = `{
		"additionalNTPSources": [
		  "NTP.server1"
		],
		"baseDomain": "example.com",
		"clusterImageSetNameRef": "openshift-v4.15",
		"caBundleRef": {
		  "name": "my-bundle-ref"
		},
		"clusterLabels": {
		  "cluster-version": "v4.16"
		},
		"clusterName": "cluster-1",
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
			  "username": "aaaa",
			  "password": "aaaa"
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

	testPolicyTemplateSchema = `{
	"type": "object",
	"properties": {
	  "cpu-isolated": {
		"type": "string"
	  }
	}
}`
	testPolicyTemplateInput = `{
	"cpu-isolated": "1-2"
}`
)

/*
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
		fakeClient := getFakeClientFromObjects(objs...)

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

	Entry(
		"ClusterTemplate specified by ClusterTemplateRef is missing and input is valid",
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
						ClusterInstanceInput: runtime.RawExtension{
							Raw: []byte(`{
								"name": "Bob",
								"age": 35,
								"email": "bob@example.com",
								"phoneNumbers": ["123-456-7890", "987-654-3210"]
							}`),
						},
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
		},
	),
)
*/

var _ = Describe("getCrClusterTemplateRef", func() {
	var (
		ctx          context.Context
		c            client.Client
		reconciler   *ClusterRequestReconciler
		task         *clusterRequestReconcilerTask
		ctName       = "clustertemplate-a-v1"
		ctNamespace  = "clustertemplate-a-v4-16"
		ciDefaultsCm = "clusterinstance-defaults-v1"
		ptDefaultsCm = "policytemplate-defaults-v1"
		crName       = "cluster-1"
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Define the cluster request.
		cr := &oranv1alpha1.ClusterRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: ctNamespace,
			},
			Spec: oranv1alpha1.ClusterRequestSpec{
				ClusterTemplateRef: ctName,
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr}...)
		reconciler = &ClusterRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &clusterRequestReconcilerTask{
			logger: reconciler.Logger,
			client: reconciler.Client,
			object: cr,
		}
	})

	It("returns error if the referred ClusterTemplate is missing", func() {
		// Define the cluster template.
		ct := &oranv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-cluster-template-name",
				Namespace: ctNamespace,
			},
			Spec: oranv1alpha1.ClusterTemplateSpec{
				Templates: oranv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
				},
				InputDataSchema: oranv1alpha1.InputDataSchema{
					ClusterInstanceSchema: runtime.RawExtension{},
				},
			},
		}

		Expect(c.Create(ctx, ct)).To(Succeed())

		retCt, err := task.getCrClusterTemplateRef(context.TODO())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			fmt.Sprintf("the referenced ClusterTemplate (%s) does not exist in the %s namespace",
				ctName, ctNamespace)))
		Expect(retCt).To(Equal((*oranv1alpha1.ClusterTemplate)(nil)))
	})

	It("returns the referred ClusterTemplate if it exists", func() {
		// Define the cluster template.
		ct := &oranv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ctName,
				Namespace: ctNamespace,
			},
			Spec: oranv1alpha1.ClusterTemplateSpec{
				Templates: oranv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
				},
				InputDataSchema: oranv1alpha1.InputDataSchema{
					ClusterInstanceSchema: runtime.RawExtension{},
				},
			},
		}

		Expect(c.Create(ctx, ct)).To(Succeed())

		retCt, err := task.getCrClusterTemplateRef(context.TODO())
		Expect(err).ToNot(HaveOccurred())
		Expect(retCt.Name).To(Equal(ctName))
		Expect(retCt.Namespace).To(Equal(ctNamespace))
		Expect(retCt.Spec.Templates.ClusterInstanceDefaults).To(Equal(ciDefaultsCm))
		Expect(retCt.Spec.Templates.PolicyTemplateDefaults).To(Equal(ptDefaultsCm))
	})
})

var _ = Describe("createPolicyTemplateConfigMap", func() {
	var (
		ctx         context.Context
		c           client.Client
		reconciler  *ClusterRequestReconciler
		task        *clusterRequestReconcilerTask
		ctName      = "clustertemplate-a-v1"
		ctNamespace = "clustertemplate-a-v4-16"
		crName      = "cluster-1"
	)

	BeforeEach(func() {
		ctx := context.Background()
		// Define the cluster request.
		cr := &oranv1alpha1.ClusterRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: ctNamespace,
			},
			Spec: oranv1alpha1.ClusterRequestSpec{
				ClusterTemplateRef: ctName,
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr}...)
		reconciler = &ClusterRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &clusterRequestReconcilerTask{
			logger:       reconciler.Logger,
			client:       reconciler.Client,
			object:       cr,
			clusterInput: &clusterInput{},
		}

		// Define the cluster template.
		ztpNs := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ztp-" + ctNamespace,
			},
		}

		Expect(c.Create(ctx, ztpNs)).To(Succeed())
	})

	It("it returns no error if there is no template data", func() {
		err := task.createPolicyTemplateConfigMap(ctx, crName)
		Expect(err).ToNot(HaveOccurred())
	})

	It("it creates the policy template configmap with the correct content", func() {
		// Declare the merged policy template data.
		task.clusterInput.policyTemplateData = map[string]any{
			"cpu-isolated":    "0-1,64-65",
			"hugepages-count": "32",
		}

		// Create the configMap.
		err := task.createPolicyTemplateConfigMap(ctx, crName)
		Expect(err).ToNot(HaveOccurred())

		// Check that the configMap exists in the expected namespace.
		configMapName := crName + "-pg"
		configMapNs := "ztp-" + ctNamespace
		configMap := &corev1.ConfigMap{}
		configMapExists, err := utils.DoesK8SResourceExist(
			ctx, c, configMapName, configMapNs, configMap)
		Expect(err).ToNot(HaveOccurred())
		Expect(configMapExists).To(BeTrue())
		Expect(configMap.Data).To(Equal(
			map[string]string{
				"cpu-isolated":    "0-1,64-65",
				"hugepages-count": "32",
			},
		))
	})
})

var _ = Describe("renderHardwareTemplate", func() {
	var (
		ctx             context.Context
		c               client.Client
		reconciler      *ClusterRequestReconciler
		task            *clusterRequestReconcilerTask
		clusterInstance *unstructured.Unstructured
		ct              *oranv1alpha1.ClusterTemplate
		ctName          = "clustertemplate-a-v1"
		ctNamespace     = "clustertemplate-a-v4-16"
		hwTemplateCm    = "hwTemplate-v1"
		crName          = "cluster-1"
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Define the cluster instance.
		clusterInstance = &unstructured.Unstructured{}
		clusterInstance.SetName(crName)
		clusterInstance.SetNamespace(ctNamespace)
		clusterInstance.Object = map[string]interface{}{
			"spec": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{"role": "master"},
					map[string]interface{}{"role": "master"},
					map[string]interface{}{"role": "worker"},
				},
			},
		}

		// Define the cluster request.
		cr := &oranv1alpha1.ClusterRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: ctNamespace,
			},
			Spec: oranv1alpha1.ClusterRequestSpec{
				ClusterTemplateRef: ctName,
			},
		}

		// Define the cluster template.
		ct = &oranv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ctName,
				Namespace: ctNamespace,
			},
			Spec: oranv1alpha1.ClusterTemplateSpec{
				Templates: oranv1alpha1.Templates{
					HwTemplate: hwTemplateCm,
				},
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr}...)
		reconciler = &ClusterRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &clusterRequestReconcilerTask{
			logger: reconciler.Logger,
			client: reconciler.Client,
			object: cr,
		}
	})

	It("returns no error when renderHardwareTemplate succeeds", func() {
		// Ensure the ClusterTemplate is created
		Expect(c.Create(ctx, ct)).To(Succeed())

		// Define the hardware template config map
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hwTemplateCm,
				Namespace: utils.ORANO2IMSNamespace,
			},
			Data: map[string]string{
				"hwMgrId": "hwmgr",
				utils.HwTemplateNodePool: `
- name: master
  hwProfile: profile-spr-single-processor-64G
- name: worker
  hwProfile: profile-spr-dual-processor-128G`,
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		nodePool, err := task.renderHardwareTemplate(ctx, clusterInstance)
		Expect(err).ToNot(HaveOccurred())
		Expect(nodePool).ToNot(BeNil())
		Expect(nodePool.ObjectMeta.Name).To(Equal(clusterInstance.GetName()))
		Expect(nodePool.ObjectMeta.Namespace).To(Equal(cm.Data[utils.HwTemplatePluginMgr]))

		Expect(nodePool.Spec.CloudID).To(Equal(clusterInstance.GetName()))
		Expect(nodePool.Labels[clusterRequestNameLabel]).To(Equal(task.object.Name))
		Expect(nodePool.Labels[clusterRequestNamespaceLabel]).To(Equal(task.object.Namespace))

		Expect(nodePool.Spec.NodeGroup).To(HaveLen(2))
		for _, group := range nodePool.Spec.NodeGroup {
			switch group.Name {
			case "master":
				Expect(group.Size).To(Equal(2)) // 2 master
			case "worker":
				Expect(group.Size).To(Equal(1)) // 1 worker
			default:
				Fail(fmt.Sprintf("Unexpected Group Name: %s", group.Name))
			}
		}
	})

	It("returns an error when the HwTemplate is not found", func() {
		// Ensure the ClusterTemplate is created
		Expect(c.Create(ctx, ct)).To(Succeed())
		nodePool, err := task.renderHardwareTemplate(ctx, clusterInstance)
		Expect(err).To(HaveOccurred())
		Expect(nodePool).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("failed to get the %s configmap for Hardware Template", hwTemplateCm))
	})

	It("returns an error when the ClusterTemplate is not found", func() {
		nodePool, err := task.renderHardwareTemplate(ctx, clusterInstance)
		Expect(err).To(HaveOccurred())
		Expect(nodePool).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("failed to get the ClusterTemplate"))
	})
})

var _ = Describe("waitForNodePoolProvision", func() {
	var (
		ctx         context.Context
		c           client.Client
		reconciler  *ClusterRequestReconciler
		task        *clusterRequestReconcilerTask
		cr          *oranv1alpha1.ClusterRequest
		ci          *unstructured.Unstructured
		np          *hwv1alpha1.NodePool
		crName      = "cluster-1"
		ctNamespace = "clustertemplate-a-v4-16"
	)

	BeforeEach(func() {
		ctx = context.Background()
		// Define the cluster instance.
		ci = &unstructured.Unstructured{}
		ci.SetName(crName)
		ci.SetNamespace(ctNamespace)
		ci.Object = map[string]interface{}{
			"spec": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{"role": "master"},
					map[string]interface{}{"role": "master"},
					map[string]interface{}{"role": "worker"},
				},
			},
		}

		// Define the cluster request.
		cr = &oranv1alpha1.ClusterRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
		}

		// Define the node pool.
		np = &hwv1alpha1.NodePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
			// Set up your NodePool object as needed
			Status: hwv1alpha1.NodePoolStatus{
				Conditions: []metav1.Condition{},
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr}...)
		reconciler = &ClusterRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &clusterRequestReconcilerTask{
			logger: reconciler.Logger,
			client: reconciler.Client,
			object: cr,
		}
	})

	It("returns false when error fetching NodePool", func() {
		rt := task.waitForNodePoolProvision(ctx, np)
		Expect(rt).To(Equal(false))
	})

	It("returns false when NodePool is not provisioned", func() {
		provisionedCondition := metav1.Condition{
			Type:   "Provisioned",
			Status: metav1.ConditionFalse,
		}
		np.Status.Conditions = append(np.Status.Conditions, provisionedCondition)
		Expect(c.Create(ctx, np)).To(Succeed())

		rt := task.waitForNodePoolProvision(ctx, np)
		Expect(rt).To(Equal(false))
		condition := meta.FindStatusCondition(cr.Status.Conditions, string(utils.CRconditionTypes.HardwareProvisioned))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
	})

	It("returns true when NodePool is provisioned", func() {
		provisionedCondition := metav1.Condition{
			Type:   "Provisioned",
			Status: metav1.ConditionTrue,
		}
		np.Status.Conditions = append(np.Status.Conditions, provisionedCondition)
		Expect(c.Create(ctx, np)).To(Succeed())
		rt := task.waitForNodePoolProvision(ctx, np)
		Expect(rt).To(Equal(true))
		condition := meta.FindStatusCondition(cr.Status.Conditions, string(utils.CRconditionTypes.HardwareProvisioned))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
	})
})

var _ = Describe("updateClusterInstance", func() {
	var (
		ctx         context.Context
		c           client.Client
		reconciler  *ClusterRequestReconciler
		task        *clusterRequestReconcilerTask
		cr          *oranv1alpha1.ClusterRequest
		ci          *unstructured.Unstructured
		np          *hwv1alpha1.NodePool
		crName      = "cluster-1"
		crNamespace = "clustertemplate-a-v4-16"
		mn          = "master-node"
		wn          = "worker-node"
		mhost       = "node1.test.com"
		whost       = "node2.test.com"
		pns         = "hwmgr"
		masterNode  = createNode(mn, "idrac-virtualmedia+https://10.16.2.1/redfish/v1/Systems/System.Embedded.1", "site-1-master-bmc-secret", "00:00:00:01:20:30", "master", pns, crName)
		workerNode  = createNode(wn, "idrac-virtualmedia+https://10.16.3.4/redfish/v1/Systems/System.Embedded.1", "site-1-worker-bmc-secret", "00:00:00:01:30:10", "worker", pns, crName)
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Define the cluster instance.
		ci = &unstructured.Unstructured{}
		ci.SetName(crName)
		ci.SetNamespace(crNamespace)
		ci.Object = map[string]interface{}{
			"spec": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{"role": "master", "hostName": mhost},
					map[string]interface{}{"role": "worker", "hostName": whost},
				},
			},
		}

		// Define the cluster request.
		cr = &oranv1alpha1.ClusterRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
		}

		// Define the node pool.
		np = &hwv1alpha1.NodePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: pns,
			},
			Status: hwv1alpha1.NodePoolStatus{
				Conditions: []metav1.Condition{
					{
						Type:   "Provisioned",
						Status: "True",
					},
				},
				Properties: hwv1alpha1.Properties{
					NodeNames: []string{mn, wn},
				},
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr}...)
		reconciler = &ClusterRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &clusterRequestReconcilerTask{
			logger: reconciler.Logger,
			client: reconciler.Client,
			object: cr,
		}
	})

	It("returns false when failing to get the Node object", func() {
		rt := task.updateClusterInstance(ctx, ci, np)
		Expect(rt).To(Equal(false))
	})

	It("returns true when updateClusterInstance succeeds", func() {
		nodes := []*hwv1alpha1.Node{masterNode, workerNode}
		secrets := createSecrets([]string{masterNode.Status.BMC.CredentialsName, workerNode.Status.BMC.CredentialsName}, pns)

		createResources(c, ctx, nodes, secrets)

		rt := task.updateClusterInstance(ctx, ci, np)
		Expect(rt).To(Equal(true))

		// Define expected details
		expectedDetails := []expectedNodeDetails{
			{
				BMCAddress:         masterNode.Status.BMC.Address,
				BMCCredentialsName: masterNode.Status.BMC.CredentialsName,
				BootMACAddress:     masterNode.Status.BootMACAddress,
			},
			{
				BMCAddress:         workerNode.Status.BMC.Address,
				BMCCredentialsName: workerNode.Status.BMC.CredentialsName,
				BootMACAddress:     workerNode.Status.BootMACAddress,
			},
		}

		// verify the bmc address, secret and boot mac address are set correctly in the cluster instance
		verifyClusterInstance(ci, expectedDetails)

		// verify the host name is set in the node status
		verifyNodeStatus(c, ctx, nodes, mhost, whost)
	})
})

func createNode(name, bmcAddress, bmcSecret, mac, groupName, namespace, npName string) *hwv1alpha1.Node {
	return &hwv1alpha1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: hwv1alpha1.NodeSpec{
			NodePool:  npName,
			GroupName: groupName,
		},
		Status: hwv1alpha1.NodeStatus{
			BMC: &hwv1alpha1.BMC{
				Address:         bmcAddress,
				CredentialsName: bmcSecret,
			},
			BootMACAddress: mac,
		},
	}
}

func createSecrets(names []string, namespace string) []*corev1.Secret {
	var secrets []*corev1.Secret
	for _, name := range names {
		secrets = append(secrets, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		})
	}
	return secrets
}

func createResources(c client.Client, ctx context.Context, nodes []*hwv1alpha1.Node, secrets []*corev1.Secret) {
	for _, node := range nodes {
		Expect(c.Create(ctx, node)).To(Succeed())
	}
	for _, secret := range secrets {
		Expect(c.Create(ctx, secret)).To(Succeed())
	}
}

func verifyClusterInstance(ci *unstructured.Unstructured, expectedDetails []expectedNodeDetails) {
	for i, expected := range expectedDetails {
		node := ci.Object["spec"].(map[string]interface{})["nodes"].([]interface{})[i].(map[string]interface{})
		Expect(node["bmcAddress"]).To(Equal(expected.BMCAddress))
		Expect(node["bmcCredentialsName"].(map[string]interface{})["name"]).To(Equal(expected.BMCCredentialsName))
		Expect(node["bootMACAddress"]).To(Equal(expected.BootMACAddress))
	}
}

func verifyNodeStatus(c client.Client, ctx context.Context, nodes []*hwv1alpha1.Node, mhost, whost string) {
	for _, node := range nodes {
		updatedNode := &hwv1alpha1.Node{}
		Expect(c.Get(ctx, client.ObjectKey{Name: node.Name, Namespace: node.Namespace}, updatedNode)).To(Succeed())
		switch updatedNode.Spec.GroupName {
		case "master":
			Expect(updatedNode.Status.Hostname).To(Equal(mhost))
		case "worker":
			Expect(updatedNode.Status.Hostname).To(Equal(whost))
		default:
			Fail(fmt.Sprintf("Unexpected GroupName: %s", updatedNode.Spec.GroupName))
		}
	}
}

var _ = Describe("policyManagement", func() {
	var (
		ctx          context.Context
		c            client.Client
		CRReconciler *ClusterRequestReconciler
		CRTask       *clusterRequestReconcilerTask
		CTReconciler *ClusterTemplateReconciler
		ctName       = "clustertemplate-a-v1"
		ctNamespace  = "clustertemplate-a-v4-16"
		ciDefaultsCm = "clusterinstance-defaults-v1"
		ptDefaultsCm = "policytemplate-defaults-v1"
		hwTemplateCm = "hwTemplate-v1"
		updateEvent  *event.UpdateEvent
	)

	BeforeEach(func() {
		// Define the needed resources.
		crs := []client.Object{
			// Cluster Template Namespace.
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ctNamespace,
				},
			},
			// Cluster Template.
			&oranv1alpha1.ClusterTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ctName,
					Namespace: ctNamespace,
				},
				Spec: oranv1alpha1.ClusterTemplateSpec{
					Templates: oranv1alpha1.Templates{
						ClusterInstanceDefaults: ciDefaultsCm,
						PolicyTemplateDefaults:  ptDefaultsCm,
						HwTemplate:              hwTemplateCm,
					},
					InputDataSchema: oranv1alpha1.InputDataSchema{
						// APIserver has enforced the validation for this field who holds
						// the arbirary JSON data
						ClusterInstanceSchema: runtime.RawExtension{
							Raw: []byte(testClusterTemplateSchema),
						},
						PolicyTemplateSchema: runtime.RawExtension{
							Raw: []byte(testPolicyTemplateSchema),
						},
					},
				},
			},
			// ConfigMaps.
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ciDefaultsCm,
					Namespace: ctNamespace,
				},
				Data: map[string]string{
					utils.ClusterInstanceTemplateDefaultsConfigmapKey: `key: value`,
				},
			},
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foobar1",
					Namespace: ctNamespace,
				},
				Data: map[string]string{},
			},
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ptDefaultsCm,
					Namespace: ctNamespace,
				},
				Data: map[string]string{
					utils.PolicyTemplateDefaultsConfigmapKey: `
cpu-isolated: "2-31"
cpu-reserved: "0-1"
defaultHugepagesSize: "1G"`,
				},
			},
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      hwTemplateCm,
					Namespace: utils.ORANO2IMSNamespace,
				},
				Data: map[string]string{
					"hwMgrId": "hwmgr",
					utils.HwTemplateNodePool: `
- name: master
  hwProfile: profile-spr-single-processor-64G
- name: worker
  hwProfile: profile-spr-dual-processor-128G`,
				},
			},
			// Pull secret.
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "site-sno-du-1-pull-secret",
					Namespace: ctNamespace,
				},
			},
			// Cluster Requests.
			&oranv1alpha1.ClusterRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "cluster-1",
					Namespace:  ctNamespace,
					Finalizers: []string{clusterRequestFinalizer},
				},
				Spec: oranv1alpha1.ClusterRequestSpec{
					ClusterTemplateRef: ctName,
					ClusterTemplateInput: oranv1alpha1.ClusterTemplateInput{
						ClusterInstanceInput: runtime.RawExtension{
							Raw: []byte(testClusterTemplateInput),
						},
						PolicyTemplateInput: runtime.RawExtension{
							Raw: []byte(testPolicyTemplateInput),
						},
					},
				},
			},
			&oranv1alpha1.ClusterRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "cluster-2",
					Namespace:  ctNamespace,
					Finalizers: []string{clusterRequestFinalizer},
				},
				Spec: oranv1alpha1.ClusterRequestSpec{
					ClusterTemplateRef: ctName,
					ClusterTemplateInput: oranv1alpha1.ClusterTemplateInput{
						ClusterInstanceInput: runtime.RawExtension{
							Raw: []byte(testClusterTemplateInput),
						},
					},
				},
			},
			&oranv1alpha1.ClusterRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "cluster-3",
					Namespace:  ctNamespace,
					Finalizers: []string{clusterRequestFinalizer},
				},
				Spec: oranv1alpha1.ClusterRequestSpec{
					ClusterTemplateRef: ctName,
				},
			},
		}

		c = getFakeClientFromObjects(crs...)
		// Reconcile the ClusterTemplate.
		CTReconciler = &ClusterTemplateReconciler{
			Client: c,
			Logger: logger,
		}

		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      ctName,
				Namespace: ctNamespace,
			},
		}

		_, err := CTReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())

		CRReconciler = &ClusterRequestReconciler{
			Client: c,
			Logger: logger,
		}
	})

	It("It handles updated/deleted policies for matched clusters", func() {

		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "cluster-1",
				Namespace: ctNamespace,
			},
		}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid cluster request.
		Expect(result.Requeue).To(BeFalse())

		// Check updated policies for matched clusters result in reconciliation request.
		updateEvent = &event.UpdateEvent{
			ObjectOld: &policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.policy",
					Namespace: "cluster-1",
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
				},
			},
			ObjectNew: &policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.policy",
					Namespace: "cluster-1",
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "enforce",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
				},
			},
		}
		queue := workqueue.NewRateLimitingQueueWithConfig(
			workqueue.DefaultControllerRateLimiter(),
			workqueue.RateLimitingQueueConfig{
				Name: "ClusterRequestsQueue",
			})
		CRReconciler.findPoliciesForClusterRequestsForUpdate(ctx, *updateEvent, queue)
		Expect(queue.Len()).To(Equal(1))

		// Get the first request from the queue.
		item, shutdown := queue.Get()
		Expect(shutdown).To(BeFalse())

		Expect(item).To(Equal(
			reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      "cluster-1",
				Namespace: ctNamespace,
			}},
		))

		// Check that deleted policies for matched clusters result in reconciliation requests.
		deleteEvent := &event.DeleteEvent{
			Object: &policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.policy",
					Namespace: "cluster-1",
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
				},
			},
		}
		queue = workqueue.NewRateLimitingQueueWithConfig(
			workqueue.DefaultControllerRateLimiter(),
			workqueue.RateLimitingQueueConfig{
				Name: "ClusterRequestsQueue",
			})
		CRReconciler.findPoliciesForClusterRequestsForDelete(ctx, *deleteEvent, queue)
		Expect(queue.Len()).To(Equal(1))

		// Get the first request from the queue.
		item, shutdown = queue.Get()
		Expect(shutdown).To(BeFalse())

		Expect(item).To(Equal(
			reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      "cluster-1",
				Namespace: ctNamespace,
			}},
		))
	})

	It("Updates ClusterRequest ConfigurationApplied condition to OutOfDate when the cluster is "+
		"NonCompliant with at least one matched policies and the policy is not in enforce", func() {
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "cluster-1",
				Namespace: ctNamespace,
			},
		}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid cluster request.
		Expect(result.Requeue).To(BeFalse())

		newPolicies := []client.Object{
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "NonCompliant",
				},
			},
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "enforce",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
				},
			},
		}
		// Create all the ACM policies.
		for _, newPolicy := range newPolicies {
			Expect(c.Create(ctx, newPolicy)).To(Succeed())
		}
		clusterRequest := &oranv1alpha1.ClusterRequest{}

		// Create the ClusterRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{
				Name:      "cluster-1",
				Namespace: "clustertemplate-a-v4-16",
			},
			clusterRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &clusterRequestReconcilerTask{
			logger: CRReconciler.Logger,
			client: CRReconciler.Client,
			object: clusterRequest, // cluster-1 request
		}

		// Call the handleClusterPolicyConfiguration function.
		err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
			[]oranv1alpha1.PolicyDetails{
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-sriov-configuration-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "enforce",
				},
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "inform",
				},
			},
		))

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		Expect(conditions[4].Type).To(Equal(string(utils.CRconditionTypes.ConfigurationApplied)))
		Expect(conditions[4].Status).To(Equal(metav1.ConditionFalse))
		Expect(conditions[4].Reason).To(Equal(string(utils.CRconditionReasons.OutOfDate)))
		Expect(conditions[4].Message).To(Equal("The configuration is out of date"))
	})

	It("Updates ClusterRequest ConfigurationApplied condition to Completed when the cluster is "+
		"Compliant with all the matched policies", func() {
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "cluster-1",
				Namespace: ctNamespace,
			},
		}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid cluster request.
		Expect(result.Requeue).To(BeFalse())

		newPolicies := []client.Object{
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
				},
			},
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "enforce",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
				},
			},
		}
		// Create all the ACM policies.
		for _, newPolicy := range newPolicies {
			Expect(c.Create(ctx, newPolicy)).To(Succeed())
		}
		clusterRequest := &oranv1alpha1.ClusterRequest{}

		// Create the ClusterRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{
				Name:      "cluster-1",
				Namespace: "clustertemplate-a-v4-16",
			},
			clusterRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &clusterRequestReconcilerTask{
			logger: CRReconciler.Logger,
			client: CRReconciler.Client,
			object: clusterRequest, // cluster-1 request
		}

		// Call the handleClusterPolicyConfiguration function.
		err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
			[]oranv1alpha1.PolicyDetails{
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-sriov-configuration-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "enforce",
				},
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "inform",
				},
			},
		))

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		Expect(conditions[4].Type).To(Equal(string(utils.CRconditionTypes.ConfigurationApplied)))
		Expect(conditions[4].Status).To(Equal(metav1.ConditionTrue))
		Expect(conditions[4].Reason).To(Equal(string(utils.CRconditionReasons.Completed)))
		Expect(conditions[4].Message).To(Equal("The configuration is up to date"))
	})

	It("Updates ClusterRequest ConfigurationApplied condition to InProgress when the cluster is "+
		"NonCompliant with at least one enforce policy", func() {
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "cluster-1",
				Namespace: ctNamespace,
			},
		}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid cluster request.
		Expect(result.Requeue).To(BeFalse())

		newPolicies := []client.Object{
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
				},
			},
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "enforce",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "NonCompliant",
				},
			},
		}
		// Create all the ACM policies.
		for _, newPolicy := range newPolicies {
			Expect(c.Create(ctx, newPolicy)).To(Succeed())
		}
		clusterRequest := &oranv1alpha1.ClusterRequest{}

		// Create the ClusterRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{
				Name:      "cluster-1",
				Namespace: "clustertemplate-a-v4-16",
			},
			clusterRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &clusterRequestReconcilerTask{
			logger: CRReconciler.Logger,
			client: CRReconciler.Client,
			object: clusterRequest, // cluster-1 request
		}

		// Call the handleClusterPolicyConfiguration function.
		err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
			[]oranv1alpha1.PolicyDetails{
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v1-sriov-configuration-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "enforce",
				},
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "inform",
				},
			},
		))

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		Expect(conditions[4].Type).To(Equal(string(utils.CRconditionTypes.ConfigurationApplied)))
		Expect(conditions[4].Status).To(Equal(metav1.ConditionFalse))
		Expect(conditions[4].Reason).To(Equal(string(utils.CRconditionReasons.InProgress)))
		Expect(conditions[4].Message).To(Equal("The configuration is still being applied"))
	})
})
