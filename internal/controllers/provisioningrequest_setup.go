package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ibgu "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
)

const ztpDoneLabel = "ztp-done"

// SetupWithManager sets up the controller with the Manager.
func (r *ProvisioningRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	//nolint:wrapcheck
	return ctrl.NewControllerManagedBy(mgr).
		Named("o2ims-cluster-request").
		For(
			&provisioningv1alpha1.ProvisioningRequest{},
			// Watch for create and update event for ProvisioningRequest.
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(
			&ibgu.ImageBasedGroupUpgrade{},
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Watch on status changes only.
					return e.ObjectOld.GetGeneration() == e.ObjectNew.GetGeneration()
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return true },
			})).
		Watches(
			&provisioningv1alpha1.ClusterTemplate{},
			handler.Funcs{UpdateFunc: r.findClusterTemplateForProvisioningRequest},
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Watch on status changes only.
					return e.ObjectOld.GetGeneration() == e.ObjectNew.GetGeneration()
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return true },
			})).
		Watches(
			&siteconfig.ClusterInstance{},
			handler.Funcs{
				UpdateFunc: r.findClusterInstanceForProvisioningRequest,
			},
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
			handler.Funcs{
				UpdateFunc: r.findNodePoolForProvisioningRequest,
			},
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
			handler.Funcs{
				UpdateFunc: r.handlePolicyEventUpdate,
				DeleteFunc: r.handlePolicyEventDelete,
			},
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
			handler.Funcs{
				UpdateFunc: r.findManagedClusterForProvisioningRequest,
			},
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Check if the event was adding the label "ztp-done".
					// Return true for that event only, and false for everything else.
					_, doneLabelExistsInOld := e.ObjectOld.GetLabels()[ztpDoneLabel]
					_, doneLabelExistsInNew := e.ObjectNew.GetLabels()[ztpDoneLabel]

					doneLabelAdded := !doneLabelExistsInOld && doneLabelExistsInNew

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

					var hubAccepted bool
					acceptedCondition := meta.FindStatusCondition(
						e.ObjectNew.(*clusterv1.ManagedCluster).Status.Conditions,
						clusterv1.ManagedClusterConditionHubAccepted)
					if acceptedCondition != nil && acceptedCondition.Status == metav1.ConditionTrue {
						hubAccepted = true
					}

					return (doneLabelAdded && availableInNew && hubAccepted) ||
						(!availableInOld && availableInNew && doneLabelExistsInNew && hubAccepted)
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return false },
			})).
		Complete(r)
}

// findClusterInstanceForProvisioningRequest maps the ClusterInstance created by a
// ProvisioningRequest to a reconciliation request.
func (r *ProvisioningRequestReconciler) findClusterInstanceForProvisioningRequest(
	ctx context.Context, event event.UpdateEvent,
	queue workqueue.RateLimitingInterface) {

	newClusterInstance := event.ObjectNew.(*siteconfig.ClusterInstance)
	crName, nameExists := newClusterInstance.GetLabels()[provisioningRequestNameLabel]
	if nameExists {
		// Create reconciling requests only for the ProvisioningRequest that has generated
		// the current ClusterInstance.
		r.Logger.Info(
			"[findClusterInstanceForProvisioningRequest] Add new reconcile request for ProvisioningRequest",
			"name", crName)
		queue.Add(
			reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: crName,
				},
			},
		)
	}
}

// findNodePoolForProvisioningRequest maps the NodePool created by a
// ProvisioningRequest to a reconciliation request.
func (r *ProvisioningRequestReconciler) findNodePoolForProvisioningRequest(
	ctx context.Context, event event.UpdateEvent,
	queue workqueue.RateLimitingInterface) {

	newNodePool := event.ObjectNew.(*hwv1alpha1.NodePool)

	crName, nameExists := newNodePool.GetLabels()[provisioningRequestNameLabel]
	if nameExists {
		// Create reconciling requests only for the ProvisioningRequest that has generated
		// the current NodePool.
		r.Logger.Info(
			"[findNodePoolForProvisioningRequest] Add new reconcile request for ProvisioningRequest",
			"name", crName)
		queue.Add(
			reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: crName,
				},
			},
		)
	}
}

