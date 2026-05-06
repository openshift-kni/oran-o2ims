/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonapi "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	api "github.com/openshift-kni/oran-o2ims/internal/service/provisioning/api/generated"
)

type ProvisioningServer struct {
	HubClient client.Client
}

type ProvisioningServerConfig struct {
	svcutils.CommonServerConfig
}

// ProvisioningServer implements StrictServerInterface. This ensures that we've conformed to the `StrictServerInterface` with a compile-time check
var _ api.StrictServerInterface = (*ProvisioningServer)(nil)

// baseURL is the prefix for all of our supported API endpoints
var baseURL = constants.O2IMSProvisioningBaseURL
var currentVersion = "1.2.0"

// GetAllVersions handles an API request to fetch all versions
func (r *ProvisioningServer) GetAllVersions(ctx context.Context, request api.GetAllVersionsRequestObject) (api.GetAllVersionsResponseObject, error) {
	// We currently only support a single version
	versions := []common.APIVersion{
		{
			Version: &currentVersion,
		},
	}
	return api.GetAllVersions200JSONResponse(common.APIVersions{
		ApiVersions: &versions,
		UriPrefix:   &baseURL,
	}), nil
}

// GetMinorVersions handles an API request to fetch minor versions
func (r *ProvisioningServer) GetMinorVersions(ctx context.Context, request api.GetMinorVersionsRequestObject) (api.GetMinorVersionsResponseObject, error) {
	// We currently only support a single version
	versions := []common.APIVersion{
		{
			Version: &currentVersion,
		},
	}
	return api.GetMinorVersions200JSONResponse(common.APIVersions{
		ApiVersions: &versions,
		UriPrefix:   &baseURL,
	}), nil
}

// GetProvisioningRequests handles an API request to fetch provisioning requests
func (r *ProvisioningServer) GetProvisioningRequests(ctx context.Context, request api.GetProvisioningRequestsRequestObject) (api.GetProvisioningRequestsResponseObject, error) {
	options := commonapi.NewFieldOptions(request.Params.AllFields, request.Params.Fields, request.Params.ExcludeFields)
	if err := options.Validate(api.ProvisioningRequestInfo{}); err != nil {
		return api.GetProvisioningRequests400ApplicationProblemPlusJSONResponse{
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}, nil
	}

	provisioningRequests := provisioningv1alpha1.ProvisioningRequestList{}
	err := r.HubClient.List(ctx, &provisioningRequests)
	if err != nil {
		return nil, fmt.Errorf("failed to get ProvisioningRequests: %w", err)
	}
	objects := make([]api.ProvisioningRequestInfo, 0, len(provisioningRequests.Items))
	for _, provisioningRequest := range provisioningRequests.Items {
		// Convert the ProvisioningRequest's name to uuid
		// TODO: Check name is a valid uuid in the validation webhook
		provisioningRequestId, err := uuid.Parse(provisioningRequest.Name)
		if err != nil {
			return nil, fmt.Errorf("could not convert ProvisioningRequest name (%s) to uuid: %w",
				provisioningRequest.Name, err)
		}

		object, err := convertProvisioningRequestCRToApi(provisioningRequestId, provisioningRequest)
		if err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}

	return api.GetProvisioningRequests200JSONResponse(objects), nil
}

// GetProvisioningRequest handles an API request to retrieve a provisioning request
func (r *ProvisioningServer) GetProvisioningRequest(ctx context.Context, request api.GetProvisioningRequestRequestObject) (api.GetProvisioningRequestResponseObject, error) {
	provisioningRequest := provisioningv1alpha1.ProvisioningRequest{}
	err := r.HubClient.Get(ctx, types.NamespacedName{Name: request.ProvisioningRequestId.String()}, &provisioningRequest)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return api.GetProvisioningRequest404ApplicationProblemPlusJSONResponse(common.ProblemDetails{
				AdditionalAttributes: &map[string]string{
					"provisioningRequestId": request.ProvisioningRequestId.String(),
				},
				Detail: "requested ProvisioningRequest not found",
				Status: http.StatusNotFound,
			}), nil
		}
		return nil, fmt.Errorf("failed to get ProvisioningRequest: %w", err)
	}

	object, err := convertProvisioningRequestCRToApi(request.ProvisioningRequestId, provisioningRequest)
	if err != nil {
		return nil, err
	}

	return api.GetProvisioningRequest200JSONResponse(object), nil
}

