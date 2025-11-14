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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	hwmgrpluginapi "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/client/provisioning"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
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
	k8sClient              client.Client
	queryK8S               bool
}

// NewMockHardwarePluginServer creates and starts a new mock hardware plugin server
func NewMockHardwarePluginServer() *MockHardwarePluginServer {
	mock := &MockHardwarePluginServer{
		nodeAllocationRequests: make(map[string]*hwmgrpluginapi.NodeAllocationRequestResponse),
		allocatedNodes:         make(map[string][]hwmgrpluginapi.AllocatedNode),
		queryK8S:               false, // Default to false for backward compatibility
	}

	return mock
}

// NewMockHardwarePluginServerWithClient creates and starts a new mock hardware plugin server with Kubernetes client
func NewMockHardwarePluginServerWithClient(k8sClient client.Client) *MockHardwarePluginServer {
	mock := &MockHardwarePluginServer{
		nodeAllocationRequests: make(map[string]*hwmgrpluginapi.NodeAllocationRequestResponse),
		allocatedNodes:         make(map[string][]hwmgrpluginapi.AllocatedNode),
		k8sClient:              k8sClient,
		queryK8S:               false, // Default to false for backward compatibility
	}

	// Setup default mock data
	mock.setupDefaultData()

	// Create HTTP server with routes
	mux := http.NewServeMux()
	mock.setupRoutes(mux)
	mock.server = httptest.NewServer(mux)

	return mock
}

