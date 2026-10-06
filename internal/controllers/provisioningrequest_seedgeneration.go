/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;delete,namespace=system
//+kubebuilder:rbac:groups="",resources=pods;persistentvolumeclaims;secrets;configmaps,verbs=get;list;watch;delete,namespace=system
//+kubebuilder:rbac:groups="",resources=pods,verbs=create,namespace=system
//+kubebuilder:rbac:groups="",resources=pods/log,verbs=get,namespace=system
//+kubebuilder:rbac:groups=config.openshift.io,resources=images;imagedigestmirrorsets;imagetagmirrorsets,verbs=list;watch
//+kubebuilder:rbac:groups=operator.openshift.io,resources=imagecontentsourcepolicies,verbs=list;watch

// IsSeedGenerationRequested checks for the operation independently of the
// ManagedCluster release and returns the resolved ClusterTemplate for reuse by
// upgrade dispatch. A seed request must never enter the upgrade path.
func (t *provisioningRequestReconcilerTask) IsSeedGenerationRequested(
	ctx context.Context,
) (bool, *provisioningv1alpha1.ClusterTemplate, error) {
	if !ctlrutils.IsClusterZtpDone(t.object) {
		return false, nil, nil
	}
	template, err := t.object.GetClusterTemplateRef(ctx, t.client)
	if err != nil {
		return false, nil, fmt.Errorf("failed to get ClusterTemplate for seed generation: %w", err)
	}
	if template.DeletionTimestamp != nil {
		return false, nil, fmt.Errorf("cluster template %s/%s is being deleted", template.Namespace, template.Name)
	}
	requested, err := provisioningv1alpha1.HasSeedGenerationConfig(template, t.object)
	if err != nil {
		return false, nil, fmt.Errorf("failed to inspect seed generation configuration: %w", err)
	}
	return requested, template, nil
}

func seedGenerationCondition(pr *provisioningv1alpha1.ProvisioningRequest) *metav1.Condition {
	return meta.FindStatusCondition(pr.Status.Conditions,
		string(provisioningv1alpha1.PRconditionTypes.SeedGenerationCompleted))
}

func seedGenerationTerminal(pr *provisioningv1alpha1.ProvisioningRequest) bool {
	condition := seedGenerationCondition(pr)
	if condition == nil {
		return false
	}
	if condition.Status == metav1.ConditionTrue && condition.Reason == string(provisioningv1alpha1.CRconditionReasons.Completed) {
		return true
	}
	if condition.Status != metav1.ConditionFalse {
		return false
	}
	switch condition.Reason {
	case string(provisioningv1alpha1.CRconditionReasons.Failed),
		string(provisioningv1alpha1.CRconditionReasons.TimedOut),
		string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed):
		return true
	default:
		return false
	}
}

func seedGenerationFailureNeedsCleanup(pr *provisioningv1alpha1.ProvisioningRequest) bool {
	condition := seedGenerationCondition(pr)
	return condition != nil && condition.Status == metav1.ConditionFalse &&
		(condition.Reason == string(provisioningv1alpha1.CRconditionReasons.Failed) ||
			condition.Reason == string(provisioningv1alpha1.CRconditionReasons.TimedOut) ||
			condition.Reason == string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed))
}

func (t *provisioningRequestReconcilerTask) reconcileSeedGenerationFailureCleanup(
	ctx context.Context,
) (bool, ctrl.Result, error) {
	if !seedGenerationFailureNeedsCleanup(t.object) {
		return false, ctrl.Result{}, nil
	}
	pending, err := t.cleanupSeedGenerationPreflightResources(ctx)
	if err != nil {
		result, retryErr := requeueWithError(fmt.Errorf("failed to clean up seed generation preflight resources: %w", err))
		return true, result, retryErr
	}
	if pending {
		return true, requeueWithShortInterval(), nil
	}
	return false, ctrl.Result{}, nil
}

func (t *provisioningRequestReconcilerTask) handleSeedGenerationPreflightError(
	ctx context.Context,
	operation string,
	err error,
) (ctrl.Result, error) {
	if isSeedGenerationTransientError(err) {
		return requeueWithError(fmt.Errorf("%s: %w", operation, err))
	}
	return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed,
		fmt.Sprintf("%s: %v", operation, err))
}

