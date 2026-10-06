/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"crypto/x509"
	"fmt"
	"sort"
	"strings"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"
)

const (
	seedGenerationMirrorConfigAPIVersion = "config.openshift.io/v1"
	seedGenerationMirrorConfigKind       = "ImageDigestMirrorSet"
)

type seedGenerationIDMSFile struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		ImageDigestMirrors []seedGenerationImageDigestSource `json:"imageDigestMirrors"`
	} `json:"spec"`
}

func seedGenerationMirrorConfigYAML(sources []seedGenerationImageDigestSource) (string, error) {
	file := seedGenerationIDMSFile{
		APIVersion: seedGenerationMirrorConfigAPIVersion,
		Kind:       seedGenerationMirrorConfigKind,
	}
	file.Metadata.Name = "seed-generation-preflight"
	file.Spec.ImageDigestMirrors = append([]seedGenerationImageDigestSource{}, sources...)
	encoded, err := yaml.Marshal(file)
	if err != nil {
		return "", fmt.Errorf("failed to encode release mirror configuration: %w", err)
	}
	return string(encoded), nil
}

func (t *provisioningRequestReconcilerTask) resolveSeedGenerationMirrorInputs(
	ctx context.Context,
	clusterTemplate *provisioningv1alpha1.ClusterTemplate,
	liveISO map[string]any,
	releaseImage string,
) (string, []seedGenerationImageDigestSource, map[string]string, error) {
	sources, err := t.seedGenerationImageDigestSources(ctx, liveISO)
	if err != nil {
		return "", nil, nil, err
	}
	trustedCAs, err := t.seedGenerationRegistryTrust(ctx, clusterTemplate, liveISO)
	if err != nil {
		return "", nil, nil, err
	}
	resolvedReleaseImage := releaseImage
	if seedGenerationImageHasTag(releaseImage) {
		tagMirrors := &configv1.ImageTagMirrorSetList{}
		if err := t.client.List(ctx, tagMirrors); err != nil {
			return "", nil, nil, fmt.Errorf("failed to list hub ImageTagMirrorSets: %w", err)
		}
		for i := range tagMirrors.Items {
			if tagMirrors.Items[i].DeletionTimestamp != nil {
				return "", nil, nil, fmt.Errorf("hub ImageTagMirrorSet %s is being deleted", tagMirrors.Items[i].Name)
			}
		}
		var matched bool
		resolvedReleaseImage, matched, err = resolveSeedGenerationImageTagMirror(releaseImage, tagMirrors.Items)
		if err != nil {
			return "", nil, nil, err
		}
		if !matched && hasDigestMirrorForImage(releaseImage, sources) {
			return "", nil, nil, fmt.Errorf("releaseImage %q uses a mutable tag covered only by digest mirrors; use a direct mirror pull-spec or configure an ImageTagMirrorSet", releaseImage)
		}
	}
	return resolvedReleaseImage, sources, trustedCAs, nil
}

func (t *provisioningRequestReconcilerTask) seedGenerationImageDigestSources(
	ctx context.Context,
	liveISO map[string]any,
) ([]seedGenerationImageDigestSource, error) {
	if raw, exists := liveISO["imageDigestSources"]; exists {
		return parseSeedGenerationImageDigestSources(raw)
	}
	return t.hubSeedGenerationImageDigestSources(ctx)
}

func parseSeedGenerationImageDigestSources(raw any) ([]seedGenerationImageDigestSource, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("liveISO.imageDigestSources must be an array")
	}
	sources := make([]seedGenerationImageDigestSource, 0, len(items))
	for index, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("liveISO.imageDigestSources[%d] must be an object", index)
		}
		source, ok := entry["source"].(string)
		if !ok || strings.TrimSpace(source) == "" {
			return nil, fmt.Errorf("liveISO.imageDigestSources[%d].source must be a non-empty string", index)
		}
		mirrors, ok := entry["mirrors"].([]any)
		if !ok || len(mirrors) == 0 {
			return nil, fmt.Errorf("liveISO.imageDigestSources[%d].mirrors must be a non-empty array", index)
		}
		sourceEntry := seedGenerationImageDigestSource{Source: strings.TrimSuffix(source, "/")}
		for mirrorIndex, value := range mirrors {
			mirror, ok := value.(string)
			if !ok || strings.TrimSpace(mirror) == "" {
				return nil, fmt.Errorf("liveISO.imageDigestSources[%d].mirrors[%d] must be a non-empty string", index, mirrorIndex)
			}
			sourceEntry.Mirrors = append(sourceEntry.Mirrors, strings.TrimSuffix(mirror, "/"))
		}
		if rawPolicy, exists := entry["mirrorSourcePolicy"]; exists {
			policy, ok := rawPolicy.(string)
			if !ok || (policy != string(configv1.NeverContactSource) && policy != string(configv1.AllowContactingSource)) {
				return nil, fmt.Errorf("liveISO.imageDigestSources[%d].mirrorSourcePolicy is invalid", index)
			}
			sourceEntry.MirrorSourcePolicy = policy
		}
		sources = append(sources, sourceEntry)
	}
	return normalizeSeedGenerationImageDigestSources(sources), nil
}

