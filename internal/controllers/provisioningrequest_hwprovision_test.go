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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift/assisted-service/api/v1beta1"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
)

var _ = Describe("renderHardwareTemplate", func() {
	var (
		ctx             context.Context
		c               client.Client
		reconciler      *ProvisioningRequestReconciler
		task            *provisioningRequestReconcilerTask
		clusterInstance *siteconfig.ClusterInstance
		ct              *provisioningv1alpha1.ClusterTemplate
		cr              *provisioningv1alpha1.ProvisioningRequest
		tName           = "clustertemplate-a"
		tVersion        = "v1.0.0"
		ctNamespace     = "clustertemplate-a-v4-16"
		hwTemplateCm    = "hwTemplate-v1"
		hwTemplateCmv2  = "hwTemplate-v2"
		crName          = "cluster-1"
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Define the cluster instance.
		clusterInstance = &siteconfig.ClusterInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: ctNamespace,
			},
			Spec: siteconfig.ClusterInstanceSpec{
				Nodes: []siteconfig.NodeSpec{
					{
						Role: "master",
						NodeNetwork: &v1beta1.NMStateConfigSpec{
							Interfaces: []*v1beta1.Interface{
								{Name: "eno12399"},
								{Name: "eno1"},
							},
						},
					},
					{
						Role: "master",
						NodeNetwork: &v1beta1.NMStateConfigSpec{
							Interfaces: []*v1beta1.Interface{
								{Name: "eno12399"},
								{Name: "eno1"},
							},
						},
					},
					{
						Role: "worker",
						NodeNetwork: &v1beta1.NMStateConfigSpec{
							Interfaces: []*v1beta1.Interface{
								{Name: "eno1"},
							},
						},
					},
				},
			},
		}

		// Define the provisioning request.
		cr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    tName,
				TemplateVersion: tVersion,
				TemplateParameters: runtime.RawExtension{
					Raw: []byte(testFullTemplateParameters),
				},
			},
		}

		// Define the cluster template.
		ct = &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:    tName,
				Version: tVersion,
				Templates: provisioningv1alpha1.Templates{
					HwTemplate: hwTemplateCm,
				},
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
		}

		c = getFakeClientFromObjects([]client.Object{cr}...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &provisioningRequestReconcilerTask{
			logger: reconciler.Logger,
			client: reconciler.Client,
			object: cr,
		}
	})

	It("returns no error when renderHardwareTemplate succeeds", func() {
		// Ensure the ClusterTemplate is created
		Expect(c.Create(ctx, ct)).To(Succeed())

		// Define the hardware template config map
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hwTemplateCm,
				Namespace: utils.InventoryNamespace,
			},
			Data: map[string]string{
				utils.HwTemplatePluginMgr:      utils.UnitTestHwmgrID,
				utils.HwTemplateBootIfaceLabel: "bootable-interface",
				utils.HwTemplateNodePool: `
- name: master
  hwProfile: profile-spr-single-processor-64G
- name: worker
  hwProfile: profile-spr-dual-processor-128G`,
				utils.HwTemplateExtensions: `resourceTypeId: ResourceGroup~2.1.1`,
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		nodePool, err := task.renderHardwareTemplate(ctx, clusterInstance)
		Expect(err).ToNot(HaveOccurred())
		Expect(nodePool).ToNot(BeNil())
		Expect(nodePool.ObjectMeta.Name).To(Equal(clusterInstance.GetName()))
		Expect(nodePool.ObjectMeta.Namespace).To(Equal(utils.UnitTestHwmgrNamespace))
		Expect(nodePool.Annotations[utils.HwTemplateBootIfaceLabel]).To(Equal(cm.Data[utils.HwTemplateBootIfaceLabel]))

		Expect(nodePool.Spec.CloudID).To(Equal(clusterInstance.GetName()))
		Expect(nodePool.Spec.HwMgrId).To(Equal(cm.Data[utils.HwTemplatePluginMgr]))
		Expect(nodePool.Labels[provisioningRequestNameLabel]).To(Equal(task.object.Name))

		roleCounts := make(map[string]int)
		err = utils.ProcessClusterNodeGroups(clusterInstance, nodePool.Spec.NodeGroup, roleCounts)
		Expect(err).ToNot(HaveOccurred())
		masterNodeGroup, err := utils.FindNodeGroupByRole("master", nodePool.Spec.NodeGroup)
		Expect(err).ToNot(HaveOccurred())
		workerNodeGroup, err := utils.FindNodeGroupByRole("worker", nodePool.Spec.NodeGroup)
		Expect(err).ToNot(HaveOccurred())

		Expect(nodePool.Spec.NodeGroup).To(HaveLen(2))
		expectedNodeGroups := map[string]struct {
			size       int
			interfaces []string
		}{
			"master": {size: roleCounts["master"], interfaces: masterNodeGroup.Interfaces},
			"worker": {size: roleCounts["worker"], interfaces: workerNodeGroup.Interfaces},
		}

		for _, group := range nodePool.Spec.NodeGroup {
			expected, found := expectedNodeGroups[group.Name]
			Expect(found).To(BeTrue())
			Expect(group.Size).To(Equal(expected.size))
			Expect(group.Interfaces).To(ConsistOf(expected.interfaces))
		}
	})

	It("returns an error when the HwTemplate is not found", func() {
		// Ensure the ClusterTemplate is created
		Expect(c.Create(ctx, ct)).To(Succeed())
		nodePool, err := task.renderHardwareTemplate(ctx, clusterInstance)
		Expect(err).To(HaveOccurred())
		Expect(nodePool).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("failed to get the %s configmap for Hardware Template", hwTemplateCm))
	})

	It("returns an error when the ClusterTemplate is not found", func() {
		nodePool, err := task.renderHardwareTemplate(ctx, clusterInstance)
		Expect(err).To(HaveOccurred())
		Expect(nodePool).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("failed to get the ClusterTemplate"))
	})

	Context("When NodePool has been created", func() {
		var nodePool *hwv1alpha1.NodePool

		BeforeEach(func() {
			// Create NodePool resource
			nodePool = &hwv1alpha1.NodePool{}

			nodePool.SetName(crName)
			nodePool.SetNamespace("hwmgr")
			nodePool.Spec.HwMgrId = utils.UnitTestHwmgrID
			nodePool.Spec.NodeGroup = []hwv1alpha1.NodeGroup{
				{Name: "master", HwProfile: "profile-spr-single-processor-64G", Size: 1, Interfaces: []string{"eno1"}},
			}
			nodePool.Status.Conditions = []metav1.Condition{
				{Type: string(hwv1alpha1.Provisioned), Status: metav1.ConditionFalse, Reason: string(hwv1alpha1.InProgress)},
			}
			Expect(c.Create(ctx, nodePool)).To(Succeed())
		})
		It("returns an error when the hardware template contains a change in hwMgrId", func() {
			ct.Spec.Templates.HwTemplate = hwTemplateCmv2
			// Ensure the ClusterTemplate is created
			Expect(c.Create(ctx, ct)).To(Succeed())

			// Define the new version of hardware template config map
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      hwTemplateCmv2,
					Namespace: utils.InventoryNamespace,
				},
				Data: map[string]string{
					utils.HwTemplatePluginMgr:      "new id",
					utils.HwTemplateBootIfaceLabel: "bootable-interface",
					utils.HwTemplateNodePool: `
	- name: master
      hwProfile: profile-spr-single-processor-64G`,
					utils.HwTemplateExtensions: `resourceTypeId: ResourceGroup~2.1.1`,
				},
			}
			Expect(c.Create(ctx, cm)).To(Succeed())

			_, err := task.renderHardwareTemplate(ctx, clusterInstance)
			Expect(err).To(HaveOccurred())
			cond := meta.FindStatusCondition(cr.Status.Conditions, string(utils.PRconditionTypes.HardwareTemplateRendered))
			Expect(cond).ToNot(BeNil())
			verifyStatusCondition(*cond, metav1.Condition{
				Type:    string(utils.PRconditionTypes.HardwareTemplateRendered),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.Failed),
				Message: "Failed to render the Hardware template",
			})
		})

		It("returns an error when the hardware template contains a change in bootIntefaceLabel", func() {
			ct.Spec.Templates.HwTemplate = hwTemplateCmv2
			// Ensure the ClusterTemplate is created
			Expect(c.Create(ctx, ct)).To(Succeed())

			// Define the new version of hardware template config map
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      hwTemplateCmv2,
					Namespace: utils.InventoryNamespace,
				},
				Data: map[string]string{
					utils.HwTemplatePluginMgr:      utils.UnitTestHwmgrID,
					utils.HwTemplateBootIfaceLabel: "new-label",
					utils.HwTemplateNodePool: `
	- name: master
      hwProfile: profile-spr-single-processor-64G`,
					utils.HwTemplateExtensions: `resourceTypeId: ResourceGroup~2.1.1`,
				},
			}
			Expect(c.Create(ctx, cm)).To(Succeed())

			_, err := task.renderHardwareTemplate(ctx, clusterInstance)
			Expect(err).To(HaveOccurred())
			cond := meta.FindStatusCondition(cr.Status.Conditions, string(utils.PRconditionTypes.HardwareTemplateRendered))
			Expect(cond).ToNot(BeNil())
			verifyStatusCondition(*cond, metav1.Condition{
				Type:    string(utils.PRconditionTypes.HardwareTemplateRendered),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.Failed),
				Message: "Failed to render the Hardware template",
			})
		})

		It("returns an error when the hardware template contains a change in groups", func() {
			ct.Spec.Templates.HwTemplate = hwTemplateCmv2
			// Ensure the ClusterTemplate is created
			Expect(c.Create(ctx, ct)).To(Succeed())

			// Define the new version of hardware template config map
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      hwTemplateCmv2,
					Namespace: utils.InventoryNamespace,
				},
				Data: map[string]string{
					utils.HwTemplatePluginMgr:      utils.UnitTestHwmgrID,
					utils.HwTemplateBootIfaceLabel: "bootable-interface",
					utils.HwTemplateNodePool: `
	- name: master
      hwProfile: profile-spr-single-processor-64G
	- name: worker
      hwProfile: profile-spr-single-processor-64G`,
					utils.HwTemplateExtensions: `resourceTypeId: ResourceGroup~2.1.1`,
				},
			}
			Expect(c.Create(ctx, cm)).To(Succeed())

			_, err := task.renderHardwareTemplate(ctx, clusterInstance)
			Expect(err).To(HaveOccurred())
			cond := meta.FindStatusCondition(cr.Status.Conditions, string(utils.PRconditionTypes.HardwareTemplateRendered))
			Expect(cond).ToNot(BeNil())
			verifyStatusCondition(*cond, metav1.Condition{
				Type:    string(utils.PRconditionTypes.HardwareTemplateRendered),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.CRconditionReasons.Failed),
				Message: "Failed to render the Hardware template",
			})
		})
	})
})

