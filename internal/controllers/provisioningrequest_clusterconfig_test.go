/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Assisted-by: Cursor/claude-4-sonnet
*/

/*
Test Cases for ProvisioningRequest Cluster Configuration

This file contains unit tests for the cluster configuration phase of ProvisioningRequest processing,
specifically focusing on policy management, configuration timeouts, and post-provisioning labeling.

Test Suites:

1. policyManagement - Tests for handling ACM (Advanced Cluster Management) policies during cluster configuration:
   • Does not handle the policy configuration without the cluster provisioning having started
   • Moves from Missing to OutOfDate when expected child policies are all Inform and not yet Compliant
   • Does not set NonCompliantAt timestamp when the cluster is NotReady even if expected policies are missing
   • Sets InProgress and requeues when an expected enforce child policy is missing, then times out
   • Moves from preparing InProgress to applying InProgress when expected child policies appear NonCompliant, then times out
   • Moves from InProgress to Completed when all bound (expected and present) policies are Compliant
   • Moves from TimedOut to Completed if all the policies are compliant
   • Clears the NonCompliantAt timestamp and timeout when policies are switched to inform
   • It transitions from InProgress to ClusterNotReady to InProgress
   • It sets ClusterNotReady if the cluster is unstable/not ready
   • Sets the NonCompliantAt timestamp and times out
   • Sets InProgress when the enforce policy is Compliant but the inform policy is still NonCompliant and times out
   • Sets Missing if there are no policies
   • It handles updated/deleted policies for matched clusters
   • It does not requeue ProvisioningRequest matched by policies outside the ztp-<clustertemplate-ns> namespace
   • It handles changes to the ClusterTemplate
   • Sets Completed when the cluster is Compliant with all the matched policies
   • Sets InProgress when the cluster is NonCompliant with at least one enforce policy
   • Sets InProgress when the cluster is Pending with at least one enforce policy

2. hasPolicyConfigurationTimedOut - Tests for policy configuration timeout logic:
   • Returns false if the status is unexpected and NonCompliantAt is not set
   • Returns false if the status is Completed and sets NonCompliantAt
   • Returns false if the status is OutOfDate and sets NonCompliantAt
   • Returns false if the status is Missing and sets NonCompliantAt
   • Returns true if the status is InProgress and the timeout has passed
   • Sets NonCompliantAt if there is no ConfigurationApplied condition

3. addPostProvisioningLabels - Tests for adding labels to resources after provisioning:
   • When the HW template is provided and the HW CRs do not exist:
     - Returns error for the NodeAllocationRequest missing
     - Returns error for missing Nodes
   • When the HW template is provided and the expected HW CRs exist:
     - Updates Agent and ManagedCluster labels as expected
     - Fails to get a ClusterTemplate
     - Sets the label for MNO when there are multiple Agents
     - Fails for multiple Agents with unexpected labels
   • When the HW template is not provided:
     - Does not add hardwarePluginRef and hwMgrNodeId labels to the Agents
*/

package controllers

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	hwmgrpluginapi "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/client/provisioning"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	testutils "github.com/openshift-kni/oran-o2ims/test/utils"
	assistedservicev1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
)

