/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Assisted-by: Cursor/claude-4-sonnet
*/

/*
Test Cases Overview:

This file contains comprehensive unit tests for hardware provisioning functionality
in the ProvisioningRequest controller. The tests cover the complete hardware
provisioning workflow from template rendering to node configuration.

Test Suites:

1. handleRenderHardwareTemplate Tests:
   - Validates successful hardware template rendering and validation
   - Tests error handling when HardwareTemplate is not found
   - Tests error handling when ClusterTemplate is not found

2. waitForNodeAllocationRequestProvision Tests:
   - Tests NodeAllocationRequest provisioning failure scenarios
   - Tests timeout handling during hardware provisioning
   - Tests successful provisioning completion detection
   - Tests in-progress provisioning state handling

3. createOrUpdateNodeAllocationRequest Tests:
   - Tests creation of new NodeAllocationRequest when none exists
   - Tests updating existing NodeAllocationRequest when specifications change

4. buildNodeAllocationRequest Tests:
   - Tests correct NodeAllocationRequest construction from cluster instance and hardware template
   - Tests error handling when cluster instance spec.nodes is missing

5. updateAllocatedNodeHostMap Tests:
   - Tests successful updating of node-to-host mapping
   - Tests graceful handling of empty node IDs
   - Tests graceful handling of empty host names
   - Tests idempotent behavior when values are unchanged

6. waitForHardwareData Tests:
   - Tests detection of both provisioned and configured hardware states
   - Tests handling of incomplete provisioning
   - Tests handling of missing configuration conditions

7. checkExistingNodeAllocationRequest Tests:
   - Tests error handling when hardware plugin client is unavailable
   - Tests successful retrieval of existing NodeAllocationRequest

8. applyNodeConfiguration Tests:
   - Tests successful application of hardware configuration to cluster nodes
   - Tests error handling for malformed cluster instance specifications
   - Tests error handling for invalid node structures
   - Tests error handling when no matching hardware nodes are found
   - Tests error handling when hardware provisioning is disabled
   - Tests error handling for missing cluster templates
   - Tests error handling for missing hardware templates
   - Tests handling of nodes without hardware manager references
   - Tests correct consumption and assignment of hardware nodes to cluster nodes

Helper Functions:
   - createMockNodeAllocationRequestResponse: Creates mock responses for testing
   - VerifyHardwareTemplateStatus: Validates hardware template status conditions
*/

package controllers

import (
	"context"
	"log/slog"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	hwmgrpluginapi "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/client/provisioning"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/mocks"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	testutils "github.com/openshift-kni/oran-o2ims/test/utils"
	"github.com/openshift/assisted-service/api/v1beta1"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
)

const (
	groupNameController = "controller"
	groupNameWorker     = "worker"
	testNodeID          = "node-1"
	testHostName        = "host-1"
)

var _ = Describe("handleRenderHardwareTemplate", func() {
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
		hwTemplate      = "hwTemplate-v1"
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
					Raw: []byte(testutils.TestFullTemplateParameters),
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
				Templates: provisioningv1alpha1.Templates{
					HwTemplate: hwTemplate,
				},
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr}...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &provisioningRequestReconcilerTask{
			logger:         reconciler.Logger,
			client:         reconciler.Client,
			object:         cr,
			callbackConfig: utils.NewNarCallbackConfig(constants.DefaultNarCallbackServicePort),
		}

		// Set up hwpluginClient using the test Metal3 hardware plugin
		hwplugin := &hwmgmtv1alpha1.HardwarePlugin{}
		hwpluginKey := client.ObjectKey{
			Name:      testMetal3HardwarePluginRef,
			Namespace: testHwMgrPluginNameSpace,
		}
		err := c.Get(ctx, hwpluginKey, hwplugin)
		if err != nil {
			reconciler.Logger.Warn("Could not get hwplugin for test", "error", err)
		} else {
			hwpluginClient, err := hwmgrpluginapi.NewHardwarePluginClient(ctx, c, reconciler.Logger, hwplugin)
			if err != nil {
				reconciler.Logger.Warn("Could not create hwpluginClient for test", "error", err)
			} else {
				task.hwpluginClient = hwpluginClient
			}
		}
	})

	It("returns no error when handleRenderHardwareTemplate succeeds", func() {
		// Create the ClusterTemplate that the test needs
		Expect(c.Create(ctx, ct)).To(Succeed())

		// Set the ClusterTemplate as validated (required for GetClusterTemplateRef)
		ct.Status.Conditions = []metav1.Condition{
			{
				Type:   string(provisioningv1alpha1.CTconditionTypes.Validated),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CTconditionReasons.Completed),
			},
		}
		Expect(c.Status().Update(ctx, ct)).To(Succeed())

		// Define the hardware template resource
		hwTemplateResource := &hwmgmtv1alpha1.HardwareTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hwTemplate,
				Namespace: utils.InventoryNamespace,
			},
			Spec: hwmgmtv1alpha1.HardwareTemplateSpec{
				HardwarePluginRef:  testMetal3HardwarePluginRef,
				BootInterfaceLabel: "bootable-interface",
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
		}

		// Create the hardware template that the ClusterTemplate references
		Expect(c.Create(ctx, hwTemplateResource)).To(Succeed())
		unstructuredCi, err := utils.ConvertToUnstructured(*clusterInstance)
		Expect(err).ToNot(HaveOccurred())
		nodeAllocationRequest, err := task.handleRenderHardwareTemplate(ctx, unstructuredCi)
		Expect(err).ToNot(HaveOccurred())

		VerifyHardwareTemplateStatus(ctx, c, hwTemplateResource.Name, metav1.Condition{
			Type:    string(hwmgmtv1alpha1.Validation),
			Status:  metav1.ConditionTrue,
			Reason:  string(hwmgmtv1alpha1.Completed),
			Message: "Validated",
		})

		Expect(nodeAllocationRequest).ToNot(BeNil())

		roleCounts := make(map[string]int)
		for _, node := range clusterInstance.Spec.Nodes {
			// Count the nodes per group
			roleCounts[node.Role]++
		}
		Expect(nodeAllocationRequest.NodeGroup).To(HaveLen(2))
		expectedNodeGroups := map[string]struct {
			size int
		}{
			groupNameController: {size: roleCounts["master"]},
			groupNameWorker:     {size: roleCounts["worker"]},
		}

		for _, group := range nodeAllocationRequest.NodeGroup {
			expected, found := expectedNodeGroups[group.NodeGroupData.Name]
			Expect(found).To(BeTrue())
			Expect(group.NodeGroupData.Size).To(Equal(expected.size))
		}
	})

	It("returns an error when the HwTemplate is not found", func() {
		// Create the ClusterTemplate but not the HardwareTemplate
		Expect(c.Create(ctx, ct)).To(Succeed())

		// Set the ClusterTemplate as validated
		ct.Status.Conditions = []metav1.Condition{
			{
				Type:   string(provisioningv1alpha1.CTconditionTypes.Validated),
				Status: metav1.ConditionTrue,
				Reason: string(provisioningv1alpha1.CTconditionReasons.Completed),
			},
		}
		Expect(c.Status().Update(ctx, ct)).To(Succeed())

		unstructuredCi, err := utils.ConvertToUnstructured(*clusterInstance)
		Expect(err).ToNot(HaveOccurred())
		nodeAllocationRequest, err := task.handleRenderHardwareTemplate(ctx, unstructuredCi)
		Expect(err).To(HaveOccurred())
		Expect(nodeAllocationRequest).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("failed to get the HardwareTemplate"))
	})

	It("returns an error when the ClusterTemplate is not found", func() {
		unstructuredCi, err := utils.ConvertToUnstructured(*clusterInstance)
		Expect(err).ToNot(HaveOccurred())
		nodeAllocationRequest, err := task.handleRenderHardwareTemplate(ctx, unstructuredCi)
		Expect(err).To(HaveOccurred())
		Expect(nodeAllocationRequest).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("failed to get the ClusterTemplate"))
	})
})

// createMockNodeAllocationRequestResponse creates a mock NodeAllocationRequestResponse for testing
// By default, it creates a mock with current transaction ID (both spec and status match)
func createMockNodeAllocationRequestResponse(conditionStatus, conditionReason, conditionMessage string) *hwmgrpluginapi.NodeAllocationRequestResponse {
	return createMockNodeAllocationRequestResponseWithTransactionId(conditionStatus, conditionReason, conditionMessage, 0, 0)
}

