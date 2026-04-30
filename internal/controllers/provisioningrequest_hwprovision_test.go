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
   - Tests error handling when hwMgmtData has no nodeGroupData
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
*/

package controllers

import (
	"context"
	"log/slog"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
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
		cr              *provisioningv1alpha1.ProvisioningRequest
		tName           = "clustertemplate-a"
		tVersion        = "v1.0.0"
		ctNamespace     = "clustertemplate-a-v4-16"
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

		c = getFakeClientFromObjects([]client.Object{cr}...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &provisioningRequestReconcilerTask{
			logger: reconciler.Logger,
			client: reconciler.Client,
			object: cr,
			ctDetails: &clusterTemplateDetails{
				namespace: ctNamespace,
				templates: provisioningv1alpha1.TemplateDefaults{
					HwMgmtDefaults: provisioningv1alpha1.HwMgmtDefaults{
						HardwarePluginRef: testMetal3HardwarePluginRef,
					},
				},
			},
		}

		// Set up hwpluginClient using the test Metal3 hardware plugin
	})

	It("returns no error when handleRenderHardwareTemplate succeeds", func() {
		// The default HardwarePlugin (metal3-hwplugin) is created in suite_test.go
		// Set up the merged hwMgmt data on the task (handleRenderHardwareTemplate reads from t.clusterInput.hwMgmtData)
		task.clusterInput = &clusterInput{
			hwMgmtData: map[string]any{
				"nodeGroupData": []any{
					map[string]any{"name": "controller", "role": "master", "hwProfile": "profile-spr-single-processor-64G", "resourcePoolId": "xyz"},
					map[string]any{"name": "worker", "role": "worker", "hwProfile": "profile-spr-dual-processor-128G", "resourcePoolId": "xyz"},
				},
			},
		}

		unstructuredCi, err := utils.ConvertToUnstructured(*clusterInstance)
		Expect(err).ToNot(HaveOccurred())
		nodeAllocationRequest, err := task.handleRenderHardwareTemplate(ctx, unstructuredCi)
		Expect(err).ToNot(HaveOccurred())

		Expect(nodeAllocationRequest).ToNot(BeNil())

		roleCounts := make(map[string]int)
		for _, node := range clusterInstance.Spec.Nodes {
			// Count the nodes per group
			roleCounts[node.Role]++
		}
		Expect(nodeAllocationRequest.Spec.NodeGroup).To(HaveLen(2))
		expectedNodeGroups := map[string]struct {
			size int
		}{
			groupNameController: {size: roleCounts["master"]},
			groupNameWorker:     {size: roleCounts["worker"]},
		}

		for _, group := range nodeAllocationRequest.Spec.NodeGroup {
			expected, found := expectedNodeGroups[group.NodeGroupData.Name]
			Expect(found).To(BeTrue())
			Expect(group.Size).To(Equal(expected.size))
		}
	})

	It("returns an error when hwMgmtData has no nodeGroupData", func() {
		// Set up clusterInput with hwMgmtData that has no nodeGroupData
		task.clusterInput = &clusterInput{
			hwMgmtData: map[string]any{
				"hardwarePluginRef": testMetal3HardwarePluginRef,
			},
		}

		unstructuredCi, err := utils.ConvertToUnstructured(*clusterInstance)
		Expect(err).ToNot(HaveOccurred())
		// Test buildNodeAllocationRequest directly since it validates nodeGroupData
		nodeAllocationRequest, err := task.buildNodeAllocationRequest(unstructuredCi)
		Expect(err).To(HaveOccurred())
		Expect(nodeAllocationRequest).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("nodeGroupData not found"))
	})

	It("returns an error when hwMgmtData is empty", func() {
		// hwMgmtData is empty — plugin exists but nodeGroupData is missing
		task.clusterInput = &clusterInput{
			hwMgmtData: map[string]any{},
		}

		unstructuredCi, err := utils.ConvertToUnstructured(*clusterInstance)
		Expect(err).ToNot(HaveOccurred())
		nodeAllocationRequest, err := task.handleRenderHardwareTemplate(ctx, unstructuredCi)
		Expect(err).To(HaveOccurred())
		Expect(nodeAllocationRequest).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("nodeGroupData not found"))
	})
})

// createMockNodeAllocationRequestResponse creates a mock NodeAllocationRequestResponse for testing
// By default, it creates a mock with current transaction ID (both spec and status match)
func createMockNodeAllocationRequestResponse(conditionStatus, conditionReason, conditionMessage string) *pluginsv1alpha1.NodeAllocationRequest {
	return createMockNodeAllocationRequestResponseWithTransactionId(conditionStatus, conditionReason, conditionMessage, 0, 0)
}