var _ = Describe("policyManagement", func() {
	var (
		c            client.Client
		ctx          context.Context
		CRReconciler *ProvisioningRequestReconciler
		CRTask       *provisioningRequestReconcilerTask
		CTReconciler *ClusterTemplateReconciler
		tName        = "clustertemplate-a"
		tVersion     = "v1.0.0"
		ctNamespace  = "clustertemplate-a-v4-16"
		ciDefaultsCm = "clusterinstance-defaults-v1"
		ptDefaultsCm = "policytemplate-defaults-v1"
		hwTemplate   = "hwTemplate-v1"
		clusterName  = "cluster-1"
	)

	var createRootPolicy = func(
		name string, anns map[string]string,
		remediationAction string,
	) {
		Expect(c.Create(ctx, &policiesv1.Policy{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Namespace:   fmt.Sprintf("ztp-%s", ctNamespace),
				Annotations: anns,
			},
			Spec: policiesv1.PolicySpec{
				RemediationAction: policiesv1.RemediationAction(remediationAction)},
		})).To(Succeed())
	}

	var createChildPolicy = func(
		name string, anns map[string]string,
		remediationAction string,
		complianceState string,
	) {
		Expect(c.Create(ctx, &policiesv1.Policy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: clusterName,
				Labels: map[string]string{
					utils.ChildPolicyRootPolicyLabel:       name,
					utils.ChildPolicyClusterNameLabel:      clusterName,
					utils.ChildPolicyClusterNamespaceLabel: clusterName,
				},
				Annotations: anns,
			},
			Spec: policiesv1.PolicySpec{
				RemediationAction: policiesv1.RemediationAction(remediationAction),
			},
			Status: policiesv1.PolicyStatus{
				ComplianceState: policiesv1.ComplianceState(complianceState),
			},
		})).To(Succeed())
	}

	BeforeEach(func() {
		// Initialize context
		ctx = context.Background()

		// Define the needed resources.
		clusterInstanceCRD, err := utils.BuildTestClusterInstanceCRD(utils.TestClusterInstanceSpecOk)
		Expect(err).ToNot(HaveOccurred())
		crs := []client.Object{
			// Cluster Template Namespace.
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ctNamespace,
				},
			},
			// Cluster Template.
			&provisioningv1alpha1.ClusterTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      GetClusterTemplateRefName(tName, tVersion),
					Namespace: ctNamespace,
				},
				Spec: provisioningv1alpha1.ClusterTemplateSpec{
					Name:       tName,
					Version:    tVersion,
					TemplateID: "57b39bda-ac56-4143-9b10-d1a71517d04f",
					Release:    "4.15.0",
					Templates: provisioningv1alpha1.Templates{
						ClusterInstanceDefaults: ciDefaultsCm,
						PolicyTemplateDefaults:  ptDefaultsCm,
						HwTemplate:              hwTemplate,
					},
					TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testutils.TestFullTemplateSchema)},
				},
				Status: provisioningv1alpha1.ClusterTemplateStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(provisioningv1alpha1.CTconditionTypes.Validated),
							Reason: string(provisioningv1alpha1.CTconditionReasons.Completed),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			// ConfigMaps.
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ciDefaultsCm,
					Namespace: ctNamespace,
				},
				Data: map[string]string{
					utils.ClusterInstanceTemplateDefaultsConfigmapKey: `
clusterImageSetNameRef: "4.15.0"
pullSecretRef:
  name: "pull-secret"
templateRefs:
- name: "ai-cluster-templates-v1"
  namespace: "siteconfig-operator"
nodes:
- hostName: "node1"
  role: master
  ironicInspect: ""
  automatedCleaningMode: "disabled"
  bootMode: "UEFI"
  nodeNetwork:
    interfaces:
    - name: eno1
      label: boot-interface
    - name: eth0
      label: base-interface
    - name: eth1
      label: data-interface
  templateRefs:
  - name: "ai-node-templates-v1"
    namespace: "siteconfig-operator"
`,
				},
			},
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foobar1",
					Namespace: ctNamespace,
				},
				Data: map[string]string{},
			},
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ptDefaultsCm,
					Namespace: ctNamespace,
				},
				Data: map[string]string{
					utils.PolicyTemplateDefaultsConfigmapKey: `
cpu-isolated: "2-31"
cpu-reserved: "0-1"
defaultHugepagesSize: "1G"`,
				},
			},
			// hardware template
			&hwmgmtv1alpha1.HardwareTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      hwTemplate,
					Namespace: utils.InventoryNamespace,
				},
				Spec: hwmgmtv1alpha1.HardwareTemplateSpec{
					HardwarePluginRef:           utils.UnitTestHwPluginRef,
					HardwareProvisioningTimeout: "1m",
					NodeGroupData: []hwmgmtv1alpha1.NodeGroupData{
						{
							Name:           "controller",
							Role:           "master",
							ResourcePoolId: "xyz",
							HwProfile:      "profile-spr-single-processor-64G",
						},
						{
							Name:           "worker",
							Role:           "worker",
							ResourcePoolId: "xyz",
							HwProfile:      "profile-spr-dual-processor-128G",
						},
					},
				},
			},
			// Pull secret.
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pull-secret",
					Namespace: ctNamespace,
				},
			},
			// ClusterInstance CRD.
			clusterInstanceCRD,
			// Provisioning Requests.
			&provisioningv1alpha1.ProvisioningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "cluster-1",
					Finalizers: []string{provisioningv1alpha1.ProvisioningRequestFinalizer},
				},
				Spec: provisioningv1alpha1.ProvisioningRequestSpec{
					TemplateName:    tName,
					TemplateVersion: tVersion,
					TemplateParameters: runtime.RawExtension{
						Raw: []byte(testutils.TestFullTemplateParameters),
					},
				},
				Status: provisioningv1alpha1.ProvisioningRequestStatus{
					// Fake the hw provision status
					Conditions: []metav1.Condition{
						{
							Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
							Status: metav1.ConditionTrue,
						},
					},
					Extensions: provisioningv1alpha1.Extensions{
						NodeAllocationRequestRef: &provisioningv1alpha1.NodeAllocationRequestRef{
							NodeAllocationRequestID: "cluster-1", // Use the default ID that exists in mock server
						},
					},
				},
			},
			// Managed clusters
			&clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
				},
				Spec: clusterv1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
			},
			// ClusterImageSet for the test
			&hivev1.ClusterImageSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "4.15.0",
				},
				Spec: hivev1.ClusterImageSetSpec{
					ReleaseImage: "quay.io/openshift-release-dev/ocp-release:4.15.0-x86_64",
				},
			},
		}

		c = getFakeClientFromObjects(crs...)

		// Reconcile the ClusterTemplate.
		CTReconciler = &ClusterTemplateReconciler{
			Client: c,
			Logger: logger,
		}

		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      GetClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
		}

		_, err = CTReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())

		CRReconciler = &ProvisioningRequestReconciler{
			Client:         c,
			Logger:         logger,
			CallbackConfig: utils.NewNarCallbackConfig(constants.DefaultNarCallbackServicePort),
		}

		// Create the provisioned NodeAllocationRequest
		hwPluginNs := &corev1.Namespace{}
		hwPluginNs.SetName(constants.DefaultNamespace)
		Expect(c.Create(ctx, hwPluginNs)).To(Succeed())
		nodeAllocationRequest := &pluginsv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-1",
				Namespace: constants.DefaultNamespace,
			},
			Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
				HardwarePluginRef: utils.UnitTestHwPluginRef,
				NodeGroup: []pluginsv1alpha1.NodeGroup{
					{
						NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
							Name:           "controller",
							Role:           "master",
							HwProfile:      "profile-spr-single-processor-64G",
							ResourcePoolId: "xyz",
						},
						Size: 1,
					},
					{
						NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
							Name:           "worker",
							Role:           "worker",
							HwProfile:      "profile-spr-dual-processor-128G",
							ResourcePoolId: "xyz",
						},
						Size: 0,
					},
				},
			},
			Status: pluginsv1alpha1.NodeAllocationRequestStatus{
				Conditions: []metav1.Condition{
					{
						Type:   string(hwmgmtv1alpha1.Provisioned),
						Status: metav1.ConditionTrue,
						Reason: string(hwmgmtv1alpha1.Completed),
					},
				},
				Properties: pluginsv1alpha1.Properties{
					NodeNames: []string{testutils.MasterNodeName},
				},
			},
		}
		Expect(c.Create(ctx, nodeAllocationRequest)).To(Succeed())
		testutils.CreateNodeResources(ctx, c, nodeAllocationRequest.Name)

		// Update the managedCluster cluster-1 to be available, joined and accepted.
		managedCluster1 := &clusterv1.ManagedCluster{}
		managedClusterExists, err := utils.DoesK8SResourceExist(
			ctx, c, "cluster-1", "", managedCluster1)
		Expect(err).ToNot(HaveOccurred())
		Expect(managedClusterExists).To(BeTrue())
		utils.SetStatusCondition(&managedCluster1.Status.Conditions,
			provisioningv1alpha1.ConditionType(clusterv1.ManagedClusterConditionAvailable),
			"ManagedClusterAvailable",
			metav1.ConditionTrue,
			"Managed cluster is available",
		)
		utils.SetStatusCondition(&managedCluster1.Status.Conditions,
			provisioningv1alpha1.ConditionType(clusterv1.ManagedClusterConditionHubAccepted),
			"HubClusterAdminAccepted",
			metav1.ConditionTrue,
			"Accepted by hub cluster admin",
		)
		utils.SetStatusCondition(&managedCluster1.Status.Conditions,
			provisioningv1alpha1.ConditionType(clusterv1.ManagedClusterConditionJoined),
			"ManagedClusterJoined",
			metav1.ConditionTrue,
			"Managed cluster joined",
		)
		err = CRReconciler.Client.Status().Update(context.TODO(), managedCluster1)
		Expect(err).ToNot(HaveOccurred())

		// Initial reconciliation of the ProvisioningRequest.
		req = reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}
		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())

		// Create the ProvisioningRequest reconciliation task.
		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{Name: "cluster-1"},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger:       CRReconciler.Logger,
			client:       CRReconciler.Client,
			object:       provisioningRequest, // cluster-1 request
			ctDetails:    &clusterTemplateDetails{namespace: ctNamespace},
			clusterInput: &clusterInput{},
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterInstallationTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
			callbackConfig: utils.NewNarCallbackConfig(constants.DefaultNarCallbackServicePort),
		}
	})

	It("Does not handle the policy configuration without the cluster provisioning having started", func() {
		// Create the policies, all Compliant, one in inform and one in enforce.
		createChildPolicy(fmt.Sprintf("ztp-%s.v1-subscriptions-policy", ctNamespace), map[string]string{}, "enforce", "NonCompliant")
		createChildPolicy(fmt.Sprintf("ztp-%s.v1-sriov-configuration-policy", ctNamespace), map[string]string{}, "inform", "Compliant")

		// Check the current ConfigurationApplied status condition.
		conditions := CRTask.object.Status.Conditions
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).To(BeNil())

		// Add the ClusterProvisioned condition.
		utils.SetStatusCondition(&CRTask.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ClusterProvisioned,
			provisioningv1alpha1.CRconditionReasons.InProgress,
			metav1.ConditionFalse,
			"Provisioning cluster",
		)
		currentTime := metav1.Now()
		CRTask.object.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt = &currentTime
		Expect(c.Status().Update(ctx, CRTask.object)).To(Succeed())

		result, err := CRTask.run(ctx)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())
		Expect(result.RequeueAfter).To(Equal(5 * time.Minute)) // Long interval
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).ToNot(BeZero())
		Expect(CRTask.object.Status.Extensions.Policies).ToNot(BeEmpty())
	})

	It("Moves from Missing to OutOfDate when expected child policies are all Inform and not yet Compliant", func() {
		// Annotated expected root policies
		annot := map[string]string{
			utils.CTPolicyTemplatesAnnotation: GetClusterTemplateRefName(tName, tVersion),
		}

		// Create the expected root policies.
		createRootPolicy("v2-inform-1", annot, "inform")
		createRootPolicy("v2-inform-2", annot, "inform")
		// Create one expected child policy.
		createChildPolicy(fmt.Sprintf("ztp-%s.v2-inform-1", ctNamespace), annot, "inform", "Compliant")
		// Create a child policy that is present but its annotation does not contain the current ClusterTemplate.
		createChildPolicy(fmt.Sprintf("ztp-%s.v1-enforce", ctNamespace),
			map[string]string{utils.CTPolicyTemplatesAnnotation: GetClusterTemplateRefName(tName, "v1.0.1")}, "enforce", "Compliant")

		// First run: missing policies but all expected policies are inform, so set Missing and no requeue.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(requeue).To(BeFalse())

		cond := meta.FindStatusCondition(CRTask.object.Status.Conditions,
			string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(cond).ToNot(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		Expect(cond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.Missing)))
		Expect(cond.Message).To(Equal("Not all expected configuration is present"))
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())
		// The policies should include all bound policies including the one that does not contain the current ClusterTemplate.
		Expect(CRTask.object.Status.Extensions.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "Compliant",
					PolicyName:        "v2-inform-1",
					PolicyNamespace:   fmt.Sprintf("ztp-%s", ctNamespace),
					RemediationAction: "inform",
				},
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-enforce",
					PolicyNamespace:   fmt.Sprintf("ztp-%s", ctNamespace),
					RemediationAction: "enforce",
				},
			},
		))

		// Unbind the unexpected child policy before the second run.
		policy := &policiesv1.Policy{}
		Expect(c.Get(ctx, client.ObjectKey{
			Name: fmt.Sprintf("ztp-%s.v1-enforce", ctNamespace), Namespace: clusterName,
		}, policy)).To(Succeed())
		Expect(c.Delete(ctx, policy)).To(Succeed())
		// Create the other expected child policy before the second run.
		createChildPolicy(fmt.Sprintf("ztp-%s.v2-inform-2", ctNamespace), annot, "inform", "NonCompliant")

		// Second run: all expected inform policies are present but not compliant, so set OutOfDate and no requeue.
		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(requeue).To(BeFalse())
		cond = meta.FindStatusCondition(CRTask.object.Status.Conditions,
			string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(cond).ToNot(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.OutOfDate)))
		Expect(cond.Message).To(Equal("The configuration is out of date"))
		Expect(CRTask.object.Status.Extensions.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "Compliant",
					PolicyName:        "v2-inform-1",
					PolicyNamespace:   fmt.Sprintf("ztp-%s", ctNamespace),
					RemediationAction: "inform",
				},
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v2-inform-2",
					PolicyNamespace:   fmt.Sprintf("ztp-%s", ctNamespace),
					RemediationAction: "inform",
				},
			},
		))
	})

	It("Does not set NonCompliantAt timestamp when the cluster is NotReady even if expected policies are missing", func() {
		annot := map[string]string{
			utils.CTPolicyTemplatesAnnotation: GetClusterTemplateRefName(tName, tVersion) + "," + GetClusterTemplateRefName(tName, "v1.0.1"),
		}
		// One expected Enforce, one expected Inform
		createRootPolicy("v2-inform", annot, "inform")
		createRootPolicy("v2-enforce", annot, "enforce")
		// Create expected child for Inform; Enforce child remains missing
		createChildPolicy(fmt.Sprintf("ztp-%s.v2-inform", ctNamespace), annot, "inform", "NonCompliant")

		currentTime := metav1.Now()
		CRTask.object.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt = &currentTime
		Expect(c.Status().Update(ctx, CRTask.object)).To(Succeed())

		// Update the managed cluster to be not available.
		managedCluster1 := &clusterv1.ManagedCluster{}
		managedClusterExists, err := utils.DoesK8SResourceExist(
			ctx, c, "cluster-1", "", managedCluster1)
		Expect(err).ToNot(HaveOccurred())
		Expect(managedClusterExists).To(BeTrue())
		utils.SetStatusCondition(&managedCluster1.Status.Conditions,
			provisioningv1alpha1.ConditionType(clusterv1.ManagedClusterConditionAvailable),
			"ManagedClusterAvailable",
			metav1.ConditionFalse,
			"Managed cluster is not available",
		)
		err = c.Status().Update(ctx, managedCluster1)
		Expect(err).ToNot(HaveOccurred())

		// Missing expected enforce policy, but cluster is not ready, so set ClusterNotReady.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
		cond := meta.FindStatusCondition(CRTask.object.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(cond).ToNot(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.ClusterNotReady)))
		Expect(cond.Message).To(Equal("The Cluster is not yet ready"))
		// NonCompliantAt should be zero because the cluster is not ready.
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())
		Expect(CRTask.object.Status.ProvisioningStatus.ProvisioningPhase).To(Equal(provisioningv1alpha1.StateProgressing))
		Expect(CRTask.object.Status.Extensions.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v2-inform",
					PolicyNamespace:   fmt.Sprintf("ztp-%s", ctNamespace),
					RemediationAction: "inform",
				},
			},
		))
	})

	It("Sets InProgress and requeues when an expected enforce child policy is missing, then times out", func() {
		annot := map[string]string{
			utils.CTPolicyTemplatesAnnotation: GetClusterTemplateRefName(tName, tVersion) + "," + GetClusterTemplateRefName(tName, "v1.0.1"),
		}

		// One expected Enforce, one expected Inform, one expected Enforce but missing annotation
		createRootPolicy("v2-inform", annot, "inform")
		createRootPolicy("v2-enforce", annot, "enforce")
		createRootPolicy("v2-enforce-missing-annotation", map[string]string{}, "enforce")
		// Create expected child for Inform; Enforce child with annotation remains missing
		createChildPolicy(fmt.Sprintf("ztp-%s.v2-inform", ctNamespace), annot, "inform", "NonCompliant")
		// Create the expected child policy that is missing annotation.
		createChildPolicy(fmt.Sprintf("ztp-%s.v2-enforce-missing-annotation", ctNamespace), map[string]string{}, "enforce", "NonCompliant")

		// First run: missing expected enforce policy, so set InProgress and requeue.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(requeue).To(BeTrue())
		cond := meta.FindStatusCondition(CRTask.object.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(cond).ToNot(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.InProgress)))
		Expect(cond.Message).To(Equal("Expected configuration is not yet prepared"))
		Expect(CRTask.object.Status.ProvisioningStatus.ProvisioningPhase).To(Equal(provisioningv1alpha1.StateProgressing))
		Expect(CRTask.object.Status.ProvisioningStatus.ProvisioningDetails).To(Equal("Cluster configuration is being prepared"))
		// The policies should include all bound policies including the one that is missing the ClusterTemplate annotation.
		Expect(CRTask.object.Status.Extensions.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v2-inform",
					PolicyNamespace:   fmt.Sprintf("ztp-%s", ctNamespace),
					RemediationAction: "inform",
				},
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v2-enforce-missing-annotation",
					PolicyNamespace:   fmt.Sprintf("ztp-%s", ctNamespace),
					RemediationAction: "enforce",
				},
			},
		))

		// Simulate timeout
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt.IsZero()).To(BeFalse())
		CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt.Time =
			CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt.Add(-2 * utils.DefaultClusterConfigurationTimeout)
		Expect(c.Status().Update(ctx, CRTask.object)).To(Succeed())

		// Second run: all expected policies are present but timed out, so set TimedOut and no requeue.
		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(requeue).To(BeFalse())
		cond = meta.FindStatusCondition(CRTask.object.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(cond).ToNot(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.TimedOut)))
		Expect(cond.Message).To(Equal("Timed out waiting for expected configuration to be present"))
		Expect(CRTask.object.Status.ProvisioningStatus.ProvisioningPhase).To(Equal(provisioningv1alpha1.StateFailed))
		Expect(CRTask.object.Status.ProvisioningStatus.ProvisioningDetails).To(Equal("Cluster configuration timed out"))
	})

	It("Moves from preparing InProgress to applying InProgress when expected child policies appear NonCompliant, then times out", func() {
		annot := map[string]string{
			utils.CTPolicyTemplatesAnnotation: GetClusterTemplateRefName(tName, tVersion) + "," + GetClusterTemplateRefName(tName, "v1.0.1"),
		}
		// Expected Enforce and Inform root policies with no child created yet.
		createRootPolicy("v2-enforce", annot, "enforce")
		createRootPolicy("v2-inform", annot, "inform")

		// First run: missing expected policies both enforce and inform, so set InProgress and requeue.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(requeue).To(BeTrue())
		cond := meta.FindStatusCondition(CRTask.object.Status.Conditions,
			string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(cond).ToNot(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.InProgress)))
		Expect(cond.Message).To(Equal("Expected configuration is not yet prepared"))
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).ToNot(BeZero())
		Expect(CRTask.object.Status.ProvisioningStatus.ProvisioningPhase).To(Equal(provisioningv1alpha1.StateProgressing))
		Expect(CRTask.object.Status.ProvisioningStatus.ProvisioningDetails).To(Equal("Cluster configuration is being prepared"))
		Expect(CRTask.object.Status.Extensions.Policies).To(BeEmpty())

		// Create expected child policies as NonCompliant.
		createChildPolicy(fmt.Sprintf("ztp-%s.v2-enforce", ctNamespace), annot, "enforce", "NonCompliant")
		createChildPolicy(fmt.Sprintf("ztp-%s.v2-inform", ctNamespace), annot, "inform", "NonCompliant")

		// Second run: all expected policies are present but not compliant, so set InProgress and requeue.
		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(requeue).To(BeTrue())
		cond = meta.FindStatusCondition(CRTask.object.Status.Conditions,
			string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(cond).ToNot(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.InProgress)))
		Expect(cond.Message).To(Equal("The configuration is still being applied"))
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).ToNot(BeZero())
		Expect(CRTask.object.Status.ProvisioningStatus.ProvisioningPhase).To(Equal(provisioningv1alpha1.StateProgressing))
		Expect(CRTask.object.Status.ProvisioningStatus.ProvisioningDetails).To(Equal("Cluster configuration is being applied"))
		Expect(CRTask.object.Status.Extensions.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v2-enforce",
					PolicyNamespace:   fmt.Sprintf("ztp-%s", ctNamespace),
					RemediationAction: "enforce",
				},
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v2-inform",
					PolicyNamespace:   fmt.Sprintf("ztp-%s", ctNamespace),
					RemediationAction: "inform",
				},
			},
		))

		// Simulate timeout
		CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt.Time =
			CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt.Add(-2 * utils.DefaultClusterConfigurationTimeout)
		Expect(c.Status().Update(ctx, CRTask.object)).To(Succeed())

		// Third run: all expected policies are present but timed out, so set TimedOut and no requeue.
		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(requeue).To(BeFalse())
		cond = meta.FindStatusCondition(CRTask.object.Status.Conditions,
			string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(cond).ToNot(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.TimedOut)))
		Expect(cond.Message).To(Equal("The configuration is still being applied, but it timed out"))
		Expect(CRTask.object.Status.ProvisioningStatus.ProvisioningPhase).To(Equal(provisioningv1alpha1.StateFailed))
		Expect(CRTask.object.Status.ProvisioningStatus.ProvisioningDetails).To(Equal("Cluster configuration timed out"))
	})

	It("Moves from InProgress to Completed when all bound (expected and present) policies are Compliant", func() {
		annot := map[string]string{
			utils.CTPolicyTemplatesAnnotation: GetClusterTemplateRefName(tName, tVersion) + "," + GetClusterTemplateRefName(tName, "v1.0.2"),
		}
		// Create expected root policies with expected ClusterTemplate in the annotation.
		createRootPolicy("v2-enforce", annot, "enforce")
		createRootPolicy("v2-inform", annot, "inform")
		// Create a root policy that is missing the expected ClusterTemplate in the annotation.
		createRootPolicy("v2-enforce-missing-ct", map[string]string{
			utils.CTPolicyTemplatesAnnotation: GetClusterTemplateRefName(tName, "v1.0.2"),
		}, "enforce")
		// Create expected child policies as Compliant.
		createChildPolicy(fmt.Sprintf("ztp-%s.v2-enforce", ctNamespace), annot, "enforce", "Compliant")
		createChildPolicy(fmt.Sprintf("ztp-%s.v2-inform", ctNamespace), annot, "inform", "Compliant")
		// Create the child policy that is missing the expected ClusterTemplate in the annotation, and is NonCompliant.
		createChildPolicy(fmt.Sprintf("ztp-%s.v2-enforce-missing-ct", ctNamespace), map[string]string{
			utils.CTPolicyTemplatesAnnotation: GetClusterTemplateRefName(tName, "v1.0.2"),
		}, "enforce", "NonCompliant")

		// First run: all expected policies are present and compliant, but the one that is missing
		// the expected ClusterTemplate in the annotation is NonCompliant, so set InProgress and requeue.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(requeue).To(BeTrue())
		cond := meta.FindStatusCondition(CRTask.object.Status.Conditions,
			string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(cond).ToNot(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.InProgress)))
		Expect(cond.Message).To(Equal("The configuration is still being applied"))
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).ToNot(BeZero())
		Expect(CRTask.object.Status.Extensions.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "Compliant",
					PolicyName:        "v2-enforce",
					PolicyNamespace:   fmt.Sprintf("ztp-%s", ctNamespace),
					RemediationAction: "enforce",
				},
				{
					Compliant:         "Compliant",
					PolicyName:        "v2-inform",
					PolicyNamespace:   fmt.Sprintf("ztp-%s", ctNamespace),
					RemediationAction: "inform",
				},
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v2-enforce-missing-ct",
					PolicyNamespace:   fmt.Sprintf("ztp-%s", ctNamespace),
					RemediationAction: "enforce",
				},
			},
		))

		// Set the policy that is missing the expected ClusterTemplate in its annotation to Compliant.
		policy := &policiesv1.Policy{}
		Expect(c.Get(ctx, client.ObjectKey{
			Name: fmt.Sprintf("ztp-%s.v2-enforce-missing-ct", ctNamespace), Namespace: clusterName,
		}, policy)).To(Succeed())
		policy.Status.ComplianceState = policiesv1.Compliant
		Expect(c.Status().Update(ctx, policy)).To(Succeed())

		// Second run: all bound policies are compliant, so set Completed and no requeue.
		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(requeue).To(BeFalse())
		cond = meta.FindStatusCondition(CRTask.object.Status.Conditions,
			string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(cond).ToNot(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		Expect(cond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.Completed)))
		Expect(cond.Message).To(Equal("The configuration is up to date"))
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())
		Expect(CRTask.object.Status.Extensions.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "Compliant",
					PolicyName:        "v2-enforce",
					PolicyNamespace:   fmt.Sprintf("ztp-%s", ctNamespace),
					RemediationAction: "enforce",
				},
				{
					Compliant:         "Compliant",
					PolicyName:        "v2-inform",
					PolicyNamespace:   fmt.Sprintf("ztp-%s", ctNamespace),
					RemediationAction: "inform",
				},
				{
					Compliant:         "Compliant",
					PolicyName:        "v2-enforce-missing-ct",
					PolicyNamespace:   fmt.Sprintf("ztp-%s", ctNamespace),
					RemediationAction: "enforce",
				},
			},
		))
	})

	It("Moves from TimedOut to Completed if all the policies are compliant", func() {
		// Update the ProvisioningRequest ConfigurationApplied condition to TimedOut.
		utils.SetStatusCondition(&CRTask.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ConfigurationApplied,
			provisioningv1alpha1.CRconditionReasons.TimedOut,
			metav1.ConditionFalse,
			"The configuration is still being applied, but it timed out",
		)
		Expect(c.Status().Update(ctx, CRTask.object)).To(Succeed())

		// Create the policies, all Compliant, one in inform and one in enforce.
		createChildPolicy(fmt.Sprintf("ztp-%s.v1-subscriptions-policy", ctNamespace), map[string]string{}, "enforce", "Compliant")
		createChildPolicy(fmt.Sprintf("ztp-%s.v1-sriov-configuration-policy", ctNamespace), map[string]string{}, "inform", "Compliant")

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse()) // there are no NonCompliant policies
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())
		Expect(CRTask.object.Status.Extensions.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-sriov-configuration-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "inform",
				},
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "enforce",
				},
			},
		))

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionTrue))
		Expect(configAppliedCond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.Completed)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is up to date"))
	})

	It("Clears the NonCompliantAt timestamp and timeout when policies are switched to inform", func() {
		// Create inform policies, one Compliant and one NonCompliant.
		createChildPolicy(fmt.Sprintf("ztp-%s.v1-subscriptions-policy", ctNamespace), map[string]string{}, "enforce", "NonCompliant")
		createChildPolicy(fmt.Sprintf("ztp-%s.v1-sriov-configuration-policy", ctNamespace), map[string]string{}, "inform", "Compliant")

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		// There are NonCompliant policies in enforce and the configuration has not timed out,
		// so we need to requeue to re-evaluate the timeout.
		Expect(requeue).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).ToNot(BeZero())
		Expect(CRTask.object.Status.Extensions.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-sriov-configuration-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "inform",
				},
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "enforce",
				},
			},
		))

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.InProgress)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is still being applied"))

		// Take 2 minutes to the NonCompliantAt timestamp to mock timeout.
		CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt.Time =
			CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt.Add(-2 * utils.DefaultClusterConfigurationTimeout)
		Expect(c.Status().Update(ctx, CRTask.object)).To(Succeed())

		// Call the handleClusterPolicyConfiguration function.
		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		// There are NonCompliant policies in enforce, but the configuration has timed out,
		// so we do not need to requeue.
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())

		// Check the status conditions.
		conditions = CRTask.object.Status.Conditions
		configAppliedCond = meta.FindStatusCondition(
			conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.TimedOut)))
		Expect(configAppliedCond.Message).To(
			Equal("The configuration is still being applied, but it timed out"))

		// Check that the NonCompliantAt and timeout are cleared if the policies are in inform.
		// Inform the NonCompliant policy.
		policy := &policiesv1.Policy{}
		policyExists, err := utils.DoesK8SResourceExist(
			ctx, c, "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy", "cluster-1", policy)
		Expect(err).ToNot(HaveOccurred())
		Expect(policyExists).To(BeTrue())
		policy.Spec.RemediationAction = policiesv1.Inform
		Expect(c.Update(ctx, policy)).To(Succeed())
		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse()) // all policies are in inform
		Expect(err).ToNot(HaveOccurred())
		// Check that the NonCompliantAt is zero.
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())

		// Check the status conditions.
		conditions = CRTask.object.Status.Conditions
		configAppliedCond = meta.FindStatusCondition(
			conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.OutOfDate)))
		Expect(configAppliedCond.Message).To(
			Equal("The configuration is out of date"))
	})

	It("It transitions from InProgress to ClusterNotReady to InProgress", func() {
		// Create one policy in the namespace for the clustertemplate and one outside.
		createChildPolicy(fmt.Sprintf("ztp-%s.v1-subscriptions-policy", ctNamespace), map[string]string{}, "enforce", "NonCompliant")
		createChildPolicy("test-ns.test-policy", map[string]string{}, "inform", "NonCompliant")

		// Step 1: Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue()) // we have non compliant enforce policies
		Expect(err).ToNot(HaveOccurred())
		// Only policies created in the namespace for the clustertemplate should be added.
		Expect(len(CRTask.object.Status.Extensions.Policies)).To(Equal(1))
		Expect(CRTask.object.Status.Extensions.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "enforce",
				},
			},
		))
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).ToNot(BeZero())
		initialNonCompliantAt := CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.InProgress)))

		// Step 2: Update the managed cluster to make it not ready.
		managedCluster1 := &clusterv1.ManagedCluster{}
		managedClusterExists, err := utils.DoesK8SResourceExist(ctx, c, "cluster-1", "", managedCluster1)
		Expect(err).ToNot(HaveOccurred())
		Expect(managedClusterExists).To(BeTrue())
		utils.SetStatusCondition(&managedCluster1.Status.Conditions,
			provisioningv1alpha1.ConditionType(clusterv1.ManagedClusterConditionAvailable),
			"ManagedClusterAvailable",
			metav1.ConditionFalse,
			"Managed cluster is not available",
		)
		err = CRReconciler.Client.Status().Update(context.TODO(), managedCluster1)
		Expect(err).ToNot(HaveOccurred())

		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Extensions.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "enforce",
				},
			},
		))
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).ToNot(BeZero())
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).To(Equal(initialNonCompliantAt))

		// Check the status conditions.
		conditions = CRTask.object.Status.Conditions
		configAppliedCond = meta.FindStatusCondition(
			conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.ClusterNotReady)))

		// Step 3: Update the managed cluster to make it ready again.
		utils.SetStatusCondition(&managedCluster1.Status.Conditions,
			provisioningv1alpha1.ConditionType(clusterv1.ManagedClusterConditionAvailable),
			"ManagedClusterAvailable",
			metav1.ConditionTrue,
			"Managed cluster is available",
		)
		err = CRReconciler.Client.Status().Update(context.TODO(), managedCluster1)
		Expect(err).ToNot(HaveOccurred())

		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Extensions.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "enforce",
				},
			},
		))
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).ToNot(BeZero())
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).To(Equal(initialNonCompliantAt))

		// Check the status conditions.
		conditions = CRTask.object.Status.Conditions
		configAppliedCond = meta.FindStatusCondition(
			conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.InProgress)))
	})

	It("It sets ClusterNotReady if the cluster is unstable/not ready", func() {
		// Update the managed cluster to make it not ready.
		managedCluster1 := &clusterv1.ManagedCluster{}
		managedClusterExists, err := utils.DoesK8SResourceExist(
			ctx, c, "cluster-1", "", managedCluster1)
		Expect(err).ToNot(HaveOccurred())
		Expect(managedClusterExists).To(BeTrue())
		utils.SetStatusCondition(&managedCluster1.Status.Conditions,
			provisioningv1alpha1.ConditionType(clusterv1.ManagedClusterConditionAvailable),
			"ManagedClusterAvailable",
			metav1.ConditionFalse,
			"Managed cluster is not available",
		)
		err = CRReconciler.Client.Status().Update(context.TODO(), managedCluster1)
		Expect(err).ToNot(HaveOccurred())

		// Create policies.
		// The cluster is not ready, so there is no compliance status
		createChildPolicy(fmt.Sprintf("ztp-%s.v1-subscriptions-policy", ctNamespace), map[string]string{}, "inform", "")
		policy := &policiesv1.Policy{}
		Expect(c.Get(ctx, client.ObjectKey{
			Name: fmt.Sprintf("ztp-%s.v1-subscriptions-policy", ctNamespace), Namespace: clusterName,
		}, policy)).To(Succeed())
		policy.Status = policiesv1.PolicyStatus{}
		Expect(c.Status().Update(ctx, policy)).To(Succeed())

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Extensions.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "inform",
				},
			},
		))
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.ClusterNotReady)))
	})

	It("Sets the NonCompliantAt timestamp and times out", func() {
		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Extensions.Policies).To(BeEmpty())
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())

		// Create inform policies, one Compliant and one NonCompliant.
		createChildPolicy(fmt.Sprintf("ztp-%s.v1-subscriptions-policy", ctNamespace), map[string]string{}, "inform", "NonCompliant")
		createChildPolicy(fmt.Sprintf("ztp-%s.v1-sriov-configuration-policy", ctNamespace), map[string]string{}, "inform", "Compliant")

		// Call the handleClusterPolicyConfiguration function.
		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
		// NonCompliantAt should still be zero since we don't consider inform policies in the timeout if all policies are inform.
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())
		Expect(CRTask.object.Status.Extensions.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-sriov-configuration-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "inform",
				},
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "inform",
				},
			},
		))

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.OutOfDate)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is out of date"))

		// Enforce the NonCompliant policy.
		policy := &policiesv1.Policy{}
		policyExists, err := utils.DoesK8SResourceExist(
			ctx, c, "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy", "cluster-1", policy)
		Expect(err).ToNot(HaveOccurred())
		Expect(policyExists).To(BeTrue())
		policy.Spec.RemediationAction = policiesv1.Enforce
		Expect(c.Update(ctx, policy)).To(Succeed())

		policyExists, err = utils.DoesK8SResourceExist(
			ctx, c, "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy", "cluster-1", policy)
		Expect(err).ToNot(HaveOccurred())
		Expect(policyExists).To(BeTrue())

		// Call the handleClusterPolicyConfiguration function.
		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		// There are NonCompliant policies in enforce and the configuration has not timed out,
		// so we need to requeue to re-evaluate the timeout.
		Expect(requeue).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).ToNot(BeZero())
		Expect(CRTask.object.Status.Extensions.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-sriov-configuration-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "inform",
				},
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "Enforce",
				},
			},
		))

		// Check the status conditions.
		conditions = CRTask.object.Status.Conditions
		configAppliedCond = meta.FindStatusCondition(
			conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.InProgress)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is still being applied"))

		// Take 2 minutes to the NonCompliantAt timestamp to mock timeout.
		CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt.Time =
			CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt.Add(-2 * utils.DefaultClusterConfigurationTimeout)
		Expect(c.Status().Update(ctx, CRTask.object)).To(Succeed())

		// Call the handleClusterPolicyConfiguration function.
		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		// There are NonCompliant policies in enforce, but the configuration has timed out,
		// so we do not need to requeue.
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())

		// Check the status conditions.
		conditions = CRTask.object.Status.Conditions
		configAppliedCond = meta.FindStatusCondition(
			conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.TimedOut)))
		Expect(configAppliedCond.Message).To(
			Equal("The configuration is still being applied, but it timed out"))

		// Check that another handleClusterPolicyConfiguration call doesn't change the status if
		// the policies are the same.
		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		// There are NonCompliant policies in enforce, but the configuration has timed out,
		// so we do not need to requeue.
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())

		// Check the status conditions.
		conditions = CRTask.object.Status.Conditions
		configAppliedCond = meta.FindStatusCondition(
			conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.TimedOut)))
		Expect(configAppliedCond.Message).To(
			Equal("The configuration is still being applied, but it timed out"))
	})

	It("Sets InProgress when the enforce policy is "+
		"Compliant but the inform policy is still NonCompliant and times out", func() {
		// Create policies, one Compliant enforce policy and one NonCompliant inform policy.
		createChildPolicy(fmt.Sprintf("ztp-%s.v1-validator-policy", ctNamespace), map[string]string{}, "inform", "NonCompliant")
		createChildPolicy(fmt.Sprintf("ztp-%s.v1-sriov-configuration-policy", ctNamespace), map[string]string{}, "enforce", "Compliant")

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		// There are NonCompliant policies in inform and the configuration has not timed out,
		// so we need to requeue to re-evaluate the timeout.
		Expect(requeue).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).ToNot(BeZero())
		Expect(CRTask.object.Status.Extensions.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-sriov-configuration-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "enforce",
				},
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v1-validator-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "inform",
				},
			},
		))

		// Verify that the ConfigurationApplied condition is set to InProgress.
		conditions := CRTask.object.Status.Conditions
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.InProgress)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is still being applied"))

		// Take 2 minutes to the NonCompliantAt timestamp to mock timeout.
		CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt.Time =
			CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt.Add(-2 * utils.DefaultClusterConfigurationTimeout)
		Expect(c.Status().Update(ctx, CRTask.object)).To(Succeed())

		// Call the handleClusterPolicyConfiguration function.
		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		// There are NonCompliant policies in inform, but the configuration has timed out,
		// so we do not need to requeue.
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).ToNot(BeZero())

		// Verify that the ConfigurationApplied condition is set to TimedOut.
		conditions = CRTask.object.Status.Conditions
		configAppliedCond = meta.FindStatusCondition(
			conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.TimedOut)))
		Expect(configAppliedCond.Message).To(
			Equal("The configuration is still being applied, but it timed out"))

		// Check that another handleClusterPolicyConfiguration call doesn't change the status if
		// the policies are the same.
		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		// There are NonCompliant policies in inform, but the configuration has timed out,
		// so we do not need to requeue.
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())

		// Check the status conditions.
		conditions = CRTask.object.Status.Conditions
		configAppliedCond = meta.FindStatusCondition(
			conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.TimedOut)))
		Expect(configAppliedCond.Message).To(
			Equal("The configuration is still being applied, but it timed out"))
	})

	It("Sets Missing if there are no policies", func() {
		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Extensions.Policies).To(BeEmpty())
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionTrue))
		Expect(configAppliedCond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.Missing)))
	})

	It("It handles updated/deleted policies for matched clusters", func() {
		// Expect the ClusterInstance and its namespace to have been created.
		clusterInstanceNs := &corev1.Namespace{}
		err := CRReconciler.Client.Get(
			context.TODO(),
			client.ObjectKey{Name: "cluster-1"},
			clusterInstanceNs,
		)
		Expect(err).ToNot(HaveOccurred())
		clusterInstance := &siteconfig.ClusterInstance{}
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{Name: "cluster-1", Namespace: "cluster-1"},
			clusterInstance)
		Expect(err).ToNot(HaveOccurred())

		// Check updated policies for matched clusters result in reconciliation request.
		policy := &policiesv1.Policy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ztp-clustertemplate-a-v4-16.policy",
				Namespace: "cluster-1",
			},
			Spec: policiesv1.PolicySpec{
				RemediationAction: "enforce",
			},
			Status: policiesv1.PolicyStatus{
				ComplianceState: "Compliant",
			},
		}

		res := CRReconciler.enqueueProvisioningRequestForPolicy(ctx, policy)
		Expect(len(res)).To(Equal(1))

		// Get the first request from the queue.
		Expect(res[0]).To(Equal(reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}))
	})

	It("It does not requeue ProvisioningRequest matched by policies outside the ztp-<clustertemplate-ns> namespace", func() {
		// Expect the ClusterInstance and its namespace to have been created.
		clusterInstanceNs := &corev1.Namespace{}
		err := CRReconciler.Client.Get(
			context.TODO(),
			client.ObjectKey{Name: "cluster-1"},
			clusterInstanceNs,
		)
		Expect(err).ToNot(HaveOccurred())
		clusterInstance := &siteconfig.ClusterInstance{}
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{Name: "cluster-1", Namespace: "cluster-1"},
			clusterInstance)
		Expect(err).ToNot(HaveOccurred())

		// The parent policy of updated child policy is not from ztp-<clustertemplate-ns> ns.
		policy := &policiesv1.Policy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ztp-common.policy",
				Namespace: "cluster-1",
			},
			Spec: policiesv1.PolicySpec{
				RemediationAction: "enforce",
			},
			Status: policiesv1.PolicyStatus{
				ComplianceState: "Compliant",
			},
		}

		// Verify that no request is sent.
		res := CRReconciler.enqueueProvisioningRequestForPolicy(ctx, policy)
		Expect(len(res)).To(Equal(0))
	})

	It("It handles changes to the ClusterTemplate", func() {
		// Get the existing ClusterTemplate since it has a status.
		clusterTemplate := &provisioningv1alpha1.ClusterTemplate{}
		err := CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{
				Name: GetClusterTemplateRefName(tName, tVersion), Namespace: ctNamespace},
			clusterTemplate,
		)
		Expect(err).ToNot(HaveOccurred())

		res := CRReconciler.enqueueProvisioningRequestForClusterTemplate(ctx, clusterTemplate)
		Expect(len(res)).To(Equal(1))
		// Get the first request from the queue.
		Expect(res[0]).To(Equal(reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}))

		// Call enqueueProvisioningRequestForClusterTemplate for a different ClusterTemplate.
		clusterTemplate = &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "random-name",
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:    tName,
				Version: tVersion,
			},
		}
		res = CRReconciler.enqueueProvisioningRequestForClusterTemplate(ctx, clusterTemplate)
		Expect(len(res)).To(Equal(0))
	})

	It("Sets Completed when the cluster is "+
		"Compliant with all the matched policies", func() {
		createChildPolicy(fmt.Sprintf("ztp-%s.v1-subscriptions-policy", ctNamespace), map[string]string{}, "inform", "Compliant")
		createChildPolicy(fmt.Sprintf("ztp-%s.v1-sriov-configuration-policy", ctNamespace), map[string]string{}, "enforce", "Compliant")

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())
		Expect(CRTask.object.Status.Extensions.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-sriov-configuration-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "enforce",
				},
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "inform",
				},
			},
		))

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionTrue))
		Expect(configAppliedCond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.Completed)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is up to date"))
	})

	It("Sets InProgress when the cluster is "+
		"NonCompliant with at least one enforce policy", func() {
		createChildPolicy(fmt.Sprintf("ztp-%s.v1-subscriptions-policy", ctNamespace), map[string]string{}, "inform", "Compliant")
		createChildPolicy(fmt.Sprintf("ztp-%s.v1-sriov-configuration-policy", ctNamespace), map[string]string{}, "enforce", "NonCompliant")

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).ToNot(BeZero())
		Expect(CRTask.object.Status.Extensions.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v1-sriov-configuration-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "enforce",
				},
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "inform",
				},
			},
		))

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.InProgress)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is still being applied"))
	})

	It("Sets InProgress when the cluster is "+
		"Pending with at least one enforce policy", func() {
		createChildPolicy(fmt.Sprintf("ztp-%s.v1-subscriptions-policy", ctNamespace), map[string]string{}, "inform", "Compliant")
		createChildPolicy(fmt.Sprintf("ztp-%s.v1-sriov-configuration-policy", ctNamespace), map[string]string{}, "enforce", "Pending")

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).ToNot(BeZero())
		Expect(CRTask.object.Status.Extensions.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "Pending",
					PolicyName:        "v1-sriov-configuration-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "enforce",
				},
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "inform",
				},
			},
		))

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.InProgress)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is still being applied"))
	})
})

