package clusterserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"

	"github.com/google/uuid"
	"k8s.io/client-go/transport"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/infrastructure/clusterserver/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients"
)

const (
	Name = "Cluster"

	clusterServerURLEnvName = "CLUSTER_SERVER_URL"
	tokenPathEnvName        = "TOKEN_PATH"
)

type NodeCluster = generated.NodeCluster
type NodeClusterType = generated.NodeClusterType

type ClusterServer struct {
	client                    *generated.ClientWithResponses
	nodeClusters              *[]NodeCluster
	nodeClusterTypes          *[]NodeClusterType
	clusterIDToResourceTypeID map[uuid.UUID]uuid.UUID

	sync.Mutex
}

// Name returns the name of the client
func (r *ClusterServer) Name() string {
	return Name
}

// Setup setups a new client for the cluster server
func (r *ClusterServer) Setup() error {
	slog.Info("Creating ClusterServer client")

	url := utils.GetServiceURL(utils.InventoryClusterServerName)

	// Use for local development
	clusterServerURL := os.Getenv(clusterServerURLEnvName)
	if clusterServerURL != "" {
		url = clusterServerURL
	}

	// Set up transport
	tr, err := utils.GetDefaultBackendTransport()
	if err != nil {
		return fmt.Errorf("failed to create http transport: %w", err)
	}

	hc := http.Client{Transport: tr}

	tokenPath := utils.DefaultBackendTokenFile

	// Use for local development
	path := os.Getenv(tokenPathEnvName)
	if path != "" {
		tokenPath = path
	}

	// Create a request editor that uses a cached token source capable of re-reading from file to pickup changes
	// as our token is renewed.
	editor := clients.AuthorizationEditor{
		Source: transport.NewCachedFileTokenSource(tokenPath),
	}
	c, err := generated.NewClientWithResponses(url, generated.WithHTTPClient(&hc), generated.WithRequestEditorFn(editor.Editor))
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	r.client = c
	return nil
}

// FetchAll fetches all necessary data from the cluster server
func (r *ClusterServer) FetchAll(ctx context.Context) error {
	slog.Info("Getting all objects from the cluster server")

	// List node clusters
	nodeClusters, err := r.getNodeClusters(ctx)
	if err != nil {
		return fmt.Errorf("failed to get node clusters: %w", err)
	}
	if nodeClusters == nil {
		return fmt.Errorf("no node clusters found: %w", err)
	}

	// List node cluster types
	nodeClusterTypes, err := r.getNodeClusterTypes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get node cluster types: %w", err)
	}
	if nodeClusterTypes == nil {
		return fmt.Errorf("no node cluster types found: %w", err)
	}

	r.Lock()
	defer r.Unlock()

	r.nodeClusters = nodeClusters
	r.nodeClusterTypes = nodeClusterTypes

	r.clusterResourceTypeMapping()

	return nil
}

// getNodeClusters lists all node clusters
func (r *ClusterServer) getNodeClusters(ctx context.Context) (*[]NodeCluster, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, clients.ListRequestTimeout)
	defer cancel()

	resp, err := r.client.GetNodeClustersWithResponse(ctxWithTimeout, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to execute Get operation: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("status code different from 200 OK: %s", resp.Status())
	}

	slog.Info("Got node clusters", "count", len(*resp.JSON200))

	return resp.JSON200, nil
}

// getNodeClusterTypes lists all node cluster types
func (r *ClusterServer) getNodeClusterTypes(ctx context.Context) (*[]NodeClusterType, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, clients.ListRequestTimeout)
	defer cancel()

	resp, err := r.client.GetNodeClusterTypesWithResponse(ctxWithTimeout, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to execute Get operation: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("status code different from 200 OK: %s", resp.Status())
	}

	slog.Info("Got node cluster types", "count", len(*resp.JSON200))

	return resp.JSON200, nil
}

// clusterResourceTypeMapping map cluster ID with objectType ID for faster lookup during Caas alerts
func (r *ClusterServer) clusterResourceTypeMapping() {
	mapping := make(map[uuid.UUID]uuid.UUID)
	for _, cluster := range *r.nodeClusters {
		mapping[cluster.NodeClusterId] = cluster.NodeClusterTypeId
		slog.Info("Mapping cluster ID to resource type ID", "ClusterID", cluster.NodeClusterId, "NodeClusterTypeId", cluster.NodeClusterTypeId)
	}

	r.clusterIDToResourceTypeID = mapping
}

// GetNodeClusterTypes returns a copy of the node cluster types
func (r *ClusterServer) GetNodeClusterTypes() []NodeClusterType {
	r.Lock()
	defer r.Unlock()

	if r.nodeClusterTypes == nil {
		return nil
	}

	nodeClusterTypesCopy := make([]NodeClusterType, len(*r.nodeClusterTypes))
	copy(nodeClusterTypesCopy, *r.nodeClusterTypes)

	return nodeClusterTypesCopy
}

// GetClusterIDToResourceTypeID returns a copy of the cluster ID to resource type ID mapping
func (r *ClusterServer) GetClusterIDToResourceTypeID() map[uuid.UUID]uuid.UUID {
	r.Lock()
	defer r.Unlock()

	c := make(map[uuid.UUID]uuid.UUID)
	for k, v := range r.clusterIDToResourceTypeID {
		c[k] = v
	}

	return c
}
