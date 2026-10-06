/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"strings"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

func TestSeedGenerationDigestMirrorConfiguration(t *testing.T) {
	raw := []any{
		map[string]any{
			"source":             " quay.io/team/ ",
			"mirrors":            []any{"mirror.example/team", "mirror2.example/team"},
			"mirrorSourcePolicy": string(configv1.NeverContactSource),
		},
		map[string]any{
			"source":             "quay.io/team",
			"mirrors":            []any{"mirror.example/team", "mirror3.example/team"},
			"mirrorSourcePolicy": string(configv1.AllowContactingSource),
		},
	}
	sources, err := parseSeedGenerationImageDigestSources(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Source != "quay.io/team" ||
		sources[0].MirrorSourcePolicy != string(configv1.NeverContactSource) ||
		strings.Join(sources[0].Mirrors, ",") != "mirror.example/team,mirror2.example/team,mirror3.example/team" {
		t.Fatalf("unexpected normalized mirror sources: %+v", sources)
	}

	invalid := []struct {
		name string
		raw  any
	}{
		{name: "not an array", raw: "invalid"},
		{name: "entry is not an object", raw: []any{"invalid"}},
		{name: "missing source", raw: []any{map[string]any{"mirrors": []any{"mirror.example"}}}},
		{name: "source is not a string", raw: []any{map[string]any{"source": 1, "mirrors": []any{"mirror.example"}}}},
		{name: "mirrors is not an array", raw: []any{map[string]any{"source": "quay.io/team", "mirrors": "mirror.example"}}},
		{name: "mirrors is empty", raw: []any{map[string]any{"source": "quay.io/team", "mirrors": []any{}}}},
		{name: "mirror is not a string", raw: []any{map[string]any{"source": "quay.io/team", "mirrors": []any{1}}}},
		{name: "mirror is empty", raw: []any{map[string]any{"source": "quay.io/team", "mirrors": []any{" "}}}},
		{name: "invalid policy", raw: []any{map[string]any{
			"source": "quay.io/team", "mirrors": []any{"mirror.example"}, "mirrorSourcePolicy": "Sometimes",
		}}},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseSeedGenerationImageDigestSources(tc.raw); err == nil {
				t.Fatal("invalid mirror input was accepted")
			}
		})
	}

	normalized := normalizeSeedGenerationImageDigestSources([]seedGenerationImageDigestSource{
		{Source: "z.example/", Mirrors: []string{" mirror.example/", "mirror.example"}},
		{Source: " ", Mirrors: []string{"ignored.example"}},
		{Source: "a.example", Mirrors: []string{"a-mirror.example"}},
	})
	if len(normalized) != 2 || normalized[0].Source != "a.example" || normalized[1].Source != "z.example" ||
		len(normalized[1].Mirrors) != 1 {
		t.Fatalf("normalization did not sort, trim, deduplicate, or drop empty sources: %+v", normalized)
	}

	encoded, err := seedGenerationMirrorConfigYAML(sources)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"apiVersion: config.openshift.io/v1", "kind: ImageDigestMirrorSet", "quay.io/team"} {
		if !strings.Contains(encoded, expected) {
			t.Fatalf("encoded mirror config does not contain %q: %s", expected, encoded)
		}
	}
}

