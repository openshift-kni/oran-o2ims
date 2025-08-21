/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Generated-By: Cursor/claude-4-sonnet
*/

// nolint: wrapcheck
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
)

// debugTransport logs HTTP requests for debugging OAuth issues
type debugTransport struct {
	base http.RoundTripper
}

func (t *debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path != "" && req.Method == http.MethodPost {
		klog.V(3).Infof("OAuth request to %s", req.URL.String())
		if req.Body != nil {
			body, err := io.ReadAll(req.Body)
			if err == nil {
				klog.V(3).Infof("OAuth request body: %s", string(body))
				// Restore the body for the actual request
				req.Body = io.NopCloser(strings.NewReader(string(body)))
			}
		}
	}
	return t.base.RoundTrip(req)
}

// InventoryConfig holds the configuration for the inventory module
type InventoryConfig struct {
	ServerURL    string
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scopes       []string
	// TLS configuration
	ClientCertFile string
	ClientKeyFile  string
	CACertFile     string
	TLSSkipVerify  bool
	// Service account token configuration
	ServiceAccountName      string
	ServiceAccountNamespace string
	// Kubernetes client configuration (for token creation)
	KubernetesConfig *rest.Config
	// Retry configuration
	MaxRetries   int
	RetryDelayMs int // Initial delay in milliseconds
}

// InventoryClient provides access to O2IMS inventory resources
type InventoryClient struct {
	httpClient *http.Client
	config     *InventoryConfig
	baseURL    string
	maxRetries int
	retryDelay time.Duration
}

// InventoryResource represents a resource from the inventory API
type InventoryResource struct {
	ResourceID   string                 `json:"resourceId"`
	ResourceType string                 `json:"resourceTypeId"`
	Description  string                 `json:"description"`
	Status       string                 `json:"status"`
	Extensions   map[string]interface{} `json:"extensions"`
	CreatedAt    time.Time              `json:"createdAt"`
}

