/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Extensions).NotTo(BeNil())

			extensions := *result.Extensions
			Expect(extensions).To(HaveKey("kubernetesVersion"))
			Expect(extensions["kubernetesVersion"]).To(Equal("v1.29.7"))
		})

		It("extracts capacity resources to extensions", func() {
			result, err := dataSource.convertManagedClusterToNodeCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Extensions).NotTo(BeNil())

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
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Extensions).NotTo(BeNil())

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
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Extensions).NotTo(BeNil())

			extensions := *result.Extensions
			Expect(extensions).NotTo(HaveKey("kubernetesVersion"))
		})

		It("handles missing capacity gracefully", func() {
			cluster.Status.Capacity = nil

			result, err := dataSource.convertManagedClusterToNodeCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Extensions).NotTo(BeNil())

			extensions := *result.Extensions
			Expect(extensions).NotTo(HaveKey("capacity"))
		})

		It("handles missing allocatable gracefully", func() {
			cluster.Status.Allocatable = nil

			result, err := dataSource.convertManagedClusterToNodeCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Extensions).NotTo(BeNil())

			extensions := *result.Extensions
			Expect(extensions).NotTo(HaveKey("allocatable"))
		})

		It("preserves existing extensions from labels", func() {
			result, err := dataSource.convertManagedClusterToNodeCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Extensions).NotTo(BeNil())

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
})
