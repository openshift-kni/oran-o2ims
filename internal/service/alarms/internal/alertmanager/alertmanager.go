package alertmanager

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	template "text/template"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

const (
	namespace  = "open-cluster-management-observability"
	secretName = "alertmanager-config"
	secretKey  = "alertmanager.yaml"
)

const templateName = "alertmanager.yaml"

//go:embed alertmanager.yaml.template
var alertManagerConfig []byte

// Setup updates the alertmanager config secret with the new configuration
func Setup(ctx context.Context, cl client.Client) error {
	// ACM recreates the secret when it is deleted, so we can safely assume it exists
	var secret corev1.Secret
	err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, &secret)
	if err != nil {
		return fmt.Errorf("failed to get secret %s/%s: %w", namespace, secretName, err)
	}

	// Verify that Data has the key "alertmanager.yaml"
	if _, ok := secret.Data[secretKey]; !ok {
		return fmt.Errorf("%s not found in secret %s/%s", secretKey, namespace, secretName)
	}

	t, err := template.New(templateName).Parse(string(alertManagerConfig))
	if err != nil {
		return fmt.Errorf("failed to parse alertmanager.yaml: %w", err)
	}

	var rendered bytes.Buffer
	err = t.ExecuteTemplate(&rendered, templateName, map[string]string{
		"url": utils.GetServiceURL(utils.InventoryAlarmServerName),
	})
	if err != nil {
		return fmt.Errorf("failed to render alertmanager.yaml: %w", err)
	}

	secret.Data[secretKey] = rendered.Bytes()
	err = cl.Update(ctx, &secret)
	if err != nil {
		return fmt.Errorf("failed to update secret %s/%s: %w", namespace, secretName, err)
	}

	slog.Info("Successfully configured alertmanager")
	return nil
}
