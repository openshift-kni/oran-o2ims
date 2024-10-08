package controllers

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("ClusterTemplateReconciler", func() {
	var (
		ctx          context.Context
		c            client.Client
		reconciler   *ClusterTemplateReconciler
		tName        = "cluster-template-a"
		tVersion     = "v1.0.0"
		ctNamespace  = "cluster-template-a"
		ciDefaultsCm = "clusterinstance-defaults-v1"
		ptDefaultsCm = "policytemplate-defaults-v1"
		hwTemplateCm = "hwTemplate-v1"
	)

	BeforeEach(func() {
		ctx = context.Background()
		ct := &provisioningv1alpha1.ClusterTemplate{
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
		}

		c = getFakeClientFromObjects([]client.Object{ct}...)
		reconciler = &ClusterTemplateReconciler{
			Client: c,
			Logger: logger,
		}
	})

	It("should not requeue a valid ClusterTemplate", func() {
		// Create valid ConfigMaps and ClusterTemplate
		cms := []*corev1.ConfigMap{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ciDefaultsCm,
					Namespace: ctNamespace,
				},
				Data: map[string]string{
					utils.ClusterInstanceTemplateDefaultsConfigmapKey: `
key: value`,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ptDefaultsCm,
					Namespace: ctNamespace,
				},
				Data: map[string]string{
					utils.PolicyTemplateDefaultsConfigmapKey: `
clustertemplate-a-policy-v1-cpu-isolated: "2-31"
clustertemplate-a-policy-v1-cpu-reserved: "0-1"
clustertemplate-a-policy-v1-defaultHugepagesSize: "1G"`,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      hwTemplateCm,
					Namespace: utils.InventoryNamespace,
				},
				Data: map[string]string{
					utils.HwTemplateNodePool: `
- name: master
  hwProfile: profile-spr-single-processor-64G
- name: worker
  hwProfile: profile-spr-dual-processor-128G`,
				},
			},
		}
		for _, cm := range cms {
			Expect(c.Create(ctx, cm)).To(Succeed())
		}

		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      getClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
		}

		result, err := reconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid cluster template
		Expect(result.Requeue).To(BeFalse())

		// Check the status condition
		updatedCT := &provisioningv1alpha1.ClusterTemplate{}
		Expect(c.Get(ctx, req.NamespacedName, updatedCT)).To(Succeed())
		conditions := updatedCT.Status.Conditions
		Expect(conditions).To(HaveLen(1))
		Expect(conditions[0].Type).To(Equal(string(utils.CTconditionTypes.Validated)))
		Expect(conditions[0].Status).To(Equal(metav1.ConditionTrue))
		Expect(conditions[0].Reason).To(Equal(string(utils.CTconditionReasons.Completed)))
		Expect(conditions[0].Message).To(Equal("The cluster template validation succeeded"))
	})

	It("should requeue an invalid ClusterTemplate", func() {
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      getClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
		}

		result, err := reconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to requeue invalid cluster template
		Expect(result).To(Equal(requeueWithLongInterval()))

		// Check the status condition
		updatedCT := &provisioningv1alpha1.ClusterTemplate{}
		Expect(c.Get(ctx, req.NamespacedName, updatedCT)).To(Succeed())
		conditions := updatedCT.Status.Conditions
		Expect(conditions).To(HaveLen(1))
		Expect(conditions[0].Type).To(Equal(string(utils.CTconditionTypes.Validated)))
		Expect(conditions[0].Status).To(Equal(metav1.ConditionFalse))
		Expect(conditions[0].Reason).To(Equal(string(utils.CTconditionReasons.Failed)))
		Expect(conditions[0].Message).To(ContainSubstring(fmt.Sprintf(
			"the ConfigMap '%s' is not found in the namespace '%s'", ciDefaultsCm, ctNamespace)))
		Expect(conditions[0].Message).To(ContainSubstring(fmt.Sprintf(
			"the ConfigMap '%s' is not found in the namespace '%s'", ptDefaultsCm, ctNamespace)))
	})
})

