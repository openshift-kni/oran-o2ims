/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Test Cases for NodeAllocationRequest Controller

This test suite covers the hardware manager's NodeAllocationRequest controller,
focusing on the new timeout handling implementation that was moved from the O-Cloud Manager.

Key Test Areas:
1. checkHardwareTimeout function - Core timeout detection logic
2. HardwareProvisioningTimeout field handling
3. Day 2 retry scenarios with spec changes
4. Callback integration for timeout notifications
5. Integration with HardwareOperationStartTime
*/

package controller

import (
	"context"
	"log/slog"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/internal/hardwaremanager/utils"
	"github.com/openshift-kni/oran-o2ims/internal/spokeclient"
)

var _ = Describe("NodeAllocationRequest Controller Timeout Handling", func() {
	var (
		c          client.Client
		reconciler *NodeAllocationRequestReconciler
		logger     *slog.Logger
	)

	BeforeEach(func() {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

		scheme := runtime.NewScheme()
		Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())

		c = fake.NewClientBuilder().WithScheme(scheme).Build()

		reconciler = &NodeAllocationRequestReconciler{
			Client:          c,
			NoncachedClient: c,
			Logger:          logger,
		}
	})

	Describe("checkHardwareTimeout", func() {
		var nar *hwmgmtv1alpha1.NodeAllocationRequest

		BeforeEach(func() {
			nar = &hwmgmtv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nar",
					Namespace: "default",
				},
				Spec: hwmgmtv1alpha1.NodeAllocationRequestSpec{
					HardwareProvisioningTimeout: &metav1.Duration{Duration: 5 * time.Minute},
				},
				Status: hwmgmtv1alpha1.NodeAllocationRequestStatus{
					Conditions: []metav1.Condition{},
				},
			}
		})

		Context("when HardwareProvisioningTimeout is specified", func() {
			It("should use the specified timeout value", func() {
				nar.Spec.HardwareProvisioningTimeout = &metav1.Duration{Duration: 10 * time.Minute}
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(context.Background(), nar)
				Expect(err).ToNot(HaveOccurred())
				Expect(timeoutExceeded).To(BeFalse())
				Expect(conditionType).To(Equal(hwmgmtv1alpha1.ConditionType("")))
			})
		})

		Context("when HardwareProvisioningTimeout is nil", func() {
			It("should use default timeout", func() {
				nar.Spec.HardwareProvisioningTimeout = nil
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(context.Background(), nar)
				Expect(err).ToNot(HaveOccurred())
				Expect(timeoutExceeded).To(BeFalse())
				Expect(conditionType).To(Equal(hwmgmtv1alpha1.ConditionType("")))
			})
		})

		Context("when HardwareProvisioningTimeout is invalid", func() {
			It("should return error for zero timeout", func() {
				nar.Spec.HardwareProvisioningTimeout = &metav1.Duration{Duration: 0}
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(context.Background(), nar)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("hardware provisioning timeout must be > 0"))
				Expect(timeoutExceeded).To(BeFalse())
				Expect(conditionType).To(Equal(hwmgmtv1alpha1.ConditionType("")))
			})
		})

		Context("when provisioning is in progress and times out", func() {
			BeforeEach(func() {
				// Set operation start time to 10 minutes ago (exceeds 5m timeout)
				startTime := metav1.Time{Time: time.Now().Add(-10 * time.Minute)}
				nar.Status.HardwareOperationStartTime = &startTime

				// Add provisioning condition in progress
				hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
					string(hwmgmtv1alpha1.Provisioned),
					string(hwmgmtv1alpha1.InProgress),
					metav1.ConditionFalse,
					"Hardware provisioning in progress")
			})

			It("should detect provisioning timeout", func() {
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(context.Background(), nar)
				Expect(err).ToNot(HaveOccurred())
				Expect(timeoutExceeded).To(BeTrue())
				Expect(conditionType).To(Equal(hwmgmtv1alpha1.Provisioned))
			})
		})

		Context("when provisioning is in progress but not timed out", func() {
			BeforeEach(func() {
				// Set operation start time to 2 minutes ago (within 5m timeout)
				startTime := metav1.Time{Time: time.Now().Add(-2 * time.Minute)}
				nar.Status.HardwareOperationStartTime = &startTime

				// Add provisioning condition in progress
				hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
					string(hwmgmtv1alpha1.Provisioned),
					string(hwmgmtv1alpha1.InProgress),
					metav1.ConditionFalse,
					"Hardware provisioning in progress")
			})

			It("should not detect timeout", func() {
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(context.Background(), nar)
				Expect(err).ToNot(HaveOccurred())
				Expect(timeoutExceeded).To(BeFalse())
				Expect(conditionType).To(Equal(hwmgmtv1alpha1.ConditionType("")))
			})
		})

		Context("when configuration is in progress and times out", func() {
			BeforeEach(func() {
				// Set provisioning as completed
				hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
					string(hwmgmtv1alpha1.Provisioned),
					string(hwmgmtv1alpha1.Completed),
					metav1.ConditionTrue,
					"Hardware provisioning completed")

				// Set operation start time to 10 minutes ago (exceeds 5m timeout)
				startTime := metav1.Time{Time: time.Now().Add(-10 * time.Minute)}
				nar.Status.HardwareOperationStartTime = &startTime

				// Add configuration condition in progress
				hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
					string(hwmgmtv1alpha1.Configured),
					string(hwmgmtv1alpha1.InProgress),
					metav1.ConditionFalse,
					"Hardware configuration in progress")
			})

			It("should detect configuration timeout", func() {
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(context.Background(), nar)
				Expect(err).ToNot(HaveOccurred())
				Expect(timeoutExceeded).To(BeTrue())
				Expect(conditionType).To(Equal(hwmgmtv1alpha1.Configured))
			})
		})

		Context("when configuration is in progress but not timed out", func() {
			BeforeEach(func() {
				// Set provisioning as completed
				hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
					string(hwmgmtv1alpha1.Provisioned),
					string(hwmgmtv1alpha1.Completed),
					metav1.ConditionTrue,
					"Hardware provisioning completed")

				// Set operation start time to 2 minutes ago (within 5m timeout)
				startTime := metav1.Time{Time: time.Now().Add(-2 * time.Minute)}
				nar.Status.HardwareOperationStartTime = &startTime

				// Add configuration condition in progress
				hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
					string(hwmgmtv1alpha1.Configured),
					string(hwmgmtv1alpha1.InProgress),
					metav1.ConditionFalse,
					"Hardware configuration in progress")
			})

			It("should not detect timeout", func() {
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(context.Background(), nar)
				Expect(err).ToNot(HaveOccurred())
				Expect(timeoutExceeded).To(BeFalse())
				Expect(conditionType).To(Equal(hwmgmtv1alpha1.ConditionType("")))
			})
		})

		Context("when both provisioning and configuration are completed", func() {
			BeforeEach(func() {
				// Set both conditions as completed
				hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
					string(hwmgmtv1alpha1.Provisioned),
					string(hwmgmtv1alpha1.Completed),
					metav1.ConditionTrue,
					"Hardware provisioning completed")

				hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
					string(hwmgmtv1alpha1.Configured),
					string(hwmgmtv1alpha1.Completed),
					metav1.ConditionTrue,
					"Hardware configuration completed")
			})

			It("should not detect any timeout", func() {
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(context.Background(), nar)
				Expect(err).ToNot(HaveOccurred())
				Expect(timeoutExceeded).To(BeFalse())
				Expect(conditionType).To(Equal(hwmgmtv1alpha1.ConditionType("")))
			})
		})

		Context("when provisioning is in progress but HardwareOperationStartTime is missing", func() {
			BeforeEach(func() {
				// Add provisioning condition in progress but no start time
				hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
					string(hwmgmtv1alpha1.Provisioned),
					string(hwmgmtv1alpha1.InProgress),
					metav1.ConditionFalse,
					"Hardware provisioning in progress")
			})

			It("should not detect timeout without start time", func() {
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(context.Background(), nar)
				Expect(err).ToNot(HaveOccurred())
				Expect(timeoutExceeded).To(BeFalse())
				Expect(conditionType).To(Equal(hwmgmtv1alpha1.ConditionType("")))
			})
		})

		Context("when configuration is in progress but HardwareOperationStartTime is missing", func() {
			BeforeEach(func() {
				// Set provisioning as completed
				hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
					string(hwmgmtv1alpha1.Provisioned),
					string(hwmgmtv1alpha1.Completed),
					metav1.ConditionTrue,
					"Hardware provisioning completed")

				// Add configuration condition in progress but no start time
				hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
					string(hwmgmtv1alpha1.Configured),
					string(hwmgmtv1alpha1.InProgress),
					metav1.ConditionFalse,
					"Hardware configuration in progress")
			})

			It("should not detect timeout without start time", func() {
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(context.Background(), nar)
				Expect(err).ToNot(HaveOccurred())
				Expect(timeoutExceeded).To(BeFalse())
				Expect(conditionType).To(Equal(hwmgmtv1alpha1.ConditionType("")))
			})
		})
	})

	Describe("Day 2 retry scenarios", func() {
		var nar *hwmgmtv1alpha1.NodeAllocationRequest

		BeforeEach(func() {
			nar = &hwmgmtv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nar-day2",
					Namespace: "default",
				},
				Spec: hwmgmtv1alpha1.NodeAllocationRequestSpec{
					HardwareProvisioningTimeout: &metav1.Duration{Duration: 5 * time.Minute},
					ConfigTransactionId:         2, // Indicates spec change
				},
				Status: hwmgmtv1alpha1.NodeAllocationRequestStatus{
					Conditions: []metav1.Condition{},
				},
			}
		})

		Context("when configuration failed and spec changed", func() {
			BeforeEach(func() {
				// Set provisioning as completed
				hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
					string(hwmgmtv1alpha1.Provisioned),
					string(hwmgmtv1alpha1.Completed),
					metav1.ConditionTrue,
					"Hardware provisioning completed")

				// Set configuration as failed
				hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
					string(hwmgmtv1alpha1.Configured),
					string(hwmgmtv1alpha1.Failed),
					metav1.ConditionFalse,
					"Hardware configuration failed")

				// Set operation start time to old (exceeded timeout) - this should be ignored when spec changes
				startTime := metav1.Time{Time: time.Now().Add(-10 * time.Minute)}
				nar.Status.HardwareOperationStartTime = &startTime

				// Set ObservedConfigTransactionId to 1, but Spec.ConfigTransactionId is 2 (mismatch = spec change)
				nar.Status.ObservedConfigTransactionId = 1
			})

			It("should allow retry when spec changes", func() {
				// The hardware manager controller should detect the spec change and skip timeout checking
				// This allows retry even when the previous configuration failed/timed out
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(context.Background(), nar)
				Expect(err).ToNot(HaveOccurred())
				Expect(timeoutExceeded).To(BeFalse())
				Expect(conditionType).To(Equal(hwmgmtv1alpha1.ConditionType("")))
			})
		})

		Context("when configuration timed out and spec changed", func() {
			BeforeEach(func() {
				// Set provisioning as completed
				hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
					string(hwmgmtv1alpha1.Provisioned),
					string(hwmgmtv1alpha1.Completed),
					metav1.ConditionTrue,
					"Hardware provisioning completed")

				// Set configuration as timed out
				hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
					string(hwmgmtv1alpha1.Configured),
					string(hwmgmtv1alpha1.TimedOut),
					metav1.ConditionFalse,
					"Hardware configuration timed out")

				// Set operation start time to old (exceeded timeout) - this should be ignored when spec changes
				startTime := metav1.Time{Time: time.Now().Add(-10 * time.Minute)}
				nar.Status.HardwareOperationStartTime = &startTime

				// Set ObservedConfigTransactionId to 1, but Spec.ConfigTransactionId is 2 (mismatch = spec change)
				nar.Status.ObservedConfigTransactionId = 1
			})

			It("should allow retry when spec changes", func() {
				// Similar to failed case, should allow retry with spec change
				// Timeout check should be skipped when spec changes
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(context.Background(), nar)
				Expect(err).ToNot(HaveOccurred())
				Expect(timeoutExceeded).To(BeFalse())
				Expect(conditionType).To(Equal(hwmgmtv1alpha1.ConditionType("")))
			})
		})
	})
})

