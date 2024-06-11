package cli

import (
	"fmt"

	"github.com/replicatedhq/embedded-cluster-operator/pkg/migrations"
	"github.com/spf13/cobra"
)

func MigrateCmd() *cobra.Command {
	var migration string

	cmd := &cobra.Command{
		Use:          "migrate",
		Short:        "Run the specified migration",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := migrations.Run(cmd.Context(), migration)
			if err != nil {
				return fmt.Errorf("migration %q failed: %w", migration, err)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&migration, "migration", "", "The migration to run")
	err := cmd.MarkFlagRequired("migration")
	if err != nil {
		panic(err)
	}

	return cmd
}