var _ = Describe("enqueueClusterTemplatesForConfigmap", func() {
	var (
		c            client.Client
		ctx          context.Context
		r            *ClusterTemplateReconciler
		cm           *corev1.ConfigMap
		ciDefaultsCm = "clusterinstance-defaults-v1"
		cts          []*provisioningv1alpha1.ClusterTemplate
	)

	BeforeEach(func() {
		ctx = context.Background()
		cts = []*provisioningv1alpha1.ClusterTemplate{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-template-a.v1",
					Namespace: "cluster-template-a",
				},
				Spec: provisioningv1alpha1.ClusterTemplateSpec{
					Name:      "cluster-template-a",
					Version:   "v1",
					Templates: provisioningv1alpha1.Templates{},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-template-a.v2",
					Namespace: "cluster-template-a",
				},
				Spec: provisioningv1alpha1.ClusterTemplateSpec{
					Name:      "cluster-template-a",
					Version:   "v2",
					Templates: provisioningv1alpha1.Templates{},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-template-b.v1",
					Namespace: "cluster-template-b",
				},
				Spec: provisioningv1alpha1.ClusterTemplateSpec{
					Name:      "cluster-template-b",
					Version:   "v1",
					Templates: provisioningv1alpha1.Templates{},
				},
			},
		}

		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ciDefaultsCm,
				Namespace: "cluster-template-a",
			},
		}

		objs := []client.Object{cm}
		for _, ct := range cts {
			objs = append(objs, ct)
		}
		c = getFakeClientFromObjects(objs...)
		r = &ClusterTemplateReconciler{
			Client: c,
			Logger: logger,
		}
	})

	It("should enqueue ClusterTemplates referencing the clusterinstance defaults ConfigMap", func() {
		cts[0].Spec.Templates.ClusterInstanceDefaults = ciDefaultsCm
		cts[1].Spec.Templates.ClusterInstanceDefaults = ciDefaultsCm
		cts[2].Spec.Templates.ClusterInstanceDefaults = ciDefaultsCm

		Expect(c.Update(ctx, cts[0])).To(Succeed())
		Expect(c.Update(ctx, cts[1])).To(Succeed())
		Expect(c.Update(ctx, cts[2])).To(Succeed())

		// Call the function
		reqs := r.enqueueClusterTemplatesForConfigmap(ctx, cm)

		// Verify the result
		Expect(reqs).To(HaveLen(2))
		Expect(reqs).To(ContainElements(
			reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-template-a.v1", Namespace: "cluster-template-a"}},
			reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-template-a.v2", Namespace: "cluster-template-a"}},
		))
	})

	It("should not enqueue ClusterTemplates not referencing the clusterinstance defaults ConfigMap", func() {
		cts[0].Spec.Templates.ClusterInstanceDefaults = "clusterinstance-defaults-v2"
		cts[1].Spec.Templates.ClusterInstanceDefaults = "clusterinstance-defaults-v2"
		cts[2].Spec.Templates.ClusterInstanceDefaults = ciDefaultsCm

		Expect(c.Update(ctx, cts[0])).To(Succeed())
		Expect(c.Update(ctx, cts[1])).To(Succeed())
		Expect(c.Update(ctx, cts[2])).To(Succeed())

		// Call the function
		reqs := r.enqueueClusterTemplatesForConfigmap(ctx, cm)

		// Verify the result
		Expect(reqs).To(HaveLen(0))
	})
})

