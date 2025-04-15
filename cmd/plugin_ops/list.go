package plugin_ops

import (
	"fmt"
	"os"
	"path/filepath"
	"reflow/internal/plugin"
	"reflow/internal/util"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// AddListCommand defines the list command for plugins.
func AddListCommand(parentCmd *cobra.Command) {
	var listCmd = &cobra.Command{
		Use:     "list",
		Short:   "List installed Reflow plugins",
		Long:    `Displays a summary of all plugins currently installed in the Reflow environment.`,
		Aliases: []string{"ls"},
		Args:    cobra.NoArgs,
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
			util.Log.Debugf("Using reflow base path: %s", reflowBasePath)

			plugins, err := plugin.ListInstalledPlugins(reflowBasePath)
			if err != nil {
				return fmt.Errorf("failed to list plugins: %w", err)
			}

			if len(plugins) == 0 {
				util.Log.Info("No plugins installed.")
				return nil
			}

			util.Log.Info("Installed Plugins:")
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "NAME\tDISPLAY NAME\tVERSION\tTYPE\tENABLED\tREPO URL")
			fmt.Fprintln(w, "----\t------------\t-------\t----\t-------\t--------")
			for _, p := range plugins {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%v\t%s\n",
					p.PluginName,
					p.DisplayName,
					p.Version,
					p.Type,
					p.Enabled,
					p.RepoURL)
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
