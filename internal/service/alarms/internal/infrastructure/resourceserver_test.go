/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

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

	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/infrastructure/resourceserver/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/infrastructure/resourceserver/generated/mock_generated"
	commonsgenerated "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
)

type resourceObjectIDAssociations struct {
	resourceID        uuid.UUID
	resourceTypeID    uuid.UUID
	alarmDictionaryID uuid.UUID

	alarmDefinition1ID         uuid.UUID
	alarmDefinition1Identifier AlarmDefinitionUniqueIdentifier
	alarmDefinition2ID         uuid.UUID
	alarmDefinition2Identifier AlarmDefinitionUniqueIdentifier
}

var _ = Describe("ResourceServer", func() {
	var resourceServer *ResourceServer
	var ctrl *gomock.Controller
	var mockRepo *mock_generated.MockClientInterface
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockRepo = mock_generated.NewMockClientInterface(ctrl)

		resourceServer = &ResourceServer{
			client: &generated.ClientWithResponses{ClientInterface: mockRepo},
		}
	})

	Describe("FetchAll", func() {
		It("should fetch data and populate internal maps", func() {
			firstAssociation := resourceObjectIDAssociations{
				resourceID:         uuid.New(),
				resourceTypeID:     uuid.New(),
				alarmDictionaryID:  uuid.New(),
				alarmDefinition1ID: uuid.New(),
				alarmDefinition1Identifier: AlarmDefinitionUniqueIdentifier{
					Name:     "HardwareTemperatureHigh",
					Severity: "critical",
				},
				alarmDefinition2ID: uuid.New(),
				alarmDefinition2Identifier: AlarmDefinitionUniqueIdentifier{
					Name:     "HardwareFanFailure",
					Severity: "warning",
				},
			}

			secondAssociation := resourceObjectIDAssociations{
				resourceID:         uuid.New(),
				resourceTypeID:     uuid.New(),
				alarmDictionaryID:  uuid.New(),
				alarmDefinition1ID: uuid.New(),
				alarmDefinition1Identifier: AlarmDefinitionUniqueIdentifier{
					Name:     "PowerSupplyFailure",
					Severity: "critical",
				},
				alarmDefinition2ID: uuid.New(),
				alarmDefinition2Identifier: AlarmDefinitionUniqueIdentifier{
					Name:     "DiskFailure",
					Severity: "major",
				},
			}

			resourceTypes := []generated.ResourceType{
				{
					ResourceTypeId: firstAssociation.resourceTypeID,
				},
				{
					ResourceTypeId: secondAssociation.resourceTypeID,
				},
			}
			body, err := json.Marshal(resourceTypes)
			Expect(err).To(BeNil())

			mockRepo.EXPECT().GetResourceTypes(gomock.Any(), &generated.GetResourceTypesParams{}).Return(
				&http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil)

			// Mock alarm dictionary responses for each resource type
			firstDictionary := commonsgenerated.AlarmDictionary{
				AlarmDictionaryId: firstAssociation.alarmDictionaryID,
				AlarmDefinition: []commonsgenerated.AlarmDefinition{
					{
						AlarmDefinitionId: firstAssociation.alarmDefinition1ID,
						AlarmName:         firstAssociation.alarmDefinition1Identifier.Name,
						AlarmAdditionalFields: &map[string]interface{}{
							ctlrutils.AlarmDefinitionSeverityField: firstAssociation.alarmDefinition1Identifier.Severity,
						},
					},
					{
						AlarmDefinitionId: firstAssociation.alarmDefinition2ID,
						AlarmName:         firstAssociation.alarmDefinition2Identifier.Name,
						AlarmAdditionalFields: &map[string]interface{}{
							ctlrutils.AlarmDefinitionSeverityField: firstAssociation.alarmDefinition2Identifier.Severity,
						},
					},
				},
			}
			body1, err := json.Marshal(firstDictionary)
			Expect(err).To(BeNil())

			mockRepo.EXPECT().GetResourceTypeAlarmDictionary(gomock.Any(), firstAssociation.resourceTypeID).Return(
				&http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader(body1)),
				}, nil)

			secondDictionary := commonsgenerated.AlarmDictionary{
				AlarmDictionaryId: secondAssociation.alarmDictionaryID,
				AlarmDefinition: []commonsgenerated.AlarmDefinition{
					{
						AlarmDefinitionId: secondAssociation.alarmDefinition1ID,
						AlarmName:         secondAssociation.alarmDefinition1Identifier.Name,
						AlarmAdditionalFields: &map[string]interface{}{
							ctlrutils.AlarmDefinitionSeverityField: secondAssociation.alarmDefinition1Identifier.Severity,
						},
					},
					{
						AlarmDefinitionId: secondAssociation.alarmDefinition2ID,
						AlarmName:         secondAssociation.alarmDefinition2Identifier.Name,
						AlarmAdditionalFields: &map[string]interface{}{
							ctlrutils.AlarmDefinitionSeverityField: secondAssociation.alarmDefinition2Identifier.Severity,
						},
					},
				},
			}
			body2, err := json.Marshal(secondDictionary)
			Expect(err).To(BeNil())

			mockRepo.EXPECT().GetResourceTypeAlarmDictionary(gomock.Any(), secondAssociation.resourceTypeID).Return(
				&http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader(body2)),
				}, nil)

			err = resourceServer.FetchAll(ctx)
			Expect(err).To(BeNil())

			// Verify the maps are populated correctly
			Expect(resourceServer.resourceTypeIDToAlarmDefinitions).To(HaveLen(2))
			Expect(resourceServer.resourceTypeIDToAlarmDefinitions[firstAssociation.resourceTypeID]).To(HaveLen(2))
			Expect(resourceServer.resourceTypeIDToAlarmDefinitions[secondAssociation.resourceTypeID]).To(HaveLen(2))

			// Verify alarm definition mappings
			Expect(resourceServer.resourceTypeIDToAlarmDefinitions[firstAssociation.resourceTypeID][firstAssociation.alarmDefinition1Identifier]).To(Equal(firstAssociation.alarmDefinition1ID))
			Expect(resourceServer.resourceTypeIDToAlarmDefinitions[firstAssociation.resourceTypeID][firstAssociation.alarmDefinition2Identifier]).To(Equal(firstAssociation.alarmDefinition2ID))
			Expect(resourceServer.resourceTypeIDToAlarmDefinitions[secondAssociation.resourceTypeID][secondAssociation.alarmDefinition1Identifier]).To(Equal(secondAssociation.alarmDefinition1ID))
			Expect(resourceServer.resourceTypeIDToAlarmDefinitions[secondAssociation.resourceTypeID][secondAssociation.alarmDefinition2Identifier]).To(Equal(secondAssociation.alarmDefinition2ID))
		})

		It("should handle missing alarm dictionaries gracefully", func() {
			resourceTypeID := uuid.New()

			resourceTypes := []generated.ResourceType{
				{
					ResourceTypeId: resourceTypeID,
				},
			}
			body, err := json.Marshal(resourceTypes)
			Expect(err).To(BeNil())

			mockRepo.EXPECT().GetResourceTypes(gomock.Any(), &generated.GetResourceTypesParams{}).Return(
				&http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil)

			// Return 404 for alarm dictionary
			mockRepo.EXPECT().GetResourceTypeAlarmDictionary(gomock.Any(), resourceTypeID).Return(
				&http.Response{
					StatusCode: http.StatusNotFound,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader([]byte{})),
				}, nil)

			err = resourceServer.FetchAll(ctx)
			Expect(err).To(BeNil())

			// Should not have entry for this resource type
			Expect(resourceServer.resourceTypeIDToAlarmDefinitions).To(HaveLen(0))
		})

		It("should return error when GetResourceTypes fails", func() {
			mockRepo.EXPECT().GetResourceTypes(gomock.Any(), &generated.GetResourceTypesParams{}).Return(
				&http.Response{
					StatusCode: http.StatusInternalServerError,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader([]byte{})),
				}, nil)

			err := resourceServer.FetchAll(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get resource types"))
		})
	})

	Describe("GetObjectTypeID", func() {
		It("should find the resource type ID in cache", func() {
			resourceID := uuid.New()
			resourceTypeID := uuid.New()

			resourceServer.resourceIDToResourceTypeID = make(map[uuid.UUID]uuid.UUID)
			resourceServer.resourceIDToResourceTypeID[resourceID] = resourceTypeID

			id, err := resourceServer.GetObjectTypeID(context.Background(), resourceID)
			Expect(err).To(BeNil())
			Expect(id).To(Equal(resourceTypeID))
		})

		It("should fetch the resource type ID from server if not found in cache", func() {
			resourceID := uuid.New()
			resourceTypeID := uuid.New()

			resourceServer.resourceIDToResourceTypeID = make(map[uuid.UUID]uuid.UUID)

			resource := generated.Resource{
				ResourceId:     resourceID,
				ResourceTypeId: resourceTypeID,
			}
			body, err := json.Marshal(resource)
			Expect(err).To(BeNil())

			mockRepo.EXPECT().GetInternalResourceById(gomock.Any(), resourceID).Return(
				&http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil)

			id, err := resourceServer.GetObjectTypeID(context.Background(), resourceID)
			Expect(err).To(BeNil())
			Expect(id).To(Equal(resourceTypeID))

			// Verify it was cached
			Expect(resourceServer.resourceIDToResourceTypeID[resourceID]).To(Equal(resourceTypeID))
		})

		It("should return an error if resource is not found", func() {
			resourceID := uuid.New()

			resourceServer.resourceIDToResourceTypeID = make(map[uuid.UUID]uuid.UUID)

			mockRepo.EXPECT().GetInternalResourceById(gomock.Any(), resourceID).Return(
				&http.Response{
					StatusCode: http.StatusNotFound,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader([]byte{})),
				}, nil)

			_, err := resourceServer.GetObjectTypeID(context.Background(), resourceID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get resource"))
		})
	})

	Describe("GetAlarmDefinitionID", func() {
		It("should find the alarm definition ID in cache", func() {
			resourceTypeID := uuid.New()
			alarmDefinitionID := uuid.New()
			alarmDefinitionIdentifier := AlarmDefinitionUniqueIdentifier{
				Name:     "HardwareTemperatureHigh",
				Severity: "critical",
			}

			resourceServer.resourceTypeIDToAlarmDefinitions = make(map[uuid.UUID]AlarmDefinition)
			resourceServer.resourceTypeIDToAlarmDefinitions[resourceTypeID] = make(AlarmDefinition)
			resourceServer.resourceTypeIDToAlarmDefinitions[resourceTypeID][alarmDefinitionIdentifier] = alarmDefinitionID

			id, err := resourceServer.GetAlarmDefinitionID(context.Background(), resourceTypeID, alarmDefinitionIdentifier.Name, alarmDefinitionIdentifier.Severity)
			Expect(err).To(BeNil())
			Expect(id).To(Equal(alarmDefinitionID))
		})

		It("should fetch alarm definitions from server when resource type ID is not in cache", func() {
			resourceTypeID := uuid.New()
			alarmDictionaryID := uuid.New()
			alarmDefinitionID := uuid.New()
			alarmDefinitionIdentifier := AlarmDefinitionUniqueIdentifier{
				Name:     "HardwareTemperatureHigh",
				Severity: "critical",
			}

			resourceServer.resourceTypeIDToAlarmDefinitions = make(map[uuid.UUID]AlarmDefinition)

			// Mock the alarm dictionary response
			dictionary := commonsgenerated.AlarmDictionary{
				AlarmDictionaryId: alarmDictionaryID,
				AlarmDefinition: []commonsgenerated.AlarmDefinition{
					{
						AlarmDefinitionId: alarmDefinitionID,
						AlarmName:         alarmDefinitionIdentifier.Name,
						AlarmAdditionalFields: &map[string]interface{}{
							ctlrutils.AlarmDefinitionSeverityField: alarmDefinitionIdentifier.Severity,
						},
					},
				},
			}
			body, err := json.Marshal(dictionary)
			Expect(err).To(BeNil())

			mockRepo.EXPECT().GetResourceTypeAlarmDictionary(gomock.Any(), resourceTypeID).Return(
				&http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil)

			id, err := resourceServer.GetAlarmDefinitionID(context.Background(), resourceTypeID, alarmDefinitionIdentifier.Name, alarmDefinitionIdentifier.Severity)
			Expect(err).To(BeNil())
			Expect(id).To(Equal(alarmDefinitionID))

			// Verify it was cached
			Expect(resourceServer.resourceTypeIDToAlarmDefinitions[resourceTypeID]).To(HaveLen(1))
			Expect(resourceServer.resourceTypeIDToAlarmDefinitions[resourceTypeID][alarmDefinitionIdentifier]).To(Equal(alarmDefinitionID))
		})

		It("should return error when alarm definition is not found in cache", func() {
			resourceTypeID := uuid.New()
			alarmDefinitionIdentifier := AlarmDefinitionUniqueIdentifier{
				Name:     "NonexistentAlarm",
				Severity: "critical",
			}

			resourceServer.resourceTypeIDToAlarmDefinitions = make(map[uuid.UUID]AlarmDefinition)
			resourceServer.resourceTypeIDToAlarmDefinitions[resourceTypeID] = make(AlarmDefinition)

			_, err := resourceServer.GetAlarmDefinitionID(context.Background(), resourceTypeID, alarmDefinitionIdentifier.Name, alarmDefinitionIdentifier.Severity)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("alarm definition not found"))
		})

		It("should handle multiple alarm definitions for same resource type", func() {
			resourceTypeID := uuid.New()
			alarmDef1ID := uuid.New()
			alarmDef2ID := uuid.New()
			alarmDef1Identifier := AlarmDefinitionUniqueIdentifier{
				Name:     "HardwareTemperatureHigh",
				Severity: "critical",
			}
			alarmDef2Identifier := AlarmDefinitionUniqueIdentifier{
				Name:     "HardwareFanFailure",
				Severity: "warning",
			}

			resourceServer.resourceTypeIDToAlarmDefinitions = make(map[uuid.UUID]AlarmDefinition)
			resourceServer.resourceTypeIDToAlarmDefinitions[resourceTypeID] = make(AlarmDefinition)
			resourceServer.resourceTypeIDToAlarmDefinitions[resourceTypeID][alarmDef1Identifier] = alarmDef1ID
			resourceServer.resourceTypeIDToAlarmDefinitions[resourceTypeID][alarmDef2Identifier] = alarmDef2ID

			// Fetch first alarm definition
			id1, err := resourceServer.GetAlarmDefinitionID(context.Background(), resourceTypeID, alarmDef1Identifier.Name, alarmDef1Identifier.Severity)
			Expect(err).To(BeNil())
			Expect(id1).To(Equal(alarmDef1ID))

			// Fetch second alarm definition
			id2, err := resourceServer.GetAlarmDefinitionID(context.Background(), resourceTypeID, alarmDef2Identifier.Name, alarmDef2Identifier.Severity)
			Expect(err).To(BeNil())
			Expect(id2).To(Equal(alarmDef2ID))
		})
	})
})
