/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package provisioning

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	clientutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/client/utils"
	sharedutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
)

// HTTP status messages as constants
const (
	MsgBadRequest          = "Bad Request"
	MsgUnauthorized        = "Unauthorized"
	MsgForbidden           = "Forbidden"
	MsgNotFound            = "Not Found"
	MsgInternalServerError = "Internal Server Error"
)

// HardwarePluginClient provides functions for calling the HardwarePlugin APIs
type HardwarePluginClient struct {
	client   *ClientWithResponses
	logger   *slog.Logger
	hwPlugin *hwv1alpha1.HardwarePlugin
}

// NewHardwarePluginClient creates an authenticated client connected to the HardwarePlugin server.
func NewHardwarePluginClient(
	ctx context.Context,
	c client.Client,
	logger *slog.Logger,
	hwPlugin *hwv1alpha1.HardwarePlugin,
) (*HardwarePluginClient, error) {
	// Construct OAuth client configuration
	config, err := clientutils.SetupOAuthClientConfig(ctx, c, hwPlugin)
	if err != nil {
		return nil, fmt.Errorf("failed to setup OAuth client config: %w", err)
	}

	// Build OAuth client information if type is not ServiceAccount
	cf := notifier.NewClientFactory(config, sharedutils.DefaultBackendTokenFile)
	httpClient, err := cf.NewClient(ctx, hwPlugin.Spec.AuthClientConfig.Type)
	if err != nil {
		return nil, fmt.Errorf("failed to build HTTP client: %w", err)
	}

	hClient, err := NewClientWithResponses(
		hwPlugin.Spec.ApiRoot,
		WithHTTPClient(httpClient),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create client with responses: %w", err)
	}

	return &HardwarePluginClient{
		client:   hClient,
		logger:   logger,
		hwPlugin: hwPlugin,
	}, nil
}

// GetNodeAllocationRequest retrieves a specific NodeAllocationRequest by ID
// returns: NodeAllocationRequestResponse, exists (true/false), error (if applicable)
func (h *HardwarePluginClient) GetNodeAllocationRequest(
	ctx context.Context,
	nodeAllocationRequestID string,
) (*NodeAllocationRequestResponse, bool, error) {
	response, err := h.client.GetNodeAllocationRequestWithResponse(ctx, nodeAllocationRequestID)
	if err != nil {
		h.logger.Error("Failed to get NodeAllocationRequest", slog.String("id", nodeAllocationRequestID), slog.Any("error", err))
		return nil, false, fmt.Errorf("failed to get NodeAllocationRequest '%s': %w", nodeAllocationRequestID, err)
	}

	switch response.StatusCode() {
	case http.StatusOK:
		if response.JSON200 == nil {
			h.logger.Error("Received nil JSON200 response", slog.String("id", nodeAllocationRequestID))
			return nil, true, fmt.Errorf("received nil response for NodeAllocationRequest '%s'", nodeAllocationRequestID)
		}
		return response.JSON200, true, nil
	case http.StatusNotFound:
		h.logger.Info("NodeAllocationRequest not found", slog.String("id", nodeAllocationRequestID))
		return nil, false, nil
	default:
		problem, status := h.getProblemDetails(response, response.StatusCode())
		return nil, false, h.handleErrorResponse(status, problem,
			"NodeAllocationRequest", nodeAllocationRequestID, http.MethodGet)
	}
}

// GetAllVersions retrieves all API versions
func (h *HardwarePluginClient) GetAllVersions(ctx context.Context) (*APIVersions, error) {
	response, err := h.client.GetAllVersionsWithResponse(ctx)
	if err != nil {
		h.logger.Error("Failed to get API versions", slog.Any("error", err))
		return nil, fmt.Errorf("failed to get API versions: %w", err)
	}

	switch response.StatusCode() {
	case http.StatusOK:
		if response.JSON200 == nil {
			h.logger.Error("Received nil JSON200 response for API versions")
			return nil, fmt.Errorf("received nil response for API versions")
		}
		return response.JSON200, nil
	default:
		problem, status := h.getProblemDetails(response, response.StatusCode())
		return nil, h.handleErrorResponse(status, problem,
			"APIVersions", "", http.MethodGet)
	}
}

