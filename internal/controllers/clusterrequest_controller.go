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
	"maps"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// clusterRequestReconciler reconciles a ClusterRequest object
type ClusterRequestReconciler struct {
	client.Client
	Logger *slog.Logger
}

type clusterRequestReconcilerTask struct {
	logger *slog.Logger
	client client.Client
	object *oranv1alpha1.ClusterRequest
}

//+kubebuilder:rbac:groups=oran.openshift.io,resources=clusterrequests,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=oran.openshift.io,resources=clusterrequests/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=oran.openshift.io,resources=clusterrequests/finalizers,verbs=update

//+kubebuilder:rbac:groups=oran.openshift.io,resources=clustertemplates,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ClusterRequest object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *ClusterRequestReconciler) Reconcile(
	ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)

	// Fetch the object:
	object := &oranv1alpha1.ClusterRequest{}
	if err := r.Client.Get(ctx, req.NamespacedName, object); err != nil {
		if errors.IsNotFound(err) {
			err = nil
			return ctrl.Result{RequeueAfter: 5 * time.Minute}, err
		}
		r.Logger.ErrorContext(
			ctx,
			"Unable to fetch Cluster Request",
			slog.String("error", err.Error()),
		)
	}

	r.Logger.InfoContext(ctx, "[Reconcile Cluster Request] "+object.Name)

	// Create and run the task:
	task := &clusterRequestReconcilerTask{
		logger: r.Logger,
		client: r.Client,
		object: object,
	}
	result, err = task.run(ctx)
	return
}

func (t *clusterRequestReconcilerTask) run(ctx context.Context) (nextReconcile ctrl.Result, err error) {
	// Set the default reconcile time to 5 minutes.
	nextReconcile = ctrl.Result{RequeueAfter: 5 * time.Minute}

	// ### JSON VALIDATION ###

	// Check if the clusterTemplateInput is in a JSON format; the schema itself is not of importance.
	err = utils.ValidateInputDataSchema(t.object.Spec.ClusterTemplateInput)

	// If there is an error, log it and stop the reconciliation.
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"clusterTemplateInput is not in a JSON format",
		)
		return ctrl.Result{}, nil
	}

	// Update the ClusterRequest status.
	err = t.updateClusterTemplateInputValidationStatus(ctx, err)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to update the ClusterTemplateInputValidation status for ClusterRequest",
			slog.String("name", t.object.Name),
		)
		return ctrl.Result{}, nil
	}

	// ### VALIDATION AGAINST CLUSTER TEMPLATE REF ###

	// Check if the clusterTemplateInput matches the inputDataSchema of the ClusterTemplate
	// specified in spec.clusterTemplateRef.
	err = t.validateClusterTemplateInputMatchesClusterTemplate(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			err.Error(),
		)
		return ctrl.Result{}, nil
	}

	// Update the ClusterRequest status.
	err = t.updateClusterTemplateMatchStatus(ctx, err)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to update the ClusterTemplateValidation status for ClusterRequest",
			slog.String("name", t.object.Name),
		)
		return ctrl.Result{}, nil
	}

	// ### CLUSTERINSTANCE GENERATION ###
	renderedClusterInstance, renderErr := t.renderClusterInstanceTemplate(ctx)
	if renderErr != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to render the ClusterInstance template for ClusterInstance",
			slog.String("name", t.object.Name),
			slog.String("error", renderErr.Error()),
		)
		renderErr = fmt.Errorf("failed to render the ClusterInstance template: %s", renderErr.Error())
	}

	err = t.updateRenderTemplateStatus(ctx, renderErr)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to update the RenderTemplate status for ClusterRequest",
			slog.String("name", t.object.Name),
		)
		return
	}
	if renderErr != nil {
		return ctrl.Result{}, nil
	}

	// Create/Update the ClusterInstance CR
	createErr := utils.CreateK8sCR(ctx, t.client, renderedClusterInstance, nil, utils.UPDATE)
	if createErr != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create/update the ClusterInstance",
			slog.String("name", renderedClusterInstance.GetName()),
			slog.String("namespace", renderedClusterInstance.GetNamespace()),
			slog.String("error", createErr.Error()),
		)
		createErr = fmt.Errorf("failed to create/update the rendered ClusterInstance: %s", createErr.Error())
	}

	err = t.updateRenderTemplateAppliedStatus(ctx, createErr)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to update the RenderTemplateApplied status for ClusterRequest",
			slog.String("name", t.object.Name),
		)
		return
	}
	if createErr != nil {
		return ctrl.Result{}, nil
	}
	return
}

