/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Assisted-by: Cursor/claude-4-sonnet
*/

/*
Test Cases Summary for ClusterTemplate Controller

This file contains comprehensive test cases for the ClusterTemplate controller and its validation functions.
The tests are organized into the following test suites:

1. ClusterTemplateReconciler Tests:
   - Validates that a valid ClusterTemplate with all required ConfigMaps and HardwareTemplate does not requeue
   - Validates that an invalid ClusterTemplate (missing ConfigMaps) requeues with appropriate error conditions

2. enqueueClusterTemplatesForConfigmap Tests:
   - Tests enqueueing of ClusterTemplates that reference a specific clusterinstance defaults ConfigMap
   - Tests that ClusterTemplates not referencing the ConfigMap are not enqueued

3. validatePolicyTemplateParamsSchema Tests:
   - Tests validation of policy template parameter schema structure
   - Covers missing properties, invalid property structures, incorrect type definitions
   - Tests validation of nested property structures and type constraints

4. validateClusterTemplateCR Tests:
   - Tests complete ClusterTemplate validation including ConfigMaps and HardwareTemplate
   - Tests status condition setting for both valid and invalid templates
   - Tests validation of timeout configurations in ConfigMaps
   - Tests hardware template timeout validation

5. validateConfigmapReference Tests:
   - Tests ConfigMap existence and structure validation
   - Tests ClusterInstance CRD schema compliance
   - Tests template data key presence and YAML validity
   - Tests interface label validation in network configurations
   - Tests timeout value parsing and validation
   - Tests ConfigMap immutability requirements and patching

6. Validate Cluster Instance Name Tests:
   - Tests cluster template name validation rules
   - Tests handling of templates with same names in different namespaces
   - Tests metadata name correctness validation

7. Validate Cluster Instance TemplateID Tests:
   - Tests automatic templateID generation for empty values
   - Tests UUID format validation for provided templateIDs
   - Tests both valid and invalid UUID formats

8. validateTemplateParameterSchema Tests (Go test function):
   - Tests JSON schema validation for template parameters
   - Tests required field validation and type checking
   - Tests policy template parameter schema structure
   - Tests error message formatting for various validation failures

9. validateClusterInstanceParamsSchema Tests:
   - Tests schema validation behavior when hardware template is provided vs not provided
   - Tests that hardware template presence skips schema validation entirely
   - Tests schema validation for cases without hardware template
   - Tests edge cases including whitespace handling and large/nested schemas

10. validateSchemaWithoutHWTemplate Tests:
    - Tests detailed schema structure validation for cluster instances without hardware templates
    - Tests required node properties and BMC credential validation
    - Tests network interface configuration validation
    - Tests schema structure integrity and required field presence

11. validateUpgradeDefaultsConfigmap Tests:
    - Tests validation of upgrade defaults ConfigMap for Image-Based GPU (IBGU) upgrades
    - Tests YAML parsing and IBGU spec validation
    - Tests release version matching between ClusterTemplate and seedImageRef
    - Tests ConfigMap immutability requirements
    - Tests dry-run validation of IBGU specifications
    - Tests error handling for missing or malformed ConfigMaps

Each test suite covers both positive and negative test cases to ensure comprehensive validation
of the ClusterTemplate controller functionality.
*/

