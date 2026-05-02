/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("ProvisioningRequestValidator", func() {
	var (
		ctx        context.Context
		validator  *provisioningRequestValidator
		oldPr      *ProvisioningRequest
		newPr      *ProvisioningRequest
		fakeClient client.Client
	)

	BeforeEach(func() {
		ctx = context.TODO()
		fakeClient = fake.NewClientBuilder().WithScheme(s).
			WithStatusSubresource(
				&ClusterTemplate{},
				&ProvisioningRequest{},
			).Build()

		validator = &provisioningRequestValidator{
			Client: fakeClient,
		}

		oldPr = &ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: "123e4567-e89b-12d3-a456-426614174000",
			},
			Spec: ProvisioningRequestSpec{
				Name:            "cluster-1",
				TemplateName:    "clustertemplate-a",
				TemplateVersion: "v1.0.1",
				TemplateParameters: runtime.RawExtension{Raw: []byte(`{
					"oCloudSiteId": "local-123",
					"nodeClusterName": "exampleCluster",
					"clusterInstanceParameters": {"additionalNTPSources": ["1.1.1.1"]},
					"policyTemplateParameters": {"sriov-network-vlan-1": "140"}
					}`)},
			},
		}

		// Copy the old PR to serve as a base for new PR
		newPr = oldPr.DeepCopy()
	})

	Describe("ValidateUpdate", func() {
		const (
			testClusterTemplateB = "clustertemplate-b"
			testVersionB         = "v1.0.2"
		)

		BeforeEach(func() {
			// Create a new ClusterTemplate
			newCt := &ClusterTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testClusterTemplateB + "." + testVersionB,
					Namespace: "default",
				},
				Spec: ClusterTemplateSpec{
					Name:       testClusterTemplateB,
					Version:    testVersionB,
					TemplateID: "57b39bda-ac56-4143-9b10-d1a71517d04f",
					TemplateDefaults: TemplateDefaults{
						ClusterInstanceDefaults: "clusterinstance-defaults-v1",
						PolicyTemplateDefaults:  "policytemplate-defaults-v1",
					},
					TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testTemplate)},
				},
				Status: ClusterTemplateStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(CTconditionTypes.Validated),
							Status: metav1.ConditionTrue,
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, newCt)).To(Succeed())
		})

		Context("when spec.templateName or spec.templateVersion is changed", func() {
			BeforeEach(func() {
				newPr.Spec.TemplateName = testClusterTemplateB
				newPr.Spec.TemplateVersion = testVersionB
			})

			It("should allow the change when the ProvisioningRequest is fulfilled", func() {
				newPr.Status.ProvisioningStatus.ProvisioningPhase = StateFulfilled
				_, err := validator.ValidateUpdate(ctx, oldPr, newPr)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should allow the change when the ProvisioningRequest is failed", func() {
				newPr.Status.ProvisioningStatus.ProvisioningPhase = StateFailed
				_, err := validator.ValidateUpdate(ctx, oldPr, newPr)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("ClusterInstance Immutable Fields Validation", func() {
			BeforeEach(func() {
				// Create a ClusterTemplate with schema for validation
				testSchema := `{
				"properties": {
					"nodeClusterName": {
						"type": "string"
					},
					"oCloudSiteId": {
						"type": "string"
					},
					"policyTemplateParameters": {
						"type": "object",
						"properties": {
							"sriov-network-vlan-1": {
								"type": "string"
							}
						}
					},
					"clusterInstanceParameters": {
						"type": "object",
						"properties": {
							"clusterName": {
								"type": "string"
							},
							"baseDomain": {
								"type": "string"
							},
							"nodes": {
								"type": "array",
								"items": {
									"type": "object",
									"properties": {
										"hostName": {
											"type": "string"
										}
									}
								}
							},
							"extraAnnotations": {
								"type": "object"
							},
							"extraLabels": {
								"type": "object"
							}
						}
					}
				},
				"required": [
					"nodeClusterName",
					"oCloudSiteId"
				],
				"type": "object"
			}`

				ct := &ClusterTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "clustertemplate-a.v1.0.1",
						Namespace: "default",
					},
					Spec: ClusterTemplateSpec{
						Name:       "clustertemplate-a",
						Version:    "v1.0.1",
						TemplateID: "test-template-id",
						TemplateDefaults: TemplateDefaults{
							ClusterInstanceDefaults: "defaults-v1",
							PolicyTemplateDefaults:  "policy-defaults-v1",
						},
						TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testSchema)},
					},
					Status: ClusterTemplateStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(CTconditionTypes.Validated),
								Status: metav1.ConditionTrue,
							},
						},
					},
				}
				Expect(fakeClient.Create(ctx, ct)).To(Succeed())

				// Base ProvisioningRequest with ClusterInstance parameters
				oldPr = &ProvisioningRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name: "123e4567-e89b-12d3-a456-426614174000",
					},
					Spec: ProvisioningRequestSpec{
						Name:            "cluster-1",
						TemplateName:    "clustertemplate-a",
						TemplateVersion: "v1.0.1",
						TemplateParameters: runtime.RawExtension{Raw: []byte(`{
					"oCloudSiteId": "local-123",
					"nodeClusterName": "exampleCluster",
					"clusterInstanceParameters": {
						"clusterName": "test-cluster",
						"baseDomain": "example.com",
						"nodes": [
							{
								"hostName": "node1.example.com"
							}
						],
						"extraAnnotations": {
							"ManagedCluster": {
								"key1": "value1"
							}
						},
						"extraLabels": {
							"ManagedCluster": {
								"label1": "labelvalue1"
							}
						}
					},
					"policyTemplateParameters": {"sriov-network-vlan-1": "140"}
				}`)},
					},
				}

				newPr = oldPr.DeepCopy()
			})

			Context("When ClusterProvisioned condition is InProgress", func() {
				BeforeEach(func() {
					newPr.Status.Conditions = []metav1.Condition{
						{
							Type:   string(PRconditionTypes.ClusterProvisioned),
							Status: metav1.ConditionFalse,
							Reason: string(CRconditionReasons.InProgress),
						},
					}
				})

				It("should reject ANY field changes during installation", func() {
					// Try to change baseDomain (immutable field)
					newPr.Spec.TemplateParameters.Raw = []byte(`{
				"oCloudSiteId": "local-123",
				"nodeClusterName": "exampleCluster",
				"clusterInstanceParameters": {
					"clusterName": "test-cluster",
					"baseDomain": "newdomain.com",
					"nodes": [
						{
							"hostName": "node1.example.com"
						}
					],
					"extraAnnotations": {
						"ManagedCluster": {
							"key1": "value1"
						}
					},
					"extraLabels": {
						"ManagedCluster": {
							"label1": "labelvalue1"
						}
					}
				},
				"policyTemplateParameters": {"sriov-network-vlan-1": "140"}
			}`)

					_, err := validator.ValidateUpdate(ctx, oldPr, newPr)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("disallowed during cluster installation"))
					Expect(err.Error()).To(ContainSubstring("baseDomain"))
				})

				It("should reject extraAnnotations changes during installation", func() {
					// Try to change extraAnnotations (normally allowed after completion)
					newPr.Spec.TemplateParameters.Raw = []byte(`{
				"oCloudSiteId": "local-123",
				"nodeClusterName": "exampleCluster",
				"clusterInstanceParameters": {
					"clusterName": "test-cluster",
					"baseDomain": "example.com",
					"nodes": [
						{
							"hostName": "node1.example.com"
						}
					],
					"extraAnnotations": {
						"ManagedCluster": {
							"key1": "changed-value"
						}
					},
					"extraLabels": {
						"ManagedCluster": {
							"label1": "labelvalue1"
						}
					}
				},
				"policyTemplateParameters": {"sriov-network-vlan-1": "140"}
			}`)

					_, err := validator.ValidateUpdate(ctx, oldPr, newPr)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("disallowed during cluster installation"))
				})

				It("should reject node scaling (adding nodes) during installation", func() {
					// Try to add a node
					newPr.Spec.TemplateParameters.Raw = []byte(`{
				"oCloudSiteId": "local-123",
				"nodeClusterName": "exampleCluster",
				"clusterInstanceParameters": {
					"clusterName": "test-cluster",
					"baseDomain": "example.com",
					"nodes": [
						{
							"hostName": "node1.example.com"
						},
						{
							"hostName": "node2.example.com"
						}
					],
					"extraAnnotations": {
						"ManagedCluster": {
							"key1": "value1"
						}
					},
					"extraLabels": {
						"ManagedCluster": {
							"label1": "labelvalue1"
						}
					}
				},
				"policyTemplateParameters": {"sriov-network-vlan-1": "140"}
			}`)

					_, err := validator.ValidateUpdate(ctx, oldPr, newPr)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("disallowed during cluster installation"))
					Expect(err.Error()).To(ContainSubstring("nodes.1"))
				})

				It("should reject node scaling (removing nodes) during installation", func() {
					// Start with 2 nodes in oldPr
					oldPr.Spec.TemplateParameters.Raw = []byte(`{
				"oCloudSiteId": "local-123",
				"nodeClusterName": "exampleCluster",
				"clusterInstanceParameters": {
					"clusterName": "test-cluster",
					"baseDomain": "example.com",
					"nodes": [
						{
							"hostName": "node1.example.com"
						},
						{
							"hostName": "node2.example.com"
						}
					],
					"extraAnnotations": {
						"ManagedCluster": {
							"key1": "value1"
						}
					}
				},
				"policyTemplateParameters": {"sriov-network-vlan-1": "140"}
			}`)

					// Try to remove a node
					newPr.Spec.TemplateParameters.Raw = []byte(`{
				"oCloudSiteId": "local-123",
				"nodeClusterName": "exampleCluster",
				"clusterInstanceParameters": {
					"clusterName": "test-cluster",
					"baseDomain": "example.com",
					"nodes": [
						{
							"hostName": "node1.example.com"
						}
					],
					"extraAnnotations": {
						"ManagedCluster": {
							"key1": "value1"
						}
					}
				},
				"policyTemplateParameters": {"sriov-network-vlan-1": "140"}
			}`)

					_, err := validator.ValidateUpdate(ctx, oldPr, newPr)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("disallowed during cluster installation"))
				})
			})

			Context("When ClusterProvisioned condition is Completed", func() {
				BeforeEach(func() {
					newPr.Status.Conditions = []metav1.Condition{
						{
							Type:   string(PRconditionTypes.ClusterProvisioned),
							Status: metav1.ConditionTrue,
							Reason: string(CRconditionReasons.Completed),
						},
					}
				})

				It("should allow extraAnnotations changes after completion", func() {
					newPr.Spec.TemplateParameters.Raw = []byte(`{
				"oCloudSiteId": "local-123",
				"nodeClusterName": "exampleCluster",
				"clusterInstanceParameters": {
					"clusterName": "test-cluster",
					"baseDomain": "example.com",
					"nodes": [
						{
							"hostName": "node1.example.com"
						}
					],
					"extraAnnotations": {
						"ManagedCluster": {
							"key1": "changed-value",
							"key2": "new-value"
						}
					},
					"extraLabels": {
						"ManagedCluster": {
							"label1": "labelvalue1"
						}
					}
				},
				"policyTemplateParameters": {"sriov-network-vlan-1": "140"}
			}`)

					_, err := validator.ValidateUpdate(ctx, oldPr, newPr)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should allow extraLabels changes after completion", func() {
					newPr.Spec.TemplateParameters.Raw = []byte(`{
				"oCloudSiteId": "local-123",
				"nodeClusterName": "exampleCluster",
				"clusterInstanceParameters": {
					"clusterName": "test-cluster",
					"baseDomain": "example.com",
					"nodes": [
						{
							"hostName": "node1.example.com"
						}
					],
					"extraAnnotations": {
						"ManagedCluster": {
							"key1": "value1"
						}
					},
					"extraLabels": {
						"ManagedCluster": {
							"label1": "changed-label",
							"label2": "new-label"
						}
					}
				},
				"policyTemplateParameters": {"sriov-network-vlan-1": "140"}
			}`)

					_, err := validator.ValidateUpdate(ctx, oldPr, newPr)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should allow node scaling (adding nodes) after completion", func() {
					// Add a node
					newPr.Spec.TemplateParameters.Raw = []byte(`{
				"oCloudSiteId": "local-123",
				"nodeClusterName": "exampleCluster",
				"clusterInstanceParameters": {
					"clusterName": "test-cluster",
					"baseDomain": "example.com",
					"nodes": [
						{
							"hostName": "node1.example.com"
						},
						{
							"hostName": "node2.example.com"
						}
					],
					"extraAnnotations": {
						"ManagedCluster": {
							"key1": "value1"
						}
					},
					"extraLabels": {
						"ManagedCluster": {
							"label1": "labelvalue1"
						}
					}
				},
				"policyTemplateParameters": {"sriov-network-vlan-1": "140"}
			}`)

					_, err := validator.ValidateUpdate(ctx, oldPr, newPr)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should allow node scaling (removing nodes) after completion", func() {
					// Start with 2 nodes
					oldPr.Spec.TemplateParameters.Raw = []byte(`{
				"oCloudSiteId": "local-123",
				"nodeClusterName": "exampleCluster",
				"clusterInstanceParameters": {
					"clusterName": "test-cluster",
					"baseDomain": "example.com",
					"nodes": [
						{
							"hostName": "node1.example.com"
						},
						{
							"hostName": "node2.example.com"
						}
					],
					"extraAnnotations": {
						"ManagedCluster": {
							"key1": "value1"
						}
					}
				},
				"policyTemplateParameters": {"sriov-network-vlan-1": "140"}
			}`)

					// Remove a node
					newPr.Spec.TemplateParameters.Raw = []byte(`{
				"oCloudSiteId": "local-123",
				"nodeClusterName": "exampleCluster",
				"clusterInstanceParameters": {
					"clusterName": "test-cluster",
					"baseDomain": "example.com",
					"nodes": [
						{
							"hostName": "node1.example.com"
						}
					],
					"extraAnnotations": {
						"ManagedCluster": {
							"key1": "value1"
						}
					}
				},
				"policyTemplateParameters": {"sriov-network-vlan-1": "140"}
			}`)

					_, err := validator.ValidateUpdate(ctx, oldPr, newPr)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should reject immutable field changes after completion", func() {
					// Try to change baseDomain (immutable)
					newPr.Spec.TemplateParameters.Raw = []byte(`{
				"oCloudSiteId": "local-123",
				"nodeClusterName": "exampleCluster",
				"clusterInstanceParameters": {
					"clusterName": "test-cluster",
					"baseDomain": "newdomain.com",
					"nodes": [
						{
							"hostName": "node1.example.com"
						}
					],
					"extraAnnotations": {
						"ManagedCluster": {
							"key1": "value1"
						}
					},
					"extraLabels": {
						"ManagedCluster": {
							"label1": "labelvalue1"
						}
					}
				},
				"policyTemplateParameters": {"sriov-network-vlan-1": "140"}
			}`)

					_, err := validator.ValidateUpdate(ctx, oldPr, newPr)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("only"))
					Expect(err.Error()).To(ContainSubstring("extraAnnotations"))
					Expect(err.Error()).To(ContainSubstring("extraLabels"))
					Expect(err.Error()).To(ContainSubstring("are allowed"))
					Expect(err.Error()).To(ContainSubstring("baseDomain"))
				})

				It("should allow combined changes (annotations + labels + node scaling)", func() {
					newPr.Spec.TemplateParameters.Raw = []byte(`{
				"oCloudSiteId": "local-123",
				"nodeClusterName": "exampleCluster",
				"clusterInstanceParameters": {
					"clusterName": "test-cluster",
					"baseDomain": "example.com",
					"nodes": [
						{
							"hostName": "node1.example.com"
						},
						{
							"hostName": "node2.example.com"
						}
					],
					"extraAnnotations": {
						"ManagedCluster": {
							"key1": "changed-value"
						}
					},
					"extraLabels": {
						"ManagedCluster": {
							"label1": "changed-label"
						}
					}
				},
				"policyTemplateParameters": {"sriov-network-vlan-1": "140"}
			}`)

					_, err := validator.ValidateUpdate(ctx, oldPr, newPr)
					Expect(err).NotTo(HaveOccurred())
				})
			})

			Context("When ClusterProvisioned condition is Unknown or Failed", func() {
				It("should allow all changes when condition is Unknown", func() {
					newPr.Status.Conditions = []metav1.Condition{
						{
							Type:   string(PRconditionTypes.ClusterProvisioned),
							Status: metav1.ConditionUnknown,
							Reason: string(CRconditionReasons.Unknown),
						},
					}

					// Change anything
					newPr.Spec.TemplateParameters.Raw = []byte(`{
				"oCloudSiteId": "local-123",
				"nodeClusterName": "exampleCluster",
				"clusterInstanceParameters": {
					"clusterName": "new-cluster-name",
					"baseDomain": "newdomain.com",
					"nodes": [
						{
							"hostName": "newnode.example.com"
						}
					]
				},
				"policyTemplateParameters": {"sriov-network-vlan-1": "999"}
			}`)

					_, err := validator.ValidateUpdate(ctx, oldPr, newPr)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should allow all changes when condition is Failed", func() {
					newPr.Status.Conditions = []metav1.Condition{
						{
							Type:   string(PRconditionTypes.ClusterProvisioned),
							Status: metav1.ConditionFalse,
							Reason: string(CRconditionReasons.Failed),
						},
					}

					// Change anything for recovery
					newPr.Spec.TemplateParameters.Raw = []byte(`{
				"oCloudSiteId": "local-123",
				"nodeClusterName": "exampleCluster",
				"clusterInstanceParameters": {
					"clusterName": "new-cluster-name",
					"baseDomain": "newdomain.com",
					"nodes": [
						{
							"hostName": "newnode.example.com"
						}
					]
				},
				"policyTemplateParameters": {"sriov-network-vlan-1": "999"}
			}`)

					_, err := validator.ValidateUpdate(ctx, oldPr, newPr)
					Expect(err).NotTo(HaveOccurred())
				})
			})
		})

		Context("When HardwareProvisioned condition is TimedOut or Failed", func() {
			BeforeEach(func() {
				// Create ClusterTemplate for validation
				ct := &ClusterTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "clustertemplate-a.v1.0.1",
						Namespace: "default",
					},
					Spec: ClusterTemplateSpec{
						Name:       "clustertemplate-a",
						Version:    "v1.0.1",
						TemplateID: "test-template-id",
						TemplateDefaults: TemplateDefaults{
							ClusterInstanceDefaults: "defaults-v1",
							PolicyTemplateDefaults:  "policy-defaults-v1",
						},
						TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testTemplate)},
					},
					Status: ClusterTemplateStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(CTconditionTypes.Validated),
								Status: metav1.ConditionTrue,
							},
						},
					},
				}
				Expect(fakeClient.Create(ctx, ct)).To(Succeed())

				// Create ClusterTemplate-b that will be used by tests
				newCt := &ClusterTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testClusterTemplateB + "." + testVersionB,
						Namespace: "default",
					},
					Spec: ClusterTemplateSpec{
						Name:       testClusterTemplateB,
						Version:    testVersionB,
						TemplateID: "test-template-id-b",
						TemplateDefaults: TemplateDefaults{
							ClusterInstanceDefaults: "defaults-v1",
							PolicyTemplateDefaults:  "policy-defaults-v1",
						},
						TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testTemplate)},
					},
					Status: ClusterTemplateStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(CTconditionTypes.Validated),
								Status: metav1.ConditionTrue,
							},
						},
					},
				}
				// Create only if it doesn't exist (to avoid conflicts between tests)
				existing := &ClusterTemplate{}
				if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(newCt), existing); err != nil {
					Expect(fakeClient.Create(ctx, newCt)).To(Succeed())
				}

				// Set HardwareProvisioned condition to TimedOut
				newPr.Status.Conditions = []metav1.Condition{
					{
						Type:   string(PRconditionTypes.HardwareProvisioned),
						Status: metav1.ConditionFalse,
						Reason: string(CRconditionReasons.TimedOut),
					},
				}
			})

			It("should reject spec changes when hardware provisioning has timed out", func() {
				// clustertemplate-b is already created in BeforeEach
				// Try to change TemplateName (a hardware provisioning-related field)
				newPr.Spec.TemplateName = testClusterTemplateB
				newPr.Spec.TemplateVersion = testVersionB

				_, err := validator.ValidateUpdate(ctx, oldPr, newPr)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("hardware provisioning has timed out or failed"))
				Expect(err.Error()).To(ContainSubstring("Spec changes are not allowed"))
				Expect(err.Error()).To(ContainSubstring("delete and recreate"))
			})

			It("should reject TemplateParameters changes when hardware provisioning has timed out", func() {
				// Try to change TemplateParameters
				newPr.Spec.TemplateParameters.Raw = []byte(`{
					"oCloudSiteId": "local-123",
					"nodeClusterName": "exampleCluster",
					"clusterInstanceParameters": {"additionalNTPSources": ["2.2.2.2"]},
					"policyTemplateParameters": {"sriov-network-vlan-1": "200"}
				}`)

				_, err := validator.ValidateUpdate(ctx, oldPr, newPr)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("hardware provisioning has timed out or failed"))
				Expect(err.Error()).To(ContainSubstring("Spec changes are not allowed"))
			})

			It("should allow non-spec changes when hardware provisioning has timed out", func() {
				// Only change metadata (like labels/annotations) - should be allowed
				newPr.Labels = map[string]string{"new-label": "new-value"}
				newPr.Annotations = map[string]string{"new-annotation": "new-value"}

				_, err := validator.ValidateUpdate(ctx, oldPr, newPr)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should reject spec changes when hardware provisioning has failed", func() {
				// Set HardwareProvisioned condition to Failed
				newPr.Status.Conditions = []metav1.Condition{
					{
						Type:   string(PRconditionTypes.HardwareProvisioned),
						Status: metav1.ConditionFalse,
						Reason: string(CRconditionReasons.Failed),
					},
				}

				// clustertemplate-b is already created in BeforeEach
				// Try to change TemplateName and TemplateVersion
				newPr.Spec.TemplateName = testClusterTemplateB
				newPr.Spec.TemplateVersion = testVersionB

				_, err := validator.ValidateUpdate(ctx, oldPr, newPr)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("hardware provisioning has timed out or failed"))
			})
		})
	})

	Describe("ValidateCreate - HwProfile Validation", func() {
		Context("when HwMgmtDefaults is empty in ClusterTemplate", func() {
			It("should skip hwProfile validation", func() {
				ct := &ClusterTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "clustertemplate-a.v1.0.1",
						Namespace: "default",
					},
					Spec: ClusterTemplateSpec{
						Name:       "clustertemplate-a",
						Version:    "v1.0.1",
						TemplateID: "test-template-id",
						TemplateDefaults: TemplateDefaults{
							ClusterInstanceDefaults: "defaults-v1",
							PolicyTemplateDefaults:  "policy-defaults-v1",
							HwMgmtDefaults:          HwMgmtDefaults{}, // empty = hw provisioning skipped
						},
						TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testTemplate)},
					},
					Status: ClusterTemplateStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(CTconditionTypes.Validated),
								Status: metav1.ConditionTrue,
							},
						},
					},
				}
				Expect(fakeClient.Create(ctx, ct)).To(Succeed())

				pr := &ProvisioningRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name: "123e4567-e89b-12d3-a456-426614174000",
					},
					Spec: ProvisioningRequestSpec{
						Name:            "cluster-1",
						TemplateName:    "clustertemplate-a",
						TemplateVersion: "v1.0.1",
						TemplateParameters: runtime.RawExtension{Raw: []byte(`{
							"oCloudSiteId": "local-123",
							"nodeClusterName": "exampleCluster",
							"clusterInstanceParameters": {"additionalNTPSources": ["1.1.1.1"]},
							"policyTemplateParameters": {"sriov-network-vlan-1": "140"}
						}`)},
					},
				}

				_, err := validator.ValidateCreate(ctx, pr)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when HwMgmtDefaults has nodeGroupData", func() {
			var ct *ClusterTemplate

			BeforeEach(func() {
				ct = &ClusterTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "clustertemplate-hw.v1.0.0",
						Namespace: "default",
					},
					Spec: ClusterTemplateSpec{
						Name:       "clustertemplate-hw",
						Version:    "v1.0.0",
						TemplateID: "test-hw-template-id",
						TemplateDefaults: TemplateDefaults{
							ClusterInstanceDefaults: "defaults-v1",
							PolicyTemplateDefaults:  "policy-defaults-v1",
							HwMgmtDefaults: HwMgmtDefaults{
								NodeGroupData: []hwmgmtv1alpha1.NodeGroupData{
									{Name: "controller", Role: "master"},
								},
							},
						},
						TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testTemplate)},
					},
					Status: ClusterTemplateStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(CTconditionTypes.Validated),
								Status: metav1.ConditionTrue,
							},
						},
					},
				}
				Expect(fakeClient.Create(ctx, ct)).To(Succeed())
			})

			It("should reject a PR referencing a non-existent HardwareProfile", func() {
				pr := &ProvisioningRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name: "123e4567-e89b-12d3-a456-426614174001",
					},
					Spec: ProvisioningRequestSpec{
						Name:            "cluster-hw-1",
						TemplateName:    "clustertemplate-hw",
						TemplateVersion: "v1.0.0",
						TemplateParameters: runtime.RawExtension{Raw: []byte(`{
							"oCloudSiteId": "local-123",
							"nodeClusterName": "exampleCluster",
							"clusterInstanceParameters": {"additionalNTPSources": ["1.1.1.1"]},
							"policyTemplateParameters": {"sriov-network-vlan-1": "140"},
							"hwMgmtParameters": {
								"nodeGroupData": [
									{"name": "controller", "hwProfile": "nonexistent-profile"}
								]
							}
						}`)},
					},
				}

				_, err := validator.ValidateCreate(ctx, pr)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("does not exist"))
			})

			It("should accept a PR referencing an existing HardwareProfile", func() {
				hwProfile := &hwmgmtv1alpha1.HardwareProfile{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "valid-profile",
						Namespace: "oran-o2ims",
					},
				}
				Expect(fakeClient.Create(ctx, hwProfile)).To(Succeed())

				pr := &ProvisioningRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name: "123e4567-e89b-12d3-a456-426614174002",
					},
					Spec: ProvisioningRequestSpec{
						Name:            "cluster-hw-2",
						TemplateName:    "clustertemplate-hw",
						TemplateVersion: "v1.0.0",
						TemplateParameters: runtime.RawExtension{Raw: []byte(`{
							"oCloudSiteId": "local-123",
							"nodeClusterName": "exampleCluster",
							"clusterInstanceParameters": {"additionalNTPSources": ["1.1.1.1"]},
							"policyTemplateParameters": {"sriov-network-vlan-1": "140"},
							"hwMgmtParameters": {
								"nodeGroupData": [
									{"name": "controller", "hwProfile": "valid-profile"}
								]
							}
						}`)},
					},
				}

				_, err := validator.ValidateCreate(ctx, pr)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should skip hwProfile validation when PR has no hwMgmtParameters at all", func() {
				pr := &ProvisioningRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name: "123e4567-e89b-12d3-a456-426614174004",
					},
					Spec: ProvisioningRequestSpec{
						Name:            "cluster-hw-4",
						TemplateName:    "clustertemplate-hw",
						TemplateVersion: "v1.0.0",
						TemplateParameters: runtime.RawExtension{Raw: []byte(`{
							"oCloudSiteId": "local-123",
							"nodeClusterName": "exampleCluster",
							"clusterInstanceParameters": {"additionalNTPSources": ["1.1.1.1"]},
							"policyTemplateParameters": {"sriov-network-vlan-1": "140"}
						}`)},
					},
				}

				_, err := validator.ValidateCreate(ctx, pr)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should skip validation when hwMgmtParameters has no nodeGroupData", func() {
				pr := &ProvisioningRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name: "123e4567-e89b-12d3-a456-426614174005",
					},
					Spec: ProvisioningRequestSpec{
						Name:            "cluster-hw-5",
						TemplateName:    "clustertemplate-hw",
						TemplateVersion: "v1.0.0",
						TemplateParameters: runtime.RawExtension{Raw: []byte(`{
							"oCloudSiteId": "local-123",
							"nodeClusterName": "exampleCluster",
							"clusterInstanceParameters": {"additionalNTPSources": ["1.1.1.1"]},
							"policyTemplateParameters": {"sriov-network-vlan-1": "140"},
							"hwMgmtParameters": {
								"hardwareProvisioningTimeout": "120m"
							}
						}`)},
					},
				}

				_, err := validator.ValidateCreate(ctx, pr)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should reject when hwMgmtParameters is not an object", func() {
				pr := &ProvisioningRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name: "123e4567-e89b-12d3-a456-426614174007",
					},
					Spec: ProvisioningRequestSpec{
						Name:            "cluster-hw-7",
						TemplateName:    "clustertemplate-hw",
						TemplateVersion: "v1.0.0",
						TemplateParameters: runtime.RawExtension{Raw: []byte(`{
							"oCloudSiteId": "local-123",
							"nodeClusterName": "exampleCluster",
							"clusterInstanceParameters": {"additionalNTPSources": ["1.1.1.1"]},
							"policyTemplateParameters": {"sriov-network-vlan-1": "140"},
							"hwMgmtParameters": "not-an-object"
						}`)},
					},
				}

				_, err := validator.ValidateCreate(ctx, pr)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Invalid type"))
			})

			It("should skip validation when nodeGroupData is not an array", func() {
				pr := &ProvisioningRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name: "123e4567-e89b-12d3-a456-426614174008",
					},
					Spec: ProvisioningRequestSpec{
						Name:            "cluster-hw-8",
						TemplateName:    "clustertemplate-hw",
						TemplateVersion: "v1.0.0",
						TemplateParameters: runtime.RawExtension{Raw: []byte(`{
							"oCloudSiteId": "local-123",
							"nodeClusterName": "exampleCluster",
							"clusterInstanceParameters": {"additionalNTPSources": ["1.1.1.1"]},
							"policyTemplateParameters": {"sriov-network-vlan-1": "140"},
							"hwMgmtParameters": {
								"nodeGroupData": "not-an-array"
							}
						}`)},
					},
				}

				_, err := validator.ValidateCreate(ctx, pr)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should skip validation when nodeGroupData entry has no hwProfile", func() {
				pr := &ProvisioningRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name: "123e4567-e89b-12d3-a456-426614174006",
					},
					Spec: ProvisioningRequestSpec{
						Name:            "cluster-hw-6",
						TemplateName:    "clustertemplate-hw",
						TemplateVersion: "v1.0.0",
						TemplateParameters: runtime.RawExtension{Raw: []byte(`{
							"oCloudSiteId": "local-123",
							"nodeClusterName": "exampleCluster",
							"clusterInstanceParameters": {"additionalNTPSources": ["1.1.1.1"]},
							"policyTemplateParameters": {"sriov-network-vlan-1": "140"},
							"hwMgmtParameters": {
								"nodeGroupData": [
									{"name": "controller"}
								]
							}
						}`)},
					},
				}

				_, err := validator.ValidateCreate(ctx, pr)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should skip validation when PR has no hwMgmtParameters", func() {
				pr := &ProvisioningRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name: "123e4567-e89b-12d3-a456-426614174003",
					},
					Spec: ProvisioningRequestSpec{
						Name:            "cluster-hw-3",
						TemplateName:    "clustertemplate-hw",
						TemplateVersion: "v1.0.0",
						TemplateParameters: runtime.RawExtension{Raw: []byte(`{
							"oCloudSiteId": "local-123",
							"nodeClusterName": "exampleCluster",
							"clusterInstanceParameters": {"additionalNTPSources": ["1.1.1.1"]},
							"policyTemplateParameters": {"sriov-network-vlan-1": "140"}
						}`)},
					},
				}

				_, err := validator.ValidateCreate(ctx, pr)
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	Describe("SchemaDefinesHwMgmtParameters", func() {
		It("should return true when schema defines hwMgmtParameters", func() {
			ct := &ClusterTemplate{
				Spec: ClusterTemplateSpec{
					TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testTemplate)},
				},
			}
			Expect(SchemaDefinesHwMgmtParameters(ct)).To(BeTrue())
		})

		It("should return false when schema has no hwMgmtParameters", func() {
			ct := &ClusterTemplate{
				Spec: ClusterTemplateSpec{
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
			ct := &ClusterTemplate{}
			Expect(SchemaDefinesHwMgmtParameters(ct)).To(BeFalse())
		})

		It("should return false when schema is invalid JSON", func() {
			ct := &ClusterTemplate{
				Spec: ClusterTemplateSpec{
					TemplateParameterSchema: runtime.RawExtension{Raw: []byte(`not json`)},
				},
			}
			Expect(SchemaDefinesHwMgmtParameters(ct)).To(BeFalse())
		})

		It("should return false when schema has no properties key", func() {
			ct := &ClusterTemplate{
				Spec: ClusterTemplateSpec{
					TemplateParameterSchema: runtime.RawExtension{Raw: []byte(`{"type": "object"}`)},
				},
			}
			Expect(SchemaDefinesHwMgmtParameters(ct)).To(BeFalse())
		})
	})

	Describe("ValidateTemplateInputMatchesSchema - hwMgmt stripping", func() {
		It("should not reject partial hwMgmtParameters overrides", func() {
			ct := &ClusterTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ct-hwmgmt-test.v1",
					Namespace: "default",
				},
				Spec: ClusterTemplateSpec{
					Name:       "ct-hwmgmt-test",
					Version:    "v1",
					TemplateID: "test-id",
					TemplateDefaults: TemplateDefaults{
						ClusterInstanceDefaults: "defaults-v1",
						PolicyTemplateDefaults:  "policy-defaults-v1",
						HwMgmtDefaults: HwMgmtDefaults{
							NodeGroupData: []hwmgmtv1alpha1.NodeGroupData{
								{Name: "controller", Role: "master"},
							},
						},
					},
					TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testTemplate)},
				},
				Status: ClusterTemplateStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(CTconditionTypes.Validated),
							Status: metav1.ConditionTrue,
						},
					},
				},
			}

			pr := &ProvisioningRequest{
				Spec: ProvisioningRequestSpec{
					TemplateParameters: runtime.RawExtension{Raw: []byte(`{
						"nodeClusterName": "test",
						"oCloudSiteId": "site-1",
						"clusterInstanceParameters": {"additionalNTPSources": ["1.1.1.1"]},
						"policyTemplateParameters": {"sriov-network-vlan-1": "140"},
						"hwMgmtParameters": {
							"nodeGroupData": [
								{"name": "controller", "hwProfile": "my-profile"}
							]
						}
					}`)},
				},
			}

			// Should not fail — hwMgmtParameters properties are stripped before schema validation
			err := pr.ValidateTemplateInputMatchesSchema(ct)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("ValidateDelete", func() {
		It("should return a warning about deletion time", func() {
			pr := &ProvisioningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: "123e4567-e89b-12d3-a456-426614174000",
				},
				Spec: ProvisioningRequestSpec{
					Name: "cluster-1",
				},
			}
			Expect(fakeClient.Create(ctx, pr)).To(Succeed())

			warnings, err := validator.ValidateDelete(ctx, pr)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(HaveLen(1))
			Expect(warnings[0]).To(ContainSubstring("several minutes"))
		})

		It("should block deletion when hardware configuration is in progress", func() {
			pr := &ProvisioningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: "123e4567-e89b-12d3-a456-426614174001",
				},
				Spec: ProvisioningRequestSpec{
					Name: "cluster-2",
				},
			}
			Expect(fakeClient.Create(ctx, pr)).To(Succeed())

			pr.Status.ProvisioningStatus.ProvisioningDetails = HardwareConfigInProgress
			pr.Status.ProvisioningStatus.ProvisioningPhase = StateProgressing
			Expect(fakeClient.Status().Update(ctx, pr)).To(Succeed())

			_, err := validator.ValidateDelete(ctx, pr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("hardware configuration is in progress"))
		})
	})
})
