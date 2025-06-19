/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	generated "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/generated/server"
	hwpluginutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	sharedutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

// HardwarePluginServer implements StricerServerInterface.
// This ensures that we've conformed to the `StrictServerInterface` with a compile-time check.
var _ generated.StrictServerInterface = (*HardwarePluginServer)(nil)

type HardwarePluginServer struct {
	utils.CommonServerConfig
	HubClient        client.Client
	Logger           *slog.Logger
	Namespace        string
	HardwarePluginID string
	ResourcePrefix   string
}

var baseURL = "/hardware-manager/provisioning/v1"
var currentVerion = "1.0.0"

// GetAllVersions handles an API request to fetch all versions
func (h *HardwarePluginServer) GetAllVersions(_ context.Context, _ generated.GetAllVersionsRequestObject,
) (generated.GetAllVersionsResponseObject, error) {
	// We currently only support a single version
	versions := []generated.APIVersion{
		{
			Version: &currentVerion,
		},
	}
	return generated.GetAllVersions200JSONResponse(generated.APIVersions{
		ApiVersions: &versions,
		UriPrefix:   &baseURL,
	}), nil
}

// GetMinorVersions handles an API request to fetch minor versions
func (h *HardwarePluginServer) GetMinorVersions(_ context.Context, _ generated.GetMinorVersionsRequestObject,
) (generated.GetMinorVersionsResponseObject, error) {
	// We currently support a single version
	versions := []generated.APIVersion{
		{
			Version: &currentVerion,
		},
	}
	return generated.GetMinorVersions200JSONResponse(generated.APIVersions{
		ApiVersions: &versions,
		UriPrefix:   &baseURL,
	}), nil
}

func (h *HardwarePluginServer) GetNodeAllocationRequests(
	ctx context.Context,
	request generated.GetNodeAllocationRequestsRequestObject,
) (generated.GetNodeAllocationRequestsResponseObject, error) {

	// List NodeAllocationRequests with the HardwarePlugin label
	nodeAllocationRequestList := &hwv1alpha1.NodeAllocationRequestList{}
	listOptions := client.MatchingLabels{
		hwpluginutils.HardwarePluginLabel: h.HardwarePluginID,
	}
	if err := h.HubClient.List(ctx, nodeAllocationRequestList, &listOptions); err != nil {
		return nil, fmt.Errorf("failed to list all NodeAllocationRequests: %w", err)
	}

	nodeAllocationRequestResponse := []generated.NodeAllocationRequestResponse{}
	for _, nodeAllocationRequest := range nodeAllocationRequestList.Items {
		// Convert NodeAllocationRequest CR to NodeAllocationRequestResponse object
		resp, err := NodeAllocationRequestCRToResponseObject(&nodeAllocationRequest)
		if err != nil {
			return nil, fmt.Errorf("failed to generate a response object from NodeAllocationRequest CR: %w", err)
		}

		nodeAllocationRequestResponse = append(nodeAllocationRequestResponse, resp)
	}

	return generated.GetNodeAllocationRequests200JSONResponse(nodeAllocationRequestResponse), nil
}

func (h *HardwarePluginServer) GetNodeAllocationRequest(
	ctx context.Context,
	request generated.GetNodeAllocationRequestRequestObject,
) (generated.GetNodeAllocationRequestResponseObject, error) {

	nodeAllocationRequest, err := GetNodeAllocationRequest(ctx, h.HubClient, h.Namespace, request.NodeAllocationRequestId)
	if errors.IsNotFound(err) {
		return generated.GetNodeAllocationRequest404ApplicationProblemPlusJSONResponse(generated.ProblemDetails{
			Detail: fmt.Sprintf("could not find NodeAllocationRequest '%s', err: %s", request.NodeAllocationRequestId, err.Error()),
			Status: http.StatusNotFound,
		}), nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get NodeAllocationRequest '%s', err: %w", request.NodeAllocationRequestId, err)
	}

	// Convert NodeAllocationRequest CR to NodeAllocationRequestResponse object
	nodeAllocationRequestResponse, err := NodeAllocationRequestCRToResponseObject(nodeAllocationRequest)

	if err != nil {
		return nil, fmt.Errorf("failed to parse convert NodeAllocationRequest CR to object, err: %w", err)
	}

	return generated.GetNodeAllocationRequest200JSONResponse(nodeAllocationRequestResponse), nil
}

