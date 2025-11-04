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
	hwprovisioningapi "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/client/provisioning"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
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

	// List all the root policies in ztp-<ctNamespace>.
	rootPolicies := &policiesv1.PolicyList{}
	listOpts := []client.ListOption{
		client.InNamespace(fmt.Sprintf("ztp-%s", t.ctDetails.namespace)),
	}
	if err := t.client.List(ctx, rootPolicies, listOpts...); err != nil {
		return false, fmt.Errorf("failed to list root Policies: %w", err)
	}

	// Derive the expected child policies set from root policies annotated with the
	// ClusterTemplate reference "<name>.<version>".
	// - If the annotation is absent, or the policy does not match the current ClusterTemplate,
	//   we do not derive an expected set and consider the current present child policies only.
	// - Store each policy's intended remediation to drive requeue/timeout decisions.
	expectedChildPolicies := map[string]string{}
	clusterTemplateRef := fmt.Sprintf("%s.%s", t.object.Spec.TemplateName, t.object.Spec.TemplateVersion)
	for _, rootPolicy := range rootPolicies.Items {
		if ctlrutils.RootPolicyMatchesClusterTemplate(rootPolicy.GetAnnotations(), clusterTemplateRef) {
			t.logger.DebugContext(ctx, fmt.Sprintf("Detected matching root policy (%s in %s) for ClusterTemplate (%s)",
				rootPolicy.GetName(), rootPolicy.GetNamespace(), clusterTemplateRef))
			childPolicyName := fmt.Sprintf("%s.%s", rootPolicy.GetNamespace(), rootPolicy.GetName())
			expectedChildPolicies[childPolicyName] = strings.ToLower(string(rootPolicy.Spec.RemediationAction))
		}
	}

	// Get all the child policies in the namespace of the managed cluster created through
	// the ProvisioningRequest.
	childPolicies := &policiesv1.PolicyList{}
	listOpts = []client.ListOption{
		client.HasLabels{ctlrutils.ChildPolicyRootPolicyLabel},
		client.InNamespace(t.object.Status.Extensions.ClusterDetails.Name),
	}
	err = t.client.List(ctx, childPolicies, listOpts...)
	if err != nil {
		return false, fmt.Errorf("failed to list child Policies: %w", err)
	}

	allChildPoliciesCompliant := true
	allChildPoliciesInInform := true
	currentChildPolicyNames := map[string]bool{}
	var targetChildPolicies []provisioningv1alpha1.PolicyDetails
	// Go through all the policies and get those that are matched with the managed cluster created
	// by the current provisioning request.
	for _, childPolicy := range childPolicies.Items {
		rootPolicyName, rootPolicyNamespace := ctlrutils.GetParentPolicyNameAndNamespace(childPolicy.Name)
		if !ctlrutils.IsParentPolicyInZtpClusterTemplateNs(rootPolicyNamespace, t.ctDetails.namespace) {
			continue
		}

		targetChildPolicy := &provisioningv1alpha1.PolicyDetails{
			Compliant:         string(childPolicy.Status.ComplianceState),
			PolicyName:        rootPolicyName,
			PolicyNamespace:   rootPolicyNamespace,
			RemediationAction: string(childPolicy.Spec.RemediationAction),
		}
		targetChildPolicies = append(targetChildPolicies, *targetChildPolicy)

		if childPolicy.Status.ComplianceState != policiesv1.Compliant {
			allChildPoliciesCompliant = false
		}
		if !strings.EqualFold(string(childPolicy.Spec.RemediationAction), string(policiesv1.Inform)) {
			allChildPoliciesInInform = false
		}

		currentChildPolicyNames[childPolicy.Name] = true
	}

	missingChildPolicies, allExpectedChildPoliciesInInform := t.summarizeExpectedChildPolicies(
		ctx, expectedChildPolicies, currentChildPolicyNames)

	policyConfigTimedOut, err := t.updateConfigurationAppliedStatus(
		ctx, targetChildPolicies, allChildPoliciesCompliant, allChildPoliciesInInform,
		missingChildPolicies, allExpectedChildPoliciesInInform)
	if err != nil {
		return false, err
	}
	err = t.updateZTPStatus(ctx, allChildPoliciesCompliant)
	if err != nil {
		return false, err
	}

	// Requeue only when there's work to converge:
	//  - present enforce policies not yet compliant and not timed out, or
	//  - expected enforce policies not yet created and not timed out.
	// Do not requeue for inform-only states (either present or expected).
	requeue = ((!allChildPoliciesCompliant && !allChildPoliciesInInform) || (missingChildPolicies && !allExpectedChildPoliciesInInform)) && !policyConfigTimedOut
	return requeue, nil
}

