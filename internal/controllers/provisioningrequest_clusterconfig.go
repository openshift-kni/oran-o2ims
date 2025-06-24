/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	assistedservicev1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
)

// handleClusterPolicyConfiguration updates the ProvisioningRequest status to reflect the status
// of the policies that match the managed cluster created through the ProvisioningRequest.
func (t *provisioningRequestReconcilerTask) handleClusterPolicyConfiguration(ctx context.Context) (
	requeue bool, err error) {
	if t.object.Status.Extensions.ClusterDetails == nil {
		return false, fmt.Errorf("status.clusterDetails is empty")
	}

	// Get all the child policies in the namespace of the managed cluster created through
	// the ProvisioningRequest.
	policies := &policiesv1.PolicyList{}
	listOpts := []client.ListOption{
		client.HasLabels{utils.ChildPolicyRootPolicyLabel},
		client.InNamespace(t.object.Status.Extensions.ClusterDetails.Name),
	}

	err = t.client.List(ctx, policies, listOpts...)
	if err != nil {
		return false, fmt.Errorf("failed to list Policies: %w", err)
	}

	allPoliciesCompliant := true
	allPoliciesInInform := true
	var targetPolicies []provisioningv1alpha1.PolicyDetails
	// Go through all the policies and get those that are matched with the managed cluster created
	// by the current provisioning request.
	for _, policy := range policies.Items {
		targetPolicyName, targetPolicyNamespace := utils.GetParentPolicyNameAndNamespace(policy.Name)
		if !utils.IsParentPolicyInZtpClusterTemplateNs(targetPolicyNamespace, t.ctDetails.namespace) {
			continue
		}

		targetPolicy := &provisioningv1alpha1.PolicyDetails{
			Compliant:         string(policy.Status.ComplianceState),
			PolicyName:        targetPolicyName,
			PolicyNamespace:   targetPolicyNamespace,
			RemediationAction: string(policy.Spec.RemediationAction),
		}
		targetPolicies = append(targetPolicies, *targetPolicy)

		if policy.Status.ComplianceState != policiesv1.Compliant {
			allPoliciesCompliant = false
		}
		if !strings.EqualFold(string(policy.Spec.RemediationAction), string(policiesv1.Inform)) {
			allPoliciesInInform = false
		}
	}
	policyConfigTimedOut, err := t.updateConfigurationAppliedStatus(
		ctx, targetPolicies, allPoliciesCompliant, allPoliciesInInform)
	if err != nil {
		return false, err
	}
	err = t.updateZTPStatus(ctx, allPoliciesCompliant)
	if err != nil {
		return false, err
	}

	// If there are policies that are not Compliant and the configuration has not timed out,
	// we need to requeue and see if the timeout is reached.
	return (!allPoliciesCompliant && !allPoliciesInInform) && !policyConfigTimedOut, nil
}

