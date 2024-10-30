/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ProvisioningRequestReconciler reconciles a ProvisioningRequest object
type ProvisioningRequestReconciler struct {
	client.Client
	Logger *slog.Logger
}

type provisioningRequestReconcilerTask struct {
	logger       *slog.Logger
	client       client.Client
	object       *provisioningv1alpha1.ProvisioningRequest
	clusterInput *clusterInput
	ctNamespace  string
	timeouts     *timeouts
}

// clusterInput holds the merged input data for a cluster
type clusterInput struct {
	clusterInstanceData map[string]any
	policyTemplateData  map[string]any
}

// timeouts holds the timeout values, in minutes,
// for hardware provisioning, cluster provisioning
// and cluster configuration.
type timeouts struct {
	hardwareProvisioning time.Duration
	clusterProvisioning  time.Duration
	clusterConfiguration time.Duration
}

const (
	provisioningRequestFinalizer = "provisioningrequest.o2ims.provisioning.oran.org/finalizer"
	provisioningRequestNameLabel = "provisioningrequest.o2ims.provisioning.oran.org/name"
)

func getClusterTemplateRefName(name, version string) string {
	return fmt.Sprintf("%s.%s", name, version)
}

//+kubebuilder:rbac:groups=o2ims.provisioning.oran.org,resources=provisioningrequests,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims.provisioning.oran.org,resources=provisioningrequests/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=o2ims.provisioning.oran.org,resources=provisioningrequests/finalizers,verbs=update
//+kubebuilder:rbac:groups=o2ims.provisioning.oran.org,resources=clustertemplates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=siteconfig.open-cluster-management.io,resources=clusterinstances,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodepools,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodepools/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;create;update;patch;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;create;update;patch;watch
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies,verbs=list;watch
//+kubebuilder:rbac:groups=lcm.openshift.io,resources=imagebasedgroupupgrades,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=lcm.openshift.io,resources=imagebasedgroupupgrades/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ProvisioningRequest object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *ProvisioningRequestReconciler) Reconcile(
	ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)
	result = doNotRequeue()

	// Reconciliation loop can be triggered multiple times for the same resource
	// due to changes in related resources, events or conditions.
	// Wait a bit so that API server/etcd syncs up and this reconcile has a
	// better chance of getting the latest resources.
	time.Sleep(100 * time.Millisecond)

	// Fetch the object:
	object := &provisioningv1alpha1.ProvisioningRequest{}
	if err = r.Client.Get(ctx, req.NamespacedName, object); err != nil {
		if errors.IsNotFound(err) {
			// The provisioning request could have been deleted
			err = nil
			return
		}
		r.Logger.ErrorContext(
			ctx,
			"Unable to fetch ProvisioningRequest",
			slog.String("error", err.Error()),
		)
		return
	}

	r.Logger.InfoContext(ctx, "[Reconcile ProvisioningRequest]",
		"name", object.Name, "namespace", object.Namespace)

	if res, stop, err := r.handleFinalizer(ctx, object); !res.IsZero() || stop || err != nil {
		if err != nil {
			r.Logger.ErrorContext(
				ctx,
				"Encountered error while handling the ProvisioningRequest finalizer",
				slog.String("err", err.Error()))
		}
		return res, err
	}

	// Create and run the task:
	task := &provisioningRequestReconcilerTask{
		logger:       r.Logger,
		client:       r.Client,
		object:       object,
		clusterInput: &clusterInput{},
		ctNamespace:  "",
		timeouts:     &timeouts{},
	}
	result, err = task.run(ctx)
	return
}

