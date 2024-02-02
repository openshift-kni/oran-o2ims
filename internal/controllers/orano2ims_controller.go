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

package controller

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/retry"
	k8sptr "k8s.io/utils/ptr"

	"github.com/go-logr/logr"
	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// ORANO2IMSReconciler reconciles a ORANO2IMS object
type ORANO2IMSReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=oran.openshift.io,resources=orano2ims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=oran.openshift.io,resources=orano2ims/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=oran.openshift.io,resources=orano2ims/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ORANO2IMS object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *ORANO2IMSReconciler) Reconcile(ctx context.Context, req ctrl.Request) (nextReconcile ctrl.Result, err error) {
	orano2ims := &oranv1alpha1.ORANO2IMS{}
	if err := r.Get(ctx, req.NamespacedName, orano2ims); err != nil {
		if errors.IsNotFound(err) {
			err = nil
			return ctrl.Result{RequeueAfter: 5 * time.Minute}, err
		}

		r.Log.Error(err, "Unable to fetch ORANO2IMS")
	}

	// Set the default reconcile time to 5 minutes.
	nextReconcile = ctrl.Result{RequeueAfter: 5 * time.Minute}

	// Create the needed Ingress if at least one server is required by the Spec.
	if orano2ims.Spec.MetadataServer || orano2ims.Spec.DeploymentManagerServer {
		err = r.createIngress(ctx, orano2ims)
		if err != nil {
			r.Log.Error(err, "Failed to deploy Service for Metadata server.")
			return
		}
	}

	// Start the metadata server if required by the Spec.
	if orano2ims.Spec.MetadataServer {
		// Create the needed ServiceAccount.
		err = r.createServiceAccount(ctx, orano2ims, utils.ORANO2IMSMetadataServerName)
		if err != nil {
			r.Log.Error(err, "Failed to deploy ServiceAccount for Metadata server.")
			return
		}

		// Create the Service needed for the Metadata server.
		err = r.createService(ctx, orano2ims, utils.ORANO2IMSMetadataServerName)
		if err != nil {
			r.Log.Error(err, "Failed to deploy Service for Metadata server.")
			return
		}

		// Create the metadata-server deployment.
		err = r.deployMetadataServer(ctx, orano2ims)
		if err != nil {
			r.Log.Error(err, "Failed to deploy the Metadata server.")
			return
		}
	}

	// Start the deployment server if required by the Spec.
	if orano2ims.Spec.DeploymentManagerServer {
		// Create the client ServiceAccount.
		err = r.createServiceAccount(ctx, orano2ims, utils.ORANO2IMSClientSAName)
		if err != nil {
			r.Log.Error(err, fmt.Sprintf("Failed to deploy ServiceAccount %s for Deployment Manager server.", utils.ORANO2IMSClientSAName))
			return
		}

		// Create authz ConfigMap.
		err = r.createConfigMap(ctx, orano2ims, utils.ORANO2IMSConfigMapName)
		if err != nil {
			r.Log.Error(err, "Failed to deploy ConfigMap for Deployment Manager server.")
			return
		}

		// Create the needed ServiceAccount.
		err = r.createServiceAccount(ctx, orano2ims, utils.ORANO2IMSDeploymentManagerServerName)
		if err != nil {
			r.Log.Error(err, "Failed to deploy ServiceAccount for Deployment Manager server.")
			return
		}

		// Create the Service needed for the Metadata server.
		err = r.createService(ctx, orano2ims, utils.ORANO2IMSDeploymentManagerServerName)
		if err != nil {
			r.Log.Error(err, "Failed to deploy Service for Deployment Manager server.")
			return
		}

		// Create the metadata-server deployment.
		err = r.deployManagerServer(ctx, orano2ims)
		if err != nil {
			r.Log.Error(err, "Failed to deploy the Deployment Manager server.")
			return
		}
	}

	err = r.updateORANO2ISMStatus(ctx, orano2ims)
	if err != nil {
		r.Log.Error(err, fmt.Sprintf("Failed to update status for ORANO2IMS %s.", orano2ims.Name))
		nextReconcile = ctrl.Result{RequeueAfter: 30 * time.Second}
	}
	return
}