// CreateProvisioningRequest handles an API request to create provisioning requests
func (r *ProvisioningServer) CreateProvisioningRequest(ctx context.Context, request api.CreateProvisioningRequestRequestObject) (api.CreateProvisioningRequestResponseObject, error) {
	provisioningRequest, err := convertProvisioningRequestApiToCR(*request.Body)
	if err != nil {
		return nil, err
	}

	err = r.HubClient.Create(ctx, provisioningRequest)
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			return api.CreateProvisioningRequest409ApplicationProblemPlusJSONResponse(common.ProblemDetails{
				AdditionalAttributes: &map[string]string{
					"provisioningRequestId": request.Body.ProvisioningRequestId.String(),
				},
				Detail: "requested ProvisioningRequest already exists",
				Status: http.StatusConflict,
			}), nil
		}
		// API server and webhook validation errors
		if k8serrors.IsForbidden(err) || k8serrors.IsBadRequest(err) || k8serrors.IsInvalid(err) {
			return api.CreateProvisioningRequest400ApplicationProblemPlusJSONResponse(common.ProblemDetails{
				AdditionalAttributes: &map[string]string{
					"provisioningRequestId": request.Body.ProvisioningRequestId.String(),
				},
				Detail: err.Error(),
				Status: http.StatusBadRequest,
			}), nil
		}
		return nil, fmt.Errorf("failed to create ProvisioningRequest (%s): %w", request.Body.ProvisioningRequestId.String(), err) // 500 error
	}

	// Query the created ProvisioningRequest to get the latest status and convert to API provisioningRequestInfo
	createdProvisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}
	err = r.HubClient.Get(ctx, types.NamespacedName{Name: provisioningRequest.Name}, createdProvisioningRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to get ProvisioningRequest (%s): %w", provisioningRequest.Name, err)
	}
	provisioningRequestInfo, err := convertProvisioningRequestCRToApi(request.Body.ProvisioningRequestId, *createdProvisioningRequest)
	if err != nil {
		return nil, err
	}

	slog.Info("Created ProvisioningRequest", "provisioningRequestId", request.Body.ProvisioningRequestId.String())
	location := fmt.Sprintf("%s/provisioningRequests/%s", constants.O2IMSProvisioningBaseURL, request.Body.ProvisioningRequestId)
	return api.CreateProvisioningRequest201JSONResponse{
		Body: provisioningRequestInfo,
		Headers: api.CreateProvisioningRequest201ResponseHeaders{
			Location: location,
		},
	}, nil
}

