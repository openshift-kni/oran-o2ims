/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/db/models"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

var _ = Describe("Collector", func() {
	Describe("makeClusterResourceGroups", func() {
		var (
			clusterID    uuid.UUID
			dataSourceID uuid.UUID
		)

		BeforeEach(func() {
			clusterID = uuid.New()
			dataSourceID = uuid.New()
		})

		It("builds a group from its observed members", func() {
			groupID := uuid.New()
			memberID := uuid.New()
			resources := []models.ClusterResource{{
				ClusterResourceID:        memberID,
				ClusterResourceGroupID:   groupID,
				ClusterResourceGroupName: "worker-east",
				DataSourceID:             dataSourceID,
				GenerationID:             7,
			}}

			groups := makeClusterResourceGroups(clusterID, "cluster-a", resources)

			Expect(groups).To(HaveLen(1))
			group := groups[groupID]
			Expect(group.ClusterResourceGroupID).To(Equal(groupID))
			Expect(group.NodeClusterID).To(Equal(clusterID))
			Expect(group.Name).To(Equal("worker-east"))
			Expect(group.Description).To(Equal("worker-east node group of cluster cluster-a"))
			Expect(group.Extensions).ToNot(BeNil())
			Expect(*group.Extensions).To(Equal(map[string]any{
				"nodeClusterId":   clusterID.String(),
				"nodeClusterName": "cluster-a",
			}))
			Expect(group.DataSourceID).To(Equal(dataSourceID))
			Expect(group.GenerationID).To(Equal(7))
			Expect(group.ClusterResources).To(Equal([]uuid.UUID{memberID}))
		})

		It("sorts members so the result does not depend on the input order", func() {
			groupID := uuid.New()
			low := uuid.MustParse("00000000-0000-0000-0000-000000000001")
			mid := uuid.MustParse("00000000-0000-0000-0000-000000000002")
			high := uuid.MustParse("00000000-0000-0000-0000-000000000003")
			resources := []models.ClusterResource{
				{ClusterResourceID: high, ClusterResourceGroupID: groupID, ClusterResourceGroupName: "worker"},
				{ClusterResourceID: low, ClusterResourceGroupID: groupID, ClusterResourceGroupName: "worker"},
				{ClusterResourceID: mid, ClusterResourceGroupID: groupID, ClusterResourceGroupName: "worker"},
			}

			groups := makeClusterResourceGroups(clusterID, "cluster-a", resources)

			Expect(groups[groupID].ClusterResources).To(Equal([]uuid.UUID{low, mid, high}))
		})

		It("keeps members of different groups apart", func() {
			masterID, workerID := uuid.New(), uuid.New()
			masterMember, workerMember := uuid.New(), uuid.New()
			resources := []models.ClusterResource{
				{ClusterResourceID: masterMember, ClusterResourceGroupID: masterID, ClusterResourceGroupName: "master"},
				{ClusterResourceID: workerMember, ClusterResourceGroupID: workerID, ClusterResourceGroupName: "worker"},
			}

			groups := makeClusterResourceGroups(clusterID, "cluster-a", resources)

			Expect(groups).To(HaveLen(2))
			Expect(groups[masterID].Name).To(Equal("master"))
			Expect(groups[masterID].ClusterResources).To(Equal([]uuid.UUID{masterMember}))
			Expect(groups[workerID].Name).To(Equal("worker"))
			Expect(groups[workerID].ClusterResources).To(Equal([]uuid.UUID{workerMember}))
		})

		It("ignores resources that do not belong to a group", func() {
			groupID := uuid.New()
			grouped := uuid.New()
			resources := []models.ClusterResource{
				{ClusterResourceID: grouped, ClusterResourceGroupID: groupID, ClusterResourceGroupName: "worker"},
				// No group at all.
				{ClusterResourceID: uuid.New()},
				// Group ID without a name.
				{ClusterResourceID: uuid.New(), ClusterResourceGroupID: uuid.New()},
				// Group name without an ID.
				{ClusterResourceID: uuid.New(), ClusterResourceGroupName: "orphan"},
			}

			groups := makeClusterResourceGroups(clusterID, "cluster-a", resources)

			Expect(groups).To(HaveLen(1))
			Expect(groups[groupID].ClusterResources).To(Equal([]uuid.UUID{grouped}))
		})

		It("builds groups that compare as unchanged against an identical stored copy", func() {
			groupID := uuid.New()
			resources := []models.ClusterResource{
				{ClusterResourceID: uuid.New(), ClusterResourceGroupID: groupID, ClusterResourceGroupName: "worker", DataSourceID: dataSourceID},
			}
			desired := makeClusterResourceGroups(clusterID, "cluster-a", resources)[groupID]

			// A stored row differs only in the columns the sync ignores.
			createdAt := time.Now()
			stored := makeClusterResourceGroups(clusterID, "cluster-a", resources)[groupID]
			stored.GenerationID = 42
			stored.CreatedAt = &createdAt
			Expect(svcutils.CompareObjects(stored, desired, "CreatedAt", "GenerationID")).To(BeEmpty())

			// A change in membership must be detected so that the group is persisted.
			desired.ClusterResources = append(desired.ClusterResources, uuid.New())
			Expect(svcutils.CompareObjects(stored, desired, "CreatedAt", "GenerationID")).To(HaveKey("ClusterResources"))
		})

		It("does not build empty groups", func() {
			Expect(makeClusterResourceGroups(clusterID, "cluster-a", nil)).To(BeEmpty())
			Expect(makeClusterResourceGroups(clusterID, "cluster-a", []models.ClusterResource{{ClusterResourceID: uuid.New()}})).To(BeEmpty())
		})
	})
})
