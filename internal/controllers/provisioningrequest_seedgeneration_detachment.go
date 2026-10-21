/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	seedGenerationKlusterletWorksLabel  = "import.open-cluster-management.io/klusterlet-works"
	seedGenerationAddonConfigAPIVersion = "agent.open-cluster-management.io/v1"
	seedGenerationAddonConfigPause      = "klusterletaddonconfig-pause"
)

// KlusterletAddonConfig is read through the metadata cache: only annotations
// are needed. Import ManifestWorks use the existing typed cache.
//+kubebuilder:rbac:groups=agent.open-cluster-management.io,resources=klusterletaddonconfigs,verbs=list;watch;patch
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=patch

// reconcileSeedGenerationAgentDetachment lets add-on pre-delete hooks finish
// while the work agent is running. Klusterlet teardown removes the agent and
// its AppliedManifestWork finalizers, but leaves those work records behind.
// Removing the import work records then garbage-collects the Klusterlet CRD.
func (t *provisioningRequestReconcilerTask) reconcileSeedGenerationAgentDetachment(
	ctx context.Context, snapshot *seedGenerationInputSnapshot,
) (bool, error) {
	status := t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
	if status == nil {
		return false, fmt.Errorf("seed generation run state is missing")
	}
	if status.AgentDetachmentCompleted {
		return false, nil
	}
	spokeClient, err := t.verifiedSeedGenerationSpokeClient(ctx, snapshot)
	if err != nil {
		return false, err
	}
	clusterName := t.object.Status.Extensions.ClusterDetails.Name
	if err := t.ensureSeedGenerationNoSelectTaint(ctx, clusterName); err != nil {
		return false, err
	}
	pending, err := t.pauseSeedGenerationAddonConfigs(ctx, clusterName)
	if err != nil {
		return false, err
	}
	if pending {
		return t.waitForSeedGenerationDetachment(ctx, "Waiting for KlusterletAddonConfig reconciliation to pause")
	}
	addons := &addonv1alpha1.ManagedClusterAddOnList{}
	if err := t.client.List(ctx, addons, client.InNamespace(clusterName)); err != nil {
		return false, fmt.Errorf("failed to list seed cluster add-ons: %w", err)
	}
	for i := range addons.Items {
		if err := deleteSeedGenerationObject(ctx, t.client, &addons.Items[i], metav1.DeletePropagationBackground); err != nil {
			return false, err
		}
	}
	if len(addons.Items) > 0 {
		names := make([]string, 0, len(addons.Items))
		for _, addon := range addons.Items {
			names = append(names, addon.Name)
		}
		slices.Sort(names)
		return t.waitForSeedGenerationDetachment(ctx, "Waiting for ACM add-on removal: "+strings.Join(names, ", "))
	}
	// Check the API before using its resource type. It disappears during
	// teardown, at which point a REST mapper returns NoMatch, not NotFound.
	crd := &apiextensionsv1.CustomResourceDefinition{}
	err = spokeClient.Get(ctx, client.ObjectKey{Name: seedGenerationKlusterletCRDName}, crd)
	if err != nil && !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("failed to read Klusterlet CRD: %w", err)
	}
	if err == nil {
		pending, err := t.removeSeedGenerationKlusterlet(ctx, spokeClient, clusterName)
		if err != nil || pending {
			return pending, err
		}
	}
	pending, err = t.cleanupSeedGenerationAppliedWorks(ctx, spokeClient)
	if err != nil || pending {
		return pending, err
	}
	if crd.Name != "" {
		// Normally the import work's owner reference removes the CRD. Handle
		// manually imported clusters without assuming that owner exists.
		if err := deleteSeedGenerationObject(ctx, spokeClient, crd, metav1.DeletePropagationBackground); err != nil {
			return false, err
		}
		return t.waitForSeedGenerationDetachment(ctx, "Waiting for Klusterlet CRD removal on the spoke")
	}
	namespaces := &corev1.NamespaceList{}
	if err := spokeClient.List(ctx, namespaces); err != nil {
		return false, fmt.Errorf("failed to check ACM namespace removal: %w", err)
	}
	for i := range namespaces.Items {
		name := namespaces.Items[i].Name
		if strings.HasPrefix(name, "open-cluster-management-agent") ||
			strings.HasPrefix(name, "open-cluster-management-addon-") {
			return t.waitForSeedGenerationDetachment(ctx, "Waiting for ACM namespace removal: "+name)
		}
	}
	pending, err = cleanupSeedGenerationObservabilitySecrets(ctx, spokeClient)
	if err != nil {
		return pending, err
	}
	if pending {
		return t.waitForSeedGenerationDetachment(ctx, "Waiting for ACM observability artifact cleanup")
	}
	status.AgentDetachmentCompleted = true
	setSeedGenerationProgress(t.object, provisioningv1alpha1.CRconditionReasons.InProgress,
		"ACM detachment completed; preparing LCA seed generation")
	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return false, fmt.Errorf("failed to record ACM detachment completion: %w", err)
	}
	return false, nil
}

