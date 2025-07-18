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
	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("HostUpdatePolicy Manager", func() {
	var (
		ctx        context.Context
		logger     *slog.Logger
		scheme     *runtime.Scheme
		fakeClient client.Client
		bmh        *metal3v1alpha1.BareMetalHost
	)

	BeforeEach(func() {
		ctx = context.Background()
		logger = slog.Default()
		scheme = runtime.NewScheme()
		Expect(metal3v1alpha1.AddToScheme(scheme)).To(Succeed())

		// Create a test BareMetalHost
		bmh = &metal3v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bmh",
				Namespace: "test-namespace",
			},
		}

		fakeClient = fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(bmh).
			Build()
	})

	Describe("createOrUpdateHostUpdatePolicy", func() {
		Context("when HostUpdatePolicy does not exist", func() {
			It("should create a new HostUpdatePolicy with firmware updates only", func() {
				err := createOrUpdateHostUpdatePolicy(ctx, fakeClient, logger, bmh, true, false)
				Expect(err).NotTo(HaveOccurred())

				// Verify the policy was created
				policy := &metal3v1alpha1.HostUpdatePolicy{}
				key := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
				err = fakeClient.Get(ctx, key, policy)
				Expect(err).NotTo(HaveOccurred())
				Expect(policy.Name).To(Equal(bmh.Name))
				Expect(policy.Namespace).To(Equal(bmh.Namespace))
				Expect(policy.Spec.FirmwareUpdates).To(Equal(metal3v1alpha1.HostUpdatePolicyOnReboot))
				Expect(policy.Spec.FirmwareSettings).To(BeEmpty())
			})

			It("should create a new HostUpdatePolicy with BIOS updates only", func() {
				err := createOrUpdateHostUpdatePolicy(ctx, fakeClient, logger, bmh, false, true)
				Expect(err).NotTo(HaveOccurred())

				// Verify the policy was created
				policy := &metal3v1alpha1.HostUpdatePolicy{}
				key := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
				err = fakeClient.Get(ctx, key, policy)
				Expect(err).NotTo(HaveOccurred())
				Expect(policy.Name).To(Equal(bmh.Name))
				Expect(policy.Namespace).To(Equal(bmh.Namespace))
				Expect(policy.Spec.FirmwareUpdates).To(BeEmpty())
				Expect(policy.Spec.FirmwareSettings).To(Equal(metal3v1alpha1.HostUpdatePolicyOnReboot))
			})

			It("should create a new HostUpdatePolicy with both firmware and BIOS updates", func() {
				err := createOrUpdateHostUpdatePolicy(ctx, fakeClient, logger, bmh, true, true)
				Expect(err).NotTo(HaveOccurred())

				// Verify the policy was created
				policy := &metal3v1alpha1.HostUpdatePolicy{}
				key := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
				err = fakeClient.Get(ctx, key, policy)
				Expect(err).NotTo(HaveOccurred())
				Expect(policy.Name).To(Equal(bmh.Name))
				Expect(policy.Namespace).To(Equal(bmh.Namespace))
				Expect(policy.Spec.FirmwareUpdates).To(Equal(metal3v1alpha1.HostUpdatePolicyOnReboot))
				Expect(policy.Spec.FirmwareSettings).To(Equal(metal3v1alpha1.HostUpdatePolicyOnReboot))
			})

			It("should create a new HostUpdatePolicy with empty spec when no updates required", func() {
				err := createOrUpdateHostUpdatePolicy(ctx, fakeClient, logger, bmh, false, false)
				Expect(err).NotTo(HaveOccurred())

				// Verify the policy was created
				policy := &metal3v1alpha1.HostUpdatePolicy{}
				key := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
				err = fakeClient.Get(ctx, key, policy)
				Expect(err).NotTo(HaveOccurred())
				Expect(policy.Name).To(Equal(bmh.Name))
				Expect(policy.Namespace).To(Equal(bmh.Namespace))
				Expect(policy.Spec.FirmwareUpdates).To(BeEmpty())
				Expect(policy.Spec.FirmwareSettings).To(BeEmpty())
			})
		})

		Context("when HostUpdatePolicy already exists", func() {
			var existingPolicy *metal3v1alpha1.HostUpdatePolicy

			BeforeEach(func() {
				// Create an existing HostUpdatePolicy
				existingPolicy = &metal3v1alpha1.HostUpdatePolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      bmh.Name,
						Namespace: bmh.Namespace,
					},
					Spec: metal3v1alpha1.HostUpdatePolicySpec{
						FirmwareUpdates: metal3v1alpha1.HostUpdatePolicyOnReboot,
					},
				}
				Expect(fakeClient.Create(ctx, existingPolicy)).To(Succeed())
			})

			It("should update the policy when firmware updates requirement changes", func() {
				err := createOrUpdateHostUpdatePolicy(ctx, fakeClient, logger, bmh, false, true)
				Expect(err).NotTo(HaveOccurred())

				// Verify the policy was updated
				policy := &metal3v1alpha1.HostUpdatePolicy{}
				key := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
				err = fakeClient.Get(ctx, key, policy)
				Expect(err).NotTo(HaveOccurred())
				Expect(policy.Spec.FirmwareUpdates).To(BeEmpty())
				Expect(policy.Spec.FirmwareSettings).To(Equal(metal3v1alpha1.HostUpdatePolicyOnReboot))
			})

			It("should update the policy when BIOS updates requirement is added", func() {
				err := createOrUpdateHostUpdatePolicy(ctx, fakeClient, logger, bmh, true, true)
				Expect(err).NotTo(HaveOccurred())

				// Verify the policy was updated
				policy := &metal3v1alpha1.HostUpdatePolicy{}
				key := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
				err = fakeClient.Get(ctx, key, policy)
				Expect(err).NotTo(HaveOccurred())
				Expect(policy.Spec.FirmwareUpdates).To(Equal(metal3v1alpha1.HostUpdatePolicyOnReboot))
				Expect(policy.Spec.FirmwareSettings).To(Equal(metal3v1alpha1.HostUpdatePolicyOnReboot))
			})

			It("should not update the policy when spec is already correct", func() {
				// Policy already has firmware updates = "onReboot", call with same requirements
				err := createOrUpdateHostUpdatePolicy(ctx, fakeClient, logger, bmh, true, false)
				Expect(err).NotTo(HaveOccurred())

				// Verify the policy was not changed
				policy := &metal3v1alpha1.HostUpdatePolicy{}
				key := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
				err = fakeClient.Get(ctx, key, policy)
				Expect(err).NotTo(HaveOccurred())
				Expect(policy.Spec.FirmwareUpdates).To(Equal(metal3v1alpha1.HostUpdatePolicyOnReboot))
				Expect(policy.Spec.FirmwareSettings).To(BeEmpty())
			})

			It("should clear both settings when no updates are required", func() {
				err := createOrUpdateHostUpdatePolicy(ctx, fakeClient, logger, bmh, false, false)
				Expect(err).NotTo(HaveOccurred())

				// Verify the policy was updated to have empty spec
				policy := &metal3v1alpha1.HostUpdatePolicy{}
				key := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
				err = fakeClient.Get(ctx, key, policy)
				Expect(err).NotTo(HaveOccurred())
				Expect(policy.Spec.FirmwareUpdates).To(BeEmpty())
				Expect(policy.Spec.FirmwareSettings).To(BeEmpty())
			})
		})

		Context("error handling", func() {
			It("should return error when BareMetalHost is nil", func() {
				err := createOrUpdateHostUpdatePolicy(ctx, fakeClient, logger, nil, true, false)
				Expect(err).To(HaveOccurred())
			})

			It("should return error when context is nil", func() {
				err := createOrUpdateHostUpdatePolicy(nil, fakeClient, logger, bmh, true, false)
				Expect(err).To(HaveOccurred())
			})

			It("should return error when client is nil", func() {
				err := createOrUpdateHostUpdatePolicy(ctx, nil, logger, bmh, true, false)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("edge cases with BareMetalHost names and namespaces", func() {
			It("should handle BareMetalHost with different name and namespace", func() {
				differentBmh := &metal3v1alpha1.BareMetalHost{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "different-bmh",
						Namespace: "different-namespace",
					},
				}

				err := createOrUpdateHostUpdatePolicy(ctx, fakeClient, logger, differentBmh, true, true)
				Expect(err).NotTo(HaveOccurred())

				// Verify the policy was created with correct name and namespace
				policy := &metal3v1alpha1.HostUpdatePolicy{}
				key := types.NamespacedName{Name: differentBmh.Name, Namespace: differentBmh.Namespace}
				err = fakeClient.Get(ctx, key, policy)
				Expect(err).NotTo(HaveOccurred())
				Expect(policy.Name).To(Equal(differentBmh.Name))
				Expect(policy.Namespace).To(Equal(differentBmh.Namespace))
				Expect(policy.Spec.FirmwareUpdates).To(Equal(metal3v1alpha1.HostUpdatePolicyOnReboot))
				Expect(policy.Spec.FirmwareSettings).To(Equal(metal3v1alpha1.HostUpdatePolicyOnReboot))
			})

			It("should handle BareMetalHost with special characters in name", func() {
				specialBmh := &metal3v1alpha1.BareMetalHost{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "bmh-with-special-chars-123",
						Namespace: "test-namespace",
					},
				}

				err := createOrUpdateHostUpdatePolicy(ctx, fakeClient, logger, specialBmh, false, true)
				Expect(err).NotTo(HaveOccurred())

				// Verify the policy was created with the special name
				policy := &metal3v1alpha1.HostUpdatePolicy{}
				key := types.NamespacedName{Name: specialBmh.Name, Namespace: specialBmh.Namespace}
				err = fakeClient.Get(ctx, key, policy)
				Expect(err).NotTo(HaveOccurred())
				Expect(policy.Name).To(Equal(specialBmh.Name))
				Expect(policy.Spec.FirmwareSettings).To(Equal(metal3v1alpha1.HostUpdatePolicyOnReboot))
			})
		})

		Context("spec validation", func() {
			It("should set correct values for firmware updates", func() {
				err := createOrUpdateHostUpdatePolicy(ctx, fakeClient, logger, bmh, true, false)
				Expect(err).NotTo(HaveOccurred())

				policy := &metal3v1alpha1.HostUpdatePolicy{}
				key := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
				err = fakeClient.Get(ctx, key, policy)
				Expect(err).NotTo(HaveOccurred())
				
				// Verify that the firmware updates field is set to exactly HostUpdatePolicyOnReboot
				Expect(policy.Spec.FirmwareUpdates).To(Equal(metal3v1alpha1.HostUpdatePolicyOnReboot))
				Expect(string(policy.Spec.FirmwareUpdates)).To(Equal("onReboot"))
				Expect(string(policy.Spec.FirmwareUpdates)).NotTo(Equal("OnReboot"))
				Expect(string(policy.Spec.FirmwareUpdates)).NotTo(Equal("onreboot"))
			})

			It("should set correct values for BIOS updates", func() {
				err := createOrUpdateHostUpdatePolicy(ctx, fakeClient, logger, bmh, false, true)
				Expect(err).NotTo(HaveOccurred())

				policy := &metal3v1alpha1.HostUpdatePolicy{}
				key := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
				err = fakeClient.Get(ctx, key, policy)
				Expect(err).NotTo(HaveOccurred())
				
				// Verify that the firmware settings field is set to exactly HostUpdatePolicyOnReboot
				Expect(policy.Spec.FirmwareSettings).To(Equal(metal3v1alpha1.HostUpdatePolicyOnReboot))
				Expect(string(policy.Spec.FirmwareSettings)).To(Equal("onReboot"))
				Expect(string(policy.Spec.FirmwareSettings)).NotTo(Equal("OnReboot"))
				Expect(string(policy.Spec.FirmwareSettings)).NotTo(Equal("onreboot"))
			})
		})
	})
}) 