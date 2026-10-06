/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/provisioning"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	k8sptr "k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	seedGenerationSnapshotLabel      = "clcm.openshift.io/seed-generation-snapshot"
	seedGenerationSnapshotKindLabel  = "clcm.openshift.io/seed-generation-snapshot-kind"
	seedGenerationSnapshotConfig     = "seed-generation-inputs.json"
	seedGenerationSnapshotConfigKind = "config"
	seedGenerationSnapshotSecretKind = "credential"
	seedGenerationSnapshotToolKind   = "tooling"
	seedGenerationAuthSecretKey      = "seedAuth"
	seedGenerationSnapshotMirrorFile = "idms.yaml"
	seedGenerationRunLabel           = "clcm.openshift.io/seed-generation"
	seedGenerationTLSTimeout         = 10 * time.Second
	dockerHubRegistry                = "docker.io"
)

var errIncompleteSeedGenerationSnapshot = errors.New("incomplete seed generation input snapshot")
var errSeedGenerationSnapshotNotFound = errors.New("seed generation input snapshot not found")
var errSeedGenerationSnapshotLost = errors.New("seed generation input snapshot was lost after it was pinned")

type seedGenerationTransientError struct {
	cause error
}

func (e *seedGenerationTransientError) Error() string { return e.cause.Error() }
func (e *seedGenerationTransientError) Unwrap() error { return e.cause }

func markSeedGenerationTransient(err error) error {
	if err == nil {
		return nil
	}
	return &seedGenerationTransientError{cause: err}
}

func seedGenerationHTTPError(status int, format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	if status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= http.StatusInternalServerError {
		return markSeedGenerationTransient(err)
	}
	return err
}

func isSeedGenerationTransientError(err error) bool {
	var transientErr *seedGenerationTransientError
	if errors.As(err, &transientErr) || apierrors.IsConflict(err) || apierrors.IsInternalError(err) ||
		apierrors.IsServiceUnavailable(err) || apierrors.IsServerTimeout(err) || apierrors.IsTimeout(err) ||
		apierrors.IsTooManyRequests(err) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return dnsErr.IsTimeout || dnsErr.IsTemporary
	}
	var networkErr net.Error
	return errors.As(err, &networkErr)
}

// seedGenerationInputDocument contains only non-secret inputs. Credential
// bytes are stored in separate immutable Secret objects named in this document.
type seedGenerationInputDocument struct {
	ClusterTemplateName      string                            `json:"clusterTemplateName"`
	ClusterTemplateNamespace string                            `json:"clusterTemplateNamespace"`
	ClusterTemplateUID       types.UID                         `json:"clusterTemplateUID,omitempty"`
	Release                  string                            `json:"release"`
	SeedGeneration           map[string]any                    `json:"seedGeneration"`
	ResolvedReleaseImage     string                            `json:"resolvedReleaseImage,omitempty"`
	ImageDigestSources       []seedGenerationImageDigestSource `json:"imageDigestSources,omitempty"`
	RegistryTrustedCAs       map[string]string                 `json:"registryTrustedCAs,omitempty"`
	RegistryTrustedCAKeys    map[string]string                 `json:"registryTrustedCAKeys,omitempty"`
	CredentialSecretNames    map[string]string                 `json:"credentialSecretNames"`
}

type seedGenerationImageDigestSource struct {
	Source             string   `json:"source"`
	Mirrors            []string `json:"mirrors"`
	MirrorSourcePolicy string   `json:"mirrorSourcePolicy,omitempty"`
}

// seedGenerationInputSnapshot is the frozen, hub-side input set for one PR.
// Object UIDs are recorded in ProvisioningRequest status after all resources
// have been created or adopted.
type seedGenerationInputSnapshot struct {
	Document  *seedGenerationInputDocument
	ConfigMap *corev1.ConfigMap
	Secrets   map[string]*corev1.Secret
}

func seedGenerationSnapshotNamespace() string {
	return ctlrutils.GetEnvOrDefault(constants.DefaultNamespaceEnvName, constants.DefaultNamespace)
}

func seedGenerationSnapshotNames(pr *provisioningv1alpha1.ProvisioningRequest) map[string]string {
	return map[string]string{
		"config":     seedGenerationResourceName(pr, "inputs"),
		"seedAuth":   seedGenerationResourceName(pr, "seed-auth"),
		"pullSecret": seedGenerationResourceName(pr, "pull-secret"),
		"upload":     seedGenerationResourceName(pr, "upload"),
	}
}

func seedGenerationResourceName(pr *provisioningv1alpha1.ProvisioningRequest, suffix string) string {
	name := fmt.Sprintf("%s-seedgen-%s", pr.Name, suffix)
	if len(name) <= 63 {
		return name
	}
	// ProvisioningRequest names are normally UUIDs. Keep deterministic names
	// valid for longer names while retaining a stable suffix for ownership.
	hashSum := sha256.Sum256([]byte(pr.Name))
	hash := hex.EncodeToString(hashSum[:])
	if len(hash) > 8 {
		hash = hash[:8]
	}
	maxPrefix := 63 - len(suffix) - len("-sg-") - len(hash) - 1
	if maxPrefix < 1 {
		maxPrefix = 1
	}
	prefix := pr.Name
	if len(prefix) > maxPrefix {
		prefix = prefix[:maxPrefix]
	}
	return fmt.Sprintf("%s-sg-%s-%s", prefix, hash, suffix)
}

func seedGenerationSnapshotLabels(pr *provisioningv1alpha1.ProvisioningRequest, kind string) map[string]string {
	return map[string]string{
		provisioningv1alpha1.ProvisioningRequestNameLabel: pr.Name,
		provisioningv1alpha1.ProvisioningRequestUIDLabel:  string(pr.UID),
		seedGenerationSnapshotLabel:                       "true",
		seedGenerationSnapshotKindLabel:                   kind,
		seedGenerationRunLabel:                            "true",
	}
}