func (t *provisioningRequestReconcilerTask) waitForSeedGenerationDetachment(ctx context.Context, message string) (bool, error) {
	return true, t.updateSeedGenerationProgress(ctx, provisioningv1alpha1.CRconditionReasons.CleaningACMResources, message)
}

// Legacy add-ons with a Manual install strategy are recreated from this
// configuration independently of Placement and its NoSelect taints. Pause
// their producer before deleting add-ons, leaving their pre-delete hooks active.
func (t *provisioningRequestReconcilerTask) pauseSeedGenerationAddonConfigs(ctx context.Context, clusterName string) (bool, error) {
	configs := &metav1.PartialObjectMetadataList{TypeMeta: metav1.TypeMeta{
		APIVersion: seedGenerationAddonConfigAPIVersion, Kind: "KlusterletAddonConfigList",
	}}
	if err := t.client.List(ctx, configs, client.InNamespace(clusterName)); err != nil {
		if meta.IsNoMatchError(err) || apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to read seed cluster KlusterletAddonConfigs: %w", err)
	}
	pending := false
	for i := range configs.Items {
		config := &configs.Items[i]
		if config.DeletionTimestamp != nil {
			pending = true
			continue
		}
		if strings.EqualFold(config.Annotations[seedGenerationAddonConfigPause], "true") {
			continue
		}
		config.APIVersion = seedGenerationAddonConfigAPIVersion
		config.Kind = "KlusterletAddonConfig"
		original := config.DeepCopy()
		if config.Annotations == nil {
			config.Annotations = map[string]string{}
		}
		config.Annotations[seedGenerationAddonConfigPause] = "true"
		if err := t.client.Patch(ctx, config, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
			return false, fmt.Errorf("failed to pause KlusterletAddonConfig %s/%s: %w", config.Namespace, config.Name, err)
		}
		pending = true
	}
	return pending, nil
}

// removeSeedGenerationKlusterlet refuses to tear down shared ACM resources
// if another agent has appeared since preflight.
func (t *provisioningRequestReconcilerTask) removeSeedGenerationKlusterlet(
	ctx context.Context, spokeClient client.Client, clusterName string,
) (bool, error) {
	status := t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
	klusterlets := &unstructured.UnstructuredList{}
	klusterlets.SetAPIVersion(seedGenerationKlusterletAPIVersion)
	klusterlets.SetKind("KlusterletList")
	if err := spokeClient.List(ctx, klusterlets); err != nil && !meta.IsNoMatchError(err) && !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("failed to inspect remaining spoke Klusterlets: %w", err)
	}
	for _, remaining := range klusterlets.Items {
		if remaining.GetName() != seedGenerationKlusterletName || string(remaining.GetUID()) != status.KlusterletUID {
			return false, fmt.Errorf("spoke has a different Klusterlet; refusing to remove shared ACM resources")
		}
	}
	klusterlet := newSeedGenerationKlusterlet()
	err := spokeClient.Get(ctx, client.ObjectKey{Name: seedGenerationKlusterletName}, klusterlet)
	if err == nil {
		if string(klusterlet.GetUID()) != status.KlusterletUID {
			return false, fmt.Errorf("spoke Klusterlet identity changed after preflight")
		}
		paused, err := t.ensureSeedGenerationImportPaused(ctx, spokeClient, clusterName)
		if err != nil {
			return false, err
		}
		if !paused {
			return t.waitForSeedGenerationDetachment(ctx, "Waiting for import ManifestWorks to acknowledge ReadOnly")
		}
		if err := deleteSeedGenerationObject(ctx, spokeClient, klusterlet, metav1.DeletePropagationBackground); err != nil {
			return false, err
		}
		return t.waitForSeedGenerationDetachment(ctx, "Waiting for Klusterlet removal on the spoke")
	}
	if !apierrors.IsNotFound(err) && !meta.IsNoMatchError(err) {
		return false, fmt.Errorf("failed to read spoke Klusterlet: %w", err)
	}
	return false, nil
}

