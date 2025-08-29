// SPDX-FileCopyrightText: Red Hat
//
// SPDX-License-Identifier: Apache-2.0
package alertmanager

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/infrastructure"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ACMObsAMRouteName route to collect ACM's AM API host
	ACMObsAMRouteName = "alertmanager"
	// ACMObsAMAuthSecretName secret to collect ACM's AM API BEARER and ca.Crt
	ACMObsAMAuthSecretName = "observability-alertmanager-accessor-token" // nolint: gosec
)

// APIAlert represents the alert structure returned by the Alertmanager API.
// https://github.com/prometheus/alertmanager/blob/56dace4f61a0649f3ad97c2f2f9b49dc20c786bd/api/v2/openapi.yaml
type APIAlert struct {
	Annotations  *map[string]string `json:"annotations"`
	Labels       *map[string]string `json:"labels"`
	StartsAt     *time.Time         `json:"startsAt"`
	EndsAt       *time.Time         `json:"endsAt"`
	GeneratorURL *string            `json:"generatorURL"`
	Fingerprint  *string            `json:"fingerprint"`
	Status       *Status            `json:"status"`
}

type Status struct {
	State       string   `json:"state"`
	SilencedBy  []string `json:"silencedBy"`
	InhibitedBy []string `json:"inhibitedBy"`
}

// AMClient provides methods to interact with Alertmanager
type AMClient struct {
	k8sClient        client.Client
	alarmsRepository repo.AlarmRepositoryInterface
	infrastructure   *infrastructure.Infrastructure
}

// NewAlertmanagerClient creates a new AMClient
func NewAlertmanagerClient(k8sClient client.Client, amrepo repo.AlarmRepositoryInterface, infra *infrastructure.Infrastructure) *AMClient {
	return &AMClient{
		k8sClient:        k8sClient,
		alarmsRepository: amrepo,
		infrastructure:   infra,
	}
}

// RunAlertSyncScheduler runs sync alerts at regular intervals until context is canceled
// This function blocks until the context is canceled and returns any error encountered
func (c *AMClient) RunAlertSyncScheduler(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info("Alert sync scheduler started", "interval", interval.String())

	// Continue syncing at regular intervals until context is canceled
	for {
		select {
		case <-ticker.C:
			slog.Info("Running scheduled alert sync")
			if err := c.SyncAlerts(ctx); err != nil {
				slog.Error("failed to sync alerts", "error", err)
				// Continue running even if a sync fails
			}
		case <-ctx.Done():
			slog.Info("Alert sync scheduler shutting down")
			return nil
		}
	}
}

// SyncAlerts sync events table based on the current set of alarms
// This is designed to be called at regular intervals
func (c *AMClient) SyncAlerts(ctx context.Context) error {
	apiPayload, err := c.getAlerts(ctx)
	if err != nil {
		return fmt.Errorf("failed to get alerts: %w", err)
	}

	// Covert to Webhook payload to allow us to maintain a single point of entry in the DB
	webhookPayload := ConvertAPIAlertsToWebhook(&apiPayload)
	if len(webhookPayload) != 0 {
		if err := HandleAlerts(ctx, c.infrastructure.Clients, c.alarmsRepository, &webhookPayload, API); err != nil {
			return fmt.Errorf("failed to handle alerts during full sync: %w", err)
		}
	}

	slog.Info("Alertmanager synced successfully")
	return nil
}