// NewMockHardwarePluginServerWithK8SQuery creates and starts a new mock hardware plugin server with Kubernetes client and queryK8S enabled
func NewMockHardwarePluginServerWithK8SQuery(k8sClient client.Client, queryK8S bool) *MockHardwarePluginServer {
	mock := &MockHardwarePluginServer{
		nodeAllocationRequests: make(map[string]*hwmgrpluginapi.NodeAllocationRequestResponse),
		allocatedNodes:         make(map[string][]hwmgrpluginapi.AllocatedNode),
		k8sClient:              k8sClient,
		queryK8S:               queryK8S,
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
			ObservedConfigTransactionId: 0, // 0 indicates transaction not observed yet
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
		if m.queryK8S && m.k8sClient != nil {
			// Query Kubernetes for actual NodeAllocationRequests
			requests, err := m.getKubernetesNodeAllocationRequests(r.Context())
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to query K8s NodeAllocationRequests: %v", err), http.StatusInternalServerError)
				return
			}
			if err := json.NewEncoder(w).Encode(requests); err != nil {
				http.Error(w, "Failed to encode response", http.StatusInternalServerError)
				return
			}
		} else {
			// Return mock data for backward compatibility
			var requests []hwmgrpluginapi.NodeAllocationRequestResponse
			for _, req := range m.nodeAllocationRequests {
				requests = append(requests, *req)
			}
			if err := json.NewEncoder(w).Encode(requests); err != nil {
				http.Error(w, "Failed to encode response", http.StatusInternalServerError)
				return
			}
		}

	case http.MethodPost:
		// Create new NodeAllocationRequest
		var request hwmgrpluginapi.NodeAllocationRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Use the ClusterId from the request as the NodeAllocationRequestID
		// This ensures each cluster gets its own unique NodeAllocationRequest
		requestID := request.ClusterId

		// Store the request
		response := &hwmgrpluginapi.NodeAllocationRequestResponse{
			NodeAllocationRequest: &request,
			Status: &hwmgrpluginapi.NodeAllocationRequestStatus{
				// No conditions initially - hardware provisioning hasn't started yet
				// This matches real hardware plugin behavior where conditions are added later
				Conditions: &[]hwmgrpluginapi.Condition{},
			},
		}
		m.nodeAllocationRequests[requestID] = response

		// Create Kubernetes NodeAllocationRequest resource if k8sClient is available and queryK8S is enabled
		if m.k8sClient != nil && m.queryK8S {
			if err := m.createKubernetesNodeAllocationRequest(r.Context(), &request, requestID); err != nil {
				// Log the error for debugging but continue
				fmt.Printf("DEBUG: Failed to create K8s NodeAllocationRequest %s: %v\n", requestID, err)
			} else {
				// Success case - log for debugging
				fmt.Printf("DEBUG: Successfully created K8s NodeAllocationRequest %s in namespace %s\n", requestID, constants.DefaultNamespace)
			}
		}

		// Don't automatically create default allocated nodes when queryK8S is enabled
		// The real allocated nodes will be created by the hardware plugin and queried from K8s
		if !m.queryK8S {
			// Only create default allocated nodes for backward compatibility when not using K8s
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
			if m.queryK8S && m.k8sClient != nil {
				// Query Kubernetes for actual AllocatedNodes
				nodes, err := m.getKubernetesAllocatedNodes(r.Context(), requestID)
				if err != nil {
					http.Error(w, fmt.Sprintf("Failed to query K8s AllocatedNodes: %v", err), http.StatusInternalServerError)
					return
				}
				if err := json.NewEncoder(w).Encode(nodes); err != nil {
					http.Error(w, "Failed to encode response", http.StatusInternalServerError)
					return
				}
			} else {
				// Return mock data for backward compatibility
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
			}
			return
		}

		// Return specific NodeAllocationRequest
		if m.queryK8S && m.k8sClient != nil {
			// Query Kubernetes for actual NodeAllocationRequest
			response, err := m.getKubernetesNodeAllocationRequest(r.Context(), requestID)
			if err != nil {
				http.Error(w, "Not found", http.StatusNotFound)
				return
			}
			if err := json.NewEncoder(w).Encode(response); err != nil {
				http.Error(w, "Failed to encode response", http.StatusInternalServerError)
				return
			}
		} else {
			// Return mock data for backward compatibility
			if response, exists := m.nodeAllocationRequests[requestID]; exists {
				// Check if we should update the status to simulate hardware provisioning completion
				// This simulates the behavior of real hardware plugins that update status when AllocatedNodes are created
				m.updateNodeAllocationRequestStatus(requestID, response)

				if err := json.NewEncoder(w).Encode(response); err != nil {
					http.Error(w, "Failed to encode response", http.StatusInternalServerError)
					return
				}
			} else {
				http.Error(w, "Not found", http.StatusNotFound)
			}
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
		if m.queryK8S && m.k8sClient != nil {
			// Query Kubernetes for all AllocatedNodes
			allNodes, err := m.getAllKubernetesAllocatedNodes(r.Context())
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to query K8s AllocatedNodes: %v", err), http.StatusInternalServerError)
				return
			}
			if err := json.NewEncoder(w).Encode(allNodes); err != nil {
				http.Error(w, "Failed to encode response", http.StatusInternalServerError)
				return
			}
		} else {
			// Return mock data for backward compatibility
			var allNodes []hwmgrpluginapi.AllocatedNode
			for _, nodes := range m.allocatedNodes {
				allNodes = append(allNodes, nodes...)
			}
			if err := json.NewEncoder(w).Encode(allNodes); err != nil {
				http.Error(w, "Failed to encode response", http.StatusInternalServerError)
				return
			}
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

// updateNodeAllocationRequestStatus simulates the status update that would occur when AllocatedNodes are created
func (m *MockHardwarePluginServer) updateNodeAllocationRequestStatus(requestID string, response *hwmgrpluginapi.NodeAllocationRequestResponse) {
	// If no conditions exist, this simulates the initial state
	if response.Status == nil || response.Status.Conditions == nil || len(*response.Status.Conditions) == 0 {
		// Check if we have Kubernetes AllocatedNode resources for this request
		if m.k8sClient != nil {
			hasAllocatedNodes := m.checkForAllocatedNodes(requestID)
			if hasAllocatedNodes {
				// Simulate the transition to Provisioned=True
				conditions := []hwmgrpluginapi.Condition{
					{
						Type:               string(hwmgmtv1alpha1.Provisioned),
						Status:             string(metav1.ConditionTrue),
						Reason:             string(hwmgmtv1alpha1.Completed),
						Message:            "Hardware provisioning completed",
						LastTransitionTime: time.Now(),
					},
				}
				if response.Status == nil {
					response.Status = &hwmgrpluginapi.NodeAllocationRequestStatus{}
				}
				response.Status.Conditions = &conditions
			}
		}
	}
}

// checkForAllocatedNodes checks if AllocatedNode resources exist for the given request
func (m *MockHardwarePluginServer) checkForAllocatedNodes(requestID string) bool {
	if m.k8sClient == nil {
		return false
	}

	// Check if any AllocatedNode resources exist for this NodeAllocationRequest
	allocatedNodeList := &pluginsv1alpha1.AllocatedNodeList{}
	err := m.k8sClient.List(context.Background(), allocatedNodeList, client.InNamespace(constants.DefaultNamespace))
	if err != nil {
		return false
	}

	for _, node := range allocatedNodeList.Items {
		if node.Spec.NodeAllocationRequest == requestID {
			return true
		}
	}
	return false
}

// createKubernetesNodeAllocationRequest creates a Kubernetes NodeAllocationRequest resource
func (m *MockHardwarePluginServer) createKubernetesNodeAllocationRequest(ctx context.Context, request *hwmgrpluginapi.NodeAllocationRequest, requestID string) error {
	// Convert the hardware plugin API request to Kubernetes NodeAllocationRequest
	k8sNodeAllocationRequest := &pluginsv1alpha1.NodeAllocationRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      requestID,
			Namespace: constants.DefaultNamespace,
			Labels: map[string]string{
				hwmgrutils.HardwarePluginLabel: hwmgrutils.Metal3HardwarePluginID,
			},
		},
		Spec: pluginsv1alpha1.NodeAllocationRequestSpec{
			ClusterId:          request.ClusterId,
			BootInterfaceLabel: request.BootInterfaceLabel,
		},
	}

	// Convert NodeGroup data
	if request.NodeGroup != nil {
		for _, group := range request.NodeGroup {
			k8sNodeGroup := pluginsv1alpha1.NodeGroup{
				Size: group.NodeGroupData.Size,
				NodeGroupData: hwmgmtv1alpha1.NodeGroupData{
					Name:             group.NodeGroupData.Name,
					Role:             group.NodeGroupData.Role,
					HwProfile:        group.NodeGroupData.HwProfile,
					ResourcePoolId:   group.NodeGroupData.ResourceGroupId,
					ResourceSelector: group.NodeGroupData.ResourceSelector,
				},
			}
			k8sNodeAllocationRequest.Spec.NodeGroup = append(k8sNodeAllocationRequest.Spec.NodeGroup, k8sNodeGroup)
		}
	}

	// Create the Kubernetes resource
	if err := m.k8sClient.Create(ctx, k8sNodeAllocationRequest); err != nil {
		return fmt.Errorf("failed to create NodeAllocationRequest: %w", err)
	}
	return nil
}

