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
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/infrastructure/clusterserver/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients"
	sharedtls "github.com/openshift-kni/oran-o2ims/internal/shared/tls"
)

const (
	Name = "Cluster"

	clusterServerURLEnvName = "CLUSTER_SERVER_URL"
	tokenPathEnvName        = "TOKEN_PATH"
)

type NodeCluster = generated.NodeCluster
type NodeClusterType = generated.NodeClusterType

type ClusterServer struct {
	client                               *generated.ClientWithResponses
	nodeClusterIDToNodeClusterTypeID     map[uuid.UUID]uuid.UUID
	nodeClusterTypeIDToAlarmDictionaryID map[uuid.UUID]uuid.UUID
	alarmDictionaryIDToAlarmDefinitions  map[uuid.UUID]AlarmDefinition

	sync.Mutex
}

// Name returns the name of the client
func (r *ClusterServer) Name() string {
	return Name
}

// Setup setups a new client for the cluster server
func (r *ClusterServer) Setup() error {
	slog.Info("Creating ClusterServer client")

	url := ctlrutils.GetServiceURL(ctlrutils.InventoryClusterServerName)

	// Use for local development
	clusterServerURL := os.Getenv(clusterServerURLEnvName)
	if clusterServerURL != "" {
		url = clusterServerURL
	}

	// Set up transport
	tr, err := sharedtls.GetDefaultBackendTransport()
	if err != nil {
		return fmt.Errorf("failed to create http transport: %w", err)
	}

	hc := http.Client{Transport: tr}

	tokenPath := constants.DefaultBackendTokenFile

	// Use for local development
	path := os.Getenv(tokenPathEnvName)
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
	r.nodeClusterIDToNodeClusterTypeID = make(map[uuid.UUID]uuid.UUID)
	r.nodeClusterTypeIDToAlarmDictionaryID = make(map[uuid.UUID]uuid.UUID)
	r.alarmDictionaryIDToAlarmDefinitions = make(map[uuid.UUID]AlarmDefinition)

	return nil
}

// FetchAll fetches all necessary data from the cluster server
func (r *ClusterServer) FetchAll(ctx context.Context) error {
	slog.InfoContext(ctx, "Getting all objects from the cluster server")

	// List node clusters
	nodeClusters, err := r.getNodeClusters(ctx)
	if err != nil {
		return fmt.Errorf("failed to get node clusters: %w", err)
	}

	// List node cluster types
	nodeClusterTypes, err := r.getNodeClusterTypes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get node cluster types: %w", err)
	}

	// List alarm dictionaries
	alarmDictionaries, err := r.getAlarmDictionaries(ctx)
	if err != nil {
		return fmt.Errorf("failed to get alarm dictionaries: %w", err)
	}

	r.Lock()
	defer r.Unlock()

	r.nodeClusterIDToNodeClusterTypeID = r.buildNodeClusterIDToNodeClusterTypeID(nodeClusters)
	r.nodeClusterTypeIDToAlarmDictionaryID = r.buildNodeClusterTypeIDToAlarmDictionaryID(nodeClusterTypes)
	r.alarmDictionaryIDToAlarmDefinitions = r.buildAlarmDictionaryIDToAlarmDefinitions(alarmDictionaries)

	slog.InfoContext(ctx, "Successfully synced ClusterServer objects")
	return nil
}

