/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
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
	"strconv"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8sptr "k8s.io/utils/ptr"
	"k8s.io/utils/strings/slices"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

//+kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents,verbs=get;list;watch
//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=prometheusrules,verbs=get;list;watch
//+kubebuilder:rbac:groups=operator.openshift.io,resources=ingresscontrollers,verbs=get;list;watch
//+kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
//+kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create
//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=inventories,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=inventories/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ocloud.openshift.io,resources=inventories/finalizers,verbs=update
//+kubebuilder:rbac:groups="apps",resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="networking.k8s.io",resources=ingresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="cluster.open-cluster-management.io",resources=managedclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;delete;list;watch;update
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups="config.openshift.io",resources=clusterversions,verbs=get;list;watch
//+kubebuilder:rbac:urls="/internal/v1/caas-alerts/alertmanager",verbs=create
//+kubebuilder:rbac:urls="/o2ims-infrastructureCluster/v1/nodeClusterTypes",verbs=get;list
//+kubebuilder:rbac:urls="/o2ims-infrastructureCluster/v1/nodeClusters",verbs=get;list
//+kubebuilder:rbac:urls="/o2ims-infrastructureCluster/v1/alarmDictionaries",verbs=get;list
//+kubebuilder:rbac:urls="/o2ims-infrastructureCluster/v1/nodeClusterTypes/*",verbs=get
//+kubebuilder:rbac:urls="/o2ims-infrastructureCluster/v1/nodeClusters/*",verbs=get
//+kubebuilder:rbac:urls="/o2ims-infrastructureCluster/v1/alarmDictionaries/*",verbs=get
//+kubebuilder:rbac:urls="/o2ims-infrastructureInventory/v1/internal/resources/*",verbs=get
//+kubebuilder:rbac:urls="/o2ims-infrastructureInventory/v1/resourceTypes",verbs=get;list
//+kubebuilder:rbac:urls="/o2ims-infrastructureInventory/v1/resourceTypes/*",verbs=get
//+kubebuilder:rbac:groups="batch",resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=nodeallocationrequests,verbs=get;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=nodeallocationrequests/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=nodeallocationrequests/finalizers,verbs=update;patch
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=allocatednodes,verbs=get;create;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=allocatednodes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=allocatednodes/finalizers,verbs=update;patch
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=hardwareprofiles,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=hardwareprofiles/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=metal3.io,resources=baremetalhosts,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=metal3.io,resources=preprovisioningimages,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=metal3.io,resources=hostfirmwaresettings,verbs=get;create;list;watch;update;patch
//+kubebuilder:rbac:groups=metal3.io,resources=hostfirmwarecomponents,verbs=get;create;list;watch;update;patch
//+kubebuilder:rbac:groups=metal3.io,resources=hostupdatepolicies,verbs=get;create;list;watch;update;patch
//+kubebuilder:rbac:groups=metal3.io,resources=firmwareschemas,verbs=get;list;watch

