/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/drain"
	"sigs.k8s.io/controller-runtime/pkg/client"

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	k8sclients "github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
)

const (
	DefaultDrainTimeout   = 30 * time.Second
	DefaultMaxUnavailable = 1
)

// logWriter adapts an slog function into an io.Writer so that the kubectl drain
// helper's Out/ErrOut output is routed to structured logging.
type logWriter struct {
	logFunc func(msg string, args ...any)
}

func (w logWriter) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\n")
	if msg != "" {
		w.logFunc(msg)
	}
	return len(p), nil
}

// Testability hooks for spoke client creation.
var (
	spokeClientCreatorsMu      sync.RWMutex
	newClientForClusterFunc    = k8sclients.NewClientForCluster
	newClientsetForClusterFunc = k8sclients.NewClientsetForCluster
)

// SetTestSpokeClientCreators overrides the spoke client creation functions for
// e2e tests. It returns a restore function that resets the originals.
func SetTestSpokeClientCreators(
	clientFunc func(ctx context.Context, hubClient client.Client, clusterName string) (client.Client, error),
	clientsetFunc func(ctx context.Context, hubClient client.Client, clusterName string) (kubernetes.Interface, error),
) func() {
	spokeClientCreatorsMu.Lock()
	defer spokeClientCreatorsMu.Unlock()

	origClient := newClientForClusterFunc
	origClientset := newClientsetForClusterFunc
	newClientForClusterFunc = clientFunc
	newClientsetForClusterFunc = clientsetFunc
	return func() {
		spokeClientCreatorsMu.Lock()
		defer spokeClientCreatorsMu.Unlock()
		newClientForClusterFunc = origClient
		newClientsetForClusterFunc = origClientset
	}
}

// NodeOps abstracts node-level operations against a managed cluster
// (cordon/drain/uncordon, readiness checks, MCP maxUnavailable read).
//
//go:generate mockgen -source=node_operations.go -destination=mock_node_operations_test.go -package=controller
type NodeOps interface {
	DrainNode(ctx context.Context, hostname string) error
	UncordonNode(ctx context.Context, hostname string) error
	IsNodeReady(ctx context.Context, hostname string) (bool, error)
	GetMaxUnavailable(ctx context.Context, mcpName string, totalNodes int) (int, error)
	SkipDrain() bool
}

type nodeOps struct {
	client    client.Client
	clientset kubernetes.Interface
	logger    *slog.Logger
	skipDrain bool // whether to skip drain operations (e.g. for SNO clusters)
}

// NewNodeOps creates a NodeOps backed by the given spoke cluster clients.
func NewNodeOps(client client.Client, clientset kubernetes.Interface, logger *slog.Logger, skipDrain bool) NodeOps {
	return &nodeOps{client: client, clientset: clientset, logger: logger, skipDrain: skipDrain}
}

// SkipDrain reports whether drain/uncordon operations are skipped (e.g. SNO clusters).
func (n *nodeOps) SkipDrain() bool {
	return n.skipDrain
}

// DrainNode cordons the node (marks it unschedulable) and drains all
// evictable pods using the kubelet drain package.
// It is a no-op when skipDrain is true (e.g. SNO).
func (n *nodeOps) DrainNode(ctx context.Context, hostname string) error {
	if n.skipDrain {
		return nil
	}

	node, err := n.clientset.CoreV1().Nodes().Get(ctx, hostname, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get node %s: %w", hostname, err)
	}

	drainer := &drain.Helper{
		Ctx:                 ctx,
		Client:              n.clientset,
		Force:               true,
		IgnoreAllDaemonSets: true,
		DeleteEmptyDirData:  true,
		GracePeriodSeconds:  -1,
		Timeout:             DefaultDrainTimeout,
		Out:                 logWriter{logFunc: func(msg string, _ ...any) { n.logger.InfoContext(ctx, msg) }},
		ErrOut:              logWriter{logFunc: func(msg string, _ ...any) { n.logger.ErrorContext(ctx, msg) }},
		OnPodDeletionOrEvictionFinished: func(pod *corev1.Pod, usingEviction bool, err error) {
			if err != nil {
				verb := "delete"
				if usingEviction {
					verb = "evict"
				}
				n.logger.ErrorContext(ctx, fmt.Sprintf(
					"failed to %s pod %s/%s from node %s: %v",
					verb, pod.Namespace, pod.Name, hostname, err))
				return
			}
			verb := "Deleted"
			if usingEviction {
				verb = "Evicted"
			}
			n.logger.InfoContext(ctx, fmt.Sprintf(
				"%s pod %s/%s from node %s", verb, pod.Namespace, pod.Name, hostname))
		},
	}

	if err := drain.RunCordonOrUncordon(drainer, node, true); err != nil {
		return fmt.Errorf("failed to cordon node %s: %w", hostname, err)
	}

	if err := drain.RunNodeDrain(drainer, hostname); err != nil {
		return fmt.Errorf("failed to drain node %s: %w", hostname, err)
	}
	n.logger.InfoContext(ctx, "Node drain complete", slog.String("hostname", hostname))
	return nil
}