func (t *provisioningRequestReconcilerTask) seedGenerationNeedsFastPath() bool {
	details := t.object.Status.Extensions.ClusterDetails
	if details != nil && details.SeedGenerationStatus != nil && details.SeedGenerationStatus.DetachmentStarted {
		return true
	}
	if seedGenerationTerminal(t.object) {
		// A pre-detachment terminal failure leaves normal ACM reconciliation safe.
		return false
	}
	if details != nil && details.SeedGenerationStatus != nil && details.SeedGenerationStatus.StartedAt != nil {
		return true
	}
	return seedGenerationCondition(t.object) != nil
}

// startSeedGeneration persists the clock and timeout before resource creation
// begins. The admission guard remains active until the workflow can run.
func (t *provisioningRequestReconcilerTask) startSeedGeneration(ctx context.Context, timeout time.Duration) error {
	details := t.object.Status.Extensions.ClusterDetails
	if timeout <= 0 || details == nil || seedGenerationCondition(t.object) != nil {
		return fmt.Errorf("cannot start seed generation from the current state")
	}
	if details.SeedGenerationStatus != nil &&
		(details.SeedGenerationStatus.StartedAt != nil || details.SeedGenerationStatus.DetachmentStarted) {
		return fmt.Errorf("seed generation has already started")
	}
	seconds := int64(timeout / time.Second)
	if timeout%time.Second != 0 {
		seconds++
	}
	if seconds == 0 {
		seconds = 1
	}
	now := metav1.Now()
	details.SeedGenerationStatus = &provisioningv1alpha1.SeedGenerationStatus{
		StartedAt:      &now,
		TimeoutSeconds: seconds,
	}
	ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
		provisioningv1alpha1.PRconditionTypes.SeedGenerationCompleted,
		provisioningv1alpha1.CRconditionReasons.Validating, metav1.ConditionFalse,
		"Seed generation validation is in progress")
	ctlrutils.SetProvisioningStateInProgress(t.object, "Seed generation is in progress")
	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to record seed generation start: %w", err)
	}
	return nil
}

// seedGenerationTimeout resolves the operation-specific default or an
// override. The result is persisted at the start and never recalculated
// after detachment from mutable template input.
func seedGenerationTimeout(config map[string]any) (time.Duration, error) {
	if _, ok := config[ctlrutils.ClusterUpgradeTimeoutConfigKey]; ok {
		return 0, fmt.Errorf("clusterUpgradeTimeout is not valid for seed generation")
	}
	if _, ok := config[ctlrutils.SeedGenerationTimeoutConfigKey]; ok {
		return 0, fmt.Errorf("seedGenerationTimeout must be nested under seedGeneration")
	}
	seed, ok := config[ctlrutils.UpgradeDefaultsSeedGenerationKey].(map[string]any)
	if !ok {
		return 0, fmt.Errorf("seedGeneration must be an object")
	}
	timeout := ctlrutils.DefaultSeedGenerationTimeout
	if _, hasISO := seed["liveISO"]; hasISO {
		timeout = ctlrutils.DefaultSeedGenerationWithISOTimeout
	}
	if raw, ok := seed[ctlrutils.SeedGenerationTimeoutConfigKey]; ok {
		value, ok := raw.(string)
		if !ok {
			return 0, fmt.Errorf("seedGenerationTimeout must be a duration string")
		}
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 {
			return 0, fmt.Errorf("seedGenerationTimeout must be a positive duration: %q", value)
		}
		return parsed, nil
	}
	return timeout, nil
}