// Reconciler reconciles a Inventory object
type Reconciler struct {
	client.Client
	Logger    *slog.Logger
	Image     string
	setupOnce sync.Once
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

const registerOnRestartAnnotation = "ocloud.openshift.io/register-on-restart"

var registerOnRestart = false

// setRegisterOnRestart initializes the `registerOnRestart` value from an annotation.
func setRegisterOnRestart(ctx context.Context, logger *slog.Logger, object *inventoryv1alpha1.Inventory) {
	if annotation, ok := object.Annotations[registerOnRestartAnnotation]; ok {
		if value, err := strconv.ParseBool(annotation); err == nil {
			registerOnRestart = value
			logger.WarnContext(ctx, "SMO registration will be repeated on all subsequent restarts")
		}
	}
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
	startTime := time.Now()
	result = ctrl.Result{RequeueAfter: 5 * time.Minute}

	// Add standard reconciliation context
	ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, request, "Inventory")

	defer func() {
		duration := time.Since(startTime)
		if err != nil {
			r.Logger.ErrorContext(ctx, "Reconciliation failed",
				slog.Duration("duration", duration),
				slog.Any("error", err))
		} else {
			r.Logger.InfoContext(ctx, "Reconciliation completed",
				slog.Duration("duration", duration),
				slog.Bool("requeue", result.Requeue),
				slog.Duration("requeueAfter", result.RequeueAfter))
		}
	}()

	// Fetch the object:
	object := &inventoryv1alpha1.Inventory{}
	if err = r.Client.Get(ctx, request.NamespacedName, object); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.InfoContext(ctx, "Inventory not found, assuming deleted")
			err = nil
			return
		}
		ctlrutils.LogError(ctx, r.Logger, "Unable to fetch Inventory", err)
		return
	}

	// Add object-specific context
	ctx = ctlrutils.AddObjectContext(ctx, object)
	r.Logger.InfoContext(ctx, "Fetched Inventory successfully")

	// On the first reconcile, we set the `registerOnRestart` value from an annotation.  This is a one-time operation
	// since we don't want to repeat the registration on every reconcile loop if it was previously successful.
	r.setupOnce.Do(func() { setRegisterOnRestart(ctx, r.Logger, object) })

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

	// Create the Service needed for the Resource server.
	err = t.createService(ctx, ctlrutils.InventoryResourceServerName, constants.DefaultServicePort, ctlrutils.DefaultServiceTargetPort)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy Service for Resource server.",
			slog.Any("error", err),
		)
		return
	}

	err = t.createNetworkPolicy(ctx, ctlrutils.InventoryResourceServerName, constants.DefaultServicePort, ExternalIngress)
	if err != nil {
		t.logger.ErrorContext(ctx, "Failed to create NetworkPolicy for Resource server.", slog.Any("error", err))
		return
	}

	// Create the resource-server deployment.
	errorReason, err := t.deployServer(ctx, ctlrutils.InventoryResourceServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy the Resource server.",
			slog.Any("error", err),
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

	// Create the Service needed for the cluster server.
	err = t.createService(ctx, ctlrutils.InventoryClusterServerName, constants.DefaultServicePort, ctlrutils.DefaultServiceTargetPort)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy Service for cluster server.",
			slog.Any("error", err),
		)
		return
	}

	err = t.createNetworkPolicy(ctx, ctlrutils.InventoryClusterServerName, constants.DefaultServicePort, ExternalIngress)
	if err != nil {
		t.logger.ErrorContext(ctx, "Failed to create NetworkPolicy for cluster server.", slog.Any("error", err))
		return
	}

	// Create the cluster-server deployment.
	errorReason, err := t.deployServer(ctx, ctlrutils.InventoryClusterServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy the cluster server.",
			slog.Any("error", err),
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

	// Create the Service needed for the Artifacts server.
	err = t.createService(ctx, ctlrutils.InventoryArtifactsServerName, constants.DefaultServicePort, ctlrutils.DefaultServiceTargetPort)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy Service for the Artifacts server.",
			slog.Any("error", err),
		)
		return
	}

	err = t.createNetworkPolicy(ctx, ctlrutils.InventoryArtifactsServerName, constants.DefaultServicePort, ExternalIngress)
	if err != nil {
		t.logger.ErrorContext(ctx, "Failed to create NetworkPolicy for Artifacts server.", slog.Any("error", err))
		return
	}

	// Create the artifacts-server deployment.
	errorReason, err := t.deployServer(ctx, ctlrutils.InventoryArtifactsServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy the Artifacts server.",
			slog.Any("error", err),
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

	// Create the Service needed for the alarm server.
	err = t.createService(ctx, ctlrutils.InventoryAlarmServerName, constants.DefaultServicePort, ctlrutils.DefaultServiceTargetPort)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy Service for alarm server.",
			slog.Any("error", err),
		)
		return
	}

	// The alarm server additionally accepts webhook POSTs from alertmanager.
	alertmanagerPort := intstr.FromInt32(constants.DefaultServicePort)
	alertmanagerProtocol := corev1.ProtocolTCP
	alertmanagerIngress := networkingv1.NetworkPolicyIngressRule{
		From: []networkingv1.NetworkPolicyPeer{
			{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kubernetes.io/metadata.name": ctlrutils.OpenClusterManagementObservabilityNamespace,
					},
				},
				PodSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						ctlrutils.AlertmanagerObjectName: "observability",
					},
				},
			},
		},
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &alertmanagerProtocol, Port: &alertmanagerPort},
		},
	}
	err = t.createNetworkPolicy(ctx, ctlrutils.InventoryAlarmServerName, constants.DefaultServicePort, ExternalIngress, alertmanagerIngress)
	if err != nil {
		t.logger.ErrorContext(ctx, "Failed to create NetworkPolicy for alarm server.", slog.Any("error", err))
		return
	}

	// Create the alarm-server deployment.
	errorReason, err := t.deployServer(ctx, ctlrutils.InventoryAlarmServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy the alarm server.",
			slog.Any("error", err),
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

	// Create the Service needed for the provisioning server.
	err = t.createService(ctx, ctlrutils.InventoryProvisioningServerName, constants.DefaultServicePort, ctlrutils.DefaultServiceTargetPort)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy Service for the provisioning server.",
			slog.Any("error", err),
		)
		return
	}

	err = t.createNetworkPolicy(ctx, ctlrutils.InventoryProvisioningServerName, constants.DefaultServicePort, ExternalIngress)
	if err != nil {
		t.logger.ErrorContext(ctx, "Failed to create NetworkPolicy for provisioning server.", slog.Any("error", err))
		return
	}

	// Create the provisioning-server deployment.
	errorReason, err := t.deployServer(ctx, ctlrutils.InventoryProvisioningServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy the provisioning server.",
			slog.Any("error", err),
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
		cm, err := ctlrutils.GetConfigmap(ctx, t.client, *t.object.Spec.CaBundleName, t.object.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get configmap: %s", err.Error())
		}

		caBundle, err = ctlrutils.GetConfigMapField(cm, constants.CABundleFilename)
		if err != nil {
			return nil, fmt.Errorf("failed to get certificate bundle from configmap: %s", err.Error())
		}
	}

	config := ctlrutils.OAuthClientConfig{
		TLSConfig: &ctlrutils.TLSConfig{CaBundle: []byte(caBundle)},
	}

	oAuthConfig := t.object.Spec.SmoConfig.OAuthConfig
	if oAuthConfig != nil {
		clientSecrets, err := ctlrutils.GetSecret(ctx, t.client, oAuthConfig.ClientSecretName, t.object.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get client secret: %w", err)
		}

		clientId, err := ctlrutils.GetSecretField(clientSecrets, "client-id")
		if err != nil {
			return nil, fmt.Errorf("failed to get client-id from secret: %s, %w", oAuthConfig.ClientSecretName, err)
		}

		clientSecret, err := ctlrutils.GetSecretField(clientSecrets, "client-secret")
		if err != nil {
			return nil, fmt.Errorf("failed to get client-secret from secret: %s, %w", oAuthConfig.ClientSecretName, err)
		}

		o := ctlrutils.OAuthConfig{
			ClientID:     clientId,
			ClientSecret: clientSecret,
			TokenURL:     fmt.Sprintf("%s%s", oAuthConfig.URL, oAuthConfig.TokenEndpoint),
			Scopes:       oAuthConfig.Scopes,
		}
		config.OAuthConfig = &o
	}

	if t.object.Spec.SmoConfig.TLS != nil && t.object.Spec.SmoConfig.TLS.SecretName != nil {
		secretName := *t.object.Spec.SmoConfig.TLS.SecretName
		cert, key, err := ctlrutils.GetKeyPairFromSecret(ctx, t.client, secretName, t.object.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get certificate and key from secret: %w", err)
		}

		config.TLSConfig.ClientCert = ctlrutils.NewStaticKeyPairLoader(cert, key)
	}

	httpClient, err := ctlrutils.SetupOAuthClient(ctx, t.logger, &config)
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

	data := ctlrutils.AvailableNotification{
		GlobalCloudId: *t.object.Spec.CloudID,
		OCloudId:      t.object.Status.ClusterID,
		ImsEndpoint:   fmt.Sprintf("https://%s", t.object.Status.IngressHost),
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

	if result.StatusCode > http.StatusNoContent {
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
				Type:    string(ctlrutils.InventoryConditionTypes.SmoRegistrationCompleted),
				Status:  metav1.ConditionFalse,
				Reason:  string(ctlrutils.InventoryConditionReasons.SmoRegistrationSuccessful),
				Message: "SMO configuration not present",
			},
		)
		return nil
	}

	if !ctlrutils.IsSmoRegistrationCompleted(t.object) || registerOnRestart {
		err = t.registerWithSmo(ctx)
		if err != nil {
			t.logger.ErrorContext(
				ctx, "Failed to register with SMO.",
				slog.Any("error", err),
			)

			meta.SetStatusCondition(
				&t.object.Status.Conditions,
				metav1.Condition{
					Type:    string(ctlrutils.InventoryConditionTypes.SmoRegistrationCompleted),
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
				Type:    string(ctlrutils.InventoryConditionTypes.SmoRegistrationCompleted),
				Status:  metav1.ConditionTrue,
				Reason:  string(ctlrutils.InventoryConditionReasons.SmoRegistrationSuccessful),
				Message: fmt.Sprintf("Registered with SMO at: %s", t.object.Spec.SmoConfig.URL),
			},
		)
		t.logger.InfoContext(
			ctx, fmt.Sprintf("successfully registered with the SMO at: %s", t.object.Spec.SmoConfig.URL),
		)

		registerOnRestart = false // this is a one-time registration on restarts for development/debug
	} else {
		t.logger.DebugContext(
			ctx, fmt.Sprintf("already registered with the SMO at: %s", t.object.Spec.SmoConfig.URL),
		)
	}

	return nil
}