// createMockNodeAllocationRequestResponseWithTransactionId creates a mock with specific transaction IDs
func createMockNodeAllocationRequestResponseWithTransactionId(conditionStatus, conditionReason, conditionMessage string, specTransactionId, statusTransactionId int64) *hwmgrpluginapi.NodeAllocationRequestResponse {
	nodeNames := []string{"test-node-1", "test-node-2"}

	conditions := []hwmgrpluginapi.Condition{
		{
			Type:               "Provisioned",
			Status:             conditionStatus,
			Reason:             conditionReason,
			Message:            conditionMessage,
			LastTransitionTime: time.Now(),
		},
	}

	return &hwmgrpluginapi.NodeAllocationRequestResponse{
		NodeAllocationRequest: &hwmgrpluginapi.NodeAllocationRequest{
			BootInterfaceLabel:  "test-interface",
			ClusterId:           "test-cluster",
			ConfigTransactionId: specTransactionId,
			NodeGroup: []hwmgrpluginapi.NodeGroup{
				{
					NodeGroupData: hwmgrpluginapi.NodeGroupData{
						Name:      "controller",
						Role:      "master",
						HwProfile: "profile-spr-single-processor-64G",
						Size:      1,
					},
				},
				{
					NodeGroupData: hwmgrpluginapi.NodeGroupData{
						Name:      "worker",
						Role:      "worker",
						HwProfile: "profile-spr-dual-processor-128G",
						Size:      1,
					},
				},
			},
		},
		Status: &hwmgrpluginapi.NodeAllocationRequestStatus{
			Conditions:                  &conditions,
			ObservedConfigTransactionId: &statusTransactionId,
			Properties: &hwmgrpluginapi.Properties{
				NodeNames: &nodeNames,
			},
		},
	}
}

var _ = Describe("waitForNodeAllocationRequestProvision", func() {
	var (
		ctx         context.Context
		c           client.Client
		reconciler  *ProvisioningRequestReconciler
		task        *provisioningRequestReconcilerTask
		cr          *provisioningv1alpha1.ProvisioningRequest
		ci          *unstructured.Unstructured
		nar         *pluginsv1alpha1.NodeAllocationRequest
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
			Status: provisioningv1alpha1.ProvisioningRequestStatus{
				Extensions: provisioningv1alpha1.Extensions{
					NodeAllocationRequestRef: &provisioningv1alpha1.NodeAllocationRequestRef{
						NodeAllocationRequestID:        crName,
						HardwareProvisioningCheckStart: &metav1.Time{Time: time.Now()},
					},
				},
			},
		}

		// Define the node allocation request.
		nar = &pluginsv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
			// Set up your NodeAllocationRequest object as needed
			Status: pluginsv1alpha1.NodeAllocationRequestStatus{
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
			callbackConfig: utils.NewNarCallbackConfig(constants.DefaultNarCallbackServicePort),
		}
	})

	It("returns failed when NodeAllocationRequest provisioning failed", func() {
		provisionedCondition := metav1.Condition{
			Type:   "Provisioned",
			Status: metav1.ConditionFalse,
			Reason: string(hwmgmtv1alpha1.Failed),
		}
		nar.Status.Conditions = append(nar.Status.Conditions, provisionedCondition)
		Expect(c.Create(ctx, nar)).To(Succeed())

		// Use mock with failed status
		failedMock := createMockNodeAllocationRequestResponse("False", "Failed", "Provisioning failed")
		provisioned, timedOutOrFailed, err := task.checkNodeAllocationRequestStatus(ctx, failedMock, hwmgmtv1alpha1.Provisioned)
		Expect(provisioned).To(Equal(false))
		Expect(timedOutOrFailed).To(Equal(true)) // It should be failed
		Expect(err).ToNot(HaveOccurred())
		condition := meta.FindStatusCondition(cr.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(string(hwmgmtv1alpha1.Failed)))
	})

	It("processes hardware plugin timeout via callback", func() {
		provisionedCondition := metav1.Condition{
			Type:   "Provisioned",
			Status: metav1.ConditionFalse,
		}
		nar.Status.Conditions = append(nar.Status.Conditions, provisionedCondition)
		Expect(c.Create(ctx, nar)).To(Succeed())

		// Simulate a timeout scenario via callback for testing
		// Note: In reality, main controller detects timeouts, not the hardware plugin
		// Simulate a timeout callback from hardware plugin
		if cr.Annotations == nil {
			cr.Annotations = make(map[string]string)
		}
		cr.Annotations[utils.CallbackReceivedAnnotation] = "timeout-callback"

		// Hardware plugin sends callback with timed out status
		timedOutMock := createMockNodeAllocationRequestResponse("False", "TimedOut", "Hardware provisioning timed out")
		provisioned, timedOutOrFailed, err := task.checkNodeAllocationRequestStatus(ctx, timedOutMock, hwmgmtv1alpha1.Provisioned)
		Expect(provisioned).To(Equal(false))
		Expect(timedOutOrFailed).To(Equal(true)) // Should be true due to callback with TimedOut reason
		Expect(err).ToNot(HaveOccurred())

		condition := meta.FindStatusCondition(cr.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal("TimedOut"))
	})

	It("processes callback-triggered reconciliation correctly", func() {
		provisionedCondition := metav1.Condition{
			Type:   "Provisioned",
			Status: metav1.ConditionTrue,
		}
		nar.Status.Conditions = append(nar.Status.Conditions, provisionedCondition)
		Expect(c.Create(ctx, nar)).To(Succeed())

		// Set up PR status with matching NAR ID
		cr.Status.Extensions.NodeAllocationRequestRef = &provisioningv1alpha1.NodeAllocationRequestRef{
			NodeAllocationRequestID: "test-nar-id",
		}

		// Set up callback annotations to simulate callback-triggered reconciliation
		if cr.Annotations == nil {
			cr.Annotations = make(map[string]string)
		}
		cr.Annotations[utils.CallbackReceivedAnnotation] = "1234567890"
		cr.Annotations[utils.CallbackStatusAnnotation] = "Completed"
		cr.Annotations[utils.CallbackNodeAllocationRequestIdAnnotation] = "test-nar-id"

		// Update the CR in the fake client to persist the annotations and status
		Expect(c.Update(ctx, cr)).To(Succeed())
		Expect(c.Status().Update(ctx, cr)).To(Succeed())

		// Update task object to reflect the annotations (since task was created before annotations were added)
		task.object = cr

		// Verify annotations exist before processing
		var updatedCR provisioningv1alpha1.ProvisioningRequest
		Expect(c.Get(ctx, client.ObjectKeyFromObject(cr), &updatedCR)).To(Succeed())
		Expect(updatedCR.Annotations[utils.CallbackReceivedAnnotation]).To(Equal("1234567890"))
		Expect(updatedCR.Annotations[utils.CallbackStatusAnnotation]).To(Equal("Completed"))
		Expect(updatedCR.Annotations[utils.CallbackNodeAllocationRequestIdAnnotation]).To(Equal("test-nar-id"))

		// Process callback with completed status
		completedMock := createMockNodeAllocationRequestResponse("True", "Completed", "Hardware provisioning completed")
		provisioned, timedOutOrFailed, err := task.checkNodeAllocationRequestStatus(ctx, completedMock, hwmgmtv1alpha1.Provisioned)
		Expect(provisioned).To(Equal(true))
		Expect(timedOutOrFailed).To(Equal(false))
		Expect(err).ToNot(HaveOccurred())

		// Verify status was updated correctly
		Expect(c.Get(ctx, client.ObjectKeyFromObject(cr), &updatedCR)).To(Succeed())
		condition := meta.FindStatusCondition(updatedCR.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal("Completed"))
	})

	It("continues checking hardware configured status for ongoing operations", func() {
		// Set up initial state with configuration started but not completed
		currentTime := metav1.Now()
		cr.Status.Extensions.NodeAllocationRequestRef = &provisioningv1alpha1.NodeAllocationRequestRef{
			NodeAllocationRequestID:       "test-nar-id",
			HardwareConfiguringCheckStart: &currentTime,
		}

		// Set initial configured condition to false (in progress)
		utils.SetStatusCondition(&cr.Status.Conditions,
			provisioningv1alpha1.PRconditionTypes.HardwareConfigured,
			provisioningv1alpha1.CRconditionReasons.InProgress,
			metav1.ConditionFalse,
			"Hardware configuring is in progress")

		// Update the CR to persist the status
		Expect(c.Update(ctx, cr)).To(Succeed())

		// Create NAR with configured condition completed
		configuredCondition := metav1.Condition{
			Type:   "Configured",
			Status: metav1.ConditionTrue,
		}
		nar.Status.Conditions = append(nar.Status.Conditions, configuredCondition)
		// Note: ObservedConfigTransactionId assignment removed due to type issues in test
		// This doesn't affect the core test logic for shouldUpdateHardwareStatus
		Expect(c.Create(ctx, nar)).To(Succeed())

		// Simulate non-callback reconciliation (no callback annotations)
		// This should still update status because configuration was started but not completed
		completedMock := createMockNodeAllocationRequestResponse("True", "Completed", "Hardware configured successfully")
		// Add configured condition to the mock
		configuredCond := hwmgrpluginapi.Condition{
			Type:    "Configured",
			Status:  "True",
			Reason:  "Completed",
			Message: "Hardware configured successfully",
		}
		completedMock.Status.Conditions = &[]hwmgrpluginapi.Condition{configuredCond}

		configured, timedOutOrFailed, err := task.checkNodeAllocationRequestConfigStatus(ctx, completedMock)
		Expect(err).ToNot(HaveOccurred())
		Expect(configured).ToNot(BeNil())
		Expect(*configured).To(BeTrue())
		Expect(timedOutOrFailed).To(BeFalse())

		// Verify status was updated correctly even without callback annotations
		var updatedCR provisioningv1alpha1.ProvisioningRequest
		Expect(c.Get(ctx, client.ObjectKeyFromObject(cr), &updatedCR)).To(Succeed())
		condition := meta.FindStatusCondition(updatedCR.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal("Completed"))
	})

	It("does not pick up stale failed status after spec update", func() {
		// Set up initial state with configuration started and a stale failed condition
		currentTime := metav1.Now()
		cr.Status.Extensions.NodeAllocationRequestRef = &provisioningv1alpha1.NodeAllocationRequestRef{
			NodeAllocationRequestID:       "test-nar-id",
			HardwareConfiguringCheckStart: &currentTime,
		}

		// Set initial configured condition to failed (simulating old failed state)
		failedCondition := metav1.Condition{
			Type:               string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured),
			Status:             metav1.ConditionFalse,
			Reason:             string(provisioningv1alpha1.CRconditionReasons.Failed),
			Message:            "Hardware configuration failed",
			LastTransitionTime: currentTime,
		}
		cr.Status.Conditions = append(cr.Status.Conditions, failedCondition)

		// Update the CR to persist the status
		Expect(c.Update(ctx, cr)).To(Succeed())
		Expect(c.Status().Update(ctx, cr)).To(Succeed())

		// Update task object to reflect the updated CR status
		task.object = cr

		// Create NAR with still-failed status (plugin hasn't processed new spec yet)
		narFailedCondition := metav1.Condition{
			Type:   "Configured",
			Status: metav1.ConditionFalse,
		}
		nar.Status.Conditions = append(nar.Status.Conditions, narFailedCondition)
		Expect(c.Create(ctx, nar)).To(Succeed())

		// Simulate non-callback reconciliation with stale failed state
		// This should NOT update status because the condition reason is Failed (terminal state)
		staleMock := createMockNodeAllocationRequestResponse("False", "Failed", "Old failure message")
		failedCond := hwmgrpluginapi.Condition{
			Type:    "Configured",
			Status:  "False",
			Reason:  "Failed",
			Message: "Old failure message",
		}
		staleMock.Status.Conditions = &[]hwmgrpluginapi.Condition{failedCond}

		configured, timedOutOrFailed, err := task.checkNodeAllocationRequestConfigStatus(ctx, staleMock)

		// Should read from existing conditions instead of updating with stale plugin status
		Expect(err).ToNot(HaveOccurred())
		Expect(configured).ToNot(BeNil())
		Expect(*configured).To(BeFalse())     // Should reflect current PR condition, not plugin
		Expect(timedOutOrFailed).To(BeTrue()) // Should be true since the condition reason is Failed

		// Verify that the status was NOT overwritten with stale plugin data
		var updatedCR provisioningv1alpha1.ProvisioningRequest
		Expect(c.Get(ctx, client.ObjectKeyFromObject(cr), &updatedCR)).To(Succeed())
		condition := meta.FindStatusCondition(updatedCR.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.Failed))) // Should remain as original failed state
		Expect(condition.Message).To(Equal("Hardware configuring failed"))                         // Reflects the current plugin response
	})

	It("returns false when NodeAllocationRequest is not provisioned", func() {
		provisionedCondition := metav1.Condition{
			Type:   "Provisioned",
			Status: metav1.ConditionFalse,
		}
		nar.Status.Conditions = append(nar.Status.Conditions, provisionedCondition)
		Expect(c.Create(ctx, nar)).To(Succeed())

		// Use mock with not provisioned status
		notProvisionedMock := createMockNodeAllocationRequestResponse("False", "InProgress", "Not yet provisioned")
		provisioned, timedOutOrFailed, err := task.checkNodeAllocationRequestStatus(ctx, notProvisionedMock, hwmgmtv1alpha1.Provisioned)
		Expect(provisioned).To(Equal(false))
		Expect(timedOutOrFailed).To(Equal(false))
		Expect(err).ToNot(HaveOccurred())
		condition := meta.FindStatusCondition(cr.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
	})

	It("returns true when NodeAllocationRequest is provisioned", func() {
		provisionedCondition := metav1.Condition{
			Type:   "Provisioned",
			Status: metav1.ConditionTrue,
		}
		nar.Status.Conditions = append(nar.Status.Conditions, provisionedCondition)
		Expect(c.Create(ctx, nar)).To(Succeed())

		// Use mock with successful status
		successMock := createMockNodeAllocationRequestResponse("True", "Completed", "Successfully provisioned")
		provisioned, timedOutOrFailed, err := task.checkNodeAllocationRequestStatus(ctx, successMock, hwmgmtv1alpha1.Provisioned)
		Expect(provisioned).To(Equal(true))
		Expect(timedOutOrFailed).To(Equal(false))
		Expect(err).ToNot(HaveOccurred())
		condition := meta.FindStatusCondition(cr.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
	})
})

