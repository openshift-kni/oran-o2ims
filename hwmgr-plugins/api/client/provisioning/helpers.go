/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package provisioning

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	sharedutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// setupOAuthClientConfig constructs an OAuth client configuration from the HardwarePlugin CR.
func setupOAuthClientConfig(ctx context.Context, c client.Client, hwPlugin *hwv1alpha1.HardwarePlugin) (*sharedutils.OAuthClientConfig, error) {
	config := &sharedutils.OAuthClientConfig{
		TLSConfig: &sharedutils.TLSConfig{},
	}

	// Set up CA bundle if specified
	if err := setupCABundle(ctx, c, hwPlugin, config); err != nil {
		return nil, err
	}

	// Set up TLS client certificate if specified
	if err := setupTLSClientCert(ctx, c, hwPlugin, config); err != nil {
		return nil, err
	}

	// Set up OAuth configuration if specified
	if err := setupOAuthConfig(ctx, c, hwPlugin, config); err != nil {
		return nil, err
	}

	// TODO: process hwPlugin.Spec.AuthClientConfig.BasicAuthSecret when `Basic` authType is supported

	return config, nil
}

// setupCABundle configures the CA bundle for TLS verification
func setupCABundle(ctx context.Context, c client.Client, hwPlugin *hwv1alpha1.HardwarePlugin, config *sharedutils.OAuthClientConfig) error {
	if hwPlugin.Spec.CaBundleName == nil {
		return nil
	}

	cm, err := sharedutils.GetConfigmap(ctx, c, *hwPlugin.Spec.CaBundleName, hwPlugin.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get CA bundle configmap: %w", err)
	}

	caBundle, err := sharedutils.GetConfigMapField(cm, sharedutils.CABundleFilename)
	if err != nil {
		return fmt.Errorf("failed to get certificate bundle from configmap: %w", err)
	}

	config.TLSConfig.CaBundle = []byte(caBundle)
	return nil
}

// setupTLSClientCert configures the TLS client certificate for mutual TLS
func setupTLSClientCert(ctx context.Context, c client.Client, hwPlugin *hwv1alpha1.HardwarePlugin, config *sharedutils.OAuthClientConfig) error {
	if hwPlugin.Spec.AuthClientConfig.TLSConfig == nil ||
		hwPlugin.Spec.AuthClientConfig.TLSConfig.SecretName == nil {
		return nil
	}

	secretName := *hwPlugin.Spec.AuthClientConfig.TLSConfig.SecretName
	cert, key, err := sharedutils.GetKeyPairFromSecret(ctx, c, secretName, hwPlugin.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get certificate and key from secret: %w", err)
	}

	config.TLSConfig.ClientCert = sharedutils.NewStaticKeyPairLoader(cert, key)
	return nil
}

// setupOAuthConfig configures OAuth client credentials
func setupOAuthConfig(ctx context.Context, c client.Client, hwPlugin *hwv1alpha1.HardwarePlugin, config *sharedutils.OAuthClientConfig) error {
	if hwPlugin.Spec.AuthClientConfig.OAuthClientConfig == nil {
		return nil
	}

	oauthConf := hwPlugin.Spec.AuthClientConfig.OAuthClientConfig
	secret, err := sharedutils.GetSecret(ctx, c, oauthConf.ClientSecretName, hwPlugin.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get OAuth secret '%s': %w", oauthConf.ClientSecretName, err)
	}

	clientID, err := sharedutils.GetSecretField(secret, sharedutils.OAuthClientIDField)
	if err != nil {
		return fmt.Errorf("failed to get '%s' from OAuth secret: %w", sharedutils.OAuthClientIDField, err)
	}

	clientSecret, err := sharedutils.GetSecretField(secret, sharedutils.OAuthClientSecretField)
	if err != nil {
		return fmt.Errorf("failed to get '%s' from OAuth secret: %w", sharedutils.OAuthClientSecretField, err)
	}

	config.OAuthConfig = &sharedutils.OAuthConfig{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     buildTokenURL(oauthConf.URL, oauthConf.TokenEndpoint),
		Scopes:       oauthConf.Scopes,
	}

	return nil
}

// buildTokenURL constructs the token URL from base URL and token endpoint
func buildTokenURL(baseURL, tokenEndpoint string) string {
	return strings.TrimSuffix(baseURL, "/") + "/" + strings.TrimPrefix(tokenEndpoint, "/")
}

// handleErrorResponse processes error responses and returns a formatted error
func (h *HardwarePluginClient) handleErrorResponse(
	responseStatus string,
	problem *ProblemDetails,
	resourceType, resourceID, action string,
) error {
	logger := h.logger.With(
		slog.String("resourceType", resourceType),
		slog.String("resourceID", resourceID),
		slog.String("action", action),
		slog.String("status", responseStatus),
	)

	if problem == nil || problem.Detail == "" {
		logger.Error("Received empty or unexpected error response")
		return fmt.Errorf("empty or unexpected error response for %s '%s': %s", resourceType, resourceID, responseStatus)
	}

	logger.Error("Failed to process request", slog.String("detail", problem.Detail))
	return fmt.Errorf("failed to %s %s '%s': %s - %s", action, resourceType, resourceID, responseStatus, problem.Detail)
}