func seedGenerationSnapshotOwnerReference(pr *provisioningv1alpha1.ProvisioningRequest) metav1.OwnerReference {
	controller := false
	blockOwnerDeletion := false
	return metav1.OwnerReference{
		APIVersion:         provisioningv1alpha1.GroupVersion.String(),
		Kind:               "ProvisioningRequest",
		Name:               pr.Name,
		UID:                pr.UID,
		Controller:         &controller,
		BlockOwnerDeletion: &blockOwnerDeletion,
	}
}

func hasSeedGenerationSnapshotOwner(resource client.Object, pr *provisioningv1alpha1.ProvisioningRequest) bool {
	for _, ref := range resource.GetOwnerReferences() {
		if ref.APIVersion == provisioningv1alpha1.GroupVersion.String() &&
			ref.Kind == "ProvisioningRequest" && ref.Name == pr.Name && ref.UID == pr.UID {
			return true
		}
	}
	return false
}

func validateSeedGenerationSnapshotOwner(
	resource client.Object,
	pr *provisioningv1alpha1.ProvisioningRequest,
	kind string,
	statusUIDs map[string]string,
) error {
	labels := resource.GetLabels()
	if labels[provisioningv1alpha1.ProvisioningRequestNameLabel] != pr.Name ||
		labels[provisioningv1alpha1.ProvisioningRequestUIDLabel] != string(pr.UID) ||
		labels[seedGenerationSnapshotLabel] != "true" ||
		labels[seedGenerationSnapshotKindLabel] != kind ||
		!hasSeedGenerationSnapshotOwner(resource, pr) {
		return fmt.Errorf("seed generation snapshot resource %s/%s is not owned by ProvisioningRequest %s with UID %s",
			resource.GetNamespace(), resource.GetName(), pr.Name, pr.UID)
	}
	if len(statusUIDs) > 0 {
		uid, ok := statusUIDs[resource.GetName()]
		if !ok || uid != string(resource.GetUID()) {
			return fmt.Errorf("seed generation snapshot resource %s/%s UID does not match ProvisioningRequest status",
				resource.GetNamespace(), resource.GetName())
		}
	}
	if labels[seedGenerationRunLabel] != "true" {
		return fmt.Errorf("seed generation resource %s/%s is missing its run label", resource.GetNamespace(), resource.GetName())
	}
	return nil
}

func seedGenerationInputUIDs(snapshot *seedGenerationInputSnapshot) map[string]string {
	uids := map[string]string{snapshot.ConfigMap.Name: string(snapshot.ConfigMap.UID)}
	for _, secret := range snapshot.Secrets {
		uids[secret.Name] = string(secret.UID)
	}
	return uids
}

func seedGenerationSnapshotStatusUIDs(pr *provisioningv1alpha1.ProvisioningRequest) map[string]string {
	if pr.Status.Extensions.ClusterDetails == nil || pr.Status.Extensions.ClusterDetails.SeedGenerationStatus == nil {
		return nil
	}
	return pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.InputSnapshotResourceUIDs
}

// ensureSeedGenerationInputSnapshot adopts a complete frozen snapshot or
// creates one from input bytes read and validated during the same reconcile.
// It returns retry=true after deleting an incomplete snapshot so a later
// reconcile can rebuild it from current source objects.
func (t *provisioningRequestReconcilerTask) ensureSeedGenerationInputSnapshot(
	ctx context.Context,
	clusterTemplate *provisioningv1alpha1.ClusterTemplate,
) (*seedGenerationInputSnapshot, bool, error) {
	statusUIDs := seedGenerationSnapshotStatusUIDs(t.object)
	if t.object.Status.Extensions.ClusterDetails == nil ||
		t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus == nil ||
		t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.StartedAt == nil {
		return nil, false, fmt.Errorf("seed generation must be started before snapshot creation")
	}
	snapshot, err := t.loadSeedGenerationInputSnapshot(ctx)
	if errors.Is(err, errIncompleteSeedGenerationSnapshot) {
		if len(statusUIDs) > 0 || t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.DetachmentStarted {
			return nil, false, fmt.Errorf("%w: %w", errSeedGenerationSnapshotLost, err)
		}
		return nil, true, nil
	}
	if errors.Is(err, errSeedGenerationSnapshotNotFound) {
		if len(statusUIDs) > 0 || t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.DetachmentStarted {
			return nil, false, errSeedGenerationSnapshotLost
		}
		snapshot, err = nil, nil
	}
	if err != nil {
		return nil, false, err
	}
	if snapshot == nil {
		if clusterTemplate == nil {
			var err error
			clusterTemplate, err = provisioning.GetClusterTemplateRef(ctx, t.client, t.object)
			if err != nil {
				return nil, false, fmt.Errorf("failed to get ClusterTemplate for seed generation inputs: %w", err)
			}
			if clusterTemplate.DeletionTimestamp != nil {
				return nil, false, fmt.Errorf("cluster template %s/%s is being deleted", clusterTemplate.Namespace, clusterTemplate.Name)
			}
		}
		document, credentials, err := t.resolveSeedGenerationInputSnapshot(ctx, clusterTemplate)
		if err != nil {
			return nil, false, err
		}
		snapshot, err = t.createSeedGenerationInputSnapshot(ctx, document, credentials)
		if errors.Is(err, errIncompleteSeedGenerationSnapshot) {
			return nil, true, nil
		}
		if err != nil {
			return nil, false, err
		}
	}
	actualUIDs := seedGenerationInputUIDs(snapshot)
	if len(statusUIDs) > 0 && !seedGenerationSnapshotUIDsEqual(statusUIDs, actualUIDs) {
		return nil, false, fmt.Errorf("seed generation snapshot UIDs do not match ProvisioningRequest status")
	}
	if len(statusUIDs) == 0 || !seedGenerationSnapshotUIDsEqual(statusUIDs, actualUIDs) {
		t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.InputSnapshotResourceUIDs = actualUIDs
		if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
			return nil, false, fmt.Errorf("failed to record seed generation snapshot UIDs: %w", err)
		}
	}
	return snapshot, false, nil
}

