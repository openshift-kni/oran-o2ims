/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package operator

import (
	"crypto/tls"
	"flag"
	"log/slog"

	openshiftv1 "github.com/openshift/api/config/v1"
	openshiftoperatorv1 "github.com/openshift/api/operator/v1"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/go-logr/logr"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	assistedservicev1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/spf13/cobra"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"

	"github.com/openshift-kni/oran-o2ims/internal"
	"github.com/openshift-kni/oran-o2ims/internal/controllers"
	"github.com/openshift-kni/oran-o2ims/internal/exit"
)

// ControllerManager creates and returns the `start controller-manager` command.
func ControllerManager() *cobra.Command {
	c := NewControllerManager()
	result := &cobra.Command{
		Use:   "controller-manager",
		Short: "Starts the controller manager",
		Args:  cobra.NoArgs,
		RunE:  c.run,
	}
	flags := result.Flags()
	flags.StringVar(
		&c.metricsAddr,
		"metrics-bind-address",
		":8080",
		"The address the metric endpoint binds to.",
	)
	flags.StringVar(
		&c.metricsCertDir,
		"metrics-tls-cert-dir",
		"",
		"The directory containing the tls.crt and tls.key.",
	)
	flags.StringVar(
		&c.probeAddr,
		"health-probe-bind-address",
		":8081",
		"The address the probe endpoint binds to.",
	)
	flag.BoolVar(
		&c.enableHTTP2,
		"enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers",
	)
	flags.BoolVar(
		&c.enableLeaderElection,
		"leader-elect",
		false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.",
	)
	flags.BoolVar(&c.enableWebhooks,
		"enable-webhooks",
		true,
		"Enable the o2ims validating webhooks")
	flags.StringVar(
		&c.image,
		imageFlagName,
		// Intentionally setting the default value to "" if the environment variable is not set to ensure we never
		// run an image that we didn't intend on running.
		utils.GetEnvOrDefault(utils.ServerImageName, ""),
		"Reference of the container image containing the servers.",
	)
	return result
}

// ControllerManagerCommand contains the data and logic needed to run the `start controller-manager`
// command.
type ControllerManagerCommand struct {
	metricsAddr          string
	metricsCertDir       string
	enableHTTP2          bool
	enableLeaderElection bool
	enableWebhooks       bool
	probeAddr            string
	image                string
}

// NewControllerManager creates a new runner that knows how to execute the `start
// controller-manager` command.
func NewControllerManager() *ControllerManagerCommand {
	return &ControllerManagerCommand{}
}

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(provisioningv1alpha1.AddToScheme(scheme))
	utilruntime.Must(inventoryv1alpha1.AddToScheme(scheme))
	utilruntime.Must(siteconfig.AddToScheme(scheme))
	utilruntime.Must(hwv1alpha1.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(policiesv1.AddToScheme(scheme))
	utilruntime.Must(openshiftv1.AddToScheme(scheme))
	utilruntime.Must(openshiftoperatorv1.AddToScheme(scheme))
	utilruntime.Must(ibguv1alpha1.AddToScheme(scheme))
	utilruntime.Must(pluginv1alpha1.AddToScheme(scheme))
	utilruntime.Must(assistedservicev1beta1.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(metal3v1alpha1.AddToScheme(scheme))
}

// run executes the `start controller-manager` command.
func (c *ControllerManagerCommand) run(cmd *cobra.Command, argv []string) error {
	// Get the context:
	ctx := cmd.Context()

	// Get the dependencies from the context:
	logger := internal.LoggerFromContext(ctx)

	// Configure the controller runtime library to use our logger:
	adapter := logr.FromSlogHandler(logger.Handler())
	ctrl.SetLogger(adapter)
	klog.SetLogger(adapter)

	// Check the flags:
	if c.image == "" {
		logger.ErrorContext(
			ctx,
			"Image flag is mandatory",
			slog.String("flag", imageFlagName),
		)
		return exit.Error(1)
	}

	// Set the TLS options.
	// If the enable-http2 flag is false (the default), http/2 will be disabled due to its vulnerabilities.
	// More specifically, disabling http/2 will prevent from being vulnerable to the HTTP/2 Stream
	// Cancelation and Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	tlsOpts := []func(*tls.Config){}
	if !c.enableHTTP2 {
		tlsOpts = append(tlsOpts, func(c *tls.Config) {
			logger.InfoContext(ctx, "disabling http/2")
			c.NextProtos = []string{"http/1.1"}
		})
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			SecureServing:  c.metricsCertDir != "",
			CertDir:        c.metricsCertDir,
			BindAddress:    c.metricsAddr,
			TLSOpts:        tlsOpts,
			FilterProvider: filters.WithAuthenticationAndAuthorization,
		},
		HealthProbeBindAddress: c.probeAddr,
		LeaderElection:         c.enableLeaderElection,
		LeaderElectionID:       "a73bc4d2.openshift.io",
		WebhookServer: webhook.NewServer(
			webhook.Options{
				TLSOpts: tlsOpts,
			},
		),

		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Unable to start manager",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}

	// Create the default inventory CR
	err = utils.CreateDefaultInventoryCR(ctx, mgr.GetClient())
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to create default inventory CR",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}

	// Start the O2IMS controller.
	if err = (&controllers.Reconciler{
		Client: mgr.GetClient(),
		Logger: slog.With("controller", "ORAN-O2IMS"),
		Image:  c.image,
	}).SetupWithManager(mgr); err != nil {
		logger.ErrorContext(
			ctx,
			"Unable to create controller",
			slog.String("controller", "ORANO2IMS"),
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}

	// Register the field index for BareMetalHost
	if err := controllers.SetupBareMetalHostIndexer(ctx, mgr); err != nil {
		logger.ErrorContext(ctx, "Unable to set up BareMetalHost indexer", slog.Any("error", err))
		return exit.Error(1)
	}

	// Start the Cluster Template controller.
	if err = (&controllers.ClusterTemplateReconciler{
		Client: mgr.GetClient(),
		Logger: slog.With("controller", "ClusterTemplate"),
	}).SetupWithManager(mgr); err != nil {
		logger.ErrorContext(
			ctx,
			"Unable to create controller",
			slog.String("controller", "ClusterTemplate"),
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}

	// Start the Provisioning Request controller.
	if err = (&controllers.ProvisioningRequestReconciler{
		Client: mgr.GetClient(),
		Logger: slog.With("controller", "ProvisioningRequest"),
	}).SetupWithManager(mgr); err != nil {
		logger.ErrorContext(
			ctx,
			"Unable to create controller",
			slog.String("controller", "ProvisioningRequest"),
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}

	if c.enableWebhooks {
		if err = (&provisioningv1alpha1.ProvisioningRequest{}).SetupWebhookWithManager(mgr); err != nil {
			logger.ErrorContext(
				ctx,
				"Unable to create webhook",
				slog.String("webhook", "ProvisioningRequest"),
				slog.String("error", err.Error()),
			)
			return exit.Error(1)
		}

	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.ErrorContext(
			ctx,
			"Unable to set up health check",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.ErrorContext(
			ctx,
			"Unable to set up ready check",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}

	logger.InfoContext(
		ctx,
		"Starting manager",
		slog.String("image", c.image),
	)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.ErrorContext(
			ctx,
			"Problem running manager",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}

	return nil
}

// Names of command line flags:
const (
	imageFlagName = "image"
)
