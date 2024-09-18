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
	"context"
	"fmt"
	"log/slog"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8sptr "k8s.io/utils/ptr"

	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"

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
	object *oranv1alpha1.Inventory
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
	object := &oranv1alpha1.Inventory{}
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

	// Create the Service needed for the Resource server.
	err = t.createService(ctx, utils.InventoryResourceServerName)
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

	// Create the Service needed for the Metadata server.
	err = t.createService(ctx, utils.InventoryMetadataServerName)
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

	// Create authz ConfigMap.
	err = t.createConfigMap(ctx, utils.InventoryConfigMapName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy ConfigMap for Deployment Manager server.",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the Service needed for the Deployment Manager server.
	err = t.createService(ctx, utils.InventoryDeploymentManagerServerName)
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

	err = t.createConfigMap(ctx, utils.InventoryConfigMapName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy ConfigMap for alarm subscription server.",
			slog.String("error", err.Error()),
		)
		return
	}

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

	// Create the Service needed for the alarm subscription server.
	err = t.createService(ctx, utils.InventoryAlarmSubscriptionServerName)
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

func (t *reconcilerTask) run(ctx context.Context) (nextReconcile ctrl.Result, err error) {
	// Set the default reconcile time to 5 minutes.
	nextReconcile = ctrl.Result{RequeueAfter: 5 * time.Minute}

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
	}

	// Create the client service account.
	err = t.createServiceAccount(ctx, utils.InventoryClientSAName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create client service account",
			slog.String("error", err.Error()),
		)
		return
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
		// Create authz ConfigMap.
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

	deploymentContainerArgs, err := utils.GetServerArgs(ctx, t.client, t.object, serverName)
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
	image := t.object.Spec.Image
	if image == "" {
		image = t.image
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
				Labels: map[string]string{
					"app": serverName,
				},
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: serverName,
				Volumes:            deploymentVolumes,
				Containers: []corev1.Container{
					{
						Name:            "server",
						Image:           image,
						ImagePullPolicy: "Always",
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

func (t *reconcilerTask) createConfigMap(ctx context.Context, resourceName string) error {
	t.logger.InfoContext(ctx, "[createConfigMap]")

	// Build the ConfigMap object.
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: t.object.Namespace,
		},
		Data: map[string]string{
			"acl.yaml": fmt.Sprintf("- claim: sub\n  pattern: ^system:serviceaccount:%s:client$", utils.InventoryNamespace),
		},
	}

	t.logger.InfoContext(ctx, "[createService] Create/Update/Patch Service: ", "name", resourceName)
	if err := utils.CreateK8sCR(ctx, t.client, configMap, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create ConfigMap for deployment: %w", err)
	}

	return nil
}

func (t *reconcilerTask) createServiceAccount(ctx context.Context, resourceName string) error {
	t.logger.InfoContext(ctx, "[createServiceAccount]")
	// Build the ServiceAccount object.
	serviceAccountMeta := metav1.ObjectMeta{
		Name:      resourceName,
		Namespace: t.object.Namespace,
	}

	if resourceName != utils.InventoryClientSAName {
		serviceAccountMeta.Annotations = map[string]string{
			"service.beta.openshift.io/serving-cert-secret-name": fmt.Sprintf("%s-tls", resourceName),
		}
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

func (t *reconcilerTask) createService(ctx context.Context, resourceName string) error {
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
				Port:       8000,
				TargetPort: intstr.FromString("api"),
			},
		},
	}

	newService := &corev1.Service{
		ObjectMeta: serviceMeta,
		Spec:       serviceSpec,
	}

	t.logger.InfoContext(ctx, "[createService] Create/Update/Patch Service: ", "name", resourceName)
	if err := utils.CreateK8sCR(ctx, t.client, newService, t.object, utils.PATCH); err != nil {
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
				Host: t.object.Spec.IngressHost,
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
		For(&oranv1alpha1.Inventory{},
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