func seedGenerationSnapshotUIDsEqual(statusUIDs, actualUIDs map[string]string) bool {
	if len(statusUIDs) != len(actualUIDs) {
		return false
	}
	for name, uid := range actualUIDs {
		if statusUIDs[name] != uid {
			return false
		}
	}
	return true
}

func makeSeedGenerationSnapshotObjects(
	pr *provisioningv1alpha1.ProvisioningRequest,
	document *seedGenerationInputDocument,
	credentials map[string]map[string][]byte,
) (*corev1.ConfigMap, map[string]*corev1.Secret, error) {
	if pr == nil || pr.UID == "" || pr.Name == "" {
		return nil, nil, fmt.Errorf("provisioning request name and UID are required for a seed generation snapshot")
	}
	if document == nil || document.SeedGeneration == nil {
		return nil, nil, fmt.Errorf("seed generation input document is incomplete")
	}
	names := seedGenerationSnapshotNames(pr)
	secretNames := make(map[string]string, len(credentials))
	secrets := make(map[string]*corev1.Secret, len(credentials))
	for key, data := range credentials {
		name, ok := names[key]
		if !ok || key == "config" {
			return nil, nil, fmt.Errorf("unsupported seed generation credential snapshot %q", key)
		}
		if len(data) == 0 {
			return nil, nil, fmt.Errorf("seed generation credential snapshot %q is empty", key)
		}
		secretNames[key] = name
		secrets[key] = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:            name,
				Namespace:       seedGenerationSnapshotNamespace(),
				Labels:          seedGenerationSnapshotLabels(pr, seedGenerationSnapshotSecretKind),
				OwnerReferences: []metav1.OwnerReference{seedGenerationSnapshotOwnerReference(pr)},
			},
			Immutable: k8sptr.To(true),
			Type:      corev1.SecretTypeOpaque,
			Data:      cloneByteMap(data),
		}
	}
	if _, ok := secretNames["seedAuth"]; !ok {
		return nil, nil, fmt.Errorf("seed generation snapshot is missing seedAuth credentials")
	}
	document.CredentialSecretNames = secretNames
	if document.ImageDigestSources == nil {
		document.ImageDigestSources = []seedGenerationImageDigestSource{}
	}
	caRegistries := make([]string, 0, len(document.RegistryTrustedCAs))
	for registry := range document.RegistryTrustedCAs {
		caRegistries = append(caRegistries, registry)
	}
	sort.Strings(caRegistries)
	document.RegistryTrustedCAKeys = make(map[string]string, len(caRegistries))
	configData := make(map[string]string, len(caRegistries)+2)
	for index, registry := range caRegistries {
		key := fmt.Sprintf("registry-ca-%d", index)
		document.RegistryTrustedCAKeys[registry] = key
		configData[key] = document.RegistryTrustedCAs[registry]
	}
	mirrorConfig, err := seedGenerationMirrorConfigYAML(document.ImageDigestSources)
	if err != nil {
		return nil, nil, err
	}
	configData[seedGenerationSnapshotMirrorFile] = mirrorConfig
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to encode seed generation input document: %w", err)
	}
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names["config"],
			Namespace:       seedGenerationSnapshotNamespace(),
			Labels:          seedGenerationSnapshotLabels(pr, seedGenerationSnapshotConfigKind),
			OwnerReferences: []metav1.OwnerReference{seedGenerationSnapshotOwnerReference(pr)},
		},
		Immutable: k8sptr.To(true),
		Data:      configData,
	}
	configMap.Data[seedGenerationSnapshotConfig] = string(encoded)
	return configMap, secrets, nil
}

func cloneByteMap(in map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(in))
	for key, value := range in {
		out[key] = append([]byte(nil), value...)
	}
	return out
}

func validateSeedGenerationSnapshotObjectsNotDeleting(configMaps []corev1.ConfigMap, secrets []corev1.Secret) error {
	for i := range configMaps {
		if configMaps[i].DeletionTimestamp != nil {
			return fmt.Errorf("seed generation input ConfigMap %s is being deleted", configMaps[i].Name)
		}
	}
	for i := range secrets {
		if secrets[i].DeletionTimestamp != nil {
			return fmt.Errorf("seed generation input Secret %s is being deleted", secrets[i].Name)
		}
	}
	return nil
}

func (t *provisioningRequestReconcilerTask) loadSeedGenerationInputSnapshot(
	ctx context.Context,
) (*seedGenerationInputSnapshot, error) {
	pr := t.object
	namespace := seedGenerationSnapshotNamespace()
	selector := labels.Set{
		provisioningv1alpha1.ProvisioningRequestNameLabel: pr.Name,
		provisioningv1alpha1.ProvisioningRequestUIDLabel:  string(pr.UID),
		seedGenerationSnapshotLabel:                       "true",
	}.AsSelector()
	configMaps := &corev1.ConfigMapList{}
	if err := t.client.List(ctx, configMaps, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, fmt.Errorf("failed to list seed generation input ConfigMaps: %w", err)
	}
	secrets := &corev1.SecretList{}
	if err := t.client.List(ctx, secrets, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, fmt.Errorf("failed to list seed generation input Secrets: %w", err)
	}
	if len(configMaps.Items) == 0 && len(secrets.Items) == 0 {
		return nil, errSeedGenerationSnapshotNotFound
	}
	if err := validateSeedGenerationSnapshotObjectsNotDeleting(configMaps.Items, secrets.Items); err != nil {
		return nil, err
	}
	if len(configMaps.Items) != 1 {
		return nil, t.removeIncompleteSeedGenerationSnapshot(ctx, configMaps.Items, secrets.Items)
	}
	configMap := configMaps.Items[0].DeepCopy()
	statusUIDs := seedGenerationSnapshotStatusUIDs(pr)
	if err := validateSeedGenerationSnapshotOwner(configMap, pr, seedGenerationSnapshotConfigKind, statusUIDs); err != nil {
		return nil, err
	}
	if configMap.Name != seedGenerationResourceName(pr, "inputs") {
		return nil, fmt.Errorf("seed generation input ConfigMap %s does not have the expected deterministic name", configMap.Name)
	}
	encoded, ok := configMap.Data[seedGenerationSnapshotConfig]
	if !ok {
		return nil, fmt.Errorf("seed generation input ConfigMap %s is missing its configuration document", configMap.Name)
	}
	document := &seedGenerationInputDocument{}
	if err := json.Unmarshal([]byte(encoded), document); err != nil {
		return nil, fmt.Errorf("seed generation input ConfigMap %s contains invalid JSON: %w", configMap.Name, err)
	}
	if document.SeedGeneration == nil || document.ClusterTemplateName == "" || document.ClusterTemplateNamespace == "" {
		return nil, fmt.Errorf("seed generation input ConfigMap %s contains an incomplete document", configMap.Name)
	}
	resolvedSecrets, err := t.resolveSeedGenerationSnapshotCredentials(ctx, configMap, document, secrets.Items, statusUIDs)
	if err != nil {
		return nil, err
	}
	if configMap.Immutable == nil || !*configMap.Immutable {
		return nil, fmt.Errorf("seed generation input ConfigMap %s is not immutable", configMap.Name)
	}
	if len(statusUIDs) > 0 && len(statusUIDs) != 1+len(resolvedSecrets) {
		return nil, fmt.Errorf("seed generation snapshot UID set does not match its resources")
	}
	return &seedGenerationInputSnapshot{Document: document, ConfigMap: configMap, Secrets: resolvedSecrets}, nil
}

