package controllers

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type expectedNodeDetails struct {
	BMCAddress         string
	BMCCredentialsName string
	BootMACAddress     string
}

var _ = DescribeTable(
	"Reconciler",
	func(objs []client.Object, request reconcile.Request,
		validate func(result ctrl.Result, reconciler ClusterRequestReconciler)) {

		// Declare the Namespace for the ClusterRequest resource.
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster-template",
			},
		}
		// Declare the Namespace for the managed cluster resource.
		nsSite := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "site-sno-du-1",
			},
		}

		// Update the testcase objects to include the Namespace.
		objs = append(objs, ns, nsSite)

		// Get the fake client.
		fakeClient := getFakeClientFromObjects(objs...)

		// Initialize the O-RAN O2IMS reconciler.
		r := &ClusterRequestReconciler{
			Client: fakeClient,
			Logger: logger,
		}

		// Reconcile.
		result, err := r.Reconcile(context.TODO(), request)
		Expect(err).ToNot(HaveOccurred())

		validate(result, *r)
	},

	Entry(
		"ClusterTemplate specified by ClusterTemplateRef is missing and input is valid",
		[]client.Object{
			&oranv1alpha1.ClusterRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "cluster-request",
					Namespace:  "cluster-template",
					Finalizers: []string{clusterRequestFinalizer},
				},
				Spec: oranv1alpha1.ClusterRequestSpec{
					ClusterTemplateRef: "cluster-template",
					ClusterTemplateInput: oranv1alpha1.ClusterTemplateInput{
						ClusterInstanceInput: runtime.RawExtension{
							Raw: []byte(`{
								"name": "Bob",
								"age": 35,
								"email": "bob@example.com",
								"phoneNumbers": ["123-456-7890", "987-654-3210"]
							}`),
						},
					},
				},
			},
		},
		reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "cluster-request",
				Namespace: "cluster-template",
			},
		},
		func(result ctrl.Result, reconciler ClusterRequestReconciler) {
			// Get the ClusterRequest and check that everything is valid.
			clusterRequest := &oranv1alpha1.ClusterRequest{}
			err := reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      "cluster-request",
					Namespace: "cluster-template",
				},
				clusterRequest)
			Expect(err).ToNot(HaveOccurred())
		},
	),
)

var _ = Describe("getCrClusterTemplateRef", func() {
	var (
		ctx          context.Context
		c            client.Client
		reconciler   *ClusterRequestReconciler
		task         *clusterRequestReconcilerTask
		ctName       = "clustertemplate-a-v1"
		ctNamespace  = "clustertemplate-a-v4-16"
		ciDefaultsCm = "clusterinstance-defaults-v1"
		ptDefaultsCm = "policytemplate-defaults-v1"
		crName       = "cluster-1"
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Define the cluster request.
		cr := &oranv1alpha1.ClusterRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: ctNamespace,
			},
			Spec: oranv1alpha1.ClusterRequestSpec{
				ClusterTemplateRef: ctName,
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr}...)
		reconciler = &ClusterRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &clusterRequestReconcilerTask{
			logger: reconciler.Logger,
			client: reconciler.Client,
			object: cr,
		}
	})

	It("returns error if the referred ClusterTemplate is missing", func() {
		// Define the cluster template.
		ct := &oranv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-cluster-template-name",
				Namespace: ctNamespace,
			},
			Spec: oranv1alpha1.ClusterTemplateSpec{
				Templates: oranv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
				},
				InputDataSchema: oranv1alpha1.InputDataSchema{
					ClusterInstanceSchema: runtime.RawExtension{},
				},
			},
		}

		Expect(c.Create(ctx, ct)).To(Succeed())

		retCt, err := task.getCrClusterTemplateRef(context.TODO())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			fmt.Sprintf("the referenced ClusterTemplate (%s) does not exist in the %s namespace",
				ctName, ctNamespace)))
		Expect(retCt).To(Equal((*oranv1alpha1.ClusterTemplate)(nil)))
	})

	It("returns the referred ClusterTemplate if it exists", func() {
		// Define the cluster template.
		ct := &oranv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ctName,
				Namespace: ctNamespace,
			},
			Spec: oranv1alpha1.ClusterTemplateSpec{
				Templates: oranv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
				},
				InputDataSchema: oranv1alpha1.InputDataSchema{
					ClusterInstanceSchema: runtime.RawExtension{},
				},
			},
		}

		Expect(c.Create(ctx, ct)).To(Succeed())

		retCt, err := task.getCrClusterTemplateRef(context.TODO())
		Expect(err).ToNot(HaveOccurred())
		Expect(retCt.Name).To(Equal(ctName))
		Expect(retCt.Namespace).To(Equal(ctNamespace))
		Expect(retCt.Spec.Templates.ClusterInstanceDefaults).To(Equal(ciDefaultsCm))
		Expect(retCt.Spec.Templates.PolicyTemplateDefaults).To(Equal(ptDefaultsCm))
	})
})