var _ = Describe("hasPolicyConfigurationTimedOut", func() {
	var (
		ctx          context.Context
		c            client.Client
		CRReconciler *ProvisioningRequestReconciler
		CRTask       *provisioningRequestReconcilerTask
		CTReconciler *ClusterTemplateReconciler
		tName        = "clustertemplate-a"
		tVersion     = "v1.0.0"
		ctNamespace  = "clustertemplate-a-v4-16"
	)

	BeforeEach(func() {
		// Initialize context
		ctx = context.Background()

		// Define the needed resources.
		crs := []client.Object{
			// Cluster Template Namespace.
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ctNamespace,
				},
			},
			// Provisioning Request.
			&provisioningv1alpha1.ProvisioningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "cluster-1",
					Finalizers: []string{provisioningv1alpha1.ProvisioningRequestFinalizer},
				},
				Spec: provisioningv1alpha1.ProvisioningRequestSpec{
					TemplateName:    tName,
					TemplateVersion: tVersion,
					TemplateParameters: runtime.RawExtension{
						Raw: []byte(testutils.TestFullTemplateParameters),
					},
				},
				Status: provisioningv1alpha1.ProvisioningRequestStatus{
					Extensions: provisioningv1alpha1.Extensions{
						ClusterDetails: &provisioningv1alpha1.ClusterDetails{},
					},
				},
			},
		}

		c = getFakeClientFromObjects(crs...)
		// Reconcile the ClusterTemplate.
		CTReconciler = &ClusterTemplateReconciler{
			Client: c,
			Logger: logger,
		}

		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      GetClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
		}

		_, err := CTReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())

		CRReconciler = &ProvisioningRequestReconciler{
			Client:         c,
			Logger:         logger,
			CallbackConfig: utils.NewNarCallbackConfig(constants.DefaultNarCallbackServicePort),
		}

		CRTask = &provisioningRequestReconcilerTask{
			logger: CRReconciler.Logger,
			client: CRReconciler.Client,
			object: crs[1].(*provisioningv1alpha1.ProvisioningRequest), // cluster-1 request
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterInstallationTimeout,
				clusterConfiguration: 1 * time.Minute,
			},
		}
	})

	It("Returns false if the status is unexpected and NonCompliantAt is not set", func() {
		// Set the status to InProgress.
		utils.SetStatusCondition(&CRTask.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ConfigurationApplied,
			provisioningv1alpha1.CRconditionReasons.Unknown,
			metav1.ConditionFalse,
			"",
		)
		// Start from empty NonCompliantAt.
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())
		policyTimedOut := CRTask.hasPolicyConfigurationTimedOut(ctx)
		// Check that NonCompliantAt was set and that the return is false.
		Expect(policyTimedOut).To(BeFalse())
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())
	})

	It("Returns false if the status is Completed and sets NonCompliantAt", func() {
		// Set the status to InProgress.
		utils.SetStatusCondition(&CRTask.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ConfigurationApplied,
			provisioningv1alpha1.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"",
		)
		// Start from empty NonCompliantAt.
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())
		policyTimedOut := CRTask.hasPolicyConfigurationTimedOut(ctx)
		// Check that NonCompliantAt was set and that the return is false.
		Expect(policyTimedOut).To(BeFalse())
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).ToNot(BeZero())
	})

	It("Returns false if the status is OutOfDate and sets NonCompliantAt", func() {
		// Set the status to InProgress.
		utils.SetStatusCondition(&CRTask.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ConfigurationApplied,
			provisioningv1alpha1.CRconditionReasons.OutOfDate,
			metav1.ConditionFalse,
			"",
		)
		// Start from empty NonCompliantAt.
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())
		policyTimedOut := CRTask.hasPolicyConfigurationTimedOut(ctx)
		// Check that NonCompliantAt was set and that the return is false.
		Expect(policyTimedOut).To(BeFalse())
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).ToNot(BeZero())
	})

	It("Returns false if the status is Missing and sets NonCompliantAt", func() {
		// Set the status to InProgress.
		utils.SetStatusCondition(&CRTask.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ConfigurationApplied,
			provisioningv1alpha1.CRconditionReasons.Missing,
			metav1.ConditionFalse,
			"",
		)
		// Start from empty NonCompliantAt.
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())
		policyTimedOut := CRTask.hasPolicyConfigurationTimedOut(ctx)
		// Check that NonCompliantAt was set and that the return is false.
		Expect(policyTimedOut).To(BeFalse())
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).ToNot(BeZero())
	})

	It("Returns true if the status is InProgress and the timeout has passed", func() {
		// Set the status to InProgress.
		utils.SetStatusCondition(&CRTask.object.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.ConfigurationApplied,
			provisioningv1alpha1.CRconditionReasons.InProgress,
			metav1.ConditionFalse,
			"",
		)
		// Set NonCompliantAt.
		nonCompliantAt := metav1.Now().Add(-2 * time.Minute)
		CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt = &metav1.Time{Time: nonCompliantAt}
		policyTimedOut := CRTask.hasPolicyConfigurationTimedOut(ctx)
		// Check that NonCompliantAt wasn't changed and that the return is true.
		Expect(policyTimedOut).To(BeTrue())
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt.Time).To(Equal(nonCompliantAt))
	})

	It("Sets NonCompliantAt if there is no ConfigurationApplied condition", func() {
		policyTimedOut := CRTask.hasPolicyConfigurationTimedOut(ctx)
		Expect(policyTimedOut).To(BeFalse())
		Expect(CRTask.object.Status.Extensions.ClusterDetails.NonCompliantAt).ToNot(BeZero())
	})
})

