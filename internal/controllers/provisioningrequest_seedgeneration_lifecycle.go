/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	seedGenerationLCASeedSecretName    = "seedgen"
	seedGenerationLCASeedCRName        = "seedimage"
	seedGenerationSeedAuthKey          = "seedAuth"
	seedGenerationKlusterletName       = "klusterlet"
	seedGenerationKlusterletCRDName    = "klusterlets.operator.open-cluster-management.io"
	seedGenerationLCAAPIVersion        = "lca.openshift.io/v1"
	seedGenerationLCAKind              = "SeedGenerator"
	seedGenerationKlusterletAPIVersion = "operator.open-cluster-management.io/v1"
	seedGenerationKlusterletKind       = "Klusterlet"
	seedGenerationLCAInProgress        = "SeedGenInProgress"
	seedGenerationLCACompleted         = "SeedGenCompleted"
	seedGenerationManagedClusterTaint  = "seed-generation.clcm.openshift.io/detached"
)

func seedGenerationLCAOwnershipLabels(pr *provisioningv1alpha1.ProvisioningRequest) map[string]string {
	return map[string]string{
		provisioningv1alpha1.ProvisioningRequestNameLabel: pr.Name,
		provisioningv1alpha1.ProvisioningRequestUIDLabel:  string(pr.UID),
		seedGenerationRunLabel:                            "true",
	}
}

func validateSeedGenerationLCAOwnership(
	resource client.Object,
	pr *provisioningv1alpha1.ProvisioningRequest,
) error {
	labels := resource.GetLabels()
	if labels[provisioningv1alpha1.ProvisioningRequestNameLabel] != pr.Name ||
		labels[provisioningv1alpha1.ProvisioningRequestUIDLabel] != string(pr.UID) ||
		labels[seedGenerationRunLabel] != "true" {
		return fmt.Errorf("lca resource %s/%s is not owned by ProvisioningRequest %s with UID %s",
			resource.GetNamespace(), resource.GetName(), pr.Name, pr.UID)
	}
	return nil
}

func (t *provisioningRequestReconcilerTask) reconcileSeedGenerationDetached(
	ctx context.Context,
	snapshot *seedGenerationInputSnapshot,
) (ctrl.Result, error) {
	pending, err := t.reconcileSeedGenerationAgentDetachment(ctx, snapshot)
	if err != nil {
		return t.handleSeedGenerationLifecycleError(ctx, "failed to detach ACM agents", err)
	}
	if pending {
		return requeueWithShortInterval(), nil
	}
	return t.reconcileSeedGenerator(ctx, snapshot)
}

func (t *provisioningRequestReconcilerTask) handleSeedGenerationLifecycleError(
	ctx context.Context,
	operation string,
	err error,
) (ctrl.Result, error) {
	if isSeedGenerationTransientError(err) {
		return requeueWithError(fmt.Errorf("%s: %w", operation, err))
	}
	return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.Failed,
		fmt.Sprintf("%s: %v", operation, err))
}

