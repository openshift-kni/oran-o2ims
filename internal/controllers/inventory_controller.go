/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8sptr "k8s.io/utils/ptr"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

//+kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents,verbs=get;list;watch
//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=prometheusrules,verbs=get;list;watch
//+kubebuilder:rbac:groups=view.open-cluster-management.io,resources=managedclusterviews,verbs=create
//+kubebuilder:rbac:groups=operator.openshift.io,resources=ingresscontrollers,verbs=get;list;watch
//+kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
//+kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create
//+kubebuilder:rbac:groups=o2ims.oran.openshift.io,resources=inventories,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims.oran.openshift.io,resources=inventories/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=o2ims.oran.openshift.io,resources=inventories/finalizers,verbs=update
//+kubebuilder:rbac:groups="apps",resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="networking.k8s.io",resources=ingresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterroles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="cluster.open-cluster-management.io",resources=managedclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;delete;list;watch;update
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups="internal.open-cluster-management.io",resources=managedclusterinfos,verbs=get;list;watch
//+kubebuilder:rbac:groups="config.openshift.io",resources=clusterversions,verbs=get;list;watch
//+kubebuilder:rbac:urls="/internal/v1/caas-alerts/alertmanager",verbs=create;post
//+kubebuilder:rbac:urls="/o2ims-infrastructureCluster/v1/nodeClusterTypes",verbs=get;list
//+kubebuilder:rbac:urls="/o2ims-infrastructureCluster/v1/nodeClusters",verbs=get;list
//+kubebuilder:rbac:groups="batch",resources=cronjobs,verbs=get;list;watch;create;update;patch;delete

// Reconciler reconciles a Inventory object
type Reconciler struct {
	client.Client
	Logger *slog.Logger
	Image  string
}

// reconcilerTask contains the information and logic needed to perform a single reconciliation
// task. This reduces the need to pass things like the current state of the object as function
// parameters.
type reconcilerTask struct {
	logger *slog.Logger
	image  string
	client client.Client
	object *inventoryv1alpha1.Inventory
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Inventory object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result,
	err error) {
	// Fetch the object:
	object := &inventoryv1alpha1.Inventory{}
	if err := r.Client.Get(ctx, request.NamespacedName, object); err != nil {
		if errors.IsNotFound(err) {
			err = nil
			return ctrl.Result{RequeueAfter: 5 * time.Minute}, err
		}
		r.Logger.ErrorContext(
			ctx,
			"Unable to fetch Inventory",
			slog.String("error", err.Error()),
		)
	}

	// Create and run the task:
	task := &reconcilerTask{
		logger: r.Logger,
		client: r.Client,
		image:  r.Image,
		object: object,
	}
	result, err = task.run(ctx)
	return
}

// setupResourceServerConfig creates the resources necessary to start the Resource Server.
func (t *reconcilerTask) setupResourceServerConfig(ctx context.Context, defaultResult ctrl.Result) (nextReconcile ctrl.Result, err error) {
	nextReconcile = defaultResult

	err = t.createServiceAccount(ctx, utils.InventoryResourceServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy ServiceAccount for the Resource server.",
			slog.String("error", err.Error()),
		)
		return
	}

	err = t.createResourceServerClusterRole(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create resource server cluster role",
			slog.String("error", err.Error()),
		)
		return
	}

	err = t.createServerClusterRoleBinding(ctx, utils.InventoryResourceServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create resource server cluster role binding",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the role binding needed to allow the kube-rbac-proxy to interact with the API server to validate incoming
	// API requests from clients.
	err = t.createServerRbacClusterRoleBinding(ctx, utils.InventoryResourceServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create resource server RBAC proxy cluster role binding",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the Service needed for the Resource server.
	err = t.createService(ctx, utils.InventoryResourceServerName, utils.DefaultServicePort, utils.DefaultTargetPort)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy Service for Resource server.",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the resource-server deployment.
	errorReason, err := t.deployServer(ctx, utils.InventoryResourceServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy the Resource server.",
			slog.String("error", err.Error()),
		)
		if errorReason == "" {
			nextReconcile = ctrl.Result{RequeueAfter: 60 * time.Second}
			return nextReconcile, err
		}
	}

	return nextReconcile, err
}

// setupClusterServerConfig creates the resources necessary to start the Resource Server.
func (t *reconcilerTask) setupClusterServerConfig(ctx context.Context, defaultResult ctrl.Result) (nextReconcile ctrl.Result, err error) {
	nextReconcile = defaultResult

	err = t.createServiceAccount(ctx, utils.InventoryClusterServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy ServiceAccount for the cluster server.",
			slog.String("error", err.Error()),
		)
		return
	}

	err = t.createClusterServerClusterRole(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create cluster server cluster role",
			slog.String("error", err.Error()),
		)
		return
	}

	err = t.createServerClusterRoleBinding(ctx, utils.InventoryClusterServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create cluster server cluster role binding",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the role binding needed to allow the kube-rbac-proxy to interact with the API server to validate incoming
	// API requests from clients.
	err = t.createServerRbacClusterRoleBinding(ctx, utils.InventoryClusterServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create cluster server RBAC proxy cluster role binding",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the Service needed for the cluster server.
	err = t.createService(ctx, utils.InventoryClusterServerName, utils.DefaultServicePort, utils.DefaultTargetPort)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy Service for cluster server.",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the cluster-server deployment.
	errorReason, err := t.deployServer(ctx, utils.InventoryClusterServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy the cluster server.",
			slog.String("error", err.Error()),
		)
		if errorReason == "" {
			nextReconcile = ctrl.Result{RequeueAfter: 60 * time.Second}
			return nextReconcile, err
		}
	}

	return nextReconcile, err
}