// updateConfigurationAppliedStatus updates the ProvisioningRequest ConfigurationApplied condition
// based on the state of the policies matched with the managed cluster.
func (t *provisioningRequestReconcilerTask) updateConfigurationAppliedStatus(
	ctx context.Context, targetPolicies []provisioningv1alpha1.PolicyDetails, allPoliciesCompliant bool,
	allPoliciesInInform bool) (policyConfigTimedOut bool, err error) {
	err = nil
	policyConfigTimedOut = false

	defer func() {
		t.object.Status.Extensions.Policies = targetPolicies
		// Update the current policy status.
		if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
			err = fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
		} else {
			err = nil
		}
	}()

	if len(targetPolicies) == 0 {
		t.object.Status.Extensions.ClusterDetails.NonCompliantAt = nil
		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ConfigurationApplied,
			provisioningv1alpha1.CRconditionReasons.Missing,
			metav1.ConditionTrue,
			"No configuration present",
		)
		return
	}

	// Update the ConfigurationApplied condition.
	if allPoliciesCompliant {
		t.logger.InfoContext(
			ctx,
			fmt.Sprintf(
				"Cluster (%s) configuration is up to date",
				t.object.Status.Extensions.ClusterDetails.Name,
			),
		)
		t.object.Status.Extensions.ClusterDetails.NonCompliantAt = nil
		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ConfigurationApplied,
			provisioningv1alpha1.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"The configuration is up to date",
		)
		return
	}

	clusterIsReadyForPolicyConfig, err := utils.ClusterIsReadyForPolicyConfig(
		ctx, t.client, t.object.Status.Extensions.ClusterDetails.Name,
	)
	if err != nil {
		return policyConfigTimedOut, fmt.Errorf(
			"error determining if the cluster is ready for policy configuration: %w", err)
	}

	if !clusterIsReadyForPolicyConfig {
		t.logger.InfoContext(
			ctx,
			fmt.Sprintf(
				"Cluster (%s) is not ready for policy configuration",
				t.object.Status.Extensions.ClusterDetails.Name,
			),
		)
		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ConfigurationApplied,
			provisioningv1alpha1.CRconditionReasons.ClusterNotReady,
			metav1.ConditionFalse,
			"The Cluster is not yet ready",
		)
		if utils.IsClusterProvisionCompleted(t.object) &&
			(!utils.IsClusterUpgradeInitiated(t.object) ||
				utils.IsClusterUpgradeCompleted(t.object)) &&
			!allPoliciesInInform {
			utils.SetProvisioningStateInProgress(t.object,
				"Waiting for cluster to be ready for policy configuration")
		}
		return
	}

	var message string
	if allPoliciesInInform {
		// No timeout is computed if all policies are in inform, just out of date.
		t.object.Status.Extensions.ClusterDetails.NonCompliantAt = nil
		message = "The configuration is out of date"
		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ConfigurationApplied,
			provisioningv1alpha1.CRconditionReasons.OutOfDate,
			metav1.ConditionFalse,
			message,
		)
	} else {
		policyConfigTimedOut = t.hasPolicyConfigurationTimedOut(ctx)

		message = "The configuration is still being applied"
		reason := provisioningv1alpha1.CRconditionReasons.InProgress
		if !utils.IsClusterUpgradeInitiated(t.object) ||
			utils.IsClusterUpgradeCompleted(t.object) {
			utils.SetProvisioningStateInProgress(t.object,
				"Cluster configuration is being applied")
		}
		if policyConfigTimedOut {
			message += ", but it timed out"
			reason = provisioningv1alpha1.CRconditionReasons.TimedOut

			if !utils.IsClusterUpgradeInitiated(t.object) ||
				utils.IsClusterUpgradeCompleted(t.object) {
				utils.SetProvisioningStateFailed(t.object,
					"Cluster configuration timed out")
			}
		}
		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ConfigurationApplied,
			reason,
			metav1.ConditionFalse,
			message,
		)
	}
	t.logger.InfoContext(
		ctx,
		fmt.Sprintf(
			"Cluster (%s) configuration status: %s",
			t.object.Status.Extensions.ClusterDetails.Name,
			message,
		),
	)

	return
}

// updateZTPStatus updates status.ClusterDetails.ZtpStatus.
func (t *provisioningRequestReconcilerTask) updateZTPStatus(ctx context.Context, allPoliciesCompliant bool) error {
	// Check if the cluster provision has started.
	crProvisionedCond := meta.FindStatusCondition(t.object.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned))
	if crProvisionedCond != nil {
		// If the provisioning has started, and the ZTP status is empty or not done.
		if t.object.Status.Extensions.ClusterDetails.ZtpStatus != utils.ClusterZtpDone {
			t.object.Status.Extensions.ClusterDetails.ZtpStatus = utils.ClusterZtpNotDone
			// If the provisioning finished and all the policies are compliant, then ZTP is done.
			if crProvisionedCond.Status == metav1.ConditionTrue && allPoliciesCompliant {
				// Once the ZTPStatus reaches ZTP Done, it will stay that way.
				t.object.Status.Extensions.ClusterDetails.ZtpStatus = utils.ClusterZtpDone
			}
		}
	}

	if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to update the ZTP status for ProvisioningRequest %s: %w", t.object.Name, err)
	}
	return nil
}