func (t *provisioningRequestReconcilerTask) reconcileSeedGenerator(
	ctx context.Context,
	snapshot *seedGenerationInputSnapshot,
) (ctrl.Result, error) {
	spokeClient, err := t.verifiedSeedGenerationSpokeClient(ctx, snapshot)
	if err != nil {
		return t.handleSeedGeneratorObservationError(ctx, "failed to create spoke client for LCA", err)
	}
	seedGenerator := newSeedGenerationSeedGenerator()
	key := client.ObjectKey{Name: seedGenerationLCASeedCRName}
	err = spokeClient.Get(ctx, key, seedGenerator)
	if err == nil {
		if err := validateSeedGenerationLCAOwnership(seedGenerator, t.object); err != nil {
			return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed, err.Error())
		}
		if err := validateSeedGeneratorSpec(seedGenerator, snapshot); err != nil {
			return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed, err.Error())
		}
		if seedGenerator.GetDeletionTimestamp() != nil {
			return t.waitForSeedGeneratorStatus(ctx)
		}
		if !t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.SeedGeneratorSubmitted {
			t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.SeedGeneratorSubmitted = true
			if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
				return requeueWithError(fmt.Errorf("failed to record SeedGenerator submission: %w", err))
			}
		}
		return t.observeSeedGenerator(ctx, seedGenerator, snapshot)
	}
	if !apierrors.IsNotFound(err) {
		return t.handleSeedGeneratorObservationError(ctx, "failed to get spoke SeedGenerator", err)
	}
	if t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.SeedGeneratorSubmitted {
		// LCA intentionally removes the Secret and SeedGenerator while the image
		// builder runs. Once submitted, a missing singleton is not recreated.
		return t.waitForSeedGeneratorStatus(ctx)
	}

	seedSecret := &corev1.Secret{}
	secretKey := client.ObjectKey{Name: seedGenerationLCASeedSecretName, Namespace: seedGenerationLCAOperatorNamespace}
	err = spokeClient.Get(ctx, secretKey, seedSecret)
	switch {
	case apierrors.IsNotFound(err):
		credentials := snapshot.Secrets["seedAuth"]
		if credentials == nil || len(credentials.Data[seedGenerationSeedAuthKey]) == 0 {
			return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed,
				"seed generation snapshot is missing seedAuth credentials")
		}
		seedSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      seedGenerationLCASeedSecretName,
				Namespace: seedGenerationLCAOperatorNamespace,
				Labels:    seedGenerationLCAOwnershipLabels(t.object),
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{seedGenerationSeedAuthKey: append([]byte(nil), credentials.Data[seedGenerationSeedAuthKey]...)},
		}
		if err := spokeClient.Create(ctx, seedSecret); err != nil {
			if apierrors.IsAlreadyExists(err) || apierrors.IsConflict(err) {
				return requeueWithShortInterval(), nil
			}
			return t.handleSeedGenerationLifecycleError(ctx, "failed to create spoke seedgen Secret", err)
		}
	case err != nil:
		return t.handleSeedGenerationLifecycleError(ctx, "failed to get spoke seedgen Secret", err)
	default:
		if err := validateSeedGenerationLCAOwnership(seedSecret, t.object); err != nil {
			return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed, err.Error())
		}
		if seedSecret.DeletionTimestamp != nil {
			return requeueWithShortInterval(), nil
		}
		credentials := snapshot.Secrets["seedAuth"]
		if credentials == nil || !bytes.Equal(seedSecret.Data[seedGenerationSeedAuthKey], credentials.Data[seedGenerationSeedAuthKey]) {
			return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed,
				"existing spoke seedgen Secret does not match the frozen seedAuth credentials")
		}
	}

	seedGenerator = desiredSeedGenerator(t.object, snapshot)
	// Persist intent before Create. If the response is lost, or LCA removes
	// the CR before the next reconcile, it must never be submitted twice.
	t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.SeedGeneratorSubmitted = true
	setSeedGenerationProgress(t.object, provisioningv1alpha1.CRconditionReasons.InProgress,
		"Starting LCA seed image generation and push")
	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return requeueWithError(fmt.Errorf("failed to record SeedGenerator submission intent: %w", err))
	}
	if err := spokeClient.Create(ctx, seedGenerator); err != nil {
		if apierrors.IsAlreadyExists(err) || apierrors.IsConflict(err) {
			return requeueWithShortInterval(), nil
		}
		if apierrors.IsTooManyRequests(err) {
			// The API rejected this request before creating the CR. Persist the
			// cleared guard so the next reconcile can retry the submission.
			t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.SeedGeneratorSubmitted = false
			setSeedGenerationProgress(t.object, provisioningv1alpha1.CRconditionReasons.InProgress,
				"Waiting to retry SeedGenerator submission after API throttling")
			if statusErr := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); statusErr != nil {
				return requeueWithError(fmt.Errorf("failed to record rejected SeedGenerator submission: %w", statusErr))
			}
		}
		// A definitive admission rejection cannot have created the CR.
		// Transport errors are ambiguous and leave the one-shot guard set.
		if apierrors.IsInvalid(err) || apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
			t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.SeedGeneratorSubmitted = false
		}
		return t.handleSeedGenerationLifecycleError(ctx, "failed to create spoke SeedGenerator", err)
	}
	return requeueWithShortInterval(), nil
}

func (t *provisioningRequestReconcilerTask) handleSeedGeneratorObservationError(
	ctx context.Context, operation string, err error,
) (ctrl.Result, error) {
	if t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.SeedGeneratorSubmitted && isSeedGenerationTransientError(err) {
		// LCA stops the spoke API during image creation. Report the observation
		// gap without claiming that the image build or push has completed.
		if statusErr := t.updateSeedGenerationProgress(ctx, provisioningv1alpha1.CRconditionReasons.InProgress,
			"Waiting to reconnect to the spoke API and observe LCA seed generation completion; API downtime is expected during image creation"); statusErr != nil {
			return requeueWithError(statusErr)
		}
	}
	return t.handleSeedGenerationLifecycleError(ctx, operation, err)
}

