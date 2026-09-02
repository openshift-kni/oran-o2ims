/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/drain"
	"sigs.k8s.io/controller-runtime/pkg/client"

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/spokeclient"
)

const (
	DefaultDrainTimeout   = 30 * time.Second
	DefaultMaxUnavailable = 1

	hwConfigMSASuffix = "-hwconfig"
	hwConfigMWSuffix  = "-hwconfig-rbac"
	scaleInMSASuffix  = "-scalein"
	scaleInMWSuffix   = "-scalein-rbac"
)

// hwConfigRBACRules is the least-privilege spoke RBAC for day2 hardware
// configuration (node readiness, cordon/drain/uncordon, MCP maxUnavailable).
var hwConfigRBACRules = []rbacv1.PolicyRule{
	{
		APIGroups: []string{""},
		Resources: []string{"nodes"},
		Verbs:     []string{"get", "list", "update", "patch"},
	},
	{
		APIGroups: []string{""},
		Resources: []string{"pods"},
		Verbs:     []string{"get", "list", "delete"},
	},
	{
		APIGroups: []string{""},
		Resources: []string{"pods/eviction"},
		Verbs:     []string{"create"},
	},
	{
		APIGroups: []string{"apps"},
		Resources: []string{"daemonsets"},
		Verbs:     []string{"get"},
	},
	{
		APIGroups: []string{"machineconfiguration.openshift.io"},
		Resources: []string{"machineconfigpools"},
		Verbs:     []string{"get"},
	},
}

// scaleInRBACRules is the least-privilege spoke RBAC for scale-in (drain plus
// Node delete).
var scaleInRBACRules = []rbacv1.PolicyRule{
	{
		APIGroups: []string{""},
		Resources: []string{"nodes"},
		Verbs:     []string{"get", "list", "update", "patch", "delete"},
	},
	{
		APIGroups: []string{""},
		Resources: []string{"pods"},
		Verbs:     []string{"get", "list", "delete"},
	},
	{
		APIGroups: []string{""},
		Resources: []string{"pods/eviction"},
		Verbs:     []string{"create"},
	},
	{
		APIGroups: []string{"apps"},
		Resources: []string{"daemonsets"},
		Verbs:     []string{"get"},
	},
}

var (
	hwConfigSpokeScheme = spokeclient.NewSpokeScheme(corev1.AddToScheme, machineconfigv1.Install)
	scaleInSpokeScheme  = spokeclient.NewSpokeScheme(corev1.AddToScheme)
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

// ensureSpokeClients returns scoped spoke clients for a managed cluster.
// ready=false means the MSA token or ManifestWork is not available yet.
func ensureSpokeClients(
	ctx context.Context,
	hubClient client.Client,
	logger *slog.Logger,
	nar *hwmgmtv1alpha1.NodeAllocationRequest,
	msaName, mwName string,
	rules []rbacv1.PolicyRule,
	spokeScheme *runtime.Scheme,
) (spokeclient.Clients, bool, error) {
	clusterName, err := getClusterNameFromPR(ctx, hubClient, nar.Name)
	if err != nil {
		return spokeclient.Clients{}, false, fmt.Errorf("failed to resolve cluster name for NAR %s: %w", nar.Name, err)
	}

	clients, ready, err := spokeclient.EnsureSpokeClient(
		ctx, hubClient, logger, clusterName, msaName, mwName, rules, spokeScheme, spokeclient.WithClientset)
	if err != nil {
		return spokeclient.Clients{}, false, fmt.Errorf("failed to ensure scoped spoke clients: %w", err)
	}
	if !ready {
		return spokeclient.Clients{}, false, nil
	}
	return clients, true, nil
}

func cleanupSpokeAccess(
	ctx context.Context,
	hubClient client.Client,
	logger *slog.Logger,
	nar *hwmgmtv1alpha1.NodeAllocationRequest,
	msaName, mwName string,
) error {
	clusterName, err := getClusterNameFromPR(ctx, hubClient, nar.Name)
	if err != nil {
		if k8serrors.IsNotFound(err) || errors.Is(err, errMissingClusterDetails) {
			logger.InfoContext(ctx, "Skipping spoke access cleanup; cluster name could not be resolved",
				slog.String("nar", nar.Name), slog.Any("error", err))
			return nil
		}
		return fmt.Errorf("failed to get cluster name for NodeAllocationRequest %s: %w", nar.Name, err)
	}
	if err := spokeclient.CleanupSpokeAccess(ctx, hubClient, clusterName, msaName, mwName); err != nil {
		return fmt.Errorf("failed to clean up spoke access %s: %w", msaName, err)
	}
	return nil
}

// errMissingClusterDetails is returned when the ProvisioningRequest exists but
// has no cluster name in ClusterDetails.
var errMissingClusterDetails = errors.New("provisioningRequest has no cluster name in ClusterDetails")

// getClusterNameFromPR looks up the ProvisioningRequest (1:1 with the NAR by name)
// and returns the actual cluster name from ClusterDetails. The cluster name is the
// namespace where hub MSA/ManifestWork objects live, which may differ from
// nar.Spec.ClusterId.
func getClusterNameFromPR(ctx context.Context, hubClient client.Client, narName string) (string, error) {
	pr := &provisioningv1alpha1.ProvisioningRequest{}
	if err := hubClient.Get(ctx, client.ObjectKey{Name: narName}, pr); err != nil {
		return "", fmt.Errorf("failed to get ProvisioningRequest %s: %w", narName, err)
	}

	if pr.Status.Extensions.ClusterDetails == nil {
		return "", fmt.Errorf("%w: provisioningRequest %s has no ClusterDetails in status", errMissingClusterDetails, narName)
	}

	if pr.Status.Extensions.ClusterDetails.Name == "" {
		return "", fmt.Errorf("%w: provisioningRequest %s has no cluster name in ClusterDetails", errMissingClusterDetails, narName)
	}

	return pr.Status.Extensions.ClusterDetails.Name, nil
}