var _ = Describe("waitForNodePoolProvision", func() {
	var (
		ctx         context.Context
		c           client.Client
		reconciler  *ProvisioningRequestReconciler
		task        *provisioningRequestReconcilerTask
		cr          *provisioningv1alpha1.ProvisioningRequest
		ci          *unstructured.Unstructured
		np          *hwv1alpha1.NodePool
		crName      = "cluster-1"
		ctNamespace = "clustertemplate-a-v4-16"
	)

	BeforeEach(func() {
		ctx = context.Background()
		// Define the cluster instance.
		ci = &unstructured.Unstructured{}
		ci.SetName(crName)
		ci.SetNamespace(ctNamespace)
		ci.Object = map[string]interface{}{
			"spec": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{"role": "master"},
					map[string]interface{}{"role": "master"},
					map[string]interface{}{"role": "worker"},
				},
			},
		}

		// Define the provisioning request.
		cr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
		}

		// Define the node pool.
		np = &hwv1alpha1.NodePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
			// Set up your NodePool object as needed
			Status: hwv1alpha1.NodePoolStatus{
				Conditions: []metav1.Condition{},
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr}...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &provisioningRequestReconcilerTask{
			logger: reconciler.Logger,
			client: reconciler.Client,
			object: cr,
			timeouts: &timeouts{
				hardwareProvisioning: 1 * time.Minute,
			},
		}
	})

	It("returns error when error fetching NodePool", func() {
		provisioned, timedOutOrFailed, err := task.checkNodePoolStatus(ctx, np, hwv1alpha1.Provisioned)
		Expect(provisioned).To(Equal(false))
		Expect(timedOutOrFailed).To(Equal(false))
		Expect(err).To(HaveOccurred())
	})

	It("returns failed when NodePool provisioning failed", func() {
		provisionedCondition := metav1.Condition{
			Type:   "Provisioned",
			Status: metav1.ConditionFalse,
			Reason: string(hwv1alpha1.Failed),
		}
		np.Status.Conditions = append(np.Status.Conditions, provisionedCondition)
		Expect(c.Create(ctx, np)).To(Succeed())
		provisioned, timedOutOrFailed, err := task.checkNodePoolStatus(ctx, np, hwv1alpha1.Provisioned)
		Expect(provisioned).To(Equal(false))
		Expect(timedOutOrFailed).To(Equal(true)) // It should be failed
		Expect(err).ToNot(HaveOccurred())
		condition := meta.FindStatusCondition(cr.Status.Conditions, string(utils.PRconditionTypes.HardwareProvisioned))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(string(hwv1alpha1.Failed)))
	})

	It("returns timeout when NodePool provisioning timed out", func() {
		provisionedCondition := metav1.Condition{
			Type:   "Provisioned",
			Status: metav1.ConditionFalse,
		}
		np.Status.Conditions = append(np.Status.Conditions, provisionedCondition)
		Expect(c.Create(ctx, np)).To(Succeed())

		// First call to checkNodePoolStatus (before timeout)
		provisioned, timedOutOrFailed, err := task.checkNodePoolStatus(ctx, np, hwv1alpha1.Provisioned)
		Expect(provisioned).To(Equal(false))
		Expect(timedOutOrFailed).To(Equal(false))
		Expect(err).ToNot(HaveOccurred())

		// Simulate a timeout by moving the start time back
		adjustedTime := cr.Status.NodePoolRef.HardwareProvisioningCheckStart.Time.Add(-1 * time.Minute)
		cr.Status.NodePoolRef.HardwareProvisioningCheckStart = metav1.NewTime(adjustedTime)

		// Call checkNodePoolStatus again (after timeout)
		provisioned, timedOutOrFailed, err = task.checkNodePoolStatus(ctx, np, hwv1alpha1.Provisioned)
		Expect(provisioned).To(Equal(false))
		Expect(timedOutOrFailed).To(Equal(true)) // Now it should time out
		Expect(err).ToNot(HaveOccurred())

		condition := meta.FindStatusCondition(cr.Status.Conditions, string(utils.PRconditionTypes.HardwareProvisioned))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(string(hwv1alpha1.TimedOut)))
	})

	It("returns false when NodePool is not provisioned", func() {
		provisionedCondition := metav1.Condition{
			Type:   "Provisioned",
			Status: metav1.ConditionFalse,
		}
		np.Status.Conditions = append(np.Status.Conditions, provisionedCondition)
		Expect(c.Create(ctx, np)).To(Succeed())

		provisioned, timedOutOrFailed, err := task.checkNodePoolStatus(ctx, np, hwv1alpha1.Provisioned)
		Expect(provisioned).To(Equal(false))
		Expect(timedOutOrFailed).To(Equal(false))
		Expect(err).ToNot(HaveOccurred())
		condition := meta.FindStatusCondition(cr.Status.Conditions, string(utils.PRconditionTypes.HardwareProvisioned))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
	})

	It("returns true when NodePool is provisioned", func() {
		provisionedCondition := metav1.Condition{
			Type:   "Provisioned",
			Status: metav1.ConditionTrue,
		}
		np.Status.Conditions = append(np.Status.Conditions, provisionedCondition)
		Expect(c.Create(ctx, np)).To(Succeed())
		provisioned, timedOutOrFailed, err := task.checkNodePoolStatus(ctx, np, hwv1alpha1.Provisioned)
		Expect(provisioned).To(Equal(true))
		Expect(timedOutOrFailed).To(Equal(false))
		Expect(err).ToNot(HaveOccurred())
		condition := meta.FindStatusCondition(cr.Status.Conditions, string(utils.PRconditionTypes.HardwareProvisioned))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
	})

	It("returns timeout when NodePool configuring timed out", func() {
		provisionedCondition := metav1.Condition{
			Type:   "Provisioned",
			Status: metav1.ConditionTrue,
		}
		np.Status.Conditions = append(np.Status.Conditions, provisionedCondition)
		Expect(c.Create(ctx, np)).To(Succeed())

		configuredCondition := metav1.Condition{
			Type:   "Configured",
			Status: metav1.ConditionFalse,
		}
		np.Status.Conditions = append(np.Status.Conditions, configuredCondition)
		Expect(c.Status().Update(ctx, np)).To(Succeed())

		// First call to checkNodePoolStatus (before timeout)
		status, timedOutOrFailed, err := task.checkNodePoolStatus(ctx, np, hwv1alpha1.Configured)
		Expect(status).To(Equal(false))
		Expect(timedOutOrFailed).To(Equal(false))
		Expect(err).ToNot(HaveOccurred())

		// Simulate a timeout by moving the start time back
		adjustedTime := cr.Status.NodePoolRef.HardwareConfiguringCheckStart.Time.Add(-1 * time.Minute)
		cr.Status.NodePoolRef.HardwareConfiguringCheckStart = metav1.NewTime(adjustedTime)

		// Call checkNodePoolStatus again (after timeout)
		status, timedOutOrFailed, err = task.checkNodePoolStatus(ctx, np, hwv1alpha1.Configured)
		Expect(status).To(Equal(false))
		Expect(timedOutOrFailed).To(Equal(true)) // Now it should time out
		Expect(err).ToNot(HaveOccurred())

		condition := meta.FindStatusCondition(cr.Status.Conditions, string(utils.PRconditionTypes.HardwareConfigured))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(string(hwv1alpha1.TimedOut)))
	})
})

