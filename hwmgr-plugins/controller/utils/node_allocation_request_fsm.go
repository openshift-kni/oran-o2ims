/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"log/slog"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
)

// HardwarePlugin FSM for NodeAllocationRequest
type fsmAction int

const (
	NodeAllocationRequestFSMCreate = iota
	NodeAllocationRequestFSMProcessing
	NodeAllocationRequestFSMSpecChanged
	NodeAllocationRequestFSMNoop
)

func DetermineAction(ctx context.Context, logger *slog.Logger, nodeAllocationRequest *hwmgmtv1alpha1.NodeAllocationRequest) fsmAction {
	if len(nodeAllocationRequest.Status.Conditions) == 0 {
		logger.InfoContext(ctx, "Handling Create NodeAllocationRequest request")
		return NodeAllocationRequestFSMCreate
	}

	provisionedCondition := meta.FindStatusCondition(
		nodeAllocationRequest.Status.Conditions,
		string(hwmgmtv1alpha1.Provisioned))
	if provisionedCondition != nil {
		if provisionedCondition.Status == metav1.ConditionTrue {
			// Check if the generation has changed
			if nodeAllocationRequest.ObjectMeta.Generation != nodeAllocationRequest.Status.HwMgrPlugin.ObservedGeneration {
				logger.InfoContext(ctx, "Handling NodeAllocationRequest Spec change")
				return NodeAllocationRequestFSMSpecChanged
			}
			logger.InfoContext(ctx, "NodeAllocationRequest request in Provisioned state")
			return NodeAllocationRequestFSMNoop
		}

		if provisionedCondition.Reason == string(hwmgmtv1alpha1.Failed) {
			logger.InfoContext(ctx, "NodeAllocationRequest request in Failed state")
			return NodeAllocationRequestFSMNoop
		}

		return NodeAllocationRequestFSMProcessing
	}

	return NodeAllocationRequestFSMNoop
}