func TestSeedGenerationMirrorMatchingAndTagResolution(t *testing.T) {
	matching := []struct {
		name       string
		source     string
		registry   string
		repository string
		want       bool
	}{
		{name: "host and repository prefix", source: "quay.io/team", registry: "quay.io", repository: "team/release", want: true},
		{name: "host only", source: "quay.io", registry: "quay.io", repository: "other/release", want: true},
		{name: "wildcard child", source: "*.example.com", registry: "east.example.com", repository: "team/release", want: true},
		{name: "wildcard excludes apex", source: "*.example.com", registry: "example.com", repository: "team/release", want: false},
		{name: "repository mismatch", source: "quay.io/team", registry: "quay.io", repository: "team2/release", want: false},
		{name: "host mismatch", source: "quay.io/team", registry: "registry.example", repository: "team/release", want: false},
	}
	for _, tc := range matching {
		t.Run(tc.name, func(t *testing.T) {
			if got := seedGenerationMirrorSourceMatches(tc.source, tc.registry, tc.repository); got != tc.want {
				t.Fatalf("match=%t, want %t", got, tc.want)
			}
		})
	}

	image := "quay.io/team/release:4.22"
	sets := []configv1.ImageTagMirrorSet{{Spec: configv1.ImageTagMirrorSetSpec{ImageTagMirrors: []configv1.ImageTagMirrors{
		{Source: "quay.io", Mirrors: []configv1.ImageMirror{"mirror.example/general"}},
		{Source: "quay.io/team", Mirrors: []configv1.ImageMirror{"mirror.example/team"}},
	}}}}
	resolved, matched, err := resolveSeedGenerationImageTagMirror(image, sets)
	if err != nil || !matched || resolved != "mirror.example/team/release:4.22" {
		t.Fatalf("resolved image=%q matched=%t err=%v", resolved, matched, err)
	}

	resolved, matched, err = resolveSeedGenerationImageTagMirror(image, nil)
	if err != nil || matched || resolved != image {
		t.Fatalf("unmatched image=%q matched=%t err=%v", resolved, matched, err)
	}
	if _, _, err := resolveSeedGenerationImageTagMirror("invalid?image", nil); err == nil {
		t.Fatal("invalid release image reference was accepted")
	}
	noMirrors := []configv1.ImageTagMirrorSet{{Spec: configv1.ImageTagMirrorSetSpec{ImageTagMirrors: []configv1.ImageTagMirrors{
		{Source: "quay.io/team"},
	}}}}
	if _, _, err := resolveSeedGenerationImageTagMirror(image, noMirrors); err == nil {
		t.Fatal("matching tag mirror set without mirrors was accepted")
	}

	if !hasDigestMirrorForImage("quay.io/team/release:4.22", []seedGenerationImageDigestSource{{
		Source: "quay.io/team", Mirrors: []string{"mirror.example/team"},
	}}) {
		t.Fatal("matching digest mirror was not detected")
	}
	if hasDigestMirrorForImage("quay.io/other/release:4.22", []seedGenerationImageDigestSource{{
		Source: "quay.io/team", Mirrors: []string{"mirror.example/team"},
	}}) || hasDigestMirrorForImage("invalid?image", nil) {
		t.Fatal("non-matching digest mirror was detected")
	}
	if !seedGenerationImageHasDigest("quay.io/team/release@sha256:abc") ||
		seedGenerationImageHasDigest("quay.io/team/release:4.22") || !seedGenerationImageHasTag("quay.io/team/release:4.22") {
		t.Fatal("release image tag and digest classification is incorrect")
	}

	for image, want := range map[string]string{
		"quay.io/team/release:4.22":          ":4.22",
		"quay.io/team/release":               ":latest",
		"docker://quay.io/team/release:4.22": ":4.22",
		"quay.io/team/release@sha256:abc":    "@sha256:abc",
	} {
		if got := seedGenerationImageTagSuffix(image); got != want {
			t.Errorf("suffix for %q = %q, want %q", image, got, want)
		}
	}
}

func TestNormalizeSeedGenerationRegistryTrustHost(t *testing.T) {
	for _, tc := range []struct {
		encoded string
		want    string
		valid   bool
	}{
		{encoded: "mirror.example.com", want: "mirror.example.com", valid: true},
		{encoded: "mirror.example.com..5000", want: "mirror.example.com:5000", valid: true},
		{encoded: "mirror.example.com..5000..6000", want: "mirror.example.com:5000..6000", valid: false},
	} {
		got := normalizeSeedGenerationRegistryTrustHost(tc.encoded)
		if got != tc.want || validRegistryCertificateHost(got) != tc.valid {
			t.Errorf("normalize(%q) = %q, valid=%t; want %q, valid=%t",
				tc.encoded, got, validRegistryCertificateHost(got), tc.want, tc.valid)
		}
	}
}