var _ = Describe("validateClusterTemplateCR", func() {
	var (
		c            client.Client
		ctx          context.Context
		cms          []*corev1.ConfigMap
		tName        = "cluster-template-a"
		tVersion     = "v1.0.0"
		ctNamespace  = "cluster-template-a"
		ciDefaultsCm = "clusterinstance-ci-defaults"
		ptDefaultsCm = "policytemplate-ci-defaults"
		hwTemplateCm = "hwTemplate-v1"
		t            *clusterTemplateReconcilerTask
	)

	BeforeEach(func() {
		ctx = context.Background()
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:    tName,
				Version: tVersion,
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
					HwTemplate:              hwTemplateCm,
				},
			},
		}

		// Valid ConfigMaps
		cms = []*corev1.ConfigMap{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ciDefaultsCm,
					Namespace: ctNamespace,
				},
				Data: map[string]string{
					utils.ClusterProvisioningTimeoutConfigKey: "80",
					utils.ClusterInstanceTemplateDefaultsConfigmapKey: `
key: value`,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ptDefaultsCm,
					Namespace: ctNamespace,
				},
				Data: map[string]string{
					utils.ClusterConfigurationTimeoutConfigKey: "40",
					utils.PolicyTemplateDefaultsConfigmapKey: `
clustertemplate-a-policy-v1-cpu-isolated: "2-31"
clustertemplate-a-policy-v1-cpu-reserved: "0-1"
clustertemplate-a-policy-v1-defaultHugepagesSize: "1G"`,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      hwTemplateCm,
					Namespace: utils.InventoryNamespace,
				},
				Data: map[string]string{
					utils.HwTemplateNodePool: `
- name: master
  hwProfile: profile-spr-single-processor-64G
- name: worker
  hwProfile: profile-spr-dual-processor-128G`,
				},
			},
		}

		c = getFakeClientFromObjects([]client.Object{ct}...)
		t = &clusterTemplateReconcilerTask{
			client: c,
			logger: logger,
			object: ct,
		}
	})

	It("should validate a valid ClusterTemplate and set status condition to true", func() {
		for _, cm := range cms {
			Expect(c.Create(ctx, cm)).To(Succeed())
		}

		valid, err := t.validateClusterTemplateCR(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(valid).To(BeTrue())

		// Check the status condition
		conditions := t.object.Status.Conditions
		Expect(conditions).To(HaveLen(1))
		Expect(conditions[0].Type).To(Equal(string(utils.CTconditionTypes.Validated)))
		Expect(conditions[0].Status).To(Equal(metav1.ConditionTrue))
		Expect(conditions[0].Reason).To(Equal(string(utils.CTconditionReasons.Completed)))
		Expect(conditions[0].Message).To(Equal("The cluster template validation succeeded"))
	})

	It("should return false and set status condition to false if reference ConfigMap is missing", func() {
		// No ConfigMap created
		valid, err := t.validateClusterTemplateCR(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(valid).To(BeFalse())

		// Check the status condition
		conditions := t.object.Status.Conditions
		Expect(conditions).To(HaveLen(1))
		Expect(conditions[0].Type).To(Equal(string(utils.CTconditionTypes.Validated)))
		Expect(conditions[0].Status).To(Equal(metav1.ConditionFalse))
		Expect(conditions[0].Reason).To(Equal(string(utils.CTconditionReasons.Failed)))
		Expect(conditions[0].Message).To(ContainSubstring(fmt.Sprintf(
			"the ConfigMap '%s' is not found in the namespace '%s'", ciDefaultsCm, ctNamespace)))
		Expect(conditions[0].Message).To(ContainSubstring(fmt.Sprintf(
			"the ConfigMap '%s' is not found in the namespace '%s'", ptDefaultsCm, ctNamespace)))
	})

	It("should return false and set status condition to false if timeouts in ConfigMaps are invalid", func() {
		cms[0].Data[utils.ClusterProvisioningTimeoutConfigKey] = "invalidCiTimeout"
		cms[1].Data[utils.ClusterConfigurationTimeoutConfigKey] = "invalidPtTimeout"
		cms[2].Data[utils.HardwareProvisioningTimeoutConfigKey] = "invalidHwTimeout"
		for _, cm := range cms {
			Expect(c.Create(ctx, cm)).To(Succeed())
		}

		valid, err := t.validateClusterTemplateCR(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(valid).To(BeFalse())

		// Check the status condition
		conditions := t.object.Status.Conditions
		Expect(conditions).To(HaveLen(1))
		Expect(conditions[0].Type).To(Equal(string(utils.CTconditionTypes.Validated)))
		Expect(conditions[0].Status).To(Equal(metav1.ConditionFalse))
		Expect(conditions[0].Reason).To(Equal(string(utils.CTconditionReasons.Failed)))
		Expect(conditions[0].Message).To(ContainSubstring(fmt.Sprintf(
			"the value of key %s from ConfigMap %s is not an integer", utils.HardwareProvisioningTimeoutConfigKey, hwTemplateCm)))
		Expect(conditions[0].Message).To(ContainSubstring(fmt.Sprintf(
			"the value of key %s from ConfigMap %s is not an integer", utils.ClusterConfigurationTimeoutConfigKey, ptDefaultsCm)))
		Expect(conditions[0].Message).To(ContainSubstring(fmt.Sprintf(
			"the value of key %s from ConfigMap %s is not an integer", utils.ClusterProvisioningTimeoutConfigKey, ciDefaultsCm)))
	})
})

var _ = Describe("validateConfigmapReference", func() {
	var (
		c             client.Client
		ctx           context.Context
		namespace     = "default"
		configmapName = "test-configmap"
	)

	BeforeEach(func() {
		ctx = context.Background()
		c = getFakeClientFromObjects()
	})

	It("should validate a valid configmap", func() {
		// Create a valid ConfigMap
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: namespace,
			},
			Data: map[string]string{
				utils.ClusterProvisioningTimeoutConfigKey: "40",
				utils.ClusterInstanceTemplateDefaultsConfigmapKey: `
key: value`,
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())
		err := validateConfigmapReference[map[string]any](
			ctx, c, configmapName, namespace,
			utils.ClusterInstanceTemplateDefaultsConfigmapKey,
			utils.ClusterProvisioningTimeoutConfigKey)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should return validation error message for a missing configmap", func() {
		// No ConfigMap created
		err := validateConfigmapReference[map[string]any](
			ctx, c, configmapName, namespace,
			utils.ClusterInstanceTemplateDefaultsConfigmapKey,
			utils.ClusterProvisioningTimeoutConfigKey)
		Expect(err).To(HaveOccurred())
		Expect(utils.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(Equal(fmt.Sprintf(
			"failed to get ConfigmapReference: the ConfigMap '%s' is not found in the namespace '%s'", configmapName, namespace)))
	})

	It("should return validation error message for missing template data key in configmap", func() {
		// Create a ConfigMap without the expected key
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: namespace,
			},
			Data: map[string]string{
				"wrong-key": "value",
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		err := validateConfigmapReference[map[string]any](
			ctx, c, configmapName, namespace,
			utils.ClusterInstanceTemplateDefaultsConfigmapKey,
			utils.ClusterProvisioningTimeoutConfigKey)
		Expect(err).To(HaveOccurred())
		Expect(utils.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(Equal(fmt.Sprintf(
			"the ConfigMap '%s' does not contain a field named '%s'", configmapName, utils.ClusterInstanceTemplateDefaultsConfigmapKey)))
	})

	It("should return validation error message for invalid YAML in configmap template data", func() {
		// Create a ConfigMap with invalid data YAML
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: namespace,
			},
			Data: map[string]string{
				utils.ClusterInstanceTemplateDefaultsConfigmapKey: `invalid-yaml`,
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		err := validateConfigmapReference[map[string]any](
			ctx, c, configmapName, namespace,
			utils.ClusterInstanceTemplateDefaultsConfigmapKey,
			utils.ClusterProvisioningTimeoutConfigKey)
		Expect(err).To(HaveOccurred())
		Expect(utils.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("the value of key"))
	})

	It("should return validation error message for invalid timeout value in configmap", func() {
		// Create a ConfigMap with non-integer string
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: namespace,
			},
			Data: map[string]string{
				utils.ClusterProvisioningTimeoutConfigKey: "invalid-timeout",
				utils.ClusterInstanceTemplateDefaultsConfigmapKey: `
key: value`,
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		err := validateConfigmapReference[map[string]any](
			ctx, c, configmapName, namespace,
			utils.ClusterInstanceTemplateDefaultsConfigmapKey,
			utils.ClusterProvisioningTimeoutConfigKey)
		Expect(err).To(HaveOccurred())
		Expect(utils.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("is not an integer"))
	})

	It("should return validation error message if configmap is mutable", func() {
		// Create a mutable ConfigMap
		mutable := false
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: namespace,
			},
			Data: map[string]string{
				utils.ClusterInstanceTemplateDefaultsConfigmapKey: `
key: value`,
			},
			Immutable: &mutable,
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		err := validateConfigmapReference[map[string]any](
			ctx, c, configmapName, namespace,
			utils.ClusterInstanceTemplateDefaultsConfigmapKey,
			utils.ClusterProvisioningTimeoutConfigKey)
		Expect(err).To(HaveOccurred())
		Expect(utils.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(Equal(fmt.Sprintf(
			"It is not allowed to set Immutable to false in the ConfigMap %s", configmapName)))
	})

	It("should patch a validated configmap to immutable if not set", func() {
		// Create a ConfigMap without immutable field
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: namespace,
			},
			Data: map[string]string{
				utils.ClusterInstanceTemplateDefaultsConfigmapKey: `
key: value`,
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		err := validateConfigmapReference[map[string]any](
			ctx, c, configmapName, namespace,
			utils.ClusterInstanceTemplateDefaultsConfigmapKey,
			utils.ClusterProvisioningTimeoutConfigKey)
		Expect(err).ToNot(HaveOccurred())

		// Verify that the configmap is patched to be immutable
		updatedCM := &corev1.ConfigMap{}
		Expect(c.Get(ctx, client.ObjectKey{Name: configmapName, Namespace: namespace}, updatedCM)).To(Succeed())
		Expect(updatedCM.Immutable).ToNot(BeNil())
		Expect(*updatedCM.Immutable).To(BeTrue())
	})
})

var _ = Describe("Validate Cluster Instance Name", func() {
	var (
		c            client.Client
		ctx          context.Context
		tName        = "cluster-template-a"
		tVersion     = "v1.0.0"
		ctNamespace  = "cluster-template-a"
		ciDefaultsCm = "clusterinstance-ci-defaults"
		ptDefaultsCm = "policytemplate-ci-defaults"
		hwTemplateCm = "hwTemplate-v1"
	)

	BeforeEach(func() {
		ctx = context.Background()
		c = getFakeClientFromObjects()
	})

	It("should validate a cluster template name", func() {
		// Create a valid cluster template
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:    tName,
				Version: tVersion,
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
					HwTemplate:              hwTemplateCm,
				},
			},
		}
		Expect(c.Create(ctx, ct)).To(Succeed())
		err := validateName(
			c, "myClusterInstance", "v11", "myClusterInstance.v11", "namespace1")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should validate a cluster template name, if a cluster template with a different name exists", func() {
		// Create a valid cluster template
		ct1 := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:    tName,
				Version: tVersion,
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
					HwTemplate:              hwTemplateCm,
				},
			},
		}
		ct2 := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getClusterTemplateRefName(tName, tVersion),
				Namespace: "namespace1",
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:    tName,
				Version: tVersion,
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
					HwTemplate:              hwTemplateCm,
				},
			},
		}
		Expect(c.Create(ctx, ct1)).To(Succeed())
		Expect(c.Create(ctx, ct2)).To(Succeed())
		err := validateName(
			c, "myClusterInstance", "v11", "myClusterInstance.v11", "namespace1")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should fail cluster template validation if metadata name is incorrect", func() {
		err := validateName(
			c, "cluster-template-a", "v1", "cluster-template-a.v1", "cluster-template-a")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should fail cluster template validation if a cluster template with a same name"+
		" but in a different namespace exists", func() {
		// Create a valid cluster template
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:    tName,
				Version: tVersion,
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
					HwTemplate:              hwTemplateCm,
				},
			},
		}
		Expect(c.Create(ctx, ct)).To(Succeed())
		err := validateName(
			c, "cluster-template-a", "v1.0.0", "cluster-template-a.v1.0.0", "cluster-template-b")
		Expect(err).To(HaveOccurred())
	})
})