// UpdateProvisioningRequest handles an API request to update a provisioning request
func (r *ProvisioningServer) UpdateProvisioningRequest(ctx context.Context, request api.UpdateProvisioningRequestRequestObject) (api.UpdateProvisioningRequestResponseObject, error) {
	if request.Body.ProvisioningRequestId.String() != request.ProvisioningRequestId.String() {
		return api.UpdateProvisioningRequest422ApplicationProblemPlusJSONResponse(common.ProblemDetails{
			Detail: "the provisioningRequestId in the request body must match the provisioningRequestId in the request path",
			Status: http.StatusUnprocessableEntity,
		}), nil
	}

	// Use retry logic to handle concurrent update conflicts
	var provisioningRequest *provisioningv1alpha1.ProvisioningRequest
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Get the latest ProvisioningRequest
		existingProvisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}
		if err := r.HubClient.Get(ctx, types.NamespacedName{Name: request.ProvisioningRequestId.String()}, existingProvisioningRequest); err != nil {
			return fmt.Errorf("failed to get ProvisioningRequest (%s): %w", request.ProvisioningRequestId.String(), err)
		}

		// Convert API request to CR
		var convErr error
		provisioningRequest, convErr = convertProvisioningRequestApiToCR(*request.Body)
		if convErr != nil {
			return convErr
		}

		// Set the ResourceVersion from the latest fetched object
		provisioningRequest.SetResourceVersion(existingProvisioningRequest.ResourceVersion)

		// Attempt the update
		if err := r.HubClient.Update(ctx, provisioningRequest); err != nil {
			return fmt.Errorf("failed to update ProvisioningRequest (%s): %w", request.ProvisioningRequestId.String(), err)
		}
		return nil
	})

	if err != nil {
		if k8serrors.IsNotFound(err) {
			return api.UpdateProvisioningRequest404ApplicationProblemPlusJSONResponse(common.ProblemDetails{
				AdditionalAttributes: &map[string]string{
					"provisioningRequestId": request.ProvisioningRequestId.String(),
				},
				Detail: "requested ProvisioningRequest not found",
				Status: http.StatusNotFound,
			}), nil
		}
		// API server and webhook validation errors
		if k8serrors.IsForbidden(err) || k8serrors.IsBadRequest(err) || k8serrors.IsInvalid(err) {
			return api.UpdateProvisioningRequest400ApplicationProblemPlusJSONResponse(common.ProblemDetails{
				AdditionalAttributes: &map[string]string{
					"provisioningRequestId": request.Body.ProvisioningRequestId.String(),
				},
				Detail: err.Error(),
				Status: http.StatusBadRequest,
			}), nil
		}
		return nil, fmt.Errorf("failed to update ProvisioningRequest (%s): %w", request.ProvisioningRequestId.String(), err)
	}

	// Query the updated ProvisioningRequest to get the latest status and convert to API provisioningRequestInfo
	updatedProvisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}
	err = r.HubClient.Get(ctx, types.NamespacedName{Name: provisioningRequest.Name}, updatedProvisioningRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to get ProvisioningRequest (%s): %w", provisioningRequest.Name, err)
	}
	provisioningRequestInfo, err := convertProvisioningRequestCRToApi(request.ProvisioningRequestId, *updatedProvisioningRequest)
	if err != nil {
		return nil, err
	}

	slog.Info("Updated ProvisioningRequest", "provisioningRequestId", request.ProvisioningRequestId.String())
	return api.UpdateProvisioningRequest200JSONResponse(provisioningRequestInfo), nil
}

// DeleteProvisioningRequest handles an API request to delete a provisioning request
func (r *ProvisioningServer) DeleteProvisioningRequest(ctx context.Context, request api.DeleteProvisioningRequestRequestObject) (api.DeleteProvisioningRequestResponseObject, error) {
	err := r.HubClient.Delete(ctx, &provisioningv1alpha1.ProvisioningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: request.ProvisioningRequestId.String(),
		},
	})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return api.DeleteProvisioningRequest404ApplicationProblemPlusJSONResponse(common.ProblemDetails{
				AdditionalAttributes: &map[string]string{
					"provisioningRequestId": request.ProvisioningRequestId.String(),
				},
				Detail: "requested ProvisioningRequest not found",
				Status: http.StatusNotFound,
			}), nil
		}
		return nil, fmt.Errorf("failed to delete ProvisioningRequest (%s): %w", request.ProvisioningRequestId.String(), err)
	}
	slog.Info("The deletion request for ProvisioningRequest has been sent successfully", "provisioningRequestId", request.ProvisioningRequestId.String())
	location := fmt.Sprintf("%s/provisioningRequests/%s", constants.O2IMSProvisioningBaseURL, request.ProvisioningRequestId)
	return api.DeleteProvisioningRequest202Response{
		Headers: api.DeleteProvisioningRequest202ResponseHeaders{
			Location: location,
		},
	}, nil
}