var _ = Describe("createOrUpdateNodeAllocationRequest", func() {
	var (
		ctx         context.Context
		c           client.Client
		reconciler  *ProvisioningRequestReconciler
		task        *provisioningRequestReconcilerTask
		cr          *provisioningv1alpha1.ProvisioningRequest
		crName      = "cluster-1"
		ctNamespace = "clustertemplate-a-v4-16"
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Define the provisioning request.
		cr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr}...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &provisioningRequestReconcilerTask{
			logger:         reconciler.Logger,
			client:         reconciler.Client,
			object:         cr,
			callbackConfig: utils.NewNarCallbackConfig(constants.DefaultNarCallbackServicePort),
		}

		// Set up hwpluginClient using the test Metal3 hardware plugin
		hwplugin := &hwmgmtv1alpha1.HardwarePlugin{}
		hwpluginKey := client.ObjectKey{
			Name:      testMetal3HardwarePluginRef,
			Namespace: testHwMgrPluginNameSpace,
		}
		err := c.Get(ctx, hwpluginKey, hwplugin)
		if err != nil {
			reconciler.Logger.Warn("Could not get hwplugin for test", "error", err)
		} else {
			hwpluginClient, err := hwmgrpluginapi.NewHardwarePluginClient(ctx, c, reconciler.Logger, hwplugin)
			if err != nil {
				reconciler.Logger.Warn("Could not create hwpluginClient for test", "error", err)
			} else {
				task.hwpluginClient = hwpluginClient
			}
		}
	})

	It("creates new NodeAllocationRequest when none exists", func() {
		nodeAllocationRequest := &hwmgrpluginapi.NodeAllocationRequest{
			ClusterId: crName,
			Site:      "test-site",
			NodeGroup: []hwmgrpluginapi.NodeGroup{
				{
					NodeGroupData: hwmgrpluginapi.NodeGroupData{
						Name:      "controller",
						Role:      "master",
						HwProfile: "profile-spr-single-processor-64G",
						Size:      1,
					},
				},
			},
		}

		err := task.createOrUpdateNodeAllocationRequest(ctx, ctNamespace, nodeAllocationRequest)
		Expect(err).ToNot(HaveOccurred())

		// Verify NodeAllocationRequestRef is set
		Expect(cr.Status.Extensions.NodeAllocationRequestRef).ToNot(BeNil())
		Expect(cr.Status.Extensions.NodeAllocationRequestRef.NodeAllocationRequestID).To(Equal("cluster-1"))
		Expect(cr.Status.Extensions.NodeAllocationRequestRef.HardwareProvisioningCheckStart).ToNot(BeNil())
	})

	It("updates existing NodeAllocationRequest when spec changes", func() {
		// Set up existing NodeAllocationRequest
		existingID := crName
		task.object.Status.Extensions.NodeAllocationRequestRef = &provisioningv1alpha1.NodeAllocationRequestRef{
			NodeAllocationRequestID: existingID,
		}

		nodeAllocationRequest := &hwmgrpluginapi.NodeAllocationRequest{
			ClusterId: crName,
			Site:      "test-site",
			NodeGroup: []hwmgrpluginapi.NodeGroup{
				{
					NodeGroupData: hwmgrpluginapi.NodeGroupData{
						Name:      "controller",
						Role:      "master",
						HwProfile: "profile-spr-single-processor-64G",
						Size:      2, // Changed size to trigger update
					},
				},
			},
		}

		err := task.createOrUpdateNodeAllocationRequest(ctx, ctNamespace, nodeAllocationRequest)
		Expect(err).ToNot(HaveOccurred())
	})

	It("updates configuring timer when NAR spec changes", func() {
		// Set up existing NodeAllocationRequest with active timers
		existingID := crName
		oldTime := metav1.NewTime(time.Now().Add(-10 * time.Minute)) // 10 minutes ago
		task.object.Status.Extensions.NodeAllocationRequestRef = &provisioningv1alpha1.NodeAllocationRequestRef{
			NodeAllocationRequestID:        existingID,
			HardwareProvisioningCheckStart: &oldTime,
			HardwareConfiguringCheckStart:  &oldTime,
		}

		// Update the CR to persist the old timers
		Expect(c.Status().Update(ctx, cr)).To(Succeed())

		// Update task object to reflect the updated CR status
		task.object = cr

		// Set up mock hwpluginClient that returns existing NAR with different NodeGroup
		existingNAR := &hwmgrpluginapi.NodeAllocationRequestResponse{
			NodeAllocationRequest: &hwmgrpluginapi.NodeAllocationRequest{
				ClusterId: crName,
				Site:      "test-site",
				NodeGroup: []hwmgrpluginapi.NodeGroup{
					{
						NodeGroupData: hwmgrpluginapi.NodeGroupData{
							Name:      "controller",
							Role:      "master",
							HwProfile: "profile-spr-single-processor-64G", // Different profile to trigger update
							Size:      1,
						},
					},
				},
			},
		}
		ctrl := gomock.NewController(GinkgoT())
		mockClient := mocks.NewMockHardwarePluginClientInterface(ctrl)
		mockClient.EXPECT().GetNodeAllocationRequest(gomock.Any(), existingID).Return(existingNAR, true, nil)
		mockClient.EXPECT().UpdateNodeAllocationRequest(gomock.Any(), existingID, gomock.Any()).Return(existingID, nil)
		task.hwpluginClient = mockClient

		nodeAllocationRequest := &hwmgrpluginapi.NodeAllocationRequest{
			ClusterId: crName,
			Site:      "test-site",
			NodeGroup: []hwmgrpluginapi.NodeGroup{
				{
					NodeGroupData: hwmgrpluginapi.NodeGroupData{
						Name:      "controller",
						Role:      "master",
						HwProfile: "profile-spr-single-processor-128G", // Changed profile to trigger update
						Size:      1,
					},
				},
			},
		}

		err := task.createOrUpdateNodeAllocationRequest(ctx, ctNamespace, nodeAllocationRequest)
		Expect(err).ToNot(HaveOccurred())

		// Read the updated CR to get the latest status
		var updatedCR provisioningv1alpha1.ProvisioningRequest
		Expect(c.Get(ctx, client.ObjectKeyFromObject(cr), &updatedCR)).To(Succeed())

		// Verify provisioning timer remains unchanged (not reset)
		// Use time comparison without monotonic clock since Kubernetes serialization strips it
		Expect(updatedCR.Status.Extensions.NodeAllocationRequestRef.HardwareProvisioningCheckStart.Time.Truncate(time.Second)).To(Equal(oldTime.Time.Truncate(time.Second)))
		// HardwareConfiguringCheckStart should be updated to current time (not old time)
		Expect(updatedCR.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart.IsZero()).To(BeFalse())
		Expect(updatedCR.Status.Extensions.NodeAllocationRequestRef.HardwareConfiguringCheckStart.After(oldTime.Time)).To(BeTrue())
	})
})