func (t *provisioningRequestReconcilerTask) hubSeedGenerationImageDigestSources(
	ctx context.Context,
) ([]seedGenerationImageDigestSource, error) {
	imageDigestMirrorSets := &configv1.ImageDigestMirrorSetList{}
	if err := t.client.List(ctx, imageDigestMirrorSets); err != nil {
		return nil, fmt.Errorf("failed to list hub ImageDigestMirrorSets: %w", err)
	}
	for i := range imageDigestMirrorSets.Items {
		if imageDigestMirrorSets.Items[i].DeletionTimestamp != nil {
			return nil, fmt.Errorf("hub ImageDigestMirrorSet %s is being deleted", imageDigestMirrorSets.Items[i].Name)
		}
	}
	imageContentSourcePolicies := &unstructured.UnstructuredList{}
	imageContentSourcePolicies.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "operator.openshift.io", Version: "v1alpha1", Kind: "ImageContentSourcePolicyList",
	})
	if err := t.client.List(ctx, imageContentSourcePolicies); err != nil {
		return nil, fmt.Errorf("failed to list hub ImageContentSourcePolicies: %w", err)
	}
	for i := range imageContentSourcePolicies.Items {
		if imageContentSourcePolicies.Items[i].GetDeletionTimestamp() != nil {
			return nil, fmt.Errorf("hub ImageContentSourcePolicy %s is being deleted", imageContentSourcePolicies.Items[i].GetName())
		}
	}
	sources := make([]seedGenerationImageDigestSource, 0)
	for i := range imageDigestMirrorSets.Items {
		for _, source := range imageDigestMirrorSets.Items[i].Spec.ImageDigestMirrors {
			mirrors := make([]string, 0, len(source.Mirrors))
			for _, mirror := range source.Mirrors {
				mirrors = append(mirrors, string(mirror))
			}
			sources = append(sources, seedGenerationImageDigestSource{
				Source: source.Source, Mirrors: mirrors, MirrorSourcePolicy: string(source.MirrorSourcePolicy),
			})
		}
	}
	for i := range imageContentSourcePolicies.Items {
		mirrorEntries, found, err := unstructured.NestedSlice(imageContentSourcePolicies.Items[i].Object,
			"spec", "repositoryDigestMirrors")
		if err != nil {
			return nil, fmt.Errorf("failed to read ImageContentSourcePolicy repositoryDigestMirrors: %w", err)
		}
		if !found {
			continue
		}
		for _, rawEntry := range mirrorEntries {
			entry, ok := rawEntry.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("hub ImageContentSourcePolicy contains an invalid repositoryDigestMirrors entry")
			}
			source, found, err := unstructured.NestedString(entry, "source")
			if err != nil || !found || strings.TrimSpace(source) == "" {
				return nil, fmt.Errorf("hub ImageContentSourcePolicy contains an invalid repositoryDigestMirrors source")
			}
			mirrors, found, err := unstructured.NestedStringSlice(entry, "mirrors")
			if err != nil || !found {
				return nil, fmt.Errorf("hub ImageContentSourcePolicy contains invalid repositoryDigestMirrors for %q", source)
			}
			sources = append(sources, seedGenerationImageDigestSource{Source: source, Mirrors: mirrors})
		}
	}
	return normalizeSeedGenerationImageDigestSources(sources), nil
}