func (t *provisioningRequestReconcilerTask) waitForSeedGeneratorStatus(ctx context.Context) (ctrl.Result, error) {
	if err := t.updateSeedGenerationProgress(ctx, provisioningv1alpha1.CRconditionReasons.InProgress,
		"Waiting for LCA seed generation status; LCA temporarily removes the SeedGenerator during image creation"); err != nil {
		return requeueWithError(err)
	}
	return requeueWithMediumInterval(), nil
}

func desiredSeedGenerator(pr *provisioningv1alpha1.ProvisioningRequest, snapshot *seedGenerationInputSnapshot) *unstructured.Unstructured {
	desired := newSeedGenerationSeedGenerator()
	desired.SetName(seedGenerationLCASeedCRName)
	desired.SetLabels(seedGenerationLCAOwnershipLabels(pr))
	desired.Object["spec"] = seedGenerationSeedGeneratorSpec(snapshot)
	return desired
}

func validateSeedGeneratorSpec(seedGenerator *unstructured.Unstructured, snapshot *seedGenerationInputSnapshot) error {
	seedImage, found, err := unstructured.NestedString(seedGenerator.Object, "spec", "seedImage")
	expectedSeedImage, _ := snapshot.Document.SeedGeneration["seedImage"].(string)
	if err != nil || !found || seedImage != expectedSeedImage {
		return fmt.Errorf("existing spoke SeedGenerator spec does not match the frozen seed generation configuration")
	}
	recertImage, _, err := unstructured.NestedString(seedGenerator.Object, "spec", "recertImage")
	if err != nil || recertImage != seedGenerationRecertImage(snapshot) {
		return fmt.Errorf("existing spoke SeedGenerator spec does not match the frozen seed generation configuration")
	}
	return nil
}

func seedGenerationSeedGeneratorSpec(snapshot *seedGenerationInputSnapshot) map[string]any {
	seed := snapshot.Document.SeedGeneration
	spec := map[string]any{"seedImage": seed["seedImage"].(string)}
	if recertImage := seedGenerationRecertImage(snapshot); recertImage != "" {
		spec["recertImage"] = recertImage
	}
	return spec
}

func seedGenerationRecertImage(snapshot *seedGenerationInputSnapshot) string {
	recertImage, _ := snapshot.Document.SeedGeneration["recertImage"].(string)
	return recertImage
}

func (t *provisioningRequestReconcilerTask) observeSeedGenerator(
	ctx context.Context,
	seedGenerator *unstructured.Unstructured,
	snapshot *seedGenerationInputSnapshot,
) (ctrl.Result, error) {
	conditions, _, _ := unstructured.NestedSlice(seedGenerator.Object, "status", "conditions")
	completed := seedGenerationLCACondition(conditions, seedGenerationLCACompleted)
	if completed != nil && completed.Status == metav1.ConditionFalse && completed.Reason == "Failed" {
		t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.SeedGeneratorFinished = true
		return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.Failed,
			fmt.Sprintf("LCA seed generation failed: %s", completed.Message))
	}
	if completed != nil && completed.Status == metav1.ConditionTrue && completed.Reason == "Completed" {
		seedImage, ok := snapshot.Document.SeedGeneration["seedImage"].(string)
		if !ok || strings.TrimSpace(seedImage) == "" {
			return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.Failed,
				"completed LCA SeedGenerator has no configured seed image reference")
		}
		status := t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
		status.SeedGeneratorFinished = true
		if status.SeedImage != "" && status.SeedImage != seedImage {
			return t.failSeedGeneration(ctx, provisioningv1alpha1.CRconditionReasons.Failed,
				"persisted seed image reference does not match the frozen seed generation configuration")
		}
		status.SeedImage = seedImage
		message := "Seed image generation and push completed"
		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.SeedGenerationCompleted,
			provisioningv1alpha1.CRconditionReasons.Completed, metav1.ConditionTrue, message)
		ctlrutils.SetProvisioningStateFulfilled(t.object)
		if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
			return requeueWithError(fmt.Errorf("failed to record seed generation completion: %w", err))
		}
		return requeueWithShortInterval(), nil
	}
	inProgress := seedGenerationLCACondition(conditions, seedGenerationLCAInProgress)
	message := "Waiting for LCA to report seed generation progress"
	if inProgress != nil && strings.TrimSpace(inProgress.Message) != "" {
		message = "LCA seed generation last reported: " + inProgress.Message
	}
	if err := t.updateSeedGenerationProgress(ctx, provisioningv1alpha1.CRconditionReasons.InProgress, message); err != nil {
		return requeueWithError(err)
	}
	return requeueWithMediumInterval(), nil
}

