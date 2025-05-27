/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
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

	ibgu "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
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
	// Setup Node CRD indexer. This field indexer allows us to query a list of Node CRs, filtered by the spec.nodeAllocationRequest field.
	nodeIndexFunc := func(obj client.Object) []string {
		return []string{obj.(*hwv1alpha1.Node).Spec.NodeAllocationRequest}
	}

	if err := mgr.GetFieldIndexer().IndexField(context.TODO(), &hwv1alpha1.Node{}, "spec.nodeAllocationRequest", nodeIndexFunc); err != nil {
		return fmt.Errorf("failed to setup indexer for o2ims-hardwaremanagement.oran.openshift.io/v1alpha Node: %w", err)
	}
	//nolint:wrapcheck
	return ctrl.NewControllerManagedBy(mgr).
		Named("o2ims-cluster-request").
		For(
			&provisioningv1alpha1.ProvisioningRequest{},
			// Watch for create and update event for ProvisioningRequest.
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(
			&corev1.Namespace{},
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc:  func(e event.UpdateEvent) bool { return false },
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return true },
			})).
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
		Owns(
			&siteconfig.ClusterInstance{},
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
		Owns(
			&hwv1alpha1.NodeAllocationRequest{},
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
		clusterTemplateRefName := GetClusterTemplateRefName(
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
	crName, nameExists := clusterInstance.GetLabels()[provisioningv1alpha1.ProvisioningRequestNameLabel]
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
	provisioningRequest, okCR := clusterInstance.GetLabels()[provisioningv1alpha1.ProvisioningRequestNameLabel]
	if okCR {
		provReq := &provisioningv1alpha1.ProvisioningRequest{}
		if err := r.Get(ctx, types.NamespacedName{Name: provisioningRequest}, provReq); err != nil {
			if errors.IsNotFound(err) {
				// The provisioning request could have been deleted
				return nil
			}
			r.Logger.Error("[enqueueProvisioningRequestForPolicy] Error getting ProvisioningRequest. ", "Error: ", err)
			return nil
		}

		clusterTemplates := &provisioningv1alpha1.ClusterTemplateList{}
		if err := r.List(ctx, clusterTemplates); err != nil {
			r.Logger.Error("[enqueueProvisioningRequestForPolicy] Error listing ClusterTemplates. ", "Error: ", err)
			return nil
		}

		ctRefName := GetClusterTemplateRefName(
			provReq.Spec.TemplateName, provReq.Spec.TemplateVersion)
		ctRefNamespace := ""
		for _, ct := range clusterTemplates.Items {
			if ctRefName == ct.Name {
				// Break if found, as the metadata name of ClusterTemplate is unique across all namespaces.
				ctRefNamespace = ct.Namespace
				break
			}
		}

		_, parentPolicyNs := utils.GetParentPolicyNameAndNamespace(obj.GetName())
		if utils.IsParentPolicyInZtpClusterTemplateNs(parentPolicyNs, ctRefNamespace) {
			r.Logger.Info(
				"[enqueueProvisioningRequestForPolicy] Add new reconcile request for ProvisioningRequest ",
				"name", provisioningRequest, "policyName", obj.GetName())
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: provisioningRequest},
			})
		}
	}

	return requests
}
