/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package cmd

import (
	"crypto/tls"
	"log/slog"

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
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/exit"
	hwmgrctrl "github.com/openshift-kni/oran-o2ims/internal/hardwaremanager/controller"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/internal/hardwaremanager/utils"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(hwmgmtv1alpha1.AddToScheme(scheme))
	utilruntime.Must(bmhv1alpha1.AddToScheme(scheme))
	utilruntime.Must(provisioningv1alpha1.AddToScheme(scheme))
	utilruntime.Must(inventoryv1alpha1.AddToScheme(scheme))
}

// Start creates and returns the `hardwaremanager` command.
func Start() *cobra.Command {
	result := &cobra.Command{
		Use:   constants.HardwareManagerCmd,
		Short: "Hardware Manager",
		Args:  cobra.NoArgs,
	}
	result.AddCommand(ControllerManager())
	return result
}

// ControllerManagerCommand contains the data and logic needed to run the
// `hardwaremanager start` command.
type ControllerManagerCommand struct {
	metricsAddr          string
	metricsCertDir       string
	enableHTTP2          bool
	enableLeaderElection bool
	probeAddr            string
}

// NewControllerManager creates a new runner that knows how to execute the
// `hardwaremanager start` command.
func NewControllerManager() *ControllerManagerCommand {
	return &ControllerManagerCommand{}
}

// ControllerManager represents the start command for the hardware manager
func ControllerManager() *cobra.Command {
	c := NewControllerManager()
	result := &cobra.Command{
		Use:   "start",
		Short: "Start the hardware manager",
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
	return result
}

// run executes the `hardwaremanager start` command.
func (c *ControllerManagerCommand) run(cmd *cobra.Command, argv []string) error {
	_ = argv
	ctx := cmd.Context()

	logger := internal.LoggerFromContext(ctx)

	klog.SetSlogLogger(logger)
	logAdapter := logr.FromSlogHandler(logger.Handler())
	ctrl.SetLogger(logAdapter)
	klog.SetLogger(logAdapter)

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

	namespace := ctlrutils.GetEnvOrDefault(constants.DefaultNamespaceEnvName, constants.DefaultNamespace)
	err = hwmgrctrl.SetupControllers(mgr, namespace, logger)
	if err != nil {
		logger.ErrorContext(ctx, "Unable to create hardware manager controller",
			slog.String("error", err.Error()))
		return exit.Error(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.ErrorContext(ctx, "Unable to set up health check", slog.String("error", err.Error()))
		return exit.Error(1)
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.ErrorContext(ctx, "Unable to set up ready check", slog.String("error", err.Error()))
		return exit.Error(1)
	}

	logger.Info("Starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.ErrorContext(ctx, "Problem running manager", slog.String("error", err.Error()))
		return exit.Error(1)
	}

	return nil
}