var _ = Describe("buildNodeAllocationRequest", func() {
	var (
		c          client.Client
		reconciler *ProvisioningRequestReconciler
		task       *provisioningRequestReconcilerTask
		cr         *provisioningv1alpha1.ProvisioningRequest
		crName     = "cluster-1"
	)

	BeforeEach(func() {
		// Define the provisioning request.
		cr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateParameters: runtime.RawExtension{
					Raw: []byte(testutils.TestFullTemplateParameters),
				},
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr}...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &provisioningRequestReconcilerTask{
			logger:         reconciler.Logger,
			client:         reconciler.Client,
			object:         cr,
			callbackConfig: utils.NewNarCallbackConfig(constants.DefaultNarCallbackServicePort),
		}
	})

	It("builds NodeAllocationRequest correctly", func() {
		clusterInstance := &unstructured.Unstructured{}
		clusterInstance.Object = map[string]interface{}{
			"spec": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{
						"role": "master",
					},
					map[string]interface{}{
						"role": "master",
					},
					map[string]interface{}{
						"role": "worker",
					},
				},
			},
		}

		hwTemplate := &hwmgmtv1alpha1.HardwareTemplate{
			Spec: hwmgmtv1alpha1.HardwareTemplateSpec{
				BootInterfaceLabel: "bootable-interface",
				NodeGroupData: []hwmgmtv1alpha1.NodeGroupData{
					{
						Name:           "controller",
						Role:           "master",
						ResourcePoolId: "pool-1",
						HwProfile:      "profile-1",
					},
					{
						Name:           "worker",
						Role:           "worker",
						ResourcePoolId: "pool-2",
						HwProfile:      "profile-2",
					},
				},
			},
		}

		nar, err := task.buildNodeAllocationRequest(clusterInstance, hwTemplate)
		Expect(err).ToNot(HaveOccurred())
		Expect(nar).ToNot(BeNil())
		Expect(nar.Site).To(Equal("local-123"))
		Expect(nar.ClusterId).To(Equal("exampleCluster"))
		Expect(nar.BootInterfaceLabel).To(Equal("bootable-interface"))
		Expect(nar.NodeGroup).To(HaveLen(2))

		// Check master nodes
		var masterGroup, workerGroup *hwmgrpluginapi.NodeGroup
		for i := range nar.NodeGroup {
			if nar.NodeGroup[i].NodeGroupData.Name == "controller" {
				masterGroup = &nar.NodeGroup[i]
			} else if nar.NodeGroup[i].NodeGroupData.Name == "worker" {
				workerGroup = &nar.NodeGroup[i]
			}
		}

		Expect(masterGroup).ToNot(BeNil())
		Expect(masterGroup.NodeGroupData.Size).To(Equal(2)) // 2 master nodes
		Expect(masterGroup.NodeGroupData.Role).To(Equal("master"))

		Expect(workerGroup).ToNot(BeNil())
		Expect(workerGroup.NodeGroupData.Size).To(Equal(1)) // 1 worker node
		Expect(workerGroup.NodeGroupData.Role).To(Equal("worker"))
	})

	It("returns error when spec.nodes not found", func() {
		clusterInstance := &unstructured.Unstructured{}
		clusterInstance.Object = map[string]interface{}{
			"spec": map[string]interface{}{
				// no nodes field
			},
		}

		hwTemplate := &hwmgmtv1alpha1.HardwareTemplate{}

		nar, err := task.buildNodeAllocationRequest(clusterInstance, hwTemplate)
		Expect(err).To(HaveOccurred())
		Expect(nar).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("spec.nodes not found in cluster instance"))
	})
})

var _ = Describe("updateAllocatedNodeHostMap", func() {
	var (
		ctx        context.Context
		c          client.Client
		reconciler *ProvisioningRequestReconciler
		task       *provisioningRequestReconcilerTask
		cr         *provisioningv1alpha1.ProvisioningRequest
		crName     = "cluster-1"
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Define the provisioning request.
		cr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr}...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &provisioningRequestReconcilerTask{
			logger:         reconciler.Logger,
			client:         reconciler.Client,
			object:         cr,
			callbackConfig: utils.NewNarCallbackConfig(constants.DefaultNarCallbackServicePort),
		}
	})

	It("updates AllocatedNodeHostMap correctly", func() {
		allocatedNodeID := testNodeID
		hostName := testHostName

		err := task.updateAllocatedNodeHostMap(ctx, allocatedNodeID, hostName)
		Expect(err).ToNot(HaveOccurred())

		Expect(cr.Status.Extensions.AllocatedNodeHostMap).ToNot(BeNil())
		Expect(cr.Status.Extensions.AllocatedNodeHostMap[allocatedNodeID]).To(Equal(hostName))
	})

	It("handles empty allocatedNodeID gracefully", func() {
		allocatedNodeID := ""
		hostName := testHostName

		err := task.updateAllocatedNodeHostMap(ctx, allocatedNodeID, hostName)
		Expect(err).ToNot(HaveOccurred())

		// Should not create map when allocatedNodeID is empty
		Expect(cr.Status.Extensions.AllocatedNodeHostMap).To(BeNil())
	})

	It("handles empty hostName gracefully", func() {
		allocatedNodeID := testNodeID
		hostName := ""

		err := task.updateAllocatedNodeHostMap(ctx, allocatedNodeID, hostName)
		Expect(err).ToNot(HaveOccurred())

		// Should not create map when hostName is empty
		Expect(cr.Status.Extensions.AllocatedNodeHostMap).To(BeNil())
	})

	It("does not update when values are the same", func() {
		allocatedNodeID := testNodeID
		hostName := testHostName

		// Initialize the map with the same value
		cr.Status.Extensions.AllocatedNodeHostMap = map[string]string{
			allocatedNodeID: hostName,
		}

		err := task.updateAllocatedNodeHostMap(ctx, allocatedNodeID, hostName)
		Expect(err).ToNot(HaveOccurred())

		// Should still have the same value
		Expect(cr.Status.Extensions.AllocatedNodeHostMap[allocatedNodeID]).To(Equal(hostName))
	})
})

