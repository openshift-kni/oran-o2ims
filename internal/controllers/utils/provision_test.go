/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	"github.com/openshift-kni/oran-o2ims/test/fakeclient"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

var _ = Describe("ClusterIsReadyForPolicyConfig", func() {
	var (
		ctx         context.Context
		fakeClient  client.Client
		clusterName = "cluster-1"
	)

	BeforeEach(func() {
		// Define the needed resources.
		crs := []client.Object{
			// Managed clusters
			&clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Spec: clusterv1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
				Status: clusterv1.ManagedClusterStatus{},
			},
		}

		fakeClient = fakeclient.GetFakeClientFromObjects(crs...)
	})

	It("returns false and no error if the cluster doesn't exist", func() {
		isReadyForConfig, err := ClusterIsReadyForPolicyConfig(ctx, fakeClient, "randomName")
		Expect(err).ToNot(HaveOccurred())
		Expect(isReadyForConfig).To(BeFalse())
	})

	It("returns false if cluster is either not available, hubAccepted or has not joined", func() {
		// Update the managedCluster cluster-1 to be available, joined and accepted.
		managedCluster1 := &clusterv1.ManagedCluster{}
		managedClusterExists, err := DoesK8SResourceExist(
			ctx, fakeClient, clusterName, "", managedCluster1)
		Expect(err).ToNot(HaveOccurred())
		Expect(managedClusterExists).To(BeTrue())
		SetStatusCondition(&managedCluster1.Status.Conditions,
			provisioningv1alpha1.ConditionType(clusterv1.ManagedClusterConditionAvailable),
			"ManagedClusterAvailable",
			metav1.ConditionFalse,
			"Managed cluster is available",
		)
		SetStatusCondition(&managedCluster1.Status.Conditions,
			provisioningv1alpha1.ConditionType(clusterv1.ManagedClusterConditionHubAccepted),
			"HubClusterAdminAccepted",
			metav1.ConditionTrue,
			"Accepted by hub cluster admin",
		)
		SetStatusCondition(&managedCluster1.Status.Conditions,
			provisioningv1alpha1.ConditionType(clusterv1.ManagedClusterConditionJoined),
			"ManagedClusterJoined",
			metav1.ConditionTrue,
			"Managed cluster joined",
		)
		Expect(fakeClient.Status().Update(ctx, managedCluster1)).To(Succeed())

		isReadyForConfig, err := ClusterIsReadyForPolicyConfig(ctx, fakeClient, clusterName)
		Expect(err).ToNot(HaveOccurred())
		Expect(isReadyForConfig).To(BeFalse())
	})

	It("returns true if cluster is available, hubAccepted and has joined", func() {
		managedCluster1 := &clusterv1.ManagedCluster{}
		managedClusterExists, err := DoesK8SResourceExist(
			ctx, fakeClient, clusterName, "", managedCluster1)
		Expect(err).ToNot(HaveOccurred())
		Expect(managedClusterExists).To(BeTrue())
		SetStatusCondition(&managedCluster1.Status.Conditions,
			provisioningv1alpha1.ConditionType(clusterv1.ManagedClusterConditionAvailable),
			"ManagedClusterAvailable",
			metav1.ConditionTrue,
			"Managed cluster is available",
		)
		SetStatusCondition(&managedCluster1.Status.Conditions,
			provisioningv1alpha1.ConditionType(clusterv1.ManagedClusterConditionHubAccepted),
			"HubClusterAdminAccepted",
			metav1.ConditionTrue,
			"Accepted by hub cluster admin",
		)
		SetStatusCondition(&managedCluster1.Status.Conditions,
			provisioningv1alpha1.ConditionType(clusterv1.ManagedClusterConditionJoined),
			"ManagedClusterJoined",
			metav1.ConditionTrue,
			"Managed cluster joined",
		)
		Expect(fakeClient.Status().Update(ctx, managedCluster1)).To(Succeed())

		isReadyForConfig, err := ClusterIsReadyForPolicyConfig(ctx, fakeClient, clusterName)
		Expect(err).ToNot(HaveOccurred())
		Expect(isReadyForConfig).To(BeTrue())
	})
})