func (t *provisioningRequestReconcilerTask) resolveSeedGenerationSnapshotCredentials(
	ctx context.Context,
	configMap *corev1.ConfigMap,
	document *seedGenerationInputDocument,
	secretItems []corev1.Secret,
	statusUIDs map[string]string,
) (map[string]*corev1.Secret, error) {
	pr := t.object
	if len(document.CredentialSecretNames) == 0 {
		return nil, t.removeIncompleteSeedGenerationSnapshot(ctx, []corev1.ConfigMap{*configMap}, secretItems)
	}
	names := seedGenerationSnapshotNames(pr)
	for key, name := range document.CredentialSecretNames {
		if names[key] != name || key == "config" {
			return nil, fmt.Errorf("seed generation input ConfigMap %s contains an invalid credential reference", configMap.Name)
		}
	}
	if _, ok := document.CredentialSecretNames["seedAuth"]; !ok {
		return nil, t.removeIncompleteSeedGenerationSnapshot(ctx, []corev1.ConfigMap{*configMap}, secretItems)
	}
	secretByName := make(map[string]*corev1.Secret, len(secretItems))
	for i := range secretItems {
		secretByName[secretItems[i].Name] = secretItems[i].DeepCopy()
	}
	if len(secretByName) != len(document.CredentialSecretNames) {
		return nil, t.removeIncompleteSeedGenerationSnapshot(ctx, []corev1.ConfigMap{*configMap}, secretItems)
	}
	resolvedSecrets := make(map[string]*corev1.Secret, len(document.CredentialSecretNames))
	for key, name := range document.CredentialSecretNames {
		secret, ok := secretByName[name]
		if !ok {
			return nil, t.removeIncompleteSeedGenerationSnapshot(ctx, []corev1.ConfigMap{*configMap}, secretItems)
		}
		if err := validateSeedGenerationSnapshotOwner(secret, pr, seedGenerationSnapshotSecretKind, statusUIDs); err != nil {
			return nil, err
		}
		if secret.Immutable == nil || !*secret.Immutable {
			return nil, fmt.Errorf("seed generation input Secret %s is not immutable", secret.Name)
		}
		if len(secret.Data) == 0 {
			return nil, fmt.Errorf("seed generation input Secret %s has no data", secret.Name)
		}
		resolvedSecrets[key] = secret
	}
	return resolvedSecrets, nil
}

func (t *provisioningRequestReconcilerTask) removeIncompleteSeedGenerationSnapshot(
	ctx context.Context,
	configMaps []corev1.ConfigMap,
	secrets []corev1.Secret,
) error {
	pr := t.object
	for i := range configMaps {
		resource := &configMaps[i]
		if err := validateSeedGenerationSnapshotOwner(resource, pr, seedGenerationSnapshotConfigKind,
			seedGenerationSnapshotStatusUIDs(pr)); err != nil {
			return err
		}
	}
	for i := range secrets {
		resource := &secrets[i]
		if err := validateSeedGenerationSnapshotOwner(resource, pr, seedGenerationSnapshotSecretKind,
			seedGenerationSnapshotStatusUIDs(pr)); err != nil {
			return err
		}
	}
	for i := range configMaps {
		if err := client.IgnoreNotFound(t.client.Delete(ctx, &configMaps[i])); err != nil {
			return fmt.Errorf("failed to remove incomplete seed generation input ConfigMap %s: %w", configMaps[i].Name, err)
		}
	}
	for i := range secrets {
		if err := client.IgnoreNotFound(t.client.Delete(ctx, &secrets[i])); err != nil {
			return fmt.Errorf("failed to remove incomplete seed generation input Secret %s: %w", secrets[i].Name, err)
		}
	}
	return errIncompleteSeedGenerationSnapshot
}

