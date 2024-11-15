package controllers

import (
	"context"
	"encoding/base64"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
)

var _ = Describe("createPolicyTemplateConfigMap", func() {
	var (
		ctx         context.Context
		c           client.Client
		reconciler  *ProvisioningRequestReconciler
		task        *provisioningRequestReconcilerTask
		tName       = "clustertemplate-a"
		tVersion    = "v1.0.0"
		ctNamespace = "clustertemplate-a-v4-16"
		crName      = "cluster-1"
	)

	BeforeEach(func() {
		ctx := context.Background()
		// Define the provisioning request.
		cr := &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    tName,
				TemplateVersion: tVersion,
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
			ctDetails: &clusterTemplateDetails{
				namespace: ctNamespace,
			},
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

	It("it returns an error for a type different than string", func() {
		// Declare the merged policy template data.
		task.clusterInput.policyTemplateData = map[string]any{
			"cpu-isolated":    "0-1,64-65",
			"hugepages-count": 32,
		}

		// Create the configMap.
		err := task.createPolicyTemplateConfigMap(ctx, crName)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError("policyTemplateParameters/policyTemplateSchema for the hugepages-count key (32) is not a string"))
	})
})

var _ = Describe("GetLabelsForPolicies", func() {
	var (
		clusterLabels = map[string]string{}
		clusterName   = "cluster-1"
	)

	It("returns error if the clusterInstance does not have any labels", func() {

		err := checkClusterLabelsForPolicies(clusterName, clusterLabels)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			fmt.Sprintf("No cluster labels configured by the ClusterInstance %s(%s)",
				clusterName, clusterName)))
	})

	It("returns error if the clusterInstance does not have the cluster-version label", func() {

		clusterLabels["clustertemplate-a-policy"] = "v1"
		err := checkClusterLabelsForPolicies(clusterName, clusterLabels)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			fmt.Sprintf("Managed cluster %s is missing the cluster-version label.",
				clusterName)))
	})

	It("returns no error if the cluster-version label exists", func() {

		clusterLabels["cluster-version"] = "v4.17"
		err := checkClusterLabelsForPolicies(clusterName, clusterLabels)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("createClusterInstanceBMCSecrets", func() {
	var (
		ctx         context.Context
		c           client.Client
		reconciler  *ProvisioningRequestReconciler
		task        *provisioningRequestReconcilerTask
		tName       = "clustertemplate-a"
		tVersion    = "v1.0.0"
		ctNamespace = "clustertemplate-a-v4-16"
		crName      = "cluster-1"
	)

	BeforeEach(func() {
		// Define the provisioning request.
		cr := &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:       tName,
				TemplateVersion:    tVersion,
				TemplateParameters: runtime.RawExtension{},
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
			ctDetails: &clusterTemplateDetails{
				namespace: ctNamespace,
			},
		}
	})

	It("It returns error if bmcCredentialsDetails is missing in the input", func() {
		input := `{
			"clusterInstanceParameters": {
				"nodes": [
					{
						"bmcAddress": "idrac-virtualmedia+https://203.0.113.5/redfish/v1/Systems/System.Embedded.1",
						"bootMACAddress": "00:00:00:01:20:30",
						"hostName": "node1",
						"nodeNetwork": {
							"interfaces": [
								{
									"macAddress": "00:00:00:01:20:30"
						  		}
							]
						}
				  	}
				]
			}
		}`
		task.object.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(input)}
		err := task.createClusterInstanceBMCSecrets(ctx, crName)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			`\"bmcCredentialsDetails\" key expected to exist in spec.templateParameters.clusterInstanceParameters`))
	})

	It("it creates the BMC secret with correct content", func() {
		input := `{
			"clusterInstanceParameters": {
				"nodes": [
					{
						"bmcAddress": "idrac-virtualmedia+https://203.0.113.5/redfish/v1/Systems/System.Embedded.1",
						"bootMACAddress": "00:00:00:01:20:30",
						"bmcCredentialsDetails": {
							"username": "QURNSU4K",
							"password": "QURNSU4K"
						},
						"hostName": "node1",
						"nodeNetwork": {
							"interfaces": [
								{
									"macAddress": "00:00:00:01:20:30"
						  		}
							]
						}
				  	}
				]
			}
		}`
		task.object.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(input)}
		err := task.createClusterInstanceBMCSecrets(ctx, crName)
		Expect(err).ToNot(HaveOccurred())

		bmcSecret := &corev1.Secret{}
		err = c.Get(context.Background(), types.NamespacedName{Name: "node1-bmc-secret", Namespace: "cluster-1"}, bmcSecret)
		Expect(err).ToNot(HaveOccurred())
		decoded, _ := base64.StdEncoding.DecodeString("QURNSU4K")
		Expect(bmcSecret.Data["username"]).To(Equal(decoded))
		Expect(bmcSecret.Data["password"]).To(Equal(decoded))
	})

	It("It creates the BMC secret with provided bmcCredentialsName name", func() {
		input := `{
			"clusterInstanceParameters": {
				"nodes": [
					{
						"bmcAddress": "idrac-virtualmedia+https://203.0.113.5/redfish/v1/Systems/System.Embedded.1",
						"bootMACAddress": "00:00:00:01:20:30",
						"bmcCredentialsName": {
							"name": "node1-secret"
						},
						"bmcCredentialsDetails": {
							"username": "QURNSU4K",
							"password": "QURNSU4K"
						},
						"hostName": "node1",
						"nodeNetwork": {
							"interfaces": [
								{
									"macAddress": "00:00:00:01:20:30"
						  		}
							]
						}
				  	}
				]
			}
		}`
		task.object.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(input)}
		err := task.createClusterInstanceBMCSecrets(ctx, crName)
		Expect(err).ToNot(HaveOccurred())

		bmcSecret := &corev1.Secret{}
		err = c.Get(context.Background(), types.NamespacedName{Name: "node1-secret", Namespace: "cluster-1"}, bmcSecret)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("createOrUpdateClusterResources", func() {
	var (
		ctx         context.Context
		c           client.Client
		reconciler  *ProvisioningRequestReconciler
		task        *provisioningRequestReconcilerTask
		tName       = "clustertemplate-a"
		tVersion    = "v1.0.0"
		ctNamespace = "clustertemplate-a-v4-16"
		crName      = "cluster-1"
	)

	BeforeEach(func() {
		// Define the provisioning request.
		cr := &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:       tName,
				TemplateVersion:    tVersion,
				TemplateParameters: runtime.RawExtension{},
			},
		}
		pull_secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pull-secret",
				Namespace: ctNamespace,
			},
			Data: map[string][]byte{},
		}
		c = getFakeClientFromObjects([]client.Object{cr, pull_secret}...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &provisioningRequestReconcilerTask{
			logger:       reconciler.Logger,
			client:       reconciler.Client,
			object:       cr,
			clusterInput: &clusterInput{},
			ctDetails: &clusterTemplateDetails{
				namespace: ctNamespace,
				templates: provisioningv1alpha1.Templates{},
			},
		}
	})

	It("It creates BMC secret when hwTemplate is not provided", func() {
		renderedClusterInstance := &siteconfig.ClusterInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: crName,
			},
			Spec: siteconfig.ClusterInstanceSpec{
				PullSecretRef: corev1.LocalObjectReference{Name: "pull-secret"},
			},
		}

		input := `{
			"clusterInstanceParameters": {
				"nodes": [
					{
						"bmcAddress": "idrac-virtualmedia+https://203.0.113.5/redfish/v1/Systems/System.Embedded.1",
						"bootMACAddress": "00:00:00:01:20:30",
						"bmcCredentialsDetails": {
							"username": "QURNSU4K",
							"password": "QURNSU4K"
						},
						"hostName": "node1",
						"nodeNetwork": {
							"interfaces": [
								{
									"macAddress": "00:00:00:01:20:30"
						  		}
							]
						}
				  	}
				]
			}
		}`
		task.object.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(input)}
		err := task.createOrUpdateClusterResources(ctx, renderedClusterInstance)
		Expect(err).To(HaveOccurred())

		bmcSecret := &corev1.Secret{}
		err = c.Get(context.Background(), types.NamespacedName{Name: "node1-bmc-secret", Namespace: "cluster-1"}, bmcSecret)
		Expect(err).ToNot(HaveOccurred())
	})

	It("No BMC secret is created when hwTemplate is provided", func() {
		renderedClusterInstance := &siteconfig.ClusterInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: crName,
			},
			Spec: siteconfig.ClusterInstanceSpec{
				PullSecretRef: corev1.LocalObjectReference{Name: "pull-secret"},
			},
		}

		task.ctDetails.templates.HwTemplate = "hwTemplate-v1"
		err := task.createOrUpdateClusterResources(ctx, renderedClusterInstance)
		Expect(err).To(HaveOccurred())

		bmcSecret := &corev1.Secret{}
		err = c.Get(context.Background(), types.NamespacedName{Name: "node1-bmc-secret", Namespace: "cluster-1"}, bmcSecret)
		Expect(err).To(HaveOccurred())
		Expect(errors.IsNotFound(err)).To(BeTrue())
	})
})