func (t *provisioningRequestReconcilerTask) run(ctx context.Context) (ctrl.Result, error) {
	// Validate the ProvisioningRequest
	err := t.handleValidation(ctx)
	if err != nil {
		if utils.IsInputError(err) {
			return t.checkClusterDeployConfigState(ctx)
		}
		// internal error that might recover
		return requeueWithError(err)
	}

	// Render and validate ClusterInstance
	renderedClusterInstance, err := t.handleRenderClusterInstance(ctx)
	if err != nil {
		if utils.IsInputError(err) {
			return t.checkClusterDeployConfigState(ctx)
		}
		return requeueWithError(err)
	}

	// Handle the creation of resources required for cluster deployment
	err = t.handleClusterResources(ctx, renderedClusterInstance)
	if err != nil {
		if utils.IsInputError(err) {
			_, err = t.checkClusterDeployConfigState(ctx)
			if err != nil {
				return requeueWithError(err)
			}
			// Requeue since we are not watching for updates to required resources
			// if they are missing
			return requeueWithMediumInterval(), nil
		}
		return requeueWithError(err)
	}

	// Render the hardware template for NodePool
	renderedNodePool, err := t.renderHardwareTemplate(ctx, renderedClusterInstance)
	if err != nil {
		if utils.IsInputError(err) {
			return t.checkClusterDeployConfigState(ctx)
		}
		return requeueWithError(err)
	}

	// Create/Update the NodePool
	err = t.createNodePoolResources(ctx, renderedNodePool)
	if err != nil {
		return requeueWithError(err)
	}

	// wait for the NodePool to be provisioned and update BMC details in ClusterInstance
	provisioned, timedOutOrFailed, err := t.waitForHardwareData(ctx, renderedClusterInstance, renderedNodePool)
	if err != nil {
		return requeueWithError(err)
	}
	if timedOutOrFailed {
		// Timeout occurred or failed, stop requeuing
		return doNotRequeue(), nil
	}
	if !provisioned {
		t.logger.InfoContext(
			ctx,
			fmt.Sprintf(
				"Waiting for NodePool %s in the namespace %s to be provisioned",
				renderedNodePool.GetName(),
				renderedNodePool.GetNamespace(),
			),
		)
		return requeueWithMediumInterval(), nil
	}

	// Handle the cluster install with ClusterInstance
	err = t.handleClusterInstallation(ctx, renderedClusterInstance)
	if err != nil {
		return requeueWithError(err)
	}

	// Handle policy configuration only after the cluster provisioning
	// has started, and not failed or timedout (completed, in-progress or unknown)
	if utils.IsClusterProvisionPresent(t.object) &&
		!utils.IsClusterProvisionTimedOutOrFailed(t.object) {

		// Handle configuration through policies.
		requeue, err := t.handleClusterPolicyConfiguration(ctx)
		if err != nil {
			return requeueWithError(err)
		}

		// Requeue if cluster provisioning is not completed (in-progress or unknown)
		// or there are enforce policies that are not Compliant.
		if !utils.IsClusterProvisionCompleted(t.object) || requeue {
			return requeueWithLongInterval(), nil
		}

		shouldUpgrade, err := t.IsUpgradeRequested(ctx, renderedClusterInstance.GetName())
		if err != nil {
			return requeueWithError(err)
		}

		if utils.IsClusterUpgradeInitiated(t.object) && !utils.IsClusterUpgradeCompleted(t.object) ||
			utils.IsClusterProvisionCompleted(t.object) && shouldUpgrade {
			t.logger.InfoContext(
				ctx,
				"Upgrade requested. Start handling upgrade.",
			)
			requeue, err := t.handleUpgrade(ctx, renderedClusterInstance)
			if err != nil {
				return requeueWithError(err)
			}
			return requeue, nil
		}

	}

	return doNotRequeue(), nil
}

