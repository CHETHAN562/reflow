package project_ops

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"reflow/internal/app"
	"reflow/internal/util"
	"syscall"

	"github.com/spf13/cobra"
)

// AddLogsCommand defines the logs command and adds it to the parent command.
func AddLogsCommand(parentCmd *cobra.Command) {
	var env string
	var follow bool
	var tail string

	var logsCmd = &cobra.Command{
		Use:   "logs <project-name>",
		Short: "Show logs for the active container of a project environment",
		Long: `Workspaces and displays logs from the Docker container associated with the currently
active deployment for the specified project and environment. Allows following
logs in real-time and specifying the number of tail lines.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			projectName := args[0]

			if env != "test" && env != "prod" {
				return fmt.Errorf("invalid value for --env flag: '%s'. Must be 'test' or 'prod'", env)
			}

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

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			err := app.StreamAppLogs(ctx, reflowBasePath, projectName, env, follow, tail)
			if err != nil {
				return fmt.Errorf("failed to get logs")
			}

			return nil
		},
	}

	logsCmd.Flags().StringVar(&env, "env", "test", "Specify environment ('test' or 'prod')")
	logsCmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	logsCmd.Flags().StringVar(&tail, "tail", "100", "Number of lines to show from the end of the logs")

	parentCmd.AddCommand(logsCmd)
}