// GetObjectTypeID gets the node cluster type ID for a given node cluster ID
// It uses the cache if available, otherwise it fetches the data from the server
func (r *ClusterServer) GetObjectTypeID(ctx context.Context, nodeClusterID uuid.UUID) (uuid.UUID, error) {
	r.Lock()
	defer r.Unlock()

	nodeClusterTypeID, ok := r.nodeClusterIDToNodeClusterTypeID[nodeClusterID]
	if !ok {
		slog.InfoContext(ctx, "Node cluster ID not found in cache", slog.String("nodeClusterID", nodeClusterID.String()))

		// Try to fetch it from the server
		nodeCluster, err := r.getNodeCluster(ctx, nodeClusterID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("failed to fetch node cluster type ID: %w", err)
		}

		nodeClusterTypeID = nodeCluster.NodeClusterTypeId
		r.nodeClusterIDToNodeClusterTypeID[nodeClusterID] = nodeClusterTypeID
		slog.InfoContext(ctx, "Mapping node cluster ID to node cluster type ID", slog.String("nodeClusterID", nodeClusterID.String()), slog.String("nodeClusterTypeID", nodeClusterTypeID.String()))
	}

	return nodeClusterTypeID, nil
}

// GetAlarmDefinitionID gets the alarm definition ID for a given node cluster type ID, name and severity
// It uses the cache if available, otherwise it fetches the data from the server
func (r *ClusterServer) GetAlarmDefinitionID(ctx context.Context, nodeClusterTypeID uuid.UUID, name, severity string) (uuid.UUID, error) {
	r.Lock()
	defer r.Unlock()

	alarmDictionaryID, ok := r.nodeClusterTypeIDToAlarmDictionaryID[nodeClusterTypeID]
	if !ok {
		slog.InfoContext(ctx, "Node Cluster Type ID not found in cache", slog.String("nodeClusterTypeID", nodeClusterTypeID.String()))

		// Try to fetch it from the server
		nodeClusterType, err := r.getNodeClusterType(ctx, nodeClusterTypeID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("failed to fetch alarm dictionary ID: %w", err)
		}

		alarmDictionaryID, err = getAlarmDictionaryIDFromNodeClusterType(nodeClusterType)
		if err != nil {
			return uuid.Nil, fmt.Errorf("failed to get alarm dictionary ID from node cluster type object: %w", err)
		}

		r.nodeClusterTypeIDToAlarmDictionaryID[nodeClusterTypeID] = alarmDictionaryID
		slog.InfoContext(ctx, "Mapping node cluster type ID to alarm dictionary ID", slog.String("nodeClusterTypeID", nodeClusterTypeID.String()), slog.String("alarmDictionaryID", alarmDictionaryID.String()))
	}

	definitionsResynced := false
	alarmDefinitions, ok := r.alarmDictionaryIDToAlarmDefinitions[alarmDictionaryID]
	if !ok {
		slog.InfoContext(ctx, "Alarm dictionary ID not found in cache", slog.String("alarmDictionaryID", alarmDictionaryID.String()))

		// Try to fetch it from the server
		alarmDictionary, err := r.getAlarmDictionary(ctx, alarmDictionaryID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("failed to fetch alarm dictionary - alarm Dictionary ID: %w", err)
		}

		definitionsResynced = true
		alarmDefinitions = getAlarmDefinitionsFromAlarmDictionary(alarmDictionary)
		r.alarmDictionaryIDToAlarmDefinitions[alarmDictionaryID] = alarmDefinitions
		slog.InfoContext(ctx, "Mapping alarm dictionary ID to alarm definitions", slog.String("alarmDictionaryID", alarmDictionaryID.String()))
	}

	uniqueAlarmDefinitionIdentifier := AlarmDefinitionUniqueIdentifier{
		Name:     name,
		Severity: severity,
	}

	alarmDefinitionID, ok := alarmDefinitions[uniqueAlarmDefinitionIdentifier]
	if !ok {
		if !definitionsResynced {
			// Resync definitions and try again. It is possible that cache is not up to date
			slog.DebugContext(ctx, "Resynced alarm definitions", slog.String("alarmDictionaryID", alarmDictionaryID.String()), slog.Any("uniqueAlarmDefinitionIdentifier", uniqueAlarmDefinitionIdentifier))

			alarmDictionary, err := r.getAlarmDictionary(ctx, alarmDictionaryID)
			if err != nil {
				return uuid.Nil, fmt.Errorf("failed to fetch alarm dictionary - alarm Dictionary ID: %w", err)
			}

			alarmDefinitions = getAlarmDefinitionsFromAlarmDictionary(alarmDictionary)
			r.alarmDictionaryIDToAlarmDefinitions[alarmDictionaryID] = alarmDefinitions
			slog.InfoContext(ctx, "Mapping alarm dictionary ID to alarm definitions", slog.String("alarmDictionaryID", alarmDictionaryID.String()))

			alarmDefinitionID, ok = alarmDefinitions[uniqueAlarmDefinitionIdentifier]
			if ok {
				return alarmDefinitionID, nil
			}
		}

		return uuid.Nil, fmt.Errorf("failed to find alarm definition ID for unique identifier: %v", uniqueAlarmDefinitionIdentifier)
	}

	return alarmDefinitionID, nil
}

