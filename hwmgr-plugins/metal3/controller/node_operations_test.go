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
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
)

var _ = Describe("Node Operations", func() {
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
		Expect(machineconfigv1.Install(scheme)).To(Succeed())
	})

	Describe("nodeOps.DrainNode", func() {
		It("should be a no-op when skipDrain is true", func() {
			ops := NewNodeOps(nil, nil, logger, true)
			Expect(ops.DrainNode(ctx, "any-node")).To(Succeed())
		})
	})

	Describe("nodeOps.UncordonNode", func() {
		It("should be a no-op when skipDrain is true", func() {
			ops := NewNodeOps(nil, nil, logger, true)
			Expect(ops.UncordonNode(ctx, "any-node")).To(Succeed())
		})
	})

	Describe("nodeOps.IsNodeReady", func() {
		var ops NodeOps

		newOps := func(spokeClient client.Client) NodeOps {
			return NewNodeOps(spokeClient, nil, logger, false)
		}

		It("should return error when node doesn't exist", func() {
			spokeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			ops = newOps(spokeClient)

			ready, err := ops.IsNodeReady(ctx, "non-existent-node")
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
			spokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()
			ops = newOps(spokeClient)

			ready, err := ops.IsNodeReady(ctx, "test-node")
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
			spokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()
			ops = newOps(spokeClient)

			ready, err := ops.IsNodeReady(ctx, "test-node")
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
			spokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()
			ops = newOps(spokeClient)

			ready, err := ops.IsNodeReady(ctx, "test-node")
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
			spokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()
			ops = newOps(spokeClient)

			ready, err := ops.IsNodeReady(ctx, "test-node")
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
			spokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()
			ops = newOps(spokeClient)

			ready, err := ops.IsNodeReady(ctx, "test-node")
			Expect(err).ToNot(HaveOccurred())
			Expect(ready).To(BeTrue())
		})
	})

	Describe("nodeOps.GetMaxUnavailable", func() {
		newOps := func(spokeClient client.Client) NodeOps {
			return NewNodeOps(spokeClient, nil, logger, false)
		}

		intOrStrPtr := func(val intstr.IntOrString) *intstr.IntOrString {
			return &val
		}

		It("should return 1 for SNO (totalNodes == 1) without MCP lookup", func() {
			spokeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			ops := newOps(spokeClient)

			result, err := ops.GetMaxUnavailable(ctx, "master", 1)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(1))
		})

		It("should default to 1 when MCP is not found", func() {
			spokeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			ops := newOps(spokeClient)

			result, err := ops.GetMaxUnavailable(ctx, "worker", 8)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(1))
		})

		It("should default to 1 when MCP has nil maxUnavailable", func() {
			mcp := &machineconfigv1.MachineConfigPool{
				ObjectMeta: metav1.ObjectMeta{Name: "worker"},
				Spec:       machineconfigv1.MachineConfigPoolSpec{},
			}
			spokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcp).Build()
			ops := newOps(spokeClient)

			result, err := ops.GetMaxUnavailable(ctx, "worker", 8)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(1))
		})

		It("should return absolute int value from MCP", func() {
			mcp := &machineconfigv1.MachineConfigPool{
				ObjectMeta: metav1.ObjectMeta{Name: "worker"},
				Spec: machineconfigv1.MachineConfigPoolSpec{
					MaxUnavailable: intOrStrPtr(intstr.FromInt32(3)),
				},
			}
			spokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcp).Build()
			ops := newOps(spokeClient)

			result, err := ops.GetMaxUnavailable(ctx, "worker", 8)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(3))
		})

		It("should compute percentage value from MCP (25% of 8 = 2)", func() {
			mcp := &machineconfigv1.MachineConfigPool{
				ObjectMeta: metav1.ObjectMeta{Name: "worker"},
				Spec: machineconfigv1.MachineConfigPoolSpec{
					MaxUnavailable: intOrStrPtr(intstr.FromString("25%")),
				},
			}
			spokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcp).Build()
			ops := newOps(spokeClient)

			result, err := ops.GetMaxUnavailable(ctx, "worker", 8)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(2))
		})

		It("should floor to 1 when percentage rounds to 0 (10% of 3 = 0 -> 1)", func() {
			mcp := &machineconfigv1.MachineConfigPool{
				ObjectMeta: metav1.ObjectMeta{Name: "master"},
				Spec: machineconfigv1.MachineConfigPoolSpec{
					MaxUnavailable: intOrStrPtr(intstr.FromString("10%")),
				},
			}
			spokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcp).Build()
			ops := newOps(spokeClient)

			result, err := ops.GetMaxUnavailable(ctx, "master", 3)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(1))
		})

		It("should compute 100% correctly (100% of 8 = 8)", func() {
			mcp := &machineconfigv1.MachineConfigPool{
				ObjectMeta: metav1.ObjectMeta{Name: "worker"},
				Spec: machineconfigv1.MachineConfigPoolSpec{
					MaxUnavailable: intOrStrPtr(intstr.FromString("100%")),
				},
			}
			spokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcp).Build()
			ops := newOps(spokeClient)

			result, err := ops.GetMaxUnavailable(ctx, "worker", 8)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(8))
		})
	})
})
