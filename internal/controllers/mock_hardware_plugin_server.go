/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Generated-By: Cursor/claude-4-sonnet
*/

/*
Package controllers provides MockHardwarePluginServer for testing hardware plugin interactions.

This mock server enables testing of the following scenarios:

API Version Testing:
- GET /hardware-manager/provisioning/api-versions - Returns mock API version information
- GET /hardware-manager/provisioning/v1/api-versions - Returns v1 API version details

NodeAllocationRequest Lifecycle Testing:
- POST /hardware-manager/provisioning/v1/node-allocation-requests - Create new allocation requests
- GET /hardware-manager/provisioning/v1/node-allocation-requests - List all allocation requests
- GET /hardware-manager/provisioning/v1/node-allocation-requests/{id} - Get specific allocation request
- PUT /hardware-manager/provisioning/v1/node-allocation-requests/{id} - Update allocation request
- DELETE /hardware-manager/provisioning/v1/node-allocation-requests/{id} - Delete allocation request

AllocatedNodes Testing:
- GET /hardware-manager/provisioning/v1/node-allocation-requests/{id}/allocated-nodes - Get allocated nodes for a request
- GET /hardware-manager/provisioning/v1/allocated-nodes - Get all allocated nodes across requests

Authentication Testing:
- All endpoints support authentication middleware (accepts any auth for testing)

Mock Data Scenarios:
- Default NodeAllocationRequest with controller and worker node groups
- Default AllocatedNodes with BMC details, network interfaces, and status conditions
- Configurable mock responses via SetNodeAllocationRequest() and SetAllocatedNodes()
- Automatic creation of allocated nodes when new requests are posted

Status and Condition Testing:
- NodeAllocationRequest status with Provisioned/Configured conditions
- AllocatedNode status with Ready conditions
- Transition time tracking for status changes
- Configuration transaction ID tracking

Error Scenario Testing:
- 404 Not Found for non-existent resources
- 400 Bad Request for invalid request bodies
- 405 Method Not Allowed for unsupported HTTP methods
- 500 Internal Server Error for encoding failures

This mock server is designed to be used by controller tests that need to simulate
hardware plugin API interactions without requiring a real hardware plugin server.
*/

package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	hwmgrpluginapi "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/client/provisioning"
)

const (
	// Test cluster ID used throughout mock server
	testClusterID = "cluster-1"
)

// MockHardwarePluginServer is a test HTTP server that implements hardware plugin API endpoints
type MockHardwarePluginServer struct {
	server                 *httptest.Server
	nodeAllocationRequests map[string]*hwmgrpluginapi.NodeAllocationRequestResponse
	allocatedNodes         map[string][]hwmgrpluginapi.AllocatedNode
}

// NewMockHardwarePluginServer creates and starts a new mock hardware plugin server
func NewMockHardwarePluginServer() *MockHardwarePluginServer {
	mock := &MockHardwarePluginServer{
		nodeAllocationRequests: make(map[string]*hwmgrpluginapi.NodeAllocationRequestResponse),
		allocatedNodes:         make(map[string][]hwmgrpluginapi.AllocatedNode),
	}

	// Setup default mock data
	mock.setupDefaultData()

	// Create HTTP server with routes
	mux := http.NewServeMux()
	mock.setupRoutes(mux)
	mock.server = httptest.NewServer(mux)

	return mock
}

// GetURL returns the mock server URL
func (m *MockHardwarePluginServer) GetURL() string {
	return m.server.URL
}

// Close shuts down the mock server
func (m *MockHardwarePluginServer) Close() {
	m.server.Close()
}

