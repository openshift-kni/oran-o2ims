/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	k8svalidation "k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"
)

const seedGenerationHTTPScheme = "https"

// Validate the exact credential bytes both before provisioning and when they
// are frozen. A successful condition on the PR cannot validate a rotated Secret.
func (t *provisioningRequestReconcilerTask) validateSeedGenerationISOUploadInputs(
	ctx context.Context, liveISO map[string]any, upload *corev1.Secret,
) error {
	for _, key := range []string{"host", "username", "privateKey", "remotePath", "knownHosts"} {
		if upload == nil || strings.TrimSpace(string(upload.Data[key])) == "" {
			return fmt.Errorf("upload Secret is missing non-empty %q data", key)
		}
	}
	artifact, err := seedGenerationISOArtifactForRun(t.object, liveISO, upload)
	if err != nil {
		return err
	}
	urlBase, err := seedGenerationString(liveISO, "urlBase")
	if err != nil {
		return err
	}
	if err := validateSeedGenerationHTTPS(ctx, urlBase, upload.Data["caCert"]); err != nil {
		return fmt.Errorf("liveISO.urlBase TLS preflight failed: %w", err)
	}
	if rawBaseISOURL, exists := liveISO["baseISOURL"]; exists {
		baseISOURL, ok := rawBaseISOURL.(string)
		if !ok || !validSeedGenerationISOHTTPSURL(baseISOURL) {
			return fmt.Errorf("liveISO.baseISOURL must be an HTTPS URL with a host and no user info, query, or fragment")
		}
		if err := validateSeedGenerationHTTPS(ctx, baseISOURL, upload.Data["caCert"]); err != nil {
			return fmt.Errorf("liveISO.baseISOURL TLS preflight failed: %w", err)
		}
	}
	return validateSeedGenerationSSHUploadAccess(ctx, string(upload.Data["host"]), upload.Data, artifact, string(t.object.UID))
}