// setupArtifactsServerConfig creates the resource necessary to start the Artifacts Server.
func (t *reconcilerTask) setupArtifactsServerConfig(ctx context.Context, defaultResult ctrl.Result) (nextReconcile ctrl.Result, err error) {
	nextReconcile = defaultResult

	err = t.createServiceAccount(ctx, utils.InventoryArtifactsServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy ServiceAccount for the Artifacts server.",
			slog.String("error", err.Error()),
		)
		return
	}

	err = t.createArtifactsServerClusterRole(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create artifacts cluster role",
			slog.String("error", err.Error()),
		)
		return
	}
	err = t.createServerClusterRoleBinding(ctx, utils.InventoryArtifactsServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create artifacts cluster role binding",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the role binding needed to allow the kube-rbac-proxy to interact with the API server to validate incoming
	// API requests from clients.
	err = t.createServerRbacClusterRoleBinding(ctx, utils.InventoryArtifactsServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create Artifacts server RBAC proxy cluster role binding",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the Service needed for the Artifacts server.
	err = t.createService(ctx, utils.InventoryArtifactsServerName, utils.DefaultServicePort, utils.DefaultTargetPort)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy Service for the Artifacts server.",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the artifacts-server deployment.
	errorReason, err := t.deployServer(ctx, utils.InventoryArtifactsServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy the Artifacts server.",
			slog.String("error", err.Error()),
		)
		if errorReason == "" {
			nextReconcile = ctrl.Result{RequeueAfter: 60 * time.Second}
			return nextReconcile, err
		}
	}

	return
}

// setupAlarmServerConfig creates the resources necessary to start the Alarm Server.
func (t *reconcilerTask) setupAlarmServerConfig(ctx context.Context, defaultResult ctrl.Result) (nextReconcile ctrl.Result, err error) {
	nextReconcile = defaultResult

	err = t.createServiceAccount(ctx, utils.InventoryAlarmServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy ServiceAccount for the alarm server.",
			slog.String("error", err.Error()),
		)
		return
	}

	err = t.createAlarmServerClusterRole(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create alarm server cluster role",
			slog.String("error", err.Error()),
		)
		return
	}

	err = t.createServerClusterRoleBinding(ctx, utils.InventoryAlarmServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create alarm server cluster role binding",
			slog.String("error", err.Error()),
		)
		return
	}

	// Needed for the Alertmanager to be able to send alerts to the alarm server
	err = t.createAlertmanagerClusterRoleAndBinding(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create alertmanager cluster role and binding",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the role binding needed to allow the kube-rbac-proxy to interact with the API server to validate incoming
	// API requests from clients.
	err = t.createServerRbacClusterRoleBinding(ctx, utils.InventoryAlarmServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create alarm server RBAC proxy cluster role binding",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the Service needed for the alarm server.
	err = t.createService(ctx, utils.InventoryAlarmServerName, utils.DefaultServicePort, utils.DefaultTargetPort)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy Service for alarm server.",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the alarm-server deployment.
	errorReason, err := t.deployServer(ctx, utils.InventoryAlarmServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy the alarm server.",
			slog.String("error", err.Error()),
		)
		if errorReason == "" {
			nextReconcile = ctrl.Result{RequeueAfter: 60 * time.Second}
			return nextReconcile, err
		}
	}

	return nextReconcile, err
}

func (t *reconcilerTask) setupProvisioningServerConfig(ctx context.Context, defaultResult ctrl.Result) (nextReconcile ctrl.Result, err error) {
	nextReconcile = defaultResult

	err = t.createServiceAccount(ctx, utils.InventoryProvisioningServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy ServiceAccount for the Provisioning server.",
			slog.String("error", err.Error()),
		)
		return
	}

	err = t.createProvisioningServerClusterRole(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create provisioning cluster role",
			slog.String("error", err.Error()),
		)
		return
	}

	err = t.createServerClusterRoleBinding(ctx, utils.InventoryProvisioningServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create provisioning cluster role binding",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the role binding needed to allow the kube-rbac-proxy to interact with the API server to validate incoming
	// API requests from clients.
	err = t.createServerRbacClusterRoleBinding(ctx, utils.InventoryProvisioningServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create Provisioning server RBAC proxy cluster role binding",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the Service needed for the provisioning server.
	err = t.createService(ctx, utils.InventoryProvisioningServerName, utils.DefaultServicePort, utils.DefaultTargetPort)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy Service for the provisioning server.",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the provisioning-server deployment.
	errorReason, err := t.deployServer(ctx, utils.InventoryProvisioningServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy the provisioning server.",
			slog.String("error", err.Error()),
		)
		if errorReason == "" {
			nextReconcile = ctrl.Result{RequeueAfter: 60 * time.Second}
			return nextReconcile, err
		}
	}

	return
}

