/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/api/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

var _ = Describe("K8S Collector", func() {
	Describe("convertManagedClusterToNodeCluster", func() {
		var (
			dataSource *K8SDataSource
			cluster    *clusterv1.ManagedCluster
		)

		BeforeEach(func() {
			dataSource = &K8SDataSource{
				dataSourceID: uuid.New(),
				cloudID:      uuid.New(),
				generationID: 1,
			}

			cluster = &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
					Labels: map[string]string{
						ctlrutils.ClusterVendorExtension:    "OpenShift",
						ctlrutils.OpenshiftVersionLabelName: "4.16.3",
						ctlrutils.ClusterIDLabelName:        uuid.New().String(),
					},
				},
				Status: clusterv1.ManagedClusterStatus{
					Version: clusterv1.ManagedClusterVersion{
						Kubernetes: "v1.29.7",
					},
					Capacity: clusterv1.ResourceList{
						"cpu":               resource.MustParse("96"),
						"memory":            resource.MustParse("395531120Ki"),
						"pods":              resource.MustParse("750"),
						"ephemeral-storage": resource.MustParse("556845Mi"),
					},
					Allocatable: clusterv1.ResourceList{
						"cpu":               resource.MustParse("91500m"),
						"memory":            resource.MustParse("389843312Ki"),
						"pods":              resource.MustParse("750"),
						"ephemeral-storage": resource.MustParse("530005Mi"),
					},
				},
			}
		})

		It("extracts kubernetes version to extensions", func() {
			result, err := dataSource.convertManagedClusterToNodeCluster(cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Extensions).ToNot(BeNil())

			extensions := *result.Extensions
			Expect(extensions).To(HaveKey("kubernetesVersion"))
			Expect(extensions["kubernetesVersion"]).To(Equal("v1.29.7"))
		})

		It("extracts capacity resources to extensions", func() {
			result, err := dataSource.convertManagedClusterToNodeCluster(cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Extensions).ToNot(BeNil())

			extensions := *result.Extensions
			Expect(extensions).To(HaveKey("capacity"))

			capacity, ok := extensions["capacity"].(map[string]string)
			Expect(ok).To(BeTrue())
			Expect(capacity).To(HaveKey("cpu"))
			Expect(capacity).To(HaveKey("memory"))
			Expect(capacity).To(HaveKey("pods"))
			Expect(capacity).To(HaveKey("ephemeral-storage"))
			Expect(capacity["cpu"]).To(Equal("96"))
			Expect(capacity["pods"]).To(Equal("750"))
		})

		It("extracts allocatable resources to extensions", func() {
			result, err := dataSource.convertManagedClusterToNodeCluster(cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Extensions).ToNot(BeNil())

			extensions := *result.Extensions
			Expect(extensions).To(HaveKey("allocatable"))

			allocatable, ok := extensions["allocatable"].(map[string]string)
			Expect(ok).To(BeTrue())
			Expect(allocatable).To(HaveKey("cpu"))
			Expect(allocatable).To(HaveKey("memory"))
			Expect(allocatable).To(HaveKey("pods"))
			Expect(allocatable).To(HaveKey("ephemeral-storage"))
			Expect(allocatable["cpu"]).To(Equal("91500m"))
		})

		It("handles missing kubernetes version gracefully", func() {
			cluster.Status.Version.Kubernetes = ""

			result, err := dataSource.convertManagedClusterToNodeCluster(cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Extensions).ToNot(BeNil())

			extensions := *result.Extensions
			Expect(extensions).ToNot(HaveKey("kubernetesVersion"))
		})

		It("handles missing capacity gracefully", func() {
			cluster.Status.Capacity = nil

			result, err := dataSource.convertManagedClusterToNodeCluster(cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Extensions).ToNot(BeNil())

			extensions := *result.Extensions
			Expect(extensions).ToNot(HaveKey("capacity"))
		})

		It("handles missing allocatable gracefully", func() {
			cluster.Status.Allocatable = nil

			result, err := dataSource.convertManagedClusterToNodeCluster(cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Extensions).ToNot(BeNil())

			extensions := *result.Extensions
			Expect(extensions).ToNot(HaveKey("allocatable"))
		})

		It("preserves existing extensions from labels", func() {
			result, err := dataSource.convertManagedClusterToNodeCluster(cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Extensions).ToNot(BeNil())

			extensions := *result.Extensions
			// Check that label-based extensions are still present
			Expect(extensions).To(HaveKey(ctlrutils.ClusterVendorExtension))
			Expect(extensions[ctlrutils.ClusterVendorExtension]).To(Equal("OpenShift"))
			Expect(extensions).To(HaveKey(ctlrutils.ClusterModelExtension))

			// And new resource-based extensions are also present
			Expect(extensions).To(HaveKey("kubernetesVersion"))
			Expect(extensions).To(HaveKey("capacity"))
			Expect(extensions).To(HaveKey("allocatable"))
		})
	})

	Describe("convertAgentToClusterResource", func() {
		var (
			dataSource  *K8SDataSource
			agent       *v1beta1.Agent
			templateID  uuid.UUID
			hwMgrNodeID uuid.UUID
		)

		// expectedGroupID is the group UUID the data source is expected to derive for a cluster and group name.
		expectedGroupID := func(clusterName, groupName string) uuid.UUID {
			return ctlrutils.MakeUUIDFromNames(ClusterResourceGroupUUIDNamespace, dataSource.cloudID, clusterName, groupName)
		}

		BeforeEach(func() {
			dataSource = &K8SDataSource{
				dataSourceID: uuid.New(),
				cloudID:      uuid.New(),
				generationID: 3,
			}
			templateID = uuid.New()
			hwMgrNodeID = uuid.New()

			agent = &v1beta1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node-1",
					Namespace: "cluster-a",
					Labels: map[string]string{
						ctlrutils.ClusterTemplateArtifactsLabel: templateID.String(),
						ctlrutils.HardwareManagerNodeIdLabel:    hwMgrNodeID.String(),
					},
					Annotations: map[string]string{ctlrutils.NodeGroupAnnotation: "worker-east"},
				},
				Spec: v1beta1.AgentSpec{
					Hostname:              "node-1.example.com",
					ClusterDeploymentName: &v1beta1.ClusterReference{Name: "cluster-a", Namespace: "cluster-a"},
				},
				Status: v1beta1.AgentStatus{
					Role: "worker",
					Inventory: v1beta1.HostInventory{
						Cpu:          v1beta1.HostCPU{Architecture: "x86_64", Count: 32, ModelName: "Xeon"},
						Memory:       v1beta1.HostMemory{PhysicalBytes: 64 * 1024 * 1024 * 1024},
						SystemVendor: v1beta1.HostSystemVendor{Manufacturer: "Dell", ProductName: "R760", SerialNumber: "SN123"},
					},
				},
			}
		})

		It("converts the Agent into a cluster resource", func() {
			result, err := dataSource.convertAgentToClusterResource(agent)
			Expect(err).ToNot(HaveOccurred())

			Expect(result.ClusterResourceID).To(Equal(
				ctlrutils.MakeUUIDFromNames(ClusterResourceUUIDNamespace, dataSource.cloudID, "cluster-a/node-1")))
			Expect(result.ClusterResourceTypeID).To(Equal(dataSource.makeClusterResourceTypeID("x86_64", "32")))
			Expect(result.Name).To(Equal("node-1.example.com"))
			Expect(result.Description).To(Equal("node-1.example.com"))
			Expect(result.NodeClusterName).To(Equal("cluster-a"))
			// The cluster ID is filled in later by the collector.
			Expect(result.NodeClusterID).To(BeNil())
			Expect(result.ExternalID).To(Equal("cluster-a/node-1"))
			Expect(result.ResourceID).To(Equal(hwMgrNodeID))
			Expect(result.DataSourceID).To(Equal(dataSource.dataSourceID))
			Expect(result.GenerationID).To(Equal(3))
			Expect(result.ArtifactResourceIDs).ToNot(BeNil())
			Expect(*result.ArtifactResourceIDs).To(Equal([]uuid.UUID{templateID}))
			Expect(result.Extensions).ToNot(BeNil())
			Expect(*result.Extensions).To(Equal(map[string]any{
				"cpu":    map[string]string{"cores": "32", "architecture": "x86_64", "model": "Xeon"},
				"memory": map[string]string{"GiB": "64"},
				"role":   "worker",
				"system": map[string]string{"manufacturer": "Dell", "product": "R760", "serial": "SN123"},
			}))
		})

		Describe("inventory identifiers", func() {
			It("leaves the resource ID empty when the hardware manager node label is missing", func() {
				delete(agent.Labels, ctlrutils.HardwareManagerNodeIdLabel)

				result, err := dataSource.convertAgentToClusterResource(agent)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.ResourceID).To(Equal(uuid.Nil))
			})

			It("leaves the resource ID empty when the hardware manager node label is not a UUID", func() {
				agent.Labels[ctlrutils.HardwareManagerNodeIdLabel] = "not-a-uuid"

				result, err := dataSource.convertAgentToClusterResource(agent)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.ResourceID).To(Equal(uuid.Nil))
			})

			It("leaves the artifact IDs unset when the template label is missing", func() {
				delete(agent.Labels, ctlrutils.ClusterTemplateArtifactsLabel)

				result, err := dataSource.convertAgentToClusterResource(agent)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.ArtifactResourceIDs).To(BeNil())
			})

			It("returns an error when the template label is not a UUID", func() {
				agent.Labels[ctlrutils.ClusterTemplateArtifactsLabel] = "not-a-uuid"

				_, err := dataSource.convertAgentToClusterResource(agent)
				Expect(err).To(HaveOccurred())
			})
		})

		Describe("cluster resource group", func() {
			It("records the group name and a group UUID derived from the cloud, cluster and group", func() {
				result, err := dataSource.convertAgentToClusterResource(agent)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.ClusterResourceGroupName).To(Equal("worker-east"))
				Expect(result.ClusterResourceGroupID).To(Equal(expectedGroupID("cluster-a", "worker-east")))
			})

			It("derives the same group UUID on every conversion", func() {
				first, err := dataSource.convertAgentToClusterResource(agent)
				Expect(err).ToNot(HaveOccurred())
				again, err := dataSource.convertAgentToClusterResource(agent)
				Expect(err).ToNot(HaveOccurred())
				Expect(again.ClusterResourceGroupID).To(Equal(first.ClusterResourceGroupID))
			})

			It("derives different group UUIDs for different clusters, groups and clouds", func() {
				first, err := dataSource.convertAgentToClusterResource(agent)
				Expect(err).ToNot(HaveOccurred())

				agent.Spec.ClusterDeploymentName.Name = "cluster-b"
				otherCluster, err := dataSource.convertAgentToClusterResource(agent)
				Expect(err).ToNot(HaveOccurred())
				Expect(otherCluster.ClusterResourceGroupID).ToNot(Equal(first.ClusterResourceGroupID))

				agent.Spec.ClusterDeploymentName.Name = "cluster-a"
				agent.Annotations[ctlrutils.NodeGroupAnnotation] = "worker-west"
				otherGroup, err := dataSource.convertAgentToClusterResource(agent)
				Expect(err).ToNot(HaveOccurred())
				Expect(otherGroup.ClusterResourceGroupID).ToNot(Equal(first.ClusterResourceGroupID))

				agent.Annotations[ctlrutils.NodeGroupAnnotation] = "worker-east"
				otherCloud := &K8SDataSource{dataSourceID: dataSource.dataSourceID, cloudID: uuid.New(), generationID: 3}
				otherCloudResult, err := otherCloud.convertAgentToClusterResource(agent)
				Expect(err).ToNot(HaveOccurred())
				Expect(otherCloudResult.ClusterResourceGroupID).ToNot(Equal(first.ClusterResourceGroupID))
			})

			It("only adds the group to the resource and does not change anything else", func() {
				grouped, err := dataSource.convertAgentToClusterResource(agent)
				Expect(err).ToNot(HaveOccurred())

				delete(agent.Annotations, ctlrutils.NodeGroupAnnotation)
				ungrouped, err := dataSource.convertAgentToClusterResource(agent)
				Expect(err).ToNot(HaveOccurred())

				grouped.ClusterResourceGroupID = uuid.Nil
				grouped.ClusterResourceGroupName = ""
				Expect(grouped).To(Equal(ungrouped))
			})

			It("leaves the resource ungrouped when the annotation is missing", func() {
				delete(agent.Annotations, ctlrutils.NodeGroupAnnotation)

				result, err := dataSource.convertAgentToClusterResource(agent)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.ClusterResourceGroupID).To(Equal(uuid.Nil))
				Expect(result.ClusterResourceGroupName).To(BeEmpty())
			})

			It("leaves the resource ungrouped when the annotation is empty", func() {
				agent.Annotations[ctlrutils.NodeGroupAnnotation] = ""

				result, err := dataSource.convertAgentToClusterResource(agent)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.ClusterResourceGroupID).To(Equal(uuid.Nil))
				Expect(result.ClusterResourceGroupName).To(BeEmpty())
			})

			It("leaves the resource ungrouped when the Agent is not bound to a cluster", func() {
				agent.Spec.ClusterDeploymentName.Name = ""

				result, err := dataSource.convertAgentToClusterResource(agent)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.ClusterResourceGroupID).To(Equal(uuid.Nil))
				Expect(result.ClusterResourceGroupName).To(BeEmpty())
			})
		})
	})
})