var _ = Describe("updateClusterInstance", func() {
	var (
		ctx         context.Context
		c           client.Client
		reconciler  *ProvisioningRequestReconciler
		task        *provisioningRequestReconcilerTask
		cr          *provisioningv1alpha1.ProvisioningRequest
		ci          *siteconfig.ClusterInstance
		np          *hwv1alpha1.NodePool
		crName      = "cluster-1"
		crNamespace = "clustertemplate-a-v4-16"
		mn          = "master-node"
		wn          = "worker-node"
		mhost       = "node1.test.com"
		whost       = "node2.test.com"
		poolns      = utils.UnitTestHwmgrNamespace
		mIfaces     = []*hwv1alpha1.Interface{
			{
				Name:       "eth0",
				Label:      "test",
				MACAddress: "00:00:00:01:20:30",
			},
			{
				Name:       "eth1",
				Label:      "test2",
				MACAddress: "66:77:88:99:CC:BB",
			},
		}
		wIfaces = []*hwv1alpha1.Interface{
			{
				Name:       "eno1",
				Label:      "test",
				MACAddress: "00:00:00:01:30:10",
			},
			{
				Name:       "eno2",
				Label:      "test2",
				MACAddress: "66:77:88:99:AA:BB",
			},
		}
		masterNode = createNode(mn, "idrac-virtualmedia+https://10.16.2.1/redfish/v1/Systems/System.Embedded.1",
			"site-1-master-bmc-secret", "master", poolns, crName, mIfaces)
		workerNode = createNode(wn, "idrac-virtualmedia+https://10.16.3.4/redfish/v1/Systems/System.Embedded.1",
			"site-1-worker-bmc-secret", "worker", poolns, crName, wIfaces)
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Define the cluster instance.
		ci = &siteconfig.ClusterInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: crNamespace,
			},
			Spec: siteconfig.ClusterInstanceSpec{
				Nodes: []siteconfig.NodeSpec{
					{
						Role: "master", HostName: mhost,
						NodeNetwork: &v1beta1.NMStateConfigSpec{
							Interfaces: []*v1beta1.Interface{
								{Name: "eth0"}, {Name: "eth1"},
							},
						},
					},
					{
						Role: "worker", HostName: whost,
						NodeNetwork: &v1beta1.NMStateConfigSpec{
							Interfaces: []*v1beta1.Interface{
								{Name: "eno1"}, {Name: "eno2"},
							},
						},
					},
				},
			},
		}

		// Define the provisioning request.
		cr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
		}

		// Define the node pool.
		np = &hwv1alpha1.NodePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: poolns,
				Annotations: map[string]string{
					utils.HwTemplateBootIfaceLabel: "test",
				},
			},
			Status: hwv1alpha1.NodePoolStatus{
				Conditions: []metav1.Condition{
					{
						Type:   "Provisioned",
						Status: "True",
					},
				},
				Properties: hwv1alpha1.Properties{
					NodeNames: []string{mn, wn},
				},
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr}...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &provisioningRequestReconcilerTask{
			logger:       reconciler.Logger,
			client:       reconciler.Client,
			object:       cr,
			clusterInput: &clusterInput{},
		}
	})

	It("returns error when failing to get the Node object", func() {
		err := task.updateClusterInstance(ctx, ci, np)
		Expect(err).To(HaveOccurred())
	})

	It("returns no error when updateClusterInstance succeeds", func() {
		task.clusterInput.clusterInstanceData = map[string]any{
			"nodes": []any{
				map[string]any{
					"hostName": mhost,
					"nodeNetwork": map[string]any{
						"interfaces": []any{
							map[string]any{
								"name":  "eth0",
								"label": "test",
							},
							map[string]any{
								"name":  "eth1",
								"label": "test2",
							},
						},
					},
				},
				map[string]any{
					"hostName": whost,
					"nodeNetwork": map[string]any{
						"interfaces": []any{
							map[string]any{
								"name":  "eno1",
								"label": "test",
							},
							map[string]any{
								"name":  "eno2",
								"label": "test2",
							},
						},
					},
				},
			},
		}
		nodes := []*hwv1alpha1.Node{masterNode, workerNode}
		secrets := createSecrets([]string{masterNode.Status.BMC.CredentialsName, workerNode.Status.BMC.CredentialsName}, poolns)

		createResources(ctx, c, nodes, secrets)

		err := task.updateClusterInstance(ctx, ci, np)
		Expect(err).ToNot(HaveOccurred())

		masterBootMAC, err := utils.GetBootMacAddress(masterNode.Status.Interfaces, np)
		Expect(err).ToNot(HaveOccurred())
		workerBootMAC, err := utils.GetBootMacAddress(workerNode.Status.Interfaces, np)
		Expect(err).ToNot(HaveOccurred())

		// Define expected details
		expectedDetails := []expectedNodeDetails{
			{
				BMCAddress:         masterNode.Status.BMC.Address,
				BMCCredentialsName: masterNode.Status.BMC.CredentialsName,
				BootMACAddress:     masterBootMAC,
				Interfaces:         getInterfaceMap(masterNode.Status.Interfaces),
			},
			{
				BMCAddress:         workerNode.Status.BMC.Address,
				BMCCredentialsName: workerNode.Status.BMC.CredentialsName,
				BootMACAddress:     workerBootMAC,
				Interfaces:         getInterfaceMap(workerNode.Status.Interfaces),
			},
		}

		// Verify the bmc address, secret, boot mac address and interface mac addresses are set correctly in the cluster instance
		verifyClusterInstance(ci, expectedDetails)

		// Verify the host name is set in the node status
		verifyNodeStatus(ctx, c, nodes, mhost, whost)
	})
})

