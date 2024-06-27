/*
Copyright 2024 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package operator

import (
	"log/slog"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/go-logr/logr"
	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal"
	"github.com/openshift-kni/oran-o2ims/internal/controllers"
	"github.com/openshift-kni/oran-o2ims/internal/exit"
	"github.com/spf13/cobra"
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
		&c.probeAddr,
		"health-probe-bind-address",
		":8081",
		"The address the probe endpoint binds to.",
	)
	flags.BoolVar(
		&c.enableLeaderElection,
		"leader-elect",
		false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.",
	)
	flags.StringVar(
		&c.image,
		imageFlagName,
		"quay.io/openshift-kni/oran-o2ims:latest",
		"Reference of the container image containing the servers.",
	)
	return result
}

// ControllerManagerCommand contains the data and logic needed to run the `start controller-manager`
// command.
type ControllerManagerCommand struct {
	metricsAddr          string
	enableLeaderElection bool
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
	utilruntime.Must(oranv1alpha1.AddToScheme(scheme))
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

	// Restrict to the following namespaces - subject to change.
	namespaces := [...]string{"default", "oran", "o2ims", "oran-o2ims"} // List of Namespaces
	defaultNamespaces := make(map[string]cache.Config)

	for _, ns := range namespaces {
		defaultNamespaces[ns] = cache.Config{}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: c.metricsAddr},
		HealthProbeBindAddress: c.probeAddr,
		LeaderElection:         c.enableLeaderElection,
		LeaderElectionID:       "a73bc4d2.openshift.io",
		Cache: cache.Options{
			DefaultNamespaces: defaultNamespaces,
		},

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

	// Start the Cluster Request controller.
	if err = (&controllers.ClusterRequestReconciler{
		Client: mgr.GetClient(),
		Logger: slog.With("controller", "ClusterRequest"),
	}).SetupWithManager(mgr); err != nil {
		logger.ErrorContext(
			ctx,
			"Unable to create controller",
			slog.String("controller", "ClusterRequest"),
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
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