// getKubernetesNodeAllocationRequests retrieves all NodeAllocationRequests from Kubernetes
func (m *MockHardwarePluginServer) getKubernetesNodeAllocationRequests(ctx context.Context) ([]hwmgrpluginapi.NodeAllocationRequestResponse, error) {
	narList := &pluginsv1alpha1.NodeAllocationRequestList{}
	err := m.k8sClient.List(ctx, narList, client.InNamespace(constants.DefaultNamespace))
	if err != nil {
		return nil, fmt.Errorf("failed to list NodeAllocationRequests: %w", err)
	}

	var responses []hwmgrpluginapi.NodeAllocationRequestResponse
	for _, nar := range narList.Items {
		response := m.convertK8sNARToPluginAPI(&nar)
		responses = append(responses, response)
	}
	return responses, nil
}

// getKubernetesNodeAllocationRequest retrieves a specific NodeAllocationRequest from Kubernetes
func (m *MockHardwarePluginServer) getKubernetesNodeAllocationRequest(ctx context.Context, requestID string) (*hwmgrpluginapi.NodeAllocationRequestResponse, error) {
	nar := &pluginsv1alpha1.NodeAllocationRequest{}
	err := m.k8sClient.Get(ctx, client.ObjectKey{Name: requestID, Namespace: constants.DefaultNamespace}, nar)
	if err != nil {
		return nil, fmt.Errorf("failed to get NodeAllocationRequest: %w", err)
	}

	response := m.convertK8sNARToPluginAPI(nar)
	return &response, nil
}

// getKubernetesAllocatedNodes retrieves AllocatedNodes for a specific NodeAllocationRequest from Kubernetes
func (m *MockHardwarePluginServer) getKubernetesAllocatedNodes(ctx context.Context, requestID string) ([]hwmgrpluginapi.AllocatedNode, error) {
	allocatedNodeList := &pluginsv1alpha1.AllocatedNodeList{}
	err := m.k8sClient.List(ctx, allocatedNodeList, client.InNamespace(constants.DefaultNamespace))
	if err != nil {
		return nil, fmt.Errorf("failed to list AllocatedNodes: %w", err)
	}

	var nodes []hwmgrpluginapi.AllocatedNode
	for _, allocatedNode := range allocatedNodeList.Items {
		if allocatedNode.Spec.NodeAllocationRequest == requestID {
			node := m.convertK8sAllocatedNodeToPluginAPI(&allocatedNode)
			nodes = append(nodes, node)
		}
	}
	return nodes, nil
}