// Sync starts the sync process for the cluster server objects
func (r *ClusterServer) Sync(ctx context.Context) {
	slog.InfoContext(ctx, "Starting sync process for cluster server objects")

	// First fetch of all objects.
	// When doing a clean deployment cluster server may not be ready which results incomplete data during startup Alerts sync
	// Making an effort with retry to make sure everything comes out clean before Alarms server starts up
	// This is edge case and even if the Cluster server cant come up within retry time, we can still continue
	// But once it does come up, user may get unwanted "CHANGED" alerts
	if err := r.FetchAllWithRetry(ctx, 3); err != nil {
		slog.ErrorContext(ctx, "Failed to run initial sync for cluster server objects", slog.Any("error", err))
	}

	go func() {
		ticker := time.NewTicker(resyncInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.InfoContext(ctx, "Stopping sync process for cluster server objects")
				return
			case <-ticker.C:
				slog.InfoContext(ctx, "Syncing ClusterServer objects")
				if err := r.FetchAll(ctx); err != nil {
					slog.ErrorContext(ctx, "Failed to sync cluster server objects", slog.Any("error", err))
				}
			}
		}
	}()
}

// FetchAllWithRetry Helper function to retry FetchAll with exponential backoff
func (r *ClusterServer) FetchAllWithRetry(ctx context.Context, maxRetries int) error {
	var err error
	backoff := time.Second // Start with 1 second backoff

	for attempt := 0; attempt < maxRetries; attempt++ {
		err = r.FetchAll(ctx)

		// Success
		if err == nil {
			return nil
		}

		// If this was the last attempt, break out
		if attempt == maxRetries-1 {
			break
		}

		// Wait before retrying with exponential backoff
		slog.WarnContext(ctx, "Fetch operation failed", slog.Int("attempt", attempt+1), slog.Int("maxRetries", maxRetries), slog.Any("error", err))
		select {
		case <-time.After(backoff):
			backoff *= 2
		case <-ctx.Done():
			return nil
		}
	}

	return fmt.Errorf("failed after %d retries: %w", maxRetries, err)
}

// getNodeClusters lists all node clusters
func (r *ClusterServer) getNodeClusters(ctx context.Context) ([]NodeCluster, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, clients.ListRequestTimeout)
	defer cancel()

	// TODO: use filters to only request the clusterTypeID field
	resp, err := r.client.GetNodeClustersWithResponse(ctxWithTimeout, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to execute Get operation: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("status code different from 200 OK: %s", resp.Status())
	}

	slog.InfoContext(ctx, "Got node clusters", slog.Int("count", len(*resp.JSON200)))
	return *resp.JSON200, nil
}

// getNodeCluster gets a node cluster by ID
func (r *ClusterServer) getNodeCluster(ctx context.Context, nodeClusterID uuid.UUID) (NodeCluster, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, clients.SingleRequestTimeout)
	defer cancel()

	// TODO: use filters to only request the clusterTypeID field
	resp, err := r.client.GetNodeClusterWithResponse(ctxWithTimeout, nodeClusterID)
	if err != nil {
		return NodeCluster{}, fmt.Errorf("failed to execute Get operation: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return NodeCluster{}, fmt.Errorf("status code different from 200 OK: %s", resp.Status())
	}

	slog.InfoContext(ctx, "Got node cluster", slog.String("nodeClusterID", nodeClusterID.String()))
	return *resp.JSON200, nil
}

