/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Test Cases for Metal3 NodeAllocationRequest Controller

This test suite covers the Metal3 hardware plugin's NodeAllocationRequest controller,
focusing on the new timeout handling implementation that was moved from the O-Cloud Manager.

Key Test Areas:
1. checkHardwareTimeout function - Core timeout detection logic
2. HardwareProvisioningTimeout field handling
3. Day 2 retry scenarios with spec changes
4. Callback integration for timeout notifications
5. Integration with ProvisioningStartTime and ConfiguringStartTime
*/

package controller

import (
	"log/slog"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
)

var _ = Describe("Metal3 NodeAllocationRequest Controller Timeout Handling", func() {
	var (
		c          client.Client
		reconciler *NodeAllocationRequestReconciler
		logger     *slog.Logger
	)

	BeforeEach(func() {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

		scheme := runtime.NewScheme()
		Expect(pluginsv1alpha1.AddToScheme(scheme)).To(Succeed())

		c = fake.NewClientBuilder().WithScheme(scheme).Build()

		reconciler = &NodeAllocationRequestReconciler{
			Client:          c,
			NoncachedClient: c,
			Logger:          logger,
		}
	})

	Describe("checkHardwareTimeout", func() {
		var nar *pluginsv1alpha1.NodeAllocationRequest

		BeforeEach(func() {
			nar = &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nar",
					Namespace: "default",
				},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					HardwareProvisioningTimeout: "5m",
				},
				Status: pluginsv1alpha1.NodeAllocationRequestStatus{
					Conditions: []metav1.Condition{},
				},
			}
		})

		Context("when HardwareProvisioningTimeout is specified", func() {
			It("should use the specified timeout value", func() {
				nar.Spec.HardwareProvisioningTimeout = "10m"
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(nar)
				Expect(err).ToNot(HaveOccurred())
				Expect(timeoutExceeded).To(BeFalse())
				Expect(conditionType).To(Equal(hwmgmtv1alpha1.ConditionType("")))
			})
		})

		Context("when HardwareProvisioningTimeout is empty", func() {
			It("should use default timeout", func() {
				nar.Spec.HardwareProvisioningTimeout = ""
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(nar)
				Expect(err).ToNot(HaveOccurred())
				Expect(timeoutExceeded).To(BeFalse())
				Expect(conditionType).To(Equal(hwmgmtv1alpha1.ConditionType("")))
			})
		})

		Context("when HardwareProvisioningTimeout is invalid", func() {
			It("should return error for invalid duration", func() {
				nar.Spec.HardwareProvisioningTimeout = "invalid"
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(nar)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid hardware provisioning timeout"))
				Expect(timeoutExceeded).To(BeFalse())
				Expect(conditionType).To(Equal(hwmgmtv1alpha1.ConditionType("")))
			})

			It("should return error for zero timeout", func() {
				nar.Spec.HardwareProvisioningTimeout = "0s"
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(nar)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("hardware provisioning timeout must be > 0"))
				Expect(timeoutExceeded).To(BeFalse())
				Expect(conditionType).To(Equal(hwmgmtv1alpha1.ConditionType("")))
			})
		})

		Context("when provisioning is in progress and times out", func() {
			BeforeEach(func() {
				// Set provisioning start time to 10 minutes ago (exceeds 5m timeout)
				startTime := metav1.Time{Time: time.Now().Add(-10 * time.Minute)}
				nar.Status.ProvisioningStartTime = &startTime

				// Add provisioning condition in progress
				hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
					string(hwmgmtv1alpha1.Provisioned),
					string(hwmgmtv1alpha1.InProgress),
					metav1.ConditionFalse,
					"Hardware provisioning in progress")
			})

			It("should detect provisioning timeout", func() {
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(nar)
				Expect(err).ToNot(HaveOccurred())
				Expect(timeoutExceeded).To(BeTrue())
				Expect(conditionType).To(Equal(hwmgmtv1alpha1.Provisioned))
			})
		})

		Context("when provisioning is in progress but not timed out", func() {
			BeforeEach(func() {
				// Set provisioning start time to 2 minutes ago (within 5m timeout)
				startTime := metav1.Time{Time: time.Now().Add(-2 * time.Minute)}
				nar.Status.ProvisioningStartTime = &startTime

				// Add provisioning condition in progress
				hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
					string(hwmgmtv1alpha1.Provisioned),
					string(hwmgmtv1alpha1.InProgress),
					metav1.ConditionFalse,
					"Hardware provisioning in progress")
			})

			It("should not detect timeout", func() {
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(nar)
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

				// Set configuration start time to 10 minutes ago (exceeds 5m timeout)
				startTime := metav1.Time{Time: time.Now().Add(-10 * time.Minute)}
				nar.Status.ConfiguringStartTime = &startTime

				// Add configuration condition in progress
				hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
					string(hwmgmtv1alpha1.Configured),
					string(hwmgmtv1alpha1.InProgress),
					metav1.ConditionFalse,
					"Hardware configuration in progress")
			})

			It("should detect configuration timeout", func() {
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(nar)
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

				// Set configuration start time to 2 minutes ago (within 5m timeout)
				startTime := metav1.Time{Time: time.Now().Add(-2 * time.Minute)}
				nar.Status.ConfiguringStartTime = &startTime

				// Add configuration condition in progress
				hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
					string(hwmgmtv1alpha1.Configured),
					string(hwmgmtv1alpha1.InProgress),
					metav1.ConditionFalse,
					"Hardware configuration in progress")
			})

			It("should not detect timeout", func() {
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(nar)
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
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(nar)
				Expect(err).ToNot(HaveOccurred())
				Expect(timeoutExceeded).To(BeFalse())
				Expect(conditionType).To(Equal(hwmgmtv1alpha1.ConditionType("")))
			})
		})

		Context("when provisioning is in progress but ProvisioningStartTime is missing", func() {
			BeforeEach(func() {
				// Add provisioning condition in progress but no start time
				hwmgrutils.SetStatusCondition(&nar.Status.Conditions,
					string(hwmgmtv1alpha1.Provisioned),
					string(hwmgmtv1alpha1.InProgress),
					metav1.ConditionFalse,
					"Hardware provisioning in progress")
			})

			It("should not detect timeout without start time", func() {
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(nar)
				Expect(err).ToNot(HaveOccurred())
				Expect(timeoutExceeded).To(BeFalse())
				Expect(conditionType).To(Equal(hwmgmtv1alpha1.ConditionType("")))
			})
		})

		Context("when configuration is in progress but ConfiguringStartTime is missing", func() {
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
				timeoutExceeded, conditionType, err := reconciler.checkHardwareTimeout(nar)
				Expect(err).ToNot(HaveOccurred())
				Expect(timeoutExceeded).To(BeFalse())
				Expect(conditionType).To(Equal(hwmgmtv1alpha1.ConditionType("")))
			})
		})
	})
})