// setupOAuthClient is a wrapper around the similarly named method in the utils package.  The purpose for this wrapper
// is to gather the parameters required by the utils version of this function before invoking it.  The reason for the
// two layers is that it is expected that other parts of the system will need to get these inputs parameters differently
// (i.e., from mounted files rather than API objects) but will need to share the underlying client creation code.
func (t *reconcilerTask) setupOAuthClient(ctx context.Context) (*http.Client, error) {
	var caBundle string
	if t.object.Spec.CaBundleName != nil {
		cm, err := utils.GetConfigmap(ctx, t.client, *t.object.Spec.CaBundleName, t.object.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get configmap: %s", err.Error())
		}

		caBundle, err = utils.GetConfigMapField(cm, "ca-bundle.pem")
		if err != nil {
			return nil, fmt.Errorf("failed to get certificate bundle from configmap: %s", err.Error())
		}
	}

	config := utils.OAuthClientConfig{
		TLSConfig: &utils.TLSConfig{CaBundle: []byte(caBundle)},
	}

	oAuthConfig := t.object.Spec.SmoConfig.OAuthConfig
	if oAuthConfig != nil {
		clientSecrets, err := utils.GetSecret(ctx, t.client, oAuthConfig.ClientSecretName, t.object.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get client secret: %w", err)
		}

		clientId, err := utils.GetSecretField(clientSecrets, "client-id")
		if err != nil {
			return nil, fmt.Errorf("failed to get client-id from secret: %s, %w", oAuthConfig.ClientSecretName, err)
		}

		clientSecret, err := utils.GetSecretField(clientSecrets, "client-secret")
		if err != nil {
			return nil, fmt.Errorf("failed to get client-secret from secret: %s, %w", oAuthConfig.ClientSecretName, err)
		}

		o := utils.OAuthConfig{
			ClientID:     clientId,
			ClientSecret: clientSecret,
			TokenURL:     fmt.Sprintf("%s%s", oAuthConfig.URL, oAuthConfig.TokenEndpoint),
			Scopes:       oAuthConfig.Scopes,
		}
		config.OAuthConfig = &o
	}

	if t.object.Spec.SmoConfig.TLS != nil && t.object.Spec.SmoConfig.TLS.ClientCertificateName != nil {
		secretName := *t.object.Spec.SmoConfig.TLS.ClientCertificateName
		cert, err := utils.GetCertFromSecret(ctx, t.client, secretName, t.object.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get client certificate from secret: %w", err)
		}

		config.TLSConfig.ClientCert = cert
	}

	httpClient, err := utils.SetupOAuthClient(ctx, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to setup OAuth client: %w", err)
	}

	return httpClient, nil
}

// registerWithSmo sends a message to the SMO to register our identifiers and URL
func (t *reconcilerTask) registerWithSmo(ctx context.Context) error {
	smo := t.object.Spec.SmoConfig

	httpClient, err := t.setupOAuthClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to setup oauth client: %w", err)
	}

	data := utils.AvailableNotification{
		GlobalCloudId: *t.object.Spec.CloudID,
		OCloudId:      t.object.Status.ClusterID,
		ImsEndpoint:   fmt.Sprintf("https://%s/o2ims-infrastructureInventory/v1", t.object.Status.IngressHost),
	}

	body, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal AvailableNotification: %s", err.Error())
	}

	url := fmt.Sprintf("%s%s", smo.URL, smo.RegistrationEndpoint)
	result, err := httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to send registration request to '%s': %s", url, err.Error())
	}

	if result.StatusCode != http.StatusOK {
		return fmt.Errorf("registration request failed to '%s', HTTP code=%s", url, result.Status)
	}

	return nil
}

// setupSmo executes the high-level action set register with the SMO and set up the related conditions accordingly
func (t *reconcilerTask) setupSmo(ctx context.Context) (err error) {
	if t.object.Spec.SmoConfig == nil || t.object.Spec.CloudID == nil {
		meta.SetStatusCondition(
			&t.object.Status.Conditions,
			metav1.Condition{
				Type:    string(utils.InventoryConditionTypes.SmoRegistrationCompleted),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.InventoryConditionReasons.SmoRegistrationSuccessful),
				Message: "SMO configuration not present",
			},
		)
		return nil
	}

	if !utils.IsSmoRegistrationCompleted(t.object) {
		err = t.registerWithSmo(ctx)
		if err != nil {
			t.logger.ErrorContext(
				ctx, "Failed to register with SMO.",
				slog.String("error", err.Error()),
			)

			meta.SetStatusCondition(
				&t.object.Status.Conditions,
				metav1.Condition{
					Type:    string(utils.InventoryConditionTypes.SmoRegistrationCompleted),
					Status:  metav1.ConditionFalse,
					Reason:  err.Error(),
					Message: fmt.Sprintf("Error registering with SMO at: %s", t.object.Spec.SmoConfig.URL),
				},
			)

			return
		}

		meta.SetStatusCondition(
			&t.object.Status.Conditions,
			metav1.Condition{
				Type:    string(utils.InventoryConditionTypes.SmoRegistrationCompleted),
				Status:  metav1.ConditionTrue,
				Reason:  string(utils.InventoryConditionReasons.SmoRegistrationSuccessful),
				Message: fmt.Sprintf("Registered with SMO at: %s", t.object.Spec.SmoConfig.URL),
			},
		)
		t.logger.InfoContext(
			ctx, fmt.Sprintf("successfully registered with the SMO at: %s", t.object.Spec.SmoConfig.URL),
		)
	} else {
		t.logger.InfoContext(
			ctx, fmt.Sprintf("already registered with the SMO at: %s", t.object.Spec.SmoConfig.URL),
		)
	}

	return nil
}

// storeIngressDomain stores the ingress domain to be used onto the object status for later retrieval.
func (t *reconcilerTask) storeIngressDomain(ctx context.Context) error {
	// Determine our ingress domain
	var ingressHost string
	if t.object.Spec.IngressHost == nil {
		var err error
		ingressHost, err = utils.GetIngressDomain(ctx, t.client)
		if err != nil {
			t.logger.ErrorContext(
				ctx,
				"Failed to get ingress domain.",
				slog.String("error", err.Error()))
			return fmt.Errorf("failed to get ingress domain: %s", err.Error())
		}
		ingressHost = utils.DefaultAppName + "." + ingressHost
	} else {
		ingressHost = *t.object.Spec.IngressHost
	}

	t.object.Status.IngressHost = ingressHost

	return nil
}

// storeClusterID stores the local cluster id onto the object status for later retrieval.
func (t *reconcilerTask) storeClusterID(ctx context.Context) error {
	clusterID, err := utils.GetClusterID(ctx, t.client, utils.ClusterVersionName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to get cluster id.",
			slog.String("error", err.Error()))
		return fmt.Errorf("failed to get cluster id: %s", err.Error())
	}

	t.object.Status.ClusterID = clusterID
	return nil
}

