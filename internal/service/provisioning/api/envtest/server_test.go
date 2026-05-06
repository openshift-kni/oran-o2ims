/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package envtest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	commonapi "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
	provisioningapi "github.com/openshift-kni/oran-o2ims/internal/service/provisioning/api/generated"
)

var _ = Describe("ProvisioningServer", Label("envtest"), func() {
	provisioningAPIPath := constants.O2IMSProvisioningAPIPath
	provisioningBase := constants.O2IMSProvisioningBaseURL

	// buildPR builds a new ProvisioningRequest CR with a cluster template
	// with the given name and template parameters
	buildPR := func(name string) *provisioningv1alpha1.ProvisioningRequest {
		templateParamsBytes, err := json.Marshal(map[string]interface{}{"key": "value"})
		Expect(err).NotTo(HaveOccurred())

		return &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				Name:               "test",
				Description:        "test provisioning request",
				TemplateName:       "test-template",
				TemplateVersion:    "v1.0.0",
				TemplateParameters: runtime.RawExtension{Raw: templateParamsBytes},
			},
		}
	}

	// createPR creates a new ProvisioningRequest CR in the Kubernetes cluster
	createPR := func(pr *provisioningv1alpha1.ProvisioningRequest) {
		Expect(k8sClient.Create(ctx, pr)).To(Succeed())
	}

	// updatePRStatus updates the status of the ProvisioningRequest CR with the given cluster details,
	// provisioned resources, conditions, provisioning phase, provisioning details, and infrastructure resource statuses
	updatePRStatus := func(pr *provisioningv1alpha1.ProvisioningRequest,
		clusterDetails *provisioningv1alpha1.ClusterDetails,
		provisionedResources *provisioningv1alpha1.ProvisionedResources,
		conditions []metav1.Condition,
		provisioningPhase provisioningv1alpha1.ProvisioningPhase,
		provisioningDetails string,
		infraStatuses []provisioningv1alpha1.InfrastructureResourceStatus,
	) {
		fetched := &provisioningv1alpha1.ProvisioningRequest{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pr), fetched)).To(Succeed())
		now := metav1.NewTime(time.Now())
		for i := range conditions {
			if conditions[i].LastTransitionTime.IsZero() {
				conditions[i].LastTransitionTime = now
			}
		}
		fetched.Status.Conditions = conditions
		fetched.Status.Extensions = provisioningv1alpha1.Extensions{
			ClusterDetails:                 clusterDetails,
			InfrastructureResourceStatuses: infraStatuses,
		}
		fetched.Status.ProvisioningStatus = provisioningv1alpha1.ProvisioningStatus{
			ProvisioningPhase:    provisioningPhase,
			ProvisioningDetails:  provisioningDetails,
			ProvisionedResources: provisionedResources,
		}
		Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())
	}

	httpClient := &http.Client{Timeout: 5 * time.Second}

	// httpDo makes a request with the given method, path, and JSON body and returns the response and body
	httpDo := func(method, path string, jsonBody string) (*http.Response, []byte) {
		var bodyReader io.Reader
		if jsonBody != "" {
			bodyReader = strings.NewReader(jsonBody)
		}
		req, err := http.NewRequestWithContext(ctx, method, baseURL+path, bodyReader)
		Expect(err).NotTo(HaveOccurred())
		if jsonBody != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := httpClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		return resp, body
	}

	Describe("GetAllVersions", func() {
		It("should return the list of API versions", func() {
			resp, body := httpDo(http.MethodGet, provisioningAPIPath+"/api_versions", "")
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var versions commonapi.APIVersions
			Expect(json.Unmarshal(body, &versions)).To(Succeed())
			Expect(versions.ApiVersions).NotTo(BeNil())
			Expect(*versions.ApiVersions).To(HaveLen(1))
			Expect(*(*versions.ApiVersions)[0].Version).To(Equal("1.2.0"))
			Expect(versions.UriPrefix).NotTo(BeNil())
		})
	})

	Describe("GetMinorVersions", func() {
		It("should return the list of minor API versions", func() {
			resp, body := httpDo(http.MethodGet, provisioningBase+"/api_versions", "")
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var versions commonapi.APIVersions
			Expect(json.Unmarshal(body, &versions)).To(Succeed())
			Expect(versions.ApiVersions).NotTo(BeNil())
			Expect(*versions.ApiVersions).To(HaveLen(1))
			Expect(*(*versions.ApiVersions)[0].Version).To(Equal("1.2.0"))
			Expect(versions.UriPrefix).NotTo(BeNil())
		})
	})

	Describe("GetProvisioningRequests", func() {
		It("should return a list of provisioning requests", func() {
			prName1 := uuid.New().String()
			prName2 := uuid.New().String()
			pr1 := buildPR(prName1)
			pr2 := buildPR(prName2)
			createPR(pr1)
			createPR(pr2)
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, pr1)
				_ = k8sClient.Delete(ctx, pr2)
			})

			resp, body := httpDo(http.MethodGet, provisioningBase+"/provisioningRequests", "")
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var list []provisioningapi.ProvisioningRequestInfo
			Expect(json.Unmarshal(body, &list)).To(Succeed())
			Expect(len(list)).To(BeNumerically(">=", 2))

			ids := make([]string, 0, len(list))
			for _, item := range list {
				ids = append(ids, item.ProvisioningRequestData.ProvisioningRequestId.String())
			}
			Expect(ids).To(ContainElement(prName1))
			Expect(ids).To(ContainElement(prName2))
		})
	})

	Describe("GetProvisioningRequest", func() {
		When("ProvisioningRequest does not exist", func() {
			It("should return 404", func() {
				nonExistentUUID := uuid.New().String()
				resp, body := httpDo(http.MethodGet, provisioningBase+"/provisioningRequests/"+nonExistentUUID, "")
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))

				var problem commonapi.ProblemDetails
				Expect(json.Unmarshal(body, &problem)).To(Succeed())
				Expect(problem.Status).To(Equal(404))
			})
		})
	})

	Describe("CreateProvisioningRequest", func() {
		When("ProvisioningRequest already exists", func() {
			It("should return 409", func() {
				prUUID := uuid.New()
				createBody := provisioningapi.ProvisioningRequestData{
					ProvisioningRequestId: prUUID,
					Name:                  "test",
					Description:           "test description",
					TemplateName:          "template",
					TemplateVersion:       "v1.0.0",
					TemplateParameters:    map[string]interface{}{"key": "value"},
				}
				b, err := json.Marshal(createBody)
				Expect(err).NotTo(HaveOccurred())

				resp, _ := httpDo(http.MethodPost, provisioningBase+"/provisioningRequests", string(b))
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))
				DeferCleanup(func() {
					pr := &provisioningv1alpha1.ProvisioningRequest{}
					pr.Name = prUUID.String()
					_ = k8sClient.Delete(ctx, pr)
				})

				resp, body := httpDo(http.MethodPost, provisioningBase+"/provisioningRequests", string(b))
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))

				var problem commonapi.ProblemDetails
				Expect(json.Unmarshal(body, &problem)).To(Succeed())
				Expect(problem.Status).To(Equal(409))
			})
		})
	})

	Describe("DeleteProvisioningRequest", func() {
		When("ProvisioningRequest does not exist", func() {
			It("should return 404", func() {
				nonExistentUUID := uuid.New().String()
				resp, body := httpDo(http.MethodDelete, provisioningBase+"/provisioningRequests/"+nonExistentUUID, "")
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))

				var problem commonapi.ProblemDetails
				Expect(json.Unmarshal(body, &problem)).To(Succeed())
				Expect(problem.Status).To(Equal(404))
			})
		})
	})

	DescribeTable("GetProvisioningRequest nodeClusterProvisioningStatus",
		func(
			clusterDetails *provisioningv1alpha1.ClusterDetails,
			provisionedResources *provisioningv1alpha1.ProvisionedResources,
			conditions []metav1.Condition,
			expectedPhase provisioningapi.ResourceProvisioningPhase,
			expectedName string,
			expectedId string,
		) {
			prName := uuid.New().String()
			pr := buildPR(prName)
			createPR(pr)
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, pr) })

			if clusterDetails != nil || provisionedResources != nil || conditions != nil {
				updatePRStatus(pr, clusterDetails, provisionedResources, conditions, "", "", nil)
			}

			resp, body := httpDo(http.MethodGet, provisioningBase+"/provisioningRequests/"+prName, "")
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var info provisioningapi.ProvisioningRequestInfo
			Expect(json.Unmarshal(body, &info)).To(Succeed())
			status := info.Status.NodeClusterProvisioningStatus

			Expect(status.ResourceName).To(Equal(expectedName))
			Expect(status.ResourceProvisioningPhase).To(Equal(expectedPhase))
			if expectedId != "" {
				Expect(status.ResourceId).To(Equal(expectedId))
			} else {
				Expect(status.ResourceId).To(BeEmpty())
			}
		},
		Entry("ClusterDetails is nil → PROCESSING with no resource name",
			nil, nil, nil,
			provisioningapi.ResourceProvisioningPhasePROCESSING, "", ""),

		Entry("ClusterInstanceProcessed absent → PROCESSING with no resource name",
			&provisioningv1alpha1.ClusterDetails{Name: "cluster-abc"}, nil,
			[]metav1.Condition{{
				Type:   string(provisioningv1alpha1.PRconditionTypes.Validated),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			}},
			provisioningapi.ResourceProvisioningPhasePROCESSING, "", ""),

		Entry("ClusterInstanceProcessed False/InProgress → PROCESSING with cluster name",
			&provisioningv1alpha1.ClusterDetails{Name: "cluster-inprog"}, nil,
			[]metav1.Condition{{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed),
				Status: metav1.ConditionFalse,
				Reason: string(provisioningv1alpha1.CRconditionReasons.InProgress),
			}},
			provisioningapi.ResourceProvisioningPhasePROCESSING, "cluster-inprog", ""),

		Entry("ClusterInstanceProcessed False/Failed → FAILED with cluster name",
			&provisioningv1alpha1.ClusterDetails{Name: "cluster-cifail"}, nil,
			[]metav1.Condition{{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed),
				Status: metav1.ConditionFalse,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Failed),
			}},
			provisioningapi.ResourceProvisioningPhaseFAILED, "cluster-cifail", ""),

		Entry("ClusterInstanceProcessed False/TimedOut → FAILED with cluster name",
			&provisioningv1alpha1.ClusterDetails{Name: "cluster-citimeout"}, nil,
			[]metav1.Condition{{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed),
				Status: metav1.ConditionFalse,
				Reason: string(provisioningv1alpha1.CRconditionReasons.TimedOut),
			}},
			provisioningapi.ResourceProvisioningPhaseFAILED, "cluster-citimeout", ""),

		Entry("ClusterInstanceProcessed True, no ClusterProvisioned → PROCESSING",
			&provisioningv1alpha1.ClusterDetails{Name: "cluster-001"}, nil,
			[]metav1.Condition{{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			}},
			provisioningapi.ResourceProvisioningPhasePROCESSING, "cluster-001", ""),

		Entry("ClusterProvisioned False/InProgress → PROCESSING",
			&provisioningv1alpha1.ClusterDetails{Name: "cluster-002"}, nil,
			[]metav1.Condition{
				{Type: string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed), Status: metav1.ConditionTrue, Reason: string(provisioningv1alpha1.CRconditionReasons.Completed)},
				{Type: string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned), Status: metav1.ConditionFalse, Reason: string(provisioningv1alpha1.CRconditionReasons.InProgress)},
			},
			provisioningapi.ResourceProvisioningPhasePROCESSING, "cluster-002", ""),

		Entry("ClusterProvisioned True → PROVISIONED with resourceId",
			&provisioningv1alpha1.ClusterDetails{Name: "cluster-003"},
			&provisioningv1alpha1.ProvisionedResources{OCloudNodeClusterId: "a1478db9-651f-4d30-96d6-8af13481d779"},
			[]metav1.Condition{
				{Type: string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed), Status: metav1.ConditionTrue, Reason: string(provisioningv1alpha1.CRconditionReasons.Completed)},
				{Type: string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned), Status: metav1.ConditionTrue, Reason: string(provisioningv1alpha1.CRconditionReasons.Completed)},
			},
			provisioningapi.ResourceProvisioningPhasePROVISIONED, "cluster-003", "a1478db9-651f-4d30-96d6-8af13481d779"),

		Entry("ClusterProvisioned True but resourceId not yet populated → PROVISIONED empty resourceId",
			&provisioningv1alpha1.ClusterDetails{Name: "cluster-004"}, nil,
			[]metav1.Condition{
				{Type: string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed), Status: metav1.ConditionTrue, Reason: string(provisioningv1alpha1.CRconditionReasons.Completed)},
				{Type: string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned), Status: metav1.ConditionTrue, Reason: string(provisioningv1alpha1.CRconditionReasons.Completed)},
			},
			provisioningapi.ResourceProvisioningPhasePROVISIONED, "cluster-004", ""),

		Entry("ClusterProvisioned False/Failed → FAILED with resourceId",
			&provisioningv1alpha1.ClusterDetails{Name: "cluster-005"},
			&provisioningv1alpha1.ProvisionedResources{OCloudNodeClusterId: "c3690fd1-873h-6f52-b8cg-0ch35603f991"},
			[]metav1.Condition{
				{Type: string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed), Status: metav1.ConditionTrue, Reason: string(provisioningv1alpha1.CRconditionReasons.Completed)},
				{Type: string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned), Status: metav1.ConditionFalse, Reason: string(provisioningv1alpha1.CRconditionReasons.Failed)},
			},
			provisioningapi.ResourceProvisioningPhaseFAILED, "cluster-005", "c3690fd1-873h-6f52-b8cg-0ch35603f991"),

		Entry("ClusterProvisioned False/TimedOut → FAILED empty resourceId",
			&provisioningv1alpha1.ClusterDetails{Name: "cluster-006"}, nil,
			[]metav1.Condition{
				{Type: string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed), Status: metav1.ConditionTrue, Reason: string(provisioningv1alpha1.CRconditionReasons.Completed)},
				{Type: string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned), Status: metav1.ConditionFalse, Reason: string(provisioningv1alpha1.CRconditionReasons.TimedOut)},
			},
			provisioningapi.ResourceProvisioningPhaseFAILED, "cluster-006", ""),

		Entry("multiple conditions → picks ClusterProvisioned correctly",
			&provisioningv1alpha1.ClusterDetails{Name: "cluster-007"},
			&provisioningv1alpha1.ProvisionedResources{OCloudNodeClusterId: "b2589ec0-762g-5e41-a7bf-9bg24592e880"},
			[]metav1.Condition{
				{Type: string(provisioningv1alpha1.PRconditionTypes.Validated), Status: metav1.ConditionTrue, Reason: string(provisioningv1alpha1.CRconditionReasons.Completed)},
				{Type: string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed), Status: metav1.ConditionTrue, Reason: string(provisioningv1alpha1.CRconditionReasons.Completed)},
				{Type: string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned), Status: metav1.ConditionTrue, Reason: string(provisioningv1alpha1.CRconditionReasons.Completed)},
				{Type: string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied), Status: metav1.ConditionFalse, Reason: string(provisioningv1alpha1.CRconditionReasons.InProgress)},
			},
			provisioningapi.ResourceProvisioningPhasePROVISIONED, "cluster-007", "b2589ec0-762g-5e41-a7bf-9bg24592e880"),
	)

	Describe("GetProvisioningRequest returns infrastructureResourceProvisioningStatus", func() {
		getInfraStatuses := func(prName string) (int, []provisioningapi.ResourceProvisioningStatus) {
			resp, body := httpDo(http.MethodGet, provisioningBase+"/provisioningRequests/"+prName, "")
			if resp.StatusCode != http.StatusOK {
				return resp.StatusCode, nil
			}
			var info provisioningapi.ProvisioningRequestInfo
			Expect(json.Unmarshal(body, &info)).To(Succeed())
			return resp.StatusCode, info.Status.InfrastructureResourceProvisioningStatus
		}

		When("no infrastructure resource statuses on CRD", func() {
			It("should return an empty array", func() {
				prName := uuid.New().String()
				pr := buildPR(prName)
				createPR(pr)
				DeferCleanup(func() { _ = k8sClient.Delete(ctx, pr) })

				code, statuses := getInfraStatuses(prName)
				Expect(code).To(Equal(http.StatusOK))
				Expect(statuses).To(HaveLen(0))
			})
		})

		When("single node is PROCESSING", func() {
			It("should return one status with PROCESSING phase", func() {
				prName := uuid.New().String()
				pr := buildPR(prName)
				createPR(pr)
				updatePRStatus(pr, nil, nil, nil, "", "",
					[]provisioningv1alpha1.InfrastructureResourceStatus{
						{ResourceName: "host-a", ResourceId: "node-a", ResourceProvisioningPhase: "PROCESSING"},
					})
				DeferCleanup(func() { _ = k8sClient.Delete(ctx, pr) })

				code, statuses := getInfraStatuses(prName)
				Expect(code).To(Equal(http.StatusOK))
				Expect(statuses).To(HaveLen(1))
				Expect(statuses[0].ResourceName).To(Equal("host-a"))
				Expect(statuses[0].ResourceId).To(Equal("node-a"))
				Expect(statuses[0].ResourceProvisioningPhase).To(Equal(provisioningapi.ResourceProvisioningPhasePROCESSING))
			})
		})

		When("multiple nodes with mixed phases", func() {
			It("should return all statuses with correct phases", func() {
				prName := uuid.New().String()
				pr := buildPR(prName)
				createPR(pr)
				updatePRStatus(pr, nil, nil, nil, "", "",
					[]provisioningv1alpha1.InfrastructureResourceStatus{
						{ResourceName: "host-a", ResourceId: "node-a", ResourceProvisioningPhase: "PROVISIONED"},
						{ResourceName: "host-b", ResourceId: "node-b", ResourceProvisioningPhase: "PROCESSING"},
						{ResourceName: "host-c", ResourceId: "node-c", ResourceProvisioningPhase: "FAILED"},
					})
				DeferCleanup(func() { _ = k8sClient.Delete(ctx, pr) })

				code, statuses := getInfraStatuses(prName)
				Expect(code).To(Equal(http.StatusOK))
				Expect(statuses).To(HaveLen(3))
				Expect(statuses[0].ResourceProvisioningPhase).To(Equal(provisioningapi.ResourceProvisioningPhasePROVISIONED))
				Expect(statuses[1].ResourceProvisioningPhase).To(Equal(provisioningapi.ResourceProvisioningPhasePROCESSING))
				Expect(statuses[2].ResourceProvisioningPhase).To(Equal(provisioningapi.ResourceProvisioningPhaseFAILED))
			})
		})

	})

	Describe("UpdateProvisioningRequest", func() {
		createPRBody := func(id uuid.UUID, name, description string, templateParams map[string]interface{}) string {
			body := provisioningapi.ProvisioningRequestData{
				ProvisioningRequestId: id,
				Name:                  name,
				Description:           description,
				TemplateName:          "test-template",
				TemplateVersion:       "v1.0.0",
				TemplateParameters:    templateParams,
			}
			b, err := json.Marshal(body)
			Expect(err).NotTo(HaveOccurred())
			return string(b)
		}

		When("ProvisioningRequest exists", func() {
			It("should update and persist the changes", func() {
				prName := uuid.New().String()
				pr := buildPR(prName)
				createPR(pr)
				DeferCleanup(func() { _ = k8sClient.Delete(ctx, pr) })

				body := createPRBody(uuid.MustParse(prName),
					"test-updated", "updated description",
					map[string]interface{}{"clusterName": "test-cluster-updated", "baseDomain": "example.com"})

				resp, _ := httpDo(http.MethodPut,
					provisioningBase+"/provisioningRequests/"+prName, body)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				fetched := &provisioningv1alpha1.ProvisioningRequest{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pr), fetched)).To(Succeed())
				Expect(fetched.Spec.Name).To(Equal("test-updated"))
				Expect(fetched.Spec.Description).To(Equal("updated description"))
			})
		})

		When("ProvisioningRequest ID mismatch in body and path", func() {
			It("should return 422 error", func() {
				pathUUID := uuid.New()
				bodyUUID := uuid.New()

				body := createPRBody(bodyUUID,
					"test", "description",
					map[string]interface{}{"key": "value"})

				resp, respBody := httpDo(http.MethodPut,
					provisioningBase+"/provisioningRequests/"+pathUUID.String(), body)
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))

				var problem commonapi.ProblemDetails
				Expect(json.Unmarshal(respBody, &problem)).To(Succeed())
				Expect(problem.Status).To(Equal(422))
			})
		})

		When("ProvisioningRequest does not exist", func() {
			It("should return 404 error", func() {
				nonExistentUUID := uuid.New()
				body := createPRBody(nonExistentUUID,
					"test", "description",
					map[string]interface{}{"key": "value"})

				resp, respBody := httpDo(http.MethodPut,
					provisioningBase+"/provisioningRequests/"+nonExistentUUID.String(), body)
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))

				var problem commonapi.ProblemDetails
				Expect(json.Unmarshal(respBody, &problem)).To(Succeed())
				Expect(problem.Status).To(Equal(404))
			})
		})
	})

	Describe("CreateProvisioningRequest Location header", func() {
		It("should populate the Location header with the resource URI", func() {
			prUUID := uuid.New()
			body := provisioningapi.ProvisioningRequestData{
				ProvisioningRequestId: prUUID,
				Name:                  "test",
				Description:           "test description",
				TemplateName:          "template",
				TemplateVersion:       "v1.0.0",
				TemplateParameters:    map[string]interface{}{"key": "value"},
			}
			b, err := json.Marshal(body)
			Expect(err).NotTo(HaveOccurred())

			resp, _ := httpDo(http.MethodPost,
				provisioningBase+"/provisioningRequests", string(b))
			DeferCleanup(func() {
				pr := &provisioningv1alpha1.ProvisioningRequest{}
				pr.Name = prUUID.String()
				_ = k8sClient.Delete(ctx, pr)
			})
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))

			expectedLocation := fmt.Sprintf("%s/provisioningRequests/%s", provisioningBase, prUUID)
			Expect(resp.Header.Get("Location")).To(Equal(expectedLocation))
		})
	})

	Describe("DeleteProvisioningRequest Location header", func() {
		It("should populate the Location header with the resource URI", func() {
			prName := uuid.New().String()
			pr := buildPR(prName)
			createPR(pr)

			resp, _ := httpDo(http.MethodDelete,
				provisioningBase+"/provisioningRequests/"+prName, "")
			Expect(resp.StatusCode).To(Equal(http.StatusAccepted))

			expectedLocation := fmt.Sprintf("%s/provisioningRequests/%s", provisioningBase, prName)
			Expect(resp.Header.Get("Location")).To(Equal(expectedLocation))
		})
	})
})