var _ = Describe("addPostProvisioningLabels", func() {
	var (
		c                     client.Client
		ctx                   context.Context
		ctNamespace           = "clustertemplate-a-v4-16"
		ciDefaultsCm          = "clusterinstance-defaults-v1"
		ptDefaultsCm          = "policytemplate-defaults-v1"
		mclName               = "cluster-1"
		AgentName             = "agent-for-cluster-1"
		ProvReqReconciler     *ProvisioningRequestReconciler
		ProvReqTask           *provisioningRequestReconcilerTask
		hwTemplate            = "hwTemplate-v1"
		managedCluster        = &clusterv1.ManagedCluster{}
		nodeAllocationRequest = &pluginsv1alpha1.NodeAllocationRequest{}
	)

	BeforeEach(func() {
		// Initialize context
		ctx = context.Background()

		// Define the needed resources.
		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:       mclName,
				Finalizers: []string{provisioningv1alpha1.ProvisioningRequestFinalizer},
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    tName,
				TemplateVersion: tVersion,
				TemplateParameters: runtime.RawExtension{
					Raw: []byte(testutils.TestFullTemplateParameters),
				},
			},
			Status: provisioningv1alpha1.ProvisioningRequestStatus{
				// Fake the hw provision status
				Conditions: []metav1.Condition{
					{
						Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
						Status: metav1.ConditionTrue,
					},
				},
				Extensions: provisioningv1alpha1.Extensions{
					NodeAllocationRequestRef: &provisioningv1alpha1.NodeAllocationRequestRef{
						NodeAllocationRequestID: "cluster-1", // Use the default ID that exists in mock server
					},
				},
			},
		}

		managedCluster = &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: mclName,
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}

		hwPluginNs := &corev1.Namespace{}
		hwPluginNs.SetName(constants.DefaultNamespace)

		crs := []client.Object{
			// Cluster Template Namespace.
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ctNamespace,
				},
			},
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: constants.DefaultNamespace,
				},
			},
			// ManagedCluster Namespace.
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: mclName,
				},
			},
			// Cluster Template.
			&provisioningv1alpha1.ClusterTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      GetClusterTemplateRefName(tName, tVersion),
					Namespace: ctNamespace,
				},
				Spec: provisioningv1alpha1.ClusterTemplateSpec{
					Name:       tName,
					Version:    tVersion,
					TemplateID: "57b39bda-ac56-4143-9b10-d1a71517d04f",
					Release:    "4.15.0",
					Templates: provisioningv1alpha1.Templates{
						ClusterInstanceDefaults: ciDefaultsCm,
						PolicyTemplateDefaults:  ptDefaultsCm,
						HwTemplate:              hwTemplate,
					},
					TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testutils.TestFullTemplateSchema)},
				},
				Status: provisioningv1alpha1.ClusterTemplateStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(provisioningv1alpha1.CTconditionTypes.Validated),
							Reason: string(provisioningv1alpha1.CTconditionReasons.Completed),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			provisioningRequest,
			// Managed clusters
			managedCluster,
			// ClusterImageSet for the test
			&hivev1.ClusterImageSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "4.15.0",
				},
				Spec: hivev1.ClusterImageSetSpec{
					ReleaseImage: "quay.io/openshift-release-dev/ocp-release:4.15.0-x86_64",
				},
			},
		}

		c = getFakeClientFromObjects(crs...)

		// Populate the NodeAllocationRequest without creating it.
		nodeAllocationRequest = &pluginsv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mclName,
				Namespace: constants.DefaultNamespace,
			},
			Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
				HardwarePluginRef: utils.UnitTestHwPluginRef,
				NodeGroup: []pluginsv1alpha1.NodeGroup{
					{
						NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
							Name:           "controller",
							Role:           "master",
							HwProfile:      "profile-spr-single-processor-64G",
							ResourcePoolId: "xyz",
						},
						Size: 1,
					},
					{
						NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
							Name:           "worker",
							Role:           "worker",
							HwProfile:      "profile-spr-dual-processor-128G",
							ResourcePoolId: "xyz",
						},
						Size: 0,
					},
				},
			},
			Status: pluginsv1alpha1.NodeAllocationRequestStatus{
				Conditions: []metav1.Condition{
					{
						Type:   string(hwmgmtv1alpha1.Provisioned),
						Status: metav1.ConditionTrue,
						Reason: string(hwmgmtv1alpha1.Completed),
					},
				},
				Properties: pluginsv1alpha1.Properties{
					NodeNames: []string{testutils.MasterNodeName},
				},
			},
		}

		// Get the ProvisioningRequest Task.
		ProvReqReconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}

		// Get hwpluginClient for the test
		hwpluginKey := types.NamespacedName{
			Name:      utils.UnitTestHwPluginRef,
			Namespace: constants.DefaultNamespace,
		}
		hwplugin := &hwmgmtv1alpha1.HardwarePlugin{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hwpluginKey.Name,
				Namespace: hwpluginKey.Namespace,
			},
		}
		err := c.Get(ctx, hwpluginKey, hwplugin)
		Expect(err).ToNot(HaveOccurred())

		hwpluginClient, err := hwmgrpluginapi.NewHardwarePluginClient(ctx, c, ProvReqReconciler.Logger, hwplugin)
		Expect(err).ToNot(HaveOccurred())

		ProvReqTask = &provisioningRequestReconcilerTask{
			logger:         ProvReqReconciler.Logger,
			client:         ProvReqReconciler.Client,
			object:         provisioningRequest, // cluster-1 request
			hwpluginClient: hwpluginClient,
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterInstallationTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
			callbackConfig: utils.NewNarCallbackConfig(constants.DefaultNarCallbackServicePort),
		}
	})

	Context("When the HW template is provided and the HW CRs do not exist", func() {
		It("Returns error for the NodeAllocationRequest missing", func() {
			// Set a NodeAllocationRequestID that doesn't exist in the mock server
			ProvReqTask.object.Status.Extensions.NodeAllocationRequestRef = &provisioningv1alpha1.NodeAllocationRequestRef{
				NodeAllocationRequestID: "non-existent-cluster", // Use an ID that doesn't exist in mock server
			}
			// Update the status in the fake client so the change persists
			Expect(c.Status().Update(ctx, ProvReqTask.object)).To(Succeed())

			// Create an Agent so the function proceeds beyond the Agent check
			agent := &assistedservicev1beta1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: mclName,
					Labels: map[string]string{
						"agent-install.openshift.io/clusterdeployment-namespace": mclName,
					},
				},
				Spec: assistedservicev1beta1.AgentSpec{
					Approved: true,
					ClusterDeploymentName: &assistedservicev1beta1.ClusterReference{
						Name:      mclName,
						Namespace: mclName,
					},
				},
			}
			Expect(c.Create(ctx, agent)).To(Succeed())

			// Run the function.
			err := ProvReqTask.addPostProvisioningLabels(ctx, managedCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(
				"empty or unexpected error response for AllocatedNodesFromNodeAllocationRequest 'non-existent-cluster'"))
		})

		It("Returns error for missing Nodes", func() {
			// Set a NodeAllocationRequestID that has no allocated nodes (hardware allocation failed)
			ProvReqTask.object.Status.Extensions.NodeAllocationRequestRef = &provisioningv1alpha1.NodeAllocationRequestRef{
				NodeAllocationRequestID: "empty-cluster", // Use an ID that exists but has no allocated nodes
			}
			// Update the status in the fake client so the change persists
			Expect(c.Status().Update(ctx, ProvReqTask.object)).To(Succeed())

			// Create the NodeAllocationRequest, but not the nodes.
			Expect(c.Create(ctx, nodeAllocationRequest)).To(Succeed())

			// Don't create any Agents - if hardware allocation failed, no physical machines
			// would be available, so no Agents would be discovered/created

			// Run the function - it should return an error for missing Agents
			// (which is what happens when hardware allocation fails)
			err := ProvReqTask.addPostProvisioningLabels(ctx, managedCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(
				fmt.Sprintf("the expected Agents were not found in the %s namespace", managedCluster.Name)))
		})
	})

	Context("When the HW template is provided and the expected HW CRs exist", func() {
		BeforeEach(func() {
			hwPluginNs := &corev1.Namespace{}
			hwPluginNs.SetName(constants.DefaultNamespace)

			// Create the NodeAllocationRequest.
			Expect(c.Create(ctx, nodeAllocationRequest)).To(Succeed())
			testutils.CreateNodeResources(ctx, c, nodeAllocationRequest.Name)
		})

		It("Updates Agent and ManagedCluster labels as expected", func() {
			// Create an Agent CR with the expected label.
			agent := &assistedservicev1beta1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      AgentName,
					Namespace: mclName,
					Labels: map[string]string{
						"agent-install.openshift.io/clusterdeployment-namespace": mclName,
					},
				},
				Spec: assistedservicev1beta1.AgentSpec{
					Approved: true,
					ClusterDeploymentName: &assistedservicev1beta1.ClusterReference{
						Name:      mclName,
						Namespace: mclName,
					},
				},
			}
			Expect(ProvReqTask.client.Create(ctx, agent)).To(Succeed())
			// Run the function.
			err := ProvReqTask.addPostProvisioningLabels(ctx, managedCluster)
			Expect(err).ToNot(HaveOccurred())

			// Check that the new label was added for the ManagedCluster CR.
			mclUpdated := &clusterv1.ManagedCluster{}
			err = ProvReqTask.client.Get(ctx, types.NamespacedName{Name: mclName}, mclUpdated)
			Expect(err).ToNot(HaveOccurred())
			Expect(mclUpdated.GetLabels()).To(Equal(map[string]string{
				utils.ClusterTemplateArtifactsLabel: "57b39bda-ac56-4143-9b10-d1a71517d04f",
			}))

			// Check that the new label was added and the old label was kept for the Agent CR.
			err = ProvReqTask.client.Get(ctx, types.NamespacedName{Name: AgentName, Namespace: mclName}, agent)
			Expect(err).ToNot(HaveOccurred())
			Expect(agent.GetLabels()).To(Equal(map[string]string{
				utils.ClusterTemplateArtifactsLabel:                      "57b39bda-ac56-4143-9b10-d1a71517d04f",
				"agent-install.openshift.io/clusterdeployment-namespace": mclName,
			}))
		})

		It("Fails to get a ClusterTemplate", func() {
			// Update the ClusterTemplate to be invalid.
			oranct := &provisioningv1alpha1.ClusterTemplate{}

			err := ProvReqTask.client.Get(
				ctx,
				types.NamespacedName{Name: GetClusterTemplateRefName(tName, tVersion), Namespace: ctNamespace},
				oranct,
			)
			Expect(err).ToNot(HaveOccurred())
			oranct.Status.Conditions[0].Status = "False"
			Expect(ProvReqTask.client.Status().Update(ctx, oranct)).To(Succeed())
			Expect(err).ToNot(HaveOccurred())

			err = ProvReqTask.addPostProvisioningLabels(ctx, managedCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(
				"failed to get ClusterTemplate: a valid ClusterTemplate (%s) does not exist in any namespace",
				fmt.Sprintf("%s.%s", tName, tVersion)))
		})

		It("Sets the label for MNO when there are multiple Agents", func() {
			// Set up AllocatedNodeHostMap for this test
			ProvReqTask.object.Status.Extensions.AllocatedNodeHostMap = map[string]string{
				"test-node-1": "some-other-cluster.lab.example.com", // Map test-node-1 to agent2's hostname
			}
			// Update the status in the fake client so the change persists
			Expect(c.Status().Update(ctx, ProvReqTask.object)).To(Succeed())

			// Create 2 Agents in the expected namespace
			agent2Name := "agent-2-for-cluster-1"
			agent2Hostname := "some-other-cluster.lab.example.com"
			agent := &assistedservicev1beta1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      AgentName,
					Namespace: mclName,
					Labels: map[string]string{
						"agent-install.openshift.io/clusterdeployment-namespace": mclName,
					},
				},
				Spec: assistedservicev1beta1.AgentSpec{
					Approved: true,
					ClusterDeploymentName: &assistedservicev1beta1.ClusterReference{
						Name:      mclName,
						Namespace: mclName,
					},
					Hostname: fmt.Sprintf("%s.lab.example.com", mclName),
				},
			}
			agent2 := agent.DeepCopy()
			agent2.Name = agent2Name
			agent2.Spec.Hostname = agent2Hostname
			Expect(ProvReqTask.client.Create(ctx, agent)).To(Succeed())
			Expect(ProvReqTask.client.Create(ctx, agent2)).To(Succeed())

			// Create the corresponding Node for the 2nd Agent only.
			masterNodeName2 := "master-node-2"
			// #nosec G101
			bmcSecretName2 := "bmc-secret-2"
			node := testutils.CreateNode(
				masterNodeName2, "idrac-virtualmedia+https://10.16.2.1/redfish/v1/Systems/System.Embedded.1",
				"bmc-secret", "controller", constants.DefaultNamespace, mclName, nil)
			node.Status.Hostname = agent2Hostname
			secrets := testutils.CreateSecrets([]string{bmcSecretName2}, constants.DefaultNamespace)
			testutils.CreateResources(ctx, c, []*pluginsv1alpha1.AllocatedNode{node}, secrets)

			// Create the corresponding BareMetalHost that the function will look for
			bmhUID := "f47ac10b-58cc-4372-a567-0e02b2c3d479" // Fixed UUID for testing
			bmh := &metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      masterNodeName2,
					Namespace: constants.DefaultNamespace,
					UID:       types.UID(bmhUID),
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					BMC: metal3v1alpha1.BMCDetails{
						Address:         "idrac-virtualmedia+https://10.16.2.1/redfish/v1/Systems/System.Embedded.1",
						CredentialsName: bmcSecretName2,
					},
				},
				Status: metal3v1alpha1.BareMetalHostStatus{
					HardwareDetails: &metal3v1alpha1.HardwareDetails{
						Hostname: agent2Hostname, // This is what the function looks for
					},
				},
			}
			Expect(c.Create(ctx, bmh)).To(Succeed())

			// Run the function.
			err := ProvReqTask.addPostProvisioningLabels(ctx, managedCluster)
			Expect(err).To(Not(HaveOccurred()))
			// Check that both agents have the expected labels.
			listOpts := []client.ListOption{
				client.MatchingLabels{
					"agent-install.openshift.io/clusterdeployment-namespace": managedCluster.Name,
				},
				client.InNamespace(managedCluster.Name),
			}
			agents := &assistedservicev1beta1.AgentList{}
			err = ProvReqTask.client.List(ctx, agents, listOpts...)
			Expect(err).To(Not(HaveOccurred()))
			Expect(len(agents.Items)).To(Equal(2))
			checkedAgents := 0
			for _, agent := range agents.Items {
				if agent.Name == agent2Name {
					checkedAgents += 1
					Expect(agent.Labels).To(Equal(map[string]string{
						utils.ClusterTemplateArtifactsLabel:                      "57b39bda-ac56-4143-9b10-d1a71517d04f",
						"agent-install.openshift.io/clusterdeployment-namespace": mclName,
						"clcm.openshift.io/hardwarePluginRef":                    utils.UnitTestHwPluginRef,
						"clcm.openshift.io/hwMgrNodeId":                          bmhUID,
					}))
				}
				if agent.Name == AgentName {
					checkedAgents += 1
					Expect(agents.Items[1].Labels).To(Equal(map[string]string{
						utils.ClusterTemplateArtifactsLabel:                      "57b39bda-ac56-4143-9b10-d1a71517d04f",
						"agent-install.openshift.io/clusterdeployment-namespace": mclName,
					}))
				}
			}
			Expect(checkedAgents).To(Equal(len(agents.Items)))
		})

		It("Fails for multiple Agents with unexpected labels", func() {
			// Create 2 Agents in the expected namespace
			agent := &assistedservicev1beta1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      AgentName,
					Namespace: mclName,
					Labels: map[string]string{
						"agent-install.openshift.io/clusterdeployment-namespace": "some-other-cluster",
					},
				},
				Spec: assistedservicev1beta1.AgentSpec{
					Approved: true,
					ClusterDeploymentName: &assistedservicev1beta1.ClusterReference{
						Name:      mclName,
						Namespace: mclName,
					},
					Hostname: "some-other-cluster.lab.example.com",
				},
			}
			Expect(ProvReqTask.client.Create(ctx, agent)).To(Succeed())

			// Create the corresponding Node.
			masterNodeName2 := "master-node-2"
			// #nosec G101
			bmcSecretName2 := "bmc-secret-2"
			node := testutils.CreateNode(
				masterNodeName2, "idrac-virtualmedia+https://10.16.2.1/redfish/v1/Systems/System.Embedded.1",
				"bmc-secret", "controller", constants.DefaultNamespace, mclName, nil)
			node.Status.Hostname = "some-other-cluster.lab.example.com"
			secrets := testutils.CreateSecrets([]string{bmcSecretName2}, constants.DefaultNamespace)
			testutils.CreateResources(ctx, c, []*pluginsv1alpha1.AllocatedNode{node}, secrets)

			// Run the function.
			err := ProvReqTask.addPostProvisioningLabels(ctx, managedCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(
				fmt.Sprintf("the expected Agents were not found in the %s namespace", mclName)))
		})
	})

	Context("When the HW template is not provided", func() {
		BeforeEach(func() {
			// Remove the HW template from the ClusterTemplate.
			ct := &provisioningv1alpha1.ClusterTemplate{}
			Expect(c.Get(ctx, types.NamespacedName{
				Name:      GetClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			}, ct)).To(Succeed())
			ct.Spec.Templates.HwTemplate = ""
			Expect(c.Update(ctx, ct)).To(Succeed())
		})

		It("Does not add hardwarePluginRef and hwMgrNodeId labels to the Agents", func() {
			// Create an Agent CR with the expected label.
			agent := &assistedservicev1beta1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      AgentName,
					Namespace: mclName,
					Labels: map[string]string{
						"agent-install.openshift.io/clusterdeployment-namespace": mclName,
					},
				},
				Spec: assistedservicev1beta1.AgentSpec{
					Approved: true,
					ClusterDeploymentName: &assistedservicev1beta1.ClusterReference{
						Name:      mclName,
						Namespace: mclName,
					},
					Hostname: fmt.Sprintf("%s.lab.example.com", mclName),
				},
			}
			Expect(ProvReqTask.client.Create(ctx, agent)).To(Succeed())

			// Run the function.
			err := ProvReqTask.addPostProvisioningLabels(ctx, managedCluster)
			Expect(err).ToNot(HaveOccurred())

			// Check that the new label was added for the ManagedCluster CR.
			mclUpdated := &clusterv1.ManagedCluster{}
			err = ProvReqTask.client.Get(ctx, types.NamespacedName{Name: mclName}, mclUpdated)
			Expect(err).ToNot(HaveOccurred())
			Expect(mclUpdated.GetLabels()).To(Equal(map[string]string{
				utils.ClusterTemplateArtifactsLabel: "57b39bda-ac56-4143-9b10-d1a71517d04f",
			}))

			// Check that the templateArtifacts label is present and hardwarePluginRef and hwMgrNodeId labels are not present.
			err = ProvReqTask.client.Get(ctx, types.NamespacedName{Name: AgentName, Namespace: mclName}, agent)
			Expect(err).ToNot(HaveOccurred())
			Expect(agent.GetLabels()).To(Equal(map[string]string{
				utils.ClusterTemplateArtifactsLabel:                      "57b39bda-ac56-4143-9b10-d1a71517d04f",
				"agent-install.openshift.io/clusterdeployment-namespace": mclName,
			}))
			Expect(agent.Labels).To(Not(HaveKey("clcm.openshift.io/hardwarePluginRef")))
			Expect(agent.Labels).To(Not(HaveKey("clcm.openshift.io/hwMgrNodeId")))
		})
	})

})