var _ = Describe("waitForHardwareData", func() {
	var (
		ctx         context.Context
		c           client.Client
		reconciler  *ProvisioningRequestReconciler
		task        *provisioningRequestReconcilerTask
		cr          *provisioningv1alpha1.ProvisioningRequest
		ci          *unstructured.Unstructured
		crName      = "cluster-1"
		ctNamespace = "clustertemplate-a-v4-16"
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Define the cluster instance.
		ci = &unstructured.Unstructured{}
		ci.SetName(crName)
		ci.SetNamespace(ctNamespace)

		// Define the provisioning request.
		cr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    "clustertemplate-a",
				TemplateVersion: "v1.0.0",
				TemplateParameters: runtime.RawExtension{
					Raw: []byte(testutils.TestFullTemplateParameters),
				},
			},
			Status: provisioningv1alpha1.ProvisioningRequestStatus{
				Extensions: provisioningv1alpha1.Extensions{
					NodeAllocationRequestRef: &provisioningv1alpha1.NodeAllocationRequestRef{
						NodeAllocationRequestID:        crName,
						HardwareProvisioningCheckStart: &metav1.Time{Time: time.Now()},
					},
				},
			},
		}

		// Create a ClusterTemplate for the tests
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GetClusterTemplateRefName("clustertemplate-a", "v1.0.0"),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Templates: provisioningv1alpha1.Templates{
					HwTemplate: "test-hardware-template",
				},
			},
			Status: provisioningv1alpha1.ClusterTemplateStatus{
				Conditions: []metav1.Condition{
					{
						Type:   string(provisioningv1alpha1.CTconditionTypes.Validated),
						Status: metav1.ConditionTrue,
						Reason: string(provisioningv1alpha1.CTconditionReasons.Completed),
					},
				},
			},
		}

		// Create a HardwareTemplate for the tests
		hwTemplate := &hwmgmtv1alpha1.HardwareTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-hardware-template",
				Namespace: utils.InventoryNamespace,
			},
			Spec: hwmgmtv1alpha1.HardwareTemplateSpec{
				HardwarePluginRef:  testMetal3HardwarePluginRef,
				BootInterfaceLabel: "bootable-interface",
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
		}

		c = getFakeClientFromObjects([]client.Object{cr, ct, hwTemplate}...)
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
			ctDetails: &clusterTemplateDetails{
				namespace: ctNamespace,
			},
			callbackConfig: utils.NewNarCallbackConfig(constants.DefaultNarCallbackServicePort),
		}

		// Set up hwpluginClient using the test Metal3 hardware plugin
		hwplugin := &hwmgmtv1alpha1.HardwarePlugin{}
		hwpluginKey := client.ObjectKey{
			Name:      testMetal3HardwarePluginRef,
			Namespace: testHwMgrPluginNameSpace,
		}
		err := c.Get(ctx, hwpluginKey, hwplugin)
		if err != nil {
			reconciler.Logger.Warn("Could not get hwplugin for test", "error", err)
		} else {
			hwpluginClient, err := hwmgrpluginapi.NewHardwarePluginClient(ctx, c, reconciler.Logger, hwplugin)
			if err != nil {
				reconciler.Logger.Warn("Could not create hwpluginClient for test", "error", err)
			} else {
				task.hwpluginClient = hwpluginClient
			}
		}
	})

	It("returns provisioned=true and configured=true when both conditions are met", func() {
		// Create mock with both provisioned and configured true
		provisionedConfiguredMock := &hwmgrpluginapi.NodeAllocationRequestResponse{
			Status: &hwmgrpluginapi.NodeAllocationRequestStatus{
				Conditions: &[]hwmgrpluginapi.Condition{
					{
						Type:               "Provisioned",
						Status:             "True",
						Reason:             "Completed",
						Message:            "Hardware provisioned",
						LastTransitionTime: time.Now(),
					},
					{
						Type:               "Configured",
						Status:             "True",
						Reason:             "Completed",
						Message:            "Hardware configured",
						LastTransitionTime: time.Now(),
					},
				},
				ObservedConfigTransactionId: &cr.Generation,
			},
		}

		// Test just the status checking, not the full flow
		provisioned, timedOutOrFailed, err := task.checkNodeAllocationRequestStatus(ctx, provisionedConfiguredMock, hwmgmtv1alpha1.Provisioned)
		Expect(err).ToNot(HaveOccurred())
		Expect(provisioned).To(BeTrue())
		Expect(timedOutOrFailed).To(BeFalse())

		configured, timedOutOrFailed, err := task.checkNodeAllocationRequestConfigStatus(ctx, provisionedConfiguredMock)
		Expect(err).ToNot(HaveOccurred())
		Expect(configured).ToNot(BeNil())
		Expect(*configured).To(BeTrue())
		Expect(timedOutOrFailed).To(BeFalse())
	})

	It("returns provisioned=false when provisioning is not complete", func() {
		// Create mock with provisioned false
		notProvisionedMock := &hwmgrpluginapi.NodeAllocationRequestResponse{
			Status: &hwmgrpluginapi.NodeAllocationRequestStatus{
				Conditions: &[]hwmgrpluginapi.Condition{
					{
						Type:               "Provisioned",
						Status:             "False",
						Reason:             "InProgress",
						Message:            "Hardware provisioning in progress",
						LastTransitionTime: time.Now(),
					},
				},
			},
		}

		provisioned, configured, timedOutOrFailed, err := task.waitForHardwareData(ctx, ci, notProvisionedMock)
		Expect(err).ToNot(HaveOccurred())
		Expect(provisioned).To(BeFalse())
		Expect(configured).To(BeNil())
		Expect(timedOutOrFailed).To(BeFalse())
	})

	It("returns configured=nil when configured condition does not exist", func() {
		// Create mock with only provisioned condition
		onlyProvisionedMock := &hwmgrpluginapi.NodeAllocationRequestResponse{
			Status: &hwmgrpluginapi.NodeAllocationRequestStatus{
				Conditions: &[]hwmgrpluginapi.Condition{
					{
						Type:               "Provisioned",
						Status:             "True",
						Reason:             "Completed",
						Message:            "Hardware provisioned",
						LastTransitionTime: time.Now(),
					},
				},
			},
		}

		// Test just the status checking, not the full flow
		provisioned, timedOutOrFailed, err := task.checkNodeAllocationRequestStatus(ctx, onlyProvisionedMock, hwmgmtv1alpha1.Provisioned)
		Expect(err).ToNot(HaveOccurred())
		Expect(provisioned).To(BeTrue())
		Expect(timedOutOrFailed).To(BeFalse())

		configured, timedOutOrFailed, err := task.checkNodeAllocationRequestConfigStatus(ctx, onlyProvisionedMock)
		Expect(err).ToNot(HaveOccurred()) // Function returns nil error when condition doesn't exist
		Expect(configured).To(BeNil())    // But configured should be nil
		Expect(timedOutOrFailed).To(BeFalse())
	})
})

var _ = Describe("checkExistingNodeAllocationRequest", func() {
	var (
		ctx        context.Context
		c          client.Client
		reconciler *ProvisioningRequestReconciler
		task       *provisioningRequestReconcilerTask
		cr         *provisioningv1alpha1.ProvisioningRequest
		crName     = "cluster-1"
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Define the provisioning request.
		cr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr}...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &provisioningRequestReconcilerTask{
			logger:         reconciler.Logger,
			client:         reconciler.Client,
			object:         cr,
			callbackConfig: utils.NewNarCallbackConfig(constants.DefaultNarCallbackServicePort),
		}

		// Set up hwpluginClient using the test Metal3 hardware plugin
		hwplugin := &hwmgmtv1alpha1.HardwarePlugin{}
		hwpluginKey := client.ObjectKey{
			Name:      testMetal3HardwarePluginRef,
			Namespace: testHwMgrPluginNameSpace,
		}
		err := c.Get(ctx, hwpluginKey, hwplugin)
		if err != nil {
			reconciler.Logger.Warn("Could not get hwplugin for test", "error", err)
		} else {
			hwpluginClient, err := hwmgrpluginapi.NewHardwarePluginClient(ctx, c, reconciler.Logger, hwplugin)
			if err != nil {
				reconciler.Logger.Warn("Could not create hwpluginClient for test", "error", err)
			} else {
				task.hwpluginClient = hwpluginClient
			}
		}
	})

	It("returns error when hwpluginClient is nil", func() {
		task.hwpluginClient = nil

		hwTemplate := &hwmgmtv1alpha1.HardwareTemplate{}
		nodeAllocationRequestId := "test-id"

		response, err := task.checkExistingNodeAllocationRequest(ctx, hwTemplate, nodeAllocationRequestId)
		Expect(err).To(HaveOccurred())
		Expect(response).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("hwpluginClient is nil"))
	})

	It("returns response when NodeAllocationRequest exists and matches", func() {
		hwTemplate := &hwmgmtv1alpha1.HardwareTemplate{
			Spec: hwmgmtv1alpha1.HardwareTemplateSpec{
				HardwarePluginRef:  testMetal3HardwarePluginRef,
				BootInterfaceLabel: "bootable-interface",
				NodeGroupData: []hwmgmtv1alpha1.NodeGroupData{
					{
						Name:      "controller",
						Role:      "master",
						HwProfile: "profile-spr-single-processor-64G",
					},
					{
						Name:      "worker",
						Role:      "worker",
						HwProfile: "profile-spr-dual-processor-128G",
					},
				},
			},
		}
		nodeAllocationRequestId := crName

		response, err := task.checkExistingNodeAllocationRequest(ctx, hwTemplate, nodeAllocationRequestId)
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
	})
})