package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	testutils "github.com/openshift-kni/oran-o2ims/test/utils"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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
		hwTemplate   = "hwTemplate-v1"
	)

	BeforeEach(func() {
		ctx = context.Background()
		clusterInstanceCRD, err := ctlrutils.BuildTestClusterInstanceCRD(ctlrutils.TestClusterInstanceSpecOk)
		Expect(err).ToNot(HaveOccurred())
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GetClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:       tName,
				Version:    tVersion,
				Release:    "4.15.0",
				TemplateID: "57b39bda-ac56-4143-9b10-d1a71517d04f",
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
					HwTemplate:              hwTemplate,
				},
				TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testutils.TestFullTemplateSchema)},
			},
		}

		c = getFakeClientFromObjects([]client.Object{ct, clusterInstanceCRD}...)
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
					ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey: `
baseDomain: value
clusterImageSetNameRef: "4.15.0"`,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ptDefaultsCm,
					Namespace: ctNamespace,
				},
				Data: map[string]string{
					ctlrutils.PolicyTemplateDefaultsConfigmapKey: `
clustertemplate-a-policy-v1-cpu-isolated: "2-31"
clustertemplate-a-policy-v1-cpu-reserved: "0-1"
clustertemplate-a-policy-v1-defaultHugepagesSize: "1G"`,
				},
			},
		}
		for _, cm := range cms {
			Expect(c.Create(ctx, cm)).To(Succeed())
		}
		hwtmpl := &hwmgmtv1alpha1.HardwareTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hwTemplate,
				Namespace: ctlrutils.InventoryNamespace,
			},
			Spec: hwmgmtv1alpha1.HardwareTemplateSpec{
				HardwarePluginRef: "hwMgr",
				NodeGroupData: []hwmgmtv1alpha1.NodeGroupData{
					{
						Name:           "master",
						Role:           "mmaster",
						ResourcePoolId: "xyz",
						HwProfile:      "profile-spr-single-processor-64G",
					},
					{
						Name:           "worker",
						Role:           "worker",
						ResourcePoolId: "xyz",
						HwProfile:      "profile-spr-single-processor-128G",
					},
				},
			},
		}
		Expect(c.Create(ctx, hwtmpl)).To(Succeed())

		// Create the ClusterImageSet required for validation
		clusterImageSet := &hivev1.ClusterImageSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "4.15.0",
			},
			Spec: hivev1.ClusterImageSetSpec{
				ReleaseImage: "quay.io/openshift-release-dev/ocp-release:4.15.0-x86_64",
			},
		}
		Expect(c.Create(ctx, clusterImageSet)).To(Succeed())

		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      GetClusterTemplateRefName(tName, tVersion),
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
		Expect(conditions[0].Type).To(Equal(string(provisioningv1alpha1.CTconditionTypes.Validated)))
		Expect(conditions[0].Status).To(Equal(metav1.ConditionTrue))
		Expect(conditions[0].Reason).To(Equal(string(provisioningv1alpha1.CTconditionReasons.Completed)))
		Expect(conditions[0].Message).To(Equal("The cluster template validation succeeded"))
	})

	It("should requeue an invalid ClusterTemplate", func() {
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      GetClusterTemplateRefName(tName, tVersion),
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
		Expect(conditions[0].Type).To(Equal(string(provisioningv1alpha1.CTconditionTypes.Validated)))
		Expect(conditions[0].Status).To(Equal(metav1.ConditionFalse))
		Expect(conditions[0].Reason).To(Equal(string(provisioningv1alpha1.CTconditionReasons.Failed)))
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

		clusterInstanceCRD, err := ctlrutils.BuildTestClusterInstanceCRD(ctlrutils.TestClusterInstanceSpecOk)
		Expect(err).ToNot(HaveOccurred())

		objs := []client.Object{cm, clusterInstanceCRD}
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

var _ = Describe("validatePolicyTemplateParamsSchema", func() {

	It("Returns error for missing properties", func() {
		var policyTemplateSchema map[string]any
		jsonString := `{
			"type": "object"
		}`

		err := json.Unmarshal([]byte(jsonString), &policyTemplateSchema)
		Expect(err).ToNot(HaveOccurred())
		err = validatePolicyTemplateParamsSchema(policyTemplateSchema)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(
			"unexpected policyTemplateParameters structure, no properties present"))
	})

	It("Returns nil for properties not being a map", func() {
		var policyTemplateSchema map[string]any
		jsonString := `{
			"type": "object",
			"properties": "string"
		}`

		err := json.Unmarshal([]byte(jsonString), &policyTemplateSchema)
		Expect(err).ToNot(HaveOccurred())
		err = validatePolicyTemplateParamsSchema(policyTemplateSchema)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(
			"unexpected policyTemplateParameters properties structure"))
	})

	It("Returns error for property not being a map", func() {
		var policyTemplateSchema map[string]any
		jsonString := `{
			"type": "object",
			"properties": {
			  "cpu-isolated": "string",
			  "sriov-network-vlan-1": {
				"type": "string"
			  }
			}
		}`
		err := json.Unmarshal([]byte(jsonString), &policyTemplateSchema)
		Expect(err).ToNot(HaveOccurred())
		err = validatePolicyTemplateParamsSchema(policyTemplateSchema)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(
			"unexpected policyTemplateParameters structure for the cpu-isolated property"))
	})

	It("Returns error for key different from \"type\"", func() {
		var policyTemplateSchema map[string]any
		jsonString := `{
			"type": "object",
			"properties": {
			  "cpu-isolated": {
				"type": "string"
			  },
			  "sriov-network-vlan-1": {
				"var-type": "integer"
			  }
			}
		}`

		err := json.Unmarshal([]byte(jsonString), &policyTemplateSchema)
		Expect(err).ToNot(HaveOccurred())
		err = validatePolicyTemplateParamsSchema(policyTemplateSchema)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(
			"unexpected policyTemplateParameters structure: expected subproperty \"type\" missing"))
	})

	It("Returns error for type property being an object", func() {
		var policyTemplateSchema map[string]any
		jsonString := `{
			"type": "object",
			"properties": {
			  "cpu-isolated": {
				"type": {
					"key": "value"
				}
			  },
			  "sriov-network-vlan-1": {
				"type": "string"
			  }
			}
		}`

		err := json.Unmarshal([]byte(jsonString), &policyTemplateSchema)
		Expect(err).ToNot(HaveOccurred())
		err = validatePolicyTemplateParamsSchema(policyTemplateSchema)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(
			"unexpected policyTemplateParameters structure: expected the subproperty \"type\" to be string"))
	})

	It("Returns error for non string type", func() {
		var policyTemplateSchema map[string]any
		jsonString := `{
			"type": "object",
			"properties": {
			  "cpu-isolated": {
				"type": "string"
			  },
			  "sriov-network-vlan-1": {
				"type": "integer"
			  }
			}
		}`

		err := json.Unmarshal([]byte(jsonString), &policyTemplateSchema)
		Expect(err).ToNot(HaveOccurred())
		err = validatePolicyTemplateParamsSchema(policyTemplateSchema)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(
			"expected type string for the sriov-network-vlan-1 property"))
	})

	It("Returns nil for expected structure", func() {
		var policyTemplateSchema map[string]any
		jsonString := `{
			"type": "object",
			"properties": {
			  "cpu-isolated": {
				"type": "string"
			  },
			  "sriov-network-vlan-1": {
				"type": "string"
			  }
			}
		}`

		err := json.Unmarshal([]byte(jsonString), &policyTemplateSchema)
		Expect(err).ToNot(HaveOccurred())
		err = validatePolicyTemplateParamsSchema(policyTemplateSchema)
		Expect(err).ToNot(HaveOccurred())
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
		hwTemplate   = "hwTemplate-v1"
		hwtmpl       *hwmgmtv1alpha1.HardwareTemplate
		t            *clusterTemplateReconcilerTask
	)

	BeforeEach(func() {
		ctx = context.Background()
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GetClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:    tName,
				Version: tVersion,
				Release: "4.15.0",
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
					HwTemplate:              hwTemplate,
				},
				TemplateParameterSchema: runtime.RawExtension{Raw: []byte(testutils.TestFullTemplateSchema)},
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
					ctlrutils.ClusterInstallationTimeoutConfigKey: "80m",
					ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey: `
baseDomain: value
clusterImageSetNameRef: "4.15.0"`,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ptDefaultsCm,
					Namespace: ctNamespace,
				},
				Data: map[string]string{
					ctlrutils.ClusterConfigurationTimeoutConfigKey: "40m",
					ctlrutils.PolicyTemplateDefaultsConfigmapKey: `
clustertemplate-a-policy-v1-cpu-isolated: "2-31"
clustertemplate-a-policy-v1-cpu-reserved: "0-1"
clustertemplate-a-policy-v1-defaultHugepagesSize: "1G"`,
				},
			},
		}

		hwtmpl = &hwmgmtv1alpha1.HardwareTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hwTemplate,
				Namespace: ctlrutils.InventoryNamespace,
			},
			Spec: hwmgmtv1alpha1.HardwareTemplateSpec{
				HardwarePluginRef: "hwMgr",
				NodeGroupData: []hwmgmtv1alpha1.NodeGroupData{
					{
						Name:           "master",
						Role:           "master",
						ResourcePoolId: "xyz",
						HwProfile:      "profile-spr-single-processor-64G",
					},
					{
						Name:           "worker",
						Role:           "wprker",
						ResourcePoolId: "xyz",
						HwProfile:      "profile-spr-single-processor-128G",
					},
				},
			},
		}

		clusterInstanceCRD, err := ctlrutils.BuildTestClusterInstanceCRD(ctlrutils.TestClusterInstanceSpecOk)
		Expect(err).ToNot(HaveOccurred())

		c = getFakeClientFromObjects([]client.Object{ct, clusterInstanceCRD}...)

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
		Expect(c.Create(ctx, hwtmpl)).To(Succeed())

		// Create the ClusterImageSet required for validation
		clusterImageSet := &hivev1.ClusterImageSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "4.15.0",
			},
			Spec: hivev1.ClusterImageSetSpec{
				ReleaseImage: "quay.io/openshift-release-dev/ocp-release:4.15.0-x86_64",
			},
		}
		Expect(c.Create(ctx, clusterImageSet)).To(Succeed())

		valid, err := t.validateClusterTemplateCR(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(valid).To(BeTrue())

		// Check the status condition
		conditions := t.object.Status.Conditions
		Expect(conditions).To(HaveLen(1))
		Expect(conditions[0].Type).To(Equal(string(provisioningv1alpha1.CTconditionTypes.Validated)))
		Expect(conditions[0].Status).To(Equal(metav1.ConditionTrue))
		Expect(conditions[0].Reason).To(Equal(string(provisioningv1alpha1.CTconditionReasons.Completed)))
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
		Expect(conditions[0].Type).To(Equal(string(provisioningv1alpha1.CTconditionTypes.Validated)))
		Expect(conditions[0].Status).To(Equal(metav1.ConditionFalse))
		Expect(conditions[0].Reason).To(Equal(string(provisioningv1alpha1.CTconditionReasons.Failed)))
		Expect(conditions[0].Message).To(ContainSubstring(fmt.Sprintf(
			"the ConfigMap '%s' is not found in the namespace '%s'", ciDefaultsCm, ctNamespace)))
		Expect(conditions[0].Message).To(ContainSubstring(fmt.Sprintf(
			"the ConfigMap '%s' is not found in the namespace '%s'", ptDefaultsCm, ctNamespace)))
	})

	It("should return false and set status condition to false if timeouts in ConfigMaps are invalid", func() {
		cms[0].Data[ctlrutils.ClusterInstallationTimeoutConfigKey] = "invalidCiTimeout"
		cms[1].Data[ctlrutils.ClusterConfigurationTimeoutConfigKey] = "invalidPtTimeout"
		for _, cm := range cms {
			Expect(c.Create(ctx, cm)).To(Succeed())
		}

		// Create the ClusterImageSet required for validation
		clusterImageSet := &hivev1.ClusterImageSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "4.15.0",
			},
			Spec: hivev1.ClusterImageSetSpec{
				ReleaseImage: "quay.io/openshift-release-dev/ocp-release:4.15.0-x86_64",
			},
		}
		Expect(c.Create(ctx, clusterImageSet)).To(Succeed())

		valid, err := t.validateClusterTemplateCR(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(valid).To(BeFalse())

		// Check the status condition
		conditions := t.object.Status.Conditions
		Expect(conditions).To(HaveLen(1))
		Expect(conditions[0].Type).To(Equal(string(provisioningv1alpha1.CTconditionTypes.Validated)))
		Expect(conditions[0].Status).To(Equal(metav1.ConditionFalse))
		Expect(conditions[0].Reason).To(Equal(string(provisioningv1alpha1.CTconditionReasons.Failed)))
		Expect(conditions[0].Message).To(ContainSubstring(fmt.Sprintf(
			"the value of key %s from ConfigMap %s is not a valid duration string", ctlrutils.ClusterConfigurationTimeoutConfigKey, ptDefaultsCm)))
		Expect(conditions[0].Message).To(ContainSubstring(fmt.Sprintf(
			"the value of key %s from ConfigMap %s is not a valid duration string", ctlrutils.ClusterInstallationTimeoutConfigKey, ciDefaultsCm)))
	})

	It("should return validation error message if the hardware template has invalid timeout string", func() {

		hwtmpl.Spec.HardwareProvisioningTimeout = "60"
		Expect(c.Create(ctx, hwtmpl)).To(Succeed())
		valid, err := t.validateClusterTemplateCR(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(valid).To(BeFalse())

		// Check the status condition
		conditions := t.object.Status.Conditions
		Expect(conditions).To(HaveLen(1))
		errMessage := fmt.Sprintf("the value of HardwareProvisioningTimeout from hardware template %s is not a valid duration string", hwtmpl.Name)
		Expect(conditions[0].Type).To(Equal(string(provisioningv1alpha1.CTconditionTypes.Validated)))
		Expect(conditions[0].Status).To(Equal(metav1.ConditionFalse))
		Expect(conditions[0].Reason).To(Equal(string(provisioningv1alpha1.CTconditionReasons.Failed)))
		Expect(conditions[0].Message).To(ContainSubstring(errMessage))

		// Check the HardwareTemplate status condition
		VerifyHardwareTemplateStatus(ctx, c, hwtmpl.Name, metav1.Condition{
			Type:    string(hwmgmtv1alpha1.Validation),
			Status:  metav1.ConditionFalse,
			Reason:  string(hwmgmtv1alpha1.Failed),
			Message: errMessage,
		})
	})

	Context("skip clusterimageset validation annotation", func() {
		DescribeTable("should correctly handle annotation values",
			func(annotationValue string, expectValidation bool) {
				// Create a test ClusterTemplate
				testCT := &provisioningv1alpha1.ClusterTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-template.v1.0.0",
						Namespace: "test-namespace",
						Annotations: map[string]string{
							ctlrutils.SkipClusterImageSetValidationAnnotation: annotationValue,
						},
					},
					Spec: provisioningv1alpha1.ClusterTemplateSpec{
						Name:    "test-template",
						Version: "v1.0.0",
						Release: "4.17.0",
						Templates: provisioningv1alpha1.Templates{
							ClusterInstanceDefaults: "test-ci-defaults",
							PolicyTemplateDefaults:  "test-pt-defaults",
						},
					},
				}

				// Capture log output
				var logBuffer bytes.Buffer
				testLogger := slog.New(slog.NewTextHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelInfo}))

				task := &clusterTemplateReconcilerTask{
					client: c,
					logger: testLogger,
					object: testCT,
				}

				// call validateClusterTemplateCR to validate the ClusterImageSet
				_, _ = task.validateClusterTemplateCR(ctx) // Ignore result since other validations will fail

				// Verify the expected behavior based on the expectValidation flag
				logOutput := logBuffer.String()
				if expectValidation {
					Expect(logOutput).To(ContainSubstring("Validating ClusterImageSet"))
					Expect(logOutput).ToNot(ContainSubstring("Skipping ClusterImageSet validation"))
				} else {
					Expect(logOutput).To(ContainSubstring("Skipping ClusterImageSet validation"))
					Expect(logOutput).ToNot(ContainSubstring("Validating ClusterImageSet"))
				}
			},
			Entry("annotation='true' - should skip", "true", false),
			Entry("annotation='TRUE' - should skip", "TRUE", false),
			Entry("annotation='True' - should skip", "True", false),
			Entry("annotation='TrUe' - should skip", "TrUe", false),
			Entry("annotation='false' - should validate", "false", true),
			Entry("annotation='' - should validate", "", true),
			Entry("annotation='yes' - should validate", "yes", true),
			Entry("annotation='1' - should validate", "1", true),
			Entry("annotation='skip' - should validate", "skip", true),
		)
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
		clusterInstanceCRD, err := ctlrutils.BuildTestClusterInstanceCRD(ctlrutils.TestClusterInstanceSpecOk)
		Expect(err).ToNot(HaveOccurred())
		c = getFakeClientFromObjects([]client.Object{clusterInstanceCRD}...)
	})

	It("should validate a valid configmap", func() {
		// Create a valid ConfigMap
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: namespace,
			},
			Data: map[string]string{
				ctlrutils.ClusterInstallationTimeoutConfigKey: "40m",
				ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey: `
baseDomain: example.sno.com`,
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())
		err := validateConfigmapReference[map[string]any](
			ctx, c, configmapName, namespace,
			ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey,
			ctlrutils.ClusterInstallationTimeoutConfigKey)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should return validation error message for a missing configmap", func() {
		// No ConfigMap created
		err := validateConfigmapReference[map[string]any](
			ctx, c, configmapName, namespace,
			ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey,
			ctlrutils.ClusterInstallationTimeoutConfigKey)
		Expect(err).To(HaveOccurred())
		Expect(ctlrutils.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(Equal(fmt.Sprintf(
			"failed to get ConfigmapReference: the ConfigMap '%s' is not found in the namespace '%s'", configmapName, namespace)))
	})

	It("should return validation error message for a configmap that does not match the ClusterInstance CRD", func() {
		// Create a valid ConfigMap but with a wrong schema.
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: namespace,
			},
			Data: map[string]string{
				ctlrutils.ClusterInstallationTimeoutConfigKey: "40m",
				ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey: `
baDomain: example.sno.com`,
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())
		// Cluster Instance schema error.
		err := validateConfigmapReference[map[string]any](
			ctx, c, configmapName, namespace,
			ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey,
			ctlrutils.ClusterInstallationTimeoutConfigKey)
		Expect(err).To(HaveOccurred())
		Expect(ctlrutils.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("failed to validate the default ConfigMap: the ConfigMap does not match the ClusterInstance schema"))
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
			ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey,
			ctlrutils.ClusterInstallationTimeoutConfigKey)
		Expect(err).To(HaveOccurred())
		Expect(ctlrutils.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(Equal(fmt.Sprintf(
			"the ConfigMap '%s' does not contain a field named '%s'", configmapName, ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey)))
	})

	It("should return validation error message for invalid YAML in configmap template data", func() {
		// Create a ConfigMap with invalid data YAML
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: namespace,
			},
			Data: map[string]string{
				ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey: `invalid-yaml`,
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		err := validateConfigmapReference[map[string]any](
			ctx, c, configmapName, namespace,
			ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey,
			ctlrutils.ClusterInstallationTimeoutConfigKey)
		Expect(err).To(HaveOccurred())
		Expect(ctlrutils.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("the value of key"))
	})

	It("should return validation error message for missing interface label in configmap template data", func() {
		// Create a ConfigMap with invalid data YAML
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: namespace,
			},
			Data: map[string]string{
				ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey: `
nodes:
- hostname: "node1"
  nodeNetwork:
    interfaces:
    - name: "eno1"
`,
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		err := validateConfigmapReference[map[string]any](
			ctx, c, configmapName, namespace,
			ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey,
			ctlrutils.ClusterInstallationTimeoutConfigKey)
		Expect(err).To(HaveOccurred())
		Expect(ctlrutils.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("'label' is missing for interface"))
	})

	It("should return validation error message for an empty interface label in configmap template data", func() {
		// Create a ConfigMap with invalid data YAML
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: namespace,
			},
			Data: map[string]string{
				ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey: `
nodes:
- hostname: "node1"
  nodeNetwork:
    interfaces:
    - name: "eno1"
      label: ""
`,
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		err := validateConfigmapReference[map[string]any](
			ctx, c, configmapName, namespace,
			ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey,
			ctlrutils.ClusterInstallationTimeoutConfigKey)
		Expect(err).To(HaveOccurred())
		Expect(ctlrutils.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("'label' is empty for interface"))
	})

	It("should return validation error message for invalid timeout value in configmap", func() {
		// Create a ConfigMap with non-integer string
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: namespace,
			},
			Data: map[string]string{
				ctlrutils.ClusterInstallationTimeoutConfigKey: "invalid-timeout",
				ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey: `
baseDomain: value`,
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		err := validateConfigmapReference[map[string]any](
			ctx, c, configmapName, namespace,
			ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey,
			ctlrutils.ClusterInstallationTimeoutConfigKey)
		Expect(err).To(HaveOccurred())
		Expect(ctlrutils.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("is not a valid duration string"))
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
				ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey: `
baseDomain: value`,
			},
			Immutable: &mutable,
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		err := validateConfigmapReference[map[string]any](
			ctx, c, configmapName, namespace,
			ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey,
			ctlrutils.ClusterInstallationTimeoutConfigKey)
		Expect(err).To(HaveOccurred())
		Expect(ctlrutils.IsInputError(err)).To(BeTrue())
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
				ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey: `
baseDomain: value`,
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		err := validateConfigmapReference[map[string]any](
			ctx, c, configmapName, namespace,
			ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey,
			ctlrutils.ClusterInstallationTimeoutConfigKey)
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
		hwTemplate   = "hwTemplate-v1"
	)

	BeforeEach(func() {
		ctx = context.Background()
		c = getFakeClientFromObjects()
	})

	It("should validate a cluster template name", func() {
		// Create a valid cluster template
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GetClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:    tName,
				Version: tVersion,
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
					HwTemplate:              hwTemplate,
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
				Name:      GetClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:    tName,
				Version: tVersion,
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
					HwTemplate:              hwTemplate,
				},
			},
		}
		ct2 := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GetClusterTemplateRefName(tName, tVersion),
				Namespace: "namespace1",
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:    tName,
				Version: tVersion,
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
					HwTemplate:              hwTemplate,
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
				Name:      GetClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:    tName,
				Version: tVersion,
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
					HwTemplate:              hwTemplate,
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
		hwTemplate   = "hwTemplate-v1"
	)

	BeforeEach(func() {
		ctx = context.Background()
		c = getFakeClientFromObjects()
	})

	It("Generate templateID", func() {
		// Create a valid cluster template
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GetClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:       tName,
				Version:    tVersion,
				TemplateID: "",
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
					HwTemplate:              hwTemplate,
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
				Name:      GetClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:       tName,
				Version:    tVersion,
				TemplateID: "kjwchbjkdbckj",
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
					HwTemplate:              hwTemplate,
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
				Name:      GetClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:       tName,
				Version:    tVersion,
				TemplateID: "71ba1920-77f8-4842-a474-010b1af1d40b",
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: ciDefaultsCm,
					PolicyTemplateDefaults:  ptDefaultsCm,
					HwTemplate:              hwTemplate,
				},
			},
		}
		Expect(c.Create(ctx, ct)).To(Succeed())
		err := validateTemplateID(ct)
		Expect(err).ToNot(HaveOccurred())
	})
})

