package infrastructure

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/infrastructure/clusterserver/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/infrastructure/clusterserver/generated/mock_generated"
	commonsgenerated "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
)

type objectIDAssociations struct {
	nodeClusterID     uuid.UUID
	nodeClusterTypeID uuid.UUID
	alarmDictionaryID uuid.UUID

	alarmDefinition1ID         uuid.UUID
	alarmDefinition1Identifier AlarmDefinitionUniqueIdentifier
	alarmDefinition2ID         uuid.UUID
	alarmDefinition2Identifier AlarmDefinitionUniqueIdentifier
}

var _ = Describe("ClusterServer", func() {
	var clusterServer *ClusterServer
	var ctrl *gomock.Controller
	var mockRepo *mock_generated.MockClientInterface
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockRepo = mock_generated.NewMockClientInterface(ctrl)

		clusterServer = &ClusterServer{
			client: &generated.ClientWithResponses{ClientInterface: mockRepo},
		}
	})

	Describe("FetchAll", func() {
		It("should fetch data and populate internal maps", func() {
			firstAssociation := objectIDAssociations{
				nodeClusterID:      uuid.New(),
				nodeClusterTypeID:  uuid.New(),
				alarmDictionaryID:  uuid.New(),
				alarmDefinition1ID: uuid.New(),
				alarmDefinition1Identifier: AlarmDefinitionUniqueIdentifier{
					Name:     "alarm1",
					Severity: "critical",
				},
				alarmDefinition2ID: uuid.New(),
				alarmDefinition2Identifier: AlarmDefinitionUniqueIdentifier{
					Name:     "alarm2",
					Severity: "warning",
				},
			}

			secondAssociation := objectIDAssociations{
				nodeClusterID:      uuid.New(),
				nodeClusterTypeID:  uuid.New(),
				alarmDictionaryID:  uuid.New(),
				alarmDefinition1ID: uuid.New(),
				alarmDefinition1Identifier: AlarmDefinitionUniqueIdentifier{
					Name:     "alarm3",
					Severity: "critical",
				},
				alarmDefinition2ID: uuid.New(),
				alarmDefinition2Identifier: AlarmDefinitionUniqueIdentifier{
					Name:     "alarm4",
					Severity: "warning",
				},
			}

			nodeClusters := []generated.NodeCluster{
				{
					NodeClusterId:     firstAssociation.nodeClusterID,
					NodeClusterTypeId: firstAssociation.nodeClusterTypeID,
				},
				{
					NodeClusterId:     secondAssociation.nodeClusterID,
					NodeClusterTypeId: secondAssociation.nodeClusterTypeID,
				},
			}
			body, err := json.Marshal(nodeClusters)
			Expect(err).To(BeNil())

			mockRepo.EXPECT().GetNodeClusters(gomock.Any(), nil).Return(
				&http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil)

			nodeClusterTypes := []generated.NodeClusterType{
				{
					NodeClusterTypeId: firstAssociation.nodeClusterTypeID,
					Extensions: &map[string]interface{}{
						utils.ClusterAlarmDictionaryIDExtension: firstAssociation.alarmDictionaryID,
					},
				},
				{
					NodeClusterTypeId: secondAssociation.nodeClusterTypeID,
					Extensions: &map[string]interface{}{
						utils.ClusterAlarmDictionaryIDExtension: secondAssociation.alarmDictionaryID,
					},
				},
			}
			body, err = json.Marshal(nodeClusterTypes)
			Expect(err).To(BeNil())

			mockRepo.EXPECT().GetNodeClusterTypes(gomock.Any(), nil).Return(
				&http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil)

			alarmDictionaries := []commonsgenerated.AlarmDictionary{
				{
					AlarmDictionaryId: firstAssociation.alarmDictionaryID,
					AlarmDefinition: []commonsgenerated.AlarmDefinition{
						{
							AlarmDefinitionId: firstAssociation.alarmDefinition1ID,
							AlarmName:         firstAssociation.alarmDefinition1Identifier.Name,
							AlarmAdditionalFields: &map[string]interface{}{
								"severity": firstAssociation.alarmDefinition1Identifier.Severity,
							},
						},
						{
							AlarmDefinitionId: firstAssociation.alarmDefinition2ID,
							AlarmName:         firstAssociation.alarmDefinition2Identifier.Name,
							AlarmAdditionalFields: &map[string]interface{}{
								"severity": firstAssociation.alarmDefinition2Identifier.Severity,
							},
						},
					},
				},
				{
					AlarmDictionaryId: secondAssociation.alarmDictionaryID,
					AlarmDefinition: []commonsgenerated.AlarmDefinition{
						{
							AlarmDefinitionId: secondAssociation.alarmDefinition1ID,
							AlarmName:         secondAssociation.alarmDefinition1Identifier.Name,
							AlarmAdditionalFields: &map[string]interface{}{
								"severity": secondAssociation.alarmDefinition1Identifier.Severity,
							},
						},
						{
							AlarmDefinitionId: secondAssociation.alarmDefinition2ID,
							AlarmName:         secondAssociation.alarmDefinition2Identifier.Name,
							AlarmAdditionalFields: &map[string]interface{}{
								"severity": secondAssociation.alarmDefinition2Identifier.Severity,
							},
						},
					},
				},
			}
			body, err = json.Marshal(alarmDictionaries)
			Expect(err).To(BeNil())

			mockRepo.EXPECT().GetAlarmDictionaries(gomock.Any(), nil).Return(
				&http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil)

			err = clusterServer.FetchAll(ctx)
			Expect(err).To(BeNil())

			Expect(clusterServer.nodeClusterIDToNodeClusterTypeID).To(HaveLen(2))
			Expect(clusterServer.nodeClusterTypeIDToAlarmDictionaryID).To(HaveLen(2))
			Expect(clusterServer.alarmDictionaryIDToAlarmDefinitions).To(HaveLen(2))
			Expect(clusterServer.alarmDictionaryIDToAlarmDefinitions[firstAssociation.alarmDictionaryID]).To(HaveLen(2))
			Expect(clusterServer.alarmDictionaryIDToAlarmDefinitions[secondAssociation.alarmDictionaryID]).To(HaveLen(2))

			Expect(clusterServer.nodeClusterIDToNodeClusterTypeID[firstAssociation.nodeClusterID]).To(Equal(firstAssociation.nodeClusterTypeID))
			Expect(clusterServer.nodeClusterIDToNodeClusterTypeID[secondAssociation.nodeClusterID]).To(Equal(secondAssociation.nodeClusterTypeID))
			Expect(clusterServer.nodeClusterTypeIDToAlarmDictionaryID[firstAssociation.nodeClusterTypeID]).To(Equal(firstAssociation.alarmDictionaryID))
			Expect(clusterServer.nodeClusterTypeIDToAlarmDictionaryID[secondAssociation.nodeClusterTypeID]).To(Equal(secondAssociation.alarmDictionaryID))
			Expect(clusterServer.alarmDictionaryIDToAlarmDefinitions[firstAssociation.alarmDictionaryID][firstAssociation.alarmDefinition1Identifier]).To(Equal(firstAssociation.alarmDefinition1ID))
			Expect(clusterServer.alarmDictionaryIDToAlarmDefinitions[firstAssociation.alarmDictionaryID][firstAssociation.alarmDefinition2Identifier]).To(Equal(firstAssociation.alarmDefinition2ID))
		})
	})

	Describe("GetObjectTypeID", func() {
		It("should find the node cluster type ID in cache", func() {
			nodeClusterID := uuid.New()
			nodeClusterTypeID := uuid.New()

			clusterServer.nodeClusterIDToNodeClusterTypeID = make(map[uuid.UUID]uuid.UUID)
			clusterServer.nodeClusterIDToNodeClusterTypeID[nodeClusterID] = nodeClusterTypeID

			id, err := clusterServer.GetObjectTypeID(nodeClusterID)
			Expect(err).To(BeNil())
			Expect(id).To(Equal(nodeClusterTypeID))
		})

		It("should fetch the node cluster type ID from server if not found in cache", func() {
			nodeClusterID := uuid.New()
			nodeClusterTypeID := uuid.New()

			clusterServer.nodeClusterIDToNodeClusterTypeID = make(map[uuid.UUID]uuid.UUID)

			nodeClusters := generated.NodeCluster{
				NodeClusterId:     nodeClusterID,
				NodeClusterTypeId: nodeClusterTypeID,
			}
			body, err := json.Marshal(nodeClusters)
			Expect(err).To(BeNil())

			mockRepo.EXPECT().GetNodeCluster(gomock.Any(), nodeClusterID).Return(
				&http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil)

			id, err := clusterServer.GetObjectTypeID(nodeClusterID)
			Expect(err).To(BeNil())
			Expect(id).To(Equal(nodeClusterTypeID))
			Expect(clusterServer.nodeClusterIDToNodeClusterTypeID[nodeClusterID]).To(Equal(nodeClusterTypeID))
		})

		It("should return an error if node cluster type ID is not in cache nor in server", func() {
			nodeClusterID := uuid.New()

			clusterServer.nodeClusterIDToNodeClusterTypeID = make(map[uuid.UUID]uuid.UUID)

			mockRepo.EXPECT().GetNodeCluster(gomock.Any(), nodeClusterID).Return(
				&http.Response{
					StatusCode: http.StatusNotFound,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader([]byte{})),
				}, nil)

			_, err := clusterServer.GetObjectTypeID(nodeClusterID)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("GetAlarmDefinitionID", func() {
		It("should find the alarm dictionary ID in cache", func() {
			nodeClusterTypeID := uuid.New()
			alarmDictionaryID := uuid.New()
			alarmDefinitionID := uuid.New()
			alarmDefinitionIdentifier := AlarmDefinitionUniqueIdentifier{
				Name:     "alarm1",
				Severity: "critical",
			}

			clusterServer.nodeClusterTypeIDToAlarmDictionaryID = make(map[uuid.UUID]uuid.UUID)
			clusterServer.nodeClusterTypeIDToAlarmDictionaryID[nodeClusterTypeID] = alarmDictionaryID
			clusterServer.alarmDictionaryIDToAlarmDefinitions = make(map[uuid.UUID]AlarmDefinition)
			clusterServer.alarmDictionaryIDToAlarmDefinitions[alarmDictionaryID] = make(AlarmDefinition)
			clusterServer.alarmDictionaryIDToAlarmDefinitions[alarmDictionaryID][alarmDefinitionIdentifier] = alarmDefinitionID

			id, err := clusterServer.GetAlarmDefinitionID(nodeClusterTypeID, alarmDefinitionIdentifier.Name, alarmDefinitionIdentifier.Severity)
			Expect(err).To(BeNil())
			Expect(id).To(Equal(alarmDefinitionID))
		})

		It("should succeed when node cluster type ID is not found in cache and server fetch is successful", func() {
			nodeClusterTypeID := uuid.New()
			alarmDictionaryID := uuid.New()
			alarmDefinitionID := uuid.New()
			alarmDefinitionIdentifier := AlarmDefinitionUniqueIdentifier{
				Name:     "alarm1",
				Severity: "critical",
			}

			clusterServer.nodeClusterTypeIDToAlarmDictionaryID = make(map[uuid.UUID]uuid.UUID)

			clusterServer.alarmDictionaryIDToAlarmDefinitions = make(map[uuid.UUID]AlarmDefinition)
			clusterServer.alarmDictionaryIDToAlarmDefinitions[alarmDictionaryID] = make(AlarmDefinition)
			clusterServer.alarmDictionaryIDToAlarmDefinitions[alarmDictionaryID][alarmDefinitionIdentifier] = alarmDefinitionID

			nodeClusterType := generated.NodeClusterType{
				NodeClusterTypeId: nodeClusterTypeID,
				Extensions: &map[string]interface{}{
					utils.ClusterAlarmDictionaryIDExtension: alarmDictionaryID,
				},
			}
			body, err := json.Marshal(nodeClusterType)
			Expect(err).To(BeNil())

			mockRepo.EXPECT().GetNodeClusterType(gomock.Any(), nodeClusterTypeID).Return(
				&http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil)

			id, err := clusterServer.GetAlarmDefinitionID(nodeClusterTypeID, alarmDefinitionIdentifier.Name, alarmDefinitionIdentifier.Severity)
			Expect(err).To(BeNil())
			Expect(id).To(Equal(alarmDefinitionID))
		})

		It("should fail when node cluster type ID is not found in cache nor server", func() {
			nodeClusterTypeID := uuid.New()
			alarmDefinitionIdentifier := AlarmDefinitionUniqueIdentifier{
				Name:     "alarm1",
				Severity: "critical",
			}

			clusterServer.nodeClusterTypeIDToAlarmDictionaryID = make(map[uuid.UUID]uuid.UUID)

			mockRepo.EXPECT().GetNodeClusterType(gomock.Any(), nodeClusterTypeID).Return(
				&http.Response{
					StatusCode: http.StatusNotFound,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader([]byte{})),
				}, nil)

			_, err := clusterServer.GetAlarmDefinitionID(nodeClusterTypeID, alarmDefinitionIdentifier.Name, alarmDefinitionIdentifier.Severity)
			Expect(err).To(HaveOccurred())
		})

		It("should succeed when alarm dictionary ID is not found in cache and server fetch is successful", func() {
			nodeClusterTypeID := uuid.New()
			alarmDictionaryID := uuid.New()
			alarmDefinitionID := uuid.New()
			alarmDefinitionIdentifier := AlarmDefinitionUniqueIdentifier{
				Name:     "alarm1",
				Severity: "critical",
			}

			clusterServer.nodeClusterTypeIDToAlarmDictionaryID = make(map[uuid.UUID]uuid.UUID)
			clusterServer.nodeClusterTypeIDToAlarmDictionaryID[nodeClusterTypeID] = alarmDictionaryID
			clusterServer.alarmDictionaryIDToAlarmDefinitions = make(map[uuid.UUID]AlarmDefinition)

			alarmDictionary := commonsgenerated.AlarmDictionary{
				AlarmDictionaryId: alarmDictionaryID,
				AlarmDefinition: []commonsgenerated.AlarmDefinition{
					{
						AlarmDefinitionId: alarmDefinitionID,
						AlarmName:         alarmDefinitionIdentifier.Name,
						AlarmAdditionalFields: &map[string]interface{}{
							"severity": alarmDefinitionIdentifier.Severity,
						},
					},
				},
			}
			body, err := json.Marshal(alarmDictionary)
			Expect(err).To(BeNil())

			mockRepo.EXPECT().GetAlarmDictionary(gomock.Any(), alarmDictionaryID).Return(
				&http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil)

			id, err := clusterServer.GetAlarmDefinitionID(nodeClusterTypeID, alarmDefinitionIdentifier.Name, alarmDefinitionIdentifier.Severity)
			Expect(err).To(BeNil())
			Expect(id).To(Equal(alarmDefinitionID))
		})

		It("should fail when alarm dictionary ID is not found in cache nor in the server", func() {
			nodeClusterTypeID := uuid.New()
			alarmDictionaryID := uuid.New()
			alarmDefinitionIdentifier := AlarmDefinitionUniqueIdentifier{
				Name:     "alarm1",
				Severity: "critical",
			}

			clusterServer.nodeClusterTypeIDToAlarmDictionaryID = make(map[uuid.UUID]uuid.UUID)
			clusterServer.nodeClusterTypeIDToAlarmDictionaryID[nodeClusterTypeID] = alarmDictionaryID
			clusterServer.alarmDictionaryIDToAlarmDefinitions = make(map[uuid.UUID]AlarmDefinition)

			mockRepo.EXPECT().GetAlarmDictionary(gomock.Any(), alarmDictionaryID).Return(
				&http.Response{
					StatusCode: http.StatusNotFound,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader([]byte{})),
				}, nil)

			_, err := clusterServer.GetAlarmDefinitionID(nodeClusterTypeID, alarmDefinitionIdentifier.Name, alarmDefinitionIdentifier.Severity)
			Expect(err).To(HaveOccurred())
		})

		It("should succeed when alarm dictionary is in cache but the definition is not. There should a retry that updates the cache", func() {
			nodeClusterTypeID := uuid.New()
			alarmDictionaryID := uuid.New()
			alarmDefinitionID := uuid.New()
			alarmDefinitionIdentifier := AlarmDefinitionUniqueIdentifier{
				Name:     "alarm1",
				Severity: "critical",
			}

			clusterServer.nodeClusterTypeIDToAlarmDictionaryID = make(map[uuid.UUID]uuid.UUID)
			clusterServer.nodeClusterTypeIDToAlarmDictionaryID[nodeClusterTypeID] = alarmDictionaryID
			clusterServer.alarmDictionaryIDToAlarmDefinitions = make(map[uuid.UUID]AlarmDefinition)
			clusterServer.alarmDictionaryIDToAlarmDefinitions[alarmDictionaryID] = make(AlarmDefinition)

			alarmDictionary := commonsgenerated.AlarmDictionary{
				AlarmDictionaryId: alarmDictionaryID,
				AlarmDefinition: []commonsgenerated.AlarmDefinition{
					{
						AlarmDefinitionId: alarmDefinitionID,
						AlarmName:         alarmDefinitionIdentifier.Name,
						AlarmAdditionalFields: &map[string]interface{}{
							"severity": alarmDefinitionIdentifier.Severity,
						},
					},
				},
			}
			body, err := json.Marshal(alarmDictionary)
			Expect(err).To(BeNil())

			mockRepo.EXPECT().GetAlarmDictionary(gomock.Any(), alarmDictionaryID).Return(
				&http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil)

			id, err := clusterServer.GetAlarmDefinitionID(nodeClusterTypeID, alarmDefinitionIdentifier.Name, alarmDefinitionIdentifier.Severity)
			Expect(err).To(BeNil())
			Expect(id).To(Equal(alarmDefinitionID))
		})

		It("should fail when alarm definition is not found in cache but there was a previous resync due to a missing dictionary ID or node cluster type ID", func() {
			nodeClusterTypeID := uuid.New()
			alarmDictionaryID := uuid.New()
			alarmDefinitionID := uuid.New()
			alarmDefinitionIdentifier := AlarmDefinitionUniqueIdentifier{
				Name:     "alarm1",
				Severity: "critical",
			}

			clusterServer.nodeClusterTypeIDToAlarmDictionaryID = make(map[uuid.UUID]uuid.UUID)
			clusterServer.nodeClusterTypeIDToAlarmDictionaryID[nodeClusterTypeID] = alarmDictionaryID
			clusterServer.alarmDictionaryIDToAlarmDefinitions = make(map[uuid.UUID]AlarmDefinition)

			alarmDictionary := commonsgenerated.AlarmDictionary{
				AlarmDictionaryId: alarmDictionaryID,
				AlarmDefinition: []commonsgenerated.AlarmDefinition{
					{
						AlarmDefinitionId: alarmDefinitionID,
						AlarmName:         alarmDefinitionIdentifier.Name,
						AlarmAdditionalFields: &map[string]interface{}{
							"severity": alarmDefinitionIdentifier.Severity,
						},
					},
				},
			}
			body, err := json.Marshal(alarmDictionary)
			Expect(err).To(BeNil())

			mockRepo.EXPECT().GetAlarmDictionary(gomock.Any(), alarmDictionaryID).Return(
				&http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil)

			missingAlarmDefinitionIdentifier := AlarmDefinitionUniqueIdentifier{
				Name:     "alarm2",
				Severity: "warning",
			}

			_, err = clusterServer.GetAlarmDefinitionID(nodeClusterTypeID, missingAlarmDefinitionIdentifier.Name, missingAlarmDefinitionIdentifier.Severity)
			Expect(err).To(HaveOccurred())
		})
	})
})