var _ = Describe("handleScaleOut", func() {
	var (
		reconciler *NodeAllocationRequestReconciler
		fakeClient client.Client
		ctx        context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		testLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		scheme := runtime.NewScheme()
		Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())

		fakeClient = fake.NewClientBuilder().WithScheme(scheme).
			WithStatusSubresource(&hwmgmtv1alpha1.NodeAllocationRequest{}).Build()

		reconciler = &NodeAllocationRequestReconciler{
			Client:          fakeClient,
			NoncachedClient: fakeClient,
			Logger:          testLogger,
			Namespace:       "oran-o2ims",
		}
	})

	It("should set Provisioned=InProgress when NodeGroup size increased", func() {
		nar := &hwmgmtv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-nar",
				Namespace:  "oran-o2ims",
				Generation: 3,
			},
			Spec: hwmgmtv1alpha1.NodeAllocationRequestSpec{
				NodeGroup: []hwmgmtv1alpha1.NodeGroup{
					{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", Role: "worker"}, Size: 3},
				},
			},
		}
		Expect(fakeClient.Create(ctx, nar)).To(Succeed())
		hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
			string(hwmgmtv1alpha1.Provisioned), string(hwmgmtv1alpha1.Completed),
			metav1.ConditionTrue, "Provisioned")
		Expect(fakeClient.Status().Update(ctx, nar)).To(Succeed())

		// Only 2 AllocatedNodes exist — Size is 3, so scale-out needed
		for _, name := range []string{"w1", "w2"} {
			node := &hwmgmtv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{
					Name: name, Namespace: "oran-o2ims",
					Labels: map[string]string{"clcm.openshift.io/nodeAllocationRequest": "test-nar"},
				},
				Spec: hwmgmtv1alpha1.AllocatedNodeSpec{
					GroupName:             "worker",
					NodeAllocationRequest: "test-nar",
				},
			}
			Expect(fakeClient.Create(ctx, node)).To(Succeed())
		}

		_, handled, err := reconciler.handleScaleOut(ctx, nar)
		Expect(err).ToNot(HaveOccurred())
		Expect(handled).To(BeTrue())

		// Verify Provisioned condition was set to InProgress
		updatedNAR := &hwmgmtv1alpha1.NodeAllocationRequest{}
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(nar), updatedNAR)).To(Succeed())
		provCond := meta.FindStatusCondition(updatedNAR.Status.Conditions, string(hwmgmtv1alpha1.Provisioned))
		Expect(provCond).ToNot(BeNil())
		Expect(provCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(provCond.Reason).To(Equal(string(hwmgmtv1alpha1.InProgress)))

		// ObservedGeneration must NOT be advanced, so that the FSM re-enters
		// SpecChanged after allocation completes to process other spec changes
		// (e.g., HwProfile updates in the same generation).
		Expect(updatedNAR.Status.ObservedGeneration).To(Equal(int64(0)))
	})

	It("should not trigger when allocated count matches desired size", func() {
		nar := &hwmgmtv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-nar",
				Namespace:  "oran-o2ims",
				Generation: 2,
			},
			Spec: hwmgmtv1alpha1.NodeAllocationRequestSpec{
				NodeGroup: []hwmgmtv1alpha1.NodeGroup{
					{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{Name: "worker", Role: "worker"}, Size: 2},
				},
			},
		}
		Expect(fakeClient.Create(ctx, nar)).To(Succeed())

		for _, name := range []string{"w1", "w2"} {
			node := &hwmgmtv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{
					Name: name, Namespace: "oran-o2ims",
					Labels: map[string]string{"clcm.openshift.io/nodeAllocationRequest": "test-nar"},
				},
				Spec: hwmgmtv1alpha1.AllocatedNodeSpec{
					GroupName:             "worker",
					NodeAllocationRequest: "test-nar",
				},
			}
			Expect(fakeClient.Create(ctx, node)).To(Succeed())
		}

		_, handled, err := reconciler.handleScaleOut(ctx, nar)
		Expect(err).ToNot(HaveOccurred())
		Expect(handled).To(BeFalse())
	})
})

