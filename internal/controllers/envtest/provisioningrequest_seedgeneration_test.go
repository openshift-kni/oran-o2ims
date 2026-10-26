/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package envtest

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Seed generation status persistence", Label("envtest"), func() {
	It("should preserve identity and recovery checkpoints through the generated CRD", func() {
		pr := &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{Name: "seed-status-persistence"},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName: "seed-template", TemplateVersion: "v1",
				TemplateParameters: runtime.RawExtension{Raw: []byte(`{}`)},
			},
		}
		Expect(k8sClient.Create(ctx, pr)).ToNot(HaveOccurred())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, pr)).ToNot(HaveOccurred()) })
		started := metav1.NewTime(time.Now().UTC().Truncate(time.Second))
		expected := &provisioningv1alpha1.SeedGenerationStatus{
			StartedAt: &started, TimeoutSeconds: 7200,
			ClusterID: "cluster-id", ManagedClusterUID: "managed-cluster-uid", KlusterletUID: "klusterlet-uid",
			DetachmentStarted: true, AgentDetachmentCompleted: true,
			SeedGeneratorSubmitted: true, SeedGeneratorFinished: true, DetachedCleanupCompleted: true,
			SeedImage: "quay.io/example/seed:4.22", ReleaseImageDigest: "quay.io/example/release@sha256:digest",
			ISOURL: "https://example.test/seed.iso", ISODigest: "sha256:digest",
			ISOJobSubmitted: true, ISOJobSubmittedAt: &started, ISOJobUID: "iso-job-uid",
			ISOUploadCleanupDeadline: &started, ISOUploadCleanupCompleted: true,
			ISOFailureDiagnosticsExpiresAt: &started,
			ISOFailureDiagnosticsRef:       &provisioningv1alpha1.ConfigMapKeyRef{Name: "diagnostics", Namespace: "oran-o2ims", Key: "build.log"},
			ISOServerCACertRef:             &provisioningv1alpha1.ConfigMapKeyRef{Name: "iso-ca", Namespace: "oran-o2ims", Key: "ca.crt"},
			InputSnapshotResourceUIDs:      map[string]string{"config": "config-uid", "spokeKubeconfig": "kubeconfig-uid"},
		}
		pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{
			Name: "seed-cluster", SeedGenerationStatus: expected.DeepCopy(),
		}
		Expect(k8sClient.Status().Update(ctx, pr)).ToNot(HaveOccurred())
		stored := &provisioningv1alpha1.ProvisioningRequest{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pr), stored)).ToNot(HaveOccurred())
		Expect(stored.Status.Extensions.ClusterDetails).ToNot(BeNil())
		actual := stored.Status.Extensions.ClusterDetails.SeedGenerationStatus
		Expect(equality.Semantic.DeepEqual(actual, expected)).To(BeTrue(), "stored status: %#v", actual)
	})
})