func normalizeSeedGenerationImageDigestSources(
	sources []seedGenerationImageDigestSource,
) []seedGenerationImageDigestSource {
	bySource := make(map[string]*seedGenerationImageDigestSource, len(sources))
	for _, entry := range sources {
		source := strings.TrimSuffix(strings.TrimSpace(entry.Source), "/")
		if source == "" {
			continue
		}
		merged, exists := bySource[source]
		if !exists {
			merged = &seedGenerationImageDigestSource{Source: source}
			bySource[source] = merged
		}
		if entry.MirrorSourcePolicy == string(configv1.NeverContactSource) {
			merged.MirrorSourcePolicy = entry.MirrorSourcePolicy
		} else if merged.MirrorSourcePolicy == "" {
			merged.MirrorSourcePolicy = entry.MirrorSourcePolicy
		}
		for _, mirror := range entry.Mirrors {
			mirror = strings.TrimSuffix(strings.TrimSpace(mirror), "/")
			if mirror != "" && !containsString(merged.Mirrors, mirror) {
				merged.Mirrors = append(merged.Mirrors, mirror)
			}
		}
	}
	keys := make([]string, 0, len(bySource))
	for source := range bySource {
		keys = append(keys, source)
	}
	sort.Strings(keys)
	normalized := make([]seedGenerationImageDigestSource, 0, len(keys))
	for _, source := range keys {
		normalized = append(normalized, *bySource[source])
	}
	return normalized
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func (t *provisioningRequestReconcilerTask) seedGenerationRegistryTrust(
	ctx context.Context,
	clusterTemplate *provisioningv1alpha1.ClusterTemplate,
	liveISO map[string]any,
) (map[string]string, error) {
	var configMap *corev1.ConfigMap
	if raw, exists := liveISO["additionalTrustBundleConfigMapRef"]; exists {
		reference, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("liveISO.additionalTrustBundleConfigMapRef must be an object")
		}
		name, err := seedGenerationString(reference, "name")
		if err != nil {
			return nil, fmt.Errorf("liveISO.additionalTrustBundleConfigMapRef requires name")
		}
		configMap = &corev1.ConfigMap{}
		key := types.NamespacedName{Name: name, Namespace: clusterTemplate.Namespace}
		if err := t.client.Get(ctx, key, configMap); err != nil {
			return nil, fmt.Errorf("failed to get additional trust ConfigMap %s/%s: %w", key.Namespace, key.Name, err)
		}
		if configMap.DeletionTimestamp != nil {
			return nil, fmt.Errorf("additional trust ConfigMap %s/%s is being deleted", key.Namespace, key.Name)
		}
	} else {
		imageConfig := &configv1.Image{}
		if err := t.client.Get(ctx, types.NamespacedName{Name: "cluster"}, imageConfig); err != nil {
			if apierrors.IsNotFound(err) {
				return map[string]string{}, nil
			}
			return nil, fmt.Errorf("failed to get hub image config: %w", err)
		}
		if imageConfig.DeletionTimestamp != nil {
			return nil, fmt.Errorf("hub image config is being deleted")
		}
		if imageConfig.Spec.AdditionalTrustedCA.Name == "" {
			return map[string]string{}, nil
		}
		configMap = &corev1.ConfigMap{}
		key := types.NamespacedName{Name: imageConfig.Spec.AdditionalTrustedCA.Name, Namespace: "openshift-config"}
		if err := t.client.Get(ctx, key, configMap); err != nil {
			return nil, fmt.Errorf("failed to get hub additional trusted CA ConfigMap %s/%s: %w", key.Namespace, key.Name, err)
		}
		if configMap.DeletionTimestamp != nil {
			return nil, fmt.Errorf("hub additional trusted CA ConfigMap %s/%s is being deleted", key.Namespace, key.Name)
		}
	}
	trustedCAs := make(map[string]string, len(configMap.Data)+len(configMap.BinaryData))
	for registry, bundle := range configMap.Data {
		trustedCAs[normalizeSeedGenerationRegistryTrustHost(registry)] = bundle
	}
	for registry, bundle := range configMap.BinaryData {
		trustedCAs[normalizeSeedGenerationRegistryTrustHost(registry)] = string(bundle)
	}
	for registry, bundle := range trustedCAs {
		if !validRegistryCertificateHost(registry) {
			return nil, fmt.Errorf("additional trusted CA ConfigMap has invalid registry key %q", registry)
		}
		if err := validateSeedGenerationCertificateBundle([]byte(bundle)); err != nil {
			return nil, fmt.Errorf("additional trusted CA for %s is invalid: %w", registry, err)
		}
	}
	return trustedCAs, nil
}

