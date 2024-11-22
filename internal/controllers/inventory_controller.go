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

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8sptr "k8s.io/utils/ptr"

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
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;delete;list;watch
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups="internal.open-cluster-management.io",resources=managedclusterinfos,verbs=get;list;watch
//+kubebuilder:rbac:groups="config.openshift.io",resources=clusterversions,verbs=get;list;watch

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

	err = t.createResourceServerClusterRoleBinding(ctx)
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

// setupMetadataServerConfig creates the resource necessary to start the Metadata Server.
func (t *reconcilerTask) setupMetadataServerConfig(ctx context.Context, defaultResult ctrl.Result) (nextReconcile ctrl.Result, err error) {
	nextReconcile = defaultResult

	err = t.createServiceAccount(ctx, utils.InventoryMetadataServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy ServiceAccount for Metadata server.",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the role binding needed to allow the kube-rbac-proxy to interact with the API server to validate incoming
	// API requests from clients.
	err = t.createServerRbacClusterRoleBinding(ctx, utils.InventoryMetadataServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create Metadata server RBAC proxy cluster role binding",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the Service needed for the Metadata server.
	err = t.createService(ctx, utils.InventoryMetadataServerName, utils.DefaultServicePort, utils.DefaultTargetPort)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy Service for Metadata server.",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the metadata-server deployment.
	errorReason, err := t.deployServer(ctx, utils.InventoryMetadataServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy the Metadata server.",
			slog.String("error", err.Error()),
		)
		if errorReason == "" {
			nextReconcile = ctrl.Result{RequeueAfter: 60 * time.Second}
			return nextReconcile, err
		}
	}

	return
}

// setupDeploymentManagerServerConfig creates the resources necessary to start the Deployment Manager Server.
func (t *reconcilerTask) setupDeploymentManagerServerConfig(ctx context.Context, defaultResult ctrl.Result) (nextReconcile ctrl.Result, err error) {
	nextReconcile = defaultResult

	err = t.createServiceAccount(ctx, utils.InventoryDeploymentManagerServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create deployment manager service account",
			slog.String("error", err.Error()),
		)
		return
	}
	err = t.createDeploymentManagerClusterRole(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create deployment manager cluster role",
			slog.String("error", err.Error()),
		)
		return
	}
	err = t.createDeploymentManagerClusterRoleBinding(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create deployment manager cluster role binding",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the role binding needed to allow the kube-rbac-proxy to interact with the API server to validate incoming
	// API requests from clients.
	err = t.createServerRbacClusterRoleBinding(ctx, utils.InventoryDeploymentManagerServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create deployment manager RBAC proxy cluster role binding",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the Service needed for the Deployment Manager server.
	err = t.createService(ctx, utils.InventoryDeploymentManagerServerName, utils.DefaultServicePort, utils.DefaultTargetPort)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy Service for Deployment Manager server.",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the deployment-manager-server deployment.
	errorReason, err := t.deployServer(ctx, utils.InventoryDeploymentManagerServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy the Deployment Manager server.",
			slog.String("error", err.Error()),
		)
		if errorReason == "" {
			nextReconcile = ctrl.Result{RequeueAfter: 60 * time.Second}
			return nextReconcile, err
		}
	}

	return
}

