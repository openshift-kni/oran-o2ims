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
	"strings"

	"log/slog"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/yaml"

	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/files"
)

// ClusterTemplateReconciler reconciles a ClusterTemplate object
type ClusterTemplateReconciler struct {
	client.Client
	Logger *slog.Logger
}

type clusterTemplateReconcilerTask struct {
	logger *slog.Logger
	client client.Client
	object *oranv1alpha1.ClusterTemplate
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

func requeueWithCustomInterval(interval time.Duration) ctrl.Result {
	return ctrl.Result{RequeueAfter: interval}
}

//+kubebuilder:rbac:groups=oran.openshift.io,resources=clustertemplates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=oran.openshift.io,resources=clustertemplates/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=oran.openshift.io,resources=clustertemplates/finalizers,verbs=update

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
	object := &oranv1alpha1.ClusterTemplate{}
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

	// TODO: validition for hw template configmap

	// Validition for the policy template defaults configmap.
	validationErr, err := validateConfigmapReference(
		ctx, t.client,
		t.object.Spec.Templates.PolicyTemplateDefaults,
		t.object.Namespace,
		utils.PolicyTemplateDefaultsConfigmapKey)
	if err != nil {
		return false, fmt.Errorf("failed to validate the ConfigMap %s for policy template defaults: %w",
			t.object.Spec.Templates.PolicyTemplateDefaults, err)
	}
	if validationErr != "" {
		validationErrs = append(validationErrs, validationErr)
	}

	// Validate the ClusterInstance defaults configmap.
	validationErr, err = validateConfigmapReference(
		ctx, t.client,
		t.object.Spec.Templates.ClusterInstanceDefaults,
		t.object.Namespace,
		utils.ClusterInstanceTemplateDefaultsConfigmapKey)
	if err != nil {
		return false, fmt.Errorf("failed to validate the ConfigMap %s for ClusterInstance defaults: %w",
			t.object.Spec.Templates.ClusterInstanceDefaults, err)
	}
	if validationErr != "" {
		validationErrs = append(validationErrs, validationErr)
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
func validateConfigmapReference(
	ctx context.Context, c client.Client, name, namespace, expectedKey string) (string, error) {

	var validationErr string

	existingConfigmap := &corev1.ConfigMap{}
	cmExists, err := utils.DoesK8SResourceExist(
		ctx, c, name, namespace, existingConfigmap)
	if err != nil {
		return validationErr, err
	}

	if !cmExists {
		// Check if the configmap is missing
		validationErr = fmt.Sprintf(
			"The referenced ConfigMap %s is not found in the namespace %s", name, namespace)
		return validationErr, nil
	} else {
		// Check if the expected key is present in the configmap data
		defaults, exists := existingConfigmap.Data[expectedKey]
		if !exists {
			validationErr = fmt.Sprintf(
				"the expected key %s does not exist in the ConfigMap %s data", expectedKey, name)
			return validationErr, nil
		}

		// Check if the data value is valid YAML
		var validData map[string]interface{}
		err = yaml.Unmarshal([]byte(defaults), &validData)
		if err != nil {
			validationErr = fmt.Sprintf(
				"the value of key %s from ConfigMap %s is not in a valid YAML string: %s", expectedKey, name, err.Error())
			return validationErr, nil
		}
	}

	// Check if the configmap is set to mutable
	if existingConfigmap.Immutable != nil && !*existingConfigmap.Immutable {
		validationErr = fmt.Sprintf(
			"It is not allowed to set Immutable to false in the ConfigMap %s", name)
		return validationErr, nil
	} else if existingConfigmap.Immutable == nil {
		// Patch the validated ConfigMap to make it immutable if not already set
		immutable := true
		newConfigmap := existingConfigmap.DeepCopy()
		newConfigmap.Immutable = &immutable

		if err = utils.CreateK8sCR(ctx, c, newConfigmap, nil, utils.PATCH); err != nil {
			return validationErr, err
		}
	}

	return validationErr, nil
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

// initConfigmapClusterInstanceTemplate creates the configmap for cluster instance template
func (r *ClusterTemplateReconciler) initConfigmapClusterInstanceTemplate() (err error) {
	oranNs := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: utils.ORANO2IMSNamespace,
		},
	}
	err = r.Create(context.TODO(), &oranNs)
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			return
		}
	}

	// Load the template from yaml file
	clusterInstanceTmpl, err := files.Controllers.ReadFile(utils.ClusterInstanceTemplatePath)
	if err != nil {
		err = fmt.Errorf("error reading template file: %w", err)
		return
	}

	// Create immutable configmap
	immutable := true
	clusterInstanceTmplCm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.ClusterInstanceTemplateConfigmapName,
			Namespace: utils.ClusterInstanceTemplateConfigmapNamespace,
		},
		Immutable: &immutable,
		Data: map[string]string{
			utils.ClusterInstanceTemplateConfigmapName: string(clusterInstanceTmpl),
		},
	}

	if err = r.Delete(context.TODO(), clusterInstanceTmplCm); err != nil {
		if !errors.IsNotFound(err) {
			return
		}
	}

	if err = r.Create(context.TODO(), clusterInstanceTmplCm); err != nil {
		return
	}

	r.Logger.Info(
		"Created the ClusterInstance template ConfigMap",
		slog.String("name", clusterInstanceTmplCm.Name),
		slog.String("namespace", clusterInstanceTmplCm.Namespace),
	)
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := r.initConfigmapClusterInstanceTemplate(); err != nil {
		r.Logger.Error(
			"Error creating the ClusterInstance template ConfigMap",
			slog.String("error", err.Error()),
		)
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("orano2ims-cluster-template").
		For(&oranv1alpha1.ClusterTemplate{},
			// Watch for create and update event for ClusterTemplate.
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
	clusterTemplates := &oranv1alpha1.ClusterTemplateList{}
	err := r.List(ctx, clusterTemplates)
	if err != nil {
		r.Logger.Error("Unable to list ClusterTemplate resources. ", "error: ", err.Error())
		return nil
	}

	for _, clusterTemplate := range clusterTemplates.Items {
		if clusterTemplate.Namespace == obj.GetNamespace() {
			// TODO: add check for policy template configmap
			if clusterTemplate.Spec.Templates.ClusterInstanceDefaults == obj.GetName() {
				// The configmap is referenced in this cluster template , enqueue it
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: clusterTemplate.Namespace,
						Name:      clusterTemplate.Name,
					},
				})
			}
		}
		// TODO: add check for hw template
	}
	return requests
}
