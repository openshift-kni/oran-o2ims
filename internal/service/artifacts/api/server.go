package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	api "github.com/openshift-kni/oran-o2ims/internal/service/artifacts/api/generated"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ArtifactsServerConfig struct {
	utils.CommonServerConfig
}
type ArtifactsServer struct {
	HubClient client.Client
}

// ArtifactsServer implements StrictServerInterface. This ensures that we've conformed to the `StrictServerInterface` with a compile-time check
var _ api.StrictServerInterface = (*ArtifactsServer)(nil)

// baseURL is the prefix for all of our supported API endpoints
var baseURL = "/o2ims-infrastructureArtifacts/v1"
var currentVersion = "1.0.0"

// GetAllVersions receives the API request to this endpoint, executes the request, and responds appropriately.
func (a *ArtifactsServer) GetAllVersions(ctx context.Context, request api.GetAllVersionsRequestObject) (api.GetAllVersionsResponseObject, error) {
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

// GetMinorVersions receives the API request to this endpoint, executes the request, and responds appropriately.
func (a *ArtifactsServer) GetMinorVersions(ctx context.Context, request api.GetMinorVersionsRequestObject) (api.GetMinorVersionsResponseObject, error) {
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

// Get managed infrastructure templates
// (GET /o2ims-infrastructureArtifacts/v1/managedInfrastructureTemplates)
func (r *ArtifactsServer) GetManagedInfrastructureTemplates(
	ctx context.Context,
	request api.GetManagedInfrastructureTemplatesRequestObject) (api.GetManagedInfrastructureTemplatesResponseObject, error) {

	// Get all the ClusterTemplates from the hub cluster.
	var allClusterTemplates provisioningv1alpha1.ClusterTemplateList
	err := r.HubClient.List(ctx, &allClusterTemplates)
	if err != nil {
		return nil, fmt.Errorf("could not get list of ManagedInfrastructureTemplate across the cluster: %w", err)
	}

	// Range through the ClusterTemplates and convert them to the ManagedInfrastructureTemplate model.
	objects := make([]api.ManagedInfrastructureTemplate, 0, len(allClusterTemplates.Items))
	for _, clusterTemplate := range allClusterTemplates.Items {
		// Convert the current ClusterTemplate to ManagedInfrastructureTemplate.
		managedInfrastructureTemplate, err := clusterTemplateToManagedInfrastructureTemplate(clusterTemplate)
		if err != nil {
			return nil, err
		}
		objects = append(objects, managedInfrastructureTemplate)
	}

	return api.GetManagedInfrastructureTemplates200JSONResponse(objects), nil
}

// Get managed infrastructure templates
// (GET /o2ims-infrastructureArtifacts/v1/managedInfrastructureTemplates/{managedInfrastructureTemplateId})
func (r *ArtifactsServer) GetManagedInfrastructureTemplate(
	ctx context.Context, request api.GetManagedInfrastructureTemplateRequestObject) (api.GetManagedInfrastructureTemplateResponseObject, error) {

	// Get the ClusterTemplates with the requested ID.
	var clusterTemplates provisioningv1alpha1.ClusterTemplateList
	err := r.HubClient.List(
		ctx,
		&clusterTemplates,
		client.MatchingFields{"metadata.name": request.ManagedInfrastructureTemplateId},
	)

	if err != nil {
		return nil, fmt.Errorf("could not list ManagedInfrastructureTemplates across the cluster: %w", err)
	}

	// There should be just one ClusterTemplate with the requested ID.
	// We need ManagedInfrastructureTemplateId to match ClusterTemplate metadata.name
	// which is of format ClusterTemplate <spec.name>.<spec.version>.
	if len(clusterTemplates.Items) > 1 {
		return nil, fmt.Errorf(
			"more than one ManagedInfrastructureTemplate with the requested ID: %s",
			request.ManagedInfrastructureTemplateId)
	}

	if len(clusterTemplates.Items) == 0 {
		return api.GetManagedInfrastructureTemplate404ApplicationProblemPlusJSONResponse(
			common.ProblemDetails{
				AdditionalAttributes: &map[string]string{
					"managedInfrastructureTemplateId": request.ManagedInfrastructureTemplateId,
				},
				Detail: "requested ManagedInfrastructureTemplate not found",
				Status: http.StatusNotFound,
			}), nil
	}

	// Convert the ClusterTemplate to the ManagedInfrastructureTemplate format.
	object, err := clusterTemplateToManagedInfrastructureTemplate(clusterTemplates.Items[0])
	if err != nil {
		return nil, err
	}
	return api.GetManagedInfrastructureTemplate200JSONResponse(object), nil
}

func clusterTemplateToManagedInfrastructureTemplate(clusterTemplate provisioningv1alpha1.ClusterTemplate) (
	api.ManagedInfrastructureTemplate, error) {

	// Validate and transform the string to UUID.
	if _, err := uuid.Parse(clusterTemplate.Spec.TemplateID); err != nil {
		return api.ManagedInfrastructureTemplate{}, fmt.Errorf("could not get uuid from ManagedInfrastructureTemplate: %w", err)
	}
	uuid := uuid.MustParse(clusterTemplate.Spec.TemplateID)
	// Obtain the parameter schema in the desired map format.
	var parameterSchema map[string]interface{}
	if err := json.Unmarshal(clusterTemplate.Spec.TemplateParameterSchema.Raw, &parameterSchema); err != nil {
		return api.ManagedInfrastructureTemplate{}, fmt.Errorf("could not get parameterSchema from ManagedInfrastructureTemplate: %w", err)
	}
	// Convert the current ClusterTemplate to ManagedInfrastructureTemplate.
	return api.ManagedInfrastructureTemplate{
		ArtifactResourceId: uuid,
		Name:               clusterTemplate.Spec.Name,
		Version:            clusterTemplate.Spec.Version,
		Description:        clusterTemplate.Spec.Description,
		ParameterSchema:    parameterSchema,
	}, nil
}