// createMockNodeAllocationRequestResponseWithTransactionId creates a mock NAR with specific transaction IDs
func createMockNodeAllocationRequestResponseWithTransactionId(conditionStatus, conditionReason, conditionMessage string, specTransactionId, statusTransactionId int64) *pluginsv1alpha1.NodeAllocationRequest {
	return &pluginsv1alpha1.NodeAllocationRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-nar",
			Namespace: constants.DefaultNamespace,
		},
		Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
			ClusterId:           "test-cluster",
			ConfigTransactionId: specTransactionId,
			NodeGroup: []pluginsv1alpha1.NodeGroup{
				{
					NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
						Name:      "controller",
						Role:      "master",
						HwProfile: "profile-spr-single-processor-64G",
					},
					Size: 1,
				},
				{
					NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
						Name:      "worker",
						Role:      "worker",
						HwProfile: "profile-spr-dual-processor-128G",
					},
					Size: 1,
				},
			},
		},
		Status: pluginsv1alpha1.NodeAllocationRequestStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Provisioned",
					Status:             metav1.ConditionStatus(conditionStatus),
					Reason:             conditionReason,
					Message:            conditionMessage,
					LastTransitionTime: metav1.Now(),
				},
			},
			ObservedConfigTransactionId: statusTransactionId,
			Properties: pluginsv1alpha1.Properties{
				NodeNames: []string{"test-node-1", "test-node-2"},
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
						NodeAllocationRequestID: crName,
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
			ctDetails: &clusterTemplateDetails{
				namespace: ctNamespace,
				templates: provisioningv1alpha1.TemplateDefaults{
					HwMgmtDefaults: provisioningv1alpha1.HwMgmtDefaults{
						HardwarePluginRef: testMetal3HardwarePluginRef,
					},
				},
			},
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

		// Update the CR in the fake client to persist the status
		Expect(c.Status().Update(ctx, cr)).To(Succeed())

		// Update task object to reflect the status (since task was created before status was added)
		task.object = cr

		// Process callback with completed status
		completedMock := createMockNodeAllocationRequestResponse("True", "Completed", "Hardware provisioning completed")
		provisioned, timedOutOrFailed, err := task.checkNodeAllocationRequestStatus(ctx, completedMock, hwmgmtv1alpha1.Provisioned)
		Expect(provisioned).To(Equal(true))
		Expect(timedOutOrFailed).To(Equal(false))
		Expect(err).ToNot(HaveOccurred())

		// Verify status was updated correctly
		var updatedCR provisioningv1alpha1.ProvisioningRequest
		Expect(c.Get(ctx, client.ObjectKeyFromObject(cr), &updatedCR)).To(Succeed())
		condition := meta.FindStatusCondition(updatedCR.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal("Completed"))
	})

	It("continues checking hardware configured status for ongoing operations", func() {
		// Set up initial state with configuration started but not completed
		cr.Status.Extensions.NodeAllocationRequestRef = &provisioningv1alpha1.NodeAllocationRequestRef{
			NodeAllocationRequestID: "test-nar-id",
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
		completedMock.Status.Conditions = []metav1.Condition{
			{
				Type:               "Configured",
				Status:             metav1.ConditionTrue,
				Reason:             "Completed",
				Message:            "Hardware configured successfully",
				LastTransitionTime: metav1.Now(),
			},
		}

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
		cr.Status.Extensions.NodeAllocationRequestRef = &provisioningv1alpha1.NodeAllocationRequestRef{
			NodeAllocationRequestID: "test-nar-id",
		}

		// Set initial configured condition to failed (simulating old failed state)
		failedCondition := metav1.Condition{
			Type:               string(provisioningv1alpha1.PRconditionTypes.HardwareConfigured),
			Status:             metav1.ConditionFalse,
			Reason:             string(provisioningv1alpha1.CRconditionReasons.Failed),
			Message:            "Hardware configuration failed",
			LastTransitionTime: metav1.Now(),
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
		staleMock.Status.Conditions = []metav1.Condition{
			{
				Type:               "Configured",
				Status:             metav1.ConditionFalse,
				Reason:             "Failed",
				Message:            "Old failure message",
				LastTransitionTime: metav1.Now(),
			},
		}

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
		Expect(condition.Message).To(Equal("Hardware configuring failed: Old failure message"))    // Reflects the current plugin response with the detailed error
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
			logger: reconciler.Logger,
			client: reconciler.Client,
			object: cr,
			ctDetails: &clusterTemplateDetails{
				namespace: ctNamespace,
				templates: provisioningv1alpha1.TemplateDefaults{
					HwMgmtDefaults: provisioningv1alpha1.HwMgmtDefaults{
						HardwarePluginRef: testMetal3HardwarePluginRef,
					},
				},
			},
		}

		// Set up hwpluginClient using the test Metal3 hardware plugin
	})

	It("creates new NodeAllocationRequest when none exists", func() {
		nodeAllocationRequest := &pluginsv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cr.Name,
				Namespace: constants.DefaultNamespace,
			},
			Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
				ClusterId:    crName,
				LocationSpec: pluginsv1alpha1.LocationSpec{Site: "test-site"},
				NodeGroup: []pluginsv1alpha1.NodeGroup{
					{
						NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
							Name:      "controller",
							Role:      "master",
							HwProfile: "profile-spr-single-processor-64G",
						},
						Size: 1,
					},
				},
			},
		}

		err := task.createOrUpdateNodeAllocationRequest(ctx, ctNamespace, nodeAllocationRequest)
		Expect(err).ToNot(HaveOccurred())

		// Verify the NAR was created in the cluster
		createdNAR := &pluginsv1alpha1.NodeAllocationRequest{}
		err = c.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: constants.DefaultNamespace}, createdNAR)
		Expect(err).ToNot(HaveOccurred())
		Expect(createdNAR.Spec.ClusterId).To(Equal(crName))
	})

	It("updates existing NodeAllocationRequest when spec changes", func() {
		// Create existing NAR CR
		existingNAR := &pluginsv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cr.Name,
				Namespace: constants.DefaultNamespace,
			},
			Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
				ClusterId:    crName,
				LocationSpec: pluginsv1alpha1.LocationSpec{Site: "test-site"},
				NodeGroup: []pluginsv1alpha1.NodeGroup{
					{
						NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
							Name:      "controller",
							Role:      "master",
							HwProfile: "profile-spr-single-processor-64G",
						},
						Size: 1,
					},
				},
			},
		}
		Expect(c.Create(ctx, existingNAR)).To(Succeed())

		// New NAR with changed size
		nodeAllocationRequest := &pluginsv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cr.Name,
				Namespace: constants.DefaultNamespace,
			},
			Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
				ClusterId:    crName,
				LocationSpec: pluginsv1alpha1.LocationSpec{Site: "test-site"},
				NodeGroup: []pluginsv1alpha1.NodeGroup{
					{
						NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
							Name:      "controller",
							Role:      "master",
							HwProfile: "profile-spr-single-processor-64G",
						},
						Size: 2, // Changed size
					},
				},
			},
		}

		err := task.createOrUpdateNodeAllocationRequest(ctx, ctNamespace, nodeAllocationRequest)
		Expect(err).ToNot(HaveOccurred())
	})

	It("updates configuring timer when NAR spec changes", func() {
		// Create existing NAR CR in fake client with original profile
		existingNAR := &pluginsv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cr.Name,
				Namespace: constants.DefaultNamespace,
			},
			Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
				ClusterId:    crName,
				LocationSpec: pluginsv1alpha1.LocationSpec{Site: "test-site"},
				NodeGroup: []pluginsv1alpha1.NodeGroup{
					{
						NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
							Name:      "controller",
							Role:      "master",
							HwProfile: "profile-spr-single-processor-64G",
						},
						Size: 1,
					},
				},
			},
		}
		Expect(c.Create(ctx, existingNAR)).To(Succeed())

		// New NAR with changed profile to trigger update
		nodeAllocationRequest := &pluginsv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cr.Name,
				Namespace: constants.DefaultNamespace,
			},
			Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
				ClusterId:    crName,
				LocationSpec: pluginsv1alpha1.LocationSpec{Site: "test-site"},
				NodeGroup: []pluginsv1alpha1.NodeGroup{
					{
						NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
							Name:      "controller",
							Role:      "master",
							HwProfile: "profile-spr-single-processor-128G", // Changed profile
						},
						Size: 1,
					},
				},
			},
		}

		err := task.createOrUpdateNodeAllocationRequest(ctx, ctNamespace, nodeAllocationRequest)
		Expect(err).ToNot(HaveOccurred())

		// Verify the NAR spec was updated in the cluster
		updatedNAR := &pluginsv1alpha1.NodeAllocationRequest{}
		Expect(c.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: constants.DefaultNamespace}, updatedNAR)).To(Succeed())
		Expect(updatedNAR.Spec.NodeGroup[0].NodeGroupData.HwProfile).To(Equal("profile-spr-single-processor-128G"))
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
				Name:       crName,
				Generation: 1, // Set explicit generation for testing
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
			logger: reconciler.Logger,
			client: reconciler.Client,
			object: cr,
			ctDetails: &clusterTemplateDetails{
				namespace: "test-ns",
				templates: provisioningv1alpha1.TemplateDefaults{
					HwMgmtDefaults: provisioningv1alpha1.HwMgmtDefaults{
						HardwarePluginRef: testMetal3HardwarePluginRef,
					},
				},
			},
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

		task.clusterInput = &clusterInput{
			hwMgmtData: map[string]any{
				"nodeGroupData": []any{
					map[string]any{"name": "controller", "role": "master", "hwProfile": "profile-1", "resourcePoolId": "pool-1"},
					map[string]any{"name": "worker", "role": "worker", "hwProfile": "profile-2", "resourcePoolId": "pool-2"},
				},
			},
		}

		nar, err := task.buildNodeAllocationRequest(clusterInstance)
		Expect(err).ToNot(HaveOccurred())
		Expect(nar).ToNot(BeNil())
		Expect(nar.Spec.LocationSpec.Site).To(Equal("local-123"))
		Expect(nar.Spec.ClusterId).To(Equal("exampleCluster"))
		Expect(nar.Spec.NodeGroup).To(HaveLen(2))

		// Check master nodes
		var masterGroup, workerGroup *pluginsv1alpha1.NodeGroup
		for i := range nar.Spec.NodeGroup {
			if nar.Spec.NodeGroup[i].NodeGroupData.Name == "controller" {
				masterGroup = &nar.Spec.NodeGroup[i]
			} else if nar.Spec.NodeGroup[i].NodeGroupData.Name == groupNameWorker {
				workerGroup = &nar.Spec.NodeGroup[i]
			}
		}

		Expect(masterGroup).ToNot(BeNil())
		Expect(masterGroup.Size).To(Equal(2)) // 2 master nodes
		Expect(masterGroup.NodeGroupData.Role).To(Equal("master"))

		Expect(workerGroup).ToNot(BeNil())
		Expect(workerGroup.Size).To(Equal(1)) // 1 worker node
		Expect(workerGroup.NodeGroupData.Role).To(Equal("worker"))
	})

	It("should set ConfigTransactionId to ProvisioningRequest generation", func() {
		clusterInstance := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "siteconfig.openshift.io/v1alpha1",
				"kind":       "ClusterInstance",
				"metadata": map[string]interface{}{
					"name":      "exampleCluster",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"nodes": []interface{}{
						map[string]interface{}{
							"role": "master",
						},
					},
				},
			},
		}

		task.clusterInput = &clusterInput{
			hwMgmtData: map[string]any{
				"hardwarePluginRef":           "test-plugin",
				"hardwareProvisioningTimeout": "60m",
				"nodeGroupData": []any{
					map[string]any{"name": "controller", "role": "master", "hwProfile": "test-profile"},
				},
			},
		}

		nar, err := task.buildNodeAllocationRequest(clusterInstance)
		Expect(err).ToNot(HaveOccurred())
		Expect(nar).ToNot(BeNil())
		Expect(nar.Spec.ConfigTransactionId).To(Equal(int64(1))) // Should match PR generation
		Expect(nar.Spec.HardwareProvisioningTimeout).ToNot(BeNil())
		Expect(nar.Spec.HardwareProvisioningTimeout).To(Equal("60m"))
	})

	It("should use default timeout when template timeout is empty", func() {
		clusterInstance := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "siteconfig.openshift.io/v1alpha1",
				"kind":       "ClusterInstance",
				"metadata": map[string]interface{}{
					"name":      "exampleCluster",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"nodes": []interface{}{
						map[string]interface{}{
							"role": "master",
						},
					},
				},
			},
		}

		task.clusterInput = &clusterInput{
			hwMgmtData: map[string]any{
				"hardwarePluginRef":           "test-plugin",
				"hardwareProvisioningTimeout": "", // Empty timeout
				"nodeGroupData": []any{
					map[string]any{"name": "controller", "role": "master", "hwProfile": "test-profile"},
				},
			},
		}

		nar, err := task.buildNodeAllocationRequest(clusterInstance)
		Expect(err).ToNot(HaveOccurred())
		Expect(nar).ToNot(BeNil())
		Expect(nar.Spec.HardwareProvisioningTimeout).ToNot(BeNil())
		Expect(nar.Spec.HardwareProvisioningTimeout).To(Equal("1h30m0s")) // Default timeout
	})

	It("returns error when spec.nodes not found", func() {
		clusterInstance := &unstructured.Unstructured{}
		clusterInstance.Object = map[string]interface{}{
			"spec": map[string]interface{}{
				// no nodes field
			},
		}

		task.clusterInput = &clusterInput{
			hwMgmtData: map[string]any{},
		}

		nar, err := task.buildNodeAllocationRequest(clusterInstance)
		Expect(err).To(HaveOccurred())
		Expect(nar).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("spec.nodes not found in cluster instance"))
	})

	It("uses overridden hwProfile from merged hwMgmtData", func() {
		clusterInstance := &unstructured.Unstructured{}
		clusterInstance.Object = map[string]interface{}{
			"spec": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{
						"role": "master",
					},
					map[string]interface{}{
						"role": "worker",
					},
				},
			},
		}

		// The hwMgmtData is already merged with overrides before buildNodeAllocationRequest is called
		task.clusterInput = &clusterInput{
			hwMgmtData: map[string]any{
				"nodeGroupData": []any{
					map[string]any{"name": "controller", "role": "master", "hwProfile": "override-profile-1", "resourcePoolId": "pool-1"},
					map[string]any{"name": "worker", "role": "worker", "hwProfile": "template-profile-2", "resourcePoolId": "pool-2"},
				},
			},
		}

		nar, err := task.buildNodeAllocationRequest(clusterInstance)
		Expect(err).ToNot(HaveOccurred())
		Expect(nar).ToNot(BeNil())
		Expect(nar.Spec.NodeGroup).To(HaveLen(2))

		// Check that controller group uses the overridden profile
		var controllerGroup, workerGroup *pluginsv1alpha1.NodeGroup
		for i := range nar.Spec.NodeGroup {
			if nar.Spec.NodeGroup[i].NodeGroupData.Name == "controller" {
				controllerGroup = &nar.Spec.NodeGroup[i]
			} else if nar.Spec.NodeGroup[i].NodeGroupData.Name == groupNameWorker {
				workerGroup = &nar.Spec.NodeGroup[i]
			}
		}

		Expect(controllerGroup).ToNot(BeNil())
		Expect(controllerGroup.NodeGroupData.HwProfile).To(Equal("override-profile-1"))

		// Worker should still use the template profile
		Expect(workerGroup).ToNot(BeNil())
		Expect(workerGroup.NodeGroupData.HwProfile).To(Equal("template-profile-2"))
	})

	It("uses hwProfile from hwMgmtData defaults", func() {
		clusterInstance := &unstructured.Unstructured{}
		clusterInstance.Object = map[string]interface{}{
			"spec": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{
						"role": "master",
					},
				},
			},
		}

		task.clusterInput = &clusterInput{
			hwMgmtData: map[string]any{
				"nodeGroupData": []any{
					map[string]any{"name": "controller", "role": "master", "hwProfile": "template-profile-1", "resourcePoolId": "pool-1"},
				},
			},
		}

		nar, err := task.buildNodeAllocationRequest(clusterInstance)
		Expect(err).ToNot(HaveOccurred())
		Expect(nar).ToNot(BeNil())
		Expect(nar.Spec.NodeGroup).To(HaveLen(1))
		Expect(nar.Spec.NodeGroup[0].NodeGroupData.HwProfile).To(Equal("template-profile-1"))
	})

	It("succeeds when nodeGroup has no hwProfile, resourcePoolId, or resourceSelector", func() {
		clusterInstance := &unstructured.Unstructured{}
		clusterInstance.Object = map[string]interface{}{
			"spec": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{
						"role": "master",
					},
				},
			},
		}

		task.clusterInput = &clusterInput{
			hwMgmtData: map[string]any{
				"nodeGroupData": []any{
					map[string]any{"name": "controller", "role": "master"},
				},
			},
		}

		nar, err := task.buildNodeAllocationRequest(clusterInstance)
		Expect(err).ToNot(HaveOccurred())
		Expect(nar).ToNot(BeNil())
		Expect(nar.Spec.NodeGroup).To(HaveLen(1))
		Expect(nar.Spec.NodeGroup[0].NodeGroupData.Name).To(Equal("controller"))
		Expect(nar.Spec.NodeGroup[0].NodeGroupData.Role).To(Equal("master"))
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
			logger: reconciler.Logger,
			client: reconciler.Client,
			object: cr,
			ctDetails: &clusterTemplateDetails{
				namespace: "test-ns",
				templates: provisioningv1alpha1.TemplateDefaults{
					HwMgmtDefaults: provisioningv1alpha1.HwMgmtDefaults{
						HardwarePluginRef: testMetal3HardwarePluginRef,
					},
				},
			},
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
						NodeAllocationRequestID: crName,
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
				TemplateDefaults: provisioningv1alpha1.TemplateDefaults{
					HwMgmtDefaults: provisioningv1alpha1.HwMgmtDefaults{
						HardwarePluginRef:           testMetal3HardwarePluginRef,
						HardwareProvisioningTimeout: "90m",
						NodeGroupData: []hwmgmtv1alpha1.NodeGroupData{
							{Name: "controller", Role: "master", HwProfile: "profile-64G"},
						},
					},
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

		c = getFakeClientFromObjects([]client.Object{cr, ct}...)
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
				templates: provisioningv1alpha1.TemplateDefaults{
					HwMgmtDefaults: provisioningv1alpha1.HwMgmtDefaults{
						HardwarePluginRef: testMetal3HardwarePluginRef,
					},
				},
			},
		}

		// Set up hwpluginClient using the test Metal3 hardware plugin
	})

	It("returns provisioned=true and configured=true when both conditions are met", func() {
		provisionedConfiguredMock := &pluginsv1alpha1.NodeAllocationRequest{
			Status: pluginsv1alpha1.NodeAllocationRequestStatus{
				Conditions: []metav1.Condition{
					{Type: "Provisioned", Status: metav1.ConditionTrue, Reason: "Completed", Message: "Hardware provisioned", LastTransitionTime: metav1.Now()},
					{Type: "Configured", Status: metav1.ConditionTrue, Reason: "Completed", Message: "Hardware configured", LastTransitionTime: metav1.Now()},
				},
				ObservedConfigTransactionId: cr.Generation,
			},
		}

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
		notProvisionedMock := &pluginsv1alpha1.NodeAllocationRequest{
			Status: pluginsv1alpha1.NodeAllocationRequestStatus{
				Conditions: []metav1.Condition{
					{Type: "Provisioned", Status: metav1.ConditionFalse, Reason: "InProgress", Message: "Hardware provisioning in progress", LastTransitionTime: metav1.Now()},
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
		onlyProvisionedMock := &pluginsv1alpha1.NodeAllocationRequest{
			Status: pluginsv1alpha1.NodeAllocationRequestStatus{
				Conditions: []metav1.Condition{
					{Type: "Provisioned", Status: metav1.ConditionTrue, Reason: "Completed", Message: "Hardware provisioned", LastTransitionTime: metav1.Now()},
				},
			},
		}

		provisioned, timedOutOrFailed, err := task.checkNodeAllocationRequestStatus(ctx, onlyProvisionedMock, hwmgmtv1alpha1.Provisioned)
		Expect(err).ToNot(HaveOccurred())
		Expect(provisioned).To(BeTrue())
		Expect(timedOutOrFailed).To(BeFalse())

		configured, timedOutOrFailed, err := task.checkNodeAllocationRequestConfigStatus(ctx, onlyProvisionedMock)
		Expect(err).ToNot(HaveOccurred())
		Expect(configured).To(BeNil())
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
			logger: reconciler.Logger,
			client: reconciler.Client,
			object: cr,
			ctDetails: &clusterTemplateDetails{
				namespace: "test-ns",
				templates: provisioningv1alpha1.TemplateDefaults{
					HwMgmtDefaults: provisioningv1alpha1.HwMgmtDefaults{
						HardwarePluginRef: testMetal3HardwarePluginRef,
					},
				},
			},
		}

		// Set up hwpluginClient using the test Metal3 hardware plugin
	})

	It("returns nil when NAR does not exist", func() {
		hwMgmtData := map[string]any{}
		nodeAllocationRequestId := "test-id"

		response, err := task.checkExistingNodeAllocationRequest(ctx, hwMgmtData, nodeAllocationRequestId)
		Expect(err).ToNot(HaveOccurred())
		Expect(response).To(BeNil())
	})

	It("returns response when NodeAllocationRequest exists and matches", func() {
		// Create a NAR CR in the fake client
		nar := &pluginsv1alpha1.NodeAllocationRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crName,
				Namespace: constants.DefaultNamespace,
			},
			Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
				ClusterId: crName,
				NodeGroup: []pluginsv1alpha1.NodeGroup{
					{
						NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
							Name: "controller", Role: "master", HwProfile: "profile-spr-single-processor-64G",
						},
					},
					{
						NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
							Name: "worker", Role: "worker", HwProfile: "profile-spr-dual-processor-128G",
						},
					},
				},
			},
		}
		Expect(c.Create(ctx, nar)).To(Succeed())

		hwMgmtData := map[string]any{
			"hardwarePluginRef": testMetal3HardwarePluginRef,
			"nodeGroupData": []any{
				map[string]any{"name": "controller", "role": "master", "hwProfile": "profile-spr-single-processor-64G"},
				map[string]any{"name": "worker", "role": "worker", "hwProfile": "profile-spr-dual-processor-128G"},
			},
		}
		nodeAllocationRequestId := crName

		response, err := task.checkExistingNodeAllocationRequest(ctx, hwMgmtData, nodeAllocationRequestId)
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
		nar         *pluginsv1alpha1.NodeAllocationRequest
		crName      = "cluster-1"
		ctNamespace = "clustertemplate-a-v4-16"
		tName       = "clustertemplate-a"
		tVersion    = "v1.0.0"
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
									"label":      constants.BootInterfaceLabel,
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
									"label":      constants.BootInterfaceLabel,
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
				TemplateDefaults: provisioningv1alpha1.TemplateDefaults{
					HwMgmtDefaults: provisioningv1alpha1.HwMgmtDefaults{
						HardwarePluginRef:           testMetal3HardwarePluginRef,
						HardwareProvisioningTimeout: "90m",
						NodeGroupData: []hwmgmtv1alpha1.NodeGroupData{
							{Name: "controller", Role: "master", HwProfile: "profile-64G"},
						},
					},
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
							Label:      constants.BootInterfaceLabel,
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
							Label:      constants.BootInterfaceLabel,
						},
					},
				},
			},
		}

		// Set up node allocation request
		nar = &pluginsv1alpha1.NodeAllocationRequest{
			Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
				NodeGroup: []pluginsv1alpha1.NodeGroup{
					{
						NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
							Name: "controller",
							Role: "master",
						},
					},
					{
						NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
							Name: "worker",
							Role: "worker",
						},
					},
				},
			},
		}

		c = getFakeClientFromObjects([]client.Object{cr, ct}...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &provisioningRequestReconcilerTask{
			logger: reconciler.Logger,
			client: reconciler.Client,
			object: cr,
			ctDetails: &clusterTemplateDetails{
				templates: provisioningv1alpha1.TemplateDefaults{
					HwMgmtDefaults: provisioningv1alpha1.HwMgmtDefaults{
						HardwarePluginRef:           testMetal3HardwarePluginRef,
						HardwareProvisioningTimeout: "90m",
						NodeGroupData: []hwmgmtv1alpha1.NodeGroupData{
							{Name: "controller", Role: "master", HwProfile: "profile-64G"},
						},
					},
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
										"label":      constants.BootInterfaceLabel,
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
										"label":      constants.BootInterfaceLabel,
										"macAddress": "",
									},
								},
							},
						},
					},
				},
			},
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
							Label:      constants.BootInterfaceLabel,
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
							Label:      constants.BootInterfaceLabel,
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
									"label":      constants.BootInterfaceLabel,
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
									"label":      constants.BootInterfaceLabel,
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
					Label:      constants.BootInterfaceLabel,
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