func (t *provisioningRequestReconcilerTask) createSeedGenerationInputSnapshot(
	ctx context.Context,
	document *seedGenerationInputDocument,
	credentials map[string]map[string][]byte,
) (*seedGenerationInputSnapshot, error) {
	existing, err := t.loadSeedGenerationInputSnapshot(ctx)
	if err == nil && existing != nil {
		return existing, nil
	}
	if !errors.Is(err, errSeedGenerationSnapshotNotFound) {
		return nil, err
	}
	configMap, secrets, err := makeSeedGenerationSnapshotObjects(t.object, document, credentials)
	if err != nil {
		return nil, err
	}
	created := make([]client.Object, 0, len(secrets)+1)
	if err := t.client.Create(ctx, configMap); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return t.loadSeedGenerationInputSnapshot(ctx)
		}
		return nil, fmt.Errorf("failed to create seed generation input ConfigMap: %w", err)
	}
	created = append(created, configMap)
	keys := make([]string, 0, len(secrets))
	for key := range secrets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		secret := secrets[key]
		if err := t.client.Create(ctx, secret); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return nil, t.rollbackSeedGenerationSnapshot(ctx, created,
					fmt.Errorf("seed generation input Secret %s already exists without a complete snapshot", secret.Name))
			}
			return nil, t.rollbackSeedGenerationSnapshot(ctx, created,
				fmt.Errorf("failed to create seed generation input Secret %s: %w", secret.Name, err))
		}
		created = append(created, secret)
	}
	snapshot, err := t.loadSeedGenerationInputSnapshot(ctx)
	if err != nil && !errors.Is(err, errSeedGenerationSnapshotNotFound) {
		return nil, t.rollbackSeedGenerationSnapshot(ctx, created, err)
	}
	if snapshot == nil || err != nil {
		return nil, t.rollbackSeedGenerationSnapshot(ctx, created,
			fmt.Errorf("seed generation input snapshot was not visible after creation"))
	}
	return snapshot, nil
}

func (t *provisioningRequestReconcilerTask) rollbackSeedGenerationSnapshot(
	ctx context.Context,
	created []client.Object,
	cause error,
) error {
	for i := len(created) - 1; i >= 0; i-- {
		if err := client.IgnoreNotFound(t.client.Delete(ctx, created[i])); err != nil {
			cause = errors.Join(cause, fmt.Errorf("failed to remove partial snapshot resource %s: %w", created[i].GetName(), err))
		}
	}
	return fmt.Errorf("seed generation snapshot rollback failed: %w", cause)
}

func seedGenerationSnapshotInputDocument(
	clusterTemplate *provisioningv1alpha1.ClusterTemplate,
	seedGeneration map[string]any,
) *seedGenerationInputDocument {
	return &seedGenerationInputDocument{
		ClusterTemplateName:      clusterTemplate.Name,
		ClusterTemplateNamespace: clusterTemplate.Namespace,
		ClusterTemplateUID:       clusterTemplate.UID,
		Release:                  clusterTemplate.Spec.Release,
		SeedGeneration:           cloneSeedGenerationMap(seedGeneration),
	}
}

func cloneSeedGenerationMap(source map[string]any) map[string]any {
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = cloneSeedGenerationValue(value)
	}
	return cloned
}

func cloneSeedGenerationValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneSeedGenerationMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for i := range typed {
			cloned[i] = cloneSeedGenerationValue(typed[i])
		}
		return cloned
	default:
		return value
	}
}

func (t *provisioningRequestReconcilerTask) resolveSeedGenerationInputSnapshot(
	ctx context.Context,
	clusterTemplate *provisioningv1alpha1.ClusterTemplate,
) (*seedGenerationInputDocument, map[string]map[string][]byte, error) {
	merged, err := t.mergeAndValidateUpgradeData(clusterTemplate)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve seed generation configuration: %w", err)
	}
	seedGeneration, ok := merged[ctlrutils.UpgradeDefaultsSeedGenerationKey].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("effective seedGeneration configuration is missing")
	}
	document := seedGenerationSnapshotInputDocument(clusterTemplate, seedGeneration)
	credentials := make(map[string]map[string][]byte)

	seedImage, err := seedGenerationString(seedGeneration, "seedImage")
	if err != nil {
		return nil, nil, err
	}
	seedRegistry, err := registryHostFromImage(seedImage)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid seedImage registry: %w", err)
	}
	seedAuthRef, err := seedGenerationSecretReference(seedGeneration, "seedAuthSecretRef")
	if err != nil {
		return nil, nil, err
	}
	seedAuthSource, err := t.getTemplateSecret(ctx, clusterTemplate, seedAuthRef, "seedAuthSecretRef")
	if err != nil {
		return nil, nil, err
	}
	seedAuth := seedAuthSource.Data[seedGenerationAuthSecretKey]
	if len(seedAuth) == 0 {
		return nil, nil, fmt.Errorf("secret %s/%s is missing non-empty %q data", clusterTemplate.Namespace, seedAuthRef, seedGenerationAuthSecretKey)
	}
	if err := validateDockerAuthForRegistry(seedAuth, seedRegistry); err != nil {
		return nil, nil, fmt.Errorf("invalid seed registry credentials for %s: %w", seedRegistry, err)
	}
	if err := validateRegistryPushAccess(ctx, seedImage, seedAuth); err != nil {
		return nil, nil, fmt.Errorf("seed image registry push preflight failed: %w", err)
	}
	credentials["seedAuth"] = map[string][]byte{seedGenerationAuthSecretKey: append([]byte(nil), seedAuth...)}

	liveISO, hasISO := seedGeneration["liveISO"]
	if !hasISO {
		return document, credentials, nil
	}
	liveISOConfig, ok := liveISO.(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("liveISO must be an object")
	}
	if err := t.resolveISOInputs(ctx, clusterTemplate, liveISOConfig, document, seedAuth, credentials); err != nil {
		return nil, nil, err
	}
	return document, credentials, nil
}

func seedGenerationString(config map[string]any, key string) (string, error) {
	value, ok := config[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("seed generation configuration requires a non-empty %s", key)
	}
	return value, nil
}

func seedGenerationSecretReference(config map[string]any, key string) (string, error) {
	ref, ok := config[key].(map[string]any)
	if !ok {
		return "", fmt.Errorf("seed generation configuration requires %s.name", key)
	}
	return seedGenerationString(ref, "name")
}

