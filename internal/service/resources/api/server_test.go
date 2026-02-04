/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package api_test

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	commonmodels "github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/api"
	apiGenerated "github.com/openshift-kni/oran-o2ims/internal/service/resources/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/repo/generated"

	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

const (
	invalidField   = "invalidField"
	testLocationID = "LOC-001"
)

// mockSubscriptionEventHandler is a simple mock for testing
type mockSubscriptionEventHandler struct{}

func (m *mockSubscriptionEventHandler) SubscriptionEvent(_ context.Context, _ *notifier.SubscriptionEvent) {
}

func (m *mockSubscriptionEventHandler) GetClientFactory() notifier.ClientProvider {
	return nil
}

var _ = Describe("ResourceServer", func() {
	var (
		ctrl     *gomock.Controller
		mockRepo *generated.MockResourcesRepositoryInterface
		server   *api.ResourceServer
		ctx      context.Context
		testUUID uuid.UUID
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockRepo = generated.NewMockResourcesRepositoryInterface(ctrl)
		server = &api.ResourceServer{
			Repo:                     mockRepo,
			SubscriptionEventHandler: &mockSubscriptionEventHandler{},
		}
		ctx = context.Background()
		testUUID = uuid.New()
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	//
	// Metadata endpoints
	//
	Describe("GetAllVersions", func() {
		When("called", func() {
			It("returns 200 response with API versions", func() {

				resp, err := server.GetAllVersions(ctx, apiGenerated.GetAllVersionsRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetAllVersions200JSONResponse{}))
			})
		})
	})

	Describe("GetMinorVersions", func() {
		When("called", func() {
			It("returns 200 response with API versions", func() {

				resp, err := server.GetMinorVersions(ctx, apiGenerated.GetMinorVersionsRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetMinorVersions200JSONResponse{}))
			})
		})
	})

	Describe("GetCloudInfo", func() {
		When("called with valid parameters", func() {
			It("returns 200 response with O-Cloud info", func() {

				resp, err := server.GetCloudInfo(ctx, apiGenerated.GetCloudInfoRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetCloudInfo200JSONResponse{}))
			})
		})

		When("called with invalid field parameter", func() {
			It("returns 400 response", func() {
				field := invalidField
				resp, err := server.GetCloudInfo(ctx, apiGenerated.GetCloudInfoRequestObject{
					Params: apiGenerated.GetCloudInfoParams{
						Fields: &field,
					},
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetCloudInfo400ApplicationProblemPlusJSONResponse{}))
			})
		})
	})

	//
	// DeploymentManager endpoints
	//
	Describe("GetDeploymentManager", func() {
		When("deployment manager is found", func() {
			It("returns 200 response with deployment manager", func() {
				mockRepo.EXPECT().
					GetDeploymentManager(ctx, testUUID).
					Return(&models.DeploymentManager{DeploymentManagerID: testUUID, Name: "test-dm"}, nil)

				resp, err := server.GetDeploymentManager(ctx, apiGenerated.GetDeploymentManagerRequestObject{
					DeploymentManagerId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetDeploymentManager200JSONResponse{}))
			})
		})

		When("deployment manager not found", func() {
			It("returns 404 response", func() {
				mockRepo.EXPECT().
					GetDeploymentManager(ctx, testUUID).
					Return(nil, svcutils.ErrNotFound)

				resp, err := server.GetDeploymentManager(ctx, apiGenerated.GetDeploymentManagerRequestObject{
					DeploymentManagerId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetDeploymentManager404ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusNotFound))
			})
		})

		When("repository returns error", func() {
			It("returns 500 response", func() {
				mockRepo.EXPECT().
					GetDeploymentManager(ctx, testUUID).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetDeploymentManager(ctx, apiGenerated.GetDeploymentManagerRequestObject{
					DeploymentManagerId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetDeploymentManager500ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusInternalServerError))
			})
		})
	})

	Describe("GetDeploymentManagers", func() {
		When("deployment managers are found", func() {
			It("returns 200 response with list", func() {
				mockRepo.EXPECT().
					GetDeploymentManagers(ctx).
					Return([]models.DeploymentManager{
						{DeploymentManagerID: testUUID, Name: "dm-1"},
						{DeploymentManagerID: uuid.New(), Name: "dm-2"},
					}, nil)

				resp, err := server.GetDeploymentManagers(ctx, apiGenerated.GetDeploymentManagersRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetDeploymentManagers200JSONResponse{}))
				deploymentManagers := resp.(apiGenerated.GetDeploymentManagers200JSONResponse)
				Expect(deploymentManagers).To(HaveLen(2))
				Expect(deploymentManagers[0].Name).To(Equal("dm-1"))
				Expect(deploymentManagers[1].Name).To(Equal("dm-2"))
			})
		})

		When("no deployment managers exist", func() {
			It("returns 200 response with empty list", func() {
				mockRepo.EXPECT().
					GetDeploymentManagers(ctx).
					Return([]models.DeploymentManager{}, nil)

				resp, err := server.GetDeploymentManagers(ctx, apiGenerated.GetDeploymentManagersRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetDeploymentManagers200JSONResponse{}))
				Expect(resp.(apiGenerated.GetDeploymentManagers200JSONResponse)).To(HaveLen(0))
			})
		})

		When("repository returns error", func() {
			It("returns error", func() {
				mockRepo.EXPECT().
					GetDeploymentManagers(ctx).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetDeploymentManagers(ctx, apiGenerated.GetDeploymentManagersRequestObject{})

				// verify
				Expect(err).To(HaveOccurred())
				Expect(resp).To(BeNil())
			})
		})

		When("called with invalid field parameter", func() {
			It("returns 400 response", func() {
				field := invalidField

				resp, err := server.GetDeploymentManagers(ctx, apiGenerated.GetDeploymentManagersRequestObject{
					Params: apiGenerated.GetDeploymentManagersParams{
						Fields: &field,
					},
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetDeploymentManagers400ApplicationProblemPlusJSONResponse{}))
			})
		})
	})

	//
	// Subscription endpoints
	//
	Describe("GetSubscription", func() {
		When("subscription is found", func() {
			It("returns 200 response with subscription", func() {
				mockRepo.EXPECT().
					GetSubscription(ctx, testUUID).
					Return(&commonmodels.Subscription{SubscriptionID: &testUUID, Callback: "https://example.com/callback"}, nil)

				resp, err := server.GetSubscription(ctx, apiGenerated.GetSubscriptionRequestObject{
					SubscriptionId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetSubscription200JSONResponse{}))
			})
		})

		When("subscription not found", func() {
			It("returns 404 response", func() {
				mockRepo.EXPECT().
					GetSubscription(ctx, testUUID).
					Return(nil, svcutils.ErrNotFound)

				resp, err := server.GetSubscription(ctx, apiGenerated.GetSubscriptionRequestObject{
					SubscriptionId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetSubscription404ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusNotFound))
			})
		})

		When("repository returns error", func() {
			It("returns 500 response", func() {
				mockRepo.EXPECT().
					GetSubscription(ctx, testUUID).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetSubscription(ctx, apiGenerated.GetSubscriptionRequestObject{
					SubscriptionId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetSubscription500ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusInternalServerError))
			})
		})
	})

	Describe("GetSubscriptions", func() {
		When("subscriptions are found", func() {
			It("returns 200 response with list", func() {
				subID1 := uuid.New()
				subID2 := uuid.New()
				mockRepo.EXPECT().
					GetSubscriptions(ctx).
					Return([]commonmodels.Subscription{
						{SubscriptionID: &subID1, Callback: "https://example.com/callback1"},
						{SubscriptionID: &subID2, Callback: "https://example.com/callback2"},
					}, nil)

				resp, err := server.GetSubscriptions(ctx, apiGenerated.GetSubscriptionsRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetSubscriptions200JSONResponse{}))
				subscriptions := resp.(apiGenerated.GetSubscriptions200JSONResponse)
				Expect(subscriptions).To(HaveLen(2))
				Expect(subscriptions[0].Callback).To(Equal("https://example.com/callback1"))
				Expect(subscriptions[1].Callback).To(Equal("https://example.com/callback2"))
			})
		})

		When("no subscriptions exist", func() {
			It("returns 200 response with empty list", func() {
				mockRepo.EXPECT().
					GetSubscriptions(ctx).
					Return([]commonmodels.Subscription{}, nil)

				resp, err := server.GetSubscriptions(ctx, apiGenerated.GetSubscriptionsRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetSubscriptions200JSONResponse{}))
				Expect(resp.(apiGenerated.GetSubscriptions200JSONResponse)).To(HaveLen(0))
			})
		})

		When("repository returns error", func() {
			It("returns error", func() {
				mockRepo.EXPECT().
					GetSubscriptions(ctx).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetSubscriptions(ctx, apiGenerated.GetSubscriptionsRequestObject{})

				// verify
				Expect(err).To(HaveOccurred())
				Expect(resp).To(BeNil())
			})
		})

		When("called with invalid field parameter", func() {
			It("returns 400 response", func() {
				field := invalidField

				resp, err := server.GetSubscriptions(ctx, apiGenerated.GetSubscriptionsRequestObject{
					Params: apiGenerated.GetSubscriptionsParams{
						Fields: &field,
					},
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetSubscriptions400ApplicationProblemPlusJSONResponse{}))
			})
		})
	})

	Describe("DeleteSubscription", func() {
		When("subscription exists and is deleted", func() {
			It("returns 200 response", func() {
				mockRepo.EXPECT().
					DeleteSubscription(ctx, testUUID).
					Return(int64(1), nil)

				resp, err := server.DeleteSubscription(ctx, apiGenerated.DeleteSubscriptionRequestObject{
					SubscriptionId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.DeleteSubscription200Response{}))
			})
		})

		When("subscription not found", func() {
			It("returns 404 response", func() {
				mockRepo.EXPECT().
					DeleteSubscription(ctx, testUUID).
					Return(int64(0), nil)

				resp, err := server.DeleteSubscription(ctx, apiGenerated.DeleteSubscriptionRequestObject{
					SubscriptionId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.DeleteSubscription404ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusNotFound))
			})
		})

		When("repository returns error", func() {
			It("returns 500 response", func() {
				mockRepo.EXPECT().
					DeleteSubscription(ctx, testUUID).
					Return(int64(0), fmt.Errorf("db error"))

				resp, err := server.DeleteSubscription(ctx, apiGenerated.DeleteSubscriptionRequestObject{
					SubscriptionId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.DeleteSubscription500ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusInternalServerError))
			})
		})
	})

	//
	// ResourcePool endpoints
	//
	Describe("GetResourcePool", func() {
		When("resource pool is found", func() {
			It("returns 200 response with resource pool", func() {
				mockRepo.EXPECT().
					GetResourcePool(ctx, testUUID).
					Return(&models.ResourcePool{ResourcePoolID: testUUID, Name: "test-pool"}, nil)

				resp, err := server.GetResourcePool(ctx, apiGenerated.GetResourcePoolRequestObject{
					ResourcePoolId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetResourcePool200JSONResponse{}))
			})
		})

		When("resource pool not found", func() {
			It("returns 404 response", func() {
				mockRepo.EXPECT().
					GetResourcePool(ctx, testUUID).
					Return(nil, svcutils.ErrNotFound)

				resp, err := server.GetResourcePool(ctx, apiGenerated.GetResourcePoolRequestObject{
					ResourcePoolId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetResourcePool404ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusNotFound))
			})
		})

		When("repository returns error", func() {
			It("returns 500 response", func() {
				mockRepo.EXPECT().
					GetResourcePool(ctx, testUUID).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetResourcePool(ctx, apiGenerated.GetResourcePoolRequestObject{
					ResourcePoolId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetResourcePool500ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusInternalServerError))
			})
		})
	})

	Describe("GetResourcePools", func() {
		When("resource pools are found", func() {
			It("returns 200 response with list", func() {
				mockRepo.EXPECT().
					GetResourcePools(ctx).
					Return([]models.ResourcePool{
						{ResourcePoolID: testUUID, Name: "pool-1"},
						{ResourcePoolID: uuid.New(), Name: "pool-2"},
					}, nil)

				resp, err := server.GetResourcePools(ctx, apiGenerated.GetResourcePoolsRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetResourcePools200JSONResponse{}))
				resourcePools := resp.(apiGenerated.GetResourcePools200JSONResponse)
				Expect(resourcePools).To(HaveLen(2))
				Expect(resourcePools[0].Name).To(Equal("pool-1"))
				Expect(resourcePools[1].Name).To(Equal("pool-2"))
			})
		})

		When("no resource pools exist", func() {
			It("returns 200 response with empty list", func() {
				mockRepo.EXPECT().
					GetResourcePools(ctx).
					Return([]models.ResourcePool{}, nil)

				resp, err := server.GetResourcePools(ctx, apiGenerated.GetResourcePoolsRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetResourcePools200JSONResponse{}))
				Expect(resp.(apiGenerated.GetResourcePools200JSONResponse)).To(HaveLen(0))
			})
		})

		When("repository returns error", func() {
			It("returns error", func() {
				mockRepo.EXPECT().
					GetResourcePools(ctx).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetResourcePools(ctx, apiGenerated.GetResourcePoolsRequestObject{})

				// verify
				Expect(err).To(HaveOccurred())
				Expect(resp).To(BeNil())
			})
		})

		When("called with invalid field parameter", func() {
			It("returns 400 response", func() {
				field := invalidField

				resp, err := server.GetResourcePools(ctx, apiGenerated.GetResourcePoolsRequestObject{
					Params: apiGenerated.GetResourcePoolsParams{
						Fields: &field,
					},
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetResourcePools400ApplicationProblemPlusJSONResponse{}))
			})
		})
	})

	//
	// Resource endpoints
	//
	Describe("GetResource", func() {
		var poolID, resourceID uuid.UUID

		BeforeEach(func() {
			poolID = uuid.New()
			resourceID = uuid.New()
		})

		When("resource is found", func() {
			It("returns 200 response with resource", func() {
				mockRepo.EXPECT().
					ResourcePoolExists(ctx, poolID).
					Return(true, nil)
				mockRepo.EXPECT().
					GetResource(ctx, resourceID).
					Return(&models.Resource{ResourceID: resourceID, ResourcePoolID: poolID}, nil)

				resp, err := server.GetResource(ctx, apiGenerated.GetResourceRequestObject{
					ResourcePoolId: poolID,
					ResourceId:     resourceID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetResource200JSONResponse{}))
			})
		})

		When("resource pool does not exist", func() {
			It("returns 404 response", func() {
				mockRepo.EXPECT().
					ResourcePoolExists(ctx, poolID).
					Return(false, nil)

				resp, err := server.GetResource(ctx, apiGenerated.GetResourceRequestObject{
					ResourcePoolId: poolID,
					ResourceId:     resourceID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetResource404ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusNotFound))
				Expect(*problemResp.AdditionalAttributes).To(HaveKeyWithValue("resourcePoolId", poolID.String()))
			})
		})

		When("resource not found", func() {
			It("returns 404 response", func() {
				mockRepo.EXPECT().
					ResourcePoolExists(ctx, poolID).
					Return(true, nil)
				mockRepo.EXPECT().
					GetResource(ctx, resourceID).
					Return(nil, svcutils.ErrNotFound)

				resp, err := server.GetResource(ctx, apiGenerated.GetResourceRequestObject{
					ResourcePoolId: poolID,
					ResourceId:     resourceID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetResource404ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusNotFound))
			})
		})

		When("repository returns error checking pool", func() {
			It("returns 500 response", func() {
				mockRepo.EXPECT().
					ResourcePoolExists(ctx, poolID).
					Return(false, fmt.Errorf("db error"))

				resp, err := server.GetResource(ctx, apiGenerated.GetResourceRequestObject{
					ResourcePoolId: poolID,
					ResourceId:     resourceID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetResource500ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusInternalServerError))
			})
		})

		When("repository returns error getting resource", func() {
			It("returns 500 response", func() {
				mockRepo.EXPECT().
					ResourcePoolExists(ctx, poolID).
					Return(true, nil)
				mockRepo.EXPECT().
					GetResource(ctx, resourceID).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetResource(ctx, apiGenerated.GetResourceRequestObject{
					ResourcePoolId: poolID,
					ResourceId:     resourceID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetResource500ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusInternalServerError))
			})
		})
	})

	Describe("GetResources", func() {
		var poolID uuid.UUID

		BeforeEach(func() {
			poolID = uuid.New()
		})

		When("resources are found", func() {
			It("returns 200 response with list", func() {
				resourceID1 := uuid.New()
				resourceID2 := uuid.New()
				mockRepo.EXPECT().
					ResourcePoolExists(ctx, poolID).
					Return(true, nil)
				mockRepo.EXPECT().
					GetResourcePoolResources(ctx, poolID).
					Return([]models.Resource{
						{ResourceID: resourceID1, ResourcePoolID: poolID},
						{ResourceID: resourceID2, ResourcePoolID: poolID},
					}, nil)

				resp, err := server.GetResources(ctx, apiGenerated.GetResourcesRequestObject{
					ResourcePoolId: poolID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetResources200JSONResponse{}))
				resources := resp.(apiGenerated.GetResources200JSONResponse)
				Expect(resources).To(HaveLen(2))
				Expect(resources[0].ResourceId).To(Equal(resourceID1))
				Expect(resources[1].ResourceId).To(Equal(resourceID2))
			})
		})

		When("resource pool does not exist", func() {
			It("returns 404 response", func() {
				mockRepo.EXPECT().
					ResourcePoolExists(ctx, poolID).
					Return(false, nil)

				resp, err := server.GetResources(ctx, apiGenerated.GetResourcesRequestObject{
					ResourcePoolId: poolID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetResources404ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusNotFound))
			})
		})

		When("repository returns error", func() {
			It("returns 500 response", func() {
				mockRepo.EXPECT().
					ResourcePoolExists(ctx, poolID).
					Return(true, nil)
				mockRepo.EXPECT().
					GetResourcePoolResources(ctx, poolID).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetResources(ctx, apiGenerated.GetResourcesRequestObject{
					ResourcePoolId: poolID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetResources500ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusInternalServerError))
			})
		})

		When("called with invalid field parameter", func() {
			It("returns 400 response", func() {
				field := invalidField

				resp, err := server.GetResources(ctx, apiGenerated.GetResourcesRequestObject{
					ResourcePoolId: poolID,
					Params: apiGenerated.GetResourcesParams{
						Fields: &field,
					},
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetResources400ApplicationProblemPlusJSONResponse{}))
			})
		})
	})

	Describe("GetInternalResourceById", func() {
		When("resource is found", func() {
			It("returns 200 response with resource", func() {
				mockRepo.EXPECT().
					GetResource(ctx, testUUID).
					Return(&models.Resource{ResourceID: testUUID}, nil)

				resp, err := server.GetInternalResourceById(ctx, apiGenerated.GetInternalResourceByIdRequestObject{
					ResourceId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetInternalResourceById200JSONResponse{}))
			})
		})

		When("resource not found", func() {
			It("returns 404 response", func() {
				mockRepo.EXPECT().
					GetResource(ctx, testUUID).
					Return(nil, svcutils.ErrNotFound)

				resp, err := server.GetInternalResourceById(ctx, apiGenerated.GetInternalResourceByIdRequestObject{
					ResourceId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetInternalResourceById404ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusNotFound))
			})
		})

		When("repository returns error", func() {
			It("returns 500 response", func() {
				mockRepo.EXPECT().
					GetResource(ctx, testUUID).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetInternalResourceById(ctx, apiGenerated.GetInternalResourceByIdRequestObject{
					ResourceId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetInternalResourceById500ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusInternalServerError))
			})
		})
	})

	//
	// ResourceType endpoints
	//
	Describe("GetResourceType", func() {
		When("resource type is found without alarm dictionary", func() {
			It("returns 200 response with resource type", func() {
				mockRepo.EXPECT().
					GetResourceType(ctx, testUUID).
					Return(&models.ResourceType{ResourceTypeID: testUUID, Name: "test-type"}, nil)
				mockRepo.EXPECT().
					GetResourceTypeAlarmDictionary(ctx, testUUID).
					Return([]models.AlarmDictionary{}, nil)

				resp, err := server.GetResourceType(ctx, apiGenerated.GetResourceTypeRequestObject{
					ResourceTypeId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetResourceType200JSONResponse{}))
			})
		})

		When("resource type is found with alarm dictionary", func() {
			It("returns 200 response with resource type and dictionary", func() {
				dictID := uuid.New()
				mockRepo.EXPECT().
					GetResourceType(ctx, testUUID).
					Return(&models.ResourceType{ResourceTypeID: testUUID, Name: "test-type"}, nil)
				mockRepo.EXPECT().
					GetResourceTypeAlarmDictionary(ctx, testUUID).
					Return([]models.AlarmDictionary{{AlarmDictionaryID: dictID, ResourceTypeID: testUUID}}, nil)
				mockRepo.EXPECT().
					GetAlarmDefinitionsByAlarmDictionaryID(ctx, dictID).
					Return([]models.AlarmDefinition{}, nil)

				resp, err := server.GetResourceType(ctx, apiGenerated.GetResourceTypeRequestObject{
					ResourceTypeId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetResourceType200JSONResponse{}))
			})
		})

		When("resource type not found", func() {
			It("returns 404 response", func() {
				mockRepo.EXPECT().
					GetResourceType(ctx, testUUID).
					Return(nil, svcutils.ErrNotFound)

				resp, err := server.GetResourceType(ctx, apiGenerated.GetResourceTypeRequestObject{
					ResourceTypeId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetResourceType404ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusNotFound))
			})
		})

		When("repository returns error", func() {
			It("returns 500 response", func() {
				mockRepo.EXPECT().
					GetResourceType(ctx, testUUID).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetResourceType(ctx, apiGenerated.GetResourceTypeRequestObject{
					ResourceTypeId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetResourceType500ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusInternalServerError))
			})
		})
	})

	Describe("GetResourceTypes", func() {
		When("resource types are found without alarm dictionaries", func() {
			It("returns 200 response with list", func() {
				mockRepo.EXPECT().
					GetResourceTypes(ctx).
					Return([]models.ResourceType{
						{ResourceTypeID: testUUID, Name: "type-1"},
						{ResourceTypeID: uuid.New(), Name: "type-2"},
					}, nil)
				mockRepo.EXPECT().
					GetAlarmDictionaries(ctx).
					Return([]models.AlarmDictionary{}, nil)

				resp, err := server.GetResourceTypes(ctx, apiGenerated.GetResourceTypesRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetResourceTypes200JSONResponse{}))
				resourceTypes := resp.(apiGenerated.GetResourceTypes200JSONResponse)
				Expect(resourceTypes).To(HaveLen(2))
				Expect(resourceTypes[0].Name).To(Equal("type-1"))
				Expect(resourceTypes[1].Name).To(Equal("type-2"))
			})
		})

		When("resource types are found with alarm dictionaries", func() {
			It("returns 200 response with list including dictionaries", func() {
				typeID := uuid.New()
				dictID := uuid.New()
				mockRepo.EXPECT().
					GetResourceTypes(ctx).
					Return([]models.ResourceType{
						{ResourceTypeID: typeID, Name: "type-1"},
					}, nil)
				mockRepo.EXPECT().
					GetAlarmDictionaries(ctx).
					Return([]models.AlarmDictionary{
						{AlarmDictionaryID: dictID, ResourceTypeID: typeID},
					}, nil)
				mockRepo.EXPECT().
					GetAlarmDefinitionsByAlarmDictionaryID(ctx, dictID).
					Return([]models.AlarmDefinition{}, nil)

				resp, err := server.GetResourceTypes(ctx, apiGenerated.GetResourceTypesRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetResourceTypes200JSONResponse{}))
				resourceTypes := resp.(apiGenerated.GetResourceTypes200JSONResponse)
				Expect(resourceTypes).To(HaveLen(1))
				Expect(resourceTypes[0].Name).To(Equal("type-1"))
				Expect(resourceTypes[0].ResourceTypeId).To(Equal(typeID))
			})
		})

		When("no resource types exist", func() {
			It("returns 200 response with empty list", func() {
				mockRepo.EXPECT().
					GetResourceTypes(ctx).
					Return([]models.ResourceType{}, nil)
				mockRepo.EXPECT().
					GetAlarmDictionaries(ctx).
					Return([]models.AlarmDictionary{}, nil)

				resp, err := server.GetResourceTypes(ctx, apiGenerated.GetResourceTypesRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetResourceTypes200JSONResponse{}))
				Expect(resp.(apiGenerated.GetResourceTypes200JSONResponse)).To(HaveLen(0))
			})
		})

		When("repository returns error getting resource types", func() {
			It("returns error", func() {
				mockRepo.EXPECT().
					GetResourceTypes(ctx).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetResourceTypes(ctx, apiGenerated.GetResourceTypesRequestObject{})

				// verify
				Expect(err).To(HaveOccurred())
				Expect(resp).To(BeNil())
			})
		})

		When("repository returns error getting alarm dictionaries", func() {
			It("returns 500 response", func() {
				mockRepo.EXPECT().
					GetResourceTypes(ctx).
					Return([]models.ResourceType{
						{ResourceTypeID: testUUID, Name: "type-1"},
					}, nil)
				mockRepo.EXPECT().
					GetAlarmDictionaries(ctx).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetResourceTypes(ctx, apiGenerated.GetResourceTypesRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetResourceTypes500ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusInternalServerError))
			})
		})

		When("called with invalid field parameter", func() {
			It("returns 400 response", func() {
				field := invalidField

				resp, err := server.GetResourceTypes(ctx, apiGenerated.GetResourceTypesRequestObject{
					Params: apiGenerated.GetResourceTypesParams{
						Fields: &field,
					},
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetResourceTypes400ApplicationProblemPlusJSONResponse{}))
			})
		})
	})

	//
	// AlarmDictionary endpoints
	//
	Describe("GetAlarmDictionary", func() {
		When("alarm dictionary is found", func() {
			It("returns 200 response with alarm dictionary", func() {
				mockRepo.EXPECT().
					GetAlarmDictionary(ctx, testUUID).
					Return(&models.AlarmDictionary{AlarmDictionaryID: testUUID}, nil)
				mockRepo.EXPECT().
					GetAlarmDefinitionsByAlarmDictionaryID(ctx, testUUID).
					Return([]models.AlarmDefinition{}, nil)

				resp, err := server.GetAlarmDictionary(ctx, apiGenerated.GetAlarmDictionaryRequestObject{
					AlarmDictionaryId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetAlarmDictionary200JSONResponse{}))
			})
		})

		When("alarm dictionary not found", func() {
			It("returns 404 response", func() {
				mockRepo.EXPECT().
					GetAlarmDictionary(ctx, testUUID).
					Return(nil, svcutils.ErrNotFound)

				resp, err := server.GetAlarmDictionary(ctx, apiGenerated.GetAlarmDictionaryRequestObject{
					AlarmDictionaryId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetAlarmDictionary404ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusNotFound))
			})
		})

		When("repository returns error", func() {
			It("returns 500 response", func() {
				mockRepo.EXPECT().
					GetAlarmDictionary(ctx, testUUID).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetAlarmDictionary(ctx, apiGenerated.GetAlarmDictionaryRequestObject{
					AlarmDictionaryId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetAlarmDictionary500ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusInternalServerError))
			})
		})
	})

	Describe("GetAlarmDictionaries", func() {
		When("alarm dictionaries are found", func() {
			It("returns 200 response with list", func() {
				dictID1 := uuid.New()
				dictID2 := uuid.New()
				mockRepo.EXPECT().
					GetAlarmDictionaries(ctx).
					Return([]models.AlarmDictionary{
						{AlarmDictionaryID: dictID1},
						{AlarmDictionaryID: dictID2},
					}, nil)
				mockRepo.EXPECT().
					GetAlarmDefinitionsByAlarmDictionaryID(ctx, dictID1).
					Return([]models.AlarmDefinition{}, nil)
				mockRepo.EXPECT().
					GetAlarmDefinitionsByAlarmDictionaryID(ctx, dictID2).
					Return([]models.AlarmDefinition{}, nil)

				resp, err := server.GetAlarmDictionaries(ctx, apiGenerated.GetAlarmDictionariesRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetAlarmDictionaries200JSONResponse{}))
				alarmDictionaries := resp.(apiGenerated.GetAlarmDictionaries200JSONResponse)
				Expect(alarmDictionaries).To(HaveLen(2))
				Expect(alarmDictionaries[0].AlarmDictionaryId).To(Equal(dictID1))
				Expect(alarmDictionaries[1].AlarmDictionaryId).To(Equal(dictID2))
			})
		})

		When("no alarm dictionaries exist", func() {
			It("returns 200 response with empty list", func() {
				mockRepo.EXPECT().
					GetAlarmDictionaries(ctx).
					Return([]models.AlarmDictionary{}, nil)

				resp, err := server.GetAlarmDictionaries(ctx, apiGenerated.GetAlarmDictionariesRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetAlarmDictionaries200JSONResponse{}))
				Expect(resp.(apiGenerated.GetAlarmDictionaries200JSONResponse)).To(HaveLen(0))
			})
		})

		When("repository returns error", func() {
			It("returns error", func() {
				mockRepo.EXPECT().
					GetAlarmDictionaries(ctx).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetAlarmDictionaries(ctx, apiGenerated.GetAlarmDictionariesRequestObject{})

				// verify
				Expect(err).To(HaveOccurred())
				Expect(resp).To(BeNil())
			})
		})
	})

	Describe("GetResourceTypeAlarmDictionary", func() {
		When("alarm dictionary is found for resource type", func() {
			It("returns 200 response with alarm dictionary", func() {
				dictID := uuid.New()
				mockRepo.EXPECT().
					GetResourceTypeAlarmDictionary(ctx, testUUID).
					Return([]models.AlarmDictionary{{AlarmDictionaryID: dictID, ResourceTypeID: testUUID}}, nil)
				mockRepo.EXPECT().
					GetAlarmDefinitionsByAlarmDictionaryID(ctx, dictID).
					Return([]models.AlarmDefinition{}, nil)

				resp, err := server.GetResourceTypeAlarmDictionary(ctx, apiGenerated.GetResourceTypeAlarmDictionaryRequestObject{
					ResourceTypeId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetResourceTypeAlarmDictionary200JSONResponse{}))
			})
		})

		When("no alarm dictionary exists for resource type", func() {
			It("returns 404 response", func() {
				mockRepo.EXPECT().
					GetResourceTypeAlarmDictionary(ctx, testUUID).
					Return([]models.AlarmDictionary{}, nil)

				resp, err := server.GetResourceTypeAlarmDictionary(ctx, apiGenerated.GetResourceTypeAlarmDictionaryRequestObject{
					ResourceTypeId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetResourceTypeAlarmDictionary404ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusNotFound))
			})
		})

		When("repository returns error", func() {
			It("returns 500 response", func() {
				mockRepo.EXPECT().
					GetResourceTypeAlarmDictionary(ctx, testUUID).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetResourceTypeAlarmDictionary(ctx, apiGenerated.GetResourceTypeAlarmDictionaryRequestObject{
					ResourceTypeId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetResourceTypeAlarmDictionary500ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusInternalServerError))
			})
		})
	})

	//
	// Location endpoints
	//
	Describe("GetLocation", func() {
		var globalLocationID string

		BeforeEach(func() {
			globalLocationID = testLocationID
		})

		When("location is found", func() {
			It("returns 200 response with location", func() {
				mockRepo.EXPECT().
					GetLocation(ctx, globalLocationID).
					Return(&models.Location{GlobalLocationID: globalLocationID, Name: "Test Location"}, nil)
				mockRepo.EXPECT().
					GetOCloudSiteIDsForLocation(ctx, globalLocationID).
					Return([]uuid.UUID{uuid.New()}, nil)

				resp, err := server.GetLocation(ctx, apiGenerated.GetLocationRequestObject{
					GlobalLocationId: globalLocationID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetLocation200JSONResponse{}))
			})
		})

		When("location not found", func() {
			It("returns 404 response", func() {
				mockRepo.EXPECT().
					GetLocation(ctx, globalLocationID).
					Return(nil, svcutils.ErrNotFound)

				resp, err := server.GetLocation(ctx, apiGenerated.GetLocationRequestObject{
					GlobalLocationId: globalLocationID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetLocation404ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusNotFound))
			})
		})

		When("repository returns error getting location", func() {
			It("returns 500 response", func() {
				mockRepo.EXPECT().
					GetLocation(ctx, globalLocationID).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetLocation(ctx, apiGenerated.GetLocationRequestObject{
					GlobalLocationId: globalLocationID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetLocation500ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusInternalServerError))
			})
		})

		When("repository returns error getting site IDs", func() {
			It("returns 500 response", func() {
				mockRepo.EXPECT().
					GetLocation(ctx, globalLocationID).
					Return(&models.Location{GlobalLocationID: globalLocationID, Name: "Test Location"}, nil)
				mockRepo.EXPECT().
					GetOCloudSiteIDsForLocation(ctx, globalLocationID).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetLocation(ctx, apiGenerated.GetLocationRequestObject{
					GlobalLocationId: globalLocationID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetLocation500ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusInternalServerError))
			})
		})
	})

	Describe("GetLocations", func() {
		When("locations are found", func() {
			It("returns 200 response with list", func() {
				loc1 := testLocationID
				loc2 := "LOC-002"
				mockRepo.EXPECT().
					GetLocations(ctx).
					Return([]models.Location{
						{GlobalLocationID: loc1, Name: "Location 1"},
						{GlobalLocationID: loc2, Name: "Location 2"},
					}, nil)
				mockRepo.EXPECT().
					GetOCloudSiteIDsForLocation(ctx, loc1).
					Return([]uuid.UUID{uuid.New()}, nil)
				mockRepo.EXPECT().
					GetOCloudSiteIDsForLocation(ctx, loc2).
					Return([]uuid.UUID{}, nil)

				resp, err := server.GetLocations(ctx, apiGenerated.GetLocationsRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetLocations200JSONResponse{}))
				locations := resp.(apiGenerated.GetLocations200JSONResponse)
				Expect(locations).To(HaveLen(2))
				Expect(locations[0].Name).To(Equal("Location 1"))
				Expect(locations[1].Name).To(Equal("Location 2"))
			})
		})

		When("no locations exist", func() {
			It("returns 200 response with empty list", func() {
				mockRepo.EXPECT().
					GetLocations(ctx).
					Return([]models.Location{}, nil)

				resp, err := server.GetLocations(ctx, apiGenerated.GetLocationsRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetLocations200JSONResponse{}))
				Expect(resp.(apiGenerated.GetLocations200JSONResponse)).To(HaveLen(0))
			})
		})

		When("repository returns error", func() {
			It("returns 500 response", func() {
				mockRepo.EXPECT().
					GetLocations(ctx).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetLocations(ctx, apiGenerated.GetLocationsRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetLocations500ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusInternalServerError))
			})
		})

		When("GetOCloudSiteIDsForLocation fails during iteration", func() {
			It("returns 500 response with the failing location ID", func() {
				loc1 := testLocationID
				loc2 := "LOC-002"
				mockRepo.EXPECT().
					GetLocations(ctx).
					Return([]models.Location{
						{GlobalLocationID: loc1, Name: "Location 1"},
						{GlobalLocationID: loc2, Name: "Location 2"},
					}, nil)
				// First location succeeds
				mockRepo.EXPECT().
					GetOCloudSiteIDsForLocation(ctx, loc1).
					Return([]uuid.UUID{uuid.New()}, nil)
				// Second location fails (e.g., DB connection dropped mid-iteration)
				mockRepo.EXPECT().
					GetOCloudSiteIDsForLocation(ctx, loc2).
					Return(nil, fmt.Errorf("connection timeout"))

				resp, err := server.GetLocations(ctx, apiGenerated.GetLocationsRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetLocations500ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusInternalServerError))
				Expect(problemResp.Detail).To(ContainSubstring("connection timeout"))
				Expect((*problemResp.AdditionalAttributes)["globalLocationId"]).To(Equal(loc2))
			})
		})
	})

	//
	// OCloudSite endpoints
	//
	Describe("GetOCloudSite", func() {
		When("O-Cloud site is found", func() {
			It("returns 200 response with site", func() {
				mockRepo.EXPECT().
					GetOCloudSite(ctx, testUUID).
					Return(&models.OCloudSite{OCloudSiteID: testUUID, Name: "test-site"}, nil)
				mockRepo.EXPECT().
					GetResourcePoolIDsForSite(ctx, testUUID).
					Return([]uuid.UUID{uuid.New()}, nil)

				resp, err := server.GetOCloudSite(ctx, apiGenerated.GetOCloudSiteRequestObject{
					OCloudSiteId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetOCloudSite200JSONResponse{}))
			})
		})

		When("O-Cloud site not found", func() {
			It("returns 404 response", func() {
				mockRepo.EXPECT().
					GetOCloudSite(ctx, testUUID).
					Return(nil, svcutils.ErrNotFound)

				resp, err := server.GetOCloudSite(ctx, apiGenerated.GetOCloudSiteRequestObject{
					OCloudSiteId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetOCloudSite404ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusNotFound))
			})
		})

		When("repository returns error getting site", func() {
			It("returns 500 response", func() {
				mockRepo.EXPECT().
					GetOCloudSite(ctx, testUUID).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetOCloudSite(ctx, apiGenerated.GetOCloudSiteRequestObject{
					OCloudSiteId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetOCloudSite500ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusInternalServerError))
			})
		})

		When("repository returns error getting pool IDs", func() {
			It("returns 500 response", func() {
				mockRepo.EXPECT().
					GetOCloudSite(ctx, testUUID).
					Return(&models.OCloudSite{OCloudSiteID: testUUID, Name: "test-site"}, nil)
				mockRepo.EXPECT().
					GetResourcePoolIDsForSite(ctx, testUUID).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetOCloudSite(ctx, apiGenerated.GetOCloudSiteRequestObject{
					OCloudSiteId: testUUID,
				})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetOCloudSite500ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusInternalServerError))
			})
		})
	})

	Describe("GetOCloudSites", func() {
		When("O-Cloud sites are found", func() {
			It("returns 200 response with list", func() {
				siteID1 := uuid.New()
				siteID2 := uuid.New()
				mockRepo.EXPECT().
					GetOCloudSites(ctx).
					Return([]models.OCloudSite{
						{OCloudSiteID: siteID1, Name: "site-1"},
						{OCloudSiteID: siteID2, Name: "site-2"},
					}, nil)
				mockRepo.EXPECT().
					GetResourcePoolIDsForSite(ctx, siteID1).
					Return([]uuid.UUID{uuid.New()}, nil)
				mockRepo.EXPECT().
					GetResourcePoolIDsForSite(ctx, siteID2).
					Return([]uuid.UUID{}, nil)

				resp, err := server.GetOCloudSites(ctx, apiGenerated.GetOCloudSitesRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetOCloudSites200JSONResponse{}))
				sites := resp.(apiGenerated.GetOCloudSites200JSONResponse)
				Expect(sites).To(HaveLen(2))
				Expect(sites[0].Name).To(Equal("site-1"))
				Expect(sites[1].Name).To(Equal("site-2"))
			})
		})

		When("no O-Cloud sites exist", func() {
			It("returns 200 response with empty list", func() {
				mockRepo.EXPECT().
					GetOCloudSites(ctx).
					Return([]models.OCloudSite{}, nil)

				resp, err := server.GetOCloudSites(ctx, apiGenerated.GetOCloudSitesRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apiGenerated.GetOCloudSites200JSONResponse{}))
				Expect(resp.(apiGenerated.GetOCloudSites200JSONResponse)).To(HaveLen(0))
			})
		})

		When("repository returns error", func() {
			It("returns 500 response", func() {
				mockRepo.EXPECT().
					GetOCloudSites(ctx).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetOCloudSites(ctx, apiGenerated.GetOCloudSitesRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetOCloudSites500ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusInternalServerError))
			})
		})

		When("GetResourcePoolIDsForSite fails during iteration", func() {
			It("returns 500 response with the failing site ID", func() {
				siteID1 := uuid.New()
				siteID2 := uuid.New()
				mockRepo.EXPECT().
					GetOCloudSites(ctx).
					Return([]models.OCloudSite{
						{OCloudSiteID: siteID1, Name: "site-1"},
						{OCloudSiteID: siteID2, Name: "site-2"},
					}, nil)
				// First site succeeds
				mockRepo.EXPECT().
					GetResourcePoolIDsForSite(ctx, siteID1).
					Return([]uuid.UUID{uuid.New()}, nil)
				// Second site fails (e.g., query timeout on large table)
				mockRepo.EXPECT().
					GetResourcePoolIDsForSite(ctx, siteID2).
					Return(nil, fmt.Errorf("query timeout"))

				resp, err := server.GetOCloudSites(ctx, apiGenerated.GetOCloudSitesRequestObject{})

				// verify
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(apiGenerated.GetOCloudSites500ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusInternalServerError))
				Expect(problemResp.Detail).To(ContainSubstring("query timeout"))
				Expect((*problemResp.AdditionalAttributes)["oCloudSiteId"]).To(Equal(siteID2.String()))
			})
		})
	})
})
