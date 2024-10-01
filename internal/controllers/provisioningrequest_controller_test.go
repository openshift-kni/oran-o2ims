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
	"github.com/openshift/assisted-service/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"
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
					utils.ClusterProvisioningTimeoutConfigKey: "60s",
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
		})

		It("Verify status conditions when configuration change causes ProvisioningRequest validation to fail but NodePool becomes provsioned ", func() {
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
		})

		It("Verify status conditions when configuration change causes ClusterInstance rendering to fail but NodePool becomes provsioned", func() {
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
				{Type: "ClusterInstanceValidated", Status: metav1.ConditionTrue},
				{Type: "RenderedTemplates", Status: metav1.ConditionTrue},
				{Type: "RenderedTemplatesValidated", Status: metav1.ConditionTrue},
				{Type: "RenderedTemplatesApplied", Status: metav1.ConditionTrue},
				{Type: "RenderedTemplatesApplied", Status: metav1.ConditionTrue}}
			Expect(c.Create(ctx, clusterInstance)).To(Succeed())
			// Create ManagedCluster
			managedCluster = &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: crName},
				Status: clusterv1.ManagedClusterStatus{
					Conditions: []metav1.Condition{{Type: clusterv1.ManagedClusterConditionAvailable, Status: metav1.ConditionFalse}},
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

		It("Verify status conditions when ClusterInstance provision is still in progress and ManagedCluster is not ready", func() {
			// Patch ClusterInstance provisioned status to InProgress
			crProvisionedCond := metav1.Condition{
				Type: "Provisioned", Status: metav1.ConditionFalse, Reason: "InProgress", Message: "Provisioning cluster",
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
		})

		It("Verify status conditions when ClusterInstance provision has timedout", func() {
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
		})

		It("Verify status conditions when ClusterInstance provision has completed, ManagedCluster becomes ready and configuration policy becomes compliant", func() {
			// Patch ClusterInstance provisioned status to Completed
			crProvisionedCond := metav1.Condition{
				Type: "Provisioned", Status: metav1.ConditionTrue, Reason: "Completed", Message: "Provisioning completed",
			}
			clusterInstance.Status.Conditions = append(clusterInstance.Status.Conditions, crProvisionedCond)
			Expect(c.Status().Update(ctx, clusterInstance)).To(Succeed())
			// Patch ManagedCluster to ready
			readyCond := meta.FindStatusCondition(
				managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
			readyCond.Status = metav1.ConditionTrue
			Expect(c.Status().Update(ctx, managedCluster)).To(Succeed())
			// Patch enforce policy to Compliant
			policy.Status.ComplianceState = "Compliant"
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
		})

		It("Verify status conditions when configuration change causes ProvisioningRequest validation to fail but ClusterInstance becomes provisioned", func() {
			// Initial reconciliation
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			// Patch ClusterInstance provisioned status to Completed.
			crProvisionedCond := metav1.Condition{
				Type: "Provisioned", Status: metav1.ConditionTrue, Reason: "Completed", Message: "Provisioning completed",
			}
			currentCI := &siteconfig.ClusterInstance{}
			Expect(c.Get(ctx, types.NamespacedName{Name: crName, Namespace: crName}, currentCI)).To(Succeed())
			currentCI.Status.Conditions = append(clusterInstance.Status.Conditions, crProvisionedCond)
			Expect(c.Status().Update(ctx, currentCI)).To(Succeed())

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
				Reason:  string(utils.CRconditionReasons.ClusterNotReady),
				Message: "The Cluster is not yet ready",
			})

			// Verify the start timestamp is not cleared even Cluster provision has completed
			Expect(reconciledCR.Status.ClusterDetails.ClusterProvisionStartedAt).ToNot(BeZero())
			// Verify the nonCompliantAt timestamp is not set, even though Non-compliant enforce policy exists
			// but Cluster is not ready
			Expect(reconciledCR.Status.ClusterDetails.NonCompliantAt).To(BeZero())
		})

		It("Verify status conditions when configuration change causes ProvisioningRequest validation to fail but ClusterProvision becomes timeout", func() {
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
		})

		It("Verify status conditions when configuration change causes ClusterInstance rendering to fail but configuration policy becomes compliant", func() {
			// Initial reconciliation
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			// Patch ClusterInstance provisioned status to Completed
			crProvisionedCond := metav1.Condition{
				Type: "Provisioned", Status: metav1.ConditionTrue, Reason: "Completed", Message: "Provisioning completed",
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
			// Patch enforce policy to Compliant
			policy.Status.ComplianceState = "Compliant"
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

var _ = Describe("handleRenderClusterInstance", func() {
	var (
		ctx          context.Context
		c            client.Client
		reconciler   *ProvisioningRequestReconciler
		task         *provisioningRequestReconcilerTask
		cr           *provisioningv1alpha1.ProvisioningRequest
		ciDefaultsCm = "clusterinstance-defaults-v1"
		tName        = "clustertemplate-a"
		tVersion     = "v1.0.0"
		ctNamespace  = "clustertemplate-a-v4-16"
		crName       = "cluster-1"
	)

	BeforeEach(func() {
		ctx = context.Background()
		// Define the provisioning request.
		cr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    tName,
				TemplateVersion: tVersion,
				TemplateParameters: runtime.RawExtension{
					Raw: []byte(testFullTemplateParameters),
				},
			},
		}

		// Define the cluster template.
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
				},
			},
		}
		// Configmap for ClusterInstance defaults
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ciDefaultsCm,
				Namespace: ctNamespace,
			},
			Data: map[string]string{
				utils.ClusterInstanceTemplateDefaultsConfigmapKey: `
clusterImageSetNameRef: "4.15"
pullSecretRef:
  name: "pull-secret"
holdInstallation: false
templateRefs:
  - name: "ai-cluster-templates-v1"
    namespace: "siteconfig-operator"
nodes:
- hostname: "node1"
  ironicInspect: ""
  templateRefs:
    - name: "ai-node-templates-v1"
      namespace: "siteconfig-operator"`,
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr, ct, cm}...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &provisioningRequestReconcilerTask{
			logger:       reconciler.Logger,
			client:       reconciler.Client,
			object:       cr,
			clusterInput: &clusterInput{},
			ctNamespace:  ctNamespace,
		}

		clusterInstanceInputParams, err := utils.ExtractMatchingInput(
			cr.Spec.TemplateParameters.Raw, utils.TemplateParamClusterInstance)
		Expect(err).ToNot(HaveOccurred())
		mergedClusterInstanceData, err := task.getMergedClusterInputData(
			ctx, ciDefaultsCm, clusterInstanceInputParams.(map[string]any), utils.TemplateParamClusterInstance)
		Expect(err).ToNot(HaveOccurred())
		task.clusterInput.clusterInstanceData = mergedClusterInstanceData
	})

	It("should successfully render and validate ClusterInstance with dry-run", func() {
		renderedClusterInstance, shouldUpgrade, err := task.handleRenderClusterInstance(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(renderedClusterInstance).ToNot(BeNil())
		Expect(shouldUpgrade).To(BeFalse())

		// Check if status condition was updated correctly
		cond := meta.FindStatusCondition(task.object.Status.Conditions,
			string(utils.PRconditionTypes.ClusterInstanceRendered))
		Expect(cond).ToNot(BeNil())
		verifyStatusCondition(*cond, metav1.Condition{
			Type:    string(utils.PRconditionTypes.ClusterInstanceRendered),
			Status:  metav1.ConditionTrue,
			Reason:  string(utils.CRconditionReasons.Completed),
			Message: "ClusterInstance rendered and passed dry-run validation",
		})
	})

	It("should fail to render ClusterInstance due to invalid input", func() {
		// Modify input data to be invalid
		task.clusterInput.clusterInstanceData["clusterName"] = ""
		_, _, err := task.handleRenderClusterInstance(ctx)
		Expect(err).To(HaveOccurred())

		// Check if status condition was updated correctly
		cond := meta.FindStatusCondition(task.object.Status.Conditions,
			string(utils.PRconditionTypes.ClusterInstanceRendered))
		Expect(cond).ToNot(BeNil())
		verifyStatusCondition(*cond, metav1.Condition{
			Type:    string(utils.PRconditionTypes.ClusterInstanceRendered),
			Status:  metav1.ConditionFalse,
			Reason:  string(utils.CRconditionReasons.Failed),
			Message: "spec.clusterName cannot be empty",
		})
	})

	It("should detect updates to immutable fields and fail rendering", func() {
		// Simulate that the ClusterInstance has been provisioned
		task.object.Status.Conditions = []metav1.Condition{
			{
				Type:   string(utils.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			},
		}

		oldSpec := make(map[string]any)
		newSpec := make(map[string]any)
		data, err := yaml.Marshal(task.clusterInput.clusterInstanceData)
		Expect(err).ToNot(HaveOccurred())
		Expect(yaml.Unmarshal(data, &oldSpec)).To(Succeed())
		Expect(yaml.Unmarshal(data, &newSpec)).To(Succeed())

		clusterInstanceObj := map[string]any{
			"Cluster": task.clusterInput.clusterInstanceData,
		}
		oldClusterInstance, err := utils.RenderTemplateForK8sCR(
			utils.ClusterInstanceTemplateName, utils.ClusterInstanceTemplatePath, clusterInstanceObj)
		Expect(err).ToNot(HaveOccurred())
		Expect(c.Create(ctx, oldClusterInstance)).To(Succeed())

		// Update the cluster data with modified field
		// Change an immutable field at the cluster-level
		newSpec["baseDomain"] = "newdomain.example.com"
		task.clusterInput.clusterInstanceData = newSpec

		_, _, err = task.handleRenderClusterInstance(ctx)
		Expect(err).To(HaveOccurred())

		// Note that the detected changed fields in this unittest include nodes.0.ironicInspect, baseDomain,
		// and holdInstallation, even though nodes.0.ironicInspect and holdInstallation were not actually changed.
		// This is due to the difference between the fakeclient and a real cluster. When applying a manifest
		// to a cluster, the API server preserves the full resource, including optional fields with empty values.
		// However, the fakeclient in unittests behaves differently, as it uses an in-memory store and
		// does not go through the API server. As a result, fields with empty values like false or "" are
		// stripped from the retrieved ClusterInstance CR (existing ClusterInstance) in the fakeclient.
		cond := meta.FindStatusCondition(task.object.Status.Conditions,
			string(utils.PRconditionTypes.ClusterInstanceRendered))
		Expect(cond).ToNot(BeNil())
		verifyStatusCondition(*cond, metav1.Condition{
			Type:    string(utils.PRconditionTypes.ClusterInstanceRendered),
			Status:  metav1.ConditionFalse,
			Reason:  string(utils.CRconditionReasons.Failed),
			Message: "Failed to render and validate ClusterInstance: detected changes in immutable fields",
		})
	})
})