// summarizeExpectedChildPolicies summarizes if there are any expected child policies that are not yet present in the cluster,
// and if all expected policies are inform.
func (t *provisioningRequestReconcilerTask) summarizeExpectedChildPolicies(
	ctx context.Context, expectedChildPolicies map[string]string, currentChildPolicies map[string]bool) (bool, bool) {
	allExpectedChildPoliciesInInform := true
	missingChildPolicyNames := []string{}

	for expectedName, expectedRemediationAction := range expectedChildPolicies {
		if !currentChildPolicies[expectedName] {
			missingChildPolicyNames = append(missingChildPolicyNames, expectedName)
		}
		if !strings.EqualFold(expectedRemediationAction, string(policiesv1.Inform)) {
			allExpectedChildPoliciesInInform = false
		}
	}
	if len(missingChildPolicyNames) > 0 {
		t.logger.InfoContext(ctx, fmt.Sprintf("Cluster (%s) is missing the expected policies: %s",
			t.object.Status.Extensions.ClusterDetails.Name, strings.Join(missingChildPolicyNames, ", ")))
		return true, allExpectedChildPoliciesInInform
	}
	return false, allExpectedChildPoliciesInInform
}

// updateConfigurationAppliedStatus updates the ProvisioningRequest ConfigurationApplied condition
// based on:
//   - the set of present child policies (their compliance and remediation)
//   - the set of expected child policies derived from root policies
//   - whether the managed cluster is ready for policy configuration
//
// It sets one of the following reasons (with representative messages):
//   - Missing: No present policies or expected missing policies are all Inform.
//   - Completed: No expected missing policies and all present policies are Compliant.
//   - ClusterNotReady: Cluster not ready to apply policy configuration.
//   - OutOfDate: No expected missing policies and present policies are all Inform and NonCompliant.
//   - InProgress: Expected policies missing or present policies not yet compliant.
//   - TimedOut: Expected policies didn't appear in time, or present policies didn't become compliant in time.
//
// Additional effects:
//   - Maintains status.extensions.clusterDetails.nonCompliantAt to drive timeout checks.
//   - May update provisioning state to InProgress or Failed depending on context.
//
// Returns whether a timeout was detected and any error encountered while updating status.
func (t *provisioningRequestReconcilerTask) updateConfigurationAppliedStatus(
	ctx context.Context,
	targetChildPolicies []provisioningv1alpha1.PolicyDetails,
	allChildPoliciesCompliant bool,
	allChildPoliciesInInform bool,
	missingChildPolicies bool,
	allExpectedChildPoliciesInInform bool,
) (policyConfigTimedOut bool, err error) {
	err = nil
	policyConfigTimedOut = false

	defer func() {
		t.object.Status.Extensions.Policies = targetChildPolicies
		// Update the current policy status.
		if updateErr := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
			err = fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
		} else {
			err = nil
		}
	}()

	if !missingChildPolicies && len(targetChildPolicies) == 0 {
		t.object.Status.Extensions.ClusterDetails.NonCompliantAt = nil
		t.setConfigurationAppliedStatus(
			provisioningv1alpha1.CRconditionReasons.Missing,
			"No configuration present")
		return
	}

	if !missingChildPolicies && allChildPoliciesCompliant {
		t.logger.InfoContext(
			ctx,
			fmt.Sprintf(
				"Cluster (%s) configuration is up to date",
				t.object.Status.Extensions.ClusterDetails.Name,
			),
		)
		t.object.Status.Extensions.ClusterDetails.NonCompliantAt = nil
		t.setConfigurationAppliedStatus(
			provisioningv1alpha1.CRconditionReasons.Completed,
			"The configuration is up to date")
		return
	}

	clusterIsReadyForPolicyConfig, err := ctlrutils.ClusterIsReadyForPolicyConfig(
		ctx, t.client, t.object.Status.Extensions.ClusterDetails.Name,
	)
	if err != nil {
		return policyConfigTimedOut, fmt.Errorf(
			"error determining if the cluster is ready for policy configuration: %w", err)
	}
	if !clusterIsReadyForPolicyConfig {
		t.updateClusterNotReadyStatus(allChildPoliciesInInform)
		return
	}

	if missingChildPolicies {
		// If any expected child policies are missing, gate completion until they appear.
		if allExpectedChildPoliciesInInform {
			t.object.Status.Extensions.ClusterDetails.NonCompliantAt = nil
			t.setConfigurationAppliedStatus(
				provisioningv1alpha1.CRconditionReasons.Missing,
				"Not all expected configuration is present")
			return
		}

		// Check if expected policies generation has timed out.
		policyConfigTimedOut = t.hasPolicyConfigurationTimedOut(ctx)
		t.updatePolicyConfigPreparationStatus(policyConfigTimedOut)
		return
	}

	if allChildPoliciesInInform {
		// No timeout is computed if all policies are in inform, just out of date.
		t.object.Status.Extensions.ClusterDetails.NonCompliantAt = nil
		t.setConfigurationAppliedStatus(
			provisioningv1alpha1.CRconditionReasons.OutOfDate,
			"The configuration is out of date")
		return
	}

	// Check if the policy configuration has timed out.
	policyConfigTimedOut = t.hasPolicyConfigurationTimedOut(ctx)
	t.updatePolicyConfigApplyingStatus(policyConfigTimedOut)
	return
}

