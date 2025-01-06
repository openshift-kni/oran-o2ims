package v1alpha1

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

		err = ValidateJsonAgainstJsonSchema(schemaMap, inputMap)
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

		err = ValidateJsonAgainstJsonSchema(schemaMap, inputMap)
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

		err = ValidateJsonAgainstJsonSchema(schemaMap, inputMap)
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

		err = ValidateJsonAgainstJsonSchema(schemaMap, inputMap)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(
			ContainSubstring("Additional property clusterType is not allowed"))
	})
})

var _ = Describe("GetClusterTemplateRef", func() {
	var (
		ctx          context.Context
		fakeClient   client.Client
		pr           *ProvisioningRequest
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
		pr = &ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:       crName,
				Finalizers: []string{},
			},
			Spec: ProvisioningRequestSpec{
				TemplateName:    tName,
				TemplateVersion: tVersion,
			},
		}

		fakeClient = fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(pr).Build()
	})

	It("returns error if the referred ClusterTemplate is missing", func() {
		// Define the cluster template.
		ct := &ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-cluster-template-name.v1.0.0",
				Namespace: ctNamespace,
			},
			Spec: ClusterTemplateSpec{
				Name:       "other-cluster-template-name",
				Version:    "v1.0.0",
				TemplateID: "57b39bda-ac56-4143-9b10-d1a71517d04f",
				Templates: Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
				},
				TemplateParameterSchema: runtime.RawExtension{},
			},
		}

		Expect(fakeClient.Create(ctx, ct)).To(Succeed())

		retCt, err := pr.GetClusterTemplateRef(context.TODO(), fakeClient)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			fmt.Sprintf(
				"a valid ClusterTemplate (%s) does not exist in any namespace",
				fmt.Sprintf("%s.%s", tName, tVersion))))
		Expect(retCt).To(Equal((*ClusterTemplate)(nil)))
	})

	It("returns the referred ClusterTemplate if it exists", func() {
		// Define the cluster template.
		ctName := fmt.Sprintf("%s.%s", tName, tVersion)
		ct := &ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ctName,
				Namespace: ctNamespace,
			},
			Spec: ClusterTemplateSpec{
				Name:       tName,
				Version:    tVersion,
				TemplateID: "57b39bda-ac56-4143-9b10-d1a71517d04f",
				Templates: Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
				},
				TemplateParameterSchema: runtime.RawExtension{},
			},
			Status: ClusterTemplateStatus{
				Conditions: []metav1.Condition{
					{
						Reason: "Completed",
						Type:   "ClusterTemplateValidated",
						Status: metav1.ConditionTrue,
					},
				},
			},
		}

		Expect(fakeClient.Create(ctx, ct)).To(Succeed())

		retCt, err := pr.GetClusterTemplateRef(context.TODO(), fakeClient)
		Expect(err).ToNot(HaveOccurred())
		Expect(retCt.Name).To(Equal(ctName))
		Expect(retCt.Namespace).To(Equal(ctNamespace))
		Expect(retCt.Spec.Templates.ClusterInstanceDefaults).To(Equal(ciDefaultsCm))
		Expect(retCt.Spec.Templates.PolicyTemplateDefaults).To(Equal(ptDefaultsCm))
	})
})

const testTemplate = `{
	"properties": {
	  "nodeClusterName": {
		"type": "string"
	  },
	  "oCloudSiteId": {
		"type": "string"
	  },
	  "policyTemplateParameters": {
		"description": "policyTemplateParameters.",
		"properties": {
		  "sriov-network-vlan-1": {
			"type": "string"
		  },
		  "install-plan-approval": {
			"type": "string",
			"default": "Automatic"
		  }
		}
	  },
	  "clusterInstanceParameters": {
		"description": "clusterInstanceParameters.",
		"properties": {
		  "additionalNTPSources": {
			"description": "AdditionalNTPSources.",
			"items": {
			  "type": "string"
			},
			"type": "array"
		  }
		}
	  }
	},
	"required": [
	  "nodeClusterName",
	  "oCloudSiteId",
	  "policyTemplateParameters",
	  "clusterInstanceParameters"
	],
	"type": "object"
  }`

func TestExtractSubSchema(t *testing.T) {
	type args struct {
		mainSchema []byte
		node       string
	}
	tests := []struct {
		name          string
		args          args
		wantSubSchema map[string]any
		wantErr       bool
	}{
		{
			name: "ok",
			args: args{
				mainSchema: []byte(testTemplate),
				node:       "clusterInstanceParameters",
			},
			wantSubSchema: map[string]any{
				"description": "clusterInstanceParameters.",
				"properties": map[string]any{
					"additionalNTPSources": map[string]any{
						"description": "AdditionalNTPSources.",
						"items":       map[string]any{"type": "string"},
						"type":        "array",
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSubSchema, err := ExtractSubSchema(tt.args.mainSchema, tt.args.node)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractSubSchema() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotSubSchema, tt.wantSubSchema) {
				t.Errorf("ExtractSubSchema() = %v, want %v", gotSubSchema, tt.wantSubSchema)
			}
		})
	}
}

func TestExtractMatchingInput(t *testing.T) {
	type args struct {
		input        []byte
		subSchemaKey string
	}
	tests := []struct {
		name              string
		args              args
		wantMatchingInput any
		wantErr           bool
	}{
		{
			name: "ok - valid map input",
			args: args{
				input: []byte(`{
					  "clusterInstanceParameters": {
						  "additionalNTPSources": ["1.1.1.1"]
					  }
				  }`),
				subSchemaKey: "clusterInstanceParameters",
			},
			wantMatchingInput: map[string]any{
				"additionalNTPSources": []any{"1.1.1.1"},
			},
			wantErr: false,
		},
		{
			name: "ok - valid string input",
			args: args{
				input: []byte(`{
	"required": [
	  "nodeClusterName",
	  "oCloudSiteId",
	  "policyTemplateParameters",
	  "clusterInstanceParameters"
	]
  }`),
				subSchemaKey: "required",
			},
			wantMatchingInput: []any{"nodeClusterName", "oCloudSiteId", "policyTemplateParameters", "clusterInstanceParameters"},
			wantErr:           false,
		},
		{
			name: "ok - valid string input",
			args: args{
				input: []byte(`{
					  "oCloudSiteId": "local-123"
				  }`),
				subSchemaKey: "oCloudSiteId",
			},
			wantMatchingInput: "local-123",
			wantErr:           false,
		},
		{
			name: "error - missing subSchemaKey",
			args: args{
				input: []byte(`{
					  "clusterInstanceParameters": {
						  "additionalNTPSources": ["1.1.1.1"]
					  }
				  }`),
				subSchemaKey: "oCloudSiteId",
			},
			wantMatchingInput: nil,
			wantErr:           true,
		},
		{
			name: "error - invalid JSON",
			args: args{
				input:        []byte(`{invalid JSON}`),
				subSchemaKey: "clusterInstance",
			},
			wantMatchingInput: nil,
			wantErr:           true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMatchingInput, err := ExtractMatchingInput(tt.args.input, tt.args.subSchemaKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractMatchingInput() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotMatchingInput, tt.wantMatchingInput) {
				t.Errorf("ExtractMatchingInput() = %s, want %s", gotMatchingInput, tt.wantMatchingInput)
			}
		})
	}
}