var _ = Describe("ProvisioningRequest Status Update After Hardware Failure", func() {
	var (
		c               client.Client
		ctx             context.Context
		logger          *slog.Logger
		reconciler      *ProvisioningRequestReconciler
		task            *provisioningRequestReconcilerTask
		cr              *provisioningv1alpha1.ProvisioningRequest
		template        *provisioningv1alpha1.ClusterTemplate
		testClusterName = "test-update-after-failure-cluster"
		testNARID       = "test-nar-failed-update"
	)

	BeforeEach(func() {
		ctx = context.Background()
		logger = slog.New(slog.DiscardHandler)

		// Create a ClusterTemplate
		template = &provisioningv1alpha1.ClusterTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-template-update-after-failure.v1.0.0",
				Namespace: "test-ns",
			},
			Spec: provisioningv1alpha1.ClusterTemplateSpec{
				Release: "4.17.0",
				TemplateDefaults: provisioningv1alpha1.TemplateDefaults{
					ClusterInstanceDefaults: "test-cluster-defaults",
					HwMgmtDefaults: provisioningv1alpha1.HwMgmtDefaults{
						HardwarePluginRef:           testMetal3HardwarePluginRef,
						HardwareProvisioningTimeout: "90m",
						NodeGroupData: []hwmgmtv1alpha1.NodeGroupData{
							{Name: "controller", Role: "master", HwProfile: "profile-64G"},
						},
					},
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
						NodeAllocationRequestID: testNARID,
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

		c = getFakeClientFromObjects([]client.Object{cr, template}...)
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
				templates: provisioningv1alpha1.TemplateDefaults{
					HwMgmtDefaults: provisioningv1alpha1.HwMgmtDefaults{
						HardwarePluginRef: testMetal3HardwarePluginRef,
					},
				},
			},
			timeouts: &timeouts{
				hardwareProvisioning: 30 * time.Minute,
			},
		}
	})

	Context("when ProvisioningRequest is updated after hardware failure", func() {
		It("should process hardware failure normally with callback-only approach", func() {
			// With callback-only approach, hardware failures are processed when received via callbacks
			// No transaction ID protection is needed since callbacks ensure fresh status

			// Create a mock NodeAllocationRequest response that shows failure
			failedMock := createMockNodeAllocationRequestResponse("False", "Failed", "Hardware provisioning failed")

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

var _ = Describe("processExistingHardwareCondition", func() {
	var (
		ctx         context.Context
		c           client.Client
		reconciler  *ProvisioningRequestReconciler
		task        *provisioningRequestReconcilerTask
		pr          *provisioningv1alpha1.ProvisioningRequest
		clusterName = "cluster-1"
	)

	BeforeEach(func() {
		ctx = context.Background()

		pr = &provisioningv1alpha1.ProvisioningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
			Status: provisioningv1alpha1.ProvisioningRequestStatus{
				Extensions: provisioningv1alpha1.Extensions{
					NodeAllocationRequestRef: &provisioningv1alpha1.NodeAllocationRequestRef{
						NodeAllocationRequestID: clusterName,
					},
				},
			},
		}

		c = getFakeClientFromObjects([]client.Object{pr}...)
		reconciler = &ProvisioningRequestReconciler{
			Client: c,
			Logger: logger,
		}
		task = &provisioningRequestReconcilerTask{
			logger: reconciler.Logger,
			client: reconciler.Client,
			object: pr,
			timeouts: &timeouts{
				hardwareProvisioning: 1 * time.Minute,
			},
			ctDetails: &clusterTemplateDetails{
				namespace: "test-ns",
				templates: provisioningv1alpha1.TemplateDefaults{
					HwMgmtDefaults: provisioningv1alpha1.HwMgmtDefaults{
						HardwarePluginRef: testMetal3HardwarePluginRef,
					},
				},
			},
		}
	})

	Context("when HardwareProvisioned condition fails", func() {
		It("preserves detailed NAR error message in PR condition", func() {
			detailedError := "Creation request failed: not enough free resources matching nodegroup=controller criteria: freenodes=0, required=1"
			hwCondition := &metav1.Condition{
				Type:    string(hwmgmtv1alpha1.Provisioned),
				Status:  metav1.ConditionFalse,
				Reason:  string(hwmgmtv1alpha1.Failed),
				Message: detailedError,
			}

			status, reason, message, timedOutOrFailed := task.processExistingHardwareCondition(hwCondition, hwmgmtv1alpha1.Provisioned)

			Expect(status).To(Equal(metav1.ConditionFalse))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.Failed)))
			Expect(timedOutOrFailed).To(BeTrue())
			// Verify the detailed error is preserved with context prefix
			Expect(message).To(Equal("Hardware provisioning failed: " + detailedError))
		})
	})

	Context("when HardwareConfigured condition fails", func() {
		It("preserves detailed NAR error message in PR condition", func() {
			detailedError := "Configuration failed: unable to apply BIOS settings on node controller-0"
			hwCondition := &metav1.Condition{
				Type:    string(hwmgmtv1alpha1.Configured),
				Status:  metav1.ConditionFalse,
				Reason:  string(hwmgmtv1alpha1.Failed),
				Message: detailedError,
			}

			status, reason, message, timedOutOrFailed := task.processExistingHardwareCondition(hwCondition, hwmgmtv1alpha1.Configured)

			Expect(status).To(Equal(metav1.ConditionFalse))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.Failed)))
			Expect(timedOutOrFailed).To(BeTrue())
			// Verify the detailed error is preserved with context prefix
			Expect(message).To(Equal("Hardware configuring failed: " + detailedError))
		})
	})

	Context("when HardwareProvisioned times out", func() {
		It("preserves timeout message", func() {
			// With the new timeout handling approach, timeouts are detected at the NodeAllocationRequest level
			// and propagated via callbacks. The ProvisioningRequest controller no longer detects timeouts directly.
			// Instead, it receives timeout status via callbacks from the hardware plugin.
			hwCondition := &metav1.Condition{
				Type:    string(hwmgmtv1alpha1.Provisioned),
				Status:  metav1.ConditionFalse,
				Reason:  string(hwmgmtv1alpha1.TimedOut), // Hardware plugin reports timeout via callback
				Message: "Hardware provisioning timed out",
			}

			status, reason, message, timedOutOrFailed := task.processExistingHardwareCondition(hwCondition, hwmgmtv1alpha1.Provisioned)

			Expect(status).To(Equal(metav1.ConditionFalse))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.TimedOut)))
			Expect(timedOutOrFailed).To(BeTrue())
			Expect(message).To(Equal("Hardware provisioning failed: Hardware provisioning timed out"))
		})
	})

	Context("when HardwareConfigured times out", func() {
		It("preserves timeout message", func() {
			// With the new timeout handling approach, timeouts are detected at the NodeAllocationRequest level
			// and propagated via callbacks. The ProvisioningRequest controller no longer detects timeouts directly.
			// Instead, it receives timeout status via callbacks from the hardware plugin.
			hwCondition := &metav1.Condition{
				Type:    string(hwmgmtv1alpha1.Configured),
				Status:  metav1.ConditionFalse,
				Reason:  string(hwmgmtv1alpha1.TimedOut), // Hardware plugin reports timeout via callback
				Message: "Hardware configuration timed out",
			}
			status, reason, message, timedOutOrFailed := task.processExistingHardwareCondition(hwCondition, hwmgmtv1alpha1.Configured)

			Expect(status).To(Equal(metav1.ConditionFalse))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.TimedOut)))
			Expect(timedOutOrFailed).To(BeTrue())
			Expect(message).To(Equal("Hardware configuring failed: Hardware configuration timed out"))
		})
	})

	Context("when HardwareProvisioned completes successfully", func() {
		It("updates provisioningStatus with success message", func() {
			hwCondition := &metav1.Condition{
				Type:    string(hwmgmtv1alpha1.Provisioned),
				Status:  metav1.ConditionTrue,
				Reason:  string(hwmgmtv1alpha1.Completed),
				Message: "Created",
			}

			status, reason, message, timedOutOrFailed := task.processExistingHardwareCondition(hwCondition, hwmgmtv1alpha1.Provisioned)

			Expect(status).To(Equal(metav1.ConditionTrue))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.Completed)))
			Expect(timedOutOrFailed).To(BeFalse())
			Expect(message).To(Equal("Hardware provisioning completed: Created"))
			// Verify provisioningStatus is updated to progressing
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(Equal(provisioningv1alpha1.StateProgressing))
			Expect(task.object.Status.ProvisioningStatus.ProvisioningDetails).To(ContainSubstring("Hardware provisioning completed"))
		})
	})

	Context("when HardwareConfigured completes successfully", func() {
		It("updates provisioningStatus with success message", func() {
			hwCondition := &metav1.Condition{
				Type:    string(hwmgmtv1alpha1.Configured),
				Status:  metav1.ConditionTrue,
				Reason:  string(hwmgmtv1alpha1.Completed),
				Message: "Configuration applied",
			}

			status, reason, message, timedOutOrFailed := task.processExistingHardwareCondition(hwCondition, hwmgmtv1alpha1.Configured)

			Expect(status).To(Equal(metav1.ConditionTrue))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.Completed)))
			Expect(timedOutOrFailed).To(BeFalse())
			Expect(message).To(Equal("Hardware configuring completed: Configuration applied"))
			// Verify provisioningStatus is updated to progressing
			Expect(task.object.Status.ProvisioningStatus.ProvisioningPhase).To(Equal(provisioningv1alpha1.StateProgressing))
			Expect(task.object.Status.ProvisioningStatus.ProvisioningDetails).To(ContainSubstring("Hardware configuring completed"))
		})
	})

	Context("when HardwareProvisioned is in progress with context", func() {
		It("preserves NAR context message", func() {
			narContextMessage := "Handling creation"
			hwCondition := &metav1.Condition{
				Type:    string(hwmgmtv1alpha1.Provisioned),
				Status:  metav1.ConditionFalse,
				Reason:  string(hwmgmtv1alpha1.InProgress),
				Message: narContextMessage,
			}

			status, reason, message, timedOutOrFailed := task.processExistingHardwareCondition(hwCondition, hwmgmtv1alpha1.Provisioned)

			Expect(status).To(Equal(metav1.ConditionFalse))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.InProgress)))
			Expect(timedOutOrFailed).To(BeFalse())
			// Verify the NAR context is preserved
			Expect(message).To(Equal("Hardware provisioning is in progress: " + narContextMessage))
		})
	})

	Context("when HardwareProvisioned is in progress without context", func() {
		It("uses generic in-progress message", func() {
			hwCondition := &metav1.Condition{
				Type:    string(hwmgmtv1alpha1.Provisioned),
				Status:  metav1.ConditionFalse,
				Reason:  string(hwmgmtv1alpha1.InProgress),
				Message: "", // Empty message
			}

			status, reason, message, timedOutOrFailed := task.processExistingHardwareCondition(hwCondition, hwmgmtv1alpha1.Provisioned)

			Expect(status).To(Equal(metav1.ConditionFalse))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.InProgress)))
			Expect(timedOutOrFailed).To(BeFalse())
			// Verify generic message when no NAR context
			Expect(message).To(Equal("Hardware provisioning is in progress"))
		})
	})

	Context("when HardwareConfigured is in progress with context", func() {
		It("preserves NAR context message", func() {
			narContextMessage := "AwaitConfig"
			hwCondition := &metav1.Condition{
				Type:    string(hwmgmtv1alpha1.Configured),
				Status:  metav1.ConditionFalse,
				Reason:  string(hwmgmtv1alpha1.InProgress),
				Message: narContextMessage,
			}

			status, reason, message, timedOutOrFailed := task.processExistingHardwareCondition(hwCondition, hwmgmtv1alpha1.Configured)

			Expect(status).To(Equal(metav1.ConditionFalse))
			Expect(reason).To(Equal(string(hwmgmtv1alpha1.InProgress)))
			Expect(timedOutOrFailed).To(BeFalse())
			// Verify the NAR context is preserved with "configuring" (verb form)
			Expect(message).To(Equal("Hardware configuring is in progress: " + narContextMessage))
		})
	})

	Context("stale terminal NAR status guard", func() {
		It("skips update when NAR has Failed condition and plugin has not observed new transaction", func() {
			narWithStaleFailure := &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-1",
					Namespace: constants.DefaultNamespace,
				},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					ConfigTransactionId: 3,
				},
				Status: pluginsv1alpha1.NodeAllocationRequestStatus{
					ObservedConfigTransactionId: 2,
					Conditions: []metav1.Condition{
						{
							Type:               string(hwmgmtv1alpha1.Configured),
							Status:             metav1.ConditionFalse,
							Reason:             string(hwmgmtv1alpha1.Failed),
							Message:            "Previous attempt failed",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			}

			status, timedOutOrFailed, err := task.checkNodeAllocationRequestStatus(ctx, narWithStaleFailure, hwmgmtv1alpha1.Configured)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(BeFalse())
			Expect(timedOutOrFailed).To(BeFalse())
		})

		It("skips update when NAR has TimedOut condition and plugin has not observed new transaction", func() {
			narWithStaleTimeout := &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-1",
					Namespace: constants.DefaultNamespace,
				},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					ConfigTransactionId: 3,
				},
				Status: pluginsv1alpha1.NodeAllocationRequestStatus{
					ObservedConfigTransactionId: 2,
					Conditions: []metav1.Condition{
						{
							Type:               string(hwmgmtv1alpha1.Configured),
							Status:             metav1.ConditionFalse,
							Reason:             string(hwmgmtv1alpha1.TimedOut),
							Message:            "Previous attempt timed out",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			}

			status, timedOutOrFailed, err := task.checkNodeAllocationRequestStatus(ctx, narWithStaleTimeout, hwmgmtv1alpha1.Configured)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(BeFalse())
			Expect(timedOutOrFailed).To(BeFalse())
		})

		It("skips update when NAR has stale True condition from previous success", func() {
			narWithStaleSuccess := &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-1",
					Namespace: constants.DefaultNamespace,
				},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					ConfigTransactionId: 3,
				},
				Status: pluginsv1alpha1.NodeAllocationRequestStatus{
					ObservedConfigTransactionId: 2,
					Conditions: []metav1.Condition{
						{
							Type:               string(hwmgmtv1alpha1.Configured),
							Status:             metav1.ConditionTrue,
							Reason:             string(hwmgmtv1alpha1.ConfigApplied),
							Message:            "Previous config applied",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			}

			status, timedOutOrFailed, err := task.checkNodeAllocationRequestStatus(ctx, narWithStaleSuccess, hwmgmtv1alpha1.Configured)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(BeFalse())
			Expect(timedOutOrFailed).To(BeFalse())
		})

		It("allows update when plugin has observed the current transaction", func() {
			narWithCurrentFailure := &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-1",
					Namespace: constants.DefaultNamespace,
				},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					ConfigTransactionId: 3,
				},
				Status: pluginsv1alpha1.NodeAllocationRequestStatus{
					ObservedConfigTransactionId: 3,
					Conditions: []metav1.Condition{
						{
							Type:               string(hwmgmtv1alpha1.Configured),
							Status:             metav1.ConditionFalse,
							Reason:             string(hwmgmtv1alpha1.Failed),
							Message:            "Current attempt failed",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			}

			status, timedOutOrFailed, err := task.checkNodeAllocationRequestStatus(ctx, narWithCurrentFailure, hwmgmtv1alpha1.Configured)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(BeFalse())
			Expect(timedOutOrFailed).To(BeTrue())
		})

		It("does not apply guard to Provisioned condition during initial provisioning", func() {
			narInitialProvisioning := &pluginsv1alpha1.NodeAllocationRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-1",
					Namespace: constants.DefaultNamespace,
				},
				Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
					ConfigTransactionId: 1,
				},
				Status: pluginsv1alpha1.NodeAllocationRequestStatus{
					ObservedConfigTransactionId: 0,
					Conditions: []metav1.Condition{
						{
							Type:               string(hwmgmtv1alpha1.Provisioned),
							Status:             metav1.ConditionTrue,
							Reason:             string(hwmgmtv1alpha1.Completed),
							Message:            "Hardware provisioning completed",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			}

			status, timedOutOrFailed, err := task.checkNodeAllocationRequestStatus(ctx, narInitialProvisioning, hwmgmtv1alpha1.Provisioned)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(BeTrue())
			Expect(timedOutOrFailed).To(BeFalse())
		})
	})

	Context("integration test: updateHardwareStatus with callback for failed condition", func() {
		It("propagates detailed error through the full flow", func() {
			detailedError := "Creation request failed: not enough free resources matching nodegroup=controller criteria: freenodes=0, required=1"

			// Get or create the ProvisioningRequest from client
			// Use local variable to avoid shadowing issues
			pr := &provisioningv1alpha1.ProvisioningRequest{}
			key := client.ObjectKey{Name: "test-pr-update-after-failure", Namespace: "test-ns"}
			if err := c.Get(ctx, key, pr); err != nil {
				// Object doesn't exist, create a basic one
				pr = &provisioningv1alpha1.ProvisioningRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:      key.Name,
						Namespace: key.Namespace,
					},
				}
				Expect(c.Create(ctx, pr)).To(Succeed())
				// Refresh to get the created object
				Expect(c.Get(ctx, key, pr)).To(Succeed())
			}

			// Set up NodeAllocationRequestRef if not already set (required for updateHardwareStatus)
			if pr.Status.Extensions.NodeAllocationRequestRef == nil {
				pr.Status.Extensions.NodeAllocationRequestRef = &provisioningv1alpha1.NodeAllocationRequestRef{
					NodeAllocationRequestID: "test-nar-id",
				}
				Expect(c.Status().Update(ctx, pr)).To(Succeed())
				// Refresh to get the updated status
				Expect(c.Get(ctx, key, pr)).To(Succeed())
			}

			// Update task object to reflect the current PR
			task.object = pr

			// Create mock NAR response with detailed error
			failedMock := createMockNodeAllocationRequestResponse("False", "Failed", detailedError)

			// Call updateHardwareStatus
			provisioned, timedOutOrFailed, err := task.updateHardwareStatus(ctx, failedMock, hwmgmtv1alpha1.Provisioned)

			Expect(err).ToNot(HaveOccurred())
			Expect(provisioned).To(BeFalse())
			Expect(timedOutOrFailed).To(BeTrue())

			// Refresh CR to get updated status
			var updatedCR provisioningv1alpha1.ProvisioningRequest
			Expect(c.Get(ctx, client.ObjectKeyFromObject(pr), &updatedCR)).To(Succeed())

			// Verify the condition has the detailed error message
			condition := meta.FindStatusCondition(updatedCR.Status.Conditions, string(provisioningv1alpha1.PRconditionTypes.HardwareProvisioned))
			Expect(condition).ToNot(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(string(provisioningv1alpha1.CRconditionReasons.Failed)))
			Expect(condition.Message).To(ContainSubstring("Hardware provisioning failed"))
			Expect(condition.Message).To(ContainSubstring(detailedError))

			// Verify provisioning status details also contain the error
			Expect(updatedCR.Status.ProvisioningStatus.ProvisioningDetails).To(ContainSubstring("Hardware provisioning failed"))
			Expect(updatedCR.Status.ProvisioningStatus.ProvisioningDetails).To(ContainSubstring(detailedError))
		})
	})

	Context("waitingForConfigStart code path", func() {
		BeforeEach(func() {
			// Create or get the ProvisioningRequest for this test
			// Use local variable to avoid shadowing issues
			pr := &provisioningv1alpha1.ProvisioningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pr-update-after-failure",
					Namespace:  "test-ns",
					Generation: 5,
				},
				Status: provisioningv1alpha1.ProvisioningRequestStatus{
					Extensions: provisioningv1alpha1.Extensions{
						NodeAllocationRequestRef: &provisioningv1alpha1.NodeAllocationRequestRef{
							NodeAllocationRequestID: "test-nar-id",
						},
					},
				},
			}
			// Try to get it first, if it doesn't exist, create it
			existing := &provisioningv1alpha1.ProvisioningRequest{}
			if err := c.Get(ctx, client.ObjectKeyFromObject(pr), existing); err != nil {
				// Object doesn't exist, create it
				Expect(c.Create(ctx, pr)).To(Succeed())
			} else {
				// Object exists, update it with our test configuration
				existing.Status.Extensions.NodeAllocationRequestRef = pr.Status.Extensions.NodeAllocationRequestRef
				existing.Generation = 5
				Expect(c.Status().Update(ctx, existing)).To(Succeed())
				pr = existing
			}
			// Refresh from client to get the latest version
			Expect(c.Get(ctx, client.ObjectKeyFromObject(pr), pr)).To(Succeed())
			task.object = pr
		})

		It("should return ConditionDoesNotExistsErr when waiting for config start", func() {
			// Create NAR response without Configured condition and ObservedConfigTransactionId not matching
			// This simulates waiting for a new configuration transaction to start
			observedID := int64(3) // Different from PR Generation (5)
			mockResponse := &pluginsv1alpha1.NodeAllocationRequest{
				Status: pluginsv1alpha1.NodeAllocationRequestStatus{
					ObservedConfigTransactionId: observedID,
					Conditions:                  []metav1.Condition{}, // No Configured condition
				},
			}

			configured, timedOutOrFailed, err := task.updateHardwareStatus(ctx, mockResponse, hwmgmtv1alpha1.Configured)

			Expect(err).To(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(&utils.ConditionDoesNotExistsErr{}))
			Expect(configured).To(BeFalse())
			Expect(timedOutOrFailed).To(BeFalse())
		})

		It("should return ConditionDoesNotExistsErr when ObservedConfigTransactionId is zero", func() {
			// Create NAR response without Configured condition and zero ObservedConfigTransactionId
			// Zero indicates the transaction has not been observed yet
			mockResponse := &pluginsv1alpha1.NodeAllocationRequest{
				Status: pluginsv1alpha1.NodeAllocationRequestStatus{
					ObservedConfigTransactionId: 0,
					Conditions:                  []metav1.Condition{}, // No Configured condition
				},
			}

			configured, timedOutOrFailed, err := task.updateHardwareStatus(ctx, mockResponse, hwmgmtv1alpha1.Configured)

			Expect(err).To(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(&utils.ConditionDoesNotExistsErr{}))
			Expect(configured).To(BeFalse())
			Expect(timedOutOrFailed).To(BeFalse())
		})

		It("should proceed normally when ObservedConfigTransactionId matches Generation", func() {
			// Create NAR response with matching ObservedConfigTransactionId
			observedID := int64(5) // Matches PR Generation
			configuredCondition := metav1.Condition{
				Type:    "Configured",
				Status:  "True",
				Reason:  "Completed",
				Message: "Hardware configured successfully",
			}
			mockResponse := &pluginsv1alpha1.NodeAllocationRequest{
				Status: pluginsv1alpha1.NodeAllocationRequestStatus{
					ObservedConfigTransactionId: observedID,
					Conditions:                  []metav1.Condition{configuredCondition},
				},
			}

			configured, timedOutOrFailed, err := task.updateHardwareStatus(ctx, mockResponse, hwmgmtv1alpha1.Configured)

			Expect(err).ToNot(HaveOccurred())
			Expect(configured).To(BeTrue())
			Expect(timedOutOrFailed).To(BeFalse())
		})
	})

	Context("race condition: ConfigTransactionId changes mid-reconciliation", func() {
		BeforeEach(func() {
			// Create or get the ProvisioningRequest for this test
			// Use local variable to avoid shadowing issues
			pr := &provisioningv1alpha1.ProvisioningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pr-update-after-failure",
					Namespace:  "test-ns",
					Generation: 5,
				},
				Status: provisioningv1alpha1.ProvisioningRequestStatus{
					Extensions: provisioningv1alpha1.Extensions{
						NodeAllocationRequestRef: &provisioningv1alpha1.NodeAllocationRequestRef{
							NodeAllocationRequestID: "test-nar-id",
						},
					},
				},
			}
			// Try to get it first, if it doesn't exist, create it
			existing := &provisioningv1alpha1.ProvisioningRequest{}
			if err := c.Get(ctx, client.ObjectKeyFromObject(pr), existing); err != nil {
				// Object doesn't exist, create it
				Expect(c.Create(ctx, pr)).To(Succeed())
			} else {
				// Object exists, update it with our test configuration
				existing.Status.Extensions.NodeAllocationRequestRef = pr.Status.Extensions.NodeAllocationRequestRef
				existing.Generation = 5
				Expect(c.Status().Update(ctx, existing)).To(Succeed())
				pr = existing
			}
			// Refresh from client to get the latest version
			Expect(c.Get(ctx, client.ObjectKeyFromObject(pr), pr)).To(Succeed())
			task.object = pr
		})

		It("should handle ConfigTransactionId change during reconciliation", func() {
			// Simulate a race condition: PR Generation changes during reconciliation
			// Step 1: Start reconciliation with Generation=5
			observedID := int64(5) // Matches initial Generation
			mockResponse1 := &pluginsv1alpha1.NodeAllocationRequest{
				Status: pluginsv1alpha1.NodeAllocationRequestStatus{
					ObservedConfigTransactionId: observedID,
					Conditions:                  []metav1.Condition{}, // No Configured condition yet
				},
			}

			// Step 2: PR spec changes, Generation increments to 6
			task.object.Generation = 6
			Expect(c.Update(ctx, task.object)).To(Succeed())
			// Refresh from client to get updated generation
			Expect(c.Get(ctx, client.ObjectKeyFromObject(task.object), task.object)).To(Succeed())

			// Step 3: Continue reconciliation - ObservedConfigTransactionId (5) no longer matches Generation (6)
			// This should trigger waitingForConfigStart
			configured, timedOutOrFailed, err := task.updateHardwareStatus(ctx, mockResponse1, hwmgmtv1alpha1.Configured)

			Expect(err).To(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(&utils.ConditionDoesNotExistsErr{}))
			Expect(configured).To(BeFalse())
			Expect(timedOutOrFailed).To(BeFalse())
		})

		It("should process new ConfigTransactionId when plugin catches up", func() {
			// Step 1: PR Generation changes from 5 to 6
			task.object.Generation = 6
			Expect(c.Update(ctx, task.object)).To(Succeed())
			// Refresh from client to get updated generation
			Expect(c.Get(ctx, client.ObjectKeyFromObject(task.object), task.object)).To(Succeed())

			// Step 2: Plugin eventually processes the new transaction (Generation 6)
			observedID := int64(6) // Matches new Generation
			configuredCondition := metav1.Condition{
				Type:    "Configured",
				Status:  "True",
				Reason:  "Completed",
				Message: "Hardware configured successfully",
			}
			mockResponse := &pluginsv1alpha1.NodeAllocationRequest{
				Status: pluginsv1alpha1.NodeAllocationRequestStatus{
					ObservedConfigTransactionId: observedID,
					Conditions:                  []metav1.Condition{configuredCondition},
				},
			}

			configured, timedOutOrFailed, err := task.updateHardwareStatus(ctx, mockResponse, hwmgmtv1alpha1.Configured)

			Expect(err).ToNot(HaveOccurred())
			Expect(configured).To(BeTrue())
			Expect(timedOutOrFailed).To(BeFalse())
		})
	})
})
