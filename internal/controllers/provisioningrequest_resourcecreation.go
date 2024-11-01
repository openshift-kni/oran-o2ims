package controllers

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
)

// createOrUpdateClusterResources creates/updates all the resources needed for cluster deployment
func (t *provisioningRequestReconcilerTask) createOrUpdateClusterResources(
	ctx context.Context, clusterInstance *siteconfig.ClusterInstance) error {

	clusterName := clusterInstance.GetName()

	// Copy the pull secret from the cluster template namespace to the
	// clusterInstance namespace.
	err := t.createPullSecret(ctx, clusterInstance)
	if err != nil {
		return fmt.Errorf("failed to create pull Secret for cluster %s: %w", clusterName, err)
	}

	// Copy the extra-manifests ConfigMaps from the cluster template namespace
	// to the clusterInstance namespace.
	err = t.createExtraManifestsConfigMap(ctx, clusterInstance)
	if err != nil {
		return fmt.Errorf("failed to create extraManifests ConfigMap for cluster %s: %w", clusterName, err)
	}

	// Create the cluster ConfigMap which will be used by ACM policies.
	err = t.createPoliciesConfigMap(ctx, clusterInstance)
	if err != nil {
		return fmt.Errorf("failed to create policy template ConfigMap for cluster %s: %w", clusterName, err)
	}

	return nil
}

// createPullSecret copies the pull secret from the cluster template namespace
// to the clusterInstance namespace
func (t *provisioningRequestReconcilerTask) createPullSecret(
	ctx context.Context, clusterInstance *siteconfig.ClusterInstance) error {

	clusterTemplateRefName := getClusterTemplateRefName(
		t.object.Spec.TemplateName, t.object.Spec.TemplateVersion)
	// If we got to this point, we can assume that all the keys exist, including
	// clusterName

	// Check the pull secret already exists in the clusterTemplate namespace.
	pullSecret := &corev1.Secret{}
	pullSecretName := clusterInstance.Spec.PullSecretRef.Name
	pullSecretExistsInTemplateNamespace, err := utils.DoesK8SResourceExist(
		ctx, t.client, pullSecretName, t.ctNamespace, pullSecret)
	if err != nil {
		return fmt.Errorf(
			"failed to check if pull secret %s exists in namespace %s: %w",
			pullSecretName, clusterTemplateRefName, err,
		)
	}
	if !pullSecretExistsInTemplateNamespace {
		return utils.NewInputError(
			"pull secret %s expected to exist in the %s namespace, but it is missing",
			pullSecretName, clusterTemplateRefName)
	}

	newClusterInstancePullSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pullSecretName,
			Namespace: clusterInstance.Name,
		},
		Data: pullSecret.Data,
		Type: corev1.SecretTypeDockerConfigJson,
	}

	if err := utils.CreateK8sCR(ctx, t.client, newClusterInstancePullSecret, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Kubernetes CR for ClusterInstancePullSecret: %w", err)
	}

	return nil
}

// createExtraManifestsConfigMap copies the extra-manifests ConfigMaps from the
// cluster template namespace to the clusterInstance namespace.
func (t *provisioningRequestReconcilerTask) createExtraManifestsConfigMap(
	ctx context.Context, clusterInstance *siteconfig.ClusterInstance) error {
	for _, extraManifestsRef := range clusterInstance.Spec.ExtraManifestsRefs {
		// Make sure the extra-manifests ConfigMap exists in the clusterTemplate namespace.
		configMap := &corev1.ConfigMap{}
		extraManifestCmName := extraManifestsRef.Name
		configMapExists, err := utils.DoesK8SResourceExist(
			ctx, t.client, extraManifestCmName, t.ctNamespace, configMap)
		if err != nil {
			return fmt.Errorf("failed to check if ConfigMap exists: %w", err)
		}
		if !configMapExists {
			return utils.NewInputError(
				"extra-manifests configmap %s expected to exist in the %s namespace, but it is missing",
				extraManifestCmName, t.ctNamespace)
		}

		// Create the extra-manifests ConfigMap in the clusterInstance namespace
		newExtraManifestsConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      extraManifestCmName,
				Namespace: clusterInstance.Name,
			},
			Data: configMap.Data,
		}
		if err := utils.CreateK8sCR(ctx, t.client, newExtraManifestsConfigMap, t.object, utils.UPDATE); err != nil {
			return fmt.Errorf("failed to create extra-manifests ConfigMap: %w", err)
		}
	}

	return nil
}

