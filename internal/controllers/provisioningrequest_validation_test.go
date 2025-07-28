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
*/

package controllers

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
})