// storeSearchURL stores the Search API URL onto the object status for later retrieval.
func (t *reconcilerTask) storeSearchURL(ctx context.Context) error {
	if t.object.Spec.ResourceServerConfig.BackendURL == "" {
		searchURL, err := utils.GetSearchURL(ctx, t.client)
		if err != nil {
			t.logger.ErrorContext(
				ctx,
				"Failed to get Search API URL.",
				slog.String("error", err.Error()))
			return fmt.Errorf("failed to get Search API URL: %s", err.Error())
		}
		t.object.Status.SearchURL = searchURL
	} else {
		t.object.Status.SearchURL = t.object.Spec.ResourceServerConfig.BackendURL
	}

	return nil
}

func (t *reconcilerTask) run(ctx context.Context) (nextReconcile ctrl.Result, err error) {
	// Set the default reconcile time to 5 minutes.
	nextReconcile = ctrl.Result{RequeueAfter: 5 * time.Minute}

	err = t.storeIngressDomain(ctx)
	if err != nil {
		return
	}

	if t.object.Status.ClusterID == "" {
		err = t.storeClusterID(ctx)
		if err != nil {
			return
		}
	}

	err = t.storeSearchURL(ctx)
	if err != nil {
		return
	}

	// Register with SMO (if necessary)
	err = t.setupSmo(ctx)
	if err != nil {
		return
	}

	// Create the database
	err = t.createDatabase(ctx)
	if updateError := t.updateInventoryUsedConfigStatus(ctx, utils.InventoryDatabaseServerName,
		nil, utils.InventoryConditionReasons.DatabaseDeploymentFailed, err); updateError != nil {
		t.logger.ErrorContext(ctx, "Failed to report database status", slog.String("error", updateError.Error()))
		return nextReconcile, updateError
	}

	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create database.",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the needed Ingress if at least one server is required by the Spec.
	if t.object.Spec.ResourceServerConfig.Enabled || t.object.Spec.AlarmServerConfig.Enabled ||
		t.object.Spec.ArtifactsServerConfig.Enabled {
		err = t.createIngress(ctx)
		if err != nil {
			t.logger.ErrorContext(
				ctx,
				"Failed to deploy Ingress.",
				slog.String("error", err.Error()),
			)
			return
		}

		// Create the shared cluster role for the kube-rbac-proxy
		err = t.createSharedRbacProxyRole(ctx)
		if err != nil {
			t.logger.ErrorContext(
				ctx,
				"Failed to deploy RBAC Proxy cluster role.",
				slog.String("error", err.Error()),
			)
			return
		}
	}

	// Start the resource server if required by the Spec.
	if t.object.Spec.ResourceServerConfig.Enabled {
		// Create the resources required by the resource server
		nextReconcile, err = t.setupResourceServerConfig(ctx, nextReconcile)
		if err != nil {
			return
		}
	}

	// Start the cluster server if required by the Spec.
	if t.object.Spec.ClusterServerConfig.Enabled {
		// Create the resources required by the cluster server
		nextReconcile, err = t.setupClusterServerConfig(ctx, nextReconcile)
		if err != nil {
			return
		}
	}

	// Start the alarm server if required by the Spec.
	if t.object.Spec.AlarmServerConfig.Enabled {
		// Create the alarm server.
		nextReconcile, err = t.setupAlarmServerConfig(ctx, nextReconcile)
		if err != nil {
			return
		}
	}

	// Start the artifacts server if required by the Spec.
	if t.object.Spec.ArtifactsServerConfig.Enabled {
		// Create the artifacts server.
		nextReconcile, err = t.setupArtifactsServerConfig(ctx, nextReconcile)
		if err != nil {
			return
		}
	}

	// Start the provisioning server
	nextReconcile, err = t.setupProvisioningServerConfig(ctx, nextReconcile)
	if err != nil {
		return
	}

	err = t.updateInventoryDeploymentStatus(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to update status for Inventory",
			slog.String("name", t.object.Name),
		)
		nextReconcile = ctrl.Result{RequeueAfter: 30 * time.Second}
	}
	return
}

func (t *reconcilerTask) createArtifactsServerClusterRole(ctx context.Context) error {
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(
				"%s-%s", t.object.Namespace, utils.InventoryArtifactsServerName,
			),
		},
		Rules: []rbacv1.PolicyRule{
			// We need to read ClusterTemplates.
			{
				APIGroups: []string{
					"o2ims.provisioning.oran.org",
				},
				Resources: []string{
					"clustertemplates",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
			},
		},
	}

	if err := utils.CreateK8sCR(ctx, t.client, role, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Artifacts server cluster role: %w", err)
	}

	return nil
}

// createSharedRbacProxyRole creates a cluster role that is used by the kube-rbac-proxy to access the authentication and
// authorization parts of the kubernetes API so that incoming API requests can be validated.  This same cluster role is
// attached to each of the server service accounts since each of them has its own kube-rbac-proxy that all share the
// exact same configuration.
func (t *reconcilerTask) createSharedRbacProxyRole(ctx context.Context) error {
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(
				"%s-%s", t.object.Namespace, "kube-rbac-proxy",
			),
		},
		Rules: []rbacv1.PolicyRule{
			// The kube-rbac-proxy needs access to the authentication API to validate tokens and access authorization.
			{
				APIGroups: []string{
					"authentication.k8s.io",
				},
				Resources: []string{
					"tokenreviews",
				},
				Verbs: []string{
					"create",
				},
			},
			{
				APIGroups: []string{
					"authorization.k8s.io",
				},
				Resources: []string{
					"subjectaccessreviews",
				},
				Verbs: []string{
					"create",
				},
			},
		},
	}

	if err := utils.CreateK8sCR(ctx, t.client, role, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create RBAC Proxy cluster role: %w", err)
	}

	return nil
}

