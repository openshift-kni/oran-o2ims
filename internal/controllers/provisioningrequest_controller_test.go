package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/api/v1beta1"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"

	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
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

const (
	masterNodeName = "master-node"
	bmcSecretName  = "bmc-secret"
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
	expectedPhase provisioningv1alpha1.ProvisioningPhase, expectedDetail string,
	expectedResources *provisioningv1alpha1.ProvisionedResources) {

	Expect(provStatus.ProvisioningPhase).To(Equal(expectedPhase))
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

func createNodeResources(ctx context.Context, c client.Client, npName string) {
	node := createNode(masterNodeName, "idrac-virtualmedia+https://10.16.2.1/redfish/v1/Systems/System.Embedded.1", "bmc-secret", "controller", utils.UnitTestHwmgrNamespace, npName, nil)
	secrets := createSecrets([]string{bmcSecretName}, utils.UnitTestHwmgrNamespace)
	createResources(ctx, c, []*hwv1alpha1.Node{node}, secrets)
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
		hwTemplate   = "hwTemplate-v1"
		crName       = "cluster-1"
	)

	BeforeEach(func() {
		ctx = context.Background()

		crs := []client.Object{
			// HW plugin test namespace
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: utils.UnitTestHwmgrNamespace,
				},
			},
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
      nodeNetwork:
        interfaces:
        - name: eno1
          label: bootable-interface
        - name: eth0
          label: base-interface
        - name: eth1
          label: data-interface
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
			// hardware template
			&hwv1alpha1.HardwareTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      hwTemplate,
					Namespace: utils.InventoryNamespace,
				},
				Spec: hwv1alpha1.HardwareTemplateSpec{
					HwMgrId:                     utils.UnitTestHwmgrID,
					BootInterfaceLabel:          "bootable-interface",
					HardwareProvisioningTimeout: "1m",
					NodePoolData: []hwv1alpha1.NodePoolData{
						{
							Name:           "controller",
							Role:           "master",
							ResourcePoolId: "xyz",
							HwProfile:      "profile-spr-single-processor-64G",
						},
						{
							Name:           "worker",
							Role:           "worker",
							ResourcePoolId: "xyz",
							HwProfile:      "profile-spr-dual-processor-128G",
						},
					},
					Extensions: map[string]string{
						"resourceTypeId": "ResourceGroup~2.1.1",
					},
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
					HwTemplate:              hwTemplate,
				},
				TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testFullTemplateSchema)},
			},
			Status: provisioningv1alpha1.ClusterTemplateStatus{
				Conditions: []metav1.Condition{
					{
						Type:   string(provisioningv1alpha1.CTconditionTypes.Validated),
						Reason: string(provisioningv1alpha1.CTconditionReasons.Completed),
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
				ct.Status.Conditions, string(provisioningv1alpha1.CTconditionTypes.Validated))
			ctValidatedCond.Status = metav1.ConditionFalse
			ctValidatedCond.Reason = string(provisioningv1alpha1.CTconditionReasons.Failed)
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
				Type:   string(provisioningv1alpha1.PRconditionTypes.Validated),
				Status: metav1.ConditionFalse,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Failed),
				Message: fmt.Sprintf(
					"Failed to validate the ProvisioningRequest: failed to get the ClusterTemplate for "+
						"ProvisioningRequest cluster-1: a valid ClusterTemplate (%s) does not exist in any namespace",
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
				Type:   string(provisioningv1alpha1.PRconditionTypes.Validated),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[1], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Failed),
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
				Type:   string(provisioningv1alpha1.PRconditionTypes.Validated),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[1], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[2], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ClusterResourcesCreated),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Failed),
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
			Expect(result).To(Equal(requeueWithMediumInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify NodePool was created
			nodePool := &hwv1alpha1.NodePool{}
			Expect(c.Get(ctx, types.NamespacedName{
				Name: crName, Namespace: utils.UnitTestHwmgrNamespace}, nodePool)).To(Succeed())

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(5))
			verifyStatusCondition(conditions[0], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.Validated),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[1], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[2], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterResourcesCreated),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[3], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareTemplateRendered),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionUnknown,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Unknown),
			})
			// Verify the start timestamp has been set for HardwareProvisioning
			Expect(reconciledCR.Status.Extensions.NodePoolRef.HardwareProvisioningCheckStart).ToNot(BeZero())
			// Verify provisioningState is progressing when nodePool has been created
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Hardware provisioning is in progress", nil)
		})
	})

	Context("When NodePool has been created", func() {
		var nodePool *hwv1alpha1.NodePool

		BeforeEach(func() {
			// Create NodePool resource
			nodePool = &hwv1alpha1.NodePool{}
			nodePool.SetName(crName)
			nodePool.SetNamespace(utils.UnitTestHwmgrNamespace)
			nodePool.Spec.HwMgrId = "hwmgr"
			nodePool.Spec.NodeGroup = []hwv1alpha1.NodeGroup{
				{NodePoolData: hwv1alpha1.NodePoolData{
					Name: "controller", HwProfile: "profile-spr-single-processor-64G"},
					Size: 1},
				{NodePoolData: hwv1alpha1.NodePoolData{
					Name: "worker", HwProfile: "profile-spr-dual-processor-128G"}, Size: 0},
			}
			nodePool.Status.Conditions = []metav1.Condition{
				{Type: string(hwv1alpha1.Provisioned), Status: metav1.ConditionFalse, Reason: string(hwv1alpha1.InProgress)},
			}
			nodePool.Status.Properties = hwv1alpha1.Properties{NodeNames: []string{masterNodeName}}
			nodePool.Annotations = map[string]string{"bootInterfaceLabel": "bootable-interface"}
			Expect(c.Create(ctx, nodePool)).To(Succeed())
			createNodeResources(ctx, c, nodePool.Name)
		})

		It("Verify ClusterInstance should not be created when NodePool provision is in-progress", func() {
			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithMediumInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify no ClusterInstance was created
			clusterInstance := &siteconfig.ClusterInstance{}
			Expect(c.Get(ctx, types.NamespacedName{
				Name: crName, Namespace: crName}, clusterInstance)).To(HaveOccurred())

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(5))
			verifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(provisioningv1alpha1.CRconditionReasons.InProgress),
			})
			// Verify the start timestamp has been set for HardwareProvisioning
			Expect(reconciledCR.Status.Extensions.NodePoolRef.HardwareProvisioningCheckStart).ToNot(BeZero())
			// Verify provisioningState is progressing when nodePool is in-progress
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Hardware provisioning is in progress", nil)
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
			Expect(len(conditions)).To(Equal(7))
			verifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[5], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareNodeConfigApplied),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[6], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed),
				Status: metav1.ConditionUnknown,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Unknown),
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
				Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Failed),
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
			Expect(cr.Status.Extensions.NodePoolRef.HardwareProvisioningCheckStart).ToNot(BeZero())

			// Patch HardwareProvisioningCheckStart timestamp to mock timeout
			cr.Status.Extensions.NodePoolRef.HardwareProvisioningCheckStart.Time = metav1.Now().Add(-2 * time.Minute)
			Expect(c.Status().Update(ctx, cr)).To(Succeed())

			// Start reconciliation again
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue())) // stop reconciliation on HwProvision timeout

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(5))
			verifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(provisioningv1alpha1.CRconditionReasons.TimedOut),
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
			Expect(c.Get(ctx, types.NamespacedName{Name: crName, Namespace: utils.UnitTestHwmgrNamespace}, currentNp)).To(Succeed())
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

			// Verify that the validated condition fails but hw provisioned condition
			// has changed to Completed
			Expect(len(conditions)).To(Equal(5))
			verifyStatusCondition(conditions[0], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.Validated),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Failed),
				Message: "nodes.0: hostName is required",
			})
			verifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			// Verify the provisioningState moves to failed
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed,
				"Failed to validate the ProvisioningRequest: failed to validate ClusterInstance input", nil)
		})

		It("Verify status when configuration change causes ClusterInstance rendering to fail but NodePool becomes provisioned", func() {
			// Initial reconciliation
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			// Patch NodePool provision status to Completed
			currentNp := &hwv1alpha1.NodePool{}
			Expect(c.Get(ctx, types.NamespacedName{Name: crName, Namespace: utils.UnitTestHwmgrNamespace}, currentNp)).To(Succeed())
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

			// Verify that the ClusterInstanceRendered condition fails but hw provisioned condition
			// has changed to Completed
			Expect(len(conditions)).To(Equal(5))
			verifyStatusCondition(conditions[1], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Failed),
				Message: "spec.nodes[0].templateRefs must be provided",
			})
			verifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			// Verify the provisioningState moves to failed
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Failed to render and validate ClusterInstance", nil)
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
			nodePool.SetNamespace(utils.UnitTestHwmgrNamespace)
			nodePool.Spec.HwMgrId = "hwmgr"
			nodePool.Spec.NodeGroup = []hwv1alpha1.NodeGroup{
				{NodePoolData: hwv1alpha1.NodePoolData{
					Name: "controller", HwProfile: "profile-spr-single-processor-64G"},
					Size: 1},
				{NodePoolData: hwv1alpha1.NodePoolData{
					Name: "worker", HwProfile: "profile-spr-dual-processor-128G"},
					Size: 0},
			}
			nodePool.Status.Conditions = []metav1.Condition{
				{Type: string(hwv1alpha1.Provisioned), Status: metav1.ConditionTrue, Reason: string(hwv1alpha1.Completed)},
			}
			nodePool.Status.Properties = hwv1alpha1.Properties{NodeNames: []string{masterNodeName}}
			nodePool.Annotations = map[string]string{"bootInterfaceLabel": "bootable-interface"}
			Expect(c.Create(ctx, nodePool)).To(Succeed())
			createNodeResources(ctx, c, nodePool.Name)
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
			Expect(len(conditions)).To(Equal(9))
			verifyStatusCondition(conditions[6], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.InProgress),
				Message: "Provisioning cluster",
			})
			verifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.ClusterNotReady),
				Message: "The Cluster is not yet ready",
			})

			// Verify the start timestamp has been set for ClusterInstance
			Expect(reconciledCR.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt).ToNot(BeZero())
			// Verify the nonCompliantAt timestamp is not set, even though Non-compliant enforce policy exists
			// but Cluster is not ready
			Expect(reconciledCR.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())
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
			Expect(cr.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt).ToNot(BeZero())
			// Verify the nonCompliantAt timestamp is not set, even though Non-compliant enforce policy exists
			// but Cluster is not ready
			Expect(cr.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())

			// Patch ClusterProvisionStartedAt timestamp to mock timeout
			cr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{Name: "cluster-1"}
			cr.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt = &metav1.Time{Time: metav1.Now().Add(-2 * time.Minute)}
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
			Expect(len(conditions)).To(Equal(9))
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(provisioningv1alpha1.CRconditionReasons.TimedOut),
			})
			verifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.ClusterNotReady),
				Message: "The Cluster is not yet ready",
			})
			// Verify the start timestamp has been set for HardwareProvisioning
			Expect(reconciledCR.Status.Extensions.NodePoolRef.HardwareProvisioningCheckStart).ToNot(BeZero())
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
			Expect(len(conditions)).To(Equal(8))
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Failed),
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
			Expect(len(conditions)).To(Equal(9))
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.InProgress),
				Message: "The configuration is still being applied",
			})

			// Verify the start timestamp has been set for ClusterInstance
			Expect(reconciledCR.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt).ToNot(BeZero())
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
			Expect(len(conditions)).To(Equal(9))
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionTrue,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Completed),
				Message: "The configuration is up to date",
			})

			// Verify the start timestamp is not cleared even Cluster provision has completed
			Expect(reconciledCR.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt).ToNot(BeZero())
			// Verify the nonCompliantAt timestamp is not set since enforce policy is compliant
			Expect(reconciledCR.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())
			// Verify the ztpStatus is set to ZTP done
			Expect(reconciledCR.Status.Extensions.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpDone))
			// Verify the provisioningState sets to fulfilled when the provisioning process is completed
			// and oCloudNodeClusterId is stored
			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFulfilled, "Provisioning request has completed successfully",
				&provisioningv1alpha1.ProvisionedResources{OCloudNodeClusterId: "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})
		})

		It("Verify status when configuration change causes ProvisioningRequest validation to fail but ClusterInstall is still in progress", func() {
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
			Expect(len(conditions)).To(Equal(9))
			verifyStatusCondition(conditions[0], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.Validated),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Failed),
				Message: "nodes.0: hostName is required",
			})
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(provisioningv1alpha1.CRconditionReasons.InProgress),
			})
			verifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.ClusterNotReady),
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
			Expect(len(conditions)).To(Equal(9))
			verifyStatusCondition(conditions[0], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.Validated),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Failed),
				Message: "nodes.0: hostName is required",
			})
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.InProgress),
				Message: "The configuration is still being applied",
			})

			// Verify the start timestamp is not cleared even Cluster provision has completed
			Expect(reconciledCR.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt).ToNot(BeZero())
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
			Expect(cr.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt).ToNot(BeZero())
			// Patch ClusterProvisionStartedAt timestamp to mock timeout
			cr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{Name: "cluster-1"}
			cr.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt = &metav1.Time{Time: metav1.Now().Add(-2 * time.Minute)}
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
			Expect(len(conditions)).To(Equal(9))
			verifyStatusCondition(conditions[0], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.Validated),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Failed),
				Message: "nodes.0: hostName is required",
			})
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(provisioningv1alpha1.CRconditionReasons.TimedOut),
			})
			verifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.ClusterNotReady),
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
			Expect(len(conditions)).To(Equal(9))
			verifyStatusCondition(conditions[1], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Failed),
				Message: "spec.nodes[0].templateRefs must be provided",
			})
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionTrue,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Completed),
				Message: "The configuration is up to date",
			})

			// Verify the ztpStatus is set to ZTP done
			Expect(reconciledCR.Status.Extensions.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpDone))
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

			Expect(len(conditions)).To(Equal(9))
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionTrue,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Completed),
				Message: "The configuration is up to date",
			})

			// Verify the ztpStatus is still set to ZTP done
			Expect(reconciledCR.Status.Extensions.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpDone))
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

			Expect(len(conditions)).To(Equal(9))
			verifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.InProgress),
				Message: "The configuration is still being applied",
			})

			// Verify the ztpStatus is still set to ZTP done
			Expect(reconciledCR.Status.Extensions.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpDone))
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

			nodePool := &hwv1alpha1.NodePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-1",
					Namespace: utils.UnitTestHwmgrNamespace,
					Annotations: map[string]string{
						utils.HwTemplateBootIfaceLabel: "bootable-interface",
					},
				},
				Spec: hwv1alpha1.NodePoolSpec{
					HwMgrId: utils.UnitTestHwmgrID,
					NodeGroup: []hwv1alpha1.NodeGroup{
						{
							NodePoolData: hwv1alpha1.NodePoolData{
								Name:      "controller",
								HwProfile: "profile-spr-single-processor-64G",
							},
							Size: 1,
						},
						{
							NodePoolData: hwv1alpha1.NodePoolData{
								Name:      "worker",
								HwProfile: "profile-spr-dual-processor-128G",
							},
							Size: 0,
						},
					},
				},
				Status: hwv1alpha1.NodePoolStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(hwv1alpha1.Provisioned),
							Status: metav1.ConditionTrue,
							Reason: string(hwv1alpha1.Completed),
						},
					},
					Properties: hwv1alpha1.Properties{
						NodeNames: []string{masterNodeName},
					},
				},
			}
			Expect(c.Create(ctx, nodePool)).To(Succeed())
			createNodeResources(ctx, c, nodePool.Name)

			provisionedCond := metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionFalse,
			}
			cr.Status.Conditions = append(cr.Status.Conditions, provisionedCond)
			cr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{}
			cr.Status.Extensions.ClusterDetails.Name = crName
			cr.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt = &metav1.Time{Time: time.Now()}
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

			Expect(reconciledCR.Status.Extensions.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpNotDone))
			conditions := reconciledCR.Status.Conditions
			// Verify the ProvisioningRequest's status conditions
			verifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.InProgress),
				Message: "The configuration is still being applied",
			})
		})

		It("Sets the status to ZTP Done", func() {
			// Set the policies to compliant.
			policy.Status.ComplianceState = policiesv1.Compliant
			Expect(c.Status().Update(ctx, policy)).To(Succeed())
			// Complete the cluster provisioning.
			cr.Status.Conditions[0].Status = metav1.ConditionTrue
			cr.Status.Conditions[0].Reason = string(provisioningv1alpha1.CRconditionReasons.Completed)
			Expect(c.Status().Update(ctx, cr)).To(Succeed())
			// Start reconciliation.
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result.
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))
			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			Expect(reconciledCR.Status.Extensions.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpDone))
			// Verify the ProvisioningRequest's status conditions
			conditions := reconciledCR.Status.Conditions
			verifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionTrue,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Completed),
				Message: "The configuration is up to date",
			})
		})

		It("Keeps the ZTP status as ZTP Done if a policy becomes NonCompliant", func() {
			cr.Status.Extensions.ClusterDetails.ZtpStatus = utils.ClusterZtpDone
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

			Expect(reconciledCR.Status.Extensions.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpDone))
			conditions := reconciledCR.Status.Conditions
			// Verify the ProvisioningRequest's status conditions
			verifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.InProgress),
				Message: "The configuration is still being applied",
			})
		})
	})

	Context("When handling upgrade", func() {
		var (
			managedCluster    *clusterv1.ManagedCluster
			clusterInstance   *siteconfig.ClusterInstance
			newReleaseVersion string
		)

		BeforeEach(func() {
			newReleaseVersion = "4.16.3"
			managedCluster = &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
					Labels: map[string]string{
						"openshiftVersion": "4.16.0",
					},
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
			networkConfig := &v1beta1.NMStateConfigSpec{
				NetConfig: v1beta1.NetConfig{
					Raw: []byte(
						`
      dns-resolver:
        config:
          server:
          - 192.0.2.22
      interfaces:
      - ipv4:
          address:
          - ip: 192.0.2.10
            prefix-length: 24
          - ip: 192.0.2.11
            prefix-length: 24
          - ip: 192.0.2.12
            prefix-length: 24
          dhcp: false
          enabled: true
        ipv6:
          address:
          - ip: 2001:db8:0:1::42
            prefix-length: 32
          - ip: 2001:db8:0:1::43
            prefix-length: 32
          - ip: 2001:db8:0:1::44
            prefix-length: 32
          dhcp: false
          enabled: true
        name: eno1
        type: ethernet
      - ipv6:
          address:
          - ip: 2001:db8:abcd:1234::1
          enabled: true
          link-aggregation:
            mode: balance-rr
            options:
              miimon: '140'
            slaves:
            - eth0
            - eth1
          prefix-length: 64
        name: bond99
        state: up
        type: bond
      routes:
        config:
        - destination: 0.0.0.0/0
          next-hop-address: 192.0.2.254
          next-hop-interface: eno1
          table-id: 254
                    `,
					),
				},
				Interfaces: []*v1beta1.Interface{
					{Name: "eno1", MacAddress: "00:00:00:01:20:30"},
					{Name: "eth0", MacAddress: "02:00:00:80:12:14"},
					{Name: "eth1", MacAddress: "02:00:00:80:12:15"},
				},
			}
			clusterInstance = &siteconfig.ClusterInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-1",
					Namespace: "cluster-1",
				},
				Spec: siteconfig.ClusterInstanceSpec{
					ClusterName:            "cluster-1",
					ClusterImageSetNameRef: "4.16.0",
					PullSecretRef: corev1.LocalObjectReference{
						Name: "pull-secret",
					},
					BaseDomain: "example.com",
					TemplateRefs: []siteconfig.TemplateRef{
						{
							Name:      "ai-cluster-templates-v1",
							Namespace: "siteconfig-operator",
						},
					},
					IngressVIPs: []string{
						"192.0.2.4",
					},
					ApiVIPs: []string{
						"192.0.2.2",
					},
					Nodes: []siteconfig.NodeSpec{
						{
							HostName: "node1",
							TemplateRefs: []siteconfig.TemplateRef{{
								Name: "ai-node-templates-v1", Namespace: "siteconfig-operator",
							}},
							BootMACAddress: "00:00:00:01:20:30",
							BmcCredentialsName: siteconfig.BmcCredentialsName{
								Name: "site-sno-du-1-bmc-secret",
							},
							IronicInspect:         "false",
							Role:                  "master",
							AutomatedCleaningMode: "disabled",
							BootMode:              "UEFI",
							NodeLabels:            map[string]string{"node-role.kubernetes.io/infra": "", "node-role.kubernetes.io/master": ""},
							NodeNetwork:           networkConfig,
							BmcAddress:            "idrac-virtualmedia+https://203.0.113.5/redfish/v1/Systems/System.Embedded.1",
						},
					},
					ExtraAnnotations: map[string]map[string]string{
						"AgentClusterInstall": {
							"extra-annotation-key": "extra-annotation-value",
						},
					},
					CPUPartitioning: siteconfig.CPUPartitioningNone,
					MachineNetwork: []siteconfig.MachineNetworkEntry{
						{
							CIDR: "192.0.2.0/24",
						},
					},
					AdditionalNTPSources: []string{
						"NTP.server1",
					},
					NetworkType:  "OVNKubernetes",
					SSHPublicKey: "ssh-rsa ",
					ServiceNetwork: []siteconfig.ServiceNetworkEntry{
						{
							CIDR: "233.252.0.0/24",
						},
					},
					ExtraLabels: map[string]map[string]string{
						"AgentClusterInstall": {
							"extra-label-key": "extra-label-value",
						},
						"ManagedCluster": {
							"cluster-version": "v4.17",
						},
					},
					HoldInstallation: true,
				},
			}
			Expect(c.Create(ctx, clusterInstance)).To(Succeed())

			upgradeDefaults := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "upgrade-defaults",
					Namespace: ctNamespace,
				},
				Data: map[string]string{
					utils.UpgradeDefaultsConfigmapKey: `
    ibuSpec:
      seedImageRef:
        image: "image"
        version: "4.16.3"
      oadpContent:
        - name: platform-backup-cm
          namespace: openshift-adp
    plan:
      - actions: ["Prep"]
        rolloutStrategy:
          maxConcurrency: 1
          timeout: 10
      - actions: ["AbortOnFailure"]
        rolloutStrategy:
          maxConcurrency: 1
          timeout: 5
      - actions: ["Upgrade"]
        rolloutStrategy:
          maxConcurrency: 1
          timeout: 30
      - actions: ["AbortOnFailure"]
        rolloutStrategy:
          maxConcurrency: 1
          timeout: 5
      - actions: ["FinalizeUpgrade"]
        rolloutStrategy:
          maxConcurrency: 1
          timeout: 5
    `,
				},
			}
			Expect(c.Create(ctx, upgradeDefaults)).To(Succeed())
			clusterInstanceDefaultsV2 := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-instance-defaults-v2",
					Namespace: ctNamespace,
				},
				Data: map[string]string{
					utils.ClusterInstallationTimeoutConfigKey: "60s",
					utils.ClusterInstanceTemplateDefaultsConfigmapKey: `
    clusterImageSetNameRef: "4.16.3"
    pullSecretRef:
      name: "pull-secret"
    templateRefs:
    - name: "ai-cluster-templates-v1"
      namespace: "siteconfig-operator"
    holdInstallation: true
    nodes:
    - hostname: "node1"
      ironicInspect: "false"
      templateRefs:
      - name: "ai-node-templates-v1"
        namespace: "siteconfig-operator"
      nodeNetwork:
        interfaces:
        - name: eno1
          label: bootable-interface
        - name: eth0
          label: base-interface
        - name: eth1
          label: data-interface
    `,
				},
			}
			Expect(c.Create(ctx, clusterInstanceDefaultsV2)).To(Succeed())

			nodePool := &hwv1alpha1.NodePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-1",
					Namespace: utils.UnitTestHwmgrNamespace,
					Annotations: map[string]string{
						utils.HwTemplateBootIfaceLabel: "bootable-interface",
					},
				},
				Spec: hwv1alpha1.NodePoolSpec{
					HwMgrId: utils.UnitTestHwmgrID,
					NodeGroup: []hwv1alpha1.NodeGroup{
						{
							NodePoolData: hwv1alpha1.NodePoolData{
								Name:      "controller",
								Role:      "master",
								HwProfile: "profile-spr-single-processor-64G",
							},
							Size: 1,
						},
						{
							NodePoolData: hwv1alpha1.NodePoolData{
								Name:      "worker",
								Role:      "worker",
								HwProfile: "profile-spr-dual-processor-128G",
							},
							Size: 0,
						},
					},
				},
				Status: hwv1alpha1.NodePoolStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(hwv1alpha1.Provisioned),
							Status: metav1.ConditionTrue,
							Reason: string(hwv1alpha1.Completed),
						},
					},
					Properties: hwv1alpha1.Properties{
						NodeNames: []string{masterNodeName},
					},
				},
			}
			Expect(c.Create(ctx, nodePool)).To(Succeed())
			createNodeResources(ctx, c, nodePool.Name)

			provisionedCond := metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			}
			cr.Status.Conditions = append(cr.Status.Conditions, provisionedCond)
			cr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{}
			cr.Status.Extensions.ClusterDetails.Name = crName
			cr.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt = &metav1.Time{Time: time.Now()}
			Expect(c.Status().Update(ctx, cr)).To(Succeed())
			object := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, object)).To(Succeed())
			object.Spec.TemplateVersion = "v3.0.0"
			Expect(c.Update(ctx, object)).To(Succeed())

			ctNew := &provisioningv1alpha1.ClusterTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      getClusterTemplateRefName(tName, object.Spec.TemplateVersion),
					Namespace: ctNamespace,
				},
				Spec: provisioningv1alpha1.ClusterTemplateSpec{
					Name:       tName,
					Version:    object.Spec.TemplateVersion,
					Release:    newReleaseVersion,
					TemplateID: "57b39bda-ac56-4143-9b10-d1a71517d04f",
					Templates: provisioningv1alpha1.Templates{
						ClusterInstanceDefaults: "cluster-instance-defaults-v2",
						PolicyTemplateDefaults:  ptDefaultsCm,
						HwTemplate:              hwTemplate,
						UpgradeDefaults:         "upgrade-defaults",
					},
					TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testFullTemplateSchema)},
				},
				Status: provisioningv1alpha1.ClusterTemplateStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(provisioningv1alpha1.CTconditionTypes.Validated),
							Reason: string(provisioningv1alpha1.CTconditionReasons.Completed),
							Status: metav1.ConditionTrue,
						},
					},
				},
			}
			Expect(c.Create(ctx, ctNew)).To(Succeed())
		})

		It("Creates ImageBasedUpgrade", func() {
			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithMediumInterval()))

			// check ProvisioningRequest conditions
			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())

			verifyStatusCondition(reconciledCR.Status.Conditions[9], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.InProgress),
				Message: "Upgrade is in progress",
			})

			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Cluster upgrade is in progress",
				nil)

			// check ClusterInstance fields
			ci := &siteconfig.ClusterInstance{}
			Expect(c.Get(ctx, types.NamespacedName{Namespace: "cluster-1", Name: "cluster-1"}, ci)).To(Succeed())

			Expect(ci.Spec.ClusterImageSetNameRef).To(Equal(newReleaseVersion))
			Expect(ci.Spec.SuppressedManifests).To(Equal(utils.CRDsToBeSuppressedForUpgrade))

			// check IBGU fields
			ibgu := &ibguv1alpha1.ImageBasedGroupUpgrade{}
			Expect(c.Get(ctx, types.NamespacedName{Namespace: "cluster-1", Name: "cluster-1"}, ibgu)).To(Succeed())
			Expect(ibgu.Spec.IBUSpec.SeedImageRef.Image).To(Equal("image"))
			Expect(ibgu.Spec.IBUSpec.SeedImageRef.Version).To(Equal(newReleaseVersion))
			Expect(len(ibgu.Spec.IBUSpec.OADPContent)).To(Equal(1))
			Expect(len(ibgu.Spec.Plan)).To(Equal(5))
		})

		It("Checks IBGU is in progress", func() {

			ibgu := &ibguv1alpha1.ImageBasedGroupUpgrade{ObjectMeta: metav1.ObjectMeta{
				Name: "cluster-1", Namespace: "cluster-1",
			}}
			Expect(c.Create(ctx, ibgu)).To(Succeed())

			clusterInstance.Spec.ClusterImageSetNameRef = newReleaseVersion
			clusterInstance.Spec.SuppressedManifests = utils.CRDsToBeSuppressedForUpgrade
			Expect(c.Update(ctx, clusterInstance)).To(Succeed())

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithMediumInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())

			verifyStatusCondition(reconciledCR.Status.Conditions[9], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.InProgress),
				Message: "Upgrade is in progress",
			})

			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Cluster upgrade is in progress",
				nil)

			// checks SuppressedManifests are not wiped
			ci := &siteconfig.ClusterInstance{}
			Expect(c.Get(ctx, types.NamespacedName{Namespace: "cluster-1", Name: "cluster-1"}, ci)).To(Succeed())
			Expect(ci.Spec.SuppressedManifests).To(Equal(utils.CRDsToBeSuppressedForUpgrade))
		})

		It("Checks IBGU is completed", func() {

			ibgu := &ibguv1alpha1.ImageBasedGroupUpgrade{ObjectMeta: metav1.ObjectMeta{
				Name: "cluster-1", Namespace: "cluster-1",
			},
				Spec: ibguv1alpha1.ImageBasedGroupUpgradeSpec{
					IBUSpec: lcav1.ImageBasedUpgradeSpec{
						SeedImageRef: lcav1.SeedImageRef{
							Version: newReleaseVersion,
						},
					},
				},
				Status: ibguv1alpha1.ImageBasedGroupUpgradeStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Progressing",
							Status: "False",
						},
					},
				}}
			Expect(c.Create(ctx, ibgu)).To(Succeed())

			clusterInstance.Spec.ClusterImageSetNameRef = newReleaseVersion
			clusterInstance.Spec.SuppressedManifests = utils.CRDsToBeSuppressedForUpgrade
			Expect(c.Update(ctx, clusterInstance)).To(Succeed())

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())

			verifyStatusCondition(reconciledCR.Status.Conditions[9], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted),
				Status:  metav1.ConditionTrue,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Completed),
				Message: "Upgrade is completed",
			})

			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFulfilled, "Provisioning request has completed successfully",
				nil)
			Expect(c.Get(ctx, types.NamespacedName{Namespace: "cluster-1", Name: "cluster-1"}, ibgu)).To(Not(Succeed()))
		})
		It("Checks IBGU is failed", func() {

			ibgu := &ibguv1alpha1.ImageBasedGroupUpgrade{ObjectMeta: metav1.ObjectMeta{
				Name: "cluster-1", Namespace: "cluster-1",
			},
				Spec: ibguv1alpha1.ImageBasedGroupUpgradeSpec{
					IBUSpec: lcav1.ImageBasedUpgradeSpec{
						SeedImageRef: lcav1.SeedImageRef{
							Version: newReleaseVersion,
						},
					},
				},
				Status: ibguv1alpha1.ImageBasedGroupUpgradeStatus{
					Clusters: []ibguv1alpha1.ClusterState{
						{
							Name: "cluster-1",
							FailedActions: []ibguv1alpha1.ActionMessage{
								{
									Action:  "Prep",
									Message: "pre-cache failed",
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   "Progressing",
							Status: "False",
						},
					},
				}}
			Expect(c.Create(ctx, ibgu)).To(Succeed())

			clusterInstance.Spec.ClusterImageSetNameRef = newReleaseVersion
			clusterInstance.Spec.SuppressedManifests = utils.CRDsToBeSuppressedForUpgrade
			Expect(c.Update(ctx, clusterInstance)).To(Succeed())

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())

			verifyStatusCondition(reconciledCR.Status.Conditions[9], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Failed),
				Message: "Upgrade Failed: Action Prep failed: pre-cache failed",
			})

			verifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Cluster upgrade is failed",
				nil)
		})
	})
})
