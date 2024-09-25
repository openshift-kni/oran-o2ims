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
	"sort"
	"strings"

	"log/slog"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/google/uuid"
	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// ClusterTemplateReconciler reconciles a ClusterTemplate object
type ClusterTemplateReconciler struct {
	client.Client
	Logger *slog.Logger
}

type clusterTemplateReconcilerTask struct {
	logger *slog.Logger
	client client.Client
	object *provisioningv1alpha1.ClusterTemplate
}

func doNotRequeue() ctrl.Result {
	return ctrl.Result{Requeue: false}
}

func requeueWithError(err error) (ctrl.Result, error) {
	// can not be fixed by user during reconcile
	return ctrl.Result{}, err
}

func requeueWithLongInterval() ctrl.Result {
	return requeueWithCustomInterval(5 * time.Minute)
}

func requeueWithMediumInterval() ctrl.Result {
	return requeueWithCustomInterval(1 * time.Minute)
}

func requeueWithCustomInterval(interval time.Duration) ctrl.Result {
	return ctrl.Result{RequeueAfter: interval}
}

//+kubebuilder:rbac:groups=o2ims.provisioning.oran.org,resources=clustertemplates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims.provisioning.oran.org,resources=clustertemplates/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=o2ims.provisioning.oran.org,resources=clustertemplates/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ClusterTemplate object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile

