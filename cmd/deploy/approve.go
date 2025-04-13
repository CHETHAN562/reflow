package deploy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflow/internal/orchestrator"
	"reflow/internal/util"

	"github.com/spf13/cobra"
)

// AddApproveCommand defines the approval command and adds it to the root command.
func AddApproveCommand(rootCmd *cobra.Command) {
	var approveCmd = &cobra.Command{
		Use:   "approve <project-name>",
		Short: "Promotes the current 'test' deployment to 'production'",
		Long: `Takes the currently active commit in the 'test' environment for the specified project,
deploys the corresponding Docker image to the inactive 'prod' environment slot (blue/green),
waits for it to become healthy, and then switches live production traffic by updating Nginx.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			projectName := args[0]
			ctx := context.Background()

			configFlag, _ := cobraCmd.Root().PersistentFlags().GetString("config")
			var reflowBasePath string
			var err error
			if configFlag == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("failed to get current working directory: %w", err)
				}
				reflowBasePath = filepath.Join(cwd, "reflow")
			} else {
				reflowBasePath, err = filepath.Abs(configFlag)
				if err != nil {
					return fmt.Errorf("failed to get absolute path for --config flag: %w", err)
				}
			}
			util.Log.Debugf("Using reflow base path: %s", reflowBasePath)

			// --- Call Orchestration Logic ---
			err = orchestrator.ApproveProd(ctx, reflowBasePath, projectName)
			if err != nil {
				util.Log.Errorf("Approval process failed: %v", err)
				return err
			}

			return nil
		},
	}

	rootCmd.AddCommand(approveCmd)
}