// convertProvisioningRequestCRToApi converts a ProvisioningRequest CR to an API model ProvisioningRequestInfo
func convertProvisioningRequestCRToApi(id uuid.UUID, provisioningRequest provisioningv1alpha1.ProvisioningRequest) (api.ProvisioningRequestInfo, error) {
	provisioningRequestInfo := api.ProvisioningRequestInfo{}

	// Map the ProvisioningRequest CR metadata UID to the provisioningRequestReference in the API model
	provisioningRequestReferenceId, err := uuid.Parse(string(provisioningRequest.UID))
	if err != nil {
		return api.ProvisioningRequestInfo{}, fmt.Errorf("could not convert ProvisioningRequest UID (%s) to uuid: %w",
			string(provisioningRequest.UID), err)
	}
	provisioningRequestInfo.ProvisioningRequestReference = provisioningRequestReferenceId

	// Unmarshal the TemplateParameters bytes into a map
	var templateParameters = make(map[string]interface{})
	err = json.Unmarshal(provisioningRequest.Spec.TemplateParameters.Raw, &templateParameters)
	if err != nil {
		return provisioningRequestInfo, fmt.Errorf("failed to unmarshal TemplateParameters into a map: %w", err)
	}
	provisioningRequestInfo.ProvisioningRequestData = api.ProvisioningRequestData{
		ProvisioningRequestId: id,
		Name:                  provisioningRequest.Spec.Name,
		Description:           provisioningRequest.Spec.Description,
		TemplateName:          provisioningRequest.Spec.TemplateName,
		TemplateVersion:       provisioningRequest.Spec.TemplateVersion,
		TemplateParameters:    templateParameters,
	}

	provisioningPhase := mapProvisioningPhase(provisioningRequest.Status.ProvisioningStatus.ProvisioningPhase)

	status := api.ProvisioningStatus{
		ProvisioningPhase:                        provisioningPhase,
		Message:                                  provisioningRequest.Status.ProvisioningStatus.ProvisioningDetails,
		UpdateTime:                               provisioningRequest.Status.ProvisioningStatus.UpdateTime.Time,
		NodeClusterProvisioningStatus:            getNodeClusterProvisioningStatus(provisioningRequest),
		InfrastructureResourceProvisioningStatus: getInfrastructureResourceProvisioningStatus(provisioningRequest),
	}
	provisioningRequestInfo.Status = status

	provisioningRequestInfo.ProvisionedResourceSet = api.ProvisionedResourceSet{
		NodeClusterId:             getNodeClusterId(provisioningRequest),
		InfrastructureResourceIds: getProvisionedInfrastructureResourceIds(provisioningRequest),
	}

	return provisioningRequestInfo, nil
}

// mapProvisioningPhase converts the CRD-level lowercase ProvisioningPhase to
// the O-RAN spec uppercase enum (table 3.4.6.3.3.1-1).
func mapProvisioningPhase(phase provisioningv1alpha1.ProvisioningPhase) api.ProvisioningStatusProvisioningPhase {
	switch phase {
	case provisioningv1alpha1.StateProgressing:
		return api.ProvisioningStatusProvisioningPhasePROGRESSING
	case provisioningv1alpha1.StateFulfilled:
		return api.ProvisioningStatusProvisioningPhaseFULFILLED
	case provisioningv1alpha1.StateFailed:
		return api.ProvisioningStatusProvisioningPhaseFAILED
	case provisioningv1alpha1.StateDeleting:
		return api.ProvisioningStatusProvisioningPhaseDELETING
	default:
		return api.ProvisioningStatusProvisioningPhasePENDING
	}
}

func getClusterName(pr provisioningv1alpha1.ProvisioningRequest) string {
	if pr.Status.Extensions.ClusterDetails == nil {
		return ""
	}
	return pr.Status.Extensions.ClusterDetails.Name
}

// getNodeClusterId returns the OCloudNodeClusterId from ProvisionedResources, or empty string if unavailable.
func getNodeClusterId(pr provisioningv1alpha1.ProvisioningRequest) string {
	if pr.Status.ProvisioningStatus.ProvisionedResources == nil {
		return ""
	}
	return pr.Status.ProvisioningStatus.ProvisionedResources.OCloudNodeClusterId
}

