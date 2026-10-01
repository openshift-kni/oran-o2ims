/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
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

var _ = Describe("DisallowUnknownFieldsInSchema", func() {
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
		DisallowUnknownFieldsInSchema(schemaMap)

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

	// When the ClusterTemplate schema does not declare cpuArchitecture,
	// additionalProperties:false rejects a ProvisioningRequest that sets it -
	// the original bug that blocked ARM (aarch64) deployments.
	It("rejects cpuArchitecture when the schema does not declare it", func() {
		DisallowUnknownFieldsInSchema(schemaMap)
		input := `
clusterName: sno1
cpuArchitecture: aarch64
machineNetwork:
  - cidr: 192.0.2.0/24
nodes:
  - hostName: sno1.example.com
`
		inputMap := make(map[string]any)
		Expect(yaml.Unmarshal([]byte(input), &inputMap)).To(Succeed())

		err := ValidateJsonAgainstJsonSchema(schemaMap, inputMap)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(
			ContainSubstring("Additional property cpuArchitecture is not allowed"))
	})

	// Declaring cpuArchitecture as a cluster-level property (as the reference
	// schemas now do) lets the field pass validation.
	It("accepts cpuArchitecture once the schema declares it", func() {
		props := schemaMap["properties"].(map[string]any)
		props["cpuArchitecture"] = map[string]any{
			"type": "string",
			"enum": []any{"x86_64", "aarch64", "multi"},
		}
		DisallowUnknownFieldsInSchema(schemaMap)
		input := `
clusterName: sno1
cpuArchitecture: aarch64
machineNetwork:
  - cidr: 192.0.2.0/24
nodes:
  - hostName: sno1.example.com
`
		inputMap := make(map[string]any)
		Expect(yaml.Unmarshal([]byte(input), &inputMap)).To(Succeed())

		Expect(ValidateJsonAgainstJsonSchema(schemaMap, inputMap)).To(Succeed())
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
				TemplateDefaults: TemplateDefaults{
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
				TemplateDefaults: TemplateDefaults{
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
		Expect(retCt.Spec.TemplateDefaults.ClusterInstanceDefaults).To(Equal(ciDefaultsCm))
		Expect(retCt.Spec.TemplateDefaults.PolicyTemplateDefaults).To(Equal(ptDefaultsCm))
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
	  },
	  "hwMgmtParameters": {
		"description": "hwMgmtParameters allows overriding hardware management defaults.",
		"type": "object",
		"properties": {
		  "hardwareProvisioningTimeout": {
			"type": "string"
		  },
		  "nodeGroupData": {
			"type": "array",
			"items": {
			  "type": "object",
			  "required": ["name"],
			  "properties": {
				"name": {"type": "string"},
				"role": {"type": "string"},
				"hwProfile": {"type": "string"},
				"resourcePoolId": {"type": "string"},
				"resourceSelector": {"type": "object", "additionalProperties": {"type": "string"}}
			  }
			}
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
		{
			name: "empty schema",
			args: args{
				mainSchema: []byte{},
				node:       "anything",
			},
			wantSubSchema: nil,
			wantErr:       false,
		},
		{
			name: "invalid JSON",
			args: args{
				mainSchema: []byte(`not json`),
				node:       "anything",
			},
			wantSubSchema: nil,
			wantErr:       true,
		},
		{
			name: "missing properties section",
			args: args{
				mainSchema: []byte(`{"type": "object"}`),
				node:       "anything",
			},
			wantSubSchema: nil,
			wantErr:       true,
		},
		{
			name: "subSchema not found",
			args: args{
				mainSchema: []byte(testTemplate),
				node:       "nonExistentKey",
			},
			wantSubSchema: nil,
			wantErr:       true,
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

func TestIsErrSubSchemaNotFound(t *testing.T) {
	if !IsErrSubSchemaNotFound(ErrSubSchemaNotFound) {
		t.Error("expected true for ErrSubSchemaNotFound")
	}
	if !IsErrSubSchemaNotFound(fmt.Errorf("wrapped: %w", ErrSubSchemaNotFound)) {
		t.Error("expected true for wrapped ErrSubSchemaNotFound")
	}
	if !IsErrSubSchemaNotFound(fmt.Errorf("wrapped: %w", fmt.Errorf("wrapped: %w", ErrSubSchemaNotFound))) {
		t.Error("expected true for wrapped wrapped ErrSubSchemaNotFound")
	}
	if IsErrSubSchemaNotFound(fmt.Errorf("some other error")) {
		t.Error("expected false for unrelated error")
	}
	if IsErrSubSchemaNotFound(errors.New(ErrSubSchemaNotFound.Error())) {
		t.Error("expected false for not an ErrSubSchemaNotFound error")
	}
	if IsErrSubSchemaNotFound(nil) {
		t.Error("expected false for nil")
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

func TestValidateActiveUpgradeUpdateEquivalentEmptyInputs(t *testing.T) {
	tests := []struct {
		name   string
		oldRaw string
		newRaw string
	}{
		{
			name:   "remove empty workerPoolUpgrade",
			oldRaw: `{"upgradeParameters": {"clusterVersion": {"workerPoolUpgrade": {}}}}`,
			newRaw: `{"upgradeParameters":{}}`,
		},
		{
			name:   "add empty workerPoolUpgrade",
			oldRaw: `{"upgradeParameters":{}}`,
			newRaw: `{"upgradeParameters": {"clusterVersion": {"workerPoolUpgrade": {}}}}`,
		},
		{
			name:   "add timeout to empty parameters",
			oldRaw: "",
			newRaw: `{"upgradeParameters": {"clusterVersion": {"clusterUpgradeTimeout": "3h"}}}`,
		},
		{
			name:   "add empty upgrade parameters to unrelated input",
			oldRaw: `{"clusterInstanceParameters":{}}`,
			newRaw: `{"clusterInstanceParameters":{},"upgradeParameters":{}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldPr := &ProvisioningRequest{
				Spec: ProvisioningRequestSpec{
					TemplateName:       "template",
					TemplateVersion:    "v1",
					TemplateParameters: runtime.RawExtension{Raw: []byte(tt.oldRaw)},
				},
				Status: ProvisioningRequestStatus{Conditions: []metav1.Condition{{
					Type:   string(PRconditionTypes.UpgradeCompleted),
					Status: metav1.ConditionFalse,
					Reason: string(CRconditionReasons.InProgress),
				}}},
			}
			newPr := oldPr.DeepCopy()
			newPr.Spec.TemplateParameters.Raw = []byte(tt.newRaw)
			if err := validateActiveUpgradeUpdate(oldPr, newPr, &ClusterTemplate{}); err != nil {
				t.Fatalf("empty upgrade input should be equivalent to omission: %v", err)
			}
		})
	}
}

var _ = Describe("active upgrade input validation", func() {
	var oldPr, newPr *ProvisioningRequest
	var clusterTemplate *ClusterTemplate

	BeforeEach(func() {
		clusterTemplate = &ClusterTemplate{}
		oldPr = &ProvisioningRequest{
			Spec: ProvisioningRequestSpec{
				TemplateName:    "clustertemplate-a",
				TemplateVersion: "v1.0.1",
				TemplateParameters: runtime.RawExtension{Raw: []byte(`{
  "upgradeParameters": {
    "clusterVersion": {
      "clusterUpgradeTimeout": "2h",
      "workerPoolUpgrade": {
        "strategy": "Custom",
        "stages": [
          {
            "name": "canary",
            "pools": [
              "worker-a"
            ]
          },
          {
            "name": "rest",
            "pools": [
              "worker-b"
            ]
          }
        ]
      }
    }
  }
}`)},
			},
			Status: ProvisioningRequestStatus{
				Conditions: []metav1.Condition{{
					Type: string(PRconditionTypes.UpgradeCompleted), Status: metav1.ConditionFalse,
					Reason: string(CRconditionReasons.InProgress),
				}},
			},
		}
		newPr = oldPr.DeepCopy()
		oldPr.Status.Extensions.ClusterDetails = &ClusterDetails{
			ClusterUpgradeStatus: &ClusterUpgradeStatus{
				WorkerPoolUpgrade: &WorkerPoolUpgradeStatus{
					Strategy: constants.WorkerPoolUpgradeStrategyCustom,
					Stages: []WorkerPoolUpgradeStage{
						{Name: "canary", Pools: []string{"worker-a"}},
						{Name: "rest", Pools: []string{"worker-b"}},
					},
				},
			},
		}
	})

	for _, reason := range []ConditionReason{
		CRconditionReasons.InProgress,
		CRconditionReasons.Unknown,
		CRconditionReasons.AwaitingStageAuthorization,
	} {
		Context(fmt.Sprintf("with UpgradeCompleted reason %s", reason), func() {
			BeforeEach(func() {
				oldPr.Status.Conditions[0].Reason = string(reason)
			})

			It("should reject plan changes while active", func() {
				newPr.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(`{
  "upgradeParameters": {
    "clusterVersion": {
      "workerPoolUpgrade": {
        "strategy": "Custom",
        "stages": [
          {
            "name": "changed",
            "pools": [
              "worker-a"
            ]
          }
        ]
      }
    }
  }
}`)}
				err := validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("cannot be changed"))
			})

			It("should reject switching ClusterTemplate while active", func() {
				newPr.Spec.TemplateVersion = "v2.0.0"
				err := validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("switching ClusterTemplate"))
			})

			DescribeTable("should reject changes to other upgrade parameters while active",
				func(changedParameter string) {
					newPr.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(`{
				"upgradeParameters": {
					"clusterVersion": {
						"clusterUpgradeTimeout": "2h",
						"workerPoolUpgrade": {
							"strategy": "Custom",
							"stages": [
								{"name": "canary", "pools": ["worker-a"]},
								{"name": "rest", "pools": ["worker-b"]}
							]
						},
					` + changedParameter + `
					}
				}
			}`)}
					err := validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("upgradeParameters cannot be changed"))
				},
				Entry("cvSpec", `"cvSpec":{"channel":"stable-4.22"}`),
				Entry("intermediateVersion", `"intermediateVersion":"4.21.3"`),
			)

			It("should allow a timeout update", func() {
				newPr.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(`{
  "upgradeParameters": {
    "clusterVersion": {
      "clusterUpgradeTimeout": "3h",
      "workerPoolUpgrade": {
        "strategy": "Custom",
        "stages": [
          {
            "name": "canary",
            "pools": [
              "worker-a"
            ]
          },
          {
            "name": "rest",
            "pools": [
              "worker-b"
            ]
          }
        ]
      }
    }
  }
}`)}
				Expect(validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)).To(Succeed())
			})

			It("should allow forward stage authorization only while awaiting", func() {
				newPr.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(`{
  "upgradeParameters": {
    "clusterVersion": {
      "clusterUpgradeTimeout": "2h",
      "workerPoolUpgrade": {
        "strategy": "Custom",
        "stages": [
          {
            "name": "canary",
            "pools": [
              "worker-a"
            ]
          },
          {
            "name": "rest",
            "pools": [
              "worker-b"
            ]
          }
        ],
        "upgradeThrough": "canary"
      }
    }
  }
}`)}
				err := validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
				if reason == CRconditionReasons.AwaitingStageAuthorization {
					Expect(err).ToNot(HaveOccurred())
				} else {
					Expect(err).To(MatchError(ContainSubstring(
						"clusterVersion.workerPoolUpgrade.upgradeThrough can only change while awaiting stage authorization")))
				}
			})

			It("should reject a non-string upgradeThrough", func() {
				newPr.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(
					`{"upgradeParameters": {"clusterVersion": {"workerPoolUpgrade": {"upgradeThrough": 1}}}}`)}
				err := validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
				Expect(err).To(MatchError(ContainSubstring("workerPoolUpgrade.upgradeThrough must be a string")))
			})

			It("should reject a non-string default upgradeThrough", func() {
				newPr.Spec.TemplateParameters = oldPr.Spec.TemplateParameters
				clusterTemplate.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{Raw: []byte(
					`{"clusterVersion": {"workerPoolUpgrade": {"upgradeThrough": 1}}}`)}
				err := validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
				Expect(err).To(MatchError(ContainSubstring("workerPoolUpgrade.upgradeThrough must be a string")))
			})

		})
	}

	Context("while awaiting Custom stage authorization", func() {
		BeforeEach(func() {
			oldPr.Status.Conditions[0].Reason = string(CRconditionReasons.AwaitingStageAuthorization)
		})

		It("should reject moving upgradeThrough behind persisted authorization", func() {
			oldPr.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.
				WorkerPoolUpgrade.UpgradeThrough = "rest"
			newPr.Spec.TemplateParameters = oldPr.Spec.TemplateParameters
			err := validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
			Expect(err).To(MatchError(ContainSubstring(
				"clusterVersion.workerPoolUpgrade.upgradeThrough cannot move backwards")))
		})

		It("should report the nested path for an unknown stage", func() {
			var params map[string]any
			Expect(json.Unmarshal(oldPr.Spec.TemplateParameters.Raw, &params)).To(Succeed())
			pool := params["upgradeParameters"].(map[string]any)["clusterVersion"].(map[string]any)["workerPoolUpgrade"].(map[string]any)
			pool["upgradeThrough"] = "missing"
			updated, err := json.Marshal(params)
			Expect(err).ToNot(HaveOccurred())
			newPr.Spec.TemplateParameters.Raw = updated
			err = validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
			Expect(err).To(MatchError(ContainSubstring(
				"clusterVersion.workerPoolUpgrade.upgradeThrough refers to unknown stage \"missing\"")))
		})

		It("should reject a rapid backward edit before authorization is persisted", func() {
			oldPr.Spec.TemplateParameters.Raw = []byte(`{
  "upgradeParameters": {
    "clusterVersion": {
      "clusterUpgradeTimeout": "2h",
      "workerPoolUpgrade": {
        "strategy": "Custom",
        "stages": [
          {
            "name": "canary",
            "pools": [
              "worker-a"
            ]
          },
          {
            "name": "rest",
            "pools": [
              "worker-b"
            ]
          }
        ],
        "upgradeThrough": "rest"
      }
    }
  }
}`)
			newPr.Spec.TemplateParameters.Raw = []byte(`{
  "upgradeParameters": {
    "clusterVersion": {
      "clusterUpgradeTimeout": "2h",
      "workerPoolUpgrade": {
        "strategy": "Custom",
        "stages": [
          {
            "name": "canary",
            "pools": [
              "worker-a"
            ]
          },
          {
            "name": "rest",
            "pools": [
              "worker-b"
            ]
          }
        ],
        "upgradeThrough": "canary"
      }
    }
  }
}`)
			err := validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
			Expect(err).To(MatchError(ContainSubstring(
				"clusterVersion.workerPoolUpgrade.upgradeThrough cannot move backwards from \"rest\" to \"canary\"")))
		})

		It("should reject an explicitly empty upgradeThrough over a template default", func() {
			oldPr.Status.Extensions.ClusterDetails.ClusterUpgradeStatus.
				WorkerPoolUpgrade.UpgradeThrough = "canary"
			clusterTemplate.Spec.TemplateDefaults.UpgradeDefaults = runtime.RawExtension{Raw: []byte(
				`{"clusterVersion": {"workerPoolUpgrade": {"upgradeThrough": "rest"}}}`)}
			newPr.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(`{
  "upgradeParameters": {
    "clusterVersion": {
      "clusterUpgradeTimeout": "2h",
      "workerPoolUpgrade": {
        "strategy": "Custom",
        "stages": [
          {
            "name": "canary",
            "pools": [
              "worker-a"
            ]
          },
          {
            "name": "rest",
            "pools": [
              "worker-b"
            ]
          }
        ],
        "upgradeThrough": ""
      }
    }
  }
}`)}

			err := validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
			Expect(err).To(MatchError(ContainSubstring("cannot move backwards")))
		})
	})

	DescribeTable("should reject malformed active upgrade input",
		func(raw, message string) {
			newPr.Spec.TemplateParameters.Raw = []byte(raw)
			err := validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)
			Expect(err).To(MatchError(ContainSubstring(message)))
		},
		Entry("invalid JSON", `{`, "failed to decode templateParameters"),
		Entry("null template parameters", `null`, "templateParameters must be an object"),
		Entry("non-object upgrade parameters", `{"upgradeParameters":null}`,
			"templateParameters.upgradeParameters must be an object"),
		Entry("non-object worker pool upgrade", `{"upgradeParameters": {"clusterVersion": {"workerPoolUpgrade": []}}}`,
			"templateParameters.upgradeParameters.clusterVersion.workerPoolUpgrade must be an object"),
	)

	DescribeTable("should allow plan changes outside an active upgrade",
		func(reason ConditionReason) {
			oldPr.Status.Conditions[0].Reason = string(reason)
			newPr.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(`{
  "upgradeParameters": {
    "clusterVersion": {
      "workerPoolUpgrade": {
        "strategy": "Parallel"
      }
    }
  }
}`)}
			Expect(validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)).To(Succeed())
		},
		Entry("Pending", CRconditionReasons.Pending),
		Entry("PreconditionChecksFailed", CRconditionReasons.PreconditionChecksFailed),
	)

	It("should allow authorizing the final stage before the upgrade starts", func() {
		oldPr.Status.Conditions[0].Reason = string(CRconditionReasons.Pending)
		newPr.Spec.TemplateParameters.Raw = []byte(`{
  "upgradeParameters": {
    "clusterVersion": {
      "workerPoolUpgrade": {
        "strategy": "Custom",
        "stages": [
          {
            "name": "canary",
            "pools": [
              "worker-a"
            ]
          },
          {
            "name": "rest",
            "pools": [
              "worker-b"
            ]
          }
        ],
        "upgradeThrough": "rest"
      }
    }
  }
}`)
		Expect(validateActiveUpgradeUpdate(oldPr, newPr, clusterTemplate)).To(Succeed())
	})
})

var _ = Describe("WorkerPoolUpgradeStatus.StageIndex", func() {
	It("returns no authorized stage for a missing status", func() {
		var status *WorkerPoolUpgradeStatus
		Expect(status.StageIndex("canary")).To(Equal(-1))
	})

	DescribeTable("StageIndex",
		func(name string, expected int) {
			status := &WorkerPoolUpgradeStatus{Stages: []WorkerPoolUpgradeStage{
				{Name: "canary"}, {Name: "remaining"},
			}}
			Expect(status.StageIndex(name)).To(Equal(expected))
		},
		Entry("no stage authorized", "", -1),
		Entry("first stage", "canary", 0),
		Entry("last stage", "remaining", 1),
		Entry("unknown stage", "missing", -1),
	)
})