// GetMinorVersions retrieves minor API versions
func (h *HardwarePluginClient) GetMinorVersions(ctx context.Context) (*APIVersions, error) {
	response, err := h.client.GetMinorVersionsWithResponse(ctx)
	if err != nil {
		h.logger.Error("Failed to get minor API versions", slog.Any("error", err))
		return nil, fmt.Errorf("failed to get minor API versions: %w", err)
	}

	switch response.StatusCode() {
	case http.StatusOK:
		if response.JSON200 == nil {
			h.logger.Error("Received nil JSON200 response for minor API versions")
			return nil, fmt.Errorf("received nil response for minor API versions")
		}
		return response.JSON200, nil
	default:
		problem, status := h.getProblemDetails(response, response.StatusCode())
		return nil, h.handleErrorResponse(status, problem,
			"MinorAPIVersions", "", http.MethodGet)
	}
}

// GetAllocatedNodes retrieves all AllocatedNodes
func (h *HardwarePluginClient) GetAllocatedNodes(ctx context.Context) (*[]AllocatedNode, error) {
	response, err := h.client.GetAllocatedNodesWithResponse(ctx)
	if err != nil {
		h.logger.Error("Failed to get AllocatedNodes", slog.Any("error", err))
		return nil, fmt.Errorf("failed to get AllocatedNodes: %w", err)
	}

	switch response.StatusCode() {
	case http.StatusOK:
		if response.JSON200 == nil {
			h.logger.Error("Received nil JSON200 response for AllocatedNodes")
			return nil, fmt.Errorf("received nil response for AllocatedNodes")
		}
		return response.JSON200, nil
	default:
		problem, status := h.getProblemDetails(response, response.StatusCode())
		return nil, h.handleErrorResponse(status, problem,
			"AllocatedNodes", "", http.MethodGet)
	}
}

// GetAllocatedNode retrieves a specific allocated node by ID
func (h *HardwarePluginClient) GetAllocatedNode(ctx context.Context, allocatedNodeID string) (*AllocatedNode, error) {
	response, err := h.client.GetAllocatedNodeWithResponse(ctx, allocatedNodeID)
	if err != nil {
		h.logger.Error("Failed to get allocated node", slog.String("id", allocatedNodeID), slog.Any("error", err))
		return nil, fmt.Errorf("failed to get allocated node '%s': %w", allocatedNodeID, err)
	}

	switch response.StatusCode() {
	case http.StatusOK:
		if response.JSON200 == nil {
			h.logger.Error("Received nil JSON200 response", slog.String("id", allocatedNodeID))
			return nil, fmt.Errorf("received nil response for allocated node '%s'", allocatedNodeID)
		}
		return response.JSON200, nil
	default:
		problem, status := h.getProblemDetails(response, response.StatusCode())
		return nil, h.handleErrorResponse(status, problem,
			"AllocatedNode", allocatedNodeID, http.MethodGet)
	}
}

// GetNodeAllocationRequests retrieves all NodeAllocationRequests
func (h *HardwarePluginClient) GetNodeAllocationRequests(ctx context.Context) (*[]NodeAllocationRequestResponse, error) {
	response, err := h.client.GetNodeAllocationRequestsWithResponse(ctx)
	if err != nil {
		h.logger.Error("Failed to get NodeAllocationRequests", slog.Any("error", err))
		return nil, fmt.Errorf("failed to get NodeAllocationRequests: %w", err)
	}

	switch response.StatusCode() {
	case http.StatusOK:
		if response.JSON200 == nil {
			h.logger.Error("Received nil JSON200 response for NodeAllocationRequests")
			return nil, fmt.Errorf("received nil response for NodeAllocationRequests")
		}
		return response.JSON200, nil
	default:
		problem, status := h.getProblemDetails(response, response.StatusCode())
		return nil, h.handleErrorResponse(status, problem,
			"NodeAllocationRequests", "", http.MethodGet)
	}
}

// CreateNodeAllocationRequest creates a new NodeAllocationRequest
func (h *HardwarePluginClient) CreateNodeAllocationRequest(
	ctx context.Context,
	body CreateNodeAllocationRequestJSONRequestBody,
) (string, error) {
	response, err := h.client.CreateNodeAllocationRequestWithResponse(ctx, body)
	if err != nil {
		h.logger.Error("Failed to create NodeAllocationRequest", slog.Any("error", err))
		return "", fmt.Errorf("failed to create NodeAllocationRequest: %w", err)
	}

	switch response.StatusCode() {
	case http.StatusAccepted:
		if response.JSON202 == nil {
			h.logger.Error("Received nil JSON202 response for create NodeAllocationRequest")
			return "", fmt.Errorf("received nil response for create NodeAllocationRequest")
		}
		return *response.JSON202, nil
	default:
		problem, status := h.getProblemDetails(response, response.StatusCode())
		return "", h.handleErrorResponse(status, problem,
			"NodeAllocationRequest", "", http.MethodPost)
	}
}

