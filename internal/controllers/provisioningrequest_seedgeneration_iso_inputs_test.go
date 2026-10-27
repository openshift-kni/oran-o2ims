/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"
)

func seedGenerationISOInputFixture() (*provisioningv1alpha1.ProvisioningRequest, *seedGenerationInputSnapshot) {
	pr := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{
		Name: "seed-request", UID: types.UID("generation-uid"),
	}}
	snapshot := &seedGenerationInputSnapshot{
		Document: &seedGenerationInputDocument{
			Release: "4.22.15",
			SeedGeneration: map[string]any{
				"seedImage": "quay.io/example/seed:4.22.15",
				"liveISO": map[string]any{
					"installationDisk": "/dev/disk/by-id/example", "sshKey": "ssh-ed25519 public-key",
					"urlBase": "https://iso.example.test/ibi/",
				},
			},
			ImageDigestSources: []seedGenerationImageDigestSource{{
				Source: "registry.example/source", Mirrors: []string{"mirror.example/source"},
				MirrorSourcePolicy: "NeverContactSource",
			}},
			RegistryTrustedCAs: map[string]string{"registry-b": "CA-B\n", "registry-a": "CA-A\n"},
		},
		Secrets: map[string]*corev1.Secret{
			"pullSecret": {Data: map[string][]byte{
				corev1.DockerConfigJsonKey: []byte(`{"auths":{"quay.io/example":{"auth":"sensitive-auth"}}}`),
			}},
			"upload": {Data: map[string][]byte{
				"host": []byte("iso.example.test"), "username": []byte("uploader"),
				"remotePath": []byte("/var/www/html/ibi/"),
			}},
		},
	}
	return pr, snapshot
}

func TestSeedGenerationISOArtifactUsesUniqueRunDirectory(t *testing.T) {
	g := NewWithT(t)
	pr, snapshot := seedGenerationISOInputFixture()
	liveISO := snapshot.Document.SeedGeneration["liveISO"].(map[string]any)
	artifact, err := seedGenerationISOArtifactForRun(pr, liveISO, snapshot.Secrets["upload"])
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(artifact.URL).To(Equal("https://iso.example.test/ibi/seedgen/generation-uid/rhcos-ibi.iso"))
	g.Expect(artifact.RemoteDirectory).To(Equal("/var/www/html/ibi/seedgen/generation-uid"))
	g.Expect(artifact.SSHTarget).To(Equal("uploader@iso.example.test"))
	g.Expect(artifact.SCPTarget).To(Equal("uploader@iso.example.test"))

	pr.UID = types.UID("replacement-uid")
	replacement, err := seedGenerationISOArtifactForRun(pr, liveISO, snapshot.Secrets["upload"])
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(replacement.URL).ToNot(Equal(artifact.URL))
	g.Expect(replacement.RemoteDirectory).ToNot(Equal(artifact.RemoteDirectory))
}

func TestSeedGenerationISOArtifactSupportsIPv6AndURLRoot(t *testing.T) {
	g := NewWithT(t)
	pr, snapshot := seedGenerationISOInputFixture()
	liveISO := snapshot.Document.SeedGeneration["liveISO"].(map[string]any)
	liveISO["urlBase"] = "https://[2001:db8::1]:8443/"
	snapshot.Secrets["upload"].Data["host"] = []byte("2001:db8::1")
	artifact, err := seedGenerationISOArtifactForRun(pr, liveISO, snapshot.Secrets["upload"])
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(artifact.URL).To(Equal("https://[2001:db8::1]:8443/seedgen/generation-uid/rhcos-ibi.iso"))
	g.Expect(artifact.SSHTarget).To(Equal("uploader@2001:db8::1"))
	g.Expect(artifact.SCPTarget).To(Equal("uploader@[2001:db8::1]"))
}

