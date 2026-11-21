/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	commonapi "github.com/openshift-kni/oran-o2ims/api/common"
	apigenerated "github.com/openshift-kni/oran-o2ims/internal/service/cluster/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/db/repo/generated"
	svcapi "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

type mockClientProvider struct {
	client *http.Client
}

func (m *mockClientProvider) NewClient(_ context.Context, _ commonapi.AuthType) (*http.Client, error) {
	return m.client, nil
}

type mockSubscriptionEventHandler struct {
	provider notifier.ClientProvider
}

func (m *mockSubscriptionEventHandler) SubscriptionEvent(_ context.Context, _ *notifier.SubscriptionEvent) {
}

func (m *mockSubscriptionEventHandler) GetClientFactory() notifier.ClientProvider {
	return m.provider
}

var _ = Describe("Cluster Server", func() {
	var (
		ctrl     *gomock.Controller
		mockRepo *generated.MockRepositoryInterface
		server   *ClusterServer
		ctx      context.Context
		testUUID uuid.UUID
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockRepo = generated.NewMockRepositoryInterface(ctrl)
		server = &ClusterServer{
			Config: &ClusterServerConfig{},
			Repo:   mockRepo,
		}
		server.InitAlarmDictCache()
		ctx = context.Background()
		testUUID = uuid.New()
	})

	Describe("GetNodeClusterTypeAlarmDictionary", func() {
		When("cache loading fails", func() {
			It("returns internal server error", func() {
				mockRepo.EXPECT().
					GetAlarmDictionaries(gomock.Any()).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetNodeClusterTypeAlarmDictionary(ctx, apigenerated.GetNodeClusterTypeAlarmDictionaryRequestObject{
					NodeClusterTypeId: testUUID,
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetNodeClusterTypeAlarmDictionary500ApplicationProblemPlusJSONResponse{}))
				Expect(resp.(apigenerated.GetNodeClusterTypeAlarmDictionary500ApplicationProblemPlusJSONResponse).Status).To(Equal(http.StatusInternalServerError))
			})
		})

		When("node cluster type not found in cache", func() {
			It("returns 404 not found response", func() {
				mockRepo.EXPECT().
					GetAlarmDictionaries(gomock.Any()).
					Return([]models.AlarmDictionary{}, nil)
				mockRepo.EXPECT().
					GetThanosAlarmDefinitions(gomock.Any()).
					Return([]models.AlarmDefinition{}, nil)

				resp, err := server.GetNodeClusterTypeAlarmDictionary(ctx, apigenerated.GetNodeClusterTypeAlarmDictionaryRequestObject{
					NodeClusterTypeId: testUUID,
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetNodeClusterTypeAlarmDictionary404ApplicationProblemPlusJSONResponse{}))
				Expect(resp.(apigenerated.GetNodeClusterTypeAlarmDictionary404ApplicationProblemPlusJSONResponse).Status).To(Equal(http.StatusNotFound))
			})
		})

		When("alarm dictionary is found for node cluster type", func() {
			It("returns 200 OK", func() {
				dictID := uuid.New()
				mockRepo.EXPECT().
					GetAlarmDictionaries(gomock.Any()).
					Return([]models.AlarmDictionary{{AlarmDictionaryID: dictID, NodeClusterTypeID: testUUID}}, nil)
				mockRepo.EXPECT().
					GetThanosAlarmDefinitions(gomock.Any()).
					Return([]models.AlarmDefinition{}, nil)
				mockRepo.EXPECT().
					GetAlarmDefinitionsByAlarmDictionaryID(gomock.Any(), dictID).
					Return([]models.AlarmDefinition{}, nil)

				resp, err := server.GetNodeClusterTypeAlarmDictionary(ctx, apigenerated.GetNodeClusterTypeAlarmDictionaryRequestObject{
					NodeClusterTypeId: testUUID,
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetNodeClusterTypeAlarmDictionary200JSONResponse{}))
				Expect(resp.(apigenerated.GetNodeClusterTypeAlarmDictionary200JSONResponse).AlarmDictionaryId).To(Equal(dictID))
			})
		})
	})

	Describe("Alarm dictionary cache mid-loader failure", func() {
		It("returns 500 when alarm definitions query fails during cache load", func() {
			dictID := uuid.New()
			mockRepo.EXPECT().
				GetAlarmDictionaries(gomock.Any()).
				Return([]models.AlarmDictionary{{AlarmDictionaryID: dictID}}, nil)
			mockRepo.EXPECT().
				GetThanosAlarmDefinitions(gomock.Any()).
				Return([]models.AlarmDefinition{}, nil)
			mockRepo.EXPECT().
				GetAlarmDefinitionsByAlarmDictionaryID(gomock.Any(), dictID).
				Return(nil, fmt.Errorf("definitions query failed"))

			resp, err := server.GetAlarmDictionaries(ctx, apigenerated.GetAlarmDictionariesRequestObject{})

			Expect(err).ToNot(HaveOccurred())
			Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetAlarmDictionaries500ApplicationProblemPlusJSONResponse{}))
		})
	})

	Describe("GetAlarmDictionaries", func() {
		When("cache loading fails", func() {
			It("returns internal server error", func() {
				mockRepo.EXPECT().
					GetAlarmDictionaries(gomock.Any()).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetAlarmDictionaries(ctx, apigenerated.GetAlarmDictionariesRequestObject{})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetAlarmDictionaries500ApplicationProblemPlusJSONResponse{}))
				Expect(resp.(apigenerated.GetAlarmDictionaries500ApplicationProblemPlusJSONResponse).Status).To(Equal(http.StatusInternalServerError))
			})
		})
		When("repository does not have alarm dictionaries", func() {
			It("returns 200 OK empty list", func() {
				mockRepo.EXPECT().
					GetAlarmDictionaries(gomock.Any()).
					Return([]models.AlarmDictionary{}, nil)
				mockRepo.EXPECT().
					GetThanosAlarmDefinitions(gomock.Any()).
					Return([]models.AlarmDefinition{}, nil)

				resp, err := server.GetAlarmDictionaries(ctx, apigenerated.GetAlarmDictionariesRequestObject{})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetAlarmDictionaries200JSONResponse{}))
				Expect(resp.(apigenerated.GetAlarmDictionaries200JSONResponse)).To(HaveLen(0))
			})
		})

		When("alarm dictionary and definitions are found", func() {
			alarmDefinitionUUID := uuid.New()

			It("returns 200 OK", func() {
				mockRepo.EXPECT().
					GetAlarmDictionaries(gomock.Any()).
					Return([]models.AlarmDictionary{
						{
							AlarmDictionaryID: testUUID,
						},
					}, nil)

				mockRepo.EXPECT().
					GetThanosAlarmDefinitions(gomock.Any()).
					Return([]models.AlarmDefinition{}, nil)

				mockRepo.EXPECT().
					GetAlarmDefinitionsByAlarmDictionaryID(gomock.Any(), testUUID).
					Return([]models.AlarmDefinition{
						{
							AlarmDefinitionID: alarmDefinitionUUID,
						},
					}, nil)

				resp, err := server.GetAlarmDictionaries(ctx, apigenerated.GetAlarmDictionariesRequestObject{})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetAlarmDictionaries200JSONResponse{}))
				Expect(resp.(apigenerated.GetAlarmDictionaries200JSONResponse)).To(HaveLen(1))
				Expect(resp.(apigenerated.GetAlarmDictionaries200JSONResponse)[0].AlarmDictionaryId).To(Equal(testUUID))
				Expect(resp.(apigenerated.GetAlarmDictionaries200JSONResponse)[0].AlarmDefinition).To(HaveLen(1))
				Expect(resp.(apigenerated.GetAlarmDictionaries200JSONResponse)[0].AlarmDefinition[0].AlarmDefinitionId).To(Equal(alarmDefinitionUUID))
			})
		})

		When("GetThanosAlarmDefinitions returns error", func() {
			It("returns 500 internal server error", func() {
				mockRepo.EXPECT().
					GetAlarmDictionaries(gomock.Any()).
					Return([]models.AlarmDictionary{}, nil)
				mockRepo.EXPECT().
					GetThanosAlarmDefinitions(gomock.Any()).
					Return(nil, fmt.Errorf("thanos db error"))

				resp, err := server.GetAlarmDictionaries(ctx, apigenerated.GetAlarmDictionariesRequestObject{})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetAlarmDictionaries500ApplicationProblemPlusJSONResponse{}))
				Expect(resp.(apigenerated.GetAlarmDictionaries500ApplicationProblemPlusJSONResponse).Detail).To(ContainSubstring("thanos"))
			})
		})
	})

	Describe("GetAlarmDictionary", func() {
		When("cache loading fails", func() {
			It("returns internal server error", func() {
				mockRepo.EXPECT().
					GetAlarmDictionaries(gomock.Any()).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetAlarmDictionary(ctx, apigenerated.GetAlarmDictionaryRequestObject{
					AlarmDictionaryId: testUUID,
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetAlarmDictionary500ApplicationProblemPlusJSONResponse{}))
				Expect(resp.(apigenerated.GetAlarmDictionary500ApplicationProblemPlusJSONResponse).Status).To(Equal(http.StatusInternalServerError))
			})
		})

		When("alarm dictionary not found in cache", func() {
			It("returns 404 not found response", func() {
				mockRepo.EXPECT().
					GetAlarmDictionaries(gomock.Any()).
					Return([]models.AlarmDictionary{}, nil)
				mockRepo.EXPECT().
					GetThanosAlarmDefinitions(gomock.Any()).
					Return([]models.AlarmDefinition{}, nil)

				resp, err := server.GetAlarmDictionary(ctx, apigenerated.GetAlarmDictionaryRequestObject{
					AlarmDictionaryId: testUUID,
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetAlarmDictionary404ApplicationProblemPlusJSONResponse{}))
				Expect(resp.(apigenerated.GetAlarmDictionary404ApplicationProblemPlusJSONResponse).Status).To(Equal(http.StatusNotFound))
			})
		})

		When("alarm dictionary is found in cache", func() {
			alarmDefinitionUUID := uuid.New()

			It("returns 200 OK", func() {
				mockRepo.EXPECT().
					GetAlarmDictionaries(gomock.Any()).
					Return([]models.AlarmDictionary{{AlarmDictionaryID: testUUID}}, nil)
				mockRepo.EXPECT().
					GetThanosAlarmDefinitions(gomock.Any()).
					Return([]models.AlarmDefinition{}, nil)
				mockRepo.EXPECT().
					GetAlarmDefinitionsByAlarmDictionaryID(gomock.Any(), testUUID).
					Return([]models.AlarmDefinition{{AlarmDefinitionID: alarmDefinitionUUID}}, nil)

				resp, err := server.GetAlarmDictionary(ctx, apigenerated.GetAlarmDictionaryRequestObject{
					AlarmDictionaryId: testUUID,
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetAlarmDictionary200JSONResponse{}))
				Expect(resp.(apigenerated.GetAlarmDictionary200JSONResponse).AlarmDictionaryId).To(Equal(testUUID))
				Expect(resp.(apigenerated.GetAlarmDictionary200JSONResponse).AlarmDefinition).To(HaveLen(1))
			})
		})

		When("alarm dictionary found and Thanos definitions exist", func() {
			It("appends Thanos definitions to the dictionary", func() {
				dictDefUUID := uuid.New()
				thanosDefUUID := uuid.New()

				mockRepo.EXPECT().
					GetAlarmDictionaries(gomock.Any()).
					Return([]models.AlarmDictionary{{AlarmDictionaryID: testUUID}}, nil)
				mockRepo.EXPECT().
					GetThanosAlarmDefinitions(gomock.Any()).
					Return([]models.AlarmDefinition{
						{AlarmDefinitionID: thanosDefUUID, AlarmName: "ViolatedPolicyReport"},
					}, nil)
				mockRepo.EXPECT().
					GetAlarmDefinitionsByAlarmDictionaryID(gomock.Any(), testUUID).
					Return([]models.AlarmDefinition{
						{AlarmDefinitionID: dictDefUUID},
					}, nil)

				resp, err := server.GetAlarmDictionary(ctx, apigenerated.GetAlarmDictionaryRequestObject{
					AlarmDictionaryId: testUUID,
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetAlarmDictionary200JSONResponse{}))
				Expect(resp.(apigenerated.GetAlarmDictionary200JSONResponse).AlarmDefinition).To(HaveLen(2))
			})
		})
	})

	Describe("GetClusterResourceGroups", func() {
		When("the repository query fails", func() {
			It("returns an error", func() {
				mockRepo.EXPECT().
					GetClusterResourceGroups(gomock.Any()).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetClusterResourceGroups(ctx, apigenerated.GetClusterResourceGroupsRequestObject{})

				Expect(err).To(HaveOccurred())
				Expect(resp).To(BeNil())
			})
		})

		When("an unknown field is requested", func() {
			It("returns 400 bad request", func() {
				fields := "doesNotExist"

				resp, err := server.GetClusterResourceGroups(ctx, apigenerated.GetClusterResourceGroupsRequestObject{
					Params: apigenerated.GetClusterResourceGroupsParams{Fields: &fields},
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetClusterResourceGroups400ApplicationProblemPlusJSONResponse{}))
				Expect(resp.(apigenerated.GetClusterResourceGroups400ApplicationProblemPlusJSONResponse).Status).To(Equal(http.StatusBadRequest))
			})
		})

		When("no groups exist", func() {
			It("returns 200 OK with an empty list", func() {
				mockRepo.EXPECT().
					GetClusterResourceGroups(gomock.Any()).
					Return([]models.ClusterResourceGroup{}, nil)

				resp, err := server.GetClusterResourceGroups(ctx, apigenerated.GetClusterResourceGroupsRequestObject{})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetClusterResourceGroups200JSONResponse{}))
				Expect(resp.(apigenerated.GetClusterResourceGroups200JSONResponse)).To(BeEmpty())
			})
		})

		When("groups exist", func() {
			It("returns 200 OK with the groups and their members", func() {
				memberID := uuid.New()
				mockRepo.EXPECT().
					GetClusterResourceGroups(gomock.Any()).
					Return([]models.ClusterResourceGroup{{
						ClusterResourceGroupID: testUUID,
						Name:                   "worker",
						Description:            "worker node group of cluster test",
						ClusterResources:       []uuid.UUID{memberID},
					}}, nil)

				resp, err := server.GetClusterResourceGroups(ctx, apigenerated.GetClusterResourceGroupsRequestObject{})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetClusterResourceGroups200JSONResponse{}))
				groups := resp.(apigenerated.GetClusterResourceGroups200JSONResponse)
				Expect(groups).To(HaveLen(1))
				Expect(groups[0].ClusterResourceGroupId).To(Equal(testUUID))
				Expect(groups[0].Name).To(Equal("worker"))
				Expect(*groups[0].ClusterResources).To(Equal([]uuid.UUID{memberID}))
			})
		})
	})

	Describe("GetClusterResourceGroup", func() {
		When("the repository query fails", func() {
			It("returns internal server error", func() {
				mockRepo.EXPECT().
					GetClusterResourceGroup(gomock.Any(), testUUID).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetClusterResourceGroup(ctx, apigenerated.GetClusterResourceGroupRequestObject{
					ClusterResourceGroupId: testUUID,
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetClusterResourceGroup500ApplicationProblemPlusJSONResponse{}))
				Expect(resp.(apigenerated.GetClusterResourceGroup500ApplicationProblemPlusJSONResponse).Status).To(Equal(http.StatusInternalServerError))
			})
		})

		When("the group is not found", func() {
			It("returns 404 not found response", func() {
				mockRepo.EXPECT().
					GetClusterResourceGroup(gomock.Any(), testUUID).
					Return(nil, svcutils.ErrNotFound)

				resp, err := server.GetClusterResourceGroup(ctx, apigenerated.GetClusterResourceGroupRequestObject{
					ClusterResourceGroupId: testUUID,
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetClusterResourceGroup404ApplicationProblemPlusJSONResponse{}))
				Expect(resp.(apigenerated.GetClusterResourceGroup404ApplicationProblemPlusJSONResponse).Status).To(Equal(http.StatusNotFound))
			})
		})

		When("the group is found", func() {
			It("returns 200 OK", func() {
				mockRepo.EXPECT().
					GetClusterResourceGroup(gomock.Any(), testUUID).
					Return(&models.ClusterResourceGroup{
						ClusterResourceGroupID: testUUID,
						Name:                   "master",
						Description:            "master node group of cluster test",
						ClusterResources:       []uuid.UUID{uuid.New()},
					}, nil)

				resp, err := server.GetClusterResourceGroup(ctx, apigenerated.GetClusterResourceGroupRequestObject{
					ClusterResourceGroupId: testUUID,
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetClusterResourceGroup200JSONResponse{}))
				Expect(resp.(apigenerated.GetClusterResourceGroup200JSONResponse).ClusterResourceGroupId).To(Equal(testUUID))
				Expect(resp.(apigenerated.GetClusterResourceGroup200JSONResponse).Name).To(Equal("master"))
			})
		})
	})

	Describe("OpenAPI specification", func() {
		It("is valid", func() {
			spec, err := apigenerated.GetSpec()
			Expect(err).ToNot(HaveOccurred())
			Expect(spec.Validate(ctx)).To(Succeed())
		})
	})

	Describe("CreateSubscription", func() {
		When("validation fails due to invalid callback URL scheme", func() {
			It("returns 400 without reflecting the callback or filter in AdditionalAttributes", func() {
				server.SubscriptionEventHandler = &mockSubscriptionEventHandler{}

				callbackURL := "http://malicious.example.com/callback?secret=token123"
				filterValue := "sensitive-filter-value"
				resp, err := server.CreateSubscription(ctx, apigenerated.CreateSubscriptionRequestObject{
					Body: &apigenerated.Subscription{
						Callback: callbackURL,
						Filter:   &filterValue,
					},
				})

				Expect(err).ToNot(HaveOccurred())
				problemResp := resp.(apigenerated.CreateSubscription400ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusBadRequest))
				Expect(problemResp.AdditionalAttributes).ToNot(BeNil())
				attrs := *problemResp.AdditionalAttributes
				Expect(attrs).ToNot(HaveKey("callback"))
				Expect(attrs).ToNot(HaveKey("filter"))
				Expect(problemResp.Detail).ToNot(ContainSubstring(callbackURL))
			})
		})

		When("validation fails due to unreachable callback URL", func() {
			It("returns 400 without reflecting the callback URL in Detail", func() {
				server.Config.SmoURL = "https://192.0.2.1"
				callbackURL := "https://192.0.2.1/callback?secret=token123"
				shortTimeoutClient := &http.Client{Timeout: 1}

				server.SubscriptionEventHandler = &mockSubscriptionEventHandler{
					provider: &mockClientProvider{client: shortTimeoutClient},
				}

				resp, err := server.CreateSubscription(ctx, apigenerated.CreateSubscriptionRequestObject{
					Body: &apigenerated.Subscription{
						Callback: callbackURL,
					},
				})

				Expect(err).ToNot(HaveOccurred())
				problemResp := resp.(apigenerated.CreateSubscription400ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusBadRequest))
				Expect(problemResp.Detail).ToNot(ContainSubstring(callbackURL))
				Expect(problemResp.Detail).ToNot(ContainSubstring("192.0.2.1"))
				Expect(problemResp.Detail).ToNot(ContainSubstring("secret=token123"))
			})
		})

		When("repository returns unique_callback error", func() {
			It("returns 400 without reflecting the callback in AdditionalAttributes", func() {
				ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNoContent)
				}))
				defer ts.Close()

				server.Config.SmoURL = ts.URL
				savedResolver := svcapi.DefaultResolver
				svcapi.DefaultResolver = &svcapi.TestResolver{Addrs: map[string][]string{
					"127.0.0.1": {"203.0.113.1"},
				}}
				DeferCleanup(func() { svcapi.DefaultResolver = savedResolver })

				server.SubscriptionEventHandler = &mockSubscriptionEventHandler{
					provider: &mockClientProvider{client: ts.Client()},
				}

				mockRepo.EXPECT().
					CreateSubscription(gomock.Any(), gomock.Any()).
					Return(nil, fmt.Errorf("unique_callback constraint violation"))

				resp, err := server.CreateSubscription(ctx, apigenerated.CreateSubscriptionRequestObject{
					Body: &apigenerated.Subscription{
						Callback: ts.URL + "/callback",
					},
				})

				Expect(err).ToNot(HaveOccurred())
				problemResp := resp.(apigenerated.CreateSubscription400ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusBadRequest))
				Expect(problemResp.Detail).To(Equal("callback value must be unique"))
				Expect(problemResp.AdditionalAttributes).ToNot(BeNil())
				attrs := *problemResp.AdditionalAttributes
				Expect(attrs).ToNot(HaveKey("callback"))
			})
		})
	})

})
