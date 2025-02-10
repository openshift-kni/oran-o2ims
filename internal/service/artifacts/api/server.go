package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	orano2imsutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"

	api "github.com/openshift-kni/oran-o2ims/internal/service/artifacts/api/generated"
	commonapi "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
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
	options := commonapi.NewFieldOptions(request.Params.AllFields, request.Params.Fields, request.Params.ExcludeFields)
	if err := options.Validate(api.ManagedInfrastructureTemplate{}); err != nil {
		return api.GetManagedInfrastructureTemplates400ApplicationProblemPlusJSONResponse{
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		}, nil
	}

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
		managedInfrastructureTemplate, err := clusterTemplateToManagedInfrastructureTemplate(clusterTemplate, options)
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

	clusterTemplatesItems, err := getClusterTemplateById(ctx, r.HubClient, request.ManagedInfrastructureTemplateId)
	if err != nil {
		return nil, err
	}

	if len(clusterTemplatesItems) == 0 {
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
	object, err := clusterTemplateToManagedInfrastructureTemplate(clusterTemplatesItems[0], commonapi.NewDefaultFieldOptions())
	if err != nil {
		return nil, err
	}
	return api.GetManagedInfrastructureTemplate200JSONResponse(object), nil
}

// Get managed infrastructure template defaults
// (GET /o2ims-infrastructureArtifacts/v1/managedInfrastructureTemplates/{managedInfrastructureTemplateId}/defaults)
func (r *ArtifactsServer) GetManagedInfrastructureTemplateDefaults(
	ctx context.Context,
	request api.GetManagedInfrastructureTemplateDefaultsRequestObject) (api.GetManagedInfrastructureTemplateDefaultsResponseObject, error) {

	clusterTemplatesItems, err := getClusterTemplateById(ctx, r.HubClient, request.ManagedInfrastructureTemplateId)
	if err != nil {
		return nil, err
	}

	if len(clusterTemplatesItems) == 0 {
		return api.GetManagedInfrastructureTemplateDefaults404ApplicationProblemPlusJSONResponse(
			common.ProblemDetails{
				AdditionalAttributes: &map[string]string{
					"managedInfrastructureTemplateId": request.ManagedInfrastructureTemplateId,
				},
				Detail: "requested ManagedInfrastructureTemplate not found",
				Status: http.StatusNotFound,
			}), nil
	}

	oranct := clusterTemplatesItems[0]
	// Get the response for the ClusterInstance default values.
	configMap, err := orano2imsutils.GetConfigmap(
		ctx, r.HubClient, oranct.Spec.Templates.ClusterInstanceDefaults, oranct.Namespace)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get ConfigMap %s/%s: %w", oranct.Spec.Templates.ClusterInstanceDefaults, oranct.Namespace, err)
	}
	clusterInstanceDefaults, err := orano2imsutils.ExtractTemplateDataFromConfigMap[map[string]any](
		configMap, orano2imsutils.ClusterInstanceTemplateDefaultsConfigmapKey)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to extract the default values from the %s/%s ConfigMap: %w",
			oranct.Spec.Templates.ClusterInstanceDefaults, oranct.Namespace, err)
	}

	// Get the response for the Policy default values.
	configMap, err = orano2imsutils.GetConfigmap(
		ctx, r.HubClient, oranct.Spec.Templates.PolicyTemplateDefaults, oranct.Namespace)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get ConfigMap %s/%s: %w", oranct.Spec.Templates.PolicyTemplateDefaults, oranct.Namespace, err)
	}
	policyTemplateDefaults, err := orano2imsutils.ExtractTemplateDataFromConfigMap[map[string]any](
		configMap, orano2imsutils.PolicyTemplateDefaultsConfigmapKey)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to extract the default values from the %s/%s ConfigMap: %w",
			oranct.Spec.Templates.PolicyTemplateDefaults, oranct.Namespace, err)
	}

	// Build the final response object.
	object := api.ManagedInfrastructureTemplateDefaults{
		PolicyTemplateDefaults:  &policyTemplateDefaults,
		ClusterInstanceDefaults: &clusterInstanceDefaults,
	}

	// Convert the current ClusterTemplate to ManagedInfrastructureTemplate.
	return api.GetManagedInfrastructureTemplateDefaults200JSONResponse(object), nil
}

// getClusterTemplateById returns either the ClusterTemplateItems containing the
// requested ClusterTemplate or an error.
func getClusterTemplateById(ctx context.Context, c client.Client, clusterTemplateId string) (
	[]provisioningv1alpha1.ClusterTemplate, error) {
	// Get the ClusterTemplates with the requested ID.
	var clusterTemplates provisioningv1alpha1.ClusterTemplateList
	err := c.List(
		ctx,
		&clusterTemplates,
		client.MatchingFields{"metadata.name": clusterTemplateId},
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
			clusterTemplateId)
	}

	return clusterTemplates.Items, nil
}

func clusterTemplateToManagedInfrastructureTemplate(clusterTemplate provisioningv1alpha1.ClusterTemplate, options *commonapi.FieldOptions) (
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

	// ClusterTemplates have just one condition holding validation information.
	// Include it as an extension.
	var clusterTemplateExtensions = make(map[string]string)
	validatedCond := meta.FindStatusCondition(
		clusterTemplate.Status.Conditions,
		string(provisioningv1alpha1.CTconditionTypes.Validated))

	if validatedCond != nil {
		clusterTemplateExtensions["status"] = fmt.Sprintf(
			"%s has %s: %s", validatedCond.Type, validatedCond.Reason, validatedCond.Message)
	}

	// Convert the current ClusterTemplate to ManagedInfrastructureTemplate.
	result := api.ManagedInfrastructureTemplate{
		ArtifactResourceId: uuid,
		Name:               clusterTemplate.Spec.Name,
		Version:            clusterTemplate.Spec.Version,
		Description:        clusterTemplate.Spec.Description,
		ParameterSchema:    parameterSchema,
	}

	if options.IsIncluded(commonapi.ExtensionsAttribute) {
		result.Extensions = &clusterTemplateExtensions
	}

	return result, nil
}
