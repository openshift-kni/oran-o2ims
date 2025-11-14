/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package infrastructure

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"k8s.io/client-go/transport"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/infrastructure/resourceserver/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients"
)

const (
	ResourceName             = "Resource"
	resourceServerURLEnvName = "RESOURCE_SERVER_URL"
	resourceTokenPathEnvName = "TOKEN_PATH"
)

var _ Client = (*ResourceServer)(nil)

type ResourceServer struct {
	client                           *generated.ClientWithResponses
	resourceIDToResourceTypeID       map[uuid.UUID]uuid.UUID
	resourceTypeIDToAlarmDefinitions map[uuid.UUID]AlarmDefinition

	sync.Mutex
}

// Name returns the name of the client
func (r *ResourceServer) Name() string {
	return ResourceName
}

// Setup setups a new client for the resource server
func (r *ResourceServer) Setup() error {
	slog.Info("Creating ResourceServer client")

	url := ctlrutils.GetServiceURL(ctlrutils.InventoryResourceServerName)

	// Use for local development
	resourceServerURL := os.Getenv(resourceServerURLEnvName)
	if resourceServerURL != "" {
		url = resourceServerURL
	}

	// Set up transport
	tr, err := ctlrutils.GetDefaultBackendTransport()
	if err != nil {
		return fmt.Errorf("failed to create http transport: %w", err)
	}

	hc := http.Client{Transport: tr}

	tokenPath := constants.DefaultBackendTokenFile

	// Use for local development
	path := os.Getenv(resourceTokenPathEnvName)
	if path != "" {
		tokenPath = path
	}

	// Create a request editor that uses a cached token source capable of re-reading from file to pickup changes
	// as our token is renewed.
	editor := clients.AuthorizationEditor{
		Source: transport.NewCachedFileTokenSource(tokenPath),
	}
	c, err := generated.NewClientWithResponses(url, generated.WithHTTPClient(&hc), generated.WithRequestEditorFn(editor.Editor))
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	r.client = c
	r.resourceIDToResourceTypeID = make(map[uuid.UUID]uuid.UUID)
	r.resourceTypeIDToAlarmDefinitions = make(map[uuid.UUID]AlarmDefinition)

	return nil
}

// FetchAll fetches all necessary data from the resource server
func (r *ResourceServer) FetchAll(ctx context.Context) error {
	slog.Info("Getting all objects from the resource server")

	// Fetch all resource types
	resourceTypes, err := r.getResourceTypes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get resource types: %w", err)
	}

	// Build resource type ID to alarm definitions map without holding lock
	newResourceTypeIDToAlarmDefinitions := make(map[uuid.UUID]AlarmDefinition)
	for _, resourceType := range resourceTypes {
		// Fetch the real alarm dictionary for this resource type
		resp, err := r.client.GetResourceTypeAlarmDictionaryWithResponse(ctx, resourceType.ResourceTypeId)
		if err != nil {
			slog.Warn("Failed to get alarm dictionary for resource type", "resourceTypeID", resourceType.ResourceTypeId, "error", err)
			continue
		}

		if resp.StatusCode() != http.StatusOK {
			slog.Warn("Unexpected status code getting alarm dictionary", "resourceTypeID", resourceType.ResourceTypeId, "statusCode", resp.StatusCode())
			continue
		}

		if resp.JSON200 == nil {
			slog.Debug("No alarm dictionary available for resource type", "resourceTypeID", resourceType.ResourceTypeId)
			continue
		}

		// Build alarm definitions from the real alarm dictionary
		alarmDefinitions := buildAlarmDefinitionsFromDictionary(*resp.JSON200)
		newResourceTypeIDToAlarmDefinitions[resourceType.ResourceTypeId] = alarmDefinitions
		slog.Info("Mapping resource type ID to alarm definitions", "resourceTypeID", resourceType.ResourceTypeId, "alarmDictionaryID", resp.JSON200.AlarmDictionaryId, "definitionCount", len(alarmDefinitions))
	}

	// Atomically update the map while holding lock
	r.Lock()
	r.resourceTypeIDToAlarmDefinitions = newResourceTypeIDToAlarmDefinitions
	r.Unlock()

	slog.Info("Successfully synced ResourceServer objects")
	return nil
}

// GetObjectTypeID gets the resource type ID for a given resource ID
// It uses the cache if available, otherwise fetches from resource server
func (r *ResourceServer) GetObjectTypeID(ctx context.Context, resourceID uuid.UUID) (uuid.UUID, error) {
	r.Lock()
	resourceTypeID, ok := r.resourceIDToResourceTypeID[resourceID]
	r.Unlock()

	if ok {
		return resourceTypeID, nil
	}

	// Not in cache, fetch from resource server
	slog.Info("Resource ID not found in cache, fetching from resource server", "resourceID", resourceID)
	resource, err := r.getResource(ctx, resourceID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to get resource: %w", err)
	}

	// Cache the mapping
	r.Lock()
	r.resourceIDToResourceTypeID[resourceID] = resource.ResourceTypeId
	r.Unlock()

	return resource.ResourceTypeId, nil
}