// checkClusterDeployConfigState checks the current deployment and configuration state of
// the cluster by evaluating the statuses of related resources like NodePool, ClusterInstance
// and policy configuration when applicable, and update the corresponding ProvisioningRequest
// status conditions
func (t *provisioningRequestReconcilerTask) checkClusterDeployConfigState(ctx context.Context) (result ctrl.Result, err error) {
	// Check the NodePool status if exists
	if t.object.Status.NodePoolRef == nil {
		if err = t.checkResourcePreparationStatus(ctx); err != nil {
			return requeueWithError(err)
		}
		return doNotRequeue(), nil
	}
	nodePool := &hwv1alpha1.NodePool{}
	nodePool.SetName(t.object.Status.NodePoolRef.Name)
	nodePool.SetNamespace(t.object.Status.NodePoolRef.Namespace)
	hwProvisioned, timedOutOrFailed, err := t.checkNodePoolProvisionStatus(ctx, nodePool)
	if err != nil {
		return requeueWithError(err)
	}
	if timedOutOrFailed {
		if err = t.checkResourcePreparationStatus(ctx); err != nil {
			return requeueWithError(err)
		}
		// Timeout occurred or failed, stop requeuing
		return doNotRequeue(), nil
	}
	if !hwProvisioned {
		return requeueWithMediumInterval(), nil
	}

	// Check the ClusterInstance status if exists
	if t.object.Status.ClusterDetails != nil {
		err = t.checkClusterProvisionStatus(
			ctx, t.object.Status.ClusterDetails.Name)
		if err != nil {
			return requeueWithError(err)
		}

		// Check the policy configuration status only after the cluster provisioning
		// has started, and not failed or timedout
		if utils.IsClusterProvisionPresent(t.object) &&
			!utils.IsClusterProvisionTimedOutOrFailed(t.object) {
			requeue, err := t.handleClusterPolicyConfiguration(ctx)
			if err != nil {
				return requeueWithError(err)
			}
			// Requeue if Cluster Provisioned is not completed (in-progress or unknown)
			// or there are enforce policies that are not Compliant
			if !utils.IsClusterProvisionCompleted(t.object) || requeue {
				return requeueWithLongInterval(), nil
			}
		}
	}

	if err = t.checkResourcePreparationStatus(ctx); err != nil {
		return requeueWithError(err)
	}
	return doNotRequeue(), nil
}

// checkResourcePreparationStatus checks for validation and preparation failures, setting the
// provisioningState to failed if no provisioning is currently in progress and issues are found.
func (t *provisioningRequestReconcilerTask) checkResourcePreparationStatus(ctx context.Context) error {
	conditionTypes := []utils.ConditionType{
		utils.PRconditionTypes.Validated,
		utils.PRconditionTypes.ClusterInstanceRendered,
		utils.PRconditionTypes.ClusterResourcesCreated,
		utils.PRconditionTypes.HardwareTemplateRendered,
	}

	for _, condType := range conditionTypes {
		cond := meta.FindStatusCondition(t.object.Status.Conditions, string(condType))
		if cond != nil && cond.Status == metav1.ConditionFalse {
			// Set the provisioning state to failed if any condition is false
			utils.SetProvisioningStateFailed(t.object, cond.Message)
			break
		}
	}

	if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, err)
	}
	return nil
}

func (t *provisioningRequestReconcilerTask) handleValidation(ctx context.Context) error {
	// Validate provisioning request CR
	err := t.validateProvisioningRequestCR(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to validate the ProvisioningRequest",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.Validated,
			utils.CRconditionReasons.Failed,
			metav1.ConditionFalse,
			"Failed to validate the ProvisioningRequest: "+err.Error(),
		)
	} else {
		t.logger.InfoContext(
			ctx,
			"Validated the ProvisioningRequest CR",
			slog.String("name", t.object.Name),
		)
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.Validated,
			utils.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"The provisioning request validation succeeded",
		)
	}

	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
	}

	return err
}

