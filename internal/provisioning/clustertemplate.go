/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package provisioning

import (
	"context"
	"fmt"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetClusterTemplateRef finds a validated ClusterTemplate matching the request's name and version.
func GetClusterTemplateRef(ctx context.Context, c client.Client, r *provisioningv1alpha1.ProvisioningRequest) (*provisioningv1alpha1.ClusterTemplate, error) {
	clusterTemplateRefName := fmt.Sprintf("%s.%s", r.Spec.TemplateName, r.Spec.TemplateVersion)
	clusterTemplates := &provisioningv1alpha1.ClusterTemplateList{}

	// Get the one clusterTemplate that's valid.
	err := c.List(ctx, clusterTemplates)
	// If there was an error in trying to get the ClusterTemplate, return it.
	if err != nil {
		return nil, fmt.Errorf("failed to get ClusterTemplate: %w", err)
	}
	for _, ct := range clusterTemplates.Items {
		if ct.Name == clusterTemplateRefName {
			validatedCond := meta.FindStatusCondition(
				ct.Status.Conditions,
				string(provisioningv1alpha1.CTconditionTypes.Validated))
			if validatedCond != nil && validatedCond.Status == metav1.ConditionTrue {
				return &ct, nil
			}
		}
	}

	// If the referenced ClusterTemplate does not exist, log and return an appropriate error.
	return nil, fmt.Errorf(
		"a valid ClusterTemplate (%s) does not exist in any namespace",
		clusterTemplateRefName)
}