var _ = Describe("createPolicyTemplateConfigMap", func() {
	var (
		ctx         context.Context
		c           client.Client
		reconciler  *ClusterRequestReconciler
		task        *clusterRequestReconcilerTask
		ctName      = "clustertemplate-a-v1"
		ctNamespace = "clustertemplate-a-v4-16"
		crName      = "cluster-1"
	)

	BeforeEach(func() {
		ctx := context.Background()
		// Define the cluster request.
		cr := &oranv1alpha1.ClusterRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: ctNamespace,
			},
			Spec: oranv1alpha1.ClusterRequestSpec{
				ClusterTemplateRef: ctName,
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr}...)
		reconciler = &ClusterRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &clusterRequestReconcilerTask{
			logger:       reconciler.Logger,
			client:       reconciler.Client,
			object:       cr,
			clusterInput: &clusterInput{},
		}

		// Define the cluster template.
		ztpNs := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ztp-" + ctNamespace,
			},
		}

		Expect(c.Create(ctx, ztpNs)).To(Succeed())
	})

	It("it returns no error if there is no template data", func() {
		err := task.createPolicyTemplateConfigMap(ctx, crName)
		Expect(err).ToNot(HaveOccurred())
	})

	It("it creates the policy template configmap with the correct content", func() {
		// Declare the merged policy template data.
		task.clusterInput.policyTemplateData = map[string]any{
			"cpu-isolated":    "0-1,64-65",
			"hugepages-count": "32",
		}

		// Create the configMap.
		err := task.createPolicyTemplateConfigMap(ctx, crName)
		Expect(err).ToNot(HaveOccurred())

		// Check that the configMap exists in the expected namespace.
		configMapName := crName + "-pg"
		configMapNs := "ztp-" + ctNamespace
		configMap := &corev1.ConfigMap{}
		configMapExists, err := utils.DoesK8SResourceExist(
			ctx, c, configMapName, configMapNs, configMap)
		Expect(err).ToNot(HaveOccurred())
		Expect(configMapExists).To(BeTrue())
		Expect(configMap.Data).To(Equal(
			map[string]string{
				"cpu-isolated":    "0-1,64-65",
				"hugepages-count": "32",
			},
		))
	})
})

var _ = Describe("renderHardwareTemplate", func() {
	var (
		ctx             context.Context
		c               client.Client
		reconciler      *ClusterRequestReconciler
		task            *clusterRequestReconcilerTask
		clusterInstance *unstructured.Unstructured
		ct              *oranv1alpha1.ClusterTemplate
		ctName          = "clustertemplate-a-v1"
		ctNamespace     = "clustertemplate-a-v4-16"
		hwTemplateCm    = "hwTemplate-v1"
		crName          = "cluster-1"
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Define the cluster instance.
		clusterInstance = &unstructured.Unstructured{}
		clusterInstance.SetName(crName)
		clusterInstance.SetNamespace(ctNamespace)
		clusterInstance.Object = map[string]interface{}{
			"spec": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{"role": "master"},
					map[string]interface{}{"role": "master"},
					map[string]interface{}{"role": "worker"},
				},
			},
		}

		// Define the cluster request.
		cr := &oranv1alpha1.ClusterRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: ctNamespace,
			},
			Spec: oranv1alpha1.ClusterRequestSpec{
				ClusterTemplateRef: ctName,
			},
		}

		// Define the cluster template.
		ct = &oranv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ctName,
				Namespace: ctNamespace,
			},
			Spec: oranv1alpha1.ClusterTemplateSpec{
				Templates: oranv1alpha1.Templates{
					HwTemplate: hwTemplateCm,
				},
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr}...)
		reconciler = &ClusterRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &clusterRequestReconcilerTask{
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
				Namespace: utils.ORANO2IMSNamespace,
			},
			Data: map[string]string{
				"hwMgrId": "hwmgr",
				utils.HwTemplateNodePool: `
- name: master
  hwProfile: profile-spr-single-processor-64G
- name: worker
  hwProfile: profile-spr-dual-processor-128G`,
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		nodePool, err := task.renderHardwareTemplate(ctx, clusterInstance)
		Expect(err).ToNot(HaveOccurred())
		Expect(nodePool).ToNot(BeNil())
		Expect(nodePool.ObjectMeta.Name).To(Equal(clusterInstance.GetName()))
		Expect(nodePool.ObjectMeta.Namespace).To(Equal(cm.Data[utils.HwTemplatePluginMgr]))

		Expect(nodePool.Spec.CloudID).To(Equal(clusterInstance.GetName()))
		Expect(nodePool.Labels[clusterRequestNameLabel]).To(Equal(task.object.Name))
		Expect(nodePool.Labels[clusterRequestNamespaceLabel]).To(Equal(task.object.Namespace))

		Expect(nodePool.Spec.NodeGroup).To(HaveLen(2))
		for _, group := range nodePool.Spec.NodeGroup {
			switch group.Name {
			case "master":
				Expect(group.Size).To(Equal(2)) // 2 master
			case "worker":
				Expect(group.Size).To(Equal(1)) // 1 worker
			default:
				Fail(fmt.Sprintf("Unexpected Group Name: %s", group.Name))
			}
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
})

