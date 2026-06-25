/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
)

var _ = Describe("collectNodeDetails", func() {
	It("sorts nodes by name for deterministic ordering", func() {
		nodeList := &hwmgmtv1alpha1.AllocatedNodeList{
			Items: []hwmgmtv1alpha1.AllocatedNode{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-z"},
					Spec:       hwmgmtv1alpha1.AllocatedNodeSpec{GroupName: "worker"},
					Status: hwmgmtv1alpha1.AllocatedNodeStatus{
						BMC: &hwmgmtv1alpha1.BMC{Address: "10.0.0.3", CredentialsName: "secret-z"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
					Spec:       hwmgmtv1alpha1.AllocatedNodeSpec{GroupName: "worker"},
					Status: hwmgmtv1alpha1.AllocatedNodeStatus{
						BMC: &hwmgmtv1alpha1.BMC{Address: "10.0.0.1", CredentialsName: "secret-a"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-m"},
					Spec:       hwmgmtv1alpha1.AllocatedNodeSpec{GroupName: "worker"},
					Status: hwmgmtv1alpha1.AllocatedNodeStatus{
						BMC: &hwmgmtv1alpha1.BMC{Address: "10.0.0.2", CredentialsName: "secret-m"},
					},
				},
			},
		}

		hwNodes, err := collectNodeDetails(nodeList)
		Expect(err).ToNot(HaveOccurred())

		workers := hwNodes["worker"]
		Expect(workers).To(HaveLen(3))
		Expect(workers[0].NodeID).To(Equal("node-a"))
		Expect(workers[1].NodeID).To(Equal("node-m"))
		Expect(workers[2].NodeID).To(Equal("node-z"))
	})

	It("returns error when node has no BMC details", func() {
		nodeList := &hwmgmtv1alpha1.AllocatedNodeList{
			Items: []hwmgmtv1alpha1.AllocatedNode{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Spec:       hwmgmtv1alpha1.AllocatedNodeSpec{GroupName: "worker"},
					Status:     hwmgmtv1alpha1.AllocatedNodeStatus{},
				},
			},
		}

		_, err := collectNodeDetails(nodeList)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("does not have BMC details"))
	})

	It("returns error when node has empty BMC credentials", func() {
		nodeList := &hwmgmtv1alpha1.AllocatedNodeList{
			Items: []hwmgmtv1alpha1.AllocatedNode{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Spec:       hwmgmtv1alpha1.AllocatedNodeSpec{GroupName: "worker"},
					Status: hwmgmtv1alpha1.AllocatedNodeStatus{
						BMC: &hwmgmtv1alpha1.BMC{Address: "10.0.0.1", CredentialsName: ""},
					},
				},
			},
		}

		_, err := collectNodeDetails(nodeList)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("does not have BMC details"))
	})

	It("groups nodes by group name across multiple groups", func() {
		nodeList := &hwmgmtv1alpha1.AllocatedNodeList{
			Items: []hwmgmtv1alpha1.AllocatedNode{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "worker-2"},
					Spec:       hwmgmtv1alpha1.AllocatedNodeSpec{GroupName: "worker"},
					Status: hwmgmtv1alpha1.AllocatedNodeStatus{
						BMC: &hwmgmtv1alpha1.BMC{Address: "10.0.0.2", CredentialsName: "secret-w2"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "master-1"},
					Spec:       hwmgmtv1alpha1.AllocatedNodeSpec{GroupName: "controller"},
					Status: hwmgmtv1alpha1.AllocatedNodeStatus{
						BMC: &hwmgmtv1alpha1.BMC{Address: "10.0.0.3", CredentialsName: "secret-m1"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
					Spec:       hwmgmtv1alpha1.AllocatedNodeSpec{GroupName: "worker"},
					Status: hwmgmtv1alpha1.AllocatedNodeStatus{
						BMC: &hwmgmtv1alpha1.BMC{Address: "10.0.0.1", CredentialsName: "secret-w1"},
					},
				},
			},
		}

		hwNodes, err := collectNodeDetails(nodeList)
		Expect(err).ToNot(HaveOccurred())

		Expect(hwNodes).To(HaveLen(2))

		workers := hwNodes["worker"]
		Expect(workers).To(HaveLen(2))
		Expect(workers[0].NodeID).To(Equal("worker-1"))
		Expect(workers[1].NodeID).To(Equal("worker-2"))

		controllers := hwNodes["controller"]
		Expect(controllers).To(HaveLen(1))
		Expect(controllers[0].NodeID).To(Equal("master-1"))
	})

	It("returns empty map for empty node list", func() {
		nodeList := &hwmgmtv1alpha1.AllocatedNodeList{}

		hwNodes, err := collectNodeDetails(nodeList)
		Expect(err).ToNot(HaveOccurred())
		Expect(hwNodes).To(BeEmpty())
	})
})
