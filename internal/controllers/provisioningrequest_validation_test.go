/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Assisted-by: Cursor/claude-4-sonnet
*/

/*
Test Cases Overview:

This file contains unit tests for the overrideClusterInstanceLabelsOrAnnotations function
which handles merging configuration from configmaps into provisioning request inputs.

Test Cases:
1. "should override only existing keys" - Verifies that only pre-existing keys in the
   destination are overridden, while new keys from source are ignored.

2. "should not add new resource types to dstProvisioningRequestInput" - Ensures that
   new resource types from the source configmap are not added to the destination.

3. "should not add extraLabels/extraAnnotations field if not found in ProvisioningRequestInput" -
   Confirms that missing top-level fields in the destination are not created from the source.

4. "should merge nodes and handle nested labels/annotations" - Tests the merging logic
   for node-level configurations, ensuring proper override of existing nested values.

5. "should not add the new node to dstProvisioningRequestInput" - Verifies that additional
   nodes from the source are not added to the destination node list.

6. "should override only existing keys in nodeGroups format" - Same as (1) for group-level
   extraLabels/extraAnnotations in the nodeGroups format.

7. "should merge nodeGroups nodes and handle nested labels/annotations" - Same as (4) for
   per-node extraLabels/extraAnnotations under nodeGroups, matched by group name.

8. "should override existing keys at both nodeGroups and nodes levels" - Verifies defaults
   win for the same key when the PR sets it on both the group and a nested node.
*/

package controllers

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/test/fakeclient"
)

