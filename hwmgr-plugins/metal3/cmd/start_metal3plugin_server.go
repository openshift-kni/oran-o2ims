/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	bmhv1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	hwpluginserver "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/server/provisioning"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	metal3ctrl "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/metal3/controller"
	metal3server "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/metal3/server"
	"github.com/openshift-kni/oran-o2ims/internal"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	"github.com/openshift-kni/oran-o2ims/internal/exit"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(hwmgmtv1alpha1.AddToScheme(scheme))
	utilruntime.Must(pluginsv1alpha1.AddToScheme(scheme))
	utilruntime.Must(bmhv1alpha1.AddToScheme(scheme))
}

// Create creates and returns the `start` command.
func Start() *cobra.Command {
	result := &cobra.Command{
		Use:   constants.Metal3HardwarePluginManagerCmd,
		Short: "Metal3 HardwarePlugin Manager",
		Args:  cobra.NoArgs,
	}
	result.AddCommand(ControllerManager())
	return result
}

// ControllerManagerCommand contains the data and logic needed to run the `metal3-hardwareplugin-manager start` command.
type ControllerManagerCommand struct {
	metricsAddr          string
	metricsCertDir       string
	enableHTTP2          bool
	enableLeaderElection bool
	probeAddr            string
	svcutils.CommonServerConfig
}

// NewControllerManager creates a new runner that knows how to execute the `metal3-hardwareplugin-manager start` command.
func NewControllerManager() *ControllerManagerCommand {
	return &ControllerManagerCommand{}
}

// ControllerManager represents the start command for the Metal3 HardwarePlugin Manager
func ControllerManager() *cobra.Command {
	c := NewControllerManager()
	result := &cobra.Command{
		Use:   "start",
		Short: "Start the Metal3 HardwarePlugin manager",
		Args:  cobra.NoArgs,
		RunE:  c.run,
	}

	flags := result.Flags()

	flags.StringVar(
		&c.metricsAddr,
		"metrics-bind-address",
		constants.MetricsPort,
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
		constants.HealthProbePort,
		"The address the probe endpoint binds to.",
	)
	flags.BoolVar(
		&c.enableHTTP2,
		"enable-http2",
		false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers",
	)
	flags.BoolVar(
		&c.enableLeaderElection,
		"leader-elect",
		false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.",
	)
	flags.StringVar(
		&c.Listener.Address,
		svcutils.ListenerFlagName,
		fmt.Sprintf("%s:%d", constants.Localhost, constants.DefaultContainerPort),
		"API listener address",
	)
	flags.StringVar(
		&c.TLS.CertFile,
		svcutils.ServerCertFileFlagName,
		fmt.Sprintf("%s/tls.crt", constants.TLSServerMountPath),
		"Server certificate file",
	)
	flags.StringVar(
		&c.TLS.KeyFile,
		svcutils.ServerKeyFileFlagName,
		fmt.Sprintf("%s/tls.key", constants.TLSServerMountPath),
		"Server private key file",
	)
	return result
}

// run executes the `metal3-hardwareplugin-manager start` command.
func (c *ControllerManagerCommand) run(cmd *cobra.Command, argv []string) error {

	ctx := cmd.Context()

	// Set the logger from context
	logger := internal.LoggerFromContext(ctx)

	// Configure klog to use our structured logger for vendor modules:
	klog.SetSlogLogger(logger)

	logAdapter := logr.FromSlogHandler(logger.Handler())
	ctrl.SetLogger(logAdapter)
	klog.SetLogger(logAdapter)

	// Set the TLS options
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

	if err := hwmgrutils.InitNodeAllocationRequestUtils(scheme); err != nil {
		logger.ErrorContext(ctx, "failed InitNodeAllocationRequestUtils", slog.String("error", err.Error()))
		return exit.Error(1)
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
		LeaderElectionID:       "a3c1dd20.openshift.io",
	})
	if err != nil {
		logger.ErrorContext(ctx, "Unable to start manager", slog.String("error", err.Error()))
		return exit.Error(1)
	}

	controllers, err := metal3ctrl.SetupMetal3Controllers(mgr, hwpluginserver.GetMetal3HWPluginNamespace(), logger)
	if err != nil {
		logger.ErrorContext(ctx, "Unable to create metal3 plugin controller",
			slog.String("controller", "Metal3HWPlugin"), slog.String("error", err.Error()))
		return exit.Error(1)
	}

	// Initialize callback context for NodeAllocationRequest controller
	controllers.NodeAllocationReconciler.InitializeCallbackContext(ctx)

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.ErrorContext(ctx, "Unable to set up health check", slog.String("error", err.Error()))
		return exit.Error(1)
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.ErrorContext(ctx, "Unable to set up ready check", slog.String("error", err.Error()))
		return exit.Error(1)
	}

	serverErrors := make(chan error, 1)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		logger.Info("Starting Metal3 HardwarePlugin API server")
		err = metal3server.Serve(ctx, logger, c.CommonServerConfig, mgr.GetClient())
	}()

	go func() {
		logger.Info("Starting manager")
		if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
			logger.ErrorContext(ctx, "Problem running manager", slog.String("error", err.Error()))
			serverErrors <- err
			return
		}
		// The manager has terminated normally. Cancel the context to allow the API server to shutdown
		cancel()
	}()

	select {
	case err = <-serverErrors:
		// Server failed to start
		logger.ErrorContext(ctx, "Problem running internal server", slog.String("error", err.Error()))
		// Shutdown callbacks before exit
		controllers.NodeAllocationReconciler.ShutdownCallbacks(30 * time.Second)
		return exit.Error(1)
	case <-ctx.Done():
		// Graceful shutdown - wait for callbacks to complete
		controllers.NodeAllocationReconciler.ShutdownCallbacks(30 * time.Second)
		return exit.Error(0)
	}
}