var _ = Describe("createPolicyTemplateConfigMap", func() {
	var (
		ctx         context.Context
		c           client.Client
		reconciler  *ProvisioningRequestReconciler
		task        *provisioningRequestReconcilerTask
		tName       = "clustertemplate-a"
		tVersion    = "v1.0.0"
		ctNamespace = "clustertemplate-a-v4-16"
		crName      = "cluster-1"
	)

	BeforeEach(func() {
		ctx := context.Background()
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
			logger:       reconciler.Logger,
			client:       reconciler.Client,
			object:       cr,
			clusterInput: &clusterInput{},
			ctNamespace:  ctNamespace,
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
		reconciler      *ProvisioningRequestReconciler
		task            *provisioningRequestReconcilerTask
		clusterInstance *siteconfig.ClusterInstance
		ct              *provisioningv1alpha1.ClusterTemplate
		tName           = "clustertemplate-a"
		tVersion        = "v1.0.0"
		ctNamespace     = "clustertemplate-a-v4-16"
		hwTemplateCm    = "hwTemplate-v1"
		crName          = "cluster-1"
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Define the cluster instance.
		clusterInstance = &siteconfig.ClusterInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: ctNamespace,
			},
			Spec: siteconfig.ClusterInstanceSpec{
				Nodes: []siteconfig.NodeSpec{
					{
						Role: "master",
						NodeNetwork: &v1beta1.NMStateConfigSpec{
							Interfaces: []*v1beta1.Interface{
								{Name: "eno12399"},
								{Name: "eno1"},
							},
						},
					},
					{
						Role: "master",
						NodeNetwork: &v1beta1.NMStateConfigSpec{
							Interfaces: []*v1beta1.Interface{
								{Name: "eno12399"},
								{Name: "eno1"},
							},
						},
					},
					{
						Role: "worker",
						NodeNetwork: &v1beta1.NMStateConfigSpec{
							Interfaces: []*v1beta1.Interface{
								{Name: "eno1"},
							},
						},
					},
				},
			},
		}

		// Define the provisioning request.
		cr := &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    tName,
				TemplateVersion: tVersion,
				TemplateParameters: runtime.RawExtension{
					Raw: []byte(testFullTemplateParameters),
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
				Name:    tName,
				Version: tVersion,
				Templates: provisioningv1alpha1.Templates{
					HwTemplate: hwTemplateCm,
				},
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

	It("returns no error when renderHardwareTemplate succeeds", func() {
		// Ensure the ClusterTemplate is created
		Expect(c.Create(ctx, ct)).To(Succeed())

		// Define the hardware template config map
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hwTemplateCm,
				Namespace: utils.InventoryNamespace,
			},
			Data: map[string]string{
				"hwMgrId":                      utils.UnitTestHwmgrID,
				utils.HwTemplateBootIfaceLabel: "bootable-interface",
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
		Expect(nodePool.ObjectMeta.Namespace).To(Equal(utils.UnitTestHwmgrNamespace))
		Expect(nodePool.Annotations[utils.HwTemplateBootIfaceLabel]).To(Equal(cm.Data[utils.HwTemplateBootIfaceLabel]))

		Expect(nodePool.Spec.CloudID).To(Equal(clusterInstance.GetName()))
		Expect(nodePool.Spec.HwMgrId).To(Equal(cm.Data[utils.HwTemplatePluginMgr]))
		Expect(nodePool.Labels[provisioningRequestNameLabel]).To(Equal(task.object.Name))

		roleCounts := make(map[string]int)
		err = utils.ProcessClusterNodeGroups(clusterInstance, nodePool.Spec.NodeGroup, roleCounts)
		Expect(err).ToNot(HaveOccurred())
		masterNodeGroup, err := utils.FindNodeGroupByRole("master", nodePool.Spec.NodeGroup)
		Expect(err).ToNot(HaveOccurred())
		workerNodeGroup, err := utils.FindNodeGroupByRole("worker", nodePool.Spec.NodeGroup)
		Expect(err).ToNot(HaveOccurred())

		Expect(nodePool.Spec.NodeGroup).To(HaveLen(2))
		expectedNodeGroups := map[string]struct {
			size       int
			interfaces []string
		}{
			"master": {size: roleCounts["master"], interfaces: masterNodeGroup.Interfaces},
			"worker": {size: roleCounts["worker"], interfaces: workerNodeGroup.Interfaces},
		}

		for _, group := range nodePool.Spec.NodeGroup {
			expected, found := expectedNodeGroups[group.Name]
			Expect(found).To(BeTrue())
			Expect(group.Size).To(Equal(expected.size))
			Expect(group.Interfaces).To(ConsistOf(expected.interfaces))
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
		reconciler  *ProvisioningRequestReconciler
		task        *provisioningRequestReconcilerTask
		cr          *provisioningv1alpha1.ProvisioningRequest
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

		// Define the provisioning request.
		cr = &provisioningv1alpha1.ProvisioningRequest{
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
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &provisioningRequestReconcilerTask{
			logger: reconciler.Logger,
			client: reconciler.Client,
			object: cr,
			timeouts: &timeouts{
				hardwareProvisioning: 1 * time.Minute,
			},
		}
	})

	It("returns error when error fetching NodePool", func() {
		provisioned, timedOutOrFailed, err := task.checkNodePoolProvisionStatus(ctx, np)
		Expect(provisioned).To(Equal(false))
		Expect(timedOutOrFailed).To(Equal(false))
		Expect(err).To(HaveOccurred())
	})

	It("returns failed when NodePool provisioning failed", func() {
		provisionedCondition := metav1.Condition{
			Type:   "Provisioned",
			Status: metav1.ConditionFalse,
			Reason: string(hwv1alpha1.Failed),
		}
		np.Status.Conditions = append(np.Status.Conditions, provisionedCondition)
		Expect(c.Create(ctx, np)).To(Succeed())
		provisioned, timedOutOrFailed, err := task.checkNodePoolProvisionStatus(ctx, np)
		Expect(provisioned).To(Equal(false))
		Expect(timedOutOrFailed).To(Equal(true)) // It should be failed
		Expect(err).ToNot(HaveOccurred())
		condition := meta.FindStatusCondition(cr.Status.Conditions, string(utils.PRconditionTypes.HardwareProvisioned))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(string(hwv1alpha1.Failed)))
	})

	It("returns timeout when NodePool provisioning timed out", func() {
		provisionedCondition := metav1.Condition{
			Type:   "Provisioned",
			Status: metav1.ConditionFalse,
		}
		np.Status.Conditions = append(np.Status.Conditions, provisionedCondition)
		Expect(c.Create(ctx, np)).To(Succeed())

		// First call to checkNodePoolProvisionStatus (before timeout)
		provisioned, timedOutOrFailed, err := task.checkNodePoolProvisionStatus(ctx, np)
		Expect(provisioned).To(Equal(false))
		Expect(timedOutOrFailed).To(Equal(false))
		Expect(err).ToNot(HaveOccurred())

		// Simulate a timeout by moving the start time back
		adjustedTime := cr.Status.NodePoolRef.HardwareProvisioningCheckStart.Time.Add(-1 * time.Minute)
		cr.Status.NodePoolRef.HardwareProvisioningCheckStart = metav1.NewTime(adjustedTime)

		// Call checkNodePoolProvisionStatus again (after timeout)
		provisioned, timedOutOrFailed, err = task.checkNodePoolProvisionStatus(ctx, np)
		Expect(provisioned).To(Equal(false))
		Expect(timedOutOrFailed).To(Equal(true)) // Now it should time out
		Expect(err).ToNot(HaveOccurred())

		condition := meta.FindStatusCondition(cr.Status.Conditions, string(utils.PRconditionTypes.HardwareProvisioned))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(string(hwv1alpha1.TimedOut)))
	})

	It("returns false when NodePool is not provisioned", func() {
		provisionedCondition := metav1.Condition{
			Type:   "Provisioned",
			Status: metav1.ConditionFalse,
		}
		np.Status.Conditions = append(np.Status.Conditions, provisionedCondition)
		Expect(c.Create(ctx, np)).To(Succeed())

		provisioned, timedOutOrFailed, err := task.checkNodePoolProvisionStatus(ctx, np)
		Expect(provisioned).To(Equal(false))
		Expect(timedOutOrFailed).To(Equal(false))
		Expect(err).ToNot(HaveOccurred())
		condition := meta.FindStatusCondition(cr.Status.Conditions, string(utils.PRconditionTypes.HardwareProvisioned))
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
		provisioned, timedOutOrFailed, err := task.checkNodePoolProvisionStatus(ctx, np)
		Expect(provisioned).To(Equal(true))
		Expect(timedOutOrFailed).To(Equal(false))
		Expect(err).ToNot(HaveOccurred())
		condition := meta.FindStatusCondition(cr.Status.Conditions, string(utils.PRconditionTypes.HardwareProvisioned))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
	})
})

var _ = Describe("updateClusterInstance", func() {
	var (
		ctx         context.Context
		c           client.Client
		reconciler  *ProvisioningRequestReconciler
		task        *provisioningRequestReconcilerTask
		cr          *provisioningv1alpha1.ProvisioningRequest
		ci          *siteconfig.ClusterInstance
		np          *hwv1alpha1.NodePool
		crName      = "cluster-1"
		crNamespace = "clustertemplate-a-v4-16"
		mn          = "master-node"
		wn          = "worker-node"
		mhost       = "node1.test.com"
		whost       = "node2.test.com"
		poolns      = utils.UnitTestHwmgrNamespace
		mIfaces     = []*hwv1alpha1.Interface{
			{
				Name:       "eth0",
				Label:      "test",
				MACAddress: "00:00:00:01:20:30",
			},
			{
				Name:       "eth1",
				Label:      "",
				MACAddress: "66:77:88:99:CC:BB",
			},
		}
		wIfaces = []*hwv1alpha1.Interface{
			{
				Name:       "eno1",
				Label:      "test",
				MACAddress: "00:00:00:01:30:10",
			},
			{
				Name:       "eno2",
				Label:      "",
				MACAddress: "66:77:88:99:AA:BB",
			},
		}
		masterNode = createNode(mn, "idrac-virtualmedia+https://10.16.2.1/redfish/v1/Systems/System.Embedded.1",
			"site-1-master-bmc-secret", "master", poolns, crName, mIfaces)
		workerNode = createNode(wn, "idrac-virtualmedia+https://10.16.3.4/redfish/v1/Systems/System.Embedded.1",
			"site-1-worker-bmc-secret", "worker", poolns, crName, wIfaces)
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Define the cluster instance.
		ci = &siteconfig.ClusterInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: crNamespace,
			},
			Spec: siteconfig.ClusterInstanceSpec{
				Nodes: []siteconfig.NodeSpec{
					{
						Role: "master", HostName: mhost,
						NodeNetwork: &v1beta1.NMStateConfigSpec{
							Interfaces: []*v1beta1.Interface{
								{Name: "eth0"}, {Name: "eth1"},
							},
						},
					},
					{
						Role: "worker", HostName: whost,
						NodeNetwork: &v1beta1.NMStateConfigSpec{
							Interfaces: []*v1beta1.Interface{
								{Name: "eno1"}, {Name: "eno2"},
							},
						},
					},
				},
			},
		}

		// Define the provisioning request.
		cr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
		}

		// Define the node pool.
		np = &hwv1alpha1.NodePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: poolns,
				Annotations: map[string]string{
					utils.HwTemplateBootIfaceLabel: "test",
				},
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

	It("returns false when failing to get the Node object", func() {
		rt := task.updateClusterInstance(ctx, ci, np)
		Expect(rt).To(Equal(false))
	})

	It("returns true when updateClusterInstance succeeds", func() {
		nodes := []*hwv1alpha1.Node{masterNode, workerNode}
		secrets := createSecrets([]string{masterNode.Status.BMC.CredentialsName, workerNode.Status.BMC.CredentialsName}, poolns)

		createResources(c, ctx, nodes, secrets)

		rt := task.updateClusterInstance(ctx, ci, np)
		Expect(rt).To(Equal(true))

		masterBootMAC, err := utils.GetBootMacAddress(masterNode.Status.Interfaces, np)
		Expect(err).ToNot(HaveOccurred())
		workerBootMAC, err := utils.GetBootMacAddress(workerNode.Status.Interfaces, np)
		Expect(err).ToNot(HaveOccurred())

		// Define expected details
		expectedDetails := []expectedNodeDetails{
			{
				BMCAddress:         masterNode.Status.BMC.Address,
				BMCCredentialsName: masterNode.Status.BMC.CredentialsName,
				BootMACAddress:     masterBootMAC,
				Interfaces:         getInterfaceMap(masterNode.Status.Interfaces),
			},
			{
				BMCAddress:         workerNode.Status.BMC.Address,
				BMCCredentialsName: workerNode.Status.BMC.CredentialsName,
				BootMACAddress:     workerBootMAC,
				Interfaces:         getInterfaceMap(workerNode.Status.Interfaces),
			},
		}

		// Verify the bmc address, secret, boot mac address and interface mac addresses are set correctly in the cluster instance
		verifyClusterInstance(ci, expectedDetails)

		// Verify the host name is set in the node status
		verifyNodeStatus(c, ctx, nodes, mhost, whost)
	})
})