// Disable both Placement-driven add-ons and observability's independent
// placement. Disabling automatic import prevents bootstrap attempts, but older
// ACM versions also need explicit ReadOnly configs on existing import works.
func (t *provisioningRequestReconcilerTask) ensureSeedGenerationNoSelectTaint(ctx context.Context, clusterName string) error {
	managedCluster := &clusterv1.ManagedCluster{}
	if err := t.client.Get(ctx, client.ObjectKey{Name: clusterName}, managedCluster); err != nil {
		return fmt.Errorf("failed to get seed ManagedCluster: %w", err)
	}
	status := t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
	if status.ManagedClusterUID == "" || string(managedCluster.UID) != status.ManagedClusterUID || managedCluster.DeletionTimestamp != nil {
		return fmt.Errorf("seed ManagedCluster identity changed or deletion has started")
	}
	original := managedCluster.DeepCopy()
	found := false
	for _, taint := range managedCluster.Spec.Taints {
		if taint.Key != seedGenerationManagedClusterTaint {
			continue
		}
		if taint.Value != "true" || taint.Effect != clusterv1.TaintEffectNoSelect {
			return fmt.Errorf("seed ManagedCluster has a conflicting detachment taint")
		}
		found = true
	}
	if !found {
		managedCluster.Spec.Taints = append(managedCluster.Spec.Taints, clusterv1.Taint{
			Key: seedGenerationManagedClusterTaint, Value: "true", Effect: clusterv1.TaintEffectNoSelect,
		})
	}
	_, importDisabled := managedCluster.Annotations[disableAutoImportAnnotation]
	if found && importDisabled && managedCluster.Labels["observability"] == "disabled" {
		return nil
	}
	if managedCluster.Annotations == nil {
		managedCluster.Annotations = map[string]string{}
	}
	managedCluster.Annotations[disableAutoImportAnnotation] = "true"
	if managedCluster.Labels == nil {
		managedCluster.Labels = map[string]string{}
	}
	managedCluster.Labels["observability"] = "disabled"
	if err := t.client.Patch(ctx, managedCluster, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
		return fmt.Errorf("failed to disable ACM placement and import for seed cluster: %w", err)
	}
	return nil
}

func (t *provisioningRequestReconcilerTask) ensureSeedGenerationImportPaused(
	ctx context.Context, spokeClient client.Client, clusterName string,
) (bool, error) {
	works := &workv1.ManifestWorkList{}
	if err := t.client.List(ctx, works, client.InNamespace(clusterName),
		client.MatchingLabels{seedGenerationKlusterletWorksLabel: "true"}); err != nil {
		return false, fmt.Errorf("failed to inspect import ManifestWorks: %w", err)
	}
	paused := true
	for i := range works.Items {
		work := &works.Items[i]
		if work.DeletionTimestamp != nil {
			paused = false
			continue
		}
		configs, err := seedGenerationReadOnlyConfigs(work, spokeClient.RESTMapper())
		if err != nil {
			return false, fmt.Errorf("failed to configure import ManifestWork %s: %w", work.Name, err)
		}
		if !equality.Semantic.DeepEqual(work.Spec.ManifestConfigs, configs) {
			original := work.DeepCopy()
			work.Spec.ManifestConfigs = configs
			if err := t.client.Patch(ctx, work, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
				return false, fmt.Errorf("failed to pause import ManifestWork %s: %w", work.Name, err)
			}
			paused = false
			continue
		}
		condition := meta.FindStatusCondition(work.Status.Conditions, workv1.WorkApplied)
		if condition == nil || condition.Status != metav1.ConditionTrue || condition.ObservedGeneration != work.Generation {
			paused = false
		}
	}
	return paused, nil
}

