/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	apigenerated "github.com/openshift-kni/oran-o2ims/internal/service/cluster/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/db/repo/generated"
)

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
			Repo: mockRepo,
		}
		server.InitAlarmDictCache()
		ctx = context.Background()
		testUUID = uuid.New()
	})

	Describe("GetNodeClusterTypeAlarmDictionary", func() {
		When("cache loading fails", func() {
			It("returns internal server error", func() {
				mockRepo.EXPECT().
					GetAlarmDictionaries(ctx).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetNodeClusterTypeAlarmDictionary(ctx, apigenerated.GetNodeClusterTypeAlarmDictionaryRequestObject{
					NodeClusterTypeId: testUUID,
				})

				Expect(err).To(BeNil())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetNodeClusterTypeAlarmDictionary500ApplicationProblemPlusJSONResponse{}))
				Expect(resp.(apigenerated.GetNodeClusterTypeAlarmDictionary500ApplicationProblemPlusJSONResponse).Status).To(Equal(http.StatusInternalServerError))
			})
		})

		When("node cluster type not found in cache", func() {
			It("returns 404 not found response", func() {
				mockRepo.EXPECT().
					GetAlarmDictionaries(ctx).
					Return([]models.AlarmDictionary{}, nil)
				mockRepo.EXPECT().
					GetThanosAlarmDefinitions(ctx).
					Return([]models.AlarmDefinition{}, nil)

				resp, err := server.GetNodeClusterTypeAlarmDictionary(ctx, apigenerated.GetNodeClusterTypeAlarmDictionaryRequestObject{
					NodeClusterTypeId: testUUID,
				})

				Expect(err).To(BeNil())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetNodeClusterTypeAlarmDictionary404ApplicationProblemPlusJSONResponse{}))
				Expect(resp.(apigenerated.GetNodeClusterTypeAlarmDictionary404ApplicationProblemPlusJSONResponse).Status).To(Equal(http.StatusNotFound))
			})
		})

		When("alarm dictionary is found for node cluster type", func() {
			It("returns 200 OK", func() {
				dictID := uuid.New()
				mockRepo.EXPECT().
					GetAlarmDictionaries(ctx).
					Return([]models.AlarmDictionary{{AlarmDictionaryID: dictID, NodeClusterTypeID: testUUID}}, nil)
				mockRepo.EXPECT().
					GetThanosAlarmDefinitions(ctx).
					Return([]models.AlarmDefinition{}, nil)
				mockRepo.EXPECT().
					GetAlarmDefinitionsByAlarmDictionaryID(ctx, dictID).
					Return([]models.AlarmDefinition{}, nil)

				resp, err := server.GetNodeClusterTypeAlarmDictionary(ctx, apigenerated.GetNodeClusterTypeAlarmDictionaryRequestObject{
					NodeClusterTypeId: testUUID,
				})

				Expect(err).To(BeNil())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetNodeClusterTypeAlarmDictionary200JSONResponse{}))
				Expect(resp.(apigenerated.GetNodeClusterTypeAlarmDictionary200JSONResponse).AlarmDictionaryId).To(Equal(dictID))
			})
		})
	})

	Describe("Alarm dictionary cache mid-loader failure", func() {
		It("returns 500 when alarm definitions query fails during cache load", func() {
			dictID := uuid.New()
			mockRepo.EXPECT().
				GetAlarmDictionaries(ctx).
				Return([]models.AlarmDictionary{{AlarmDictionaryID: dictID}}, nil)
			mockRepo.EXPECT().
				GetThanosAlarmDefinitions(ctx).
				Return([]models.AlarmDefinition{}, nil)
			mockRepo.EXPECT().
				GetAlarmDefinitionsByAlarmDictionaryID(ctx, dictID).
				Return(nil, fmt.Errorf("definitions query failed"))

			resp, err := server.GetAlarmDictionaries(ctx, apigenerated.GetAlarmDictionariesRequestObject{})

			Expect(err).To(BeNil())
			Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetAlarmDictionaries500ApplicationProblemPlusJSONResponse{}))
		})
	})

	Describe("GetAlarmDictionaries", func() {
		When("cache loading fails", func() {
			It("returns internal server error", func() {
				mockRepo.EXPECT().
					GetAlarmDictionaries(ctx).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetAlarmDictionaries(ctx, apigenerated.GetAlarmDictionariesRequestObject{})

				Expect(err).To(BeNil())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetAlarmDictionaries500ApplicationProblemPlusJSONResponse{}))
				Expect(resp.(apigenerated.GetAlarmDictionaries500ApplicationProblemPlusJSONResponse).Status).To(Equal(http.StatusInternalServerError))
			})
		})
		When("repository does not have alarm dictionaries", func() {
			It("returns 200 OK empty list", func() {
				mockRepo.EXPECT().
					GetAlarmDictionaries(ctx).
					Return([]models.AlarmDictionary{}, nil)
				mockRepo.EXPECT().
					GetThanosAlarmDefinitions(ctx).
					Return([]models.AlarmDefinition{}, nil)

				resp, err := server.GetAlarmDictionaries(ctx, apigenerated.GetAlarmDictionariesRequestObject{})

				Expect(err).To(BeNil())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetAlarmDictionaries200JSONResponse{}))
				Expect(resp.(apigenerated.GetAlarmDictionaries200JSONResponse)).To(HaveLen(0))
			})
		})

		When("alarm dictionary and definitions are found", func() {
			alarmDefinitionUUID := uuid.New()

			It("returns 200 OK", func() {
				mockRepo.EXPECT().
					GetAlarmDictionaries(ctx).
					Return([]models.AlarmDictionary{
						{
							AlarmDictionaryID: testUUID,
						},
					}, nil)

				mockRepo.EXPECT().
					GetThanosAlarmDefinitions(ctx).
					Return([]models.AlarmDefinition{}, nil)

				mockRepo.EXPECT().
					GetAlarmDefinitionsByAlarmDictionaryID(ctx, testUUID).
					Return([]models.AlarmDefinition{
						{
							AlarmDefinitionID: alarmDefinitionUUID,
						},
					}, nil)

				resp, err := server.GetAlarmDictionaries(ctx, apigenerated.GetAlarmDictionariesRequestObject{})

				Expect(err).To(BeNil())
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
					GetAlarmDictionaries(ctx).
					Return([]models.AlarmDictionary{}, nil)
				mockRepo.EXPECT().
					GetThanosAlarmDefinitions(ctx).
					Return(nil, fmt.Errorf("thanos db error"))

				resp, err := server.GetAlarmDictionaries(ctx, apigenerated.GetAlarmDictionariesRequestObject{})

				Expect(err).To(BeNil())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetAlarmDictionaries500ApplicationProblemPlusJSONResponse{}))
				Expect(resp.(apigenerated.GetAlarmDictionaries500ApplicationProblemPlusJSONResponse).Detail).To(ContainSubstring("thanos"))
			})
		})
	})

	Describe("GetAlarmDictionary", func() {
		When("cache loading fails", func() {
			It("returns internal server error", func() {
				mockRepo.EXPECT().
					GetAlarmDictionaries(ctx).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetAlarmDictionary(ctx, apigenerated.GetAlarmDictionaryRequestObject{
					AlarmDictionaryId: testUUID,
				})

				Expect(err).To(BeNil())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetAlarmDictionary500ApplicationProblemPlusJSONResponse{}))
				Expect(resp.(apigenerated.GetAlarmDictionary500ApplicationProblemPlusJSONResponse).Status).To(Equal(http.StatusInternalServerError))
			})
		})

		When("alarm dictionary not found in cache", func() {
			It("returns 404 not found response", func() {
				mockRepo.EXPECT().
					GetAlarmDictionaries(ctx).
					Return([]models.AlarmDictionary{}, nil)
				mockRepo.EXPECT().
					GetThanosAlarmDefinitions(ctx).
					Return([]models.AlarmDefinition{}, nil)

				resp, err := server.GetAlarmDictionary(ctx, apigenerated.GetAlarmDictionaryRequestObject{
					AlarmDictionaryId: testUUID,
				})

				Expect(err).To(BeNil())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetAlarmDictionary404ApplicationProblemPlusJSONResponse{}))
				Expect(resp.(apigenerated.GetAlarmDictionary404ApplicationProblemPlusJSONResponse).Status).To(Equal(http.StatusNotFound))
			})
		})

		When("alarm dictionary is found in cache", func() {
			alarmDefinitionUUID := uuid.New()

			It("returns 200 OK", func() {
				mockRepo.EXPECT().
					GetAlarmDictionaries(ctx).
					Return([]models.AlarmDictionary{{AlarmDictionaryID: testUUID}}, nil)
				mockRepo.EXPECT().
					GetThanosAlarmDefinitions(ctx).
					Return([]models.AlarmDefinition{}, nil)
				mockRepo.EXPECT().
					GetAlarmDefinitionsByAlarmDictionaryID(ctx, testUUID).
					Return([]models.AlarmDefinition{{AlarmDefinitionID: alarmDefinitionUUID}}, nil)

				resp, err := server.GetAlarmDictionary(ctx, apigenerated.GetAlarmDictionaryRequestObject{
					AlarmDictionaryId: testUUID,
				})

				Expect(err).To(BeNil())
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
					GetAlarmDictionaries(ctx).
					Return([]models.AlarmDictionary{{AlarmDictionaryID: testUUID}}, nil)
				mockRepo.EXPECT().
					GetThanosAlarmDefinitions(ctx).
					Return([]models.AlarmDefinition{
						{AlarmDefinitionID: thanosDefUUID, AlarmName: "ViolatedPolicyReport"},
					}, nil)
				mockRepo.EXPECT().
					GetAlarmDefinitionsByAlarmDictionaryID(ctx, testUUID).
					Return([]models.AlarmDefinition{
						{AlarmDefinitionID: dictDefUUID},
					}, nil)

				resp, err := server.GetAlarmDictionary(ctx, apigenerated.GetAlarmDictionaryRequestObject{
					AlarmDictionaryId: testUUID,
				})

				Expect(err).To(BeNil())
				Expect(resp).To(BeAssignableToTypeOf(apigenerated.GetAlarmDictionary200JSONResponse{}))
				Expect(resp.(apigenerated.GetAlarmDictionary200JSONResponse).AlarmDefinition).To(HaveLen(2))
			})
		})
	})

})