// Helper function to transform interfaces into the required map[string]interface{} format
func getInterfaceMap(interfaces []*hwv1alpha1.Interface) []map[string]interface{} {
	var ifaceList []map[string]interface{}
	for _, iface := range interfaces {
		ifaceList = append(ifaceList, map[string]interface{}{
			"Name":       iface.Name,
			"MACAddress": iface.MACAddress,
		})
	}
	return ifaceList
}

func createNode(name, bmcAddress, bmcSecret, groupName, namespace, npName string, interfaces []*hwv1alpha1.Interface) *hwv1alpha1.Node {
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
			Interfaces: interfaces,
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

func verifyClusterInstance(ci *siteconfig.ClusterInstance, expectedDetails []expectedNodeDetails) {
	for i, expected := range expectedDetails {
		Expect(ci.Spec.Nodes[i].BmcAddress).To(Equal(expected.BMCAddress))
		Expect(ci.Spec.Nodes[i].BmcCredentialsName.Name).To(Equal(expected.BMCCredentialsName))
		Expect(ci.Spec.Nodes[i].BootMACAddress).To(Equal(expected.BootMACAddress))
		// Verify Interface MAC Address
		for _, iface := range ci.Spec.Nodes[i].NodeNetwork.Interfaces {
			for _, expectedIface := range expected.Interfaces {
				// Compare the interface name and MAC address
				if iface.Name == expectedIface["Name"] {
					Expect(iface.MacAddress).To(Equal(expectedIface["MACAddress"]), "MAC Address mismatch for interface")
				}
			}
		}
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
		c            client.Client
		ctx          context.Context
		CRReconciler *ProvisioningRequestReconciler
		CRTask       *provisioningRequestReconcilerTask
		CTReconciler *ClusterTemplateReconciler
		tName        = "clustertemplate-a"
		tVersion     = "v1.0.0"
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
			&provisioningv1alpha1.ClusterTemplate{
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
			},
			// ConfigMaps.
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ciDefaultsCm,
					Namespace: ctNamespace,
				},
				Data: map[string]string{
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
					Namespace: utils.InventoryNamespace,
				},
				Data: map[string]string{
					"hwMgrId":                      utils.UnitTestHwmgrID,
					utils.HwTemplateBootIfaceLabel: "bootable-interface",
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
					Name:      "pull-secret",
					Namespace: ctNamespace,
				},
			},
			// Provisioning Requests.
			&provisioningv1alpha1.ProvisioningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "cluster-1",
					Finalizers: []string{provisioningRequestFinalizer},
				},
				Spec: provisioningv1alpha1.ProvisioningRequestSpec{
					TemplateName:    tName,
					TemplateVersion: tVersion,
					TemplateParameters: runtime.RawExtension{
						Raw: []byte(testFullTemplateParameters),
					},
				},
				Status: provisioningv1alpha1.ProvisioningRequestStatus{
					// Fake the hw provision status
					Conditions: []metav1.Condition{
						{
							Type:   string(utils.PRconditionTypes.HardwareProvisioned),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			// Managed clusters
			&clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
				},
				Spec: clusterv1.ManagedClusterSpec{
					HubAcceptsClient: true,
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
				Name:      getClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
		}

		_, err := CTReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())

		CRReconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}

		// Update the managedCluster cluster-1 to be available, joined and accepted.
		managedCluster1 := &clusterv1.ManagedCluster{}
		managedClusterExists, err := utils.DoesK8SResourceExist(
			ctx, c, "cluster-1", "", managedCluster1)
		Expect(err).ToNot(HaveOccurred())
		Expect(managedClusterExists).To(BeTrue())
		utils.SetStatusCondition(&managedCluster1.Status.Conditions,
			utils.ConditionType(clusterv1.ManagedClusterConditionAvailable),
			"ManagedClusterAvailable",
			metav1.ConditionTrue,
			"Managed cluster is available",
		)
		utils.SetStatusCondition(&managedCluster1.Status.Conditions,
			utils.ConditionType(clusterv1.ManagedClusterConditionHubAccepted),
			"HubClusterAdminAccepted",
			metav1.ConditionTrue,
			"Accepted by hub cluster admin",
		)
		utils.SetStatusCondition(&managedCluster1.Status.Conditions,
			utils.ConditionType(clusterv1.ManagedClusterConditionJoined),
			"ManagedClusterJoined",
			metav1.ConditionTrue,
			"Managed cluster joined",
		)
		err = CRReconciler.Client.Status().Update(context.TODO(), managedCluster1)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Does not handle the policy configuration without the cluster provisioning having started", func() {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())

		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{
				Name: "cluster-1",
			},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger: CRReconciler.Logger,
			client: CRReconciler.Client,
			object: provisioningRequest, // cluster-1 request
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}

		// Create the policies, all Compliant, one in inform and one in enforce.
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
					RemediationAction: "enforce",
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
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
				},
			},
		}
		for _, newPolicy := range newPolicies {
			Expect(c.Create(ctx, newPolicy)).To(Succeed())
		}

		// Call the Reconciliation function.
		result, err = CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt).To(BeZero())
		Expect(CRTask.object.Status.Policies).To(BeEmpty())

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).To(BeNil())

		// Add the ClusterProvisioned condition.
		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{Name: "cluster-1"},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger: CRReconciler.Logger,
			client: CRReconciler.Client,
			object: provisioningRequest, // cluster-1 request
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}

		utils.SetStatusCondition(&CRTask.object.Status.Conditions,
			utils.PRconditionTypes.ClusterProvisioned,
			utils.CRconditionReasons.InProgress,
			metav1.ConditionFalse,
			"",
		)
		CRTask.object.Status.ClusterDetails.ClusterProvisionStartedAt = metav1.Now()
		Expect(c.Status().Update(ctx, CRTask.object)).To(Succeed())

		// Call the Reconciliation function.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{Name: "cluster-1"},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger:       CRReconciler.Logger,
			client:       CRReconciler.Client,
			object:       provisioningRequest, // cluster-1 request
			clusterInput: &clusterInput{},
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}
		result, err = CRTask.run(ctx)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())
		Expect(result.RequeueAfter).To(Equal(5 * time.Minute)) // Long interval
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt).ToNot(BeZero())
		Expect(CRTask.object.Status.Policies).ToNot(BeEmpty())
	})

	It("Moves from TimedOut to Completed if all the policies are compliant", func() {
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: "cluster-1",
			},
		}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())

		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{Name: "cluster-1"},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger: CRReconciler.Logger,
			client: CRReconciler.Client,
			object: provisioningRequest, // cluster-1 request
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}

		// Update the ProvisioningRequest ConfigurationApplied condition to TimedOut.
		utils.SetStatusCondition(&CRTask.object.Status.Conditions,
			utils.PRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.TimedOut,
			metav1.ConditionFalse,
			"The configuration is still being applied, but it timed out",
		)
		Expect(c.Status().Update(ctx, CRTask.object)).To(Succeed())

		// Create the policies, all Compliant, one in inform and one in enforce.
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
					RemediationAction: "enforce",
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
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
				},
			},
		}
		for _, newPolicy := range newPolicies {
			Expect(c.Create(ctx, newPolicy)).To(Succeed())
		}

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse()) // there are no NonCompliant policies
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt).To(BeZero())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-sriov-configuration-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "inform",
				},
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "enforce",
				},
			},
		))

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionTrue))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.Completed)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is up to date"))
	})

	It("Clears the NonCompliantAt timestamp and timeout when policies are switched to inform", func() {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())

		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{Name: "cluster-1"},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger: CRReconciler.Logger,
			client: CRReconciler.Client,
			object: provisioningRequest, // cluster-1 request
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: 60 * time.Second,
			},
		}

		// Create inform policies, one Compliant and one NonCompliant.
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
					RemediationAction: "enforce",
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
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
				},
			},
		}
		for _, newPolicy := range newPolicies {
			Expect(c.Create(ctx, newPolicy)).To(Succeed())
		}

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue()) // there are NonCompliant policies in enforce
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt).ToNot(BeZero())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-sriov-configuration-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "inform",
				},
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "enforce",
				},
			},
		))

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.InProgress)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is still being applied"))

		// Take 2 minutes to the NonCompliantAt timestamp to mock timeout.
		CRTask.object.Status.ClusterDetails.NonCompliantAt.Time =
			CRTask.object.Status.ClusterDetails.NonCompliantAt.Add(-2 * time.Minute)
		Expect(c.Status().Update(ctx, CRTask.object)).To(Succeed())

		// Call the handleClusterPolicyConfiguration function.
		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue()) // there are NonCompliant policies in enforce
		Expect(err).ToNot(HaveOccurred())

		// Check the status conditions.
		conditions = CRTask.object.Status.Conditions
		configAppliedCond = meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.TimedOut)))
		Expect(configAppliedCond.Message).To(
			Equal("The configuration is still being applied, but it timed out"))

		// Check that the NonCompliantAt and timeout are cleared if the policies are in inform.
		// Inform the NonCompliant policy.
		policy := &policiesv1.Policy{}
		policyExists, err := utils.DoesK8SResourceExist(
			ctx, c, "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy", "cluster-1", policy)
		Expect(err).ToNot(HaveOccurred())
		Expect(policyExists).To(BeTrue())
		policy.Spec.RemediationAction = policiesv1.Inform
		Expect(c.Update(ctx, policy)).To(Succeed())
		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse()) // all policies are in inform
		Expect(err).ToNot(HaveOccurred())
		// Check that the NonCompliantAt is zero.
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt).To(BeZero())

		// Check the status conditions.
		conditions = CRTask.object.Status.Conditions
		configAppliedCond = meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.OutOfDate)))
		Expect(configAppliedCond.Message).To(
			Equal("The configuration is out of date"))
	})

	It("It transitions from InProgress to ClusterNotReady to InProgress", func() {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())

		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{Name: "cluster-1"},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger: CRReconciler.Logger,
			client: CRReconciler.Client,
			object: provisioningRequest, // cluster-1 request
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}
		// Create policies.
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
					RemediationAction: "enforce",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "NonCompliant",
				},
			},
		}
		for _, newPolicy := range newPolicies {
			Expect(c.Create(ctx, newPolicy)).To(Succeed())
		}

		// Step 1: Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue()) // we have non compliant enforce policies
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "enforce",
				},
			},
		))
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt).ToNot(BeZero())
		initialNonCompliantAt := CRTask.object.Status.ClusterDetails.NonCompliantAt

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.InProgress)))

		// Step 2: Update the managed cluster to make it not ready.
		managedCluster1 := &clusterv1.ManagedCluster{}
		managedClusterExists, err := utils.DoesK8SResourceExist(ctx, c, "cluster-1", "", managedCluster1)
		Expect(err).ToNot(HaveOccurred())
		Expect(managedClusterExists).To(BeTrue())
		utils.SetStatusCondition(&managedCluster1.Status.Conditions,
			utils.ConditionType(clusterv1.ManagedClusterConditionAvailable),
			"ManagedClusterAvailable",
			metav1.ConditionFalse,
			"Managed cluster is not available",
		)
		err = CRReconciler.Client.Status().Update(context.TODO(), managedCluster1)
		Expect(err).ToNot(HaveOccurred())

		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "enforce",
				},
			},
		))
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt).ToNot(BeZero())
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt).To(Equal(initialNonCompliantAt))

		// Check the status conditions.
		conditions = CRTask.object.Status.Conditions
		configAppliedCond = meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.ClusterNotReady)))

		// Step 3: Update the managed cluster to make it ready again.
		utils.SetStatusCondition(&managedCluster1.Status.Conditions,
			utils.ConditionType(clusterv1.ManagedClusterConditionAvailable),
			"ManagedClusterAvailable",
			metav1.ConditionTrue,
			"Managed cluster is available",
		)
		err = CRReconciler.Client.Status().Update(context.TODO(), managedCluster1)
		Expect(err).ToNot(HaveOccurred())

		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "enforce",
				},
			},
		))
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt).ToNot(BeZero())
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt).To(Equal(initialNonCompliantAt))

		// Check the status conditions.
		conditions = CRTask.object.Status.Conditions
		configAppliedCond = meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.InProgress)))
	})

	It("It sets ClusterNotReady if the cluster is unstable/not ready", func() {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())

		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{Name: "cluster-1"},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger: CRReconciler.Logger,
			client: CRReconciler.Client,
			object: provisioningRequest, // cluster-1 request
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}
		// Update the managed cluster to make it not ready.
		managedCluster1 := &clusterv1.ManagedCluster{}
		managedClusterExists, err := utils.DoesK8SResourceExist(
			ctx, c, "cluster-1", "", managedCluster1)
		Expect(err).ToNot(HaveOccurred())
		Expect(managedClusterExists).To(BeTrue())
		utils.SetStatusCondition(&managedCluster1.Status.Conditions,
			utils.ConditionType(clusterv1.ManagedClusterConditionAvailable),
			"ManagedClusterAvailable",
			metav1.ConditionFalse,
			"Managed cluster is not available",
		)
		err = CRReconciler.Client.Status().Update(context.TODO(), managedCluster1)
		Expect(err).ToNot(HaveOccurred())

		// Create policies.
		// The cluster is not ready, so there is no compliance status
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
			},
		}
		for _, newPolicy := range newPolicies {
			Expect(c.Create(ctx, newPolicy)).To(Succeed())
		}

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "inform",
				},
			},
		))
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt).To(BeZero())

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.ClusterNotReady)))
	})

	It("Sets the NonCompliantAt timestamp and times out", func() {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())

		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{
				Name: "cluster-1",
			},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger:      CRReconciler.Logger,
			client:      CRReconciler.Client,
			object:      provisioningRequest, // cluster-1 request
			ctNamespace: ctNamespace,
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: 1 * time.Minute,
			},
		}

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(BeEmpty())
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt).To(BeZero())

		// Create inform policies, one Compliant and one NonCompliant.
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
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
				},
			},
		}
		for _, newPolicy := range newPolicies {
			Expect(c.Create(ctx, newPolicy)).To(Succeed())
		}

		// Call the handleClusterPolicyConfiguration function.
		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
		// NonCompliantAt should still be zero since we don't consider inform policies in the timeout.
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt).To(BeZero())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-sriov-configuration-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "inform",
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
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.OutOfDate)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is out of date"))

		// Enforce the NonCompliant policy.
		policy := &policiesv1.Policy{}
		policyExists, err := utils.DoesK8SResourceExist(
			ctx, c, "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy", "cluster-1", policy)
		Expect(err).ToNot(HaveOccurred())
		Expect(policyExists).To(BeTrue())
		policy.Spec.RemediationAction = policiesv1.Enforce
		Expect(c.Update(ctx, policy)).To(Succeed())

		policyExists, err = utils.DoesK8SResourceExist(
			ctx, c, "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy", "cluster-1", policy)
		Expect(err).ToNot(HaveOccurred())
		Expect(policyExists).To(BeTrue())

		// Call the handleClusterPolicyConfiguration function.
		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue()) // there are NonCompliant policies in enforce
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt).ToNot(BeZero())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-sriov-configuration-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "inform",
				},
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "Enforce",
				},
			},
		))

		// Check the status conditions.
		conditions = CRTask.object.Status.Conditions
		configAppliedCond = meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.InProgress)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is still being applied"))

		// Take 2 minutes to the NonCompliantAt timestamp to mock timeout.
		CRTask.object.Status.ClusterDetails.NonCompliantAt.Time =
			CRTask.object.Status.ClusterDetails.NonCompliantAt.Add(-2 * time.Minute)
		Expect(c.Status().Update(ctx, CRTask.object)).To(Succeed())

		// Call the handleClusterPolicyConfiguration function.
		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue()) // there are NonCompliant policies in enforce
		Expect(err).ToNot(HaveOccurred())

		// Check the status conditions.
		conditions = CRTask.object.Status.Conditions
		configAppliedCond = meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.TimedOut)))
		Expect(configAppliedCond.Message).To(
			Equal("The configuration is still being applied, but it timed out"))

		// Check that another handleClusterPolicyConfiguration call doesn't change the status if
		// the policies are the same.
		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue()) // there are NonCompliant policies in enforce
		Expect(err).ToNot(HaveOccurred())

		// Check the status conditions.
		conditions = CRTask.object.Status.Conditions
		configAppliedCond = meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.TimedOut)))
		Expect(configAppliedCond.Message).To(
			Equal("The configuration is still being applied, but it timed out"))
	})

	It("Updates ProvisioningRequest ConfigurationApplied condition to Missing if there are no policies", func() {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())

		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{
				Name: "cluster-1",
			},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger:      CRReconciler.Logger,
			client:      CRReconciler.Client,
			object:      provisioningRequest, // cluster-1 request
			ctNamespace: ctNamespace,
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(BeEmpty())
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt).To(BeZero())

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.Missing)))
	})

	It("It handles updated/deleted policies for matched clusters", func() {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())
		// Expect the ClusterInstance and its namespace to have been created.
		clusterInstanceNs := &corev1.Namespace{}
		err = CRReconciler.Client.Get(
			context.TODO(),
			client.ObjectKey{Name: "cluster-1"},
			clusterInstanceNs,
		)
		Expect(err).ToNot(HaveOccurred())
		clusterInstance := &siteconfig.ClusterInstance{}
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{Name: "cluster-1", Namespace: "cluster-1"},
			clusterInstance)
		Expect(err).ToNot(HaveOccurred())

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
				Name: "ProvisioningRequestsQueue",
			})
		CRReconciler.handlePolicyEventUpdate(ctx, *updateEvent, queue)
		Expect(queue.Len()).To(Equal(1))

		// Get the first request from the queue.
		item, shutdown := queue.Get()
		Expect(shutdown).To(BeFalse())

		Expect(item).To(Equal(reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}))

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
				Name: "ProvisioningRequestsQueue",
			})
		CRReconciler.handlePolicyEventDelete(ctx, *deleteEvent, queue)
		Expect(queue.Len()).To(Equal(1))

		// Get the first request from the queue.
		item, shutdown = queue.Get()
		Expect(shutdown).To(BeFalse())

		Expect(item).To(Equal(reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}))
	})

	It("Updates ProvisioningRequest ConfigurationApplied condition to OutOfDate when the cluster is "+
		"NonCompliant with at least one matched policies and the policy is not in enforce", func() {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
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
		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{
				Name: "cluster-1",
			},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger:       CRReconciler.Logger,
			client:       CRReconciler.Client,
			object:       provisioningRequest, // cluster-1 request
			clusterInput: &clusterInput{},
			ctNamespace:  ctNamespace,
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
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
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.OutOfDate)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is out of date"))
	})

	It("Updates ProvisioningRequest ConfigurationApplied condition to Completed when the cluster is "+
		"Compliant with all the matched policies", func() {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
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
		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{
				Name: "cluster-1",
			},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger:       CRReconciler.Logger,
			client:       CRReconciler.Client,
			object:       provisioningRequest, // cluster-1 request
			clusterInput: &clusterInput{},
			ctNamespace:  ctNamespace,
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
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
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionTrue))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.Completed)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is up to date"))
	})

	It("Updates ProvisioningRequest ConfigurationApplied condition to InProgress when the cluster is "+
		"NonCompliant with at least one enforce policy", func() {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
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
		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{
				Name: "cluster-1",
			},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger:      CRReconciler.Logger,
			client:      CRReconciler.Client,
			object:      provisioningRequest, // cluster-1 request
			ctNamespace: ctNamespace,
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
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
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.InProgress)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is still being applied"))
	})

	It("Updates ProvisioningRequest ConfigurationApplied condition to InProgress when the cluster is "+
		"Pending with at least one enforce policy", func() {
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: "cluster-1",
			},
		}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
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
					ComplianceState: "Pending",
				},
			},
		}
		// Create all the ACM policies.
		for _, newPolicy := range newPolicies {
			Expect(c.Create(ctx, newPolicy)).To(Succeed())
		}
		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		namespacedName := types.NamespacedName{Name: "cluster-1"}
		err = c.Get(context.TODO(), namespacedName, provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger:      CRReconciler.Logger,
			client:      CRReconciler.Client,
			object:      provisioningRequest, // cluster-1 request
			ctNamespace: ctNamespace,
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "Pending",
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
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.InProgress)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is still being applied"))
	})
})

