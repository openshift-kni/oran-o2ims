/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
					Templates: Templates{
						ClusterInstanceDefaults: "clusterinstance-defaults-v1",
						PolicyTemplateDefaults:  "policytemplate-defaults-v1",
						HwTemplate:              "hardwaretemplate-v1",
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
						Templates: Templates{
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
						Templates: Templates{
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
						Templates: Templates{
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
})