// storeIngressDomain stores the ingress domain to be used onto the object status for later retrieval.
func (t *reconcilerTask) storeIngressDomain(ctx context.Context) error {
	// Determine our ingress domain
	var ingressHost string
	if t.object.Spec.IngressConfig == nil || t.object.Spec.IngressConfig.IngressHost == nil {
		var err error
		ingressHost, err = ctlrutils.GetIngressDomain(ctx, t.client)
		if err != nil {
			t.logger.ErrorContext(
				ctx,
				"Failed to get ingress domain.",
				slog.Any("error", err))
			return fmt.Errorf("failed to get ingress domain: %s", err.Error())
		}
		ingressHost = constants.DefaultAppName + "." + ingressHost
	} else {
		ingressHost = *t.object.Spec.IngressConfig.IngressHost
	}

	t.object.Status.IngressHost = ingressHost

	return nil
}

// storeClusterID stores the local cluster id onto the object status for later retrieval.
func (t *reconcilerTask) storeClusterID(ctx context.Context) error {
	clusterID, err := ctlrutils.GetClusterID(ctx, t.client, ctlrutils.ClusterVersionName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to get cluster id.",
			slog.Any("error", err))
		return fmt.Errorf("failed to get cluster id: %s", err.Error())
	}

	t.object.Status.ClusterID = clusterID
	return nil
}

