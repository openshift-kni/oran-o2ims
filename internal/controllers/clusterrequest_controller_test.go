package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
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
)

type expectedNodeDetails struct {
	BMCAddress         string
	BMCCredentialsName string
	BootMACAddress     string
	Interfaces         []map[string]interface{}
}

const (
	testClusterTemplateSchema = `{
		"description": "ClusterInstanceSpec defines the params that are allowed in the ClusterRequest spec.clusterInstanceInput",
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
				  "description": "A workaround to provide bmc creds through ClusterRequest",
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
						  "macAddress"
						],
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

	testClusterTemplateInput = `{
		"additionalNTPSources": [
		  "NTP.server1"
		],
		"apiVIPs": [
		  "10.16.231.1"
		],
		"baseDomain": "example.com",
		"clusterName": "cluster-1",
		"machineNetwork": [
		  {
			"cidr": "10.16.231.0/24"
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
		  "10.16.231.2"
		],
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
			}
		  }
		],
		"serviceNetwork": [
		  {
			"cidr": "172.30.0.0/16"
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
	ctx context.Context, c client.Client, crName, crNamespace string) {
	// Remove required field hostname
	currentCR := &oranv1alpha1.ClusterRequest{}
	Expect(c.Get(ctx, types.NamespacedName{Name: crName, Namespace: crNamespace}, currentCR)).To(Succeed())

	clusterInstanceInput := make(map[string]any)
	err := json.Unmarshal([]byte(testClusterTemplateInput), &clusterInstanceInput)
	Expect(err).ToNot(HaveOccurred())
	node1 := clusterInstanceInput["nodes"].([]any)[0]
	delete(node1.(map[string]any), "hostName")
	updatedClusterInstanceInput, err := json.Marshal(clusterInstanceInput)
	Expect(err).ToNot(HaveOccurred())

	currentCR.Spec.ClusterTemplateInput.ClusterInstanceInput.Raw = updatedClusterInstanceInput
	Expect(c.Update(ctx, currentCR)).To(Succeed())
}

var _ = Describe("ClusterRequestReconcile", func() {
	var (
		c            client.Client
		ctx          context.Context
		reconciler   *ClusterRequestReconciler
		req          reconcile.Request
		cr           *oranv1alpha1.ClusterRequest
		ct           *oranv1alpha1.ClusterTemplate
		ctName       = "clustertemplate-a-v1"
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
		ct = &oranv1alpha1.ClusterTemplate{
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
					ClusterInstanceSchema: runtime.RawExtension{
						Raw: []byte(testClusterTemplateSchema),
					},
					PolicyTemplateSchema: runtime.RawExtension{
						Raw: []byte(testPolicyTemplateSchema),
					},
				},
			},
			Status: oranv1alpha1.ClusterTemplateStatus{
				Conditions: []metav1.Condition{
					{
						Type:   string(utils.CTconditionTypes.Validated),
						Reason: string(utils.CTconditionReasons.Completed),
						Status: metav1.ConditionTrue,
					},
				},
			},
		}

		// Define the cluster request.
		cr = &oranv1alpha1.ClusterRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:       crName,
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
		}

		crs = append(crs, cr, ct)
		c = getFakeClientFromObjects(crs...)
		reconciler = &ClusterRequestReconciler{
			Client: c,
			Logger: logger,
		}

		// Request for ClusterRequest
		req = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      crName,
				Namespace: ctNamespace,
			},
		}
	})

	Context("Resources preparation during initial Provisioning", func() {
		It("Verify status conditions if ClusterRequest validation fails", func() {
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

			reconciledCR := &oranv1alpha1.ClusterRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ClusterRequest's status conditions
			Expect(len(conditions)).To(Equal(1))
			verifyStatusCondition(conditions[0], metav1.Condition{
				Type:    string(utils.CRconditionTypes.Validated),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.Failed),
				Message: "Failed to validate the ClusterRequest: the clustertemplate validation has failed",
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

			reconciledCR := &oranv1alpha1.ClusterRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ClusterRequest's status conditions
			Expect(len(conditions)).To(Equal(2))
			verifyStatusCondition(conditions[0], metav1.Condition{
				Type:   string(utils.CRconditionTypes.Validated),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[1], metav1.Condition{
				Type:    string(utils.CRconditionTypes.ClusterInstanceRendered),
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

			reconciledCR := &oranv1alpha1.ClusterRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ClusterRequest's status conditions
			Expect(len(conditions)).To(Equal(3))
			verifyStatusCondition(conditions[0], metav1.Condition{
				Type:   string(utils.CRconditionTypes.Validated),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[1], metav1.Condition{
				Type:   string(utils.CRconditionTypes.ClusterInstanceRendered),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[2], metav1.Condition{
				Type:    string(utils.CRconditionTypes.ClusterResourcesCreated),
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

			reconciledCR := &oranv1alpha1.ClusterRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify NodePool was created
			nodePool := &hwv1alpha1.NodePool{}
			Expect(c.Get(ctx, types.NamespacedName{
				Name: crName, Namespace: "hwmgr"}, nodePool)).To(Succeed())

			// Verify the ClusterRequest's status conditions
			Expect(len(conditions)).To(Equal(4))
			verifyStatusCondition(conditions[0], metav1.Condition{
				Type:   string(utils.CRconditionTypes.Validated),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[1], metav1.Condition{
				Type:   string(utils.CRconditionTypes.ClusterInstanceRendered),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[2], metav1.Condition{
				Type:   string(utils.CRconditionTypes.ClusterResourcesCreated),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[3], metav1.Condition{
				Type:   string(utils.CRconditionTypes.HardwareTemplateRendered),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
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

			reconciledCR := &oranv1alpha1.ClusterRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// TODO: turn it on when hw plugin is ready
			//
			// Verify no ClusterInstance was created
			// clusterInstance := &siteconfig.ClusterInstance{}
			// Expect(c.Get(ctx, types.NamespacedName{
			// 	Name: crName, Namespace: crName}, clusterInstance)).To(HaveOccurred())

			// Verify the ClusterRequest's status conditions
			// TODO: change the number of conditions to 5 when hw plugin is ready
			Expect(len(conditions)).To(Equal(6))
			verifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(utils.CRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(utils.CRconditionReasons.InProgress),
			})
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

			reconciledCR := &oranv1alpha1.ClusterRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ClusterInstance was created
			clusterInstance := &siteconfig.ClusterInstance{}
			Expect(c.Get(ctx, types.NamespacedName{
				Name: crName, Namespace: crName}, clusterInstance)).To(Succeed())

			// Verify the ClusterRequest's status conditions
			Expect(len(conditions)).To(Equal(6))
			verifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(utils.CRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[5], metav1.Condition{
				Type:   string(utils.CRconditionTypes.ClusterInstanceProcessed),
				Status: metav1.ConditionUnknown,
				Reason: string(utils.CRconditionReasons.Unknown),
			})
		})

		It("Verify status conditions when configuration change causes ClusterRequest validation to fail but NodePool becomes provsioned ", func() {
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

			// Remove required field hostname to fail ClusterRequest validation
			removeRequiredFieldFromClusterInstanceInput(ctx, c, crName, ctNamespace)

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &oranv1alpha1.ClusterRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// TODO: change the number of conditions to 5 when hw plugin is ready
			// Verify that the validated condition fails but hw provisioned condition
			// has changed to Completed
			Expect(len(conditions)).To(Equal(6))
			verifyStatusCondition(conditions[0], metav1.Condition{
				Type:    string(utils.CRconditionTypes.Validated),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.Failed),
				Message: "nodes.0: hostName is required",
			})
			verifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(utils.CRconditionTypes.HardwareProvisioned),
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

			reconciledCR := &oranv1alpha1.ClusterRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// TODO: change the number of conditions to 5 when hw plugin is ready
			// Verify that the ClusterInstanceRendered condition fails but hw provisioned condition
			// has changed to Completed
			Expect(len(conditions)).To(Equal(6))
			verifyStatusCondition(conditions[1], metav1.Condition{
				Type:    string(utils.CRconditionTypes.ClusterInstanceRendered),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.Failed),
				Message: "spec.nodes[0].templateRefs must be provided",
			})
			verifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(utils.CRconditionTypes.HardwareProvisioned),
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
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &oranv1alpha1.ClusterRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ClusterRequest's status conditions
			Expect(len(conditions)).To(Equal(8))
			verifyStatusCondition(conditions[5], metav1.Condition{
				Type:   string(utils.CRconditionTypes.ClusterInstanceProcessed),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[6], metav1.Condition{
				Type:    string(utils.CRconditionTypes.ClusterProvisioned),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.InProgress),
				Message: "Provisioning cluster",
			})
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:    string(utils.CRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.InProgress),
				Message: "The configuration is still being applied",
			})
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

			reconciledCR := &oranv1alpha1.ClusterRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ClusterRequest's status conditions
			Expect(len(conditions)).To(Equal(8))
			verifyStatusCondition(conditions[6], metav1.Condition{
				Type:   string(utils.CRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:    string(utils.CRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionTrue,
				Reason:  string(utils.CRconditionReasons.Completed),
				Message: "The configuration is up to date",
			})
		})

		It("Verify status conditions when configuration change causes ClusterRequest validation to fail but ClusterInstance becomes provisioned", func() {
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

			// Remove required field hostname to fail ClusterRequest validation
			removeRequiredFieldFromClusterInstanceInput(ctx, c, crName, ctNamespace)

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &oranv1alpha1.ClusterRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify that the Validated condition fails but ClusterProvisioned condition
			// has changed to Completed
			Expect(len(conditions)).To(Equal(8))
			verifyStatusCondition(conditions[0], metav1.Condition{
				Type:    string(utils.CRconditionTypes.Validated),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.Failed),
				Message: "nodes.0: hostName is required",
			})
			verifyStatusCondition(conditions[6], metav1.Condition{
				Type:   string(utils.CRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:    string(utils.CRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.InProgress),
				Message: "The configuration is still being applied",
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

			reconciledCR := &oranv1alpha1.ClusterRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify that the ClusterInstanceRendered condition fails but configurationApplied
			// has changed to Completed
			Expect(len(conditions)).To(Equal(8))
			verifyStatusCondition(conditions[1], metav1.Condition{
				Type:    string(utils.CRconditionTypes.ClusterInstanceRendered),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.Failed),
				Message: "spec.nodes[0].templateRefs must be provided",
			})
			verifyStatusCondition(conditions[6], metav1.Condition{
				Type:   string(utils.CRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			})
			verifyStatusCondition(conditions[7], metav1.Condition{
				Type:    string(utils.CRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionTrue,
				Reason:  string(utils.CRconditionReasons.Completed),
				Message: "The configuration is up to date",
			})
		})
	})
})

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
		clusterInstance *siteconfig.ClusterInstance
		ct              *oranv1alpha1.ClusterTemplate
		ctName          = "clustertemplate-a-v1"
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
		Expect(nodePool.ObjectMeta.Namespace).To(Equal(cm.Data[utils.HwTemplatePluginMgr]))
		Expect(nodePool.Annotations[utils.HwTemplateBootIfaceLabel]).To(Equal(cm.Data[utils.HwTemplateBootIfaceLabel]))

		Expect(nodePool.Spec.CloudID).To(Equal(clusterInstance.GetName()))
		Expect(nodePool.Labels[clusterRequestNameLabel]).To(Equal(task.object.Name))
		Expect(nodePool.Labels[clusterRequestNamespaceLabel]).To(Equal(task.object.Namespace))

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
		rt := task.checkNodePoolProvisionStatus(ctx, np)
		Expect(rt).To(Equal(false))
	})

	It("returns false when NodePool is not provisioned", func() {
		provisionedCondition := metav1.Condition{
			Type:   "Provisioned",
			Status: metav1.ConditionFalse,
		}
		np.Status.Conditions = append(np.Status.Conditions, provisionedCondition)
		Expect(c.Create(ctx, np)).To(Succeed())

		rt := task.checkNodePoolProvisionStatus(ctx, np)
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
		rt := task.checkNodePoolProvisionStatus(ctx, np)
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
				Status: oranv1alpha1.ClusterRequestStatus{
					// Fake the hw provision status
					Conditions: []metav1.Condition{
						{
							Type:   string(utils.CRconditionTypes.HardwareProvisioned),
							Status: metav1.ConditionTrue,
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
			types.NamespacedName{
				Name:      "cluster-1",
				Namespace: "cluster-1",
			},
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
				Name: "ClusterRequestsQueue",
			})
		CRReconciler.handlePolicyEventUpdate(ctx, *updateEvent, queue)
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
		CRReconciler.handlePolicyEventDelete(ctx, *deleteEvent, queue)
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
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(utils.CRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.CRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.OutOfDate)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is out of date"))
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
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(utils.CRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.CRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionTrue))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.Completed)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is up to date"))
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
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(utils.CRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.CRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.InProgress)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is still being applied"))
	})

	It("Updates ClusterRequest ConfigurationApplied condition to InProgress when the cluster is "+
		"Pending with at least one enforce policy", func() {
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
					ComplianceState: "Pending",
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
			conditions, string(utils.CRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.CRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.InProgress)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is still being applied"))
	})
})
