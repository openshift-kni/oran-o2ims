package controllers

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
)

// handleClusterPolicyConfiguration updates the ProvisioningRequest status to reflect the status
// of the policies that match the managed cluster created through the ProvisioningRequest.
func (t *provisioningRequestReconcilerTask) handleClusterPolicyConfiguration(ctx context.Context) (
	requeue bool, err error) {
	if t.object.Status.ClusterDetails == nil {
		return false, fmt.Errorf("status.clusterDetails is empty")
	}

	// Get all the child policies in the namespace of the managed cluster created through
	// the ProvisioningRequest.
	policies := &policiesv1.PolicyList{}
	listOpts := []client.ListOption{
		client.HasLabels{utils.ChildPolicyRootPolicyLabel},
		client.InNamespace(t.object.Status.ClusterDetails.Name),
	}

	err = t.client.List(ctx, policies, listOpts...)
	if err != nil {
		return false, fmt.Errorf("failed to list Policies: %w", err)
	}

	allPoliciesCompliant := true
	nonCompliantPolicyInEnforce := false
	var targetPolicies []provisioningv1alpha1.PolicyDetails
	// Go through all the policies and get those that are matched with the managed cluster created
	// by the current provisioning request.
	for _, policy := range policies.Items {
		if policy.Status.ComplianceState != policiesv1.Compliant {
			allPoliciesCompliant = false
			if strings.EqualFold(string(policy.Spec.RemediationAction), string(policiesv1.Enforce)) {
				nonCompliantPolicyInEnforce = true
			}
		}
		// Child policy name = parent_policy_namespace.parent_policy_name
		policyNameArr := strings.Split(policy.Name, ".")
		targetPolicy := &provisioningv1alpha1.PolicyDetails{
			Compliant:         string(policy.Status.ComplianceState),
			PolicyName:        policyNameArr[1],
			PolicyNamespace:   policyNameArr[0],
			RemediationAction: string(policy.Spec.RemediationAction),
		}
		targetPolicies = append(targetPolicies, *targetPolicy)
	}
	policyConfigTimedOut, err := t.updateConfigurationAppliedStatus(
		ctx, targetPolicies, allPoliciesCompliant, nonCompliantPolicyInEnforce)
	if err != nil {
		return false, err
	}
	err = t.updateZTPStatus(ctx, allPoliciesCompliant)
	if err != nil {
		return false, err
	}
	err = t.finalizeProvisioningIfComplete(ctx, allPoliciesCompliant)
	if err != nil {
		return false, err
	}

	// If there are policies that are not Compliant and the configuration has not timed out,
	// we need to requeue and see if the timeout is reached.
	return nonCompliantPolicyInEnforce && !policyConfigTimedOut, nil
}

// updateConfigurationAppliedStatus updates the ProvisioningRequest ConfigurationApplied condition
// based on the state of the policies matched with the managed cluster.
func (t *provisioningRequestReconcilerTask) updateConfigurationAppliedStatus(
	ctx context.Context, targetPolicies []provisioningv1alpha1.PolicyDetails, allPoliciesCompliant bool,
	nonCompliantPolicyInEnforce bool) (policyConfigTimedOut bool, err error) {
	err = nil
	policyConfigTimedOut = false

	defer func() {
		t.object.Status.Policies = targetPolicies
		// Update the current policy status.
		if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
			err = fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
		} else {
			err = nil
		}
	}()

	if len(targetPolicies) == 0 {
		t.object.Status.ClusterDetails.NonCompliantAt = metav1.Time{}
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.Missing,
			metav1.ConditionFalse,
			"No configuration present",
		)
		return
	}

	// Update the ConfigurationApplied condition.
	if allPoliciesCompliant {
		t.object.Status.ClusterDetails.NonCompliantAt = metav1.Time{}
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"The configuration is up to date",
		)
		return
	}

	clusterIsReadyForPolicyConfig, err := utils.ClusterIsReadyForPolicyConfig(
		ctx, t.client, t.object.Status.ClusterDetails.Name,
	)
	if err != nil {
		return policyConfigTimedOut, fmt.Errorf(
			"error determining if the cluster is ready for policy configuration: %w", err)
	}

	if !clusterIsReadyForPolicyConfig {
		t.logger.InfoContext(
			ctx,
			fmt.Sprintf(
				"Cluster %s (%s) is not ready for policy configuration",
				t.object.Status.ClusterDetails.Name,
				t.object.Status.ClusterDetails.Name,
			),
		)
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.ClusterNotReady,
			metav1.ConditionFalse,
			"The Cluster is not yet ready",
		)
		if utils.IsClusterProvisionCompleted(t.object) &&
			nonCompliantPolicyInEnforce {
			utils.SetProvisioningStateInProgress(t.object,
				"Waiting for cluster to be ready for policy configuration")
		}
		return
	}

	if nonCompliantPolicyInEnforce {
		policyConfigTimedOut = t.hasPolicyConfigurationTimedOut(ctx)

		message := "The configuration is still being applied"
		reason := utils.CRconditionReasons.InProgress
		utils.SetProvisioningStateInProgress(t.object,
			"Cluster configuration is being applied")
		if policyConfigTimedOut {
			message += ", but it timed out"
			reason = utils.CRconditionReasons.TimedOut
			utils.SetProvisioningStateFailed(t.object,
				"Cluster configuration timed out")
		}
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ConfigurationApplied,
			reason,
			metav1.ConditionFalse,
			message,
		)
	} else {
		// No timeout is reported if all policies are in inform, just out of date.
		t.object.Status.ClusterDetails.NonCompliantAt = metav1.Time{}
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.OutOfDate,
			metav1.ConditionFalse,
			"The configuration is out of date",
		)
	}

	return
}