// checkForPodReadyStatus checks for all server pods to be ready.  If any pod is not yet ready, the reconciler timer is
// set to a short value so that we can try again quickly; otherwise either an error is returned without a reconciler
// timer to indicate that an exponential backoff is required
func (t *reconcilerTask) checkForPodReadyStatus(ctx context.Context) (ctrl.Result, error) {
	servers := []string{ctlrutils.InventoryResourceServerName,
		ctlrutils.InventoryClusterServerName,
		ctlrutils.InventoryAlarmServerName,
		ctlrutils.InventoryArtifactsServerName,
		ctlrutils.InventoryProvisioningServerName,
		ctlrutils.InventoryDatabaseServerName}

	var list corev1.PodList
	err := t.client.List(ctx, &list, client.InNamespace(t.object.Namespace))
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list pods: %w", err)
	}

	for _, pod := range list.Items {
		name, ok := pod.Labels["app"]
		if !ok {
			t.logger.WarnContext(ctx, "Pod without an 'app' label in our namespace", slog.String("name", pod.Name))
			continue
		}
		if !slices.Contains(servers, name) {
			continue
		}
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status != corev1.ConditionTrue {
				t.logger.WarnContext(ctx, "Pod is not yet ready", slog.String("name", pod.Name), slog.String("reason", condition.Reason), slog.String("message", condition.Message))
				return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}
		}
	}

	// No reconcile required, and no error
	return ctrl.Result{}, nil
}