func (t *provisioningRequestReconcilerTask) getTemplateSecret(
	ctx context.Context,
	clusterTemplate *provisioningv1alpha1.ClusterTemplate,
	name string,
	field string,
) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	key := types.NamespacedName{Name: name, Namespace: clusterTemplate.Namespace}
	if err := t.client.Get(ctx, key, secret); err != nil {
		return nil, fmt.Errorf("failed to get %s Secret %s/%s: %w", field, key.Namespace, key.Name, err)
	}
	if secret.DeletionTimestamp != nil {
		return nil, fmt.Errorf("%s Secret %s/%s is being deleted", field, key.Namespace, key.Name)
	}
	return secret, nil
}

func registryHostFromImage(image string) (string, error) {
	image = strings.TrimSpace(image)
	if strings.Contains(image, "://") {
		parsed, err := url.Parse(image)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "docker" && parsed.Scheme != "oci") {
			return "", fmt.Errorf("unsupported image reference %q", image)
		}
		image = strings.TrimPrefix(image, parsed.Scheme+"://")
	}
	if image == "" || strings.ContainsAny(image, " \t\r\n") {
		return "", fmt.Errorf("invalid image reference")
	}
	first, _, hasPath := strings.Cut(image, "/")
	if !hasPath {
		return dockerHubRegistry, nil
	}
	if first == "localhost" || strings.ContainsAny(first, ".:") {
		return normalizeRegistryHost(first), nil
	}
	return "docker.io", nil
}

func imageRepositoryFromPullSpec(image string) (string, string, error) {
	image = strings.TrimSpace(image)
	if image == "" || strings.ContainsAny(image, " \t\r\n?#") {
		return "", "", fmt.Errorf("image reference is empty or contains invalid characters")
	}
	if strings.Contains(image, "://") {
		parsed, err := url.Parse(image)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "docker" && parsed.Scheme != "oci") {
			return "", "", fmt.Errorf("unsupported image reference %q", image)
		}
		image = strings.TrimPrefix(image, parsed.Scheme+"://")
	}
	first, remainder, hasPath := strings.Cut(image, "/")
	if !hasPath {
		return dockerHubRegistry, "library/" + stripImageTag(first), nil
	}
	if first == "localhost" || strings.ContainsAny(first, ".:") {
		registry := normalizeRegistryHost(first)
		repository := stripImageTag(remainder)
		if registry == "" || repository == "" {
			return "", "", fmt.Errorf("image reference has an empty registry or repository")
		}
		return registry, repository, nil
	}
	return dockerHubRegistry, stripImageTag(image), nil
}

func stripImageTag(image string) string {
	lastSlash := strings.LastIndexByte(image, '/')
	lastColon := strings.LastIndexByte(image, ':')
	lastDigest := strings.LastIndexByte(image, '@')
	if lastDigest > lastSlash {
		return image[:lastDigest]
	}
	if lastColon > lastSlash {
		return image[:lastColon]
	}
	return image
}

func normalizeRegistryHost(registry string) string {
	registry = strings.TrimSpace(strings.ToLower(registry))
	if strings.Contains(registry, "://") {
		parsed, err := url.Parse(registry)
		if err == nil && parsed.Hostname() != "" {
			registry = parsed.Host
		}
	}
	registry = strings.TrimSuffix(registry, "/")
	registry = strings.TrimSuffix(registry, "/v1")
	if registry == "index.docker.io" || registry == "registry-1.docker.io" {
		return dockerHubRegistry
	}
	return registry
}

type dockerAuthFile struct {
	Auths map[string]json.RawMessage `json:"auths"`
}

func validateDockerAuthForRegistry(data []byte, registry string) error {
	_, err := credentialsForRegistry(data, registry)
	return err
}

type registryCredentials struct {
	username string
	password string
	token    string
}

func credentialsForRegistry(data []byte, registry string) (registryCredentials, error) {
	var config dockerAuthFile
	if err := json.Unmarshal(data, &config); err != nil {
		return registryCredentials{}, fmt.Errorf("credential data is not valid Docker auth JSON")
	}
	for key, raw := range config.Auths {
		if normalizeRegistryHost(key) != normalizeRegistryHost(registry) {
			continue
		}
		var auth struct {
			Auth          string `json:"auth"`
			Username      string `json:"username"`
			Password      string `json:"password"`
			IdentityToken string `json:"identitytoken"`
			RegistryToken string `json:"registrytoken"`
		}
		if err := json.Unmarshal(raw, &auth); err != nil {
			return registryCredentials{}, fmt.Errorf("registry auth entry is invalid")
		}
		if auth.Auth != "" && (auth.Username == "" || auth.Password == "") {
			decoded, err := base64.StdEncoding.DecodeString(auth.Auth)
			if err != nil {
				decoded, err = base64.RawStdEncoding.DecodeString(auth.Auth)
			}
			if err != nil {
				return registryCredentials{}, fmt.Errorf("registry auth entry has invalid encoded credentials")
			}
			username, password, ok := strings.Cut(string(decoded), ":")
			if !ok || username == "" || password == "" {
				return registryCredentials{}, fmt.Errorf("registry auth entry has invalid encoded credentials")
			}
			auth.Username, auth.Password = username, password
		}
		if auth.RegistryToken != "" {
			return registryCredentials{token: auth.RegistryToken}, nil
		}
		if auth.IdentityToken != "" {
			return registryCredentials{token: auth.IdentityToken}, nil
		}
		if auth.Username != "" && auth.Password != "" {
			return registryCredentials{username: auth.Username, password: auth.Password}, nil
		}
		return registryCredentials{}, fmt.Errorf("registry auth entry contains no usable credentials")
	}
	return registryCredentials{}, fmt.Errorf("credential data has no auth entry for registry %s", registry)
}

var registryChallengeParam = regexp.MustCompile(`([A-Za-z][A-Za-z0-9_-]*)="([^"]*)"`)

func parseRegistryChallenge(header string) (string, map[string]string, error) {
	parts := strings.Fields(header)
	if len(parts) == 0 {
		return "", nil, fmt.Errorf("registry did not provide an authentication challenge")
	}
	params := make(map[string]string)
	for _, match := range registryChallengeParam.FindAllStringSubmatch(strings.Join(parts[1:], " "), -1) {
		params[strings.ToLower(match[1])] = match[2]
	}
	return strings.ToLower(parts[0]), params, nil
}

