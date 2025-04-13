package project_ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflow/internal/project"
	"reflow/internal/util"

	"github.com/spf13/cobra"
)

// AddStatusCommand defines the status command and adds it to the parent command.
func AddStatusCommand(parentCmd *cobra.Command) {
	var statusCmd = &cobra.Command{
		Use:     "status <project-name>",
		Short:   "Show detailed status for a specific project",
		Long:    `Displays detailed information about a specific Reflow project, including configuration paths, deployment state, and the status of associated Docker containers for both test and production environments.`,
		Aliases: []string{"info"},
		Args:    cobra.ExactArgs(1),
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

			// --- Get Project Details ---
			details, err := project.GetProjectDetails(ctx, reflowBasePath, projectName)
			if err != nil {
				return fmt.Errorf("failed to get status for project '%s': %w", projectName, err)
			}

			// --- Print Details ---
			fmt.Printf("Project Status: %s\n", details.Name)
			fmt.Printf("  Repository:   %s\n", details.RepoURL)
			fmt.Printf("  Local Path:   %s\n", details.LocalRepoPath)
			fmt.Printf("  Config File:  %s\n", details.ConfigFilePath)
			fmt.Printf("  State File:   %s\n", details.StateFilePath)
			fmt.Println("---")

			printEnvDetails("Test Environment", details.TestDetails)
			fmt.Println("---")
			printEnvDetails("Prod Environment", details.ProdDetails)

			return nil
		},
	}

	parentCmd.AddCommand(statusCmd)
}

func printEnvDetails(title string, details project.EnvironmentDetails) {
	fmt.Printf("%s:\n", title)
	fmt.Printf("  Deployed:        %v\n", details.IsActive)
	fmt.Printf("  Active Slot:     %s\n", details.ActiveSlot)
	fmt.Printf("  Active Commit:   %s\n", details.ActiveCommit)
	fmt.Printf("  Domain:          %s\n", details.EffectiveDomain)
	fmt.Printf("  App Port:        %d\n", details.AppPort)
	fmt.Printf("  Env File Path:   %s\n", details.EnvFilePath)
	fmt.Printf("  Container ID:    %s\n", details.ContainerID)
	fmt.Printf("  Container Names: %v\n", details.ContainerNames)
	fmt.Printf("  Container Status:%s\n", details.ContainerStatus)
}