func (t *reconcilerTask) run(ctx context.Context) (nextReconcile ctrl.Result, err error) {
	// Set the default reconcile time to 5 minutes.
	nextReconcile = ctrl.Result{RequeueAfter: 5 * time.Minute}

	// Phase 1: Infrastructure setup (ingress domain and cluster ID)
	ctx = ctlrutils.LogPhaseStart(ctx, t.logger, "infrastructure_setup")
	phaseStartTime := time.Now()

	err = t.storeIngressDomain(ctx)
	if err != nil {
		ctlrutils.LogError(ctx, t.logger, "Failed to store ingress domain", err)
		return
	}

	if t.object.Status.ClusterID == "" {
		err = t.storeClusterID(ctx)
		if err != nil {
			ctlrutils.LogError(ctx, t.logger, "Failed to store cluster ID", err)
			return
		}
	}
	ctlrutils.LogPhaseComplete(ctx, t.logger, "infrastructure_setup", time.Since(phaseStartTime))

	// Phase 2: Database setup
	ctx = ctlrutils.LogPhaseStart(ctx, t.logger, "database_setup")
	phaseStartTime = time.Now()

	err = t.createDatabase(ctx)
	if updateError := t.updateInventoryUsedConfigStatus(ctx, ctlrutils.InventoryDatabaseServerName,
		nil, ctlrutils.InventoryConditionReasons.DatabaseDeploymentFailed, err); updateError != nil {
		ctlrutils.LogError(ctx, t.logger, "Failed to report database status", updateError)
		return nextReconcile, updateError
	}

	if err != nil {
		ctlrutils.LogError(ctx, t.logger, "Failed to create database", err)
		return
	}
	ctlrutils.LogPhaseComplete(ctx, t.logger, "database_setup", time.Since(phaseStartTime))

	// Phase 3: Networking and RBAC setup
	ctx = ctlrutils.LogPhaseStart(ctx, t.logger, "networking_rbac_setup")
	phaseStartTime = time.Now()

	err = t.createIngress(ctx)
	if err != nil {
		ctlrutils.LogError(ctx, t.logger, "Failed to deploy Ingress", err)
		return
	}
	ctlrutils.LogPhaseComplete(ctx, t.logger, "networking_rbac_setup", time.Since(phaseStartTime))

	// Phase 4: O2IMS Servers setup
	ctx = ctlrutils.LogPhaseStart(ctx, t.logger, "o2ims_servers_setup")
	phaseStartTime = time.Now()

	// Setup Resource Server
	t.logger.InfoContext(ctx, "Setting up Resource Server")
	nextReconcile, err = t.setupResourceServerConfig(ctx, nextReconcile)
	if err != nil {
		ctlrutils.LogError(ctx, t.logger, "Failed to setup Resource Server", err)
		return
	}

	// Setup Cluster Server
	t.logger.InfoContext(ctx, "Setting up Cluster Server")
	nextReconcile, err = t.setupClusterServerConfig(ctx, nextReconcile)
	if err != nil {
		ctlrutils.LogError(ctx, t.logger, "Failed to setup Cluster Server", err)
		return
	}

	// Setup Alarm Server
	t.logger.InfoContext(ctx, "Setting up Alarm Server")
	nextReconcile, err = t.setupAlarmServerConfig(ctx, nextReconcile)
	if err != nil {
		ctlrutils.LogError(ctx, t.logger, "Failed to setup Alarm Server", err)
		return
	}

	// Setup Artifacts Server
	t.logger.InfoContext(ctx, "Setting up Artifacts Server")
	nextReconcile, err = t.setupArtifactsServerConfig(ctx, nextReconcile)
	if err != nil {
		ctlrutils.LogError(ctx, t.logger, "Failed to setup Artifacts Server", err)
		return
	}

	// Setup Provisioning Server
	t.logger.InfoContext(ctx, "Setting up Provisioning Server")
	nextReconcile, err = t.setupProvisioningServerConfig(ctx, nextReconcile)
	if err != nil {
		ctlrutils.LogError(ctx, t.logger, "Failed to setup Provisioning Server", err)
		return
	}
	ctlrutils.LogPhaseComplete(ctx, t.logger, "o2ims_servers_setup", time.Since(phaseStartTime))

	// Phase 5: Hardware manager setup
	ctx = ctlrutils.LogPhaseStart(ctx, t.logger, "hardware_manager_setup")
	phaseStartTime = time.Now()

	// Setup hardware manager
	t.logger.InfoContext(ctx, "Setting up hardware manager")
	nextReconcile, err = t.setupHardwareManager(ctx, nextReconcile)
	if err != nil {
		ctlrutils.LogError(ctx, t.logger, "Failed to setup hardware manager", err)
		return
	}
	ctlrutils.LogPhaseComplete(ctx, t.logger, "hardware_manager_setup", time.Since(phaseStartTime))

	// Phase 6: Readiness validation
	ctx = ctlrutils.LogPhaseStart(ctx, t.logger, "readiness_validation")
	phaseStartTime = time.Now()

	t.logger.InfoContext(ctx, "Checking pod readiness status")
	nextReconcile, err = t.checkForPodReadyStatus(ctx)
	if err != nil {
		ctlrutils.LogError(ctx, t.logger, "Failed readiness validation", err)
		return
	}
	ctlrutils.LogPhaseComplete(ctx, t.logger, "readiness_validation", time.Since(phaseStartTime))

	t.logger.InfoContext(ctx, "All inventory infrastructure components are ready")

	if !nextReconcile.IsZero() {
		// The pods are not ready and we are going to retry
		return
	}

	// Register with SMO (if necessary)
	err = t.setupSmo(ctx)
	if err != nil {
		return
	}

	// Reset the next reconcile back to our default of 5 minutes
	nextReconcile = ctrl.Result{RequeueAfter: 5 * time.Minute}

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

func (t *reconcilerTask) deployServer(ctx context.Context, serverName string) (ctlrutils.InventoryConditionReason, error) {
	t.logger.DebugContext(ctx, "[deploy server]", slog.String("name", serverName))

	// Server variables.
	deploymentVolumes := ctlrutils.GetDeploymentVolumes(serverName, t.object)
	deploymentVolumeMounts := ctlrutils.GetDeploymentVolumeMounts(serverName, t.object)

	// Build the deployment's metadata.
	deploymentMeta := metav1.ObjectMeta{
		Name:      serverName,
		Namespace: t.object.Namespace,
		Labels: map[string]string{
			"oran/o2ims": t.object.Name,
			"app":        serverName,
		},
	}

	deploymentContainerArgs, err := ctlrutils.GetServerArgs(t.object, serverName)
	if err != nil {
		err2 := t.updateInventoryUsedConfigStatus(
			ctx, serverName, deploymentContainerArgs,
			ctlrutils.InventoryConditionReasons.ServerArgumentsError, err)
		if err2 != nil {
			return "", fmt.Errorf("failed to update ORANO2ISMUsedConfigStatus: %w", err2)
		}
		return ctlrutils.InventoryConditionReasons.ServerArgumentsError, fmt.Errorf("failed to get server arguments: %w", err)
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

	var envVars []corev1.EnvVar
	if ctlrutils.HasDatabase(serverName) {
		envVarName, err := ctlrutils.GetServerDatabasePasswordName(serverName)
		if err != nil {
			return "", fmt.Errorf("failed to get server database password: %w", err)
		}
		envVar := corev1.EnvVar{
			Name: envVarName,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: fmt.Sprintf("%s-passwords", ctlrutils.InventoryDatabaseServerName),
					},
					Key: envVarName,
				},
			},
		}
		envVars = append(envVars, envVar)
	}

	if ctlrutils.NeedsOAuthAccess(serverName) && ctlrutils.IsOAuthEnabled(t.object) {
		envVars = append(envVars, []corev1.EnvVar{
			{
				Name: ctlrutils.OAuthClientIDEnvName,
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
				Name: ctlrutils.OAuthClientSecretEnvName,
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

	// Common env for server deployments
	envVars = append(envVars, []corev1.EnvVar{
		{
			Name: "POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
		{
			Name: constants.DefaultNamespaceEnvName,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
		{
			Name:  constants.InternalServicePortName,
			Value: fmt.Sprintf("%d", constants.DefaultServicePort),
		},
	}...)

	// Server specific env var
	if serverName == ctlrutils.InventoryAlarmServerName {
		postgresImage := os.Getenv(constants.PostgresImageName)
		if postgresImage == "" {
			return "", fmt.Errorf("missing %s environment variable value", constants.PostgresImageName)
		}
		envVars = append(envVars, corev1.EnvVar{
			Name:  constants.PostgresImageName,
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
					"kubectl.kubernetes.io/default-container": constants.ServerContainerName,
				},
				Labels: map[string]string{
					"app": serverName,
				},
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: fmt.Sprintf("%s-%s", constants.DefaultNamespace, serverName),
				Volumes:            deploymentVolumes,
				Containers: []corev1.Container{
					{
						Name:            constants.ServerContainerName,
						Image:           image,
						ImagePullPolicy: corev1.PullPolicy(os.Getenv(constants.ImagePullPolicyEnvName)),
						VolumeMounts:    deploymentVolumeMounts,
						Command:         []string{constants.ManagerExec},
						Args:            deploymentContainerArgs,
						Env:             envVars,
						Ports: []corev1.ContainerPort{
							{
								Name:          ctlrutils.DefaultServiceTargetPort,
								Protocol:      corev1.ProtocolTCP,
								ContainerPort: constants.DefaultContainerPort,
							},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("200m"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
						},
					},
				},
			},
		},
	}

	if ctlrutils.HasDatabase(serverName) {
		deploymentSpec.Template.Spec.InitContainers = []corev1.Container{
			{
				Name:    constants.MigrationContainerName,
				Image:   image,
				Command: []string{constants.ManagerExec},
				Args:    []string{serverName, "migrate"},
				Env:     envVars,
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				},
			},
		}
	}

	// Build the deployment.
	newDeployment := &appsv1.Deployment{
		ObjectMeta: deploymentMeta,
		Spec:       deploymentSpec,
	}

	t.logger.DebugContext(ctx, "[deployManagerServer] Create/Update/Patch Server", slog.String("name", serverName))
	if err := ctlrutils.CreateK8sCR(ctx, t.client, newDeployment, t.object, ctlrutils.UPDATE); err != nil {
		return "", fmt.Errorf("failed to deploy ManagerServer: %w", err)
	}

	return "", nil
}

// NetworkPolicyScope controls whether a NetworkPolicy permits external ingress traffic.
type NetworkPolicyScope int

const (
	// InternalOnly restricts ingress to pods in the same namespace.
	InternalOnly NetworkPolicyScope = iota
	// ExternalIngress additionally allows traffic from the OpenShift ingress controller.
	ExternalIngress
)

func (t *reconcilerTask) createNetworkPolicy(ctx context.Context, serverName string, port int32, scope NetworkPolicyScope, extraIngressRules ...networkingv1.NetworkPolicyIngressRule) error {
	t.logger.DebugContext(ctx, "[createNetworkPolicy]", slog.String("server", serverName))

	protocol := corev1.ProtocolTCP
	portVal := intstr.FromInt32(port)

	ingressRules := []networkingv1.NetworkPolicyIngressRule{
		{
			// Allow from pods in the same namespace (inter-service communication)
			From: []networkingv1.NetworkPolicyPeer{
				{
					PodSelector: &metav1.LabelSelector{},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protocol, Port: &portVal},
			},
		},
	}

	if scope == ExternalIngress {
		ingressRules = append([]networkingv1.NetworkPolicyIngressRule{
			{
				// Allow from the OpenShift ingress controller (external SMO traffic via Routes)
				From: []networkingv1.NetworkPolicyPeer{
					{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"network.openshift.io/policy-group": "ingress",
							},
						},
					},
				},
				Ports: []networkingv1.NetworkPolicyPort{
					{Protocol: &protocol, Port: &portVal},
				},
			},
		}, ingressRules...)
	}

	ingressRules = append(ingressRules, extraIngressRules...)

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serverName,
			Namespace: t.object.Namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": serverName,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
			Ingress: ingressRules,
		},
	}

	if err := ctlrutils.CreateK8sCR(ctx, t.client, np, t.object, ctlrutils.UPDATE); err != nil {
		return fmt.Errorf("failed to create NetworkPolicy for %s: %w", serverName, err)
	}

	return nil
}

