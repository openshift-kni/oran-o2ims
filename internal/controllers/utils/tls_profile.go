/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	tlspkg "github.com/openshift/controller-runtime-common/pkg/tls"
	"k8s.io/apiserver/pkg/server/dynamiccertificates"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FetchAPIServerTLSProfile retrieves the cluster TLS security profile from the APIServer resource.
// If no profile is configured, the Intermediate profile is returned as the default.
func FetchAPIServerTLSProfile(ctx context.Context, k8sClient client.Client) (configv1.TLSProfileSpec, error) {
	profile, err := tlspkg.FetchAPIServerTLSProfile(ctx, k8sClient)
	if err != nil {
		return configv1.TLSProfileSpec{}, fmt.Errorf("failed to fetch cluster TLS profile: %w", err)
	}
	return profile, nil
}

// NewTLSConfiguratorFromProfile returns a function that configures a tls.Config based on the given profile.
// This is intended to be used with controller-runtime's TLSOpts slice.
func NewTLSConfiguratorFromProfile(profile configv1.TLSProfileSpec) func(*tls.Config) {
	configurator, unsupported := tlspkg.NewTLSConfigFromProfile(profile)
	if len(unsupported) > 0 {
		slog.Warn("Unsupported cipher suites in TLS profile (skipped)",
			slog.Any("ciphers", unsupported))
	}
	return configurator
}

// startCertLoader creates a file-watching loader that hot-reloads cert/key without pod restart,
// and returns a function that fetches the current key pair on each TLS handshake.
func startCertLoader(ctx context.Context, name, certFile, keyFile string) (func() (*tls.Certificate, error), error) {
	loader, err := dynamiccertificates.NewDynamicServingContentFromFiles(name, certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to setup certificate loader: %w", err)
	}
	go loader.Run(ctx, 1)

	return func() (*tls.Certificate, error) {
		certBytes, keyBytes := loader.CurrentCertKeyContent()
		cert, err := tls.X509KeyPair(certBytes, keyBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to load certificate: %w", err)
		}
		return &cert, nil
	}, nil
}

// NewInboundTLSConfig creates a server tls.Config for accepting incoming connections,
// using the cluster TLS profile for MinVersion/CipherSuites and dynamic cert loading for cert rotation.
func NewInboundTLSConfig(ctx context.Context, profile configv1.TLSProfileSpec, certFile, keyFile string) (*tls.Config, error) {
	getCert, err := startCertLoader(ctx, "tls-server", certFile, keyFile)
	if err != nil {
		return nil, err
	}

	tlsConfig := &tls.Config{
		GetCertificate: func(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
			return getCert()
		},
	}
	NewTLSConfiguratorFromProfile(profile)(tlsConfig)
	return tlsConfig, nil
}

// NewOutboundMTLSConfig creates a tls.Config for outbound connections requiring
// mutual TLS (mTLS): we verify the remote server (CA bundle) and present our own certificate.
func NewOutboundMTLSConfig(ctx context.Context, profile configv1.TLSProfileSpec, certFile, keyFile, caFile string) (*tls.Config, error) {
	tlsConfig := &tls.Config{} //nolint:gosec

	if caFile != "" {
		if err := AddCABundle(tlsConfig, caFile); err != nil {
			return nil, fmt.Errorf("failed to add ca bundle: %w", err)
		}
	}

	if certFile != "" {
		getCert, err := startCertLoader(ctx, "tls-client", certFile, keyFile)
		if err != nil {
			return nil, err
		}
		tlsConfig.GetClientCertificate = func(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return getCert()
		}
	}

	NewTLSConfiguratorFromProfile(profile)(tlsConfig)
	return tlsConfig, nil
}

// NewOutboundTLSConfig creates a tls.Config for general outbound connections
// where we only verify the remote server (system CA bundles) without presenting a client certificate.
func NewOutboundTLSConfig(profile configv1.TLSProfileSpec, config *tls.Config) (*tls.Config, error) {
	if config == nil {
		config = &tls.Config{} //nolint:gosec
	}

	config.InsecureSkipVerify = GetTLSSkipVerify()
	if !config.InsecureSkipVerify {
		if err := loadDefaultCABundles(config); err != nil {
			return nil, fmt.Errorf("error loading default CABundles: %w", err)
		}
	}

	NewTLSConfiguratorFromProfile(profile)(config)
	return config, nil
}

// TLSProfileHash computes a short hex digest that changes whenever the effective
// TLS profile settings change. If the cluster admin changes the TLS profile, this hash
// changes and it is intended to be used with pod template annotation to trigger a rolling restart.
func TLSProfileHash(profile configv1.TLSProfileSpec) string {
	data := fmt.Sprintf("%s|%s", profile.MinTLSVersion, strings.Join(profile.Ciphers, ","))
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])[:16]
}

// newTLSProfileFromEnv reconstructs a TLSProfileSpec from operator-injected environment variables.
func newTLSProfileFromEnv() configv1.TLSProfileSpec {
	minVersion := os.Getenv(TLSProfileMinVersionEnvName)
	ciphersStr := os.Getenv(TLSProfileCiphersEnvName)

	if minVersion == "" {
		slog.Warn("TLS profile env vars not set, falling back to Intermediate profile")
		return *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	}

	// Get "," separated ciphers
	var ciphers []string
	if ciphersStr != "" {
		ciphers = strings.Split(ciphersStr, ",")
	} else if minVersion == string(configv1.VersionTLS13) {
		slog.Warn("TLS profile ciphers env var is empty, TLS 1.3 ciphers will be managed by Go internally",
			slog.String("minVersion", minVersion))
	} else {
		slog.Warn("TLS profile ciphers env var is empty for non-TLS1.3 profile, falling back to Intermediate ciphers",
			slog.String("minVersion", minVersion))
		return *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	}

	// Full cipher list is available in the TLS_PROFILE_CIPHERS env var on the pod spec
	slog.Info("TLS profile loaded from environment",
		slog.String("minVersion", minVersion),
		slog.Int("cipherCount", len(ciphers)))

	return configv1.TLSProfileSpec{
		MinTLSVersion: configv1.TLSProtocolVersion(minVersion),
		Ciphers:       ciphers,
	}
}