// getProblemDetails extracts problem details based on status code
//
//nolint:gocyclo
func (h *HardwarePluginClient) getProblemDetails(
	response interface{},
	statusCode int,
) (*ProblemDetails, string) {
	switch resp := response.(type) {
	case *GetAllVersionsResponse:
		switch statusCode {
		case http.StatusBadRequest:
			return resp.ApplicationProblemJSON400, MsgBadRequest
		case http.StatusInternalServerError:
			return resp.ApplicationProblemJSON500, MsgInternalServerError
		}
	case *GetAllocatedNodesResponse:
		switch statusCode {
		case http.StatusBadRequest:
			return resp.ApplicationProblemJSON400, MsgBadRequest
		case http.StatusUnauthorized:
			return resp.ApplicationProblemJSON401, MsgUnauthorized
		case http.StatusForbidden:
			return resp.ApplicationProblemJSON403, MsgForbidden
		case http.StatusInternalServerError:
			return resp.ApplicationProblemJSON500, MsgInternalServerError
		}
	case *GetAllocatedNodeResponse:
		switch statusCode {
		case http.StatusBadRequest:
			return resp.ApplicationProblemJSON400, MsgBadRequest
		case http.StatusUnauthorized:
			return resp.ApplicationProblemJSON401, MsgUnauthorized
		case http.StatusForbidden:
			return resp.ApplicationProblemJSON403, MsgForbidden
		case http.StatusNotFound:
			return resp.ApplicationProblemJSON404, MsgNotFound
		case http.StatusInternalServerError:
			return resp.ApplicationProblemJSON500, MsgInternalServerError
		}
	case *GetMinorVersionsResponse:
		switch statusCode {
		case http.StatusBadRequest:
			return resp.ApplicationProblemJSON400, MsgBadRequest
		case http.StatusInternalServerError:
			return resp.ApplicationProblemJSON500, MsgInternalServerError
		}
	case *GetNodeAllocationRequestsResponse:
		switch statusCode {
		case http.StatusBadRequest:
			return resp.ApplicationProblemJSON400, MsgBadRequest
		case http.StatusUnauthorized:
			return resp.ApplicationProblemJSON401, MsgUnauthorized
		case http.StatusForbidden:
			return resp.ApplicationProblemJSON403, MsgForbidden
		case http.StatusInternalServerError:
			return resp.ApplicationProblemJSON500, MsgInternalServerError
		}
	case *CreateNodeAllocationRequestResponse:
		switch statusCode {
		case http.StatusBadRequest:
			return resp.ApplicationProblemJSON400, MsgBadRequest
		case http.StatusUnauthorized:
			return resp.ApplicationProblemJSON401, MsgUnauthorized
		case http.StatusForbidden:
			return resp.ApplicationProblemJSON403, MsgForbidden
		case http.StatusInternalServerError:
			return resp.ApplicationProblemJSON500, MsgInternalServerError
		}
	case *DeleteNodeAllocationRequestResponse:
		switch statusCode {
		case http.StatusBadRequest:
			return resp.ApplicationProblemJSON400, MsgBadRequest
		case http.StatusUnauthorized:
			return resp.ApplicationProblemJSON401, MsgUnauthorized
		case http.StatusForbidden:
			return resp.ApplicationProblemJSON403, MsgForbidden
		case http.StatusNotFound:
			return resp.ApplicationProblemJSON404, MsgNotFound
		case http.StatusInternalServerError:
			return resp.ApplicationProblemJSON500, MsgInternalServerError
		}
	case *GetNodeAllocationRequestResponse:
		switch statusCode {
		case http.StatusBadRequest:
			return resp.ApplicationProblemJSON400, MsgBadRequest
		case http.StatusUnauthorized:
			return resp.ApplicationProblemJSON401, MsgUnauthorized
		case http.StatusForbidden:
			return resp.ApplicationProblemJSON403, MsgForbidden
		case http.StatusNotFound:
			return resp.ApplicationProblemJSON404, MsgNotFound
		case http.StatusInternalServerError:
			return resp.ApplicationProblemJSON500, MsgInternalServerError
		}
	case *UpdateNodeAllocationRequestResponse:
		switch statusCode {
		case http.StatusBadRequest:
			return resp.ApplicationProblemJSON400, MsgBadRequest
		case http.StatusUnauthorized:
			return resp.ApplicationProblemJSON401, MsgUnauthorized
		case http.StatusForbidden:
			return resp.ApplicationProblemJSON403, MsgForbidden
		case http.StatusNotFound:
			return resp.ApplicationProblemJSON404, MsgNotFound
		case http.StatusInternalServerError:
			return resp.ApplicationProblemJSON500, MsgInternalServerError
		}
	case *GetAllocatedNodesFromNodeAllocationRequestResponse:
		switch statusCode {
		case http.StatusBadRequest:
			return resp.ApplicationProblemJSON400, MsgBadRequest
		case http.StatusUnauthorized:
			return resp.ApplicationProblemJSON401, MsgUnauthorized
		case http.StatusForbidden:
			return resp.ApplicationProblemJSON403, MsgForbidden
		case http.StatusNotFound:
			return resp.ApplicationProblemJSON404, MsgNotFound
		case http.StatusInternalServerError:
			return resp.ApplicationProblemJSON500, MsgInternalServerError
		}
	}
	return nil, http.StatusText(statusCode)
}