// CreateNodeAllocationRequest creates a NodeAllocationRequest object
func (h *HardwarePluginServer) CreateNodeAllocationRequest(
	ctx context.Context,
	request generated.CreateNodeAllocationRequestRequestObject,
) (generated.CreateNodeAllocationRequestResponseObject, error) {

	// construct nodeGroups object
	nodeGroups := []hwv1alpha1.NodeGroup{}
	for _, group := range request.Body.NodeGroup {
		nodeGroups = append(nodeGroups, hwv1alpha1.NodeGroup{
			Size: group.NodeGroupData.Size,
			NodeGroupData: hwv1alpha1.NodeGroupData{
				Name:             group.NodeGroupData.Name,
				Role:             group.NodeGroupData.Role,
				HwProfile:        group.NodeGroupData.HwProfile,
				ResourcePoolId:   group.NodeGroupData.ResourceGroupId,
				ResourceSelector: group.NodeGroupData.ResourceSelector,
			},
		})
	}

	// Construct NodeAllocationRequest resource

	nodeAllocationRequestID, err := GenerateResourceIdentifier(h.ResourcePrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to generate unique NodeAllocationRequest identifier, err: %w", err)
	}

	nodeAllocationRequest := &hwv1alpha1.NodeAllocationRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodeAllocationRequestID,
			Namespace: h.Namespace,
			Labels: map[string]string{
				hwpluginutils.HardwarePluginLabel: h.HardwarePluginID,
			},
		},
		Spec: hwv1alpha1.NodeAllocationRequestSpec{
			// CloudID:      request.Body.CloudID,
			HwMgrId:      h.HardwarePluginID,
			NodeGroup:    nodeGroups,
			LocationSpec: hwv1alpha1.LocationSpec{Site: request.Body.Site},
		},
	}

	if err := CreateOrUpdateNodeAllocationRequest(ctx, h.HubClient, h.Logger, nodeAllocationRequest); err != nil {
		return nil, fmt.Errorf("failed to create NodeAllocationRequest resource, err: %w", err)
	}

	return generated.CreateNodeAllocationRequest202JSONResponse(nodeAllocationRequestID), nil
}

func (h *HardwarePluginServer) UpdateNodeAllocationRequest(
	ctx context.Context,
	request generated.UpdateNodeAllocationRequestRequestObject,
) (generated.UpdateNodeAllocationRequestResponseObject, error) {

	// Check that NodeAllocationRequest object exists
	existingNodeAllocationRequest := &hwv1alpha1.NodeAllocationRequest{}
	exist, err := sharedutils.DoesK8SResourceExist(ctx, h.HubClient,
		request.NodeAllocationRequestId, h.Namespace, existingNodeAllocationRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to get NodeAllocationRequest %s, err: %w", request.NodeAllocationRequestId, err)
	}
	if !exist {
		return generated.UpdateNodeAllocationRequest404ApplicationProblemPlusJSONResponse(
			generated.ProblemDetails{
				Detail: fmt.Sprintf("could not find NodeAllocationRequest '%s'", request.NodeAllocationRequestId),
				Status: http.StatusNotFound,
			}), nil
	}

	// Construct nodeGroups object
	nodeGroups := []hwv1alpha1.NodeGroup{}
	for _, ng := range request.Body.NodeGroup {
		nodeGroups = append(nodeGroups, hwv1alpha1.NodeGroup{
			Size: ng.NodeGroupData.Size,
			NodeGroupData: hwv1alpha1.NodeGroupData{
				Name:             ng.NodeGroupData.Name,
				Role:             ng.NodeGroupData.Role,
				HwProfile:        ng.NodeGroupData.HwProfile,
				ResourcePoolId:   ng.NodeGroupData.ResourceGroupId,
				ResourceSelector: ng.NodeGroupData.ResourceSelector,
			},
		})
	}

	// construct NodeAllocationRequest resource
	nodeAllocationRequest := &hwv1alpha1.NodeAllocationRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      existingNodeAllocationRequest.Name,
			Namespace: existingNodeAllocationRequest.Namespace,
		},
		Spec: hwv1alpha1.NodeAllocationRequestSpec{
			// CloudID:      request.Body.CloudID,
			HwMgrId:      existingNodeAllocationRequest.Spec.HwMgrId,
			NodeGroup:    nodeGroups,
			LocationSpec: hwv1alpha1.LocationSpec{Site: request.Body.Site},
		},
	}

	if err := CreateOrUpdateNodeAllocationRequest(ctx, h.HubClient, h.Logger, nodeAllocationRequest); err != nil {
		return nil, fmt.Errorf("failed to update NodeAllocationRequest resource '%s', err: %w", request.NodeAllocationRequestId, err)
	}

	return generated.UpdateNodeAllocationRequest202JSONResponse(request.NodeAllocationRequestId), nil
}

func (h *HardwarePluginServer) DeleteNodeAllocationRequest(
	ctx context.Context,
	request generated.DeleteNodeAllocationRequestRequestObject,
) (generated.DeleteNodeAllocationRequestResponseObject, error) {

	// Check that NodeAllocationRequest object exists
	existingNodeAllocationRequest := &hwv1alpha1.NodeAllocationRequest{}
	exist, err := sharedutils.DoesK8SResourceExist(ctx, h.HubClient, request.NodeAllocationRequestId, h.Namespace, existingNodeAllocationRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to get NodeAllocationRequest '%s', err: %w", request.NodeAllocationRequestId, err)
	}
	if !exist {
		return generated.DeleteNodeAllocationRequest404ApplicationProblemPlusJSONResponse(
			generated.ProblemDetails{
				Detail: fmt.Sprintf("could not find NodeAllocationRequest '%s'", request.NodeAllocationRequestId),
				Status: http.StatusNotFound,
			}), nil
	}

	// Delete the NodeAllocationRequest resource
	if err := h.HubClient.Delete(ctx, existingNodeAllocationRequest); err != nil {
		return nil, fmt.Errorf("failed to delete NodeAllocationRequest '%s', err: %w", request.NodeAllocationRequestId, err)
	}

	return generated.DeleteNodeAllocationRequest202JSONResponse(request.NodeAllocationRequestId), nil
}

