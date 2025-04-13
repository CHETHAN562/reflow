package cmd

import (
	"github.com/spf13/cobra"
	"reflow/cmd/project_ops"
)

// projectCmd represents the base command for project operations
var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Manage Reflow projects (create, list, status, etc.)",
	Long:  `Provides subcommands to manage your deployed Next.js projects within Reflow.`,
}

func init() {
	rootCmd.AddCommand(projectCmd)

	project_ops.AddCreateCommand(projectCmd)
	project_ops.AddListCommand(projectCmd)
	project_ops.AddStatusCommand(projectCmd)
	project_ops.AddStopCommand(projectCmd)
	project_ops.AddStartCommand(projectCmd)
	project_ops.AddLogsCommand(projectCmd)
	project_ops.AddCleanupCommand(projectCmd)
	project_ops.AddConfigCommand(projectCmd)
}