func (t *reconcilerTask) createServiceAccount(ctx context.Context, resourceName string) error {
	t.logger.DebugContext(ctx, "[createServiceAccount]")
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

	t.logger.DebugContext(ctx, "[createServiceAccount] Create/Update/Patch ServiceAccount: ", slog.String("name", resourceName))
	if err := ctlrutils.CreateK8sCR(ctx, t.client, newServiceAccount, t.object, ctlrutils.UPDATE); err != nil {
		return fmt.Errorf("failed to create ServiceAccount for deployment: %w", err)
	}

	return nil
}

func (t *reconcilerTask) createService(ctx context.Context, resourceName string, port int32, targetPort string) error {
	t.logger.DebugContext(ctx, "[createService]")
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
				Name:       ctlrutils.IngressPortName,
				Port:       port,
				TargetPort: intstr.FromString(targetPort),
			},
		},
	}

	newService := &corev1.Service{
		ObjectMeta: serviceMeta,
		Spec:       serviceSpec,
	}

	t.logger.DebugContext(ctx, "[createService] Create/Update/Patch Service: ", slog.String("name", resourceName))
	if err := ctlrutils.CreateK8sCR(ctx, t.client, newService, t.object, ctlrutils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Service for deployment: %w", err)
	}

	return nil
}

