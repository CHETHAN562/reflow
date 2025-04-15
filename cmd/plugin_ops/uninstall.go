package plugin_ops

import (
	"fmt"
	"os"
	"path/filepath"
	"reflow/internal/plugin"
	"reflow/internal/util"

	"github.com/spf13/cobra"
)

// AddUninstallCommand defines the uninstall command for plugins.
func AddUninstallCommand(parentCmd *cobra.Command) {
	var uninstallCmd = &cobra.Command{
		Use:   "uninstall <plugin-name>",
		Short: "Uninstall a Reflow plugin",
		Long: `Removes the specified plugin from Reflow. This includes stopping and removing
any associated Docker containers, removing Nginx configurations, deleting the
plugin's files, and updating the Reflow state.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			pluginName := args[0]

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

			err := plugin.UninstallPlugin(reflowBasePath, pluginName)
			if err != nil {
				util.Log.Errorf("Plugin uninstallation failed: %v", err)
				return err
			}

			return nil
		},
	}

	parentCmd.AddCommand(uninstallCmd)
}