var _ = Describe("handleScaleInAnnotations", func() {
	var (
		ctx        context.Context
		fakeClient client.Client
		reconciler *NodeAllocationRequestReconciler
		logger     *slog.Logger
	)

	BeforeEach(func() {
		ctx = context.Background()
		logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

		scheme := runtime.NewScheme()
		Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(provisioningv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(addonv1alpha1.Install(scheme)).To(Succeed())
		Expect(clusterv1.Install(scheme)).To(Succeed())
		Expect(workv1.Install(scheme)).To(Succeed())
		Expect(msav1beta1.AddToScheme(scheme)).To(Succeed())

		fakeClient = fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(
				&hwmgmtv1alpha1.AllocatedNode{},
				&hwmgmtv1alpha1.NodeAllocationRequest{},
				&provisioningv1alpha1.ProvisioningRequest{},
			).
			WithIndex(&hwmgmtv1alpha1.AllocatedNode{}, "spec.nodeAllocationRequest",
				func(obj client.Object) []string {
					an := obj.(*hwmgmtv1alpha1.AllocatedNode)
					return []string{an.Spec.NodeAllocationRequest}
				}).
			Build()

		reconciler = &NodeAllocationRequestReconciler{
			Client:    fakeClient,
			Logger:    logger,
			Namespace: "test-ns",
		}

		// Create a ProvisioningRequest (looked up by ensureSpokeClients)
		pr := &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-nar",
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				Name:            "test",
				TemplateName:    "test-template",
				TemplateVersion: "v1",
			},
		}
		Expect(fakeClient.Create(ctx, pr)).To(Succeed())
		pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{
			Name: "test-cluster",
		}
		Expect(fakeClient.Status().Update(ctx, pr)).To(Succeed())
	})

	It("should return handled=false when no annotation is present", func() {
		nar := &hwmgmtv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-nar",
				Namespace: "test-ns",
			},
		}
		Expect(fakeClient.Create(ctx, nar)).To(Succeed())

		_, handled, err := reconciler.handleScaleInAnnotations(ctx, nar)
		Expect(err).ToNot(HaveOccurred())
		Expect(handled).To(BeFalse())
	})

	It("should return handled=true when scale-in annotation is present", func() {
		nar := &hwmgmtv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-nar",
				Namespace: "test-ns",
				Annotations: map[string]string{
					ScaleInNodesAnnotation: "node-1",
				},
			},
		}
		Expect(fakeClient.Create(ctx, nar)).To(Succeed())

		_, handled, err := reconciler.handleScaleInAnnotations(ctx, nar)
		Expect(err).ToNot(HaveOccurred())
		Expect(handled).To(BeTrue())
	})

	It("should set Deprovisioned condition and delete AllocatedNode when no spoke", func() {
		nar := &hwmgmtv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-nar",
				Namespace: "test-ns",
				Annotations: map[string]string{
					ScaleInNodesAnnotation: "node-1",
				},
			},
		}
		Expect(fakeClient.Create(ctx, nar)).To(Succeed())
		nar.Status.Properties.NodeNames = []string{"node-1", "node-2"}
		Expect(fakeClient.Status().Update(ctx, nar)).To(Succeed())

		node := &hwmgmtv1alpha1.AllocatedNode{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "node-1",
				Namespace: "test-ns",
			},
			Spec: hwmgmtv1alpha1.AllocatedNodeSpec{
				NodeAllocationRequest: "test-nar",
			},
		}
		Expect(fakeClient.Create(ctx, node)).To(Succeed())
		node.Status.Hostname = "worker1.example.com"
		Expect(fakeClient.Status().Update(ctx, node)).To(Succeed())

		_, handled, err := reconciler.handleScaleInAnnotations(ctx, nar)
		Expect(err).ToNot(HaveOccurred())
		Expect(handled).To(BeTrue())

		// Verify annotation was removed
		updatedNAR := &hwmgmtv1alpha1.NodeAllocationRequest{}
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(nar), updatedNAR)).To(Succeed())
		Expect(updatedNAR.GetAnnotations()).ToNot(HaveKey(ScaleInNodesAnnotation))

		// Verify nodeNames was pruned
		Expect(updatedNAR.Status.Properties.NodeNames).To(ConsistOf("node-2"))

		// Verify AllocatedNode was deleted
		deletedNode := &hwmgmtv1alpha1.AllocatedNode{}
		err = fakeClient.Get(ctx, client.ObjectKeyFromObject(node), deletedNode)
		Expect(k8serrors.IsNotFound(err)).To(BeTrue())
	})

	It("should skip AllocatedNodes that are not found and prune nodeNames", func() {
		nar := &hwmgmtv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-nar",
				Namespace: "test-ns",
				Annotations: map[string]string{
					ScaleInNodesAnnotation: "nonexistent-node",
				},
			},
		}
		Expect(fakeClient.Create(ctx, nar)).To(Succeed())
		nar.Status.Properties.NodeNames = []string{"nonexistent-node", "other-node"}
		Expect(fakeClient.Status().Update(ctx, nar)).To(Succeed())

		_, handled, err := reconciler.handleScaleInAnnotations(ctx, nar)
		Expect(err).ToNot(HaveOccurred())
		Expect(handled).To(BeTrue())

		// Verify annotation was removed
		updatedNAR := &hwmgmtv1alpha1.NodeAllocationRequest{}
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(nar), updatedNAR)).To(Succeed())
		Expect(updatedNAR.GetAnnotations()).ToNot(HaveKey(ScaleInNodesAnnotation))

		// Verify nodeNames was pruned even for NotFound nodes
		Expect(updatedNAR.Status.Properties.NodeNames).To(ConsistOf("other-node"))
	})
})