// new
var _ = Describe("Validate Cluster Instance TemplateID", func() {
	var (
		c            client.Client
		ctx          context.Context
		tName        = "cluster-template-a"
		tVersion     = "v1.0.0"
		ctNamespace  = "cluster-template-a"
		ciDefaultsCm = "clusterinstance-ci-defaults"
		ptDefaultsCm = "policytemplate-ci-defaults"
		hwTemplateCm = "hwTemplate-v1"
	)

	BeforeEach(func() {
		ctx = context.Background()
		c = getFakeClientFromObjects()
	})

	It("Generate templateID", func() {
		// Create a valid cluster template
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:       tName,
				Version:    tVersion,
				TemplateID: "",
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
					HwTemplate:              hwTemplateCm,
				},
			},
		}
		Expect(c.Create(ctx, ct)).To(Succeed())
		err := generateTemplateID(ctx, c, ct)
		Expect(err).ToNot(HaveOccurred())
		ct1 := &provisioningv1alpha1.ClusterTemplate{}
		err = c.Get(ctx, client.ObjectKeyFromObject(ct), ct1)
		Expect(err).ToNot(HaveOccurred())
		Expect(ct1.Spec.TemplateID).NotTo(Equal(""))
	})
	It("should validate templateID if is not empty, bad UUID", func() {
		// Create a valid cluster template
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:       tName,
				Version:    tVersion,
				TemplateID: "kjwchbjkdbckj",
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
					HwTemplate:              hwTemplateCm,
				},
			},
		}
		Expect(c.Create(ctx, ct)).To(Succeed())
		err := validateTemplateID(ct)
		Expect(err).To(HaveOccurred())
	})
	It("should validate templateID if is not empty, good UUID", func() {
		// Create a valid cluster template
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:       tName,
				Version:    tVersion,
				TemplateID: "71ba1920-77f8-4842-a474-010b1af1d40b",
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
					HwTemplate:              hwTemplateCm,
				},
			},
		}
		Expect(c.Create(ctx, ct)).To(Succeed())
		err := validateTemplateID(ct)
		Expect(err).ToNot(HaveOccurred())
	})
})