func (t *reconcilerTask) createIngress(ctx context.Context) error {
	t.logger.DebugContext(ctx, "[createIngress]")
	// Build the Ingress object.
	className := ctlrutils.IngressClassName
	ingressMeta := metav1.ObjectMeta{
		Name:      ctlrutils.IngressName,
		Namespace: t.object.Namespace,
		Annotations: map[string]string{
			"route.openshift.io/termination": "reencrypt",
		},
	}

	ingressSpec := networkingv1.IngressSpec{
		IngressClassName: &className,
		Rules: []networkingv1.IngressRule{
			{
				Host: t.object.Status.IngressHost,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{
							{
								Path: constants.O2IMSInventoryAPIPath,
								PathType: func() *networkingv1.PathType {
									pathType := networkingv1.PathTypePrefix
									return &pathType
								}(),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: ctlrutils.InventoryResourceServerName,
										Port: networkingv1.ServiceBackendPort{
											Name: ctlrutils.IngressPortName,
										},
									},
								},
							},
							{
								Path: constants.O2IMSClusterAPIPath,
								PathType: func() *networkingv1.PathType {
									pathType := networkingv1.PathTypePrefix
									return &pathType
								}(),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: ctlrutils.InventoryClusterServerName,
										Port: networkingv1.ServiceBackendPort{
											Name: ctlrutils.IngressPortName,
										},
									},
								},
							},
							{
								Path: constants.O2IMSArtifactsAPIPath,
								PathType: func() *networkingv1.PathType {
									pathType := networkingv1.PathTypePrefix
									return &pathType
								}(),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: ctlrutils.InventoryArtifactsServerName,
										Port: networkingv1.ServiceBackendPort{
											Name: ctlrutils.IngressPortName,
										},
									},
								},
							},
							{
								Path: constants.O2IMSProvisioningAPIPath,
								PathType: func() *networkingv1.PathType {
									pathType := networkingv1.PathTypePrefix
									return &pathType
								}(),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: ctlrutils.InventoryProvisioningServerName,
										Port: networkingv1.ServiceBackendPort{
											Name: ctlrutils.IngressPortName,
										},
									},
								},
							},
							{
								Path: constants.O2IMSMonitoringAPIPath,
								PathType: func() *networkingv1.PathType {
									pathType := networkingv1.PathTypePrefix
									return &pathType
								}(),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: ctlrutils.InventoryAlarmServerName,
										Port: networkingv1.ServiceBackendPort{
											Name: ctlrutils.IngressPortName,
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

	if t.object.Spec.IngressConfig != nil && t.object.Spec.IngressConfig.TLS != nil && t.object.Spec.IngressConfig.TLS.SecretName != nil {
		ingressSpec.TLS = []networkingv1.IngressTLS{
			{
				Hosts:      []string{t.object.Status.IngressHost},
				SecretName: *t.object.Spec.IngressConfig.TLS.SecretName,
			},
		}
	}

	newIngress := &networkingv1.Ingress{
		ObjectMeta: ingressMeta,
		Spec:       ingressSpec,
	}

	t.logger.DebugContext(ctx, "[createIngress] Create/Update/Patch Ingress: ", slog.String("name", ctlrutils.IngressPortName))
	if err := ctlrutils.CreateK8sCR(ctx, t.client, newIngress, t.object, ctlrutils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Ingress for deployment: %w", err)
	}

	return nil
}

func (t *reconcilerTask) updateInventoryStatusConditions(ctx context.Context, deploymentName string) {
	deployment := &appsv1.Deployment{}
	err := t.client.Get(ctx, types.NamespacedName{Name: deploymentName, Namespace: t.object.Namespace}, deployment)

	if err != nil {
		reason := string(ctlrutils.InventoryConditionReasons.ErrorGettingDeploymentInformation)
		if errors.IsNotFound(err) {
			reason = string(ctlrutils.InventoryConditionReasons.DeploymentNotFound)
		}
		meta.SetStatusCondition(
			&t.object.Status.Conditions,
			metav1.Condition{
				Type:    string(ctlrutils.InventoryConditionTypes.Error),
				Status:  metav1.ConditionTrue,
				Reason:  reason,
				Message: fmt.Sprintf("Error when querying for the %s server", deploymentName),
			},
		)

		meta.SetStatusCondition(
			&t.object.Status.Conditions,
			metav1.Condition{
				Type:    string(ctlrutils.InventoryConditionTypes.Ready),
				Status:  metav1.ConditionFalse,
				Reason:  string(ctlrutils.InventoryConditionReasons.DeploymentsReady),
				Message: "The O-Cloud Manager Deployments are not yet ready",
			},
		)
	} else {
		meta.RemoveStatusCondition(
			&t.object.Status.Conditions,
			string(ctlrutils.InventoryConditionTypes.Error))
		meta.RemoveStatusCondition(
			&t.object.Status.Conditions,
			string(ctlrutils.InventoryConditionTypes.Ready))
		for _, condition := range deployment.Status.Conditions {
			// Obtain the status directly from the Deployment resources.
			if condition.Type == "Available" {
				meta.SetStatusCondition(
					&t.object.Status.Conditions,
					metav1.Condition{
						Type:    string(ctlrutils.MapAvailableDeploymentNameConditionType[deploymentName]),
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
	errorReason ctlrutils.InventoryConditionReason, err error) error {
	t.logger.DebugContext(ctx, "[updateInventoryUsedConfigStatus]")

	if serverName == ctlrutils.InventoryResourceServerName {
		t.object.Status.UsedServerConfig.ResourceServerUsedConfig = deploymentArgs
	}

	if serverName == ctlrutils.InventoryClusterServerName {
		t.object.Status.UsedServerConfig.ClusterServerUsedConfig = deploymentArgs
	}

	if serverName == ctlrutils.InventoryAlarmServerName {
		t.object.Status.UsedServerConfig.AlarmsServerUsedConfig = deploymentArgs
	}

	if serverName == ctlrutils.InventoryArtifactsServerName {
		t.object.Status.UsedServerConfig.ArtifactsServerUsedConfig = deploymentArgs
	}

	if serverName == ctlrutils.InventoryProvisioningServerName {
		t.object.Status.UsedServerConfig.ProvisioningServerUsedConfig = deploymentArgs
	}

	// If there is an error passed, include it in the condition.
	if err != nil {
		meta.SetStatusCondition(
			&t.object.Status.Conditions,
			metav1.Condition{
				Type:    string(ctlrutils.MapErrorDeploymentNameConditionType[serverName]),
				Status:  "True",
				Reason:  string(errorReason),
				Message: err.Error(),
			},
		)

		meta.RemoveStatusCondition(
			&t.object.Status.Conditions,
			string(ctlrutils.MapAvailableDeploymentNameConditionType[serverName]))
	} else {
		meta.RemoveStatusCondition(
			&t.object.Status.Conditions,
			string(ctlrutils.MapErrorDeploymentNameConditionType[serverName]))
	}

	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to update inventory used config CR status: %w", err)
	}

	return nil
}

func (t *reconcilerTask) updateInventoryDeploymentStatus(ctx context.Context) error {

	t.logger.DebugContext(ctx, "[updateInventoryDeploymentStatus]")
	t.updateInventoryStatusConditions(ctx, ctlrutils.InventoryAlarmServerName)
	t.updateInventoryStatusConditions(ctx, ctlrutils.InventoryResourceServerName)
	t.updateInventoryStatusConditions(ctx, ctlrutils.InventoryClusterServerName)
	t.updateInventoryStatusConditions(ctx, ctlrutils.InventoryArtifactsServerName)
	t.updateInventoryStatusConditions(ctx, ctlrutils.InventoryDatabaseServerName)
	t.updateInventoryStatusConditions(ctx, ctlrutils.InventoryProvisioningServerName)

	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
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