// updateOCloudNodeClusterId stores the clusterID in the provisionedResources status if it exists.
func (t *provisioningRequestReconcilerTask) updateOCloudNodeClusterId(ctx context.Context) (*clusterv1.ManagedCluster, error) {
	managedCluster := &clusterv1.ManagedCluster{}
	managedClusterExists, err := utils.DoesK8SResourceExist(
		ctx, t.client, t.object.Status.Extensions.ClusterDetails.Name, "", managedCluster)
	if err != nil {
		return managedCluster, fmt.Errorf("failed to check if managed cluster exists: %w", err)
	}

	if managedClusterExists {
		// If the clusterID label exists, set it in the provisionedResources.
		clusterID, exists := managedCluster.GetLabels()["clusterID"]
		if exists {
			if t.object.Status.ProvisioningStatus.ProvisionedResources == nil {
				t.object.Status.ProvisioningStatus.ProvisionedResources = &provisioningv1alpha1.ProvisionedResources{}
			}
			t.object.Status.ProvisioningStatus.ProvisionedResources.OCloudNodeClusterId = clusterID
		}
	}
	return managedCluster, nil
}

// addPostProvisioningLabels adds multiple labels on the ManagedCluster and Agent objects that
// are associated to the current ProvisioningRequest.
// These labels are useful in the proper functioning of the resource server.
func (t *provisioningRequestReconcilerTask) addPostProvisioningLabels(ctx context.Context, mcl *clusterv1.ManagedCluster) error {
	// Get the ClusterTemplate used by the current ProvisioningRequest.
	oranct, err := t.object.GetClusterTemplateRef(ctx, t.client)
	if err != nil {
		return fmt.Errorf("failed to get ClusterTemplate: %w", err)
	}

	// Add the clustertemplates.o2ims.provisioning.oran.org/templateId label to the ManagedCluster
	// associated to the current ProvisioningRequest.
	err = t.setLabelValue(ctx, mcl, utils.ClusterTemplateArtifactsLabel, oranct.Spec.TemplateID)
	if err != nil {
		return err
	}

	// Add the needed label to the Agent(s) associated to the current ProvisioningRequest:
	//   clustertemplates.o2ims.provisioning.oran.org/templateIds
	//   o2ims-hardwaremanagement.oran.openshift.io/hardwarePluginRef
	//   o2ims-hardwaremanagement.oran.openshift.io/hwMgrNodeId

	agents := &assistedservicev1beta1.AgentList{}
	listOpts := []client.ListOption{
		client.MatchingLabels{
			"agent-install.openshift.io/clusterdeployment-namespace": mcl.Name,
		},
	}

	err = t.client.List(ctx, agents, listOpts...)
	if err != nil {
		return fmt.Errorf("failed to list Agents in the %s namespace: %w", mcl.Name, err)
	}

	if len(agents.Items) == 0 {
		return fmt.Errorf("the expected Agents were not found in the %s namespace", mcl.Name)
	}

	// Get AllocatedNodes
	nodeAllocationRequestID := t.getNodeAllocationRequestID()
	nodes, err := t.hwpluginClient.GetAllocatedNodesFromNodeAllocationRequest(ctx, nodeAllocationRequestID)
	if err != nil {
		return fmt.Errorf("failed to get AllocatedNodes for NodeAllocationRequest '%s': %w", nodeAllocationRequestID, err)
	}

	// Go through all the obtained agents and apply the above labels.
	for _, agent := range agents.Items {
		err = t.setLabelValue(ctx, &agent, utils.ClusterTemplateArtifactsLabel, oranct.Spec.TemplateID)
		if err != nil {
			return err
		}

		if oranct.Spec.Templates.HwTemplate == "" {
			// Skip adding hardwarePluginRef and hwMgrNodeId labels if hardware provisioning is skipped.
			continue
		}

		// Get the corresponding o2ims-hardwaremanagement.oran.openshift.io and add the needed labels.
		// Map by hostname.
		foundNode := false

		for _, node := range *nodes {

			// Get the Hostname
			hostName := t.object.Status.Extensions.AllocatedNodeHostMap[node.Id]
			// Skip the Node if its hostname status is empty.
			if hostName == "" {
				continue
			}

			bmh, err := utils.GetBareMetalHostFromHostname(ctx, t.client, hostName)
			if err != nil {
				return fmt.Errorf("failed to retrieve BareMetalHost corresponding with hostname '%s': %w", hostName, err)
			}
			if bmh == nil {
				continue
			}

			if bmh.Status.HardwareDetails.Hostname != hostName {
				return fmt.Errorf("bareMetalHost Hostname '%s' is different from the ProvisioningRequest's Hostname: '%s'",
					bmh.Status.HardwareDetails.Hostname, hostName)
			}

			if hostName == agent.Spec.Hostname {
				foundNode = true

				err = t.setLabelValue(ctx, &agent, utils.HardwarePluginRefLabel, t.hwpluginClient.GetHardwarePluginRef())
				if err != nil {
					return err
				}

				err = t.setLabelValue(ctx, &agent, utils.HardwareManagerNodeIdLabel, bmh.Name)
				if err != nil {
					return err
				}

				break
			}
		}
		if !foundNode {
			t.logger.WarnContext(
				ctx,
				"The corresponding o2ims-hardwaremanagement.oran.openshift.io Node not found for the %s/%s Agent",
				agent.Name, agent.Namespace,
			)
		}
	}

	return nil
}