func (t *reconcilerTask) createResourceServerClusterRole(ctx context.Context) error {
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(
				"%s-%s", t.object.Namespace, utils.InventoryResourceServerName,
			),
		},
		Rules: []rbacv1.PolicyRule{
			// We need to read managed clusters, as that is the main source for the
			// information about clusters.
			{
				APIGroups: []string{
					"cluster.open-cluster-management.io",
				},
				Resources: []string{
					"managedclusters",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
			},
			{
				APIGroups: []string{
					"view.open-cluster-management.io",
				},
				Resources: []string{
					"managedclusterviews",
				},
				Verbs: []string{
					"create",
				},
			},
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"nodes",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
			},
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"configmaps",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
					"create",
					"update",
					"patch",
					"delete",
				},
			},
			{
				APIGroups: []string{
					"internal.open-cluster-management.io",
				},
				Resources: []string{
					"managedclusterinfos",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
			},
		},
	}

	if err := utils.CreateK8sCR(ctx, t.client, role, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Resource Server cluster role: %w", err)
	}

	return nil
}

func (t *reconcilerTask) createClusterServerClusterRole(ctx context.Context) error {
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(
				"%s-%s", t.object.Namespace, utils.InventoryClusterServerName,
			),
		},
		Rules: []rbacv1.PolicyRule{
			// We need to read managed clusters, as that is the main source for the
			// information about clusters.
			{
				APIGroups: []string{
					"cluster.open-cluster-management.io",
				},
				Resources: []string{
					"managedclusters",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
			},
			{
				APIGroups: []string{
					"agent-install.openshift.io",
				},
				Resources: []string{
					"agents",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
			},
		},
	}

	if err := utils.CreateK8sCR(ctx, t.client, role, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Cluster Server cluster role: %w", err)
	}

	return nil
}

func (t *reconcilerTask) createProvisioningServerClusterRole(ctx context.Context) error {
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(
				"%s-%s", t.object.Namespace, utils.InventoryProvisioningServerName,
			),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{
					"o2ims.provisioning.oran.org",
				},
				Resources: []string{
					"provisioningrequests",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
					"create",
					"update",
					"delete",
				},
			},
		},
	}

	if err := utils.CreateK8sCR(ctx, t.client, role, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Provisioning Server cluster role: %w", err)
	}

	return nil
}

func (t *reconcilerTask) createAlarmServerClusterRole(ctx context.Context) error {
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(
				"%s-%s", t.object.Namespace, utils.InventoryAlarmServerName,
			),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{
					"cluster.open-cluster-management.io",
				},
				Resources: []string{
					"managedclusters",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
			},
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"services",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
			},
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"secrets",
					"configmaps",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
					"update",
					"create",
					"delete",
				},
			},
			{
				APIGroups: []string{
					"monitoring.coreos.com",
				},
				Resources: []string{
					"prometheusrules",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
			},
			{
				APIGroups: []string{
					"apps",
				},
				Resources: []string{
					"deployments",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
			},
			{
				APIGroups: []string{
					"batch",
				},
				Resources: []string{
					"cronjobs",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
					"create",
					"delete",
					"update",
					"patch",
				},
			},
			{
				NonResourceURLs: []string{
					"/o2ims-infrastructureCluster/v1/nodeClusterTypes",
					"/o2ims-infrastructureCluster/v1/nodeClusters",
				},
				Verbs: []string{
					"get",
					"list",
				},
			},
		},
	}

	if err := utils.CreateK8sCR(ctx, t.client, role, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Alarm Server cluster role: %w", err)
	}

	return nil
}

// createAlertmanagerClusterRoleAndBinding creates the cluster role and binding needed for the Alertmanager to be able to
// send alerts to the alarm server.
func (t *reconcilerTask) createAlertmanagerClusterRoleAndBinding(ctx context.Context) error {
	// Cluster Role
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(
				"%s-%s", t.object.Namespace, utils.AlertmanagerObjectName,
			),
		},
		Rules: []rbacv1.PolicyRule{
			{
				NonResourceURLs: []string{
					"/internal/v1/caas-alerts/alertmanager",
				},
				Verbs: []string{
					// Expected to be post given that it is a nonResourceURLs, but it only works with create.
					// Adding post in case something changes in the future. It doesn't hurt to have it.
					"create",
					"post",
				},
			},
		},
	}

	if err := utils.CreateK8sCR(ctx, t.client, role, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Alertmanager cluster role: %w", err)
	}

	// Cluster Role Binding
	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(
				"%s-%s",
				t.object.Namespace, utils.AlertmanagerObjectName,
			),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name: fmt.Sprintf(
				"%s-%s",
				t.object.Namespace, utils.AlertmanagerObjectName,
			),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Namespace: utils.AlertmanagerNamespace,
				Name:      utils.AlertmanagerSA,
			},
		},
	}

	if err := utils.CreateK8sCR(ctx, t.client, binding, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Alertmanager cluster role binding: %w", err)
	}

	return nil
}

// createServerRbacClusterRoleBinding attaches the kube-rbac-proxy cluster role to the server's service account so that
// its instance of the kube-rbac-proxy can access the kubernetes API via the service account credentials.
func (t *reconcilerTask) createServerRbacClusterRoleBinding(ctx context.Context, serverName string) error {
	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(
				"%s-%s",
				t.object.Namespace, serverName+"-kube-rbac-proxy",
			),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name: fmt.Sprintf(
				"%s-%s",
				t.object.Namespace, "kube-rbac-proxy",
			),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Namespace: t.object.Namespace,
				Name:      serverName,
			},
		},
	}

	if err := utils.CreateK8sCR(ctx, t.client, binding, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create RBAC Proxy cluster role binding: %w", err)
	}

	return nil
}

