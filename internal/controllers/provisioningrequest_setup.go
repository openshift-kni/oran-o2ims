package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
)

// SetupWithManager sets up the controller with the Manager.
func (r *ProvisioningRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	//nolint:wrapcheck
	return ctrl.NewControllerManagedBy(mgr).
		Named("o2ims-cluster-request").
		For(
			&provisioningv1alpha1.ProvisioningRequest{},
			// Watch for create and update event for ProvisioningRequest.
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&provisioningv1alpha1.ClusterTemplate{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueProvisioningRequestForClusterTemplate),
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Watch on status changes only.
					return e.ObjectOld.GetGeneration() == e.ObjectNew.GetGeneration()
				},
				CreateFunc: func(ce event.CreateEvent) bool {
					// Only process a CreateEvent if the ClusterTemplate already has a status.
					ct := ce.Object.(*provisioningv1alpha1.ClusterTemplate)
					return ct.Status.Conditions != nil
				},
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return true },
			})).
		Watches(
			&siteconfig.ClusterInstance{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueProvisioningRequestForClusterInstance),
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Watch on ClusterInstance status conditions changes only
					ciOld := e.ObjectOld.(*siteconfig.ClusterInstance)
					ciNew := e.ObjectNew.(*siteconfig.ClusterInstance)

					if ciOld.GetGeneration() == ciNew.GetGeneration() {
						if !equality.Semantic.DeepEqual(ciOld.Status.Conditions, ciNew.Status.Conditions) {
							return true
						}
					}
					return false
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return true },
			})).
		Watches(
			&hwv1alpha1.NodePool{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueProvisioningRequestForNodePool),
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Watch on status changes.
					// TODO: Filter on further conditions that the ProvisioningRequest is interested in
					return e.ObjectOld.GetGeneration() == e.ObjectNew.GetGeneration()
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return true },
			})).
		Watches(
			&policiesv1.Policy{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueProvisioningRequestForPolicy),
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Filter out updates to parent policies.
					if _, ok := e.ObjectNew.GetLabels()[utils.ChildPolicyRootPolicyLabel]; !ok {
						return false
					}

					policyNew := e.ObjectNew.(*policiesv1.Policy)
					policyOld := e.ObjectOld.(*policiesv1.Policy)

					// Process status.status and remediation action changes.
					return policyOld.Status.ComplianceState != policyNew.Status.ComplianceState ||
						(policyOld.Spec.RemediationAction != policyNew.Spec.RemediationAction)
				},
				CreateFunc:  func(e event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc: func(de event.DeleteEvent) bool {
					// Filter out updates to parent policies.
					if _, ok := de.Object.GetLabels()[utils.ChildPolicyRootPolicyLabel]; !ok {
						return false
					}
					return true
				},
			})).
		Watches(
			&clusterv1.ManagedCluster{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueProvisioningRequestForManagedCluster),
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					var availableInNew, availableInOld bool
					availableCondition := meta.FindStatusCondition(
						e.ObjectOld.(*clusterv1.ManagedCluster).Status.Conditions,
						clusterv1.ManagedClusterConditionAvailable)
					if availableCondition != nil && availableCondition.Status == metav1.ConditionTrue {
						availableInOld = true
					}
					availableCondition = meta.FindStatusCondition(
						e.ObjectNew.(*clusterv1.ManagedCluster).Status.Conditions,
						clusterv1.ManagedClusterConditionAvailable)
					if availableCondition != nil && availableCondition.Status == metav1.ConditionTrue {
						availableInNew = true
					}

					return availableInOld != availableInNew
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return false },
			})).
		Complete(r)
}

// enqueueProvisioningRequestForClusterInstance maps the ClusterInstance created by a
// ProvisioningRequest to a reconciliation request.
func (r *ProvisioningRequestReconciler) enqueueProvisioningRequestForClusterInstance(
	ctx context.Context, obj client.Object) []reconcile.Request {
	var requests []reconcile.Request

	newClusterInstance := obj.(*siteconfig.ClusterInstance)
	crName, nameExists := newClusterInstance.GetLabels()[provisioningRequestNameLabel]
	if nameExists {
		// Create reconciling requests only for the ProvisioningRequest that has generated
		// the current ClusterInstance.
		r.Logger.Info(
			"[enqueueProvisioningRequestForClusterInstance] Add new reconcile request for ProvisioningRequest",
			"name", crName)
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: crName,
			},
		})
	}

	return requests
}