// Resolve resource plurals using spoke discovery. A ReadOnly entry with the
// wrong resource identifier silently fails to match and still permits writes.
func seedGenerationReadOnlyConfigs(work *workv1.ManifestWork, mapper meta.RESTMapper) ([]workv1.ManifestConfigOption, error) {
	configs := make([]workv1.ManifestConfigOption, 0, len(work.Spec.Workload.Manifests))
	for _, manifest := range work.Spec.Workload.Manifests {
		object := &unstructured.Unstructured{}
		if err := json.Unmarshal(manifest.Raw, object); err != nil {
			return nil, fmt.Errorf("invalid import manifest: %w", err)
		}
		gvk := object.GroupVersionKind()
		if gvk.Kind == "" || gvk.Version == "" || object.GetName() == "" {
			return nil, fmt.Errorf("import manifest is missing apiVersion, kind or name")
		}
		mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve import resource %s: %w", gvk, err)
		}
		identifier := workv1.ResourceIdentifier{
			Group: gvk.Group, Resource: mapping.Resource.Resource, Name: object.GetName(), Namespace: object.GetNamespace(),
		}
		config := workv1.ManifestConfigOption{ResourceIdentifier: identifier}
		for _, existing := range work.Spec.ManifestConfigs {
			if existing.ResourceIdentifier == identifier {
				config = existing
				break
			}
		}
		config.UpdateStrategy = &workv1.UpdateStrategy{Type: workv1.UpdateStrategyTypeReadOnly}
		configs = append(configs, config)
	}
	return configs, nil
}

func (t *provisioningRequestReconcilerTask) cleanupSeedGenerationAppliedWorks(ctx context.Context, spokeClient client.Client) (bool, error) {
	status := t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
	if status.KlusterletUID == "" {
		return false, fmt.Errorf("seed generation has no pinned ACM agent identity")
	}
	works := &workv1.AppliedManifestWorkList{}
	if err := spokeClient.List(ctx, works); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to list spoke AppliedManifestWorks: %w", err)
	}
	clusterName := t.object.Status.Extensions.ClusterDetails.Name
	// A foreign work may be left over from an earlier agent. Wait within the
	// run deadline for it to disappear, without deleting any work records or
	// assuming ACM will garbage-collect it. Check non-import works as well.
	for _, work := range works.Items {
		if work.Spec.AgentID != status.KlusterletUID {
			return t.waitForSeedGenerationDetachment(ctx,
				fmt.Sprintf("Waiting for AppliedManifestWork %s owned by a different ACM agent (%q); expected agent %q",
					work.Name, work.Spec.AgentID, status.KlusterletUID))
		}
	}
	for i := range works.Items {
		work := &works.Items[i]
		// The Klusterlet operator removes its own work finalizers before
		// stopping the agent. Never force-remove an unfinished finalizer.
		if len(work.Finalizers) > 0 {
			return t.waitForSeedGenerationDetachment(ctx, "Waiting for AppliedManifestWork cleanup on the spoke")
		}
		propagation := metav1.DeletePropagationOrphan
		if work.Spec.ManifestWorkName == clusterName+"-klusterlet" || work.Spec.ManifestWorkName == clusterName+"-klusterlet-crds" {
			propagation = metav1.DeletePropagationBackground
		}
		// Preserve configured workloads from non-import works. Only the
		// import works should garbage-collect their agent resources.
		if err := deleteSeedGenerationObject(ctx, spokeClient, work, propagation); err != nil {
			return false, err
		}
	}
	if len(works.Items) > 0 {
		return t.waitForSeedGenerationDetachment(ctx, "Waiting for AppliedManifestWork cleanup on the spoke")
	}
	return false, nil
}

