package controllers

import (
	"context"
	"fmt"

	ibgu "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	ibuv1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func generateIBGUForCluster(clusterRequest *oranv1alpha1.ClusterRequest, clusterInstance *siteconfig.ClusterInstance, clusterTemple *oranv1alpha1.ClusterTemplate) *ibgu.ImageBasedGroupUpgrade {
	return &ibgu.ImageBasedGroupUpgrade{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterRequest.Name,
			Namespace: clusterRequest.Namespace,
		},
		Spec: ibgu.ImageBasedGroupUpgradeSpec{
			IBUSpec: ibuv1.ImageBasedUpgradeSpec{
				SeedImageRef: clusterTemple.Spec.SeedImageRef,
				OADPContent: []ibuv1.ConfigMapRef{
					{
						Name:      "oadp-cm",
						Namespace: "openshift-adp",
					},
					// TODO: add user application backup
				},
			},
			ClusterLabelSelectors: []metav1.LabelSelector{
				{
					MatchLabels: map[string]string{
						"name": clusterInstance.Spec.ClusterName,
					},
				},
			},
			Plan: []ibgu.PlanItem{
				{
					Actions: []string{ibgu.Prep, ibgu.Upgrade, ibgu.FinalizeUpgrade},
					RolloutStrategy: ibgu.RolloutStrategy{
						Timeout:        60,
						MaxConcurrency: 1,
					},
				},
			},
		},
	}
}

func generateOADPConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "oadp-cm",
			Namespace: "openshift-adp",
		},
		Data: map[string]string{
			"klusterlet.yaml": `apiVersion: velero.io/v1\nkind: Backup\nmetadata:\n  name: acm-klusterlet\n
    \ annotations:\n    lca.openshift.io/apply-label: \"apps/v1/deployments/open-cluster-management-agent/klusterlet,v1/secrets/open-cluster-management-agent/bootstrap-hub-kubeconfig,rbac.authorization.k8s.io/v1/clusterroles/klusterlet,v1/serviceaccounts/open-cluster-management-agent/klusterlet,scheduling.k8s.io/v1/priorityclasses/klusterlet-critical,rbac.authorization.k8s.io/v1/clusterroles/open-cluster-management:klusterlet-work:ibu-role,rbac.authorization.k8s.io/v1/clusterroles/open-cluster-management:klusterlet-admin-aggregate-clusterrole,rbac.authorization.k8s.io/v1/clusterrolebindings/klusterlet,operator.open-cluster-management.io/v1/klusterlets/klusterlet,apiextensions.k8s.io/v1/customresourcedefinitions/klusterlets.operator.open-cluster-management.io,v1/secrets/open-cluster-management-agent/open-cluster-management-image-pull-credentials\"
    \n  labels:\n    velero.io/storage-location: default\n  namespace: openshift-adp\nspec:\n
    \ includedNamespaces:\n  - open-cluster-management-agent\n  includedClusterScopedResources:\n
    \ - klusterlets.operator.open-cluster-management.io\n  - clusterroles.rbac.authorization.k8s.io\n
    \ - clusterrolebindings.rbac.authorization.k8s.io\n  - priorityclasses.scheduling.k8s.io\n
    \ includedNamespaceScopedResources:\n  - deployments\n  - serviceaccounts\n  -
    secrets\n  excludedNamespaceScopedResources: []\n---\napiVersion: velero.io/v1\nkind:
    Restore\nmetadata:\n  name: acm-klusterlet\n  namespace: openshift-adp\n  labels:\n
    \   velero.io/storage-location: default\n  annotations:\n    lca.openshift.io/apply-wave:
    \"1\"\nspec:\n  backupName:\n    acm-klusterlet\n`,
		},
	}
}

func (t *clusterRequestReconcilerTask) reconcileUpgrade(
	ctx context.Context, renderedClusterInstance *siteconfig.ClusterInstance) (ctrl.Result, error) {
	oadpCM := generateOADPConfigMap()
	cmNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "openshift-adp"}}
	if err := utils.CreateK8sCR(ctx, t.client, cmNamespace, t.object, utils.UPDATE); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create namespace for oadp configmap: %w", err)
	}
	if err := utils.CreateK8sCR(ctx, t.client, oadpCM, t.object, utils.UPDATE); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create oadp configmap: %w", err)
	}
	clusterTemplate, err := t.getCrClusterTemplateRef(ctx)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to get clusterTemplate: %w", err)
	}
	ibgu := generateIBGUForCluster(t.object, renderedClusterInstance, clusterTemplate)
	err = t.client.Get(ctx, types.NamespacedName{Name: ibgu.Name, Namespace: ibgu.Namespace}, ibgu)
	if err != nil && errors.IsNotFound(err) {
		if err := utils.CreateK8sCR(ctx, t.client, ibgu, t.object, utils.UPDATE); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create IBGU: %w", err)
		}
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.ClusterProvisioned,
			utils.CRconditionReasons.InProgress,
			metav1.ConditionFalse,
			"Upgrade is initiated",
		)
		if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
			return doNotRequeue(), fmt.Errorf("failed to update ClusterRequest CR status: %w", err)
		}
		return requeueWithMediumInterval(), nil
	} else if err != nil {
		return ctrl.Result{}, fmt.Errorf("error getting IBGU: %w", err)
	}
	if IsIBGUProgressing(ibgu) {
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.CRconditionTypes.ClusterProvisioned,
			utils.CRconditionReasons.InProgress,
			metav1.ConditionFalse,
			"Upgrade is in progress",
		)
		if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
			return doNotRequeue(), fmt.Errorf("failed to update ClusterRequest CR status: %w", err)
		}
		return requeueWithMediumInterval(), nil
	}
	// TODO: Check if upgrade is failed
	utils.SetStatusCondition(&t.object.Status.Conditions,
		utils.CRconditionTypes.ClusterProvisioned,
		utils.CRconditionReasons.Completed,
		metav1.ConditionTrue,
		"Upgrade is completed",
	)
	if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return doNotRequeue(), fmt.Errorf("failed to update ClusterRequest CR status: %w", err)
	}
	return doNotRequeue(), nil
}

func IsIBGUProgressing(cr *ibgu.ImageBasedGroupUpgrade) bool {
	condition := meta.FindStatusCondition(cr.Status.Conditions, "Progressing")
	if condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return false
}