// setupDefaultData creates default mock responses
func (m *MockHardwarePluginServer) setupDefaultData() {
	// Default NodeAllocationRequest
	nodeAllocRequestID := testClusterID
	m.nodeAllocationRequests[nodeAllocRequestID] = &hwmgrpluginapi.NodeAllocationRequestResponse{
		Status: &hwmgrpluginapi.NodeAllocationRequestStatus{
			Conditions: &[]hwmgrpluginapi.Condition{
				{
					Type:               "Provisioned",
					Status:             "True",
					Reason:             "Completed",
					Message:            "Hardware provisioning completed successfully",
					LastTransitionTime: time.Now(),
				},
				{
					Type:               "Configured",
					Status:             "True",
					Reason:             "Completed",
					Message:            "Hardware configuration completed successfully",
					LastTransitionTime: time.Now(),
				},
			},
			ObservedConfigTransactionId: &[]int64{0}[0], // Pointer to int64(0) to match test object Generation
		},
		NodeAllocationRequest: &hwmgrpluginapi.NodeAllocationRequest{
			ClusterId:           testClusterID,
			Site:                "test-site",
			BootInterfaceLabel:  "bootable-interface",
			ConfigTransactionId: 0,
			NodeGroup: []hwmgrpluginapi.NodeGroup{
				{
					NodeGroupData: hwmgrpluginapi.NodeGroupData{
						Name:             "controller",
						Role:             "master",
						HwProfile:        "profile-spr-single-processor-64G",
						ResourceGroupId:  "xyz",
						ResourceSelector: map[string]string{},
						Size:             1,
					},
				},
				{
					NodeGroupData: hwmgrpluginapi.NodeGroupData{
						Name:             "worker",
						Role:             "worker",
						HwProfile:        "profile-spr-dual-processor-128G",
						ResourceGroupId:  "xyz",
						ResourceSelector: map[string]string{},
						Size:             0,
					},
				},
			},
		},
	}

	// Default AllocatedNodes
	m.allocatedNodes[nodeAllocRequestID] = []hwmgrpluginapi.AllocatedNode{
		{
			Id:                  "test-node-1",
			GroupName:           "controller",
			HwProfile:           "profile-spr-single-processor-64G",
			ConfigTransactionId: 1,
			Bmc: hwmgrpluginapi.BMC{
				Address:         "redfish+http://192.168.111.20/redfish/v1/Systems/1",
				CredentialsName: "test-node-1-bmc-secret",
			},
			Interfaces: []hwmgrpluginapi.Interface{
				{
					Name:       "eth0",
					MacAddress: "00:11:22:33:44:55",
					Label:      "base-interface",
				},
				{
					Name:       "eth1",
					MacAddress: "66:77:88:99:CC:BB",
					Label:      "data-interface",
				},
				{
					Name:       "eno1",
					MacAddress: "AA:BB:CC:DD:EE:FF",
					Label:      "bootable-interface",
				},
			},
			Status: hwmgrpluginapi.AllocatedNodeStatus{
				Conditions: &[]hwmgrpluginapi.Condition{
					{
						Type:               "Ready",
						Status:             "True",
						Reason:             "Provisioned",
						Message:            "Node is ready",
						LastTransitionTime: time.Now(),
					},
				},
			},
		},
	}
}

// setupRoutes configures the HTTP routes for the mock server
func (m *MockHardwarePluginServer) setupRoutes(mux *http.ServeMux) {
	// API versions endpoints
	mux.HandleFunc("/hardware-manager/provisioning/api-versions", m.withAuth(m.handleAPIVersions))
	mux.HandleFunc("/hardware-manager/provisioning/v1/api-versions", m.withAuth(m.handleAPIVersions))

	// NodeAllocationRequest endpoints
	mux.HandleFunc("/hardware-manager/provisioning/v1/node-allocation-requests", m.withAuth(m.handleNodeAllocationRequests))
	mux.HandleFunc("/hardware-manager/provisioning/v1/node-allocation-requests/", m.withAuth(m.handleNodeAllocationRequestByID))

	// AllocatedNodes endpoints
	mux.HandleFunc("/hardware-manager/provisioning/v1/allocated-nodes", m.withAuth(m.handleAllocatedNodes))
}

// withAuth is a middleware that accepts any authentication for testing purposes
func (m *MockHardwarePluginServer) withAuth(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// For testing, accept any authentication or no authentication
		// In a real server, this would validate the credentials
		handler(w, r)
	}
}