// getAlerts retrieves all alerts from Alertmanager API
func (c *AMClient) getAlerts(ctx context.Context) ([]APIAlert, error) {
	// Get the alertmanager route
	alertmanagerHost, err := c.GetAlertmanagerRoute(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get alertmanager route: %w", err)
	}

	// Initialize a new client each time to pick up the latest secrets
	httpClient, token, err := c.createAlertmanagerClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create alertmanager client: %w", err)
	}

	// Create request
	u := url.URL{
		Scheme: "https",
		Host:   alertmanagerHost,
		Path:   "/api/v2/alerts",
	}

	// Build query parameters
	q := u.Query()
	q.Set("active", "true")
	// Get alerts meant for OranReceiverName webhook
	q.Set("receiver", fmt.Sprintf("^(%s)$", OranReceiverName))
	// Get alerts even it user silenced it
	q.Set("silenced", "true")

	u.RawQuery = q.Encode()
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	// Add auth header
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Add("Accept", "application/json")

	// Send request
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %w", err)
	}
	defer func(Body io.ReadCloser) {
		if err := Body.Close(); err != nil {
			slog.Error("failed to close response body during AM get call", "error", err.Error())
		}
	}(resp.Body)

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response: %w", err)
	}

	// Check HTTP status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http error during call to alertmanager API: %d - %s", resp.StatusCode, string(body))
	}

	// Parse response as array of alerts
	var alerts []APIAlert
	if err := json.Unmarshal(body, &alerts); err != nil {
		return nil, fmt.Errorf("error parsing response: %w, body: %s", err, string(body))
	}

	slog.Info("Got alerts with AM API", "alerts", len(alerts))
	return alerts, nil
}

// GetAlertmanagerRoute gets the hostname for the alertmanager route using unstructured type
func (c *AMClient) GetAlertmanagerRoute(ctx context.Context) (string, error) {
	// Define the route GVK (Group, Version, Kind)
	routeGVK := schema.GroupVersionKind{
		Group:   "route.openshift.io",
		Version: "v1",
		Kind:    "Route",
	}

	// Create an unstructured object
	route := &unstructured.Unstructured{}
	route.SetGroupVersionKind(routeGVK)

	// Get the route
	if err := c.k8sClient.Get(ctx, client.ObjectKey{
		Namespace: ACMObsAMNamespace,
		Name:      ACMObsAMRouteName,
	}, route); err != nil {
		return "", fmt.Errorf("error getting '%s' route: %w", ACMObsAMRouteName, err)
	}

	// Try to get host from spec
	specHost, found, err := unstructured.NestedString(route.Object, "spec", "host")
	if err != nil {
		return "", fmt.Errorf("error accessing spec.host: %w", err)
	}
	if found && specHost != "" {
		return specHost, nil
	}

	// Fallback to status if spec host is empty
	statusIngress, found, err := unstructured.NestedSlice(route.Object, "status", "ingress")
	if err != nil {
		return "", fmt.Errorf("error accessing status.ingress: %w", err)
	}

	if found && len(statusIngress) > 0 {
		ingressMap, ok := statusIngress[0].(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("invalid ingress format")
		}

		host, found := ingressMap["host"]
		if found && host != "" {
			return host.(string), nil
		}
	}

	return "", fmt.Errorf("no host found in alertmanager route")
}

// createAlertmanagerClient creates a new HTTP client with the latest token
// TODO: we can be more robust by reading our `/var/run/secrets/kubernetes.io/serviceaccount/token` for token instead of relying on ACMObsAMAuthSecretName data.
func (c *AMClient) createAlertmanagerClient(ctx context.Context) (*http.Client, string, error) {
	var secret corev1.Secret
	if err := c.k8sClient.Get(ctx, client.ObjectKey{
		Namespace: ACMObsAMNamespace,
		Name:      ACMObsAMAuthSecretName,
	}, &secret); err != nil {
		return nil, "", fmt.Errorf("error getting token secret from '%s': %w", ACMObsAMAuthSecretName, err)
	}

	// Extract token
	token, ok := secret.Data["token"]
	if !ok {
		return nil, "", fmt.Errorf("token not found in secret")
	}

	// Extract CA certificate
	caCrt, ok := secret.Data["ca.crt"]
	if !ok {
		return nil, "", fmt.Errorf("ca.crt not found in secret")
	}

	// Create certificate pool with the CA cert
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(caCrt) {
		return nil, "", fmt.Errorf("failed to append CA certificate")
	}

	// Create HTTP client with the CA certificate
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    rootCAs,
				MinVersion: tls.VersionTLS12,
			},
		},
	}

	return httpClient, string(token), nil
}