// enqueueProvisioningRequestForNodePool maps the NodePool created by a
// ProvisioningRequest to a reconciliation request.
func (r *ProvisioningRequestReconciler) enqueueProvisioningRequestForNodePool(
	ctx context.Context, obj client.Object) []reconcile.Request {
	var requests []reconcile.Request

	newNodePool := obj.(*hwv1alpha1.NodePool)

	crName, nameExists := newNodePool.GetLabels()[provisioningRequestNameLabel]
	if nameExists {
		// Create reconciling requests only for the ProvisioningRequest that has generated
		// the current NodePool.
		r.Logger.Info(
			"[enqueueProvisioningRequestForNodePool] Add new reconcile request for ProvisioningRequest",
			"name", crName)
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: crName,
			},
		})
	}

	return requests
}

// enqueueProvisioningRequestForClusterTemplate maps the ClusterTemplates used by ProvisioningRequests
// to reconciling requests for those ProvisioningRequests.
func (r *ProvisioningRequestReconciler) enqueueProvisioningRequestForClusterTemplate(
	ctx context.Context, obj client.Object) []reconcile.Request {
	var requests []reconcile.Request

	// Get all the ProvisioningRequests.
	provisioningRequests := &provisioningv1alpha1.ProvisioningRequestList{}
	err := r.Client.List(ctx, provisioningRequests)
	if err != nil {
		return nil
	}

	// Create reconciling requests only for the ProvisioningRequests that are using the
	// current ClusterTemplate.
	for _, provisioningRequest := range provisioningRequests.Items {
		clusterTemplateRefName := getClusterTemplateRefName(
			provisioningRequest.Spec.TemplateName, provisioningRequest.Spec.TemplateVersion)
		if clusterTemplateRefName == obj.GetName() {
			r.Logger.Info(
				"[enqueueProvisioningRequestForClusterTemplate] Add new reconcile request for ProvisioningRequest ",
				"name", provisioningRequest.Name)
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: provisioningRequest.Name,
				},
			})
		}
	}

	return requests
}

// enqueueProvisioningRequestForManagedCluster maps the ManagedClusters created
// by ClusterInstances through ProvisioningRequests.
func (r *ProvisioningRequestReconciler) enqueueProvisioningRequestForManagedCluster(
	ctx context.Context, obj client.Object) []reconcile.Request {
	var requests []reconcile.Request

	// Get the ClusterInstance.
	newManagedCluster := obj.(*clusterv1.ManagedCluster)
	clusterInstance := &siteconfig.ClusterInstance{}
	err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: newManagedCluster.Name,
		Name:      newManagedCluster.Name,
	}, clusterInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Return as this ManagedCluster is not deployed/managed by ClusterInstance.
			return nil
		}
		r.Logger.Error("[enqueueProvisioningRequestForManagedCluster] Error getting ClusterInstance. ", "Error: ", err)
		return nil
	}

	// Get the ProvisioningRequest name.
	crName, nameExists := clusterInstance.GetLabels()[provisioningRequestNameLabel]
	if nameExists {
		r.Logger.Info(
			"[enqueueProvisioningRequestForManagedCluster] Add new reconcile request for ProvisioningRequest",
			"name", crName)
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: crName,
			},
		})
	}

	return requests
}

// enqueueProvisioningRequestForPolicy creates reconciliation requests for the ProvisioningRequests
// whose associated ManagedClusters have matched policies Updated or Deleted.
func (r *ProvisioningRequestReconciler) enqueueProvisioningRequestForPolicy(
	ctx context.Context, obj client.Object) []reconcile.Request {
	var requests []reconcile.Request
	// Get the ClusterInstance. The obj parameter is a child policy, so its namespace is the same
	// as the name of the ClusterInstance.
	clusterInstance := &siteconfig.ClusterInstance{}
	err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetNamespace(),
	}, clusterInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Return, as the ManagedCluster for this Namespace is not deployed/managed by ClusterInstance.
			return nil
		}
		return nil
	}

	// Requeue for the ProvisioningRequest which created the ClusterInstance and thus the
	// ManagedCluster to which the policy is matched.
	provisioningRequest, okCR := clusterInstance.GetLabels()[provisioningRequestNameLabel]
	if okCR {
		r.Logger.Info(
			"[enqueueProvisioningRequestForPolicy] Add new reconcile request for ProvisioningRequest ",
			"name", provisioningRequest)
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: provisioningRequest},
		})
	}

	return requests
}