var _ = Describe("RemoveLabelFromInterfaces", func() {

	It("returns error for invalid nodes structure", func() {
		data := map[string]interface{}{
			"baseDomain": "example.sno",
			"nodes": []interface{}{
				42, // should be a map
			},
		}
		err := RemoveLabelFromInterfaces(data, constants.ClusterInstanceNodesKey)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("unexpected: invalid nodes data structure"))
	})

	It("returns error for failing to extract nodes interfaces", func() {
		data := map[string]interface{}{
			"baseDomain": "example.sno",
			"nodes": []interface{}{
				map[string]interface{}{
					"nodeNetwork": "value", // should be a map
				},
			},
		}
		err := RemoveLabelFromInterfaces(data, constants.ClusterInstanceNodesKey)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("failed to extract the interfaces from the node map"))
	})

	It("removes the labels from the nodes interfaces", func() {
		data := map[string]interface{}{
			"baseDomain": "example.sno",
			"nodes": []interface{}{
				map[string]interface{}{
					"nodeNetwork": map[string]interface{}{
						"interfaces": []interface{}{
							map[string]interface{}{
								"name":       "eth0",
								"label":      constants.BootInterfaceLabel,
								"macAddress": "some-mac",
							},
						},
					},
				},
			},
		}
		err := RemoveLabelFromInterfaces(data, constants.ClusterInstanceNodesKey)
		Expect(err).To(Not(HaveOccurred()))
		Expect(
			data["nodes"].([]interface{})[0].(map[string]interface{})["nodeNetwork"].(map[string]interface{})["interfaces"].([]interface{})[0].(map[string]interface{})).
			To(Equal(
				map[string]interface{}{
					"name":       "eth0",
					"macAddress": "some-mac",
				}),
			)
	})

	It("returns error for invalid nodeGroups structure", func() {
		data := map[string]any{
			"nodeGroups": []any{
				42, // should be a map
			},
		}
		err := RemoveLabelFromInterfaces(data, constants.ClusterInstanceNodeGroupsKey)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("unexpected: invalid nodeGroups data structure"))
	})

	It("returns error for failing to extract nodeGroups interfaces", func() {
		data := map[string]any{
			"nodeGroups": []any{
				map[string]any{
					"nodeNetwork": "value", // should be a map
				},
			},
		}
		err := RemoveLabelFromInterfaces(data, constants.ClusterInstanceNodeGroupsKey)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("failed to extract the interfaces from the node map"))
	})

	It("removes the labels from the nodeGroups interfaces", func() {
		data := map[string]any{
			"nodeGroups": []any{
				map[string]any{
					"nodeNetwork": map[string]any{
						"interfaces": []any{
							map[string]any{
								"name":       "ens3f0",
								"label":      constants.BootInterfaceLabel,
								"macAddress": "some-mac",
							},
						},
					},
				},
			},
		}
		err := RemoveLabelFromInterfaces(data, constants.ClusterInstanceNodeGroupsKey)
		Expect(err).ToNot(HaveOccurred())
		Expect(
			data["nodeGroups"].([]any)[0].(map[string]any)["nodeNetwork"].(map[string]any)["interfaces"].([]any)[0].(map[string]any)).
			To(Equal(
				map[string]any{
					"name":       "ens3f0",
					"macAddress": "some-mac",
				}),
			)
	})
})