// updateZTPStatus updates status.ClusterDetails.ZtpStatus.
func (t *provisioningRequestReconcilerTask) updateZTPStatus(ctx context.Context, allPoliciesCompliant bool) error {
	// Check if the cluster provision has started.
	crProvisionedCond := meta.FindStatusCondition(t.object.Status.Conditions, string(utils.PRconditionTypes.ClusterProvisioned))
	if crProvisionedCond != nil {
		// If the provisioning has started, and the ZTP status is empty or not done.
		if t.object.Status.ClusterDetails.ZtpStatus != utils.ClusterZtpDone {
			t.object.Status.ClusterDetails.ZtpStatus = utils.ClusterZtpNotDone
			// If the provisioning finished and all the policies are compliant, then ZTP is done.
			if crProvisionedCond.Status == metav1.ConditionTrue && allPoliciesCompliant {
				// Once the ZTPStatus reaches ZTP Done, it will stay that way.
				t.object.Status.ClusterDetails.ZtpStatus = utils.ClusterZtpDone
			}
		}
	}

	if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to update the ZTP status for ProvisioningRequest %s: %w", t.object.Name, err)
	}
	return nil
}

// updateOCloudNodeClusterId stores the clusterID in the provisionedResources status if it exists.
func (t *provisioningRequestReconcilerTask) updateOCloudNodeClusterId(ctx context.Context) error {
	managedCluster := &clusterv1.ManagedCluster{}
	managedClusterExists, err := utils.DoesK8SResourceExist(
		ctx, t.client, t.object.Status.ClusterDetails.Name, "", managedCluster)
	if err != nil {
		return fmt.Errorf("failed to check if managed cluster exists: %w", err)
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
	return nil
}

// finalizeProvisioningIfComplete checks if the provisioning process is completed.
// If so, it sets the provisioning state to "fulfilled" and updates the provisioned
// resources in the status.
func (t *provisioningRequestReconcilerTask) finalizeProvisioningIfComplete(ctx context.Context, allPoliciesCompliant bool) error {
	if utils.IsClusterProvisionCompleted(t.object) && allPoliciesCompliant {
		utils.SetProvisioningStateFulfilled(t.object)
		if err := t.updateOCloudNodeClusterId(ctx); err != nil {
			return err
		}
	}

	if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, err)
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
		string(utils.PRconditionTypes.ConfigurationApplied))

	// If the condition does not exist, set the non compliant timestamp since we
	// get here just for policies that have a status different from Compliant.
	if configurationAppliedCondition == nil {
		t.object.Status.ClusterDetails.NonCompliantAt = metav1.Now()
		return policyTimedOut
	}

	// If the current status of the Condition is false.
	if configurationAppliedCondition.Status == metav1.ConditionFalse {
		switch configurationAppliedCondition.Reason {
		case string(utils.CRconditionReasons.InProgress):
			// Check if the configuration application has timed out.
			if t.object.Status.ClusterDetails.NonCompliantAt.IsZero() {
				t.object.Status.ClusterDetails.NonCompliantAt = metav1.Now()
			} else {
				// If NonCompliantAt has been previously set, check for timeout.
				policyTimedOut = utils.TimeoutExceeded(
					t.object.Status.ClusterDetails.NonCompliantAt.Time,
					t.timeouts.clusterConfiguration)
			}
		case string(utils.CRconditionReasons.TimedOut):
			policyTimedOut = true
		case string(utils.CRconditionReasons.Missing):
			t.object.Status.ClusterDetails.NonCompliantAt = metav1.Now()
		case string(utils.CRconditionReasons.OutOfDate):
			t.object.Status.ClusterDetails.NonCompliantAt = metav1.Now()
		case string(utils.CRconditionReasons.ClusterNotReady):
			// The cluster might not be ready because its being initially provisioned or
			// there are problems after provisionion, so it might be that NonCompliantAt
			// has been previously set.
			if !t.object.Status.ClusterDetails.NonCompliantAt.IsZero() {
				// If NonCompliantAt has been previously set, check for timeout.
				policyTimedOut = utils.TimeoutExceeded(
					t.object.Status.ClusterDetails.NonCompliantAt.Time,
					t.timeouts.clusterConfiguration)
			}
		default:
			t.logger.InfoContext(ctx,
				fmt.Sprintf("Unexpected Reason for condition type %s",
					utils.PRconditionTypes.ConfigurationApplied,
				),
			)
		}
	} else if configurationAppliedCondition.Reason == string(utils.CRconditionReasons.Completed) {
		t.object.Status.ClusterDetails.NonCompliantAt = metav1.Now()
	}

	return policyTimedOut
}