func (h *HardwarePluginServer) GetAllocatedNodes(
	ctx context.Context,
	request generated.GetAllocatedNodesRequestObject,
) (generated.GetAllocatedNodesResponseObject, error) {

	// List AllocatedNodes with the HardwarePlugin label
	allocatedNodeList := &hwv1alpha1.AllocatedNodeList{}
	listOptions := client.MatchingLabels{
		hwpluginutils.HardwarePluginLabel: h.HardwarePluginID,
	}

	if err := h.HubClient.List(ctx, allocatedNodeList, &listOptions); err != nil {
		return nil, fmt.Errorf("failed to get AllocatedNodes, err: %w", err)
	}

	allocatedNodeObjects := []generated.AllocatedNode{}
	for _, node := range allocatedNodeList.Items {
		// Convert AllocatedNode CR to AllocatedNode object
		allocatedNodeObject, err := AllocatedNodeCRToAllocatedNodeObject(&node)
		if err != nil {
			return nil, fmt.Errorf("encountered an error converting AllocatedNode resource to response object, err: %w", err)
		}
		allocatedNodeObjects = append(allocatedNodeObjects, allocatedNodeObject)
	}

	return generated.GetAllocatedNodes200JSONResponse(allocatedNodeObjects), nil
}

func (h *HardwarePluginServer) GetAllocatedNode(
	ctx context.Context,
	request generated.GetAllocatedNodeRequestObject,
) (generated.GetAllocatedNodeResponseObject, error) {

	allocatedNode := &hwv1alpha1.AllocatedNode{}
	if err := h.HubClient.Get(ctx, types.NamespacedName{Namespace: h.Namespace, Name: request.AllocatedNodeId},
		allocatedNode,
	); err != nil {
		if errors.IsNotFound(err) {
			return generated.GetAllocatedNode404ApplicationProblemPlusJSONResponse(
				generated.ProblemDetails{
					Detail: fmt.Sprintf("could not find AllocatedNode '%s', err: %s", request.AllocatedNodeId, err.Error()),
					Status: http.StatusNotFound,
				},
			), nil
		}

		return generated.GetAllocatedNode500ApplicationProblemPlusJSONResponse(
			generated.ProblemDetails{
				Detail: fmt.Sprintf("failed to get AllocatedNode '%s', err: %s", request.AllocatedNodeId, err.Error()),
				Status: http.StatusInternalServerError,
			},
		), nil
	}

	// Convert AllocatedNode CR to AllocatedNode response object
	allocatedNodeObject, err := AllocatedNodeCRToAllocatedNodeObject(allocatedNode)
	if err != nil {
		return nil, fmt.Errorf("encountered an error converting AllocatedNode resource to response object, err: %w", err)
	}

	return generated.GetAllocatedNode200JSONResponse(allocatedNodeObject), nil
}

func (h *HardwarePluginServer) GetAllocatedNodesFromNodeAllocationRequest(
	ctx context.Context,
	request generated.GetAllocatedNodesFromNodeAllocationRequestRequestObject,
) (generated.GetAllocatedNodesFromNodeAllocationRequestResponseObject, error) {

	nodeAllocationRequest, err := GetNodeAllocationRequest(ctx, h.HubClient, h.Namespace, request.NodeAllocationRequestId)
	if err != nil {
		return nil, fmt.Errorf("failed to get AllocatedNodes from NodeAllocationRequest '%s', err: %w", request.NodeAllocationRequestId, err)
	}

	allocatedNodeList := nodeAllocationRequest.Status.Properties.NodeNames
	allocatedNodeObjects := []generated.AllocatedNode{}

	// Get the AllocatedNodes corresponding to the NodeAllocationRequest.
	for _, nodeId := range allocatedNodeList {
		allocatedNode, err := GetAllocatedNode(ctx, h.HubClient, h.Namespace, nodeId)
		if err != nil {
			return nil, fmt.Errorf("failed to get AllocatedNode '%s' from NodeAllocationRequest '%s', err: %w", nodeId, request.NodeAllocationRequestId, err)
		}

		// convert AllocatedNode CR -> object
		allocatedNodeObject, err := AllocatedNodeCRToAllocatedNodeObject(allocatedNode)
		if err != nil {
			return nil, fmt.Errorf("encountered an error converting AllocatedNode resource '%s' to response object, err: %w", nodeId, err)
		}

		allocatedNodeObjects = append(allocatedNodeObjects, allocatedNodeObject)
	}

	return generated.GetAllocatedNodesFromNodeAllocationRequest200JSONResponse(allocatedNodeObjects), nil
}
