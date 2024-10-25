package controllers

import (
	"context"
	"fmt"

	"github.com/coreos/go-semver/semver"
	ibgu "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
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
	template, err := t.getCrClusterTemplateRef(ctx)
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

func (t *provisioningRequestReconcilerTask) handleUpgrade(
	ctx context.Context, renderedClusterInstance *siteconfig.ClusterInstance) (ctrl.Result, error) {
	t.logger.InfoContext(
		ctx,
		"Start handleUpgrade",
	)
	clusterTemplate, err := t.getCrClusterTemplateRef(ctx)
	if err != nil {
		return requeueWithError(fmt.Errorf("failed to get clusterTemplate: %w", err))
	}

	ibgu := &ibgu.ImageBasedGroupUpgrade{}
	err = t.client.Get(ctx, types.NamespacedName{Name: t.object.Name, Namespace: renderedClusterInstance.Namespace}, ibgu)
	if err != nil && errors.IsNotFound(err) {
		ibgu, err = utils.GetIBGUFromUpgradeDefaultsConfigmap(
			ctx, t.client, clusterTemplate.Spec.Templates.UpgradeDefaults,
			clusterTemplate.Namespace, utils.UpgradeDefaultsConfigmapKey,
			renderedClusterInstance.Spec.ClusterName, t.object.Name, renderedClusterInstance.Namespace)
		if err != nil {
			return requeueWithError(fmt.Errorf("failed to generate IBGU for cluster: %w", err))
		}
		if err := utils.CreateK8sCR(ctx, t.client, ibgu, t.object, utils.UPDATE); err != nil {
			return requeueWithError(fmt.Errorf("failed to create IBGU: %w", err))
		}

		t.logger.InfoContext(
			ctx,
			fmt.Sprintf(
				"Upgrade initiated. Created IBGU %s in the namespace %s",
				ibgu.GetName(),
				ibgu.GetNamespace(),
			),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.UpgradeCompleted,
			utils.CRconditionReasons.InProgress,
			metav1.ConditionFalse,
			"Upgrade is initiated",
		)
		if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
			return requeueWithError(fmt.Errorf("failed to update ClusterRequest CR status: %w", err))
		}

	} else if err != nil {
		return requeueWithError(fmt.Errorf("error getting IBGU: %w", err))
	}

	if isIBGUProgressing(ibgu) {
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.UpgradeCompleted,
			utils.CRconditionReasons.InProgress,
			metav1.ConditionFalse,
			"Upgrade is in progress",
		)
		t.logger.InfoContext(
			ctx,
			"Wait for upgrade to be completed",
		)
		if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
			return requeueWithError(fmt.Errorf("failed to update ClusterRequest CR status: %w", err))
		}
		return requeueWithMediumInterval(), nil
	} else {
		// IBGU completed or failed. Collect results if it matches the current template ocp version
		if clusterTemplate.Spec.Release == ibgu.Spec.IBUSpec.SeedImageRef.Version {
			if failed, message := isIBGUFailed(ibgu); failed {
				utils.SetStatusCondition(&t.object.Status.Conditions,
					utils.PRconditionTypes.UpgradeCompleted,
					utils.CRconditionReasons.Failed,
					metav1.ConditionFalse,
					message,
				)
			} else {
				utils.SetStatusCondition(&t.object.Status.Conditions,
					utils.PRconditionTypes.UpgradeCompleted,
					utils.CRconditionReasons.Completed,
					metav1.ConditionTrue,
					"Upgrade is completed",
				)
				err := t.client.Delete(ctx, ibgu)
				if err != nil {
					return requeueWithError(fmt.Errorf("failed to cleanup IBGU: %w", err))
				}
			}
			if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
				return requeueWithError(fmt.Errorf("failed to update ClusterRequest CR status: %w", err))
			}

		} else {
			// Clean up IBGU and the condition if it doesn't match the current template ocp release version,
			// i.e. the failed and revert case
			err := t.client.Delete(ctx, ibgu)
			if err != nil {
				return requeueWithError(fmt.Errorf("failed to cleanup IBGU: %w", err))
			}
			meta.RemoveStatusCondition(&t.object.Status.Conditions, string(utils.PRconditionTypes.UpgradeCompleted))
			if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
				return requeueWithError(fmt.Errorf("failed to update ClusterRequest CR status: %w", err))
			}
		}
	}

	return doNotRequeue(), nil
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