func (t *clusterRequestReconcilerTask) renderClusterInstanceTemplate(ctx context.Context) (*unstructured.Unstructured, error) {
	t.logger.InfoContext(
		ctx,
		"Rendering ClusterInstance template",
	)

	// Unmarshal the clusterTemplateInput JSON string into a map
	clusterTemplateInputMap := make(map[string]any)
	err := utils.UnmarshalYAMLOrJSONString(t.object.Spec.ClusterTemplateInput, &clusterTemplateInputMap)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal clusterTemplateInput: %w", err)
	}

	ctClusterInstanceDefaultsMap := make(map[string]any)
	ctClusterInstanceDefaultsCm := &corev1.ConfigMap{}
	err = t.client.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-clusterinstance-defaults", t.object.Spec.ClusterTemplateRef),
		Namespace: t.object.Namespace,
	}, ctClusterInstanceDefaultsCm)
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get configmap, err: %w", err)
		}
	} else {
		defaults, exists := ctClusterInstanceDefaultsCm.Data["clusterinstance-defaults"]
		if !exists {
			return nil, fmt.Errorf("clusterinstance-defaults does not exist in configmap data")
		}
		// Unmarshal the clusterinstance defaults JSON string into a map
		err := utils.UnmarshalYAMLOrJSONString(defaults, &ctClusterInstanceDefaultsMap)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal configmap data, err: %w", err)
		}
	}

	mergedClusterInstanceData, err := t.buildClusterInstanceData(clusterTemplateInputMap, ctClusterInstanceDefaultsMap)
	if err != nil {
		return nil, fmt.Errorf("failed to build ClusterInstance data, err: %w", err)
	}

	// TODO: validateClusterTemplateInputMatchesClusterTemplate for mergedClusterInstanceMap

	renderedClusterInstance, err := utils.RenderTemplateForK8sCR("ClusterInstance", utils.ClusterInstanceTemplatePath, mergedClusterInstanceData)
	if err != nil {
		return nil, fmt.Errorf("failed to render the template, err: %w", err)
	}

	t.logger.InfoContext(
		ctx,
		"ClusterInstance template is rendered successfully",
		slog.String("name", renderedClusterInstance.GetName()),
		slog.String("namespace", renderedClusterInstance.GetNamespace()),
	)
	return renderedClusterInstance, nil
}

// buildClusterInstanceData returns an object that is consumed for rendering clusterinstance template
func (t *clusterRequestReconcilerTask) buildClusterInstanceData(
	clusterTemplateInput, ctClusterInstanceDefaults map[string]any) (map[string]any, error) {
	// Merging the clusterTemplateInput data with defaults
	// TODO: create custom function for merging
	mergedClusterInstanceMap := maps.Clone(ctClusterInstanceDefaults)
	maps.Copy(mergedClusterInstanceMap, clusterTemplateInput)

	// Wrap the data in a map with key "Cluster"
	mergedClusterInstanceData := map[string]any{
		"Cluster": mergedClusterInstanceMap,
	}

	return mergedClusterInstanceData, nil
}

// validateClusterTemplateInput validates if the clusterTemplateInput matches the
// inputDataSchema of the ClusterTemplate
func (t *clusterRequestReconcilerTask) validateClusterTemplateInputMatchesClusterTemplate(
	ctx context.Context) error {

	// Check the clusterTemplateRef references an existing template in the same namespace
	// as the current clusterRequest.
	clusterTemplateRef := &oranv1alpha1.ClusterTemplate{}
	clusterTemplateRefExists, err := utils.DoesK8SResourceExist(
		ctx, t.client, t.object.Spec.ClusterTemplateRef, t.object.Namespace, clusterTemplateRef)

	// If there was an error in trying to get the ClusterTemplate, return it.
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Error obtaining the ClusterTemplate referenced by ClusterTemplateRef",
			slog.String("clusterTemplateRef", t.object.Spec.ClusterTemplateRef),
		)
		return err
	}

	// If the referenced ClusterTemplate does not exist, log and return an appropriate error.
	if !clusterTemplateRefExists {
		err := fmt.Errorf(
			fmt.Sprintf(
				"The referenced ClusterTemplate (%s) does not exist in the %s namespace",
				t.object.Spec.ClusterTemplateRef, t.object.Namespace))

		t.logger.ErrorContext(
			ctx,
			err.Error())
		return err
	}

	// Check that the clusterTemplateInput matches the inputDataSchema from the ClusterTemplate.
	err = utils.ValidateJsonAgainstJsonSchema(
		clusterTemplateRef.Spec.InputDataSchema, t.object.Spec.ClusterTemplateInput)

	if err != nil {
		t.logger.ErrorContext(
			ctx,
			fmt.Sprintf(
				"The provided clusterTemplateInput does not match "+
					"the inputDataSchema from the ClusterTemplateRef (%s)",
				t.object.Spec.ClusterTemplateRef))

		return err
	}

	return nil
}