func TestSeedGenerationISOArtifactRejectsUnsafeTargets(t *testing.T) {
	for _, tc := range []struct{ name, field, value string }{
		{"host options", "host", "-oProxyCommand=bad"},
		{"host shell", "host", "host;command"},
		{"user shell", "username", "user;command"},
		{"relative remote path", "remotePath", "images"},
		{"remote traversal", "remotePath", "/var/www/../other"},
		{"remote shell", "remotePath", "/var/www/$(command)"},
		{"unencrypted URL", "urlBase", "http://iso.example.test/ibi/"},
		{"URL credentials", "urlBase", "https://user:password@iso.example.test/ibi/"},
		{"URL query", "urlBase", "https://iso.example.test/ibi/?token=secret"},
		{"URL fragment", "urlBase", "https://iso.example.test/ibi/#fragment"},
		{"URL traversal", "urlBase", "https://iso.example.test/ibi/../other"},
		{"escaped URL traversal", "urlBase", "https://iso.example.test/ibi/%2e%2e/other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			pr, snapshot := seedGenerationISOInputFixture()
			liveISO := snapshot.Document.SeedGeneration["liveISO"].(map[string]any)
			if tc.field == "urlBase" {
				liveISO[tc.field] = tc.value
			} else {
				snapshot.Secrets["upload"].Data[tc.field] = []byte(tc.value)
			}
			_, err := seedGenerationISOArtifactForRun(pr, liveISO, snapshot.Secrets["upload"])
			g.Expect(err).To(HaveOccurred())
		})
	}
}

func TestSeedGenerationISOConfigUsesFrozenCredentialsAndMirrors(t *testing.T) {
	g := NewWithT(t)
	_, snapshot := seedGenerationISOInputFixture()
	snapshot.Secrets["upload"].Data["caCert"] = []byte("UPLOAD-SERVER-CA\n")
	data, err := seedGenerationISOConfig(snapshot)
	g.Expect(err).ToNot(HaveOccurred())
	var config map[string]any
	g.Expect(yaml.Unmarshal(data, &config)).To(Succeed())
	g.Expect(config).To(HaveKeyWithValue("apiVersion", "v1beta1"))
	g.Expect(config).To(HaveKeyWithValue("kind", "ImageBasedInstallationConfig"))
	g.Expect(config).To(HaveKeyWithValue("seedImage", "quay.io/example/seed:4.22.15"))
	g.Expect(config).To(HaveKeyWithValue("seedVersion", "4.22.15"))
	g.Expect(config).To(HaveKeyWithValue("installationDisk", "/dev/disk/by-id/example"))
	g.Expect(config).To(HaveKeyWithValue("sshKey", "ssh-ed25519 public-key"))
	g.Expect(config).To(HaveKeyWithValue("pullSecret", string(snapshot.Secrets["pullSecret"].Data[corev1.DockerConfigJsonKey])))
	g.Expect(config).To(HaveKeyWithValue("additionalTrustBundle", "CA-A\nCA-B\n"))
	g.Expect(config).ToNot(HaveKey("networkConfig"))
	g.Expect(string(data)).ToNot(ContainSubstring("mirrorSourcePolicy"))
	g.Expect(string(data)).To(ContainSubstring("mirror.example/source"))
	g.Expect(snapshot.Document.ImageDigestSources[0].MirrorSourcePolicy).To(Equal("NeverContactSource"))

	snapshot.Document.RegistryTrustedCAs = nil
	snapshot.Document.ImageDigestSources = nil
	delete(snapshot.Document.SeedGeneration["liveISO"].(map[string]any), "sshKey")
	data, err = seedGenerationISOConfig(snapshot)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(data)).ToNot(ContainSubstring("additionalTrustBundle"))
	g.Expect(string(data)).ToNot(ContainSubstring("imageDigestSources"))
	g.Expect(string(data)).ToNot(ContainSubstring("sshKey"))
}

func TestSeedGenerationISOInputsRejectMissingSnapshots(t *testing.T) {
	g := NewWithT(t)
	_, err := seedGenerationISOConfig(nil)
	g.Expect(err).To(HaveOccurred())
	_, snapshot := seedGenerationISOInputFixture()
	snapshot.Secrets["pullSecret"] = nil
	_, err = seedGenerationISOConfig(snapshot)
	g.Expect(err).To(HaveOccurred())
	g.Expect(strings.Contains(err.Error(), "sensitive-auth")).To(BeFalse())
	pr, snapshot := seedGenerationISOInputFixture()
	pr.UID = ""
	_, err = seedGenerationISOArtifactForRun(pr, snapshot.Document.SeedGeneration["liveISO"].(map[string]any), snapshot.Secrets["upload"])
	g.Expect(err).To(HaveOccurred())
}