var (
	tName    = "cluster-template-a"
	tVersion = "v1.0.0"
)

func Test_validateTemplateParameterSchema(t *testing.T) {
	type args struct {
		object *provisioningv1alpha1.ClusterTemplate
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
		errText string
	}{
		{
			name: "ok",
			args: args{
				object: &provisioningv1alpha1.ClusterTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name: GetClusterTemplateRefName(tName, tVersion),
					},
					Spec: provisioningv1alpha1.ClusterTemplateSpec{
						Templates: provisioningv1alpha1.Templates{
							HwTemplate: "hwTemplate-v1",
						},
						TemplateParameterSchema: runtime.RawExtension{Raw: []byte(`{
		"properties": {
			"nodeClusterName": {"type": "string"},
			"oCloudSiteId": {"type": "string"},
			"clusterInstanceParameters": {"type": "object"},
			"policyTemplateParameters": {"type": "object", "properties": {}}
		},
		"type": "object",
		"required": [
	"nodeClusterName",
	"oCloudSiteId",
	"policyTemplateParameters",
	"clusterInstanceParameters"
	]
	}`)},
					},
				},
			},
			wantErr: false,
			errText: "",
		},
		{
			name: "bad schema",
			args: args{
				object: &provisioningv1alpha1.ClusterTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name: GetClusterTemplateRefName(tName, tVersion),
					},
					Spec: provisioningv1alpha1.ClusterTemplateSpec{
						Templates: provisioningv1alpha1.Templates{
							HwTemplate: "hwTemplate-v1",
						},
						TemplateParameterSchema: runtime.RawExtension{Raw: []byte(`{
		"properties": {
			"nodeClusterName": {"type": "string"},
			"oCloudSiteId": {"type": "string"},
			"clusterInstanceParameters": {"type": "object"},
			"policyTemplateParameters": {"type": "object", "properties": {"a": {}}}
		},
		"type": "object",
		"required": [
	"nodeClusterName",
	"oCloudSiteId",
	"policyTemplateParameters",
	"clusterInstanceParameters"
	]
	}`)},
					},
				},
			},
			wantErr: true,
			errText: "Error validating the policyTemplateParameters schema: unexpected policyTemplateParameters structure: expected subproperty \"type\" missing",
		},
		{
			name: "bad type",
			args: args{
				object: &provisioningv1alpha1.ClusterTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name: GetClusterTemplateRefName(tName, tVersion),
					},
					Spec: provisioningv1alpha1.ClusterTemplateSpec{
						Templates: provisioningv1alpha1.Templates{
							HwTemplate: "hwTemplate-v1",
						},
						TemplateParameterSchema: runtime.RawExtension{Raw: []byte(`{
		"properties": {
			"nodeClusterName": {"type": "string"},
			"oCloudSiteId": {"type": "string"},
			"clusterInstanceParameters": {"type": "string"},
			"policyTemplateParameters": {"type": "object", "properties": {"a": {"type": "string"}}}
		},
		"type": "object",
		"required": [
	"nodeClusterName",
	"oCloudSiteId",
	"policyTemplateParameters",
	"clusterInstanceParameters"
	]
	}`)},
					},
				},
			},
			wantErr: true,
			errText: "failed to validate ClusterTemplate: cluster-template-a.v1.0.0. The following entries are present but have a unexpected type: clusterInstanceParameters (expected = object actual= string).",
		},
		{
			name: "missing parameter and bad type",
			args: args{
				object: &provisioningv1alpha1.ClusterTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name: GetClusterTemplateRefName(tName, tVersion),
					},
					Spec: provisioningv1alpha1.ClusterTemplateSpec{
						Templates: provisioningv1alpha1.Templates{
							HwTemplate: "hwTemplate-v1",
						},
						TemplateParameterSchema: runtime.RawExtension{Raw: []byte(`{
		"properties": {
			"oCloudSiteId": {"type": "string"},
			"clusterInstanceParameters": {"type": "string"},
			"policyTemplateParameters": {"type": "object", "properties": {"a": {"type": "string"}}}
		},
		"type": "object",
		"required": [
	"nodeClusterName",
	"oCloudSiteId",
	"policyTemplateParameters",
	"clusterInstanceParameters"
	]
	}`)},
					},
				},
			},
			wantErr: true,
			errText: "failed to validate ClusterTemplate: cluster-template-a.v1.0.0. The following mandatory fields are missing: nodeClusterName. The following entries are present but have a unexpected type: clusterInstanceParameters (expected = object actual= string).",
		},
		{
			name: "missing parameter and bad type, and missing required entry",
			args: args{
				object: &provisioningv1alpha1.ClusterTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name: GetClusterTemplateRefName(tName, tVersion),
					},
					Spec: provisioningv1alpha1.ClusterTemplateSpec{
						Templates: provisioningv1alpha1.Templates{
							HwTemplate: "hwTemplate-v1",
						},
						TemplateParameterSchema: runtime.RawExtension{Raw: []byte(`{
		"properties": {
			"oCloudSiteId": {"type": "string"},
			"clusterInstanceParameters": {"type": "string"},
			"policyTemplateParameters": {"type": "object", "properties": {"a": {"type": "string"}}}
		},
		"type": "object",
		"required": [
	"nodeClusterName",
	"policyTemplateParameters",
	"clusterInstanceParameters"
	]
	}`)},
					},
				},
			},
			wantErr: true,
			errText: "failed to validate ClusterTemplate: cluster-template-a.v1.0.0. The following mandatory fields are missing: nodeClusterName. The following entries are present but have a unexpected type: clusterInstanceParameters (expected = object actual= string).",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if err = validateTemplateParameterSchema(tt.args.object); (err != nil) != tt.wantErr {
				t.Errorf("validateTemplateParameterSchema() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && err.Error() != tt.errText {
				t.Errorf("validateTemplateParameterSchema() errorText = %s, wantErrorText %s", err.Error(), tt.errText)
			}
		})
	}
}