var (
	seedGenerationSSHUserPattern    = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.-]*$`)
	seedGenerationRemotePathPattern = regexp.MustCompile(`^/[a-zA-Z0-9/_.-]*$`)
	seedGenerationRunIDPattern      = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)
)

// remotePath and urlBase are the SSH and HTTPS views of the configured base
// directory. The run UID gives both views the same unique suffix. The upload
// script verifies the actual HTTPS bytes before publishing this URL in status.
type seedGenerationISOArtifact struct {
	URL             string
	RemoteDirectory string
	SSHTarget       string
	SCPTarget       string
}

func seedGenerationISOArtifactForRun(
	pr *provisioningv1alpha1.ProvisioningRequest,
	liveISO map[string]any,
	upload *corev1.Secret,
) (*seedGenerationISOArtifact, error) {
	if pr == nil || !seedGenerationRunIDPattern.MatchString(string(pr.UID)) {
		return nil, fmt.Errorf("a valid ProvisioningRequest UID is required for ISO publication")
	}
	if upload == nil {
		return nil, fmt.Errorf("live ISO upload credentials are missing")
	}
	host := string(upload.Data["host"])
	username := string(upload.Data["username"])
	remotePath := string(upload.Data["remotePath"])
	if net.ParseIP(host) == nil && len(k8svalidation.IsDNS1123Subdomain(host)) != 0 {
		return nil, fmt.Errorf("live ISO upload host must be an IP address or DNS hostname")
	}
	if !seedGenerationSSHUserPattern.MatchString(username) {
		return nil, fmt.Errorf("live ISO upload username contains unsupported characters")
	}
	if !seedGenerationCleanDirectory(remotePath) {
		return nil, fmt.Errorf("live ISO upload remotePath must be a clean absolute directory path without shell metacharacters")
	}
	base, err := seedGenerationString(liveISO, "urlBase")
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != seedGenerationHTTPScheme || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return nil, fmt.Errorf("liveISO.urlBase must be an HTTPS directory URL without credentials, escapes, query, or fragment")
	}
	if parsed.Path != "" && !seedGenerationCleanDirectory(parsed.Path) {
		return nil, fmt.Errorf("liveISO.urlBase must have a clean directory path")
	}
	runDirectory := path.Join("seedgen", string(pr.UID))
	parsed.Path = path.Join(parsed.Path, runDirectory, "rhcos-ibi.iso")
	if !strings.HasPrefix(parsed.Path, "/") {
		parsed.Path = "/" + parsed.Path
	}
	scpHost := host
	if strings.Contains(host, ":") {
		scpHost = "[" + host + "]"
	}
	return &seedGenerationISOArtifact{
		URL: parsed.String(), RemoteDirectory: path.Join(remotePath, runDirectory),
		SSHTarget: username + "@" + host, SCPTarget: username + "@" + scpHost,
	}, nil
}

func seedGenerationCleanDirectory(directory string) bool {
	return seedGenerationRemotePathPattern.MatchString(directory) &&
		(directory == "/" || path.Clean(directory) == strings.TrimSuffix(directory, "/"))
}

func validSeedGenerationISOHTTPSURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && parsed.Scheme == seedGenerationHTTPScheme && parsed.Hostname() != "" &&
		parsed.User == nil && parsed.RawQuery == "" && !parsed.ForceQuery &&
		parsed.Fragment == "" && !strings.Contains(rawURL, "#")
}

type seedGenerationISOInstallationConfig struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name string `json:"name"`
	} `json:"metadata"`
	SeedImage             string                               `json:"seedImage"`
	SeedVersion           string                               `json:"seedVersion"`
	InstallationDisk      string                               `json:"installationDisk"`
	SSHKey                string                               `json:"sshKey,omitempty"`
	PullSecret            string                               `json:"pullSecret"`
	ImageDigestSources    []seedGenerationISOImageDigestSource `json:"imageDigestSources,omitempty"`
	AdditionalTrustBundle string                               `json:"additionalTrustBundle,omitempty"`
}

// The installer's imageDigestSources does not accept mirrorSourcePolicy. That
// policy remains in the separate IDMS file used by oc during release extraction.
type seedGenerationISOImageDigestSource struct {
	Source  string   `json:"source"`
	Mirrors []string `json:"mirrors"`
}

// seedGenerationISOConfig reads only the immutable snapshot. The result embeds
// registry credentials and must be stored in a Secret, never in a ConfigMap,
// status, a command argument, or a log message.
func seedGenerationISOConfig(snapshot *seedGenerationInputSnapshot) ([]byte, error) {
	if snapshot == nil || snapshot.Document == nil {
		return nil, fmt.Errorf("live ISO input snapshot is missing")
	}
	document := snapshot.Document
	liveISO, ok := document.SeedGeneration["liveISO"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("frozen liveISO configuration is missing")
	}
	pullSecret := snapshot.Secrets["pullSecret"]
	if pullSecret == nil || len(pullSecret.Data[corev1.DockerConfigJsonKey]) == 0 {
		return nil, fmt.Errorf("frozen ISO pull credentials are missing")
	}
	seedImage, err := seedGenerationString(document.SeedGeneration, "seedImage")
	if err != nil {
		return nil, err
	}
	disk, err := seedGenerationString(liveISO, "installationDisk")
	if err != nil {
		return nil, err
	}
	config := seedGenerationISOInstallationConfig{
		APIVersion: "v1beta1", Kind: "ImageBasedInstallationConfig",
		SeedImage: seedImage, SeedVersion: document.Release,
		InstallationDisk: disk, PullSecret: string(pullSecret.Data[corev1.DockerConfigJsonKey]),
	}
	config.Metadata.Name = "ibi-config"
	config.SSHKey, _ = liveISO["sshKey"].(string)
	for _, source := range document.ImageDigestSources {
		config.ImageDigestSources = append(config.ImageDigestSources, seedGenerationISOImageDigestSource{
			Source: source.Source, Mirrors: slices.Clone(source.Mirrors),
		})
	}
	registries := make([]string, 0, len(document.RegistryTrustedCAs))
	for registry := range document.RegistryTrustedCAs {
		registries = append(registries, registry)
	}
	slices.Sort(registries)
	for _, registry := range registries {
		config.AdditionalTrustBundle += strings.TrimSpace(document.RegistryTrustedCAs[registry]) + "\n"
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to encode ISO installation configuration: %w", err)
	}
	return data, nil
}