func (t *provisioningRequestReconcilerTask) setConfigurationAppliedStatus(
	reason provisioningv1alpha1.ConditionReason, message string) {
	var status metav1.ConditionStatus = metav1.ConditionFalse
	if reason == provisioningv1alpha1.CRconditionReasons.Completed ||
		reason == provisioningv1alpha1.CRconditionReasons.Missing {
		status = metav1.ConditionTrue
	}
	ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
		provisioningv1alpha1.PRconditionTypes.ConfigurationApplied,
		reason,
		status,
		message,
	)
}

func (t *provisioningRequestReconcilerTask) updatePolicyConfigPreparationStatus(policyConfigTimedOut bool) {
	var message string
	var reason provisioningv1alpha1.ConditionReason

	if policyConfigTimedOut {
		message = "Timed out waiting for expected configuration to be present"
		reason = provisioningv1alpha1.CRconditionReasons.TimedOut

		if !ctlrutils.IsClusterUpgradeInitiated(t.object) ||
			ctlrutils.IsClusterUpgradeCompleted(t.object) {
			ctlrutils.SetProvisioningStateFailed(t.object,
				"Cluster configuration timed out")
		}
	} else {
		message = "Expected configuration is not yet prepared"
		reason = provisioningv1alpha1.CRconditionReasons.InProgress

		if !ctlrutils.IsClusterUpgradeInitiated(t.object) ||
			ctlrutils.IsClusterUpgradeCompleted(t.object) {
			ctlrutils.SetProvisioningStateInProgress(t.object,
				"Cluster configuration is being prepared")
		}
	}
	t.setConfigurationAppliedStatus(reason, message)

	t.logger.Info(
		fmt.Sprintf(
			"Cluster (%s) configuration status: %s",
			t.object.Status.Extensions.ClusterDetails.Name,
			message,
		),
	)
}

func (t *provisioningRequestReconcilerTask) updatePolicyConfigApplyingStatus(policyConfigTimedOut bool) {
	var message string
	var reason provisioningv1alpha1.ConditionReason

	if policyConfigTimedOut {
		message = "The configuration is still being applied, but it timed out"
		reason = provisioningv1alpha1.CRconditionReasons.TimedOut

		if !ctlrutils.IsClusterUpgradeInitiated(t.object) ||
			ctlrutils.IsClusterUpgradeCompleted(t.object) {
			ctlrutils.SetProvisioningStateFailed(t.object,
				"Cluster configuration timed out")
		}
	} else {
		message = "The configuration is still being applied"
		reason = provisioningv1alpha1.CRconditionReasons.InProgress

		if !ctlrutils.IsClusterUpgradeInitiated(t.object) ||
			ctlrutils.IsClusterUpgradeCompleted(t.object) {
			ctlrutils.SetProvisioningStateInProgress(t.object,
				"Cluster configuration is being applied")
		}
	}
	t.setConfigurationAppliedStatus(reason, message)

	t.logger.Info(
		fmt.Sprintf(
			"Cluster (%s) configuration status: %s",
			t.object.Status.Extensions.ClusterDetails.Name,
			message,
		),
	)
}

func (t *provisioningRequestReconcilerTask) updateClusterNotReadyStatus(allChildPoliciesInInform bool) {
	t.setConfigurationAppliedStatus(
		provisioningv1alpha1.CRconditionReasons.ClusterNotReady,
		"The Cluster is not yet ready")
	if ctlrutils.IsClusterProvisionCompleted(t.object) &&
		(!ctlrutils.IsClusterUpgradeInitiated(t.object) ||
			ctlrutils.IsClusterUpgradeCompleted(t.object)) &&
		!allChildPoliciesInInform {
		ctlrutils.SetProvisioningStateInProgress(t.object,
			"Waiting for cluster to be ready for policy configuration")
	}

	t.logger.Info(fmt.Sprintf(
		"Cluster (%s) is not ready for policy configuration",
		t.object.Status.Extensions.ClusterDetails.Name,
	))
}

