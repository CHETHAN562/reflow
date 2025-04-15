package project_ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflow/internal/orchestrator"
	"reflow/internal/util"
	"strings"

	"github.com/spf13/cobra"
)

// AddCleanupCommand defines the cleanup command and adds it to the parent command.
func AddCleanupCommand(parentCmd *cobra.Command) {
	var env string
	var pruneImages bool

	var cleanupCmd = &cobra.Command{
		Use:   "cleanup <project-name>",
		Short: "Removes inactive containers and optionally images for a project",
		Long: `Cleans up resources associated with inactive deployments for a project.
Specifically, it finds and removes Docker containers that do not correspond
to the currently active deployment slot and commit hash in the specified environment(s).

Use the --prune-images flag cautiously to also remove Docker images associated
with commits that are no longer active in either 'test' or 'prod' for this project.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			projectName := args[0]
			ctx := context.Background()

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
			util.Log.Debugf("Using reflow base path: %s", reflowBasePath)

			var targetEnvs []string
			switch strings.ToLower(env) {
			case "test":
				targetEnvs = []string{"test"}
			case "prod":
				targetEnvs = []string{"prod"}
			case "all":
				targetEnvs = []string{"test", "prod"}
			default:
				return fmt.Errorf("invalid value for --env flag: %s. Must be 'test', 'prod', or 'all'", env)
			}

			util.Log.Infof("Executing 'cleanup' for project '%s', environment(s): %v", projectName, targetEnvs)

			var finalErr error
			totalCleanedContainers := 0

			for _, targetEnv := range targetEnvs {
				cleanedCount, err := orchestrator.CleanupProjectEnv(ctx, reflowBasePath, projectName, targetEnv)
				totalCleanedContainers += cleanedCount
				if err != nil {
					util.Log.Errorf("Error cleaning project '%s' env '%s': %v", projectName, targetEnv, err)
					if finalErr == nil {
						finalErr = fmt.Errorf("error cleaning env '%s': %w", targetEnv, err)
					} else {
						finalErr = fmt.Errorf("%w; error cleaning env '%s': %v", finalErr, targetEnv, err)
					}
				}
			}

			totalPrunedImages := 0
			if pruneImages {
				prunedCount, err := orchestrator.PruneProjectImages(ctx, reflowBasePath, projectName)
				totalPrunedImages = prunedCount
				if err != nil {
					util.Log.Errorf("Error pruning images for project '%s': %v", projectName, err)
					if finalErr == nil {
						finalErr = fmt.Errorf("error pruning images: %w", err)
					} else {
						finalErr = fmt.Errorf("%w; error pruning images: %v", finalErr, err)
					}
				}
			}

			util.Log.Infof("Cleanup summary for '%s': Removed %d container(s), Pruned %d image(s).", projectName, totalCleanedContainers, totalPrunedImages)

			return finalErr
		},
	}

	cleanupCmd.Flags().StringVar(&env, "env", "all", "Specify environment for container cleanup ('test', 'prod', or 'all')")
	cleanupCmd.Flags().BoolVar(&pruneImages, "prune-images", false, "Also remove docker images for inactive commits (use with caution)")

	parentCmd.AddCommand(cleanupCmd)
}
