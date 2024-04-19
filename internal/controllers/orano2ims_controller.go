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
	"k8s.io/client-go/util/retry"
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

//+kubebuilder:rbac:groups=oran.openshift.io,resources=orano2imses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=oran.openshift.io,resources=orano2imses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=oran.openshift.io,resources=orano2imses/finalizers,verbs=update
//+kubebuilder:rbac:groups="apps",resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="networking.k8s.io",resources=ingresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterroles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="cluster.open-cluster-management.io",resources=managedclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconciler reconciles a ORANO2IMS object
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
	object *oranv1alpha1.ORANO2IMS
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ORANO2IMS object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result,
	err error) {
	// Fetch the object:
	object := &oranv1alpha1.ORANO2IMS{}
	if err := r.Client.Get(ctx, request.NamespacedName, object); err != nil {
		if errors.IsNotFound(err) {
			err = nil
			return ctrl.Result{RequeueAfter: 5 * time.Minute}, err
		}
		r.Logger.Error(
			"Unable to fetch ORANO2IMS",
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

func (t *reconcilerTask) run(ctx context.Context) (nextReconcile ctrl.Result, err error) {
	// Set the default reconcile time to 5 minutes.
	nextReconcile = ctrl.Result{RequeueAfter: 5 * time.Minute}

	// Create the needed Ingress if at least one server is required by the Spec.
	if t.object.Spec.MetadataServer || t.object.Spec.DeploymentManagerServer || t.object.Spec.ResourceServer || t.object.Spec.AlarmSubscriptionServer {
		err = t.createIngress(ctx)
		if err != nil {
			t.logger.Error(
				"Failed to deploy Ingress.",
				slog.String("error", err.Error()),
			)
			return
		}
	}

	// Create the client service account.
	err = t.createServiceAccount(ctx, utils.ORANO2IMSClientSAName)
	if err != nil {
		t.logger.Error(
			"Failed to create client service account",
			slog.String("error", err.Error()),
		)
		return
	}

	// Start the resource server if required by the Spec.
	if t.object.Spec.ResourceServer {
		// Create the needed ServiceAccount.
		err = t.createServiceAccount(ctx, utils.ORANO2IMSResourceServerName)
		if err != nil {
			t.logger.Error(
				"Failed to deploy ServiceAccount for the Resource server.",
				slog.String("error", err.Error()),
			)
			return
		}

		// Create the Service needed for the Resource server.
		err = t.createService(ctx, utils.ORANO2IMSResourceServerName)
		if err != nil {
			t.logger.Error(
				"Failed to deploy Service for Resource server.",
				slog.String("error", err.Error()),
			)
			return
		}

		// Create the resource-server deployment.
		err = t.deployServer(ctx, utils.ORANO2IMSResourceServerName)
		if err != nil {
			t.logger.Error(
				"Failed to deploy the Resource server.",
				slog.String("error", err.Error()),
			)
			return
		}
	}

	// Start the metadata server if required by the Spec.
	if t.object.Spec.MetadataServer {
		// Create the needed ServiceAccount.
		err = t.createServiceAccount(ctx, utils.ORANO2IMSMetadataServerName)
		if err != nil {
			t.logger.Error(
				"Failed to deploy ServiceAccount for Metadata server.",
				slog.String("error", err.Error()),
			)
			return
		}

		// Create the Service needed for the Metadata server.
		err = t.createService(ctx, utils.ORANO2IMSMetadataServerName)
		if err != nil {
			t.logger.Error(
				"Failed to deploy Service for Metadata server.",
				slog.String("error", err.Error()),
			)
			return
		}

		// Create the metadata-server deployment.
		err = t.deployServer(ctx, utils.ORANO2IMSMetadataServerName)
		if err != nil {
			t.logger.Error(
				"Failed to deploy the Metadata server.",
				slog.String("error", err.Error()),
			)
			return
		}
	}

	// Start the deployment server if required by the Spec.
	if t.object.Spec.DeploymentManagerServer {
		// Create the service account, role and binding:
		err = t.createServiceAccount(ctx, utils.ORANO2IMSDeploymentManagerServerName)
		if err != nil {
			t.logger.Error(
				"Failed to create deployment manager service account",
				slog.String("error", err.Error()),
			)
			return
		}
		err = t.createDeploymentManagerClusterRole(ctx)
		if err != nil {
			t.logger.Error(
				"Failed to create deployment manager cluster role",
				slog.String("error", err.Error()),
			)
			return
		}
		err = t.createDeploymentManagerClusterRoleBinding(ctx)
		if err != nil {
			t.logger.Error(
				"Failed to create deployment manager cluster role binding",
				slog.String("error", err.Error()),
			)
			return
		}

		// Create authz ConfigMap.
		err = t.createConfigMap(ctx, utils.ORANO2IMSConfigMapName)
		if err != nil {
			t.logger.Error(
				"Failed to deploy ConfigMap for Deployment Manager server.",
				slog.String("error", err.Error()),
			)
			return
		}

		// Create the Service needed for the Deployment Manager server.
		err = t.createService(ctx, utils.ORANO2IMSDeploymentManagerServerName)
		if err != nil {
			t.logger.Error(
				"Failed to deploy Service for Deployment Manager server.",
				slog.String("error", err.Error()),
			)
			return
		}

		// Create the deployment-manager-server deployment.
		err = t.deployServer(ctx, utils.ORANO2IMSDeploymentManagerServerName)
		if err != nil {
			t.logger.Error(
				"Failed to deploy the Deployment Manager server.",
				slog.String("error", err.Error()),
			)
			return
		}
	}
	// Start the alarm subscription server if required by the Spec.
	if t.object.Spec.AlarmSubscriptionServer {
		// Create authz ConfigMap.
		err = t.createConfigMap(ctx, utils.ORANO2IMSConfigMapName)
		if err != nil {
			t.logger.Error(
				"Failed to deploy ConfigMap for alarm subscription server.",
				slog.String("error", err.Error()),
			)
			return
		}

		// Create the needed ServiceAccount.
		err = t.createServiceAccount(ctx, utils.ORANO2IMSAlarmSubscriptionServerName)
		if err != nil {
			t.logger.Error(
				"Failed to deploy ServiceAccount for Alarm Subscription server.",
				slog.String("error", err.Error()),
			)
			return
		}

		// Create the Service needed for the alarm subscription server.
		err = t.createService(ctx, utils.ORANO2IMSAlarmSubscriptionServerName)
		if err != nil {
			t.logger.Error(
				"Failed to deploy Service for Alarm Subscription server.",
				slog.String("error", err.Error()),
			)
			return
		}

		// Create the alarm subscription-server deployment.
		err = t.deployServer(ctx, utils.ORANO2IMSAlarmSubscriptionServerName)
		if err != nil {
			t.logger.Error(
				"Failed to deploy the alarm subscription server.",
				slog.String("error", err.Error()),
			)
			return
		}
	}

	err = t.updateORANO2ISMStatus(ctx)
	if err != nil {
		t.logger.Error(
			"Failed to update status for ORANO2IMS",
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
				"%s-%s", t.object.Namespace, utils.ORANO2IMSDeploymentManagerServerName,
			),
		},
		Rules: []rbacv1.PolicyRule{
			// We need to read manged clusters, as that is the main source for the
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

			// We also need to read the secrets containing the admin kubeconfigs of the
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
	return utils.CreateK8sCR(ctx, t.client, role, t.object, utils.UPDATE)
}

func (t *reconcilerTask) createDeploymentManagerClusterRoleBinding(ctx context.Context) error {
	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(
				"%s-%s",
				t.object.Namespace, utils.ORANO2IMSDeploymentManagerServerName,
			),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name: fmt.Sprintf(
				"%s-%s",
				t.object.Namespace, utils.ORANO2IMSDeploymentManagerServerName,
			),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Namespace: t.object.Namespace,
				Name:      utils.ORANO2IMSDeploymentManagerServerName,
			},
		},
	}
	return utils.CreateK8sCR(ctx, t.client, binding, t.object, utils.UPDATE)
}

func (t *reconcilerTask) deployServer(ctx context.Context, serverName string) error {
	t.logger.Info("[deploy server]", "Name", serverName)

	// Server variables.
	deploymentVolumes := utils.GetDeploymentVolumes(serverName)
	deploymentVolumeMounts := utils.GetDeploymentVolumeMounts(serverName)

	// Build the deployment's metadata.
	deploymentMeta := metav1.ObjectMeta{
		Name:      serverName,
		Namespace: utils.ORANO2IMSNamespace,
		Labels: map[string]string{
			"oran/o2ims": t.object.Name,
			"app":        serverName,
		},
	}

	deploymentContainerArgs, err := utils.BuildServerContainerArgs(t.object, serverName)
	if err != nil {
		return err
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

	t.logger.Info("[deployManagerServer] Create/Update/Patch Server", "Name", serverName)
	return utils.CreateK8sCR(ctx, t.client, newDeployment, t.object, utils.UPDATE)
}

func (t *reconcilerTask) createConfigMap(ctx context.Context, resourceName string) error {
	t.logger.Info("[createConfigMap]")

	// Build the ConfigMap object.
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: t.object.Namespace,
		},
		Data: map[string]string{
			"acl.yaml": fmt.Sprintf("- claim: sub\n  pattern: ^system:serviceaccount:%s:client$", utils.ORANO2IMSNamespace),
		},
	}

	t.logger.Info("[createService] Create/Update/Patch Service: ", "name", resourceName)
	return utils.CreateK8sCR(ctx, t.client, configMap, t.object, utils.UPDATE)
}