func (r *ORANO2IMSReconciler) deployMetadataServer(ctx context.Context, orano2ims *oranv1alpha1.ORANO2IMS) error {
	r.Log.Info("[deployMetadataServer]")

	// Build the deployment's metadata.
	deploymentMeta := metav1.ObjectMeta{
		Name:      utils.ORANO2IMSMetadataServerName,
		Namespace: utils.ORANO2IMSNamespace,
		Labels: map[string]string{
			"oran-o2ims": orano2ims.Name,
			"app":        utils.ORANO2IMSMetadataServerName,
		},
	}

	// Build the deployment's spec.
	deploymentSpec := appsv1.DeploymentSpec{
		Replicas: k8sptr.To(int32(1)),
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": utils.ORANO2IMSMetadataServerName,
			},
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"app": utils.ORANO2IMSMetadataServerName,
				},
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: utils.ORANO2IMSMetadataServerName,
				Volumes: []corev1.Volume{
					{
						Name: "tls",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: fmt.Sprintf("%s-tls", utils.ORANO2IMSMetadataServerName),
							},
						},
					},
				},
				Containers: []corev1.Container{
					{
						Name:            "server",
						Image:           utils.ORANImage,
						ImagePullPolicy: "Always",
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "tls",
								MountPath: "/secrets/tls",
							},
						},
						Command: []string{"/usr/bin/oran-o2ims"},
						Args: []string{
							"start",
							"metadata-server",
							"--log-level=debug",
							"--log-file=stdout",
							"--api-listener-address=0.0.0.0:8000",
							"--api-listener-tls-crt=/secrets/tls/tls.crt",
							"--api-listener-tls-key=/secrets/tls/tls.key",
							fmt.Sprintf("--cloud-id=%s", orano2ims.Spec.CloudId),
							fmt.Sprintf("--external-address=https://%s", orano2ims.Spec.IngressHost),
						},
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

	r.Log.Info("[deployMetadataServer] Create/Update/Patch the Metadata Server")
	return utils.CreateK8sCR(ctx, r.Client, utils.ORANO2IMSMetadataServerName, utils.ORANO2IMSNamespace,
		newDeployment, orano2ims, &appsv1.Deployment{}, r.Scheme, utils.UPDATE)
}

func (r *ORANO2IMSReconciler) deployManagerServer(ctx context.Context, orano2ims *oranv1alpha1.ORANO2IMS) error {
	r.Log.Info("[deployManagerServer]")

	// Build the deployment's metadata.
	deploymentMeta := metav1.ObjectMeta{
		Name:      utils.ORANO2IMSDeploymentManagerServerName,
		Namespace: utils.ORANO2IMSNamespace,
		Labels: map[string]string{
			"oran/o2ims": orano2ims.Name,
			"app":        utils.ORANO2IMSDeploymentManagerServerName,
		},
	}

	// Build the deployment's spec.
	deploymentSpec := appsv1.DeploymentSpec{
		Replicas: k8sptr.To(int32(1)),
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": utils.ORANO2IMSDeploymentManagerServerName,
			},
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"app": utils.ORANO2IMSDeploymentManagerServerName,
				},
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: utils.ORANO2IMSDeploymentManagerServerName,
				Volumes: []corev1.Volume{
					{
						Name: "tls",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: fmt.Sprintf("%s-tls", utils.ORANO2IMSDeploymentManagerServerName),
							},
						},
					},
					{
						Name: "authz",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "authz"},
							},
						},
					},
				},
				Containers: []corev1.Container{
					{
						Name:            "server",
						Image:           utils.ORANImage,
						ImagePullPolicy: "Always",
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "tls",
								MountPath: "/secrets/tls",
							},
							{
								Name:      "authz",
								MountPath: "/configmaps/authz",
							},
						},
						Command: []string{"/usr/bin/oran-o2ims"},
						Args: []string{
							"start",
							"deployment-manager-server",
							"--log-level=debug",
							"--log-file=stdout",
							"--api-listener-address=0.0.0.0:8000",
							"--api-listener-tls-crt=/secrets/tls/tls.crt",
							"--api-listener-tls-key=/secrets/tls/tls.key",
							"--authn-jwks-url=https://kubernetes.default.svc/openid/v1/jwks",
							"--authn-jwks-token-file=/run/secrets/kubernetes.io/serviceaccount/token",
							"--authn-jwks-ca-file=/run/secrets/kubernetes.io/serviceaccount/ca.crt",
							"--authz-acl-file=/configmaps/authz/acl.yaml",
							fmt.Sprintf("--cloud-id=%s", orano2ims.Spec.CloudId),
							fmt.Sprintf("--backend-url=%s", orano2ims.Spec.BackendURL),
							fmt.Sprintf("--backend-token=%s", orano2ims.Spec.BackendToken),
							fmt.Sprintf("--backend-type=%s", orano2ims.Spec.BackendType),
							fmt.Sprintln(
								// TODO: properly hold the extensions instead of hardcoding them.
								"--extensions={\n",
								"\"country\": .metadata.labels[\"country\"],\n",
								"\"version\": .metadata.labels[\"openshiftVersion\"],\n",
								"\"hub\": .metadata.annotations[\"global-hub.open-cluster-management.io/managed-by\"],",
								"}"),
						},
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

	r.Log.Info("[deployManagerServer] Create/Update/Patch the Manager Server")
	return utils.CreateK8sCR(ctx, r.Client, utils.ORANO2IMSDeploymentManagerServerName, orano2ims.Namespace,
		newDeployment, orano2ims, &appsv1.Deployment{}, r.Scheme, utils.UPDATE)
}