// setLabelValue sets a desired label on a K8S Object.
func (t *provisioningRequestReconcilerTask) setLabelValue(
	ctx context.Context, object client.Object, labelKey string, labelValue string) error {
	allLabels := object.GetLabels()
	value, exists := allLabels[labelKey]
	if !exists || (exists && value != labelValue) {
		if len(allLabels) == 0 {
			allLabels = make(map[string]string)
		}
		allLabels[labelKey] = labelValue
		object.SetLabels(allLabels)
		if err := t.client.Update(ctx, object); err != nil {
			return fmt.Errorf("failed to update status for %s %s: %w", object.GetObjectKind(), object.GetName(), err)
		}
	}

	return nil
}

// hasPolicyConfigurationTimedOut determines if the policy configuration for the
// ProvisioningRequest has timed out.
func (t *provisioningRequestReconcilerTask) hasPolicyConfigurationTimedOut(ctx context.Context) bool {
	policyTimedOut := false
	// Get the ConfigurationApplied condition.
	configurationAppliedCondition := meta.FindStatusCondition(
		t.object.Status.Conditions,
		string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))

	// If the condition does not exist, set the non compliant timestamp since we
	// get here just for policies that have a status different from Compliant.
	if configurationAppliedCondition == nil {
		t.object.Status.Extensions.ClusterDetails.NonCompliantAt = &metav1.Time{Time: time.Now()}
		return policyTimedOut
	}

	// If the current status of the Condition is false.
	if configurationAppliedCondition.Status == metav1.ConditionFalse {
		switch configurationAppliedCondition.Reason {
		case string(provisioningv1alpha1.CRconditionReasons.InProgress):
			// Check if the configuration application has timed out.
			if t.object.Status.Extensions.ClusterDetails.NonCompliantAt.IsZero() {
				t.object.Status.Extensions.ClusterDetails.NonCompliantAt = &metav1.Time{Time: time.Now()}
			} else {
				// If NonCompliantAt has been previously set, check for timeout.
				policyTimedOut = utils.TimeoutExceeded(
					t.object.Status.Extensions.ClusterDetails.NonCompliantAt.Time,
					t.timeouts.clusterConfiguration)
			}
		case string(provisioningv1alpha1.CRconditionReasons.TimedOut):
			policyTimedOut = true
		case string(provisioningv1alpha1.CRconditionReasons.Missing):
			t.object.Status.Extensions.ClusterDetails.NonCompliantAt = &metav1.Time{Time: time.Now()}
		case string(provisioningv1alpha1.CRconditionReasons.OutOfDate):
			t.object.Status.Extensions.ClusterDetails.NonCompliantAt = &metav1.Time{Time: time.Now()}
		case string(provisioningv1alpha1.CRconditionReasons.ClusterNotReady):
			// The cluster might not be ready because its being initially provisioned or
			// there are problems after provisionion, so it might be that NonCompliantAt
			// has been previously set.
			if !t.object.Status.Extensions.ClusterDetails.NonCompliantAt.IsZero() {
				// If NonCompliantAt has been previously set, check for timeout.
				policyTimedOut = utils.TimeoutExceeded(
					t.object.Status.Extensions.ClusterDetails.NonCompliantAt.Time,
					t.timeouts.clusterConfiguration)
			}
		default:
			t.logger.InfoContext(ctx,
				fmt.Sprintf("Unexpected Reason for condition type %s",
					provisioningv1alpha1.PRconditionTypes.ConfigurationApplied,
				),
			)
		}
	} else if configurationAppliedCondition.Reason == string(provisioningv1alpha1.CRconditionReasons.Completed) {
		t.object.Status.Extensions.ClusterDetails.NonCompliantAt = &metav1.Time{Time: time.Now()}
	}

	return policyTimedOut
}
