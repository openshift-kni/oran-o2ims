/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
)

var _ = Describe("Node Readiness Functions", func() {
	var (
		ctx    context.Context
		logger *slog.Logger
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		logger = slog.Default()
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(pluginsv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(provisioningv1alpha1.AddToScheme(scheme)).To(Succeed())
	})

	Describe("extractPRNameFromCallback", func() {
		It("should return error when callback is nil", func() {
			result, err := extractPRNameFromCallback(nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no callback configured"))
			Expect(result).To(BeEmpty())
		})

		It("should return error when callback URL is empty", func() {
			callback := &pluginsv1alpha1.Callback{
				CallbackURL: "",
			}
			result, err := extractPRNameFromCallback(callback)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no callback configured"))
			Expect(result).To(BeEmpty())
		})

		It("should return error when URL is malformed", func() {
			callback := &pluginsv1alpha1.Callback{
				CallbackURL: "://invalid-url",
			}
			result, err := extractPRNameFromCallback(callback)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse callback URL"))
			Expect(result).To(BeEmpty())
		})

		It("should return error when URL path doesn't match expected pattern", func() {
			callback := &pluginsv1alpha1.Callback{
				CallbackURL: "http://localhost/wrong-path/my-pr",
			}
			result, err := extractPRNameFromCallback(callback)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("callback URL does not match expected pattern"))
			Expect(result).To(BeEmpty())
		})

		It("should return error when PR name is empty in URL", func() {
			callback := &pluginsv1alpha1.Callback{
				CallbackURL: "http://localhost" + constants.NarCallbackServicePath + "/",
			}
			result, err := extractPRNameFromCallback(callback)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("could not extract provisioning request name"))
			Expect(result).To(BeEmpty())
		})

		It("should extract PR name from valid callback URL", func() {
			callback := &pluginsv1alpha1.Callback{
				CallbackURL: "http://localhost" + constants.NarCallbackServicePath + "/my-provisioning-request",
			}
			result, err := extractPRNameFromCallback(callback)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal("my-provisioning-request"))
		})

		It("should extract PR name with complex characters", func() {
			callback := &pluginsv1alpha1.Callback{
				CallbackURL: "http://localhost:8080" + constants.NarCallbackServicePath + "/cluster-123-pr",
			}
			result, err := extractPRNameFromCallback(callback)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal("cluster-123-pr"))
		})
	})

	Describe("getHostnameForAllocatedNode", func() {
		var (
			hubClient client.Client
			nar       *pluginsv1alpha1.NodeAllocationRequest
		)

		BeforeEach(func() {
			nar = &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nar",
					Namespace: "default",
				},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					ClusterId: "test-cluster",
				},
			}
		})

		It("should return error when callback is nil", func() {
			hubClient = fake.NewClientBuilder().WithScheme(scheme).Build()
			nar.Spec.Callback = nil

			hostname, err := getHostnameForAllocatedNode(ctx, hubClient, nar, "node-1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to extract provisioning request name"))
			Expect(hostname).To(BeEmpty())
		})

		It("should return error when callback URL doesn't match pattern", func() {
			hubClient = fake.NewClientBuilder().WithScheme(scheme).Build()
			nar.Spec.Callback = &pluginsv1alpha1.Callback{
				CallbackURL: "http://localhost/wrong-path",
			}

			hostname, err := getHostnameForAllocatedNode(ctx, hubClient, nar, "node-1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to extract provisioning request name"))
			Expect(hostname).To(BeEmpty())
		})

		It("should return error when ProvisioningRequest doesn't exist", func() {
			hubClient = fake.NewClientBuilder().WithScheme(scheme).Build()
			nar.Spec.Callback = &pluginsv1alpha1.Callback{
				CallbackURL: "http://localhost" + constants.NarCallbackServicePath + "/non-existent-pr",
			}

			hostname, err := getHostnameForAllocatedNode(ctx, hubClient, nar, "node-1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get ProvisioningRequest"))
			Expect(hostname).To(BeEmpty())
		})

		It("should return error when AllocatedNodeHostMap is nil", func() {
			pr := &provisioningv1alpha1.ProvisioningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pr",
				},
				Status: provisioningv1alpha1.ProvisioningRequestStatus{
					Extensions: provisioningv1alpha1.Extensions{
						AllocatedNodeHostMap: nil,
					},
				},
			}
			hubClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(pr).Build()
			nar.Spec.Callback = &pluginsv1alpha1.Callback{
				CallbackURL: "http://localhost" + constants.NarCallbackServicePath + "/test-pr",
			}

			hostname, err := getHostnameForAllocatedNode(ctx, hubClient, nar, "node-1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("hostname not found for AllocatedNode"))
			Expect(hostname).To(BeEmpty())
		})

		It("should return error when node is not in the map", func() {
			pr := &provisioningv1alpha1.ProvisioningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pr",
				},
				Status: provisioningv1alpha1.ProvisioningRequestStatus{
					Extensions: provisioningv1alpha1.Extensions{
						AllocatedNodeHostMap: map[string]string{
							"other-node": "other-hostname",
						},
					},
				},
			}
			hubClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(pr).Build()
			nar.Spec.Callback = &pluginsv1alpha1.Callback{
				CallbackURL: "http://localhost" + constants.NarCallbackServicePath + "/test-pr",
			}

			hostname, err := getHostnameForAllocatedNode(ctx, hubClient, nar, "node-1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("hostname not found for AllocatedNode node-1"))
			Expect(hostname).To(BeEmpty())
		})

		It("should return hostname when node is in the map", func() {
			pr := &provisioningv1alpha1.ProvisioningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pr",
				},
				Status: provisioningv1alpha1.ProvisioningRequestStatus{
					Extensions: provisioningv1alpha1.Extensions{
						AllocatedNodeHostMap: map[string]string{
							"node-1": "worker-1.example.com",
							"node-2": "worker-2.example.com",
						},
					},
				},
			}
			hubClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(pr).Build()
			nar.Spec.Callback = &pluginsv1alpha1.Callback{
				CallbackURL: "http://localhost" + constants.NarCallbackServicePath + "/test-pr",
			}

			hostname, err := getHostnameForAllocatedNode(ctx, hubClient, nar, "node-1")
			Expect(err).ToNot(HaveOccurred())
			Expect(hostname).To(Equal("worker-1.example.com"))
		})
	})

	Describe("isNodeReady", func() {
		var spokeClient client.Client

		It("should return error when node doesn't exist", func() {
			spokeClient = fake.NewClientBuilder().WithScheme(scheme).Build()

			ready, err := isNodeReady(ctx, spokeClient, logger, "test-cluster", "non-existent-node", "allocated-node-1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get node"))
			Expect(ready).To(BeFalse())
		})

		It("should return false when NodeReady condition is not found", func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeMemoryPressure,
							Status: corev1.ConditionFalse,
						},
					},
				},
			}
			spokeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()

			ready, err := isNodeReady(ctx, spokeClient, logger, "test-cluster", "test-node", "allocated-node-1")
			Expect(err).ToNot(HaveOccurred())
			Expect(ready).To(BeFalse())
		})

		It("should return false when NodeReady condition is False", func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:    corev1.NodeReady,
							Status:  corev1.ConditionFalse,
							Reason:  "KubeletNotReady",
							Message: "container runtime is not ready",
						},
					},
				},
			}
			spokeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()

			ready, err := isNodeReady(ctx, spokeClient, logger, "test-cluster", "test-node", "allocated-node-1")
			Expect(err).ToNot(HaveOccurred())
			Expect(ready).To(BeFalse())
		})

		It("should return false when NodeReady condition is Unknown", func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:    corev1.NodeReady,
							Status:  corev1.ConditionUnknown,
							Reason:  "NodeStatusUnknown",
							Message: "Kubelet stopped posting node status",
						},
					},
				},
			}
			spokeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()

			ready, err := isNodeReady(ctx, spokeClient, logger, "test-cluster", "test-node", "allocated-node-1")
			Expect(err).ToNot(HaveOccurred())
			Expect(ready).To(BeFalse())
		})

		It("should return true when NodeReady condition is True", func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:    corev1.NodeReady,
							Status:  corev1.ConditionTrue,
							Reason:  "KubeletReady",
							Message: "kubelet is posting ready status",
						},
					},
				},
			}
			spokeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()

			ready, err := isNodeReady(ctx, spokeClient, logger, "test-cluster", "test-node", "allocated-node-1")
			Expect(err).ToNot(HaveOccurred())
			Expect(ready).To(BeTrue())
		})

		It("should return true when NodeReady is True among multiple conditions", func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeMemoryPressure,
							Status: corev1.ConditionFalse,
						},
						{
							Type:   corev1.NodeDiskPressure,
							Status: corev1.ConditionFalse,
						},
						{
							Type:    corev1.NodeReady,
							Status:  corev1.ConditionTrue,
							Reason:  "KubeletReady",
							Message: "kubelet is posting ready status",
						},
						{
							Type:   corev1.NodePIDPressure,
							Status: corev1.ConditionFalse,
						},
					},
				},
			}
			spokeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()

			ready, err := isNodeReady(ctx, spokeClient, logger, "test-cluster", "test-node", "allocated-node-1")
			Expect(err).ToNot(HaveOccurred())
			Expect(ready).To(BeTrue())
		})
	})
})
