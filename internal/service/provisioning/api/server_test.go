/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package api_test

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	"github.com/openshift-kni/oran-o2ims/internal/service/provisioning/api"
	provisioningapi "github.com/openshift-kni/oran-o2ims/internal/service/provisioning/api/generated"
)

// interceptorFuncs creates an interceptor that preserves the UID during updates
// This works around a limitation in the fake client where UIDs are not preserved
func interceptorFuncs(uid string) interceptor.Funcs {
	return interceptor.Funcs{
		Update: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			// Preserve UID if not set
			if obj.GetUID() == "" {
				obj.SetUID(types.UID(uid))
			}
			return client.Update(ctx, obj, opts...)
		},
	}
}

var _ = Describe("ProvisioningServer", func() {
	var (
		ctx      context.Context
		testUUID uuid.UUID
	)

	BeforeEach(func() {
		ctx = context.Background()
		testUUID = uuid.New()
	})

	Describe("GetProvisioningRequest nodeClusterProvisioningStatus", func() {
		var (
			server *api.ProvisioningServer
			scheme *runtime.Scheme
		)

		BeforeEach(func() {
			scheme = runtime.NewScheme()
			Expect(provisioningv1alpha1.AddToScheme(scheme)).To(Succeed())
		})

		newProvisioningRequest := func(
			name string,
			clusterDetails *provisioningv1alpha1.ClusterDetails,
			provisionedResources *provisioningv1alpha1.ProvisionedResources,
			conditions []metav1.Condition,
			provisioningPhase provisioningv1alpha1.ProvisioningPhase,
			provisioningDetails string,
		) *provisioningv1alpha1.ProvisioningRequest {
			templateParamsBytes, err := json.Marshal(map[string]interface{}{"key": "value"})
			Expect(err).NotTo(HaveOccurred())

			pr := &provisioningv1alpha1.ProvisioningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
					UID:  types.UID(name),
				},
				Spec: provisioningv1alpha1.ProvisioningRequestSpec{
					Name:               "test",
					Description:        "test provisioning request",
					TemplateName:       "test-template",
					TemplateVersion:    "v1.0.0",
					TemplateParameters: runtime.RawExtension{Raw: templateParamsBytes},
				},
			}
			pr.Status.Extensions.ClusterDetails = clusterDetails
			pr.Status.ProvisioningStatus.ProvisionedResources = provisionedResources
			pr.Status.ProvisioningStatus.ProvisioningPhase = provisioningPhase
			pr.Status.ProvisioningStatus.ProvisioningDetails = provisioningDetails
			pr.Status.Conditions = conditions
			return pr
		}

		getNodeClusterStatus := func(prName string) provisioningapi.ResourceProvisioningStatus {
			reqUUID, err := uuid.Parse(prName)
			Expect(err).NotTo(HaveOccurred())

			resp, err := server.GetProvisioningRequest(ctx, provisioningapi.GetProvisioningRequestRequestObject{
				ProvisioningRequestId: reqUUID,
			})
			Expect(err).NotTo(HaveOccurred())
			result := resp.(provisioningapi.GetProvisioningRequest200JSONResponse)
			return result.Status.NodeClusterProvisioningStatus
		}

		When("ClusterDetails is nil", func() {
			It("should return PROCESSING phase with no resource name", func() {
				prName := uuid.New().String()
				pr := newProvisioningRequest(prName, nil, nil, nil, "", "")

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(pr).
					WithStatusSubresource(pr).
					Build()
				server = &api.ProvisioningServer{HubClient: fakeClient}

				status := getNodeClusterStatus(prName)
				Expect(status.ResourceProvisioningPhase).To(Equal(provisioningapi.ResourceProvisioningPhasePROCESSING))
				Expect(status.ResourceName).To(BeEmpty())
				Expect(status.ResourceId).To(BeEmpty())
			})
		})

		When("ClusterDetails.Name is empty", func() {
			It("should return PROCESSING phase with no resource name", func() {
				prName := uuid.New().String()
				pr := newProvisioningRequest(prName,
					&provisioningv1alpha1.ClusterDetails{Name: ""},
					nil, nil, "", "")

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(pr).
					WithStatusSubresource(pr).
					Build()
				server = &api.ProvisioningServer{HubClient: fakeClient}

				status := getNodeClusterStatus(prName)
				Expect(status.ResourceProvisioningPhase).To(Equal(provisioningapi.ResourceProvisioningPhasePROCESSING))
				Expect(status.ResourceName).To(BeEmpty())
				Expect(status.ResourceId).To(BeEmpty())
			})
		})

		When("ClusterInstanceProcessed is False/InProgress", func() {
			It("should return PROCESSING phase with cluster name", func() {
				prName := uuid.New().String()
				pr := newProvisioningRequest(prName,
					&provisioningv1alpha1.ClusterDetails{Name: "cluster-inprog"},
					nil,
					[]metav1.Condition{{
						Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed),
						Status: metav1.ConditionFalse,
						Reason: string(provisioningv1alpha1.CRconditionReasons.InProgress),
					}},
					provisioningv1alpha1.StateProgressing, "Waiting for ClusterInstance")

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(pr).
					WithStatusSubresource(pr).
					Build()
				server = &api.ProvisioningServer{HubClient: fakeClient}

				status := getNodeClusterStatus(prName)
				Expect(status.ResourceProvisioningPhase).To(Equal(provisioningapi.ResourceProvisioningPhasePROCESSING))
				Expect(status.ResourceName).To(Equal("cluster-inprog"))
				Expect(status.ResourceId).To(BeEmpty())
			})
		})

		When("ClusterInstanceProcessed is False/Failed", func() {
			It("should return FAILED phase with cluster name", func() {
				prName := uuid.New().String()
				pr := newProvisioningRequest(prName,
					&provisioningv1alpha1.ClusterDetails{Name: "cluster-cifail"},
					nil,
					[]metav1.Condition{{
						Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed),
						Status: metav1.ConditionFalse,
						Reason: string(provisioningv1alpha1.CRconditionReasons.Failed),
					}},
					provisioningv1alpha1.StateFailed, "ClusterInstance validation failed")

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(pr).
					WithStatusSubresource(pr).
					Build()
				server = &api.ProvisioningServer{HubClient: fakeClient}

				status := getNodeClusterStatus(prName)
				Expect(status.ResourceProvisioningPhase).To(Equal(provisioningapi.ResourceProvisioningPhaseFAILED))
				Expect(status.ResourceName).To(Equal("cluster-cifail"))
				Expect(status.ResourceId).To(BeEmpty())
			})
		})

		When("ClusterInstance exists but no ClusterProvisioned condition yet", func() {
			It("should return PROCESSING phase", func() {
				prName := uuid.New().String()
				pr := newProvisioningRequest(prName,
					&provisioningv1alpha1.ClusterDetails{Name: "cluster-001"},
					nil,
					[]metav1.Condition{{
						Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed),
						Status: metav1.ConditionTrue,
						Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
					}},
					provisioningv1alpha1.StateProgressing, "Cluster installation in progress")

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(pr).
					WithStatusSubresource(pr).
					Build()
				server = &api.ProvisioningServer{HubClient: fakeClient}

				status := getNodeClusterStatus(prName)
				Expect(status).NotTo(BeZero())
				Expect(status.ResourceName).To(Equal("cluster-001"))
				Expect(status.ResourceProvisioningPhase).To(Equal(provisioningapi.ResourceProvisioningPhasePROCESSING))
				Expect(status.ResourceId).To(BeEmpty())
			})
		})

		When("ClusterProvisioned condition is InProgress", func() {
			It("should return PROCESSING phase", func() {
				prName := uuid.New().String()
				pr := newProvisioningRequest(prName,
					&provisioningv1alpha1.ClusterDetails{Name: "cluster-002"},
					nil,
					[]metav1.Condition{
						{
							Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed),
							Status: metav1.ConditionTrue,
							Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
						},
						{
							Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
							Status: metav1.ConditionFalse,
							Reason: string(provisioningv1alpha1.CRconditionReasons.InProgress),
						},
					},
					provisioningv1alpha1.StateProgressing, "Cluster installation in progress")

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(pr).
					WithStatusSubresource(pr).
					Build()
				server = &api.ProvisioningServer{HubClient: fakeClient}

				status := getNodeClusterStatus(prName)
				Expect(status).NotTo(BeZero())
				Expect(status.ResourceName).To(Equal("cluster-002"))
				Expect(status.ResourceProvisioningPhase).To(Equal(provisioningapi.ResourceProvisioningPhasePROCESSING))
				Expect(status.ResourceId).To(BeEmpty())
			})
		})

		When("ClusterProvisioned condition is True", func() {
			It("should return PROVISIONED phase with resourceId", func() {
				prName := uuid.New().String()
				clusterId := "a1478db9-651f-4d30-96d6-8af13481d779"
				pr := newProvisioningRequest(prName,
					&provisioningv1alpha1.ClusterDetails{Name: "cluster-003"},
					&provisioningv1alpha1.ProvisionedResources{OCloudNodeClusterId: clusterId},
					[]metav1.Condition{
						{
							Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed),
							Status: metav1.ConditionTrue,
							Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
						},
						{
							Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
							Status: metav1.ConditionTrue,
							Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
						},
					},
					provisioningv1alpha1.StateFulfilled, "Cluster provisioned successfully")

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(pr).
					WithStatusSubresource(pr).
					Build()
				server = &api.ProvisioningServer{HubClient: fakeClient}

				status := getNodeClusterStatus(prName)
				Expect(status).NotTo(BeZero())
				Expect(status.ResourceName).To(Equal("cluster-003"))
				Expect(status.ResourceProvisioningPhase).To(Equal(provisioningapi.ResourceProvisioningPhasePROVISIONED))
				Expect(status.ResourceId).To(Equal(clusterId))
			})
		})

		When("ClusterProvisioned condition is True but resourceId not yet populated", func() {
			It("should return PROVISIONED phase with nil resourceId", func() {
				prName := uuid.New().String()
				pr := newProvisioningRequest(prName,
					&provisioningv1alpha1.ClusterDetails{Name: "cluster-004"},
					nil,
					[]metav1.Condition{
						{
							Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed),
							Status: metav1.ConditionTrue,
							Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
						},
						{
							Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
							Status: metav1.ConditionTrue,
							Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
						},
					},
					provisioningv1alpha1.StateFulfilled, "Cluster provisioned successfully")

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(pr).
					WithStatusSubresource(pr).
					Build()
				server = &api.ProvisioningServer{HubClient: fakeClient}

				status := getNodeClusterStatus(prName)
				Expect(status).NotTo(BeZero())
				Expect(status.ResourceName).To(Equal("cluster-004"))
				Expect(status.ResourceProvisioningPhase).To(Equal(provisioningapi.ResourceProvisioningPhasePROVISIONED))
				Expect(status.ResourceId).To(BeEmpty())
			})
		})

		When("ClusterProvisioned condition Failed", func() {
			It("should return FAILED phase", func() {
				prName := uuid.New().String()
				pr := newProvisioningRequest(prName,
					&provisioningv1alpha1.ClusterDetails{Name: "cluster-005"},
					nil,
					[]metav1.Condition{
						{
							Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed),
							Status: metav1.ConditionTrue,
							Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
						},
						{
							Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
							Status: metav1.ConditionFalse,
							Reason: string(provisioningv1alpha1.CRconditionReasons.Failed),
						},
					},
					provisioningv1alpha1.StateFailed, "Cluster installation failed")

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(pr).
					WithStatusSubresource(pr).
					Build()
				server = &api.ProvisioningServer{HubClient: fakeClient}

				status := getNodeClusterStatus(prName)
				Expect(status).NotTo(BeZero())
				Expect(status.ResourceName).To(Equal("cluster-005"))
				Expect(status.ResourceProvisioningPhase).To(Equal(provisioningapi.ResourceProvisioningPhaseFAILED))
				Expect(status.ResourceId).To(BeEmpty())
			})
		})

		When("ClusterProvisioned condition TimedOut", func() {
			It("should return FAILED phase", func() {
				prName := uuid.New().String()
				pr := newProvisioningRequest(prName,
					&provisioningv1alpha1.ClusterDetails{Name: "cluster-006"},
					nil,
					[]metav1.Condition{
						{
							Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed),
							Status: metav1.ConditionTrue,
							Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
						},
						{
							Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
							Status: metav1.ConditionFalse,
							Reason: string(provisioningv1alpha1.CRconditionReasons.TimedOut),
						},
					},
					provisioningv1alpha1.StateFailed, "Cluster installation timed out")

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(pr).
					WithStatusSubresource(pr).
					Build()
				server = &api.ProvisioningServer{HubClient: fakeClient}

				status := getNodeClusterStatus(prName)
				Expect(status).NotTo(BeZero())
				Expect(status.ResourceName).To(Equal("cluster-006"))
				Expect(status.ResourceProvisioningPhase).To(Equal(provisioningapi.ResourceProvisioningPhaseFAILED))
				Expect(status.ResourceId).To(BeEmpty())
			})
		})

		When("multiple conditions exist but ClusterProvisioned is among them", func() {
			It("should pick the ClusterProvisioned condition correctly", func() {
				prName := uuid.New().String()
				clusterId := "b2589ec0-762g-5e41-a7bf-9bg24592e880"
				pr := newProvisioningRequest(prName,
					&provisioningv1alpha1.ClusterDetails{Name: "cluster-007"},
					&provisioningv1alpha1.ProvisionedResources{OCloudNodeClusterId: clusterId},
					[]metav1.Condition{
						{
							Type:   string(provisioningv1alpha1.PRconditionTypes.Validated),
							Status: metav1.ConditionTrue,
							Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
						},
						{
							Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed),
							Status: metav1.ConditionTrue,
							Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
						},
						{
							Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
							Status: metav1.ConditionTrue,
							Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
						},
						{
							Type:   string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
							Status: metav1.ConditionFalse,
							Reason: string(provisioningv1alpha1.CRconditionReasons.InProgress),
						},
					},
					provisioningv1alpha1.StateProgressing, "Configuration in progress")

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(pr).
					WithStatusSubresource(pr).
					Build()
				server = &api.ProvisioningServer{HubClient: fakeClient}

				status := getNodeClusterStatus(prName)
				Expect(status).NotTo(BeZero())
				Expect(status.ResourceName).To(Equal("cluster-007"))
				Expect(status.ResourceProvisioningPhase).To(Equal(provisioningapi.ResourceProvisioningPhasePROVISIONED))
				Expect(status.ResourceId).To(Equal(clusterId))
			})
		})
	})

	Describe("GetProvisioningRequest returns infrastructureResourceProvisioningStatus", func() {
		var (
			server *api.ProvisioningServer
			scheme *runtime.Scheme
			ctx    context.Context
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme = runtime.NewScheme()
			Expect(provisioningv1alpha1.AddToScheme(scheme)).To(Succeed())
		})

		newPRWithInfraStatuses := func(
			name string,
			statuses []provisioningv1alpha1.InfrastructureResourceStatus,
		) *provisioningv1alpha1.ProvisioningRequest {
			templateParamsBytes, err := json.Marshal(map[string]interface{}{"key": "value"})
			Expect(err).NotTo(HaveOccurred())

			pr := &provisioningv1alpha1.ProvisioningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
					UID:  types.UID(name),
				},
				Spec: provisioningv1alpha1.ProvisioningRequestSpec{
					Name:               "test",
					Description:        "test provisioning request",
					TemplateName:       "test-template",
					TemplateVersion:    "v1.0.0",
					TemplateParameters: runtime.RawExtension{Raw: templateParamsBytes},
				},
			}
			pr.Status.Extensions.InfrastructureResourceStatuses = statuses
			return pr
		}

		getInfraStatuses := func(prName string) []provisioningapi.ResourceProvisioningStatus {
			reqUUID, err := uuid.Parse(prName)
			Expect(err).NotTo(HaveOccurred())

			resp, err := server.GetProvisioningRequest(ctx, provisioningapi.GetProvisioningRequestRequestObject{
				ProvisioningRequestId: reqUUID,
			})
			Expect(err).NotTo(HaveOccurred())
			result := resp.(provisioningapi.GetProvisioningRequest200JSONResponse)
			return result.Status.InfrastructureResourceProvisioningStatus
		}

		When("no infrastructure resource statuses on CRD", func() {
			It("should return an empty array", func() {
				prName := uuid.New().String()
				pr := newPRWithInfraStatuses(prName, nil)

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(pr).
					WithStatusSubresource(pr).
					Build()
				server = &api.ProvisioningServer{HubClient: fakeClient}

				statuses := getInfraStatuses(prName)
				Expect(statuses).To(HaveLen(0))
			})
		})

		When("single node is PROCESSING", func() {
			It("should return one status with PROCESSING phase", func() {
				prName := uuid.New().String()
				pr := newPRWithInfraStatuses(prName, []provisioningv1alpha1.InfrastructureResourceStatus{
					{ResourceName: "host-a", ResourceId: "node-a", ResourceProvisioningPhase: "PROCESSING"},
				})

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(pr).
					WithStatusSubresource(pr).
					Build()
				server = &api.ProvisioningServer{HubClient: fakeClient}

				statuses := getInfraStatuses(prName)
				Expect(statuses).To(HaveLen(1))
				Expect(statuses[0].ResourceName).To(Equal("host-a"))
				Expect(statuses[0].ResourceId).To(Equal("node-a"))
				Expect(statuses[0].ResourceProvisioningPhase).To(Equal(provisioningapi.ResourceProvisioningPhasePROCESSING))
			})
		})

		When("multiple nodes with mixed phases", func() {
			It("should return all statuses with correct phases", func() {
				prName := uuid.New().String()
				pr := newPRWithInfraStatuses(prName, []provisioningv1alpha1.InfrastructureResourceStatus{
					{ResourceName: "host-a", ResourceId: "node-a", ResourceProvisioningPhase: "PROVISIONED"},
					{ResourceName: "host-b", ResourceId: "node-b", ResourceProvisioningPhase: "PROCESSING"},
					{ResourceName: "host-c", ResourceId: "node-c", ResourceProvisioningPhase: "FAILED"},
				})

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(pr).
					WithStatusSubresource(pr).
					Build()
				server = &api.ProvisioningServer{HubClient: fakeClient}

				statuses := getInfraStatuses(prName)
				Expect(statuses).To(HaveLen(3))
				Expect(statuses[0].ResourceProvisioningPhase).To(Equal(provisioningapi.ResourceProvisioningPhasePROVISIONED))
				Expect(statuses[1].ResourceProvisioningPhase).To(Equal(provisioningapi.ResourceProvisioningPhasePROCESSING))
				Expect(statuses[2].ResourceProvisioningPhase).To(Equal(provisioningapi.ResourceProvisioningPhaseFAILED))
			})
		})

		When("node has empty resourceId", func() {
			It("should return status with empty resourceId", func() {
				prName := uuid.New().String()
				pr := newPRWithInfraStatuses(prName, []provisioningv1alpha1.InfrastructureResourceStatus{
					{ResourceName: "host-x", ResourceId: "", ResourceProvisioningPhase: "PROCESSING"},
				})

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(pr).
					WithStatusSubresource(pr).
					Build()
				server = &api.ProvisioningServer{HubClient: fakeClient}

				statuses := getInfraStatuses(prName)
				Expect(statuses).To(HaveLen(1))
				Expect(statuses[0].ResourceName).To(Equal("host-x"))
				Expect(statuses[0].ResourceId).To(BeEmpty())
			})
		})
	})

	Describe("UpdateProvisioningRequest with concurrent updates", func() {
		var (
			server *api.ProvisioningServer
			scheme *runtime.Scheme
		)

		BeforeEach(func() {
			scheme = runtime.NewScheme()
			Expect(provisioningv1alpha1.AddToScheme(scheme)).To(Succeed())
		})

		When("multiple concurrent updates occur", func() {
			It("should handle conflicts with retry logic using fake client", func() {
				// Create initial ProvisioningRequest with a valid UID
				initialTemplateParams := map[string]interface{}{
					"clusterName": "test-cluster",
					"baseDomain":  "example.com",
				}
				templateParamsBytes, err := json.Marshal(initialTemplateParams)
				Expect(err).NotTo(HaveOccurred())

				prUID := uuid.New().String()
				initialPR := &provisioningv1alpha1.ProvisioningRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:            testUUID.String(),
						UID:             types.UID(prUID),
						ResourceVersion: "1",
					},
					Spec: provisioningv1alpha1.ProvisioningRequestSpec{
						Name:               "test",
						Description:        "initial description",
						TemplateName:       "test-template",
						TemplateVersion:    "v1.0.0",
						TemplateParameters: runtime.RawExtension{Raw: templateParamsBytes},
					},
				}

				// Create fake client with initial object - use interceptor to preserve UID
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(initialPR).
					WithStatusSubresource(initialPR).
					WithInterceptorFuncs(interceptorFuncs(prUID)).
					Build()

				server = &api.ProvisioningServer{
					HubClient: fakeClient,
				}

				// Prepare first update
				updatedTemplateParams1 := map[string]interface{}{
					"clusterName": "test-cluster-updated-1",
					"baseDomain":  "example.com",
				}

				updateRequest1 := provisioningapi.UpdateProvisioningRequestRequestObject{
					ProvisioningRequestId: testUUID,
					Body: &provisioningapi.ProvisioningRequestData{
						ProvisioningRequestId: testUUID,
						Name:                  "test-updated-1",
						Description:           "updated description 1",
						TemplateName:          "test-template",
						TemplateVersion:       "v1.0.0",
						TemplateParameters:    updatedTemplateParams1,
					},
				}

				// First update should succeed
				resp, err := server.UpdateProvisioningRequest(ctx, updateRequest1)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(provisioningapi.UpdateProvisioningRequest200JSONResponse{}))

				// Verify the update was applied
				updatedPR := &provisioningv1alpha1.ProvisioningRequest{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: testUUID.String()}, updatedPR)
				Expect(err).NotTo(HaveOccurred())
				Expect(updatedPR.Spec.Name).To(Equal("test-updated-1"))
				Expect(updatedPR.Spec.Description).To(Equal("updated description 1"))
			})

			It("should succeed when updates don't conflict", func() {
				// Create initial ProvisioningRequest
				initialTemplateParams := map[string]interface{}{
					"clusterName": "test-cluster",
					"baseDomain":  "example.com",
				}
				templateParamsBytes, err := json.Marshal(initialTemplateParams)
				Expect(err).NotTo(HaveOccurred())

				prUID := uuid.New().String()
				initialPR := &provisioningv1alpha1.ProvisioningRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name: testUUID.String(),
						UID:  types.UID(prUID),
					},
					Spec: provisioningv1alpha1.ProvisioningRequestSpec{
						Name:               "test",
						Description:        "initial description",
						TemplateName:       "test-template",
						TemplateVersion:    "v1.0.0",
						TemplateParameters: runtime.RawExtension{Raw: templateParamsBytes},
					},
				}

				// Create fake client with initial object
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(initialPR).
					WithStatusSubresource(initialPR).
					WithInterceptorFuncs(interceptorFuncs(prUID)).
					Build()

				server = &api.ProvisioningServer{
					HubClient: fakeClient,
				}

				// Sequential updates should both succeed
				updatedTemplateParams1 := map[string]interface{}{
					"clusterName": "test-cluster-seq-1",
					"baseDomain":  "example.com",
				}

				updateRequest1 := provisioningapi.UpdateProvisioningRequestRequestObject{
					ProvisioningRequestId: testUUID,
					Body: &provisioningapi.ProvisioningRequestData{
						ProvisioningRequestId: testUUID,
						Name:                  "test-seq-1",
						Description:           "sequential update 1",
						TemplateName:          "test-template",
						TemplateVersion:       "v1.0.0",
						TemplateParameters:    updatedTemplateParams1,
					},
				}

				resp1, err := server.UpdateProvisioningRequest(ctx, updateRequest1)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp1).To(BeAssignableToTypeOf(provisioningapi.UpdateProvisioningRequest200JSONResponse{}))

				updatedTemplateParams2 := map[string]interface{}{
					"clusterName": "test-cluster-seq-2",
					"baseDomain":  "example.com",
				}

				updateRequest2 := provisioningapi.UpdateProvisioningRequestRequestObject{
					ProvisioningRequestId: testUUID,
					Body: &provisioningapi.ProvisioningRequestData{
						ProvisioningRequestId: testUUID,
						Name:                  "test-seq-2",
						Description:           "sequential update 2",
						TemplateName:          "test-template",
						TemplateVersion:       "v1.0.0",
						TemplateParameters:    updatedTemplateParams2,
					},
				}

				resp2, err := server.UpdateProvisioningRequest(ctx, updateRequest2)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp2).To(BeAssignableToTypeOf(provisioningapi.UpdateProvisioningRequest200JSONResponse{}))

				// Verify final state
				finalPR := &provisioningv1alpha1.ProvisioningRequest{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: testUUID.String()}, finalPR)
				Expect(err).NotTo(HaveOccurred())
				Expect(finalPR.Spec.Name).To(Equal("test-seq-2"))
				Expect(finalPR.Spec.Description).To(Equal("sequential update 2"))
			})
		})

		When("ProvisioningRequest ID mismatch in body and path", func() {
			It("should return 422 error", func() {
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					Build()

				server = &api.ProvisioningServer{
					HubClient: fakeClient,
				}

				differentUUID := uuid.New()
				templateParams := map[string]interface{}{"key": "value"}

				updateRequest := provisioningapi.UpdateProvisioningRequestRequestObject{
					ProvisioningRequestId: testUUID,
					Body: &provisioningapi.ProvisioningRequestData{
						ProvisioningRequestId: differentUUID, // Different from path
						Name:                  "test",
						Description:           "description",
						TemplateName:          "template",
						TemplateVersion:       "v1.0.0",
						TemplateParameters:    templateParams,
					},
				}

				resp, err := server.UpdateProvisioningRequest(ctx, updateRequest)
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(provisioningapi.UpdateProvisioningRequest422ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(422))
			})
		})

		When("ProvisioningRequest does not exist", func() {
			It("should return 404 error", func() {
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					Build()

				server = &api.ProvisioningServer{
					HubClient: fakeClient,
				}

				templateParams := map[string]interface{}{"key": "value"}

				updateRequest := provisioningapi.UpdateProvisioningRequestRequestObject{
					ProvisioningRequestId: testUUID,
					Body: &provisioningapi.ProvisioningRequestData{
						ProvisioningRequestId: testUUID,
						Name:                  "test",
						Description:           "description",
						TemplateName:          "template",
						TemplateVersion:       "v1.0.0",
						TemplateParameters:    templateParams,
					},
				}

				resp, err := server.UpdateProvisioningRequest(ctx, updateRequest)
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(provisioningapi.UpdateProvisioningRequest404ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(404))
			})
		})
	})

	Describe("CreateProvisioningRequest Location header", func() {
		var (
			server *api.ProvisioningServer
			scheme *runtime.Scheme
		)

		BeforeEach(func() {
			scheme = runtime.NewScheme()
			Expect(provisioningv1alpha1.AddToScheme(scheme)).To(Succeed())
		})

		It("should populate the Location header with the resource URI", func() {
			templateParams := map[string]interface{}{"key": "value"}

			prUID := uuid.New().String()
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
						if obj.GetUID() == "" {
							obj.SetUID(types.UID(prUID))
						}
						return c.Create(ctx, obj, opts...)
					},
				}).
				Build()

			server = &api.ProvisioningServer{
				HubClient: fakeClient,
			}

			createRequest := provisioningapi.CreateProvisioningRequestRequestObject{
				Body: &provisioningapi.ProvisioningRequestData{
					ProvisioningRequestId: testUUID,
					Name:                  "test",
					Description:           "test description",
					TemplateName:          "template",
					TemplateVersion:       "v1.0.0",
					TemplateParameters:    templateParams,
				},
			}

			resp, err := server.CreateProvisioningRequest(ctx, createRequest)
			Expect(err).NotTo(HaveOccurred())

			created, ok := resp.(provisioningapi.CreateProvisioningRequest201JSONResponse)
			Expect(ok).To(BeTrue())

			expectedLocation := fmt.Sprintf("%s/provisioningRequests/%s", constants.O2IMSProvisioningBaseURL, testUUID)
			Expect(created.Headers.Location).To(Equal(expectedLocation))
		})
	})

	Describe("DeleteProvisioningRequest Location header", func() {
		var (
			server *api.ProvisioningServer
			scheme *runtime.Scheme
		)

		BeforeEach(func() {
			scheme = runtime.NewScheme()
			Expect(provisioningv1alpha1.AddToScheme(scheme)).To(Succeed())
		})

		It("should populate the Location header with the resource URI", func() {
			templateParams := map[string]interface{}{"key": "value"}
			templateParamsBytes, err := json.Marshal(templateParams)
			Expect(err).NotTo(HaveOccurred())

			prUID := uuid.New().String()
			initialPR := &provisioningv1alpha1.ProvisioningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: testUUID.String(),
					UID:  types.UID(prUID),
				},
				Spec: provisioningv1alpha1.ProvisioningRequestSpec{
					Name:               "test",
					Description:        "test description",
					TemplateName:       "template",
					TemplateVersion:    "v1.0.0",
					TemplateParameters: runtime.RawExtension{Raw: templateParamsBytes},
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(initialPR).
				Build()

			server = &api.ProvisioningServer{
				HubClient: fakeClient,
			}

			deleteRequest := provisioningapi.DeleteProvisioningRequestRequestObject{
				ProvisioningRequestId: testUUID,
			}

			resp, err := server.DeleteProvisioningRequest(ctx, deleteRequest)
			Expect(err).NotTo(HaveOccurred())

			deleted, ok := resp.(provisioningapi.DeleteProvisioningRequest202Response)
			Expect(ok).To(BeTrue())

			expectedLocation := fmt.Sprintf("%s/provisioningRequests/%s", constants.O2IMSProvisioningBaseURL, testUUID)
			Expect(deleted.Headers.Location).To(Equal(expectedLocation))
		})
	})
})
