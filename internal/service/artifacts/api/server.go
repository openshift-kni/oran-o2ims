package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	api "github.com/openshift-kni/oran-o2ims/internal/service/artifacts/api/generated"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ArtifactsServer implements StrictServerInterface. This ensures that we've conformed to the `StrictServerInterface` with a compile-time check
var _ api.StrictServerInterface = (*ArtifactsServer)(nil)

type ArtifactsServer struct {
	HubClient client.Client
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
		// Validate and transform the string to UUID.
		if _, err := uuid.Parse(clusterTemplate.Spec.TemplateID); err != nil {
			return nil, fmt.Errorf("could not get uuid from ManagedInfrastructureTemplate: %w", err)
		}
		uuid := uuid.MustParse(clusterTemplate.Spec.TemplateID)
		// Obtain the parameter schema in the desired map format.
		var parameterSchema map[string]interface{}
		if err := json.Unmarshal(clusterTemplate.Spec.TemplateParameterSchema.Raw, &parameterSchema); err != nil {
			return nil, fmt.Errorf("could not get parameterSchema from ManagedInfrastructureTemplate: %w", err)
		}
		// Convert the current ClusterTemplate to ManagedInfrastructureTemplate.
		managedInfrastructureTemplate := api.ManagedInfrastructureTemplate{
			ArtifactResourceId: uuid,
			Name:               clusterTemplate.Name,
			Version:            clusterTemplate.Spec.Version,
			Description:        clusterTemplate.Spec.Description,
			ParameterSchema:    parameterSchema,
		}
		objects = append(objects, managedInfrastructureTemplate)
	}

	return api.GetManagedInfrastructureTemplates200JSONResponse(objects), nil
}

// Get managed infrastructure templates
// (GET /o2ims-infrastructureArtifacts/v1/managedInfrastructureTemplates/{managedInfrastructureTemplateId})
func (r *ArtifactsServer) GetManagedInfrastructureTemplate(
	ctx context.Context, request api.GetManagedInfrastructureTemplateRequestObject) (api.GetManagedInfrastructureTemplateResponseObject, error) {

	// TODO implement me
	return nil, fmt.Errorf("not implemented")
}
