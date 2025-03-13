/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package alertmanager

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
)

const (
	ACMObsAMNamespace  = utils.AlertmanagerNamespace
	ACMObsAMSecretName = "alertmanager-config"
	ACMObsAMSecretKey  = "alertmanager.yaml"
	OranReceiverName   = "oran_alarm_receiver"
)

var GetHubClient = k8s.NewClientForHub

// Setup updates the alertmanager config secret with the new configuration
func Setup(ctx context.Context) error {
	hubClient, err := GetHubClient()
	if err != nil {
		return fmt.Errorf("error creating client for hub: %w", err)
	}

	// ACM recreates the secret when it is deleted, so we can safely assume it exists
	var secret corev1.Secret
	if err = hubClient.Get(ctx, client.ObjectKey{Namespace: ACMObsAMNamespace, Name: ACMObsAMSecretName}, &secret); err != nil {
		return fmt.Errorf("failed to get secret %s/%s: %w", ACMObsAMNamespace, ACMObsAMSecretName, err)
	}

	// If there's no existing config, return error
	existingYAML, exists := secret.Data[ACMObsAMSecretKey]
	if !exists {
		return fmt.Errorf("secret %s/%s does not contain key %s", ACMObsAMNamespace, ACMObsAMSecretName, ACMObsAMSecretKey)
	}

	// Merge the existing configuration with oran-specific settings
	updateCfg, err := AddOranRouteToConfig(existingYAML)
	if err != nil {
		return fmt.Errorf("failed to update existing alertmanager config with oran config: %w", err)
	}

	// Marshal the updated configuration back to YAML with custom indentation.
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(updateCfg); err != nil {
		return fmt.Errorf("failed to encode updated alertmanager config: %w", err)
	}

	// Set it back to the secret for ACM
	secret.Data[ACMObsAMSecretKey] = buf.Bytes()

	if err = hubClient.Update(ctx, &secret); err != nil {
		return fmt.Errorf("failed to update secret %s/%s: %w", ACMObsAMNamespace, ACMObsAMSecretName, err)
	}

	slog.Info("Successfully merged with existing alertmanager")
	return nil
}

// AddOranRouteToConfig updates the existing alertmanager configuration with oran-specific changes.
func AddOranRouteToConfig(existingYAML []byte) (map[string]interface{}, error) {
	// Unmarshal the existing YAML into a map.
	var config map[string]interface{}
	if err := yaml.Unmarshal(existingYAML, &config); err != nil {
		return nil, fmt.Errorf("error unmarshalling YAML: %w", err)
	}

	// Validate config exists.
	if config == nil {
		return nil, fmt.Errorf("existing alertmanager.yaml is empty, it must already come with at least few defaults")
	}

	updateReceivers(config)
	updateRoutes(config)

	slog.Debug("Alertmanager config update complete", "receiver_name", OranReceiverName)
	return config, nil
}

// updateReceivers updates the alertmanager configuration with the oran receiver.
func updateReceivers(config map[string]interface{}) {
	// Directly compute the URL for the oran receiver.
	url := fmt.Sprintf("%s/internal/v1/caas-alerts/alertmanager", utils.GetServiceURL(utils.InventoryAlarmServerName))

	// Create oran receiver with webhook config.
	oranReceiver := map[string]interface{}{
		"name": OranReceiverName,
		"webhook_configs": []interface{}{
			map[string]interface{}{
				"send_resolved": true, // Notify alarms server if anything is resolved.
				"url":           url,  // Internal API.
				"http_config": map[string]interface{}{ // Auth config.
					"authorization": map[string]interface{}{
						"type":             "Bearer",
						"credentials_file": utils.DefaultBackendTokenFile,
					},
					"tls_config": map[string]interface{}{
						"ca_file": utils.DefaultServiceCAFile,
					},
				},
			},
		},
	}

	// Merge existing receivers, filtering out any previous oran receivers.
	var receivers []interface{}
	if existingReceivers, ok := config["receivers"].([]interface{}); ok {
		for _, rcv := range existingReceivers {
			rcvMap, ok := rcv.(map[string]interface{})
			if !ok || rcvMap["name"] != OranReceiverName {
				receivers = append(receivers, rcv)
			}
		}
	}
	// Prepend the new oran receiver.
	receivers = append([]interface{}{oranReceiver}, receivers...)
	config["receivers"] = receivers
	slog.Info("Configured oran receiver in alertmanager config")
}

// updateRoutes updates the alertmanager route configuration with the oran route.
func updateRoutes(config map[string]interface{}) {
	// Retrieve or create the main route.
	var mainRoute map[string]interface{}
	if route, ok := config["route"].(map[string]interface{}); ok {
		mainRoute = route
	} else {
		mainRoute = map[string]interface{}{
			"receiver": OranReceiverName,
		}
		slog.Info("Creating new main route configuration with oran receiver as default")
	}

	// This is the only global config that needs to be replaced.
	// The child route is not override "group_by" unlike other attributes if child is an empty list
	// TODO: Our code depends on the full list coming in at the same, check with AM team and fix this.
	mainRoute["group_by"] = []string{}

	// Create oran route config.
	oranRoute := map[string]interface{}{
		"receiver":        OranReceiverName,
		"group_wait":      "30s",
		"group_interval":  "1m",
		"repeat_interval": "4h",
		"matchers":        []string{`alertname!~"Watchdog"`}, // Exclude Watchdog alerts.
		"continue":        true,                              // Process subsequent routes.
		// "group_by":        []string{},                        // Empty array groups all alerts together.
	}

	// Merge existing child routes, filtering out any previous oran routes.
	var routes []interface{}
	if existingRoutes, ok := mainRoute["routes"].([]interface{}); ok {
		for _, route := range existingRoutes {
			routeMap, ok := route.(map[string]interface{})
			if !ok || routeMap["receiver"] != OranReceiverName {
				routes = append(routes, route)
			}
		}
	}
	// Prepend the new oran route.
	routes = append([]interface{}{oranRoute}, routes...)
	mainRoute["routes"] = routes
	config["route"] = mainRoute
	slog.Info("Configured oran route in alertmanager config")
}