// getAllKubernetesAllocatedNodes retrieves all AllocatedNodes from Kubernetes
func (m *MockHardwarePluginServer) getAllKubernetesAllocatedNodes(ctx context.Context) ([]hwmgrpluginapi.AllocatedNode, error) {
	allocatedNodeList := &pluginsv1alpha1.AllocatedNodeList{}
	err := m.k8sClient.List(ctx, allocatedNodeList, client.InNamespace(constants.DefaultNamespace))
	if err != nil {
		return nil, fmt.Errorf("failed to list AllocatedNodes: %w", err)
	}

	var nodes []hwmgrpluginapi.AllocatedNode
	for _, allocatedNode := range allocatedNodeList.Items {
		node := m.convertK8sAllocatedNodeToPluginAPI(&allocatedNode)
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// convertK8sNARToPluginAPI converts Kubernetes NodeAllocationRequest to plugin API format
func (m *MockHardwarePluginServer) convertK8sNARToPluginAPI(k8sNAR *pluginsv1alpha1.NodeAllocationRequest) hwmgrpluginapi.NodeAllocationRequestResponse {
	// Convert NodeGroups
	var nodeGroups []hwmgrpluginapi.NodeGroup
	for _, group := range k8sNAR.Spec.NodeGroup {
		nodeGroup := hwmgrpluginapi.NodeGroup{
			NodeGroupData: hwmgrpluginapi.NodeGroupData{
				Name:             group.NodeGroupData.Name,
				Role:             group.NodeGroupData.Role,
				HwProfile:        group.NodeGroupData.HwProfile,
				ResourceGroupId:  group.NodeGroupData.ResourcePoolId,
				ResourceSelector: group.NodeGroupData.ResourceSelector,
				Size:             group.Size,
			},
		}
		nodeGroups = append(nodeGroups, nodeGroup)
	}

	// Convert conditions
	var conditions []hwmgrpluginapi.Condition
	for _, condition := range k8sNAR.Status.Conditions {
		hwCondition := hwmgrpluginapi.Condition{
			Type:               condition.Type,
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastTransitionTime: condition.LastTransitionTime.Time,
		}
		conditions = append(conditions, hwCondition)
	}

	return hwmgrpluginapi.NodeAllocationRequestResponse{
		NodeAllocationRequest: &hwmgrpluginapi.NodeAllocationRequest{
			ClusterId:           k8sNAR.Spec.ClusterId,
			Site:                k8sNAR.Spec.Site,
			BootInterfaceLabel:  k8sNAR.Spec.BootInterfaceLabel,
			ConfigTransactionId: k8sNAR.Spec.ConfigTransactionId,
			NodeGroup:           nodeGroups,
		},
		Status: &hwmgrpluginapi.NodeAllocationRequestStatus{
			Conditions:                  &conditions,
			ObservedConfigTransactionId: k8sNAR.Status.ObservedConfigTransactionId,
		},
	}
}

// convertK8sAllocatedNodeToPluginAPI converts Kubernetes AllocatedNode to plugin API format
func (m *MockHardwarePluginServer) convertK8sAllocatedNodeToPluginAPI(k8sNode *pluginsv1alpha1.AllocatedNode) hwmgrpluginapi.AllocatedNode {
	// Convert BMC
	var bmc hwmgrpluginapi.BMC
	if k8sNode.Status.BMC != nil {
		bmc = hwmgrpluginapi.BMC{
			Address:         k8sNode.Status.BMC.Address,
			CredentialsName: k8sNode.Status.BMC.CredentialsName,
		}
	}

	// Convert interfaces
	var interfaces []hwmgrpluginapi.Interface
	for _, iface := range k8sNode.Status.Interfaces {
		if iface != nil {
			hwInterface := hwmgrpluginapi.Interface{
				Name:       iface.Name,
				Label:      iface.Label,
				MacAddress: iface.MACAddress,
			}
			interfaces = append(interfaces, hwInterface)
		}
	}

	// Convert conditions
	var conditions []hwmgrpluginapi.Condition
	for _, condition := range k8sNode.Status.Conditions {
		hwCondition := hwmgrpluginapi.Condition{
			Type:               condition.Type,
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastTransitionTime: condition.LastTransitionTime.Time,
		}
		conditions = append(conditions, hwCondition)
	}

	return hwmgrpluginapi.AllocatedNode{
		Id:                  k8sNode.Spec.HwMgrNodeId,
		GroupName:           k8sNode.Spec.GroupName,
		HwProfile:           k8sNode.Spec.HwProfile,
		ConfigTransactionId: 0, // Not directly available in K8s spec
		Bmc:                 bmc,
		Interfaces:          interfaces,
		Status: hwmgrpluginapi.AllocatedNodeStatus{
			Conditions: &conditions,
		},
	}
}