func registryToken(ctx context.Context, httpClient *http.Client, challenge map[string]string, scope string, creds registryCredentials) (string, error) {
	realm := challenge["realm"]
	parsed, err := url.Parse(realm)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return "", fmt.Errorf("registry authentication challenge has an invalid HTTPS token realm")
	}
	query := parsed.Query()
	if service := challenge["service"]; service != "" {
		query.Set("service", service)
	}
	query.Set("scope", scope)
	parsed.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create registry token request")
	}
	if creds.username != "" {
		req.SetBasicAuth(creds.username, creds.password)
	} else if creds.token != "" {
		req.Header.Set("Authorization", "Bearer "+creds.token)
	}
	response, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("registry token request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", seedGenerationHTTPError(response.StatusCode,
			"registry token request returned HTTP %d", response.StatusCode)
	}
	var tokenResponse struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&tokenResponse); err != nil {
		return "", fmt.Errorf("registry token response was invalid")
	}
	token := tokenResponse.Token
	if token == "" {
		token = tokenResponse.AccessToken
	}
	if token == "" {
		return "", fmt.Errorf("registry token response did not contain a token")
	}
	return token, nil
}

// validateRegistryPushAccess performs the registry upload handshake without
// uploading a blob. It creates and then cancels an empty upload session so
// credentials are proven to have repository push permission.
func validateRegistryPushAccess(ctx context.Context, image string, authJSON []byte) error {
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) == 0 || len(via) > 5 {
				return http.ErrUseLastResponse
			}
			return fmt.Errorf("registry authentication endpoint redirected the request")
		},
	}
	return validateRegistryPushAccessWithClient(ctx, image, authJSON, client)
}

func validateRegistryPushAccessWithClient(ctx context.Context, image string, authJSON []byte, client *http.Client) error {
	registry, repository, err := imageRepositoryFromPullSpec(image)
	if err != nil {
		return err
	}
	creds, err := credentialsForRegistry(authJSON, registry)
	if err != nil {
		return err
	}
	if strings.ContainsAny(registry, "/?#@") {
		return fmt.Errorf("image reference has an invalid registry host")
	}
	registryEndpoint := registry
	if registryEndpoint == dockerHubRegistry {
		registryEndpoint = "registry-1.docker.io"
	}
	registryURL := "https://" + registryEndpoint + "/v2/" + repository + "/blobs/uploads/"
	upload, err := http.NewRequestWithContext(ctx, http.MethodPost, registryURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create registry access request")
	}
	response, err := client.Do(upload)
	if err != nil {
		return fmt.Errorf("registry %s is not reachable: %w", registry, err)
	}
	if response.StatusCode == http.StatusUnauthorized {
		challengeHeader := response.Header.Get("WWW-Authenticate")
		response.Body.Close()
		scheme, challenge, err := parseRegistryChallenge(challengeHeader)
		if err != nil {
			return fmt.Errorf("registry %s rejected authentication: %w", registry, err)
		}
		switch scheme {
		case "bearer":
			token, err := registryToken(ctx, client, challenge, "repository:"+repository+":push", creds)
			if err != nil {
				return fmt.Errorf("registry %s push authorization failed: %w", registry, err)
			}
			upload, err = http.NewRequestWithContext(ctx, http.MethodPost, registryURL, nil)
			if err != nil {
				return fmt.Errorf("failed to create registry upload request")
			}
			upload.Header.Set("Authorization", "Bearer "+token)
		case "basic":
			if creds.username == "" || creds.password == "" {
				return fmt.Errorf("registry %s requires username and password authentication", registry)
			}
			upload, err = http.NewRequestWithContext(ctx, http.MethodPost, registryURL, nil)
			if err != nil {
				return fmt.Errorf("failed to create registry upload request")
			}
			upload.SetBasicAuth(creds.username, creds.password)
		default:
			return fmt.Errorf("registry %s uses unsupported authentication scheme %q", registry, scheme)
		}
		response, err = client.Do(upload)
		if err != nil {
			return fmt.Errorf("registry %s is not reachable: %w", registry, err)
		}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted && response.StatusCode != http.StatusCreated {
		return seedGenerationHTTPError(response.StatusCode,
			"registry %s did not grant push access (HTTP %d)", registry, response.StatusCode)
	}
	if response.StatusCode == http.StatusAccepted {
		location := response.Header.Get("Location")
		if location != "" {
			parsedLocation, err := url.Parse(location)
			if err == nil {
				resolved := upload.URL.ResolveReference(parsedLocation)
				if resolved.Scheme == "https" && resolved.Host == upload.URL.Host && strings.Contains(resolved.Path, "/blobs/uploads/") {
					cancelRequest, err := http.NewRequestWithContext(ctx, http.MethodDelete, resolved.String(), nil)
					if err == nil {
						cancelRequest.Header = upload.Header.Clone()
						cancelResponse, cancelErr := client.Do(cancelRequest)
						if cancelErr == nil {
							cancelResponse.Body.Close()
						}
					}
				}
			}
		}
	}
	return nil
}

func dockerAuthEntries(data []byte) (map[string]json.RawMessage, error) {
	var config dockerAuthFile
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("credential data is not valid Docker auth JSON")
	}
	if config.Auths == nil {
		config.Auths = make(map[string]json.RawMessage)
	}
	return config.Auths, nil
}

func mergeDockerAuthFiles(base, override []byte) ([]byte, error) {
	baseEntries, err := dockerAuthEntries(base)
	if err != nil {
		return nil, err
	}
	overrideEntries, err := dockerAuthEntries(override)
	if err != nil {
		return nil, err
	}
	for registry, entry := range overrideEntries {
		baseEntries[registry] = entry
	}
	merged, err := json.Marshal(dockerAuthFile{Auths: baseEntries})
	if err != nil {
		return nil, fmt.Errorf("failed to encode merged Docker auth file: %w", err)
	}
	return merged, nil
}