var _ = Describe("handleNodeAllocationRequestSpecChanged", func() {
	var (
		reconciler *NodeAllocationRequestReconciler
		fakeClient client.Client
		ctx        context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		testLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		scheme := runtime.NewScheme()
		Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(provisioningv1alpha1.AddToScheme(scheme)).To(Succeed())

		fakeClient = fake.NewClientBuilder().WithScheme(scheme).
			WithStatusSubresource(&hwmgmtv1alpha1.NodeAllocationRequest{}, &hwmgmtv1alpha1.AllocatedNode{}).
			WithIndex(&hwmgmtv1alpha1.AllocatedNode{}, "spec.nodeAllocationRequest", func(obj client.Object) []string {
				return []string{obj.(*hwmgmtv1alpha1.AllocatedNode).Spec.NodeAllocationRequest}
			}).
			Build()

		reconciler = &NodeAllocationRequestReconciler{
			Client:          fakeClient,
			NoncachedClient: fakeClient,
			Logger:          testLogger,
			Namespace:       "oran-o2ims",
		}
	})

	createNAR := func(configuredReason string, configuredStatus metav1.ConditionStatus) *hwmgmtv1alpha1.NodeAllocationRequest {
		nar := &hwmgmtv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-nar",
				Namespace:  "oran-o2ims",
				Generation: 2,
			},
			Spec: hwmgmtv1alpha1.NodeAllocationRequestSpec{
				NodeGroup: []hwmgmtv1alpha1.NodeGroup{
					{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
						Name: "master", Role: hwmgmtv1alpha1.NodeRoleMaster, HwProfile: "profile-v1",
					}, Size: 2},
				},
			},
		}
		Expect(fakeClient.Create(ctx, nar)).To(Succeed())
		hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
			string(hwmgmtv1alpha1.Provisioned), string(hwmgmtv1alpha1.Completed),
			metav1.ConditionTrue, "Provisioned")
		hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
			string(hwmgmtv1alpha1.Configured), configuredReason,
			configuredStatus, "stale")
		nar.Status.ObservedGeneration = 1
		Expect(fakeClient.Status().Update(ctx, nar)).To(Succeed())
		return nar
	}

	createNode := func(name, reason string, status metav1.ConditionStatus) {
		node := &hwmgmtv1alpha1.AllocatedNode{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "oran-o2ims",
			},
			Spec: hwmgmtv1alpha1.AllocatedNodeSpec{
				GroupName:             "master",
				HwProfile:             "profile-v1",
				NodeAllocationRequest: "test-nar",
			},
		}
		Expect(fakeClient.Create(ctx, node)).To(Succeed())
		hwmgrutils.SetStatusCondition(&node.Status.Conditions,
			string(hwmgmtv1alpha1.Configured), reason, status, reason)
		Expect(fakeClient.Status().Update(ctx, node)).To(Succeed())
	}

	It("should keep Configured=Failed when a child node is Failed after a spec revert", func() {
		nar := createNAR(string(hwmgmtv1alpha1.Failed), metav1.ConditionFalse)
		createNode("n1", string(hwmgmtv1alpha1.Failed), metav1.ConditionFalse)
		createNode("n2", string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)

		result, err := reconciler.handleNodeAllocationRequestSpecChanged(ctx, nar)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		updated := &hwmgmtv1alpha1.NodeAllocationRequest{}
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(nar), updated)).To(Succeed())
		cond := meta.FindStatusCondition(updated.Status.Conditions, string(hwmgmtv1alpha1.Configured))
		Expect(cond).ToNot(BeNil())
		Expect(cond.Reason).To(Equal(string(hwmgmtv1alpha1.Failed)))
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(updated.Status.ObservedGeneration).To(Equal(int64(2)))
	})

	It("should clear NAR-level Failed when all nodes are ConfigApplied after a spec revert", func() {
		nar := createNAR(string(hwmgmtv1alpha1.Failed), metav1.ConditionFalse)
		createNode("n1", string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)
		createNode("n2", string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue)

		result, err := reconciler.handleNodeAllocationRequestSpecChanged(ctx, nar)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		updated := &hwmgmtv1alpha1.NodeAllocationRequest{}
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(nar), updated)).To(Succeed())
		cond := meta.FindStatusCondition(updated.Status.Conditions, string(hwmgmtv1alpha1.Configured))
		Expect(cond).ToNot(BeNil())
		Expect(cond.Reason).To(Equal(string(hwmgmtv1alpha1.ConfigApplied)))
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		Expect(updated.Status.ObservedGeneration).To(Equal(int64(2)))
	})

	It("should ack a non-profile spec change without deriving from AllocatedNodes when Configured is not present", func() {
		nar := &hwmgmtv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-nar",
				Namespace:  "oran-o2ims",
				Generation: 2,
			},
			Spec: hwmgmtv1alpha1.NodeAllocationRequestSpec{
				ConfigTransactionId: 2,
				NodeGroup: []hwmgmtv1alpha1.NodeGroup{
					{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
						Name: "master", Role: hwmgmtv1alpha1.NodeRoleMaster, HwProfile: "profile-v1",
					}, Size: 1},
				},
			},
		}
		Expect(fakeClient.Create(ctx, nar)).To(Succeed())
		hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
			string(hwmgmtv1alpha1.Provisioned), string(hwmgmtv1alpha1.Completed),
			metav1.ConditionTrue, "Provisioned")
		nar.Status.ObservedGeneration = 1
		nar.Status.ObservedConfigTransactionId = 1
		Expect(fakeClient.Status().Update(ctx, nar)).To(Succeed())

		node := &hwmgmtv1alpha1.AllocatedNode{
			ObjectMeta: metav1.ObjectMeta{Name: "n1", Namespace: "oran-o2ims"},
			Spec: hwmgmtv1alpha1.AllocatedNodeSpec{
				GroupName:             "master",
				HwProfile:             "profile-v1",
				NodeAllocationRequest: "test-nar",
			},
		}
		Expect(fakeClient.Create(ctx, node)).To(Succeed())

		result, err := reconciler.handleNodeAllocationRequestSpecChanged(ctx, nar)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		updated := &hwmgmtv1alpha1.NodeAllocationRequest{}
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(nar), updated)).To(Succeed())
		Expect(meta.FindStatusCondition(updated.Status.Conditions, string(hwmgmtv1alpha1.Configured))).To(BeNil())
		Expect(updated.Status.ObservedGeneration).To(Equal(int64(2)))
		Expect(updated.Status.ObservedConfigTransactionId).To(Equal(int64(2)))
	})
})