// GetAlarmDefinitionID gets the alarm definition ID for a given resource type ID, name and severity
// It uses the cache if available, otherwise it fetches the data from the server
func (r *ResourceServer) GetAlarmDefinitionID(ctx context.Context, resourceTypeID uuid.UUID, name, severity string) (uuid.UUID, error) {
	r.Lock()
	alarmDefinitions, ok := r.resourceTypeIDToAlarmDefinitions[resourceTypeID]
	r.Unlock()

	if !ok {
		slog.Info("Resource type ID not found in cache, fetching from resource server", "resourceTypeID", resourceTypeID)

		// Try to fetch the alarm dictionary from the server
		resp, err := r.client.GetResourceTypeAlarmDictionaryWithResponse(ctx, resourceTypeID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("failed to get alarm dictionary for resource type: %w", err)
		}

		if resp.StatusCode() != http.StatusOK {
			return uuid.Nil, fmt.Errorf("unexpected status code getting alarm dictionary: %d", resp.StatusCode())
		}

		if resp.JSON200 == nil {
			return uuid.Nil, fmt.Errorf("no alarm dictionary available for resource type: %s", resourceTypeID)
		}

		// Build alarm definitions from the alarm dictionary
		alarmDefinitions = buildAlarmDefinitionsFromDictionary(*resp.JSON200)

		// Cache the alarm definitions
		r.Lock()
		r.resourceTypeIDToAlarmDefinitions[resourceTypeID] = alarmDefinitions
		r.Unlock()

		slog.Info("Mapping resource type ID to alarm definitions", "resourceTypeID", resourceTypeID, "alarmDictionaryID", resp.JSON200.AlarmDictionaryId, "definitionCount", len(alarmDefinitions))
	}

	uniqueIdentifier := AlarmDefinitionUniqueIdentifier{
		Name:     name,
		Severity: severity,
	}

	alarmDefinitionID, ok := alarmDefinitions[uniqueIdentifier]
	if !ok {
		slog.Info("Alarm definition not found in cache", "name", name, "severity", severity, "resourceTypeID", resourceTypeID)
		return uuid.Nil, fmt.Errorf("alarm definition not found: name=%s, severity=%s", name, severity)
	}

	return alarmDefinitionID, nil
}

// FetchAllWithRetry fetches all objects with retry logic
func (r *ResourceServer) FetchAllWithRetry(ctx context.Context, retries int) error {
	var err error
	for i := 0; i < retries; i++ {
		err = r.FetchAll(ctx)
		if err == nil {
			return nil
		}
		slog.Error("Failed to fetch all objects from resource server, retrying", "attempt", i+1, "error", err)
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("failed to fetch all objects after %d retries: %w", retries, err)
}

// Sync starts a background process to populate and keep up-to-date a local cache with data from the resource server
func (r *ResourceServer) Sync(ctx context.Context) {
	slog.Info("Starting sync process for resource server objects")

	// First fetch of all objects.
	// When doing a clean deployment resource server may not be ready which results incomplete data during startup Alerts sync
	// Making an effort with retry to make sure everything comes out clean before Alarms server starts up
	// This is edge case and even if the Resource server cant come up within retry time, we can still continue
	// But once it does come up, user may get unwanted "CHANGED" alerts
	if err := r.FetchAllWithRetry(ctx, 3); err != nil {
		slog.Error("Failed to run initial sync for resource server objects", "error", err)
	}

	go func() {
		ticker := time.NewTicker(resyncInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("Stopping sync process for resource server objects")
				return
			case <-ticker.C:
				slog.Info("Syncing resource server objects")
				if err := r.FetchAll(ctx); err != nil {
					slog.Error("Failed to sync resource server objects", "error", err)
				}
			}
		}
	}()
}

// getResourceTypes fetches all resource types from the resource server
func (r *ResourceServer) getResourceTypes(ctx context.Context) ([]generated.ResourceType, error) {
	resp, err := r.client.GetResourceTypesWithResponse(ctx, &generated.GetResourceTypesParams{})
	if err != nil {
		return nil, fmt.Errorf("failed to get resource types: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return nil, fmt.Errorf("empty response from resource server")
	}

	return *resp.JSON200, nil
}

// getResource fetches a resource by ID from the resource server
func (r *ResourceServer) getResource(ctx context.Context, resourceID uuid.UUID) (*generated.Resource, error) {
	resp, err := r.client.GetInternalResourceByIdWithResponse(ctx, resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return nil, fmt.Errorf("empty response from resource server")
	}

	return resp.JSON200, nil
}

// buildAlarmDefinitionsFromDictionary builds the alarm definitions map from an alarm dictionary
func buildAlarmDefinitionsFromDictionary(dictionary AlarmDictionary) AlarmDefinition {
	alarmDefinitions := make(AlarmDefinition)
	for _, definition := range dictionary.AlarmDefinition {
		if definition.AlarmAdditionalFields == nil {
			slog.Error("Alarm definition has no additional fields", "alarmDefinitionID", definition.AlarmDefinitionId)
			continue
		}

		severity, ok := (*definition.AlarmAdditionalFields)[ctlrutils.AlarmDefinitionSeverityField].(string)
		if !ok {
			// It should have one, even if it is empty
			slog.Error("Alarm definition has no severity", "alarmDefinitionID", definition.AlarmDefinitionId)
			continue
		}

		uniqueIdentifier := AlarmDefinitionUniqueIdentifier{
			Name:     definition.AlarmName,
			Severity: severity,
		}

		alarmDefinitions[uniqueIdentifier] = definition.AlarmDefinitionId
	}

	return alarmDefinitions
}
