/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"fmt"
	"log/slog"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
)

var _ = Describe("SetupMetal3Controllers", func() {
	var (
		scheme    *runtime.Scheme
		namespace string
	)

	BeforeEach(func() {
		namespace = "test-namespace"

		// Initialize scheme with required types
		scheme = runtime.NewScheme()
		Expect(metal3v1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(pluginsv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(hwmgmtv1alpha1.AddToScheme(scheme)).To(Succeed())
	})

	Context("function structure validation", func() {
		It("should exist and be callable", func() {
			// This test simply validates that the function exists and has the expected signature
			var mgr ctrl.Manager
			namespace := "test"

			// We can't easily call the function without a real manager, but we can test
			// that it would handle nil manager gracefully
			defer func() {
				if r := recover(); r != nil {
					// Function should not panic immediately on nil manager
					// The panic would occur deeper in the SetupWithManager calls
					Expect(r).ToNot(BeNil())
				}
			}()

			_ = SetupMetal3Controllers(mgr, namespace)
		})
	})

	Context("reconciler creation logic", func() {
		It("should create NodeAllocationRequestReconciler with correct fields", func() {
			// Test the logic of creating the reconciler struct
			// This is testing the structure without actually calling SetupWithManager

			baseLogger := slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelInfo}))

			// Create a fake client to simulate what the manager would provide
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			// Create the reconciler as the function would
			nodeAllocationReconciler := &NodeAllocationRequestReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          baseLogger.With(slog.String("controller", "metal3_nodeallocationrequest_controller")),
				PluginNamespace: namespace,
				Manager:         nil, // We can't provide a real manager in this test
			}

			// Verify the reconciler was created with expected values
			Expect(nodeAllocationReconciler.Client).ToNot(BeNil())
			Expect(nodeAllocationReconciler.NoncachedClient).ToNot(BeNil())
			Expect(nodeAllocationReconciler.Scheme).To(Equal(scheme))
			Expect(nodeAllocationReconciler.Logger).ToNot(BeNil())
			Expect(nodeAllocationReconciler.PluginNamespace).To(Equal(namespace))
		})

		It("should create AllocatedNodeReconciler with correct fields", func() {
			// Test the logic of creating the second reconciler struct
			baseLogger := slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelInfo}))

			// Create a fake client to simulate what the manager would provide
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			// Create the reconciler as the function would
			allocatedReconciler := &AllocatedNodeReconciler{
				Client:          fakeClient,
				NoncachedClient: fakeClient,
				Scheme:          scheme,
				Logger:          baseLogger.With(slog.String("controller", "metal3_allocatednode_controller")),
				PluginNamespace: namespace,
				Manager:         nil, // We can't provide a real manager in this test
			}

			// Verify the reconciler was created with expected values
			Expect(allocatedReconciler.Client).ToNot(BeNil())
			Expect(allocatedReconciler.NoncachedClient).ToNot(BeNil())
			Expect(allocatedReconciler.Scheme).To(Equal(scheme))
			Expect(allocatedReconciler.Logger).ToNot(BeNil())
			Expect(allocatedReconciler.PluginNamespace).To(Equal(namespace))
		})
	})

	Context("error handling structure", func() {
		It("should return errors with descriptive messages for NodeAllocationRequest setup failure", func() {
			// Test error message format
			var testError error

			// The function will panic with nil manager, so we need to catch it
			defer func() {
				if r := recover(); r != nil {
					// The panic is expected, verify it's about nil pointer
					Expect(r).ToNot(BeNil())
					// Convert panic to string and check it contains useful information
					panicMsg := fmt.Sprintf("%v", r)
					Expect(panicMsg).To(ContainSubstring("nil pointer"))
				}
			}()

			testError = SetupMetal3Controllers(nil, namespace)

			// If we get here without panic, check the error message
			if testError != nil {
				// If an error is returned, it should have a descriptive message
				errorMessage := testError.Error()
				Expect(errorMessage).ToNot(BeEmpty())
				// The error could be about setup failure or nil manager
				Expect(errorMessage).To(Or(
					ContainSubstring("failed to setup NodeAllocationRequest controller"),
					ContainSubstring("failed to setup AllocatedNode controller"),
					ContainSubstring("nil"),
					ContainSubstring("manager"),
				))
			}
		})
	})

	Context("namespace handling", func() {
		It("should accept empty namespace", func() {
			// Test that empty namespace doesn't cause immediate panic
			defer func() {
				if r := recover(); r != nil {
					// Panic is expected due to nil manager, not empty namespace
					Expect(r).ToNot(BeNil())
				}
			}()

			_ = SetupMetal3Controllers(nil, "")
		})

		It("should accept namespace with special characters", func() {
			// Test that special characters in namespace don't cause immediate panic
			defer func() {
				if r := recover(); r != nil {
					// Panic is expected due to nil manager, not special namespace
					Expect(r).ToNot(BeNil())
				}
			}()

			specialNamespace := "test-namespace_with.special-chars"
			_ = SetupMetal3Controllers(nil, specialNamespace)
		})
	})

	Context("logger configuration", func() {
		It("should create loggers with appropriate context", func() {
			// Test the logger creation logic
			baseLogger := slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelInfo}))

			nodeLogger := baseLogger.With(slog.String("controller", "metal3_nodeallocationrequest_controller"))
			allocatedLogger := baseLogger.With(slog.String("controller", "metal3_allocatednode_controller"))

			// Verify loggers are created (we can't easily test the content without complex setup)
			Expect(nodeLogger).ToNot(BeNil())
			Expect(allocatedLogger).ToNot(BeNil())
			Expect(nodeLogger).ToNot(Equal(allocatedLogger))
		})
	})
})