var _ = Describe("handleHardwareProfileChanges", func() {
	const (
		narName   = "test-nar"
		namespace = "oran-o2ims"
	)

	var (
		ctx        context.Context
		logger     *slog.Logger
		reconciler *NodeAllocationRequestReconciler
	)

	BeforeEach(func() {
		ctx = context.Background()
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	})

	AfterEach(func() {
		spokeclient.ClearCache()
	})

	hubScheme := func() *runtime.Scheme {
		scheme := runtime.NewScheme()
		Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(provisioningv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(addonv1alpha1.Install(scheme)).To(Succeed())
		Expect(clusterv1.Install(scheme)).To(Succeed())
		Expect(workv1.Install(scheme)).To(Succeed())
		Expect(msav1beta1.AddToScheme(scheme)).To(Succeed())
		return scheme
	}

	spokeAccessReadyObjects := func() []client.Object {
		msaName := narName + hwConfigMSASuffix
		mwName := narName + hwConfigMWSuffix
		tokenName := msaName + "-token"
		return []client.Object{
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"}},
			&addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{Name: "managed-serviceaccount", Namespace: "test-cluster"},
			},
			&msav1beta1.ManagedServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: msaName, Namespace: "test-cluster"},
				Status: msav1beta1.ManagedServiceAccountStatus{
					TokenSecretRef: &msav1beta1.SecretRef{Name: tokenName},
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: tokenName, Namespace: "test-cluster", ResourceVersion: "100",
				},
				Data: map[string][]byte{"token": []byte("t"), "ca.crt": []byte("c")},
			},
			&workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{Name: mwName, Namespace: "test-cluster"},
				Status: workv1.ManifestWorkStatus{
					Conditions: []metav1.Condition{{
						Type:               workv1.WorkAvailable,
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.Now(),
					}},
				},
			},
			&clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"},
				Spec: clusterv1.ManagedClusterSpec{
					ManagedClusterClientConfigs: []clusterv1.ClientConfig{
						{URL: "https://api.test-cluster.example.com:6443"},
					},
				},
			},
		}
	}

	stubSpokeCreators := func() {
		restore := spokeclient.SetTestSpokeClientCreator(
			func(string, string, []byte, *runtime.Scheme) (client.Client, error) {
				return fake.NewClientBuilder().Build(), nil
			})
		DeferCleanup(restore)
		restoreCS := spokeclient.SetTestSpokeClientsetCreator(
			func(string, string, []byte) (kubernetes.Interface, error) {
				return kubefake.NewSimpleClientset(), nil
			})
		DeferCleanup(restoreCS)
	}

	newReconciler := func(scheme *runtime.Scheme, objs ...client.Object) *NodeAllocationRequestReconciler {
		c := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(objs...).
			WithStatusSubresource(
				&hwmgmtv1alpha1.NodeAllocationRequest{},
				&hwmgmtv1alpha1.AllocatedNode{},
				&provisioningv1alpha1.ProvisioningRequest{},
			).
			WithIndex(&hwmgmtv1alpha1.AllocatedNode{}, "spec.nodeAllocationRequest",
				func(obj client.Object) []string {
					return []string{obj.(*hwmgmtv1alpha1.AllocatedNode).Spec.NodeAllocationRequest}
				}).
			Build()
		return &NodeAllocationRequestReconciler{
			Client:          c,
			NoncachedClient: c,
			Logger:          logger,
			Namespace:       namespace,
		}
	}

	narAndNode := func(configuredReason string, configuredStatus metav1.ConditionStatus) (*hwmgmtv1alpha1.NodeAllocationRequest, *hwmgmtv1alpha1.AllocatedNode) {
		nar := &hwmgmtv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{Name: narName, Namespace: namespace, Generation: 2},
			Spec: hwmgmtv1alpha1.NodeAllocationRequestSpec{
				ClusterId: "test-cluster",
				NodeGroup: []hwmgmtv1alpha1.NodeGroup{
					{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
						Name: "master", Role: hwmgmtv1alpha1.NodeRoleMaster, HwProfile: "profile-v2",
					}, Size: 1},
				},
			},
		}
		hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
			string(hwmgmtv1alpha1.Configured), configuredReason, configuredStatus, "in progress")
		node := &hwmgmtv1alpha1.AllocatedNode{
			ObjectMeta: metav1.ObjectMeta{Name: "node-1", Namespace: namespace},
			Spec: hwmgmtv1alpha1.AllocatedNodeSpec{
				GroupName:             "master",
				HwProfile:             "profile-v1",
				NodeAllocationRequest: narName,
			},
		}
		hwmgrutils.SetStatusCondition(&node.Status.Conditions,
			string(hwmgmtv1alpha1.Configured), string(hwmgmtv1alpha1.ConfigApplied),
			metav1.ConditionTrue, "applied")
		return nar, node
	}

	prWithCluster := func() *provisioningv1alpha1.ProvisioningRequest {
		return &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{Name: narName},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				Name: "test", TemplateName: "test-template", TemplateVersion: "v1",
			},
			Status: provisioningv1alpha1.ProvisioningRequestStatus{
				Extensions: provisioningv1alpha1.Extensions{
					ClusterDetails: &provisioningv1alpha1.ClusterDetails{Name: "test-cluster"},
				},
			},
		}
	}

	It("should fail NAR when managed-serviceaccount addon is not available", func() {
		nar, node := narAndNode(string(hwmgmtv1alpha1.InProgress), metav1.ConditionFalse)
		reconciler = newReconciler(hubScheme(), nar, node, prWithCluster())

		_, err := reconciler.handleHardwareProfileChanges(ctx, nar)
		Expect(err).ToNot(HaveOccurred())

		updated := &hwmgmtv1alpha1.NodeAllocationRequest{}
		Expect(reconciler.Get(ctx, client.ObjectKeyFromObject(nar), updated)).To(Succeed())
		cond := meta.FindStatusCondition(updated.Status.Conditions, string(hwmgmtv1alpha1.Configured))
		Expect(cond).ToNot(BeNil())
		Expect(cond.Reason).To(Equal(string(hwmgmtv1alpha1.Failed)))
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Message).To(ContainSubstring("managed-serviceaccount addon is not available"))

		updatedNode := &hwmgmtv1alpha1.AllocatedNode{}
		Expect(reconciler.Get(ctx, client.ObjectKeyFromObject(node), updatedNode)).To(Succeed())
		Expect(updatedNode.Spec.HwProfile).To(Equal("profile-v1"))
	})

	It("should not aggregate while spoke clients are not ready", func() {
		nar, node := narAndNode(string(hwmgmtv1alpha1.InProgress), metav1.ConditionFalse)
		reconciler = newReconciler(hubScheme(), nar, node, prWithCluster(),
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"}},
			&addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{Name: "managed-serviceaccount", Namespace: "test-cluster"},
			},
		)

		result, err := reconciler.handleHardwareProfileChanges(ctx, nar)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(15 * time.Second))

		updated := &hwmgmtv1alpha1.NodeAllocationRequest{}
		Expect(reconciler.Get(ctx, client.ObjectKeyFromObject(nar), updated)).To(Succeed())
		cond := meta.FindStatusCondition(updated.Status.Conditions, string(hwmgmtv1alpha1.Configured))
		Expect(cond).ToNot(BeNil())
		Expect(cond.Reason).To(Equal(string(hwmgmtv1alpha1.InProgress)))
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))

		msa := &msav1beta1.ManagedServiceAccount{}
		Expect(reconciler.Get(ctx, client.ObjectKey{
			Name: narName + hwConfigMSASuffix, Namespace: "test-cluster",
		}, msa)).To(Succeed())
	})

	DescribeTable("should clean up hw-config spoke access after terminal configuration",
		func(nodeReason string, nodeStatus metav1.ConditionStatus) {
			stubSpokeCreators()

			nar, node := narAndNode(string(hwmgmtv1alpha1.InProgress), metav1.ConditionFalse)
			node.Spec.HwProfile = "profile-v2"
			node.Status.Hostname = "node-1"
			hwmgrutils.SetStatusCondition(&node.Status.Conditions,
				string(hwmgmtv1alpha1.Configured), nodeReason, nodeStatus, nodeReason)

			objs := append([]client.Object{nar, node, prWithCluster()}, spokeAccessReadyObjects()...)
			reconciler = newReconciler(hubScheme(), objs...)

			_, err := reconciler.handleHardwareProfileChanges(ctx, nar)
			Expect(err).ToNot(HaveOccurred())

			msa := &msav1beta1.ManagedServiceAccount{}
			err = reconciler.Get(ctx, client.ObjectKey{
				Name: narName + hwConfigMSASuffix, Namespace: "test-cluster",
			}, msa)
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())

			mw := &workv1.ManifestWork{}
			err = reconciler.Get(ctx, client.ObjectKey{
				Name: narName + hwConfigMWSuffix, Namespace: "test-cluster",
			}, mw)
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		},
		Entry("when configuration completed", string(hwmgmtv1alpha1.ConfigApplied), metav1.ConditionTrue),
		Entry("when configuration failed", string(hwmgmtv1alpha1.Failed), metav1.ConditionFalse),
	)
})
