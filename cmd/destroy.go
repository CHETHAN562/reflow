package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflow/internal/orchestrator"
	"reflow/internal/util"

	"github.com/spf13/cobra"
)

// AddDestroyCommand defines the destroy command and adds it to the root command.
func AddDestroyCommand(rootCmd *cobra.Command) {
	var force bool

	var destroyCmd = &cobra.Command{
		Use:   "destroy",
		Short: "Permanently destroy the entire Reflow setup",
		Long: `WARNING: This command is destructive and irreversible!

It stops and removes ALL Reflow managed containers (application containers and the
reflow-nginx container), removes the reflow-network Docker network, and then
completely deletes the Reflow base directory (usually './reflow/'), including all
configurations, state files, cloned repositories, logs, and any other associated data.

Use with extreme caution. Requires confirmation unless '--force' is used.`,
		Args: cobra.NoArgs,
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			configFlag, _ := cobraCmd.Root().PersistentFlags().GetString("config")
			var reflowBasePath string
			var pathErr error
			if configFlag == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("failed to get current working directory: %w", err)
				}
				reflowBasePath = filepath.Join(cwd, "reflow")
			} else {
				reflowBasePath, pathErr = filepath.Abs(configFlag)
				if pathErr != nil {
					return fmt.Errorf("failed to get absolute path for --config flag: %w", pathErr)
				}
			}
			util.Log.Debugf("Using reflow base path for destruction: %s", reflowBasePath)

			ctx := context.Background()

			err := orchestrator.DestroyReflow(ctx, reflowBasePath, force)
			if err != nil {
				return fmt.Errorf("destruction process failed")
			}

			return nil
		},
	}

	destroyCmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt (use with extreme caution)")

	rootCmd.AddCommand(destroyCmd)
}
