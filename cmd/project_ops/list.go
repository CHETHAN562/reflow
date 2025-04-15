package project_ops

import (
	"fmt"
	"os"
	"path/filepath"
	"reflow/internal/project"
	"reflow/internal/util"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// AddListCommand defines the list command and adds it to the parent command.
func AddListCommand(parentCmd *cobra.Command) {
	var listCmd = &cobra.Command{
		Use:     "list",
		Short:   "List all configured Reflow projects and their status",
		Long:    `Scans the Reflow apps directory and displays a summary of each configured project, including its deployment status for test and production environments.`,
		Aliases: []string{"ls"},
		Args:    cobra.NoArgs,
		RunE: func(cobraCmd *cobra.Command, args []string) error {
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

			summaries, err := project.ListProjects(reflowBasePath)
			if err != nil {
				return fmt.Errorf("failed to list projects: %w", err)
			}

			if len(summaries) == 0 {
				util.Log.Info("No projects found.")
				return nil
			}

			util.Log.Info("Configured Projects:")
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "NAME\tREPOSITORY\tTEST STATUS\tPROD STATUS")
			fmt.Fprintln(w, "----\t----------\t-----------\t-----------")
			for _, s := range summaries {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Name, s.RepoURL, s.TestStatus, s.ProdStatus)
			}
			err = w.Flush()
			if err != nil {
				util.Log.Errorf("Failed to flush tabwriter: %v", err)
				return err
			}

			return nil
		},
	}

	parentCmd.AddCommand(listCmd)
}
