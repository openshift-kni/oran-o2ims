package controllers

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
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
		ctName       = "cluster-template-a-v1"
		ctNamespace  = "cluster-template-a"
		ciDefaultsCm = "clusterinstance-defaults-v1"
		ptDefaultsCm = "policytemplate-defaults-v1"
	)

	BeforeEach(func() {
		ctx = context.Background()

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
					// APIserver has enforced the validation for this field who holds
					// the arbirary JSON data
					ClusterInstanceSchema: runtime.RawExtension{},
				},
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
		}
		for _, cm := range cms {
			Expect(c.Create(ctx, cm)).To(Succeed())
		}

		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      ctName,
				Namespace: ctNamespace,
			},
		}

		result, err := reconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to not requeue on valid cluster template
		Expect(result.Requeue).To(BeFalse())

		// Check the status condition
		updatedCT := &oranv1alpha1.ClusterTemplate{}
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
				Name:      ctName,
				Namespace: ctNamespace,
			},
		}

		result, err := reconciler.Reconcile(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		// Expect to requeue invalid cluster template
		Expect(result).To(Equal(requeueWithLongInterval()))

		// Check the status condition
		updatedCT := &oranv1alpha1.ClusterTemplate{}
		Expect(c.Get(ctx, req.NamespacedName, updatedCT)).To(Succeed())
		conditions := updatedCT.Status.Conditions
		Expect(conditions).To(HaveLen(1))
		Expect(conditions[0].Type).To(Equal(string(utils.CTconditionTypes.Validated)))
		Expect(conditions[0].Status).To(Equal(metav1.ConditionFalse))
		Expect(conditions[0].Reason).To(Equal(string(utils.CTconditionReasons.Failed)))
		Expect(conditions[0].Message).To(ContainSubstring(fmt.Sprintf(
			"The referenced ConfigMap %s is not found in the namespace %s", ciDefaultsCm, ctNamespace)))
		Expect(conditions[0].Message).To(ContainSubstring(fmt.Sprintf(
			"The referenced ConfigMap %s is not found in the namespace %s", ptDefaultsCm, ctNamespace)))
	})
})

var _ = Describe("enqueueClusterTemplatesForConfigmap", func() {
	var (
		c            client.Client
		ctx          context.Context
		r            *ClusterTemplateReconciler
		cm           *corev1.ConfigMap
		ciDefaultsCm = "clusterinstance-defaults-v1"
		cts          []*oranv1alpha1.ClusterTemplate
	)

	BeforeEach(func() {
		ctx = context.Background()
		cts = []*oranv1alpha1.ClusterTemplate{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-template-a-v1",
					Namespace: "cluster-template-a",
				},
				Spec: oranv1alpha1.ClusterTemplateSpec{
					Templates: oranv1alpha1.Templates{},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-template-a-v2",
					Namespace: "cluster-template-a",
				},
				Spec: oranv1alpha1.ClusterTemplateSpec{
					Templates: oranv1alpha1.Templates{},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-template-b-v1",
					Namespace: "cluster-template-b",
				},
				Spec: oranv1alpha1.ClusterTemplateSpec{
					Templates: oranv1alpha1.Templates{},
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
			reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-template-a-v1", Namespace: "cluster-template-a"}},
			reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster-template-a-v2", Namespace: "cluster-template-a"}},
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
		ctName       = "cluster-template-a-v1"
		ctNamespace  = "cluster-template-a"
		ciDefaultsCm = "clusterinstance-ci-defaults"
		ptDefaultsCm = "policytemplate-ci-defaults"
		t            *clusterTemplateReconcilerTask
	)

	BeforeEach(func() {
		ctx = context.Background()
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
		// Create valid ConfigMaps
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
		}
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
			"The referenced ConfigMap %s is not found in the namespace %s", ciDefaultsCm, ctNamespace)))
		Expect(conditions[0].Message).To(ContainSubstring(fmt.Sprintf(
			"The referenced ConfigMap %s is not found in the namespace %s", ptDefaultsCm, ctNamespace)))
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
				utils.ClusterInstanceTemplateDefaultsConfigmapKey: `
key: value`,
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())
		validationErr, err := validateConfigmapReference(
			ctx, c, configmapName, namespace, utils.ClusterInstanceTemplateDefaultsConfigmapKey)
		Expect(err).ToNot(HaveOccurred())
		Expect(validationErr).To(BeEmpty())
	})

	It("should return validation error message for a missing configmap", func() {
		// No ConfigMap created
		validationErr, err := validateConfigmapReference(
			ctx, c, configmapName, namespace, utils.ClusterInstanceTemplateDefaultsConfigmapKey)
		Expect(err).ToNot(HaveOccurred())
		Expect(validationErr).To(Equal(fmt.Sprintf(
			"The referenced ConfigMap %s is not found in the namespace %s", configmapName, namespace)))
	})

	It("should return validation error message for missing expected key in configmap", func() {
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

		validationErr, err := validateConfigmapReference(
			ctx, c, configmapName, namespace, utils.ClusterInstanceTemplateDefaultsConfigmapKey)
		Expect(err).ToNot(HaveOccurred())
		Expect(validationErr).To(Equal(fmt.Sprintf(
			"the expected key %s does not exist in the ConfigMap %s data", utils.ClusterInstanceTemplateDefaultsConfigmapKey, configmapName)))
	})

	It("should return validation error message for invalid YAML in configmap data", func() {
		// Create a ConfigMap with invalid YAML
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

		validationErr, err := validateConfigmapReference(
			ctx, c, configmapName, namespace, utils.ClusterInstanceTemplateDefaultsConfigmapKey)
		Expect(err).ToNot(HaveOccurred())
		Expect(validationErr).To(ContainSubstring("the value of key"))
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

		validationErr, err := validateConfigmapReference(
			ctx, c, configmapName, namespace, utils.ClusterInstanceTemplateDefaultsConfigmapKey)
		Expect(err).ToNot(HaveOccurred())
		Expect(validationErr).To(Equal(fmt.Sprintf(
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

		validationErr, err := validateConfigmapReference(
			ctx, c, configmapName, namespace, utils.ClusterInstanceTemplateDefaultsConfigmapKey)
		Expect(err).ToNot(HaveOccurred())
		Expect(validationErr).To(BeEmpty())

		// Verify that the configmap is patched to be immutable
		updatedCM := &corev1.ConfigMap{}
		Expect(c.Get(ctx, client.ObjectKey{Name: configmapName, Namespace: namespace}, updatedCM)).To(Succeed())
		Expect(updatedCM.Immutable).ToNot(BeNil())
		Expect(*updatedCM.Immutable).To(BeTrue())
	})
})
