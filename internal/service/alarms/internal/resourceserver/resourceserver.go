package resourceserver

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/oapi-codegen/oapi-codegen/v2/pkg/securityprovider"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/resourceserver/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients"
)

const (
	resourceServerURLEnvName = "RESOURCE_SERVER_URL"
	tokenPathEnvName         = "TOKEN_PATH"
)

type Resource = generated.Resource
type ResourceType = generated.ResourceType

type ResourceServer struct {
	client        *generated.ClientWithResponses
	Resources     *[]Resource
	ResourceTypes *[]ResourceType
}

// New creates a new resource server object
func New() (*ResourceServer, error) {
	url := utils.GetServiceURL(utils.InventoryResourceServerName)

	// Use for local development
	resourceServerURL := os.Getenv(resourceServerURLEnvName)
	if resourceServerURL != "" {
		url = resourceServerURL
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

	return &ResourceServer{client: c}, nil
}

// GetAll fetches all necessary data from the resource server
func (r *ResourceServer) GetAll(ctx context.Context) error {
	// TODO: list resources when it will be possible to get them without pool ID

	// List resource types
	resourceTypes, err := r.GetResourceTypes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get resource types: %w", err)
	}

	r.ResourceTypes = resourceTypes
	return nil
}

// GetResourceTypes lists all resource types
func (r *ResourceServer) GetResourceTypes(ctx context.Context) (*[]ResourceType, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, clients.SingleRequestTimeout)
	defer cancel()

	resp, err := r.client.GetResourceTypesWithResponse(ctxWithTimeout, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to execute Get operation: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("status code different from 200 OK: %s", resp.Status())
	}

	return resp.JSON200, nil
}
