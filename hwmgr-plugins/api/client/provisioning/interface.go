/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package provisioning

import (
	"context"
)

//go:generate mockgen -source=interface.go -destination=mocks/mock_interface.go -package=mocks

// HardwarePluginClientInterface defines the interface for hardware plugin client operations
// This interface includes only the methods actually used by the provisioning request controller
type HardwarePluginClientInterface interface {
	// GetNodeAllocationRequest retrieves a specific NodeAllocationRequest by ID
	// returns: NodeAllocationRequestResponse, exists (true/false), error (if applicable)
	GetNodeAllocationRequest(ctx context.Context, nodeAllocationRequestID string) (*NodeAllocationRequestResponse, bool, error)

	// DeleteNodeAllocationRequest deletes a specific NodeAllocationRequest by ID
	DeleteNodeAllocationRequest(ctx context.Context, nodeAllocationRequestID string) (string, bool, error)

	// GetAllocatedNodesFromNodeAllocationRequest retrieves allocated nodes from a specific NodeAllocationRequest
	GetAllocatedNodesFromNodeAllocationRequest(ctx context.Context, nodeAllocationRequestID string) (*[]AllocatedNode, error)

	// GetHardwarePluginRef returns a reference to the hardware plugin
	GetHardwarePluginRef() string

	// UpdateNodeAllocationRequest updates a specific NodeAllocationRequest
	UpdateNodeAllocationRequest(ctx context.Context, nodeAllocationRequestID string, nodeAllocationRequest NodeAllocationRequest) (string, error)

	// CreateNodeAllocationRequest creates a new NodeAllocationRequest
	CreateNodeAllocationRequest(ctx context.Context, nodeAllocationRequest NodeAllocationRequest) (string, error)
}

// HardwarePluginClientAdapter adapts the concrete HardwarePluginClient to implement HardwarePluginClientInterface
type HardwarePluginClientAdapter struct {
	client *HardwarePluginClient
}

// NewHardwarePluginClientAdapter creates a new adapter for the hardware plugin client
func NewHardwarePluginClientAdapter(client *HardwarePluginClient) HardwarePluginClientInterface {
	return &HardwarePluginClientAdapter{client: client}
}

// GetNodeAllocationRequest implements HardwarePluginClientInterface
func (a *HardwarePluginClientAdapter) GetNodeAllocationRequest(ctx context.Context, nodeAllocationRequestID string) (*NodeAllocationRequestResponse, bool, error) {
	return a.client.GetNodeAllocationRequest(ctx, nodeAllocationRequestID)
}

// DeleteNodeAllocationRequest implements HardwarePluginClientInterface
func (a *HardwarePluginClientAdapter) DeleteNodeAllocationRequest(ctx context.Context, nodeAllocationRequestID string) (string, bool, error) {
	return a.client.DeleteNodeAllocationRequest(ctx, nodeAllocationRequestID)
}

// GetAllocatedNodesFromNodeAllocationRequest implements HardwarePluginClientInterface
func (a *HardwarePluginClientAdapter) GetAllocatedNodesFromNodeAllocationRequest(ctx context.Context, nodeAllocationRequestID string) (*[]AllocatedNode, error) {
	return a.client.GetAllocatedNodesFromNodeAllocationRequest(ctx, nodeAllocationRequestID)
}

// GetHardwarePluginRef implements HardwarePluginClientInterface
func (a *HardwarePluginClientAdapter) GetHardwarePluginRef() string {
	return a.client.GetHardwarePluginRef()
}

// UpdateNodeAllocationRequest implements HardwarePluginClientInterface
func (a *HardwarePluginClientAdapter) UpdateNodeAllocationRequest(ctx context.Context, nodeAllocationRequestID string, nodeAllocationRequest NodeAllocationRequest) (string, error) {
	return a.client.UpdateNodeAllocationRequest(ctx, nodeAllocationRequestID, nodeAllocationRequest)
}

// CreateNodeAllocationRequest implements HardwarePluginClientInterface
func (a *HardwarePluginClientAdapter) CreateNodeAllocationRequest(ctx context.Context, nodeAllocationRequest NodeAllocationRequest) (string, error) {
	return a.client.CreateNodeAllocationRequest(ctx, nodeAllocationRequest)
}