// ResourcePool represents a resource pool from the inventory API
type ResourcePool struct {
	ResourcePoolID string                 `json:"resourcePoolId"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	Extensions     map[string]interface{} `json:"extensions"`
}

// NodeCluster represents a node cluster from the inventory API
type NodeCluster struct {
	Name              string                 `json:"name"`
	NodeClusterID     string                 `json:"nodeClusterId"`
	NodeClusterTypeID string                 `json:"nodeClusterTypeId"`
	Extensions        map[string]interface{} `json:"extensions"`
}

// GetSite extracts site information from the resource pool
func (rp *ResourcePool) GetSite() string {
	// Try to get site from extensions
	if rp.Extensions != nil {
		if site, ok := rp.Extensions["site"].(string); ok && site != "" {
			return site
		}
		if location, ok := rp.Extensions["location"].(string); ok && location != "" {
			return location
		}
		if globalLocationId, ok := rp.Extensions["globalLocationId"].(string); ok && globalLocationId != "" {
			return globalLocationId
		}
	}

	// Fallback: try to extract from name or description
	if strings.Contains(rp.Name, "-") {
		parts := strings.Split(rp.Name, "-")
		if len(parts) > 1 {
			return parts[0] // Assume first part is site
		}
	}

	return StringUnknown
}

// GetPoolName returns a formatted pool name
func (rp *ResourcePool) GetPoolName() string {
	if rp.Name != "" {
		return rp.Name
	}
	return rp.ResourcePoolID
}

// ToRuntimeObject converts a NodeCluster to a runtime.Object for use with formatters
func (nc *NodeCluster) ToRuntimeObject() runtime.Object {
	return &NodeClusterObject{
		NodeCluster: *nc,
	}
}

// NewInventoryClient creates a new inventory client with OAuth or service account token authentication
func NewInventoryClient(config *InventoryConfig) (*InventoryClient, error) {
	if config.ServerURL == "" {
		return nil, fmt.Errorf("inventory server URL is required")
	}

	// Check if OAuth is configured (all OAuth fields must be present)
	useOAuth := config.TokenURL != "" && config.ClientID != "" && config.ClientSecret != ""
	if !useOAuth {
		klog.V(1).Info("OAuth not fully configured, falling back to service account token authentication")
	}

	// Create TLS configuration
	tlsConfig, err := createTLSConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create TLS config: %w", err)
	}

	// Create HTTP transport with TLS configuration
	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}

	var httpClient *http.Client

	if useOAuth {
		// Set up OAuth2 client credentials flow with custom transport
		oauthConfig := &clientcredentials.Config{
			ClientID:     config.ClientID,
			ClientSecret: config.ClientSecret,
			TokenURL:     config.TokenURL,
			Scopes:       config.Scopes,
		}

		klog.V(2).Infof("Creating OAuth client with token URL: %s, scopes: %v", config.TokenURL, config.Scopes)
		klog.V(2).Infof("OAuth config scopes field: %#v", oauthConfig.Scopes)

		// Create context with custom HTTP client for OAuth
		ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{
			Transport: transport,
		})

		// Add debug transport to see what's actually being sent
		if klog.V(3).Enabled() {
			debugTransport := &debugTransport{base: transport}
			ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{
				Transport: debugTransport,
			})
		}

		httpClient = oauthConfig.Client(ctx)
	} else {
		// Use service account token authentication
		tokenSource, err := createServiceAccountTokenSource(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create service account token source: %w", err)
		}

		// Create HTTP client with token source
		oauthTransport := &oauth2.Transport{Source: tokenSource, Base: transport}

		// Add debug transport to see what's actually being sent for service account tokens
		if klog.V(3).Enabled() {
			debugTransport := &debugTransport{base: oauthTransport}
			httpClient = &http.Client{
				Transport: debugTransport,
			}
		} else {
			httpClient = &http.Client{
				Transport: oauthTransport,
			}
		}

		klog.V(2).Infof("Created service account token client for account %s in namespace %s",
			config.ServiceAccountName, config.ServiceAccountNamespace)
	}

	// Set default retry values if not configured
	maxRetries := config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3 // Default to 3 retries
	}

	retryDelayMs := config.RetryDelayMs
	if retryDelayMs <= 0 {
		retryDelayMs = 1000 // Default to 1 second initial delay
	}

	return &InventoryClient{
		httpClient: httpClient,
		config:     config,
		baseURL:    config.ServerURL + constants.O2IMSInventoryBaseURL,
		maxRetries: maxRetries,
		retryDelay: time.Duration(retryDelayMs) * time.Millisecond,
	}, nil
}

// createTLSConfig creates a TLS configuration with optional client certificates and CA bundle
func createTLSConfig(config *InventoryConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: config.TLSSkipVerify, // nolint: gosec // This is intentional when user requests TLS skip verification
	}

	if config.TLSSkipVerify {
		klog.V(1).Info("TLS server certificate verification disabled (insecure)")
	}

	// Load client certificate and key if provided
	if config.ClientCertFile != "" && config.ClientKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(config.ClientCertFile, config.ClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
		klog.V(1).Infof("Loaded client certificate from %s", config.ClientCertFile)
	}

	// Load CA certificate bundle if provided (only when not skipping verification)
	if config.CACertFile != "" && !config.TLSSkipVerify {
		caCert, err := os.ReadFile(config.CACertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate file: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsConfig.RootCAs = caCertPool
		klog.V(1).Infof("Loaded CA certificate bundle from %s", config.CACertFile)
	} else if config.CACertFile != "" && config.TLSSkipVerify {
		klog.V(1).Info("CA certificate file provided but ignored due to TLS skip verification")
	}

	return tlsConfig, nil
}

// createServiceAccountTokenSource creates a token source for service account authentication
func createServiceAccountTokenSource(config *InventoryConfig) (oauth2.TokenSource, error) {
	if config.KubernetesConfig == nil {
		return nil, fmt.Errorf("kubernetes config is required for service account token creation")
	}

	// Create Kubernetes client
	clientset, err := kubernetes.NewForConfig(config.KubernetesConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Create token source that uses Kubernetes TokenRequest API
	tokenSource := &serviceAccountTokenSource{
		clientset:   clientset,
		namespace:   config.ServiceAccountNamespace,
		accountName: config.ServiceAccountName,
		mutex:       &sync.Mutex{},
	}

	// Test the token source by getting an initial token
	initialToken, err := tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to create initial service account token for '%s' in namespace '%s': %w. "+
			"Ensure the service account exists and you have permission to create tokens, "+
			"or provide OAuth credentials (--oauth-token-url, --oauth-client-id, --oauth-client-secret)",
			config.ServiceAccountName, config.ServiceAccountNamespace, err)
	}

	klog.V(2).Infof("Successfully created service account token, token type: %s, expires: %v",
		initialToken.TokenType, initialToken.Expiry)

	// Check if service account exists and provide helpful guidance
	if klog.V(1).Enabled() {
		checkServiceAccountPermissions(clientset, config.ServiceAccountNamespace, config.ServiceAccountName)
	}

	klog.V(2).Infof("Successfully created service account token source for account %s in namespace %s",
		config.ServiceAccountName, config.ServiceAccountNamespace)

	return tokenSource, nil
}

// serviceAccountTokenSource implements oauth2.TokenSource using Kubernetes TokenRequest API
type serviceAccountTokenSource struct {
	clientset   kubernetes.Interface
	namespace   string
	accountName string
	mutex       *sync.Mutex
	token       *oauth2.Token
}

// Token implements oauth2.TokenSource.Token()
func (ts *serviceAccountTokenSource) Token() (*oauth2.Token, error) {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()

	// Check if we have a valid cached token
	if ts.token != nil && ts.token.Valid() {
		klog.V(4).Infof("Using cached service account token for %s/%s, expires at %v",
			ts.namespace, ts.accountName, ts.token.Expiry)
		return ts.token, nil
	}

	// Create a new token using Kubernetes TokenRequest API
	timeout := int64(24 * 60 * 60)
	tokenRequest := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			ExpirationSeconds: &timeout,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := ts.clientset.CoreV1().ServiceAccounts(ts.namespace).CreateToken(
		ctx, ts.accountName, tokenRequest, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create service account token: %w", err)
	}

	// Calculate expiration time (subtract 5 minutes for safety margin)
	expirationTime := result.Status.ExpirationTimestamp.Time.Add(-5 * time.Minute)

	// Create oauth2.Token
	token := &oauth2.Token{
		AccessToken: result.Status.Token,
		TokenType:   "Bearer",
		Expiry:      expirationTime,
	}

	ts.token = token

	klog.V(3).Infof("Created new service account token for %s/%s, expires at %v",
		ts.namespace, ts.accountName, expirationTime)

	// Log first and last 10 characters of token for debugging (never log full token)
	if klog.V(4).Enabled() && len(result.Status.Token) > 20 {
		klog.V(4).Infof("Token preview: %s...%s",
			result.Status.Token[:10], result.Status.Token[len(result.Status.Token)-10:])
	}

	return token, nil
}

// checkServiceAccountPermissions provides helpful guidance about service account configuration
func checkServiceAccountPermissions(clientset kubernetes.Interface, namespace, accountName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Check if service account exists
	_, err := clientset.CoreV1().ServiceAccounts(namespace).Get(ctx, accountName, metav1.GetOptions{})
	if err != nil {
		klog.V(1).Infof("Warning: Service account '%s' in namespace '%s' may not exist or is not accessible: %v",
			accountName, namespace, err)
		klog.V(1).Infof("Note: If using service account authentication, ensure the service account exists and has appropriate RBAC permissions for the O2IMS API")
		klog.V(1).Infof("Example: The service account should have 'role:o2ims-reader' permissions or equivalent")
		return
	}

	klog.V(2).Infof("Service account '%s' found in namespace '%s'", accountName, namespace)
	klog.V(1).Infof("Note: Ensure the service account '%s' has appropriate RBAC permissions for the O2IMS API (e.g., 'role:o2ims-reader')", accountName)
}

// retryHTTPRequest performs an HTTP request with exponential backoff retry logic
func (c *InventoryClient) retryHTTPRequest(ctx context.Context, requestFunc func() (*http.Response, error)) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		// Check if context is cancelled
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		resp, err := requestFunc()
		//nolint:gocritic // if-else chain is more readable than switch for this error handling pattern
		if err != nil {
			lastErr = err
			klog.V(2).Infof("HTTP request attempt %d/%d failed: %v", attempt+1, c.maxRetries+1, err)
		} else if c.isRetryableStatusCode(resp.StatusCode) {
			resp.Body.Close() // Close the response body before retrying
			lastErr = fmt.Errorf("received retryable status code: %d", resp.StatusCode)
			klog.V(2).Infof("HTTP request attempt %d/%d got retryable status %d", attempt+1, c.maxRetries+1, resp.StatusCode)
		} else {
			// Success or non-retryable error
			return resp, nil
		}

		// Don't sleep after the last attempt
		if attempt < c.maxRetries {
			// Exponential backoff with jitter: delay * 2^attempt + random(0, delay)
			// Cap the shift to prevent overflow (max 2^10 = 1024x multiplier)
			shiftAmount := attempt
			if shiftAmount > 10 {
				shiftAmount = 10
			}
			backoffDelay := c.retryDelay * time.Duration(1<<shiftAmount)
			maxJitter := c.retryDelay / 2
			jitter := time.Duration(float64(maxJitter) * (0.5 + 0.5*float64(attempt%100)/100.0))
			totalDelay := backoffDelay + jitter

			klog.V(2).Infof("Retrying in %v (next attempt %d/%d)", totalDelay, attempt+2, c.maxRetries+1)

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(totalDelay):
			}
		}
	}

	return nil, fmt.Errorf("max retries (%d) exceeded, last error: %w", c.maxRetries, lastErr)
}

// isRetryableStatusCode determines if an HTTP status code should trigger a retry
func (c *InventoryClient) isRetryableStatusCode(statusCode int) bool {
	switch statusCode {
	case http.StatusInternalServerError, // 500
		http.StatusBadGateway,         // 502
		http.StatusServiceUnavailable, // 503
		http.StatusGatewayTimeout:     // 504
		return true
	default:
		return false
	}
}

// GetResourcePools fetches all resource pools from the inventory
func (c *InventoryClient) GetResourcePools(ctx context.Context) ([]ResourcePool, error) {
	klog.V(2).Info("Fetching resource pools from inventory API")

	url := c.baseURL + "/resourcePools"

	resp, err := c.retryHTTPRequest(ctx, func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		return c.httpClient.Do(req)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			klog.V(2).Infof("Failed to close response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("inventory API returned status %d", resp.StatusCode)
	}

	var pools []ResourcePool
	if err := json.NewDecoder(resp.Body).Decode(&pools); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return pools, nil
}

// GetNodeClusters fetches all node clusters from the inventory
func (c *InventoryClient) GetNodeClusters(ctx context.Context) ([]NodeCluster, error) {
	klog.V(2).Info("Fetching node clusters from inventory API")

	// Node clusters use a different API path: /o2ims-infrastructureCluster/v1 instead of /o2ims-infrastructureInventory/v1
	clusterBaseURL := strings.Replace(c.baseURL, constants.O2IMSInventoryBaseURL, constants.O2IMSClusterBaseURL, 1)
	url := clusterBaseURL + "/nodeClusters"

	resp, err := c.retryHTTPRequest(ctx, func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		return c.httpClient.Do(req)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			klog.V(2).Infof("Failed to close response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("inventory API returned status %d", resp.StatusCode)
	}

	var clusters []NodeCluster
	if err := json.NewDecoder(resp.Body).Decode(&clusters); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return clusters, nil
}

// GetResources fetches all resources from a specific resource pool
func (c *InventoryClient) GetResources(ctx context.Context, resourcePoolID string) ([]InventoryResource, error) {
	klog.V(2).Infof("Fetching resources from resource pool: %s", resourcePoolID)

	url := c.baseURL + "/resourcePools/" + resourcePoolID + "/resources"

	resp, err := c.retryHTTPRequest(ctx, func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		return c.httpClient.Do(req)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			klog.V(2).Infof("Failed to close response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("inventory API returned status %d", resp.StatusCode)
	}

	var resources []InventoryResource
	if err := json.NewDecoder(resp.Body).Decode(&resources); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Set creation time since API doesn't provide it
	for i := range resources {
		resources[i].CreatedAt = time.Now()
		// Extract status from extensions if available
		if resources[i].Extensions != nil {
			if status, ok := resources[i].Extensions["status"]; ok {
				if statusStr, ok := status.(string); ok {
					resources[i].Status = statusStr
				}
			}
		}
	}

	return resources, nil
}

// GetAllResources fetches all resources from all resource pools
func (c *InventoryClient) GetAllResources(ctx context.Context) ([]InventoryResource, error) {
	// First get all resource pools
	pools, err := c.GetResourcePools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource pools: %w", err)
	}

	var allResources []InventoryResource

	// For each pool, get its resources
	for _, pool := range pools {
		resources, err := c.GetResources(ctx, pool.ResourcePoolID)
		if err != nil {
			klog.Errorf("Failed to get resources from pool %s: %v", pool.ResourcePoolID, err)
			continue
		}
		allResources = append(allResources, resources...)
	}

	klog.V(1).Infof("Fetched %d resources from %d resource pools", len(allResources), len(pools))
	return allResources, nil
}

// GetAllResourcePools fetches all resource pools as inventory items
func (c *InventoryClient) GetAllResourcePools(ctx context.Context) ([]ResourcePool, error) {
	pools, err := c.GetResourcePools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource pools: %w", err)
	}

	klog.V(1).Infof("Fetched %d resource pools", len(pools))
	return pools, nil
}

// GetAllNodeClusters fetches all node clusters as inventory items
func (c *InventoryClient) GetAllNodeClusters(ctx context.Context) ([]NodeCluster, error) {
	clusters, err := c.GetNodeClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get node clusters: %w", err)
	}

	klog.V(1).Infof("Fetched %d node clusters", len(clusters))
	return clusters, nil
}

// ToRuntimeObject converts an InventoryResource to a runtime.Object for use with formatters
func (ir *InventoryResource) ToRuntimeObject() runtime.Object {
	return &InventoryResourceObject{
		Resource: *ir,
	}
}

// InventoryResourceObject is a runtime.Object wrapper for InventoryResource
type InventoryResourceObject struct {
	Resource InventoryResource
}

// DeepCopyObject implements runtime.Object
func (o *InventoryResourceObject) DeepCopyObject() runtime.Object {
	return &InventoryResourceObject{
		Resource: o.Resource,
	}
}

// GetObjectKind implements runtime.Object
func (o *InventoryResourceObject) GetObjectKind() schema.ObjectKind {
	return &InventoryObjectKind{}
}

// InventoryObjectKind implements schema.ObjectKind for inventory resources
type InventoryObjectKind struct{}

// SetGroupVersionKind implements schema.ObjectKind
func (k *InventoryObjectKind) SetGroupVersionKind(gvk schema.GroupVersionKind) {}

// GroupVersionKind implements schema.ObjectKind
func (k *InventoryObjectKind) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "inventory.o2ims.io",
		Version: "v1",
		Kind:    "Resource",
	}
}

// ToRuntimeObject converts a ResourcePool to a runtime.Object for use with formatters
func (rp *ResourcePool) ToRuntimeObject() runtime.Object {
	return &ResourcePoolObject{
		ResourcePool: *rp,
	}
}

// ResourcePoolObject is a runtime.Object wrapper for ResourcePool
type ResourcePoolObject struct {
	ResourcePool ResourcePool
}

// DeepCopyObject implements runtime.Object
func (o *ResourcePoolObject) DeepCopyObject() runtime.Object {
	return &ResourcePoolObject{
		ResourcePool: o.ResourcePool,
	}
}

// GetObjectKind implements runtime.Object
func (o *ResourcePoolObject) GetObjectKind() schema.ObjectKind {
	return &ResourcePoolObjectKind{}
}

// ResourcePoolObjectKind implements schema.ObjectKind for resource pools
type ResourcePoolObjectKind struct{}

// SetGroupVersionKind implements schema.ObjectKind
func (k *ResourcePoolObjectKind) SetGroupVersionKind(gvk schema.GroupVersionKind) {}

// GroupVersionKind implements schema.ObjectKind
func (k *ResourcePoolObjectKind) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "inventory.o2ims.io",
		Version: "v1",
		Kind:    "ResourcePool",
	}
}

// NodeClusterObject is a runtime.Object wrapper for NodeCluster
type NodeClusterObject struct {
	NodeCluster NodeCluster
}

// DeepCopyObject implements runtime.Object
func (o *NodeClusterObject) DeepCopyObject() runtime.Object {
	return &NodeClusterObject{
		NodeCluster: o.NodeCluster,
	}
}

// GetObjectKind implements runtime.Object
func (o *NodeClusterObject) GetObjectKind() schema.ObjectKind {
	return &NodeClusterObjectKind{}
}

// NodeClusterObjectKind implements schema.ObjectKind for node clusters
type NodeClusterObjectKind struct{}

// SetGroupVersionKind implements schema.ObjectKind
func (k *NodeClusterObjectKind) SetGroupVersionKind(gvk schema.GroupVersionKind) {}

// GroupVersionKind implements schema.ObjectKind
func (k *NodeClusterObjectKind) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "inventory.o2ims.io",
		Version: "v1",
		Kind:    "NodeCluster",
	}
}
