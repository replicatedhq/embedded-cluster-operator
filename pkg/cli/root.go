package cli

import (
	"os"

	"github.com/spf13/cobra"
	"k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/replicatedhq/embedded-cluster-operator/controllers"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/k8sutil"
)

func RootCmd() *cobra.Command {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string

	cmd := &cobra.Command{
		Use:          "manager",
		Short:        "Embedded Cluster Operator",
		SilenceUsage: true,
		Run: func(cmd *cobra.Command, args []string) {
			log := ctrl.LoggerFrom(cmd.Context())

			mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
				Scheme: k8sutil.Scheme(),
				Metrics: metricsserver.Options{
					BindAddress: metricsAddr,
				},
				WebhookServer:                 webhook.NewServer(webhook.Options{Port: 9443}),
				HealthProbeBindAddress:        probeAddr,
				LeaderElection:                enableLeaderElection,
				LeaderElectionID:              "3f2343ef.replicated.com",
				LeaderElectionReleaseOnCancel: true,
			})
			if err != nil {
				log.Error(err, "unable to start manager")
				os.Exit(1)
			}

			if err = (&controllers.InstallationReconciler{
				Client:    mgr.GetClient(),
				Scheme:    mgr.GetScheme(),
				Discovery: discovery.NewDiscoveryClientForConfigOrDie(ctrl.GetConfigOrDie()),
			}).SetupWithManager(mgr); err != nil {
				log.Error(err, "unable to create controller", "controller", "Installation")
				os.Exit(1)
			}

			if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
				log.Error(err, "unable to set up health check")
				os.Exit(1)
			}
			if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
				log.Error(err, "unable to set up ready check")
				os.Exit(1)
			}

			log.Info("Starting manager")
			if err := mgr.Start(cmd.Context()); err != nil {
				log.Error(err, "problem running manager")
				os.Exit(1)
			}
		},
	}

	setupLog(cmd)
	addSubcommands(cmd)

	cmd.Flags().StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	cmd.Flags().StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	cmd.Flags().BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	return cmd
}

func setupLog(cmd *cobra.Command) {
	log := ctrl.Log.WithName("cli")

	zaplog := zap.New(zap.UseDevMode(true))
	ctrl.SetLogger(zaplog)

	ctx := ctrl.SetupSignalHandler()
	ctx = ctrl.LoggerInto(ctx, log)
	cmd.SetContext(ctx)
}

func addSubcommands(cmd *cobra.Command) {
	cmd.AddCommand(
		MigrateCmd(),
		UpgradeCmd(),
	)
}
