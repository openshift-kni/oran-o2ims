package controllers

import (
	"context"
	"reflect"
	"testing"
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

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
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
		hwTemplateCm = "hwTemplate-v1"
	)

	BeforeEach(func() {
		// Define the needed resources.
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
					Name:      getClusterTemplateRefName(tName, tVersion),
					Namespace: ctNamespace,
				},
				Spec: provisioningv1alpha1.ClusterTemplateSpec{
					Name:       tName,
					Version:    tVersion,
					TemplateID: "57b39bda-ac56-4143-9b10-d1a71517d04f",
					Templates: provisioningv1alpha1.Templates{
						ClusterInstanceDefaults: ciDefaultsCm,
						PolicyTemplateDefaults:  ptDefaultsCm,
						HwTemplate:              hwTemplateCm,
					},
					TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testFullTemplateSchema)},
				},
				Status: provisioningv1alpha1.ClusterTemplateStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(utils.CTconditionTypes.Validated),
							Reason: string(utils.CTconditionReasons.Completed),
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
clusterImageSetNameRef: "4.15"
pullSecretRef:
  name: "pull-secret"
templateRefs:
- name: "ai-cluster-templates-v1"
  namespace: "siteconfig-operator"
nodes:
- hostname: "node1"
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
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      hwTemplateCm,
					Namespace: utils.InventoryNamespace,
				},
				Data: map[string]string{
					"hwMgrId":                      utils.UnitTestHwmgrID,
					utils.HwTemplateBootIfaceLabel: "bootable-interface",
					utils.HwTemplateNodePool: `
- name: master
  hwProfile: profile-spr-single-processor-64G
- name: worker
  hwProfile: profile-spr-dual-processor-128G`,
				},
			},
			// Pull secret.
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pull-secret",
					Namespace: ctNamespace,
				},
			},
			// Provisioning Requests.
			&provisioningv1alpha1.ProvisioningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "cluster-1",
					Finalizers: []string{provisioningRequestFinalizer},
				},
				Spec: provisioningv1alpha1.ProvisioningRequestSpec{
					TemplateName:    tName,
					TemplateVersion: tVersion,
					TemplateParameters: runtime.RawExtension{
						Raw: []byte(testFullTemplateParameters),
					},
				},
				Status: provisioningv1alpha1.ProvisioningRequestStatus{
					// Fake the hw provision status
					Conditions: []metav1.Condition{
						{
							Type:   string(utils.PRconditionTypes.HardwareProvisioned),
							Status: metav1.ConditionTrue,
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
		}

		c = getFakeClientFromObjects(crs...)
		// Reconcile the ClusterTemplate.
		CTReconciler = &ClusterTemplateReconciler{
			Client: c,
			Logger: logger,
		}

		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      getClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
		}

		_, err := CTReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())

		CRReconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}

		// Update the managedCluster cluster-1 to be available, joined and accepted.
		managedCluster1 := &clusterv1.ManagedCluster{}
		managedClusterExists, err := utils.DoesK8SResourceExist(
			ctx, c, "cluster-1", "", managedCluster1)
		Expect(err).ToNot(HaveOccurred())
		Expect(managedClusterExists).To(BeTrue())
		utils.SetStatusCondition(&managedCluster1.Status.Conditions,
			utils.ConditionType(clusterv1.ManagedClusterConditionAvailable),
			"ManagedClusterAvailable",
			metav1.ConditionTrue,
			"Managed cluster is available",
		)
		utils.SetStatusCondition(&managedCluster1.Status.Conditions,
			utils.ConditionType(clusterv1.ManagedClusterConditionHubAccepted),
			"HubClusterAdminAccepted",
			metav1.ConditionTrue,
			"Accepted by hub cluster admin",
		)
		utils.SetStatusCondition(&managedCluster1.Status.Conditions,
			utils.ConditionType(clusterv1.ManagedClusterConditionJoined),
			"ManagedClusterJoined",
			metav1.ConditionTrue,
			"Managed cluster joined",
		)
		err = CRReconciler.Client.Status().Update(context.TODO(), managedCluster1)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Does not handle the policy configuration without the cluster provisioning having started", func() {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())

		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{
				Name: "cluster-1",
			},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger: CRReconciler.Logger,
			client: CRReconciler.Client,
			object: provisioningRequest, // cluster-1 request
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}

		// Create the policies, all Compliant, one in inform and one in enforce.
		newPolicies := []client.Object{
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "enforce",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "NonCompliant",
					Details: []*policiesv1.DetailsPerTemplate{
						{
							History: []policiesv1.ComplianceHistory{
								{
									LastTimestamp: metav1.Now(),
									Message:       "NonCompliant;",
								},
							},
						},
					},
				},
			},
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
					Details: []*policiesv1.DetailsPerTemplate{
						{
							History: []policiesv1.ComplianceHistory{
								{
									LastTimestamp: metav1.Now(),
									Message:       "Compliant;",
								},
							},
						},
					},
				},
			},
		}
		for _, newPolicy := range newPolicies {
			Expect(c.Create(ctx, newPolicy)).To(Succeed())
		}

		// Call the Reconciliation function.
		result, err = CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())
		Expect(CRTask.object.Status.Policies).To(BeEmpty())

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).To(BeNil())

		// Add the ClusterProvisioned condition.
		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{Name: "cluster-1"},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger: CRReconciler.Logger,
			client: CRReconciler.Client,
			object: provisioningRequest, // cluster-1 request
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}

		utils.SetStatusCondition(&CRTask.object.Status.Conditions,
			utils.PRconditionTypes.ClusterProvisioned,
			utils.CRconditionReasons.InProgress,
			metav1.ConditionFalse,
			"",
		)
		CRTask.object.Status.ClusterDetails.ClusterProvisionStartedAt = metav1.Now()
		Expect(c.Status().Update(ctx, CRTask.object)).To(Succeed())

		// Call the Reconciliation function.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{Name: "cluster-1"},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger:       CRReconciler.Logger,
			client:       CRReconciler.Client,
			object:       provisioningRequest, // cluster-1 request
			clusterInput: &clusterInput{},
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}
		result, err = CRTask.run(ctx)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())
		Expect(result.RequeueAfter).To(Equal(5 * time.Minute)) // Long interval
		Expect(CRTask.object.Status.Policies).ToNot(BeEmpty())
	})

	It("Moves from TimedOut to Completed if all the policies are compliant", func() {
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: "cluster-1",
			},
		}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())

		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{Name: "cluster-1"},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger: CRReconciler.Logger,
			client: CRReconciler.Client,
			object: provisioningRequest, // cluster-1 request
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}

		// Update the ProvisioningRequest ConfigurationApplied condition to TimedOut.
		utils.SetStatusCondition(&CRTask.object.Status.Conditions,
			utils.PRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.TimedOut,
			metav1.ConditionFalse,
			"The configuration is still being applied, but it timed out",
		)
		Expect(c.Status().Update(ctx, CRTask.object)).To(Succeed())

		// Create the policies, all Compliant, one in inform and one in enforce.
		newPolicies := []client.Object{
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "enforce",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
				},
			},
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
					Details: []*policiesv1.DetailsPerTemplate{
						{
							History: []policiesv1.ComplianceHistory{
								{
									LastTimestamp: metav1.Now(),
									Message:       "Compliant;",
								},
							},
						},
					},
				},
			},
		}
		for _, newPolicy := range newPolicies {
			Expect(c.Create(ctx, newPolicy)).To(Succeed())
		}

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse()) // there are no NonCompliant policies
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
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
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionTrue))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.Completed)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is up to date"))
	})

	It("Timeout when policies are switched to inform", func() {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())

		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{Name: "cluster-1"},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger: CRReconciler.Logger,
			client: CRReconciler.Client,
			object: provisioningRequest, // cluster-1 request
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: 60 * time.Second,
			},
		}

		// Create inform policies, one Compliant and one NonCompliant.
		newPolicies := []*policiesv1.Policy{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "enforce",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "NonCompliant",
					Details: []*policiesv1.DetailsPerTemplate{
						{
							History: []policiesv1.ComplianceHistory{
								{
									LastTimestamp: metav1.Now(),
									Message:       "NonCompliant;",
								},
							},
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
					Details: []*policiesv1.DetailsPerTemplate{
						{
							History: []policiesv1.ComplianceHistory{
								{
									LastTimestamp: metav1.Now(),
									Message:       "Compliant;",
								},
							},
						},
					},
				},
			},
		}
		for _, newPolicy := range newPolicies {
			Expect(c.Create(ctx, newPolicy)).To(Succeed())
		}

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue()) // there are NonCompliant policies in enforce
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
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
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.InProgress)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is still being applied"))

		// Take 2 minutes to the Policy history timestamp to mock timeout.
		newPolicies[0].Status.Details[0].History[0].LastTimestamp = metav1.NewTime(metav1.Now().Add(-2 * time.Minute))
		Expect(c.Status().Update(ctx, newPolicies[0])).To(Succeed())
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
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.TimedOut)))
		Expect(configAppliedCond.Message).To(
			Equal("The configuration is still being applied, but it timed out"))

		// Check that timeout is cleared if the policies are in inform.
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

		// Check the status conditions.
		conditions = CRTask.object.Status.Conditions
		configAppliedCond = meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.OutOfDate)))
		Expect(configAppliedCond.Message).To(
			Equal("The configuration is out of date"))
	})

	It("It transitions from InProgress to ClusterNotReady to InProgress", func() {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())

		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{Name: "cluster-1"},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger: CRReconciler.Logger,
			client: CRReconciler.Client,
			object: provisioningRequest, // cluster-1 request
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}
		// Create policies.
		newPolicies := []client.Object{
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "enforce",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "NonCompliant",
					Details: []*policiesv1.DetailsPerTemplate{
						{
							History: []policiesv1.ComplianceHistory{
								{
									LastTimestamp: metav1.Now(),
									Message:       "NonCompliant;",
								},
							},
						},
					},
				},
			},
		}
		for _, newPolicy := range newPolicies {
			Expect(c.Create(ctx, newPolicy)).To(Succeed())
		}

		// Step 1: Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue()) // we have non compliant enforce policies
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
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
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.InProgress)))

		// Step 2: Update the managed cluster to make it not ready.
		managedCluster1 := &clusterv1.ManagedCluster{}
		managedClusterExists, err := utils.DoesK8SResourceExist(ctx, c, "cluster-1", "", managedCluster1)
		Expect(err).ToNot(HaveOccurred())
		Expect(managedClusterExists).To(BeTrue())
		utils.SetStatusCondition(&managedCluster1.Status.Conditions,
			utils.ConditionType(clusterv1.ManagedClusterConditionAvailable),
			"ManagedClusterAvailable",
			metav1.ConditionFalse,
			"Managed cluster is not available",
		)
		err = CRReconciler.Client.Status().Update(context.TODO(), managedCluster1)
		Expect(err).ToNot(HaveOccurred())

		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "enforce",
				},
			},
		))

		// Check the status conditions.
		conditions = CRTask.object.Status.Conditions
		configAppliedCond = meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.ClusterNotReady)))

		// Step 3: Update the managed cluster to make it ready again.
		utils.SetStatusCondition(&managedCluster1.Status.Conditions,
			utils.ConditionType(clusterv1.ManagedClusterConditionAvailable),
			"ManagedClusterAvailable",
			metav1.ConditionTrue,
			"Managed cluster is available",
		)
		err = CRReconciler.Client.Status().Update(context.TODO(), managedCluster1)
		Expect(err).ToNot(HaveOccurred())

		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "enforce",
				},
			},
		))

		// Check the status conditions.
		conditions = CRTask.object.Status.Conditions
		configAppliedCond = meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.InProgress)))
	})

	It("It sets ClusterNotReady if the cluster is unstable/not ready", func() {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())

		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{Name: "cluster-1"},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger: CRReconciler.Logger,
			client: CRReconciler.Client,
			object: provisioningRequest, // cluster-1 request
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}
		// Update the managed cluster to make it not ready.
		managedCluster1 := &clusterv1.ManagedCluster{}
		managedClusterExists, err := utils.DoesK8SResourceExist(
			ctx, c, "cluster-1", "", managedCluster1)
		Expect(err).ToNot(HaveOccurred())
		Expect(managedClusterExists).To(BeTrue())
		utils.SetStatusCondition(&managedCluster1.Status.Conditions,
			utils.ConditionType(clusterv1.ManagedClusterConditionAvailable),
			"ManagedClusterAvailable",
			metav1.ConditionFalse,
			"Managed cluster is not available",
		)
		err = CRReconciler.Client.Status().Update(context.TODO(), managedCluster1)
		Expect(err).ToNot(HaveOccurred())

		// Create policies.
		// The cluster is not ready, so there is no compliance status
		newPolicies := []client.Object{
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
			},
		}
		for _, newPolicy := range newPolicies {
			Expect(c.Create(ctx, newPolicy)).To(Succeed())
		}

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "",
					PolicyName:        "v1-subscriptions-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "inform",
				},
			},
		))

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.ClusterNotReady)))
	})

	It("Sets the policies timestamp and times out", func() {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())

		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{
				Name: "cluster-1",
			},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger:      CRReconciler.Logger,
			client:      CRReconciler.Client,
			object:      provisioningRequest, // cluster-1 request
			ctNamespace: ctNamespace,
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: 1 * time.Minute,
			},
		}

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(BeEmpty())

		// Create inform policies, one Compliant and one NonCompliant.
		newPolicies := []client.Object{
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "NonCompliant",
					Details: []*policiesv1.DetailsPerTemplate{
						{
							History: []policiesv1.ComplianceHistory{
								{
									LastTimestamp: metav1.Now(),
									Message:       "NonCompliant;",
								},
							},
						},
					},
				},
			},
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
					Details: []*policiesv1.DetailsPerTemplate{
						{
							History: []policiesv1.ComplianceHistory{
								{
									LastTimestamp: metav1.Now(),
									Message:       "Compliant;",
								},
							},
						},
					},
				},
			},
		}
		for _, newPolicy := range newPolicies {
			Expect(c.Create(ctx, newPolicy)).To(Succeed())
		}

		// Call the handleClusterPolicyConfiguration function.
		requeue, err = CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
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
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.OutOfDate)))
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
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
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
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.InProgress)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is still being applied"))

		// Take 2 minutes to the policy timestamp to mock timeout.
		policy.Status.Details[0].History[0].LastTimestamp = metav1.NewTime(metav1.Now().Add(-2 * time.Minute))
		Expect(c.Status().Update(ctx, policy)).To(Succeed())
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
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.TimedOut)))
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
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.TimedOut)))
		Expect(configAppliedCond.Message).To(
			Equal("The configuration is still being applied, but it timed out"))
	})

	It("Updates ProvisioningRequest ConfigurationApplied condition to Missing if there are no policies", func() {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())

		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{
				Name: "cluster-1",
			},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger:      CRReconciler.Logger,
			client:      CRReconciler.Client,
			object:      provisioningRequest, // cluster-1 request
			ctNamespace: ctNamespace,
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(BeEmpty())

		// Check the status conditions.
		conditions := CRTask.object.Status.Conditions
		configAppliedCond := meta.FindStatusCondition(
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.Missing)))
	})

	It("It handles updated/deleted policies for matched clusters", func() {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())
		// Expect the ClusterInstance and its namespace to have been created.
		clusterInstanceNs := &corev1.Namespace{}
		err = CRReconciler.Client.Get(
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
				Details: []*policiesv1.DetailsPerTemplate{
					{
						History: []policiesv1.ComplianceHistory{
							{
								LastTimestamp: metav1.Now(),
								Message:       "Compliant;",
							},
						},
					},
				},
			},
		}

		res := CRReconciler.enqueueProvisioningRequestForPolicy(ctx, policy)
		Expect(len(res)).To(Equal(1))

		// Get the first request from the queue.
		Expect(res[0]).To(Equal(reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}))
	})

	It("It handles changes to the ClusterTemplate", func() {
		// Get the existing ClusterTemplate since it has a status.
		clusterTemplate := &provisioningv1alpha1.ClusterTemplate{}
		err := CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{
				Name: getClusterTemplateRefName(tName, tVersion), Namespace: ctNamespace},
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

	It("Updates ProvisioningRequest ConfigurationApplied condition to OutOfDate when the cluster is "+
		"NonCompliant with at least one matched policies and the policy is not in enforce", func() {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())

		newPolicies := []client.Object{
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "NonCompliant",
					Details: []*policiesv1.DetailsPerTemplate{
						{
							History: []policiesv1.ComplianceHistory{
								{
									LastTimestamp: metav1.Now(),
									Message:       "NonCompliant;",
								},
							},
						},
					},
				},
			},
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "enforce",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
					Details: []*policiesv1.DetailsPerTemplate{
						{
							History: []policiesv1.ComplianceHistory{
								{
									LastTimestamp: metav1.Now(),
									Message:       "Compliant;",
								},
							},
						},
					},
				},
			},
		}
		// Create all the ACM policies.
		for _, newPolicy := range newPolicies {
			Expect(c.Create(ctx, newPolicy)).To(Succeed())
		}
		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{
				Name: "cluster-1",
			},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger:       CRReconciler.Logger,
			client:       CRReconciler.Client,
			object:       provisioningRequest, // cluster-1 request
			clusterInput: &clusterInput{},
			ctNamespace:  ctNamespace,
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
			[]provisioningv1alpha1.PolicyDetails{
				{
					Compliant:         "Compliant",
					PolicyName:        "v1-sriov-configuration-policy",
					PolicyNamespace:   "ztp-clustertemplate-a-v4-16",
					RemediationAction: "enforce",
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
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.OutOfDate)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is out of date"))
	})

	It("Updates ProvisioningRequest ConfigurationApplied condition to Completed when the cluster is "+
		"Compliant with all the matched policies", func() {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())

		newPolicies := []client.Object{
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
					Details: []*policiesv1.DetailsPerTemplate{
						{
							History: []policiesv1.ComplianceHistory{
								{
									LastTimestamp: metav1.Now(),
									Message:       "Compliant;",
								},
							},
						},
					},
				},
			},
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "enforce",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
					Details: []*policiesv1.DetailsPerTemplate{
						{
							History: []policiesv1.ComplianceHistory{
								{
									LastTimestamp: metav1.Now(),
									Message:       "Compliant;",
								},
							},
						},
					},
				},
			},
		}
		// Create all the ACM policies.
		for _, newPolicy := range newPolicies {
			Expect(c.Create(ctx, newPolicy)).To(Succeed())
		}
		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{
				Name: "cluster-1",
			},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger:       CRReconciler.Logger,
			client:       CRReconciler.Client,
			object:       provisioningRequest, // cluster-1 request
			clusterInput: &clusterInput{},
			ctNamespace:  ctNamespace,
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
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
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionTrue))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.Completed)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is up to date"))
	})

	It("Updates ProvisioningRequest ConfigurationApplied condition to InProgress when the cluster is "+
		"NonCompliant with at least one enforce policy", func() {
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-1"}}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())

		newPolicies := []client.Object{
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
					Details: []*policiesv1.DetailsPerTemplate{
						{
							History: []policiesv1.ComplianceHistory{
								{
									LastTimestamp: metav1.Now(),
									Message:       "Compliant;",
								},
							},
						},
					},
				},
			},
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "enforce",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "NonCompliant",
					Details: []*policiesv1.DetailsPerTemplate{
						{
							History: []policiesv1.ComplianceHistory{
								{
									LastTimestamp: metav1.Now(),
									Message:       "NonCompliant;",
								},
							},
						},
					},
				},
			},
		}
		// Create all the ACM policies.
		for _, newPolicy := range newPolicies {
			Expect(c.Create(ctx, newPolicy)).To(Succeed())
		}
		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		err = CRReconciler.Client.Get(
			context.TODO(),
			types.NamespacedName{
				Name: "cluster-1",
			},
			provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger:      CRReconciler.Logger,
			client:      CRReconciler.Client,
			object:      provisioningRequest, // cluster-1 request
			ctNamespace: ctNamespace,
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
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
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.InProgress)))
		Expect(configAppliedCond.Message).To(Equal("The configuration is still being applied"))
	})

	It("Updates ProvisioningRequest ConfigurationApplied condition to InProgress when the cluster is "+
		"Pending with at least one enforce policy", func() {
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: "cluster-1",
			},
		}

		result, err := CRReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid provisioning request.
		Expect(result.Requeue).To(BeFalse())

		newPolicies := []client.Object{
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-subscriptions-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
					Details: []*policiesv1.DetailsPerTemplate{
						{
							History: []policiesv1.ComplianceHistory{
								{
									LastTimestamp: metav1.Now(),
									Message:       "Compliant;",
								},
							},
						},
					},
				},
			},
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
					Namespace: "cluster-1",
					Labels: map[string]string{
						utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-16.v1-sriov-configuration-policy",
						utils.ChildPolicyClusterNameLabel:      "cluster-1",
						utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
					},
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "enforce",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Pending",
					Details: []*policiesv1.DetailsPerTemplate{
						{
							History: []policiesv1.ComplianceHistory{
								{
									LastTimestamp: metav1.Now(),
									Message:       "Pending;",
								},
							},
						},
					},
				},
			},
		}
		// Create all the ACM policies.
		for _, newPolicy := range newPolicies {
			Expect(c.Create(ctx, newPolicy)).To(Succeed())
		}
		provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}

		// Create the ProvisioningRequest reconciliation task.
		namespacedName := types.NamespacedName{Name: "cluster-1"}
		err = c.Get(context.TODO(), namespacedName, provisioningRequest)
		Expect(err).ToNot(HaveOccurred())

		CRTask = &provisioningRequestReconcilerTask{
			logger:      CRReconciler.Logger,
			client:      CRReconciler.Client,
			object:      provisioningRequest, // cluster-1 request
			ctNamespace: ctNamespace,
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: utils.DefaultClusterConfigurationTimeout,
			},
		}

		// Call the handleClusterPolicyConfiguration function.
		requeue, err := CRTask.handleClusterPolicyConfiguration(context.Background())
		Expect(requeue).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
		Expect(CRTask.object.Status.Policies).To(ConsistOf(
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
			conditions, string(utils.PRconditionTypes.ConfigurationApplied))
		Expect(configAppliedCond).ToNot(BeNil())
		Expect(configAppliedCond.Type).To(Equal(string(utils.PRconditionTypes.ConfigurationApplied)))
		Expect(configAppliedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(configAppliedCond.Reason).To(Equal(string(utils.CRconditionReasons.InProgress)))
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
					Finalizers: []string{provisioningRequestFinalizer},
				},
				Spec: provisioningv1alpha1.ProvisioningRequestSpec{
					TemplateName:    tName,
					TemplateVersion: tVersion,
					TemplateParameters: runtime.RawExtension{
						Raw: []byte(testFullTemplateParameters),
					},
				},
				Status: provisioningv1alpha1.ProvisioningRequestStatus{
					ClusterDetails: &provisioningv1alpha1.ClusterDetails{},
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
				Name:      getClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
		}

		_, err := CTReconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())

		CRReconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}

		CRTask = &provisioningRequestReconcilerTask{
			logger: CRReconciler.Logger,
			client: CRReconciler.Client,
			object: crs[1].(*provisioningv1alpha1.ProvisioningRequest), // cluster-1 request
			timeouts: &timeouts{
				hardwareProvisioning: utils.DefaultHardwareProvisioningTimeout,
				clusterProvisioning:  utils.DefaultClusterProvisioningTimeout,
				clusterConfiguration: 1 * time.Minute,
			},
		}
	})

	It("Returns true if the status is InProgress and the timeout has passed", func() {
		// Set the status to InProgress.
		utils.SetStatusCondition(&CRTask.object.Status.Conditions,
			utils.PRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.InProgress,
			metav1.ConditionFalse,
			"",
		)
		// Create Non-compliant enforce policy
		policy := &policiesv1.Policy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ztp-clustertemplate-a-v4-15.v1-sriov-configuration-policy",
				Namespace: "cluster-1",
				Labels: map[string]string{
					utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-15.v1-sriov-configuration-policy",
					utils.ChildPolicyClusterNameLabel:      "cluster-1",
					utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
				},
			},
			Spec: policiesv1.PolicySpec{
				RemediationAction: "enforce",
			},
			Status: policiesv1.PolicyStatus{
				ComplianceState: "NonCompliant",
				Details: []*policiesv1.DetailsPerTemplate{
					{
						History: []policiesv1.ComplianceHistory{
							{
								LastTimestamp: metav1.NewTime(metav1.Now().Add(-2 * time.Minute)),
								Message:       "NonCompliant;",
							},
						},
					},
				},
			},
		}
		Expect(c.Create(ctx, policy)).To(Succeed())
		policyTimedOut, err := CRTask.hasPolicyConfigurationTimedOut(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(policyTimedOut).To(BeTrue())
	})

	It("Returns false if the status is InProgress and the timeout has not passed", func() {
		// Set the status to InProgress.
		utils.SetStatusCondition(&CRTask.object.Status.Conditions,
			utils.PRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.InProgress,
			metav1.ConditionFalse,
			"",
		)
		// Create Non-compliant enforce policy
		policy := &policiesv1.Policy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ztp-clustertemplate-a-v4-15.v1-sriov-configuration-policy",
				Namespace: "cluster-1",
				Labels: map[string]string{
					utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-15.v1-sriov-configuration-policy",
					utils.ChildPolicyClusterNameLabel:      "cluster-1",
					utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
				},
			},
			Spec: policiesv1.PolicySpec{
				RemediationAction: "enforce",
			},
			Status: policiesv1.PolicyStatus{
				ComplianceState: "NonCompliant",
				Details: []*policiesv1.DetailsPerTemplate{
					{
						History: []policiesv1.ComplianceHistory{
							{
								LastTimestamp: metav1.NewTime(metav1.Now().Add(0 * time.Minute)),
								Message:       "NonCompliant;",
							},
						},
					},
				},
			},
		}
		Expect(c.Create(ctx, policy)).To(Succeed())
		policyTimedOut, err := CRTask.hasPolicyConfigurationTimedOut(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(policyTimedOut).To(BeFalse())
	})

	It("Returns false if the status is Completed", func() {
		// Set the status to InProgress.
		utils.SetStatusCondition(&CRTask.object.Status.Conditions,
			utils.PRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.InProgress,
			metav1.ConditionFalse,
			"",
		)
		// Create Non-compliant enforce policy
		policy := &policiesv1.Policy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ztp-clustertemplate-a-v4-15.v1-sriov-configuration-policy",
				Namespace: "cluster-1",
				Labels: map[string]string{
					utils.ChildPolicyRootPolicyLabel:       "ztp-clustertemplate-a-v4-15.v1-sriov-configuration-policy",
					utils.ChildPolicyClusterNameLabel:      "cluster-1",
					utils.ChildPolicyClusterNamespaceLabel: "cluster-1",
				},
			},
			Spec: policiesv1.PolicySpec{
				RemediationAction: "enforce",
			},
			Status: policiesv1.PolicyStatus{
				ComplianceState: "Compliant",
				Details: []*policiesv1.DetailsPerTemplate{
					{
						History: []policiesv1.ComplianceHistory{
							{
								LastTimestamp: metav1.NewTime(metav1.Now().Add(-2 * time.Minute)),
								Message:       "Compliant;",
							},
						},
					},
				},
			},
		}
		Expect(c.Create(ctx, policy)).To(Succeed())
		policyTimedOut, err := CRTask.hasPolicyConfigurationTimedOut(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(policyTimedOut).To(BeFalse())
	})
})

