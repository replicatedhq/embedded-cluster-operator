package cli

import (
	"fmt"

	"github.com/replicatedhq/embedded-cluster-operator/pkg/k8sutil"
	"github.com/replicatedhq/embedded-cluster-operator/pkg/upgrade"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
)

func UpgradeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "upgrade",
		Short:        "Upgrade the embedded cluster operator",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			log := ctrl.LoggerFrom(cmd.Context())

			log.Info("Upgrade command started")

			cli, err := k8sutil.KubeClient()
			if err != nil {
				return fmt.Errorf("failed to create kubernetes client: %w", err)
			}

			err = upgrade.Upgrade(cmd.Context(), cli)
			if err != nil {
				return fmt.Errorf("failed to upgrade: %w", err)
			}

			log.Info("Upgrade command completed successfully")
			return nil
		},
	}

	return cmd
}