func (r *ClusterTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (
	result ctrl.Result, err error) {
	_ = log.FromContext(ctx)
	result = doNotRequeue()

	// Fetch the object:
	object := &provisioningv1alpha1.ClusterTemplate{}
	if err = r.Client.Get(ctx, req.NamespacedName, object); err != nil {
		if errors.IsNotFound(err) {
			// The cluster template could have been deleted
			err = nil
			return
		}
		r.Logger.ErrorContext(
			ctx,
			"Unable to fetch ClusterTemplate",
			slog.String("error", err.Error()),
		)
		return
	}

	r.Logger.InfoContext(ctx, "[Reconcile Cluster Template] "+object.Name)

	// Create and run the task:
	task := &clusterTemplateReconcilerTask{
		logger: r.Logger,
		client: r.Client,
		object: object,
	}
	result, err = task.run(ctx)
	return
}

func (t *clusterTemplateReconcilerTask) run(ctx context.Context) (ctrl.Result, error) {
	valid, err := t.validateClusterTemplateCR(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to validate ClusterTemplate",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)
		return requeueWithError(err)
	}
	if !valid {
		// Requeue for invalid clustertemplate
		return requeueWithLongInterval(), nil
	}

	return doNotRequeue(), nil
}

// validateClusterTemplateCR validates the ClusterTemplate CR and updates the Validated status condition.
// It returns true if valid, false otherwise.
func (t *clusterTemplateReconcilerTask) validateClusterTemplateCR(ctx context.Context) (bool, error) {
	var validationErrs []string

	// Validate the ClusterInstance name
	err := validateName(t.client, t.object.Spec.HumanReadableName, t.object.Spec.Version, t.object.Name, t.object.Namespace)
	if err != nil {
		validationErrs = append(validationErrs, err.Error())
	}

	// Validate the ClusterInstance name
	err = validateTemplateID(ctx, t.client, t.object)
	if err != nil {
		validationErrs = append(validationErrs, err.Error())
	}

	// Validate the HW template configmap
	err = validateConfigmapReference[[]hwv1alpha1.NodeGroup](
		ctx, t.client,
		t.object.Spec.Templates.HwTemplate,
		utils.InventoryNamespace,
		utils.HwTemplateNodePool)
	if err != nil {
		if !utils.IsInputError(err) {
			return false, fmt.Errorf("failed to validate the ConfigMap %s for hw template: %w",
				t.object.Spec.Templates.HwTemplate, err)
		}
		validationErrs = append(validationErrs, err.Error())
	}

	// Validate the ClusterInstance defaults configmap
	err = validateConfigmapReference[map[string]any](
		ctx, t.client,
		t.object.Spec.Templates.ClusterInstanceDefaults,
		t.object.Namespace,
		utils.ClusterInstanceTemplateDefaultsConfigmapKey)
	if err != nil {
		if !utils.IsInputError(err) {
			return false, fmt.Errorf("failed to validate the ConfigMap %s for ClusterInstance defaults: %w",
				t.object.Spec.Templates.ClusterInstanceDefaults, err)
		}
		validationErrs = append(validationErrs, err.Error())
	}

	// Validation for the policy template defaults configmap.
	err = validateConfigmapReference[map[string]any](
		ctx, t.client,
		t.object.Spec.Templates.PolicyTemplateDefaults,
		t.object.Namespace,
		utils.PolicyTemplateDefaultsConfigmapKey)
	if err != nil {
		if !utils.IsInputError(err) {
			return false, fmt.Errorf("failed to validate the ConfigMap %s for policy template defaults: %w",
				t.object.Spec.Templates.PolicyTemplateDefaults, err)
		}
		validationErrs = append(validationErrs, err.Error())
	}

	validationErrsMsg := strings.Join(validationErrs, ";")
	if validationErrsMsg != "" {
		t.logger.ErrorContext(ctx, fmt.Sprintf(
			"Failed to validate for ClusterTemplate %s: %s", t.object.Name, validationErrsMsg))
	} else {
		t.logger.InfoContext(ctx, fmt.Sprintf(
			"Validation passing for ClusterTemplate %s", t.object.Name))
	}

	err = t.updateStatusConditionValidated(ctx, validationErrsMsg)
	if err != nil {
		return false, err
	}
	return validationErrsMsg == "", nil
}

// validateConfigmapReference validates a given configmap reference within the ClusterTemplate
func validateConfigmapReference[T any](
	ctx context.Context, c client.Client, name, namespace, expectedKey string) error {

	existingConfigmap, err := utils.GetConfigmap(ctx, c, name, namespace)
	if err != nil {
		return fmt.Errorf("failed to get ConfigmapReference: %w", err)
	}

	// Extract and validate the template from the configmap
	_, err = utils.ExtractTemplateDataFromConfigMap[T](ctx, c, existingConfigmap, expectedKey)
	if err != nil {
		return err
	}

	// Check if the configmap is set to mutable
	if existingConfigmap.Immutable != nil && !*existingConfigmap.Immutable {
		return utils.NewInputError("It is not allowed to set Immutable to false in the ConfigMap %s", name)
	} else if existingConfigmap.Immutable == nil {
		// Patch the validated ConfigMap to make it immutable if not already set
		immutable := true
		newConfigmap := existingConfigmap.DeepCopy()
		newConfigmap.Immutable = &immutable

		if err := utils.CreateK8sCR(ctx, c, newConfigmap, nil, utils.PATCH); err != nil {
			return fmt.Errorf("failed to patch ConfigMap as immutable: %w", err)
		}
	}

	return nil
}

// validateName return true if the ClusterTemplate name is the
// format: <name>.<version>, false otherwise
func validateName(c client.Client, name, version, metadataName, namespace string) error {
	if metadataName != name+"."+version {
		return utils.NewInputError("failed to validate ClusterTemplate name %s, should be in the format <spec.name>.<spec.version>: %s", metadataName, name+"."+version)
	}

	allClusterTemplates := &oranv1alpha1.ClusterTemplateList{}
	err := c.List(context.Background(), allClusterTemplates)
	if err != nil {
		return fmt.Errorf("could not get list of ClusterTemplate across the cluster: %w", err)
	}

	sameNameVersion := map[string]bool{}
	sameMetadataName := map[string]bool{}
	for _, aClusterTemplate := range allClusterTemplates.Items {
		if aClusterTemplate.Namespace == namespace {
			continue
		}
		if aClusterTemplate.Name == metadataName {
			sameMetadataName[aClusterTemplate.Namespace] = true
		}
		if aClusterTemplate.Spec.HumanReadableName == name &&
			aClusterTemplate.Spec.Version == version {
			sameNameVersion[aClusterTemplate.Namespace] = true
		}
	}
	if len(sameMetadataName) != 0 {
		return utils.NewInputError("failed to validate ClusterTemplate name %s, a identical name already exists in namespaces: %s",
			metadataName, sliceToString(mapKeysToSlice(sameMetadataName)))
	}
	if len(sameMetadataName) != 0 {
		return utils.NewInputError("failed to validate ClusterTemplate name %s, the combination of <spec.name>.<spec.version>: %s already exists in namespace: %s",
			metadataName, name+"."+version, sliceToString(mapKeysToSlice(sameNameVersion)))
	}
	return nil
}

// validateName return true if the ClusterTemplate name is the
// format: <name>.<version>, false otherwise
func validateTemplateID(ctx context.Context, c client.Client, object *provisioningv1alpha1.ClusterTemplate) error {
	if object.Spec.TemplateID != "" {
		_, err := uuid.Parse(object.Spec.TemplateID)
		if err != nil {
			return utils.NewInputError("failed to validate templateID, invalid UUID:%s", object.Spec.TemplateID)
		}
		return nil
	}

	newID := uuid.New()
	newTemplate := object
	newTemplate.Spec.TemplateID = newID.String()

	err := utils.CreateK8sCR(ctx, c, newTemplate, object, utils.PATCH)
	if err != nil {
		return fmt.Errorf("failed to patch templateID in ClusterTemplate %s: %w", object.Name, err)
	}
	err = c.Get(ctx, types.NamespacedName{Name: object.Name, Namespace: object.Namespace}, object)
	if err != nil {
		return fmt.Errorf("failed to get updated ClusterTemplate %s: %w", object.Name, err)
	}
	return nil
}

// setStatusConditionValidated updates the Validated status condition of the ClusterTemplate object
func (t *clusterTemplateReconcilerTask) updateStatusConditionValidated(ctx context.Context, errMsg string) error {
	if errMsg != "" {
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CTconditionTypes.Validated,
			utils.CTconditionReasons.Failed,
			metav1.ConditionFalse,
			errMsg,
		)
	} else {
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CTconditionTypes.Validated,
			utils.CTconditionReasons.Completed,
			metav1.ConditionTrue,
			"The cluster template validation succeeded",
		)
	}

	err := utils.UpdateK8sCRStatus(ctx, t.client, t.object)
	if err != nil {
		return fmt.Errorf("failed to update status for ClusterTemplate %s: %w", t.object.Name, err)
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	//nolint:wrapcheck
	return ctrl.NewControllerManagedBy(mgr).
		Named("o2ims-cluster-template").
		For(&provisioningv1alpha1.ClusterTemplate{},
			// Watch for create and update events for ClusterTemplate.
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					oldGeneration := e.ObjectOld.GetGeneration()
					newGeneration := e.ObjectNew.GetGeneration()

					// Reconcile on spec update only
					return oldGeneration != newGeneration
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return true },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return false },
			})).
		Watches(&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueClusterTemplatesForConfigmap),
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					cmOld := e.ObjectOld.(*corev1.ConfigMap)
					cmNew := e.ObjectNew.(*corev1.ConfigMap)

					// Reconcile on data update only
					return !equality.Semantic.DeepEqual(cmOld.Data, cmNew.Data)
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return true },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return true },
			})).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(r)
}

