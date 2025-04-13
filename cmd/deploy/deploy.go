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

// AddDeployCommand defines the deploy command and adds it to the root command.
func AddDeployCommand(rootCmd *cobra.Command) {
	var deployCmd = &cobra.Command{
		Use:   "deploy <project-name> [commit-ish]",
		Short: "Deploys a project version to the 'test' environment",
		Long: `Builds the specified commit (or the latest if none provided) for the given project,
deploys it to the inactive 'test' environment slot (blue/green), waits for it
to become healthy, and then switches live traffic by updating the Nginx configuration.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			projectName := args[0]
			commitIsh := ""
			if len(args) > 1 {
				commitIsh = args[1]
			}

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
			err = orchestrator.DeployTest(ctx, reflowBasePath, projectName, commitIsh)
			if err != nil {
				util.Log.Errorf("Deployment failed: %v", err)
				return err
			}

			return nil
		},
	}

	rootCmd.AddCommand(deployCmd)
}