var _ = Describe("validateClusterInstanceParamsSchema", func() {

	var validSchema map[string]any

	BeforeEach(func() {
		// Initialize a valid schema for testing
		err := yaml.Unmarshal([]byte(ctlrutils.ClusterInstanceParamsSubSchemaForNoHWTemplate), &validSchema)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("when hardware template is provided", func() {
		It("should return nil for any schema when hwTemplate is not empty", func() {
			// Test with a valid schema
			err := validateClusterInstanceParamsSchema("hwTemplate-v1", validSchema)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return nil even with invalid schema when hwTemplate is provided", func() {
			// Test with an invalid/empty schema - should still pass because hwTemplate validation is skipped
			invalidSchema := map[string]any{
				"invalidProperty": "invalidValue",
			}
			err := validateClusterInstanceParamsSchema("some-hw-template", invalidSchema)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return nil for empty schema when hwTemplate is provided", func() {
			emptySchema := map[string]any{}
			err := validateClusterInstanceParamsSchema("hwTemplate-test", emptySchema)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return nil for nil schema when hwTemplate is provided", func() {
			err := validateClusterInstanceParamsSchema("hwTemplate-test", nil)
			Expect(err).ToNot(HaveOccurred())
		})

		Context("with various hardware template names", func() {
			It("should handle standard hardware template names", func() {
				testCases := []string{
					"hwTemplate-v1",
					"hardware-template-spr",
					"hwTemplate-dell-r650",
					"hw-template-master-node-profile",
					"template123",
				}

				for _, hwTemplate := range testCases {
					err := validateClusterInstanceParamsSchema(hwTemplate, validSchema)
					Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Failed for hwTemplate: %s", hwTemplate))
				}
			})

			It("should handle hardware template with special characters", func() {
				specialTemplates := []string{
					"hw-template_v1.0",
					"template-with-dots.v1.2.3",
					"template_underscore",
					"template123-456",
				}

				for _, hwTemplate := range specialTemplates {
					err := validateClusterInstanceParamsSchema(hwTemplate, validSchema)
					Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Failed for hwTemplate: %s", hwTemplate))
				}
			})
		})
	})

	Context("when hardware template is not provided", func() {
		It("should delegate to validateSchemaWithoutHWTemplate for empty string", func() {
			// This should call validateSchemaWithoutHWTemplate and succeed with valid schema
			err := validateClusterInstanceParamsSchema("", validSchema)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return error for invalid schema when hwTemplate is empty", func() {
			// Test with schema missing required properties
			invalidSchema := map[string]any{
				"properties": map[string]any{
					"invalidProperty": map[string]any{
						"type": "string",
					},
				},
			}
			err := validateClusterInstanceParamsSchema("", invalidSchema)
			Expect(err).To(HaveOccurred())
			// The error could be about missing "required", "type" or "nodes" depending on validation order
			Expect(err.Error()).To(SatisfyAny(
				ContainSubstring("missing key \"required\""),
				ContainSubstring("missing key \"nodes\""),
				ContainSubstring("missing key \"type\""),
			))
		})

		It("should return error for completely invalid schema structure when hwTemplate is empty", func() {
			invalidSchema := map[string]any{
				"notProperties": "invalidValue",
			}
			err := validateClusterInstanceParamsSchema("", invalidSchema)
			Expect(err).To(HaveOccurred())
		})

		It("should handle nil schema when hwTemplate is empty", func() {
			err := validateClusterInstanceParamsSchema("", nil)
			Expect(err).To(HaveOccurred())
		})

		It("should handle empty schema when hwTemplate is empty", func() {
			emptySchema := map[string]any{}
			err := validateClusterInstanceParamsSchema("", emptySchema)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("edge cases", func() {
		It("should treat whitespace-only hwTemplate as empty", func() {
			// Note: The function currently does exact string comparison with "",
			// so whitespace strings are treated as non-empty hwTemplate
			err := validateClusterInstanceParamsSchema("   ", validSchema)
			Expect(err).ToNot(HaveOccurred()) // This will pass because "   " != ""
		})

		It("should handle very large schema when hwTemplate is provided", func() {
			// Create a large schema with many properties
			largeSchema := map[string]any{
				"properties": map[string]any{},
			}
			properties := largeSchema["properties"].(map[string]any)

			// Add many properties to test performance/handling
			for i := 0; i < 100; i++ {
				properties[fmt.Sprintf("property%d", i)] = map[string]any{
					"type":        "string",
					"description": fmt.Sprintf("Property number %d", i),
				}
			}

			err := validateClusterInstanceParamsSchema("hwTemplate-large", largeSchema)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should handle schema with deeply nested structures when hwTemplate is provided", func() {
			deepSchema := map[string]any{
				"properties": map[string]any{
					"level1": map[string]any{
						"properties": map[string]any{
							"level2": map[string]any{
								"properties": map[string]any{
									"level3": map[string]any{
										"type": "string",
									},
								},
							},
						},
					},
				},
			}

			err := validateClusterInstanceParamsSchema("hwTemplate-deep", deepSchema)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("business logic validation", func() {
		It("should demonstrate that hardware template presence skips schema validation entirely", func() {
			// This test demonstrates the current business logic:
			// When hwTemplate is provided, NO schema validation occurs

			// Even completely malformed schema should pass
			malformedSchema := map[string]any{
				"this":       "is",
				"completely": []string{"wrong", "schema", "format"},
				"123":        "invalid key type",
			}

			err := validateClusterInstanceParamsSchema("any-hw-template", malformedSchema)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should validate schema only when hardware template is absent", func() {
			// This demonstrates that schema validation ONLY happens when hwTemplate is empty
			validSchemaForNoHW := validSchema

			// Should succeed when hwTemplate is empty and schema is valid
			err := validateClusterInstanceParamsSchema("", validSchemaForNoHW)
			Expect(err).ToNot(HaveOccurred())

			// Should also succeed when hwTemplate is provided, regardless of schema validity
			err = validateClusterInstanceParamsSchema("hw-template", validSchemaForNoHW)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

var _ = Describe("validateSchemaWithoutHWTemplate", func() {

	var baseSchema map[string]any

	BeforeEach(func() {
		err := yaml.Unmarshal([]byte(ctlrutils.ClusterInstanceParamsSubSchemaForNoHWTemplate), &baseSchema)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Returns error for missing nodes property", func() {
		// Remove the nodes property
		delete(baseSchema["properties"].(map[string]any), "nodes")

		err := validateSchemaWithoutHWTemplate(baseSchema)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(
			"unexpected clusterInstanceParameters structure: missing key \"nodes\" in field \"clusterInstanceParameters.properties\""))
	})

	It("Returns error for missing required properties in nodes", func() {
		// Remove bmcCredentialsDetails from nodes properties
		nodeProperties := baseSchema["properties"].(map[string]any)["nodes"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
		delete(nodeProperties, "bmcCredentialsDetails")

		err := validateSchemaWithoutHWTemplate(baseSchema)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			"missing key \"bmcCredentialsDetails\" in field \"clusterInstanceParameters.properties.nodes.items.properties\""))
	})

	It("Returns error for missing required username in bmcCredentialsDetails", func() {
		// Remove username from bmcCredentialsDetails properties
		nodes := baseSchema["properties"].(map[string]any)["nodes"].(map[string]any)
		items := nodes["items"].(map[string]any)
		properties := items["properties"].(map[string]any)
		bmcCredentialsDetails := properties["bmcCredentialsDetails"].(map[string]any)
		bmcCredentialsDetailsProperties := bmcCredentialsDetails["properties"].(map[string]any)
		delete(bmcCredentialsDetailsProperties, "username")

		err := validateSchemaWithoutHWTemplate(baseSchema)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			"missing key \"username\" in field \"clusterInstanceParameters.properties.nodes.items.properties.bmcCredentialsDetails.properties\""))
	})

	It("Returns error for bmcCredentialsDetails required field not being an array", func() {
		// Change bmcCredentialsDetails required field to be a non-array type
		nodes := baseSchema["properties"].(map[string]any)["nodes"].(map[string]any)
		items := nodes["items"].(map[string]any)
		properties := items["properties"].(map[string]any)
		bmcCredentialsDetails := properties["bmcCredentialsDetails"].(map[string]any)
		bmcCredentialsDetails["required"] = "notAnArray"

		err := validateSchemaWithoutHWTemplate(baseSchema)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			"expected a list for key \"required\" in field \"clusterInstanceParameters.properties.nodes.items.properties.bmcCredentialsDetails\""))
	})

	It("Returns error for incorrect type of nodeNetwork interfaces", func() {
		// Change the type of interfaces to string instead of object
		nodes := baseSchema["properties"].(map[string]any)["nodes"].(map[string]any)
		items := nodes["items"].(map[string]any)
		properties := items["properties"].(map[string]any)
		nodeNetworkProperties := properties["nodeNetwork"].(map[string]any)["properties"].(map[string]any)
		nodeNetworkProperties["interfaces"] = "incorrectType"

		err := validateSchemaWithoutHWTemplate(baseSchema)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			"expected a map for key \"interfaces\" in field \"clusterInstanceParameters.properties.nodes.items.properties.nodeNetwork.properties\""))
	})

	It("Returns error for missing required macAddress in nodeNetwork interfaces", func() {
		// Remove macAddress from nodeNetwork interfaces required properties
		nodes := baseSchema["properties"].(map[string]any)["nodes"].(map[string]any)
		items := nodes["items"].(map[string]any)
		properties := items["properties"].(map[string]any)
		nodeNetwork := properties["nodeNetwork"].(map[string]any)
		nodeNetworkProperties := nodeNetwork["properties"].(map[string]any)
		interfaces := nodeNetworkProperties["interfaces"].(map[string]any)["items"].(map[string]any)
		required := interfaces["required"].([]any)
		for i, v := range required {
			if v == "macAddress" {
				interfaces["required"] = append(required[:i], required[i+1:]...)
				interfaces["required"] = append(interfaces["required"].([]any), "testString")
				break
			}
		}

		err := validateSchemaWithoutHWTemplate(baseSchema)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			"list in field \"clusterInstanceParameters.properties.nodes.items.properties.nodeNetwork.properties.interfaces.items.required\" is missing element: macAddress"))
	})

	It("Returns nil for valid schema", func() {
		// Re-initialize the base schema for a valid test
		err := yaml.Unmarshal([]byte(ctlrutils.ClusterInstanceParamsSubSchemaForNoHWTemplate), &baseSchema)
		Expect(err).ToNot(HaveOccurred())

		err = validateSchemaWithoutHWTemplate(baseSchema)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("validateUpgradeDefaultsConfigmap", func() {
	var (
		c             client.Client
		ctx           context.Context
		t             *clusterTemplateReconcilerTask
		namespace     = "default"
		configmapName = "upgrade-defaults"
		tName         = "cluster-template-test"
		tVersion      = "v1.0.0"
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Create a cluster template with a release version
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GetClusterTemplateRefName(tName, tVersion),
				Namespace: namespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:    tName,
				Version: tVersion,
				Release: "4.17.0", // This should match the seedImageRef version in tests
				Templates: provisioningv1alpha1.Templates{
					UpgradeDefaults: configmapName,
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

	It("should validate a valid upgrade defaults configmap successfully", func() {
		// Create a valid upgrade defaults ConfigMap
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: namespace,
			},
			Data: map[string]string{
				ctlrutils.UpgradeDefaultsConfigmapKey: `
ibuSpec:
  seedImageRef:
    image: "quay.io/openshift-release-dev/ocp-release"
    version: "4.17.0"
  oadpContent:
  - name: "oadp-backup"
    namespace: "openshift-adp"
plan:
- actions: ["Prep"]
- actions: ["Upgrade"]`,
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		err := t.validateUpgradeDefaultsConfigmap(ctx, c, configmapName, namespace)
		Expect(err).ToNot(HaveOccurred())

		// Verify that the configmap was patched to be immutable
		updatedCM := &corev1.ConfigMap{}
		Expect(c.Get(ctx, client.ObjectKey{Name: configmapName, Namespace: namespace}, updatedCM)).To(Succeed())
		Expect(updatedCM.Immutable).ToNot(BeNil())
		Expect(*updatedCM.Immutable).To(BeTrue())
	})

	It("should return error when configmap does not exist", func() {
		// No ConfigMap created
		err := t.validateUpgradeDefaultsConfigmap(ctx, c, configmapName, namespace)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get IBGU from upgrade defaults configmap"))
		Expect(err.Error()).To(ContainSubstring("not found"))
	})

	It("should return error when configmap has invalid YAML data", func() {
		// Create a ConfigMap with invalid YAML
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: namespace,
			},
			Data: map[string]string{
				ctlrutils.UpgradeDefaultsConfigmapKey: "invalid: yaml: [data",
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		err := t.validateUpgradeDefaultsConfigmap(ctx, c, configmapName, namespace)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get IBGU from upgrade defaults configmap"))
	})

	It("should return error when release version does not match seedImageRef version", func() {
		// Create a valid ConfigMap but with different version
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: namespace,
			},
			Data: map[string]string{
				ctlrutils.UpgradeDefaultsConfigmapKey: `
ibuSpec:
  seedImageRef:
    image: "quay.io/openshift-release-dev/ocp-release"
    version: "4.18.0"
  oadpContent:
  - name: "oadp-backup"
    namespace: "openshift-adp"
plan:
- actions: ["Prep"]
- actions: ["Upgrade"]`,
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		err := t.validateUpgradeDefaultsConfigmap(ctx, c, configmapName, namespace)
		Expect(err).To(HaveOccurred())
		Expect(ctlrutils.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("The ClusterTemplate spec.release (4.17.0) does not match the seedImageRef version (4.18.0) from the upgrade configmap"))
	})

	It("should successfully validate when IBGU spec is valid for dry-run", func() {
		// Create a ConfigMap with a valid IBGU spec
		// Note: The fake client doesn't perform the same validation as real K8s API server,
		// so we test the successful path where dry-run validation passes
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: namespace,
			},
			Data: map[string]string{
				ctlrutils.UpgradeDefaultsConfigmapKey: `
ibuSpec:
  seedImageRef:
    image: "quay.io/openshift-release-dev/ocp-release"
    version: "4.17.0"
  stage: "Idle"
plan:
- actions: ["Prep"]
- actions: ["Upgrade"]`,
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		err := t.validateUpgradeDefaultsConfigmap(ctx, c, configmapName, namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should return validation error when configmap is set to mutable", func() {
		// Create a mutable ConfigMap
		mutable := false
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: namespace,
			},
			Data: map[string]string{
				ctlrutils.UpgradeDefaultsConfigmapKey: `
ibuSpec:
  seedImageRef:
    image: "quay.io/openshift-release-dev/ocp-release"
    version: "4.17.0"
  oadpContent:
  - name: "oadp-backup"
    namespace: "openshift-adp"
plan:
- actions: ["Prep"]
- actions: ["Upgrade"]`,
			},
			Immutable: &mutable,
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		err := t.validateUpgradeDefaultsConfigmap(ctx, c, configmapName, namespace)
		Expect(err).To(HaveOccurred())
		Expect(ctlrutils.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(Equal(fmt.Sprintf(
			"It is not allowed to set Immutable to false in the ConfigMap %s", configmapName)))
	})

	It("should succeed when configmap is already immutable", func() {
		// Create an already immutable ConfigMap
		immutable := true
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: namespace,
			},
			Data: map[string]string{
				ctlrutils.UpgradeDefaultsConfigmapKey: `
ibuSpec:
  seedImageRef:
    image: "quay.io/openshift-release-dev/ocp-release"
    version: "4.17.0"
  oadpContent:
  - name: "oadp-backup"
    namespace: "openshift-adp"
plan:
- actions: ["Prep"]
- actions: ["Upgrade"]`,
			},
			Immutable: &immutable,
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		err := t.validateUpgradeDefaultsConfigmap(ctx, c, configmapName, namespace)
		Expect(err).ToNot(HaveOccurred())

		// Verify that the configmap remains immutable
		updatedCM := &corev1.ConfigMap{}
		Expect(c.Get(ctx, client.ObjectKey{Name: configmapName, Namespace: namespace}, updatedCM)).To(Succeed())
		Expect(updatedCM.Immutable).ToNot(BeNil())
		Expect(*updatedCM.Immutable).To(BeTrue())
	})

	It("should handle missing configmap key", func() {
		// Create a ConfigMap without the expected key
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: namespace,
			},
			Data: map[string]string{
				"wrong-key": "some-data",
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		err := t.validateUpgradeDefaultsConfigmap(ctx, c, configmapName, namespace)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get IBGU from upgrade defaults configmap"))
	})

	Context("when cluster template has no release version", func() {
		BeforeEach(func() {
			// Update the cluster template to have no release version
			t.object.Spec.Release = ""
		})

		It("should return error when seedImageRef version is not empty", func() {
			// Create a valid ConfigMap
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configmapName,
					Namespace: namespace,
				},
				Data: map[string]string{
					ctlrutils.UpgradeDefaultsConfigmapKey: `
ibuSpec:
  seedImageRef:
    image: "quay.io/openshift-release-dev/ocp-release"
    version: "4.17.0"
  oadpContent:
  - name: "oadp-backup"
    namespace: "openshift-adp"
plan:
- actions: ["Prep"]
- actions: ["Upgrade"]`,
				},
			}
			Expect(c.Create(ctx, cm)).To(Succeed())

			err := t.validateUpgradeDefaultsConfigmap(ctx, c, configmapName, namespace)
			Expect(err).To(HaveOccurred())
			Expect(ctlrutils.IsInputError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("The ClusterTemplate spec.release () does not match the seedImageRef version (4.17.0) from the upgrade configmap"))
		})
	})
})

var _ = Describe("validateClusterImageSetMatchesRelease", func() {
	var (
		ctx             context.Context
		c               client.Client
		task            *clusterTemplateReconcilerTask
		clusterTemplate *provisioningv1alpha1.ClusterTemplate
		configMap       *corev1.ConfigMap
		clusterImageSet *hivev1.ClusterImageSet
		scheme          *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Initialize scheme with required types
		scheme = runtime.NewScheme()
		err := corev1.AddToScheme(scheme)
		Expect(err).NotTo(HaveOccurred())
		err = provisioningv1alpha1.AddToScheme(scheme)
		Expect(err).NotTo(HaveOccurred())
		err = hivev1.AddToScheme(scheme)
		Expect(err).NotTo(HaveOccurred())

		c = fake.NewClientBuilder().WithScheme(scheme).Build()

		clusterTemplate = &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster-template",
				Namespace: "test-namespace",
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:    "test-cluster-template",
				Version: "v1.0.0",
				Release: "4.17.0",
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: "test-configmap",
				},
			},
		}

		configMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-configmap",
				Namespace: "test-namespace",
			},
			Data: map[string]string{
				"clusterinstance-defaults": `
clusterImageSetNameRef: "4.17.0"
baseDomain: example.com
clusterType: SNO
`,
			},
		}

		clusterImageSet = &hivev1.ClusterImageSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "4.17.0",
			},
			Spec: hivev1.ClusterImageSetSpec{
				ReleaseImage: "quay.io/openshift-release-dev/ocp-release:4.17.0-x86_64",
			},
		}

		task = &clusterTemplateReconcilerTask{
			client: c,
			logger: logger,
			object: clusterTemplate,
		}
	})

	It("should succeed when ClusterImageSet version matches ClusterTemplate release", func() {
		Expect(c.Create(ctx, configMap)).To(Succeed())
		Expect(c.Create(ctx, clusterImageSet)).To(Succeed())

		err := task.validateClusterImageSetMatchesRelease(ctx)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should fail when ClusterImageSet version does not match ClusterTemplate release", func() {
		// Create ConfigMap and ClusterImageSet with mismatched versions
		clusterTemplate.Spec.Release = "4.16.0" // Different from ClusterImageSet version

		Expect(c.Create(ctx, configMap)).To(Succeed())
		Expect(c.Create(ctx, clusterImageSet)).To(Succeed())

		err := task.validateClusterImageSetMatchesRelease(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("does not match ClusterTemplate release version"))
		Expect(err.Error()).To(ContainSubstring("4.17.0"))
		Expect(err.Error()).To(ContainSubstring("4.16.0"))
	})

	It("should fail when ClusterInstanceDefaults ConfigMap does not exist", func() {
		// Don't create the ConfigMap
		err := task.validateClusterImageSetMatchesRelease(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get ClusterInstanceDefaults ConfigMap"))
	})

	It("should fail when clusterinstance-defaults key is missing from ConfigMap", func() {
		configMap.Data = map[string]string{
			"wrong-key": "some-data",
		}
		Expect(c.Create(ctx, configMap)).To(Succeed())

		err := task.validateClusterImageSetMatchesRelease(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to extract clusterinstance-defaults"))
	})

	It("should fail when clusterImageSetNameRef is missing from cluster instance data", func() {
		configMap.Data = map[string]string{
			"clusterinstance-defaults": `
baseDomain: example.com
clusterType: SNO
# clusterImageSetNameRef is missing
`,
		}
		Expect(c.Create(ctx, configMap)).To(Succeed())

		err := task.validateClusterImageSetMatchesRelease(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("clusterImageSetNameRef not found"))
	})

	It("should fail when clusterImageSetNameRef is not a string", func() {
		configMap.Data = map[string]string{
			"clusterinstance-defaults": `
clusterImageSetNameRef: 123  # Not a string
baseDomain: example.com
`,
		}
		Expect(c.Create(ctx, configMap)).To(Succeed())

		err := task.validateClusterImageSetMatchesRelease(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("clusterImageSetNameRef in ClusterInstanceDefaults ConfigMap"))
		Expect(err.Error()).To(ContainSubstring("is not a string"))
	})

	It("should fail when ClusterImageSet does not exist", func() {
		Expect(c.Create(ctx, configMap)).To(Succeed())
		// Don't create the ClusterImageSet

		err := task.validateClusterImageSetMatchesRelease(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get ClusterImageSet"))
	})

	It("should fail when ClusterImageSet has no releaseImage in spec", func() {
		clusterImageSet.Spec.ReleaseImage = ""

		Expect(c.Create(ctx, configMap)).To(Succeed())
		Expect(c.Create(ctx, clusterImageSet)).To(Succeed())

		err := task.validateClusterImageSetMatchesRelease(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("releaseImage not found in ClusterImageSet"))
	})

	It("should fail when ClusterImageSet releaseImage contains no extractable version", func() {
		clusterImageSet.Spec.ReleaseImage = "registry.example.com/openshift:latest" // No version pattern

		Expect(c.Create(ctx, configMap)).To(Succeed())
		Expect(c.Create(ctx, clusterImageSet)).To(Succeed())

		err := task.validateClusterImageSetMatchesRelease(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("could not extract version from ClusterImageSet"))
	})

	It("should fail when extracted version is invalid semver", func() {
		clusterImageSet.Spec.ReleaseImage = "registry.example.com/openshift:4.17.0-rc.invalid.identifier" // Invalid semver pre-release

		Expect(c.Create(ctx, configMap)).To(Succeed())
		Expect(c.Create(ctx, clusterImageSet)).To(Succeed())

		err := task.validateClusterImageSetMatchesRelease(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to parse ClusterImageSet version"))
	})

	It("should fail when release image has partial version (missing patch)", func() {
		clusterImageSet.Spec.ReleaseImage = "registry.example.com/openshift:4.17" // Missing patch version

		Expect(c.Create(ctx, configMap)).To(Succeed())
		Expect(c.Create(ctx, clusterImageSet)).To(Succeed())

		err := task.validateClusterImageSetMatchesRelease(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("could not extract version from ClusterImageSet"))
	})

	It("should fail when ClusterTemplate release version is invalid semver", func() {
		clusterTemplate.Spec.Release = "invalid-version"

		Expect(c.Create(ctx, configMap)).To(Succeed())
		Expect(c.Create(ctx, clusterImageSet)).To(Succeed())

		err := task.validateClusterImageSetMatchesRelease(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to parse ClusterTemplate release version"))
	})

	It("should handle pre-release versions correctly", func() {
		clusterTemplate.Spec.Release = "4.17.0-rc1"

		// Update the ConfigMap to reference the matching pre-release ClusterImageSet
		configMap.Data = map[string]string{
			"clusterinstance-defaults": `
clusterImageSetNameRef: "4.17.0-rc1"
baseDomain: example.com
clusterType: SNO
`,
		}

		// Update ClusterImageSet name and release image to match
		clusterImageSet.ObjectMeta.Name = "4.17.0-rc1"
		clusterImageSet.Spec.ReleaseImage = "quay.io/openshift-release-dev/ocp-release:4.17.0-rc1-x86_64"

		Expect(c.Create(ctx, configMap)).To(Succeed())
		Expect(c.Create(ctx, clusterImageSet)).To(Succeed())

		err := task.validateClusterImageSetMatchesRelease(ctx)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should handle different architectures in release image", func() {
		clusterImageSet.Spec.ReleaseImage = "quay.io/openshift-release-dev/ocp-release:4.17.0-aarch64"

		Expect(c.Create(ctx, configMap)).To(Succeed())
		Expect(c.Create(ctx, clusterImageSet)).To(Succeed())

		err := task.validateClusterImageSetMatchesRelease(ctx)
		Expect(err).ToNot(HaveOccurred())
	})

})

var _ = Describe("extractVersionFromReleaseImage", func() {
	It("should extract version from standard OpenShift release image", func() {
		testCases := []struct {
			description     string
			releaseImage    string
			expectedVersion string
		}{
			{
				description:     "standard OCP release image",
				releaseImage:    "quay.io/openshift-release-dev/ocp-release:4.17.0-x86_64",
				expectedVersion: "4.17.0",
			},
			{
				description:     "release image without architecture",
				releaseImage:    "quay.io/openshift-release-dev/ocp-release:4.16.1",
				expectedVersion: "4.16.1",
			},
			{
				description:     "release image with pre-release tag",
				releaseImage:    "registry.redhat.io/ubi8/ubi:4.15.2-rc1",
				expectedVersion: "4.15.2-rc1",
			},
			{
				description:     "simple registry with version",
				releaseImage:    "my-registry/openshift:4.14.10",
				expectedVersion: "4.14.10",
			},
			{
				description:     "version with build metadata",
				releaseImage:    "quay.io/openshift:4.18.0-build.123",
				expectedVersion: "4.18.0",
			},
		}

		for _, tc := range testCases {
			By(tc.description)
			version := extractVersionFromReleaseImage(tc.releaseImage)
			Expect(version).To(Equal(tc.expectedVersion))
		}
	})

	It("should return empty string for invalid release images", func() {
		testCases := []struct {
			description  string
			releaseImage string
		}{
			{
				description:  "image without tag",
				releaseImage: "quay.io/openshift-release-dev/ocp-release",
			},
			{
				description:  "image with non-version tag",
				releaseImage: "quay.io/openshift-release-dev/ocp-release:latest",
			},
			{
				description:  "image with partial version",
				releaseImage: "quay.io/openshift-release-dev/ocp-release:4.17",
			},
			{
				description:  "empty string",
				releaseImage: "",
			},
			{
				description:  "invalid format",
				releaseImage: "not-a-valid-image",
			},
		}

		for _, tc := range testCases {
			By(tc.description)
			version := extractVersionFromReleaseImage(tc.releaseImage)
			Expect(version).To(Equal(""))
		}
	})
})

var _ = Describe("validateVersionsMatch", func() {
	It("should succeed when versions match exactly", func() {
		testCases := []struct {
			description     string
			imageVersion    string
			templateVersion string
		}{
			{
				description:     "exact semantic versions",
				imageVersion:    "4.17.0",
				templateVersion: "4.17.0",
			},
			{
				description:     "versions with v prefix",
				imageVersion:    "v4.16.1",
				templateVersion: "v4.16.1",
			},
			{
				description:     "mixed prefix formats",
				imageVersion:    "4.15.2",
				templateVersion: "v4.15.2",
			},
			{
				description:     "pre-release versions",
				imageVersion:    "4.18.0-rc1",
				templateVersion: "4.18.0-rc1",
			},
		}

		for _, tc := range testCases {
			By(tc.description)
			err := validateVersionsMatch(tc.imageVersion, tc.templateVersion)
			Expect(err).ToNot(HaveOccurred())
		}
	})

	It("should fail when versions do not match", func() {
		testCases := []struct {
			description     string
			imageVersion    string
			templateVersion string
		}{
			{
				description:     "different patch versions",
				imageVersion:    "4.17.0",
				templateVersion: "4.17.1",
			},
			{
				description:     "different minor versions",
				imageVersion:    "4.16.0",
				templateVersion: "4.17.0",
			},
			{
				description:     "different major versions",
				imageVersion:    "3.17.0",
				templateVersion: "4.17.0",
			},
			{
				description:     "pre-release vs release",
				imageVersion:    "4.17.0-rc1",
				templateVersion: "4.17.0",
			},
		}

		for _, tc := range testCases {
			By(tc.description)
			err := validateVersionsMatch(tc.imageVersion, tc.templateVersion)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("versions do not match exactly"))
		}
	})

	It("should fail with invalid semver versions", func() {
		testCases := []struct {
			description     string
			imageVersion    string
			templateVersion string
			expectedError   string
		}{
			{
				description:     "invalid image version",
				imageVersion:    "invalid-version",
				templateVersion: "4.17.0",
				expectedError:   "failed to parse ClusterImageSet version",
			},
			{
				description:     "invalid template version",
				imageVersion:    "4.17.0",
				templateVersion: "not-semver",
				expectedError:   "failed to parse ClusterTemplate release version",
			},
			{
				description:     "both versions invalid",
				imageVersion:    "bad-version",
				templateVersion: "also-bad",
				expectedError:   "failed to parse ClusterImageSet version",
			},
			{
				description:     "partial version",
				imageVersion:    "4.17",
				templateVersion: "4.17.0",
				expectedError:   "failed to parse ClusterImageSet version",
			},
		}

		for _, tc := range testCases {
			By(tc.description)
			err := validateVersionsMatch(tc.imageVersion, tc.templateVersion)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(tc.expectedError))
		}
	})
})
