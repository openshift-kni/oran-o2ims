/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"encoding/base64"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
)

// createOrUpdateClusterResources creates/updates all the resources needed for cluster deployment
func (t *provisioningRequestReconcilerTask) createOrUpdateClusterResources(
	ctx context.Context, clusterInstance *siteconfig.ClusterInstance) error {

	clusterName := clusterInstance.GetName()

	// Create BMC secret if no hardware provisioning
	if t.isHardwareProvisionSkipped() {
		err := t.createClusterInstanceBMCSecrets(ctx, clusterName)
		if err != nil {
			return err
		}
	}

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

	// Check the pull secret exists in the clusterTemplate namespace.
	pullSecret := &corev1.Secret{}
	pullSecretName := clusterInstance.Spec.PullSecretRef.Name
	pullSecretExistsInTemplateNamespace, err := utils.DoesK8SResourceExist(
		ctx, t.client, pullSecretName, t.ctDetails.namespace, pullSecret)
	if err != nil {
		return fmt.Errorf(
			"failed to check if pull secret %s exists in namespace %s: %w",
			pullSecretName, t.ctDetails.namespace, err,
		)
	}
	if !pullSecretExistsInTemplateNamespace {
		return utils.NewInputError(
			"pull secret %s expected to exist in the %s namespace, but it is missing",
			pullSecretName, t.ctDetails.namespace)
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
			ctx, t.client, extraManifestCmName, t.ctDetails.namespace, configMap)
		if err != nil {
			return fmt.Errorf("failed to check if ConfigMap exists: %w", err)
		}
		if !configMapExists {
			return utils.NewInputError(
				"extra-manifests configmap %s expected to exist in the %s namespace, but it is missing",
				extraManifestCmName, t.ctDetails.namespace)
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

	if clusterName == "" {
		return fmt.Errorf("spec.clusterName cannot be empty")
	}

	// Create the namespace.
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
	}

	// Add ProvisioningRequest labels to the namespace
	labels := make(map[string]string)
	labels[provisioningv1alpha1.ProvisioningRequestNameLabel] = t.object.Name
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
		data, ok := value.(string)
		if !ok {
			return utils.NewInputError(
				"policyTemplateParameters/policyTemplateSchema for the %s key (%v) is not a string",
				key, value)
		}
		finalPolicyTemplateData[key] = data
	}

	// Put all the data from the mergedPolicyTemplateData in a configMap in the same
	// namespace as the templated ACM policies.
	// The namespace is: ztp + <clustertemplate-namespace>
	policyTemplateConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-pg", clusterName),
			Namespace: fmt.Sprintf("ztp-%s", t.ctDetails.namespace),
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

// createClusterInstanceBMCSecrets creates all the BMC secrets needed by the nodes included
// in the ProvisioningRequest.
func (t *provisioningRequestReconcilerTask) createClusterInstanceBMCSecrets(
	ctx context.Context, clusterName string) error {

	// The BMC credential details are obtained from the ProvisioningRequest.
	clusterInstanceMatchingInput, err := provisioningv1alpha1.ExtractMatchingInput(
		t.object.Spec.TemplateParameters.Raw, utils.TemplateParamClusterInstance)
	if err != nil {
		return utils.NewInputError(
			"failed to extract matching input for subSchema %s: %w", utils.TemplateParamClusterInstance, err)
	}
	clusterInstanceMatchingInputMap := clusterInstanceMatchingInput.(map[string]any)

	nodes, nodesExists := clusterInstanceMatchingInputMap["nodes"]
	if !nodesExists {
		return utils.NewInputError(
			`\"nodes\" key expected to exist in spec.templateParameters.clusterInstanceParameters `+
				`of ProvisioningRequest %s, but it is missing`,
			t.object.Name,
		)
	}
	// Go through all the nodes.
	for _, nodeInterface := range nodes.([]any) {
		node := nodeInterface.(map[string]any)
		username, password, secretName, err :=
			getBMCDetailsForClusterInstance(node, t.object.Name)
		if err != nil {
			return err
		}

		// Create the node's BMC secret.
		bmcSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: clusterName,
			},
			Data: map[string][]byte{
				"username": username,
				"password": password,
			},
		}

		if err = utils.CreateK8sCR(ctx, t.client, bmcSecret, nil, utils.UPDATE); err != nil {
			return fmt.Errorf("failed to create BMC secret: %w", err)
		}
	}

	return nil
}

func getBMCDetailsForClusterInstance(node map[string]any, provisioningRequest string) (
	[]byte, []byte, string, error) {
	// Get the BMC details.
	bmcCredentialsDetailsInterface, bmcCredentialsDetailsExist := node["bmcCredentialsDetails"]
	if !bmcCredentialsDetailsExist {
		return nil, nil, "", utils.NewInputError(
			`\"bmcCredentialsDetails\" key expected to exist in `+
				`spec.templateParameters.clusterInstanceParameters `+
				`of ProvisioningRequest %s, but it's missing`,
			provisioningRequest,
		)
	}
	bmcCredentialsDetails := bmcCredentialsDetailsInterface.(map[string]any)

	// Get the BMC username and password.
	usernameBase64, usernameExists := bmcCredentialsDetails["username"].(string)
	if !usernameExists {
		return nil, nil, "", utils.NewInputError(
			`\"bmcCredentialsDetails.username\" key expected to exist in `+
				`spec.templateParameters.clusterInstanceParameters `+
				`of ProvisioningRequest %s, but it's missing`,
			provisioningRequest,
		)
	}
	username, err := base64.StdEncoding.DecodeString(usernameBase64)
	if err != nil {
		return nil, nil, "", utils.NewInputError(
			"failed to decode usernameBase64 string (%s): %w", username, err)
	}

	passwordBase64, passwordExists := bmcCredentialsDetails["password"].(string)
	if !passwordExists {
		return nil, nil, "", utils.NewInputError(
			`\"bmcCredentialsDetails.password\" key expected to exist in `+
				`spec.templateParameters.clusterInstanceParameters `+
				`of ProvisioningRequest %s, but it's missing`,
			provisioningRequest,
		)
	}
	password, err := base64.StdEncoding.DecodeString(passwordBase64)
	if err != nil {
		return nil, nil, "", utils.NewInputError(
			"failed to decode passwordBase64 string (%s): %w", passwordBase64, err)
	}

	secretName := ""
	// Get the BMC CredentialsName.
	bmcCredentialsNameInterface, bmcCredentialsNameExist := node["bmcCredentialsName"]
	if !bmcCredentialsNameExist {
		secretName, err = utils.GenerateSecretName(node, provisioningRequest)
		if err != nil {
			return nil, nil, "", utils.NewInputError("failed to generate Secret name: %w", err)
		}
	} else {
		secretName = bmcCredentialsNameInterface.(map[string]any)["name"].(string)
	}

	return username, password, secretName, nil
}