func (r *ORANO2IMSReconciler) createConfigMap(ctx context.Context, orano2ims *oranv1alpha1.ORANO2IMS, resourceName string) error {
	r.Log.Info("[createConfigMap]")

	// Build the ConfigMap object.
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: orano2ims.Namespace,
		},
		Data: map[string]string{
			"acl.yaml": fmt.Sprintf("- claim: sub\n  pattern: ^system:serviceaccount:%s:client$", utils.ORANO2IMSNamespace),
		},
	}

	r.Log.Info("[createService] Create/Update/Patch Service: ", "name", resourceName)
	return utils.CreateK8sCR(ctx, r.Client, resourceName, orano2ims.Namespace,
		configMap, orano2ims, &corev1.ConfigMap{}, r.Scheme, utils.UPDATE)
}

func (r *ORANO2IMSReconciler) createServiceAccount(ctx context.Context, orano2ims *oranv1alpha1.ORANO2IMS, resourceName string) error {
	r.Log.Info("[createServiceAccount]")
	// Build the ServiceAccount object.
	serviceAccountMeta := metav1.ObjectMeta{
		Name:      resourceName,
		Namespace: orano2ims.Namespace,
	}

	if resourceName != utils.ORANO2IMSClientSAName {
		serviceAccountMeta.Annotations = map[string]string{
			"service.beta.openshift.io/serving-cert-secret-name": fmt.Sprintf("%s-tls", resourceName),
		}
	}

	newServiceAccount := &corev1.ServiceAccount{
		ObjectMeta: serviceAccountMeta,
	}

	r.Log.Info("[createServiceAccount] Create/Update/Patch ServiceAccount: ", "name", resourceName)
	return utils.CreateK8sCR(ctx, r.Client, resourceName, orano2ims.Namespace,
		newServiceAccount, orano2ims, &corev1.ServiceAccount{}, r.Scheme, utils.UPDATE)
}

func (r *ORANO2IMSReconciler) createService(ctx context.Context, orano2ims *oranv1alpha1.ORANO2IMS, resourceName string) error {
	r.Log.Info("[createService]")
	// Build the Service object.
	serviceMeta := metav1.ObjectMeta{
		Name:      resourceName,
		Namespace: orano2ims.Namespace,
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

	r.Log.Info("[createService] Create/Update/Patch Service: ", "name", resourceName)
	return utils.CreateK8sCR(ctx, r.Client, resourceName, orano2ims.Namespace,
		newService, orano2ims, &corev1.Service{}, r.Scheme, utils.PATCH)
}

func (r *ORANO2IMSReconciler) createIngress(ctx context.Context, orano2ims *oranv1alpha1.ORANO2IMS) error {
	r.Log.Info("[createIngress]")
	// Build the Ingress object.
	ingressMeta := metav1.ObjectMeta{
		Name:      utils.ORANO2IMSIngressName,
		Namespace: orano2ims.ObjectMeta.Namespace,
		Annotations: map[string]string{
			"route.openshift.io/termination": "reencrypt",
		},
	}

	ingressSpec := networkingv1.IngressSpec{
		Rules: []networkingv1.IngressRule{
			{
				Host: orano2ims.Spec.IngressHost,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{
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

	r.Log.Info("[createIngress] Create/Update/Patch Ingress: ", "name", utils.ORANO2IMSIngressName)
	return utils.CreateK8sCR(ctx, r.Client, utils.ORANO2IMSIngressName, orano2ims.Namespace,
		newIngress, orano2ims, &networkingv1.Ingress{}, r.Scheme, utils.UPDATE)
}

func (r *ORANO2IMSReconciler) updateORANO2ISMStatusConditions(ctx context.Context, orano2ims *oranv1alpha1.ORANO2IMS, deploymentName string) {
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{Name: deploymentName, Namespace: utils.ORANO2IMSNamespace}, deployment)

	if err != nil {
		reason := string(utils.ORANO2IMSConditionReasons.ErrorGettingDeploymentInformation)
		if errors.IsNotFound(err) {
			reason = string(utils.ORANO2IMSConditionReasons.DeploymentNotFound)
		}
		meta.SetStatusCondition(
			&orano2ims.Status.DeploymentsStatus.Conditions,
			metav1.Condition{
				Type:    string(utils.ORANO2IMSConditionTypes.Error),
				Status:  metav1.ConditionTrue,
				Reason:  reason,
				Message: fmt.Sprintf("Error when querying for the %s server", deploymentName),
			},
		)

		meta.SetStatusCondition(
			&orano2ims.Status.DeploymentsStatus.Conditions,
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
					&orano2ims.Status.DeploymentsStatus.Conditions,
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

func (r *ORANO2IMSReconciler) updateORANO2ISMStatus(ctx context.Context, orano2ims *oranv1alpha1.ORANO2IMS) error {

	r.Log.Info("[updateORANO2ISMStatus]")
	if orano2ims.Spec.MetadataServer {
		r.updateORANO2ISMStatusConditions(ctx, orano2ims, utils.ORANO2IMSMetadataServerName)
	}

	if orano2ims.Spec.DeploymentManagerServer {
		r.updateORANO2ISMStatusConditions(ctx, orano2ims, utils.ORANO2IMSDeploymentManagerServerName)
	}

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := r.Status().Update(ctx, orano2ims)
		return err
	})

	if err != nil {
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ORANO2IMSReconciler) SetupWithManager(mgr ctrl.Manager) error {

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