// updateClusterTemplateInputValidationStatus update the status of the ClusterTemplate object (CR).
func (t *clusterRequestReconcilerTask) updateClusterTemplateInputValidationStatus(
	ctx context.Context, inputError error) error {

	t.object.Status.ClusterTemplateInputValidation.InputIsValid = true
	t.object.Status.ClusterTemplateInputValidation.InputError = ""

	if inputError != nil {
		t.object.Status.ClusterTemplateInputValidation.InputIsValid = false
		t.object.Status.ClusterTemplateInputValidation.InputError = inputError.Error()
	}

	return utils.UpdateK8sCRStatus(ctx, t.client, t.object)
}

// updateClusterTemplateMatchStatus update the status of the ClusterTemplate object (CR).
func (t *clusterRequestReconcilerTask) updateClusterTemplateMatchStatus(
	ctx context.Context, inputError error) error {

	t.object.Status.ClusterTemplateInputValidation.InputMatchesTemplate = true
	t.object.Status.ClusterTemplateInputValidation.InputMatchesTemplateError = ""

	if inputError != nil {
		t.object.Status.ClusterTemplateInputValidation.InputMatchesTemplate = false
		t.object.Status.ClusterTemplateInputValidation.InputMatchesTemplateError = inputError.Error()
	}

	return utils.UpdateK8sCRStatus(ctx, t.client, t.object)
}

// updateRenderTemplateStatus update the status of the ClusterRequest object (CR).
func (t *clusterRequestReconcilerTask) updateRenderTemplateStatus(
	ctx context.Context, inputError error) error {

	if t.object.Status.RenderedTemplateStatus == nil {
		t.object.Status.RenderedTemplateStatus = &oranv1alpha1.RenderedTemplateStatus{}
	}
	t.object.Status.RenderedTemplateStatus.RenderedTemplate = true
	t.object.Status.RenderedTemplateStatus.RenderedTemplateError = ""

	if inputError != nil {
		t.object.Status.RenderedTemplateStatus.RenderedTemplate = false
		t.object.Status.RenderedTemplateStatus.RenderedTemplateError = inputError.Error()
	}

	return utils.UpdateK8sCRStatus(ctx, t.client, t.object)
}

// updateRenderTemplateAppliedStatus update the status of the ClusterRequest object (CR).
func (t *clusterRequestReconcilerTask) updateRenderTemplateAppliedStatus(
	ctx context.Context, inputError error) error {
	if t.object.Status.RenderedTemplateStatus == nil {
		t.object.Status.RenderedTemplateStatus = &oranv1alpha1.RenderedTemplateStatus{}
	}

	t.object.Status.RenderedTemplateStatus.RenderedTemplateApplied = true
	t.object.Status.RenderedTemplateStatus.RenderedTemplateAppliedError = ""

	if inputError != nil {
		t.object.Status.RenderedTemplateStatus.RenderedTemplateApplied = false
		t.object.Status.RenderedTemplateStatus.RenderedTemplateAppliedError = inputError.Error()
	}

	return utils.UpdateK8sCRStatus(ctx, t.client, t.object)
}

// findClusterRequestsForClusterTemplate maps the ClusterTemplates used by ClusterRequests
// to reconciling requests for those ClusterRequests.
func (r *ClusterRequestReconciler) findClusterRequestsForClusterTemplate(
	ctx context.Context, clusterTemplate client.Object) []reconcile.Request {

	// Empty array of reconciling requests.
	reqs := make([]reconcile.Request, 0)
	// Get all the clusterRequests.
	clusterRequests := &oranv1alpha1.ClusterRequestList{}
	err := r.Client.List(ctx, clusterRequests, client.InNamespace(clusterTemplate.GetNamespace()))
	if err != nil {
		return reqs
	}

	// Create reconciling requests only for the clusterRequests that are using the
	// current clusterTemplate.
	for _, clusterRequest := range clusterRequests.Items {
		if clusterRequest.Spec.ClusterTemplateRef == clusterTemplate.GetName() {
			reqs = append(
				reqs,
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: clusterRequest.Namespace,
						Name:      clusterRequest.Name,
					},
				},
			)
		}
	}

	return reqs
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("orano2ims-cluster-request").
		For(
			&oranv1alpha1.ClusterRequest{},
			// Watch for create and update event for ClusterRequest.
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&oranv1alpha1.ClusterTemplate{},
			handler.EnqueueRequestsFromMapFunc(r.findClusterRequestsForClusterTemplate),
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return true },
			})).
		Complete(r)
}