// Helper function to transform interfaces into the required map[string]interface{} format
func getInterfaceMap(interfaces []*hwv1alpha1.Interface) []map[string]interface{} {
	var ifaceList []map[string]interface{}
	for _, iface := range interfaces {
		ifaceList = append(ifaceList, map[string]interface{}{
			"Name":       iface.Name,
			"MACAddress": iface.MACAddress,
		})
	}
	return ifaceList
}

func createNode(name, bmcAddress, bmcSecret, groupName, namespace, npName string, interfaces []*hwv1alpha1.Interface) *hwv1alpha1.Node {
	return &hwv1alpha1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: hwv1alpha1.NodeSpec{
			NodePool:  npName,
			GroupName: groupName,
		},
		Status: hwv1alpha1.NodeStatus{
			BMC: &hwv1alpha1.BMC{
				Address:         bmcAddress,
				CredentialsName: bmcSecret,
			},
			Interfaces: interfaces,
		},
	}
}

func createSecrets(names []string, namespace string) []*corev1.Secret {
	var secrets []*corev1.Secret
	for _, name := range names {
		secrets = append(secrets, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		})
	}
	return secrets
}

func createResources(ctx context.Context, c client.Client, nodes []*hwv1alpha1.Node, secrets []*corev1.Secret) {
	for _, node := range nodes {
		Expect(c.Create(ctx, node)).To(Succeed())
	}
	for _, secret := range secrets {
		Expect(c.Create(ctx, secret)).To(Succeed())
	}
}