// createClusterInstanceNamespace creates the namespace of the ClusterInstance
// where all the other resources needed for installation will exist.
func (t *provisioningRequestReconcilerTask) createClusterInstanceNamespace(
	ctx context.Context, clusterName string) error {

	// Create the namespace.
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
	}

	// Add ProvisioningRequest labels to the namespace
	labels := make(map[string]string)
	labels[provisioningRequestNameLabel] = t.object.Name
	namespace.SetLabels(labels)

	err := utils.CreateK8sCR(ctx, t.client, namespace, t.object, "")
	if err != nil {
		return fmt.Errorf("failed to create or update namespace %s: %w", clusterName, err)
	}

	if namespace.Status.Phase == corev1.NamespaceTerminating {
		return utils.NewInputError("the namespace %s is terminating", clusterName)
	}

	return nil
}

// createPoliciesConfigMap creates the cluster ConfigMap which will be used
// by the ACM policies.
func (t *provisioningRequestReconcilerTask) createPoliciesConfigMap(
	ctx context.Context, clusterInstance *siteconfig.ClusterInstance) error {

	// Check the cluster version for the cluster-version label.
	clusterLabels := clusterInstance.Spec.ExtraLabels["ManagedCluster"]
	if err := checkClusterLabelsForPolicies(clusterInstance.Name, clusterLabels); err != nil {
		return fmt.Errorf("failed to check cluster labels: %w", err)
	}

	return t.createPolicyTemplateConfigMap(ctx, clusterInstance.Name)
}

// createPolicyTemplateConfigMap updates the keys of the default ConfigMap to match the
// clusterTemplate and the cluster version and creates/updates the ConfigMap for the
// required version of the policy template.
func (t *provisioningRequestReconcilerTask) createPolicyTemplateConfigMap(
	ctx context.Context, clusterName string) error {

	// If there is no policy configuration data, log a message and return without an error.
	if len(t.clusterInput.policyTemplateData) == 0 {
		t.logger.InfoContext(ctx, "Policy template data is empty")
		return nil
	}

	// Update the keys to match the ClusterTemplate name and the version.
	finalPolicyTemplateData := make(map[string]string)
	for key, value := range t.clusterInput.policyTemplateData {
		finalPolicyTemplateData[key] = value.(string)
	}

	// Put all the data from the mergedPolicyTemplateData in a configMap in the same
	// namespace as the templated ACM policies.
	// The namespace is: ztp + <clustertemplate-namespace>
	policyTemplateConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-pg", clusterName),
			Namespace: fmt.Sprintf("ztp-%s", t.ctNamespace),
		},
		Data: finalPolicyTemplateData,
	}

	if err := utils.CreateK8sCR(ctx, t.client, policyTemplateConfigMap, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Kubernetes CR: %w", err)
	}

	return nil
}

// checkClusterLabelsForPolicies checks if the cluster_version
// label exist for a certain ClusterInstance and returns it.
func checkClusterLabelsForPolicies(
	clusterName string, clusterLabels map[string]string) error {

	if len(clusterLabels) == 0 {
		return utils.NewInputError(
			"No cluster labels configured by the ClusterInstance %s(%s). "+
				"Labels are needed for cluster configuration",
			clusterName, clusterName,
		)
	}

	// Make sure the cluster-version label exists.
	_, clusterVersionLabelExists := clusterLabels[utils.ClusterVersionLabelKey]
	if !clusterVersionLabelExists {
		return utils.NewInputError(
			"Managed cluster %s is missing the %s label. This label is needed for correctly "+
				"generating and populating configuration data",
			clusterName, utils.ClusterVersionLabelKey,
		)
	}
	return nil
}
