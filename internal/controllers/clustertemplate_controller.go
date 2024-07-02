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
	"log/slog"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
)

// ClusterTemplateReconciler reconciles a ClusterTemplate object
type ClusterTemplateReconciler struct {
	client.Client
	Logger *slog.Logger
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
func (r *ClusterTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// Fetch the object:
	object := &oranv1alpha1.ClusterTemplate{}
	if err := r.Client.Get(ctx, req.NamespacedName, object); err != nil {
		if errors.IsNotFound(err) {
			err = nil
			return ctrl.Result{RequeueAfter: 5 * time.Minute}, err
		}
		r.Logger.ErrorContext(
			ctx,
			"Unable to fetch ORANO2IMS",
			slog.String("error", err.Error()),
		)
	}

	r.Logger.InfoContext(ctx, ">>> Reconcile Cluster Template")

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
	    Named("orano2ims-cluster-template").
		For(&oranv1alpha1.ClusterTemplate{}).
		Complete(r)
}