// handleRenderClusterInstance handles the ClusterInstance rendering and validation.
func (t *provisioningRequestReconcilerTask) handleRenderClusterInstance(ctx context.Context) (*siteconfig.ClusterInstance, error) {
	renderedClusterInstance, err := t.renderClusterInstanceTemplate(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to render and validate the ClusterInstance for ProvisioningRequest",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ClusterInstanceRendered,
			utils.CRconditionReasons.Failed,
			metav1.ConditionFalse,
			"Failed to render and validate ClusterInstance: "+err.Error(),
		)
	} else {
		t.logger.InfoContext(
			ctx,
			"Successfully rendered the ClusterInstance and validated it with dry-run",
			slog.String("name", t.object.Name),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ClusterInstanceRendered,
			utils.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"ClusterInstance rendered and passed dry-run validation",
		)
	}

	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return nil, fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to handle ClusterInstance rendering and validation: %w", err)
	}
	return renderedClusterInstance, nil
}

func (t *provisioningRequestReconcilerTask) handleClusterResources(ctx context.Context, clusterInstance *siteconfig.ClusterInstance) error {
	err := t.createOrUpdateClusterResources(ctx, clusterInstance)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to apply the required cluster resource for ProvisioningRequest",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ClusterResourcesCreated,
			utils.CRconditionReasons.Failed,
			metav1.ConditionFalse,
			"Failed to apply the required cluster resource: "+err.Error(),
		)
	} else {
		t.logger.InfoContext(
			ctx,
			"Applied the required cluster resources for ProvisioningRequest",
			slog.String("name", t.object.Name),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ClusterResourcesCreated,
			utils.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"Cluster resources applied",
		)
	}
	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
	}

	return err
}

func (t *provisioningRequestReconcilerTask) renderHardwareTemplate(ctx context.Context,
	clusterInstance *siteconfig.ClusterInstance) (*hwv1alpha1.NodePool, error) {
	renderedNodePool, err := t.handleRenderHardwareTemplate(ctx, clusterInstance)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to render the Hardware template for NodePool",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.HardwareTemplateRendered,
			utils.CRconditionReasons.Failed,
			metav1.ConditionFalse,
			"Failed to render the Hardware template: "+err.Error(),
		)
	} else {
		t.logger.InfoContext(
			ctx,
			"Successfully rendered Hardware template for NodePool",
			slog.String("name", t.object.Name),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.HardwareTemplateRendered,
			utils.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"Rendered Hardware template successfully",
		)
	}

	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return nil, fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
	}

	return renderedNodePool, err
}

func (t *provisioningRequestReconcilerTask) getCrClusterTemplateRef(ctx context.Context) (*provisioningv1alpha1.ClusterTemplate, error) {
	// Check the clusterTemplateRef references an existing template in the same namespace
	// as the current provisioningRequest.
	clusterTemplateRefName := getClusterTemplateRefName(
		t.object.Spec.TemplateName, t.object.Spec.TemplateVersion)
	clusterTemplates := &provisioningv1alpha1.ClusterTemplateList{}

	// Get the one clusterTemplate that's valid.
	err := t.client.List(ctx, clusterTemplates)
	// If there was an error in trying to get the ClusterTemplate, return it.
	if err != nil {
		return nil, fmt.Errorf("failed to get ClusterTemplate: %w", err)
	}
	for _, ct := range clusterTemplates.Items {
		if ct.Name == clusterTemplateRefName {
			validatedCond := meta.FindStatusCondition(
				ct.Status.Conditions,
				string(utils.CTconditionTypes.Validated))
			if validatedCond != nil && validatedCond.Status == metav1.ConditionTrue {
				t.ctNamespace = ct.Namespace
				return &ct, nil
			}
		}
	}

	// If the referenced ClusterTemplate does not exist, log and return an appropriate error.
	return nil, utils.NewInputError(
		fmt.Sprintf(
			"a valid (%s) ClusterTemplate does not exist in any namespace",
			clusterTemplateRefName))
}