// handleAPIVersions returns mock API version information
func (m *MockHardwarePluginServer) handleAPIVersions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	version := "v1"
	response := hwmgrpluginapi.APIVersions{
		ApiVersions: &[]hwmgrpluginapi.APIVersion{
			{
				Version: &version,
			},
		},
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// handleNodeAllocationRequests handles requests to the node allocation requests endpoint
func (m *MockHardwarePluginServer) handleNodeAllocationRequests(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		// Return list of all NodeAllocationRequests
		var requests []hwmgrpluginapi.NodeAllocationRequestResponse
		for _, req := range m.nodeAllocationRequests {
			requests = append(requests, *req)
		}
		if err := json.NewEncoder(w).Encode(requests); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}

	case http.MethodPost:
		// Create new NodeAllocationRequest
		var request hwmgrpluginapi.NodeAllocationRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// For tests, use testClusterID as the default ID to match test expectations
		// Each test should be isolated, so we can reuse testClusterID
		requestID := testClusterID

		// Store the request
		response := &hwmgrpluginapi.NodeAllocationRequestResponse{
			NodeAllocationRequest: &request,
			Status: &hwmgrpluginapi.NodeAllocationRequestStatus{
				Conditions: &[]hwmgrpluginapi.Condition{
					{
						Type:               "Provisioned",
						Status:             "False",
						Reason:             "InProgress",
						Message:            "Hardware provisioning in progress",
						LastTransitionTime: time.Now(),
					},
				},
			},
		}
		m.nodeAllocationRequests[requestID] = response

		// Automatically create default allocated nodes for this request
		if _, exists := m.allocatedNodes[requestID]; !exists {
			m.allocatedNodes[requestID] = []hwmgrpluginapi.AllocatedNode{
				{
					Id:                  "test-node-1",
					GroupName:           "controller",
					HwProfile:           "profile-spr-single-processor-64G",
					ConfigTransactionId: 1,
					Bmc: hwmgrpluginapi.BMC{
						Address:         "redfish+http://192.168.111.20/redfish/v1/Systems/1",
						CredentialsName: "test-node-1-bmc-secret",
					},
					Interfaces: []hwmgrpluginapi.Interface{
						{
							Name:       "eth0",
							MacAddress: "00:11:22:33:44:55",
							Label:      "base-interface",
						},
						{
							Name:       "eth1",
							MacAddress: "66:77:88:99:CC:BB",
							Label:      "data-interface",
						},
						{
							Name:       "eno1",
							MacAddress: "AA:BB:CC:DD:EE:FF",
							Label:      "bootable-interface",
						},
					},
					Status: hwmgrpluginapi.AllocatedNodeStatus{
						Conditions: &[]hwmgrpluginapi.Condition{
							{
								Type:               "Ready",
								Status:             "True",
								Reason:             "Provisioned",
								Message:            "Node is ready",
								LastTransitionTime: time.Now(),
							},
						},
					},
				},
				{
					Id:                  "master-node-2",
					GroupName:           "controller",
					HwProfile:           "profile-spr-single-processor-64G",
					ConfigTransactionId: 1,
					Bmc: hwmgrpluginapi.BMC{
						Address:         "redfish+http://192.168.111.21/redfish/v1/Systems/1",
						CredentialsName: "master-node-2-bmc-secret",
					},
					Interfaces: []hwmgrpluginapi.Interface{
						{
							Name:       "eth0",
							MacAddress: "00:11:22:33:44:56",
							Label:      "base-interface",
						},
						{
							Name:       "eth1",
							MacAddress: "66:77:88:99:CC:BC",
							Label:      "data-interface",
						},
						{
							Name:       "eno1",
							MacAddress: "AA:BB:CC:DD:EE:F0",
							Label:      "bootable-interface",
						},
					},
					Status: hwmgrpluginapi.AllocatedNodeStatus{
						Conditions: &[]hwmgrpluginapi.Condition{
							{
								Type:               "Ready",
								Status:             "True",
								Reason:             "Provisioned",
								Message:            "Node is ready",
								LastTransitionTime: time.Now(),
							},
						},
					},
				},
			}
		}

		// Return the ID - client expects 202 Accepted with string response
		w.WriteHeader(http.StatusAccepted)
		if err := json.NewEncoder(w).Encode(requestID); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleNodeAllocationRequestByID handles requests for specific NodeAllocationRequests
func (m *MockHardwarePluginServer) handleNodeAllocationRequestByID(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract ID from path
	path := strings.TrimPrefix(r.URL.Path, "/hardware-manager/provisioning/v1/node-allocation-requests/")
	parts := strings.Split(path, "/")
	requestID := parts[0]

	switch r.Method {
	case http.MethodGet:
		if strings.HasSuffix(path, "/allocated-nodes") {
			// First check if the NodeAllocationRequest exists
			if _, exists := m.nodeAllocationRequests[requestID]; !exists {
				http.Error(w, "Not found", http.StatusNotFound)
				return
			}

			// Return allocated nodes for this request
			if nodes, exists := m.allocatedNodes[requestID]; exists {
				if err := json.NewEncoder(w).Encode(nodes); err != nil {
					http.Error(w, "Failed to encode response", http.StatusInternalServerError)
					return
				}
			} else {
				// If no specific allocated nodes exist for this request ID,
				// return default allocated nodes to prevent test failures
				defaultNodes := []hwmgrpluginapi.AllocatedNode{
					{
						Id:                  "test-node-1",
						GroupName:           "controller",
						HwProfile:           "profile-spr-single-processor-64G",
						ConfigTransactionId: 1,
						Bmc: hwmgrpluginapi.BMC{
							Address:         "redfish+http://192.168.111.20/redfish/v1/Systems/1",
							CredentialsName: "test-node-1-bmc-secret",
						},
						Interfaces: []hwmgrpluginapi.Interface{
							{
								Name:       "eth0",
								MacAddress: "00:11:22:33:44:55",
								Label:      "base-interface",
							},
							{
								Name:       "eth1",
								MacAddress: "66:77:88:99:CC:BB",
								Label:      "data-interface",
							},
							{
								Name:       "eno1",
								MacAddress: "AA:BB:CC:DD:EE:FF",
								Label:      "bootable-interface",
							},
						},
						Status: hwmgrpluginapi.AllocatedNodeStatus{
							Conditions: &[]hwmgrpluginapi.Condition{
								{
									Type:               "Ready",
									Status:             "True",
									Reason:             "Provisioned",
									Message:            "Node is ready",
									LastTransitionTime: time.Now(),
								},
							},
						},
					},
				}
				if err := json.NewEncoder(w).Encode(defaultNodes); err != nil {
					http.Error(w, "Failed to encode response", http.StatusInternalServerError)
					return
				}
			}
			return
		}

		// Return specific NodeAllocationRequest
		if response, exists := m.nodeAllocationRequests[requestID]; exists {
			if err := json.NewEncoder(w).Encode(response); err != nil {
				http.Error(w, "Failed to encode response", http.StatusInternalServerError)
				return
			}
		} else {
			http.Error(w, "Not found", http.StatusNotFound)
		}

	case http.MethodPut:
		// Update NodeAllocationRequest
		var request hwmgrpluginapi.NodeAllocationRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if existing, exists := m.nodeAllocationRequests[requestID]; exists {
			existing.NodeAllocationRequest = &request
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			if err := json.NewEncoder(w).Encode(requestID); err != nil {
				http.Error(w, "Failed to encode response", http.StatusInternalServerError)
				return
			}
		} else {
			http.Error(w, "Not found", http.StatusNotFound)
		}

	case http.MethodDelete:
		// Delete NodeAllocationRequest
		if _, exists := m.nodeAllocationRequests[requestID]; exists {
			delete(m.nodeAllocationRequests, requestID)
			delete(m.allocatedNodes, requestID)
			w.WriteHeader(http.StatusAccepted)
			if err := json.NewEncoder(w).Encode(requestID); err != nil {
				http.Error(w, "Failed to encode response", http.StatusInternalServerError)
				return
			}
		} else {
			http.Error(w, "Not found", http.StatusNotFound)
		}

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAllocatedNodes handles requests to the allocated nodes endpoint
func (m *MockHardwarePluginServer) handleAllocatedNodes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodGet {
		// Return all allocated nodes
		var allNodes []hwmgrpluginapi.AllocatedNode
		for _, nodes := range m.allocatedNodes {
			allNodes = append(allNodes, nodes...)
		}
		if err := json.NewEncoder(w).Encode(allNodes); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// SetNodeAllocationRequest allows tests to set up specific mock data
func (m *MockHardwarePluginServer) SetNodeAllocationRequest(id string, response *hwmgrpluginapi.NodeAllocationRequestResponse) {
	m.nodeAllocationRequests[id] = response
}

// SetAllocatedNodes allows tests to set up specific allocated nodes
func (m *MockHardwarePluginServer) SetAllocatedNodes(nodeAllocationRequestID string, nodes []hwmgrpluginapi.AllocatedNode) {
	m.allocatedNodes[nodeAllocationRequestID] = nodes
}