var _ = Describe("hasPolicyConfigurationTimedOut", func() {
	var (
		ctx          context.Context
		c            client.Client
		CRReconciler *ProvisioningRequestReconciler
		CRTask       *provisioningRequestReconcilerTask
		CTReconciler *ClusterTemplateReconciler
		tName        = "clustertemplate-a"
		tVersion     = "v1.0.0"
		ctNamespace  = "clustertemplate-a-v4-16"
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
			// Provisioning Request.
			&provisioningv1alpha1.ProvisioningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "cluster-1",
					Finalizers: []string{provisioningRequestFinalizer},
				},
				Spec: provisioningv1alpha1.ProvisioningRequestSpec{
					TemplateName:    tName,
					TemplateVersion: tVersion,
					TemplateParameters: runtime.RawExtension{
						Raw: []byte(testFullTemplateParameters),
					},
				},
				Status: provisioningv1alpha1.ProvisioningRequestStatus{
					ClusterDetails: &provisioningv1alpha1.ClusterDetails{},
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
				Name:      getClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
		}

		_, err := CTReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())

		CRReconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}

		CRTask = &provisioningRequestReconcilerTask{
			logger: CRReconciler.Logger,
			client: CRReconciler.Client,
			object: crs[1].(*provisioningv1alpha1.ProvisioningRequest), // cluster-1 request
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: 1 * time.Minute,
			},
		}
	})

	It("Returns false if the status is unexpected and NonCompliantAt is not set", func() {
		// Set the status to InProgress.
		utils.SetStatusCondition(&CRTask.object.Status.Conditions,
			utils.PRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.Unknown,
			metav1.ConditionFalse,
			"",
		)
		// Start from empty NonCompliantAt.
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt.Time).To(BeZero())
		policyTimedOut := CRTask.hasPolicyConfigurationTimedOut(ctx)
		// Check that NonCompliantAt was set and that the return is false.
		Expect(policyTimedOut).To(BeFalse())
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt.Time).To(BeZero())
	})

	It("Returns false if the status is Completed and sets NonCompliantAt", func() {
		// Set the status to InProgress.
		utils.SetStatusCondition(&CRTask.object.Status.Conditions,
			utils.PRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"",
		)
		// Start from empty NonCompliantAt.
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt.Time).To(BeZero())
		policyTimedOut := CRTask.hasPolicyConfigurationTimedOut(ctx)
		// Check that NonCompliantAt was set and that the return is false.
		Expect(policyTimedOut).To(BeFalse())
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt.Time).ToNot(BeZero())
	})

	It("Returns false if the status is OutOfDate and sets NonCompliantAt", func() {
		// Set the status to InProgress.
		utils.SetStatusCondition(&CRTask.object.Status.Conditions,
			utils.PRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.OutOfDate,
			metav1.ConditionFalse,
			"",
		)
		// Start from empty NonCompliantAt.
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt.Time).To(BeZero())
		policyTimedOut := CRTask.hasPolicyConfigurationTimedOut(ctx)
		// Check that NonCompliantAt was set and that the return is false.
		Expect(policyTimedOut).To(BeFalse())
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt.Time).ToNot(BeZero())
	})

	It("Returns false if the status is Missing and sets NonCompliantAt", func() {
		// Set the status to InProgress.
		utils.SetStatusCondition(&CRTask.object.Status.Conditions,
			utils.PRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.Missing,
			metav1.ConditionFalse,
			"",
		)
		// Start from empty NonCompliantAt.
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt.Time).To(BeZero())
		policyTimedOut := CRTask.hasPolicyConfigurationTimedOut(ctx)
		// Check that NonCompliantAt was set and that the return is false.
		Expect(policyTimedOut).To(BeFalse())
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt.Time).ToNot(BeZero())
	})

	It("Returns true if the status is InProgress and the timeout has passed", func() {
		// Set the status to InProgress.
		utils.SetStatusCondition(&CRTask.object.Status.Conditions,
			utils.PRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.InProgress,
			metav1.ConditionFalse,
			"",
		)
		// Set NonCompliantAt.
		nonCompliantAt := metav1.Now().Add(-2 * time.Minute)
		CRTask.object.Status.ClusterDetails.NonCompliantAt.Time = nonCompliantAt
		policyTimedOut := CRTask.hasPolicyConfigurationTimedOut(ctx)
		// Check that NonCompliantAt wasn't changed and that the return is true.
		Expect(policyTimedOut).To(BeTrue())
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt.Time).To(Equal(nonCompliantAt))
	})

	It("Sets NonCompliantAt if there is no ConfigurationApplied condition", func() {
		policyTimedOut := CRTask.hasPolicyConfigurationTimedOut(ctx)
		Expect(policyTimedOut).To(BeFalse())
		Expect(CRTask.object.Status.ClusterDetails.NonCompliantAt).ToNot(BeZero())
	})
})
