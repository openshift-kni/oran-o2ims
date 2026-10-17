/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"encoding/json"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
)

// CheckHardwareProfileReferences returns an error when the named HardwareProfile
// is still referenced by a ClusterTemplate or ProvisioningRequest. It is
// injected into the HardwareProfile validating webhook to block deletion of an
// in-use profile.
//
// It lives in this package rather than the api/hardwaremanagement/v1alpha1
// webhook package to avoid an import cycle: api/provisioning/v1alpha1 imports
// api/hardwaremanagement/v1alpha1, so the HardwareProfile webhook package cannot
// import the provisioning API group. internal/controllers/utils can import both.
//
// HardwareProfiles are only ever resolved from the operator namespace (see the
// ClusterTemplate controller's hardware profile resolution), so a profile in any
// other namespace cannot be referenced and is always safe to delete.
func CheckHardwareProfileReferences(ctx context.Context, reader client.Reader, hpName, hpNamespace string) error {
	operatorNamespace := GetEnvOrDefault(constants.DefaultNamespaceEnvName, constants.DefaultNamespace)
	if hpNamespace != operatorNamespace {
		return nil
	}

	if err := checkClusterTemplateReferences(ctx, reader, hpName); err != nil {
		return err
	}
	return checkProvisioningRequestReferences(ctx, reader, hpName)
}

// checkClusterTemplateReferences inspects the inline
// spec.templateDefaults.hwMgmtDefaults.nodeGroupData[].hwProfile of every
// ClusterTemplate across all namespaces.
func checkClusterTemplateReferences(ctx context.Context, reader client.Reader, hpName string) error {
	ctList := &provisioningv1alpha1.ClusterTemplateList{}
	if err := reader.List(ctx, ctList); err != nil {
		return fmt.Errorf("failed to list ClusterTemplates: %w", err)
	}

	for i := range ctList.Items {
		ct := &ctList.Items[i]
		for _, ng := range ct.Spec.TemplateDefaults.HwMgmtDefaults.NodeGroupData {
			if ng.HwProfile == hpName {
				return fmt.Errorf("cannot delete HardwareProfile %q: referenced by ClusterTemplate %q nodeGroup %q",
					hpName, ct.Name, ng.Name)
			}
		}
	}
	return nil
}

// checkProvisioningRequestReferences inspects the
// hwMgmtParameters.nodeGroupData[].hwProfile references carried in each
// ProvisioningRequest's spec.templateParameters. ProvisioningRequest is
// cluster-scoped, so the list is not namespace-filtered. templateParameters is
// free-form JSON: missing or malformed sections are skipped rather than
// treated as references.
func checkProvisioningRequestReferences(ctx context.Context, reader client.Reader, hpName string) error {
	prList := &provisioningv1alpha1.ProvisioningRequestList{}
	if err := reader.List(ctx, prList); err != nil {
		return fmt.Errorf("failed to list ProvisioningRequests: %w", err)
	}

	for i := range prList.Items {
		pr := &prList.Items[i]
		if len(pr.Spec.TemplateParameters.Raw) == 0 {
			continue
		}

		var params map[string]any
		if err := json.Unmarshal(pr.Spec.TemplateParameters.Raw, &params); err != nil {
			// Malformed templateParameters cannot be interpreted as a reference;
			// skip rather than block deletion on unparseable input.
			continue
		}

		hwMgmt, ok := params[constants.TemplateParamHwMgmt].(map[string]any)
		if !ok {
			continue
		}
		nodeGroups, ok := hwMgmt[HwMgmtNodeGroupDataKey].([]any)
		if !ok {
			continue
		}

		for _, ngRaw := range nodeGroups {
			ngMap, ok := ngRaw.(map[string]any)
			if !ok {
				continue
			}
			hwProfile, _ := ngMap["hwProfile"].(string)
			if hwProfile != hpName {
				continue
			}
			ngName, _ := ngMap["name"].(string)
			return fmt.Errorf("cannot delete HardwareProfile %q: referenced by ProvisioningRequest %q nodeGroup %q",
				hpName, pr.Name, ngName)
		}
	}
	return nil
}