var _ = Describe("waitForNodePoolProvision", func() {
	var (
		ctx         context.Context
		c           client.Client
		reconciler  *ClusterRequestReconciler
		task        *clusterRequestReconcilerTask
		cr          *oranv1alpha1.ClusterRequest
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

		// Define the cluster request.
		cr = &oranv1alpha1.ClusterRequest{
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
		reconciler = &ClusterRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &clusterRequestReconcilerTask{
			logger: reconciler.Logger,
			client: reconciler.Client,
			object: cr,
		}
	})

	It("returns false when error fetching NodePool", func() {
		rt := task.waitForNodePoolProvision(ctx, np)
		Expect(rt).To(Equal(false))
	})

	It("returns false when NodePool is not provisioned", func() {
		provisionedCondition := metav1.Condition{
			Type:   "Provisioned",
			Status: metav1.ConditionFalse,
		}
		np.Status.Conditions = append(np.Status.Conditions, provisionedCondition)
		Expect(c.Create(ctx, np)).To(Succeed())

		rt := task.waitForNodePoolProvision(ctx, np)
		Expect(rt).To(Equal(false))
		condition := meta.FindStatusCondition(cr.Status.Conditions, string(utils.CRconditionTypes.HardwareProvisioned))
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
		rt := task.waitForNodePoolProvision(ctx, np)
		Expect(rt).To(Equal(true))
		condition := meta.FindStatusCondition(cr.Status.Conditions, string(utils.CRconditionTypes.HardwareProvisioned))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
	})
})

var _ = Describe("updateClusterInstance", func() {
	var (
		ctx         context.Context
		c           client.Client
		reconciler  *ClusterRequestReconciler
		task        *clusterRequestReconcilerTask
		cr          *oranv1alpha1.ClusterRequest
		ci          *unstructured.Unstructured
		np          *hwv1alpha1.NodePool
		crName      = "cluster-1"
		crNamespace = "clustertemplate-a-v4-16"
		mn          = "master-node"
		wn          = "worker-node"
		mhost       = "node1.test.com"
		whost       = "node2.test.com"
		pns         = "hwmgr"
		masterNode  = createNode(mn, "idrac-virtualmedia+https://10.16.2.1/redfish/v1/Systems/System.Embedded.1", "site-1-master-bmc-secret", "00:00:00:01:20:30", "master", pns, crName)
		workerNode  = createNode(wn, "idrac-virtualmedia+https://10.16.3.4/redfish/v1/Systems/System.Embedded.1", "site-1-worker-bmc-secret", "00:00:00:01:30:10", "worker", pns, crName)
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Define the cluster instance.
		ci = &unstructured.Unstructured{}
		ci.SetName(crName)
		ci.SetNamespace(crNamespace)
		ci.Object = map[string]interface{}{
			"spec": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{"role": "master", "hostName": mhost},
					map[string]interface{}{"role": "worker", "hostName": whost},
				},
			},
		}

		// Define the cluster request.
		cr = &oranv1alpha1.ClusterRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
		}

		// Define the node pool.
		np = &hwv1alpha1.NodePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: pns,
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
		reconciler = &ClusterRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &clusterRequestReconcilerTask{
			logger: reconciler.Logger,
			client: reconciler.Client,
			object: cr,
		}
	})

	It("returns false when failing to get the Node object", func() {
		rt := task.updateClusterInstance(ctx, ci, np)
		Expect(rt).To(Equal(false))
	})

	It("returns true when updateClusterInstance succeeds", func() {
		nodes := []*hwv1alpha1.Node{masterNode, workerNode}
		secrets := createSecrets([]string{masterNode.Status.BMC.CredentialsName, workerNode.Status.BMC.CredentialsName}, pns)

		createResources(c, ctx, nodes, secrets)

		rt := task.updateClusterInstance(ctx, ci, np)
		Expect(rt).To(Equal(true))

		// Define expected details
		expectedDetails := []expectedNodeDetails{
			{
				BMCAddress:         masterNode.Status.BMC.Address,
				BMCCredentialsName: masterNode.Status.BMC.CredentialsName,
				BootMACAddress:     masterNode.Status.BootMACAddress,
			},
			{
				BMCAddress:         workerNode.Status.BMC.Address,
				BMCCredentialsName: workerNode.Status.BMC.CredentialsName,
				BootMACAddress:     workerNode.Status.BootMACAddress,
			},
		}

		// verify the bmc address, secret and boot mac address are set correctly in the cluster instance
		verifyClusterInstance(ci, expectedDetails)

		// verify the host name is set in the node status
		verifyNodeStatus(c, ctx, nodes, mhost, whost)
	})
})