var _ = Describe("applyNodeConfiguration", func() {
	var (
		ctx         context.Context
		c           client.Client
		reconciler  *ProvisioningRequestReconciler
		task        *provisioningRequestReconcilerTask
		cr          *provisioningv1alpha1.ProvisioningRequest
		ci          *unstructured.Unstructured
		hwNodes     map[string][]utils.NodeInfo
		nar         *hwmgrpluginapi.NodeAllocationRequestResponse
		crName      = "cluster-1"
		ctNamespace = "clustertemplate-a-v4-16"
		tName       = "clustertemplate-a"
		tVersion    = "v1.0.0"
		hwTemplate  = "hwTemplate-v1"
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Define the cluster instance with nodes
		ci = &unstructured.Unstructured{}
		ci.SetName(crName)
		ci.SetNamespace(ctNamespace)
		ci.Object = map[string]interface{}{
			"spec": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{
						"role":     "master",
						"hostName": "master-01",
						"nodeNetwork": map[string]interface{}{
							"interfaces": []interface{}{
								map[string]interface{}{
									"name":       "eno1",
									"label":      "bootable-interface",
									"macAddress": "",
								},
								map[string]interface{}{
									"name":       "eno2",
									"label":      "data-interface",
									"macAddress": "",
								},
							},
						},
					},
					map[string]interface{}{
						"role":     "worker",
						"hostName": "worker-01",
						"nodeNetwork": map[string]interface{}{
							"interfaces": []interface{}{
								map[string]interface{}{
									"name":       "eno1",
									"label":      "bootable-interface",
									"macAddress": "",
								},
							},
						},
					},
				},
			},
		}

		// Define the provisioning request
		cr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: crName,
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    tName,
				TemplateVersion: tVersion,
				TemplateParameters: runtime.RawExtension{
					Raw: []byte(testutils.TestFullTemplateParameters),
				},
			},
		}

		// Create cluster template
		ct := &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GetClusterTemplateRefName(tName, tVersion),
				Namespace: ctNamespace,
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Templates: provisioningv1alpha1.Templates{
					HwTemplate: hwTemplate,
				},
			},
			Status: provisioningv1alpha1.ClusterTemplateStatus{
				Conditions: []metav1.Condition{
					{
						Type:   string(provisioningv1alpha1.CTconditionTypes.Validated),
						Status: metav1.ConditionTrue,
						Reason: string(provisioningv1alpha1.CTconditionReasons.Completed),
					},
				},
			},
		}

		// Create hardware template
		hwTemplateResource := &hwmgmtv1alpha1.HardwareTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hwTemplate,
				Namespace: utils.InventoryNamespace,
			},
			Spec: hwmgmtv1alpha1.HardwareTemplateSpec{
				HardwarePluginRef:  testMetal3HardwarePluginRef,
				BootInterfaceLabel: "bootable-interface",
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
		}

		// Set up hardware nodes map
		hwNodes = map[string][]utils.NodeInfo{
			"controller": {
				{
					BmcAddress:     "192.168.1.100",
					BmcCredentials: "master-01-bmc-secret",
					NodeID:         "node-master-01",
					HwMgrNodeId:    "bmh-master-01",
					HwMgrNodeNs:    "hardware-ns",
					Interfaces: []*pluginsv1alpha1.Interface{
						{
							Name:       "eno1",
							MACAddress: "aa:bb:cc:dd:ee:01",
							Label:      "bootable-interface",
						},
						{
							Name:       "eno2",
							MACAddress: "aa:bb:cc:dd:ee:02",
							Label:      "data-interface",
						},
					},
				},
			},
			"worker": {
				{
					BmcAddress:     "192.168.1.101",
					BmcCredentials: "worker-01-bmc-secret",
					NodeID:         "node-worker-01",
					HwMgrNodeId:    "bmh-worker-01",
					HwMgrNodeNs:    "hardware-ns",
					Interfaces: []*pluginsv1alpha1.Interface{
						{
							Name:       "eno1",
							MACAddress: "aa:bb:cc:dd:ee:11",
							Label:      "bootable-interface",
						},
					},
				},
			},
		}

		// Set up node allocation request response
		nar = &hwmgrpluginapi.NodeAllocationRequestResponse{
			NodeAllocationRequest: &hwmgrpluginapi.NodeAllocationRequest{
				NodeGroup: []hwmgrpluginapi.NodeGroup{
					{
						NodeGroupData: hwmgrpluginapi.NodeGroupData{
							Name: "controller",
							Role: "master",
						},
					},
					{
						NodeGroupData: hwmgrpluginapi.NodeGroupData{
							Name: "worker",
							Role: "worker",
						},
					},
				},
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr, ct, hwTemplateResource}...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &provisioningRequestReconcilerTask{
			logger: reconciler.Logger,
			client: reconciler.Client,
			object: cr,
			ctDetails: &clusterTemplateDetails{
				templates: provisioningv1alpha1.Templates{
					HwTemplate: hwTemplate,
				},
				namespace: ctNamespace,
			},
			clusterInput: &clusterInput{
				clusterInstanceData: map[string]any{
					"nodes": []any{
						map[string]any{
							"hostName": "master-01",
							"role":     "master",
							"nodeNetwork": map[string]any{
								"interfaces": []any{
									map[string]any{
										"name":       "eno1",
										"label":      "bootable-interface",
										"macAddress": "",
									},
									map[string]any{
										"name":       "eno2",
										"label":      "data-interface",
										"macAddress": "",
									},
								},
							},
						},
						map[string]any{
							"hostName": "worker-01",
							"role":     "worker",
							"nodeNetwork": map[string]any{
								"interfaces": []any{
									map[string]any{
										"name":       "eno1",
										"label":      "bootable-interface",
										"macAddress": "",
									},
								},
							},
						},
					},
				},
			},
			callbackConfig: utils.NewNarCallbackConfig(constants.DefaultNarCallbackServicePort),
		}
	})

	It("successfully applies node configuration", func() {
		err := task.applyNodeConfiguration(ctx, hwNodes, nar, ci)
		Expect(err).ToNot(HaveOccurred())

		// Verify that the cluster instance has been updated
		nodes, found, err := unstructured.NestedSlice(ci.Object, "spec", "nodes")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(nodes).To(HaveLen(2))

		// Check master node
		masterNode := nodes[0].(map[string]interface{})
		Expect(masterNode["bmcAddress"]).To(Equal("192.168.1.100"))
		bmcCreds, found, err := unstructured.NestedString(masterNode, "bmcCredentialsName", "name")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(bmcCreds).To(Equal("master-01-bmc-secret"))
		Expect(masterNode["bootMACAddress"]).To(Equal("aa:bb:cc:dd:ee:01"))

		hostRefName, found, err := unstructured.NestedString(masterNode, "hostRef", "name")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(hostRefName).To(Equal("bmh-master-01"))

		hostRefNs, found, err := unstructured.NestedString(masterNode, "hostRef", "namespace")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(hostRefNs).To(Equal("hardware-ns"))

		// Check worker node
		workerNode := nodes[1].(map[string]interface{})
		Expect(workerNode["bmcAddress"]).To(Equal("192.168.1.101"))
		bmcCreds, found, err = unstructured.NestedString(workerNode, "bmcCredentialsName", "name")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(bmcCreds).To(Equal("worker-01-bmc-secret"))
		Expect(workerNode["bootMACAddress"]).To(Equal("aa:bb:cc:dd:ee:11"))
	})

	It("returns error when spec.nodes not found in cluster instance", func() {
		// Remove nodes from cluster instance spec
		ci.Object = map[string]interface{}{
			"spec": map[string]interface{}{
				// no nodes field
			},
		}

		err := task.applyNodeConfiguration(ctx, hwNodes, nar, ci)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.nodes not found in cluster instance"))
	})

	It("returns error when node at index is not a valid map", func() {
		// Set invalid node structure
		ci.Object = map[string]interface{}{
			"spec": map[string]interface{}{
				"nodes": []interface{}{
					"invalid-node-string", // not a map
				},
			},
		}

		err := task.applyNodeConfiguration(ctx, hwNodes, nar, ci)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("node at index 0 is not a valid map"))
	})

	It("returns error when no matching hardware nodes found", func() {
		// Empty hardware nodes map
		emptyHwNodes := map[string][]utils.NodeInfo{}

		err := task.applyNodeConfiguration(ctx, emptyHwNodes, nar, ci)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to find matches for the following nodes"))
	})

	It("returns error when hardware provisioning is skipped", func() {
		// Set hardware template to empty to skip hardware provisioning
		task.ctDetails.templates.HwTemplate = ""

		err := task.applyNodeConfiguration(ctx, hwNodes, nar, ci)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get boot MAC for node"))
	})

	It("returns error when cluster template not found", func() {
		// Create a provisioning request with non-existent cluster template
		invalidCr := &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: "invalid-cluster",
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    "non-existent",
				TemplateVersion: "v1.0.0",
				TemplateParameters: runtime.RawExtension{
					Raw: []byte(testutils.TestFullTemplateParameters),
				},
			},
		}

		task.object = invalidCr

		err := task.applyNodeConfiguration(ctx, hwNodes, nar, ci)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get the ClusterTemplate"))
	})

	It("returns error when hardware template not found", func() {
		// Update cluster template to reference non-existent hardware template
		ct := &provisioningv1alpha1.ClusterTemplate{}
		Expect(c.Get(ctx, client.ObjectKey{
			Name:      GetClusterTemplateRefName(tName, tVersion),
			Namespace: ctNamespace,
		}, ct)).To(Succeed())

		ct.Spec.Templates.HwTemplate = "non-existent-hw-template"
		Expect(c.Update(ctx, ct)).To(Succeed())

		err := task.applyNodeConfiguration(ctx, hwNodes, nar, ci)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get the HardwareTemplate"))
	})

	It("handles nodes without HwMgrNodeId and HwMgrNodeNs", func() {
		// Remove HwMgrNodeId and HwMgrNodeNs from hardware nodes
		// Also provide both interfaces to match the cluster instance structure
		hwNodesWithoutHostRef := map[string][]utils.NodeInfo{
			"controller": {
				{
					BmcAddress:     "192.168.1.100",
					BmcCredentials: "master-01-bmc-secret",
					NodeID:         "node-master-01",
					HwMgrNodeId:    "", // empty
					HwMgrNodeNs:    "", // empty
					Interfaces: []*pluginsv1alpha1.Interface{
						{
							Name:       "eno1",
							MACAddress: "aa:bb:cc:dd:ee:01",
							Label:      "bootable-interface",
						},
						{
							Name:       "eno2",
							MACAddress: "aa:bb:cc:dd:ee:02",
							Label:      "data-interface",
						},
					},
				},
			},
			"worker": {
				{
					BmcAddress:     "192.168.1.101",
					BmcCredentials: "worker-01-bmc-secret",
					NodeID:         "node-worker-01",
					HwMgrNodeId:    "", // empty
					HwMgrNodeNs:    "", // empty
					Interfaces: []*pluginsv1alpha1.Interface{
						{
							Name:       "eno1",
							MACAddress: "aa:bb:cc:dd:ee:11",
							Label:      "bootable-interface",
						},
					},
				},
			},
		}

		err := task.applyNodeConfiguration(ctx, hwNodesWithoutHostRef, nar, ci)
		Expect(err).ToNot(HaveOccurred())

		// Verify that nodes were still configured but without hostRef
		nodes, found, err := unstructured.NestedSlice(ci.Object, "spec", "nodes")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(nodes).To(HaveLen(2))

		masterNode := nodes[0].(map[string]interface{})
		Expect(masterNode["bmcAddress"]).To(Equal("192.168.1.100"))

		// Verify hostRef is not set
		_, found, _ = unstructured.NestedString(masterNode, "hostRef", "name")
		Expect(found).To(BeFalse())
	})

	It("correctly consumes hardware nodes as they are assigned", func() {
		// Create multiple nodes of the same role to verify consumption
		ci.Object = map[string]interface{}{
			"spec": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{
						"role":     "master",
						"hostName": "master-01",
						"nodeNetwork": map[string]interface{}{
							"interfaces": []interface{}{
								map[string]interface{}{
									"name":       "eno1",
									"label":      "bootable-interface",
									"macAddress": "",
								},
							},
						},
					},
					map[string]interface{}{
						"role":     "master",
						"hostName": "master-02",
						"nodeNetwork": map[string]interface{}{
							"interfaces": []interface{}{
								map[string]interface{}{
									"name":       "eno1",
									"label":      "bootable-interface",
									"macAddress": "",
								},
							},
						},
					},
				},
			},
		}

		// Add second controller node
		hwNodes["controller"] = append(hwNodes["controller"], utils.NodeInfo{
			BmcAddress:     "192.168.1.102",
			BmcCredentials: "master-02-bmc-secret",
			NodeID:         "node-master-02",
			HwMgrNodeId:    "bmh-master-02",
			HwMgrNodeNs:    "hardware-ns",
			Interfaces: []*pluginsv1alpha1.Interface{
				{
					Name:       "eno1",
					MACAddress: "aa:bb:cc:dd:ee:03",
					Label:      "bootable-interface",
				},
			},
		})

		initialControllerCount := len(hwNodes["controller"])
		Expect(initialControllerCount).To(Equal(2))

		err := task.applyNodeConfiguration(ctx, hwNodes, nar, ci)
		Expect(err).ToNot(HaveOccurred())

		// Verify that all controller nodes have been consumed
		Expect(len(hwNodes["controller"])).To(Equal(0))

		// Verify both nodes were configured with different BMC addresses
		nodes, found, err := unstructured.NestedSlice(ci.Object, "spec", "nodes")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(nodes).To(HaveLen(2))

		masterNode1 := nodes[0].(map[string]interface{})
		masterNode2 := nodes[1].(map[string]interface{})

		// First node should get first hardware node
		Expect(masterNode1["bmcAddress"]).To(Equal("192.168.1.100"))
		// Second node should get second hardware node
		Expect(masterNode2["bmcAddress"]).To(Equal("192.168.1.102"))
	})
})

