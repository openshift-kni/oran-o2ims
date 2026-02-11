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
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
)

// Config holds all configuration for the watcher
type Config struct {
	CRDTypes        []string
	AllNamespaces   bool
	Namespace       string
	OutputFormat    string
	Watch           bool
	RefreshInterval int
	LogLevel        int
	Kubeconfig      string
	// Inventory module configuration
	EnableInventory    bool
	InventoryServerURL string
	OAuthTokenURL      string
	OAuthClientID      string
	OAuthClientSecret  string
	OAuthScopes        []string
	// TLS certificate configuration
	ClientCertFile string
	ClientKeyFile  string
	CACertFile     string
	TLSSkipVerify  bool
	// Service account token configuration
	ServiceAccountName      string
	ServiceAccountNamespace string
	// Retry configuration
	InventoryMaxRetries   int
	InventoryRetryDelayMs int
	// Refresh configuration
	InventoryRefreshInterval int // Interval in seconds for refreshing inventory data from O2IMS API
	// Output formatting configuration
	UseASCII bool // Use ASCII characters instead of Unicode for table formatting
}

var (
	config = &Config{
		OutputFormat:             "table",
		LogLevel:                 1,
		AllNamespaces:            true,
		RefreshInterval:          2,
		InventoryRefreshInterval: 120, // Default: refresh inventory data every 2 minutes
	}

	// Available CRD types that can be watched
	availableCRDs = map[string]string{
		CRDTypeProvisioningRequests:   "clcm.openshift.io/v1alpha1",
		CRDTypeNodeAllocationRequests: "plugins.clcm.openshift.io/v1alpha1",
		CRDTypeAllocatedNodes:         "plugins.clcm.openshift.io/v1alpha1",
		CRDTypeBareMetalHosts:         "metal3.io/v1alpha1",
		CRDTypeHostFirmwareComponents: "metal3.io/v1alpha1",
		CRDTypeHostFirmwareSettings:   "metal3.io/v1alpha1",
	}
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "crd-watcher",
		Short: "Watch O-Cloud Manager provisioning CRDs for changes",
		Long: `A standalone tool that watches Custom Resource Definitions (CRDs) for O-Cloud Manager provisioning.
By default, watches all namespaces for better visibility across the cluster.

This tool can monitor the following CRD types:
- provisioningrequests (ProvisioningRequest)
- nodeallocationrequests (NodeAllocationRequest)
- allocatednodes (AllocatedNode)

Examples:
  # Watch all CRDs across all namespaces (default behavior)
  crd-watcher

  # Watch specific CRDs across all namespaces
  crd-watcher --crds provisioningrequests,nodeallocationrequests

  # Watch in a specific namespace only
  crd-watcher --namespace my-namespace

  # Watch with JSON output format
  crd-watcher --output json

  # Enable real-time dashboard mode
  crd-watcher --watch`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCommand()
		},
	}

	addFlags(rootCmd.Flags())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func addFlags(flags *pflag.FlagSet) {
	flags.StringVar(&config.Kubeconfig, "kubeconfig", "", "Path to kubeconfig file (default: use in-cluster config, $KUBECONFIG env var, or ~/.kube/config)")
	flags.StringVarP(&config.Namespace, "namespace", "n", "", "Namespace to watch (overrides --all-namespaces when specified)")
	flags.BoolVar(&config.AllNamespaces, "all-namespaces", true, "Watch resources across all namespaces")
	flags.StringVarP(&config.OutputFormat, "output", "o", "table", "Output format: table, json, yaml")
	flags.StringSliceVar(&config.CRDTypes, "crds", []string{CRDTypeProvisioningRequests, CRDTypeNodeAllocationRequests, CRDTypeAllocatedNodes, CRDTypeBareMetalHosts, CRDTypeHostFirmwareComponents, CRDTypeHostFirmwareSettings}, fmt.Sprintf("CRD types to watch (comma-separated). Available: %s", strings.Join(getAvailableCRDNames(), ", ")))
	flags.IntVarP(&config.LogLevel, "log-level", "v", 0, "Log level (0-4)")
	flags.BoolVarP(&config.Watch, "watch", "w", false, "Enable real-time screen updates (live dashboard mode)")
	flags.IntVar(&config.RefreshInterval, "refresh-interval", 2, "Screen refresh interval in seconds during inactivity (watch mode only)")
	flags.IntVar(&config.InventoryRefreshInterval, "inventory-refresh-interval", 120, "Inventory data refresh interval in seconds (0 to disable periodic refresh)")
	flags.BoolVar(&config.UseASCII, "ascii", false, "Use ASCII characters instead of Unicode for table formatting")

	// Inventory module flags
	flags.BoolVar(&config.EnableInventory, "enable-inventory", false, "Enable inventory module to fetch resources from O2IMS API")
	flags.StringVar(&config.InventoryServerURL, "inventory-server", "", "O2IMS Inventory server base URL (e.g., https://o2ims.example.com)")
	flags.StringVar(&config.OAuthTokenURL, "oauth-token-url", "", "OAuth token endpoint URL")
	flags.StringVar(&config.OAuthClientID, "oauth-client-id", "", "OAuth client ID")
	flags.StringVar(&config.OAuthClientSecret, "oauth-client-secret", "", "OAuth client secret")
	flags.StringSliceVar(&config.OAuthScopes, "oauth-scopes", []string{"role:o2ims-reader"}, "OAuth scopes to request")

	// TLS certificate flags for inventory module
	flags.StringVar(&config.ClientCertFile, "tls-cert", "", "Client certificate file for mutual TLS authentication")
	flags.StringVar(&config.ClientKeyFile, "tls-key", "", "Client private key file for mutual TLS authentication")
	flags.StringVar(&config.CACertFile, "tls-cacert", "", "CA certificate bundle file for server verification")
	flags.BoolVar(&config.TLSSkipVerify, "tls-skip-verify", false, "Skip TLS server certificate verification (insecure)")

	// Service account token flags for inventory module
	flags.StringVar(&config.ServiceAccountName, "service-account-name", "test-client", "Service account name for token authentication (used when OAuth is not configured)")
	flags.StringVar(&config.ServiceAccountNamespace, "service-account-namespace", "oran-o2ims", "Service account namespace for token authentication")

	// Retry configuration flags for inventory module
	flags.IntVar(&config.InventoryMaxRetries, "inventory-max-retries", 3, "Maximum number of retries for inventory API requests")
	flags.IntVar(&config.InventoryRetryDelayMs, "inventory-retry-delay", 1000, "Initial retry delay in milliseconds for inventory API requests")
}

