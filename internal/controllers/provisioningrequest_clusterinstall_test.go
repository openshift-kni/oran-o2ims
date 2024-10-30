package controllers

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

var _ = Describe("handleRenderClusterInstance", func() {
	var (
		ctx          context.Context
		c            client.Client
		reconciler   *ProvisioningRequestReconciler
		task         *provisioningRequestReconcilerTask
		cr           *provisioningv1alpha1.ProvisioningRequest
		ciDefaultsCm = "clusterinstance-defaults-v1"
		tName        = "clustertemplate-a"
		tVersion     = "v1.0.0"
		ctNamespace  = "clustertemplate-a-v4-16"
		crName       = "cluster-1"
	)

	BeforeEach(func() {
		ctx = context.Background()
		// Define the provisioning request.
		cr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: ctNamespace,
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
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
				},
			},
		}
		// Configmap for ClusterInstance defaults
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ciDefaultsCm,
				Namespace: ctNamespace,
			},
			Data: map[string]string{
				utils.ClusterInstanceTemplateDefaultsConfigmapKey: `
clusterImageSetNameRef: "4.15"
pullSecretRef:
  name: "pull-secret"
holdInstallation: false
templateRefs:
  - name: "ai-cluster-templates-v1"
    namespace: "siteconfig-operator"
nodes:
- hostname: "node1"
  ironicInspect: ""
  nodeNetwork:
    interfaces:
    - name: eno1
      label: bootable-interface
    - name: eth0
      label: base-interface
    - name: eth1
      label: data-interface
  templateRefs:
    - name: "ai-node-templates-v1"
      namespace: "siteconfig-operator"`,
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr, ct, cm}...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &provisioningRequestReconcilerTask{
			logger:       reconciler.Logger,
			client:       reconciler.Client,
			object:       cr,
			clusterInput: &clusterInput{},
			ctNamespace:  ctNamespace,
		}

		clusterInstanceInputParams, err := utils.ExtractMatchingInput(
			cr.Spec.TemplateParameters.Raw, utils.TemplateParamClusterInstance)
		Expect(err).ToNot(HaveOccurred())
		mergedClusterInstanceData, err := task.getMergedClusterInputData(
			ctx, ciDefaultsCm, clusterInstanceInputParams.(map[string]any), utils.TemplateParamClusterInstance)
		Expect(err).ToNot(HaveOccurred())
		task.clusterInput.clusterInstanceData = mergedClusterInstanceData
	})

	It("should successfully render and validate ClusterInstance with dry-run", func() {
		renderedClusterInstance, err := task.handleRenderClusterInstance(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(renderedClusterInstance).ToNot(BeNil())

		// Check if status condition was updated correctly
		cond := meta.FindStatusCondition(task.object.Status.Conditions,
			string(utils.PRconditionTypes.ClusterInstanceRendered))
		Expect(cond).ToNot(BeNil())
		verifyStatusCondition(*cond, metav1.Condition{
			Type:    string(utils.PRconditionTypes.ClusterInstanceRendered),
			Status:  metav1.ConditionTrue,
			Reason:  string(utils.CRconditionReasons.Completed),
			Message: "ClusterInstance rendered and passed dry-run validation",
		})
	})

	It("should fail to render ClusterInstance due to invalid input", func() {
		// Modify input data to be invalid
		task.clusterInput.clusterInstanceData["clusterName"] = ""
		_, err := task.handleRenderClusterInstance(ctx)
		Expect(err).To(HaveOccurred())

		// Check if status condition was updated correctly
		cond := meta.FindStatusCondition(task.object.Status.Conditions,
			string(utils.PRconditionTypes.ClusterInstanceRendered))
		Expect(cond).ToNot(BeNil())
		verifyStatusCondition(*cond, metav1.Condition{
			Type:    string(utils.PRconditionTypes.ClusterInstanceRendered),
			Status:  metav1.ConditionFalse,
			Reason:  string(utils.CRconditionReasons.Failed),
			Message: "spec.clusterName cannot be empty",
		})
	})

	It("should detect updates to immutable fields and fail rendering", func() {
		// Simulate that the ClusterInstance has been provisioned
		task.object.Status.Conditions = []metav1.Condition{
			{
				Type:   string(utils.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(utils.CRconditionReasons.Completed),
			},
		}

		oldSpec := make(map[string]any)
		newSpec := make(map[string]any)
		data, err := yaml.Marshal(task.clusterInput.clusterInstanceData)
		Expect(err).ToNot(HaveOccurred())
		Expect(yaml.Unmarshal(data, &oldSpec)).To(Succeed())
		Expect(yaml.Unmarshal(data, &newSpec)).To(Succeed())

		clusterInstanceObj := map[string]any{
			"Cluster": task.clusterInput.clusterInstanceData,
		}
		oldClusterInstance, err := utils.RenderTemplateForK8sCR(
			utils.ClusterInstanceTemplateName, utils.ClusterInstanceTemplatePath, clusterInstanceObj)
		Expect(err).ToNot(HaveOccurred())
		Expect(c.Create(ctx, oldClusterInstance)).To(Succeed())

		// Update the cluster data with modified field
		// Change an immutable field at the cluster-level
		newSpec["baseDomain"] = "newdomain.example.com"
		task.clusterInput.clusterInstanceData = newSpec

		_, err = task.handleRenderClusterInstance(ctx)
		Expect(err).To(HaveOccurred())

		// Note that the detected changed fields in this unittest include nodes.0.ironicInspect, baseDomain,
		// and holdInstallation, even though nodes.0.ironicInspect and holdInstallation were not actually changed.
		// This is due to the difference between the fakeclient and a real cluster. When applying a manifest
		// to a cluster, the API server preserves the full resource, including optional fields with empty values.
		// However, the fakeclient in unittests behaves differently, as it uses an in-memory store and
		// does not go through the API server. As a result, fields with empty values like false or "" are
		// stripped from the retrieved ClusterInstance CR (existing ClusterInstance) in the fakeclient.
		cond := meta.FindStatusCondition(task.object.Status.Conditions,
			string(utils.PRconditionTypes.ClusterInstanceRendered))
		Expect(cond).ToNot(BeNil())
		verifyStatusCondition(*cond, metav1.Condition{
			Type:    string(utils.PRconditionTypes.ClusterInstanceRendered),
			Status:  metav1.ConditionFalse,
			Reason:  string(utils.CRconditionReasons.Failed),
			Message: "Failed to render and validate ClusterInstance: detected changes in immutable fields",
		})
	})
})