// getNodeClusterProvisioningStatus builds the per-cluster ResourceProvisioningStatus
// from existing fields on the ProvisioningRequest CR.
func getNodeClusterProvisioningStatus(pr provisioningv1alpha1.ProvisioningRequest) api.ResourceProvisioningStatus {
	ciCond := meta.FindStatusCondition(pr.Status.Conditions,
		string(provisioningv1alpha1.PRconditionTypes.ClusterInstanceProcessed))

	if ciCond == nil {
		return api.ResourceProvisioningStatus{
			ResourceProvisioningPhase: api.ResourceProvisioningPhasePROCESSING,
		}
	}

	// CI processing failed
	if ciCond.Status != metav1.ConditionTrue {
		phase := api.ResourceProvisioningPhasePROCESSING
		if ciCond.Reason == string(provisioningv1alpha1.CRconditionReasons.Failed) ||
			ciCond.Reason == string(provisioningv1alpha1.CRconditionReasons.TimedOut) {
			phase = api.ResourceProvisioningPhaseFAILED
		}
		return api.ResourceProvisioningStatus{
			ResourceName:              getClusterName(pr),
			ResourceId:                getNodeClusterId(pr),
			ResourceProvisioningPhase: phase,
		}
	}

	// CI applied successfully: determine phase from ClusterProvisioned condition.
	phase := api.ResourceProvisioningPhasePROCESSING
	if cond := meta.FindStatusCondition(pr.Status.Conditions,
		string(provisioningv1alpha1.PRconditionTypes.ClusterProvisioned)); cond != nil {
		switch {
		case cond.Status == metav1.ConditionTrue:
			phase = api.ResourceProvisioningPhasePROVISIONED
		case cond.Reason == string(provisioningv1alpha1.CRconditionReasons.Failed) ||
			cond.Reason == string(provisioningv1alpha1.CRconditionReasons.TimedOut):
			phase = api.ResourceProvisioningPhaseFAILED
		}
	}

	return api.ResourceProvisioningStatus{
		ResourceName:              getClusterName(pr),
		ResourceId:                getNodeClusterId(pr),
		ResourceProvisioningPhase: phase,
	}
}

// convertProvisioningRequestApiToCR converts an API model ProvisioningRequestData to a ProvisioningRequest CR
func convertProvisioningRequestApiToCR(request api.ProvisioningRequestData) (*provisioningv1alpha1.ProvisioningRequest, error) {
	// Marshal the TemplateParameters map into bytes
	templateParametersBytes, err := json.Marshal(request.TemplateParameters)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal TemplateParameters into bytes: %w", err)
	}

	provisioningRequest := &provisioningv1alpha1.ProvisioningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: request.ProvisioningRequestId.String(), // provisioningRequestId is used as the name of the ProvisioningRequest CR
		},
		Spec: provisioningv1alpha1.ProvisioningRequestSpec{
			Name:               request.Name,
			Description:        request.Description,
			TemplateName:       request.TemplateName,
			TemplateVersion:    request.TemplateVersion,
			TemplateParameters: runtime.RawExtension{Raw: templateParametersBytes},
		},
	}

	return provisioningRequest, nil
}

// getInfrastructureResourceProvisioningStatus converts the per-node statuses
// stored on the ProvisioningRequest CRD to the API []ResourceProvisioningStatus.
func getInfrastructureResourceProvisioningStatus(pr provisioningv1alpha1.ProvisioningRequest) []api.ResourceProvisioningStatus {
	if len(pr.Status.Extensions.InfrastructureResourceStatuses) == 0 {
		return []api.ResourceProvisioningStatus{}
	}
	result := make([]api.ResourceProvisioningStatus, 0, len(pr.Status.Extensions.InfrastructureResourceStatuses))
	for _, s := range pr.Status.Extensions.InfrastructureResourceStatuses {
		result = append(result, api.ResourceProvisioningStatus{
			ResourceName:              s.ResourceName,
			ResourceId:                s.ResourceId,
			ResourceProvisioningPhase: api.ResourceProvisioningPhase(s.ResourceProvisioningPhase),
		})
	}
	return result
}

// getProvisionedInfrastructureResourceIds collects ResourceId values from nodes
// that have reached the PROVISIONED phase.
func getProvisionedInfrastructureResourceIds(pr provisioningv1alpha1.ProvisioningRequest) []string {
	ids := make([]string, 0)
	for _, s := range pr.Status.Extensions.InfrastructureResourceStatuses {
		if s.ResourceProvisioningPhase == provisioningv1alpha1.ResourceProvisioningPhaseProvisioned {
			ids = append(ids, s.ResourceId)
		}
	}
	return ids
}