func normalizeSeedGenerationRegistryTrustHost(registry string) string {
	return normalizeRegistryHost(strings.Replace(registry, "..", ":", 1))
}

func validRegistryCertificateHost(value string) bool {
	return value != "" && !strings.ContainsAny(value, "/\\?#@ \t\r\n") &&
		!strings.Contains(value, "..") && !strings.HasPrefix(value, "*")
}

func validateSeedGenerationCertificateBundle(bundle []byte) error {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(bundle) {
		return fmt.Errorf("no PEM certificates found")
	}
	return nil
}

func seedGenerationImageHasDigest(image string) bool {
	return strings.Contains(image, "@sha256:")
}

func seedGenerationImageHasTag(image string) bool {
	return !seedGenerationImageHasDigest(image)
}

func hasDigestMirrorForImage(image string, sources []seedGenerationImageDigestSource) bool {
	registry, repository, err := imageRepositoryFromPullSpec(image)
	if err != nil {
		return false
	}
	for _, source := range sources {
		if len(source.Mirrors) > 0 && seedGenerationMirrorSourceMatches(source.Source, registry, repository) {
			return true
		}
	}
	return false
}

func seedGenerationMirrorSourceMatches(source, registry, repository string) bool {
	sourceHost, sourceRepository, hasRepository := strings.Cut(strings.ToLower(strings.TrimSuffix(source, "/")), "/")
	registry = normalizeRegistryHost(registry)
	if strings.HasPrefix(sourceHost, "*.") {
		suffix := strings.TrimPrefix(sourceHost, "*")
		if !strings.HasSuffix(registry, suffix) || registry == strings.TrimPrefix(suffix, ".") {
			return false
		}
	} else if normalizeRegistryHost(sourceHost) != registry {
		return false
	}
	if !hasRepository || sourceRepository == "" {
		return true
	}
	return repository == sourceRepository || strings.HasPrefix(repository, sourceRepository+"/")
}

func resolveSeedGenerationImageTagMirror(
	image string,
	sets []configv1.ImageTagMirrorSet,
) (string, bool, error) {
	registry, repository, err := imageRepositoryFromPullSpec(image)
	if err != nil {
		return "", false, err
	}
	var candidates []configv1.ImageTagMirrors
	longest := -1
	for i := range sets {
		for _, source := range sets[i].Spec.ImageTagMirrors {
			if !seedGenerationMirrorSourceMatches(source.Source, registry, repository) {
				continue
			}
			if len(source.Source) > longest {
				candidates = candidates[:0]
				longest = len(source.Source)
			}
			if len(source.Source) == longest {
				candidates = append(candidates, source)
			}
		}
	}
	if len(candidates) == 0 {
		return image, false, nil
	}
	var mirrors []string
	for _, candidate := range candidates {
		for _, mirror := range candidate.Mirrors {
			mirrors = append(mirrors, string(mirror))
		}
	}
	if len(mirrors) == 0 {
		return image, false, fmt.Errorf("hub ImageTagMirrorSet has no mirror for releaseImage %q", image)
	}
	_, sourceRepository, _ := strings.Cut(strings.ToLower(strings.TrimSuffix(candidates[0].Source, "/")), "/")
	suffix := "/" + repository
	if sourceRepository != "" && repository != sourceRepository {
		suffix = strings.TrimPrefix(repository, sourceRepository)
	}
	if sourceRepository == repository {
		suffix = ""
	}
	imageSuffix := seedGenerationImageTagSuffix(image)
	resolved := strings.TrimSuffix(mirrors[0], "/") + suffix + imageSuffix
	if _, _, err := imageRepositoryFromPullSpec(resolved); err != nil {
		return "", false, fmt.Errorf("image tag mirror set resolved releaseImage to an invalid reference: %w", err)
	}
	return resolved, true, nil
}

func seedGenerationImageTagSuffix(image string) string {
	image = strings.TrimPrefix(image, "docker://")
	if digestIndex := strings.LastIndexByte(image, '@'); digestIndex >= 0 {
		return image[digestIndex:]
	}
	lastSlash := strings.LastIndexByte(image, '/')
	if tagIndex := strings.LastIndexByte(image, ':'); tagIndex > lastSlash {
		return image[tagIndex:]
	}
	return ":latest"
}