func createNode(name, bmcAddress, bmcSecret, mac, groupName, namespace, npName string) *hwv1alpha1.Node {
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
			BootMACAddress: mac,
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

func createResources(c client.Client, ctx context.Context, nodes []*hwv1alpha1.Node, secrets []*corev1.Secret) {
	for _, node := range nodes {
		Expect(c.Create(ctx, node)).To(Succeed())
	}
	for _, secret := range secrets {
		Expect(c.Create(ctx, secret)).To(Succeed())
	}
}

func verifyClusterInstance(ci *unstructured.Unstructured, expectedDetails []expectedNodeDetails) {
	for i, expected := range expectedDetails {
		node := ci.Object["spec"].(map[string]interface{})["nodes"].([]interface{})[i].(map[string]interface{})
		Expect(node["bmcAddress"]).To(Equal(expected.BMCAddress))
		Expect(node["bmcCredentialsName"].(map[string]interface{})["name"]).To(Equal(expected.BMCCredentialsName))
		Expect(node["bootMACAddress"]).To(Equal(expected.BootMACAddress))
	}
}

func verifyNodeStatus(c client.Client, ctx context.Context, nodes []*hwv1alpha1.Node, mhost, whost string) {
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

var _ = Describe("findPoliciesForClusterRequestsForUpdate", func() {
	var (
		ctx         context.Context
		c           client.Client
		reconciler  *ClusterRequestReconciler
		ctName      = "clustertemplate-a-v1"
		ctNamespace = "clustertemplate-a-v4-16"
		updateEvent *event.UpdateEvent
	)

	BeforeEach(func() {
		// Define the cluster requests.
		crs := []client.Object{
			&oranv1alpha1.ClusterRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-1",
					Namespace: ctNamespace,
				},
				Spec: oranv1alpha1.ClusterRequestSpec{
					ClusterTemplateRef: ctName,
				},
			},
			&oranv1alpha1.ClusterRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-2",
					Namespace: ctNamespace,
				},
				Spec: oranv1alpha1.ClusterRequestSpec{
					ClusterTemplateRef: ctName,
				},
			},
			&oranv1alpha1.ClusterRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-3",
					Namespace: ctNamespace,
				},
				Spec: oranv1alpha1.ClusterRequestSpec{
					ClusterTemplateRef: ctName,
				},
			},
		}

		c = getFakeClientFromObjects(crs...)
		reconciler = &ClusterRequestReconciler{
			Client: c,
			Logger: logger,
		}
	})

	It("It handles updated remediation action for policy for unmatched clusters", func() {
		// Add cluster-request 0.
		clusterRequest0 := &oranv1alpha1.ClusterRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-0",
				Namespace: ctNamespace,
			},
			Spec: oranv1alpha1.ClusterRequestSpec{
				ClusterTemplateRef: ctName,
			},
		}

		Expect(c.Create(ctx, clusterRequest0)).To(Succeed())

		updateEvent = &event.UpdateEvent{
			ObjectOld: &policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "policy",
					Namespace: "ztp-install",
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
					Status: []*policiesv1.CompliancePerClusterStatus{
						{
							ClusterName:      "cluster-0",
							ClusterNamespace: "cluster-0",
							ComplianceState:  policiesv1.Compliant,
						},
						{
							ClusterName:      "cluster-1",
							ClusterNamespace: "cluster-1",
							ComplianceState:  policiesv1.Compliant,
						},
						{
							ClusterName:      "cluster-2",
							ClusterNamespace: "cluster-2",
							ComplianceState:  policiesv1.Compliant,
						},
					},
				},
			},
			ObjectNew: &policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "policy",
					Namespace: "ztp-install",
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "enforce",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
					Status: []*policiesv1.CompliancePerClusterStatus{
						{
							ClusterName:      "cluster-1",
							ClusterNamespace: "cluster-1",
							ComplianceState:  policiesv1.NonCompliant,
						},
						{
							ClusterName:      "cluster-2",
							ClusterNamespace: "cluster-2",
							ComplianceState:  policiesv1.Compliant,
						},
						{
							ClusterName:      "cluster-3",
							ClusterNamespace: "cluster-3",
							ComplianceState:  policiesv1.Compliant,
						},
					},
				},
			},
		}
		queue := workqueue.NewRateLimitingQueueWithConfig(
			workqueue.DefaultControllerRateLimiter(),
			workqueue.RateLimitingQueueConfig{
				Name: "ClusterRequestsQueue",
			})
		reconciler.findPoliciesForClusterRequestsForUpdate(ctx, *updateEvent, queue)
		Expect(queue.Len()).To(Equal(4))

		var items []interface{}
		// Get the first request from the queue.
		item, shutdown := queue.Get()
		Expect(shutdown).To(BeFalse())
		items = append(items, item)
		// Get the 2nd request from the queue.
		item, shutdown = queue.Get()
		Expect(shutdown).To(BeFalse())
		items = append(items, item)
		// Get the 3rd request from the queue.
		item, shutdown = queue.Get()
		Expect(shutdown).To(BeFalse())
		items = append(items, item)
		// Get the 4th request from the queue.
		item, shutdown = queue.Get()
		Expect(shutdown).To(BeFalse())
		items = append(items, item)

		Expect(items).To(ConsistOf(
			reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      "cluster-1",
				Namespace: ctNamespace,
			}},
			reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      "cluster-2",
				Namespace: ctNamespace,
			}},
			reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      "cluster-3",
				Namespace: ctNamespace,
			}},
			reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      "cluster-0",
				Namespace: ctNamespace,
			}},
		))
	})

	It("It handles unchanged remediation action for policy ", func() {
		updateEvent = &event.UpdateEvent{
			ObjectOld: &policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "policy",
					Namespace: "ztp-install",
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
					Status: []*policiesv1.CompliancePerClusterStatus{
						{
							ClusterName:      "cluster-0",
							ClusterNamespace: "cluster-0",
							ComplianceState:  policiesv1.Compliant,
						},
						{
							ClusterName:      "cluster-1",
							ClusterNamespace: "cluster-1",
							ComplianceState:  policiesv1.Compliant,
						},
						{
							ClusterName:      "cluster-2",
							ClusterNamespace: "cluster-2",
							ComplianceState:  policiesv1.Compliant,
						},
					},
				},
			},
			ObjectNew: &policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "policy",
					Namespace: "ztp-install",
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
					Status: []*policiesv1.CompliancePerClusterStatus{
						{
							ClusterName:      "cluster-1",
							ClusterNamespace: "cluster-1",
							ComplianceState:  policiesv1.NonCompliant,
						},
						{
							ClusterName:      "cluster-2",
							ClusterNamespace: "cluster-2",
							ComplianceState:  policiesv1.Compliant,
						},
						{
							ClusterName:      "cluster-3",
							ClusterNamespace: "cluster-3",
							ComplianceState:  policiesv1.Compliant,
						},
					},
				},
			},
		}
		queue := workqueue.NewRateLimitingQueueWithConfig(
			workqueue.DefaultControllerRateLimiter(),
			workqueue.RateLimitingQueueConfig{
				Name: "ClusterRequestsQueue",
			})
		reconciler.findPoliciesForClusterRequestsForUpdate(ctx, *updateEvent, queue)
		Expect(queue.Len()).To(Equal(2))

		var items []interface{}
		// Get the first request from the queue.
		item, shutdown := queue.Get()
		Expect(shutdown).To(BeFalse())
		items = append(items, item)
		// Get the 2nd request from the queue.
		item, shutdown = queue.Get()
		Expect(shutdown).To(BeFalse())
		items = append(items, item)

		Expect(items).To(ConsistOf(
			reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      "cluster-3",
				Namespace: ctNamespace,
			}},
			reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      "cluster-1",
				Namespace: ctNamespace,
			}},
		))
	})

	It("It handles updated remediation action for policy ", func() {
		updateEvent = &event.UpdateEvent{
			ObjectOld: &policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "policy",
					Namespace: "ztp-install",
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
					Status: []*policiesv1.CompliancePerClusterStatus{
						{
							ClusterName:      "cluster-0",
							ClusterNamespace: "cluster-0",
							ComplianceState:  policiesv1.Compliant,
						},
						{
							ClusterName:      "cluster-1",
							ClusterNamespace: "cluster-1",
							ComplianceState:  policiesv1.Compliant,
						},
						{
							ClusterName:      "cluster-2",
							ClusterNamespace: "cluster-2",
							ComplianceState:  policiesv1.Compliant,
						},
					},
				},
			},
			ObjectNew: &policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "policy",
					Namespace: "ztp-install",
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "enforce",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
					Status: []*policiesv1.CompliancePerClusterStatus{
						{
							ClusterName:      "cluster-1",
							ClusterNamespace: "cluster-1",
							ComplianceState:  policiesv1.NonCompliant,
						},
						{
							ClusterName:      "cluster-2",
							ClusterNamespace: "cluster-2",
							ComplianceState:  policiesv1.Compliant,
						},
						{
							ClusterName:      "cluster-3",
							ClusterNamespace: "cluster-3",
							ComplianceState:  policiesv1.Compliant,
						},
					},
				},
			},
		}
		queue := workqueue.NewRateLimitingQueueWithConfig(
			workqueue.DefaultControllerRateLimiter(),
			workqueue.RateLimitingQueueConfig{
				Name: "ClusterRequestsQueue",
			})
		reconciler.findPoliciesForClusterRequestsForUpdate(ctx, *updateEvent, queue)
		Expect(queue.Len()).To(Equal(3))

		var items []interface{}
		// Get the first request from the queue.
		item, shutdown := queue.Get()
		Expect(shutdown).To(BeFalse())
		items = append(items, item)
		// Get the 2nd request from the queue.
		item, shutdown = queue.Get()
		Expect(shutdown).To(BeFalse())
		items = append(items, item)
		// Get the 3rd request from the queue.
		item, shutdown = queue.Get()
		Expect(shutdown).To(BeFalse())
		items = append(items, item)

		Expect(items).To(ConsistOf(
			reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      "cluster-1",
				Namespace: ctNamespace,
			}},
			reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      "cluster-2",
				Namespace: ctNamespace,
			}},
			reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      "cluster-3",
				Namespace: ctNamespace,
			}},
		))
	})
})