func (t *reconcilerTask) createServiceAccount(ctx context.Context, resourceName string) error {
	t.logger.Info("[createServiceAccount]")
	// Build the ServiceAccount object.
	serviceAccountMeta := metav1.ObjectMeta{
		Name:      resourceName,
		Namespace: t.object.Namespace,
	}

	if resourceName != utils.ORANO2IMSClientSAName {
		serviceAccountMeta.Annotations = map[string]string{
			"service.beta.openshift.io/serving-cert-secret-name": fmt.Sprintf("%s-tls", resourceName),
		}
	}

	newServiceAccount := &corev1.ServiceAccount{
		ObjectMeta: serviceAccountMeta,
	}

	t.logger.Info("[createServiceAccount] Create/Update/Patch ServiceAccount: ", "name", resourceName)
	return utils.CreateK8sCR(ctx, t.client, newServiceAccount, t.object, utils.UPDATE)
}

func (t *reconcilerTask) createService(ctx context.Context, resourceName string) error {
	t.logger.Info("[createService]")
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

	t.logger.Info("[createService] Create/Update/Patch Service: ", "name", resourceName)
	return utils.CreateK8sCR(ctx, t.client, newService, t.object, utils.PATCH)
}

func (t *reconcilerTask) createIngress(ctx context.Context) error {
	t.logger.Info("[createIngress]")
	// Build the Ingress object.
	ingressMeta := metav1.ObjectMeta{
		Name:      utils.ORANO2IMSIngressName,
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
											Name: utils.ORANO2IMSIngressName,
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
											Name: utils.ORANO2IMSIngressName,
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
											Name: utils.ORANO2IMSIngressName,
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
											Name: utils.ORANO2IMSIngressName,
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
											Name: utils.ORANO2IMSIngressName,
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

	t.logger.Info("[createIngress] Create/Update/Patch Ingress: ", "name", utils.ORANO2IMSIngressName)
	return utils.CreateK8sCR(ctx, t.client, newIngress, t.object, utils.UPDATE)
}

func (t *reconcilerTask) updateORANO2ISMStatusConditions(ctx context.Context, deploymentName string) {
	deployment := &appsv1.Deployment{}
	err := t.client.Get(ctx, types.NamespacedName{Name: deploymentName, Namespace: utils.ORANO2IMSNamespace}, deployment)

	if err != nil {
		reason := string(utils.ORANO2IMSConditionReasons.ErrorGettingDeploymentInformation)
		if errors.IsNotFound(err) {
			reason = string(utils.ORANO2IMSConditionReasons.DeploymentNotFound)
		}
		meta.SetStatusCondition(
			&t.object.Status.DeploymentsStatus.Conditions,
			metav1.Condition{
				Type:    string(utils.ORANO2IMSConditionTypes.Error),
				Status:  metav1.ConditionTrue,
				Reason:  reason,
				Message: fmt.Sprintf("Error when querying for the %s server", deploymentName),
			},
		)

		meta.SetStatusCondition(
			&t.object.Status.DeploymentsStatus.Conditions,
			metav1.Condition{
				Type:    string(utils.ORANO2IMSConditionTypes.Ready),
				Status:  metav1.ConditionFalse,
				Reason:  string(utils.ORANO2IMSConditionReasons.DeploymentsReady),
				Message: "The ORAN O2IMS Deployments are not yet ready",
			},
		)
	} else {
		for _, condition := range deployment.Status.Conditions {
			if condition.Type == "Available" {
				meta.SetStatusCondition(
					&t.object.Status.DeploymentsStatus.Conditions,
					metav1.Condition{
						Type:    string(utils.MapDeploymentNameConditionType[deploymentName]),
						Status:  metav1.ConditionStatus(condition.Status),
						Reason:  condition.Reason,
						Message: condition.Message,
					},
				)
			}
		}
	}
}

func (t *reconcilerTask) updateORANO2ISMStatus(ctx context.Context) error {

	t.logger.Info("[updateORANO2ISMStatus]")
	if t.object.Spec.MetadataServer {
		t.updateORANO2ISMStatusConditions(ctx, utils.ORANO2IMSMetadataServerName)
	}

	if t.object.Spec.DeploymentManagerServer {
		t.updateORANO2ISMStatusConditions(ctx, utils.ORANO2IMSDeploymentManagerServerName)
	}

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := t.client.Status().Update(ctx, t.object)
		return err
	})

	if err != nil {
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {

	return ctrl.NewControllerManagedBy(mgr).
		Named("orano2ims").
		For(&oranv1alpha1.ORANO2IMS{},
			// Watch for create event for orano2ims.
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Generation is only updated on spec changes (also on deletion),
					// not metadata or status.
					oldGeneration := e.ObjectOld.GetGeneration()
					newGeneration := e.ObjectNew.GetGeneration()
					// spec update only for orano2ims
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
