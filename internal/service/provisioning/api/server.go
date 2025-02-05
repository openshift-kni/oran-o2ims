package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonapi "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	api "github.com/openshift-kni/oran-o2ims/internal/service/provisioning/api/generated"
)

type ProvisioningServer struct {
	HubClient client.Client
}

type ProvisioningServerConfig struct {
	utils.CommonServerConfig
}

// ProvisioningServer implements StrictServerInterface. This ensures that we've conformed to the `StrictServerInterface` with a compile-time check
var _ api.StrictServerInterface = (*ProvisioningServer)(nil)

// baseURL is the prefix for all of our supported API endpoints
var baseURL = "/o2ims-infrastructureProvisioning/v1"
var currentVersion = "1.0.0"

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
	if err := options.Validate(api.ProvisioningRequest{}); err != nil {
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
	objects := make([]api.ProvisioningRequest, 0, len(provisioningRequests.Items))
	for _, provisioningRequest := range provisioningRequests.Items {
		// Convert the ProvisioningRequest's name to uuid
		// TODO: Check name is a valid uuid in the validation webhook
		provisioningRequestId, err := uuid.Parse(provisioningRequest.Name)
		if err != nil {
			return nil, fmt.Errorf("could not convert ProvisioningRequest name (%s) to uuid: %w",
				provisioningRequest.Name, err)
		}

		object, err := convertProvisioningRequestCRToApi(provisioningRequestId, provisioningRequest, options)
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

	object, err := convertProvisioningRequestCRToApi(request.ProvisioningRequestId, provisioningRequest, commonapi.NewDefaultFieldOptions())
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

	slog.Info("Created ProvisioningRequest", "provisioningRequestId", request.Body.ProvisioningRequestId.String())
	return api.CreateProvisioningRequest201JSONResponse(*request.Body), nil
}

// UpdateProvisioningRequest handles an API request to update a provisioning request
func (r *ProvisioningServer) UpdateProvisioningRequest(ctx context.Context, request api.UpdateProvisioningRequestRequestObject) (api.UpdateProvisioningRequestResponseObject, error) {
	// Get the existing ProvisioningRequest
	existingProvisioningRequest := &provisioningv1alpha1.ProvisioningRequest{}
	err := r.HubClient.Get(ctx, types.NamespacedName{Name: request.ProvisioningRequestId.String()}, existingProvisioningRequest)
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
		return nil, fmt.Errorf("failed to get ProvisioningRequest (%s): %w", request.ProvisioningRequestId.String(), err)
	}

	provisioningRequest, err := convertProvisioningRequestApiToCR(*request.Body)
	if err != nil {
		return nil, err
	}
	provisioningRequest.SetResourceVersion(existingProvisioningRequest.ResourceVersion)
	err = r.HubClient.Update(ctx, provisioningRequest)
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

	return api.UpdateProvisioningRequest200JSONResponse(*request.Body), nil
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
	return api.DeleteProvisioningRequest200Response{}, nil
}

// convertProvisioningRequestCRToApi converts a ProvisioningRequest CR to an API model
func convertProvisioningRequestCRToApi(id uuid.UUID, provisioningRequest provisioningv1alpha1.ProvisioningRequest, options *commonapi.FieldOptions) (api.ProvisioningRequest, error) {
	var status api.ProvisioningRequestStatus
	if provisioningRequest.Status.ProvisioningStatus.ProvisioningPhase != "" {
		provisioningPhase := api.ProvisioningStatusProvisioningPhase(provisioningRequest.Status.ProvisioningStatus.ProvisioningPhase)
		status.ProvisioningStatus.ProvisioningPhase = &provisioningPhase
	}
	if provisioningRequest.Status.ProvisioningStatus.ProvisioningDetails != "" {
		status.ProvisioningStatus.Message = &provisioningRequest.Status.ProvisioningStatus.ProvisioningDetails
	}
	if !provisioningRequest.Status.ProvisioningStatus.UpdateTime.IsZero() {
		status.ProvisioningStatus.UpdateTime = &provisioningRequest.Status.ProvisioningStatus.UpdateTime.Time
	}

	// Convert the OCloudNodeClusterId string to uuid if it exists
	if provisioningRequest.Status.ProvisioningStatus.ProvisionedResources != nil &&
		provisioningRequest.Status.ProvisioningStatus.ProvisionedResources.OCloudNodeClusterId != "" {
		nodeClusterId, err := uuid.Parse(provisioningRequest.Status.ProvisioningStatus.ProvisionedResources.OCloudNodeClusterId)
		if err != nil {
			return api.ProvisioningRequest{}, fmt.Errorf("could not convert OCloudNodeClusterId (%s) to uuid: %w",
				provisioningRequest.Status.ProvisioningStatus.ProvisionedResources.OCloudNodeClusterId, err)
		}
		status.ProvisionedResourceSets = &api.ProvisionedResourceSets{
			NodeClusterId: &nodeClusterId,
		}
	}

	// Unmarshal the TemplateParameters bytes into a map
	var templateParameters = make(map[string]interface{})
	err := json.Unmarshal(provisioningRequest.Spec.TemplateParameters.Raw, &templateParameters)
	if err != nil {
		return api.ProvisioningRequest{}, fmt.Errorf("failed to unmarshal TemplateParameters into a map: %w", err)
	}

	result := api.ProvisioningRequest{
		ProvisioningRequestId: id,
		Name:                  provisioningRequest.Spec.Name,
		Description:           provisioningRequest.Spec.Description,
		TemplateName:          provisioningRequest.Spec.TemplateName,
		TemplateVersion:       provisioningRequest.Spec.TemplateVersion,
		TemplateParameters:    templateParameters,
		Status:                &status,
	}

	if options.IsIncluded(commonapi.ExtensionsAttribute) {
		// Convert the CR's status.extensions to map[string]interface{}
		var extensions = make(map[string]interface{})
		extensionsBytes, err := json.Marshal(provisioningRequest.Status.Extensions)
		if err != nil {
			return api.ProvisioningRequest{}, fmt.Errorf("failed to marshal Extensions into bytes: %w", err)
		}
		if err := json.Unmarshal(extensionsBytes, &extensions); err != nil {
			return api.ProvisioningRequest{}, fmt.Errorf("failed to unmarshal Extensions into a map: %w", err)
		}

		result.Extensions = &extensions
	}

	return result, nil
}

// convertProvisioningRequestApiToCR converts an API model to a ProvisioningRequest CR
func convertProvisioningRequestApiToCR(request api.ProvisioningRequest) (*provisioningv1alpha1.ProvisioningRequest, error) {
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
