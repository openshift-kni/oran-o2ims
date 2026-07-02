/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package tls

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	tlspkg "github.com/openshift/controller-runtime-common/pkg/tls"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apiserver/pkg/server/dynamiccertificates"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
)

// Environment variable names for TLS profile configuration injected by the operator.
const (
	TLSProfileMinVersionEnvName = "TLS_PROFILE_MIN_VERSION"
	TLSProfileCiphersEnvName    = "TLS_PROFILE_CIPHERS"
)

// Default CA bundle path for the service account.
const defaultBackendCABundle = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt" // nolint: gosec // hardcoded path only

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
// TLS profile (MinVersion, CipherSuites). It does not load CA bundles; use GetDefaultTLSConfig
// for connections that need in-cluster CA trust.
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

// newTLSProfileFromEnv reconstructs a TLSProfileSpec from operator-injected environment variables.
func newTLSProfileFromEnv() configv1.TLSProfileSpec {
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

var validTLSVersions = map[string]bool{
	string(configv1.VersionTLS10): true,
	string(configv1.VersionTLS11): true,
	string(configv1.VersionTLS12): true,
	string(configv1.VersionTLS13): true,
}

func isValidTLSVersion(v string) bool {
	return validTLSVersions[v]
}

// loadDefaultCABundles loads the default service account and ingress CA bundles.
func loadDefaultCABundles(config *tls.Config) error {
	config.RootCAs = x509.NewCertPool()
	if data, err := os.ReadFile(defaultBackendCABundle); err != nil {
		return fmt.Errorf("failed to read CA bundle '%s': %w", defaultBackendCABundle, err)
	} else {
		config.RootCAs.AppendCertsFromPEM(data)
	}

	if data, err := os.ReadFile(constants.DefaultServiceCAFile); err != nil {
		return fmt.Errorf("failed to read service CA file '%s': %w", constants.DefaultServiceCAFile, err)
	} else {
		config.RootCAs.AppendCertsFromPEM(data)
	}

	return nil
}

// GetDefaultTLSConfig sets the TLS configuration attributes appropriately to enable communication between internal
// services and accessing the public facing API endpoints. TLS version and cipher suites are inherited from the
// cluster TLS security profile (via operator-injected environment variables).
// When loadCAs is true, default in-cluster CA bundles are loaded.
// Pass loadCAs=false when the caller provides its own trust anchors (e.g., a pinned service CA).
func GetDefaultTLSConfig(config *tls.Config, loadCAs bool) (*tls.Config, error) {
	profile := newTLSProfileFromEnv()
	tlsConfig, err := NewOutboundTLSConfig(profile, config)
	if err != nil {
		return nil, err
	}

	if loadCAs {
		if err := loadDefaultCABundles(tlsConfig); err != nil {
			return nil, fmt.Errorf("error loading default CABundles: %w", err)
		}
	}

	return tlsConfig, nil
}

// AddCABundle appends a PEM-encoded CA bundle file to the TLS configuration's root CA pool.
func AddCABundle(config *tls.Config, caBundle string) error {
	data, err := os.ReadFile(caBundle)
	if err != nil {
		return fmt.Errorf("failed to read CA bundle '%s': %w", caBundle, err)
	}

	if config.RootCAs == nil {
		config.RootCAs = x509.NewCertPool()
	}
	config.RootCAs.AppendCertsFromPEM(data)

	return nil
}

// GetClientTLSConfig creates a tls.Config for outbound mTLS connections with dynamic cert/key rotation.
// TLS version and cipher suites are inherited from the cluster TLS security profile
// (via operator-injected environment variables).
func GetClientTLSConfig(ctx context.Context, certFile, keyFile, caFile string) (*tls.Config, error) {
	profile := newTLSProfileFromEnv()
	return NewOutboundMTLSConfig(ctx, profile, certFile, keyFile, caFile)
}

// GetServerTLSConfig creates a tls.Config for accepting incoming connections with dynamic cert/key rotation.
// TLS version and cipher suites are inherited from the cluster TLS security profile
// (via operator-injected environment variables).
func GetServerTLSConfig(ctx context.Context, certFile, keyFile string) (*tls.Config, error) {
	profile := newTLSProfileFromEnv()
	return NewInboundTLSConfig(ctx, profile, certFile, keyFile)
}

// GetDefaultBackendTransport returns an HTTP transport with the proper TLS defaults set.
func GetDefaultBackendTransport() (http.RoundTripper, error) {
	tlsConfig, err := GetDefaultTLSConfig(nil, true)
	if err != nil {
		return nil, err
	}

	return utilnet.SetTransportDefaults(&http.Transport{TLSClientConfig: tlsConfig}), nil
}