var _ = Describe("findPoliciesForClusterRequestsForDelete", func() {
	var (
		ctx         context.Context
		c           client.Client
		reconciler  *ClusterRequestReconciler
		ctName      = "clustertemplate-a-v1"
		ctNamespace = "clustertemplate-a-v4-16"
		deleteEvent *event.DeleteEvent
	)

	BeforeEach(func() {
		// Define the cluster requests.
		crs := []client.Object{
			&oranv1alpha1.ClusterRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-1",
					Namespace: ctNamespace,
				},
				Spec: oranv1alpha1.ClusterRequestSpec{
					ClusterTemplateRef: ctName,
				},
			},
			&oranv1alpha1.ClusterRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-2",
					Namespace: ctNamespace,
				},
				Spec: oranv1alpha1.ClusterRequestSpec{
					ClusterTemplateRef: ctName,
				},
			},
			&oranv1alpha1.ClusterRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-3",
					Namespace: ctNamespace,
				},
				Spec: oranv1alpha1.ClusterRequestSpec{
					ClusterTemplateRef: ctName,
				},
			},
			&oranv1alpha1.ClusterRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-10",
					Namespace: ctNamespace,
				},
				Spec: oranv1alpha1.ClusterRequestSpec{
					ClusterTemplateRef: ctName,
				},
			},
		}

		c = getFakeClientFromObjects(crs...)
		reconciler = &ClusterRequestReconciler{
			Client: c,
			Logger: logger,
		}
	})

	It("It creates reconciliation requests for all clusters that matched the deleted policy", func() {
		deleteEvent = &event.DeleteEvent{
			Object: &policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "policy",
					Namespace: "ztp-install",
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
					Status: []*policiesv1.CompliancePerClusterStatus{
						{
							ClusterName:      "cluster-0",
							ClusterNamespace: "cluster-0",
							ComplianceState:  policiesv1.Compliant,
						},
						{
							ClusterName:      "cluster-1",
							ClusterNamespace: "cluster-1",
							ComplianceState:  policiesv1.Compliant,
						},
						{
							ClusterName:      "cluster-2",
							ClusterNamespace: "cluster-2",
							ComplianceState:  policiesv1.Compliant,
						},
					},
				},
			},
		}
		queue := workqueue.NewRateLimitingQueueWithConfig(
			workqueue.DefaultControllerRateLimiter(),
			workqueue.RateLimitingQueueConfig{
				Name: "ClusterRequestsQueue",
			})
		reconciler.findPoliciesForClusterRequestsForDelete(ctx, *deleteEvent, queue)
		Expect(queue.Len()).To(Equal(2))

		// Get the first request from the queue.
		item, shutdown := queue.Get()
		Expect(shutdown).To(BeFalse())
		Expect(item).To(Equal(
			reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      "cluster-1",
				Namespace: ctNamespace,
			}},
		))

		// Get the 2nd request from the queue.
		item, shutdown = queue.Get()
		Expect(shutdown).To(BeFalse())
		Expect(item).To(Equal(
			reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      "cluster-2",
				Namespace: ctNamespace,
			}},
		))
	})

	It("It creates no reconciliation requests if no clusters match the deleted policy", func() {
		deleteEvent = &event.DeleteEvent{
			Object: &policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "policy",
					Namespace: "ztp-install",
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
					Status: []*policiesv1.CompliancePerClusterStatus{
						{
							ClusterName:      "cluster-0",
							ClusterNamespace: "cluster-0",
							ComplianceState:  policiesv1.Compliant,
						},
						{
							ClusterName:      "cluster-4",
							ClusterNamespace: "cluster-4",
							ComplianceState:  policiesv1.Compliant,
						},
						{
							ClusterName:      "cluster-5",
							ClusterNamespace: "cluster-5",
							ComplianceState:  policiesv1.Compliant,
						},
					},
				},
			},
		}
		queue := workqueue.NewRateLimitingQueueWithConfig(
			workqueue.DefaultControllerRateLimiter(),
			workqueue.RateLimitingQueueConfig{
				Name: "ClusterRequestsQueue",
			})
		reconciler.findPoliciesForClusterRequestsForDelete(ctx, *deleteEvent, queue)
		Expect(queue.Len()).To(Equal(0))
	})
})