// createServerClusterRoleBinding creates a ClusterRoleBinding for the specified server,
// associating the server's service account with the corresponding ClusterRole.
// The ClusterRoleBinding ensures the server has the necessary permissions defined
// by the ClusterRole.
func (t *reconcilerTask) createServerClusterRoleBinding(ctx context.Context, serverName string) error {
	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(
				"%s-%s",
				t.object.Namespace, serverName,
			),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name: fmt.Sprintf(
				"%s-%s",
				t.object.Namespace, serverName,
			),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Namespace: t.object.Namespace,
				Name:      serverName,
			},
		},
	}

	if err := utils.CreateK8sCR(ctx, t.client, binding, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create %s cluster role binding: %w", serverName, err)
	}
	return nil
}

func (t *reconcilerTask) deployServer(ctx context.Context, serverName string) (utils.InventoryConditionReason, error) {
	t.logger.InfoContext(ctx, "[deploy server]", "Name", serverName)

	// Server variables.
	deploymentVolumes := utils.GetDeploymentVolumes(serverName, t.object)
	deploymentVolumeMounts := utils.GetDeploymentVolumeMounts(serverName, t.object)

	// Build the deployment's metadata.
	deploymentMeta := metav1.ObjectMeta{
		Name:      serverName,
		Namespace: utils.InventoryNamespace,
		Labels: map[string]string{
			"oran/o2ims": t.object.Name,
			"app":        serverName,
		},
	}

	deploymentContainerArgs, err := utils.GetServerArgs(t.object, serverName)
	if err != nil {
		err2 := t.updateInventoryUsedConfigStatus(
			ctx, serverName, deploymentContainerArgs,
			utils.InventoryConditionReasons.ServerArgumentsError, err)
		if err2 != nil {
			return "", fmt.Errorf("failed to update ORANO2ISMUsedConfigStatus: %w", err2)
		}
		return utils.InventoryConditionReasons.ServerArgumentsError, fmt.Errorf("failed to get server arguments: %w", err)
	}
	err = t.updateInventoryUsedConfigStatus(ctx, serverName, deploymentContainerArgs, "", nil)
	if err != nil {
		return "", fmt.Errorf("failed to update ORANO2ISMUsedConfigStatus: %w", err)
	}

	// Select the container image to use:
	image := t.image
	if t.object.Spec.Image != nil {
		image = *t.object.Spec.Image
	}

	rbacProxyImage := os.Getenv(utils.KubeRbacProxyImageName)
	if rbacProxyImage == "" {
		return "", fmt.Errorf("missing %s environment variable value", utils.KubeRbacProxyImageName)
	}

	// Disable privilege escalation for the RBAC proxy
	privilegeEscalation := false

	var envVars []corev1.EnvVar
	if utils.HasDatabase(serverName) {
		envVarName, err := utils.GetServerDatabasePasswordName(serverName)
		if err != nil {
			return "", fmt.Errorf("failed to get server database password: %w", err)
		}
		envVar := corev1.EnvVar{
			Name: envVarName,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: fmt.Sprintf("%s-passwords", utils.InventoryDatabaseServerName),
					},
					Key: envVarName,
				},
			},
		}
		envVars = append(envVars, envVar)
	}

	if utils.HasConnectivityToSMO(serverName) &&
		(t.object.Spec.SmoConfig != nil && t.object.Spec.SmoConfig.OAuthConfig != nil) {
		envVars = append(envVars, []corev1.EnvVar{
			{
				Name: utils.OAuthClientIDEnvName,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: t.object.Spec.SmoConfig.OAuthConfig.ClientSecretName,
						},
						Key: "client-id",
					},
				},
			},
			{
				Name: utils.OAuthClientSecretEnvName,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: t.object.Spec.SmoConfig.OAuthConfig.ClientSecretName,
						},
						Key: "client-secret",
					},
				},
			},
		}...)
	}

	// Common evn for server deployments
	envVars = append(envVars,
		corev1.EnvVar{
			Name: "POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
	)

	// Server specific env var
	if serverName == utils.InventoryAlarmServerName {
		postgresImage := os.Getenv(utils.PostgresImageName)
		if postgresImage == "" {
			return "", fmt.Errorf("missing %s environment variable value", utils.PostgresImageName)
		}
		envVars = append(envVars, corev1.EnvVar{
			Name:  utils.PostgresImageName,
			Value: postgresImage,
		})
	}

	// Build the deployment's spec.
	deploymentSpec := appsv1.DeploymentSpec{
		Replicas: k8sptr.To(int32(1)),
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": serverName,
			},
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"kubectl.kubernetes.io/default-container": utils.ServerContainerName,
				},
				Labels: map[string]string{
					"app": serverName,
				},
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: serverName,
				Volumes:            deploymentVolumes,
				Containers: []corev1.Container{
					{
						Name: utils.RbacContainerName,
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: &privilegeEscalation,
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
							},
						},
						Image: rbacProxyImage,
						Args: []string{
							"--secure-listen-address=0.0.0.0:8443",
							"--upstream=http://127.0.0.1:8000/",
							"--logtostderr=true",
							"--tls-cert-file=/secrets/tls/tls.crt",
							"--tls-private-key-file=/secrets/tls/tls.key",
							"--tls-min-version=VersionTLS12",
							"--v=0"},
						Ports: []corev1.ContainerPort{
							{
								Name:          "https",
								Protocol:      corev1.ProtocolTCP,
								ContainerPort: 8443,
							},
						},
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("500m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("5m"),
								corev1.ResourceMemory: resource.MustParse("64Mi"),
							},
						},
						VolumeMounts: deploymentVolumeMounts,
					},
					{
						Name:            utils.ServerContainerName,
						Image:           image,
						ImagePullPolicy: corev1.PullPolicy(os.Getenv(utils.ImagePullPolicyEnvName)),
						VolumeMounts:    deploymentVolumeMounts,
						Command:         []string{"/usr/bin/oran-o2ims"},
						Args:            deploymentContainerArgs,
						Env:             envVars,
						Ports: []corev1.ContainerPort{
							{
								Name:          "api",
								Protocol:      corev1.ProtocolTCP,
								ContainerPort: 8000,
							},
						},
					},
				},
			},
		},
	}

	if utils.HasDatabase(serverName) {
		deploymentSpec.Template.Spec.InitContainers = []corev1.Container{
			{
				Name:    utils.MigrationContainerName,
				Image:   image,
				Command: []string{"/usr/bin/oran-o2ims"},
				Args:    []string{serverName, "migrate"},
				Env:     envVars,
			},
		}
	}

	// Build the deployment.
	newDeployment := &appsv1.Deployment{
		ObjectMeta: deploymentMeta,
		Spec:       deploymentSpec,
	}

	t.logger.InfoContext(ctx, "[deployManagerServer] Create/Update/Patch Server", "Name", serverName)
	if err := utils.CreateK8sCR(ctx, t.client, newDeployment, t.object, utils.UPDATE); err != nil {
		return "", fmt.Errorf("failed to deploy ManagerServer: %w", err)
	}

	return "", nil
}