var _ = Describe("ValidateNodeGroupsNames", func() {
	hwMgmt := map[string]struct{}{"master": {}, "worker": {}}

	It("accepts well-formed groups", func() {
		err := ValidateNodeGroupsNames(map[string]any{
			"nodeGroups": []any{
				map[string]any{"name": "master", "role": "master"},
				map[string]any{"name": "worker", "role": "worker"},
			},
		}, hwMgmt)
		Expect(err).ToNot(HaveOccurred())
	})

	It("rejects nested nodes including empty list", func() {
		err := ValidateNodeGroupsNames(map[string]any{
			"nodeGroups": []any{
				map[string]any{"name": "master", "nodes": []any{}},
			},
		}, hwMgmt)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal(
			`nodeGroups[0] must omit "nodes"; per-host identity belongs only in the ProvisioningRequest`))
	})

	It("rejects missing name", func() {
		err := ValidateNodeGroupsNames(map[string]any{
			"nodeGroups": []any{
				map[string]any{"role": "master"},
			},
		}, hwMgmt)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal(`nodeGroups[0].name must be a non-empty string`))
	})

	It("rejects duplicate names", func() {
		err := ValidateNodeGroupsNames(map[string]any{
			"nodeGroups": []any{
				map[string]any{"name": "master"},
				map[string]any{"name": "master"},
			},
		}, hwMgmt)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal(`duplicate nodeGroups name "master"`))
	})

	It("rejects name not in allowed hardware node groups", func() {
		err := ValidateNodeGroupsNames(map[string]any{
			"nodeGroups": []any{
				map[string]any{"name": "extra"},
			},
		}, hwMgmt)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal(
			`nodeGroups name "extra" does not match any hardware node group in the nodeGroupData`))
	})

	It("skips membership check when allowedGroupNames is nil", func() {
		err := ValidateNodeGroupsNames(map[string]any{
			"nodeGroups": []any{
				map[string]any{"name": "any-group"},
			},
		}, nil)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("ValidateDefaultInterfaces", func() {
	It("returns error if nodes interfaces are missing labels", func() {
		err := ValidateDefaultInterfaces(map[string]any{
			"nodes": []any{
				map[string]any{
					"hostName": "node1",
					"nodeNetwork": map[string]any{
						"interfaces": []any{
							map[string]any{"name": "eno1"},
						},
					},
				},
			},
		}, constants.ClusterInstanceNodesKey)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("'label' is missing for interface: eno1"))
	})

	It("returns no error if nodes interfaces have labels", func() {
		err := ValidateDefaultInterfaces(map[string]any{
			"nodes": []any{
				map[string]any{
					"hostName": "node1",
					"nodeNetwork": map[string]any{
						"interfaces": []any{
							map[string]any{"name": "eno1", "label": "boot-interface"},
						},
					},
				},
			},
		}, constants.ClusterInstanceNodesKey)
		Expect(err).ToNot(HaveOccurred())
	})

	It("returns error if nodeGroups interfaces are missing labels", func() {
		err := ValidateDefaultInterfaces(map[string]any{
			"nodeGroups": []any{
				map[string]any{
					"name": "master",
					"nodeNetwork": map[string]any{
						"interfaces": []any{
							map[string]any{"name": "ens3f0"},
						},
					},
				},
			},
		}, constants.ClusterInstanceNodeGroupsKey)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("'label' is missing for interface: ens3f0"))
	})

	It("returns no error if nodeGroups interfaces have labels", func() {
		err := ValidateDefaultInterfaces(map[string]any{
			"nodeGroups": []any{
				map[string]any{
					"name": "master",
					"nodeNetwork": map[string]any{
						"interfaces": []any{
							map[string]any{"name": "ens3f0", "label": "boot-interface"},
						},
					},
				},
			},
		}, constants.ClusterInstanceNodeGroupsKey)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("ValidateConfigmapSchemaAgainstClusterInstanceCRD", func() {
	var (
		ctx        context.Context
		fakeClient client.Client
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeClient = fakeclient.GetFakeClientFromObjects()
	})

	It("returns an error when the ClusterInstance CRD does not exist", func() {
		err := ValidateConfigmapSchemaAgainstClusterInstanceCRD(
			ctx, fakeClient, nil, constants.ClusterInstanceNodesKey)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).
			To(Equal(
				fmt.Sprintf(
					"failed to obtain the %s.%s CRD: customresourcedefinitions.apiextensions.k8s.io \"%s.%s\" not found",
					ClusterInstanceCrdName, siteconfig.Group, ClusterInstanceCrdName, siteconfig.Group)))
	})

	Context("The ClusterInstance CRD does exists", func() {
		It("returns error if the ClusterInstance CRD is missing its versions", func() {
			clusterInstanceCRD, err := BuildTestClusterInstanceCRD(TestClusterInstanceSpecNoVersions)
			Expect(err).ToNot(HaveOccurred())
			Expect(fakeClient.Create(ctx, clusterInstanceCRD)).To(Succeed())

			err = ValidateConfigmapSchemaAgainstClusterInstanceCRD(
				ctx, fakeClient, nil, constants.ClusterInstanceNodesKey)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("failed to obtain the versions of the %s.%s CRD", ClusterInstanceCrdName, siteconfig.Group)))
		})

		It("returns error if no version of the ClusterInstance CRD is served", func() {
			clusterInstanceCRD, err := BuildTestClusterInstanceCRD(TestClusterInstanceSpecServedFalse)
			Expect(err).ToNot(HaveOccurred())
			Expect(fakeClient.Create(ctx, clusterInstanceCRD)).To(Succeed())

			err = ValidateConfigmapSchemaAgainstClusterInstanceCRD(
				ctx, fakeClient, nil, constants.ClusterInstanceNodesKey)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("no version served & stored in the %s.%s CRD ", ClusterInstanceCrdName, siteconfig.Group)))
		})

		It("returns error if the nodes-format ConfigMap schema does not match the ClusterInstance CRD schema", func() {
			clusterInstanceCRD, err := BuildTestClusterInstanceCRD(TestClusterInstanceSpecOk)
			Expect(err).ToNot(HaveOccurred())
			Expect(fakeClient.Create(ctx, clusterInstanceCRD)).To(Succeed())

			data := map[string]any{
				"clusterImageSetNameRef": "4.15",
				"pullSecretRef": map[string]any{
					"should-be-name": "pull-secret",
				},
			}
			err = ValidateConfigmapSchemaAgainstClusterInstanceCRD(
				ctx, fakeClient, data, constants.ClusterInstanceNodesKey)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(
				"ConfigMap does not match the ClusterInstance schema: " +
					"invalid input: pullSecretRef: Additional property should-be-name is not allowed"))
		})

		It("returns no error if the nodes-format ConfigMap schema matches the ClusterInstance CRD schema", func() {
			clusterInstanceCRD, err := BuildTestClusterInstanceCRD(TestClusterInstanceSpecOk)
			Expect(err).ToNot(HaveOccurred())
			Expect(fakeClient.Create(ctx, clusterInstanceCRD)).To(Succeed())

			data := map[string]any{
				"clusterImageSetNameRef": "4.15",
				"pullSecretRef": map[string]any{
					"name": "pull-secret",
				},
			}
			err = ValidateConfigmapSchemaAgainstClusterInstanceCRD(
				ctx, fakeClient, data, constants.ClusterInstanceNodesKey)
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns no error if the nodeGroups-format ConfigMap schema matches the ClusterInstance CRD schema", func() {
			clusterInstanceCRD, err := BuildTestClusterInstanceCRD(TestClusterInstanceSpecOk)
			Expect(err).ToNot(HaveOccurred())
			Expect(fakeClient.Create(ctx, clusterInstanceCRD)).To(Succeed())

			data := map[string]any{
				"baseDomain": "example.com",
				"nodeGroups": []any{
					map[string]any{
						"role": "master",
						"nodeNetwork": map[string]any{
							"interfaces": []any{
								map[string]any{"name": "ens3f0"},
							},
						},
					},
				},
			}
			Expect(ValidateConfigmapSchemaAgainstClusterInstanceCRD(
				ctx, fakeClient, data, constants.ClusterInstanceNodeGroupsKey)).To(Succeed())
		})

		It("returns error if the nodeGroups-format ConfigMap schema does not match the ClusterInstance CRD schema", func() {
			clusterInstanceCRD, err := BuildTestClusterInstanceCRD(TestClusterInstanceSpecOk)
			Expect(err).ToNot(HaveOccurred())
			Expect(fakeClient.Create(ctx, clusterInstanceCRD)).To(Succeed())

			data := map[string]any{
				"nodeGroups": []any{
					map[string]any{
						"notAField": true,
						"nodeNetwork": map[string]any{
							"interfaces": []any{
								map[string]any{"name": "ens3f0"},
							},
						},
					},
				},
			}
			err = ValidateConfigmapSchemaAgainstClusterInstanceCRD(
				ctx, fakeClient, data, constants.ClusterInstanceNodeGroupsKey)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(
				"ConfigMap does not match the ClusterInstance schema: " +
					"invalid input: nodeGroups.0: Additional property notAField is not allowed"))
		})
	})
})

