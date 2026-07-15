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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/internal/hardwaremanager/utils"
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
