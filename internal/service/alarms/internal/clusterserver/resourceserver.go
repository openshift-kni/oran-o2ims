package clusterserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/oapi-codegen/oapi-codegen/v2/pkg/securityprovider"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/clusterserver/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients"
)

const (
	clusterServerURLEnvName = "CLUSTER_SERVER_URL"
	tokenPathEnvName        = "TOKEN_PATH"
)

type NodeCluster = generated.NodeCluster
type NodeClusterType = generated.NodeClusterType

type ClusterServer struct {
	client           *generated.ClientWithResponses
	NodeClusters     *[]NodeCluster
	NodeClusterTypes *[]NodeClusterType
}

// New creates a new cluster server object
func New() (*ClusterServer, error) {
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
		return nil, fmt.Errorf("failed to create http transport: %w", err)
	}

	hc := http.Client{Transport: tr}

	tokenPath := utils.DefaultBackendTokenFile

	// Use for local development
	path := os.Getenv(tokenPathEnvName)
	if path != "" {
		tokenPath = path
	}

	// Read token
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read token file: %w", err)
	}

	// Create Bearer token
	token, err := securityprovider.NewSecurityProviderBearerToken(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to create Bearer token: %w", err)
	}

	c, err := generated.NewClientWithResponses(url, generated.WithHTTPClient(&hc), generated.WithRequestEditorFn(token.Intercept))
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	return &ClusterServer{client: c}, nil
}

// GetAll fetches all necessary data from the cluster server
func (r *ClusterServer) GetAll(ctx context.Context) error {
	slog.Info("Getting all objects from the cluster server")

	// List node clusters
	nodeClusters, err := r.GetNodeClusters(ctx)
	if err != nil {
		return fmt.Errorf("failed to get node clusters: %w", err)
	}

	// List node cluster types
	nodeClusterTypes, err := r.GetNodeClusterTypes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get node cluster types: %w", err)
	}

	r.NodeClusters = nodeClusters
	r.NodeClusterTypes = nodeClusterTypes

	return nil
}

// GetNodeClusters lists all node clusters
func (r *ClusterServer) GetNodeClusters(ctx context.Context) (*[]NodeCluster, error) {
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

// GetNodeClusterTypes lists all node cluster types
func (r *ClusterServer) GetNodeClusterTypes(ctx context.Context) (*[]NodeClusterType, error) {
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