// getNodeClusterTypes lists all node cluster types
func (r *ClusterServer) getNodeClusterTypes(ctx context.Context) ([]NodeClusterType, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, clients.ListRequestTimeout)
	defer cancel()

	// TODO: use filters to only request the extensions field or the alarmDictionaryID once it is added
	resp, err := r.client.GetNodeClusterTypesWithResponse(ctxWithTimeout, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to execute Get operation: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("status code different from 200 OK: %s", resp.Status())
	}

	slog.InfoContext(ctx, "Got node cluster types", slog.Int("count", len(*resp.JSON200)))
	return *resp.JSON200, nil
}

// getNodeClusterType gets a node cluster type by ID
func (r *ClusterServer) getNodeClusterType(ctx context.Context, nodeClusterTypeID uuid.UUID) (NodeClusterType, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, clients.SingleRequestTimeout)
	defer cancel()

	// TODO: use filters to only request the extensions field or the alarmDictionaryID once it is added
	resp, err := r.client.GetNodeClusterTypeWithResponse(ctxWithTimeout, nodeClusterTypeID)
	if err != nil {
		return NodeClusterType{}, fmt.Errorf("failed to execute Get operation: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return NodeClusterType{}, fmt.Errorf("status code different from 200 OK: %s", resp.Status())
	}

	slog.InfoContext(ctx, "Got node cluster type", slog.String("nodeClusterTypeID", nodeClusterTypeID.String()))
	return *resp.JSON200, nil
}

// getAlarmDictionaries lists all alarm dictionaries
func (r *ClusterServer) getAlarmDictionaries(ctx context.Context) ([]AlarmDictionary, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, clients.ListRequestTimeout)
	defer cancel()

	// TODO: use filters to only request the definition field
	resp, err := r.client.GetAlarmDictionariesWithResponse(ctxWithTimeout, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to execute Get operation: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("status code different from 200 OK: %s", resp.Status())
	}

	slog.InfoContext(ctx, "Got alarm dictionaries", slog.Int("count", len(*resp.JSON200)))
	return *resp.JSON200, nil
}

// GetAlarmDictionary gets an alarm dictionary by ID
func (r *ClusterServer) getAlarmDictionary(ctx context.Context, alarmDictionaryID uuid.UUID) (AlarmDictionary, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, clients.SingleRequestTimeout)
	defer cancel()

	// TODO: use filters to only request the definition field
	resp, err := r.client.GetAlarmDictionaryWithResponse(ctxWithTimeout, alarmDictionaryID)
	if err != nil {
		return AlarmDictionary{}, fmt.Errorf("failed to execute Get operation: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return AlarmDictionary{}, fmt.Errorf("status code different from 200 OK: %s", resp.Status())
	}

	slog.InfoContext(ctx, "Got alarm dictionary", slog.String("alarmDictionaryID", alarmDictionaryID.String()))
	return *resp.JSON200, nil
}

// buildNodeClusterIDToNodeClusterTypeID builds the mapping of node cluster ID to node cluster type ID
func (r *ClusterServer) buildNodeClusterIDToNodeClusterTypeID(nodeClusters []NodeCluster) map[uuid.UUID]uuid.UUID {
	mapping := make(map[uuid.UUID]uuid.UUID)
	for _, nodeCluster := range nodeClusters {
		mapping[nodeCluster.NodeClusterId] = nodeCluster.NodeClusterTypeId
		slog.Info("Mapping node cluster ID to node cluster type ID", slog.String("nodeClusterID", nodeCluster.NodeClusterId.String()), slog.String("nodeClusterTypeID", nodeCluster.NodeClusterTypeId.String()))
	}

	return mapping
}

