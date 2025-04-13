package project_ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflow/internal/app"
	"reflow/internal/util"
	"strings"

	"github.com/spf13/cobra"
)

// AddStartCommand defines the start command and adds it to the parent command.
func AddStartCommand(parentCmd *cobra.Command) {
	var env string

	var startCmd = &cobra.Command{
		Use:   "start <project-name>",
		Short: "Starts previously stopped active container(s) for a project environment",
		Long: `Starts the Docker container(s) associated with the currently active deployment
slot (blue/green) that were previously stopped using the 'stop' command.
It only starts containers that match the active deployment state.`,
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

			util.Log.Infof("Executing 'start' for project '%s', environment(s): %v", projectName, targetEnvs)

			var finalErr error
			for _, targetEnv := range targetEnvs {
				err := app.StartProjectEnv(ctx, reflowBasePath, projectName, targetEnv)
				if err != nil {
					util.Log.Errorf("Error starting project '%s' env '%s': %v", projectName, targetEnv, err)
					if finalErr == nil {
						finalErr = fmt.Errorf("error starting env '%s': %w", targetEnv, err)
					} else {
						finalErr = fmt.Errorf("%w; error starting env '%s': %v", finalErr, targetEnv, err)
					}
				}
			}

			return finalErr
		},
	}

	startCmd.Flags().StringVar(&env, "env", "all", "Specify environment ('test', 'prod', or 'all')")

	parentCmd.AddCommand(startCmd)
}
