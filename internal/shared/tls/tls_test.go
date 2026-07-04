/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package tls

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
)

// generateSelfSignedCert creates a self-signed cert/key pair written to disk, returning the file paths.
func generateSelfSignedCert(dir, suffix string) (certPath, keyPath string) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	certPath = filepath.Join(dir, "tls-"+suffix+".crt")
	keyPath = filepath.Join(dir, "tls-"+suffix+".key")
	ExpectWithOffset(1, os.WriteFile(certPath, certPEM, 0o600)).To(Succeed())
	ExpectWithOffset(1, os.WriteFile(keyPath, keyPEM, 0o600)).To(Succeed())
	return certPath, keyPath
}

var _ = Describe("TLS Profile", func() {

	Describe("NewTLSConfiguratorFromProfile", func() {
		It("should configure Intermediate profile with TLS 1.2 and cipher suites", func() {
			profile := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
			configurator := NewTLSConfiguratorFromProfile(profile)

			cfg := &tls.Config{}
			configurator(cfg)

			Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
			Expect(cfg.CipherSuites).ToNot(BeEmpty())
		})

		It("should configure Modern profile with TLS 1.3 and no explicit cipher suites", func() {
			profile := *configv1.TLSProfiles[configv1.TLSProfileModernType]
			configurator := NewTLSConfiguratorFromProfile(profile)

			cfg := &tls.Config{}
			configurator(cfg)

			Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS13)))
			// Go manages TLS 1.3 ciphers internally; CipherSuites should not be set
			Expect(cfg.CipherSuites).To(BeEmpty())
		})

		It("should configure Old profile with TLS 1.0", func() {
			profile := *configv1.TLSProfiles[configv1.TLSProfileOldType]
			configurator := NewTLSConfiguratorFromProfile(profile)

			cfg := &tls.Config{}
			configurator(cfg)

			Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS10)))
			Expect(cfg.CipherSuites).ToNot(BeEmpty())
		})

		It("should handle custom profile with specific ciphers", func() {
			profile := configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS12,
				Ciphers: []string{
					"TLS_AES_128_GCM_SHA256",
					"TLS_AES_256_GCM_SHA384",
				},
			}
			configurator := NewTLSConfiguratorFromProfile(profile)

			cfg := &tls.Config{}
			configurator(cfg)

			Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
		})

		It("should not overwrite existing config fields unrelated to TLS profile", func() {
			profile := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
			configurator := NewTLSConfiguratorFromProfile(profile)

			cfg := &tls.Config{
				ServerName: "test.example.com",
			}
			configurator(cfg)

			Expect(cfg.ServerName).To(Equal("test.example.com"))
			Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
		})

		It("should skip unsupported ciphers and still return a working configurator", func() {
			profile := configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS12,
				Ciphers: []string{
					"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
					"BOGUS_CIPHER_THAT_DOES_NOT_EXIST",
				},
			}
			configurator := NewTLSConfiguratorFromProfile(profile)
			Expect(configurator).ToNot(BeNil())

			cfg := &tls.Config{}
			configurator(cfg)

			Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
			// Only the valid cipher should be configured; the bogus one is skipped
			Expect(cfg.CipherSuites).To(HaveLen(1))
		})
	})

	Describe("NewOutboundTLSConfig", func() {
		It("should create a config with profile settings", func() {
			profile := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
			cfg, err := NewOutboundTLSConfig(profile, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg).ToNot(BeNil())
			Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
		})

		It("should apply Modern profile MinVersion", func() {
			profile := *configv1.TLSProfiles[configv1.TLSProfileModernType]
			cfg, err := NewOutboundTLSConfig(profile, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS13)))
		})

		It("should not set RootCAs", func() {
			profile := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
			cfg, err := NewOutboundTLSConfig(profile, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.RootCAs).To(BeNil())
		})
	})

	Describe("newTLSProfileFromEnv", func() {
		AfterEach(func() {
			os.Unsetenv(TLSProfileMinVersionEnvName)
			os.Unsetenv(TLSProfileCiphersEnvName)
		})

		It("should fall back to Intermediate when env vars are not set", func() {
			profile := newTLSProfileFromEnv()
			Expect(profile.MinTLSVersion).To(Equal(configv1.VersionTLS12))
			Expect(profile.Ciphers).ToNot(BeEmpty())
		})

		It("should parse min version and ciphers from env vars", func() {
			os.Setenv(TLSProfileMinVersionEnvName, string(configv1.VersionTLS13))
			os.Setenv(TLSProfileCiphersEnvName, "TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384")

			profile := newTLSProfileFromEnv()
			Expect(profile.MinTLSVersion).To(Equal(configv1.VersionTLS13))
			Expect(profile.Ciphers).To(Equal([]string{
				"TLS_AES_128_GCM_SHA256",
				"TLS_AES_256_GCM_SHA384",
			}))
		})

		It("should handle min version set but ciphers empty (TLS 1.3)", func() {
			os.Setenv(TLSProfileMinVersionEnvName, string(configv1.VersionTLS13))
			os.Setenv(TLSProfileCiphersEnvName, "")

			profile := newTLSProfileFromEnv()
			Expect(profile.MinTLSVersion).To(Equal(configv1.VersionTLS13))
			Expect(profile.Ciphers).To(BeEmpty())
		})

		It("should fall back to Intermediate when min version is unrecognized", func() {
			os.Setenv(TLSProfileMinVersionEnvName, "TLS1.2") // invalid format
			os.Setenv(TLSProfileCiphersEnvName, "ECDHE-RSA-AES128-GCM-SHA256")

			profile := newTLSProfileFromEnv()
			Expect(profile.MinTLSVersion).To(Equal(configv1.VersionTLS12))
			Expect(profile.Ciphers).ToNot(BeEmpty())
		})
	})

	Describe("TLSProfileHash", func() {
		It("should return a deterministic 16-char hex string", func() {
			profile := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
			h1 := TLSProfileHash(profile)
			h2 := TLSProfileHash(profile)
			Expect(h1).To(Equal(h2))
			Expect(h1).To(HaveLen(16))
		})

		It("should produce different hashes for different MinTLSVersion", func() {
			intermediate := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
			modern := *configv1.TLSProfiles[configv1.TLSProfileModernType]
			Expect(TLSProfileHash(intermediate)).ToNot(Equal(TLSProfileHash(modern)))
		})

		It("should produce different hashes for different cipher lists", func() {
			a := configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS12,
				Ciphers:       []string{"TLS_AES_128_GCM_SHA256"},
			}
			b := configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS12,
				Ciphers:       []string{"TLS_AES_256_GCM_SHA384"},
			}
			Expect(TLSProfileHash(a)).ToNot(Equal(TLSProfileHash(b)))
		})
	})

	Describe("NewOutboundMTLSConfig", func() {
		var (
			ctx    context.Context
			cancel context.CancelFunc
			tmpDir string
		)

		BeforeEach(func() {
			ctx, cancel = context.WithCancel(context.Background())
			tmpDir = GinkgoT().TempDir()
		})

		AfterEach(func() {
			cancel()
		})

		It("should reject certFile without keyFile", func() {
			profile := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
			_, err := NewOutboundMTLSConfig(ctx, profile, "/some/cert.pem", "", "")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("certFile and keyFile must both be provided or both be empty"))
		})

		It("should reject keyFile without certFile", func() {
			profile := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
			_, err := NewOutboundMTLSConfig(ctx, profile, "", "/some/key.pem", "")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("certFile and keyFile must both be provided or both be empty"))
		})

		It("should succeed when both certFile and keyFile are provided", func() {
			profile := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
			certFile, keyFile := generateSelfSignedCert(tmpDir, "mtls")

			cfg, err := NewOutboundMTLSConfig(ctx, profile, certFile, keyFile, "")
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.GetClientCertificate).ToNot(BeNil())
			Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
		})

		It("should succeed when neither certFile nor keyFile are provided", func() {
			profile := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]

			cfg, err := NewOutboundMTLSConfig(ctx, profile, "", "", "")
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.GetClientCertificate).To(BeNil())
			Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
		})
	})

	Describe("startCertLoader", func() {
		var (
			ctx    context.Context
			cancel context.CancelFunc
			tmpDir string
		)

		BeforeEach(func() {
			ctx, cancel = context.WithCancel(context.Background())
			tmpDir = GinkgoT().TempDir()
		})

		AfterEach(func() {
			cancel()
		})

		It("should return a function that provides the current certificate", func() {
			certFile, keyFile := generateSelfSignedCert(tmpDir, "loader")

			getCert, err := startCertLoader(ctx, "test-server", certFile, keyFile)
			Expect(err).ToNot(HaveOccurred())

			cert, err := getCert()
			Expect(err).ToNot(HaveOccurred())
			Expect(cert).ToNot(BeNil())
			Expect(cert.Certificate).To(HaveLen(1))
		})

		It("should pick up rotated certificates", func() {
			certFile, keyFile := generateSelfSignedCert(tmpDir, "rotate")

			getCert, err := startCertLoader(ctx, "test-rotate", certFile, keyFile)
			Expect(err).ToNot(HaveOccurred())

			originalCert, err := getCert()
			Expect(err).ToNot(HaveOccurred())
			originalRaw := originalCert.Certificate[0]

			// Overwrite files with a new cert (different serial number)
			generateSelfSignedCertToFiles(certFile, keyFile)

			Eventually(func() bool {
				newCert, err := getCert()
				if err != nil {
					return false
				}
				return !bytes.Equal(newCert.Certificate[0], originalRaw)
			}, 5*time.Second, 200*time.Millisecond).Should(BeTrue())
		})

		It("should fail when cert file does not exist", func() {
			_, err := startCertLoader(ctx, "test-missing", "/nonexistent/tls.crt", "/nonexistent/tls.key")
			Expect(err).To(HaveOccurred())
		})

		It("should be usable with NewInboundTLSConfig", func() {
			certFile, keyFile := generateSelfSignedCert(tmpDir, "inbound")
			profile := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]

			cfg, err := NewInboundTLSConfig(ctx, profile, certFile, keyFile)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
			Expect(cfg.GetCertificate).ToNot(BeNil())

			cert, err := cfg.GetCertificate(nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(cert.Certificate).To(HaveLen(1))
		})
	})
})

// generateSelfSignedCertToFiles overwrites existing cert/key files with a freshly generated pair.
func generateSelfSignedCertToFiles(certPath, keyPath string) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	ExpectWithOffset(1, os.WriteFile(certPath, certPEM, 0o600)).To(Succeed())
	ExpectWithOffset(1, os.WriteFile(keyPath, keyPEM, 0o600)).To(Succeed())
}