// buildNodeClusterTypeIDToAlarmDictionaryID builds the mapping of node cluster type ID to alarm dictionary ID
func (r *ClusterServer) buildNodeClusterTypeIDToAlarmDictionaryID(nodeClusterTypes []NodeClusterType) map[uuid.UUID]uuid.UUID {
	mapping := make(map[uuid.UUID]uuid.UUID)
	for _, nodeClusterType := range nodeClusterTypes {
		alarmDictionaryID, err := getAlarmDictionaryIDFromNodeClusterType(nodeClusterType)
		if err != nil {
			slog.Error("Failed to get alarm dictionary ID from node cluster type", slog.String("nodeClusterTypeID", nodeClusterType.NodeClusterTypeId.String()), slog.Any("error", err))
			continue
		}

		mapping[nodeClusterType.NodeClusterTypeId] = alarmDictionaryID
		slog.Info("Mapping node cluster type ID to alarm dictionary ID", slog.String("nodeClusterTypeID", nodeClusterType.NodeClusterTypeId.String()), slog.String("alarmDictionaryID", alarmDictionaryID.String()))
	}

	return mapping
}

// buildAlarmDictionaryIDToAlarmDefinitions builds the mapping of alarm dictionary ID to alarm definitions
func (r *ClusterServer) buildAlarmDictionaryIDToAlarmDefinitions(dictionaries []AlarmDictionary) map[uuid.UUID]AlarmDefinition {
	mapping := make(map[uuid.UUID]AlarmDefinition)
	for _, dictionary := range dictionaries {
		mapping[dictionary.AlarmDictionaryId] = getAlarmDefinitionsFromAlarmDictionary(dictionary)
		slog.Info("Mapping alarm dictionary ID to alarm definitions", slog.String("alarmDictionaryID", dictionary.AlarmDictionaryId.String()))
	}

	return mapping
}

// getAlarmDictionaryIDFromNodeClusterType gets the alarm dictionary ID from a node cluster type
func getAlarmDictionaryIDFromNodeClusterType(nodeClusterType NodeClusterType) (uuid.UUID, error) {
	if nodeClusterType.Extensions == nil {
		return uuid.Nil, fmt.Errorf("node cluster type has no extensions")
	}

	alarmDictionaryIDString, ok := (*nodeClusterType.Extensions)[ctlrutils.ClusterAlarmDictionaryIDExtension].(string)
	if !ok {
		return uuid.Nil, fmt.Errorf("node cluster type has no alarm dictionary ID")
	}

	alarmDictionaryID, err := uuid.Parse(alarmDictionaryIDString)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to parse alarm dictionary ID: %w", err)
	}

	return alarmDictionaryID, nil
}

// getAlarmDefinitionsFromAlarmDictionary gets the alarm definitions from an alarm dictionary
func getAlarmDefinitionsFromAlarmDictionary(dictionary AlarmDictionary) AlarmDefinition {
	alarmDefinitions := make(AlarmDefinition)
	for _, definition := range dictionary.AlarmDefinition {
		if definition.AlarmAdditionalFields == nil {
			slog.Error("Alarm definition has no additional fields", slog.String("alarmDefinitionID", definition.AlarmDefinitionId.String()))
			continue
		}

		severity, ok := (*definition.AlarmAdditionalFields)[ctlrutils.AlarmDefinitionSeverityField].(string)
		if !ok {
			// It should have one, even if it is empty
			slog.Error("Alarm definition has no severity", slog.String("alarmDefinitionID", definition.AlarmDefinitionId.String()))
			continue
		}

		uniqueIdentifier := AlarmDefinitionUniqueIdentifier{
			Name:     definition.AlarmName,
			Severity: severity,
		}

		alarmDefinitions[uniqueIdentifier] = definition.AlarmDefinitionId
	}

	slog.Debug("Got alarm definitions", slog.Int("count", len(alarmDefinitions)), slog.String("alarmDictionaryID", dictionary.AlarmDictionaryId.String()))
	return alarmDefinitions
}