// reconcileSeedGeneration is the early route for an already started run. It
// completes and freezes all preflight inputs before subsequent steps detach
// the spoke or create seed artifacts.
func (t *provisioningRequestReconcilerTask) reconcileSeedGeneration(ctx context.Context) (ctrl.Result, error) {
	if seedGenerationTerminal(t.object) {
		if seedGenerationFailureNeedsCleanup(t.object) {
			pending, err := t.cleanupSeedGenerationPreflightResources(ctx)
			if err != nil {
				return requeueWithError(fmt.Errorf("failed to clean up seed generation preflight resources: %w", err))
			}
			if pending {
				return requeueWithShortInterval(), nil
			}
		}
		return doNotRequeue(), nil
	}
	details := t.object.Status.Extensions.ClusterDetails
	if details == nil || details.SeedGenerationStatus == nil || details.SeedGenerationStatus.StartedAt == nil ||
		details.SeedGenerationStatus.TimeoutSeconds <= 0 {
		return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.Failed,
			"Seed generation has incomplete persisted run state")
	}
	status := details.SeedGenerationStatus
	deadline := status.StartedAt.Add(time.Duration(status.TimeoutSeconds) * time.Second)
	if !time.Now().Before(deadline) {
		return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.TimedOut,
			fmt.Sprintf("Seed generation timed out after %s", time.Duration(status.TimeoutSeconds)*time.Second))
	}
	condition := seedGenerationCondition(t.object)
	if !status.DetachmentStarted && condition != nil &&
		condition.Status == metav1.ConditionFalse &&
		condition.Reason == string(provisioningv1alpha1.CRconditionReasons.Validating) {
		return t.reconcileSeedGenerationPreflight(ctx)
	}
	condition = seedGenerationCondition(t.object)
	if !status.DetachmentStarted && condition != nil && condition.Status == metav1.ConditionFalse &&
		condition.Reason == string(provisioningv1alpha1.CRconditionReasons.InProgress) {
		return t.reconcileSeedGenerationSnapshot(ctx, status)
	}
	return requeueWithMediumInterval(), nil
}

func (t *provisioningRequestReconcilerTask) reconcileSeedGenerationPreflight(ctx context.Context) (ctrl.Result, error) {
	if err := t.validateSeedGenerationSpokePrerequisites(ctx); err != nil {
		return t.handleSeedGenerationPreflightError(ctx, "seed generation spoke preflight failed", err)
	}
	snapshot, retry, err := t.ensureSeedGenerationInputSnapshot(ctx, nil)
	if err != nil {
		return t.handleSeedGenerationPreflightError(ctx, "seed generation preflight failed", err)
	}
	if retry {
		return requeueWithShortInterval(), nil
	}
	complete, retry, err := t.ensureSeedGenerationToolsPreflight(ctx, snapshot)
	if err != nil {
		return t.handleSeedGenerationPreflightError(ctx, "seed generation tools preflight failed", err)
	}
	if retry || !complete {
		return requeueWithShortInterval(), nil
	}
	if current := seedGenerationCondition(t.object); current != nil &&
		current.Reason == string(provisioningv1alpha1.CRconditionReasons.Validating) {
		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.SeedGenerationCompleted,
			provisioningv1alpha1.CRconditionReasons.InProgress, metav1.ConditionFalse,
			"Seed generation inputs are validated and frozen")
		if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
			return requeueWithError(fmt.Errorf("failed to record seed generation preflight completion: %w", err))
		}
	}
	return requeueWithMediumInterval(), nil
}

func (t *provisioningRequestReconcilerTask) reconcileSeedGenerationSnapshot(
	ctx context.Context,
	status *provisioningv1alpha1.SeedGenerationStatus,
) (ctrl.Result, error) {
	if len(seedGenerationSnapshotStatusUIDs(t.object)) == 0 {
		return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed,
			"seed generation input snapshot UIDs are missing after preflight")
	}
	snapshot, err := t.loadSeedGenerationInputSnapshot(ctx)
	if err != nil {
		return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed,
			fmt.Sprintf("failed to adopt frozen seed generation inputs: %v", err))
	}
	if _, hasISO := snapshot.Document.SeedGeneration["liveISO"]; hasISO && status.ReleaseImageDigest == "" {
		return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed,
			"resolved release image digest is missing after ISO preflight")
	}
	pending, err := t.cleanupSeedGenerationPreflightPods(ctx)
	if err != nil {
		return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed,
			fmt.Sprintf("failed to clean up seed generation preflight Pods: %v", err))
	}
	if pending {
		return requeueWithShortInterval(), nil
	}
	return requeueWithMediumInterval(), nil
}

func (t *provisioningRequestReconcilerTask) failSeedGeneration(
	ctx context.Context, reason provisioningv1alpha1.ConditionReason, message string) (ctrl.Result, error) {
	ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
		provisioningv1alpha1.PRconditionTypes.SeedGenerationCompleted, reason, metav1.ConditionFalse, message)
	ctlrutils.SetProvisioningStateFailed(t.object, message)
	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return requeueWithError(fmt.Errorf("failed to record seed generation failure: %w", err))
	}
	pending, err := t.cleanupSeedGenerationPreflightResources(ctx)
	if err != nil {
		return requeueWithError(fmt.Errorf("failed to clean up seed generation preflight resources after failure: %w", err))
	}
	if pending {
		return requeueWithShortInterval(), nil
	}
	return doNotRequeue(), nil
}

