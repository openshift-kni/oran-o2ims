package controllers

import (
	"context"
	"fmt"

	"github.com/coreos/go-semver/semver"
	ibgu "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// IsUpgradeRequested retruns true if cluster template release version is higher than
// managedCluster openshift release version
func (t *provisioningRequestReconcilerTask) IsUpgradeRequested(
	ctx context.Context, managedClusterName string,
) (bool, error) {
	template, err := t.object.GetClusterTemplateRef(ctx, t.client)
	if err != nil {
		return false, fmt.Errorf("failed to get ClusterTemplate: %w", err)
	}

	if template.Spec.Release == "" {
		return false, nil
	}

	managedCluster := &clusterv1.ManagedCluster{}
	err = t.client.Get(ctx, types.NamespacedName{Name: managedClusterName}, managedCluster)
	if err != nil {
		return false, fmt.Errorf("failed to get ManagedCluster: %w", err)
	}

	templateReleaseVersion, err := semver.NewVersion(template.Spec.Release)
	if err != nil {
		return false, fmt.Errorf("failed to parse template version: %w", err)
	}
	managedClusterVersion, err := semver.NewVersion(managedCluster.GetLabels()["openshiftVersion"])
	if err != nil {
		return false, fmt.Errorf("failed to parse ManagedCluster version: %w", err)
	}
	cmp := templateReleaseVersion.Compare(*managedClusterVersion)
	if cmp == 1 {
		return true, nil
	} else if cmp == -1 {
		return false, fmt.Errorf("template version (%v) is lower then ManagedCluster version (%v), no upgrade requested",
			templateReleaseVersion, managedClusterVersion)
	}
	return false, nil
}

// handleUpgrade handles the upgrade of the cluster through IBGU. It returns a ctrl.Result to indicate
// if/when to requeue, a bool to indicate whether to process with further processing and an error if any issues occur.
func (t *provisioningRequestReconcilerTask) handleUpgrade(ctx context.Context, clusterName string) (ctrl.Result, bool, error) {
	nextReconcile := ctrl.Result{}
	proceed := false

	t.logger.InfoContext(
		ctx,
		"Start handling upgrade",
	)
	clusterTemplate, err := t.object.GetClusterTemplateRef(ctx, t.client)
	if err != nil {
		return nextReconcile, proceed, fmt.Errorf("failed to get clusterTemplate: %w", err)
	}

	ibgu := &ibgu.ImageBasedGroupUpgrade{}
	err = t.client.Get(ctx, types.NamespacedName{Name: t.object.Name, Namespace: clusterName}, ibgu)
	if err != nil {
		if !errors.IsNotFound(err) {
			return nextReconcile, proceed, fmt.Errorf("failed to get IBGU: %w", err)
		}

		// Create IBGU if it doesn't exist
		ibgu, err = utils.GetIBGUFromUpgradeDefaultsConfigmap(
			ctx, t.client, clusterTemplate.Spec.Templates.UpgradeDefaults,
			clusterTemplate.Namespace, utils.UpgradeDefaultsConfigmapKey,
			clusterName, t.object.Name, clusterName)
		if err != nil {
			return nextReconcile, proceed, fmt.Errorf("failed to generate IBGU for cluster: %w", err)
		}
		if err := utils.CreateK8sCR(ctx, t.client, ibgu, t.object, utils.UPDATE); err != nil {
			return nextReconcile, proceed, fmt.Errorf("failed to create IBGU: %w", err)
		}

		t.logger.InfoContext(
			ctx,
			fmt.Sprintf(
				"Upgrade initiated. Created IBGU %s in the namespace %s",
				ibgu.GetName(),
				ibgu.GetNamespace(),
			),
		)

		utils.SetProvisioningStateInProgress(t.object, "Cluster upgrade is initiated")
		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.UpgradeCompleted,
			provisioningv1alpha1.CRconditionReasons.InProgress,
			metav1.ConditionFalse,
			"Upgrade is initiated",
		)
		if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
			return nextReconcile, proceed, fmt.Errorf("failed to update ProvisioningRequest CR status: %w", err)
		}
	}

	if isIBGUProgressing(ibgu) {
		t.logger.InfoContext(
			ctx,
			"Wait for upgrade to be completed",
		)

		utils.SetProvisioningStateInProgress(t.object, "Cluster upgrade is in progress")
		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.UpgradeCompleted,
			provisioningv1alpha1.CRconditionReasons.InProgress,
			metav1.ConditionFalse,
			"Upgrade is in progress",
		)
		nextReconcile = requeueWithMediumInterval()
	} else if failed, message := isIBGUFailed(ibgu); failed {
		utils.SetProvisioningStateFailed(t.object, "Cluster upgrade is failed")
		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.UpgradeCompleted,
			provisioningv1alpha1.CRconditionReasons.Failed,
			metav1.ConditionFalse,
			message,
		)
	} else {
		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.UpgradeCompleted,
			provisioningv1alpha1.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"Upgrade is completed",
		)
		err := t.client.Delete(ctx, ibgu)
		if err != nil {
			return nextReconcile, proceed, fmt.Errorf("failed to cleanup IBGU: %w", err)
		}
		// Proceed to further processing only when IBGU is completed
		proceed = true
	}

	if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return nextReconcile, proceed, fmt.Errorf("failed to update ProvisioningRequest CR status: %w", err)
	}

	return nextReconcile, proceed, nil
}

func isIBGUFailed(cr *ibgu.ImageBasedGroupUpgrade) (bool, string) {
	for _, cluster := range cr.Status.Clusters {
		if len(cluster.FailedActions) == 0 {
			continue
		}
		message := "Upgrade Failed: "
		for _, action := range cluster.FailedActions {
			message += fmt.Sprintf("Action %s failed: %s\n", action.Action, action.Message)
		}
		return true, message
	}
	return false, ""
}

func isIBGUProgressing(cr *ibgu.ImageBasedGroupUpgrade) bool {
	condition := meta.FindStatusCondition(cr.Status.Conditions, "Progressing")
	if condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return true
}