// enqueueClusterTemplatesForConfigmap identifies and enqueues ClusterTemplates that reference a given ConfigMap.
func (r *ClusterTemplateReconciler) enqueueClusterTemplatesForConfigmap(ctx context.Context, obj client.Object) []reconcile.Request {
	var requests []reconcile.Request

	// Get all the cluster templates
	clusterTemplates := &provisioningv1alpha1.ClusterTemplateList{}
	err := r.List(ctx, clusterTemplates)
	if err != nil {
		r.Logger.Error("Unable to list ClusterTemplate resources. ", "error: ", err.Error())
		return nil
	}

	for _, clusterTemplate := range clusterTemplates.Items {
		if clusterTemplate.Namespace == obj.GetNamespace() {
			if clusterTemplate.Spec.Templates.ClusterInstanceDefaults == obj.GetName() ||
				clusterTemplate.Spec.Templates.PolicyTemplateDefaults == obj.GetName() ||
				clusterTemplate.Spec.Templates.HwTemplate == obj.GetName() {
				// The configmap is referenced in this cluster template , enqueue it
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: clusterTemplate.Namespace,
						Name:      clusterTemplate.Name,
					},
				})
			}
		} else if obj.GetNamespace() == utils.InventoryNamespace {
			if clusterTemplate.Spec.Templates.HwTemplate == obj.GetName() {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: clusterTemplate.Namespace,
						Name:      clusterTemplate.Name,
					},
				})
			}
		}
	}
	return requests
}

// mapKeysToSlice takes a map[string]bool and returns a slice of strings containing the keys
func mapKeysToSlice(inputMap map[string]bool) []string {
	keys := make([]string, 0, len(inputMap))
	for key := range inputMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// sliceToString takes a slice of strings an returns a comma separated string including the slice content
func sliceToString(aSlice []string) (out string) {
	if len(aSlice) != 0 {
		out += aSlice[0]
		for _, aString := range aSlice[1:] {
			out += ", " + aString
		}
	}
	return out
}
