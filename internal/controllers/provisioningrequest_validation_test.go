package controllers

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/yaml"
)

var testSchema = `
properties:
  additionalNTPSources:
    items:
      type: string
    type: array
  apiVIPs:
    items:
      type: string
    maxItems: 2
    type: array
  baseDomain:
    type: string
  clusterName:
    description: ClusterName is the name of the cluster.
    type: string
  extraLabels:
    additionalProperties:
      additionalProperties:
        type: string
      type: object
    type: object
  extraAnnotations:
    additionalProperties:
      additionalProperties:
        type: string
      type: object
    type: object
  ingressVIPs:
    items:
      type: string
    maxItems: 2
    type: array
  machineNetwork:
    description: MachineNetwork is the list of IP address pools for machines.
    items:
      description: MachineNetworkEntry is a single IP address block for
        node IP blocks.
      properties:
        cidr:
          type: string
      required:
      - cidr
      type: object
    type: array
  nodes:
    items:
      description: NodeSpec
      properties:
        extraAnnotations:
          additionalProperties:
            additionalProperties:
              type: string
            type: object
          description: Additional node-level annotations to be applied
            to the rendered templates
          type: object
        hostName:
          description: Hostname is the desired hostname for the host
          type: string
        nodeLabels:
          additionalProperties:
            type: string
          type: object
        nodeNetwork:
          properties:
            config:
              type: object
              x-kubernetes-preserve-unknown-fields: true
            interfaces:
              items:
                properties:
                  macAddress:
                    type: string
                  name:
                    type: string
                type: object
              minItems: 1
              type: array
          type: object
      required:
      - hostName
      type: object
    type: array
  serviceNetwork:
    items:
      properties:
        cidr:
          type: string
      required:
      - cidr
      type: object
    type: array
  sshPublicKey:
    type: string
required:
- clusterName
- nodes
type: object
`

var _ = Describe("disallowUnknownFieldsInSchema", func() {
	var schemaMap map[string]any

	BeforeEach(func() {
		err := yaml.Unmarshal([]byte(testSchema), &schemaMap)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should add 'additionalProperties': false to all objects with 'properties'", func() {
		var expected = `
additionalProperties: false
properties:
  additionalNTPSources:
    items:
      type: string
    type: array
  apiVIPs:
    items:
      type: string
    maxItems: 2
    type: array
  baseDomain:
    type: string
  clusterName:
    description: ClusterName is the name of the cluster.
    type: string
  extraLabels:
    additionalProperties:
      additionalProperties:
        type: string
      type: object
    type: object
  extraAnnotations:
    additionalProperties:
      additionalProperties:
        type: string
      type: object
    type: object
  ingressVIPs:
    items:
      type: string
    maxItems: 2
    type: array
  machineNetwork:
    description: MachineNetwork is the list of IP address pools for machines.
    items:
      description: MachineNetworkEntry is a single IP address block for
        node IP blocks.
      additionalProperties: false
      properties:
        cidr:
          type: string
      required:
      - cidr
      type: object
    type: array
  nodes:
    items:
      description: NodeSpec
      additionalProperties: false
      properties:
        extraAnnotations:
          additionalProperties:
            additionalProperties:
              type: string
            type: object
          description: Additional node-level annotations to be applied
            to the rendered templates
          type: object
        hostName:
          description: Hostname is the desired hostname for the host
          type: string
        nodeLabels:
          additionalProperties:
            type: string
          type: object
        nodeNetwork:
          additionalProperties: false
          properties:
            config:
              type: object
              x-kubernetes-preserve-unknown-fields: true
            interfaces:
              items:
                additionalProperties: false
                properties:
                  macAddress:
                    type: string
                  name:
                    type: string
                type: object
              minItems: 1
              type: array
          type: object
      required:
      - hostName
      type: object
    type: array
  serviceNetwork:
    items:
      additionalProperties: false
      properties:
        cidr:
          type: string
      required:
      - cidr
      type: object
    type: array
  sshPublicKey:
    type: string
required:
- clusterName
- nodes
type: object
`
		// Call the function
		disallowUnknownFieldsInSchema(schemaMap)

		var expectedSchema map[string]any
		err := yaml.Unmarshal([]byte(expected), &expectedSchema)
		Expect(err).ToNot(HaveOccurred())
		Expect(schemaMap).To(Equal(expectedSchema))
	})
})

