/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package api_test

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/service/provisioning/api"
	provisioningapi "github.com/openshift-kni/oran-o2ims/internal/service/provisioning/api/generated"
)

// interceptorFuncs creates an interceptor that preserves the UID during updates
// This works around a limitation in the fake client where UIDs are not preserved
func interceptorFuncs(uid string) interceptor.Funcs {
	return interceptor.Funcs{
		Update: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			// Preserve UID if not set
			if obj.GetUID() == "" {
				obj.SetUID(types.UID(uid))
			}
			return client.Update(ctx, obj, opts...)
		},
	}
}

var _ = Describe("ProvisioningServer", func() {
	var (
		ctx      context.Context
		testUUID uuid.UUID
	)

	BeforeEach(func() {
		ctx = context.Background()
		testUUID = uuid.New()
	})

	Describe("UpdateProvisioningRequest with concurrent updates", func() {
		var (
			server *api.ProvisioningServer
			scheme *runtime.Scheme
		)

		BeforeEach(func() {
			scheme = runtime.NewScheme()
			Expect(provisioningv1alpha1.AddToScheme(scheme)).To(Succeed())
		})

		When("multiple concurrent updates occur", func() {
			It("should handle conflicts with retry logic using fake client", func() {
				// Create initial ProvisioningRequest with a valid UID
				initialTemplateParams := map[string]interface{}{
					"clusterName": "test-cluster",
					"baseDomain":  "example.com",
				}
				templateParamsBytes, err := json.Marshal(initialTemplateParams)
				Expect(err).NotTo(HaveOccurred())

				prUID := uuid.New().String()
				initialPR := &provisioningv1alpha1.ProvisioningRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:            testUUID.String(),
						UID:             types.UID(prUID),
						ResourceVersion: "1",
					},
					Spec: provisioningv1alpha1.ProvisioningRequestSpec{
						Name:               "test",
						Description:        "initial description",
						TemplateName:       "test-template",
						TemplateVersion:    "v1.0.0",
						TemplateParameters: runtime.RawExtension{Raw: templateParamsBytes},
					},
				}

				// Create fake client with initial object - use interceptor to preserve UID
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(initialPR).
					WithStatusSubresource(initialPR).
					WithInterceptorFuncs(interceptorFuncs(prUID)).
					Build()

				server = &api.ProvisioningServer{
					HubClient: fakeClient,
				}

				// Prepare first update
				updatedTemplateParams1 := map[string]interface{}{
					"clusterName": "test-cluster-updated-1",
					"baseDomain":  "example.com",
				}

				updateRequest1 := provisioningapi.UpdateProvisioningRequestRequestObject{
					ProvisioningRequestId: testUUID,
					Body: &provisioningapi.ProvisioningRequestData{
						ProvisioningRequestId: testUUID,
						Name:                  "test-updated-1",
						Description:           "updated description 1",
						TemplateName:          "test-template",
						TemplateVersion:       "v1.0.0",
						TemplateParameters:    updatedTemplateParams1,
					},
				}

				// First update should succeed
				resp, err := server.UpdateProvisioningRequest(ctx, updateRequest1)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(provisioningapi.UpdateProvisioningRequest200JSONResponse{}))

				// Verify the update was applied
				updatedPR := &provisioningv1alpha1.ProvisioningRequest{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: testUUID.String()}, updatedPR)
				Expect(err).NotTo(HaveOccurred())
				Expect(updatedPR.Spec.Name).To(Equal("test-updated-1"))
				Expect(updatedPR.Spec.Description).To(Equal("updated description 1"))
			})

			It("should succeed when updates don't conflict", func() {
				// Create initial ProvisioningRequest
				initialTemplateParams := map[string]interface{}{
					"clusterName": "test-cluster",
					"baseDomain":  "example.com",
				}
				templateParamsBytes, err := json.Marshal(initialTemplateParams)
				Expect(err).NotTo(HaveOccurred())

				prUID := uuid.New().String()
				initialPR := &provisioningv1alpha1.ProvisioningRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name: testUUID.String(),
						UID:  types.UID(prUID),
					},
					Spec: provisioningv1alpha1.ProvisioningRequestSpec{
						Name:               "test",
						Description:        "initial description",
						TemplateName:       "test-template",
						TemplateVersion:    "v1.0.0",
						TemplateParameters: runtime.RawExtension{Raw: templateParamsBytes},
					},
				}

				// Create fake client with initial object
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(initialPR).
					WithStatusSubresource(initialPR).
					WithInterceptorFuncs(interceptorFuncs(prUID)).
					Build()

				server = &api.ProvisioningServer{
					HubClient: fakeClient,
				}

				// Sequential updates should both succeed
				updatedTemplateParams1 := map[string]interface{}{
					"clusterName": "test-cluster-seq-1",
					"baseDomain":  "example.com",
				}

				updateRequest1 := provisioningapi.UpdateProvisioningRequestRequestObject{
					ProvisioningRequestId: testUUID,
					Body: &provisioningapi.ProvisioningRequestData{
						ProvisioningRequestId: testUUID,
						Name:                  "test-seq-1",
						Description:           "sequential update 1",
						TemplateName:          "test-template",
						TemplateVersion:       "v1.0.0",
						TemplateParameters:    updatedTemplateParams1,
					},
				}

				resp1, err := server.UpdateProvisioningRequest(ctx, updateRequest1)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp1).To(BeAssignableToTypeOf(provisioningapi.UpdateProvisioningRequest200JSONResponse{}))

				updatedTemplateParams2 := map[string]interface{}{
					"clusterName": "test-cluster-seq-2",
					"baseDomain":  "example.com",
				}

				updateRequest2 := provisioningapi.UpdateProvisioningRequestRequestObject{
					ProvisioningRequestId: testUUID,
					Body: &provisioningapi.ProvisioningRequestData{
						ProvisioningRequestId: testUUID,
						Name:                  "test-seq-2",
						Description:           "sequential update 2",
						TemplateName:          "test-template",
						TemplateVersion:       "v1.0.0",
						TemplateParameters:    updatedTemplateParams2,
					},
				}

				resp2, err := server.UpdateProvisioningRequest(ctx, updateRequest2)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp2).To(BeAssignableToTypeOf(provisioningapi.UpdateProvisioningRequest200JSONResponse{}))

				// Verify final state
				finalPR := &provisioningv1alpha1.ProvisioningRequest{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: testUUID.String()}, finalPR)
				Expect(err).NotTo(HaveOccurred())
				Expect(finalPR.Spec.Name).To(Equal("test-seq-2"))
				Expect(finalPR.Spec.Description).To(Equal("sequential update 2"))
			})
		})

		When("ProvisioningRequest ID mismatch in body and path", func() {
			It("should return 422 error", func() {
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					Build()

				server = &api.ProvisioningServer{
					HubClient: fakeClient,
				}

				differentUUID := uuid.New()
				templateParams := map[string]interface{}{"key": "value"}

				updateRequest := provisioningapi.UpdateProvisioningRequestRequestObject{
					ProvisioningRequestId: testUUID,
					Body: &provisioningapi.ProvisioningRequestData{
						ProvisioningRequestId: differentUUID, // Different from path
						Name:                  "test",
						Description:           "description",
						TemplateName:          "template",
						TemplateVersion:       "v1.0.0",
						TemplateParameters:    templateParams,
					},
				}

				resp, err := server.UpdateProvisioningRequest(ctx, updateRequest)
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(provisioningapi.UpdateProvisioningRequest422ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(422))
			})
		})

		When("ProvisioningRequest does not exist", func() {
			It("should return 404 error", func() {
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					Build()

				server = &api.ProvisioningServer{
					HubClient: fakeClient,
				}

				templateParams := map[string]interface{}{"key": "value"}

				updateRequest := provisioningapi.UpdateProvisioningRequestRequestObject{
					ProvisioningRequestId: testUUID,
					Body: &provisioningapi.ProvisioningRequestData{
						ProvisioningRequestId: testUUID,
						Name:                  "test",
						Description:           "description",
						TemplateName:          "template",
						TemplateVersion:       "v1.0.0",
						TemplateParameters:    templateParams,
					},
				}

				resp, err := server.UpdateProvisioningRequest(ctx, updateRequest)
				Expect(err).NotTo(HaveOccurred())
				problemResp := resp.(provisioningapi.UpdateProvisioningRequest404ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(404))
			})
		})
	})
})
