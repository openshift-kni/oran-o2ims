/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

/*
import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"

	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	testutils "github.com/openshift-kni/oran-o2ims/test/utils"
	assistedservicev1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type expectedNodeDetails struct {
	BMCAddress         string
	BMCCredentialsName string
	BootMACAddress     string
	Interfaces         []map[string]interface{}
}

var _ = Describe("ProvisioningRequestReconcile", func() {
	var (
		c            client.Client
		ctx          context.Context
		reconciler   *ProvisioningRequestReconciler
		req          reconcile.Request
		cr           *provisioningv1alpha1.ProvisioningRequest
		ct           *provisioningv1alpha1.ClusterTemplate
		tName        = "clustertemplate-a"
		tVersion     = "v1.0.0"
		ctNamespace  = "clustertemplate-a-v4-16"
		ciDefaultsCm = "clusterinstance-defaults-v1"
		ptDefaultsCm = "policytemplate-defaults-v1"
		hwTemplate   = "hwTemplate-v1"
		crName       = "cluster-1"
		agentName    = "agent-cluster-1"
	)

	BeforeEach(func() {
		ctx = context.Background()

		crs := []client.Object{
			// HW plugin test namespace
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: utils.UnitTestHwmgrNamespace,
				},
			},
			// Cluster Template Namespace
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ctNamespace,
				},
			},
			// Configmap for ClusterInstance defaults
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ciDefaultsCm,
					Namespace: ctNamespace,
				},
				Data: map[string]string{
					utils.ClusterInstallationTimeoutConfigKey: "60s",
					utils.ClusterInstanceTemplateDefaultsConfigmapKey: `
    clusterImageSetNameRef: "4.15"
    pullSecretRef:
      name: "pull-secret"
    templateRefs:
    - name: "ai-cluster-templates-v1"
      namespace: "siteconfig-operator"
    nodes:
    - hostName: "node1"
      role: master
      bootMode: UEFI
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
        namespace: "siteconfig-operator"
    `,
				},
			},
			// Configmap for policy template defaults
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ptDefaultsCm,
					Namespace: ctNamespace,
				},
				Data: map[string]string{
					utils.ClusterConfigurationTimeoutConfigKey: "1m",
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
					BootInterfaceLabel:          "bootable-interface",
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
					Extensions: map[string]string{
						"resourceTypeId": "ResourceGroup~2.1.1",
					},
				},
			},
			// Pull secret for ClusterInstance
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pull-secret",
					Namespace: ctNamespace,
				},
			},
		}
		// Define the cluster template.
		ct = &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GetClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Name:       tName,
				Version:    tVersion,
				TemplateID: "57b39bda-ac56-4143-9b10-d1a71517d04f",
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
		}

		// Define the provisioning request.
		cr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:       crName,
				Finalizers: []string{provisioningv1alpha1.ProvisioningRequestFinalizer},
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    tName,
				TemplateVersion: tVersion,
				TemplateParameters: runtime.RawExtension{
					Raw: []byte(testutils.TestFullTemplateParameters),
				},
			},
		}

		crs = append(crs, cr, ct)
		c = getFakeClientFromObjects(crs...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}

		// Request for ProvisioningRequest
		req = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: crName,
			},
		}
	})

	Context("Resources preparation during initial Provisioning", func() {
		It("Verify status conditions if ProvisioningRequest validation fails", func() {
			// Fail the ClusterTemplate validation
			ctValidatedCond := meta.FindStatusCondition(
				ct.Status.Conditions, string(provisioningv1alpha1.CTconditionTypes.Validated))
			ctValidatedCond.Status = metav1.ConditionFalse
			ctValidatedCond.Reason = string(provisioningv1alpha1.CTconditionReasons.Failed)
			Expect(c.Status().Update(ctx, ct)).To(Succeed())

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(1))
			testutils.VerifyStatusCondition(conditions[0], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.Validated),
				Status: metav1.ConditionFalse,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Failed),
				Message: fmt.Sprintf(
					"Failed to validate the ProvisioningRequest: failed to get the ClusterTemplate for "+
						"ProvisioningRequest cluster-1: a valid ClusterTemplate (%s) does not exist in any namespace",
					ct.Name),
			})
			// Verify provisioningState is failed when cr validation fails.
			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Failed to validate the ProvisioningRequest", nil)
		})

		It("Verify status conditions if Cluster resources creation fails", func() {
			// Delete the pull secret for ClusterInstance
			secret := &corev1.Secret{}
			secret.SetName("pull-secret")
			secret.SetNamespace(ctNamespace)
			Expect(c.Delete(ctx, secret)).To(Succeed())

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithMediumInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(3))
			testutils.VerifyStatusCondition(conditions[0], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.Validated),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			testutils.VerifyStatusCondition(conditions[1], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			testutils.VerifyStatusCondition(conditions[2], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ClusterResourcesCreated),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Failed),
				Message: "failed to create pull Secret for cluster cluster-1",
			})

			// Verify provisioningState is failed when the required resource creation fails.
			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Failed to apply the required cluster resource", nil)
		})

		It("Verify status conditions if all preparation work completes", func() {
			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithMediumInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify NodeAllocationRequest was created
			nodeAllocationRequest := &pluginsv1alpha1.NodeAllocationRequest{}
			Expect(c.Get(ctx, types.NamespacedName{
				Name: crName, Namespace: utils.UnitTestHwmgrNamespace}, nodeAllocationRequest)).To(Succeed())

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(5))
			testutils.VerifyStatusCondition(conditions[0], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.Validated),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			testutils.VerifyStatusCondition(conditions[1], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			testutils.VerifyStatusCondition(conditions[2], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterResourcesCreated),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			testutils.VerifyStatusCondition(conditions[3], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareTemplateRendered),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			testutils.VerifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionUnknown,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Unknown),
			})
			// Verify the start timestamp has been set for HardwareProvisioning
			Expect(reconciledCR.Status.Extensions.NodeAllocationRequestRef.HardwareProvisioningCheckStart).ToNot(BeZero())
			// Verify provisioningState is progressing when nodeAllocationRequest has been created
			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Waiting for NodeAllocationRequest (cluster-1) to be processed", nil)
		})
	})

	Context("When NodeAllocationRequest has been created", func() {
		var nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest

		BeforeEach(func() {
			// Create NodeAllocationRequest resource
			nodeAllocationRequest = &pluginsv1alpha1.NodeAllocationRequest{}
			nodeAllocationRequest.SetName(crName)
			nodeAllocationRequest.SetNamespace(utils.UnitTestHwmgrNamespace)
			nodeAllocationRequest.Spec.HardwarePluginRef = utils.UnitTestHwPluginRef
			// Ensure that the NodeGroup matches the data in the hwTemplate
			nodeAllocationRequest.Spec.NodeGroup = []pluginsv1alpha1.NodeGroup{
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
					Name:           "controller",
					Role:           "master",
					HwProfile:      "profile-spr-single-processor-64G",
					ResourcePoolId: "xyz",
				},
					Size: 1,
				},
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
					Name:           "worker",
					Role:           "worker",
					HwProfile:      "profile-spr-dual-processor-128G",
					ResourcePoolId: "xyz",
				},
					Size: 0,
				},
			}
			nodeAllocationRequest.Status.Conditions = []metav1.Condition{
				{Type: string(hwmgmtv1alpha1.Provisioned), Status: metav1.ConditionFalse, Reason: string(hwmgmtv1alpha1.InProgress)},
			}
			nodeAllocationRequest.Status.Properties = hwmgmtv1alpha1.Properties{NodeNames: []string{testutils.MasterNodeName}}
			nodeAllocationRequest.Annotations = map[string]string{hwmgmtv1alpha1.BootInterfaceLabelAnnotation: "bootable-interface"}
			Expect(c.Create(ctx, nodeAllocationRequest)).To(Succeed())
			testutils.CreateNodeResources(ctx, c, nodeAllocationRequest.Name)

			cr.Status.Extensions.NodeAllocationRequestRef = &provisioningv1alpha1.NodeAllocationRequestRef{
				NodeAllocationRequestID:        nodeAllocationRequest.Name,
				HardwareProvisioningCheckStart: &metav1.Time{Time: time.Now()},
			}
			Expect(c.Status().Update(ctx, cr)).To(Succeed())
		})

		It("Verify ClusterInstance should not be created when NodeAllocationRequest provision is in-progress", func() {
			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithMediumInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify no ClusterInstance was created
			clusterInstance := &siteconfig.ClusterInstance{}
			Expect(c.Get(ctx, types.NamespacedName{
				Name: crName, Namespace: crName}, clusterInstance)).To(HaveOccurred())

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(5))
			testutils.VerifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(provisioningv1alpha1.CRconditionReasons.InProgress),
			})
			// Verify the start timestamp has been set for HardwareProvisioning
			Expect(reconciledCR.Status.Extensions.NodeAllocationRequestRef.HardwareProvisioningCheckStart).ToNot(BeZero())
			// Verify provisioningState is progressing when nodeAllocationRequest is in-progress
			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Hardware provisioning is in progress", nil)
		})

		It("Verify ClusterInstance should be created when NodeAllocationRequest has provisioned", func() {
			// Patch NodeAllocationRequest provision status to Completed
			narProvisionedCond := meta.FindStatusCondition(
				nodeAllocationRequest.Status.Conditions, string(hwmgmtv1alpha1.Provisioned),
			)
			narProvisionedCond.Status = metav1.ConditionTrue
			narProvisionedCond.Reason = string(hwmgmtv1alpha1.Completed)
			Expect(c.Status().Update(ctx, nodeAllocationRequest)).To(Succeed())

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ClusterInstance was created
			clusterInstance := &siteconfig.ClusterInstance{}
			Expect(c.Get(ctx, types.NamespacedName{
				Name: crName, Namespace: crName}, clusterInstance)).To(Succeed())

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(7))
			testutils.VerifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			testutils.VerifyStatusCondition(conditions[5], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareNodeConfigApplied),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			testutils.VerifyStatusCondition(conditions[6], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed),
				Status: metav1.ConditionUnknown,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Unknown),
			})
			// Verify provisioningState is still progressing when nodeAllocationRequest is provisioned and clusterInstance is created
			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Waiting for ClusterInstance (cluster-1) to be processed", nil)
		})

		It("Verify status when HW provision has failed", func() {
			// Patch NodeAllocationRequest provision status to Completed
			narProvisionedCond := meta.FindStatusCondition(
				nodeAllocationRequest.Status.Conditions, string(hwmgmtv1alpha1.Provisioned),
			)
			narProvisionedCond.Status = metav1.ConditionFalse
			narProvisionedCond.Reason = string(hwmgmtv1alpha1.Failed)
			Expect(c.Status().Update(ctx, nodeAllocationRequest)).To(Succeed())

			// Start reconciliation again
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue())) // stop reconciliation on HwProvision failed

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(5))
			testutils.VerifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Failed),
			})
			// Verify the provisioningState moves to failed when HW provisioning fails
			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Hardware provisioning failed", nil)
		})

		It("Verify status when HW provision has timedout", func() {
			// Initial reconciliation to populate start timestamp
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			cr := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, cr)).To(Succeed())
			// Verify the start timestamp has been set for NodeAllocationRequest
			Expect(cr.Status.Extensions.NodeAllocationRequestRef.HardwareProvisioningCheckStart).ToNot(BeZero())

			// Patch HardwareProvisioningCheckStart timestamp to mock timeout
			cr.Status.Extensions.NodeAllocationRequestRef.HardwareProvisioningCheckStart.Time = metav1.Now().Add(-2 * time.Minute)
			Expect(c.Status().Update(ctx, cr)).To(Succeed())

			// Start reconciliation again
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue())) // stop reconciliation on HwProvision timeout

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(5))
			testutils.VerifyStatusCondition(conditions[4], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(provisioningv1alpha1.CRconditionReasons.TimedOut),
			})

			// Verify the provisioningState moves to failed when HW provisioning times out
			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Hardware provisioning timed out", nil)
		})

		It("Verify status when configuration change causes ProvisioningRequest validation to fail but NodeAllocationRequest is also failed", func() {
			// Initial reconciliation
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			// Patch NodeAllocationRequest provision status to Completed
			currentNar := &pluginsv1alpha1.NodeAllocationRequest{}
			Expect(c.Get(ctx, types.NamespacedName{Name: crName, Namespace: utils.UnitTestHwmgrNamespace}, currentNar)).To(Succeed())
			narProvisionedCond := meta.FindStatusCondition(
				currentNar.Status.Conditions, string(hwmgmtv1alpha1.Provisioned),
			)
			narProvisionedCond.Status = metav1.ConditionFalse
			narProvisionedCond.Reason = string(hwmgmtv1alpha1.Failed)
			narProvisionedCond.Message = "NodeAllocationRequest failed"
			Expect(c.Status().Update(ctx, currentNar)).To(Succeed())

			// Remove required field hostname to fail ProvisioningRequest validation
			testutils.RemoveRequiredFieldFromClusterInstanceInput(ctx, c, crName)

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify that the validated condition fails but hw provisioned condition
			// has changed to Completed
			Expect(len(conditions)).To(Equal(5))
			testutils.VerifyStatusCondition(conditions[0], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.Validated),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Failed),
				Message: "nodes.0: hostName is required",
			})
			testutils.VerifyStatusCondition(conditions[4], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Failed),
				Message: "Hardware provisioning failed",
			})
			// Verify the provisioningPhase has changed to failed with the reason
			// hardware provisioning failed as on-going provisioning process is failed.
			// Although new changes cause validation to fail as well, the provisioningPhase
			// should remain failed with the on-going provisioning failed reason.
			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed,
				"Hardware provisioning failed", nil)
		})
	})

	Context("When ClusterInstance has been created", func() {
		var (
			nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest
			clusterInstance       *siteconfig.ClusterInstance
			managedCluster        *clusterv1.ManagedCluster
			policy                *policiesv1.Policy
		)
		BeforeEach(func() {
			// Create NodeAllocationRequest resource that has provisioned
			nodeAllocationRequest = &pluginsv1alpha1.NodeAllocationRequest{}
			nodeAllocationRequest.SetName(crName)
			nodeAllocationRequest.SetNamespace(utils.UnitTestHwmgrNamespace)
			nodeAllocationRequest.Spec.HardwarePluginRef = utils.UnitTestHwPluginRef
			// Ensure that the NodeGroup matches the data in the hwTemplate
			nodeAllocationRequest.Spec.NodeGroup = []pluginsv1alpha1.NodeGroup{
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
					Name:           "controller",
					Role:           "master",
					HwProfile:      "profile-spr-single-processor-64G",
					ResourcePoolId: "xyz",
				},
					Size: 1,
				},
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
					Name:           "worker",
					Role:           "worker",
					HwProfile:      "profile-spr-dual-processor-128G",
					ResourcePoolId: "xyz",
				},
					Size: 0,
				},
			}
			nodeAllocationRequest.Status.Conditions = []metav1.Condition{
				{Type: string(hwmgmtv1alpha1.Provisioned), Status: metav1.ConditionTrue, Reason: string(hwmgmtv1alpha1.Completed)},
			}
			nodeAllocationRequest.Status.Properties = hwmgmtv1alpha1.Properties{NodeNames: []string{testutils.MasterNodeName}}
			nodeAllocationRequest.Annotations = map[string]string{hwmgmtv1alpha1.BootInterfaceLabelAnnotation: "bootable-interface"}
			Expect(c.Create(ctx, nodeAllocationRequest)).To(Succeed())
			testutils.CreateNodeResources(ctx, c, nodeAllocationRequest.Name)
			// Set the provisioningRequest extensions.nodeAllocationRequestRef
			cr.Status.Extensions.NodeAllocationRequestRef = &provisioningv1alpha1.NodeAllocationRequestRef{
				NodeAllocationRequestID:        nodeAllocationRequest.Name,
				HardwareProvisioningCheckStart: &metav1.Time{Time: time.Now()},
			}
			Expect(c.Status().Update(ctx, cr)).To(Succeed())

			// Create ClusterInstance resource
			clusterInstance = &siteconfig.ClusterInstance{}
			clusterInstance.SetName(crName)
			clusterInstance.SetNamespace(crName)
			clusterInstance.Spec = siteconfig.ClusterInstanceSpec{
				Nodes: []siteconfig.NodeSpec{
					{
						HostName:           "node-1",
						BmcAddress:         "192.168.111.1",
						BmcCredentialsName: siteconfig.BmcCredentialsName{Name: "node-1-bmc-secret"},
						NodeNetwork: &assistedservicev1beta1.NMStateConfigSpec{
							Interfaces: []*assistedservicev1beta1.Interface{
								{
									Name:       "eno1",
									MacAddress: "00:00:00:00:00:00",
								},
							},
						},
					},
				},
			}
			clusterInstance.Status.Conditions = []metav1.Condition{
				{Type: string(siteconfig.ClusterInstanceValidated), Status: metav1.ConditionTrue},
				{Type: string(siteconfig.RenderedTemplates), Status: metav1.ConditionTrue},
				{Type: string(siteconfig.RenderedTemplatesValidated), Status: metav1.ConditionTrue},
				{Type: string(siteconfig.RenderedTemplatesApplied), Status: metav1.ConditionTrue}}
			Expect(c.Create(ctx, clusterInstance)).To(Succeed())
			// Create ManagedCluster
			managedCluster = &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: crName},
				Status: clusterv1.ManagedClusterStatus{
					Conditions: []metav1.Condition{
						{Type: clusterv1.ManagedClusterConditionAvailable, Status: metav1.ConditionFalse},
						{Type: clusterv1.ManagedClusterConditionHubAccepted, Status: metav1.ConditionTrue},
						{Type: clusterv1.ManagedClusterConditionJoined, Status: metav1.ConditionTrue}},
				},
			}
			Expect(c.Create(ctx, managedCluster)).To(Succeed())
			// Create the agent.
			agent := &assistedservicev1beta1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: crName,
					Labels: map[string]string{
						"agent-install.openshift.io/clusterdeployment-namespace": crName,
					},
				},
				Spec: assistedservicev1beta1.AgentSpec{
					Approved: true,
					ClusterDeploymentName: &assistedservicev1beta1.ClusterReference{
						Name:      crName,
						Namespace: crName,
					},
				},
			}
			Expect(c.Create(ctx, agent)).To(Succeed())
			// Create Non-compliant enforce policy
			policy = &policiesv1.Policy{
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
				},
			}
			Expect(c.Create(ctx, policy)).To(Succeed())
		})

		It("Verify status when ClusterInstance provision is still in progress and ManagedCluster is not ready", func() {
			// Patch ClusterInstance provisioned status to InProgress
			crProvisionedCond := metav1.Condition{
				Type: string(siteconfig.ClusterProvisioned), Status: metav1.ConditionFalse,
				Reason: string(siteconfig.InProgress), Message: "Provisioning cluster",
			}
			clusterInstance.Status.Conditions = append(clusterInstance.Status.Conditions, crProvisionedCond)
			Expect(c.Status().Update(ctx, clusterInstance)).To(Succeed())

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithLongInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(9))
			testutils.VerifyStatusCondition(conditions[6], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			testutils.VerifyStatusCondition(conditions[7], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.InProgress),
				Message: "Provisioning cluster",
			})
			testutils.VerifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.ClusterNotReady),
				Message: "The Cluster is not yet ready",
			})

			// Verify the start timestamp has been set for ClusterInstance
			Expect(reconciledCR.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt).ToNot(BeZero())
			// Verify the nonCompliantAt timestamp is not set, even though Non-compliant enforce policy exists
			// but Cluster is not ready
			Expect(reconciledCR.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())
			// Verify the provisioningState remains progressing when cluster provisioning is in-progress
			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Cluster installation is in progress", nil)
		})

		It("Verify status when ClusterInstance provision has timedout", func() {
			// Initial reconciliation to populate ClusterProvisionStartedAt timestamp
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			cr := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, cr)).To(Succeed())
			// Verify the start timestamp has been set for ClusterInstance
			Expect(cr.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt).ToNot(BeZero())
			// Verify the nonCompliantAt timestamp is not set, even though Non-compliant enforce policy exists
			// but Cluster is not ready
			Expect(cr.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())

			// Patch ClusterProvisionStartedAt timestamp to mock timeout
			cr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{Name: "cluster-1"}
			cr.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt = &metav1.Time{Time: metav1.Now().Add(-2 * time.Minute)}
			Expect(c.Status().Update(ctx, cr)).To(Succeed())

			// Start reconciliation again
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue())) // stop reconciliation on ClusterProvision timeout

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(9))
			testutils.VerifyStatusCondition(conditions[7], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(provisioningv1alpha1.CRconditionReasons.TimedOut),
			})
			testutils.VerifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.ClusterNotReady),
				Message: "The Cluster is not yet ready",
			})
			// Verify the start timestamp has been set for HardwareProvisioning
			Expect(reconciledCR.Status.Extensions.NodeAllocationRequestRef.HardwareProvisioningCheckStart).ToNot(BeZero())
			// Verify the provisioningState moves to failed when cluster provisioning times out
			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Cluster installation timed out", nil)
		})

		It("Verify status when ClusterInstance provision has failed", func() {
			// Patch ClusterInstance provisioned status to failed
			crProvisionedCond := metav1.Condition{
				Type: string(siteconfig.ClusterProvisioned), Status: metav1.ConditionFalse,
				Reason: string(siteconfig.Failed), Message: "Provisioning failed",
			}
			clusterInstance.Status.Conditions = append(clusterInstance.Status.Conditions, crProvisionedCond)
			Expect(c.Status().Update(ctx, clusterInstance)).To(Succeed())

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue())) // stop reconciliation on ClusterProvision failed

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(8))
			testutils.VerifyStatusCondition(conditions[7], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Failed),
			})
			// Verify the provisioningState moves to failed when cluster provisioning is failed
			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Cluster installation failed", nil)
		})

		It("Verify status when ClusterInstance provision has completed, ManagedCluster becomes ready and non-compliant enforce policy is being applied", func() {
			// Patch ClusterInstance provisioned status to Provisioned
			crProvisionedCond := metav1.Condition{
				Type: string(siteconfig.ClusterProvisioned), Status: metav1.ConditionTrue,
				Reason: string(siteconfig.Completed), Message: "Provisioning completed",
			}
			clusterInstance.Status.Conditions = append(clusterInstance.Status.Conditions, crProvisionedCond)
			Expect(c.Status().Update(ctx, clusterInstance)).To(Succeed())
			// Patch ManagedCluster to ready
			readyCond := meta.FindStatusCondition(
				managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
			readyCond.Status = metav1.ConditionTrue
			Expect(c.Status().Update(ctx, managedCluster)).To(Succeed())
			// Patch ManagedCluster with clusterID label
			managedCluster.SetLabels(map[string]string{"clusterID": "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})
			Expect(c.Update(ctx, managedCluster)).To(Succeed())

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithLongInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(9))
			testutils.VerifyStatusCondition(conditions[7], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			testutils.VerifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.InProgress),
				Message: "The configuration is still being applied",
			})

			// Verify the start timestamp has been set for ClusterInstance
			Expect(reconciledCR.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt).ToNot(BeZero())
			// Verify the oCloudNodeClusterId is not stored when cluster configuration is still in-progress
			Expect(reconciledCR.Status.ProvisioningStatus.ProvisionedResources).To(BeNil())
			// Verify the provisioningState remains progressing when cluster configuration is in-progress
			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Cluster configuration is being applied", nil)

			// Check that the templateId label was not added for the ManagedCluster and the Agent CRs at
			// this point.
			mcl := &clusterv1.ManagedCluster{}
			err = c.Get(ctx, types.NamespacedName{Name: crName}, mcl)
			Expect(err).ToNot(HaveOccurred())
			Expect(mcl.GetLabels()).To(Not(HaveKey(utils.ClusterTemplateArtifactsLabel)))

			// Check that the new label was added and the old label was kept for the Agent CR.
			agent := &assistedservicev1beta1.Agent{}
			err = c.Get(ctx, types.NamespacedName{Name: agentName, Namespace: crName}, agent)
			Expect(err).ToNot(HaveOccurred())
			Expect(agent.GetLabels()).To(Not(HaveKey(utils.ClusterTemplateArtifactsLabel)))
		})

		It("Verify status when ClusterInstance provision has completed, ManagedCluster becomes ready and configuration policy becomes compliant", func() {
			// Patch ClusterInstance provisioned status to Completed
			crProvisionedCond := metav1.Condition{
				Type: string(siteconfig.ClusterProvisioned), Status: metav1.ConditionTrue,
				Reason: string(siteconfig.Completed), Message: "Provisioning completed",
			}
			clusterInstance.Status.Conditions = append(clusterInstance.Status.Conditions, crProvisionedCond)
			Expect(c.Status().Update(ctx, clusterInstance)).To(Succeed())
			// Patch ManagedCluster to ready
			readyCond := meta.FindStatusCondition(
				managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
			readyCond.Status = metav1.ConditionTrue
			Expect(c.Status().Update(ctx, managedCluster)).To(Succeed())
			// Patch ManagedCluster with clusterID label
			managedCluster.SetLabels(map[string]string{"clusterID": "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})
			Expect(c.Update(ctx, managedCluster)).To(Succeed())

			// Patch enforce policy to Compliant
			policy.Status.ComplianceState = policiesv1.Compliant
			Expect(c.Status().Update(ctx, policy)).To(Succeed())

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify the ProvisioningRequest's status conditions
			Expect(len(conditions)).To(Equal(9))
			testutils.VerifyStatusCondition(conditions[7], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			testutils.VerifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionTrue,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Completed),
				Message: "The configuration is up to date",
			})

			// Verify the start timestamp is not cleared even Cluster provision has completed
			Expect(reconciledCR.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt).ToNot(BeZero())
			// Verify the nonCompliantAt timestamp is not set since enforce policy is compliant
			Expect(reconciledCR.Status.Extensions.ClusterDetails.NonCompliantAt).To(BeZero())
			// Verify the ztpStatus is set to ZTP done
			Expect(reconciledCR.Status.Extensions.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpDone))
			// Verify the provisioningState sets to fulfilled when the provisioning process is completed
			// and oCloudNodeClusterId is stored
			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFulfilled, "Provisioning request has completed successfully",
				&provisioningv1alpha1.ProvisionedResources{OCloudNodeClusterId: "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})

			// Check that the templateId label was added for the ManagedCluster and the Agent CRs at this point.
			mcl := &clusterv1.ManagedCluster{}
			err = c.Get(ctx, types.NamespacedName{Name: crName}, mcl)
			Expect(err).ToNot(HaveOccurred())
			Expect(mcl.GetLabels()).To(HaveKeyWithValue(
				utils.ClusterTemplateArtifactsLabel, "57b39bda-ac56-4143-9b10-d1a71517d04f"))

			// Check that the new label was added and the old label was kept for the Agent CR.
			agent := &assistedservicev1beta1.Agent{}
			err = c.Get(ctx, types.NamespacedName{Name: agentName, Namespace: crName}, agent)
			Expect(err).ToNot(HaveOccurred())
			Expect(agent.GetLabels()).To(Equal(map[string]string{
				utils.ClusterTemplateArtifactsLabel:                      "57b39bda-ac56-4143-9b10-d1a71517d04f",
				"agent-install.openshift.io/clusterdeployment-namespace": crName,
			}))
		})

		It("Verify status when configuration change causes ProvisioningRequest validation to fail but ClusterInstall is still in progress", func() {
			// Patch ClusterInstance provisioned status to InProgress
			crProvisionedCond := metav1.Condition{
				Type: string(siteconfig.ClusterProvisioned), Status: metav1.ConditionFalse,
				Reason: string(siteconfig.InProgress), Message: "Provisioning cluster",
			}
			clusterInstance.Status.Conditions = append(clusterInstance.Status.Conditions, crProvisionedCond)
			Expect(c.Status().Update(ctx, clusterInstance)).To(Succeed())

			// Initial reconciliation to populate initial status.
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			// Remove required field hostname to fail ProvisioningRequest validation.
			testutils.RemoveRequiredFieldFromClusterInstanceInput(ctx, c, crName)

			// Start reconciliation again
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithLongInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify that the Validated condition fails but ClusterProvisioned condition
			// is also up-to-date with the current status timeout.
			Expect(len(conditions)).To(Equal(9))
			testutils.VerifyStatusCondition(conditions[0], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.Validated),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Failed),
				Message: "nodes.0: hostName is required",
			})
			testutils.VerifyStatusCondition(conditions[7], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(provisioningv1alpha1.CRconditionReasons.InProgress),
			})
			testutils.VerifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.ClusterNotReady),
				Message: "The Cluster is not yet ready",
			})
			// Verify the provisioningState remains progressing to reflect the on-going provisioning process
			// even if new changes cause validation to fail
			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Cluster installation is in progress", nil)
		})

		It("Verify status when configuration change causes ProvisioningRequest validation to fail but ClusterInstance "+
			"becomes provisioned and policy configuration is still being applied", func() {
			// Initial reconciliation
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			// Patch ClusterInstance provisioned status to Completed.
			crProvisionedCond := metav1.Condition{
				Type: string(siteconfig.ClusterProvisioned), Status: metav1.ConditionTrue,
				Reason: string(siteconfig.Completed), Message: "Provisioning completed",
			}
			currentCI := &siteconfig.ClusterInstance{}
			Expect(c.Get(ctx, types.NamespacedName{Name: crName, Namespace: crName}, currentCI)).To(Succeed())
			currentCI.Status.Conditions = append(clusterInstance.Status.Conditions, crProvisionedCond)
			Expect(c.Status().Update(ctx, currentCI)).To(Succeed())
			// Patch ManagedCluster to ready
			readyCond := meta.FindStatusCondition(
				managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
			readyCond.Status = metav1.ConditionTrue
			Expect(c.Status().Update(ctx, managedCluster)).To(Succeed())
			// Patch ManagedCluster with clusterID label
			managedCluster.SetLabels(map[string]string{"clusterID": "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})
			Expect(c.Update(ctx, managedCluster)).To(Succeed())

			// Remove required field hostname to fail ProvisioningRequest validation.
			testutils.RemoveRequiredFieldFromClusterInstanceInput(ctx, c, crName)

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithLongInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify that the Validated condition fails but ClusterProvisioned condition
			// has changed to Completed.
			Expect(len(conditions)).To(Equal(9))
			testutils.VerifyStatusCondition(conditions[0], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.Validated),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Failed),
				Message: "nodes.0: hostName is required",
			})
			testutils.VerifyStatusCondition(conditions[7], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			testutils.VerifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.InProgress),
				Message: "The configuration is still being applied",
			})

			// Verify the start timestamp is not cleared even Cluster provision has completed
			Expect(reconciledCR.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt).ToNot(BeZero())
			// Verify the provisioningState remains progressing to reflect to the on-going provisioning process
			// even if new changes cause validation to fail.
			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Cluster configuration is being applied", nil)
		})

		It("Verify status when configuration change causes ProvisioningRequest validation to fail but ClusterProvision becomes timeout", func() {
			// Initial reconciliation to populate initial status.
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			cr := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, cr)).To(Succeed())
			// Verify the start timestamp has been set for ClusterInstance.
			Expect(cr.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt).ToNot(BeZero())
			// Patch ClusterProvisionStartedAt timestamp to mock timeout
			cr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{Name: "cluster-1"}
			cr.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt = &metav1.Time{Time: metav1.Now().Add(-2 * time.Minute)}
			Expect(c.Status().Update(ctx, cr)).To(Succeed())

			// Remove required field hostname to fail ProvisioningRequest validation.
			testutils.RemoveRequiredFieldFromClusterInstanceInput(ctx, c, crName)

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue())) // stop reconciliation on ClusterProvision timeout

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify that the Validated condition fails but ClusterProvisioned condition
			// is also up-to-date with the current status timeout.
			Expect(len(conditions)).To(Equal(9))
			testutils.VerifyStatusCondition(conditions[0], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.Validated),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Failed),
				Message: "nodes.0: hostName is required",
			})
			testutils.VerifyStatusCondition(conditions[7], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionFalse,
				Reason: string(provisioningv1alpha1.CRconditionReasons.TimedOut),
			})
			testutils.VerifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.ClusterNotReady),
				Message: "The Cluster is not yet ready",
			})
			// Verify the provisioningPhase has changed to failed with the reason timeout
			// as on-going provisioning process is timedout.
			// Although new changes cause validation to fail as well, the provisioningPhase
			// should remain failed with the on-going provisioning failed reason.
			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Cluster installation timed out", nil)
		})

		It("Verify status when configuration change causes ClusterInstance rendering to fail but configuration policy becomes compliant", func() {
			// Initial reconciliation
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			// Patch ClusterInstance provisioned status to Completed
			crProvisionedCond := metav1.Condition{
				Type: string(siteconfig.ClusterProvisioned), Status: metav1.ConditionTrue,
				Reason: string(siteconfig.Completed), Message: "Provisioning completed",
			}
			currentCI := &siteconfig.ClusterInstance{}
			Expect(c.Get(ctx, types.NamespacedName{Name: crName, Namespace: crName}, currentCI)).To(Succeed())
			currentCI.Status.Conditions = append(clusterInstance.Status.Conditions, crProvisionedCond)
			Expect(c.Status().Update(ctx, currentCI)).To(Succeed())
			// Patch ManagedCluster to ready
			readyCond := meta.FindStatusCondition(
				managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
			readyCond.Status = metav1.ConditionTrue
			Expect(c.Status().Update(ctx, managedCluster)).To(Succeed())
			// Patch ManagedCluster with clusterID label
			managedCluster.SetLabels(map[string]string{"clusterID": "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})
			Expect(c.Update(ctx, managedCluster)).To(Succeed())
			// Patch enforce policy to Compliant
			policy.Status.ComplianceState = policiesv1.Compliant
			Expect(c.Status().Update(ctx, policy)).To(Succeed())

			// Start reconciliation to complete the ProvisioningRequest.
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			// Fail the ClusterInstance rendering.
			testutils.RemoveRequiredFieldFromClusterInstanceCm(ctx, c, ciDefaultsCm, ctNamespace)

			// Start reconciliation.
			result, err = reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			// Verify that the ClusterInstanceRendered condition fails but configurationApplied
			// has changed to Completed
			Expect(len(conditions)).To(Equal(9))
			testutils.VerifyStatusCondition(conditions[1], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceRendered),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Failed),
				Message: "detected disallowed changes in immutable fields: nodes.0.templateRefs",
			})
			testutils.VerifyStatusCondition(conditions[7], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			testutils.VerifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionTrue,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Completed),
				Message: "The configuration is up to date",
			})

			// Verify the ztpStatus is set to ZTP done
			Expect(reconciledCR.Status.Extensions.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpDone))
			// Verify the oCloudNodeClusterId is stored and the provisioningState has changed to failed,
			// as on-going provisioning process has fulfilled and new changes cause rendering to fail.
			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Failed to render and validate ClusterInstance",
				&provisioningv1alpha1.ProvisionedResources{OCloudNodeClusterId: "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})
		})

		It("Verify status when configuration change causes ProvisioningRequest validation to fail after provisioning has fulfilled", func() {
			// Patch ClusterInstance provisioned status to Completed
			crProvisionedCond := metav1.Condition{
				Type: string(siteconfig.ClusterProvisioned), Status: metav1.ConditionTrue,
				Reason: string(siteconfig.Completed), Message: "Provisioning completed",
			}
			currentCI := &siteconfig.ClusterInstance{}
			Expect(c.Get(ctx, types.NamespacedName{Name: crName, Namespace: crName}, currentCI)).To(Succeed())
			currentCI.Status.Conditions = append(clusterInstance.Status.Conditions, crProvisionedCond)
			Expect(c.Status().Update(ctx, currentCI)).To(Succeed())
			// Patch ManagedCluster to ready
			readyCond := meta.FindStatusCondition(
				managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
			readyCond.Status = metav1.ConditionTrue
			Expect(c.Status().Update(ctx, managedCluster)).To(Succeed())
			// Patch ManagedCluster with clusterID label
			managedCluster.SetLabels(map[string]string{"clusterID": "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})
			Expect(c.Update(ctx, managedCluster)).To(Succeed())
			// Patch enforce policy to Compliant
			policy.Status.ComplianceState = policiesv1.Compliant
			Expect(c.Status().Update(ctx, policy)).To(Succeed())
			// Initial reconciliation to fulfill the provisioning
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			// Remove required field hostname to fail ProvisioningRequest validation.
			testutils.RemoveRequiredFieldFromClusterInstanceInput(ctx, c, crName)

			// Start reconciliation again
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			Expect(len(conditions)).To(Equal(9))
			testutils.VerifyStatusCondition(conditions[7], metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			})
			testutils.VerifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionTrue,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Completed),
				Message: "The configuration is up to date",
			})

			// Verify the ztpStatus is still set to ZTP done
			Expect(reconciledCR.Status.Extensions.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpDone))
			// Verify the oCloudNodeClusterId is still stored
			Expect(reconciledCR.Status.ProvisioningStatus.ProvisionedResources.OCloudNodeClusterId).To(Equal("76b8cbad-9928-48a0-bcf0-bb16a777b5f7"))
			// Verify the oCloudNodeClusterId is stored and the provisioningState has changed to failed,
			// as on-going provisioning process has fulfilled and new changes cause validation to fail.
			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Failed to validate the ProvisioningRequest",
				&provisioningv1alpha1.ProvisionedResources{OCloudNodeClusterId: "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})
		})

		It("Verify status when enforce policy becomes non-compliant after provisioning has fulfilled", func() {
			// Patch ClusterInstance provisioned status to Completed
			crProvisionedCond := metav1.Condition{
				Type: string(siteconfig.ClusterProvisioned), Status: metav1.ConditionTrue,
				Reason: string(siteconfig.Completed), Message: "Provisioning completed",
			}
			currentCI := &siteconfig.ClusterInstance{}
			Expect(c.Get(ctx, types.NamespacedName{Name: crName, Namespace: crName}, currentCI)).To(Succeed())
			currentCI.Status.Conditions = append(clusterInstance.Status.Conditions, crProvisionedCond)
			Expect(c.Status().Update(ctx, currentCI)).To(Succeed())
			// Patch ManagedCluster to ready
			readyCond := meta.FindStatusCondition(
				managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
			readyCond.Status = metav1.ConditionTrue
			Expect(c.Status().Update(ctx, managedCluster)).To(Succeed())
			// Patch ManagedCluster with clusterID label
			managedCluster.SetLabels(map[string]string{"clusterID": "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})
			Expect(c.Update(ctx, managedCluster)).To(Succeed())
			// Patch enforce policy to Compliant
			policy.Status.ComplianceState = policiesv1.Compliant
			Expect(c.Status().Update(ctx, policy)).To(Succeed())

			// Initial reconciliation to fulfill the provisioning
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			// Patch enforce policy to non-Compliant
			policy.Status.ComplianceState = policiesv1.NonCompliant
			Expect(c.Status().Update(ctx, policy)).To(Succeed())

			// Start reconciliation again
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithLongInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			conditions := reconciledCR.Status.Conditions

			Expect(len(conditions)).To(Equal(9))
			testutils.VerifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.InProgress),
				Message: "The configuration is still being applied",
			})

			// Verify the ztpStatus is still set to ZTP done
			Expect(reconciledCR.Status.Extensions.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpDone))
			// Verify the oCloudNodeClusterId is still stored and the provisioningState becomes to progressing since configuration is in-progress
			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Cluster configuration is being applied",
				&provisioningv1alpha1.ProvisionedResources{OCloudNodeClusterId: "76b8cbad-9928-48a0-bcf0-bb16a777b5f7"})
		})
	})

	Context("When hw template is updated", func() {
		var (
			hwTemplateName        = "hw-template-updated"
			hwTemplate            *hwmgmtv1alpha1.HardwareTemplate
			nodeAllocationRequest *pluginsv1alpha1.NodeAllocationRequest
			tVersion              = "v1.0.0-1"
		)

		BeforeEach(func() {
			hwTemplate = &hwmgmtv1alpha1.HardwareTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      hwTemplateName,
					Namespace: utils.InventoryNamespace,
				},
				Spec: hwmgmtv1alpha1.HardwareTemplateSpec{
					HardwarePluginRef:           utils.UnitTestHwPluginRef,
					BootInterfaceLabel:          "bootable-interface",
					HardwareProvisioningTimeout: "1m",
					NodeGroupData: []hwmgmtv1alpha1.NodeGroupData{
						{
							Name:           "controller",
							Role:           "master",
							ResourcePoolId: "xyz",
							HwProfile:      "profile-spr-single-processor-64G-v2", // updated hw profile
						},
						{
							Name:           "worker",
							Role:           "worker",
							ResourcePoolId: "xyz",
							HwProfile:      "profile-spr-dual-processor-128G",
						},
					},
					Extensions: map[string]string{
						"resourceTypeId": "ResourceGroup~2.1.1",
					},
				},
			}
			Expect(c.Create(ctx, hwTemplate)).To(Succeed())

			// Create a new ClusterTemplate with the updated hw template
			ct := &provisioningv1alpha1.ClusterTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      GetClusterTemplateRefName(tName, tVersion),
					Namespace: ctNamespace,
				},
				Spec: provisioningv1alpha1.ClusterTemplateSpec{
					Name:       tName,
					Version:    tVersion,
					TemplateID: "58b39bda-ac56-4143-9b10-d1a71517d04f",
					Templates: provisioningv1alpha1.Templates{
						ClusterInstanceDefaults: ciDefaultsCm,
						PolicyTemplateDefaults:  ptDefaultsCm,
						HwTemplate:              hwTemplateName,
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
			}
			Expect(c.Create(ctx, ct)).To(Succeed())

			// Create NodeAllocationRequest resource that has provisioned
			nodeAllocationRequest = &pluginsv1alpha1.NodeAllocationRequest{}
			nodeAllocationRequest.SetName(crName)
			nodeAllocationRequest.SetNamespace(utils.UnitTestHwmgrNamespace)
			nodeAllocationRequest.Spec.HardwarePluginRef = utils.UnitTestHwPluginRef
			nodeAllocationRequest.Spec.NodeGroup = []pluginsv1alpha1.NodeGroup{
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
					Name:           "controller",
					Role:           "master",
					HwProfile:      "profile-spr-single-processor-64G",
					ResourcePoolId: "xyz",
				},
					Size: 1,
				},
				{NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
					Name:           "worker",
					Role:           "worker",
					HwProfile:      "profile-spr-dual-processor-128G",
					ResourcePoolId: "xyz",
				},
					Size: 0,
				},
			}
			nodeAllocationRequest.Status.Conditions = []metav1.Condition{
				{Type: string(hwmgmtv1alpha1.Provisioned), Status: metav1.ConditionTrue, Reason: string(hwmgmtv1alpha1.Completed)},
			}
			nodeAllocationRequest.Status.Properties = hwmgmtv1alpha1.Properties{NodeNames: []string{testutils.MasterNodeName}}
			nodeAllocationRequest.Annotations = map[string]string{hwmgmtv1alpha1.BootInterfaceLabelAnnotation: "bootable-interface"}
			Expect(c.Create(ctx, nodeAllocationRequest)).To(Succeed())
			testutils.CreateNodeResources(ctx, c, nodeAllocationRequest.Name)
			cr.Status.Extensions.NodeAllocationRequestRef = &provisioningv1alpha1.NodeAllocationRequestRef{
				NodeAllocationRequestID:        nodeAllocationRequest.Name,
				HardwareProvisioningCheckStart: &metav1.Time{Time: time.Now()},
			}
			Expect(c.Status().Update(ctx, cr)).To(Succeed())

			// Update the provisioningRequest to the new ClusterTemplate
			cr.Spec.TemplateVersion = tVersion
			Expect(c.Update(ctx, cr)).To(Succeed())
		})

		It("should update the status to unknown when nodeAllocationRequest does not have configured condition", func() {
			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithMediumInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())

			// Verify the nodeAllocationRequest change is detected and configuration check start time is set
			Expect(reconciledCR.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart).ToNot(BeNil())
			hwConfiguredCond := meta.FindStatusCondition(
				reconciledCR.Status.Conditions,
				string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured))
			Expect(hwConfiguredCond).ToNot(BeNil())
			testutils.VerifyStatusCondition(*hwConfiguredCond, metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured),
				Status:  metav1.ConditionUnknown,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Unknown),
				Message: "Waiting for NodeAllocationRequest (cluster-1) to be processed",
			})
		})

		It("should update the status to InProgress when nodeAllocationRequest has configured condition in progress", func() {
			// Set the configured condition to in progress
			nodeAllocationRequest.Status.Conditions = append(nodeAllocationRequest.Status.Conditions, metav1.Condition{
				Type:    string(hwmgmtv1alpha1.Configured),
				Status:  metav1.ConditionFalse,
				Reason:  string(hwmgmtv1alpha1.InProgress),
				Message: "Hardware configuring is in progress",
			})
			Expect(c.Status().Update(ctx, nodeAllocationRequest)).To(Succeed())

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithMediumInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())

			// Verify the nodeAllocationRequest change is detected and configuration check start time is set
			Expect(reconciledCR.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart).ToNot(BeNil())
			hwConfiguredCond := meta.FindStatusCondition(
				reconciledCR.Status.Conditions,
				string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured))
			Expect(hwConfiguredCond).ToNot(BeNil())
			testutils.VerifyStatusCondition(*hwConfiguredCond, metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.InProgress),
				Message: "Hardware configuring is in progress",
			})
		})

		It("should update the status to completed when nodeAllocationRequest has configured condition completed", func() {
			// Set the configured condition to completed
			nodeAllocationRequest.Status.Conditions = append(nodeAllocationRequest.Status.Conditions, metav1.Condition{
				Type:    string(hwmgmtv1alpha1.Configured),
				Status:  metav1.ConditionTrue,
				Reason:  string(hwmgmtv1alpha1.ConfigApplied),
				Message: "Configuration has been applied successfully",
			})
			Expect(c.Status().Update(ctx, nodeAllocationRequest)).To(Succeed())

			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())

			// Verify configuration check start time is reset
			Expect(reconciledCR.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart).To(BeNil())
			hwConfiguredCond := meta.FindStatusCondition(
				reconciledCR.Status.Conditions,
				string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured))
			Expect(hwConfiguredCond).ToNot(BeNil())
			testutils.VerifyStatusCondition(*hwConfiguredCond, metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured),
				Status:  metav1.ConditionTrue,
				Reason:  string(hwmgmtv1alpha1.ConfigApplied),
				Message: "Configuration has been applied successfully",
			})
		})
	})

	Context("When evaluating ZTP Done", func() {
		var (
			policy         *policiesv1.Policy
			managedCluster *clusterv1.ManagedCluster
		)

		BeforeEach(func() {
			policy = &policiesv1.Policy{
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
					ComplianceState: policiesv1.NonCompliant,
				},
			}
			Expect(c.Create(ctx, policy)).To(Succeed())

			managedCluster = &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
				},
				Spec: clusterv1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
				Status: clusterv1.ManagedClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   clusterv1.ManagedClusterConditionAvailable,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.ManagedClusterConditionHubAccepted,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.ManagedClusterConditionJoined,
							Status: metav1.ConditionTrue,
						},
					},
				},
			}
			Expect(c.Create(ctx, managedCluster)).To(Succeed())

			agent := &assistedservicev1beta1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "agent-for-cluster-1",
					Namespace: "cluster-1",
					Labels: map[string]string{
						"agent-install.openshift.io/clusterdeployment-namespace": "cluster-1",
					},
				},
				Spec: assistedservicev1beta1.AgentSpec{
					Approved: true,
					ClusterDeploymentName: &assistedservicev1beta1.ClusterReference{
						Name:      "cluster-1",
						Namespace: "cluster-1",
					},
				},
			}
			Expect(c.Create(ctx, agent)).To(Succeed())

			nodeAllocationRequest := &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-1",
					Namespace: utils.UnitTestHwmgrNamespace,
					Annotations: map[string]string{
						hwmgmtv1alpha1.BootInterfaceLabelAnnotation: "bootable-interface",
					},
				},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					HardwarePluginRef: utils.UnitTestHwPluginRef,
					// Ensure that the NodeGroup matches the data in the hwTemplate
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
					Properties: hwmgmtv1alpha1.Properties{
						NodeNames: []string{testutils.MasterNodeName},
					},
				},
			}
			Expect(c.Create(ctx, nodeAllocationRequest)).To(Succeed())
			testutils.CreateNodeResources(ctx, c, nodeAllocationRequest.Name)

			provisionedCond := metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionFalse,
			}
			cr.Status.Conditions = append(cr.Status.Conditions, provisionedCond)
			cr.Status.Extensions.NodeAllocationRequestRef = &provisioningv1alpha1.NodeAllocationRequestRef{
				NodeAllocationRequestID:        nodeAllocationRequest.Name,
				HardwareProvisioningCheckStart: &metav1.Time{Time: time.Now().Add(-time.Minute)},
			}
			cr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{}
			cr.Status.Extensions.ClusterDetails.Name = crName
			cr.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt = &metav1.Time{Time: time.Now()}
			Expect(c.Status().Update(ctx, cr)).To(Succeed())
		})

		It("Sets the status to ZTP Not Done", func() {
			// Start reconciliation
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithLongInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())

			Expect(reconciledCR.Status.Extensions.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpNotDone))
			conditions := reconciledCR.Status.Conditions
			// Verify the ProvisioningRequest's status conditions
			testutils.VerifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.InProgress),
				Message: "The configuration is still being applied",
			})
		})

		It("Sets the status to ZTP Done", func() {
			// Set the policies to compliant.
			policy.Status.ComplianceState = policiesv1.Compliant
			Expect(c.Status().Update(ctx, policy)).To(Succeed())
			// Complete the cluster provisioning.
			cr.Status.Conditions[0].Status = metav1.ConditionTrue
			cr.Status.Conditions[0].Reason = string(provisioningv1alpha1.CRconditionReasons.Completed)
			Expect(c.Status().Update(ctx, cr)).To(Succeed())
			// Start reconciliation.
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result.
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))
			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())
			Expect(reconciledCR.Status.Extensions.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpDone))
			// Verify the ProvisioningRequest's status conditions
			conditions := reconciledCR.Status.Conditions
			testutils.VerifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionTrue,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Completed),
				Message: "The configuration is up to date",
			})
		})

		It("Keeps the ZTP status as ZTP Done if a policy becomes NonCompliant", func() {
			cr.Status.Extensions.ClusterDetails.ZtpStatus = utils.ClusterZtpDone
			Expect(c.Status().Update(ctx, cr)).To(Succeed())
			policy.Status.ComplianceState = policiesv1.NonCompliant
			Expect(c.Status().Update(ctx, policy)).To(Succeed())
			// Start reconciliation.
			result, err := reconciler.Reconcile(ctx, req)
			// Verify the reconciliation result.
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithLongInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())

			Expect(reconciledCR.Status.Extensions.ClusterDetails.ZtpStatus).To(Equal(utils.ClusterZtpDone))
			conditions := reconciledCR.Status.Conditions
			// Verify the ProvisioningRequest's status conditions
			testutils.VerifyStatusCondition(conditions[8], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.ConfigurationApplied),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.InProgress),
				Message: "The configuration is still being applied",
			})
		})
	})

	Context("When handling upgrade", func() {
		var (
			managedCluster    *clusterv1.ManagedCluster
			clusterInstance   *siteconfig.ClusterInstance
			policy            *policiesv1.Policy
			newReleaseVersion string
		)

		BeforeEach(func() {
			newReleaseVersion = "4.16.3"
			managedCluster = &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
					Labels: map[string]string{
						"openshiftVersion": "4.16.0",
					},
				},
				Spec: clusterv1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
				Status: clusterv1.ManagedClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   clusterv1.ManagedClusterConditionAvailable,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.ManagedClusterConditionHubAccepted,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.ManagedClusterConditionJoined,
							Status: metav1.ConditionTrue,
						},
					},
				},
			}
			Expect(c.Create(ctx, managedCluster)).To(Succeed())
			agent := &assistedservicev1beta1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "agent-for-cluster-1",
					Namespace: "cluster-1",
					Labels: map[string]string{
						"agent-install.openshift.io/clusterdeployment-namespace": "cluster-1",
					},
				},
				Spec: assistedservicev1beta1.AgentSpec{
					Approved: true,
					ClusterDeploymentName: &assistedservicev1beta1.ClusterReference{
						Name:      "cluster-1",
						Namespace: "cluster-1",
					},
				},
			}
			Expect(c.Create(ctx, agent)).To(Succeed())

			networkConfig := &assistedservicev1beta1.NMStateConfigSpec{
				NetConfig: assistedservicev1beta1.NetConfig{
					Raw: []byte(
						`
      dns-resolver:
        config:
          server:
          - 192.0.2.22
      interfaces:
      - ipv4:
          address:
          - ip: 192.0.2.10
            prefix-length: 24
          - ip: 192.0.2.11
            prefix-length: 24
          - ip: 192.0.2.12
            prefix-length: 24
          dhcp: false
          enabled: true
        ipv6:
          address:
          - ip: 2001:db8:0:1::42
            prefix-length: 32
          - ip: 2001:db8:0:1::43
            prefix-length: 32
          - ip: 2001:db8:0:1::44
            prefix-length: 32
          dhcp: false
          enabled: true
        name: eno1
        type: ethernet
      - ipv6:
          address:
          - ip: 2001:db8:abcd:1234::1
          enabled: true
          link-aggregation:
            mode: balance-rr
            options:
              miimon: '140'
            slaves:
            - eth0
            - eth1
          prefix-length: 64
        name: bond99
        state: up
        type: bond
      routes:
        config:
        - destination: 0.0.0.0/0
          next-hop-address: 192.0.2.254
          next-hop-interface: eno1
          table-id: 254
                    `,
					),
				},
				Interfaces: []*assistedservicev1beta1.Interface{
					{Name: "eno1", MacAddress: "00:00:00:01:20:30"},
					{Name: "eth0", MacAddress: "02:00:00:80:12:14"},
					{Name: "eth1", MacAddress: "02:00:00:80:12:15"},
				},
			}
			clusterInstance = &siteconfig.ClusterInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-1",
					Namespace: "cluster-1",
				},
				Spec: siteconfig.ClusterInstanceSpec{
					ClusterName:            "cluster-1",
					ClusterImageSetNameRef: "4.16.0",
					PullSecretRef: corev1.LocalObjectReference{
						Name: "pull-secret",
					},
					BaseDomain: "example.com",
					TemplateRefs: []siteconfig.TemplateRef{
						{
							Name:      "ai-cluster-templates-v1",
							Namespace: "siteconfig-operator",
						},
					},
					IngressVIPs: []string{
						"192.0.2.4",
					},
					ApiVIPs: []string{
						"192.0.2.2",
					},
					Nodes: []siteconfig.NodeSpec{
						{
							HostName: "node1",
							TemplateRefs: []siteconfig.TemplateRef{{
								Name: "ai-node-templates-v1", Namespace: "siteconfig-operator",
							}},
							BootMACAddress: "00:00:00:01:20:30",
							BmcCredentialsName: siteconfig.BmcCredentialsName{
								Name: "site-sno-du-1-bmc-secret",
							},
							IronicInspect:         "false",
							Role:                  "master",
							AutomatedCleaningMode: "disabled",
							BootMode:              "UEFI",
							NodeLabels:            map[string]string{"node-role.kubernetes.io/infra": "", "node-role.kubernetes.io/master": ""},
							NodeNetwork:           networkConfig,
							BmcAddress:            "idrac-virtualmedia+https://203.0.113.5/redfish/v1/Systems/System.Embedded.1",
						},
					},
					ExtraAnnotations: map[string]map[string]string{
						"AgentClusterInstall": {
							"extra-annotation-key": "extra-annotation-value",
						},
					},
					CPUPartitioning: siteconfig.CPUPartitioningNone,
					MachineNetwork: []siteconfig.MachineNetworkEntry{
						{
							CIDR: "192.0.2.0/24",
						},
					},
					AdditionalNTPSources: []string{
						"NTP.server1",
					},
					NetworkType:  "OVNKubernetes",
					SSHPublicKey: "ssh-rsa ",
					ServiceNetwork: []siteconfig.ServiceNetworkEntry{
						{
							CIDR: "233.252.0.0/24",
						},
					},
					ExtraLabels: map[string]map[string]string{
						"AgentClusterInstall": {
							"extra-label-key": "extra-label-value",
						},
						"ManagedCluster": {
							"cluster-version": "v4.17",
						},
					},
					HoldInstallation: true,
				},
			}
			Expect(c.Create(ctx, clusterInstance)).To(Succeed())

			upgradeDefaults := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "upgrade-defaults",
					Namespace: ctNamespace,
				},
				Data: map[string]string{
					utils.UpgradeDefaultsConfigmapKey: `
    ibuSpec:
      seedImageRef:
        image: "image"
        version: "4.16.3"
      oadpContent:
        - name: platform-backup-cm
          namespace: openshift-adp
    plan:
      - actions: ["Prep"]
        rolloutStrategy:
          maxConcurrency: 1
          timeout: 10
      - actions: ["AbortOnFailure"]
        rolloutStrategy:
          maxConcurrency: 1
          timeout: 5
      - actions: ["Upgrade"]
        rolloutStrategy:
          maxConcurrency: 1
          timeout: 30
      - actions: ["AbortOnFailure"]
        rolloutStrategy:
          maxConcurrency: 1
          timeout: 5
      - actions: ["FinalizeUpgrade"]
        rolloutStrategy:
          maxConcurrency: 1
          timeout: 5
    `,
				},
			}
			Expect(c.Create(ctx, upgradeDefaults)).To(Succeed())
			clusterInstanceDefaultsV2 := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-instance-defaults-v2",
					Namespace: ctNamespace,
				},
				Data: map[string]string{
					utils.ClusterInstallationTimeoutConfigKey: "60s",
					utils.ClusterInstanceTemplateDefaultsConfigmapKey: `
    clusterImageSetNameRef: "4.16.3"
    cpuPartitioningMode: "None"
    pullSecretRef:
      name: "pull-secret"
    templateRefs:
    - name: "ai-cluster-templates-v1"
      namespace: "siteconfig-operator"
    holdInstallation: true
    networkType: "OVNKubernetes"
    nodes:
    - hostName: "node1"
      ironicInspect: "false"
      automatedCleaningMode: "disabled"
      bootMode: "UEFI"
      role: "master"
      templateRefs:
      - name: "ai-node-templates-v1"
        namespace: "siteconfig-operator"
      nodeNetwork:
        interfaces:
        - name: eno1
          label: bootable-interface
        - name: eth0
          label: base-interface
        - name: eth1
          label: data-interface
    `,
				},
			}
			Expect(c.Create(ctx, clusterInstanceDefaultsV2)).To(Succeed())

			nodeAllocationRequest := &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-1",
					Namespace: utils.UnitTestHwmgrNamespace,
					Annotations: map[string]string{
						hwmgmtv1alpha1.BootInterfaceLabelAnnotation: "bootable-interface",
					},
				},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					HardwarePluginRef: utils.UnitTestHwPluginRef,
					// Ensure that the NodeGroup matches the data in the hwTemplate
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
					Properties: hwmgmtv1alpha1.Properties{
						NodeNames: []string{testutils.MasterNodeName},
					},
				},
			}
			Expect(c.Create(ctx, nodeAllocationRequest)).To(Succeed())
			testutils.CreateNodeResources(ctx, c, nodeAllocationRequest.Name)

			policy = &policiesv1.Policy{
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
					ComplianceState: policiesv1.Compliant,
				},
			}
			Expect(c.Create(ctx, policy)).To(Succeed())

			provisionedCond := metav1.Condition{
				Type:   string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CRconditionReasons.Completed),
			}
			cr.Status.Conditions = append(cr.Status.Conditions, provisionedCond)
			cr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{}
			cr.Status.Extensions.ClusterDetails.Name = crName
			cr.Status.Extensions.ClusterDetails.ClusterProvisionStartedAt = &metav1.Time{Time: time.Now()}
			cr.Status.Extensions.ClusterDetails.ZtpStatus = utils.ClusterZtpDone
			cr.Status.Extensions.NodeAllocationRequestRef = &provisioningv1alpha1.NodeAllocationRequestRef{
				NodeAllocationRequestID:        nodeAllocationRequest.Name,
				HardwareProvisioningCheckStart: &metav1.Time{Time: time.Now().Add(-time.Minute)},
			}
			Expect(c.Status().Update(ctx, cr)).To(Succeed())
			object := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, object)).To(Succeed())
			object.Spec.TemplateVersion = "v3.0.0"
			Expect(c.Update(ctx, object)).To(Succeed())

			ctNew := &provisioningv1alpha1.ClusterTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      GetClusterTemplateRefName(tName, object.Spec.TemplateVersion),
					Namespace: ctNamespace,
				},
				Spec: provisioningv1alpha1.ClusterTemplateSpec{
					Name:       tName,
					Version:    object.Spec.TemplateVersion,
					Release:    newReleaseVersion,
					TemplateID: "57b39bda-ac56-4143-9b10-d1a71517d04f",
					Templates: provisioningv1alpha1.Templates{
						ClusterInstanceDefaults: "cluster-instance-defaults-v2",
						PolicyTemplateDefaults:  ptDefaultsCm,
						HwTemplate:              hwTemplate,
						UpgradeDefaults:         "upgrade-defaults",
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
			}
			Expect(c.Create(ctx, ctNew)).To(Succeed())
		})

		It("Creates ImageBasedUpgrade", func() {
			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithMediumInterval()))

			// check ProvisioningRequest conditions
			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())

			testutils.VerifyStatusCondition(reconciledCR.Status.Conditions[9], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.InProgress),
				Message: "Upgrade is in progress",
			})

			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Cluster upgrade is in progress",
				nil)

			// check ClusterInstance fields
			ci := &siteconfig.ClusterInstance{}
			Expect(c.Get(ctx, types.NamespacedName{Namespace: "cluster-1", Name: "cluster-1"}, ci)).To(Succeed())

			Expect(ci.Spec.ClusterImageSetNameRef).To(Equal(newReleaseVersion))
			Expect(ci.Spec.SuppressedManifests).To(Equal(utils.CRDsToBeSuppressedForUpgrade))

			// check IBGU fields
			ibgu := &ibguv1alpha1.ImageBasedGroupUpgrade{}
			Expect(c.Get(ctx, types.NamespacedName{Namespace: "cluster-1", Name: "cluster-1"}, ibgu)).To(Succeed())
			Expect(ibgu.Spec.IBUSpec.SeedImageRef.Image).To(Equal("image"))
			Expect(ibgu.Spec.IBUSpec.SeedImageRef.Version).To(Equal(newReleaseVersion))
			Expect(len(ibgu.Spec.IBUSpec.OADPContent)).To(Equal(1))
			Expect(len(ibgu.Spec.Plan)).To(Equal(5))
		})

		It("Checks IBGU is in progress", func() {

			ibgu := &ibguv1alpha1.ImageBasedGroupUpgrade{ObjectMeta: metav1.ObjectMeta{
				Name: "cluster-1", Namespace: "cluster-1",
			}}
			Expect(c.Create(ctx, ibgu)).To(Succeed())

			clusterInstance.Spec.ClusterImageSetNameRef = newReleaseVersion
			clusterInstance.Spec.SuppressedManifests = utils.CRDsToBeSuppressedForUpgrade
			Expect(c.Update(ctx, clusterInstance)).To(Succeed())

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(requeueWithMediumInterval()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())

			testutils.VerifyStatusCondition(reconciledCR.Status.Conditions[9], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.InProgress),
				Message: "Upgrade is in progress",
			})

			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateProgressing, "Cluster upgrade is in progress",
				nil)

			// checks SuppressedManifests are not wiped
			ci := &siteconfig.ClusterInstance{}
			Expect(c.Get(ctx, types.NamespacedName{Namespace: "cluster-1", Name: "cluster-1"}, ci)).To(Succeed())
			Expect(ci.Spec.SuppressedManifests).To(Equal(utils.CRDsToBeSuppressedForUpgrade))
		})

		It("Checks IBGU is completed", func() {

			ibgu := &ibguv1alpha1.ImageBasedGroupUpgrade{ObjectMeta: metav1.ObjectMeta{
				Name: "cluster-1", Namespace: "cluster-1",
			},
				Spec: ibguv1alpha1.ImageBasedGroupUpgradeSpec{
					IBUSpec: lcav1.ImageBasedUpgradeSpec{
						SeedImageRef: lcav1.SeedImageRef{
							Version: newReleaseVersion,
						},
					},
				},
				Status: ibguv1alpha1.ImageBasedGroupUpgradeStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Progressing",
							Status: "False",
						},
					},
				}}
			Expect(c.Create(ctx, ibgu)).To(Succeed())

			clusterInstance.Spec.ClusterImageSetNameRef = newReleaseVersion
			clusterInstance.Spec.SuppressedManifests = utils.CRDsToBeSuppressedForUpgrade
			Expect(c.Update(ctx, clusterInstance)).To(Succeed())

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())

			testutils.VerifyStatusCondition(reconciledCR.Status.Conditions[9], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted),
				Status:  metav1.ConditionTrue,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Completed),
				Message: "Upgrade is completed",
			})

			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFulfilled, "Provisioning request has completed successfully",
				nil)
			Expect(c.Get(ctx, types.NamespacedName{Namespace: "cluster-1", Name: "cluster-1"}, ibgu)).To(Not(Succeed()))
		})
		It("Checks IBGU is failed", func() {

			ibgu := &ibguv1alpha1.ImageBasedGroupUpgrade{ObjectMeta: metav1.ObjectMeta{
				Name: "cluster-1", Namespace: "cluster-1",
			},
				Spec: ibguv1alpha1.ImageBasedGroupUpgradeSpec{
					IBUSpec: lcav1.ImageBasedUpgradeSpec{
						SeedImageRef: lcav1.SeedImageRef{
							Version: newReleaseVersion,
						},
					},
				},
				Status: ibguv1alpha1.ImageBasedGroupUpgradeStatus{
					Clusters: []ibguv1alpha1.ClusterState{
						{
							Name: "cluster-1",
							FailedActions: []ibguv1alpha1.ActionMessage{
								{
									Action:  "Prep",
									Message: "pre-cache failed",
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   "Progressing",
							Status: "False",
						},
					},
				}}
			Expect(c.Create(ctx, ibgu)).To(Succeed())

			clusterInstance.Spec.ClusterImageSetNameRef = newReleaseVersion
			clusterInstance.Spec.SuppressedManifests = utils.CRDsToBeSuppressedForUpgrade
			Expect(c.Update(ctx, clusterInstance)).To(Succeed())

			// Patch the policy to NonCompliant
			policy.Status.ComplianceState = policiesv1.NonCompliant
			Expect(c.Status().Update(ctx, policy)).To(Succeed())

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			// Upgrade failed, it does not requeue even if the policy is NonCompliant
			Expect(result).To(Equal(doNotRequeue()))

			reconciledCR := &provisioningv1alpha1.ProvisioningRequest{}
			Expect(c.Get(ctx, req.NamespacedName, reconciledCR)).To(Succeed())

			testutils.VerifyStatusCondition(reconciledCR.Status.Conditions[9], metav1.Condition{
				Type:    string(provisioningv1alpha1.PRconditionTypes.UpgradeCompleted),
				Status:  metav1.ConditionFalse,
				Reason:  string(provisioningv1alpha1.CRconditionReasons.Failed),
				Message: "Upgrade Failed: Action Prep failed: pre-cache failed",
			})

			testutils.VerifyProvisioningStatus(reconciledCR.Status.ProvisioningStatus,
				provisioningv1alpha1.StateFailed, "Cluster upgrade is failed",
				nil)
		})
	})
})
*/