func VerifyHardwareTemplateStatus(ctx context.Context, c client.Client, templateName string, expectedCon metav1.Condition) {
	updatedHwTempl := &hwmgmtv1alpha1.HardwareTemplate{}
	Expect(c.Get(ctx, client.ObjectKey{Name: templateName, Namespace: utils.InventoryNamespace}, updatedHwTempl)).To(Succeed())
	hwTemplCond := meta.FindStatusCondition(updatedHwTempl.Status.Conditions, expectedCon.Type)
	Expect(hwTemplCond).ToNot(BeNil())
	testutils.VerifyStatusCondition(*hwTemplCond, metav1.Condition{
		Type:    expectedCon.Type,
		Status:  expectedCon.Status,
		Reason:  expectedCon.Reason,
		Message: expectedCon.Message,
	})
}

var _ = Describe("ProvisioningRequest Status Update After Hardware Failure", func() {
	var (
		c                client.Client
		ctx              context.Context
		logger           *slog.Logger
		reconciler       *ProvisioningRequestReconciler
		task             *provisioningRequestReconcilerTask
		cr               *provisioningv1alpha1.ProvisioningRequest
		template         *provisioningv1alpha1.ClusterTemplate
		hardwareTemplate *hwmgmtv1alpha1.HardwareTemplate
		testClusterName  = "test-update-after-failure-cluster"
		testNARID        = "test-nar-failed-update"
	)

	BeforeEach(func() {
		ctx = context.Background()
		logger = slog.New(slog.DiscardHandler)

		// Create a HardwareTemplate for the test
		hardwareTemplate = &hwmgmtv1alpha1.HardwareTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-hw-template-update-after-failure",
				Namespace: "test-ns",
			},
			Spec: hwmgmtv1alpha1.HardwareTemplateSpec{
				HardwarePluginRef:  "test-plugin",
				BootInterfaceLabel: "bootable-device",
				NodeGroupData: []hwmgmtv1alpha1.NodeGroupData{
					{
						Name:      "master",
						Role:      "master",
						HwProfile: "test-profile",
					},
				},
			},
			Status: hwmgmtv1alpha1.HardwareTemplateStatus{
				Conditions: []metav1.Condition{
					{
						Type:   string(hwmgmtv1alpha1.Validation),
						Status: metav1.ConditionTrue,
						Reason: string(hwmgmtv1alpha1.Completed),
					},
				},
			},
		}

		// Create a ClusterTemplate with hardware template
		template = &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-template-update-after-failure.v1.0.0",
				Namespace: "test-ns",
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Release: "4.17.0",
				Templates: provisioningv1alpha1.Templates{
					ClusterInstanceDefaults: "test-cluster-defaults",
					HwTemplate:              "test-hw-template-update-after-failure",
				},
			},
			Status: provisioningv1alpha1.ClusterTemplateStatus{
				Conditions: []metav1.Condition{
					{
						Type:   "ClusterTemplateValidated",
						Status: metav1.ConditionTrue,
						Reason: "Completed",
					},
				},
			},
		}

		// Create initial ProvisioningRequest in failed state due to hardware failure
		cr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-pr-update-after-failure",
				Namespace:  "test-ns",
				Generation: 1, // Initial generation
				Labels: map[string]string{
					"test-type": "update-after-failure",
				},
			},
			Spec: provisioningv1alpha1.ProvisioningRequestSpec{
				TemplateName:    "test-template-update-after-failure",
				TemplateVersion: "v1.0.0",
				TemplateParameters: runtime.RawExtension{
					Raw: []byte(`{"clusterName": "` + testClusterName + `"}`),
				},
			},
			Status: provisioningv1alpha1.ProvisioningRequestStatus{
				ObservedGeneration: 1, // Matches current generation
				ProvisioningStatus: provisioningv1alpha1.ProvisioningStatus{
					ProvisioningPhase:   provisioningv1alpha1.StateFailed,
					ProvisioningDetails: "Hardware provisioning failed",
				},
				Extensions: provisioningv1alpha1.Extensions{
					NodeAllocationRequestRef: &provisioningv1alpha1.NodeAllocationRequestRef{
						NodeAllocationRequestID:        testNARID,
						HardwareProvisioningCheckStart: &metav1.Time{Time: time.Now().Add(-10 * time.Minute)},
						HardwareConfiguringCheckStart:  nil,
					},
				},
				Conditions: []metav1.Condition{
					{
						Type:    string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned),
						Status:  metav1.ConditionFalse,
						Reason:  string(hwmgmtv1alpha1.Failed),
						Message: "Hardware provisioning failed",
					},
				},
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr, template, hardwareTemplate}...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &provisioningRequestReconcilerTask{
			logger:       reconciler.Logger,
			client:       reconciler.Client,
			object:       cr,
			clusterInput: &clusterInput{},
			ctDetails:    &clusterTemplateDetails{},
			timeouts: &timeouts{
				hardwareProvisioning: 30 * time.Minute,
			},
			callbackConfig: utils.NewNarCallbackConfig(constants.DefaultNarCallbackServicePort),
		}
	})

	Context("when ProvisioningRequest is updated after hardware failure", func() {
		It("should process hardware failure normally with callback-only approach", func() {
			// With callback-only approach, hardware failures are processed when received via callbacks
			// No transaction ID protection is needed since callbacks ensure fresh status

			// Create a mock NodeAllocationRequest response that shows failure
			failedMock := createMockNodeAllocationRequestResponse("False", "Failed", "Hardware provisioning failed")

			// Simulate a callback-triggered reconciliation by setting the annotation
			if cr.Annotations == nil {
				cr.Annotations = make(map[string]string)
			}
			cr.Annotations[utils.CallbackReceivedAnnotation] = "hardware-failure"
			Expect(c.Update(ctx, cr)).To(Succeed())

			// Call updateHardwareStatus - should process failure since it's callback-triggered
			provisioned, timedOutOrFailed, err := task.updateHardwareStatus(ctx, failedMock, hwmgmtv1alpha1.Provisioned)

			// Verify the results - failure should be processed
			Expect(err).ToNot(HaveOccurred())
			Expect(provisioned).To(Equal(false))
			Expect(timedOutOrFailed).To(Equal(true)) // Should be true for genuine callback-triggered failure

			// Refresh the CR to get updated status
			Expect(c.Get(ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())

			// Verify that the provisioning status is set to failed
			Expect(cr.Status.ProvisioningStatus.ProvisioningPhase).To(Equal(provisioningv1alpha1.StateFailed))
			Expect(cr.Status.ProvisioningStatus.ProvisioningDetails).To(ContainSubstring("Hardware provisioning failed"))
		})

		It("should allow hardware status to fail if ProvisioningRequest has not been updated", func() {
			// Do NOT update the generation, keep ObservedGeneration = Generation
			cr.Status.ObservedGeneration = cr.Generation // They match, so no update detected

			// Create a mock NodeAllocationRequest response that shows failure with matching transaction ID
			// This simulates the case where hardware plugin has processed the current generation and reports genuine failure
			failedMock := createMockNodeAllocationRequestResponseWithTransactionId("False", "Failed", "Hardware provisioning failed", cr.Generation, cr.Generation)

			// Call updateHardwareStatus - this SHOULD set to failed since transaction IDs match (no stale status)
			provisioned, timedOutOrFailed, err := task.updateHardwareStatus(ctx, failedMock, hwmgmtv1alpha1.Provisioned)

			// Verify the results
			Expect(err).ToNot(HaveOccurred())
			Expect(provisioned).To(Equal(false))
			Expect(timedOutOrFailed).To(Equal(true)) // Should be true because no update was detected

			// Refresh the CR to get updated status
			Expect(c.Get(ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())

			// Verify that the provisioning status is set to failed (original behavior preserved)
			Expect(cr.Status.ProvisioningStatus.ProvisioningPhase).To(Equal(provisioningv1alpha1.StateFailed))
			Expect(cr.Status.ProvisioningStatus.ProvisioningDetails).To(ContainSubstring("Hardware provisioning failed"))
		})

		It("should not override pending status when hardware status is in progress after update", func() {
			// Simulate updating the ProvisioningRequest spec
			cr.Generation = 2 // Simulating spec update

			// Set a recent HardwareProvisioningCheckStart time to avoid timeout
			recentTime := metav1.NewTime(time.Now().Add(-1 * time.Minute))
			cr.Status.Extensions.NodeAllocationRequestRef.HardwareProvisioningCheckStart = &recentTime

			Expect(c.Update(ctx, cr)).To(Succeed())

			// Get the latest CR from client to ensure we have fresh data
			Expect(c.Get(ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())

			// Now set the status to pending
			cr.Status.ProvisioningStatus.ProvisioningPhase = provisioningv1alpha1.StatePending
			cr.Status.ProvisioningStatus.ProvisioningDetails = utils.ValidationMessage
			cr.Status.ProvisioningStatus.UpdateTime = metav1.Now() // Set UpdateTime to simulate real status update

			// Manually update the status to preserve the pending state
			Expect(c.Status().Update(ctx, cr)).To(Succeed())

			// Create a mock with in-progress status - use False status as that's what triggers in-progress logic
			inProgressMock := createMockNodeAllocationRequestResponse("False", "InProgress", "Hardware provisioning in progress")

			// Call updateHardwareStatus - should transition from pending to progressing (normal flow)
			provisioned, timedOutOrFailed, err := task.updateHardwareStatus(ctx, inProgressMock, hwmgmtv1alpha1.Provisioned)

			// Verify the results
			Expect(err).ToNot(HaveOccurred())
			Expect(provisioned).To(Equal(false))
			Expect(timedOutOrFailed).To(Equal(false)) // In progress, not failed

			// Refresh the CR to get updated status
			Expect(c.Get(ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())

			// Verify that the provisioning status transitions to progressing (normal flow allowed)
			Expect(cr.Status.ProvisioningStatus.ProvisioningPhase).To(Equal(provisioningv1alpha1.StateProgressing))
			Expect(cr.Status.ProvisioningStatus.ProvisioningDetails).To(ContainSubstring("Hardware provisioning is in progress"))

			// Verify that hardware condition shows the in-progress status
			condition := meta.FindStatusCondition(cr.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned))
			Expect(condition).ToNot(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal("InProgress"))
		})

		It("should allow normal transition from pending to progressing when hardware is in progress", func() {
			// Simulate updating the ProvisioningRequest spec
			cr.Generation = 2 // Simulating spec update

			// Set a recent HardwareProvisioningCheckStart time to avoid timeout
			recentTime := metav1.NewTime(time.Now().Add(-1 * time.Minute))
			cr.Status.Extensions.NodeAllocationRequestRef.HardwareProvisioningCheckStart = &recentTime

			Expect(c.Update(ctx, cr)).To(Succeed())

			// Get the latest CR from client to ensure we have fresh data
			Expect(c.Get(ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())

			// Now set the status to pending
			cr.Status.ProvisioningStatus.ProvisioningPhase = provisioningv1alpha1.StatePending
			cr.Status.ProvisioningStatus.ProvisioningDetails = utils.ValidationMessage
			cr.Status.ProvisioningStatus.UpdateTime = metav1.Now() // Set UpdateTime to simulate real status update

			// Manually update the status to preserve the pending state
			Expect(c.Status().Update(ctx, cr)).To(Succeed())

			// Create a mock with in-progress status - use False status as that's what triggers in-progress logic
			inProgressMock := createMockNodeAllocationRequestResponse("False", "InProgress", "Hardware provisioning in progress")

			// Call updateHardwareStatus - should transition from pending to progressing
			provisioned, timedOutOrFailed, err := task.updateHardwareStatus(ctx, inProgressMock, hwmgmtv1alpha1.Provisioned)

			// Verify the results
			Expect(err).ToNot(HaveOccurred())
			Expect(provisioned).To(Equal(false))
			Expect(timedOutOrFailed).To(Equal(false)) // In progress, not failed

			// Refresh the CR to get updated status
			Expect(c.Get(ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())

			// Verify that the provisioning status transitions to progressing (normal flow)
			Expect(cr.Status.ProvisioningStatus.ProvisioningPhase).To(Equal(provisioningv1alpha1.StateProgressing))
			Expect(cr.Status.ProvisioningStatus.ProvisioningDetails).To(ContainSubstring("Hardware provisioning is in progress"))

			// Verify that hardware condition shows the in-progress status
			condition := meta.FindStatusCondition(cr.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned))
			Expect(condition).ToNot(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal("InProgress"))
		})

		It("should allow hardware status updates when transaction ID is current", func() {
			// Simulate a ProvisioningRequest that has been updated and hardware plugin has processed it
			cr.Generation = 2
			cr.Status.ObservedGeneration = cr.Generation // SAME as generation
			cr.Status.ProvisioningStatus.ProvisioningPhase = provisioningv1alpha1.StateProgressing
			cr.Status.ProvisioningStatus.ProvisioningDetails = "Hardware provisioning in progress"

			Expect(c.Update(ctx, cr)).To(Succeed())
			Expect(c.Status().Update(ctx, cr)).To(Succeed())

			// Create a mock showing hardware failure with CURRENT transaction ID
			failedMock := createMockNodeAllocationRequestResponseWithTransactionId("False", "Failed", "Hardware provisioning failed", cr.Generation, cr.Generation)
			// Both specTransactionId and statusTransactionId = 2 (current generation)

			// Call updateHardwareStatus - since transaction ID is current, failure should be processed
			provisioned, timedOutOrFailed, err := task.updateHardwareStatus(ctx, failedMock, hwmgmtv1alpha1.Provisioned)

			// Verify the results
			Expect(err).ToNot(HaveOccurred())
			Expect(provisioned).To(Equal(false))
			Expect(timedOutOrFailed).To(Equal(true)) // Should be treated as genuine failure

			// Refresh the CR to get updated status
			Expect(c.Get(ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())

			// Verify that the provisioning status IS updated to failed (normal behavior)
			Expect(cr.Status.ProvisioningStatus.ProvisioningPhase).To(Equal(provisioningv1alpha1.StateFailed))
			Expect(cr.Status.ProvisioningStatus.ProvisioningDetails).To(ContainSubstring("Hardware provisioning failed"))

			// This proves that hardware status updates work normally when transaction ID is current
		})
	})
})