func TestExtractSchemaRequired(t *testing.T) {
	type args struct {
		mainSchema []byte
	}
	tests := []struct {
		name    string
		args    args
		want    []string
		wantErr bool
	}{
		{
			name: "ok",
			args: args{
				mainSchema: []byte(`{
					"required": [
					  "nodeClusterName",
					  "oCloudSiteId",
					  "policyTemplateParameters",
					  "clusterInstanceParameters"
					]
				  }`),
			},
			want:    []string{"nodeClusterName", "oCloudSiteId", "policyTemplateParameters", "clusterInstanceParameters"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractSchemaRequired(tt.args.mainSchema)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractSchemaRequired() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractSchemaRequired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRootPolicyMatchesClusterTemplate(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		ctRef       string
		want        bool
	}{
		{
			name:        "nil annotations returns false",
			annotations: nil,
			ctRef:       "cluster-template.v4.20.0-1",
			want:        false,
		},
		{
			name:        "empty annotations returns false",
			annotations: map[string]string{},
			ctRef:       "cluster-template.v4.20.0-1",
			want:        false,
		},
		{
			name:        "missing key returns false",
			annotations: map[string]string{"other": "cluster-template.v4.20.0-1"},
			ctRef:       "cluster-template.v4.20.0-1",
			want:        false,
		},
		{
			name:        "empty value for key returns false",
			annotations: map[string]string{CTPolicyTemplatesAnnotation: ""},
			ctRef:       "cluster-template.v4.20.0-1",
			want:        false,
		},
		{
			name:        "ctRef empty returns false",
			annotations: map[string]string{CTPolicyTemplatesAnnotation: "cluster-template.v4.20.0-1"},
			ctRef:       "",
			want:        false,
		},
		{
			name:        "single exact match returns true",
			annotations: map[string]string{CTPolicyTemplatesAnnotation: "cluster-template.v4.20.0-1"},
			ctRef:       "cluster-template.v4.20.0-1",
			want:        true,
		},
		{
			name:        "single non-match returns false",
			annotations: map[string]string{CTPolicyTemplatesAnnotation: "cluster-template.v4.20.0-1"},
			ctRef:       "cluster-template.v4.20.0-2",
			want:        false,
		},
		{
			name:        "multiple values with spaces match returns true",
			annotations: map[string]string{CTPolicyTemplatesAnnotation: "cluster-template.v4.20.0-1, cluster-template.v4.20.0-2 , cluster-template.v4.20.0-3"},
			ctRef:       "cluster-template.v4.20.0-2",
			want:        true,
		},
		{
			name:        "multiple values without spaces match returns true",
			annotations: map[string]string{CTPolicyTemplatesAnnotation: "cluster-template.v4.20.0-1,cluster-template.v4.20.0-2,cluster-template.v4.20.0-3"},
			ctRef:       "cluster-template.v4.20.0-2",
			want:        true,
		},
		{
			name:        "case-insensitive match returns true",
			annotations: map[string]string{CTPolicyTemplatesAnnotation: "Cluster-Template.V4.20.0-2"},
			ctRef:       "cluster-template.v4.20.0-2",
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RootPolicyMatchesClusterTemplate(tt.annotations, tt.ctRef)
			if got != tt.want {
				t.Errorf("RootPolicyMatchesClusterTemplate() = %v, want %v", got, tt.want)
			}
		})
	}
}

var _ = Describe("ExpandNodeGroupsToNodes", func() {
	mustYAMLMap := func(s string) map[string]any {
		var out map[string]any
		Expect(yaml.Unmarshal([]byte(s), &out)).To(Succeed())
		return out
	}

	It("expands group shared fields onto per-node hosts with mapped addresses", func() {
		merged := mustYAMLMap(`
clusterName: from-defaults
nodeGroups:
- name: master
  role: master
  bootMode: UEFI
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
        ipv6:
          enabled: true
  nodes:
  - hostName: master-1.example.com
    nodeNetwork:
      interfaces:
      - name: eno1
        addresses:
          ipv4:
          - "192.0.2.10/24"
          ipv6:
          - "fd00:1:1::10/64"
  - hostName: master-2.example.com
    nodeNetwork:
      interfaces:
      - name: eno1
        addresses:
          ipv4:
          - "192.0.2.11/24"
          ipv6:
          - "fd00:1:1::11/64"
- name: worker
  role: worker
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
  nodes:
  - hostName: worker-1.example.com
    nodeNetwork:
      interfaces:
      - name: eno1
        addresses:
          ipv4:
          - "192.0.2.20/24"
- name: worker-cnf
  role: worker
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
  nodes:
  - hostName: worker-2.example.com
    nodeNetwork:
      interfaces:
      - name: eno1
        addresses:
          ipv4:
          - "192.0.2.21/24"
`)
		expected := `
clusterName: from-defaults
nodes:
- hostName: master-1.example.com
  role: master
  bootMode: UEFI
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
          enabled: true
          address:
          - ip: "fd00:1:1::10"
            prefix-length: 64
- hostName: master-2.example.com
  role: master
  bootMode: UEFI
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
          enabled: true
          address:
          - ip: "fd00:1:1::11"
            prefix-length: 64
- hostName: worker-1.example.com
  role: worker
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
          address:
          - ip: "192.0.2.20"
            prefix-length: 24
        ipv6:
          enabled: false
- hostName: worker-2.example.com
  role: worker
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
          address:
          - ip: "192.0.2.21"
            prefix-length: 24
        ipv6:
          enabled: false
`

		hostToGroupName, err := ExpandNodeGroupsToNodes(merged)
		Expect(err).ToNot(HaveOccurred())
		Expect(hostToGroupName).To(Equal(map[string]string{
			"master-1.example.com": "master",
			"master-2.example.com": "master",
			"worker-1.example.com": "worker",
			"worker-2.example.com": "worker-cnf",
		}))
		got, err := yaml.Marshal(merged)
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(MatchYAML(expected))
	})

	// A spec-level cpuArchitecture (e.g. aarch64 for an ARM cluster) must
	// survive nodeGroups expansion so it reaches the rendered ClusterInstance
	// spec instead of defaulting to x86_64.
	It("preserves a spec-level cpuArchitecture while expanding nodeGroups", func() {
		merged := mustYAMLMap(`
clusterName: sno-arm
cpuArchitecture: aarch64
nodeGroups:
- name: master
  role: master
  nodes:
  - hostName: sno-arm.example.com
`)
		hostToGroupName, err := ExpandNodeGroupsToNodes(merged)
		Expect(err).ToNot(HaveOccurred())
		Expect(hostToGroupName).To(Equal(map[string]string{
			"sno-arm.example.com": "master",
		}))
		// nodeGroups is replaced by a flat nodes list, but the cluster-level
		// cpuArchitecture is untouched.
		Expect(merged).ToNot(HaveKey("nodeGroups"))
		Expect(merged["cpuArchitecture"]).To(Equal("aarch64"))
		nodes, ok := merged["nodes"].([]any)
		Expect(ok).To(BeTrue())
		Expect(nodes).To(HaveLen(1))
	})

	It("applies last-wins on duplicate interface names in a node", func() {
		merged := mustYAMLMap(`
nodeGroups:
- name: master
  role: master
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
  nodes:
  - hostName: master-1.example.com
    nodeNetwork:
      interfaces:
      - name: eno1
        addresses:
          ipv4:
          - "192.0.2.10/24"
      - name: eno1
        addresses:
          ipv4:
          - "192.0.2.99/24"
`)
		expected := `
nodes:
- hostName: master-1.example.com
  role: master
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
          address:
          - ip: "192.0.2.99"
            prefix-length: 24
`

		_, err := ExpandNodeGroupsToNodes(merged)
		Expect(err).ToNot(HaveOccurred())
		got, err := yaml.Marshal(merged)
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(MatchYAML(expected))
	})

	It("rejects a per-node interface not defined on the group", func() {
		merged := mustYAMLMap(`
nodeGroups:
- name: master
  role: master
  nodeNetwork:
    interfaces:
    - name: eno1
      label: boot-interface
    config:
      interfaces:
      - name: eno1
        type: ethernet
  nodes:
  - hostName: master-1.example.com
    nodeNetwork:
      interfaces:
      - name: eth99
        addresses:
          ipv4:
          - "192.0.2.10/24"
`)
		_, err := ExpandNodeGroupsToNodes(merged)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`name "eth99" at path "nodeNetwork.interfaces" in source does not match any destination entry`))
	})

	It("rejects an interface with addresses that has no matching config interface", func() {
		merged := mustYAMLMap(`
nodeGroups:
- name: master
  role: master
  nodeNetwork:
    interfaces:
    - name: eno1
      label: boot-interface
    config:
      interfaces: []
  nodes:
  - hostName: master-1.example.com
    nodeNetwork:
      interfaces:
      - name: eno1
        addresses:
          ipv4:
          - "192.0.2.10/24"
`)
		_, err := ExpandNodeGroupsToNodes(merged)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`interface "eno1" has addresses but no matching entry in nodeNetwork.config.interfaces`))
	})

	It("rejects an invalid CIDR string", func() {
		merged := mustYAMLMap(`
nodeGroups:
- name: master
  role: master
  nodeNetwork:
    interfaces:
    - name: eno1
      label: boot-interface
    config:
      interfaces:
      - name: eno1
        type: ethernet
        ipv4:
          enabled: true
  nodes:
  - hostName: master-1.example.com
    nodeNetwork:
      interfaces:
      - name: eno1
        addresses:
          ipv4:
          - "192.0.2.10"
`)
		_, err := ExpandNodeGroupsToNodes(merged)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not a valid CIDR"))
	})

	It("skips a group with no nodes and expands the others", func() {
		merged := mustYAMLMap(`
nodeGroups:
- name: master
  role: master
  nodes:
  - hostName: master-1.example.com
- name: worker
  role: worker
`)
		expected := `
nodes:
- hostName: master-1.example.com
  role: master
`
		hostToGroupName, err := ExpandNodeGroupsToNodes(merged)
		Expect(err).ToNot(HaveOccurred())
		Expect(hostToGroupName).To(Equal(map[string]string{
			"master-1.example.com": "master",
		}))
		got, err := yaml.Marshal(merged)
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(MatchYAML(expected))
	})

	It("skips a group with an empty nodes list", func() {
		merged := mustYAMLMap(`
nodeGroups:
- name: master
  role: master
  nodes:
  - hostName: master-1.example.com
- name: worker
  role: worker
  nodes: []
`)
		expected := `
nodes:
- hostName: master-1.example.com
  role: master
`
		hostToGroupName, err := ExpandNodeGroupsToNodes(merged)
		Expect(err).ToNot(HaveOccurred())
		Expect(hostToGroupName).To(Equal(map[string]string{
			"master-1.example.com": "master",
		}))
		got, err := yaml.Marshal(merged)
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(MatchYAML(expected))
	})

	It("errors when every group defines no nodes", func() {
		merged := mustYAMLMap(`
nodeGroups:
- name: master
  role: master
- name: worker
  role: worker
  nodes: []
`)
		_, err := ExpandNodeGroupsToNodes(merged)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("at least one node is required across nodeGroups"))
	})

	It("expands an empty nodeGroups list to empty nodes", func() {
		merged := mustYAMLMap(`
clusterName: x
nodeGroups: []
`)
		expected := `
clusterName: x
nodes: []
`
		hostToGroupName, err := ExpandNodeGroupsToNodes(merged)
		Expect(err).ToNot(HaveOccurred())
		Expect(hostToGroupName).To(BeEmpty())
		got, err := yaml.Marshal(merged)
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(MatchYAML(expected))
	})

	It("rejects malformed nodeGroups", func() {
		_, err := ExpandNodeGroupsToNodes(map[string]any{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`"nodeGroups" is required for the nodeGroups format`))

		_, err = ExpandNodeGroupsToNodes(map[string]any{
			"nodeGroups": "not-an-array",
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`"nodeGroups" must be an array`))

		_, err = ExpandNodeGroupsToNodes(map[string]any{
			"nodeGroups": []any{"not-an-object"},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("nodeGroups[0] is not an object"))

		_, err = ExpandNodeGroupsToNodes(map[string]any{
			"nodeGroups": []any{map[string]any{"nodes": []any{}}},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`nodeGroups[0] is missing required field "name"`))

		_, err = ExpandNodeGroupsToNodes(map[string]any{
			"nodeGroups": []any{map[string]any{"name": "master", "nodes": "not-an-array"}},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`node group "master" nodes must be an array`))

		_, err = ExpandNodeGroupsToNodes(map[string]any{
			"nodeGroups": []any{map[string]any{"name": "master", "nodes": []any{"not-an-object"}}},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`node group "master" node[0] is not an object`))

		_, err = ExpandNodeGroupsToNodes(map[string]any{
			"nodeGroups": []any{map[string]any{"name": "master", "nodes": []any{map[string]any{}}}},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`node group "master" node[0] is missing required field "hostName"`))
	})

	It("rejects duplicate hostName within or across groups", func() {
		_, err := ExpandNodeGroupsToNodes(mustYAMLMap(`
nodeGroups:
- name: master
  role: master
  nodes:
  - hostName: master-1.example.com
  - hostName: master-1.example.com
`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal(`node group "master" node[1] has duplicate hostName "master-1.example.com" (already in node group "master")`))

		_, err = ExpandNodeGroupsToNodes(mustYAMLMap(`
nodeGroups:
- name: master
  role: master
  nodes:
  - hostName: shared.example.com
- name: worker
  role: worker
  nodes:
  - hostName: shared.example.com
`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal(`node group "worker" node[0] has duplicate hostName "shared.example.com" (already in node group "master")`))
	})
})

var _ = Describe("SchemaDefinesHwMgmtParameters", func() {
	It("should return true when schema defines hwMgmtParameters", func() {
		ct := &provisioningv1alpha1.ClusterTemplate{
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				TemplateParameterSchema: runtime.RawExtension{Raw: []byte("{\"properties\":{\"hwMgmtParameters\":{\"type\":\"object\"}}}")},
			},
		}
		Expect(SchemaDefinesHwMgmtParameters(ct)).To(BeTrue())
	})

	It("should return false when schema has no hwMgmtParameters", func() {
		ct := &provisioningv1alpha1.ClusterTemplate{
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				TemplateParameterSchema: runtime.RawExtension{Raw: []byte(`{
						"properties": {
							"nodeClusterName": {"type": "string"}
						}
					}`)},
			},
		}
		Expect(SchemaDefinesHwMgmtParameters(ct)).To(BeFalse())
	})

	It("should return false when schema is nil", func() {
		ct := &provisioningv1alpha1.ClusterTemplate{}
		Expect(SchemaDefinesHwMgmtParameters(ct)).To(BeFalse())
	})

	It("should return false when schema is invalid JSON", func() {
		ct := &provisioningv1alpha1.ClusterTemplate{
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				TemplateParameterSchema: runtime.RawExtension{Raw: []byte(`not json`)},
			},
		}
		Expect(SchemaDefinesHwMgmtParameters(ct)).To(BeFalse())
	})

	It("should return false when schema has no properties key", func() {
		ct := &provisioningv1alpha1.ClusterTemplate{
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				TemplateParameterSchema: runtime.RawExtension{Raw: []byte(`{"type": "object"}`)},
			},
		}
		Expect(SchemaDefinesHwMgmtParameters(ct)).To(BeFalse())
	})
})
