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

// IsSeedGenerationRequested checks for the operation independently of the
// ManagedCluster release. A seed request must never enter the upgrade path.
func (t *provisioningRequestReconcilerTask) IsSeedGenerationRequested(ctx context.Context) (bool, error) {
	if !ctlrutils.IsClusterZtpDone(t.object) {
		return false, nil
	}
	template, err := t.object.GetClusterTemplateRef(ctx, t.client)
	if err != nil {
		return false, fmt.Errorf("failed to get ClusterTemplate for seed generation: %w", err)
	}
	requested, err := provisioningv1alpha1.HasSeedGenerationConfig(template, t.object)
	if err != nil {
		return false, fmt.Errorf("failed to inspect seed generation configuration: %w", err)
	}
	return requested, nil
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

// startSeedGeneration persists the clock and timeout before any phase creates
// resources. The admission guard remains active until the workflow can run.
func (t *provisioningRequestReconcilerTask) startSeedGeneration(ctx context.Context, timeout time.Duration) error {
	details := t.object.Status.Extensions.ClusterDetails
	if timeout <= 0 || details == nil ||
		seedGenerationTerminal(t.object) || seedGenerationCondition(t.object) != nil {
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

// reconcileSeedGeneration is the early route for an already started run. The
// phase drivers arrive in later packages; until then it only enforces the
// persisted one-shot state and overall timeout.
func (t *provisioningRequestReconcilerTask) reconcileSeedGeneration(ctx context.Context) (ctrl.Result, error) {
	if seedGenerationTerminal(t.object) {
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
	return doNotRequeue(), nil
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