func cleanupSeedGenerationObservabilitySecrets(ctx context.Context, spokeClient client.Client) (bool, error) {
	pending := false
	for _, namespace := range []string{"openshift-monitoring", "openshift-user-workload-monitoring"} {
		if err := cleanupSeedGenerationMonitoringConfig(ctx, spokeClient, namespace); err != nil {
			return false, err
		}
		secrets := &corev1.SecretList{}
		if err := spokeClient.List(ctx, secrets, client.InNamespace(namespace)); err != nil {
			return false, fmt.Errorf("failed to inspect residual observability Secrets: %w", err)
		}
		for i := range secrets.Items {
			secret := &secrets.Items[i]
			if !seedGenerationObservabilitySecret(secret.Name) {
				continue
			}
			pending = true
			if err := deleteSeedGenerationObject(ctx, spokeClient, secret, metav1.DeletePropagationBackground); err != nil {
				return false, err
			}
		}
	}
	return pending, nil
}

func seedGenerationObservabilitySecret(name string) bool {
	for _, prefix := range []string{"hub-alertmanager-router-ca", "observability-alertmanager-accessor",
		"obs-alertmanager-mtls-ca", "obs-alertmanager-mtls-cert", "hub-mtls-ca"} {
		if name == prefix || strings.HasPrefix(name, prefix+"-") {
			return true
		}
	}
	return false
}

// Remove only alertmanager entries that reference ACM's known Secrets. Use
// an untyped map so unrelated monitoring settings survive the round trip.
func cleanupSeedGenerationMonitoringConfig(ctx context.Context, c client.Client, namespace string) error {
	name, component, field := "cluster-monitoring-config", "prometheusK8s", "additionalAlertManagerConfigs"
	if namespace == "openshift-user-workload-monitoring" {
		name, component, field = "user-workload-monitoring-config", "prometheus", "additionalAlertmanagerConfigs"
	}
	configMap := &corev1.ConfigMap{}
	if err := c.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, configMap); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to read monitoring config: %w", err)
	}
	if configMap.DeletionTimestamp != nil {
		return nil
	}
	var config map[string]any
	if err := yaml.Unmarshal([]byte(configMap.Data["config.yaml"]), &config); err != nil {
		return fmt.Errorf("failed to decode monitoring config %s/%s: %w", namespace, name, err)
	}
	entries, _, err := unstructured.NestedSlice(config, component, field)
	if err != nil {
		return fmt.Errorf("invalid monitoring alertmanager configuration: %w", err)
	}
	kept := make([]any, 0, len(entries))
	for _, entry := range entries {
		value, ok := entry.(map[string]any)
		if !ok {
			return fmt.Errorf("monitoring alertmanager configuration must be an object")
		}
		secret, _, err := unstructured.NestedString(value, "tlsConfig", "ca", "name")
		if err != nil {
			return fmt.Errorf("invalid monitoring alertmanager CA reference: %w", err)
		}
		if !seedGenerationObservabilitySecret(secret) {
			kept = append(kept, entry)
		}
	}
	if len(kept) == len(entries) {
		return nil
	}
	if err := unstructured.SetNestedSlice(config, kept, component, field); err != nil {
		return fmt.Errorf("failed to remove ACM alertmanager configuration: %w", err)
	}
	encoded, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to encode monitoring configuration: %w", err)
	}
	original := configMap.DeepCopy()
	configMap.Data["config.yaml"] = string(encoded)
	if err := c.Patch(ctx, configMap, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
		return fmt.Errorf("failed to remove ACM monitoring configuration: %w", err)
	}
	return nil
}

func deleteSeedGenerationObject(ctx context.Context, c client.Client, object client.Object, propagation metav1.DeletionPropagation) error {
	if object.GetDeletionTimestamp() != nil {
		return nil
	}
	uid := object.GetUID()
	if err := client.IgnoreNotFound(c.Delete(ctx, object, client.Preconditions{UID: &uid}, client.PropagationPolicy(propagation))); err != nil {
		return fmt.Errorf("failed to delete seed generation resource %s/%s: %w", object.GetNamespace(), object.GetName(), err)
	}
	return nil
}
