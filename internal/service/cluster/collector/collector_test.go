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
	Describe("groupMembershipChanged", func() {
		var (
			clusterID uuid.UUID
			groupID   uuid.UUID
			previous  *models.ClusterResource
			current   models.ClusterResource
		)

		BeforeEach(func() {
			clusterID = uuid.New()
			groupID = uuid.New()
			previous = &models.ClusterResource{
				NodeClusterID:            &clusterID,
				ClusterResourceGroupID:   groupID,
				ClusterResourceGroupName: "worker",
			}
			current = models.ClusterResource{
				NodeClusterID:            &clusterID,
				ClusterResourceGroupID:   groupID,
				ClusterResourceGroupName: "worker",
			}
		})

		It("reports no change when the cluster and group are the same", func() {
			Expect(groupMembershipChanged(previous, current)).To(BeFalse())
		})

		It("reports no change when only other details of the resource changed", func() {
			previous.Description = "old"
			previous.GenerationID = 1
			current.Description = "new"
			current.GenerationID = 2
			Expect(groupMembershipChanged(previous, current)).To(BeFalse())
		})

		It("reports no change for a resource that is not in any group", func() {
			previous.ClusterResourceGroupID, previous.ClusterResourceGroupName = uuid.Nil, ""
			current.ClusterResourceGroupID, current.ClusterResourceGroupName = uuid.Nil, ""
			Expect(groupMembershipChanged(previous, current)).To(BeFalse())
		})

		It("reports a change for a resource that was not stored before", func() {
			Expect(groupMembershipChanged(nil, current)).To(BeTrue())
		})

		It("reports a change when the stored resource was not linked to a cluster", func() {
			previous.NodeClusterID = nil
			Expect(groupMembershipChanged(previous, current)).To(BeTrue())
		})

		It("reports a change when the resource is not linked to a cluster", func() {
			current.NodeClusterID = nil
			Expect(groupMembershipChanged(previous, current)).To(BeTrue())
		})

		It("reports a change when the resource moved to another cluster", func() {
			otherCluster := uuid.New()
			current.NodeClusterID = &otherCluster
			Expect(groupMembershipChanged(previous, current)).To(BeTrue())
		})

		It("reports a change when the resource switched group", func() {
			current.ClusterResourceGroupID = uuid.New()
			current.ClusterResourceGroupName = "master"
			Expect(groupMembershipChanged(previous, current)).To(BeTrue())
		})

		It("reports a change when the resource left its group", func() {
			current.ClusterResourceGroupID, current.ClusterResourceGroupName = uuid.Nil, ""
			Expect(groupMembershipChanged(previous, current)).To(BeTrue())
		})

		It("reports a change when the resource joined a group", func() {
			previous.ClusterResourceGroupID, previous.ClusterResourceGroupName = uuid.Nil, ""
			Expect(groupMembershipChanged(previous, current)).To(BeTrue())
		})
	})

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