func seedGenerationLCACondition(rawConditions []any, conditionType string) *metav1.Condition {
	for _, raw := range rawConditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		gotType, _, _ := unstructured.NestedString(condition, "type")
		if gotType != conditionType {
			continue
		}
		status, _, _ := unstructured.NestedString(condition, "status")
		reason, _, _ := unstructured.NestedString(condition, "reason")
		message, _, _ := unstructured.NestedString(condition, "message")
		return &metav1.Condition{Type: gotType, Status: metav1.ConditionStatus(status), Reason: reason, Message: message}
	}
	return nil
}

func newSeedGenerationSeedGenerator() *unstructured.Unstructured {
	object := &unstructured.Unstructured{Object: map[string]any{}}
	object.SetAPIVersion(seedGenerationLCAAPIVersion)
	object.SetKind(seedGenerationLCAKind)
	return object
}

func newSeedGenerationKlusterlet() *unstructured.Unstructured {
	object := &unstructured.Unstructured{Object: map[string]any{}}
	object.SetAPIVersion(seedGenerationKlusterletAPIVersion)
	object.SetKind(seedGenerationKlusterletKind)
	object.SetName(seedGenerationKlusterletName)
	return object
}

func (t *provisioningRequestReconcilerTask) cleanupSeedGenerationDetachedResources(ctx context.Context) (bool, error) {
	if t.object.Status.Extensions.ClusterDetails == nil ||
		t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus == nil ||
		!t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus.DetachmentStarted {
		return false, nil
	}
	snapshot, err := t.loadSeedGenerationInputSnapshot(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to load seed generation snapshot for spoke cleanup: %w", err)
	}
	spokeClient, err := t.verifiedSeedGenerationSpokeClient(ctx, snapshot)
	if err != nil {
		return false, err
	}
	clean := true
	status := t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
	seedGenerator := newSeedGenerationSeedGenerator()
	if err := spokeClient.Get(ctx, client.ObjectKey{Name: seedGenerationLCASeedCRName}, seedGenerator); err == nil {
		if validateSeedGenerationLCAOwnership(seedGenerator, t.object) == nil {
			if !status.SeedGeneratorFinished {
				conditions, _, _ := unstructured.NestedSlice(seedGenerator.Object, "status", "conditions")
				completed := seedGenerationLCACondition(conditions, seedGenerationLCACompleted)
				if completed == nil || !((completed.Status == metav1.ConditionTrue && completed.Reason == "Completed") ||
					(completed.Status == metav1.ConditionFalse && completed.Reason == "Failed")) {
					// Deleting the CR does not cancel LCA's host process. Wait
					// until LCA has finished restoring its CR and credentials.
					return true, nil
				}
				status.SeedGeneratorFinished = true
				if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
					return false, fmt.Errorf("failed to record LCA termination before cleanup: %w", err)
				}
			}
			clean = false
			if err := deleteSeedGenerationObject(ctx, spokeClient, seedGenerator, metav1.DeletePropagationBackground); err != nil {
				return false, err
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("failed to get spoke SeedGenerator during cleanup: %w", err)
	} else if status.SeedGeneratorSubmitted && !status.SeedGeneratorFinished {
		return true, nil
	}
	seedSecret := &corev1.Secret{}
	secretKey := client.ObjectKey{Name: seedGenerationLCASeedSecretName, Namespace: seedGenerationLCAOperatorNamespace}
	if err := spokeClient.Get(ctx, secretKey, seedSecret); err == nil {
		if validateSeedGenerationLCAOwnership(seedSecret, t.object) == nil {
			clean = false
			if err := deleteSeedGenerationObject(ctx, spokeClient, seedSecret, metav1.DeletePropagationBackground); err != nil {
				return false, err
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("failed to get spoke seedgen Secret during cleanup: %w", err)
	}
	return !clean, nil
}

// cleanupSeedGenerationDetachedResourcesAndRecordCompletion records completion before
// snapshot resources are removed. Finalizer retries can then proceed without
// reloading the spoke kubeconfig from a snapshot that was already deleted.
func (t *provisioningRequestReconcilerTask) cleanupSeedGenerationDetachedResourcesAndRecordCompletion(
	ctx context.Context,
) (bool, error) {
	details := t.object.Status.Extensions.ClusterDetails
	if details == nil || details.SeedGenerationStatus == nil ||
		!details.SeedGenerationStatus.DetachmentStarted || details.SeedGenerationStatus.DetachedCleanupCompleted {
		return false, nil
	}
	pending, err := t.cleanupSeedGenerationDetachedResources(ctx)
	if err != nil || pending {
		return pending, err
	}
	details.SeedGenerationStatus.DetachedCleanupCompleted = true
	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return false, fmt.Errorf("failed to record spoke seed generation cleanup completion: %w", err)
	}
	return false, nil
}
