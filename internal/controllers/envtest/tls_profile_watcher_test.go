/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package envtest

import (
	"crypto/tls"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

var _ = Describe("TLS Profile Watcher Integration", Label("envtest"), func() {
	Context("when the APIServer CRD is installed", func() {
		BeforeEach(func() {
			existing := &configv1.APIServer{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, existing); err == nil {
				Expect(k8sClient.Delete(ctx, existing)).To(Succeed())
			}
		})

		It("should fetch the default Intermediate profile when no APIServer exists", func() {
			profile, err := ctlrutils.FetchAPIServerTLSProfile(ctx, k8sClient)
			if err != nil {
				errMsg := err.Error()
				if strings.Contains(errMsg, "no matches for kind") ||
					strings.Contains(errMsg, "not found") {
					Skip("APIServer resource not available: " + errMsg)
				}
			}
			Expect(err).ToNot(HaveOccurred())
			Expect(profile.MinTLSVersion).To(Equal(configv1.VersionTLS12))
		})

		It("should fetch profile from a created APIServer resource", func() {
			apiServer := &configv1.APIServer{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: configv1.APIServerSpec{
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: configv1.TLSProfileModernType,
					},
				},
			}

			Expect(k8sClient.Create(ctx, apiServer)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, apiServer)
			}()

			profile, err := ctlrutils.FetchAPIServerTLSProfile(ctx, k8sClient)
			Expect(err).ToNot(HaveOccurred())
			Expect(profile.MinTLSVersion).To(Equal(configv1.VersionTLS13))
		})

		It("should fetch a Custom profile with explicit ciphers and minTLSVersion", func() {
			apiServer := &configv1.APIServer{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: configv1.APIServerSpec{
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: configv1.TLSProfileCustomType,
						Custom: &configv1.CustomTLSProfile{
							TLSProfileSpec: configv1.TLSProfileSpec{
								MinTLSVersion: configv1.VersionTLS13,
								Ciphers:       []string{"TLS_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384"},
							},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, apiServer)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, apiServer)
			}()

			profile, err := ctlrutils.FetchAPIServerTLSProfile(ctx, k8sClient)
			Expect(err).ToNot(HaveOccurred())
			Expect(profile.MinTLSVersion).To(Equal(configv1.VersionTLS13))
			Expect(profile.Ciphers).To(Equal([]string{"TLS_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384"}))
		})

		It("should produce a valid tls.Config from the fetched profile", func() {
			profile := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
			configurator := ctlrutils.NewTLSConfiguratorFromProfile(profile)

			cfg := &tls.Config{}
			configurator(cfg)

			Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
			Expect(cfg.CipherSuites).ToNot(BeEmpty())
		})

		It("should generate distinct hashes for different profiles", func() {
			intermediate := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
			modern := *configv1.TLSProfiles[configv1.TLSProfileModernType]

			hashIntermediate := ctlrutils.TLSProfileHash(intermediate)
			hashModern := ctlrutils.TLSProfileHash(modern)

			Expect(hashIntermediate).ToNot(Equal(hashModern))
			Expect(hashIntermediate).To(HaveLen(16))
			Expect(hashModern).To(HaveLen(16))
		})

		It("should update profile when APIServer is modified", func() {
			apiServer := &configv1.APIServer{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: configv1.APIServerSpec{
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: configv1.TLSProfileIntermediateType,
					},
				},
			}

			Expect(k8sClient.Create(ctx, apiServer)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, apiServer)
			}()

			profile1, err := ctlrutils.FetchAPIServerTLSProfile(ctx, k8sClient)
			Expect(err).ToNot(HaveOccurred())
			Expect(profile1.MinTLSVersion).To(Equal(configv1.VersionTLS12))

			// Update to Modern
			apiServer.Spec.TLSSecurityProfile = &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileModernType,
			}
			Expect(k8sClient.Update(ctx, apiServer)).To(Succeed())

			profile2, err := ctlrutils.FetchAPIServerTLSProfile(ctx, k8sClient)
			Expect(err).ToNot(HaveOccurred())
			Expect(profile2.MinTLSVersion).To(Equal(configv1.VersionTLS13))
		})
	})
})
