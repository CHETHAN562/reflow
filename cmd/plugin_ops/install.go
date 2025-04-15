package plugin_ops

import (
	"fmt"
	"os"
	"path/filepath"
	"reflow/internal/plugin"
	"reflow/internal/util"

	"github.com/spf13/cobra"
)

// AddInstallCommand defines the install command for plugins.
func AddInstallCommand(parentCmd *cobra.Command) {
	var installCmd = &cobra.Command{
		Use:   "install <git-repo-url>",
		Short: "Install a new plugin from a Git repository",
		Long: `Clones the specified Git repository, parses the plugin metadata (reflow-plugin.yaml),
runs any defined setup prompts, and registers the plugin with Reflow.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			repoURL := args[0]

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

			err := plugin.InstallPlugin(reflowBasePath, repoURL)
			if err != nil {
				util.Log.Errorf("Plugin installation failed: %v", err)
				return err
			}

			return nil
		},
	}

	parentCmd.AddCommand(installCmd)
}