// UncordonNode marks a node as schedulable on the managed cluster.
// It is a no-op when skipDrain is true (e.g. SNO).
func (n *nodeOps) UncordonNode(ctx context.Context, hostname string) error {
	if n.skipDrain {
		return nil
	}

	node, err := n.clientset.CoreV1().Nodes().Get(ctx, hostname, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get node %s: %w", hostname, err)
	}

	drainer := &drain.Helper{
		Ctx:    ctx,
		Client: n.clientset,
	}
	n.logger.InfoContext(ctx, "Uncordoning node", slog.String("node", hostname))
	if err := drain.RunCordonOrUncordon(drainer, node, false); err != nil {
		return fmt.Errorf("failed to uncordon node %s: %w", hostname, err)
	}
	return nil
}

// IsNodeReady checks if a K8s node is in Ready state on the spoke cluster.
func (n *nodeOps) IsNodeReady(ctx context.Context, hostname string) (bool, error) {
	k8sNode := &corev1.Node{}
	if err := n.client.Get(ctx, client.ObjectKey{Name: hostname}, k8sNode); err != nil {
		return false, fmt.Errorf("failed to get node %s: %w", hostname, err)
	}

	// The node ready condition must be present and be true.
	if !isNodeStatusConditionTrue(k8sNode.Status.Conditions, corev1.NodeReady) {
		n.logger.InfoContext(ctx, "Node is not yet ready on spoke cluster",
			slog.String("hostname", hostname))
		return false, nil
	}

	// Network unavailable condition should be absent or be false.
	if isNodeStatusConditionTrue(k8sNode.Status.Conditions, corev1.NodeNetworkUnavailable) {
		n.logger.InfoContext(ctx, "Node network is unavailable on spoke cluster",
			slog.String("hostname", hostname))
		return false, nil
	}

	return true, nil
}

// GetMaxUnavailable reads the MachineConfigPool's maxUnavailable from the managed cluster.
// The nodegroup name is used as the MCP name as each group is supposed to map a MCP.
// Default to 1 if the MCP is not found or maxUnavailable is not set.
func (n *nodeOps) GetMaxUnavailable(ctx context.Context, mcpName string, totalNodes int) (int, error) {
	if totalNodes == 1 {
		// For SNO clusters, maxUnavailable is always 1. Skip the MCP lookup.
		return DefaultMaxUnavailable, nil
	}

	mcp := &machineconfigv1.MachineConfigPool{}
	if err := n.client.Get(ctx, types.NamespacedName{Name: mcpName}, mcp); err != nil {
		if k8serrors.IsNotFound(err) {
			n.logger.WarnContext(ctx,
				fmt.Sprintf("MachineConfigPool not found, defaulting maxUnavailable to %d", DefaultMaxUnavailable),
				slog.String("mcp", mcpName))
			return DefaultMaxUnavailable, nil
		}
		return 0, fmt.Errorf("failed to get MachineConfigPool %s: %w", mcpName, err)
	}

	if mcp.Spec.MaxUnavailable == nil {
		return DefaultMaxUnavailable, nil
	}
	maxUnavailable, err := intstr.GetScaledValueFromIntOrPercent(mcp.Spec.MaxUnavailable, totalNodes, false)
	if err != nil {
		return 0, fmt.Errorf("failed to parse maxUnavailable for MCP %s: %w", mcpName, err)
	}
	if maxUnavailable < 1 {
		maxUnavailable = DefaultMaxUnavailable
	}
	return maxUnavailable, nil
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

// createSpokeClients creates both a controller-runtime client and a kubernetes clientset
// for a managed cluster. The controller-runtime client is used for MCP and node status reads,
// and the clientset is used for cordon/drain/uncordon operations.
func createSpokeClients(
	ctx context.Context,
	hubClient client.Client,
	nar *pluginsv1alpha1.NodeAllocationRequest,
) (client.Client, kubernetes.Interface, error) {
	clusterName := nar.Spec.ClusterId

	spokeClient, err := newClientForClusterFunc(ctx, hubClient, clusterName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create spoke client for cluster %s: %w", clusterName, err)
	}

	clientset, err := newClientsetForClusterFunc(ctx, hubClient, clusterName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create spoke clientset for cluster %s: %w", clusterName, err)
	}

	return spokeClient, clientset, nil
}