var _ = Describe("overrideClusterInstanceLabelsOrAnnotations", func() {
	var (
		dstProvisioningRequestInput map[string]any
		srcConfigmap                map[string]any
		task                        *provisioningRequestReconcilerTask
	)

	BeforeEach(func() {
		dstProvisioningRequestInput = make(map[string]any)
		srcConfigmap = make(map[string]any)

		task = &provisioningRequestReconcilerTask{
			logger:       logger,
			client:       nil,
			object:       nil,
			clusterInput: &clusterInput{},
			ctDetails:    &clusterTemplateDetails{},
		}
	})

	It("should override only existing keys", func() {
		dstProvisioningRequestInput = map[string]any{
			"extraLabels": map[string]any{
				"ManagedCluster": map[string]any{
					"label1": "value1",
				},
			},
			"extraAnnotations": map[string]any{
				"ManagedCluster": map[string]any{
					"annotation1": "value1",
				},
			},
			"clusterName": "cluster-1",
		}

		srcConfigmap = map[string]any{
			"extraLabels": map[string]any{
				"ManagedCluster": map[string]any{
					"label1": "new_value1", // Existing key in dst
					"label2": "value2",     // New key, should be ignored
				},
			},
			"extraAnnotations": map[string]any{
				"ManagedCluster": map[string]any{
					"annotation2": "value2", // New key, should be ignored
				},
			},
		}

		expected := map[string]any{
			"extraLabels": map[string]any{
				"ManagedCluster": map[string]any{
					"label1": "new_value1", // Overridden
				},
			},
			"extraAnnotations": map[string]any{
				"ManagedCluster": map[string]any{
					"annotation1": "value1",
				},
			},
			"clusterName": "cluster-1",
		}

		err := task.overrideClusterInstanceLabelsOrAnnotations(dstProvisioningRequestInput, srcConfigmap)
		Expect(err).ToNot(HaveOccurred())
		Expect(dstProvisioningRequestInput).To(Equal(expected))
	})

	It("should not add new resource types to dstProvisioningRequestInput", func() {
		dstProvisioningRequestInput = map[string]any{
			"extraLabels": map[string]any{
				"ManagedCluster": map[string]any{
					"label1": "value1",
				},
			},
			"clusterName": "cluster-1",
		}

		srcConfigmap = map[string]any{
			"extraLabels": map[string]any{
				"AgentClusterInstall": map[string]any{
					"label1": "value1", // New resource type, should be ignored
				},
			},
		}

		expected := map[string]any{
			"extraLabels": map[string]any{
				"ManagedCluster": map[string]any{
					"label1": "value1", // Should remain unchanged
				},
			},
			"clusterName": "cluster-1",
		}

		err := task.overrideClusterInstanceLabelsOrAnnotations(dstProvisioningRequestInput, srcConfigmap)
		Expect(err).ToNot(HaveOccurred())
		Expect(dstProvisioningRequestInput).To(Equal(expected))
	})

	It("should not add extraLabels/extraAnnotations field if not found in ProvisioningRequestInput", func() {
		dstProvisioningRequestInput = map[string]any{
			"extraLabels": map[string]any{
				"ManagedCluster": map[string]any{
					"label1": "value1",
				},
			},
			"clusterName": "cluster-1",
		}

		srcConfigmap = map[string]any{
			"extraAnnotations": map[string]any{ // Field does not exist in dstProvisioningRequestInput
				"ManagedCluster": map[string]any{
					"annotation1": "value1",
				},
			},
		}

		expected := map[string]any{
			"extraLabels": map[string]any{ // Should remain unchanged
				"ManagedCluster": map[string]any{
					"label1": "value1",
				},
			},
			"clusterName": "cluster-1",
		}

		err := task.overrideClusterInstanceLabelsOrAnnotations(dstProvisioningRequestInput, srcConfigmap)
		Expect(err).ToNot(HaveOccurred())
		Expect(dstProvisioningRequestInput).To(Equal(expected))
	})

	It("should merge nodes and handle nested labels/annotations", func() {
		dstProvisioningRequestInput = map[string]any{
			"clusterName": "cluster-1",
			"nodes": []any{
				map[string]any{
					"hostName": "node1",
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"label1": "value1",
						},
					},
					"extraAnnotations": map[string]any{
						"ManagedCluster": map[string]any{
							"annotation1": "value1",
						},
					},
				},
				map[string]any{
					"hostName": "node2",
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"label2": "value2",
						},
					},
					"extraAnnotations": map[string]any{
						"ManagedCluster": map[string]any{
							"annotation2": "value2",
						},
					},
				},
			},
		}

		srcConfigmap = map[string]any{
			"nodes": []any{
				map[string]any{
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"label1": "new_value1", // Existing label, should be overridden
							"label2": "value2",     // New label, should be ignored
						},
					},
					"extraAnnotations": map[string]any{
						"ManagedCluster": map[string]any{
							"annotation2": "value2", // New annotation, should be ignored
						},
					},
				},
				map[string]any{
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"label1": "value1",     // New label, should be ignored
							"label2": "new_value2", // Existing label, should be overridden
						},
					},
				},
			},
		}

		expected := map[string]any{
			"clusterName": "cluster-1",
			"nodes": []any{
				map[string]any{
					"hostName": "node1",
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"label1": "new_value1", // Overridden
						},
					},
					"extraAnnotations": map[string]any{
						"ManagedCluster": map[string]any{
							"annotation1": "value1", // no change
						},
					},
				},
				map[string]any{
					"hostName": "node2",
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"label2": "new_value2", // Overridden
						},
					},
					"extraAnnotations": map[string]any{
						"ManagedCluster": map[string]any{
							"annotation2": "value2",
						},
					},
				},
			},
		}

		err := task.overrideClusterInstanceLabelsOrAnnotations(dstProvisioningRequestInput, srcConfigmap)
		Expect(err).ToNot(HaveOccurred())
		Expect(dstProvisioningRequestInput).To(Equal(expected))
	})

	It("should not add the new node to dstProvisioningRequestInput", func() {
		dstProvisioningRequestInput = map[string]any{
			"clusterName": "cluster-1",
			"nodes": []any{
				map[string]any{
					"hostName": "node1",
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"label1": "value1",
						},
					},
					"extraAnnotations": map[string]any{
						"ManagedCluster": map[string]any{
							"annotation1": "value1",
						},
					},
				},
			},
		}

		srcConfigmap = map[string]any{
			"nodes": []any{
				map[string]any{
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"label1": "new_value1", // Existing label, should be overridden
							"label2": "value2",     // New label, should be ignored
						},
					},
					"extraAnnotations": map[string]any{
						"ManagedCluster": map[string]any{
							"annotation2": "value2", // New annotation, should be ignored
						},
					},
				},
				// New node, should be ignored
				map[string]any{
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"label1": "value1",
							"label2": "value2",
						},
					},
				},
			},
		}

		expected := map[string]any{
			"clusterName": "cluster-1",
			"nodes": []any{
				map[string]any{
					"hostName": "node1",
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"label1": "new_value1", // Overridden
						},
					},
					"extraAnnotations": map[string]any{
						"ManagedCluster": map[string]any{
							"annotation1": "value1", // no change
						},
					},
				},
			},
		}

		err := task.overrideClusterInstanceLabelsOrAnnotations(dstProvisioningRequestInput, srcConfigmap)
		Expect(err).ToNot(HaveOccurred())
		Expect(dstProvisioningRequestInput).To(Equal(expected))
	})

	It("should override only existing keys in nodeGroups format", func() {
		dstProvisioningRequestInput = map[string]any{
			"nodeGroups": []any{
				map[string]any{
					"name": "master",
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"enforced": "from-pr",
							"pr-only":  "pr-value",
						},
					},
					"extraAnnotations": map[string]any{
						"BareMetalHost": map[string]any{
							"enforced": "from-pr",
						},
					},
					"nodes": []any{
						map[string]any{"hostName": "master-1.example.com"},
					},
				},
			},
		}
		srcConfigmap = map[string]any{
			"nodeGroups": []any{
				map[string]any{
					"name": "master",
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"enforced": "from-defaults",
						},
					},
					"extraAnnotations": map[string]any{
						"BareMetalHost": map[string]any{
							"enforced": "from-defaults",
						},
					},
				},
			},
		}
		expected := map[string]any{
			"nodeGroups": []any{
				map[string]any{
					"name": "master",
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"enforced": "from-defaults",
							"pr-only":  "pr-value",
						},
					},
					"extraAnnotations": map[string]any{
						"BareMetalHost": map[string]any{
							"enforced": "from-defaults",
						},
					},
					"nodes": []any{
						map[string]any{"hostName": "master-1.example.com"},
					},
				},
			},
		}

		err := task.overrideClusterInstanceLabelsOrAnnotations(dstProvisioningRequestInput, srcConfigmap)
		Expect(err).ToNot(HaveOccurred())
		Expect(dstProvisioningRequestInput).To(Equal(expected))
	})

	It("should merge nodeGroups nodes and handle nested labels/annotations", func() {
		dstProvisioningRequestInput = map[string]any{
			"nodeGroups": []any{
				map[string]any{
					"name": "master",
					"nodes": []any{
						map[string]any{
							"hostName": "master-1.example.com",
							"extraLabels": map[string]any{
								"ManagedCluster": map[string]any{
									"enforced":  "from-pr",
									"host-only": "host-value",
								},
							},
						},
						map[string]any{
							"hostName": "master-2.example.com",
						},
					},
				},
			},
		}
		srcConfigmap = map[string]any{
			"nodeGroups": []any{
				map[string]any{
					"name": "master",
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"enforced": "from-defaults",
						},
					},
				},
			},
		}
		expected := map[string]any{
			"nodeGroups": []any{
				map[string]any{
					"name": "master",
					"nodes": []any{
						map[string]any{
							"hostName": "master-1.example.com",
							"extraLabels": map[string]any{
								"ManagedCluster": map[string]any{
									"enforced":  "from-defaults",
									"host-only": "host-value",
								},
							},
						},
						// No extraLabels on the PR node, so defaults are not applied here.
						map[string]any{
							"hostName": "master-2.example.com",
						},
					},
				},
			},
		}

		err := task.overrideClusterInstanceLabelsOrAnnotations(dstProvisioningRequestInput, srcConfigmap)
		Expect(err).ToNot(HaveOccurred())
		Expect(dstProvisioningRequestInput).To(Equal(expected))
	})

	It("should override existing keys at both nodeGroups and nodes levels", func() {
		dstProvisioningRequestInput = map[string]any{
			"nodeGroups": []any{
				map[string]any{
					"name": "master",
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"enforced": "from-pr-group",
						},
					},
					"nodes": []any{
						map[string]any{
							"hostName": "master-1.example.com",
							"extraLabels": map[string]any{
								"ManagedCluster": map[string]any{
									"enforced": "from-pr-node",
								},
							},
						},
					},
				},
			},
		}
		srcConfigmap = map[string]any{
			"nodeGroups": []any{
				map[string]any{
					"name": "master",
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"enforced": "from-defaults",
						},
					},
				},
			},
		}
		expected := map[string]any{
			"nodeGroups": []any{
				map[string]any{
					"name": "master",
					"extraLabels": map[string]any{
						"ManagedCluster": map[string]any{
							"enforced": "from-defaults",
						},
					},
					"nodes": []any{
						map[string]any{
							"hostName": "master-1.example.com",
							"extraLabels": map[string]any{
								"ManagedCluster": map[string]any{
									"enforced": "from-defaults",
								},
							},
						},
					},
				},
			},
		}

		err := task.overrideClusterInstanceLabelsOrAnnotations(dstProvisioningRequestInput, srcConfigmap)
		Expect(err).ToNot(HaveOccurred())
		Expect(dstProvisioningRequestInput).To(Equal(expected))
	})
})