func (r *ProvisioningRequestReconciler) finalizeProvisioningRequest(
	ctx context.Context, provisioningRequest *provisioningv1alpha1.ProvisioningRequest) error {

	var labels = map[string]string{
		provisioningRequestNameLabel: provisioningRequest.Name,
	}
	listOpts := []client.ListOption{
		client.MatchingLabels(labels),
	}

	// Query the NodePool created by this ProvisioningRequest. Delete it if exists.
	nodePoolList := &hwv1alpha1.NodePoolList{}
	if err := r.Client.List(ctx, nodePoolList, listOpts...); err != nil {
		return fmt.Errorf("failed to list node pools: %w", err)
	}
	for _, nodePool := range nodePoolList.Items {
		copiedNodePool := nodePool
		if err := r.Client.Delete(ctx, &copiedNodePool); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete node pool: %w", err)
		}
	}

	// If the ClusterInstance has been created by this ProvisioningRequest, delete it.
	// The SiteConfig operator will also delete the namespace.
	clusterInstanceList := &siteconfig.ClusterInstanceList{}
	if err := r.Client.List(ctx, clusterInstanceList, listOpts...); err != nil {
		return fmt.Errorf("failed to list cluster instances: %w", err)
	}
	for _, clusterInstance := range clusterInstanceList.Items {
		copiedClusterInstance := clusterInstance
		if err := r.Client.Delete(ctx, &copiedClusterInstance); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete cluster instance: %w", err)
		}
	}

	if len(clusterInstanceList.Items) == 0 {
		// If the ClusterInstance has not been created. Query the namespace created by
		// this ProvisioningRequest. Delete it if exists.
		namespaceList := &corev1.NamespaceList{}
		if err := r.Client.List(ctx, namespaceList, listOpts...); err != nil {
			return fmt.Errorf("failed to list namespaces: %w", err)
		}
		for _, ns := range namespaceList.Items {
			copiedNamespace := ns
			if err := r.Client.Delete(ctx, &copiedNamespace); client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("failed to delete namespace: %w", err)
			}
		}
	}

	return nil
}

func (r *ProvisioningRequestReconciler) handleFinalizer(
	ctx context.Context, provisioningRequest *provisioningv1alpha1.ProvisioningRequest) (ctrl.Result, bool, error) {

	// Check if the ProvisioningRequest is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	if provisioningRequest.DeletionTimestamp.IsZero() {
		// Check and add finalizer for this CR.
		if !controllerutil.ContainsFinalizer(provisioningRequest, provisioningRequestFinalizer) {
			controllerutil.AddFinalizer(provisioningRequest, provisioningRequestFinalizer)
			// Update and requeue since the finalizer has been added.
			if err := r.Update(ctx, provisioningRequest); err != nil {
				return ctrl.Result{}, true, fmt.Errorf("failed to update ProvisioningRequest with finalizer: %w", err)
			}
			return ctrl.Result{Requeue: true}, true, nil
		}
		return ctrl.Result{}, false, nil
	} else if controllerutil.ContainsFinalizer(provisioningRequest, provisioningRequestFinalizer) {
		// Run finalization logic for provisioningRequestFinalizer. If the finalization logic
		// fails, don't remove the finalizer so that we can retry during the next reconciliation.
		if err := r.finalizeProvisioningRequest(ctx, provisioningRequest); err != nil {
			return ctrl.Result{}, true, err
		}

		// Remove provisioningRequestFinalizer. Once all finalizers have been
		// removed, the object will be deleted.
		r.Logger.Info("Removing provisioningRequest finalizer", "name", provisioningRequest.Name)
		patch := client.MergeFrom(provisioningRequest.DeepCopy())
		if controllerutil.RemoveFinalizer(provisioningRequest, provisioningRequestFinalizer) {
			if err := r.Patch(ctx, provisioningRequest, patch); err != nil {
				return ctrl.Result{}, true, fmt.Errorf("failed to patch ProvisioningRequest: %w", err)
			}
			return ctrl.Result{}, true, nil
		}
	}
	return ctrl.Result{}, false, nil
}
