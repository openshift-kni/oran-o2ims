/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/openshift-kni/oran-o2ims/internal/logging"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	pluginv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	sharedutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// HardwarePluginReconciler reconciles a HardwarePlugin object
type HardwarePluginReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger *slog.Logger
}

//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=hardwareplugins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=hardwareplugins/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=hardwareplugins/finalizers,verbs=update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *HardwarePluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)
	result = utils.DoNotRequeue()

	// Fetch the CR:
	hwplugin := &pluginv1alpha1.HardwarePlugin{}
	if err = r.Client.Get(ctx, req.NamespacedName, hwplugin); err != nil {
		if errors.IsNotFound(err) {
			err = nil
			return
		}
		r.Logger.ErrorContext(
			ctx,
			"Unable to fetch HardwarePlugin",
			slog.String("error", err.Error()),
		)
		return
	}

	// Make sure this is an instance for this adaptor and that this generation hasn't already been handled
	if hwplugin.Status.ObservedGeneration == hwplugin.Generation {
		// Nothing to do
		return
	}

	ctx = logging.AppendCtx(ctx, slog.String("HardwarePlugin", hwplugin.Name))

	hwplugin.Status.ObservedGeneration = hwplugin.Generation

	// Validate the HardwarePlugin
	condReason := pluginv1alpha1.ConditionReasons.InProgress
	condStatus := metav1.ConditionFalse
	condMessage := ""

	var isValid bool
	isValid, err = r.validateHardwarePlugin(ctx, hwplugin)
	if err != nil {
		err = fmt.Errorf("encountered an error while attempting to validate HardwarePlugin (%s): %w", hwplugin.Name, err)
		condMessage = err.Error()

	} else {
		if isValid {
			condReason = pluginv1alpha1.ConditionReasons.Completed
			condStatus = metav1.ConditionTrue
			condMessage = fmt.Sprintf("Validated connection to %s", hwplugin.Spec.ApiRoot)
		} else {
			condReason = pluginv1alpha1.ConditionReasons.Failed
			condStatus = metav1.ConditionFalse
			condMessage = fmt.Sprintf("Failed to validate connection to %s", hwplugin.Spec.ApiRoot)
		}
	}

	if updateErr := utils.UpdateHardwarePluginStatusCondition(ctx, r.Client, hwplugin,
		pluginv1alpha1.ConditionTypes.Registration, condReason, condStatus, condMessage); updateErr != nil {
		err = fmt.Errorf("failed to update status for HardwarePlugin (%s) with validation success: %w", hwplugin.Name, updateErr)
	}

	return
}

// setupOAuthClientConfig constructs an OAuth client configuration from the HardwarePlugin CR.
func (r *HardwarePluginReconciler) setupOAuthClientConfig(ctx context.Context, hwplugin *pluginv1alpha1.HardwarePlugin) (*sharedutils.OAuthClientConfig, error) {
	var caBundle string
	if hwplugin.Spec.CaBundleName != nil {
		cm, err := sharedutils.GetConfigmap(ctx, r.Client, *hwplugin.Spec.CaBundleName, hwplugin.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get CA Bundle configmap: %w", err)
		}

		caBundle, err = sharedutils.GetConfigMapField(cm, sharedutils.CABundleFilename)
		if err != nil {
			return nil, fmt.Errorf("failed to get certificate bundle from configmap: %w", err)
		}
	}

	config := sharedutils.OAuthClientConfig{
		TLSConfig: &sharedutils.TLSConfig{CaBundle: []byte(caBundle)},
	}

	if hwplugin.Spec.AuthClientConfig.TLSConfig != nil && hwplugin.Spec.AuthClientConfig.TLSConfig.SecretName != nil {
		secretName := *hwplugin.Spec.AuthClientConfig.TLSConfig.SecretName
		cert, key, err := sharedutils.GetKeyPairFromSecret(ctx, r.Client, secretName, hwplugin.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get certificate and key from secret: %w", err)
		}

		config.TLSConfig.ClientCert = sharedutils.NewStaticKeyPairLoader(cert, key)
	}

	if hwplugin.Spec.AuthClientConfig.OAuthClientConfig != nil {
		oauthConf := hwplugin.Spec.AuthClientConfig.OAuthClientConfig
		secret, err := sharedutils.GetSecret(ctx, r.Client, oauthConf.ClientSecretName, hwplugin.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get oauth secret '%s': %w", oauthConf.ClientSecretName, err)
		}

		clientID, err := sharedutils.GetSecretField(secret, sharedutils.OAuthClientIDField)
		if err != nil {
			return nil, fmt.Errorf("failed to get '%s' from oauth secret: %s", sharedutils.OAuthClientIDField, err.Error())
		}

		clientSecret, err := sharedutils.GetSecretField(secret, sharedutils.OAuthClientSecretField)
		if err != nil {
			return nil, fmt.Errorf("failed to get '%s' from oauth secret: %s", sharedutils.OAuthClientSecretField, err.Error())
		}

		config.OAuthConfig = &sharedutils.OAuthConfig{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			TokenURL:     strings.TrimSuffix(oauthConf.URL, "/") + "/" + strings.TrimPrefix(oauthConf.TokenEndpoint, "/"),
			Scopes:       oauthConf.Scopes,
		}
	}

	// TODO: process hwplugin.Spec.AuthClientConfig.BasicAuthSecret when `Basic` authType is supported

	return &config, nil
}

// validateHardwarePlugin verifies secure connectivity to the HardwarePlugin's apiRoot using mTLS.
func (r *HardwarePluginReconciler) validateHardwarePlugin(ctx context.Context, hwplugin *pluginv1alpha1.HardwarePlugin) (bool, error) {

	if hwplugin.Spec.AuthClientConfig == nil {
		return false, fmt.Errorf("missing authClientConfig configuration")
	}

	// Construct OAuth client configuration
	config, err := r.setupOAuthClientConfig(ctx, hwplugin)
	if err != nil {
		return false, fmt.Errorf("failed to setup OAuthClientConfig: %w", err)
	}

	// build OAuth client information if type is not ServiceAccount
	cf := notifier.NewClientFactory(config, sharedutils.DefaultBackendTokenFile)
	httpClient, err := cf.NewClient(ctx, hwplugin.Spec.AuthClientConfig.Type)
	if err != nil {
		return false, fmt.Errorf("failed to build client: %w", err)
	}

	// Validate apiRoot URL
	apiRoot, err := url.Parse(hwplugin.Spec.ApiRoot)
	if err != nil {
		return false, fmt.Errorf("invalid apiRoot URL '%s': %w", hwplugin.Spec.ApiRoot, err)
	}

	// Build request to validation endpoint url
	url := fmt.Sprintf("%s%s", apiRoot, sharedutils.HardwarePluginValidationEndpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	// Send request to HardwarePlugin server
	// TODO: replace this section with a generated client version from the plugin API spec.
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to send request to '%s': %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode > http.StatusNoContent {
		r.Logger.ErrorContext(ctx, fmt.Sprintf("validation attempt to '%s' failed, HTTP code=%d", url, resp.StatusCode))
		return false, nil
	}

	r.Logger.InfoContext(ctx, fmt.Sprintf("validation attempt to '%s' succeeded, HTTP code=%d", url, resp.StatusCode))
	return true, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HardwarePluginReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Logger.Info("Setting up HardwarePlugin controller")
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&pluginv1alpha1.HardwarePlugin{}).
		Complete(r); err != nil {
		return fmt.Errorf("failed to setup HardwarePlugin controller: %w", err)
	}

	return nil
}