var _ = Describe("resolveClusterInstanceFormat", func() {
	It("returns nodeGroups when both sides exclusively use nodeGroups", func() {
		format, err := resolveClusterInstanceFormat(
			map[string]any{"nodeGroups": []any{}},
			map[string]any{"nodeGroups": []any{}},
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(format).To(Equal(ctlrutils.ClusterInstanceNodeGroupsKey))
	})

	It("returns nodes when both sides exclusively use nodes", func() {
		format, err := resolveClusterInstanceFormat(
			map[string]any{"nodes": []any{}},
			map[string]any{"nodes": []any{}},
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(format).To(Equal(ctlrutils.ClusterInstanceNodesKey))
	})

	It("rejects mixed or incomplete formats", func() {
		_, err := resolveClusterInstanceFormat(
			map[string]any{"nodeGroups": []any{}},
			map[string]any{"nodes": []any{}},
		)
		Expect(err).To(HaveOccurred())

		_, err = resolveClusterInstanceFormat(
			map[string]any{"nodeGroups": []any{}, "nodes": []any{}},
			map[string]any{"nodeGroups": []any{}},
		)
		Expect(err).To(HaveOccurred())

		_, err = resolveClusterInstanceFormat(
			map[string]any{"nodeGroups": []any{}},
			map[string]any{"clusterName": "x"},
		)
		Expect(err).To(HaveOccurred())

		// Both sides omit nodes/nodeGroups — reject (no defaults-only inheritance).
		_, err = resolveClusterInstanceFormat(
			map[string]any{"clusterName": "x"},
			map[string]any{"clusterName": "y"},
		)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("getMergedClusterInstanceData", func() {
	var (
		ctx          context.Context
		task         *provisioningRequestReconcilerTask
		ciDefaultsCm = "clusterinstance-defaults-v1"
		ctNamespace  = "clustertemplate-merge-ns"
	)

	newTask := func(defaultsYAML string, clusterInstanceParamsYAML string) *provisioningRequestReconcilerTask {
		paramsJSON, err := yaml.YAMLToJSON([]byte(clusterInstanceParamsYAML))
		Expect(err).ToNot(HaveOccurred())
		cr := &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-merge"},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    "clustertemplate-merge",
				TemplateVersion: "v1.0.0",
				TemplateParameters: runtime.RawExtension{Raw: fmt.Appendf(nil, `{
					"%s": "exampleCluster",
					"%s": "local-123",
					"%s": %s
				}`, constants.TemplateParamNodeClusterName,
					constants.TemplateParamOCloudSiteId,
					constants.TemplateParamClusterInstance,
					paramsJSON)},
			},
		}
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GetClusterTemplateRefName("clustertemplate-merge", "v1.0.0"),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				TemplateDefaults: provisioningv1alpha1.TemplateDefaults{
					ClusterInstanceDefaults: ciDefaultsCm,
				},
			},
		}
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: ciDefaultsCm, Namespace: ctNamespace},
			Data: map[string]string{
				ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey: defaultsYAML,
			},
		}
		c := fakeclient.GetFakeClientFromObjects([]client.Object{cr, ct, cm}...)
		return &provisioningRequestReconcilerTask{
			logger:       logger,
			client:       c,
			object:       cr,
			clusterInput: &clusterInput{},
			ctDetails:    &clusterTemplateDetails{namespace: ctNamespace},
		}
	}

	extractClusterInstanceInput := func(t *provisioningRequestReconcilerTask) map[string]any {
		input, err := provisioningv1alpha1.ExtractMatchingInput(
			t.object.Spec.TemplateParameters.Raw, constants.TemplateParamClusterInstance)
		Expect(err).ToNot(HaveOccurred())
		return input.(map[string]any)
	}

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("merges nodeGroups format and expands into flat nodes with mapped addresses", func() {
		defaults := `
clusterImageSetNameRef: "4.15"
pullSecretRef:
  name: "pull-secret"
templateRefs:
  - name: "ai-cluster-templates-v1"
    namespace: "siteconfig-operator"
nodeGroups:
- name: master
  role: master
  bootMode: UEFI
  nodeNetwork:
    interfaces:
    - name: eno1
      label: boot-interface
    config:
      interfaces:
      - name: eno1
        type: ethernet
        state: up
        ipv4:
          enabled: true
        ipv6:
          enabled: false
  templateRefs:
    - name: "ai-node-templates-v1"
      namespace: "siteconfig-operator"`

		params := `
clusterName: ng-cluster
nodeGroups:
- name: master
  nodeNetwork:
    config:
      dns-resolver:
        config:
          server:
          - "8.8.8.8"
  nodes:
  - hostName: master-1.example.com
    nodeNetwork:
      interfaces:
      - name: eno1
        addresses:
          ipv4:
          - "192.0.2.10/24"
  - hostName: master-2.example.com
    nodeNetwork:
      interfaces:
      - name: eno1
        addresses:
          ipv4:
          - "192.0.2.11/24"
`

		expectedYAML := `
clusterImageSetNameRef: "4.15"
pullSecretRef:
  name: "pull-secret"
templateRefs:
  - name: "ai-cluster-templates-v1"
    namespace: "siteconfig-operator"
clusterName: "ng-cluster"
nodes:
- hostName: "master-1.example.com"
  role: master
  bootMode: UEFI
  templateRefs:
    - name: "ai-node-templates-v1"
      namespace: "siteconfig-operator"
  nodeNetwork:
    interfaces:
    - name: eno1
      label: boot-interface
    config:
      dns-resolver:
        config:
          server:
          - "8.8.8.8"
      interfaces:
      - name: eno1
        type: ethernet
        state: up
        ipv4:
          enabled: true
          address:
          - ip: "192.0.2.10"
            prefix-length: 24
        ipv6:
          enabled: false
- hostName: "master-2.example.com"
  role: master
  bootMode: UEFI
  templateRefs:
    - name: "ai-node-templates-v1"
      namespace: "siteconfig-operator"
  nodeNetwork:
    interfaces:
    - name: eno1
      label: boot-interface
    config:
      dns-resolver:
        config:
          server:
          - "8.8.8.8"
      interfaces:
      - name: eno1
        type: ethernet
        state: up
        ipv4:
          enabled: true
          address:
          - ip: "192.0.2.11"
            prefix-length: 24
        ipv6:
          enabled: false
`

		task = newTask(defaults, params)
		merged, err := task.getMergedClusterInstanceData(ctx, ciDefaultsCm, extractClusterInstanceInput(task))
		Expect(err).ToNot(HaveOccurred())
		mergedYAML, err := yaml.Marshal(merged)
		Expect(err).ToNot(HaveOccurred())
		Expect(mergedYAML).To(MatchYAML(expectedYAML))
	})

	It("merges the legacy nodes format", func() {
		defaults := `
clusterImageSetNameRef: "4.15"
pullSecretRef:
  name: "pull-secret"
nodes:
- hostName: "node1"
  role: master
  bootMode: UEFI
  nodeNetwork:
    interfaces:
    - name: eno1
      label: boot-interface`

		params := `
clusterName: nodes-cluster
nodes:
- hostName: node1.example.com
  nodeNetwork:
    interfaces:
    - name: eno1
      macAddress: "aa:bb:cc:dd:ee:ff"
`

		expectedYAML := `
clusterImageSetNameRef: "4.15"
pullSecretRef:
  name: "pull-secret"
clusterName: "nodes-cluster"
nodes:
- hostName: "node1.example.com"
  role: master
  bootMode: UEFI
  nodeNetwork:
    interfaces:
    - name: eno1
      label: boot-interface
      macAddress: "aa:bb:cc:dd:ee:ff"
`

		task = newTask(defaults, params)
		merged, err := task.getMergedClusterInstanceData(ctx, ciDefaultsCm, extractClusterInstanceInput(task))
		Expect(err).ToNot(HaveOccurred())
		mergedYAML, err := yaml.Marshal(merged)
		Expect(err).ToNot(HaveOccurred())
		Expect(mergedYAML).To(MatchYAML(expectedYAML))
	})

	It("rejects mixed nodeGroups and nodes formats", func() {
		defaultsWithNodeGroups := `
nodeGroups:
- name: master
  role: master`

		paramsWithNodes := `
clusterName: mixed
nodes:
- hostName: node1.example.com
`

		task = newTask(defaultsWithNodeGroups, paramsWithNodes)
		_, err := task.getMergedClusterInstanceData(ctx, ciDefaultsCm, extractClusterInstanceInput(task))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must both use"))

		defaultsWithNodes := `
nodes:
- hostName: "node1"
  role: master`

		paramsWithNodeGroups := `
clusterName: mixed
nodeGroups:
- name: master
  nodes:
  - hostName: m1
`

		task = newTask(defaultsWithNodes, paramsWithNodeGroups)
		_, err = task.getMergedClusterInstanceData(ctx, ciDefaultsCm, extractClusterInstanceInput(task))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must both use"))
	})

	It("rejects when only one side uses nodeGroups", func() {
		defaults := `
clusterName: from-defaults`

		params := `
clusterName: only-pr
nodeGroups:
- name: master
  nodes:
  - hostName: m1
`

		task = newTask(defaults, params)
		_, err := task.getMergedClusterInstanceData(ctx, ciDefaultsCm, extractClusterInstanceInput(task))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must both use"))
	})

	It("rejects a ProvisioningRequest group not defined in the defaults", func() {
		defaults := `
nodeGroups:
- name: master
  role: master`

		params := `
clusterName: unknown-group
nodeGroups:
- name: master
  nodes:
  - hostName: m1
- name: worker
  nodes:
  - hostName: w1
`

		task = newTask(defaults, params)
		_, err := task.getMergedClusterInstanceData(ctx, ciDefaultsCm, extractClusterInstanceInput(task))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`name "worker" at path "nodeGroups" in source does not match any destination entry`))
	})

	It("rejects duplicate group names in the ProvisioningRequest", func() {
		defaults := `
nodeGroups:
- name: master
  role: master`

		params := `
clusterName: dup-groups
nodeGroups:
- name: master
  nodes:
  - hostName: m1
- name: master
  nodes:
  - hostName: m2
`

		task = newTask(defaults, params)
		_, err := task.getMergedClusterInstanceData(ctx, ciDefaultsCm, extractClusterInstanceInput(task))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`duplicate name "master" at path "nodeGroups" in source`))
	})

	It("rejects a defaults group missing from the ProvisioningRequest", func() {
		defaults := `
nodeGroups:
- name: master
  role: master
- name: worker
  role: worker`

		params := `
clusterName: missing-group
nodeGroups:
- name: master
  nodes:
  - hostName: m1
`

		task = newTask(defaults, params)
		_, err := task.getMergedClusterInstanceData(ctx, ciDefaultsCm, extractClusterInstanceInput(task))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`node group "worker" must define at least one node in clusterInstanceParameters`))
	})

	It("rejects a group-level interface not defined in the defaults", func() {
		defaults := `
nodeGroups:
- name: master
  role: master
  nodeNetwork:
    interfaces:
    - name: eno1
      label: boot-interface`

		params := `
clusterName: unknown-iface
nodeGroups:
- name: master
  nodeNetwork:
    interfaces:
    - name: eth99
  nodes:
  - hostName: master-1.example.com
`

		task = newTask(defaults, params)
		_, err := task.getMergedClusterInstanceData(ctx, ciDefaultsCm, extractClusterInstanceInput(task))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`name "eth99" at path "nodeGroups[].nodeNetwork.interfaces" in source does not match any destination entry`))
	})
})