func getAvailableCRDNames() []string {
	names := make([]string, 0, len(availableCRDs))
	for name := range availableCRDs {
		names = append(names, name)
	}
	return names
}

func runCommand() error {
	// Set up logging
	klog.InitFlags(nil)
	if err := flag.Set("v", fmt.Sprintf("%d", config.LogLevel)); err != nil {
		return fmt.Errorf("failed to set log level: %w", err)
	}

	// In watch mode, suppress klog stderr output to prevent TUI corruption
	// Only FATAL logs will go to stderr, preventing transient errors from persisting on screen
	if config.Watch {
		if err := flag.Set("stderrthreshold", "FATAL"); err != nil {
			return fmt.Errorf("failed to set stderr threshold: %w", err)
		}
	}

	// If no CRDs specified, watch all available ones
	if len(config.CRDTypes) == 0 {
		config.CRDTypes = getAvailableCRDNames()
	}

	// Validate CRD types
	for _, crdType := range config.CRDTypes {
		if _, exists := availableCRDs[crdType]; !exists {
			return fmt.Errorf("unknown CRD type: %s. Available types: %s", crdType, strings.Join(getAvailableCRDNames(), ", "))
		}
	}

	// Process OAuth scopes - handle space-separated scopes in a single string
	processedScopes := processOAuthScopes(config.OAuthScopes)
	config.OAuthScopes = processedScopes

	// Log processed scopes for debugging
	if config.EnableInventory && len(config.OAuthScopes) > 0 {
		klog.V(1).Infof("Using OAuth scopes: %v", config.OAuthScopes)
	}

	// Create Kubernetes client
	restConfig, err := createK8sConfig()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	// Create scheme and register types
	scheme, err := createScheme()
	if err != nil {
		return err
	}

	// Create context for graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	watcher := NewCRDWatcher(clientset, restConfig, scheme, config)

	// Ensure cleanup happens on exit (including Ctrl+C)
	defer func() {
		// Cleanup the watcher (stops inventory refresh timer)
		watcher.Cleanup()

		// Cleanup the TUI formatter
		if tuiFormatter, ok := watcher.GetFormatter().(*TUIFormatter); ok {
			tuiFormatter.Cleanup()
		}
	}()

	if config.Watch {
		// Watch mode - events are displayed in real-time
		return watcher.Start(ctx)
	} else {
		// Non-watch mode - list existing resources, sort them, and display once
		return watcher.ListAndDisplay(ctx)
	}
}

// processOAuthScopes processes OAuth scopes to handle space-separated values in strings
func processOAuthScopes(scopes []string) []string {
	klog.V(2).Infof("Input OAuth scopes: %v", scopes)

	var processedScopes []string

	for i, scope := range scopes {
		klog.V(2).Infof("Processing scope %d: '%s'", i, scope)

		// Split each scope string by spaces to handle cases like "scope=profile role:o2ims-admin openid"
		splitScopes := strings.Fields(scope)
		klog.V(2).Infof("Split into %d parts: %v", len(splitScopes), splitScopes)

		for j, splitScope := range splitScopes {
			splitScope = strings.TrimSpace(splitScope)
			if splitScope != "" {
				// Check for common OAuth scope formatting issues
				if strings.HasPrefix(splitScope, "scope=") {
					klog.V(1).Infof("OAuth scope '%s' contains 'scope=' prefix which may be invalid. Consider using just '%s' instead.", splitScope, strings.TrimPrefix(splitScope, "scope="))
				}

				klog.V(2).Infof("Adding scope part %d: '%s'", j, splitScope)
				processedScopes = append(processedScopes, splitScope)
			}
		}
	}

	// Remove duplicates
	seen := make(map[string]bool)
	var uniqueScopes []string
	for _, scope := range processedScopes {
		if !seen[scope] {
			seen[scope] = true
			uniqueScopes = append(uniqueScopes, scope)
		}
	}

	klog.V(1).Infof("Final processed OAuth scopes: %v", uniqueScopes)
	return uniqueScopes
}

func createK8sConfig() (*rest.Config, error) {
	if config.Kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", config.Kubeconfig)
	}

	// Try in-cluster config first
	if restConfig, err := rest.InClusterConfig(); err == nil {
		return restConfig, nil
	}

	// Use KUBECONFIG environment variable if set
	if kubeconfigEnv := os.Getenv("KUBECONFIG"); kubeconfigEnv != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfigEnv)
	}

	// Fall back to default kubeconfig location
	return clientcmd.BuildConfigFromFlags("", "")
}

func createScheme() (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	if err := hwmgmtv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add hardwaremanagement scheme: %w", err)
	}
	if err := inventoryv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add inventory scheme: %w", err)
	}
	if err := provisioningv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add provisioning scheme: %w", err)
	}
	if err := pluginsv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add plugins scheme: %w", err)
	}
	if err := metal3v1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add metal3 scheme: %w", err)
	}
	return scheme, nil
}