func (t *provisioningRequestReconcilerTask) cleanupSeedGenerationPreflightResources(ctx context.Context) (bool, error) {
	podsPending, err := t.cleanupSeedGenerationPreflightPods(ctx)
	if err != nil {
		return false, err
	}
	selector := client.MatchingLabels{
		provisioningv1alpha1.ProvisioningRequestNameLabel: t.object.Name,
		provisioningv1alpha1.ProvisioningRequestUIDLabel:  string(t.object.UID),
		seedGenerationSnapshotLabel:                       "true",
		seedGenerationRunLabel:                            "true",
	}
	configMaps := &corev1.ConfigMapList{}
	if err := t.client.List(ctx, configMaps, client.InNamespace(seedGenerationSnapshotNamespace()), selector); err != nil {
		return false, fmt.Errorf("failed to list seed generation snapshot ConfigMaps: %w", err)
	}
	secrets := &corev1.SecretList{}
	if err := t.client.List(ctx, secrets, client.InNamespace(seedGenerationSnapshotNamespace()), selector); err != nil {
		return false, fmt.Errorf("failed to list seed generation snapshot Secrets: %w", err)
	}
	pending := podsPending
	for i := range configMaps.Items {
		resource := &configMaps.Items[i]
		if err := validateSeedGenerationSnapshotOwner(resource, t.object, seedGenerationSnapshotConfigKind, nil); err != nil {
			return false, err
		}
		pending = true
		if resource.DeletionTimestamp == nil {
			if err := client.IgnoreNotFound(t.client.Delete(ctx, resource)); err != nil {
				return false, fmt.Errorf("failed to delete seed generation snapshot ConfigMap %s: %w", resource.Name, err)
			}
		}
	}
	for i := range secrets.Items {
		resource := &secrets.Items[i]
		if err := validateSeedGenerationSnapshotOwner(resource, t.object, seedGenerationSnapshotSecretKind, nil); err != nil {
			return false, err
		}
		pending = true
		if resource.DeletionTimestamp == nil {
			if err := client.IgnoreNotFound(t.client.Delete(ctx, resource)); err != nil {
				return false, fmt.Errorf("failed to delete seed generation snapshot Secret %s: %w", resource.Name, err)
			}
		}
	}
	return pending, nil
}

// cleanupSeedGenerationResources deletes only objects labeled with this PR's
// UID. List again on the next reconcile to wait for their finalizers.
func (r *ProvisioningRequestReconciler) cleanupSeedGenerationResources(
	ctx context.Context, pr *provisioningv1alpha1.ProvisioningRequest) (bool, error) {
	details := pr.Status.Extensions.ClusterDetails
	if (details == nil || details.SeedGenerationStatus == nil) && seedGenerationCondition(pr) == nil {
		return true, nil
	}
	if pr.UID == "" {
		return false, fmt.Errorf("cannot clean seed resources for ProvisioningRequest %s without a UID", pr.Name)
	}
	namespace := ctlrutils.GetEnvOrDefault(constants.DefaultNamespaceEnvName, constants.DefaultNamespace)
	labels := client.MatchingLabels{
		provisioningv1alpha1.ProvisioningRequestNameLabel: pr.Name,
		provisioningv1alpha1.ProvisioningRequestUIDLabel:  string(pr.UID),
	}
	lists := []client.ObjectList{
		&batchv1.JobList{}, &corev1.PodList{}, &corev1.PersistentVolumeClaimList{},
		&corev1.SecretList{}, &corev1.ConfigMapList{},
	}
	clean := true
	for _, list := range lists {
		if err := r.Client.List(ctx, list, client.InNamespace(namespace), labels); err != nil {
			return false, fmt.Errorf("failed to list seed generation resources: %w", err)
		}
		objects, err := meta.ExtractList(list)
		if err != nil {
			return false, fmt.Errorf("failed to inspect seed generation resources: %w", err)
		}
		for _, object := range objects {
			resource, ok := object.(client.Object)
			if !ok {
				return false, fmt.Errorf("seed generation resource does not implement client.Object")
			}
			clean = false
			if resource.GetDeletionTimestamp() == nil {
				if err := client.IgnoreNotFound(r.Client.Delete(ctx, resource)); err != nil {
					return false, fmt.Errorf("failed to delete seed generation resource %s: %w", resource.GetName(), err)
				}
			}
		}
	}
	return clean, nil
}