// updateZTPStatus updates status.ClusterDetails.ZtpStatus.
func (t *provisioningRequestReconcilerTask) updateZTPStatus(ctx context.Context, allPoliciesCompliant bool) error {
	// Check if the cluster provision has started.
	crProvisionedCond := meta.FindStatusCondition(t.object.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned))
	if crProvisionedCond != nil {
		// If the provisioning has started, and the ZTP status is empty or not done.
		if t.object.Status.Extensions.ClusterDetails.ZtpStatus != ctlrutils.ClusterZtpDone {
			t.object.Status.Extensions.ClusterDetails.ZtpStatus = ctlrutils.ClusterZtpNotDone
			// If the provisioning finished and all the policies are compliant, then ZTP is done.
			if crProvisionedCond.Status == metav1.ConditionTrue && allPoliciesCompliant {
				// Once the ZTPStatus reaches ZTP Done, it will stay that way.
				t.object.Status.Extensions.ClusterDetails.ZtpStatus = ctlrutils.ClusterZtpDone
			}
		}
	}

	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to update the ZTP status for ProvisioningRequest %s: %w", t.object.Name, err)
	}
	return nil
}

// updateOCloudNodeClusterId stores the clusterID in the provisionedResources status if it exists.
func (t *provisioningRequestReconcilerTask) updateOCloudNodeClusterId(ctx context.Context) (*clusterv1.ManagedCluster, error) {
	managedCluster := &clusterv1.ManagedCluster{}
	managedClusterExists, err := ctlrutils.DoesK8SResourceExist(
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

	// Add the clustertemplates.clcm.openshift.io/templateId label to the ManagedCluster
	// associated to the current ProvisioningRequest.
	err = t.setLabelValue(ctx, mcl, ctlrutils.ClusterTemplateArtifactsLabel, oranct.Spec.TemplateID)
	if err != nil {
		return err
	}

	// Add the needed label to the Agent(s) associated to the current ProvisioningRequest:
	//   clustertemplates.clcm.openshift.io/templateIds
	//   clcm.openshift.io/hardwarePluginRef
	//   clcm.openshift.io/hwMgrNodeId

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

	// Get AllocatedNodes if hardware provisioning is not skipped.
	nodes := &[]hwprovisioningapi.AllocatedNode{}
	if oranct.Spec.Templates.HwTemplate != "" {
		nodeAllocationRequestID := t.getNodeAllocationRequestID()
		if nodeAllocationRequestID != "" {
			nodes, err = t.hwpluginClient.GetAllocatedNodesFromNodeAllocationRequest(ctx, nodeAllocationRequestID)
			if err != nil {
				return fmt.Errorf("failed to get AllocatedNodes for NodeAllocationRequest '%s': %w", nodeAllocationRequestID, err)
			}
		}
	}

	// Go through all the obtained agents and apply the above labels.
	for _, agent := range agents.Items {
		err = t.setLabelValue(ctx, &agent, ctlrutils.ClusterTemplateArtifactsLabel, oranct.Spec.TemplateID)
		if err != nil {
			return err
		}

		if oranct.Spec.Templates.HwTemplate == "" {
			// Skip adding hardwarePluginRef and hwMgrNodeId labels if hardware provisioning is skipped.
			continue
		}

		// Get the corresponding clcm.openshift.io and add the needed labels.
		// Map by hostname.
		foundNode := false

		for _, node := range *nodes {

			// Get the Hostname
			hostName := t.object.Status.Extensions.AllocatedNodeHostMap[node.Id]
			// Skip the Node if its hostname status is empty.
			if hostName == "" {
				continue
			}

			bmh, err := ctlrutils.GetBareMetalHostFromHostname(ctx, t.client, hostName)
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

				err = t.setLabelValue(ctx, &agent, ctlrutils.HardwarePluginRefLabel, t.hwpluginClient.GetHardwarePluginRef())
				if err != nil {
					return err
				}

				err = t.setLabelValue(ctx, &agent, ctlrutils.HardwareManagerNodeIdLabel, string(bmh.UID))
				if err != nil {
					return err
				}

				break
			}
		}
		if !foundNode {
			t.logger.WarnContext(
				ctx,
				"The corresponding clcm.openshift.io Node not found for the %s/%s Agent",
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
				policyTimedOut = ctlrutils.TimeoutExceeded(
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
				policyTimedOut = ctlrutils.TimeoutExceeded(
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