var _ = Describe("validateJsonAgainstJsonSchema", func() {

	var schemaMap map[string]any

	BeforeEach(func() {
		err := yaml.Unmarshal([]byte(testSchema), &schemaMap)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Return error if required field is missing", func() {
		// The required field nodes[0].hostName is missing.
		input := `
clusterName: sno1
machineNetwork:
  - cidr: 192.0.2.0/24
serviceNetwork:
  - cidr: 172.30.0.0/16
nodes:
  - nodeNetwork:
      interfaces:
        - macAddress: 00:00:00:01:20:30
        - macAddress: 00:00:00:01:20:31
      config:
        dns-resolver:
          config:
            server:
              - 192.0.2.22
        routes:
          config:
            - next-hop-address: 192.0.2.254
        interfaces:
          - ipv6:
              enabled: false
            ipv4:
              enabled: true
              address:
                - ip: 192.0.2.12
                  prefix-length: 24
`
		inputMap := make(map[string]any)
		err := yaml.Unmarshal([]byte(input), &inputMap)
		Expect(err).ToNot(HaveOccurred())

		err = validateJsonAgainstJsonSchema(schemaMap, inputMap)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(
			ContainSubstring("invalid input: nodes.0: hostName is required"))
	})

	It("Return error if field is of different type", func() {
		// ExtraLabels - ManagedCluster is a map instead of list.
		input := `
clusterName: sno1
machineNetwork:
  - cidr: 192.0.2.0/24
serviceNetwork:
  - cidr: 172.30.0.0/16
extraLabels:
  ManagedCluster:
    - label1
    - label2
nodes:
  - hostName: sno1.example.com
    nodeNetwork:
      interfaces:
        - macAddress: 00:00:00:01:20:30
        - macAddress: 00:00:00:01:20:31
      config:
        dns-resolver:
          config:
            server:
              - 192.0.2.22
        routes:
          config:
            - next-hop-address: 192.0.2.254
        interfaces:
          - ipv6:
              enabled: false
            ipv4:
              enabled: true
              address:
                - ip: 192.0.2.12
                  prefix-length: 24
`

		inputMap := make(map[string]any)
		err := yaml.Unmarshal([]byte(input), &inputMap)
		Expect(err).ToNot(HaveOccurred())

		err = validateJsonAgainstJsonSchema(schemaMap, inputMap)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(
			ContainSubstring("invalid input: extraLabels.ManagedCluster: Invalid type. Expected: object, given: array"))
	})

	It("Returns success if optional field with required fields is missing", func() {
		// The optional field serviceNetwork has required field - cidr, but it's missing completely.
		input := `
clusterName: sno1
machineNetwork:
  - cidr: 192.0.2.0/24
nodes:
  - hostName: sno1.example.com
    nodeNetwork:
      interfaces:
        - macAddress: 00:00:00:01:20:30
        - macAddress: 00:00:00:01:20:31
      config:
        dns-resolver:
          config:
            server:
              - 192.0.2.22
        routes:
          config:
            - next-hop-address: 192.0.2.254
        interfaces:
          - ipv6:
              enabled: false
            ipv4:
              enabled: true
              address:
                - ip: 192.0.2.12
                  prefix-length: 24
`

		inputMap := make(map[string]any)
		err := yaml.Unmarshal([]byte(input), &inputMap)
		Expect(err).ToNot(HaveOccurred())

		err = validateJsonAgainstJsonSchema(schemaMap, inputMap)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Return error if unknown field is provided", func() {
		// clusterType is not in the schema
		input := `
clusterType: SNO
clusterName: sno1
nodes:
  - hostName: sno1.example.com
    nodeNetwork:
      interfaces:
        - macAddress: 00:00:00:01:20:30
        - macAddress: 00:00:00:01:20:31
      config:
        dns-resolver:
          config:
            server:
              - 192.0.2.22
        routes:
          config:
            - next-hop-address: 192.0.2.254
        interfaces:
          - ipv6:
              enabled: false
            ipv4:
              enabled: true
              address:
                - ip: 192.0.2.12
                  prefix-length: 24
`

		schemaMap["additionalProperties"] = false
		inputMap := make(map[string]any)
		err := yaml.Unmarshal([]byte(input), &inputMap)
		Expect(err).ToNot(HaveOccurred())

		err = validateJsonAgainstJsonSchema(schemaMap, inputMap)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(
			ContainSubstring("Additional property clusterType is not allowed"))
	})
})

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
			ctNamespace:  "",
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
