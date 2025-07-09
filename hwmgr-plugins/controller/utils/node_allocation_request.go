/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	pluginv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	sharedutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

const (
	NodeAllocationRequestFinalizer = "o2ims-hardwaremanagement.oran.openshift.io/nodeallocationrequest-finalizer"
)

var nodeAllocationRequestGVK schema.GroupVersionKind

func InitNodeAllocationRequestUtils(scheme *runtime.Scheme) error {
	nodeAllocationRequest := &pluginv1alpha1.NodeAllocationRequest{}
	gvks, unversioned, err := scheme.ObjectKinds(nodeAllocationRequest)
	if err != nil {
		return fmt.Errorf("failed to query scheme to get GVK for NodeAllocationRequest CR: %w", err)
	}
	if unversioned || len(gvks) != 1 {
		return fmt.Errorf("expected a single versioned item in ObjectKinds response, got %d with unversioned=%t", len(gvks), unversioned)
	}

	nodeAllocationRequestGVK = gvks[0]

	return nil
}

func GetNodeAllocationRequest(ctx context.Context, client client.Reader, key client.ObjectKey, nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest) error {
	if err := client.Get(ctx, key, nodeAllocationRequest); err != nil {
		return fmt.Errorf("failed to get NodeAllocationRequest: %w", err)
	}

	if nodeAllocationRequest.Kind == "" {
		// The non-caching query doesn't set the GVK for the CR, so do it now
		nodeAllocationRequest.SetGroupVersionKind(nodeAllocationRequestGVK)
	}

	return nil
}

func GetNodeAllocationRequestProvisionedCondition(nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest) *metav1.Condition {
	return meta.FindStatusCondition(
		nodeAllocationRequest.Status.Conditions,
		string(pluginv1alpha1.Provisioned))
}

func IsNodeAllocationRequestProvisionedCompleted(nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest) bool {
	provisionedCondition := GetNodeAllocationRequestProvisionedCondition(nodeAllocationRequest)
	if provisionedCondition != nil && provisionedCondition.Status == metav1.ConditionTrue {
		return true
	}

	return false
}

func IsNodeAllocationRequestProvisionedFailed(nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest) bool {
	provisionedCondition := GetNodeAllocationRequestProvisionedCondition(nodeAllocationRequest)
	if provisionedCondition != nil && provisionedCondition.Reason == string(pluginv1alpha1.Failed) {
		return true
	}

	return false
}

func UpdateNodeAllocationRequestSelectedGroups(
	ctx context.Context,
	c client.Client,
	nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest) error {

	// nolint: wrapcheck
	err := sharedutils.RetryOnConflictOrRetriable(retry.DefaultRetry, func() error {
		newNodeAllocationRequest := &pluginv1alpha1.NodeAllocationRequest{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(nodeAllocationRequest), newNodeAllocationRequest); err != nil {
			return err
		}
		newNodeAllocationRequest.Status.SelectedGroups = nodeAllocationRequest.Status.SelectedGroups
		if err := c.Status().Update(ctx, newNodeAllocationRequest); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to update NodeAllocationRequest selectedGroups: %w", err)
	}

	return nil
}

func UpdateNodeAllocationRequestPluginStatus(
	ctx context.Context,
	c client.Client,
	nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest) error {

	// nolint: wrapcheck
	err := sharedutils.RetryOnConflictOrRetriable(retry.DefaultRetry, func() error {
		newNodeAllocationRequest := &pluginv1alpha1.NodeAllocationRequest{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(nodeAllocationRequest), newNodeAllocationRequest); err != nil {
			return err
		}
		newNodeAllocationRequest.Status.HwMgrPlugin.ObservedGeneration = newNodeAllocationRequest.ObjectMeta.Generation
		if err := c.Status().Update(ctx, newNodeAllocationRequest); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to update NodeAllocationRequest condition: %w", err)
	}

	return nil
}

func UpdateNodeAllocationRequestStatusCondition(
	ctx context.Context,
	c client.Client,
	nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest,
	conditionType pluginv1alpha1.ConditionType,
	conditionReason pluginv1alpha1.ConditionReason,
	conditionStatus metav1.ConditionStatus,
	message string) error {

	SetStatusCondition(&nodeAllocationRequest.Status.Conditions,
		string(conditionType),
		string(conditionReason),
		conditionStatus,
		message)

	// nolint: wrapcheck
	err := sharedutils.RetryOnConflictOrRetriable(retry.DefaultRetry, func() error {
		newNodeAllocationRequest := &pluginv1alpha1.NodeAllocationRequest{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(nodeAllocationRequest), newNodeAllocationRequest); err != nil {
			return err
		}

		SetStatusCondition(&newNodeAllocationRequest.Status.Conditions,
			string(conditionType),
			string(conditionReason),
			conditionStatus,
			message)

		// Update the observed config transaction id if the condition is Configured
		if conditionType == pluginv1alpha1.Configured {
			newNodeAllocationRequest.Status.ObservedConfigTransactionId = nodeAllocationRequest.Spec.ConfigTransactionId
		}

		if err := c.Status().Update(ctx, newNodeAllocationRequest); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to update NodeAllocationRequest condition: %s, %w", nodeAllocationRequest.Name, err)
	}

	return nil
}

func UpdateNodeAllocationRequestProperties(
	ctx context.Context,
	c client.Client,
	nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest) error {

	// nolint: wrapcheck
	err := sharedutils.RetryOnConflictOrRetriable(retry.DefaultRetry, func() error {
		newNodeAllocationRequest := &pluginv1alpha1.NodeAllocationRequest{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(nodeAllocationRequest), newNodeAllocationRequest); err != nil {
			return err
		}
		newNodeAllocationRequest.Status.Properties = nodeAllocationRequest.Status.Properties
		if err := c.Status().Update(ctx, newNodeAllocationRequest); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to update NodeAllocationRequest properties: %w", err)
	}

	return nil
}

func NodeAllocationRequestAddFinalizer(
	ctx context.Context,
	c client.Client,
	nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest,
) error {
	// nolint: wrapcheck
	err := sharedutils.RetryOnConflictOrRetriable(retry.DefaultRetry, func() error {
		newNodeAllocationRequest := &pluginv1alpha1.NodeAllocationRequest{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(nodeAllocationRequest), newNodeAllocationRequest); err != nil {
			return err
		}
		controllerutil.AddFinalizer(newNodeAllocationRequest, NodeAllocationRequestFinalizer)
		if err := c.Update(ctx, newNodeAllocationRequest); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to add finalizer to NodeAllocationRequest: %w", err)
	}
	return nil
}

func NodeAllocationRequestRemoveFinalizer(
	ctx context.Context,
	c client.Client,
	nodeAllocationRequest *pluginv1alpha1.NodeAllocationRequest,
) error {
	// nolint: wrapcheck
	err := sharedutils.RetryOnConflictOrRetriable(retry.DefaultRetry, func() error {
		newNodeAllocationRequest := &pluginv1alpha1.NodeAllocationRequest{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(nodeAllocationRequest), newNodeAllocationRequest); err != nil {
			return err
		}
		controllerutil.RemoveFinalizer(newNodeAllocationRequest, NodeAllocationRequestFinalizer)
		if err := c.Update(ctx, newNodeAllocationRequest); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to remove finalizer from NodeAllocationRequest: %w", err)
	}
	return nil
}