var _ = Describe("handleClusterPolicyConfiguration", func() {
	var (
		c           client.Client
		reconciler  *ClusterRequestReconciler
		ctName      = "clustertemplate-a-v1"
		ctNamespace = "clustertemplate-a-v4-16"
		task        *clusterRequestReconcilerTask
		objects     []client.Object
		ctx         context.Context
	)

	BeforeEach(func() {
		// Define the cluster requests.
		cr := &oranv1alpha1.ClusterRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-11",
				Namespace: ctNamespace,
			},
			Spec: oranv1alpha1.ClusterRequestSpec{
				ClusterTemplateRef: ctName,
			},
		}
		objects = []client.Object{
			cr,
			// Needed namespaces.
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ztp-" + ctNamespace,
				},
			},
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ztp-test",
				},
			},
			// ACM policies.
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "v1-subscriptions-policy",
					Namespace: "ztp-clustertemplate-a-v4-16",
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
					Status: []*policiesv1.CompliancePerClusterStatus{
						{
							ClusterName:      "cluster-0",
							ClusterNamespace: "cluster-0",
							ComplianceState:  policiesv1.NonCompliant,
						},
						{
							ClusterName:      "cluster-11",
							ClusterNamespace: "cluster-1",
							ComplianceState:  policiesv1.Compliant,
						},
						{
							ClusterName:      "cluster-12",
							ClusterNamespace: "cluster-2",
							ComplianceState:  policiesv1.Compliant,
						},
					},
				},
			},
			&policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "v1-sriov-configuration-policy",
					Namespace: "ztp-clustertemplate-a-v4-16",
				},
				Spec: policiesv1.PolicySpec{
					RemediationAction: "inform",
				},
				Status: policiesv1.PolicyStatus{
					ComplianceState: "Compliant",
					Status: []*policiesv1.CompliancePerClusterStatus{
						{
							ClusterName:      "cluster-0",
							ClusterNamespace: "cluster-0",
							ComplianceState:  policiesv1.NonCompliant,
						},
						{
							ClusterName:      "cluster-11",
							ClusterNamespace: "cluster-1",
							ComplianceState:  policiesv1.Compliant,
						},
						{
							ClusterName:      "cluster-12",
							ClusterNamespace: "cluster-2",
							ComplianceState:  policiesv1.Compliant,
						},
					},
				},
			},
		}

		ctx = context.Background()
		c = getFakeClientFromObjects(objects...)
		reconciler = &ClusterRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &clusterRequestReconcilerTask{
			logger: reconciler.Logger,
			client: reconciler.Client,
			object: cr,
		}
	})

	It("Updates ClusterRequest ConfigurationApplied condition to OutOfDate when the cluster is "+
		"NonCompliant with at least one matched policies and the policy is not in enforce", func() {

		// Create a new policy: v1-new-policy.
		newPolicy := &policiesv1.Policy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "v1-new-policy",
				Namespace: "ztp-test",
			},
			Spec: policiesv1.PolicySpec{
				RemediationAction: "inform",
			},
			Status: policiesv1.PolicyStatus{
				ComplianceState: "NonCompliant",
				Status: []*policiesv1.CompliancePerClusterStatus{
					{
						ClusterName:      "cluster-0",
						ClusterNamespace: "cluster-0",
						ComplianceState:  policiesv1.Compliant,
					},
					{
						ClusterName:      "cluster-11",
						ClusterNamespace: "cluster-1",
						ComplianceState:  policiesv1.NonCompliant,
					},
					{
						ClusterName:      "cluster-12",
						ClusterNamespace: "cluster-2",
						ComplianceState:  policiesv1.Compliant,
					},
				},
			},
		}
		Expect(c.Create(ctx, newPolicy)).To(Succeed())
		err := task.handleClusterPolicyConfiguration(context.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(task.object.Status.Policies).To(ConsistOf(
			[]oranv1alpha1.PolicyDetails{
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
					RemediationAction: "inform",
				},
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v1-new-policy",
					PolicyNamespace:   "ztp-test",
					RemediationAction: "inform",
				},
			},
		))

		// Check the status conditions.
		conditions := task.object.Status.Conditions
		Expect(conditions[0].Type).To(Equal(string(utils.CRconditionTypes.ConfigurationApplied)))
		Expect(conditions[0].Status).To(Equal(metav1.ConditionFalse))
		Expect(conditions[0].Reason).To(Equal(string(utils.CRconditionReasons.OutOfDate)))
		Expect(conditions[0].Message).To(Equal("The configuration is out of date"))
	})

	It("Updates ClusterRequest ConfigurationApplied condition to Completed when the cluster is "+
		"Compliant with all the matched policies", func() {

		err := task.handleClusterPolicyConfiguration(context.Background())
		Expect(err).ToNot(HaveOccurred())

		Expect(task.object.Status.Policies).To(ConsistOf(
			[]oranv1alpha1.PolicyDetails{
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
					RemediationAction: "inform",
				},
			},
		))

		// Check the status conditions.
		conditions := task.object.Status.Conditions
		Expect(conditions[0].Type).To(Equal(string(utils.CRconditionTypes.ConfigurationApplied)))
		Expect(conditions[0].Status).To(Equal(metav1.ConditionTrue))
		Expect(conditions[0].Reason).To(Equal(string(utils.CRconditionReasons.Completed)))
		Expect(conditions[0].Message).To(Equal("The configuration is up to date"))
	})

	It("Updates ClusterRequest ConfigurationApplied condition to InProgress when the cluster is "+
		"NonCompliant with at least one enforce policy", func() {
		// Create a new policy: v1-new-policy.
		newPolicy := &policiesv1.Policy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "v1-new-policy",
				Namespace: "ztp-test",
			},
			Spec: policiesv1.PolicySpec{
				RemediationAction: "enforce",
			},
			Status: policiesv1.PolicyStatus{
				ComplianceState: "NonCompliant",
				Status: []*policiesv1.CompliancePerClusterStatus{
					{
						ClusterName:      "cluster-0",
						ClusterNamespace: "cluster-0",
						ComplianceState:  policiesv1.Compliant,
					},
					{
						ClusterName:      "cluster-11",
						ClusterNamespace: "cluster-1",
						ComplianceState:  policiesv1.NonCompliant,
					},
					{
						ClusterName:      "cluster-12",
						ClusterNamespace: "cluster-2",
						ComplianceState:  policiesv1.Compliant,
					},
				},
			},
		}
		Expect(c.Create(ctx, newPolicy)).To(Succeed())

		err := task.handleClusterPolicyConfiguration(context.Background())
		Expect(err).ToNot(HaveOccurred())

		Expect(task.object.Status.Policies).To(ConsistOf(
			[]oranv1alpha1.PolicyDetails{
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
					RemediationAction: "inform",
				},
				{
					Compliant:         "NonCompliant",
					PolicyName:        "v1-new-policy",
					PolicyNamespace:   "ztp-test",
					RemediationAction: "enforce",
				},
			},
		))

		// Check the status conditions.
		conditions := task.object.Status.Conditions
		Expect(conditions[0].Type).To(Equal(string(utils.CRconditionTypes.ConfigurationApplied)))
		Expect(conditions[0].Status).To(Equal(metav1.ConditionFalse))
		Expect(conditions[0].Reason).To(Equal(string(utils.CRconditionReasons.InProgress)))
		Expect(conditions[0].Message).To(Equal("The configuration is still being applied"))
	})
})