func (t *provisioningRequestReconcilerTask) resolveISOInputs(
	ctx context.Context,
	clusterTemplate *provisioningv1alpha1.ClusterTemplate,
	liveISO map[string]any,
	document *seedGenerationInputDocument,
	seedAuth []byte,
	credentials map[string]map[string][]byte,
) error {
	releaseImage, err := seedGenerationString(liveISO, "releaseImage")
	if err != nil {
		return err
	}
	if _, _, err := imageRepositoryFromPullSpec(releaseImage); err != nil {
		return fmt.Errorf("invalid liveISO.releaseImage: %w", err)
	}
	resolvedReleaseImage, imageDigestSources, trustedCAs, err :=
		t.resolveSeedGenerationMirrorInputs(ctx, clusterTemplate, liveISO, releaseImage)
	if err != nil {
		return fmt.Errorf("failed to resolve release image mirror configuration: %w", err)
	}
	document.ResolvedReleaseImage = resolvedReleaseImage
	document.ImageDigestSources = imageDigestSources
	document.RegistryTrustedCAs = trustedCAs
	uploadSecretName, err := seedGenerationSecretReference(liveISO, "uploadSecretRef")
	if err != nil {
		return err
	}
	uploadSource, err := t.getTemplateSecret(ctx, clusterTemplate, uploadSecretName, "liveISO.uploadSecretRef")
	if err != nil {
		return err
	}
	for _, key := range []string{"host", "username", "privateKey", "remotePath", "knownHosts"} {
		if strings.TrimSpace(string(uploadSource.Data[key])) == "" {
			return fmt.Errorf("upload Secret %s/%s is missing non-empty %q data",
				clusterTemplate.Namespace, uploadSecretName, key)
		}
	}
	if !strings.HasPrefix(string(uploadSource.Data["remotePath"]), "/") {
		return fmt.Errorf("upload Secret %s/%s remotePath must be absolute", clusterTemplate.Namespace, uploadSecretName)
	}
	if caCert := uploadSource.Data["caCert"]; len(caCert) > 0 {
		if _, err := seedGenerationRootCAs(caCert); err != nil {
			return fmt.Errorf("upload Secret %s/%s caCert is invalid: %w", clusterTemplate.Namespace, uploadSecretName, err)
		}
	}
	urlBase, err := seedGenerationString(liveISO, "urlBase")
	if err != nil {
		return err
	}
	if err := validateSeedGenerationHTTPS(ctx, urlBase, uploadSource.Data["caCert"]); err != nil {
		return fmt.Errorf("liveISO.urlBase TLS preflight failed: %w", err)
	}
	uploadData := make(map[string][]byte, 6)
	for _, key := range []string{"host", "username", "privateKey", "remotePath", "knownHosts", "caCert"} {
		if value, ok := uploadSource.Data[key]; ok {
			uploadData[key] = append([]byte(nil), value...)
		}
	}
	credentials["upload"] = uploadData

	var pullSecret []byte
	if pullRef, hasPullRef := liveISO["pullSecretRef"]; hasPullRef {
		ref, ok := pullRef.(map[string]any)
		if !ok {
			return fmt.Errorf("liveISO.pullSecretRef must be an object")
		}
		name, err := seedGenerationString(ref, "name")
		if err != nil {
			return fmt.Errorf("liveISO.pullSecretRef requires name")
		}
		secret, err := t.getTemplateSecret(ctx, clusterTemplate, name, "liveISO.pullSecretRef")
		if err != nil {
			return err
		}
		pullSecret = secret.Data[corev1.DockerConfigJsonKey]
		if len(pullSecret) == 0 {
			return fmt.Errorf("pull Secret %s/%s is missing non-empty %q data",
				clusterTemplate.Namespace, name, corev1.DockerConfigJsonKey)
		}
	} else {
		hubPullSecret := &corev1.Secret{}
		key := types.NamespacedName{Name: "pull-secret", Namespace: "openshift-config"}
		if err := t.client.Get(ctx, key, hubPullSecret); err != nil {
			return fmt.Errorf("failed to get hub pull Secret %s/%s: %w", key.Namespace, key.Name, err)
		}
		if hubPullSecret.DeletionTimestamp != nil {
			return fmt.Errorf("hub pull Secret %s/%s is being deleted", key.Namespace, key.Name)
		}
		pullSecret, err = mergeDockerAuthFiles(hubPullSecret.Data[corev1.DockerConfigJsonKey], seedAuth)
		if err != nil {
			return fmt.Errorf("failed to merge hub and seed registry pull credentials: %w", err)
		}
	}
	if _, err := dockerAuthEntries(pullSecret); err != nil {
		return fmt.Errorf("effective ISO pull Secret is invalid: %w", err)
	}
	credentials["pullSecret"] = map[string][]byte{corev1.DockerConfigJsonKey: append([]byte(nil), pullSecret...)}
	return nil
}

func seedGenerationRootCAs(caCert []byte) (*x509.CertPool, error) {
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("failed to load system certificate roots: %w", err)
	}
	if roots == nil {
		roots = x509.NewCertPool()
	}
	if len(caCert) > 0 && !roots.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("no PEM certificates found")
	}
	return roots, nil
}

func validateSeedGenerationHTTPS(ctx context.Context, rawURL string, caCert []byte) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("urlBase must be an HTTPS URL with a host and no user info, query, or fragment")
	}
	roots, err := seedGenerationRootCAs(caCert)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, seedGenerationTLSTimeout)
	defer cancel()
	dialer := &net.Dialer{Timeout: seedGenerationTLSTimeout}
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	address := net.JoinHostPort(parsed.Hostname(), port)
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", parsed.Host, err)
	}
	defer conn.Close()
	tlsConn := tls.Client(conn, &tls.Config{ServerName: parsed.Hostname(), RootCAs: roots, MinVersion: tls.VersionTLS12})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return fmt.Errorf("tls handshake to %s failed: %w", parsed.Host, err)
	}
	return nil
}