func verifyClusterInstance(ci *siteconfig.ClusterInstance, expectedDetails []expectedNodeDetails) {
	for i, expected := range expectedDetails {
		Expect(ci.Spec.Nodes[i].BmcAddress).To(Equal(expected.BMCAddress))
		Expect(ci.Spec.Nodes[i].BmcCredentialsName.Name).To(Equal(expected.BMCCredentialsName))
		Expect(ci.Spec.Nodes[i].BootMACAddress).To(Equal(expected.BootMACAddress))
		// Verify Interface MAC Address
		for _, iface := range ci.Spec.Nodes[i].NodeNetwork.Interfaces {
			for _, expectedIface := range expected.Interfaces {
				// Compare the interface name and MAC address
				if iface.Name == expectedIface["Name"] {
					Expect(iface.MacAddress).To(Equal(expectedIface["MACAddress"]), "MAC Address mismatch for interface")
				}
			}
		}
	}
}

func verifyNodeStatus(ctx context.Context, c client.Client, nodes []*hwv1alpha1.Node, mhost, whost string) {
	for _, node := range nodes {
		updatedNode := &hwv1alpha1.Node{}
		Expect(c.Get(ctx, client.ObjectKey{Name: node.Name, Namespace: node.Namespace}, updatedNode)).To(Succeed())
		switch updatedNode.Spec.GroupName {
		case "master":
			Expect(updatedNode.Status.Hostname).To(Equal(mhost))
		case "worker":
			Expect(updatedNode.Status.Hostname).To(Equal(whost))
		default:
			Fail(fmt.Sprintf("Unexpected GroupName: %s", updatedNode.Spec.GroupName))
		}
	}
}