// findClusterTemplateForProvisioningRequest maps the ClusterTemplates used by ProvisioningRequests
// to reconciling requests for those ProvisioningRequests.
func (r *ProvisioningRequestReconciler) findClusterTemplateForProvisioningRequest(
	ctx context.Context, event event.UpdateEvent,
	queue workqueue.RateLimitingInterface) {

	// For this case, we can use either new or old object.
	newClusterTemplate := event.ObjectNew.(*provisioningv1alpha1.ClusterTemplate)

	// Get all the provisioningRequests.
	provisioningRequests := &provisioningv1alpha1.ProvisioningRequestList{}
	err := r.Client.List(ctx, provisioningRequests, client.InNamespace(newClusterTemplate.GetNamespace()))
	if err != nil {
		r.Logger.Error("[findProvisioningRequestsForClusterTemplate] Error listing ProvisioningRequests. ", "Error: ", err)
		return
	}

	// Create reconciling requests only for the ProvisioningRequests that are using the
	// current clusterTemplate.
	for _, provisioningRequest := range provisioningRequests.Items {
		clusterTemplateRefName := getClusterTemplateRefName(
			provisioningRequest.Spec.TemplateName, provisioningRequest.Spec.TemplateVersion)
		if clusterTemplateRefName == newClusterTemplate.GetName() {
			r.Logger.Info(
				"[findProvisioningRequestsForClusterTemplate] Add new reconcile request for ProvisioningRequest",
				"name", provisioningRequest.Name)
			queue.Add(
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name: provisioningRequest.Name,
					},
				},
			)
		}
	}
}

// findManagedClusterForProvisioningRequest maps the ManagedClusters created
// by ClusterInstances through ProvisioningRequests.
func (r *ProvisioningRequestReconciler) findManagedClusterForProvisioningRequest(
	ctx context.Context, event event.UpdateEvent,
	queue workqueue.RateLimitingInterface) {

	// For this case, we can use either new or old object.
	newManagedCluster := event.ObjectNew.(*clusterv1.ManagedCluster)

	// Get the ClusterInstance
	clusterInstance := &siteconfig.ClusterInstance{}
	err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: newManagedCluster.Name,
		Name:      newManagedCluster.Name,
	}, clusterInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Return as this ManagedCluster is not deployed/managed by ClusterInstance
			return
		}
		r.Logger.Error("[findManagedClusterForProvisioningRequest] Error getting ClusterInstance. ", "Error: ", err)
		return
	}

	crName, nameExists := clusterInstance.GetLabels()[provisioningRequestNameLabel]
	if nameExists {
		r.Logger.Info(
			"[findManagedClusterForProvisioningRequest] Add new reconcile request for ProvisioningRequest",
			"name", crName)
		queue.Add(
			reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: crName,
				},
			},
		)
	}
}

// findPoliciesForProvisioningRequests creates reconciliation requests for the ProvisioningRequests
// whose associated ManagedClusters have matched policies Updated or Deleted.
func findPoliciesForProvisioningRequests[T deleteOrUpdateEvent](
	ctx context.Context, c client.Client, e T, q workqueue.RateLimitingInterface) {

	policy := &policiesv1.Policy{}
	switch evt := any(e).(type) {
	case event.UpdateEvent:
		policy = evt.ObjectOld.(*policiesv1.Policy)
	case event.DeleteEvent:
		policy = evt.Object.(*policiesv1.Policy)
	default:
		// Only Update and Delete events are supported
		return
	}

	// Get the ClusterInstance.
	clusterInstance := &siteconfig.ClusterInstance{}
	err := c.Get(ctx, types.NamespacedName{
		Namespace: policy.Namespace,
		Name:      policy.Namespace,
	}, clusterInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Return as the ManagedCluster for this Namespace is not deployed/managed by ClusterInstance.
			return
		}
		return
	}

	ProvisioningRequest, okCR := clusterInstance.GetLabels()[provisioningRequestNameLabel]
	if okCR {
		q.Add(reconcile.Request{NamespacedName: types.NamespacedName{Name: ProvisioningRequest}})
	}
}

// handlePolicyEvent handled Updates and Deleted events.
func (r *ProvisioningRequestReconciler) handlePolicyEventDelete(
	ctx context.Context, e event.DeleteEvent, q workqueue.RateLimitingInterface) {

	// Call the generic function for determining the corresponding ProvisioningRequest.
	findPoliciesForProvisioningRequests(ctx, r.Client, e, q)
}

// handlePolicyEvent handled Updates and Deleted events.
func (r *ProvisioningRequestReconciler) handlePolicyEventUpdate(
	ctx context.Context, e event.UpdateEvent, q workqueue.RateLimitingInterface) {

	// Call the generic function.
	findPoliciesForProvisioningRequests(ctx, r.Client, e, q)
}