func (t *reconcilerTask) createServiceAccount(ctx context.Context, resourceName string) error {
	t.logger.InfoContext(ctx, "[createServiceAccount]")
	// Build the ServiceAccount object.
	serviceAccountMeta := metav1.ObjectMeta{
		Name:      resourceName,
		Namespace: t.object.Namespace,
		Annotations: map[string]string{
			"service.beta.openshift.io/serving-cert-secret-name": fmt.Sprintf("%s-tls", resourceName)},
	}

	newServiceAccount := &corev1.ServiceAccount{
		ObjectMeta: serviceAccountMeta,
	}

	t.logger.InfoContext(ctx, "[createServiceAccount] Create/Update/Patch ServiceAccount: ", "name", resourceName)
	if err := utils.CreateK8sCR(ctx, t.client, newServiceAccount, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create ServiceAccount for deployment: %w", err)
	}

	return nil
}

func (t *reconcilerTask) createService(ctx context.Context, resourceName string, port int32, targetPort string) error {
	t.logger.InfoContext(ctx, "[createService]")
	// Build the Service object.
	serviceMeta := metav1.ObjectMeta{
		Name:      resourceName,
		Namespace: t.object.Namespace,
		Labels: map[string]string{
			"app": resourceName,
		},
		Annotations: map[string]string{
			"service.beta.openshift.io/serving-cert-secret-name": fmt.Sprintf("%s-tls", resourceName),
		},
	}

	serviceSpec := corev1.ServiceSpec{
		Selector: map[string]string{
			"app": resourceName,
		},
		Ports: []corev1.ServicePort{
			{
				Name:       "api",
				Port:       port,
				TargetPort: intstr.FromString(targetPort),
			},
		},
	}

	newService := &corev1.Service{
		ObjectMeta: serviceMeta,
		Spec:       serviceSpec,
	}

	t.logger.InfoContext(ctx, "[createService] Create/Update/Patch Service: ", "name", resourceName)
	if err := utils.CreateK8sCR(ctx, t.client, newService, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Service for deployment: %w", err)
	}

	return nil
}

func (t *reconcilerTask) createIngress(ctx context.Context) error {
	t.logger.InfoContext(ctx, "[createIngress]")
	// Build the Ingress object.
	ingressMeta := metav1.ObjectMeta{
		Name:      utils.InventoryIngressName,
		Namespace: t.object.Namespace,
		Annotations: map[string]string{
			"route.openshift.io/termination": "reencrypt",
		},
	}

	ingressSpec := networkingv1.IngressSpec{
		Rules: []networkingv1.IngressRule{
			{
				Host: t.object.Status.IngressHost,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{
							{
								Path: "/o2ims-infrastructureInventory",
								PathType: func() *networkingv1.PathType {
									pathType := networkingv1.PathTypePrefix
									return &pathType
								}(),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: utils.InventoryResourceServerName,
										Port: networkingv1.ServiceBackendPort{
											Name: utils.InventoryIngressName,
										},
									},
								},
							},
							{
								Path: "/o2ims-infrastructureCluster",
								PathType: func() *networkingv1.PathType {
									pathType := networkingv1.PathTypePrefix
									return &pathType
								}(),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: utils.InventoryClusterServerName,
										Port: networkingv1.ServiceBackendPort{
											Name: utils.InventoryIngressName,
										},
									},
								},
							},
							{
								Path: "/o2ims-infrastructureArtifacts",
								PathType: func() *networkingv1.PathType {
									pathType := networkingv1.PathTypePrefix
									return &pathType
								}(),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: utils.InventoryArtifactsServerName,
										Port: networkingv1.ServiceBackendPort{
											Name: utils.InventoryIngressName,
										},
									},
								},
							},
							{
								Path: "/o2ims-infrastructureProvisioning",
								PathType: func() *networkingv1.PathType {
									pathType := networkingv1.PathTypePrefix
									return &pathType
								}(),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: utils.InventoryProvisioningServerName,
										Port: networkingv1.ServiceBackendPort{
											Name: utils.InventoryIngressName,
										},
									},
								},
							},
							{
								Path: "/o2ims-infrastructureMonitoring",
								PathType: func() *networkingv1.PathType {
									pathType := networkingv1.PathTypePrefix
									return &pathType
								}(),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: utils.InventoryAlarmServerName,
										Port: networkingv1.ServiceBackendPort{
											Name: utils.InventoryIngressName,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	newIngress := &networkingv1.Ingress{
		ObjectMeta: ingressMeta,
		Spec:       ingressSpec,
	}

	t.logger.InfoContext(ctx, "[createIngress] Create/Update/Patch Ingress: ", "name", utils.InventoryIngressName)
	if err := utils.CreateK8sCR(ctx, t.client, newIngress, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Ingress for deployment: %w", err)
	}

	return nil
}

func (t *reconcilerTask) updateInventoryStatusConditions(ctx context.Context, deploymentName string) {
	deployment := &appsv1.Deployment{}
	err := t.client.Get(ctx, types.NamespacedName{Name: deploymentName, Namespace: utils.InventoryNamespace}, deployment)

	if err != nil {
		reason := string(utils.InventoryConditionReasons.ErrorGettingDeploymentInformation)
		if errors.IsNotFound(err) {
			reason = string(utils.InventoryConditionReasons.DeploymentNotFound)
		}
		meta.SetStatusCondition(
			&t.object.Status.Conditions,
			metav1.Condition{
				Type:    string(utils.InventoryConditionTypes.Error),
				Status:  metav1.ConditionTrue,
				Reason:  reason,
				Message: fmt.Sprintf("Error when querying for the %s server", deploymentName),
			},
		)

		meta.SetStatusCondition(
			&t.object.Status.Conditions,
			metav1.Condition{
				Type:    string(utils.InventoryConditionTypes.Ready),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.InventoryConditionReasons.DeploymentsReady),
				Message: "The ORAN O2IMS Deployments are not yet ready",
			},
		)
	} else {
		meta.RemoveStatusCondition(
			&t.object.Status.Conditions,
			string(utils.InventoryConditionTypes.Error))
		meta.RemoveStatusCondition(
			&t.object.Status.Conditions,
			string(utils.InventoryConditionTypes.Ready))
		for _, condition := range deployment.Status.Conditions {
			// Obtain the status directly from the Deployment resources.
			if condition.Type == "Available" {
				meta.SetStatusCondition(
					&t.object.Status.Conditions,
					metav1.Condition{
						Type:    string(utils.MapAvailableDeploymentNameConditionType[deploymentName]),
						Status:  metav1.ConditionStatus(condition.Status),
						Reason:  condition.Reason,
						Message: condition.Message,
					},
				)
			}
		}
	}
}

