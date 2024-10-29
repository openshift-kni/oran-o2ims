package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type expectedNodeDetails struct {
	BMCAddress         string
	BMCCredentialsName string
	BootMACAddress     string
	Interfaces         []map[string]interface{}
}

const (
	testClusterInstanceSchema = `{
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
				"bmcAddress": {
				  "description": "(workaround)BmcAddress holds the URL for accessing the controller on the network.",
				  "type": "string"
				},
				"bmcCredentialsName": {
				  "description": "(workaround)BmcCredentialsName is the name of the secret containing the BMC credentials (requires keys \"username\" and \"password\").",
				  "properties": {
					"name": {
					  "type": "string"
					}
				  },
				  "required": [
					"name"
				  ],
				  "type": "object"
				},
				"bmcCredentialsDetails": {
				  "description": "A workaround to provide bmc creds through ProvisioningRequest",
				  "properties": {
					"username": {
					  "type": "string"
					},
					"password": {
					  "type": "string"
					}
				  }
				},
				"bootMACAddress": {
				  "description": "(workaround)Which MAC address will PXE boot? This is optional for some types, but required for libvirt VMs driven by vbmc.",
				  "pattern": "[0-9a-fA-F]{2}(:[0-9a-fA-F]{2}){5}",
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
					},
					"interfaces": {
					  "description": "Interfaces is an array of interface objects containing the name and MAC address for interfaces that are referenced in the raw nmstate config YAML. Interfaces listed here will be automatically renamed in the nmstate config YAML to match the real device name that is observed to have the corresponding MAC address. At least one interface must be listed so that it can be used to identify the correct host, which is done by matching any MAC address in this list to any MAC address observed on the host.",
					  "items": {
						"properties": {
						  "macAddress": {
							"description": "(workaround)mac address present on the host.",
							"pattern": "^([0-9A-Fa-f]{2}[:]){5}([0-9A-Fa-f]{2})$",
							"type": "string"
						  },
						  "name": {
							"description": "nic name used in the yaml, which relates 1:1 to the mac address. Name in REST API: logicalNICName",
							"type": "string"
						  }
						},
						"type": "object"
					  },
					  "minItems": 1,
					  "type": "array"
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

	testClusterInstanceInput = `{
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
			"bmcAddress": "idrac-virtualmedia+https://203.0.113.5/redfish/v1/Systems/System.Embedded.1",
			"bmcCredentialsName": {
			  "name": "site-sno-du-1-bmc-secret"
			},
			"bmcCredentialsDetails": {
			  "username": "aaaa",
			  "password": "aaaa"
			},
			"bootMACAddress": "00:00:00:01:20:30",
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
	testFullClusterSchemaTemplate = `{
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
)

var (
	testFullTemplateSchema = fmt.Sprintf(testFullClusterSchemaTemplate, utils.TemplateParamClusterInstance, testClusterInstanceSchema,
		utils.TemplateParamPolicyConfig, testPolicyTemplateSchema)

	testFullTemplateParameters = fmt.Sprintf(`{
		"%s": "exampleCluster",
		"%s": "local-123",
		"%s": %s,
		"%s": %s
	}`, utils.TemplateParamNodeClusterName,
		utils.TemplateParamOCloudSiteId,
		utils.TemplateParamClusterInstance,
		testClusterInstanceInput,
		utils.TemplateParamPolicyConfig,
		testPolicyTemplateInput,
	)
)

func verifyStatusCondition(actualCond, expectedCon metav1.Condition) {
	Expect(actualCond.Type).To(Equal(expectedCon.Type))
	Expect(actualCond.Status).To(Equal(expectedCon.Status))
	Expect(actualCond.Reason).To(Equal(expectedCon.Reason))
	if expectedCon.Message != "" {
		Expect(actualCond.Message).To(ContainSubstring(expectedCon.Message))
	}
}

func verifyProvisioningStatus(provStatus provisioningv1alpha1.ProvisioningStatus,
	expectedState provisioningv1alpha1.ProvisioningState, expectedDetail string,
	expectedResources *provisioningv1alpha1.ProvisionedResources) {

	Expect(provStatus.ProvisioningState).To(Equal(expectedState))
	Expect(provStatus.ProvisioningDetails).To(ContainSubstring(expectedDetail))
	Expect(provStatus.ProvisionedResources).To(Equal(expectedResources))
}

func removeRequiredFieldFromClusterInstanceCm(
	ctx context.Context, c client.Client, cmName, cmNamespace string) {
	// Remove a required field from ClusterInstance default configmap
	ciConfigmap := &corev1.ConfigMap{}
	Expect(c.Get(ctx, types.NamespacedName{Name: cmName, Namespace: cmNamespace}, ciConfigmap)).To(Succeed())

	ciConfigmap.Data = map[string]string{
		utils.ClusterInstanceTemplateDefaultsConfigmapKey: `
    clusterImageSetNameRef: "4.15"
    pullSecretRef:
      name: "pull-secret"
    nodes:
    - hostname: "node1"
    templateRefs:
    - name: "ai-node-templates-v1"
      namespace: "siteconfig-operator"
    `}
	Expect(c.Update(ctx, ciConfigmap)).To(Succeed())
}

func removeRequiredFieldFromClusterInstanceInput(
	ctx context.Context, c client.Client, crName string) {
	// Remove required field hostname
	currentCR := &provisioningv1alpha1.ProvisioningRequest{}
	Expect(c.Get(ctx, types.NamespacedName{Name: crName}, currentCR)).To(Succeed())

	clusterTemplateInput := make(map[string]any)
	err := json.Unmarshal([]byte(testFullTemplateParameters), &clusterTemplateInput)
	Expect(err).ToNot(HaveOccurred())
	node1 := clusterTemplateInput["clusterInstanceParameters"].(map[string]any)["nodes"].([]any)[0]
	delete(node1.(map[string]any), "hostName")
	updatedClusterTemplateInput, err := json.Marshal(clusterTemplateInput)
	Expect(err).ToNot(HaveOccurred())

	currentCR.Spec.TemplateParameters.Raw = updatedClusterTemplateInput
	Expect(c.Update(ctx, currentCR)).To(Succeed())
}

var _ = Describe("ProvisioningRequestReconcile", func() {
	var (
		c            client.Client
		ctx          context.Context
		reconciler   *ProvisioningRequestReconciler
		req          reconcile.Request
		cr           *provisioningv1alpha1.ProvisioningRequest
		ct           *provisioningv1alpha1.ClusterTemplate
		tName        = "clustertemplate-a"
		tVersion     = "v1.0.0"
		ctNamespace  = "clustertemplate-a-v4-16"
		ciDefaultsCm = "clusterinstance-defaults-v1"
		ptDefaultsCm = "policytemplate-defaults-v1"
		hwTemplateCm = "hwTemplate-v1"
		crName       = "cluster-1"
	)

	BeforeEach(func() {
		ctx = context.Background()

		crs := []client.Object{
			// Cluster Template Namespace
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ctNamespace,
				},
			},
			// Configmap for ClusterInstance defaults
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ciDefaultsCm,
					Namespace: ctNamespace,
				},
				Data: map[string]string{
					utils.ClusterInstallationTimeoutConfigKey: "60s",
					utils.ClusterInstanceTemplateDefaultsConfigmapKey: `
    clusterImageSetNameRef: "4.15"
    pullSecretRef:
      name: "pull-secret"
    templateRefs:
    - name: "ai-cluster-templates-v1"
      namespace: "siteconfig-operator"
    nodes:
    - hostname: "node1"
      templateRefs:
      - name: "ai-node-templates-v1"
        namespace: "siteconfig-operator"
    `,
				},
			},
			// Configmap for policy template defaults
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ptDefaultsCm,
					Namespace: ctNamespace,
				},
				Data: map[string]string{
					utils.ClusterConfigurationTimeoutConfigKey: "1m",
					utils.PolicyTemplateDefaultsConfigmapKey: `
    cpu-isolated: "2-31"
    cpu-reserved: "0-1"
    defaultHugepagesSize: "1G"`,
				},
			},
			// Configmap for hardware template
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      hwTemplateCm,
					Namespace: utils.InventoryNamespace,
				},
				Data: map[string]string{
					utils.HardwareProvisioningTimeoutConfigKey: "1m",
					"hwMgrId": "hwmgr",
					utils.HwTemplateNodePool: `
    - name: master
      hwProfile: profile-spr-single-processor-64G
    - name: worker
      hwProfile: profile-spr-dual-processor-128G`,
				},
			},
			// Pull secret for ClusterInstance
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pull-secret",
					Namespace: ctNamespace,
				},
			},
		}
		// Define the cluster template.
		ct = &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:       tName,
				Version:    tVersion,
				TemplateID: "57b39bda-ac56-4143-9b10-d1a71517d04f",
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
					HwTemplate:              hwTemplateCm,
				},
				TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testFullTemplateSchema)},
			},
			Status: provisioningv1alpha1.ClusterTemplateStatus{
				Conditions: []metav1.Condition{
					{
						Type:   string(utils.CTconditionTypes.Validated),
						Reason: string(utils.CTconditionReasons.Completed),
						Status: metav1.ConditionTrue,
					},
				},
			},
		}

		// Define the provisioning request.
		cr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:       crName,
				Finalizers: []string{provisioningRequestFinalizer},
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    tName,
				TemplateVersion: tVersion,
				TemplateParameters: runtime.RawExtension{
					Raw: []byte(testFullTemplateParameters),
				},
			},
		}

		crs = append(crs, cr, ct)
		c = getFakeClientFromObjects(crs...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}

		// Request for ProvisioningRequest
		req = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: crName,
			},
		}
	})

	Context("Resources preparation during initial Provisioning", func() {
		It("Verify status conditions if ProvisioningRequest validation fails", func() {
			// Fail the ClusterTemplate validation
			ctValidatedCond := meta.FindStatusCondition(
				ct.Status.Conditions, string(utils.CTconditionTypes.Validated))
			ctValidatedCond.Status = metav1.ConditionFalse
			ctValidatedCond.Reason = string(utils.CTconditionReasons.Failed)
			Expect(c.Status().Update(ctx, ct)).To(Succeed())

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(1))
			verifyStatusCondition(conditions[0], metav1.Condition{
				Type:   string(utils.PRconditionTypes.Validated),
				Status: metav1.ConditionFalse,
				Reason: string(utils.CRconditionReasons.Failed),
				Message: fmt.Sprintf(
					"Failed to validate the ProvisioningRequest: failed to get the ClusterTemplate for "+
						"ProvisioningRequest cluster-1: a valid (%s) ClusterTemplate does not exist in any namespace",
					ct.Name),
			})
			// Verify provisioningState is failed when cr validation fails.
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Failed to validate the ProvisioningRequest", nil)
		})

		It("Verify status conditions if ClusterInstance rendering fails", func() {
			// Fail the ClusterInstance rendering
			removeRequiredFieldFromClusterInstanceCm(ctx, c, ciDefaultsCm, ctNamespace)

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(2))
			verifyStatusCondition(conditions[0], metav1.Condition{
				Type:   string(utils.PRconditionTypes.Validated),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[1], metav1.Condition{
				Type:    string(utils.PRconditionTypes.ClusterInstanceRendered),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.Failed),
				Message: "spec.nodes[0].templateRefs must be provided",
			})

			// Verify provisioningState is failed when the clusterInstance rendering fails.
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Failed to render and validate ClusterInstance", nil)
		})

		It("Verify status conditions if Cluster resources creation fails", func() {
			// Delete the pull secret for ClusterInstance
			secret := &corev1.Secret{}
			secret.SetName("pull-secret")
			secret.SetNamespace(ctNamespace)
			Expect(c.Delete(ctx, secret)).To(Succeed())

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithMediumInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(3))
			verifyStatusCondition(conditions[0], metav1.Condition{
				Type:   string(utils.PRconditionTypes.Validated),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[1], metav1.Condition{
				Type:   string(utils.PRconditionTypes.ClusterInstanceRendered),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[2], metav1.Condition{
				Type:    string(utils.PRconditionTypes.ClusterResourcesCreated),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.Failed),
				Message: "failed to create pull Secret for cluster cluster-1",
			})

			// Verify provisioningState is failed when the required resource creation fails.
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Failed to apply the required cluster resource", nil)
		})

		It("Verify status conditions if all preparation work completes", func() {
			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify NodePool was created
			nodePool := &hwv1alpha1.NodePool{}
			Expect(c.Get(ctx, types.NamespacedName{
				Name: crName, Namespace: "hwmgr"}, nodePool)).To(Succeed())

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(6))
			verifyStatusCondition(conditions[0], metav1.Condition{
				Type:   string(utils.PRconditionTypes.Validated),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[1], metav1.Condition{
				Type:   string(utils.PRconditionTypes.ClusterInstanceRendered),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[2], metav1.Condition{
				Type:   string(utils.PRconditionTypes.ClusterResourcesCreated),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[3], metav1.Condition{
				Type:   string(utils.PRconditionTypes.HardwareTemplateRendered),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(utils.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionUnknown,
				Reason: string(utils.CRconditionReasons.Unknown),
			})
			verifyStatusCondition(conditions[5], metav1.Condition{
				Type:   string(utils.PRconditionTypes.ClusterInstanceProcessed),
				Status: metav1.ConditionUnknown,
				Reason: string(utils.CRconditionReasons.Unknown),
			})
			// Verify the start timestamp has been set for HardwareProvisioning
			Expect(reconciledCR.Status.NodePoolRef.HardwareProvisioningCheckStart).ToNot(BeZero())
			// Verify provisioningState is progressing when nodePool has been created
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Waiting for ClusterInstance (cluster-1) to be processed", nil)
		})
	})

	Context("When NodePool has been created", func() {
		var nodePool *hwv1alpha1.NodePool

		BeforeEach(func() {
			// Create NodePool resource
			nodePool = &hwv1alpha1.NodePool{}
			nodePool.SetName(crName)
			nodePool.SetNamespace("hwmgr")
			nodePool.Status.Conditions = []metav1.Condition{
				{Type: string(hwv1alpha1.Provisioned), Status: metav1.ConditionFalse, Reason: string(hwv1alpha1.InProgress)},
			}
			Expect(c.Create(ctx, nodePool)).To(Succeed())
		})

		It("Verify ClusterInstance should not be created when NodePool provision is in-progress", func() {
			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// TODO: turn it on when hw plugin is ready
			//
			// Verify no ClusterInstance was created
			// clusterInstance := &siteconfig.ClusterInstance{}
			// Expect(c.Get(ctx, types.NamespacedName{
			// 	Name: crName, Namespace: crName}, clusterInstance)).To(HaveOccurred())

			// Verify the ProvisioningRequest's status conditions
			// TODO: change the number of conditions to 5 when hw plugin is ready
			Expect(len(conditions)).To(Equal(6))
			verifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(utils.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(utils.CRconditionReasons.InProgress),
			})
			// Verify the start timestamp has been set for HardwareProvisioning
			Expect(reconciledCR.Status.NodePoolRef.HardwareProvisioningCheckStart).ToNot(BeZero())
			// Verify provisioningState is progressing when nodePool is in-progress
			// TODO: the message should be changed to "Hardware provisioning is in progress"
			//       when workaround code for hw plugin is removed
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Waiting for ClusterInstance (cluster-1) to be processed", nil)
		})

		It("Verify ClusterInstance should be created when NodePool has provisioned", func() {
			// Patch NodePool provision status to Completed
			npProvisionedCond := meta.FindStatusCondition(
				nodePool.Status.Conditions, string(hwv1alpha1.Provisioned),
			)
			npProvisionedCond.Status = metav1.ConditionTrue
			npProvisionedCond.Reason = string(hwv1alpha1.Completed)
			Expect(c.Status().Update(ctx, nodePool)).To(Succeed())

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ClusterInstance was created
			clusterInstance := &siteconfig.ClusterInstance{}
			Expect(c.Get(ctx, types.NamespacedName{
				Name: crName, Namespace: crName}, clusterInstance)).To(Succeed())

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(6))
			verifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(utils.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[5], metav1.Condition{
				Type:   string(utils.PRconditionTypes.ClusterInstanceProcessed),
				Status: metav1.ConditionUnknown,
				Reason: string(utils.CRconditionReasons.Unknown),
			})
			// Verify provisioningState is still progressing when nodePool is provisioned and clusterInstance is created
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Waiting for ClusterInstance (cluster-1) to be processed", nil)
		})

		It("Verify status when HW provision has failed", func() {
			// Patch NodePool provision status to Completed
			npProvisionedCond := meta.FindStatusCondition(
				nodePool.Status.Conditions, string(hwv1alpha1.Provisioned),
			)
			npProvisionedCond.Status = metav1.ConditionFalse
			npProvisionedCond.Reason = string(hwv1alpha1.Failed)
			Expect(c.Status().Update(ctx, nodePool)).To(Succeed())

			// Start reconciliation again
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue())) // stop reconciliation on HwProvision failed

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(5))
			verifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(utils.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(utils.CRconditionReasons.Failed),
			})
			// Verify the provisioningState moves to failed when HW provisioning fails
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Hardware provisioning failed", nil)
		})

		It("Verify status when HW provision has timedout", func() {
			// Initial reconciliation to populate start timestamp
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			cr := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, cr)).To(Succeed())
			// Verify the start timestamp has been set for NodePool
			Expect(cr.Status.NodePoolRef.HardwareProvisioningCheckStart).ToNot(BeZero())

			// Patch HardwareProvisioningCheckStart timestamp to mock timeout
			cr.Status.NodePoolRef.HardwareProvisioningCheckStart.Time = metav1.Now().Add(-2 * time.Minute)
			Expect(c.Status().Update(ctx, cr)).To(Succeed())

			// Start reconciliation again
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue())) // stop reconciliation on HwProvision timeout

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// TODO: change the number of conditions to 5 when hw plugin is ready
			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(6))
			verifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(utils.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(utils.CRconditionReasons.TimedOut),
			})
			// Verify the provisioningState moves to failed when HW provisioning times out
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Hardware provisioning timed out", nil)
		})

		It("Verify status when configuration change causes ProvisioningRequest validation to fail but NodePool becomes provisioned ", func() {
			// Initial reconciliation
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			// Patch NodePool provision status to Completed
			currentNp := &hwv1alpha1.NodePool{}
			Expect(c.Get(ctx, types.NamespacedName{Name: crName, Namespace: "hwmgr"}, currentNp)).To(Succeed())
			npProvisionedCond := meta.FindStatusCondition(
				currentNp.Status.Conditions, string(hwv1alpha1.Provisioned),
			)
			npProvisionedCond.Status = metav1.ConditionTrue
			npProvisionedCond.Reason = string(hwv1alpha1.Completed)
			Expect(c.Status().Update(ctx, currentNp)).To(Succeed())

			// Remove required field hostname to fail ProvisioningRequest validation
			removeRequiredFieldFromClusterInstanceInput(ctx, c, crName)

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// TODO: change the number of conditions to 5 when hw plugin is ready
			// Verify that the validated condition fails but hw provisioned condition
			// has changed to Completed
			Expect(len(conditions)).To(Equal(6))
			verifyStatusCondition(conditions[0], metav1.Condition{
				Type:    string(utils.PRconditionTypes.Validated),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.Failed),
				Message: "nodes.0: hostName is required",
			})
			verifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(utils.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			// Verify the provisioningState remains progressing to reflect the on-going provisioning process
			// even if new changes cause validation to fail
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Waiting for ClusterInstance (cluster-1) to be processed", nil)
		})

		It("Verify status when configuration change causes ClusterInstance rendering to fail but NodePool becomes provisioned", func() {
			// Initial reconciliation
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			// Patch NodePool provision status to Completed
			currentNp := &hwv1alpha1.NodePool{}
			Expect(c.Get(ctx, types.NamespacedName{Name: crName, Namespace: "hwmgr"}, currentNp)).To(Succeed())
			npProvisionedCond := meta.FindStatusCondition(
				currentNp.Status.Conditions, string(hwv1alpha1.Provisioned),
			)
			npProvisionedCond.Status = metav1.ConditionTrue
			npProvisionedCond.Reason = string(hwv1alpha1.Completed)
			Expect(c.Status().Update(ctx, currentNp)).To(Succeed())

			// Fail the ClusterInstance rendering
			removeRequiredFieldFromClusterInstanceCm(ctx, c, ciDefaultsCm, ctNamespace)

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// TODO: change the number of conditions to 5 when hw plugin is ready
			// Verify that the ClusterInstanceRendered condition fails but hw provisioned condition
			// has changed to Completed
			Expect(len(conditions)).To(Equal(6))
			verifyStatusCondition(conditions[1], metav1.Condition{
				Type:    string(utils.PRconditionTypes.ClusterInstanceRendered),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.Failed),
				Message: "spec.nodes[0].templateRefs must be provided",
			})
			verifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(utils.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			// Verify the provisioningState remains progressing to reflect the on-going provisioning process
			// even if new changes cause clusterInstance rendering to fail
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Waiting for ClusterInstance (cluster-1) to be processed", nil)
		})
	})

	Context("When ClusterInstance has been created", func() {
		var (
			nodePool        *hwv1alpha1.NodePool
			clusterInstance *siteconfig.ClusterInstance
			managedCluster  *clusterv1.ManagedCluster
			policy          *policiesv1.Policy
		)
		BeforeEach(func() {
			// Create NodePool resource that has provisioned
			nodePool = &hwv1alpha1.NodePool{}
			nodePool.SetName(crName)
			nodePool.SetNamespace("hwmgr")
			nodePool.Status.Conditions = []metav1.Condition{
				{Type: string(hwv1alpha1.Provisioned), Status: metav1.ConditionTrue, Reason: string(hwv1alpha1.Completed)},
			}
			Expect(c.Create(ctx, nodePool)).To(Succeed())
			// Create ClusterInstance resource
			clusterInstance = &siteconfig.ClusterInstance{}
			clusterInstance.SetName(crName)
			clusterInstance.SetNamespace(crName)
			clusterInstance.Status.Conditions = []metav1.Condition{
				{Type: string(siteconfig.ClusterInstanceValidated), Status: metav1.ConditionTrue},
				{Type: string(siteconfig.RenderedTemplates), Status: metav1.ConditionTrue},
				{Type: string(siteconfig.RenderedTemplatesValidated), Status: metav1.ConditionTrue},
				{Type: string(siteconfig.RenderedTemplatesApplied), Status: metav1.ConditionTrue}}
			Expect(c.Create(ctx, clusterInstance)).To(Succeed())
			// Create ManagedCluster
			managedCluster = &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: crName},
				Status: clusterv1.ManagedClusterStatus{
					Conditions: []metav1.Condition{
						{Type: clusterv1.ManagedClusterConditionAvailable, Status: metav1.ConditionFalse},
						{Type: clusterv1.ManagedClusterConditionHubAccepted, Status: metav1.ConditionTrue},
						{Type: clusterv1.ManagedClusterConditionJoined, Status: metav1.ConditionTrue}},
				},
			}
			Expect(c.Create(ctx, managedCluster)).To(Succeed())
			// Create Non-compliant enforce policy
			policy = &policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-15.v1-sriov-configuration-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-15.v1-sriov-configuration-policy",
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
			}
			Expect(c.Create(ctx, policy)).To(Succeed())
		})

		It("Verify status when ClusterInstance provision is still in progress and ManagedCluster is not ready", func() {
			// Patch ClusterInstance provisioned status to InProgress
			crProvisionedCond := metav1.Condition{
				Type: string(siteconfig.ClusterProvisioned), Status: metav1.ConditionFalse,
				Reason: string(siteconfig.InProgress), Message: "Provisioning cluster",
			}
			clusterInstance.Status.Conditions = append(clusterInstance.Status.Conditions, crProvisionedCond)
			Expect(c.Status().Update(ctx, clusterInstance)).To(Succeed())

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithLongInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(8))
			verifyStatusCondition(conditions[5], metav1.Condition{
				Type:   string(utils.PRconditionTypes.ClusterInstanceProcessed),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[6], metav1.Condition{
				Type:    string(utils.PRconditionTypes.ClusterProvisioned),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.InProgress),
				Message: "Provisioning cluster",
			})
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:    string(utils.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.ClusterNotReady),
				Message: "The Cluster is not yet ready",
			})

			// Verify the start timestamp has been set for ClusterInstance
			Expect(reconciledCR.Status.ClusterDetails.ClusterProvisionStartedAt).ToNot(BeZero())
			// Verify the nonCompliantAt timestamp is not set, even though Non-compliant enforce policy exists
			// but Cluster is not ready
			Expect(reconciledCR.Status.ClusterDetails.NonCompliantAt).To(BeZero())
			// Verify the provisioningState remains progressing when cluster provisioning is in-progress
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Cluster installation is in progress", nil)
		})

		It("Verify status when ClusterInstance provision has timedout", func() {
			// Initial reconciliation to populate ClusterProvisionStartedAt timestamp
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			cr := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, cr)).To(Succeed())
			// Verify the start timestamp has been set for ClusterInstance
			Expect(cr.Status.ClusterDetails.ClusterProvisionStartedAt).ToNot(BeZero())
			// Verify the nonCompliantAt timestamp is not set, even though Non-compliant enforce policy exists
			// but Cluster is not ready
			Expect(cr.Status.ClusterDetails.NonCompliantAt).To(BeZero())

			// Patch ClusterProvisionStartedAt timestamp to mock timeout
			cr.Status.ClusterDetails = &provisioningv1alpha1.ClusterDetails{Name: "cluster-1"}
			cr.Status.ClusterDetails.ClusterProvisionStartedAt.Time = metav1.Now().Add(-2 * time.Minute)
			Expect(c.Status().Update(ctx, cr)).To(Succeed())

			// Start reconciliation again
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue())) // stop reconciliation on ClusterProvision timeout

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(8))
			verifyStatusCondition(conditions[6], metav1.Condition{
				Type:   string(utils.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(utils.CRconditionReasons.TimedOut),
			})
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:    string(utils.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.ClusterNotReady),
				Message: "The Cluster is not yet ready",
			})
			// Verify the start timestamp has been set for HardwareProvisioning
			Expect(reconciledCR.Status.NodePoolRef.HardwareProvisioningCheckStart).ToNot(BeZero())
			// Verify the provisioningState moves to failed when cluster provisioning times out
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Cluster installation timed out", nil)
		})

		It("Verify status when ClusterInstance provision has failed", func() {
			// Patch ClusterInstance provisioned status to failed
			crProvisionedCond := metav1.Condition{
				Type: string(siteconfig.ClusterProvisioned), Status: metav1.ConditionFalse,
				Reason: string(siteconfig.Failed), Message: "Provisioning failed",
			}
			clusterInstance.Status.Conditions = append(clusterInstance.Status.Conditions, crProvisionedCond)
			Expect(c.Status().Update(ctx, clusterInstance)).To(Succeed())

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue())) // stop reconciliation on ClusterProvision failed

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(7))
			verifyStatusCondition(conditions[6], metav1.Condition{
				Type:   string(utils.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(utils.CRconditionReasons.Failed),
			})
			// Verify the provisioningState moves to failed when cluster provisioning is failed
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Cluster installation failed", nil)
		})

		It("Verify status when ClusterInstance provision has completed, ManagedCluster becomes ready and non-compliant enforce policy is being applied", func() {
			// Patch ClusterInstance provisioned status to Provisioned
			crProvisionedCond := metav1.Condition{
				Type: string(siteconfig.ClusterProvisioned), Status: metav1.ConditionTrue,
				Reason: string(siteconfig.Completed), Message: "Provisioning completed",
			}
			clusterInstance.Status.Conditions = append(clusterInstance.Status.Conditions, crProvisionedCond)
			Expect(c.Status().Update(ctx, clusterInstance)).To(Succeed())
			// Patch ManagedCluster to ready
			readyCond := meta.FindStatusCondition(
				managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
			readyCond.Status = metav1.ConditionTrue
			Expect(c.Status().Update(ctx, managedCluster)).To(Succeed())
			// Patch ManagedCluster with clusterID label
			managedCluster.SetLabels(map[string]string{"clusterID": "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})
			Expect(c.Update(ctx, managedCluster)).To(Succeed())

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithLongInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(8))
			verifyStatusCondition(conditions[6], metav1.Condition{
				Type:   string(utils.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:    string(utils.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.InProgress),
				Message: "The configuration is still being applied",
			})

			// Verify the start timestamp has been set for ClusterInstance
			Expect(reconciledCR.Status.ClusterDetails.ClusterProvisionStartedAt).ToNot(BeZero())
			// Verify the oCloudNodeClusterId is not stored when cluster configuration is still in-progress
			Expect(reconciledCR.Status.ProvisioningStatus.ProvisionedResources).To(BeNil())
			// Verify the provisioningState remains progressing when cluster configuration is in-progress
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Cluster configuration is being applied", nil)
		})

		It("Verify status when ClusterInstance provision has completed, ManagedCluster becomes ready and configuration policy becomes compliant", func() {
			// Patch ClusterInstance provisioned status to Completed
			crProvisionedCond := metav1.Condition{
				Type: string(siteconfig.ClusterProvisioned), Status: metav1.ConditionTrue,
				Reason: string(siteconfig.Completed), Message: "Provisioning completed",
			}
			clusterInstance.Status.Conditions = append(clusterInstance.Status.Conditions, crProvisionedCond)
			Expect(c.Status().Update(ctx, clusterInstance)).To(Succeed())
			// Patch ManagedCluster to ready
			readyCond := meta.FindStatusCondition(
				managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
			readyCond.Status = metav1.ConditionTrue
			Expect(c.Status().Update(ctx, managedCluster)).To(Succeed())
			// Patch ManagedCluster with clusterID label
			managedCluster.SetLabels(map[string]string{"clusterID": "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})
			Expect(c.Update(ctx, managedCluster)).To(Succeed())

			// Patch enforce policy to Compliant
			policy.Status.ComplianceState = policiesv1.Compliant
			Expect(c.Status().Update(ctx, policy)).To(Succeed())

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(8))
			verifyStatusCondition(conditions[6], metav1.Condition{
				Type:   string(utils.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:    string(utils.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionTrue,
				Reason:  string(utils.CRconditionReasons.Completed),
				Message: "The configuration is up to date",
			})

			// Verify the start timestamp is not cleared even Cluster provision has completed
			Expect(reconciledCR.Status.ClusterDetails.ClusterProvisionStartedAt).ToNot(BeZero())
			// Verify the nonCompliantAt timestamp is not set since enforce policy is compliant
			Expect(reconciledCR.Status.ClusterDetails.NonCompliantAt).To(BeZero())
			// Verify the ztpStatus is set to ZTP done
			Expect(reconciledCR.Status.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpDone))
			// Verify the provisioningState sets to fulfilled when the provisioning process is completed
			// and oCloudNodeClusterId is stored
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFulfilled, "Cluster has installed and configured successfully",
				&provisioningv1alpha1.ProvisionedResources{OCloudNodeClusterId: "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})
		})

		It("Verify status when configuration change causes ProvisioningRequest validation to fail but ClusterProvision is still in progress", func() {
			// Patch ClusterInstance provisioned status to InProgress
			crProvisionedCond := metav1.Condition{
				Type: string(siteconfig.ClusterProvisioned), Status: metav1.ConditionFalse,
				Reason: string(siteconfig.InProgress), Message: "Provisioning cluster",
			}
			clusterInstance.Status.Conditions = append(clusterInstance.Status.Conditions, crProvisionedCond)
			Expect(c.Status().Update(ctx, clusterInstance)).To(Succeed())

			// Initial reconciliation to populate initial status.
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			// Remove required field hostname to fail ProvisioningRequest validation.
			removeRequiredFieldFromClusterInstanceInput(ctx, c, crName)

			// Start reconciliation again
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithLongInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify that the Validated condition fails but ClusterProvisioned condition
			// is also up-to-date with the current status timeout.
			Expect(len(conditions)).To(Equal(8))
			verifyStatusCondition(conditions[0], metav1.Condition{
				Type:    string(utils.PRconditionTypes.Validated),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.Failed),
				Message: "nodes.0: hostName is required",
			})
			verifyStatusCondition(conditions[6], metav1.Condition{
				Type:   string(utils.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(utils.CRconditionReasons.InProgress),
			})
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:    string(utils.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.ClusterNotReady),
				Message: "The Cluster is not yet ready",
			})
			// Verify the provisioningState remains progressing to reflect the on-going provisioning process
			// even if new changes cause validation to fail
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Cluster installation is in progress", nil)
		})

		It("Verify status when configuration change causes ProvisioningRequest validation to fail but ClusterInstance "+
			"becomes provisioned and policy configuration is still being applied", func() {
			// Initial reconciliation
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			// Patch ClusterInstance provisioned status to Completed.
			crProvisionedCond := metav1.Condition{
				Type: string(siteconfig.ClusterProvisioned), Status: metav1.ConditionTrue,
				Reason: string(siteconfig.Completed), Message: "Provisioning completed",
			}
			currentCI := &siteconfig.ClusterInstance{}
			Expect(c.Get(ctx, types.NamespacedName{Name: crName, Namespace: crName}, currentCI)).To(Succeed())
			currentCI.Status.Conditions = append(clusterInstance.Status.Conditions, crProvisionedCond)
			Expect(c.Status().Update(ctx, currentCI)).To(Succeed())
			// Patch ManagedCluster to ready
			readyCond := meta.FindStatusCondition(
				managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
			readyCond.Status = metav1.ConditionTrue
			Expect(c.Status().Update(ctx, managedCluster)).To(Succeed())
			// Patch ManagedCluster with clusterID label
			managedCluster.SetLabels(map[string]string{"clusterID": "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})
			Expect(c.Update(ctx, managedCluster)).To(Succeed())

			// Remove required field hostname to fail ProvisioningRequest validation.
			removeRequiredFieldFromClusterInstanceInput(ctx, c, crName)

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithLongInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify that the Validated condition fails but ClusterProvisioned condition
			// has changed to Completed.
			Expect(len(conditions)).To(Equal(8))
			verifyStatusCondition(conditions[0], metav1.Condition{
				Type:    string(utils.PRconditionTypes.Validated),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.Failed),
				Message: "nodes.0: hostName is required",
			})
			verifyStatusCondition(conditions[6], metav1.Condition{
				Type:   string(utils.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:    string(utils.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.InProgress),
				Message: "The configuration is still being applied",
			})

			// Verify the start timestamp is not cleared even Cluster provision has completed
			Expect(reconciledCR.Status.ClusterDetails.ClusterProvisionStartedAt).ToNot(BeZero())
			// Verify the provisioningState remains progressing to reflect to the on-going provisioning process
			// even if new changes cause validation to fail.
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Cluster configuration is being applied", nil)
		})

		It("Verify status when configuration change causes ProvisioningRequest validation to fail but ClusterProvision becomes timeout", func() {
			// Initial reconciliation to populate initial status.
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			cr := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, cr)).To(Succeed())
			// Verify the start timestamp has been set for ClusterInstance.
			Expect(cr.Status.ClusterDetails.ClusterProvisionStartedAt).ToNot(BeZero())
			// Patch ClusterProvisionStartedAt timestamp to mock timeout
			cr.Status.ClusterDetails = &provisioningv1alpha1.ClusterDetails{Name: "cluster-1"}
			cr.Status.ClusterDetails.ClusterProvisionStartedAt.Time = metav1.Now().Add(-2 * time.Minute)
			Expect(c.Status().Update(ctx, cr)).To(Succeed())

			// Remove required field hostname to fail ProvisioningRequest validation.
			removeRequiredFieldFromClusterInstanceInput(ctx, c, crName)

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue())) // stop reconciliation on ClusterProvision timeout

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify that the Validated condition fails but ClusterProvisioned condition
			// is also up-to-date with the current status timeout.
			Expect(len(conditions)).To(Equal(8))
			verifyStatusCondition(conditions[0], metav1.Condition{
				Type:    string(utils.PRconditionTypes.Validated),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.Failed),
				Message: "nodes.0: hostName is required",
			})
			verifyStatusCondition(conditions[6], metav1.Condition{
				Type:   string(utils.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(utils.CRconditionReasons.TimedOut),
			})
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:    string(utils.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.ClusterNotReady),
				Message: "The Cluster is not yet ready",
			})
			// Verify the provisioningState has changed to failed as on-going provisioning process has reached to
			// a final state timedout and new changes cause validation to fail
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Failed to validate the ProvisioningRequest", nil)
		})

		It("Verify status when configuration change causes ClusterInstance rendering to fail but configuration policy becomes compliant", func() {
			// Initial reconciliation
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			// Patch ClusterInstance provisioned status to Completed
			crProvisionedCond := metav1.Condition{
				Type: string(siteconfig.ClusterProvisioned), Status: metav1.ConditionTrue,
				Reason: string(siteconfig.Completed), Message: "Provisioning completed",
			}
			currentCI := &siteconfig.ClusterInstance{}
			Expect(c.Get(ctx, types.NamespacedName{Name: crName, Namespace: crName}, currentCI)).To(Succeed())
			currentCI.Status.Conditions = append(clusterInstance.Status.Conditions, crProvisionedCond)
			Expect(c.Status().Update(ctx, currentCI)).To(Succeed())
			// Patch ManagedCluster to ready
			readyCond := meta.FindStatusCondition(
				managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
			readyCond.Status = metav1.ConditionTrue
			Expect(c.Status().Update(ctx, managedCluster)).To(Succeed())
			// Patch ManagedCluster with clusterID label
			managedCluster.SetLabels(map[string]string{"clusterID": "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})
			Expect(c.Update(ctx, managedCluster)).To(Succeed())
			// Patch enforce policy to Compliant
			policy.Status.ComplianceState = policiesv1.Compliant
			Expect(c.Status().Update(ctx, policy)).To(Succeed())

			// Fail the ClusterInstance rendering
			removeRequiredFieldFromClusterInstanceCm(ctx, c, ciDefaultsCm, ctNamespace)

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify that the ClusterInstanceRendered condition fails but configurationApplied
			// has changed to Completed
			Expect(len(conditions)).To(Equal(8))
			verifyStatusCondition(conditions[1], metav1.Condition{
				Type:    string(utils.PRconditionTypes.ClusterInstanceRendered),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.Failed),
				Message: "spec.nodes[0].templateRefs must be provided",
			})
			verifyStatusCondition(conditions[6], metav1.Condition{
				Type:   string(utils.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:    string(utils.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionTrue,
				Reason:  string(utils.CRconditionReasons.Completed),
				Message: "The configuration is up to date",
			})

			// Verify the ztpStatus is set to ZTP done
			Expect(reconciledCR.Status.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpDone))
			// Verify the oCloudNodeClusterId is stored and the provisioningState has changed to failed,
			// as on-going provisioning process has reached to a final state completed and new changes
			// cause rendering to fail.
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Failed to render and validate ClusterInstance",
				&provisioningv1alpha1.ProvisionedResources{OCloudNodeClusterId: "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})
		})

		It("Verify status when configuration change causes ProvisioningRequest validation to fail after provisioning has fulfilled", func() {
			// Patch ClusterInstance provisioned status to Completed
			crProvisionedCond := metav1.Condition{
				Type: string(siteconfig.ClusterProvisioned), Status: metav1.ConditionTrue,
				Reason: string(siteconfig.Completed), Message: "Provisioning completed",
			}
			currentCI := &siteconfig.ClusterInstance{}
			Expect(c.Get(ctx, types.NamespacedName{Name: crName, Namespace: crName}, currentCI)).To(Succeed())
			currentCI.Status.Conditions = append(clusterInstance.Status.Conditions, crProvisionedCond)
			Expect(c.Status().Update(ctx, currentCI)).To(Succeed())
			// Patch ManagedCluster to ready
			readyCond := meta.FindStatusCondition(
				managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
			readyCond.Status = metav1.ConditionTrue
			Expect(c.Status().Update(ctx, managedCluster)).To(Succeed())
			// Patch ManagedCluster with clusterID label
			managedCluster.SetLabels(map[string]string{"clusterID": "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})
			Expect(c.Update(ctx, managedCluster)).To(Succeed())
			// Patch enforce policy to Compliant
			policy.Status.ComplianceState = policiesv1.Compliant
			Expect(c.Status().Update(ctx, policy)).To(Succeed())
			// Initial reconciliation to fulfill the provisioning
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			// Remove required field hostname to fail ProvisioningRequest validation.
			removeRequiredFieldFromClusterInstanceInput(ctx, c, crName)

			// Start reconciliation again
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			Expect(len(conditions)).To(Equal(8))
			verifyStatusCondition(conditions[6], metav1.Condition{
				Type:   string(utils.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:    string(utils.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionTrue,
				Reason:  string(utils.CRconditionReasons.Completed),
				Message: "The configuration is up to date",
			})

			// Verify the ztpStatus is still set to ZTP done
			Expect(reconciledCR.Status.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpDone))
			// Verify the oCloudNodeClusterId is still stored
			Expect(reconciledCR.Status.ProvisioningStatus.ProvisionedResources.OCloudNodeClusterId).To(Equal("76b8cbad-9928-48a0-bcf0-bb16a777b5f7"))
			// Verify the oCloudNodeClusterId is stored and the provisioningState has changed to failed,
			// as on-going provisioning process has reached to a final state completed and new changes
			// cause validation to fail.
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Failed to validate the ProvisioningRequest",
				&provisioningv1alpha1.ProvisionedResources{OCloudNodeClusterId: "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})
		})

		It("Verify status when enforce policy becomes non-compliant after provisioning has fulfilled", func() {
			// Patch ClusterInstance provisioned status to Completed
			crProvisionedCond := metav1.Condition{
				Type: string(siteconfig.ClusterProvisioned), Status: metav1.ConditionTrue,
				Reason: string(siteconfig.Completed), Message: "Provisioning completed",
			}
			currentCI := &siteconfig.ClusterInstance{}
			Expect(c.Get(ctx, types.NamespacedName{Name: crName, Namespace: crName}, currentCI)).To(Succeed())
			currentCI.Status.Conditions = append(clusterInstance.Status.Conditions, crProvisionedCond)
			Expect(c.Status().Update(ctx, currentCI)).To(Succeed())
			// Patch ManagedCluster to ready
			readyCond := meta.FindStatusCondition(
				managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
			readyCond.Status = metav1.ConditionTrue
			Expect(c.Status().Update(ctx, managedCluster)).To(Succeed())
			// Patch ManagedCluster with clusterID label
			managedCluster.SetLabels(map[string]string{"clusterID": "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})
			Expect(c.Update(ctx, managedCluster)).To(Succeed())
			// Patch enforce policy to Compliant
			policy.Status.ComplianceState = policiesv1.Compliant
			Expect(c.Status().Update(ctx, policy)).To(Succeed())

			// Initial reconciliation to fulfill the provisioning
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			// Patch enforce policy to non-Compliant
			policy.Status.ComplianceState = policiesv1.NonCompliant
			Expect(c.Status().Update(ctx, policy)).To(Succeed())

			// Start reconciliation again
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithLongInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			Expect(len(conditions)).To(Equal(8))
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:    string(utils.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.InProgress),
				Message: "The configuration is still being applied",
			})

			// Verify the ztpStatus is still set to ZTP done
			Expect(reconciledCR.Status.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpDone))
			// Verify the oCloudNodeClusterId is still stored and the provisioningState becomes to progressing since configuration is in-progress
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Cluster configuration is being applied",
				&provisioningv1alpha1.ProvisionedResources{OCloudNodeClusterId: "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})
		})
	})

	Context("When evaluating ZTP Done", func() {
		var (
			policy         *policiesv1.Policy
			managedCluster *clusterv1.ManagedCluster
		)

		BeforeEach(func() {
			policy = &policiesv1.Policy{
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
					RemediationAction: "enforce",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: policiesv1.NonCompliant,
				},
			}
			Expect(c.Create(ctx, policy)).To(Succeed())

			managedCluster = &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
				},
				Spec: clusterv1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
				Status: clusterv1.ManagedClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   clusterv1.ManagedClusterConditionAvailable,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.ManagedClusterConditionHubAccepted,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.ManagedClusterConditionJoined,
							Status: metav1.ConditionTrue,
						},
					},
				},
			}
			Expect(c.Create(ctx, managedCluster)).To(Succeed())

			provisionedCond := metav1.Condition{
				Type:   string(utils.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionFalse,
			}
			cr.Status.Conditions = append(cr.Status.Conditions, provisionedCond)
			cr.Status.ClusterDetails = &provisioningv1alpha1.ClusterDetails{}
			cr.Status.ClusterDetails.Name = crName
			cr.Status.ClusterDetails.ClusterProvisionStartedAt = metav1.Now()
			Expect(c.Status().Update(ctx, cr)).To(Succeed())
		})

		It("Sets the status to ZTP Not Done", func() {
			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithLongInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())

			Expect(reconciledCR.Status.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpNotDone))
			conditions := reconciledCR.Status.Conditions
			// Verify the ProvisioningRequest's status conditions
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:    string(utils.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.InProgress),
				Message: "The configuration is still being applied",
			})
		})

		It("Sets the status to ZTP Done", func() {
			// Set the policies to compliant.
			policy.Status.ComplianceState = policiesv1.Compliant
			Expect(c.Status().Update(ctx, policy)).To(Succeed())
			// Complete the cluster provisioning.
			cr.Status.Conditions[0].Status = metav1.ConditionTrue
			cr.Status.Conditions[0].Reason = string(utils.CRconditionReasons.Completed)
			Expect(c.Status().Update(ctx, cr)).To(Succeed())
			// Start reconciliation.
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result.
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))
			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			Expect(reconciledCR.Status.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpDone))
			// Verify the ProvisioningRequest's status conditions
			conditions := reconciledCR.Status.Conditions
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:    string(utils.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionTrue,
				Reason:  string(utils.CRconditionReasons.Completed),
				Message: "The configuration is up to date",
			})
		})

		It("Keeps the ZTP status as ZTP Done if a policy becomes NonCompliant", func() {
			cr.Status.ClusterDetails.ZtpStatus = utils.ClusterZtpDone
			Expect(c.Status().Update(ctx, cr)).To(Succeed())
			policy.Status.ComplianceState = policiesv1.NonCompliant
			Expect(c.Status().Update(ctx, policy)).To(Succeed())
			// Start reconciliation.
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result.
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithLongInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())

			Expect(reconciledCR.Status.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpDone))
			conditions := reconciledCR.Status.Conditions
			// Verify the ProvisioningRequest's status conditions
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:    string(utils.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.InProgress),
				Message: "The configuration is still being applied",
			})
		})
	})
})

var _ = Describe("getCrClusterTemplateRef", func() {
	var (
		ctx          context.Context
		c            client.Client
		reconciler   *ProvisioningRequestReconciler
		task         *provisioningRequestReconcilerTask
		tName        = "clustertemplate-a"
		tVersion     = "v1.0.0"
		ctNamespace  = "clustertemplate-a-v4-16"
		ciDefaultsCm = "clusterinstance-defaults-v1"
		ptDefaultsCm = "policytemplate-defaults-v1"
		crName       = "cluster-1"
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Define the provisioning request.
		cr := &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    tName,
				TemplateVersion: tVersion,
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr}...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &provisioningRequestReconcilerTask{
			logger: reconciler.Logger,
			client: reconciler.Client,
			object: cr,
		}
	})

	It("returns error if the referred ClusterTemplate is missing", func() {
		schema := []byte(testFullTemplateSchema)
		// Define the cluster template.
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-cluster-template-name.v1.0.0",
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:       "other-cluster-template-name",
				Version:    "v1.0.0",
				TemplateID: "57b39bda-ac56-4143-9b10-d1a71517d04f",
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
				},
				TemplateParameterSchema: runtime.RawExtension{Raw: schema},
			},
		}

		Expect(c.Create(ctx, ct)).To(Succeed())

		retCt, err := task.getCrClusterTemplateRef(context.TODO())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			fmt.Sprintf(
				"a valid (%s) ClusterTemplate does not exist in any namespace",
				getClusterTemplateRefName(tName, tVersion))))
		Expect(retCt).To(Equal((*provisioningv1alpha1.ClusterTemplate)(nil)))
	})

	It("returns the referred ClusterTemplate if it exists", func() {
		// Define the cluster template.
		ctName := getClusterTemplateRefName(tName, tVersion)
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ctName,
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:       tName,
				Version:    tVersion,
				TemplateID: "57b39bda-ac56-4143-9b10-d1a71517d04f",
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
				},
				TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testFullTemplateSchema)},
			},
			Status: provisioningv1alpha1.ClusterTemplateStatus{
				Conditions: []metav1.Condition{
					{
						Reason: string(utils.CTconditionReasons.Completed),
						Type:   string(utils.CTconditionTypes.Validated),
						Status: metav1.ConditionTrue,
					},
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
