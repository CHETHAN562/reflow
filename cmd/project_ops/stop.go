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

// AddStopCommand defines the stop command and adds it to the parent command.
func AddStopCommand(parentCmd *cobra.Command) {
	var env string

	var stopCmd = &cobra.Command{
		Use:   "stop <project-name>",
		Short: "Stops the active container(s) for a project environment",
		Long: `Stops the running Docker container(s) associated with the currently active deployment
slot (blue/green) for the specified project and environment(s).`,
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

			util.Log.Infof("Executing 'stop' for project '%s', environment(s): %v", projectName, targetEnvs)

			var finalErr error
			for _, targetEnv := range targetEnvs {
				err := app.StopProjectEnv(ctx, reflowBasePath, projectName, targetEnv)
				if err != nil {
					util.Log.Errorf("Error stopping project '%s' env '%s': %v", projectName, targetEnv, err)
					if finalErr == nil {
						finalErr = fmt.Errorf("error stopping env '%s': %w", targetEnv, err)
					} else {
						finalErr = fmt.Errorf("%w; error stopping env '%s': %v", finalErr, targetEnv, err)
					}
				}
			}

			return finalErr
		},
	}

	stopCmd.Flags().StringVar(&env, "env", "all", "Specify environment ('test', 'prod', or 'all')")

	parentCmd.AddCommand(stopCmd)
}