func (t *reconcilerTask) updateInventoryUsedConfigStatus(
	ctx context.Context, serverName string, deploymentArgs []string,
	errorReason utils.InventoryConditionReason, err error) error {
	t.logger.InfoContext(ctx, "[updateInventoryUsedConfigStatus]")

	if serverName == utils.InventoryResourceServerName {
		t.object.Status.UsedServerConfig.ResourceServerUsedConfig = deploymentArgs
	}

	if serverName == utils.InventoryArtifactsServerName {
		t.object.Status.UsedServerConfig.ArtifactsServerUsedConfig = deploymentArgs
	}

	if serverName == utils.InventoryProvisioningServerName {
		t.object.Status.UsedServerConfig.ProvisioningServerUsedConfig = deploymentArgs
	}

	// If there is an error passed, include it in the condition.
	if err != nil {
		meta.SetStatusCondition(
			&t.object.Status.Conditions,
			metav1.Condition{
				Type:    string(utils.MapErrorDeploymentNameConditionType[serverName]),
				Status:  "True",
				Reason:  string(errorReason),
				Message: err.Error(),
			},
		)

		meta.RemoveStatusCondition(
			&t.object.Status.Conditions,
			string(utils.MapAvailableDeploymentNameConditionType[serverName]))
	} else {
		meta.RemoveStatusCondition(
			&t.object.Status.Conditions,
			string(utils.MapErrorDeploymentNameConditionType[serverName]))
	}

	if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to update inventory used config CR status: %w", err)
	}

	return nil
}

func (t *reconcilerTask) updateInventoryDeploymentStatus(ctx context.Context) error {

	t.logger.InfoContext(ctx, "[updateInventoryDeploymentStatus]")
	if t.object.Spec.AlarmServerConfig.Enabled {
		t.updateInventoryStatusConditions(ctx, utils.InventoryAlarmServerName)
	}

	if t.object.Spec.ResourceServerConfig.Enabled {
		t.updateInventoryStatusConditions(ctx, utils.InventoryResourceServerName)
	}

	if t.object.Spec.ClusterServerConfig.Enabled {
		t.updateInventoryStatusConditions(ctx, utils.InventoryClusterServerName)
	}

	if t.object.Spec.ArtifactsServerConfig.Enabled {
		t.updateInventoryStatusConditions(ctx, utils.InventoryArtifactsServerName)
	}

	t.updateInventoryStatusConditions(ctx, utils.InventoryDatabaseServerName)
	t.updateInventoryStatusConditions(ctx, utils.InventoryProvisioningServerName)

	if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to update inventory deployment CR status: %w", err)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {

	//nolint:wrapcheck
	return ctrl.NewControllerManagedBy(mgr).
		Named("o2ims-inventory").
		For(&inventoryv1alpha1.Inventory{},
			// Watch for create event for Inventory.
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Generation is only updated on spec changes (also on deletion),
					// not metadata or status.
					oldGeneration := e.ObjectOld.GetGeneration()
					newGeneration := e.ObjectNew.GetGeneration()
					// spec update only for Inventory
					return oldGeneration != newGeneration
				},
				CreateFunc:  func(e event.CreateEvent) bool { return true },
				GenericFunc: func(e event.GenericEvent) bool { return false },
				DeleteFunc:  func(e event.DeleteEvent) bool { return false },
			})).
		Owns(&appsv1.Deployment{},
			// Watch for delete events for owned Deployments.
			builder.WithPredicates(predicate.Funcs{
				GenericFunc: func(e event.GenericEvent) bool { return false },
				CreateFunc:  func(e event.CreateEvent) bool { return false },
				DeleteFunc:  func(e event.DeleteEvent) bool { return true },
				UpdateFunc:  func(e event.UpdateEvent) bool { return false },
			})).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(r)
}
