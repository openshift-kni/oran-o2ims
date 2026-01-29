/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	k8sclients "github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
)

// newClientForClusterFunc is a variable that holds the function to create a client for a spoke cluster.
// This allows tests to mock the spoke client creation.
var newClientForClusterFunc = k8sclients.NewClientForCluster

// extractPRNameFromCallback extracts the ProvisioningRequest name from the callback URL.
// The callback URL follows the pattern: /nar-callback/v1/provisioning-requests/{provisioningRequestName}
//
// Note: The callback URL is automatically populated by the provisioning controller when creating
// the NAR, so format errors indicate a bug in the provisioning controller that should be fixed there
// or user corruption should be fixed by the user.
//
// Returns an error if the ProvisioningRequest name cannot be extracted from the callback URL.
func extractPRNameFromCallback(callback *pluginsv1alpha1.Callback) (string, error) {
	if callback == nil || callback.CallbackURL == "" {
		return "", fmt.Errorf("no callback configured")
	}

	callbackURL, err := url.Parse(callback.CallbackURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse callback URL: %w", err)
	}

	if !strings.HasPrefix(callbackURL.Path, constants.NarCallbackServicePath+"/") {
		return "", fmt.Errorf("callback URL does not match expected pattern: %s", callback.CallbackURL)
	}

	prName := strings.TrimPrefix(callbackURL.Path, constants.NarCallbackServicePath+"/")
	if prName == "" {
		return "", fmt.Errorf("could not extract provisioning request name from callback URL: %s", callback.CallbackURL)
	}
	return prName, nil
}

// getHostnameForAllocatedNode retrieves the hostname for an AllocatedNode by querying
// the ProvisioningRequest's AllocatedNodeHostMap. The mapping is automatically populated
// by the provisioning controller during day0 provisioning.
// Returns the hostname or an error if the hostname is not found.
func getHostnameForAllocatedNode(
	ctx context.Context,
	hubClient client.Client,
	nar *pluginsv1alpha1.NodeAllocationRequest,
	allocatedNodeName string,
) (string, error) {
	// Extract PR name from callback URL
	prName, err := extractPRNameFromCallback(nar.Spec.Callback)
	if err != nil {
		return "", fmt.Errorf("failed to extract provisioning request name: %w", err)
	}

	// Query the ProvisioningRequest using hub client
	var pr provisioningv1alpha1.ProvisioningRequest
	if err := hubClient.Get(ctx, client.ObjectKey{Name: prName}, &pr); err != nil {
		return "", fmt.Errorf("failed to get ProvisioningRequest %s: %w", prName, err)
	}

	// Look up the hostname in the AllocatedNodeHostMap
	hostname, ok := pr.Status.Extensions.AllocatedNodeHostMap[allocatedNodeName]
	if !ok || hostname == "" {
		return "", fmt.Errorf("hostname not found for AllocatedNode %s in ProvisioningRequest %s", allocatedNodeName, prName)
	}

	return hostname, nil
}

// CheckNodeReady checks if the K8s node corresponding to the AllocatedNode
// is in Ready state on the spoke cluster.
// Returns (true, nil) if the node is ready.
// Returns (false, nil) if the node is not ready.
// Returns (false, error) if there was an error checking the node status.
func CheckNodeReady(
	ctx context.Context,
	hubClient client.Client,
	logger *slog.Logger,
	nar *pluginsv1alpha1.NodeAllocationRequest,
	allocatedNode *pluginsv1alpha1.AllocatedNode,
) (bool, error) {
	// Get the hostname for this allocated node
	hostname, err := getHostnameForAllocatedNode(ctx, hubClient, nar, allocatedNode.Name)
	if err != nil {
		return false, fmt.Errorf("failed to get hostname for AllocatedNode %s: %w", allocatedNode.Name, err)
	}

	clusterName := nar.Spec.ClusterId
	// Create a spoke client for each check. This is acceptable because:
	// - The hub client fetches the kubeconfig secret from cache, not the API server
	// - Day2 hardware updates are infrequent, and medium requeue intervals limit call frequency
	// - Caching the client would add complexity for invalidation with minimal benefit
	spokeClient, err := newClientForClusterFunc(ctx, hubClient, clusterName)
	if err != nil {
		return false, fmt.Errorf("failed to create spoke client for cluster %s: %w", clusterName, err)
	}

	return isNodeReady(ctx, spokeClient, logger, clusterName, hostname, allocatedNode.Name)
}

// isNodeReady checks if a given K8s node is in Ready state on the spoke cluster.
func isNodeReady(
	ctx context.Context,
	spokeClient client.Client,
	logger *slog.Logger,
	clusterName, hostname, allocatedNodeName string,
) (bool, error) {
	// Get the K8s node from the spoke cluster
	k8sNode := &corev1.Node{}
	if err := spokeClient.Get(ctx, client.ObjectKey{Name: hostname}, k8sNode); err != nil {
		return false, fmt.Errorf("failed to get node %s from cluster %s: %w", hostname, clusterName, err)
	}

	// The node ready condition must be present and be true.
	if !isNodeStatusConditionTrue(k8sNode.Status.Conditions, corev1.NodeReady) {
		logger.InfoContext(ctx, "node is not yet ready on spoke cluster",
			slog.String("allocatedNode", allocatedNodeName),
			slog.String("hostname", hostname),
			slog.String("cluster", clusterName))
		return false, nil
	}

	// Network unavailable condition should be absent or be false.
	if isNodeStatusConditionTrue(k8sNode.Status.Conditions, corev1.NodeNetworkUnavailable) {
		logger.InfoContext(ctx, "node network is unavailable on spoke cluster",
			slog.String("allocatedNode", allocatedNodeName),
			slog.String("hostname", hostname),
			slog.String("cluster", clusterName))
		return false, nil
	}

	// Node is ready
	return true, nil
}

// isNodeStatusConditionTrue checks if the condition type is present and set to true.
func isNodeStatusConditionTrue(conditions []corev1.NodeCondition, conditionType corev1.NodeConditionType) bool {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}