func TestBuildPoliciesEventHistory(t *testing.T) {
	type args struct {
		policies *policiesv1.PolicyList
	}
	tests := []struct {
		name  string
		args  args
		wantH utils.EventHistory
	}{
		{
			name: "ok",
			args: args{
				policies: &policiesv1.PolicyList{
					Items: []policiesv1.Policy{
						// policy 1
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "policy1",
								Namespace: "namespace1",
							},
							Spec: policiesv1.PolicySpec{
								RemediationAction: "enforce",
							},
							Status: policiesv1.PolicyStatus{
								ComplianceState: "NonCompliant",
								Details: []*policiesv1.DetailsPerTemplate{
									{
										TemplateMeta: metav1.ObjectMeta{Name: "template1"},
										History: []policiesv1.ComplianceHistory{
											{
												LastTimestamp: metav1.Date(2024,
													10,
													18,
													10,
													59,
													21,
													0,
													time.Local),
												Message: "NonCompliant;",
											},
										},
									},
								},
							},
						},
						// policy 2
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "policy2",
								Namespace: "namespace1",
							},
							Spec: policiesv1.PolicySpec{
								RemediationAction: "enforce",
							},
							Status: policiesv1.PolicyStatus{
								ComplianceState: "NonCompliant",
								Details: []*policiesv1.DetailsPerTemplate{
									{
										TemplateMeta: metav1.ObjectMeta{Name: "template1"},
										History: []policiesv1.ComplianceHistory{
											{
												LastTimestamp: metav1.Date(2024,
													10,
													18,
													10,
													59,
													21,
													0,
													time.Local),
												Message: "NonCompliant;",
											},
										},
									},
								},
							},
						},
						// policy 3
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "policy3",
								Namespace: "namespace1",
							},
							Spec: policiesv1.PolicySpec{
								RemediationAction: "enforce",
							},
							Status: policiesv1.PolicyStatus{
								ComplianceState: "Compliant",
								Details: []*policiesv1.DetailsPerTemplate{
									{
										TemplateMeta: metav1.ObjectMeta{Name: "template2"},
										History: []policiesv1.ComplianceHistory{
											{
												LastTimestamp: metav1.Date(2024,
													10,
													18,
													10,
													59,
													21,
													0,
													time.Local),
												Message: "Compliant;",
											},
										},
									},
								},
							},
						},
						// policy 4
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "policy4",
								Namespace: "namespace1",
							},
							Spec: policiesv1.PolicySpec{
								RemediationAction: "enforce",
							},
							Status: policiesv1.PolicyStatus{
								ComplianceState: "Pending",
								Details: []*policiesv1.DetailsPerTemplate{
									{
										TemplateMeta: metav1.ObjectMeta{Name: "template2"},
										History: []policiesv1.ComplianceHistory{
											{
												LastTimestamp: metav1.Date(2024,
													10,
													18,
													10,
													59,
													21,
													0,
													time.Local),
												Message: "Pending;",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantH: utils.EventHistory{
				History: []*utils.Event{
					{
						ObjectID: "policy1.template1", Timestamp: time.Date(2024,
							10,
							18,
							10,
							59,
							21,
							0,
							time.Local),
						State: "InProgress",
					},
					{
						ObjectID: "policy2.template1", Timestamp: time.Date(2024,
							10,
							18,
							10,
							59,
							21,
							0,
							time.Local),
						State: "InProgress",
					},
					{
						ObjectID: "policy3.template2", Timestamp: time.Date(2024,
							10,
							18,
							10,
							59,
							21,
							0,
							time.Local),
						State: "Completed",
					},
					{
						ObjectID: "policy4.template2", Timestamp: time.Date(2024,
							10,
							18,
							10,
							59,
							21,
							0,
							time.Local),
						State: "InProgress",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotH := BuildPoliciesEventHistory(tt.args.policies); !reflect.DeepEqual(gotH, tt.wantH) {
				t.Errorf("BuildPoliciesEventHistory() = %v, want %v", gotH, tt.wantH)
			}
		})
	}
}
