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

	isCertWithoutKey := certFile != "" && keyFile == ""
	isKeyWithoutCert := keyFile != "" && certFile == ""
	if isCertWithoutKey || isKeyWithoutCert {
		return nil, fmt.Errorf("certFile and keyFile must both be provided or both be empty for mTLS (got certFile=%q, keyFile=%q)", certFile, keyFile)
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

// NewOutboundTLSConfig creates a tls.Config for general outbound connections applying only the
// TLS profile (MinVersion, CipherSuites). It does not load CA bundles; that is handled by the
// GetDefaultTLSConfig wrapper in utils.go.
func NewOutboundTLSConfig(profile configv1.TLSProfileSpec, config *tls.Config) (*tls.Config, error) {
	if config == nil {
		config = &tls.Config{} //nolint:gosec
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

// NewTLSProfileFromEnv reconstructs a TLSProfileSpec from operator-injected environment variables.
func NewTLSProfileFromEnv() configv1.TLSProfileSpec {
	minVersion := os.Getenv(TLSProfileMinVersionEnvName)
	ciphersStr := os.Getenv(TLSProfileCiphersEnvName)

	if minVersion == "" {
		slog.Warn("TLS profile env vars not set, falling back to Intermediate profile")
		return *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	}

	if !isValidTLSVersion(minVersion) {
		slog.Warn("Unrecognized TLS_PROFILE_MIN_VERSION value, falling back to Intermediate profile",
			slog.String("value", minVersion))
		return *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	}

	// Get "," separated ciphers
	var ciphers []string
	switch {
	case ciphersStr != "":
		ciphers = strings.Split(ciphersStr, ",")
	case minVersion == string(configv1.VersionTLS13):
		slog.Warn("TLS profile ciphers env var is empty, TLS 1.3 ciphers will be managed by Go internally",
			slog.String("minVersion", minVersion))
	default:
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

// TLSVersionToPostgres converts an OpenShift TLS version string (e.g. "VersionTLS13")
// to the PostgreSQL ssl_min_protocol_version format (e.g. "TLSv1.3").
// Returns "TLSv1.2" as a safe default for unrecognized values.
func TLSVersionToPostgres(version string) string {
	pgVersions := map[string]string{
		string(configv1.VersionTLS10): "TLSv1",
		string(configv1.VersionTLS11): "TLSv1.1",
		string(configv1.VersionTLS12): "TLSv1.2",
		string(configv1.VersionTLS13): "TLSv1.3",
	}
	if v, ok := pgVersions[version]; ok {
		return v
	}
	slog.Warn("Unrecognized TLS version for PostgreSQL, falling back to TLSv1.2",
		slog.String("version", version))
	return "TLSv1.2"
}

// TLSCiphersToPostgres converts a cipher list from the cluster TLS profile into
// the colon-separated format expected by PostgreSQL's ssl_ciphers setting.
//
// TLS 1.3 cipher suites (prefixed with "TLS_") are excluded because PostgreSQL's
// ssl_ciphers only accepts TLS 1.2 cipher names — passing TLS 1.3 names causes
// "could not set the cipher list (no valid ciphers available)".
// PostgreSQL manages TLS 1.3 ciphers internally via OpenSSL.
//
// Returns an empty string if no TLS 1.2 ciphers are present (e.g., Modern profile),
// signaling to the caller that ssl_ciphers should not be set.
func TLSCiphersToPostgres(ciphers []string) string {
	var pgCiphers []string
	for _, c := range ciphers {
		if !strings.HasPrefix(c, "TLS_") {
			pgCiphers = append(pgCiphers, c)
		}
	}
	return strings.Join(pgCiphers, ":")
}

// PostgreSQL TLS 1.3 and key-exchange defaults for PG18+ native controls.
// These are the OpenShift-approved values that satisfy tls-scanner compliance.
const (
	// PostgresTLS13Ciphers is the ssl_tls13_ciphers value: all standard TLS 1.3
	// ciphers except TLS_AES_128_CCM_SHA256 (not in the OpenShift Modern profile).
	PostgresTLS13Ciphers = "TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256:TLS_AES_128_GCM_SHA256"

	// PostgresSSLGroups is the ssl_groups value: ML-KEM for PQC key exchange
	// alongside classical fallbacks.
	PostgresSSLGroups = "X25519MLKEM768:X25519:prime256v1"
)

var validTLSVersions = map[string]bool{
	string(configv1.VersionTLS10): true,
	string(configv1.VersionTLS11): true,
	string(configv1.VersionTLS12): true,
	string(configv1.VersionTLS13): true,
}

func isValidTLSVersion(v string) bool {
	return validTLSVersions[v]
}