// DeleteNodeAllocationRequest deletes a NodeAllocationRequest by ID
func (h *HardwarePluginClient) DeleteNodeAllocationRequest(
	ctx context.Context,
	nodeAllocationRequestID string,
) (string, bool, error) {
	_, exists, err := h.GetNodeAllocationRequest(ctx, nodeAllocationRequestID)
	if err != nil {
		return "", false, fmt.Errorf("failed to get NodeAllocationRequest '%s': %w", nodeAllocationRequestID, err)
	}
	if !exists {
		return "", false, nil
	}

	response, err := h.client.DeleteNodeAllocationRequestWithResponse(ctx, nodeAllocationRequestID)
	if err != nil {
		h.logger.Error("Failed to delete NodeAllocationRequest", slog.String("id", nodeAllocationRequestID), slog.Any("error", err))
		return "", false, fmt.Errorf("failed to delete NodeAllocationRequest '%s': %w", nodeAllocationRequestID, err)
	}

	switch response.StatusCode() {
	case http.StatusAccepted:
		if response.JSON202 == nil {
			h.logger.Error("Received nil JSON202 response", slog.String("id", nodeAllocationRequestID))
			return "", false, fmt.Errorf("received nil response for delete NodeAllocationRequest '%s'", nodeAllocationRequestID)
		}
		return *response.JSON202, true, nil
	case http.StatusNotFound:
		h.logger.Info("NodeAllocationRequest not found", slog.String("id", nodeAllocationRequestID))
		return "", false, nil
	default:
		problem, status := h.getProblemDetails(response, response.StatusCode())
		return "", false, h.handleErrorResponse(status, problem,
			"NodeAllocationRequest", nodeAllocationRequestID, http.MethodDelete)
	}
}

// UpdateNodeAllocationRequest updates a NodeAllocationRequest by ID
func (h *HardwarePluginClient) UpdateNodeAllocationRequest(
	ctx context.Context,
	nodeAllocationRequestID string,
	body UpdateNodeAllocationRequestJSONRequestBody,
) (string, error) {
	response, err := h.client.UpdateNodeAllocationRequestWithResponse(ctx, nodeAllocationRequestID, body)
	if err != nil {
		h.logger.Error("Failed to update NodeAllocationRequest", slog.String("id", nodeAllocationRequestID), slog.Any("error", err))
		return "", fmt.Errorf("failed to update NodeAllocationRequest '%s': %w", nodeAllocationRequestID, err)
	}

	switch response.StatusCode() {
	case http.StatusAccepted:
		if response.JSON202 == nil {
			h.logger.Error("Received nil JSON202 response", slog.String("id", nodeAllocationRequestID))
			return "", fmt.Errorf("received nil response for update NodeAllocationRequest '%s'", nodeAllocationRequestID)
		}
		return *response.JSON202, nil
	default:
		problem, status := h.getProblemDetails(response, response.StatusCode())
		return "", h.handleErrorResponse(status, problem,
			"NodeAllocationRequest", nodeAllocationRequestID, http.MethodPut)
	}
}

// GetAllocatedNodesFromNodeAllocationRequest retrieves AllocatedNodes for a NodeAllocationRequest
func (h *HardwarePluginClient) GetAllocatedNodesFromNodeAllocationRequest(
	ctx context.Context,
	nodeAllocationRequestID string,
) (*[]AllocatedNode, error) {
	response, err := h.client.GetAllocatedNodesFromNodeAllocationRequestWithResponse(ctx, nodeAllocationRequestID)
	if err != nil {
		h.logger.Error("Failed to get AllocatedNodes from NodeAllocationRequest", slog.String("id", nodeAllocationRequestID), slog.Any("error", err))
		return nil, fmt.Errorf("failed to get AllocatedNodes from NodeAllocationRequest '%s': %w", nodeAllocationRequestID, err)
	}

	switch response.StatusCode() {
	case http.StatusOK:
		if response.JSON200 == nil {
			h.logger.Error("Received nil JSON200 response", slog.String("id", nodeAllocationRequestID))
			return nil, fmt.Errorf("received nil response for AllocatedNodes from NodeAllocationRequest '%s'", nodeAllocationRequestID)
		}
		return response.JSON200, nil
	default:
		problem, status := h.getProblemDetails(response, response.StatusCode())
		return nil, h.handleErrorResponse(status, problem,
			"AllocatedNodesFromNodeAllocationRequest", nodeAllocationRequestID, http.MethodGet)
	}
}

// GetHardwarePluginRef returns the reference (name) of the HardwarePlugin
func (h *HardwarePluginClient) GetHardwarePluginRef() string {
	return h.hwPlugin.Name
}