// setupAlarmSubscriptionServerConfig creates the resources necessary to start the Alarm Subscription Server.
func (t *reconcilerTask) setupAlarmSubscriptionServerConfig(ctx context.Context, defaultResult ctrl.Result) (nextReconcile ctrl.Result, err error) {
	nextReconcile = defaultResult

	// Create the needed ServiceAccount.
	err = t.createServiceAccount(ctx, utils.InventoryAlarmSubscriptionServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy ServiceAccount for Alarm Subscription server.",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the role binding needed to allow the kube-rbac-proxy to interact with the API server to validate incoming
	// API requests from clients.
	err = t.createServerRbacClusterRoleBinding(ctx, utils.InventoryAlarmSubscriptionServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create alarm subscription RBAC proxy cluster role binding",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the Service needed for the alarm subscription server.
	err = t.createService(ctx, utils.InventoryAlarmSubscriptionServerName, utils.DefaultServicePort, utils.DefaultTargetPort)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy Service for Alarm Subscription server.",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the alarm subscription-server deployment.
	errorReason, err := t.deployServer(ctx, utils.InventoryAlarmSubscriptionServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy the alarm subscription server.",
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
		CaBundle: []byte(caBundle),
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

		config.ClientId = clientId
		config.ClientSecret = clientSecret
		config.TokenUrl = fmt.Sprintf("%s%s", oAuthConfig.Url, oAuthConfig.TokenEndpoint)
		config.Scopes = oAuthConfig.Scopes
	}

	if t.object.Spec.SmoConfig.Tls != nil && t.object.Spec.SmoConfig.Tls.ClientCertificateName != nil {
		secretName := *t.object.Spec.SmoConfig.Tls.ClientCertificateName
		cert, err := utils.GetCertFromSecret(ctx, t.client, secretName, t.object.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get client certificate from secret: %w", err)
		}

		config.ClientCert = cert
	}

	httpClient, err := utils.SetupOAuthClient(ctx, config)
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

	url := fmt.Sprintf("%s%s", smo.Url, smo.RegistrationEndpoint)
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
			&t.object.Status.DeploymentsStatus.Conditions,
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
				&t.object.Status.DeploymentsStatus.Conditions,
				metav1.Condition{
					Type:    string(utils.InventoryConditionTypes.SmoRegistrationCompleted),
					Status:  metav1.ConditionFalse,
					Reason:  err.Error(),
					Message: fmt.Sprintf("Error registering with SMO at: %s", t.object.Spec.SmoConfig.Url),
				},
			)

			return
		}

		meta.SetStatusCondition(
			&t.object.Status.DeploymentsStatus.Conditions,
			metav1.Condition{
				Type:    string(utils.InventoryConditionTypes.SmoRegistrationCompleted),
				Status:  metav1.ConditionTrue,
				Reason:  string(utils.InventoryConditionReasons.SmoRegistrationSuccessful),
				Message: fmt.Sprintf("Registered with SMO at: %s", t.object.Spec.SmoConfig.Url),
			},
		)
		t.logger.InfoContext(
			ctx, fmt.Sprintf("successfully registered with the SMO at: %s", t.object.Spec.SmoConfig.Url),
		)
	} else {
		t.logger.InfoContext(
			ctx, fmt.Sprintf("already registered with the SMO at: %s", t.object.Spec.SmoConfig.Url),
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

func (t *reconcilerTask) run(ctx context.Context) (nextReconcile ctrl.Result, err error) {
	// Set the default reconcile time to 5 minutes.
	nextReconcile = ctrl.Result{RequeueAfter: 5 * time.Minute}

	if t.object.Status.IngressHost == "" {
		err = t.storeIngressDomain(ctx)
		if err != nil {
			return
		}
	}

	if t.object.Status.ClusterID == "" {
		err = t.storeClusterID(ctx)
		if err != nil {
			return
		}
	}

	// Register with SMO (if necessary)
	err = t.setupSmo(ctx)
	if err != nil {
		return
	}

	// Create the database
	err = t.createDatabase(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create database.",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the needed Ingress if at least one server is required by the Spec.
	if t.object.Spec.MetadataServerConfig.Enabled || t.object.Spec.DeploymentManagerServerConfig.Enabled ||
		t.object.Spec.ResourceServerConfig.Enabled || t.object.Spec.AlarmSubscriptionServerConfig.Enabled {
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
		// The ResourceServer requires the searchAPIBackendURL. Check it has been defined.
		// Create the needed ServiceAccount.
		nextReconcile, err = t.setupResourceServerConfig(ctx, nextReconcile)
		if err != nil {
			return
		}
	}

	// Start the metadata server if required by the Spec.
	if t.object.Spec.MetadataServerConfig.Enabled {
		// Create the needed ServiceAccount.
		nextReconcile, err = t.setupMetadataServerConfig(ctx, nextReconcile)
		if err != nil {
			return
		}
	}

	// Start the deployment server if required by the Spec.
	if t.object.Spec.DeploymentManagerServerConfig.Enabled {
		// Create the service account, role and binding:
		nextReconcile, err = t.setupDeploymentManagerServerConfig(ctx, nextReconcile)
		if err != nil {
			return
		}
	}

	// Start the alarm subscription server if required by the Spec.
	if t.object.Spec.AlarmSubscriptionServerConfig.Enabled {
		// Create the alarm subscription server.
		nextReconcile, err = t.setupAlarmSubscriptionServerConfig(ctx, nextReconcile)
		if err != nil {
			return
		}
	}

	err = t.updateORANO2ISMDeploymentStatus(ctx)
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

func (t *reconcilerTask) createDeploymentManagerClusterRole(ctx context.Context) error {
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(
				"%s-%s", t.object.Namespace, utils.InventoryDeploymentManagerServerName,
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

			// We also need to read the secrets containing the admin kubeConfigs of the
			// clusters.
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"secrets",
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
		return fmt.Errorf("failed to create Deployment Manager cluster role: %w", err)
	}

	return nil
}

func (t *reconcilerTask) createDeploymentManagerClusterRoleBinding(ctx context.Context) error {
	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(
				"%s-%s",
				t.object.Namespace, utils.InventoryDeploymentManagerServerName,
			),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name: fmt.Sprintf(
				"%s-%s",
				t.object.Namespace, utils.InventoryDeploymentManagerServerName,
			),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Namespace: t.object.Namespace,
				Name:      utils.InventoryDeploymentManagerServerName,
			},
		},
	}

	if err := utils.CreateK8sCR(ctx, t.client, binding, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Deployment Manager cluster role binding: %w", err)
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

func (t *reconcilerTask) createResourceServerClusterRoleBinding(ctx context.Context) error {
	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(
				"%s-%s",
				t.object.Namespace, utils.InventoryResourceServerName,
			),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name: fmt.Sprintf(
				"%s-%s",
				t.object.Namespace, utils.InventoryResourceServerName,
			),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Namespace: t.object.Namespace,
				Name:      utils.InventoryResourceServerName,
			},
		},
	}

	if err := utils.CreateK8sCR(ctx, t.client, binding, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Resource Server cluster role binding: %w", err)
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

func (t *reconcilerTask) deployServer(ctx context.Context, serverName string) (utils.InventoryConditionReason, error) {
	t.logger.InfoContext(ctx, "[deploy server]", "Name", serverName)

	// Server variables.
	deploymentVolumes := utils.GetDeploymentVolumes(serverName)
	deploymentVolumeMounts := utils.GetDeploymentVolumeMounts(serverName)

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
		err2 := t.updateORANO2ISMUsedConfigStatus(
			ctx, serverName, deploymentContainerArgs,
			utils.InventoryConditionReasons.ServerArgumentsError, err)
		if err2 != nil {
			return "", fmt.Errorf("failed to update ORANO2ISMUsedConfigStatus: %w", err2)
		}
		return utils.InventoryConditionReasons.ServerArgumentsError, fmt.Errorf("failed to get server arguments: %w", err)
	}
	err = t.updateORANO2ISMUsedConfigStatus(ctx, serverName, deploymentContainerArgs, "", nil)
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
						Image:           rbacProxyImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
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
						ImagePullPolicy: corev1.PullIfNotPresent,
						VolumeMounts:    deploymentVolumeMounts,
						Command:         []string{"/usr/bin/oran-o2ims"},
						Args:            deploymentContainerArgs,
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
								Path: "/o2ims-infrastructureInventory/v1/resourcePools",
								PathType: func() *networkingv1.PathType {
									pathType := networkingv1.PathTypePrefix
									return &pathType
								}(),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "resource-server",
										Port: networkingv1.ServiceBackendPort{
											Name: utils.InventoryIngressName,
										},
									},
								},
							},
							{
								Path: "/o2ims-infrastructureInventory/v1/resourceTypes",
								PathType: func() *networkingv1.PathType {
									pathType := networkingv1.PathTypePrefix
									return &pathType
								}(),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "resource-server",
										Port: networkingv1.ServiceBackendPort{
											Name: utils.InventoryIngressName,
										},
									},
								},
							},
							{
								Path: "/o2ims-infrastructureInventory/v1/subscriptions",
								PathType: func() *networkingv1.PathType {
									pathType := networkingv1.PathTypePrefix
									return &pathType
								}(),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "resource-server",
										Port: networkingv1.ServiceBackendPort{
											Name: utils.InventoryIngressName,
										},
									},
								},
							},
							{
								Path: "/o2ims-infrastructureInventory/v1/deploymentManagers",
								PathType: func() *networkingv1.PathType {
									pathType := networkingv1.PathTypePrefix
									return &pathType
								}(),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "deployment-manager-server",
										Port: networkingv1.ServiceBackendPort{
											Name: utils.InventoryIngressName,
										},
									},
								},
							},
							{
								Path: "/o2ims-infrastructureMonitoring/v1/alarmSubscriptions",
								PathType: func() *networkingv1.PathType {
									pathType := networkingv1.PathTypePrefix
									return &pathType
								}(),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "alarm-subscription-server",
										Port: networkingv1.ServiceBackendPort{
											Name: utils.InventoryIngressName,
										},
									},
								},
							},
							{
								Path: "/",
								PathType: func() *networkingv1.PathType {
									pathType := networkingv1.PathTypePrefix
									return &pathType
								}(),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "metadata-server",
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

func (t *reconcilerTask) updateORANO2ISMStatusConditions(ctx context.Context, deploymentName string) {
	deployment := &appsv1.Deployment{}
	err := t.client.Get(ctx, types.NamespacedName{Name: deploymentName, Namespace: utils.InventoryNamespace}, deployment)

	if err != nil {
		reason := string(utils.InventoryConditionReasons.ErrorGettingDeploymentInformation)
		if errors.IsNotFound(err) {
			reason = string(utils.InventoryConditionReasons.DeploymentNotFound)
		}
		meta.SetStatusCondition(
			&t.object.Status.DeploymentsStatus.Conditions,
			metav1.Condition{
				Type:    string(utils.InventoryConditionTypes.Error),
				Status:  metav1.ConditionTrue,
				Reason:  reason,
				Message: fmt.Sprintf("Error when querying for the %s server", deploymentName),
			},
		)

		meta.SetStatusCondition(
			&t.object.Status.DeploymentsStatus.Conditions,
			metav1.Condition{
				Type:    string(utils.InventoryConditionTypes.Ready),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.InventoryConditionReasons.DeploymentsReady),
				Message: "The ORAN O2IMS Deployments are not yet ready",
			},
		)
	} else {
		meta.RemoveStatusCondition(
			&t.object.Status.DeploymentsStatus.Conditions,
			string(utils.InventoryConditionTypes.Error))
		meta.RemoveStatusCondition(
			&t.object.Status.DeploymentsStatus.Conditions,
			string(utils.InventoryConditionTypes.Ready))
		for _, condition := range deployment.Status.Conditions {
			// Obtain the status directly from the Deployment resources.
			if condition.Type == "Available" {
				meta.SetStatusCondition(
					&t.object.Status.DeploymentsStatus.Conditions,
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

func (t *reconcilerTask) updateORANO2ISMUsedConfigStatus(
	ctx context.Context, serverName string, deploymentArgs []string,
	errorReason utils.InventoryConditionReason, err error) error {
	t.logger.InfoContext(ctx, "[updateORANO2ISMUsedConfigStatus]")

	if serverName == utils.InventoryMetadataServerName {
		t.object.Status.UsedServerConfig.MetadataServerUsedConfig = deploymentArgs
	}

	if serverName == utils.InventoryDeploymentManagerServerName {
		t.object.Status.UsedServerConfig.DeploymentManagerServerUsedConfig = deploymentArgs
	}

	if serverName == utils.InventoryResourceServerName {
		t.object.Status.UsedServerConfig.ResourceServerUsedConfig = deploymentArgs
	}

	// If there is an error passed, include it in the condition.
	if err != nil {
		meta.SetStatusCondition(
			&t.object.Status.DeploymentsStatus.Conditions,
			metav1.Condition{
				Type:    string(utils.MapErrorDeploymentNameConditionType[serverName]),
				Status:  "True",
				Reason:  string(errorReason),
				Message: err.Error(),
			},
		)

		meta.RemoveStatusCondition(
			&t.object.Status.DeploymentsStatus.Conditions,
			string(utils.MapAvailableDeploymentNameConditionType[serverName]))
	} else {
		meta.RemoveStatusCondition(
			&t.object.Status.DeploymentsStatus.Conditions,
			string(utils.MapErrorDeploymentNameConditionType[serverName]))
	}

	if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to update ORANO2ISMUsedConfig CR status: %w", err)
	}

	return nil
}

func (t *reconcilerTask) updateORANO2ISMDeploymentStatus(ctx context.Context) error {

	t.logger.InfoContext(ctx, "[updateORANO2ISMDeploymentStatus]")
	if t.object.Spec.MetadataServerConfig.Enabled {
		t.updateORANO2ISMStatusConditions(ctx, utils.InventoryMetadataServerName)
	}

	if t.object.Spec.DeploymentManagerServerConfig.Enabled {
		t.updateORANO2ISMStatusConditions(ctx, utils.InventoryDeploymentManagerServerName)
	}

	if t.object.Spec.ResourceServerConfig.Enabled {
		t.updateORANO2ISMStatusConditions(ctx, utils.InventoryResourceServerName)
	}

	t.updateORANO2ISMStatusConditions(ctx, utils.InventoryDatabaseServerName)

	if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to update ORANO2ISMDeployment CR status: %w", err)
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
